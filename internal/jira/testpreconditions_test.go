package jira

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestListTestPreconditionsReadsTestSideAndEnriches covers the happy path of
// the per-test association read (RND_P_4TFINT_05-339): the plural test-side
// endpoint supplies keys and types, and one batched issue search fills in the
// summaries the endpoint omits.
func TestListTestPreconditionsReadsTestSideAndEnriches(t *testing.T) {
	var searchJQL string
	searches := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/raven/1.0/api/test/QA-1/preconditions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"id":1,"rank":1,"key":"PC-9","type":"Manual"},
				{"id":2,"rank":2,"key":"PC-4","type":"Generic"}
			]`))
		case r.URL.Path == "/rest/api/2/search":
			searches++
			searchJQL = r.URL.Query().Get("jql")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"issues":[
				{"key":"PC-4","fields":{"summary":"Card inserted","description":"d4"}},
				{"key":"PC-9","fields":{"summary":"HSM online","description":"d9"}}
			]}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	got, err := newTestClient(srv).ListTestPreconditions(context.Background(), "QA-1")
	if err != nil {
		t.Fatalf("ListTestPreconditions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 preconditions, got %d: %+v", len(got), got)
	}

	// Association order is preserved, not the search's response order.
	if got[0].Key != "PC-9" || got[1].Key != "PC-4" {
		t.Errorf("want association order PC-9, PC-4; got %s, %s", got[0].Key, got[1].Key)
	}
	if got[0].Summary != "HSM online" || got[0].Description != "d9" {
		t.Errorf("PC-9 not enriched: %+v", got[0])
	}
	// Type comes from the association endpoint, which the project-wide sync
	// cannot supply.
	if got[0].Type != "Manual" || got[1].Type != "Generic" {
		t.Errorf("types not carried through: %q, %q", got[0].Type, got[1].Type)
	}

	// Enrichment must be one batched search, not one request per precondition.
	if searches != 1 {
		t.Errorf("want 1 search call, got %d", searches)
	}
	for _, k := range []string{"PC-9", "PC-4"} {
		if !strings.Contains(searchJQL, k) {
			t.Errorf("search JQL %q missing %s", searchJQL, k)
		}
	}
}

// TestListTestPreconditionsTreats400AsEmpty pins the contract for a key that is
// not a Test: Xray answers 400, which means "nothing to link" rather than a
// failure that should surface to the user.
func TestListTestPreconditionsTreats400AsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Issue with key QA-404 not found or is not of type Test."))
	}))
	defer srv.Close()

	got, err := newTestClient(srv).ListTestPreconditions(context.Background(), "QA-404")
	if err != nil {
		t.Fatalf("want no error for a non-Test key, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want no preconditions, got %+v", got)
	}
}

// TestListTestPreconditionsPropagatesPermissionError checks that a 403 is a
// real error. A user who cannot browse the issue must be told, not shown an
// empty list that looks like "this test has no preconditions".
func TestListTestPreconditionsPropagatesPermissionError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("User does not have permission to browse or edit QA-1."))
	}))
	defer srv.Close()

	if _, err := newTestClient(srv).ListTestPreconditions(context.Background(), "QA-1"); err == nil {
		t.Fatal("want an error for 403, got nil")
	}
}

// TestListTestPreconditionsSurvivesEnrichmentFailure checks that losing the
// summaries does not lose the links. The association is the part that matters.
func TestListTestPreconditionsSurvivesEnrichmentFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/preconditions") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":1,"rank":1,"key":"PC-9","type":"Manual"}]`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	got, err := newTestClient(srv).ListTestPreconditions(context.Background(), "QA-1")
	if err != nil {
		t.Fatalf("ListTestPreconditions: %v", err)
	}
	if len(got) != 1 || got[0].Key != "PC-9" {
		t.Fatalf("want PC-9 kept despite enrichment failure, got %+v", got)
	}
	if got[0].Summary != "" {
		t.Errorf("want empty summary when enrichment failed, got %q", got[0].Summary)
	}
}

// TestListTestPreconditionsUsesPluralPath guards the discovery this fix rests
// on: the singular /precondition path 404s on Xray Server, so the request must
// go to the plural one.
func TestListTestPreconditionsUsesPluralPath(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/preconditions") {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_, _ = w.Write([]byte(`{"issues":[]}`))
	}))
	defer srv.Close()

	if _, err := newTestClient(srv).ListTestPreconditions(context.Background(), "QA-1"); err != nil {
		t.Fatalf("ListTestPreconditions: %v", err)
	}
	if len(paths) != 1 || paths[0] != "/rest/raven/1.0/api/test/QA-1/preconditions" {
		t.Fatalf("want a single call to the plural test-side path, got %v", paths)
	}
}

// TestListTestPreconditionsDemoMatchesSyncedLinks checks the demo answer for
// one Test agrees with the project-wide link map the demo sync uses. If it did
// not, pressing Refresh in demo mode would overwrite correct links with an
// empty set, since the caller persists whatever this returns.
func TestListTestPreconditionsDemoMatchesSyncedLinks(t *testing.T) {
	c := &Client{baseURL: "demo", token: "t", http: http.DefaultClient}

	_, links, err := demoPreconditionsAndLinks(themeFor("demo"), "DEMO")
	if err != nil {
		t.Fatalf("demoPreconditionsAndLinks: %v", err)
	}
	if len(links) == 0 {
		t.Fatal("demo generated no precondition links; the comparison below would be vacuous")
	}

	checked := 0
	for testKey, want := range links {
		if len(want) == 0 {
			continue
		}
		got, err := c.ListTestPreconditions(context.Background(), testKey)
		if err != nil {
			t.Fatalf("demo ListTestPreconditions(%s): %v", testKey, err)
		}
		gotKeys := make([]string, len(got))
		for i, p := range got {
			gotKeys[i] = p.Key
		}
		if strings.Join(gotKeys, ",") != strings.Join(want, ",") {
			t.Fatalf("demo %s: want links %v, got %v", testKey, want, gotKeys)
		}
		if checked++; checked == 5 {
			break
		}
	}
	if checked == 0 {
		t.Fatal("no demo test had preconditions to compare")
	}
}

// TestListTestPreconditionsDemoUnlinkedTest checks a demo Test with no
// preconditions answers empty rather than erroring.
func TestListTestPreconditionsDemoUnlinkedTest(t *testing.T) {
	c := &Client{baseURL: "demo", token: "t", http: http.DefaultClient}
	got, err := c.ListTestPreconditions(context.Background(), "DEMO-999999")
	if err != nil {
		t.Fatalf("demo ListTestPreconditions: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want no preconditions for an unlinked test, got %+v", got)
	}
}

// TestParseTestPreconditionsAcceptsBothShapes covers the bare array Xray
// Server returns and the wrapped form the other association endpoints use.
func TestParseTestPreconditionsAcceptsBothShapes(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"bare array", `[{"key":"PC-1"},{"key":"PC-2"}]`, 2},
		{"wrapped", `{"preconditions":[{"key":"PC-1"}]}`, 1},
		{"empty array", `[]`, 0},
		{"empty body", ``, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTestPreconditions([]byte(tc.body))
			if err != nil {
				t.Fatalf("parseTestPreconditions: %v", err)
			}
			if len(got) != tc.want {
				t.Errorf("want %d entries, got %d", tc.want, len(got))
			}
		})
	}
}

// TestPreconditionDetailsQuotesKeys checks the enrichment JQL quotes issue keys
// so a project key containing characters JQL treats specially cannot break the
// query.
func TestPreconditionDetailsQuotesKeys(t *testing.T) {
	var raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[]}`))
	}))
	defer srv.Close()

	if _, err := newTestClient(srv).preconditionDetails(
		context.Background(), []string{"RND_P_4JKTVA_05-4563"},
	); err != nil {
		t.Fatalf("preconditionDetails: %v", err)
	}
	q, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("parse query: %v", err)
	}
	if want := `key in ("RND_P_4JKTVA_05-4563")`; q.Get("jql") != want {
		t.Errorf("want jql %q, got %q", want, q.Get("jql"))
	}
}
