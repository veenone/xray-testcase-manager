package coverage

import (
	"fmt"
	"testing"
)

// euiccTestSummaries lists synced test summaries per feature code, matching the
// format the demo-euicc backend would produce (summaries start with the procedure
// name so the LIKE query in SeedEUICCReference picks them up).
var euiccTestSummaries = map[string][]string{
	"DLD": {
		"Profile Download basic activation code flow",
		"Profile Download SM-DS event trigger",
		"Profile Download eIM-triggered SGP.32",
		"Profile Download segmented BPP delivery",
		"Profile Download EID mismatch error",
		"Profile Download insufficient eUICC memory",
	},
	"ENA": {
		"Enable Profile from Disabled state via ICCID",
		"Enable Profile via ISD-P AID",
		"Enable Profile with refreshFlag=true REFRESH",
		"Enable Profile with refreshFlag=false",
		"Enable Profile blocked by policy violation",
		"Enable Profile ICCID not found",
	},
	"DIS": {
		"Disable Profile from Enabled state",
		"Disable Profile with refreshFlag=true",
		"Disable Profile with fall-back profile active",
		"Disable Profile policy violation POL1",
		"Disable Profile ICCID not found",
		"Disable Profile fall-back becomes Enabled",
	},
	"DEL": {
		"Delete Profile from Disabled state",
		"Delete Profile must disable first error",
		"Delete Profile ICCID not found",
		"Delete Profile allowed by policy",
		"Delete Profile blocked by POL1",
		"Delete Profile operator profile type",
	},
	"RST": {
		"eUICC Memory Reset operational profiles only",
		"eUICC Memory Reset factory reset keep provisioning",
		"eUICC Memory Reset authorized via LPA confirm",
		"eUICC Memory Reset unauthorized attempt",
		"eUICC Memory Reset invalid scope",
		"eUICC Memory Reset nothing to reset",
	},
	"FBK": {
		"Profile Fall-Back attribute set on one profile",
		"Profile Fall-Back not configured",
		"Profile Fall-Back connectivity loss trigger",
		"Profile Fall-Back SM-DS unreachable trigger",
		"Profile Fall-Back NB-IoT bearer",
		"Profile Fall-Back LTE-M bearer",
	},
	"RBK": {
		"Profile Enable with Rollback valid Disabled profile",
		"Profile Enable with Rollback REFRESH failed",
		"Profile Enable with Rollback no network after enable",
		"Profile Enable with Rollback previous profile retained",
		"Profile Enable with Rollback eUICC busy",
		"Profile Enable with Rollback network check failure",
	},
}

// seedEUICCSync inserts the rows that a demo-euicc backend sync would produce so
// that SeedEUICCReference can map onto them. The seeder only reads these rows; it
// must never modify or remove them.
func seedEUICCSync(t *testing.T, m *Module, profileID string) {
	t.Helper()
	db := m.db
	const now = "2024-01-01T00:00:00Z"

	feats := euiccFeatures()
	for _, f := range feats {
		// Functional requirement (FUNC project) — present in a real sync but not
		// queried by the seeder; included here to represent a realistic store state.
		if _, err := db.Exec(
			`INSERT INTO requirement (profile_id, jira_key, project_key, summary, status, updated_at)
			 VALUES (?, ?, 'FUNC', ?, 'Approved', ?)`,
			profileID, "FUNC-EUICC-"+f.code, f.summary, now); err != nil {
			t.Fatalf("seed functional req FUNC-EUICC-%s: %v", f.code, err)
		}

		// Customer requirements — queried by the seeder as canonical members.
		type custReq struct{ key, proj, summary string }
		custReqs := []custReq{
			{"CUST-MNO-CONSUMER-" + f.code, "CUST-MNO-CONSUMER", f.fn + " — MNO customer requirement"},
			{"CUST-IOT-FLEET-" + f.code, "CUST-IOT-FLEET", f.fn + " — IOT customer requirement"},
			{"CUST-M2M-AUTO-" + f.code, "CUST-M2M-AUTO", f.fn + " — M2M customer requirement"},
		}
		for _, cr := range custReqs {
			if _, err := db.Exec(
				`INSERT INTO requirement (profile_id, jira_key, project_key, summary, status, updated_at)
				 VALUES (?, ?, ?, ?, 'In Progress', ?)`,
				profileID, cr.key, cr.proj, cr.summary, now); err != nil {
				t.Fatalf("seed customer req %s: %v", cr.key, err)
			}
		}

		// Test cases — queried by the seeder as the candidate pool for value mappings.
		summaries := euiccTestSummaries[f.code]
		for i, summary := range summaries {
			key := fmt.Sprintf("TC-EUICC-%s-%d", f.code, i+1)
			if _, err := db.Exec(
				`INSERT INTO test_case (profile_id, jira_key, jira_id, summary, status, updated_at)
				 VALUES (?, ?, ?, ?, 'Approved', ?)`,
				profileID, key, fmt.Sprintf("%d%02d", 2, i+1), summary, now); err != nil {
				t.Fatalf("seed test_case %s: %v", key, err)
			}
			// Link each test to all three customer requirements so it appears as a candidate.
			for _, cr := range custReqs {
				if _, err := db.Exec(
					`INSERT INTO test_requirement (profile_id, test_key, requirement_key, link_id)
					 VALUES (?, ?, ?, ?)`,
					profileID, key, cr.key, fmt.Sprintf("L-EUICC-%s-%d-%s", f.code, i+1, cr.proj)); err != nil {
					t.Fatalf("seed test_requirement %s→%s: %v", key, cr.key, err)
				}
			}
		}
	}
}

func TestSeedEUICCReferenceIsConsistent(t *testing.T) {
	m, _ := newTestModule(t)
	const p = "p1"

	// Seed synced-like rows first (stands in for a demo-euicc backend sync).
	seedEUICCSync(t, m, p)

	sum, err := m.SeedEUICCReference(p)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Logf("summary: %+v", sum)

	// Summary sanity: 7 features, 21 member reqs (3 per feature),
	// 42 tests (6 per feature), 14 versions (2 per feature), 7 CRs.
	if sum.Features != 7 {
		t.Errorf("features = %d, want 7", sum.Features)
	}
	if sum.Requirements != 21 {
		t.Errorf("requirements = %d, want 21 (3 CUST reqs per feature)", sum.Requirements)
	}
	if sum.Tests != 42 {
		t.Errorf("tests = %d, want 42 (6 tests per feature)", sum.Tests)
	}
	if sum.Versions != 14 {
		t.Errorf("versions = %d, want 14 (2 per feature)", sum.Versions)
	}
	if sum.ChangeReqs != 7 {
		t.Errorf("change requests = %d, want 7", sum.ChangeReqs)
	}

	db := m.db
	count := func(q string) int {
		var n int
		if err := db.QueryRow(q, p).Scan(&n); err != nil {
			t.Fatalf("count: %v\n%s", err, q)
		}
		return n
	}

	// Seeder must not add or remove synced rows — counts stay exactly as seeded.
	// 7 features × 6 tests each = 42 test_case rows.
	if got := count(`SELECT COUNT(*) FROM test_case WHERE profile_id=?`); got != 42 {
		t.Errorf("test_case rows = %d after seed, want 42 (seeder must not write test rows)", got)
	}
	// 7 FUNC + 21 CUST = 28 requirements total.
	if got := count(`SELECT COUNT(*) FROM requirement WHERE profile_id=?`); got != 28 {
		t.Errorf("requirement rows = %d after seed, want 28 (seeder must not write req rows)", got)
	}
	// 42 tests × 3 customer reqs each = 126 test_requirement links.
	if got := count(`SELECT COUNT(*) FROM test_requirement WHERE profile_id=?`); got != 126 {
		t.Errorf("test_requirement rows = %d after seed, want 126", got)
	}

	// Referential integrity: every value->test mapping points at a seeded test.
	stale, err := m.DetectStaleMappings(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Errorf("stale mappings = %d, want 0 (every mapped test must exist in test_case)\n%+v", len(stale), stale)
	}

	// Seven canonicals, each with 2 versions, 3 members, a CR, and partial coverage.
	canons, _ := m.ListCanonical(p)
	if len(canons) != 7 {
		t.Fatalf("canonicals = %d, want 7", len(canons))
	}
	for _, c := range canons {
		vers, _ := m.ListVersions(p, c.ID)
		if len(vers) != 2 {
			t.Errorf("%s versions = %d, want 2", c.Name, len(vers))
		}
		if c.MemberCount != 3 {
			t.Errorf("%s members = %d, want 3 (MNO-CONSUMER + IOT-FLEET + M2M-AUTO)", c.Name, c.MemberCount)
		}

		// Coverage on the stable (2.4) version is partial because gaps exist.
		var v24 string
		for _, vv := range vers {
			if vv.Name == "2.4" {
				v24 = vv.ID
			}
		}
		if v24 == "" {
			t.Errorf("%s missing 2.4 version", c.Name)
			continue
		}
		rep, err := m.ComputeCoverage(p, v24)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Percent <= 0 || rep.Percent >= 100 {
			t.Errorf("%s coverage = %v%%, want partial (gaps present)", c.Name, rep.Percent)
		}
		gaps, _ := m.ListGaps(p, v24)
		if len(gaps) == 0 {
			t.Errorf("%s should have gaps on 2.4", c.Name)
		}

		// Candidate tests resolve: tests are linked to the member customer reqs.
		cands, _ := m.ListCandidateTests(p, c.ID)
		if len(cands) == 0 {
			t.Errorf("%s has no candidate tests", c.Name)
		}

		// One change request with three member decisions (2 can_accept, 1 cannot_accept).
		crs, _ := m.ListChangeRequests(p, c.ID)
		if len(crs) != 1 {
			t.Errorf("%s change requests = %d, want 1", c.Name, len(crs))
			continue
		}
		imp, _ := m.CRImpact(p, crs[0].ID)
		if imp.CanAccept != 2 || imp.CannotAccept != 1 {
			t.Errorf("%s CR decisions = %d can / %d cannot, want 2/1", c.Name, imp.CanAccept, imp.CannotAccept)
		}
	}

	// Idempotent: re-seed produces the same shape, not duplicates.
	sum2, err := m.SeedEUICCReference(p)
	if err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	if sum2.Features != 7 || sum2.Requirements != 21 || sum2.Tests != 42 || sum2.Versions != 14 {
		t.Errorf("re-seed not idempotent: %+v", sum2)
	}
	if canons2, _ := m.ListCanonical(p); len(canons2) != 7 {
		t.Errorf("after re-seed, canonicals = %d, want 7 (no duplicates)", len(canons2))
	}
	// Synced rows must still be untouched after re-seed.
	if got := count(`SELECT COUNT(*) FROM test_case WHERE profile_id=?`); got != 42 {
		t.Errorf("after re-seed, test_case rows = %d, want 42", got)
	}
	if got := count(`SELECT COUNT(*) FROM requirement WHERE profile_id=?`); got != 28 {
		t.Errorf("after re-seed, requirement rows = %d, want 28", got)
	}
	// No stale mappings after re-seed either.
	stale2, _ := m.DetectStaleMappings(p, "")
	if len(stale2) != 0 {
		t.Errorf("after re-seed, stale mappings = %d, want 0", len(stale2))
	}
}

func TestCoverageMapAfterDemoEUICCSeed(t *testing.T) {
	m, _ := newTestModule(t)
	const p = "p1"

	// Step 1: sync-only — what a demo-euicc Sync writes. No coverage layer yet.
	seedEUICCSync(t, m, p)

	preProjects, _ := m.ListProjects(p)
	preCov, _ := m.ProjectCoverage(p)
	t.Logf("BEFORE Load coverage: ListProjects=%d  ProjectCoverage rows=%d", len(preProjects), len(preCov))

	// Step 2: "Load eUICC coverage".
	if _, err := m.SeedEUICCReference(p); err != nil {
		t.Fatalf("SeedEUICCReference: %v", err)
	}

	projects, _ := m.ListProjects(p)
	cov, err := m.ProjectCoverage(p)
	if err != nil {
		t.Fatal(err)
	}
	sankey, _ := m.ProjectRelationSankey(p)
	t.Logf("AFTER Load coverage: ListProjects=%d  ProjectCoverage rows=%d  Sankey nodes=%d links=%d",
		len(projects), len(cov), len(sankey.Nodes), len(sankey.Links))
	for _, r := range cov {
		t.Logf("  project %s (%s): %d/%d = %.1f%%, %d reqs, %d funcs",
			r.ProjectKey, r.Role, r.CoveredValues, r.TotalValues, r.Percent, r.RequirementCount, r.FunctionsReused)
	}

	if len(cov) == 0 {
		t.Errorf("ProjectCoverage empty AFTER seeding — Coverage Map blank (CODE BUG)")
	}
	if len(sankey.Links) == 0 {
		t.Errorf("ProjectRelationSankey empty AFTER seeding (CODE BUG)")
	}
}
