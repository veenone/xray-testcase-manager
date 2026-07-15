package syncer_test

import (
	"context"
	"path/filepath"
	"testing"

	"xray-test-manager/internal/backend/xray"
	"xray-test-manager/internal/jira"
	"xray-test-manager/internal/store"
	"xray-test-manager/internal/syncer"
	"xray-test-manager/internal/testrepo"
)

// TestSyncStoresTestRunsAndExecPlans runs a full demo sync and confirms that:
// - test_run rows are stored for a test that belongs to a demo execution, and
// - exec_plan rows are stored for that same execution.
//
// The demo generator seeds DEMO-TE-1 (execIdx 0) with runs for DEMO-1,
// DEMO-9, etc. (every 8th test). DEMO-TE-1 is also linked to two demo plans.
func TestSyncStoresTestRunsAndExecPlans(t *testing.T) {
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
	eng := syncer.New(xray.New(jira.NewClient("demo", "tok")), repo)
	if err := eng.Sync(context.Background(), profileID, projectKey, "", "", nil); err != nil {
		t.Fatalf("sync: %v", err)
	}

	// DEMO-1 is in exec DEMO-TE-1 (execIdx=0, testIdx=0 in the demo run seed).
	const testKey = "DEMO-1"
	runs, err := repo.RunsForTest(profileID, testKey)
	if err != nil {
		t.Fatalf("RunsForTest: %v", err)
	}
	if len(runs) == 0 {
		t.Fatalf("RunsForTest(%q) returned no rows; expected at least one after container sync", testKey)
	}
	for _, r := range runs {
		if r.RunStatus == "" {
			t.Errorf("run for %s in %s has empty RunStatus", testKey, r.ExecKey)
		}
	}

	// DEMO-TE-1 should have exec_plan links (demoExecPlans seeds two plans per exec).
	const execKey = "DEMO-TE-1"
	plans, err := repo.ExecPlansForExec(profileID, execKey)
	if err != nil {
		t.Fatalf("ExecPlansForExec: %v", err)
	}
	if len(plans) == 0 {
		t.Fatalf("ExecPlansForExec(%q) returned no rows; expected at least one after container sync", execKey)
	}
}
