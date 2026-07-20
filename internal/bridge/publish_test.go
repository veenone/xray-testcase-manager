package bridge_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"xray-test-manager/internal/backend"
	"xray-test-manager/internal/bridge"
)

// --- fakes -------------------------------------------------------------

// fakeHub is an in-memory bridge.HubReader: a workspace maps to a fixed test
// list, and each test key maps to a fixed step list.
type fakeHub struct {
	tests map[string][]bridge.HubTest
	steps map[string][]bridge.HubStep
}

func newFakeHub() *fakeHub {
	return &fakeHub{tests: map[string][]bridge.HubTest{}, steps: map[string][]bridge.HubStep{}}
}

func (h *fakeHub) ListHubTests(workspaceID string) ([]bridge.HubTest, error) {
	return h.tests[workspaceID], nil
}

func (h *fakeHub) ListHubSteps(workspaceID, testKey string) ([]bridge.HubStep, error) {
	return h.steps[workspaceID+"/"+testKey], nil
}

// fakeRefStore is an in-memory bridge.ExternalRefStore mirroring
// store.Store's real ExternalRef/PutExternalRef upsert semantics, so
// PublishTests's resumability logic can be exercised without a real SQLite
// store (the round-trip against the real store is covered separately in
// internal/store's own test).
type fakeRefStore struct {
	mu   sync.Mutex
	refs map[string]string
}

func newFakeRefStore() *fakeRefStore {
	return &fakeRefStore{refs: map[string]string{}}
}

func refKey(workspaceID, entityType, localID, connection string) string {
	return workspaceID + "|" + entityType + "|" + localID + "|" + connection
}

func (s *fakeRefStore) ExternalRef(workspaceID, entityType, localID, connection string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.refs[refKey(workspaceID, entityType, localID, connection)]
	return v, ok, nil
}

func (s *fakeRefStore) PutExternalRef(workspaceID, entityType, localID, connection, externalKey, versionToken string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refs[refKey(workspaceID, entityType, localID, connection)] = externalKey
	return nil
}

type stepCall struct {
	key, action, data, expected string
}

type createCall struct {
	projectKey, summary, description, priority string
	labels, components                         []string
}

type updateCall struct {
	key    string
	fields map[string]any
}

// fakeTarget is a minimal backend.Backend: only the methods PublishTests is
// expected to call are implemented. Every other method is left on the nil
// embedded interface, so a mis-routed call (e.g. GetTransitions) panics and
// fails the test loudly, mirroring the kiwiFakeBackend pattern in
// internal/syncer/kiwi_routing_commit_test.go.
type fakeTarget struct {
	backend.Backend
	caps backend.Capabilities

	mu          sync.Mutex
	createCalls []createCall
	stepCalls   []stepCall
	updateCalls []updateCall

	nextID      int
	failFor     map[string]error // summary -> error CreateTest returns instead of succeeding
	failStepFor map[string]error // target key -> error CreateTestStep returns instead of succeeding
}

func newFakeTarget(caps backend.Capabilities) *fakeTarget {
	return &fakeTarget{caps: caps, failFor: map[string]error{}, failStepFor: map[string]error{}}
}

func (f *fakeTarget) Capabilities() backend.Capabilities { return f.caps }

func (f *fakeTarget) CreateTest(ctx context.Context, projectKey, summary, description, priority string, labels, components []string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.failFor[summary]; ok {
		return "", err
	}
	f.nextID++
	key := fmt.Sprintf("T-%d", f.nextID)
	f.createCalls = append(f.createCalls, createCall{
		projectKey: projectKey, summary: summary, description: description,
		priority: priority, labels: labels, components: components,
	})
	return key, nil
}

func (f *fakeTarget) CreateTestStep(ctx context.Context, key, action, data, expected string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.failStepFor[key]; ok {
		return "", err
	}
	f.stepCalls = append(f.stepCalls, stepCall{key: key, action: action, data: data, expected: expected})
	return fmt.Sprintf("step-%d", len(f.stepCalls)), nil
}

// FieldsForJira is the identity mapping here — the fake doesn't need to
// mirror a real backend's native field names, only prove UpdateIssue is
// reached with whatever FieldsForJira produced.
func (f *fakeTarget) FieldsForJira(updates map[string]string) map[string]any {
	out := make(map[string]any, len(updates))
	for k, v := range updates {
		out[k] = v
	}
	return out
}

func (f *fakeTarget) UpdateIssue(ctx context.Context, key string, fields map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updateCalls = append(f.updateCalls, updateCall{key: key, fields: fields})
	return nil
}

func (f *fakeTarget) callCounts() (create, step, update int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.createCalls), len(f.stepCalls), len(f.updateCalls)
}

// settableCaps is a Kiwi-shaped Capabilities: settable status, inline-text
// steps.
func settableCaps() backend.Capabilities {
	return backend.Capabilities{
		Name:                        "kiwi",
		StepModel:                   "inline-text",
		StatusModel:                 "settable",
		SupportsWorkflowTransitions: false,
	}
}

// --- tests ---------------------------------------------------------------

func containsFailure(fails []bridge.PublishFailure, localKey string) bool {
	for _, f := range fails {
		if f.LocalKey == localKey {
			return true
		}
	}
	return false
}

func containsCreated(created []bridge.PublishedTest, localKey string) bool {
	for _, c := range created {
		if c.LocalKey == localKey {
			return true
		}
	}
	return false
}

// TestPublishTestsCreatesThenResumableRerunSkipsAll proves the core publish +
// resumability loop: N hub tests publish once (N CreateTest calls, N
// external_ref rows, N entries in Created), and re-running against the same
// refs store skips every one of them (AlleadyPublished == N, zero additional
// CreateTest calls) — the mechanism that makes a re-run and dual-publish safe.
func TestPublishTestsCreatesThenResumableRerunSkipsAll(t *testing.T) {
	hub := newFakeHub()
	hub.tests["ws1"] = []bridge.HubTest{
		{Key: "QA-1", Summary: "First", Priority: "High"},
		{Key: "QA-2", Summary: "Second", Priority: "Medium"},
		{Key: "QA-3", Summary: "Third", Priority: "Low"},
	}
	refs := newFakeRefStore()
	target := newFakeTarget(settableCaps())
	mapping := bridge.Mapping{StatusMap: map[string]string{}, StepMode: bridge.StepModePassthrough}
	pub := bridge.NewPublisher(target, "PROJ", hub, refs, mapping)

	result, err := pub.PublishTests(context.Background(), "ws1", "src", "tgt", nil)
	if err != nil {
		t.Fatalf("PublishTests: %v", err)
	}
	if len(result.Created) != 3 {
		t.Fatalf("Created = %d, want 3: %+v", len(result.Created), result.Created)
	}
	if len(result.Failed) != 0 {
		t.Fatalf("Failed = %+v, want none", result.Failed)
	}
	if len(result.AlreadyPublished) != 0 {
		t.Fatalf("AlreadyPublished = %+v, want none on first run", result.AlreadyPublished)
	}
	createCalls, _, _ := target.callCounts()
	if createCalls != 3 {
		t.Fatalf("CreateTest calls = %d, want 3", createCalls)
	}
	for _, key := range []string{"QA-1", "QA-2", "QA-3"} {
		if extKey, ok, err := refs.ExternalRef("ws1", "test", key, "tgt"); err != nil || !ok || extKey == "" {
			t.Errorf("external_ref for %s = (%q, %v, %v), want a recorded key", key, extKey, ok, err)
		}
	}

	// Re-run: resumable, nothing new created.
	result2, err := pub.PublishTests(context.Background(), "ws1", "src", "tgt", nil)
	if err != nil {
		t.Fatalf("PublishTests (rerun): %v", err)
	}
	if len(result2.AlreadyPublished) != 3 {
		t.Fatalf("rerun AlreadyPublished = %d, want 3: %+v", len(result2.AlreadyPublished), result2.AlreadyPublished)
	}
	if len(result2.Created) != 0 {
		t.Fatalf("rerun Created = %+v, want none", result2.Created)
	}
	createCallsAfter, _, _ := target.callCounts()
	if createCallsAfter != 3 {
		t.Fatalf("CreateTest calls after rerun = %d, want still 3 (0 new)", createCallsAfter)
	}
}

// TestPublishTestsIsolatesCreateTestFailure proves that one test's CreateTest
// error is recorded in Failed and does not abort the run — the other tests
// still publish.
func TestPublishTestsIsolatesCreateTestFailure(t *testing.T) {
	hub := newFakeHub()
	hub.tests["ws1"] = []bridge.HubTest{
		{Key: "QA-1", Summary: "Good one"},
		{Key: "QA-2", Summary: "Boom"},
		{Key: "QA-3", Summary: "Also good"},
	}
	refs := newFakeRefStore()
	target := newFakeTarget(settableCaps())
	target.failFor["Boom"] = errors.New("target rejected the test")
	mapping := bridge.Mapping{StatusMap: map[string]string{}, StepMode: bridge.StepModePassthrough}
	pub := bridge.NewPublisher(target, "PROJ", hub, refs, mapping)

	result, err := pub.PublishTests(context.Background(), "ws1", "src", "tgt", nil)
	if err != nil {
		t.Fatalf("PublishTests: %v", err)
	}
	if !containsFailure(result.Failed, "QA-2") {
		t.Errorf("Failed = %+v, want it to contain QA-2", result.Failed)
	}
	if len(result.Failed) != 1 {
		t.Errorf("Failed = %+v, want exactly 1 failure", result.Failed)
	}
	if !containsCreated(result.Created, "QA-1") || !containsCreated(result.Created, "QA-3") {
		t.Errorf("Created = %+v, want QA-1 and QA-3 to have published despite QA-2 failing", result.Created)
	}
	if _, ok, _ := refs.ExternalRef("ws1", "test", "QA-2", "tgt"); ok {
		t.Errorf("QA-2 must not have an external_ref after a failed publish")
	}
}

// TestPublishTestsRecordsRefBeforeStepsSoPartialFailureIsResumable proves the
// fix for the resumability gap: when CreateTest succeeds but a downstream
// step write fails (the deterministic case for a real Kiwi target, whose
// CreateTestStep returns ErrUnsupported), the external_ref for the test MUST
// already be recorded — target test exists ⇔ external_ref exists — so the
// failure is reported with its TargetKey (created-but-incomplete, not
// "nothing happened"), and a second PublishTests run against the same refs
// store skips the test (AlreadyPublished, not Created or Failed again) and
// makes zero additional CreateTest or CreateTestStep calls: no orphaned
// duplicate accumulates on retry.
func TestPublishTestsRecordsRefBeforeStepsSoPartialFailureIsResumable(t *testing.T) {
	hub := newFakeHub()
	hub.tests["ws1"] = []bridge.HubTest{{Key: "QA-1", Summary: "Partial"}}
	hub.steps["ws1/QA-1"] = []bridge.HubStep{{Action: "Open login", Expected: "Form shown"}}
	refs := newFakeRefStore()
	target := newFakeTarget(settableCaps())
	target.failStepFor["T-1"] = errors.New("step create: unsupported by this backend")
	mapping := bridge.Mapping{StatusMap: map[string]string{}, StepMode: bridge.StepModePassthrough}
	pub := bridge.NewPublisher(target, "PROJ", hub, refs, mapping)

	result, err := pub.PublishTests(context.Background(), "ws1", "src", "tgt", nil)
	if err != nil {
		t.Fatalf("PublishTests: %v", err)
	}
	if len(result.Created) != 0 {
		t.Fatalf("Created = %+v, want none (steps failed, so publish is incomplete)", result.Created)
	}
	if len(result.Failed) != 1 {
		t.Fatalf("Failed = %+v, want exactly 1 failure", result.Failed)
	}
	failure := result.Failed[0]
	if failure.LocalKey != "QA-1" {
		t.Errorf("failure.LocalKey = %q, want QA-1", failure.LocalKey)
	}
	if failure.TargetKey != "T-1" {
		t.Errorf("failure.TargetKey = %q, want T-1 (proves the failure message can say the target test WAS created)", failure.TargetKey)
	}
	if !strings.Contains(failure.Error, "T-1") {
		t.Errorf("failure.Error = %q, want it to mention the target key T-1", failure.Error)
	}

	// The critical assertion: external_ref IS recorded even though publish
	// failed, because CreateTest succeeded before the step write blew up.
	extKey, ok, err := refs.ExternalRef("ws1", "test", "QA-1", "tgt")
	if err != nil || !ok || extKey != "T-1" {
		t.Fatalf("external_ref for QA-1 = (%q, %v, %v), want (\"T-1\", true, nil)", extKey, ok, err)
	}

	createCalls, stepCalls, _ := target.callCounts()
	if createCalls != 1 {
		t.Fatalf("CreateTest calls after first run = %d, want 1", createCalls)
	}
	if stepCalls != 0 {
		t.Fatalf("CreateTestStep calls recorded after first run = %d, want 0 (the only attempt failed)", stepCalls)
	}

	// Second run: must skip QA-1 entirely — no duplicate CreateTest, no retry
	// of the failed step.
	result2, err := pub.PublishTests(context.Background(), "ws1", "src", "tgt", nil)
	if err != nil {
		t.Fatalf("PublishTests (rerun): %v", err)
	}
	if len(result2.AlreadyPublished) != 1 || result2.AlreadyPublished[0] != "QA-1" {
		t.Fatalf("rerun AlreadyPublished = %+v, want [QA-1]", result2.AlreadyPublished)
	}
	if len(result2.Created) != 0 {
		t.Errorf("rerun Created = %+v, want none", result2.Created)
	}
	if len(result2.Failed) != 0 {
		t.Errorf("rerun Failed = %+v, want none (already-published tests are skipped, not retried)", result2.Failed)
	}
	createCallsAfter, stepCallsAfter, _ := target.callCounts()
	if createCallsAfter != 1 {
		t.Fatalf("CreateTest calls after rerun = %d, want still 1 (0 new — no duplicate)", createCallsAfter)
	}
	if stepCallsAfter != 0 {
		t.Fatalf("CreateTestStep calls after rerun = %d, want still 0 (the failed step is not retried)", stepCallsAfter)
	}
}

// TestPublishTestsStepMode proves the step-mode branching: "flatten" produces
// ONE CreateTestStep call whose text carries every source step joined
// together; "passthrough" produces one CreateTestStep call per source step,
// each carrying its own action/data/expected untouched.
func TestPublishTestsStepMode(t *testing.T) {
	steps := []bridge.HubStep{
		{Action: "Open login", Expected: "Form shown"},
		{Action: "Submit credentials", Data: "user=admin", Expected: "Dashboard shown"},
	}

	t.Run("flatten", func(t *testing.T) {
		hub := newFakeHub()
		hub.tests["ws1"] = []bridge.HubTest{{Key: "QA-1", Summary: "T"}}
		hub.steps["ws1/QA-1"] = steps
		refs := newFakeRefStore()
		target := newFakeTarget(settableCaps())
		mapping := bridge.Mapping{StatusMap: map[string]string{}, StepMode: bridge.StepModeFlatten}
		pub := bridge.NewPublisher(target, "PROJ", hub, refs, mapping)

		if _, err := pub.PublishTests(context.Background(), "ws1", "src", "tgt", nil); err != nil {
			t.Fatalf("PublishTests: %v", err)
		}
		_, stepCalls, _ := target.callCounts()
		if stepCalls != 1 {
			t.Fatalf("CreateTestStep calls = %d, want 1 (flatten)", stepCalls)
		}
		joined := target.stepCalls[0].action
		if !containsAll(joined, "Open login", "Form shown", "Submit credentials", "user=admin", "Dashboard shown") {
			t.Errorf("flattened text = %q, want it to contain every step's content", joined)
		}
	})

	t.Run("passthrough", func(t *testing.T) {
		hub := newFakeHub()
		hub.tests["ws1"] = []bridge.HubTest{{Key: "QA-1", Summary: "T"}}
		hub.steps["ws1/QA-1"] = steps
		refs := newFakeRefStore()
		target := newFakeTarget(settableCaps())
		mapping := bridge.Mapping{StatusMap: map[string]string{}, StepMode: bridge.StepModePassthrough}
		pub := bridge.NewPublisher(target, "PROJ", hub, refs, mapping)

		if _, err := pub.PublishTests(context.Background(), "ws1", "src", "tgt", nil); err != nil {
			t.Fatalf("PublishTests: %v", err)
		}
		_, stepCalls, _ := target.callCounts()
		if stepCalls != 2 {
			t.Fatalf("CreateTestStep calls = %d, want 2 (passthrough, one per step)", stepCalls)
		}
		if target.stepCalls[0].action != "Open login" || target.stepCalls[1].action != "Submit credentials" {
			t.Errorf("passthrough calls = %+v, want each step's own action preserved in order", target.stepCalls)
		}
		if target.stepCalls[1].data != "user=admin" {
			t.Errorf("passthrough second call data = %q, want %q", target.stepCalls[1].data, "user=admin")
		}
	})
}

// TestPublishTestsWritesStatusViaSettablePath proves that on a settable
// target (SupportsWorkflowTransitions=false) the mapped status is written via
// FieldsForJira+UpdateIssue — the same settable-status pattern
// syncer/commit.go's commit path uses.
func TestPublishTestsWritesStatusViaSettablePath(t *testing.T) {
	hub := newFakeHub()
	hub.tests["ws1"] = []bridge.HubTest{{Key: "QA-1", Summary: "T", Status: "Open"}}
	refs := newFakeRefStore()
	target := newFakeTarget(settableCaps())
	mapping := bridge.Mapping{
		StatusMap: map[string]string{"Open": "CONFIRMED"},
		StepMode:  bridge.StepModePassthrough,
	}
	pub := bridge.NewPublisher(target, "PROJ", hub, refs, mapping)

	if _, err := pub.PublishTests(context.Background(), "ws1", "src", "tgt", nil); err != nil {
		t.Fatalf("PublishTests: %v", err)
	}
	if len(target.updateCalls) != 1 {
		t.Fatalf("UpdateIssue calls = %d, want 1 (settable status write)", len(target.updateCalls))
	}
	if got := target.updateCalls[0].fields["status"]; got != "CONFIRMED" {
		t.Errorf("status field = %v, want CONFIRMED", got)
	}
}

// TestPublishTestsSkipsStatusWriteForWorkflowTarget proves the converse: when
// the target IS workflow-driven (SupportsWorkflowTransitions=true), publish
// does not attempt a status write at all — out of scope for B5 per the brief
// (resolving a valid transition off a freshly-created issue is deferred).
func TestPublishTestsSkipsStatusWriteForWorkflowTarget(t *testing.T) {
	hub := newFakeHub()
	hub.tests["ws1"] = []bridge.HubTest{{Key: "QA-1", Summary: "T", Status: "Open"}}
	refs := newFakeRefStore()
	caps := settableCaps()
	caps.SupportsWorkflowTransitions = true
	target := newFakeTarget(caps)
	mapping := bridge.Mapping{
		StatusMap: map[string]string{"Open": "In Progress"},
		StepMode:  bridge.StepModePassthrough,
	}
	pub := bridge.NewPublisher(target, "PROJ", hub, refs, mapping)

	result, err := pub.PublishTests(context.Background(), "ws1", "src", "tgt", nil)
	if err != nil {
		t.Fatalf("PublishTests: %v", err)
	}
	if len(result.Failed) != 0 {
		t.Fatalf("Failed = %+v, want none", result.Failed)
	}
	if len(target.updateCalls) != 0 {
		t.Errorf("UpdateIssue calls = %d, want 0 (workflow target skips the status write)", len(target.updateCalls))
	}
	if len(result.Created) != 1 {
		t.Errorf("Created = %+v, want the test to still publish (minus status)", result.Created)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
