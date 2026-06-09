package testrepo

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// RunStatuses is the standard Xray Test Run result vocabulary offered when
// updating a Test's result within a Test Execution.
var RunStatuses = []string{"TODO", "EXECUTING", "PASS", "FAIL", "ABORTED", "BLOCKED"}

// SetTestRunStatus updates a Test's run result inside a Test Execution and
// queues it for commit to Xray. The Test must already be a member of the
// execution; setting the status it already has is a no-op. The pending change is
// keyed by "<execKey>:<testKey>" so each (execution, test) pair coalesces on its
// own row.
func (r *Repository) SetTestRunStatus(profileID, execKey, testKey, status string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var current string
	err = tx.QueryRow(
		`SELECT run_status FROM test_container_test
		 WHERE profile_id = ? AND container_key = ? AND test_key = ?`,
		profileID, execKey, testKey,
	).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s is not in execution %s", testKey, execKey)
	}
	if err != nil {
		return fmt.Errorf("read run status: %w", err)
	}
	if current == status {
		return nil
	}

	if _, err := tx.Exec(
		`UPDATE test_container_test SET run_status = ?
		 WHERE profile_id = ? AND container_key = ? AND test_key = ?`,
		status, profileID, execKey, testKey,
	); err != nil {
		return fmt.Errorf("update run status: %w", err)
	}

	ek := runKey(execKey, testKey)
	if err := upsertPendingChange(
		tx, profileID, entityTestRun, ek, "run_status", current, status, "",
	); err != nil {
		return err
	}
	if err := writeAudit(
		tx, profileID, entityTestRun, ek, "run-status-local", "run_status", current, status, "",
	); err != nil {
		return err
	}
	return tx.Commit()
}

func runKey(execKey, testKey string) string { return execKey + ":" + testKey }

// splitRunKey parses a test_run entity_key "<execKey>:<testKey>". Issue keys
// never contain a colon, so the first one is the separator.
func splitRunKey(entityKey string) (execKey, testKey string, ok bool) {
	i := strings.Index(entityKey, ":")
	if i < 0 {
		return "", "", false
	}
	return entityKey[:i], entityKey[i+1:], true
}
