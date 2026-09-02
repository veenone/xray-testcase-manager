package testrepo_test

import (
	"strings"
	"testing"

	"xray-test-manager/internal/testrepo"
)

func seedRenameTests(t *testing.T) *testrepo.Repository {
	t.Helper()
	repo := newRepo(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "Login works"},
		{Key: "QA-2", ID: "2", Summary: "Logout works"},
	}); err != nil {
		t.Fatalf("seed tests: %v", err)
	}
	return repo
}

func TestBulkRenameTestsQueuesSummaryEdits(t *testing.T) {
	repo := seedRenameTests(t)

	res, err := repo.BulkRenameTests("p1", []testrepo.TestRename{
		{Key: "QA-1", Summary: "[SMOKE] Login works", ExpectedBefore: "Login works"},
		{Key: "QA-2", Summary: "[SMOKE] Logout works", ExpectedBefore: "Logout works"},
	})
	if err != nil {
		t.Fatalf("bulk rename: %v", err)
	}
	if len(res.Succeeded) != 2 {
		t.Fatalf("succeeded %v, want both", res.Succeeded)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("failed %+v, want none", res.Failed)
	}

	// The rename must be an ordinary summary field edit, so the existing
	// journal and commit path carry it with no special casing.
	pcs, err := repo.ListPendingChanges("p1")
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	got := map[string]string{}
	for _, pc := range pcs {
		if pc.Field == "summary" {
			got[pc.EntityKey] = pc.AfterVal
		}
	}
	if got["QA-1"] != "[SMOKE] Login works" {
		t.Errorf("QA-1 pending after = %q", got["QA-1"])
	}
	if got["QA-2"] != "[SMOKE] Logout works" {
		t.Errorf("QA-2 pending after = %q", got["QA-2"])
	}
}

func TestBulkRenameTestsReportsUnknownKeyWithoutStoppingSiblings(t *testing.T) {
	repo := seedRenameTests(t)

	res, err := repo.BulkRenameTests("p1", []testrepo.TestRename{
		{Key: "QA-1", Summary: "[SMOKE] Login works", ExpectedBefore: "Login works"},
		{Key: "QA-GONE", Summary: "[SMOKE] Nothing", ExpectedBefore: "Nothing"},
	})
	if err != nil {
		t.Fatalf("bulk rename: %v", err)
	}
	if len(res.Succeeded) != 1 || res.Succeeded[0] != "QA-1" {
		t.Errorf("succeeded %v, want just QA-1", res.Succeeded)
	}
	if len(res.Failed) != 1 || res.Failed[0].TestKey != "QA-GONE" {
		t.Errorf("failed %+v, want just QA-GONE", res.Failed)
	}
}

func TestBulkRenameTestsRejectsAStalePreview(t *testing.T) {
	// A sync can rewrite test_case.summary while the rename modal is open.
	// Applying a rename computed from the old value would revert what the sync
	// just pulled and queue that reversion for Jira (spec N2).
	repo := seedRenameTests(t)

	// Stand in for the sync: the stored summary moves on.
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "Login works, revised upstream"},
	}); err != nil {
		t.Fatalf("simulate sync: %v", err)
	}

	res, err := repo.BulkRenameTests("p1", []testrepo.TestRename{
		{Key: "QA-1", Summary: "[SMOKE] Login works", ExpectedBefore: "Login works"},
	})
	if err != nil {
		t.Fatalf("bulk rename: %v", err)
	}
	if len(res.Succeeded) != 0 {
		t.Fatalf("succeeded %v, want the stale rename rejected", res.Succeeded)
	}
	if len(res.Failed) != 1 {
		t.Fatalf("failed %+v, want one rejection", res.Failed)
	}
	if !strings.Contains(res.Failed[0].Error, "changed") {
		t.Errorf("reason = %q, want it to say the summary changed", res.Failed[0].Error)
	}

	// The synced value must survive untouched.
	got, err := repo.ListTestSummaries("p1", []string{"QA-1"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got[0].Summary != "Login works, revised upstream" {
		t.Errorf("summary = %q, want the synced value intact", got[0].Summary)
	}
}

func TestBulkRenameTestsEmptyListIsNoOp(t *testing.T) {
	repo := seedRenameTests(t)

	res, err := repo.BulkRenameTests("p1", nil)
	if err != nil {
		t.Fatalf("bulk rename: %v", err)
	}
	if len(res.Succeeded) != 0 || len(res.Failed) != 0 {
		t.Errorf("got %+v, want an empty result", res)
	}
	pcs, err := repo.ListPendingChanges("p1")
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pcs) != 0 {
		t.Errorf("got %d pending changes, want none", len(pcs))
	}
}
