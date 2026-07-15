package store

import (
	"path/filepath"
	"testing"
)

// TestExternalRefBackfillV40 verifies the schema v40 migration backfills the
// neutral-identity table external_ref exactly 1:1 from the existing entity
// tables, idempotently, and without disturbing existing queries.
//
// A fresh Open already runs at v40, so to genuinely exercise the BACKFILL we
// populate the entity tables, then simulate a pre-v40 DB (option (a) in the
// brief): reset the stored schema_version below 40 and clear external_ref, then
// re-Open so applyMigrations re-runs the v40 backfill against populated tables.
func TestExternalRefBackfillV40(t *testing.T) {
	path := filepath.Join(t.TempDir(), "external_ref.db")

	st, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	// Seed representative rows across every backfilled entity table for one
	// profile, each with a non-empty "updated" value where the table has one.
	execs := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO test_case (profile_id, jira_key, jira_id, summary, updated_at)
		  VALUES ('p1','TEST-1','1001','A test','2026-01-02T03:04:05Z')`, nil},
		{`INSERT INTO test_case (profile_id, jira_key, jira_id, summary, updated_at)
		  VALUES ('p1','TEST-2','1002','Another test','2026-02-03T04:05:06Z')`, nil},
		{`INSERT INTO precondition (profile_id, jira_key, summary)
		  VALUES ('p1','PRE-1','A precondition')`, nil},
		{`INSERT INTO test_container (profile_id, jira_key, kind, summary, updated)
		  VALUES ('p1','SET-1','testset','A set','2026-03-04T05:06:07Z')`, nil},
		{`INSERT INTO bug (profile_id, jira_key, summary, updated_at)
		  VALUES ('p1','BUG-1','A bug','2026-04-05T06:07:08Z')`, nil},
		{`INSERT INTO requirement (profile_id, jira_key, summary, updated_at)
		  VALUES ('p1','REQ-1','A requirement','2026-05-06T07:08:09Z')`, nil},
	}
	for _, e := range execs {
		if _, err := st.DB().Exec(e.sql, e.args...); err != nil {
			t.Fatalf("seed exec: %v", err)
		}
	}

	// Simulate a pre-v40 database: drop the freshly-backfilled rows and roll the
	// recorded schema version back below 40 so the next Open re-runs the backfill.
	if _, err := st.DB().Exec(`DELETE FROM external_ref`); err != nil {
		t.Fatalf("clear external_ref: %v", err)
	}
	if _, err := st.DB().Exec(
		`INSERT INTO meta (key, value) VALUES ('schema_version','39')
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
	); err != nil {
		t.Fatalf("reset schema_version: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	// Re-open: applyMigrations sees current=39 < 40 and runs the backfill.
	st, err = Open(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer st.Close()

	// One external_ref row per entity row: 6 total.
	var total int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM external_ref`).Scan(&total); err != nil {
		t.Fatalf("count external_ref: %v", err)
	}
	if total != 6 {
		t.Fatalf("external_ref row count = %d, want 6", total)
	}

	// Every row must be 1:1: local_id == external_key == the source jira_key,
	// connection 'xray', and the expected entity_type / version_token.
	type want struct {
		entityType, key, versionToken string
	}
	wants := []want{
		{"test", "TEST-1", "2026-01-02T03:04:05Z"},
		{"test", "TEST-2", "2026-02-03T04:05:06Z"},
		{"precondition", "PRE-1", ""},
		{"container", "SET-1", "2026-03-04T05:06:07Z"},
		{"bug", "BUG-1", "2026-04-05T06:07:08Z"},
		{"requirement", "REQ-1", "2026-05-06T07:08:09Z"},
	}
	for _, w := range wants {
		var localID, externalKey, connection, versionToken, baseVersion, lastPulled string
		err := st.DB().QueryRow(
			`SELECT local_id, external_key, connection, version_token, base_version, last_pulled_at
			   FROM external_ref WHERE profile_id='p1' AND entity_type=? AND local_id=?`,
			w.entityType, w.key,
		).Scan(&localID, &externalKey, &connection, &versionToken, &baseVersion, &lastPulled)
		if err != nil {
			t.Fatalf("select external_ref %s/%s: %v", w.entityType, w.key, err)
		}
		if localID != w.key || externalKey != w.key {
			t.Errorf("%s/%s: local_id=%q external_key=%q, want both %q",
				w.entityType, w.key, localID, externalKey, w.key)
		}
		if connection != "xray" {
			t.Errorf("%s/%s: connection=%q, want xray", w.entityType, w.key, connection)
		}
		if versionToken != w.versionToken {
			t.Errorf("%s/%s: version_token=%q, want %q", w.entityType, w.key, versionToken, w.versionToken)
		}
		if baseVersion != "" || lastPulled != "" {
			t.Errorf("%s/%s: base_version=%q last_pulled_at=%q, want both empty",
				w.entityType, w.key, baseVersion, lastPulled)
		}
	}

	// Idempotency: re-running the open/backfill must not create duplicates.
	if err := st.Close(); err != nil {
		t.Fatalf("close before idempotency reopen: %v", err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatalf("reopen for idempotency: %v", err)
	}
	defer st.Close()
	var totalAgain int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM external_ref`).Scan(&totalAgain); err != nil {
		t.Fatalf("count external_ref after reopen: %v", err)
	}
	if totalAgain != total {
		t.Fatalf("external_ref row count after reopen = %d, want stable %d", totalAgain, total)
	}

	// Spot check: adding external_ref did not disturb existing entity queries.
	var testCount int
	if err := st.DB().QueryRow(
		`SELECT COUNT(*) FROM test_case WHERE profile_id='p1'`,
	).Scan(&testCount); err != nil {
		t.Fatalf("count test_case: %v", err)
	}
	if testCount != 2 {
		t.Fatalf("test_case row count = %d, want 2", testCount)
	}
}
