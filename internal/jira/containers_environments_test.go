package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestParseOptionValues covers the multi-value shapes the Xray Test Environments
// custom field can take: an array of option objects, an array of bare strings,
// and the empty cases (null, absent, garbage) which all yield an empty slice.
// Empty entries are skipped.
func TestParseOptionValues(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{"array of objects", `[{"value":"Staging"},{"value":"Chrome"}]`, []string{"Staging", "Chrome"}},
		{"array of bare strings", `["Staging","Chrome"]`, []string{"Staging", "Chrome"}},
		{"objects with empty value skipped", `[{"value":"Staging"},{"value":""},{"value":"  "}]`, []string{"Staging"}},
		{"bare strings with empties skipped", `["Staging","","  ","Chrome"]`, []string{"Staging", "Chrome"}},
		{"empty array", `[]`, nil},
		{"null", `null`, nil},
		{"absent (empty raw)", ``, nil},
		{"garbage", `{not json`, nil},
		{"single object (not array)", `{"value":"Staging"}`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseOptionValues(json.RawMessage(tc.raw))
			if len(got) != len(tc.want) {
				t.Fatalf("parseOptionValues(%q) = %v, want %v", tc.raw, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("parseOptionValues(%q)[%d] = %q, want %q", tc.raw, i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestListContainersReadsEnvironments drives the live container search for Test
// Executions against a mock Jira serving both /rest/api/2/field (so the Test
// Environments field id resolves) and /rest/api/2/search. It asserts the resolved
// custom field id is appended to the executions' fields query param (and NOT to
// the Test Set / Plan searches) and that the multi-value is mapped onto
// Container.Environments.
func TestListContainersReadsEnvironments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/field":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "customfield_10200", "name": "Test Environments", "custom": true},
			})
		case "/rest/api/2/search":
			jql := r.URL.Query().Get("jql")
			fields := r.URL.Query().Get("fields")
			switch {
			case strings.Contains(jql, `issuetype = "Test Execution"`):
				if !strings.Contains(fields, "customfield_10200") {
					t.Errorf("execution search fields should include customfield_10200, got %q", fields)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"total": 2,
					"issues": []map[string]any{
						{
							"key": "QA-100",
							"fields": map[string]any{
								"summary":           "Regression run",
								"status":            map[string]any{"name": "Open"},
								"customfield_10200": []map[string]any{{"value": "Staging"}, {"value": "Chrome"}},
							},
						},
						{
							// Bare-string multi-value shape, defensively parsed.
							"key": "QA-101",
							"fields": map[string]any{
								"summary":           "Smoke run",
								"customfield_10200": []string{"Prod"},
							},
						},
					},
				})
			case strings.Contains(jql, `issuetype = "Test Set"`) || strings.Contains(jql, `issuetype = "Test Plan"`):
				// Sets / Plans must not request the environments field.
				if strings.Contains(fields, "customfield_10200") {
					t.Errorf("set/plan search should not request environments field, got %q", fields)
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
	got100 := byKey["QA-100"].Environments
	if len(got100) != 2 || got100[0] != "Staging" || got100[1] != "Chrome" {
		t.Errorf("QA-100 Environments = %v, want [Staging Chrome]", got100)
	}
	got101 := byKey["QA-101"].Environments
	if len(got101) != 1 || got101[0] != "Prod" {
		t.Errorf("QA-101 Environments (bare strings) = %v, want [Prod]", got101)
	}
}

// TestSetContainerEnvironmentsWritesField asserts the live write resolves the
// field id and PUTs /rest/api/2/issue/{key} with fields[<id>] shaped as an array
// of option objects.
func TestSetContainerEnvironmentsWritesField(t *testing.T) {
	var putBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/2/field":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "customfield_10200", "name": "Test Environments", "custom": true},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/2/issue/QA-100":
			if err := json.NewDecoder(r.Body).Decode(&putBody); err != nil {
				t.Errorf("decode PUT body: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	err := newTestClient(srv).SetContainerEnvironments(context.Background(), "QA-100", []string{"Staging", "", "Chrome"})
	if err != nil {
		t.Fatalf("SetContainerEnvironments: %v", err)
	}

	fields, ok := putBody["fields"].(map[string]any)
	if !ok {
		t.Fatalf("PUT body missing fields object: %#v", putBody)
	}
	raw, ok := fields["customfield_10200"].([]any)
	if !ok {
		t.Fatalf("fields[customfield_10200] not an array: %#v", fields["customfield_10200"])
	}
	// Empty entry must have been skipped.
	if len(raw) != 2 {
		t.Fatalf("want 2 option objects, got %d: %#v", len(raw), raw)
	}
	want := []string{"Staging", "Chrome"}
	for i, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("option %d not an object: %#v", i, item)
		}
		if obj["value"] != want[i] {
			t.Errorf("option %d value = %v, want %q", i, obj["value"], want[i])
		}
	}
}

// TestSetContainerEnvironmentsNoFieldErrors asserts that an instance with no
// Test Environments custom field returns a clear error rather than silently
// dropping the user's edit.
func TestSetContainerEnvironmentsNoFieldErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/2/field" {
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "customfield_10101", "name": "Automation Status", "custom": true},
			})
			return
		}
		t.Errorf("unexpected request %s %s (no PUT expected when field unresolved)", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	err := newTestClient(srv).SetContainerEnvironments(context.Background(), "QA-100", []string{"Staging"})
	if err == nil {
		t.Fatal("expected an error when no Test Environments field exists, got nil")
	}
	if !strings.Contains(err.Error(), "Test Environments") {
		t.Errorf("error should mention Test Environments, got %v", err)
	}
}
