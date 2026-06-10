package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCreateTestUsesResolvedTypeAndPostsIssue verifies CreateTest resolves the
// plain "Test" issue type (not Test Set / Test Plan / Test Execution) and POSTs
// the mapped fields to /rest/api/2/issue, returning the created key.
func TestCreateTestUsesResolvedTypeAndPostsIssue(t *testing.T) {
	var gotFields map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/issuetype":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "9", "name": "Test Set"},
				{"id": "5", "name": "Test"},
				{"id": "7", "name": "Test Execution"},
			})
		case "/rest/api/2/issue":
			body, _ := io.ReadAll(r.Body)
			var payload struct {
				Fields map[string]any `json:"fields"`
			}
			_ = json.Unmarshal(body, &payload)
			gotFields = payload.Fields
			_ = json.NewEncoder(w).Encode(map[string]string{"key": "QA-100"})
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	key, err := newTestClient(srv).CreateTest(
		context.Background(), "QA", "New login test", "A description",
		"High", []string{"smoke", "api"}, []string{"Auth"})
	if err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	if key != "QA-100" {
		t.Fatalf("key = %q, want QA-100", key)
	}
	issuetype, _ := gotFields["issuetype"].(map[string]any)
	if issuetype["id"] != "5" {
		t.Fatalf("create used issuetype %v, want id 5 (plain Test)", gotFields["issuetype"])
	}
	if gotFields["summary"] != "New login test" {
		t.Errorf("summary = %v", gotFields["summary"])
	}
	prio, _ := gotFields["priority"].(map[string]any)
	if prio["name"] != "High" {
		t.Errorf("priority = %v, want name High", gotFields["priority"])
	}
	labels, _ := gotFields["labels"].([]any)
	if len(labels) != 2 || labels[0] != "smoke" {
		t.Errorf("labels = %v, want [smoke api]", gotFields["labels"])
	}
	comps, _ := gotFields["components"].([]any)
	if len(comps) != 1 {
		t.Fatalf("components = %v, want one", gotFields["components"])
	}
	c0, _ := comps[0].(map[string]any)
	if c0["name"] != "Auth" {
		t.Errorf("component = %v, want name Auth", comps[0])
	}
}
