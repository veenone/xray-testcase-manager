package coverage

import "testing"

// coverageByCanonicalName returns the coverage percent of the first version of
// the canonical with the given name (assessment canonicals have one version).
func coverageByCanonicalName(t *testing.T, m *Module, profileID, name string) float64 {
	t.Helper()
	canons, _ := m.ListCanonical(profileID)
	for _, c := range canons {
		if c.Name != name {
			continue
		}
		vers, _ := m.ListVersions(profileID, c.ID)
		if len(vers) != 1 {
			t.Fatalf("%s versions = %d, want 1", name, len(vers))
		}
		rep, err := m.ComputeCoverage(profileID, vers[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		return rep.Percent
	}
	t.Fatalf("canonical %q not found", name)
	return 0
}

func TestSeedEUICCASPICEAssessment(t *testing.T) {
	m, _ := newTestModule(t)
	const p = "p1"

	// Stand in for a demo-euicc sync (reused from demoeuicc_test.go).
	seedEUICCSync(t, m, p)

	sum, err := m.SeedEUICCASPICEAssessment(p)
	if err != nil {
		t.Fatalf("assess: %v", err)
	}
	t.Logf("summary: %+v", sum)

	// Seven ASPICE processes seeded; 42 eUICC candidate tests found.
	if sum.Processes != 7 {
		t.Errorf("processes = %d, want 7", sum.Processes)
	}
	if sum.Tests != 42 {
		t.Errorf("tests = %d, want 42 (eUICC candidate pool)", sum.Tests)
	}
	// mappings + gaps must equal the total required values across the 7 models
	// (14+14+13+12+14+13+12 = 92).
	if sum.Mappings+sum.Gaps != 92 {
		t.Errorf("mappings+gaps = %d, want 92 (all required values)", sum.Mappings+sum.Gaps)
	}
	if sum.Mappings == 0 || sum.Gaps == 0 {
		t.Errorf("want a partial assessment (some mapped, some gaps); got %d/%d", sum.Mappings, sum.Gaps)
	}

	// Seven canonicals, each with members = all eUICC CUST-* reqs (21).
	canons, _ := m.ListCanonical(p)
	if len(canons) != 7 {
		t.Fatalf("canonicals = %d, want 7", len(canons))
	}
	for _, c := range canons {
		if c.MemberCount != 21 {
			t.Errorf("%s members = %d, want 21 (all eUICC CUST-* reqs)", c.Name, c.MemberCount)
		}
	}

	// Verdict shape: SWE.4 has no eUICC evidence (0%), SUP.10 is the strongest,
	// and a partial process sits strictly between.
	swe4 := coverageByCanonicalName(t, m, p, "SWE.4 Software Unit Verification")
	if swe4 != 0 {
		t.Errorf("SWE.4 coverage = %v%%, want 0 (no unit tests in eUICC)", swe4)
	}
	sup10 := coverageByCanonicalName(t, m, p, "SUP.10 Change Request Management")
	sys2 := coverageByCanonicalName(t, m, p, "SYS.2 System Requirements Analysis")
	if !(sup10 > sys2 && sys2 > 0 && sup10 < 100) {
		t.Errorf("verdict shape wrong: SUP.10=%v SYS.2=%v (want SUP.10 > SYS.2 > 0, SUP.10 < 100)", sup10, sys2)
	}

	// Every satisfied label must be a real value in that process's model.
	catalog := map[string]map[string]bool{}
	for _, f := range aspiceFeatures() {
		labels := map[string]bool{}
		for _, g := range f.groups {
			for _, v := range g.vals {
				labels[v.label] = true
			}
		}
		catalog[f.code] = labels
	}
	for code, sat := range euiccASPICESatisfied {
		if catalog[code] == nil {
			t.Errorf("euiccASPICESatisfied has unknown process code %q", code)
			continue
		}
		for _, label := range sat {
			if !catalog[code][label] {
				t.Errorf("satisfied label %q not a value in process %q", label, code)
			}
		}
	}

	// Every mapped evidence test is a real eUICC test_case (no stale mappings).
	stale, err := m.DetectStaleMappings(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Errorf("stale mappings = %d, want 0\n%+v", len(stale), stale)
	}

	// Idempotent: re-assess produces the same shape, no duplicate canonicals.
	sum2, err := m.SeedEUICCASPICEAssessment(p)
	if err != nil {
		t.Fatalf("re-assess: %v", err)
	}
	if sum2.Processes != 7 || sum2.Mappings+sum2.Gaps != 92 {
		t.Errorf("re-assess not idempotent: %+v", sum2)
	}
	if canons2, _ := m.ListCanonical(p); len(canons2) != 7 {
		t.Errorf("after re-assess, canonicals = %d, want 7 (no duplicates)", len(canons2))
	}
}
