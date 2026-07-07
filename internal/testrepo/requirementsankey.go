package testrepo

import (
	"fmt"
	"sort"
	"strings"
)

// GetRequirementTraceability builds a six-layer requirement traceability flow:
//
//	requirement(0) -> epic(1) -> coverage(2) -> Test plan(3) -> Test(4) -> run result(5)
//
// When a requirement has no EpicKey the epic layer is skipped and the link
// runs directly from requirement(0) to coverage(2), spanning the empty epic
// column. The flow unit is a coverage link (a requirement<->Test pair); each
// uncovered requirement contributes one synthetic thread ("No plan" / "No
// test") so it still appears in the diagram. Computed entirely from the local
// store.
//
// The requirement is always the labelled first node ("KEY - summary").
// reqFilters narrows the flow to the listed requirement keys; an empty list
// shows every requirement as its own first-layer node.
func (r *Repository) GetRequirementTraceability(profileID string, reqFilters []string) (Sankey, error) {
	out := Sankey{Nodes: []SankeyNode{}, Links: []SankeyLink{}}

	reqs, err := r.ListRequirementsWithCoverage(profileID)
	if err != nil {
		return out, err
	}
	testsByReq, err := r.requirementTestKeys(profileID)
	if err != nil {
		return out, err
	}
	runByTest, err := r.consolidatedRunByTest(profileID)
	if err != nil {
		return out, err
	}
	plansByTest, err := r.testPlanMemberships(profileID)
	if err != nil {
		return out, err
	}
	summaryByKey, err := r.containerSummaries(profileID)
	if err != nil {
		return out, err
	}
	testSummariesMap, err := r.testSummaries(profileID)
	if err != nil {
		return out, err
	}

	filterSet := map[string]bool{}
	for _, k := range reqFilters {
		if s := strings.TrimSpace(k); s != "" {
			filterSet[s] = true
		}
	}

	value := map[string]int{}
	label := map[string]string{}
	layer := map[string]int{}
	// reqEpic: requirement(0) -> epic(1) when EpicKey is set
	reqEpic := map[[2]string]int{}
	// reqCov: requirement(0) -> coverage(2) direct, when EpicKey is empty
	reqCov := map[[2]string]int{}
	// epicCov: epic(1) -> coverage(2) when EpicKey is set
	epicCov := map[[2]string]int{}
	covPlan := map[[2]string]int{}
	planTest := map[[2]string]int{}
	testResult := map[[2]string]int{}

	note := func(id, lbl string, lyr int) {
		value[id]++
		label[id] = lbl
		layer[id] = lyr
	}

	// addThread records one coverage link across all six layers.
	// When epicID == "" the epic layer is bypassed: the req links directly to
	// cov (spanning the empty epic column in the diagram).
	addThread := func(reqID, reqLbl, epicID, epicLbl, covID, covLbl, planID, planLbl, testID, testLbl, resID, resLbl string) {
		note(reqID, reqLbl, 0)
		note(covID, covLbl, 2)
		note(planID, planLbl, 3)
		note(testID, testLbl, 4)
		note(resID, resLbl, 5)
		if epicID != "" {
			note(epicID, epicLbl, 1)
			reqEpic[[2]string{reqID, epicID}]++
			epicCov[[2]string{epicID, covID}]++
		} else {
			reqCov[[2]string{reqID, covID}]++
		}
		covPlan[[2]string{covID, planID}]++
		planTest[[2]string{planID, testID}]++
		testResult[[2]string{testID, resID}]++
	}

	for _, rq := range reqs {
		if len(filterSet) > 0 && !filterSet[rq.Key] {
			continue
		}
		reqID := "req:" + rq.Key
		reqLbl := rq.Key
		if s := strings.TrimSpace(rq.Summary); s != "" {
			reqLbl = rq.Key + " — " + truncateRunes(s, 48)
		}

		// Epic node — present only when the requirement has an EpicKey.
		epicID := ""
		epicLbl := ""
		if rq.EpicKey != "" {
			epicID = "epic:" + rq.EpicKey
			epicLbl = rq.EpicKey
		}

		covID := "cov:" + rq.Coverage
		covLbl := requirementCoverageLabel(rq.Coverage)
		tests := testsByReq[rq.Key]
		if len(tests) == 0 {
			addThread(reqID, reqLbl, epicID, epicLbl, covID, covLbl,
				"plan:__none__", "No plan",
				"test:__none__", "No test",
				"res:__notest__", "No test")
			continue
		}
		for _, tk := range tests {
			planID, planLbl := planBucket(plansByTest[tk], summaryByKey)
			resID, resLbl := runResultNode(runByTest[tk])
			testLbl := tk
			if s := strings.TrimSpace(testSummariesMap[tk]); s != "" {
				testLbl = tk + " — " + truncateRunes(s, 48)
			}
			addThread(reqID, reqLbl, epicID, epicLbl, covID, covLbl,
				planID, planLbl,
				"test:"+tk, testLbl,
				resID, resLbl)
		}
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
	out.Links = append(out.Links, flatten(reqEpic)...)
	out.Links = append(out.Links, flatten(reqCov)...)
	out.Links = append(out.Links, flatten(epicCov)...)
	out.Links = append(out.Links, flatten(covPlan)...)
	out.Links = append(out.Links, flatten(planTest)...)
	out.Links = append(out.Links, flatten(testResult)...)
	return out, nil
}

// testSummaries returns a map of test_case jira_key -> summary for a profile.
// Used to build the Test node label in the requirement traceability diagram.
func (r *Repository) testSummaries(profileID string) (map[string]string, error) {
	rows, err := r.db.Query(
		`SELECT jira_key, summary FROM test_case WHERE profile_id = ?`,
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("read test summaries: %w", err)
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

// truncateRunes shortens s to at most n runes, appending an ellipsis.
func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}

func requirementCoverageLabel(coverage string) string {
	switch coverage {
	case CoveragePassed:
		return "Passed"
	case CoverageFailed:
		return "Failed"
	case CoverageNotRun:
		return "Not run"
	case CoverageUncovered:
		return "Uncovered"
	}
	return coverage
}

// runResultNode maps a Test's consolidated run status to a run-result node. Pass
// and fail are normalised; an empty status means the Test is in no execution;
// any other Xray status keeps its own bucket.
func runResultNode(status string) (id, label string) {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "":
		return "res:__norun__", "Not run"
	case "PASS", "PASSED":
		return "res:PASS", "Pass"
	case "FAIL", "FAILED":
		return "res:FAIL", "Fail"
	default:
		return "res:" + strings.ToUpper(status), status
	}
}
