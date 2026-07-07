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
	// Six layers: requirement(0) -> epic(1, optional) -> coverage(2) ->
	// Test plan(3) -> Test(4) -> run result(5).
	// Layer 1 (epic) is empty here because the seeded requirements have no
	// EpicKey; balance checks skip that layer.
	for _, layer := range []int{0, 2, 3, 4, 5} {
		if layerTotal[layer] != wantTotal {
			t.Errorf("layer %d total = %d, want %d (balanced flow)", layer, layerTotal[layer], wantTotal)
		}
	}
	if layerTotal[1] != 0 {
		t.Errorf("layer 1 (epic) total = %d, want 0 (no EpicKey in seed)", layerTotal[1])
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

	// Every requirement is its own layer-0 node ("KEY — summary").
	for _, key := range []string{"PRD-10", "PRD-11", "PRD-12"} {
		if layerOf["req:"+key] != 0 {
			t.Errorf("req:%s missing from layer 0; nodes: %+v", key, sk.Nodes)
		}
	}
	if val["req:PRD-10"] != 2 {
		t.Errorf("req:PRD-10 = %d, want 2 covering tests", val["req:PRD-10"])
	}

	// Coverage nodes at layer 2.
	if val["cov:FAILED"] != 2 {
		t.Errorf("cov:FAILED = %d, want 2", val["cov:FAILED"])
	}
	if val["cov:NOTRUN"] != 1 {
		t.Errorf("cov:NOTRUN = %d, want 1", val["cov:NOTRUN"])
	}
	if val["cov:UNCOVERED"] != 1 {
		t.Errorf("cov:UNCOVERED = %d, want 1", val["cov:UNCOVERED"])
	}
	for _, covID := range []string{"cov:FAILED", "cov:NOTRUN", "cov:UNCOVERED"} {
		if layerOf[covID] != 2 {
			t.Errorf("%s layer = %d, want 2", covID, layerOf[covID])
		}
	}

	// No test plans seeded; every thread buckets to "No plan" at layer 3.
	if val["plan:__none__"] != 4 {
		t.Errorf("plan:__none__ = %d, want 4 (no plans seeded)", val["plan:__none__"])
	}
	if layerOf["plan:__none__"] != 3 {
		t.Errorf("plan:__none__ layer = %d, want 3", layerOf["plan:__none__"])
	}

	// Test nodes at layer 4: QA-1, QA-2, QA-3 covered; synthetic "No test" for PRD-12.
	for _, tk := range []string{"QA-1", "QA-2", "QA-3"} {
		if layerOf["test:"+tk] != 4 {
			t.Errorf("test:%s layer = %d, want 4", tk, layerOf["test:"+tk])
		}
	}
	if layerOf["test:__none__"] != 4 {
		t.Errorf("test:__none__ layer = %d, want 4 (uncovered)", layerOf["test:__none__"])
	}

	// Run results at layer 5.
	if val["res:PASS"] != 1 || val["res:FAIL"] != 1 {
		t.Errorf("run results = PASS %d / FAIL %d, want 1 / 1", val["res:PASS"], val["res:FAIL"])
	}
	if val["res:__notest__"] != 1 {
		t.Errorf("res:__notest__ = %d, want 1 (uncovered)", val["res:__notest__"])
	}
	if layerOf["res:PASS"] != 5 {
		t.Errorf("res:PASS layer = %d, want 5", layerOf["res:PASS"])
	}

	// The uncovered thread: UNCOVERED -> No plan -> No test (test:__none__) -> No test (res).
	hasCovPlan, hasPlanTest, hasTestRes := false, false, false
	for _, l := range sk.Links {
		if l.Source == "cov:UNCOVERED" && l.Target == "plan:__none__" {
			hasCovPlan = true
		}
		if l.Source == "plan:__none__" && l.Target == "test:__none__" {
			hasPlanTest = true
		}
		if l.Source == "test:__none__" && l.Target == "res:__notest__" {
			hasTestRes = true
		}
	}
	if !hasCovPlan || !hasPlanTest || !hasTestRes {
		t.Errorf("missing UNCOVERED -> No plan -> No test thread; links: %+v", sk.Links)
	}
}
