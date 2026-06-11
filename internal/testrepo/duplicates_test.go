package testrepo

import (
	"path/filepath"
	"testing"

	"xray-test-manager/internal/store"
)

func TestNormalizeSummaryCollapsesAndLowercases(t *testing.T) {
	cases := map[string]string{
		"  Login   With  VALID creds ": "login with valid creds",
		"Login with valid creds":       "login with valid creds",
		"\tReset\nPassword ":           "reset password",
	}
	for in, want := range cases {
		if got := normalizeText(in); got != want {
			t.Errorf("normalizeText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStepFingerprintEquality(t *testing.T) {
	a := []Step{{Action: "Open  page", Data: "", Expected: "Form SHOWN"}}
	b := []Step{{Action: "open page", Data: "", Expected: "form shown"}}
	c := []Step{{Action: "Open page", Data: "x", Expected: "form shown"}}
	if StepFingerprint(a) != StepFingerprint(b) {
		t.Error("a and b should fingerprint equal after normalization")
	}
	if StepFingerprint(a) == StepFingerprint(c) {
		t.Error("a and c should differ (data differs)")
	}
	if StepFingerprint(nil) != "" {
		t.Error("no steps should fingerprint to empty string")
	}
}

func newDupRepo(t *testing.T) *Repository {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "dup.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return NewRepository(st)
}

func TestExcludeUnexcludeRoundTrip(t *testing.T) {
	repo := newDupRepo(t)
	if err := repo.ExcludeFromDuplicates("p1", "QA-1"); err != nil {
		t.Fatalf("exclude: %v", err)
	}
	// Idempotent.
	if err := repo.ExcludeFromDuplicates("p1", "QA-1"); err != nil {
		t.Fatalf("exclude again: %v", err)
	}
	n, err := repo.countExcluded("p1")
	if err != nil || n != 1 {
		t.Fatalf("excluded = %d (err %v), want 1", n, err)
	}
	if err := repo.UnexcludeFromDuplicates("p1", "QA-1"); err != nil {
		t.Fatalf("unexclude: %v", err)
	}
	n, _ = repo.countExcluded("p1")
	if n != 0 {
		t.Fatalf("excluded after remove = %d, want 0", n)
	}
}

func TestRecordStepScanStoresFingerprint(t *testing.T) {
	repo := newDupRepo(t)
	steps := []Step{{Action: "do", Expected: "ok"}}
	if err := repo.RecordStepScan("p1", "QA-1", steps); err != nil {
		t.Fatalf("record: %v", err)
	}
	fps, scannedAt, err := repo.stepScans("p1")
	if err != nil {
		t.Fatalf("stepScans: %v", err)
	}
	if fps["QA-1"] != StepFingerprint(steps) {
		t.Errorf("fingerprint = %q, want %q", fps["QA-1"], StepFingerprint(steps))
	}
	if scannedAt == "" {
		t.Error("scannedAt should be set")
	}
}

// seedDupTest inserts test_case rows directly for grouping tests.
func seedDupTest(t *testing.T, repo *Repository, profile, key, summary, status string) {
	t.Helper()
	_, err := repo.db.Exec(
		`INSERT INTO test_case (profile_id, jira_key, jira_id, summary, status, folder_id)
		 VALUES (?, ?, '', ?, ?, '/F')`,
		profile, key, summary, status,
	)
	if err != nil {
		t.Fatalf("seed test %s: %v", key, err)
	}
}

func TestScanDuplicatesGroupsAndCounts(t *testing.T) {
	repo := newDupRepo(t)
	// Two identical-summary groups + one unique test.
	seedDupTest(t, repo, "p1", "QA-1", "Login with valid creds", "Approved")
	seedDupTest(t, repo, "p1", "QA-2", "login WITH valid creds", "Draft") // normalizes equal
	seedDupTest(t, repo, "p1", "QA-3", "Reset password", "Approved")
	seedDupTest(t, repo, "p1", "QA-4", "Reset password", "Approved")
	seedDupTest(t, repo, "p1", "QA-9", "Unique test", "Approved")

	rep, err := repo.ScanDuplicates("p1")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if rep.GroupCount != 2 {
		t.Fatalf("groupCount = %d, want 2", rep.GroupCount)
	}
	if rep.TestCount != 4 {
		t.Fatalf("testCount = %d, want 4", rep.TestCount)
	}
	// No step scans yet → both groups unscanned.
	if rep.StepsUnscanned != 2 || rep.StepsIdentical != 0 {
		t.Fatalf("verdict counts = %+v, want 2 unscanned", rep)
	}

	// Exclude QA-2 → the login group drops below 2 members and disappears.
	if err := repo.ExcludeFromDuplicates("p1", "QA-2"); err != nil {
		t.Fatalf("exclude: %v", err)
	}
	rep, _ = repo.ScanDuplicates("p1")
	if rep.GroupCount != 1 || rep.Excluded != 1 {
		t.Fatalf("after exclude: groups=%d excluded=%d, want 1 and 1", rep.GroupCount, rep.Excluded)
	}
}

func TestScanDuplicateGroupStepVerdicts(t *testing.T) {
	repo := newDupRepo(t)
	seedDupTest(t, repo, "p1", "QA-3", "Reset password", "Approved")
	seedDupTest(t, repo, "p1", "QA-4", "Reset password", "Approved")
	same := []Step{{Action: "open reset", Expected: "form"}}
	if err := repo.RecordStepScan("p1", "QA-3", same); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordStepScan("p1", "QA-4", same); err != nil {
		t.Fatal(err)
	}
	g, err := repo.ScanDuplicateGroup("p1", "reset password")
	if err != nil {
		t.Fatalf("group: %v", err)
	}
	if g.StepsVerdict != "identical" {
		t.Fatalf("verdict = %q, want identical", g.StepsVerdict)
	}
	// Change QA-4's steps → differ.
	if err := repo.RecordStepScan("p1", "QA-4", []Step{{Action: "different"}}); err != nil {
		t.Fatal(err)
	}
	g, _ = repo.ScanDuplicateGroup("p1", "reset password")
	if g.StepsVerdict != "differ" {
		t.Fatalf("verdict = %q, want differ", g.StepsVerdict)
	}
}

func TestScanDuplicateGroupPartialScanIsUnscanned(t *testing.T) {
	repo := newDupRepo(t)
	// Seed two tests with the same summary; QA-3 < QA-4 lexicographically.
	seedDupTest(t, repo, "p1", "QA-3", "Reset password", "Approved")
	seedDupTest(t, repo, "p1", "QA-4", "Reset password", "Draft")

	// Record a step scan for QA-3 only — QA-4 remains unscanned.
	if err := repo.RecordStepScan("p1", "QA-3", []Step{{Action: "x"}}); err != nil {
		t.Fatalf("RecordStepScan: %v", err)
	}

	g, err := repo.ScanDuplicateGroup("p1", "reset password")
	if err != nil {
		t.Fatalf("ScanDuplicateGroup: %v", err)
	}

	// Because QA-4 has no step scan the whole group must be "unscanned".
	if g.StepsVerdict != "unscanned" {
		t.Fatalf("StepsVerdict = %q, want \"unscanned\"", g.StepsVerdict)
	}

	// DisplaySummary comes from the lexicographically-smallest key (QA-3).
	wantDisplay := "Reset password"
	if g.DisplaySummary != wantDisplay {
		t.Fatalf("DisplaySummary = %q, want %q", g.DisplaySummary, wantDisplay)
	}
}
