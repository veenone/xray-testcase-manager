package jira

import (
	"strings"
	"testing"
)

func TestIsDemoURLRecognisesDemoSchemes(t *testing.T) {
	cases := map[string]bool{
		"demo":                     true,
		"DEMO":                     true,
		"demo://anything":          true,
		"mock://local":             true,
		"  demo  ":                 true,
		"https://jira.example.com": false,
		"":                         false,
		"jira.demo.example.com":    false,
	}
	for url, want := range cases {
		if got := isDemoURL(url); got != want {
			t.Errorf("isDemoURL(%q) = %v, want %v", url, got, want)
		}
	}
}

func TestDemoTestsPageReportsTheFullTotal(t *testing.T) {
	_, total := demoTestsPage("QA", 0, 100)
	if total != demoTestCount {
		t.Errorf("total = %d, want %d", total, demoTestCount)
	}
}

func TestDemoTestsPagePaginates(t *testing.T) {
	first, _ := demoTestsPage("QA", 0, 100)
	second, _ := demoTestsPage("QA", 100, 100)

	if len(first) != 100 || len(second) != 100 {
		t.Fatalf("page sizes = %d / %d, want 100 / 100", len(first), len(second))
	}
	if first[0].Key == second[0].Key {
		t.Errorf("page boundary leaked: first[0]=%s second[0]=%s",
			first[0].Key, second[0].Key)
	}
}

func TestDemoTestsPageClampsBeyondTotal(t *testing.T) {
	page, _ := demoTestsPage("QA", demoTestCount-3, 100)
	if len(page) != 3 {
		t.Errorf("tail page size = %d, want 3", len(page))
	}
}

func TestMakeDemoTestIsDeterministic(t *testing.T) {
	a := makeDemoTest("QA", 42)
	b := makeDemoTest("QA", 42)
	if a.Summary != b.Summary || a.Status != b.Status || a.Key != b.Key {
		t.Errorf("makeDemoTest not deterministic: %+v vs %+v", a, b)
	}
}

func TestDemoStepsSeedDeterministicCallGraph(t *testing.T) {
	// The seeded callers (numbers 6, 8, 9) each expose a call step pointing at
	// a sibling Test in the SAME project. 6 and 8 share callee 7; 9 calls 10.
	cases := []struct {
		caller string
		callee string
	}{
		{"QA-6", "QA-7"},
		{"QA-8", "QA-7"},
		{"QA-9", "QA-10"},
		{"DEMO-6", "DEMO-7"},
	}
	for _, c := range cases {
		steps := demoStepsForKey(c.caller)
		var got string
		for _, s := range steps {
			if s.CalledTestKey != "" {
				got = s.CalledTestKey
				break
			}
		}
		if got != c.callee {
			t.Errorf("demoStepsForKey(%q) called %q, want %q", c.caller, got, c.callee)
		}
	}
}

func TestDemoStepsCallGraphAvoidsDuplicateClustersAndIsStable(t *testing.T) {
	// Duplicate-cluster keys (numbers 1..4, indices 0..3) must carry no call
	// step — the Duplicates feature depends on their fixed step content.
	for _, key := range []string{"QA-1", "QA-2", "QA-3", "QA-4"} {
		for _, s := range demoStepsForKey(key) {
			if s.CalledTestKey != "" {
				t.Errorf("%s should have no call step, got CalledTestKey=%q", key, s.CalledTestKey)
			}
		}
	}

	// Determinism: re-pulling returns identical steps. This is what makes a
	// SyncTestCalls re-pull stable (the graph is preserved, not wiped).
	for _, key := range []string{"QA-6", "QA-8", "QA-9", "QA-100"} {
		a := demoStepsForKey(key)
		b := demoStepsForKey(key)
		if len(a) != len(b) {
			t.Fatalf("demoStepsForKey(%q) length not stable: %d vs %d", key, len(a), len(b))
		}
		for i := range a {
			if a[i] != b[i] {
				t.Errorf("demoStepsForKey(%q)[%d] not deterministic: %+v vs %+v", key, i, a[i], b[i])
			}
		}
	}
}

func TestIncrementalSinceClauseEmptyReturnsEmpty(t *testing.T) {
	if got := incrementalSinceClause(""); got != "" {
		t.Errorf("empty input should yield empty clause, got %q", got)
	}
}

func TestIncrementalSinceClauseBuildsJQLWithHourBuffer(t *testing.T) {
	// 13:00 UTC minus the 1-hour buffer => 12:00 should appear in the clause.
	clause := incrementalSinceClause("2026-05-20T13:00:00Z")

	if !strings.HasPrefix(clause, `updated >= "`) {
		t.Errorf("missing JQL prefix in %q", clause)
	}
	if !strings.Contains(clause, "2026-05-20") {
		t.Errorf("date missing from clause %q", clause)
	}
	if !strings.Contains(clause, "12:00") {
		t.Errorf("1-hour buffer not applied: clause = %q", clause)
	}
}

func TestIncrementalSinceClauseToleratesBadInput(t *testing.T) {
	if got := incrementalSinceClause("not-a-time"); got != "" {
		t.Errorf("bad input should yield empty clause, got %q", got)
	}
}

// TestMakeDemoTestFixVersionsDeterministic asserts that FixVersions on demo
// Tests are deterministic (same index always returns the same slice) and that
// the first 30 tests include at least one test with a non-empty FixVersions
// slice and at least one with an empty one, so both paths are exercised.
func TestMakeDemoTestFixVersionsDeterministic(t *testing.T) {
	for i := range 10 {
		a := makeDemoTest("QA", i)
		b := makeDemoTest("QA", i)
		if len(a.FixVersions) != len(b.FixVersions) {
			t.Errorf("index %d: FixVersions not deterministic: %v vs %v", i, a.FixVersions, b.FixVersions)
			continue
		}
		for j := range a.FixVersions {
			if a.FixVersions[j] != b.FixVersions[j] {
				t.Errorf("index %d: FixVersions[%d] not deterministic: %q vs %q", i, j, a.FixVersions[j], b.FixVersions[j])
			}
		}
	}

	// The first 30 demo tests must include at least one with FixVersions and one
	// without, so both paths are exercised in the UI.
	hasVersioned, hasEmpty := false, false
	for i := range 30 {
		fv := makeDemoTest("QA", i).FixVersions
		if len(fv) > 0 {
			hasVersioned = true
		} else {
			hasEmpty = true
		}
	}
	if !hasVersioned {
		t.Error("no demo test in first 30 has a FixVersions entry")
	}
	if !hasEmpty {
		t.Error("no demo test in first 30 has empty FixVersions")
	}
}
