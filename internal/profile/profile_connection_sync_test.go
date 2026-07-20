package profile_test

import (
	"path/filepath"
	"testing"

	"xray-test-manager/internal/connection"
	"xray-test-manager/internal/profile"
	"xray-test-manager/internal/store"
)

// newManagerWithConnections returns a profile.Manager and a connection.Manager
// backed by the same store, so a test can drive profile writes and inspect the
// resulting connection rows directly (Phase 6 bridge task B1's shim seam).
func newManagerWithConnections(t *testing.T) (*profile.Manager, *connection.Manager) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return profile.NewManager(st), connection.NewManager(st)
}

// TestCreateProfileCreatesMatchingConnection verifies profile.Manager.Create
// also creates the profile's primary connection row: same id as the profile,
// workspace_id == the profile's id, every backend field copied, role "both".
func TestCreateProfileCreatesMatchingConnection(t *testing.T) {
	pm, cm := newManagerWithConnections(t)

	p, err := pm.Create("QA", "https://jira.example.com", "QA", "labels = smoke",
		"Defect", "dedicated", "DEFECTS", "-----BEGIN CERT-----", true, "")
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}

	c, err := cm.Get(p.ID)
	if err != nil {
		t.Fatalf("get connection for new profile: %v", err)
	}
	if c.ID != p.ID || c.WorkspaceID != p.ID {
		t.Errorf("connection id/workspace_id = %q/%q, want both %q", c.ID, c.WorkspaceID, p.ID)
	}
	if c.Name != p.Name || c.Backend != p.Backend || c.URL != p.JiraURL || c.ProjectKey != p.ProjectKey ||
		c.ScopeJQL != p.ScopeJQL || c.BugIssueType != p.BugIssueType || c.BugProjectMode != p.BugProjectMode ||
		c.BugProjectKey != p.BugProjectKey || c.CACert != p.CACert || c.AllowUntrustedTLS != p.AllowUntrustedTLS {
		t.Errorf("connection fields did not mirror the created profile: connection=%+v profile=%+v", c, p)
	}
	if c.Role != "both" {
		t.Errorf("connection role = %q, want 'both'", c.Role)
	}

	// Sanity: the primary lookup finds the same row.
	primary, err := cm.Primary(p.ID)
	if err != nil {
		t.Fatalf("primary connection: %v", err)
	}
	if primary.ID != p.ID {
		t.Errorf("primary connection id = %q, want %q", primary.ID, p.ID)
	}
}

// TestCreateKiwiProfileBackfillsKiwiConnection verifies a profile created on
// the "kiwi" backend gets a connection row with backend "kiwi" (not defaulted
// back to "xray").
func TestCreateKiwiProfileBackfillsKiwiConnection(t *testing.T) {
	pm, cm := newManagerWithConnections(t)

	p, err := pm.Create("Lab", "https://kiwi.example.com", "LAB", "", "", "", "", "", false, "kiwi")
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	if p.Backend != "kiwi" {
		t.Fatalf("profile backend = %q, want 'kiwi'", p.Backend)
	}

	c, err := cm.Get(p.ID)
	if err != nil {
		t.Fatalf("get connection: %v", err)
	}
	if c.Backend != "kiwi" {
		t.Errorf("connection backend = %q, want 'kiwi'", c.Backend)
	}
}

// TestUpdateProfileUpdatesConnection verifies profile.Manager.Update propagates
// every changed field onto the profile's connection row.
func TestUpdateProfileUpdatesConnection(t *testing.T) {
	pm, cm := newManagerWithConnections(t)

	p, err := pm.Create("QA", "https://jira.example.com", "QA", "", "", "", "", "", false, "")
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}

	if err := pm.Update(p.ID, "QA Renamed", "https://jira2.example.com", "QA2", "component = Login",
		"Incident", "execution", "", "cert-data", true, "kiwi"); err != nil {
		t.Fatalf("update profile: %v", err)
	}

	updated, err := pm.Get(p.ID)
	if err != nil {
		t.Fatalf("get updated profile: %v", err)
	}

	c, err := cm.Get(p.ID)
	if err != nil {
		t.Fatalf("get connection after update: %v", err)
	}
	if c.Name != updated.Name || c.Backend != updated.Backend || c.URL != updated.JiraURL ||
		c.ProjectKey != updated.ProjectKey || c.ScopeJQL != updated.ScopeJQL ||
		c.BugIssueType != updated.BugIssueType || c.BugProjectMode != updated.BugProjectMode ||
		c.CACert != updated.CACert || c.AllowUntrustedTLS != updated.AllowUntrustedTLS {
		t.Errorf("connection did not reflect update: connection=%+v profile=%+v", c, updated)
	}
	if c.Backend != "kiwi" {
		t.Errorf("connection backend after update = %q, want 'kiwi'", c.Backend)
	}
	// The connection's own id/workspace_id/role are untouched by Update.
	if c.ID != p.ID || c.WorkspaceID != p.ID || c.Role != "both" {
		t.Errorf("connection identity/role changed unexpectedly: %+v", c)
	}
}

// TestDeleteProfileDeletesConnection verifies profile.Manager.Delete removes
// the profile's primary connection row alongside it.
func TestDeleteProfileDeletesConnection(t *testing.T) {
	pm, cm := newManagerWithConnections(t)

	p, err := pm.Create("QA", "https://jira.example.com", "QA", "", "", "", "", "", false, "")
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	if _, err := cm.Get(p.ID); err != nil {
		t.Fatalf("connection should exist before delete: %v", err)
	}

	if err := pm.Delete(p.ID); err != nil {
		t.Fatalf("delete profile: %v", err)
	}

	if _, err := cm.Get(p.ID); err != connection.ErrNotFound {
		t.Errorf("connection lookup after delete = %v, want connection.ErrNotFound", err)
	}
}
