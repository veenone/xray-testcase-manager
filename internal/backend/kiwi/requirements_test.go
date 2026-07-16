package kiwi

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"xray-test-manager/internal/backend"
)

// This file covers the P4.3 deliverables: plugin detection during
// TestConnection (both ways), the Capabilities() flip it drives, the
// ListRequirements plugin-backed mapping, and the (cached-but-unexposed)
// review-plugin detection flag.

// loginOK registers the two calls every TestConnection needs to succeed
// (Auth.login + User.filter) so tests can focus on the plugin-detection
// probes that run afterward.
func loginOK(mock *mockRPCServer) {
	mock.handleResult("Auth.login", "sess")
	mock.handleResult("User.filter", []map[string]any{{"username": "alice"}})
}

// TestPluginDetectionRequirementsPresent: a mock WITH Requirement.filter
// registered flips SupportsRequirementObjects true after TestConnection,
// and also flips the cached (unexposed) hasReviewPlugin false since
// ReviewRequest.filter is left unregistered (-> -32601, per mockRPCServer's
// default fallback).
func TestPluginDetectionRequirementsPresent(t *testing.T) {
	mock := newMockRPCServer(t)
	loginOK(mock)
	mock.handleResult("Requirement.filter", []map[string]any{})
	// ReviewRequest.filter intentionally left unregistered -> -32601 -> absent.
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	if _, err := a.TestConnection(context.Background()); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	if !a.hasRequirementsPlugin {
		t.Error("expected hasRequirementsPlugin=true after a successful Requirement.filter probe")
	}
	if a.hasReviewPlugin {
		t.Error("expected hasReviewPlugin=false: ReviewRequest.filter was never registered (-32601)")
	}
	if got := a.Capabilities().SupportsRequirementObjects; !got {
		t.Error("Capabilities().SupportsRequirementObjects = false, want true after requirements-plugin detection")
	}
	// SupportsIssueLinkTypes flips true alongside SupportsRequirementObjects
	// (spec §4.2's full requirements-plugin delta).
	if got := a.Capabilities().SupportsIssueLinkTypes; !got {
		t.Error("Capabilities().SupportsIssueLinkTypes = false, want true after requirements-plugin detection")
	}
	// ListIssueLinkTypes serves the static vocabulary once the plugin is
	// detected (spec §3.8).
	lts, err := a.ListIssueLinkTypes(context.Background())
	if err != nil {
		t.Fatalf("ListIssueLinkTypes: %v", err)
	}
	wantLTs := []string{"verifies", "validates", "derives-from", "related"}
	if !reflect.DeepEqual(lts, wantLTs) {
		t.Errorf("ListIssueLinkTypes = %#v, want %#v", lts, wantLTs)
	}
}

// TestPluginDetectionAbsent: a mock WITHOUT either plugin method (both fall
// through to the mock's default -32601) leaves both flags false and
// SupportsRequirementObjects false, and ListRequirements returns
// empty+nil, not an error.
func TestPluginDetectionAbsent(t *testing.T) {
	mock := newMockRPCServer(t)
	loginOK(mock)
	// Neither Requirement.filter nor ReviewRequest.filter registered.
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	if _, err := a.TestConnection(context.Background()); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	if a.hasRequirementsPlugin {
		t.Error("expected hasRequirementsPlugin=false: Requirement.filter was never registered (-32601)")
	}
	if a.hasReviewPlugin {
		t.Error("expected hasReviewPlugin=false: ReviewRequest.filter was never registered (-32601)")
	}
	if got := a.Capabilities().SupportsRequirementObjects; got {
		t.Error("Capabilities().SupportsRequirementObjects = true, want false when the plugin is absent")
	}

	reqs, links, err := a.ListRequirements(context.Background(), "DEMO", nil, nil)
	if err != nil {
		t.Fatalf("ListRequirements: expected nil error when the plugin is absent, got %v", err)
	}
	if len(reqs) != 0 || len(links) != 0 {
		t.Fatalf("ListRequirements: expected empty, got reqs=%#v links=%#v", reqs, links)
	}
}

// TestPluginDetectionBeforeTestConnection: Capabilities() called before
// TestConnection ever ran must report the base (plugin-off) caps — the
// brief's explicit "safe default" requirement.
func TestPluginDetectionBeforeTestConnection(t *testing.T) {
	a := New("http://example.invalid", "alice:secret")
	if got := a.Capabilities().SupportsRequirementObjects; got {
		t.Error("Capabilities() before TestConnection should report SupportsRequirementObjects=false")
	}
}

// TestPluginDetectionDegradedStillPresent: a non-32601 RPC error (e.g.
// PermissionDenied) from the probe means "installed, degraded" per spec
// §4.3 -- the flag should still flip true, not be treated as absent.
func TestPluginDetectionDegradedStillPresent(t *testing.T) {
	mock := newMockRPCServer(t)
	loginOK(mock)
	mock.handleError("Requirement.filter", 403, "PermissionDenied: no access to requirements")
	mock.handleError("ReviewRequest.filter", 403, "PermissionDenied: no access to reviews")
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	if _, err := a.TestConnection(context.Background()); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	if !a.hasRequirementsPlugin {
		t.Error("expected hasRequirementsPlugin=true: a 403 proves the method IS registered (installed, degraded)")
	}
	if !a.hasReviewPlugin {
		t.Error("expected hasReviewPlugin=true: a 403 proves the method IS registered (installed, degraded)")
	}
}

// TestPluginDetectionTransportErrorLeavesFlagOff is the safety-critical
// fallback: if the plugin probe hits a RAW TRANSPORT error (HTTP 500 with a
// non-JSON body here — could equally be a closed connection or truncated
// response), the error carries NO signal about whether the method is
// registered, so detectPlugin must default the flag OFF, NOT misread it as
// "installed". Login + User.filter succeed, so TestConnection itself still
// returns OK — only the plugin probe transport-fails. Asserts
// Capabilities().SupportsRequirementObjects/SupportsIssueLinkTypes stay
// false and ListIssueLinkTypes stays empty.
func TestPluginDetectionTransportErrorLeavesFlagOff(t *testing.T) {
	mock := newMockRPCServer(t)
	loginOK(mock)
	// The requirements probe transport-fails; the review probe is simply
	// unregistered (-32601 -> absent), keeping the setup focused.
	mock.handleTransportFail("Requirement.filter")
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	u, err := a.TestConnection(context.Background())
	if err != nil {
		t.Fatalf("TestConnection should still succeed (login/user-fetch are fine): %v", err)
	}
	if u == nil {
		t.Fatal("expected a non-nil user from a successful TestConnection")
	}
	if a.hasRequirementsPlugin {
		t.Error("expected hasRequirementsPlugin=false: a raw transport error must NOT be read as installed")
	}
	caps := a.Capabilities()
	if caps.SupportsRequirementObjects {
		t.Error("Capabilities().SupportsRequirementObjects = true, want false after a transport-errored probe")
	}
	if caps.SupportsIssueLinkTypes {
		t.Error("Capabilities().SupportsIssueLinkTypes = true, want false after a transport-errored probe")
	}
	if lts, err := a.ListIssueLinkTypes(context.Background()); err != nil || len(lts) != 0 {
		t.Errorf("ListIssueLinkTypes: expected (empty, nil) after a transport-errored probe, got (%v, %v)", lts, err)
	}
}

// TestListRequirementsMapsFilterAndCoverage exercises the full plugin read
// path: Requirement.filter({}) for the registry, then Requirement.coverage
// per requirement for links, against a canned link_types shape (the FLAGGED
// inferred part of this task -- see requirements.go's doc comment).
func TestListRequirementsMapsFilterAndCoverage(t *testing.T) {
	mock := newMockRPCServer(t)
	loginOK(mock)
	mock.handleResult("Requirement.filter", []map[string]any{
		// Registry-row fixture per spec §3.8/§8.1 field list (id, title,
		// status, priority -- the fields this task's DTO decodes).
		{"id": 11, "identifier": "REQ-001", "title": "User can log in", "status": "approved", "priority": "P1", "level": "SYS"},
	})
	mock.handle("Requirement.coverage", func(params []json.RawMessage) (any, *rpcErrorObj) {
		return map[string]any{
			"id": 11, "identifier": "REQ-001", "link_count": 2, "suspect_count": 0,
			"link_types": map[string]any{
				"verifies": []int{42, 43},
			},
		}, nil
	})
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	if _, err := a.TestConnection(context.Background()); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}

	reqs, links, err := a.ListRequirements(context.Background(), "DEMO", nil, nil)
	if err != nil {
		t.Fatalf("ListRequirements: %v", err)
	}

	wantReqs := []backend.Requirement{
		{Key: "11", ProjectKey: "", IssueType: "requirement", Summary: "User can log in", Status: "approved", Priority: "P1"},
	}
	if !reflect.DeepEqual(reqs, wantReqs) {
		t.Fatalf("Requirements = %#v, want %#v", reqs, wantReqs)
	}

	sort.Slice(links, func(i, j int) bool { return links[i].TestKey < links[j].TestKey })
	wantLinks := []backend.RequirementLink{
		{TestKey: "42", RequirementKey: "11", LinkID: "11-42"},
		{TestKey: "43", RequirementKey: "11", LinkID: "11-43"},
	}
	if !reflect.DeepEqual(links, wantLinks) {
		t.Fatalf("RequirementLinks = %#v, want %#v", links, wantLinks)
	}
}

// TestListRequirementsCoverageDegradesOnUnknownLinkTypesShape asserts the
// FLAGGED inference degrades safely: if link_types turns out to be pure
// per-type counts (no case ids), ListRequirements still returns the
// Requirement objects with zero links, not an error.
func TestListRequirementsCoverageDegradesOnUnknownLinkTypesShape(t *testing.T) {
	mock := newMockRPCServer(t)
	loginOK(mock)
	mock.handleResult("Requirement.filter", []map[string]any{
		{"id": 11, "title": "User can log in", "status": "approved", "priority": "P1"},
	})
	mock.handle("Requirement.coverage", func(params []json.RawMessage) (any, *rpcErrorObj) {
		return map[string]any{
			"id": 11, "link_count": 2, "suspect_count": 0,
			"link_types": map[string]any{
				"verifies": 2, // bare count, not a case-id list
			},
		}, nil
	})
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	if _, err := a.TestConnection(context.Background()); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}

	reqs, links, err := a.ListRequirements(context.Background(), "DEMO", nil, nil)
	if err != nil {
		t.Fatalf("ListRequirements: expected nil error on an unrecognized link_types shape, got %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 requirement, got %#v", reqs)
	}
	if len(links) != 0 {
		t.Fatalf("expected 0 links (degraded, not invented), got %#v", links)
	}
}

// countCalls counts how many recorded requests on mock invoked method,
// letting a test assert on the exact number of probe/data round trips
// ensureDetected and ListRequirements made.
func countCalls(mock *mockRPCServer, method string) int {
	n := 0
	for _, r := range mock.requests {
		if r.Method == method {
			n++
		}
	}
	return n
}

// TestListRequirementsLazyDetectionWithoutTestConnection is the P4.5
// regression test: it proves ListRequirements returns real requirements the
// FIRST time it is called on a fresh Adapter that never had TestConnection
// called on it -- exactly the shape of a real sync, where app.go's backend
// factory builds a stateless, fresh Adapter per call and the sync path goes
// straight to ListRequirements. Before this task, hasRequirementsPlugin
// stayed at its unset false zero-value in this scenario and ListRequirements
// silently returned (nil, nil, nil) even with the requirements plugin
// present on the server -- this test would have failed against the P4.3
// code and passes now that ListRequirements calls ensureDetected itself.
func TestListRequirementsLazyDetectionWithoutTestConnection(t *testing.T) {
	mock := newMockRPCServer(t)
	// Deliberately NOT calling loginOK/TestConnection here.
	mock.handleResult("Requirement.filter", []map[string]any{
		{"id": 11, "title": "User can log in", "status": "approved", "priority": "P1"},
	})
	mock.handle("Requirement.coverage", func(params []json.RawMessage) (any, *rpcErrorObj) {
		return map[string]any{
			"id": 11, "link_count": 1, "suspect_count": 0,
			"link_types": map[string]any{"verifies": []int{42}},
		}, nil
	})
	// ReviewRequest.filter intentionally left unregistered -> -32601 -> absent.
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	reqs, links, err := a.ListRequirements(context.Background(), "DEMO", nil, nil)
	if err != nil {
		t.Fatalf("ListRequirements without a prior TestConnection: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("ListRequirements returned %d requirements without a prior TestConnection call, want 1 (lazy self-detection failed)", len(reqs))
	}
	if len(links) != 1 {
		t.Fatalf("ListRequirements returned %d coverage links, want 1", len(links))
	}
	if !a.hasRequirementsPlugin {
		t.Error("expected hasRequirementsPlugin=true after ListRequirements self-detected the plugin")
	}
}

// TestEnsureDetectedIsIdempotent asserts the detection probes run exactly
// once across two ListRequirements calls on the same Adapter, not once per
// call -- the concurrency-safe idempotency ensureDetected is required to
// provide (P4.5 brief). Requirement.filter is used BOTH as the detection
// probe (empty-params call) AND the real per-call data fetch, so its count
// is probe(1) + data(1) + data(1) = 3 across two ListRequirements calls;
// ReviewRequest.filter is ONLY ever the probe, so it must appear exactly
// once total. A non-idempotent ensureDetected would double both counts.
func TestEnsureDetectedIsIdempotent(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("Requirement.filter", []map[string]any{
		{"id": 11, "title": "User can log in", "status": "approved", "priority": "P1"},
	})
	mock.handleResult("Requirement.coverage", map[string]any{
		"id": 11, "link_count": 0, "suspect_count": 0, "link_types": map[string]any{},
	})
	// ReviewRequest.filter intentionally left unregistered -> -32601 -> confirmed absent.
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	if _, _, err := a.ListRequirements(context.Background(), "DEMO", nil, nil); err != nil {
		t.Fatalf("ListRequirements (1st call): %v", err)
	}
	if _, _, err := a.ListRequirements(context.Background(), "DEMO", nil, nil); err != nil {
		t.Fatalf("ListRequirements (2nd call): %v", err)
	}

	if got := countCalls(mock, "Requirement.filter"); got != 3 {
		t.Errorf("Requirement.filter called %d times across 2 ListRequirements calls, want 3 (1 detection probe + 2 data fetches) -- ensureDetected is not idempotent", got)
	}
	if got := countCalls(mock, "ReviewRequest.filter"); got != 1 {
		t.Errorf("ReviewRequest.filter called %d times across 2 ListRequirements calls, want 1 (probe-only, and only on the first call) -- ensureDetected is not idempotent", got)
	}
	if !a.detectDone {
		t.Error("expected detectDone=true after a confirmed detection round")
	}
}
