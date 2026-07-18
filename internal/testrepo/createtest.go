package testrepo

import (
	"fmt"
	"strings"
)

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
	Summary           string      `json:"summary"`
	Description       string      `json:"description"`
	Priority          string      `json:"priority"`
	Labels            string      `json:"labels"`
	Components        string      `json:"components"`
	ExecType          string      `json:"execType"`
	CucumberScenario  string      `json:"cucumberScenario"`
	CucumberType      string      `json:"cucumberType"`
	GenericDefinition string      `json:"genericDefinition"`
	FolderID          string      `json:"folderId"`
	Steps             []StepDraft `json:"steps"`
	PrecondKeys       []string    `json:"precondKeys"`
}

// CreateTest queues a brand-new local Test (temp "NEW-N" key) with its steps,
// and — when supplied — a folder placement and precondition links, all keyed by
// the temp key so the commit engine pushes them after the Test is created
// (FR-1). Returns the temp key. Folder is deliberately left out of the create
// payload and applied via MoveTestToFolder so a folder pending row is queued
// (the create POST itself does not set the Test Repository folder).
func (r *Repository) CreateTest(profileID string, d TestDraft) (string, error) {
	p := testCreatePayload{
		Summary:           d.Summary,
		Description:       d.Description,
		Priority:          d.Priority,
		Labels:            d.Labels,
		Components:        d.Components,
		ExecType:          d.ExecType,
		CucumberScenario:  d.CucumberScenario,
		CucumberType:      d.CucumberType,
		GenericDefinition: d.GenericDefinition,
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

// CloneTest drafts a brand-new local Test (temp "NEW-N" key) that copies an
// existing Test's fields and steps, so a similar test can be created without
// re-entering everything (RND_P_4TFINT_05-206). The clone's summary gets a
// " (copy)" suffix; it has no link back to the source and is queued for creation
// on commit like any New Test. Steps come from the local cache, so the caller
// should ensure they are loaded first (the detail panel loads them on open);
// call-test steps are preserved as call steps, manual steps as manual content.
func (r *Repository) CloneTest(profileID, sourceKey string) (string, error) {
	src, err := r.GetTest(profileID, sourceKey)
	if err != nil {
		return "", fmt.Errorf("read source test: %w", err)
	}
	steps, err := r.ListTestSteps(profileID, sourceKey)
	if err != nil {
		return "", fmt.Errorf("read source steps: %w", err)
	}

	// Create the test shell first (no steps in the draft), then append each step
	// so call-test steps survive — TestDraft only carries manual step content.
	tempKey, err := r.CreateTest(profileID, TestDraft{
		Summary:           strings.TrimSpace(src.Summary) + " (copy)",
		Description:       src.Description,
		Priority:          src.Priority,
		Labels:            strings.Join(src.Labels, " "),
		Components:        strings.Join(src.Components, ","),
		ExecType:          src.ExecType,
		CucumberScenario:  src.CucumberScenario,
		CucumberType:      src.CucumberType,
		GenericDefinition: src.GenericDefinition,
	})
	if err != nil {
		return "", err
	}

	for _, s := range steps {
		if s.CalledTestKey != "" {
			if _, err := r.AddCalledTestStep(profileID, tempKey, s.CalledTestKey); err != nil {
				return tempKey, fmt.Errorf("clone call step: %w", err)
			}
			continue
		}
		if s.Action == "" && s.Data == "" && s.Expected == "" {
			continue // skip blank steps (Xray rejects them)
		}
		if _, err := r.AddTestStep(profileID, tempKey, s.Action, s.Data, s.Expected); err != nil {
			return tempKey, fmt.Errorf("clone step: %w", err)
		}
	}
	return tempKey, nil
}
