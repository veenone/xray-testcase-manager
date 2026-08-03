package testrepo

import "testing"

// seedPrecond inserts a precondition row via the public upsert path.
func seedPrecond(t *testing.T, repo *Repository, profile, key, summary, condition, description string) {
	t.Helper()
	if err := repo.UpsertPreconditions(profile, []Precondition{
		{Key: key, Summary: summary, Type: "Manual", Condition: condition, Description: description},
	}); err != nil {
		t.Fatalf("seed precondition %s: %v", key, err)
	}
}

func TestScanPreconditionDuplicatesGroupsAndVerdicts(t *testing.T) {
	repo := newDupRepo(t)
	// Group A: same normalized summary AND same definition → identical.
	seedPrecond(t, repo, "p1", "PC-1", "User is logged in", "session active", "")
	seedPrecond(t, repo, "p1", "PC-2", "user IS  logged in", "session active", "")
	// Group B: same normalized summary but different definition → differ.
	seedPrecond(t, repo, "p1", "PC-3", "Cart has items", "cart > 0", "")
	seedPrecond(t, repo, "p1", "PC-4", "Cart has items", "cart >= 1", "")
	// Unique → no group.
	seedPrecond(t, repo, "p1", "PC-9", "Unique precondition", "x", "")

	rep, err := repo.ScanPreconditionDuplicates("p1")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if rep.GroupCount != 2 {
		t.Fatalf("groupCount = %d, want 2", rep.GroupCount)
	}
	if rep.PreconditionCount != 4 {
		t.Fatalf("preconditionCount = %d, want 4", rep.PreconditionCount)
	}
	if rep.DefinitionIdentical != 1 || rep.DefinitionDiffer != 1 {
		t.Fatalf("verdicts = identical %d / differ %d, want 1/1",
			rep.DefinitionIdentical, rep.DefinitionDiffer)
	}

	// Groups are emitted in normalized-summary order: "cart has items" < "user...".
	if got := rep.Groups[0].DefinitionVerdict; got != defVerdictDiffer {
		t.Errorf("group[0] verdict = %q, want differ", got)
	}
	if got := rep.Groups[1].DefinitionVerdict; got != defVerdictIdentical {
		t.Errorf("group[1] verdict = %q, want identical", got)
	}

	// Exclude PC-2 → the login group drops below 2 members and disappears.
	if err := repo.ExcludePreconditionFromDuplicates("p1", "PC-2"); err != nil {
		t.Fatalf("exclude: %v", err)
	}
	rep, _ = repo.ScanPreconditionDuplicates("p1")
	if rep.GroupCount != 1 || rep.Excluded != 1 {
		t.Fatalf("after exclude: groups=%d excluded=%d, want 1 and 1",
			rep.GroupCount, rep.Excluded)
	}

	// Unexclude restores the group.
	if err := repo.UnexcludePreconditionFromDuplicates("p1", "PC-2"); err != nil {
		t.Fatalf("unexclude: %v", err)
	}
	rep, _ = repo.ScanPreconditionDuplicates("p1")
	if rep.GroupCount != 2 || rep.Excluded != 0 {
		t.Fatalf("after unexclude: groups=%d excluded=%d, want 2 and 0",
			rep.GroupCount, rep.Excluded)
	}
}

func TestScanPreconditionDuplicatesTestCount(t *testing.T) {
	repo := newDupRepo(t)
	seedPrecond(t, repo, "p1", "PC-1", "Shared setup", "ready", "")
	seedPrecond(t, repo, "p1", "PC-2", "Shared setup", "ready", "")
	// Link two tests to PC-1 so its member reports testCount = 2.
	for _, tk := range []string{"QA-1", "QA-2"} {
		if _, err := repo.db.Exec(
			`INSERT INTO test_precondition (profile_id, test_key, precondition_key)
			 VALUES (?, ?, ?)`, "p1", tk, "PC-1"); err != nil {
			t.Fatalf("link %s: %v", tk, err)
		}
	}

	rep, err := repo.ScanPreconditionDuplicates("p1")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(rep.Groups) != 1 {
		t.Fatalf("want 1 group, got %d", len(rep.Groups))
	}
	var pc1 *PreconditionDuplicateMember
	for i := range rep.Groups[0].Members {
		if rep.Groups[0].Members[i].Key == "PC-1" {
			pc1 = &rep.Groups[0].Members[i]
		}
	}
	if pc1 == nil {
		t.Fatal("PC-1 not found in group")
	}
	if pc1.TestCount != 2 {
		t.Errorf("PC-1 testCount = %d, want 2", pc1.TestCount)
	}
}
