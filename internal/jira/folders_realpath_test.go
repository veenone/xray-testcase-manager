package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// folderTreeJSON is a Test Repository tree response in the shape
// parseFolderTree/flattenFolders consume: a root object (id -1, no name) whose
// folders carry numeric ids and nested children. resolveFolderID maps each
// slash path to the matching node id.
const folderTreeJSON = `{
  "id": -1,
  "name": "",
  "folders": [
    {
      "id": 10,
      "name": "Authentication",
      "testCount": 0,
      "folders": [
        {"id": 20, "name": "Login", "testCount": 0, "folders": []}
      ]
    }
  ]
}`

// newFolderTreeServer serves the canned tree for the folder-tree GET and routes
// every other request to handle. It records each non-tree request so a test can
// assert exactly one management call was made.
func newFolderTreeServer(t *testing.T, handle func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/folders") {
			_, _ = io.WriteString(w, folderTreeJSON)
			return
		}
		handle(w, r)
	}))
}

func TestResolveFolderID(t *testing.T) {
	srv := newFolderTreeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected non-tree request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	})
	defer srv.Close()
	c := NewClient(srv.URL, "t")
	ctx := context.Background()

	cases := []struct {
		path string
		want string
	}{
		{"/Authentication", "10"},
		{"/Authentication/Login", "20"},
		{"", "-1"},
		{"/", "-1"},
		{"/Authentication/Login/", "20"}, // trailing slash normalised
	}
	for _, tc := range cases {
		got, err := c.resolveFolderID(ctx, "QA", tc.path)
		if err != nil {
			t.Errorf("resolveFolderID(%q): %v", tc.path, err)
			continue
		}
		if got != tc.want {
			t.Errorf("resolveFolderID(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}

	if _, err := c.resolveFolderID(ctx, "QA", "/Nope"); err == nil {
		t.Error("resolveFolderID for unknown path: want error, got nil")
	} else if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unknown-path error = %q, want it to mention 'not found'", err.Error())
	}
}

func TestRealCreateFolder(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := newFolderTreeServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
	})
	defer srv.Close()

	c := NewClient(srv.URL, "t")
	if err := c.CreateFolder(context.Background(), "QA", "/Authentication", "Logout"); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	// Parent "/Authentication" resolves to id 10.
	wantPath := "/rest/raven/1.0/api/testrepository/QA/folders/10/folders"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
	if gotBody["name"] != "Logout" {
		t.Errorf("body name = %v, want Logout", gotBody["name"])
	}
}

func TestRealCreateFolderAtRoot(t *testing.T) {
	var gotPath string
	srv := newFolderTreeServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusCreated)
	})
	defer srv.Close()

	c := NewClient(srv.URL, "t")
	if err := c.CreateFolder(context.Background(), "QA", "/", "Top"); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	wantPath := "/rest/raven/1.0/api/testrepository/QA/folders/-1/folders"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
}

func TestRealRenameFolder(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := newFolderTreeServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
	})
	defer srv.Close()

	c := NewClient(srv.URL, "t")
	if err := c.RenameFolder(context.Background(), "QA", "/Authentication/Login", "SignIn"); err != nil {
		t.Fatalf("RenameFolder: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	wantPath := "/rest/raven/1.0/api/testrepository/QA/folders/20"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
	if gotBody["name"] != "SignIn" {
		t.Errorf("body name = %v, want SignIn", gotBody["name"])
	}
}

func TestRealDeleteFolder(t *testing.T) {
	var gotMethod, gotPath string
	srv := newFolderTreeServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
	})
	defer srv.Close()

	c := NewClient(srv.URL, "t")
	if err := c.DeleteFolder(context.Background(), "QA", "/Authentication/Login"); err != nil {
		t.Fatalf("DeleteFolder: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	wantPath := "/rest/raven/1.0/api/testrepository/QA/folders/20"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
}

// TestDemoFolderCRUDNoHTTP confirms the demo short-circuit makes zero HTTP
// calls for all three management methods.
func TestDemoFolderCRUDNoHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("demo path made an HTTP call: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	// A demo client is keyed off the base URL "demo", so no request can reach srv.
	c := NewClient("demo", "t")
	ctx := context.Background()
	if err := c.CreateFolder(ctx, "QA", "/Authentication", "X"); err != nil {
		t.Errorf("demo CreateFolder: %v", err)
	}
	if err := c.RenameFolder(ctx, "QA", "/Authentication", "Y"); err != nil {
		t.Errorf("demo RenameFolder: %v", err)
	}
	if err := c.DeleteFolder(ctx, "QA", "/Authentication"); err != nil {
		t.Errorf("demo DeleteFolder: %v", err)
	}
}
