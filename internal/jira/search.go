package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Test is a Jira issue of type Test, flattened to the fields the app caches.
type Test struct {
	Key         string
	ID          string
	Summary     string
	Description string
	Status      string
	Priority    string
	Labels      []string
	Components  []string
	Updated     string
	FolderID    string
	// ExecType is the Xray Test Type (a.k.a. execution type): Manual /
	// Automated / Generic / Cucumber. It is a Jira custom field; the live
	// pull resolves the "Test Type" custom field id per instance
	// (Client.testTypeFieldID), requests it, and parses the option value
	// (parseOptionValue). Best-effort: if the field id cannot be resolved the
	// pull proceeds without ExecType. Demo mode populates it via the generator.
	ExecType string
	// FixVersions are the standard Jira Fix Version(s) assigned to this Test
	// issue. Populated by the live pull (fields=...,fixVersions) and the demo
	// generator. Read-only display values; never edited locally.
	FixVersions []string
}

// testFields are the issue fields requested from Jira's search API.
// NOTE(xtm): fixVersions is a standard Jira field (issue.fields.fixVersions,
// an array of {id, name, ...} objects); the name field carries the version
// name string. This shape matches Jira DC REST API 2 conventions and should
// be verified against the live Xray Server/DC 8.4.0 instance.
const testFields = "summary,description,status,priority,labels,components,updated,fixVersions"

// testIssueFields is the typed subset of a Test issue's `fields` object the app
// caches. It is decoded from the raw fields message (testIssue.Fields) so the
// same raw bytes can also yield a dynamically-named custom field (the Xray Test
// Type, whose id varies per instance) without a duplicate-tag decode conflict.
type testIssueFields struct {
	Summary     string   `json:"summary"`
	Description string   `json:"description"`
	Updated     string   `json:"updated"`
	Labels      []string `json:"labels"`
	Status      *struct {
		Name string `json:"name"`
	} `json:"status"`
	Priority *struct {
		Name string `json:"name"`
	} `json:"priority"`
	Components []struct {
		Name string `json:"name"`
	} `json:"components"`
	// FixVersions is the standard Jira Fix Version(s) array. Each element is a
	// version object; only the Name field is stored.
	// NOTE(xtm): the shape {name: "1.5.0", ...} is standard Jira DC; verify
	// against the live Xray Server/DC 8.4.0 instance.
	FixVersions []struct {
		Name string `json:"name"`
	} `json:"fixVersions"`
}

// searchResponse is the /rest/api/2/search payload. Each issue keeps its `fields`
// object as a raw message; the typed fields and the custom Test Type field are
// both decoded from it (see parseIssueTest / execTypeFromRawFields).
type searchResponse struct {
	Total  int `json:"total"`
	Issues []struct {
		ID     string          `json:"id"`
		Key    string          `json:"key"`
		Fields json.RawMessage `json:"fields"`
	} `json:"issues"`
}

// parseIssueTest maps one search/issue payload (raw `fields` plus key/id) into a
// Test, decoding the typed fields and reading the Xray Test Type option value at
// execTypeID (when non-empty) onto ExecType. Pure: no network, so it is unit
// tested via the SearchTestsPage / GetTestFields httptest paths.
func parseIssueTest(id, key string, rawFields json.RawMessage, execTypeID string) Test {
	var f testIssueFields
	_ = json.Unmarshal(rawFields, &f)
	t := Test{
		Key:         key,
		ID:          id,
		Summary:     f.Summary,
		Description: f.Description,
		Updated:     f.Updated,
		Labels:      f.Labels,
	}
	if f.Status != nil {
		t.Status = f.Status.Name
	}
	if f.Priority != nil {
		t.Priority = f.Priority.Name
	}
	for _, comp := range f.Components {
		if comp.Name != "" {
			t.Components = append(t.Components, comp.Name)
		}
	}
	for _, fv := range f.FixVersions {
		if fv.Name != "" {
			t.FixVersions = append(t.FixVersions, fv.Name)
		}
	}
	t.ExecType = execTypeFromRawFields(rawFields, execTypeID)
	return t
}

// parseOptionValue extracts the string value of a Jira single-select custom field
// from its raw JSON. Xray Test Type is typically an option object
// ({"value": "Manual"}) but some instances return a bare string; anything else
// (null, absent, an object without a string "value", malformed JSON) yields "".
func parseOptionValue(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var obj struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj.Value
	}
	return ""
}

// parseOptionValues extracts the string values of a Jira multi-select custom
// field (the Xray Test Environments field) from its raw JSON. Such a field is
// typically an array of option objects ([{"value":"Staging"},{"value":"Chrome"}])
// but some instances return an array of bare strings (["Staging","Chrome"]);
// both shapes are handled. Empty entries are skipped, and null / absent /
// malformed JSON yields a nil slice.
func parseOptionValues(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	// Array of option objects: [{"value": "Staging"}, ...].
	var objs []struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(raw, &objs); err == nil {
		out := make([]string, 0, len(objs))
		for _, o := range objs {
			if v := strings.TrimSpace(o.Value); v != "" {
				out = append(out, v)
			}
		}
		if len(out) > 0 {
			return out
		}
		// Fall through: an array that decoded into objects with no values may have
		// actually been an array of bare strings.
	}
	// Array of bare strings: ["Staging", "Chrome"].
	var strs []string
	if err := json.Unmarshal(raw, &strs); err == nil {
		out := make([]string, 0, len(strs))
		for _, s := range strs {
			if v := strings.TrimSpace(s); v != "" {
				out = append(out, v)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

// execTypeFromRawFields pulls the Test Type option value out of an issue's raw
// `fields` object given the resolved custom field id. Returns "" when fieldID is
// empty, the fields object is absent/malformed, or the field has no option value.
func execTypeFromRawFields(rawFields json.RawMessage, fieldID string) string {
	if fieldID == "" || len(rawFields) == 0 {
		return ""
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rawFields, &fields); err != nil {
		return ""
	}
	return parseOptionValue(fields[fieldID])
}

// SearchTestsPage fetches one page of Test issues for a project, beginning
// at startAt. If `since` is non-empty (RFC3339), only Tests updated at or
// after that time are returned — the incremental-sync path (FR-1.2). The
// caller pages until the total reported by Jira is reached.
func (c *Client) SearchTestsPage(ctx context.Context, projectKey, scopeJQL, since string, startAt, maxResults int) ([]Test, int, error) {
	if isDemoURL(c.baseURL) {
		// Demo mode ignores `since` and the scope so an incremental sync still
		// fills the progress bar against the regenerated dataset.
		tests, total := demoTestsPage(themeFor(c.baseURL), projectKey, startAt, maxResults)
		return tests, total, nil
	}
	jql := fmt.Sprintf("project = %s AND issuetype = Test", projectKey)
	// Per-profile scope override (FR-5.4) — wrapped in parens so it can't break
	// the surrounding clause structure.
	if s := strings.TrimSpace(scopeJQL); s != "" {
		jql += " AND (" + s + ")"
	}
	if extra := incrementalSinceClause(since); extra != "" {
		jql += " AND " + extra
	}
	// Order by a UNIQUE key, not `updated`. Paging with startAt over a non-unique
	// sort is unstable: when many Tests share an `updated` timestamp (common for
	// bulk-imported suites), the tie order shifts between page requests, so some
	// Tests are skipped entirely and never synced — which is why a Test could be
	// listed in its folder by Xray yet be missing from the local cache. Issue key
	// is unique, so pagination is stable and every Test is fetched exactly once.
	jql += " ORDER BY key ASC"

	// Resolve the Test Type custom field id so we can request and read it
	// (FR-2 / exec_type). Best-effort: on error, log and pull without it rather
	// than fail the whole sync.
	fields := testFields
	execTypeID, err := c.testTypeFieldID(ctx)
	if err != nil {
		log.Printf("xtm: resolve Test Type custom field failed, syncing without exec_type: %v", err)
		execTypeID = ""
	}
	if execTypeID != "" {
		fields = testFields + "," + execTypeID
	}

	q := url.Values{}
	q.Set("jql", jql)
	q.Set("startAt", strconv.Itoa(startAt))
	q.Set("maxResults", strconv.Itoa(maxResults))
	q.Set("fields", fields)

	var resp searchResponse
	if err := c.get(ctx, "/rest/api/2/search?"+q.Encode(), &resp); err != nil {
		return nil, 0, err
	}

	tests := make([]Test, 0, len(resp.Issues))
	for _, iss := range resp.Issues {
		tests = append(tests, parseIssueTest(iss.ID, iss.Key, iss.Fields, execTypeID))
	}
	return tests, resp.Total, nil
}

// BugLinkRef is an issue-link reference carried on a TestBasic: the linked
// issue's key and issue type (so the cross-project harvest can keep only the
// links whose target is a defect matching the profile's configured bug issue
// type), plus the linked issue's basics so the harvested bug row carries a
// summary/status/priority instead of a bare key (#219). Populated by both the
// demo generator and the live path, which reads the basics from each Test's
// issuelink expansion (the same shape ListBugs consumes).
type BugLinkRef struct {
	Key        string
	IssueType  string
	LinkID     string
	ProjectKey string
	Summary    string
	Status     string
	Priority   string
}

// TestBasic is the minimal shape of a Test issue used to cache cross-project
// execution members: just enough for the board (summary, status) plus the
// project the Test lives in, plus the Test's issue links so the cross-project
// bug harvest can collect defects reached through a foreign member (#219).
type TestBasic struct {
	Key        string
	Summary    string
	Status     string
	ProjectKey string
	// IssueLinks are the issues this Test links to (key + issue type), used to
	// harvest bugs reached through cross-project member Tests. Populated by both
	// the demo generator and the live `key in (...)` search (it requests
	// fields=...,issuelinks and maps each link's linked issue).
	IssueLinks []BugLinkRef
}

// testBasicLinkedFields are the linked-issue fields carried inside an issuelink
// expansion that a TestBasic needs: summary plus the name-bearing status /
// priority / issuetype objects. Mirrors bugLinkedFields (bugs.go); kept separate
// only so the field set reads with the TestBasic mapping.
type testBasicLinkedFields struct {
	Summary   string    `json:"summary"`
	Status    *nameOnly `json:"status"`
	Priority  *nameOnly `json:"priority"`
	IssueType *nameOnly `json:"issuetype"`
}

// testBasicLinkedIssue is the inward/outward issue carried on an issuelink: its
// key plus the basic fields above.
type testBasicLinkedIssue struct {
	Key    string                `json:"key"`
	Fields testBasicLinkedFields `json:"fields"`
}

// testBasicIssueLink is one entry of a Test's issuelinks: the link id and
// whichever of inward/outward issue is the linked counterpart.
type testBasicIssueLink struct {
	ID           string                `json:"id"`
	InwardIssue  *testBasicLinkedIssue `json:"inwardIssue"`
	OutwardIssue *testBasicLinkedIssue `json:"outwardIssue"`
}

// testBasicFields is the fields object of a Test issue in the basics search: the
// Test-level summary / status / project plus its issuelinks.
type testBasicFields struct {
	Summary string    `json:"summary"`
	Status  *nameOnly `json:"status"`
	Project *struct {
		Key string `json:"key"`
	} `json:"project"`
	IssueLinks []testBasicIssueLink `json:"issuelinks"`
}

// testBasicIssue is one Test issue returned by the basics search.
type testBasicIssue struct {
	Key    string          `json:"key"`
	Fields testBasicFields `json:"fields"`
}

// testBasicResponse is the /rest/api/2/search payload for ListTestsBasic.
type testBasicResponse struct {
	Total  int              `json:"total"`
	Issues []testBasicIssue `json:"issues"`
}

// testBasicFieldList are the issue fields requested by the basics search: enough
// for the board (summary, status), the owning project, and the issuelinks the
// cross-project bug harvest walks.
const testBasicFieldList = "summary,status,project,issuelinks"

// parseTestBasics is the pure mapping from search issues to TestBasic rows: it
// is unit-tested without a network call. Each issue's summary / status / project
// become the TestBasic basics (ProjectKey falls back to the key prefix via
// bugProjectKey when the project object is absent), and every issuelink's
// non-nil inward/outward issue becomes a BugLinkRef carrying the linked issue's
// key, issuetype, link id, project, summary, status, and priority - exactly the
// fields harvestExternalBugs (internal/syncer/engine.go) reads.
func parseTestBasics(issues []testBasicIssue) []TestBasic {
	out := make([]TestBasic, 0, len(issues))
	for _, iss := range issues {
		tb := TestBasic{
			Key:        iss.Key,
			Summary:    iss.Fields.Summary,
			Status:     iss.Fields.Status.nameOr(),
			ProjectKey: bugProjectKey(iss.Key),
		}
		if iss.Fields.Project != nil && iss.Fields.Project.Key != "" {
			tb.ProjectKey = iss.Fields.Project.Key
		}
		for _, lk := range iss.Fields.IssueLinks {
			linked := lk.InwardIssue
			if linked == nil {
				linked = lk.OutwardIssue
			}
			if linked == nil || linked.Key == "" {
				continue
			}
			projectKey := bugProjectKey(linked.Key)
			tb.IssueLinks = append(tb.IssueLinks, BugLinkRef{
				Key:        linked.Key,
				IssueType:  linked.Fields.IssueType.nameOr(),
				LinkID:     lk.ID,
				ProjectKey: projectKey,
				Summary:    linked.Fields.Summary,
				Status:     linked.Fields.Status.nameOr(),
				Priority:   linked.Fields.Priority.nameOr(),
			})
		}
		out = append(out, tb)
	}
	return out
}

// ListTestsBasic fetches the basics (summary, status, project, issue links) of
// the given Test issue keys, regardless of their project, in chunked
// `key in (...)` searches. It backs the external_test cache for cross-project
// execution members (members that live in a different project than the profile's
// and so are never returned by the project-scoped bulk pull), and feeds the
// cross-project bug harvest via each member's issue links.
//
// Demo mode returns deterministic entries for the seeded XRAYINT-* keys (and any
// other key it can parse), including a bug link on at least one member so the
// harvest is exercised offline.
//
// Live path: the keys are chunked (cap bugSearchKeyChunk, ~50 per clause) into
// `key in (...)` JQL searches requesting fields=summary,status,project,
// issuelinks, paged to the reported total. Each returned issue maps to a
// TestBasic (summary, status, project; ProjectKey falls back to the key prefix),
// and every issuelink's non-nil inward/outward issue becomes a BugLinkRef with
// the linked issue's basics, which feeds the cross-project bug harvest. A chunk
// rejected with 400 logs and is skipped (best-effort), mirroring ListBugs; other
// errors abort.
//
// NOTE(xtm): the `key in (...)` search and the issuelink expansion shape (the
// linked issue carrying summary / status / priority / issuetype under fields)
// were implemented per Jira DC conventions and should be verified against the
// live Xray Server/DC 8.4.0 instance. Like ListBugs, if a given instance omits
// the linked-issue fields from the issuelink expansion, a second `key in
// (...)&fields=summary,status,priority,issuetype,project` enrich pass would be
// needed for the linked issues.
func (c *Client) ListTestsBasic(ctx context.Context, keys []string) ([]TestBasic, error) {
	if len(keys) == 0 {
		return []TestBasic{}, nil
	}
	if isDemoURL(c.baseURL) {
		out := make([]TestBasic, 0, len(keys))
		for _, k := range keys {
			out = append(out, demoTestBasicForKey(themeFor(c.baseURL), k))
		}
		return out, nil
	}

	out := []TestBasic{}
	for _, chunk := range chunkKeys(keys, bugSearchKeyChunk) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		issues, err := c.searchTestBasics(ctx, chunk)
		if err != nil {
			var he *HTTPError
			if errors.As(err, &he) && he.Code == http.StatusBadRequest {
				// Bad chunk (e.g. a key the instance rejects); skip it rather
				// than abort the whole pull.
				log.Printf("xtm: test-basics search rejected for chunk of %d keys: %v", len(chunk), err)
				continue
			}
			return nil, err
		}
		out = append(out, parseTestBasics(issues)...)
	}
	return out, nil
}

// searchTestBasics pages a `key in (...)` search over one chunk of Test keys,
// requesting the basics plus issuelinks, and returns the decoded issues.
func (c *Client) searchTestBasics(ctx context.Context, keys []string) ([]testBasicIssue, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	quoted := make([]string, len(keys))
	for i, k := range keys {
		quoted[i] = `"` + strings.TrimSpace(k) + `"`
	}
	jql := "key in (" + strings.Join(quoted, ", ") + ") ORDER BY key ASC"

	issues := []testBasicIssue{}
	startAt := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		q := url.Values{}
		q.Set("jql", jql)
		q.Set("startAt", strconv.Itoa(startAt))
		q.Set("maxResults", "100")
		q.Set("fields", testBasicFieldList)

		var resp testBasicResponse
		if err := c.get(ctx, "/rest/api/2/search?"+q.Encode(), &resp); err != nil {
			return nil, err
		}
		issues = append(issues, resp.Issues...)
		startAt += len(resp.Issues)
		if len(resp.Issues) == 0 || startAt >= resp.Total {
			break
		}
		time.Sleep(throttleContainers)
	}
	return issues, nil
}

// GetTestFields fetches one Test's current field values from Jira — the
// "remote" side of three-way conflict detection at commit (FR-1.4). Demo mode
// returns the deterministically generated Test for the key so the offline path
// never errors.
func (c *Client) GetTestFields(ctx context.Context, key string) (Test, error) {
	if isDemoURL(c.baseURL) {
		return demoTestForKey(themeFor(c.baseURL), key), nil
	}
	// Resolve and request the Test Type custom field so the conflict re-fetch
	// carries exec_type too (consistency with the bulk pull). Best-effort.
	fields := testFields
	execTypeID, err := c.testTypeFieldID(ctx)
	if err != nil {
		log.Printf("xtm: resolve Test Type custom field failed, re-fetching without exec_type: %v", err)
		execTypeID = ""
	}
	if execTypeID != "" {
		fields = testFields + "," + execTypeID
	}

	var resp struct {
		ID     string          `json:"id"`
		Key    string          `json:"key"`
		Fields json.RawMessage `json:"fields"`
	}
	if err := c.get(ctx, "/rest/api/2/issue/"+key+"?fields="+fields, &resp); err != nil {
		return Test{}, err
	}
	t := parseIssueTest(resp.ID, orFallback(resp.Key, key), resp.Fields, execTypeID)
	return t, nil
}

func orFallback(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// resolveTestType finds the plain "Test" issue type id on this instance and
// caches it. Matching is exact on the letters-only name (see normalizeTypeName)
// so "Test Set" / "Test Plan" / "Test Execution" are not mistaken for it.
// Returns an empty id (no error) when the instance has no plain Test type.
func (c *Client) resolveTestType(ctx context.Context) (id, name string, err error) {
	c.testTypeOnce.Do(func() {
		var types []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if e := c.get(ctx, "/rest/api/2/issuetype", &types); e != nil {
			c.testTypeErr = e
			return
		}
		for _, t := range types {
			if normalizeTypeName(t.Name) == "test" {
				c.testTypeID, c.testTypeName = t.ID, t.Name
				return
			}
		}
	})
	return c.testTypeID, c.testTypeName, c.testTypeErr
}

// CreateTest creates a new Xray Test issue and returns its key (FR-1). Demo URLs
// short-circuit to a no-op (the local NEW-N key is kept). Priority is sent only
// when non-empty; labels and components are sent as Jira expects them.
func (c *Client) CreateTest(ctx context.Context, projectKey, summary, description, priority string, labels, components []string) (string, error) {
	if isDemoURL(c.baseURL) {
		return "", nil
	}
	typeID, _, err := c.resolveTestType(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve Test issue type: %w", err)
	}
	if typeID == "" {
		return "", fmt.Errorf("this Jira instance has no plain Test issue type")
	}
	fields := map[string]any{
		"project":   map[string]string{"key": projectKey},
		"issuetype": map[string]string{"id": typeID},
		"summary":   summary,
	}
	if strings.TrimSpace(description) != "" {
		fields["description"] = description
	}
	if strings.TrimSpace(priority) != "" {
		fields["priority"] = map[string]string{"name": priority}
	}
	if len(labels) > 0 {
		fields["labels"] = labels
	}
	if len(components) > 0 {
		comps := make([]map[string]string, 0, len(components))
		for _, name := range components {
			if s := strings.TrimSpace(name); s != "" {
				comps = append(comps, map[string]string{"name": s})
			}
		}
		if len(comps) > 0 {
			fields["components"] = comps
		}
	}
	var resp struct {
		Key string `json:"key"`
	}
	if err := c.writeJSONReturning(ctx, http.MethodPost, "/rest/api/2/issue", map[string]any{"fields": fields}, &resp); err != nil {
		return "", err
	}
	return resp.Key, nil
}

// incrementalSinceClause builds the JQL fragment for an updated-since filter.
// A 1-hour safety buffer is subtracted from the watermark to absorb clock
// skew and Jira's server-timezone interpretation of bare date literals — a
// small overlap is harmless because UpsertTests is idempotent.
func incrementalSinceClause(rfc3339 string) string {
	if rfc3339 == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return ""
	}
	return fmt.Sprintf(`updated >= "%s"`,
		t.Add(-time.Hour).Format("2006-01-02 15:04"))
}

// TODO(xtm): Test Steps (Xray /rest/raven/2.0/api/test/{key}/step) are fetched
// lazily on the detail view, not during sync — one call per Test would be
// 10k+ requests. Test Repository folders and Preconditions (FR-13) follow.
