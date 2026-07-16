package kiwi

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"xray-test-manager/internal/backend"
)

func newTestAdapter(t *testing.T, mock *mockRPCServer) (*Adapter, func()) {
	t.Helper()
	srv := mock.start()
	return New(srv.URL, "alice:secret"), srv.Close
}

// TestConnectionMapsUser exercises the real login + User.filter mapping
// path. Fixture fields follow standard Django User fields (spec §3.1).
func TestTestConnectionMapsUser(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("Auth.login", "abc123sessionkeydef456")
	mock.handleResult("User.filter", []map[string]any{
		{
			"id":         7,
			"username":   "alice",
			"first_name": "Alice",
			"last_name":  "Doe",
			"email":      "alice@example.com",
			"is_active":  true,
		},
	})
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	u, err := a.TestConnection(context.Background())
	if err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	want := &backend.User{Name: "alice", DisplayName: "Alice Doe", Email: "alice@example.com"}
	if !reflect.DeepEqual(u, want) {
		t.Fatalf("TestConnection user = %#v, want %#v", u, want)
	}
}

// TestTestConnectionFallsBackToUsername covers the no-first/last-name case.
func TestTestConnectionFallsBackToUsername(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleResult("Auth.login", "sess")
	mock.handleResult("User.filter", []map[string]any{
		{"username": "bob", "first_name": "", "last_name": "", "email": "bob@example.com"},
	})
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	u, err := a.TestConnection(context.Background())
	if err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	if u.DisplayName != "bob" {
		t.Fatalf("expected DisplayName to fall back to username %q, got %q", "bob", u.DisplayName)
	}
}

// TestTestConnectionLoginFailurePropagates asserts a login error surfaces
// without a partial/zero-value user being returned.
func TestTestConnectionLoginFailurePropagates(t *testing.T) {
	mock := newMockRPCServer(t)
	mock.handleError("Auth.login", 401, "Invalid credentials")
	a, closeFn := newTestAdapter(t, mock)
	defer closeFn()

	u, err := a.TestConnection(context.Background())
	if err == nil {
		t.Fatal("expected an error from a failed login")
	}
	if u != nil {
		t.Fatalf("expected a nil user on login failure, got %#v", u)
	}
	var rpcErr *kiwiRPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != 401 {
		t.Fatalf("expected a *kiwiRPCError{Code:401}, got %v", err)
	}
}

// TestCapabilitiesBaseValues pins the exact base-Kiwi Capabilities values
// from spec §4.1 so P4.3 can diff plugin deltas against a known baseline.
func TestCapabilitiesBaseValues(t *testing.T) {
	a := New("http://example.invalid", "alice:secret")
	got := a.Capabilities()
	want := backend.Capabilities{
		Name:                        "kiwi",
		IDStyle:                     "numeric",
		SupportsJQLScope:            false,
		StepModel:                   "inline-text",
		SupportsTestTypes:           true,
		SupportsFolders:             false,
		SupportsPreconditionObjects: false,
		SupportsRequirementObjects:  false,
		SupportsIssueLinkTypes:      false,
		SupportsEnvironments:        true,
		SupportsContainers:          true,
		ContainerKinds:              []string{backend.KindTestPlan, backend.KindTestExec},
		SupportsTestRuns:            true,
		StatusModel:                 "settable",
		SupportsWorkflowTransitions: false,
		SupportsBugCreation:         false,
		SupportsBugLinks:            true,
		SupportsTags:                true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Capabilities() = %#v, want %#v", got, want)
	}
}

// TestIsDemoAndSetRequirementLinkType covers the two other "local" methods
// implemented in this task.
func TestIsDemoAndSetRequirementLinkType(t *testing.T) {
	a := New("http://example.invalid", "alice:secret")
	if a.IsDemo() {
		t.Fatal("IsDemo() should be false for a non-kiwi-demo URL")
	}
	// Must not panic; genuinely a no-op today.
	a.SetRequirementLinkType("verifies")
}

// TestRemoteAhead table-drives the content-hash ordering rule (spec §5)
// including the conservative empty-token handling documented on the method.
func TestRemoteAhead(t *testing.T) {
	a := New("http://example.invalid", "alice:secret")
	cases := []struct {
		name         string
		base, remote backend.VersionToken
		want         bool
	}{
		{"equal tokens -> not ahead", "hash1", "hash1", false},
		{"different tokens -> ahead", "hash1", "hash2", true},
		{"empty base -> not ahead", "", "hash2", false},
		{"empty remote -> not ahead", "hash1", "", false},
		{"both empty -> not ahead", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := a.RemoteAhead(tc.base, tc.remote); got != tc.want {
				t.Errorf("RemoteAhead(%q, %q) = %v, want %v", tc.base, tc.remote, got, tc.want)
			}
		})
	}
}

// TestUnimplementedMethodsReturnErrUnsupported spot-checks a representative
// sample of the P4.3-and-later stubs across signature shapes (read, write,
// 2-return, 3-return, 4-return) to confirm they surface backend.ErrUnsupported
// rather than a silent zero value. SearchTestsPage/GetTestFields/
// ListStatuses/ListContainers/RemoteVersion moved to read_test.go once P4.2
// implemented them for real.
func TestUnimplementedMethodsReturnErrUnsupported(t *testing.T) {
	a := New("http://example.invalid", "alice:secret")
	ctx := context.Background()

	if err := a.UpdateIssue(ctx, "1", map[string]any{}); !errors.Is(err, backend.ErrUnsupported) {
		t.Errorf("UpdateIssue: expected ErrUnsupported, got %v", err)
	}
	if _, _, ok, err := a.ExecTypeFieldValue(ctx, "Manual"); err != nil || ok {
		t.Errorf("ExecTypeFieldValue: expected (ok=false, err=nil), got (ok=%v, err=%v)", ok, err)
	}
	if err := a.CreateFolder(ctx, "PROJ", "", "New"); !errors.Is(err, backend.ErrUnsupported) {
		t.Errorf("CreateFolder: expected ErrUnsupported, got %v", err)
	}
	if err := a.PostTransition(ctx, "1", "11"); !errors.Is(err, backend.ErrUnsupported) {
		t.Errorf("PostTransition: expected ErrUnsupported, got %v", err)
	}
	if _, _, err := a.ListBugs(ctx, "PROJ", nil, "Bug", nil); !errors.Is(err, backend.ErrUnsupported) {
		t.Errorf("ListBugs: expected ErrUnsupported, got %v", err)
	}
}

// TestEmptyStubsReturnEmptyNoError spot-checks the methods the spec decided
// are EMPTY (no analog, non-fatal) rather than UNSUP.
func TestEmptyStubsReturnEmptyNoError(t *testing.T) {
	a := New("http://example.invalid", "alice:secret")
	ctx := context.Background()

	if defs, err := a.ListCustomFields(ctx, "PROJ"); err != nil || len(defs) != 0 {
		t.Errorf("ListCustomFields: expected (empty, nil), got (%v, %v)", defs, err)
	}
	if pcs, membership, err := a.ListPreconditions(ctx, "PROJ", nil); err != nil || len(pcs) != 0 || len(membership) != 0 {
		t.Errorf("ListPreconditions: expected (empty, empty, nil), got (%v, %v, %v)", pcs, membership, err)
	}
	tree, err := a.FolderTree(ctx, "PROJ")
	if err != nil || !reflect.DeepEqual(tree, backend.FolderTreeResult{}) {
		t.Errorf("FolderTree: expected (zero-value, nil), got (%#v, %v)", tree, err)
	}
	if ts, err := a.GetTransitions(ctx, "1", "Open"); err != nil || len(ts) != 0 {
		t.Errorf("GetTransitions: expected (empty, nil), got (%v, %v)", ts, err)
	}
	if links, err := a.ListReqToReqLinks(ctx, []string{"1"}); err != nil || len(links) != 0 {
		t.Errorf("ListReqToReqLinks: expected (empty, nil), got (%v, %v)", links, err)
	}
	// ListIssueLinkTypes with no plugin detected (fresh adapter, no
	// TestConnection) returns empty, not an error — same EMPTY class as the
	// absent requirements read path.
	if lts, err := a.ListIssueLinkTypes(ctx); err != nil || len(lts) != 0 {
		t.Errorf("ListIssueLinkTypes (plugin absent): expected (empty, nil), got (%v, %v)", lts, err)
	}
}

// TestFieldsForJiraStub confirms the no-error stub shape (the interface
// method has no error return).
func TestFieldsForJiraStub(t *testing.T) {
	a := New("http://example.invalid", "alice:secret")
	got := a.FieldsForJira(map[string]string{"summary": "x"})
	if len(got) != 0 {
		t.Errorf("FieldsForJira: expected an empty map stub, got %#v", got)
	}
}
