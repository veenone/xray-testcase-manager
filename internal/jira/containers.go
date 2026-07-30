package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Container kinds — the three Xray issue types that group Tests. The values
// match the Xray REST path segments (testset / testplan / testexec).
const (
	KindTestSet  = "testset"
	KindTestPlan = "testplan"
	KindTestExec = "testexec"
)

// Container mirrors a Xray Test Set, Test Plan or Test Execution issue
// (FR-1.3). The three share a shape — a key, a summary and a workflow status —
// and differ only in how they relate to Tests, so one type with a Kind
// discriminator avoids three near-identical structs.
type Container struct {
	Key           string
	Kind          string
	Summary       string
	Status        string
	ParentKey     string // parent issue key for a sub-task Test Execution; else ""
	ParentSummary string // parent issue summary; empty when no parent or not fetched
	IssueType     string // Jira issuetype name (e.g. "Sub Test Execution"); informational
	// Labels are the standard Jira labels on the container issue (any kind).
	Labels []string
	// Environments is the Xray Test Environments field on a Test Execution
	// (empty for Test Sets / Plans). The live container search resolves the
	// configured "Test Environments" custom field id per instance
	// (Client.testEnvironmentsFieldID), requests it on testexec issues, and
	// parses the multi-value option list (parseOptionValues). Demo mode seeds it.
	Environments []string
	// FixVersions is the standard Jira Fix Version(s) field on a Test Execution
	// (empty for Test Sets / Plans). It is a plain Jira field (array of {name}),
	// requested by literal name on the testexec search and shown read-only, with
	// no custom-field resolution and no editing. Demo mode seeds it.
	FixVersions []string
	// Created is the ISO-8601 creation timestamp of the Test Execution issue
	// (from Jira's standard "created" field). Empty for non-execution containers
	// or when not yet fetched.
	Created string
	// Updated is the ISO-8601 last-update timestamp of the Test Execution issue
	// (from Jira's standard "updated" field). Empty for non-execution containers
	// or when not yet fetched.
	Updated string
	// Resolved is the ISO-8601 resolution timestamp of the Test Execution issue
	// (from Jira's standard "resolutiondate" field). Empty when the execution is
	// unresolved, for non-execution containers, or when not yet fetched.
	Resolved string
	// Description is the issue description (markdown/wiki text) from the Jira
	// standard "description" field. Available for all container kinds when the
	// field is requested. Empty when not fetched.
	Description string
}

// ContainerLink is one Test's membership in a Container. RunStatus carries the
// Test Run result (PASS / FAIL / TODO / …) when the Container is a Test
// Execution; it is empty for Test Sets and Test Plans, which are plain
// memberships.
type ContainerLink struct {
	ContainerKey string
	TestKey      string
	RunStatus    string
}

// ListContainers returns every Test Set, Test Plan and Test Execution for a
// project plus the Test memberships that link them to Tests. Demo URLs
// short-circuit to generated data; the real-Jira call is a best-effort no-op
// pending verification against an actual Xray Server 8.4.0 instance.
//
// TODO(xtm): wire to the Xray Server REST endpoints — JQL search by issuetype
// for the containers, then /rest/raven/2.0/api/{testset,testplan,testexec}/
// {key}/test for memberships (the testexec variant returns run status) — once
// the response shapes can be verified on a live instance.
// onProgress (optional) is called once per container as its Test memberships
// are read — the slow part of a container sync — so the UI can show progress.
func (c *Client) ListContainers(ctx context.Context, projectKey string, onProgress func(done, total int)) ([]Container, []ContainerLink, error) {
	if isDemoURL(c.baseURL) {
		return demoContainersAndLinks(themeFor(c.baseURL), projectKey)
	}

	// NOTE(xtm, #4): this lists containers in projectKey only. To auto-discover
	// cross-project Test Executions (the project's tests run in executions that
	// live in another project — surfaced by the Sankey "cross-project only"
	// filter), also pull each test's executions regardless of project — e.g. GET
	// /rest/raven/2.0/api/test/{key}/testexecs (or a JQL using testTestExecutions)
	// — and append those executions + their run links here. Pending live Xray
	// verification; demo mode already seeds cross-project executions.

	// Gather every container across the three kinds first, so the membership pass
	// below has a known total for progress reporting.
	type kindContainer struct {
		kind string
		key  string
	}
	containers := []Container{}
	all := []kindContainer{}
	for _, kind := range []string{KindTestSet, KindTestPlan, KindTestExec} {
		found, err := c.searchContainers(ctx, projectKey, kind)
		if err != nil {
			return nil, nil, fmt.Errorf("search %s issues: %w", kind, err)
		}
		containers = append(containers, found...)
		for _, ct := range found {
			all = append(all, kindContainer{kind: kind, key: ct.Key})
		}
		log.Printf("xtm: containers %s: %d found for %s", kind, len(found), projectKey)
	}

	// Sub-task Test Executions are a separate Jira issue type that hangs off a
	// parent issue. They are still Kind=testexec and use the same testexec
	// membership endpoint; the extra datum is the parent key. Their issue type
	// name is discovered from the instance (it defaults to "Sub Test Execution"
	// but can be renamed / localised), so a renamed type is not silently missed.
	// Searched on its own issue type so a project that lacks it (a 400, handled
	// as "none") can't affect the standalone Test Execution pass above.
	for _, steType := range c.subTaskTestExecIssueTypeNames(ctx) {
		subExecs, err := c.searchContainersByIssueType(ctx, projectKey, KindTestExec, steType)
		if err != nil {
			return nil, nil, fmt.Errorf("search sub-task test executions (%q): %w", steType, err)
		}
		containers = append(containers, subExecs...)
		for _, ct := range subExecs {
			all = append(all, kindContainer{kind: KindTestExec, key: ct.Key})
		}
		log.Printf("xtm: containers sub-testexec %q: %d found for %s", steType, len(subExecs), projectKey)
	}

	// Pull each container's Test memberships. Best-effort per container so a
	// single inaccessible container can't abort the whole sync.
	links := []ContainerLink{}
	total := len(all)
	for i, kc := range all {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		ls, err := c.listContainerTests(ctx, kc.kind, kc.key)
		if err != nil {
			log.Printf("xtm: container %s members: %v", kc.key, err)
		} else {
			links = append(links, ls...)
		}
		if onProgress != nil {
			onProgress(i+1, total)
		}
		time.Sleep(150 * time.Millisecond)
	}
	return containers, links, nil
}

// TestExecutionsForTest returns every Test Execution (standalone and sub-task,
// any project) that testKey is a member of, as Containers and ContainerLinks.
// This is the per-test cross-project discovery path: unlike ListContainers
// (which is project-scoped), this lookup finds executions in ANY project that
// include testKey, so cross-project sub-task executions are not missed.
//
// Demo mode returns deterministic data from the demo seed (one cross-project
// sub-task exec for member tests at i%11 == 0). The live path is best-effort:
// per-test and per-exec errors are logged and skipped; a top-level network
// failure returns (nil, nil, nil) so the caller can degrade gracefully.
//
// NOTE(xtm): Live endpoint: GET /rest/raven/2.0/api/test/{testKey}/testexec
// returns a JSON array of objects, each with at least a "key" field and
// optionally a "status" field. The per-exec Container detail is fetched via
// GET /rest/api/2/issue/{execKey}?fields=summary,status,issuetype,parent and
// parsed the same way as searchContainersByIssueType. These shapes need
// verification against a live Xray Server/DC 8.4.0 instance.
func (c *Client) TestExecutionsForTest(ctx context.Context, testKey string) ([]Container, []ContainerLink, error) {
	if isDemoURL(c.baseURL) {
		containers, links := demoTestExecutionsForTest(testKey)
		return containers, links, nil
	}

	// Live path: look up the executions this test belongs to.
	// NOTE(xtm): GET /rest/raven/2.0/api/test/{testKey}/testexec is the assumed
	// Xray Server/DC endpoint. Response shape: a JSON array of objects, each
	// with at least a "key" field and an optional "status" field carrying the
	// test's run status in that execution. Verify response shape on a live
	// instance before removing this marker.
	path := "/rest/raven/2.0/api/test/" + url.PathEscape(testKey) + "/testexec"
	body, err := c.getBytes(ctx, path)
	if err != nil {
		log.Printf("xtm: TestExecutionsForTest %s: %v (skipping)", testKey, err)
		return nil, nil, nil
	}

	type execRef struct {
		Key    string `json:"key"`
		Status string `json:"status"`
	}
	var refs []execRef
	if err := json.Unmarshal(body, &refs); err != nil {
		log.Printf("xtm: TestExecutionsForTest %s: decode: %v (skipping)", testKey, err)
		return nil, nil, nil
	}

	containers := make([]Container, 0, len(refs))
	links := make([]ContainerLink, 0, len(refs))

	for _, ref := range refs {
		if ref.Key == "" {
			continue
		}
		if ctx.Err() != nil {
			break
		}

		// Fetch the execution issue detail to populate the Container fields,
		// including the standard Jira timestamp fields so the run-history
		// breakdown can show when the execution was created, updated, and
		// resolved. NOTE(xtm): verify that all requested fields are present on
		// Xray Server/DC 8.4.0; resolutiondate is the standard Jira field name.
		issueURL := "/rest/api/2/issue/" + url.PathEscape(ref.Key) + "?fields=summary,status,issuetype,parent,description,created,updated,resolutiondate,labels"
		var issueResp struct {
			Key    string          `json:"key"`
			Fields json.RawMessage `json:"fields"`
		}
		if err := c.get(ctx, issueURL, &issueResp); err != nil {
			log.Printf("xtm: TestExecutionsForTest %s: fetch exec %s: %v (skipping)", testKey, ref.Key, err)
			time.Sleep(throttleContainers)
			continue
		}

		ct := parseContainerIssue(ref.Key, KindTestExec, issueResp.Fields, "")
		containers = append(containers, ct)
		links = append(links, ContainerLink{
			ContainerKey: ref.Key,
			TestKey:      testKey,
			RunStatus:    strings.ToUpper(strings.TrimSpace(ref.Status)),
		})
		time.Sleep(throttleContainers)
	}
	return containers, links, nil
}

// subTestExecIssueType is the default Jira issue type name for a sub-task Test
// Execution in Xray Server/DC, used as the fallback when the instance issue type
// list cannot be read or yields no match.
const subTestExecIssueType = "Sub Test Execution"

// subTaskTestExecIssueTypeNames returns the issue type name(s) this instance uses
// for sub-task Test Executions. Rather than hardcoding "Sub Test Execution"
// (which fails silently when an instance renames or localises it, because the
// JQL search then 400s and is treated as "none"), it lists the instance issue
// types once and selects every subtask type whose letters-only name contains
// "testexecution" (so "Sub Test Execution", "Test Execution Sub-task", localised
// variants, etc. all match). It falls back to the default name when the listing
// fails or matches nothing. Cached for the client's lifetime; never returns an
// error so the container sync stays best-effort.
func (c *Client) subTaskTestExecIssueTypeNames(ctx context.Context) []string {
	c.subTaskTEOnce.Do(func() {
		var types []struct {
			Name    string `json:"name"`
			Subtask bool   `json:"subtask"`
		}
		if err := c.get(ctx, "/rest/api/2/issuetype", &types); err != nil {
			log.Printf("xtm: list issue types for sub-task Test Execution discovery failed, using default %q: %v", subTestExecIssueType, err)
			return
		}
		seen := map[string]bool{}
		for _, t := range types {
			if t.Subtask && strings.Contains(normalizeTypeName(t.Name), "testexecution") {
				if !seen[t.Name] {
					seen[t.Name] = true
					c.subTaskTENames = append(c.subTaskTENames, t.Name)
				}
			}
		}
		if len(c.subTaskTENames) == 0 {
			log.Printf("xtm: no sub-task Test Execution issue type found in the instance list; falling back to %q", subTestExecIssueType)
		} else {
			log.Printf("xtm: sub-task Test Execution issue type(s) discovered: %v", c.subTaskTENames)
		}
	})
	if len(c.subTaskTENames) > 0 {
		return c.subTaskTENames
	}
	return []string{subTestExecIssueType}
}

// searchContainers finds every container issue of one kind in a project, mapping
// the kind to its standard Jira issue type name.
func (c *Client) searchContainers(ctx context.Context, projectKey, kind string) ([]Container, error) {
	issueType, err := containerIssueType(kind)
	if err != nil {
		return nil, err
	}
	return c.searchContainersByIssueType(ctx, projectKey, kind, issueType)
}

// searchContainersByIssueType finds container issues of an explicit Jira issue
// type in a project via JQL, paging until the reported total is reached, and
// tags each with the supplied Kind. It captures the issue-type name and, for
// sub-task issues, the parent key — so sub-task Test Executions (issue type
// distinct from "Test Execution", Kind still testexec) carry their parent.
func (c *Client) searchContainersByIssueType(ctx context.Context, projectKey, kind, issueType string) ([]Container, error) {
	jql := fmt.Sprintf(`project = "%s" AND issuetype = "%s" ORDER BY key ASC`, projectKey, issueType)

	// Test Environments live on Test Executions only (Sets / Plans do not carry
	// them). Resolve the custom field id once and, when present, request it so the
	// read path can populate Container.Environments. Best-effort: on a resolver
	// error, log and proceed without environments rather than fail the sync.
	fields := "summary,status,issuetype,parent,description,labels"
	envFieldID := ""
	if kind == KindTestExec {
		// Fix Version(s), created, updated, and resolutiondate are standard Jira
		// fields requested by their literal names (no resolver). They are shown
		// read-only on executions. NOTE(xtm): resolutiondate is the standard Jira
		// field name; verify on a live Xray Server/DC 8.4.0 instance.
		fields = fields + ",fixVersions,created,updated,resolutiondate"
		id, err := c.testEnvironmentsFieldID(ctx)
		if err != nil {
			log.Printf("xtm: resolve Test Environments custom field failed, syncing executions without environments: %v", err)
		} else if id != "" {
			envFieldID = id
			fields = fields + "," + envFieldID
		}
	}

	out := []Container{}
	startAt := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		q := url.Values{}
		q.Set("jql", jql)
		q.Set("startAt", strconv.Itoa(startAt))
		q.Set("maxResults", "100")
		q.Set("fields", fields)

		var resp struct {
			Total  int `json:"total"`
			Issues []struct {
				Key string `json:"key"`
				// Fields is kept raw so the typed container fields and the
				// instance-specific Test Environments custom field can both be
				// decoded from the same object without a duplicate-tag conflict.
				Fields json.RawMessage `json:"fields"`
			} `json:"issues"`
		}
		if err := c.get(ctx, "/rest/api/2/search?"+q.Encode(), &resp); err != nil {
			var he *HTTPError
			if errors.As(err, &he) && he.Code == http.StatusBadRequest {
				// This instance/project has no such issue type — treat as none
				// rather than aborting the sync.
				log.Printf("xtm: %s search rejected (issue type %q absent?): %v", kind, issueType, err)
				return out, nil
			}
			return nil, err
		}
		for _, iss := range resp.Issues {
			out = append(out, parseContainerIssue(iss.Key, kind, iss.Fields, envFieldID))
		}
		startAt += len(resp.Issues)
		if len(resp.Issues) == 0 || startAt >= resp.Total {
			break
		}
		time.Sleep(throttleContainers)
	}
	return out, nil
}

// containerIssueFields is the typed subset of a container issue's `fields` object
// the app caches. It is decoded from the raw fields message so the same bytes can
// also yield the instance-specific Test Environments custom field.
type containerIssueFields struct {
	Summary string `json:"summary"`
	Status  *struct {
		Name string `json:"name"`
	} `json:"status"`
	IssueType *struct {
		Name string `json:"name"`
	} `json:"issuetype"`
	Parent *struct {
		Key    string `json:"key"`
		Fields struct {
			Summary string `json:"summary"`
		} `json:"fields"`
	} `json:"parent"`
	FixVersions []struct {
		Name string `json:"name"`
	} `json:"fixVersions"`
	Created        string   `json:"created"`
	Updated        string   `json:"updated"`
	ResolutionDate string   `json:"resolutiondate"`
	Description    string   `json:"description"`
	Labels         []string `json:"labels"`
}

// parseContainerIssue maps one container search issue (raw `fields` plus key) into
// a Container, decoding the typed fields and, for Test Executions with a resolved
// envFieldID, the multi-value Test Environments custom field onto Environments.
// Pure: no network, so it is unit tested via the search httptest path.
func parseContainerIssue(key, kind string, rawFields json.RawMessage, envFieldID string) Container {
	var f containerIssueFields
	_ = json.Unmarshal(rawFields, &f)
	ct := Container{Key: key, Kind: kind, Summary: f.Summary}
	if f.Status != nil {
		ct.Status = f.Status.Name
	}
	if f.IssueType != nil {
		ct.IssueType = f.IssueType.Name
	}
	if f.Parent != nil {
		ct.ParentKey = f.Parent.Key
		ct.ParentSummary = f.Parent.Fields.Summary
	}
	ct.Description = f.Description
	for _, l := range f.Labels {
		if s := strings.TrimSpace(l); s != "" {
			ct.Labels = append(ct.Labels, s)
		}
	}
	if kind == KindTestExec && envFieldID != "" {
		ct.Environments = environmentsFromRawFields(rawFields, envFieldID)
	}
	if kind == KindTestExec {
		for _, v := range f.FixVersions {
			if name := strings.TrimSpace(v.Name); name != "" {
				ct.FixVersions = append(ct.FixVersions, name)
			}
		}
		ct.Created = f.Created
		ct.Updated = f.Updated
		ct.Resolved = f.ResolutionDate
	}
	return ct
}

// environmentsFromRawFields pulls the Test Environments multi-select values out of
// a container issue's raw `fields` object given the resolved custom field id.
// Returns nil when fieldID is empty, the fields object is absent / malformed, or
// the field has no values.
func environmentsFromRawFields(rawFields json.RawMessage, fieldID string) []string {
	if fieldID == "" || len(rawFields) == 0 {
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rawFields, &fields); err != nil {
		return nil
	}
	return parseOptionValues(fields[fieldID])
}

// throttleContainers paces container issue-search pages.
const throttleContainers = 150 * time.Millisecond

// listContainerTests returns the Test memberships of one container. For a Test
// Execution each membership carries the Test's run status; Test Sets and Test
// Plans are plain memberships (run status left empty — the Plan board
// consolidates run status from executions locally).
//
// Maps to GET /rest/raven/2.0/api/{testset|testplan|testexec}/{key}/test, which
// returns a bare array of {id, key, rank, status} objects. Xray caps each
// response (default 200), returning a 400 otherwise, so the call is paged.
func (c *Client) listContainerTests(ctx context.Context, kind, containerKey string) ([]ContainerLink, error) {
	segment, err := containerPathSegment(kind)
	if err != nil {
		return nil, err
	}
	out := []ContainerLink{}
	seen := map[string]bool{}
	for page := 1; page <= ravenMaxPages; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path := fmt.Sprintf("/rest/raven/2.0/api/%s/%s/test?page=%d&limit=%d",
			segment, containerKey, page, ravenPageLimit)
		body, err := c.getBytes(ctx, path)
		if err != nil {
			return nil, err
		}
		links, err := parseContainerTests(kind, containerKey, body)
		if err != nil {
			return nil, err
		}
		added := 0
		for _, l := range links {
			if seen[l.TestKey] {
				continue
			}
			seen[l.TestKey] = true
			out = append(out, l)
			added++
		}
		// Stop on a short page, an empty page, or a page that added nothing new
		// (an instance that ignores the page param won't loop forever).
		if len(links) < ravenPageLimit || added == 0 {
			break
		}
		time.Sleep(throttleContainers)
	}
	return out, nil
}

// Xray paginates its raven "/test" association endpoints with page (1-based) and
// limit (max 200). ravenMaxPages bounds the walk defensively.
const (
	ravenPageLimit = 200
	ravenMaxPages  = 500
)

// parseContainerTests pulls Test keys (and, for executions, run status) out of a
// container's tests response, tolerating a bare array or a {"tests":[…]} wrapper
// the way the folder endpoints do.
func parseContainerTests(kind, containerKey string, body []byte) ([]ContainerLink, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" || trimmed == "null" {
		return []ContainerLink{}, nil
	}
	type ref struct {
		Key     string `json:"key"`
		TestKey string `json:"testKey"`
		Status  string `json:"status"`
	}
	var refs []ref
	switch trimmed[0] {
	case '[':
		if err := json.Unmarshal([]byte(trimmed), &refs); err != nil {
			return nil, fmt.Errorf("decode container tests: %w", err)
		}
	case '{':
		var wrapper struct {
			Tests []ref `json:"tests"`
		}
		if err := json.Unmarshal([]byte(trimmed), &wrapper); err != nil || wrapper.Tests == nil {
			if msg := jiraErrorMessage([]byte(trimmed)); msg != "" {
				return nil, fmt.Errorf("xray could not return container tests: %s", msg)
			}
			return nil, fmt.Errorf("unexpected container tests response: %s", snippet([]byte(trimmed), 256))
		}
		refs = wrapper.Tests
	default:
		return nil, fmt.Errorf("unexpected container tests response: %s", snippet([]byte(trimmed), 256))
	}

	out := make([]ContainerLink, 0, len(refs))
	for _, r := range refs {
		key := r.Key
		if key == "" {
			key = r.TestKey
		}
		if key == "" {
			continue
		}
		link := ContainerLink{ContainerKey: containerKey, TestKey: key}
		if kind == KindTestExec {
			link.RunStatus = strings.ToUpper(strings.TrimSpace(r.Status))
		}
		out = append(out, link)
	}
	return out, nil
}

// containerPathSegment maps a Container kind to its Xray REST path segment.
func containerPathSegment(kind string) (string, error) {
	switch kind {
	case KindTestSet:
		return "testset", nil
	case KindTestPlan:
		return "testplan", nil
	case KindTestExec:
		return "testexec", nil
	}
	return "", fmt.Errorf("unknown container kind %q", kind)
}

// containerIssueType maps a Container kind to its Jira issue type name.
func containerIssueType(kind string) (string, error) {
	switch kind {
	case KindTestSet:
		return "Test Set", nil
	case KindTestPlan:
		return "Test Plan", nil
	case KindTestExec:
		return "Test Execution", nil
	}
	return "", fmt.Errorf("unknown container kind %q", kind)
}

// CreateContainer creates a new Test Set, Test Plan or Test Execution issue and
// returns its key (FR-3.4–3.6). Demo URLs short-circuit to a no-op that still
// returns a real, non-empty, per-call-unique key (see demoCreatedContainerKey),
// since callers (e.g. the coverage publish engine and the board Test Set create
// flow) treat an empty key as a failed create, and a key shared between two
// calls would collide when the board flow renames a local placeholder
// container to it. The demo backend has no persistence, so a container
// created this way is not reflected in a subsequent ListContainers call; the
// placeholder is reconciled on the next full sync.
//
// Maps to POST /rest/api/2/issue with the matching issue type. NOTE(xtm):
// required fields beyond summary vary per project/screen and may need to be
// supplied — verify against a live instance.
func (c *Client) CreateContainer(ctx context.Context, projectKey, kind, summary string) (string, error) {
	if isDemoURL(c.baseURL) {
		return demoCreatedContainerKey(projectKey, kind, summary), nil
	}
	issueType, err := containerIssueType(kind)
	if err != nil {
		return "", err
	}
	body := map[string]any{
		"fields": map[string]any{
			"project":   map[string]string{"key": projectKey},
			"issuetype": map[string]string{"name": issueType},
			"summary":   summary,
		},
	}
	var resp struct {
		Key string `json:"key"`
	}
	if err := c.writeJSONReturning(ctx, http.MethodPost, "/rest/api/2/issue", body, &resp); err != nil {
		return "", err
	}
	return resp.Key, nil
}

// AddTestsToContainer adds Tests to a Test Set, Test Plan or Test Execution
// (FR-3.4–3.6, add-only). Demo URLs short-circuit to a no-op.
//
// Maps to POST /rest/raven/2.0/api/{testset|testplan|testexec}/{key}/test with
// an {"add": [keys]} body. NOTE(xtm): the endpoint + body shape are assumed
// from the Xray Server/DC API and need verification on a live instance.
func (c *Client) AddTestsToContainer(ctx context.Context, kind, containerKey string, testKeys []string) error {
	if len(testKeys) == 0 {
		return nil
	}
	if isDemoURL(c.baseURL) {
		return nil
	}
	segment, err := containerPathSegment(kind)
	if err != nil {
		return err
	}
	body := map[string]any{"add": testKeys}
	return c.post(ctx, fmt.Sprintf("/rest/raven/2.0/api/%s/%s/test", segment, containerKey), body)
}

// SetTestRunStatus sets a Test's run result inside a Test Execution. Demo URLs
// short-circuit to a no-op.
//
// Xray Server/DC has no single "set status by exec+test" call: the Test Run is
// resolved first via GET /rest/raven/1.0/api/testrun?testExecIssueKey=…&
// testIssueKey=… (which returns the run, including its id), then its status is
// set via PUT /rest/raven/1.0/api/testrun/{id}/status?status=<status>.
func (c *Client) SetTestRunStatus(ctx context.Context, execKey, testKey, status string) error {
	if isDemoURL(c.baseURL) {
		return nil
	}
	status = strings.ToUpper(strings.TrimSpace(status))
	if status == "" {
		return fmt.Errorf("a run status is required")
	}

	runID, err := c.resolveTestRunID(ctx, execKey, testKey)
	if err != nil {
		return err
	}

	qs := url.Values{}
	qs.Set("status", status)
	return c.put(ctx, fmt.Sprintf("/rest/raven/1.0/api/testrun/%s/status?%s", runID, qs.Encode()), struct{}{})
}

// resolveTestRunID returns the Xray Test Run id for a Test within an Execution.
//
// When a Test is added to an Execution and its result set in the same commit,
// Xray creates the Test Run for the just-added membership asynchronously, so an
// immediate lookup can return a 400/404 ("no run yet"). That case is retried
// with a short backoff to give Xray a moment to materialise the run; a
// genuinely different error fails fast.
func (c *Client) resolveTestRunID(ctx context.Context, execKey, testKey string) (string, error) {
	q := url.Values{}
	q.Set("testExecIssueKey", execKey)
	q.Set("testIssueKey", testKey)
	path := "/rest/raven/1.0/api/testrun?" + q.Encode()

	backoff := []time.Duration{0, 800 * time.Millisecond, 1600 * time.Millisecond, 3200 * time.Millisecond}
	for attempt, wait := range backoff {
		if wait > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(wait):
			}
		}
		var run struct {
			ID json.Number `json:"id"`
		}
		err := c.get(ctx, path, &run)
		if err == nil {
			if id := run.ID.String(); id != "" && id != "0" {
				return id, nil
			}
			// 200 but no run id — treat like "not ready yet" and keep retrying.
		} else {
			var he *HTTPError
			notReady := errors.As(err, &he) && (he.Code == http.StatusBadRequest || he.Code == http.StatusNotFound)
			if !notReady {
				return "", fmt.Errorf("look up test run for %s in %s: %w", testKey, execKey, err)
			}
		}
		if attempt < len(backoff)-1 {
			log.Printf("xtm: test run for %s in %s not ready yet (attempt %d) — retrying", testKey, execKey, attempt+1)
		}
	}
	return "", fmt.Errorf(
		"%s has no test run in execution %s yet — Xray may still be creating it; "+
			"sync the execution and commit again", testKey, execKey)
}

// AddTestRunDefect links a Bug to a Test's run inside a Test Execution. Demo
// URLs short-circuit to a no-op.
//
// Live endpoint (verified against a live Xray Server/DC instance): POST
// /rest/raven/1.0/api/testrun/{id}/defect with a JSON array body of one or
// more issue keys, e.g. ["BUG-123"].
func (c *Client) AddTestRunDefect(ctx context.Context, execKey, testKey, bugKey string) error {
	if isDemoURL(c.baseURL) {
		return nil
	}
	bugKey = strings.TrimSpace(bugKey)
	if bugKey == "" {
		return fmt.Errorf("a defect key is required")
	}

	runID, err := c.resolveTestRunID(ctx, execKey, testKey)
	if err != nil {
		return err
	}

	return c.post(ctx, fmt.Sprintf("/rest/raven/1.0/api/testrun/%s/defect", runID), []string{bugKey})
}

// RemoveTestRunDefect unlinks a Bug from a Test's run inside a Test
// Execution. Demo URLs short-circuit to a no-op.
//
// Live endpoint (verified against a live Xray Server/DC instance): DELETE
// /rest/raven/1.0/api/testrun/{id}/defect/{key}, the defect key in the path.
func (c *Client) RemoveTestRunDefect(ctx context.Context, execKey, testKey, bugKey string) error {
	if isDemoURL(c.baseURL) {
		return nil
	}
	bugKey = strings.TrimSpace(bugKey)
	if bugKey == "" {
		return fmt.Errorf("a defect key is required")
	}

	runID, err := c.resolveTestRunID(ctx, execKey, testKey)
	if err != nil {
		return err
	}

	return c.delete(ctx, fmt.Sprintf("/rest/raven/1.0/api/testrun/%s/defect/%s", runID, url.PathEscape(bugKey)))
}

// SetTestRunComment sets the free-text comment on a Test's run inside a Test
// Execution. Demo URLs short-circuit to a no-op.
//
// Live endpoint (verified against a live Xray Server/DC instance): PUT
// /rest/raven/1.0/api/testrun/{id}/comment. CRITICAL: the body is the RAW
// comment text, NOT a JSON-encoded string — sending json.Marshal(comment)
// stores the literal surrounding quotes in Xray. The Content-Type header must
// still be application/json (text/plain is rejected with 415). An empty
// comment clears it.
func (c *Client) SetTestRunComment(ctx context.Context, execKey, testKey, comment string) error {
	if isDemoURL(c.baseURL) {
		return nil
	}

	runID, err := c.resolveTestRunID(ctx, execKey, testKey)
	if err != nil {
		return err
	}

	return c.putRaw(ctx, fmt.Sprintf("/rest/raven/1.0/api/testrun/%s/comment", runID), "application/json", []byte(comment))
}

// putRaw performs an authenticated PUT sending body's bytes verbatim (no JSON
// marshaling), with the given Content-Type header. Used where the API expects
// a raw string payload rather than a JSON-encoded value (see
// SetTestRunComment).
func (c *Client) putRaw(ctx context.Context, path, contentType string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", contentType)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf(
			"jira: PUT %s -> %s: %s",
			path, resp.Status, strings.TrimSpace(string(respBody)),
		)
	}
	return nil
}

// SetContainerEnvironments sets the Xray Test Environments field on a Test
// Execution issue. Demo URLs short-circuit to a no-op success so a demo commit
// clears the pending change.
//
// Live path: resolves the configured "Test Environments" custom field id for
// this instance and PUTs /rest/api/2/issue/{key} with {"fields": {<id>: value}}.
// Unlike the read path (which degrades), an unresolvable field returns an error
// so the user's edit is not silently dropped. An empty envs list clears the
// field (sends an empty array).
//
// NOTE(xtm): the multi-select write shape (an array of option objects,
// [{"value": "Staging"}, ...]) and the field NAME ("Test Environments") should be
// verified against the live Xray Server/DC 8.4.0 instance. Some Xray versions
// expose Test Environments through a dedicated raven endpoint rather than a plain
// custom field; if the PUT is rejected, that is the thing to check.
func (c *Client) SetContainerEnvironments(ctx context.Context, execKey string, envs []string) error {
	if isDemoURL(c.baseURL) {
		return nil
	}
	fieldID, err := c.testEnvironmentsFieldID(ctx)
	if err != nil {
		return fmt.Errorf("resolve Test Environments custom field: %w", err)
	}
	if fieldID == "" {
		return fmt.Errorf("no Test Environments custom field found on this Jira instance")
	}
	// A multi-select write value is an array of option objects; an empty list
	// clears the field. Skip empty entries defensively.
	value := make([]map[string]string, 0, len(envs))
	for _, env := range envs {
		if v := strings.TrimSpace(env); v != "" {
			value = append(value, map[string]string{"value": v})
		}
	}
	body := map[string]any{"fields": map[string]any{fieldID: value}}
	return c.put(ctx, "/rest/api/2/issue/"+execKey, body)
}

// DeleteContainer deletes a Test Set, Test Plan or Test Execution issue
// (container CRUD). Demo URLs short-circuit to a no-op.
//
// Maps to DELETE /rest/api/2/issue/{key}. NOTE(xtm): verify on a live instance
// — issue deletion can be permission-restricted; the project may prefer a
// deprecate workflow instead.
func (c *Client) DeleteContainer(ctx context.Context, kind, containerKey string) error {
	if isDemoURL(c.baseURL) {
		return nil
	}
	return c.delete(ctx, fmt.Sprintf("/rest/api/2/issue/%s", containerKey))
}

// RemoveTestsFromContainer removes Tests from a Test Set, Test Plan or Test
// Execution (FR-3.4–3.6). Demo URLs short-circuit to a no-op.
//
// Maps to POST /rest/raven/2.0/api/{testset|testplan|testexec}/{key}/test with
// a {"remove": [keys]} body. NOTE(xtm): endpoint + body shape assumed from the
// Xray Server/DC API; verify on a live instance.
func (c *Client) RemoveTestsFromContainer(ctx context.Context, kind, containerKey string, testKeys []string) error {
	if len(testKeys) == 0 {
		return nil
	}
	if isDemoURL(c.baseURL) {
		return nil
	}
	segment, err := containerPathSegment(kind)
	if err != nil {
		return err
	}
	body := map[string]any{"remove": testKeys}
	return c.post(ctx, fmt.Sprintf("/rest/raven/2.0/api/%s/%s/test", segment, containerKey), body)
}
