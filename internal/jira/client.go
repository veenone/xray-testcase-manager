// Package jira is the REST client for Jira Data Center and Xray Server / DC.
//
// It targets Jira DC 8.14+ (Personal Access Tokens) and Xray Server / DC 8.4.0.
// Jira issue operations use /rest/api/2/; Xray test entities use /rest/raven/2.0/.
package jira

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Shared client request pacing. A token-bucket limiter governs every outbound
// HTTP call (see Client.do), which lets the sync passes run their per-item
// fetches concurrently while staying within a safe request rate — instead of
// the old blind 150ms sleep after each serial call. Tunable if an instance has
// tighter or looser rate limits.
const (
	syncReqPerSec = 20 // sustained requests/second across all calls on this client
	syncBurst     = 10 // allowed burst above the sustained rate
)

// Client talks to a single Jira Data Center instance.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
	// limiter paces every outbound request (Client.do). Shared across goroutines
	// so concurrent sync fetches respect one global rate.
	limiter *rate.Limiter

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
type User struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Email       string `json:"emailAddress"`
}

// HTTPError carries the HTTP status of a failed Jira request so callers can
// treat specific statuses as soft failures -- e.g. a 400 from an issue-type
// search the instance/project doesn't support (no "Precondition" type)
// shouldn't abort the whole sync.
type HTTPError struct {
	Method  string
	Path    string
	Code    int
	Status  string
	Message string // human-readable Jira error, parsed from the response body
}

func (e *HTTPError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("jira: %s: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("jira: %s %s -> %s", e.Method, e.Path, e.Status)
}

// clientConfig holds optional TLS settings for a Jira client.
type clientConfig struct {
	caCertPEM string
	insecure  bool
}

// Option is a functional option for NewClient.
type Option func(*clientConfig)

// WithCACert returns an Option that adds a PEM-encoded CA certificate to the
// TLS trust pool. The system certificate pool is used as the base, so existing
// trusted roots are preserved. The certificate is appended; if the PEM is
// empty or unparseable no certificate is added.
func WithCACert(pem string) Option {
	return func(c *clientConfig) { c.caCertPEM = pem }
}

// WithInsecureTLS returns an Option that disables TLS certificate verification.
// NOTE(xtm): InsecureSkipVerify suppresses all certificate checks including
// hostname verification. Only use for trusted internal servers where no CA
// certificate is available. Needs live verification against a real macOS +
// internal-CA environment (RND_P_4TFINT_05-243).
func WithInsecureTLS(b bool) Option {
	return func(c *clientConfig) { c.insecure = b }
}

// buildHTTPClient constructs the http.Client for a Jira client. When no TLS
// options are set (caCertPEM == "" and !insecure) it returns the plain
// &http.Client{Timeout: 30 * time.Second} that the app has always used, so
// the default path is byte-for-byte identical to the old behaviour. Otherwise
// it clones http.DefaultTransport and sets a custom TLSClientConfig so other
// transport defaults (connection pooling, keep-alives, etc.) are preserved.
//
// NOTE(xtm): The custom-CA and insecure paths have not been exercised against
// a real macOS + internal-CA environment. Verify before declaring the feature
// complete (RND_P_4TFINT_05-243).
func buildHTTPClient(cfg clientConfig) *http.Client {
	if cfg.caCertPEM == "" && !cfg.insecure {
		return &http.Client{Timeout: 30 * time.Second}
	}

	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if cfg.caCertPEM != "" {
		pool.AppendCertsFromPEM([]byte(cfg.caCertPEM))
	}

	base := http.DefaultTransport.(*http.Transport).Clone()
	base.TLSClientConfig = &tls.Config{
		RootCAs:            pool,
		InsecureSkipVerify: cfg.insecure, //nolint:gosec // user-controlled escape hatch
	}
	return &http.Client{Timeout: 30 * time.Second, Transport: base}
}

// NewClient builds a client for the given Jira base URL authenticated with a
// Personal Access Token. baseURL is the instance root, e.g.
// https://jira.example.com. Pass WithCACert or WithInsecureTLS to override the
// default system TLS trust (FR-8.4 / RND_P_4TFINT_05-243).
func NewClient(baseURL, token string, opts ...Option) *Client {
	var cfg clientConfig
	for _, o := range opts {
		o(&cfg)
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    buildHTTPClient(cfg),
		limiter: rate.NewLimiter(syncReqPerSec, syncBurst),
	}
}

// do sends an HTTP request after waiting for a slot from the shared rate
// limiter, so every call (get/getBytes/put/post/delete) is paced by one global
// token bucket. This is the single throttle point: sync passes can fire their
// per-item requests concurrently and the limiter smooths them to a safe rate,
// replacing the old fixed per-call sleep. A nil limiter (older construction
// paths, tests) is a no-op so the client still works without one.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	if c.limiter != nil {
		if err := c.limiter.Wait(req.Context()); err != nil {
			return nil, err
		}
	}
	return c.http.Do(req)
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

// get performs an authenticated GET and decodes a JSON response into out.
func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		msg := jiraErrorMessage(body)
		return &HTTPError{Method: http.MethodGet, Path: path, Code: resp.StatusCode, Status: resp.Status, Message: msg}
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// getBytes performs an authenticated GET and returns the raw response body
// instead of decoding it. Unlike get, it surfaces a non-200 response body in
// the error and leaves decoding to the caller — used where the response shape
// varies between Xray versions and has to be sniffed (see parseStepsResponse).
func (c *Client) getBytes(ctx context.Context, path string) ([]byte, error) {
	body, _, err := c.getBytesStatus(ctx, path)
	return body, err
}

// getBytesStatus is getBytes plus the HTTP status code, for the callers that
// treat a particular status as data rather than as a failure (Xray answers 400
// when an issue key is simply not a Test, for instance). The status is reported
// alongside the error rather than encoded into it so the error text stays
// byte-for-byte what getBytes has always produced.
func (c *Client) getBytesStatus(ctx context.Context, path string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("jira: GET %s -> %s: %s", path, resp.Status, snippet(body, 1024))
	}
	return body, resp.StatusCode, nil
}

// put performs an authenticated JSON PUT.
func (c *Client) put(ctx context.Context, path string, body any) error {
	return c.writeJSON(ctx, http.MethodPut, path, body)
}

// post performs an authenticated JSON POST.
func (c *Client) post(ctx context.Context, path string, body any) error {
	return c.writeJSON(ctx, http.MethodPost, path, body)
}

// delete performs an authenticated DELETE with no body. 2xx is success;
// anything else returns an error with a short slice of the response body
// for diagnostics.
func (c *Client) delete(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf(
			"jira: DELETE %s -> %s: %s",
			path, resp.Status, strings.TrimSpace(string(respBody)),
		)
	}
	return nil
}

// writeJSON marshals body as JSON and sends it with the given method,
// discarding the response body. See writeJSONReturning when the caller needs
// the response decoded.
func (c *Client) writeJSON(ctx context.Context, method, path string, body any) error {
	return c.writeJSONReturning(ctx, method, path, body, nil)
}

// writeJSONReturning marshals body as JSON, sends it with the given method,
// and decodes a 2xx response into out when out is non-nil and a body is
// present. Any non-2xx status returns an error that includes a short slice of
// the response body for diagnostics. A non-nil out tolerates an empty body
// (some endpoints answer 201/204 with nothing).
func (c *Client) writeJSONReturning(ctx context.Context, method, path string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(
		ctx, method, c.baseURL+path, bytes.NewReader(payload),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf(
			"jira: %s %s -> %s: %s",
			method, path, resp.Status, strings.TrimSpace(string(respBody)),
		)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil && err != io.EOF {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
