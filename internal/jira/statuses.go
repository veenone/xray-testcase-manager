package jira

import (
	"context"
	"strings"
)

// ListStatuses returns the workflow status names available to the Test issue
// type in a project (FR-4). It reads the authoritative list from Jira rather
// than inferring it from synced data, so a status no Test currently holds is
// still offered in the filter. Demo URLs return the demo workflow's statuses.
//
// Maps to GET /rest/api/2/project/{projectKey}/statuses, which returns the
// status set per issue type; we prefer the "Test" issue type and fall back to
// the union across issue types if it isn't present.
func (c *Client) ListStatuses(ctx context.Context, projectKey string) ([]string, error) {
	if isDemoURL(c.baseURL) {
		return demoStatusList(), nil
	}
	var resp []struct {
		Name     string `json:"name"` // issue type name
		Statuses []struct {
			Name string `json:"name"`
		} `json:"statuses"`
	}
	if err := c.get(ctx, "/rest/api/2/project/"+projectKey+"/statuses", &resp); err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	out := []string{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, dup := seen[name]; dup {
			return
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}

	for _, it := range resp {
		if strings.EqualFold(strings.TrimSpace(it.Name), "Test") {
			for _, s := range it.Statuses {
				add(s.Name)
			}
		}
	}
	if len(out) == 0 {
		// No "Test" issue type matched — union every issue type's statuses.
		for _, it := range resp {
			for _, s := range it.Statuses {
				add(s.Name)
			}
		}
	}
	return out, nil
}
