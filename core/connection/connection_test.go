package connection_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"agile-suite/core/connection"
	"agile-suite/core/shareddb"
)

func newManager(t *testing.T) *connection.Manager {
	t.Helper()
	st, err := shareddb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return connection.NewManager(st.DB())
}

func TestCreateGetListDelete(t *testing.T) {
	m := newManager(t)
	now := time.Now().UTC().Truncate(time.Second)

	c, err := m.Create("w1", "w1", "Primary", "xray", "https://jira.example.com", "QA",
		"", "Bug", "test", "", "", false, "", now)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.Role != "both" {
		t.Errorf("blank role normalized to %q, want 'both'", c.Role)
	}

	got, err := m.Get("w1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.WorkspaceID != "w1" || got.Backend != "xray" {
		t.Errorf("got = %+v", got)
	}

	list, err := m.List("w1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != "w1" {
		t.Errorf("list = %+v, want one connection w1", list)
	}

	primary, err := m.Primary("w1")
	if err != nil {
		t.Fatalf("primary: %v", err)
	}
	if primary.ID != "w1" {
		t.Errorf("primary = %+v, want w1", primary)
	}

	if err := m.Delete("w1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := m.Get("w1"); err != connection.ErrNotFound {
		t.Errorf("get after delete = %v, want ErrNotFound", err)
	}
}

func TestUpdateChangesFields(t *testing.T) {
	m := newManager(t)
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := m.Create("c1", "w1", "Primary", "xray", "https://jira.example.com", "QA",
		"", "Bug", "test", "", "", false, "both", now); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := m.Update("c1", "Renamed", "kiwi", "https://kiwi.example.com", "LAB",
		"component = X", "Defect", "dedicated", "DEFECTS", "cert", true, "target"); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := m.Get("c1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Renamed" || got.Backend != "kiwi" || got.URL != "https://kiwi.example.com" ||
		got.ProjectKey != "LAB" || got.ScopeJQL != "component = X" || got.BugIssueType != "Defect" ||
		got.BugProjectMode != "dedicated" || got.BugProjectKey != "DEFECTS" || got.CACert != "cert" ||
		!got.AllowUntrustedTLS || got.Role != "target" {
		t.Errorf("update did not apply: %+v", got)
	}
	// created_at and workspace_id are untouched by Update.
	if got.WorkspaceID != "w1" {
		t.Errorf("workspace_id changed to %q, want 'w1'", got.WorkspaceID)
	}
}

func TestUpdateUnknownIDErrors(t *testing.T) {
	m := newManager(t)
	if err := m.Update("nope", "n", "xray", "u", "p", "", "", "", "", "", false, ""); err != connection.ErrNotFound {
		t.Errorf("update unknown id = %v, want ErrNotFound", err)
	}
}

func TestPutCreatesThenUpdates(t *testing.T) {
	m := newManager(t)
	now := time.Now().UTC().Truncate(time.Second)

	c1, err := m.Put("c1", "w1", "A", "xray", "https://jira.example.com", "QA",
		"", "Bug", "test", "", "", false, "both", now)
	if err != nil {
		t.Fatalf("put create: %v", err)
	}
	if c1.Name != "A" {
		t.Fatalf("first put name = %q, want 'A'", c1.Name)
	}

	// A second Put with the same id overwrites fields but keeps created_at.
	c2, err := m.Put("c1", "w1", "B", "kiwi", "https://kiwi.example.com", "LAB",
		"", "Bug", "test", "", "", false, "both", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("put update: %v", err)
	}
	if c2.Name != "B" || c2.Backend != "kiwi" {
		t.Errorf("second put did not overwrite: %+v", c2)
	}

	list, err := m.List("w1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list = %+v, want exactly one row (Put must not duplicate)", list)
	}
}

// TestBugsRoleSurvivesARoundTrip pins the role vocabulary. roleOrDefault
// normalizes anything it does not recognize to "both", so a bug connection
// written before "bugs" was a known role would read back as a second PRIMARY
// connection rather than a bug connection.
func TestBugsRoleSurvivesARoundTrip(t *testing.T) {
	m := newManager(t)
	now := time.Now().UTC().Truncate(time.Second)

	if _, err := m.Create("bug1", "w1", "Bug Jira", "xray", "https://jira.example.com", "DEF",
		"", "Bug", "dedicated", "DEF", "", false, "bugs", now); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := m.Get("bug1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Role != "bugs" {
		t.Errorf("role = %q, want %q", got.Role, "bugs")
	}
}

// TestPrimaryIgnoresABugConnection is the ordering trap. Primary used
// ORDER BY created_at, id LIMIT 1, so two rows sharing a created_at break the
// tie by id, and a generated connection id can sort ahead of a profile id.
// Primary must select the row whose id equals the workspace id.
func TestPrimaryIgnoresABugConnection(t *testing.T) {
	m := newManager(t)
	now := time.Now().UTC().Truncate(time.Second)

	// "aaa" sorts before the workspace id "w1" and shares its created_at.
	if _, err := m.Create("aaa", "w1", "Bug Jira", "xray", "https://jira.example.com", "DEF",
		"", "Bug", "dedicated", "DEF", "", false, "bugs", now); err != nil {
		t.Fatalf("create bug connection: %v", err)
	}
	if _, err := m.Create("w1", "w1", "Primary", "kiwi", "https://kiwi.example.com", "SNMP",
		"", "Bug", "test", "", "", false, "both", now); err != nil {
		t.Fatalf("create primary: %v", err)
	}

	p, err := m.Primary("w1")
	if err != nil {
		t.Fatalf("primary: %v", err)
	}
	if p.ID != "w1" {
		t.Errorf("primary id = %q, want the workspace id w1 (a bug connection displaced it)", p.ID)
	}
}

// TestByRoleFindsTheBugConnection is the lookup the app uses to decide whether
// a profile routes bugs. No profile column records it: the role on the
// connection row is the single source of truth.
func TestByRoleFindsTheBugConnection(t *testing.T) {
	m := newManager(t)
	now := time.Now().UTC().Truncate(time.Second)

	if _, err := m.Create("w1", "w1", "Primary", "kiwi", "https://kiwi.example.com", "SNMP",
		"", "Bug", "test", "", "", false, "both", now); err != nil {
		t.Fatalf("create primary: %v", err)
	}
	if _, err := m.ByRole("w1", "bugs"); !errors.Is(err, connection.ErrNotFound) {
		t.Fatalf("ByRole on a workspace with no bug connection = %v, want ErrNotFound", err)
	}

	if _, err := m.Create("bug1", "w1", "Bug Jira", "xray", "https://jira.example.com", "DEF",
		"", "Bug", "dedicated", "DEF", "", false, "bugs", now); err != nil {
		t.Fatalf("create bug connection: %v", err)
	}
	got, err := m.ByRole("w1", "bugs")
	if err != nil {
		t.Fatalf("ByRole: %v", err)
	}
	if got.ID != "bug1" || got.ProjectKey != "DEF" {
		t.Errorf("got = %+v, want the bug connection bug1/DEF", got)
	}
}
