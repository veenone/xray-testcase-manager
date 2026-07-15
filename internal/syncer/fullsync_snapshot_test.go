package syncer_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"sort"
	"testing"

	"xray-test-manager/internal/jira"
	"xray-test-manager/internal/store"
	"xray-test-manager/internal/syncer"
	"xray-test-manager/internal/testrepo"
)

// TestFullSyncStoreSnapshot is a broad behavior-lock (golden) characterization
// test for the full pull path. It runs a complete demo-backed Sync (since = "",
// the full-pull path) and pins a comprehensive snapshot of what lands in the
// local store: row counts across every entity table plus representative field
// values on known demo entities.
//
// The generic demo generator (theme.Variant == "", selected by the plain "demo"
// URL) is deterministic, so the exact counts below are stable. This test exists
// to FAIL if an upcoming backend-interface extraction changes any pull result.
// The counts marked "captured" were read back from an actual run and hardcoded;
// the derivation of each is noted so a reviewer can trace it to the generator in
// internal/jira/demo.go, internal/jira/requirements.go, and internal/jira/bugs.go.
func TestFullSyncStoreSnapshot(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	repo := testrepo.NewRepository(st)

	const (
		profileID  = "p1"
		projectKey = "DEMO"
	)
	eng := syncer.New(jira.NewClient("demo", "tok"), repo)
	if err := eng.Sync(context.Background(), profileID, projectKey, "", "", nil); err != nil {
		t.Fatalf("full sync: %v", err)
	}

	db := st.DB()
	count := func(table string) int {
		t.Helper()
		var n int
		if err := db.QueryRow(
			"SELECT COUNT(*) FROM "+table+" WHERE profile_id = ?", profileID,
		).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}

	// --- Exact row counts (hand-derivable from the generators) ---

	// test_case: demoTestCount = 5000 (internal/jira/demo.go).
	if got := count("test_case"); got != 5000 {
		t.Errorf("test_case count = %d, want 5000 (demoTestCount)", got)
	}
	// test_folder: 7 category folders + 30 leaf feature folders across
	// demoFolderCategories (Authentication 6, Browse 4, Commerce 6, User 3,
	// Reporting 4, Admin 3, System 4 = 30 features) = 37.
	if got := count("test_folder"); got != 37 {
		t.Errorf("test_folder count = %d, want 37 (7 categories + 30 features)", got)
	}
	// precondition: len(preconditionDefs) = 15 (internal/jira/demo.go).
	if got := count("precondition"); got != 15 {
		t.Errorf("precondition count = %d, want 15 (len(preconditionDefs))", got)
	}
	// requirement: generic demoRequirements seeds PRD-1..PRD-24 = 24
	// (internal/jira/requirements.go, const count = 24).
	if got := count("requirement"); got != 24 {
		t.Errorf("requirement count = %d, want 24 (PRD-1..PRD-24)", got)
	}

	// --- Row counts captured from an actual full-sync run, then hardcoded ---
	// These derive from modulo-driven loops in the generators that are error
	// prone to compute by hand, so they were read back from a real run and
	// pinned. A reviewer can re-derive them from the cited generator functions;
	// the point is that any drift in the pull result flips these assertions.

	// test_container: demoContainersAndLinks seeds 7 Test Sets + 5 Test Plans +
	// 8 Test Executions + 2 cross-project execs (XRAYINT-TE-1/2) + 1
	// cross-project-member exec (DEMO-TE-XPROJ) + 2 sub-task execs
	// (DEMO-STE-1/2) = 25, plus XRAYINT-STE-1 discovered by
	// discoverCrossProjectExecs during the full Sync (it walks all test keys) = 26.
	if got := count("test_container"); got != 26 {
		t.Errorf("test_container count = %d, want 26 (see demoContainersAndLinks + cross-project discovery)", got)
	}
	// test_container_test: container memberships written by the container sync
	// plus cross-project discovery (captured).
	if got := count("test_container_test"); got != 1128 {
		t.Errorf("test_container_test count = %d, want 1128 (container memberships)", got)
	}
	// test_precondition: one row per (test, precondition) association across the
	// 5000 tests via featurePreconditions (captured).
	if got := count("test_precondition"); got != 6664 {
		t.Errorf("test_precondition count = %d, want 6664 (featurePreconditions associations)", got)
	}
	// test_requirement: requirement coverage links from demoRequirements
	// (generic branch, captured).
	if got := count("test_requirement"); got != 55 {
		t.Errorf("test_requirement count = %d, want 55 (requirement coverage links)", got)
	}
	// bug: merged project-wide + link-harvest demo defects (captured).
	if got := count("bug"); got != 13 {
		t.Errorf("bug count = %d, want 13 (demoBugs)", got)
	}
	// test_bug: Test<->Bug links from the demo bug harvest (captured).
	if got := count("test_bug"); got != 14 {
		t.Errorf("test_bug count = %d, want 14 (demo bug links)", got)
	}
	// test_run: per-execution run rows fetched for every Test Execution during
	// the container sync (captured).
	if got := count("test_run"); got != 309 {
		t.Errorf("test_run count = %d, want 309 (execution run rows)", got)
	}
	// exec_plan: Test Execution -> Test Plan associations (captured).
	if got := count("exec_plan"); got != 20 {
		t.Errorf("exec_plan count = %d, want 20 (exec-plan links)", got)
	}

	// --- Representative field values on known demo entities ---

	// DEMO-1 (index 0): makeDemoTest special-cases i==0 in the generic theme to
	// seed a duplicate cluster, overwriting the summary. Pin that exact behavior.
	demo1, err := repo.GetTest(profileID, "DEMO-1")
	if err != nil {
		t.Fatalf("GetTest DEMO-1: %v", err)
	}
	if demo1.Summary != "Duplicate demo A — user can log in" {
		t.Errorf("DEMO-1 summary = %q, want the duplicate-cluster override", demo1.Summary)
	}
	if demo1.Status != "Approved" {
		t.Errorf("DEMO-1 status = %q, want Approved (demoStatuses[0])", demo1.Status)
	}
	if demo1.Priority != "High" {
		t.Errorf("DEMO-1 priority = %q, want High (demoPriorities[(0*7+3)%%7])", demo1.Priority)
	}
	if demo1.FolderID != "/Authentication/Login" {
		t.Errorf("DEMO-1 folder = %q, want /Authentication/Login (Login feature)", demo1.FolderID)
	}
	if demo1.ExecType != "Manual" {
		t.Errorf("DEMO-1 exec_type = %q, want Manual (demoExecTypes[0])", demo1.ExecType)
	}

	// DEMO-5 (index 4): a non-clustered test that follows the general
	// "<feature> <condition>" summary template — feature demoFeatures[4] =
	// "Search", condition demoConditions[0] = "with valid input".
	demo5, err := repo.GetTest(profileID, "DEMO-5")
	if err != nil {
		t.Fatalf("GetTest DEMO-5: %v", err)
	}
	if demo5.Summary != "Search with valid input" {
		t.Errorf("DEMO-5 summary = %q, want \"Search with valid input\"", demo5.Summary)
	}
	if demo5.Status != "Done" {
		t.Errorf("DEMO-5 status = %q, want Done (demoStatuses[4])", demo5.Status)
	}
	if demo5.FolderID != "/Browse/Search" {
		t.Errorf("DEMO-5 folder = %q, want /Browse/Search", demo5.FolderID)
	}

	// Precondition DEMO-P-1 — first entry of preconditionDefs.
	var preSummary, preCondition string
	if err := db.QueryRow(
		`SELECT summary, condition FROM precondition WHERE profile_id = ? AND jira_key = ?`,
		profileID, "DEMO-P-1",
	).Scan(&preSummary, &preCondition); err != nil {
		t.Fatalf("read DEMO-P-1: %v", err)
	}
	if preSummary != "User account exists" {
		t.Errorf("DEMO-P-1 summary = %q, want \"User account exists\"", preSummary)
	}
	if preCondition != "Given a registered user account exists in the system" {
		t.Errorf("DEMO-P-1 condition = %q, want the seeded condition text", preCondition)
	}

	// Precondition association: DEMO-1's feature "Login" maps to
	// featurePreconditions["Login"] = {0} -> precondition key DEMO-P-1.
	testPres, err := repo.ListTestPreconditions(profileID, "DEMO-1")
	if err != nil {
		t.Fatalf("ListTestPreconditions DEMO-1: %v", err)
	}
	if len(testPres) != 1 || testPres[0].Key != "DEMO-P-1" {
		t.Errorf("DEMO-1 preconditions = %+v, want exactly [DEMO-P-1]", testPres)
	}

	// Container DEMO-TS-1 — Authentication test set (category index 0).
	var tsKind, tsSummary, tsStatus string
	if err := db.QueryRow(
		`SELECT kind, summary, status FROM test_container WHERE profile_id = ? AND jira_key = ?`,
		profileID, "DEMO-TS-1",
	).Scan(&tsKind, &tsSummary, &tsStatus); err != nil {
		t.Fatalf("read DEMO-TS-1: %v", err)
	}
	if tsKind != jira.KindTestSet {
		t.Errorf("DEMO-TS-1 kind = %q, want %q", tsKind, jira.KindTestSet)
	}
	if tsSummary != "Authentication test set" {
		t.Errorf("DEMO-TS-1 summary = %q, want \"Authentication test set\"", tsSummary)
	}
	if tsStatus != "Open" {
		t.Errorf("DEMO-TS-1 status = %q, want Open (demoContainerStatuses[0])", tsStatus)
	}

	// Container membership: DEMO-1 belongs to its category set DEMO-TS-1, plan
	// DEMO-TP-1, and execution DEMO-TE-1 (the deterministic i=0 links). The exec
	// membership carries a non-empty run status.
	memberships, err := repo.ListContainersForTest(profileID, "DEMO-1")
	if err != nil {
		t.Fatalf("ListContainersForTest DEMO-1: %v", err)
	}
	memberKeys := map[string]testrepo.ContainerMembership{}
	for _, m := range memberships {
		memberKeys[m.Key] = m
	}
	for _, want := range []string{"DEMO-TS-1", "DEMO-TP-1", "DEMO-TE-1"} {
		if _, ok := memberKeys[want]; !ok {
			t.Errorf("DEMO-1 missing expected container membership %s (got %v)", want, containerKeysOf(memberships))
		}
	}
	if exec, ok := memberKeys["DEMO-TE-1"]; ok && exec.RunStatus == "" {
		t.Errorf("DEMO-1 membership in DEMO-TE-1 has empty run status, want a seeded run status")
	}

	// Requirement coverage: PRD-1 (r=1) covers linkCount = 1 + (1%4) = 2 tests,
	// testNum = ((1-1)*7 + j*13) % 200 + 1 for j=0,1 -> 1 and 14, i.e. DEMO-1
	// and DEMO-14.
	reqTests, err := repo.ListTestsForRequirement(profileID, "PRD-1")
	if err != nil {
		t.Fatalf("ListTestsForRequirement PRD-1: %v", err)
	}
	gotReqKeys := make([]string, 0, len(reqTests))
	for _, rt := range reqTests {
		gotReqKeys = append(gotReqKeys, rt.Key)
	}
	sort.Strings(gotReqKeys)
	if len(gotReqKeys) != 2 || gotReqKeys[0] != "DEMO-1" || gotReqKeys[1] != "DEMO-14" {
		t.Errorf("PRD-1 covered tests = %v, want [DEMO-1 DEMO-14]", gotReqKeys)
	}

	// Sanity: the full Sync must advance the sync watermark (unlike SyncTests).
	var watermark sql.NullString
	if err := db.QueryRow(
		`SELECT last_synced_at FROM sync_state WHERE profile_id = ?`, profileID,
	).Scan(&watermark); err != nil {
		t.Fatalf("read sync_state: %v", err)
	}
	if !watermark.Valid || watermark.String == "" {
		t.Errorf("full Sync must set the sync watermark, got empty")
	}
}

// containerKeysOf is a small helper for readable failure messages.
func containerKeysOf(ms []testrepo.ContainerMembership) []string {
	keys := make([]string, 0, len(ms))
	for _, m := range ms {
		keys = append(keys, m.Key)
	}
	return keys
}
