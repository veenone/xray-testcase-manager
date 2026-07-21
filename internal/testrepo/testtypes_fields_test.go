package testrepo

import "testing"

func TestEditAndReadTestTypeBodies(t *testing.T) {
	repo := newTestRepo(t) // mirror the nearest existing testrepo test helper
	if err := repo.UpsertTests("p1", []TestCase{{Key: "QA-1", ID: "1", Summary: "S", ExecType: "Cucumber"}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.EditTestField("p1", "QA-1", "cucumber_scenario", "Scenario: x\n  Given y"); err != nil {
		t.Fatalf("edit scenario: %v", err)
	}
	if err := repo.EditTestField("p1", "QA-1", "cucumber_type", "Scenario"); err != nil {
		t.Fatalf("edit type: %v", err)
	}
	if err := repo.EditTestField("p1", "QA-1", "generic_definition", "com.acme.Foo#bar"); err != nil {
		t.Fatalf("edit def: %v", err)
	}
	tc, err := repo.GetTest("p1", "QA-1")
	if err != nil {
		t.Fatal(err)
	}
	if tc.CucumberScenario == "" || tc.CucumberType != "Scenario" || tc.GenericDefinition == "" {
		t.Errorf("bodies not persisted/read: %+v", tc)
	}
}
