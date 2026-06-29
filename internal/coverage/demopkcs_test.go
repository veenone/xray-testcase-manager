package coverage

import (
	"testing"
)

func TestSeedPKCSReferenceIsConsistent(t *testing.T) {
	m, st := newTestModule(t)
	const p = "p1"

	sum, err := m.SeedPKCSReference(p)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Logf("summary: %+v", sum)

	// Summary counts: 3 features, 3 func + 6 customer reqs, 8+6+5 tests,
	// 2 versions each (6), 1 CR each (3).
	if sum.Features != 3 {
		t.Errorf("features = %d, want 3", sum.Features)
	}
	if sum.Requirements != 9 {
		t.Errorf("requirements = %d, want 9", sum.Requirements)
	}
	if sum.Tests != 19 {
		t.Errorf("tests = %d, want 19", sum.Tests)
	}
	if sum.Versions != 6 {
		t.Errorf("versions = %d, want 6", sum.Versions)
	}
	if sum.ChangeReqs != 3 {
		t.Errorf("change requests = %d, want 3", sum.ChangeReqs)
	}

	db := st.DB()
	count := func(q string) int {
		var n int
		if err := db.QueryRow(q, p).Scan(&n); err != nil {
			t.Fatalf("count: %v\n%s", err, q)
		}
		return n
	}

	// Jira-cache rows landed.
	if got := count(`SELECT COUNT(*) FROM test_case WHERE profile_id=?`); got != 19 {
		t.Errorf("test_case rows = %d, want 19", got)
	}
	if got := count(`SELECT COUNT(*) FROM requirement WHERE profile_id=?`); got != 9 {
		t.Errorf("requirement rows = %d, want 9", got)
	}
	// Each of 19 tests linked to 2 customer reqs = 38 links.
	if got := count(`SELECT COUNT(*) FROM test_requirement WHERE profile_id=?`); got != 38 {
		t.Errorf("test_requirement rows = %d, want 38", got)
	}
	if got := count(`SELECT COUNT(*) FROM test_container WHERE profile_id=? AND kind='testexec'`); got != 3 {
		t.Errorf("executions = %d, want 3", got)
	}

	// Referential integrity: NO stale mappings — every value→test mapping points
	// at a test that exists in test_case.
	stale, err := m.DetectStaleMappings(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Errorf("stale mappings = %d, want 0 (every mapped test must exist)\n%+v", len(stale), stale)
	}

	// Three canonicals, each with 2 versions, members, a CR, and partial coverage.
	canons, _ := m.ListCanonical(p)
	if len(canons) != 3 {
		t.Fatalf("canonicals = %d, want 3", len(canons))
	}
	for _, c := range canons {
		vers, _ := m.ListVersions(p, c.ID)
		if len(vers) != 2 {
			t.Errorf("%s versions = %d, want 2", c.Name, len(vers))
		}
		if c.MemberCount != 2 {
			t.Errorf("%s members = %d, want 2 (BANK + SAMSU)", c.Name, c.MemberCount)
		}
		// Coverage on the stable (2.40) version is partial (has gaps).
		var v240 string
		for _, vv := range vers {
			if vv.Name == "2.40" {
				v240 = vv.ID
			}
		}
		if v240 == "" {
			t.Errorf("%s missing 2.40 version", c.Name)
			continue
		}
		rep, err := m.ComputeCoverage(p, v240)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Percent <= 0 || rep.Percent >= 100 {
			t.Errorf("%s coverage = %v%%, want partial (gaps present)", c.Name, rep.Percent)
		}
		gaps, _ := m.ListGaps(p, v240)
		if len(gaps) == 0 {
			t.Errorf("%s should have gaps", c.Name)
		}
		// Candidate tests resolve (tests linked to member requirements).
		cands, _ := m.ListCandidateTests(p, c.ID)
		if len(cands) == 0 {
			t.Errorf("%s has no candidate tests", c.Name)
		}
		// One change request with two member decisions.
		crs, _ := m.ListChangeRequests(p, c.ID)
		if len(crs) != 1 {
			t.Errorf("%s change requests = %d, want 1", c.Name, len(crs))
			continue
		}
		imp, _ := m.CRImpact(p, crs[0].ID)
		if imp.CanAccept != 1 || imp.CannotAccept != 1 {
			t.Errorf("%s CR decisions = %d can / %d cannot, want 1/1", c.Name, imp.CanAccept, imp.CannotAccept)
		}
	}

	// Idempotent: re-seed yields the same shape, not duplicates.
	sum2, err := m.SeedPKCSReference(p)
	if err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	if sum2.Features != 3 || sum2.Tests != 19 || sum2.Requirements != 9 {
		t.Errorf("re-seed not idempotent: %+v", sum2)
	}
	if canons2, _ := m.ListCanonical(p); len(canons2) != 3 {
		t.Errorf("after re-seed, canonicals = %d, want 3 (no duplicates)", len(canons2))
	}
	if got := count(`SELECT COUNT(*) FROM test_case WHERE profile_id=?`); got != 19 {
		t.Errorf("after re-seed, test_case rows = %d, want 19 (cleared+reinserted)", got)
	}
}
