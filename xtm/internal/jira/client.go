// Package jira is the REST client for Jira Data Center and Xray Server / DC.
//
// The transport (auth, TLS, request pacing, JSON helpers) is the suite's
// shared core/jira client, embedded below. This package adds the Xray
// endpoints (/rest/raven/2.0/) and the per-instance caches XTM's sync and
// commit passes rely on. It targets Xray Server / DC 8.4.0.
package jira

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	corejira "agile-suite/core/jira"
)

// Client talks to a single Jira Data Center instance with Xray.
type Client struct {
	*corejira.Client
	// baseURL and token are copies of what the embedded client holds. The
	// Xray methods read c.baseURL for the demo checks and c.token for the one
	// raw multipart request, so keeping the two here leaves their bodies as
	// they were.
	baseURL string
	token   string

	// precondTypeOnce lazily resolves and caches the Precondition issue type
	// for this instance (its name varies / may be localised), so the JQL search
	// and the create call both target the right type.
	precondTypeOnce sync.Once
	precondTypeID   string
	precondTypeName string
	precondTypeErr  error

	// testTypeOnce lazily resolves and caches the plain "Test" issue type id for
	// this instance, used when creating new Tests (FR-1).
	testTypeOnce sync.Once
	testTypeID   string
	testTypeName string
	testTypeErr  error

	// subTaskTEOnce lazily resolves and caches the issue type name(s) used for
	// sub-task Test Executions on this instance. Their name varies (default "Sub
	// Test Execution", but instances may rename / localise it), so they are
	// discovered from the instance issue type list rather than hardcoded.
	subTaskTEOnce  sync.Once
	subTaskTENames []string

	// customFieldMu guards customFieldIDs, the per-instance cache of resolved
	// custom field ids keyed by field name (see resolveCustomFieldID), so a sync
	// or commit resolves a given field (e.g. "Test Type") from /rest/api/2/field
	// at most once.
	customFieldMu  sync.Mutex
	customFieldIDs map[string]string
	// fieldCatalog caches the whole /rest/api/2/field answer (see customFields).
	// Every resolver consults it, and one resolver consults it twice, once for
	// the defining plugin's key and once for the display name; the answer runs
	// to hundreds of entries, so it is read once per client.
	fieldCatalog []customField
	// customFieldTypes caches the coarse schema type of every custom field on
	// the instance, keyed by field id (see customFieldType), filled from one
	// /rest/api/2/field fetch so a commit pushing several custom field edits
	// resolves each field's type without re-fetching.
	customFieldTypes map[string]string
	// customFieldTypesLoaded records that the one-shot /rest/api/2/field type
	// fetch has run, so an unknown id (absent from customFieldTypes) does not
	// trigger a redundant re-fetch.
	customFieldTypesLoaded bool

	// bugLinkTypeOnce lazily resolves and caches the issue-link type CreateBugLink
	// uses (a defect-oriented type if the instance defines one, else "Relates"),
	// so linking many bugs in one commit resolves the type just once.
	bugLinkTypeOnce sync.Once
	bugLinkTypeName string
	bugLinkTypeErr  error

	// reqLinkTypeOnce lazily resolves and caches the Requirement->Requirement
	// issue-link type used by UpdateRequirementLinks. Preferred candidates:
	// "requires", "Requires", "depends on", "Depends".
	reqLinkTypeOnce sync.Once
	reqLinkTypeName string
	reqLinkTypeErr  error

	// requirementLinkType is the configured issue-link type for Test->Requirement
	// coverage links (e.g. "Tested By"). When non-empty it overrides
	// resolveRequirementLinkType; set at construction from the persisted setting.
	requirementLinkType string

	// covLinkTypeOnce lazily resolves and caches the issue-link type for
	// Test->Requirement coverage when no explicit type is configured.
	// Preferred candidates: "tested by", "tests", "relates".
	covLinkTypeOnce sync.Once
	covLinkTypeName string
	covLinkTypeErr  error

	// currentUserOnce lazily resolves and caches the authenticated user's
	// username (PAT owner) via GET /rest/api/2/myself, so CreateBug can set the
	// reporter field without an extra round-trip on every call.
	currentUserOnce sync.Once
	currentUserName string
	currentUserErr  error
}

// User is the subset of /rest/api/2/myself the app needs to confirm a connection.
type User = corejira.User

// HTTPError carries the HTTP status of a failed Jira request so callers can
// treat specific statuses as soft failures.
type HTTPError = corejira.HTTPError

// Option is a functional option for NewClient.
type Option = corejira.Option

// WithCACert adds a PEM-encoded CA certificate to the TLS trust pool.
func WithCACert(pem string) Option { return corejira.WithCACert(pem) }

// WithInsecureTLS disables TLS certificate verification.
func WithInsecureTLS(b bool) Option { return corejira.WithInsecureTLS(b) }

// NewClient builds a client for the given Jira base URL authenticated with a
// Personal Access Token. baseURL is the instance root, e.g.
// https://jira.example.com. Pass WithCACert or WithInsecureTLS to override the
// default system TLS trust (FR-8.4 / RND_P_4TFINT_05-243).
func NewClient(baseURL, token string, opts ...Option) *Client {
	return wrap(corejira.NewClient(baseURL, token, opts...), token)
}

// newClientWith builds a client on a caller-supplied http.Client; tests use
// it to point at an httptest server. A nil h uses the default client.
func newClientWith(baseURL, token string, h *http.Client) *Client {
	return wrap(corejira.NewClientWithHTTP(baseURL, token, h), token)
}

func wrap(core *corejira.Client, token string) *Client {
	return &Client{Client: core, baseURL: core.BaseURL(), token: token}
}

// The helpers below keep every Xray method's body unchanged: they are the
// names those methods have always called, delegating to the shared transport.

func (c *Client) do(req *http.Request) (*http.Response, error) { return c.Client.Do(req) }

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.Client.Get(ctx, path, out)
}

func (c *Client) getBytes(ctx context.Context, path string) ([]byte, error) {
	return c.Client.GetBytes(ctx, path)
}

func (c *Client) getBytesStatus(ctx context.Context, path string) ([]byte, int, error) {
	return c.Client.GetBytesStatus(ctx, path)
}

func (c *Client) put(ctx context.Context, path string, body any) error {
	return c.Client.Put(ctx, path, body)
}

func (c *Client) post(ctx context.Context, path string, body any) error {
	return c.Client.Post(ctx, path, body)
}

func (c *Client) delete(ctx context.Context, path string) error {
	return c.Client.Delete(ctx, path)
}

func (c *Client) writeJSON(ctx context.Context, method, path string, body any) error {
	return c.Client.WriteJSON(ctx, method, path, body)
}

func (c *Client) writeJSONReturning(ctx context.Context, method, path string, body, out any) error {
	return c.Client.WriteJSONReturning(ctx, method, path, body, out)
}

// IsDemo reports whether this client is in demo mode (no Jira network calls).
func (c *Client) IsDemo() bool { return isDemoURL(c.baseURL) }

// SetRequirementLinkType configures the issue-link type name used when linking
// a Test to a Requirement (FR-13 / #275). When non-empty it takes precedence
// over the auto-resolved type from resolveRequirementLinkType. Call this once
// at construction; the field is not guarded by a mutex.
func (c *Client) SetRequirementLinkType(name string) {
	c.requirementLinkType = name
}

// TestConnection verifies the base URL and token by fetching the current user
// (FR-8.4). It returns the authenticated user on success. Demo URLs
// short-circuit to a fake user so the UI can be exercised without Jira.
func (c *Client) TestConnection(ctx context.Context) (*User, error) {
	if isDemoURL(c.baseURL) {
		return &User{Name: "demo", DisplayName: "Demo User", Email: "demo@local"}, nil
	}
	var u User
	if err := c.get(ctx, "/rest/api/2/myself", &u); err != nil {
		return nil, fmt.Errorf("connection test failed: %w", err)
	}
	return &u, nil
}

// currentUser resolves and caches the authenticated user's username (PAT
// owner) via GET /rest/api/2/myself. The result is computed at most once per
// client (sync.Once). Demo mode returns the synthetic username "demo.user".
//
// NOTE(xtm): Jira DC REST v2 identifies users by their login name ("name").
// Some newer Jira instances (Server/DC migrated from Cloud) may use "accountId"
// instead; verify against the live Xray Server/DC 8.4.0 instance and adjust
// the reporter field key in CreateBug if needed.
func (c *Client) currentUser(ctx context.Context) (string, error) {
	c.currentUserOnce.Do(func() {
		if isDemoURL(c.baseURL) {
			c.currentUserName = "demo.user"
			return
		}
		var u User
		if e := c.get(ctx, "/rest/api/2/myself", &u); e != nil {
			c.currentUserErr = e
			return
		}
		c.currentUserName = u.Name
	})
	return c.currentUserName, c.currentUserErr
}
