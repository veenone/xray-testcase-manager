package testrepo

import (
	"bytes"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestWriteXLSXSheets(t *testing.T) {
	sheets := []namedRows{
		{
			Name:   "Flow",
			Header: []string{"Source", "Target", "Value"},
			Rows:   [][]string{{"Plan A", "Exec 1", "3"}},
		},
		{
			Name:   "Table",
			Header: []string{"Test Plan", "Test"},
			Rows:   [][]string{{"Plan A", "DEMO-1"}},
		},
	}
	data, err := writeXLSXSheets(sheets)
	if err != nil {
		t.Fatalf("writeXLSXSheets: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	defer func() { _ = f.Close() }()

	names := f.GetSheetList()
	have := map[string]bool{}
	for _, n := range names {
		have[n] = true
	}
	if !have["Flow"] || !have["Table"] {
		t.Fatalf("expected Flow and Table sheets, got %v", names)
	}
	if have["Sheet1"] {
		t.Errorf("default Sheet1 should be removed, got %v", names)
	}

	// Header round-trips.
	if v, _ := f.GetCellValue("Flow", "A1"); v != "Source" {
		t.Errorf("Flow A1 = %q, want Source", v)
	}
	if v, _ := f.GetCellValue("Flow", "C2"); v != "3" {
		t.Errorf("Flow C2 = %q, want 3", v)
	}
	if v, _ := f.GetCellValue("Table", "B2"); v != "DEMO-1" {
		t.Errorf("Table B2 = %q, want DEMO-1", v)
	}
}

func TestExecutionTraceabilityExportRows(t *testing.T) {
	r := newTestRepo(t)
	const p = "p1"

	// One plan and one execution sharing a member test with a run status.
	seedContainer(t, r, p, "DEMO-TP-1", "testplan", "Plan One", "Open")
	seedContainer(t, r, p, "DEMO-TE-1", "testexec", "Cycle 1", "Open")
	seedContainerTest(t, r, p, "DEMO-TP-1", "DEMO-1", "")
	seedContainerTest(t, r, p, "DEMO-TE-1", "DEMO-1", "PASS")

	flow, err := r.ExecutionFlowRows(p, "DEMO", nil, nil, false)
	if err != nil {
		t.Fatalf("ExecutionFlowRows: %v", err)
	}
	// Flow rows resolve endpoint labels, not raw node ids.
	for _, row := range flow {
		if len(row) != 3 {
			t.Fatalf("flow row should have 3 cols, got %v", row)
		}
		for _, cell := range row[:2] {
			if got := cell; len(got) >= 5 && got[:5] == "exec:" {
				t.Errorf("flow endpoint not resolved to label: %q", cell)
			}
		}
	}
	if len(flow) == 0 {
		t.Fatalf("expected flow rows")
	}

	table, err := r.ExecutionTableRows(p, "DEMO", nil, nil, false)
	if err != nil {
		t.Fatalf("ExecutionTableRows: %v", err)
	}
	if len(table) != 1 {
		t.Fatalf("expected 1 execution thread row, got %d: %v", len(table), table)
	}
	row := table[0]
	if len(row) != 4 {
		t.Fatalf("execution table row should have 4 cols, got %v", row)
	}
	// Columns: Test Plan, Test Execution, Test, Run status.
	if row[2] != "DEMO-1" {
		t.Errorf("Test col = %q, want DEMO-1", row[2])
	}
	if row[3] != "PASS" {
		t.Errorf("Run status col = %q, want PASS", row[3])
	}
}
