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

// SearchTestsAcrossProjects browses/searches Tests in the given source
// projects, for cross-project linking of test calls and cloned steps. An empty
// query lists all Tests in the source projects (browse); a non-empty query
// narrows by key or summary. Results are paged from offset (up to limit),
// ordered by key, and the total match count is returned so the caller can
// paginate. No source projects yields no results.
func (c *Client) SearchTestsAcrossProjects(ctx context.Context, projectKeys []string, query string, offset, limit int) ([]TestBasic, int, error) {
	proj := inProjectClause(projectKeys)
	if proj == "" {
		return []TestBasic{}, 0, nil
	}
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	if isDemoURL(c.baseURL) {
		rows, total := demoSearchTestsAcrossProjects(projectKeys, query, offset, limit)
		return rows, total, nil
	}

	jql := proj + " AND issuetype = Test"
	if strings.TrimSpace(query) != "" {
		jql += " AND " + crossProjectClause(query)
	}
	jql += " ORDER BY key ASC"

	q := url.Values{}
	q.Set("jql", jql)
	q.Set("startAt", strconv.Itoa(offset))
	q.Set("maxResults", strconv.Itoa(limit))
	q.Set("fields", testBasicFieldList)

	var resp testBasicResponse
	if err := c.get(ctx, "/rest/api/2/search?"+q.Encode(), &resp); err != nil {
		var he *HTTPError
		if errors.As(err, &he) && he.Code == http.StatusBadRequest {
			// A malformed query (e.g. reserved JQL characters) shouldn't error
			// the picker; return no matches so the user can refine.
			return []TestBasic{}, 0, nil
		}
		return nil, 0, err
	}
	return parseTestBasics(resp.Issues), resp.Total, nil
}

// SearchPreconditionsAcrossProjects browses/searches Precondition issues in the
// given source projects for cross-project precondition linking. An empty query
// lists all; a non-empty query narrows. Paged from offset (up to limit) with a
// total count. No source projects — or an instance without a Precondition issue
// type — yields no results.
func (c *Client) SearchPreconditionsAcrossProjects(ctx context.Context, projectKeys []string, query string, offset, limit int) ([]Precondition, int, error) {
	proj := inProjectClause(projectKeys)
	if proj == "" {
		return []Precondition{}, 0, nil
	}
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	if isDemoURL(c.baseURL) {
		rows, total := demoSearchPreconditionsAcrossProjects(projectKeys, query, offset, limit)
		return rows, total, nil
	}

	typeID, _, err := c.resolvePreconditionType(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("resolve precondition issue type: %w", err)
	}
	if typeID == "" {
		return []Precondition{}, 0, nil
	}

	jql := fmt.Sprintf("%s AND issuetype = %s", proj, typeID)
	if strings.TrimSpace(query) != "" {
		jql += " AND " + crossProjectClause(query)
	}
	jql += " ORDER BY key ASC"

	q := url.Values{}
	q.Set("jql", jql)
	q.Set("startAt", strconv.Itoa(offset))
	q.Set("maxResults", strconv.Itoa(limit))
	q.Set("fields", "summary,description")

	var resp struct {
		Total  int `json:"total"`
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
			return []Precondition{}, 0, nil
		}
		return nil, 0, err
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
	return out, resp.Total, nil
}

// demoCrossProjectFeatures gives the demo browse list varied, queryable
// summaries so both browsing and searching are demonstrable offline.
var demoCrossProjectFeatures = []string{
	"Login flow", "Logout flow", "User registration", "Password reset",
	"Search results", "Checkout", "Add to cart", "Payment", "Refund",
	"Profile update", "Dashboard", "Notifications", "File upload",
	"File download", "Session timeout", "Multi-factor auth", "Admin console",
	"Permissions", "Audit log", "Bulk operations", "Export to CSV", "Import data",
	"Reports", "API rate limit",
}

// demoCrossProjectVariants multiplies the feature list into a larger demo browse
// set (24 features x 3 = 72 items per project), so the picker's pager is
// demonstrable offline (a single project exceeds one 50-item page).
var demoCrossProjectVariants = []string{"", " (regression)", " (edge cases)"}

// demoPageBasics filters a browse list by query (key or summary substring) and
// returns the requested page plus the total match count.
func demoPageBasics(all []TestBasic, query string, offset, limit int) ([]TestBasic, int) {
	q := strings.ToLower(strings.TrimSpace(query))
	filtered := all[:0:0]
	for _, t := range all {
		if q == "" || strings.Contains(strings.ToLower(t.Summary), q) ||
			strings.Contains(strings.ToLower(t.Key), q) {
			filtered = append(filtered, t)
		}
	}
	total := len(filtered)
	if offset >= total {
		return []TestBasic{}, total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return filtered[offset:end], total
}

// demoSearchTestsAcrossProjects returns a deterministic browse list of tests
// across the given source projects (so the picker's project sidebar is
// demonstrable offline), filtered by query and paged. Minimal by design — live
// Jira is the real target.
func demoSearchTestsAcrossProjects(projectKeys []string, query string, offset, limit int) ([]TestBasic, int) {
	all := []TestBasic{}
	for _, src := range projectKeys {
		if src = strings.TrimSpace(src); src == "" {
			continue
		}
		n := 0
		for _, variant := range demoCrossProjectVariants {
			for _, feat := range demoCrossProjectFeatures {
				n++
				all = append(all, TestBasic{
					Key:        fmt.Sprintf("%s-%d", src, n),
					Summary:    feat + variant,
					Status:     "Approved",
					ProjectKey: src,
				})
			}
		}
	}
	return demoPageBasics(all, query, offset, limit)
}

// demoSearchPreconditionsAcrossProjects mirrors demoSearchTestsAcrossProjects
// for preconditions.
func demoSearchPreconditionsAcrossProjects(projectKeys []string, query string, offset, limit int) ([]Precondition, int) {
	q := strings.ToLower(strings.TrimSpace(query))
	all := []Precondition{}
	for _, src := range projectKeys {
		if src = strings.TrimSpace(src); src == "" {
			continue
		}
		n := 0
		for _, variant := range demoCrossProjectVariants {
			for _, feat := range demoCrossProjectFeatures {
				n++
				summary := feat + variant + " precondition"
				key := fmt.Sprintf("%s-P-%d", src, n)
				if q != "" && !strings.Contains(strings.ToLower(summary), q) &&
					!strings.Contains(strings.ToLower(key), q) {
					continue
				}
				all = append(all, Precondition{
					Key:         key,
					Summary:     summary,
					Type:        "Manual",
					Description: fmt.Sprintf("(Demo cross-project precondition: %s%s)", feat, variant),
				})
			}
		}
	}
	total := len(all)
	if offset >= total {
		return []Precondition{}, total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return all[offset:end], total
}
