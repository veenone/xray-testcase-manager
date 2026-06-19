package testrepo_test

import (
	"testing"

	"xray-test-manager/internal/testrepo"
)

func seedReqRepo(t *testing.T) *testrepo.Repository {
	t.Helper()
	repo := newRepo(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "Login"},
		{Key: "QA-2", ID: "2", Summary: "Logout"},
		{Key: "QA-3", ID: "3", Summary: "Reset"},
	}); err != nil {
		t.Fatalf("seed tests: %v", err)
	}
	// PRD-10 is covered by QA-1 (PASS) and QA-2 (FAIL); PRD-11 by QA-3 (not run);
	// PRD-12 is uncovered.
	if err := repo.ReplaceAllRequirements("p1", []testrepo.Requirement{
		{Key: "PRD-10", ProjectKey: "PRD", IssueType: "Story", Summary: "Auth works"},
		{Key: "PRD-11", ProjectKey: "PRD", IssueType: "Story", Summary: "Session policy"},
		{Key: "PRD-12", ProjectKey: "PRD", IssueType: "Epic", Summary: "Untested"},
	}); err != nil {
		t.Fatalf("seed reqs: %v", err)
	}
	if err := repo.ReplaceAllRequirementLinks("p1", []testrepo.RequirementLink{
		{TestKey: "QA-1", RequirementKey: "PRD-10", LinkID: "100"},
		{TestKey: "QA-2", RequirementKey: "PRD-10", LinkID: "101"},
		{TestKey: "QA-3", RequirementKey: "PRD-11", LinkID: "102"},
	}); err != nil {
		t.Fatalf("seed links: %v", err)
	}
	if err := repo.UpsertContainers("p1", []testrepo.Container{
		{Key: "QA-TE-1", Kind: "testexec", Summary: "Cycle 1"},
	}); err != nil {
		t.Fatalf("seed exec: %v", err)
	}
	if err := repo.ReplaceAllContainerLinks("p1", []testrepo.ContainerLink{
		{ContainerKey: "QA-TE-1", TestKey: "QA-1", RunStatus: "PASS"},
		{ContainerKey: "QA-TE-1", TestKey: "QA-2", RunStatus: "FAIL"},
		{ContainerKey: "QA-TE-1", TestKey: "QA-3", RunStatus: "TODO"},
	}); err != nil {
		t.Fatalf("seed runs: %v", err)
	}
	return repo
}

func TestRequirementCoverageDerivation(t *testing.T) {
	repo := seedReqRepo(t)

	cov, err := repo.ListRequirementsWithCoverage("p1")
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}
	got := map[string]testrepo.RequirementCoverage{}
	for _, c := range cov {
		got[c.Key] = c
	}
	if got["PRD-10"].Coverage != "FAILED" || got["PRD-10"].TestCount != 2 {
		t.Errorf("PRD-10 = %+v, want FAILED / 2 tests", got["PRD-10"])
	}
	if got["PRD-11"].Coverage != "NOTRUN" {
		t.Errorf("PRD-11 coverage = %q, want NOTRUN", got["PRD-11"].Coverage)
	}
	if got["PRD-12"].Coverage != "UNCOVERED" {
		t.Errorf("PRD-12 coverage = %q, want UNCOVERED", got["PRD-12"].Coverage)
	}
}

func TestRequirementPassedWhenAllCoveringTestsPass(t *testing.T) {
	repo := newRepo(t)
	_ = repo.UpsertTests("p1", []testrepo.TestCase{{Key: "QA-1", ID: "1"}})
	_ = repo.ReplaceAllRequirements("p1", []testrepo.Requirement{{Key: "PRD-1", ProjectKey: "PRD"}})
	_ = repo.ReplaceAllRequirementLinks("p1", []testrepo.RequirementLink{{TestKey: "QA-1", RequirementKey: "PRD-1"}})
	_ = repo.UpsertContainers("p1", []testrepo.Container{{Key: "QA-TE-1", Kind: "testexec"}})
	_ = repo.ReplaceAllContainerLinks("p1", []testrepo.ContainerLink{{ContainerKey: "QA-TE-1", TestKey: "QA-1", RunStatus: "PASS"}})

	cov, _ := repo.ListRequirementsWithCoverage("p1")
	if len(cov) != 1 || cov[0].Coverage != "PASSED" {
		t.Errorf("coverage = %+v, want one PASSED requirement", cov)
	}
}

func TestListTestsForRequirementAndReverse(t *testing.T) {
	repo := seedReqRepo(t)

	tests, _ := repo.ListTestsForRequirement("p1", "PRD-10")
	if len(tests) != 2 {
		t.Fatalf("PRD-10 covering tests = %d, want 2", len(tests))
	}

	reqs, _ := repo.GetTestRequirements("p1", "QA-1")
	if len(reqs) != 1 || reqs[0].Key != "PRD-10" || reqs[0].ProjectKey != "PRD" {
		t.Errorf("QA-1 requirements = %+v, want [PRD-10 in PRD]", reqs)
	}
}

func TestSetTestRequirementsQueuesAndDiscardRestores(t *testing.T) {
	repo := seedReqRepo(t)
	// QA-1 currently covers PRD-10 (from the seed). Re-cover it with PRD-11 only.
	if err := repo.SetTestRequirements("p1", "QA-1", []string{"PRD-11"}); err != nil {
		t.Fatalf("set: %v", err)
	}

	reqs, _ := repo.GetTestRequirements("p1", "QA-1")
	if len(reqs) != 1 || reqs[0].Key != "PRD-11" {
		t.Fatalf("QA-1 requirements = %+v, want [PRD-11]", reqs)
	}
	changes, _ := repo.ListPendingChanges("p1")
	var rs int
	for _, c := range changes {
		if c.EntityType == "requirement_set" && c.EntityKey == "QA-1" {
			rs++
		}
	}
	if rs != 1 {
		t.Fatalf("want one requirement_set for QA-1, got %d (all: %+v)", rs, changes)
	}

	// Discard restores the original link (PRD-10, with its link id).
	var id int64
	for _, c := range changes {
		if c.EntityType == "requirement_set" {
			id = c.ID
		}
	}
	if err := repo.DiscardPendingChange("p1", id); err != nil {
		t.Fatalf("discard: %v", err)
	}
	reqs, _ = repo.GetTestRequirements("p1", "QA-1")
	if len(reqs) != 1 || reqs[0].Key != "PRD-10" {
		t.Errorf("QA-1 requirements after discard = %+v, want [PRD-10]", reqs)
	}
}

func TestBulkReplaceRequirements(t *testing.T) {
	repo := seedReqRepo(t)
	// Both tests start covering {PRD-10, PRD-11}.
	if err := repo.ReplaceAllRequirementLinks("p1", []testrepo.RequirementLink{
		{TestKey: "QA-1", RequirementKey: "PRD-10", LinkID: "100"},
		{TestKey: "QA-1", RequirementKey: "PRD-11", LinkID: "101"},
		{TestKey: "QA-2", RequirementKey: "PRD-10", LinkID: "102"},
		{TestKey: "QA-2", RequirementKey: "PRD-11", LinkID: "103"},
	}); err != nil {
		t.Fatalf("seed links: %v", err)
	}

	res, err := repo.BulkReplaceRequirements("p1", []string{"QA-1", "QA-2"}, []string{"PRD-10"}, []string{"PRD-12"})
	if err != nil {
		t.Fatalf("bulk replace: %v", err)
	}
	if len(res.Succeeded) != 2 || len(res.Failed) != 0 {
		t.Fatalf("result = %+v, want 2 succeeded / 0 failed", res)
	}

	for _, key := range []string{"QA-1", "QA-2"} {
		reqs, _ := repo.GetTestRequirements("p1", key)
		got := map[string]bool{}
		for _, rq := range reqs {
			got[rq.Key] = true
		}
		if len(got) != 2 || !got["PRD-11"] || !got["PRD-12"] {
			t.Errorf("%s requirements = %+v, want exactly {PRD-11, PRD-12}", key, reqs)
		}
	}
}

func TestSetTestRequirementsSameSetIsNoop(t *testing.T) {
	repo := seedReqRepo(t)
	if err := repo.SetTestRequirements("p1", "QA-1", []string{"PRD-10"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	changes, _ := repo.ListPendingChanges("p1")
	for _, c := range changes {
		if c.EntityType == "requirement_set" {
			t.Errorf("setting the same requirement set should not queue a change: %+v", c)
		}
	}
}

func TestRequirementSourceCRUD(t *testing.T) {
	repo := newRepo(t)

	if err := repo.SetRequirementSource("p1", "PRD", "Story Epic", "labels = audit"); err != nil {
		t.Fatalf("set source: %v", err)
	}
	if err := repo.SetRequirementSource("p1", "PRD", "Story", ""); err != nil { // upsert
		t.Fatalf("update source: %v", err)
	}
	srcs, _ := repo.ListRequirementSources("p1")
	if len(srcs) != 1 || srcs[0].IssueTypes != "Story" {
		t.Fatalf("sources = %+v, want one PRD/Story", srcs)
	}
	if err := repo.RemoveRequirementSource("p1", "PRD"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	srcs, _ = repo.ListRequirementSources("p1")
	if len(srcs) != 0 {
		t.Errorf("sources after remove = %+v, want none", srcs)
	}
}
