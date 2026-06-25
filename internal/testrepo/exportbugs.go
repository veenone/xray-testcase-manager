package testrepo

import (
	"fmt"
	"strings"
)

// BugExport carries all data needed to render one bug's rows in the XLSX export:
// the basic cached fields, lazily-fetched detail fields, and the test+run rows.
type BugExport struct {
	// Cached fields (from the bug table).
	Key        string
	ProjectKey string
	IssueType  string
	Status     string
	Priority   string
	Summary    string
	// Live-fetched fields (may be empty when the live fetch failed).
	Description       string
	DefectOrigin      string
	DefectAnalysis    string
	CorrectionDetails string
	Reporter          string
	Severity          string
	// AffectedTests holds the tests linked to this bug.
	AffectedTests []BugTest
	// RunHistory maps each affected test key to its run history entries.
	RunHistory map[string][]TestRunEntry
}

// GetBug returns the cached Bug row for a single key. Returns an error
// wrapping sql.ErrNoRows when the key is not in the local store.
func (r *Repository) GetBug(profileID, key string) (Bug, error) {
	var b Bug
	err := r.db.QueryRow(
		`SELECT jira_key, project_key, issue_type, summary, status, priority, updated_at
		 FROM bug WHERE profile_id = ? AND jira_key = ?`,
		profileID, key,
	).Scan(&b.Key, &b.ProjectKey, &b.IssueType, &b.Summary, &b.Status, &b.Priority, &b.Updated)
	if err != nil {
		return Bug{}, fmt.Errorf("get bug %s: %w", key, err)
	}
	return b, nil
}

// BuildBugExportWorkbook renders exports as a two-sheet XLSX workbook:
//   - "Bugs"        one row per bug with all available fields.
//   - "Run History" one row per (bug, affected test, run entry).
//
// It reuses writeXLSXSheets from exportcsv.go (same package).
func (r *Repository) BuildBugExportWorkbook(exports []BugExport) ([]byte, error) {
	bugsHeader := []string{
		"Key", "Project", "Issue Type", "Status", "Priority", "Severity",
		"Reporter", "Summary", "Description", "Defect Origin", "Defect Analysis",
		"Correction Details", "Affected Test Count",
	}
	runHeader := []string{
		"Bug", "Test", "Test Summary", "Test Project",
		"Execution", "Execution Type", "Parent Key", "Parent Summary",
		"Result", "Fix Version(s)", "Environment", "Run Date",
		"Executed By", "Test Plan(s)", "Defects",
	}

	var bugsRows [][]string
	var runRows [][]string

	for _, ex := range exports {
		bugsRows = append(bugsRows, []string{
			ex.Key,
			ex.ProjectKey,
			ex.IssueType,
			ex.Status,
			ex.Priority,
			ex.Severity,
			ex.Reporter,
			ex.Summary,
			ex.Description,
			ex.DefectOrigin,
			ex.DefectAnalysis,
			ex.CorrectionDetails,
			fmt.Sprintf("%d", len(ex.AffectedTests)),
		})
		for _, bt := range ex.AffectedTests {
			history := ex.RunHistory[bt.Key]
			if len(history) == 0 {
				// No run data yet: emit one row with blank run fields so the
				// test still appears in the Run History sheet.
				runRows = append(runRows, []string{
					ex.Key, bt.Key, bt.Summary, bt.Project,
					"", "", "", "",
					"", "", "", "",
					"", "", "",
				})
				continue
			}
			for _, run := range history {
				runDate := run.FinishedAt
				if runDate == "" {
					runDate = run.StartedAt
				}
				runRows = append(runRows, []string{
					ex.Key,
					bt.Key,
					bt.Summary,
					bt.Project,
					run.ExecKey,
					run.ExecIssueType,
					run.ExecParentKey,
					run.ExecParentSummary,
					run.RunStatus,
					strings.Join(run.FixVersions, ", "),
					run.Environment,
					runDate,
					run.ExecutedBy,
					strings.Join(run.PlanKeys, ", "),
					strings.Join(run.Defects, ", "),
				})
			}
		}
	}

	return writeXLSXSheets([]namedRows{
		{Name: "Bugs", Header: bugsHeader, Rows: bugsRows},
		{Name: "Run History", Header: runHeader, Rows: runRows},
	})
}
