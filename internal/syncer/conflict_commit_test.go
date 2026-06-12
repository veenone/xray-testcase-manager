package syncer_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"xray-test-manager/internal/jira"
	"xray-test-manager/internal/store"
	"xray-test-manager/internal/syncer"
	"xray-test-manager/internal/testrepo"
)

const (
	conflictT0 = "2026-01-01T00:00:00.000+0000" // local base
	conflictT1 = "2026-02-01T00:00:00.000+0000" // remote moved later
)

// seedPriorityEdit caches QA-1 (priority Medium, updated T0) and queues a local
// priority edit Medium -> High.
func seedPriorityEdit(t *testing.T) *testrepo.Repository {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	repo := testrepo.NewRepository(st)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "Original", Priority: "Medium", Updated: conflictT0},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := repo.EditTestField("p1", "QA-1", "priority", "High"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	return repo
}

// conflictServer mocks just enough of Jira: the `updated` pre-check returns T1
// (remote moved); the full-field fetch returns remoteSummary/remotePriority; a
// PUT flips putHit.
func conflictServer(remoteSummary, remotePriority string, putHit *bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/rest/api/2/issue/"):
			if r.URL.Query().Get("fields") == "updated" {
				_ = json.NewEncoder(w).Encode(map[string]any{"fields": map[string]any{"updated": conflictT1}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"key": "QA-1",
				"fields": map[string]any{
					"summary":  remoteSummary,
					"priority": map[string]any{"name": remotePriority},
					"updated":  conflictT1,
				},
			})
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/rest/api/2/issue/"):
			if putHit != nil {
				*putHit = true
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
}

// Remote changed a DIFFERENT field (summary). My priority edit's base still
// matches remote priority, so it auto-merges and commits — no conflict.
func TestCommitAutoMergesNonOverlappingRemoteChange(t *testing.T) {
	repo := seedPriorityEdit(t)
	put := false
	srv := conflictServer("Changed by someone else", "Medium", &put)
	defer srv.Close()

	res, err := syncer.New(jira.NewClient(srv.URL, "tok"), repo).
		CommitChanges(context.Background(), "p1", "QA")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(res.Conflicted) != 0 {
		t.Fatalf("expected no conflict (non-overlapping), got %+v", res.Conflicted)
	}
	if len(res.Succeeded) != 1 || res.Succeeded[0] != "QA-1" {
		t.Fatalf("expected QA-1 committed, got %+v", res.Succeeded)
	}
	if !put {
		t.Fatalf("expected the priority PUT (auto-merge) to happen")
	}
}

// Remote changed the SAME field (priority) to a third value — a true conflict,
// held back with the three-way detail and no PUT.
func TestCommitHoldsTrueConflict(t *testing.T) {
	repo := seedPriorityEdit(t)
	put := false
	srv := conflictServer("Original", "Critical", &put)
	defer srv.Close()

	res, err := syncer.New(jira.NewClient(srv.URL, "tok"), repo).
		CommitChanges(context.Background(), "p1", "QA")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if put {
		t.Fatalf("must not PUT a conflicting field")
	}
	if len(res.Conflicted) != 1 {
		t.Fatalf("expected 1 conflicted test, got %+v", res.Conflicted)
	}
	c := res.Conflicted[0]
	if len(c.Fields) != 1 {
		t.Fatalf("expected 1 conflict field, got %+v", c.Fields)
	}
	f := c.Fields[0]
	if f.Field != "priority" || f.Base != "Medium" || f.Remote != "Critical" || f.Mine != "High" {
		t.Fatalf("bad three-way: base=%q remote=%q mine=%q", f.Base, f.Remote, f.Mine)
	}
}
