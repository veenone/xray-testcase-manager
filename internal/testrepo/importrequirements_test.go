package testrepo_test

import (
	"strings"
	"testing"

	"xray-test-manager/internal/testrepo"
)

const reqImportCSV = "Summary,Description,Priority,Components,FixVersions\n" +
	"Auth works,Verify auth,High,Frontend,v1.0\n" +
	"Session policy,Timeout enforcement,Medium,,\n" +
	"New requirement,Brand new,Low,,\n"

func TestRequirementImportTemplateCSV(t *testing.T) {
	csv := testrepo.RequirementImportTemplateCSV()
	if !strings.Contains(csv, "Summary") {
		t.Error("template must contain Summary column")
	}
	if !strings.Contains(csv, "FixVersions") {
		t.Error("template must contain FixVersions column")
	}
	lines := strings.Split(strings.TrimSpace(csv), "\n")
	if len(lines) < 2 {
		t.Error("template must have at least one example row")
	}
}

func TestAnalyzeRequirementImport_ClassifiesExistingAndNew(t *testing.T) {
	// seedReqRepo seeds: PRD-10 "Auth works", PRD-11 "Session policy", PRD-12 "Untested".
	repo := seedReqRepo(t)
	records, err := testrepo.ParseRecords([]byte(reqImportCSV), false)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	preview, err := repo.AnalyzeRequirementImport("p1", records)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if preview.ExistingCount != 2 {
		t.Errorf("existingCount = %d, want 2", preview.ExistingCount)
	}
	if preview.NewCount != 1 {
		t.Errorf("newCount = %d, want 1", preview.NewCount)
	}
	for _, row := range preview.Rows {
		switch row.Summary {
		case "Auth works", "Session policy":
			if row.Status != "existing" {
				t.Errorf("%q: status = %q, want existing", row.Summary, row.Status)
			}
		case "New requirement":
			if row.Status != "new" {
				t.Errorf("%q: status = %q, want new", row.Summary, row.Status)
			}
			if row.Priority != "Low" {
				t.Errorf("%q: priority = %q, want Low", row.Summary, row.Priority)
			}
		}
	}
}

func TestImportRequirements_CreatesOnlyNew(t *testing.T) {
	repo := seedReqRepo(t)
	records, err := testrepo.ParseRecords([]byte(reqImportCSV), false)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	result, err := repo.ImportRequirements("p1", "PRD", "Story", records)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Created != 1 {
		t.Errorf("created = %d, want 1", result.Created)
	}
	if result.SkippedExisting != 2 {
		t.Errorf("skippedExisting = %d, want 2", result.SkippedExisting)
	}
	// The newly created requirement should appear in coverage list.
	cov, _ := repo.ListRequirementsWithCoverage("p1")
	found := false
	for _, c := range cov {
		if strings.EqualFold(c.Summary, "New requirement") {
			found = true
		}
	}
	if !found {
		t.Error("imported requirement 'New requirement' not found in coverage list")
	}
}

func TestImportRequirements_BlankSummaryIsSkipped(t *testing.T) {
	repo := seedReqRepo(t)
	csv := "Summary,Description\n" +
		"Real requirement,has summary\n" +
		",no summary\n"
	records, _ := testrepo.ParseRecords([]byte(csv), false)
	result, err := repo.ImportRequirements("p1", "PRD", "Story", records)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Created != 1 {
		t.Errorf("created = %d, want 1 (blank row skipped)", result.Created)
	}
}
