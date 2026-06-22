package jira

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// defectLinkType is the issue-link type used when linking a Test to a defect.
// "Relates" is universally present in Jira. NOTE(xtm): the exact defect link
// type and direction may vary per Xray instance (some shops use a dedicated
// "Defect"/"is caused by" link); verify against the live Xray Server/DC 8.4.0
// instance and make this configurable if needed.
const defectLinkType = "Relates"

// bugSearchKeyChunk caps how many Test keys go into one `key in (...)` JQL
// clause, keeping the query well under Jira's clause-size limits.
const bugSearchKeyChunk = 50

// Bug is a defect issue (possibly cross-project) linked to Tests.
type Bug struct {
	Key        string
	ProjectKey string
	IssueType  string
	Summary    string
	Status     string
	Priority   string
	Updated    string
}

// BugLink is a Test <-> Bug link.
type BugLink struct {
	TestKey string
	BugKey  string
	LinkID  string
}

// nameOnly is the {"name": ...} shape Jira uses for status / priority /
// issuetype on a linked issue.
type nameOnly struct {
	Name string `json:"name"`
}

// bugLinkedFields are the basic fields Jira includes for the linked issue inside
// an issuelink expansion: summary plus the name-bearing status / priority /
// issuetype objects.
type bugLinkedFields struct {
	Summary   string    `json:"summary"`
	Status    *nameOnly `json:"status"`
	Priority  *nameOnly `json:"priority"`
	IssueType *nameOnly `json:"issuetype"`
}

// bugLinkedIssue is the inward/outward issue carried on an issuelink: its key
// plus the basic fields above.
type bugLinkedIssue struct {
	Key    string          `json:"key"`
	Fields bugLinkedFields `json:"fields"`
}

// bugIssueLink is one entry of a Test's issuelinks: the link id and whichever of
// inward/outward issue is the linked counterpart.
type bugIssueLink struct {
	ID           string          `json:"id"`
	InwardIssue  *bugLinkedIssue `json:"inwardIssue"`
	OutwardIssue *bugLinkedIssue `json:"outwardIssue"`
}

// bugSearchFields is the fields object of a Test issue in the bug search: just
// its issuelinks.
type bugSearchFields struct {
	IssueLinks []bugIssueLink `json:"issuelinks"`
}

// bugSearchIssue is one Test issue returned by the bug search.
type bugSearchIssue struct {
	Key    string          `json:"key"`
	Fields bugSearchFields `json:"fields"`
}

// bugSearchResponse is the /rest/api/2/search payload for the bug harvest.
type bugSearchResponse struct {
	Total  int              `json:"total"`
	Issues []bugSearchIssue `json:"issues"`
}

// bugProjectKey derives a bug's project key from its issue key (everything
// before the last '-'), since the issuelink expansion may not carry a project
// object. Returns "" when the key has no '-'.
func bugProjectKey(bugKey string) string {
	if i := strings.LastIndex(bugKey, "-"); i > 0 {
		return bugKey[:i]
	}
	return ""
}

// parseBugsFromIssueLinks is the pure harvest: given the Test issues from a
// search page (each carrying its issuelinks with the linked issue's basic
// fields) and the configured defect issuetype, it returns the deduped defect
// Bugs and one BugLink per (test, bug) pair. issueType matching is
// case-insensitive and trim-tolerant. A defect linked to several Tests yields a
// single Bug but a link per Test. This is unit-tested without a network call.
func parseBugsFromIssueLinks(issues []bugSearchIssue, issueType string) ([]Bug, []BugLink) {
	want := strings.ToLower(strings.TrimSpace(issueType))
	seen := map[string]struct{}{}
	bugs := []Bug{}
	links := []BugLink{}
	for _, iss := range issues {
		for _, lk := range iss.Fields.IssueLinks {
			linked := lk.InwardIssue
			if linked == nil {
				linked = lk.OutwardIssue
			}
			if linked == nil || linked.Key == "" {
				continue
			}
			itype := strings.ToLower(strings.TrimSpace(linked.Fields.IssueType.nameOr()))
			if itype != want {
				continue
			}
			if _, dup := seen[linked.Key]; !dup {
				seen[linked.Key] = struct{}{}
				b := Bug{
					Key:        linked.Key,
					ProjectKey: bugProjectKey(linked.Key),
					IssueType:  linked.Fields.IssueType.nameOr(),
					Summary:    linked.Fields.Summary,
					Status:     linked.Fields.Status.nameOr(),
					Priority:   linked.Fields.Priority.nameOr(),
					// Updated is not part of the issuelink expansion; left "".
				}
				bugs = append(bugs, b)
			}
			links = append(links, BugLink{
				TestKey: iss.Key,
				BugKey:  linked.Key,
				LinkID:  lk.ID,
			})
		}
	}
	return bugs, links
}

// nameOr returns the name on a *nameOnly, or "" when nil.
func (n *nameOnly) nameOr() string {
	if n == nil {
		return ""
	}
	return n.Name
}

// ListBugs returns the defect issues linked to the given Tests, plus the links.
// issueType is the profile's configured defect issuetype (default "Bug") used to
// recognize which linked issues are defects.
//
// testKeys scopes the result: nil returns the full seed (unfiltered); a non-nil
// empty slice returns nothing; a non-nil non-empty slice filters to only the
// listed Test keys. For the demo path this distinction selects from the seed;
// for the live path nil/empty simply means there are no Tests to search.
//
// Live path: defects are reached through the synced Tests' issue links. The keys
// are chunked into `key in (...)` JQL searches (fields=issuelinks); for each
// returned Test, every issuelink whose linked issue's issuetype matches
// issueType (case-insensitive) is recorded as a defect. The linked issue's basic
// fields (summary, status, priority, issuetype) come from the issuelink
// expansion, which Jira DC includes inline. Bugs are deduped by key; one BugLink
// is emitted per (test, bug) pair. Best-effort per chunk: a 400 logs and skips,
// other errors abort.
//
// NOTE(xtm): the issuelink expansion shape (the linked issue carrying summary /
// status / priority / issuetype under fields) and the defect link type were
// implemented per Jira DC conventions and should be verified against the live
// Xray Server/DC 8.4.0 instance. If a given instance omits the linked issue's
// fields from the search expansion, enrich the harvested keys with a second
// `key in (...)&fields=summary,status,priority,issuetype,project` batch fetch.
func (c *Client) ListBugs(ctx context.Context, testProjectKey string, testKeys []string, issueType string, onProgress func(done, total int)) ([]Bug, []BugLink, error) {
	if isDemoURL(c.baseURL) {
		bugs, links := demoBugs(testProjectKey, testKeys)
		if onProgress != nil {
			onProgress(len(bugs), len(bugs))
		}
		return bugs, links, nil
	}
	if strings.TrimSpace(issueType) == "" {
		issueType = "Bug"
	}

	chunks := chunkKeys(testKeys, bugSearchKeyChunk)
	total := len(testKeys)
	seen := map[string]struct{}{}
	bugs := []Bug{}
	links := []BugLink{}
	done := 0
	for _, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		issues, err := c.searchBugIssueLinks(ctx, chunk)
		if err != nil {
			var he *HTTPError
			if errors.As(err, &he) && he.Code == http.StatusBadRequest {
				// Bad chunk (e.g. a key the instance rejects); skip it rather
				// than abort the whole harvest.
				log.Printf("xtm: bug search rejected for chunk of %d keys: %v", len(chunk), err)
				done += len(chunk)
				if onProgress != nil {
					onProgress(done, total)
				}
				continue
			}
			return nil, nil, err
		}
		cBugs, cLinks := parseBugsFromIssueLinks(issues, issueType)
		for _, b := range cBugs {
			if _, dup := seen[b.Key]; dup {
				continue
			}
			seen[b.Key] = struct{}{}
			bugs = append(bugs, b)
		}
		links = append(links, cLinks...)
		done += len(chunk)
		if onProgress != nil {
			onProgress(done, total)
		}
	}
	return bugs, links, nil
}

// searchBugIssueLinks pages a `key in (...)` search over one chunk of Test keys,
// requesting only issuelinks, and returns the decoded Test issues.
func (c *Client) searchBugIssueLinks(ctx context.Context, keys []string) ([]bugSearchIssue, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	quoted := make([]string, len(keys))
	for i, k := range keys {
		quoted[i] = `"` + strings.TrimSpace(k) + `"`
	}
	jql := "key in (" + strings.Join(quoted, ", ") + ") ORDER BY key ASC"

	issues := []bugSearchIssue{}
	startAt := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		q := url.Values{}
		q.Set("jql", jql)
		q.Set("startAt", strconv.Itoa(startAt))
		q.Set("maxResults", "100")
		q.Set("fields", "issuelinks")

		var resp bugSearchResponse
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

// chunkKeys splits keys into slices of at most size. A non-nil empty input
// yields no chunks (so the live path searches nothing).
func chunkKeys(keys []string, size int) [][]string {
	if size <= 0 {
		size = 1
	}
	var out [][]string
	for i := 0; i < len(keys); i += size {
		end := i + size
		if end > len(keys) {
			end = len(keys)
		}
		out = append(out, keys[i:end])
	}
	return out
}

// CreateBug creates a defect issue of the given issuetype (the profile's
// configured bug issue type, default "Bug") and returns its key. Demo URLs
// return a synthetic key.
//
// Live path: POST /rest/api/2/issue with fields {project:{key}, issuetype:{name:
// issueType}, summary, description (if set), priority:{name} (if set), labels (if
// any)}, mirroring CreateTest. NOTE(xtm): some instances mark extra fields
// mandatory on the defect type (e.g. a detection phase or affects-version); when
// the instance rejects the create, the returned error carries the Jira response
// body which names the missing field. That requirement is instance-specific and
// must be verified live.
func (c *Client) CreateBug(ctx context.Context, projectKey, issueType, summary, description, priority string, labels []string) (string, error) {
	if isDemoURL(c.baseURL) {
		return fmt.Sprintf("%s-BUG-DEMO", projectKey), nil
	}
	if strings.TrimSpace(issueType) == "" {
		issueType = "Bug"
	}
	fields := map[string]any{
		"project":   map[string]string{"key": projectKey},
		"issuetype": map[string]string{"name": issueType},
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
	var resp struct {
		Key string `json:"key"`
	}
	if err := c.writeJSONReturning(ctx, http.MethodPost, "/rest/api/2/issue", map[string]any{"fields": fields}, &resp); err != nil {
		return "", err
	}
	return resp.Key, nil
}

// CreateBugLink links a Test to a Bug. Demo URLs no-op.
//
// Live path: POST /rest/api/2/issueLink with {type:{name:defectLinkType},
// inwardIssue:{key:bugKey}, outwardIssue:{key:testKey}}; Jira answers 201 with no
// body. NOTE(xtm): the defect link type (defectLinkType, default "Relates") and
// its direction may differ per Xray instance; verify against the live instance.
func (c *Client) CreateBugLink(ctx context.Context, testKey, bugKey string) error {
	if isDemoURL(c.baseURL) {
		return nil
	}
	body := map[string]any{
		"type":         map[string]string{"name": defectLinkType},
		"inwardIssue":  map[string]string{"key": bugKey},
		"outwardIssue": map[string]string{"key": testKey},
	}
	return c.writeJSONReturning(ctx, http.MethodPost, "/rest/api/2/issueLink", body, nil)
}

// Two separate defect-tracking projects (both distinct from the test project) so
// the demo shows the feature's cross-project capability — defects rarely live in
// the same project as the tests.
const (
	demoBugProject  = "BUGS"
	demoBugProject2 = "SUP"
)

var demoBugStatuses = []string{"Open", "In Progress", "Reopened", "Done"}
var demoBugPriorities = []string{"Critical", "High", "Medium", "Low"}
var demoBugSummaries = []string{
	"crashes on submit", "returns HTTP 500", "wrong total displayed",
	"times out under load", "validation is bypassed", "data is not persisted",
	"UI freezes intermittently", "incorrect permission check",
	"race condition on save", "leaks memory over time", "off-by-one in pagination",
	"stale cache after edit",
}

// demoFailedTestNums returns the 1-based numbers of demo Tests that carry a FAIL
// run status in their primary execution. Derived from the same demoRunStatuses
// mapping demoContainersAndLinks uses, so the bug seed stays in sync with which
// tests the demo actually fails — keeping the story coherent (a failed test has
// a filed defect).
func demoFailedTestNums(limit int) []int {
	out := make([]int, 0, limit)
	for i := 0; i < demoLinkedTests && i < demoTestCount; i++ {
		if demoRunStatuses[i%len(demoRunStatuses)] == "FAIL" {
			out = append(out, i+1)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

// demoBugs seeds defect issues across two non-test projects, each linked to a
// demo Test that is actually marked FAILED, plus a test with two defects and a
// defect spanning two tests. When scope is non-nil it is the set of in-scope
// Test keys (the synced/ScopeJQL-narrowed tests); only bugs linked to those
// Tests are returned. A nil scope means unfiltered (full seed); an empty,
// non-nil scope means no in-scope tests, so nothing is returned.
func demoBugs(testProjectKey string, scope []string) ([]Bug, []BugLink) {
	if testProjectKey == "" {
		testProjectKey = "DEMO"
	}
	var inScope map[string]bool
	if scope != nil {
		inScope = make(map[string]bool, len(scope))
		for _, k := range scope {
			inScope[k] = true
		}
	}
	testInScope := func(testNum int) bool {
		if inScope == nil {
			return true
		}
		return inScope[fmt.Sprintf("%s-%d", testProjectKey, testNum)]
	}

	failed := demoFailedTestNums(10)
	if len(failed) < 3 {
		return []Bug{}, []BugLink{}
	}

	projects := []string{demoBugProject, demoBugProject2}
	bugs := []Bug{}
	links := []BugLink{}

	addBug := func(testNum int) string {
		n := len(bugs)
		project := projects[n%len(projects)]
		key := fmt.Sprintf("%s-%d", project, 100+n)
		bugs = append(bugs, Bug{
			Key:        key,
			ProjectKey: project,
			IssueType:  "Bug",
			Summary:    fmt.Sprintf("%s-%d %s", testProjectKey, testNum, demoBugSummaries[n%len(demoBugSummaries)]),
			Status:     demoBugStatuses[n%len(demoBugStatuses)],
			Priority:   demoBugPriorities[n%len(demoBugPriorities)],
		})
		return key
	}
	link := func(testNum int, bugKey string) {
		links = append(links, BugLink{
			TestKey: fmt.Sprintf("%s-%d", testProjectKey, testNum),
			BugKey:  bugKey,
			LinkID:  fmt.Sprintf("bl-%d", len(links)+1),
		})
	}

	// One defect per failed, in-scope test.
	for _, n := range failed {
		if testInScope(n) {
			link(n, addBug(n))
		}
	}
	// The first failed test carries a second defect.
	if testInScope(failed[0]) {
		link(failed[0], addBug(failed[0]))
	}
	// One defect spans two failed tests; keep links only for in-scope tests, and
	// only emit the bug if at least one endpoint is in scope.
	if testInScope(failed[1]) || testInScope(failed[2]) {
		spanKey := addBug(failed[1])
		if testInScope(failed[1]) {
			link(failed[1], spanKey)
		}
		if testInScope(failed[2]) {
			link(failed[2], spanKey)
		}
	}

	return bugs, links
}
