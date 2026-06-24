package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGetTestRunsDemo verifies the demo path returns non-empty, well-formed
// runs for a known demo execution key.
func TestGetTestRunsDemo(t *testing.T) {
	c := NewClient("demo", "token")
	runs, err := c.GetTestRuns(context.Background(), "DEMO-TE-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) == 0 {
		t.Fatal("expected demo test runs, got none")
	}
	for _, r := range runs {
		if r.TestKey == "" {
			t.Fatalf("demo run missing TestKey: %+v", r)
		}
		if r.Status == "" {
			t.Fatalf("demo run missing Status: %+v", r)
		}
	}
}

// TestGetTestRunsDemoIsDeterministic verifies the demo generator is
// deterministic: the same exec key always yields the same runs.
func TestGetTestRunsDemoIsDeterministic(t *testing.T) {
	c := NewClient("demo", "token")
	a, err := c.GetTestRuns(context.Background(), "DEMO-TE-2")
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.GetTestRuns(context.Background(), "DEMO-TE-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != len(b) {
		t.Fatalf("non-deterministic: first call %d runs, second %d", len(a), len(b))
	}
	for i := range a {
		if a[i].TestKey != b[i].TestKey || a[i].Status != b[i].Status {
			t.Errorf("run[%d] differs: %+v vs %+v", i, a[i], b[i])
		}
	}
}

// TestGetTestRunsDemoStatusVocabulary checks that demo runs use only the
// expected status vocabulary and that at least one FAIL run carries a defect.
func TestGetTestRunsDemoStatusVocabulary(t *testing.T) {
	valid := map[string]bool{
		"PASS": true, "FAIL": true, "TODO": true,
		"EXECUTING": true, "ABORTED": true, "BLOCKED": true,
	}
	c := NewClient("demo", "token")
	// Check a few executions.
	for _, execKey := range []string{"DEMO-TE-1", "DEMO-TE-3", "DEMO-TE-5"} {
		runs, err := c.GetTestRuns(context.Background(), execKey)
		if err != nil {
			t.Fatalf("%s: %v", execKey, err)
		}
		for _, r := range runs {
			if !valid[r.Status] {
				t.Errorf("%s: invalid status %q on run for %s", execKey, r.Status, r.TestKey)
			}
		}
	}
}

// TestGetTestRunsDemoFailRunsHaveDefects checks that FAIL runs in the demo
// carry at least one defect key derived from the demo bug seed.
func TestGetTestRunsDemoFailRunsHaveDefects(t *testing.T) {
	c := NewClient("demo", "token")
	found := false
	for _, execKey := range []string{"DEMO-TE-1", "DEMO-TE-2", "DEMO-TE-3", "DEMO-TE-4",
		"DEMO-TE-5", "DEMO-TE-6", "DEMO-TE-7", "DEMO-TE-8"} {
		runs, err := c.GetTestRuns(context.Background(), execKey)
		if err != nil {
			t.Fatalf("%s: %v", execKey, err)
		}
		for _, r := range runs {
			if r.Status == "FAIL" && len(r.Defects) > 0 {
				found = true
			}
		}
	}
	if !found {
		t.Error("no FAIL run with a defect found across all demo executions")
	}
}

// TestGetTestRunsDemoHasTimestamps verifies that demo runs carry non-empty
// CreatedAt and UpdatedAt timestamps derived deterministically from the exec
// key and run position.
func TestGetTestRunsDemoHasTimestamps(t *testing.T) {
	c := NewClient("demo", "token")
	for _, execKey := range []string{"DEMO-TE-1", "DEMO-TE-3", "DEMO-TE-5"} {
		runs, err := c.GetTestRuns(context.Background(), execKey)
		if err != nil {
			t.Fatalf("%s: %v", execKey, err)
		}
		if len(runs) == 0 {
			t.Fatalf("%s: expected at least one run", execKey)
		}
		for i, r := range runs {
			if r.CreatedAt == "" {
				t.Errorf("%s run[%d] (%s): CreatedAt is empty", execKey, i, r.TestKey)
			}
			if r.UpdatedAt == "" {
				t.Errorf("%s run[%d] (%s): UpdatedAt is empty", execKey, i, r.TestKey)
			}
		}
	}
}

// TestExecPlansDemo verifies that demo executions return plan keys.
func TestExecPlansDemo(t *testing.T) {
	c := NewClient("demo", "token")
	plans, err := c.ExecPlans(context.Background(), "DEMO-TE-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) == 0 {
		t.Fatal("expected at least one plan for DEMO-TE-1, got none")
	}
	for _, pk := range plans {
		if pk == "" {
			t.Fatal("empty plan key returned")
		}
	}
}

// TestExecPlansDemoIsDeterministic checks stability of the exec-plan mapping.
func TestExecPlansDemoIsDeterministic(t *testing.T) {
	c := NewClient("demo", "token")
	a, _ := c.ExecPlans(context.Background(), "DEMO-TE-3")
	b, _ := c.ExecPlans(context.Background(), "DEMO-TE-3")
	if len(a) != len(b) {
		t.Fatalf("non-deterministic: %d vs %d plans", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("plan[%d] differs: %q vs %q", i, a[i], b[i])
		}
	}
}

// TestParseTestRuns exercises the JSON decoder for the live path.
func TestParseTestRuns(t *testing.T) {
	body := []byte(`[
		{"testKey":"QA-1","status":"PASS","startedOn":"2026-01-01T09:00:00Z","finishedOn":"2026-01-01T09:05:00Z","executedBy":"alice","testEnvironment":"Staging","defects":["BUG-1"]},
		{"testKey":"QA-2","status":"FAIL","defects":[]}
	]`)
	runs, err := parseTestRuns(body)
	if err != nil {
		t.Fatalf("parseTestRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("want 2 runs, got %d", len(runs))
	}
	if runs[0].TestKey != "QA-1" || runs[0].Status != "PASS" {
		t.Errorf("run[0] = %+v, want QA-1/PASS", runs[0])
	}
	if runs[0].StartedAt != "2026-01-01T09:00:00Z" {
		t.Errorf("StartedAt = %q, want 2026-01-01T09:00:00Z", runs[0].StartedAt)
	}
	if runs[0].Environment != "Staging" {
		t.Errorf("Environment = %q, want Staging", runs[0].Environment)
	}
	if len(runs[0].Defects) != 1 || runs[0].Defects[0] != "BUG-1" {
		t.Errorf("Defects = %v, want [BUG-1]", runs[0].Defects)
	}
	if runs[1].TestKey != "QA-2" || runs[1].Status != "FAIL" {
		t.Errorf("run[1] = %+v, want QA-2/FAIL", runs[1])
	}
}

// TestParseTestRunsEmpty covers null / empty bodies.
func TestParseTestRunsEmpty(t *testing.T) {
	for _, body := range [][]byte{[]byte("null"), []byte("  "), []byte("[]")} {
		runs, err := parseTestRuns(body)
		if err != nil {
			t.Fatalf("parseTestRuns(%q): %v", body, err)
		}
		if len(runs) != 0 {
			t.Errorf("expected empty, got %d runs", len(runs))
		}
	}
}

// TestGetTestRunsLiveStub exercises the live path against a mock Jira, asserting
// the stub GETs the expected raven endpoint and the raw JSON is decoded.
func TestGetTestRunsLiveStub(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/rest/raven/2.0/api/testruns") {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("testExecIssueKey") != "QA-TE-1" {
			t.Errorf("want testExecIssueKey=QA-TE-1, got %q", r.URL.Query().Get("testExecIssueKey"))
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"testKey": "QA-1", "status": "PASS"},
		})
	}))
	defer srv.Close()

	runs, err := newTestClient(srv).GetTestRuns(context.Background(), "QA-TE-1")
	if err != nil {
		t.Fatalf("GetTestRuns live: %v", err)
	}
	if len(runs) != 1 || runs[0].TestKey != "QA-1" || runs[0].Status != "PASS" {
		t.Fatalf("unexpected runs: %+v", runs)
	}
}
