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
	"Key", "Summary", "Description", "Status", "Priority", "Labels", "Folder",
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
		`SELECT jira_key, jira_id, summary, description, status, priority, labels, components, updated_at, folder_id
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
			strings.Join(t.Labels, " "), t.FolderID,
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
	sheet := f.GetSheetName(0)
	for rIdx, row := range rows {
		for cIdx, val := range row {
			cell, err := excelize.CoordinatesToCellName(cIdx+1, rIdx+1)
			if err != nil {
				return nil, fmt.Errorf("xlsx cell: %w", err)
			}
			if err := f.SetCellStr(sheet, cell, val); err != nil {
				return nil, fmt.Errorf("xlsx set: %w", err)
			}
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, fmt.Errorf("write XLSX: %w", err)
	}
	return buf.Bytes(), nil
}
