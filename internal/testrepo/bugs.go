package testrepo

import (
	"fmt"
	"strings"
)

// Bug is a cached defect issue (possibly in another project) linked to Tests.
type Bug struct {
	Key        string `json:"key"`
	ProjectKey string `json:"projectKey"`
	IssueType  string `json:"issueType"`
	Summary    string `json:"summary"`
	Status     string `json:"status"`
	Priority   string `json:"priority"`
	Updated    string `json:"updated"`
}

// BugLink is a Test <-> Bug link.
type BugLink struct {
	TestKey string
	BugKey  string
	LinkID  string
}

// BugWithTests is a bug plus the Test keys it affects, for the Bugs panel.
type BugWithTests struct {
	Key        string   `json:"key"`
	ProjectKey string   `json:"projectKey"`
	Summary    string   `json:"summary"`
	Status     string   `json:"status"`
	Priority   string   `json:"priority"`
	TestKeys   []string `json:"testKeys"`
}

// TestBug is a bug linked to one Test, for the test-detail section.
type TestBug struct {
	Key        string `json:"key"`
	ProjectKey string `json:"projectKey"`
	Summary    string `json:"summary"`
	Status     string `json:"status"`
	Priority   string `json:"priority"`
}

// BugTest is a Test affected by a bug, with its consolidated run status — for
// the bug detail pane.
type BugTest struct {
	Key       string `json:"key"`
	Summary   string `json:"summary"`
	Status    string `json:"status"`
	RunStatus string `json:"runStatus"`
}

// BugDraft is the payload for creating a new bug from a failed test.
type BugDraft struct {
	ProjectKey  string   `json:"projectKey"`
	IssueType   string   `json:"issueType"`
	Summary     string   `json:"summary"`
	Description string   `json:"description"`
	Priority    string   `json:"priority"`
	Labels      []string `json:"labels"`
}

// bugLinkSnap mirrors reqLinkSnap: a Test link snapshot for discard.
type bugLinkSnap struct {
	Key    string `json:"key"`
	LinkID string `json:"linkId"`
}

// ProfileBugIssueType returns the profile's configured defect issuetype,
// defaulting to "Bug". It reads the profiles table directly (same store) so the
// syncer can recognize the right defect type without depending on the profile
// manager.
func (r *Repository) ProfileBugIssueType(profileID string) string {
	var t string
	err := r.db.QueryRow(`SELECT bug_issue_type FROM profiles WHERE id = ?`, profileID).Scan(&t)
	if err != nil || strings.TrimSpace(t) == "" {
		return "Bug"
	}
	return t
}

// ReplaceAllBugs reconciles the cached bug issues for a profile (full replace on
// sync). Mirrors ReplaceAllRequirements.
func (r *Repository) ReplaceAllBugs(profileID string, bugs []Bug) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM bug WHERE profile_id = ?`, profileID); err != nil {
		return fmt.Errorf("clear bugs: %w", err)
	}
	for _, b := range bugs {
		if _, err := tx.Exec(
			`INSERT OR REPLACE INTO bug (profile_id, jira_key, project_key, issue_type, summary, status, priority, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			profileID, b.Key, b.ProjectKey, b.IssueType, b.Summary, b.Status, b.Priority, b.Updated,
		); err != nil {
			return fmt.Errorf("insert bug: %w", err)
		}
	}
	return tx.Commit()
}

// ReplaceAllBugLinks reconciles the Test<->Bug links for a profile (full replace
// on sync). Mirrors ReplaceAllRequirementLinks.
func (r *Repository) ReplaceAllBugLinks(profileID string, links []BugLink) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM test_bug WHERE profile_id = ?`, profileID); err != nil {
		return fmt.Errorf("clear bug links: %w", err)
	}
	for _, l := range links {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO test_bug (profile_id, test_key, bug_key, link_id)
			 VALUES (?, ?, ?, ?)`,
			profileID, l.TestKey, l.BugKey, l.LinkID,
		); err != nil {
			return fmt.Errorf("insert bug link: %w", err)
		}
	}
	return tx.Commit()
}

// GetTestBugs returns the bugs linked to a Test (for the detail section),
// ordered by key.
func (r *Repository) GetTestBugs(profileID, testKey string) ([]TestBug, error) {
	rows, err := r.db.Query(
		`SELECT b.jira_key, b.project_key, b.summary, b.status, b.priority
		 FROM test_bug l
		 JOIN bug b ON b.profile_id = l.profile_id AND b.jira_key = l.bug_key
		 WHERE l.profile_id = ? AND l.test_key = ?
		 ORDER BY b.jira_key`, profileID, testKey)
	if err != nil {
		return nil, fmt.Errorf("get test bugs: %w", err)
	}
	defer rows.Close()
	out := []TestBug{}
	for rows.Next() {
		var b TestBug
		if err := rows.Scan(&b.Key, &b.ProjectKey, &b.Summary, &b.Status, &b.Priority); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ListTestsForBug returns the Tests a bug affects, each with its consolidated
// run status — for the bug detail pane. Mirrors ListTestsForRequirement.
//
// LEFT JOINs both the local test_case cache and the external_test cache (a
// cross-project member of an execution lives only in the latter), COALESCEing
// summary/status so a foreign member shows by key/summary instead of being
// dropped by an INNER JOIN (#219).
func (r *Repository) ListTestsForBug(profileID, bugKey string) ([]BugTest, error) {
	rows, err := r.db.Query(
		`SELECT l.test_key,
		        COALESCE(t.summary, x.summary, '') AS summary,
		        COALESCE(t.status,  x.status,  '') AS status
		 FROM test_bug l
		 LEFT JOIN test_case     t ON t.profile_id = l.profile_id AND t.jira_key = l.test_key
		 LEFT JOIN external_test x ON x.profile_id = l.profile_id AND x.jira_key = l.test_key
		 WHERE l.profile_id = ? AND l.bug_key = ?
		 ORDER BY l.test_key`,
		profileID, bugKey)
	if err != nil {
		return nil, fmt.Errorf("list bug tests: %w", err)
	}
	defer rows.Close()

	runByTest, err := r.consolidatedRunByTest(profileID)
	if err != nil {
		return nil, err
	}
	out := []BugTest{}
	for rows.Next() {
		var bt BugTest
		if err := rows.Scan(&bt.Key, &bt.Summary, &bt.Status); err != nil {
			return nil, err
		}
		bt.RunStatus = runByTest[bt.Key]
		out = append(out, bt)
	}
	return out, rows.Err()
}

// ListBugsForContainer returns the distinct bugs reached through any member Test
// of a container (Test Execution / Set / Plan), ordered by key. It joins
// test_container_test -> test_bug -> bug, so a bug linked to a cross-project
// member of an execution is surfaced even though that member has no test_case
// row (the link/bug were harvested from the external member's issuelinks, #219).
func (r *Repository) ListBugsForContainer(profileID, containerKey string) ([]Bug, error) {
	rows, err := r.db.Query(
		`SELECT DISTINCT b.jira_key, b.project_key, b.issue_type, b.summary, b.status, b.priority, b.updated_at
		 FROM test_container_test m
		 JOIN test_bug l ON l.profile_id = m.profile_id AND l.test_key = m.test_key
		 JOIN bug      b ON b.profile_id = l.profile_id AND b.jira_key = l.bug_key
		 WHERE m.profile_id = ? AND m.container_key = ?
		 ORDER BY b.jira_key`,
		profileID, containerKey)
	if err != nil {
		return nil, fmt.Errorf("list bugs for container: %w", err)
	}
	defer rows.Close()
	out := []Bug{}
	for rows.Next() {
		var b Bug
		if err := rows.Scan(&b.Key, &b.ProjectKey, &b.IssueType, &b.Summary, &b.Status, &b.Priority, &b.Updated); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// UpsertBugs inserts-or-updates the given bugs WITHOUT wiping the existing cache.
// Unlike ReplaceAllBugs (wipe-and-replace, used by the normal syncBugs path), it
// is additive so the cross-project harvest in syncContainers can merge bugs
// reached through external member Tests alongside the bugs the normal path
// already wrote, without clobbering them (#219).
func (r *Repository) UpsertBugs(profileID string, bugs []Bug) error {
	if len(bugs) == 0 {
		return nil
	}
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, b := range bugs {
		if _, err := tx.Exec(
			`INSERT OR REPLACE INTO bug (profile_id, jira_key, project_key, issue_type, summary, status, priority, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			profileID, b.Key, b.ProjectKey, b.IssueType, b.Summary, b.Status, b.Priority, b.Updated,
		); err != nil {
			return fmt.Errorf("upsert bug: %w", err)
		}
	}
	return tx.Commit()
}

// UpsertBugLinks inserts the given Test<->Bug links WITHOUT wiping existing ones
// (additive counterpart to ReplaceAllBugLinks). Used by the cross-project harvest
// so external-member links are merged with the normal path's links (#219).
func (r *Repository) UpsertBugLinks(profileID string, links []BugLink) error {
	if len(links) == 0 {
		return nil
	}
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, l := range links {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO test_bug (profile_id, test_key, bug_key, link_id)
			 VALUES (?, ?, ?, ?)`,
			profileID, l.TestKey, l.BugKey, l.LinkID,
		); err != nil {
			return fmt.Errorf("upsert bug link: %w", err)
		}
	}
	return tx.Commit()
}

// ListBugsWithTests returns every cached bug with the Test keys it affects, for
// the Bugs panel. Ordered by project then key.
func (r *Repository) ListBugsWithTests(profileID string) ([]BugWithTests, error) {
	rows, err := r.db.Query(
		`SELECT jira_key, project_key, summary, status, priority
		 FROM bug WHERE profile_id = ? ORDER BY project_key, jira_key`, profileID)
	if err != nil {
		return nil, fmt.Errorf("list bugs: %w", err)
	}
	defer rows.Close()
	out := []BugWithTests{}
	idx := map[string]int{}
	for rows.Next() {
		var b BugWithTests
		if err := rows.Scan(&b.Key, &b.ProjectKey, &b.Summary, &b.Status, &b.Priority); err != nil {
			return nil, err
		}
		b.TestKeys = []string{}
		idx[b.Key] = len(out)
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	lrows, err := r.db.Query(
		`SELECT bug_key, test_key FROM test_bug WHERE profile_id = ? ORDER BY test_key`, profileID)
	if err != nil {
		return nil, fmt.Errorf("list bug links: %w", err)
	}
	defer lrows.Close()
	for lrows.Next() {
		var bugKey, testKey string
		if err := lrows.Scan(&bugKey, &testKey); err != nil {
			return nil, err
		}
		if i, ok := idx[bugKey]; ok {
			out[i].TestKeys = append(out[i].TestKeys, testKey)
		}
	}
	return out, lrows.Err()
}
