package coverage

import (
	"fmt"
	"testing"
)

// aspiceTestSummaries lists synced test summaries per process code, matching the
// format the demo-aspice backend would produce (summaries start with the process
// name so the LIKE query in SeedASPICEReference picks them up).
var aspiceTestSummaries = map[string][]string{
	"SRA": {
		"SYS.2 System Requirements Analysis — requirement specified and structured",
		"SYS.2 System Requirements Analysis — verification criteria defined",
		"SYS.2 System Requirements Analysis — bidirectional trace to stakeholder needs",
		"SYS.2 System Requirements Analysis — feasibility analyzed",
		"SYS.2 System Requirements Analysis — baselined and communicated",
		"SYS.2 System Requirements Analysis — missing verification criterion",
	},
	"SQT": {
		"SYS.5 System Qualification Test — strategy and regression defined",
		"SYS.5 System Qualification Test — cases specified from system requirements",
		"SYS.5 System Qualification Test — integrated system executed",
		"SYS.5 System Qualification Test — bidirectional trace req to test",
		"SYS.5 System Qualification Test — results summarized",
		"SYS.5 System Qualification Test — failure raised as problem",
	},
	"SWR": {
		"SWE.1 Software Requirements Analysis — requirements specified",
		"SWE.1 Software Requirements Analysis — functional/non-functional structured",
		"SWE.1 Software Requirements Analysis — verification criteria defined",
		"SWE.1 Software Requirements Analysis — trace system to software",
		"SWE.1 Software Requirements Analysis — baselined",
		"SWE.1 Software Requirements Analysis — timing constraints captured",
	},
	"SUV": {
		"SWE.4 Software Unit Verification — strategy and criteria defined",
		"SWE.4 Software Unit Verification — code reviews performed",
		"SWE.4 Software Unit Verification — static analysis run",
		"SWE.4 Software Unit Verification — units tested with branch coverage",
		"SWE.4 Software Unit Verification — trace design to unit test",
		"SWE.4 Software Unit Verification — results communicated",
	},
	"SWQ": {
		"SWE.6 Software Qualification Test — strategy and regression defined",
		"SWE.6 Software Qualification Test — cases from software requirements",
		"SWE.6 Software Qualification Test — integrated software executed",
		"SWE.6 Software Qualification Test — bidirectional trace req to test",
		"SWE.6 Software Qualification Test — results summarized",
		"SWE.6 Software Qualification Test — failure raised as problem",
	},
	"PRM": {
		"SUP.9 Problem Resolution Management — problem identified and recorded",
		"SUP.9 Problem Resolution Management — status recorded",
		"SUP.9 Problem Resolution Management — root cause determined",
		"SUP.9 Problem Resolution Management — trace problem to change request",
		"SUP.9 Problem Resolution Management — affected parties alerted",
		"SUP.9 Problem Resolution Management — corrective action verified",
	},
	"CRM": {
		"SUP.10 Change Request Management — CR identified and recorded",
		"SUP.10 Change Request Management — status recorded",
		"SUP.10 Change Request Management — impact and dependencies analyzed",
		"SUP.10 Change Request Management — approval before implementation",
		"SUP.10 Change Request Management — implementation reviewed",
		"SUP.10 Change Request Management — trace CR to affected work-products",
	},
}

// seedASPICESync inserts the rows that a demo-aspice backend sync would produce
// so that SeedASPICEReference can map onto them. The seeder only reads these
// rows; it must never modify or remove them.
func seedASPICESync(t *testing.T, m *Module, profileID string) {
	t.Helper()
	db := m.db
	const now = "2024-01-01T00:00:00Z"

	feats := aspiceFeatures()
	for _, f := range feats {
		// Functional (system-tier) requirement (FUNC project) — present in a real
		// sync but not queried by the seeder; included here for realistic state.
		if _, err := db.Exec(
			`INSERT INTO requirement (profile_id, jira_key, project_key, summary, status, updated_at)
			 VALUES (?, ?, 'FUNC', ?, 'Approved', ?)`,
			profileID, "FUNC-ASPICE-"+f.code, f.summary, now); err != nil {
			t.Fatalf("seed functional req FUNC-ASPICE-%s: %v", f.code, err)
		}

		// Program (software-tier) requirements — queried by the seeder as members.
		type custReq struct{ key, proj, summary string }
		custReqs := []custReq{
			{"CUST-OEM-PLATFORM-" + f.code, "CUST-OEM-PLATFORM", f.fn + " — OEM program requirement"},
			{"CUST-TIER1-ECU-" + f.code, "CUST-TIER1-ECU", f.fn + " — TIER1 program requirement"},
			{"CUST-SAFETY-DOMAIN-" + f.code, "CUST-SAFETY-DOMAIN", f.fn + " — SAFETY program requirement"},
		}
		for _, cr := range custReqs {
			if _, err := db.Exec(
				`INSERT INTO requirement (profile_id, jira_key, project_key, summary, status, updated_at)
				 VALUES (?, ?, ?, ?, 'In Progress', ?)`,
				profileID, cr.key, cr.proj, cr.summary, now); err != nil {
				t.Fatalf("seed program req %s: %v", cr.key, err)
			}
		}

		// Test cases — queried by the seeder as the candidate pool for value mappings.
		summaries := aspiceTestSummaries[f.code]
		for i, summary := range summaries {
			key := fmt.Sprintf("TC-ASPICE-%s-%d", f.code, i+1)
			if _, err := db.Exec(
				`INSERT INTO test_case (profile_id, jira_key, jira_id, summary, status, updated_at)
				 VALUES (?, ?, ?, ?, 'Approved', ?)`,
				profileID, key, fmt.Sprintf("%d%02d", 3, i+1), summary, now); err != nil {
				t.Fatalf("seed test_case %s: %v", key, err)
			}
			// Link each test to all three program requirements so it appears as a candidate.
			for _, cr := range custReqs {
				if _, err := db.Exec(
					`INSERT INTO test_requirement (profile_id, test_key, requirement_key, link_id)
					 VALUES (?, ?, ?, ?)`,
					profileID, key, cr.key, fmt.Sprintf("L-ASPICE-%s-%d-%s", f.code, i+1, cr.proj)); err != nil {
					t.Fatalf("seed test_requirement %s→%s: %v", key, cr.key, err)
				}
			}
		}
	}
}

func TestSeedASPICEReferenceIsConsistent(t *testing.T) {
	m, _ := newTestModule(t)
	const p = "p1"

	// Seed synced-like rows first (stands in for a demo-aspice backend sync).
	seedASPICESync(t, m, p)

	sum, err := m.SeedASPICEReference(p)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Logf("summary: %+v", sum)

	// Summary sanity: 7 processes, 21 member reqs (3 per process),
	// 42 tests (6 per process), 14 versions (2 per process), 7 CRs.
	if sum.Features != 7 {
		t.Errorf("features = %d, want 7", sum.Features)
	}
	if sum.Requirements != 21 {
		t.Errorf("requirements = %d, want 21 (3 CUST reqs per process)", sum.Requirements)
	}
	if sum.Tests != 42 {
		t.Errorf("tests = %d, want 42 (6 tests per process)", sum.Tests)
	}
	if sum.Versions != 14 {
		t.Errorf("versions = %d, want 14 (2 per process)", sum.Versions)
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
	// 7 processes × 6 tests each = 42 test_case rows.
	if got := count(`SELECT COUNT(*) FROM test_case WHERE profile_id=?`); got != 42 {
		t.Errorf("test_case rows = %d after seed, want 42 (seeder must not write test rows)", got)
	}
	// 7 FUNC + 21 CUST = 28 requirements total.
	if got := count(`SELECT COUNT(*) FROM requirement WHERE profile_id=?`); got != 28 {
		t.Errorf("requirement rows = %d after seed, want 28 (seeder must not write req rows)", got)
	}
	// 42 tests × 3 program reqs each = 126 test_requirement links.
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
			t.Errorf("%s members = %d, want 3 (OEM-PLATFORM + TIER1-ECU + SAFETY-DOMAIN)", c.Name, c.MemberCount)
		}

		// Coverage on the stable (3.1) version is partial because gaps exist.
		var v31 string
		for _, vv := range vers {
			if vv.Name == "3.1" {
				v31 = vv.ID
			}
		}
		if v31 == "" {
			t.Errorf("%s missing 3.1 version", c.Name)
			continue
		}
		rep, err := m.ComputeCoverage(p, v31)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Percent <= 0 || rep.Percent >= 100 {
			t.Errorf("%s coverage = %v%%, want partial (gaps present)", c.Name, rep.Percent)
		}
		gaps, _ := m.ListGaps(p, v31)
		if len(gaps) == 0 {
			t.Errorf("%s should have gaps on 3.1", c.Name)
		}

		// Candidate tests resolve: tests are linked to the member program reqs.
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
	sum2, err := m.SeedASPICEReference(p)
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

func TestCoverageMapAfterDemoASPICESeed(t *testing.T) {
	m, _ := newTestModule(t)
	const p = "p1"

	// Step 1: sync-only — what a demo-aspice Sync writes. No coverage layer yet.
	seedASPICESync(t, m, p)

	preProjects, _ := m.ListProjects(p)
	preCov, _ := m.ProjectCoverage(p)
	t.Logf("BEFORE Load coverage: ListProjects=%d  ProjectCoverage rows=%d", len(preProjects), len(preCov))

	// Step 2: "Load ASPICE coverage".
	if _, err := m.SeedASPICEReference(p); err != nil {
		t.Fatalf("SeedASPICEReference: %v", err)
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
