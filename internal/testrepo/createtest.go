package testrepo

import "fmt"

// StepDraft is one step entered in the interactive New Test form (FR-1).
type StepDraft struct {
	Action   string `json:"action"`
	Data     string `json:"data"`
	Expected string `json:"expected"`
}

// TestDraft is the payload the New Test panel submits (FR-1). Labels are
// space-separated and Components comma-separated, matching CSV import. FolderID
// and PrecondKeys are optional.
type TestDraft struct {
	Summary     string      `json:"summary"`
	Description string      `json:"description"`
	Priority    string      `json:"priority"`
	Labels      string      `json:"labels"`
	Components  string      `json:"components"`
	FolderID    string      `json:"folderId"`
	Steps       []StepDraft `json:"steps"`
	PrecondKeys []string    `json:"precondKeys"`
}

// CreateTest queues a brand-new local Test (temp "NEW-N" key) with its steps,
// and — when supplied — a folder placement and precondition links, all keyed by
// the temp key so the commit engine pushes them after the Test is created
// (FR-1). Returns the temp key. Folder is deliberately left out of the create
// payload and applied via MoveTestToFolder so a folder pending row is queued
// (the create POST itself does not set the Test Repository folder).
func (r *Repository) CreateTest(profileID string, d TestDraft) (string, error) {
	p := testCreatePayload{
		Summary:     d.Summary,
		Description: d.Description,
		Priority:    d.Priority,
		Labels:      d.Labels,
		Components:  d.Components,
	}
	for _, s := range d.Steps {
		if s.Action == "" && s.Data == "" && s.Expected == "" {
			continue // drop blank rows
		}
		p.Steps = append(p.Steps, importStep{Action: s.Action, Data: s.Data, Expected: s.Expected})
	}

	tx, err := r.db.Begin()
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	tempKey, err := insertLocalTest(tx, profileID, p, "create-test-local")
	if err != nil {
		_ = tx.Rollback()
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit create: %w", err)
	}

	// Folder + preconditions ride the existing per-Test rails, keyed by tempKey.
	if d.FolderID != "" {
		if err := r.MoveTestToFolder(profileID, tempKey, d.FolderID); err != nil {
			return tempKey, fmt.Errorf("set folder on new test: %w", err)
		}
	}
	if len(d.PrecondKeys) > 0 {
		if err := r.SetTestPreconditions(profileID, tempKey, d.PrecondKeys); err != nil {
			return tempKey, fmt.Errorf("link preconditions on new test: %w", err)
		}
	}
	return tempKey, nil
}
