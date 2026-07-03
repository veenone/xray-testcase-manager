package coverage

import (
	"bytes"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestExportReportProfileWideSheets(t *testing.T) {
	m, st := newTestModule(t)
	const p = "p1"
	db := st.DB()

	// Seed a requirement and test_case; jira_id is NOT NULL in the schema.
	db.Exec(`INSERT INTO requirement (profile_id, jira_key, project_key, summary) VALUES (?,?,?,?)`,
		p, "CUST-1", "CUST", "req one")
	db.Exec(`INSERT INTO test_case (profile_id, jira_key, jira_id, summary) VALUES (?,?,?,?)`,
		p, "T-1", "t1", "test one")

	// Build a canonical with one stable version and two required values.
	cid, vid, _, vids := buildModel(t, m, p, "SignMech", []string{"RSA", "EC"})

	// Link the requirement to the canonical.
	if err := m.SetMembers(p, cid, []string{"CUST-1"}); err != nil {
		t.Fatal(err)
	}

	// Map the first test to the first value so coverage is non-zero.
	if err := m.SetValueTests(p, vids[0], []string{"T-1"}); err != nil {
		t.Fatal(err)
	}

	// Register the customer project explicitly.
	if err := m.SetProjects(p, []ProjectConfig{
		{ProjectKey: "CUST", Role: "customer", Label: "Customer A", SortOrder: 0},
	}); err != nil {
		t.Fatal(err)
	}

	data, err := m.ExportReport(p, vid)
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
	if !has("By project") {
		t.Fatalf("sheets = %v, want 'By project'", sheets)
	}
	if !has("Reuse map") {
		t.Fatalf("sheets = %v, want 'Reuse map'", sheets)
	}

	// "By project" header row must include Project and Coverage %.
	byProjRows, _ := f.GetRows("By project")
	if len(byProjRows) < 1 {
		t.Fatal("By project sheet has no rows")
	}
	hasCol := func(cols []string, name string) bool {
		for _, c := range cols {
			if c == name {
				return true
			}
		}
		return false
	}
	if !hasCol(byProjRows[0], "Project") {
		t.Errorf("By project header = %v, want column 'Project'", byProjRows[0])
	}
	if !hasCol(byProjRows[0], "Coverage %") {
		t.Errorf("By project header = %v, want column 'Coverage %%'", byProjRows[0])
	}

	// There must be a data row for the seeded customer project.
	found := false
	for _, row := range byProjRows[1:] {
		for _, c := range row {
			if c == "Customer A" || c == "CUST" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("By project sheet has no row for CUST; rows=%v", byProjRows)
	}
}

func TestExportReportRoundTrips(t *testing.T) {
	m, st := newTestModule(t)
	const p = "p1"
	_, vid, _, vids := buildModel(t, m, p, "Mechanism", []string{"RSA_PKCS", "ED25519"})
	seedTest(t, st, p, "QA-1", "PASS", "")
	if err := m.SetValueTests(p, vids[0], []string{"QA-1"}); err != nil {
		t.Fatal(err)
	}

	data, err := m.ExportReport(p, vid)
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
