package syncer

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"xray-test-manager/internal/backend"
	"xray-test-manager/internal/store"
	"xray-test-manager/internal/testrepo"
)

// discoveryMockBackend is a minimal backend.Backend for exercising
// discoverCrossProjectExecs in isolation. It implements only the two methods
// that pass touches -- ListContainers and GetTestRuns -- and embeds a nil
// backend.Backend so any other call panics loudly (proving the pass is as
// bounded as claimed). It records the source projects it was asked to list so
// the no-sources case can assert zero backend calls.
type discoveryMockBackend struct {
	backend.Backend // nil: unimplemented methods panic if ever called

	byProject map[string]struct {
		containers []backend.Container
		links      []backend.ContainerLink
	}
	runs map[string][]backend.TestRun

	mu          sync.Mutex
	listedFor   []string
	runsFetched []string
}

func (m *discoveryMockBackend) ListContainers(_ context.Context, projectKey string, _ func(done, total int)) ([]backend.Container, []backend.ContainerLink, error) {
	m.mu.Lock()
	m.listedFor = append(m.listedFor, projectKey)
	m.mu.Unlock()
	p := m.byProject[projectKey]
	return p.containers, p.links, nil
}

func (m *discoveryMockBackend) GetTestRuns(_ context.Context, execKey string) ([]backend.TestRun, error) {
	m.mu.Lock()
	m.runsFetched = append(m.runsFetched, execKey)
	m.mu.Unlock()
	return m.runs[execKey], nil
}

// newDiscoveryRepo opens a temp store and seeds the profile's own tests so
// AllTestKeys populates the membership-matching set.
func newDiscoveryRepo(t *testing.T, profileID string, testKeys []string) *testrepo.Repository {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	repo := testrepo.NewRepository(st)
	tests := make([]testrepo.TestCase, len(testKeys))
	for i, k := range testKeys {
		tests[i] = testrepo.TestCase{Key: k, Summary: k}
	}
	if err := repo.UpsertTests(profileID, tests); err != nil {
		t.Fatalf("seed tests: %v", err)
	}
	return repo
}

// TestDiscoverCrossProjectExecsKeepsOnlyMatchingExecs asserts that source-scoped
// discovery stores only the source-project executions that include one of the
// profile's own tests, keeps only the matching links, ignores non-execution
// containers, dedupes an execution seen in two sources, and skips executions
// already known locally.
func TestDiscoverCrossProjectExecsKeepsOnlyMatchingExecs(t *testing.T) {
	const profileID = "p1"
	// The profile owns DEMO-1 and DEMO-2. Foreign keys (SRCA-*) are not ours.
	repo := newDiscoveryRepo(t, profileID, []string{"DEMO-1", "DEMO-2"})

	mock := &discoveryMockBackend{
		byProject: map[string]struct {
			containers []backend.Container
			links      []backend.ContainerLink
		}{
			"SRCA": {
				containers: []backend.Container{
					{Key: "SRCA-TE-1", Kind: backend.KindTestExec, Summary: "matches"},
					{Key: "SRCA-TE-2", Kind: backend.KindTestExec, Summary: "no match"},
					{Key: "KNOWN-TE-1", Kind: backend.KindTestExec, Summary: "already known"},
					{Key: "SRCA-TS-1", Kind: backend.KindTestSet, Summary: "wrong kind"},
					{Key: "SHARED-TE-1", Kind: backend.KindTestExec, Summary: "in both sources"},
				},
				links: []backend.ContainerLink{
					{ContainerKey: "SRCA-TE-1", TestKey: "DEMO-1", RunStatus: "PASS"},  // ours -> kept
					{ContainerKey: "SRCA-TE-1", TestKey: "SRCA-99", RunStatus: "FAIL"}, // foreign -> dropped
					{ContainerKey: "SRCA-TE-2", TestKey: "SRCA-50", RunStatus: "PASS"}, // no ours -> exec ignored
					{ContainerKey: "KNOWN-TE-1", TestKey: "DEMO-2", RunStatus: "PASS"}, // ours but exec already known
					{ContainerKey: "SRCA-TS-1", TestKey: "DEMO-1", RunStatus: "PASS"},  // ours but not an execution
					{ContainerKey: "SHARED-TE-1", TestKey: "DEMO-2", RunStatus: "PASS"},
				},
			},
			"SRCB": {
				containers: []backend.Container{
					{Key: "SHARED-TE-1", Kind: backend.KindTestExec, Summary: "in both sources"},
				},
				links: []backend.ContainerLink{
					{ContainerKey: "SHARED-TE-1", TestKey: "DEMO-1", RunStatus: "PASS"},
				},
			},
		},
		runs: map[string][]backend.TestRun{
			"SRCA-TE-1":   {{TestKey: "DEMO-1", Status: "PASS"}},
			"SHARED-TE-1": {{TestKey: "DEMO-2", Status: "PASS"}},
		},
	}

	eng := New(mock, repo, WithCrossProjectSources([]string{"SRCA", "SRCB"}))
	known := map[string]bool{"KNOWN-TE-1": true}
	eng.discoverCrossProjectExecs(context.Background(), profileID, known, nil)

	// Only the two matching, not-already-known executions are stored.
	execs, err := repo.ListContainers(profileID, "testexec")
	if err != nil {
		t.Fatalf("list containers: %v", err)
	}
	got := map[string]bool{}
	for _, c := range execs {
		got[c.Key] = true
	}
	want := map[string]bool{"SRCA-TE-1": true, "SHARED-TE-1": true}
	if len(got) != len(want) {
		t.Fatalf("stored execs = %v, want exactly %v", got, want)
	}
	for k := range want {
		if !got[k] {
			t.Errorf("expected execution %s to be stored", k)
		}
	}

	// SRCA-TE-1 keeps only the matching member (DEMO-1), not the foreign SRCA-99.
	members, err := repo.GetExecutionMembersWithRuns(profileID, "SRCA-TE-1")
	if err != nil {
		t.Fatalf("members for SRCA-TE-1: %v", err)
	}
	if len(members) != 1 || members[0].TestKey != "DEMO-1" {
		t.Fatalf("SRCA-TE-1 members = %+v, want exactly [DEMO-1]", members)
	}

	// SHARED-TE-1 is stored once despite appearing in both SRCA and SRCB.
	sharedMembers, err := repo.GetExecutionMembersWithRuns(profileID, "SHARED-TE-1")
	if err != nil {
		t.Fatalf("members for SHARED-TE-1: %v", err)
	}
	if len(sharedMembers) != 1 || sharedMembers[0].TestKey != "DEMO-2" {
		t.Fatalf("SHARED-TE-1 members = %+v, want exactly [DEMO-2]", sharedMembers)
	}

	// Runs were fetched and stored for a discovered execution.
	runs, err := repo.RunsForTest(profileID, "DEMO-1")
	if err != nil {
		t.Fatalf("runs for DEMO-1: %v", err)
	}
	if len(runs) == 0 {
		t.Errorf("expected at least one stored run for DEMO-1 after discovery")
	}
}

// TestDiscoverCrossProjectExecsNoSources asserts that with no configured source
// projects the pass makes no backend calls and writes nothing. The container
// sync guards the call on len(crossProjectSources) > 0; the loop is likewise a
// no-op, so the two behave identically.
func TestDiscoverCrossProjectExecsNoSources(t *testing.T) {
	const profileID = "p1"
	repo := newDiscoveryRepo(t, profileID, []string{"DEMO-1"})
	mock := &discoveryMockBackend{}

	eng := New(mock, repo) // no WithCrossProjectSources
	eng.discoverCrossProjectExecs(context.Background(), profileID, map[string]bool{}, nil)

	if len(mock.listedFor) != 0 {
		t.Errorf("expected no ListContainers calls, got %v", mock.listedFor)
	}
	execs, err := repo.ListContainers(profileID, "testexec")
	if err != nil {
		t.Fatalf("list containers: %v", err)
	}
	if len(execs) != 0 {
		t.Errorf("expected no stored executions, got %d", len(execs))
	}
}
