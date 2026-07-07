package jira

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// CreateRequirement creates a new requirement issue in Jira and returns its key.
// Demo URLs return a synthetic key without contacting Jira.
//
// NOTE(xtm): the live path uses the issue-type name directly (no createmeta
// lookup). Required custom fields per project/issuetype are applied on commit
// with only standard fields; verify against a live Xray Server 8.4.0 instance
// and add createmeta integration in Phase-7.
func (c *Client) CreateRequirement(ctx context.Context, projectKey, issueType, summary, description, priority, components, fixVersions string) (string, error) {
	if isDemoURL(c.baseURL) {
		return fmt.Sprintf("%s-REQ-DEMO", projectKey), nil
	}
	if strings.TrimSpace(issueType) == "" {
		issueType = "Story"
	}
	fields := map[string]any{
		"project":   map[string]string{"key": projectKey},
		"issuetype": map[string]string{"name": issueType},
		"summary":   summary,
	}
	if strings.TrimSpace(description) != "" {
		fields["description"] = description
	}
	if strings.TrimSpace(priority) != "" {
		fields["priority"] = map[string]string{"name": priority}
	}
	if strings.TrimSpace(components) != "" {
		comps := []map[string]string{}
		for _, comp := range strings.Split(components, ",") {
			if s := strings.TrimSpace(comp); s != "" {
				comps = append(comps, map[string]string{"name": s})
			}
		}
		if len(comps) > 0 {
			fields["components"] = comps
		}
	}
	if strings.TrimSpace(fixVersions) != "" {
		fvs := []map[string]string{}
		for _, v := range strings.Split(fixVersions, ",") {
			if s := strings.TrimSpace(v); s != "" {
				fvs = append(fvs, map[string]string{"name": s})
			}
		}
		if len(fvs) > 0 {
			fields["fixVersions"] = fvs
		}
	}
	var resp struct {
		Key string `json:"key"`
	}
	if err := c.writeJSONReturning(ctx, http.MethodPost, "/rest/api/2/issue", map[string]any{"fields": fields}, &resp); err != nil {
		return "", err
	}
	return resp.Key, nil
}
