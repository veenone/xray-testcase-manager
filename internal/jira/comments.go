package jira

import "context"

// AddComment posts a comment to an issue — used to record a Test review on the
// Test issue (test review). Demo URLs short-circuit to a no-op.
//
// Maps to POST /rest/api/2/issue/{key}/comment with a {"body": "..."} payload.
// NOTE(xtm): implemented against the Jira DC 8.x shape, where the comment "body"
// is a plain-text string. Atlassian document format (a nested object) is a Jira
// Cloud concern; confirm the plain-text shape on the target live instance.
func (c *Client) AddComment(ctx context.Context, issueKey, body string) error {
	if isDemoURL(c.baseURL) {
		return nil
	}
	return c.post(ctx, "/rest/api/2/issue/"+issueKey+"/comment", map[string]any{"body": body})
}
