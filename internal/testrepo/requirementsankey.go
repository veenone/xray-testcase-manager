package testrepo

import (
	"sort"
	"strings"
)

// GetRequirementTraceability builds a four-layer requirement traceability flow:
// requirement -> coverage status -> Test plan -> covering Test run result. The
// flow unit is a coverage link (a requirement<->Test pair); each uncovered
// requirement contributes one synthetic thread ("No plan" / "No test") so it
// still appears in the diagram. Because every link is counted once per layer,
// the layers sum to the same total and the diagram balances. Computed entirely
// from the local store, so it tracks the cache without a Jira call.
//
// The requirement is always the labelled first node ("KEY — summary"). reqFilter
// narrows the flow to a single requirement (by key); an empty reqFilter shows
// every requirement as its own first-layer node.
func (r *Repository) GetRequirementTraceability(profileID, reqFilter string) (Sankey, error) {
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

	reqFilter = strings.TrimSpace(reqFilter)
	filtered := reqFilter != ""

	value := map[string]int{}
	label := map[string]string{}
	layer := map[string]int{}
	reqCov := map[[2]string]int{}
	covPlan := map[[2]string]int{}
	planResult := map[[2]string]int{}

	note := func(id, lbl string, lyr int) {
		value[id]++
		label[id] = lbl
		layer[id] = lyr
	}

	// addThread records one coverage link across all four layers: requirement
	// (0) -> coverage (1) -> Test plan (2) -> run result (3).
	addThread := func(reqID, reqLbl, covID, covLbl, planID, planLbl, resID, resLbl string) {
		note(reqID, reqLbl, 0)
		note(covID, covLbl, 1)
		note(planID, planLbl, 2)
		note(resID, resLbl, 3)
		reqCov[[2]string{reqID, covID}]++
		covPlan[[2]string{covID, planID}]++
		planResult[[2]string{planID, resID}]++
	}

	for _, rq := range reqs {
		if filtered && rq.Key != reqFilter {
			continue
		}
		reqID := "req:" + rq.Key
		reqLbl := rq.Key
		if s := strings.TrimSpace(rq.Summary); s != "" {
			reqLbl = rq.Key + " — " + truncateRunes(s, 48)
		}
		covID := "cov:" + rq.Coverage
		covLbl := requirementCoverageLabel(rq.Coverage)
		tests := testsByReq[rq.Key]
		if len(tests) == 0 {
			addThread(reqID, reqLbl, covID, covLbl, "plan:__none__", "No plan", "res:__notest__", "No test")
			continue
		}
		for _, tk := range tests {
			planID, planLbl := planBucket(plansByTest[tk], summaryByKey)
			resID, resLbl := runResultNode(runByTest[tk])
			addThread(reqID, reqLbl, covID, covLbl, planID, planLbl, resID, resLbl)
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
	out.Links = append(out.Links, flatten(reqCov)...)
	out.Links = append(out.Links, flatten(covPlan)...)
	out.Links = append(out.Links, flatten(planResult)...)
	return out, nil
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
