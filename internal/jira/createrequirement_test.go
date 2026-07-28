package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCreateRequirementMergesExtraFields verifies createmeta-driven extra
// fields are merged into the POST /issue payload, so an instance that marks a
// custom field required (e.g. "Req. type") accepts the create.
func TestCreateRequirementMergesExtraFields(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/rest/api/2/issue" {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"key":"PRD-42"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	extra := map[string]any{"customfield_14312": map[string]string{"id": "15201"}}
	key, err := newTestClient(srv).CreateRequirement(
		context.Background(), "PRD", "Story", "My req", "desc", "High", "", "", extra)
	if err != nil {
		t.Fatalf("CreateRequirement: %v", err)
	}
	if key != "PRD-42" {
		t.Fatalf("key = %q, want PRD-42", key)
	}
	fields, _ := body["fields"].(map[string]any)
	if fields == nil {
		t.Fatalf("no fields in POST body: %v", body)
	}
	cf, ok := fields["customfield_14312"].(map[string]any)
	if !ok || cf["id"] != "15201" {
		t.Errorf("customfield_14312 = %v, want {id:15201}", fields["customfield_14312"])
	}
	// Extra fields must not clobber the standard ones.
	if fields["summary"] != "My req" {
		t.Errorf("summary = %v, want My req", fields["summary"])
	}
}

// TestCreateRequirementExtraDoesNotOverrideStandard verifies an extra field
// colliding with a standard key does not overwrite the standard value.
func TestCreateRequirementExtraDoesNotOverrideStandard(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"key":"PRD-1"}`))
	}))
	defer srv.Close()

	extra := map[string]any{"summary": "HACKED"}
	if _, err := newTestClient(srv).CreateRequirement(
		context.Background(), "PRD", "Story", "Real summary", "", "", "", "", extra); err != nil {
		t.Fatalf("CreateRequirement: %v", err)
	}
	fields, _ := body["fields"].(map[string]any)
	if fields["summary"] != "Real summary" {
		t.Errorf("summary = %v, want the standard value to win", fields["summary"])
	}
}

// TestGetRequirementCreateFieldsLive parses createmeta, keeping only required
// custom fields and dropping the standard ones the form already collects.
func TestGetRequirementCreateFieldsLive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/2/issue/createmeta" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"projects":[{"issuetypes":[{"fields":{
			"summary":{"name":"Summary","required":true,"schema":{"type":"string"}},
			"priority":{"name":"Priority","required":true,"schema":{"type":"priority"}},
			"customfield_14312":{"name":"Req. type","required":true,"schema":{"type":"option"},
				"allowedValues":[{"id":"15201","value":"Functional"},{"id":"15202","value":"Security"}]},
			"customfield_99":{"name":"Optional CF","required":false,"schema":{"type":"string"}}
		}}]}]}`))
	}))
	defer srv.Close()

	got, err := newTestClient(srv).GetRequirementCreateFields(context.Background(), "PRD", "Story")
	if err != nil {
		t.Fatalf("GetRequirementCreateFields: %v", err)
	}
	// Only the required, non-standard custom field survives.
	if len(got) != 1 {
		t.Fatalf("got %d fields, want 1: %+v", len(got), got)
	}
	f := got[0]
	if f.ID != "customfield_14312" || f.Name != "Req. type" || f.Type != "option" {
		t.Errorf("field = %+v, want customfield_14312/Req. type/option", f)
	}
	if len(f.AllowedValues) != 2 || f.AllowedValues[0].Value != "Functional" {
		t.Errorf("allowedValues = %+v, want [Functional, Security]", f.AllowedValues)
	}
}

// TestGetRequirementCreateFieldsSurfacesRequiredLabels verifies the standard
// Labels field is NOT skipped for requirements (CreateRequirement never sets
// it), and surfaces as a "stringarray" kind so the form can collect it and send
// a plain string array rather than silently dropping it (which would 400).
func TestGetRequirementCreateFieldsSurfacesRequiredLabels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"projects":[{"issuetypes":[{"fields":{
			"labels":{"name":"Labels","required":true,"schema":{"type":"array","items":"string"}},
			"components":{"name":"Component/s","required":true,"schema":{"type":"array","items":"component"}}
		}}]}]}`))
	}))
	defer srv.Close()

	got, err := newTestClient(srv).GetRequirementCreateFields(context.Background(), "PRD", "Story")
	if err != nil {
		t.Fatalf("GetRequirementCreateFields: %v", err)
	}
	// components is collected by the form (skipped); labels surfaces.
	if len(got) != 1 || got[0].ID != "labels" {
		t.Fatalf("got %+v, want only the labels field", got)
	}
	if got[0].Type != "stringarray" {
		t.Errorf("labels type = %q, want stringarray", got[0].Type)
	}
}

// TestGetRequirementCreateFieldsDemo returns a representative required field
// offline so the create flow can be exercised in demo mode.
func TestGetRequirementCreateFieldsDemo(t *testing.T) {
	demo := &Client{baseURL: "demo", token: "t", http: http.DefaultClient}
	got, err := demo.GetRequirementCreateFields(context.Background(), "DEMO", "Story")
	if err != nil {
		t.Fatalf("GetRequirementCreateFields (demo): %v", err)
	}
	if len(got) == 0 || got[0].ID != "customfield_14312" {
		t.Fatalf("demo fields = %+v, want a customfield_14312 entry", got)
	}
}
