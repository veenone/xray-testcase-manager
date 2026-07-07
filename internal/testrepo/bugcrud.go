package testrepo

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// bugCreatePayload is the after_val of a bug_create pending change: everything
// needed to POST the Bug issue and link it to the test on commit.
// Fields carries the extra, createmeta-driven field values (already Jira-shaped)
// that were collected from the Create Bug form at queue time.
type bugCreatePayload struct {
	ProjectKey  string         `json:"projectKey"`
	IssueType   string         `json:"issueType"`
	Summary     string         `json:"summary"`
	Description string         `json:"description"`
	Priority    string         `json:"priority"`
	Labels      []string       `json:"labels"`
	TestKey     string         `json:"testKey"`
	Fields      map[string]any `json:"fields,omitempty"`
}

// CreateBugForTest queues a brand-new local Bug (temp "NEW-BUG-N" key) linked to
// a failed Test, committed to Jira on the next sync (mirrors CreateTest). execKey
// is recorded only for the audit note. Returns the temp key.
func (r *Repository) CreateBugForTest(profileID, testKey, execKey string, d BugDraft) (string, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	tempKey, err := nextNewBugKey(tx, profileID)
	if err != nil {
		return "", err
	}
	issueType := d.IssueType
	if issueType == "" {
		issueType = "Bug"
	}
	if _, err := tx.Exec(
		`INSERT INTO bug (profile_id, jira_key, project_key, issue_type, summary, status, priority, updated_at)
		 VALUES (?, ?, ?, ?, ?, '(new)', ?, '')`,
		profileID, tempKey, d.ProjectKey, issueType, d.Summary, d.Priority,
	); err != nil {
		return "", fmt.Errorf("insert local bug: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO test_bug (profile_id, test_key, bug_key, link_id) VALUES (?, ?, ?, '')`,
		profileID, testKey, tempKey,
	); err != nil {
		return "", fmt.Errorf("insert local bug link: %w", err)
	}

	payload, _ := json.Marshal(bugCreatePayload{
		ProjectKey: d.ProjectKey, IssueType: issueType, Summary: d.Summary, Description: d.Description,
		Priority: d.Priority, Labels: d.Labels, TestKey: testKey, Fields: d.Fields,
	})
	if err := upsertPendingChange(
		tx, profileID, entityBugCreate, tempKey, "bug", "", string(payload), "",
	); err != nil {
		return "", err
	}
	if err := writeAudit(
		tx, profileID, entityBugCreate, tempKey, "create-bug-local", "bug", "", d.Summary, execKey,
	); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit create bug: %w", err)
	}
	return tempKey, nil
}

// RenameBug repoints a cached bug + its Test links from the temporary key to the
// real key Jira assigned at commit (mirrors RenameTest, scoped to bug tables).
func (r *Repository) RenameBug(profileID, oldKey, newKey string) error {
	if newKey == "" || newKey == oldKey {
		return nil
	}
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(
		`UPDATE bug SET jira_key = ? WHERE profile_id = ? AND jira_key = ?`,
		newKey, profileID, oldKey,
	); err != nil {
		return fmt.Errorf("rename bug: %w", err)
	}
	if _, err := tx.Exec(
		`UPDATE test_bug SET bug_key = ? WHERE profile_id = ? AND bug_key = ?`,
		newKey, profileID, oldKey,
	); err != nil {
		return fmt.Errorf("rename bug link: %w", err)
	}
	return tx.Commit()
}

// nextNewBugKey allocates an unused "NEW-BUG-N" placeholder (mirrors the temp-key
// probe loop in importcsv.go's reserveTempKey, namespaced for bugs).
func nextNewBugKey(tx *sql.Tx, profileID string) (string, error) {
	for n := 1; ; n++ {
		key := fmt.Sprintf("NEW-BUG-%d", n)
		var one int
		err := tx.QueryRow(
			`SELECT 1 FROM bug WHERE profile_id = ? AND jira_key = ?`, profileID, key,
		).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return key, nil
		}
		if err != nil {
			return "", fmt.Errorf("probe temp bug key: %w", err)
		}
	}
}
