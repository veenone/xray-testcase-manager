package testrepo

import (
	"strings"
	"testing"
)

func TestGetSubTaskTraceability(t *testing.T) {
	r := newTestRepo(t) // shared helper in sankey_crossproject_test.go
	const p = "p1"

	// Two sub-task execs under one parent, one standalone exec (excluded).
	seedContainer(t, r, p, "DEMO-STE-1", "testexec", "Sub 1", "Open")
	seedContainer(t, r, p, "DEMO-STE-2", "testexec", "Sub 2", "Open")
	seedContainer(t, r, p, "DEMO-TE-9", "testexec", "Standalone", "Open")
	setContainerParent(t, r, p, "DEMO-STE-1", "DEMO-S-1")
	setContainerParent(t, r, p, "DEMO-STE-2", "DEMO-S-1")
	// DEMO-TE-9 keeps parent_key = '' (standalone).

	seedContainerTest(t, r, p, "DEMO-STE-1", "DEMO-1", "PASS")
	seedContainerTest(t, r, p, "DEMO-STE-1", "DEMO-2", "FAIL")
	seedContainerTest(t, r, p, "DEMO-STE-2", "DEMO-3", "PASS")
	seedContainerTest(t, r, p, "DEMO-TE-9", "DEMO-4", "PASS") // standalone, excluded

	sk, err := r.GetSubTaskTraceability(p, nil, true)
	if err != nil {
		t.Fatalf("GetSubTaskTraceability: %v", err)
	}

	// 3 memberships under sub-task execs (the standalone one is excluded).
	sumLayer := func(layer int) int {
		n := 0
		for _, nd := range sk.Nodes {
			if nd.Layer == layer {
				n += nd.Value
			}
		}
		return n
	}
	if sumLayer(0) != 3 || sumLayer(1) != 3 || sumLayer(2) != 3 {
		t.Fatalf("layers should each total 3 memberships, got %d/%d/%d", sumLayer(0), sumLayer(1), sumLayer(2))
	}
	if !hasNode(sk, "parent:DEMO-S-1") {
		t.Errorf("missing parent node")
	}
	if hasNode(sk, "exec:DEMO-TE-9") {
		t.Errorf("standalone execution must be excluded")
	}

	// Parent filter to a non-existent parent yields an empty (not error) result.
	empty, err := r.GetSubTaskTraceability(p, []string{"NOPE-1"}, true)
	if err != nil {
		t.Fatalf("filtered: %v", err)
	}
	if len(empty.Nodes) != 0 {
		t.Errorf("unknown parent filter should yield no nodes, got %d", len(empty.Nodes))
	}
}

// TestSubTaskTraceabilityIncludesExternalWhenEnabled verifies that a sub-task
// execution whose only member Test lives in another project (cached in
// external_test, absent from test_case) is drawn in the flow when crossProject
// is true and excluded when it is false (the legacy, project-scoped behavior).
func TestSubTaskTraceabilityIncludesExternalWhenEnabled(t *testing.T) {
	r := newTestRepo(t)
	const p = "p1"

	// A sub-task execution under a parent, whose single member (OTHER-9) is a
	// cross-project Test: it has no test_case row, only an external_test cache
	// entry, exactly as the sync's missing-keys pass records it.
	seedContainer(t, r, p, "DEMO-STE-X", "testexec", "Cross sub", "Open")
	setContainerParent(t, r, p, "DEMO-STE-X", "DEMO-S-9")
	seedContainerTest(t, r, p, "DEMO-STE-X", "OTHER-9", "PASS")
	if err := r.ReplaceExternalTests(p, []ExternalTest{
		{Key: "OTHER-9", Summary: "Foreign test", Status: "Approved", ProjectKey: "OTHER"},
	}); err != nil {
		t.Fatalf("seed external: %v", err)
	}

	// With cross-project members included, the external member's sub-task
	// execution is drawn (its parent, exec and status nodes all appear).
	on, err := r.GetSubTaskTraceability(p, nil, true)
	if err != nil {
		t.Fatalf("crossProject=true: %v", err)
	}
	if !hasNode(on, "exec:DEMO-STE-X") {
		t.Errorf("crossProject=true should draw the sub-task exec of the external member: %+v", on.Nodes)
	}
	if !hasNode(on, "parent:DEMO-S-9") {
		t.Errorf("crossProject=true should draw the external member's parent node")
	}

	// With cross-project members excluded, the external-only membership is
	// dropped; its sub-task execution has no remaining members and disappears.
	off, err := r.GetSubTaskTraceability(p, nil, false)
	if err != nil {
		t.Fatalf("crossProject=false: %v", err)
	}
	if hasNode(off, "exec:DEMO-STE-X") {
		t.Errorf("crossProject=false should exclude the external-only sub-task exec: %+v", off.Nodes)
	}
	if len(off.Nodes) != 0 {
		t.Errorf("crossProject=false should yield an empty flow (only member was external), got %d nodes", len(off.Nodes))
	}
}

// setContainerParent sets parent_key on a seeded container.
func setContainerParent(t *testing.T, r *Repository, profileID, key, parent string) {
	t.Helper()
	if _, err := r.db.Exec(
		`UPDATE test_container SET parent_key = ? WHERE profile_id = ? AND jira_key = ?`,
		parent, profileID, key); err != nil {
		t.Fatalf("set parent on %s: %v", key, err)
	}
}

// setContainerParentSummary sets parent_summary on a seeded container.
func setContainerParentSummary(t *testing.T, r *Repository, profileID, key, summary string) {
	t.Helper()
	if _, err := r.db.Exec(
		`UPDATE test_container SET parent_summary = ? WHERE profile_id = ? AND jira_key = ?`,
		summary, profileID, key); err != nil {
		t.Fatalf("set parent_summary on %s: %v", key, err)
	}
}

func TestSubTaskParentNodeIncludesSummary(t *testing.T) {
	r := newTestRepo(t)
	const p = "p1"

	seedContainer(t, r, p, "DEMO-STE-1", "testexec", "Sub 1", "Open")
	setContainerParent(t, r, p, "DEMO-STE-1", "DEMO-S-1")
	setContainerParentSummary(t, r, p, "DEMO-STE-1", "Story One")
	seedContainerTest(t, r, p, "DEMO-STE-1", "DEMO-1", "PASS")

	sk, err := r.GetSubTaskTraceability(p, nil, true)
	if err != nil {
		t.Fatalf("GetSubTaskTraceability: %v", err)
	}

	// The parent node label should contain the summary text.
	var parentLabel string
	for _, n := range sk.Nodes {
		if n.ID == "parent:DEMO-S-1" {
			parentLabel = n.Label
			break
		}
	}
	if parentLabel == "" {
		t.Fatalf("parent node not found; nodes: %+v", sk.Nodes)
	}
	if !strings.Contains(parentLabel, "Story One") {
		t.Errorf("parent node label %q should contain the summary %q", parentLabel, "Story One")
	}
	if !strings.HasPrefix(parentLabel, "DEMO-S-1") {
		t.Errorf("parent node label %q should start with the key %q", parentLabel, "DEMO-S-1")
	}
}
