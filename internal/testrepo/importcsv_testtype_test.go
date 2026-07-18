package testrepo_test

import (
	"testing"

	"xray-test-manager/internal/testrepo"
)

// TestImportTestsPopulatesTestTypeAndBody verifies the importer maps the
// optional Test Type / Cucumber / Generic body columns onto the created
// tests' ExecType/CucumberScenario/CucumberType/GenericDefinition fields,
// while a plain Manual row (with those cells blank) still imports cleanly.
func TestImportTestsPopulatesTestTypeAndBody(t *testing.T) {
	repo := newRepo(t)
	csv := "Summary,Test Type,Cucumber Scenario,Scenario Type,Generic Test Definition\n" +
		"BDD login,Cucumber,\"Scenario: login\nGiven a user\",Scenario,\n" +
		"Generic check,Generic,,,com.acme.Foo#bar\n" +
		"Plain manual test,Manual,,,\n"
	mapping := testrepo.ImportMapping{
		Summary:           "Summary",
		TestType:          "Test Type",
		CucumberScenario:  "Cucumber Scenario",
		CucumberType:      "Scenario Type",
		GenericDefinition: "Generic Test Definition",
	}

	res, err := repo.ImportTests("p1", recordsOf(t, csv), mapping, false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Created != 3 {
		t.Fatalf("Created = %d, want 3", res.Created)
	}

	page, err := repo.ListTests("p1", testrepo.Query{})
	if err != nil || page.Total != 3 {
		t.Fatalf("expected 3 tests, got %d (err %v)", page.Total, err)
	}

	byKey := map[string]testrepo.TestCase{}
	for _, tc := range page.Tests {
		byKey[tc.Summary] = tc
	}

	cuke, ok := byKey["BDD login"]
	if !ok {
		t.Fatal("BDD login test not created")
	}
	if cuke.ExecType != "Cucumber" || cuke.CucumberType != "Scenario" || cuke.CucumberScenario == "" {
		t.Errorf("cucumber test type/body not imported: %+v", cuke)
	}

	generic, ok := byKey["Generic check"]
	if !ok {
		t.Fatal("Generic check test not created")
	}
	if generic.ExecType != "Generic" || generic.GenericDefinition != "com.acme.Foo#bar" {
		t.Errorf("generic test type/body not imported: %+v", generic)
	}

	manual, ok := byKey["Plain manual test"]
	if !ok {
		t.Fatal("Plain manual test not created")
	}
	if manual.ExecType != "Manual" || manual.CucumberScenario != "" || manual.CucumberType != "" || manual.GenericDefinition != "" {
		t.Errorf("manual row should have empty body fields: %+v", manual)
	}
}

// TestImportTestsWithoutTestTypeColumnsStillWorks verifies the new mapping
// fields are fully optional (backward compatibility): a mapping/CSV that
// never mentions Test Type/Cucumber/Generic columns imports exactly as
// before, leaving those fields empty.
func TestImportTestsWithoutTestTypeColumnsStillWorks(t *testing.T) {
	repo := newRepo(t)
	csv := "Summary,Description\n" +
		"No type columns here,Just a plain row\n"
	mapping := testrepo.ImportMapping{Summary: "Summary", Description: "Description"}

	res, err := repo.ImportTests("p1", recordsOf(t, csv), mapping, false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Created != 1 {
		t.Fatalf("Created = %d, want 1", res.Created)
	}

	page, _ := repo.ListTests("p1", testrepo.Query{})
	if page.Total != 1 {
		t.Fatalf("expected 1 test, got %d", page.Total)
	}
	tc := page.Tests[0]
	if tc.ExecType != "" || tc.CucumberScenario != "" || tc.CucumberType != "" || tc.GenericDefinition != "" {
		t.Errorf("unmapped type/body fields should stay empty: %+v", tc)
	}
}
