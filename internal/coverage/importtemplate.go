package coverage

import (
	"bytes"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
)

// ImportSummary reports what an XLSX template import produced.
type ImportSummary struct {
	Groups      int      `json:"groups"`
	Parameters  int      `json:"parameters"`
	Values      int      `json:"values"`
	MappedTests int      `json:"mappedTests"`
	Skipped     int      `json:"skipped"`
	Warnings    []string `json:"warnings"`
}

// testKeyRe matches a Jira/Xray issue key (e.g. TEST-4501). Used to pull real
// test keys out of free-form "Linked Test(s)" cells that may also contain "...".
var testKeyRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*-\d+$`)

// ImportCoverageTemplate ingests the manual PKCS#11 parameter-extraction
// workbook into a version's model. It is idempotent: the version's existing
// model is replaced in one transaction, so re-importing an updated workbook is
// safe. Test mappings are seeded only for test keys that already exist in
// test_case (others are reported as skipped) — matching the no-Jira-admin,
// local-first constraint.
//
// Recognised sheets (matched loosely by name, header-tolerant):
//   - "Parameter Values"   → groups from the Parameter Group column, values
//     from the Parameter Value column (value_kind=value).
//   - "Error Paths"        → an "Error Paths" group, one value per CKR_* code
//     (value_kind=errorcode).
//   - "Boundary Conditions" → a "Boundary Conditions" group (value_kind=
//     boundary, is_required=0 so boundaries are tracked without inflating the
//     headline denominator). N/A rows are skipped.
//
// Unrecognised sheets are ignored and noted in Warnings.
func (m *Module) ImportCoverageTemplate(profileID, versionID string, data []byte) (ImportSummary, error) {
	sum := ImportSummary{Warnings: []string{}}

	var exists int
	if err := m.db.QueryRow(
		`SELECT COUNT(*) FROM canonical_version WHERE profile_id = ? AND id = ?`,
		profileID, versionID).Scan(&exists); err != nil {
		return sum, fmt.Errorf("check version: %w", err)
	}
	if exists == 0 {
		return sum, fmt.Errorf("version %q not found", versionID)
	}

	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return sum, fmt.Errorf("open workbook: %w", err)
	}
	defer f.Close()

	// Set of test keys present locally, to seed only real mappings.
	known, err := m.knownTestKeys(profileID)
	if err != nil {
		return sum, err
	}

	tx, err := m.db.Begin()
	if err != nil {
		return sum, err
	}
	defer tx.Rollback()

	if err := clearModel(tx, profileID, versionID); err != nil {
		return sum, fmt.Errorf("clear existing model: %w", err)
	}

	sortGroup := 0
	for _, sheet := range f.GetSheetList() {
		rows, err := f.GetRows(sheet)
		if err != nil {
			sum.Warnings = append(sum.Warnings, fmt.Sprintf("sheet %q unreadable: %v", sheet, err))
			continue
		}
		low := strings.ToLower(sheet)
		switch {
		case strings.Contains(low, "parameter value"):
			if err := m.importValueSheet(tx, profileID, versionID, rows, known, &sum, &sortGroup); err != nil {
				return sum, err
			}
		case strings.Contains(low, "error path"):
			if err := m.importFlatSheet(tx, profileID, versionID, "Error Paths", "errorcode", rows, known, &sum, &sortGroup); err != nil {
				return sum, err
			}
		case strings.Contains(low, "boundary"):
			if err := m.importFlatSheet(tx, profileID, versionID, "Boundary Conditions", "boundary", rows, known, &sum, &sortGroup); err != nil {
				return sum, err
			}
		default:
			// Sheets 1/5/6/7 are reference/derived; ignore quietly unless they
			// look like they carried data we should have mapped.
			sum.Warnings = append(sum.Warnings, fmt.Sprintf("sheet %q not imported (not a recognised data sheet)", sheet))
		}
	}

	if err := tx.Commit(); err != nil {
		return sum, err
	}
	return sum, nil
}

// importValueSheet handles "2. Parameter Values": one group per distinct
// Parameter Group cell, each holding a single synthetic parameter whose values
// are the rows.
func (m *Module) importValueSheet(tx *sql.Tx, profileID, versionID string, rows [][]string, known map[string]bool, sum *ImportSummary, sortGroup *int) error {
	hdr, idx := findHeader(rows, []string{"parameter group", "parameter value"})
	if hdr < 0 {
		sum.Warnings = append(sum.Warnings, "Parameter Values: header row not found")
		return nil
	}
	colGroup := idx["parameter group"]
	colValue := idx["parameter value"]
	colDesc := idx["description"]
	colTests := idx["linked test(s)"]

	groupIDs := map[string]string{} // group name -> group id
	paramIDs := map[string]string{} // group name -> synthetic parameter id
	sortVal := map[string]int{}     // group name -> running value sort
	for _, row := range rows[hdr+1:] {
		groupName := cell(row, colGroup)
		valueLabel := cell(row, colValue)
		if groupName == "" || valueLabel == "" {
			sum.Skipped++
			continue
		}
		gid, ok := groupIDs[groupName]
		if !ok {
			gid = uuid.NewString()
			if _, err := tx.Exec(
				`INSERT INTO coverage_param_group (profile_id, id, canonical_id, version_id, name, sort_order)
				 VALUES (?, ?, '', ?, ?, ?)`,
				profileID, gid, versionID, groupName, *sortGroup); err != nil {
				return err
			}
			*sortGroup++
			groupIDs[groupName] = gid
			sum.Groups++

			pid := uuid.NewString()
			if _, err := tx.Exec(
				`INSERT INTO coverage_parameter (profile_id, id, group_id, name, kind, sort_order)
				 VALUES (?, ?, ?, ?, 'value', 0)`,
				profileID, pid, gid, groupName); err != nil {
				return err
			}
			paramIDs[groupName] = pid
			sum.Parameters++
		}
		vid := uuid.NewString()
		if _, err := tx.Exec(
			`INSERT INTO coverage_param_value
			   (profile_id, id, parameter_id, value_label, value_kind, is_required, notes, sort_order)
			 VALUES (?, ?, ?, ?, 'value', 1, ?, ?)`,
			profileID, vid, paramIDs[groupName], valueLabel, cell(row, colDesc), sortVal[groupName]); err != nil {
			return err
		}
		sortVal[groupName]++
		sum.Values++
		sum.MappedTests += seedMappings(tx, profileID, vid, cell(row, colTests), known, sum)
	}
	return nil
}

// importFlatSheet handles the Error Paths and Boundary Conditions sheets: a
// single group with one synthetic parameter, one value per row.
func (m *Module) importFlatSheet(tx *sql.Tx, profileID, versionID, groupName, valueKind string, rows [][]string, known map[string]bool, sum *ImportSummary, sortGroup *int) error {
	var labelKeys, testKeys []string
	if valueKind == "errorcode" {
		labelKeys = []string{"error code"}
		testKeys = []string{"test case(s)", "test case"}
	} else {
		labelKeys = []string{"parameter"}
		testKeys = []string{"test case", "test case(s)"}
	}
	hdr, idx := findHeader(rows, labelKeys)
	if hdr < 0 {
		sum.Warnings = append(sum.Warnings, groupName+": header row not found")
		return nil
	}
	colLabel := idx[labelKeys[0]]
	colDesc := idx["description"]
	if _, ok := idx["expected behavior"]; ok && colDesc < 0 {
		colDesc = idx["expected behavior"]
	}
	colBoundary := idx["boundary type"]
	colTests := -1
	for _, tk := range testKeys {
		if c, ok := idx[tk]; ok {
			colTests = c
			break
		}
	}

	gid := uuid.NewString()
	if _, err := tx.Exec(
		`INSERT INTO coverage_param_group (profile_id, id, canonical_id, version_id, name, sort_order)
		 VALUES (?, ?, '', ?, ?, ?)`,
		profileID, gid, versionID, groupName, *sortGroup); err != nil {
		return err
	}
	*sortGroup++
	sum.Groups++
	pid := uuid.NewString()
	if _, err := tx.Exec(
		`INSERT INTO coverage_parameter (profile_id, id, group_id, name, kind, sort_order)
		 VALUES (?, ?, ?, ?, 'value', 0)`,
		profileID, pid, gid, groupName); err != nil {
		return err
	}
	sum.Parameters++

	required := 1
	if valueKind == "boundary" {
		required = 0 // tracked but not part of the headline denominator
	}
	sortV := 0
	for _, row := range rows[hdr+1:] {
		label := cell(row, colLabel)
		if label == "" {
			sum.Skipped++
			continue
		}
		// Boundary rows often carry "N/A" placeholders — skip them.
		if valueKind == "boundary" {
			if colBoundary >= 0 {
				if bt := cell(row, colBoundary); strings.EqualFold(bt, "N/A") {
					sum.Skipped++
					continue
				}
				label = label + " — " + cell(row, colBoundary)
			}
		}
		errCode := ""
		if valueKind == "errorcode" {
			errCode = label
		}
		vid := uuid.NewString()
		if _, err := tx.Exec(
			`INSERT INTO coverage_param_value
			   (profile_id, id, parameter_id, value_label, value_kind, error_code, is_required, notes, sort_order)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			profileID, vid, pid, label, valueKind, errCode, required, cell(row, colDesc), sortV); err != nil {
			return err
		}
		sortV++
		sum.Values++
		if colTests >= 0 {
			sum.MappedTests += seedMappings(tx, profileID, vid, cell(row, colTests), known, sum)
		}
	}
	return nil
}

// seedMappings parses a free-form "linked tests" cell and inserts a mapping for
// each real, locally-known test key. Returns how many it inserted; unknown keys
// bump sum.Skipped.
func seedMappings(tx *sql.Tx, profileID, valueID, cellText string, known map[string]bool, sum *ImportSummary) int {
	n := 0
	for _, tok := range strings.Split(cellText, ",") {
		key := strings.TrimSpace(tok)
		if key == "" || !testKeyRe.MatchString(key) {
			continue
		}
		if !known[key] {
			sum.Skipped++
			continue
		}
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO coverage_value_test (profile_id, value_id, test_key, created_at)
			 VALUES (?, ?, ?, ?)`,
			profileID, valueID, key, nowISO()); err == nil {
			n++
		}
	}
	return n
}

// knownTestKeys returns the set of test_case keys for the profile.
func (m *Module) knownTestKeys(profileID string) (map[string]bool, error) {
	rows, err := m.db.Query(`SELECT jira_key FROM test_case WHERE profile_id = ?`, profileID)
	if err != nil {
		return nil, fmt.Errorf("read test keys: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out[k] = true
	}
	return out, rows.Err()
}

// clearModel deletes a version's entire parameter model (and mappings) inside
// the given transaction, leaving the version and canonical intact.
func clearModel(tx *sql.Tx, profileID, versionID string) error {
	stmts := []struct {
		q    string
		args []any
	}{
		{`DELETE FROM coverage_value_test WHERE profile_id = ? AND value_id IN (
			SELECT v.id FROM coverage_param_value v
			JOIN coverage_parameter p   ON p.profile_id = v.profile_id AND p.id = v.parameter_id
			JOIN coverage_param_group g ON g.profile_id = p.profile_id AND g.id = p.group_id
			WHERE g.profile_id = ? AND g.version_id = ?)`, []any{profileID, profileID, versionID}},
		{`DELETE FROM coverage_param_value WHERE profile_id = ? AND parameter_id IN (
			SELECT p.id FROM coverage_parameter p
			JOIN coverage_param_group g ON g.profile_id = p.profile_id AND g.id = p.group_id
			WHERE g.profile_id = ? AND g.version_id = ?)`, []any{profileID, profileID, versionID}},
		{`DELETE FROM coverage_parameter WHERE profile_id = ? AND group_id IN (
			SELECT id FROM coverage_param_group WHERE profile_id = ? AND version_id = ?)`, []any{profileID, profileID, versionID}},
		{`DELETE FROM coverage_param_group WHERE profile_id = ? AND version_id = ?`, []any{profileID, versionID}},
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s.q, s.args...); err != nil {
			return err
		}
	}
	return nil
}

// findHeader scans the first ~10 rows for a header row containing all the given
// (lower-cased) marker labels, returning its index and a map of lower-cased
// header text → column index. Returns -1 when not found.
func findHeader(rows [][]string, markers []string) (int, map[string]int) {
	limit := len(rows)
	if limit > 10 {
		limit = 10
	}
	for i := 0; i < limit; i++ {
		idx := map[string]int{}
		for c, val := range rows[i] {
			idx[strings.ToLower(strings.TrimSpace(val))] = c
		}
		ok := true
		for _, mk := range markers {
			if _, found := idx[mk]; !found {
				ok = false
				break
			}
		}
		if ok {
			return i, idx
		}
	}
	return -1, nil
}

// cell safely reads a column from a row that may be short or have a negative
// (absent) index.
func cell(row []string, col int) string {
	if col < 0 || col >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[col])
}
