package testrepo

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

// TestBuildBugExportWorkbook verifies the single-sheet collapsible outline bug
// export. The "Bug Report" sheet must contain a Bug row (level 0), one Test row
// per affected test (level 1), and one Execution row per run or a "(no runs)"
// placeholder row (level 2).
func TestBuildBugExportWorkbook(t *testing.T) {
	r := newTestRepo(t)

	exports := []BugExport{
		{
			Key:               "BUGS-100",
			ProjectKey:        "BUGS",
			IssueType:         "Bug",
			Status:            "Open",
			Priority:          "High",
			Severity:          "Major",
			Reporter:          "Alice QA",
			Summary:           "Login crashes",
			Description:       "Steps to reproduce ...",
			DefectOrigin:      "Requirements",
			DefectAnalysis:    "Edge case unhandled",
			CorrectionDetails: "Guard added",
			AffectedTests: []BugTest{
				{Key: "QA-1", Project: "QA", Summary: "Login", Status: "Done", RunStatus: "FAIL"},
				{Key: "QA-2", Project: "QA", Summary: "Logout", Status: "Done", RunStatus: ""},
			},
			RunHistory: map[string][]TestRunEntry{
				"QA-1": {
					{
						ExecKey:           "QA-STE-1",
						ExecSummary:       "Sprint 1 cycle",
						ExecIssueType:     "Sub Test Execution",
						ExecParentKey:     "QA-9",
						ExecParentSummary: "Story 9",
						RunStatus:         "FAIL",
						FixVersions:       []string{"1.5.0"},
						Environment:       "Staging",
						FinishedAt:        "2024-02-01T10:00:00Z",
						ExecutedBy:        "alice",
						Defects:           []string{"BUGS-100"},
					},
				},
				// QA-2 intentionally has no run history.
			},
		},
	}

	data, err := r.BuildBugExportWorkbook(exports)
	if err != nil {
		t.Fatalf("BuildBugExportWorkbook: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	defer func() { _ = f.Close() }()

	// Sheet "Bug Report" must exist.
	sheets := f.GetSheetList()
	hasBugReport := false
	for _, s := range sheets {
		if s == "Bug Report" {
			hasBugReport = true
		}
	}
	if !hasBugReport {
		t.Fatalf("sheet 'Bug Report' not found; sheets = %v", sheets)
	}

	// Row sequence: header(1), bug(2), test-QA1(3), exec-QA1(4), test-QA2(5), exec-norun(6).
	rows, err := f.GetRows("Bug Report")
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	// 6 rows total (header + bug + 2 tests each with 1 exec/no-run row).
	if len(rows) != 6 {
		t.Fatalf("row count = %d, want 6; rows=%v", len(rows), rows)
	}

	// Row 2 = Bug row.
	bugRow := rows[1]
	if len(bugRow) < 1 || bugRow[0] != "Bug" {
		t.Errorf("row 2 Type = %q, want Bug", safeIdx(bugRow, 0))
	}
	if safeIdx(bugRow, 1) != "BUGS-100" {
		t.Errorf("row 2 Key = %q, want BUGS-100", safeIdx(bugRow, 1))
	}

	// Row 3 = Test row for QA-1 (outline level 1).
	testRow := rows[2]
	if safeIdx(testRow, 0) != "Test" {
		t.Errorf("row 3 Type = %q, want Test", safeIdx(testRow, 0))
	}
	if safeIdx(testRow, 1) != "QA-1" {
		t.Errorf("row 3 Key = %q, want QA-1", safeIdx(testRow, 1))
	}
	level, err := f.GetRowOutlineLevel("Bug Report", 3)
	if err != nil {
		t.Fatalf("GetRowOutlineLevel row 3: %v", err)
	}
	if level != 1 {
		t.Errorf("row 3 outline level = %d, want 1", level)
	}

	// Row 4 = Execution row for QA-1's run (outline level 2).
	execRow := rows[3]
	if safeIdx(execRow, 0) != "Execution" {
		t.Errorf("row 4 Type = %q, want Execution", safeIdx(execRow, 0))
	}
	if safeIdx(execRow, 1) != "QA-STE-1" {
		t.Errorf("row 4 Key = %q, want QA-STE-1", safeIdx(execRow, 1))
	}
	level, err = f.GetRowOutlineLevel("Bug Report", 4)
	if err != nil {
		t.Fatalf("GetRowOutlineLevel row 4: %v", err)
	}
	if level != 2 {
		t.Errorf("row 4 outline level = %d, want 2", level)
	}

	// Row 5 = Test row for QA-2 (outline level 1).
	test2Row := rows[4]
	if safeIdx(test2Row, 0) != "Test" {
		t.Errorf("row 5 Type = %q, want Test", safeIdx(test2Row, 0))
	}
	if safeIdx(test2Row, 1) != "QA-2" {
		t.Errorf("row 5 Key = %q, want QA-2", safeIdx(test2Row, 1))
	}
	level, err = f.GetRowOutlineLevel("Bug Report", 5)
	if err != nil {
		t.Fatalf("GetRowOutlineLevel row 5: %v", err)
	}
	if level != 1 {
		t.Errorf("row 5 outline level = %d, want 1", level)
	}

	// Row 6 = no-run execution row (outline level 2).
	noRunRow := rows[5]
	if safeIdx(noRunRow, 0) != "Execution" {
		t.Errorf("row 6 Type = %q, want Execution", safeIdx(noRunRow, 0))
	}
	// excelize trims trailing empty cells, so Key may be absent.
	// Summary column (C = index 2) should be "(no runs)".
	if safeIdx(noRunRow, 2) != "(no runs)" {
		t.Errorf("row 6 Summary = %q, want (no runs)", safeIdx(noRunRow, 2))
	}
	level, err = f.GetRowOutlineLevel("Bug Report", 6)
	if err != nil {
		t.Fatalf("GetRowOutlineLevel row 6: %v", err)
	}
	if level != 2 {
		t.Errorf("row 6 outline level = %d, want 2", level)
	}
}

// TestBugExportSetsSheetOutlineLevelRow asserts the worksheet declares its
// outline depth (sheetFormatPr/@outlineLevelRow), without which some Excel
// builds render the row groups flat (no collapse +/- controls).
func TestBugExportSetsSheetOutlineLevelRow(t *testing.T) {
	r := newTestRepo(t)
	exports := []BugExport{{
		Key: "BUGS-1", ProjectKey: "BUGS", IssueType: "Bug", Status: "Open", Summary: "x",
		AffectedTests: []BugTest{{Key: "QA-1", Project: "QA", Summary: "Login", Status: "Done"}},
		RunHistory: map[string][]TestRunEntry{
			"QA-1": {{ExecKey: "QA-STE-1", ExecSummary: "c", RunStatus: "FAIL"}},
		},
	}}
	data, err := r.BuildBugExportWorkbook(exports)
	if err != nil {
		t.Fatalf("BuildBugExportWorkbook: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip read: %v", err)
	}
	var sheet string
	for _, f := range zr.File {
		if f.Name != "xl/worksheets/sheet1.xml" {
			continue
		}
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		sheet = string(b)
	}
	if sheet == "" {
		t.Fatal("sheet1.xml not found in workbook")
	}
	if !strings.Contains(sheet, `outlineLevelRow="2"`) {
		t.Errorf("sheetFormatPr is missing outlineLevelRow=\"2\"; got: %s", sheetFormatPrOf(sheet))
	}
}

// TestBugExportRowsAreStyled asserts each outline tier (bug/test/execution) gets
// its own cell style, and that the style enables word wrap and borders.
func TestBugExportRowsAreStyled(t *testing.T) {
	r := newTestRepo(t)
	exports := []BugExport{{
		Key: "BUGS-1", ProjectKey: "BUGS", IssueType: "Bug", Status: "Open", Summary: "x",
		AffectedTests: []BugTest{{Key: "QA-1", Project: "QA", Summary: "Login", Status: "Done"}},
		RunHistory: map[string][]TestRunEntry{
			"QA-1": {{ExecKey: "QA-STE-1", ExecSummary: "c", RunStatus: "FAIL"}},
		},
	}}
	data, err := r.BuildBugExportWorkbook(exports)
	if err != nil {
		t.Fatalf("BuildBugExportWorkbook: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	defer func() { _ = f.Close() }()

	// Rows: 1 header, 2 bug, 3 test, 4 execution.
	bug, _ := f.GetCellStyle("Bug Report", "A2")
	test, _ := f.GetCellStyle("Bug Report", "A3")
	exec, _ := f.GetCellStyle("Bug Report", "A4")
	if bug == 0 || test == 0 || exec == 0 {
		t.Errorf("a tier is unstyled: bug=%d test=%d exec=%d", bug, test, exec)
	}
	if bug == test || test == exec || bug == exec {
		t.Errorf("tiers share a style, want distinct: bug=%d test=%d exec=%d", bug, test, exec)
	}

	st, err := f.GetStyle(bug)
	if err != nil {
		t.Fatalf("GetStyle: %v", err)
	}
	if st.Alignment == nil || !st.Alignment.WrapText {
		t.Error("bug row style does not enable word wrap")
	}
	if len(st.Border) == 0 {
		t.Error("bug row style has no borders")
	}
}

// sheetFormatPrOf extracts the <sheetFormatPr ...> element for error messages.
func sheetFormatPrOf(xml string) string {
	i := strings.Index(xml, "<sheetFormatPr")
	if i < 0 {
		return "(no sheetFormatPr)"
	}
	j := strings.IndexByte(xml[i:], '>')
	if j < 0 {
		return xml[i:]
	}
	return xml[i : i+j+1]
}

func safeIdx(row []string, i int) string {
	if i < len(row) {
		return row[i]
	}
	return ""
}
