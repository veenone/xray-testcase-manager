package syncer

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"xray-test-manager/internal/backend"
	"xray-test-manager/internal/store"
	"xray-test-manager/internal/testrepo"
)

// newPreconditionTestRepo opens an empty store with one Test to hang links off.
func newPreconditionTestRepo(t *testing.T) *testrepo.Repository {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	repo := testrepo.NewRepository(st)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "Login"},
	}); err != nil {
		t.Fatalf("seed test: %v", err)
	}
	return repo
}

// batchOf builds a batch of n preconditions with keys prefixed by the batch id.
func batchOf(id string, n int) []backend.Precondition {
	out := make([]backend.Precondition, n)
	for i := range out {
		out[i] = backend.Precondition{Key: fmt.Sprintf("PC-%s-%d", id, i+1), Summary: "s"}
	}
	return out
}

// streamingPreBackend implements both Backend (nil, so stray calls panic) and
// PreconditionStreamer. It emits the configured batches in order and can fail
// partway, which is the -336 scenario.
type streamingPreBackend struct {
	backend.Backend
	batches [][]backend.Precondition
	failAt  int // batch index to fail before; -1 never fails
}

func (f *streamingPreBackend) ListPreconditionsStream(
	ctx context.Context,
	projectKey string,
	onProgress func(done, total int),
	onBatch func(pre []backend.Precondition, links map[string][]string) error,
) error {
	for i, b := range f.batches {
		if i == f.failAt {
			return errors.New("association read failed")
		}
		links := map[string][]string{}
		for _, p := range b {
			links["QA-1"] = append(links["QA-1"], p.Key)
		}
		if err := onBatch(b, links); err != nil {
			return err
		}
	}
	return nil
}

// oneShotPreBackend implements only Backend, so the engine's type assertion for
// PreconditionStreamer must fail and the fallback path must run. This is the
// Kiwi shape.
type oneShotPreBackend struct {
	backend.Backend
	pre   []backend.Precondition
	links map[string][]string
	calls int
}

func (f *oneShotPreBackend) ListPreconditions(
	ctx context.Context, projectKey string, onProgress func(done, total int),
) ([]backend.Precondition, map[string][]string, error) {
	f.calls++
	return f.pre, f.links, nil
}

// TestSyncPreconditionsPersistsEachBatch is the -336 regression at the engine
// level: a stream that delivers two batches then fails must leave both of them
// in the store, instead of the old all-or-nothing buffer that saved nothing.
func TestSyncPreconditionsPersistsEachBatch(t *testing.T) {
	repo := newPreconditionTestRepo(t)
	b := &streamingPreBackend{
		batches: [][]backend.Precondition{batchOf("a", 2), batchOf("b", 2), batchOf("c", 2)},
		failAt:  2,
	}
	e := New(b, repo)

	if err := e.syncPreconditions(context.Background(), "p1", "QA", nil); err == nil {
		t.Fatal("want the stage to report the failure")
	}

	got, err := repo.ListAllPreconditions("p1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d preconditions, want the 4 from the two batches before the failure", len(got))
	}
	links, err := repo.ListTestPreconditions("p1", "QA-1")
	if err != nil {
		t.Fatalf("list links: %v", err)
	}
	if len(links) != 4 {
		t.Errorf("got %d links, want 4 persisted before the failure", len(links))
	}
}

// TestSyncPreconditionsSkipsSweepOnFailure guards the other half of the fix: a
// pass that dies partway must not sweep, or it deletes links it never reached.
func TestSyncPreconditionsSkipsSweepOnFailure(t *testing.T) {
	repo := newPreconditionTestRepo(t)
	// A link from an earlier successful sync, at an older generation.
	if err := repo.UpsertPreconditions("p1", []testrepo.Precondition{
		{Key: "PC-OLD", Summary: "From a previous run"},
	}); err != nil {
		t.Fatalf("seed precondition: %v", err)
	}
	if err := repo.MarkTestPreconditions("p1", 1, map[string][]string{"QA-1": {"PC-OLD"}}); err != nil {
		t.Fatalf("seed link: %v", err)
	}

	b := &streamingPreBackend{
		batches: [][]backend.Precondition{batchOf("a", 1), batchOf("b", 1)},
		failAt:  1,
	}
	e := New(b, repo)
	if err := e.syncPreconditions(context.Background(), "p1", "QA", nil); err == nil {
		t.Fatal("want the stage to report the failure")
	}

	links, err := repo.ListTestPreconditions("p1", "QA-1")
	if err != nil {
		t.Fatalf("list links: %v", err)
	}
	var kept bool
	for _, l := range links {
		if l.Key == "PC-OLD" {
			kept = true
		}
	}
	if !kept {
		t.Error("the pre-existing link was swept after a partial pass; the sweep must be skipped")
	}
}

// TestSyncPreconditionsSweepsAfterCleanPass checks the sweep still happens when
// the pass completes, so links removed in Jira really do disappear.
func TestSyncPreconditionsSweepsAfterCleanPass(t *testing.T) {
	repo := newPreconditionTestRepo(t)
	if err := repo.UpsertPreconditions("p1", []testrepo.Precondition{
		{Key: "PC-GONE", Summary: "Unlinked in Jira"},
	}); err != nil {
		t.Fatalf("seed precondition: %v", err)
	}
	if err := repo.MarkTestPreconditions("p1", 1, map[string][]string{"QA-1": {"PC-GONE"}}); err != nil {
		t.Fatalf("seed link: %v", err)
	}

	b := &streamingPreBackend{batches: [][]backend.Precondition{batchOf("a", 1)}, failAt: -1}
	e := New(b, repo)
	if err := e.syncPreconditions(context.Background(), "p1", "QA", nil); err != nil {
		t.Fatalf("sync preconditions: %v", err)
	}

	links, err := repo.ListTestPreconditions("p1", "QA-1")
	if err != nil {
		t.Fatalf("list links: %v", err)
	}
	for _, l := range links {
		if l.Key == "PC-GONE" {
			t.Error("a stale link survived a clean pass; the sweep must run")
		}
	}
}

// TestSyncPreconditionsFallsBackForNonStreamingBackend checks Kiwi, which does
// not implement PreconditionStreamer, still syncs through the one-shot path.
func TestSyncPreconditionsFallsBackForNonStreamingBackend(t *testing.T) {
	repo := newPreconditionTestRepo(t)
	b := &oneShotPreBackend{
		pre:   []backend.Precondition{{Key: "PC-1", Summary: "One"}},
		links: map[string][]string{"QA-1": {"PC-1"}},
	}
	e := New(b, repo)

	if err := e.syncPreconditions(context.Background(), "p1", "QA", nil); err != nil {
		t.Fatalf("sync preconditions: %v", err)
	}
	if b.calls != 1 {
		t.Errorf("ListPreconditions called %d times, want 1", b.calls)
	}
	got, err := repo.ListAllPreconditions("p1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d preconditions, want 1 through the fallback path", len(got))
	}
}

// TestSyncPreconditionsNoTypeSkipsSweep covers the benign case the removed
// "empty result" guard used to cover: an instance with no Precondition issue
// type streams nothing and returns nil, which must not sweep existing links.
func TestSyncPreconditionsNoTypeSkipsSweep(t *testing.T) {
	repo := newPreconditionTestRepo(t)
	if err := repo.UpsertPreconditions("p1", []testrepo.Precondition{
		{Key: "PC-KEEP", Summary: "Existing"},
	}); err != nil {
		t.Fatalf("seed precondition: %v", err)
	}
	if err := repo.MarkTestPreconditions("p1", 1, map[string][]string{"QA-1": {"PC-KEEP"}}); err != nil {
		t.Fatalf("seed link: %v", err)
	}

	// No batches at all, and no error: the no-Precondition-type shape.
	b := &streamingPreBackend{batches: nil, failAt: -1}
	e := New(b, repo)
	if err := e.syncPreconditions(context.Background(), "p1", "QA", nil); err != nil {
		t.Fatalf("sync preconditions: %v", err)
	}

	links, err := repo.ListTestPreconditions("p1", "QA-1")
	if err != nil {
		t.Fatalf("list links: %v", err)
	}
	if len(links) != 1 {
		t.Errorf("got %d links, want the existing link kept when the stage is a benign skip", len(links))
	}
}
