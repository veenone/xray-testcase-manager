package store_test

import (
	"path/filepath"
	"testing"

	"xray-test-manager/internal/store"
)

// TestSchemaV31FixVersionsOnTestCase asserts that test_case has a fix_versions
// column (added in schema v31) and that the schema version is at least 31.
func TestSchemaV31FixVersionsOnTestCase(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "v31.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if store.SchemaVersion() < 31 {
		t.Errorf("schemaVersion = %d, want >= 31", store.SchemaVersion())
	}

	// Column must exist and be usable.
	db := st.DB()
	if _, err := db.Exec(
		`INSERT INTO test_case
		   (profile_id, jira_key, jira_id, summary, fix_versions)
		 VALUES ('p1', 'QA-1', '1', 'Test', '["1.5.0"]')`,
	); err != nil {
		t.Errorf("fix_versions column should be usable on test_case: %v", err)
	}
}

// TestSchemaV31MigrationIdempotent verifies that re-opening a v31 database
// does not error (migration block must be idempotent via the duplicate-column
// guard).
func TestSchemaV31MigrationIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v31idem.db")
	for i := range 2 {
		st, err := store.Open(dbPath)
		if err != nil {
			t.Fatalf("open #%d: %v", i+1, err)
		}
		st.Close()
	}
}
