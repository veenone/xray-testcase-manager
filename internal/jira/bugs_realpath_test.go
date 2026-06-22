package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestParseBugsFromIssueLinks exercises the pure harvest that turns a search
// page (Tests with issuelinks) into deduped Bugs plus one BugLink per
// (test, bug) pair. It covers: a Test linked to a defect (kept), a Test linked
// to a non-defect issue type like Story (ignored), a defect spanning two Tests
// (one Bug row, two BugLinks), and case-insensitive issuetype matching.
func TestParseBugsFromIssueLinks(t *testing.T) {
	field := func(summary, status, priority, itype string) bugLinkedFields {
		f := bugLinkedFields{Summary: summary}
		if status != "" {
			f.Status = &nameOnly{Name: status}
		}
		if priority != "" {
			f.Priority = &nameOnly{Name: priority}
		}
		if itype != "" {
			f.IssueType = &nameOnly{Name: itype}
		}
		return f
	}

	issues := []bugSearchIssue{
		{
			Key: "QA-1",
			Fields: bugSearchFields{
				IssueLinks: []bugIssueLink{
					// Defect linked outward (issuetype "bug", lowercase -> matches).
					{ID: "9001", OutwardIssue: &bugLinkedIssue{
						Key: "BUGS-100", Fields: field("crashes on submit", "Open", "High", "bug")}},
					// Non-defect (Story) -> ignored.
					{ID: "9002", InwardIssue: &bugLinkedIssue{
						Key: "PRD-5", Fields: field("a story", "Open", "Low", "Story")}},
				},
			},
		},
		{
			Key: "QA-2",
			Fields: bugSearchFields{
				IssueLinks: []bugIssueLink{
					// Same defect as QA-1 -> dedup the bug, add a second link.
					{ID: "9003", InwardIssue: &bugLinkedIssue{
						Key: "BUGS-100", Fields: field("crashes on submit", "Open", "High", "Bug")}},
					// A second, distinct defect on a different project.
					{ID: "9004", OutwardIssue: &bugLinkedIssue{
						Key: "SUP-7", Fields: field("times out", "In Progress", "Critical", "Bug")}},
				},
			},
		},
	}

	bugs, links := parseBugsFromIssueLinks(issues, "Bug")

	if len(bugs) != 2 {
		t.Fatalf("want 2 deduped bugs, got %d: %+v", len(bugs), bugs)
	}
	byKey := map[string]Bug{}
	for _, b := range bugs {
		byKey[b.Key] = b
	}
	b100, ok := byKey["BUGS-100"]
	if !ok {
		t.Fatalf("missing BUGS-100 in bugs: %+v", bugs)
	}
	if b100.Summary != "crashes on submit" || b100.Status != "Open" ||
		b100.Priority != "High" || b100.ProjectKey != "BUGS" || b100.IssueType != "bug" {
		t.Errorf("BUGS-100 parsed wrong: %+v", b100)
	}
	if sup, ok := byKey["SUP-7"]; !ok || sup.ProjectKey != "SUP" || sup.Priority != "Critical" {
		t.Errorf("SUP-7 parsed wrong: %+v", sup)
	}

	// One BugLink per (test, bug) pair: QA-1/BUGS-100, QA-2/BUGS-100, QA-2/SUP-7.
	if len(links) != 3 {
		t.Fatalf("want 3 links, got %d: %+v", len(links), links)
	}
	type pair struct{ test, bug, id string }
	got := map[pair]bool{}
	for _, l := range links {
		got[pair{l.TestKey, l.BugKey, l.LinkID}] = true
	}
	want := []pair{
		{"QA-1", "BUGS-100", "9001"},
		{"QA-2", "BUGS-100", "9003"},
		{"QA-2", "SUP-7", "9004"},
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing link %+v in %+v", w, links)
		}
	}
}

// TestRealListBugsSearchesAndHarvests exercises the live ListBugs path against a
// mock Jira: it issues a `key in (...)` search over the test keys, requests
// fields=issuelinks, and harvests defects from the returned issuelinks.
func TestRealListBugsSearchesAndHarvests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/2/search" {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		jql := r.URL.Query().Get("jql")
		if !strings.Contains(jql, `key in (`) ||
			!strings.Contains(jql, `"QA-1"`) || !strings.Contains(jql, `"QA-2"`) {
			t.Errorf("unexpected jql: %q", jql)
		}
		if f := r.URL.Query().Get("fields"); !strings.Contains(f, "issuelinks") {
			t.Errorf("expected fields=issuelinks, got %q", f)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total": 2,
			"issues": []map[string]any{
				{
					"key": "QA-1",
					"fields": map[string]any{
						"issuelinks": []map[string]any{
							{"id": "1", "type": map[string]any{"name": "Relates"},
								"outwardIssue": map[string]any{
									"key": "BUGS-100",
									"fields": map[string]any{
										"summary":   "boom",
										"status":    map[string]any{"name": "Open"},
										"priority":  map[string]any{"name": "High"},
										"issuetype": map[string]any{"name": "Bug"},
									}}},
							{"id": "2", "type": map[string]any{"name": "Relates"},
								"inwardIssue": map[string]any{
									"key": "PRD-9",
									"fields": map[string]any{
										"issuetype": map[string]any{"name": "Story"},
									}}},
						},
					},
				},
				{
					"key": "QA-2",
					"fields": map[string]any{
						"issuelinks": []map[string]any{
							{"id": "3", "type": map[string]any{"name": "Relates"},
								"inwardIssue": map[string]any{
									"key": "BUGS-100",
									"fields": map[string]any{
										"summary":   "boom",
										"status":    map[string]any{"name": "Open"},
										"priority":  map[string]any{"name": "High"},
										"issuetype": map[string]any{"name": "Bug"},
									}}},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	bugs, links, err := newTestClient(srv).ListBugs(
		context.Background(), "QA", []string{"QA-1", "QA-2"}, "Bug", nil)
	if err != nil {
		t.Fatalf("ListBugs: %v", err)
	}
	if len(bugs) != 1 || bugs[0].Key != "BUGS-100" || bugs[0].ProjectKey != "BUGS" {
		t.Fatalf("want one deduped BUGS-100, got %+v", bugs)
	}
	if len(links) != 2 {
		t.Fatalf("want two links (QA-1, QA-2 -> BUGS-100), got %+v", links)
	}
}
