package testrepo_test

import (
	"testing"

	"xray-test-manager/internal/testrepo"
)

// seedPreconditionFixture creates one Test and two Preconditions so the cache
// helpers have something to link. Returns the repo.
func seedPreconditionFixture(t *testing.T) *testrepo.Repository {
	t.Helper()
	repo := newRepo(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "Login", Updated: "2026-08-21T00:00:00.000+0000"},
	}); err != nil {
		t.Fatalf("seed test: %v", err)
	}
	if err := repo.UpsertPreconditions("p1", []testrepo.Precondition{
		{Key: "PC-1", Summary: "HSM online", Type: "Manual"},
		{Key: "PC-2", Summary: "Card inserted", Type: "Manual"},
	}); err != nil {
		t.Fatalf("seed preconditions: %v", err)
	}
	return repo
}

// TestCacheTestPreconditionLinksWritesAndReplaces covers the refresh path used
// by the live fallback (RND_P_4TFINT_05-339): links appear, then a second call
// replaces rather than accumulates them.
func TestCacheTestPreconditionLinksWritesAndReplaces(t *testing.T) {
	repo := seedPreconditionFixture(t)

	if err := repo.CacheTestPreconditionLinks("p1", "QA-1", []string{"PC-1", "PC-2"}); err != nil {
		t.Fatalf("cache links: %v", err)
	}
	got, err := repo.ListTestPreconditions("p1", "QA-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 linked preconditions, got %d: %+v", len(got), got)
	}
	if got[0].Summary != "HSM online" {
		t.Errorf("want the joined precondition row, got %+v", got[0])
	}

	// A refresh that reports fewer links must shrink the set, not merge into it.
	if err := repo.CacheTestPreconditionLinks("p1", "QA-1", []string{"PC-2"}); err != nil {
		t.Fatalf("re-cache links: %v", err)
	}
	got, err = repo.ListTestPreconditions("p1", "QA-1")
	if err != nil {
		t.Fatalf("list after replace: %v", err)
	}
	if len(got) != 1 || got[0].Key != "PC-2" {
		t.Fatalf("want only PC-2 after replace, got %+v", got)
	}
}

// TestCacheTestPreconditionLinksQueuesNoPendingChange is the guard that keeps a
// cache refresh from being mistaken for a user edit. Reading state back from
// Jira must never queue something to push to Jira.
func TestCacheTestPreconditionLinksQueuesNoPendingChange(t *testing.T) {
	repo := seedPreconditionFixture(t)

	if err := repo.CacheTestPreconditionLinks("p1", "QA-1", []string{"PC-1"}); err != nil {
		t.Fatalf("cache links: %v", err)
	}
	pending, err := repo.ListPendingChanges("p1")
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("want no pending changes from a cache refresh, got %+v", pending)
	}
}

// TestCacheTestPreconditionLinksIsPerTest checks the replace is scoped to one
// Test, so refreshing one panel cannot wipe another Test's links.
func TestCacheTestPreconditionLinksIsPerTest(t *testing.T) {
	repo := seedPreconditionFixture(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-2", ID: "2", Summary: "Logout", Updated: "2026-08-21T00:00:00.000+0000"},
	}); err != nil {
		t.Fatalf("seed second test: %v", err)
	}
	if err := repo.CacheTestPreconditionLinks("p1", "QA-2", []string{"PC-1"}); err != nil {
		t.Fatalf("cache QA-2: %v", err)
	}
	if err := repo.CacheTestPreconditionLinks("p1", "QA-1", []string{"PC-2"}); err != nil {
		t.Fatalf("cache QA-1: %v", err)
	}

	other, err := repo.ListTestPreconditions("p1", "QA-2")
	if err != nil {
		t.Fatalf("list QA-2: %v", err)
	}
	if len(other) != 1 || other[0].Key != "PC-1" {
		t.Fatalf("QA-2 links disturbed by refreshing QA-1: %+v", other)
	}
}

// TestHasPendingPreconditionChangeDetectsLocalEdit is the guard behind the
// "don't refresh over an uncommitted edit" rule. It matters most for the empty
// case: a user who removed every precondition locally leaves no links behind,
// which by link count alone is indistinguishable from a cache that was never
// populated.
func TestHasPendingPreconditionChangeDetectsLocalEdit(t *testing.T) {
	repo := seedPreconditionFixture(t)
	if err := repo.CacheTestPreconditionLinks("p1", "QA-1", []string{"PC-1"}); err != nil {
		t.Fatalf("cache links: %v", err)
	}

	pending, err := repo.HasPendingPreconditionChange("p1", "QA-1")
	if err != nil {
		t.Fatalf("check pending: %v", err)
	}
	if pending {
		t.Fatal("want no pending change before any edit")
	}

	// Remove every precondition locally. The link table is now empty *and* an
	// uncommitted change exists.
	if err := repo.SetTestPreconditions("p1", "QA-1", nil); err != nil {
		t.Fatalf("set preconditions: %v", err)
	}
	linked, err := repo.ListTestPreconditions("p1", "QA-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(linked) != 0 {
		t.Fatalf("want no links after removing them, got %+v", linked)
	}

	pending, err = repo.HasPendingPreconditionChange("p1", "QA-1")
	if err != nil {
		t.Fatalf("check pending after edit: %v", err)
	}
	if !pending {
		t.Fatal("want a pending change after removing every precondition locally")
	}
}

// TestHasPendingPreconditionChangeIsPerTest checks an edit on one Test does not
// suppress the fallback for a different one.
func TestHasPendingPreconditionChangeIsPerTest(t *testing.T) {
	repo := seedPreconditionFixture(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-2", ID: "2", Summary: "Logout", Updated: "2026-08-21T00:00:00.000+0000"},
	}); err != nil {
		t.Fatalf("seed second test: %v", err)
	}
	if err := repo.SetTestPreconditions("p1", "QA-1", []string{"PC-1"}); err != nil {
		t.Fatalf("set preconditions: %v", err)
	}

	pending, err := repo.HasPendingPreconditionChange("p1", "QA-2")
	if err != nil {
		t.Fatalf("check pending: %v", err)
	}
	if pending {
		t.Fatal("QA-1's edit must not mark QA-2 as pending")
	}
}
