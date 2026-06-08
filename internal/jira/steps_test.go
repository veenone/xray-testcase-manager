package jira

import (
	"strings"
	"testing"
)

// TestParseStepsResponse_Array covers the normal Xray Server/DC shape: a bare
// array whose step content fields are {"raw": …} objects.
func TestParseStepsResponse_Array(t *testing.T) {
	body := []byte(`[
		{"id": "101", "index": 1,
		 "step":   {"raw": "Open the page", "rendered": "<p>Open the page</p>"},
		 "data":   {"raw": "user=admin"},
		 "result": {"raw": "Page loads"}}
	]`)
	steps, err := parseStepsResponse(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("want 1 step, got %d", len(steps))
	}
	got := steps[0]
	if got.ID != "101" || got.Index != 1 || got.Action != "Open the page" ||
		got.Data != "user=admin" || got.Expected != "Page loads" {
		t.Fatalf("unexpected step: %+v", got)
	}
}

// TestParseStepsResponse_StringFieldsAndNumericID covers Xray variants that
// return step content as bare strings (including special characters) and a
// numeric id — both previously fragile.
func TestParseStepsResponse_StringFieldsAndNumericID(t *testing.T) {
	body := []byte(`[
		{"id": 42, "index": 1,
		 "step": "Enter <name> & \"quote\" — ünïcödé 😀",
		 "data": null,
		 "result": "OK"}
	]`)
	steps, err := parseStepsResponse(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("want 1 step, got %d", len(steps))
	}
	got := steps[0]
	if got.ID != "42" {
		t.Fatalf("want id 42, got %q", got.ID)
	}
	if got.Action != `Enter <name> & "quote" — ünïcödé 😀` {
		t.Fatalf("special-character action not preserved: %q", got.Action)
	}
	if got.Data != "" {
		t.Fatalf("want empty data for null, got %q", got.Data)
	}
}

// TestParseStepsResponse_Wrapper covers the {"steps": [...]} object wrapper.
func TestParseStepsResponse_Wrapper(t *testing.T) {
	body := []byte(`{"steps": [
		{"id": "1", "index": 1, "step": {"raw": "A"}, "data": {"raw": ""}, "result": {"raw": "B"}}
	]}`)
	steps, err := parseStepsResponse(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(steps) != 1 || steps[0].Action != "A" || steps[0].Expected != "B" {
		t.Fatalf("unexpected steps: %+v", steps)
	}
}

// TestParseStepsResponse_ErrorObject is the reported regression: an error
// object returned with a 200 must become a clear message, not the opaque
// "cannot unmarshal object into []struct" error.
func TestParseStepsResponse_ErrorObject(t *testing.T) {
	body := []byte(`{"errorMessages": ["Rendering failed for step content"], "errors": {"step": "invalid character"}}`)
	_, err := parseStepsResponse(body)
	if err == nil {
		t.Fatal("expected an error for an error-object response")
	}
	msg := err.Error()
	if want := "Rendering failed for step content"; !strings.Contains(msg, want) {
		t.Fatalf("error message %q does not mention %q", msg, want)
	}
	if want := "step: invalid character"; !strings.Contains(msg, want) {
		t.Fatalf("error message %q does not include the errors map entry %q", msg, want)
	}
}

// TestParseStepsResponse_AltKeys covers Xray variants that name the content
// fields action/expected (instead of step/result) or nest them under "fields" —
// the "steps load but their fields are blank" regression.
func TestParseStepsResponse_AltKeys(t *testing.T) {
	body := []byte(`[
		{"id": 1, "index": 1, "action": "Click login", "data": "creds", "expected": "Logged in"},
		{"id": 2, "index": 2, "fields": {"step": "Open page", "result": "Page shown"}}
	]`)
	steps, err := parseStepsResponse(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("want 2 steps, got %d", len(steps))
	}
	if steps[0].Action != "Click login" || steps[0].Data != "creds" || steps[0].Expected != "Logged in" {
		t.Fatalf("alt top-level keys not mapped: %+v", steps[0])
	}
	if steps[1].Action != "Open page" || steps[1].Expected != "Page shown" {
		t.Fatalf("nested fields not mapped: %+v", steps[1])
	}
}

// TestParseStepsResponse_V2Fields covers the canonical Xray Server/DC v2.0
// shape: {"steps":[{id,index,fields:{Action,Data,Expected Result},attachmentIds}]}
// with Wiki ({raw,rendered}) values — plus an Option field whose array value
// must NOT fail the decode (the "zero steps loaded" regression).
func TestParseStepsResponse_V2Fields(t *testing.T) {
	body := []byte(`{
		"steps": [
			{
				"id": 7, "index": 1,
				"fields": {
					"Action": {"raw": "Open the login page", "rendered": "<p>Open the login page</p>"},
					"Data": {"raw": "user=admin", "rendered": "<p>user=admin</p>"},
					"Expected Result": {"raw": "Login form shown", "rendered": "<p>Login form shown</p>"},
					"Custom Select": [{"id": 1, "value": "High"}]
				},
				"attachmentIds": [101, 102]
			}
		]
	}`)
	steps, err := parseStepsResponse(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("want 1 step, got %d", len(steps))
	}
	s := steps[0]
	if s.ID != "7" || s.Index != 1 {
		t.Fatalf("unexpected id/index: %+v", s)
	}
	if s.Action != "Open the login page" {
		t.Fatalf("action not mapped from fields: %q", s.Action)
	}
	if s.Data != "user=admin" {
		t.Fatalf("data not mapped from fields: %q", s.Data)
	}
	if s.Expected != "Login form shown" {
		t.Fatalf("'Expected Result' not mapped from fields: %q", s.Expected)
	}
}

// TestParseStepsResponse_FieldsArray covers the "fields" container being an
// ARRAY of {name,value} objects with nested {"value":{"raw":…}} values — a
// shape that previously failed to decode (no steps at all).
func TestParseStepsResponse_FieldsArray(t *testing.T) {
	body := []byte(`{
		"steps": [
			{
				"id": 3, "index": 1,
				"fields": [
					{"name": "Action", "value": {"raw": "Do the thing"}},
					{"name": "Expected Result", "value": {"value": {"raw": "It happened"}}},
					{"name": "Priority", "value": [{"id": 2, "value": "High"}]}
				]
			}
		]
	}`)
	steps, err := parseStepsResponse(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("want 1 step, got %d", len(steps))
	}
	if steps[0].Action != "Do the thing" {
		t.Fatalf("action from array fields: %q", steps[0].Action)
	}
	if steps[0].Expected != "It happened" {
		t.Fatalf("nested value.raw not resolved: %q", steps[0].Expected)
	}
}

// TestAllStepsBlank flags a list whose rows all have empty content (the shape
// mismatch the diagnostics log key off).
func TestAllStepsBlank(t *testing.T) {
	if !allStepsBlank([]Step{{ID: "1"}, {ID: "2"}}) {
		t.Fatal("expected all-blank to be true")
	}
	if allStepsBlank([]Step{{Action: "x"}}) {
		t.Fatal("expected all-blank to be false when content present")
	}
	if allStepsBlank(nil) {
		t.Fatal("expected all-blank false for empty list")
	}
}

// TestParseStepsResponse_Empty treats an empty body as an empty step list.
func TestParseStepsResponse_Empty(t *testing.T) {
	steps, err := parseStepsResponse([]byte("   "))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(steps) != 0 {
		t.Fatalf("want 0 steps, got %d", len(steps))
	}
}
