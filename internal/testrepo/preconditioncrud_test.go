package testrepo_test

import (
	"testing"

	"xray-test-manager/internal/testrepo"
)

// TestListPreconditionsWithUsageCountsLinks verifies the management-view rollup
// reports the number of Tests referencing each Precondition.
func TestListPreconditionsWithUsageCountsLinks(t *testing.T) {
	repo := seedTestWithPreconditions(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-2", ID: "2", Summary: "b"},
	}); err != nil {
		t.Fatalf("seed test: %v", err)
	}
	// QA-P-1 used by two tests, QA-P-2 by one, QA-P-3 by none.
	if err := repo.SetTestPreconditions("p1", "QA-1", []string{"QA-P-1", "QA-P-2"}); err != nil {
		t.Fatalf("link QA-1: %v", err)
	}
	if err := repo.SetTestPreconditions("p1", "QA-2", []string{"QA-P-1"}); err != nil {
		t.Fatalf("link QA-2: %v", err)
	}

	usage, err := repo.ListPreconditionsWithUsage("p1")
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	counts := map[string]int{}
	for _, u := range usage {
		counts[u.Key] = u.TestCount
	}
	if counts["QA-P-1"] != 2 {
		t.Errorf("QA-P-1 used by %d tests, want 2", counts["QA-P-1"])
	}
	if counts["QA-P-2"] != 1 {
		t.Errorf("QA-P-2 used by %d tests, want 1", counts["QA-P-2"])
	}
	if counts["QA-P-3"] != 0 {
		t.Errorf("QA-P-3 used by %d tests, want 0", counts["QA-P-3"])
	}
}

// TestListTestsForPreconditionReturnsLinkedTests verifies the detail pane lists
// exactly the Tests linked to a Precondition, with summary/status.
func TestListTestsForPreconditionReturnsLinkedTests(t *testing.T) {
	repo := seedTestWithPreconditions(t)
	if err := repo.SetTestPreconditions("p1", "QA-1", []string{"QA-P-1"}); err != nil {
		t.Fatalf("link: %v", err)
	}
	tests, err := repo.ListTestsForPrecondition("p1", "QA-P-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tests) != 1 || tests[0].Key != "QA-1" {
		t.Fatalf("expected [QA-1], got %+v", tests)
	}
	if tests[0].Summary != "a" {
		t.Errorf("summary = %q, want %q", tests[0].Summary, "a")
	}
}

// TestDeletePreconditionQueuesDeleteAndUnlinks verifies deleting a committed
// Precondition removes it locally, unlinks its tests, and queues a delete.
func TestDeletePreconditionQueuesDeleteAndUnlinks(t *testing.T) {
	repo := seedTestWithPreconditions(t)
	if err := repo.SetTestPreconditions("p1", "QA-1", []string{"QA-P-1"}); err != nil {
		t.Fatalf("link: %v", err)
	}

	if err := repo.DeletePrecondition("p1", "QA-P-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	all, err := repo.ListAllPreconditions("p1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, p := range all {
		if p.Key == "QA-P-1" {
			t.Fatal("QA-P-1 should be gone from the cache")
		}
	}
	linked, err := repo.ListTestPreconditions("p1", "QA-1")
	if err != nil {
		t.Fatalf("linked: %v", err)
	}
	if len(linked) != 0 {
		t.Errorf("QA-1 should have no preconditions, got %+v", linked)
	}

	pending, err := repo.ListPendingChanges("p1")
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	var found bool
	for _, p := range pending {
		if p.EntityType == "precondition_delete" && p.EntityKey == "QA-P-1" {
			found = true
		}
	}
	if !found {
		t.Error("expected a precondition_delete pending row for QA-P-1")
	}
}

// TestDeleteLocalPreconditionCancelsCreate verifies deleting a not-yet-committed
// Precondition cancels its create instead of queuing a remote delete.
func TestDeleteLocalPreconditionCancelsCreate(t *testing.T) {
	repo := seedTestWithPreconditions(t)
	tempKey, err := repo.CreatePrecondition("p1", "QA", "Brand new", "Manual", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := repo.DeletePrecondition("p1", tempKey); err != nil {
		t.Fatalf("delete: %v", err)
	}

	pending, err := repo.ListPendingChanges("p1")
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	for _, p := range pending {
		if p.EntityKey == tempKey {
			t.Errorf("local precondition should leave no pending rows, found %s", p.EntityType)
		}
	}
}
