package syncer_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"xray-test-manager/internal/jira"
	"xray-test-manager/internal/store"
	"xray-test-manager/internal/syncer"
	"xray-test-manager/internal/testrepo"
)

// TestSyncHarvestsCrossProjectBugForExecution exercises the full demo sync and
// asserts that the demo cross-project execution (*-TE-XPROJ) surfaces a bug that
// is reachable only through its foreign member Test (#219). It also confirms the
// normal in-project bugs are still present (the additive harvest must not clobber
// the wipe-and-replace bug sync).
func TestSyncHarvestsCrossProjectBugForExecution(t *testing.T) {
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
	eng := syncer.New(jira.NewClient("demo", "tok"), repo)
	if err := eng.Sync(context.Background(), profileID, projectKey, "", "", nil); err != nil {
		t.Fatalf("sync: %v", err)
	}

	xprojExec := projectKey + "-TE-XPROJ"
	bugs, err := repo.ListBugsForContainer(profileID, xprojExec)
	if err != nil {
		t.Fatalf("list bugs for container: %v", err)
	}
	if len(bugs) == 0 {
		t.Fatalf("cross-project execution %s should surface at least one bug reached via its foreign member", xprojExec)
	}

	// The normal in-project bug harvest must still be intact.
	all, err := repo.ListBugsWithTests(profileID)
	if err != nil {
		t.Fatalf("list all bugs: %v", err)
	}
	if len(all) < len(bugs) {
		t.Fatalf("all bugs (%d) should include the cross-project bugs (%d) plus the normal ones", len(all), len(bugs))
	}

	// The harvested bug's per-bug test list should include the foreign member.
	var foundForeign bool
	for _, b := range bugs {
		tests, err := repo.ListTestsForBug(profileID, b.Key)
		if err != nil {
			t.Fatalf("list tests for bug %s: %v", b.Key, err)
		}
		for _, bt := range tests {
			if !strings.HasPrefix(bt.Key, projectKey+"-") {
				foundForeign = true
			}
		}
	}
	if !foundForeign {
		t.Errorf("expected a harvested bug whose per-bug test list includes a foreign (non-%s) member", projectKey)
	}
}
