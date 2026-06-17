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
