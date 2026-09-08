package main

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"agile-suite/xtm/internal/backend"
	"agile-suite/core/connection"
	"agile-suite/core/profile"
)

// newTestAppWithKiwiProfile builds an App (via newTestApp, the shared
// temp-store + in-memory-credential-store helper in app_connection_test.go)
// with one saved Kiwi profile, and returns the profile id.
func newTestAppWithKiwiProfile(t *testing.T) (*App, string) {
	t.Helper()
	a := newTestApp(t)
	p, err := a.CreateProfile("Kiwi", "https://kiwi.example.com", "SNMP", "", "", "", "", "u:p", "", false, "kiwi")
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	return a, p.ID
}

// TestSaveBugConnectionStoresTheCredentialOutsideTheDatabase is the security
// rule the whole feature inherits: a token reaches the OS credential manager
// and never a column. The bug connection is a SECOND credential, keyed by its
// own connection id, which is the rule connection.go already documents.
func TestSaveBugConnectionStoresTheCredentialOutsideTheDatabase(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)

	c, err := a.SaveBugConnection(profileID, "https://jira.example.com", "DEF", "Bug", "s3cret", "", false)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if c.ID == profileID {
		t.Fatal("bug connection reused the profile id; it must have its own id and its own credential")
	}
	if c.Role != "bugs" {
		t.Errorf("role = %q, want bugs", c.Role)
	}

	tok, err := a.creds.Load(c.ID)
	if err != nil {
		t.Fatalf("load bug credential: %v", err)
	}
	if tok != "s3cret" {
		t.Errorf("stored credential = %q, want the token passed in", tok)
	}

	// The primary connection's credential is untouched.
	if _, err := a.creds.Load(profileID); err != nil {
		t.Errorf("primary credential disturbed: %v", err)
	}

	// The claim in this test's name: the token itself never reaches the
	// database. Scan the raw connection row rather than trusting the typed
	// Connection struct, which has no field the token could even go into.
	rows, err := a.store.DB().Query(`SELECT * FROM connection WHERE id = ?`, c.ID)
	if err != nil {
		t.Fatalf("query connection row: %v", err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("columns: %v", err)
	}
	if !rows.Next() {
		t.Fatal("bug connection row not found in the database")
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		t.Fatalf("scan connection row: %v", err)
	}
	for i, v := range vals {
		var s string
		switch tv := v.(type) {
		case string:
			s = tv
		case []byte:
			s = string(tv)
		default:
			continue
		}
		if strings.Contains(s, "s3cret") {
			t.Errorf("column %q of the connection row contains the literal token: %q", cols[i], s)
		}
	}
}

// TestSaveBugConnectionIsIdempotent checks a second save edits the existing row
// rather than accumulating bug connections.
func TestSaveBugConnectionIsIdempotent(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)

	first, err := a.SaveBugConnection(profileID, "https://jira.example.com", "DEF", "Bug", "tok", "", false)
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	second, err := a.SaveBugConnection(profileID, "https://jira2.example.com", "OPS", "Defect", "tok2", "", false)
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("second save created a new row (%s then %s)", first.ID, second.ID)
	}
	got, err := a.GetBugConnection(profileID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.URL != "https://jira2.example.com" || got.ProjectKey != "OPS" || got.BugIssueType != "Defect" {
		t.Errorf("got = %+v, want the second save's values", got)
	}
}

// TestGetBugConnectionIsEmptyWhenUnconfigured keeps the frontend simple: an
// unconfigured profile is not an error condition.
func TestGetBugConnectionIsEmptyWhenUnconfigured(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)

	got, err := a.GetBugConnection(profileID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != "" {
		t.Errorf("got = %+v, want a zero Connection", got)
	}
}

// TestDeleteBugConnectionRemovesTheCredentialToo guards against a token
// outliving the configuration that justified storing it.
func TestDeleteBugConnectionRemovesTheCredentialToo(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)

	c, err := a.SaveBugConnection(profileID, "https://jira.example.com", "DEF", "Bug", "tok", "", false)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := a.DeleteBugConnection(profileID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := a.connections.ByRole(profileID, "bugs"); !errors.Is(err, connection.ErrNotFound) {
		t.Errorf("bug connection still present after delete: %v", err)
	}
	if tok, err := a.creds.Load(c.ID); err == nil && tok != "" {
		t.Error("bug credential survived the delete")
	}
}

// TestDeleteProfileRemovesTheBugConnection is the cleanup path. The primary
// connection's credential is keyed by the profile id, so DeleteProfile already
// removed it; the bug connection has its own id and was being left behind.
func TestDeleteProfileRemovesTheBugConnection(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)

	c, err := a.SaveBugConnection(profileID, "https://jira.example.com", "DEF", "Bug", "tok", "", false)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := a.DeleteProfile(profileID); err != nil {
		t.Fatalf("delete profile: %v", err)
	}
	if _, err := a.connections.Get(c.ID); !errors.Is(err, connection.ErrNotFound) {
		t.Errorf("bug connection row survived profile delete: %v", err)
	}
	if tok, err := a.creds.Load(c.ID); err == nil && tok != "" {
		t.Error("bug credential survived profile delete")
	}
}

// TestBugBackendForIsNilWhenUnconfigured pins the routing default. Every
// engine that takes a bug backend must behave exactly as before when this
// returns nil, which is what keeps Xray profiles untouched.
func TestBugBackendForIsNilWhenUnconfigured(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)

	b, err := a.bugBackendFor(profileID)
	if err != nil {
		t.Fatalf("bugBackendFor: %v", err)
	}
	if b != nil {
		t.Errorf("got %T, want nil for a profile with no bug connection", b)
	}
}

// erroringCredentialStore is a profile.CredentialStore fake whose Load
// returns an error for an id it has never Saved — matching the real
// Windows Credential Manager and OS-keyring stores (credentials_windows.go,
// credentials_nonwindows.go), which both fail on an unknown id rather than
// returning ("", nil). memCredentialStore (app_connection_test.go) returns
// ("", nil) for a missing key instead, so it cannot exercise
// SaveBugConnection's blank-token guard: with memCredentialStore the guard's
// a.creds.Load(id) call always succeeds, even for a connection id nothing
// was ever saved under, which would make deleting the guard entirely pass
// every existing test. This fake is kept separate from memCredentialStore
// specifically so that store's behavior (relied on by other tests) is not
// changed.
type erroringCredentialStore struct {
	mu   sync.Mutex
	data map[string]string
}

func newErroringCredentialStore() *erroringCredentialStore {
	return &erroringCredentialStore{data: map[string]string{}}
}

func (e *erroringCredentialStore) Save(id, secret string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.data[id] = secret
	return nil
}

func (e *erroringCredentialStore) Load(id string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	v, ok := e.data[id]
	if !ok {
		return "", errors.New("load credential: element not found")
	}
	return v, nil
}

func (e *erroringCredentialStore) Delete(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.data, id)
	return nil
}

var _ profile.CredentialStore = (*erroringCredentialStore)(nil)

// TestSaveBugConnectionRejectsBlankTokenWithNoStoredCredential exercises the
// blank-token guard in SaveBugConnection against a credential store that
// fails closed like the real ones do. Without a stored bug credential to
// fall back to, a blank token must be rejected rather than silently creating
// a connection with no credential behind it.
func TestSaveBugConnectionRejectsBlankTokenWithNoStoredCredential(t *testing.T) {
	a := newTestApp(t)
	a.creds = newErroringCredentialStore()
	p, err := a.CreateProfile("Kiwi", "https://kiwi.example.com", "SNMP", "", "", "", "", "u:p", "", false, "kiwi")
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}

	if _, err := a.SaveBugConnection(p.ID, "https://jira.example.com", "DEF", "Bug", "" /* blank token */, "", false); err == nil {
		t.Fatal("SaveBugConnection with a blank token and no stored bug credential = nil error, want a rejection")
	}
	if _, err := a.connections.ByRole(p.ID, "bugs"); !errors.Is(err, connection.ErrNotFound) {
		t.Errorf("a rejected save still created a bug connection row: %v", err)
	}
}

// failingSaveCredentialStore delegates Load and Delete to an inner
// profile.CredentialStore but always fails Save, to simulate a credential
// store that accepts the connection row write but then fails to persist the
// secret (e.g. a locked keyring).
type failingSaveCredentialStore struct {
	inner profile.CredentialStore
}

func (f failingSaveCredentialStore) Save(id, secret string) error {
	return errors.New("simulated credential store failure")
}
func (f failingSaveCredentialStore) Load(id string) (string, error) { return f.inner.Load(id) }
func (f failingSaveCredentialStore) Delete(id string) error         { return f.inner.Delete(id) }

var _ profile.CredentialStore = failingSaveCredentialStore{}

// TestSaveBugConnectionRollsBackOrphanRowOnCreateCredentialFailure guards the
// window between creating the bug connection row and saving its credential:
// if the credential save fails on the CREATE path (no prior row, no prior
// credential), the just-created row must not be left behind. Left in place,
// GetCapabilities would report SupportsBugRouting = true off the row alone,
// and bugBackendFor would only fail later, when something actually tries to
// use it.
func TestSaveBugConnectionRollsBackOrphanRowOnCreateCredentialFailure(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)
	a.creds = failingSaveCredentialStore{inner: a.creds}

	if _, err := a.SaveBugConnection(profileID, "https://jira.example.com", "DEF", "Bug", "tok", "", false); err == nil {
		t.Fatal("SaveBugConnection = nil error, want the simulated credential-store failure")
	}
	if _, err := a.connections.ByRole(profileID, "bugs"); !errors.Is(err, connection.ErrNotFound) {
		t.Errorf("orphan bug connection row survived a failed credential save: %v", err)
	}
}

// TestBugBackendForCarriesTheConnectionSettings checks the backend that comes
// back is bound to the BUG connection, not the profile's own. A profile whose
// tests live in Kiwi must not end up filing its bugs into Kiwi.
func TestBugBackendForCarriesTheConnectionSettings(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)

	if _, err := a.SaveBugConnection(profileID, "https://jira.example.com", "DEF", "Bug", "tok", "", false); err != nil {
		t.Fatalf("save bug connection: %v", err)
	}
	b, err := a.bugBackendFor(profileID)
	if err != nil {
		t.Fatalf("bugBackendFor: %v", err)
	}
	if b == nil {
		t.Fatal("no bug backend returned after configuring one")
	}
	if name := b.Capabilities().Name; name != "xray" {
		t.Errorf("bug backend is %q, want xray: bugs are filed into Jira", name)
	}
}

// TestBugBrowseBaseForReturnsTheConnectionURLPlusBrowse pins bugBrowseBaseFor
// itself: the browse prefix is the connection's URL with exactly one
// trailing slash then "browse/" appended. (Formerly misnamed
// "TestPrimaryBackendLearnsTheBugBrowseBase" — no primary backend appears
// here; that wiring is covered separately by
// TestBugRoutingOptionsWiresTheBugBackendAndBrowseBaseIntoThePrimary below.)
func TestBugBrowseBaseForReturnsTheConnectionURLPlusBrowse(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)

	if _, err := a.SaveBugConnection(profileID, "https://jira.example.com", "DEF", "Bug", "tok", "", false); err != nil {
		t.Fatalf("save bug connection: %v", err)
	}
	base, err := a.bugBrowseBaseFor(profileID)
	if err != nil {
		t.Fatalf("bugBrowseBaseFor: %v", err)
	}
	if base != "https://jira.example.com/browse/" {
		t.Errorf("browse base = %q, want the connection URL plus /browse/", base)
	}
}

// TestBugBrowseBaseForTrimsExactlyOneTrailingSlash pins the Jira DC
// context-path case alongside the bare-host ones: a base URL that already
// ends in a path segment (a context path like /jira) must keep that segment,
// with or without its own trailing slash.
func TestBugBrowseBaseForTrimsExactlyOneTrailingSlash(t *testing.T) {
	cases := []struct{ url, want string }{
		{"https://h", "https://h/browse/"},
		{"https://h/", "https://h/browse/"},
		{"https://h/jira", "https://h/jira/browse/"},
		{"https://h/jira/", "https://h/jira/browse/"},
	}
	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			a, profileID := newTestAppWithKiwiProfile(t)
			if _, err := a.SaveBugConnection(profileID, tc.url, "DEF", "Bug", "tok", "", false); err != nil {
				t.Fatalf("save bug connection: %v", err)
			}
			got, err := a.bugBrowseBaseFor(profileID)
			if err != nil {
				t.Fatalf("bugBrowseBaseFor: %v", err)
			}
			if got != tc.want {
				t.Errorf("bugBrowseBaseFor(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}

// fakePrimaryWithBrowseBase is a minimal backend.Backend fake (the embedded
// interface is left nil; bugRoutingOptions never calls any method on it other
// than the SetBugBrowseBase type assertion) used to prove bugRoutingOptions
// actually reaches the PRIMARY backend, not just the returned option slice.
type fakePrimaryWithBrowseBase struct {
	backend.Backend
	gotBase string
}

func (f *fakePrimaryWithBrowseBase) SetBugBrowseBase(base string) { f.gotBase = base }

// TestBugRoutingOptionsWiresTheBugBackendAndBrowseBaseIntoThePrimary is the
// wiring assertion this task exists to build: a routed profile must yield
// exactly one syncer.Option, and the PRIMARY backend must learn the bug
// tracker's browse URL through SetBugBrowseBase. Deleting that type assertion
// in bugRoutingOptions leaves every other bug-routing test in this file
// passing — this is the one that catches it.
func TestBugRoutingOptionsWiresTheBugBackendAndBrowseBaseIntoThePrimary(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)

	if _, err := a.SaveBugConnection(profileID, "https://jira.example.com", "DEF", "Bug", "tok", "", false); err != nil {
		t.Fatalf("save bug connection: %v", err)
	}

	primary := &fakePrimaryWithBrowseBase{}
	opts := a.bugRoutingOptions(profileID, primary)
	if len(opts) != 1 {
		t.Fatalf("bugRoutingOptions returned %d option(s), want exactly 1 for a routed profile", len(opts))
	}
	if primary.gotBase != "https://jira.example.com/browse/" {
		t.Errorf("primary.SetBugBrowseBase got %q, want the connection URL plus /browse/", primary.gotBase)
	}
}

// TestCreateBugForTestRoutesToTheBugConnectionsProjectAndIssueType is the
// CRITICAL regression this round exists to close: a Kiwi profile's defects
// must land in the BUG CONNECTION's Jira project under its issue type, never
// in a project named after the Kiwi product configured on the profile itself
// (which has no corresponding Jira project to create an issue in).
func TestCreateBugForTestRoutesToTheBugConnectionsProjectAndIssueType(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)

	if _, err := a.SaveBugConnection(profileID, "https://jira.example.com", "DEF", "Defect", "tok", "", false); err != nil {
		t.Fatalf("save bug connection: %v", err)
	}

	if _, err := a.CreateBugForTest(profileID, "T-1", "", "Boom", "desc", "High", nil, nil); err != nil {
		t.Fatalf("create bug for test: %v", err)
	}

	bugs, err := a.ListBugsWithTests(profileID)
	if err != nil {
		t.Fatalf("list bugs: %v", err)
	}
	if len(bugs) != 1 {
		t.Fatalf("got %d queued bugs, want 1", len(bugs))
	}
	if bugs[0].ProjectKey != "DEF" {
		t.Errorf("queued bug project = %q, want the bug connection's project DEF, not the Kiwi profile's own project (SNMP)", bugs[0].ProjectKey)
	}
	if bugs[0].IssueType != "Defect" {
		t.Errorf("queued bug issue type = %q, want the bug connection's issue type Defect", bugs[0].IssueType)
	}
}

// TestCreateBugForTestUsesTheProfileWhenUnrouted is the regression guard for
// every Xray profile (and an unconfigured Kiwi one): without a bug
// connection, a queued bug still uses the profile's own project/issue type,
// exactly as before bug routing existed.
func TestCreateBugForTestUsesTheProfileWhenUnrouted(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)

	if _, err := a.CreateBugForTest(profileID, "T-1", "", "Boom", "desc", "High", nil, nil); err != nil {
		t.Fatalf("create bug for test: %v", err)
	}

	bugs, err := a.ListBugsWithTests(profileID)
	if err != nil {
		t.Fatalf("list bugs: %v", err)
	}
	if len(bugs) != 1 {
		t.Fatalf("got %d queued bugs, want 1", len(bugs))
	}
	if bugs[0].ProjectKey != "SNMP" {
		t.Errorf("queued bug project = %q, want the profile's own project SNMP (no bug connection configured)", bugs[0].ProjectKey)
	}
}

// TestGetBugCreateFieldsAsksTheBugConnectionsBackendWhenRouted proves
// GetBugCreateFields asks the BUG backend, not the profile's own Kiwi one, for
// create-screen fields once routing is configured. The Kiwi adapter's
// GetBugCreateFields always returns backend.ErrUnsupported (see
// internal/backend/kiwi/adapter.go), so a successful, non-empty result here
// is only possible if the call reached the bug connection's (demo) Jira
// backend instead.
func TestGetBugCreateFieldsAsksTheBugConnectionsBackendWhenRouted(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)

	if _, err := a.SaveBugConnection(profileID, "demo", "DEF", "Bug", "tok", "", false); err != nil {
		t.Fatalf("save bug connection: %v", err)
	}

	fields, err := a.GetBugCreateFields(profileID)
	if err != nil {
		t.Fatalf("GetBugCreateFields: %v (want the demo bug connection's fields, not Kiwi's ErrUnsupported)", err)
	}
	if len(fields) == 0 {
		t.Fatal("got no create fields; want the demo Jira bug connection's fields")
	}
}

// TestCreateBugForTestSucceedsWithoutAReadableCredential is the local-first
// regression this round exists to close: queuing a bug is a pending-change
// journal write, like every other mutating method in this app, so it must
// not require the credential store to be readable. Before this fix,
// CreateBugForTest called bugCreateTarget (via the shared resolver), which
// always called a.creds.Load and built a Backend even though the Backend was
// discarded — so a locked/unreadable keyring broke queuing a bug on the most
// widely used (unrouted, Xray) path too.
func TestCreateBugForTestSucceedsWithoutAReadableCredential(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)

	if _, err := a.SaveBugConnection(profileID, "https://jira.example.com", "DEF", "Defect", "tok", "", false); err != nil {
		t.Fatalf("save bug connection: %v", err)
	}

	// Simulate the credential store becoming unreadable (a locked keyring)
	// after the bug connection was configured: swap in a fresh erroring store
	// whose Load fails for every id, including ones a working store already
	// saved a token under.
	a.creds = newErroringCredentialStore()

	if _, err := a.CreateBugForTest(profileID, "T-1", "", "Boom", "desc", "High", nil, nil); err != nil {
		t.Fatalf("CreateBugForTest with an unreadable credential store: %v (want it to succeed: queuing is a local journal write, not a remote call)", err)
	}

	bugs, err := a.ListBugsWithTests(profileID)
	if err != nil {
		t.Fatalf("list bugs: %v", err)
	}
	if len(bugs) != 1 {
		t.Fatalf("got %d queued bugs, want 1", len(bugs))
	}
	if bugs[0].ProjectKey != "DEF" || bugs[0].IssueType != "Defect" {
		t.Errorf("queued bug = %+v, want project DEF / issue type Defect from the bug connection", bugs[0])
	}
}

// TestGetBugDetailAsksTheBugConnectionsBackendWhenRouted is the read-side twin
// of the create-side routing: on a routed profile the defect lives in the bug
// tracker, so asking the profile's own Kiwi backend can only ever answer
// ErrUnsupported (internal/backend/kiwi/adapter.go). A non-empty result here
// is only possible if the call reached the bug connection's (demo) Jira
// backend instead.
func TestGetBugDetailAsksTheBugConnectionsBackendWhenRouted(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)

	if _, err := a.SaveBugConnection(profileID, "demo", "DEF", "Bug", "tok", "", false); err != nil {
		t.Fatalf("save bug connection: %v", err)
	}

	detail, err := a.GetBugDetail(profileID, "DEF-42")
	if err != nil {
		t.Fatalf("GetBugDetail: %v (want the demo bug connection's detail, not Kiwi's ErrUnsupported)", err)
	}
	if detail.Description == "" {
		t.Error("got an empty detail; want the demo Jira bug connection's fields")
	}
}

// TestGetBugBrowseBaseIsEmptyWhenUnrouted keeps the frontend's fallback
// honest: an empty base means "use the profile's own URL", which is what every
// Xray profile must keep doing.
func TestGetBugBrowseBaseIsEmptyWhenUnrouted(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)

	base, err := a.GetBugBrowseBase(profileID)
	if err != nil {
		t.Fatalf("GetBugBrowseBase: %v", err)
	}
	if base != "" {
		t.Errorf("browse base = %q, want empty for a profile with no bug connection", base)
	}
}

// TestGetBugBrowseBaseNamesTheBugTracker is what stops a bug key from opening
// a 404 on the Kiwi host: the browse prefix must come from the BUG
// connection's URL, never the profile's.
func TestGetBugBrowseBaseNamesTheBugTracker(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)

	if _, err := a.SaveBugConnection(profileID, "https://jira.example.com", "DEF", "Bug", "tok", "", false); err != nil {
		t.Fatalf("save bug connection: %v", err)
	}
	base, err := a.GetBugBrowseBase(profileID)
	if err != nil {
		t.Fatalf("GetBugBrowseBase: %v", err)
	}
	if base != "https://jira.example.com/browse/" {
		t.Errorf("browse base = %q, want the bug connection's URL plus /browse/", base)
	}
}
