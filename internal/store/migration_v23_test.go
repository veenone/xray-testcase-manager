package store

import "testing"

func TestMigrationV23AddsContainerParentColumns(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// Insert a container using the new columns; fails if they don't exist.
	if _, err := db.DB().Exec(
		`INSERT INTO test_container
		   (profile_id, jira_key, kind, summary, status, parent_key, issue_type)
		 VALUES ('p1', 'DEMO-TE-1', 'testexec', 'Cycle', 'Open', 'DEMO-S-1', 'Sub Test Execution')`,
	); err != nil {
		t.Fatalf("insert with parent_key/issue_type: %v", err)
	}
	var parent, issueType string
	if err := db.DB().QueryRow(
		`SELECT parent_key, issue_type FROM test_container WHERE jira_key = 'DEMO-TE-1'`,
	).Scan(&parent, &issueType); err != nil {
		t.Fatalf("select: %v", err)
	}
	if parent != "DEMO-S-1" || issueType != "Sub Test Execution" {
		t.Fatalf("got parent=%q issueType=%q", parent, issueType)
	}
}

func TestMigrationV32AddsContainerParentSummary(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if _, err := db.DB().Exec(
		`INSERT INTO test_container
		   (profile_id, jira_key, kind, summary, status, parent_key, issue_type, parent_summary)
		 VALUES ('p1', 'DEMO-TE-2', 'testexec', 'Cycle', 'Open', 'DEMO-S-1', 'Sub Test Execution', 'Story One')`,
	); err != nil {
		t.Fatalf("insert with parent_summary: %v", err)
	}
	var parentSummary string
	if err := db.DB().QueryRow(
		`SELECT parent_summary FROM test_container WHERE jira_key = 'DEMO-TE-2'`,
	).Scan(&parentSummary); err != nil {
		t.Fatalf("select: %v", err)
	}
	if parentSummary != "Story One" {
		t.Fatalf("got parent_summary=%q, want %q", parentSummary, "Story One")
	}
	if SchemaVersion() < 32 {
		t.Fatalf("SchemaVersion() = %d, want >= 32", SchemaVersion())
	}
}
