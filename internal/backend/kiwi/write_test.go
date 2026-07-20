package kiwi

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// This file exercises P5.1's Kiwi TestCase write surface (FieldsForJira,
// UpdateIssue, CreateTest) against the mock RPC server, per
// p5_1-brief.md's test list.

// requestParams decodes recorded request idx's params[argIdx] into out, for
// asserting exactly what an RPC call sent.
func requestParams(t *testing.T, mock *mockRPCServer, method string, argIdx int, out any) {
	t.Helper()
	for _, r := range mock.requests {
		if r.Method != method {
			continue
		}
		if argIdx >= len(r.Params) {
			t.Fatalf("%s: recorded request has only %d params, want index %d", method, len(r.Params), argIdx)
		}
		if err := json.Unmarshal(r.Params[argIdx], out); err != nil {
			t.Fatalf("%s: decode param %d: %v", method, argIdx, err)
		}
		return
	}
	t.Fatalf("no recorded request for method %q", method)
}

// callCount returns how many times method was invoked.
func callCount(mock *mockRPCServer, method string) int {
	n := 0
	for _, r := range mock.requests {
		if r.Method == method {
			n++
		}
	}
	return n
}

// --- FieldsForJira (spec item 1) ---

func TestFieldsForJiraMapsNeutralNames(t *testing.T) {
	a := &Adapter{}
	got := a.FieldsForJira(map[string]string{
		"summary":     "New summary",
		"description": "New description",
		"priority":    "P1",
		"labels":      "smoke regression",
		"status":      "CONFIRMED",
		"components":  "Login, Backend",
		"unsupported": "should be dropped",
	})
	want := map[string]any{
		"summary":     "New summary",
		"text":        "New description",
		"priority":    "P1",
		"labels":      "smoke regression",
		"case_status": "CONFIRMED",
		"components":  "Login, Backend",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FieldsForJira = %#v, want %#v", got, want)
	}
}

func TestFieldsForJiraEmptyForUnknownFields(t *testing.T) {
	a := &Adapter{}
	got := a.FieldsForJira(map[string]string{"folder": "x", "exec_type": "Automated"})
	if len(got) != 0 {
		t.Fatalf("expected no Kiwi analog to map, got %#v", got)
	}
}

// --- UpdateIssue: summary+priority+status resolve to ids (spec item 2) ---

func TestUpdateIssueResolvesIdsAndAppliesFields(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("Priority.filter", []map[string]any{{"id": 5, "value": "P1"}})
	mock.handleResult("TestCaseStatus.filter", []map[string]any{{"id": 3, "name": "CONFIRMED"}})
	mock.handleResult("TestCase.update", nil)
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	fields := a.FieldsForJira(map[string]string{
		"summary":  "Renamed test",
		"priority": "P1",
		"status":   "CONFIRMED",
	})
	if err := a.UpdateIssue(context.Background(), "42", fields); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}

	var id int
	requestParams(t, mock, "TestCase.update", 0, &id)
	if id != 42 {
		t.Fatalf("TestCase.update id = %d, want 42", id)
	}
	var values map[string]any
	requestParams(t, mock, "TestCase.update", 1, &values)
	if values["summary"] != "Renamed test" {
		t.Errorf("summary = %v, want %q", values["summary"], "Renamed test")
	}
	if values["priority"] != float64(5) {
		t.Errorf("priority = %v, want resolved id 5", values["priority"])
	}
	if values["case_status"] != float64(3) {
		t.Errorf("case_status = %v, want resolved id 3", values["case_status"])
	}
	// labels/components must NOT ride along on TestCase.update's values.
	if _, ok := values["labels"]; ok {
		t.Errorf("labels leaked into TestCase.update values: %#v", values)
	}
}

// TestUpdateIssueUnresolvablePriorityErrors confirms a priority value with
// no matching Priority row fails loudly rather than silently dropping the
// field or guessing an id.
func TestUpdateIssueUnresolvablePriorityErrors(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("Priority.filter", []map[string]any{}) // no match
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	fields := a.FieldsForJira(map[string]string{"priority": "P99"})
	if err := a.UpdateIssue(context.Background(), "42", fields); err == nil {
		t.Fatal("expected an error for an unresolvable priority")
	}
	if callCount(mock, "TestCase.update") != 0 {
		t.Error("TestCase.update must not be called when priority resolution fails")
	}
}

// TestResolvePriorityFallsBackWhenNameUnknown covers the cross-backend
// tolerance (B7): when the requested priority name is not one of Kiwi's (e.g.
// an Xray "High" published across the bridge), resolvePriorityID falls back to
// Kiwi's first available priority instead of hard-failing the write. The mock
// distinguishes the two Priority.filter calls by params: the value lookup
// (`{"value":...}`) returns no match, the all lookup (`{}`) returns the list.
func TestResolvePriorityFallsBackWhenNameUnknown(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handle("Priority.filter", func(params []json.RawMessage) (any, *rpcErrorObj) {
		if len(params) > 0 && strings.Contains(string(params[0]), `"value"`) {
			return []map[string]any{}, nil // no exact match for the name
		}
		return []map[string]any{{"id": 7, "value": "P1"}}, nil // fall back to the first
	})
	mock.handleResult("TestCase.update", nil)
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	fields := a.FieldsForJira(map[string]string{"priority": "High"})
	if err := a.UpdateIssue(context.Background(), "42", fields); err != nil {
		t.Fatalf("expected fallback to the first priority, got error: %v", err)
	}
	if callCount(mock, "TestCase.update") != 1 {
		t.Error("TestCase.update should be called with the fallback priority id")
	}
}

// --- UpdateIssue: tag diff (spec item 3) ---

func TestUpdateIssueTagDiffAddsOnly(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("Tag.filter", []map[string]any{{"name": "smoke", "case": 1}})
	mock.handleResult("TestCase.add_tag", nil)
	mock.handleResult("TestCase.remove_tag", nil)
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	fields := a.FieldsForJira(map[string]string{"labels": "smoke regression"})
	if err := a.UpdateIssue(context.Background(), "1", fields); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}

	if callCount(mock, "TestCase.remove_tag") != 0 {
		t.Error("expected no remove_tag calls (existing tag stays, no removals)")
	}
	if n := callCount(mock, "TestCase.add_tag"); n != 1 {
		t.Fatalf("expected exactly 1 add_tag call, got %d", n)
	}
	var id int
	var name string
	for _, r := range mock.requests {
		if r.Method == "TestCase.add_tag" {
			_ = json.Unmarshal(r.Params[0], &id)
			_ = json.Unmarshal(r.Params[1], &name)
		}
	}
	if id != 1 || name != "regression" {
		t.Fatalf("add_tag(%d, %q), want add_tag(1, \"regression\")", id, name)
	}
}

func TestUpdateIssueTagDiffIsIdempotentForExistingTag(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("Tag.filter", []map[string]any{{"name": "smoke", "case": 1}})
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	fields := a.FieldsForJira(map[string]string{"labels": "smoke"})
	if err := a.UpdateIssue(context.Background(), "1", fields); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	if callCount(mock, "TestCase.add_tag") != 0 || callCount(mock, "TestCase.remove_tag") != 0 {
		t.Fatal("re-asserting an already-present tag must not call add_tag/remove_tag")
	}
}

// --- UpdateIssue: component diff, removal by NAME (spec item 3) ---

func TestUpdateIssueComponentDiffRemovesByName(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("Component.filter", []map[string]any{
		{"name": "Login", "cases": 1}, {"name": "Backend", "cases": 1},
	})
	mock.handleResult("TestCase.add_component", nil)
	mock.handleResult("TestCase.remove_component", nil)
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	fields := a.FieldsForJira(map[string]string{"components": "Login"})
	if err := a.UpdateIssue(context.Background(), "1", fields); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}

	if callCount(mock, "TestCase.add_component") != 0 {
		t.Error("expected no add_component calls")
	}
	if n := callCount(mock, "TestCase.remove_component"); n != 1 {
		t.Fatalf("expected exactly 1 remove_component call, got %d", n)
	}
	var id int
	var name string
	for _, r := range mock.requests {
		if r.Method == "TestCase.remove_component" {
			_ = json.Unmarshal(r.Params[0], &id)
			_ = json.Unmarshal(r.Params[1], &name)
		}
	}
	if id != 1 || name != "Backend" {
		t.Fatalf("remove_component(%d, %q), want remove_component(1, \"Backend\") — component by NAME not id", id, name)
	}
}

// --- CreateTest (spec item 4) ---

func TestCreateTestSendsRequiredFieldsAndAttaches(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("Priority.filter", []map[string]any{{"id": 5, "value": "P1"}})
	mock.handleResult("TestCaseStatus.filter", []map[string]any{
		{"id": 2, "name": "PROPOSED"}, {"id": 3, "name": "CONFIRMED"},
	})
	mock.handleResult("Category.filter", []map[string]any{{"id": 7, "name": "Functional"}})
	mock.handleResult("TestCase.create", map[string]any{"id": 99})
	mock.handleResult("TestCase.add_tag", nil)
	mock.handleResult("TestCase.add_component", nil)
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	key, err := a.CreateTest(context.Background(), "DEMO", "New test", "step text", "P1",
		[]string{"smoke"}, []string{"Login"})
	if err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	if key != "99" {
		t.Fatalf("key = %q, want %q", key, "99")
	}

	var values map[string]any
	requestParams(t, mock, "TestCase.create", 0, &values)
	if values["summary"] != "New test" {
		t.Errorf("summary = %v", values["summary"])
	}
	if values["text"] != "step text" {
		t.Errorf("text = %v", values["text"])
	}
	if values["priority"] != float64(5) {
		t.Errorf("priority = %v, want resolved id 5", values["priority"])
	}
	if values["category"] != float64(7) {
		t.Errorf("category = %v, want resolved id 7", values["category"])
	}
	// Default status: PROPOSED preferred over CONFIRMED (documented choice).
	if values["case_status"] != float64(2) {
		t.Errorf("case_status = %v, want default PROPOSED id 2", values["case_status"])
	}

	var tagID int
	var tagName string
	requestParams(t, mock, "TestCase.add_tag", 0, &tagID)
	requestParams(t, mock, "TestCase.add_tag", 1, &tagName)
	if tagID != 99 || tagName != "smoke" {
		t.Fatalf("add_tag(%d,%q), want add_tag(99,\"smoke\")", tagID, tagName)
	}
	var compID int
	var compName string
	requestParams(t, mock, "TestCase.add_component", 0, &compID)
	requestParams(t, mock, "TestCase.add_component", 1, &compName)
	if compID != 99 || compName != "Login" {
		t.Fatalf("add_component(%d,%q), want add_component(99,\"Login\")", compID, compName)
	}
}

// TestCreateTestNoCategoryErrorsWithoutCreating asserts that when the
// product has no resolvable Category, CreateTest fails loudly and never
// calls TestCase.create (no partial/invented remote-mutating write).
func TestCreateTestNoCategoryErrorsWithoutCreating(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("Priority.filter", []map[string]any{{"id": 5, "value": "P1"}})
	mock.handleResult("TestCaseStatus.filter", []map[string]any{{"id": 2, "name": "PROPOSED"}})
	mock.handleResult("Category.filter", []map[string]any{}) // no category under this product
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	if _, err := a.CreateTest(context.Background(), "EMPTY-PRODUCT", "New test", "", "P1", nil, nil); err == nil {
		t.Fatal("expected an error when no Category is resolvable")
	}
	if callCount(mock, "TestCase.create") != 0 {
		t.Error("TestCase.create must not be called when category resolution fails")
	}
}

// TestCreateTestEmptyProjectKeyErrors asserts CreateTest refuses to guess a
// category when no product is given at all.
func TestCreateTestEmptyProjectKeyErrors(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("Priority.filter", []map[string]any{{"id": 5, "value": "P1"}})
	mock.handleResult("TestCaseStatus.filter", []map[string]any{{"id": 2, "name": "PROPOSED"}})
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	if _, err := a.CreateTest(context.Background(), "", "New test", "", "P1", nil, nil); err == nil {
		t.Fatal("expected an error when projectKey is empty")
	}
	if callCount(mock, "Category.filter") != 0 {
		t.Error("Category.filter should not even be called for an empty projectKey")
	}
	if callCount(mock, "TestCase.create") != 0 {
		t.Error("TestCase.create must not be called when projectKey is empty")
	}
}

// --- CreateTestStep (P6 — inline-text step write) ---

// TestCreateTestStepSetsTextWhenEmpty covers the empty-text case: the
// assembled step content becomes the whole `text` value, and the returned
// step id round-trips through flattenSteps' fixed "1" id.
func TestCreateTestStepSetsTextWhenEmpty(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("TestCase.filter", []map[string]any{{"id": 42, "text": ""}})
	mock.handleResult("TestCase.update", nil)
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	stepID, err := a.CreateTestStep(context.Background(), "42", "Click login", "user=alice", "Login succeeds")
	if err != nil {
		t.Fatalf("CreateTestStep: %v", err)
	}
	if stepID == "" {
		t.Fatal("expected a non-empty step id")
	}

	var id int
	requestParams(t, mock, "TestCase.update", 0, &id)
	if id != 42 {
		t.Fatalf("TestCase.update id = %d, want 42", id)
	}
	var values map[string]any
	requestParams(t, mock, "TestCase.update", 1, &values)
	text, _ := values["text"].(string)
	if text == "" {
		t.Fatal("expected TestCase.update text to be non-empty")
	}
	for _, want := range []string{"Click login", "Data: user=alice", "Expected: Login succeeds"} {
		if !strings.Contains(text, want) {
			t.Errorf("text %q missing %q", text, want)
		}
	}

	// Round-trip through the read-side transform: the written text maps
	// back to exactly one neutral Step carrying the content just written.
	steps := flattenSteps(text)
	if len(steps) != 1 {
		t.Fatalf("flattenSteps(%q) = %d steps, want 1", text, len(steps))
	}
	if steps[0].ID != stepID {
		t.Errorf("flattenSteps step id = %q, want %q (CreateTestStep's returned id)", steps[0].ID, stepID)
	}
	if !strings.Contains(steps[0].Action, "Click login") {
		t.Errorf("flattenSteps step action = %q, missing written content", steps[0].Action)
	}
}

// TestCreateTestStepAppendsToExistingText covers the non-empty case: prior
// text must survive, and the new content must be present alongside it
// (append, not overwrite) — repeat calls accumulate.
func TestCreateTestStepAppendsToExistingText(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("TestCase.filter", []map[string]any{{"id": 7, "text": "Step 1: Open the app"}})
	mock.handleResult("TestCase.update", nil)
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	if _, err := a.CreateTestStep(context.Background(), "7", "Step 2: Log out", "", ""); err != nil {
		t.Fatalf("CreateTestStep: %v", err)
	}

	var values map[string]any
	requestParams(t, mock, "TestCase.update", 1, &values)
	text, _ := values["text"].(string)
	if !strings.Contains(text, "Step 1: Open the app") {
		t.Errorf("text %q lost the existing content", text)
	}
	if !strings.Contains(text, "Step 2: Log out") {
		t.Errorf("text %q missing the newly appended content", text)
	}

	steps := flattenSteps(text)
	if len(steps) != 1 {
		t.Fatalf("flattenSteps(%q) = %d steps, want 1", text, len(steps))
	}
	if !strings.Contains(steps[0].Action, "Step 1: Open the app") || !strings.Contains(steps[0].Action, "Step 2: Log out") {
		t.Errorf("round-tripped step action = %q, missing prior or new content", steps[0].Action)
	}
}

// TestCreateTestStepInvalidKeyErrors asserts a non-numeric (non-TestCase)
// key fails before any RPC is attempted, matching UpdateIssue/parseKiwiID.
func TestCreateTestStepInvalidKeyErrors(t *testing.T) {
	mock := newMockRPCServer(t)
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	if _, err := a.CreateTestStep(context.Background(), "not-a-kiwi-id", "action", "", ""); err == nil {
		t.Fatal("expected an error for a non-numeric key")
	}
	if callCount(mock, "TestCase.filter") != 0 {
		t.Error("TestCase.filter must not be called for an invalid key")
	}
	if callCount(mock, "TestCase.update") != 0 {
		t.Error("TestCase.update must not be called for an invalid key")
	}
}
