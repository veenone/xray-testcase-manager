package testrepo

import "strings"

// RunRollup summarizes run results for a Test Plan or Test Set across the
// executions that ran its member tests.
type RunRollup struct {
	Passed    int `json:"passed"`
	Failed    int `json:"failed"`
	NotRun    int `json:"notRun"`
	Executing int `json:"executing"`
	Aborted   int `json:"aborted"`
	Blocked   int `json:"blocked"`
	Total     int `json:"total"`
	ExecCount int `json:"execCount"`
}

// ExecMemberRun is one member test of an execution with its run details.
type ExecMemberRun struct {
	TestKey     string `json:"testKey"`
	Summary     string `json:"summary"`
	Status      string `json:"status"`
	RunStatus   string `json:"runStatus"`
	StartedAt   string `json:"startedAt"`
	FinishedAt  string `json:"finishedAt"`
	ExecutedBy  string `json:"executedBy"`
	Environment string `json:"environment"`
	// FixVersions are the Jira Fix Version(s) assigned to this member Test
	// issue itself (from test_case.fix_versions), not the execution's fix
	// versions. Empty when the Test has none or has not been synced locally.
	FixVersions []string `json:"fixVersions"`
	// Defects and Comment are staged-over-synced: when a test_run_defect /
	// test_run_comment pending change exists for this run, the local
	// (test_container_test) value wins; otherwise the Xray-synced test_run
	// value is used. See GetExecutionMembersWithRuns.
	Defects []string `json:"defects"`
	Comment string   `json:"comment"`
}

// GetRunRollup returns a run-status rollup for the member tests of a Test Plan
// or Test Set. Each member's consolidated run status across all executions
// (worst-wins, reusing consolidateRunStatus) is bucketed into the result.
// Total is the number of member tests; ExecCount is the number of distinct
// Test Executions that share at least one member test with this container.
func (r *Repository) GetRunRollup(profileID, containerKey string) (RunRollup, error) {
	var roll RunRollup

	// Collect the direct member keys for this container.
	memberRows, err := r.db.Query(
		`SELECT test_key FROM test_container_test
		 WHERE profile_id = ? AND container_key = ?`,
		profileID, containerKey)
	if err != nil {
		return roll, err
	}
	defer memberRows.Close()
	members := map[string]bool{}
	for memberRows.Next() {
		var k string
		if err := memberRows.Scan(&k); err != nil {
			return roll, err
		}
		members[k] = true
	}
	if err := memberRows.Err(); err != nil {
		return roll, err
	}
	roll.Total = len(members)
	if roll.Total == 0 {
		return roll, nil
	}

	// Gather run statuses across all executions for each member, and count
	// distinct executions that share at least one member.
	runsByTest := map[string][]string{}
	execsSeen := map[string]bool{}

	execRows, err := r.db.Query(
		`SELECT l.test_key, l.run_status, l.container_key
		 FROM test_container_test l
		 JOIN test_container c
		   ON c.profile_id = l.profile_id AND c.jira_key = l.container_key
		 WHERE l.profile_id = ? AND c.kind = 'testexec'`,
		profileID)
	if err != nil {
		return roll, err
	}
	defer execRows.Close()
	for execRows.Next() {
		var testKey, runStatus, execKey string
		if err := execRows.Scan(&testKey, &runStatus, &execKey); err != nil {
			return roll, err
		}
		if members[testKey] {
			runsByTest[testKey] = append(runsByTest[testKey], runStatus)
			execsSeen[execKey] = true
		}
	}
	if err := execRows.Err(); err != nil {
		return roll, err
	}
	roll.ExecCount = len(execsSeen)

	// Bucket each member's consolidated run status.
	for k := range members {
		status := consolidateRunStatus(runsByTest[k])
		switch strings.ToUpper(status) {
		case "PASS", "PASSED":
			roll.Passed++
		case "FAIL", "FAILED":
			roll.Failed++
		case "EXECUTING":
			roll.Executing++
		case "ABORTED":
			roll.Aborted++
		case "BLOCKED":
			roll.Blocked++
		default:
			// "", "TODO", or any unrecognised status counts as not run.
			roll.NotRun++
		}
	}
	return roll, nil
}

// GetExecutionMembersWithRuns returns the member tests of a Test Execution
// enriched with run details from the test_run table. Summary and status are
// resolved from the local test_case cache, falling back to the external_test
// cache for cross-project members. RunStatus prefers the test_run row, falling
// back to the test_container_test membership run_status.
//
// Defects and Comment are staged-over-synced, chosen by the PRESENCE of a
// test_run_defect / test_run_comment pending_change row for this run (LEFT
// JOINed on entity_key = container_key||':'||test_key), not by whether
// l.run_defects/l.run_comment happen to be non-empty — a staged empty defect
// set ("[]") or a cleared comment ("") must still read back as the local
// value, and the column alone can't tell "cleared" apart from "never edited"
// for a comment.
func (r *Repository) GetExecutionMembersWithRuns(profileID, execKey string) ([]ExecMemberRun, error) {
	rows, err := r.db.Query(
		`SELECT l.test_key,
		        COALESCE(t.summary, x.summary, '') AS summary,
		        COALESCE(t.status,  x.status,  '') AS status,
		        COALESCE(tr.run_status, l.run_status, '') AS run_status,
		        COALESCE(tr.started_at,   '') AS started_at,
		        COALESCE(tr.finished_at,  '') AS finished_at,
		        COALESCE(tr.executed_by,  '') AS executed_by,
		        COALESCE(tr.environment,  '') AS environment,
		        COALESCE(t.fix_versions,  '') AS fix_versions,
		        CASE WHEN pd.entity_key IS NOT NULL THEN l.run_defects ELSE COALESCE(tr.defects, '') END AS defects,
		        CASE WHEN pc.entity_key IS NOT NULL THEN l.run_comment ELSE COALESCE(tr.comment, '') END AS comment
		 FROM test_container_test l
		 LEFT JOIN test_case     t  ON t.profile_id  = l.profile_id AND t.jira_key = l.test_key
		 LEFT JOIN external_test x  ON x.profile_id  = l.profile_id AND x.jira_key = l.test_key
		 LEFT JOIN test_run      tr ON tr.profile_id = l.profile_id
		                           AND tr.exec_key   = l.container_key
		                           AND tr.test_key   = l.test_key
		 LEFT JOIN pending_change pd ON pd.profile_id  = l.profile_id
		                            AND pd.entity_type = ?
		                            AND pd.entity_key  = l.container_key || ':' || l.test_key
		                            AND pd.field       = 'run_defects'
		 LEFT JOIN pending_change pc ON pc.profile_id  = l.profile_id
		                            AND pc.entity_type = ?
		                            AND pc.entity_key  = l.container_key || ':' || l.test_key
		                            AND pc.field       = 'run_comment'
		 WHERE l.profile_id = ? AND l.container_key = ?
		 ORDER BY l.test_key`,
		entityTestRunDefect, entityTestRunComment, profileID, execKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ExecMemberRun
	for rows.Next() {
		var m ExecMemberRun
		var fixVersionsJSON, defectsJSON string
		if err := rows.Scan(
			&m.TestKey, &m.Summary, &m.Status,
			&m.RunStatus, &m.StartedAt, &m.FinishedAt,
			&m.ExecutedBy, &m.Environment, &fixVersionsJSON,
			&defectsJSON, &m.Comment,
		); err != nil {
			return nil, err
		}
		m.FixVersions = decodeFixVersions(fixVersionsJSON)
		m.Defects = decodeFixVersions(defectsJSON)
		out = append(out, m)
	}
	return out, rows.Err()
}

// TestRunEntry is one execution-run of a test, with the execution's context.
// CreatedAt and UpdatedAt carry the run's creation and last-update timestamps
// from Xray (ISO-8601, empty when unknown) and drive sort order.
type TestRunEntry struct {
	ExecKey     string   `json:"execKey"`
	ExecSummary string   `json:"execSummary"`
	PlanKeys    []string `json:"planKeys"`
	Environment string   `json:"environment"`
	FixVersions []string `json:"fixVersions"`
	RunStatus   string   `json:"runStatus"`
	StartedAt   string   `json:"startedAt"`
	FinishedAt  string   `json:"finishedAt"`
	ExecutedBy  string   `json:"executedBy"`
	Defects     []string `json:"defects"`
	CreatedAt   string   `json:"createdAt"`
	UpdatedAt   string   `json:"updatedAt"`
	// ExecIssueType is the execution's Jira issue type (e.g. "Test Execution" or
	// "Sub Test Execution"), and ExecParentKey / ExecParentSummary identify the
	// parent issue of a sub-task Test Execution (both empty for a standalone
	// execution). They let the run-history breakdown distinguish sub-task
	// executions and link to their parent.
	ExecIssueType     string `json:"execIssueType"`
	ExecParentKey     string `json:"execParentKey"`
	ExecParentSummary string `json:"execParentSummary"`
	// ExecCreated, ExecUpdated, and ExecResolved are the ISO-8601 timestamps
	// from the Test Execution issue itself (Jira created/updated/resolutiondate
	// fields), distinct from the run's own started_at/finished_at. Empty when
	// not yet synced or for non-execution containers.
	ExecCreated  string `json:"execCreated"`
	ExecUpdated  string `json:"execUpdated"`
	ExecResolved string `json:"execResolved"`
}

// GetTestRunHistory returns every execution-run of a test, sorted by
// updated_at descending (then finished_at, then exec_key for a stable
// secondary order), enriched with the execution summary, fix versions, and
// associated Test Plans from the local cache.
func (r *Repository) GetTestRunHistory(profileID, testKey string) ([]TestRunEntry, error) {
	rows, err := r.db.Query(`
		SELECT tr.exec_key,
		       COALESCE(c.summary, ''),
		       tr.run_status,
		       tr.started_at,
		       tr.finished_at,
		       tr.executed_by,
		       tr.environment,
		       tr.defects,
		       COALESCE(c.fix_versions, ''),
		       tr.created_at,
		       tr.updated_at,
		       COALESCE(c.issue_type, ''),
		       COALESCE(c.parent_key, ''),
		       COALESCE(c.parent_summary, ''),
		       COALESCE(c.created, ''),
		       COALESCE(c.updated, ''),
		       COALESCE(c.resolved, '')
		FROM test_run tr
		LEFT JOIN test_container c
		       ON c.profile_id = tr.profile_id AND c.jira_key = tr.exec_key
		WHERE tr.profile_id = ? AND tr.test_key = ?
		ORDER BY tr.updated_at DESC, tr.finished_at DESC, tr.exec_key`,
		profileID, testKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TestRunEntry
	for rows.Next() {
		var e TestRunEntry
		var defectsJSON, fixJSON string
		if err := rows.Scan(
			&e.ExecKey, &e.ExecSummary, &e.RunStatus,
			&e.StartedAt, &e.FinishedAt, &e.ExecutedBy, &e.Environment,
			&defectsJSON, &fixJSON,
			&e.CreatedAt, &e.UpdatedAt,
			&e.ExecIssueType, &e.ExecParentKey, &e.ExecParentSummary,
			&e.ExecCreated, &e.ExecUpdated, &e.ExecResolved,
		); err != nil {
			return nil, err
		}
		// decodeFixVersions reuses decodeEnvironments: returns [] for "" or malformed JSON.
		e.Defects = decodeFixVersions(defectsJSON)
		e.FixVersions = decodeFixVersions(fixJSON)
		// ExecPlansForExec is already defined in testrun.go; reuse it.
		plans, _ := r.ExecPlansForExec(profileID, e.ExecKey)
		if plans == nil {
			plans = []string{}
		}
		e.PlanKeys = plans
		out = append(out, e)
	}
	return out, rows.Err()
}
