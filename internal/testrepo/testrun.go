package testrepo

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// TestRunRow holds per-execution run details for a single Test, as stored in
// the test_run table. Defects is a JSON array string (e.g. `["PROJ-1"]`).
// Comment is the Xray-synced run remark (empty when unset). CreatedAt and
// UpdatedAt are ISO-8601 strings from Xray (empty when unknown).
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
	Comment     string
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
			   executed_by, environment, defects, created_at, updated_at, comment)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			profileID, row.ExecKey, row.TestKey, row.RunStatus,
			row.StartedAt, row.FinishedAt, row.ExecutedBy, row.Environment, row.Defects,
			row.CreatedAt, row.UpdatedAt, row.Comment,
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

// encodeDefectSet serialises a defect-key set into its staged JSON array
// form, deduped and sorted for stable comparison (reuses uniqueSorted, so
// equality checks don't depend on caller order). Unlike encodeEnvironments,
// an empty set encodes as "[]" — not "" — so a deliberately staged empty
// defect set stays distinguishable from run_defects = "" (no local edit at
// all).
func encodeDefectSet(set []string) string {
	clean := uniqueSorted(set)
	b, err := json.Marshal(clean)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// AddTestRunDefect stages a bug key onto a Test's run-defect set for one Test
// Execution and queues it for commit to Xray. Adding a bug already present in
// the current effective set is a no-op. See stageRunDefects for the
// read-modify-write and revert semantics.
func (r *Repository) AddTestRunDefect(profileID, execKey, testKey, bugKey string) error {
	return r.stageRunDefects(profileID, execKey, testKey, func(set []string) []string {
		return append(append([]string{}, set...), bugKey)
	})
}

// RemoveTestRunDefect unstages a bug key from a Test's run-defect set for one
// Test Execution and queues the change for commit to Xray. Removing a bug not
// present in the current effective set is a no-op. See stageRunDefects for
// the read-modify-write and revert semantics.
func (r *Repository) RemoveTestRunDefect(profileID, execKey, testKey, bugKey string) error {
	return r.stageRunDefects(profileID, execKey, testKey, func(set []string) []string {
		out := make([]string, 0, len(set))
		for _, k := range set {
			if k != bugKey {
				out = append(out, k)
			}
		}
		return out
	})
}

// stageRunDefects is the shared read-modify-write behind AddTestRunDefect and
// RemoveTestRunDefect. The Test must already be a member of the execution
// (ErrNoRows on the membership read becomes a "not in execution" error, as in
// SetTestRunStatus).
//
// The current effective set is the staged run_defects from an existing
// test_run_defect pending change if one exists, else the synced set from
// test_run.defects — this is deliberately keyed off pending-change presence
// (queried here), not off run_defects != "", mirroring how
// GetExecutionMembersWithRuns reads it back. mutate receives that set and
// returns the desired new set; the result is deduped/sorted and compared
// against the current effective set first (no-op if unchanged), then against
// the synced base:
//
//   - new set == synced base: the local edit reverts. run_defects resets to
//     "" and the pending row is dropped outright (dropPendingChange, not
//     upsertPendingChange's own before_val comparison — see its doc comment).
//   - otherwise: run_defects is set to the new set's JSON, which is "[]" (not
//     "") when the new set is empty, so removing the last staged defect while
//     the synced set is non-empty stays staged rather than reading back as
//     "no local edit". The pending change is recorded via putPendingChange,
//     not upsertPendingChange — upsertPendingChange's own before_val
//     comparison is frozen from when the row was first created and can't see
//     a synced base that moved since, so a genuine edit that coincidentally
//     matches that stale before_val must not be second-guessed into a revert.
func (r *Repository) stageRunDefects(profileID, execKey, testKey string, mutate func([]string) []string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var member string
	err = tx.QueryRow(
		`SELECT run_defects FROM test_container_test
		 WHERE profile_id = ? AND container_key = ? AND test_key = ?`,
		profileID, execKey, testKey,
	).Scan(&member)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s is not in execution %s", testKey, execKey)
	}
	if err != nil {
		return fmt.Errorf("read run defects: %w", err)
	}

	var syncedRaw string
	// No test_run row yet (never synced) is fine — treat the synced base as empty.
	_ = tx.QueryRow(
		`SELECT defects FROM test_run WHERE profile_id = ? AND exec_key = ? AND test_key = ?`,
		profileID, execKey, testKey,
	).Scan(&syncedRaw)
	syncedJSON := encodeDefectSet(decodeFixVersions(syncedRaw))

	ek := runKey(execKey, testKey)
	var stagedJSON string
	pErr := tx.QueryRow(
		`SELECT after_val FROM pending_change
		 WHERE profile_id = ? AND entity_type = ? AND entity_key = ? AND field = 'run_defects'`,
		profileID, entityTestRunDefect, ek,
	).Scan(&stagedJSON)
	hasPending := pErr == nil
	if pErr != nil && !errors.Is(pErr, sql.ErrNoRows) {
		return fmt.Errorf("read pending run defects: %w", pErr)
	}

	currentJSON := syncedJSON
	if hasPending {
		currentJSON = encodeDefectSet(decodeFixVersions(stagedJSON))
	}

	nextJSON := encodeDefectSet(mutate(decodeFixVersions(currentJSON)))
	if nextJSON == currentJSON {
		return nil
	}

	if nextJSON == syncedJSON {
		if _, err := tx.Exec(
			`UPDATE test_container_test SET run_defects = ''
			 WHERE profile_id = ? AND container_key = ? AND test_key = ?`,
			profileID, execKey, testKey,
		); err != nil {
			return fmt.Errorf("update run defects: %w", err)
		}
		if err := dropPendingChange(tx, profileID, entityTestRunDefect, ek, "run_defects"); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(
			`UPDATE test_container_test SET run_defects = ?
			 WHERE profile_id = ? AND container_key = ? AND test_key = ?`,
			nextJSON, profileID, execKey, testKey,
		); err != nil {
			return fmt.Errorf("update run defects: %w", err)
		}
		if err := putPendingChange(
			tx, profileID, entityTestRunDefect, ek, "run_defects", currentJSON, nextJSON, "",
		); err != nil {
			return err
		}
	}
	if err := writeAudit(
		tx, profileID, entityTestRunDefect, ek, "run-defect-local", "run_defects", currentJSON, nextJSON, "",
	); err != nil {
		return err
	}
	return tx.Commit()
}

// SetTestRunComment stages a run remark for a Test within a Test Execution
// and queues it for commit to Xray. The Test must already be a member of the
// execution. Setting the comment to what is already effective (staged, or
// synced when nothing is staged) is a no-op.
//
// The current effective comment, like the defect set above, is read via
// pending-change presence rather than the run_comment column alone: unlike
// the defect JSON, an empty comment has no separate "staged empty" encoding
// (run_comment = "" both when nothing is staged and when a clear IS staged),
// so the pending_change row's existence is the only reliable signal — this
// matches how GetExecutionMembersWithRuns reads it back.
//
// Setting the comment to Xray's synced value (test_run.comment) reverts the
// local edit: run_comment resets to "" and the pending row is dropped
// outright (dropPendingChange). Otherwise run_comment is set to the given
// text — including "" — and the pending change is recorded via
// putPendingChange (not upsertPendingChange — see stageRunDefects' doc
// comment for why), so clearing a comment that differs from a non-empty
// synced comment stays staged rather than silently reverting.
func (r *Repository) SetTestRunComment(profileID, execKey, testKey, comment string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var member string
	err = tx.QueryRow(
		`SELECT run_comment FROM test_container_test
		 WHERE profile_id = ? AND container_key = ? AND test_key = ?`,
		profileID, execKey, testKey,
	).Scan(&member)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s is not in execution %s", testKey, execKey)
	}
	if err != nil {
		return fmt.Errorf("read run comment: %w", err)
	}

	var syncedComment string
	// No test_run row yet (never synced) is fine — treat the synced base as empty.
	_ = tx.QueryRow(
		`SELECT comment FROM test_run WHERE profile_id = ? AND exec_key = ? AND test_key = ?`,
		profileID, execKey, testKey,
	).Scan(&syncedComment)

	ek := runKey(execKey, testKey)
	var stagedComment string
	pErr := tx.QueryRow(
		`SELECT after_val FROM pending_change
		 WHERE profile_id = ? AND entity_type = ? AND entity_key = ? AND field = 'run_comment'`,
		profileID, entityTestRunComment, ek,
	).Scan(&stagedComment)
	hasPending := pErr == nil
	if pErr != nil && !errors.Is(pErr, sql.ErrNoRows) {
		return fmt.Errorf("read pending run comment: %w", pErr)
	}

	current := syncedComment
	if hasPending {
		current = stagedComment
	}
	if comment == current {
		return nil
	}

	if comment == syncedComment {
		if _, err := tx.Exec(
			`UPDATE test_container_test SET run_comment = ''
			 WHERE profile_id = ? AND container_key = ? AND test_key = ?`,
			profileID, execKey, testKey,
		); err != nil {
			return fmt.Errorf("update run comment: %w", err)
		}
		if err := dropPendingChange(tx, profileID, entityTestRunComment, ek, "run_comment"); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(
			`UPDATE test_container_test SET run_comment = ?
			 WHERE profile_id = ? AND container_key = ? AND test_key = ?`,
			comment, profileID, execKey, testKey,
		); err != nil {
			return fmt.Errorf("update run comment: %w", err)
		}
		if err := putPendingChange(
			tx, profileID, entityTestRunComment, ek, "run_comment", current, comment, "",
		); err != nil {
			return err
		}
	}
	if err := writeAudit(
		tx, profileID, entityTestRunComment, ek, "run-comment-local", "run_comment", current, comment, "",
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
