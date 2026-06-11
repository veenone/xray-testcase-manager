package testrepo_test

import (
	"testing"

	"xray-test-manager/internal/testrepo"
)

func seedConflictRepo(t *testing.T) *testrepo.Repository {
	t.Helper()
	repo := newRepo(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "Original", Updated: "2026-01-01T00:00:00Z"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return repo
}

func TestRebaseTestConflictUpdatesBaseVersion(t *testing.T) {
	repo := seedConflictRepo(t)
	if err := repo.EditTestField("p1", "QA-1", "summary", "Edited"); err != nil {
		t.Fatalf("edit: %v", err)
	}

	if err := repo.RebaseTestConflict("p1", "QA-1", "2026-06-08T10:00:00Z"); err != nil {
		t.Fatalf("rebase: %v", err)
	}

	changes, _ := repo.ListPendingChanges("p1")
	if len(changes) != 1 {
		t.Fatalf("want 1 pending change, got %d", len(changes))
	}
	if changes[0].BaseVersion != "2026-06-08T10:00:00Z" {
		t.Errorf("base_version = %q after rebase, want the new remote version", changes[0].BaseVersion)
	}
}

func TestDiscardTestChangesRevertsAndClears(t *testing.T) {
	repo := seedConflictRepo(t)
	if err := repo.EditTestField("p1", "QA-1", "summary", "Edited"); err != nil {
		t.Fatalf("edit summary: %v", err)
	}
	if err := repo.EditTestField("p1", "QA-1", "priority", "High"); err != nil {
		t.Fatalf("edit priority: %v", err)
	}

	if err := repo.DiscardTestChanges("p1", "QA-1"); err != nil {
		t.Fatalf("discard: %v", err)
	}

	changes, _ := repo.ListPendingChanges("p1")
	if len(changes) != 0 {
		t.Errorf("want 0 pending changes after discard, got %d", len(changes))
	}
	tc, _ := repo.GetTest("p1", "QA-1")
	if tc.Summary != "Original" {
		t.Errorf("summary = %q after discard, want reverted to Original", tc.Summary)
	}
}

func TestDiscardAllPendingChangesRevertsEverything(t *testing.T) {
	repo := seedConflictRepo(t)
	// A field edit and a locally-added step span two revert paths.
	if err := repo.EditTestField("p1", "QA-1", "summary", "Edited"); err != nil {
		t.Fatalf("edit summary: %v", err)
	}
	if _, err := repo.AddTestStep("p1", "QA-1", "new step", "", "ok"); err != nil {
		t.Fatalf("add step: %v", err)
	}
	if n := mustCount(t, repo); n != 2 {
		t.Fatalf("want 2 pending before discard-all, got %d", n)
	}

	discarded, err := repo.DiscardAllPendingChanges("p1")
	if err != nil {
		t.Fatalf("discard all: %v", err)
	}
	if discarded != 2 {
		t.Errorf("discarded = %d, want 2", discarded)
	}

	if n := mustCount(t, repo); n != 0 {
		t.Errorf("want 0 pending after discard-all, got %d", n)
	}
	tc, _ := repo.GetTest("p1", "QA-1")
	if tc.Summary != "Original" {
		t.Errorf("summary = %q after discard-all, want reverted to Original", tc.Summary)
	}
	steps, _ := repo.ListTestSteps("p1", "QA-1")
	if len(steps) != 0 {
		t.Errorf("want the locally-added step gone after discard-all, got %d steps", len(steps))
	}
}

func mustCount(t *testing.T, repo *testrepo.Repository) int {
	t.Helper()
	changes, err := repo.ListPendingChanges("p1")
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	return len(changes)
}
