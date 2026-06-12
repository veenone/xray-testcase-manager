package testrepo

import (
	"fmt"
	"strings"
)

// ConflictDecision is the user's per-field resolution of a commit conflict
// (FR-1.4): keep "mine" (override the remote on the next commit) or "theirs"
// (discard my edit and take the remote value).
type ConflictDecision struct {
	PendingID   int64  `json:"pendingId"`
	EntityType  string `json:"entityType"`
	EntityKey   string `json:"entityKey"`
	Field       string `json:"field"`
	Choice      string `json:"choice"`      // "mine" | "theirs"
	RemoteValue string `json:"remoteValue"` // value to keep when choice == "theirs"
}

// ResolveConflictMerge applies per-field conflict decisions for a Test and
// re-bases its remaining pending changes onto remoteVersion so the next commit
// succeeds:
//   - "theirs": set the local cache to the remote value and drop the pending edit.
//   - "mine":   keep the pending edit (it overrides the remote on commit).
//
// Clean (non-conflicting) edits that were held back with the Test are re-based
// too, so they commit alongside the kept-mine edits.
func (r *Repository) ResolveConflictMerge(profileID, testKey, remoteVersion string, decisions []ConflictDecision) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, d := range decisions {
		if d.Choice != "theirs" {
			continue
		}
		switch d.EntityType {
		case entityTestCase:
			col, ok := columnForField(d.Field)
			if !ok {
				continue
			}
			if _, err := tx.Exec(
				fmt.Sprintf(`UPDATE test_case SET %s = ? WHERE profile_id = ? AND jira_key = ?`, col),
				d.RemoteValue, profileID, testKey,
			); err != nil {
				return fmt.Errorf("revert %s to remote: %w", d.Field, err)
			}
		case entityTestStep:
			col, ok := stepFields[d.Field]
			if !ok {
				continue
			}
			xrayID := strings.TrimPrefix(d.EntityKey, testKey+":")
			if _, err := tx.Exec(
				fmt.Sprintf(`UPDATE test_step SET %s = ? WHERE profile_id = ? AND test_key = ? AND xray_id = ?`, col),
				d.RemoteValue, profileID, testKey, xrayID,
			); err != nil {
				return fmt.Errorf("revert step %s to remote: %w", d.Field, err)
			}
		}
		if _, err := tx.Exec(
			`DELETE FROM pending_change WHERE profile_id = ? AND id = ?`,
			profileID, d.PendingID,
		); err != nil {
			return fmt.Errorf("drop resolved change: %w", err)
		}
	}

	// Re-base the remaining pending changes (kept-mine + clean) onto the remote
	// so the next commit's conflict pre-check passes.
	if _, err := tx.Exec(
		`UPDATE pending_change SET base_version = ?
		 WHERE profile_id = ? AND (entity_key = ? OR entity_key LIKE ?)`,
		remoteVersion, profileID, testKey, testKey+":%",
	); err != nil {
		return fmt.Errorf("rebase after resolve: %w", err)
	}
	return tx.Commit()
}
