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

// TestCommitContainerEnvClearsInDemo verifies a container_env pending change
// (the Test Environments set for an execution) commits as a no-op success
// against a demo client and is cleared from the pending journal.
func TestCommitContainerEnvClearsInDemo(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	repo := testrepo.NewRepository(st)

	if err := repo.UpsertContainers("p1", []testrepo.Container{
		{Key: "DEMO-TE-1", Kind: "testexec", Summary: "Cycle", Status: "Open"},
	}); err != nil {
		t.Fatalf("seed container: %v", err)
	}
	if err := repo.SetContainerEnvironments("p1", "DEMO-TE-1", []string{"Staging", "Chrome"}); err != nil {
		t.Fatalf("set environments: %v", err)
	}

	res, err := syncer.New(jira.NewClient("demo", "tok"), repo).
		CommitChanges(context.Background(), "p1", "DEMO")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("commit failed: %+v", res.Failed)
	}
	pending, err := repo.ListPendingChanges("p1")
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	for _, c := range pending {
		if c.EntityType == "container_env" {
			t.Fatalf("container_env pending change not cleared after demo commit: %+v", c)
		}
	}
}
