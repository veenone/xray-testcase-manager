package testrepo_test

import (
	"bytes"
	"encoding/csv"
	"testing"
)

func TestExportRequirementAuditCSVRows(t *testing.T) {
	repo := seedReqRepo(t)
	// Sign off QA-1 so the audit carries a review verdict + reviewer.
	if err := repo.SetTestReview("p1", "QA-1", "approved", "Ana", "looks good"); err != nil {
		t.Fatalf("review: %v", err)
	}

	data, err := repo.ExportRequirementAudit("p1", "csv")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	records, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}

	header := records[0]
	col := map[string]int{}
	for i, h := range header {
		col[h] = i
	}
	for _, want := range []string{"Requirement", "Coverage", "Test", "Run Result", "Review Verdict", "Reviewer"} {
		if _, ok := col[want]; !ok {
			t.Fatalf("audit header missing %q: %v", want, header)
		}
	}

	// PRD-10 is covered by QA-1 (PASS) and QA-2 (FAIL); find the QA-1 row.
	var qa1Row []string
	var uncoveredRow []string
	for _, row := range records[1:] {
		if row[col["Requirement"]] == "PRD-10" && row[col["Test"]] == "QA-1" {
			qa1Row = row
		}
		if row[col["Requirement"]] == "PRD-12" {
			uncoveredRow = row
		}
	}
	if qa1Row == nil {
		t.Fatalf("no audit row for PRD-10 / QA-1")
	}
	if qa1Row[col["Coverage"]] != "FAILED" {
		t.Errorf("PRD-10 coverage = %q, want FAILED", qa1Row[col["Coverage"]])
	}
	if qa1Row[col["Run Result"]] != "PASS" {
		t.Errorf("QA-1 run result = %q, want PASS", qa1Row[col["Run Result"]])
	}
	if qa1Row[col["Review Verdict"]] != "approved" || qa1Row[col["Reviewer"]] != "Ana" {
		t.Errorf("QA-1 review = (%q, %q), want (approved, Ana)", qa1Row[col["Review Verdict"]], qa1Row[col["Reviewer"]])
	}

	// An uncovered requirement still produces one row, with empty Test columns.
	if uncoveredRow == nil {
		t.Fatalf("uncovered PRD-12 missing from audit")
	}
	if uncoveredRow[col["Coverage"]] != "UNCOVERED" || uncoveredRow[col["Test"]] != "" {
		t.Errorf("PRD-12 row = %v, want UNCOVERED with empty Test", uncoveredRow)
	}
}

func TestStatisticsByCoverage(t *testing.T) {
	repo := seedReqRepo(t)
	stats, err := repo.GetStatistics("p1")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	got := map[string]int{}
	for _, b := range stats.ByCoverage {
		got[b.Label] = b.Count
	}
	if got["FAILED"] != 1 || got["NOTRUN"] != 1 || got["UNCOVERED"] != 1 {
		t.Errorf("ByCoverage = %+v, want one each of FAILED/NOTRUN/UNCOVERED", stats.ByCoverage)
	}
}
