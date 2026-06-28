package coverage

import (
	"bytes"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestExportReportRoundTrips(t *testing.T) {
	m, st := newTestModule(t)
	const p = "p1"
	cid, _, vids := buildModel(t, m, p, "Mechanism", []string{"RSA_PKCS", "ED25519"})
	seedTest(t, st, p, "QA-1", "PASS", "")
	if err := m.SetValueTests(p, vids[0], []string{"QA-1"}); err != nil {
		t.Fatal(err)
	}

	data, err := m.ExportReport(p, cid)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("reopen workbook: %v", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	has := func(name string) bool {
		for _, s := range sheets {
			if s == name {
				return true
			}
		}
		return false
	}
	if !has("Summary") || !has("Mechanism") || !has("Gaps") {
		t.Fatalf("sheets = %v, want Summary + Mechanism + Gaps", sheets)
	}

	// The Gaps sheet must list ED25519 (the one untested required value).
	gapRows, _ := f.GetRows("Gaps")
	found := false
	for _, row := range gapRows {
		for _, c := range row {
			if c == "ED25519" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("Gaps sheet should list ED25519; rows=%v", gapRows)
	}
}
