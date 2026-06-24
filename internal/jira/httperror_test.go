package jira

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHTTPErrorSurfacesJiraMessage verifies that a non-200 response from
// get parses the Jira error body and surfaces it in the error string without
// echoing the raw request URL.
func TestHTTPErrorSurfacesJiraMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errorMessages":["The value 'X' does not exist for the field 'project'."]}`))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	var out struct{}
	err := c.get(context.Background(), "/rest/api/2/search?jql=project%3DX", &out)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "The value 'X' does not exist for the field 'project'.") {
		t.Errorf("error does not contain Jira message: %q", msg)
	}
	if strings.Contains(msg, "jql=project") {
		t.Errorf("error should not contain raw query string (giant URL): %q", msg)
	}
	httpErr, ok := err.(*HTTPError)
	if !ok {
		t.Fatalf("expected *HTTPError, got %T", err)
	}
	if httpErr.Code != http.StatusBadRequest {
		t.Errorf("Code = %d, want 400", httpErr.Code)
	}
}
