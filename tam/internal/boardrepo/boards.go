package boardrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"agile-suite/tam/internal/backend"
)

const upsertBoardSQL = `
	INSERT INTO board (profile_id, id, name, type, synced_at) VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(profile_id, id) DO UPDATE SET
		name = excluded.name, type = excluded.type, synced_at = excluded.synced_at`

const insertColumnSQL = `
	INSERT INTO board_column (profile_id, board_id, position, name, status_ids) VALUES (?, ?, ?, ?, ?)`

const insertSprintSQL = `
	INSERT INTO sprint (profile_id, id, board_id, name, state, start_date, end_date) VALUES (?, ?, ?, ?, ?, ?, ?)`

const insertIssueKeySQL = `
	INSERT INTO board_issue (profile_id, board_id, sprint_id, key, position) VALUES (?, ?, ?, ?, ?)`

const listBoardsSQL = `
	SELECT id, name, type FROM board WHERE profile_id = ? ORDER BY name, id`

const columnsSQL = `
	SELECT name, status_ids FROM board_column WHERE profile_id = ? AND board_id = ? ORDER BY position`

// listSprintsSQL puts the sprint the user most likely wants first: the
// active one, then the future ones, then what is closed, each group by start
// date. Jira sends the state lowercase and the backends keep it that way, so
// the CASE matches without folding.
const listSprintsSQL = `
	SELECT id, board_id, name, state, start_date, end_date FROM sprint
	WHERE profile_id = ? AND board_id = ?
	ORDER BY CASE state WHEN 'active' THEN 0 WHEN 'future' THEN 1 WHEN 'closed' THEN 2 ELSE 3 END, start_date, id`

const boardKeysSQL = `
	SELECT key FROM board_issue WHERE profile_id = ? AND board_id = ? AND sprint_id = ? ORDER BY position`

// RemoveBoards drops the boards and everything hanging off them: their
// columns, their issue keys, and their sprints, in one transaction.
func (r *Repository) RemoveBoards(ctx context.Context, profileID string, boardIDs []int) error {
	if len(boardIDs) == 0 {
		return nil
	}
	return r.inTx(ctx, func(tx *sql.Tx) error {
		for _, id := range boardIDs {
			for _, table := range []string{"board_column", "board_issue", "sprint", "board"} {
				column := "board_id"
				if table == "board" {
					column = "id"
				}
				if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE profile_id = ? AND `+column+` = ?`, profileID, id); err != nil {
					return fmt.Errorf("remove %s of board %d: %w", table, id, err)
				}
			}
		}
		return nil
	})
}

// ListBoards returns the profile's cached boards by name.
func (r *Repository) ListBoards(ctx context.Context, profileID string) ([]Board, error) {
	rows, err := r.db.QueryContext(ctx, listBoardsSQL, profileID)
	if err != nil {
		return nil, fmt.Errorf("list boards: %w", err)
	}
	defer rows.Close()
	out := []Board{}
	for rows.Next() {
		var b Board
		if err := rows.Scan(&b.ID, &b.Name, &b.Type); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// queryer is the read surface the board reads need, satisfied by both *sql.DB
// and *sql.Tx. Taking it as a parameter is what lets two of these reads share
// one transaction, and so one snapshot (see Shape).
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Columns returns one board's columns in board order.
func (r *Repository) Columns(ctx context.Context, profileID string, boardID int) ([]backend.BoardColumn, error) {
	return columnsFrom(ctx, r.db, profileID, boardID)
}

func columnsFrom(ctx context.Context, q queryer, profileID string, boardID int) ([]backend.BoardColumn, error) {
	rows, err := q.QueryContext(ctx, columnsSQL, profileID, boardID)
	if err != nil {
		return nil, fmt.Errorf("board %d columns: %w", boardID, err)
	}
	defer rows.Close()
	out := []backend.BoardColumn{}
	for rows.Next() {
		var (
			c   backend.BoardColumn
			ids string
		)
		if err := rows.Scan(&c.Name, &ids); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(ids), &c.StatusIDs); err != nil {
			return nil, fmt.Errorf("status ids of column %q: %w", c.Name, err)
		}
		c.StatusIDs = backend.NonNil(c.StatusIDs)
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListSprints returns one board's sprints, active first, then future, then
// closed, each group by start date.
func (r *Repository) ListSprints(ctx context.Context, profileID string, boardID int) ([]Sprint, error) {
	rows, err := r.db.QueryContext(ctx, listSprintsSQL, profileID, boardID)
	if err != nil {
		return nil, fmt.Errorf("board %d sprints: %w", boardID, err)
	}
	defer rows.Close()
	out := []Sprint{}
	for rows.Next() {
		var s Sprint
		if err := rows.Scan(&s.ID, &s.BoardID, &s.Name, &s.State, &s.StartDate, &s.EndDate); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// issueKeys returns the keys one board holds for a sprint, in board order.
// An empty sprintID reads the board's own list, the way the sync stored it.
func (r *Repository) issueKeys(ctx context.Context, profileID string, boardID int, sprintID string) ([]string, error) {
	return issueKeysFrom(ctx, r.db, profileID, boardID, sprintID)
}

func issueKeysFrom(ctx context.Context, q queryer, profileID string, boardID int, sprintID string) ([]string, error) {
	rows, err := q.QueryContext(ctx, boardKeysSQL, profileID, boardID, sprintID)
	if err != nil {
		return nil, fmt.Errorf("board %d issue keys: %w", boardID, err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

// BoardShape is the pair ReplaceBoard writes together: a board's columns and
// the key list of one scope. One column per card is not the invariant; landing
// together is. Both are read in one snapshot so a caller cannot draw the
// columns of one sync against the membership of another.
type BoardShape struct {
	Columns []backend.BoardColumn
	Keys    []string
}

// Shape reads a board's columns and one scope's membership inside a single
// read transaction.
//
// Reading them as two statements meant two snapshots, and ReplaceBoard commits
// between them often enough to matter: the sync replaces every board on every
// pass, so the Boards view could draw a board's new columns against its
// previous membership. The write side has always been one transaction; this is
// the other half of that guarantee.
func (r *Repository) Shape(ctx context.Context, profileID string, boardID int, sprintID string) (BoardShape, error) {
	// Deferred and read-only: SQLite opens the snapshot at the first read and
	// holds it until the transaction ends, which is exactly the window the two
	// reads below need to share.
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return BoardShape{}, fmt.Errorf("board %d read: %w", boardID, err)
	}
	defer tx.Rollback()

	cols, err := columnsFrom(ctx, tx, profileID, boardID)
	if err != nil {
		return BoardShape{}, err
	}
	keys, err := issueKeysFrom(ctx, tx, profileID, boardID, sprintID)
	if err != nil {
		return BoardShape{}, err
	}
	return BoardShape{Columns: cols, Keys: keys}, nil
}

// inTx runs fn inside one transaction, so a replace never leaves the table
// holding a delete without its inserts.
func (r *Repository) inTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
