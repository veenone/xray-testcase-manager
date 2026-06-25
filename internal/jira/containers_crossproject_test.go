package jira

import (
	"context"
	"testing"
)

// TestTestExecutionsForTestDemoMember verifies that a DEMO test at a member
// index (i%11 == 0, so DEMO-1 at index 0, DEMO-12 at index 11, etc.) returns
// the cross-project sub-task execution and a link with a run status.
func TestTestExecutionsForTestDemoMember(t *testing.T) {
	c := NewClient("demo", "token")

	// DEMO-1 is at index 0, which satisfies i%11 == 0.
	containers, links, err := c.TestExecutionsForTest(context.Background(), "DEMO-1")
	if err != nil {
		t.Fatalf("TestExecutionsForTest: %v", err)
	}
	if len(containers) != 1 {
		t.Fatalf("want 1 container, got %d", len(containers))
	}
	if containers[0].Key != demoCrossProjectSubExecKey {
		t.Errorf("Key = %q, want %q", containers[0].Key, demoCrossProjectSubExecKey)
	}
	if containers[0].Kind != KindTestExec {
		t.Errorf("Kind = %q, want %q", containers[0].Kind, KindTestExec)
	}
	if containers[0].IssueType != "Sub Test Execution" {
		t.Errorf("IssueType = %q, want Sub Test Execution", containers[0].IssueType)
	}
	if containers[0].ParentKey == "" {
		t.Error("ParentKey should be non-empty for a sub-task exec")
	}
	if containers[0].ParentSummary == "" {
		t.Error("ParentSummary should be non-empty")
	}
	if len(links) != 1 {
		t.Fatalf("want 1 link, got %d", len(links))
	}
	if links[0].ContainerKey != demoCrossProjectSubExecKey {
		t.Errorf("link.ContainerKey = %q", links[0].ContainerKey)
	}
	if links[0].TestKey != "DEMO-1" {
		t.Errorf("link.TestKey = %q, want DEMO-1", links[0].TestKey)
	}
	if links[0].RunStatus == "" {
		t.Error("link.RunStatus should be non-empty")
	}
}

// TestTestExecutionsForTestDemoNonMember verifies that a DEMO test at a
// non-member index (i%11 != 0) returns empty slices.
func TestTestExecutionsForTestDemoNonMember(t *testing.T) {
	c := NewClient("demo", "token")

	// DEMO-2 is at index 1, which does NOT satisfy i%11 == 0.
	containers, links, err := c.TestExecutionsForTest(context.Background(), "DEMO-2")
	if err != nil {
		t.Fatalf("TestExecutionsForTest: %v", err)
	}
	if len(containers) != 0 {
		t.Errorf("want 0 containers for non-member, got %d", len(containers))
	}
	if len(links) != 0 {
		t.Errorf("want 0 links for non-member, got %d", len(links))
	}
}

// TestTestExecutionsForTestDemoMemberAt11 verifies index 11 (DEMO-12) is also
// a member, confirming the i%11 rule covers more than just index 0.
func TestTestExecutionsForTestDemoMemberAt11(t *testing.T) {
	c := NewClient("demo", "token")

	// DEMO-12 is at index 11, which satisfies i%11 == 0.
	containers, links, err := c.TestExecutionsForTest(context.Background(), "DEMO-12")
	if err != nil {
		t.Fatalf("TestExecutionsForTest: %v", err)
	}
	if len(containers) != 1 {
		t.Fatalf("want 1 container for DEMO-12, got %d", len(containers))
	}
	if len(links) != 1 {
		t.Fatalf("want 1 link for DEMO-12, got %d", len(links))
	}
	if links[0].TestKey != "DEMO-12" {
		t.Errorf("link.TestKey = %q, want DEMO-12", links[0].TestKey)
	}
}

// TestTestExecutionsForTestDemoIsDeterministic confirms the demo path is
// stable across repeated calls for the same test key.
func TestTestExecutionsForTestDemoIsDeterministic(t *testing.T) {
	c := NewClient("demo", "token")
	a1, b1, _ := c.TestExecutionsForTest(context.Background(), "DEMO-1")
	a2, b2, _ := c.TestExecutionsForTest(context.Background(), "DEMO-1")
	if len(a1) != len(a2) || len(b1) != len(b2) {
		t.Fatalf("non-deterministic: containers %d/%d links %d/%d", len(a1), len(a2), len(b1), len(b2))
	}
	for i := range a1 {
		if a1[i].Key != a2[i].Key {
			t.Errorf("container[%d] key differs", i)
		}
	}
	for i := range b1 {
		if b1[i].RunStatus != b2[i].RunStatus {
			t.Errorf("link[%d] RunStatus differs: %q vs %q", i, b1[i].RunStatus, b2[i].RunStatus)
		}
	}
}

// TestGetTestRunsDemoCrossProjectSubExec verifies that GetTestRuns returns
// non-empty runs for the cross-project sub-task execution key.
func TestGetTestRunsDemoCrossProjectSubExec(t *testing.T) {
	c := NewClient("demo", "token")
	runs, err := c.GetTestRuns(context.Background(), demoCrossProjectSubExecKey)
	if err != nil {
		t.Fatalf("GetTestRuns(%q): %v", demoCrossProjectSubExecKey, err)
	}
	if len(runs) == 0 {
		t.Fatalf("want non-empty runs for %q, got none", demoCrossProjectSubExecKey)
	}
	for _, r := range runs {
		if r.TestKey == "" {
			t.Errorf("run missing TestKey: %+v", r)
		}
		if r.Status == "" {
			t.Errorf("run missing Status: %+v", r)
		}
	}
}

// TestGetTestRunsDemoCrossProjectSubExecIsDeterministic verifies repeated
// calls return identical results.
func TestGetTestRunsDemoCrossProjectSubExecIsDeterministic(t *testing.T) {
	c := NewClient("demo", "token")
	a, _ := c.GetTestRuns(context.Background(), demoCrossProjectSubExecKey)
	b, _ := c.GetTestRuns(context.Background(), demoCrossProjectSubExecKey)
	if len(a) != len(b) {
		t.Fatalf("non-deterministic: %d vs %d runs", len(a), len(b))
	}
	for i := range a {
		if a[i].TestKey != b[i].TestKey || a[i].Status != b[i].Status {
			t.Errorf("run[%d] differs", i)
		}
	}
}
