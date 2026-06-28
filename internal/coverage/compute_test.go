package coverage

import (
	"testing"

	"xray-test-manager/internal/store"
)

// seedTest inserts a test_case and, when runStatus != "", a Test Execution
// container membership carrying that run status (so ConsolidatedRunByTest sees
// it). reqKey links the test to a requirement for candidate-test queries.
func seedTest(t *testing.T, st *store.Store, profileID, testKey, runStatus, reqKey string) {
	t.Helper()
	db := st.DB()
	if _, err := db.Exec(
		`INSERT INTO test_case (profile_id, jira_key, jira_id, summary) VALUES (?, ?, '1', ?)`,
		profileID, testKey, "Test "+testKey); err != nil {
		t.Fatalf("seed test_case: %v", err)
	}
	if reqKey != "" {
		if _, err := db.Exec(
			`INSERT INTO test_requirement (profile_id, test_key, requirement_key) VALUES (?, ?, ?)`,
			profileID, testKey, reqKey); err != nil {
			t.Fatalf("seed test_requirement: %v", err)
		}
	}
	if runStatus != "" {
		const exec = "EXEC-1"
		db.Exec(`INSERT OR IGNORE INTO test_container (profile_id, jira_key, kind) VALUES (?, ?, 'testexec')`, profileID, exec)
		if _, err := db.Exec(
			`INSERT INTO test_container_test (profile_id, container_key, test_key, run_status) VALUES (?, ?, ?, ?)`,
			profileID, exec, testKey, runStatus); err != nil {
			t.Fatalf("seed container_test: %v", err)
		}
	}
}

// buildModel creates a canonical node with one version, one group, one parameter
// holding the given value labels (all required). Returns (canonicalID, versionID,
// parameterID, valueIDs).
func buildModel(t *testing.T, m *Module, profileID, groupName string, values []string) (string, string, string, []string) {
	t.Helper()
	cid, err := m.CreateCanonical(profileID, "C_Sign", "PKCS11", "")
	if err != nil {
		t.Fatalf("create canonical: %v", err)
	}
	vid, err := m.CreateVersion(profileID, cid, "1.0", "stable", "")
	if err != nil {
		t.Fatalf("create version: %v", err)
	}
	gid, err := m.UpsertNode(profileID, NodeEdit{Kind: "group", VersionID: vid, Name: groupName})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	pid, err := m.UpsertNode(profileID, NodeEdit{Kind: "parameter", GroupID: gid, Name: "pParam"})
	if err != nil {
		t.Fatalf("create parameter: %v", err)
	}
	ids := make([]string, len(values))
	for i, label := range values {
		v, err := m.UpsertNode(profileID, NodeEdit{Kind: "value", ParameterID: pid, Name: label, IsRequired: true, SortOrder: i})
		if err != nil {
			t.Fatalf("create value %q: %v", label, err)
		}
		ids[i] = v
	}
	return cid, vid, pid, ids
}

func TestComputeCoverageAndGaps(t *testing.T) {
	m, st := newTestModule(t)
	const p = "p1"

	cid, vid, _, vids := buildModel(t, m, p, "Mechanism", []string{"RSA_PKCS", "SHA256_RSA", "SHA512_ECDSA", "ED25519"})

	// Two passing tests, linked to a member requirement.
	if _, err := st.DB().Exec(`INSERT INTO requirement (profile_id, jira_key, project_key) VALUES (?, 'FUNC-1', 'FUNC')`, p); err != nil {
		t.Fatal(err)
	}
	if err := m.SetMembers(p, cid, []string{"FUNC-1"}); err != nil {
		t.Fatal(err)
	}
	seedTest(t, st, p, "QA-1", "PASS", "FUNC-1")
	seedTest(t, st, p, "QA-2", "FAIL", "FUNC-1")

	// Map: RSA_PKCS -> QA-1 (pass), SHA256_RSA -> QA-2 (fail). Two values untested.
	if err := m.SetValueTests(p, vids[0], []string{"QA-1"}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetValueTests(p, vids[1], []string{"QA-2"}); err != nil {
		t.Fatal(err)
	}

	rep, err := m.ComputeCoverage(p, vid)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if rep.TotalValues != 4 || rep.TestedValues != 2 {
		t.Fatalf("totals = %d/%d, want 2/4", rep.TestedValues, rep.TotalValues)
	}
	if rep.Percent != 50 {
		t.Errorf("percent = %v, want 50", rep.Percent)
	}
	if got := rep.Values[vids[0]].RunStatus; got != "PASSED" {
		t.Errorf("RSA_PKCS run status = %q, want PASSED", got)
	}
	if got := rep.Values[vids[1]].RunStatus; got != "FAILED" {
		t.Errorf("SHA256_RSA run status = %q, want FAILED", got)
	}
	if rep.Values[vids[2]].Tested {
		t.Errorf("SHA512_ECDSA should be untested")
	}

	gaps, err := m.ListGaps(p, vid)
	if err != nil {
		t.Fatalf("gaps: %v", err)
	}
	if len(gaps) != 2 {
		t.Fatalf("gaps = %d, want 2", len(gaps))
	}
	gapLabels := map[string]bool{gaps[0].ValueLabel: true, gaps[1].ValueLabel: true}
	if !gapLabels["SHA512_ECDSA"] || !gapLabels["ED25519"] {
		t.Errorf("gap labels = %v, want SHA512_ECDSA + ED25519", gapLabels)
	}
}

func TestStaleMappingDetection(t *testing.T) {
	m, st := newTestModule(t)
	const p = "p1"
	_, vid, _, vids := buildModel(t, m, p, "Session", []string{"Valid"})

	seedTest(t, st, p, "QA-9", "PASS", "")
	if err := m.SetValueTests(p, vids[0], []string{"QA-9"}); err != nil {
		t.Fatal(err)
	}

	// Initially live → not stale, and counts as tested.
	// Use "" (profile-wide scan) since groups are version-scoped now, not canonical-scoped.
	if stale, _ := m.DetectStaleMappings(p, ""); len(stale) != 0 {
		t.Fatalf("expected no stale mappings, got %d", len(stale))
	}
	rep, _ := m.ComputeCoverage(p, vid)
	if rep.Percent != 100 {
		t.Fatalf("percent = %v, want 100", rep.Percent)
	}

	// Delete the test → mapping rots; value is no longer tested but mapping kept.
	if _, err := st.DB().Exec(`DELETE FROM test_case WHERE profile_id = ? AND jira_key = 'QA-9'`, p); err != nil {
		t.Fatal(err)
	}
	stale, err := m.DetectStaleMappings(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 || stale[0].TestKey != "QA-9" {
		t.Fatalf("stale = %+v, want one QA-9", stale)
	}
	rep, _ = m.ComputeCoverage(p, vid)
	if rep.Percent != 0 || rep.TestedValues != 0 {
		t.Errorf("after test deletion percent = %v tested = %d, want 0/0 (stale excluded)", rep.Percent, rep.TestedValues)
	}
}

func TestCandidateTestsLimitedToMembers(t *testing.T) {
	m, st := newTestModule(t)
	const p = "p1"
	cid, _, _, _ := buildModel(t, m, p, "Session", []string{"Valid"})
	if _, err := st.DB().Exec(`INSERT INTO requirement (profile_id, jira_key, project_key) VALUES (?, 'FUNC-1', 'FUNC')`, p); err != nil {
		t.Fatal(err)
	}
	if err := m.SetMembers(p, cid, []string{"FUNC-1"}); err != nil {
		t.Fatal(err)
	}
	seedTest(t, st, p, "QA-1", "", "FUNC-1")  // linked to member
	seedTest(t, st, p, "QA-2", "", "OTHER-9") // linked to non-member

	cands, err := m.ListCandidateTests(p, cid)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 || cands[0].TestKey != "QA-1" {
		t.Fatalf("candidates = %+v, want only QA-1", cands)
	}
}
