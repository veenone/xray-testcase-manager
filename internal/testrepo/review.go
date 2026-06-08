package testrepo

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Review is a Test's review state (verdict + who + note + when). An empty
// Verdict means the Test has not been reviewed.
type Review struct {
	Verdict    string `json:"verdict"` // "approved" | "rejected" | "pending" | ""
	Reviewer   string `json:"reviewer"`
	Note       string `json:"note"`
	ReviewedAt string `json:"reviewedAt"`
}

var validVerdicts = map[string]bool{
	"approved": true, "rejected": true, "pending": true, "": true,
}

// GetTestReview returns the Test's current review, with zero values when none
// has been recorded.
func (r *Repository) GetTestReview(profileID, testKey string) (Review, error) {
	var rv Review
	err := r.db.QueryRow(
		`SELECT verdict, reviewer, note, reviewed_at
		 FROM test_review WHERE profile_id = ? AND test_key = ?`,
		profileID, testKey,
	).Scan(&rv.Verdict, &rv.Reviewer, &rv.Note, &rv.ReviewedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Review{}, nil
	}
	if err != nil {
		return Review{}, fmt.Errorf("read review: %w", err)
	}
	return rv, nil
}

// SetTestReview records a review verdict for a Test and queues it for commit as
// a Jira comment. An empty verdict clears the review. The change is a no-op
// when nothing actually differs.
func (r *Repository) SetTestReview(profileID, testKey, verdict, reviewer, note string) error {
	if !validVerdicts[verdict] {
		return fmt.Errorf("unknown review verdict %q", verdict)
	}

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var before Review
	err = tx.QueryRow(
		`SELECT verdict, reviewer, note, reviewed_at
		 FROM test_review WHERE profile_id = ? AND test_key = ?`,
		profileID, testKey,
	).Scan(&before.Verdict, &before.Reviewer, &before.Note, &before.ReviewedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read existing review: %w", err)
	}

	after := Review{Verdict: verdict, Reviewer: reviewer, Note: note}
	if verdict != "" {
		after.ReviewedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if before.Verdict == after.Verdict && before.Reviewer == after.Reviewer && before.Note == after.Note {
		return nil
	}

	if verdict == "" {
		if _, err := tx.Exec(
			`DELETE FROM test_review WHERE profile_id = ? AND test_key = ?`,
			profileID, testKey,
		); err != nil {
			return fmt.Errorf("clear review: %w", err)
		}
	} else {
		if _, err := tx.Exec(
			`INSERT INTO test_review (profile_id, test_key, verdict, reviewer, note, reviewed_at)
			 VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT(profile_id, test_key) DO UPDATE SET
			   verdict = excluded.verdict,
			   reviewer = excluded.reviewer,
			   note = excluded.note,
			   reviewed_at = excluded.reviewed_at`,
			profileID, testKey, after.Verdict, after.Reviewer, after.Note, after.ReviewedAt,
		); err != nil {
			return fmt.Errorf("save review: %w", err)
		}
	}

	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	if err := upsertPendingChange(
		tx, profileID, entityTestReview, testKey, "review", string(beforeJSON), string(afterJSON), "",
	); err != nil {
		return err
	}
	if err := writeAudit(
		tx, profileID, entityTestReview, testKey, "review-local", "review", before.Verdict, after.Verdict, note,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// AddTestComment queues a free-text comment to post on a Test issue (FR-4.4 —
// e.g. the reason for a workflow transition). Each comment is a distinct
// pending row (keyed by time, so repeated comments don't coalesce) committed
// via the Jira comment API.
func (r *Repository) AddTestComment(profileID, testKey, body string) error {
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("a comment is required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Comments must never coalesce — each is its own row. Keying the pending row
	// by the timestamp alone collides when two comments land in the same clock
	// tick (the Windows clock is coarse), so add a per-Test sequence to keep the
	// field unique regardless of clock resolution.
	var seq int
	if err := tx.QueryRow(
		`SELECT COUNT(*) FROM pending_change
		 WHERE profile_id = ? AND entity_type = ? AND entity_key = ?`,
		profileID, entityIssueComment, testKey,
	).Scan(&seq); err != nil {
		return fmt.Errorf("count queued comments: %w", err)
	}
	field := fmt.Sprintf("comment:%s:%d", now, seq)

	if _, err := tx.Exec(
		`INSERT INTO pending_change
		   (profile_id, entity_type, entity_key, field, before_val, after_val, base_version, created_at)
		 VALUES (?, ?, ?, ?, '', ?, '', ?)`,
		profileID, entityIssueComment, testKey, field, body, now,
	); err != nil {
		return fmt.Errorf("queue comment: %w", err)
	}
	if err := writeAudit(
		tx, profileID, entityIssueComment, testKey, "comment-local", "comment", "", body, "",
	); err != nil {
		return err
	}
	return tx.Commit()
}

// BulkSetReview applies one review verdict to a batch of Tests (bulk sign-off),
// reporting per-Test success/failure.
func (r *Repository) BulkSetReview(profileID string, testKeys []string, verdict, reviewer, note string) (BulkEditResult, error) {
	result := BulkEditResult{Succeeded: []string{}, Failed: []BulkFailure{}}
	if !validVerdicts[verdict] {
		return result, fmt.Errorf("unknown review verdict %q", verdict)
	}
	for _, key := range testKeys {
		if err := r.SetTestReview(profileID, key, verdict, reviewer, note); err != nil {
			result.Failed = append(result.Failed, BulkFailure{TestKey: key, Error: err.Error()})
			continue
		}
		result.Succeeded = append(result.Succeeded, key)
	}
	return result, nil
}
