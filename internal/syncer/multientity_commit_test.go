package syncer_test

import (
	"context"
	"path/filepath"
	"testing"

	"xray-test-manager/internal/jira"
	"xray-test-manager/internal/store"
	"xray-test-manager/internal/syncer"
	"xray-test-manager/internal/testrepo"
)

// TestCommitMultiEntityRoundTrip is a behavior-lock characterization test for
// the per-Test grouping in CommitChanges combined with the separate membership
// pass. The existing focused commit tests each isolate ONE entity kind (temp-key
// create choreography, conflict pre-check, container_env); none exercises a
// field edit + workflow transition + step add/edit/delete + precondition
// association + container-membership add together in a single CommitChanges call.
// That cross-entity grouping/ordering (field updates -> transition -> step CRUD
// per Test, plus the independent membership pass) is exactly what a backend
// interface extraction could break without any single-entity test noticing.
//
// It seeds narrowly (like TestCommitContainerEnvClearsInDemo) rather than doing
// a full sync, then commits against a demo client (all remote writes are no-ops)
// and asserts: the commit succeeds, the pending journal is cleared, the result
// reports both the Test and the Container, and the local store reflects the
// committed state.
func TestCommitMultiEntityRoundTrip(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	repo := testrepo.NewRepository(st)

	const (
		profileID  = "p1"
		projectKey = "DEMO"
		baseTS     = "2026-01-01T00:00:00.000+0000"
	)

	// Seed one Test (status Open so the demo Open->In Progress transition is
	// valid), one Precondition, one Container, and two existing steps with real
	// Xray ids (so an edit and a delete target stable ids).
	if err := repo.UpsertTests(profileID, []testrepo.TestCase{
		{Key: "DEMO-1", ID: "1", Summary: "Original", Status: "Open", Priority: "Medium", Updated: baseTS},
	}); err != nil {
		t.Fatalf("seed test: %v", err)
	}
	if err := repo.UpsertPreconditions(profileID, []testrepo.Precondition{
		{Key: "DEMO-P-1", Summary: "User account exists", Type: "Manual"},
	}); err != nil {
		t.Fatalf("seed precondition: %v", err)
	}
	if err := repo.UpsertContainers(profileID, []testrepo.Container{
		{Key: "DEMO-TS-1", Kind: jira.KindTestSet, Summary: "Authentication test set", Status: "Open"},
	}); err != nil {
		t.Fatalf("seed container: %v", err)
	}
	if err := repo.SetTestSteps(profileID, "DEMO-1", []testrepo.Step{
		{XrayID: "S-1", Index: 1, Action: "Open login page", Expected: "Form shown"},
		{XrayID: "S-2", Index: 2, Action: "Submit credentials", Expected: "Logged in"},
	}); err != nil {
		t.Fatalf("seed steps: %v", err)
	}

	// Queue a mix of local pending changes across several entity types.
	if err := repo.EditTestField(profileID, "DEMO-1", "priority", "High"); err != nil {
		t.Fatalf("edit field: %v", err)
	}
	if err := repo.TransitionTest(profileID, "DEMO-1", "In Progress"); err != nil {
		t.Fatalf("transition: %v", err)
	}
	if err := repo.SetTestPreconditions(profileID, "DEMO-1", []string{"DEMO-P-1"}); err != nil {
		t.Fatalf("associate precondition: %v", err)
	}
	if err := repo.EditTestStepField(profileID, "DEMO-1", "S-1", "expected", "Login form is shown"); err != nil {
		t.Fatalf("edit step: %v", err)
	}
	if err := repo.DeleteTestStep(profileID, "DEMO-1", "S-2"); err != nil {
		t.Fatalf("delete step: %v", err)
	}
	newStep, err := repo.AddTestStep(profileID, "DEMO-1", "Verify dashboard", "", "Dashboard visible")
	if err != nil {
		t.Fatalf("add step: %v", err)
	}
	if _, err := repo.AllocateTests(profileID, "DEMO-TS-1", []string{"DEMO-1"}); err != nil {
		t.Fatalf("allocate to container: %v", err)
	}

	// Sanity: there are pending changes queued before the commit.
	before, err := repo.ListPendingChanges(profileID)
	if err != nil {
		t.Fatalf("list pending before: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("expected pending changes queued before commit, got none")
	}

	eng := syncer.New(jira.NewClient("demo", "tok"), repo)
	result, err := eng.CommitChanges(context.Background(), profileID, projectKey)
	if err != nil {
		t.Fatalf("CommitChanges: %v", err)
	}
	if len(result.Failed) != 0 {
		t.Fatalf("unexpected commit failures: %+v", result.Failed)
	}
	if len(result.Conflicted) != 0 {
		t.Fatalf("unexpected conflicts: %+v", result.Conflicted)
	}

	// The per-Test pass reports the Test key; the membership pass reports the
	// Container key (commitMembershipAdd returns the container, not the test).
	if !containsStr(result.Succeeded, "DEMO-1") {
		t.Errorf("result.Succeeded = %v, want it to contain DEMO-1", result.Succeeded)
	}
	if !containsStr(result.Succeeded, "DEMO-TS-1") {
		t.Errorf("result.Succeeded = %v, want it to contain DEMO-TS-1 (membership pass)", result.Succeeded)
	}

	// All pending changes must be cleared after a clean commit.
	after, err := repo.ListPendingChanges(profileID)
	if err != nil {
		t.Fatalf("list pending after: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("pending changes not cleared after commit: %+v", after)
	}

	// Local store reflects the committed state.
	tc, err := repo.GetTest(profileID, "DEMO-1")
	if err != nil {
		t.Fatalf("GetTest after commit: %v", err)
	}
	if tc.Priority != "High" {
		t.Errorf("priority = %q, want High", tc.Priority)
	}
	if tc.Status != "In Progress" {
		t.Errorf("status = %q, want In Progress", tc.Status)
	}

	// Steps: S-2 deleted, S-1 edited, the new step present with its text.
	steps, err := repo.ListTestSteps(profileID, "DEMO-1")
	if err != nil {
		t.Fatalf("ListTestSteps after commit: %v", err)
	}
	byAction := map[string]testrepo.Step{}
	for _, s := range steps {
		byAction[s.Action] = s
		if s.XrayID == "S-2" {
			t.Errorf("deleted step S-2 still present: %+v", s)
		}
	}
	if s1, ok := byAction["Open login page"]; !ok {
		t.Errorf("edited step S-1 missing after commit (steps=%+v)", steps)
	} else if s1.Expected != "Login form is shown" {
		t.Errorf("S-1 expected = %q, want \"Login form is shown\"", s1.Expected)
	}
	if _, ok := byAction["Verify dashboard"]; !ok {
		t.Errorf("added step missing after commit (steps=%+v)", steps)
	}
	_ = newStep

	// Precondition association committed and reflected locally.
	pres, err := repo.ListTestPreconditions(profileID, "DEMO-1")
	if err != nil {
		t.Fatalf("ListTestPreconditions after commit: %v", err)
	}
	if len(pres) != 1 || pres[0].Key != "DEMO-P-1" {
		t.Errorf("preconditions = %+v, want exactly [DEMO-P-1]", pres)
	}

	// Container membership committed and reflected locally.
	memberships, err := repo.ListContainersForTest(profileID, "DEMO-1")
	if err != nil {
		t.Fatalf("ListContainersForTest after commit: %v", err)
	}
	found := false
	for _, m := range memberships {
		if m.Key == "DEMO-TS-1" {
			found = true
		}
	}
	if !found {
		t.Errorf("DEMO-1 not a member of DEMO-TS-1 after commit (memberships=%+v)", memberships)
	}
}

// containsStr reports whether s contains v.
func containsStr(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
