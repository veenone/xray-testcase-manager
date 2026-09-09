package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fieldList is the /rest/api/2/field answer a real Xray Server/DC instance
// gives, trimmed to what these tests need. The Conditions field id and name
// were read off RND_P_4NSSPRT_05-61 on a live instance (RND_P_4TFINT_05-358).
func fieldList() []map[string]any {
	return []map[string]any{
		{"id": "summary", "name": "Summary", "custom": false},
		{"id": "customfield_13988", "name": "Pre-Condition Type", "custom": true,
			"schema": map[string]any{"type": "option"}},
		{"id": "customfield_13989", "name": "Conditions", "custom": true,
			"schema": map[string]any{"type": "string"}},
	}
}

// The condition text lives in an instance-specific custom field, so the search
// has to resolve that field by name and ask for it alongside summary and
// description. Before this, the field was never requested and every synced
// precondition arrived with an empty Condition.
func TestListPreconditionsReadsTheConditionField(t *testing.T) {
	var searchFields string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/issuetype":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "42", "name": "Pre-Condition"}})
		case "/rest/api/2/field":
			_ = json.NewEncoder(w).Encode(fieldList())
		case "/rest/api/2/search":
			searchFields = r.URL.Query().Get("fields")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total": 1,
				"issues": []map[string]any{{
					"key": "PC-1",
					"fields": map[string]any{
						"summary":           "SNMP client tools reachable",
						"description":       "d",
						"customfield_13989": "snmpget -v3 ... $BOARD sysName.0",
					},
				}},
			})
		case "/rest/raven/1.0/api/precondition/PC-1/test":
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	pres, _, err := newTestClient(srv).ListPreconditions(context.Background(), "PROJ", nil)
	if err != nil {
		t.Fatalf("ListPreconditions: %v", err)
	}
	if !strings.Contains(searchFields, "customfield_13989") {
		t.Errorf("search must request the condition field, asked for %q", searchFields)
	}
	if len(pres) != 1 {
		t.Fatalf("preconditions = %+v, want one", pres)
	}
	if pres[0].Condition != "snmpget -v3 ... $BOARD sysName.0" {
		t.Errorf("Condition = %q, want the custom field's text", pres[0].Condition)
	}
}

// An instance that has no such field must still sync. The search falls back to
// summary and description alone and Condition stays empty, rather than the
// whole precondition sync failing.
func TestListPreconditionsSyncsWhenTheInstanceHasNoConditionField(t *testing.T) {
	var searchFields string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/issuetype":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "42", "name": "Pre-Condition"}})
		case "/rest/api/2/field":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "summary", "name": "Summary", "custom": false},
			})
		case "/rest/api/2/search":
			searchFields = r.URL.Query().Get("fields")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total": 1,
				"issues": []map[string]any{{
					"key":    "PC-1",
					"fields": map[string]any{"summary": "s", "description": "d"},
				}},
			})
		case "/rest/raven/1.0/api/precondition/PC-1/test":
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	pres, _, err := newTestClient(srv).ListPreconditions(context.Background(), "PROJ", nil)
	if err != nil {
		t.Fatalf("ListPreconditions: %v", err)
	}
	if strings.Contains(searchFields, "customfield_") {
		t.Errorf("no field to request, but asked for %q", searchFields)
	}
	if len(pres) != 1 || pres[0].Condition != "" {
		t.Errorf("preconditions = %+v, want one with an empty Condition", pres)
	}
}

// The name varies by Xray version: the live instance calls it "Conditions",
// older ones "Condition". Both must resolve.
func TestConditionFieldIDAcceptsEitherName(t *testing.T) {
	for _, name := range []string{"Conditions", "Condition"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/rest/api/2/field" {
				t.Errorf("unexpected request: %s", r.URL.Path)
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "customfield_99", "name": name, "custom": true},
			})
		}))
		id, err := newTestClient(srv).conditionFieldID(context.Background())
		srv.Close()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if id != "customfield_99" {
			t.Errorf("%s resolved to %q, want customfield_99", name, id)
		}
	}
}

// ConditionFieldValue is what the commit path uses to turn a stored condition
// edit into the field id and value Jira wants.
func TestConditionFieldValue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(fieldList())
	}))
	defer srv.Close()

	id, value, ok, err := newTestClient(srv).ConditionFieldValue(context.Background(), "a condition")
	if err != nil {
		t.Fatalf("ConditionFieldValue: %v", err)
	}
	if !ok || id != "customfield_13989" || value != "a condition" {
		t.Errorf("got (%q, %v, %v), want (customfield_13989, \"a condition\", true)", id, value, ok)
	}
}

// An instance without the field reports ok=false and no error, so a commit
// carrying a condition edit skips that one field rather than failing the whole
// commit, the same way exec_type does.
func TestConditionFieldValueDegradesWhenTheFieldIsAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "summary", "name": "Summary", "custom": false},
		})
	}))
	defer srv.Close()

	_, _, ok, err := newTestClient(srv).ConditionFieldValue(context.Background(), "a condition")
	if err != nil {
		t.Fatalf("ConditionFieldValue: %v", err)
	}
	if ok {
		t.Error("ok should be false when the instance has no condition field")
	}
}

// An instance can carry two custom fields with the same display name. On the
// live one (RND_P_4NSSPRT_05) "Conditions" is both a generic select at
// customfield_10051 and Xray's own precondition editor at customfield_13989,
// and /rest/api/2/field lists the select first. Matching on the name alone
// took the select, which is empty on every Precondition, so the whole view
// read "No condition defined". The plugin key is the field's real identity, so
// resolve on that and let the name be the fallback.
func TestConditionFieldIDPrefersTheXrayEditorOverASameNamedField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/2/field" {
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "summary", "name": "Summary", "custom": false},
			// The decoy comes first, exactly as the live instance orders them.
			{"id": "customfield_10051", "name": "Conditions", "custom": true,
				"schema": map[string]any{
					"type":   "option",
					"custom": "com.atlassian.jira.plugin.system.customfieldtypes:select",
				}},
			{"id": "customfield_13989", "name": "Conditions", "custom": true,
				"schema": map[string]any{
					"type":   "string",
					"custom": "com.xpandit.plugins.xray:precondition-editor-custom-field",
				}},
		})
	}))
	defer srv.Close()

	id, err := newTestClient(srv).conditionFieldID(context.Background())
	if err != nil {
		t.Fatalf("conditionFieldID: %v", err)
	}
	if id != "customfield_13989" {
		t.Fatalf("condition field id = %q, want customfield_13989", id)
	}
}

// The plugin key is preferred, not required: an instance whose field carries a
// different plugin key (or none in the response) still resolves by name, which
// is what every instance did before this.
func TestConditionFieldIDStillFallsBackToTheName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "customfield_20001", "name": "Condition", "custom": true},
		})
	}))
	defer srv.Close()

	id, err := newTestClient(srv).conditionFieldID(context.Background())
	if err != nil {
		t.Fatalf("conditionFieldID: %v", err)
	}
	if id != "customfield_20001" {
		t.Fatalf("condition field id = %q, want customfield_20001", id)
	}
}
