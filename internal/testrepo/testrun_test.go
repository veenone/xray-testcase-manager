package testrepo_test

import (
	"testing"

	"xray-test-manager/internal/testrepo"
)

func seedRunRepo(t *testing.T) *testrepo.Repository {
	t.Helper()
	repo := newRepo(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1"}, {Key: "QA-2", ID: "2"},
	}); err != nil {
		t.Fatalf("seed tests: %v", err)
	}
	if err := repo.UpsertContainers("p1", []testrepo.Container{
		{Key: "QA-TE-1", Kind: "testexec", Summary: "Cycle 1"},
	}); err != nil {
		t.Fatalf("seed exec: %v", err)
	}
	if err := repo.ReplaceAllContainerLinks("p1", []testrepo.ContainerLink{
		{ContainerKey: "QA-TE-1", TestKey: "QA-1", RunStatus: "TODO"},
	}); err != nil {
		t.Fatalf("seed link: %v", err)
	}
	return repo
}

func TestSetTestRunStatusUpdatesAndQueues(t *testing.T) {
	repo := seedRunRepo(t)

	if err := repo.SetTestRunStatus("p1", "QA-TE-1", "QA-1", "PASS"); err != nil {
		t.Fatalf("set run status: %v", err)
	}

	board, _ := repo.GetContainerBoard("p1", "QA-TE-1")
	var got string
	for _, r := range board.Rows {
		if r.TestKey == "QA-1" {
			got = r.RunStatus
		}
	}
	if got != "PASS" {
		t.Errorf("QA-1 run status = %q, want PASS", got)
	}

	changes, _ := repo.ListPendingChanges("p1")
	if len(changes) != 1 || changes[0].EntityType != "test_run" || changes[0].EntityKey != "QA-TE-1:QA-1" {
		t.Fatalf("pending = %+v, want one test_run for QA-TE-1:QA-1", changes)
	}
}

func TestSetTestRunStatusRejectsNonMember(t *testing.T) {
	repo := seedRunRepo(t)
	if err := repo.SetTestRunStatus("p1", "QA-TE-1", "QA-2", "PASS"); err == nil {
		t.Error("setting run status for a non-member should error")
	}
}

func TestDiscardRunStatusReverts(t *testing.T) {
	repo := seedRunRepo(t)
	if err := repo.SetTestRunStatus("p1", "QA-TE-1", "QA-1", "FAIL"); err != nil {
		t.Fatalf("set: %v", err)
	}
	changes, _ := repo.ListPendingChanges("p1")
	if err := repo.DiscardPendingChange("p1", changes[0].ID); err != nil {
		t.Fatalf("discard: %v", err)
	}
	board, _ := repo.GetContainerBoard("p1", "QA-TE-1")
	for _, r := range board.Rows {
		if r.TestKey == "QA-1" && r.RunStatus != "TODO" {
			t.Errorf("run status = %q after discard, want reverted to TODO", r.RunStatus)
		}
	}
}
