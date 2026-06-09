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
	Key     string
	Kind    string
	Summary string
	Status  string
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
func (c *Client) ListContainers(ctx context.Context, projectKey string) ([]Container, []ContainerLink, error) {
	if isDemoURL(c.baseURL) {
		return demoContainersAndLinks(projectKey)
	}

	containers := []Container{}
	links := []ContainerLink{}
	for _, kind := range []string{KindTestSet, KindTestPlan, KindTestExec} {
		found, err := c.searchContainers(ctx, projectKey, kind)
		if err != nil {
			return nil, nil, fmt.Errorf("search %s issues: %w", kind, err)
		}
		containers = append(containers, found...)

		// Pull each container's Test memberships. Best-effort per container so a
		// single inaccessible container can't abort the whole sync.
		for _, ct := range found {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			ls, err := c.listContainerTests(ctx, kind, ct.Key)
			if err != nil {
				log.Printf("xtm: container %s members: %v", ct.Key, err)
				continue
			}
			links = append(links, ls...)
			time.Sleep(150 * time.Millisecond)
		}
		log.Printf("xtm: containers %s: %d found for %s", kind, len(found), projectKey)
	}
	return containers, links, nil
}

// searchContainers finds every container issue of one kind in a project via JQL,
// paging until the reported total is reached. Returns key / summary / workflow
// status for each.
func (c *Client) searchContainers(ctx context.Context, projectKey, kind string) ([]Container, error) {
	issueType, err := containerIssueType(kind)
	if err != nil {
		return nil, err
	}
	jql := fmt.Sprintf(`project = "%s" AND issuetype = "%s" ORDER BY key ASC`, projectKey, issueType)

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
		q.Set("fields", "summary,status")

		var resp struct {
			Total  int `json:"total"`
			Issues []struct {
				Key    string `json:"key"`
				Fields struct {
					Summary string `json:"summary"`
					Status  *struct {
						Name string `json:"name"`
					} `json:"status"`
				} `json:"fields"`
			} `json:"issues"`
		}
		if err := c.get(ctx, "/rest/api/2/search?"+q.Encode(), &resp); err != nil {
			var he *HTTPError
			if errors.As(err, &he) && he.Code == http.StatusBadRequest {
				// This instance/project has no such container issue type —
				// treat as none rather than aborting the sync.
				log.Printf("xtm: %s search rejected (issue type %q absent?): %v", kind, issueType, err)
				return out, nil
			}
			return nil, err
		}
		for _, iss := range resp.Issues {
			ct := Container{Key: iss.Key, Kind: kind, Summary: iss.Fields.Summary}
			if iss.Fields.Status != nil {
				ct.Status = iss.Fields.Status.Name
			}
			out = append(out, ct)
		}
		startAt += len(resp.Issues)
		if len(resp.Issues) == 0 || startAt >= resp.Total {
			break
		}
		time.Sleep(throttleContainers)
	}
	return out, nil
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
// returns its key (FR-3.4–3.6). Demo URLs short-circuit to a no-op, returning
// an empty key (the demo backend has no persistence, so the placeholder is
// reconciled on the next sync).
//
// Maps to POST /rest/api/2/issue with the matching issue type. NOTE(xtm):
// required fields beyond summary vary per project/screen and may need to be
// supplied — verify against a live instance.
func (c *Client) CreateContainer(ctx context.Context, projectKey, kind, summary string) (string, error) {
	if isDemoURL(c.baseURL) {
		return "", nil
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
// Xray Server/DC has no single-call "set status by exec+test"; the run id is
// resolved first. NOTE(xtm): wire as GET
// /rest/raven/2.0/api/testexec/{execKey}/test to find the testRun id for
// testKey, then PUT /rest/raven/2.0/api/testrun/{id}/status?status=<status>.
// Verify the exact shapes on a live Xray Server 8.4.0 instance.
func (c *Client) SetTestRunStatus(ctx context.Context, execKey, testKey, status string) error {
	_ = ctx
	if isDemoURL(c.baseURL) {
		return nil
	}
	return nil
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
