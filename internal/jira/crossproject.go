package jira

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Cross-project search backs linking preconditions, test calls, and cloned
// steps from Tests in OTHER projects (RND_P_4TFINT_05-322). The normal pull
// sync is scoped to the profile's own project; these searches deliberately omit
// that scope so a picker can reach shared test assets across projects. Results
// are transient (shown in a picker) — linking still happens by issue key, which
// the commit path already pushes without assuming a single project.

// jqlIssueKeyPattern matches a Jira issue key like "PROJ-123", so a query that
// looks like a key is matched exactly by key rather than by summary text.
var jqlIssueKeyPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*-[0-9]+$`)

// jqlEscape escapes a free-text term for use inside a double-quoted JQL string.
func jqlEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// crossProjectClause builds the "match this query" JQL fragment: an exact key
// match when the query looks like an issue key, otherwise a summary text match.
func crossProjectClause(query string) string {
	q := strings.TrimSpace(query)
	if jqlIssueKeyPattern.MatchString(q) {
		return fmt.Sprintf(`key = "%s"`, q)
	}
	return fmt.Sprintf(`summary ~ "%s"`, jqlEscape(q))
}

// excludeProjectClause returns ` AND project != "X"` when a project key is
// given, so a cross-project search returns only OTHER projects' issues.
func excludeProjectClause(excludeProjectKey string) string {
	if p := strings.TrimSpace(excludeProjectKey); p != "" {
		return fmt.Sprintf(` AND project != "%s"`, p)
	}
	return ""
}

// SearchTestsAcrossProjects finds Tests in projects OTHER than
// excludeProjectKey matching query (by key or summary), for cross-project
// linking of test calls and cloned steps. Returns up to limit lightweight
// basics ordered by key. An empty query yields no results.
func (c *Client) SearchTestsAcrossProjects(ctx context.Context, excludeProjectKey, query string, limit int) ([]TestBasic, error) {
	if strings.TrimSpace(query) == "" {
		return []TestBasic{}, nil
	}
	if limit <= 0 {
		limit = 50
	}
	if isDemoURL(c.baseURL) {
		return demoSearchTestsAcrossProjects(excludeProjectKey, query, limit), nil
	}

	jql := "issuetype = Test AND " + crossProjectClause(query) +
		excludeProjectClause(excludeProjectKey) + " ORDER BY key ASC"

	q := url.Values{}
	q.Set("jql", jql)
	q.Set("startAt", "0")
	q.Set("maxResults", strconv.Itoa(limit))
	q.Set("fields", testBasicFieldList)

	var resp testBasicResponse
	if err := c.get(ctx, "/rest/api/2/search?"+q.Encode(), &resp); err != nil {
		var he *HTTPError
		if errors.As(err, &he) && he.Code == http.StatusBadRequest {
			// A malformed query (e.g. reserved JQL characters) shouldn't error
			// the picker; return no matches so the user can refine.
			return []TestBasic{}, nil
		}
		return nil, err
	}
	return parseTestBasics(resp.Issues), nil
}

// SearchPreconditionsAcrossProjects finds Precondition issues in projects OTHER
// than excludeProjectKey matching query, for cross-project precondition
// linking. Returns up to limit results ordered by key. An empty query yields no
// results; an instance without a Precondition issue type yields no results.
func (c *Client) SearchPreconditionsAcrossProjects(ctx context.Context, excludeProjectKey, query string, limit int) ([]Precondition, error) {
	if strings.TrimSpace(query) == "" {
		return []Precondition{}, nil
	}
	if limit <= 0 {
		limit = 50
	}
	if isDemoURL(c.baseURL) {
		return demoSearchPreconditionsAcrossProjects(excludeProjectKey, query, limit), nil
	}

	typeID, _, err := c.resolvePreconditionType(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve precondition issue type: %w", err)
	}
	if typeID == "" {
		return []Precondition{}, nil
	}

	jql := fmt.Sprintf("issuetype = %s AND %s", typeID, crossProjectClause(query)) +
		excludeProjectClause(excludeProjectKey) + " ORDER BY key ASC"

	q := url.Values{}
	q.Set("jql", jql)
	q.Set("startAt", "0")
	q.Set("maxResults", strconv.Itoa(limit))
	q.Set("fields", "summary,description")

	var resp struct {
		Issues []struct {
			Key    string `json:"key"`
			Fields struct {
				Summary     string `json:"summary"`
				Description string `json:"description"`
			} `json:"fields"`
		} `json:"issues"`
	}
	if err := c.get(ctx, "/rest/api/2/search?"+q.Encode(), &resp); err != nil {
		var he *HTTPError
		if errors.As(err, &he) && he.Code == http.StatusBadRequest {
			return []Precondition{}, nil
		}
		return nil, err
	}
	out := make([]Precondition, 0, len(resp.Issues))
	for _, is := range resp.Issues {
		out = append(out, Precondition{
			Key:         is.Key,
			Summary:     is.Fields.Summary,
			Type:        "Manual",
			Description: is.Fields.Description,
		})
	}
	return out, nil
}

// demoForeignProject is the synthetic other-project key used by the demo
// cross-project search so the pickers are demonstrable offline.
const demoForeignProject = "XRAYINT"

// demoSearchTestsAcrossProjects returns a small deterministic set of foreign
// tests matching the query. Minimal by design — live Jira is the real target.
func demoSearchTestsAcrossProjects(excludeProjectKey, query string, limit int) []TestBasic {
	q := strings.TrimSpace(query)
	if q == "" || strings.EqualFold(strings.TrimSpace(excludeProjectKey), demoForeignProject) {
		return []TestBasic{}
	}
	out := []TestBasic{}
	for i := 1; i <= 3 && i <= limit; i++ {
		out = append(out, TestBasic{
			Key:        fmt.Sprintf("%s-%d", demoForeignProject, i),
			Summary:    fmt.Sprintf("%s (cross-project test %d)", q, i),
			Status:     "Approved",
			ProjectKey: demoForeignProject,
		})
	}
	return out
}

// demoSearchPreconditionsAcrossProjects mirrors demoSearchTestsAcrossProjects
// for preconditions.
func demoSearchPreconditionsAcrossProjects(excludeProjectKey, query string, limit int) []Precondition {
	q := strings.TrimSpace(query)
	if q == "" || strings.EqualFold(strings.TrimSpace(excludeProjectKey), demoForeignProject) {
		return []Precondition{}
	}
	out := []Precondition{}
	for i := 1; i <= 3 && i <= limit; i++ {
		out = append(out, Precondition{
			Key:         fmt.Sprintf("%s-P-%d", demoForeignProject, i),
			Summary:     fmt.Sprintf("%s (cross-project precondition %d)", q, i),
			Type:        "Manual",
			Description: fmt.Sprintf("(Demo cross-project precondition for %q)", q),
		})
	}
	return out
}
