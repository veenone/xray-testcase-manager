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
	Key        string
	ProjectKey string
	IssueType  string
	Summary    string
	Status     string
	Updated    string
}

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
		reqs, links := demoRequirements(profileProjectKey)
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
		q.Set("fields", "summary,status,issuetype,project,issuelinks")

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
// to delete. Demo URLs short-circuit to a no-op.
//
// TODO(xtm): real path — resolve the coverage issue-link type once
// (GET /rest/api/2/issueLinkType, default "Tests"/"is tested by"); for each add,
// POST /rest/api/2/issueLink {type, inwardIssue:test, outwardIssue:requirement}
// (verify direction); for each removeLinkID, DELETE /rest/api/2/issueLink/{id}.
// Verify on a live Xray Server 8.4.0 instance.
func (c *Client) UpdateTestRequirements(ctx context.Context, testKey string, add []string, removeLinkIDs []string) error {
	_ = ctx
	if isDemoURL(c.baseURL) {
		return nil
	}
	_ = testKey
	_ = add
	_ = removeLinkIDs
	return nil
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

// demoRequirements generates ~two dozen requirement issues in a different
// project, linked to the run-status-bearing demo Tests, with a couple left
// uncovered — so coverage spans PASSED / FAILED / NOTRUN / UNCOVERED.
func demoRequirements(testProjectKey string) ([]Requirement, []RequirementLink) {
	if testProjectKey == "" {
		testProjectKey = "DEMO"
	}
	const count = 24
	reqs := make([]Requirement, 0, count)
	links := make([]RequirementLink, 0)
	for r := 1; r <= count; r++ {
		itype := "Story"
		if r%5 == 0 {
			itype = "Epic"
		}
		reqKey := fmt.Sprintf("%s-%d", demoRequirementProject, r)
		reqs = append(reqs, Requirement{
			Key:        reqKey,
			ProjectKey: demoRequirementProject,
			IssueType:  itype,
			Summary:    demoFeatures[(r-1)%len(demoFeatures)] + " requirement",
			Status:     demoReqStatuses[r%len(demoReqStatuses)],
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
