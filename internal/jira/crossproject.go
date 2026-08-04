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

// inProjectClause returns `project in ("A", "B")` for the configured source
// projects, or "" when none are given. Cross-project search is scoped to these
// projects (RND_P_4TFINT_05-322: linking is restricted to configured sources).
func inProjectClause(projectKeys []string) string {
	quoted := make([]string, 0, len(projectKeys))
	for _, p := range projectKeys {
		if p = strings.TrimSpace(p); p != "" {
			quoted = append(quoted, `"`+p+`"`)
		}
	}
	if len(quoted) == 0 {
		return ""
	}
	return "project in (" + strings.Join(quoted, ", ") + ")"
}

// SearchTestsAcrossProjects finds Tests in the given source projects matching
// query (by key or summary), for cross-project linking of test calls and cloned
// steps. Returns up to limit lightweight basics ordered by key. An empty query
// or no source projects yields no results.
func (c *Client) SearchTestsAcrossProjects(ctx context.Context, projectKeys []string, query string, limit int) ([]TestBasic, error) {
	proj := inProjectClause(projectKeys)
	if strings.TrimSpace(query) == "" || proj == "" {
		return []TestBasic{}, nil
	}
	if limit <= 0 {
		limit = 50
	}
	if isDemoURL(c.baseURL) {
		return demoSearchTestsAcrossProjects(projectKeys, query, limit), nil
	}

	jql := proj + " AND issuetype = Test AND " + crossProjectClause(query) +
		" ORDER BY key ASC"

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

// SearchPreconditionsAcrossProjects finds Precondition issues in the given
// source projects matching query, for cross-project precondition linking.
// Returns up to limit results ordered by key. An empty query or no source
// projects yields no results; an instance without a Precondition issue type
// yields no results.
func (c *Client) SearchPreconditionsAcrossProjects(ctx context.Context, projectKeys []string, query string, limit int) ([]Precondition, error) {
	proj := inProjectClause(projectKeys)
	if strings.TrimSpace(query) == "" || proj == "" {
		return []Precondition{}, nil
	}
	if limit <= 0 {
		limit = 50
	}
	if isDemoURL(c.baseURL) {
		return demoSearchPreconditionsAcrossProjects(projectKeys, query, limit), nil
	}

	typeID, _, err := c.resolvePreconditionType(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve precondition issue type: %w", err)
	}
	if typeID == "" {
		return []Precondition{}, nil
	}

	jql := fmt.Sprintf("%s AND issuetype = %s AND %s", proj, typeID, crossProjectClause(query)) +
		" ORDER BY key ASC"

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

// demoSourceProject returns the first configured source project key, so the
// demo search yields results keyed by a project the user actually configured.
func demoSourceProject(projectKeys []string) string {
	for _, p := range projectKeys {
		if p = strings.TrimSpace(p); p != "" {
			return p
		}
	}
	return ""
}

// demoSearchTestsAcrossProjects returns a small deterministic set of tests in
// the first configured source project matching the query. Minimal by design —
// live Jira is the real target.
func demoSearchTestsAcrossProjects(projectKeys []string, query string, limit int) []TestBasic {
	q := strings.TrimSpace(query)
	src := demoSourceProject(projectKeys)
	if q == "" || src == "" {
		return []TestBasic{}
	}
	out := []TestBasic{}
	for i := 1; i <= 3 && i <= limit; i++ {
		out = append(out, TestBasic{
			Key:        fmt.Sprintf("%s-%d", src, i),
			Summary:    fmt.Sprintf("%s (cross-project test %d)", q, i),
			Status:     "Approved",
			ProjectKey: src,
		})
	}
	return out
}

// demoSearchPreconditionsAcrossProjects mirrors demoSearchTestsAcrossProjects
// for preconditions.
func demoSearchPreconditionsAcrossProjects(projectKeys []string, query string, limit int) []Precondition {
	q := strings.TrimSpace(query)
	src := demoSourceProject(projectKeys)
	if q == "" || src == "" {
		return []Precondition{}
	}
	out := []Precondition{}
	for i := 1; i <= 3 && i <= limit; i++ {
		out = append(out, Precondition{
			Key:         fmt.Sprintf("%s-P-%d", src, i),
			Summary:     fmt.Sprintf("%s (cross-project precondition %d)", q, i),
			Type:        "Manual",
			Description: fmt.Sprintf("(Demo cross-project precondition for %q)", q),
		})
	}
	return out
}
