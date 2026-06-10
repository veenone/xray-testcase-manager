// Package profile manages connection profiles and their credentials.
//
// A profile binds the application to one Jira project and connection (FR-5).
// Each profile owns an isolated local dataset; the user switches the active
// profile to switch projects.
package profile

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"xray-test-manager/internal/store"
)

// ErrNotFound is returned when a profile id does not exist.
var ErrNotFound = errors.New("profile not found")

// Profile is one Jira project connection. Credentials are stored separately in
// the OS credential manager (see CredentialStore) — never in this struct or the
// database.
type Profile struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	JiraURL    string    `json:"jiraUrl"`
	ProjectKey string    `json:"projectKey"`
	ScopeJQL   string    `json:"scopeJql"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Manager is the profile CRUD service backed by the local store (FR-5.1).
type Manager struct {
	db *sql.DB
}

// NewManager returns a profile manager backed by the given store.
func NewManager(s *store.Store) *Manager {
	return &Manager{db: s.DB()}
}

// List returns all profiles, ordered by name.
func (m *Manager) List() ([]Profile, error) {
	rows, err := m.db.Query(
		`SELECT id, name, jira_url, project_key, scope_jql, created_at FROM profiles ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}
	defer rows.Close()

	out := []Profile{}
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Get returns one profile by id, or ErrNotFound.
func (m *Manager) Get(id string) (Profile, error) {
	row := m.db.QueryRow(
		`SELECT id, name, jira_url, project_key, scope_jql, created_at FROM profiles WHERE id = ?`, id)
	p, err := scanProfile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, ErrNotFound
	}
	return p, err
}

// Create persists a new profile, generating its id and creation timestamp.
// scopeJQL is an optional JQL fragment that narrows which Tests sync (FR-5.4).
func (m *Manager) Create(name, jiraURL, projectKey, scopeJQL string) (Profile, error) {
	p := Profile{
		ID:         uuid.NewString(),
		Name:       name,
		JiraURL:    jiraURL,
		ProjectKey: projectKey,
		ScopeJQL:   scopeJQL,
		CreatedAt:  time.Now().UTC(),
	}
	_, err := m.db.Exec(
		`INSERT INTO profiles (id, name, jira_url, project_key, scope_jql, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.JiraURL, p.ProjectKey, p.ScopeJQL, p.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return Profile{}, fmt.Errorf("create profile: %w", err)
	}
	return p, nil
}

// Update changes a profile's editable fields — name, Jira URL, project key, and
// JQL scope (FR-5) — e.g. to correct a wrong project key. Returns ErrNotFound
// if the id doesn't exist. Credentials are managed separately.
func (m *Manager) Update(id, name, jiraURL, projectKey, scopeJQL string) error {
	res, err := m.db.Exec(
		`UPDATE profiles SET name = ?, jira_url = ?, project_key = ?, scope_jql = ? WHERE id = ?`,
		name, jiraURL, projectKey, scopeJQL, id)
	if err != nil {
		return fmt.Errorf("update profile: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateScope changes a profile's JQL scope override (FR-5.4).
func (m *Manager) UpdateScope(id, scopeJQL string) error {
	res, err := m.db.Exec(
		`UPDATE profiles SET scope_jql = ? WHERE id = ?`, scopeJQL, id)
	if err != nil {
		return fmt.Errorf("update profile scope: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes a profile, or returns ErrNotFound.
func (m *Manager) Delete(id string) error {
	res, err := m.db.Exec(`DELETE FROM profiles WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete profile: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	// TODO(xtm): cascade-delete this profile's test_case / sync_state rows (FR-5.3).
	return nil
}

// scanner abstracts *sql.Row and *sql.Rows so scanProfile serves both Get and List.
type scanner interface {
	Scan(dest ...any) error
}

func scanProfile(s scanner) (Profile, error) {
	var (
		p       Profile
		created string
	)
	if err := s.Scan(&p.ID, &p.Name, &p.JiraURL, &p.ProjectKey, &p.ScopeJQL, &created); err != nil {
		return Profile{}, err
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return p, nil
}
