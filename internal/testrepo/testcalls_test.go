package testrepo

import (
	"path/filepath"
	"testing"

	"xray-test-manager/internal/store"
)

// DistinctTestCallers lists the cached tests that call another test, de-duped
// and ordered, driving the Test Calls partial sync (RND_P_4TFINT_05-207).
func TestDistinctTestCallers(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	repo := NewRepository(st)

	if err := repo.UpsertTests("p1", []TestCase{
		{Key: "QA-1", ID: "1"}, {Key: "QA-2", ID: "2"}, {Key: "QA-3", ID: "3"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := repo.SetTestSteps("p1", "QA-1", []Step{
		{XrayID: "10", Index: 1, CalledTestKey: "QA-2"},
		{XrayID: "11", Index: 2, CalledTestKey: "QA-3"},
	}); err != nil {
		t.Fatalf("steps QA-1: %v", err)
	}
	if err := repo.SetTestSteps("p1", "QA-2", []Step{
		{XrayID: "20", Index: 1, CalledTestKey: "QA-3"},
	}); err != nil {
		t.Fatalf("steps QA-2: %v", err)
	}
	if err := repo.SetTestSteps("p1", "QA-3", []Step{
		{XrayID: "30", Index: 1, Action: "plain step"},
	}); err != nil {
		t.Fatalf("steps QA-3: %v", err)
	}

	callers, err := repo.DistinctTestCallers("p1")
	if err != nil {
		t.Fatalf("callers: %v", err)
	}
	want := []string{"QA-1", "QA-2"}
	if len(callers) != len(want) {
		t.Fatalf("callers = %v, want %v", callers, want)
	}
	for i := range want {
		if callers[i] != want[i] {
			t.Errorf("callers[%d] = %q, want %q", i, callers[i], want[i])
		}
	}
}

// TestSyncTestCallsRefreshesGraph locks in the contract behind App.SyncTestCalls
// (RND_P_4TFINT_05-227): re-pulling a caller's steps via SetTestSteps must make
// ListTestCallLinks reflect the refreshed graph — a call added, retargeted, or
// removed on the caller all show up without a full profile sync. App.SyncTestCalls
// is a thin loop of DistinctTestCallers -> client.GetTestSteps -> SetTestSteps;
// this exercises that store-side sequence directly.
func TestSyncTestCallsRefreshesGraph(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	repo := NewRepository(st)

	if err := repo.UpsertTests("p1", []TestCase{
		{Key: "QA-1", ID: "1", Summary: "caller"},
		{Key: "QA-2", ID: "2", Summary: "callee two"},
		{Key: "QA-3", ID: "3", Summary: "callee three"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Initial cache: QA-1 calls QA-2.
	if err := repo.SetTestSteps("p1", "QA-1", []Step{
		{XrayID: "10", Index: 1, CalledTestKey: "QA-2"},
	}); err != nil {
		t.Fatalf("steps QA-1: %v", err)
	}

	calledKeys := func(t *testing.T) []string {
		t.Helper()
		links, err := repo.ListTestCallLinks("p1")
		if err != nil {
			t.Fatalf("links: %v", err)
		}
		out := make([]string, len(links))
		for i, l := range links {
			out[i] = l.CalledKey
		}
		return out
	}

	if got := calledKeys(t); len(got) != 1 || got[0] != "QA-2" {
		t.Fatalf("initial called keys = %v, want [QA-2]", got)
	}

	// Simulate the refresh: the caller's remote steps now call QA-3 instead of
	// QA-2 (retargeted) and add a second call to QA-2. SyncTestCalls re-pulls
	// callers from DistinctTestCallers and rewrites their step cache.
	callers, err := repo.DistinctTestCallers("p1")
	if err != nil {
		t.Fatalf("callers: %v", err)
	}
	if len(callers) != 1 || callers[0] != "QA-1" {
		t.Fatalf("callers = %v, want [QA-1]", callers)
	}
	if err := repo.SetTestSteps("p1", "QA-1", []Step{
		{XrayID: "10", Index: 1, CalledTestKey: "QA-3"},
		{XrayID: "11", Index: 2, CalledTestKey: "QA-2"},
	}); err != nil {
		t.Fatalf("refresh steps QA-1: %v", err)
	}

	got := calledKeys(t)
	if len(got) != 2 || got[0] != "QA-3" || got[1] != "QA-2" {
		t.Fatalf("after refresh called keys = %v, want [QA-3 QA-2]", got)
	}

	// A refresh that drops every call step removes the caller from the graph.
	if err := repo.SetTestSteps("p1", "QA-1", []Step{
		{XrayID: "10", Index: 1, Action: "plain step"},
	}); err != nil {
		t.Fatalf("clear steps QA-1: %v", err)
	}
	if got := calledKeys(t); len(got) != 0 {
		t.Fatalf("after clearing calls, called keys = %v, want []", got)
	}
	if callers, err := repo.DistinctTestCallers("p1"); err != nil {
		t.Fatalf("callers after clear: %v", err)
	} else if len(callers) != 0 {
		t.Fatalf("callers after clear = %v, want []", callers)
	}
}
