package store

import (
	"path/filepath"
	"testing"
)

// TestMigrationV42AddsProfilesBackendColumn verifies that a pre-v42 database
// (profiles table without the backend column) gains it on Open, with existing
// rows defaulting to 'xray', and that the column keeps defaulting to 'xray'
// for rows inserted afterward without specifying it.
//
// baseSchema always includes the backend column, so a plain Open can't
// produce the "before" state; the pre-v42 table is built by hand, mirroring
// the approach in external_ref_migration_test.go.
func TestMigrationV42AddsProfilesBackendColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backend_migration.db")

	pre, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := pre.DB().Exec(`DROP TABLE profiles`); err != nil {
		t.Fatalf("drop profiles: %v", err)
	}
	if _, err := pre.DB().Exec(`CREATE TABLE profiles (
		id          TEXT PRIMARY KEY,
		name        TEXT NOT NULL,
		jira_url    TEXT NOT NULL,
		project_key TEXT NOT NULL,
		created_at  TEXT NOT NULL,
		scope_jql   TEXT NOT NULL DEFAULT '',
		bug_issue_type TEXT NOT NULL DEFAULT 'Bug',
		bug_project_mode TEXT NOT NULL DEFAULT 'test',
		bug_project_key TEXT NOT NULL DEFAULT '',
		ca_cert TEXT NOT NULL DEFAULT '',
		allow_untrusted_tls INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		t.Fatalf("create pre-v42 profiles table: %v", err)
	}
	if _, err := pre.DB().Exec(
		`INSERT INTO profiles (id, name, jira_url, project_key, created_at)
		 VALUES ('p1', 'Existing', 'https://jira.example.com', 'PROJ', '2026-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("seed pre-v42 profile: %v", err)
	}
	if _, err := pre.DB().Exec(
		`INSERT INTO meta (key, value) VALUES ('schema_version', '41')
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
	); err != nil {
		t.Fatalf("reset schema_version: %v", err)
	}
	if err := pre.Close(); err != nil {
		t.Fatalf("close pre-v42 store: %v", err)
	}

	// Re-open: applyMigrations sees current=41 < 42 and adds the column.
	st, err := Open(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer st.Close()

	var backend string
	if err := st.DB().QueryRow(`SELECT backend FROM profiles WHERE id = 'p1'`).Scan(&backend); err != nil {
		t.Fatalf("select backend: %v", err)
	}
	if backend != "xray" {
		t.Fatalf("existing row backend = %q, want 'xray'", backend)
	}

	// A freshly inserted row also defaults to 'xray' without specifying the column.
	if _, err := st.DB().Exec(
		`INSERT INTO profiles (id, name, jira_url, project_key, created_at)
		 VALUES ('p2', 'New', 'https://jira.example.com', 'PROJ2', '2026-01-02T00:00:00Z')`,
	); err != nil {
		t.Fatalf("insert new profile: %v", err)
	}
	var newBackend string
	if err := st.DB().QueryRow(`SELECT backend FROM profiles WHERE id = 'p2'`).Scan(&newBackend); err != nil {
		t.Fatalf("select new backend: %v", err)
	}
	if newBackend != "xray" {
		t.Fatalf("new row backend = %q, want 'xray'", newBackend)
	}

	if SchemaVersion() < 42 {
		t.Fatalf("SchemaVersion() = %d, want >= 42", SchemaVersion())
	}

	// Idempotency: reopening again must not error (duplicate-column tolerance).
	if err := st.Close(); err != nil {
		t.Fatalf("close before idempotency reopen: %v", err)
	}
	if st, err = Open(path); err != nil {
		t.Fatalf("reopen for idempotency: %v", err)
	}
	defer st.Close()
}
