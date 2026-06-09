package testrepo_test

import (
	"strings"
	"testing"

	"xray-test-manager/internal/testrepo"
)

const importCSV = "Summary,Description,Priority,Labels,Folder\n" +
	"Login works,Verify login,High,smoke,/Auth\n" +
	",missing summary,Low,,\n" +
	"Logout works,Verify logout,Medium,regression,/Auth\n"

func recordsOf(t *testing.T, csv string) [][]string {
	t.Helper()
	recs, err := testrepo.ParseRecords([]byte(csv), false)
	if err != nil {
		t.Fatalf("parse records: %v", err)
	}
	return recs
}

// TestImportTestsStoresComponents verifies a mapped Components column (comma-
// separated, names may contain spaces) lands on the imported Test.
func TestImportTestsStoresComponents(t *testing.T) {
	repo := newRepo(t)
	csv := "Summary,Components\n" +
		"With components,\"Frontend, User Management\"\n"
	mapping := testrepo.ImportMapping{Summary: "Summary", Components: "Components"}

	if _, err := repo.ImportTests("p1", recordsOf(t, csv), mapping, false); err != nil {
		t.Fatalf("import: %v", err)
	}
	page, err := repo.ListTests("p1", testrepo.Query{})
	if err != nil || page.Total != 1 {
		t.Fatalf("expected 1 test, got %d (err %v)", page.Total, err)
	}
	got := page.Tests[0].Components
	if len(got) != 2 || got[0] != "Frontend" || got[1] != "User Management" {
		t.Errorf("components = %v, want [Frontend, User Management]", got)
	}
}

func TestParseImportPreviewReportsHeadersAndRows(t *testing.T) {
	pv, err := testrepo.ParseImportPreview(recordsOf(t, importCSV))
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(pv.Headers) != 5 || pv.Headers[0] != "Summary" {
		t.Errorf("headers = %v, want 5 starting with Summary", pv.Headers)
	}
	if pv.RowCount != 3 {
		t.Errorf("RowCount = %d, want 3", pv.RowCount)
	}
}

func TestImportTestsDryRunReportsErrorsWithoutCreating(t *testing.T) {
	repo := newRepo(t)
	mapping := testrepo.ImportMapping{Summary: "Summary", Description: "Description", Priority: "Priority", Labels: "Labels", Folder: "Folder"}

	res, err := repo.ImportTests("p1", recordsOf(t, importCSV), mapping, true)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if res.Created != 2 {
		t.Errorf("Created = %d, want 2 valid rows", res.Created)
	}
	if len(res.Errors) != 1 || res.Errors[0].Row != 3 {
		t.Errorf("errors = %+v, want one for row 3 (empty summary)", res.Errors)
	}

	page, _ := repo.ListTests("p1", testrepo.Query{})
	if page.Total != 0 {
		t.Errorf("dry run must not create tests; got %d", page.Total)
	}
}

func TestImportTestsCreatesPendingTests(t *testing.T) {
	repo := newRepo(t)
	mapping := testrepo.ImportMapping{Summary: "Summary", Description: "Description", Priority: "Priority", Labels: "Labels", Folder: "Folder"}

	res, err := repo.ImportTests("p1", recordsOf(t, importCSV), mapping, false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Created != 2 {
		t.Errorf("Created = %d, want 2", res.Created)
	}

	page, _ := repo.ListTests("p1", testrepo.Query{})
	if page.Total != 2 {
		t.Fatalf("expected 2 created tests, got %d", page.Total)
	}

	changes, _ := repo.ListPendingChanges("p1")
	if len(changes) != 2 {
		t.Errorf("want 2 test_create pending rows, got %d", len(changes))
	}
	for _, c := range changes {
		if c.EntityType != "test_create" || !strings.HasPrefix(c.EntityKey, "NEW-") {
			t.Errorf("change = %+v, want test_create with NEW-* key", c)
		}
	}
}

func TestImportTestsGroupsMultiRowSteps(t *testing.T) {
	repo := newRepo(t)
	csv := "Summary,Action,Data,Expected\n" +
		"Login flow,Open login page,,Page shown\n" +
		",Enter credentials,user/pass,Fields filled\n" +
		",Submit,,Logged in\n" +
		"Logout flow,Click logout,,Logged out\n"
	mapping := testrepo.ImportMapping{
		Summary: "Summary", Action: "Action", Data: "Data", Expected: "Expected",
	}

	res, err := repo.ImportTests("p1", recordsOf(t, csv), mapping, false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Created != 2 {
		t.Fatalf("Created = %d, want 2 tests (rows 2-4 are one test, row 5 another)", res.Created)
	}

	// The first imported test should have 3 steps.
	page, _ := repo.ListTests("p1", testrepo.Query{})
	var loginKey string
	for _, tc := range page.Tests {
		if tc.Summary == "Login flow" {
			loginKey = tc.Key
		}
	}
	if loginKey == "" {
		t.Fatal("Login flow test not created")
	}
	steps, _ := repo.ListTestSteps("p1", loginKey)
	if len(steps) != 3 {
		t.Errorf("Login flow steps = %d, want 3", len(steps))
	}
}

func TestImportStepRowBeforeSummaryIsError(t *testing.T) {
	repo := newRepo(t)
	csv := "Summary,Action\n" +
		",orphan step\n" +
		"A test,first step\n"
	mapping := testrepo.ImportMapping{Summary: "Summary", Action: "Action"}

	res, err := repo.ImportTests("p1", recordsOf(t, csv), mapping, true)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if len(res.Errors) != 1 || res.Errors[0].Row != 2 {
		t.Errorf("errors = %+v, want one for row 2 (step before any summary)", res.Errors)
	}
	if res.Created != 1 {
		t.Errorf("Created = %d, want 1 (the real test)", res.Created)
	}
}

func TestImportTestsRequiresSummaryMapping(t *testing.T) {
	repo := newRepo(t)
	_, err := repo.ImportTests("p1", recordsOf(t, importCSV), testrepo.ImportMapping{}, true)
	if err == nil {
		t.Error("importing without a Summary mapping should error")
	}
}

func TestDiscardImportedTestRemovesIt(t *testing.T) {
	repo := newRepo(t)
	mapping := testrepo.ImportMapping{Summary: "Summary"}
	if _, err := repo.ImportTests("p1", recordsOf(t, "Summary\nOne test\n"), mapping, false); err != nil {
		t.Fatalf("import: %v", err)
	}
	changes, _ := repo.ListPendingChanges("p1")

	if err := repo.DiscardPendingChange("p1", changes[0].ID); err != nil {
		t.Fatalf("discard: %v", err)
	}

	page, _ := repo.ListTests("p1", testrepo.Query{})
	if page.Total != 0 {
		t.Errorf("discarding the import should remove the test; got %d", page.Total)
	}
}
