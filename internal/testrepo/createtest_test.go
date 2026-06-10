package testrepo_test

import (
	"path/filepath"
	"testing"

	"xray-test-manager/internal/store"
	"xray-test-manager/internal/testrepo"
)

func newCreateRepo(t *testing.T) *testrepo.Repository {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return testrepo.NewRepository(st)
}

func TestCreateTestQueuesCreateStepsFolderAndPreconditions(t *testing.T) {
	repo := newCreateRepo(t)

	tempKey, err := repo.CreateTest("p1", testrepo.TestDraft{
		Summary:     "User can reset password",
		Description: "desc",
		Priority:    "High",
		Labels:      "smoke api",
		Components:  "Auth, Frontend",
		FolderID:    "/Authentication/Login",
		Steps: []testrepo.StepDraft{
			{Action: "Open reset page", Expected: "Form shown"},
			{Action: "", Data: "", Expected: ""}, // blank step is dropped
		},
		PrecondKeys: []string{"QA-P-1"},
	})
	if err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	if tempKey == "" || tempKey[:4] != "NEW-" {
		t.Fatalf("tempKey = %q, want NEW-*", tempKey)
	}

	changes, err := repo.ListPendingChanges("p1")
	if err != nil {
		t.Fatalf("ListPendingChanges: %v", err)
	}
	byType := map[string]int{}
	for _, c := range changes {
		byType[c.EntityType]++
		if c.EntityType == "precondition_set" && c.EntityKey != tempKey {
			t.Errorf("precondition_set keyed by %q, want %q", c.EntityKey, tempKey)
		}
		if c.EntityType == "test_case" && c.Field == "folder" && c.EntityKey != tempKey {
			t.Errorf("folder row keyed by %q, want %q", c.EntityKey, tempKey)
		}
	}
	if byType["test_create"] != 1 {
		t.Errorf("test_create rows = %d, want 1", byType["test_create"])
	}
	if byType["precondition_set"] != 1 {
		t.Errorf("precondition_set rows = %d, want 1", byType["precondition_set"])
	}
	if byType["test_case"] != 1 { // the folder move
		t.Errorf("test_case (folder) rows = %d, want 1", byType["test_case"])
	}

	// The local Test exists with its folder and a single non-blank step.
	steps, err := repo.ListTestSteps("p1", tempKey)
	if err != nil {
		t.Fatalf("ListTestSteps: %v", err)
	}
	if len(steps) != 1 || steps[0].Action != "Open reset page" {
		t.Fatalf("steps = %+v, want one 'Open reset page'", steps)
	}
}

func TestRenameTestRewritesDependentPendingRows(t *testing.T) {
	repo := newCreateRepo(t)

	tempKey, err := repo.CreateTest("p1", testrepo.TestDraft{
		Summary:     "New test",
		FolderID:    "/A/B",
		PrecondKeys: []string{"QA-P-1"},
	})
	if err != nil {
		t.Fatalf("CreateTest: %v", err)
	}

	if err := repo.RenameTest("p1", tempKey, "QA-500"); err != nil {
		t.Fatalf("RenameTest: %v", err)
	}

	changes, err := repo.ListPendingChanges("p1")
	if err != nil {
		t.Fatalf("ListPendingChanges: %v", err)
	}
	for _, c := range changes {
		if c.EntityType == "precondition_set" && c.EntityKey != "QA-500" {
			t.Errorf("precondition_set still keyed by %q, want QA-500", c.EntityKey)
		}
		if c.EntityType == "test_case" && c.Field == "folder" && c.EntityKey != "QA-500" {
			t.Errorf("folder row still keyed by %q, want QA-500", c.EntityKey)
		}
		// The test_create row keeps its temp key — it is committed by id, then
		// rewritten in the cache by RenameTest's own table updates.
	}
}
