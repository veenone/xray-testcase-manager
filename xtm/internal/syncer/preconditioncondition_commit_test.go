package syncer_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"agile-suite/xtm/internal/backend/xray"
	"agile-suite/xtm/internal/jira"
	"agile-suite/xtm/internal/store"
	"agile-suite/xtm/internal/syncer"
	"agile-suite/xtm/internal/testrepo"
)

// seedPreconditionEdit opens a repo holding one synced precondition with a
// pending edit to field, and returns the repo.
func seedPreconditionEdit(t *testing.T, field, value string) *testrepo.Repository {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	repo := testrepo.NewRepository(st)

	if err := repo.UpsertPreconditions("p1", []testrepo.Precondition{
		{Key: "QA-P-1", Summary: "Board reachable", Description: "d"},
	}); err != nil {
		t.Fatalf("seed precondition: %v", err)
	}
	if err := repo.EditPreconditionField("p1", "QA-P-1", field, value); err != nil {
		t.Fatalf("edit %s: %v", field, err)
	}
	return repo
}

// conditionServer serves the field catalogue and captures the issue update.
// withField controls whether the instance has the condition custom field.
func conditionServer(t *testing.T, withField bool, captured *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/2/field":
			fields := []map[string]any{{"id": "summary", "name": "Summary", "custom": false}}
			if withField {
				fields = append(fields, map[string]any{
					"id": "customfield_13989", "name": "Conditions", "custom": true,
					"schema": map[string]any{"type": "string"},
				})
			}
			_ = json.NewEncoder(w).Encode(fields)
		case strings.HasPrefix(r.URL.Path, "/rest/api/2/issue/") && r.Method == http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			var payload struct {
				Fields map[string]any `json:"fields"`
			}
			_ = json.Unmarshal(body, &payload)
			*captured = payload.Fields
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
}

// A condition edit reaches Jira on the instance's own custom field. It used to
// be dropped: FieldsForJira maps the system fields alone, so the PUT carried
// nothing for it and the pending row was cleared as though it had committed
// (RND_P_4TFINT_05-358).
func TestCommitPreconditionConditionReachesJira(t *testing.T) {
	var sent map[string]any
	srv := conditionServer(t, true, &sent)
	defer srv.Close()

	repo := seedPreconditionEdit(t, "condition", "snmpget -v3 ... $BOARD sysName.0")
	res, err := syncer.New(xray.New(jira.NewClient(srv.URL, "tok")), repo).
		CommitChanges(context.Background(), "p1", "QA")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("commit failed: %+v", res.Failed)
	}
	if sent["customfield_13989"] != "snmpget -v3 ... $BOARD sysName.0" {
		t.Errorf("PUT fields = %v, want the condition on customfield_13989", sent)
	}
	assertNoPendingPreconditionEdit(t, repo)
}

// A summary edit alongside the condition still travels as a system field, so
// the two commit in one update rather than one clobbering the other.
func TestCommitPreconditionSendsConditionBesideSystemFields(t *testing.T) {
	var sent map[string]any
	srv := conditionServer(t, true, &sent)
	defer srv.Close()

	repo := seedPreconditionEdit(t, "condition", "a condition")
	if err := repo.EditPreconditionField("p1", "QA-P-1", "summary", "Renamed"); err != nil {
		t.Fatalf("edit summary: %v", err)
	}
	res, err := syncer.New(xray.New(jira.NewClient(srv.URL, "tok")), repo).
		CommitChanges(context.Background(), "p1", "QA")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("commit failed: %+v", res.Failed)
	}
	if sent["summary"] != "Renamed" || sent["customfield_13989"] != "a condition" {
		t.Errorf("PUT fields = %v, want both summary and the condition", sent)
	}
}

// An instance with no condition field leaves the edit pending and reports it
// skipped, rather than PUTting an empty update and reporting success, which is
// how the edit used to disappear.
func TestCommitPreconditionConditionSkippedWhenTheFieldIsAbsent(t *testing.T) {
	var sent map[string]any
	srv := conditionServer(t, false, &sent)
	defer srv.Close()

	repo := seedPreconditionEdit(t, "condition", "a condition")
	res, err := syncer.New(xray.New(jira.NewClient(srv.URL, "tok")), repo).
		CommitChanges(context.Background(), "p1", "QA")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("commit failed: %+v", res.Failed)
	}
	if sent != nil {
		t.Errorf("no field to write, but an update was sent: %v", sent)
	}
	skipped := false
	for _, s := range res.Skipped {
		if s.EntityType == "precondition_edit" && s.EntityKey == "QA-P-1" {
			skipped = true
		}
	}
	if !skipped {
		t.Errorf("skipped = %+v, want the precondition_edit reported", res.Skipped)
	}
	// The row stays pending so it can go once the instance gains the field.
	pending, err := repo.ListPendingChanges("p1")
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	found := false
	for _, c := range pending {
		if c.EntityType == "precondition_edit" && c.Field == "condition" {
			found = true
		}
	}
	if !found {
		t.Error("the condition edit should stay pending when it cannot be pushed")
	}
}

func assertNoPendingPreconditionEdit(t *testing.T, repo *testrepo.Repository) {
	t.Helper()
	pending, err := repo.ListPendingChanges("p1")
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	for _, c := range pending {
		if c.EntityType == "precondition_edit" {
			t.Errorf("precondition_edit still pending after a successful commit: %+v", c)
		}
	}
}
