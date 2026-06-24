package testrepo_test

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"xray-test-manager/internal/testrepo"
)

// junitXML builds a minimal JUnit XML document containing the given testcases.
// Each entry in tcs is a raw <testcase ...> element (no wrapping needed).
func buildJUnitXML(tcs []string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	b.WriteString("<testsuites>")
	b.WriteString(`<testsuite name="suite">`)
	for _, tc := range tcs {
		b.WriteString(tc)
	}
	b.WriteString("</testsuite>")
	b.WriteString("</testsuites>")
	return b.String()
}

// seedJUnitRepo seeds 7 member tests and one execution with those members.
//
//   - TC-1  "Pass Test"      - will map to PASS
//   - TC-2  "Fail Test"      - will map to FAIL (via <failure>)
//   - TC-3  "Error Test"     - will map to FAIL (via <error>)
//   - TC-4  "Skipped Test"   - skipped in the XML
//   - TC-5  "Unique Test"    - not present in the XML
//   - TC-6  "Ambiguous Test" - two members share this summary
//   - TC-7  "Ambiguous Test" - twin of TC-6
func seedJUnitRepo(t *testing.T) *testrepo.Repository {
	t.Helper()
	repo := newRepo(t)

	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "TC-1", ID: "1", Summary: "Pass Test"},
		{Key: "TC-2", ID: "2", Summary: "Fail Test"},
		{Key: "TC-3", ID: "3", Summary: "Error Test"},
		{Key: "TC-4", ID: "4", Summary: "Skipped Test"},
		{Key: "TC-5", ID: "5", Summary: "Unique Test"},
		{Key: "TC-6", ID: "6", Summary: "Ambiguous Test"},
		{Key: "TC-7", ID: "7", Summary: "Ambiguous Test"},
	}); err != nil {
		t.Fatalf("seed tests: %v", err)
	}

	if err := repo.UpsertContainers("p1", []testrepo.Container{
		{Key: "TE-1", Kind: "testexec", Summary: "JUnit Target Execution"},
	}); err != nil {
		t.Fatalf("seed container: %v", err)
	}

	if err := repo.ReplaceAllContainerLinks("p1", []testrepo.ContainerLink{
		{ContainerKey: "TE-1", TestKey: "TC-1", RunStatus: "TODO"},
		{ContainerKey: "TE-1", TestKey: "TC-2", RunStatus: "TODO"},
		{ContainerKey: "TE-1", TestKey: "TC-3", RunStatus: "TODO"},
		{ContainerKey: "TE-1", TestKey: "TC-4", RunStatus: "TODO"},
		{ContainerKey: "TE-1", TestKey: "TC-5", RunStatus: "TODO"},
		{ContainerKey: "TE-1", TestKey: "TC-6", RunStatus: "TODO"},
		{ContainerKey: "TE-1", TestKey: "TC-7", RunStatus: "TODO"},
	}); err != nil {
		t.Fatalf("seed links: %v", err)
	}

	return repo
}

// buildJUnitBase64 returns a base64-encoded JUnit XML with the 6 test cases
// that exercise all classification branches.
func buildJUnitBase64() string {
	tcs := []string{
		`<testcase name="Pass Test"/>`,
		`<testcase name="Fail Test"><failure message="assert failed">details</failure></testcase>`,
		`<testcase name="Error Test"><error message="exception">stack</error></testcase>`,
		`<testcase name="Skipped Test"><skipped/></testcase>`,
		`<testcase name="No Such Test"/>`,
		`<testcase name="Ambiguous Test"/>`,
	}
	return base64.StdEncoding.EncodeToString([]byte(buildJUnitXML(tcs)))
}

func TestAnalyzeJUnitImport(t *testing.T) {
	repo := seedJUnitRepo(t)
	xmlB64 := buildJUnitBase64()

	preview, err := repo.AnalyzeJUnitImport("p1", "TE-1", xmlB64)
	if err != nil {
		t.Fatalf("AnalyzeJUnitImport: %v", err)
	}

	if preview.ExecKey != "TE-1" {
		t.Errorf("ExecKey = %q, want TE-1", preview.ExecKey)
	}
	if preview.Total != 6 {
		t.Errorf("Total = %d, want 6", preview.Total)
	}
	if len(preview.Matched) != 3 {
		t.Errorf("Matched count = %d, want 3; matched = %+v", len(preview.Matched), preview.Matched)
	}
	if len(preview.Skipped) != 3 {
		t.Errorf("Skipped count = %d, want 3; skipped = %+v", len(preview.Skipped), preview.Skipped)
	}

	// Verify the three matched entries.
	matched := make(map[string]testrepo.JUnitMatch)
	for _, m := range preview.Matched {
		matched[m.Testcase] = m
	}

	if m, ok := matched["Pass Test"]; !ok {
		t.Error("Pass Test not in matched")
	} else {
		if m.Result != "PASS" {
			t.Errorf("Pass Test result = %q, want PASS", m.Result)
		}
		if m.TestKey != "TC-1" {
			t.Errorf("Pass Test key = %q, want TC-1", m.TestKey)
		}
	}
	if m, ok := matched["Fail Test"]; !ok {
		t.Error("Fail Test not in matched")
	} else if m.Result != "FAIL" {
		t.Errorf("Fail Test result = %q, want FAIL", m.Result)
	}
	if m, ok := matched["Error Test"]; !ok {
		t.Error("Error Test not in matched")
	} else if m.Result != "FAIL" {
		t.Errorf("Error Test result = %q, want FAIL", m.Result)
	}

	// Verify the three skipped entries.
	skipped := make(map[string]testrepo.JUnitSkip)
	for _, s := range preview.Skipped {
		skipped[s.Testcase] = s
	}

	if s, ok := skipped["Skipped Test"]; !ok {
		t.Error("Skipped Test not in skipped list")
	} else if !strings.Contains(s.Reason, "skipped in the report") {
		t.Errorf("Skipped Test reason = %q, want to contain 'skipped in the report'", s.Reason)
	}
	if s, ok := skipped["No Such Test"]; !ok {
		t.Error("No Such Test not in skipped list")
	} else if !strings.Contains(s.Reason, "no matching test") {
		t.Errorf("No Such Test reason = %q, want to contain 'no matching test'", s.Reason)
	}
	if s, ok := skipped["Ambiguous Test"]; !ok {
		t.Error("Ambiguous Test not in skipped list")
	} else if !strings.Contains(s.Reason, "ambiguous") {
		t.Errorf("Ambiguous Test reason = %q, want to contain 'ambiguous'", s.Reason)
	}
}

func TestApplyJUnitImport(t *testing.T) {
	repo := seedJUnitRepo(t)
	xmlB64 := buildJUnitBase64()

	preview, err := repo.AnalyzeJUnitImport("p1", "TE-1", xmlB64)
	if err != nil {
		t.Fatalf("AnalyzeJUnitImport: %v", err)
	}
	if len(preview.Matched) != 3 {
		t.Fatalf("expected 3 matches before Apply, got %d", len(preview.Matched))
	}

	result, err := repo.ApplyJUnitImport("p1", "TE-1", preview.Matched)
	if err != nil {
		t.Fatalf("ApplyJUnitImport: %v", err)
	}

	if len(result.Succeeded) != 3 {
		t.Errorf("Succeeded = %d, want 3; failures = %+v", len(result.Succeeded), result.Failed)
	}
	if len(result.Failed) != 0 {
		t.Errorf("Failed = %d, want 0; failures = %+v", len(result.Failed), result.Failed)
	}

	// Verify actual run statuses via GetContainerBoard.
	board, err := repo.GetContainerBoard("p1", "TE-1")
	if err != nil {
		t.Fatalf("GetContainerBoard: %v", err)
	}

	wantStatus := map[string]string{
		"TC-1": "PASS",
		"TC-2": "FAIL",
		"TC-3": "FAIL",
	}
	got := make(map[string]string, len(board.Rows))
	for _, row := range board.Rows {
		got[row.TestKey] = row.RunStatus
	}

	for key, want := range wantStatus {
		if got[key] != want {
			t.Errorf("%s run status = %q, want %q", key, got[key], want)
		}
	}
}

func TestAnalyzeJUnitImportBadBase64(t *testing.T) {
	repo := newRepo(t)
	_, err := repo.AnalyzeJUnitImport("p1", "TE-1", "not-valid-base64!!!")
	if err == nil {
		t.Error("expected error for invalid base64, got nil")
	}
}

func TestAnalyzeJUnitImportBadXML(t *testing.T) {
	repo := newRepo(t)
	bad := base64.StdEncoding.EncodeToString([]byte("<not valid xml"))
	_, err := repo.AnalyzeJUnitImport("p1", "TE-1", bad)
	if err == nil {
		t.Error("expected error for malformed XML, got nil")
	}
}

func TestAnalyzeJUnitImportBareTestsuiteRoot(t *testing.T) {
	repo := newRepo(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "TC-1", ID: "1", Summary: "Alpha"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := repo.UpsertContainers("p1", []testrepo.Container{
		{Key: "TE-1", Kind: "testexec"},
	}); err != nil {
		t.Fatalf("seed container: %v", err)
	}
	if err := repo.ReplaceAllContainerLinks("p1", []testrepo.ContainerLink{
		{ContainerKey: "TE-1", TestKey: "TC-1", RunStatus: "TODO"},
	}); err != nil {
		t.Fatalf("seed link: %v", err)
	}

	// Bare <testsuite> root (no <testsuites> wrapper).
	xml := fmt.Sprintf(`<?xml version="1.0"?><testsuite><testcase name="Alpha"/></testsuite>`)
	b64 := base64.StdEncoding.EncodeToString([]byte(xml))

	preview, err := repo.AnalyzeJUnitImport("p1", "TE-1", b64)
	if err != nil {
		t.Fatalf("AnalyzeJUnitImport: %v", err)
	}
	if preview.Total != 1 {
		t.Errorf("Total = %d, want 1", preview.Total)
	}
	if len(preview.Matched) != 1 {
		t.Errorf("Matched = %d, want 1", len(preview.Matched))
	}
}

// TestAnalyzeJUnitImportNormalization verifies that testcase names are matched
// to member summaries using case-insensitive, trimmed comparison so that minor
// casing differences and surrounding whitespace in the JUnit report do not
// prevent a match.
func TestAnalyzeJUnitImportNormalization(t *testing.T) {
	repo := newRepo(t)

	// Seed one member whose summary uses mixed case and a trailing space.
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "TC-1", ID: "1", Summary: "Login Flow"},
	}); err != nil {
		t.Fatalf("seed tests: %v", err)
	}
	if err := repo.UpsertContainers("p1", []testrepo.Container{
		{Key: "TE-1", Kind: "testexec"},
	}); err != nil {
		t.Fatalf("seed container: %v", err)
	}
	if err := repo.ReplaceAllContainerLinks("p1", []testrepo.ContainerLink{
		{ContainerKey: "TE-1", TestKey: "TC-1", RunStatus: "TODO"},
	}); err != nil {
		t.Fatalf("seed link: %v", err)
	}

	// The JUnit report uses all-upper-case with a leading space - would fail a
	// raw string comparison against the stored summary "Login Flow".
	xmlStr := `<?xml version="1.0"?><testsuite><testcase name="  LOGIN FLOW  "/></testsuite>`
	b64 := base64.StdEncoding.EncodeToString([]byte(xmlStr))

	preview, err := repo.AnalyzeJUnitImport("p1", "TE-1", b64)
	if err != nil {
		t.Fatalf("AnalyzeJUnitImport: %v", err)
	}
	if preview.Total != 1 {
		t.Errorf("Total = %d, want 1", preview.Total)
	}
	if len(preview.Matched) != 1 {
		t.Errorf("Matched = %d, want 1 (normalization should have matched); skipped = %+v", len(preview.Matched), preview.Skipped)
	} else if preview.Matched[0].TestKey != "TC-1" {
		t.Errorf("matched key = %q, want TC-1", preview.Matched[0].TestKey)
	}
	if len(preview.Skipped) != 0 {
		t.Errorf("Skipped = %d, want 0; skipped = %+v", len(preview.Skipped), preview.Skipped)
	}
}

func TestApplyJUnitImportRejectsNonMember(t *testing.T) {
	repo := newRepo(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "TC-1", ID: "1", Summary: "Alpha"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := repo.UpsertContainers("p1", []testrepo.Container{
		{Key: "TE-1", Kind: "testexec"},
	}); err != nil {
		t.Fatalf("seed container: %v", err)
	}
	// TC-1 is NOT a member of TE-1.

	matches := []testrepo.JUnitMatch{
		{Testcase: "Alpha", TestKey: "TC-1", Summary: "Alpha", Result: "PASS"},
	}
	result, err := repo.ApplyJUnitImport("p1", "TE-1", matches)
	if err != nil {
		t.Fatalf("ApplyJUnitImport: %v", err)
	}
	if len(result.Failed) != 1 {
		t.Errorf("Failed = %d, want 1 (non-member should fail)", len(result.Failed))
	}
	if len(result.Succeeded) != 0 {
		t.Errorf("Succeeded = %d, want 0", len(result.Succeeded))
	}
}

func TestAnalyzeJUnitImportEmptyXML(t *testing.T) {
	repo := newRepo(t)
	// A valid XML document with no <testcase> elements should return an error.
	empty := base64.StdEncoding.EncodeToString([]byte("<testsuites></testsuites>"))
	_, err := repo.AnalyzeJUnitImport("p1", "TE-1", empty)
	if err == nil {
		t.Error("expected error for XML with no testcase elements, got nil")
	}
}

func TestApplyJUnitImportRejectsInvalidResult(t *testing.T) {
	repo := seedJUnitRepo(t)

	// Pass a match with an invalid Result value - should be rejected as a failure.
	matches := []testrepo.JUnitMatch{
		{Testcase: "Pass Test", TestKey: "TC-1", Summary: "Pass Test", Result: "SKIP"},
	}
	result, err := repo.ApplyJUnitImport("p1", "TE-1", matches)
	if err != nil {
		t.Fatalf("ApplyJUnitImport: %v", err)
	}
	if len(result.Failed) != 1 {
		t.Errorf("Failed = %d, want 1 (invalid result should fail)", len(result.Failed))
	}
	if len(result.Succeeded) != 0 {
		t.Errorf("Succeeded = %d, want 0", len(result.Succeeded))
	}
	if len(result.Failed) == 1 && result.Failed[0].TestKey != "TC-1" {
		t.Errorf("Failed[0].TestKey = %q, want TC-1", result.Failed[0].TestKey)
	}
}
