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

// CreateBugLink links a Test to a Bug with a Jira issue link. The link type is
// resolved once per client (a defect-oriented type if the instance defines one,
// else the universal "Relates"). Demo URLs no-op.
//
// Maps to POST /rest/api/2/issueLink. NOTE(xtm): the link-type/direction default
// is best-effort - verify the preferred defect link type on a live Xray Server
// 8.4.0 instance.
func (c *Client) CreateBugLink(ctx context.Context, testKey, bugKey string) error {
	if isDemoURL(c.baseURL) {
		return nil
	}
	linkType, err := c.resolveBugLinkType(ctx)
	if err != nil {
		return fmt.Errorf("resolve defect link type: %w", err)
	}
	if linkType == "" {
		return fmt.Errorf("this Jira instance defines no issue link type to link %s and %s", testKey, bugKey)
	}
	// outwardIssue is the Test, inwardIssue the Bug ("Test relates to Bug"). For
	// the symmetric "Relates" default the direction is immaterial, and ListBugs
	// matches links by the linked issue's type, not by link type or direction.
	body := map[string]any{
		"type":         map[string]string{"name": linkType},
		"inwardIssue":  map[string]string{"key": bugKey},
		"outwardIssue": map[string]string{"key": testKey},
	}
	return c.post(ctx, "/rest/api/2/issueLink", body)
}

// resolveBugLinkType picks and caches the issue-link type CreateBugLink uses,
// most specific first: a defect-oriented type, then Jira's universal symmetric
// "Relates", then (fallback) the first type the instance defines. Matching is on
// the letters-only name (normalizeTypeName) so casing/spacing variants resolve.
// An empty name (no error) means the instance defines no link types at all.
func (c *Client) resolveBugLinkType(ctx context.Context) (string, error) {
	c.bugLinkTypeOnce.Do(func() {
		var resp struct {
			IssueLinkTypes []struct {
				Name string `json:"name"`
			} `json:"issueLinkTypes"`
		}
		if e := c.get(ctx, "/rest/api/2/issueLinkType", &resp); e != nil {
			c.bugLinkTypeErr = e
			return
		}
		for _, want := range []string{"defect", "relates"} {
			for _, t := range resp.IssueLinkTypes {
				if strings.Contains(normalizeTypeName(t.Name), want) {
					c.bugLinkTypeName = t.Name
					return
				}
			}
		}
		if len(resp.IssueLinkTypes) > 0 {
			c.bugLinkTypeName = resp.IssueLinkTypes[0].Name
		}
	})
	return c.bugLinkTypeName, c.bugLinkTypeErr
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

// BugDetail holds the extended fields for a defect issue fetched lazily on
// detail-panel open (description plus three instance-specific custom fields).
type BugDetail struct {
	Description       string `json:"description"`
	DefectOrigin      string `json:"defectOrigin"`
	DefectAnalysis    string `json:"defectAnalysis"`
	CorrectionDetails string `json:"correctionDetails"`
	// Reporter is the bug's Jira reporter display name. Severity is the value of
	// the instance-specific "Severity" custom field (when defined).
	Reporter string `json:"reporter"`
	Severity string `json:"severity"`
}

// demoBugDetailOrigins is the pool of Defect Origin values cycled by a simple
// hash of the bug key, so the demo returns a deterministic value per key.
var demoBugDetailOrigins = []string{"Code", "Design", "Requirements", "Test"}

// demoBugDetailReporters and demoBugDetailSeverities are cycled by a hash of the
// bug key for deterministic demo values.
var demoBugDetailReporters = []string{"Alice Tester", "Bob Reviewer", "Carol QA", "Dave Dev"}
var demoBugDetailSeverities = []string{"Critical", "Major", "Minor", "Trivial"}

// GetBugDetail fetches the extended fields for a defect issue: description and
// three instance-specific custom fields (Defect Origin, Defect Analysis,
// Correction Details). Called lazily on detail-panel open so the bulk sync
// does not pay per-bug round-trip costs.
//
// Demo mode: returns a deterministic sample derived from bugKey without any
// network call.
//
// NOTE(xtm): the three custom field display names ("Defect Origin", "Defect
// Analysis", "Correction Details") are instance-specific and must be verified
// and adjusted against the live Xray Server/DC 8.4.0 instance. The fields
// may not exist on every Jira project; resolveCustomFieldID returns "" (no
// error) when a field is absent, and that field is simply omitted from the
// request.
func (c *Client) GetBugDetail(ctx context.Context, bugKey string) (BugDetail, error) {
	if isDemoURL(c.baseURL) {
		// Deterministic hash of the key for the origin cycle.
		h := 0
		for _, ch := range bugKey {
			h = h*31 + int(ch)
		}
		if h < 0 {
			h = -h
		}
		origin := demoBugDetailOrigins[h%len(demoBugDetailOrigins)]
		return BugDetail{
			Description:       "Steps to reproduce: open the affected screen and trigger the failure condition described in the summary.",
			DefectOrigin:      origin,
			DefectAnalysis:    "Root cause: the logic at the point of failure does not handle the edge case introduced by the reported scenario.",
			CorrectionDetails: "Fixed in the next patch: the guard condition was added and the relevant unit test updated.",
			Reporter:          demoBugDetailReporters[h%len(demoBugDetailReporters)],
			Severity:          demoBugDetailSeverities[h%len(demoBugDetailSeverities)],
		}, nil
	}

	// Resolve the three custom field ids by display name; each may be "" when the
	// instance does not define that field.
	originID, err := c.resolveCustomFieldID(ctx, "Defect Origin")
	if err != nil {
		return BugDetail{}, fmt.Errorf("resolve Defect Origin field: %w", err)
	}
	analysisID, err := c.resolveCustomFieldID(ctx, "Defect Analysis")
	if err != nil {
		return BugDetail{}, fmt.Errorf("resolve Defect Analysis field: %w", err)
	}
	correctionID, err := c.resolveCustomFieldID(ctx, "Correction Details")
	if err != nil {
		return BugDetail{}, fmt.Errorf("resolve Correction Details field: %w", err)
	}
	// NOTE(xtm): "Severity" is also instance-specific; resolveCustomFieldID
	// returns "" (no error) when the instance does not define it.
	severityID, err := c.resolveCustomFieldID(ctx, "Severity")
	if err != nil {
		return BugDetail{}, fmt.Errorf("resolve Severity field: %w", err)
	}

	// Build the fields parameter: description and the standard reporter field are
	// always included; custom field ids are added only when the instance defines
	// them.
	fieldParts := []string{"description", "reporter"}
	for _, id := range []string{originID, analysisID, correctionID, severityID} {
		if id != "" {
			fieldParts = append(fieldParts, id)
		}
	}
	q := url.Values{}
	q.Set("fields", strings.Join(fieldParts, ","))

	var resp struct {
		Fields map[string]json.RawMessage `json:"fields"`
	}
	if err := c.get(ctx, "/rest/api/2/issue/"+bugKey+"?"+q.Encode(), &resp); err != nil {
		return BugDetail{}, err
	}

	rawStr := func(raw json.RawMessage) string {
		if len(raw) == 0 {
			return ""
		}
		// description in Jira DC REST v2 is a plain string.
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s
		}
		return stringifyFieldValue(raw)
	}

	var detail BugDetail
	if raw, ok := resp.Fields["description"]; ok {
		detail.Description = rawStr(raw)
	}
	if originID != "" {
		if raw, ok := resp.Fields[originID]; ok {
			detail.DefectOrigin = stringifyFieldValue(raw)
		}
	}
	if analysisID != "" {
		if raw, ok := resp.Fields[analysisID]; ok {
			detail.DefectAnalysis = stringifyFieldValue(raw)
		}
	}
	if correctionID != "" {
		if raw, ok := resp.Fields[correctionID]; ok {
			detail.CorrectionDetails = stringifyFieldValue(raw)
		}
	}
	// Reporter is a standard user object; stringifyFieldValue renders its
	// displayName / name.
	if raw, ok := resp.Fields["reporter"]; ok {
		detail.Reporter = stringifyFieldValue(raw)
	}
	if severityID != "" {
		if raw, ok := resp.Fields[severityID]; ok {
			detail.Severity = stringifyFieldValue(raw)
		}
	}
	return detail, nil
}

// projectBugSearchResponse is the /rest/api/2/search payload for the
// project-wide bug search.
type projectBugSearchResponse struct {
	Total  int               `json:"total"`
	Issues []projectBugIssue `json:"issues"`
}

// projectBugIssue is one issue returned by the project-wide bug search.
type projectBugIssue struct {
	Key    string           `json:"key"`
	Fields projectBugFields `json:"fields"`
}

// projectBugFields holds the fields we request for a project-wide bug search.
type projectBugFields struct {
	Summary   string    `json:"summary"`
	Status    *nameOnly `json:"status"`
	Priority  *nameOnly `json:"priority"`
	IssueType *nameOnly `json:"issuetype"`
	Project   struct {
		Key string `json:"key"`
	} `json:"project"`
	Updated string `json:"updated"`
}

// ListProjectBugs returns all defect issues in projKey whose issuetype
// matches issueType (case-insensitive). Paginated with maxResults=100.
//
// Demo mode: returns the full demo bug set (deduped by key) regardless of
// projKey, so the demo shows a sensible set of bugs without needing
// project-specific seeding.
//
// Live path: issues a paginated JQL search
//
//	project = "<projKey>" AND issuetype = "<issueType>" ORDER BY key ASC
//
// with fields=summary,status,priority,issuetype,project,updated. Best-effort:
// a 400 response logs and returns whatever has been accumulated so far.
//
// NOTE(xtm): the field names and Jira DC pagination behavior follow Jira DC
// conventions and need live verification against the Xray Server/DC 8.4.0
// instance. If a project key or issuetype name differs from the configured
// value the JQL will return 0 results (not an error).
func (c *Client) ListProjectBugs(ctx context.Context, projKey, issueType string) ([]Bug, error) {
	if isDemoURL(c.baseURL) {
		// Return the full demo bug set deduped by key.
		allBugs, _ := demoBugs("", nil)
		seen := map[string]struct{}{}
		out := make([]Bug, 0, len(allBugs))
		for _, b := range allBugs {
			if _, dup := seen[b.Key]; dup {
				continue
			}
			seen[b.Key] = struct{}{}
			out = append(out, b)
		}
		return out, nil
	}

	if strings.TrimSpace(issueType) == "" {
		issueType = "Bug"
	}
	jql := fmt.Sprintf(`project = "%s" AND issuetype = "%s" ORDER BY key ASC`,
		strings.ReplaceAll(projKey, `"`, `\"`),
		strings.ReplaceAll(issueType, `"`, `\"`))

	out := []Bug{}
	startAt := 0
	for {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		q := url.Values{}
		q.Set("jql", jql)
		q.Set("startAt", strconv.Itoa(startAt))
		q.Set("maxResults", "100")
		q.Set("fields", "summary,status,priority,issuetype,project,updated")

		var resp projectBugSearchResponse
		if err := c.get(ctx, "/rest/api/2/search?"+q.Encode(), &resp); err != nil {
			var he *HTTPError
			if errors.As(err, &he) && he.Code == http.StatusBadRequest {
				log.Printf("xtm: project bug search rejected (project=%s, issuetype=%s): %v",
					projKey, issueType, err)
				return out, nil
			}
			return out, err
		}
		for _, iss := range resp.Issues {
			pk := iss.Fields.Project.Key
			if pk == "" {
				pk = bugProjectKey(iss.Key)
			}
			out = append(out, Bug{
				Key:        iss.Key,
				ProjectKey: pk,
				IssueType:  iss.Fields.IssueType.nameOr(),
				Summary:    iss.Fields.Summary,
				Status:     iss.Fields.Status.nameOr(),
				Priority:   iss.Fields.Priority.nameOr(),
				Updated:    iss.Fields.Updated,
			})
		}
		startAt += len(resp.Issues)
		if len(resp.Issues) == 0 || startAt >= resp.Total {
			break
		}
		time.Sleep(throttleContainers)
	}
	return out, nil
}
