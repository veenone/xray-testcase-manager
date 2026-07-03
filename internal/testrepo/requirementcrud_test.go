package testrepo_test

import (
	"encoding/json"
	"strings"
	"testing"

	"xray-test-manager/internal/testrepo"
)

func TestEditRequirementFieldQueuesAndDiscardReverts(t *testing.T) {
	repo := seedReqRepo(t)

	if err := repo.EditRequirementField("p1", "PRD-10", "summary", "Auth works end to end"); err != nil {
		t.Fatalf("edit: %v", err)
	}

	cov, _ := repo.ListRequirementsWithCoverage("p1")
	got := map[string]testrepo.RequirementCoverage{}
	for _, c := range cov {
		got[c.Key] = c
	}
	if got["PRD-10"].Summary != "Auth works end to end" {
		t.Fatalf("PRD-10 summary = %q, want edited value", got["PRD-10"].Summary)
	}

	changes, _ := repo.ListPendingChanges("p1")
	var id int64
	var edits int
	for _, c := range changes {
		if c.EntityType == "requirement_edit" && c.EntityKey == "PRD-10" {
			edits++
			id = c.ID
		}
	}
	if edits != 1 {
		t.Fatalf("want one requirement_edit for PRD-10, got %d", edits)
	}

	if err := repo.DiscardPendingChange("p1", id); err != nil {
		t.Fatalf("discard: %v", err)
	}
	cov, _ = repo.ListRequirementsWithCoverage("p1")
	for _, c := range cov {
		if c.Key == "PRD-10" && c.Summary != "Auth works" {
			t.Errorf("PRD-10 summary after discard = %q, want original", c.Summary)
		}
	}
}

func TestEditRequirementFieldSameValueIsNoop(t *testing.T) {
	repo := seedReqRepo(t)
	if err := repo.EditRequirementField("p1", "PRD-10", "summary", "Auth works"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	changes, _ := repo.ListPendingChanges("p1")
	for _, c := range changes {
		if c.EntityType == "requirement_edit" {
			t.Errorf("editing to the same value should not queue a change: %+v", c)
		}
	}
}

func TestDeleteRequirementRemovesAndDiscardRestores(t *testing.T) {
	repo := seedReqRepo(t)
	// PRD-10 is covered by QA-1 and QA-2.
	if err := repo.DeleteRequirement("p1", "PRD-10"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	cov, _ := repo.ListRequirementsWithCoverage("p1")
	for _, c := range cov {
		if c.Key == "PRD-10" {
			t.Fatalf("PRD-10 still present after delete: %+v", c)
		}
	}
	// Its coverage links are gone too.
	if reqs, _ := repo.GetTestRequirements("p1", "QA-1"); len(reqs) != 0 {
		t.Fatalf("QA-1 requirements after delete = %+v, want none", reqs)
	}

	changes, _ := repo.ListPendingChanges("p1")
	var id int64
	var dels int
	for _, c := range changes {
		if c.EntityType == "requirement_delete" && c.EntityKey == "PRD-10" {
			dels++
			id = c.ID
		}
	}
	if dels != 1 {
		t.Fatalf("want one requirement_delete for PRD-10, got %d", dels)
	}

	// Discard restores the requirement and both coverage links.
	if err := repo.DiscardPendingChange("p1", id); err != nil {
		t.Fatalf("discard: %v", err)
	}
	cov, _ = repo.ListRequirementsWithCoverage("p1")
	restored := map[string]testrepo.RequirementCoverage{}
	for _, c := range cov {
		restored[c.Key] = c
	}
	if restored["PRD-10"].TestCount != 2 {
		t.Errorf("PRD-10 after discard = %+v, want 2 covering tests", restored["PRD-10"])
	}
}

func TestDeleteRequirementRewritesPendingCoverageSet(t *testing.T) {
	repo := seedReqRepo(t)
	// Stage a coverage edit on QA-1 that adds PRD-11 (QA-1 already covers PRD-10).
	if err := repo.SetTestRequirements("p1", "QA-1", []string{"PRD-10", "PRD-11"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	// Deleting PRD-11 must scrub it from the still-pending coverage set so a
	// commit never links a now-deleted requirement.
	if err := repo.DeleteRequirement("p1", "PRD-11"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	changes, _ := repo.ListPendingChanges("p1")
	for _, c := range changes {
		if c.EntityType == "requirement_set" && c.EntityKey == "QA-1" {
			if containsKey(c.AfterVal, "PRD-11") {
				t.Errorf("requirement_set still references deleted PRD-11: %s", c.AfterVal)
			}
		}
	}
}

func containsKey(afterVal, key string) bool {
	var keys []string
	_ = json.Unmarshal([]byte(afterVal), &keys)
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}

func TestCreateRequirementTempKeyAndPending(t *testing.T) {
	repo := newRepo(t)
	// seed a requirement source so the profile exists
	_ = repo.SetRequirementSource("p1", "PROJ", "Story", "")

	key, err := repo.CreateRequirement("p1", "PROJ", "Story", "My requirement", "desc", "High", "CompA", "v1.0")
	if err != nil {
		t.Fatalf("CreateRequirement: %v", err)
	}
	if !strings.HasPrefix(key, "NEW-REQ-") {
		t.Fatalf("temp key = %q, want NEW-REQ-*", key)
	}

	// Requirement appears in coverage list.
	list, err := repo.ListRequirementsWithCoverage("p1")
	if err != nil {
		t.Fatalf("ListRequirementsWithCoverage: %v", err)
	}
	found := false
	for _, r := range list {
		if r.Key == key {
			found = true
			if r.Summary != "My requirement" {
				t.Errorf("summary = %q, want My requirement", r.Summary)
			}
		}
	}
	if !found {
		t.Errorf("new requirement %q not in coverage list", key)
	}

	// A pending change exists.
	pending, err := repo.ListPendingChanges("p1")
	if err != nil {
		t.Fatalf("ListPendingChanges: %v", err)
	}
	hasPending := false
	for _, p := range pending {
		if p.EntityKey == key && p.EntityType == "requirement_create" {
			hasPending = true
		}
	}
	if !hasPending {
		t.Errorf("no requirement_create pending change for %q", key)
	}
}

func TestRenameRequirementRewritesLinks(t *testing.T) {
	repo := newRepo(t)
	_ = repo.SetRequirementSource("p1", "PROJ", "Story", "")

	tempKey, err := repo.CreateRequirement("p1", "PROJ", "Story", "Req", "", "", "", "")
	if err != nil {
		t.Fatalf("CreateRequirement: %v", err)
	}

	// Rename temp key to a real key.
	if err := repo.RenameRequirement("p1", tempKey, "PROJ-100"); err != nil {
		t.Fatalf("RenameRequirement: %v", err)
	}

	// Verify the coverage list shows the real key.
	list, err := repo.ListRequirementsWithCoverage("p1")
	if err != nil {
		t.Fatalf("ListRequirementsWithCoverage: %v", err)
	}
	for _, r := range list {
		if r.Key == tempKey {
			t.Errorf("old temp key %q still in list after rename", tempKey)
		}
	}
}
