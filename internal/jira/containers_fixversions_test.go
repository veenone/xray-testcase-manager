package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestListContainersReadsFixVersions drives the live container search for Test
// Executions and asserts the standard Jira fixVersions field is requested on the
// execution search (and NOT on the Test Set / Plan searches) and that the
// {name} array is mapped, in order, onto Container.FixVersions.
func TestListContainersReadsFixVersions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/field":
			// No custom fields; the environments read degrades to none and does
			// not interfere with the fixVersions assertions.
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		case "/rest/api/2/issuetype":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"name": "Test Execution", "subtask": false},
				{"name": "Sub Test Execution", "subtask": true},
			})
		case "/rest/api/2/search":
			jql := r.URL.Query().Get("jql")
			fields := r.URL.Query().Get("fields")
			switch {
			case strings.Contains(jql, `issuetype = "Test Execution"`):
				if !strings.Contains(fields, "fixVersions") {
					t.Errorf("execution search fields should include fixVersions, got %q", fields)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"total": 2,
					"issues": []map[string]any{
						{
							"key": "QA-100",
							"fields": map[string]any{
								"summary": "Regression run",
								"status":  map[string]any{"name": "Open"},
								"fixVersions": []map[string]any{
									{"name": "1.6.0"},
									{"name": "1.5.0"},
								},
							},
						},
						{
							// No fixVersions -> empty.
							"key": "QA-101",
							"fields": map[string]any{
								"summary": "Smoke run",
							},
						},
					},
				})
			case strings.Contains(jql, `issuetype = "Test Set"`) || strings.Contains(jql, `issuetype = "Test Plan"`):
				if strings.Contains(fields, "fixVersions") {
					t.Errorf("set/plan search should not request fixVersions, got %q", fields)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"total": 0, "issues": []any{}})
			default:
				// Sub Test Execution search and anything else: empty.
				_ = json.NewEncoder(w).Encode(map[string]any{"total": 0, "issues": []any{}})
			}
		default:
			if strings.HasPrefix(r.URL.Path, "/rest/raven/2.0/api/") {
				_, _ = w.Write([]byte("[]"))
				return
			}
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	containers, _, err := newTestClient(srv).ListContainers(context.Background(), "QA", nil)
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	byKey := map[string]Container{}
	for _, ct := range containers {
		byKey[ct.Key] = ct
	}
	got100 := byKey["QA-100"].FixVersions
	if len(got100) != 2 || got100[0] != "1.6.0" || got100[1] != "1.5.0" {
		t.Errorf("QA-100 FixVersions = %v, want [1.6.0 1.5.0] in order", got100)
	}
	if len(byKey["QA-101"].FixVersions) != 0 {
		t.Errorf("QA-101 FixVersions = %v, want none", byKey["QA-101"].FixVersions)
	}
}
