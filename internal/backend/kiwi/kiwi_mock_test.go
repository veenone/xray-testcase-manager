package kiwi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockRPCServer is a minimal JSON-RPC 2.0 test double that routes by the
// incoming request's "method" field, returning a canned result/error
// registered via handle/handleResult/handleError. An unregistered method
// falls back to a -32601 "method not found" error, matching real
// Kiwi/modernrpc behavior for an absent RPC (spec §1.3, §9).
type mockRPCServer struct {
	t        *testing.T
	handlers map[string]func(params []json.RawMessage) (result any, rpcErr *rpcErrorObj)
	requests []recordedRequest
}

// recordedRequest captures one decoded call for assertions (e.g. that the
// session cookie set by Auth.login was replayed on a later request).
type recordedRequest struct {
	Method  string
	Params  []json.RawMessage
	Cookies []*http.Cookie
}

func newMockRPCServer(t *testing.T) *mockRPCServer {
	t.Helper()
	return &mockRPCServer{
		t:        t,
		handlers: map[string]func(params []json.RawMessage) (any, *rpcErrorObj){},
	}
}

// handle registers a handler for method that inspects the raw params.
func (m *mockRPCServer) handle(method string, fn func(params []json.RawMessage) (result any, rpcErr *rpcErrorObj)) {
	m.handlers[method] = fn
}

// handleResult registers a method that always succeeds with the same
// canned result regardless of the params sent.
func (m *mockRPCServer) handleResult(method string, result any) {
	m.handle(method, func([]json.RawMessage) (any, *rpcErrorObj) { return result, nil })
}

// handleError registers a method that always fails with the given
// JSON-RPC error code/message (e.g. -32601 for "not installed", or a
// different code to simulate a PermissionDenied-style application error).
func (m *mockRPCServer) handleError(method string, code int, message string) {
	m.handle(method, func([]json.RawMessage) (any, *rpcErrorObj) {
		return nil, &rpcErrorObj{Code: code, Message: message}
	})
}

func (m *mockRPCServer) start() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(m.serveHTTP))
}

func (m *mockRPCServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/json-rpc/" {
		http.NotFound(w, r)
		return
	}

	var req struct {
		JSONRPC string            `json:"jsonrpc"`
		Method  string            `json:"method"`
		Params  []json.RawMessage `json:"params"`
		ID      int64             `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.t.Fatalf("mock server: decode request: %v", err)
		return
	}
	m.requests = append(m.requests, recordedRequest{Method: req.Method, Params: req.Params, Cookies: r.Cookies()})

	resp := struct {
		JSONRPC string       `json:"jsonrpc"`
		Result  any          `json:"result,omitempty"`
		Error   *rpcErrorObj `json:"error,omitempty"`
		ID      int64        `json:"id"`
	}{JSONRPC: "2.0", ID: req.ID}

	fn, ok := m.handlers[req.Method]
	if !ok {
		resp.Error = &rpcErrorObj{Code: methodNotFoundCode, Message: "Method not found: " + req.Method}
	} else {
		result, rpcErr := fn(req.Params)
		if rpcErr != nil {
			resp.Error = rpcErr
		} else {
			resp.Result = result
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		m.t.Fatalf("mock server: encode response: %v", err)
	}
}
