package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Every field XTM resolves for Xray is resolved by the key of the plugin that
// defines it, not by its display name. A display name is a label an admin can
// reuse: the live instance carries two custom fields called "Conditions", and
// matching on the name took the wrong one for a whole release
// (RND_P_4TFINT_05-358). This pins the same protection on the rest of them,
// since a second "Test Type" or "Test Plan" would fail exactly the same way and
// just as silently.
func TestXrayFieldsResolveByPluginKeyNotDisplayName(t *testing.T) {
	cases := []struct {
		name      string
		pluginKey string
		display   string
		want      string
		resolve   func(*Client) (string, error)
	}{
		{"Test Type", xrayTestTypeType, "Test Type", "customfield_13968",
			func(c *Client) (string, error) { return c.testTypeFieldID(context.Background()) }},
		{"Cucumber Scenario", xrayCucumberScenarioType, "Cucumber Scenario", "customfield_13970",
			func(c *Client) (string, error) { return c.cucumberScenarioFieldID(context.Background()) }},
		{"Cucumber Test Type", xrayCucumberTestTypeType, "Cucumber Test Type", "customfield_13969",
			func(c *Client) (string, error) { return c.cucumberTypeFieldID(context.Background()) }},
		{"Generic Test Definition", xrayGenericDefinitionType, "Generic Test Definition", "customfield_13971",
			func(c *Client) (string, error) { return c.genericDefinitionFieldID(context.Background()) }},
		{"Conditions", xrayPreconditionEditorType, "Conditions", "customfield_13989",
			func(c *Client) (string, error) { return c.conditionFieldID(context.Background()) }},
		{"Test Environments", xrayTestEnvironmentsType, "Test Environments", "customfield_13992",
			func(c *Client) (string, error) { return c.testEnvironmentsFieldID(context.Background()) }},
		{"Test Plan", xrayTestPlanType, "Test Plan", "customfield_13994",
			func(c *Client) (string, error) { return c.testPlanFieldID(context.Background()) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/rest/api/2/field" {
					t.Errorf("unexpected request: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				_ = json.NewEncoder(w).Encode([]map[string]any{
					// A decoy sharing the display name, listed first, the way
					// the live instance orders its two "Conditions" fields.
					{"id": "customfield_10051", "name": tc.display, "custom": true,
						"schema": map[string]any{
							"type":   "option",
							"custom": "com.atlassian.jira.plugin.system.customfieldtypes:select",
						}},
					{"id": tc.want, "name": tc.display, "custom": true,
						"schema": map[string]any{"type": "string", "custom": tc.pluginKey}},
				})
			}))
			defer srv.Close()

			id, err := tc.resolve(newTestClient(srv))
			if err != nil {
				t.Fatalf("resolve %s: %v", tc.name, err)
			}
			if id != tc.want {
				t.Fatalf("%s resolved to %q, want %q", tc.name, id, tc.want)
			}
		})
	}
}

// An instance that renames a field, or an older Jira that answers without a
// schema, still resolves by the display name. That is what every one of these
// did before the plugin keys, so it must keep working.
func TestXrayFieldsStillFallBackToDisplayNames(t *testing.T) {
	cases := []struct {
		name    string
		display string
		resolve func(*Client) (string, error)
	}{
		{"Test Type", "Test Type",
			func(c *Client) (string, error) { return c.testTypeFieldID(context.Background()) }},
		{"Cucumber Scenario", "Cucumber Scenario",
			func(c *Client) (string, error) { return c.cucumberScenarioFieldID(context.Background()) }},
		{"Cucumber Test Type alias", "Scenario Type",
			func(c *Client) (string, error) { return c.cucumberTypeFieldID(context.Background()) }},
		{"Generic Test Definition", "Generic Test Definition",
			func(c *Client) (string, error) { return c.genericDefinitionFieldID(context.Background()) }},
		{"Condition alias", "Condition",
			func(c *Client) (string, error) { return c.conditionFieldID(context.Background()) }},
		{"Test Environments", "Test Environments",
			func(c *Client) (string, error) { return c.testEnvironmentsFieldID(context.Background()) }},
		{"Test Plan", "Test Plan",
			func(c *Client) (string, error) { return c.testPlanFieldID(context.Background()) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// No schema at all, so only the name can match.
				_ = json.NewEncoder(w).Encode([]map[string]any{
					{"id": "customfield_20001", "name": tc.display, "custom": true},
				})
			}))
			defer srv.Close()

			id, err := tc.resolve(newTestClient(srv))
			if err != nil {
				t.Fatalf("resolve %s: %v", tc.name, err)
			}
			if id != "customfield_20001" {
				t.Fatalf("%s resolved to %q, want customfield_20001", tc.name, id)
			}
		})
	}
}

// A field the instance simply does not have resolves to "" with no error, which
// is what lets a sync run on an instance without it.
func TestXrayFieldIDIsEmptyWhenAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "summary", "name": "Summary", "custom": false},
		})
	}))
	defer srv.Close()

	id, err := newTestClient(srv).testTypeFieldID(context.Background())
	if err != nil {
		t.Fatalf("testTypeFieldID: %v", err)
	}
	if id != "" {
		t.Fatalf("absent field resolved to %q, want empty", id)
	}
}
