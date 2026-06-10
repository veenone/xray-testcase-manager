package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestListPrioritiesScopedToTestIssueType verifies the live path reads the
// Test issue type's allowed priorities from createmeta (scoped to the resolved
// Test type), in order, de-duplicated — NOT the global priority scheme.
func TestListPrioritiesScopedToTestIssueType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/issuetype":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "9", "name": "Test Set"},
				{"id": "5", "name": "Test"},
			})
		case "/rest/api/2/issue/createmeta":
			q := r.URL.Query()
			if q.Get("issuetypeIds") != "5" {
				t.Errorf("createmeta issuetypeIds = %q, want 5 (the Test type)", q.Get("issuetypeIds"))
			}
			if q.Get("projectKeys") != "QA" {
				t.Errorf("createmeta projectKeys = %q, want QA", q.Get("projectKeys"))
			}
			if q.Get("expand") != "projects.issuetypes.fields" {
				t.Errorf("createmeta expand = %q", q.Get("expand"))
			}
			// Include a second issue type (Bug) carrying the GLOBAL priority list.
			// Some instances ignore the issuetypeIds filter and return every type;
			// ListPriorities must read ONLY the Test type's priorities, never Bug's.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{
					{"key": "QA", "issuetypes": []map[string]any{
						{"id": "1", "name": "Bug", "fields": map[string]any{
							"priority": map[string]any{"allowedValues": []map[string]any{
								{"name": "Highest"},
								{"name": "High"},
								{"name": "Medium"},
								{"name": "Low"},
								{"name": "Lowest"},
							}},
						}},
						{"id": "5", "name": "Test", "fields": map[string]any{
							"priority": map[string]any{"allowedValues": []map[string]any{
								{"name": "Blocker"},
								{"name": "High"},
								{"name": "Medium"},
								{"name": "Minor"},
								{"name": "Blocker"}, // duplicate — dropped
							}},
						}},
					}},
				},
			})
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	got, err := newTestClient(srv).ListPriorities(context.Background(), "QA")
	if err != nil {
		t.Fatalf("ListPriorities: %v", err)
	}
	want := []string{"Blocker", "High", "Medium", "Minor"}
	if len(got) != len(want) {
		t.Fatalf("priorities = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("priorities = %v, want %v", got, want)
		}
	}
}

// TestListPrioritiesDemoReturnsDemoSet confirms a demo client serves the demo
// priority list without any HTTP call.
func TestListPrioritiesDemoReturnsDemoSet(t *testing.T) {
	c := NewClient("demo", "t")
	got, err := c.ListPriorities(context.Background(), "PROJ")
	if err != nil {
		t.Fatalf("ListPriorities(demo): %v", err)
	}
	if len(got) == 0 {
		t.Fatal("demo priorities should be non-empty")
	}
	for _, p := range got {
		if p == "Lowest" {
			t.Errorf("demo priorities unexpectedly include 'Lowest': %v", got)
		}
	}
}
