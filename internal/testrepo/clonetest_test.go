package testrepo

import (
	"path/filepath"
	"testing"

	"xray-test-manager/internal/store"
)

// CloneTest drafts a new local test copying the source's fields and steps
// (RND_P_4TFINT_05-206): a fresh temp key, a " (copy)" summary, and the same
// steps (manual content and call-test steps) queued for creation.
func TestCloneTestCopiesFieldsAndSteps(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	repo := NewRepository(st)

	if err := repo.UpsertTests("p1", []TestCase{
		{Key: "QA-1", ID: "1", Summary: "Login works", Description: "desc", Priority: "High", Labels: []string{"smoke"}, Components: []string{"auth"}},
		{Key: "QA-9", ID: "9", Summary: "Callee"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := repo.SetTestSteps("p1", "QA-1", []Step{
		{XrayID: "10", Index: 1, Action: "open app", Data: "", Expected: "home shows"},
		{XrayID: "11", Index: 2, CalledTestKey: "QA-9"},
	}); err != nil {
		t.Fatalf("seed steps: %v", err)
	}

	tempKey, err := repo.CloneTest("p1", "QA-1")
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	if tempKey == "QA-1" || tempKey == "" {
		t.Fatalf("clone should get a fresh temp key, got %q", tempKey)
	}

	clone, err := repo.GetTest("p1", tempKey)
	if err != nil {
		t.Fatalf("get clone: %v", err)
	}
	if clone.Summary != "Login works (copy)" {
		t.Errorf("clone summary = %q, want \"Login works (copy)\"", clone.Summary)
	}
	if clone.Priority != "High" {
		t.Errorf("clone priority = %q, want High", clone.Priority)
	}

	steps, err := repo.ListTestSteps("p1", tempKey)
	if err != nil {
		t.Fatalf("clone steps: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("clone has %d steps, want 2", len(steps))
	}
	if steps[0].Action != "open app" || steps[0].Expected != "home shows" {
		t.Errorf("manual step not copied: %+v", steps[0])
	}
	if steps[1].CalledTestKey != "QA-9" {
		t.Errorf("call step not copied: %+v", steps[1])
	}
}
