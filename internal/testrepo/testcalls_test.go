package testrepo_test

import (
	"testing"

	"xray-test-manager/internal/testrepo"
)

func TestListTestCallLinks(t *testing.T) {
	repo := seedTestWithSteps(t) // QA-1 with one manual step
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-2", ID: "2", Summary: "Login helper"},
	}); err != nil {
		t.Fatalf("seed called test: %v", err)
	}

	// QA-1 calls QA-2 (exists) and QA-404 (not in the cache → broken).
	if _, err := repo.AddCalledTestStep("p1", "QA-1", "QA-2"); err != nil {
		t.Fatalf("call QA-2: %v", err)
	}
	if _, err := repo.AddCalledTestStep("p1", "QA-1", "QA-404"); err != nil {
		t.Fatalf("call QA-404: %v", err)
	}

	links, err := repo.ListTestCallLinks("p1")
	if err != nil {
		t.Fatalf("list call links: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("want 2 call links, got %d: %+v", len(links), links)
	}

	byCalled := map[string]testrepo.TestCallLink{}
	for _, l := range links {
		if l.CallerKey != "QA-1" {
			t.Errorf("caller = %q, want QA-1", l.CallerKey)
		}
		byCalled[l.CalledKey] = l
	}
	if !byCalled["QA-2"].CalledExists || byCalled["QA-2"].CalledSummary != "Login helper" {
		t.Errorf("QA-2 link should resolve to an existing test: %+v", byCalled["QA-2"])
	}
	if byCalled["QA-404"].CalledExists {
		t.Errorf("QA-404 link should be flagged as broken (not in cache)")
	}
}
