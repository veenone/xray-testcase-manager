package syncer_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"xray-test-manager/internal/jira"
	"xray-test-manager/internal/store"
	"xray-test-manager/internal/syncer"
	"xray-test-manager/internal/testrepo"
)

// seedRunDefectsCommitRepo seeds a store with one Test Execution (QA-TE-1)
// containing QA-1 as its only member, mirroring the testrepo package's own
// seedRunDefectsRepo helper (internal/testrepo/rundefects_test.go).
func seedRunDefectsCommitRepo(t *testing.T) *testrepo.Repository {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	repo := testrepo.NewRepository(st)

	if err := repo.UpsertTests("p1", []testrepo.TestCase{{Key: "QA-1", ID: "1"}}); err != nil {
		t.Fatalf("seed test: %v", err)
	}
	if err := repo.UpsertContainers("p1", []testrepo.Container{
		{Key: "QA-TE-1", Kind: "testexec", Summary: "Cycle 1"},
	}); err != nil {
		t.Fatalf("seed exec: %v", err)
	}
	if err := repo.ReplaceAllContainerLinks("p1", []testrepo.ContainerLink{
		{ContainerKey: "QA-TE-1", TestKey: "QA-1", RunStatus: "TODO"},
	}); err != nil {
		t.Fatalf("seed link: %v", err)
	}
	return repo
}

// runIDHandler returns the standard resolveTestRunID response every one of
// these tests needs: a fixed run id for QA-TE-1/QA-1.
func runIDHandler(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(map[string]any{"id": 501})
}

// TestCommitRunCommentPostsAndClears proves a test_run_comment pending row
// commits via SetTestRunComment(execKey, testKey, afterVal) and the row is
// cleared from the pending journal on success.
func TestCommitRunCommentPostsAndClears(t *testing.T) {
	repo := seedRunDefectsCommitRepo(t)
	if err := repo.SetTestRunComment("p1", "QA-TE-1", "QA-1", "looked fine"); err != nil {
		t.Fatalf("SetTestRunComment: %v", err)
	}

	var (
		mu          sync.Mutex
		commentBody string
		commentHit  bool
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/raven/1.0/api/testrun":
			runIDHandler(w)
		case r.URL.Path == "/rest/raven/1.0/api/testrun/501/comment" && r.Method == http.MethodPut:
			raw, _ := io.ReadAll(r.Body)
			mu.Lock()
			commentBody = string(raw)
			commentHit = true
			mu.Unlock()
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()

	engine := syncer.New(jira.NewClient(srv.URL, "t"), repo)
	result, err := engine.CommitChanges(context.Background(), "p1", "QA")
	if err != nil {
		t.Fatalf("CommitChanges: %v", err)
	}
	if len(result.Failed) != 0 {
		t.Fatalf("unexpected failures: %+v", result.Failed)
	}

	mu.Lock()
	defer mu.Unlock()
	if !commentHit {
		t.Fatal("expected SetTestRunComment to PUT the run comment endpoint")
	}
	if commentBody != "looked fine" {
		t.Fatalf("comment body = %q, want %q", commentBody, "looked fine")
	}

	pending, err := repo.ListPendingChanges("p1")
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	for _, c := range pending {
		if c.EntityType == "test_run_comment" {
			t.Fatalf("test_run_comment pending change not cleared after commit: %+v", c)
		}
	}
}

// TestCommitRunDefectAddOnlyCallsAdd proves before ["A"]/after ["A","B"]
// calls AddTestRunDefect(..., "B") only, no RemoveTestRunDefect, and the row
// clears on success.
func TestCommitRunDefectAddOnlyCallsAdd(t *testing.T) {
	repo := seedRunDefectsCommitRepo(t)
	// Seed the synced base ["A"], then stage adding "B" -> before ["A"], after ["A","B"].
	if err := repo.ReplaceRunsForExec("p1", "QA-TE-1", []testrepo.TestRunRow{
		{ExecKey: "QA-TE-1", TestKey: "QA-1", RunStatus: "TODO", Defects: `["A"]`},
	}); err != nil {
		t.Fatalf("seed synced run: %v", err)
	}
	if err := repo.AddTestRunDefect("p1", "QA-TE-1", "QA-1", "B"); err != nil {
		t.Fatalf("AddTestRunDefect: %v", err)
	}

	var (
		mu      sync.Mutex
		added   []string
		removed []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/raven/1.0/api/testrun":
			runIDHandler(w)
		case r.URL.Path == "/rest/raven/1.0/api/testrun/501/defect" && r.Method == http.MethodPost:
			var keys []string
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &keys)
			mu.Lock()
			added = append(added, keys...)
			mu.Unlock()
		case r.Method == http.MethodDelete:
			mu.Lock()
			removed = append(removed, r.URL.Path)
			mu.Unlock()
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()

	engine := syncer.New(jira.NewClient(srv.URL, "t"), repo)
	result, err := engine.CommitChanges(context.Background(), "p1", "QA")
	if err != nil {
		t.Fatalf("CommitChanges: %v", err)
	}
	if len(result.Failed) != 0 {
		t.Fatalf("unexpected failures: %+v", result.Failed)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(added) != 1 || added[0] != "B" {
		t.Fatalf("added = %v, want [B]", added)
	}
	if len(removed) != 0 {
		t.Fatalf("removed = %v, want none", removed)
	}

	pending, err := repo.ListPendingChanges("p1")
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	for _, c := range pending {
		if c.EntityType == "test_run_defect" {
			t.Fatalf("test_run_defect pending change not cleared after commit: %+v", c)
		}
	}
}

// TestCommitRunDefectRemoveOnlyCallsRemove proves before ["A","B"]/after
// ["A"] calls RemoveTestRunDefect(..., "B") only, no AddTestRunDefect, and
// the row clears on success.
func TestCommitRunDefectRemoveOnlyCallsRemove(t *testing.T) {
	repo := seedRunDefectsCommitRepo(t)
	// Seed the synced base ["A","B"], then stage removing "B" -> before ["A","B"], after ["A"].
	if err := repo.ReplaceRunsForExec("p1", "QA-TE-1", []testrepo.TestRunRow{
		{ExecKey: "QA-TE-1", TestKey: "QA-1", RunStatus: "TODO", Defects: `["A","B"]`},
	}); err != nil {
		t.Fatalf("seed synced run: %v", err)
	}
	if err := repo.RemoveTestRunDefect("p1", "QA-TE-1", "QA-1", "B"); err != nil {
		t.Fatalf("RemoveTestRunDefect: %v", err)
	}

	var (
		mu      sync.Mutex
		added   []string
		removed []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/raven/1.0/api/testrun":
			runIDHandler(w)
		case r.Method == http.MethodPost && r.URL.Path == "/rest/raven/1.0/api/testrun/501/defect":
			var keys []string
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &keys)
			mu.Lock()
			added = append(added, keys...)
			mu.Unlock()
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/raven/1.0/api/testrun/501/defect/B":
			mu.Lock()
			removed = append(removed, "B")
			mu.Unlock()
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()

	engine := syncer.New(jira.NewClient(srv.URL, "t"), repo)
	result, err := engine.CommitChanges(context.Background(), "p1", "QA")
	if err != nil {
		t.Fatalf("CommitChanges: %v", err)
	}
	if len(result.Failed) != 0 {
		t.Fatalf("unexpected failures: %+v", result.Failed)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(removed) != 1 || removed[0] != "B" {
		t.Fatalf("removed = %v, want [B]", removed)
	}
	if len(added) != 0 {
		t.Fatalf("added = %v, want none", added)
	}

	pending, err := repo.ListPendingChanges("p1")
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	for _, c := range pending {
		if c.EntityType == "test_run_defect" {
			t.Fatalf("test_run_defect pending change not cleared after commit: %+v", c)
		}
	}
}

// TestCommitRunDefectClientErrorLeavesRowPending proves a client error (the
// fake server 500s the add call) leaves the test_run_defect row pending (not
// cleared) and reports the Test as Failed.
func TestCommitRunDefectClientErrorLeavesRowPending(t *testing.T) {
	repo := seedRunDefectsCommitRepo(t)
	if err := repo.AddTestRunDefect("p1", "QA-TE-1", "QA-1", "B"); err != nil {
		t.Fatalf("AddTestRunDefect: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/raven/1.0/api/testrun":
			runIDHandler(w)
		case r.URL.Path == "/rest/raven/1.0/api/testrun/501/defect" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()

	engine := syncer.New(jira.NewClient(srv.URL, "t"), repo)
	result, err := engine.CommitChanges(context.Background(), "p1", "QA")
	if err != nil {
		t.Fatalf("CommitChanges: %v", err)
	}
	if len(result.Succeeded) != 0 {
		t.Fatalf("expected no successes, got %v", result.Succeeded)
	}
	found := false
	for _, f := range result.Failed {
		if f.TestKey == "QA-1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected QA-1 in Failed, got %+v", result.Failed)
	}

	pending, err := repo.ListPendingChanges("p1")
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	stillPending := false
	for _, c := range pending {
		if c.EntityType == "test_run_defect" && c.EntityKey == "QA-TE-1:QA-1" {
			stillPending = true
		}
	}
	if !stillPending {
		t.Fatal("expected the test_run_defect row to remain pending after a client error")
	}
}
