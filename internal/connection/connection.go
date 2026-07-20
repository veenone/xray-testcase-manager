// Package connection manages the connection table — the first step toward
// multi-connection workspaces (Phase 6 bridge). A workspace (today, a
// profiles row) will eventually hold more than one backend connection; for
// now every workspace has exactly one, whose id equals the workspace id (the
// profile's id). The connection row is kept in sync with profile writes by
// internal/profile's Manager, but profiles remains the read source of truth
// for every existing flow — nothing in the app reads from this package yet.
package connection

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"xray-test-manager/internal/store"
)

// ErrNotFound is returned when a connection id does not exist.
var ErrNotFound = errors.New("connection not found")

// Connection is one backend a workspace talks to. Credentials are stored
// separately in the OS credential manager, keyed by this Connection's ID —
// never in this struct or the database.
type Connection struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Name        string `json:"name"`
	// Backend selects which system this connection talks to: "xray" (Jira Data
	// Center + Xray Server/DC) or "kiwi" (Kiwi TCMS).
	Backend           string `json:"backend"`
	URL               string `json:"url"`
	ProjectKey        string `json:"projectKey"`
	ScopeJQL          string `json:"scopeJql"`
	BugIssueType      string `json:"bugIssueType"`
	BugProjectMode    string `json:"bugProjectMode"`
	BugProjectKey     string `json:"bugProjectKey"`
	CACert            string `json:"caCert"`
	AllowUntrustedTLS bool   `json:"allowUntrustedTls"`
	// Role is 'source', 'target', or 'both'. A single-connection workspace's
	// connection is always 'both'.
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
}

// Manager is the connection CRUD service backed by the local store.
type Manager struct {
	db *sql.DB
}

// NewManager returns a connection manager backed by the given store.
func NewManager(s *store.Store) *Manager {
	return &Manager{db: s.DB()}
}

const selectColumns = `id, workspace_id, name, backend, url, project_key, scope_jql, bug_issue_type, bug_project_mode, bug_project_key, ca_cert, allow_untrusted_tls, role, created_at`

// scanner abstracts *sql.Row and *sql.Rows so scan serves both Get and List.
type scanner interface {
	Scan(dest ...any) error
}

func scan(s scanner) (Connection, error) {
	var (
		c                 Connection
		allowUntrustedInt int
		created           string
	)
	if err := s.Scan(&c.ID, &c.WorkspaceID, &c.Name, &c.Backend, &c.URL, &c.ProjectKey,
		&c.ScopeJQL, &c.BugIssueType, &c.BugProjectMode, &c.BugProjectKey, &c.CACert,
		&allowUntrustedInt, &c.Role, &created); err != nil {
		return Connection{}, err
	}
	c.AllowUntrustedTLS = allowUntrustedInt != 0
	c.Role = roleOrDefault(c.Role)
	c.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return c, nil
}

// roleOrDefault normalises the connection role, falling back to "both" for
// blank or unrecognized values.
func roleOrDefault(role string) string {
	switch strings.TrimSpace(role) {
	case "source", "target", "both":
		return strings.TrimSpace(role)
	default:
		return "both"
	}
}

// List returns every connection for a workspace, ordered by creation time
// then id (so the deterministic single-connection case is stable).
func (m *Manager) List(workspaceID string) ([]Connection, error) {
	rows, err := m.db.Query(
		`SELECT `+selectColumns+` FROM connection WHERE workspace_id = ? ORDER BY created_at, id`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list connections: %w", err)
	}
	defer rows.Close()

	out := []Connection{}
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Get returns one connection by id, or ErrNotFound.
func (m *Manager) Get(id string) (Connection, error) {
	row := m.db.QueryRow(`SELECT `+selectColumns+` FROM connection WHERE id = ?`, id)
	c, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Connection{}, ErrNotFound
	}
	return c, err
}

// Primary returns the workspace's primary connection — for a single-
// connection workspace (the only shape that exists as of task B1) this is
// its one and only connection, whose id equals workspaceID.
func (m *Manager) Primary(workspaceID string) (Connection, error) {
	row := m.db.QueryRow(
		`SELECT `+selectColumns+` FROM connection WHERE workspace_id = ? ORDER BY created_at, id LIMIT 1`, workspaceID)
	c, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Connection{}, ErrNotFound
	}
	return c, err
}

// Create inserts a new connection row with the given id (the caller chooses
// the id so a workspace's primary connection can be created with
// id == workspaceID, keeping the B1 1:1 shape deterministic). blank role
// normalizes to "both".
func (m *Manager) Create(id, workspaceID, name, backendType, url, projectKey, scopeJQL, bugIssueType, bugProjectMode, bugProjectKey, caCert string, allowUntrustedTLS bool, role string, createdAt time.Time) (Connection, error) {
	c := Connection{
		ID:                id,
		WorkspaceID:       workspaceID,
		Name:              name,
		Backend:           backendType,
		URL:               url,
		ProjectKey:        projectKey,
		ScopeJQL:          scopeJQL,
		BugIssueType:      bugIssueType,
		BugProjectMode:    bugProjectMode,
		BugProjectKey:     bugProjectKey,
		CACert:            caCert,
		AllowUntrustedTLS: allowUntrustedTLS,
		Role:              roleOrDefault(role),
		CreatedAt:         createdAt,
	}
	_, err := m.db.Exec(
		`INSERT INTO connection (id, workspace_id, name, backend, url, project_key, scope_jql, bug_issue_type, bug_project_mode, bug_project_key, ca_cert, allow_untrusted_tls, role, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.WorkspaceID, c.Name, c.Backend, c.URL, c.ProjectKey, c.ScopeJQL, c.BugIssueType,
		c.BugProjectMode, c.BugProjectKey, c.CACert, boolToInt(c.AllowUntrustedTLS), c.Role,
		c.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return Connection{}, fmt.Errorf("create connection: %w", err)
	}
	return c, nil
}

// Put creates the connection if it does not already exist, or overwrites it
// (every field except created_at) if it does. This is the shim seam used by
// profile.Manager to keep a profile's primary connection in sync without the
// caller having to know whether the row already exists.
func (m *Manager) Put(id, workspaceID, name, backendType, url, projectKey, scopeJQL, bugIssueType, bugProjectMode, bugProjectKey, caCert string, allowUntrustedTLS bool, role string, createdAt time.Time) (Connection, error) {
	c := Connection{
		ID:                id,
		WorkspaceID:       workspaceID,
		Name:              name,
		Backend:           backendType,
		URL:               url,
		ProjectKey:        projectKey,
		ScopeJQL:          scopeJQL,
		BugIssueType:      bugIssueType,
		BugProjectMode:    bugProjectMode,
		BugProjectKey:     bugProjectKey,
		CACert:            caCert,
		AllowUntrustedTLS: allowUntrustedTLS,
		Role:              roleOrDefault(role),
		CreatedAt:         createdAt,
	}
	_, err := m.db.Exec(
		`INSERT INTO connection (id, workspace_id, name, backend, url, project_key, scope_jql, bug_issue_type, bug_project_mode, bug_project_key, ca_cert, allow_untrusted_tls, role, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   workspace_id = excluded.workspace_id,
		   name = excluded.name,
		   backend = excluded.backend,
		   url = excluded.url,
		   project_key = excluded.project_key,
		   scope_jql = excluded.scope_jql,
		   bug_issue_type = excluded.bug_issue_type,
		   bug_project_mode = excluded.bug_project_mode,
		   bug_project_key = excluded.bug_project_key,
		   ca_cert = excluded.ca_cert,
		   allow_untrusted_tls = excluded.allow_untrusted_tls,
		   role = excluded.role`,
		c.ID, c.WorkspaceID, c.Name, c.Backend, c.URL, c.ProjectKey, c.ScopeJQL, c.BugIssueType,
		c.BugProjectMode, c.BugProjectKey, c.CACert, boolToInt(c.AllowUntrustedTLS), c.Role,
		c.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return Connection{}, fmt.Errorf("put connection: %w", err)
	}
	return m.Get(id)
}

// Update changes an existing connection's editable fields. Returns
// ErrNotFound if the id doesn't exist. workspace_id and created_at are not
// changed by Update.
func (m *Manager) Update(id, name, backendType, url, projectKey, scopeJQL, bugIssueType, bugProjectMode, bugProjectKey, caCert string, allowUntrustedTLS bool, role string) error {
	res, err := m.db.Exec(
		`UPDATE connection SET name = ?, backend = ?, url = ?, project_key = ?, scope_jql = ?, bug_issue_type = ?, bug_project_mode = ?, bug_project_key = ?, ca_cert = ?, allow_untrusted_tls = ?, role = ? WHERE id = ?`,
		name, backendType, url, projectKey, scopeJQL, bugIssueType, bugProjectMode, bugProjectKey,
		caCert, boolToInt(allowUntrustedTLS), roleOrDefault(role), id)
	if err != nil {
		return fmt.Errorf("update connection: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes a connection, or returns ErrNotFound.
func (m *Manager) Delete(id string) error {
	res, err := m.db.Exec(`DELETE FROM connection WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete connection: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// NewID generates a new connection id (exported so callers that create
// non-primary connections in later bridge tasks don't need to import uuid
// directly).
func NewID() string { return uuid.NewString() }

// boolToInt converts a bool to a SQLite-compatible integer (1 / 0).
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
