package testrepo_test

import (
	"fmt"
	"testing"

	"xray-test-manager/internal/testrepo"
)

func seedSummaryTests(t *testing.T) *testrepo.Repository {
	t.Helper()
	repo := newRepo(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "Login works"},
		{Key: "QA-2", ID: "2", Summary: "Logout works"},
		{Key: "QA-3", ID: "3", Summary: "Reset works"},
	}); err != nil {
		t.Fatalf("seed tests: %v", err)
	}
	return repo
}

func TestListTestSummariesReturnsRequestOrder(t *testing.T) {
	repo := seedSummaryTests(t)

	got, err := repo.ListTestSummaries("p1", []string{"QA-3", "QA-1"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}
	// Request order, not table order: the modal renders in the order the user
	// selected, and re-sorting here would silently reorder the preview.
	if got[0].Key != "QA-3" || got[1].Key != "QA-1" {
		t.Fatalf("got order %s,%s want QA-3,QA-1", got[0].Key, got[1].Key)
	}
	if got[0].Summary != "Reset works" {
		t.Errorf("QA-3 summary = %q, want %q", got[0].Summary, "Reset works")
	}
}

func TestListTestSummariesOmitsUnknownKeys(t *testing.T) {
	// A key can vanish between selection and modal open (a sync deleted it).
	// That must cost one row, not the whole dialog.
	repo := seedSummaryTests(t)

	got, err := repo.ListTestSummaries("p1", []string{"QA-1", "QA-NOPE"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].Key != "QA-1" {
		t.Fatalf("got %+v, want only QA-1", got)
	}
}

func TestListTestSummariesIsProfileScoped(t *testing.T) {
	repo := seedSummaryTests(t)

	got, err := repo.ListTestSummaries("p2", []string{"QA-1"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %+v, want nothing for another profile", got)
	}
}

func TestListTestSummariesChunksPastTheParameterLimit(t *testing.T) {
	// This driver rejects an IN (...) clause above 32,765 parameters with
	// "too many SQL variables". A select-all on a 50k project exceeds that, so
	// the read must chunk. Without chunking this fails outright (spec N1).
	repo := seedSummaryTests(t)

	keys := make([]string, 0, 40001)
	for i := 0; i < 40000; i++ {
		keys = append(keys, fmt.Sprintf("QA-MISSING-%d", i))
	}
	// One real key at the far end, so a chunking bug that drops the tail shows.
	keys = append(keys, "QA-2")

	got, err := repo.ListTestSummaries("p1", keys)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].Key != "QA-2" {
		t.Fatalf("got %+v, want only QA-2", got)
	}
}

func TestListTestSummariesEmptyInput(t *testing.T) {
	repo := seedSummaryTests(t)

	got, err := repo.ListTestSummaries("p1", nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %+v, want an empty result", got)
	}
}
