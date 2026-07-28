package testrepo

import (
	"fmt"
	"strings"
)

// RequirementImportRow is one row from a requirement import file, classified
// as "new" (not yet in the store for this profile) or "existing" (matched by
// normalized summary).
type RequirementImportRow struct {
	Summary     string `json:"summary"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
	Components  string `json:"components"`
	FixVersions string `json:"fixVersions"`
	Status      string `json:"status"` // "new" | "existing"
}

// RequirementImportPreview is the result of parsing and classifying an import
// file before the user commits. Rows preserves file order; counts are derived.
type RequirementImportPreview struct {
	Rows          []RequirementImportRow `json:"rows"`
	NewCount      int                    `json:"newCount"`
	ExistingCount int                    `json:"existingCount"`
}

// RequirementImportResult summarises a completed import: how many were created
// locally, how many were skipped because they already exist, and any per-row
// errors (blank summary, parse failures).
type RequirementImportResult struct {
	Created         int           `json:"created"`
	SkippedExisting int           `json:"skippedExisting"`
	Errors          []ImportError `json:"errors"`
}

// RequirementImportTemplateCSV returns a starter CSV for requirement import.
// Columns: Summary (required), Description, Priority, Components, FixVersions.
func RequirementImportTemplateCSV() string {
	return "Summary,Description,Priority,Components,FixVersions\n" +
		"User can log in,The authentication flow works end to end,High,\"Frontend, Auth\",v2.0\n" +
		"Session expires after timeout,Idle sessions are terminated after 30 min,Medium,,v2.0\n"
}

// reqImportCols holds column indices for the requirement import auto-mapping.
// An index of -1 means the column was not found in the header.
type reqImportCols struct {
	summary, description, priority, components, fixVersions int
}

// reqImportMapping maps canonical requirement import column names to header
// positions, case-insensitively.
func reqImportMapping(header []string) reqImportCols {
	find := func(name string) int {
		for i, h := range header {
			if strings.EqualFold(strings.TrimSpace(h), name) {
				return i
			}
		}
		return -1
	}
	return reqImportCols{
		summary:     find("Summary"),
		description: find("Description"),
		priority:    find("Priority"),
		components:  find("Components"),
		fixVersions: find("FixVersions"),
	}
}

// parseRequirementImportRecords converts spreadsheet records to import rows.
// Blank-summary rows are included with an empty Summary so callers can count
// them; callers that need to skip blanks check Summary == "".
func parseRequirementImportRecords(records [][]string) ([]RequirementImportRow, error) {
	if len(records) == 0 {
		return nil, fmt.Errorf("the file is empty")
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("the file has no data rows")
	}
	cols := reqImportMapping(records[0])
	if cols.summary < 0 {
		return nil, fmt.Errorf("the file must have a Summary column")
	}
	get := func(row []string, idx int) string {
		if idx < 0 || idx >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[idx])
	}
	out := make([]RequirementImportRow, 0, len(records)-1)
	for _, row := range records[1:] {
		out = append(out, RequirementImportRow{
			Summary:     get(row, cols.summary),
			Description: get(row, cols.description),
			Priority:    get(row, cols.priority),
			Components:  get(row, cols.components),
			FixVersions: get(row, cols.fixVersions),
		})
	}
	return out, nil
}

// AnalyzeRequirementImport parses the import records and classifies each row
// as "new" or "existing" by comparing its normalized summary against every
// requirement already cached for this profile. Returns a preview with counts.
func (r *Repository) AnalyzeRequirementImport(profileID string, records [][]string) (RequirementImportPreview, error) {
	preview := RequirementImportPreview{Rows: []RequirementImportRow{}}
	rows, err := parseRequirementImportRecords(records)
	if err != nil {
		return preview, err
	}

	// Build a set of normalized summaries already in the store.
	existing, err := r.ListRequirementsWithCoverage(profileID)
	if err != nil {
		return preview, fmt.Errorf("read existing requirements: %w", err)
	}
	existingSet := make(map[string]bool, len(existing))
	for _, rq := range existing {
		if k := normalizeSummary(rq.Summary); k != "" {
			existingSet[k] = true
		}
	}

	for _, row := range rows {
		k := normalizeSummary(row.Summary)
		if k == "" || existingSet[k] {
			row.Status = "existing"
			preview.ExistingCount++
		} else {
			row.Status = "new"
			preview.NewCount++
		}
		preview.Rows = append(preview.Rows, row)
	}
	return preview, nil
}

// ImportRequirements parses the import records, skips rows whose normalized
// summary already exists in the store for this profile, and calls
// CreateRequirement for each new row. Returns a summary of the operation.
func (r *Repository) ImportRequirements(profileID, projectKey, issueType string, records [][]string) (RequirementImportResult, error) {
	result := RequirementImportResult{Errors: []ImportError{}}
	rows, err := parseRequirementImportRecords(records)
	if err != nil {
		return result, err
	}

	// Build existing-summary set once.
	existing, err := r.ListRequirementsWithCoverage(profileID)
	if err != nil {
		return result, fmt.Errorf("read existing requirements: %w", err)
	}
	existingSet := make(map[string]bool, len(existing))
	for _, rq := range existing {
		if k := normalizeSummary(rq.Summary); k != "" {
			existingSet[k] = true
		}
	}

	for i, row := range rows {
		rowNum := i + 2 // 1-based data row number (header is row 1)
		if strings.TrimSpace(row.Summary) == "" {
			result.Errors = append(result.Errors, ImportError{Row: rowNum, Message: "blank summary — row skipped"})
			continue
		}
		if existingSet[normalizeSummary(row.Summary)] {
			result.SkippedExisting++
			continue
		}
		if _, err := r.CreateRequirement(
			profileID, projectKey, issueType,
			row.Summary, row.Description, row.Priority,
			row.Components, row.FixVersions, nil,
		); err != nil {
			result.Errors = append(result.Errors, ImportError{Row: rowNum, Message: err.Error()})
			continue
		}
		result.Created++
		// Add to existing set so duplicate rows in the same file don't double-create.
		existingSet[normalizeSummary(row.Summary)] = true
	}
	return result, nil
}
