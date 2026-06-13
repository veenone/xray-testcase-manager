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

func seedConflictEdit(t *testing.T) (*testrepo.Repository, int64) {
	t.Helper()
	repo := newRepo(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Priority: "Medium", Updated: "T0"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := repo.EditTestField("p1", "QA-1", "priority", "High"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	pc, _ := repo.ListPendingChanges("p1")
	if len(pc) != 1 {
		t.Fatalf("want 1 pending change, got %d", len(pc))
	}
	return repo, pc[0].ID
}

func TestResolveConflictMergeKeepTheirs(t *testing.T) {
	repo, id := seedConflictEdit(t)
	if err := repo.ResolveConflictMerge("p1", "QA-1", "T1", []testrepo.ConflictDecision{{
		PendingID: id, EntityType: "test_case", EntityKey: "QA-1",
		Field: "priority", Choice: "theirs", RemoteValue: "Critical",
	}}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	tc, _ := repo.GetTest("p1", "QA-1")
	if tc.Priority != "Critical" {
		t.Errorf("priority = %q, want Critical (kept theirs)", tc.Priority)
	}
	if pc, _ := repo.ListPendingChanges("p1"); len(pc) != 0 {
		t.Errorf("want 0 pending after keep-theirs, got %d", len(pc))
	}
}

func TestResolveConflictMergeKeepMine(t *testing.T) {
	repo, id := seedConflictEdit(t)
	if err := repo.ResolveConflictMerge("p1", "QA-1", "T1", []testrepo.ConflictDecision{{
		PendingID: id, EntityType: "test_case", EntityKey: "QA-1",
		Field: "priority", Choice: "mine", RemoteValue: "Critical",
	}}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	tc, _ := repo.GetTest("p1", "QA-1")
	if tc.Priority != "High" {
		t.Errorf("priority = %q, want High (kept mine)", tc.Priority)
	}
	pc, _ := repo.ListPendingChanges("p1")
	if len(pc) != 1 {
		t.Fatalf("want 1 pending (kept mine), got %d", len(pc))
	}
	if pc[0].BaseVersion != "T1" {
		t.Errorf("base_version = %q, want T1 (rebased onto remote)", pc[0].BaseVersion)
	}
}

func pendingIDByType(t *testing.T, repo *testrepo.Repository, etype string) int64 {
	t.Helper()
	pc, _ := repo.ListPendingChanges("p1")
	for _, c := range pc {
		if c.EntityType == etype {
			return c.ID
		}
	}
	t.Fatalf("no pending change of type %s", etype)
	return 0
}

func seedSteppedTest(t *testing.T) *testrepo.Repository {
	t.Helper()
	repo := newRepo(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{{Key: "QA-1", ID: "1", Updated: "T0"}}); err != nil {
		t.Fatalf("seed test: %v", err)
	}
	if err := repo.SetTestSteps("p1", "QA-1", []testrepo.Step{
		{XrayID: "10", Index: 1, Action: "a1"},
		{XrayID: "11", Index: 2, Action: "a2"},
		{XrayID: "12", Index: 3, Action: "a3"},
	}); err != nil {
		t.Fatalf("seed steps: %v", err)
	}
	return repo
}

func TestResolveConflictMergeStepOrderKeepTheirs(t *testing.T) {
	repo := seedSteppedTest(t)
	if err := repo.ReorderTestSteps("p1", "QA-1", []string{"12", "10", "11"}); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	id := pendingIDByType(t, repo, "test_step_order")
	if err := repo.ResolveConflictMerge("p1", "QA-1", "T1", []testrepo.ConflictDecision{{
		PendingID: id, EntityType: "test_step_order", EntityKey: "QA-1",
		Field: "order", Choice: "theirs", RemoteValue: `["11","10","12"]`,
	}}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	steps, _ := repo.ListTestSteps("p1", "QA-1")
	got := []string{steps[0].XrayID, steps[1].XrayID, steps[2].XrayID}
	if got[0] != "11" || got[1] != "10" || got[2] != "12" {
		t.Errorf("order = %v, want [11 10 12] (remote)", got)
	}
	if pc, _ := repo.ListPendingChanges("p1"); len(pc) != 0 {
		t.Errorf("want 0 pending after keep-theirs, got %d", len(pc))
	}
}

func TestResolveConflictMergeStepDeleteKeepTheirs(t *testing.T) {
	repo := seedSteppedTest(t)
	if err := repo.DeleteTestStep("p1", "QA-1", "11"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	id := pendingIDByType(t, repo, "test_step_delete")
	if err := repo.ResolveConflictMerge("p1", "QA-1", "T1", []testrepo.ConflictDecision{{
		PendingID: id, EntityType: "test_step_delete", EntityKey: "QA-1:11",
		Field: "step", Choice: "theirs",
		RemoteValue: `{"xrayId":"11","index":2,"action":"EDITED","data":"","expected":"","calledTestKey":""}`,
	}}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	steps, _ := repo.ListTestSteps("p1", "QA-1")
	var found *testrepo.Step
	for i := range steps {
		if steps[i].XrayID == "11" {
			found = &steps[i]
		}
	}
	if found == nil {
		t.Fatalf("step 11 should be restored (kept theirs), got %+v", steps)
	}
	if found.Action != "EDITED" {
		t.Errorf("restored step action = %q, want EDITED (remote)", found.Action)
	}
}

func TestResolveConflictMergeCustomFieldKeepTheirs(t *testing.T) {
	repo := newRepo(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{{Key: "QA-1", ID: "1", Updated: "T0"}}); err != nil {
		t.Fatalf("seed test: %v", err)
	}
	if err := repo.UpsertCustomFields("p1", []testrepo.CustomFieldDef{{FieldID: "cf1", Name: "Type", Type: "string"}}); err != nil {
		t.Fatalf("seed def: %v", err)
	}
	if err := repo.SetTestCustomFields("p1", "QA-1", map[string]string{"cf1": "Manual"}); err != nil {
		t.Fatalf("seed value: %v", err)
	}
	if err := repo.EditTestCustomField("p1", "QA-1", "cf1", "Automated"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	id := pendingIDByType(t, repo, "custom_field")
	if err := repo.ResolveConflictMerge("p1", "QA-1", "T1", []testrepo.ConflictDecision{{
		PendingID: id, EntityType: "custom_field", EntityKey: "QA-1:cf1",
		Field: "value", Choice: "theirs", RemoteValue: "Generic",
	}}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	vals, _ := repo.ListTestCustomFields("p1", "QA-1")
	var got string
	for _, v := range vals {
		if v.FieldID == "cf1" {
			got = v.Value
		}
	}
	if got != "Generic" {
		t.Errorf("custom field = %q, want Generic (kept theirs)", got)
	}
	if pc, _ := repo.ListPendingChanges("p1"); len(pc) != 0 {
		t.Errorf("want 0 pending after keep-theirs, got %d", len(pc))
	}
}

func TestRecreateDeletedTest(t *testing.T) {
	repo := newRepo(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "Login", Priority: "High", Updated: "T0"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := repo.SetTestSteps("p1", "QA-1", []testrepo.Step{{XrayID: "10", Index: 1, Action: "step1"}}); err != nil {
		t.Fatalf("seed steps: %v", err)
	}
	// A local edit the user doesn't want to lose when the remote was deleted.
	if err := repo.EditTestField("p1", "QA-1", "summary", "Login (edited locally)"); err != nil {
		t.Fatalf("edit: %v", err)
	}

	newKey, err := repo.RecreateDeletedTest("p1", "QA-1")
	if err != nil {
		t.Fatalf("recreate: %v", err)
	}
	if newKey == "" || newKey == "QA-1" {
		t.Fatalf("expected a new local key, got %q", newKey)
	}

	nt, _ := repo.GetTest("p1", newKey)
	if nt.Summary != "Login (edited locally)" {
		t.Errorf("new test summary = %q, want the locally-edited value", nt.Summary)
	}
	if nt.Priority != "High" {
		t.Errorf("new test priority = %q, want High (carried over)", nt.Priority)
	}
	nsteps, _ := repo.ListTestSteps("p1", newKey)
	if len(nsteps) != 1 || nsteps[0].Action != "step1" {
		t.Errorf("new test should carry the step, got %+v", nsteps)
	}

	// The deleted test's own edits are cleared (no pending change keyed QA-1).
	pc, _ := repo.ListPendingChanges("p1")
	for _, c := range pc {
		if c.EntityKey == "QA-1" {
			t.Errorf("old test's pending change should be discarded, found %+v", c)
		}
	}
}
