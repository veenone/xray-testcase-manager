package testrepo

import (
	"encoding/json"
	"fmt"
)

// StageFailure records one best-effort sync stage that errored without
// aborting the whole run.
type StageFailure struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

// SyncLogEntry records the outcome of one sync run (FR-1.7).
type SyncLogEntry struct {
	ID         int64  `json:"id"`
	StartedAt  string `json:"startedAt"`
	FinishedAt string `json:"finishedAt"`
	// Outcome is "success", "partial" or "error". "partial" means the run
	// finished and its data is usable, but at least one stage did not
	// complete — see StageFailures.
	Outcome       string         `json:"outcome"`
	Fetched       int            `json:"fetched"`
	Error         string         `json:"error"`
	StageFailures []StageFailure `json:"stageFailures"`
}

// RecordSyncLog appends a sync run's outcome to the history (FR-1.7).
func (r *Repository) RecordSyncLog(profileID, startedAt, finishedAt, outcome string, fetched int, errMsg string, stageFailures []StageFailure) error {
	encoded := ""
	if len(stageFailures) > 0 {
		b, err := json.Marshal(stageFailures)
		if err != nil {
			return fmt.Errorf("encode stage failures: %w", err)
		}
		encoded = string(b)
	}
	if _, err := r.db.Exec(
		`INSERT INTO sync_log (profile_id, started_at, finished_at, outcome, fetched, error, stage_failures)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		profileID, startedAt, finishedAt, outcome, fetched, errMsg, encoded,
	); err != nil {
		return fmt.Errorf("record sync log: %w", err)
	}
	return nil
}

// ListSyncLog returns a profile's most recent sync runs, newest first. A limit
// of zero or less, or above 200, defaults to 50.
func (r *Repository) ListSyncLog(profileID string, limit int) ([]SyncLogEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.Query(
		`SELECT id, started_at, finished_at, outcome, fetched, error, stage_failures
		 FROM sync_log WHERE profile_id = ?
		 ORDER BY started_at DESC, id DESC LIMIT ?`,
		profileID, limit)
	if err != nil {
		return nil, fmt.Errorf("list sync log: %w", err)
	}
	defer rows.Close()

	out := []SyncLogEntry{}
	for rows.Next() {
		var e SyncLogEntry
		var encoded string
		if err := rows.Scan(&e.ID, &e.StartedAt, &e.FinishedAt, &e.Outcome, &e.Fetched, &e.Error, &encoded); err != nil {
			return nil, err
		}
		// Rows written before v49 carry an empty column, not "[]".
		if encoded != "" {
			if err := json.Unmarshal([]byte(encoded), &e.StageFailures); err != nil {
				return nil, fmt.Errorf("decode stage failures: %w", err)
			}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
