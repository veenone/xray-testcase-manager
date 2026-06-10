// Package jira is the REST client for Jira Data Center and Xray Server / DC.
//
// It targets Jira DC 8.14+ (Personal Access Tokens) and Xray Server / DC 8.4.0.
// Jira issue operations use /rest/api/2/; Xray test entities use /rest/raven/2.0/.
package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Client talks to a single Jira Data Center instance.
type Client struct {
	baseURL string
	token   string
	http    *http.Client

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
}

// User is the subset of /rest/api/2/myself the app needs to confirm a connection.
type User struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Email       string `json:"emailAddress"`
}

// HTTPError carries the HTTP status of a failed Jira request so callers can
// treat specific statuses as soft failures — e.g. a 400 from an issue-type
// search the instance/project doesn't support (no "Precondition" type)
// shouldn't abort the whole sync.
type HTTPError struct {
	Method string
	Path   string
	Code   int
	Status string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("jira: %s %s -> %s", e.Method, e.Path, e.Status)
}

// NewClient builds a client for the given Jira base URL authenticated with a
// Personal Access Token. baseURL is the instance root, e.g.
// https://jira.example.com.
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
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

// get performs an authenticated GET and decodes a JSON response into out.
func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &HTTPError{Method: http.MethodGet, Path: path, Code: resp.StatusCode, Status: resp.Status}
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// getBytes performs an authenticated GET and returns the raw response body
// instead of decoding it. Unlike get, it surfaces a non-200 response body in
// the error and leaves decoding to the caller — used where the response shape
// varies between Xray versions and has to be sniffed (see parseStepsResponse).
func (c *Client) getBytes(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jira: GET %s -> %s: %s", path, resp.Status, snippet(body, 1024))
	}
	return body, nil
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

	resp, err := c.http.Do(req)
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

	resp, err := c.http.Do(req)
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
