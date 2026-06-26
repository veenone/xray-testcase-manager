package testrepo

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// BugExport carries all data needed to render one bug's rows in the XLSX export:
// the basic cached fields, lazily-fetched detail fields, and the test+run rows.
type BugExport struct {
	// Cached fields (from the bug table).
	Key        string
	ProjectKey string
	IssueType  string
	Status     string
	Priority   string
	Summary    string
	// Live-fetched fields (may be empty when the live fetch failed).
	Description       string
	DefectOrigin      string
	DefectAnalysis    string
	CorrectionDetails string
	Reporter          string
	Severity          string
	// AffectedTests holds the tests linked to this bug.
	AffectedTests []BugTest
	// RunHistory maps each affected test key to its run history entries.
	RunHistory map[string][]TestRunEntry
}

// GetBug returns the cached Bug row for a single key. Returns an error
// wrapping sql.ErrNoRows when the key is not in the local store.
func (r *Repository) GetBug(profileID, key string) (Bug, error) {
	var b Bug
	err := r.db.QueryRow(
		`SELECT jira_key, project_key, issue_type, summary, status, priority, updated_at
		 FROM bug WHERE profile_id = ? AND jira_key = ?`,
		profileID, key,
	).Scan(&b.Key, &b.ProjectKey, &b.IssueType, &b.Summary, &b.Status, &b.Priority, &b.Updated)
	if err != nil {
		return Bug{}, fmt.Errorf("get bug %s: %w", key, err)
	}
	return b, nil
}

// BuildBugExportWorkbook renders exports as a single-sheet XLSX workbook named
// "Bug Report" with a collapsible Excel row outline:
//
//	Level 0  Bug row
//	Level 1    Test row     (one per affected test)
//	Level 2      Execution  (one per run, or a "(no runs)" placeholder)
//
// The parent summary row sits ABOVE its children (OutlineSummaryBelow = false).
func (r *Repository) BuildBugExportWorkbook(exports []BugExport) ([]byte, error) {
	const sheet = "Bug Report"

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	// Rename the default sheet instead of creating a new one, so Sheet1 is gone.
	defaultSheet := f.GetSheetName(0)
	if err := f.SetSheetName(defaultSheet, sheet); err != nil {
		return nil, fmt.Errorf("rename sheet: %w", err)
	}

	// Collapse controls appear on the parent row (above its children).
	below := false
	if err := f.SetSheetProps(sheet, &excelize.SheetPropsOptions{OutlineSummaryBelow: &below}); err != nil {
		return nil, fmt.Errorf("set sheet props: %w", err)
	}

	// Column widths: A=Type, B=Key, C=Summary, D=Status/Result, E=Details,
	// F=Description, G=Defect Analysis, H=Correction Details.
	colWidths := []struct {
		start, end string
		width      float64
	}{
		{"A", "A", 12},
		{"B", "B", 14},
		{"C", "C", 40},
		{"D", "D", 16},
		{"E", "E", 60},
		{"F", "F", 40},
		{"G", "G", 30},
		{"H", "H", 30},
	}
	for _, cw := range colWidths {
		if err := f.SetColWidth(sheet, cw.start, cw.end, cw.width); err != nil {
			return nil, fmt.Errorf("set col width: %w", err)
		}
	}

	// Cell styles: a distinct fill per outline tier (header / bug / test /
	// execution) with thin borders and word wrap, so each group reads as its own
	// band and long Details / Description text stays legible instead of being
	// clipped.
	const borderColor = "BFBFBF"
	borders := []excelize.Border{
		{Type: "left", Color: borderColor, Style: 1},
		{Type: "top", Color: borderColor, Style: 1},
		{Type: "right", Color: borderColor, Style: 1},
		{Type: "bottom", Color: borderColor, Style: 1},
	}
	wrapTop := &excelize.Alignment{Vertical: "top", WrapText: true}
	newRowStyle := func(font *excelize.Font, fill string) (int, error) {
		return f.NewStyle(&excelize.Style{
			Font:      font,
			Fill:      excelize.Fill{Type: "pattern", Color: []string{fill}, Pattern: 1},
			Alignment: wrapTop,
			Border:    borders,
		})
	}
	headerStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"305496"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
		Border:    borders,
	})
	if err != nil {
		return nil, fmt.Errorf("new header style: %w", err)
	}
	bugStyle, err := newRowStyle(&excelize.Font{Bold: true, Color: "1F3864"}, "8EAADB")
	if err != nil {
		return nil, fmt.Errorf("new bug style: %w", err)
	}
	testStyle, err := newRowStyle(nil, "D9E1F2")
	if err != nil {
		return nil, fmt.Errorf("new test style: %w", err)
	}
	execStyle, err := newRowStyle(nil, "F2F2F2")
	if err != nil {
		return nil, fmt.Errorf("new exec style: %w", err)
	}

	// Row 1: header.
	headers := []string{"Type", "Key", "Summary", "Status / Result", "Details", "Description", "Defect Analysis", "Correction Details"}
	cols := []string{"A", "B", "C", "D", "E", "F", "G", "H"}
	for i, h := range headers {
		if err := f.SetCellValue(sheet, cols[i]+"1", h); err != nil {
			return nil, fmt.Errorf("set header cell: %w", err)
		}
	}
	if err := f.SetCellStyle(sheet, "A1", "H1", headerStyle); err != nil {
		return nil, fmt.Errorf("set header style: %w", err)
	}

	// Freeze the header row so it stays visible while scrolling.
	if err := f.SetPanes(sheet, &excelize.Panes{
		Freeze:      true,
		Split:       false,
		YSplit:      1,
		TopLeftCell: "A2",
		ActivePane:  "bottomLeft",
	}); err != nil {
		return nil, fmt.Errorf("freeze panes: %w", err)
	}

	rowNum := 2
	// Highest row outline level actually emitted, so the sheet's outlineLevelRow
	// can be set to match (see setSheetOutlineLevelRow).
	maxLevel := 0

	setCells := func(row int, values []string) error {
		for i, v := range values {
			if err := f.SetCellValue(sheet, fmt.Sprintf("%s%d", cols[i], row), v); err != nil {
				return err
			}
		}
		return nil
	}

	joinParts := func(parts []string) string {
		return strings.Join(parts, " | ")
	}

	// styleRow applies a tier style across the eight data columns of one row.
	styleRow := func(row, styleID int) error {
		return f.SetCellStyle(sheet, fmt.Sprintf("A%d", row), fmt.Sprintf("H%d", row), styleID)
	}

	for _, ex := range exports {
		// Bug row (outline level 0 -- no SetRowOutlineLevel call needed).
		bugParts := []string{}
		if ex.ProjectKey != "" {
			bugParts = append(bugParts, "Project: "+ex.ProjectKey)
		}
		if ex.Priority != "" {
			bugParts = append(bugParts, "Priority: "+ex.Priority)
		}
		if ex.Severity != "" {
			bugParts = append(bugParts, "Severity: "+ex.Severity)
		}
		if ex.Reporter != "" {
			bugParts = append(bugParts, "Reporter: "+ex.Reporter)
		}
		if ex.DefectOrigin != "" {
			bugParts = append(bugParts, "Defect Origin: "+ex.DefectOrigin)
		}
		bugParts = append(bugParts, "Affected tests: "+strconv.Itoa(len(ex.AffectedTests)))

		if err := setCells(rowNum, []string{
			"Bug", ex.Key, ex.Summary, ex.Status,
			joinParts(bugParts),
			ex.Description, ex.DefectAnalysis, ex.CorrectionDetails,
		}); err != nil {
			return nil, fmt.Errorf("set bug row cells: %w", err)
		}
		if err := styleRow(rowNum, bugStyle); err != nil {
			return nil, fmt.Errorf("style bug row: %w", err)
		}
		rowNum++

		for _, bt := range ex.AffectedTests {
			// Test row (outline level 1).
			if err := f.SetRowOutlineLevel(sheet, rowNum, 1); err != nil {
				return nil, fmt.Errorf("set test row outline: %w", err)
			}
			if maxLevel < 1 {
				maxLevel = 1
			}
			testParts := []string{}
			if bt.Project != "" {
				testParts = append(testParts, "Project: "+bt.Project)
			}
			if bt.RunStatus != "" {
				testParts = append(testParts, "Latest result: "+bt.RunStatus)
			}
			if err := setCells(rowNum, []string{
				"Test", bt.Key, bt.Summary, bt.Status,
				joinParts(testParts),
				"", "", "",
			}); err != nil {
				return nil, fmt.Errorf("set test row cells: %w", err)
			}
			if err := styleRow(rowNum, testStyle); err != nil {
				return nil, fmt.Errorf("style test row: %w", err)
			}
			rowNum++

			history := ex.RunHistory[bt.Key]
			if len(history) == 0 {
				// No runs: emit a single placeholder execution row.
				if err := f.SetRowOutlineLevel(sheet, rowNum, 2); err != nil {
					return nil, fmt.Errorf("set no-run outline: %w", err)
				}
				if maxLevel < 2 {
					maxLevel = 2
				}
				if err := setCells(rowNum, []string{
					"Execution", "", "(no runs)", "", "", "", "", "",
				}); err != nil {
					return nil, fmt.Errorf("set no-run cells: %w", err)
				}
				if err := styleRow(rowNum, execStyle); err != nil {
					return nil, fmt.Errorf("style no-run row: %w", err)
				}
				rowNum++
				continue
			}

			for _, run := range history {
				// Execution row (outline level 2).
				if err := f.SetRowOutlineLevel(sheet, rowNum, 2); err != nil {
					return nil, fmt.Errorf("set exec row outline: %w", err)
				}
				if maxLevel < 2 {
					maxLevel = 2
				}

				runDate := run.FinishedAt
				if runDate == "" {
					runDate = run.StartedAt
				}

				execParts := []string{}
				if run.ExecIssueType != "" {
					execParts = append(execParts, "Exec type: "+run.ExecIssueType)
				}
				if run.ExecParentKey != "" {
					execParts = append(execParts, "Parent: "+run.ExecParentKey+" "+run.ExecParentSummary)
				}
				if len(run.FixVersions) > 0 {
					execParts = append(execParts, "Fix: "+strings.Join(run.FixVersions, ", "))
				}
				if run.Environment != "" {
					execParts = append(execParts, "Env: "+run.Environment)
				}
				if runDate != "" {
					execParts = append(execParts, "Run date: "+runDate)
				}
				if run.ExecCreated != "" {
					execParts = append(execParts, "Created: "+run.ExecCreated)
				}
				if run.ExecUpdated != "" {
					execParts = append(execParts, "Updated: "+run.ExecUpdated)
				}
				if run.ExecResolved != "" {
					execParts = append(execParts, "Resolved: "+run.ExecResolved)
				}
				if run.ExecutedBy != "" {
					execParts = append(execParts, "By: "+run.ExecutedBy)
				}
				if len(run.Defects) > 0 {
					execParts = append(execParts, "Defects: "+strings.Join(run.Defects, ", "))
				}

				if err := setCells(rowNum, []string{
					"Execution", run.ExecKey, run.ExecSummary, run.RunStatus,
					joinParts(execParts),
					"", "", "",
				}); err != nil {
					return nil, fmt.Errorf("set exec row cells: %w", err)
				}
				if err := styleRow(rowNum, execStyle); err != nil {
					return nil, fmt.Errorf("style exec row: %w", err)
				}
				rowNum++
			}
		}
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("write workbook: %w", err)
	}
	if maxLevel == 0 {
		// No grouped rows (no bug had affected tests): nothing to collapse.
		return buf.Bytes(), nil
	}
	return setSheetOutlineLevelRow(buf.Bytes(), bugExportSheetXML, maxLevel)
}

// bugExportSheetXML is the worksheet part for the single sheet that
// BuildBugExportWorkbook writes. excelize names parts by creation order, so the
// first (and only) sheet is always sheet1.xml.
const bugExportSheetXML = "xl/worksheets/sheet1.xml"

// setSheetOutlineLevelRow injects sheetFormatPr/@outlineLevelRow into the named
// worksheet part inside the workbook ZIP. excelize writes the per-row
// outlineLevel attributes (the group membership) but leaves the sheet-level
// outlineLevelRow at 0, where it is omitted; some Excel builds then draw no
// row-group collapse (+/-) controls and the outline looks flat. Rewriting the
// target worksheet part with the attribute set makes the outline reliably
// collapsible, leaving every other part of the package untouched.
//
// sheetPart is the ZIP entry name for the worksheet, e.g.
// "xl/worksheets/sheet1.xml". excelize assigns parts by sheet creation order:
// the first sheet created is sheet1.xml, the second sheet2.xml, and so on.
func setSheetOutlineLevelRow(workbook []byte, sheetPart string, level int) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(workbook), int64(len(workbook)))
	if err != nil {
		return nil, fmt.Errorf("read workbook zip: %w", err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, file := range zr.File {
		rc, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", file.Name, err)
		}
		content, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", file.Name, err)
		}
		if file.Name == sheetPart {
			content = injectOutlineLevelRow(content, level)
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: file.Name, Method: zip.Deflate})
		if err != nil {
			return nil, fmt.Errorf("create %s: %w", file.Name, err)
		}
		if _, err := w.Write(content); err != nil {
			return nil, fmt.Errorf("write %s: %w", file.Name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("close workbook zip: %w", err)
	}
	return out.Bytes(), nil
}

// injectOutlineLevelRow adds an outlineLevelRow="<level>" attribute to the
// worksheet's <sheetFormatPr> element, handling both the self-closing
// (<sheetFormatPr .../>) and open-tag forms. A no-op if the attribute is already
// present or the element is absent.
func injectOutlineLevelRow(worksheet []byte, level int) []byte {
	s := string(worksheet)
	if strings.Contains(s, "outlineLevelRow=") {
		return worksheet
	}
	start := strings.Index(s, "<sheetFormatPr")
	if start < 0 {
		return worksheet
	}
	rel := strings.IndexByte(s[start:], '>')
	if rel < 0 {
		return worksheet
	}
	insertAt := start + rel // position of the closing '>'
	if insertAt > 0 && s[insertAt-1] == '/' {
		insertAt-- // keep the '/' of a self-closing tag after the new attribute
	}
	attr := fmt.Sprintf(` outlineLevelRow="%d"`, level)
	return []byte(s[:insertAt] + attr + s[insertAt:])
}
