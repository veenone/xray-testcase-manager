package jira

import (
	"context"
	"encoding/json"
	"fmt"
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

// TestListBugsByKeysChunksTheJQL guards the URL-length limit. A JQL
// "key in (...)" with a few thousand keys exceeds what Jira accepts on a GET,
// so the fetch is chunked; the caller must still see one flat result.
func TestListBugsByKeysChunksTheJQL(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query().Get("jql"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"startAt":0,"maxResults":50,"total":1,"issues":[
			{"key":"DEF-1","fields":{"project":{"key":"DEF"},"issuetype":{"name":"Bug"},
			 "summary":"Boom","status":{"name":"Open"},"priority":{"name":"High"},
			 "updated":"2026-09-01T10:00:00.000+0000"}}]}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	keys := make([]string, 0, 120)
	for i := 0; i < 120; i++ {
		keys = append(keys, fmt.Sprintf("DEF-%d", i+1))
	}

	bugs, err := c.ListBugsByKeys(context.Background(), keys)
	if err != nil {
		t.Fatalf("list by keys: %v", err)
	}
	if len(queries) < 2 {
		t.Errorf("issued %d searches for 120 keys, want the request chunked", len(queries))
	}
	if len(bugs) == 0 {
		t.Fatal("no bugs returned")
	}
	for _, q := range queries {
		if !strings.Contains(q, "key in (") {
			t.Errorf("query is not a key lookup: %q", q)
		}
	}
}

// TestListBugsByKeysWithNoKeysMakesNoRequest keeps an unconfigured or empty
// profile from issuing a meaningless "key in ()" search.
func TestListBugsByKeysWithNoKeysMakesNoRequest(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	bugs, err := NewClient(srv.URL, "tok").ListBugsByKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("list by keys: %v", err)
	}
	if len(bugs) != 0 {
		t.Errorf("got %d bugs, want none", len(bugs))
	}
	if called {
		t.Error("an HTTP request was made for an empty key list")
	}
}

// TestListBugsByKeysSkipsMalformedKeys guards against JQL injection through an
// untrusted hyperlink-derived key. Kiwi's bugKeyFromURL only rejects an empty
// key or one containing "/", so quotes, backslashes, and JQL syntax reach this
// function unvalidated; they must be filtered here rather than escaped and
// trusted. When every key given is malformed, no HTTP request is made at all.
func TestListBugsByKeysSkipsMalformedKeys(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	keys := []string{
		`DEF-1" OR key != "`, // embedded quote carrying JQL syntax
		`DEF-2\`,             // trailing backslash
		`DEF-3) OR (1=1`,     // parens / boolean injection attempt
	}
	bugs, err := NewClient(srv.URL, "tok").ListBugsByKeys(context.Background(), keys)
	if err != nil {
		t.Fatalf("list by keys: %v", err)
	}
	if len(bugs) != 0 {
		t.Errorf("got %d bugs, want none", len(bugs))
	}
	if called {
		t.Error("an HTTP request was made for an all-malformed key list")
	}
}

// TestListBugsByKeysQueriesOnlyValidKeys checks that a mix of well-formed and
// malformed keys queries only the well-formed ones: the malformed keys are
// dropped before the JQL is built, not escaped into it.
func TestListBugsByKeysQueriesOnlyValidKeys(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query().Get("jql"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"startAt":0,"maxResults":50,"total":1,"issues":[
			{"key":"DEF-1","fields":{"project":{"key":"DEF"},"issuetype":{"name":"Bug"},
			 "summary":"Boom","status":{"name":"Open"},"priority":{"name":"High"},
			 "updated":"2026-09-01T10:00:00.000+0000"}}]}`)
	}))
	defer srv.Close()

	keys := []string{
		"DEF-1",
		`DEF-2" OR key != "`,
		`DEF-3\`,
		"DEF-4",
	}
	bugs, err := NewClient(srv.URL, "tok").ListBugsByKeys(context.Background(), keys)
	if err != nil {
		t.Fatalf("list by keys: %v", err)
	}
	if len(bugs) == 0 {
		t.Fatal("no bugs returned")
	}
	if len(queries) != 1 {
		t.Fatalf("issued %d searches, want exactly 1 for 2 valid keys", len(queries))
	}
	q := queries[0]
	if !strings.Contains(q, `"DEF-1"`) || !strings.Contains(q, `"DEF-4"`) {
		t.Errorf("query missing a valid key: %q", q)
	}
	if strings.Contains(q, "DEF-2") || strings.Contains(q, "DEF-3") {
		t.Errorf("query contains a malformed key that should have been skipped: %q", q)
	}
}

// TestListBugsByKeysDemoFiltersToRequestedKeys verifies a demo profile answers
// ListBugsByKeys from the local demo bug seed, filtered to the keys asked
// for, instead of attempting a real HTTP call against the "demo" host. A demo
// profile can be configured as a workspace's secondary bug backend just like
// a real one (SaveBugConnection places no restriction on the connection URL),
// so this must behave the same way ListProjectBugs and ListBugs already do in
// demo mode.
func TestListBugsByKeysDemoFiltersToRequestedKeys(t *testing.T) {
	c := NewClient("demo", "")
	ctx := context.Background()

	all, err := c.ListProjectBugs(ctx, "BUGS", "Bug")
	if err != nil {
		t.Fatalf("ListProjectBugs: %v", err)
	}
	if len(all) < 2 {
		t.Fatal("expected at least 2 demo bugs to test with")
	}
	want := []string{all[0].Key, all[1].Key, "NOPE-999"} // well-formed but not in the demo seed

	bugs, err := c.ListBugsByKeys(ctx, want)
	if err != nil {
		t.Fatalf("ListBugsByKeys: %v", err)
	}
	if len(bugs) != 2 {
		t.Fatalf("got %d bugs, want exactly the 2 in-seed keys", len(bugs))
	}
	got := map[string]bool{}
	for _, b := range bugs {
		got[b.Key] = true
	}
	if !got[all[0].Key] || !got[all[1].Key] {
		t.Errorf("missing a requested demo bug: got %v, want %s and %s", got, all[0].Key, all[1].Key)
	}
}
