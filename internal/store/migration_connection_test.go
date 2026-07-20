package store

import (
	"path/filepath"
	"testing"
)

// TestConnectionBackfillV43 verifies the schema v43 migration creates the
// connection table and backfills exactly one 'both'-role connection per
// existing profiles row, 1:1 (id == workspace_id == the profile's id, every
// backend field copied), idempotently, and without disturbing profiles
// (which remains the read source of truth for task B1).
//
// A fresh Open already runs at v43, so to genuinely exercise the BACKFILL we
// seed profiles, then simulate a pre-v43 DB (mirroring
// external_ref_migration_test.go): reset the stored schema_version below 43
// and clear connection, then re-Open so applyMigrations re-runs the v43
// backfill against populated profiles rows.
func TestConnectionBackfillV43(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connection_backfill.db")

	st, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	// Seed a mix of profiles, including one on the "kiwi" backend, each with
	// every backend field populated so the backfill's field-copy can be
	// checked precisely.
	profiles := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO profiles
		    (id, name, jira_url, project_key, created_at, scope_jql, bug_issue_type, bug_project_mode, bug_project_key, ca_cert, allow_untrusted_tls, backend)
		  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			[]any{"p1", "QA", "https://jira.example.com", "QA", "2026-01-01T00:00:00Z",
				"labels = smoke", "Defect", "dedicated", "DEFECTS", "-----BEGIN CERT-----", 1, "xray"}},
		{`INSERT INTO profiles
		    (id, name, jira_url, project_key, created_at, scope_jql, bug_issue_type, bug_project_mode, bug_project_key, ca_cert, allow_untrusted_tls, backend)
		  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			[]any{"p2", "Lab", "https://kiwi.example.com", "LAB", "2026-02-02T00:00:00Z",
				"", "Bug", "test", "", "", 0, "kiwi"}},
	}
	for _, p := range profiles {
		if _, err := st.DB().Exec(p.sql, p.args...); err != nil {
			t.Fatalf("seed profile: %v", err)
		}
	}

	// Simulate a pre-v43 database: drop the connection rows created by the
	// fresh Open above (there weren't any, since the profiles didn't exist
	// yet) and roll the recorded schema version back below 43 so the next
	// Open re-runs the backfill against the now-populated profiles table.
	if _, err := st.DB().Exec(`DELETE FROM connection`); err != nil {
		t.Fatalf("clear connection: %v", err)
	}
	if _, err := st.DB().Exec(
		`INSERT INTO meta (key, value) VALUES ('schema_version','42')
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
	); err != nil {
		t.Fatalf("reset schema_version: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	// Re-open: applyMigrations sees current=42 < 43 and runs the backfill.
	st, err = Open(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer st.Close()

	var total int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM connection`).Scan(&total); err != nil {
		t.Fatalf("count connection: %v", err)
	}
	if total != 2 {
		t.Fatalf("connection row count = %d, want 2", total)
	}

	type want struct {
		id, name, backend, url, projectKey, scopeJQL, bugIssueType, bugProjectMode, bugProjectKey, caCert, createdAt string
		allowUntrustedTLS                                                                                            int
	}
	wants := []want{
		{"p1", "QA", "xray", "https://jira.example.com", "QA", "labels = smoke", "Defect", "dedicated", "DEFECTS", "-----BEGIN CERT-----", "2026-01-01T00:00:00Z", 1},
		{"p2", "Lab", "kiwi", "https://kiwi.example.com", "LAB", "", "Bug", "test", "", "", "2026-02-02T00:00:00Z", 0},
	}
	for _, w := range wants {
		var id, workspaceID, name, backend, url, projectKey, scopeJQL, bugIssueType, bugProjectMode, bugProjectKey, caCert, role, createdAt string
		var allowUntrustedTLS int
		err := st.DB().QueryRow(
			`SELECT id, workspace_id, name, backend, url, project_key, scope_jql, bug_issue_type, bug_project_mode, bug_project_key, ca_cert, allow_untrusted_tls, role, created_at
			   FROM connection WHERE id = ?`, w.id,
		).Scan(&id, &workspaceID, &name, &backend, &url, &projectKey, &scopeJQL, &bugIssueType,
			&bugProjectMode, &bugProjectKey, &caCert, &allowUntrustedTLS, &role, &createdAt)
		if err != nil {
			t.Fatalf("select connection %s: %v", w.id, err)
		}
		if id != w.id || workspaceID != w.id {
			t.Errorf("%s: id=%q workspace_id=%q, want both %q", w.id, id, workspaceID, w.id)
		}
		if name != w.name || backend != w.backend || url != w.url || projectKey != w.projectKey ||
			scopeJQL != w.scopeJQL || bugIssueType != w.bugIssueType || bugProjectMode != w.bugProjectMode ||
			bugProjectKey != w.bugProjectKey || caCert != w.caCert || allowUntrustedTLS != w.allowUntrustedTLS ||
			createdAt != w.createdAt {
			t.Errorf("%s: fields did not copy 1:1 from profiles: got %+v", w.id,
				struct {
					name, backend, url, projectKey, scopeJQL, bugIssueType, bugProjectMode, bugProjectKey, caCert, createdAt string
					allowUntrustedTLS                                                                                        int
				}{name, backend, url, projectKey, scopeJQL, bugIssueType, bugProjectMode, bugProjectKey, caCert, createdAt, allowUntrustedTLS})
		}
		if role != "both" {
			t.Errorf("%s: role = %q, want 'both'", w.id, role)
		}
	}

	// Idempotency: re-running the open/backfill must not create duplicates or
	// alter existing rows.
	if err := st.Close(); err != nil {
		t.Fatalf("close before idempotency reopen: %v", err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatalf("reopen for idempotency: %v", err)
	}
	defer st.Close()
	var totalAgain int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM connection`).Scan(&totalAgain); err != nil {
		t.Fatalf("count connection after reopen: %v", err)
	}
	if totalAgain != total {
		t.Fatalf("connection row count after reopen = %d, want stable %d", totalAgain, total)
	}

	// profiles remains untouched and fully intact — B1 is read-behaviour-
	// preserving.
	var profileCount int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM profiles`).Scan(&profileCount); err != nil {
		t.Fatalf("count profiles: %v", err)
	}
	if profileCount != 2 {
		t.Fatalf("profiles row count = %d, want 2", profileCount)
	}

	if SchemaVersion() < 43 {
		t.Fatalf("SchemaVersion() = %d, want >= 43", SchemaVersion())
	}
}
