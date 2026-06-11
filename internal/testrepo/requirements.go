package testrepo

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// reqLinkSnap is one Test->Requirement link in a requirement_set before
// snapshot. The link_id is kept so a removed link can be deleted in Jira (and
// restored on discard).
type reqLinkSnap struct {
	Key    string `json:"key"`
	LinkID string `json:"linkId"`
}

// Requirement is a cached requirement issue linked to Tests for coverage. It may
// live in a different project than the profile's Test project.
type Requirement struct {
	Key        string `json:"key"`
	ProjectKey string `json:"projectKey"`
	IssueType  string `json:"issueType"`
	Summary    string `json:"summary"`
	Status     string `json:"status"`
	Updated    string `json:"updated"`
}

// RequirementLink is one Test <-> Requirement coverage link.
type RequirementLink struct {
	TestKey        string
	RequirementKey string
	LinkID         string
}

// RequirementCoverage is a Requirement plus how its covering Tests are doing,
// for the management/coverage view.
type RequirementCoverage struct {
	Key        string `json:"key"`
	ProjectKey string `json:"projectKey"`
	IssueType  string `json:"issueType"`
	Summary    string `json:"summary"`
	Status     string `json:"status"`
	TestCount  int    `json:"testCount"`
	Coverage   string `json:"coverage"` // PASSED | FAILED | NOTRUN | UNCOVERED
}

// RequirementTest is one Test covering a Requirement, with its consolidated run
// status across executions.
type RequirementTest struct {
	Key       string `json:"key"`
	Summary   string `json:"summary"`
	Status    string `json:"status"`
	RunStatus string `json:"runStatus"`
}

// RequirementSource configures where to look for requirements to browse, beyond
// those already linked to synced Tests.
type RequirementSource struct {
	ProjectKey string `json:"projectKey"`
	IssueTypes string `json:"issueTypes"`
	ScopeJQL   string `json:"scopeJql"`
}

// Coverage status labels.
const (
	CoverageUncovered = "UNCOVERED"
	CoverageNotRun    = "NOTRUN"
	CoveragePassed    = "PASSED"
	CoverageFailed    = "FAILED"
)

// --- Sync upsert (full replace, mirroring container links) ---

// ReplaceAllRequirements rewrites the cached requirement set for a profile.
func (r *Repository) ReplaceAllRequirements(profileID string, reqs []Requirement) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM requirement WHERE profile_id = ?`, profileID); err != nil {
		return fmt.Errorf("clear requirements: %w", err)
	}
	for _, rq := range reqs {
		if _, err := tx.Exec(
			`INSERT OR REPLACE INTO requirement
			   (profile_id, jira_key, project_key, issue_type, summary, status, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			profileID, rq.Key, rq.ProjectKey, rq.IssueType, rq.Summary, rq.Status, rq.Updated,
		); err != nil {
			return fmt.Errorf("insert requirement %s: %w", rq.Key, err)
		}
	}
	return tx.Commit()
}

// ReplaceAllRequirementLinks rewrites the cached Test <-> Requirement links.
func (r *Repository) ReplaceAllRequirementLinks(profileID string, links []RequirementLink) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM test_requirement WHERE profile_id = ?`, profileID); err != nil {
		return fmt.Errorf("clear requirement links: %w", err)
	}
	for _, l := range links {
		if _, err := tx.Exec(
			`INSERT OR REPLACE INTO test_requirement
			   (profile_id, test_key, requirement_key, link_id) VALUES (?, ?, ?, ?)`,
			profileID, l.TestKey, l.RequirementKey, l.LinkID,
		); err != nil {
			return fmt.Errorf("insert requirement link %s->%s: %w", l.TestKey, l.RequirementKey, err)
		}
	}
	return tx.Commit()
}

// --- Linking (Phase 2) ---

// SetTestRequirements replaces the set of requirements a Test covers and queues
// the change for commit (creating/removing Jira issue links). Setting the same
// set is a no-op. The before snapshot keeps each prior link's id so a removed
// link can be deleted in Jira and restored on discard.
func (r *Repository) SetTestRequirements(profileID, testKey string, reqKeys []string) error {
	newSet := uniqueSorted(reqKeys)

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

	curRows, err := tx.Query(
		`SELECT requirement_key, link_id FROM test_requirement
		 WHERE profile_id = ? AND test_key = ? ORDER BY requirement_key`,
		profileID, testKey)
	if err != nil {
		return fmt.Errorf("read current links: %w", err)
	}
	cur := []reqLinkSnap{}
	curKeys := []string{}
	linkByKey := map[string]string{}
	for curRows.Next() {
		var k, id string
		if err := curRows.Scan(&k, &id); err != nil {
			_ = curRows.Close()
			return err
		}
		cur = append(cur, reqLinkSnap{Key: k, LinkID: id})
		curKeys = append(curKeys, k)
		linkByKey[k] = id
	}
	_ = curRows.Close()
	if err := curRows.Err(); err != nil {
		return err
	}
	if equalOrder(curKeys, newSet) {
		return nil
	}

	if _, err := tx.Exec(
		`DELETE FROM test_requirement WHERE profile_id = ? AND test_key = ?`,
		profileID, testKey,
	); err != nil {
		return fmt.Errorf("clear requirement links: %w", err)
	}
	for _, k := range newSet {
		if _, err := tx.Exec(
			`INSERT INTO test_requirement (profile_id, test_key, requirement_key, link_id)
			 VALUES (?, ?, ?, ?)`,
			profileID, testKey, k, linkByKey[k], // empty link_id for a new link
		); err != nil {
			return fmt.Errorf("insert requirement link: %w", err)
		}
	}

	beforeJSON, _ := json.Marshal(cur)
	afterJSON, _ := json.Marshal(newSet)
	if err := upsertPendingChange(
		tx, profileID, entityRequirementSet, testKey, "requirements",
		string(beforeJSON), string(afterJSON), baseVersion,
	); err != nil {
		return err
	}
	if err := writeAudit(
		tx, profileID, entityRequirementSet, testKey,
		"set-requirements-local", "requirements", string(beforeJSON), string(afterJSON), "",
	); err != nil {
		return err
	}
	return tx.Commit()
}

// --- Requirement sources (config) ---

// ListRequirementSources returns the configured requirement sources for a
// profile, ordered by project key.
func (r *Repository) ListRequirementSources(profileID string) ([]RequirementSource, error) {
	rows, err := r.db.Query(
		`SELECT project_key, issue_types, scope_jql FROM requirement_source
		 WHERE profile_id = ? ORDER BY project_key`, profileID)
	if err != nil {
		return nil, fmt.Errorf("list requirement sources: %w", err)
	}
	defer rows.Close()
	out := []RequirementSource{}
	for rows.Next() {
		var s RequirementSource
		if err := rows.Scan(&s.ProjectKey, &s.IssueTypes, &s.ScopeJQL); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SetRequirementSource adds or updates a requirement source.
func (r *Repository) SetRequirementSource(profileID, projectKey, issueTypes, scopeJQL string) error {
	if strings.TrimSpace(projectKey) == "" {
		return fmt.Errorf("a project key is required")
	}
	if _, err := r.db.Exec(
		`INSERT INTO requirement_source (profile_id, project_key, issue_types, scope_jql)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(profile_id, project_key) DO UPDATE SET
		   issue_types = excluded.issue_types, scope_jql = excluded.scope_jql`,
		profileID, strings.TrimSpace(projectKey), strings.TrimSpace(issueTypes), strings.TrimSpace(scopeJQL),
	); err != nil {
		return fmt.Errorf("save requirement source: %w", err)
	}
	return nil
}

// RemoveRequirementSource deletes a requirement source.
func (r *Repository) RemoveRequirementSource(profileID, projectKey string) error {
	if _, err := r.db.Exec(
		`DELETE FROM requirement_source WHERE profile_id = ? AND project_key = ?`,
		profileID, projectKey,
	); err != nil {
		return fmt.Errorf("remove requirement source: %w", err)
	}
	return nil
}

// --- Reads / coverage ---

// ListRequirementsWithCoverage returns every cached requirement with a derived
// coverage status computed from its covering Tests' run results.
func (r *Repository) ListRequirementsWithCoverage(profileID string) ([]RequirementCoverage, error) {
	reqRows, err := r.db.Query(
		`SELECT jira_key, project_key, issue_type, summary, status
		 FROM requirement WHERE profile_id = ?
		 ORDER BY project_key, jira_key`, profileID)
	if err != nil {
		return nil, fmt.Errorf("list requirements: %w", err)
	}
	defer reqRows.Close()

	type req struct{ key, project, itype, summary, status string }
	reqs := []req{}
	for reqRows.Next() {
		var rq req
		if err := reqRows.Scan(&rq.key, &rq.project, &rq.itype, &rq.summary, &rq.status); err != nil {
			return nil, err
		}
		reqs = append(reqs, rq)
	}
	if err := reqRows.Err(); err != nil {
		return nil, err
	}

	testsByReq, err := r.requirementTestKeys(profileID)
	if err != nil {
		return nil, err
	}
	runByTest, err := r.consolidatedRunByTest(profileID)
	if err != nil {
		return nil, err
	}

	out := make([]RequirementCoverage, 0, len(reqs))
	for _, rq := range reqs {
		tests := testsByReq[rq.key]
		statuses := make([]string, 0, len(tests))
		for _, tk := range tests {
			statuses = append(statuses, runByTest[tk])
		}
		out = append(out, RequirementCoverage{
			Key:        rq.key,
			ProjectKey: rq.project,
			IssueType:  rq.itype,
			Summary:    rq.summary,
			Status:     rq.status,
			TestCount:  len(tests),
			Coverage:   deriveCoverage(statuses, len(tests)),
		})
	}
	return out, nil
}

// ListTestsForRequirement returns the Tests covering a requirement, each with
// its consolidated run status.
func (r *Repository) ListTestsForRequirement(profileID, requirementKey string) ([]RequirementTest, error) {
	rows, err := r.db.Query(
		`SELECT t.jira_key, t.summary, t.status
		 FROM test_requirement l
		 JOIN test_case t ON t.profile_id = l.profile_id AND t.jira_key = l.test_key
		 WHERE l.profile_id = ? AND l.requirement_key = ?
		 ORDER BY t.jira_key`,
		profileID, requirementKey)
	if err != nil {
		return nil, fmt.Errorf("list requirement tests: %w", err)
	}
	defer rows.Close()

	runByTest, err := r.consolidatedRunByTest(profileID)
	if err != nil {
		return nil, err
	}
	out := []RequirementTest{}
	for rows.Next() {
		var rt RequirementTest
		if err := rows.Scan(&rt.Key, &rt.Summary, &rt.Status); err != nil {
			return nil, err
		}
		rt.RunStatus = runByTest[rt.Key]
		out = append(out, rt)
	}
	return out, rows.Err()
}

// GetTestRequirements returns the requirements a Test covers (for the detail
// panel).
func (r *Repository) GetTestRequirements(profileID, testKey string) ([]Requirement, error) {
	rows, err := r.db.Query(
		`SELECT rq.jira_key, rq.project_key, rq.issue_type, rq.summary, rq.status, rq.updated_at
		 FROM test_requirement l
		 JOIN requirement rq ON rq.profile_id = l.profile_id AND rq.jira_key = l.requirement_key
		 WHERE l.profile_id = ? AND l.test_key = ?
		 ORDER BY rq.jira_key`,
		profileID, testKey)
	if err != nil {
		return nil, fmt.Errorf("get test requirements: %w", err)
	}
	defer rows.Close()
	out := []Requirement{}
	for rows.Next() {
		var rq Requirement
		if err := rows.Scan(&rq.Key, &rq.ProjectKey, &rq.IssueType, &rq.Summary, &rq.Status, &rq.Updated); err != nil {
			return nil, err
		}
		out = append(out, rq)
	}
	return out, rows.Err()
}

// requirementTestKeys maps each requirement to the Test keys covering it.
func (r *Repository) requirementTestKeys(profileID string) (map[string][]string, error) {
	rows, err := r.db.Query(
		`SELECT requirement_key, test_key FROM test_requirement WHERE profile_id = ?`,
		profileID)
	if err != nil {
		return nil, fmt.Errorf("read requirement links: %w", err)
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var reqKey, testKey string
		if err := rows.Scan(&reqKey, &testKey); err != nil {
			return nil, err
		}
		out[reqKey] = append(out[reqKey], testKey)
	}
	return out, rows.Err()
}

// consolidatedRunByTest maps each Test to its run status consolidated (worst-
// wins) across every Test Execution it belongs to. Tests with no execution
// membership are absent (treated as "" / not run).
func (r *Repository) consolidatedRunByTest(profileID string) (map[string]string, error) {
	rows, err := r.db.Query(
		`SELECT l.test_key, l.run_status
		 FROM test_container_test l
		 JOIN test_container c ON c.profile_id = l.profile_id AND c.jira_key = l.container_key
		 WHERE l.profile_id = ? AND c.kind = 'testexec'`,
		profileID)
	if err != nil {
		return nil, fmt.Errorf("read execution runs: %w", err)
	}
	defer rows.Close()
	byTest := map[string][]string{}
	for rows.Next() {
		var testKey, runStatus string
		if err := rows.Scan(&testKey, &runStatus); err != nil {
			return nil, err
		}
		byTest[testKey] = append(byTest[testKey], runStatus)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(byTest))
	for k, v := range byTest {
		out[k] = consolidateRunStatus(v)
	}
	return out, nil
}

// deriveCoverage turns a requirement's covering Tests' run statuses into a
// coverage label: any failure fails it; all-passing passes it; otherwise it's
// covered but not (fully) run; no Tests means uncovered.
func deriveCoverage(testRunStatuses []string, testCount int) string {
	if testCount == 0 {
		return CoverageUncovered
	}
	allPass := true
	for _, s := range testRunStatuses {
		switch strings.ToUpper(s) {
		case "FAIL", "FAILED":
			return CoverageFailed
		case "PASS", "PASSED":
			// keep checking
		default:
			allPass = false
		}
	}
	if allPass {
		return CoveragePassed
	}
	return CoverageNotRun
}
