package syncer_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agile-suite/xtm/internal/backend"
	"agile-suite/xtm/internal/syncer"
	"agile-suite/xtm/internal/testrepo"
)

// seedTest inserts one Test row for the profile, using the same repo method
// the pull path uses to upsert synced Tests.
func seedTest(t *testing.T, repo *testrepo.Repository, profileID, key string) {
	t.Helper()
	if err := repo.UpsertTests(profileID, []testrepo.TestCase{
		{Key: key, ID: key, Summary: "T", Status: "CONFIRMED", Priority: "Medium", Updated: "2026-01-01T00:00:00Z"},
	}); err != nil {
		t.Fatalf("seed test: %v", err)
	}
}

// hydratingBackend records which key reads it received and answers key
// lookups. It stands in for the bug backend (Jira) in the routed setup.
type hydratingBackend struct {
	backend.Backend
	keyReads [][]string
}

func (h *hydratingBackend) ListBugsByKeys(ctx context.Context, keys []string) ([]backend.Bug, error) {
	h.keyReads = append(h.keyReads, keys)
	out := make([]backend.Bug, 0, len(keys))
	for _, k := range keys {
		out = append(out, backend.Bug{Key: k, Summary: "Filed from Kiwi", Status: "Open"})
	}
	return out, nil
}

// partialHydratingBackend hydrates only a subset of the keys it is asked for,
// modeling a linked key the bug backend does not return: a deleted Jira
// issue, or a key Task 7's shape check skipped as not a valid issue key.
type partialHydratingBackend struct {
	backend.Backend
	only map[string]backend.Bug
}

func (h *partialHydratingBackend) ListBugsByKeys(ctx context.Context, keys []string) ([]backend.Bug, error) {
	out := make([]backend.Bug, 0, len(keys))
	for _, k := range keys {
		if b, ok := h.only[k]; ok {
			out = append(out, b)
		}
	}
	return out, nil
}

// linkOnlyBackend stands in for Kiwi: it reports which bugs are linked but
// knows nothing about them. projectReads counts ListProjectBugs calls -
// production always calls that on the PRIMARY backend (never the bug
// backend), so the regression this guards against (Pass 1 running even when
// a bug backend is configured) can only be caught by a counter that lives
// here, not on the bug backend fake.
type linkOnlyBackend struct {
	backend.Backend
	bugs         []backend.Bug
	links        []backend.BugLink
	projectReads int
}

func (l *linkOnlyBackend) ListBugs(ctx context.Context, testProjectKey string, testKeys []string, issueType string, onProgress func(done, total int)) ([]backend.Bug, []backend.BugLink, error) {
	return l.bugs, l.links, nil
}

func (l *linkOnlyBackend) ListProjectBugs(ctx context.Context, projKey, issueType string) ([]backend.Bug, error) {
	l.projectReads++
	return nil, nil
}

// TestSyncHydratesOnlyLinkedBugs is the read rule the design settled on: the
// Bugs view shows what this workspace's executions actually link to, not every
// issue in the Jira project. The project-wide pass is skipped entirely.
func TestSyncHydratesOnlyLinkedBugs(t *testing.T) {
	repo, profileID := newSyncerRepoWithProfile(t)
	seedTest(t, repo, profileID, "1")

	primary := &linkOnlyBackend{
		bugs:  []backend.Bug{{Key: "DEF-42"}},
		links: []backend.BugLink{{TestKey: "1", BugKey: "DEF-42", LinkID: "5"}},
	}
	bugs := &hydratingBackend{}
	e := syncer.New(primary, repo, syncer.WithBugBackend(bugs))

	if err := e.SyncBugs(context.Background(), profileID, "SNMP", nil); err != nil {
		t.Fatalf("sync bugs: %v", err)
	}

	// The counter that matters lives on the PRIMARY backend: production's
	// Pass 1 always calls ListProjectBugs on e.backend, never on e.bugBackend.
	if primary.projectReads != 0 {
		t.Errorf("project-wide bug search ran %d times on the primary backend, want 0 with a bug backend configured", primary.projectReads)
	}
	if len(bugs.keyReads) != 1 {
		t.Fatalf("key hydration ran %d times, want 1", len(bugs.keyReads))
	}
	if len(bugs.keyReads[0]) != 1 || bugs.keyReads[0][0] != "DEF-42" {
		t.Errorf("hydrated %v, want exactly the linked key DEF-42", bugs.keyReads[0])
	}

	stored, err := repo.ListBugsWithTests(profileID)
	if err != nil {
		t.Fatalf("read stored bugs: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("stored %d bugs, want 1", len(stored))
	}
	if stored[0].Summary != "Filed from Kiwi" {
		t.Errorf("bug was stored key-only (summary %q); hydration did not reach the cache", stored[0].Summary)
	}
}

// TestSyncDropsUnhydratedLinkedBugs is the controller ruling: a linked key the
// bug backend does not return must not survive into the cache as a
// blank-summary row that invites a click to nowhere. Its bug row and its test
// link both have to go; the hyperlink itself still exists on the Kiwi
// execution, so nothing remote is lost.
func TestSyncDropsUnhydratedLinkedBugs(t *testing.T) {
	repo, profileID := newSyncerRepoWithProfile(t)
	seedTest(t, repo, profileID, "1")
	seedTest(t, repo, profileID, "2")

	primary := &linkOnlyBackend{
		bugs: []backend.Bug{{Key: "DEF-1"}, {Key: "DEF-2"}},
		links: []backend.BugLink{
			{TestKey: "1", BugKey: "DEF-1", LinkID: "5"},
			{TestKey: "2", BugKey: "DEF-2", LinkID: "6"},
		},
	}
	bugs := &partialHydratingBackend{only: map[string]backend.Bug{
		"DEF-1": {Key: "DEF-1", Summary: "Filed from Kiwi", Status: "Open"},
		// DEF-2 deliberately absent: models a deleted issue / skipped key.
	}}
	e := syncer.New(primary, repo, syncer.WithBugBackend(bugs))

	// The drop is also a user-facing signal, not just a log line: Jira omits
	// issues the token cannot browse rather than erroring, so this exact
	// situation is what a permissions gap on the bug project looks like.
	err := e.SyncBugs(context.Background(), profileID, "SNMP", nil)
	var partial *syncer.PartialSyncError
	if !errors.As(err, &partial) {
		t.Fatalf("sync bugs = %v, want a *PartialSyncError naming the dropped key", err)
	}
	if len(partial.StageFailures) != 1 || partial.StageFailures[0].Stage != "bugs" {
		t.Fatalf("stage failures = %+v, want exactly one for the bugs stage", partial.StageFailures)
	}
	if !strings.Contains(partial.StageFailures[0].Message, "DEF-2") {
		t.Errorf("warning %q does not name the dropped key DEF-2", partial.StageFailures[0].Message)
	}

	stored, err := repo.ListBugsWithTests(profileID)
	if err != nil {
		t.Fatalf("read stored bugs: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("stored %d bugs, want exactly 1 (the hydrated key only): %+v", len(stored), stored)
	}
	if stored[0].Key != "DEF-1" {
		t.Errorf("stored bug key = %q, want DEF-1 (DEF-2 should have been dropped)", stored[0].Key)
	}
	if stored[0].Summary == "" {
		t.Error("stored bug has a blank summary; an unhydrated husk reached the cache")
	}

	// The dropped bug's link row must be gone too, not just orphaned: test 2
	// must no longer appear as bug-affected at all.
	affected, err := repo.BugAffectedTestKeys(profileID)
	if err != nil {
		t.Fatalf("bug affected test keys: %v", err)
	}
	for _, k := range affected {
		if k == "2" {
			t.Errorf("test_bug still links test 2 to the dropped key DEF-2: affected=%v", affected)
		}
	}
	if len(affected) != 1 || affected[0] != "1" {
		t.Errorf("bug affected test keys = %v, want exactly [1]", affected)
	}
}

// mustNotReadBugsBackend fails the test if any bug-read method is invoked. It
// proves the WithBugBackendError early-return happens before syncBugs makes
// its first read (AllTestKeys) or reaches either ReplaceAll* call.
type mustNotReadBugsBackend struct {
	backend.Backend
	t *testing.T
}

func (m *mustNotReadBugsBackend) ListBugs(ctx context.Context, testProjectKey string, testKeys []string, issueType string, onProgress func(done, total int)) ([]backend.Bug, []backend.BugLink, error) {
	m.t.Error("ListBugs called despite a bug-connection error being set")
	return nil, nil, nil
}

func (m *mustNotReadBugsBackend) ListProjectBugs(ctx context.Context, projKey, issueType string) ([]backend.Bug, error) {
	m.t.Error("ListProjectBugs called despite a bug-connection error being set")
	return nil, nil
}

// TestSyncBugsWithAConfiguredButUnusableBugConnectionLeavesTheCacheUntouched
// is the IMPORTANT fix this round exists for: a transient bug-connection
// failure (a keyring hiccup, an unreadable row) must fail the bug work only,
// naming the connection, and must leave the bug/bug-link cache from the last
// good sync exactly as it was. Wiping it with an empty result set is the same
// defect class as -336: a stage silently reporting a clean, empty result
// instead of failing loudly.
func TestSyncBugsWithAConfiguredButUnusableBugConnectionLeavesTheCacheUntouched(t *testing.T) {
	repo, profileID := newSyncerRepoWithProfile(t)
	seedTest(t, repo, profileID, "1")

	// Seed the cache the way a prior successful sync would have left it.
	if err := repo.ReplaceAllBugs(profileID, []testrepo.Bug{{Key: "DEF-1", Summary: "Existing", Status: "Open"}}); err != nil {
		t.Fatalf("seed bugs: %v", err)
	}
	if err := repo.ReplaceAllBugLinks(profileID, []testrepo.BugLink{{TestKey: "1", BugKey: "DEF-1", LinkID: "5"}}); err != nil {
		t.Fatalf("seed links: %v", err)
	}

	connErr := errors.New("load credentials for bug connection https://jira.example.com: keyring locked")
	primary := &mustNotReadBugsBackend{t: t}
	e := syncer.New(primary, repo, syncer.WithBugBackendError(connErr))

	err := e.SyncBugs(context.Background(), profileID, "SNMP", nil)
	if err == nil {
		t.Fatal("SyncBugs = nil error, want the bug-connection failure surfaced")
	}
	if !strings.Contains(err.Error(), connErr.Error()) {
		t.Errorf("error = %q, want it to name/wrap the bug connection failure %q", err, connErr)
	}

	stored, err := repo.ListBugsWithTests(profileID)
	if err != nil {
		t.Fatalf("read stored bugs: %v", err)
	}
	if len(stored) != 1 || stored[0].Key != "DEF-1" || stored[0].Summary != "Existing" {
		t.Errorf("cached bugs changed: %+v, want the pre-existing DEF-1/Existing row untouched", stored)
	}

	affected, err := repo.BugAffectedTestKeys(profileID)
	if err != nil {
		t.Fatalf("bug affected test keys: %v", err)
	}
	if len(affected) != 1 || affected[0] != "1" {
		t.Errorf("cached bug links changed: %v, want the pre-existing link to test 1 untouched", affected)
	}
}
