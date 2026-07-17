package kiwi

import (
	"context"
	"errors"
	"testing"
)

// TestClientCallRoundTrip exercises the transport: marshal -> POST
// /json-rpc/ -> decode "result" into out. Spec §1.1.
func TestClientCallRoundTrip(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("Auth.login", "sess") // lazy login runs before the first call
	mock.handleResult("TestCase.filter", []map[string]any{
		{"id": 42, "summary": "Login with valid creds"},
	})
	srv := mock.start()
	defer srv.Close()

	c := NewClient(srv.URL, "alice:secret")

	var out []map[string]any
	if err := c.call(context.Background(), "TestCase.filter", []any{map[string]any{}}, &out); err != nil {
		t.Fatalf("call: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 result, got %d", len(out))
	}
	if out[0]["summary"] != "Login with valid creds" {
		t.Fatalf("unexpected result: %#v", out[0])
	}
	// Lazy login first, then the actual call.
	if len(mock.requests) != 2 || mock.requests[0].Method != "Auth.login" || mock.requests[1].Method != "TestCase.filter" {
		t.Fatalf("expected [Auth.login, TestCase.filter], got %#v", mock.requests)
	}
}

// TestSessionLoginSeedsCookie asserts the sessionLogin Authenticator calls
// Auth.login, then replays the returned session id as a cookie on later
// requests — the mechanism that keeps a session alive across calls (spec
// §1.2 Option A, §9.1a).
func TestSessionLoginSeedsCookie(t *testing.T) {
	const wantSession = "abc123sessionkeydef456"

	mock := newMockRPCServer(t)
	mock.handleResult("Auth.login", wantSession)
	mock.handleResult("User.filter", []map[string]any{{"username": "alice"}})
	srv := mock.start()
	defer srv.Close()

	c := NewClient(srv.URL, "alice:secret")
	ctx := context.Background()

	// The first authenticated call lazily logs in (Auth.login) and the
	// Authenticator seeds the sessionid cookie, which is then replayed on
	// this same request's downstream calls.
	var out []map[string]any
	if err := c.call(ctx, "User.filter", []any{map[string]any{}}, &out); err != nil {
		t.Fatalf("call after login: %v", err)
	}

	if len(mock.requests) != 2 {
		t.Fatalf("expected 2 recorded requests (login + filter), got %d", len(mock.requests))
	}
	loginReq, filterReq := mock.requests[0], mock.requests[1]
	if loginReq.Method != "Auth.login" || filterReq.Method != "User.filter" {
		t.Fatalf("unexpected request order: %#v", mock.requests)
	}

	found := false
	for _, ck := range filterReq.Cookies {
		if ck.Name == "sessionid" && ck.Value == wantSession {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected sessionid cookie %q on the post-login request, got cookies %#v", wantSession, filterReq.Cookies)
	}
}

// TestKiwiRPCErrorDecodeAndMethodNotFound asserts a JSON-RPC error object
// decodes into a *kiwiRPCError with the right Code/Message, and that
// isMethodNotFound only fires for code -32601. Spec §1.3.
func TestKiwiRPCErrorDecodeAndMethodNotFound(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("Auth.login", "sess") // lazy login precedes the probed calls
	mock.handleError("Bug.report", 403, "PermissionDenied: cannot create bugs")
	// "Requirement.filter" is intentionally left unregistered so the mock
	// falls back to -32601, mirroring an absent plugin RPC.
	srv := mock.start()
	defer srv.Close()

	c := NewClient(srv.URL, "alice:secret")
	ctx := context.Background()

	err := c.call(ctx, "Bug.report", []any{map[string]any{}}, nil)
	if err == nil {
		t.Fatal("expected an error from Bug.report")
	}
	var rpcErr *kiwiRPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected a *kiwiRPCError, got %T: %v", err, err)
	}
	if rpcErr.Code != 403 || rpcErr.Message != "PermissionDenied: cannot create bugs" {
		t.Fatalf("unexpected kiwiRPCError: %+v", rpcErr)
	}
	if isMethodNotFound(err) {
		t.Fatal("a 403 PermissionDenied must not be classified as method-not-found")
	}

	err2 := c.call(ctx, "Requirement.filter", []any{map[string]any{}}, nil)
	if err2 == nil {
		t.Fatal("expected an error from the unregistered Requirement.filter")
	}
	if !isMethodNotFound(err2) {
		t.Fatalf("expected isMethodNotFound(true) for an unregistered method, err=%v", err2)
	}
}

// TestMethodExistsProbe covers the plugin-detection mechanism itself (not
// yet wired to Capabilities — that's P4.3). Spec §4.3.
func TestMethodExistsProbe(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("Auth.login", "sess") // lazy login precedes the probed calls
	mock.handleResult("Requirement.filter", []map[string]any{})
	mock.handleError("ReviewRequest.filter", 403, "PermissionDenied")
	// "AnotherPlugin.filter" left unregistered -> -32601.
	srv := mock.start()
	defer srv.Close()

	c := NewClient(srv.URL, "alice:secret")
	ctx := context.Background()

	present, err := methodExists(ctx, c, "Requirement.filter", []any{map[string]any{}})
	if err != nil || !present {
		t.Fatalf("expected present=true, err=nil for a registered method; got present=%v err=%v", present, err)
	}

	absent, err := methodExists(ctx, c, "AnotherPlugin.filter", []any{map[string]any{}})
	if err != nil || absent {
		t.Fatalf("expected present=false, err=nil for -32601; got present=%v err=%v", absent, err)
	}

	degraded, err := methodExists(ctx, c, "ReviewRequest.filter", []any{map[string]any{}})
	if err == nil || !degraded {
		t.Fatalf("expected present=true, err!=nil for a non-32601 failure (installed but degraded); got present=%v err=%v", degraded, err)
	}
}
