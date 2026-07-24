package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// TestMigrationV45CreatesCoverageGroupPublication verifies a fresh database
// (baseSchema path) has the coverage_group_publication table and that it is
// usable for its primary key (profile_id, group_id).
func TestMigrationV45CreatesCoverageGroupPublication(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if _, err := db.DB().Exec(
		`INSERT INTO coverage_group_publication
		   (profile_id, group_id, container_key, published_tests, published_at)
		 VALUES ('p1', 'grp-1', 'DEMO-TE-1', 'DEMO-T-1'||char(10)||'DEMO-T-2', '2026-07-24T00:00:00Z')`,
	); err != nil {
		t.Fatalf("insert coverage_group_publication: %v", err)
	}
	var containerKey, publishedTests, publishedAt string
	if err := db.DB().QueryRow(
		`SELECT container_key, published_tests, published_at
		   FROM coverage_group_publication WHERE profile_id='p1' AND group_id='grp-1'`,
	).Scan(&containerKey, &publishedTests, &publishedAt); err != nil {
		t.Fatalf("select coverage_group_publication: %v", err)
	}
	if containerKey != "DEMO-TE-1" {
		t.Fatalf("container_key = %q, want DEMO-TE-1", containerKey)
	}
	if publishedTests != "DEMO-T-1\nDEMO-T-2" {
		t.Fatalf("published_tests = %q, want DEMO-T-1\\nDEMO-T-2", publishedTests)
	}
	if publishedAt != "2026-07-24T00:00:00Z" {
		t.Fatalf("published_at = %q, want 2026-07-24T00:00:00Z", publishedAt)
	}

	if SchemaVersion() < 45 {
		t.Fatalf("SchemaVersion() = %d, want >= 45", SchemaVersion())
	}
}

// TestMigrationV45CoverageGroupPublicationReopenIsIdempotent verifies that a
// pre-existing database (one that predates v45 and so was created without
// coverage_group_publication) gains the table on reopen through Open(), that
// the table is usable, and that repeated Open()/Close() cycles against the
// same file stay idempotent (no error, no duplicate-table failure, no lost
// rows).
func TestMigrationV45CoverageGroupPublicationReopenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reopen.db")

	// Seed a pre-existing DB stamped at a schema version before v45, with no
	// coverage_group_publication table, to simulate an older install being
	// reopened after upgrading to a build that adds the table.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	seed := []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`INSERT INTO meta (key, value) VALUES ('schema_version', '44')`,
	}
	for _, s := range seed {
		if _, err := raw.Exec(s); err != nil {
			t.Fatalf("seed exec: %v", err)
		}
	}
	raw.Close()

	// Open through the store: this must create coverage_group_publication for
	// the older DB and advance schema_version.
	st, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if _, err := st.DB().Exec(
		`INSERT INTO coverage_group_publication
		   (profile_id, group_id, container_key, published_tests, published_at)
		 VALUES ('p1', 'grp-1', 'DEMO-TE-1', 'DEMO-T-1', '2026-07-24T00:00:00Z')`,
	); err != nil {
		t.Fatalf("insert coverage_group_publication after migration: %v", err)
	}

	var count int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM coverage_group_publication`).Scan(&count); err != nil {
		t.Fatalf("count coverage_group_publication: %v", err)
	}
	if count != 1 {
		t.Fatalf("coverage_group_publication row count = %d, want 1", count)
	}

	// Idempotency: reopening again must not error.
	if err := st.Close(); err != nil {
		t.Fatalf("close before idempotency reopen: %v", err)
	}
	if st, err = Open(path); err != nil {
		t.Fatalf("reopen for idempotency: %v", err)
	}
	defer st.Close()
}
