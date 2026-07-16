package kiwi

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"xray-test-manager/internal/backend"
)

// This file exercises P5.2's Kiwi container/run write surface
// (CreateContainer, AddTestsToContainer, RemoveTestsFromContainer,
// SetTestRunStatus, DeleteContainer) against the mock RPC server, per
// p5_2-brief.md's test list. It reuses write_test.go's requestParams/
// callCount helpers (same package).

// --- CreateContainer(KindTestPlan) ---

func TestCreateContainerTestPlanSendsResolvedIDs(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("Product.filter", []map[string]any{{"id": 61, "name": "DEMO"}})
	mock.handleResult("PlanType.filter", []map[string]any{
		{"id": 9, "name": "Smoke"}, {"id": 1, "name": "Unit"},
	})
	mock.handleResult("Version.filter", []map[string]any{{"id": 93, "value": "1.0"}})
	mock.handleResult("TestPlan.create", map[string]any{"id": 150})
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	key, err := a.CreateContainer(context.Background(), "DEMO", backend.KindTestPlan, "Regression Plan")
	if err != nil {
		t.Fatalf("CreateContainer: %v", err)
	}
	if key != "150" {
		t.Fatalf("key = %q, want %q", key, "150")
	}

	var values map[string]any
	requestParams(t, mock, "TestPlan.create", 0, &values)
	if values["name"] != "Regression Plan" {
		t.Errorf("name = %v", values["name"])
	}
	if values["product"] != float64(61) {
		t.Errorf("product = %v, want resolved id 61", values["product"])
	}
	if values["product_version"] != float64(93) {
		t.Errorf("product_version = %v, want resolved id 93", values["product_version"])
	}
	// Default plan type: "Unit" preferred over "Smoke" (documented choice).
	if values["type"] != float64(1) {
		t.Errorf("type = %v, want default Unit id 1", values["type"])
	}
}

func TestCreateContainerTestPlanNoProductErrorsWithoutCreating(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("Product.filter", []map[string]any{}) // no match
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	if _, err := a.CreateContainer(context.Background(), "NOPE", backend.KindTestPlan, "x"); err == nil {
		t.Fatal("expected an error when Product cannot be resolved")
	}
	if callCount(mock, "TestPlan.create") != 0 {
		t.Error("TestPlan.create must not be called when product resolution fails")
	}
}

// --- CreateContainer(KindTestExec): the TestRun-needs-plan impedance gap ---

// TestCreateContainerTestExecStopsWithoutExistingPlan is the brief's
// required "assert the STOP/error path — no fabricated plan" case: the
// neutral CreateContainer(kind=testexec) signature supplies no plan, so when
// the product has no existing TestPlan at all, this must error out WITHOUT
// ever calling TestRun.create (and, critically, without calling
// TestPlan.create to invent one).
func TestCreateContainerTestExecStopsWithoutExistingPlan(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("TestPlan.filter", []map[string]any{}) // no plan under this product
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	_, err := a.CreateContainer(context.Background(), "LONELY-PRODUCT", backend.KindTestExec, "Exec 1")
	if err == nil {
		t.Fatal("expected an error (STOP-and-report) when no Test Plan exists for the product")
	}
	if callCount(mock, "TestPlan.create") != 0 {
		t.Error("TestPlan.create must NEVER be called by CreateContainer(kind=testexec) — that would fabricate a plan")
	}
	if callCount(mock, "TestRun.create") != 0 {
		t.Error("TestRun.create must not be called when plan resolution fails")
	}
}

// TestCreateContainerTestExecResolvesPlanBuildManager is the best-effort
// success path: an existing plan/build/active user all resolve, so
// TestRun.create proceeds with their real ids.
func TestCreateContainerTestExecResolvesPlanBuildManager(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("TestPlan.filter", []map[string]any{{"id": 137, "product_version": 93}})
	mock.handleResult("Build.filter", []map[string]any{{"id": 384}})
	mock.handleResult("User.filter", []map[string]any{{"id": 137, "username": "admin"}})
	mock.handleResult("TestRun.create", map[string]any{"id": 55})
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	key, err := a.CreateContainer(context.Background(), "DEMO", backend.KindTestExec, "Exec 1")
	if err != nil {
		t.Fatalf("CreateContainer: %v", err)
	}
	if key != "55" {
		t.Fatalf("key = %q, want %q", key, "55")
	}

	var values map[string]any
	requestParams(t, mock, "TestRun.create", 0, &values)
	if values["summary"] != "Exec 1" {
		t.Errorf("summary = %v", values["summary"])
	}
	if values["plan"] != float64(137) {
		t.Errorf("plan = %v, want resolved id 137", values["plan"])
	}
	if values["build"] != float64(384) {
		t.Errorf("build = %v, want resolved id 384", values["build"])
	}
	if values["manager"] != float64(137) {
		t.Errorf("manager = %v, want resolved id 137", values["manager"])
	}
}

// TestCreateContainerUnknownKindErrors covers the neither-plan-nor-exec
// branch (Kiwi has no KindTestSet).
func TestCreateContainerUnknownKindErrors(t *testing.T) {
	a := &Adapter{}
	if _, err := a.CreateContainer(context.Background(), "DEMO", backend.KindTestSet, "x"); err == nil {
		t.Fatal("expected an error for an unsupported container kind (Kiwi has no KindTestSet)")
	}
}

// --- AddTestsToContainer / RemoveTestsFromContainer ---

func TestAddTestsToContainerTestPlanCallsAddCase(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("TestPlan.add_case", nil)
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	if err := a.AddTestsToContainer(context.Background(), backend.KindTestPlan, "1", []string{"10", "11"}); err != nil {
		t.Fatalf("AddTestsToContainer: %v", err)
	}
	if n := callCount(mock, "TestPlan.add_case"); n != 2 {
		t.Fatalf("expected 2 TestPlan.add_case calls, got %d", n)
	}
	var planID, caseID int
	for _, r := range mock.requests {
		if r.Method != "TestPlan.add_case" {
			continue
		}
		_ = json.Unmarshal(r.Params[0], &planID)
		_ = json.Unmarshal(r.Params[1], &caseID)
		if planID != 1 {
			t.Errorf("plan id = %d, want 1", planID)
		}
	}
	_ = caseID
}

func TestAddTestsToContainerTestExecCallsRunAddCase(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("TestRun.add_case", nil)
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	if err := a.AddTestsToContainer(context.Background(), backend.KindTestExec, "7", []string{"10"}); err != nil {
		t.Fatalf("AddTestsToContainer: %v", err)
	}
	var runID, caseID int
	requestParams(t, mock, "TestRun.add_case", 0, &runID)
	requestParams(t, mock, "TestRun.add_case", 1, &caseID)
	if runID != 7 || caseID != 10 {
		t.Fatalf("TestRun.add_case(%d,%d), want (7,10)", runID, caseID)
	}
}

func TestRemoveTestsFromContainerTestPlanCallsRemoveCase(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("TestPlan.remove_case", nil)
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	if err := a.RemoveTestsFromContainer(context.Background(), backend.KindTestPlan, "1", []string{"10"}); err != nil {
		t.Fatalf("RemoveTestsFromContainer: %v", err)
	}
	var planID, caseID int
	requestParams(t, mock, "TestPlan.remove_case", 0, &planID)
	requestParams(t, mock, "TestPlan.remove_case", 1, &caseID)
	if planID != 1 || caseID != 10 {
		t.Fatalf("TestPlan.remove_case(%d,%d), want (1,10)", planID, caseID)
	}
}

// TestRemoveTestsFromContainerTestExecUsesTestExecutionRemove asserts the
// deprecation-aware routing: a KindTestExec removal must go through
// TestExecution.remove({"run":...,"case":...}), NOT the deprecated
// TestRun.remove_case alias.
func TestRemoveTestsFromContainerTestExecUsesTestExecutionRemove(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("TestExecution.remove", nil)
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	if err := a.RemoveTestsFromContainer(context.Background(), backend.KindTestExec, "7", []string{"10"}); err != nil {
		t.Fatalf("RemoveTestsFromContainer: %v", err)
	}
	if callCount(mock, "TestRun.remove_case") != 0 {
		t.Error("must not call the deprecated TestRun.remove_case")
	}
	var query map[string]any
	requestParams(t, mock, "TestExecution.remove", 0, &query)
	if query["run"] != float64(7) || query["case"] != float64(10) {
		t.Fatalf("TestExecution.remove query = %#v, want {run:7,case:10}", query)
	}
}

func TestRemoveTestsFromContainerMethodNotFoundDegradesToUnsupported(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleError("TestPlan.remove_case", methodNotFoundCode, "Method not found")
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	err := a.RemoveTestsFromContainer(context.Background(), backend.KindTestPlan, "1", []string{"10"})
	if !errors.Is(err, backend.ErrUnsupported) {
		t.Fatalf("expected errors.Is(err, backend.ErrUnsupported), got %v", err)
	}
}

// --- SetTestRunStatus ---

func TestSetTestRunStatusFindsExecAndUpdates(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("TestExecution.filter", []map[string]any{{"id": 501}})
	mock.handleResult("TestExecutionStatus.filter", []map[string]any{{"id": 4, "name": "PASSED"}})
	mock.handleResult("TestExecution.update", nil)
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	if err := a.SetTestRunStatus(context.Background(), "1", "2", "PASSED"); err != nil {
		t.Fatalf("SetTestRunStatus: %v", err)
	}

	var filterQuery map[string]any
	requestParams(t, mock, "TestExecution.filter", 0, &filterQuery)
	if filterQuery["run"] != float64(1) || filterQuery["case"] != float64(2) {
		t.Fatalf("TestExecution.filter query = %#v, want {run:1,case:2}", filterQuery)
	}

	var execID int
	var values map[string]any
	requestParams(t, mock, "TestExecution.update", 0, &execID)
	requestParams(t, mock, "TestExecution.update", 1, &values)
	if execID != 501 {
		t.Fatalf("TestExecution.update exec id = %d, want 501", execID)
	}
	if values["status"] != float64(4) {
		t.Fatalf("status = %v, want resolved id 4", values["status"])
	}
}

func TestSetTestRunStatusNoExecutionErrors(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("TestExecution.filter", []map[string]any{}) // no matching row
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	if err := a.SetTestRunStatus(context.Background(), "1", "2", "PASSED"); err == nil {
		t.Fatal("expected an error when no Test Execution matches run+case")
	}
	if callCount(mock, "TestExecution.update") != 0 {
		t.Error("TestExecution.update must not be called when the execution cannot be found")
	}
}

func TestSetTestRunStatusUnresolvableStatusErrors(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("TestExecution.filter", []map[string]any{{"id": 501}})
	mock.handleResult("TestExecutionStatus.filter", []map[string]any{}) // no match
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	if err := a.SetTestRunStatus(context.Background(), "1", "2", "NOT-A-REAL-STATUS"); err == nil {
		t.Fatal("expected an error for an unresolvable status")
	}
	if callCount(mock, "TestExecution.update") != 0 {
		t.Error("TestExecution.update must not be called when status resolution fails")
	}
}

// --- DeleteContainer ---

func TestDeleteContainerTestExecCallsRunRemove(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("TestRun.remove", nil)
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	if err := a.DeleteContainer(context.Background(), backend.KindTestExec, "7"); err != nil {
		t.Fatalf("DeleteContainer: %v", err)
	}
	var query map[string]any
	requestParams(t, mock, "TestRun.remove", 0, &query)
	if query["pk"] != float64(7) {
		t.Fatalf("TestRun.remove query = %#v, want {pk:7}", query)
	}
}

func TestDeleteContainerTestPlanIsUnsupported(t *testing.T) {
	a := &Adapter{}
	err := a.DeleteContainer(context.Background(), backend.KindTestPlan, "1")
	if !errors.Is(err, backend.ErrUnsupported) {
		t.Fatalf("expected errors.Is(err, backend.ErrUnsupported), got %v", err)
	}
}

// --- SetContainerEnvironments stays UNSUP (spec item 5) ---

func TestSetContainerEnvironmentsIsUnsupported(t *testing.T) {
	a := &Adapter{}
	err := a.SetContainerEnvironments(context.Background(), "1", []string{"Staging"})
	if !errors.Is(err, backend.ErrUnsupported) {
		t.Fatalf("expected errors.Is(err, backend.ErrUnsupported), got %v", err)
	}
}
