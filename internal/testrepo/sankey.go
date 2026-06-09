package testrepo

import (
	"fmt"
	"sort"
)

// SankeyNode is one node in the traceability flow (FR-9). Layer 0 = Test Plan
// bucket, 1 = Test Execution, 2 = run status.
type SankeyNode struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Layer int    `json:"layer"`
	Value int    `json:"value"`
}

// SankeyLink is a weighted flow between two nodes.
type SankeyLink struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Value  int    `json:"value"`
}

// Sankey is the traceability graph: Test Plan -> Test Execution -> run status.
type Sankey struct {
	Nodes []SankeyNode `json:"nodes"`
	Links []SankeyLink `json:"links"`
}

// GetTraceabilitySankey builds a Plan -> Execution -> Status flow (FR-9). The
// flow unit is a Test Run (a Test's membership in a Test Execution); each run
// is attributed to a single Plan bucket — the Plan if the Test belongs to
// exactly one, "Multiple plans" if more, "No plan" if none. Because each run is
// counted once per layer, all three layers sum to the same total, so the
// diagram balances. Computed entirely from the local store.
//
// planFilter / execFilter narrow the flow: a plan filter restricts to runs of
// Tests in that Plan (and collapses layer 0 to that single Plan); an execution
// filter restricts to that one Execution. Either may be "" for "all".
func (r *Repository) GetTraceabilitySankey(profileID, planFilter, execFilter string) (Sankey, error) {
	out := Sankey{Nodes: []SankeyNode{}, Links: []SankeyLink{}}

	plansByTest, err := r.testPlanMemberships(profileID)
	if err != nil {
		return out, err
	}
	summaryByKey, err := r.containerSummaries(profileID)
	if err != nil {
		return out, err
	}

	q := `SELECT l.container_key, l.test_key, l.run_status
		 FROM test_container_test l
		 JOIN test_container c
		   ON c.profile_id = l.profile_id AND c.jira_key = l.container_key
		 WHERE l.profile_id = ? AND c.kind = 'testexec'`
	args := []any{profileID}
	if execFilter != "" {
		q += " AND l.container_key = ?"
		args = append(args, execFilter)
	}
	if planFilter != "" {
		q += ` AND l.test_key IN (
			SELECT test_key FROM test_container_test
			WHERE profile_id = ? AND container_key = ?)`
		args = append(args, profileID, planFilter)
	}
	execRows, err := r.db.Query(q, args...)
	if err != nil {
		return out, fmt.Errorf("read execution runs: %w", err)
	}
	defer execRows.Close()

	planExec := map[[2]string]int{}
	execStatus := map[[2]string]int{}
	value := map[string]int{}
	label := map[string]string{}
	layer := map[string]int{}

	note := func(id, lbl string, lyr, add int) {
		value[id] += add
		label[id] = lbl
		layer[id] = lyr
	}

	for execRows.Next() {
		var execKey, testKey, runStatus string
		if err := execRows.Scan(&execKey, &testKey, &runStatus); err != nil {
			return out, err
		}

		var planID, planLabel string
		if planFilter != "" {
			planID, planLabel = "plan:"+planFilter, orKey(summaryByKey[planFilter], planFilter)
		} else {
			planID, planLabel = planBucket(plansByTest[testKey], summaryByKey)
		}
		execID := "exec:" + execKey
		execLabel := orKey(summaryByKey[execKey], execKey)
		status := runStatus
		if status == "" {
			status = "(none)"
		}
		statusID := "status:" + status

		note(planID, planLabel, 0, 1)
		note(execID, execLabel, 1, 1)
		note(statusID, status, 2, 1)
		planExec[[2]string{planID, execID}]++
		execStatus[[2]string{execID, statusID}]++
	}
	if err := execRows.Err(); err != nil {
		return out, err
	}

	for id, lbl := range label {
		out.Nodes = append(out.Nodes, SankeyNode{ID: id, Label: lbl, Layer: layer[id], Value: value[id]})
	}
	sort.Slice(out.Nodes, func(i, j int) bool {
		if out.Nodes[i].Layer != out.Nodes[j].Layer {
			return out.Nodes[i].Layer < out.Nodes[j].Layer
		}
		if out.Nodes[i].Value != out.Nodes[j].Value {
			return out.Nodes[i].Value > out.Nodes[j].Value
		}
		return out.Nodes[i].ID < out.Nodes[j].ID
	})

	out.Links = append(out.Links, flatten(planExec)...)
	out.Links = append(out.Links, flatten(execStatus)...)
	return out, nil
}

func (r *Repository) testPlanMemberships(profileID string) (map[string][]string, error) {
	rows, err := r.db.Query(
		`SELECT l.test_key, l.container_key
		 FROM test_container_test l
		 JOIN test_container c
		   ON c.profile_id = l.profile_id AND c.jira_key = l.container_key
		 WHERE l.profile_id = ? AND c.kind = 'testplan'`,
		profileID)
	if err != nil {
		return nil, fmt.Errorf("read plan memberships: %w", err)
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var testKey, planKey string
		if err := rows.Scan(&testKey, &planKey); err != nil {
			return nil, err
		}
		out[testKey] = append(out[testKey], planKey)
	}
	return out, rows.Err()
}

func (r *Repository) containerSummaries(profileID string) (map[string]string, error) {
	rows, err := r.db.Query(
		`SELECT jira_key, summary FROM test_container
		 WHERE profile_id = ? AND kind IN ('testplan', 'testexec')`,
		profileID)
	if err != nil {
		return nil, fmt.Errorf("read container summaries: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var key, summary string
		if err := rows.Scan(&key, &summary); err != nil {
			return nil, err
		}
		out[key] = summary
	}
	return out, rows.Err()
}

// planBucket attributes a Test (by its plan memberships) to a single layer-0
// node so each run is counted once.
func planBucket(plans []string, summaryByKey map[string]string) (id, label string) {
	switch len(plans) {
	case 0:
		return "plan:__none__", "No plan"
	case 1:
		return "plan:" + plans[0], orKey(summaryByKey[plans[0]], plans[0])
	default:
		return "plan:__multi__", "Multiple plans"
	}
}

func flatten(m map[[2]string]int) []SankeyLink {
	links := make([]SankeyLink, 0, len(m))
	for pair, v := range m {
		links = append(links, SankeyLink{Source: pair[0], Target: pair[1], Value: v})
	}
	sort.Slice(links, func(i, j int) bool {
		if links[i].Source != links[j].Source {
			return links[i].Source < links[j].Source
		}
		return links[i].Target < links[j].Target
	})
	return links
}

func orKey(summary, key string) string {
	if summary == "" {
		return key
	}
	return key + " — " + summary
}
