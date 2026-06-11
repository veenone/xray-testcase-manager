package jira

import (
	"context"
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

func TestRealRequirementsEmptyUntilWired(t *testing.T) {
	c := NewClient("https://jira.example.com", "t")
	reqs, links, err := c.ListRequirements(context.Background(), "QA", nil, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(reqs) != 0 || len(links) != 0 {
		t.Errorf("real path should be empty until wired, got %d / %d", len(reqs), len(links))
	}
}
