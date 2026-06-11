package testrepo

import (
	"sort"
	"strings"
)

// GetRequirementTraceability builds a requirement sign-off traceability flow:
// requirement coverage -> covering Test run result -> Test review sign-off. The
// flow unit is a coverage link (a requirement<->Test pair); each uncovered
// requirement contributes one synthetic thread ("No test" / "No review") so it
// still appears in the diagram. Because every link is counted once per layer,
// all three layers sum to the same total and the diagram balances. Computed
// entirely from the local store, so it tracks the cache without a Jira call.
func (r *Repository) GetRequirementTraceability(profileID string) (Sankey, error) {
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
	reviews, err := r.allReviews(profileID)
	if err != nil {
		return out, err
	}

	value := map[string]int{}
	label := map[string]string{}
	layer := map[string]int{}
	covResult := map[[2]string]int{}
	resultReview := map[[2]string]int{}

	note := func(id, lbl string, lyr int) {
		value[id]++
		label[id] = lbl
		layer[id] = lyr
	}

	link := func(covID, resID, revID, covLbl, resLbl, revLbl string) {
		note(covID, covLbl, 0)
		note(resID, resLbl, 1)
		note(revID, revLbl, 2)
		covResult[[2]string{covID, resID}]++
		resultReview[[2]string{resID, revID}]++
	}

	for _, rq := range reqs {
		covID := "cov:" + rq.Coverage
		covLbl := requirementCoverageLabel(rq.Coverage)
		tests := testsByReq[rq.Key]
		if len(tests) == 0 {
			link(covID, "res:__notest__", "rev:__notest__", covLbl, "No test", "No review")
			continue
		}
		for _, tk := range tests {
			resID, resLbl := runResultNode(runByTest[tk])
			revID, revLbl := reviewNode(reviews[tk].Verdict)
			link(covID, resID, revID, covLbl, resLbl, revLbl)
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
	out.Links = append(out.Links, flatten(covResult)...)
	out.Links = append(out.Links, flatten(resultReview)...)
	return out, nil
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

// runResultNode maps a Test's consolidated run status to a layer-1 node. Pass
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

// reviewNode maps a Test's review verdict to a layer-2 sign-off node. Anything
// without a recorded verdict is "Unreviewed".
func reviewNode(verdict string) (id, label string) {
	switch strings.ToLower(strings.TrimSpace(verdict)) {
	case "approved":
		return "rev:approved", "Approved"
	case "rejected":
		return "rev:rejected", "Rejected"
	case "pending":
		return "rev:pending", "Pending"
	default:
		return "rev:__unreviewed__", "Unreviewed"
	}
}
