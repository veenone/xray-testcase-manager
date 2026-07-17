package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// TestGetTestRunsDemoFailRunsHaveComment checks that FAIL runs in the demo
// also carry a non-empty Comment referencing the fabricated defect, so a demo
// Test Execution exercises both the defect and the remark UI.
func TestGetTestRunsDemoFailRunsHaveComment(t *testing.T) {
	c := NewClient("demo", "token")
	found := false
	for _, execKey := range []string{"DEMO-TE-1", "DEMO-TE-2", "DEMO-TE-3", "DEMO-TE-4",
		"DEMO-TE-5", "DEMO-TE-6", "DEMO-TE-7", "DEMO-TE-8"} {
		runs, err := c.GetTestRuns(context.Background(), execKey)
		if err != nil {
			t.Fatalf("%s: %v", execKey, err)
		}
		for _, r := range runs {
			if r.Status == "FAIL" {
				if r.Comment == "" {
					t.Errorf("%s: FAIL run for %s has empty Comment", execKey, r.TestKey)
				}
				if len(r.Defects) > 0 {
					found = true
				}
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

// TestParseTestExecTests exercises the Server/DC testexec endpoint parser.

// TestParseTestExecTestsBasic verifies a minimal array with key and status.
func TestParseTestExecTestsBasic(t *testing.T) {
	body := []byte(`[{"id":1,"key":"DEMO-1","rank":1,"status":"PASS"},{"id":2,"key":"DEMO-2","rank":2,"status":"FAIL"}]`)
	runs, err := parseTestExecTests(body)
	if err != nil {
		t.Fatalf("parseTestExecTests: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("want 2 runs, got %d", len(runs))
	}
	if runs[0].TestKey != "DEMO-1" || runs[0].Status != "PASS" {
		t.Errorf("run[0] = %+v, want DEMO-1/PASS", runs[0])
	}
	if runs[1].TestKey != "DEMO-2" || runs[1].Status != "FAIL" {
		t.Errorf("run[1] = %+v, want DEMO-2/FAIL", runs[1])
	}
}

// TestParseTestExecTestsDetailed verifies detailed optional fields are mapped.
func TestParseTestExecTestsDetailed(t *testing.T) {
	body := []byte(`[{
		"id":3,"key":"QA-5","rank":1,"status":"pass",
		"startedOn":"2026-01-01T10:00:00Z",
		"finishedOn":"2026-01-01T10:30:00Z",
		"assignee":"alice",
		"testEnvironments":["Staging"],
		"defects":["BUG-1"]
	}]`)
	runs, err := parseTestExecTests(body)
	if err != nil {
		t.Fatalf("parseTestExecTests: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(runs))
	}
	r := runs[0]
	if r.TestKey != "QA-5" {
		t.Errorf("TestKey = %q, want QA-5", r.TestKey)
	}
	if r.Status != "PASS" {
		t.Errorf("Status = %q, want PASS (uppercased)", r.Status)
	}
	if r.StartedAt != "2026-01-01T10:00:00Z" {
		t.Errorf("StartedAt = %q, want 2026-01-01T10:00:00Z", r.StartedAt)
	}
	if r.FinishedAt != "2026-01-01T10:30:00Z" {
		t.Errorf("FinishedAt = %q, want 2026-01-01T10:30:00Z", r.FinishedAt)
	}
	if r.ExecutedBy != "alice" {
		t.Errorf("ExecutedBy = %q, want alice", r.ExecutedBy)
	}
	if r.Environment != "Staging" {
		t.Errorf("Environment = %q, want Staging", r.Environment)
	}
	if len(r.Defects) != 1 || r.Defects[0] != "BUG-1" {
		t.Errorf("Defects = %v, want [BUG-1]", r.Defects)
	}
}

// TestParseTestExecTestsDefectsObjectForm verifies defects as objects with a
// "key" field are extracted correctly alongside plain string defect keys.
func TestParseTestExecTestsDefectsObjectForm(t *testing.T) {
	body := []byte(`[{"id":4,"key":"QA-6","rank":1,"status":"FAIL","defects":[{"key":"BUG-2"}]}]`)
	runs, err := parseTestExecTests(body)
	if err != nil {
		t.Fatalf("parseTestExecTests: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(runs))
	}
	if len(runs[0].Defects) != 1 || runs[0].Defects[0] != "BUG-2" {
		t.Errorf("Defects = %v, want [BUG-2]", runs[0].Defects)
	}
}

// TestParseTestExecTestsComment verifies the comment field is parsed when
// present and left empty when absent or null.
func TestParseTestExecTestsComment(t *testing.T) {
	body := []byte(`[
		{"id":7,"key":"QA-8","rank":1,"status":"PASS","comment":"some remark"},
		{"id":8,"key":"QA-9","rank":2,"status":"PASS"},
		{"id":9,"key":"QA-10","rank":3,"status":"PASS","comment":null}
	]`)
	runs, err := parseTestExecTests(body)
	if err != nil {
		t.Fatalf("parseTestExecTests: %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("want 3 runs, got %d", len(runs))
	}
	if runs[0].Comment != "some remark" {
		t.Errorf("runs[0].Comment = %q, want %q", runs[0].Comment, "some remark")
	}
	if runs[1].Comment != "" {
		t.Errorf("runs[1].Comment = %q, want empty (field absent)", runs[1].Comment)
	}
	if runs[2].Comment != "" {
		t.Errorf("runs[2].Comment = %q, want empty (field null)", runs[2].Comment)
	}
}

// TestParseTestExecTestsEmpty verifies empty/null/[] bodies return empty slice.
func TestParseTestExecTestsEmpty(t *testing.T) {
	for _, body := range [][]byte{[]byte("null"), []byte("  "), []byte("[]"), nil} {
		runs, err := parseTestExecTests(body)
		if err != nil {
			t.Fatalf("parseTestExecTests(%q): %v", body, err)
		}
		if len(runs) != 0 {
			t.Errorf("expected empty for %q, got %d runs", body, len(runs))
		}
	}
}

// TestParseTestExecTestsSkipsEmptyKey verifies objects with no key are dropped.
func TestParseTestExecTestsSkipsEmptyKey(t *testing.T) {
	body := []byte(`[{"id":5,"key":"","rank":1,"status":"PASS"},{"id":6,"key":"QA-7","rank":2,"status":"PASS"}]`)
	runs, err := parseTestExecTests(body)
	if err != nil {
		t.Fatalf("parseTestExecTests: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("want 1 run (empty key skipped), got %d", len(runs))
	}
	if runs[0].TestKey != "QA-7" {
		t.Errorf("TestKey = %q, want QA-7", runs[0].TestKey)
	}
}

// TestGetTestRunsLiveStub exercises the live path against a mock Jira server,
// asserting GetTestRuns calls the Xray Server/DC testexec endpoint, sends
// detailed=true, and accumulates results across pages. The mock serves a full
// first page (100 items, matching the internal page size) and a partial second
// page (1 item) to confirm the pagination loop terminates on a short page.
func TestGetTestRunsLiveStub(t *testing.T) {
	// Build a full page of 100 items plus one extra item on page 2.
	const pageSize = 100
	page1 := make([]map[string]any, pageSize)
	for i := 0; i < pageSize; i++ {
		page1[i] = map[string]any{
			"id": i + 1, "key": fmt.Sprintf("QA-%d", i+1), "rank": i + 1, "status": "PASS",
		}
	}
	page2 := []map[string]any{
		{"id": pageSize + 1, "key": fmt.Sprintf("QA-%d", pageSize+1), "rank": pageSize + 1, "status": "FAIL"},
	}

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/rest/raven/1.0/api/testexec/QA-TE-1/test"
		if r.URL.Path != wantPath {
			t.Errorf("unexpected path %s, want %s", r.URL.Path, wantPath)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		calls++
		q := r.URL.Query()
		if q.Get("detailed") != "true" {
			t.Errorf("want detailed=true, got %q", q.Get("detailed"))
		}
		switch calls {
		case 1:
			_ = json.NewEncoder(w).Encode(page1)
		case 2:
			_ = json.NewEncoder(w).Encode(page2)
		default:
			// Should not reach here.
			t.Errorf("unexpected page request %d", calls)
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		}
	}))
	defer srv.Close()

	runs, err := newTestClient(srv).GetTestRuns(context.Background(), "QA-TE-1")
	if err != nil {
		t.Fatalf("GetTestRuns live: %v", err)
	}
	if len(runs) != pageSize+1 {
		t.Fatalf("want %d runs (page1+page2), got %d", pageSize+1, len(runs))
	}
	if runs[0].TestKey != "QA-1" || runs[0].Status != "PASS" {
		t.Errorf("run[0] = %+v, want QA-1/PASS", runs[0])
	}
	if runs[pageSize].TestKey != fmt.Sprintf("QA-%d", pageSize+1) || runs[pageSize].Status != "FAIL" {
		t.Errorf("run[%d] = %+v, want QA-%d/FAIL", pageSize, runs[pageSize], pageSize+1)
	}
	if calls != 2 {
		t.Errorf("expected exactly 2 HTTP calls (2 pages), got %d", calls)
	}
}

// TestParseTestExecTestsLiveSample locks in the exact shape returned by a live
// Xray Server/DC instance (GET /rest/raven/1.0/api/testexec/{key}/test?detailed=true):
// a plain array with key, status, executedBy, startedOn, finishedOn, defects[],
// evidences[], rank, id. No testEnvironments and no created/updated timestamps.
func TestParseTestExecTestsLiveSample(t *testing.T) {
	body := []byte(`[
		{"id":3153818,"status":"PASS","executedBy":"muhamakb","startedOn":"2026-04-02T03:42:17+02:00","finishedOn":"2026-04-02T03:42:17+02:00","defects":[],"evidences":[],"key":"RND_P_4JKTEE_05-2429","rank":1},
		{"id":3153819,"status":"FAIL","executedBy":"muhamakb","startedOn":"2026-04-02T08:44:44+02:00","finishedOn":"2026-04-02T08:44:44+02:00","defects":[],"evidences":[],"key":"RND_P_4JKTEE_05-2432","rank":2}
	]`)
	runs, err := parseTestExecTests(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}
	r0 := runs[0]
	if r0.TestKey != "RND_P_4JKTEE_05-2429" {
		t.Errorf("TestKey = %q", r0.TestKey)
	}
	if r0.Status != "PASS" {
		t.Errorf("Status = %q, want PASS", r0.Status)
	}
	if r0.ExecutedBy != "muhamakb" {
		t.Errorf("ExecutedBy = %q", r0.ExecutedBy)
	}
	if r0.StartedAt != "2026-04-02T03:42:17+02:00" {
		t.Errorf("StartedAt = %q", r0.StartedAt)
	}
	if r0.FinishedAt != "2026-04-02T03:42:17+02:00" {
		t.Errorf("FinishedAt = %q", r0.FinishedAt)
	}
	if r0.Environment != "" {
		t.Errorf("Environment = %q, want empty (no testEnvironments in response)", r0.Environment)
	}
	if len(r0.Defects) != 0 {
		t.Errorf("Defects = %v, want empty", r0.Defects)
	}
	if runs[1].Status != "FAIL" {
		t.Errorf("runs[1].Status = %q, want FAIL", runs[1].Status)
	}
}

// TestParsePlanKeys covers the Xray Test Plan custom field value: the confirmed
// live shape (array of key strings), an array-of-objects fallback, and empty.
func TestParsePlanKeys(t *testing.T) {
	// Confirmed live shape: ["RND_P_4JKTEE_05-2804"].
	if got := parsePlanKeys([]byte(`["RND_P_4JKTEE_05-2804"]`)); len(got) != 1 || got[0] != "RND_P_4JKTEE_05-2804" {
		t.Errorf("string array: got %v", got)
	}
	// Multiple keys.
	if got := parsePlanKeys([]byte(`["A-1","A-2"]`)); len(got) != 2 || got[1] != "A-2" {
		t.Errorf("multi: got %v", got)
	}
	// Array of objects with a key field (tolerated fallback).
	if got := parsePlanKeys([]byte(`[{"key":"B-9"}]`)); len(got) != 1 || got[0] != "B-9" {
		t.Errorf("object array: got %v", got)
	}
	// Null / empty.
	if got := parsePlanKeys([]byte(`null`)); got != nil {
		t.Errorf("null: got %v", got)
	}
	if got := parsePlanKeys([]byte(``)); got != nil {
		t.Errorf("empty: got %v", got)
	}
}

// TestDemoSubExecRunsNonEmpty confirms sub-task Test Executions now carry demo
// runs, so the per-test Run history includes them (matching the live sync).
func TestDemoSubExecRunsNonEmpty(t *testing.T) {
	r1 := demoTestRuns("DEMO-STE-1")
	r2 := demoTestRuns("DEMO-STE-2")
	if len(r1) == 0 || len(r2) == 0 {
		t.Fatalf("expected demo sub-task exec runs, got STE-1=%d STE-2=%d", len(r1), len(r2))
	}
	for _, run := range r1 {
		if run.TestKey == "" || run.Status == "" {
			t.Errorf("sub-task run missing key/status: %+v", run)
		}
	}
}
