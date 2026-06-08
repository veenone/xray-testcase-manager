package testrepo_test

import (
	"testing"

	"xray-test-manager/internal/testrepo"
)

const compProfile = "p1"

func seedComponentTests(t *testing.T, repo *testrepo.Repository) {
	t.Helper()
	tests := []testrepo.TestCase{
		{Key: "QA-1", Summary: "a", Components: []string{"Frontend", "User Management"}},
		{Key: "QA-2", Summary: "b", Components: []string{"Backend"}},
		{Key: "QA-3", Summary: "c", Components: []string{"User Management"}},
		{Key: "QA-4", Summary: "d"}, // no components
	}
	if err := repo.UpsertTests(compProfile, tests); err != nil {
		t.Fatalf("upsert: %v", err)
	}
}

// TestComponentsRoundTrip verifies components survive an upsert and come back
// on read, including a multi-word name.
func TestComponentsRoundTrip(t *testing.T) {
	repo := newRepo(t)
	seedComponentTests(t, repo)

	got, err := repo.GetTest(compProfile, "QA-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Components) != 2 || got.Components[0] != "Frontend" || got.Components[1] != "User Management" {
		t.Fatalf("components not preserved: %+v", got.Components)
	}
}

// TestComponentFilterExactName ensures the component filter matches a whole
// name and never a partial — "User" must not match "User Management".
func TestComponentFilterExactName(t *testing.T) {
	repo := newRepo(t)
	seedComponentTests(t, repo)

	page, err := repo.ListTests(compProfile, testrepo.Query{Component: "User Management"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("want 2 tests in 'User Management', got %d", page.Total)
	}

	none, err := repo.ListTests(compProfile, testrepo.Query{Component: "User"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if none.Total != 0 {
		t.Fatalf("'User' should not match 'User Management'; got %d", none.Total)
	}
}

// TestListComponents returns distinct components with counts, sorted by name.
func TestListComponents(t *testing.T) {
	repo := newRepo(t)
	seedComponentTests(t, repo)

	comps, err := repo.ListComponents(compProfile)
	if err != nil {
		t.Fatalf("list components: %v", err)
	}
	if len(comps) != 3 {
		t.Fatalf("want 3 distinct components, got %d: %+v", len(comps), comps)
	}
	// Sorted: Backend, Frontend, User Management.
	if comps[0].Label != "Backend" || comps[1].Label != "Frontend" || comps[2].Label != "User Management" {
		t.Fatalf("unexpected order: %+v", comps)
	}
	if comps[2].Count != 2 {
		t.Fatalf("want User Management count 2, got %d", comps[2].Count)
	}
}

// TestListFoldersLocalCounts verifies the folder badge counts come from the
// local cache and roll up descendants — so a folder's count equals exactly what
// the grid shows when that folder is selected.
func TestListFoldersLocalCounts(t *testing.T) {
	repo := newRepo(t)
	if err := repo.UpsertFolders(compProfile, []testrepo.Folder{
		{ID: "/A", ParentID: "", Name: "A", TotalTestCount: 999}, // stored Xray count ignored
		{ID: "/A/Sub", ParentID: "/A", Name: "Sub"},
		{ID: "/B", ParentID: "", Name: "B"},
	}); err != nil {
		t.Fatalf("upsert folders: %v", err)
	}
	if err := repo.UpsertTests(compProfile, []testrepo.TestCase{
		{Key: "QA-1", Summary: "a", FolderID: "/A"},
		{Key: "QA-2", Summary: "b", FolderID: "/A/Sub"},
		{Key: "QA-3", Summary: "c", FolderID: "/A/Sub"},
		{Key: "QA-4", Summary: "d", FolderID: "/B"},
		{Key: "QA-5", Summary: "e"}, // orphan
	}); err != nil {
		t.Fatalf("upsert tests: %v", err)
	}

	folders, err := repo.ListFolders(compProfile)
	if err != nil {
		t.Fatalf("list folders: %v", err)
	}
	byID := map[string]testrepo.Folder{}
	for _, f := range folders {
		byID[f.ID] = f
	}
	if a := byID["/A"]; a.TestCount != 1 || a.TotalTestCount != 3 {
		t.Fatalf("/A want direct 1 total 3, got direct %d total %d", a.TestCount, a.TotalTestCount)
	}
	if s := byID["/A/Sub"]; s.TestCount != 2 || s.TotalTestCount != 2 {
		t.Fatalf("/A/Sub want 2/2, got %d/%d", s.TestCount, s.TotalTestCount)
	}
	if b := byID["/B"]; b.TotalTestCount != 1 {
		t.Fatalf("/B want total 1, got %d", b.TotalTestCount)
	}
}

// TestFolderXrayID resolves a folder path to its native Xray id for committing
// a move: root -> "-1", a synced folder -> its id, an unknown path -> "".
func TestFolderXrayID(t *testing.T) {
	repo := newRepo(t)
	if err := repo.UpsertFolders(compProfile, []testrepo.Folder{
		{ID: "/CPS", ParentID: "", Name: "CPS", XrayID: "93072"},
	}); err != nil {
		t.Fatalf("upsert folders: %v", err)
	}

	if id, err := repo.FolderXrayID(compProfile, ""); err != nil || id != "-1" {
		t.Fatalf("root should resolve to -1, got %q (err %v)", id, err)
	}
	if id, err := repo.FolderXrayID(compProfile, "/CPS"); err != nil || id != "93072" {
		t.Fatalf("/CPS should resolve to 93072, got %q (err %v)", id, err)
	}
	if id, err := repo.FolderXrayID(compProfile, "/Nope"); err != nil || id != "" {
		t.Fatalf("unknown path should resolve to empty, got %q (err %v)", id, err)
	}
}

// TestApplyTestFolders stamps folder_id from a membership map but leaves a Test
// with a pending local folder move untouched.
func TestApplyTestFolders(t *testing.T) {
	repo := newRepo(t)
	seedComponentTests(t, repo)

	// QA-2 has a pending local move that the sync must not clobber.
	if err := repo.MoveTestToFolder(compProfile, "QA-2", "/Local/Move"); err != nil {
		t.Fatalf("move: %v", err)
	}

	if err := repo.ApplyTestFolders(compProfile, map[string]string{
		"QA-1": "/Authentication/Login",
		"QA-2": "/Remote/Folder", // should be ignored — QA-2 has a pending move
	}); err != nil {
		t.Fatalf("apply folders: %v", err)
	}

	qa1, _ := repo.GetTest(compProfile, "QA-1")
	if qa1.FolderID != "/Authentication/Login" {
		t.Fatalf("QA-1 folder not applied: %q", qa1.FolderID)
	}
	qa2, _ := repo.GetTest(compProfile, "QA-2")
	if qa2.FolderID != "/Local/Move" {
		t.Fatalf("QA-2 pending move was clobbered: %q", qa2.FolderID)
	}
}
