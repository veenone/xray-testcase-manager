package syncer

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"xray-test-manager/internal/backend"
	"xray-test-manager/internal/backend/xray"
	"xray-test-manager/internal/jira"
	"xray-test-manager/internal/store"
	"xray-test-manager/internal/testrepo"
)

// orderRecordingBackend wraps a fully working backend (the demo Xray adapter)
// and notes the order in which the sync stages first reach it. Every method
// delegates, so the sync behaves normally.
type orderRecordingBackend struct {
	backend.Backend
	mu    sync.Mutex
	order []string
}

func (b *orderRecordingBackend) note(stage string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range b.order {
		if s == stage {
			return // first touch only
		}
	}
	b.order = append(b.order, stage)
}

func (b *orderRecordingBackend) SearchTestsPage(
	ctx context.Context, projectKey, scopeJQL, since string, startAt, maxResults int,
) ([]backend.Test, int, error) {
	b.note("tests")
	return b.Backend.SearchTestsPage(ctx, projectKey, scopeJQL, since, startAt, maxResults)
}

func (b *orderRecordingBackend) FolderTree(ctx context.Context, projectKey string) (backend.FolderTreeResult, error) {
	b.note("folders")
	return b.Backend.FolderTree(ctx, projectKey)
}

func (b *orderRecordingBackend) ListPreconditionsStream(
	ctx context.Context,
	projectKey string,
	onProgress func(done, total int),
	onBatch func(pre []backend.Precondition, links map[string][]string) error,
) error {
	b.note("preconditions")
	return b.Backend.(backend.PreconditionStreamer).
		ListPreconditionsStream(ctx, projectKey, onProgress, onBatch)
}

// failingPreconditionBackend wraps the demo backend but fails the precondition
// stream, so the rest of the sync succeeds and only that stage is broken.
type failingPreconditionBackend struct {
	backend.Backend
}

func (b *failingPreconditionBackend) ListPreconditionsStream(
	ctx context.Context,
	projectKey string,
	onProgress func(done, total int),
	onBatch func(pre []backend.Precondition, links map[string][]string) error,
) error {
	return errors.New("context deadline exceeded")
}

// TestSyncReportsPartialWhenPreconditionStageFails is the -336 headline: the
// stage's error used to be logged and dropped, so the run stamped its watermark
// and reported success while the Preconditions view sat empty.
func TestSyncReportsPartialWhenPreconditionStageFails(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	repo := testrepo.NewRepository(st)

	e := New(&failingPreconditionBackend{Backend: xray.New(jira.NewClient("demo", "tok"))}, repo)
	syncErr := e.Sync(context.Background(), "p1", "DEMO", "", "", nil)
	if syncErr == nil {
		t.Fatal("sync reported clean despite the precondition stage failing")
	}

	var partial *PartialSyncError
	if !errors.As(syncErr, &partial) {
		t.Fatalf("got %T (%v), want a *PartialSyncError so the caller can record which stage failed", syncErr, syncErr)
	}
	if len(partial.StageFailures) != 1 || partial.StageFailures[0].Stage != "preconditions" {
		t.Fatalf("got %+v, want a preconditions stage failure", partial.StageFailures)
	}

	// The rest of the sync still ran: a partial run's data is usable.
	tests, err := repo.ListTests("p1", testrepo.Query{Limit: 5})
	if err != nil {
		t.Fatalf("list tests: %v", err)
	}
	if len(tests.Tests) == 0 {
		t.Error("no tests persisted; a partial sync must still keep what succeeded")
	}
}

// TestSyncIsCleanWhenPreconditionsSucceed guards against the new partial path
// firing on a healthy sync.
func TestSyncIsCleanWhenPreconditionsSucceed(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	repo := testrepo.NewRepository(st)

	e := New(xray.New(jira.NewClient("demo", "tok")), repo)
	if err := e.Sync(context.Background(), "p1", "DEMO", "", "", nil); err != nil {
		t.Fatalf("want a clean sync, got %v", err)
	}
}

// TestSyncRunsPreconditionsBeforeFolders pins the stage order. Preconditions
// are by far the longest stage and used to sit behind a full folder walk, so on
// a first sync they were the last thing to start and the first to be cut short
// (RND_P_4TFINT_05-336). They depend only on tests existing.
func TestSyncRunsPreconditionsBeforeFolders(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	repo := testrepo.NewRepository(st)

	rec := &orderRecordingBackend{Backend: xray.New(jira.NewClient("demo", "tok"))}
	e := New(rec, repo)

	if err := e.Sync(context.Background(), "p1", "DEMO", "", "", nil); err != nil {
		t.Fatalf("sync: %v", err)
	}

	idxOf := func(stage string) int {
		for i, s := range rec.order {
			if s == stage {
				return i
			}
		}
		t.Fatalf("stage %q never ran; got %v", stage, rec.order)
		return -1
	}
	if idxOf("tests") > idxOf("preconditions") {
		t.Errorf("preconditions ran before the test pull; got order %v", rec.order)
	}
	if idxOf("preconditions") > idxOf("folders") {
		t.Errorf("preconditions ran after folders; got order %v", rec.order)
	}
}
