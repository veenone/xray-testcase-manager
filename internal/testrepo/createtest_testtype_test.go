package testrepo

import "testing"

func TestCreateTestPersistsTypeAndBody(t *testing.T) {
	repo := newTestRepo(t) // use the package's real helper
	key, err := repo.CreateTest("p1", TestDraft{
		Summary:          "BDD login",
		ExecType:         "Cucumber",
		CucumberType:     "Scenario",
		CucumberScenario: "Scenario: login\n  Given a user",
	})
	if err != nil {
		t.Fatal(err)
	}
	tc, err := repo.GetTest("p1", key)
	if err != nil {
		t.Fatal(err)
	}
	if tc.ExecType != "Cucumber" || tc.CucumberType != "Scenario" || tc.CucumberScenario == "" {
		t.Errorf("type/body not persisted on create: %+v", tc)
	}
}

func TestCloneCarriesTypeAndBody(t *testing.T) {
	repo := newTestRepo(t)
	src, _ := repo.CreateTest("p1", TestDraft{Summary: "Gen", ExecType: "Generic", GenericDefinition: "com.acme.Foo#bar"})
	cloneKey, err := repo.CloneTest("p1", src)
	if err != nil {
		t.Fatal(err)
	}
	tc, _ := repo.GetTest("p1", cloneKey)
	if tc.ExecType != "Generic" || tc.GenericDefinition == "" {
		t.Errorf("clone dropped type/body: %+v", tc)
	}
}
