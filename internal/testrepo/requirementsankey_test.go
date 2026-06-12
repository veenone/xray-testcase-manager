package testrepo_test

import (
	"testing"
)

func TestRequirementTraceabilityFlowBalances(t *testing.T) {
	repo := seedReqRepo(t)

	sk, err := repo.GetRequirementTraceability("p1", nil)
	if err != nil {
		t.Fatalf("traceability: %v", err)
	}

	// Three coverage links (PRD-10 x2, PRD-11 x1) + one uncovered thread (PRD-12).
	const wantTotal = 4
	layerTotal := map[int]int{}
	for _, n := range sk.Nodes {
		layerTotal[n.Layer] += n.Value
	}
	// Four layers: requirement -> coverage -> Test plan -> run result.
	for layer := 0; layer <= 3; layer++ {
		if layerTotal[layer] != wantTotal {
			t.Errorf("layer %d total = %d, want %d (balanced flow)", layer, layerTotal[layer], wantTotal)
		}
	}
}

func TestRequirementTraceabilityNodesAndLinks(t *testing.T) {
	repo := seedReqRepo(t)

	sk, err := repo.GetRequirementTraceability("p1", nil)
	if err != nil {
		t.Fatalf("traceability: %v", err)
	}
	val := map[string]int{}
	layerOf := map[string]int{}
	for _, n := range sk.Nodes {
		val[n.ID] = n.Value
		layerOf[n.ID] = n.Layer
	}

	// Every requirement is its own first-layer node ("KEY — summary").
	for _, key := range []string{"PRD-10", "PRD-11", "PRD-12"} {
		if layerOf["req:"+key] != 0 {
			t.Errorf("req:%s missing from layer 0; nodes: %+v", key, sk.Nodes)
		}
	}
	if val["req:PRD-10"] != 2 {
		t.Errorf("req:PRD-10 = %d, want 2 covering tests", val["req:PRD-10"])
	}

	// PRD-10 is FAILED and covered by QA-1 (PASS) + QA-2 (FAIL); PRD-11 NOTRUN
	// (QA-3 TODO); PRD-12 UNCOVERED.
	if val["cov:FAILED"] != 2 {
		t.Errorf("cov:FAILED = %d, want 2", val["cov:FAILED"])
	}
	if val["cov:NOTRUN"] != 1 {
		t.Errorf("cov:NOTRUN = %d, want 1", val["cov:NOTRUN"])
	}
	if val["cov:UNCOVERED"] != 1 {
		t.Errorf("cov:UNCOVERED = %d, want 1", val["cov:UNCOVERED"])
	}
	// No test plans seeded, so every thread buckets to "No plan".
	if val["plan:__none__"] != 4 {
		t.Errorf("plan:__none__ = %d, want 4 (no plans seeded)", val["plan:__none__"])
	}
	if val["res:PASS"] != 1 || val["res:FAIL"] != 1 {
		t.Errorf("run results = PASS %d / FAIL %d, want 1 / 1", val["res:PASS"], val["res:FAIL"])
	}
	if val["res:__notest__"] != 1 {
		t.Errorf("res:__notest__ = %d, want 1 (uncovered)", val["res:__notest__"])
	}

	// The uncovered thread links UNCOVERED -> No plan -> No test.
	hasCovPlan, hasPlanRes := false, false
	for _, l := range sk.Links {
		if l.Source == "cov:UNCOVERED" && l.Target == "plan:__none__" {
			hasCovPlan = true
		}
		if l.Source == "plan:__none__" && l.Target == "res:__notest__" {
			hasPlanRes = true
		}
	}
	if !hasCovPlan || !hasPlanRes {
		t.Errorf("missing UNCOVERED -> No plan -> No test thread; links: %+v", sk.Links)
	}
}
