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

	// Each of the two valid rows is a test_create; both also carry Folder=/Auth,
	// so each queues a folder-placement change too (so the commit moves it into
	// its Test Repository folder rather than leaving it at the root).
	changes, _ := repo.ListPendingChanges("p1")
	var creates, folders int
	for _, c := range changes {
		if !strings.HasPrefix(c.EntityKey, "NEW-") {
			t.Errorf("change = %+v, want a NEW-* key", c)
		}
		switch {
		case c.EntityType == "test_create":
			creates++
		case c.Field == "folder":
			folders++
		default:
			t.Errorf("unexpected pending change = %+v", c)
		}
	}
	if creates != 2 {
		t.Errorf("want 2 test_create pending rows, got %d", creates)
	}
	if folders != 2 {
		t.Errorf("want 2 folder-placement rows (both /Auth rows), got %d", folders)
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

// TestImportQueuesFolderPlacement is the regression guard for the import
// folder-placement bug: a mapped Folder must queue a "folder" pending change so
// the committed Test is moved into its Test Repository folder, exactly like
// CreateTest. Previously import stamped folder_id locally but queued no
// placement, so committed imports landed at the Xray repository root.
func TestImportQueuesFolderPlacement(t *testing.T) {
	repo := newRepo(t)
	path := "/[ITS_0001477] TM_MW_INT_002 - Proxy Functional Test/HSM Resilience and Routing/Role-Aware Routing"
	csv := "Summary,Folder\n\"Imported\",\"" + path + "\"\n"
	if _, err := repo.ImportTests("p1", recordsOf(t, csv),
		testrepo.ImportMapping{Summary: "Summary", Folder: "Folder"}, false); err != nil {
		t.Fatalf("import: %v", err)
	}
	pcs, err := repo.ListPendingChanges("p1")
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	var folder *testrepo.PendingChange
	for i := range pcs {
		if pcs[i].Field == "folder" {
			folder = &pcs[i]
		}
	}
	if folder == nil {
		t.Fatal("import queued no folder placement — a committed import would land at the Xray root")
	}
	if folder.AfterVal != path {
		t.Errorf("folder AfterVal = %q, want %q (spaces preserved verbatim)", folder.AfterVal, path)
	}
	if folder.BeforeVal != "" {
		t.Errorf("folder BeforeVal = %q, want empty (a brand-new imported Test)", folder.BeforeVal)
	}
}

// TestImportWithoutFolderQueuesNoPlacement: a row with no mapped/blank Folder must
// not queue a spurious folder change.
func TestImportWithoutFolderQueuesNoPlacement(t *testing.T) {
	repo := newRepo(t)
	csv := "Summary,Folder\nNo folder here,\n"
	if _, err := repo.ImportTests("p1", recordsOf(t, csv),
		testrepo.ImportMapping{Summary: "Summary", Folder: "Folder"}, false); err != nil {
		t.Fatalf("import: %v", err)
	}
	pcs, _ := repo.ListPendingChanges("p1")
	for _, pc := range pcs {
		if pc.Field == "folder" {
			t.Errorf("unexpected folder pending change for a folderless import: %+v", pc)
		}
	}
}
