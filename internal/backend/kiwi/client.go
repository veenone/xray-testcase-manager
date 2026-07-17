// Package kiwi adapts a Kiwi TCMS instance's JSON-RPC 2.0 API to the neutral
// backend.Backend interface, mirroring the reference pattern in
// internal/backend/xray. THIS task (P4.1) builds only the transport,
// authentication, base Capabilities, and the plugin-detection probe
// mechanism; every other Backend method is a stub (see adapter.go). Read
// mappings land in P4.2 (core) and P4.3 (plugins).
//
// Spec: .superpowers/sdd/p4_0-kiwi-integration-spec.md
package kiwi

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"sync/atomic"
)

// Client is a JSON-RPC 2.0 transport for a single Kiwi TCMS instance. All
// RPC calls go through one endpoint: POST {baseURL}/json-rpc/. Spec §1.1.
type Client struct {
	baseURL string
	http    *http.Client
	auth    Authenticator

	// authHeaderName/authHeaderValue are set by a bearerToken Authenticator
	// and, when non-empty, applied to every outgoing request. Unused by the
	// default sessionLogin Authenticator (session state lives in the
	// http.Client's cookiejar instead). Spec §1.2 Option B.
	authHeaderName  string
	authHeaderValue string

	nextID int64

	// demo is non-nil when baseURL selected the offline kiwi-demo mode
	// (P4.4, demo.go). When set, call() dispatches to it instead of doing
	// any HTTP.
	demo *kiwiDemoGenerator

	// loginMu/loginDone guard lazy session-login: the first authenticated
	// call() on a fresh client runs Login once (session-login has no bearer
	// token, so every request relies on the session cookie). Without this a
	// sync-created client -- which, unlike TestConnection, never calls Login
	// explicitly -- would send TestCase.filter with no session and get
	// "Authentication failed" (-32603).
	loginMu   sync.Mutex
	loginDone bool
}

// Option customizes a Client at construction time.
type Option func(*Client)

// WithAuthenticator overrides the default session-login Authenticator (spec
// §1.2 Option A) with another Authenticator implementation (e.g. bearerToken,
// or a test double).
func WithAuthenticator(a Authenticator) Option {
	return func(c *Client) { c.auth = a }
}

// WithHTTPClient overrides the underlying http.Client (e.g. to point a test
// transport at an httptest.Server). If the supplied client has no cookiejar,
// one is installed so session-login continues to work.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc.Jar == nil {
			jar, _ := cookiejar.New(nil)
			hc.Jar = jar
		}
		c.http = hc
	}
}

// WithTLS configures the client's TLS trust from a profile's settings: an
// optional PEM-encoded CA certificate appended to the system pool, and/or
// skipping certificate verification (for a self-signed server such as a
// localhost Kiwi). A no-op when both are unset, so the default system trust is
// used. Only the Transport is set, so the cookiejar installed by NewClient is
// preserved.
func WithTLS(caCertPEM string, insecure bool) Option {
	return func(c *Client) {
		if caCertPEM == "" && !insecure {
			return
		}
		cfg := &tls.Config{InsecureSkipVerify: insecure}
		if caCertPEM != "" {
			pool, err := x509.SystemCertPool()
			if err != nil || pool == nil {
				pool = x509.NewCertPool()
			}
			if pool.AppendCertsFromPEM([]byte(caCertPEM)) {
				cfg.RootCAs = pool
			}
		}
		c.http.Transport = &http.Transport{TLSClientConfig: cfg}
	}
}

// NewClient builds a Kiwi JSON-RPC client against baseURL, defaulting to
// session-login authentication with credential split as "username:password"
// on the FIRST colon (password may itself contain colons — password never
// contains a colon in our test fixtures, but the split must not assume
// that). See auth.go and p4_1-brief.md "Resolved decisions".
//
// The client does not perform any I/O until a method calls Login (directly,
// or via Adapter.TestConnection / any other authenticated call).
func NewClient(baseURL, credential string, opts ...Option) *Client {
	jar, _ := cookiejar.New(nil)
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Jar: jar},
	}
	if IsKiwiDemoURL(baseURL) {
		c.demo = newKiwiDemoGenerator()
	}
	user, pass := splitCredential(credential)
	c.auth = &sessionLogin{user: user, pass: pass}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Login runs the configured Authenticator once, establishing the session
// cookie (sessionLogin) or the outgoing auth header (bearerToken) that
// subsequent calls rely on. Calling it more than once re-authenticates.
func (c *Client) Login(ctx context.Context) error {
	if c.auth == nil {
		return nil
	}
	return c.auth.Authenticate(ctx, c)
}

// ensureLoggedIn runs Login exactly once per client (subsequent calls are a
// no-op), so any authenticated method self-establishes the session cookie.
// call() invokes it before every non-Auth.login request; the mutex serializes
// a fresh client's concurrent first calls. An explicit Login (e.g.
// TestConnection) also marks the client logged in.
func (c *Client) ensureLoggedIn(ctx context.Context) error {
	c.loginMu.Lock()
	defer c.loginMu.Unlock()
	if c.loginDone {
		return nil
	}
	if err := c.Login(ctx); err != nil {
		return err
	}
	c.loginDone = true
	return nil
}

// rpcRequest is the JSON-RPC 2.0 envelope Kiwi's modernrpc layer expects.
// Params is positional: Kiwi RPC methods like `TestCase.filter` take a
// single query-dict positional argument (so Params is a one-element array
// holding that dict), while `Auth.login` takes two positional scalar
// arguments (Params is a two-element array of strings). Spec §1.1.
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
	ID      int64  `json:"id"`
}

// rpcErrorObj is the JSON-RPC 2.0 error object shape. Spec §1.1/§1.3.
type rpcErrorObj struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcErrorObj    `json:"error"`
	ID      int64           `json:"id"`
}

// kiwiRPCError is the decoded form of a JSON-RPC error response. Code
// -32601 ("method not found") is the standard JSON-RPC code Kiwi's
// modernrpc layer returns for an unregistered method — the signal the
// plugin-detection probe relies on. Spec §1.3, §4.3.
type kiwiRPCError struct {
	Code    int
	Message string
}

func (e *kiwiRPCError) Error() string {
	return fmt.Sprintf("kiwi rpc error %d: %s", e.Code, e.Message)
}

// methodNotFoundCode is the standard JSON-RPC 2.0 "method not found" error
// code. Spec §1.3.
const methodNotFoundCode = -32601

// isMethodNotFound reports whether err is a *kiwiRPCError carrying the
// standard "method not found" code -32601. Used by the plugin-detection
// probe (caps.go) to distinguish "plugin absent" from any other failure.
// Spec §4.3.
func isMethodNotFound(err error) bool {
	var rpcErr *kiwiRPCError
	if errors.As(err, &rpcErr) {
		return rpcErr.Code == methodNotFoundCode
	}
	return false
}

// call invokes a single JSON-RPC method with positional params and decodes
// the "result" into out (a pointer; pass nil to discard the result). A
// JSON-RPC error object is returned as a *kiwiRPCError; transport/HTTP
// failures are returned as plain wrapped errors. Spec §1.1/§1.3.
func (c *Client) call(ctx context.Context, method string, params []any, out any) error {
	if c.demo != nil {
		return c.demo.call(method, params, out)
	}

	// Establish the session before any authenticated call (Auth.login itself
	// is the exception -- it IS the login and must not recurse).
	if method != "Auth.login" {
		if err := c.ensureLoggedIn(ctx); err != nil {
			return err
		}
	}

	id := atomic.AddInt64(&c.nextID, 1)
	if params == nil {
		params = []any{}
	}

	reqBody, err := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: method, Params: params, ID: id})
	if err != nil {
		return fmt.Errorf("kiwi: marshal request for %s: %w", method, err)
	}

	endpoint := c.baseURL + "/json-rpc/"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("kiwi: build request for %s: %w", method, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if c.authHeaderName != "" {
		httpReq.Header.Set(c.authHeaderName, c.authHeaderValue)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("kiwi: %s: %w", method, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("kiwi: read response for %s: %w", method, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("kiwi: %s: unexpected HTTP status %d: %s", method, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var rpcResp rpcResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return fmt.Errorf("kiwi: decode response for %s: %w", method, err)
	}
	if rpcResp.Error != nil {
		return &kiwiRPCError{Code: rpcResp.Error.Code, Message: rpcResp.Error.Message}
	}
	if out != nil && len(rpcResp.Result) > 0 {
		if err := json.Unmarshal(rpcResp.Result, out); err != nil {
			return fmt.Errorf("kiwi: decode result for %s: %w", method, err)
		}
	}
	return nil
}
