package testrepo_test

import (
	"testing"

	"xray-test-manager/internal/testrepo"
)

func TestTraceabilitySankeyBalancesAcrossLayers(t *testing.T) {
	repo := newRepo(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1"}, {Key: "QA-2", ID: "2"}, {Key: "QA-3", ID: "3"},
	}); err != nil {
		t.Fatalf("seed tests: %v", err)
	}
	if err := repo.UpsertContainers("p1", []testrepo.Container{
		{Key: "QA-TP-1", Kind: "testplan", Summary: "Plan A"},
		{Key: "QA-TE-1", Kind: "testexec", Summary: "Cycle 1"},
	}); err != nil {
		t.Fatalf("seed containers: %v", err)
	}
	// QA-1, QA-2 are in Plan A; QA-3 is in no plan. All three run in Cycle 1.
	if err := repo.ReplaceAllContainerLinks("p1", []testrepo.ContainerLink{
		{ContainerKey: "QA-TP-1", TestKey: "QA-1"},
		{ContainerKey: "QA-TP-1", TestKey: "QA-2"},
		{ContainerKey: "QA-TE-1", TestKey: "QA-1", RunStatus: "PASS"},
		{ContainerKey: "QA-TE-1", TestKey: "QA-2", RunStatus: "FAIL"},
		{ContainerKey: "QA-TE-1", TestKey: "QA-3", RunStatus: "PASS"},
	}); err != nil {
		t.Fatalf("seed links: %v", err)
	}

	s, err := repo.GetTraceabilitySankey("p1", "QA", nil, nil, false)
	if err != nil {
		t.Fatalf("sankey: %v", err)
	}

	// Three runs total; each layer should sum to 3.
	sumLayer := func(l int) int {
		total := 0
		for _, n := range s.Nodes {
			if n.Layer == l {
				total += n.Value
			}
		}
		return total
	}
	for l := 0; l < 3; l++ {
		if got := sumLayer(l); got != 3 {
			t.Errorf("layer %d sums to %d, want 3 (balanced)", l, got)
		}
	}

	// Plan bucket "No plan" should carry exactly QA-3's single run.
	var noPlan int
	for _, n := range s.Nodes {
		if n.ID == "plan:__none__" {
			noPlan = n.Value
		}
	}
	if noPlan != 1 {
		t.Errorf("No-plan bucket = %d, want 1 (QA-3)", noPlan)
	}
}

func TestTraceabilitySankeyEmptyWhenNoExecutions(t *testing.T) {
	repo := newRepo(t)
	s, err := repo.GetTraceabilitySankey("p1", "QA", nil, nil, false)
	if err != nil {
		t.Fatalf("sankey: %v", err)
	}
	if len(s.Nodes) != 0 || len(s.Links) != 0 {
		t.Errorf("expected empty graph, got %d nodes / %d links", len(s.Nodes), len(s.Links))
	}
}

func TestTraceabilitySankeyCrossProjectFilter(t *testing.T) {
	repo := newRepo(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{{Key: "QA-1", ID: "1"}}); err != nil {
		t.Fatalf("seed tests: %v", err)
	}
	if err := repo.UpsertContainers("p1", []testrepo.Container{
		{Key: "QA-TP-1", Kind: "testplan", Summary: "Plan A"},
		{Key: "QA-TE-1", Kind: "testexec", Summary: "Same project"},
		{Key: "OTHER-TE-9", Kind: "testexec", Summary: "Other project"},
	}); err != nil {
		t.Fatalf("seed containers: %v", err)
	}
	if err := repo.ReplaceAllContainerLinks("p1", []testrepo.ContainerLink{
		{ContainerKey: "QA-TP-1", TestKey: "QA-1"},
		{ContainerKey: "QA-TE-1", TestKey: "QA-1", RunStatus: "PASS"},
		{ContainerKey: "OTHER-TE-9", TestKey: "QA-1", RunStatus: "FAIL"},
	}); err != nil {
		t.Fatalf("seed links: %v", err)
	}

	has := func(s testrepo.Sankey, id string) bool {
		for _, n := range s.Nodes {
			if n.ID == id {
				return true
			}
		}
		return false
	}

	all, err := repo.GetTraceabilitySankey("p1", "QA", nil, nil, false)
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if !has(all, "exec:QA-TE-1") || !has(all, "exec:OTHER-TE-9") {
		t.Fatalf("unfiltered should include both executions; nodes=%+v", all.Nodes)
	}

	cross, err := repo.GetTraceabilitySankey("p1", "QA", nil, nil, true)
	if err != nil {
		t.Fatalf("cross: %v", err)
	}
	if has(cross, "exec:QA-TE-1") {
		t.Errorf("cross-project filter should drop the same-project exec QA-TE-1")
	}
	if !has(cross, "exec:OTHER-TE-9") {
		t.Errorf("cross-project filter should keep OTHER-TE-9; nodes=%+v", cross.Nodes)
	}
}
