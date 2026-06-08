package jira

import "testing"

// TestParseFolderTree_RootObject covers the common Xray Server/DC shape: a root
// object whose own node is skipped and whose children become top-level folders.
func TestParseFolderTree_RootObject(t *testing.T) {
	body := []byte(`{
		"id": -1, "name": "Test Repository",
		"folders": [
			{"id": 1, "name": "Authentication", "folders": [
				{"id": 5, "name": "Login", "testCount": 3, "folders": []}
			]},
			{"id": 2, "name": "Commerce", "testCount": 0, "folders": []}
		]
	}`)
	nodes, err := parseFolderTree(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := FolderTreeResult{
		Folders:        []Folder{},
		TreeMembership: map[string]string{},
	}
	flattenFolders(nodes, "", &res)
	folders := res.Folders

	if len(folders) != 3 {
		t.Fatalf("want 3 folders, got %d: %+v", len(folders), folders)
	}
	// Depth-first: Authentication, Authentication/Login, Commerce.
	if folders[0].ID != "/Authentication" || folders[0].ParentID != "" {
		t.Fatalf("unexpected top folder: %+v", folders[0])
	}
	if folders[1].ID != "/Authentication/Login" || folders[1].ParentID != "/Authentication" {
		t.Fatalf("unexpected child folder: %+v", folders[1])
	}
	// Only the folder with testCount > 0 (Login) should be queued for a
	// membership fetch — empty folders are skipped.
	if len(res.FoldersWithTests) != 1 ||
		res.FoldersWithTests[0].ID != "5" ||
		res.FoldersWithTests[0].Path != "/Authentication/Login" {
		t.Fatalf("FoldersWithTests wrong: %+v", res.FoldersWithTests)
	}
}

// TestParseFolderTree_BareArray covers instances that return the top-level
// folders as a bare array rather than wrapped in a root node.
func TestParseFolderTree_BareArray(t *testing.T) {
	body := []byte(`[{"id": 1, "name": "Smoke", "folders": []}]`)
	nodes, err := parseFolderTree(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := FolderTreeResult{
		Folders:        []Folder{},
		TreeMembership: map[string]string{},
	}
	flattenFolders(nodes, "", &res)
	if len(res.Folders) != 1 || res.Folders[0].ID != "/Smoke" {
		t.Fatalf("unexpected folders: %+v", res.Folders)
	}
}

// TestParseFolderTests covers both the bare-array and {"tests":[…]} wrapper
// shapes for a folder's member tests.
func TestParseFolderTests(t *testing.T) {
	arr := []byte(`[{"key": "QA-1"}, {"key": "QA-2"}, {"key": ""}]`)
	keys, err := parseFolderTests(arr)
	if err != nil {
		t.Fatalf("array: unexpected error: %v", err)
	}
	if len(keys) != 2 || keys[0] != "QA-1" || keys[1] != "QA-2" {
		t.Fatalf("array: unexpected keys: %v", keys)
	}

	wrapped := []byte(`{"tests": [{"key": "QA-9"}]}`)
	keys, err = parseFolderTests(wrapped)
	if err != nil {
		t.Fatalf("wrapper: unexpected error: %v", err)
	}
	if len(keys) != 1 || keys[0] != "QA-9" {
		t.Fatalf("wrapper: unexpected keys: %v", keys)
	}

	// Bare string array.
	strs := []byte(`["QA-3", "QA-4"]`)
	keys, err = parseFolderTests(strs)
	if err != nil {
		t.Fatalf("strings: unexpected error: %v", err)
	}
	if len(keys) != 2 || keys[0] != "QA-3" || keys[1] != "QA-4" {
		t.Fatalf("strings: unexpected keys: %v", keys)
	}
}

// TestFolderTreeEmbeddedMembership covers instances whose folder tree carries
// each folder's Test keys inline — membership is then harvested without any
// per-folder calls.
func TestFolderTreeEmbeddedMembership(t *testing.T) {
	body := []byte(`{
		"id": -1, "name": "Test Repository",
		"folders": [
			{"id": 1, "name": "Auth", "tests": ["QA-1", "QA-2"], "folders": [
				{"id": 5, "name": "Login", "tests": [{"key": "QA-7"}], "folders": []}
			]}
		]
	}`)
	nodes, err := parseFolderTree(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := FolderTreeResult{
		Folders:        []Folder{},
		TreeMembership: map[string]string{},
	}
	flattenFolders(nodes, "", &res)

	if res.TreeMembership["QA-1"] != "/Auth" || res.TreeMembership["QA-2"] != "/Auth" {
		t.Fatalf("top-folder membership wrong: %v", res.TreeMembership)
	}
	if res.TreeMembership["QA-7"] != "/Auth/Login" {
		t.Fatalf("nested membership wrong: %v", res.TreeMembership)
	}
}
