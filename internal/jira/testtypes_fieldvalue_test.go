package jira

import (
	"context"
	"testing"
)

func TestBodyFieldValuesDemoResolveEmpty(t *testing.T) {
	c := &Client{baseURL: "demo"} // demo short-circuits resolveCustomFieldID to ""
	for _, call := range []func() (string, any, bool, error){
		func() (string, any, bool, error) { return c.CucumberScenarioFieldValue(context.Background(), "x") },
		func() (string, any, bool, error) { return c.GenericDefinitionFieldValue(context.Background(), "x") },
		func() (string, any, bool, error) { return c.CucumberTypeFieldValue(context.Background(), "Scenario") },
	} {
		_, _, ok, err := call()
		if err != nil || ok {
			t.Errorf("demo should resolve empty (ok=false,no err); got ok=%v err=%v", ok, err)
		}
	}
}
