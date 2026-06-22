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
// /rest/api/2/issueLink with the coverage link type and Test as outwardIssue.
func TestRealUpdateTestRequirementsLinksAndUnlinks(t *testing.T) {
	var calls []recordedCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		if typ == nil || typ["name"] != "Tests" {
			t.Errorf("post type = %+v, want name=Tests", p.body["type"])
		}
		out, _ := p.body["outwardIssue"].(map[string]any)
		if out == nil || out["key"] != "QA-1" {
			t.Errorf("post outwardIssue = %+v, want key=QA-1 (the Test)", p.body["outwardIssue"])
		}
		in, _ := p.body["inwardIssue"].(map[string]any)
		if in == nil {
			t.Fatalf("post missing inwardIssue: %+v", p.body)
		}
		byReq[in["key"].(string)] = p.body
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
