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
	// SupportsIssueLinkTypes is deliberately NOT flipped by this task (see
	// adapter.go's Capabilities doc comment) even though the requirements
	// plugin was detected.
	if got := a.Capabilities().SupportsIssueLinkTypes; got {
		t.Error("Capabilities().SupportsIssueLinkTypes = true, want false: not wired in this task")
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
