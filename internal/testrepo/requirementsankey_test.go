package testrepo_test

import (
	"testing"
)

func TestRequirementTraceabilityFlowBalances(t *testing.T) {
	repo := seedReqRepo(t)
	// QA-1 (covers PRD-10) is approved; the rest are unreviewed.
	if err := repo.SetTestReview("p1", "QA-1", "approved", "Ana", ""); err != nil {
		t.Fatalf("review: %v", err)
	}

	sk, err := repo.GetRequirementTraceability("p1")
	if err != nil {
		t.Fatalf("traceability: %v", err)
	}

	// Three coverage links (PRD-10 x2, PRD-11 x1) + one uncovered thread (PRD-12).
	const wantTotal = 4
	layerTotal := map[int]int{}
	for _, n := range sk.Nodes {
		layerTotal[n.Layer] += n.Value
	}
	for layer := 0; layer <= 2; layer++ {
		if layerTotal[layer] != wantTotal {
			t.Errorf("layer %d total = %d, want %d (balanced flow)", layer, layerTotal[layer], wantTotal)
		}
	}
}

func TestRequirementTraceabilityNodesAndLinks(t *testing.T) {
	repo := seedReqRepo(t)

	sk, err := repo.GetRequirementTraceability("p1")
	if err != nil {
		t.Fatalf("traceability: %v", err)
	}
	val := map[string]int{}
	for _, n := range sk.Nodes {
		val[n.ID] = n.Value
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
	if val["res:PASS"] != 1 || val["res:FAIL"] != 1 {
		t.Errorf("run results = PASS %d / FAIL %d, want 1 / 1", val["res:PASS"], val["res:FAIL"])
	}
	if val["res:__notest__"] != 1 {
		t.Errorf("res:__notest__ = %d, want 1 (uncovered)", val["res:__notest__"])
	}
	if val["rev:__unreviewed__"] != 3 {
		t.Errorf("rev:__unreviewed__ = %d, want 3 covering tests", val["rev:__unreviewed__"])
	}

	// The uncovered thread links UNCOVERED -> No test -> No review.
	hasUncoveredThread := false
	for _, l := range sk.Links {
		if l.Source == "cov:UNCOVERED" && l.Target == "res:__notest__" {
			hasUncoveredThread = true
		}
	}
	if !hasUncoveredThread {
		t.Errorf("missing UNCOVERED -> No test link; links: %+v", sk.Links)
	}
}
