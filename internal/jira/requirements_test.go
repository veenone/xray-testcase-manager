package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDemoRequirementsAreCrossProjectAndLinked(t *testing.T) {
	c := NewClient("demo", "t")
	reqs, links, err := c.ListRequirements(context.Background(), "DEMO", nil, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(reqs) == 0 || len(links) == 0 {
		t.Fatalf("want requirements and links, got %d / %d", len(reqs), len(links))
	}

	// Requirements live in a different project than the Tests.
	for _, rq := range reqs {
		if rq.ProjectKey == "DEMO" {
			t.Errorf("requirement %s is in the Test project, expected a different project", rq.Key)
		}
		if !strings.HasPrefix(rq.Key, rq.ProjectKey+"-") {
			t.Errorf("requirement key %q not in its project %q", rq.Key, rq.ProjectKey)
		}
	}
	// Links point Test (DEMO project) -> Requirement (other project).
	for _, l := range links {
		if !strings.HasPrefix(l.TestKey, "DEMO-") {
			t.Errorf("link test key %q not in the Test project", l.TestKey)
		}
		if strings.HasPrefix(l.RequirementKey, "DEMO-") {
			t.Errorf("link requirement key %q should be cross-project", l.RequirementKey)
		}
	}
}

// TestRealRequirementsEmptyWithNoSources confirms the live path returns nothing
// when no requirement sources are configured (there is nothing to search).
func TestRealRequirementsEmptyWithNoSources(t *testing.T) {
	c := NewClient("https://jira.example.com", "t")
	reqs, links, err := c.ListRequirements(context.Background(), "QA", nil, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(reqs) != 0 || len(links) != 0 {
		t.Errorf("no sources should yield nothing, got %d / %d", len(reqs), len(links))
	}
}

// TestRealRequirementsSearchAndLinks exercises the live path: a configured source
// is JQL-searched, requirements are parsed, and coverage links are harvested from
// each requirement's issuelinks — only for linked issues in the Test project.
func TestRealRequirementsSearchAndLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/2/search" {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		jql := r.URL.Query().Get("jql")
		if !strings.Contains(jql, `project = "PRD"`) ||
			!strings.Contains(jql, `issuetype in ("Story", "Epic")`) {
			t.Errorf("unexpected jql: %q", jql)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total": 1,
			"issues": []map[string]any{
				{
					"key": "PRD-7",
					"fields": map[string]any{
						"summary":   "Login requirement",
						"status":    map[string]any{"name": "Approved"},
						"issuetype": map[string]any{"name": "Story"},
						"project":   map[string]any{"key": "PRD"},
						"issuelinks": []map[string]any{
							// A covering Test (in the QA test project) — kept.
							{"id": "5001", "type": map[string]any{"name": "Tests"},
								"inwardIssue": map[string]any{"key": "QA-42"}},
							// A link to another requirement — ignored.
							{"id": "5002", "type": map[string]any{"name": "relates to"},
								"outwardIssue": map[string]any{"key": "PRD-9"}},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	src := []RequirementSourceSpec{{ProjectKey: "PRD", IssueTypes: []string{"Story", "Epic"}}}
	reqs, links, err := newTestClient(srv).ListRequirements(context.Background(), "QA", src, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(reqs) != 1 || reqs[0].Key != "PRD-7" || reqs[0].ProjectKey != "PRD" ||
		reqs[0].IssueType != "Story" || reqs[0].Status != "Approved" {
		t.Fatalf("requirement parsed wrong: %+v", reqs)
	}
	if len(links) != 1 || links[0].TestKey != "QA-42" ||
		links[0].RequirementKey != "PRD-7" || links[0].LinkID != "5001" {
		t.Fatalf("coverage link wrong (should keep only the Test-project link): %+v", links)
	}
}
