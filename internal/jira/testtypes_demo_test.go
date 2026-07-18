package jira

import "testing"

func TestDemoTestsHaveTypeBodies(t *testing.T) {
	theme := themeFor("demo")
	var sawCuke, sawGeneric bool
	for i := 0; i < 12; i++ {
		tc := makeDemoTest(theme, "DEMO", i)
		switch tc.ExecType {
		case "Cucumber":
			sawCuke = true
			if tc.CucumberScenario == "" || tc.CucumberType == "" {
				t.Errorf("cucumber demo %d missing scenario/type", i)
			}
		case "Generic":
			sawGeneric = true
			if tc.GenericDefinition == "" {
				t.Errorf("generic demo %d missing definition", i)
			}
		}
	}
	if !sawCuke || !sawGeneric {
		t.Fatal("expected both Cucumber and Generic demo tests within first 12")
	}
}
