package coveragepublish

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"xray-test-manager/internal/coverage"
	"xray-test-manager/internal/store"
	"xray-test-manager/internal/testrepo"
)

// --- fake backend ------------------------------------------------------

type addCall struct {
	containerKey string
	testKeys     []string
}

type fakeBackend struct {
	createCalls int
	nextID      int

	addCalls    []addCall
	removeCalls []addCall

	createErr      error
	addErr         error
	removeErr      error
	updateIssueErr error

	failCreateOnCall int // 1-indexed; 0 means never fail

	// createEmptyKey makes CreateContainer return ("", nil), mirroring what
	// internal/jira.Client.CreateContainer returns for a demo profile URL:
	// no error, but also no usable key.
	createEmptyKey bool

	lastDescription string
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{}
}

func (f *fakeBackend) CreateContainer(ctx context.Context, projectKey, kind, summary string) (string, error) {
	f.createCalls++
	if f.failCreateOnCall != 0 && f.createCalls == f.failCreateOnCall {
		return "", errors.New("create failed (injected)")
	}
	if f.createErr != nil {
		return "", f.createErr
	}
	if f.createEmptyKey {
		return "", nil
	}
	f.nextID++
	return fmt.Sprintf("%s-TS%d", projectKey, f.nextID), nil
}

func (f *fakeBackend) AddTestsToContainer(ctx context.Context, kind, containerKey string, testKeys []string) error {
	if f.addErr != nil {
		return f.addErr
	}
	f.addCalls = append(f.addCalls, addCall{containerKey, append([]string(nil), testKeys...)})
	return nil
}

func (f *fakeBackend) RemoveTestsFromContainer(ctx context.Context, kind, containerKey string, testKeys []string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removeCalls = append(f.removeCalls, addCall{containerKey, append([]string(nil), testKeys...)})
	return nil
}

func (f *fakeBackend) UpdateIssue(ctx context.Context, key string, fields map[string]any) error {
	if f.updateIssueErr != nil {
		return f.updateIssueErr
	}
	if d, ok := fields["description"].(string); ok {
		f.lastDescription = d
	}
	return nil
}

// --- test fixtures -------------------------------------------------------

// newTestPublisher opens a fresh on-disk store, wires a coverage.Module over
// it, and returns a Publisher wired to the given (fake) backend, plus the
// store and module for seeding fixtures.
func newTestPublisher(t *testing.T, be Backend) (*Publisher, *store.Store, *coverage.Module) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "pub.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	cov := coverage.New(st, testrepo.NewRepository(st))
	return NewPublisher(st, cov, be), st, cov
}

func seedTestCase(t *testing.T, st *store.Store, profileID, testKey string) {
	t.Helper()
	if _, err := st.DB().Exec(
		`INSERT INTO test_case (profile_id, jira_key, jira_id, summary) VALUES (?, ?, '1', ?)`,
		profileID, testKey, "Test "+testKey); err != nil {
		t.Fatalf("seed test_case %s: %v", testKey, err)
	}
}

// mirrorMembership seeds test_container_test rows for containerKey, as if a
// pull-sync had just run and pulled back the membership a prior publish call
// wrote to the (fake) backend. The real engine only ever reads this table for
// "current membership" (never a live backend call, per the coverage-publish
// task brief), so tests that want a second PublishGroups call to see a
// non-empty starting membership must seed it explicitly, the same way a real
// sync would populate it between two user-triggered publishes.
func mirrorMembership(t *testing.T, st *store.Store, profileID, containerKey string, testKeys []string) {
	t.Helper()
	if _, err := st.DB().Exec(`DELETE FROM test_container_test WHERE profile_id = ? AND container_key = ?`, profileID, containerKey); err != nil {
		t.Fatalf("clear test_container_test: %v", err)
	}
	for _, k := range testKeys {
		if _, err := st.DB().Exec(
			`INSERT INTO test_container_test (profile_id, container_key, test_key) VALUES (?, ?, ?)`,
			profileID, containerKey, k); err != nil {
			t.Fatalf("seed test_container_test: %v", err)
		}
	}
}

// buildGroup creates a canonical requirement with one version and one group
// holding a single parameter with one value per label. Returns the version
// id, the group id, and the value ids in the same order as labels.
func buildGroup(t *testing.T, cov *coverage.Module, profileID, canonicalName, versionName, groupName string, sortOrder int, labels []string) (versionID, groupID string, valueIDs []string) {
	t.Helper()
	cid, err := cov.CreateCanonical(profileID, canonicalName, "cat", "")
	if err != nil {
		t.Fatalf("create canonical: %v", err)
	}
	versionID, err = cov.CreateVersion(profileID, cid, versionName, "stable", "")
	if err != nil {
		t.Fatalf("create version: %v", err)
	}
	groupID, err = cov.UpsertNode(profileID, coverage.NodeEdit{Kind: "group", VersionID: versionID, Name: groupName, SortOrder: sortOrder})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	pid, err := cov.UpsertNode(profileID, coverage.NodeEdit{Kind: "parameter", GroupID: groupID, Name: "pParam"})
	if err != nil {
		t.Fatalf("create parameter: %v", err)
	}
	for i, label := range labels {
		vid, err := cov.UpsertNode(profileID, coverage.NodeEdit{Kind: "value", ParameterID: pid, Name: label, IsRequired: true, SortOrder: i})
		if err != nil {
			t.Fatalf("create value %q: %v", label, err)
		}
		valueIDs = append(valueIDs, vid)
	}
	return versionID, groupID, valueIDs
}

// addGroupToVersion adds a second group (same shape as buildGroup) under an
// already-created version, for multi-group tests.
func addGroupToVersion(t *testing.T, cov *coverage.Module, profileID, versionID, groupName string, sortOrder int, labels []string) (groupID string, valueIDs []string) {
	t.Helper()
	groupID, err := cov.UpsertNode(profileID, coverage.NodeEdit{Kind: "group", VersionID: versionID, Name: groupName, SortOrder: sortOrder})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	pid, err := cov.UpsertNode(profileID, coverage.NodeEdit{Kind: "parameter", GroupID: groupID, Name: "pParam"})
	if err != nil {
		t.Fatalf("create parameter: %v", err)
	}
	for i, label := range labels {
		vid, err := cov.UpsertNode(profileID, coverage.NodeEdit{Kind: "value", ParameterID: pid, Name: label, IsRequired: true, SortOrder: i})
		if err != nil {
			t.Fatalf("create value %q: %v", label, err)
		}
		valueIDs = append(valueIDs, vid)
	}
	return groupID, valueIDs
}

func sorted(ss []string) []string {
	out := append([]string(nil), ss...)
	sort.Strings(out)
	return out
}

// --- tests -----------------------------------------------------------------

func TestPublishGroups_CreatesOneTestSetPerGroupWithMembers(t *testing.T) {
	const p = "profile-1"
	be := newFakeBackend()
	pub, st, cov := newTestPublisher(t, be)

	versionID, groupID, valueIDs := buildGroup(t, cov, p, "C_Sign", "1.0", "Mechanism", 0, []string{"RSA_PKCS", "ED25519"})
	seedTestCase(t, st, p, "QA-1")
	seedTestCase(t, st, p, "QA-2")
	if err := cov.SetValueTests(p, valueIDs[0], []string{"QA-1"}); err != nil {
		t.Fatal(err)
	}
	if err := cov.SetValueTests(p, valueIDs[1], []string{"QA-2"}); err != nil {
		t.Fatal(err)
	}

	result, err := pub.PublishGroups(context.Background(), p, "PROJ", versionID)
	if err != nil {
		t.Fatalf("PublishGroups: %v", err)
	}
	if result.Created != 1 || result.Failed != 0 {
		t.Fatalf("result = %+v, want 1 created, 0 failed", result)
	}
	if be.createCalls != 1 {
		t.Fatalf("createCalls = %d, want 1", be.createCalls)
	}
	gr := result.Groups[0]
	if gr.GroupID != groupID || gr.ContainerKey == "" {
		t.Fatalf("group result = %+v", gr)
	}
	if got := sorted(gr.Added); len(got) != 2 || got[0] != "QA-1" || got[1] != "QA-2" {
		t.Fatalf("added = %v, want [QA-1 QA-2]", got)
	}
	if len(be.addCalls) != 1 || be.addCalls[0].containerKey != gr.ContainerKey {
		t.Fatalf("addCalls = %+v", be.addCalls)
	}
}

// TestPublishGroups_RunningTwiceCreatesNothingNew proves publish is
// genuinely idempotent on its own: running PublishGroups twice back to back,
// with no pull-sync (and so no mirrorMembership call) in between, must not
// re-issue the add it already made. The engine has to keep its own
// test_container_test mirror truthful after a successful backend write for
// this to hold -- see TestPublishGroups_RunningAfterPullSyncStillNoOpsAdd
// for the case where a real sync repopulates the mirror instead.
func TestPublishGroups_RunningTwiceCreatesNothingNew(t *testing.T) {
	const p = "profile-1"
	be := newFakeBackend()
	pub, st, cov := newTestPublisher(t, be)

	versionID, _, valueIDs := buildGroup(t, cov, p, "C_Sign", "1.0", "Mechanism", 0, []string{"RSA_PKCS"})
	seedTestCase(t, st, p, "QA-1")
	if err := cov.SetValueTests(p, valueIDs[0], []string{"QA-1"}); err != nil {
		t.Fatal(err)
	}

	first, err := pub.PublishGroups(context.Background(), p, "PROJ", versionID)
	if err != nil {
		t.Fatalf("first PublishGroups: %v", err)
	}
	if first.Created != 1 {
		t.Fatalf("first run created = %d, want 1", first.Created)
	}
	containerKey := first.Groups[0].ContainerKey
	if len(be.addCalls) != 1 {
		t.Fatalf("addCalls after first run = %+v, want exactly 1", be.addCalls)
	}

	second, err := pub.PublishGroups(context.Background(), p, "PROJ", versionID)
	if err != nil {
		t.Fatalf("second PublishGroups: %v", err)
	}
	if be.createCalls != 1 {
		t.Fatalf("createCalls after second run = %d, want 1 (no duplicate Test Set)", be.createCalls)
	}
	if len(be.addCalls) != 1 || len(be.removeCalls) != 0 {
		t.Fatalf("addCalls/removeCalls after second run = %+v / %+v, want no new calls", be.addCalls, be.removeCalls)
	}
	if second.Created != 0 || second.Updated != 1 {
		t.Fatalf("second result = %+v, want 0 created, 1 updated", second)
	}
	if len(second.Groups[0].Added) != 0 || len(second.Groups[0].Removed) != 0 {
		t.Fatalf("second run diff = added %v removed %v, want none", second.Groups[0].Added, second.Groups[0].Removed)
	}
	if second.Groups[0].ContainerKey != containerKey {
		t.Fatalf("second run container = %s, want reused %s", second.Groups[0].ContainerKey, containerKey)
	}
}

// TestPublishGroups_RunningAfterPullSyncStillNoOpsAdd covers the other path
// to a no-op second run: a real pull-sync repopulates test_container_test
// from Xray between the two publishes (mirrorMembership fakes that here).
// This must stay a no-op too -- it's the case
// TestPublishGroups_RunningTwiceCreatesNothingNew used to test by accident
// before the engine mirrored its own writes.
func TestPublishGroups_RunningAfterPullSyncStillNoOpsAdd(t *testing.T) {
	const p = "profile-1"
	be := newFakeBackend()
	pub, st, cov := newTestPublisher(t, be)

	versionID, _, valueIDs := buildGroup(t, cov, p, "C_Sign", "1.0", "Mechanism", 0, []string{"RSA_PKCS"})
	seedTestCase(t, st, p, "QA-1")
	if err := cov.SetValueTests(p, valueIDs[0], []string{"QA-1"}); err != nil {
		t.Fatal(err)
	}

	first, err := pub.PublishGroups(context.Background(), p, "PROJ", versionID)
	if err != nil {
		t.Fatalf("first PublishGroups: %v", err)
	}
	containerKey := first.Groups[0].ContainerKey

	// Simulate the next pull-sync mirroring what was just published.
	mirrorMembership(t, st, p, containerKey, []string{"QA-1"})

	second, err := pub.PublishGroups(context.Background(), p, "PROJ", versionID)
	if err != nil {
		t.Fatalf("second PublishGroups: %v", err)
	}
	if be.createCalls != 1 {
		t.Fatalf("createCalls after second run = %d, want 1 (no duplicate Test Set)", be.createCalls)
	}
	if second.Created != 0 || second.Updated != 1 {
		t.Fatalf("second result = %+v, want 0 created, 1 updated", second)
	}
	if len(second.Groups[0].Added) != 0 || len(second.Groups[0].Removed) != 0 {
		t.Fatalf("second run diff = added %v removed %v, want none", second.Groups[0].Added, second.Groups[0].Removed)
	}
	if second.Groups[0].ContainerKey != containerKey {
		t.Fatalf("second run container = %s, want reused %s", second.Groups[0].ContainerKey, containerKey)
	}
}

func TestPublishGroups_ResumesAfterDescriptionWriteFails(t *testing.T) {
	const p = "profile-1"
	be := newFakeBackend()
	be.updateIssueErr = errors.New("jira unavailable")
	pub, st, cov := newTestPublisher(t, be)

	versionID, _, valueIDs := buildGroup(t, cov, p, "C_Sign", "1.0", "Mechanism", 0, []string{"RSA_PKCS"})
	seedTestCase(t, st, p, "QA-1")
	if err := cov.SetValueTests(p, valueIDs[0], []string{"QA-1"}); err != nil {
		t.Fatal(err)
	}

	first, err := pub.PublishGroups(context.Background(), p, "PROJ", versionID)
	if err != nil {
		t.Fatalf("first PublishGroups: %v", err)
	}
	if first.Failed != 1 || first.Groups[0].Error == "" {
		t.Fatalf("first result = %+v, want a failed group", first)
	}
	if be.createCalls != 1 {
		t.Fatalf("createCalls after first (failed) run = %d, want 1", be.createCalls)
	}
	containerKey := first.Groups[0].ContainerKey
	if containerKey == "" {
		t.Fatalf("expected the Test Set key to be recorded even though the description write failed")
	}

	// The backend recovers; re-run.
	be.updateIssueErr = nil
	second, err := pub.PublishGroups(context.Background(), p, "PROJ", versionID)
	if err != nil {
		t.Fatalf("second PublishGroups: %v", err)
	}
	if second.Failed != 0 {
		t.Fatalf("second result = %+v, want no failures", second)
	}
	if be.createCalls != 1 {
		t.Fatalf("createCalls after re-run = %d, want 1 total (no duplicate Test Set)", be.createCalls)
	}
	if second.Groups[0].ContainerKey != containerKey {
		t.Fatalf("second run container = %s, want reused %s", second.Groups[0].ContainerKey, containerKey)
	}
}

func TestPublishGroups_MembershipDeltaAddsAndRemoves(t *testing.T) {
	const p = "profile-1"
	be := newFakeBackend()
	pub, st, cov := newTestPublisher(t, be)

	versionID, _, valueIDs := buildGroup(t, cov, p, "C_Sign", "1.0", "Mechanism", 0, []string{"RSA_PKCS", "ED25519"})
	seedTestCase(t, st, p, "QA-1")
	seedTestCase(t, st, p, "QA-2")
	seedTestCase(t, st, p, "QA-3")
	if err := cov.SetValueTests(p, valueIDs[0], []string{"QA-1"}); err != nil {
		t.Fatal(err)
	}
	if err := cov.SetValueTests(p, valueIDs[1], []string{"QA-2"}); err != nil {
		t.Fatal(err)
	}

	first, err := pub.PublishGroups(context.Background(), p, "PROJ", versionID)
	if err != nil {
		t.Fatalf("first PublishGroups: %v", err)
	}
	containerKey := first.Groups[0].ContainerKey
	mirrorMembership(t, st, p, containerKey, []string{"QA-1", "QA-2"})

	// Drift: drop QA-2's mapping, add QA-3 in its place.
	if err := cov.SetValueTests(p, valueIDs[1], []string{"QA-3"}); err != nil {
		t.Fatal(err)
	}

	second, err := pub.PublishGroups(context.Background(), p, "PROJ", versionID)
	if err != nil {
		t.Fatalf("second PublishGroups: %v", err)
	}
	gr := second.Groups[0]
	if got := sorted(gr.Added); len(got) != 1 || got[0] != "QA-3" {
		t.Fatalf("added = %v, want [QA-3]", got)
	}
	if got := sorted(gr.Removed); len(got) != 1 || got[0] != "QA-2" {
		t.Fatalf("removed = %v, want [QA-2]", got)
	}
	if len(be.addCalls) != 2 || len(be.removeCalls) != 1 {
		t.Fatalf("addCalls = %+v, removeCalls = %+v", be.addCalls, be.removeCalls)
	}
	lastAdd := be.addCalls[len(be.addCalls)-1]
	if len(lastAdd.testKeys) != 1 || lastAdd.testKeys[0] != "QA-3" {
		t.Fatalf("second addCall = %+v, want [QA-3]", lastAdd)
	}
	lastRemove := be.removeCalls[len(be.removeCalls)-1]
	if len(lastRemove.testKeys) != 1 || lastRemove.testKeys[0] != "QA-2" {
		t.Fatalf("removeCall = %+v, want [QA-2]", lastRemove)
	}
}

func TestPublishGroups_OneFailingGroupDoesNotAbortRun(t *testing.T) {
	const p = "profile-1"
	be := newFakeBackend()
	be.failCreateOnCall = 1 // the first group's CreateContainer fails; the second must still run
	pub, st, cov := newTestPublisher(t, be)

	versionID, _, valueIDsA := buildGroup(t, cov, p, "C_Sign", "1.0", "Mechanism", 0, []string{"RSA_PKCS"})
	_, valueIDsB := addGroupToVersion(t, cov, p, versionID, "Session", 1, []string{"Valid"})
	seedTestCase(t, st, p, "QA-1")
	seedTestCase(t, st, p, "QA-2")
	if err := cov.SetValueTests(p, valueIDsA[0], []string{"QA-1"}); err != nil {
		t.Fatal(err)
	}
	if err := cov.SetValueTests(p, valueIDsB[0], []string{"QA-2"}); err != nil {
		t.Fatal(err)
	}

	result, err := pub.PublishGroups(context.Background(), p, "PROJ", versionID)
	if err != nil {
		t.Fatalf("PublishGroups: %v", err)
	}
	if len(result.Groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(result.Groups))
	}
	if result.Failed != 1 || result.Created != 1 {
		t.Fatalf("result = %+v, want 1 failed, 1 created", result)
	}
	if result.Groups[0].Error == "" {
		t.Fatalf("expected the first group (Mechanism, sort order 0) to have failed")
	}
	if result.Groups[1].Error != "" || result.Groups[1].ContainerKey == "" {
		t.Fatalf("second group = %+v, want it published despite the first group's failure", result.Groups[1])
	}
}

// countContainerTestRows returns how many test_container_test rows exist for
// profileID across all container keys, so tests can assert nothing was
// mirrored for a group that must not have published anything.
func countContainerTestRows(t *testing.T, st *store.Store, profileID string) int {
	t.Helper()
	var n int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM test_container_test WHERE profile_id = ?`, profileID).Scan(&n); err != nil {
		t.Fatalf("count test_container_test: %v", err)
	}
	return n
}

// TestPublishGroups_EmptyContainerKeyFromBackendFailsGroupCleanly covers the
// demo-backend shape: CreateContainer returns ("", nil) -- no error, but no
// usable key either. Proceeding as if that were a real key would make every
// group under the profile collide on containerKey "" (see the comment in
// publishOne). The group must be reported as failed, and neither a
// publication row nor a test_container_test mirror row may be written.
func TestPublishGroups_EmptyContainerKeyFromBackendFailsGroupCleanly(t *testing.T) {
	const p = "profile-1"
	be := newFakeBackend()
	be.createEmptyKey = true
	pub, st, cov := newTestPublisher(t, be)

	versionID, groupID, valueIDs := buildGroup(t, cov, p, "C_Sign", "1.0", "Mechanism", 0, []string{"RSA_PKCS"})
	seedTestCase(t, st, p, "QA-1")
	if err := cov.SetValueTests(p, valueIDs[0], []string{"QA-1"}); err != nil {
		t.Fatal(err)
	}

	result, err := pub.PublishGroups(context.Background(), p, "PROJ", versionID)
	if err != nil {
		t.Fatalf("PublishGroups: %v", err)
	}
	if result.Failed != 1 || result.Created != 0 {
		t.Fatalf("result = %+v, want 1 failed, 0 created", result)
	}
	gr := result.Groups[0]
	if gr.Error == "" {
		t.Fatalf("expected the group to report an error for an empty backend key")
	}
	if gr.ContainerKey != "" {
		t.Fatalf("ContainerKey = %q, want empty (no key was ever usable)", gr.ContainerKey)
	}

	_, _, existed, err := pub.publication(p, groupID)
	if err != nil {
		t.Fatalf("publication: %v", err)
	}
	if existed {
		t.Fatalf("expected no publication row to be written when the backend returned no key")
	}
	if n := countContainerTestRows(t, st, p); n != 0 {
		t.Fatalf("test_container_test rows = %d, want 0 (no membership mirrored under key \"\")", n)
	}
	if len(be.addCalls) != 0 {
		t.Fatalf("addCalls = %+v, want none (must not proceed to mirror membership against an empty key)", be.addCalls)
	}
}

// TestPublishGroups_ExistingEmptyContainerKeyFailsGroupCleanly covers the
// case TestPublishGroups_EmptyContainerKeyFromBackendFailsGroupCleanly does
// not: a coverage_group_publication row written by an earlier, buggier build
// that already holds container_key = "". Unlike the first-run case, existed
// is true here, so the guard has to sit after the publication lookup, not
// just inside the !existed branch, or execution falls through to
// currentMembers(profileID, "") and reproduces the same cross-group
// contamination the other guard removes.
func TestPublishGroups_ExistingEmptyContainerKeyFailsGroupCleanly(t *testing.T) {
	const p = "profile-1"
	be := newFakeBackend()
	pub, st, cov := newTestPublisher(t, be)

	versionID, groupID, valueIDs := buildGroup(t, cov, p, "C_Sign", "1.0", "Mechanism", 0, []string{"RSA_PKCS"})
	seedTestCase(t, st, p, "QA-1")
	if err := cov.SetValueTests(p, valueIDs[0], []string{"QA-1"}); err != nil {
		t.Fatal(err)
	}
	if err := pub.putPublication(p, groupID, "", nil); err != nil {
		t.Fatalf("seed empty-container-key publication row: %v", err)
	}

	result, err := pub.PublishGroups(context.Background(), p, "PROJ", versionID)
	if err != nil {
		t.Fatalf("PublishGroups: %v", err)
	}
	if result.Failed != 1 || result.Created != 0 {
		t.Fatalf("result = %+v, want 1 failed, 0 created", result)
	}
	gr := result.Groups[0]
	if gr.Error == "" {
		t.Fatalf("expected the group to report an error for a stored empty container key")
	}
	if gr.ContainerKey != "" {
		t.Fatalf("ContainerKey = %q, want empty (no key was ever usable)", gr.ContainerKey)
	}
	if n := countContainerTestRows(t, st, p); n != 0 {
		t.Fatalf("test_container_test rows = %d, want 0 (no membership mirrored under key \"\")", n)
	}
	if be.createCalls != 0 {
		t.Fatalf("createCalls = %d, want 0 (an existing row must not trigger a new CreateContainer)", be.createCalls)
	}
	if len(be.addCalls) != 0 {
		t.Fatalf("addCalls = %+v, want none (must not proceed to mirror membership against an empty key)", be.addCalls)
	}
}

// TestPublishGroups_DescriptionWriteFailureDoesNotCauseFalseConflict is the
// DetectDrift half of TestPublishGroups_ResumesAfterDescriptionWriteFails:
// that existing test proves a re-run recovers, but never checks what
// DetectDrift reports in between. If the published snapshot were recorded
// only after the (failing) description write, membership would already sit
// at the desired set on both the local model and the test_container_test
// mirror while the snapshot stayed old, and DetectDrift would misreport a
// Conflict -- as if Jira had independently added a test the publish call
// itself just added.
func TestPublishGroups_DescriptionWriteFailureDoesNotCauseFalseConflict(t *testing.T) {
	const p = "profile-1"
	be := newFakeBackend()
	be.updateIssueErr = errors.New("jira unavailable")
	pub, st, cov := newTestPublisher(t, be)

	versionID, _, valueIDs := buildGroup(t, cov, p, "C_Sign", "1.0", "Mechanism", 0, []string{"RSA_PKCS"})
	seedTestCase(t, st, p, "QA-1")
	if err := cov.SetValueTests(p, valueIDs[0], []string{"QA-1"}); err != nil {
		t.Fatal(err)
	}

	result, err := pub.PublishGroups(context.Background(), p, "PROJ", versionID)
	if err != nil {
		t.Fatalf("PublishGroups: %v", err)
	}
	if result.Failed != 1 {
		t.Fatalf("result = %+v, want 1 failed group (description write failed)", result)
	}

	statuses, err := pub.DetectDrift(p, versionID)
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("statuses = %+v, want 1 entry", statuses)
	}
	gs := statuses[0]
	if gs.State == Conflict || gs.State == Drift {
		t.Fatalf("State = %q, want InSync or LocalChanges, not a false Drift/Conflict caused by the failed description write", gs.State)
	}
	if len(gs.RemoteAdded) != 0 || len(gs.RemoteRemoved) != 0 {
		t.Fatalf("expected no remote-side diff, got RemoteAdded=%v RemoteRemoved=%v", gs.RemoteAdded, gs.RemoteRemoved)
	}
}

// TestPublishGroups_FailedAddNotMirrored proves a failed AddTestsToContainer
// call leaves test_container_test untouched: mirroring only happens after
// the backend call it mirrors has actually succeeded.
func TestPublishGroups_FailedAddNotMirrored(t *testing.T) {
	const p = "profile-1"
	be := newFakeBackend()
	be.addErr = errors.New("add failed (injected)")
	pub, st, cov := newTestPublisher(t, be)

	versionID, _, valueIDs := buildGroup(t, cov, p, "C_Sign", "1.0", "Mechanism", 0, []string{"RSA_PKCS"})
	seedTestCase(t, st, p, "QA-1")
	if err := cov.SetValueTests(p, valueIDs[0], []string{"QA-1"}); err != nil {
		t.Fatal(err)
	}

	result, err := pub.PublishGroups(context.Background(), p, "PROJ", versionID)
	if err != nil {
		t.Fatalf("PublishGroups: %v", err)
	}
	// Assert the failure is actually the add failure, not just that some
	// failure occurred: the prefix comes from the "add tests to test set: %v"
	// Sprintf in publishOne, so this stays tied to the real failure point and
	// would catch a regression where an earlier step (e.g. create, or the
	// publication-row write) started failing instead.
	wantPrefix := "add tests to test set:"
	if result.Failed != 1 || !strings.HasPrefix(result.Groups[0].Error, wantPrefix) {
		t.Fatalf("result = %+v, want a failed group whose error starts with %q", result, wantPrefix)
	}
	if n := countContainerTestRows(t, st, p); n != 0 {
		t.Fatalf("test_container_test rows = %d, want 0 (failed add must not be mirrored)", n)
	}
}

// TestPublishGroups_FailedRemoveNotMirrored proves a failed
// RemoveTestsFromContainer call leaves the mirror row for the not-actually
// -removed test in place.
func TestPublishGroups_FailedRemoveNotMirrored(t *testing.T) {
	const p = "profile-1"
	be := newFakeBackend()
	pub, st, cov := newTestPublisher(t, be)

	versionID, _, valueIDs := buildGroup(t, cov, p, "C_Sign", "1.0", "Mechanism", 0, []string{"RSA_PKCS", "ED25519"})
	seedTestCase(t, st, p, "QA-1")
	seedTestCase(t, st, p, "QA-2")
	if err := cov.SetValueTests(p, valueIDs[0], []string{"QA-1"}); err != nil {
		t.Fatal(err)
	}
	if err := cov.SetValueTests(p, valueIDs[1], []string{"QA-2"}); err != nil {
		t.Fatal(err)
	}

	first, err := pub.PublishGroups(context.Background(), p, "PROJ", versionID)
	if err != nil {
		t.Fatalf("first PublishGroups: %v", err)
	}
	containerKey := first.Groups[0].ContainerKey
	mirrorMembership(t, st, p, containerKey, []string{"QA-1", "QA-2"})

	// Unmap QA-2 locally so this group's desired set drops it, forcing a
	// remove call; make that remove call fail.
	if err := cov.SetValueTests(p, valueIDs[1], nil); err != nil {
		t.Fatal(err)
	}
	be.removeErr = errors.New("remove failed (injected)")

	second, err := pub.PublishGroups(context.Background(), p, "PROJ", versionID)
	if err != nil {
		t.Fatalf("second PublishGroups: %v", err)
	}
	if second.Failed != 1 || second.Groups[0].Error == "" {
		t.Fatalf("second result = %+v, want a failed group (remove failed)", second)
	}

	current, err := pub.currentMembers(p, containerKey)
	if err != nil {
		t.Fatalf("currentMembers: %v", err)
	}
	assertKeys(t, "currentMembers after failed remove", current, []string{"QA-1", "QA-2"})
}

// TestPublishGroups_CreatesTestContainerRow proves publishing a group writes
// a test_container row for the new Test Set, so it shows up in the Test Sets
// container view immediately instead of staying invisible until the next
// full sync (in demo mode, forever).
func TestPublishGroups_CreatesTestContainerRow(t *testing.T) {
	const p = "profile-1"
	be := newFakeBackend()
	pub, st, cov := newTestPublisher(t, be)

	versionID, _, valueIDs := buildGroup(t, cov, p, "C_Sign", "1.0", "Mechanism", 0, []string{"RSA_PKCS"})
	seedTestCase(t, st, p, "QA-1")
	if err := cov.SetValueTests(p, valueIDs[0], []string{"QA-1"}); err != nil {
		t.Fatal(err)
	}

	result, err := pub.PublishGroups(context.Background(), p, "PROJ", versionID)
	if err != nil {
		t.Fatalf("PublishGroups: %v", err)
	}
	if result.Failed != 0 {
		t.Fatalf("result = %+v, want no failures", result)
	}
	containerKey := result.Groups[0].ContainerKey

	var kind, summary string
	if err := st.DB().QueryRow(
		`SELECT kind, summary FROM test_container WHERE profile_id = ? AND jira_key = ?`,
		p, containerKey).Scan(&kind, &summary); err != nil {
		t.Fatalf("read test_container row: %v", err)
	}
	if kind != "testset" {
		t.Fatalf("kind = %q, want testset", kind)
	}
	wantSummary := "C_Sign 1.0 - Mechanism"
	if summary != wantSummary {
		t.Fatalf("summary = %q, want %q", summary, wantSummary)
	}
}

// TestPublishGroups_ContainerRowCreateIfMissingDoesNotClobberSync proves the
// ON CONFLICT DO NOTHING semantics: a test_container row a real Jira sync
// already populated (with a status the publish path never writes) must
// survive a publish run untouched.
func TestPublishGroups_ContainerRowCreateIfMissingDoesNotClobberSync(t *testing.T) {
	const p = "profile-1"
	be := newFakeBackend()
	pub, st, cov := newTestPublisher(t, be)

	versionID, _, valueIDs := buildGroup(t, cov, p, "C_Sign", "1.0", "Mechanism", 0, []string{"RSA_PKCS"})
	seedTestCase(t, st, p, "QA-1")
	if err := cov.SetValueTests(p, valueIDs[0], []string{"QA-1"}); err != nil {
		t.Fatal(err)
	}

	// Pre-publish the group once so a container key exists, then seed a
	// test_container row for it as a full sync would, with a status the
	// publish path never sets.
	first, err := pub.PublishGroups(context.Background(), p, "PROJ", versionID)
	if err != nil {
		t.Fatalf("first PublishGroups: %v", err)
	}
	containerKey := first.Groups[0].ContainerKey
	if containerKey == "" {
		t.Fatalf("expected a container key after the first publish")
	}
	if _, err := st.DB().Exec(
		`UPDATE test_container SET status = ? WHERE profile_id = ? AND jira_key = ?`,
		"In Progress", p, containerKey); err != nil {
		t.Fatalf("seed synced status: %v", err)
	}

	// Republish; the group's publication row already exists, so this exercises
	// the already-published path.
	second, err := pub.PublishGroups(context.Background(), p, "PROJ", versionID)
	if err != nil {
		t.Fatalf("second PublishGroups: %v", err)
	}
	if second.Failed != 0 {
		t.Fatalf("second result = %+v, want no failures", second)
	}
	if second.Groups[0].ContainerKey != containerKey {
		t.Fatalf("second run container = %s, want reused %s", second.Groups[0].ContainerKey, containerKey)
	}

	var status string
	if err := st.DB().QueryRow(
		`SELECT status FROM test_container WHERE profile_id = ? AND jira_key = ?`,
		p, containerKey).Scan(&status); err != nil {
		t.Fatalf("read test_container status: %v", err)
	}
	if status != "In Progress" {
		t.Fatalf("status = %q, want In Progress to have survived the create-if-missing insert", status)
	}
}

func TestPublishGroups_StaleTestExcludedFromDesiredSet(t *testing.T) {
	const p = "profile-1"
	be := newFakeBackend()
	pub, st, cov := newTestPublisher(t, be)

	versionID, _, valueIDs := buildGroup(t, cov, p, "C_Sign", "1.0", "Mechanism", 0, []string{"RSA_PKCS"})
	seedTestCase(t, st, p, "QA-1")
	if err := cov.SetValueTests(p, valueIDs[0], []string{"QA-1", "QA-DELETED"}); err != nil {
		t.Fatal(err)
	}

	result, err := pub.PublishGroups(context.Background(), p, "PROJ", versionID)
	if err != nil {
		t.Fatalf("PublishGroups: %v", err)
	}
	got := sorted(result.Groups[0].Added)
	if len(got) != 1 || got[0] != "QA-1" {
		t.Fatalf("added = %v, want only [QA-1] (QA-DELETED has no test_case row)", got)
	}
}
