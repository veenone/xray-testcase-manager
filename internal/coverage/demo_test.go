package coverage

import (
	"fmt"
	"testing"
)

func TestSeedDemoExampleAlignedWithDemoData(t *testing.T) {
	m, st := newTestModule(t)
	const p = "p1"

	// Seed demo-shaped tests (DEMO-1..400 so every referenced Login test exists)
	// and the demo's "Login requirement" PRD-1.
	for i := 1; i <= 400; i++ {
		seedTest(t, st, p, fmt.Sprintf("DEMO-%d", i), "PASS", "")
	}
	if _, err := st.DB().Exec(
		`INSERT INTO requirement (profile_id, jira_key, project_key, summary) VALUES (?, 'PRD-1', 'PRD', 'Login requirement')`,
		p); err != nil {
		t.Fatal(err)
	}

	cid, err := m.SeedDemoExample(p)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	rep, err := m.ComputeCoverage(p, cid)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if rep.TotalValues != 12 {
		t.Errorf("total values = %d, want 12", rep.TotalValues)
	}
	if rep.TestedValues != 10 {
		t.Errorf("tested values = %d, want 10", rep.TestedValues)
	}
	if rep.Percent != 83.3 {
		t.Errorf("percent = %v, want 83.3", rep.Percent)
	}

	gaps, _ := m.ListGaps(p, cid)
	if len(gaps) != 2 {
		t.Errorf("gaps = %d, want 2", len(gaps))
	}

	// The mapped tests must be the actual demo Login tests (DEMO-1, 31, …).
	model, _ := m.GetParamModel(p, cid)
	var validCredsVID string
	for _, g := range model.Groups {
		for _, pr := range g.Parameters {
			for _, v := range pr.Values {
				if v.ValueLabel == "Valid credentials" {
					validCredsVID = v.ID
				}
			}
		}
	}
	keys, _ := m.ListValueTests(p, validCredsVID)
	if len(keys) != 1 || keys[0] != "DEMO-1" {
		t.Errorf("Valid credentials maps to %v, want [DEMO-1]", keys)
	}

	// Reuse populated with the Login requirement.
	reuse, _ := m.ListReuse(p, cid)
	if len(reuse) != 1 || reuse[0].RequirementKey != "PRD-1" {
		t.Errorf("members = %+v, want [PRD-1]", reuse)
	}

	// Idempotent re-seed.
	if _, err := m.SeedDemoExample(p); err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	list, _ := m.ListCanonical(p)
	demoCount := 0
	for _, c := range list {
		if c.Name == demoCanonName {
			demoCount++
		}
	}
	if demoCount != 1 {
		t.Errorf("demo canonicals = %d, want 1", demoCount)
	}
}
