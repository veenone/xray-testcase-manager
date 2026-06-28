package coverage

import (
	"testing"
)

// countRows is a tiny helper for asserting cascade deletes left nothing behind.
func countRows(t *testing.T, m *Module, query string, args ...any) int {
	t.Helper()
	var n int
	if err := m.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// TestDeleteNodeCascades verifies DeleteNode removes a node and everything
// beneath it: deleting a value drops its test mappings; deleting a group drops
// its parameters, their values, and those values' mappings — no orphans.
func TestDeleteNodeCascades(t *testing.T) {
	m, _ := newTestModule(t)
	const p = "p1"

	cid, err := m.CreateCanonical(p, "C", "", "")
	if err != nil {
		t.Fatal(err)
	}
	vid, err := m.CreateVersion(p, cid, "1.0", "stable", "")
	if err != nil {
		t.Fatal(err)
	}
	gid, err := m.UpsertNode(p, NodeEdit{Kind: "group", VersionID: vid, Name: "G"})
	if err != nil {
		t.Fatal(err)
	}
	pid, err := m.UpsertNode(p, NodeEdit{Kind: "parameter", GroupID: gid, Name: "P"})
	if err != nil {
		t.Fatal(err)
	}
	v1, err := m.UpsertNode(p, NodeEdit{Kind: "value", ParameterID: pid, Name: "V1"})
	if err != nil {
		t.Fatal(err)
	}
	v2, err := m.UpsertNode(p, NodeEdit{Kind: "value", ParameterID: pid, Name: "V2"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetValueTests(p, v1, []string{"QA-1"}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetValueTests(p, v2, []string{"QA-2"}); err != nil {
		t.Fatal(err)
	}

	// Delete one value: it and its mapping go; the sibling and its mapping stay.
	if err := m.DeleteNode(p, "value", v2); err != nil {
		t.Fatalf("delete value: %v", err)
	}
	if got := countRows(t, m, `SELECT COUNT(*) FROM coverage_param_value WHERE profile_id=? AND id=?`, p, v2); got != 0 {
		t.Errorf("value v2 still present (%d)", got)
	}
	if got := countRows(t, m, `SELECT COUNT(*) FROM coverage_value_test WHERE profile_id=? AND value_id=?`, p, v2); got != 0 {
		t.Errorf("v2 mapping not cascaded (%d)", got)
	}
	if got := countRows(t, m, `SELECT COUNT(*) FROM coverage_param_value WHERE profile_id=? AND id=?`, p, v1); got != 1 {
		t.Errorf("sibling v1 was removed (%d)", got)
	}

	// Delete the group: parameter, remaining value, and its mapping all go.
	if err := m.DeleteNode(p, "group", gid); err != nil {
		t.Fatalf("delete group: %v", err)
	}
	checks := []struct {
		name, query string
	}{
		{"group", `SELECT COUNT(*) FROM coverage_param_group WHERE profile_id=? AND id=?`},
		{"parameter", `SELECT COUNT(*) FROM coverage_parameter WHERE profile_id=? AND group_id=?`},
	}
	if got := countRows(t, m, checks[0].query, p, gid); got != 0 {
		t.Errorf("group still present (%d)", got)
	}
	if got := countRows(t, m, checks[1].query, p, gid); got != 0 {
		t.Errorf("parameters under group not cascaded (%d)", got)
	}
	if got := countRows(t, m, `SELECT COUNT(*) FROM coverage_param_value WHERE profile_id=? AND id=?`, p, v1); got != 0 {
		t.Errorf("value under deleted group not cascaded (%d)", got)
	}
	if got := countRows(t, m, `SELECT COUNT(*) FROM coverage_value_test WHERE profile_id=? AND value_id=?`, p, v1); got != 0 {
		t.Errorf("mapping under deleted group not cascaded (%d)", got)
	}
}
