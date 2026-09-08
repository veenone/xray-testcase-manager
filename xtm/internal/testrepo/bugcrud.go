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
	// ExecKey is the Test Execution the bug was raised from, or "" when it was
	// raised from a Test outside any execution. Kiwi hyperlinks attach to an
	// execution rather than a test case, so the commit path needs it.
	ExecKey string `json:"execKey,omitempty"`
	// CreatedKey is set only by MarkBugCreated, after a remote create
	// succeeded but the link back failed. Its presence tells a retry the issue
	// already exists, so it links instead of creating a duplicate.
	CreatedKey string `json:"createdKey,omitempty"`
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
		Priority: d.Priority, Labels: d.Labels, TestKey: testKey, ExecKey: execKey, Fields: d.Fields,
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

// MarkBugCreated records the real issue key on a queued bug_create whose
// remote create succeeded but whose link back to the Test failed.
//
// The pending row deliberately survives that failure so the user can retry
// (the spec's partial-failure rule: never delete an issue that already
// exists). A bare retry would call CreateBug again and file a duplicate, so
// the key is written into the payload and the commit path skips the create
// when it is present. Every other field is preserved: the retry still needs
// them to build the link. The read-modify-write must be atomic in a
// transaction, or a concurrent commit could interleave and drop the key.
func (r *Repository) MarkBugCreated(profileID string, changeID int64, realKey string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var afterVal string
	err = tx.QueryRow(
		`SELECT after_val FROM pending_change WHERE id = ? AND profile_id = ?`,
		changeID, profileID).Scan(&afterVal)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("pending change %d not found", changeID)
	}
	if err != nil {
		return fmt.Errorf("read pending change: %w", err)
	}

	var p bugCreatePayload
	if err := json.Unmarshal([]byte(afterVal), &p); err != nil {
		return fmt.Errorf("malformed bug payload: %w", err)
	}
	p.CreatedKey = realKey
	next, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("encode bug payload: %w", err)
	}
	if _, err := tx.Exec(
		`UPDATE pending_change SET after_val = ? WHERE id = ? AND profile_id = ?`,
		string(next), changeID, profileID); err != nil {
		return fmt.Errorf("record created bug key: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit mark bug created: %w", err)
	}
	return nil
}
