package store

import (
	"testing"
)

func TestSchemaHasTestRunAndExecPlan(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	for _, tbl := range []string{"test_run", "exec_plan"} {
		var name string
		err := s.db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl,
		).Scan(&name)
		if err != nil || name != tbl {
			t.Fatalf("expected table %q to exist, err=%v name=%q", tbl, err, name)
		}
	}
	if schemaVersion < 28 {
		t.Fatalf("schemaVersion = %d, want >= 28", schemaVersion)
	}
}

// TestSchemaTestRunHasTimestamps verifies that the test_run table carries the
// created_at and updated_at columns added in schema v30, and that the recorded
// schema version reflects this.
func TestSchemaTestRunHasTimestamps(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	if schemaVersion < 30 {
		t.Fatalf("schemaVersion = %d, want >= 30", schemaVersion)
	}

	// Verify both new columns exist by attempting a probe INSERT/SELECT.
	// SQLite will error immediately if a column is absent.
	if _, err := s.db.Exec(
		`INSERT INTO test_run (profile_id, exec_key, test_key, created_at, updated_at)
		 VALUES ('_probe', '_probe', '_probe', '2026-01-01T00:00:00Z', '2026-01-02T00:00:00Z')`,
	); err != nil {
		t.Fatalf("insert with created_at/updated_at failed (columns missing?): %v", err)
	}

	var createdAt, updatedAt string
	if err := s.db.QueryRow(
		`SELECT created_at, updated_at FROM test_run
		 WHERE profile_id = '_probe' AND exec_key = '_probe' AND test_key = '_probe'`,
	).Scan(&createdAt, &updatedAt); err != nil {
		t.Fatalf("select created_at/updated_at: %v", err)
	}
	if createdAt != "2026-01-01T00:00:00Z" {
		t.Errorf("created_at = %q, want 2026-01-01T00:00:00Z", createdAt)
	}
	if updatedAt != "2026-01-02T00:00:00Z" {
		t.Errorf("updated_at = %q, want 2026-01-02T00:00:00Z", updatedAt)
	}
}
