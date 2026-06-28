package coverage

import (
	"os"
	"path/filepath"
	"testing"
)

// TestImportRealTemplate imports the actual PKCS#11 C_Sign workbook and checks
// the model + coverage come out sane. Seeds a few of the referenced tests so
// some mappings stick; the rest are reported as skipped (unknown locally).
func TestImportRealTemplate(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "csign_template.xlsx"))
	if err != nil {
		t.Skipf("template fixture missing: %v", err)
	}
	m, st := newTestModule(t)
	const p = "p1"

	// Seed two real tests from the workbook so mapping seeding has something to
	// match (TEST-4501 covers RSA_PKCS; TEST-4511 covers CKR_SESSION_CLOSED).
	seedTest(t, st, p, "TEST-4501", "PASS", "")
	seedTest(t, st, p, "TEST-4511", "PASS", "")

	cid, err := m.CreateCanonical(p, "C_Sign", "PKCS11", "")
	if err != nil {
		t.Fatalf("create canonical: %v", err)
	}

	sum, err := m.ImportCoverageTemplate(p, cid, data)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	t.Logf("import summary: %+v", sum)

	// 4 groups from Parameter Values (Session/Mechanism/Data/Output) + Error
	// Paths + Boundary Conditions = 6.
	if sum.Groups != 6 {
		t.Errorf("groups = %d, want 6", sum.Groups)
	}
	// Parameter Values sheet has 21 values; Error Paths 18. (Boundary rows are
	// imported as non-required, so the required denominator is 21+18 = 39.)
	if sum.Values < 39 {
		t.Errorf("values = %d, want >= 39", sum.Values)
	}
	if sum.MappedTests < 2 {
		t.Errorf("mapped tests = %d, want >= 2 (TEST-4501, TEST-4511)", sum.MappedTests)
	}

	// Re-import must be idempotent (replace, not duplicate).
	sum2, err := m.ImportCoverageTemplate(p, cid, data)
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if sum2.Groups != sum.Groups || sum2.Values != sum.Values {
		t.Errorf("re-import not idempotent: first=%d/%d second=%d/%d",
			sum.Groups, sum.Values, sum2.Groups, sum2.Values)
	}

	rep, err := m.ComputeCoverage(p, cid)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	t.Logf("coverage: %.1f%% (%d/%d required values across %d groups)",
		rep.Percent, rep.TestedValues, rep.TotalValues, len(rep.Groups))
	if rep.TotalValues < 39 {
		t.Errorf("required values = %d, want >= 39", rep.TotalValues)
	}
	if rep.Percent <= 0 || rep.Percent >= 100 {
		t.Errorf("percent = %v, want a partial coverage between 0 and 100", rep.Percent)
	}
}
