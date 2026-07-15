package syncer_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"xray-test-manager/internal/backend/xray"
	"xray-test-manager/internal/jira"
	"xray-test-manager/internal/store"
	"xray-test-manager/internal/syncer"
	"xray-test-manager/internal/testrepo"
)

const stepDeleteT0 = "2026-01-01T00:00:00.000+0000"

// stepServer mocks just enough of Xray for a no-conflict step commit, modelling
// the one behaviour that matters here: a PUT or DELETE against a step id Xray
// never issued fails (real Xray 400/500s), so a reorder or delete that targets a
// cancelled local-only "new-N" step surfaces as a commit failure rather than
// silently passing. The `updated` pre-check echoes the local base so the commit
// is not held back as a conflict; step POSTs mint a fresh real id.
type stepServer struct {
	*httptest.Server
	mu      sync.Mutex
	valid   map[string]bool // step ids Xray "knows" (seeded + created)
	creates []string
	deletes []string
	nextID  int
}

func newStepServer(seeded ...string) *stepServer {
	s := &stepServer{valid: map[string]bool{}}
	for _, id := range seeded {
		s.valid[id] = true
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

func (s *stepServer) handle(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case strings.HasSuffix(path, "/steps") && r.Method == http.MethodPost:
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.creates = append(s.creates, string(body))
		s.nextID++
		id := "real-" + string(rune('0'+s.nextID))
		s.valid[id] = true
		s.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
	case strings.Contains(path, "/steps/") && (r.Method == http.MethodPut || r.Method == http.MethodDelete):
		id := path[strings.LastIndex(path, "/steps/")+len("/steps/"):]
		s.mu.Lock()
		known := s.valid[id]
		if r.Method == http.MethodDelete {
			s.deletes = append(s.deletes, id)
			delete(s.valid, id)
		}
		s.mu.Unlock()
		if !known {
			// Xray rejects a write to a step it never created.
			http.Error(w, `{"error":"no such step"}`, http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case strings.HasSuffix(path, "/steps") && r.Method == http.MethodGet:
		_, _ = w.Write([]byte("[]"))
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/rest/api/2/issue/"):
		if r.URL.Query().Get("fields") == "updated" {
			_ = json.NewEncoder(w).Encode(map[string]any{"fields": map[string]any{"updated": stepDeleteT0}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "QA-1", "fields": map[string]any{"summary": "QA-1", "updated": stepDeleteT0}})
	default:
		_, _ = w.Write([]byte(`{}`))
	}
}

func seedTestForSteps(t *testing.T, steps []testrepo.Step) *testrepo.Repository {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	repo := testrepo.NewRepository(st)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{{Key: "QA-1", ID: "1", Updated: stepDeleteT0}}); err != nil {
		t.Fatalf("seed test: %v", err)
	}
	if len(steps) > 0 {
		if err := repo.SetTestSteps("p1", "QA-1", steps); err != nil {
			t.Fatalf("seed steps: %v", err)
		}
	}
	return repo
}

// Regression for RND_P_4TFINT_05-203: open a test, add a new step, delete it,
// add another new step, commit. The deleted step never reached Xray, so the
// commit must create only the surviving step and issue no write against the
// cancelled step's temporary "new-N" id.
func TestCommitNewStepAddedThenDeletedThenReadded(t *testing.T) {
	repo := seedTestForSteps(t, nil)

	s1, err := repo.AddTestStep("p1", "QA-1", "first step", "", "")
	if err != nil {
		t.Fatalf("add step 1: %v", err)
	}
	if err := repo.DeleteTestStep("p1", "QA-1", s1.XrayID); err != nil {
		t.Fatalf("delete step 1: %v", err)
	}
	if _, err := repo.AddTestStep("p1", "QA-1", "second step", "", ""); err != nil {
		t.Fatalf("add step 2: %v", err)
	}

	srv := newStepServer()
	defer srv.Close()

	res, err := syncer.New(xray.New(jira.NewClient(srv.URL, "tok")), repo).
		CommitChanges(context.Background(), "p1", "QA")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("commit reported failures: %+v", res.Failed)
	}
	if len(res.Succeeded) != 1 || res.Succeeded[0] != "QA-1" {
		t.Fatalf("expected QA-1 committed, got %+v", res.Succeeded)
	}
	if len(srv.deletes) != 0 {
		t.Fatalf("must not DELETE a step that only existed locally, got %v", srv.deletes)
	}
	if len(srv.creates) != 1 {
		t.Fatalf("expected exactly one step create (the surviving step), got %d: %v", len(srv.creates), srv.creates)
	}
	if !strings.Contains(srv.creates[0], "second step") {
		t.Errorf("created step body = %q, want the surviving 'second step'", srv.creates[0])
	}
}

// Regression for RND_P_4TFINT_05-203 (reorder variant): a new step that was
// reordered and then deleted before commit must not leave its temporary id in
// the queued step order, which would reorder a step Xray never created.
func TestCommitNewStepReorderedThenDeleted(t *testing.T) {
	repo := seedTestForSteps(t, []testrepo.Step{
		{XrayID: "10", Index: 1, Action: "a1"},
		{XrayID: "11", Index: 2, Action: "a2"},
	})

	// Add a scratch step, reorder so it sits between a genuine swap of the two
	// real steps, then delete the scratch step before commit. Pruning its temp
	// id must leave the real swap ([11,10]) intact and committable.
	s, err := repo.AddTestStep("p1", "QA-1", "scratch", "", "")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := repo.ReorderTestSteps("p1", "QA-1", []string{"11", s.XrayID, "10"}); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	if err := repo.DeleteTestStep("p1", "QA-1", s.XrayID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	srv := newStepServer("10", "11")
	defer srv.Close()

	res, err := syncer.New(xray.New(jira.NewClient(srv.URL, "tok")), repo).
		CommitChanges(context.Background(), "p1", "QA")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("commit reported failures (phantom-step reorder): %+v", res.Failed)
	}
	if len(res.Succeeded) != 1 || res.Succeeded[0] != "QA-1" {
		t.Fatalf("expected QA-1 committed (real swap survives), got %+v", res.Succeeded)
	}
	if len(srv.creates) != 0 {
		t.Errorf("no step should be created (scratch was cancelled), got %v", srv.creates)
	}
	if len(srv.deletes) != 0 {
		t.Errorf("no step should be deleted remotely, got %v", srv.deletes)
	}
}
