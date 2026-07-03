package coverage

import "testing"

// Reproduction for "demo-pkcs Coverage Map shows nothing". Drives the real
// flow: a demo-pkcs Sync populates requirements/tests (seedPKCSSync), and the
// "Load PKCS#11 coverage" action (SeedPKCSReference) builds the coverage layer.
// The Coverage Map reads ListProjects / ProjectCoverage / ProjectRelationSankey.
func TestCoverageMapAfterDemoPKCSSeed(t *testing.T) {
	m, _ := newTestModule(t)
	const p = "p1"

	// Step 1: sync-only — what a demo-pkcs Sync writes. No coverage layer yet.
	seedPKCSSync(t, m, p)

	preProjects, _ := m.ListProjects(p)
	preCov, _ := m.ProjectCoverage(p)
	t.Logf("BEFORE Load coverage: ListProjects=%d  ProjectCoverage rows=%d", len(preProjects), len(preCov))

	// Step 2: "Load PKCS#11 coverage".
	if _, err := m.SeedPKCSReference(p); err != nil {
		t.Fatalf("SeedPKCSReference: %v", err)
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
