package jira

import (
	"context"
	"testing"
)

func TestCrossProjectClause(t *testing.T) {
	if got := crossProjectClause("DEMO-123"); got != `key = "DEMO-123"` {
		t.Errorf("key query = %q, want key match", got)
	}
	if got := crossProjectClause("login flow"); got != `summary ~ "login flow"` {
		t.Errorf("text query = %q, want summary match", got)
	}
	// A quote in the term must be escaped so the JQL string stays valid.
	if got := crossProjectClause(`say "hi"`); got != `summary ~ "say \"hi\""` {
		t.Errorf("escaped query = %q", got)
	}
}

func TestExcludeProjectClause(t *testing.T) {
	if got := excludeProjectClause("DEMO"); got != ` AND project != "DEMO"` {
		t.Errorf("clause = %q", got)
	}
	if got := excludeProjectClause("  "); got != "" {
		t.Errorf("blank project should yield no clause, got %q", got)
	}
}

func TestSearchTestsAcrossProjectsDemo(t *testing.T) {
	c := NewClient("demo", "tok")
	ctx := context.Background()

	got, err := c.SearchTestsAcrossProjects(ctx, "DEMO", "login", 50)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 cross-project matches, got %d", len(got))
	}
	for _, tb := range got {
		if tb.ProjectKey != demoForeignProject {
			t.Errorf("result %s projectKey = %q, want %q", tb.Key, tb.ProjectKey, demoForeignProject)
		}
		if tb.ProjectKey == "DEMO" {
			t.Errorf("cross-project search must exclude the active project")
		}
	}

	// Empty query → no results.
	if got, _ := c.SearchTestsAcrossProjects(ctx, "DEMO", "  ", 50); len(got) != 0 {
		t.Errorf("empty query should yield no results, got %d", len(got))
	}
	// Excluding the foreign project itself → no results.
	if got, _ := c.SearchTestsAcrossProjects(ctx, demoForeignProject, "login", 50); len(got) != 0 {
		t.Errorf("excluding the foreign project should yield no results, got %d", len(got))
	}
}

func TestSearchPreconditionsAcrossProjectsDemo(t *testing.T) {
	c := NewClient("demo", "tok")
	ctx := context.Background()

	got, err := c.SearchPreconditionsAcrossProjects(ctx, "DEMO", "logged in", 50)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 cross-project preconditions, got %d", len(got))
	}
	for _, p := range got {
		if p.Key == "" || p.Summary == "" {
			t.Errorf("precondition missing key/summary: %+v", p)
		}
	}
	if got, _ := c.SearchPreconditionsAcrossProjects(ctx, "DEMO", "", 50); len(got) != 0 {
		t.Errorf("empty query should yield no results, got %d", len(got))
	}
}
