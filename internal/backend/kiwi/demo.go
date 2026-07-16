package kiwi

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Demo mode lets the Kiwi read path (P4.1-P4.3) be exercised in the app
// without a live Kiwi TCMS instance, mirroring the Xray precedent in
// internal/jira/demo.go (isDemoURL + a per-call short-circuit to an
// in-memory generator). A profile selects it with the Jira URL "kiwi-demo"
// (or a "kiwi-demo:"/"kiwi-demo-<variant>" prefix, kept for forward
// compatibility with a themed variant later — only one generic dataset is
// served today). P4.4 brief deliverable (1).
//
// Unlike the Xray demo (which short-circuits inside every jira.Client
// method individually), the Kiwi demo short-circuits in ONE place:
// Client.call. Every Adapter method in this package already goes through
// Client.call for every RPC it makes, so intercepting there covers the
// whole read path with a single dispatch table keyed by RPC method name —
// see kiwiDemoGenerator.dispatch below.

// IsKiwiDemoURL reports whether baseURL selects the offline Kiwi demo mode.
// Exported: app.go's backend factory (package main) needs it to route a
// profile to kiwi.New instead of the Xray path (P4.4 brief deliverable 2).
// "kiwi-demo" deliberately does NOT start with "demo", so it can never be
// misclassified by the existing internal/jira isDemoURL check.
func IsKiwiDemoURL(baseURL string) bool {
	u := strings.ToLower(strings.TrimSpace(baseURL))
	return u == "kiwi-demo" || strings.HasPrefix(u, "kiwi-demo:") || strings.HasPrefix(u, "kiwi-demo-")
}

// kiwiDemoSessionID is the fake Auth.login session token the demo generator
// hands back — never checked against anything, just needs to be non-empty
// so sessionLogin's post-login flow (auth.go) has something to work with.
const kiwiDemoSessionID = "kiwi-demo-session"

// kiwiDemoProductName is the single Product/category name every demo
// TestCase/TestPlan/TestRun carries. TestCase.filter/TestPlan.filter/
// TestRun.filter product-scoping filters (category__product__name,
// product__name, plan__product__name) are intentionally NOT honored for
// narrowing by the demo dispatch below — see filterTestCases' doc comment
// for why. The structural filters real callers in this package actually
// depend on for correct navigation (pk, pk__in, plan, run, case) ARE
// honored, so browsing a specific test/plan/run/execution works correctly.
const kiwiDemoProductName = "DEMO"

// kiwiDemoGenerator holds a small, fixed, deterministic Kiwi dataset and
// serves it by RPC method name via dispatch. Built once by
// newKiwiDemoGenerator and reused for the lifetime of the demo Client — no
// randomness, no clock reads, so repeated calls (and repeated app runs)
// return byte-identical results.
type kiwiDemoGenerator struct {
	users        []kiwiUser
	testCases    []kiwiTestCase
	plans        []kiwiTestPlan
	runs         []kiwiTestRun
	execs        []kiwiTestExecution
	requirements []kiwiRequirement
	statuses     []kiwiName
	priorities   []kiwiValue
	components   []kiwiName
	versions     []kiwiValue

	// planCases maps a TestPlan id to the TestCase ids that belong to it —
	// the membership TestCase.filter({"plan":id}) reads (adapter.go's
	// ListContainers).
	planCases map[int][]int

	// reqLinks maps a Requirement id to link-type -> covering TestCase ids —
	// the shape Requirement.coverage(id) reads (requirements.go's
	// extractCoverageCaseIDs "bare int array" branch, the same shape
	// requirements_test.go's TestListRequirementsMapsFilterAndCoverage
	// fixture uses).
	reqLinks map[int]map[string][]int
}

// newKiwiDemoGenerator builds the fixed demo dataset: 40 test cases across
// 10 features, 4 test plans (one nested), 5 test runs, a handful of
// executions per run, and 6 requirements with typed coverage links — enough
// to browse without being absurd (P4.4 brief: "small but deterministic").
// Both the requirements plugin AND the review plugin report present (see
// dispatch's Requirement.filter/ReviewRequest.filter cases), so
// Capabilities().SupportsRequirementObjects/SupportsIssueLinkTypes both
// flip true after TestConnection, same as against a real Kiwi with both
// plugins installed.
func newKiwiDemoGenerator() *kiwiDemoGenerator {
	features := []string{
		"Login", "Logout", "Password reset", "Search", "Checkout",
		"Cart", "Payment", "Reports", "Admin console", "API rate limit",
	}
	conditions := []string{
		"with valid input", "with invalid input", "as an admin user", "after timeout",
	}
	statusCycle := []string{"CONFIRMED", "CONFIRMED", "CONFIRMED", "PROPOSED", "PROPOSED", "NEED_UPDATE", "DISABLED"}
	priorityCycle := []string{"P1", "P2", "P2", "P3", "P3", "P3", "P4", "P5"}
	components := []string{"login", "auth", "search", "payment", "api", "ui"}
	tagCycle := [][]string{
		{"smoke"}, {"regression"}, {"smoke", "regression"}, {"automated"}, {"manual"}, {"flaky"},
	}
	authors := []string{"alice", "bob", "carol"}

	g := &kiwiDemoGenerator{
		planCases: map[int][]int{},
		reqLinks:  map[int]map[string][]int{},
	}

	// --- test cases: 10 features x 4 conditions = 40 cases, ids 1..40 ---
	id := 1
	for fi, feature := range features {
		for _, cond := range conditions {
			text := fmt.Sprintf("1. Set up %s\n2. Perform the action %s\n**Expected:** the expected outcome occurs",
				strings.ToLower(feature), cond)
			g.testCases = append(g.testCases, kiwiTestCase{
				ID:             id,
				Summary:        feature + " " + cond,
				Text:           text,
				CaseStatusName: statusCycle[(id-1)%len(statusCycle)],
				PriorityValue:  priorityCycle[(id-1)%len(priorityCycle)],
				IsAutomated:    id%3 == 0,
				AuthorUsername: authors[id%len(authors)],
				CreateDate:     fmt.Sprintf("2026-01-%02dT09:00:00", (id%28)+1),
				Tag:            kiwiNames(tagCycle[id%len(tagCycle)]),
				Component:      kiwiNames{components[fi%len(components)]},
				ProductName:    kiwiDemoProductName,
			})
			id++
		}
	}

	// --- test plans: 4 plans (one nested under "Regression Plan") ---
	g.plans = []kiwiTestPlan{
		{ID: 1, Name: "Regression Plan", Parent: nil, Text: "Full regression coverage"},
		{ID: 2, Name: "Smoke Plan", Parent: nil, Text: "Fast smoke coverage run before every release"},
		{ID: 3, Name: "Sprint 12 Plan", Parent: intPtr(1), Text: "Sprint 12 scope, subset of regression"},
		{ID: 4, Name: "API Plan", Parent: nil, Text: "API-focused coverage"},
	}
	// Even split of the 40 cases across the 4 plans (10 each) — simple,
	// deterministic membership good enough to exercise ListContainers'
	// per-plan TestCase.filter({"plan":id}) call.
	g.planCases[1] = idRange(1, 10)
	g.planCases[2] = idRange(11, 20)
	g.planCases[3] = idRange(21, 30)
	g.planCases[4] = idRange(31, 40)

	// --- test runs: 5 runs, tied to plans ---
	g.runs = []kiwiTestRun{
		{ID: 101, Summary: "Sprint 12 - Run 1", Plan: 3, BuildName: "1.0", StartDate: "2026-02-01T09:00:00", StopDate: "2026-02-01T10:00:00"},
		{ID: 102, Summary: "Regression - Run 1", Plan: 1, BuildName: "1.0", StartDate: "2026-02-02T09:00:00", StopDate: "2026-02-02T12:00:00"},
		{ID: 103, Summary: "Smoke - Nightly", Plan: 2, BuildName: "1.1", StartDate: "2026-02-03T02:00:00", StopDate: "2026-02-03T02:30:00"},
		{ID: 104, Summary: "API - Run 1", Plan: 4, BuildName: "2.0-beta", StartDate: "2026-02-04T09:00:00", StopDate: "2026-02-04T09:45:00"},
		{ID: 105, Summary: "Regression - Run 2", Plan: 1, BuildName: "1.1", StartDate: "2026-02-05T09:00:00", StopDate: "2026-02-05T13:00:00"},
	}

	// --- test executions: a handful per run, varied statuses ---
	execStatusCycle := []string{"PASSED", "FAILED", "IDLE", "PASSED", "ERROR"}
	execID := 501
	addExecs := func(runID int, buildName string, caseIDs []int, startDate, stopDate string) {
		for i, caseID := range caseIDs {
			g.execs = append(g.execs, kiwiTestExecution{
				ID:               execID,
				Run:              runID,
				Case:             caseID,
				StatusName:       execStatusCycle[i%len(execStatusCycle)],
				AssigneeUsername: authors[i%len(authors)],
				TestedByUsername: authors[(i+1)%len(authors)],
				BuildName:        buildName,
				StartDate:        startDate,
				StopDate:         stopDate,
			})
			execID++
		}
	}
	addExecs(101, "1.0", idRange(21, 25), "2026-02-01T09:00:00", "2026-02-01T09:30:00")
	addExecs(102, "1.0", idRange(1, 10), "2026-02-02T09:00:00", "2026-02-02T11:00:00")
	addExecs(103, "1.1", idRange(11, 15), "2026-02-03T02:00:00", "2026-02-03T02:15:00")
	addExecs(104, "2.0-beta", idRange(31, 35), "2026-02-04T09:00:00", "2026-02-04T09:20:00")
	addExecs(105, "1.1", idRange(6, 10), "2026-02-05T09:00:00", "2026-02-05T10:00:00")

	// --- requirements + coverage links (both plugins present) ---
	g.requirements = []kiwiRequirement{
		{ID: 201, Title: "User can log in", Status: "approved", Priority: "P1"},
		{ID: 202, Title: "User can reset password", Status: "approved", Priority: "P2"},
		{ID: 203, Title: "Search returns relevant results", Status: "approved", Priority: "P2"},
		{ID: 204, Title: "Checkout completes payment", Status: "approved", Priority: "P1"},
		{ID: 205, Title: "Admin can manage users", Status: "draft", Priority: "P3"},
		{ID: 206, Title: "API enforces rate limits", Status: "approved", Priority: "P3"},
	}
	g.reqLinks[201] = map[string][]int{"verifies": {1, 2}, "validates": {3}}  // Login cases 1-4
	g.reqLinks[202] = map[string][]int{"verifies": {9, 10}}                   // Password reset cases 9-12
	g.reqLinks[203] = map[string][]int{"verifies": {13, 14}}                  // Search cases 13-16
	g.reqLinks[204] = map[string][]int{"verifies": {17, 18, 25, 26}}          // Checkout+Payment
	g.reqLinks[205] = map[string][]int{"verifies": {33}, "validates": {34}}   // Admin console cases 33-36
	g.reqLinks[206] = map[string][]int{"verifies": {37, 38}, "related": {39}} // API rate limit cases 37-40

	// --- metadata vocabularies ---
	for _, s := range []string{"CONFIRMED", "PROPOSED", "NEED_UPDATE", "DISABLED"} {
		g.statuses = append(g.statuses, kiwiName{Name: s})
	}
	for _, p := range []string{"P1", "P2", "P3", "P4", "P5"} {
		g.priorities = append(g.priorities, kiwiValue{Value: p})
	}
	for _, c := range components {
		g.components = append(g.components, kiwiName{Name: c})
	}
	for _, v := range []string{"1.0", "1.1", "2.0-beta"} {
		g.versions = append(g.versions, kiwiValue{Value: v})
	}

	// --- the single demo user, returned by both Auth.login and User.filter ---
	g.users = []kiwiUser{
		{Username: "kiwi-demo", FirstName: "Kiwi", LastName: "Demo", Email: "kiwi-demo@example.invalid"},
	}

	return g
}

func intPtr(n int) *int { return &n }

// idRange returns [from, to] inclusive as a []int.
func idRange(from, to int) []int {
	out := make([]int, 0, to-from+1)
	for i := from; i <= to; i++ {
		out = append(out, i)
	}
	return out
}

// call is the demo entry point Client.call delegates to when the client is
// in demo mode (see the "if c.demo != nil" short-circuit added to call in
// client.go). It dispatches by method name, then round-trips the result
// through JSON (marshal then unmarshal into out) — the exact same decode
// step call() itself performs on a real "result" payload — so the demo path
// exercises the same lenient decoders (kiwiNames, etc.) a live Kiwi
// response would.
func (g *kiwiDemoGenerator) call(method string, params []any, out any) error {
	result, err := g.dispatch(method, params)
	if err != nil {
		return err
	}
	if out == nil || result == nil {
		return nil
	}
	return jsonRoundTrip(result, out)
}

// dispatch routes one RPC call to its canned result. An unregistered method
// returns the standard -32601 "method not found" kiwiRPCError, matching
// real Kiwi/modernrpc behavior for an absent RPC — the same convention
// mockRPCServer uses in the test fixtures this dataset's shapes were pulled
// from.
func (g *kiwiDemoGenerator) dispatch(method string, params []any) (any, error) {
	filter := demoFilterParam(params)
	switch method {
	case "Auth.login":
		return kiwiDemoSessionID, nil
	case "User.filter":
		return g.users, nil
	case "TestCase.filter":
		return g.filterTestCases(filter), nil
	case "TestPlan.filter":
		return g.plans, nil
	case "TestRun.filter":
		return g.filterTestRuns(filter), nil
	case "TestExecution.filter":
		return g.filterTestExecutions(filter), nil
	case "Component.filter":
		return g.components, nil
	case "Version.filter":
		return g.versions, nil
	case "TestCaseStatus.filter":
		return g.statuses, nil
	case "Priority.filter":
		return g.priorities, nil
	case "Requirement.filter":
		// Also the requirements-plugin detection probe (caps.go's
		// requirementsProbeMethod): a nil error here means "present", so the
		// demo always reports the requirements plugin as installed.
		return g.requirements, nil
	case "Requirement.coverage":
		return g.coverage(params)
	case "ReviewRequest.filter":
		// The review-plugin detection probe (caps.go's reviewProbeMethod).
		// The demo reports it present too (P4.4 brief: "report BOTH plugins
		// present so requirements + link-types show") even though this
		// package doesn't implement any review-plugin read path yet
		// (Adapter.hasReviewPlugin is cached but unexposed — see adapter.go).
		return []any{}, nil
	default:
		return nil, &kiwiRPCError{Code: methodNotFoundCode, Message: "Method not found: " + method}
	}
}

// filterTestCases honors the structural filters adapter.go actually sends
// (pk, pk__in, plan) so single-case fetches, ListTestsBasic, and per-plan
// membership all behave correctly. Product-name scoping
// (category__product__name) is deliberately NOT applied as a narrowing
// filter: unlike a real Kiwi instance the demo has exactly one product, and
// requiring an exact profile-project-key match would make the demo appear
// empty for any profile that doesn't happen to use the literal string
// "DEMO" as its project key — worse for a demo than always serving the
// fixed dataset. This mirrors the existing Xray demo (internal/jira/demo.go),
// which likewise never excludes data based on the requested project key.
func (g *kiwiDemoGenerator) filterTestCases(filter map[string]any) []kiwiTestCase {
	if id, ok := demoIntParam(filter, "pk"); ok {
		for _, tc := range g.testCases {
			if tc.ID == id {
				return []kiwiTestCase{tc}
			}
		}
		return []kiwiTestCase{}
	}
	if ids, ok := demoIntSliceParam(filter, "pk__in"); ok {
		want := make(map[int]bool, len(ids))
		for _, id := range ids {
			want[id] = true
		}
		out := make([]kiwiTestCase, 0, len(ids))
		for _, tc := range g.testCases {
			if want[tc.ID] {
				out = append(out, tc)
			}
		}
		return out
	}
	if planID, ok := demoIntParam(filter, "plan"); ok {
		want := make(map[int]bool, len(g.planCases[planID]))
		for _, id := range g.planCases[planID] {
			want[id] = true
		}
		out := make([]kiwiTestCase, 0, len(want))
		for _, tc := range g.testCases {
			if want[tc.ID] {
				out = append(out, tc)
			}
		}
		return out
	}
	return g.testCases
}

// filterTestRuns honors "pk" (ExecPlans) and "pk__in" (TestExecutionsForTest)
// -- the only two ways adapter.go narrows TestRun.filter beyond product
// scoping, which (like filterTestCases) is not applied as a narrowing
// filter in the demo.
func (g *kiwiDemoGenerator) filterTestRuns(filter map[string]any) []kiwiTestRun {
	if id, ok := demoIntParam(filter, "pk"); ok {
		for _, r := range g.runs {
			if r.ID == id {
				return []kiwiTestRun{r}
			}
		}
		return []kiwiTestRun{}
	}
	if ids, ok := demoIntSliceParam(filter, "pk__in"); ok {
		want := make(map[int]bool, len(ids))
		for _, id := range ids {
			want[id] = true
		}
		out := make([]kiwiTestRun, 0, len(ids))
		for _, r := range g.runs {
			if want[r.ID] {
				out = append(out, r)
			}
		}
		return out
	}
	return g.runs
}

// filterTestExecutions honors "run" (ListContainers/GetTestRuns) and "case"
// (TestExecutionsForTest) -- the only two query keys adapter.go sends.
func (g *kiwiDemoGenerator) filterTestExecutions(filter map[string]any) []kiwiTestExecution {
	if runID, ok := demoIntParam(filter, "run"); ok {
		out := make([]kiwiTestExecution, 0)
		for _, e := range g.execs {
			if e.Run == runID {
				out = append(out, e)
			}
		}
		return out
	}
	if caseID, ok := demoIntParam(filter, "case"); ok {
		out := make([]kiwiTestExecution, 0)
		for _, e := range g.execs {
			if e.Case == caseID {
				out = append(out, e)
			}
		}
		return out
	}
	return g.execs
}

// coverage serves Requirement.coverage(requirement_id), which takes a
// single positional scalar id (not a filter dict — see requirements.go's
// doc comment on the real call), in the same {id, link_count, suspect_count,
// link_types} shape requirements_test.go's fixture uses, built from
// g.reqLinks so it degrades to zero links for an unknown requirement id
// rather than erroring.
func (g *kiwiDemoGenerator) coverage(params []any) (any, error) {
	if len(params) == 0 {
		return nil, &kiwiRPCError{Code: -32602, Message: "Requirement.coverage: missing requirement id"}
	}
	id, ok := demoScalarInt(params[0])
	if !ok {
		return nil, &kiwiRPCError{Code: -32602, Message: "Requirement.coverage: invalid requirement id"}
	}
	links := g.reqLinks[id]
	linkTypes := make(map[string]any, len(links))
	total := 0
	for lt, ids := range links {
		linkTypes[lt] = ids
		total += len(ids)
	}
	return map[string]any{
		"id":            id,
		"link_count":    total,
		"suspect_count": 0,
		"link_types":    linkTypes,
	}, nil
}

// --- filter-dict helpers ---
//
// The demo dispatch runs BEFORE the real transport's json.Marshal step, so
// params[0] is still the original Go value the adapter built (e.g.
// map[string]any{"pk": 42} with a native Go int), not a JSON-decoded one —
// these helpers accept both native Go numeric types and the float64 a JSON
// round-trip would have produced, so the same helpers stay correct if a
// future caller ever feeds them post-decode values.

func demoFilterParam(params []any) map[string]any {
	if len(params) == 0 {
		return nil
	}
	f, _ := params[0].(map[string]any)
	return f
}

func demoIntParam(filter map[string]any, key string) (int, bool) {
	if filter == nil {
		return 0, false
	}
	v, ok := filter[key]
	if !ok {
		return 0, false
	}
	return demoScalarInt(v)
}

func demoIntSliceParam(filter map[string]any, key string) ([]int, bool) {
	if filter == nil {
		return nil, false
	}
	v, ok := filter[key]
	if !ok {
		return nil, false
	}
	switch s := v.(type) {
	case []int:
		return s, true
	case []any:
		out := make([]int, 0, len(s))
		for _, e := range s {
			if n, ok := demoScalarInt(e); ok {
				out = append(out, n)
			}
		}
		return out, true
	}
	return nil, false
}

func demoScalarInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

// jsonRoundTrip marshals in, then unmarshals the bytes into out (a pointer)
// -- the same two-step decode a real RPC response goes through in
// Client.call, so a demo result exercises identical lenient-decoder logic
// (e.g. kiwiNames) a live Kiwi payload would.
func jsonRoundTrip(in, out any) error {
	raw, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("kiwi demo: marshal result: %w", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("kiwi demo: decode result: %w", err)
	}
	return nil
}
