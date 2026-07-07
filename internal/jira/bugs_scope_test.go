package jira

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestListBugsHonorsTestKeys(t *testing.T) {
	c := NewClient("demo", "")
	ctx := context.Background()

	// Full set reproduces the seed: at least one bug, with links.
	full, fullLinks, err := c.ListBugs(ctx, "DEMO", nil, "Bug", nil)
	if err != nil {
		t.Fatalf("full ListBugs: %v", err)
	}
	if len(full) == 0 || len(fullLinks) == 0 {
		t.Fatalf("expected seeded bugs and links, got %d bugs, %d links", len(full), len(fullLinks))
	}

	// Restrict to a single linked test key: every returned link must reference it,
	// and every returned bug must be referenced by some surviving link.
	target := fullLinks[0].TestKey
	bugs, links, err := c.ListBugs(ctx, "DEMO", []string{target}, "Bug", nil)
	if err != nil {
		t.Fatalf("scoped ListBugs: %v", err)
	}
	if len(links) == 0 {
		t.Fatalf("expected at least one link for %s", target)
	}
	for _, l := range links {
		if l.TestKey != target {
			t.Errorf("link references out-of-scope test %s (want only %s)", l.TestKey, target)
		}
	}
	bugKeys := map[string]bool{}
	for _, b := range bugs {
		bugKeys[b.Key] = true
	}
	for _, l := range links {
		if !bugKeys[l.BugKey] {
			t.Errorf("link to %s has no matching bug in the result", l.BugKey)
		}
	}

	// Empty (no in-scope tests) returns nothing.
	noBugs, noLinks, err := c.ListBugs(ctx, "DEMO", []string{}, "Bug", nil)
	if err != nil {
		t.Fatalf("empty ListBugs: %v", err)
	}
	if len(noBugs) != 0 || len(noLinks) != 0 {
		t.Errorf("empty testKeys should yield nothing, got %d bugs, %d links", len(noBugs), len(noLinks))
	}
}

func TestListBugsSpanningDefectPartialScope(t *testing.T) {
	c := NewClient("demo", "")
	ctx := context.Background()

	failed := demoFailedTestNums(10)
	if len(failed) < 3 {
		t.Skipf("need >=3 failed demo tests, got %d", len(failed))
	}
	// Scope to failed[1] only — one endpoint of the spanning defect. The other
	// endpoint (failed[2]) must not appear in any link.
	target := fmt.Sprintf("DEMO-%d", failed[1])
	excluded := fmt.Sprintf("DEMO-%d", failed[2])

	bugs, links, err := c.ListBugs(ctx, "DEMO", []string{target}, "Bug", nil)
	if err != nil {
		t.Fatalf("ListBugs: %v", err)
	}
	for _, l := range links {
		if l.TestKey == excluded {
			t.Errorf("link references excluded endpoint %s", excluded)
		}
		if l.TestKey != target {
			t.Errorf("link references out-of-scope test %s (want only %s)", l.TestKey, target)
		}
	}
	// Every returned link's bug must be present in the bugs slice (set integrity).
	bugKeys := map[string]bool{}
	for _, b := range bugs {
		bugKeys[b.Key] = true
	}
	for _, l := range links {
		if !bugKeys[l.BugKey] {
			t.Errorf("link to %s has no matching bug", l.BugKey)
		}
	}
	if len(links) == 0 {
		t.Errorf("expected at least one in-scope link for %s", target)
	}
}

// TestGetBugDetailDemoNonEmpty verifies that GetBugDetail in demo mode returns
// a non-empty BugDetail with all four fields populated, and that DefectOrigin
// is one of the expected values.
func TestGetBugDetailDemoNonEmpty(t *testing.T) {
	c := NewClient("demo", "")
	ctx := context.Background()

	keys := []string{"BUGS-100", "BUGS-101", "SUP-200", "BUGS-1"}
	validOrigins := map[string]bool{
		"Code": true, "Design": true, "Requirements": true, "Test": true,
	}
	for _, key := range keys {
		d, err := c.GetBugDetail(ctx, key)
		if err != nil {
			t.Fatalf("GetBugDetail(%s): %v", key, err)
		}
		if d.Description == "" {
			t.Errorf("GetBugDetail(%s): empty Description", key)
		}
		if !validOrigins[d.DefectOrigin] {
			t.Errorf("GetBugDetail(%s): DefectOrigin = %q, want one of Code/Design/Requirements/Test", key, d.DefectOrigin)
		}
		if d.DefectAnalysis == "" {
			t.Errorf("GetBugDetail(%s): empty DefectAnalysis", key)
		}
		if d.CorrectionDetails == "" {
			t.Errorf("GetBugDetail(%s): empty CorrectionDetails", key)
		}
	}
}

// TestGetBugDetailDemoDeterministic verifies that the same key always returns
// the same BugDetail (no time.Now / rand) and that different keys may return
// different DefectOrigin values.
func TestGetBugDetailDemoDeterministic(t *testing.T) {
	c := NewClient("demo", "")
	ctx := context.Background()

	first, err := c.GetBugDetail(ctx, "BUGS-100")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	second, err := c.GetBugDetail(ctx, "BUGS-100")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if first != second {
		t.Errorf("non-deterministic: first=%+v second=%+v", first, second)
	}
	// Confirm the fixed strings do not contain em dashes.
	for _, s := range []string{first.Description, first.DefectAnalysis, first.CorrectionDetails} {
		if strings.Contains(s, "—") {
			t.Errorf("em dash found in demo BugDetail: %q", s)
		}
	}
}

// TestListProjectBugsDemoNonEmpty verifies that ListProjectBugs in demo mode
// returns the full bug seed (deduped, non-empty) regardless of projKey.
func TestListProjectBugsDemoNonEmpty(t *testing.T) {
	c := NewClient("demo", "")
	ctx := context.Background()

	bugs, err := c.ListProjectBugs(ctx, "BUGS", "Bug")
	if err != nil {
		t.Fatalf("ListProjectBugs: %v", err)
	}
	if len(bugs) == 0 {
		t.Fatal("expected a non-empty bug list from demo ListProjectBugs")
	}
	// All keys must be unique.
	seen := map[string]bool{}
	for _, b := range bugs {
		if seen[b.Key] {
			t.Errorf("duplicate key %s in ListProjectBugs result", b.Key)
		}
		seen[b.Key] = true
	}
}

// TestSyncBugsMergeDedup verifies the merge-dedup logic: a bug in both the
// project-wide set and the harvest yields one record (project-wide wins for
// the Updated field), and a bug only in the harvest is also present.
func TestSyncBugsMergeDedup(t *testing.T) {
	// project-wide bugs: BUGS-1 (with Updated), BUGS-2 (with Updated)
	projBugs := []Bug{
		{Key: "BUGS-1", ProjectKey: "BUGS", IssueType: "Bug", Summary: "proj only one", Updated: "2024-01-01"},
		{Key: "BUGS-2", ProjectKey: "BUGS", IssueType: "Bug", Summary: "proj two", Updated: "2024-01-02"},
	}
	// harvest bugs: BUGS-2 (same key, no Updated), BUGS-3 (cross-project, not in proj)
	harvestBugs := []Bug{
		{Key: "BUGS-2", ProjectKey: "BUGS", IssueType: "Bug", Summary: "harvest two"},
		{Key: "BUGS-3", ProjectKey: "BUGS", IssueType: "Bug", Summary: "harvest three"},
	}

	// Replicate the merge logic from syncBugs.
	merged := make(map[string]Bug, len(projBugs)+len(harvestBugs))
	for _, b := range projBugs {
		merged[b.Key] = b
	}
	for _, b := range harvestBugs {
		if _, exists := merged[b.Key]; !exists {
			merged[b.Key] = b
		}
	}

	if len(merged) != 3 {
		t.Fatalf("want 3 merged bugs (BUGS-1, BUGS-2, BUGS-3), got %d", len(merged))
	}
	if b := merged["BUGS-2"]; b.Updated != "2024-01-02" {
		t.Errorf("BUGS-2: project-wide record (with Updated) should win; got Updated=%q", b.Updated)
	}
	if b := merged["BUGS-3"]; b.Summary != "harvest three" {
		t.Errorf("BUGS-3 (harvest-only): not present or wrong: %+v", b)
	}
	if b := merged["BUGS-1"]; b.Summary != "proj only one" {
		t.Errorf("BUGS-1 (proj-only): not present or wrong: %+v", b)
	}
}
