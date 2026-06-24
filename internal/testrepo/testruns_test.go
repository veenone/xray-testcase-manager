package testrepo_test

import (
	"testing"

	"xray-test-manager/internal/testrepo"
)

// seedRollupRepo sets up a Test Plan (DEMO-TP-1) with two member tests, two
// Test Executions that ran those tests, and test_run rows with varied statuses.
func seedRollupRepo(t *testing.T) *testrepo.Repository {
	t.Helper()
	repo := newRepo(t)

	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "DEMO-1", ID: "1", Summary: "Login test"},
		{Key: "DEMO-2", ID: "2", Summary: "Logout test"},
	}); err != nil {
		t.Fatalf("seed tests: %v", err)
	}

	if err := repo.UpsertContainers("p1", []testrepo.Container{
		{Key: "DEMO-TP-1", Kind: "testplan", Summary: "Plan 1", Status: "Open"},
		{Key: "DEMO-TE-1", Kind: "testexec", Summary: "Exec 1", Status: "Open"},
		{Key: "DEMO-TE-2", Kind: "testexec", Summary: "Exec 2", Status: "Open"},
	}); err != nil {
		t.Fatalf("seed containers: %v", err)
	}

	// Plan 1 has two member tests (run_status unused for plans).
	// Both executions ran both member tests.
	if err := repo.ReplaceAllContainerLinks("p1", []testrepo.ContainerLink{
		{ContainerKey: "DEMO-TP-1", TestKey: "DEMO-1", RunStatus: ""},
		{ContainerKey: "DEMO-TP-1", TestKey: "DEMO-2", RunStatus: ""},
		{ContainerKey: "DEMO-TE-1", TestKey: "DEMO-1", RunStatus: "PASS"},
		{ContainerKey: "DEMO-TE-1", TestKey: "DEMO-2", RunStatus: "FAIL"},
		{ContainerKey: "DEMO-TE-2", TestKey: "DEMO-1", RunStatus: "PASS"},
		{ContainerKey: "DEMO-TE-2", TestKey: "DEMO-2", RunStatus: "PASS"},
	}); err != nil {
		t.Fatalf("seed links: %v", err)
	}

	// Seed test_run rows so GetExecutionMembersWithRuns has run detail data.
	if err := repo.ReplaceRunsForExec("p1", "DEMO-TE-1", []testrepo.TestRunRow{
		{ExecKey: "DEMO-TE-1", TestKey: "DEMO-1", RunStatus: "PASS",
			StartedAt: "2026-05-01T09:00:00Z", FinishedAt: "2026-05-01T10:00:00Z",
			ExecutedBy: "alice", Environment: "Staging"},
		{ExecKey: "DEMO-TE-1", TestKey: "DEMO-2", RunStatus: "FAIL",
			StartedAt: "2026-05-01T10:00:00Z", FinishedAt: "2026-05-01T11:00:00Z",
			ExecutedBy: "bob", Environment: "Staging"},
	}); err != nil {
		t.Fatalf("seed runs for DEMO-TE-1: %v", err)
	}

	return repo
}

func TestGetRunRollup(t *testing.T) {
	r := seedRollupRepo(t)
	roll, err := r.GetRunRollup("p1", "DEMO-TP-1")
	if err != nil {
		t.Fatal(err)
	}
	if roll.Total == 0 {
		t.Fatal("expected a non-zero rollup total for a demo plan")
	}
	// DEMO-1: PASS in both execs -> consolidated PASS
	// DEMO-2: FAIL in TE-1, PASS in TE-2 -> consolidated FAIL (worst-wins)
	if roll.Total != 2 {
		t.Errorf("Total = %d, want 2", roll.Total)
	}
	if roll.Passed != 1 {
		t.Errorf("Passed = %d, want 1", roll.Passed)
	}
	if roll.Failed != 1 {
		t.Errorf("Failed = %d, want 1", roll.Failed)
	}
	if roll.ExecCount != 2 {
		t.Errorf("ExecCount = %d, want 2", roll.ExecCount)
	}
}

func TestGetExecutionMembersWithRuns(t *testing.T) {
	r := seedRollupRepo(t)
	rows, err := r.GetExecutionMembersWithRuns("p1", "DEMO-TE-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("expected execution members")
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 execution members, got %d", len(rows))
	}
	// Find DEMO-1 entry and verify run details.
	var demo1 *testrepo.ExecMemberRun
	for i := range rows {
		if rows[i].TestKey == "DEMO-1" {
			demo1 = &rows[i]
		}
	}
	if demo1 == nil {
		t.Fatal("DEMO-1 not found in execution members")
	}
	if demo1.Summary != "Login test" {
		t.Errorf("Summary = %q, want %q", demo1.Summary, "Login test")
	}
	if demo1.RunStatus != "PASS" {
		t.Errorf("RunStatus = %q, want PASS", demo1.RunStatus)
	}
	if demo1.ExecutedBy != "alice" {
		t.Errorf("ExecutedBy = %q, want alice", demo1.ExecutedBy)
	}
	if demo1.Environment != "Staging" {
		t.Errorf("Environment = %q, want Staging", demo1.Environment)
	}
}

func TestGetTestRunHistory(t *testing.T) {
	repo := newRepo(t)

	// Seed the test case.
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "Login"},
	}); err != nil {
		t.Fatalf("seed test: %v", err)
	}

	// Seed two execution containers: one with fix versions, one without.
	if err := repo.UpsertContainers("p1", []testrepo.Container{
		{Key: "QA-TE-1", Kind: "testexec", Summary: "Cycle 1", Status: "Done", FixVersions: []string{"1.2.0"}},
		{Key: "QA-TE-2", Kind: "testexec", Summary: "Cycle 2", Status: "Done"},
	}); err != nil {
		t.Fatalf("seed containers: %v", err)
	}

	// Seed test_run rows for QA-1 in each execution (with timestamps).
	if err := repo.ReplaceRunsForExec("p1", "QA-TE-1", []testrepo.TestRunRow{
		{ExecKey: "QA-TE-1", TestKey: "QA-1", RunStatus: "FAIL",
			FinishedAt: "2026-05-01T10:00:00Z", Environment: "Staging",
			CreatedAt: "2026-05-01T08:00:00Z", UpdatedAt: "2026-05-01T10:00:00Z"},
	}); err != nil {
		t.Fatalf("seed runs for QA-TE-1: %v", err)
	}
	if err := repo.ReplaceRunsForExec("p1", "QA-TE-2", []testrepo.TestRunRow{
		{ExecKey: "QA-TE-2", TestKey: "QA-1", RunStatus: "PASS",
			FinishedAt: "2026-06-01T09:00:00Z",
			CreatedAt:  "2026-06-01T07:00:00Z", UpdatedAt: "2026-06-01T09:00:00Z"},
	}); err != nil {
		t.Fatalf("seed runs for QA-TE-2: %v", err)
	}

	// Link QA-TE-1 to a Test Plan.
	if err := repo.ReplaceExecPlans("p1", "QA-TE-1", []string{"QA-TP-1"}); err != nil {
		t.Fatalf("seed exec plans: %v", err)
	}

	hist, err := repo.GetTestRunHistory("p1", "QA-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 {
		t.Fatalf("expected 2 run history entries, got %d", len(hist))
	}

	// Newest updated_at should be first (QA-TE-2 updated 2026-06-01).
	first := hist[0]
	if first.ExecKey != "QA-TE-2" {
		t.Errorf("hist[0].ExecKey = %q, want QA-TE-2 (newest updated_at first)", first.ExecKey)
	}
	if first.RunStatus != "PASS" {
		t.Errorf("hist[0].RunStatus = %q, want PASS", first.RunStatus)
	}
	if first.ExecSummary != "Cycle 2" {
		t.Errorf("hist[0].ExecSummary = %q, want Cycle 2", first.ExecSummary)
	}
	if len(first.PlanKeys) != 0 {
		t.Errorf("hist[0].PlanKeys = %v, want empty", first.PlanKeys)
	}
	if first.CreatedAt != "2026-06-01T07:00:00Z" {
		t.Errorf("hist[0].CreatedAt = %q, want 2026-06-01T07:00:00Z", first.CreatedAt)
	}
	if first.UpdatedAt != "2026-06-01T09:00:00Z" {
		t.Errorf("hist[0].UpdatedAt = %q, want 2026-06-01T09:00:00Z", first.UpdatedAt)
	}

	// Second entry: QA-TE-1 (updated 2026-05-01).
	second := hist[1]
	if second.ExecKey != "QA-TE-1" {
		t.Errorf("hist[1].ExecKey = %q, want QA-TE-1", second.ExecKey)
	}
	if second.RunStatus != "FAIL" {
		t.Errorf("hist[1].RunStatus = %q, want FAIL", second.RunStatus)
	}
	if second.Environment != "Staging" {
		t.Errorf("hist[1].Environment = %q, want Staging", second.Environment)
	}
	if second.ExecSummary != "Cycle 1" {
		t.Errorf("hist[1].ExecSummary = %q, want Cycle 1", second.ExecSummary)
	}
	if len(second.FixVersions) != 1 || second.FixVersions[0] != "1.2.0" {
		t.Errorf("hist[1].FixVersions = %v, want [1.2.0]", second.FixVersions)
	}
	if len(second.PlanKeys) != 1 || second.PlanKeys[0] != "QA-TP-1" {
		t.Errorf("hist[1].PlanKeys = %v, want [QA-TP-1]", second.PlanKeys)
	}
	if second.CreatedAt != "2026-05-01T08:00:00Z" {
		t.Errorf("hist[1].CreatedAt = %q, want 2026-05-01T08:00:00Z", second.CreatedAt)
	}
	if second.UpdatedAt != "2026-05-01T10:00:00Z" {
		t.Errorf("hist[1].UpdatedAt = %q, want 2026-05-01T10:00:00Z", second.UpdatedAt)
	}
}
