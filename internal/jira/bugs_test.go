package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestDemoBugsLinkOnlyToFailedTests(t *testing.T) {
	failed := map[int]bool{}
	for _, n := range demoFailedTestNums(10) {
		failed[n] = true
	}
	if len(failed) < 3 {
		t.Fatalf("demoFailedTestNums returned %d, want >= 3", len(failed))
	}

	_, links := demoBugs("DEMO", nil)
	for _, l := range links {
		num, ok := testNumOf(l.TestKey, "DEMO")
		if !ok {
			t.Fatalf("unexpected linked test key %q", l.TestKey)
		}
		if !failed[num] {
			t.Errorf("bug linked to DEMO-%d, which is not a FAILED demo test", num)
		}
	}
}

func TestDemoBugsAreCrossProjectAndVaried(t *testing.T) {
	bugs, links := demoBugs("DEMO", nil)
	if len(bugs) < 10 {
		t.Fatalf("demoBugs produced %d bugs, want >= 10", len(bugs))
	}

	projects := map[string]int{}
	for _, b := range bugs {
		if b.ProjectKey == "DEMO" {
			t.Errorf("bug %s is in the test project DEMO; defects should be cross-project", b.Key)
		}
		projects[b.ProjectKey]++
	}
	if len(projects) < 2 {
		t.Errorf("bugs span %d projects, want >= 2 for cross-project demo", len(projects))
	}

	bugsPerTest := map[string]int{}
	testsPerBug := map[string]int{}
	for _, l := range links {
		bugsPerTest[l.TestKey]++
		testsPerBug[l.BugKey]++
	}
	multiBugTest, multiTestBug := false, false
	for _, n := range bugsPerTest {
		if n >= 2 {
			multiBugTest = true
		}
	}
	for _, n := range testsPerBug {
		if n >= 2 {
			multiTestBug = true
		}
	}
	if !multiBugTest {
		t.Error("expected at least one test linked to two bugs")
	}
	if !multiTestBug {
		t.Error("expected at least one bug linked to two tests")
	}
}

// TestCreateBugLinkPostsIssueLink exercises the live-Jira path: the link type is
// resolved from /issueLinkType, then a link is POSTed with the Test as the
// outward issue and the Bug as the inward issue.
func TestCreateBugLinkPostsIssueLink(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/issueLinkType":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issueLinkTypes": []map[string]any{
					{"id": "1", "name": "Blocks"},
					{"id": "2", "name": "Relates"},
				},
			})
		case "/rest/api/2/issueLink":
			gotPath = r.URL.Path
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &gotBody)
			w.WriteHeader(http.StatusCreated)
		default:
			t.Errorf("unexpected request to %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	if err := newTestClient(srv).CreateBugLink(context.Background(), "QA-1", "BUGS-100"); err != nil {
		t.Fatalf("CreateBugLink: %v", err)
	}
	if gotPath != "/rest/api/2/issueLink" {
		t.Fatalf("issue link not POSTed (path=%q)", gotPath)
	}
	if typ, _ := gotBody["type"].(map[string]any); typ["name"] != "Relates" {
		t.Errorf("link type = %v, want Relates", typ["name"])
	}
	if inward, _ := gotBody["inwardIssue"].(map[string]any); inward["key"] != "BUGS-100" {
		t.Errorf("inwardIssue.key = %v, want BUGS-100 (the bug)", inward["key"])
	}
	if outward, _ := gotBody["outwardIssue"].(map[string]any); outward["key"] != "QA-1" {
		t.Errorf("outwardIssue.key = %v, want QA-1 (the test)", outward["key"])
	}
}

// TestResolveBugLinkTypePrefersDefect verifies a defect-oriented link type wins
// over the universal "Relates" when the instance defines one.
func TestResolveBugLinkTypePrefersDefect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issueLinkTypes": []map[string]any{
				{"name": "Relates"},
				{"name": "Defect"},
			},
		})
	}))
	defer srv.Close()

	name, err := newTestClient(srv).resolveBugLinkType(context.Background())
	if err != nil {
		t.Fatalf("resolveBugLinkType: %v", err)
	}
	if name != "Defect" {
		t.Errorf("link type = %q, want Defect (preferred over Relates)", name)
	}
}

// testNumOf parses "<project>-<n>" and returns n when the project prefix matches.
func testNumOf(key, project string) (int, bool) {
	suffix, ok := strings.CutPrefix(key, project+"-")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(suffix)
	if err != nil {
		return 0, false
	}
	return n, true
}
