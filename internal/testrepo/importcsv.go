package testrepo

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

// ImportPreview is what a freshly-parsed import file looks like before mapping
// (FR-10.5): its column headers and the number of data rows.
type ImportPreview struct {
	Headers  []string `json:"headers"`
	RowCount int      `json:"rowCount"`
}

// ImportMapping maps Test fields to spreadsheet column headers (FR-10.4). An
// empty value means the field is unmapped. Summary is required. The step
// columns support a Test that spans several rows (FR-10.7): a row with a
// Summary starts a Test; following rows with an empty Summary add steps to it.
type ImportMapping struct {
	Summary     string `json:"summary"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
	Labels      string `json:"labels"`
	Components  string `json:"components"`
	Folder      string `json:"folder"`
	Action      string `json:"action"`
	Data        string `json:"data"`
	Expected    string `json:"expected"`
}

// importStep is one step parsed from an import row (FR-10.7).
type importStep struct {
	Action   string `json:"action"`
	Data     string `json:"data"`
	Expected string `json:"expected"`
}

// ImportError is one row that failed validation (FR-10.5).
type ImportError struct {
	Row     int    `json:"row"`
	Message string `json:"message"`
}

// ImportResult reports an import (or dry-run) outcome (FR-10.5 / 10.6).
type ImportResult struct {
	Created int           `json:"created"`
	Skipped int           `json:"skipped"`
	Errors  []ImportError `json:"errors"`
}

// testCreatePayload is the JSON stored in a test_create pending row.
type testCreatePayload struct {
	Summary     string       `json:"summary"`
	Description string       `json:"description"`
	Priority    string       `json:"priority"`
	Labels      string       `json:"labels"`
	Components  string       `json:"components"` // comma-separated names
	Folder      string       `json:"folder"`
	Steps       []importStep `json:"steps,omitempty"`
}

// ParseImportPreview reads the header row and counts data rows (FR-10.2 / 10.5).
func ParseImportPreview(records [][]string) (ImportPreview, error) {
	if len(records) == 0 {
		return ImportPreview{}, fmt.Errorf("the file is empty")
	}
	return ImportPreview{Headers: records[0], RowCount: len(records) - 1}, nil
}

// ImportTests validates a CSV import against a column mapping and, unless
// dryRun, creates a local pending Test for each valid row (FR-10.2 / 10.4 /
// 10.5 / 10.6). Each created Test gets a temporary "NEW-N" key until commit
// assigns the real one. Invalid rows are reported and skipped, not fatal.
func (r *Repository) ImportTests(profileID string, records [][]string, mapping ImportMapping, dryRun bool) (ImportResult, error) {
	result := ImportResult{Errors: []ImportError{}}

	if len(records) < 2 {
		return result, fmt.Errorf("the file has no data rows")
	}
	header := records[0]
	col := func(name string) int {
		if name == "" {
			return -1
		}
		for i, h := range header {
			if strings.EqualFold(strings.TrimSpace(h), strings.TrimSpace(name)) {
				return i
			}
		}
		return -1
	}
	summaryIdx := col(mapping.Summary)
	if summaryIdx < 0 {
		return result, fmt.Errorf("the Summary field must be mapped to a column")
	}
	descIdx := col(mapping.Description)
	prioIdx := col(mapping.Priority)
	labelsIdx := col(mapping.Labels)
	componentsIdx := col(mapping.Components)
	folderIdx := col(mapping.Folder)
	actionIdx := col(mapping.Action)
	dataIdx := col(mapping.Data)
	expectedIdx := col(mapping.Expected)

	get := func(row []string, idx int) string {
		if idx < 0 || idx >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[idx])
	}

	// First pass: group rows into Tests. A row with a Summary starts a Test;
	// a row with an empty Summary but step content extends the previous Test's
	// steps (FR-10.7).
	tests := []testCreatePayload{}
	curIdx := -1
	for i := 1; i < len(records); i++ {
		rowNum := i + 1
		summary := get(records[i], summaryIdx)
		step := importStep{
			Action:   get(records[i], actionIdx),
			Data:     get(records[i], dataIdx),
			Expected: get(records[i], expectedIdx),
		}
		hasStep := step.Action != "" || step.Data != "" || step.Expected != ""

		if summary != "" {
			tests = append(tests, testCreatePayload{
				Summary:     summary,
				Description: get(records[i], descIdx),
				Priority:    get(records[i], prioIdx),
				Labels:      get(records[i], labelsIdx),
				Components:  get(records[i], componentsIdx),
				Folder:      get(records[i], folderIdx),
			})
			curIdx = len(tests) - 1
			if hasStep {
				tests[curIdx].Steps = append(tests[curIdx].Steps, step)
			}
			continue
		}
		// Empty summary.
		if hasStep {
			if curIdx < 0 {
				result.Errors = append(result.Errors, ImportError{Row: rowNum, Message: "step row before any test summary"})
				result.Skipped++
				continue
			}
			tests[curIdx].Steps = append(tests[curIdx].Steps, step)
			continue
		}
		result.Errors = append(result.Errors, ImportError{Row: rowNum, Message: "row has neither a summary nor step content"})
		result.Skipped++
	}

	result.Created = len(tests)
	if dryRun {
		return result, nil
	}

	tx, err := r.db.Begin()
	if err != nil {
		return result, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, p := range tests {
		if err := insertImportedTest(tx, profileID, p); err != nil {
			return result, err
		}
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit import: %w", err)
	}
	return result, nil
}

// insertImportedTest creates one pending Test from an import row.
func insertImportedTest(tx *sql.Tx, profileID string, p testCreatePayload) error {
	tempKey, err := nextTempTestKey(tx, profileID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(
		`INSERT INTO test_case
		   (profile_id, jira_key, jira_id, summary, description, status, priority, labels, components, updated_at, folder_id)
		 VALUES (?, ?, '', ?, ?, '', ?, ?, ?, '', ?)`,
		profileID, tempKey, p.Summary, p.Description, p.Priority, p.Labels,
		encodeComponents(strings.Split(p.Components, ",")), p.Folder,
	); err != nil {
		return fmt.Errorf("insert imported test: %w", err)
	}
	for i, s := range p.Steps {
		if _, err := tx.Exec(
			`INSERT INTO test_step (profile_id, test_key, xray_id, idx, action, data, expected)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			profileID, tempKey, fmt.Sprintf("imp-%d", i+1), i+1, s.Action, s.Data, s.Expected,
		); err != nil {
			return fmt.Errorf("insert imported step: %w", err)
		}
	}
	encoded, _ := json.Marshal(p)
	if err := upsertPendingChange(
		tx, profileID, entityTestCreate, tempKey, "test", "", string(encoded), "",
	); err != nil {
		return err
	}
	return writeAudit(
		tx, profileID, entityTestCreate, tempKey, "import-test-local", "test", "", p.Summary, "",
	)
}

// nextTempTestKey returns a Test key of the form "NEW-N" not already used in
// this profile.
func nextTempTestKey(tx *sql.Tx, profileID string) (string, error) {
	for n := 1; ; n++ {
		key := fmt.Sprintf("NEW-%d", n)
		var one int
		err := tx.QueryRow(
			`SELECT 1 FROM test_case WHERE profile_id = ? AND jira_key = ?`, profileID, key,
		).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return key, nil
		}
		if err != nil {
			return "", fmt.Errorf("probe temp test key: %w", err)
		}
	}
}

// RenameTest rewrites a Test's key across the cache, used by the commit path to
// swap a "NEW-N" placeholder for the real key Jira assigned. A no-op when
// newKey is empty or unchanged.
func (r *Repository) RenameTest(profileID, oldKey, newKey string) error {
	if newKey == "" || newKey == oldKey {
		return nil
	}
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, stmt := range []struct {
		table, keyCol string
	}{
		{"test_case", "jira_key"},
		{"test_step", "test_key"},
		{"test_precondition", "test_key"},
		{"test_container_test", "test_key"},
	} {
		if _, err := tx.Exec(
			fmt.Sprintf(`UPDATE %s SET %s = ? WHERE profile_id = ? AND %s = ?`,
				stmt.table, stmt.keyCol, stmt.keyCol),
			newKey, profileID, oldKey,
		); err != nil {
			return fmt.Errorf("rename test in %s: %w", stmt.table, err)
		}
	}
	return tx.Commit()
}

// ParseRecords parses raw import file bytes into rows — CSV or XLSX (FR-10.1 /
// 10.2). For XLSX the first worksheet is used.
func ParseRecords(data []byte, isXlsx bool) ([][]string, error) {
	if isXlsx {
		return parseXLSX(data)
	}
	return readCSV(string(data))
}

// readCSV parses CSV content leniently (variable field counts allowed).
func readCSV(content string) ([][]string, error) {
	reader := csv.NewReader(strings.NewReader(content))
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse CSV: %w", err)
	}
	return records, nil
}

// parseXLSX reads the first worksheet of an XLSX file into rows (FR-10.1).
func parseXLSX(data []byte) ([][]string, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
	}
	defer func() { _ = f.Close() }()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("the workbook has no sheets")
	}
	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, fmt.Errorf("read xlsx rows: %w", err)
	}
	return rows, nil
}

// ImportTemplateCSV returns a starter CSV with the supported columns (FR-10.3),
// matching the current Test fields (Components included) and the export column
// order so an export round-trips through import. Labels are space-separated;
// Components are comma-separated (component names can contain spaces). The
// second test shows the multi-row step format (FR-10.7): a row with a Summary
// starts a test; following rows with an empty Summary add steps.
func ImportTemplateCSV() string {
	return "Summary,Description,Priority,Labels,Components,Folder,Action,Data,Expected\n" +
		"Login with valid credentials,Verify a user can log in,High,smoke api,\"Authentication, Frontend\",/Authentication/Login,,,\n" +
		"Login flow with steps,Multi-step example,Medium,smoke,Frontend,/Authentication/Login,Open the login page,,Login form is shown\n" +
		",,,,,,Enter credentials and submit,user / pass,User is logged in\n"
}
