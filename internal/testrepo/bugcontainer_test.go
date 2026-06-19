package testrepo_test

import (
	"testing"

	"xray-test-manager/internal/testrepo"
)

// TestListBugsForContainerCrossProject verifies that a bug reached through a
// cross-project (external) member Test of a Test Execution is surfaced by
// ListBugsForContainer, even though that member has no test_case row (#219).
func TestListBugsForContainerCrossProject(t *testing.T) {
	repo := newRepo(t)
	const profileID = "p1"

	// A sub-task Test Execution in project A whose member Test lives in project B.
	if err := repo.UpsertContainers(profileID, []testrepo.Container{
		{Key: "QA-TE-XPROJ", Kind: "testexec", Summary: "Cross-project cycle", Status: "Open"},
	}); err != nil {
		t.Fatalf("seed container: %v", err)
	}
	if err := repo.ReplaceAllContainerLinks(profileID, []testrepo.ContainerLink{
		{ContainerKey: "QA-TE-XPROJ", TestKey: "XRAYINT-1", RunStatus: "FAIL"},
	}); err != nil {
		t.Fatalf("seed membership: %v", err)
	}
	// XRAYINT-1 is a foreign member cached only in external_test (no test_case).
	if err := repo.ReplaceExternalTests(profileID, []testrepo.ExternalTest{
		{Key: "XRAYINT-1", Summary: "Integration login", Status: "In Progress", ProjectKey: "XRAYINT"},
	}); err != nil {
		t.Fatalf("seed external: %v", err)
	}
	// A bug in project A linked to the foreign member.
	if err := repo.ReplaceAllBugs(profileID, []testrepo.Bug{
		{Key: "QA-900", ProjectKey: "QA", IssueType: "Bug", Summary: "login breaks", Status: "Open", Priority: "High"},
	}); err != nil {
		t.Fatalf("seed bug: %v", err)
	}
	if err := repo.ReplaceAllBugLinks(profileID, []testrepo.BugLink{
		{TestKey: "XRAYINT-1", BugKey: "QA-900", LinkID: "1"},
	}); err != nil {
		t.Fatalf("seed bug link: %v", err)
	}

	bugs, err := repo.ListBugsForContainer(profileID, "QA-TE-XPROJ")
	if err != nil {
		t.Fatalf("list bugs for container: %v", err)
	}
	if len(bugs) != 1 {
		t.Fatalf("bugs for container = %d, want 1 (the bug reached via the foreign member)", len(bugs))
	}
	if bugs[0].Key != "QA-900" || bugs[0].Summary != "login breaks" {
		t.Errorf("bug = %+v, want QA-900 'login breaks'", bugs[0])
	}
}

// TestListTestsForBugIncludesExternalMember verifies the per-bug test list shows
// a foreign member by its cached external summary instead of dropping it (#219).
func TestListTestsForBugIncludesExternalMember(t *testing.T) {
	repo := newRepo(t)
	const profileID = "p1"

	// One local test and one foreign member, both linked to the same bug.
	if err := repo.UpsertTests(profileID, []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "Login", Status: "Approved"},
	}); err != nil {
		t.Fatalf("seed test: %v", err)
	}
	if err := repo.ReplaceExternalTests(profileID, []testrepo.ExternalTest{
		{Key: "XRAYINT-1", Summary: "Integration login", Status: "In Progress", ProjectKey: "XRAYINT"},
	}); err != nil {
		t.Fatalf("seed external: %v", err)
	}
	if err := repo.ReplaceAllBugs(profileID, []testrepo.Bug{
		{Key: "QA-900", ProjectKey: "QA", IssueType: "Bug", Summary: "login breaks", Status: "Open"},
	}); err != nil {
		t.Fatalf("seed bug: %v", err)
	}
	if err := repo.ReplaceAllBugLinks(profileID, []testrepo.BugLink{
		{TestKey: "QA-1", BugKey: "QA-900", LinkID: "1"},
		{TestKey: "XRAYINT-1", BugKey: "QA-900", LinkID: "2"},
	}); err != nil {
		t.Fatalf("seed bug links: %v", err)
	}

	tests, err := repo.ListTestsForBug(profileID, "QA-900")
	if err != nil {
		t.Fatalf("list tests for bug: %v", err)
	}
	byKey := map[string]testrepo.BugTest{}
	for _, bt := range tests {
		byKey[bt.Key] = bt
	}
	if _, ok := byKey["QA-1"]; !ok {
		t.Errorf("local member QA-1 missing from %+v", tests)
	}
	ext, ok := byKey["XRAYINT-1"]
	if !ok {
		t.Fatalf("foreign member XRAYINT-1 dropped from per-bug test list %+v", tests)
	}
	if ext.Summary != "Integration login" {
		t.Errorf("XRAYINT-1 summary = %q, want cached external summary", ext.Summary)
	}
}
