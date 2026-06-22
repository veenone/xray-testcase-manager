package testrepo

import (
	"bytes"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestExportDashboardSheets(t *testing.T) {
	r := newTestRepo(t)
	const p = "p1"

	if err := r.UpsertTests(p, []TestCase{
		{Key: "QA-1", ID: "1", Status: "Open", Priority: "High", Labels: []string{"smoke"}},
		{Key: "QA-2", ID: "2", Status: "Open", Priority: "Low", Labels: []string{"smoke", "api"}},
		{Key: "QA-3", ID: "3", Status: "Done", Priority: "High"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	data, err := r.ExportDashboardSheets(p, "", "", "")
	if err != nil {
		t.Fatalf("ExportDashboardSheets: %v", err)
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
	if !have["Summary"] {
		t.Fatalf("expected Summary sheet, got %v", f.GetSheetList())
	}
	if !have["By status"] {
		t.Fatalf("expected By status sheet, got %v", f.GetSheetList())
	}

	// Summary sheet header and the Total row.
	if v, _ := f.GetCellValue("Summary", "A1"); v != "Metric" {
		t.Errorf("Summary A1 = %q, want Metric", v)
	}
	if v, _ := f.GetCellValue("Summary", "B1"); v != "Value" {
		t.Errorf("Summary B1 = %q, want Value", v)
	}
	if v, _ := f.GetCellValue("Summary", "A2"); v != "Total tests" {
		t.Errorf("Summary A2 = %q, want Total tests", v)
	}
	if v, _ := f.GetCellValue("Summary", "B2"); v != "3" {
		t.Errorf("Summary B2 = %q, want 3", v)
	}

	// Breakdown sheet header.
	if v, _ := f.GetCellValue("By status", "A1"); v != "Label" {
		t.Errorf("By status A1 = %q, want Label", v)
	}
	if v, _ := f.GetCellValue("By status", "B1"); v != "Count" {
		t.Errorf("By status B1 = %q, want Count", v)
	}
}
