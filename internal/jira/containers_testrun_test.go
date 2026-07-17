package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// testRunIDHandler serves the resolveTestRunID lookup
// (GET /rest/raven/1.0/api/testrun?testExecIssueKey=...&testIssueKey=...)
// with a fixed run id, for tests that only care about the follow-up call.
func testRunIDHandler(t *testing.T, runID string) func(w http.ResponseWriter, r *http.Request) bool {
	return func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/rest/raven/1.0/api/testrun" {
			return false
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": runID})
		return true
	}
}

// TestAddTestRunDefectPostsBugKeyArray verifies AddTestRunDefect resolves the
// run id, then POSTs a JSON array body of the defect key to
// /rest/raven/1.0/api/testrun/{id}/defect.
func TestAddTestRunDefectPostsBugKeyArray(t *testing.T) {
	resolveRun := testRunIDHandler(t, "42")
	var gotPath, gotMethod, gotContentType string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if resolveRun(w, r) {
			return
		}
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := newTestClient(srv).AddTestRunDefect(context.Background(), "QA-TE-1", "QA-1", "BUG-1"); err != nil {
		t.Fatalf("AddTestRunDefect: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/rest/raven/1.0/api/testrun/42/defect" {
		t.Errorf("path = %q, want /rest/raven/1.0/api/testrun/42/defect", gotPath)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	var got []string
	if err := json.Unmarshal(gotBody, &got); err != nil {
		t.Fatalf("body not a JSON array: %v (body=%q)", err, gotBody)
	}
	if len(got) != 1 || got[0] != "BUG-1" {
		t.Errorf("body = %v, want [BUG-1]", got)
	}
}

// TestRemoveTestRunDefectDeletesWithKeyInPath verifies RemoveTestRunDefect
// resolves the run id, then issues a DELETE with the defect key in the path.
func TestRemoveTestRunDefectDeletesWithKeyInPath(t *testing.T) {
	resolveRun := testRunIDHandler(t, "42")
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if resolveRun(w, r) {
			return
		}
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := newTestClient(srv).RemoveTestRunDefect(context.Background(), "QA-TE-1", "QA-1", "BUG-1"); err != nil {
		t.Fatalf("RemoveTestRunDefect: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/rest/raven/1.0/api/testrun/42/defect/BUG-1" {
		t.Errorf("path = %q, want /rest/raven/1.0/api/testrun/42/defect/BUG-1", gotPath)
	}
}

// TestSetTestRunCommentSendsRawBody verifies SetTestRunComment resolves the
// run id, then PUTs the comment as RAW bytes (no JSON-encoding — no
// surrounding quotes) with Content-Type: application/json.
func TestSetTestRunCommentSendsRawBody(t *testing.T) {
	resolveRun := testRunIDHandler(t, "42")
	var gotPath, gotMethod, gotContentType string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if resolveRun(w, r) {
			return
		}
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	const comment = `looks good, retested on build 42`
	if err := newTestClient(srv).SetTestRunComment(context.Background(), "QA-TE-1", "QA-1", comment); err != nil {
		t.Fatalf("SetTestRunComment: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if gotPath != "/rest/raven/1.0/api/testrun/42/comment" {
		t.Errorf("path = %q, want /rest/raven/1.0/api/testrun/42/comment", gotPath)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if string(gotBody) != comment {
		t.Errorf("body = %q, want raw %q (no JSON-encoding / surrounding quotes)", gotBody, comment)
	}
	if len(gotBody) > 0 && (gotBody[0] == '"' || gotBody[len(gotBody)-1] == '"') {
		t.Errorf("body %q appears JSON-quoted; want the raw comment text", gotBody)
	}
}

// TestSetTestRunCommentEmptyClearsComment verifies an empty comment is sent
// as an empty body (which Xray treats as clearing the comment).
func TestSetTestRunCommentEmptyClearsComment(t *testing.T) {
	resolveRun := testRunIDHandler(t, "42")
	var gotBody []byte
	bodySet := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if resolveRun(w, r) {
			return
		}
		gotBody, _ = io.ReadAll(r.Body)
		bodySet = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := newTestClient(srv).SetTestRunComment(context.Background(), "QA-TE-1", "QA-1", ""); err != nil {
		t.Fatalf("SetTestRunComment: %v", err)
	}
	if !bodySet {
		t.Fatal("comment endpoint was never called")
	}
	if len(gotBody) != 0 {
		t.Errorf("body = %q, want empty to clear the comment", gotBody)
	}
}

// TestTestRunDefectAndCommentDemoNoOp verifies all three write methods
// short-circuit to a nil-error no-op in demo mode, making no HTTP call.
func TestTestRunDefectAndCommentDemoNoOp(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient("demo", "token")
	ctx := context.Background()

	if err := c.AddTestRunDefect(ctx, "QA-TE-1", "QA-1", "BUG-1"); err != nil {
		t.Errorf("AddTestRunDefect (demo): %v", err)
	}
	if err := c.RemoveTestRunDefect(ctx, "QA-TE-1", "QA-1", "BUG-1"); err != nil {
		t.Errorf("RemoveTestRunDefect (demo): %v", err)
	}
	if err := c.SetTestRunComment(ctx, "QA-TE-1", "QA-1", "a comment"); err != nil {
		t.Errorf("SetTestRunComment (demo): %v", err)
	}
	if called {
		t.Error("demo mode made an HTTP call; want a pure no-op")
	}
}
