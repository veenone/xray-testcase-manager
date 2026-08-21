package syncer

import (
	"context"
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
