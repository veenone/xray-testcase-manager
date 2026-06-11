package jira

import (
	"context"
	"fmt"
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
// Demo URLs generate a deterministic cross-project set so the coverage view has
// data; the real call short-circuits to empty until verified on a live
// instance.
//
// TODO(xtm): real path — (1) for each configured source, JQL-search
// `project = X AND issuetype in (...) AND (scope) ORDER BY key`; (2) resolve the
// coverage issue-link type once (GET /rest/api/2/issueLinkType, default
// "Tests"/"is tested by"); (3) read each synced Test's `issuelinks` field to
// collect linked requirement keys (any project) and batch-fetch those issues by
// key via /rest/api/2/search?jql=key in (...). Verify the link direction and
// response shapes on a live Xray Server 8.4.0 instance.
func (c *Client) ListRequirements(ctx context.Context, profileProjectKey string, sources []RequirementSourceSpec, onProgress func(done, total int)) ([]Requirement, []RequirementLink, error) {
	_ = ctx
	if isDemoURL(c.baseURL) {
		reqs, links := demoRequirements(profileProjectKey)
		if onProgress != nil {
			onProgress(len(reqs), len(reqs))
		}
		return reqs, links, nil
	}
	_ = sources
	return []Requirement{}, []RequirementLink{}, nil
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
