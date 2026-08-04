package jira

import (
	"context"
	"strings"
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

	// Browse (empty query) lists all in the source project, paged.
	got, total, err := c.SearchTestsAcrossProjects(ctx, []string{"PROJVAL"}, "", 0, 50)
	if err != nil {
		t.Fatalf("browse: %v", err)
	}
	if total == 0 || len(got) == 0 {
		t.Fatalf("browse should list source tests, got %d (total %d)", len(got), total)
	}
	for _, tb := range got {
		if tb.ProjectKey != "PROJVAL" {
			t.Errorf("result %s projectKey = %q, want PROJVAL", tb.Key, tb.ProjectKey)
		}
	}

	// A query narrows the browse list.
	narrowed, nTotal, _ := c.SearchTestsAcrossProjects(ctx, []string{"PROJVAL"}, "login", 0, 50)
	if nTotal == 0 || nTotal >= total {
		t.Errorf("query should narrow results: narrowed total %d vs all %d", nTotal, total)
	}
	for _, tb := range narrowed {
		if !strings.Contains(strings.ToLower(tb.Summary), "login") {
			t.Errorf("narrowed result %q does not match query", tb.Summary)
		}
	}

	// Paging: offset past the total yields no rows but keeps the total.
	if rows, tot, _ := c.SearchTestsAcrossProjects(ctx, []string{"PROJVAL"}, "", 1000, 50); len(rows) != 0 || tot != total {
		t.Errorf("offset past end: rows=%d total=%d (want 0 rows, total %d)", len(rows), tot, total)
	}

	// No configured source projects → no results.
	if rows, tot, _ := c.SearchTestsAcrossProjects(ctx, nil, "login", 0, 50); len(rows) != 0 || tot != 0 {
		t.Errorf("no source projects should yield no results, got %d (total %d)", len(rows), tot)
	}
}

func TestSearchPreconditionsAcrossProjectsDemo(t *testing.T) {
	c := NewClient("demo", "tok")
	ctx := context.Background()

	got, total, err := c.SearchPreconditionsAcrossProjects(ctx, []string{"PROJVAL"}, "", 0, 50)
	if err != nil {
		t.Fatalf("browse: %v", err)
	}
	if total == 0 || len(got) == 0 {
		t.Fatalf("browse should list source preconditions, got %d (total %d)", len(got), total)
	}
	for _, p := range got {
		if p.Key == "" || p.Summary == "" {
			t.Errorf("precondition missing key/summary: %+v", p)
		}
	}
	if rows, tot, _ := c.SearchPreconditionsAcrossProjects(ctx, nil, "logged in", 0, 50); len(rows) != 0 || tot != 0 {
		t.Errorf("no source projects should yield no results, got %d (total %d)", len(rows), tot)
	}
}
