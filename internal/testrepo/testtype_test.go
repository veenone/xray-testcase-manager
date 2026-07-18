package testrepo

import (
	"strings"
	"testing"
)

func TestStepsToGherkin(t *testing.T) {
	steps := []Step{
		{Action: "open login", Data: "user=bob", Expected: "form shown"},
		{Action: "submit", Expected: "dashboard shown"},
	}
	got := StepsToGherkin("Login works", steps, "Scenario")
	if !strings.HasPrefix(got, "# generated from 2 manual steps") {
		t.Errorf("missing review header: %q", got)
	}
	for _, want := range []string{"Scenario: Login works", "When open login", "And user=bob", "Then form shown", "When submit", "Then dashboard shown"} {
		if !strings.Contains(got, want) {
			t.Errorf("gherkin missing %q in:\n%s", want, got)
		}
	}
}

func TestGherkinToSteps(t *testing.T) {
	scenario := "Scenario: x\n  Given a user\n  When they click\n  Then a page loads"
	steps := GherkinToSteps(scenario)
	if len(steps) != 3 {
		t.Fatalf("want 3 steps, got %d: %+v", len(steps), steps)
	}
	if steps[0].Action != "a user" || steps[2].Action != "a page loads" {
		t.Errorf("keyword not stripped: %+v", steps)
	}
}

func TestDefinitionRoundTripsAreNonEmpty(t *testing.T) {
	if StepsToDefinition([]Step{{Action: "a", Data: "b", Expected: "c"}}) == "" {
		t.Error("StepsToDefinition empty")
	}
	if len(DefinitionToSteps("line1\nline2")) != 2 {
		t.Error("DefinitionToSteps should split by line")
	}
	if GherkinToDefinition("Scenario: x\n Given y") == "" {
		t.Error("GherkinToDefinition empty")
	}
	if !strings.Contains(DefinitionToGherkin("Sum", "com.acme.Foo"), "Scenario: Sum") {
		t.Error("DefinitionToGherkin missing scenario header")
	}
}
