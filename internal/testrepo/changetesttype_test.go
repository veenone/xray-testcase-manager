package testrepo

import (
	"strings"
	"testing"
)

func TestChangeTestTypePrefillsEmptyTarget(t *testing.T) {
	repo := newTestRepo(t)
	if err := repo.UpsertTests("p1", []TestCase{{Key: "QA-1", ID: "1", Summary: "Login works", ExecType: "Manual"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddTestStep("p1", "QA-1", "open login", "user=bob", "form shown"); err != nil {
		t.Fatal(err)
	}
	res, err := repo.ChangeTestType("p1", "QA-1", "Cucumber")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Prefilled {
		t.Errorf("expected pre-fill, got %+v", res)
	}
	tc, _ := repo.GetTest("p1", "QA-1")
	if tc.ExecType != "Cucumber" || !strings.Contains(tc.CucumberScenario, "When open login") {
		t.Errorf("type/scenario not set: %+v", tc)
	}
	// Non-destructive: switching back leaves the Manual steps intact.
	steps, _ := repo.ListTestSteps("p1", "QA-1")
	if len(steps) == 0 {
		t.Error("manual steps were destroyed by conversion")
	}
}

func TestChangeTestTypeDoesNotOverwriteExistingBody(t *testing.T) {
	repo := newTestRepo(t)
	repo.UpsertTests("p1", []TestCase{{Key: "QA-2", ID: "2", Summary: "S", ExecType: "Manual"}})
	repo.AddTestStep("p1", "QA-2", "a", "", "b")
	repo.EditTestField("p1", "QA-2", "cucumber_scenario", "Scenario: hand-written\n Given keep me")
	res, err := repo.ChangeTestType("p1", "QA-2", "Cucumber")
	if err != nil {
		t.Fatal(err)
	}
	if res.Prefilled {
		t.Error("must not overwrite non-empty target")
	}
	if !res.CanPrefill {
		t.Error("CanPrefill should be true when a source body exists")
	}
	tc, _ := repo.GetTest("p1", "QA-2")
	if !strings.Contains(tc.CucumberScenario, "hand-written") {
		t.Error("existing scenario was clobbered")
	}
}
