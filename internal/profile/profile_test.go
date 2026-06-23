package profile_test

import (
	"path/filepath"
	"testing"

	"xray-test-manager/internal/profile"
	"xray-test-manager/internal/store"
)

func newManager(t *testing.T) *profile.Manager {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return profile.NewManager(st)
}

func TestCreateProfileStoresScopeJQL(t *testing.T) {
	m := newManager(t)

	p, err := m.Create("QA", "https://jira.example.com", "QA", "labels = smoke", "Defect", "", "", "", false)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.ScopeJQL != "labels = smoke" {
		t.Errorf("ScopeJQL = %q, want 'labels = smoke'", p.ScopeJQL)
	}

	got, err := m.Get(p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ScopeJQL != "labels = smoke" {
		t.Errorf("persisted ScopeJQL = %q, want 'labels = smoke'", got.ScopeJQL)
	}
}

func TestBugIssueTypeDefaultsAndPersists(t *testing.T) {
	m := newManager(t)

	// A blank issue type defaults to "Bug".
	def, err := m.Create("Prod", "https://jira.example.com", "PROJ", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if def.BugIssueType != "Bug" {
		t.Errorf("default BugIssueType = %q, want 'Bug'", def.BugIssueType)
	}

	// A configured issue type persists, and Update can change it.
	got, err := m.Create("Stg", "https://jira.example.com", "STG", "", "Defect", "", "", "", false)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if reread, _ := m.Get(got.ID); reread.BugIssueType != "Defect" {
		t.Errorf("persisted BugIssueType = %q, want 'Defect'", reread.BugIssueType)
	}
	if err := m.Update(got.ID, "Stg", "https://jira.example.com", "STG", "", "Incident", "", "", "", false); err != nil {
		t.Fatalf("update: %v", err)
	}
	if reread, _ := m.Get(got.ID); reread.BugIssueType != "Incident" {
		t.Errorf("after update BugIssueType = %q, want 'Incident'", reread.BugIssueType)
	}
}

func TestBugProjectModeDefaultsAndPersists(t *testing.T) {
	m := newManager(t)

	// A blank mode defaults to "test".
	def, err := m.Create("Prod", "https://jira.example.com", "PROJ", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if def.BugProjectMode != "test" {
		t.Errorf("default BugProjectMode = %q, want 'test'", def.BugProjectMode)
	}

	// An unknown mode is normalised to "test".
	bad, _ := m.Create("Bad", "https://jira.example.com", "BAD", "", "", "garbage", "", "", false)
	if bad.BugProjectMode != "test" {
		t.Errorf("unknown mode = %q, want 'test'", bad.BugProjectMode)
	}

	// Dedicated mode + key persist, and Update can change them.
	got, err := m.Create("Stg", "https://jira.example.com", "STG", "", "", "dedicated", "DEFECTS", "", false)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if reread, _ := m.Get(got.ID); reread.BugProjectMode != "dedicated" || reread.BugProjectKey != "DEFECTS" {
		t.Errorf("persisted bug project = (%q, %q), want (dedicated, DEFECTS)",
			reread.BugProjectMode, reread.BugProjectKey)
	}
	if err := m.Update(got.ID, "Stg", "https://jira.example.com", "STG", "", "", "execution", "", "", false); err != nil {
		t.Fatalf("update: %v", err)
	}
	if reread, _ := m.Get(got.ID); reread.BugProjectMode != "execution" {
		t.Errorf("after update BugProjectMode = %q, want 'execution'", reread.BugProjectMode)
	}
}

func TestUpdateScopeChangesJQL(t *testing.T) {
	m := newManager(t)
	p, err := m.Create("QA", "https://jira.example.com", "QA", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := m.UpdateScope(p.ID, "component = Login"); err != nil {
		t.Fatalf("update scope: %v", err)
	}

	got, _ := m.Get(p.ID)
	if got.ScopeJQL != "component = Login" {
		t.Errorf("ScopeJQL = %q, want 'component = Login'", got.ScopeJQL)
	}
}

func TestUpdateScopeUnknownProfileErrors(t *testing.T) {
	m := newManager(t)
	if err := m.UpdateScope("nope", "x"); err == nil {
		t.Error("updating an unknown profile's scope should error")
	}
}
