package kiwi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newBugKiwi serves an instance with one execution carrying two hyperlinks,
// one of which points at a Jira issue.
func newBugKiwi(t *testing.T, calls *[]string) *Adapter {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
			ID     int               `json:"id"`
		}
		_ = json.Unmarshal(body, &req)
		if calls != nil {
			*calls = append(*calls, req.Method+" "+string(body))
		}
		w.Header().Set("Content-Type", "application/json")

		switch req.Method {
		case "Auth.login":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":"sid"}`, req.ID)
		case "TestExecution.filter":
			// The execution id (77) is deliberately NOT the run id (5) and NOT
			// the case id (1). A Kiwi TestExecution is the (run, case) pair and
			// has its own pk, so a test that passes a run id where an execution
			// id is wanted must not accidentally pass.
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":[{"id":77,"run":5,"case":1,"case__summary":"Test 1"}]}`, req.ID)
		case "TestExecution.get_links":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":[`+
				`{"id":1,"name":"DEF-42","url":"https://jira.example.com/browse/DEF-42"},`+
				`{"id":2,"name":"notes","url":"https://wiki.example.com/page"}]}`, req.ID)
		case "TestExecution.add_link":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"id":3}}`, req.ID)
		default:
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":[]}`, req.ID)
		}
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "u:p")
}

// TestCreateBugLinkSendsAUrl pins the field shape a live instance demanded:
// add_link answered [('url', ['This field is required.'])] without it.
func TestCreateBugLinkSendsAUrl(t *testing.T) {
	var calls []string
	a := newBugKiwi(t, &calls)
	a.SetBugBrowseBase("https://jira.example.com/browse/")

	if err := a.CreateBugLink(context.Background(), "77", "DEF-42"); err != nil {
		t.Fatalf("create bug link: %v", err)
	}

	var addCall string
	for _, c := range calls {
		if strings.HasPrefix(c, "TestExecution.add_link ") {
			addCall = c
		}
	}
	if addCall == "" {
		t.Fatal("add_link was never called")
	}
	if !strings.Contains(addCall, `"url"`) {
		t.Errorf("add_link payload has no url field: %s", addCall)
	}
	if !strings.Contains(addCall, "https://jira.example.com/browse/DEF-42") {
		t.Errorf("add_link payload does not carry the bug's browse URL: %s", addCall)
	}
	if !strings.Contains(addCall, "DEF-42") {
		t.Errorf("add_link payload does not name the bug: %s", addCall)
	}
}

// TestCreateBugLinkNeedsAnExecution names the caller error rather than sending
// a request that cannot work: Kiwi hyperlinks attach to an execution.
func TestCreateBugLinkNeedsAnExecution(t *testing.T) {
	a := newBugKiwi(t, nil)
	if err := a.CreateBugLink(context.Background(), "", "DEF-42"); err == nil {
		t.Fatal("want an error when no execution is given")
	}
}

// TestCreateRunBugLinkTargetsTheResolvedExecution is the id-space guard. The
// commit path holds a KindTestExec container key, which in Kiwi is a TestRun
// id (5 here), and the test case id (1). add_link must land on the EXECUTION
// joining them (77) — sending the run id would either fail or hyperlink an
// unrelated execution whose pk happened to be 5.
func TestCreateRunBugLinkTargetsTheResolvedExecution(t *testing.T) {
	var calls []string
	a := newBugKiwi(t, &calls)
	a.SetBugBrowseBase("https://jira.example.com/browse/")

	if err := a.CreateRunBugLink(context.Background(), "5", "1", "DEF-42"); err != nil {
		t.Fatalf("create run bug link: %v", err)
	}

	var addCall string
	for _, c := range calls {
		if strings.HasPrefix(c, "TestExecution.add_link ") {
			addCall = c
		}
	}
	if addCall == "" {
		t.Fatal("add_link was never called")
	}
	if !strings.Contains(addCall, `"execution_id":77`) {
		t.Errorf("add_link did not target the resolved execution 77: %s", addCall)
	}
	if strings.Contains(addCall, `"execution_id":5`) || strings.Contains(addCall, `"execution_id":1`) {
		t.Errorf("add_link targeted the run or the case instead of the execution: %s", addCall)
	}
	if !strings.Contains(addCall, "https://jira.example.com/browse/DEF-42") {
		t.Errorf("add_link payload does not carry the bug's browse URL: %s", addCall)
	}
}

// TestCreateRunBugLinkNeedsBothKeys: a run-scoped link cannot be resolved from
// half a pair, and each half is named rather than sent as a request that
// cannot work.
func TestCreateRunBugLinkNeedsBothKeys(t *testing.T) {
	a := newBugKiwi(t, nil)
	a.SetBugBrowseBase("https://jira.example.com/browse/")
	if err := a.CreateRunBugLink(context.Background(), "", "1", "DEF-42"); err == nil {
		t.Error("want an error when no execution is given")
	}
	if err := a.CreateRunBugLink(context.Background(), "5", "", "DEF-42"); err == nil {
		t.Error("want an error when no test is given")
	}
}

// TestListBugsReadsExecutionHyperlinks is the read half. Kiwi holds only the
// link; the issue's summary and status live in Jira, so the Bug records come
// back carrying just a key for the caller to hydrate.
func TestListBugsReadsExecutionHyperlinks(t *testing.T) {
	a := newBugKiwi(t, nil)
	a.SetBugBrowseBase("https://jira.example.com/browse/")

	bugs, links, err := a.ListBugs(context.Background(), "SNMP", []string{"1"}, "Bug", nil)
	if err != nil {
		t.Fatalf("list bugs: %v", err)
	}
	if len(bugs) != 1 || bugs[0].Key != "DEF-42" {
		t.Fatalf("got bugs %+v, want exactly the Jira link DEF-42 (the wiki link is not a bug)", bugs)
	}
	if len(links) != 1 || links[0].BugKey != "DEF-42" || links[0].TestKey != "1" {
		t.Fatalf("got links %+v, want test 1 linked to DEF-42", links)
	}
}

// TestListBugsWithoutABrowseBaseReturnsNothing keeps an unconfigured profile
// quiet: with no Jira base URL there is no way to tell a bug link from any
// other hyperlink, and guessing would fill the Bugs view with wiki pages.
func TestListBugsWithoutABrowseBaseReturnsNothing(t *testing.T) {
	a := newBugKiwi(t, nil)

	bugs, links, err := a.ListBugs(context.Background(), "SNMP", []string{"1"}, "Bug", nil)
	if err != nil {
		t.Fatalf("list bugs: %v", err)
	}
	if len(bugs) != 0 || len(links) != 0 {
		t.Fatalf("got %d bugs and %d links, want none without a browse base", len(bugs), len(links))
	}
}

// TestBugKeyFromURLStripsQueryAndFragment pins a shape a browser actually
// produces: a pasted Jira link often carries a query string (from a saved
// filter) or a fragment (a deep link to a comment). Neither belongs in the
// issue key — a corrupted key would silently fail to match anything when
// Task 8 hydrates it from Jira.
func TestBugKeyFromURLStripsQueryAndFragment(t *testing.T) {
	a := New("http://example.invalid", "alice:secret")
	a.SetBugBrowseBase("https://jira.example.com/browse/")

	cases := []struct {
		name string
		url  string
	}{
		{"query string", "https://jira.example.com/browse/DEF-42?jql=project%3DDEF"},
		{"fragment", "https://jira.example.com/browse/DEF-42#comment-1"},
		{"query and fragment", "https://jira.example.com/browse/DEF-42?jql=project%3DDEF#comment-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := a.bugKeyFromURL(tc.url); got != "DEF-42" {
				t.Errorf("bugKeyFromURL(%q) = %q, want %q", tc.url, got, "DEF-42")
			}
		})
	}
}
