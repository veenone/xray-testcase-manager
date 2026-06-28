package coverage

import (
	"testing"
)

// TestGeneratedTemplateRoundTrips proves the invariant behind the download +
// import features: the workbook GenerateTemplateWorkbook produces must parse
// cleanly back through ImportCoverageTemplate. If the two ever drift, this fails.
func TestGeneratedTemplateRoundTrips(t *testing.T) {
	m, st := newTestModule(t)
	const p = "p1"

	// Seed the example test keys the template references so mapping seeding
	// exercises the real path (otherwise they'd all be "skipped").
	for _, k := range []string{"TEST-0001", "TEST-0002", "TEST-0003"} {
		seedTest(t, st, p, k, "PASS", "")
	}

	data, err := GenerateTemplateWorkbook()
	if err != nil {
		t.Fatalf("generate template: %v", err)
	}

	cid, err := m.CreateCanonical(p, "Round-trip", "", "")
	if err != nil {
		t.Fatalf("create canonical: %v", err)
	}

	sum, err := m.ImportCoverageTemplate(p, cid, data)
	if err != nil {
		t.Fatalf("import generated template: %v", err)
	}

	// Parameter Values sheet → Session + Mechanism groups; plus Error Paths and
	// Boundary Conditions = 4 groups, 8 values (4 + 2 + 2).
	if sum.Groups != 4 {
		t.Errorf("groups = %d, want 4 (Session, Mechanism, Error Paths, Boundary Conditions)", sum.Groups)
	}
	if sum.Values != 8 {
		t.Errorf("values = %d, want 8", sum.Values)
	}
	if sum.MappedTests == 0 {
		t.Errorf("mapped tests = 0, want > 0 (TEST-0001/2/3 exist and are referenced)")
	}

	// The model must be readable and non-empty after import.
	model, err := m.GetParamModel(p, cid)
	if err != nil || len(model.Groups) != 4 {
		t.Fatalf("param model after import: err=%v groups=%d", err, len(model.Groups))
	}
}
