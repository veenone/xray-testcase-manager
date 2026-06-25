package testrepo

import (
	"bytes"
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

func safeIdx(row []string, i int) string {
	if i < len(row) {
		return row[i]
	}
	return ""
}
