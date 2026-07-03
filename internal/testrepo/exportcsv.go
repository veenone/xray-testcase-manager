package testrepo

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

// exportHeader is the column order for exported Tests (FR-10.8). It mirrors the
// import columns (plus Key / Status) so an export round-trips through import.
var exportHeader = []string{
	"Key", "Summary", "Description", "Status", "Priority", "Labels", "Components", "Folder",
}

// ListTestsForExport returns every Test matching a query (no pagination),
// ordered the same way the grid is — the rows an export writes (FR-10.8).
func (r *Repository) ListTestsForExport(profileID string, q Query) ([]TestCase, error) {
	whereSQL, args := buildTestFilter(profileID, q)
	sortCol, ok := sortColumns[q.SortBy]
	if !ok {
		sortCol = "jira_key"
	}
	dir := "ASC"
	if q.Desc {
		dir = "DESC"
	}
	listSQL := fmt.Sprintf(
		`SELECT jira_key, jira_id, summary, description, status, priority, labels, components, updated_at, folder_id, exec_type, fix_versions
		 FROM test_case %s ORDER BY %s %s`, whereSQL, sortCol, dir)

	rows, err := r.db.Query(listSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("list tests for export: %w", err)
	}
	defer rows.Close()

	out := []TestCase{}
	for rows.Next() {
		t, err := scanTest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ExportTests renders the Tests matching a query as a CSV or XLSX file's bytes
// (FR-10.8). format is "csv" or "xlsx".
func (r *Repository) ExportTests(profileID string, q Query, format string) ([]byte, error) {
	tests, err := r.ListTestsForExport(profileID, q)
	if err != nil {
		return nil, err
	}
	rows := make([][]string, 0, len(tests)+1)
	rows = append(rows, exportHeader)
	for _, t := range tests {
		rows = append(rows, []string{
			t.Key, t.Summary, t.Description, t.Status, t.Priority,
			strings.Join(t.Labels, " "), strings.Join(t.Components, ", "), t.FolderID,
		})
	}
	if format == "xlsx" {
		return writeXLSX(rows)
	}
	return writeCSV(rows)
}

func writeCSV(rows [][]string) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.WriteAll(rows); err != nil {
		return nil, fmt.Errorf("write CSV: %w", err)
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("flush CSV: %w", err)
	}
	return buf.Bytes(), nil
}

func writeXLSX(rows [][]string) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	st, err := newSheetStyles(f)
	if err != nil {
		return nil, err
	}
	sheet := f.GetSheetName(0)
	if err := fillSheet(f, sheet, rows, st); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, fmt.Errorf("write XLSX: %w", err)
	}
	return buf.Bytes(), nil
}

// namedRows is one sheet in a multi-sheet XLSX export: a sheet name, a header
// row, and the data rows beneath it.
type namedRows struct {
	Name   string
	Header []string
	Rows   [][]string
}

// writeXLSXSheets renders one excelize sheet per namedRows entry and returns the
// workbook bytes. The default Sheet1 is removed unless a sheet is named "Sheet1".
func writeXLSXSheets(sheets []namedRows) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	defaultSheet := f.GetSheetName(0)
	st, err := newSheetStyles(f)
	if err != nil {
		return nil, err
	}

	keepDefault := false
	for _, s := range sheets {
		if s.Name == defaultSheet {
			keepDefault = true
			break
		}
	}

	for _, s := range sheets {
		if s.Name != defaultSheet {
			if _, err := f.NewSheet(s.Name); err != nil {
				return nil, fmt.Errorf("xlsx sheet %q: %w", s.Name, err)
			}
		}
		rows := make([][]string, 0, len(s.Rows)+1)
		if len(s.Header) > 0 {
			rows = append(rows, s.Header)
		}
		rows = append(rows, s.Rows...)
		if err := fillSheet(f, s.Name, rows, st); err != nil {
			return nil, err
		}
	}

	if !keepDefault {
		if err := f.DeleteSheet(defaultSheet); err != nil {
			return nil, fmt.Errorf("xlsx remove default sheet: %w", err)
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, fmt.Errorf("write XLSX: %w", err)
	}
	return buf.Bytes(), nil
}

// NamedRows is the exported alias of namedRows so sibling packages (the coverage
// module) can build multi-sheet workbooks with the same styling.
type NamedRows = namedRows

// WriteXLSXSheets renders a styled multi-sheet workbook — the exported entry
// point over writeXLSXSheets for the coverage module's report export.
func WriteXLSXSheets(sheets []NamedRows) ([]byte, error) { return writeXLSXSheets(sheets) }

// sheetStyles holds the cell styles fillSheet applies: a banded header plus
// zebra-striped data rows, all bordered and word-wrapped, mirroring the bug
// export so every workbook the app produces reads consistently.
type sheetStyles struct {
	header int
	odd    int
	even   int
}

// sheetBorderColor is the thin grid line drawn around every exported cell.
const sheetBorderColor = "BFBFBF"

// newSheetStyles registers the export cell styles on a workbook once, so every
// sheet written into it reuses the same style ids.
func newSheetStyles(f *excelize.File) (sheetStyles, error) {
	borders := []excelize.Border{
		{Type: "left", Color: sheetBorderColor, Style: 1},
		{Type: "top", Color: sheetBorderColor, Style: 1},
		{Type: "right", Color: sheetBorderColor, Style: 1},
		{Type: "bottom", Color: sheetBorderColor, Style: 1},
	}
	wrapTop := &excelize.Alignment{Vertical: "top", WrapText: true}
	header, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"305496"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
		Border:    borders,
	})
	if err != nil {
		return sheetStyles{}, fmt.Errorf("new header style: %w", err)
	}
	odd, err := f.NewStyle(&excelize.Style{Alignment: wrapTop, Border: borders})
	if err != nil {
		return sheetStyles{}, fmt.Errorf("new row style: %w", err)
	}
	even, err := f.NewStyle(&excelize.Style{
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"EFF3FB"}, Pattern: 1},
		Alignment: wrapTop,
		Border:    borders,
	})
	if err != nil {
		return sheetStyles{}, fmt.Errorf("new band style: %w", err)
	}
	return sheetStyles{header: header, odd: odd, even: even}, nil
}

// columnWidth returns a content-fit width for column col (0-based) across rows,
// clamped to a readable range so one long cell can't make a column unwieldy.
func columnWidth(rows [][]string, col int) float64 {
	const (
		minWidth = 10.0
		maxWidth = 60.0
		padding  = 2.0
	)
	longest := 0
	for _, row := range rows {
		if col < len(row) {
			if n := len([]rune(row[col])); n > longest {
				longest = n
			}
		}
	}
	w := float64(longest) + padding
	switch {
	case w < minWidth:
		return minWidth
	case w > maxWidth:
		return maxWidth
	default:
		return w
	}
}

// fillSheet writes rows into the named sheet as a styled table: row 0 is the
// banded header; data rows are zebra-striped; every cell is bordered and
// word-wrapped; columns are sized to their content; the header row is frozen.
func fillSheet(f *excelize.File, sheet string, rows [][]string, st sheetStyles) error {
	if len(rows) == 0 {
		return nil
	}
	maxCols := 0
	for _, row := range rows {
		if len(row) > maxCols {
			maxCols = len(row)
		}
	}
	for rIdx, row := range rows {
		for cIdx, val := range row {
			cell, err := excelize.CoordinatesToCellName(cIdx+1, rIdx+1)
			if err != nil {
				return fmt.Errorf("xlsx cell: %w", err)
			}
			if err := f.SetCellStr(sheet, cell, val); err != nil {
				return fmt.Errorf("xlsx set: %w", err)
			}
		}
	}
	lastCol, err := excelize.ColumnNumberToName(maxCols)
	if err != nil {
		return fmt.Errorf("xlsx column name: %w", err)
	}
	for rIdx := range rows {
		style := st.odd
		switch {
		case rIdx == 0:
			style = st.header
		case rIdx%2 == 0:
			style = st.even
		}
		left := fmt.Sprintf("A%d", rIdx+1)
		right := fmt.Sprintf("%s%d", lastCol, rIdx+1)
		if err := f.SetCellStyle(sheet, left, right, style); err != nil {
			return fmt.Errorf("xlsx style row: %w", err)
		}
	}
	for c := 1; c <= maxCols; c++ {
		col, err := excelize.ColumnNumberToName(c)
		if err != nil {
			return fmt.Errorf("xlsx column name: %w", err)
		}
		if err := f.SetColWidth(sheet, col, col, columnWidth(rows, c-1)); err != nil {
			return fmt.Errorf("xlsx column width: %w", err)
		}
	}
	if err := f.SetPanes(sheet, &excelize.Panes{
		Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft",
	}); err != nil {
		return fmt.Errorf("xlsx freeze header: %w", err)
	}
	return nil
}
