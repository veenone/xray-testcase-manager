package spellcheck

import "testing"

func TestScanTestsStampsKeyAndCoversFields(t *testing.T) {
	c := testChecker()
	tests := []TestText{
		{Key: "T-1", Summary: "recieve", Description: "the user"},
		{Key: "T-2", GenericDefinition: "passwrd", CucumberScenario: "Given the user"},
	}
	got := ScanTests(tests, c)
	if len(got) != 2 {
		t.Fatalf("findings = %d (%+v), want 2", len(got), got)
	}
	byKey := map[string]Finding{}
	for _, f := range got {
		byKey[f.TestKey] = f
	}
	if byKey["T-1"].Field != "summary" || byKey["T-1"].Word != "recieve" {
		t.Errorf("T-1 finding = %+v", byKey["T-1"])
	}
	if byKey["T-2"].Field != "generic_definition" || byKey["T-2"].Word != "passwrd" {
		t.Errorf("T-2 finding = %+v", byKey["T-2"])
	}
}
