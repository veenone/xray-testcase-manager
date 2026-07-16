package kiwi

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"xray-test-manager/internal/backend"
)

// This file exercises the P4.2 core read mapping against canned fixtures
// derived from spec §9 (p4_0-kiwi-integration-spec.md), citing the fixture
// each test is built from.

// --- tests (spec §3.1, §3.2, §9.1b) ---

// TestSearchTestsPageMapsFieldsAndPaginates covers the full TestCase field
// map (§3.2) plus client-side pagination (§6): two cases are returned out
// of pk order by the mock, the adapter must sort by pk and slice.
func TestSearchTestsPageMapsFieldsAndPaginates(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("TestCase.filter", []map[string]any{
		{
			"id": 99, "summary": "Second case", "text": "",
			"case_status__name": "PROPOSED", "priority__value": "P3",
			"is_automated": true, "tag": []string{"slow"}, "component": []string{"api"},
			"category__product__name": "DEMO",
		},
		// Fixture from spec §9.1b (TestCase.filter single case).
		{
			"id": 42, "summary": "Login with valid creds",
			"text":              "1. open login\n2. enter creds\n**Expected:** dashboard",
			"case_status__name": "CONFIRMED", "priority__value": "P1",
			"is_automated": false, "author__username": "alice", "default_tester__username": "bob",
			"create_date": "2026-01-04T10:00:00",
			"tag":         []string{"smoke", "regression"}, "component": []string{"login", "auth"},
			"category__product__name": "DEMO",
		},
	})
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	tests, total, err := a.SearchTestsPage(context.Background(), "DEMO", "ignored jql", "ignored-since", 0, 1)
	if err != nil {
		t.Fatalf("SearchTestsPage: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	if len(tests) != 1 {
		t.Fatalf("expected page of 1, got %d", len(tests))
	}
	// Sorted by pk ascending -> case 42 first.
	want := backend.Test{
		Key: "42", ID: "42", Summary: "Login with valid creds",
		Description: "1. open login\n2. enter creds\n**Expected:** dashboard",
		Status:      "CONFIRMED", Priority: "P1",
		Labels: []string{"smoke", "regression"}, Components: []string{"login", "auth"},
		ExecType: "Manual",
	}
	if !reflect.DeepEqual(tests[0], want) {
		t.Fatalf("page[0] = %#v, want %#v", tests[0], want)
	}

	page2, total2, err := a.SearchTestsPage(context.Background(), "DEMO", "", "", 1, 1)
	if err != nil {
		t.Fatalf("SearchTestsPage page 2: %v", err)
	}
	if total2 != 2 || len(page2) != 1 || page2[0].Key != "99" {
		t.Fatalf("page 2 = %#v, total=%d, want key 99 total 2", page2, total2)
	}
	if page2[0].ExecType != "Automated" {
		t.Errorf("is_automated=true should map to ExecType=Automated, got %q", page2[0].ExecType)
	}

	// projectKey must narrow via category__product__name (spec §2).
	if len(mock.requests) == 0 {
		t.Fatal("expected recorded requests")
	}
	var params []byte
	for _, r := range mock.requests {
		if r.Method == "TestCase.filter" {
			params = r.Params[0]
			break
		}
	}
	if params == nil || !containsBytes(params, []byte("category__product__name")) {
		t.Fatalf("expected TestCase.filter params to narrow by category__product__name, got %s", params)
	}
}

func containsBytes(haystack, needle []byte) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle []byte) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == string(needle) {
			return i
		}
	}
	return -1
}

// TestGetTestFieldsSingleCase asserts the single-pk refetch path.
func TestGetTestFieldsSingleCase(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("TestCase.filter", []map[string]any{
		{"id": 42, "summary": "Login with valid creds", "text": "do the thing",
			"case_status__name": "CONFIRMED", "priority__value": "P1", "is_automated": false},
	})
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	got, err := a.GetTestFields(context.Background(), "42")
	if err != nil {
		t.Fatalf("GetTestFields: %v", err)
	}
	if got.Key != "42" || got.Summary != "Login with valid creds" || got.Description != "do the thing" {
		t.Fatalf("unexpected Test: %#v", got)
	}
}

// TestGetTestFieldsInvalidKey confirms a non-numeric key errors instead of
// silently sending a malformed filter.
func TestGetTestFieldsInvalidKey(t *testing.T) {
	a := New("http://example.invalid", "alice:secret")
	if _, err := a.GetTestFields(context.Background(), "not-a-number"); err == nil {
		t.Fatal("expected an error for a non-numeric key")
	}
}

// --- steps (spec §3.3, §7) ---

// TestGetTestStepsFlattenAndEmpty covers both branches of flattenSteps:
// non-empty text -> one step; empty text -> no steps.
func TestGetTestStepsFlattenAndEmpty(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handle("TestCase.filter", func(params []json.RawMessage) (any, *rpcErrorObj) {
		var q map[string]any
		_ = json.Unmarshal(params[0], &q)
		pk := int(q["pk"].(float64))
		if pk == 1 {
			return []map[string]any{{"id": 1, "text": "some steps here"}}, nil
		}
		return []map[string]any{{"id": 2, "text": ""}}, nil
	})
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	steps, err := a.GetTestSteps(context.Background(), "1")
	if err != nil {
		t.Fatalf("GetTestSteps: %v", err)
	}
	if len(steps) != 1 || steps[0].Action != "some steps here" || steps[0].Index != 1 || steps[0].ID != "1" {
		t.Fatalf("unexpected steps: %#v", steps)
	}

	empty, err := a.GetTestSteps(context.Background(), "2")
	if err != nil {
		t.Fatalf("GetTestSteps(empty): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected no steps for empty text, got %#v", empty)
	}
}

// TestStepFlattenSharedBetweenSearchAndSteps asserts the SAME transform
// backs GetTestSteps and toTest's Description: for the same underlying
// text, GetTestSteps()[0].Action must equal the Description that
// SearchTestsPage/GetTestFields produced for that case (round-trip
// symmetry the brief calls for).
func TestStepFlattenSharedBetweenSearchAndSteps(t *testing.T) {
	const text = "1. open login\n2. enter creds\n**Expected:** dashboard"
	mock := newMockRPCServer(t)
	mock.handleResult("TestCase.filter", []map[string]any{
		{"id": 42, "summary": "x", "text": text},
	})
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	ctx := context.Background()
	test, err := a.GetTestFields(ctx, "42")
	if err != nil {
		t.Fatalf("GetTestFields: %v", err)
	}
	steps, err := a.GetTestSteps(ctx, "42")
	if err != nil {
		t.Fatalf("GetTestSteps: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}
	if test.Description != steps[0].Action {
		t.Fatalf("Description %q != GetTestSteps action %q (flatten not shared)", test.Description, steps[0].Action)
	}
	if test.Description != text {
		t.Fatalf("Description = %q, want raw text %q", test.Description, text)
	}
}

// --- ListTestsBasic / GetTestMeta (spec §3.1) ---

func TestListTestsBasicByIDs(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("TestCase.filter", []map[string]any{
		{"id": 42, "summary": "Login with valid creds", "case_status__name": "CONFIRMED", "category__product__name": "DEMO"},
	})
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	got, err := a.ListTestsBasic(context.Background(), []string{"42"})
	if err != nil {
		t.Fatalf("ListTestsBasic: %v", err)
	}
	want := []backend.TestBasic{{Key: "42", Summary: "Login with valid creds", Status: "CONFIRMED", ProjectKey: "DEMO"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListTestsBasic = %#v, want %#v", got, want)
	}
}

func TestListTestsBasicEmptyKeys(t *testing.T) {
	a := New("http://example.invalid", "alice:secret")
	got, err := a.ListTestsBasic(context.Background(), nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("ListTestsBasic(nil) = %#v, %v; want empty, nil", got, err)
	}
}

// TestGetTestMetaBestEffort asserts create_date/author map, and
// Updated/UpdatedBy stay empty (spec §3.1: "Updated best-effort"; §9 has no
// TestCase.history fixture, so we don't invent one).
func TestGetTestMetaBestEffort(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("TestCase.filter", []map[string]any{
		{"id": 42, "author__username": "alice", "create_date": "2026-01-04T10:00:00"},
	})
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	got, err := a.GetTestMeta(context.Background(), "42")
	if err != nil {
		t.Fatalf("GetTestMeta: %v", err)
	}
	want := backend.TestMeta{Created: "2026-01-04T10:00:00", Creator: "alice", Updated: "", UpdatedBy: ""}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetTestMeta = %#v, want %#v", got, want)
	}
}

// --- containers + runs (spec §3.5, §3.7, §9.1c) ---

func TestListContainersMapsPlansAndRuns(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("TestPlan.filter", []map[string]any{
		{"id": 5, "name": "Regression Plan", "parent": nil, "text": "top-level plan"},
	})
	mock.handleResult("TestRun.filter", []map[string]any{
		{"id": 9, "summary": "Sprint 12 run", "plan": 5, "build__name": "unspecified",
			"start_date": "2026-02-01T09:00:00", "stop_date": "2026-02-01T10:00:00"},
	})
	mock.handleResult("TestCase.filter", []map[string]any{
		{"id": 42},
	})
	// Fixture from spec §9.1c (TestExecution.filter({"run": 9})).
	mock.handleResult("TestExecution.filter", []map[string]any{
		{"id": 501, "run": 9, "case": 42,
			"status__name": "PASSED", "assignee__username": "bob", "tested_by__username": "bob",
			"build__name": "unspecified",
			"start_date":  "2026-02-01T09:00:00", "stop_date": "2026-02-01T09:05:00"},
	})
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	var progressCalls int
	containers, links, err := a.ListContainers(context.Background(), "DEMO", func(done, total int) { progressCalls++ })
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if progressCalls != 2 {
		t.Errorf("expected onProgress called twice (1 plan + 1 run), got %d", progressCalls)
	}
	if len(containers) != 2 {
		t.Fatalf("expected 2 containers, got %#v", containers)
	}

	var plan, run *backend.Container
	for i := range containers {
		switch containers[i].Kind {
		case backend.KindTestPlan:
			plan = &containers[i]
		case backend.KindTestExec:
			run = &containers[i]
		}
	}
	if plan == nil || plan.Key != "5" || plan.Summary != "Regression Plan" || plan.ParentKey != "" || plan.Description != "top-level plan" {
		t.Fatalf("unexpected plan container: %#v", plan)
	}
	if run == nil || run.Key != "9" || run.Summary != "Sprint 12 run" || run.ParentKey != "5" ||
		len(run.Environments) != 1 || run.Environments[0] != "unspecified" ||
		run.Created != "2026-02-01T09:00:00" || run.Resolved != "2026-02-01T10:00:00" {
		t.Fatalf("unexpected run container: %#v", run)
	}

	if len(links) != 2 {
		t.Fatalf("expected 2 links (1 plan membership + 1 exec membership), got %#v", links)
	}
	foundPlanLink, foundExecLink := false, false
	for _, l := range links {
		if l.ContainerKey == "5" && l.TestKey == "42" {
			foundPlanLink = true
		}
		if l.ContainerKey == "9" && l.TestKey == "42" && l.RunStatus == "PASSED" {
			foundExecLink = true
		}
	}
	if !foundPlanLink || !foundExecLink {
		t.Fatalf("missing expected links: %#v", links)
	}
}

func TestTestExecutionsForTestMapsRunsAndLinks(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("TestExecution.filter", []map[string]any{
		{"id": 501, "run": 9, "case": 42, "status__name": "PASSED", "tested_by__username": "bob"},
	})
	mock.handleResult("TestRun.filter", []map[string]any{
		{"id": 9, "summary": "Sprint 12 run", "plan": 5},
	})
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	containers, links, err := a.TestExecutionsForTest(context.Background(), "42")
	if err != nil {
		t.Fatalf("TestExecutionsForTest: %v", err)
	}
	if len(containers) != 1 || containers[0].Key != "9" || containers[0].Kind != backend.KindTestExec {
		t.Fatalf("unexpected containers: %#v", containers)
	}
	if len(links) != 1 || links[0].ContainerKey != "9" || links[0].TestKey != "42" || links[0].RunStatus != "PASSED" {
		t.Fatalf("unexpected links: %#v", links)
	}
}

func TestTestExecutionsForTestNoExecutions(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("TestExecution.filter", []map[string]any{})
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	containers, links, err := a.TestExecutionsForTest(context.Background(), "42")
	if err != nil || len(containers) != 0 || len(links) != 0 {
		t.Fatalf("expected (empty, empty, nil), got (%#v, %#v, %v)", containers, links, err)
	}
}

// TestGetTestRunsMapsExecutions uses the exact §9.1c fixture and asserts
// the full TestRun DTO map (spec §3.7), including the ExecutedBy fallback.
func TestGetTestRunsMapsExecutions(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("TestExecution.filter", []map[string]any{
		{"id": 501, "run": 9, "case": 42,
			"status": 4, "status__name": "PASSED",
			"assignee__username": "bob", "tested_by__username": "bob",
			"build": 3, "build__name": "unspecified",
			"start_date": "2026-02-01T09:00:00", "stop_date": "2026-02-01T09:05:00"},
		{"id": 502, "run": 9, "case": 43,
			"status__name": "IDLE", "assignee__username": "carol", "tested_by__username": "",
			"build__name": "unspecified", "start_date": "", "stop_date": ""},
	})
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	runs, err := a.GetTestRuns(context.Background(), "9")
	if err != nil {
		t.Fatalf("GetTestRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %#v", runs)
	}
	want0 := backend.TestRun{
		TestKey: "42", Status: "PASSED", StartedAt: "2026-02-01T09:00:00", FinishedAt: "2026-02-01T09:05:00",
		ExecutedBy: "bob", Environment: "unspecified",
		CreatedAt: "2026-02-01T09:00:00", UpdatedAt: "2026-02-01T09:05:00",
	}
	if !reflect.DeepEqual(runs[0], want0) {
		t.Fatalf("runs[0] = %#v, want %#v", runs[0], want0)
	}
	if runs[1].ExecutedBy != "carol" {
		t.Fatalf("expected ExecutedBy to fall back to assignee__username, got %q", runs[1].ExecutedBy)
	}
}

func TestExecPlansReturnsParentPlan(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("TestRun.filter", []map[string]any{
		{"id": 9, "plan": 5},
	})
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	plans, err := a.ExecPlans(context.Background(), "9")
	if err != nil {
		t.Fatalf("ExecPlans: %v", err)
	}
	if len(plans) != 1 || plans[0] != "5" {
		t.Fatalf("ExecPlans = %#v, want [\"5\"]", plans)
	}
}

func TestExecPlansUnknownRun(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("TestRun.filter", []map[string]any{})
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	plans, err := a.ExecPlans(context.Background(), "999")
	if err != nil || len(plans) != 0 {
		t.Fatalf("ExecPlans(unknown) = %#v, %v; want empty, nil", plans, err)
	}
}

// --- metadata (spec §3.12) ---

func TestMetadataLists(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("TestCaseStatus.filter", []map[string]any{{"name": "CONFIRMED"}, {"name": "PROPOSED"}})
	mock.handleResult("Priority.filter", []map[string]any{{"value": "P1"}, {"value": "P2"}})
	mock.handleResult("Component.filter", []map[string]any{{"name": "auth"}})
	mock.handleResult("Version.filter", []map[string]any{{"value": "1.0"}})
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	ctx := context.Background()
	statuses, err := a.ListStatuses(ctx, "DEMO")
	if err != nil || !reflect.DeepEqual(statuses, []string{"CONFIRMED", "PROPOSED"}) {
		t.Fatalf("ListStatuses = %#v, %v", statuses, err)
	}
	priorities, err := a.ListPriorities(ctx, "DEMO")
	if err != nil || !reflect.DeepEqual(priorities, []string{"P1", "P2"}) {
		t.Fatalf("ListPriorities = %#v, %v", priorities, err)
	}
	components, err := a.ProjectComponents(ctx, "DEMO")
	if err != nil || !reflect.DeepEqual(components, []string{"auth"}) {
		t.Fatalf("ProjectComponents = %#v, %v", components, err)
	}
	versions, err := a.ProjectVersions(ctx, "DEMO")
	if err != nil || !reflect.DeepEqual(versions, []string{"1.0"}) {
		t.Fatalf("ProjectVersions = %#v, %v", versions, err)
	}

	for _, r := range mock.requests {
		if r.Method == "Component.filter" || r.Method == "Version.filter" {
			if !containsBytes(r.Params[0], []byte("product__name")) {
				t.Errorf("%s: expected product__name filter, got %s", r.Method, r.Params[0])
			}
		}
	}
}

// --- RemoteVersion content hash (spec §5) ---

func TestRemoteVersionContentHashDeterministicAndChanges(t *testing.T) {
	baseRow := map[string]any{
		"id": 42, "summary": "Login with valid creds", "text": "steps",
		"case_status__name": "CONFIRMED", "priority__value": "P1",
		"tag": []string{"b", "a"}, "component": []string{"z", "y"},
	}
	mock := newMockRPCServer(t)
	mock.handleResult("TestCase.filter", []map[string]any{baseRow})
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	ctx := context.Background()
	tok1, err := a.RemoteVersion(ctx, "test", "42")
	if err != nil {
		t.Fatalf("RemoteVersion: %v", err)
	}
	tok2, err := a.RemoteVersion(ctx, "test", "42")
	if err != nil {
		t.Fatalf("RemoteVersion (2nd call): %v", err)
	}
	if tok1 != tok2 {
		t.Fatalf("same input produced different tokens: %q vs %q", tok1, tok2)
	}
	if tok1 == "" {
		t.Fatal("expected a non-empty token")
	}

	// Change one salient field (summary) -> different token.
	mock2 := newMockRPCServer(t)
	changedRow := map[string]any{}
	for k, v := range baseRow {
		changedRow[k] = v
	}
	changedRow["summary"] = "Login with INVALID creds"
	mock2.handleResult("TestCase.filter", []map[string]any{changedRow})
	a2, closeFn2 := newTestAdapter(t, mock2)
	defer closeFn2()

	tok3, err := a2.RemoteVersion(ctx, "test", "42")
	if err != nil {
		t.Fatalf("RemoteVersion (changed): %v", err)
	}
	if tok3 == tok1 {
		t.Fatalf("expected a different token after a summary change, both = %q", tok1)
	}
	if a.RemoteAhead(tok1, tok3) != true {
		t.Fatal("RemoteAhead should report true for two different content-hash tokens")
	}
}
