package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRealSubTaskExecutionsCarryParent exercises the live path: ListContainers
// searches the "Sub Test Execution" issue type separately, tags the results as
// Kind=testexec, and captures the parent key + issue type from the search
// response.
func TestRealSubTaskExecutionsCarryParent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/2/field":
			// The testexec search resolves the Test Environments custom field id;
			// this instance has no such field, so the env read degrades to none.
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		case r.URL.Path == "/rest/api/2/issuetype":
			// Sub-task Test Execution discovery lists the instance issue types.
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"name": "Test Execution", "subtask": false},
				{"name": "Sub Test Execution", "subtask": true},
			})
		case r.URL.Path == "/rest/api/2/search":
			jql := r.URL.Query().Get("jql")
			if strings.Contains(jql, `issuetype = "Sub Test Execution"`) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"total": 1,
					"issues": []map[string]any{{
						"key": "QA-50",
						"fields": map[string]any{
							"summary":   "Sub exec for story",
							"status":    map[string]any{"name": "Open"},
							"issuetype": map[string]any{"name": "Sub Test Execution"},
							"parent":    map[string]any{"key": "QA-9"},
						},
					}},
				})
				return
			}
			// The standalone Test Set / Plan / Execution searches return nothing.
			_ = json.NewEncoder(w).Encode(map[string]any{"total": 0, "issues": []any{}})
		case strings.HasPrefix(r.URL.Path, "/rest/raven/2.0/api/"):
			// Membership endpoint: no members for the sub-task execution.
			_, _ = w.Write([]byte("[]"))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "t")
	containers, _, err := c.ListContainers(context.Background(), "QA", nil)
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}

	var sub *Container
	for i := range containers {
		if containers[i].Key == "QA-50" {
			sub = &containers[i]
		}
	}
	if sub == nil {
		t.Fatalf("sub-task execution QA-50 not found in %d containers", len(containers))
	}
	if sub.Kind != KindTestExec {
		t.Errorf("Kind = %q, want %q", sub.Kind, KindTestExec)
	}
	if sub.ParentKey != "QA-9" {
		t.Errorf("ParentKey = %q, want QA-9", sub.ParentKey)
	}
	if sub.IssueType != "Sub Test Execution" {
		t.Errorf("IssueType = %q, want Sub Test Execution", sub.IssueType)
	}
}
