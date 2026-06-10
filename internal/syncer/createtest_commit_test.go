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

// TestCommitNewTestCreatesThenLinksPrecondition proves a brand-new Test with a
// linked precondition commits in the right order: the Test is created first,
// then the precondition association targets the REAL key, never NEW-N.
func TestCommitNewTestCreatesThenLinksPrecondition(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	repo := testrepo.NewRepository(st)

	tempKey, err := repo.CreateTest("p1", testrepo.TestDraft{
		Summary:     "New login test",
		PrecondKeys: []string{"PC-1"},
	})
	if err != nil {
		t.Fatalf("CreateTest: %v", err)
	}

	var (
		mu       sync.Mutex
		sequence []string // ordered request paths
		assocAdd []string // test keys added to PC-1
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		sequence = append(sequence, r.Method+" "+r.URL.Path)
		mu.Unlock()
		switch {
		case r.URL.Path == "/rest/api/2/issuetype":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "5", "name": "Test"}})
		case r.URL.Path == "/rest/api/2/issue":
			_ = json.NewEncoder(w).Encode(map[string]string{"key": "QA-900"})
		case strings.HasPrefix(r.URL.Path, "/rest/api/2/issue/"):
			// GetIssueUpdated conflict pre-check — report no remote movement.
			_ = json.NewEncoder(w).Encode(map[string]any{"fields": map[string]any{"updated": ""}})
		case r.URL.Path == "/rest/raven/1.0/api/precondition/PC-1/test":
			var body map[string][]string
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			mu.Lock()
			assocAdd = append(assocAdd, body["add"]...)
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

	// The association must carry the real key, never the temp key.
	if len(assocAdd) != 1 || assocAdd[0] != "QA-900" {
		t.Fatalf("precondition add = %v, want [QA-900] (not %s)", assocAdd, tempKey)
	}

	// Ordering: the issue create precedes the precondition association.
	createAt, assocAt := -1, -1
	for i, s := range sequence {
		if s == "POST /rest/api/2/issue" && createAt == -1 {
			createAt = i
		}
		if s == "POST /rest/raven/1.0/api/precondition/PC-1/test" && assocAt == -1 {
			assocAt = i
		}
	}
	if createAt == -1 || assocAt == -1 || createAt > assocAt {
		t.Fatalf("expected create before association, got sequence %v", sequence)
	}
}
