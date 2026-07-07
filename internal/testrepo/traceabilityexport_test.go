package testrepo

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
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

// TestWriteOutlineTreeSheet verifies the staircase de-duplication, outline
// levels, and cell placement produced by writeOutlineTreeSheet.
//
// Input: three rows with header ["A","B","C","D"]:
//
//	[TP1, TE1, T1, PASS]
//	[TP1, TE1, T2, FAIL]
//	[TP2, TE3, T1, PASS]
//
// Expected spreadsheet rows (excelize row numbers, 1-based):
//
//	1: header
//	2: A=TP1  (level 0)
//	3:   B=TE1  (level 1)
//	4:     C=T1   (level 2)
//	5:       D=PASS (level 3)
//	6:     C=T2   (level 2)
//	7:       D=FAIL (level 3)
//	8: A=TP2  (level 0)
//	9:   B=TE3  (level 1)
//	10:    C=T1   (level 2)
//	11:      D=PASS (level 3)
func TestWriteOutlineTreeSheet(t *testing.T) {
	header := []string{"A", "B", "C", "D"}
	rows := [][]string{
		{"TP1", "TE1", "T1", "PASS"},
		{"TP1", "TE1", "T2", "FAIL"},
		{"TP2", "TE3", "T1", "PASS"},
	}

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	// Rename default sheet to "Table" for isolation.
	defaultSheet := f.GetSheetName(0)
	if err := f.SetSheetName(defaultSheet, "Table"); err != nil {
		t.Fatalf("rename sheet: %v", err)
	}

	maxLevel, err := writeOutlineTreeSheet(f, "Table", header, rows)
	if err != nil {
		t.Fatalf("writeOutlineTreeSheet: %v", err)
	}
	if maxLevel != 3 {
		t.Errorf("maxLevel = %d, want 3", maxLevel)
	}

	// Header row (row 1).
	if v, _ := f.GetCellValue("Table", "A1"); v != "A" {
		t.Errorf("A1 = %q, want A", v)
	}
	if v, _ := f.GetCellValue("Table", "D1"); v != "D" {
		t.Errorf("D1 = %q, want D", v)
	}

	// Row 2: TP1 in column A, outline level 0 (no explicit SetRowOutlineLevel).
	if v, _ := f.GetCellValue("Table", "A2"); v != "TP1" {
		t.Errorf("A2 = %q, want TP1", v)
	}
	lvl, _ := f.GetRowOutlineLevel("Table", 2)
	if lvl != 0 {
		t.Errorf("row 2 outline level = %d, want 0", lvl)
	}

	// Row 3: TE1 in column B, outline level 1.
	if v, _ := f.GetCellValue("Table", "B3"); v != "TE1" {
		t.Errorf("B3 = %q, want TE1", v)
	}
	lvl, _ = f.GetRowOutlineLevel("Table", 3)
	if lvl != 1 {
		t.Errorf("row 3 outline level = %d, want 1", lvl)
	}

	// Row 4: T1 in column C, outline level 2.
	if v, _ := f.GetCellValue("Table", "C4"); v != "T1" {
		t.Errorf("C4 = %q, want T1", v)
	}
	lvl, _ = f.GetRowOutlineLevel("Table", 4)
	if lvl != 2 {
		t.Errorf("row 4 outline level = %d, want 2", lvl)
	}

	// Row 5: PASS in column D, outline level 3.
	if v, _ := f.GetCellValue("Table", "D5"); v != "PASS" {
		t.Errorf("D5 = %q, want PASS", v)
	}
	lvl, _ = f.GetRowOutlineLevel("Table", 5)
	if lvl != 3 {
		t.Errorf("row 5 outline level = %d, want 3", lvl)
	}

	// Row 6: T2 in column C (shared TP1/TE1 prefix deduped). Column A and B
	// should be blank (excelize trims trailing empties, so we check A explicitly).
	if v, _ := f.GetCellValue("Table", "C6"); v != "T2" {
		t.Errorf("C6 = %q, want T2", v)
	}
	if v, _ := f.GetCellValue("Table", "A6"); v != "" {
		t.Errorf("A6 = %q, want empty (prefix deduped)", v)
	}
	lvl, _ = f.GetRowOutlineLevel("Table", 6)
	if lvl != 2 {
		t.Errorf("row 6 outline level = %d, want 2", lvl)
	}

	// Row 7: FAIL in column D.
	if v, _ := f.GetCellValue("Table", "D7"); v != "FAIL" {
		t.Errorf("D7 = %q, want FAIL", v)
	}
	lvl, _ = f.GetRowOutlineLevel("Table", 7)
	if lvl != 3 {
		t.Errorf("row 7 outline level = %d, want 3", lvl)
	}

	// Row 8: TP2 in column A (new root, no dedup).
	if v, _ := f.GetCellValue("Table", "A8"); v != "TP2" {
		t.Errorf("A8 = %q, want TP2", v)
	}
	lvl, _ = f.GetRowOutlineLevel("Table", 8)
	if lvl != 0 {
		t.Errorf("row 8 outline level = %d, want 0", lvl)
	}

	// Total rows: 1 header + 4 nodes per row group (but TP1/TE1 shared = 2 shared
	// prefix rows) = 1 + 4 + 2 + 4 = 11.
	allRows, _ := f.GetRows("Table")
	if len(allRows) != 11 {
		t.Errorf("row count = %d, want 11", len(allRows))
	}
}

// TestWriteOutlineTreeSheetSharedPrefixProducesSingleParentRow checks the core
// dedup contract: two rows sharing the first node emit exactly one parent row
// for that node (not two).
func TestWriteOutlineTreeSheetSharedPrefixProducesSingleParentRow(t *testing.T) {
	header := []string{"Plan", "Test", "Status"}
	rows := [][]string{
		{"PlanA", "Test1", "PASS"},
		{"PlanA", "Test2", "FAIL"},
	}

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	defaultSheet := f.GetSheetName(0)
	if err := f.SetSheetName(defaultSheet, "Table"); err != nil {
		t.Fatalf("rename sheet: %v", err)
	}

	_, err := writeOutlineTreeSheet(f, "Table", header, rows)
	if err != nil {
		t.Fatalf("writeOutlineTreeSheet: %v", err)
	}

	// PlanA should appear exactly once (row 2); rows for Test1 and Test2 follow.
	if v, _ := f.GetCellValue("Table", "A2"); v != "PlanA" {
		t.Errorf("A2 = %q, want PlanA", v)
	}
	// Row 3 should be Test1 (column B), NOT a second PlanA.
	if v, _ := f.GetCellValue("Table", "B3"); v != "Test1" {
		t.Errorf("B3 = %q, want Test1 (not a repeated PlanA)", v)
	}
	// Row 4 should be PASS (column C).
	if v, _ := f.GetCellValue("Table", "C4"); v != "PASS" {
		t.Errorf("C4 = %q, want PASS", v)
	}
	// Row 5 should be Test2 (column B), sharing PlanA.
	if v, _ := f.GetCellValue("Table", "B5"); v != "Test2" {
		t.Errorf("B5 = %q, want Test2", v)
	}
	// A5 should be blank (PlanA deduped).
	if v, _ := f.GetCellValue("Table", "A5"); v != "" {
		t.Errorf("A5 = %q, want empty (PlanA deduped)", v)
	}
}

// TestExportTraceabilitySheetsOutlineTree verifies that ExportTraceabilitySheets
// produces a workbook with a "Table" sheet that is a collapsible outline tree.
// Two execution threads sharing the same Test Plan + Test Execution produce a
// single parent node for the Test Plan (deduped), and the leaf rows carry
// the run status in the last column (D) at outline level 3.
func TestExportTraceabilitySheetsOutlineTree(t *testing.T) {
	r := newTestRepo(t)
	const p = "p1"

	// Seed: one plan, one execution, two tests with different run statuses.
	seedContainer(t, r, p, "DEMO-TP-1", "testplan", "Plan One", "Open")
	seedContainer(t, r, p, "DEMO-TE-1", "testexec", "Cycle 1", "Open")
	seedContainerTest(t, r, p, "DEMO-TP-1", "DEMO-1", "")
	seedContainerTest(t, r, p, "DEMO-TP-1", "DEMO-2", "")
	seedContainerTest(t, r, p, "DEMO-TE-1", "DEMO-1", "PASS")
	seedContainerTest(t, r, p, "DEMO-TE-1", "DEMO-2", "FAIL")

	data, err := r.ExportTraceabilitySheets(p, "DEMO", "execution", nil, nil, nil, nil, false)
	if err != nil {
		t.Fatalf("ExportTraceabilitySheets: %v", err)
	}

	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	defer func() { _ = f.Close() }()

	// Both "Table" and "Flow" sheets must exist.
	sheets := f.GetSheetList()
	hasTable, hasFlow := false, false
	for _, s := range sheets {
		switch s {
		case "Table":
			hasTable = true
		case "Flow":
			hasFlow = true
		}
	}
	if !hasTable {
		t.Fatalf("Table sheet not found; sheets = %v", sheets)
	}
	if !hasFlow {
		t.Fatalf("Flow sheet not found; sheets = %v", sheets)
	}

	// Scan the Table rows to verify the tree structure.
	// The execution table rows are sorted by container_key, test_key so both tests
	// share the same Test Plan and Test Execution prefix.
	allRows, err := f.GetRows("Table")
	if err != nil {
		t.Fatalf("GetRows Table: %v", err)
	}
	// Row 1 = header. We expect the Plan row (level 0) to appear once, with the
	// Exec row (level 1) beneath it, and the two Test rows (level 2) below that,
	// each with a Status leaf (level 3).
	// Rows: header(1) + Plan(1) + Exec(1) + [Test(1)+Status(1)]*2 = 7 rows.
	if len(allRows) < 7 {
		t.Fatalf("Table row count = %d, want >= 7", len(allRows))
	}

	// Row 1 should be the header.
	if len(allRows[0]) < 1 || allRows[0][0] != "Test Plan" {
		t.Errorf("header row[0][0] = %q, want Test Plan", safeIdx(allRows[0], 0))
	}

	// Verify the sheetFormatPr/@outlineLevelRow attribute is injected so Excel
	// draws the collapse controls.
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip read: %v", err)
	}
	var tableXML string
	for _, zf := range zr.File {
		if zf.Name != "xl/worksheets/sheet1.xml" {
			continue
		}
		rc, _ := zf.Open()
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		tableXML = string(b)
	}
	if tableXML == "" {
		t.Fatal("xl/worksheets/sheet1.xml not found in workbook")
	}
	if !strings.Contains(tableXML, "outlineLevelRow=") {
		t.Errorf("sheetFormatPr is missing outlineLevelRow; element: %s", sheetFormatPrOf(tableXML))
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
