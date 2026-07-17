package testrepo

import "testing"

// seedRunDefectsRepo sets up one Test Execution (QA-TE-1) with QA-1 as its
// only member (QA-2 stays a non-member for the "not in execution" tests).
func seedRunDefectsRepo(t *testing.T) *Repository {
	t.Helper()
	r := newTestRepo(t)
	if err := r.UpsertTests("p1", []TestCase{
		{Key: "QA-1", ID: "1"}, {Key: "QA-2", ID: "2"},
	}); err != nil {
		t.Fatalf("seed tests: %v", err)
	}
	if err := r.UpsertContainers("p1", []Container{
		{Key: "QA-TE-1", Kind: "testexec", Summary: "Cycle 1"},
	}); err != nil {
		t.Fatalf("seed exec: %v", err)
	}
	if err := r.ReplaceAllContainerLinks("p1", []ContainerLink{
		{ContainerKey: "QA-TE-1", TestKey: "QA-1", RunStatus: "TODO"},
	}); err != nil {
		t.Fatalf("seed link: %v", err)
	}
	return r
}

func rawRunDefects(t *testing.T, r *Repository, profileID, execKey, testKey string) string {
	t.Helper()
	var v string
	if err := r.db.QueryRow(
		`SELECT run_defects FROM test_container_test
		 WHERE profile_id = ? AND container_key = ? AND test_key = ?`,
		profileID, execKey, testKey,
	).Scan(&v); err != nil {
		t.Fatalf("read run_defects: %v", err)
	}
	return v
}

func rawRunComment(t *testing.T, r *Repository, profileID, execKey, testKey string) string {
	t.Helper()
	var v string
	if err := r.db.QueryRow(
		`SELECT run_comment FROM test_container_test
		 WHERE profile_id = ? AND container_key = ? AND test_key = ?`,
		profileID, execKey, testKey,
	).Scan(&v); err != nil {
		t.Fatalf("read run_comment: %v", err)
	}
	return v
}

func pendingCount(t *testing.T, r *Repository, profileID, entityType, entityKey string) int {
	t.Helper()
	var n int
	if err := r.db.QueryRow(
		`SELECT COUNT(*) FROM pending_change
		 WHERE profile_id = ? AND entity_type = ? AND entity_key = ?`,
		profileID, entityType, entityKey,
	).Scan(&n); err != nil {
		t.Fatalf("count pending: %v", err)
	}
	return n
}

func findMember(rows []ExecMemberRun, testKey string) *ExecMemberRun {
	for i := range rows {
		if rows[i].TestKey == testKey {
			return &rows[i]
		}
	}
	return nil
}

// Add a defect to a run: ExecMemberRun.Defects shows it, a test_run_defect
// pending change exists, and the staging column holds the JSON array.
func TestAddTestRunDefectStagesAndShows(t *testing.T) {
	r := seedRunDefectsRepo(t)
	if err := r.AddTestRunDefect("p1", "QA-TE-1", "QA-1", "BUG-1"); err != nil {
		t.Fatalf("AddTestRunDefect: %v", err)
	}

	rows, err := r.GetExecutionMembersWithRuns("p1", "QA-TE-1")
	if err != nil {
		t.Fatalf("GetExecutionMembersWithRuns: %v", err)
	}
	qa1 := findMember(rows, "QA-1")
	if qa1 == nil {
		t.Fatal("QA-1 not found in execution members")
	}
	if len(qa1.Defects) != 1 || qa1.Defects[0] != "BUG-1" {
		t.Errorf("Defects = %v, want [BUG-1]", qa1.Defects)
	}
	if n := pendingCount(t, r, "p1", entityTestRunDefect, "QA-TE-1:QA-1"); n != 1 {
		t.Errorf("pending test_run_defect rows = %d, want 1", n)
	}
	if got := rawRunDefects(t, r, "p1", "QA-TE-1", "QA-1"); got != `["BUG-1"]` {
		t.Errorf("run_defects column = %q, want [\"BUG-1\"]", got)
	}
}

// Remove the only staged defect when the synced base is empty: this reverts
// the local edit entirely — Defects reads back empty, the run_defects column
// resets to "" (not "[]"), and the pending change is dropped.
func TestRemoveOnlyStagedDefectRevertsWhenSyncedBaseEmpty(t *testing.T) {
	r := seedRunDefectsRepo(t)
	if err := r.AddTestRunDefect("p1", "QA-TE-1", "QA-1", "BUG-1"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := r.RemoveTestRunDefect("p1", "QA-TE-1", "QA-1", "BUG-1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	rows, err := r.GetExecutionMembersWithRuns("p1", "QA-TE-1")
	if err != nil {
		t.Fatalf("GetExecutionMembersWithRuns: %v", err)
	}
	qa1 := findMember(rows, "QA-1")
	if qa1 == nil {
		t.Fatal("QA-1 not found in execution members")
	}
	if len(qa1.Defects) != 0 {
		t.Errorf("Defects = %v, want empty", qa1.Defects)
	}
	if got := rawRunDefects(t, r, "p1", "QA-TE-1", "QA-1"); got != "" {
		t.Errorf("run_defects column = %q, want '' (reverted to no local edit)", got)
	}
	if n := pendingCount(t, r, "p1", entityTestRunDefect, "QA-TE-1:QA-1"); n != 0 {
		t.Errorf("pending test_run_defect rows = %d, want 0 (dropped)", n)
	}
}

// Remove a defect that only exists in the SYNCED set (base non-empty): the
// resulting set differs from the synced base even though it's empty, so this
// is the empty-honoring case — Defects reads back empty but the pending
// change stays present and the column holds "[]", not "".
func TestRemoveDefectFromSyncedBaseStaysStaged(t *testing.T) {
	r := seedRunDefectsRepo(t)
	if err := r.ReplaceRunsForExec("p1", "QA-TE-1", []TestRunRow{
		{ExecKey: "QA-TE-1", TestKey: "QA-1", RunStatus: "PASS", Defects: `["BUG-1"]`},
	}); err != nil {
		t.Fatalf("seed synced run: %v", err)
	}

	if err := r.RemoveTestRunDefect("p1", "QA-TE-1", "QA-1", "BUG-1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	rows, err := r.GetExecutionMembersWithRuns("p1", "QA-TE-1")
	if err != nil {
		t.Fatalf("GetExecutionMembersWithRuns: %v", err)
	}
	qa1 := findMember(rows, "QA-1")
	if qa1 == nil {
		t.Fatal("QA-1 not found in execution members")
	}
	if len(qa1.Defects) != 0 {
		t.Errorf("Defects = %v, want empty (all synced defects removed locally)", qa1.Defects)
	}
	if got := rawRunDefects(t, r, "p1", "QA-TE-1", "QA-1"); got != "[]" {
		t.Errorf("run_defects column = %q, want \"[]\" (staged empty, distinct from no local edit)", got)
	}
	if n := pendingCount(t, r, "p1", entityTestRunDefect, "QA-TE-1:QA-1"); n != 1 {
		t.Errorf("pending test_run_defect rows = %d, want 1 (stays staged)", n)
	}
}

// Set a comment, then clear it, when the synced base is non-empty: the clear
// differs from the synced comment, so it stays staged rather than silently
// reverting — Comment reads back "" and the pending change is present.
func TestSetTestRunCommentClearStaysStaged(t *testing.T) {
	r := seedRunDefectsRepo(t)
	if err := r.ReplaceRunsForExec("p1", "QA-TE-1", []TestRunRow{
		{ExecKey: "QA-TE-1", TestKey: "QA-1", RunStatus: "PASS", Comment: "looked fine"},
	}); err != nil {
		t.Fatalf("seed synced run: %v", err)
	}

	if err := r.SetTestRunComment("p1", "QA-TE-1", "QA-1", "custom note"); err != nil {
		t.Fatalf("stage comment: %v", err)
	}
	if err := r.SetTestRunComment("p1", "QA-TE-1", "QA-1", ""); err != nil {
		t.Fatalf("clear comment: %v", err)
	}

	rows, err := r.GetExecutionMembersWithRuns("p1", "QA-TE-1")
	if err != nil {
		t.Fatalf("GetExecutionMembersWithRuns: %v", err)
	}
	qa1 := findMember(rows, "QA-1")
	if qa1 == nil {
		t.Fatal("QA-1 not found in execution members")
	}
	if qa1.Comment != "" {
		t.Errorf("Comment = %q, want empty (cleared)", qa1.Comment)
	}
	if n := pendingCount(t, r, "p1", entityTestRunComment, "QA-TE-1:QA-1"); n != 1 {
		t.Errorf("pending test_run_comment rows = %d, want 1 (not silently reverted)", n)
	}
}

// Set a comment back to the synced value: this reverts the local edit — the
// pending change is dropped and Comment reads back the synced value again.
func TestSetTestRunCommentBackToSyncedDrops(t *testing.T) {
	r := seedRunDefectsRepo(t)
	if err := r.ReplaceRunsForExec("p1", "QA-TE-1", []TestRunRow{
		{ExecKey: "QA-TE-1", TestKey: "QA-1", RunStatus: "PASS", Comment: "looked fine"},
	}); err != nil {
		t.Fatalf("seed synced run: %v", err)
	}
	if err := r.SetTestRunComment("p1", "QA-TE-1", "QA-1", "custom note"); err != nil {
		t.Fatalf("stage comment: %v", err)
	}
	if n := pendingCount(t, r, "p1", entityTestRunComment, "QA-TE-1:QA-1"); n != 1 {
		t.Fatalf("pending test_run_comment rows = %d, want 1 before revert", n)
	}

	if err := r.SetTestRunComment("p1", "QA-TE-1", "QA-1", "looked fine"); err != nil {
		t.Fatalf("revert comment: %v", err)
	}

	if n := pendingCount(t, r, "p1", entityTestRunComment, "QA-TE-1:QA-1"); n != 0 {
		t.Errorf("pending test_run_comment rows = %d, want 0 (dropped)", n)
	}
	if got := rawRunComment(t, r, "p1", "QA-TE-1", "QA-1"); got != "" {
		t.Errorf("run_comment column = %q, want '' after revert", got)
	}

	rows, err := r.GetExecutionMembersWithRuns("p1", "QA-TE-1")
	if err != nil {
		t.Fatalf("GetExecutionMembersWithRuns: %v", err)
	}
	qa1 := findMember(rows, "QA-1")
	if qa1 == nil {
		t.Fatal("QA-1 not found in execution members")
	}
	if qa1.Comment != "looked fine" {
		t.Errorf("Comment = %q, want synced value %q", qa1.Comment, "looked fine")
	}
}

// A test key that isn't a member of the execution errors out for all three
// mutations, mirroring SetTestRunStatus's membership check.
func TestRunDefectAndCommentRejectNonMember(t *testing.T) {
	r := seedRunDefectsRepo(t)
	if err := r.AddTestRunDefect("p1", "QA-TE-1", "QA-2", "BUG-1"); err == nil {
		t.Error("AddTestRunDefect for a non-member should error")
	}
	if err := r.RemoveTestRunDefect("p1", "QA-TE-1", "QA-2", "BUG-1"); err == nil {
		t.Error("RemoveTestRunDefect for a non-member should error")
	}
	if err := r.SetTestRunComment("p1", "QA-TE-1", "QA-2", "note"); err == nil {
		t.Error("SetTestRunComment for a non-member should error")
	}
}
