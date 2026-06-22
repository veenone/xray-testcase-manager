package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestParseTestBasics exercises the pure mapping from search issues to TestBasic
// rows. It covers a cross-project member Test carrying two issuelinks (one to a
// Bug, one to a Story), asserting the Test-level basics (summary, status,
// project) and that each issuelink carries key + issuetype + link id plus the
// linked issue's basics, and that ProjectKey falls back to the key prefix when
// the project object is absent.
func TestParseTestBasics(t *testing.T) {
	linked := func(key, summary, status, priority, itype string) *testBasicLinkedIssue {
		f := testBasicLinkedFields{Summary: summary}
		if status != "" {
			f.Status = &nameOnly{Name: status}
		}
		if priority != "" {
			f.Priority = &nameOnly{Name: priority}
		}
		if itype != "" {
			f.IssueType = &nameOnly{Name: itype}
		}
		return &testBasicLinkedIssue{Key: key, Fields: f}
	}

	issues := []testBasicIssue{
		{
			Key: "XRAYINT-1",
			Fields: testBasicFields{
				Summary: "login works",
				Status:  &nameOnly{Name: "Ready"},
				Project: &struct {
					Key string `json:"key"`
				}{Key: "XRAYINT"},
				IssueLinks: []testBasicIssueLink{
					// Link to a defect (outward).
					{ID: "5001", OutwardIssue: linked("BUGS-100", "crashes on submit", "Open", "High", "Bug")},
					// Link to a Story (inward) - still carried as a BugLinkRef; the
					// harvest filters by issuetype downstream.
					{ID: "5002", InwardIssue: linked("PRD-7", "a story", "In Progress", "Low", "Story")},
					// A nil link (no inward/outward) is skipped.
					{ID: "5003"},
				},
			},
		},
		{
			// No project object -> ProjectKey falls back to the key prefix.
			Key: "OTHER-9",
			Fields: testBasicFields{
				Summary: "no project object",
				Status:  &nameOnly{Name: "Done"},
			},
		},
	}

	got := parseTestBasics(issues)
	if len(got) != 2 {
		t.Fatalf("want 2 basics, got %d: %+v", len(got), got)
	}

	a := got[0]
	if a.Key != "XRAYINT-1" || a.Summary != "login works" || a.Status != "Ready" || a.ProjectKey != "XRAYINT" {
		t.Errorf("XRAYINT-1 basics wrong: %+v", a)
	}
	if len(a.IssueLinks) != 2 {
		t.Fatalf("want 2 issue links (nil one skipped), got %d: %+v", len(a.IssueLinks), a.IssueLinks)
	}
	byKey := map[string]BugLinkRef{}
	for _, l := range a.IssueLinks {
		byKey[l.Key] = l
	}
	bug, ok := byKey["BUGS-100"]
	if !ok {
		t.Fatalf("missing BUGS-100 link: %+v", a.IssueLinks)
	}
	if bug.IssueType != "Bug" || bug.LinkID != "5001" || bug.ProjectKey != "BUGS" ||
		bug.Summary != "crashes on submit" || bug.Status != "Open" || bug.Priority != "High" {
		t.Errorf("BUGS-100 link parsed wrong: %+v", bug)
	}
	story, ok := byKey["PRD-7"]
	if !ok {
		t.Fatalf("missing PRD-7 link: %+v", a.IssueLinks)
	}
	if story.IssueType != "Story" || story.LinkID != "5002" || story.ProjectKey != "PRD" {
		t.Errorf("PRD-7 link parsed wrong: %+v", story)
	}

	b := got[1]
	if b.ProjectKey != "OTHER" {
		t.Errorf("OTHER-9 ProjectKey should fall back to key prefix, got %q", b.ProjectKey)
	}
	if len(b.IssueLinks) != 0 {
		t.Errorf("OTHER-9 should have no links, got %+v", b.IssueLinks)
	}
}

// TestRealListTestsBasicSearchesAndMaps exercises the live ListTestsBasic path
// against a mock Jira: it issues a `key in (...)` search over the requested
// keys, requests fields=summary,status,project,issuelinks, and maps the returned
// issues (with their issuelinks) into TestBasic rows.
func TestRealListTestsBasicSearchesAndMaps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/2/search" {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		jql := r.URL.Query().Get("jql")
		if !strings.Contains(jql, `key in (`) ||
			!strings.Contains(jql, `"XRAYINT-1"`) || !strings.Contains(jql, `"XRAYINT-2"`) {
			t.Errorf("unexpected jql: %q", jql)
		}
		if f := r.URL.Query().Get("fields"); f != "summary,status,project,issuelinks" {
			t.Errorf("expected fields=summary,status,project,issuelinks, got %q", f)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total": 2,
			"issues": []map[string]any{
				{
					"key": "XRAYINT-1",
					"fields": map[string]any{
						"summary": "login works",
						"status":  map[string]any{"name": "Ready"},
						"project": map[string]any{"key": "XRAYINT"},
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
					"key": "XRAYINT-2",
					"fields": map[string]any{
						"summary":    "logout works",
						"status":     map[string]any{"name": "Draft"},
						"project":    map[string]any{"key": "XRAYINT"},
						"issuelinks": []map[string]any{},
					},
				},
			},
		})
	}))
	defer srv.Close()

	basics, err := newTestClient(srv).ListTestsBasic(
		context.Background(), []string{"XRAYINT-1", "XRAYINT-2"})
	if err != nil {
		t.Fatalf("ListTestsBasic: %v", err)
	}
	if len(basics) != 2 {
		t.Fatalf("want 2 basics, got %d: %+v", len(basics), basics)
	}
	byKey := map[string]TestBasic{}
	for _, b := range basics {
		byKey[b.Key] = b
	}
	one, ok := byKey["XRAYINT-1"]
	if !ok {
		t.Fatalf("missing XRAYINT-1: %+v", basics)
	}
	if one.Summary != "login works" || one.Status != "Ready" || one.ProjectKey != "XRAYINT" {
		t.Errorf("XRAYINT-1 basics wrong: %+v", one)
	}
	if len(one.IssueLinks) != 2 {
		t.Fatalf("XRAYINT-1 want 2 links, got %+v", one.IssueLinks)
	}
	var sawBug bool
	for _, l := range one.IssueLinks {
		if l.Key == "BUGS-100" {
			sawBug = true
			if l.IssueType != "Bug" || l.LinkID != "1" || l.ProjectKey != "BUGS" ||
				l.Summary != "boom" || l.Status != "Open" || l.Priority != "High" {
				t.Errorf("BUGS-100 link wrong: %+v", l)
			}
		}
	}
	if !sawBug {
		t.Errorf("expected a BUGS-100 link on XRAYINT-1, got %+v", one.IssueLinks)
	}
	if two := byKey["XRAYINT-2"]; len(two.IssueLinks) != 0 || two.Status != "Draft" {
		t.Errorf("XRAYINT-2 wrong: %+v", two)
	}
}
