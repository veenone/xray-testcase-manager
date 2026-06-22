package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestParseOptionValue covers the option-value shapes a Jira single-select
// custom field (the Xray Test Type) can take: an option object {"value": ...},
// a bare string, and the empty cases (null, absent, an object without a value,
// malformed JSON) which all yield "".
func TestParseOptionValue(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"option object", `{"value":"Manual"}`, "Manual"},
		{"bare string", `"Generic"`, "Generic"},
		{"null", `null`, ""},
		{"absent (empty raw)", ``, ""},
		{"object without value", `{"id":"10000"}`, ""},
		{"garbage", `{not json`, ""},
		{"number", `42`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseOptionValue(json.RawMessage(tc.raw))
			if got != tc.want {
				t.Errorf("parseOptionValue(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestResolveCustomFieldID exercises the live resolver against a mock
// /rest/api/2/field: it returns the id of the custom "Test Type" field
// (case-insensitive, trimmed) and ignores a system field of the same name. It
// also asserts a second lookup is served from cache (no second request) and
// that an unknown field name resolves to "".
func TestResolveCustomFieldID(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/2/field" {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		calls++
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "summary", "name": "Summary", "custom": false},
			// A system field that happens to share the name must be skipped.
			{"id": "system_testtype", "name": "Test Type", "custom": false},
			{"id": "customfield_10100", "name": "Test Type", "custom": true},
			{"id": "customfield_10101", "name": "Automation Status", "custom": true},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv)

	// Case-insensitive / trimmed match, custom field only.
	id, err := c.resolveCustomFieldID(context.Background(), "  test type ")
	if err != nil {
		t.Fatalf("resolveCustomFieldID: %v", err)
	}
	if id != "customfield_10100" {
		t.Errorf("want customfield_10100, got %q", id)
	}

	// testTypeFieldID is served from cache (no second /field fetch).
	id2, err := c.testTypeFieldID(context.Background())
	if err != nil {
		t.Fatalf("testTypeFieldID: %v", err)
	}
	if id2 != "customfield_10100" {
		t.Errorf("cached lookup want customfield_10100, got %q", id2)
	}
	if calls != 1 {
		t.Errorf("expected exactly 1 /field fetch (cached), got %d", calls)
	}

	// No-match resolves to "".
	none, err := c.resolveCustomFieldID(context.Background(), "Nonexistent Field")
	if err != nil {
		t.Fatalf("resolveCustomFieldID no-match: %v", err)
	}
	if none != "" {
		t.Errorf("no-match should return empty id, got %q", none)
	}
}

// TestSearchTestsPageReadsExecType drives the full live SearchTestsPage path
// against a mock Jira serving both /rest/api/2/field (so the Test Type field id
// resolves) and /rest/api/2/search. It asserts the resolved custom field id is
// appended to the fields query param and that the option value is mapped onto
// Test.ExecType.
func TestSearchTestsPageReadsExecType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/field":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "customfield_10100", "name": "Test Type", "custom": true},
			})
		case "/rest/api/2/search":
			fields := r.URL.Query().Get("fields")
			if !strings.Contains(fields, "customfield_10100") {
				t.Errorf("expected fields to include customfield_10100, got %q", fields)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total": 2,
				"issues": []map[string]any{
					{
						"id":  "1001",
						"key": "QA-1",
						"fields": map[string]any{
							"summary":           "login works",
							"status":            map[string]any{"name": "Ready"},
							"customfield_10100": map[string]any{"value": "Manual"},
						},
					},
					{
						// Bare-string option shape, defensively parsed.
						"id":  "1002",
						"key": "QA-2",
						"fields": map[string]any{
							"summary":           "logout works",
							"customfield_10100": "Automated",
						},
					},
				},
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	tests, total, err := newTestClient(srv).SearchTestsPage(
		context.Background(), "QA", "", "", 0, 50)
	if err != nil {
		t.Fatalf("SearchTestsPage: %v", err)
	}
	if total != 2 || len(tests) != 2 {
		t.Fatalf("want 2 tests, got total=%d len=%d", total, len(tests))
	}
	byKey := map[string]Test{}
	for _, tt := range tests {
		byKey[tt.Key] = tt
	}
	if got := byKey["QA-1"].ExecType; got != "Manual" {
		t.Errorf("QA-1 ExecType want Manual, got %q", got)
	}
	if got := byKey["QA-2"].ExecType; got != "Automated" {
		t.Errorf("QA-2 ExecType (bare string) want Automated, got %q", got)
	}
}

// TestGetTestFieldsReadsExecType asserts the conflict re-fetch path also carries
// ExecType: it resolves the Test Type field, requests it on the issue fetch, and
// maps the option value.
func TestGetTestFieldsReadsExecType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/field":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "customfield_10100", "name": "Test Type", "custom": true},
			})
		case "/rest/api/2/issue/QA-1":
			if f := r.URL.Query().Get("fields"); !strings.Contains(f, "customfield_10100") {
				t.Errorf("expected issue fetch fields to include customfield_10100, got %q", f)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":  "1001",
				"key": "QA-1",
				"fields": map[string]any{
					"summary":           "login works",
					"updated":           "2026-01-01T00:00:00.000+0000",
					"customfield_10100": map[string]any{"value": "Cucumber"},
				},
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	tst, err := newTestClient(srv).GetTestFields(context.Background(), "QA-1")
	if err != nil {
		t.Fatalf("GetTestFields: %v", err)
	}
	if tst.ExecType != "Cucumber" {
		t.Errorf("ExecType want Cucumber, got %q", tst.ExecType)
	}
}

// TestExecTypeFieldValueResolvesAndShapes asserts the write helper resolves the
// field id and returns the {"value": ...} payload Jira expects, and that an
// instance without the field degrades to ok=false (no error).
func TestExecTypeFieldValueResolvesAndShapes(t *testing.T) {
	withField := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "customfield_10100", "name": "Test Type", "custom": true},
		})
	}))
	defer withField.Close()

	id, value, ok, err := newTestClient(withField).ExecTypeFieldValue(context.Background(), "Generic")
	if err != nil {
		t.Fatalf("ExecTypeFieldValue: %v", err)
	}
	if !ok || id != "customfield_10100" {
		t.Fatalf("want resolved customfield_10100, got ok=%v id=%q", ok, id)
	}
	m, isMap := value.(map[string]string)
	if !isMap || m["value"] != "Generic" {
		t.Errorf("want value {value:Generic}, got %#v", value)
	}

	noField := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "customfield_10101", "name": "Automation Status", "custom": true},
		})
	}))
	defer noField.Close()

	_, _, ok2, err := newTestClient(noField).ExecTypeFieldValue(context.Background(), "Generic")
	if err != nil {
		t.Fatalf("ExecTypeFieldValue no-field: %v", err)
	}
	if ok2 {
		t.Errorf("expected ok=false when instance has no Test Type field")
	}
}
