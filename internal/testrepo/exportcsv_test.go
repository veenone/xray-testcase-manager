package testrepo_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"xray-test-manager/internal/testrepo"
)

func seedExportTests(t *testing.T) *testrepo.Repository {
	t.Helper()
	repo := newRepo(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "Login works", Status: "Open", Priority: "High", Labels: []string{"smoke"}, FolderID: "/Auth"},
		{Key: "QA-2", ID: "2", Summary: "Logout works", Status: "Done", Priority: "Low"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return repo
}

func TestExportTestsCSVHasHeaderAndRows(t *testing.T) {
	repo := seedExportTests(t)

	data, err := repo.ExportTests("p1", testrepo.Query{}, "csv")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	text := string(data)
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3 (header + 2 tests)", len(lines))
	}
	if !strings.HasPrefix(lines[0], "Key,Summary,Description,Status,Priority,Labels,Components,Folder") {
		t.Errorf("header = %q, want the export columns", lines[0])
	}
	if !strings.Contains(text, "QA-1") || !strings.Contains(text, "Login works") {
		t.Errorf("export missing QA-1 row: %q", text)
	}
}

func TestExportTestsHonoursFilter(t *testing.T) {
	repo := seedExportTests(t)

	data, _ := repo.ExportTests("p1", testrepo.Query{Status: "Open"}, "csv")
	text := string(data)
	if !strings.Contains(text, "QA-1") {
		t.Errorf("filtered export should include QA-1: %q", text)
	}
	if strings.Contains(text, "QA-2") {
		t.Errorf("filtered export should exclude QA-2 (Done): %q", text)
	}
}

func TestExportTestsXLSXIsAValidWorkbook(t *testing.T) {
	repo := seedExportTests(t)

	data, err := repo.ExportTests("p1", testrepo.Query{}, "xlsx")
	if err != nil {
		t.Fatalf("export xlsx: %v", err)
	}
	// XLSX files are ZIP archives — they start with the "PK" signature.
	if !bytes.HasPrefix(data, []byte("PK")) {
		t.Errorf("xlsx output is not a ZIP/XLSX (no PK signature)")
	}
	if len(data) == 0 {
		t.Error("xlsx output is empty")
	}
}

// TestExportTestsXLSXIsStyled exercises the shared fillSheet styling that every
// flat XLSX export (tests, requirement audit, traceability, dashboard) goes
// through: a distinct, bold, bordered header band and word-wrapped data cells.
func TestExportTestsXLSXIsStyled(t *testing.T) {
	repo := seedExportTests(t)

	data, err := repo.ExportTests("p1", testrepo.Query{}, "xlsx")
	if err != nil {
		t.Fatalf("export xlsx: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = f.Close() }()
	sheet := f.GetSheetList()[0]

	headerStyleID, _ := f.GetCellStyle(sheet, "A1")
	dataStyleID, _ := f.GetCellStyle(sheet, "A2")
	if headerStyleID == 0 {
		t.Error("header cell A1 is unstyled")
	}
	if headerStyleID == dataStyleID {
		t.Error("header and data share a style; want a distinct header band")
	}

	hs, err := f.GetStyle(headerStyleID)
	if err != nil {
		t.Fatalf("GetStyle header: %v", err)
	}
	if hs.Font == nil || !hs.Font.Bold {
		t.Error("header style is not bold")
	}
	if len(hs.Border) == 0 {
		t.Error("header style has no borders")
	}

	ds, err := f.GetStyle(dataStyleID)
	if err != nil {
		t.Fatalf("GetStyle data: %v", err)
	}
	if ds.Alignment == nil || !ds.Alignment.WrapText {
		t.Error("data cell does not enable word wrap")
	}
	if len(ds.Border) == 0 {
		t.Error("data cell has no borders")
	}
}
