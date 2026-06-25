package jira

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDemoSeedsSubTaskExecutions(t *testing.T) {
	c := NewClient("demo", "")
	containers, links, err := c.ListContainers(context.Background(), "DEMO", nil)
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	var subs []Container
	for _, ct := range containers {
		if ct.Kind == KindTestExec && ct.ParentKey != "" {
			subs = append(subs, ct)
		}
	}
	if len(subs) < 2 {
		t.Fatalf("want >=2 sub-task executions, got %d", len(subs))
	}
	for _, s := range subs {
		if !strings.HasPrefix(s.ParentKey, "DEMO-S-") {
			t.Errorf("sub-task %s has unexpected parent %q", s.Key, s.ParentKey)
		}
		if s.IssueType == "" {
			t.Errorf("sub-task %s missing issue type", s.Key)
		}
	}
	// Sub-task executions carry run links like standalone ones.
	linked := map[string]bool{}
	for _, l := range links {
		linked[l.ContainerKey] = true
	}
	for _, s := range subs {
		if !linked[s.Key] {
			t.Errorf("sub-task execution %s has no run links", s.Key)
		}
	}
}

// TestSubTaskTestExecDiscovery confirms the instance issue-type list drives
// sub-task Test Execution discovery: a subtask test-exec type is found and the
// standalone "Test Execution" (not a subtask) is excluded.
func TestSubTaskTestExecDiscovery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"name":"Test Execution","subtask":false},
			{"name":"Sub Test Execution","subtask":true},
			{"name":"Bug","subtask":false}
		]`))
	}))
	defer srv.Close()
	c := newTestClient(srv)
	got := c.subTaskTestExecIssueTypeNames(t.Context())
	if len(got) != 1 || got[0] != "Sub Test Execution" {
		t.Fatalf("expected [Sub Test Execution], got %v", got)
	}
	for _, n := range got {
		if !strings.Contains(normalizeTypeName(n), "testexecution") {
			t.Errorf("unexpected type %q", n)
		}
	}
}

// TestSubTaskTestExecDiscoveryRenamed confirms a renamed / localised subtask
// test-execution type is still discovered (the whole point of the fix).
func TestSubTaskTestExecDiscoveryRenamed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[
			{"name":"Test Execution","subtask":false},
			{"name":"Test Execution (Sub-task)","subtask":true}
		]`))
	}))
	defer srv.Close()
	c := newTestClient(srv)
	got := c.subTaskTestExecIssueTypeNames(t.Context())
	if len(got) != 1 || got[0] != "Test Execution (Sub-task)" {
		t.Fatalf("expected [Test Execution (Sub-task)], got %v", got)
	}
}

// TestSubTaskTestExecDiscoveryFallback confirms a listing failure falls back to
// the default issue type name.
func TestSubTaskTestExecDiscoveryFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := newTestClient(srv)
	got := c.subTaskTestExecIssueTypeNames(t.Context())
	if len(got) != 1 || got[0] != subTestExecIssueType {
		t.Fatalf("expected fallback [%s], got %v", subTestExecIssueType, got)
	}
}
