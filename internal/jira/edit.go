package jira

import (
	"context"
	"strings"
)

// UpdateIssue PUTs field updates for a Jira issue (FR-2.3 / Phase 2 commit).
// Demo URLs short-circuit to a no-op so commit in demo mode just clears
// local pending changes without making any HTTP calls.
func (c *Client) UpdateIssue(ctx context.Context, key string, fields map[string]any) error {
	if isDemoURL(c.baseURL) {
		return nil
	}
	body := map[string]any{"fields": fields}
	return c.put(ctx, "/rest/api/2/issue/"+key, body)
}

// GetIssueUpdated returns the current `updated` timestamp of a Jira issue.
// Used as the conflict pre-check before a commit (FR-1.4) — compared to the
// pending change's base_version. Demo URLs return "" so the caller can
// short-circuit the check.
func (c *Client) GetIssueUpdated(ctx context.Context, key string) (string, error) {
	if isDemoURL(c.baseURL) {
		return "", nil
	}
	var resp struct {
		Fields struct {
			Updated string `json:"updated"`
		} `json:"fields"`
	}
	if err := c.get(ctx, "/rest/api/2/issue/"+key+"?fields=updated", &resp); err != nil {
		return "", err
	}
	return resp.Fields.Updated, nil
}

// FieldsForJira translates the app's internal field/value pairs into the
// Jira REST field-update payload shape:
//
//   - summary, description: passed through as strings
//   - priority:             wrapped in {"name": ...}
//   - labels:               space-separated string split into a string array
//
// exec_type (the Xray Test Type) is NOT mapped here because it targets an
// instance-specific custom field id that needs the Client to resolve. It is
// handled at the commit call site (internal/syncer/commit.go), which resolves
// the Test Type field id and adds {"value": ...} under that id before the PUT.
// Unknown fields are silently dropped so the payload stays defensive against
// a stale or corrupted pending_change row.
func FieldsForJira(updates map[string]string) map[string]any {
	out := make(map[string]any, len(updates))
	for f, v := range updates {
		switch f {
		case "summary", "description":
			out[f] = v
		case "priority":
			out["priority"] = map[string]string{"name": v}
		case "labels":
			labels := strings.Fields(v)
			if labels == nil {
				labels = []string{}
			}
			out["labels"] = labels
		}
	}
	return out
}

// ExecTypeFieldValue resolves this instance's Xray "Test Type" custom field id
// and returns it together with the option-shaped value Jira expects
// ({"value": execType}) for a field-update PUT. It returns ok=false (no error)
// when the field id cannot be resolved on this instance, so the commit can push
// the rest of the field update and skip exec_type rather than fail. Demo mode
// short-circuits in testTypeFieldID, so this also returns ok=false there (demo
// UpdateIssue is a no-op anyway).
func (c *Client) ExecTypeFieldValue(ctx context.Context, execType string) (fieldID string, value any, ok bool, err error) {
	id, err := c.testTypeFieldID(ctx)
	if err != nil {
		return "", nil, false, err
	}
	if id == "" {
		return "", nil, false, nil
	}
	return id, map[string]string{"value": execType}, true, nil
}

// CucumberScenarioFieldValue resolves this instance's Xray "Cucumber Scenario"
// custom field id and returns it with the plain-string value Jira expects for a
// text field PUT. Returns ok=false (no error) when the field id cannot be
// resolved on this instance so the commit skips this field rather than fail.
func (c *Client) CucumberScenarioFieldValue(ctx context.Context, v string) (string, any, bool, error) {
	return c.textCustomFieldValue(ctx, c.cucumberScenarioFieldID, v)
}

// GenericDefinitionFieldValue resolves this instance's Xray "Generic Test
// Definition" custom field id and returns it with the plain-string value Jira
// expects for a text field PUT. Returns ok=false (no error) when the field id
// cannot be resolved on this instance so the commit skips this field rather
// than fail.
func (c *Client) GenericDefinitionFieldValue(ctx context.Context, v string) (string, any, bool, error) {
	return c.textCustomFieldValue(ctx, c.genericDefinitionFieldID, v)
}

// CucumberTypeFieldValue resolves this instance's Xray "Cucumber Test Type"
// custom field id and returns it with the option-shaped value Jira expects
// ({"value": v}) for a select/option field PUT. Returns ok=false (no error)
// when the field id cannot be resolved on this instance so the commit skips
// this field rather than fail.
func (c *Client) CucumberTypeFieldValue(ctx context.Context, v string) (string, any, bool, error) {
	id, err := c.cucumberTypeFieldID(ctx)
	if err != nil || id == "" {
		return "", nil, false, err
	}
	return id, map[string]string{"value": v}, true, nil
}

// textCustomFieldValue is a shared helper for text (plain-string) custom field
// helpers. It calls resolve to obtain the field id, returning ok=false (no
// error) when the id is empty so the caller can degrade gracefully.
func (c *Client) textCustomFieldValue(ctx context.Context, resolve func(context.Context) (string, error), v string) (string, any, bool, error) {
	id, err := resolve(ctx)
	if err != nil || id == "" {
		return "", nil, false, err
	}
	return id, v, true, nil
}
