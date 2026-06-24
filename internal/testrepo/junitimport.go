package testrepo

import (
	"database/sql"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
)

// JUnitMatch is one testcase matched to a single member test of the execution,
// with the run result it will set.
type JUnitMatch struct {
	Testcase   string `json:"testcase"`
	TestKey    string `json:"testKey"`
	Summary    string `json:"summary"`
	Result     string `json:"result"`     // "PASS" or "FAIL"
	CurrentRun string `json:"currentRun"` // the test's current run status in this execution
}

// JUnitSkip is a testcase that will not be applied, with the reason.
type JUnitSkip struct {
	Testcase string `json:"testcase"`
	Reason   string `json:"reason"`
}

// JUnitImportPreview is the result of analyzing a JUnit report against an
// execution.
type JUnitImportPreview struct {
	ExecKey string       `json:"execKey"`
	Total   int          `json:"total"`
	Matched []JUnitMatch `json:"matched"`
	Skipped []JUnitSkip  `json:"skipped"`
}

// junitTestSuites is the root element when the XML wraps suites inside
// <testsuites>.
type junitTestSuites struct {
	XMLName    xml.Name         `xml:"testsuites"`
	TestSuites []junitTestSuite `xml:"testsuite"`
}

// junitTestSuite is a single <testsuite> element (also used as root for bare
// single-suite reports).
type junitTestSuite struct {
	XMLName   xml.Name        `xml:"testsuite"`
	TestCases []junitTestCase `xml:"testcase"`
}

// junitTestCase holds the fields we care about from a <testcase> element.
type junitTestCase struct {
	Name    string          `xml:"name,attr"`
	Failure *junitPresence  `xml:"failure"`
	Error   *junitPresence  `xml:"error"`
	Skipped *junitPresence  `xml:"skipped"`
}

// junitPresence is used for child elements whose mere presence is meaningful
// (failure, error, skipped) - the content is ignored.
type junitPresence struct{}

// parseJUnitXML decodes a JUnit XML byte slice, supporting both a bare
// <testsuite> root and a <testsuites> wrapper.
func parseJUnitXML(data []byte) ([]junitTestCase, error) {
	// Try <testsuites> wrapper first.
	var suites junitTestSuites
	if err := xml.Unmarshal(data, &suites); err == nil && len(suites.TestSuites) > 0 {
		var all []junitTestCase
		for _, s := range suites.TestSuites {
			all = append(all, s.TestCases...)
		}
		return all, nil
	}

	// Fall back to bare <testsuite>.
	var suite junitTestSuite
	if err := xml.Unmarshal(data, &suite); err != nil {
		return nil, fmt.Errorf("parse JUnit XML: %w", err)
	}
	return suite.TestCases, nil
}

// execMember holds the summary and current run status for one execution member.
type execMember struct {
	testKey   string
	summary   string
	runStatus string
}

// loadExecMembers returns every member test of the given execution, resolved
// with summary from test_case or external_test and run status from test_run or
// test_container_test.
func (r *Repository) loadExecMembers(profileID, execKey string) ([]execMember, error) {
	rows, err := r.db.Query(`
		SELECT l.test_key,
		       COALESCE(t.summary, x.summary, '') AS summary,
		       COALESCE(tr.run_status, l.run_status, '') AS run_status
		FROM test_container_test l
		LEFT JOIN test_case     t  ON t.profile_id  = l.profile_id AND t.jira_key = l.test_key
		LEFT JOIN external_test x  ON x.profile_id  = l.profile_id AND x.jira_key = l.test_key
		LEFT JOIN test_run      tr ON tr.profile_id = l.profile_id
		                          AND tr.exec_key   = l.container_key
		                          AND tr.test_key   = l.test_key
		WHERE l.profile_id = ? AND l.container_key = ?
		ORDER BY l.test_key`,
		profileID, execKey)
	if err != nil {
		return nil, fmt.Errorf("load exec members: %w", err)
	}
	defer rows.Close()

	var out []execMember
	for rows.Next() {
		var m execMember
		if err := rows.Scan(&m.testKey, &m.summary, &m.runStatus); err != nil {
			return nil, fmt.Errorf("scan exec member: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// junitResult returns "PASS", "FAIL", or "" (skipped) for a parsed testcase.
// A testcase with a <skipped/> element is left unset (caller decides).
func junitResult(tc junitTestCase) (result string, skipped bool) {
	if tc.Skipped != nil {
		return "", true
	}
	if tc.Failure != nil || tc.Error != nil {
		return "FAIL", false
	}
	return "PASS", false
}

// isExecMember checks whether testKey is a member of execKey for the given
// profile, using the db directly (used for re-validation in ApplyJUnitImport).
func isExecMember(db *sql.DB, profileID, execKey, testKey string) (bool, error) {
	var one int
	err := db.QueryRow(
		`SELECT 1 FROM test_container_test
		 WHERE profile_id = ? AND container_key = ? AND test_key = ?`,
		profileID, execKey, testKey,
	).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check membership: %w", err)
	}
	return true, nil
}

// AnalyzeJUnitImport decodes a base64-encoded JUnit XML report and matches its
// testcases to member tests of the given execution by summary, returning a
// preview of what would be applied. Each testcase is either matched (one member
// shares its name as summary) or skipped (no match, ambiguous match, or skipped
// in the report). The caller can then pass Matched to ApplyJUnitImport.
func (r *Repository) AnalyzeJUnitImport(profileID, execKey, xmlBase64 string) (JUnitImportPreview, error) {
	preview := JUnitImportPreview{
		ExecKey: execKey,
		Matched: []JUnitMatch{},
		Skipped: []JUnitSkip{},
	}

	xmlBytes, err := base64.StdEncoding.DecodeString(xmlBase64)
	if err != nil {
		return preview, fmt.Errorf("decode XML: %w", err)
	}

	testcases, err := parseJUnitXML(xmlBytes)
	if err != nil {
		return preview, err
	}
	preview.Total = len(testcases)

	members, err := r.loadExecMembers(profileID, execKey)
	if err != nil {
		return preview, err
	}

	// Build summary -> []member map (case-sensitive, as Jira stores summaries).
	byName := make(map[string][]execMember, len(members))
	for _, m := range members {
		byName[m.summary] = append(byName[m.summary], m)
	}

	for _, tc := range testcases {
		result, skipped := junitResult(tc)

		if skipped {
			preview.Skipped = append(preview.Skipped, JUnitSkip{
				Testcase: tc.Name,
				Reason:   "skipped in the report; run left unchanged",
			})
			continue
		}

		hits := byName[tc.Name]
		switch len(hits) {
		case 0:
			preview.Skipped = append(preview.Skipped, JUnitSkip{
				Testcase: tc.Name,
				Reason:   "no matching test in this execution",
			})
		case 1:
			preview.Matched = append(preview.Matched, JUnitMatch{
				Testcase:   tc.Name,
				TestKey:    hits[0].testKey,
				Summary:    hits[0].summary,
				Result:     result,
				CurrentRun: hits[0].runStatus,
			})
		default:
			preview.Skipped = append(preview.Skipped, JUnitSkip{
				Testcase: tc.Name,
				Reason:   fmt.Sprintf("ambiguous: %d tests share this summary", len(hits)),
			})
		}
	}

	return preview, nil
}

// ApplyJUnitImport sets the run result for each matched testcase in the given
// execution, re-validating membership before each update. Results are collected
// as a BulkEditResult. A match whose test is no longer a member of the
// execution is reported as failed.
func (r *Repository) ApplyJUnitImport(profileID, execKey string, matches []JUnitMatch) (BulkEditResult, error) {
	result := BulkEditResult{Succeeded: []string{}, Failed: []BulkFailure{}}

	for _, m := range matches {
		ok, err := isExecMember(r.db, profileID, execKey, m.TestKey)
		if err != nil {
			result.Failed = append(result.Failed, BulkFailure{TestKey: m.TestKey, Error: err.Error()})
			continue
		}
		if !ok {
			result.Failed = append(result.Failed, BulkFailure{
				TestKey: m.TestKey,
				Error:   fmt.Sprintf("%s is not a member of execution %s", m.TestKey, execKey),
			})
			continue
		}

		if err := r.SetTestRunStatus(profileID, execKey, m.TestKey, m.Result); err != nil {
			result.Failed = append(result.Failed, BulkFailure{TestKey: m.TestKey, Error: err.Error()})
			continue
		}
		result.Succeeded = append(result.Succeeded, m.TestKey)
	}

	return result, nil
}
