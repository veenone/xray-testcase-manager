package coverage

import (
	"fmt"
	"testing"
)

// pkcsTestSummaries lists the synced test summaries per feature code, matching
// the format the demo-pkcs backend would produce (summaries start with the
// feature function name so the LIKE query in SeedPKCSReference picks them up).
var pkcsTestSummaries = map[string][]string{
	"SIG": {
		"C_Sign with RSA-2048",
		"C_Sign with SHA256-RSA",
		"C_Sign with ECDSA P-256",
		"C_Sign with SHA256-ECDSA P-256",
		"C_Sign with large 8 MB payload",
		"C_Sign with query-length output mode",
		"C_Sign with undersized buffer",
		"C_Sign with invalid session",
	},
	"KG": {
		"C_GenerateKeyPair RSA-2048",
		"C_GenerateKeyPair RSA-4096",
		"C_GenerateKeyPair EC P-256",
		"C_GenerateKeyPair incomplete template",
		"C_GenerateKeyPair unknown mechanism",
		"C_GenerateKeyPair persistent token keys",
	},
	"VER": {
		"C_Verify RSA-2048 valid signature",
		"C_Verify SHA256-RSA valid signature",
		"C_Verify ECDSA P-256 valid signature",
		"C_Verify tampered signature",
		"C_Verify with invalid session",
	},
}

// seedPKCSSync inserts the rows that a demo-pkcs backend sync would produce so
// that SeedPKCSReference can map onto them. The seeder only reads these rows; it
// must never modify or remove them.
func seedPKCSSync(t *testing.T, m *Module, profileID string) {
	t.Helper()
	db := m.db
	const now = "2024-01-01T00:00:00Z"

	feats := pkcsFeatures()
	for _, f := range feats {
		// Functional requirement (FUNC project) — present in a real sync but not
		// queried by the seeder; included here to represent a realistic store state.
		if _, err := db.Exec(
			`INSERT INTO requirement (profile_id, jira_key, project_key, summary, status, updated_at)
			 VALUES (?, ?, 'FUNC', ?, 'Approved', ?)`,
			profileID, "FUNC-PKCS11-"+f.code, f.summary, now); err != nil {
			t.Fatalf("seed functional req FUNC-PKCS11-%s: %v", f.code, err)
		}

		// Customer requirements — queried by the seeder as canonical members.
		type custReq struct{ key, proj, summary string }
		custReqs := []custReq{
			{"CUST-HSM-BANK-" + f.code, "CUST-HSM-BANK", f.fn + " — BANK customer requirement"},
			{"CUST-HSM-SAMSU-" + f.code, "CUST-HSM-SAMSU", f.fn + " — SAMSU customer requirement"},
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
		summaries := pkcsTestSummaries[f.code]
		for i, summary := range summaries {
			key := fmt.Sprintf("TC-%s-%d", f.code, i+1)
			if _, err := db.Exec(
				`INSERT INTO test_case (profile_id, jira_key, jira_id, summary, status, updated_at)
				 VALUES (?, ?, ?, ?, 'Approved', ?)`,
				profileID, key, fmt.Sprintf("%d%02d", 1, i+1), summary, now); err != nil {
				t.Fatalf("seed test_case %s: %v", key, err)
			}
			// Link each test to both customer requirements so it appears as a candidate.
			for _, cr := range custReqs {
				if _, err := db.Exec(
					`INSERT INTO test_requirement (profile_id, test_key, requirement_key, link_id)
					 VALUES (?, ?, ?, ?)`,
					profileID, key, cr.key, fmt.Sprintf("L-%s-%d-%s", f.code, i+1, cr.proj)); err != nil {
					t.Fatalf("seed test_requirement %s→%s: %v", key, cr.key, err)
				}
			}
		}
	}
}

func TestSeedPKCSReferenceIsConsistent(t *testing.T) {
	m, _ := newTestModule(t)
	const p = "p1"

	// Seed synced-like rows first (stands in for a demo-pkcs backend sync).
	seedPKCSSync(t, m, p)

	sum, err := m.SeedPKCSReference(p)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Logf("summary: %+v", sum)

	// Summary sanity: 3 features, 6 member reqs found (2 per feature),
	// 19 tests found (8+6+5), 6 versions (2 per feature), 3 CRs.
	if sum.Features != 3 {
		t.Errorf("features = %d, want 3", sum.Features)
	}
	if sum.Requirements != 6 {
		t.Errorf("requirements = %d, want 6 (2 CUST reqs per feature)", sum.Requirements)
	}
	if sum.Tests != 19 {
		t.Errorf("tests = %d, want 19 (8+6+5 synced tests)", sum.Tests)
	}
	if sum.Versions != 6 {
		t.Errorf("versions = %d, want 6", sum.Versions)
	}
	if sum.ChangeReqs != 3 {
		t.Errorf("change requests = %d, want 3", sum.ChangeReqs)
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
	if got := count(`SELECT COUNT(*) FROM test_case WHERE profile_id=?`); got != 19 {
		t.Errorf("test_case rows = %d after seed, want 19 (seeder must not write test rows)", got)
	}
	// 3 FUNC + 6 CUST = 9 requirements total.
	if got := count(`SELECT COUNT(*) FROM requirement WHERE profile_id=?`); got != 9 {
		t.Errorf("requirement rows = %d after seed, want 9 (seeder must not write req rows)", got)
	}
	// 19 tests × 2 customer reqs = 38 links.
	if got := count(`SELECT COUNT(*) FROM test_requirement WHERE profile_id=?`); got != 38 {
		t.Errorf("test_requirement rows = %d after seed, want 38", got)
	}

	// Referential integrity: every value→test mapping points at a seeded test.
	stale, err := m.DetectStaleMappings(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Errorf("stale mappings = %d, want 0 (every mapped test must exist in test_case)\n%+v", len(stale), stale)
	}

	// Three canonicals, each with 2 versions, 2 members, a CR, and partial coverage.
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

		// Coverage on the stable (2.40) version is partial because gaps exist.
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
			t.Errorf("%s should have gaps on 2.40", c.Name)
		}

		// Candidate tests resolve: tests are linked to the member customer reqs.
		cands, _ := m.ListCandidateTests(p, c.ID)
		if len(cands) == 0 {
			t.Errorf("%s has no candidate tests", c.Name)
		}

		// One change request with two member decisions (1 can_accept, 1 cannot_accept).
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

	// Idempotent: re-seed produces the same shape, not duplicates.
	sum2, err := m.SeedPKCSReference(p)
	if err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	if sum2.Features != 3 || sum2.Requirements != 6 || sum2.Tests != 19 || sum2.Versions != 6 {
		t.Errorf("re-seed not idempotent: %+v", sum2)
	}
	if canons2, _ := m.ListCanonical(p); len(canons2) != 3 {
		t.Errorf("after re-seed, canonicals = %d, want 3 (no duplicates)", len(canons2))
	}
	// Synced rows must still be untouched after re-seed.
	if got := count(`SELECT COUNT(*) FROM test_case WHERE profile_id=?`); got != 19 {
		t.Errorf("after re-seed, test_case rows = %d, want 19", got)
	}
	if got := count(`SELECT COUNT(*) FROM requirement WHERE profile_id=?`); got != 9 {
		t.Errorf("after re-seed, requirement rows = %d, want 9", got)
	}
	// No stale mappings after re-seed either.
	stale2, _ := m.DetectStaleMappings(p, "")
	if len(stale2) != 0 {
		t.Errorf("after re-seed, stale mappings = %d, want 0", len(stale2))
	}
}
