package coverage

import "testing"

func TestProjectsCRUDAndReplace(t *testing.T) {
	m, _ := newTestModule(t) // same helper other coverage tests use
	const p = "p1"
	if err := m.SetProjects(p, []ProjectConfig{
		{ProjectKey: "FUNC", Role: "source", Label: "Source of truth", SortOrder: 0},
		{ProjectKey: "CUST-A", Role: "customer", Label: "Customer A", SortOrder: 1},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := m.ListProjects(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ProjectKey != "FUNC" || got[1].Role != "customer" {
		t.Fatalf("got %+v", got)
	}
	// Replace-all: setting a new list drops the old rows.
	if err := m.SetProjects(p, []ProjectConfig{{ProjectKey: "CUST-B", Role: "customer", Label: "B", SortOrder: 0}}); err != nil {
		t.Fatal(err)
	}
	got, _ = m.ListProjects(p)
	if len(got) != 1 || got[0].ProjectKey != "CUST-B" {
		t.Fatalf("replace-all failed: %+v", got)
	}
}

func TestSetProjectsValidation(t *testing.T) {
	m, _ := newTestModule(t)
	const p = "p1"

	// Input contains: one blank key (should be skipped), one duplicate key
	// (last write wins → role becomes "source"), and one weird role (→ "customer").
	err := m.SetProjects(p, []ProjectConfig{
		{ProjectKey: "", Role: "customer", Label: "blank – skip me", SortOrder: 0},
		{ProjectKey: "FUNC", Role: "customer", Label: "first occurrence", SortOrder: 1},
		{ProjectKey: "WEIRD", Role: "weird", Label: "Weird role", SortOrder: 2},
		{ProjectKey: "FUNC", Role: "source", Label: "duplicate – last wins", SortOrder: 3},
	})
	if err != nil {
		t.Fatalf("SetProjects returned error: %v", err)
	}

	got, err := m.ListProjects(p)
	if err != nil {
		t.Fatal(err)
	}
	// Blank key skipped; FUNC deduped to 2 entries → 2 rows total.
	if len(got) != 2 {
		t.Fatalf("expected 2 rows (blank skipped, FUNC deduped); got %d: %+v", len(got), got)
	}

	// Build a lookup for order-independent assertions.
	byKey := map[string]ProjectConfig{}
	for _, r := range got {
		byKey[r.ProjectKey] = r
	}

	// FUNC: last write wins → role must be "source".
	if r, ok := byKey["FUNC"]; !ok {
		t.Error("FUNC row missing")
	} else if r.Role != "source" {
		t.Errorf("FUNC role: want source, got %q", r.Role)
	} else if r.Label != "duplicate – last wins" {
		t.Errorf("FUNC label: want 'duplicate – last wins', got %q", r.Label)
	}

	// WEIRD: non-source role must be normalised to "customer".
	if r, ok := byKey["WEIRD"]; !ok {
		t.Error("WEIRD row missing")
	} else if r.Role != "customer" {
		t.Errorf("WEIRD role: want customer (normalised from 'weird'), got %q", r.Role)
	}

	// sort_order must be compacted: 0 and 1 (no gaps from skipped rows).
	orders := map[int]bool{}
	for _, r := range got {
		orders[r.SortOrder] = true
	}
	if !orders[0] || !orders[1] || len(orders) != 2 {
		t.Errorf("sort_order not compacted: %+v", got)
	}
}

func TestListProjectsDerivesDefaultFromMembers(t *testing.T) {
	m, st := newTestModule(t)
	const p = "p1"
	db := st.DB()
	// Two customer requirements in two projects, both members of a canonical.
	db.Exec(`INSERT INTO requirement (profile_id, jira_key, project_key, summary) VALUES (?,?,?,?)`, p, "CUST-A-1", "CUST-A", "x")
	db.Exec(`INSERT INTO requirement (profile_id, jira_key, project_key, summary) VALUES (?,?,?,?)`, p, "CUST-B-1", "CUST-B", "y")
	cid, _ := m.CreateCanonical(p, "C_Sign", "PKCS11", "")
	m.SetMembers(p, cid, []string{"CUST-A-1", "CUST-B-1"})
	got, err := m.ListProjects(p) // no coverage_project rows yet → derive
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, r := range got {
		keys[r.ProjectKey] = true
	}
	if !keys["CUST-A"] || !keys["CUST-B"] {
		t.Fatalf("derived defaults missing CUST-A/CUST-B: %+v", got)
	}
}
