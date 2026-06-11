package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// Step is one ordered step in an Xray Test (FR-2.5). Xray stores step
// content under "raw" / "rendered" subkeys so unicode and wiki markup
// round-trip; we keep just "raw" here — it's what the editor reads and
// writes, and rendering is the UI's job.
type Step struct {
	ID       string `json:"id"`
	Index    int    `json:"index"`
	Action   string `json:"action"`
	Data     string `json:"data"`
	Expected string `json:"expected"`
}

// UpdateTestStep applies field changes to a single Test Step (FR-2.5). The
// fields map is keyed by the local domain names ("action", "data",
// "expected"); only the keys present are sent — Xray leaves any field
// absent from the body untouched. Demo URLs short-circuit to a no-op so
// step edits in demo just clear local pending rows.
//
// Maps to PUT /rest/raven/2.0/api/test/{key}/steps/{stepId}. Xray's body
// uses "step" (= our "action"), "data", and "result" (= our "expected").
func (c *Client) UpdateTestStep(ctx context.Context, key, stepID string, fields map[string]string) error {
	if isDemoURL(c.baseURL) {
		return nil
	}
	stepFields := map[string]string{}
	if v, ok := fields["action"]; ok {
		stepFields[stepFieldAction] = v
	}
	if v, ok := fields["data"]; ok {
		stepFields[stepFieldData] = v
	}
	if v, ok := fields["expected"]; ok {
		stepFields[stepFieldResult] = v
	}
	if len(stepFields) == 0 {
		return nil
	}
	return c.put(ctx, fmt.Sprintf("/rest/raven/2.0/api/test/%s/steps/%s", key, stepID),
		map[string]any{"fields": stepFields})
}

// Xray Test Step field display names (the defaults). The create/update bodies
// and the GET response key step content by these names under "fields".
const (
	stepFieldAction = "Action"
	stepFieldData   = "Data"
	stepFieldResult = "Expected Result"
)

// CreateTestStep appends a new Test Step (FR-2.5) and returns the new step's
// Xray id when the create response includes one — the commit path uses it to
// swap the local "new-N" placeholder for the real id. Demo URLs short-circuit
// to a no-op, returning an empty id (the demo backend has no persistence, so
// the placeholder is reconciled by the next steps refresh).
//
// Maps to POST /rest/raven/2.0/api/test/{key}/steps, reusing the same
// step/data/result raw-wrapped body shape as UpdateTestStep.
func (c *Client) CreateTestStep(ctx context.Context, key, action, data, expected string) (string, error) {
	if isDemoURL(c.baseURL) {
		return "", nil
	}
	// Xray rejects an all-empty step with a 400 ("Step fields must be provided
	// to create a new test step"). Catch it here with a plain-language message
	// instead of letting that opaque error surface — most often the user added a
	// blank step to a Test whose existing steps hadn't loaded.
	if strings.TrimSpace(action) == "" && strings.TrimSpace(data) == "" && strings.TrimSpace(expected) == "" {
		return "", fmt.Errorf(
			"this test has an empty step that can't be saved. Open the test, fill in the " +
				"step's Action, Data, or Expected Result — or delete the blank step — then commit again. " +
				"(If the test already has steps in Jira, refresh its Steps panel first so a blank row isn't added.)")
	}
	// Xray v2.0 expects step content under "fields", keyed by the field display
	// names; sending the old step/data/result shape is what produced the
	// misleading "Step fields must be provided" 400.
	stepFields := map[string]string{}
	if strings.TrimSpace(action) != "" {
		stepFields[stepFieldAction] = action
	}
	if strings.TrimSpace(data) != "" {
		stepFields[stepFieldData] = data
	}
	if strings.TrimSpace(expected) != "" {
		stepFields[stepFieldResult] = expected
	}
	body := map[string]any{"fields": stepFields}
	var resp map[string]any
	if err := c.writeJSONReturning(
		ctx, http.MethodPost,
		fmt.Sprintf("/rest/raven/2.0/api/test/%s/steps", key), body, &resp,
	); err != nil {
		return "", err
	}
	if id, ok := resp["id"]; ok && id != nil {
		return fmt.Sprint(id), nil
	}
	return "", nil
}

// DeleteTestStep removes one Test Step (FR-2.5). Demo URLs short-circuit
// to a no-op.
//
// Maps to DELETE /rest/raven/2.0/api/test/{key}/steps/{stepId}.
func (c *Client) DeleteTestStep(ctx context.Context, key, stepID string) error {
	if isDemoURL(c.baseURL) {
		return nil
	}
	return c.delete(ctx, fmt.Sprintf("/rest/raven/2.0/api/test/%s/steps/%s", key, stepID))
}

// MoveTestStep repositions a step to a 1-based index (FR-2.5). The step's
// current content (action/data/expected) is sent alongside the index: Xray's
// v2.0 step PUT rejects a body with no "fields" — it reads a fieldless step as a
// request to create a new (empty) one and 400s ("Step fields must be provided to
// create a new test step"). Resending the existing content is idempotent (any
// field edits in the same commit are pushed before the reorder). Demo URLs
// short-circuit to a no-op.
//
// Maps to PUT /rest/raven/2.0/api/test/{key}/steps/{stepId}. NOTE(xtm): the
// commit path PUTs steps in target order so the final sequence is deterministic
// regardless of Xray's index-reflow semantics.
func (c *Client) MoveTestStep(ctx context.Context, key, stepID string, index int, action, data, expected string) error {
	if isDemoURL(c.baseURL) {
		return nil
	}
	stepFields := map[string]string{}
	if strings.TrimSpace(action) != "" {
		stepFields[stepFieldAction] = action
	}
	if strings.TrimSpace(data) != "" {
		stepFields[stepFieldData] = data
	}
	if strings.TrimSpace(expected) != "" {
		stepFields[stepFieldResult] = expected
	}
	body := map[string]any{"index": index}
	if len(stepFields) > 0 {
		body["fields"] = stepFields
	}
	return c.put(
		ctx,
		fmt.Sprintf("/rest/raven/2.0/api/test/%s/steps/%s", key, stepID),
		body,
	)
}

// GetTestSteps returns the ordered list of Steps for a Test (FR-2.5). Demo
// URLs fall through to a deterministic generator so the steps panel renders
// without a real Xray.
//
// Maps to GET /rest/raven/2.0/api/test/{key}/steps.
func (c *Client) GetTestSteps(ctx context.Context, key string) ([]Step, error) {
	if isDemoURL(c.baseURL) {
		return demoStepsForKey(key), nil
	}
	body, err := c.getBytes(ctx, fmt.Sprintf("/rest/raven/2.0/api/test/%s/steps", key))
	if err != nil {
		return nil, err
	}
	steps, err := parseStepsResponse(body)
	if err != nil {
		log.Printf("xtm: GetTestSteps %s decode error: %v; raw=%s", key, err, snippet(body, 2000))
		return nil, err
	}
	// Diagnostics for the "steps don't load / fields are blank" reports: always
	// log the count, and dump the raw response when we extracted no steps — or
	// steps with no content — so the exact shape is visible in the app log
	// without a debugger against the live instance.
	log.Printf("xtm: GetTestSteps %s parsed %d step(s)", key, len(steps))
	if len(bytes.TrimSpace(body)) > 0 && (len(steps) == 0 || allStepsBlank(steps)) {
		log.Printf("xtm: GetTestSteps %s empty/blank; raw=%s", key, snippet(body, 2000))
	}
	return steps, nil
}

// allStepsBlank reports whether every step has empty action, data and expected
// — the signature of a content-shape mismatch (rows present, fields unmapped).
func allStepsBlank(steps []Step) bool {
	for _, s := range steps {
		if s.Action != "" || s.Data != "" || s.Expected != "" {
			return false
		}
	}
	return len(steps) > 0
}

// parseOneStep decodes a single step object into a Step without ever failing on
// a field's type — every value is read leniently from a generic map, so a string
// "index", a numeric id, a nested {value:{raw}} field, or an Option field's array
// can't drop the step. Content comes from the "fields" container first (the v2.0
// shape), then top-level step/action/data/result for older shapes.
func parseOneStep(raw json.RawMessage) Step {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return Step{}
	}
	s := Step{
		ID:    jsonToString(m["id"]),
		Index: jsonToInt(m["index"]),
	}
	s.Action, s.Data, s.Expected = extractStepFields(m["fields"])
	if s.Action == "" {
		s.Action = firstText(m["step"], m["action"])
	}
	if s.Data == "" {
		s.Data = wikiText(m["data"])
	}
	if s.Expected == "" {
		s.Expected = firstText(m["result"], m["expected"])
	}
	return s
}

// firstText returns the first value that resolves to non-empty text.
func firstText(vals ...json.RawMessage) string {
	for _, v := range vals {
		if s := wikiText(v); s != "" {
			return s
		}
	}
	return ""
}

// jsonToString reads a JSON string or number as a Go string (Xray returns step
// ids as either). Empty/absent yields "".
func jsonToString(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		return s
	}
	return string(raw) // a bare number / token — keep its text
}

// jsonToInt reads a JSON number or numeric string as an int, defaulting to 0.
func jsonToInt(raw json.RawMessage) int {
	s := jsonToString(raw)
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// extractStepFields pulls action / data / expected out of the "fields" container,
// tolerating both shapes Xray emits: a {name: value} object or a [{name,value}]
// array. Each field's value is resolved by wikiText, and its role by name.
func extractStepFields(raw json.RawMessage) (action, data, expected string) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", "", ""
	}
	assign := func(role, val string) {
		if val == "" {
			return
		}
		switch role {
		case "action":
			if action == "" {
				action = val
			}
		case "data":
			if data == "" {
				data = val
			}
		case "expected":
			if expected == "" {
				expected = val
			}
		}
	}
	switch raw[0] {
	case '{':
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) == nil {
			for name, val := range m {
				assign(classifyStepField(name), wikiText(val))
			}
		}
	case '[':
		var arr []struct {
			Name  string          `json:"name"`
			Value json.RawMessage `json:"value"`
		}
		if json.Unmarshal(raw, &arr) == nil {
			for _, f := range arr {
				assign(classifyStepField(f.Name), wikiText(f.Value))
			}
		}
	}
	return action, data, expected
}

// wikiText extracts the text content of a step field value, handling a bare
// string, a {"raw":…}/{"rendered":…} Wiki object, or a {"value":…} wrapper
// (recursively). Arrays / scalars (Option / Data fields) yield "".
func wikiText(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	switch raw[0] {
	case '"':
		var s string
		_ = json.Unmarshal(raw, &s)
		return s
	case '{':
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil {
			return ""
		}
		for _, k := range []string{"raw", "value", "rendered"} {
			if v, ok := m[k]; ok {
				if s := wikiText(v); s != "" {
					return s
				}
			}
		}
	}
	return ""
}

// classifyStepField maps a step field's display name to its role. Xray's
// defaults are "Action", "Data", "Expected Result"; matching by keyword keeps
// renamed/re-cased fields working.
func classifyStepField(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	switch {
	case n == "step" || strings.Contains(n, "action"):
		return "action"
	case strings.Contains(n, "expected") || strings.Contains(n, "result"):
		return "expected"
	case strings.Contains(n, "data"):
		return "data"
	}
	return ""
}

// parseStepsResponse decodes the Xray "get test steps" body into Steps. Xray
// Server/DC normally returns a bare JSON array, but real instances also return
// a top-level object in two situations we must tolerate rather than crash on:
//
//   - a wrapper object {"steps": [...]} on some versions, and
//   - a Jira/Xray error object {"errorMessages": [...], "errors": {...}}
//     returned with a 200 when a step's content trips server-side rendering
//     (which is what surfaces for steps containing certain special characters).
//
// The previous bare `[]struct{…}` decode turned the second case into the opaque
// "json: cannot unmarshal object into Go value of type []struct{…}" error.
func parseStepsResponse(body []byte) ([]Step, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return []Step{}, nil
	}

	var rawSteps []json.RawMessage
	switch trimmed[0] {
	case '[':
		if err := json.Unmarshal(trimmed, &rawSteps); err != nil {
			return nil, fmt.Errorf("decode steps: %w", err)
		}
	case '{':
		// The {"steps": [...]} wrapper Xray v2.0 uses.
		var wrapper struct {
			Steps []json.RawMessage `json:"steps"`
		}
		if err := json.Unmarshal(trimmed, &wrapper); err == nil && wrapper.Steps != nil {
			rawSteps = wrapper.Steps
			break
		}
		// Otherwise surface a Jira/Xray error object meaningfully instead of a
		// cryptic decode error.
		if msg := jiraErrorMessage(trimmed); msg != "" {
			return nil, fmt.Errorf("xray could not return this test's steps: %s", msg)
		}
		return nil, fmt.Errorf("unexpected steps response: %s", snippet(trimmed, 256))
	default:
		return nil, fmt.Errorf("unexpected steps response: %s", snippet(trimmed, 256))
	}

	out := make([]Step, 0, len(rawSteps))
	for _, rs := range rawSteps {
		out = append(out, parseOneStep(rs))
	}
	return out, nil
}

// flexString unmarshals a JSON string or number into a Go string. Xray is
// inconsistent about whether a step id is a JSON string or a number, so
// accepting both keeps step loading from breaking on either shape.
type flexString string

func (s *flexString) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*s = ""
		return nil
	}
	if b[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		*s = flexString(str)
		return nil
	}
	*s = flexString(string(b)) // a bare number — keep its text form
	return nil
}

// jiraErrorMessage extracts a human-readable message from a Jira/Xray error
// object, joining errorMessages and the errors map (sorted for stable output).
// Returns "" if the body isn't a recognisable error object.
func jiraErrorMessage(body []byte) string {
	var e struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
		Error         string            `json:"error"`
		Message       string            `json:"message"`
	}
	if err := json.Unmarshal(body, &e); err != nil {
		return ""
	}
	parts := append([]string{}, e.ErrorMessages...)
	keys := make([]string, 0, len(e.Errors))
	for k := range e.Errors {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s: %s", k, e.Errors[k]))
	}
	if e.Error != "" {
		parts = append(parts, e.Error)
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	return strings.Join(parts, "; ")
}

// snippet returns a trimmed, length-capped view of a response body for use in
// diagnostic error messages.
func snippet(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) > n {
		s = s[:n] + "…"
	}
	return s
}
