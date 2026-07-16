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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
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
