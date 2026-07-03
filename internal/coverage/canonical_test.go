package coverage

import (
	"path/filepath"
	"testing"

	"xray-test-manager/internal/store"
	"xray-test-manager/internal/testrepo"
)

// newTestModule opens a fresh on-disk store and returns a coverage Module plus
// the raw store for seeding fixtures.
func newTestModule(t *testing.T) (*Module, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "cov.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st, testrepo.NewRepository(st)), st
}

func TestRenameCanonical(t *testing.T) {
	m, _ := newTestModule(t)
	const p = "p1"
	id, err := m.CreateCanonical(p, "Old", "cat", "desc")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.RenameCanonical(p, id, "New name", "auth", "new desc"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	list, _ := m.ListCanonical(p)
	if len(list) != 1 || list[0].Name != "New name" || list[0].Category != "auth" {
		t.Errorf("after rename = %+v, want name 'New name' category 'auth'", list)
	}
	// Empty name is rejected.
	if err := m.RenameCanonical(p, id, "", "", ""); err == nil {
		t.Error("expected error renaming to empty name")
	}
	// Unknown id is a not-found error.
	if err := m.RenameCanonical(p, "nope", "X", "", ""); err == nil {
		t.Error("expected not-found error for unknown canonical id")
	}
}

func TestCanonicalCRUDAndReuse(t *testing.T) {
	m, st := newTestModule(t)
	const p = "p1"

	// Seed requirement rows in two customer projects (as sync would).
	db := st.DB()
	for _, r := range []struct{ key, proj, summary string }{
		{"BANK-10", "CUST-HSM-BANK", "PKI Infra"},
		{"SAMSU-10", "CUST-HSM-SAMSU", "Secure Enclave"},
	} {
		if _, err := db.Exec(
			`INSERT INTO requirement (profile_id, jira_key, project_key, summary, status)
			 VALUES (?, ?, ?, ?, 'Open')`, p, r.key, r.proj, r.summary); err != nil {
			t.Fatalf("seed requirement: %v", err)
		}
	}

	id, err := m.CreateCanonical(p, "C_Sign", "PKCS11", "Signature function")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// A second profile must not see it (isolation).
	if got, _ := m.ListCanonical("other"); len(got) != 0 {
		t.Fatalf("profile isolation broken: other profile saw %d canonicals", len(got))
	}

	if err := m.SetMembers(p, id, []string{"BANK-10", "SAMSU-10", "BANK-10"}); err != nil {
		t.Fatalf("set members: %v", err)
	}

	list, err := m.ListCanonical(p)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: err=%v len=%d", err, len(list))
	}
	if list[0].MemberCount != 2 { // dedup of BANK-10
		t.Errorf("member count = %d, want 2", list[0].MemberCount)
	}

	reuse, err := m.ListReuse(p, id)
	if err != nil || len(reuse) != 2 {
		t.Fatalf("reuse: err=%v len=%d", err, len(reuse))
	}
	if reuse[0].ProjectKey != "CUST-HSM-BANK" || reuse[0].Summary != "PKI Infra" {
		t.Errorf("reuse[0] = %+v, want BANK joined to its requirement", reuse[0])
	}

	if err := m.DeleteCanonical(p, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if list, _ := m.ListCanonical(p); len(list) != 0 {
		t.Errorf("after delete, expected 0 canonicals, got %d", len(list))
	}
	// Membership rows must be gone too.
	var members int
	db.QueryRow(`SELECT COUNT(*) FROM canonical_requirement_member WHERE profile_id=?`, p).Scan(&members)
	if members != 0 {
		t.Errorf("after delete, expected 0 members, got %d", members)
	}
}
