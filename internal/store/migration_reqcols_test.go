package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// TestMigrationAddsRequirementColumnsOnVersionCollision reproduces the schema
// version collision that broke live sync: a shared DB already stamped at the
// current schema version (as another in-flight branch leaves it) but whose
// requirement / precondition tables predate the columns this branch adds.
// The column ALTERs must run regardless of the stored version, else inserts /
// selects fail with "table requirement has no column named priority".
func TestMigrationAddsRequirementColumnsOnVersionCollision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collision.db")

	// Seed a pre-existing DB: old-shape requirement + precondition tables and a
	// schema_version already at the current version.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	seed := []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`INSERT INTO meta (key, value) VALUES ('schema_version', '37')`,
		`CREATE TABLE requirement (
			profile_id TEXT NOT NULL, jira_key TEXT NOT NULL, project_key TEXT NOT NULL,
			issue_type TEXT NOT NULL, summary TEXT NOT NULL, status TEXT NOT NULL,
			updated_at TEXT NOT NULL DEFAULT '', PRIMARY KEY (profile_id, jira_key))`,
		`CREATE TABLE precondition (
			profile_id TEXT NOT NULL, jira_key TEXT NOT NULL, summary TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT '', description TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (profile_id, jira_key))`,
	}
	for _, s := range seed {
		if _, err := raw.Exec(s); err != nil {
			t.Fatalf("seed exec: %v", err)
		}
	}
	raw.Close()

	// Open through the store: applyMigrations must add the missing columns even
	// though schema_version already reads 37.
	st, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if _, err := st.DB().Exec(
		`INSERT INTO requirement
		   (profile_id, jira_key, project_key, issue_type, summary, status, updated_at,
		    priority, components, fix_versions, sprint, description)
		 VALUES ('p1','REQ-1','P','Story','Summary','Open','',
		    'High','core','V1','Sprint 1','a desc')`,
	); err != nil {
		t.Fatalf("insert requirement with new columns after migration: %v", err)
	}
	if _, err := st.DB().Exec(
		`INSERT INTO precondition
		   (profile_id, jira_key, summary, type, description, condition)
		 VALUES ('p1','PRE-1','Summary','Manual','a desc','Given x')`,
	); err != nil {
		t.Fatalf("insert precondition with condition after migration: %v", err)
	}
}
