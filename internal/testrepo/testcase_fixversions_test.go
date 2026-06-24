package testrepo_test

import (
	"testing"

	"xray-test-manager/internal/testrepo"
)

// TestTestCaseFixVersionsRoundTrip seeds Tests with FixVersions via UpsertTests
// and asserts the values survive a GetTest and a ListTests round-trip, including
// a multi-version case and the zero-version case.
func TestTestCaseFixVersionsRoundTrip(t *testing.T) {
	repo := newRepo(t)

	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "Alpha", FixVersions: []string{"1.5.0", "1.6.0"}},
		{Key: "QA-2", ID: "2", Summary: "Beta", FixVersions: []string{"1.7.0"}},
		{Key: "QA-3", ID: "3", Summary: "Gamma"}, // no fix versions
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	qa1, err := repo.GetTest("p1", "QA-1")
	if err != nil {
		t.Fatalf("GetTest QA-1: %v", err)
	}
	if len(qa1.FixVersions) != 2 || qa1.FixVersions[0] != "1.5.0" || qa1.FixVersions[1] != "1.6.0" {
		t.Errorf("QA-1 FixVersions = %v, want [1.5.0 1.6.0]", qa1.FixVersions)
	}

	qa2, err := repo.GetTest("p1", "QA-2")
	if err != nil {
		t.Fatalf("GetTest QA-2: %v", err)
	}
	if len(qa2.FixVersions) != 1 || qa2.FixVersions[0] != "1.7.0" {
		t.Errorf("QA-2 FixVersions = %v, want [1.7.0]", qa2.FixVersions)
	}

	qa3, err := repo.GetTest("p1", "QA-3")
	if err != nil {
		t.Fatalf("GetTest QA-3: %v", err)
	}
	if len(qa3.FixVersions) != 0 {
		t.Errorf("QA-3 FixVersions should be empty, got %v", qa3.FixVersions)
	}

	// ListTests must also carry fix versions.
	page, err := repo.ListTests("p1", testrepo.Query{Limit: 10})
	if err != nil {
		t.Fatalf("ListTests: %v", err)
	}
	byKey := map[string]testrepo.TestCase{}
	for _, tc := range page.Tests {
		byKey[tc.Key] = tc
	}
	if fv := byKey["QA-1"].FixVersions; len(fv) != 2 || fv[0] != "1.5.0" {
		t.Errorf("ListTests QA-1 FixVersions = %v, want [1.5.0 1.6.0]", fv)
	}
}

// TestExecMemberRunFixVersions seeds a Test Execution with two member Tests
// that carry different FixVersions and asserts GetExecutionMembersWithRuns
// returns each member's OWN fix versions (from test_case), not the
// execution's.
func TestExecMemberRunFixVersions(t *testing.T) {
	repo := newRepo(t)

	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "Login", FixVersions: []string{"1.5.0"}},
		{Key: "QA-2", ID: "2", Summary: "Logout", FixVersions: []string{"1.6.0", "1.7.0"}},
		{Key: "QA-3", ID: "3", Summary: "Register"}, // no fix versions
	}); err != nil {
		t.Fatalf("seed tests: %v", err)
	}

	// Execution with its OWN fix versions (distinct from the member tests').
	if err := repo.UpsertContainers("p1", []testrepo.Container{
		{Key: "EXEC-1", Kind: "testexec", Summary: "Sprint 42", Status: "Open",
			FixVersions: []string{"2.0.0"}},
	}); err != nil {
		t.Fatalf("seed container: %v", err)
	}

	if err := repo.ReplaceAllContainerLinks("p1", []testrepo.ContainerLink{
		{ContainerKey: "EXEC-1", TestKey: "QA-1", RunStatus: "PASS"},
		{ContainerKey: "EXEC-1", TestKey: "QA-2", RunStatus: "FAIL"},
		{ContainerKey: "EXEC-1", TestKey: "QA-3", RunStatus: "TODO"},
	}); err != nil {
		t.Fatalf("seed links: %v", err)
	}

	members, err := repo.GetExecutionMembersWithRuns("p1", "EXEC-1")
	if err != nil {
		t.Fatalf("GetExecutionMembersWithRuns: %v", err)
	}
	if len(members) != 3 {
		t.Fatalf("expected 3 members, got %d", len(members))
	}

	byKey := map[string]testrepo.ExecMemberRun{}
	for _, m := range members {
		byKey[m.TestKey] = m
	}

	// QA-1 should carry its own [1.5.0], not the execution's [2.0.0].
	if fv := byKey["QA-1"].FixVersions; len(fv) != 1 || fv[0] != "1.5.0" {
		t.Errorf("QA-1 member FixVersions = %v, want [1.5.0]", fv)
	}

	// QA-2 should carry [1.6.0 1.7.0].
	if fv := byKey["QA-2"].FixVersions; len(fv) != 2 || fv[0] != "1.6.0" || fv[1] != "1.7.0" {
		t.Errorf("QA-2 member FixVersions = %v, want [1.6.0 1.7.0]", fv)
	}

	// QA-3 should carry empty (not the execution's [2.0.0]).
	if fv := byKey["QA-3"].FixVersions; len(fv) != 0 {
		t.Errorf("QA-3 member FixVersions should be empty, got %v", fv)
	}
}

// TestUpsertTestsFixVersionsOverwrittenOnResync verifies that fix_versions is
// a plain synced field: a resync (another UpsertTests call) overwrites it
// unconditionally when no pending change for that field exists.
func TestUpsertTestsFixVersionsOverwrittenOnResync(t *testing.T) {
	repo := newRepo(t)

	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "A", FixVersions: []string{"1.5.0"}},
	}); err != nil {
		t.Fatalf("initial upsert: %v", err)
	}

	// Resync with updated fix versions.
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "A", FixVersions: []string{"1.6.0", "1.7.0"}},
	}); err != nil {
		t.Fatalf("resync upsert: %v", err)
	}

	got, err := repo.GetTest("p1", "QA-1")
	if err != nil {
		t.Fatalf("GetTest after resync: %v", err)
	}
	if len(got.FixVersions) != 2 || got.FixVersions[0] != "1.6.0" || got.FixVersions[1] != "1.7.0" {
		t.Errorf("FixVersions after resync = %v, want [1.6.0 1.7.0]", got.FixVersions)
	}
}
