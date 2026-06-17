package testrepo

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// preconditionDeleteSnapshot captures a Precondition plus the Tests it was
// linked to, so a discarded delete can restore it.
type preconditionDeleteSnapshot struct {
	Summary     string   `json:"summary"`
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Tests       []string `json:"tests"`
}

// PreconditionUsage is a Precondition plus how many Tests reference it — the
// row shape the management view lists (FR-13.4).
type PreconditionUsage struct {
	Key         string `json:"key"`
	Summary     string `json:"summary"`
	Type        string `json:"type"`
	Description string `json:"description"`
	TestCount   int    `json:"testCount"`
}

// PreconditionTest is one Test linked to a Precondition, with its summary and
// workflow status for display in the management view.
type PreconditionTest struct {
	Key     string `json:"key"`
	Summary string `json:"summary"`
	Status  string `json:"status"`
}

// ListPreconditionsWithUsage returns every cached Precondition for a profile
// with the count of Tests referencing it, ordered by key. Drives the
// dedicated Preconditions management view (FR-13.4).
func (r *Repository) ListPreconditionsWithUsage(profileID string) ([]PreconditionUsage, error) {
	rows, err := r.db.Query(
		`SELECT p.jira_key, p.summary, p.type, p.description,
		        COUNT(tp.test_key) AS test_count
		 FROM precondition p
		 LEFT JOIN test_precondition tp
		   ON tp.profile_id = p.profile_id AND tp.precondition_key = p.jira_key
		 WHERE p.profile_id = ?
		 GROUP BY p.jira_key, p.summary, p.type, p.description
		 ORDER BY `+keyNumericOrderExpr("p.jira_key")+` DESC, p.jira_key DESC`,
		profileID)
	if err != nil {
		return nil, fmt.Errorf("list preconditions with usage: %w", err)
	}
	defer rows.Close()

	out := []PreconditionUsage{}
	for rows.Next() {
		var u PreconditionUsage
		if err := rows.Scan(&u.Key, &u.Summary, &u.Type, &u.Description, &u.TestCount); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ListTestsForPrecondition returns the Tests linked to one Precondition, with
// each Test's summary and status, ordered by key (FR-13.4 / 13.6).
func (r *Repository) ListTestsForPrecondition(profileID, preconditionKey string) ([]PreconditionTest, error) {
	rows, err := r.db.Query(
		`SELECT tp.test_key, COALESCE(t.summary, ''), COALESCE(t.status, '')
		 FROM test_precondition tp
		 LEFT JOIN test_case t
		   ON t.profile_id = tp.profile_id AND t.jira_key = tp.test_key
		 WHERE tp.profile_id = ? AND tp.precondition_key = ?
		 ORDER BY tp.test_key`,
		profileID, preconditionKey)
	if err != nil {
		return nil, fmt.Errorf("list tests for precondition: %w", err)
	}
	defer rows.Close()

	out := []PreconditionTest{}
	for rows.Next() {
		var t PreconditionTest
		if err := rows.Scan(&t.Key, &t.Summary, &t.Status); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeletePrecondition removes a Precondition and its Test links and queues the
// deletion for commit (FR-13.4). Deleting a Precondition that was only created
// locally cancels the create instead of queuing a remote delete.
func (r *Repository) DeletePrecondition(profileID, key string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var snap preconditionDeleteSnapshot
	err = tx.QueryRow(
		`SELECT summary, type, description FROM precondition
		 WHERE profile_id = ? AND jira_key = ?`,
		profileID, key,
	).Scan(&snap.Summary, &snap.Type, &snap.Description)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("precondition %s not found", key)
	}
	if err != nil {
		return fmt.Errorf("read precondition: %w", err)
	}
	snap.Tests, err = preconditionLinkedTests(tx, profileID, key)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(
		`DELETE FROM precondition WHERE profile_id = ? AND jira_key = ?`, profileID, key,
	); err != nil {
		return fmt.Errorf("delete precondition: %w", err)
	}
	if _, err := tx.Exec(
		`DELETE FROM test_precondition WHERE profile_id = ? AND precondition_key = ?`, profileID, key,
	); err != nil {
		return fmt.Errorf("delete precondition links: %w", err)
	}
	// Drop this precondition from any pending association set so a committed
	// set never references a now-deleted key.
	if err := rewritePreconditionSets(tx, profileID, key, ""); err != nil {
		return err
	}

	// If this Precondition was only ever created locally, cancel the create and
	// any superseded edit rows instead of queuing a remote delete.
	var addID int64
	addErr := tx.QueryRow(
		`SELECT id FROM pending_change
		 WHERE profile_id = ? AND entity_type = ? AND entity_key = ?`,
		profileID, entityPreconditionAdd, key,
	).Scan(&addID)
	if addErr == nil {
		for _, et := range []string{entityPreconditionAdd, entityPreconditionEdit} {
			if _, err := tx.Exec(
				`DELETE FROM pending_change WHERE profile_id = ? AND entity_type = ? AND entity_key = ?`,
				profileID, et, key,
			); err != nil {
				return fmt.Errorf("cancel precondition create rows: %w", err)
			}
		}
		if err := writeAudit(
			tx, profileID, entityPreconditionAdd, key, "precondition-create-cancelled",
			"precondition", snap.Summary, "", "",
		); err != nil {
			return err
		}
		return tx.Commit()
	}
	if !errors.Is(addErr, sql.ErrNoRows) {
		return fmt.Errorf("probe pending precondition add: %w", addErr)
	}

	// Committed Precondition: drop any superseded edit rows, then queue the
	// delete with a snapshot for discard.
	if _, err := tx.Exec(
		`DELETE FROM pending_change WHERE profile_id = ? AND entity_type = ? AND entity_key = ?`,
		profileID, entityPreconditionEdit, key,
	); err != nil {
		return fmt.Errorf("clear superseded precondition edits: %w", err)
	}
	encoded, _ := json.Marshal(snap)
	if err := upsertPendingChange(
		tx, profileID, entityPreconditionDelete, key, "precondition", string(encoded), "1", "",
	); err != nil {
		return err
	}
	if err := writeAudit(
		tx, profileID, entityPreconditionDelete, key, "delete-precondition-local",
		"precondition", string(encoded), "", "",
	); err != nil {
		return err
	}
	return tx.Commit()
}

// preconditionLinkedTests returns the Test keys linked to a Precondition.
func preconditionLinkedTests(tx *sql.Tx, profileID, key string) ([]string, error) {
	rows, err := tx.Query(
		`SELECT test_key FROM test_precondition
		 WHERE profile_id = ? AND precondition_key = ?`,
		profileID, key,
	)
	if err != nil {
		return nil, fmt.Errorf("read precondition links: %w", err)
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
