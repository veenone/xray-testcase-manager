package testrepo

import (
	"fmt"
	"strconv"
)

// This file holds the flat-row and flow-row producers that back the
// Traceability XLSX export (RND_P_4TFINT_05-221). Each kind exposes a Flow-rows
// builder (the Sankey edge list with resolved node labels) and a Table-rows
// builder (one flat row per traceability thread), honouring the same filters the
// matching Sankey producer takes.

// flowRowsFromSankey turns a Sankey's links into a resolved edge list: each
// row is {Source label, Target label, Value}, mapping endpoint node ids to their
// node labels so the export is readable without the id scheme.
func flowRowsFromSankey(sk Sankey) [][]string {
	labelByID := make(map[string]string, len(sk.Nodes))
	for _, n := range sk.Nodes {
		labelByID[n.ID] = n.Label
	}
	resolve := func(id string) string {
		if lbl, ok := labelByID[id]; ok {
			return lbl
		}
		return id
	}
	rows := make([][]string, 0, len(sk.Links))
	for _, l := range sk.Links {
		rows = append(rows, []string{resolve(l.Source), resolve(l.Target), strconv.Itoa(l.Value)})
	}
	return rows
}

// ExecutionFlowRows returns the execution Sankey's edge list with resolved
// labels, honouring the same plan/exec/cross-project filters as
// GetTraceabilitySankey.
func (r *Repository) ExecutionFlowRows(profileID, projectKey string, planFilters, execFilters []string, crossProjectOnly bool) ([][]string, error) {
	sk, err := r.GetTraceabilitySankey(profileID, projectKey, planFilters, execFilters, crossProjectOnly)
	if err != nil {
		return nil, err
	}
	return flowRowsFromSankey(sk), nil
}

// ExecutionTableRows returns one flat row per execution thread (a Test's run in
// a Test Execution): Test Plan, Test Execution, Test, Run status. It reuses the
// exact WHERE/filter handling of GetTraceabilitySankey so the table matches the
// diagram. The Test Plan column carries the same single-bucket attribution the
// Sankey uses ("No plan" / "Multiple plans" / the plan label).
func (r *Repository) ExecutionTableRows(profileID, projectKey string, planFilters, execFilters []string, crossProjectOnly bool) ([][]string, error) {
	plansByTest, err := r.testPlanMemberships(profileID)
	if err != nil {
		return nil, err
	}
	summaryByKey, err := r.containerSummaries(profileID)
	if err != nil {
		return nil, err
	}

	q := `SELECT l.container_key, l.test_key, l.run_status
		 FROM test_container_test l
		 JOIN test_container c
		   ON c.profile_id = l.profile_id AND c.jira_key = l.container_key
		 WHERE l.profile_id = ? AND c.kind = 'testexec'`
	args := []any{profileID}
	if execs := nonEmptyKeys(execFilters); len(execs) > 0 {
		q += " AND l.container_key IN (" + sqlPlaceholders(len(execs)) + ")"
		for _, e := range execs {
			args = append(args, e)
		}
	}
	if plans := nonEmptyKeys(planFilters); len(plans) > 0 {
		q += " AND l.test_key IN (SELECT test_key FROM test_container_test" +
			" WHERE profile_id = ? AND container_key IN (" + sqlPlaceholders(len(plans)) + "))"
		args = append(args, profileID)
		for _, p := range plans {
			args = append(args, p)
		}
	}
	q += " ORDER BY l.container_key, l.test_key"

	rows, err := r.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("read execution threads: %w", err)
	}
	defer rows.Close()

	out := [][]string{}
	for rows.Next() {
		var execKey, testKey, runStatus string
		if err := rows.Scan(&execKey, &testKey, &runStatus); err != nil {
			return nil, err
		}
		// Cross-project filter: same predicate as the Sankey producer.
		if crossProjectOnly && projectKey != "" && projectKeyOf(execKey) == projectKey {
			continue
		}
		_, planLabel := planBucket(plansByTest[testKey], summaryByKey)
		out = append(out, []string{
			planLabel,
			orKey(summaryByKey[execKey], execKey),
			testKey,
			runStatus,
		})
	}
	return out, rows.Err()
}

// RequirementFlowRows returns the requirement Sankey's edge list with resolved
// labels, honouring the same requirement filter as GetRequirementTraceability.
func (r *Repository) RequirementFlowRows(profileID string, reqFilters []string) ([][]string, error) {
	sk, err := r.GetRequirementTraceability(profileID, reqFilters)
	if err != nil {
		return nil, err
	}
	return flowRowsFromSankey(sk), nil
}

// RequirementTableRows returns one flat row per requirement coverage thread:
// Requirement, Coverage, Test plan, Test, Run result. It reuses
// ListRequirementsWithCoverage and ListTestsForRequirement so the table tracks
// the same data as the requirement Sankey. Uncovered requirements still produce
// one synthetic row (No plan / No test), matching the diagram.
func (r *Repository) RequirementTableRows(profileID string, reqFilters []string) ([][]string, error) {
	reqs, err := r.ListRequirementsWithCoverage(profileID)
	if err != nil {
		return nil, err
	}
	plansByTest, err := r.testPlanMemberships(profileID)
	if err != nil {
		return nil, err
	}
	summaryByKey, err := r.containerSummaries(profileID)
	if err != nil {
		return nil, err
	}

	filterSet := map[string]bool{}
	for _, k := range nonEmptyKeys(reqFilters) {
		filterSet[k] = true
	}

	out := [][]string{}
	for _, rq := range reqs {
		if len(filterSet) > 0 && !filterSet[rq.Key] {
			continue
		}
		reqLabel := rq.Key
		if rq.Summary != "" {
			reqLabel = rq.Key + " — " + rq.Summary
		}
		covLabel := requirementCoverageLabel(rq.Coverage)
		tests, err := r.ListTestsForRequirement(profileID, rq.Key)
		if err != nil {
			return nil, err
		}
		if len(tests) == 0 {
			out = append(out, []string{reqLabel, covLabel, "No plan", "No test", "Not run"})
			continue
		}
		for _, tk := range tests {
			_, planLabel := planBucket(plansByTest[tk.Key], summaryByKey)
			_, resLabel := runResultNode(tk.RunStatus)
			out = append(out, []string{reqLabel, covLabel, planLabel, tk.Key, resLabel})
		}
	}
	return out, nil
}

// SubTaskFlowRows returns the sub-task Sankey's edge list with resolved labels,
// honouring the same parent filter as GetSubTaskTraceability.
func (r *Repository) SubTaskFlowRows(profileID string, parentFilters []string) ([][]string, error) {
	sk, err := r.GetSubTaskTraceability(profileID, parentFilters)
	if err != nil {
		return nil, err
	}
	return flowRowsFromSankey(sk), nil
}

// SubTaskTableRows returns one flat row per sub-task execution thread: Parent,
// Test Execution, Test, Run status. It reuses the exact WHERE/filter handling of
// GetSubTaskTraceability.
func (r *Repository) SubTaskTableRows(profileID string, parentFilters []string) ([][]string, error) {
	summaryByKey, err := r.containerSummaries(profileID)
	if err != nil {
		return nil, err
	}

	q := `SELECT c.parent_key, l.container_key, l.test_key, l.run_status
		 FROM test_container_test l
		 JOIN test_container c
		   ON c.profile_id = l.profile_id AND c.jira_key = l.container_key
		 WHERE l.profile_id = ? AND c.kind = 'testexec' AND c.parent_key != ''`
	args := []any{profileID}
	if parents := nonEmptyKeys(parentFilters); len(parents) > 0 {
		q += " AND c.parent_key IN (" + sqlPlaceholders(len(parents)) + ")"
		for _, p := range parents {
			args = append(args, p)
		}
	}
	q += " ORDER BY c.parent_key, l.container_key, l.test_key"

	rows, err := r.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("read sub-task threads: %w", err)
	}
	defer rows.Close()

	out := [][]string{}
	for rows.Next() {
		var parentKey, execKey, testKey, runStatus string
		if err := rows.Scan(&parentKey, &execKey, &testKey, &runStatus); err != nil {
			return nil, err
		}
		out = append(out, []string{
			parentKey,
			orKey(summaryByKey[execKey], execKey),
			testKey,
			runStatus,
		})
	}
	return out, rows.Err()
}

// traceabilityKind names the active Traceability tab being exported.
type traceabilityKind string

const (
	kindRequirement traceabilityKind = "requirement"
	kindExecution   traceabilityKind = "execution"
	kindSubTask     traceabilityKind = "subtask"
)

// ExportTraceabilitySheets builds the Flow + Table sheets for the active
// traceability tab and renders them to a single XLSX workbook's bytes. kind is
// "requirement", "execution", or "subtask"; the matching filter slices are used
// (the others are ignored). crossProject is threaded to the execution producer;
// the subtask producer does not yet take it (Task 12 / Feature A3).
func (r *Repository) ExportTraceabilitySheets(profileID, projectKey, kind string, planFilters, execFilters, reqFilters, parentFilters []string, crossProject bool) ([]byte, error) {
	var flow, table [][]string
	var flowHeader, tableHeader []string
	var err error

	switch traceabilityKind(kind) {
	case kindRequirement:
		flowHeader = []string{"Source", "Target", "Value"}
		tableHeader = []string{"Requirement", "Coverage", "Test plan", "Test", "Run result"}
		if flow, err = r.RequirementFlowRows(profileID, reqFilters); err != nil {
			return nil, err
		}
		if table, err = r.RequirementTableRows(profileID, reqFilters); err != nil {
			return nil, err
		}
	case kindExecution:
		flowHeader = []string{"Source", "Target", "Value"}
		tableHeader = []string{"Test Plan", "Test Execution", "Test", "Run status"}
		if flow, err = r.ExecutionFlowRows(profileID, projectKey, planFilters, execFilters, crossProject); err != nil {
			return nil, err
		}
		if table, err = r.ExecutionTableRows(profileID, projectKey, planFilters, execFilters, crossProject); err != nil {
			return nil, err
		}
	case kindSubTask:
		// NOTE(xtm): crossProject is accepted but ignored for sub-tasks until
		// Task 12 (Feature A3) adds cross-project support to the sub-task producer.
		flowHeader = []string{"Source", "Target", "Value"}
		tableHeader = []string{"Parent", "Test Execution", "Test", "Run status"}
		if flow, err = r.SubTaskFlowRows(profileID, parentFilters); err != nil {
			return nil, err
		}
		if table, err = r.SubTaskTableRows(profileID, parentFilters); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown traceability kind %q", kind)
	}

	return writeXLSXSheets([]namedRows{
		{Name: "Flow", Header: flowHeader, Rows: flow},
		{Name: "Table", Header: tableHeader, Rows: table},
	})
}
