package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// TestStringifyFieldValue covers the JSON shapes a Jira custom field value can
// take: option / user objects, arrays of either, bare string, number, and the
// empty cases (null, absent, unrecognised) which all yield "".
func TestStringifyFieldValue(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"option object value", `{"value":"Automated"}`, "Automated"},
		{"option object name only", `{"name":"High"}`, "High"},
		{"user object displayName", `{"displayName":"Jane Doe","name":"jdoe"}`, "Jane Doe"},
		{"user object name fallback", `{"name":"jdoe"}`, "jdoe"},
		{"array of options", `[{"value":"A"},{"value":"B"}]`, "A, B"},
		{"array of strings", `["X","Y"]`, "X, Y"},
		{"array of users", `[{"displayName":"Jane"},{"displayName":"John"}]`, "Jane, John"},
		{"bare string", `"hello"`, "hello"},
		{"integer number", `42`, "42"},
		{"float number", `3.5`, "3.5"},
		{"integer-valued float", `5.0`, "5"},
		{"null", `null`, ""},
		{"absent (empty raw)", ``, ""},
		{"empty object", `{}`, ""},
		{"garbage", `{not json`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stringifyFieldValue(json.RawMessage(tc.raw))
			if got != tc.want {
				t.Errorf("stringifyFieldValue(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestShapeCustomFieldValue covers the typed PUT mapping per schema type.
func TestShapeCustomFieldValue(t *testing.T) {
	if got := shapeCustomFieldValue("option", "Automated"); !reflect.DeepEqual(got, map[string]string{"value": "Automated"}) {
		t.Errorf("option shape = %#v", got)
	}
	if got := shapeCustomFieldValue("select", "High"); !reflect.DeepEqual(got, map[string]string{"value": "High"}) {
		t.Errorf("select shape = %#v", got)
	}
	if got := shapeCustomFieldValue("array", "A, B"); !reflect.DeepEqual(got, []map[string]string{{"value": "A"}, {"value": "B"}}) {
		t.Errorf("array shape = %#v", got)
	}
	if got := shapeCustomFieldValue("number", "12"); got != float64(12) {
		t.Errorf("number shape = %#v, want 12", got)
	}
	if got := shapeCustomFieldValue("number", "notanumber"); got != "notanumber" {
		t.Errorf("unparsable number should fall back to raw string, got %#v", got)
	}
	if got := shapeCustomFieldValue("string", "plain"); got != "plain" {
		t.Errorf("string shape = %#v", got)
	}
	if got := shapeCustomFieldValue("", "plain"); got != "plain" {
		t.Errorf("unknown type should default to raw string, got %#v", got)
	}
}

// TestListCustomFieldsFromCreatemeta drives the live ListCustomFields against a
// mock createmeta serving the Test issue type's fields, asserting only the
// customfield_* entries are emitted with name and type.
func TestListCustomFieldsFromCreatemeta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/2/issue/createmeta" {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"projects": []map[string]any{
				{
					"issuetypes": []map[string]any{
						{
							"name": "Test",
							"fields": map[string]any{
								"summary": map[string]any{
									"name":   "Summary",
									"schema": map[string]any{"type": "string"},
								},
								"customfield_10101": map[string]any{
									"name":   "Automation Status",
									"schema": map[string]any{"type": "option", "custom": "com.x:select"},
								},
								"customfield_10103": map[string]any{
									"name":   "Estimated Duration",
									"schema": map[string]any{"type": "number"},
								},
								"customfield_10200": map[string]any{
									"name":   "No Type Field",
									"schema": map[string]any{"custom": "com.x:weird"},
								},
							},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	defs, err := newTestClient(srv).ListCustomFields(context.Background(), "QA")
	if err != nil {
		t.Fatalf("ListCustomFields: %v", err)
	}
	byID := map[string]CustomFieldDef{}
	for _, d := range defs {
		byID[d.ID] = d
	}
	if len(byID) != 3 {
		t.Fatalf("want 3 custom fields, got %d: %#v", len(byID), defs)
	}
	if d := byID["customfield_10101"]; d.Name != "Automation Status" || d.Type != "option" {
		t.Errorf("customfield_10101 = %#v", d)
	}
	if d := byID["customfield_10103"]; d.Type != "number" {
		t.Errorf("customfield_10103 type = %q, want number", d.Type)
	}
	if d := byID["customfield_10200"]; d.Type != "com.x:weird" {
		t.Errorf("customfield_10200 should fall back to custom key, got %q", d.Type)
	}
	if _, ok := byID["summary"]; ok {
		t.Errorf("system field summary should be excluded")
	}
}

// TestListCustomFieldsFallsBackToField asserts that when createmeta yields no
// Test fields the call falls back to /rest/api/2/field and emits the custom
// entries there.
func TestListCustomFieldsFallsBackToField(t *testing.T) {
	var fieldCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/issue/createmeta":
			// Locked-down instance: empty projects.
			_ = json.NewEncoder(w).Encode(map[string]any{"projects": []any{}})
		case "/rest/api/2/field":
			fieldCalled = true
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "summary", "name": "Summary", "custom": false},
				{"id": "customfield_10101", "name": "Automation Status", "custom": true,
					"schema": map[string]any{"type": "option"}},
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	defs, err := newTestClient(srv).ListCustomFields(context.Background(), "QA")
	if err != nil {
		t.Fatalf("ListCustomFields: %v", err)
	}
	if !fieldCalled {
		t.Errorf("expected /rest/api/2/field fallback to be called")
	}
	if len(defs) != 1 || defs[0].ID != "customfield_10101" || defs[0].Type != "option" {
		t.Errorf("fallback defs = %#v", defs)
	}
}

// TestGetTestCustomFieldsReadsAndStringifies drives the live read path: it
// resolves the definitions, requests just those field ids on the issue, and
// stringifies present values (omitting null / absent).
func TestGetTestCustomFieldsReadsAndStringifies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/issue/createmeta":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"projects": []map[string]any{{
					"issuetypes": []map[string]any{{
						"name": "Test",
						"fields": map[string]any{
							"customfield_10101": map[string]any{"name": "Automation Status", "schema": map[string]any{"type": "option"}},
							"customfield_10102": map[string]any{"name": "Component", "schema": map[string]any{"type": "string"}},
							"customfield_10103": map[string]any{"name": "Duration", "schema": map[string]any{"type": "number"}},
							"customfield_10104": map[string]any{"name": "Empty", "schema": map[string]any{"type": "string"}},
						},
					}},
				}},
			})
		case "/rest/api/2/issue/QA-7":
			f := r.URL.Query().Get("fields")
			if !strings.Contains(f, "customfield_10101") {
				t.Errorf("expected fields to include customfield_10101, got %q", f)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"fields": map[string]any{
					"customfield_10101": map[string]any{"value": "Automated"},
					"customfield_10102": "Backend",
					"customfield_10103": 30,
					"customfield_10104": nil,
				},
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	vals, err := newTestClient(srv).GetTestCustomFields(context.Background(), "QA-7")
	if err != nil {
		t.Fatalf("GetTestCustomFields: %v", err)
	}
	want := map[string]string{
		"customfield_10101": "Automated",
		"customfield_10102": "Backend",
		"customfield_10103": "30",
	}
	if !reflect.DeepEqual(vals, want) {
		t.Errorf("GetTestCustomFields = %#v, want %#v", vals, want)
	}
}

// TestCustomFieldValueTypedWrite asserts the write helper resolves a field's
// schema type from /rest/api/2/field (cached) and shapes the value, and that an
// unknown id degrades to the raw string.
func TestCustomFieldValueTypedWrite(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/2/field" {
			t.Errorf("unexpected path %s", r.URL.Path)
			return
		}
		calls++
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "customfield_10101", "schema": map[string]any{"type": "option"}},
			{"id": "customfield_10105", "schema": map[string]any{"type": "array"}},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv)
	id, shaped, err := c.CustomFieldValue(context.Background(), "customfield_10101", "Automated")
	if err != nil {
		t.Fatalf("CustomFieldValue: %v", err)
	}
	if id != "customfield_10101" || !reflect.DeepEqual(shaped, map[string]string{"value": "Automated"}) {
		t.Errorf("option write = id=%q shaped=%#v", id, shaped)
	}

	// Multi-select shaping, served from the cached /field fetch (no second call).
	_, multi, err := c.CustomFieldValue(context.Background(), "customfield_10105", "A, B")
	if err != nil {
		t.Fatalf("CustomFieldValue array: %v", err)
	}
	if !reflect.DeepEqual(multi, []map[string]string{{"value": "A"}, {"value": "B"}}) {
		t.Errorf("array write shaped = %#v", multi)
	}

	// Unknown id degrades to the raw string.
	_, raw, err := c.CustomFieldValue(context.Background(), "customfield_99999", "plain")
	if err != nil {
		t.Fatalf("CustomFieldValue unknown: %v", err)
	}
	if raw != "plain" {
		t.Errorf("unknown id should yield raw string, got %#v", raw)
	}
	if calls != 1 {
		t.Errorf("expected exactly 1 /field fetch (cached), got %d", calls)
	}
}
