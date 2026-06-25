package testrepo

import (
	"bytes"
	"testing"

	"github.com/xuri/excelize/v2"
)

// TestBuildBugExportWorkbook verifies the two-sheet bug export: a "Bugs" sheet
// with one row per bug (and the affected-test count), and a "Run History" sheet
// with one row per (bug, test, run) -- plus one blank-run row for an affected
// test that has no run history yet. It also checks that the sub-task execution's
// type and parent columns round-trip.
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
						ExecIssueType:     "Sub Test Execution",
						ExecParentKey:     "QA-9",
						ExecParentSummary: "Story 9",
						RunStatus:         "FAIL",
						FixVersions:       []string{"1.5.0"},
						Environment:       "Staging",
						FinishedAt:        "2024-02-01T10:00:00Z",
						ExecutedBy:        "alice",
						PlanKeys:          []string{"QA-TP-1"},
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

	have := map[string]bool{}
	for _, n := range f.GetSheetList() {
		have[n] = true
	}
	if !have["Bugs"] || !have["Run History"] {
		t.Fatalf("expected Bugs and Run History sheets, got %v", f.GetSheetList())
	}

	// Bugs sheet: header + one bug row.
	if v, _ := f.GetCellValue("Bugs", "A2"); v != "BUGS-100" {
		t.Errorf("Bugs A2 = %q, want BUGS-100", v)
	}
	// Affected Test Count is the 13th column (M).
	if v, _ := f.GetCellValue("Bugs", "M2"); v != "2" {
		t.Errorf("Bugs affected-count M2 = %q, want 2", v)
	}

	// Run History sheet: one run row for QA-1 plus one blank-run row for QA-2.
	rows, err := f.GetRows("Run History")
	if err != nil {
		t.Fatalf("get Run History rows: %v", err)
	}
	if len(rows) != 3 { // header + QA-1 run + QA-2 blank
		t.Fatalf("Run History rows = %d, want 3 (header + 2): %v", len(rows), rows)
	}
	// Columns: Bug, Test, Test Summary, Test Project, Execution, Execution Type,
	// Parent Key, Parent Summary, Result, Fix Version(s), Environment, Run Date, ...
	run := rows[1]
	if run[1] != "QA-1" || run[4] != "QA-STE-1" {
		t.Errorf("run row test/exec = %q/%q, want QA-1/QA-STE-1", run[1], run[4])
	}
	if run[5] != "Sub Test Execution" || run[6] != "QA-9" {
		t.Errorf("run row exec type/parent = %q/%q, want Sub Test Execution/QA-9", run[5], run[6])
	}
	if run[11] != "2024-02-01T10:00:00Z" {
		t.Errorf("run row Run Date = %q, want the finished timestamp", run[11])
	}
	// The blank-run row for QA-2: test present, execution column empty.
	// excelize's GetRows trims trailing empty cells, so the all-empty run
	// columns collapse the row to just the bug/test/summary/project prefix.
	blank := rows[2]
	if blank[1] != "QA-2" {
		t.Errorf("blank row test = %q, want QA-2 (full row %v)", blank[1], blank)
	}
	if len(blank) > 4 && blank[4] != "" {
		t.Errorf("blank row execution = %q, want empty", blank[4])
	}
}
