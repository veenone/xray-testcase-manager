package jira

import "testing"

// TestParseContainerTests_ExecWithStatus covers the bare-array execution
// response: each membership carries the Test's run status (uppercased).
func TestParseContainerTests_ExecWithStatus(t *testing.T) {
	body := []byte(`[
		{"id": 1, "key": "QA-1", "rank": 1, "status": "pass"},
		{"id": 2, "key": "QA-2", "rank": 2, "status": "FAIL"}
	]`)
	links, err := parseContainerTests(KindTestExec, "EXEC-1", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("want 2 links, got %d", len(links))
	}
	if links[0].ContainerKey != "EXEC-1" || links[0].TestKey != "QA-1" || links[0].RunStatus != "PASS" {
		t.Fatalf("unexpected link: %+v", links[0])
	}
	if links[1].RunStatus != "FAIL" {
		t.Fatalf("want FAIL, got %q", links[1].RunStatus)
	}
}

// TestParseContainerTests_SetNoStatus covers a Test Set: plain memberships with
// no run status, including the {"tests":[…]} wrapper and a testKey field.
func TestParseContainerTests_SetNoStatus(t *testing.T) {
	body := []byte(`{"tests": [{"testKey": "QA-7", "status": "PASS"}]}`)
	links, err := parseContainerTests(KindTestSet, "SET-1", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(links) != 1 || links[0].TestKey != "QA-7" {
		t.Fatalf("unexpected links: %+v", links)
	}
	// A set membership must not carry a run status even if the payload had one.
	if links[0].RunStatus != "" {
		t.Fatalf("set membership should have no run status, got %q", links[0].RunStatus)
	}
}

// TestParseContainerTests_Empty treats an empty body as no memberships.
func TestParseContainerTests_Empty(t *testing.T) {
	links, err := parseContainerTests(KindTestPlan, "PLAN-1", []byte("  "))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(links) != 0 {
		t.Fatalf("want 0 links, got %d", len(links))
	}
}
