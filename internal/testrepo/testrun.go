package testrepo

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// TestRunRow holds per-execution run details for a single Test, as stored in
// the test_run table. Defects is a JSON array string (e.g. `["PROJ-1"]`).
// CreatedAt and UpdatedAt are ISO-8601 strings from Xray (empty when unknown).
type TestRunRow struct {
	ExecKey     string
	TestKey     string
	RunStatus   string
	StartedAt   string
	FinishedAt  string
	ExecutedBy  string
	Environment string
	Defects     string
	CreatedAt   string
	UpdatedAt   string
}

// ReplaceRunsForExec atomically replaces all test_run rows for the given
// execution key with the supplied slice. Used by the sync engine to persist
// the run records fetched from Xray.
func (r *Repository) ReplaceRunsForExec(profileID, execKey string, runs []TestRunRow) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(
		`DELETE FROM test_run WHERE profile_id = ? AND exec_key = ?`,
		profileID, execKey,
	); err != nil {
		return fmt.Errorf("clear test runs: %w", err)
	}
	for _, row := range runs {
		if _, err := tx.Exec(
			`INSERT INTO test_run
			  (profile_id, exec_key, test_key, run_status, started_at, finished_at,
			   executed_by, environment, defects, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			profileID, row.ExecKey, row.TestKey, row.RunStatus,
			row.StartedAt, row.FinishedAt, row.ExecutedBy, row.Environment, row.Defects,
			row.CreatedAt, row.UpdatedAt,
		); err != nil {
			return fmt.Errorf("insert test run: %w", err)
		}
	}
	return tx.Commit()
}

// ReplaceExecPlans atomically replaces all exec_plan rows for the given
// execution key with the supplied plan keys. Used by the sync engine to
// persist the exec-to-plan associations fetched from Xray.
func (r *Repository) ReplaceExecPlans(profileID, execKey string, planKeys []string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(
		`DELETE FROM exec_plan WHERE profile_id = ? AND exec_key = ?`,
		profileID, execKey,
	); err != nil {
		return fmt.Errorf("clear exec plans: %w", err)
	}
	for _, pk := range planKeys {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO exec_plan (profile_id, exec_key, plan_key) VALUES (?, ?, ?)`,
			profileID, execKey, pk,
		); err != nil {
			return fmt.Errorf("insert exec plan: %w", err)
		}
	}
	return tx.Commit()
}

// RunsForTest returns all test_run rows for the given test key, ordered by
// updated_at descending (then finished_at, then exec_key) so the most recently
// updated run appears first.
func (r *Repository) RunsForTest(profileID, testKey string) ([]TestRunRow, error) {
	rows, err := r.db.Query(
		`SELECT exec_key, test_key, run_status, started_at, finished_at,
		        executed_by, environment, defects, created_at, updated_at
		 FROM test_run
		 WHERE profile_id = ? AND test_key = ?
		 ORDER BY updated_at DESC, finished_at DESC, exec_key`,
		profileID, testKey,
	)
	if err != nil {
		return nil, fmt.Errorf("query test runs: %w", err)
	}
	defer rows.Close()
	var out []TestRunRow
	for rows.Next() {
		var row TestRunRow
		if err := rows.Scan(
			&row.ExecKey, &row.TestKey, &row.RunStatus,
			&row.StartedAt, &row.FinishedAt, &row.ExecutedBy, &row.Environment, &row.Defects,
			&row.CreatedAt, &row.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ExecPlansForExec returns the plan keys linked to the given execution key.
func (r *Repository) ExecPlansForExec(profileID, execKey string) ([]string, error) {
	rows, err := r.db.Query(
		`SELECT plan_key FROM exec_plan WHERE profile_id = ? AND exec_key = ? ORDER BY plan_key`,
		profileID, execKey,
	)
	if err != nil {
		return nil, fmt.Errorf("query exec plans: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

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

// BulkSetTestRunStatus applies one run result to several Tests in an execution
// (FR-3 bulk), queuing each via SetTestRunStatus. A Test already at that status
// is a silent success; a Test not in the execution is reported as failed. Each
// runs in its own transaction so one failure doesn't block the rest.
func (r *Repository) BulkSetTestRunStatus(profileID, execKey string, testKeys []string, status string) BulkEditResult {
	result := BulkEditResult{Succeeded: []string{}, Failed: []BulkFailure{}}
	for _, key := range testKeys {
		if err := r.SetTestRunStatus(profileID, execKey, key, status); err != nil {
			result.Failed = append(result.Failed, BulkFailure{TestKey: key, Error: err.Error()})
			continue
		}
		result.Succeeded = append(result.Succeeded, key)
	}
	return result
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
