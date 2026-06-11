package testrepo

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// requirementEditFields maps an editable requirement field to its DB column.
// Status changes go through a workflow transition, not a field edit.
var requirementEditFields = map[string]string{
	"summary": "summary",
}

// requirementDeleteSnapshot captures a requirement plus its Test links so a
// discarded delete can restore it.
type requirementDeleteSnapshot struct {
	ProjectKey string        `json:"projectKey"`
	IssueType  string        `json:"issueType"`
	Summary    string        `json:"summary"`
	Status     string        `json:"status"`
	Updated    string        `json:"updated"`
	Links      []reqLinkSnap `json:"links"`
}

// EditRequirementField edits a requirement field (summary) and queues the
// change for commit (a Jira issue update). Editing a requirement in another
// project requires edit permission there — surfaced at commit time.
func (r *Repository) EditRequirementField(profileID, requirementKey, field, newValue string) error {
	col, ok := requirementEditFields[field]
	if !ok {
		return fmt.Errorf("requirement field %q is not editable", field)
	}

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var current string
	readSQL := fmt.Sprintf(`SELECT %s FROM requirement WHERE profile_id = ? AND jira_key = ?`, col)
	err = tx.QueryRow(readSQL, profileID, requirementKey).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("requirement %s not found", requirementKey)
	}
	if err != nil {
		return fmt.Errorf("read requirement value: %w", err)
	}
	if current == newValue {
		return nil
	}

	updateSQL := fmt.Sprintf(`UPDATE requirement SET %s = ? WHERE profile_id = ? AND jira_key = ?`, col)
	if _, err := tx.Exec(updateSQL, newValue, profileID, requirementKey); err != nil {
		return fmt.Errorf("update requirement: %w", err)
	}
	if err := upsertPendingChange(
		tx, profileID, entityRequirementEdit, requirementKey, field, current, newValue, "",
	); err != nil {
		return err
	}
	if err := writeAudit(
		tx, profileID, entityRequirementEdit, requirementKey,
		"edit-requirement-local", field, current, newValue, "",
	); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteRequirement removes a requirement and its Test coverage links and queues
// the deletion for commit. Deleting a requirement issue (often cross-project) is
// permission-sensitive — surfaced at commit time.
func (r *Repository) DeleteRequirement(profileID, key string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var snap requirementDeleteSnapshot
	err = tx.QueryRow(
		`SELECT project_key, issue_type, summary, status, updated_at
		 FROM requirement WHERE profile_id = ? AND jira_key = ?`,
		profileID, key,
	).Scan(&snap.ProjectKey, &snap.IssueType, &snap.Summary, &snap.Status, &snap.Updated)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("requirement %s not found", key)
	}
	if err != nil {
		return fmt.Errorf("read requirement: %w", err)
	}
	snap.Links, err = requirementLinkSnaps(tx, profileID, key)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(
		`DELETE FROM requirement WHERE profile_id = ? AND jira_key = ?`, profileID, key,
	); err != nil {
		return fmt.Errorf("delete requirement: %w", err)
	}
	if _, err := tx.Exec(
		`DELETE FROM test_requirement WHERE profile_id = ? AND requirement_key = ?`, profileID, key,
	); err != nil {
		return fmt.Errorf("delete requirement links: %w", err)
	}
	// Drop the deleted requirement from any pending coverage set so a committed
	// set never references a now-deleted key.
	if err := rewriteRequirementSets(tx, profileID, key, ""); err != nil {
		return err
	}
	// Drop any superseded edit row.
	if _, err := tx.Exec(
		`DELETE FROM pending_change WHERE profile_id = ? AND entity_type = ? AND entity_key = ?`,
		profileID, entityRequirementEdit, key,
	); err != nil {
		return fmt.Errorf("clear superseded requirement edits: %w", err)
	}

	encoded, _ := json.Marshal(snap)
	if err := upsertPendingChange(
		tx, profileID, entityRequirementDelete, key, "requirement", string(encoded), "1", "",
	); err != nil {
		return err
	}
	if err := writeAudit(
		tx, profileID, entityRequirementDelete, key, "delete-requirement-local",
		"requirement", string(encoded), "", "",
	); err != nil {
		return err
	}
	return tx.Commit()
}

// requirementLinkSnaps returns the Test links of a requirement as snapshots.
func requirementLinkSnaps(tx *sql.Tx, profileID, requirementKey string) ([]reqLinkSnap, error) {
	rows, err := tx.Query(
		`SELECT test_key, link_id FROM test_requirement
		 WHERE profile_id = ? AND requirement_key = ?`,
		profileID, requirementKey)
	if err != nil {
		return nil, fmt.Errorf("read requirement links: %w", err)
	}
	defer rows.Close()
	out := []reqLinkSnap{}
	for rows.Next() {
		var testKey, linkID string
		if err := rows.Scan(&testKey, &linkID); err != nil {
			return nil, err
		}
		out = append(out, reqLinkSnap{Key: testKey, LinkID: linkID})
	}
	return out, rows.Err()
}

// rewriteRequirementSets rewrites a requirement key in every pending
// requirement_set row (before snapshot objects + after key list). newKey == ""
// drops the key. A row whose before and after end up equal is removed.
func rewriteRequirementSets(tx *sql.Tx, profileID, oldKey, newKey string) error {
	rows, err := tx.Query(
		`SELECT id, before_val, after_val FROM pending_change
		 WHERE profile_id = ? AND entity_type = ?`,
		profileID, entityRequirementSet)
	if err != nil {
		return fmt.Errorf("read requirement sets: %w", err)
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
		before := substituteLinkSnaps(x.before, oldKey, newKey)
		after := substituteKey(decodeKeys(x.after), oldKey, newKey)
		// A rewrite that leaves the covered set unchanged is a no-op coverage
		// edit; drop it rather than leave a stale row in the pending list.
		if sameReqSet(before, after) {
			if _, err := tx.Exec(
				`DELETE FROM pending_change WHERE profile_id = ? AND id = ?`,
				profileID, x.id,
			); err != nil {
				return fmt.Errorf("drop no-op requirement set: %w", err)
			}
			continue
		}
		bj, _ := json.Marshal(before)
		aj, _ := json.Marshal(after)
		if _, err := tx.Exec(
			`UPDATE pending_change SET before_val = ?, after_val = ? WHERE profile_id = ? AND id = ?`,
			string(bj), string(aj), profileID, x.id,
		); err != nil {
			return fmt.Errorf("rewrite requirement set: %w", err)
		}
	}
	return nil
}

// sameReqSet reports whether a requirement_set's before snapshot (links) and
// after key list cover the same requirement keys, i.e. the edit is now a no-op.
func sameReqSet(before []reqLinkSnap, after []string) bool {
	if len(before) != len(after) {
		return false
	}
	keys := make(map[string]int, len(before))
	for _, s := range before {
		keys[s.Key]++
	}
	for _, k := range after {
		if keys[k] == 0 {
			return false
		}
		keys[k]--
	}
	return true
}

// substituteLinkSnaps rewrites or drops a requirement key inside a serialised
// []reqLinkSnap (the requirement_set before snapshot).
func substituteLinkSnaps(beforeJSON, oldKey, newKey string) []reqLinkSnap {
	var snaps []reqLinkSnap
	_ = json.Unmarshal([]byte(beforeJSON), &snaps)
	out := make([]reqLinkSnap, 0, len(snaps))
	for _, s := range snaps {
		if s.Key == oldKey {
			if newKey == "" {
				continue
			}
			s.Key = newKey
		}
		out = append(out, s)
	}
	return out
}
