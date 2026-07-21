package syncer_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"xray-test-manager/internal/backend"
	"xray-test-manager/internal/store"
	"xray-test-manager/internal/syncer"
	"xray-test-manager/internal/testrepo"
)

// folderFakeBackend is a minimal folder-capable backend.Backend used to exercise
// the commit engine's folder-create-before-move pass (RND_P_4TFINT_05-305). It
// advertises Xray-style capabilities (folders on, workflow transitions on) and
// implements only the methods the folder-create + folder-move path calls; every
// other method is left on the nil embedded interface so a mis-routed call panics
// and fails the test loudly.
//
//   - CreateFolder records the (parentPath, name) call in order and, on success,
//     registers a folder in the tree with a synthesized native xray id.
//   - FolderTree returns those registered folders WITH their xray ids and counts
//     how many times it was called (so the no-op invariant can be asserted).
//   - MoveTestToFolder records the folder id it was handed, which proves the move
//     resolved a real xray id rather than "" (the "no Xray id yet" failure).
type folderFakeBackend struct {
	backend.Backend
	caps backend.Capabilities

	mu                sync.Mutex
	createFolderCalls []folderCreateCall
	moveCalls         []folderMoveCall
	folderTreeCalls   int
	tree              []backend.Folder
	// failCreateName: CreateFolder returns an error for any folder whose name is
	// present here, so a failed create can be exercised without aborting the rest.
	failCreateName map[string]bool
}

type folderCreateCall struct {
	parentPath string
	name       string
}

type folderMoveCall struct {
	testKey  string
	folderID string
}

func newFolderFakeBackend() *folderFakeBackend {
	return &folderFakeBackend{
		caps: backend.Capabilities{
			Name:                        "xray",
			IDStyle:                     "jira-key",
			StepModel:                   "xray-steps",
			SupportsTestTypes:           true,
			SupportsFolders:             true,
			SupportsPreconditionObjects: true,
			SupportsRequirementObjects:  true,
			SupportsContainers:          true,
			SupportsTestRuns:            true,
			StatusModel:                 "workflow",
			SupportsWorkflowTransitions: true,
			SupportsBugCreation:         true,
			SupportsTags:                true,
		},
		failCreateName: map[string]bool{},
	}
}

func (b *folderFakeBackend) Capabilities() backend.Capabilities { return b.caps }

func (b *folderFakeBackend) RemoteVersion(ctx context.Context, entityType, externalKey string) (backend.VersionToken, error) {
	return "", nil // no remote movement -> conflict pre-check is skipped
}

func (b *folderFakeBackend) RemoteAhead(base, remote backend.VersionToken) bool { return false }

func (b *folderFakeBackend) FieldsForJira(updates map[string]string) map[string]any {
	out := make(map[string]any, len(updates))
	for f, v := range updates {
		out[f] = v
	}
	return out
}

func (b *folderFakeBackend) CreateFolder(ctx context.Context, projectKey, parentPath, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.createFolderCalls = append(b.createFolderCalls, folderCreateCall{parentPath: parentPath, name: name})
	if b.failCreateName[name] {
		return errors.New("xray rejected folder " + name)
	}
	path := parentPath + "/" + name
	b.tree = append(b.tree, backend.Folder{
		ID:       path,
		ParentID: parentPath,
		Name:     name,
		XrayID:   "xray-" + path, // the native id the move must resolve to
	})
	return nil
}

func (b *folderFakeBackend) FolderTree(ctx context.Context, projectKey string) (backend.FolderTreeResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.folderTreeCalls++
	folders := make([]backend.Folder, len(b.tree))
	copy(folders, b.tree)
	return backend.FolderTreeResult{Folders: folders}, nil
}

func (b *folderFakeBackend) MoveTestToFolder(ctx context.Context, projectKey, testKey, folderID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.moveCalls = append(b.moveCalls, folderMoveCall{testKey: testKey, folderID: folderID})
	return nil
}

// TestCommitCreatesFoldersParentFirstThenPlacesTest proves the single-commit
// create+place path: a fresh /A/B/C folder tree plus a test moved into /A/B/C
// commit together — the folders are CREATED parent-first, their xray ids are
// captured, and the move resolves the deepest folder's real id (not the "no Xray
// id yet" failure).
func TestCommitCreatesFoldersParentFirstThenPlacesTest(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	repo := testrepo.NewRepository(st)

	const profileID = "p1"
	if err := repo.UpsertTests(profileID, []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "Login test", Status: "TODO", Priority: "Medium", Updated: "2026-01-01T00:00:00Z"},
	}); err != nil {
		t.Fatalf("seed test: %v", err)
	}

	// Create a brand-new folder hierarchy locally (queues folder_create rows).
	if _, err := repo.CreateFolder(profileID, "", "A"); err != nil {
		t.Fatalf("create /A: %v", err)
	}
	if _, err := repo.CreateFolder(profileID, "/A", "B"); err != nil {
		t.Fatalf("create /A/B: %v", err)
	}
	if _, err := repo.CreateFolder(profileID, "/A/B", "C"); err != nil {
		t.Fatalf("create /A/B/C: %v", err)
	}
	// Move the test into the deepest (not-yet-created-in-Xray) folder.
	if err := repo.MoveTestToFolder(profileID, "QA-1", "/A/B/C"); err != nil {
		t.Fatalf("move to folder: %v", err)
	}

	fake := newFolderFakeBackend()
	eng := syncer.New(fake, repo)
	result, err := eng.CommitChanges(context.Background(), profileID, "QA")
	if err != nil {
		t.Fatalf("CommitChanges: %v", err)
	}
	if len(result.Failed) != 0 {
		t.Fatalf("unexpected failures: %+v", result.Failed)
	}

	// Folders were created PARENT-FIRST: /A, then /A/B, then /A/B/C.
	wantCreates := []folderCreateCall{
		{parentPath: "", name: "A"},
		{parentPath: "/A", name: "B"},
		{parentPath: "/A/B", name: "C"},
	}
	if len(fake.createFolderCalls) != len(wantCreates) {
		t.Fatalf("CreateFolder calls = %+v, want %+v", fake.createFolderCalls, wantCreates)
	}
	for i, c := range fake.createFolderCalls {
		if c != wantCreates[i] {
			t.Errorf("CreateFolder[%d] = %+v, want %+v (parent-first order)", i, c, wantCreates[i])
		}
	}

	// The ids were refreshed exactly once (single FolderTree call after creating).
	if fake.folderTreeCalls != 1 {
		t.Errorf("FolderTree calls = %d, want exactly 1 refresh after creating folders", fake.folderTreeCalls)
	}

	// The move SUCCEEDED and resolved the deepest folder's captured native id.
	if len(fake.moveCalls) != 1 {
		t.Fatalf("MoveTestToFolder calls = %+v, want exactly 1", fake.moveCalls)
	}
	if fake.moveCalls[0].testKey != "QA-1" || fake.moveCalls[0].folderID != "xray-/A/B/C" {
		t.Errorf("move = %+v, want {QA-1 xray-/A/B/C} (resolved id, not empty)", fake.moveCalls[0])
	}

	// The test and all three folders are reported succeeded.
	for _, want := range []string{"QA-1", "/A", "/A/B", "/A/B/C"} {
		if !containsStr(result.Succeeded, want) {
			t.Errorf("Succeeded = %v, want it to contain %q", result.Succeeded, want)
		}
	}

	// Every pending row is cleared.
	after, err := repo.ListPendingChanges(profileID)
	if err != nil {
		t.Fatalf("list pending after: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("pending not cleared after commit: %+v", after)
	}
}

// TestCommitFolderMoveIntoExistingFolderIsNoOpForCreates is the characterization
// guard: a folder-move into an ALREADY-EXISTING folder (no folder_create rows)
// still commits, and the new create-before-move pass is inert — it makes NO
// FolderTree call and does not disturb the move. This is the byte-for-behavior
// invariant for every non-folder-create commit.
func TestCommitFolderMoveIntoExistingFolderIsNoOpForCreates(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	repo := testrepo.NewRepository(st)

	const profileID = "p1"
	if err := repo.UpsertTests(profileID, []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "Login test", Status: "TODO", Priority: "Medium", Updated: "2026-01-01T00:00:00Z"},
	}); err != nil {
		t.Fatalf("seed test: %v", err)
	}
	// The target folder already exists in Xray (has a native xray id already).
	if err := repo.UpsertFolders(profileID, []testrepo.Folder{
		{ID: "/Existing", ParentID: "", Name: "Existing", XrayID: "xray-existing"},
	}); err != nil {
		t.Fatalf("seed folder: %v", err)
	}
	if err := repo.MoveTestToFolder(profileID, "QA-1", "/Existing"); err != nil {
		t.Fatalf("move to folder: %v", err)
	}

	fake := newFolderFakeBackend()
	eng := syncer.New(fake, repo)
	result, err := eng.CommitChanges(context.Background(), profileID, "QA")
	if err != nil {
		t.Fatalf("CommitChanges: %v", err)
	}
	if len(result.Failed) != 0 {
		t.Fatalf("unexpected failures: %+v", result.Failed)
	}

	// No folder_create rows -> the new pass must not touch the backend at all.
	if fake.folderTreeCalls != 0 {
		t.Errorf("FolderTree calls = %d, want 0 (no-op when there are no folder creates)", fake.folderTreeCalls)
	}
	if len(fake.createFolderCalls) != 0 {
		t.Errorf("CreateFolder calls = %+v, want none", fake.createFolderCalls)
	}

	// The move still resolved the existing folder's id and succeeded.
	if len(fake.moveCalls) != 1 || fake.moveCalls[0].folderID != "xray-existing" {
		t.Fatalf("move = %+v, want exactly 1 to xray-existing", fake.moveCalls)
	}
	if !containsStr(result.Succeeded, "QA-1") {
		t.Errorf("Succeeded = %v, want it to contain QA-1", result.Succeeded)
	}
}

// TestCommitFolderCreateFailureDoesNotAbortRest proves a failed folder create is
// reported (result.Failed) without aborting the commit: a sibling folder that
// creates cleanly still succeeds, and the single post-create refresh still runs.
func TestCommitFolderCreateFailureDoesNotAbortRest(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	repo := testrepo.NewRepository(st)

	const profileID = "p1"
	if _, err := repo.CreateFolder(profileID, "", "Good"); err != nil {
		t.Fatalf("create /Good: %v", err)
	}
	if _, err := repo.CreateFolder(profileID, "", "Bad"); err != nil {
		t.Fatalf("create /Bad: %v", err)
	}

	fake := newFolderFakeBackend()
	fake.failCreateName["Bad"] = true // this create fails at the backend
	eng := syncer.New(fake, repo)
	result, err := eng.CommitChanges(context.Background(), profileID, "QA")
	if err != nil {
		t.Fatalf("CommitChanges: %v", err)
	}

	// Both creates were attempted (the failure did not abort the loop).
	if len(fake.createFolderCalls) != 2 {
		t.Errorf("CreateFolder calls = %+v, want both attempted", fake.createFolderCalls)
	}
	// The good folder succeeded; the bad one is reported failed.
	if !containsStr(result.Succeeded, "/Good") {
		t.Errorf("Succeeded = %v, want it to contain /Good", result.Succeeded)
	}
	foundFail := false
	for _, f := range result.Failed {
		if f.TestKey == "/Bad" {
			foundFail = true
		}
	}
	if !foundFail {
		t.Errorf("Failed = %+v, want it to report /Bad", result.Failed)
	}

	// One create succeeded, so the refresh ran exactly once.
	if fake.folderTreeCalls != 1 {
		t.Errorf("FolderTree calls = %d, want exactly 1", fake.folderTreeCalls)
	}

	// The failed create stays pending for retry; the good one is cleared.
	after, err := repo.ListPendingChanges(profileID)
	if err != nil {
		t.Fatalf("list pending after: %v", err)
	}
	if len(after) != 1 || after[0].EntityKey != "/Bad" {
		t.Errorf("pending after = %+v, want exactly the /Bad create left pending", after)
	}
}
