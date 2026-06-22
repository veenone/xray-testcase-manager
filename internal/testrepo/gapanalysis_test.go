package testrepo

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"xray-test-manager/internal/store"
)

// newGapRepo creates a fresh in-memory-backed Repository for gap tests.
// (The external testrepo_test.newRepo helper is in a different package, so
// we duplicate the three-liner here for the internal test package.)
func newGapRepo(t *testing.T) *Repository {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return NewRepository(st)
}

func TestNormalizeSummary(t *testing.T) {
	cases := map[string]string{
		"  Login  with   VALID credentials ": "login with valid credentials",
		"Logout":                             "logout",
		"":                                   "",
	}
	for in, want := range cases {
		if got := normalizeSummary(in); got != want {
			t.Errorf("normalizeSummary(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAnalyzeGapDirectionsAndMatch(t *testing.T) {
	reference := []GapTest{{Summary: "Login"}, {Summary: "Logout"}, {Summary: "Reset password"}}
	target := []GapTest{{Summary: "login  "}, {Summary: "SSO login"}, {Summary: "Reset Password"}}

	res := AnalyzeGap(reference, target, nil, GapOptions{ReferenceSource: "project"})
	if res.Matched != 2 { // Login + Reset password (case/space-insensitive)
		t.Errorf("Matched = %d, want 2", res.Matched)
	}
	if len(res.MissingFromReference) != 1 || res.MissingFromReference[0].Summary != "SSO login" {
		t.Errorf("MissingFromReference = %+v, want [SSO login]", res.MissingFromReference)
	}
	if len(res.MissingFromTarget) != 1 || res.MissingFromTarget[0].Summary != "Logout" {
		t.Errorf("MissingFromTarget = %+v, want [Logout]", res.MissingFromTarget)
	}
	if res.ReferenceSource != "project" || res.ReferenceCount != 3 || res.TargetCount != 3 {
		t.Errorf("meta = %q %d %d, want project 3 3", res.ReferenceSource, res.ReferenceCount, res.TargetCount)
	}
}

func TestAnalyzeGapDedupAndBlank(t *testing.T) {
	reference := []GapTest{}
	target := []GapTest{{Summary: "Dup"}, {Summary: "dup"}, {Summary: "  "}}
	res := AnalyzeGap(reference, target, nil, GapOptions{ReferenceSource: "file"})
	// "Dup"/"dup" collapse to one gap; blank summary skipped.
	if len(res.MissingFromReference) != 1 {
		t.Errorf("MissingFromReference = %+v, want 1 deduped entry", res.MissingFromReference)
	}
	if len(res.MissingFromTarget) != 0 {
		t.Errorf("MissingFromTarget = %+v, want none", res.MissingFromTarget)
	}
}

func TestAnalyzeGapThreeWayMissingFromProject(t *testing.T) {
	reference := []GapTest{{Summary: "Login"}, {Summary: "Bulk export"}}
	target := []GapTest{{Summary: "Login"}, {Summary: "SSO login"}}
	project := []GapTest{{Summary: "Login"}} // project only has Login
	res := AnalyzeGap(reference, target, project, GapOptions{ReferenceSource: "file", ThreeWay: true})
	if !res.ThreeWay || res.ProjectCount != 1 {
		t.Fatalf("ThreeWay/%v ProjectCount/%d, want true/1", res.ThreeWay, res.ProjectCount)
	}
	// union(reference,target) = {Login, Bulk export, SSO login}; minus project{Login} = {Bulk export, SSO login}
	got := map[string]bool{}
	for _, g := range res.MissingFromProject {
		got[g.Summary] = true
	}
	if len(res.MissingFromProject) != 2 || !got["Bulk export"] || !got["SSO login"] {
		t.Errorf("MissingFromProject = %+v, want [Bulk export, SSO login]", res.MissingFromProject)
	}
}

func TestAnalyzeGapFolderMismatch(t *testing.T) {
	reference := []GapTest{{Summary: "Login", Folder: "/Auth/Login"}, {Summary: "Logout", Folder: "/Auth"}}
	target := []GapTest{{Summary: "Login", Folder: "/Smoke/Login"}, {Summary: "Logout", Folder: "/Auth/"}}
	res := AnalyzeGap(reference, target, nil, GapOptions{ReferenceSource: "file", CompareFolders: true})
	// Login: /Auth/Login vs /Smoke/Login -> mismatch. Logout: /Auth vs /Auth/ -> same after normalize.
	if len(res.FolderMismatches) != 1 || res.FolderMismatches[0].Summary != "Login" {
		t.Fatalf("FolderMismatches = %+v, want [Login]", res.FolderMismatches)
	}
	if res.FolderMismatches[0].ReferenceFolder != "/Auth/Login" || res.FolderMismatches[0].TargetFolder != "/Smoke/Login" {
		t.Errorf("mismatch folders = %+v, want /Auth/Login vs /Smoke/Login", res.FolderMismatches[0])
	}
}

func TestParseGapRowsAutoMapsAndGroups(t *testing.T) {
	records := [][]string{
		{"Summary", "Description", "Priority", "Labels", "Components", "Folder", "Action", "Data", "Expected"},
		{"Login", "Can log in", "High", "smoke api", "Auth, Frontend", "/Login", "", "", ""},
		{"Stepped test", "multi", "Medium", "smoke", "Frontend", "/X", "open page", "", "shown"},
		{"", "", "", "", "", "", "enter creds", "u/p", "logged in"}, // step row, NOT a new gap
	}
	gaps, err := ParseGapRows(records)
	if err != nil {
		t.Fatalf("ParseGapRows: %v", err)
	}
	if len(gaps) != 2 {
		t.Fatalf("parsed %d gaps, want 2 (step row must not create one)", len(gaps))
	}
	if gaps[0].Summary != "Login" || len(gaps[0].Labels) != 2 || len(gaps[0].Components) != 2 {
		t.Errorf("gap[0] = %+v, want Login with 2 labels + 2 components", gaps[0])
	}
}

func TestParseGapRowsNoSummaryColumn(t *testing.T) {
	records := [][]string{{"Title", "Notes"}, {"x", "y"}}
	if _, err := ParseGapRows(records); err == nil {
		t.Error("ParseGapRows should error when no Summary column is present")
	}
}

func TestCreateTestsFromGaps(t *testing.T) {
	repo := newGapRepo(t)
	gaps := []GapTest{
		{Summary: "Logout clears session", Description: "d", Priority: "High", Labels: []string{"smoke"}, Components: []string{"Auth"}, Folder: "/X"},
		{Summary: "", Description: "blank skipped"},
	}
	res, err := repo.CreateTestsFromGaps("p1", gaps)
	if err != nil {
		t.Fatalf("CreateTestsFromGaps: %v", err)
	}
	if res.Created != 1 || res.Skipped != 1 {
		t.Fatalf("result = %+v, want Created 1 Skipped 1", res)
	}
	page, _ := repo.ListTests("p1", Query{})
	if page.Total != 1 || page.Tests[0].Summary != "Logout clears session" {
		t.Fatalf("listed = %+v, want one NEW test", page.Tests)
	}
	changes, _ := repo.ListPendingChanges("p1")
	var creates int
	for _, c := range changes {
		if c.EntityType == "test_create" {
			creates++
		}
	}
	if creates != 1 {
		t.Errorf("test_create pending rows = %d, want 1", creates)
	}
}

func TestCreateTestsFromGapsDefaultsEmptyFields(t *testing.T) {
	repo := newGapRepo(t)
	// A summary-only gap: no priority/description.
	if _, err := repo.CreateTestsFromGaps("p1", []GapTest{{Summary: "SSO login"}}); err != nil {
		t.Fatalf("CreateTestsFromGaps: %v", err)
	}
	page, _ := repo.ListTests("p1", Query{})
	if page.Total != 1 {
		t.Fatalf("listed %d, want 1", page.Total)
	}
	tc := page.Tests[0]
	if tc.Priority != defaultGapPriority {
		t.Errorf("Priority = %q, want default %q", tc.Priority, defaultGapPriority)
	}
	if tc.Description != defaultGapDescription {
		t.Errorf("Description = %q, want default %q", tc.Description, defaultGapDescription)
	}
}

func TestBuildGapReportHasHeaderAndSections(t *testing.T) {
	res := GapResult{
		ReferenceSource: "project", ReferenceCount: 5, TargetCount: 6, Matched: 4,
		MissingFromReference: []GapTest{{Summary: "SSO login", Description: "d"}},
		MissingFromTarget:    []GapTest{{Summary: "Legacy captcha"}},
	}
	data, err := BuildGapReport(res, "2026-06-20T00:00:00Z", "csv")
	if err != nil {
		t.Fatalf("BuildGapReport: %v", err)
	}
	s := string(data)
	for _, want := range []string{"2026-06-20T00:00:00Z", "project", "Missing from reference", "Missing from target", "SSO login", "Legacy captcha"} {
		if !strings.Contains(s, want) {
			t.Errorf("report missing %q\n%s", want, s)
		}
	}
	if _, err := BuildGapReport(res, "t", "xlsx"); err != nil {
		t.Errorf("xlsx report: %v", err)
	}
}

func TestBuildGapReportXLSXHasSectionSheets(t *testing.T) {
	res := GapResult{
		ReferenceSource:      "file",
		MissingFromReference: []GapTest{{Summary: "SSO login"}},
		MissingFromTarget:    []GapTest{{Summary: "Legacy captcha"}},
		ThreeWay:             true,
		MissingFromProject:   []GapTest{{Summary: "Bulk export"}},
		FolderMismatches:     []FolderMismatch{{Summary: "Login", ReferenceFolder: "/Auth", TargetFolder: "/Smoke"}},
	}
	data, err := BuildGapReport(res, "2026-06-22T00:00:00Z", "xlsx")
	if err != nil {
		t.Fatalf("BuildGapReport xlsx: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open xlsx: %v", err)
	}
	defer func() { _ = f.Close() }()

	want := []string{"Overview", "Missing from Reference", "Missing from Target", "Missing from Project", "Folder Mismatches"}
	got := f.GetSheetList()
	if len(got) != len(want) {
		t.Fatalf("sheets = %v, want %v", got, want)
	}
	for i, name := range want {
		if got[i] != name {
			t.Errorf("sheet[%d] = %q, want %q", i, got[i], name)
		}
	}

	header, err := f.GetCellValue("Missing from Reference", "A1")
	if err != nil || header != "Summary" {
		t.Errorf("Missing from Reference A1 = %q (err %v), want \"Summary\"", header, err)
	}
	summary, err := f.GetCellValue("Missing from Reference", "A2")
	if err != nil || summary != "SSO login" {
		t.Errorf("Missing from Reference A2 = %q (err %v), want \"SSO login\"", summary, err)
	}
}

func TestParseGapRowsToleratesUTF8BOM(t *testing.T) {
	// Excel/Windows-saved CSVs prepend a UTF-8 BOM, which otherwise fuses onto
	// the first header cell and breaks Summary auto-mapping.
	csv := string(utf8BOMBytes) + "Summary\nLogin with valid credentials\nLogout clears the session\n"
	recs, err := ParseRecords([]byte(csv), false)
	if err != nil {
		t.Fatalf("ParseRecords: %v", err)
	}
	tests, err := ParseGapRows(recs)
	if err != nil {
		t.Fatalf("ParseGapRows with BOM: %v", err)
	}
	if len(tests) != 2 {
		t.Fatalf("got %d tests, want 2", len(tests))
	}
	if tests[0].Summary != "Login with valid credentials" {
		t.Errorf("tests[0].Summary = %q, want \"Login with valid credentials\"", tests[0].Summary)
	}
	// Unmapped optional fields are null/empty, not an error.
	if tests[0].Description != "" || tests[0].Priority != "" {
		t.Errorf("unmapped fields should be empty, got desc=%q prio=%q", tests[0].Description, tests[0].Priority)
	}
}
