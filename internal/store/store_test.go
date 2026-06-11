package store_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"xray-test-manager/internal/store"
)

// TestOpenUpgradesV2DatabaseAndAddsFolderID exercises the migration path
// that produced the user-visible "no such column: folder_id" failure: a
// database whose test_case table predates the folder_id column (every other
// v2 column is present). Open must migrate it in place without error and
// the column must be usable afterwards.
func TestOpenUpgradesV2DatabaseAndAddsFolderID(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "upgrade.db")

	// Seed a real v2-shaped database directly: every v2 column, but no
	// folder_id and no indexes that reference it.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
		CREATE TABLE test_case (
			profile_id  TEXT NOT NULL,
			jira_key    TEXT NOT NULL,
			jira_id     TEXT NOT NULL,
			summary     TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			status      TEXT NOT NULL DEFAULT '',
			priority    TEXT NOT NULL DEFAULT '',
			labels      TEXT NOT NULL DEFAULT '',
			updated_at  TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (profile_id, jira_key)
		);
		INSERT INTO meta (key, value) VALUES ('schema_version', '2');
	`); err != nil {
		t.Fatalf("seed v2 layout: %v", err)
	}
	_ = db.Close()

	// Now reopen through store.Open — the upgrade must succeed.
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("upgrade Open failed: %v", err)
	}
	defer st.Close()

	// folder_id should now exist on test_case (added by the v3 migration).
	if _, err := st.DB().Exec(
		`INSERT INTO test_case
		   (profile_id, jira_key, jira_id, summary, folder_id)
		 VALUES ('p1', 'QA-1', '1', 's', '/x')`,
	); err != nil {
		t.Errorf("folder_id should be usable after upgrade: %v", err)
	}
}

// TestOpenIsIdempotent confirms that re-opening the same database file
// produces no errors and leaves it usable.
func TestOpenIsIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "idempotent.db")

	first, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	_ = first.Close()

	second, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	_ = second.Close()
}

func TestDuplicateTablesExist(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "dup.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	db := st.DB()
	for _, table := range []string{"duplicate_ignore", "duplicate_step_scan"} {
		var name string
		err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %s missing: %v", table, err)
		}
	}
	if store.SchemaVersion() < 17 {
		t.Errorf("schemaVersion = %d, want >= 17", store.SchemaVersion())
	}
}
