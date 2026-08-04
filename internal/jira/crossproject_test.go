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

func TestInProjectClause(t *testing.T) {
	if got := inProjectClause([]string{"PROJVAL", "JKTEE"}); got != `project in ("PROJVAL", "JKTEE")` {
		t.Errorf("clause = %q", got)
	}
	if got := inProjectClause([]string{"  ", ""}); got != "" {
		t.Errorf("blank projects should yield no clause, got %q", got)
	}
}

func TestSearchTestsAcrossProjectsDemo(t *testing.T) {
	c := NewClient("demo", "tok")
	ctx := context.Background()

	got, err := c.SearchTestsAcrossProjects(ctx, []string{"PROJVAL"}, "login", 50)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 cross-project matches, got %d", len(got))
	}
	for _, tb := range got {
		if tb.ProjectKey != "PROJVAL" {
			t.Errorf("result %s projectKey = %q, want PROJVAL", tb.Key, tb.ProjectKey)
		}
	}

	// Empty query → no results.
	if got, _ := c.SearchTestsAcrossProjects(ctx, []string{"PROJVAL"}, "  ", 50); len(got) != 0 {
		t.Errorf("empty query should yield no results, got %d", len(got))
	}
	// No configured source projects → no results.
	if got, _ := c.SearchTestsAcrossProjects(ctx, nil, "login", 50); len(got) != 0 {
		t.Errorf("no source projects should yield no results, got %d", len(got))
	}
}

func TestSearchPreconditionsAcrossProjectsDemo(t *testing.T) {
	c := NewClient("demo", "tok")
	ctx := context.Background()

	got, err := c.SearchPreconditionsAcrossProjects(ctx, []string{"PROJVAL"}, "logged in", 50)
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
	if got, _ := c.SearchPreconditionsAcrossProjects(ctx, []string{"PROJVAL"}, "", 50); len(got) != 0 {
		t.Errorf("empty query should yield no results, got %d", len(got))
	}
	if got, _ := c.SearchPreconditionsAcrossProjects(ctx, nil, "logged in", 50); len(got) != 0 {
		t.Errorf("no source projects should yield no results, got %d", len(got))
	}
}
