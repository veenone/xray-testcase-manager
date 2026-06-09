package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// newTestClient points a Client at a mock server.
func newTestClient(srv *httptest.Server) *Client {
	return &Client{baseURL: srv.URL, token: "t", http: srv.Client()}
}

// TestListPreconditionsResolvesTypeAndPaginates exercises the live-Jira path:
// the Precondition issue type is resolved by name, the JQL search uses its id,
// and each precondition's tests are pulled across Xray's 200-row page cap.
func TestListPreconditionsResolvesTypeAndPaginates(t *testing.T) {
	// One precondition (PC-1) linked to 250 tests, forcing two pages.
	const linked = 250
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/2/issuetype":
			// Real instances name it "Pre-Condition" (hyphenated) — the
			// normalised match must still resolve it.
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "1", "name": "Bug"},
				{"id": "42", "name": "Pre-Condition"},
			})
		case r.URL.Path == "/rest/api/2/search":
			if it := r.URL.Query().Get("jql"); !strings.Contains(it, "issuetype = 42") {
				t.Errorf("search JQL should target the resolved id, got %q", it)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total": 1,
				"issues": []map[string]any{
					{"key": "PC-1", "fields": map[string]any{"summary": "Logged in", "description": "d"}},
				},
			})
		case r.URL.Path == "/rest/raven/1.0/api/precondition/PC-1/test":
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			if limit != ravenPageLimit {
				t.Errorf("limit = %d, want %d", limit, ravenPageLimit)
			}
			start := (page - 1) * limit
			arr := []map[string]any{}
			for i := start; i < linked && i < start+limit; i++ {
				arr = append(arr, map[string]any{"key": fmt.Sprintf("T-%d", i+1)})
			}
			_ = json.NewEncoder(w).Encode(arr)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	pres, links, err := newTestClient(srv).ListPreconditions(context.Background(), "PROJ")
	if err != nil {
		t.Fatalf("ListPreconditions: %v", err)
	}
	if len(pres) != 1 || pres[0].Key != "PC-1" {
		t.Fatalf("preconditions = %+v, want [PC-1]", pres)
	}
	// All 250 linked tests should map back to PC-1 (both pages collected).
	count := 0
	for _, pks := range links {
		for _, pk := range pks {
			if pk == "PC-1" {
				count++
			}
		}
	}
	if count != linked {
		t.Fatalf("linked tests = %d, want %d (pagination)", count, linked)
	}
}

// TestCreatePreconditionUsesResolvedTypeID verifies the create POST carries the
// resolved issue-type id (not the bare name that some instances reject).
func TestCreatePreconditionUsesResolvedTypeID(t *testing.T) {
	var gotIssueType map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/issuetype":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "77", "name": "Pre-Condition"}, // renamed type, contains-match
			})
		case "/rest/api/2/issue":
			body, _ := io.ReadAll(r.Body)
			var payload struct {
				Fields struct {
					IssueType map[string]any `json:"issuetype"`
				} `json:"fields"`
			}
			_ = json.Unmarshal(body, &payload)
			gotIssueType = payload.Fields.IssueType
			_ = json.NewEncoder(w).Encode(map[string]string{"key": "PC-9"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	key, err := newTestClient(srv).CreatePrecondition(context.Background(), "PROJ", "New PC", "Manual", "desc")
	if err != nil {
		t.Fatalf("CreatePrecondition: %v", err)
	}
	if key != "PC-9" {
		t.Fatalf("key = %q, want PC-9", key)
	}
	if gotIssueType["id"] != "77" {
		t.Fatalf("create used issuetype %v, want id 77", gotIssueType)
	}
}

// TestListPreconditionsNoTypeIsSoft verifies that an instance without a
// Precondition issue type yields empty results, not an error (sync must go on).
func TestListPreconditionsNoTypeIsSoft(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/2/issuetype" {
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "1", "name": "Bug"}})
			return
		}
		t.Errorf("should not search when no precondition type: %s", r.URL.Path)
	}))
	defer srv.Close()

	pres, links, err := newTestClient(srv).ListPreconditions(context.Background(), "PROJ")
	if err != nil {
		t.Fatalf("expected soft success, got %v", err)
	}
	if len(pres) != 0 || len(links) != 0 {
		t.Fatalf("want empty, got %d preconditions / %d links", len(pres), len(links))
	}
}
