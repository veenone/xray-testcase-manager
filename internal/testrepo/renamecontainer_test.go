package testrepo_test

import (
	"testing"

	"xray-test-manager/internal/testrepo"
)

// TestRenameContainerRewritesPendingRows reproduces the "create execution, add
// test, set result, commit all" case: a new Execution's temporary key must be
// swapped to the real key across the still-pending membership and run-status
// rows, so they commit against the created issue rather than the placeholder.
func TestRenameContainerRewritesPendingRows(t *testing.T) {
	repo := newRepo(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "a"},
	}); err != nil {
		t.Fatalf("seed test: %v", err)
	}

	// New Execution with QA-1 allocated — gets a temporary key.
	res, err := repo.CreateContainerAllocation("p1", "QA", "testexec", "Cycle 1", []string{"QA-1"})
	if err != nil {
		t.Fatalf("create container: %v", err)
	}
	tempKey := res.TempKey
	if tempKey == "" {
		t.Fatal("expected a temporary container key")
	}

	// Set QA-1's run result inside the new (still-local) Execution.
	if err := repo.SetTestRunStatus("p1", tempKey, "QA-1", "PASS"); err != nil {
		t.Fatalf("set run status: %v", err)
	}

	// Commit assigns the real key; RenameContainer must rewrite the pending rows.
	if err := repo.RenameContainer("p1", tempKey, "QA-TE-9"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	pending, err := repo.ListPendingChanges("p1")
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	var sawRun bool
	for _, p := range pending {
		// The create row itself keeps the temp key until it's committed by id;
		// every dependent row must already point at the real key.
		if p.EntityType == "test_container_add" {
			continue
		}
		if p.EntityKey == tempKey || p.EntityKey == tempKey+":QA-1" {
			t.Errorf("pending row %s still references the temp key %q", p.EntityType, p.EntityKey)
		}
		if p.EntityType == "test_run" {
			sawRun = true
			if p.EntityKey != "QA-TE-9:QA-1" {
				t.Errorf("run-status key = %q, want QA-TE-9:QA-1", p.EntityKey)
			}
		}
	}
	if !sawRun {
		t.Error("expected a test_run pending row after rename")
	}
}
