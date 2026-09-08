package syncer_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"agile-suite/xtm/internal/backend"
	"agile-suite/xtm/internal/store"
	"agile-suite/xtm/internal/syncer"
	"agile-suite/xtm/internal/testrepo"
)

// newSyncerRepoWithProfile builds a *testrepo.Repository over a temp store,
// matching the setup every other test in this package uses (see
// kiwi_routing_commit_test.go): a fresh on-disk SQLite store and a fixed
// profile ID. Nothing in this package registers a separate "profile" row —
// the store's profile-scoped tables are keyed by profileID alone.
func newSyncerRepoWithProfile(t *testing.T) (*testrepo.Repository, string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return testrepo.NewRepository(st), "p1"
}

// recordingBackend counts which backend received which bug call.
type recordingBackend struct {
	backend.Backend // embedded so only the methods under test need defining
	name            string
	created         []string
	linked          [][2]string
	createErr       error
	linkErr         error
	// bugCreation is what Capabilities().SupportsBugCreation reports. A real
	// Kiwi adapter reports false here — it genuinely cannot create a Jira
	// issue itself, which is exactly why bug routing (a separate bugBackend)
	// exists. Tests that model a routed Kiwi profile must leave this false so
	// they exercise the e.bugBackend != nil half of the commit gate, not the
	// caps.SupportsBugCreation half.
	bugCreation bool
}

// Capabilities reports the configured SupportsBugCreation flag so tests can
// model either an Xray-style backend (bugCreation: true, no routing needed)
// or a Kiwi-style one (bugCreation: false, routed through a bug backend).
// Every other flag stays at its zero value: the only pending rows queued in
// these tests are bug_create, so no other capability gate is exercised.
func (r *recordingBackend) Capabilities() backend.Capabilities {
	return backend.Capabilities{Name: r.name, SupportsBugCreation: r.bugCreation}
}

func (r *recordingBackend) CreateBug(ctx context.Context, projectKey, issueType, summary, description, priority string, labels []string, extraFields map[string]any) (string, error) {
	r.created = append(r.created, projectKey+"/"+summary)
	if r.createErr != nil {
		return "", r.createErr
	}
	return "DEF-42", nil
}

func (r *recordingBackend) CreateBugLink(ctx context.Context, testKey, bugKey string) error {
	r.linked = append(r.linked, [2]string{testKey, bugKey})
	return r.linkErr
}

// TestBugCreateGoesToTheBugBackendAndTheLinkStaysHome is the routing rule. The
// two halves of filing a bug belong to different servers when a bug backend is
// configured: Jira creates the issue, Kiwi carries the hyperlink. The primary
// reports SupportsBugCreation: false, exactly like a real Kiwi adapter — this
// is what makes the test prove the e.bugBackend != nil half of the commit
// gate runs the bucket at all, rather than merely proving routing works on a
// backend that never needed it.
func TestBugCreateGoesToTheBugBackendAndTheLinkStaysHome(t *testing.T) {
	repo, profileID := newSyncerRepoWithProfile(t)
	if _, err := repo.CreateBugForTest(profileID, "T-1", "EXEC-9", testrepo.BugDraft{
		ProjectKey: "DEF", IssueType: "Bug", Summary: "Boom",
	}); err != nil {
		t.Fatalf("queue bug: %v", err)
	}

	primary := &recordingBackend{name: "kiwi", bugCreation: false}
	bugs := &recordingBackend{name: "jira"}
	e := syncer.New(primary, repo, syncer.WithBugBackend(bugs))

	res, err := e.CommitChanges(context.Background(), profileID, "SNMP")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("unexpected failures: %+v", res.Failed)
	}
	if len(bugs.created) != 1 {
		t.Errorf("bug backend received %d creates, want 1", len(bugs.created))
	}
	if len(primary.created) != 0 {
		t.Errorf("primary backend received %d creates, want 0", len(primary.created))
	}
	if len(primary.linked) != 1 {
		t.Fatalf("primary backend received %d links, want 1", len(primary.linked))
	}
	// The link must anchor to the EXECUTION, not the test: reverting
	// linkTarget back to p.TestKey would hyperlink an unrelated Kiwi
	// execution silently, and this is the only assertion that catches it.
	if want := [2]string{"EXEC-9", "DEF-42"}; primary.linked[0] != want {
		t.Errorf("primary linked %v, want %v (execution key, not test key)", primary.linked[0], want)
	}
	if len(bugs.linked) != 0 {
		t.Errorf("bug backend received %d links, want 0", len(bugs.linked))
	}
}

// TestNoBugBackendRoutesEverythingToThePrimary is the regression guard for
// every Xray profile: without a bug backend nothing about this path changes.
func TestNoBugBackendRoutesEverythingToThePrimary(t *testing.T) {
	repo, profileID := newSyncerRepoWithProfile(t)
	if _, err := repo.CreateBugForTest(profileID, "T-1", "", testrepo.BugDraft{
		ProjectKey: "DEF", IssueType: "Bug", Summary: "Boom",
	}); err != nil {
		t.Fatalf("queue bug: %v", err)
	}

	primary := &recordingBackend{name: "xray", bugCreation: true}
	e := syncer.New(primary, repo)

	if _, err := e.CommitChanges(context.Background(), profileID, "QA"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(primary.created) != 1 || len(primary.linked) != 1 {
		t.Fatalf("primary got %d creates and %d links, want 1 and 1", len(primary.created), len(primary.linked))
	}
	// Unrouted path still links by TEST key, not execution key.
	if want := [2]string{"T-1", "DEF-42"}; primary.linked[0] != want {
		t.Errorf("primary linked %v, want %v (test key)", primary.linked[0], want)
	}
}

// TestALinkFailureKeepsTheCreatedBug is the partial-failure rule. The Jira
// issue exists; deleting it would be worse than losing the link. The commit
// reports the failure, the pending row survives for a retry, and the real key
// is recorded so the retry links instead of creating a duplicate. The primary
// reports SupportsBugCreation: false, matching a real Kiwi adapter, so this
// only runs through the e.bugBackend != nil half of the commit gate.
func TestALinkFailureKeepsTheCreatedBug(t *testing.T) {
	repo, profileID := newSyncerRepoWithProfile(t)
	if _, err := repo.CreateBugForTest(profileID, "T-1", "EXEC-9", testrepo.BugDraft{
		ProjectKey: "DEF", IssueType: "Bug", Summary: "Boom",
	}); err != nil {
		t.Fatalf("queue bug: %v", err)
	}

	primary := &recordingBackend{name: "kiwi", bugCreation: false, linkErr: errors.New("execution not found")}
	bugs := &recordingBackend{name: "jira"}
	e := syncer.New(primary, repo, syncer.WithBugBackend(bugs))

	res, err := e.CommitChanges(context.Background(), profileID, "SNMP")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(res.Failed) != 1 {
		t.Fatalf("got %d failures, want exactly 1 naming the link", len(res.Failed))
	}

	// Even the failed attempt targeted the EXECUTION key, not the test key —
	// reverting linkTarget back to p.TestKey would hyperlink an unrelated
	// Kiwi execution silently instead of failing loudly.
	if len(primary.linked) != 1 {
		t.Fatalf("primary backend received %d link attempts, want 1", len(primary.linked))
	}
	if want := [2]string{"EXEC-9", "DEF-42"}; primary.linked[0] != want {
		t.Errorf("primary attempted to link %v, want %v (execution key, not test key)", primary.linked[0], want)
	}

	// The pending row survives so the user can retry.
	pending, err := repo.ListPendingChanges(profileID)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	found := false
	for _, p := range pending {
		if p.EntityType == "bug_create" {
			found = true
		}
	}
	if !found {
		t.Fatal("the bug_create pending row was cleared despite the link failing")
	}

	// The retry links the EXISTING issue instead of creating a second one.
	primary.linkErr = nil
	if _, err := e.CommitChanges(context.Background(), profileID, "SNMP"); err != nil {
		t.Fatalf("retry commit: %v", err)
	}
	if len(bugs.created) != 1 {
		t.Errorf("bug backend created %d issues across the retry, want 1", len(bugs.created))
	}
}

// TestNoBugBackendAndNoBugCreationSkipsTheBucket pins the other side of the
// gate: a backend that cannot create bugs and has no bug connection must still
// skip the rows with a reason, not attempt a create against a backend that
// will reject it.
func TestNoBugBackendAndNoBugCreationSkipsTheBucket(t *testing.T) {
	repo, profileID := newSyncerRepoWithProfile(t)
	if _, err := repo.CreateBugForTest(profileID, "T-1", "", testrepo.BugDraft{
		ProjectKey: "DEF", IssueType: "Bug", Summary: "Boom",
	}); err != nil {
		t.Fatalf("queue bug: %v", err)
	}

	primary := &recordingBackend{name: "kiwi", bugCreation: false}
	e := syncer.New(primary, repo)

	res, err := e.CommitChanges(context.Background(), profileID, "SNMP")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("unexpected failures: %+v", res.Failed)
	}
	found := false
	for _, s := range res.Skipped {
		if s.EntityType == "bug_create" {
			found = true
			if s.Reason == "" {
				t.Errorf("skipped bug_create row missing a reason: %+v", s)
			}
		}
	}
	if !found {
		t.Errorf("expected a skipped bug_create row, got skipped=%+v", res.Skipped)
	}
	if len(primary.created) != 0 {
		t.Errorf("primary backend received %d creates, want 0 (bucket should have been skipped)", len(primary.created))
	}
}

// TestBrokenBugConnectionFailsTheCreateInsteadOfSkippingIt is the commit-path
// counterpart to syncBugs' WithBugBackendError guard: a bug connection IS
// configured but unusable, so the queued bug create must be reported as a
// FAILURE naming the connection, not folded into the "no bug tracker
// configured" skip bucket — that message would be false here and would send
// the user hunting for a setting that already exists instead of fixing the
// broken connection. The primary reports SupportsBugCreation: false, matching
// a real Kiwi adapter, so this only runs through the routing-was-attempted
// half of the gate.
func TestBrokenBugConnectionFailsTheCreateInsteadOfSkippingIt(t *testing.T) {
	repo, profileID := newSyncerRepoWithProfile(t)
	if _, err := repo.CreateBugForTest(profileID, "T-1", "", testrepo.BugDraft{
		ProjectKey: "DEF", IssueType: "Bug", Summary: "Boom",
	}); err != nil {
		t.Fatalf("queue bug: %v", err)
	}

	primary := &recordingBackend{name: "kiwi", bugCreation: false}
	connErr := errors.New("load credentials for bug connection https://jira.example.com: keyring locked")
	e := syncer.New(primary, repo, syncer.WithBugBackendError(connErr))

	res, err := e.CommitChanges(context.Background(), profileID, "SNMP")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	for _, s := range res.Skipped {
		if s.EntityType == "bug_create" {
			t.Errorf("bug_create row was skipped (reason %q), want it FAILED naming the bug connection instead", s.Reason)
		}
	}
	if len(res.Failed) != 1 {
		t.Fatalf("got %d failures, want exactly 1 naming the bug connection: %+v", len(res.Failed), res.Failed)
	}
	if !strings.Contains(res.Failed[0].Error, connErr.Error()) {
		t.Errorf("failure error = %q, want it to name/wrap the bug connection failure %q", res.Failed[0].Error, connErr)
	}
	if len(primary.created) != 0 {
		t.Errorf("primary backend received %d creates, want 0 (no create should be attempted against a broken connection)", len(primary.created))
	}

	// The pending row must survive so the user can retry once the connection
	// is fixed, exactly as any other failed commit leaves its row in place.
	pending, err := repo.ListPendingChanges(profileID)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	found := false
	for _, p := range pending {
		if p.EntityType == "bug_create" {
			found = true
		}
	}
	if !found {
		t.Fatal("the bug_create pending row was cleared despite the create failing")
	}
}

// runLinkingBackend is a primary backend that resolves the execution itself
// (backend.RunScopedBugLinker), the way the Kiwi adapter does. It records both
// call shapes so a test can prove which one the commit path chose.
type runLinkingBackend struct {
	recordingBackend
	runLinked [][3]string
}

func (r *runLinkingBackend) CreateRunBugLink(ctx context.Context, execKey, testKey, bugKey string) error {
	r.runLinked = append(r.runLinked, [3]string{execKey, testKey, bugKey})
	return r.linkErr
}

// TestRoutedLinkPassesBothTheRunAndTheTest is the id-space guard for the
// commit path. A Kiwi KindTestExec container key is a TestRun id, not the id
// of the execution a hyperlink hangs on, so the run alone cannot resolve it.
// A backend that says it can do the resolution must be handed both keys.
func TestRoutedLinkPassesBothTheRunAndTheTest(t *testing.T) {
	repo, profileID := newSyncerRepoWithProfile(t)
	if _, err := repo.CreateBugForTest(profileID, "T-1", "EXEC-9", testrepo.BugDraft{
		ProjectKey: "DEF", IssueType: "Bug", Summary: "Boom",
	}); err != nil {
		t.Fatalf("queue bug: %v", err)
	}

	primary := &runLinkingBackend{recordingBackend: recordingBackend{name: "kiwi", bugCreation: false}}
	bugs := &recordingBackend{name: "jira"}
	e := syncer.New(primary, repo, syncer.WithBugBackend(bugs))

	res, err := e.CommitChanges(context.Background(), profileID, "SNMP")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("unexpected failures: %+v", res.Failed)
	}
	if len(primary.runLinked) != 1 {
		t.Fatalf("primary received %d run-scoped links, want 1 (it implements RunScopedBugLinker)", len(primary.runLinked))
	}
	if want := [3]string{"EXEC-9", "T-1", "DEF-42"}; primary.runLinked[0] != want {
		t.Errorf("primary linked %v, want %v (run AND test, so the execution can be resolved)", primary.runLinked[0], want)
	}
	if len(primary.linked) != 0 {
		t.Errorf("primary also received %d single-key links, want 0", len(primary.linked))
	}
}

// TestUnroutedProfileKeepsTheSingleKeyLink pins the fallback: without a bug
// connection nothing changes, even for a backend that could resolve a run —
// there is no execution key on the pending row to resolve from.
func TestUnroutedProfileKeepsTheSingleKeyLink(t *testing.T) {
	repo, profileID := newSyncerRepoWithProfile(t)
	if _, err := repo.CreateBugForTest(profileID, "T-1", "", testrepo.BugDraft{
		ProjectKey: "DEF", IssueType: "Bug", Summary: "Boom",
	}); err != nil {
		t.Fatalf("queue bug: %v", err)
	}

	primary := &runLinkingBackend{recordingBackend: recordingBackend{name: "xray", bugCreation: true}}
	e := syncer.New(primary, repo)

	if _, err := e.CommitChanges(context.Background(), profileID, "QA"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(primary.runLinked) != 0 {
		t.Errorf("primary received %d run-scoped links, want 0 without a bug connection", len(primary.runLinked))
	}
	if want := [2]string{"T-1", "DEF-42"}; len(primary.linked) != 1 || primary.linked[0] != want {
		t.Errorf("primary linked %v, want exactly one %v (test key)", primary.linked, want)
	}
}
