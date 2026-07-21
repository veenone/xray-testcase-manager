package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// resolveCustomFieldID resolves a Jira custom field id (e.g. "customfield_10100")
// from its display name via GET /rest/api/2/field, matching the first field whose
// name equals fieldName (case-insensitive, trimmed) and which is a custom field.
// The result is cached per fieldName on the Client so repeated lookups during one
// sync or commit do not re-fetch.
//
// It returns ("", nil) when no field matches, so callers can degrade gracefully
// (skip the field) rather than failing the whole sync or commit. Demo mode never
// reaches the network here: it returns ("", nil) immediately, which is harmless
// because the demo read/write paths do not call this resolver.
func (c *Client) resolveCustomFieldID(ctx context.Context, fieldName string) (string, error) {
	want := strings.ToLower(strings.TrimSpace(fieldName))
	if want == "" {
		return "", nil
	}
	if isDemoURL(c.baseURL) {
		return "", nil
	}

	c.customFieldMu.Lock()
	if c.customFieldIDs != nil {
		if id, ok := c.customFieldIDs[want]; ok {
			c.customFieldMu.Unlock()
			return id, nil
		}
	}
	c.customFieldMu.Unlock()

	var fields []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Custom bool   `json:"custom"`
	}
	if err := c.get(ctx, "/rest/api/2/field", &fields); err != nil {
		return "", err
	}

	id := ""
	for _, f := range fields {
		if f.Custom && strings.EqualFold(strings.TrimSpace(f.Name), strings.TrimSpace(fieldName)) {
			id = f.ID
			break
		}
	}

	c.customFieldMu.Lock()
	if c.customFieldIDs == nil {
		c.customFieldIDs = make(map[string]string)
	}
	c.customFieldIDs[want] = id
	c.customFieldMu.Unlock()
	return id, nil
}

// testTypeFieldID resolves and caches the custom field id of the Xray "Test Type"
// field (the app's exec_type) for this instance, returning "" (no error) when the
// instance has no such field so the caller can proceed without it.
func (c *Client) testTypeFieldID(ctx context.Context) (string, error) {
	return c.resolveCustomFieldID(ctx, "Test Type")
}

// cucumberScenarioFieldID resolves the custom field id for the Xray "Cucumber
// Scenario" field (the Gherkin text on Cucumber tests), returning "" (no error)
// when the instance does not have the field.
func (c *Client) cucumberScenarioFieldID(ctx context.Context) (string, error) {
	return c.resolveCustomFieldID(ctx, "Cucumber Scenario")
}

// cucumberTypeFieldID resolves the custom field id for the Xray "Cucumber Test
// Type" (a.k.a. "Scenario Type") field, trying the canonical name first and
// falling back to the version alias, returning "" (no error) when neither is
// found on the instance.
func (c *Client) cucumberTypeFieldID(ctx context.Context) (string, error) {
	if id, _ := c.resolveCustomFieldID(ctx, "Cucumber Test Type"); id != "" {
		return id, nil
	}
	return c.resolveCustomFieldID(ctx, "Scenario Type") // version alias
}

// genericDefinitionFieldID resolves the custom field id for the Xray "Generic
// Test Definition" field (the plain-text definition on Generic tests), returning
// "" (no error) when the instance does not have the field.
func (c *Client) genericDefinitionFieldID(ctx context.Context) (string, error) {
	return c.resolveCustomFieldID(ctx, "Generic Test Definition")
}

// testEnvironmentsFieldID resolves and caches the custom field id of the Xray
// "Test Environments" field (a multi-select on Test Executions) for this
// instance, returning "" (no error) when the instance has no such field so the
// read path can proceed without it.
func (c *Client) testEnvironmentsFieldID(ctx context.Context) (string, error) {
	return c.resolveCustomFieldID(ctx, "Test Environments")
}

// testPlanFieldID resolves and caches the custom field id of the Xray "Test
// Plan" field on a Test Execution issue (the plan(s) the execution belongs to)
// for this instance, returning "" (no error) when the instance has no such
// field so the read path can proceed without it.
func (c *Client) testPlanFieldID(ctx context.Context) (string, error) {
	return c.resolveCustomFieldID(ctx, "Test Plan")
}

// CustomFieldDef describes a Jira custom field configured for the Test issue
// type (FR-2.6): its Jira id (e.g. "customfield_10001"), display name and a
// coarse type hint.
type CustomFieldDef struct {
	ID   string
	Name string
	Type string
}

// createmetaResponse is the shape of GET /rest/api/2/issue/createmeta with the
// projects.issuetypes.fields expansion: each project carries its issue types,
// and each issue type carries a map of field id to field metadata (name plus a
// schema with the field's type / custom marker). We read the custom field
// entries (ids starting with "customfield_") off the Test issue type.
type createmetaResponse struct {
	Projects []struct {
		IssueTypes []struct {
			Name   string                        `json:"name"`
			Fields map[string]createmetaFieldDef `json:"fields"`
		} `json:"issuetypes"`
	} `json:"projects"`
}

// createmetaFieldDef is one field's metadata inside createmeta: its display name
// and a schema carrying the coarse type plus the custom-renderer key.
type createmetaFieldDef struct {
	Name   string `json:"name"`
	Schema struct {
		Type   string `json:"type"`
		Custom string `json:"custom"`
	} `json:"schema"`
}

// ListCustomFields returns the custom fields configured for the project's Test
// issue type (FR-2.6). Demo URLs short-circuit to a fixed set.
//
// Live path: it prefers GET /rest/api/2/issue/createmeta with the
// projects.issuetypes.fields expansion scoped to the Test issue type, emitting
// one CustomFieldDef per "customfield_*" field (Type taken from the field's
// schema type, falling back to the custom-renderer key). When createmeta yields
// no Test fields (older or locked-down instances that restrict createmeta), it
// falls back to GET /rest/api/2/field and emits every entry marked custom.
//
// NOTE(xtm): the createmeta projects.issuetypes.fields shape and the schema
// type / custom keys, plus the /field fallback, follow Jira DC conventions and
// should be verified against a live Xray Server/DC 8.4.0 instance: some
// instances disable the createmeta fields expansion, in which case the fallback
// path is exercised.
func (c *Client) ListCustomFields(ctx context.Context, projectKey string) ([]CustomFieldDef, error) {
	if isDemoURL(c.baseURL) {
		return demoCustomFieldDefs(), nil
	}

	path := "/rest/api/2/issue/createmeta?projectKeys=" + projectKey +
		"&issuetypeNames=Test&expand=projects.issuetypes.fields"
	var meta createmetaResponse
	if err := c.get(ctx, path, &meta); err != nil {
		return nil, err
	}
	defs := defsFromCreatemeta(meta)
	if len(defs) > 0 {
		return defs, nil
	}
	// Fallback: createmeta returned no Test fields, so list every custom field
	// on the instance.
	return c.listAllCustomFields(ctx)
}

// defsFromCreatemeta extracts the custom field definitions for the Test issue
// type out of a createmeta response. Pure (no network) so it is unit tested
// directly. Each field id starting with "customfield_" becomes a CustomFieldDef
// whose Type is the schema type when present, else the custom-renderer key.
func defsFromCreatemeta(meta createmetaResponse) []CustomFieldDef {
	out := []CustomFieldDef{}
	for _, p := range meta.Projects {
		for _, it := range p.IssueTypes {
			for id, fd := range it.Fields {
				if !strings.HasPrefix(id, "customfield_") {
					continue
				}
				typ := fd.Schema.Type
				if typ == "" {
					typ = fd.Schema.Custom
				}
				out = append(out, CustomFieldDef{ID: id, Name: fd.Name, Type: typ})
			}
		}
	}
	return out
}

// listAllCustomFields fetches every custom field on the instance via
// GET /rest/api/2/field, used as the createmeta fallback.
func (c *Client) listAllCustomFields(ctx context.Context) ([]CustomFieldDef, error) {
	var fields []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Custom bool   `json:"custom"`
		Schema struct {
			Type string `json:"type"`
		} `json:"schema"`
	}
	if err := c.get(ctx, "/rest/api/2/field", &fields); err != nil {
		return nil, err
	}
	out := []CustomFieldDef{}
	for _, f := range fields {
		if f.Custom {
			out = append(out, CustomFieldDef{ID: f.ID, Name: f.Name, Type: f.Schema.Type})
		}
	}
	return out, nil
}

// GetTestCustomFields returns a Test's custom field values, keyed by field id
// (FR-2.6). Demo URLs generate deterministic values.
//
// Live path: it resolves the project's Test custom field ids (via
// ListCustomFields, parsing the project from the test key), requests just those
// fields on GET /rest/api/2/issue/{testKey}, and stringifies each present value
// via stringifyFieldValue. Absent / null values are omitted from the map. It is
// best-effort: if no custom field ids resolve it returns an empty map without
// fetching the issue.
//
// NOTE(xtm): the per-value JSON shapes (option object, user object, arrays of
// either, bare scalar) handled by stringifyFieldValue follow Jira DC
// conventions and should be verified against a live Xray Server/DC 8.4.0
// instance.
func (c *Client) GetTestCustomFields(ctx context.Context, testKey string) (map[string]string, error) {
	if isDemoURL(c.baseURL) {
		return demoTestCustomFields(testKey), nil
	}

	defs, err := c.ListCustomFields(ctx, customFieldProjectKey(testKey))
	if err != nil {
		return nil, err
	}
	if len(defs) == 0 {
		return map[string]string{}, nil
	}
	ids := make([]string, 0, len(defs))
	for _, d := range defs {
		ids = append(ids, d.ID)
	}

	var resp struct {
		Fields map[string]json.RawMessage `json:"fields"`
	}
	path := "/rest/api/2/issue/" + testKey + "?fields=" + strings.Join(ids, ",")
	if err := c.get(ctx, path, &resp); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(ids))
	for _, id := range ids {
		if v := stringifyFieldValue(resp.Fields[id]); v != "" {
			out[id] = v
		}
	}
	return out, nil
}

// customFieldProjectKey parses the project key prefix off a Jira issue key
// (e.g. "QA-1234" -> "QA"), used to scope the createmeta lookup.
func customFieldProjectKey(testKey string) string {
	if i := strings.LastIndex(testKey, "-"); i > 0 {
		return testKey[:i]
	}
	return testKey
}

// stringifyFieldValue renders a raw Jira custom field value into the single
// string the local cache and detail panel use, handling the shapes a Jira DC
// custom field can take:
//
//   - option / select object {"value": ...} (or {"name": ...}) -> that string
//   - user object {"displayName": ...} (or {"name": ...})       -> that string
//   - array of any of the above (multi-select, multi-user)      -> joined ", "
//   - bare string                                               -> itself
//   - number                                                    -> formatted
//   - null / absent / unrecognised                              -> "" (omitted)
//
// Pure (no network) so it is unit tested directly.
func stringifyFieldValue(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if len(trimmed) == 0 || trimmed == "null" {
		return ""
	}
	switch trimmed[0] {
	case '[':
		// Array: reuse parseOptionValues for option/string element arrays, then
		// fall back to stringifying each raw element (covers user objects).
		if vals := parseOptionValues(raw); len(vals) > 0 {
			return strings.Join(vals, ", ")
		}
		var elems []json.RawMessage
		if err := json.Unmarshal(raw, &elems); err == nil {
			parts := make([]string, 0, len(elems))
			for _, e := range elems {
				if s := stringifyFieldValue(e); s != "" {
					parts = append(parts, s)
				}
			}
			return strings.Join(parts, ", ")
		}
		return ""
	case '{':
		var obj struct {
			Value       string `json:"value"`
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		}
		if err := json.Unmarshal(raw, &obj); err != nil {
			return ""
		}
		switch {
		case obj.Value != "":
			return obj.Value
		case obj.DisplayName != "":
			return obj.DisplayName
		case obj.Name != "":
			return obj.Name
		}
		return ""
	case '"':
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s
		}
		return ""
	default:
		// Number (or other bare scalar): keep its JSON text, trimming a trailing
		// ".0" so an integer-valued field reads as an integer.
		if f, err := strconv.ParseFloat(trimmed, 64); err == nil {
			return strconv.FormatFloat(f, 'f', -1, 64)
		}
		return ""
	}
}

// customFieldType resolves and caches the coarse schema type of a custom field
// by its id (e.g. "customfield_10101" -> "option" / "array" / "number" /
// "string"). It reads GET /rest/api/2/field once and caches every field's type
// keyed by id, so a commit pushing several custom field edits resolves them all
// from one fetch. Returns "" (no error) when the id is not found, so the commit
// can fall back to sending the raw string.
func (c *Client) customFieldType(ctx context.Context, fieldID string) (string, error) {
	if fieldID == "" {
		return "", nil
	}
	c.customFieldMu.Lock()
	if c.customFieldTypesLoaded {
		t := c.customFieldTypes[fieldID]
		c.customFieldMu.Unlock()
		return t, nil
	}
	c.customFieldMu.Unlock()

	var fields []struct {
		ID     string `json:"id"`
		Schema struct {
			Type string `json:"type"`
		} `json:"schema"`
	}
	if err := c.get(ctx, "/rest/api/2/field", &fields); err != nil {
		return "", err
	}
	c.customFieldMu.Lock()
	if c.customFieldTypes == nil {
		c.customFieldTypes = make(map[string]string)
	}
	for _, f := range fields {
		c.customFieldTypes[f.ID] = f.Schema.Type
	}
	c.customFieldTypesLoaded = true
	typ := c.customFieldTypes[fieldID]
	c.customFieldMu.Unlock()
	return typ, nil
}

// CustomFieldValue resolves the typed Jira PUT value for a custom field edit
// given the field id and the stored string value (FR-2.6). It resolves the
// field's schema type (cached) and shapes the value:
//
//   - option / select  -> {"value": v}
//   - array (multi)     -> [{"value": each}] splitting v on ", "
//   - number            -> the parsed number (or the raw string if unparsable)
//   - string / text / unknown -> the raw string
//
// It returns the field id unchanged and the shaped value. Errors from the type
// lookup propagate; an unresolved type degrades to the raw string with a note.
func (c *Client) CustomFieldValue(ctx context.Context, fieldID, value string) (string, any, error) {
	typ, err := c.customFieldType(ctx, fieldID)
	if err != nil {
		return "", nil, err
	}
	return fieldID, shapeCustomFieldValue(typ, value), nil
}

// shapeCustomFieldValue maps a stored custom field string to the JSON value Jira
// expects for the field's schema type. Pure (no network) so it is unit tested
// directly. An unrecognised or empty type defaults to the raw string.
func shapeCustomFieldValue(schemaType, value string) any {
	switch schemaType {
	case "option", "select":
		return map[string]string{"value": value}
	case "array":
		parts := strings.Split(value, ", ")
		opts := make([]map[string]string, 0, len(parts))
		for _, p := range parts {
			if s := strings.TrimSpace(p); s != "" {
				opts = append(opts, map[string]string{"value": s})
			}
		}
		return opts
	case "number":
		if f, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
			return f
		}
		// NOTE(xtm): unparsable number falls back to the raw string; verify
		// against the live instance whether a numeric field tolerates a string.
		return value
	default:
		// string, text, and any type that cannot be resolved on this instance.
		return value
	}
}

// demoCustomFieldDefs is the fixed set of custom fields demo mode exposes on
// the Test issue type.
func demoCustomFieldDefs() []CustomFieldDef {
	return []CustomFieldDef{
		{ID: "customfield_10100", Name: "Test Type", Type: "option"},
		{ID: "customfield_10101", Name: "Automation Status", Type: "option"},
		{ID: "customfield_10102", Name: "Component", Type: "string"},
		{ID: "customfield_10103", Name: "Estimated Duration (min)", Type: "number"},
		{ID: "customfield_20001", Name: "Cucumber Scenario", Type: "string"},
		{ID: "customfield_20002", Name: "Cucumber Test Type", Type: "option"},
		{ID: "customfield_20003", Name: "Generic Test Definition", Type: "string"},
	}
}

var demoTestTypes = []string{"Manual", "Generic", "Cucumber"}
var demoAutomationStatuses = []string{"Not Automated", "In Progress", "Automated"}
var demoComponents = []string{"Frontend", "Backend", "API", "Database", "Auth"}

// demoTestCustomFields produces deterministic custom field values for a Test so
// repeated opens are stable.
func demoTestCustomFields(testKey string) map[string]string {
	h := 0
	for _, r := range testKey {
		h = h*31 + int(r)
	}
	if h < 0 {
		h = -h
	}
	return map[string]string{
		"customfield_10100": demoTestTypes[h%len(demoTestTypes)],
		"customfield_10101": demoAutomationStatuses[(h/3)%len(demoAutomationStatuses)],
		"customfield_10102": demoComponents[(h/7)%len(demoComponents)],
		"customfield_10103": fmt.Sprintf("%d", 5+(h%55)),
	}
}
