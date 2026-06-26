package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// Folder mirrors a node in the Xray Test Repository tree (FR-13.1). ID and
// ParentID are full slash paths ("/Authentication/Login") so the tree is
// self-describing — the same model the local store and the folder CRUD use.
// TestCount is the number of Tests directly in the folder; TotalTestCount
// includes its descendants — both as Xray reports them, for the count badges
// the folder tree shows like the Xray Test Repository.
type Folder struct {
	ID             string
	ParentID       string
	Name           string
	XrayID         string // native Xray folder id, needed to move Tests on commit
	TestCount      int
	TotalTestCount int
}

// testRepositoryPath is the Xray Server/DC Test Repository REST base. The
// folder tree and per-folder test endpoints hang off it.
//
// NOTE(xtm): Xray Server/DC exposes the Test Repository under raven/1.0
// (issue/step operations elsewhere use raven/2.0). Verify the version and the
// response shapes against a live Xray Server 8.4.0 instance — parsing here is
// deliberately tolerant so a shape difference degrades to "no folders" rather
// than a hard sync failure.
const testRepositoryPath = "/rest/raven/1.0/api/testrepository"

// resolveFolderID resolves a slash-path Test Repository folder (e.g.
// "/Authentication/Login") to its native Xray folder id by fetching the folder
// tree and matching the path. The repository root ("" or "/") maps to "-1",
// matching MoveTestToFolder's root convention. A path the tree does not contain
// is an error directing the caller to sync first, since the tree is the only
// source of native ids and a stale local view can name a folder Xray no longer
// has.
//
// Paths are compared against FolderRef.Path, which flattenFolders builds as a
// leading-slash, no-trailing-slash string ("/Parent/Child"); the input is
// normalised to the same form before matching.
func (c *Client) resolveFolderID(ctx context.Context, projectKey, path string) (string, error) {
	norm := "/" + strings.Trim(strings.TrimSpace(path), "/")
	if norm == "/" {
		return "-1", nil
	}
	tree, err := c.FolderTree(ctx, projectKey)
	if err != nil {
		return "", err
	}
	for _, f := range tree.Folders {
		if f.ID == norm && f.XrayID != "" {
			return f.XrayID, nil
		}
	}
	return "", fmt.Errorf("folder %s not found; sync the Test Repository first", norm)
}

// CreateFolder adds a child folder named name under parentPath in the project's
// Test Repository tree (FR-13.3). Demo URLs short-circuit to a no-op. Live, it
// resolves parentPath to its native Xray folder id (root = "-1") and posts the
// new folder under it.
//
// Maps to POST /rest/raven/1.0/api/testrepository/{projectKey}/folders/{parentID}
// with {"name":...} (parentID = "-1" for the root). Verified against a live Xray
// Server/DC instance (RND_P_4TFINT_05-252): an earlier guess that appended a
// second "/folders" segment returned 404 "null for uri".
func (c *Client) CreateFolder(ctx context.Context, projectKey, parentPath, name string) error {
	if isDemoURL(c.baseURL) {
		return nil
	}
	parentID, err := c.resolveFolderID(ctx, projectKey, parentPath)
	if err != nil {
		return err
	}
	body := map[string]any{"name": name}
	return c.post(ctx, fmt.Sprintf("%s/%s/folders/%s", testRepositoryPath, projectKey, parentID), body)
}

// RenameFolder renames the folder at path to newName (FR-13.3). Demo URLs
// short-circuit to a no-op. Live, it resolves path to its native Xray folder id
// and updates the folder's name.
//
// NOTE(xtm): the rename endpoint and body are the best-understood raven/1.0
// shape (PUT .../folders/{folderID} with {"name":...}) and MUST be verified
// against a live Xray Server/DC 8.4.0 instance.
func (c *Client) RenameFolder(ctx context.Context, projectKey, path, newName string) error {
	if isDemoURL(c.baseURL) {
		return nil
	}
	folderID, err := c.resolveFolderID(ctx, projectKey, path)
	if err != nil {
		return err
	}
	body := map[string]any{"name": newName}
	return c.put(ctx, fmt.Sprintf("%s/%s/folders/%s", testRepositoryPath, projectKey, folderID), body)
}

// DeleteFolder removes the folder at path (FR-13.3). Demo URLs short-circuit to
// a no-op. Live, it resolves path to its native Xray folder id and deletes it.
//
// NOTE(xtm): the delete endpoint is the best-understood raven/1.0 shape
// (DELETE .../folders/{folderID}) and MUST be verified against a live Xray
// Server/DC 8.4.0 instance. Deletion may be restricted, and the behaviour for a
// non-empty folder (cascade vs reject) is instance-specific.
func (c *Client) DeleteFolder(ctx context.Context, projectKey, path string) error {
	if isDemoURL(c.baseURL) {
		return nil
	}
	folderID, err := c.resolveFolderID(ctx, projectKey, path)
	if err != nil {
		return err
	}
	return c.delete(ctx, fmt.Sprintf("%s/%s/folders/%s", testRepositoryPath, projectKey, folderID))
}

// MoveTestToFolder relocates a Test within the project's Test Repository tree
// (FR-13.3). folderID is the *native Xray folder id* (the engine resolves the
// folder path to it before calling), or "-1" for the repository root. A Test
// lives in exactly one repository folder, so adding it to the target folder
// moves it. Demo URLs short-circuit to a no-op.
//
// Maps to PUT /rest/raven/1.0/api/testrepository/{projectKey}/folders/{folderId}/tests
// with {"add":[testKey]} — the write counterpart of the GET used by the sync.
func (c *Client) MoveTestToFolder(ctx context.Context, projectKey, testKey, folderID string) error {
	if isDemoURL(c.baseURL) {
		return nil
	}
	if strings.TrimSpace(folderID) == "" {
		return fmt.Errorf(
			"target folder has no Xray id yet — sync the Test Repository, then move the test")
	}
	body := map[string]any{
		"add":    []string{testKey},
		"remove": []string{},
	}
	return c.put(ctx, fmt.Sprintf("%s/%s/folders/%s/tests", testRepositoryPath, projectKey, folderID), body)
}

// FolderRef pairs a folder's native Xray id with its slash path — what the
// membership walk needs to fetch a folder's Tests and stamp them.
type FolderRef struct {
	ID   string
	Path string
}

// FolderTreeResult is what a Test Repository tree fetch yields: the flattened
// path-based folders, any Test→path membership embedded directly in the tree
// response (some Xray versions include each folder's Test keys inline, letting
// us skip the per-folder calls), and the subset of folders that actually
// contain Tests (testCount > 0) so the membership walk skips the — often
// numerous — empty folders instead of making a wasted call per folder.
type FolderTreeResult struct {
	Folders          []Folder
	TreeMembership   map[string]string // testKey -> folder path, from the tree itself
	FoldersWithTests []FolderRef       // only folders whose testCount > 0
}

// ListFolders returns the flattened Test Repository folder tree for a project.
// Demo URLs short-circuit to a generated hierarchy.
func (c *Client) ListFolders(ctx context.Context, projectKey string) ([]Folder, error) {
	res, err := c.FolderTree(ctx, projectKey)
	if err != nil {
		return nil, err
	}
	return res.Folders, nil
}

// FolderTree fetches and parses the project's Test Repository tree. Demo URLs
// return the generated hierarchy (demo Tests carry their folder via the search
// generator, so there's no id map or embedded membership).
func (c *Client) FolderTree(ctx context.Context, projectKey string) (FolderTreeResult, error) {
	if isDemoURL(c.baseURL) {
		return FolderTreeResult{Folders: demoFolders(projectKey)}, nil
	}
	body, err := c.getBytes(ctx, fmt.Sprintf("%s/%s/folders", testRepositoryPath, projectKey))
	if err != nil {
		return FolderTreeResult{}, fmt.Errorf("list folders: %w", err)
	}
	children, err := parseFolderTree(body)
	if err != nil {
		return FolderTreeResult{}, err
	}
	res := FolderTreeResult{
		Folders:        []Folder{},
		TreeMembership: map[string]string{},
	}
	flattenFolders(children, "", &res)
	// Diagnostics for the "folders are empty" report: record what the tree
	// actually carried (folder count, how many hold Tests, embedded keys).
	log.Printf("xtm: FolderTree %s: %d folder(s), %d with tests, %d test(s) embedded in tree; raw=%s",
		projectKey, len(res.Folders), len(res.FoldersWithTests), len(res.TreeMembership), snippet(body, 1500))
	return res, nil
}

// folderNode mirrors one node in the Xray Test Repository tree response. The id
// is flexString because Xray returns it as a number. testCount is the number of
// Tests directly in the folder (Xray Server/DC includes it), so the membership
// walk can skip empty folders. Tests is kept raw because its element shape
// varies and not all versions include it.
type folderNode struct {
	ID             flexString      `json:"id"`
	Name           string          `json:"name"`
	TestCount      int             `json:"testCount"`
	TotalTestCount int             `json:"totalTestCount"`
	Folders        []folderNode    `json:"folders"`
	Tests          json.RawMessage `json:"tests"`
}

// parseFolderTree decodes the folder tree body into the top-level folders,
// tolerating the two shapes a real instance returns: a root object
// {"id":-1,"name":"…","folders":[…]} whose own node is the (unnamed) repository
// root we skip, or a bare array of top-level folders.
func parseFolderTree(body []byte) ([]folderNode, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	switch trimmed[0] {
	case '[':
		var nodes []folderNode
		if err := json.Unmarshal([]byte(trimmed), &nodes); err != nil {
			return nil, fmt.Errorf("decode folder tree: %w", err)
		}
		return nodes, nil
	case '{':
		var root folderNode
		if err := json.Unmarshal([]byte(trimmed), &root); err != nil {
			if msg := jiraErrorMessage([]byte(trimmed)); msg != "" {
				return nil, fmt.Errorf("xray could not return the folder tree: %s", msg)
			}
			return nil, fmt.Errorf("decode folder tree: %w", err)
		}
		return root.Folders, nil
	default:
		return nil, fmt.Errorf("unexpected folder tree response: %s", snippet([]byte(trimmed), 256))
	}
}

// flattenFolders walks the tree depth-first, emitting one Folder per node with
// a slash path id, recording id→path, and harvesting any Test keys embedded in
// the node into the result's membership map. Nodes with empty names are skipped
// (a stray root) but their children still recurse under the parent path.
func flattenFolders(nodes []folderNode, parentPath string, res *FolderTreeResult) {
	for _, n := range nodes {
		name := strings.TrimSpace(n.Name)
		path := parentPath
		if name != "" {
			path = parentPath + "/" + name
			res.Folders = append(res.Folders, Folder{
				ID:             path,
				ParentID:       parentPath,
				Name:           name,
				XrayID:         string(n.ID),
				TestCount:      n.TestCount,
				TotalTestCount: n.TotalTestCount,
			})
			if id := string(n.ID); id != "" && n.TestCount > 0 {
				res.FoldersWithTests = append(res.FoldersWithTests, FolderRef{ID: id, Path: path})
			}
			for _, key := range extractTestKeys(n.Tests) {
				res.TreeMembership[key] = path
			}
		}
		flattenFolders(n.Folders, path, res)
	}
}

// ListTestsInFolder returns the Jira keys of the Tests directly in one Test
// Repository folder, addressed by its native Xray folder id. Demo URLs return
// nothing (demo membership comes from the search generator).
func (c *Client) ListTestsInFolder(ctx context.Context, projectKey, folderID string) ([]string, error) {
	if isDemoURL(c.baseURL) {
		return nil, nil
	}
	body, err := c.getBytes(ctx, fmt.Sprintf("%s/%s/folders/%s/tests", testRepositoryPath, projectKey, folderID))
	if err != nil {
		return nil, err
	}
	keys, err := parseFolderTests(body)
	// Diagnostics for the "folders are empty" report: log the parsed key count,
	// the server-reported total (so page truncation is detectable), and a sample
	// key so an endpoint/shape mismatch is visible.
	if err == nil {
		var meta struct {
			Total int `json:"total"`
		}
		_ = json.Unmarshal(body, &meta)
		sample := ""
		if len(keys) > 0 {
			sample = keys[0]
		}
		log.Printf("xtm: folder %s/%s tests: %d keys parsed, server total=%d (e.g. %q); raw=%s",
			projectKey, folderID, len(keys), meta.Total, sample, snippet(body, 400))
	}
	return keys, err
}

// parseFolderTests pulls Test keys out of the per-folder tests response,
// tolerating a bare array (of key strings or test objects) or a {"tests":[…]}
// wrapper.
func parseFolderTests(body []byte) ([]string, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" || trimmed == "null" {
		return []string{}, nil
	}
	if trimmed[0] == '{' {
		// Either a {"tests":[…]} wrapper or a Jira/Xray error object.
		var wrapper struct {
			Tests json.RawMessage `json:"tests"`
		}
		if err := json.Unmarshal([]byte(trimmed), &wrapper); err == nil && len(wrapper.Tests) > 0 {
			return extractTestKeys(wrapper.Tests), nil
		}
		if msg := jiraErrorMessage([]byte(trimmed)); msg != "" {
			return nil, fmt.Errorf("xray could not return folder tests: %s", msg)
		}
		return nil, fmt.Errorf("unexpected folder tests response: %s", snippet([]byte(trimmed), 256))
	}
	if trimmed[0] != '[' {
		return nil, fmt.Errorf("unexpected folder tests response: %s", snippet([]byte(trimmed), 256))
	}
	return extractTestKeys([]byte(trimmed)), nil
}

// extractTestKeys pulls Jira keys out of a raw JSON array whose elements are
// either bare strings ("QA-1") or objects carrying a key under "key"/"testKey"/
// "jira_key". Malformed or empty input yields no keys (never an error) so it can
// be used opportunistically on embedded tree data.
func extractTestKeys(raw json.RawMessage) []string {
	trimmed := strings.TrimSpace(string(raw))
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil
	}
	// Try a string array first.
	var strs []string
	if err := json.Unmarshal([]byte(trimmed), &strs); err == nil {
		out := make([]string, 0, len(strs))
		for _, s := range strs {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	// Otherwise an array of objects with one of the known key fields.
	var objs []struct {
		Key     string `json:"key"`
		TestKey string `json:"testKey"`
		JiraKey string `json:"jira_key"`
	}
	if err := json.Unmarshal([]byte(trimmed), &objs); err != nil {
		return nil
	}
	out := make([]string, 0, len(objs))
	for _, o := range objs {
		switch {
		case o.Key != "":
			out = append(out, o.Key)
		case o.TestKey != "":
			out = append(out, o.TestKey)
		case o.JiraKey != "":
			out = append(out, o.JiraKey)
		}
	}
	return out
}
