package jira

import (
	"fmt"
	"strings"
	"testing"
)

// TestDemoExecutionHasCucumberAndGenericMembers verifies that at least one
// demo Test Execution's membership visibly includes both a Cucumber test and
// a Generic test, each carrying a non-empty run status. The main member-links
// loop in demoContainersAndLinks assigns execution by index%execCount (8) and
// test type by index%4 (demoExecTypeForIndex); since 4 divides 8, every test
// landing in a given execution via that loop always shares the same type, so
// the mix is otherwise never guaranteed. This test pins down the curated
// showcase addition that makes it explicit.
func TestDemoExecutionHasCucumberAndGenericMembers(t *testing.T) {
	theme := themeFor("demo")
	_, links, err := demoContainersAndLinks(theme, "DEMO")
	if err != nil {
		t.Fatal(err)
	}

	// testType classifies a demo Test key ("DEMO-<n>") using the same
	// index-based cycle makeDemoTest uses for ExecType: 0-based index n-1
	// fed through demoExecTypeForIndex.
	testType := func(testKey string) string {
		var n int
		if _, err := fmt.Sscanf(testKey, "DEMO-%d", &n); err != nil || n < 1 {
			return ""
		}
		return demoExecTypeForIndex(n - 1)
	}

	// Group non-empty-status members by execution container (kind/key
	// contains "-TE-") and the demo test type they carry.
	membersByExec := map[string]map[string]bool{}
	for _, l := range links {
		if !strings.Contains(l.ContainerKey, "-TE-") || l.RunStatus == "" {
			continue
		}
		tt := testType(l.TestKey)
		if tt == "" {
			continue
		}
		if membersByExec[l.ContainerKey] == nil {
			membersByExec[l.ContainerKey] = map[string]bool{}
		}
		membersByExec[l.ContainerKey][tt] = true
	}

	for exec, types := range membersByExec {
		if types["Cucumber"] && types["Generic"] {
			t.Logf("execution %s has both Cucumber and Generic members with a run status", exec)
			return
		}
	}
	t.Fatal("expected some demo Test Execution to have both a Cucumber and a Generic member with a non-empty RunStatus")
}
