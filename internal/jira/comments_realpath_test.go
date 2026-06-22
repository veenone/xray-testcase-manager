package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRealAddCommentPostsBody exercises the live AddComment path against a mock
// Jira: it POSTs to /rest/api/2/issue/{key}/comment with a plain-text
// {"body": "..."} payload.
func TestRealAddCommentPostsBody(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	err := newTestClient(srv).AddComment(context.Background(), "TEST-1", "Reviewed: approved")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("want POST, got %s", gotMethod)
	}
	if gotPath != "/rest/api/2/issue/TEST-1/comment" {
		t.Errorf("unexpected path %q", gotPath)
	}
	if b, ok := gotBody["body"].(string); !ok || b != "Reviewed: approved" {
		t.Errorf("unexpected body payload: %+v", gotBody)
	}
}

// TestRealAddCommentSurfacesError asserts a non-2xx response becomes an error.
func TestRealAddCommentSurfacesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errorMessages":["bad comment"]}`))
	}))
	defer srv.Close()

	if err := newTestClient(srv).AddComment(context.Background(), "TEST-1", "nope"); err == nil {
		t.Fatal("want error on 400 response, got nil")
	}
}

// TestRealAddCommentDemoMakesNoCall asserts a demo URL short-circuits to a no-op
// without issuing any HTTP request.
func TestRealAddCommentDemoMakesNoCall(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := &Client{baseURL: "demo", token: "t", http: srv.Client()}
	if err := c.AddComment(context.Background(), "TEST-1", "anything"); err != nil {
		t.Fatalf("AddComment (demo): %v", err)
	}
	if called {
		t.Error("demo URL must not make an HTTP call")
	}
}
