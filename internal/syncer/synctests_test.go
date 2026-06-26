package syncer_test

import (
	"context"
	"path/filepath"
	"testing"

	"xray-test-manager/internal/jira"
	"xray-test-manager/internal/store"
	"xray-test-manager/internal/syncer"
	"xray-test-manager/internal/testrepo"
)

// TestSyncTests exercises the partial-sync path behind the Browse view's Sync
// button. It verifies that:
//   - Tests are populated in the local store after SyncTests.
//   - The sync watermark has NOT advanced (SetSyncState is not called by SyncTests).
func TestSyncTests(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	repo := testrepo.NewRepository(st)

	const (
		profileID  = "p1"
		projectKey = "DEMO"
	)

	// Confirm watermark is empty before any sync.
	stateBefore, err := repo.GetSyncState(profileID)
	if err != nil {
		t.Fatalf("GetSyncState before: %v", err)
	}
	if stateBefore.LastSyncedAt != "" {
		t.Fatalf("expected empty watermark before sync, got %q", stateBefore.LastSyncedAt)
	}

	eng := syncer.New(jira.NewClient("demo", "tok"), repo)
	if err := eng.SyncTests(context.Background(), profileID, projectKey, "", "", nil); err != nil {
		t.Fatalf("SyncTests: %v", err)
	}

	// Tests must be in the store.
	keys, err := repo.AllTestKeys(profileID)
	if err != nil {
		t.Fatalf("AllTestKeys: %v", err)
	}
	if len(keys) == 0 {
		t.Fatal("SyncTests did not populate any test_case rows")
	}

	// Watermark must NOT have advanced: SyncTests does not call SetSyncState.
	stateAfter, err := repo.GetSyncState(profileID)
	if err != nil {
		t.Fatalf("GetSyncState after: %v", err)
	}
	if stateAfter.LastSyncedAt != "" {
		t.Errorf("SyncTests must not advance the sync watermark, got %q", stateAfter.LastSyncedAt)
	}
}
