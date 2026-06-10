package jira

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// ListPriorities returns the priority names valid for the *Test* issue type in
// the given project (FR-1) — not the global priority scheme. Jira scopes the
// allowed priorities per issue type via the project's priority scheme, so the
// New Test form must offer only these values; an out-of-scheme name is rejected
// at create time. Demo URLs return the demo set.
//
// Maps to the classic create-metadata endpoint
// GET /rest/api/2/issue/createmeta?projectKeys={key}&issuetypeIds={id}
//
//	&expand=projects.issuetypes.fields
//
// reading priority.allowedValues for the resolved Test issue type.
func (c *Client) ListPriorities(ctx context.Context, projectKey string) ([]string, error) {
	if isDemoURL(c.baseURL) {
		return demoPriorityList(), nil
	}
	typeID, _, err := c.resolveTestType(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve Test issue type: %w", err)
	}

	q := url.Values{}
	q.Set("projectKeys", projectKey)
	if typeID != "" {
		q.Set("issuetypeIds", typeID)
	} else {
		q.Set("issuetypeNames", "Test")
	}
	q.Set("expand", "projects.issuetypes.fields")

	var resp struct {
		Projects []struct {
			IssueTypes []struct {
				Fields struct {
					Priority struct {
						AllowedValues []struct {
							Name string `json:"name"`
						} `json:"allowedValues"`
					} `json:"priority"`
				} `json:"fields"`
			} `json:"issuetypes"`
		} `json:"projects"`
	}
	if err := c.get(ctx, "/rest/api/2/issue/createmeta?"+q.Encode(), &resp); err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	out := []string{}
	for _, pr := range resp.Projects {
		for _, it := range pr.IssueTypes {
			for _, av := range it.Fields.Priority.AllowedValues {
				name := strings.TrimSpace(av.Name)
				if name == "" {
					continue
				}
				if _, dup := seen[name]; dup {
					continue
				}
				seen[name] = struct{}{}
				out = append(out, name)
			}
		}
	}
	return out, nil
}
