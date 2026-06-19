package jira

import (
	"context"
	"fmt"
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
	// search pull does not request it yet (see SearchTestsPage), so it is
	// populated only in demo mode for now. TODO(xtm): resolve the Test Type
	// custom field id per instance and read it on the live test pull (Phase 7).
	ExecType string
}

// testFields are the issue fields requested from Jira's search API.
const testFields = "summary,description,status,priority,labels,components,updated"

// searchResponse is the /rest/api/2/search payload.
type searchResponse struct {
	Total  int `json:"total"`
	Issues []struct {
		ID     string `json:"id"`
		Key    string `json:"key"`
		Fields struct {
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
		} `json:"fields"`
	} `json:"issues"`
}

// SearchTestsPage fetches one page of Test issues for a project, beginning
// at startAt. If `since` is non-empty (RFC3339), only Tests updated at or
// after that time are returned — the incremental-sync path (FR-1.2). The
// caller pages until the total reported by Jira is reached.
func (c *Client) SearchTestsPage(ctx context.Context, projectKey, scopeJQL, since string, startAt, maxResults int) ([]Test, int, error) {
	if isDemoURL(c.baseURL) {
		// Demo mode ignores `since` and the scope so an incremental sync still
		// fills the progress bar against the regenerated dataset.
		tests, total := demoTestsPage(projectKey, startAt, maxResults)
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

	q := url.Values{}
	q.Set("jql", jql)
	q.Set("startAt", strconv.Itoa(startAt))
	q.Set("maxResults", strconv.Itoa(maxResults))
	q.Set("fields", testFields)

	var resp searchResponse
	if err := c.get(ctx, "/rest/api/2/search?"+q.Encode(), &resp); err != nil {
		return nil, 0, err
	}

	tests := make([]Test, 0, len(resp.Issues))
	for _, iss := range resp.Issues {
		t := Test{
			Key:         iss.Key,
			ID:          iss.ID,
			Summary:     iss.Fields.Summary,
			Description: iss.Fields.Description,
			Updated:     iss.Fields.Updated,
			Labels:      iss.Fields.Labels,
		}
		if iss.Fields.Status != nil {
			t.Status = iss.Fields.Status.Name
		}
		if iss.Fields.Priority != nil {
			t.Priority = iss.Fields.Priority.Name
		}
		for _, comp := range iss.Fields.Components {
			if comp.Name != "" {
				t.Components = append(t.Components, comp.Name)
			}
		}
		tests = append(tests, t)
	}
	return tests, resp.Total, nil
}

// BugLinkRef is an issue-link reference carried on a TestBasic: the linked
// issue's key and issue type (so the cross-project harvest can keep only the
// links whose target is a defect matching the profile's configured bug issue
// type), plus the linked issue's basics so the harvested bug row carries a
// summary/status/priority instead of a bare key (#219). The live path resolves
// the basics from a batch fetch of the linked issues; the demo fills them
// deterministically.
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
	// harvest bugs reached through cross-project member Tests. Populated only in
	// demo mode for now; the live read is a documented TODO(xtm).
	IssueLinks []BugLinkRef
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
// harvest is exercised offline. The real path is a documented TODO(xtm): a live
// `key in (...)` search (fields=summary,status,project,issuelinks) is plausible
// but unverified against an Xray Server/DC instance, so it returns empty rather
// than issuing an unverified call from tests.
func (c *Client) ListTestsBasic(ctx context.Context, keys []string) ([]TestBasic, error) {
	if len(keys) == 0 {
		return []TestBasic{}, nil
	}
	if isDemoURL(c.baseURL) {
		out := make([]TestBasic, 0, len(keys))
		for _, k := range keys {
			out = append(out, demoTestBasicForKey(k))
		}
		return out, nil
	}

	// TODO(xtm): wire the live chunked `key in (...)` search once verified on a
	// real Xray Server/DC instance. The intended shape is:
	//
	//   for each chunk of keys (cap ~50 per `key in (...)`):
	//     jql := `key in (` + strings.Join(chunk, ",") + `)`
	//     GET /rest/api/2/search?jql=...&fields=summary,status,project,issuelinks
	//     map issues -> TestBasic{Key, Summary, Status, ProjectKey, IssueLinks}
	//       where IssueLinks = each issuelink's inward/outward issue {key, issuetype}
	return []TestBasic{}, nil
}

// GetTestFields fetches one Test's current field values from Jira — the
// "remote" side of three-way conflict detection at commit (FR-1.4). Demo mode
// returns the deterministically generated Test for the key so the offline path
// never errors.
func (c *Client) GetTestFields(ctx context.Context, key string) (Test, error) {
	if isDemoURL(c.baseURL) {
		return demoTestForKey(key), nil
	}
	var resp struct {
		ID     string `json:"id"`
		Key    string `json:"key"`
		Fields struct {
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
		} `json:"fields"`
	}
	if err := c.get(ctx, "/rest/api/2/issue/"+key+"?fields="+testFields, &resp); err != nil {
		return Test{}, err
	}
	t := Test{
		Key:         orFallback(resp.Key, key),
		ID:          resp.ID,
		Summary:     resp.Fields.Summary,
		Description: resp.Fields.Description,
		Updated:     resp.Fields.Updated,
		Labels:      resp.Fields.Labels,
	}
	if resp.Fields.Status != nil {
		t.Status = resp.Fields.Status.Name
	}
	if resp.Fields.Priority != nil {
		t.Priority = resp.Fields.Priority.Name
	}
	for _, comp := range resp.Fields.Components {
		if comp.Name != "" {
			t.Components = append(t.Components, comp.Name)
		}
	}
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
