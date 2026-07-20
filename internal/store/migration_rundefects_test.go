package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// TestMigrationV40AddsRunDefectsAndCommentColumns verifies a fresh database
// (baseSchema path) has the run-defects/remarks columns: test_container_test's
// locally-staged run_defects/run_comment and test_run's synced comment mirror.
func TestMigrationV40AddsRunDefectsAndCommentColumns(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if _, err := db.DB().Exec(
		`INSERT INTO test_container_test
		   (profile_id, container_key, test_key, run_status, run_defects, run_comment)
		 VALUES ('p1', 'DEMO-TE-1', 'DEMO-T-1', 'PASS', '["DEMO-BUG-1","DEMO-BUG-2"]', 'looked fine')`,
	); err != nil {
		t.Fatalf("insert test_container_test with run_defects/run_comment: %v", err)
	}
	var defects, comment string
	if err := db.DB().QueryRow(
		`SELECT run_defects, run_comment FROM test_container_test WHERE test_key = 'DEMO-T-1'`,
	).Scan(&defects, &comment); err != nil {
		t.Fatalf("select test_container_test: %v", err)
	}
	if defects != `["DEMO-BUG-1","DEMO-BUG-2"]` || comment != "looked fine" {
		t.Fatalf("got run_defects=%q run_comment=%q", defects, comment)
	}

	if _, err := db.DB().Exec(
		`INSERT INTO test_run
		   (profile_id, exec_key, test_key, run_status, comment)
		 VALUES ('p1', 'DEMO-TE-1', 'DEMO-T-1', 'PASS', 'run remark')`,
	); err != nil {
		t.Fatalf("insert test_run with comment: %v", err)
	}
	var runComment string
	if err := db.DB().QueryRow(
		`SELECT comment FROM test_run WHERE test_key = 'DEMO-T-1'`,
	).Scan(&runComment); err != nil {
		t.Fatalf("select test_run: %v", err)
	}
	if runComment != "run remark" {
		t.Fatalf("got test_run.comment=%q, want %q", runComment, "run remark")
	}

	if SchemaVersion() < 42 {
		t.Fatalf("SchemaVersion() = %d, want >= 42", SchemaVersion())
	}
}

// TestMigrationV40AddsRunDefectsColumnsOnVersionCollision reproduces the same
// cross-branch schema-version collision documented in
// migration_reqcols_test.go: a shared DB already stamped at the current
// schema version (as another in-flight branch that also bumped to v40
// leaves it) but whose test_container_test / test_run tables predate the
// run-defects/remarks columns this branch adds. Because the ALTERs run
// unconditionally (not `if current < 40` gated), they must still apply, and
// pre-existing rows must survive untouched.
func TestMigrationV40AddsRunDefectsColumnsOnVersionCollision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collision.db")

	// Seed a pre-existing DB: old-shape test_container_test + test_run tables
	// (no run_defects/run_comment/comment columns) with a pre-existing row,
	// and a schema_version already at 40 (as the other branch would leave it).
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	seed := []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`INSERT INTO meta (key, value) VALUES ('schema_version', '40')`,
		`CREATE TABLE test_container_test (
			profile_id    TEXT NOT NULL,
			container_key TEXT NOT NULL,
			test_key      TEXT NOT NULL,
			run_status    TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (profile_id, container_key, test_key)
		)`,
		`INSERT INTO test_container_test (profile_id, container_key, test_key, run_status)
		 VALUES ('p1', 'DEMO-TE-1', 'DEMO-T-1', 'PASS')`,
		`CREATE TABLE test_run (
			profile_id  TEXT NOT NULL,
			exec_key    TEXT NOT NULL,
			test_key    TEXT NOT NULL,
			run_status  TEXT DEFAULT '',
			started_at  TEXT DEFAULT '',
			finished_at TEXT DEFAULT '',
			executed_by TEXT DEFAULT '',
			environment TEXT DEFAULT '',
			defects     TEXT DEFAULT '',
			created_at  TEXT DEFAULT '',
			updated_at  TEXT DEFAULT '',
			PRIMARY KEY (profile_id, exec_key, test_key)
		)`,
		`INSERT INTO test_run (profile_id, exec_key, test_key, run_status)
		 VALUES ('p1', 'DEMO-TE-1', 'DEMO-T-1', 'PASS')`,
	}
	for _, s := range seed {
		if _, err := raw.Exec(s); err != nil {
			t.Fatalf("seed exec: %v", err)
		}
	}
	raw.Close()

	// Open through the store: applyMigrations must add the missing columns
	// even though schema_version already reads 40.
	st, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	// Pre-existing row must survive, and the new columns must default to ''.
	var status, defects, comment string
	if err := st.DB().QueryRow(
		`SELECT run_status, run_defects, run_comment FROM test_container_test WHERE test_key = 'DEMO-T-1'`,
	).Scan(&status, &defects, &comment); err != nil {
		t.Fatalf("select test_container_test after migration: %v", err)
	}
	if status != "PASS" {
		t.Fatalf("pre-existing run_status lost: got %q, want %q", status, "PASS")
	}
	if defects != "" || comment != "" {
		t.Fatalf("got run_defects=%q run_comment=%q, want empty defaults", defects, comment)
	}

	var runStatus, runComment string
	if err := st.DB().QueryRow(
		`SELECT run_status, comment FROM test_run WHERE test_key = 'DEMO-T-1'`,
	).Scan(&runStatus, &runComment); err != nil {
		t.Fatalf("select test_run after migration: %v", err)
	}
	if runStatus != "PASS" {
		t.Fatalf("pre-existing test_run.run_status lost: got %q, want %q", runStatus, "PASS")
	}
	if runComment != "" {
		t.Fatalf("got test_run.comment=%q, want empty default", runComment)
	}

	// New columns must be writable going forward.
	if _, err := st.DB().Exec(
		`UPDATE test_container_test SET run_defects = ?, run_comment = ? WHERE test_key = 'DEMO-T-1'`,
		`["DEMO-BUG-3"]`, "post-migration remark",
	); err != nil {
		t.Fatalf("update run_defects/run_comment after migration: %v", err)
	}
	if _, err := st.DB().Exec(
		`UPDATE test_run SET comment = ? WHERE test_key = 'DEMO-T-1'`,
		"post-migration run comment",
	); err != nil {
		t.Fatalf("update test_run.comment after migration: %v", err)
	}
}
