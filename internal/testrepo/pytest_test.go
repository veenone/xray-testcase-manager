package testrepo_test

import (
	"strings"
	"testing"

	"xray-test-manager/internal/testrepo"
)

func seedPytestContainer(t *testing.T) *testrepo.Repository {
	t.Helper()
	repo := newRepo(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "Login works"},
		{Key: "QA-2", ID: "2", Summary: "Logout works"},
	}); err != nil {
		t.Fatalf("seed tests: %v", err)
	}
	if err := repo.SetTestSteps("p1", "QA-1", []testrepo.Step{
		{XrayID: "s1", Index: 1, Action: "Open login page", Expected: "Form shown"},
		{XrayID: "s2", Index: 2, Action: "Submit credentials", Data: "user/pass", Expected: "Logged in"},
	}); err != nil {
		t.Fatalf("seed steps: %v", err)
	}
	if err := repo.UpsertContainers("p1", []testrepo.Container{
		{Key: "QA-TE-1", Kind: "testexec", Summary: "Cycle 1", Status: "Open"},
	}); err != nil {
		t.Fatalf("seed container: %v", err)
	}
	if err := repo.ReplaceAllContainerLinks("p1", []testrepo.ContainerLink{
		{ContainerKey: "QA-TE-1", TestKey: "QA-1", RunStatus: "TODO"},
		{ContainerKey: "QA-TE-1", TestKey: "QA-2", RunStatus: "TODO"},
	}); err != nil {
		t.Fatalf("seed links: %v", err)
	}
	return repo
}

func TestGeneratePytestHasOneFunctionPerTest(t *testing.T) {
	repo := seedPytestContainer(t)

	code, err := repo.GeneratePytest("p1", "QA-TE-1", "")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if strings.Count(code, "def test_") != 2 {
		t.Errorf("want 2 test functions, got code:\n%s", code)
	}
	if !strings.Contains(code, "def test_qa_1():") || !strings.Contains(code, "def test_qa_2():") {
		t.Errorf("missing expected function names:\n%s", code)
	}
	if !strings.Contains(code, "import pytest") {
		t.Errorf("missing pytest import")
	}
}

func TestGeneratePytestIncludesStepsAndMarker(t *testing.T) {
	repo := seedPytestContainer(t)

	code, _ := repo.GeneratePytest("p1", "QA-TE-1", "")
	if !strings.Contains(code, `@pytest.mark.xray("QA-1")`) {
		t.Errorf("missing xray marker for QA-1:\n%s", code)
	}
	if !strings.Contains(code, "Open login page") || !strings.Contains(code, "Expected: Logged in") {
		t.Errorf("steps not rendered in docstring:\n%s", code)
	}
}

func TestGeneratePytestUnknownContainerErrors(t *testing.T) {
	repo := seedPytestContainer(t)
	if _, err := repo.GeneratePytest("p1", "NOPE-1", ""); err == nil {
		t.Error("unknown container should error")
	}
}

func TestGeneratePytestUnittestStyle(t *testing.T) {
	repo := seedPytestContainer(t)

	code, err := repo.GeneratePytest("p1", "QA-TE-1", "unittest")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.Contains(code, "import unittest") {
		t.Errorf("missing unittest import:\n%s", code)
	}
	if !strings.Contains(code, "(unittest.TestCase):") {
		t.Errorf("missing TestCase class:\n%s", code)
	}
	if strings.Count(code, "def test_") != 2 {
		t.Errorf("want 2 test methods, got:\n%s", code)
	}
	if !strings.Contains(code, "def test_qa_1(self):") {
		t.Errorf("methods should take self:\n%s", code)
	}
	if !strings.Contains(code, `self.xray_key = "QA-1"`) {
		t.Errorf("missing xray key traceability:\n%s", code)
	}
	if !strings.Contains(code, "unittest.main()") {
		t.Errorf("missing unittest.main() entrypoint:\n%s", code)
	}
}
