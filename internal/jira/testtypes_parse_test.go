package jira

import (
	"encoding/json"
	"testing"
)

func TestParseIssueTestReadsBodies(t *testing.T) {
	raw := json.RawMessage(`{
		"summary": "S",
		"customfield_20001": "Scenario: x\n Given y",
		"customfield_20002": {"value": "Scenario Outline"},
		"customfield_20003": "com.acme.Foo#bar"
	}`)
	ids := testFieldIDs{Scenario: "customfield_20001", ScenarioType: "customfield_20002", GenericDef: "customfield_20003"}
	got := parseIssueTest("1", "QA-1", raw, "", ids)
	if got.CucumberScenario == "" || got.CucumberType != "Scenario Outline" || got.GenericDefinition == "" {
		t.Errorf("bodies not parsed: %+v", got)
	}
}
