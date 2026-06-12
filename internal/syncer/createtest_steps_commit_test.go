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

	"xray-test-manager/internal/jira"
	"xray-test-manager/internal/store"
	"xray-test-manager/internal/syncer"
	"xray-test-manager/internal/testrepo"
)

// TestCommitNewTestWithReorderedStepsNoTempIDPut guards the 500 regression:
// committing a brand-new Test whose steps were reordered before commit must
// create the steps in their final (reordered) order and must NOT issue a step
// reorder PUT against the temporary "new-N" ids (which 500s on Xray).
func TestCommitNewTestWithReorderedStepsNoTempIDPut(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	repo := testrepo.NewRepository(st)

	tempKey, err := repo.CreateTest("p1", testrepo.TestDraft{
		Summary: "New flow",
		Steps: []testrepo.StepDraft{
			{Action: "first", Expected: "ok1"},
			{Action: "second", Expected: "ok2"},
		},
	})
	if err != nil {
		t.Fatalf("CreateTest: %v", err)
	}

	// Reorder the steps to [second, first] before committing.
	cached, _ := repo.ListTestSteps("p1", tempKey)
	if len(cached) != 2 {
		t.Fatalf("want 2 cached steps, got %d", len(cached))
	}
	if err := repo.ReorderTestSteps("p1", tempKey,
		[]string{cached[1].XrayID, cached[0].XrayID}); err != nil {
		t.Fatalf("reorder: %v", err)
	}

	var (
		mu          sync.Mutex
		stepActions []string // order steps were POSTed in
		stepPuts    []string // any PUT to /steps/{id} (the bug)
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/2/issuetype":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "5", "name": "Test"}})
		case r.URL.Path == "/rest/api/2/issue":
			_ = json.NewEncoder(w).Encode(map[string]string{"key": "QA-901"})
		case strings.HasPrefix(r.URL.Path, "/rest/api/2/issue/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"fields": map[string]any{"updated": ""}})
		case strings.HasSuffix(r.URL.Path, "/steps") && r.Method == http.MethodPost:
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				Fields map[string]string `json:"fields"`
			}
			_ = json.Unmarshal(raw, &body)
			mu.Lock()
			stepActions = append(stepActions, body.Fields["Action"])
			id := len(stepActions)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
		case strings.Contains(r.URL.Path, "/steps/") && r.Method == http.MethodPut:
			mu.Lock()
			stepPuts = append(stepPuts, r.URL.Path)
			mu.Unlock()
			// Mimic Xray rejecting a PUT against a non-existent step id.
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"Step fields must be provided to create a new test step"}`))
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
		t.Fatalf("commit failed (the 500 regression): %+v", result.Failed)
	}
	if len(stepPuts) != 0 {
		t.Errorf("a new test should not reorder via PUT, got %v", stepPuts)
	}
	if len(stepActions) != 2 || stepActions[0] != "second" || stepActions[1] != "first" {
		t.Errorf("steps created in order %v, want [second, first] (the reorder)", stepActions)
	}
	// The new test's pending rows (create + reorder) are all cleared.
	if pc, _ := repo.ListPendingChanges("p1"); len(pc) != 0 {
		t.Errorf("want 0 pending changes after commit, got %d: %+v", len(pc), pc)
	}
}
