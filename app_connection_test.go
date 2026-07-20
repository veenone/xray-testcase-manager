package main

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"xray-test-manager/internal/backend/kiwi"
	"xray-test-manager/internal/backend/xray"
	"xray-test-manager/internal/connection"
	"xray-test-manager/internal/profile"
	"xray-test-manager/internal/settings"
	"xray-test-manager/internal/store"
	"xray-test-manager/internal/testrepo"
)

// memCredentialStore is an in-memory stand-in for profile.CredentialStore
// (which backs onto the real OS credential manager in production) so P6.3
// tests can exercise App's connection methods without touching Windows
// Credential Manager / the OS keyring.
type memCredentialStore struct {
	mu   sync.Mutex
	data map[string]string
}

func newMemCredentialStore() *memCredentialStore {
	return &memCredentialStore{data: map[string]string{}}
}

func (m *memCredentialStore) Save(id, secret string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[id] = secret
	return nil
}

func (m *memCredentialStore) Load(id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data[id], nil
}

func (m *memCredentialStore) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, id)
	return nil
}

var _ profile.CredentialStore = (*memCredentialStore)(nil)

// newTestApp builds a fully-wired App against a temp-dir SQLite store, with
// an in-memory credential store, for exercising App methods directly
// (bypassing Wails/startup). Mirrors initStore's wiring.
func newTestApp(t *testing.T) *App {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	a := &App{
		statusCache:   map[string][]string{},
		priorityCache: map[string][]string{},
	}
	a.store = st
	a.profiles = profile.NewManager(st)
	a.connections = connection.NewManager(st)
	a.creds = newMemCredentialStore()
	a.settings = settings.NewManager(st)
	a.repo = testrepo.NewRepository(st)
	return a
}

// TestBackendForConnectionRoutesByBackendType verifies backendForConnection
// (the connection-scoped twin of backendFor) loads a connection row + its
// credential and routes to the right backend.Backend implementation, mirroring
// TestNewBackendRouting's contract for newBackend.
func TestBackendForConnectionRoutesByBackendType(t *testing.T) {
	a := newTestApp(t)

	xrayConn, err := a.AddConnection("w1", "Xray Source", "xray", "https://jira.example.com", "QA",
		"", "Bug", "test", "", "tok", "", false, "source")
	if err != nil {
		t.Fatalf("AddConnection(xray): %v", err)
	}
	kiwiConn, err := a.AddConnection("w1", "Kiwi Target", "kiwi", "https://kiwi.example.com", "LAB",
		"", "Bug", "test", "", "user:pass", "", false, "target")
	if err != nil {
		t.Fatalf("AddConnection(kiwi): %v", err)
	}

	xb, err := a.backendForConnection(xrayConn.ID)
	if err != nil {
		t.Fatalf("backendForConnection(xray): %v", err)
	}
	if _, ok := xb.(*xray.Adapter); !ok {
		t.Errorf("backendForConnection(xray connection) = %T, want *xray.Adapter", xb)
	}

	kb, err := a.backendForConnection(kiwiConn.ID)
	if err != nil {
		t.Fatalf("backendForConnection(kiwi): %v", err)
	}
	if _, ok := kb.(*kiwi.Adapter); !ok {
		t.Errorf("backendForConnection(kiwi connection) = %T, want *kiwi.Adapter", kb)
	}
}

// TestAddConnectionCreatesAndSavesCredential verifies AddConnection creates
// the connection row and saves its credential keyed by the new connection id,
// and that a workspace can hold more than one connection (ListConnections
// returns both).
func TestAddConnectionCreatesAndSavesCredential(t *testing.T) {
	a := newTestApp(t)

	primary, err := a.AddConnection("w1", "Primary", "xray", "https://jira.example.com", "QA",
		"", "Bug", "test", "", "tok-1", "", false, "both")
	if err != nil {
		t.Fatalf("AddConnection(primary): %v", err)
	}
	second, err := a.AddConnection("w1", "Secondary", "kiwi", "https://kiwi.example.com", "LAB",
		"", "Bug", "test", "", "user:pass", "", false, "target")
	if err != nil {
		t.Fatalf("AddConnection(second): %v", err)
	}
	if primary.ID == second.ID {
		t.Fatalf("expected distinct connection ids, both = %q", primary.ID)
	}

	list, err := a.ListConnections("w1")
	if err != nil {
		t.Fatalf("ListConnections: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListConnections returned %d connections, want 2: %+v", len(list), list)
	}

	tok, err := a.creds.Load(primary.ID)
	if err != nil {
		t.Fatalf("load credential for primary: %v", err)
	}
	if tok != "tok-1" {
		t.Errorf("credential for primary = %q, want %q", tok, "tok-1")
	}
	tok2, err := a.creds.Load(second.ID)
	if err != nil {
		t.Fatalf("load credential for second: %v", err)
	}
	if tok2 != "user:pass" {
		t.Errorf("credential for second = %q, want %q", tok2, "user:pass")
	}
}

// TestUpdateConnectionKeepsCredentialOnBlankToken verifies UpdateConnection
// edits fields and, when passed a blank token, leaves the stored credential
// untouched (mirroring UpdateProfile).
func TestUpdateConnectionKeepsCredentialOnBlankToken(t *testing.T) {
	a := newTestApp(t)

	c, err := a.AddConnection("w1", "Primary", "xray", "https://jira.example.com", "QA",
		"", "Bug", "test", "", "tok-1", "", false, "both")
	if err != nil {
		t.Fatalf("AddConnection: %v", err)
	}

	updated, err := a.UpdateConnection(c.ID, "Renamed", "xray", "https://jira.example.com", "QA",
		"component = X", "Bug", "test", "", "" /* blank token */, "", false, "both")
	if err != nil {
		t.Fatalf("UpdateConnection: %v", err)
	}
	if updated.Name != "Renamed" || updated.ScopeJQL != "component = X" {
		t.Errorf("update did not apply: %+v", updated)
	}

	tok, err := a.creds.Load(c.ID)
	if err != nil {
		t.Fatalf("load credential: %v", err)
	}
	if tok != "tok-1" {
		t.Errorf("credential after blank-token update = %q, want unchanged %q", tok, "tok-1")
	}

	// A non-blank token does overwrite the stored credential.
	if _, err := a.UpdateConnection(c.ID, "Renamed", "xray", "https://jira.example.com", "QA",
		"component = X", "Bug", "test", "", "tok-2", "", false, "both"); err != nil {
		t.Fatalf("UpdateConnection with new token: %v", err)
	}
	tok, err = a.creds.Load(c.ID)
	if err != nil {
		t.Fatalf("load credential after non-blank update: %v", err)
	}
	if tok != "tok-2" {
		t.Errorf("credential after non-blank-token update = %q, want %q", tok, "tok-2")
	}
}

// TestDeleteConnectionRefusesPrimaryAllowsSecondary verifies DeleteConnection
// refuses to remove a workspace's primary connection (id == workspaceID,
// the row backendFor's single-connection shim relies on) but allows deleting
// a non-primary connection.
func TestDeleteConnectionRefusesPrimaryAllowsSecondary(t *testing.T) {
	a := newTestApp(t)

	// A primary connection has id == workspaceID (mirrors B1's backfill
	// shape); simulate that directly via the connections manager since
	// AddConnection always mints a fresh uuid.
	if _, err := a.connections.Create("w1", "w1", "Primary", "xray", "https://jira.example.com", "QA",
		"", "Bug", "test", "", "", false, "both", time.Now().UTC()); err != nil {
		t.Fatalf("create primary connection: %v", err)
	}
	if err := a.creds.Save("w1", "tok-1"); err != nil {
		t.Fatalf("save primary credential: %v", err)
	}

	second, err := a.AddConnection("w1", "Secondary", "kiwi", "https://kiwi.example.com", "LAB",
		"", "Bug", "test", "", "user:pass", "", false, "target")
	if err != nil {
		t.Fatalf("AddConnection(second): %v", err)
	}

	if err := a.DeleteConnection("w1"); err == nil {
		t.Fatal("DeleteConnection(primary) = nil error, want a refusal")
	}
	if _, err := a.connections.Get("w1"); err != nil {
		t.Fatalf("primary connection was removed despite the refusal: %v", err)
	}

	if err := a.DeleteConnection(second.ID); err != nil {
		t.Fatalf("DeleteConnection(secondary): %v", err)
	}
	if _, err := a.connections.Get(second.ID); err != connection.ErrNotFound {
		t.Errorf("secondary connection get after delete = %v, want ErrNotFound", err)
	}
	if tok, _ := a.creds.Load(second.ID); tok != "" {
		t.Errorf("secondary credential not cleaned up after delete, got %q", tok)
	}
}
