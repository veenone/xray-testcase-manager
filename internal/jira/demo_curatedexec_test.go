package jira

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// execKeyForIndex returns the "<proj>-TE-<n>" key for a 0-based exec index.
func execKeyForIndex(projectKey string, execIdx int) string {
	return fmt.Sprintf("%s-TE-%d", projectKey, execIdx+1)
}

// testKeyForIndex returns the "<proj>-<n>" key for a 0-based test index.
func testKeyForIndex(projectKey string, testIndex int) string {
	return fmt.Sprintf("%s-%d", projectKey, testIndex+1)
}

// demoTestsByKey builds a key -> Test index over the first n demo tests so the
// assertions below can look up a member's ExecType and body.
func demoTestsByKey(t *testing.T, n int) map[string]Test {
	t.Helper()
	theme := themeFor("demo")
	byKey := make(map[string]Test, n)
	for i := 0; i < n; i++ {
		tc := makeDemoTest(theme, "DEMO", i)
		byKey[tc.Key] = tc
	}
	return byKey
}

// TestDemoCucumberExecIsTypeHomogeneous verifies that the curated all-Cucumber
// execution contains ONLY Cucumber tests. The generated membership loop cannot
// produce this (exec assignment is i%8, type is i%4, and 4 divides 8), so the
// execution is seeded past the generated range specifically to stay pure.
func TestDemoCucumberExecIsTypeHomogeneous(t *testing.T) {
	theme := themeFor("demo")
	_, links, err := demoContainersAndLinks(theme, "DEMO")
	if err != nil {
		t.Fatalf("demoContainersAndLinks: %v", err)
	}
	byKey := demoTestsByKey(t, 64)

	execKey := demoCucumberExecKey("DEMO")
	members := 0
	for _, l := range links {
		if l.ContainerKey != execKey {
			continue
		}
		members++
		tc, ok := byKey[l.TestKey]
		if !ok {
			t.Fatalf("%s member %s not among generated demo tests", execKey, l.TestKey)
		}
		if tc.ExecType != "Cucumber" {
			t.Errorf("%s member %s has ExecType %q, want Cucumber", execKey, l.TestKey, tc.ExecType)
		}
		if tc.CucumberScenario == "" {
			t.Errorf("%s member %s has an empty CucumberScenario", execKey, l.TestKey)
		}
		if l.RunStatus == "" {
			t.Errorf("%s member %s has an empty RunStatus", execKey, l.TestKey)
		}
	}
	if members == 0 {
		t.Fatalf("%s has no members", execKey)
	}
}

// TestDemoCucumberExecHasContainer verifies the curated execution is emitted as
// a real Container, not just referenced by links — otherwise it would never
// appear in the executions board.
func TestDemoCucumberExecHasContainer(t *testing.T) {
	theme := themeFor("demo")
	containers, _, err := demoContainersAndLinks(theme, "DEMO")
	if err != nil {
		t.Fatalf("demoContainersAndLinks: %v", err)
	}
	execKey := demoCucumberExecKey("DEMO")
	for _, c := range containers {
		if c.Key != execKey {
			continue
		}
		if c.Kind != KindTestExec {
			t.Errorf("%s Kind = %q, want %q", execKey, c.Kind, KindTestExec)
		}
		if c.Summary == "" || c.Status == "" {
			t.Errorf("%s has empty Summary (%q) or Status (%q)", execKey, c.Summary, c.Status)
		}
		return
	}
	t.Fatalf("no container emitted for %s", execKey)
}

// TestDemoCuratedMembersHaveRunRows is the regression guard for the defect this
// change fixes: demoContainersAndLinks and demoTestRuns derive execution
// membership separately, and the curated links were previously added to the
// link side only. Curated members then rendered with a run status but no
// executor, dates, defects or comment. Every curated member must now have a
// matching TestRun.
func TestDemoCuratedMembersHaveRunRows(t *testing.T) {
	c := NewClient("demo", "token")
	for _, ce := range demoCuratedExecLinks() {
		execKey := demoCucumberExecKey("DEMO")
		if ce.execIndex < demoExecCount {
			execKey = execKeyForIndex("DEMO", ce.execIndex)
		}
		runs, err := c.GetTestRuns(context.Background(), execKey)
		if err != nil {
			t.Fatalf("%s: %v", execKey, err)
		}
		byTest := make(map[string]TestRun, len(runs))
		for _, r := range runs {
			byTest[r.TestKey] = r
		}
		for _, m := range ce.members {
			testKey := testKeyForIndex("DEMO", m.testIndex)
			run, ok := byTest[testKey]
			if !ok {
				t.Errorf("%s: curated member %s has no run row", execKey, testKey)
				continue
			}
			if run.Status != m.runStatus {
				t.Errorf("%s/%s: run status = %q, want %q (must match the link)",
					execKey, testKey, run.Status, m.runStatus)
			}
			if run.ExecutedBy == "" || run.StartedAt == "" || run.FinishedAt == "" {
				t.Errorf("%s/%s: incomplete run row (executor=%q started=%q finished=%q)",
					execKey, testKey, run.ExecutedBy, run.StartedAt, run.FinishedAt)
			}
			if run.Status == "FAIL" && (len(run.Defects) == 0 || run.Comment == "") {
				t.Errorf("%s/%s: FAIL run missing defect (%v) or comment (%q)",
					execKey, testKey, run.Defects, run.Comment)
			}
		}
	}
}

// TestDemoCuratedLinksAndRunsAgree checks the two membership derivations agree
// on the curated executions: every curated link has a run row with the same
// status, and no run row contradicts a link.
func TestDemoCuratedLinksAndRunsAgree(t *testing.T) {
	theme := themeFor("demo")
	_, links, err := demoContainersAndLinks(theme, "DEMO")
	if err != nil {
		t.Fatalf("demoContainersAndLinks: %v", err)
	}
	c := NewClient("demo", "token")

	execKey := demoCucumberExecKey("DEMO")
	linkStatus := make(map[string]string)
	for _, l := range links {
		if l.ContainerKey == execKey {
			linkStatus[l.TestKey] = l.RunStatus
		}
	}
	runs, err := c.GetTestRuns(context.Background(), execKey)
	if err != nil {
		t.Fatalf("%s: %v", execKey, err)
	}
	if len(runs) != len(linkStatus) {
		t.Errorf("%s: %d run rows but %d link members", execKey, len(runs), len(linkStatus))
	}
	for _, r := range runs {
		want, ok := linkStatus[r.TestKey]
		if !ok {
			t.Errorf("%s: run row for %s has no matching link", execKey, r.TestKey)
			continue
		}
		if r.Status != want {
			t.Errorf("%s/%s: run status %q != link status %q", execKey, r.TestKey, r.Status, want)
		}
	}
}

// TestDemoCucumberScenarioOutlineIsReachable guards the second defect fixed
// here: the Scenario Outline branch was gated on i%8==0, which implies i%4==0
// (Manual), so no Cucumber test could reach it and the Examples-table body was
// dead code. Both Cucumber body shapes must now occur.
func TestDemoCucumberScenarioOutlineIsReachable(t *testing.T) {
	theme := themeFor("demo")
	var sawScenario, sawOutline bool
	for i := 0; i < 64; i++ {
		tc := makeDemoTest(theme, "DEMO", i)
		if tc.ExecType != "Cucumber" {
			continue
		}
		switch tc.CucumberType {
		case "Scenario":
			sawScenario = true
		case "Scenario Outline":
			sawOutline = true
			if !strings.Contains(tc.CucumberScenario, "Examples:") {
				t.Errorf("%s is a Scenario Outline but has no Examples table:\n%s",
					tc.Key, tc.CucumberScenario)
			}
		default:
			t.Errorf("%s has unexpected CucumberType %q", tc.Key, tc.CucumberType)
		}
	}
	if !sawScenario {
		t.Error("no demo Cucumber test with a plain Scenario body")
	}
	if !sawOutline {
		t.Error("no demo Cucumber test with a Scenario Outline body (branch still unreachable)")
	}
}
