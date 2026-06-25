package testrepo_test

import (
	"testing"

	"xray-test-manager/internal/testrepo"
)

func TestListTestsForBugHasProject(t *testing.T) {
	repo := newRepo(t)
	// Seed a local test (project derived from key prefix before last dash).
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-10", ID: "10", Summary: "Local test"},
	}); err != nil {
		t.Fatalf("seed tests: %v", err)
	}
	// Seed a cross-project external test (project comes from external_test.project_key).
	if err := repo.ReplaceExternalTests("p1", []testrepo.ExternalTest{
		{Key: "XRAY-20", Summary: "External test", Status: "Open", ProjectKey: "XRAY"},
	}); err != nil {
		t.Fatalf("seed external test: %v", err)
	}
	if err := repo.ReplaceAllBugs("p1", []testrepo.Bug{
		{Key: "BUGS-1", ProjectKey: "BUGS", IssueType: "Bug", Summary: "crash", Status: "Open"},
	}); err != nil {
		t.Fatalf("seed bug: %v", err)
	}
	if err := repo.ReplaceAllBugLinks("p1", []testrepo.BugLink{
		{TestKey: "QA-10", BugKey: "BUGS-1", LinkID: "1"},
		{TestKey: "XRAY-20", BugKey: "BUGS-1", LinkID: "2"},
	}); err != nil {
		t.Fatalf("seed links: %v", err)
	}

	tests, err := repo.ListTestsForBug("p1", "BUGS-1")
	if err != nil || len(tests) == 0 {
		t.Fatalf("need affected tests, err=%v n=%d", err, len(tests))
	}

	byKey := map[string]testrepo.BugTest{}
	for _, bt := range tests {
		byKey[bt.Key] = bt
	}

	// Local test: project is the key prefix before the last dash.
	if byKey["QA-10"].Project != "QA" {
		t.Errorf("local test QA-10 Project = %q, want QA", byKey["QA-10"].Project)
	}
	// Cross-project test: project comes from external_test.project_key.
	if byKey["XRAY-20"].Project != "XRAY" {
		t.Errorf("cross-project test XRAY-20 Project = %q, want XRAY", byKey["XRAY-20"].Project)
	}
}

func TestListTestsForBugReturnsAffectedTestsWithRunStatus(t *testing.T) {
	repo := newRepo(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "Login"},
		{Key: "QA-2", ID: "2", Summary: "Logout"},
	}); err != nil {
		t.Fatalf("seed tests: %v", err)
	}
	if err := repo.ReplaceAllBugs("p1", []testrepo.Bug{
		{Key: "BUGS-1", ProjectKey: "BUGS", IssueType: "Bug", Summary: "crash", Status: "Open"},
	}); err != nil {
		t.Fatalf("seed bug: %v", err)
	}
	if err := repo.ReplaceAllBugLinks("p1", []testrepo.BugLink{
		{TestKey: "QA-1", BugKey: "BUGS-1", LinkID: "1"},
		{TestKey: "QA-2", BugKey: "BUGS-1", LinkID: "2"},
	}); err != nil {
		t.Fatalf("seed links: %v", err)
	}
	if err := repo.UpsertContainers("p1", []testrepo.Container{
		{Key: "QA-TE-1", Kind: "testexec", Summary: "Cycle 1"},
	}); err != nil {
		t.Fatalf("seed exec: %v", err)
	}
	if err := repo.ReplaceAllContainerLinks("p1", []testrepo.ContainerLink{
		{ContainerKey: "QA-TE-1", TestKey: "QA-1", RunStatus: "FAIL"},
	}); err != nil {
		t.Fatalf("seed runs: %v", err)
	}

	tests, err := repo.ListTestsForBug("p1", "BUGS-1")
	if err != nil {
		t.Fatalf("list tests for bug: %v", err)
	}
	if len(tests) != 2 {
		t.Fatalf("affected tests = %d, want 2", len(tests))
	}
	byKey := map[string]testrepo.BugTest{}
	for _, bt := range tests {
		byKey[bt.Key] = bt
	}
	if byKey["QA-1"].RunStatus != "FAIL" {
		t.Errorf("QA-1 run status = %q, want FAIL", byKey["QA-1"].RunStatus)
	}
	if byKey["QA-1"].Summary != "Login" {
		t.Errorf("QA-1 summary = %q, want Login", byKey["QA-1"].Summary)
	}
	if byKey["QA-2"].RunStatus != "" {
		t.Errorf("QA-2 run status = %q, want empty (not run)", byKey["QA-2"].RunStatus)
	}
}

// TestListTestsForBugLatestRunFromTestRun verifies that ListTestsForBug sets
// RunStatus from the most recent run in test_run (not worst-wins from
// test_container_test). Seeding an older FAIL run and a newer PASS run for the
// same test, the method must return PASS.
func TestListTestsForBugLatestRunFromTestRun(t *testing.T) {
	repo := newRepo(t)

	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "Login"},
	}); err != nil {
		t.Fatalf("seed test: %v", err)
	}
	if err := repo.ReplaceAllBugs("p1", []testrepo.Bug{
		{Key: "BUGS-1", ProjectKey: "BUGS", IssueType: "Bug", Summary: "crash", Status: "Open"},
	}); err != nil {
		t.Fatalf("seed bug: %v", err)
	}
	if err := repo.ReplaceAllBugLinks("p1", []testrepo.BugLink{
		{TestKey: "QA-1", BugKey: "BUGS-1", LinkID: "1"},
	}); err != nil {
		t.Fatalf("seed link: %v", err)
	}

	// Seed two executions.
	if err := repo.UpsertContainers("p1", []testrepo.Container{
		{Key: "QA-TE-1", Kind: "testexec", Summary: "Cycle 1"},
		{Key: "QA-TE-2", Kind: "testexec", Summary: "Cycle 2"},
	}); err != nil {
		t.Fatalf("seed execs: %v", err)
	}

	// Seed test_run rows: older FAIL (TE-1), newer PASS (TE-2).
	// finished_at drives the sort; TE-2 is more recent.
	if err := repo.ReplaceRunsForExec("p1", "QA-TE-1", []testrepo.TestRunRow{
		{
			ExecKey: "QA-TE-1", TestKey: "QA-1", RunStatus: "FAIL",
			FinishedAt: "2024-01-01T10:00:00Z", UpdatedAt: "2024-01-01T10:00:00Z",
		},
	}); err != nil {
		t.Fatalf("seed runs TE-1: %v", err)
	}
	if err := repo.ReplaceRunsForExec("p1", "QA-TE-2", []testrepo.TestRunRow{
		{
			ExecKey: "QA-TE-2", TestKey: "QA-1", RunStatus: "PASS",
			FinishedAt: "2024-02-01T10:00:00Z", UpdatedAt: "2024-02-01T10:00:00Z",
		},
	}); err != nil {
		t.Fatalf("seed runs TE-2: %v", err)
	}

	tests, err := repo.ListTestsForBug("p1", "BUGS-1")
	if err != nil {
		t.Fatalf("ListTestsForBug: %v", err)
	}
	if len(tests) != 1 {
		t.Fatalf("expected 1 affected test, got %d", len(tests))
	}
	// Most recent run is PASS (TE-2, finished 2024-02-01); must NOT return
	// worst-wins FAIL.
	if tests[0].RunStatus != "PASS" {
		t.Errorf("RunStatus = %q, want PASS (most recent run)", tests[0].RunStatus)
	}
}

func TestCreateBugForTestQueuesAndDiscardRestores(t *testing.T) {
	repo := newRepo(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{{Key: "QA-1", ID: "1", Summary: "Login"}}); err != nil {
		t.Fatalf("seed test: %v", err)
	}

	key, err := repo.CreateBugForTest("p1", "QA-1", "QA-TE-1", testrepo.BugDraft{
		ProjectKey: "QA", Summary: "Login crashes", Description: "...", Priority: "High",
		Labels: []string{"regression"},
	})
	if err != nil {
		t.Fatalf("create bug: %v", err)
	}
	if len(key) < 8 || key[:8] != "NEW-BUG-" {
		t.Fatalf("temp key = %q, want NEW-BUG-*", key)
	}

	bugs, _ := repo.GetTestBugs("p1", "QA-1")
	if len(bugs) != 1 || bugs[0].Key != key || bugs[0].Summary != "Login crashes" {
		t.Fatalf("GetTestBugs = %+v, want one new bug", bugs)
	}
	changes, _ := repo.ListPendingChanges("p1")
	var id int64
	var n int
	for _, c := range changes {
		if c.EntityType == "bug_create" && c.EntityKey == key {
			n++
			id = c.ID
		}
	}
	if n != 1 {
		t.Fatalf("bug_create rows = %d, want 1", n)
	}

	if err := repo.DiscardPendingChange("p1", id); err != nil {
		t.Fatalf("discard: %v", err)
	}
	if bugs, _ := repo.GetTestBugs("p1", "QA-1"); len(bugs) != 0 {
		t.Errorf("after discard GetTestBugs = %+v, want none", bugs)
	}
	if bugsWithTests, _ := repo.ListBugsWithTests("p1"); len(bugsWithTests) != 0 {
		t.Errorf("after discard ListBugsWithTests = %+v, want none (placeholder bug row not removed)", bugsWithTests)
	}
}

func TestRenameBugRepointsCacheAndLinks(t *testing.T) {
	repo := newRepo(t)
	_ = repo.UpsertTests("p1", []testrepo.TestCase{{Key: "QA-1", ID: "1"}})
	key, _ := repo.CreateBugForTest("p1", "QA-1", "QA-TE-1", testrepo.BugDraft{ProjectKey: "QA", Summary: "x"})

	if err := repo.RenameBug("p1", key, "QA-500"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	bugs, _ := repo.GetTestBugs("p1", "QA-1")
	if len(bugs) != 1 || bugs[0].Key != "QA-500" {
		t.Errorf("after rename = %+v, want QA-500", bugs)
	}
}

func TestListBugsWithTestsExposesIssueTypeAndUpdated(t *testing.T) {
	repo := newRepo(t)
	if err := repo.ReplaceAllBugs("p1", []testrepo.Bug{
		{Key: "BUGS-1", ProjectKey: "BUGS", IssueType: "Defect", Summary: "crash", Status: "Open", Priority: "High", Updated: "2024-01-15T10:00:00.000+0000"},
	}); err != nil {
		t.Fatalf("seed bug: %v", err)
	}
	if err := repo.UpsertTests("p1", []testrepo.TestCase{{Key: "QA-1", ID: "1"}}); err != nil {
		t.Fatalf("seed test: %v", err)
	}
	if err := repo.ReplaceAllBugLinks("p1", []testrepo.BugLink{
		{TestKey: "QA-1", BugKey: "BUGS-1", LinkID: "1"},
	}); err != nil {
		t.Fatalf("seed link: %v", err)
	}

	bugs, err := repo.ListBugsWithTests("p1")
	if err != nil {
		t.Fatalf("ListBugsWithTests: %v", err)
	}
	if len(bugs) != 1 {
		t.Fatalf("len = %d, want 1", len(bugs))
	}
	b := bugs[0]
	if b.IssueType != "Defect" {
		t.Errorf("IssueType = %q, want Defect", b.IssueType)
	}
	if b.Updated != "2024-01-15T10:00:00.000+0000" {
		t.Errorf("Updated = %q, want 2024-01-15T10:00:00.000+0000", b.Updated)
	}
	if len(b.TestKeys) != 1 || b.TestKeys[0] != "QA-1" {
		t.Errorf("TestKeys = %v, want [QA-1]", b.TestKeys)
	}
}
