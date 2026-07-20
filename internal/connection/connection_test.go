package connection_test

import (
	"path/filepath"
	"testing"
	"time"

	"xray-test-manager/internal/connection"
	"xray-test-manager/internal/store"
)

func newManager(t *testing.T) *connection.Manager {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return connection.NewManager(st)
}

func TestCreateGetListDelete(t *testing.T) {
	m := newManager(t)
	now := time.Now().UTC().Truncate(time.Second)

	c, err := m.Create("c1", "w1", "Primary", "xray", "https://jira.example.com", "QA",
		"", "Bug", "test", "", "", false, "", now)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.Role != "both" {
		t.Errorf("blank role normalized to %q, want 'both'", c.Role)
	}

	got, err := m.Get("c1")
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
	if len(list) != 1 || list[0].ID != "c1" {
		t.Errorf("list = %+v, want one connection c1", list)
	}

	primary, err := m.Primary("w1")
	if err != nil {
		t.Fatalf("primary: %v", err)
	}
	if primary.ID != "c1" {
		t.Errorf("primary = %+v, want c1", primary)
	}

	if err := m.Delete("c1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := m.Get("c1"); err != connection.ErrNotFound {
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
