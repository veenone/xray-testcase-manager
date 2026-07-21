// Package testrepo is the local repository for cached Xray Test data.
//
// It is the query layer behind the browse / search / filter experience
// (FR-11), the write target of the sync engine (FR-1), and the home of the
// local change-tracking and audit-log machinery (FR-1.5 / FR-1.6 / FR-12.6).
package testrepo

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os/user"
	"sort"
	"strings"
	"time"

	"xray-test-manager/internal/store"
)

// ErrNotFound is returned when a Test key is not in the local store.
var ErrNotFound = errors.New("test not found")

// TestCase is a Xray Test as cached locally.
type TestCase struct {
	Key         string   `json:"key"`
	ID          string   `json:"id"`
	Summary     string   `json:"summary"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	Priority    string   `json:"priority"`
	Labels      []string `json:"labels"`
	Components  []string `json:"components"`
	Updated     string   `json:"updated"`
	FolderID    string   `json:"folderId"`
	// ExecType is the Xray Test Type (a.k.a. execution type): Manual /
	// Automated / Generic / Cucumber. Empty when unknown / not yet synced.
	ExecType string `json:"execType"`
	// FixVersions are the standard Jira Fix Version(s) assigned to this Test
	// issue. Populated on sync from test_case.fix_versions (JSON array). Empty
	// when none are set or the Test has not yet been synced.
	FixVersions []string `json:"fixVersions"`
	// Non-Manual test bodies. CucumberScenario holds the Gherkin text and
	// CucumberType its scenario kind ("Scenario"/"Scenario Outline") for
	// Cucumber tests; GenericDefinition holds the plain-text definition for
	// Generic tests. Empty for other types.
	CucumberScenario  string `json:"cucumberScenario"`
	CucumberType      string `json:"cucumberType"`
	GenericDefinition string `json:"genericDefinition"`
}

// Folder is one node in the Xray Test Repository tree (FR-13.1). The ID is
// the folder's full path ("/Authentication/Login"), so ParentID + Name + ID
// together describe the tree without any extra lookup tables. TestCount is the
// Tests directly in the folder; TotalTestCount includes its descendants — the
// counts the tree shows beside each folder, like Xray's Test Repository.
type Folder struct {
	ID             string `json:"id"`
	ParentID       string `json:"parentId"`
	Name           string `json:"name"`
	XrayID         string `json:"xrayId"` // native Xray folder id, used to commit moves
	TestCount      int    `json:"testCount"`
	TotalTestCount int    `json:"totalTestCount"`
}

// Precondition mirrors a Xray Precondition issue (FR-13.4).
type Precondition struct {
	Key         string `json:"key"`
	Summary     string `json:"summary"`
	Type        string `json:"type"`
	Description string `json:"description"`
	// Condition is the Xray precondition definition text — distinct from the
	// Jira issue description. Empty when the precondition has not been given one
	// or when synced from a live Jira instance (the custom-field id is
	// instance-specific; see NOTE(xtm) in internal/jira/preconditions.go).
	Condition string `json:"condition"`
}

// Container is a cached Xray Test Set, Test Plan or Test Execution (FR-1.3).
// Kind is one of "testset" / "testplan" / "testexec" — the three share a shape
// and differ only in how they relate to Tests.
type Container struct {
	Key           string `json:"key"`
	Kind          string `json:"kind"`
	Summary       string `json:"summary"`
	Status        string `json:"status"`
	ParentKey     string `json:"parentKey"`
	ParentSummary string `json:"parentSummary"`
	IssueType     string `json:"issueType"`
	// Environments are the Xray Test Environments assigned to a Test Execution
	// (empty for Test Sets / Plans, which have no such field).
	Environments []string `json:"environments"`
	// FixVersions are the standard Jira Fix Version(s) on a Test Execution
	// (empty for Test Sets / Plans). Read-only display values pulled from Jira;
	// never edited locally, so they are overwritten on every sync.
	FixVersions []string `json:"fixVersions"`
	// Created, Updated, and Resolved are the ISO-8601 timestamps from the Test
	// Execution issue (Jira created/updated/resolutiondate fields). Empty for
	// non-execution containers or when not yet fetched. Read-only; overwritten
	// on every sync.
	Created  string `json:"created"`
	Updated  string `json:"updated"`
	Resolved string `json:"resolved"`
	// Description is the Jira issue description (markdown/wiki text), fetched
	// for all container kinds and cached in the local store.
	Description string `json:"description"`
}

// ContainerQuery filters a ListContainersQuery call. Kind is required (one of
// "testset" / "testplan" / "testexec"); Environment, when set, keeps only
// containers whose environments array contains that value (membership test).
type ContainerQuery struct {
	Kind        string `json:"kind"`
	Environment string `json:"environment"` // empty = any environment
}

// ContainerLink is one Test's membership in a Container. RunStatus carries the
// Test Run result for execution memberships and is empty for sets / plans.
type ContainerLink struct {
	ContainerKey string `json:"containerKey"`
	TestKey      string `json:"testKey"`
	RunStatus    string `json:"runStatus"`
}

// ContainerMembership is a Container a Test belongs to, joined with that Test's
// run status — what the detail panel lists under Test Sets / Plans /
// Executions.
type ContainerMembership struct {
	Key       string `json:"key"`
	Kind      string `json:"kind"`
	Summary   string `json:"summary"`
	Status    string `json:"status"`
	RunStatus string `json:"runStatus"`
}

// TestPlanBoardRow is one Test within a Test Plan board (FR-13.7): the Test's
// workflow status plus its consolidated execution result across the Test
// Executions it appears in.
type TestPlanBoardRow struct {
	TestKey   string `json:"testKey"`
	Summary   string `json:"summary"`
	Status    string `json:"status"`
	RunStatus string `json:"runStatus"`
	// IsExternal is true when this member Test has no local test_case row (it
	// lives in a different Jira project than the profile's) and its summary /
	// status come from the external_test cache instead.
	IsExternal bool `json:"isExternal"`
}

// TestPlanBoard is the read-only board for one Test Plan (FR-13.7) — its member
// Tests with consolidated execution status, plus a run-status histogram for the
// header.
type TestPlanBoard struct {
	Key     string `json:"key"`
	Summary string `json:"summary"`
	// Description is the Jira issue description of the container (markdown/wiki
	// text), passed through for display in the detail sidebar.
	Description string             `json:"description"`
	Rows        []TestPlanBoardRow `json:"rows"`
	RunCounts   []Bucket           `json:"runCounts"`
}

// Step is one cached Xray Test Step (FR-2.5). XrayID is Xray's per-step
// identifier — kept on the row so the future edit-steps API can target
// each step individually without us having to rebuild the list.
type Step struct {
	XrayID   string `json:"xrayId"`
	Index    int    `json:"index"`
	Action   string `json:"action"`
	Data     string `json:"data"`
	Expected string `json:"expected"`
	// CalledTestKey is set when this step calls another Test (an Xray "test
	// call") instead of being a manual action/data/expected step.
	CalledTestKey string `json:"calledTestKey"`
}

// PendingChange is one uncommitted local edit awaiting a push (FR-1.5 / 1.6).
// At most one PendingChange exists per (profile, entity, field); repeated
// edits to the same field are coalesced — BeforeVal stays at the original
// value, AfterVal advances. Reverting to the original removes the row.
type PendingChange struct {
	ID          int64  `json:"id"`
	EntityType  string `json:"entityType"`
	EntityKey   string `json:"entityKey"`
	Field       string `json:"field"`
	BeforeVal   string `json:"beforeVal"`
	AfterVal    string `json:"afterVal"`
	BaseVersion string `json:"baseVersion"`
	CreatedAt   string `json:"createdAt"`
}

// AuditEntry is one row of the local audit trail (FR-12.6 / NFR-13). Every
// local change records who / what / when / before → after.
type AuditEntry struct {
	ID         int64  `json:"id"`
	OccurredAt string `json:"occurredAt"`
	Actor      string `json:"actor"`
	EntityType string `json:"entityType"`
	EntityKey  string `json:"entityKey"`
	Action     string `json:"action"`
	Field      string `json:"field"`
	BeforeVal  string `json:"beforeVal"`
	AfterVal   string `json:"afterVal"`
	Note       string `json:"note"`
}

// Query drives a ListTests call: free-text search, filters, sorting, paging.
type Query struct {
	Search       string `json:"search"`
	Status       string `json:"status"`
	FolderID     string `json:"folderId"`     // empty = any folder
	ContainerKey string `json:"containerKey"` // empty = any container (FR-11.6)
	Component    string `json:"component"`    // empty = any component (group-by component)
	ExecType     string `json:"execType"`     // empty = any execution type (Manual/Automated/Generic/Cucumber)
	Review       string `json:"review"`       // "approved"|"rejected"|"pending"|"unreviewed"|"" = any
	SortBy       string `json:"sortBy"`
	Desc         bool   `json:"desc"`
	Limit        int    `json:"limit"`
	Offset       int    `json:"offset"`
}

// Page is one page of list results plus the total matching count.
type Page struct {
	Tests []TestCase `json:"tests"`
	Total int        `json:"total"`
}

// SyncState records the outcome of the last sync for a profile.
type SyncState struct {
	ProfileID    string `json:"profileId"`
	LastSyncedAt string `json:"lastSyncedAt"`
	TestCount    int    `json:"testCount"`
}

// Statistics is a per-profile rollup of the local Test cache for the dashboard
// (FR-9). Everything here is computed from SQLite with no Jira round-trips
// (FR-9.5). Test type and execution coverage (FR-9.3/9.4) are absent because
// the sync doesn't store them yet.
type Statistics struct {
	Total          int      `json:"total"`
	PendingChanges int      `json:"pendingChanges"`
	ExecutedTests  int      `json:"executedTests"`
	TestSets       int      `json:"testSets"`
	TestPlans      int      `json:"testPlans"`
	TestExecutions int      `json:"testExecutions"`
	TestsInSet     int      `json:"testsInSet"`
	TestsInPlan    int      `json:"testsInPlan"`
	ByStatus       []Bucket `json:"byStatus"`
	ByPriority     []Bucket `json:"byPriority"`
	ByLabel        []Bucket `json:"byLabel"`
	ByFolder       []Bucket `json:"byFolder"`
	ByComponent    []Bucket `json:"byComponent"`
	UpdatedTrend   []Bucket `json:"updatedTrend"`
	ByRunStatus    []Bucket `json:"byRunStatus"`
	ByCoverage     []Bucket `json:"byCoverage"`
	ByRequirement  []Bucket `json:"byRequirement"`
}

// Bucket is one (label, count) pair in a distribution — also used for the
// month-keyed update trend.
type Bucket struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

// sortColumns whitelists user-supplied sort keys to real columns, so Query.SortBy
// can never reach the SQL string directly.
var sortColumns = map[string]string{
	"key":     "jira_key",
	"summary": "summary",
	"status":  "status",
	"updated": "updated_at",
}

// keyNumericOrderExpr returns a SQL ORDER-BY expression that sorts a Jira-key
// column by its trailing issue number *numerically* — so QA-2 sorts before
// QA-10 before QA-100 — instead of the lexical "-1, -10, -100" order a plain
// string sort produces (RND_P_4TFINT_05-202 / -205). It strips the trailing
// digits to find where the number starts, then CASTs the remainder to an
// integer; keys with no trailing digits collapse to 0 and fall back to the
// caller's prefix tiebreaker. col must be a trusted column name, never user
// input.
func keyNumericOrderExpr(col string) string {
	return fmt.Sprintf("CAST(substr(%[1]s, length(rtrim(%[1]s, '0123456789')) + 1) AS INTEGER)", col)
}

// editableFields whitelists which Test fields can be edited via EditTestField.
// Status transitions need workflow logic and are handled separately in a
// later slice (FR-4.2).
var editableFields = map[string]string{
	"summary":            "summary",
	"description":        "description",
	"priority":           "priority",
	"labels":             "labels",
	"exec_type":          "exec_type",
	"cucumber_scenario":  "cucumber_scenario",
	"cucumber_type":      "cucumber_type",
	"generic_definition": "generic_definition",
}

// columnForField returns the test_case column corresponding to a field
// name. It includes 'status' — which isn't free-text editable (status is
// changed via TransitionTest, not EditTestField) but is still tracked in
// pending_change rows and needs a column lookup for the discard / sync
// paths.
func columnForField(field string) (string, bool) {
	if c, ok := editableFields[field]; ok {
		return c, true
	}
	if field == "status" {
		return "status", true
	}
	if field == "folder" {
		// Test Repository moves (FR-13.3) are tracked like a field change but
		// committed via the test-repository API, not a plain issue PUT.
		return "folder_id", true
	}
	return "", false
}

// Repository reads and writes cached data, scoped per profile.
type Repository struct {
	db *sql.DB
}

// NewRepository returns a repository backed by the given store.
func NewRepository(s *store.Store) *Repository {
	return &Repository{db: s.DB()}
}

// UpsertTests inserts or updates a batch of Tests in one transaction.
//
// For Tests that have local pending edits, the ON CONFLICT clause preserves
// the user's edited value for each editable field that has a pending row —
// only fields without a pending change are overwritten by the incoming
// sync. base_version on the pending change stays untouched, so commit-time
// conflict detection (FR-1.4, next slice) still has the original watermark
// to compare against.
func (r *Repository) UpsertTests(profileID string, tests []TestCase) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Each editable field's UPDATE branch checks for a pending change on
	// that (profile, test, field) and, if one exists, keeps the existing
	// local value instead of overwriting from the incoming sync.
	stmt, err := tx.Prepare(
		`INSERT INTO test_case
		   (profile_id, jira_key, jira_id, summary, description, status, priority, labels, updated_at, folder_id, components, exec_type, fix_versions, cucumber_scenario, cucumber_type, generic_definition)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(profile_id, jira_key) DO UPDATE SET
		   jira_id      = excluded.jira_id,
		   components   = excluded.components,
		   fix_versions = excluded.fix_versions,
		   exec_type    = CASE WHEN EXISTS (
		       SELECT 1 FROM pending_change
		       WHERE pending_change.profile_id  = excluded.profile_id
		         AND pending_change.entity_type = 'test_case'
		         AND pending_change.entity_key  = excluded.jira_key
		         AND pending_change.field       = 'exec_type'
		     ) THEN test_case.exec_type ELSE excluded.exec_type END,
		   summary      = CASE WHEN EXISTS (
		       SELECT 1 FROM pending_change
		       WHERE pending_change.profile_id  = excluded.profile_id
		         AND pending_change.entity_type = 'test_case'
		         AND pending_change.entity_key  = excluded.jira_key
		         AND pending_change.field       = 'summary'
		     ) THEN test_case.summary ELSE excluded.summary END,
		   description  = CASE WHEN EXISTS (
		       SELECT 1 FROM pending_change
		       WHERE pending_change.profile_id  = excluded.profile_id
		         AND pending_change.entity_type = 'test_case'
		         AND pending_change.entity_key  = excluded.jira_key
		         AND pending_change.field       = 'description'
		     ) THEN test_case.description ELSE excluded.description END,
		   status       = CASE WHEN EXISTS (
		       SELECT 1 FROM pending_change
		       WHERE pending_change.profile_id  = excluded.profile_id
		         AND pending_change.entity_type = 'test_case'
		         AND pending_change.entity_key  = excluded.jira_key
		         AND pending_change.field       = 'status'
		     ) THEN test_case.status ELSE excluded.status END,
		   priority     = CASE WHEN EXISTS (
		       SELECT 1 FROM pending_change
		       WHERE pending_change.profile_id  = excluded.profile_id
		         AND pending_change.entity_type = 'test_case'
		         AND pending_change.entity_key  = excluded.jira_key
		         AND pending_change.field       = 'priority'
		     ) THEN test_case.priority ELSE excluded.priority END,
		   labels       = CASE WHEN EXISTS (
		       SELECT 1 FROM pending_change
		       WHERE pending_change.profile_id  = excluded.profile_id
		         AND pending_change.entity_type = 'test_case'
		         AND pending_change.entity_key  = excluded.jira_key
		         AND pending_change.field       = 'labels'
		     ) THEN test_case.labels ELSE excluded.labels END,
		   updated_at   = excluded.updated_at,
		   folder_id    = CASE WHEN EXISTS (
		       SELECT 1 FROM pending_change
		       WHERE pending_change.profile_id  = excluded.profile_id
		         AND pending_change.entity_type = 'test_case'
		         AND pending_change.entity_key  = excluded.jira_key
		         AND pending_change.field       = 'folder'
		     ) THEN test_case.folder_id ELSE excluded.folder_id END,
		   cucumber_scenario  = CASE WHEN EXISTS (
		       SELECT 1 FROM pending_change
		       WHERE pending_change.profile_id  = excluded.profile_id
		         AND pending_change.entity_type = 'test_case'
		         AND pending_change.entity_key  = excluded.jira_key
		         AND pending_change.field       = 'cucumber_scenario'
		     ) THEN test_case.cucumber_scenario ELSE excluded.cucumber_scenario END,
		   cucumber_type      = CASE WHEN EXISTS (
		       SELECT 1 FROM pending_change
		       WHERE pending_change.profile_id  = excluded.profile_id
		         AND pending_change.entity_type = 'test_case'
		         AND pending_change.entity_key  = excluded.jira_key
		         AND pending_change.field       = 'cucumber_type'
		     ) THEN test_case.cucumber_type ELSE excluded.cucumber_type END,
		   generic_definition = CASE WHEN EXISTS (
		       SELECT 1 FROM pending_change
		       WHERE pending_change.profile_id  = excluded.profile_id
		         AND pending_change.entity_type = 'test_case'
		         AND pending_change.entity_key  = excluded.jira_key
		         AND pending_change.field       = 'generic_definition'
		     ) THEN test_case.generic_definition ELSE excluded.generic_definition END`)
	if err != nil {
		return fmt.Errorf("prepare upsert: %w", err)
	}
	defer stmt.Close()

	for _, t := range tests {
		if _, err := stmt.Exec(
			profileID, t.Key, t.ID, t.Summary, t.Description,
			t.Status, t.Priority, strings.Join(t.Labels, " "),
			t.Updated, t.FolderID, encodeComponents(t.Components), t.ExecType,
			encodeFixVersions(t.FixVersions),
			t.CucumberScenario, t.CucumberType, t.GenericDefinition,
		); err != nil {
			return fmt.Errorf("upsert %s: %w", t.Key, err)
		}
	}
	return tx.Commit()
}

// UpsertFolders inserts or updates a batch of Test Repository folders.
func (r *Repository) UpsertFolders(profileID string, folders []Folder) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(
		`INSERT INTO test_folder (profile_id, id, parent_id, name, test_count, total_test_count, xray_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(profile_id, id) DO UPDATE SET
		   parent_id        = excluded.parent_id,
		   name             = excluded.name,
		   test_count       = excluded.test_count,
		   total_test_count = excluded.total_test_count,
		   xray_id          = CASE WHEN excluded.xray_id <> '' THEN excluded.xray_id ELSE test_folder.xray_id END`)
	if err != nil {
		return fmt.Errorf("prepare upsert folder: %w", err)
	}
	defer stmt.Close()

	for _, f := range folders {
		if _, err := stmt.Exec(profileID, f.ID, f.ParentID, f.Name, f.TestCount, f.TotalTestCount, f.XrayID); err != nil {
			return fmt.Errorf("upsert folder %s: %w", f.ID, err)
		}
	}
	return tx.Commit()
}

// FolderXrayID resolves a folder path to its native Xray folder id, for
// committing a Test move (the Xray move endpoint addresses folders by id, not
// path). The repository root (empty path) maps to "-1". A path with no synced
// folder, or one synced before xray_id was captured, returns "" so the caller
// can surface a clear "sync first" error.
func (r *Repository) FolderXrayID(profileID, path string) (string, error) {
	if path == "" {
		return "-1", nil
	}
	var id string
	err := r.db.QueryRow(
		`SELECT xray_id FROM test_folder WHERE profile_id = ? AND id = ?`,
		profileID, path,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("resolve folder xray id: %w", err)
	}
	return id, nil
}

// ApplyTestFolders stamps the folder_id of each Test named in the map
// (testKey -> folder path), used by the sync engine to record Test Repository
// membership pulled from Xray (FR-13.1). A Test with a pending local folder
// move is left untouched so the sync can't clobber an uncommitted edit (mirrors
// the per-field guard in UpsertTests). Unknown Test keys are ignored.
func (r *Repository) ApplyTestFolders(profileID string, testFolder map[string]string) error {
	if len(testFolder) == 0 {
		return nil
	}
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(
		`UPDATE test_case SET folder_id = ?
		 WHERE profile_id = ? AND jira_key = ?
		   AND NOT EXISTS (
		       SELECT 1 FROM pending_change
		       WHERE pending_change.profile_id  = ?
		         AND pending_change.entity_type = 'test_case'
		         AND pending_change.entity_key  = ?
		         AND pending_change.field       = 'folder'
		   )`)
	if err != nil {
		return fmt.Errorf("prepare apply folder: %w", err)
	}
	defer stmt.Close()

	for testKey, path := range testFolder {
		if _, err := stmt.Exec(path, profileID, testKey, profileID, testKey); err != nil {
			return fmt.Errorf("apply folder for %s: %w", testKey, err)
		}
	}
	return tx.Commit()
}

// ListFolders returns the folder tree for a profile, ordered by id (which is
// the path, so a stable depth-first ordering falls out naturally).
func (r *Repository) ListFolders(profileID string) ([]Folder, error) {
	rows, err := r.db.Query(
		`SELECT id, parent_id, name, xray_id
		 FROM test_folder WHERE profile_id = ? ORDER BY id`,
		profileID)
	if err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}
	out := []Folder{}
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.ID, &f.ParentID, &f.Name, &f.XrayID); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	// Count tests from the local cache so each folder's badge equals exactly
	// what the grid shows when that folder is selected — the grid filters on
	// "folder_id = path OR folder_id LIKE path/%", so TestCount is the direct
	// count and TotalTestCount rolls in descendants the same way. (Using the
	// Xray-reported counts instead drifts by any test Xray counts but the local
	// cache doesn't hold — e.g. out-of-scope or unsynced tests.)
	direct, err := r.folderDirectCounts(profileID)
	if err != nil {
		return nil, err
	}
	for i := range out {
		f := &out[i]
		f.TestCount = direct[f.ID]
		total := f.TestCount
		prefix := f.ID + "/"
		for fid, n := range direct {
			if strings.HasPrefix(fid, prefix) {
				total += n
			}
		}
		f.TotalTestCount = total
	}
	return out, nil
}

// folderDirectCounts returns the number of Tests whose folder_id is exactly each
// folder path (direct membership), for the local folder-count roll-up.
func (r *Repository) folderDirectCounts(profileID string) (map[string]int, error) {
	rows, err := r.db.Query(
		`SELECT folder_id, COUNT(*) FROM test_case
		 WHERE profile_id = ? AND folder_id <> '' GROUP BY folder_id`,
		profileID)
	if err != nil {
		return nil, fmt.Errorf("count tests per folder: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var fid string
		var n int
		if err := rows.Scan(&fid, &n); err != nil {
			return nil, err
		}
		out[fid] = n
	}
	return out, rows.Err()
}

// UpsertPreconditions inserts or updates a batch of Preconditions.
func (r *Repository) UpsertPreconditions(profileID string, preconditions []Precondition) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(
		`INSERT INTO precondition (profile_id, jira_key, summary, type, description, condition)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(profile_id, jira_key) DO UPDATE SET
		   summary     = excluded.summary,
		   type        = excluded.type,
		   description = excluded.description,
		   condition   = excluded.condition`)
	if err != nil {
		return fmt.Errorf("prepare upsert precondition: %w", err)
	}
	defer stmt.Close()

	for _, p := range preconditions {
		if _, err := stmt.Exec(profileID, p.Key, p.Summary, p.Type, p.Description, p.Condition); err != nil {
			return fmt.Errorf("upsert precondition %s: %w", p.Key, err)
		}
	}
	return tx.Commit()
}

// ReplaceAllTestPreconditions wipes a profile's Test-to-Precondition link
// table and rewrites it from the provided map. Used by FullSync so removed
// links actually disappear on resync.
func (r *Repository) ReplaceAllTestPreconditions(profileID string, links map[string][]string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(
		`DELETE FROM test_precondition WHERE profile_id = ?`, profileID,
	); err != nil {
		return fmt.Errorf("clear precondition links: %w", err)
	}

	if len(links) == 0 {
		return tx.Commit()
	}

	stmt, err := tx.Prepare(
		`INSERT INTO test_precondition (profile_id, test_key, precondition_key)
		 VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare insert link: %w", err)
	}
	defer stmt.Close()

	for testKey, preKeys := range links {
		for _, pk := range preKeys {
			if _, err := stmt.Exec(profileID, testKey, pk); err != nil {
				return fmt.Errorf("link %s -> %s: %w", testKey, pk, err)
			}
		}
	}
	return tx.Commit()
}

// ListTestPreconditions returns the Preconditions linked to a Test.
func (r *Repository) ListTestPreconditions(profileID, testKey string) ([]Precondition, error) {
	rows, err := r.db.Query(
		`SELECT p.jira_key, p.summary, p.type, p.description, p.condition
		 FROM test_precondition tp
		 JOIN precondition p
		   ON p.profile_id = tp.profile_id AND p.jira_key = tp.precondition_key
		 WHERE tp.profile_id = ? AND tp.test_key = ?
		 ORDER BY p.jira_key`,
		profileID, testKey)
	if err != nil {
		return nil, fmt.Errorf("list test preconditions: %w", err)
	}
	defer rows.Close()

	out := []Precondition{}
	for rows.Next() {
		var p Precondition
		if err := rows.Scan(&p.Key, &p.Summary, &p.Type, &p.Description, &p.Condition); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListAllPreconditions returns every cached Precondition for a profile,
// ordered by key — the master list the association pickers draw from
// (FR-13.5 / 13.6).
func (r *Repository) ListAllPreconditions(profileID string) ([]Precondition, error) {
	rows, err := r.db.Query(
		`SELECT jira_key, summary, type, description, condition FROM precondition
		 WHERE profile_id = ? ORDER BY jira_key`, profileID)
	if err != nil {
		return nil, fmt.Errorf("list preconditions: %w", err)
	}
	defer rows.Close()

	out := []Precondition{}
	for rows.Next() {
		var p Precondition
		if err := rows.Scan(&p.Key, &p.Summary, &p.Type, &p.Description, &p.Condition); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// EditPreconditionField applies a local edit to one field of a Precondition
// (FR-13.5) — summary or description — coalescing it into a per-field pending
// change keyed by the Precondition's own key. Commit pushes it as an issue
// update. Reverting to the original value drops the pending change.
func (r *Repository) EditPreconditionField(profileID, preconditionKey, field, newValue string) error {
	col, ok := preconditionFields[field]
	if !ok {
		return fmt.Errorf("precondition field %q is not editable", field)
	}

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var currentVal string
	readSQL := fmt.Sprintf(
		`SELECT %s FROM precondition WHERE profile_id = ? AND jira_key = ?`, col,
	)
	err = tx.QueryRow(readSQL, profileID, preconditionKey).Scan(&currentVal)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read precondition value: %w", err)
	}
	if currentVal == newValue {
		return nil
	}

	updateSQL := fmt.Sprintf(
		`UPDATE precondition SET %s = ? WHERE profile_id = ? AND jira_key = ?`, col,
	)
	if _, err := tx.Exec(updateSQL, newValue, profileID, preconditionKey); err != nil {
		return fmt.Errorf("update precondition: %w", err)
	}
	// Preconditions have no cached updated_at, so there's no per-issue
	// conflict watermark — commit is best-effort last-writer-wins.
	if err := upsertPendingChange(
		tx, profileID, entityPreconditionEdit, preconditionKey, field, currentVal, newValue, "",
	); err != nil {
		return err
	}
	if err := writeAudit(
		tx, profileID, entityPreconditionEdit, preconditionKey,
		"edit-precondition-local", field, currentVal, newValue, "",
	); err != nil {
		return err
	}
	return tx.Commit()
}

// SetTestPreconditions replaces a Test's Precondition associations with the
// given set and queues the change for commit (FR-13.5). The whole desired set
// is stored as one pending row (before / after key-lists) rather than per-link
// add/remove rows, so associating and disassociating share one mechanism and
// reverting to the original set drops the row. No-op when the set is unchanged.
func (r *Repository) SetTestPreconditions(profileID, testKey string, precondKeys []string) error {
	newSet := uniqueSorted(precondKeys)

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var baseVersion string
	err = tx.QueryRow(
		`SELECT updated_at FROM test_case WHERE profile_id = ? AND jira_key = ?`,
		profileID, testKey,
	).Scan(&baseVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read test: %w", err)
	}

	currentSet, err := preconditionKeysTx(tx, profileID, testKey)
	if err != nil {
		return err
	}
	if equalOrder(currentSet, newSet) {
		return nil
	}

	if _, err := tx.Exec(
		`DELETE FROM test_precondition WHERE profile_id = ? AND test_key = ?`,
		profileID, testKey,
	); err != nil {
		return fmt.Errorf("clear precondition links: %w", err)
	}
	for _, pk := range newSet {
		if _, err := tx.Exec(
			`INSERT INTO test_precondition (profile_id, test_key, precondition_key)
			 VALUES (?, ?, ?)`, profileID, testKey, pk,
		); err != nil {
			return fmt.Errorf("insert precondition link: %w", err)
		}
	}

	beforeJSON, err := json.Marshal(currentSet)
	if err != nil {
		return fmt.Errorf("marshal current preconditions: %w", err)
	}
	afterJSON, err := json.Marshal(newSet)
	if err != nil {
		return fmt.Errorf("marshal new preconditions: %w", err)
	}
	if err := upsertPendingChange(
		tx, profileID, entityPreconditionSet, testKey, "preconditions",
		string(beforeJSON), string(afterJSON), baseVersion,
	); err != nil {
		return err
	}
	if err := writeAudit(
		tx, profileID, entityPreconditionSet, testKey,
		"set-preconditions-local", "preconditions",
		string(beforeJSON), string(afterJSON), "",
	); err != nil {
		return err
	}
	return tx.Commit()
}

// BulkAssociatePreconditions adds (add=true) or removes (add=false) the given
// Preconditions across a batch of Tests (FR-13.6), computing each Test's new
// set and queuing it via SetTestPreconditions. A Test already in the desired
// state is reported as succeeded.
func (r *Repository) BulkAssociatePreconditions(profileID string, testKeys, precondKeys []string, add bool) (BulkEditResult, error) {
	result := BulkEditResult{Succeeded: []string{}, Failed: []BulkFailure{}}
	for _, testKey := range testKeys {
		current, err := r.preconditionKeys(profileID, testKey)
		if err != nil {
			result.Failed = append(result.Failed, BulkFailure{TestKey: testKey, Error: err.Error()})
			continue
		}
		newSet := applyPreconditionDelta(current, precondKeys, add)
		if err := r.SetTestPreconditions(profileID, testKey, newSet); err != nil {
			result.Failed = append(result.Failed, BulkFailure{TestKey: testKey, Error: err.Error()})
			continue
		}
		result.Succeeded = append(result.Succeeded, testKey)
	}
	return result, nil
}

// BulkReplacePreconditions swaps Preconditions across a batch of Tests: for each
// Test it removes toRemove and adds toAdd in one apply, computing the new set as
// (current minus toRemove) plus toAdd and queuing it via SetTestPreconditions
// (FR-13.6). A Test already in the desired state is reported as succeeded.
func (r *Repository) BulkReplacePreconditions(profileID string, testKeys, toRemove, toAdd []string) (BulkEditResult, error) {
	result := BulkEditResult{Succeeded: []string{}, Failed: []BulkFailure{}}
	for _, testKey := range testKeys {
		current, err := r.preconditionKeys(profileID, testKey)
		if err != nil {
			result.Failed = append(result.Failed, BulkFailure{TestKey: testKey, Error: err.Error()})
			continue
		}
		newSet := applyReplaceDelta(current, toRemove, toAdd)
		if err := r.SetTestPreconditions(profileID, testKey, newSet); err != nil {
			result.Failed = append(result.Failed, BulkFailure{TestKey: testKey, Error: err.Error()})
			continue
		}
		result.Succeeded = append(result.Succeeded, testKey)
	}
	return result, nil
}

// preconditionKeys returns the Precondition keys currently linked to a Test.
func (r *Repository) preconditionKeys(profileID, testKey string) ([]string, error) {
	rows, err := r.db.Query(
		`SELECT precondition_key FROM test_precondition
		 WHERE profile_id = ? AND test_key = ? ORDER BY precondition_key`,
		profileID, testKey)
	if err != nil {
		return nil, fmt.Errorf("read precondition keys: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// preconditionKeysTx is preconditionKeys within an open transaction.
func preconditionKeysTx(tx *sql.Tx, profileID, testKey string) ([]string, error) {
	rows, err := tx.Query(
		`SELECT precondition_key FROM test_precondition
		 WHERE profile_id = ? AND test_key = ? ORDER BY precondition_key`,
		profileID, testKey)
	if err != nil {
		return nil, fmt.Errorf("read precondition keys: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// uniqueSorted returns the input with duplicates removed and sorted, so a
// Precondition set compares stably regardless of input order.
func uniqueSorted(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// applyPreconditionDelta returns the sorted set produced by adding or removing
// delta keys from current.
func applyPreconditionDelta(current, delta []string, add bool) []string {
	set := make(map[string]struct{}, len(current))
	for _, k := range current {
		set[k] = struct{}{}
	}
	for _, k := range delta {
		if add {
			set[k] = struct{}{}
		} else {
			delete(set, k)
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return uniqueSorted(out)
}

// applyReplaceDelta returns the sorted set produced by removing toRemove from
// current and then adding toAdd. Removal happens first so a key present in both
// lists ends up added. Shared by the bulk precondition / requirement swap paths.
func applyReplaceDelta(current, toRemove, toAdd []string) []string {
	set := make(map[string]struct{}, len(current))
	for _, k := range current {
		set[k] = struct{}{}
	}
	for _, k := range toRemove {
		delete(set, k)
	}
	for _, k := range toAdd {
		set[k] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return uniqueSorted(out)
}

// CreatePrecondition creates a new Precondition locally and queues it for
// creation in Jira on commit (FR-13.5). It gets a temporary key until commit
// POSTs the issue and learns the real one; associations made against the temp
// key are rewritten to the real key on commit (see RenamePrecondition).
// Returns the temporary key so the caller can associate it immediately.
func (r *Repository) CreatePrecondition(profileID, projectKey, summary, ptype, description string) (string, error) {
	if strings.TrimSpace(summary) == "" {
		return "", fmt.Errorf("a summary is required for the new precondition")
	}
	if projectKey == "" {
		projectKey = "PRECOND"
	}
	if ptype == "" {
		ptype = "Manual"
	}

	tx, err := r.db.Begin()
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	tempKey, err := nextTempPreconditionKey(tx, profileID)
	if err != nil {
		return "", err
	}

	if _, err := tx.Exec(
		`INSERT INTO precondition (profile_id, jira_key, summary, type, description, condition)
		 VALUES (?, ?, ?, ?, ?, '')`,
		profileID, tempKey, summary, ptype, description,
	); err != nil {
		return "", fmt.Errorf("insert precondition: %w", err)
	}

	payload, err := json.Marshal(struct {
		Summary     string `json:"summary"`
		Type        string `json:"type"`
		Description string `json:"description"`
		ProjectKey  string `json:"projectKey"`
	}{summary, ptype, description, projectKey})
	if err != nil {
		return "", fmt.Errorf("encode precondition payload: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO pending_change
		   (profile_id, entity_type, entity_key, field, before_val, after_val, base_version, created_at)
		 VALUES (?, ?, ?, 'precondition', '', ?, '', ?)`,
		profileID, entityPreconditionAdd, tempKey, string(payload),
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return "", fmt.Errorf("insert pending precondition: %w", err)
	}
	if err := writeAudit(
		tx, profileID, entityPreconditionAdd, tempKey,
		"create-precondition-local", "precondition", "", summary, "",
	); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit create-precondition: %w", err)
	}
	return tempKey, nil
}

// nextTempPreconditionKey returns a precondition key of the form
// "new-precond-N" not already used in this profile.
func nextTempPreconditionKey(tx *sql.Tx, profileID string) (string, error) {
	for n := 1; ; n++ {
		key := fmt.Sprintf("new-precond-%d", n)
		var one int
		err := tx.QueryRow(
			`SELECT 1 FROM precondition WHERE profile_id = ? AND jira_key = ?`,
			profileID, key,
		).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return key, nil
		}
		if err != nil {
			return "", fmt.Errorf("probe temp precondition key: %w", err)
		}
	}
}

// RenamePrecondition rewrites a Precondition's key across the cache and any
// pending associations, used by the commit path to swap a "new-precond-N"
// placeholder for the real key Jira assigned. A no-op when newKey is empty or
// unchanged.
func (r *Repository) RenamePrecondition(profileID, oldKey, newKey string) error {
	if newKey == "" || newKey == oldKey {
		return nil
	}
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(
		`UPDATE precondition SET jira_key = ? WHERE profile_id = ? AND jira_key = ?`,
		newKey, profileID, oldKey,
	); err != nil {
		return fmt.Errorf("rename precondition: %w", err)
	}
	if _, err := tx.Exec(
		`UPDATE test_precondition SET precondition_key = ?
		 WHERE profile_id = ? AND precondition_key = ?`,
		newKey, profileID, oldKey,
	); err != nil {
		return fmt.Errorf("rename precondition links: %w", err)
	}
	if err := rewritePreconditionSets(tx, profileID, oldKey, newKey); err != nil {
		return err
	}
	return tx.Commit()
}

// rewritePreconditionSets substitutes oldKey for newKey (newKey == "" removes
// it) inside every precondition_set pending row's before / after key-lists. A
// row whose after-set ends up equal to its before-set is dropped (no-op).
func rewritePreconditionSets(tx *sql.Tx, profileID, oldKey, newKey string) error {
	rows, err := tx.Query(
		`SELECT id, before_val, after_val FROM pending_change
		 WHERE profile_id = ? AND entity_type = ?`,
		profileID, entityPreconditionSet)
	if err != nil {
		return fmt.Errorf("read precondition sets: %w", err)
	}
	type row struct {
		id            int64
		before, after string
	}
	var rs []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.id, &x.before, &x.after); err != nil {
			rows.Close()
			return err
		}
		rs = append(rs, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, x := range rs {
		before := substituteKey(decodeKeys(x.before), oldKey, newKey)
		after := substituteKey(decodeKeys(x.after), oldKey, newKey)
		if equalOrder(before, after) {
			if _, err := tx.Exec(
				`DELETE FROM pending_change WHERE profile_id = ? AND id = ?`,
				profileID, x.id,
			); err != nil {
				return fmt.Errorf("drop precondition set: %w", err)
			}
			continue
		}
		bj, _ := json.Marshal(before)
		aj, _ := json.Marshal(after)
		if _, err := tx.Exec(
			`UPDATE pending_change SET before_val = ?, after_val = ?
			 WHERE profile_id = ? AND id = ?`,
			string(bj), string(aj), profileID, x.id,
		); err != nil {
			return fmt.Errorf("update precondition set: %w", err)
		}
	}
	return nil
}

// decodeKeys parses a JSON string array, returning nil on malformed input.
func decodeKeys(s string) []string {
	var out []string
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

// substituteKey replaces oldKey with newKey in a key set (newKey == "" drops
// it), returning a deduped sorted set.
func substituteKey(keys []string, oldKey, newKey string) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if k == oldKey {
			if newKey != "" {
				out = append(out, newKey)
			}
			continue
		}
		out = append(out, k)
	}
	return uniqueSorted(out)
}

// UpsertContainers inserts or updates a batch of Test Sets / Plans /
// Executions (FR-1.3).
func (r *Repository) UpsertContainers(profileID string, containers []Container) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// environments is preserved when a pending container_env edit exists, so a
	// sync can't clobber an uncommitted local edit (mirrors the per-field guard
	// in UpsertTests).
	// fix_versions is a plain synced field (read-only, never locally edited), so
	// it is overwritten unconditionally on conflict (unlike environments, which
	// is preserved when a pending container_env edit exists).
	stmt, err := tx.Prepare(
		`INSERT INTO test_container (profile_id, jira_key, kind, summary, status, parent_key, issue_type, parent_summary, environments, fix_versions, created, updated, resolved, description)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(profile_id, jira_key) DO UPDATE SET
		   kind           = excluded.kind,
		   summary        = excluded.summary,
		   status         = excluded.status,
		   parent_key     = excluded.parent_key,
		   issue_type     = excluded.issue_type,
		   parent_summary = excluded.parent_summary,
		   fix_versions   = excluded.fix_versions,
		   created        = excluded.created,
		   updated        = excluded.updated,
		   resolved       = excluded.resolved,
		   description    = excluded.description,
		   environments   = CASE WHEN EXISTS (
		       SELECT 1 FROM pending_change
		       WHERE pending_change.profile_id  = excluded.profile_id
		         AND pending_change.entity_type = 'container_env'
		         AND pending_change.entity_key  = excluded.jira_key
		     ) THEN test_container.environments ELSE excluded.environments END`)
	if err != nil {
		return fmt.Errorf("prepare upsert container: %w", err)
	}
	defer stmt.Close()

	for _, c := range containers {
		if _, err := stmt.Exec(profileID, c.Key, c.Kind, c.Summary, c.Status, c.ParentKey, c.IssueType, c.ParentSummary, encodeEnvironments(c.Environments), encodeFixVersions(c.FixVersions), c.Created, c.Updated, c.Resolved, c.Description); err != nil {
			return fmt.Errorf("upsert container %s: %w", c.Key, err)
		}
	}
	return tx.Commit()
}

// ReplaceAllContainerLinks wipes a profile's Test-to-Container memberships and
// rewrites them from the provided list, so memberships removed in Jira
// actually disappear on resync (mirrors ReplaceAllTestPreconditions).
func (r *Repository) ReplaceAllContainerLinks(profileID string, links []ContainerLink) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(
		`DELETE FROM test_container_test WHERE profile_id = ?`, profileID,
	); err != nil {
		return fmt.Errorf("clear container links: %w", err)
	}

	if len(links) == 0 {
		return tx.Commit()
	}

	stmt, err := tx.Prepare(
		`INSERT INTO test_container_test (profile_id, container_key, test_key, run_status)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(profile_id, container_key, test_key) DO UPDATE SET
		   run_status = excluded.run_status`)
	if err != nil {
		return fmt.Errorf("prepare insert container link: %w", err)
	}
	defer stmt.Close()

	for _, l := range links {
		if _, err := stmt.Exec(profileID, l.ContainerKey, l.TestKey, l.RunStatus); err != nil {
			return fmt.Errorf("link %s -> %s: %w", l.ContainerKey, l.TestKey, err)
		}
	}
	return tx.Commit()
}

// UpsertContainerLinks inserts Test-to-Container memberships WITHOUT wiping
// existing ones. This is the additive counterpart to ReplaceAllContainerLinks
// and mirrors the UpsertBugLinks pattern: it is used by the cross-project
// execution discovery pass to merge newly found executions alongside the
// project links that ReplaceAllContainerLinks already wrote.
//
// INSERT OR IGNORE de-dupes by the primary key (profile_id, container_key,
// test_key), so calling with duplicate rows is safe and idempotent.
func (r *Repository) UpsertContainerLinks(profileID string, links []ContainerLink) error {
	if len(links) == 0 {
		return nil
	}
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.Prepare(
		`INSERT OR IGNORE INTO test_container_test (profile_id, container_key, test_key, run_status)
		 VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare upsert container link: %w", err)
	}
	defer stmt.Close()
	for _, l := range links {
		if _, err := stmt.Exec(profileID, l.ContainerKey, l.TestKey, l.RunStatus); err != nil {
			return fmt.Errorf("upsert container link %s->%s: %w", l.ContainerKey, l.TestKey, err)
		}
	}
	return tx.Commit()
}

// ExternalTest is the cached basics of a member Test that lives in a different
// Jira project than the profile's, so it is never returned by the project-scoped
// bulk test pull and has no test_case row. The container board reads these so
// such members still render with a summary / status instead of bare keys.
type ExternalTest struct {
	Key        string `json:"key"`
	Summary    string `json:"summary"`
	Status     string `json:"status"`
	ProjectKey string `json:"projectKey"`
}

// ContainerMemberKeysMissingTests returns the distinct container-member Test
// keys that have no matching row in test_case — i.e. members living in another
// project (the bulk pull only fetches the profile's project). The sync caches
// the basics of these via ReplaceExternalTests so the board can show them.
func (r *Repository) ContainerMemberKeysMissingTests(profileID string) ([]string, error) {
	rows, err := r.db.Query(
		`SELECT DISTINCT l.test_key
		 FROM test_container_test l
		 LEFT JOIN test_case t
		   ON t.profile_id = l.profile_id AND t.jira_key = l.test_key
		 WHERE l.profile_id = ? AND t.jira_key IS NULL
		 ORDER BY l.test_key`,
		profileID)
	if err != nil {
		return nil, fmt.Errorf("find missing member tests: %w", err)
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

// ReplaceExternalTests wipes a profile's external_test cache and rewrites it
// from the provided list, so external members removed upstream disappear on
// resync (mirrors ReplaceAllContainerLinks). An empty list clears the cache.
func (r *Repository) ReplaceExternalTests(profileID string, tests []ExternalTest) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(
		`DELETE FROM external_test WHERE profile_id = ?`, profileID,
	); err != nil {
		return fmt.Errorf("clear external tests: %w", err)
	}

	if len(tests) == 0 {
		return tx.Commit()
	}

	stmt, err := tx.Prepare(
		`INSERT INTO external_test (profile_id, jira_key, summary, status, project_key)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(profile_id, jira_key) DO UPDATE SET
		   summary     = excluded.summary,
		   status      = excluded.status,
		   project_key = excluded.project_key`)
	if err != nil {
		return fmt.Errorf("prepare insert external test: %w", err)
	}
	defer stmt.Close()

	for _, et := range tests {
		if _, err := stmt.Exec(profileID, et.Key, et.Summary, et.Status, et.ProjectKey); err != nil {
			return fmt.Errorf("cache external test %s: %w", et.Key, err)
		}
	}
	return tx.Commit()
}

// ListContainersForTest returns the Test Sets, Plans and Executions a Test
// belongs to, ordered by kind then key, with the run status for execution
// memberships.
func (r *Repository) ListContainersForTest(profileID, testKey string) ([]ContainerMembership, error) {
	rows, err := r.db.Query(
		`SELECT c.jira_key, c.kind, c.summary, c.status, l.run_status
		 FROM test_container_test l
		 JOIN test_container c
		   ON c.profile_id = l.profile_id AND c.jira_key = l.container_key
		 WHERE l.profile_id = ? AND l.test_key = ?
		 ORDER BY c.kind, c.jira_key`,
		profileID, testKey)
	if err != nil {
		return nil, fmt.Errorf("list containers for test: %w", err)
	}
	defer rows.Close()

	out := []ContainerMembership{}
	for rows.Next() {
		var m ContainerMembership
		if err := rows.Scan(&m.Key, &m.Kind, &m.Summary, &m.Status, &m.RunStatus); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetTestPlanBoard builds the read-only board for a Test Plan (FR-13.7): each
// member Test with a consolidated execution status derived from the Test
// Executions it belongs to. The consolidation is worst-wins so failures
// surface; a Test in no execution reads as not run. Computed entirely from the
// local store.
func (r *Repository) GetContainerBoard(profileID, containerKey string) (TestPlanBoard, error) {
	board := TestPlanBoard{Key: containerKey, Rows: []TestPlanBoardRow{}, RunCounts: []Bucket{}}

	var kind string
	var description string
	err := r.db.QueryRow(
		`SELECT kind, summary, description FROM test_container WHERE profile_id = ? AND jira_key = ?`,
		profileID, containerKey,
	).Scan(&kind, &board.Summary, &description)
	if errors.Is(err, sql.ErrNoRows) {
		return board, fmt.Errorf("container %s not found", containerKey)
	}
	if err != nil {
		return board, fmt.Errorf("read container: %w", err)
	}
	board.Description = description

	// Member Tests, plus this container's direct run status (meaningful for a
	// Test Execution). LEFT JOIN both the local test_case cache and the
	// external_test cache (cross-project members live only in the latter) and
	// COALESCE so a member with neither still shows by key. is_external flags the
	// members that have no local test_case row.
	memberRows, err := r.db.Query(
		`SELECT l.test_key,
		        COALESCE(t.summary, x.summary, '') AS summary,
		        COALESCE(t.status,  x.status,  '') AS status,
		        (t.jira_key IS NULL)               AS is_external,
		        l.run_status
		 FROM test_container_test l
		 LEFT JOIN test_case     t ON t.profile_id = l.profile_id AND t.jira_key = l.test_key
		 LEFT JOIN external_test x ON x.profile_id = l.profile_id AND x.jira_key = l.test_key
		 WHERE l.profile_id = ? AND l.container_key = ?
		 ORDER BY l.test_key`,
		profileID, containerKey)
	if err != nil {
		return board, fmt.Errorf("read container members: %w", err)
	}
	defer memberRows.Close()

	type member struct {
		summary, status, directRun string
		isExternal                 bool
	}
	members := map[string]member{}
	memberOrder := []string{}
	for memberRows.Next() {
		var key, summary, status, directRun string
		var isExternal bool
		if err := memberRows.Scan(&key, &summary, &status, &isExternal, &directRun); err != nil {
			return board, err
		}
		members[key] = member{summary: summary, status: status, directRun: directRun, isExternal: isExternal}
		memberOrder = append(memberOrder, key)
	}
	if err := memberRows.Err(); err != nil {
		return board, err
	}
	if len(memberOrder) == 0 {
		return board, nil
	}

	// For a Test Execution, the run status is the Test's result in this
	// execution. For a Test Set / Plan, consolidate each member's run status
	// across all executions (worst-wins).
	runsByTest := map[string][]string{}
	if kind != "testexec" {
		execRows, err := r.db.Query(
			`SELECT l.test_key, l.run_status
			 FROM test_container_test l
			 JOIN test_container c
			   ON c.profile_id = l.profile_id AND c.jira_key = l.container_key
			 WHERE l.profile_id = ? AND c.kind = ?`,
			profileID, "testexec")
		if err != nil {
			return board, fmt.Errorf("read execution runs: %w", err)
		}
		defer execRows.Close()
		for execRows.Next() {
			var testKey, runStatus string
			if err := execRows.Scan(&testKey, &runStatus); err != nil {
				return board, err
			}
			if _, isMember := members[testKey]; isMember {
				runsByTest[testKey] = append(runsByTest[testKey], runStatus)
			}
		}
		if err := execRows.Err(); err != nil {
			return board, err
		}
	}

	runCounts := map[string]int{}
	for _, key := range memberOrder {
		m := members[key]
		runStatus := m.directRun
		if kind != "testexec" {
			runStatus = consolidateRunStatus(runsByTest[key])
		}
		board.Rows = append(board.Rows, TestPlanBoardRow{
			TestKey:    key,
			Summary:    m.summary,
			Status:     m.status,
			RunStatus:  runStatus,
			IsExternal: m.isExternal,
		})
		runCounts[blankAs(runStatus, "(not run)")]++
	}
	board.RunCounts = topBuckets(runCounts, 0)
	return board, nil
}

// runStatusPriority ranks Test Run results so a Test appearing in several
// executions consolidates to its worst (most attention-worthy) outcome.
var runStatusPriority = map[string]int{
	"FAIL":      6,
	"BLOCKED":   5,
	"ABORTED":   4,
	"EXECUTING": 3,
	"TODO":      2,
	"PASS":      1,
}

// consolidateRunStatus reduces a Test's run statuses across executions to a
// single worst-wins result, or "" when the Test is in no execution.
func consolidateRunStatus(statuses []string) string {
	best := ""
	bestRank := 0
	for _, s := range statuses {
		rank := runStatusPriority[strings.ToUpper(s)]
		if rank == 0 {
			rank = 1 // unknown statuses rank lowest
		}
		if rank > bestRank {
			bestRank = rank
			best = s
		}
	}
	return best
}

// SeedResult reports how much sample container data SeedSampleContainers
// generated.
type SeedResult struct {
	Sets       int `json:"sets"`
	Plans      int `json:"plans"`
	Executions int `json:"executions"`
	Linked     int `json:"linked"`
}

// seedRunStatuses is the weighted Test Run vocabulary used when seeding sample
// Test Executions — mostly passing, some failing / not-yet-run.
var seedRunStatuses = []string{
	"PASS", "PASS", "PASS", "FAIL", "TODO", "TODO", "EXECUTING", "ABORTED",
}

// seedContainerStatuses cycles issue statuses for the generated containers.
var seedContainerStatuses = []string{"Open", "In Progress", "Done"}

// SeedSampleContainers populates the local store with sample Test Sets, Test
// Plans and Test Executions (with run statuses) linked to the profile's
// already-synced Tests. It exists so the board / grouping / coverage features
// can be exercised before the real Xray container endpoints are wired — the
// real-Jira sync is a no-op today, so seeded data survives a sync.
//
// Re-running is idempotent: the same containers and memberships are upserted,
// refreshing run statuses. Up to seedTestCap Tests are linked to keep the
// board readable.
func (r *Repository) SeedSampleContainers(profileID, projectKey string) (SeedResult, error) {
	var result SeedResult
	if projectKey == "" {
		projectKey = "SAMPLE"
	}

	const seedTestCap = 400
	rows, err := r.db.Query(
		`SELECT jira_key FROM test_case WHERE profile_id = ?
		 ORDER BY jira_key LIMIT ?`, profileID, seedTestCap)
	if err != nil {
		return result, fmt.Errorf("read tests to seed: %w", err)
	}
	testKeys := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return result, err
		}
		testKeys = append(testKeys, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return result, err
	}
	if len(testKeys) == 0 {
		return result, fmt.Errorf("no tests cached for this profile — run a sync first")
	}

	const nSets, nPlans, nExecs = 5, 3, 4

	tx, err := r.db.Begin()
	if err != nil {
		return result, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// parent_key / issue_type omitted on purpose: seeded containers are always
	// standalone, so a re-seed must not clobber a synced sub-task's parent link.
	containerStmt, err := tx.Prepare(
		`INSERT INTO test_container (profile_id, jira_key, kind, summary, status)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(profile_id, jira_key) DO UPDATE SET
		   kind = excluded.kind, summary = excluded.summary, status = excluded.status`)
	if err != nil {
		return result, fmt.Errorf("prepare container: %w", err)
	}
	defer containerStmt.Close()

	makeContainers := func(prefix, kind, label string, count int) ([]string, error) {
		keys := make([]string, count)
		for i := 0; i < count; i++ {
			key := fmt.Sprintf("%s-%s-%d", projectKey, prefix, i+1)
			keys[i] = key
			if _, err := containerStmt.Exec(
				profileID, key, kind,
				fmt.Sprintf("Sample %s %d", label, i+1),
				seedContainerStatuses[i%len(seedContainerStatuses)],
			); err != nil {
				return nil, fmt.Errorf("seed container %s: %w", key, err)
			}
		}
		return keys, nil
	}

	setKeys, err := makeContainers("SET", "testset", "test set", nSets)
	if err != nil {
		return result, err
	}
	planKeys, err := makeContainers("PLAN", "testplan", "test plan", nPlans)
	if err != nil {
		return result, err
	}
	execKeys, err := makeContainers("EXEC", "testexec", "execution", nExecs)
	if err != nil {
		return result, err
	}

	linkStmt, err := tx.Prepare(
		`INSERT INTO test_container_test (profile_id, container_key, test_key, run_status)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(profile_id, container_key, test_key) DO UPDATE SET
		   run_status = excluded.run_status`)
	if err != nil {
		return result, fmt.Errorf("prepare link: %w", err)
	}
	defer linkStmt.Close()

	for i, testKey := range testKeys {
		if _, err := linkStmt.Exec(profileID, setKeys[i%nSets], testKey, ""); err != nil {
			return result, fmt.Errorf("seed set link: %w", err)
		}
		if _, err := linkStmt.Exec(profileID, planKeys[i%nPlans], testKey, ""); err != nil {
			return result, fmt.Errorf("seed plan link: %w", err)
		}
		if _, err := linkStmt.Exec(
			profileID, execKeys[i%nExecs], testKey,
			seedRunStatuses[i%len(seedRunStatuses)],
		); err != nil {
			return result, fmt.Errorf("seed exec link: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit seed: %w", err)
	}
	return SeedResult{Sets: nSets, Plans: nPlans, Executions: nExecs, Linked: len(testKeys)}, nil
}

// ListContainers returns the cached Containers of a given kind for a profile,
// ordered by key — used by the bulk-allocation picker (FR-3.4–3.6).
func (r *Repository) ListContainers(profileID, kind string) ([]Container, error) {
	return r.ListContainersQuery(profileID, ContainerQuery{Kind: kind})
}

// ListContainersQuery lists the containers of one kind, optionally filtered by a
// Test Environment (membership test over the JSON environments array). The
// filter matches the JSON-quoted token so "Prod" does not collide with
// "Production" (see environmentFilterPattern).
func (r *Repository) ListContainersQuery(profileID string, q ContainerQuery) ([]Container, error) {
	sqlStr := `SELECT jira_key, kind, summary, status, parent_key, issue_type, parent_summary, environments, fix_versions
		 FROM test_container WHERE profile_id = ? AND kind = ?`
	args := []any{profileID, q.Kind}
	if q.Environment != "" {
		sqlStr += ` AND environments LIKE ?`
		args = append(args, environmentFilterPattern(q.Environment))
	}
	sqlStr += ` ORDER BY jira_key`

	rows, err := r.db.Query(sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	defer rows.Close()

	out := []Container{}
	for rows.Next() {
		var c Container
		var environments, fixVersions string
		if err := rows.Scan(&c.Key, &c.Kind, &c.Summary, &c.Status, &c.ParentKey, &c.IssueType, &c.ParentSummary, &environments, &fixVersions); err != nil {
			return nil, err
		}
		c.Environments = decodeEnvironments(environments)
		c.FixVersions = decodeFixVersions(fixVersions)
		out = append(out, c)
	}
	return out, rows.Err()
}

// AllocateResult reports a bulk allocation's outcome: which Tests were newly
// added to the Container and which were already members (FR-3.4–3.6).
type AllocateResult struct {
	Added          []string `json:"added"`
	AlreadyMembers []string `json:"alreadyMembers"`
}

// AllocateTests adds Tests to an existing Container locally and queues the
// membership for commit (FR-3.4–3.6). Tests already in the Container are
// reported as such. The pending add / remove sets are kept disjoint: adding a
// Test that's pending removal just cancels the removal.
func (r *Repository) AllocateTests(profileID, containerKey string, testKeys []string) (AllocateResult, error) {
	result := AllocateResult{Added: []string{}, AlreadyMembers: []string{}}

	tx, err := r.db.Begin()
	if err != nil {
		return result, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	kind, err := containerKindTx(tx, profileID, containerKey)
	if err != nil {
		return result, err
	}

	adds, err := readMembers(tx, profileID, entityMembershipAdd, containerKey)
	if err != nil {
		return result, err
	}
	removes, err := readMembers(tx, profileID, entityMembershipRemove, containerKey)
	if err != nil {
		return result, err
	}

	for _, testKey := range testKeys {
		linked, err := isMember(tx, profileID, containerKey, testKey)
		if err != nil {
			return result, err
		}
		if linked {
			result.AlreadyMembers = append(result.AlreadyMembers, testKey)
			continue
		}
		if _, err := tx.Exec(
			`INSERT INTO test_container_test (profile_id, container_key, test_key, run_status)
			 VALUES (?, ?, ?, '')`,
			profileID, containerKey, testKey,
		); err != nil {
			return result, fmt.Errorf("insert membership: %w", err)
		}
		// Re-adding a Test that was pending removal cancels the removal;
		// otherwise queue it as an add.
		if containsStr(removes, testKey) {
			removes = withoutStr(removes, testKey)
		} else if !containsStr(adds, testKey) {
			adds = append(adds, testKey)
		}
		result.Added = append(result.Added, testKey)
	}

	if len(result.Added) == 0 {
		return result, nil
	}
	if err := writeMembers(tx, profileID, entityMembershipAdd, containerKey, kind, adds); err != nil {
		return result, err
	}
	if err := writeMembers(tx, profileID, entityMembershipRemove, containerKey, kind, removes); err != nil {
		return result, err
	}
	if err := writeAudit(
		tx, profileID, entityMembershipAdd, containerKey,
		"allocate-local", "members", "", strings.Join(result.Added, " "), "",
	); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit allocation: %w", err)
	}
	return result, nil
}

// DeallocateResult reports a bulk de-allocation's outcome: which Tests were
// removed from the Container and which weren't members to begin with.
type DeallocateResult struct {
	Removed    []string `json:"removed"`
	NotMembers []string `json:"notMembers"`
}

// DeallocateTests removes Tests from a Container locally and queues the removal
// for commit (FR-3.4–3.6). Removing a Test that was only locally added cancels
// the add instead of queuing a remote removal; the add / remove sets stay
// disjoint.
func (r *Repository) DeallocateTests(profileID, containerKey string, testKeys []string) (DeallocateResult, error) {
	result := DeallocateResult{Removed: []string{}, NotMembers: []string{}}

	tx, err := r.db.Begin()
	if err != nil {
		return result, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	kind, err := containerKindTx(tx, profileID, containerKey)
	if err != nil {
		return result, err
	}

	adds, err := readMembers(tx, profileID, entityMembershipAdd, containerKey)
	if err != nil {
		return result, err
	}
	removes, err := readMembers(tx, profileID, entityMembershipRemove, containerKey)
	if err != nil {
		return result, err
	}

	for _, testKey := range testKeys {
		linked, err := isMember(tx, profileID, containerKey, testKey)
		if err != nil {
			return result, err
		}
		if !linked {
			result.NotMembers = append(result.NotMembers, testKey)
			continue
		}
		if _, err := tx.Exec(
			`DELETE FROM test_container_test
			 WHERE profile_id = ? AND container_key = ? AND test_key = ?`,
			profileID, containerKey, testKey,
		); err != nil {
			return result, fmt.Errorf("delete membership: %w", err)
		}
		// Removing a Test that was only locally added cancels the add;
		// otherwise queue a removal of the committed membership.
		if containsStr(adds, testKey) {
			adds = withoutStr(adds, testKey)
		} else if !containsStr(removes, testKey) {
			removes = append(removes, testKey)
		}
		result.Removed = append(result.Removed, testKey)
	}

	if len(result.Removed) == 0 {
		return result, nil
	}
	if err := writeMembers(tx, profileID, entityMembershipAdd, containerKey, kind, adds); err != nil {
		return result, err
	}
	if err := writeMembers(tx, profileID, entityMembershipRemove, containerKey, kind, removes); err != nil {
		return result, err
	}
	if err := writeAudit(
		tx, profileID, entityMembershipRemove, containerKey,
		"deallocate-local", "members", strings.Join(result.Removed, " "), "", "",
	); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit deallocation: %w", err)
	}
	return result, nil
}

// containerKindTx returns a Container's kind, or an error if it doesn't exist.
func containerKindTx(tx *sql.Tx, profileID, containerKey string) (string, error) {
	var kind string
	err := tx.QueryRow(
		`SELECT kind FROM test_container WHERE profile_id = ? AND jira_key = ?`,
		profileID, containerKey,
	).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("container %s not found", containerKey)
	}
	if err != nil {
		return "", fmt.Errorf("read container: %w", err)
	}
	return kind, nil
}

// isMember reports whether a Test is currently linked to a Container.
func isMember(tx *sql.Tx, profileID, containerKey, testKey string) (bool, error) {
	var one int
	err := tx.QueryRow(
		`SELECT 1 FROM test_container_test
		 WHERE profile_id = ? AND container_key = ? AND test_key = ?`,
		profileID, containerKey, testKey,
	).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check membership: %w", err)
	}
	return true, nil
}

// membershipPayload is the JSON shape stored in a test_membership_add pending
// row's after_val — the target Container kind plus the Test keys to add.
type membershipPayload struct {
	Kind    string   `json:"kind"`
	Members []string `json:"members"`
}

// CreateContainerResult reports a create-and-allocate operation: the temporary
// Container key assigned locally (swapped for the real one on commit) and how
// many Tests were allocated to it (FR-3.4–3.6).
type CreateContainerResult struct {
	TempKey string `json:"tempKey"`
	Added   int    `json:"added"`
}

// containerPayload is the JSON stored in a test_container_add pending row's
// after_val — everything the commit needs to create the issue and populate it.
type containerPayload struct {
	Kind       string   `json:"kind"`
	Summary    string   `json:"summary"`
	ProjectKey string   `json:"projectKey"`
	Members    []string `json:"members"`
}

// containerKinds whitelists the Container kinds that can be created.
var containerKinds = map[string]struct{}{
	"testset":  {},
	"testplan": {},
	"testexec": {},
}

// CreateContainerAllocation creates a new Container locally and queues it for
// creation in Jira on commit, allocating the given Tests to it (FR-3.4–3.6).
// The Container gets a temporary key until commit POSTs the issue and learns
// the real one. Everything the commit needs travels in the pending row.
func (r *Repository) CreateContainerAllocation(profileID, projectKey, kind, summary string, testKeys []string) (CreateContainerResult, error) {
	var result CreateContainerResult
	if _, ok := containerKinds[kind]; !ok {
		return result, fmt.Errorf("unknown container kind %q", kind)
	}
	if strings.TrimSpace(summary) == "" {
		return result, fmt.Errorf("a name is required for the new container")
	}

	tx, err := r.db.Begin()
	if err != nil {
		return result, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	tempKey, err := nextTempContainerKey(tx, profileID)
	if err != nil {
		return result, err
	}

	if _, err := tx.Exec(
		`INSERT INTO test_container (profile_id, jira_key, kind, summary, status)
		 VALUES (?, ?, ?, ?, '')`,
		profileID, tempKey, kind, summary,
	); err != nil {
		return result, fmt.Errorf("insert container: %w", err)
	}

	members := make([]string, 0, len(testKeys))
	for _, testKey := range testKeys {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO test_container_test
			   (profile_id, container_key, test_key, run_status)
			 VALUES (?, ?, ?, '')`,
			profileID, tempKey, testKey,
		); err != nil {
			return result, fmt.Errorf("insert membership: %w", err)
		}
		members = append(members, testKey)
	}

	payload := containerPayload{Kind: kind, Summary: summary, ProjectKey: projectKey, Members: members}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return result, fmt.Errorf("encode container payload: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO pending_change
		   (profile_id, entity_type, entity_key, field, before_val, after_val, base_version, created_at)
		 VALUES (?, ?, ?, 'container', '', ?, '', ?)`,
		profileID, entityContainerAdd, tempKey, string(encoded),
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return result, fmt.Errorf("insert pending container: %w", err)
	}
	if err := writeAudit(
		tx, profileID, entityContainerAdd, tempKey,
		"create-container-local", "container", "", summary, "",
	); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit create-container: %w", err)
	}
	return CreateContainerResult{TempKey: tempKey, Added: len(members)}, nil
}

// nextTempContainerKey returns a container key of the form "new-container-N"
// not already used by another container in this profile.
func nextTempContainerKey(tx *sql.Tx, profileID string) (string, error) {
	for n := 1; ; n++ {
		key := fmt.Sprintf("new-container-%d", n)
		var one int
		err := tx.QueryRow(
			`SELECT 1 FROM test_container WHERE profile_id = ? AND jira_key = ?`,
			profileID, key,
		).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return key, nil
		}
		if err != nil {
			return "", fmt.Errorf("probe temp container key: %w", err)
		}
	}
}

// RenameContainer rewrites a Container's key across the cache, used by the
// commit path to swap a "new-container-N" placeholder for the real key Jira
// assigned. A no-op when newKey is empty or unchanged.
func (r *Repository) RenameContainer(profileID, oldKey, newKey string) error {
	if newKey == "" || newKey == oldKey {
		return nil
	}
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(
		`UPDATE test_container SET jira_key = ? WHERE profile_id = ? AND jira_key = ?`,
		newKey, profileID, oldKey,
	); err != nil {
		return fmt.Errorf("rename container: %w", err)
	}
	if _, err := tx.Exec(
		`UPDATE test_container_test SET container_key = ? WHERE profile_id = ? AND container_key = ?`,
		newKey, profileID, oldKey,
	); err != nil {
		return fmt.Errorf("rename container links: %w", err)
	}
	// Rewrite still-pending changes that key off the container so they commit
	// against the real key — membership add/remove, edits and deletes key by the
	// bare container key; run-status rows key by "<execKey>:<testKey>".
	if _, err := tx.Exec(
		`UPDATE pending_change SET entity_key = ?
		 WHERE profile_id = ? AND entity_key = ?
		   AND entity_type IN ('test_membership_add','test_membership_remove','container_edit','container_delete')`,
		newKey, profileID, oldKey,
	); err != nil {
		return fmt.Errorf("rewrite container pending rows: %w", err)
	}
	if _, err := tx.Exec(
		`UPDATE pending_change SET entity_key = ? || substr(entity_key, ?)
		 WHERE profile_id = ? AND entity_type = 'test_run' AND entity_key LIKE ?`,
		newKey, len(oldKey)+1, profileID, oldKey+":%",
	); err != nil {
		return fmt.Errorf("rewrite run-status pending rows: %w", err)
	}
	return tx.Commit()
}

// readMembers returns the member list of a membership add/remove pending row,
// or an empty list when the row doesn't exist.
func readMembers(tx *sql.Tx, profileID, entityType, containerKey string) ([]string, error) {
	var afterVal string
	err := tx.QueryRow(
		`SELECT after_val FROM pending_change
		 WHERE profile_id = ? AND entity_type = ? AND entity_key = ? AND field = 'members'`,
		profileID, entityType, containerKey,
	).Scan(&afterVal)
	if errors.Is(err, sql.ErrNoRows) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read pending membership: %w", err)
	}
	var p membershipPayload
	if err := json.Unmarshal([]byte(afterVal), &p); err != nil {
		return nil, fmt.Errorf("decode pending membership: %w", err)
	}
	return p.Members, nil
}

// writeMembers upserts a membership add/remove pending row to the given member
// list, deleting the row when the list is empty so a fully-cancelled delta
// leaves nothing behind.
func writeMembers(tx *sql.Tx, profileID, entityType, containerKey, kind string, members []string) error {
	if len(members) == 0 {
		if _, err := tx.Exec(
			`DELETE FROM pending_change
			 WHERE profile_id = ? AND entity_type = ? AND entity_key = ? AND field = 'members'`,
			profileID, entityType, containerKey,
		); err != nil {
			return fmt.Errorf("clear pending membership: %w", err)
		}
		return nil
	}
	encoded, err := json.Marshal(membershipPayload{Kind: kind, Members: uniqueSorted(members)})
	if err != nil {
		return fmt.Errorf("encode pending membership: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO pending_change
		   (profile_id, entity_type, entity_key, field, before_val, after_val, base_version, created_at)
		 VALUES (?, ?, ?, 'members', '', ?, '', ?)
		 ON CONFLICT(profile_id, entity_type, entity_key, field)
		   DO UPDATE SET after_val = excluded.after_val, created_at = excluded.created_at`,
		profileID, entityType, containerKey, string(encoded),
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("write pending membership: %w", err)
	}
	return nil
}

// containsStr reports whether s contains v.
func containsStr(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// withoutStr returns s with v removed.
func withoutStr(s []string, v string) []string {
	out := make([]string, 0, len(s))
	for _, x := range s {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

// buildTestFilter builds the WHERE clause + args for a Test query. Shared
// by ListTests and ListMatchingKeys so both see the same filter semantics
// — when the user clicks "select all matching", they get the exact set
// the grid is showing across all pages.
func buildTestFilter(profileID string, q Query) (string, []any) {
	where := []string{"profile_id = ?"}
	args := []any{profileID}

	if q.Search != "" {
		where = append(where, "(jira_key LIKE ? OR summary LIKE ? OR description LIKE ?)")
		like := "%" + q.Search + "%"
		args = append(args, like, like, like)
	}
	if q.Status != "" {
		where = append(where, "status = ?")
		args = append(args, q.Status)
	}
	if q.FolderID != "" {
		where = append(where, "(folder_id = ? OR folder_id LIKE ?)")
		args = append(args, q.FolderID, q.FolderID+"/%")
	}
	if q.ContainerKey != "" {
		// Restrict to Tests belonging to the given Test Set / Plan / Execution
		// (FR-11.6 grouping). The subquery is profile-scoped already via the
		// outer profile_id predicate, but we re-scope here too so a stray
		// container key from another profile can't leak rows.
		where = append(where,
			"jira_key IN (SELECT test_key FROM test_container_test "+
				"WHERE profile_id = ? AND container_key = ?)")
		args = append(args, profileID, q.ContainerKey)
	}
	if q.Component != "" {
		// Group-by component: match Tests carrying the chosen component. The
		// stored value is newline-bounded so this LIKE matches a whole name and
		// not a prefix of a longer one.
		where = append(where, "components LIKE ?")
		args = append(args, componentFilterPattern(q.Component))
	}
	if q.ExecType != "" {
		// Filter by Xray Test Type (execution type) - exact match.
		where = append(where, "exec_type = ?")
		args = append(args, q.ExecType)
	}
	if q.Review != "" {
		// Filter by review verdict. "unreviewed" means no review row with a
		// non-empty verdict; any other value matches that verdict exactly.
		if q.Review == "unreviewed" {
			where = append(where,
				"NOT EXISTS (SELECT 1 FROM test_review tr "+
					"WHERE tr.profile_id = ? AND tr.test_key = test_case.jira_key AND tr.verdict <> '')")
			args = append(args, profileID)
		} else {
			where = append(where,
				"EXISTS (SELECT 1 FROM test_review tr "+
					"WHERE tr.profile_id = ? AND tr.test_key = test_case.jira_key AND tr.verdict = ?)")
			args = append(args, profileID, q.Review)
		}
	}
	return "WHERE " + strings.Join(where, " AND "), args
}

// SetTestSteps replaces the cached Step list for one Test (FR-2.5). The
// whole list is rewritten on each call — Xray returns steps as an ordered
// array and we mirror that semantic rather than trying to diff. Pass an
// empty slice to clear the cache for a Test.
func (r *Repository) SetTestSteps(profileID, testKey string, steps []Step) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(
		`DELETE FROM test_step WHERE profile_id = ? AND test_key = ?`,
		profileID, testKey,
	); err != nil {
		return fmt.Errorf("clear steps: %w", err)
	}
	for _, s := range steps {
		if _, err := tx.Exec(
			`INSERT INTO test_step
			   (profile_id, test_key, xray_id, idx, action, data, expected, called_test_key)
			   VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			profileID, testKey, s.XrayID, s.Index, s.Action, s.Data, s.Expected, s.CalledTestKey,
		); err != nil {
			return fmt.Errorf("insert step: %w", err)
		}
	}
	return tx.Commit()
}

// ListTestSteps returns the cached Steps for a Test in index order.
// Returns an empty slice (not an error) for tests with no cached steps —
// the caller is responsible for deciding whether to fetch from Jira.
func (r *Repository) ListTestSteps(profileID, testKey string) ([]Step, error) {
	rows, err := r.db.Query(
		`SELECT xray_id, idx, action, data, expected, called_test_key
		 FROM test_step
		 WHERE profile_id = ? AND test_key = ?
		 ORDER BY idx`,
		profileID, testKey,
	)
	if err != nil {
		return nil, fmt.Errorf("list test steps: %w", err)
	}
	defer rows.Close()

	out := []Step{}
	for rows.Next() {
		var s Step
		if err := rows.Scan(&s.XrayID, &s.Index, &s.Action, &s.Data, &s.Expected, &s.CalledTestKey); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListTests returns a filtered, sorted, paginated page of Tests for a profile.
// A FolderID filter matches the folder itself plus any descendants so
// selecting a category in the tree shows everything beneath it.
func (r *Repository) ListTests(profileID string, q Query) (Page, error) {
	whereSQL, args := buildTestFilter(profileID, q)

	var total int
	if err := r.db.QueryRow(
		"SELECT COUNT(*) FROM test_case "+whereSQL, args...,
	).Scan(&total); err != nil {
		return Page{}, fmt.Errorf("count tests: %w", err)
	}

	sortCol, ok := sortColumns[q.SortBy]
	if !ok {
		sortCol = "jira_key"
	}
	dir := "ASC"
	if q.Desc {
		dir = "DESC"
	}
	limit := q.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	// Sorting by key orders on the trailing issue number numerically (QA-2 <
	// QA-10 < QA-100), not lexically; the bare key is the stable tiebreaker.
	// Other sorts keep numeric key as their secondary sort so equal values stay
	// in issue-number order (RND_P_4TFINT_05-202 / -205).
	var orderSQL string
	if sortCol == "jira_key" {
		orderSQL = fmt.Sprintf("%s %s, jira_key %s", keyNumericOrderExpr("jira_key"), dir, dir)
	} else {
		orderSQL = fmt.Sprintf("%s %s, %s ASC", sortCol, dir, keyNumericOrderExpr("jira_key"))
	}

	listSQL := fmt.Sprintf(
		`SELECT jira_key, jira_id, summary, description, status, priority, labels, components, updated_at, folder_id, exec_type, fix_versions, cucumber_scenario, cucumber_type, generic_definition
		 FROM test_case %s ORDER BY %s LIMIT ? OFFSET ?`,
		whereSQL, orderSQL)

	rows, err := r.db.Query(listSQL, append(args, limit, q.Offset)...)
	if err != nil {
		return Page{}, fmt.Errorf("list tests: %w", err)
	}
	defer rows.Close()

	page := Page{Total: total, Tests: []TestCase{}}
	for rows.Next() {
		t, err := scanTest(rows)
		if err != nil {
			return Page{}, err
		}
		page.Tests = append(page.Tests, t)
	}
	return page, rows.Err()
}

// ListMatchingKeys returns every Test key matching the query's filter,
// ignoring pagination and sort. The bulk toolbar uses this to honor
// "select all 4,812 matching" without forcing the user to paginate
// through 96 pages (FR-3.1).
//
// Order is unspecified — the frontend treats the result as an unordered
// set when building the selection.
func (r *Repository) ListMatchingKeys(profileID string, q Query) ([]string, error) {
	whereSQL, args := buildTestFilter(profileID, q)
	rows, err := r.db.Query(
		"SELECT jira_key FROM test_case "+whereSQL, args...,
	)
	if err != nil {
		return nil, fmt.Errorf("list matching keys: %w", err)
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// AllTestKeys returns every cached Test key for a profile.
func (r *Repository) AllTestKeys(profileID string) ([]string, error) {
	rows, err := r.db.Query(`SELECT jira_key FROM test_case WHERE profile_id = ?`, profileID)
	if err != nil {
		return nil, fmt.Errorf("list test keys: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// AllContainerKeys returns every cached container key for a profile.
// Used by the cross-project execution discovery pass to identify already-known
// executions so newly discovered ones can be deduped.
func (r *Repository) AllContainerKeys(profileID string) (map[string]bool, error) {
	rows, err := r.db.Query(`SELECT jira_key FROM test_container WHERE profile_id = ?`, profileID)
	if err != nil {
		return nil, fmt.Errorf("list container keys: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out[k] = true
	}
	return out, rows.Err()
}

// ListComponents returns the distinct Jira components across a profile's Tests
// with a count each, sorted by name — the master list the group-by-component
// sidebar draws from. Computed by scanning the components column (one cheap
// pass, same approach as the stats rollup).
func (r *Repository) ListComponents(profileID string) ([]Bucket, error) {
	rows, err := r.db.Query(
		`SELECT components FROM test_case WHERE profile_id = ? AND components <> ''`,
		profileID)
	if err != nil {
		return nil, fmt.Errorf("list components: %w", err)
	}
	defer rows.Close()

	counts := map[string]int{}
	for rows.Next() {
		var stored string
		if err := rows.Scan(&stored); err != nil {
			return nil, err
		}
		for _, name := range decodeComponents(stored) {
			counts[name]++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]Bucket, 0, len(counts))
	for name, n := range counts {
		out = append(out, Bucket{Label: name, Count: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out, nil
}

// ListTestStatuses returns the distinct statuses actually present on a profile's
// synced Tests, sorted — the local fallback/supplement for the status filter so
// the dropdown is never empty even if the workflow fetch fails.
func (r *Repository) ListTestStatuses(profileID string) ([]string, error) {
	rows, err := r.db.Query(
		`SELECT DISTINCT status FROM test_case
		 WHERE profile_id = ? AND status <> '' ORDER BY status`,
		profileID)
	if err != nil {
		return nil, fmt.Errorf("list test statuses: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetTest returns one Test by its Jira key, or ErrNotFound.
func (r *Repository) GetTest(profileID, key string) (TestCase, error) {
	row := r.db.QueryRow(
		`SELECT jira_key, jira_id, summary, description, status, priority, labels, components, updated_at, folder_id, exec_type, fix_versions, cucumber_scenario, cucumber_type, generic_definition
		 FROM test_case WHERE profile_id = ? AND jira_key = ?`, profileID, key)
	t, err := scanTest(row)
	if errors.Is(err, sql.ErrNoRows) {
		return TestCase{}, ErrNotFound
	}
	return t, err
}

// SetSyncState records that a profile finished syncing now. The test count
// is derived from the current row count so the state stays accurate after
// both full and incremental syncs.
func (r *Repository) SetSyncState(profileID string) error {
	var count int
	if err := r.db.QueryRow(
		`SELECT COUNT(*) FROM test_case WHERE profile_id = ?`, profileID,
	).Scan(&count); err != nil {
		return fmt.Errorf("count tests for sync state: %w", err)
	}
	_, err := r.db.Exec(
		`INSERT INTO sync_state (profile_id, last_synced_at, test_count) VALUES (?, ?, ?)
		 ON CONFLICT(profile_id) DO UPDATE SET
		   last_synced_at = excluded.last_synced_at,
		   test_count     = excluded.test_count`,
		profileID, time.Now().UTC().Format(time.RFC3339), count)
	if err != nil {
		return fmt.Errorf("set sync state: %w", err)
	}
	return nil
}

// GetSyncState returns the last sync outcome for a profile. A profile that
// has never synced yields a zero-valued state (no error).
func (r *Repository) GetSyncState(profileID string) (SyncState, error) {
	var (
		s    SyncState
		last sql.NullString
	)
	err := r.db.QueryRow(
		`SELECT profile_id, last_synced_at, test_count FROM sync_state WHERE profile_id = ?`,
		profileID,
	).Scan(&s.ProfileID, &last, &s.TestCount)
	if errors.Is(err, sql.ErrNoRows) {
		return SyncState{ProfileID: profileID}, nil
	}
	if err != nil {
		return SyncState{}, fmt.Errorf("get sync state: %w", err)
	}
	s.LastSyncedAt = last.String
	return s, nil
}

// statsFilter builds the WHERE clause + args that scope the dashboard rollup to
// an optional folder / component / status subset. Empty values impose no
// constraint, so the zero-filter case reproduces the original full-profile
// behaviour. The folder and component matches mirror ListTests/buildTestFilter
// exactly: a folder matches itself plus any descendant ("/Auth" also covers
// "/Auth/Login"), and a component matches a whole name within the newline-
// bounded encoded string (never a prefix of a longer name). Status is exact.
func statsFilter(profileID, folder, component, status string) (string, []any) {
	where := []string{"profile_id = ?"}
	args := []any{profileID}
	if folder != "" {
		where = append(where, "(folder_id = ? OR folder_id LIKE ?)")
		args = append(args, folder, folder+"/%")
	}
	if component != "" {
		where = append(where, "components LIKE ?")
		args = append(args, componentFilterPattern(component))
	}
	if status != "" {
		where = append(where, "status = ?")
		args = append(args, status)
	}
	return "WHERE " + strings.Join(where, " AND "), args
}

// GetStatistics computes the dashboard rollup for a profile (FR-9) in a single
// table scan plus one count query — fast enough to recompute on demand even at
// 50k Tests. Status / priority distributions are returned in full; labels and
// folders are capped to the top buckets; the trend is the most recent months
// keyed by the Test's last-updated month.
//
// The optional folder / component / status arguments narrow every aggregate to
// the matching subset of Tests (empty = no constraint). The container, execution
// and requirement panels are scoped to memberships whose Test is in the subset,
// so the whole dashboard recomputes for the filtered view.
func (r *Repository) GetStatistics(profileID, folder, component, status string) (Statistics, error) {
	stats := Statistics{
		ByStatus:      []Bucket{},
		ByPriority:    []Bucket{},
		ByLabel:       []Bucket{},
		ByFolder:      []Bucket{},
		ByComponent:   []Bucket{},
		UpdatedTrend:  []Bucket{},
		ByRunStatus:   []Bucket{},
		ByCoverage:    []Bucket{},
		ByRequirement: []Bucket{},
	}

	whereSQL, args := statsFilter(profileID, folder, component, status)
	filtered := folder != "" || component != "" || status != ""

	rows, err := r.db.Query(
		`SELECT status, priority, labels, folder_id, components, updated_at
		 FROM test_case `+whereSQL, args...)
	if err != nil {
		return stats, fmt.Errorf("read tests for stats: %w", err)
	}
	defer rows.Close()

	statusCounts := map[string]int{}
	priorityCounts := map[string]int{}
	labelCounts := map[string]int{}
	folderCounts := map[string]int{}
	componentCounts := map[string]int{}
	trendCounts := map[string]int{}

	for rows.Next() {
		var status, priority, labels, folderID, components, updated string
		if err := rows.Scan(&status, &priority, &labels, &folderID, &components, &updated); err != nil {
			return stats, err
		}
		stats.Total++
		statusCounts[blankAs(status, "(none)")]++
		priorityCounts[blankAs(priority, "(none)")]++
		for _, l := range strings.Fields(labels) {
			labelCounts[l]++
		}
		folderCounts[topFolder(folderID)]++
		for _, c := range decodeComponents(components) {
			componentCounts[c]++
		}
		if m := monthOf(updated); m != "" {
			trendCounts[m]++
		}
	}
	if err := rows.Err(); err != nil {
		return stats, err
	}

	stats.ByStatus = topBuckets(statusCounts, 0)
	stats.ByPriority = topBuckets(priorityCounts, 0)
	stats.ByLabel = topBuckets(labelCounts, 12)
	stats.ByFolder = topBuckets(folderCounts, 12)
	stats.ByComponent = topBuckets(componentCounts, 12)
	stats.UpdatedTrend = recentMonths(trendCounts, 12)

	// scope is an extra predicate (with its own args) that restricts a query's
	// test_key / entity_key column to the filtered subset of Tests. When no
	// filter is active it is empty, so the queries below stay identical to the
	// original full-profile behaviour.
	scope, scopeArgs := "", []any(nil)
	if filtered {
		scope = "test_case " + whereSQL
		scopeArgs = args
	}

	pendSQL := `SELECT COUNT(*) FROM pending_change WHERE profile_id = ?`
	pendArgs := []any{profileID}
	if filtered {
		pendSQL += ` AND entity_type = 'test_case' AND entity_key IN (SELECT jira_key FROM ` + scope + `)`
		pendArgs = append(pendArgs, scopeArgs...)
	}
	if err := r.db.QueryRow(pendSQL, pendArgs...).Scan(&stats.PendingChanges); err != nil {
		return stats, fmt.Errorf("count pending for stats: %w", err)
	}

	if err := r.addExecutionCoverage(profileID, &stats, scope, scopeArgs); err != nil {
		return stats, err
	}
	if err := r.addContainerStats(profileID, &stats, scope, scopeArgs); err != nil {
		return stats, err
	}
	if err := r.addRequirementCoverage(profileID, &stats, scope, scopeArgs); err != nil {
		return stats, err
	}
	return stats, nil
}

// addRequirementCoverage tallies requirements by their derived coverage status
// for the dashboard panel, in the same canonical order as the coverage view
// (worst-first). Buckets with no requirements are omitted.
func (r *Repository) addRequirementCoverage(profileID string, stats *Statistics, scope string, scopeArgs []any) error {
	reqs, err := r.ListRequirementsWithCoverage(profileID)
	if err != nil {
		return err
	}

	// When a dashboard filter is active, keep only requirements that are covered
	// by at least one Test in the subset, so the requirement-coverage panel
	// tracks the filtered view too. A requirement whose covering Tests all fall
	// outside the subset drops out.
	var inSubset map[string]bool
	if scope != "" {
		inSubset, err = r.subsetKeyToReqSet(profileID, scope, scopeArgs)
		if err != nil {
			return err
		}
	}

	counts := map[string]int{}
	reqCounts := map[string]int{}
	for _, req := range reqs {
		if inSubset != nil && !inSubset[req.Key] {
			continue
		}
		counts[req.Coverage]++
		// ByRequirement: only include requirements that cover at least one Test
		// (zero-count entries are excluded so the chart stays meaningful).
		if req.TestCount > 0 {
			label := req.Key
			if req.Summary != "" {
				full := req.Key + ": " + req.Summary
				if len(full) > 50 {
					full = full[:47] + "..."
				}
				label = full
			}
			reqCounts[label] = req.TestCount
		}
	}
	order := []string{CoverageFailed, CoverageNotRun, CoveragePassed, CoverageUncovered}
	for _, c := range order {
		if counts[c] > 0 {
			stats.ByCoverage = append(stats.ByCoverage, Bucket{Label: c, Count: counts[c]})
		}
	}
	stats.ByRequirement = topBuckets(reqCounts, 12)
	return nil
}

// subsetKeyToReqSet returns the set of requirement keys covered by at least one
// Test in the filtered subset (scope is "test_case <whereSQL>"), so the
// requirement-coverage panel can be narrowed to the active dashboard filter.
func (r *Repository) subsetKeyToReqSet(profileID, scope string, scopeArgs []any) (map[string]bool, error) {
	args := append([]any{profileID}, scopeArgs...)
	rows, err := r.db.Query(
		`SELECT DISTINCT l.requirement_key
		 FROM test_requirement l
		 WHERE l.profile_id = ?
		   AND l.test_key IN (SELECT jira_key FROM `+scope+`)`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("scope requirement coverage: %w", err)
	}
	defer rows.Close()
	set := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		set[k] = true
	}
	return set, rows.Err()
}

// addContainerStats fills the Test Set / Plan / Execution counts and the
// set / plan coverage — how many distinct Tests belong to at least one set or
// plan (FR-9.4).
func (r *Repository) addContainerStats(profileID string, stats *Statistics, scope string, scopeArgs []any) error {
	// Unfiltered: count every container of each kind. Filtered: count only the
	// containers that hold at least one Test in the subset, so the tiles track
	// the filtered view (a Set with no matching Test drops out).
	kindSQL := `SELECT kind, COUNT(*) FROM test_container WHERE profile_id = ? GROUP BY kind`
	kindArgs := []any{profileID}
	if scope != "" {
		kindSQL = `SELECT c.kind, COUNT(DISTINCT c.jira_key)
			 FROM test_container c
			 JOIN test_container_test l
			   ON c.profile_id = l.profile_id AND c.jira_key = l.container_key
			 WHERE c.profile_id = ?
			   AND l.test_key IN (SELECT jira_key FROM ` + scope + `)
			 GROUP BY c.kind`
		kindArgs = append(kindArgs, scopeArgs...)
	}
	kindRows, err := r.db.Query(kindSQL, kindArgs...)
	if err != nil {
		return fmt.Errorf("count containers: %w", err)
	}
	defer kindRows.Close()
	for kindRows.Next() {
		var kind string
		var n int
		if err := kindRows.Scan(&kind, &n); err != nil {
			return err
		}
		switch kind {
		case "testset":
			stats.TestSets = n
		case "testplan":
			stats.TestPlans = n
		case "testexec":
			stats.TestExecutions = n
		}
	}
	if err := kindRows.Err(); err != nil {
		return err
	}

	covSQL, covArgs := scopeClause(
		`SELECT c.kind, COUNT(DISTINCT l.test_key)
		 FROM test_container_test l
		 JOIN test_container c
		   ON c.profile_id = l.profile_id AND c.jira_key = l.container_key
		 WHERE l.profile_id = ?`,
		[]any{profileID}, scope, scopeArgs)
	covSQL += ` GROUP BY c.kind`
	covRows, err := r.db.Query(covSQL, covArgs...)
	if err != nil {
		return fmt.Errorf("count container coverage: %w", err)
	}
	defer covRows.Close()
	for covRows.Next() {
		var kind string
		var n int
		if err := covRows.Scan(&kind, &n); err != nil {
			return err
		}
		switch kind {
		case "testset":
			stats.TestsInSet = n
		case "testplan":
			stats.TestsInPlan = n
		}
	}
	return covRows.Err()
}

// scopeClause appends a "l.test_key IN (SELECT jira_key FROM <scope>)" predicate
// (with its args) when a dashboard filter is active, restricting a membership
// query to runs whose Test is in the filtered subset. With an empty scope it
// returns the query and args unchanged.
func scopeClause(sql string, args []any, scope string, scopeArgs []any) (string, []any) {
	if scope == "" {
		return sql, args
	}
	return sql + " AND l.test_key IN (SELECT jira_key FROM " + scope + ")", append(args, scopeArgs...)
}

// addExecutionCoverage rolls up Test Run statuses across all Test Execution
// memberships (FR-9.3): the run-status distribution plus the count of distinct
// Tests that appear in at least one execution. Each Test-in-execution is one
// data point, so a Test in two executions counts twice in the distribution but
// once in ExecutedTests.
func (r *Repository) addExecutionCoverage(profileID string, stats *Statistics, scope string, scopeArgs []any) error {
	sql, args := scopeClause(
		`SELECT l.run_status, l.test_key
		 FROM test_container_test l
		 JOIN test_container c
		   ON c.profile_id = l.profile_id AND c.jira_key = l.container_key
		 WHERE l.profile_id = ? AND c.kind = ?`,
		[]any{profileID, "testexec"}, scope, scopeArgs)
	rows, err := r.db.Query(sql, args...)
	if err != nil {
		return fmt.Errorf("read execution coverage: %w", err)
	}
	defer rows.Close()

	runCounts := map[string]int{}
	executed := map[string]struct{}{}
	for rows.Next() {
		var runStatus, testKey string
		if err := rows.Scan(&runStatus, &testKey); err != nil {
			return err
		}
		runCounts[blankAs(runStatus, "(none)")]++
		executed[testKey] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	stats.ByRunStatus = topBuckets(runCounts, 0)
	stats.ExecutedTests = len(executed)
	return nil
}

// blankAs returns def when s is empty, else s — so empty status / priority
// values show as a labelled bucket rather than a blank one.
func blankAs(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// topFolder reduces a folder path ("/Authentication/Login") to its top-level
// category ("Authentication") so the folder distribution stays readable.
func topFolder(id string) string {
	if id == "" {
		return "(none)"
	}
	s := strings.TrimPrefix(id, "/")
	if i := strings.Index(s, "/"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return "(none)"
	}
	return s
}

// monthOf extracts the "YYYY-MM" prefix from a Jira timestamp. Both the demo
// format ("2006-01-02T15:04:05.000-0700") and RFC 3339 start with the date, so
// a prefix slice is enough — no full parse needed.
func monthOf(updated string) string {
	if len(updated) >= 7 && updated[4] == '-' {
		return updated[:7]
	}
	return ""
}

// topBuckets turns a count map into buckets sorted by count descending (ties
// broken by label) and, when limit > 0, keeps only the top `limit`.
func topBuckets(counts map[string]int, limit int) []Bucket {
	out := make([]Bucket, 0, len(counts))
	for k, v := range counts {
		out = append(out, Bucket{Label: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Label < out[j].Label
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// recentMonths returns up to n trend buckets in chronological order, keeping
// the most recent months (YYYY-MM sorts lexicographically by time).
func recentMonths(counts map[string]int, n int) []Bucket {
	months := make([]string, 0, len(counts))
	for k := range counts {
		months = append(months, k)
	}
	sort.Strings(months)
	if n > 0 && len(months) > n {
		months = months[len(months)-n:]
	}
	out := make([]Bucket, 0, len(months))
	for _, m := range months {
		out = append(out, Bucket{Label: m, Count: counts[m]})
	}
	return out
}

// --- Local editing & change tracking (FR-2 / FR-1.5 / FR-12.6) ---

// EditTestField applies a local edit to a Test field, coalescing it into the
// per-field pending change for this Test and writing an audit entry. The
// editable fields are whitelisted (see editableFields). Reverting back to the
// original value drops the pending change.
//
// The audit log records every individual edit faithfully; only the
// pending_change table is coalesced.
func (r *Repository) EditTestField(profileID, testKey, field, newValue string) error {
	col, ok := editableFields[field]
	if !ok {
		return fmt.Errorf("field %q is not editable", field)
	}

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var currentVal, baseVersion string
	readSQL := fmt.Sprintf(
		`SELECT %s, updated_at FROM test_case WHERE profile_id = ? AND jira_key = ?`,
		col,
	)
	err = tx.QueryRow(readSQL, profileID, testKey).Scan(&currentVal, &baseVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read current value: %w", err)
	}

	if currentVal == newValue {
		return nil // no-op
	}

	updateSQL := fmt.Sprintf(
		`UPDATE test_case SET %s = ? WHERE profile_id = ? AND jira_key = ?`, col,
	)
	if _, err := tx.Exec(updateSQL, newValue, profileID, testKey); err != nil {
		return fmt.Errorf("update test_case: %w", err)
	}

	if err := upsertPendingChange(
		tx, profileID, entityTestCase, testKey, field, currentVal, newValue, baseVersion,
	); err != nil {
		return err
	}
	if err := writeAudit(
		tx, profileID, entityTestCase, testKey,
		"edit-local", field, currentVal, newValue, "",
	); err != nil {
		return err
	}
	return tx.Commit()
}

// stepFields whitelists which Step columns can be edited via
// EditTestStepField. The map value is the on-disk column name in test_step.
var stepFields = map[string]string{
	"action":   "action",
	"data":     "data",
	"expected": "expected",
}

// EditTestStepField applies a local edit to one field of one Test Step
// (FR-2.5). The change is queued in pending_change with entity_type =
// "test_step" and entity_key = "<testKey>:<xrayID>" so the commit path
// can route step updates to /rest/raven/2.0/api/test/{key}/steps/{stepId}
// while keeping the same coalesce / discard machinery as test_case fields.
func (r *Repository) EditTestStepField(profileID, testKey, xrayID, field, newValue string) error {
	col, ok := stepFields[field]
	if !ok {
		return fmt.Errorf("step field %q is not editable", field)
	}

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// The conflict base_version we capture is the parent Test's updated_at —
	// step edits without parallel field edits still want to conflict-check
	// against the same remote "updated" the syncer reads.
	var currentVal, baseVersion string
	readSQL := fmt.Sprintf(
		`SELECT s.%s, t.updated_at
		   FROM test_step s
		   JOIN test_case t
		     ON t.profile_id = s.profile_id AND t.jira_key = s.test_key
		   WHERE s.profile_id = ? AND s.test_key = ? AND s.xray_id = ?`,
		col,
	)
	err = tx.QueryRow(readSQL, profileID, testKey, xrayID).Scan(&currentVal, &baseVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read current step value: %w", err)
	}
	if currentVal == newValue {
		return nil
	}

	updateSQL := fmt.Sprintf(
		`UPDATE test_step SET %s = ? WHERE profile_id = ? AND test_key = ? AND xray_id = ?`, col,
	)
	if _, err := tx.Exec(updateSQL, newValue, profileID, testKey, xrayID); err != nil {
		return fmt.Errorf("update test_step: %w", err)
	}

	ek := stepEntityKey(testKey, xrayID)
	// A not-yet-committed step has no remote id to PUT against, so its edits
	// fold into the queued add's JSON instead of becoming standalone
	// step-edit rows. Reverting such an edit doesn't drop anything — the add
	// row stays until the step is committed or discarded.
	folded, err := foldStepEditIntoAdd(tx, profileID, ek, field, newValue)
	if err != nil {
		return err
	}
	if !folded {
		if err := upsertPendingChange(
			tx, profileID, entityTestStep, ek, field, currentVal, newValue, baseVersion,
		); err != nil {
			return err
		}
	}
	if err := writeAudit(
		tx, profileID, entityTestStep, ek,
		"edit-local", field, currentVal, newValue, "",
	); err != nil {
		return err
	}
	return tx.Commit()
}

// foldStepEditIntoAdd updates the field inside a pending test_step_add row's
// JSON snapshot, returning false (without touching anything) when no add row
// exists for the step. This keeps a brand-new step to a single pending row so
// the commit POST carries the latest content.
func foldStepEditIntoAdd(tx *sql.Tx, profileID, ek, field, newValue string) (bool, error) {
	var afterVal string
	err := tx.QueryRow(
		`SELECT after_val FROM pending_change
		 WHERE profile_id = ? AND entity_type = ? AND entity_key = ? AND field = 'step'`,
		profileID, entityTestStepAdd, ek,
	).Scan(&afterVal)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read pending add: %w", err)
	}

	var s Step
	if err := json.Unmarshal([]byte(afterVal), &s); err != nil {
		return false, fmt.Errorf("decode pending add: %w", err)
	}
	switch field {
	case "action":
		s.Action = newValue
	case "data":
		s.Data = newValue
	case "expected":
		s.Expected = newValue
	}
	snapshot, err := json.Marshal(s)
	if err != nil {
		return false, fmt.Errorf("marshal pending add: %w", err)
	}
	if _, err := tx.Exec(
		`UPDATE pending_change SET after_val = ?, created_at = ?
		 WHERE profile_id = ? AND entity_type = ? AND entity_key = ? AND field = 'step'`,
		string(snapshot), time.Now().UTC().Format(time.RFC3339),
		profileID, entityTestStepAdd, ek,
	); err != nil {
		return false, fmt.Errorf("update pending add: %w", err)
	}
	return true, nil
}

// DeleteTestStep queues a Test Step for deletion (FR-2.5). The step is
// hidden from the local list immediately; the actual DELETE call to Xray
// fires at commit time. Discarding the pending row restores the local
// step from the JSON snapshot stashed in before_val.
//
// Parent test_case.updated_at is captured as base_version so the conflict
// pre-check at commit time uses the same timestamp the field-edit path
// does — a delete and an unrelated remote update on the same Test still
// surface as a conflict.
func (r *Repository) DeleteTestStep(profileID, testKey, xrayID string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var s Step
	err = tx.QueryRow(
		`SELECT xray_id, idx, action, data, expected, called_test_key
		   FROM test_step WHERE profile_id = ? AND test_key = ? AND xray_id = ?`,
		profileID, testKey, xrayID,
	).Scan(&s.XrayID, &s.Index, &s.Action, &s.Data, &s.Expected, &s.CalledTestKey)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read step: %w", err)
	}

	snapshot, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	ek := stepEntityKey(testKey, xrayID)

	if _, err := tx.Exec(
		`DELETE FROM test_step WHERE profile_id = ? AND test_key = ? AND xray_id = ?`,
		profileID, testKey, xrayID,
	); err != nil {
		return fmt.Errorf("delete test_step: %w", err)
	}

	// Deleting a step that was only ever added locally cancels the add — the
	// step never reached Xray, so there's nothing to delete remotely. Drop
	// the queued add (its folded edits go with it) instead of recording a
	// delete.
	var addID int64
	err = tx.QueryRow(
		`SELECT id FROM pending_change
		 WHERE profile_id = ? AND entity_type = ? AND entity_key = ? AND field = 'step'`,
		profileID, entityTestStepAdd, ek,
	).Scan(&addID)
	if err == nil {
		if _, derr := tx.Exec(
			`DELETE FROM pending_change WHERE profile_id = ? AND id = ?`,
			profileID, addID,
		); derr != nil {
			return fmt.Errorf("cancel pending add: %w", derr)
		}
		// The cancelled step never reached Xray, so its temporary "new-N" id must
		// not linger in a queued reorder — committing one would PUT a reorder
		// against a step Xray never created and fail (RND_P_4TFINT_05-203).
		if perr := pruneStepFromOrder(tx, profileID, testKey, xrayID); perr != nil {
			return perr
		}
		if aerr := writeAudit(
			tx, profileID, entityTestStepAdd, ek,
			"add-cancelled", "step", string(snapshot), "", "",
		); aerr != nil {
			return aerr
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("probe pending add: %w", err)
	}

	// Deleting a committed step: any queued field edits on it are superseded
	// by the delete — drop them so the commit doesn't PUT a step it's about
	// to DELETE.
	if _, err := tx.Exec(
		`DELETE FROM pending_change
		 WHERE profile_id = ? AND entity_type = ? AND entity_key = ?`,
		profileID, entityTestStep, ek,
	); err != nil {
		return fmt.Errorf("clear superseded step edits: %w", err)
	}

	var baseVersion string
	if err := tx.QueryRow(
		`SELECT updated_at FROM test_case WHERE profile_id = ? AND jira_key = ?`,
		profileID, testKey,
	).Scan(&baseVersion); err != nil {
		return fmt.Errorf("read parent updated_at: %w", err)
	}
	if err := upsertPendingChange(
		tx, profileID, entityTestStepDelete, ek, "step",
		string(snapshot), "1", baseVersion,
	); err != nil {
		return err
	}
	if err := writeAudit(
		tx, profileID, entityTestStepDelete, ek,
		"delete-local", "step", string(snapshot), "1", "",
	); err != nil {
		return err
	}
	return tx.Commit()
}

// pruneStepFromOrder removes xrayID from this Test's queued step reorder, if one
// exists, keeping the pending order self-consistent when a step is dropped. If
// removing the id leaves the order identical to its original, the reorder is a
// no-op and the pending row is deleted (mirroring revert-to-original
// coalescing). Used when a locally-added step is cancelled so its temporary
// "new-N" id can't survive in the order and make the commit reorder a step Xray
// never created (RND_P_4TFINT_05-203).
func pruneStepFromOrder(tx *sql.Tx, profileID, testKey, xrayID string) error {
	var id int64
	var beforeJSON, afterJSON string
	err := tx.QueryRow(
		`SELECT id, before_val, after_val FROM pending_change
		 WHERE profile_id = ? AND entity_type = ? AND entity_key = ? AND field = 'order'`,
		profileID, entityTestStepOrder, testKey,
	).Scan(&id, &beforeJSON, &afterJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("probe pending order: %w", err)
	}

	var before, after []string
	if err := json.Unmarshal([]byte(beforeJSON), &before); err != nil {
		return fmt.Errorf("unmarshal order before: %w", err)
	}
	if err := json.Unmarshal([]byte(afterJSON), &after); err != nil {
		return fmt.Errorf("unmarshal order after: %w", err)
	}
	before = removeString(before, xrayID)
	after = removeString(after, xrayID)

	if equalOrder(before, after) {
		_, derr := tx.Exec(`DELETE FROM pending_change WHERE profile_id = ? AND id = ?`, profileID, id)
		return derr
	}
	nb, err := json.Marshal(before)
	if err != nil {
		return fmt.Errorf("marshal pruned order before: %w", err)
	}
	na, err := json.Marshal(after)
	if err != nil {
		return fmt.Errorf("marshal pruned order after: %w", err)
	}
	_, err = tx.Exec(
		`UPDATE pending_change SET before_val = ?, after_val = ? WHERE profile_id = ? AND id = ?`,
		string(nb), string(na), profileID, id,
	)
	return err
}

// removeString returns ss without any element equal to v, preserving order.
func removeString(ss []string, v string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if s != v {
			out = append(out, s)
		}
	}
	return out
}

// AddTestStep appends a new Step to a Test locally and queues it for creation
// in Xray on commit (FR-2.5). The new step gets a temporary xray_id ("new-N")
// because Xray only assigns the real one when the POST lands; the commit path
// renames the cached row to the real id afterwards. The step content travels
// in the pending row's after_val as JSON so the commit POST has everything it
// needs, and later field edits on a not-yet-committed step fold into that same
// JSON (see EditTestStepField) rather than spawning step-edit rows that would
// PUT to a non-existent remote step.
//
// Returns the created Step so the caller can render it without a re-fetch.
func (r *Repository) AddTestStep(profileID, testKey, action, data, expected string) (Step, error) {
	return r.addStep(profileID, testKey, Step{Action: action, Data: data, Expected: expected})
}

// AddCalledTestStep appends a "call test" step — a step that invokes another
// Test (Xray test call) rather than a manual action/data/expected step. Queued
// for commit like any other step add (FR-2.5, #2).
func (r *Repository) AddCalledTestStep(profileID, testKey, calledTestKey string) (Step, error) {
	if strings.TrimSpace(calledTestKey) == "" {
		return Step{}, fmt.Errorf("a called test key is required")
	}
	if calledTestKey == testKey {
		return Step{}, fmt.Errorf("a test cannot call itself")
	}
	return r.addStep(profileID, testKey, Step{CalledTestKey: calledTestKey})
}

// addStep is the shared implementation behind AddTestStep / AddCalledTestStep:
// it appends s (XrayID and Index are assigned here) and queues a step-add.
func (r *Repository) addStep(profileID, testKey string, s Step) (Step, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return Step{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var baseVersion string
	err = tx.QueryRow(
		`SELECT updated_at FROM test_case WHERE profile_id = ? AND jira_key = ?`,
		profileID, testKey,
	).Scan(&baseVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return Step{}, ErrNotFound
	}
	if err != nil {
		return Step{}, fmt.Errorf("read parent updated_at: %w", err)
	}

	var nextIdx int
	if err := tx.QueryRow(
		`SELECT COALESCE(MAX(idx), 0) + 1 FROM test_step WHERE profile_id = ? AND test_key = ?`,
		profileID, testKey,
	).Scan(&nextIdx); err != nil {
		return Step{}, fmt.Errorf("compute next step index: %w", err)
	}

	tempID, err := nextTempStepID(tx, profileID, testKey)
	if err != nil {
		return Step{}, err
	}

	s.XrayID = tempID
	s.Index = nextIdx
	if _, err := tx.Exec(
		`INSERT INTO test_step (profile_id, test_key, xray_id, idx, action, data, expected, called_test_key)
		   VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		profileID, testKey, s.XrayID, s.Index, s.Action, s.Data, s.Expected, s.CalledTestKey,
	); err != nil {
		return Step{}, fmt.Errorf("insert test_step: %w", err)
	}

	snapshot, err := json.Marshal(s)
	if err != nil {
		return Step{}, fmt.Errorf("marshal step: %w", err)
	}
	ek := stepEntityKey(testKey, tempID)
	if err := upsertPendingChange(
		tx, profileID, entityTestStepAdd, ek, "step", "", string(snapshot), baseVersion,
	); err != nil {
		return Step{}, err
	}
	if err := writeAudit(
		tx, profileID, entityTestStepAdd, ek,
		"add-local", "step", "", string(snapshot), "",
	); err != nil {
		return Step{}, err
	}
	if err := tx.Commit(); err != nil {
		return Step{}, fmt.Errorf("commit add step: %w", err)
	}
	return s, nil
}

// CloneTestSteps appends copies of sourceSteps onto a Test, queuing each as a
// local step-add (the same path as AddTestStep) so the clones commit to Xray
// like any hand-added step. Steps are appended in source order, after any steps
// the Test already has. Returns the Test's full step list after the clone.
func (r *Repository) CloneTestSteps(profileID, targetKey string, sourceSteps []Step) ([]Step, error) {
	for _, s := range sourceSteps {
		if _, err := r.AddTestStep(profileID, targetKey, s.Action, s.Data, s.Expected); err != nil {
			return nil, err
		}
	}
	return r.ListTestSteps(profileID, targetKey)
}

// nextTempStepID returns a step xray_id of the form "new-N" not already used
// by another step on this Test. New steps need a placeholder id until Xray
// assigns the real one at commit time.
func nextTempStepID(tx *sql.Tx, profileID, testKey string) (string, error) {
	for n := 1; ; n++ {
		id := fmt.Sprintf("new-%d", n)
		var one int
		err := tx.QueryRow(
			`SELECT 1 FROM test_step WHERE profile_id = ? AND test_key = ? AND xray_id = ?`,
			profileID, testKey, id,
		).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return id, nil
		}
		if err != nil {
			return "", fmt.Errorf("probe temp step id: %w", err)
		}
	}
}

// RenameTestStepID rewrites a cached step's xray_id, used by the commit path
// to swap a "new-N" placeholder for the real id Xray returned from the create
// POST. A no-op when newID is empty (Xray didn't return one) or unchanged.
func (r *Repository) RenameTestStepID(profileID, testKey, oldID, newID string) error {
	if newID == "" || newID == oldID {
		return nil
	}
	if _, err := r.db.Exec(
		`UPDATE test_step SET xray_id = ?
		   WHERE profile_id = ? AND test_key = ? AND xray_id = ?`,
		newID, profileID, testKey, oldID,
	); err != nil {
		return fmt.Errorf("rename step id: %w", err)
	}
	return nil
}

// ReorderTestSteps records a new ordering for a Test's steps (FR-2.5). The
// caller passes the full set of step xray_ids in their new order; the order is
// stored as a single test-level pending row (before/after id-lists) rather
// than per-step edits, because reordering is a property of the list, not of
// any one step. Xray gets the new positions on commit.
//
// orderedXrayIDs must be exactly the current step set, just permuted — adding
// or removing a step is the job of AddTestStep / DeleteTestStep, not this.
// Reordering back to the original order drops the pending row.
func (r *Repository) ReorderTestSteps(profileID, testKey string, orderedXrayIDs []string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.Query(
		`SELECT xray_id FROM test_step WHERE profile_id = ? AND test_key = ? ORDER BY idx`,
		profileID, testKey,
	)
	if err != nil {
		return fmt.Errorf("read current step order: %w", err)
	}
	currentIDs := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		currentIDs = append(currentIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	if len(currentIDs) == 0 {
		return fmt.Errorf("test %s has no steps to reorder", testKey)
	}
	if !samePermutation(currentIDs, orderedXrayIDs) {
		return fmt.Errorf("reorder set must match the current steps exactly")
	}
	if equalOrder(currentIDs, orderedXrayIDs) {
		return nil // no-op
	}

	var baseVersion string
	if err := tx.QueryRow(
		`SELECT updated_at FROM test_case WHERE profile_id = ? AND jira_key = ?`,
		profileID, testKey,
	).Scan(&baseVersion); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("read parent updated_at: %w", err)
	}

	if err := applyStepOrder(tx, profileID, testKey, orderedXrayIDs); err != nil {
		return err
	}

	beforeJSON, err := json.Marshal(currentIDs)
	if err != nil {
		return fmt.Errorf("marshal current order: %w", err)
	}
	afterJSON, err := json.Marshal(orderedXrayIDs)
	if err != nil {
		return fmt.Errorf("marshal new order: %w", err)
	}
	if err := upsertPendingChange(
		tx, profileID, entityTestStepOrder, testKey, "order",
		string(beforeJSON), string(afterJSON), baseVersion,
	); err != nil {
		return err
	}
	if err := writeAudit(
		tx, profileID, entityTestStepOrder, testKey,
		"reorder-local", "order", string(beforeJSON), string(afterJSON), "",
	); err != nil {
		return err
	}
	return tx.Commit()
}

// samePermutation reports whether a and b contain exactly the same ids
// (ignoring order). Used to reject a reorder that secretly adds or drops a
// step.
func samePermutation(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, x := range a {
		counts[x]++
	}
	for _, x := range b {
		counts[x]--
		if counts[x] < 0 {
			return false
		}
	}
	return true
}

// equalOrder reports whether a and b are identical in order.
func equalOrder(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// applyStepOrder rewrites the idx column so steps fall in the order given by
// orderedXrayIDs. Shared by the reorder-discard path. Caller supplies an open
// transaction.
func applyStepOrder(tx *sql.Tx, profileID, testKey string, orderedXrayIDs []string) error {
	for i, id := range orderedXrayIDs {
		if _, err := tx.Exec(
			`UPDATE test_step SET idx = ? WHERE profile_id = ? AND test_key = ? AND xray_id = ?`,
			i+1, profileID, testKey, id,
		); err != nil {
			return fmt.Errorf("restore step index: %w", err)
		}
	}
	return nil
}

// DiscardPendingChange reverts a Test field to its before_val and removes the
// pending change. An audit entry records the discard.
func (r *Repository) DiscardPendingChange(profileID string, changeID int64) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var entityType, entityKey, field, beforeVal, afterVal string
	err = tx.QueryRow(
		`SELECT entity_type, entity_key, field, before_val, after_val FROM pending_change
		 WHERE profile_id = ? AND id = ?`,
		profileID, changeID,
	).Scan(&entityType, &entityKey, &field, &beforeVal, &afterVal)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("pending change %d not found", changeID)
	}
	if err != nil {
		return fmt.Errorf("read pending change: %w", err)
	}

	switch entityType {
	case entityTestCase:
		col, ok := columnForField(field)
		if !ok {
			return fmt.Errorf("field %q is not editable (audit log corrupt?)", field)
		}
		revertSQL := fmt.Sprintf(
			`UPDATE test_case SET %s = ? WHERE profile_id = ? AND jira_key = ?`, col,
		)
		if _, err := tx.Exec(revertSQL, beforeVal, profileID, entityKey); err != nil {
			return fmt.Errorf("revert test_case: %w", err)
		}
	case entityTestStep:
		col, ok := stepFields[field]
		if !ok {
			return fmt.Errorf("step field %q is not editable (audit log corrupt?)", field)
		}
		testKey, xrayID, ok := parseStepEntityKey(entityKey)
		if !ok {
			return fmt.Errorf("malformed step entity_key %q", entityKey)
		}
		revertSQL := fmt.Sprintf(
			`UPDATE test_step SET %s = ?
			   WHERE profile_id = ? AND test_key = ? AND xray_id = ?`, col,
		)
		if _, err := tx.Exec(revertSQL, beforeVal, profileID, testKey, xrayID); err != nil {
			return fmt.Errorf("revert test_step: %w", err)
		}
	case entityTestStepDelete:
		testKey, _, ok := parseStepEntityKey(entityKey)
		if !ok {
			return fmt.Errorf("malformed step entity_key %q", entityKey)
		}
		var snap Step
		if err := json.Unmarshal([]byte(beforeVal), &snap); err != nil {
			return fmt.Errorf("decode step snapshot: %w", err)
		}
		if _, err := tx.Exec(
			`INSERT INTO test_step (profile_id, test_key, xray_id, idx, action, data, expected, called_test_key)
			   VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			profileID, testKey, snap.XrayID, snap.Index, snap.Action, snap.Data, snap.Expected, snap.CalledTestKey,
		); err != nil {
			return fmt.Errorf("restore test_step: %w", err)
		}
	case entityTestStepAdd:
		// Discarding an add removes the locally-created step entirely — it
		// never existed remotely, so there's nothing to restore.
		testKey, xrayID, ok := parseStepEntityKey(entityKey)
		if !ok {
			return fmt.Errorf("malformed step entity_key %q", entityKey)
		}
		if _, err := tx.Exec(
			`DELETE FROM test_step WHERE profile_id = ? AND test_key = ? AND xray_id = ?`,
			profileID, testKey, xrayID,
		); err != nil {
			return fmt.Errorf("remove added test_step: %w", err)
		}
	case entityTestStepOrder:
		// entity_key is the test key itself; restore the idx column from the
		// original order snapshot in before_val.
		var order []string
		if err := json.Unmarshal([]byte(beforeVal), &order); err != nil {
			return fmt.Errorf("decode order snapshot: %w", err)
		}
		if err := applyStepOrder(tx, profileID, entityKey, order); err != nil {
			return err
		}
	case entityMembershipAdd:
		// entity_key is the Container key; remove the locally-added
		// memberships listed in the after_val payload. Pre-existing
		// memberships were never queued, so they're untouched.
		var payload membershipPayload
		if err := json.Unmarshal([]byte(afterVal), &payload); err != nil {
			return fmt.Errorf("decode membership payload: %w", err)
		}
		for _, testKey := range payload.Members {
			if _, err := tx.Exec(
				`DELETE FROM test_container_test
				 WHERE profile_id = ? AND container_key = ? AND test_key = ?`,
				profileID, entityKey, testKey,
			); err != nil {
				return fmt.Errorf("remove membership: %w", err)
			}
		}
	case entityMembershipRemove:
		// entity_key is the Container key; restore the locally-removed
		// memberships listed in the after_val payload.
		var payload membershipPayload
		if err := json.Unmarshal([]byte(afterVal), &payload); err != nil {
			return fmt.Errorf("decode membership payload: %w", err)
		}
		for _, testKey := range payload.Members {
			if _, err := tx.Exec(
				`INSERT OR IGNORE INTO test_container_test
				   (profile_id, container_key, test_key, run_status)
				 VALUES (?, ?, ?, '')`,
				profileID, entityKey, testKey,
			); err != nil {
				return fmt.Errorf("restore membership: %w", err)
			}
		}
	case entityCustomField:
		// entity_key is "<testKey>:<fieldId>"; revert the cached value.
		testKey, fieldID, ok := parseStepEntityKey(entityKey)
		if !ok {
			return fmt.Errorf("malformed custom field entity_key %q", entityKey)
		}
		if _, err := tx.Exec(
			`UPDATE test_custom_field SET value = ?
			 WHERE profile_id = ? AND test_key = ? AND field_id = ?`,
			beforeVal, profileID, testKey, fieldID,
		); err != nil {
			return fmt.Errorf("revert custom field: %w", err)
		}
	case entityIssueComment:
		// A queued comment has no local cache state — discarding it just drops
		// the pending row (handled after the switch).
	case entityRequirementEdit:
		// Revert the requirement field to the before value.
		col, ok := requirementEditFields[field]
		if !ok {
			return fmt.Errorf("unknown requirement field %q", field)
		}
		if _, err := tx.Exec(
			fmt.Sprintf(`UPDATE requirement SET %s = ? WHERE profile_id = ? AND jira_key = ?`, col),
			beforeVal, profileID, entityKey,
		); err != nil {
			return fmt.Errorf("revert requirement field: %w", err)
		}
	case entityRequirementDelete:
		// Restore the deleted requirement and its Test links from the snapshot.
		var snap requirementDeleteSnapshot
		if err := json.Unmarshal([]byte(beforeVal), &snap); err != nil {
			return fmt.Errorf("decode requirement snapshot: %w", err)
		}
		if _, err := tx.Exec(
			`INSERT INTO requirement (profile_id, jira_key, project_key, issue_type, summary, status, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			profileID, entityKey, snap.ProjectKey, snap.IssueType, snap.Summary, snap.Status, snap.Updated,
		); err != nil {
			return fmt.Errorf("restore requirement: %w", err)
		}
		for _, l := range snap.Links {
			if _, err := tx.Exec(
				`INSERT OR IGNORE INTO test_requirement (profile_id, test_key, requirement_key, link_id)
				 VALUES (?, ?, ?, ?)`,
				profileID, l.Key, entityKey, l.LinkID,
			); err != nil {
				return fmt.Errorf("restore requirement link: %w", err)
			}
		}
	case entityRequirementSet:
		// entity_key is the test key; restore the original requirement links
		// (with their Jira link ids) from the before snapshot.
		var snap []struct {
			Key    string `json:"key"`
			LinkID string `json:"linkId"`
		}
		if err := json.Unmarshal([]byte(beforeVal), &snap); err != nil {
			return fmt.Errorf("decode requirement snapshot: %w", err)
		}
		if _, err := tx.Exec(
			`DELETE FROM test_requirement WHERE profile_id = ? AND test_key = ?`,
			profileID, entityKey,
		); err != nil {
			return fmt.Errorf("clear requirement links: %w", err)
		}
		for _, l := range snap {
			if _, err := tx.Exec(
				`INSERT INTO test_requirement (profile_id, test_key, requirement_key, link_id)
				 VALUES (?, ?, ?, ?)`,
				profileID, entityKey, l.Key, l.LinkID,
			); err != nil {
				return fmt.Errorf("restore requirement link: %w", err)
			}
		}
	case entityReqReqLinkSet:
		// entity_key is the fromKey, field is the linkType; restore the prior
		// outbound links (with their Jira link ids) from the before snapshot.
		var snap []reqqLinkSnap
		if err := json.Unmarshal([]byte(beforeVal), &snap); err != nil {
			return fmt.Errorf("decode req-link snapshot: %w", err)
		}
		if _, err := tx.Exec(
			`DELETE FROM requirement_link
			 WHERE profile_id = ? AND from_requirement_key = ? AND link_type = ?`,
			profileID, entityKey, field,
		); err != nil {
			return fmt.Errorf("clear req links: %w", err)
		}
		for _, l := range snap {
			if _, err := tx.Exec(
				`INSERT INTO requirement_link
				   (profile_id, from_requirement_key, to_requirement_key, link_type, link_id)
				 VALUES (?, ?, ?, ?, ?)`,
				profileID, entityKey, l.ToKey, field, l.LinkID,
			); err != nil {
				return fmt.Errorf("restore req link: %w", err)
			}
		}
	case entityTestRun:
		// Revert the Test's run status in the execution (entity_key is
		// "<execKey>:<testKey>").
		execKey, runTestKey, ok := splitRunKey(entityKey)
		if !ok {
			return fmt.Errorf("malformed run-status key %q", entityKey)
		}
		if _, err := tx.Exec(
			`UPDATE test_container_test SET run_status = ?
			 WHERE profile_id = ? AND container_key = ? AND test_key = ?`,
			beforeVal, profileID, execKey, runTestKey,
		); err != nil {
			return fmt.Errorf("revert run status: %w", err)
		}
	case entityTestReview:
		// Restore the prior review from the before snapshot (empty verdict
		// means there was no review, so the row is removed).
		var prev Review
		if err := json.Unmarshal([]byte(beforeVal), &prev); err != nil {
			return fmt.Errorf("decode review snapshot: %w", err)
		}
		if prev.Verdict == "" {
			if _, err := tx.Exec(
				`DELETE FROM test_review WHERE profile_id = ? AND test_key = ?`,
				profileID, entityKey,
			); err != nil {
				return fmt.Errorf("revert review: %w", err)
			}
		} else if _, err := tx.Exec(
			`INSERT INTO test_review (profile_id, test_key, verdict, reviewer, note, reviewed_at)
			 VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT(profile_id, test_key) DO UPDATE SET
			   verdict = excluded.verdict, reviewer = excluded.reviewer,
			   note = excluded.note, reviewed_at = excluded.reviewed_at`,
			profileID, entityKey, prev.Verdict, prev.Reviewer, prev.Note, prev.ReviewedAt,
		); err != nil {
			return fmt.Errorf("revert review: %w", err)
		}
	case entityTestCreate:
		// entity_key is the temporary Test key; discarding removes the
		// not-yet-created Test and any steps imported with it.
		if _, err := tx.Exec(
			`DELETE FROM test_case WHERE profile_id = ? AND jira_key = ?`,
			profileID, entityKey,
		); err != nil {
			return fmt.Errorf("remove imported test: %w", err)
		}
		if _, err := tx.Exec(
			`DELETE FROM test_step WHERE profile_id = ? AND test_key = ?`,
			profileID, entityKey,
		); err != nil {
			return fmt.Errorf("remove imported test steps: %w", err)
		}
	case entityRequirementCreate:
		// entity_key is the temporary requirement key; discarding removes the
		// not-yet-created requirement.
		if _, err := tx.Exec(
			`DELETE FROM requirement WHERE profile_id = ? AND jira_key = ?`,
			profileID, entityKey,
		); err != nil {
			return fmt.Errorf("remove local requirement: %w", err)
		}
	case entityBugCreate:
		// entity_key is the temporary bug key; discarding removes the
		// not-yet-created bug and its Test link.
		if _, err := tx.Exec(
			`DELETE FROM bug WHERE profile_id = ? AND jira_key = ?`,
			profileID, entityKey,
		); err != nil {
			return fmt.Errorf("remove local bug: %w", err)
		}
		if _, err := tx.Exec(
			`DELETE FROM test_bug WHERE profile_id = ? AND bug_key = ?`,
			profileID, entityKey,
		); err != nil {
			return fmt.Errorf("remove local bug link: %w", err)
		}
	case entityFolderCreate:
		// entity_key is the new folder path; discarding removes the
		// locally-created (empty) folder.
		if _, err := tx.Exec(
			`DELETE FROM test_folder WHERE profile_id = ? AND id = ?`,
			profileID, entityKey,
		); err != nil {
			return fmt.Errorf("remove created folder: %w", err)
		}
	case entityFolderRename:
		// Reverse the rename: entity_key is the current (new) path; before_val
		// snapshots the original path + name.
		var snap folderRenameSnapshot
		if err := json.Unmarshal([]byte(beforeVal), &snap); err != nil {
			return fmt.Errorf("decode folder rename snapshot: %w", err)
		}
		if err := renameFolderTree(tx, profileID, entityKey, snap.Path, snap.Name); err != nil {
			return err
		}
	case entityFolderDelete:
		// Restore the deleted (empty) folder from the snapshot.
		var snap folderDeleteSnapshot
		if err := json.Unmarshal([]byte(beforeVal), &snap); err != nil {
			return fmt.Errorf("decode folder delete snapshot: %w", err)
		}
		if _, err := tx.Exec(
			`INSERT INTO test_folder (profile_id, id, parent_id, name) VALUES (?, ?, ?, ?)`,
			profileID, entityKey, snap.ParentPath, snap.Name,
		); err != nil {
			return fmt.Errorf("restore deleted folder: %w", err)
		}
	case entityPreconditionEdit:
		// entity_key is the Precondition key; revert the edited column.
		col, ok := preconditionFields[field]
		if !ok {
			return fmt.Errorf("precondition field %q is not editable (audit log corrupt?)", field)
		}
		revertSQL := fmt.Sprintf(
			`UPDATE precondition SET %s = ? WHERE profile_id = ? AND jira_key = ?`, col,
		)
		if _, err := tx.Exec(revertSQL, beforeVal, profileID, entityKey); err != nil {
			return fmt.Errorf("revert precondition: %w", err)
		}
	case entityPreconditionSet:
		// entity_key is the test key; restore the original Precondition set
		// from the before snapshot.
		var set []string
		if err := json.Unmarshal([]byte(beforeVal), &set); err != nil {
			return fmt.Errorf("decode precondition snapshot: %w", err)
		}
		if _, err := tx.Exec(
			`DELETE FROM test_precondition WHERE profile_id = ? AND test_key = ?`,
			profileID, entityKey,
		); err != nil {
			return fmt.Errorf("clear precondition links: %w", err)
		}
		for _, pk := range set {
			if _, err := tx.Exec(
				`INSERT INTO test_precondition (profile_id, test_key, precondition_key)
				 VALUES (?, ?, ?)`, profileID, entityKey, pk,
			); err != nil {
				return fmt.Errorf("restore precondition link: %w", err)
			}
		}
	case entityPreconditionAdd:
		// entity_key is the temporary Precondition key; discarding removes the
		// not-yet-created Precondition, its Test links, and any pending
		// association referencing it.
		if _, err := tx.Exec(
			`DELETE FROM precondition WHERE profile_id = ? AND jira_key = ?`,
			profileID, entityKey,
		); err != nil {
			return fmt.Errorf("remove precondition: %w", err)
		}
		if _, err := tx.Exec(
			`DELETE FROM test_precondition WHERE profile_id = ? AND precondition_key = ?`,
			profileID, entityKey,
		); err != nil {
			return fmt.Errorf("remove precondition links: %w", err)
		}
		if err := rewritePreconditionSets(tx, profileID, entityKey, ""); err != nil {
			return err
		}
	case entityPreconditionDelete:
		// Restore the deleted Precondition and its Test links from the snapshot.
		var snap preconditionDeleteSnapshot
		if err := json.Unmarshal([]byte(beforeVal), &snap); err != nil {
			return fmt.Errorf("decode precondition snapshot: %w", err)
		}
		if _, err := tx.Exec(
			`INSERT INTO precondition (profile_id, jira_key, summary, type, description, condition)
			   VALUES (?, ?, ?, ?, ?, ?)`,
			profileID, entityKey, snap.Summary, snap.Type, snap.Description, snap.Condition,
		); err != nil {
			return fmt.Errorf("restore precondition: %w", err)
		}
		for _, tk := range snap.Tests {
			if _, err := tx.Exec(
				`INSERT OR IGNORE INTO test_precondition (profile_id, test_key, precondition_key)
				   VALUES (?, ?, ?)`,
				profileID, tk, entityKey,
			); err != nil {
				return fmt.Errorf("restore precondition link: %w", err)
			}
		}
	case entityContainerEdit:
		// entity_key is the Container key; revert the summary.
		if _, err := tx.Exec(
			`UPDATE test_container SET summary = ? WHERE profile_id = ? AND jira_key = ?`,
			beforeVal, profileID, entityKey,
		); err != nil {
			return fmt.Errorf("revert container summary: %w", err)
		}
	case entityContainerDelete:
		// Restore the deleted Container and its memberships from the snapshot.
		var snap containerDeleteSnapshot
		if err := json.Unmarshal([]byte(beforeVal), &snap); err != nil {
			return fmt.Errorf("decode container snapshot: %w", err)
		}
		if _, err := tx.Exec(
			`INSERT INTO test_container (profile_id, jira_key, kind, summary, status, parent_key, issue_type)
			   VALUES (?, ?, ?, ?, ?, ?, ?)`,
			profileID, entityKey, snap.Kind, snap.Summary, snap.Status, snap.ParentKey, snap.IssueType,
		); err != nil {
			return fmt.Errorf("restore container: %w", err)
		}
		for _, m := range snap.Members {
			if _, err := tx.Exec(
				`INSERT OR IGNORE INTO test_container_test
				   (profile_id, container_key, test_key, run_status) VALUES (?, ?, ?, '')`,
				profileID, entityKey, m,
			); err != nil {
				return fmt.Errorf("restore container membership: %w", err)
			}
		}
	case entityContainerAdd:
		// entity_key is the temporary Container key; discarding drops the
		// not-yet-created Container and all its local memberships.
		if _, err := tx.Exec(
			`DELETE FROM test_container_test WHERE profile_id = ? AND container_key = ?`,
			profileID, entityKey,
		); err != nil {
			return fmt.Errorf("remove container links: %w", err)
		}
		if _, err := tx.Exec(
			`DELETE FROM test_container WHERE profile_id = ? AND jira_key = ?`,
			profileID, entityKey,
		); err != nil {
			return fmt.Errorf("remove container: %w", err)
		}
	default:
		return fmt.Errorf("unknown entity_type %q", entityType)
	}

	if _, err := tx.Exec(
		`DELETE FROM pending_change WHERE profile_id = ? AND id = ?`,
		profileID, changeID,
	); err != nil {
		return fmt.Errorf("delete pending: %w", err)
	}

	if err := writeAudit(
		tx, profileID, entityType, entityKey,
		"discard-pending", field, afterVal, beforeVal,
		fmt.Sprintf("discarded change #%d", changeID),
	); err != nil {
		return err
	}
	return tx.Commit()
}

// DiscardAllPendingChanges reverts every pending change for a profile, reusing
// the per-row discard so each entity type's revert semantics (field rollbacks,
// removing locally-added steps/memberships, restoring deletions) apply exactly
// as for a single discard. Returns the number discarded; on the first failure it
// stops and returns how many were undone so the user can see partial progress.
func (r *Repository) DiscardAllPendingChanges(profileID string) (int, error) {
	rows, err := r.db.Query(
		`SELECT id FROM pending_change WHERE profile_id = ? ORDER BY id`,
		profileID,
	)
	if err != nil {
		return 0, fmt.Errorf("list pending changes: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	discarded := 0
	for _, id := range ids {
		if err := r.DiscardPendingChange(profileID, id); err != nil {
			return discarded, fmt.Errorf("discard change %d: %w", id, err)
		}
		discarded++
	}
	return discarded, nil
}

// TransitionTest queues a workflow transition on a Test (FR-4.2). The
// resulting status is recorded as a pending change on the "status" field;
// commit posts to /rest/api/2/issue/{key}/transitions rather than PUTting
// the status field.
//
// The caller is responsible for picking a targetStatus that's reachable
// from the Test's current status — the UI does this by listing available
// transitions via GetTransitions before invoking this method.
//
// TODO(xtm): multi-step transitions (A->B->C locally) coalesce to a single
// pending row A->C, which needs a direct A->C transition to exist on the
// remote workflow at commit time. A future slice could record the
// transition path instead of just the target status.
func (r *Repository) TransitionTest(profileID, testKey, targetStatus string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var currentVal, baseVersion string
	err = tx.QueryRow(
		`SELECT status, updated_at FROM test_case WHERE profile_id = ? AND jira_key = ?`,
		profileID, testKey,
	).Scan(&currentVal, &baseVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read current status: %w", err)
	}
	if currentVal == targetStatus {
		return nil
	}
	if _, err := tx.Exec(
		`UPDATE test_case SET status = ? WHERE profile_id = ? AND jira_key = ?`,
		targetStatus, profileID, testKey,
	); err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	if err := upsertPendingChange(
		tx, profileID, entityTestCase, testKey, "status", currentVal, targetStatus, baseVersion,
	); err != nil {
		return err
	}
	if err := writeAudit(
		tx, profileID, entityTestCase, testKey,
		"transition-local", "status",
		currentVal, targetStatus, "",
	); err != nil {
		return err
	}
	return tx.Commit()
}

// MoveTestToFolder relocates a Test in the Test Repository tree (FR-13.3),
// queuing the move as a pending change on the "folder" field. Commit pushes it
// via the test-repository API rather than a plain issue PUT. Moving to the
// folder the Test is already in is a no-op; moving back to the original folder
// drops the pending change.
func (r *Repository) MoveTestToFolder(profileID, testKey, targetFolderID string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var currentVal, baseVersion string
	err = tx.QueryRow(
		`SELECT folder_id, updated_at FROM test_case WHERE profile_id = ? AND jira_key = ?`,
		profileID, testKey,
	).Scan(&currentVal, &baseVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read current folder: %w", err)
	}
	if currentVal == targetFolderID {
		return nil
	}
	if _, err := tx.Exec(
		`UPDATE test_case SET folder_id = ? WHERE profile_id = ? AND jira_key = ?`,
		targetFolderID, profileID, testKey,
	); err != nil {
		return fmt.Errorf("update folder: %w", err)
	}
	if err := upsertPendingChange(
		tx, profileID, entityTestCase, testKey, "folder", currentVal, targetFolderID, baseVersion,
	); err != nil {
		return err
	}
	if err := writeAudit(
		tx, profileID, entityTestCase, testKey,
		"move-local", "folder", currentVal, targetFolderID, "",
	); err != nil {
		return err
	}
	return tx.Commit()
}

// BulkMoveToFolder moves a batch of Tests to one Test Repository folder
// (FR-13.3 bulk), queuing a pending change per moved Test. Each Test is moved
// in its own transaction so one failure doesn't block the rest; a Test already
// in the target folder is reported as succeeded.
func (r *Repository) BulkMoveToFolder(profileID string, testKeys []string, targetFolderID string) (BulkEditResult, error) {
	result := BulkEditResult{Succeeded: []string{}, Failed: []BulkFailure{}}
	for _, key := range testKeys {
		if err := r.MoveTestToFolder(profileID, key, targetFolderID); err != nil {
			result.Failed = append(result.Failed, BulkFailure{TestKey: key, Error: err.Error()})
			continue
		}
		result.Succeeded = append(result.Succeeded, key)
	}
	return result, nil
}

// CommitPendingChanges deletes the given pending_change rows and writes a
// "commit" audit entry for each, in one transaction. Called by the sync
// engine after Jira accepts the corresponding PUT for that Test.
func (r *Repository) CommitPendingChanges(profileID string, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	selectStmt, err := tx.Prepare(
		`SELECT entity_type, entity_key, field, before_val, after_val FROM pending_change
		 WHERE profile_id = ? AND id = ?`)
	if err != nil {
		return fmt.Errorf("prepare select: %w", err)
	}
	defer selectStmt.Close()

	deleteStmt, err := tx.Prepare(
		`DELETE FROM pending_change WHERE profile_id = ? AND id = ?`)
	if err != nil {
		return fmt.Errorf("prepare delete: %w", err)
	}
	defer deleteStmt.Close()

	for _, id := range ids {
		var entityType, entityKey, field, beforeVal, afterVal string
		err := selectStmt.QueryRow(profileID, id).Scan(
			&entityType, &entityKey, &field, &beforeVal, &afterVal,
		)
		if errors.Is(err, sql.ErrNoRows) {
			continue // already gone — commit stays idempotent
		}
		if err != nil {
			return fmt.Errorf("read pending %d: %w", id, err)
		}
		if _, err := deleteStmt.Exec(profileID, id); err != nil {
			return fmt.Errorf("delete pending %d: %w", id, err)
		}
		if err := writeAudit(
			tx, profileID, entityType, entityKey,
			"commit", field, beforeVal, afterVal, "",
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RebaseTestConflict re-bases a Test's pending changes onto a newer remote
// version so a re-commit overrides the remote change instead of being held back
// as a conflict (FR-1.4). It matches the Test's own rows plus its step /
// custom-field / review rows, whose entity_key is "<testKey>:...".
func (r *Repository) RebaseTestConflict(profileID, testKey, baseVersion string) error {
	if _, err := r.db.Exec(
		`UPDATE pending_change SET base_version = ?
		 WHERE profile_id = ? AND (entity_key = ? OR entity_key LIKE ?)`,
		baseVersion, profileID, testKey, testKey+":%",
	); err != nil {
		return fmt.Errorf("rebase conflict for %s: %w", testKey, err)
	}
	return nil
}

// DiscardTestChanges drops every pending change belonging to a Test, reverting
// the local cache to match the committed/remote state (FR-1.4 conflict
// resolution — "keep remote"). Step / custom-field / review rows are included.
func (r *Repository) DiscardTestChanges(profileID, testKey string) error {
	rows, err := r.db.Query(
		`SELECT id FROM pending_change
		 WHERE profile_id = ? AND (entity_key = ? OR entity_key LIKE ?)`,
		profileID, testKey, testKey+":%",
	)
	if err != nil {
		return fmt.Errorf("list test pending changes: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := r.DiscardPendingChange(profileID, id); err != nil {
			return err
		}
	}
	return nil
}

// ListPendingChanges returns all uncommitted local edits for a profile,
// newest first.
func (r *Repository) ListPendingChanges(profileID string) ([]PendingChange, error) {
	rows, err := r.db.Query(
		`SELECT id, entity_type, entity_key, field, before_val, after_val, base_version, created_at
		 FROM pending_change WHERE profile_id = ?
		 ORDER BY created_at DESC, id DESC`,
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("list pending changes: %w", err)
	}
	defer rows.Close()

	out := []PendingChange{}
	for rows.Next() {
		var p PendingChange
		if err := rows.Scan(
			&p.ID, &p.EntityType, &p.EntityKey, &p.Field,
			&p.BeforeVal, &p.AfterVal, &p.BaseVersion, &p.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListAuditEntries returns the most recent audit log entries for a profile.
// A limit ≤ 0 or > 1000 defaults to 200.
func (r *Repository) ListAuditEntries(profileID string, limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := r.db.Query(
		`SELECT id, occurred_at, actor, entity_type, entity_key, action, field, before_val, after_val, note
		 FROM audit_log WHERE profile_id = ?
		 ORDER BY occurred_at DESC, id DESC LIMIT ?`,
		profileID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list audit entries: %w", err)
	}
	defer rows.Close()

	out := []AuditEntry{}
	for rows.Next() {
		var a AuditEntry
		if err := rows.Scan(
			&a.ID, &a.OccurredAt, &a.Actor, &a.EntityType, &a.EntityKey,
			&a.Action, &a.Field, &a.BeforeVal, &a.AfterVal, &a.Note,
		); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// --- Bulk operations (FR-3) ---

// BulkEdit describes a single field-level operation to apply to a set of
// Tests. Operations:
//
//   - "set":          replace the field's value with op.Value (any editable field)
//   - "append":       append op.Value to the existing value with a newline
//     (description only)
//   - "add_label":    add op.Value as a label if not already present
//   - "remove_label": remove op.Value from the labels list if present
//
// For label operations the Field is implied to be "labels" regardless of
// what the caller sets.
type BulkEdit struct {
	Operation string `json:"operation"`
	Field     string `json:"field"`
	Value     string `json:"value"`
}

// BulkEditResult reports the outcome of a bulk operation, per Test.
type BulkEditResult struct {
	Succeeded []string      `json:"succeeded"`
	Failed    []BulkFailure `json:"failed"`
}

// BulkFailure is one Test the bulk operation could not be applied to.
type BulkFailure struct {
	TestKey string `json:"testKey"`
	Error   string `json:"error"`
}

// BulkEditTests applies a single field-level operation to a batch of Tests,
// queuing a pending change for each modified Test (FR-3.2 / FR-3.3 / FR-3.7).
// Each Test is processed in its own transaction (via EditTestField) so one
// failure doesn't block the others. No-op edits — for example, add_label
// when the label is already present — are reported as succeeded.
func (r *Repository) BulkEditTests(profileID string, testKeys []string, op BulkEdit) (BulkEditResult, error) {
	result := BulkEditResult{
		Succeeded: []string{},
		Failed:    []BulkFailure{},
	}

	field, err := resolveBulkField(op)
	if err != nil {
		return result, fmt.Errorf("bulk edit: %w", err)
	}
	col, ok := editableFields[field]
	if !ok {
		return result, fmt.Errorf("bulk edit: field %q is not editable", field)
	}

	readSQL := fmt.Sprintf(
		`SELECT %s FROM test_case WHERE profile_id = ? AND jira_key = ?`, col,
	)

	for _, key := range testKeys {
		var current string
		err := r.db.QueryRow(readSQL, profileID, key).Scan(&current)
		if errors.Is(err, sql.ErrNoRows) {
			result.Failed = append(result.Failed, BulkFailure{TestKey: key, Error: "not found"})
			continue
		}
		if err != nil {
			result.Failed = append(result.Failed, BulkFailure{TestKey: key, Error: err.Error()})
			continue
		}
		newVal, applyErr := applyBulkOperation(op, current)
		if applyErr != nil {
			result.Failed = append(result.Failed, BulkFailure{TestKey: key, Error: applyErr.Error()})
			continue
		}
		if newVal == current {
			// No-op (e.g. add_label when the label is already present) —
			// still report success so the user knows the request was handled.
			result.Succeeded = append(result.Succeeded, key)
			continue
		}
		if err := r.EditTestField(profileID, key, field, newVal); err != nil {
			result.Failed = append(result.Failed, BulkFailure{TestKey: key, Error: err.Error()})
			continue
		}
		result.Succeeded = append(result.Succeeded, key)
	}
	return result, nil
}

// resolveBulkField derives which test_case column the operation targets.
// Label operations always target the labels column; other operations need
// an explicit field.
func resolveBulkField(op BulkEdit) (string, error) {
	if op.Operation == "add_label" || op.Operation == "remove_label" {
		return "labels", nil
	}
	if op.Field == "" {
		return "", fmt.Errorf("field is required")
	}
	return op.Field, nil
}

// applyBulkOperation computes the new field value given the current value
// and the operation. It does not write anything.
func applyBulkOperation(op BulkEdit, current string) (string, error) {
	switch op.Operation {
	case "set":
		return op.Value, nil

	case "append":
		if op.Field != "description" {
			return "", fmt.Errorf("append is only supported for description")
		}
		if current == "" {
			return op.Value, nil
		}
		return current + "\n" + op.Value, nil

	case "add_label":
		if strings.TrimSpace(op.Value) == "" {
			return "", fmt.Errorf("label value is required")
		}
		labels := strings.Fields(current)
		for _, l := range labels {
			if l == op.Value {
				return current, nil
			}
		}
		labels = append(labels, op.Value)
		return strings.Join(labels, " "), nil

	case "remove_label":
		if strings.TrimSpace(op.Value) == "" {
			return "", fmt.Errorf("label value is required")
		}
		labels := strings.Fields(current)
		out := make([]string, 0, len(labels))
		for _, l := range labels {
			if l != op.Value {
				out = append(out, l)
			}
		}
		return strings.Join(out, " "), nil
	}
	return "", fmt.Errorf("unknown operation %q", op.Operation)
}

// --- Helpers ---

// Entity types for pending_change / audit_log rows. New ones get added
// here so the switch/lookup code stays grep-friendly.
const (
	entityTestCase           = "test_case"
	entityTestStep           = "test_step"
	entityTestStepDelete     = "test_step_delete"
	entityTestStepAdd        = "test_step_add"
	entityTestStepOrder      = "test_step_order"
	entityMembershipAdd      = "test_membership_add"
	entityMembershipRemove   = "test_membership_remove"
	entityContainerAdd       = "test_container_add"
	entityContainerEdit      = "container_edit"
	entityContainerDelete    = "container_delete"
	entityContainerEnv       = "container_env"
	entityPreconditionSet    = "precondition_set"
	entityPreconditionEdit   = "precondition_edit"
	entityPreconditionAdd    = "precondition_add"
	entityPreconditionDelete = "precondition_delete"
	entityCustomField        = "custom_field"
	entityFolderCreate       = "folder_create"
	entityFolderRename       = "folder_rename"
	entityFolderDelete       = "folder_delete"
	entityTestCreate         = "test_create"
	entityTestReview         = "test_review"
	entityIssueComment       = "issue_comment"
	entityTestRun            = "test_run"
	entityTestRunDefect      = "test_run_defect"
	entityTestRunComment     = "test_run_comment"
	entityRequirementSet     = "requirement_set"
	entityRequirementEdit    = "requirement_edit"
	entityRequirementDelete  = "requirement_delete"
	entityRequirementCreate  = "requirement_create"
	entityBugCreate          = "bug_create"
	entityReqReqLinkSet      = "req_req_link_set"
)

// preconditionFields whitelists which Precondition columns can be edited via
// EditPreconditionField (FR-13.5).
var preconditionFields = map[string]string{
	"summary":     "summary",
	"description": "description",
	"type":        "type",
	"condition":   "condition",
}

// stepEntityKey encodes a step's parent Test plus its Xray step ID into a
// single entity_key, since pending_change has just one key column.
// "QA-12:abc-uuid" — the first colon splits cleanly because Xray test
// keys never contain one.
const stepEntityKeySep = ":"

func stepEntityKey(testKey, xrayID string) string {
	return testKey + stepEntityKeySep + xrayID
}

func parseStepEntityKey(s string) (testKey, xrayID string, ok bool) {
	i := strings.Index(s, stepEntityKeySep)
	if i < 0 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

// upsertPendingChange records (or coalesces) a pending field change. If a row
// already exists for this (profile, entityType, entity, field) the AfterVal
// is updated; if the new value matches the existing BeforeVal (i.e. the user
// reverted to the original), the row is deleted.
func upsertPendingChange(tx *sql.Tx, profileID, entityType, entityKey, field, currentVal, newValue, baseVersion string) error {
	var existingBefore string
	err := tx.QueryRow(
		`SELECT before_val FROM pending_change
		 WHERE profile_id = ? AND entity_type = ? AND entity_key = ? AND field = ?`,
		profileID, entityType, entityKey, field,
	).Scan(&existingBefore)

	now := time.Now().UTC().Format(time.RFC3339)

	if errors.Is(err, sql.ErrNoRows) {
		_, ierr := tx.Exec(
			`INSERT INTO pending_change
			   (profile_id, entity_type, entity_key, field, before_val, after_val, base_version, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			profileID, entityType, entityKey, field, currentVal, newValue, baseVersion, now,
		)
		if ierr != nil {
			return fmt.Errorf("insert pending change: %w", ierr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("read existing pending: %w", err)
	}

	if newValue == existingBefore {
		// Reverted to original — drop the pending change.
		if _, derr := tx.Exec(
			`DELETE FROM pending_change
			 WHERE profile_id = ? AND entity_type = ? AND entity_key = ? AND field = ?`,
			profileID, entityType, entityKey, field,
		); derr != nil {
			return fmt.Errorf("delete pending: %w", derr)
		}
		return nil
	}

	if _, uerr := tx.Exec(
		`UPDATE pending_change SET after_val = ?, created_at = ?
		 WHERE profile_id = ? AND entity_type = ? AND entity_key = ? AND field = ?`,
		newValue, now, profileID, entityType, entityKey, field,
	); uerr != nil {
		return fmt.Errorf("update pending: %w", uerr)
	}
	return nil
}

// putPendingChange records (or coalesces) a pending field change
// unconditionally — unlike upsertPendingChange, it never compares newValue
// against the row's before_val to detect a "revert" and delete the row.
// upsertPendingChange's revert check can only see the before_val captured
// when the row was first created, which goes stale once a resync moves the
// field's synced base out from under it; a coincidental match against that
// stale before_val would make it misdetect a genuine staged edit as a revert
// and silently drop the pending row while the staging column still holds the
// edit (see stageRunDefects / SetTestRunComment, which already decide
// revert-vs-edit against a freshly-read base and only reach here on the
// "genuine edit" branch — the write path must not second-guess that).
func putPendingChange(tx *sql.Tx, profileID, entityType, entityKey, field, currentVal, newValue, baseVersion string) error {
	var existingBefore string
	err := tx.QueryRow(
		`SELECT before_val FROM pending_change
		 WHERE profile_id = ? AND entity_type = ? AND entity_key = ? AND field = ?`,
		profileID, entityType, entityKey, field,
	).Scan(&existingBefore)

	now := time.Now().UTC().Format(time.RFC3339)

	if errors.Is(err, sql.ErrNoRows) {
		_, ierr := tx.Exec(
			`INSERT INTO pending_change
			   (profile_id, entity_type, entity_key, field, before_val, after_val, base_version, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			profileID, entityType, entityKey, field, currentVal, newValue, baseVersion, now,
		)
		if ierr != nil {
			return fmt.Errorf("insert pending change: %w", ierr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("read existing pending: %w", err)
	}

	if _, uerr := tx.Exec(
		`UPDATE pending_change SET after_val = ?, created_at = ?
		 WHERE profile_id = ? AND entity_type = ? AND entity_key = ? AND field = ?`,
		newValue, now, profileID, entityType, entityKey, field,
	); uerr != nil {
		return fmt.Errorf("update pending: %w", uerr)
	}
	return nil
}

// dropPendingChange removes a pending_change row outright. Used when a caller
// has already determined — by comparing its candidate new value against a
// freshly-read base, not against pending_change.before_val — that a local
// edit exactly reverts to that base (stageRunDefects / SetTestRunComment).
// upsertPendingChange's own revert detection compares against the frozen
// before_val from when the row was first created, which cannot see a base
// that moved since (e.g. a re-sync); calling this directly instead keeps the
// entity_key's test_container_test staging column and the pending_change
// table in the same state regardless of that drift.
func dropPendingChange(tx *sql.Tx, profileID, entityType, entityKey, field string) error {
	if _, err := tx.Exec(
		`DELETE FROM pending_change
		 WHERE profile_id = ? AND entity_type = ? AND entity_key = ? AND field = ?`,
		profileID, entityType, entityKey, field,
	); err != nil {
		return fmt.Errorf("drop pending change: %w", err)
	}
	return nil
}

// writeAudit appends one row to audit_log. Called from EditTestField,
// EditTestStepField, DiscardPendingChange, TransitionTest, and the
// commit / conflict paths.
func writeAudit(tx *sql.Tx, profileID, entityType, entityKey, action, field, beforeVal, afterVal, note string) error {
	if _, err := tx.Exec(
		`INSERT INTO audit_log
		   (profile_id, occurred_at, actor, entity_type, entity_key, action, field, before_val, after_val, note)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		profileID, time.Now().UTC().Format(time.RFC3339),
		currentActor(), entityType, entityKey, action, field, beforeVal, afterVal, note,
	); err != nil {
		return fmt.Errorf("audit log: %w", err)
	}
	return nil
}

// currentActor returns the OS username for the audit trail, falling back to
// "user" if it cannot be resolved.
func currentActor() string {
	u, err := user.Current()
	if err != nil || u == nil || u.Username == "" {
		return "user"
	}
	return u.Username
}

// scanner abstracts *sql.Row and *sql.Rows so scanTest serves Get and List.
type scanner interface {
	Scan(dest ...any) error
}

func scanTest(s scanner) (TestCase, error) {
	var (
		t           TestCase
		labels      string
		components  string
		fixVersions string
	)
	if err := s.Scan(
		&t.Key, &t.ID, &t.Summary, &t.Description,
		&t.Status, &t.Priority, &labels, &components, &t.Updated, &t.FolderID, &t.ExecType,
		&fixVersions, &t.CucumberScenario, &t.CucumberType, &t.GenericDefinition,
	); err != nil {
		return TestCase{}, err
	}
	if labels != "" {
		t.Labels = strings.Fields(labels)
	}
	t.Components = decodeComponents(components)
	t.FixVersions = decodeFixVersions(fixVersions)
	return t, nil
}

// componentSep separates component names in the stored, newline-bounded
// components string. A newline can't appear in a Jira component name, and
// bounding the whole value with separators lets a `components LIKE
// '%\nName\n%'` filter match one component exactly without a multi-word name
// like "User Management" colliding with "User".
const componentSep = "\n"

// encodeComponents joins component names into the bounded storage form, or ""
// for none. Empty / whitespace-only names are dropped.
func encodeComponents(names []string) string {
	clean := make([]string, 0, len(names))
	for _, n := range names {
		if s := strings.TrimSpace(n); s != "" {
			clean = append(clean, s)
		}
	}
	if len(clean) == 0 {
		return ""
	}
	return componentSep + strings.Join(clean, componentSep) + componentSep
}

// decodeComponents parses the stored components string back into a slice.
func decodeComponents(stored string) []string {
	out := []string{}
	for _, n := range strings.Split(stored, componentSep) {
		if s := strings.TrimSpace(n); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// componentFilterPattern builds the LIKE pattern that matches a single
// component name within the bounded storage form.
func componentFilterPattern(name string) string {
	return "%" + componentSep + name + componentSep + "%"
}

// TypeConversion reports the outcome of a ChangeTestType call.
type TypeConversion struct {
	OldType    string `json:"oldType"`
	NewType    string `json:"newType"`
	Prefilled  bool   `json:"prefilled"`  // target body was empty and got pre-filled
	CanPrefill bool   `json:"canPrefill"` // a source body exists to pre-fill from (offer opt-in)
}

// ChangeTestType sets a test's Xray Test Type and, when the destination body is
// empty, pre-fills it with a best-effort conversion of the source body. The
// source body is never modified, so switching back is lossless. When the
// destination already has content it is left untouched and CanPrefill signals
// that the caller may offer an explicit pre-fill.
func (r *Repository) ChangeTestType(profileID, testKey, newType string) (TypeConversion, error) {
	tc, err := r.GetTest(profileID, testKey)
	if err != nil {
		return TypeConversion{}, err
	}
	oldType := tc.ExecType
	res := TypeConversion{OldType: oldType, NewType: newType}
	if err := r.EditTestField(profileID, testKey, "exec_type", newType); err != nil {
		return res, err
	}
	if strings.EqualFold(oldType, newType) {
		return res, nil
	}

	targetEmpty, sourceHasBody := r.bodyState(profileID, tc, newType, oldType)
	res.CanPrefill = sourceHasBody
	if !targetEmpty || !sourceHasBody {
		return res, nil
	}
	if err := r.prefillBody(profileID, testKey, tc, oldType, newType); err != nil {
		return res, err
	}
	res.Prefilled = true
	return res, nil
}
