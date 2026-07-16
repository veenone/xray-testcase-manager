package syncer_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"xray-test-manager/internal/backend"
	"xray-test-manager/internal/jira"
	"xray-test-manager/internal/store"
	"xray-test-manager/internal/syncer"
	"xray-test-manager/internal/testrepo"
)

// kiwiFakeBackend is a minimal backend.Backend that advertises Kiwi-style
// capabilities (settable status, inline-text steps, no folders / preconditions /
// requirement writes / bug creation). Only the methods the Kiwi commit path is
// expected to call are implemented; every other method is left on the nil
// embedded interface, so a mis-routed call (e.g. GetTransitions or per-step CRUD)
// panics and fails the test loudly — proving those paths are NOT taken.
type kiwiFakeBackend struct {
	backend.Backend
	caps        backend.Capabilities
	mu          sync.Mutex
	updateCalls []fakeUpdateCall
}

type fakeUpdateCall struct {
	key    string
	fields map[string]any
}

func newKiwiFakeBackend() *kiwiFakeBackend {
	return &kiwiFakeBackend{caps: backend.Capabilities{
		Name:                        "kiwi",
		IDStyle:                     "numeric",
		StepModel:                   "inline-text",
		SupportsTestTypes:           true,
		SupportsFolders:             false,
		SupportsPreconditionObjects: false,
		SupportsRequirementObjects:  false,
		SupportsContainers:          true,
		SupportsTestRuns:            true,
		StatusModel:                 "settable",
		SupportsWorkflowTransitions: false,
		SupportsBugCreation:         false,
		SupportsTags:                true,
	}}
}

func (b *kiwiFakeBackend) Capabilities() backend.Capabilities { return b.caps }

func (b *kiwiFakeBackend) RemoteVersion(ctx context.Context, entityType, externalKey string) (backend.VersionToken, error) {
	return "", nil // no remote movement -> conflict pre-check is skipped
}

func (b *kiwiFakeBackend) RemoteAhead(base, remote backend.VersionToken) bool { return false }

// FieldsForJira mirrors the Kiwi adapter's neutral->native mapping so the engine
// routing under test resolves the same field names it would live.
func (b *kiwiFakeBackend) FieldsForJira(updates map[string]string) map[string]any {
	out := make(map[string]any, len(updates))
	for f, v := range updates {
		switch f {
		case "summary":
			out["summary"] = v
		case "description":
			out["text"] = v
		case "priority":
			out["priority"] = v
		case "status", "case_status":
			out["case_status"] = v
		case "labels":
			out["labels"] = v
		case "components":
			out["components"] = v
		}
	}
	return out
}

func (b *kiwiFakeBackend) UpdateIssue(ctx context.Context, key string, fields map[string]any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.updateCalls = append(b.updateCalls, fakeUpdateCall{key: key, fields: fields})
	return nil
}

func (b *kiwiFakeBackend) callsWithField(field string) []fakeUpdateCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []fakeUpdateCall
	for _, c := range b.updateCalls {
		if _, ok := c.fields[field]; ok {
			out = append(out, c)
		}
	}
	return out
}

// TestKiwiCommitStatusAndStepsRouteToFieldUpdates proves the capability routing:
// a settable-status backend commits a status change as a case_status FIELD update
// via UpdateIssue (never applyTransition/GetTransitions), and an inline-text step
// model collapses step edits into ONE text field update (never per-step CRUD).
// The per-step CRUD and transition methods are unimplemented on the fake, so any
// wrong route would panic.
func TestKiwiCommitStatusAndStepsRouteToFieldUpdates(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	repo := testrepo.NewRepository(st)

	const profileID = "p1"
	if err := repo.UpsertTests(profileID, []testrepo.TestCase{
		{Key: "42", ID: "42", Summary: "Original", Status: "CONFIRMED", Priority: "Medium", Updated: "2026-01-01T00:00:00Z"},
	}); err != nil {
		t.Fatalf("seed test: %v", err)
	}
	if err := repo.SetTestSteps(profileID, "42", []testrepo.Step{
		{XrayID: "1", Index: 1, Action: "Original step text"},
	}); err != nil {
		t.Fatalf("seed steps: %v", err)
	}

	// Queue a field edit + a status change + a step edit.
	if err := repo.EditTestField(profileID, "42", "summary", "Updated summary"); err != nil {
		t.Fatalf("edit summary: %v", err)
	}
	if err := repo.TransitionTest(profileID, "42", "PROPOSED"); err != nil {
		t.Fatalf("transition: %v", err)
	}
	if err := repo.EditTestStepField(profileID, "42", "1", "action", "Rewritten step text"); err != nil {
		t.Fatalf("edit step: %v", err)
	}

	fake := newKiwiFakeBackend()
	eng := syncer.New(fake, repo)
	result, err := eng.CommitChanges(context.Background(), profileID, "PROD")
	if err != nil {
		t.Fatalf("CommitChanges: %v", err)
	}
	if len(result.Failed) != 0 {
		t.Fatalf("unexpected failures: %+v", result.Failed)
	}
	if !containsStr(result.Succeeded, "42") {
		t.Errorf("Succeeded = %v, want it to contain 42", result.Succeeded)
	}

	// Status routed as a case_status field update, not a transition.
	if got := fake.callsWithField("case_status"); len(got) != 1 {
		t.Errorf("case_status field updates = %d, want 1 (status must route via UpdateIssue): %+v", len(got), fake.updateCalls)
	} else if got[0].fields["case_status"] != "PROPOSED" {
		t.Errorf("case_status value = %v, want PROPOSED", got[0].fields["case_status"])
	}

	// Steps collapsed into exactly one text field update carrying the edited text.
	textCalls := fake.callsWithField("text")
	if len(textCalls) != 1 {
		t.Fatalf("text field updates = %d, want exactly 1 (inline-text collapse): %+v", len(textCalls), fake.updateCalls)
	}
	if textCalls[0].fields["text"] != "Rewritten step text" {
		t.Errorf("text = %v, want the rewritten step text (reversible single-step flatten)", textCalls[0].fields["text"])
	}

	// Summary went through the normal field-update path too.
	if got := fake.callsWithField("summary"); len(got) != 1 {
		t.Errorf("summary field updates = %d, want 1", len(got))
	}

	// The committed rows are cleared.
	after, err := repo.ListPendingChanges(profileID)
	if err != nil {
		t.Fatalf("list pending after: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("pending not cleared after Kiwi commit: %+v", after)
	}
}

// TestKiwiCommitSkipsUnsupportedBuckets proves that every bucket whose capability
// is off on a Kiwi backend is reported in result.Skipped (not Failed) and its
// pending rows are kept in place, so nothing is silently dropped.
func TestKiwiCommitSkipsUnsupportedBuckets(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	repo := testrepo.NewRepository(st)

	const profileID = "p1"
	if err := repo.UpsertTests(profileID, []testrepo.TestCase{
		{Key: "42", ID: "42", Summary: "T", Status: "CONFIRMED", Priority: "Medium", Updated: "2026-01-01T00:00:00Z"},
	}); err != nil {
		t.Fatalf("seed test: %v", err)
	}
	if err := repo.UpsertPreconditions(profileID, []testrepo.Precondition{
		{Key: "PC-1", Summary: "precond", Type: "Manual"},
	}); err != nil {
		t.Fatalf("seed precondition: %v", err)
	}
	if err := repo.UpsertContainers(profileID, []testrepo.Container{
		{Key: "TP-1", Kind: jira.KindTestPlan, Summary: "plan", Status: "Open"},
	}); err != nil {
		t.Fatalf("seed container: %v", err)
	}

	// Queue one pending row per unsupported bucket.
	if err := repo.MoveTestToFolder(profileID, "42", "some-folder-id"); err != nil {
		t.Fatalf("move to folder: %v", err)
	}
	if err := repo.SetTestPreconditions(profileID, "42", []string{"PC-1"}); err != nil {
		t.Fatalf("set preconditions: %v", err)
	}
	if err := repo.SetTestReview(profileID, "42", "approved", "me", "ok"); err != nil {
		t.Fatalf("set review: %v", err)
	}
	if err := repo.SetTestRequirements(profileID, "42", []string{"REQ-1"}); err != nil {
		t.Fatalf("set requirements: %v", err)
	}
	if err := repo.EditContainer(profileID, "TP-1", "renamed plan"); err != nil {
		t.Fatalf("rename container: %v", err)
	}
	if _, err := repo.CreateBugForTest(profileID, "42", "", testrepo.BugDraft{
		ProjectKey: "PROD", IssueType: "Bug", Summary: "boom",
	}); err != nil {
		t.Fatalf("create bug: %v", err)
	}

	before, err := repo.ListPendingChanges(profileID)
	if err != nil {
		t.Fatalf("list pending before: %v", err)
	}

	fake := newKiwiFakeBackend()
	eng := syncer.New(fake, repo)
	result, err := eng.CommitChanges(context.Background(), profileID, "PROD")
	if err != nil {
		t.Fatalf("CommitChanges: %v", err)
	}
	if len(result.Failed) != 0 {
		t.Fatalf("skipped buckets must not be reported as failures: %+v", result.Failed)
	}
	if len(fake.updateCalls) != 0 {
		t.Errorf("no backend writes expected, got UpdateIssue calls: %+v", fake.updateCalls)
	}

	// Every unsupported bucket must be reported as skipped.
	wantTypes := map[string]bool{
		"test_case":        false, // folder move (test_case/folder) and precondition_set both use test-level keys
		"precondition_set": false,
		"test_review":      false,
		"requirement_set":  false,
		"container_edit":   false,
		"bug_create":       false,
	}
	for _, s := range result.Skipped {
		if _, ok := wantTypes[s.EntityType]; ok {
			wantTypes[s.EntityType] = true
		}
		if s.Reason == "" {
			t.Errorf("skipped row %+v missing a reason", s)
		}
	}
	for typ, seen := range wantTypes {
		if !seen {
			t.Errorf("expected a skipped %s row, got skipped=%+v", typ, result.Skipped)
		}
	}

	// Nothing committed -> every pending row stays in place.
	after, err := repo.ListPendingChanges(profileID)
	if err != nil {
		t.Fatalf("list pending after: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("pending rows changed: before=%d after=%d (skipped rows must stay pending)", len(before), len(after))
	}
}
