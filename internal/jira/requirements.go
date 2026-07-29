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

// Requirement is a requirement issue (possibly in another project) that Tests
// cover for traceability.
type Requirement struct {
	Key         string
	ProjectKey  string
	IssueType   string
	Summary     string
	Status      string
	Updated     string
	Priority    string
	Components  string // comma-separated component names
	FixVersions string // comma-separated fix version names
	Sprint      string // sprint name (best-effort; empty on live — see NOTE below)
	Description string
	// NOTE(xtm): Epic Link is an Agile custom field whose id varies per Jira
	// instance (commonly customfield_10014). It is intentionally omitted from
	// the live field list here because the field id is unknown until verified
	// against the target Xray Server/DC 8.4.0 instance. EpicKey will be empty
	// for live syncs until the field id is configured.
	// TODO(xtm): add the epic link custom field id once confirmed on the live
	// instance and parse it alongside the other fields in searchRequirements.
	EpicKey string
}

// defaultCoverageLinkType is the fallback issue-link type NAME for
// Test->Requirement coverage when no type is configured and the instance
// defines no recognisable coverage link type. The Xray convention names this
// link type "Tests" (inward "tested by" on the requirement, outward "tests"
// on the Test); "tested by" is a direction label, not a link-type name, so it
// would 404 if used as type.name. The configurable path in
// UpdateTestRequirements and resolveRequirementLinkType both take precedence;
// this is only used as a last resort.
// NOTE(xtm): verify the link-type name and direction against the live Xray
// Server/DC 8.4.0 instance; the exact name may differ per instance.
const defaultCoverageLinkType = "Tests"

// RequirementLink is a Test <-> Requirement coverage link.
type RequirementLink struct {
	TestKey        string
	RequirementKey string
	LinkID         string
}

// RequirementSourceSpec is one configured place to search for requirements.
type RequirementSourceSpec struct {
	ProjectKey string
	IssueTypes []string
	ScopeJQL   string
}

// ListRequirements returns requirement issues plus their Test coverage links.
// Demo URLs generate a deterministic cross-project set. The live path JQL-searches
// each configured requirement source for its issues, and harvests coverage links
// from each requirement's `issuelinks`: any linked issue whose key belongs to the
// Test project (profileProjectKey) is treated as a covering Test — a robust
// heuristic that avoids depending on the exact coverage link-type name/direction.
//
// NOTE(xtm): only requirements in a configured source are fetched (and their
// links to synced Tests). Requirements linked to a Test but in a project with no
// configured source are not yet collected (that would require reading every
// Test's issuelinks). Verify the issuelinks shape on a live Xray Server 8.4.0.
func (c *Client) ListRequirements(ctx context.Context, profileProjectKey string, sources []RequirementSourceSpec, onProgress func(done, total int)) ([]Requirement, []RequirementLink, error) {
	if isDemoURL(c.baseURL) {
		reqs, links := demoRequirements(themeFor(c.baseURL), profileProjectKey)
		if onProgress != nil {
			onProgress(len(reqs), len(reqs))
		}
		return reqs, links, nil
	}

	seen := map[string]struct{}{}
	reqs := []Requirement{}
	links := []RequirementLink{}
	total := len(sources)
	for i, src := range sources {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		rs, ls, err := c.searchRequirements(ctx, src, profileProjectKey)
		if err != nil {
			// Best-effort per source: one bad source (permissions, bad scope JQL)
			// must not drop the others.
			log.Printf("xtm: requirement source %s: %v", src.ProjectKey, err)
		} else {
			for _, r := range rs {
				if _, dup := seen[r.Key]; dup {
					continue
				}
				seen[r.Key] = struct{}{}
				reqs = append(reqs, r)
			}
			links = append(links, ls...)
		}
		if onProgress != nil {
			onProgress(i+1, total)
		}
	}
	return reqs, links, nil
}

// requirementJQL builds the search for one source: project, optional issue-type
// filter, optional scope, ordered by key.
func requirementJQL(spec RequirementSourceSpec) string {
	var b strings.Builder
	fmt.Fprintf(&b, `project = "%s"`, spec.ProjectKey)
	if len(spec.IssueTypes) > 0 {
		quoted := make([]string, len(spec.IssueTypes))
		for i, t := range spec.IssueTypes {
			quoted[i] = `"` + strings.TrimSpace(t) + `"`
		}
		fmt.Fprintf(&b, " AND issuetype in (%s)", strings.Join(quoted, ", "))
	}
	if s := strings.TrimSpace(spec.ScopeJQL); s != "" {
		fmt.Fprintf(&b, " AND (%s)", s)
	}
	b.WriteString(" ORDER BY key ASC")
	return b.String()
}

// searchRequirements pages one source's requirement issues and extracts coverage
// links to Tests (issues whose key is in testProjectKey) from their issuelinks.
func (c *Client) searchRequirements(ctx context.Context, spec RequirementSourceSpec, testProjectKey string) ([]Requirement, []RequirementLink, error) {
	jql := requirementJQL(spec)
	testPrefix := testProjectKey + "-"
	reqs := []Requirement{}
	links := []RequirementLink{}
	startAt := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		q := url.Values{}
		q.Set("jql", jql)
		q.Set("startAt", strconv.Itoa(startAt))
		q.Set("maxResults", "100")
		// NOTE(xtm): sprint is an Agile custom field whose id varies per Jira
		// instance (commonly customfield_10020). It is intentionally omitted from
		// the live field list here because the field id is unknown until verified
		// against the target Xray Server/DC 8.4.0 instance. The Sprint field on
		// Requirement will be empty for live syncs until the field id is configured.
		// TODO(xtm): add the sprint custom field id once confirmed on the live
		// instance and parse it alongside the other fields below.
		q.Set("fields", "summary,status,issuetype,project,issuelinks,priority,components,fixVersions,description")

		var resp struct {
			Total  int `json:"total"`
			Issues []struct {
				Key    string `json:"key"`
				Fields struct {
					Summary string `json:"summary"`
					Status  *struct {
						Name string `json:"name"`
					} `json:"status"`
					IssueType *struct {
						Name string `json:"name"`
					} `json:"issuetype"`
					Project *struct {
						Key string `json:"key"`
					} `json:"project"`
					IssueLinks []struct {
						ID          string `json:"id"`
						InwardIssue *struct {
							Key string `json:"key"`
						} `json:"inwardIssue"`
						OutwardIssue *struct {
							Key string `json:"key"`
						} `json:"outwardIssue"`
					} `json:"issuelinks"`
					Priority *struct {
						Name string `json:"name"`
					} `json:"priority"`
					Components []struct {
						Name string `json:"name"`
					} `json:"components"`
					FixVersions []struct {
						Name string `json:"name"`
					} `json:"fixVersions"`
					Description string `json:"description"`
				} `json:"fields"`
			} `json:"issues"`
		}
		if err := c.get(ctx, "/rest/api/2/search?"+q.Encode(), &resp); err != nil {
			var he *HTTPError
			if errors.As(err, &he) && he.Code == http.StatusBadRequest {
				// Bad scope JQL or unknown issue type for this project — treat as
				// no results rather than aborting.
				log.Printf("xtm: requirement search rejected for %s: %v", spec.ProjectKey, err)
				return reqs, links, nil
			}
			return nil, nil, err
		}

		for _, iss := range resp.Issues {
			req := Requirement{Key: iss.Key, Summary: iss.Fields.Summary, ProjectKey: spec.ProjectKey}
			if iss.Fields.Project != nil && iss.Fields.Project.Key != "" {
				req.ProjectKey = iss.Fields.Project.Key
			}
			if iss.Fields.IssueType != nil {
				req.IssueType = iss.Fields.IssueType.Name
			}
			if iss.Fields.Status != nil {
				req.Status = iss.Fields.Status.Name
			}
			if iss.Fields.Priority != nil {
				req.Priority = iss.Fields.Priority.Name
			}
			compNames := make([]string, 0, len(iss.Fields.Components))
			for _, c := range iss.Fields.Components {
				compNames = append(compNames, c.Name)
			}
			req.Components = strings.Join(compNames, ", ")
			fvNames := make([]string, 0, len(iss.Fields.FixVersions))
			for _, fv := range iss.Fields.FixVersions {
				fvNames = append(fvNames, fv.Name)
			}
			req.FixVersions = strings.Join(fvNames, ", ")
			req.Description = iss.Fields.Description
			// Sprint is left empty on live syncs (see NOTE above).
			reqs = append(reqs, req)

			for _, lk := range iss.Fields.IssueLinks {
				testKey := ""
				if lk.InwardIssue != nil && strings.HasPrefix(lk.InwardIssue.Key, testPrefix) {
					testKey = lk.InwardIssue.Key
				}
				if lk.OutwardIssue != nil && strings.HasPrefix(lk.OutwardIssue.Key, testPrefix) {
					testKey = lk.OutwardIssue.Key
				}
				if testKey != "" {
					links = append(links, RequirementLink{
						TestKey:        testKey,
						RequirementKey: req.Key,
						LinkID:         lk.ID,
					})
				}
			}
		}

		startAt += len(resp.Issues)
		if len(resp.Issues) == 0 || startAt >= resp.Total {
			break
		}
		time.Sleep(throttleContainers)
	}
	return reqs, links, nil
}

// UpdateTestRequirements creates and removes Test<->Requirement coverage links.
// add holds requirement keys to link; removeLinkIDs holds the Jira issueLink ids
// to delete (captured into RequirementLink.LinkID during sync). Demo URLs
// short-circuit to a no-op.
//
// Live path: for each removeLinkID, DELETE /rest/api/2/issueLink/{id}; then for
// each requirement key in add, POST /rest/api/2/issueLink with
// {type:{name:linkType}, inwardIssue:{key:test},
// outwardIssue:{key:requirement}} (Jira answers 201 with no body). The Test is
// the inward issue so the requirement renders the link as "tested by"
// (verified live). Blank keys/ids are skipped defensively. This is a commit
// write: the first error encountered is returned so the pending change is
// retried rather than silently reported as success.
//
// Link-type precedence (highest wins):
//  1. Client.requirementLinkType if non-empty (persisted setting, set at construction).
//  2. Auto-resolved via resolveRequirementLinkType (matches by direction, then name).
//  3. Hard-coded fallback defaultCoverageLinkType ("Tests").
func (c *Client) UpdateTestRequirements(ctx context.Context, testKey string, add []string, removeLinkIDs []string) error {
	if isDemoURL(c.baseURL) {
		return nil
	}
	for _, id := range removeLinkIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if err := c.delete(ctx, "/rest/api/2/issueLink/"+id); err != nil {
			return err
		}
	}

	// Only resolve the link type when there is something to add.
	hasAdds := false
	for _, k := range add {
		if strings.TrimSpace(k) != "" {
			hasAdds = true
			break
		}
	}
	if !hasAdds {
		return nil
	}

	// Determine which link type name to use.
	linkType := c.requirementLinkType
	if linkType == "" {
		resolved, err := c.resolveRequirementLinkType(ctx)
		if err != nil {
			return fmt.Errorf("resolve requirement link type: %w", err)
		}
		linkType = resolved
	}
	if linkType == "" {
		linkType = defaultCoverageLinkType
	}

	for _, reqKey := range add {
		reqKey = strings.TrimSpace(reqKey)
		if reqKey == "" {
			continue
		}
		// Direction: the Test is the inward issue and the requirement the
		// outward issue, so the requirement renders the link under "tested by"
		// (the requirement is tested by the test), matching Xray's native
		// coverage. The reverse (requirement inward) renders as "tests" on the
		// requirement, which is wrong. Verified against the live instance.
		body := map[string]any{
			"type":         map[string]string{"name": linkType},
			"inwardIssue":  map[string]string{"key": testKey},
			"outwardIssue": map[string]string{"key": reqKey},
		}
		if err := c.writeJSONReturning(ctx, http.MethodPost, "/rest/api/2/issueLink", body, nil); err != nil {
			return err
		}
	}
	return nil
}

// resolveRequirementLinkType picks and caches the issue-link type used when
// linking a Test to a Requirement (Test->Requirement coverage). It is called
// only when no explicit type is configured. Resolution matches the coverage
// relationship by DIRECTION first (a link type whose inward label is "tested
// by", meaning the requirement is tested by the test, or whose outward label
// is "tests"), because that is how Jira exposes coverage and "tested by" is a
// direction label rather than a link-type name. It then falls back to a
// name-based match ("testedby", "tests", "relates") and finally to the first
// type the instance defines. The resolved value is always a real link-type
// NAME, so the POST in UpdateTestRequirements succeeds. An empty name (no
// error) means the instance defines no link types.
//
// Demo URLs short-circuit to the coverage link resolved from the demo set.
//
// NOTE(xtm): verify the preferred link type and direction on the live Xray
// Server/DC 8.4.0 instance; instances may name it differently.
func (c *Client) resolveRequirementLinkType(ctx context.Context) (string, error) {
	c.covLinkTypeOnce.Do(func() {
		var types []IssueLinkType
		if isDemoURL(c.baseURL) {
			types = demoIssueLinkTypes()
		} else {
			var resp struct {
				IssueLinkTypes []IssueLinkType `json:"issueLinkTypes"`
			}
			if e := c.get(ctx, "/rest/api/2/issueLinkType", &resp); e != nil {
				c.covLinkTypeErr = e
				return
			}
			types = resp.IssueLinkTypes
		}
		// Direction match: the coverage link is the one whose inward label is
		// "tested by" or outward label is "tests", regardless of its name.
		for _, t := range types {
			if strings.Contains(normalizeTypeName(t.Inward), "testedby") ||
				strings.Contains(normalizeTypeName(t.Outward), "tests") {
				c.covLinkTypeName = t.Name
				return
			}
		}
		// Name-based fallback.
		for _, want := range []string{"testedby", "tests", "relates"} {
			for _, t := range types {
				if strings.Contains(normalizeTypeName(t.Name), want) {
					c.covLinkTypeName = t.Name
					return
				}
			}
		}
		if len(types) > 0 {
			c.covLinkTypeName = types[0].Name
		}
	})
	return c.covLinkTypeName, c.covLinkTypeErr
}

// IssueLinkType is a Jira issue-link type together with its directional
// labels. Name is what a link is created with (POST type.name); Inward and
// Outward are the direction descriptions Jira shows in its UI. The coverage
// link type is typically named "Tests" with Inward "tested by" and Outward
// "tests", so "tested by" is a direction label, never a link-type name.
type IssueLinkType struct {
	Name    string `json:"name"`
	Inward  string `json:"inward"`
	Outward string `json:"outward"`
}

// demoIssueLinkTypes mirrors a realistic Jira instance for demo mode: the
// coverage link is named "Tests" with the "tested by" / "tests" directions,
// so the dropdown and auto-resolve behave exactly as they do live.
func demoIssueLinkTypes() []IssueLinkType {
	return []IssueLinkType{
		{Name: "Tests", Inward: "tested by", Outward: "tests"},
		{Name: "Relates", Inward: "is related to", Outward: "relates to"},
		{Name: "Blockers", Inward: "is blocked by", Outward: "blocks"},
		{Name: "Cloners", Inward: "is cloned from", Outward: "clones to"},
		{Name: "Duplicate", Inward: "is duplicated by", Outward: "duplicates"},
	}
}

// ListIssueLinkTypes returns the names of all issue-link types defined on this
// Jira instance. Retained for callers that only need names; the config
// dropdown uses ListIssueLinkTypeDetails so it can show direction labels.
// Demo URLs return a preset list without a network call.
//
// NOTE(xtm): the available types vary per Jira instance; verify against the live
// Xray Server/DC 8.4.0 instance before using the result in UI logic.
func (c *Client) ListIssueLinkTypes(ctx context.Context) ([]string, error) {
	details, err := c.ListIssueLinkTypeDetails(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(details))
	for _, t := range details {
		names = append(names, t.Name)
	}
	return names, nil
}

// ListIssueLinkTypeDetails returns every issue-link type defined on this Jira
// instance with its name and inward/outward direction labels, for the
// requirement-link-type config dropdown. Demo URLs return a preset list
// without a network call.
//
// NOTE(xtm): the available types vary per Jira instance; verify against the live
// Xray Server/DC 8.4.0 instance before using the result in UI logic.
func (c *Client) ListIssueLinkTypeDetails(ctx context.Context) ([]IssueLinkType, error) {
	if isDemoURL(c.baseURL) {
		return demoIssueLinkTypes(), nil
	}
	var resp struct {
		IssueLinkTypes []IssueLinkType `json:"issueLinkTypes"`
	}
	if err := c.get(ctx, "/rest/api/2/issueLinkType", &resp); err != nil {
		return nil, err
	}
	return resp.IssueLinkTypes, nil
}

// DeleteRequirement deletes a requirement issue (often cross-project). Demo URLs
// short-circuit to a no-op.
//
// Maps to DELETE /rest/api/2/issue/{key}. NOTE(xtm): deletion is
// permission-sensitive, especially across projects; verify on a live instance.
func (c *Client) DeleteRequirement(ctx context.Context, requirementKey string) error {
	if isDemoURL(c.baseURL) {
		return nil
	}
	return c.delete(ctx, fmt.Sprintf("/rest/api/2/issue/%s", requirementKey))
}

// demoRequirementProject is a separate project from the Tests' project, so demo
// mode exercises the cross-project case.
const demoRequirementProject = "PRD"

var demoReqStatuses = []string{"Open", "In Progress", "Approved", "Done"}

// demoReqSprints is a small set of sprint names used to populate the Sprint
// field on demo requirements, mirroring what an Agile board would carry.
var demoReqSprints = []string{
	"Sprint 1", "Sprint 2", "Sprint 3", "Sprint 4",
	"Backlog", "Sprint 5",
}

// demoReqFixVersions is the fix-version pool for demo requirements.
var demoReqFixVersions = []string{"1.5.0", "1.6.0", "1.7.0"}

// demoReqDescriptions provides short Markdown descriptions for demo requirements,
// cycled deterministically across the generated set.
var demoReqDescriptions = []string{
	"As a user, I want this feature so that I can complete my workflow efficiently.\n\n**Acceptance criteria:**\n- Scenario is handled correctly\n- Edge cases are covered",
	"The system must support this capability to meet compliance requirements.\n\n- Must be auditable\n- Must be reversible",
	"This requirement captures the expected behaviour under normal operating conditions.\n\n> See linked test cases for coverage details.",
	"Performance target: p95 latency under 200 ms at peak load.\n\n- Measured in staging\n- Baseline established in Sprint 2",
	"Security control: all inputs validated and sanitised before processing.",
}

// demoRequirements generates requirement issues linked to demo Tests.
//
// Generic branch (theme.Variant == ""): ~24 PRD requirements linked to
// run-status-bearing Tests, with a couple left uncovered so coverage spans
// PASSED/FAILED/NOTRUN/UNCOVERED, populated with the 1.8.0 requirement detail
// fields (priority, components, fix versions, sprint, description).
//
// PKCS branch (theme.Variant == "pkcs"): for each PKCS#11 feature one
// functional requirement (FUNC project) and two customer requirements
// (CUST-HSM-BANK and CUST-HSM-SAMSU), each linked to the first 6 Tests of
// that feature (tests whose (n-1)%len(features) == featureIndex).
func demoRequirements(theme demoTheme, testProjectKey string) ([]Requirement, []RequirementLink) {
	if testProjectKey == "" {
		testProjectKey = "DEMO"
	}

	// eUICC branch — functional + per-customer requirements.
	// For each RSP procedure: one FUNC-EUICC-<code> functional requirement and
	// three customer requirements (CUST-MNO-CONSUMER/MNO, CUST-IOT-FLEET/IOT,
	// CUST-M2M-AUTO/M2M), each linked to the first up-to-6 tests of that feature.
	if theme.Variant == "euicc" {
		nFeatures := len(theme.Features)
		reqs := make([]Requirement, 0, nFeatures*4)
		links := make([]RequirementLink, 0)
		const maxLinksPerReq = 6
		for fi, f := range theme.Features {
			code := euiccCode(f)

			// Functional requirement.
			funcKey := "FUNC-EUICC-" + code
			reqs = append(reqs, Requirement{
				Key:        funcKey,
				ProjectKey: "FUNC",
				IssueType:  "Requirement",
				Summary:    f,
				Status:     "Approved",
			})

			// Customer requirements + links to feature's tests.
			customers := []struct {
				projectKey string
				label      string
			}{
				{"CUST-MNO-CONSUMER", "MNO"},
				{"CUST-IOT-FLEET", "IOT"},
				{"CUST-M2M-AUTO", "M2M"},
			}
			for _, cust := range customers {
				custKey := cust.projectKey + "-" + code
				reqs = append(reqs, Requirement{
					Key:        custKey,
					ProjectKey: cust.projectKey,
					IssueType:  "Story",
					Summary:    f + " — " + cust.label + " customer requirement",
					Status:     "In Progress",
				})
				// Link to first up-to-6 tests for this feature.
				// Tests for feature fi are those where (n-1) % nFeatures == fi,
				// i.e. n = fi+1, fi+1+nFeatures, fi+1+2*nFeatures, ...
				linked := 0
				for n := fi + 1; n <= theme.TestCount && linked < maxLinksPerReq; n += nFeatures {
					links = append(links, RequirementLink{
						TestKey:        fmt.Sprintf("%s-%d", testProjectKey, n),
						RequirementKey: custKey,
						LinkID:         fmt.Sprintf("L-%s-%d", code, n),
					})
					linked++
				}
			}
		}
		return reqs, links
	}

	// PKCS branch — functional + per-customer requirements.
	if theme.Variant == "pkcs" {
		nFeatures := len(theme.Features)
		reqs := make([]Requirement, 0, nFeatures*3)
		links := make([]RequirementLink, 0)
		const maxLinksPerReq = 6
		for fi, f := range theme.Features {
			code := pkcsCode(f)

			// Functional requirement.
			funcKey := "FUNC-PKCS11-" + code
			reqs = append(reqs, Requirement{
				Key:        funcKey,
				ProjectKey: "FUNC",
				IssueType:  "Requirement",
				Summary:    f,
				Status:     "Approved",
			})

			// Customer requirements + links to feature's tests.
			customers := []struct {
				projectKey string
				label      string
			}{
				{"CUST-HSM-BANK", "BANK"},
				{"CUST-HSM-SAMSU", "SAMSU"},
			}
			for _, cust := range customers {
				custKey := cust.projectKey + "-" + code
				reqs = append(reqs, Requirement{
					Key:        custKey,
					ProjectKey: cust.projectKey,
					IssueType:  "Story",
					Summary:    f + " — " + cust.label + " customer requirement",
					Status:     "In Progress",
				})
				// Link to first up-to-6 tests for this feature.
				// Tests for feature fi are those where (n-1) % nFeatures == fi,
				// i.e. n = fi+1, fi+1+nFeatures, fi+1+2*nFeatures, ...
				linked := 0
				for n := fi + 1; n <= theme.TestCount && linked < maxLinksPerReq; n += nFeatures {
					links = append(links, RequirementLink{
						TestKey:        fmt.Sprintf("%s-%d", testProjectKey, n),
						RequirementKey: custKey,
						LinkID:         fmt.Sprintf("L-%s-%d", code, n),
					})
					linked++
				}
			}
		}
		return reqs, links
	}

	// Generic branch — unchanged PRD-1..24 logic.
	const count = 24
	reqs := make([]Requirement, 0, count)
	links := make([]RequirementLink, 0)
	for r := 1; r <= count; r++ {
		itype := "Story"
		if r%5 == 0 {
			itype = "Epic"
		}
		reqKey := fmt.Sprintf("%s-%d", demoRequirementProject, r)

		// Deterministic multi-value fields for realistic demo data.
		compFirst := demoComponentNames[r%len(demoComponentNames)]
		compSecond := demoComponentNames[(r*3+2)%len(demoComponentNames)]
		components := compFirst
		if r%3 == 0 && compSecond != compFirst {
			components = compFirst + ", " + compSecond
		}
		fv := demoReqFixVersions[r%len(demoReqFixVersions)]
		sprint := demoReqSprints[r%len(demoReqSprints)]

		// Assign most requirements to one of three demo epics; leave every 6th
		// requirement without an epic to exercise the no-epic fallback path.
		var epicKey string
		if r%6 != 0 {
			epicKey = fmt.Sprintf("DEMO-EPIC-%d", (r%3)+1)
		}

		reqs = append(reqs, Requirement{
			Key:         reqKey,
			ProjectKey:  demoRequirementProject,
			IssueType:   itype,
			Summary:     demoFeatures[(r-1)%len(demoFeatures)] + " requirement",
			Status:      demoReqStatuses[r%len(demoReqStatuses)],
			Priority:    demoPriorities[r%len(demoPriorities)],
			Components:  components,
			FixVersions: fv,
			Sprint:      sprint,
			Description: demoReqDescriptions[(r-1)%len(demoReqDescriptions)],
			EpicKey:     epicKey,
		})
		// Leave the last two uncovered.
		if r > count-2 {
			continue
		}
		linkCount := 1 + (r % 4) // 1..4 covering tests
		for j := 0; j < linkCount; j++ {
			testNum := ((r-1)*7+j*13)%demoLinkedTests + 1 // within the run-status tests
			links = append(links, RequirementLink{
				TestKey:        fmt.Sprintf("%s-%d", testProjectKey, testNum),
				RequirementKey: reqKey,
				LinkID:         fmt.Sprintf("%d-%d", r, j),
			})
		}
	}
	return reqs, links
}

// ReqToReqLink is a Requirement -> Requirement directional issue link (e.g.
// "requires") discovered during a requirements sync.
type ReqToReqLink struct {
	FromKey  string
	ToKey    string
	LinkType string
	LinkID   string
}

// resolveReqReqLinkType picks and caches the issue-link type for
// Requirement->Requirement links. Preferred candidates (first match wins):
// "requires", "Requires", "depends on", "Depends". Falls back to "Relates" or
// the first available type. An empty name (no error) means no types are defined.
func (c *Client) resolveReqReqLinkType(ctx context.Context) (string, error) {
	c.reqLinkTypeOnce.Do(func() {
		var resp struct {
			IssueLinkTypes []struct {
				Name string `json:"name"`
			} `json:"issueLinkTypes"`
		}
		if e := c.get(ctx, "/rest/api/2/issueLinkType", &resp); e != nil {
			c.reqLinkTypeErr = e
			return
		}
		for _, want := range []string{"requires", "depends", "relates"} {
			for _, t := range resp.IssueLinkTypes {
				if strings.Contains(normalizeTypeName(t.Name), want) {
					c.reqLinkTypeName = t.Name
					return
				}
			}
		}
		if len(resp.IssueLinkTypes) > 0 {
			c.reqLinkTypeName = resp.IssueLinkTypes[0].Name
		}
	})
	return c.reqLinkTypeName, c.reqLinkTypeErr
}

// UpdateRequirementLinks creates and removes Requirement->Requirement issue
// links. add holds the target requirement keys to link; removeLinkIDs holds the
// Jira issueLink ids to delete. Demo URLs short-circuit to a no-op.
//
// Live path: for each removeLinkID, DELETE /rest/api/2/issueLink/{id}; then for
// each key in add, POST /rest/api/2/issueLink with the resolved link type
// (prefers "requires"/"depends on"). Mirroring UpdateTestRequirements.
//
// NOTE(xtm): the exact "requires" link type name and its direction (outward vs
// inward) vary per Jira instance and must be verified against the live Xray
// Server/DC 8.4.0 instance before enabling. The live path is stubbed here;
// wire it the same way as UpdateTestRequirements once the link type is
// confirmed.
//
// TODO(xtm): wire the live commit push for req_req_link_set pending changes
// (entity_type "req_req_link_set") analogously to commitRequirements in
// internal/syncer/commit.go -- diff before vs after, DELETE removed link ids,
// POST new links with the resolved type name.
func (c *Client) UpdateRequirementLinks(ctx context.Context, fromKey string, add []string, removeLinkIDs []string) error {
	if isDemoURL(c.baseURL) {
		return nil
	}
	linkType, err := c.resolveReqReqLinkType(ctx)
	if err != nil {
		return fmt.Errorf("resolve req link type: %w", err)
	}
	if linkType == "" {
		return fmt.Errorf("this Jira instance defines no issue link type to link %s to its requirements", fromKey)
	}
	for _, id := range removeLinkIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if err := c.delete(ctx, "/rest/api/2/issueLink/"+id); err != nil {
			return err
		}
	}
	for _, toKey := range add {
		toKey = strings.TrimSpace(toKey)
		if toKey == "" {
			continue
		}
		body := map[string]any{
			"type":         map[string]string{"name": linkType},
			"outwardIssue": map[string]string{"key": fromKey},
			"inwardIssue":  map[string]string{"key": toKey},
		}
		if err := c.writeJSONReturning(ctx, http.MethodPost, "/rest/api/2/issueLink", body, nil); err != nil {
			return err
		}
	}
	return nil
}

// ListReqToReqLinks returns Requirement->Requirement "requires" links for the
// synced requirements. Demo returns generated links (each requirement "requires"
// the next one in key order, forming a chain). The live path is a TODO --
// fetching issuelinks per requirement would require per-issue GET calls; defer
// to a future sync pass that reads issuelinks from the requirement search.
//
// NOTE(xtm): on a live instance, harvest req->req links from the issuelinks
// field returned by searchRequirements (already fetched per requirement) and
// filter for link types that match the "requires"/"depends on" family, checking
// that both endpoints are known requirement keys.
//
// TODO(xtm): implement the live harvest; for now, live returns empty.
func (c *Client) ListReqToReqLinks(ctx context.Context, reqKeys []string) ([]ReqToReqLink, error) {
	if isDemoURL(c.baseURL) {
		return demoReqToReqLinks(reqKeys), nil
	}
	return []ReqToReqLink{}, nil
}

// demoReqToReqLinks generates a small set of Requirement->Requirement "requires"
// links for the demo: each requirement "requires" the next one in the list,
// wrapping around. Produces exactly min(len(reqKeys), 8) links so the feature
// is visible without overwhelming the demo UI.
func demoReqToReqLinks(reqKeys []string) []ReqToReqLink {
	if len(reqKeys) < 2 {
		return nil
	}
	limit := len(reqKeys)
	if limit > 8 {
		limit = 8
	}
	out := make([]ReqToReqLink, 0, limit)
	for i := 0; i < limit; i++ {
		next := (i + 1) % len(reqKeys)
		out = append(out, ReqToReqLink{
			FromKey:  reqKeys[i],
			ToKey:    reqKeys[next],
			LinkType: "requires",
			LinkID:   fmt.Sprintf("rrl-%d", i+1),
		})
	}
	return out
}
