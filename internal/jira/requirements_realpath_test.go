package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

// recordedCall captures one HTTP request the live UpdateTestRequirements makes.
type recordedCall struct {
	method string
	path   string
	body   map[string]any
}

// TestRealUpdateTestRequirementsLinksAndUnlinks exercises the live
// UpdateTestRequirements path against a mock Jira: removeLinkIDs become
// DELETE /rest/api/2/issueLink/{id} and add keys become POST
// /rest/api/2/issueLink with the resolved coverage link type and Test as
// outwardIssue.
func TestRealUpdateTestRequirementsLinksAndUnlinks(t *testing.T) {
	var calls []recordedCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve the link-type discovery endpoint so resolveRequirementLinkType
		// can run without recording a spurious call.
		if r.Method == http.MethodGet && r.URL.Path == "/rest/api/2/issueLinkType" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"issueLinkTypes":[{"name":"Tested By"},{"name":"Tests"},{"name":"Relates"}]}`))
			return
		}
		c := recordedCall{method: r.Method, path: r.URL.Path}
		if r.Method == http.MethodPost {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &c.body)
		}
		calls = append(calls, c)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	err := newTestClient(srv).UpdateTestRequirements(
		context.Background(), "QA-1", []string{"PRD-1", "PRD-2"}, []string{"100", "101"})
	if err != nil {
		t.Fatalf("UpdateTestRequirements: %v", err)
	}

	// Two deletes + two posts, in remove-then-add order.
	if len(calls) != 4 {
		t.Fatalf("want 4 calls, got %d: %+v", len(calls), calls)
	}

	var deletes, posts []recordedCall
	for _, c := range calls {
		switch c.method {
		case http.MethodDelete:
			deletes = append(deletes, c)
		case http.MethodPost:
			posts = append(posts, c)
		default:
			t.Errorf("unexpected method %s", c.method)
		}
	}

	if len(deletes) != 2 {
		t.Fatalf("want 2 deletes, got %+v", deletes)
	}
	gotDelPaths := []string{deletes[0].path, deletes[1].path}
	sort.Strings(gotDelPaths)
	wantDelPaths := []string{"/rest/api/2/issueLink/100", "/rest/api/2/issueLink/101"}
	for i := range wantDelPaths {
		if gotDelPaths[i] != wantDelPaths[i] {
			t.Errorf("delete path[%d] = %q, want %q", i, gotDelPaths[i], wantDelPaths[i])
		}
	}

	if len(posts) != 2 {
		t.Fatalf("want 2 posts, got %+v", posts)
	}
	byReq := map[string]map[string]any{}
	for _, p := range posts {
		if p.path != "/rest/api/2/issueLink" {
			t.Errorf("post path = %q, want /rest/api/2/issueLink", p.path)
		}
		typ, _ := p.body["type"].(map[string]any)
		// resolveRequirementLinkType prefers "testedby" so "Tested By" wins.
		if typ == nil || typ["name"] != "Tested By" {
			t.Errorf("post type = %+v, want name=Tested By", p.body["type"])
		}
		// Direction: the Test is the inward issue and the requirement the
		// outward issue, so the requirement renders the link as "tested by".
		in, _ := p.body["inwardIssue"].(map[string]any)
		if in == nil || in["key"] != "QA-1" {
			t.Errorf("post inwardIssue = %+v, want key=QA-1 (the Test)", p.body["inwardIssue"])
		}
		out, _ := p.body["outwardIssue"].(map[string]any)
		if out == nil {
			t.Fatalf("post missing outwardIssue: %+v", p.body)
		}
		byReq[out["key"].(string)] = p.body
	}
	if _, ok := byReq["PRD-1"]; !ok {
		t.Errorf("missing POST linking PRD-1")
	}
	if _, ok := byReq["PRD-2"]; !ok {
		t.Errorf("missing POST linking PRD-2")
	}
}

// TestRealUpdateTestRequirementsPropagatesError verifies a non-2xx from Jira
// surfaces as an error (so the pending change is retried, not reported as
// success).
func TestRealUpdateTestRequirementsPropagatesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	if err := newTestClient(srv).UpdateTestRequirements(
		context.Background(), "QA-1", []string{"PRD-1"}, nil); err == nil {
		t.Fatal("want error from a 500 POST, got nil")
	}

	if err := newTestClient(srv).UpdateTestRequirements(
		context.Background(), "QA-1", nil, []string{"100"}); err == nil {
		t.Fatal("want error from a 500 DELETE, got nil")
	}
}

// TestResolveRequirementLinkTypeByDirection reproduces the real Jira DC
// instance shape (#275): the coverage link type is NAMED "Tests" with inward
// label "tested by" / outward "tests", and NO link type is named "tested by".
// Auto-resolve must pick "Tests" by matching the direction label, and POST it
// as type.name (a name that actually exists), not the direction "tested by".
func TestResolveRequirementLinkTypeByDirection(t *testing.T) {
	var posted map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/rest/api/2/issueLinkType" {
			w.Header().Set("Content-Type", "application/json")
			// No type named "tested by"/"Tests"-first; "tested by" only appears
			// as the inward label of the "Tests" type. "Relates" is listed
			// first to prove name-order does not decide the match.
			_, _ = w.Write([]byte(`{"issueLinkTypes":[` +
				`{"name":"Relates","inward":"is related to","outward":"relates to"},` +
				`{"name":"Blockers","inward":"is blocked by","outward":"blocks"},` +
				`{"name":"Tests","inward":"tested by","outward":"tests"}]}`))
			return
		}
		if r.Method == http.MethodPost {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &posted)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	if err := newTestClient(srv).UpdateTestRequirements(
		context.Background(), "QA-1", []string{"PRD-1"}, nil); err != nil {
		t.Fatalf("UpdateTestRequirements: %v", err)
	}
	typ, _ := posted["type"].(map[string]any)
	if typ == nil || typ["name"] != "Tests" {
		t.Fatalf("post type = %+v, want name=Tests (resolved by direction, not the 'tested by' label)", posted["type"])
	}
	// Direction: the Test is the inward issue and the requirement the outward
	// issue, so the requirement renders the link as "tested by" (verified live).
	if in, _ := posted["inwardIssue"].(map[string]any); in == nil || in["key"] != "QA-1" {
		t.Errorf("post inwardIssue = %+v, want key=QA-1 (the Test)", posted["inwardIssue"])
	}
	if out, _ := posted["outwardIssue"].(map[string]any); out == nil || out["key"] != "PRD-1" {
		t.Errorf("post outwardIssue = %+v, want key=PRD-1 (the requirement)", posted["outwardIssue"])
	}
}

// TestListIssueLinkTypeDetails verifies the dropdown source parses inward/
// outward labels live and that demo mode surfaces the "Tests" coverage type.
func TestListIssueLinkTypeDetails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issueLinkTypes":[{"name":"Tests","inward":"tested by","outward":"tests"}]}`))
	}))
	defer srv.Close()

	got, err := newTestClient(srv).ListIssueLinkTypeDetails(context.Background())
	if err != nil {
		t.Fatalf("ListIssueLinkTypeDetails (live): %v", err)
	}
	if len(got) != 1 || got[0].Name != "Tests" || got[0].Inward != "tested by" || got[0].Outward != "tests" {
		t.Fatalf("live parse = %+v, want [{Tests tested by tests}]", got)
	}

	demo := &Client{baseURL: "demo", token: "t", http: srv.Client()}
	dd, err := demo.ListIssueLinkTypeDetails(context.Background())
	if err != nil {
		t.Fatalf("ListIssueLinkTypeDetails (demo): %v", err)
	}
	var foundTests bool
	for _, lt := range dd {
		if lt.Name == "Tests" && lt.Inward == "tested by" {
			foundTests = true
		}
	}
	if !foundTests {
		t.Fatalf("demo details = %+v, want a Tests/tested by entry", dd)
	}
}

// TestRealUpdateTestRequirementsNoOps verifies that an empty add+remove (and the
// demo URL) make no HTTP calls.
func TestRealUpdateTestRequirementsNoOps(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	// Empty add + remove against a live URL: nothing to do, no calls.
	if err := newTestClient(srv).UpdateTestRequirements(
		context.Background(), "QA-1", nil, nil); err != nil {
		t.Fatalf("empty UpdateTestRequirements: %v", err)
	}
	// Blank entries are skipped defensively.
	if err := newTestClient(srv).UpdateTestRequirements(
		context.Background(), "QA-1", []string{"  "}, []string{""}); err != nil {
		t.Fatalf("blank UpdateTestRequirements: %v", err)
	}
	if calls != 0 {
		t.Fatalf("want 0 HTTP calls for empty/blank input, got %d", calls)
	}

	// Demo URL short-circuits before any HTTP.
	demo := &Client{baseURL: "demo", token: "t", http: srv.Client()}
	if err := demo.UpdateTestRequirements(
		context.Background(), "QA-1", []string{"PRD-1"}, []string{"100"}); err != nil {
		t.Fatalf("demo UpdateTestRequirements: %v", err)
	}
	if calls != 0 {
		t.Fatalf("demo path should make no calls, got %d", calls)
	}
}
