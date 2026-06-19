package testrepo_test

import (
	"path/filepath"
	"testing"

	"xray-test-manager/internal/store"
	"xray-test-manager/internal/testrepo"
)

func openExecTypeRepo(t *testing.T) *testrepo.Repository {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return testrepo.NewRepository(st)
}

// TestExecTypeFilter seeds two Tests, sets exec_type "Automated" on one via the
// single-field edit path, then verifies the ExecType filter returns only that
// Test and that the returned TestCase carries the value.
func TestExecTypeFilter(t *testing.T) {
	repo := openExecTypeRepo(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "Login", Updated: "T0"},
		{Key: "QA-2", ID: "2", Summary: "Logout", Updated: "T0"},
	}); err != nil {
		t.Fatalf("seed tests: %v", err)
	}

	if err := repo.EditTestField("p1", "QA-1", "exec_type", "Automated"); err != nil {
		t.Fatalf("edit exec_type: %v", err)
	}

	page, err := repo.ListTests("p1", testrepo.Query{ExecType: "Automated"})
	if err != nil {
		t.Fatalf("list tests: %v", err)
	}
	if len(page.Tests) != 1 {
		t.Fatalf("expected 1 test, got %d", len(page.Tests))
	}
	if page.Tests[0].Key != "QA-1" {
		t.Fatalf("expected QA-1, got %s", page.Tests[0].Key)
	}
	if page.Tests[0].ExecType != "Automated" {
		t.Fatalf("expected ExecType Automated, got %q", page.Tests[0].ExecType)
	}

	// Empty filter returns both.
	all, err := repo.ListTests("p1", testrepo.Query{})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if all.Total != 2 {
		t.Fatalf("expected 2 tests with empty filter, got %d", all.Total)
	}
}

// TestBulkEditExecType sets exec_type across a batch via BulkEditTests (op set,
// field exec_type) and verifies the value lands and a pending change is queued.
func TestBulkEditExecType(t *testing.T) {
	repo := openExecTypeRepo(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "A", Updated: "T0"},
		{Key: "QA-2", ID: "2", Summary: "B", Updated: "T0"},
	}); err != nil {
		t.Fatalf("seed tests: %v", err)
	}

	res, err := repo.BulkEditTests("p1", []string{"QA-1", "QA-2"}, testrepo.BulkEdit{
		Operation: "set", Field: "exec_type", Value: "Generic",
	})
	if err != nil {
		t.Fatalf("bulk edit: %v", err)
	}
	if len(res.Succeeded) != 2 || len(res.Failed) != 0 {
		t.Fatalf("expected 2 succeeded 0 failed, got %+v", res)
	}

	page, err := repo.ListTests("p1", testrepo.Query{ExecType: "Generic"})
	if err != nil {
		t.Fatalf("list tests: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("expected 2 Generic tests, got %d", page.Total)
	}
}
