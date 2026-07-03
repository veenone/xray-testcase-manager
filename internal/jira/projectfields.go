package jira

import (
	"context"
	"fmt"
)

// demoComponentList is the synthetic component list returned for demo profiles.
var demoComponentList = []string{"Authentication", "Billing", "Core", "Reporting"}

// demoVersionList is the synthetic fix-version list returned for demo profiles.
var demoVersionList = []string{"1.5.0", "1.6.0", "1.7.0", "1.8.0"}

// ProjectComponents returns the names of all components configured for a Jira
// project. Demo profiles receive a fixed synthetic list. The live path calls
// GET /rest/api/2/project/{key}/components and plucks the "name" field from
// each element.
//
// NOTE(xtm): The live path has not been verified against a real Jira DC 8.14+
// instance. Verify before declaring the feature complete.
func (c *Client) ProjectComponents(ctx context.Context, projectKey string) ([]string, error) {
	if isDemoURL(c.baseURL) {
		out := make([]string, len(demoComponentList))
		copy(out, demoComponentList)
		return out, nil
	}

	var items []struct {
		Name string `json:"name"`
	}
	path := fmt.Sprintf("/rest/api/2/project/%s/components", projectKey)
	if err := c.get(ctx, path, &items); err != nil {
		return nil, fmt.Errorf("project components %s: %w", projectKey, err)
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if it.Name != "" {
			out = append(out, it.Name)
		}
	}
	return out, nil
}

// ProjectVersions returns the names of all fix versions configured for a Jira
// project. Demo profiles receive a fixed synthetic list. The live path calls
// GET /rest/api/2/project/{key}/versions and plucks the "name" field from each
// element.
//
// NOTE(xtm): The live path has not been verified against a real Jira DC 8.14+
// instance. Verify before declaring the feature complete.
func (c *Client) ProjectVersions(ctx context.Context, projectKey string) ([]string, error) {
	if isDemoURL(c.baseURL) {
		out := make([]string, len(demoVersionList))
		copy(out, demoVersionList)
		return out, nil
	}

	var items []struct {
		Name string `json:"name"`
	}
	path := fmt.Sprintf("/rest/api/2/project/%s/versions", projectKey)
	if err := c.get(ctx, path, &items); err != nil {
		return nil, fmt.Errorf("project versions %s: %w", projectKey, err)
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if it.Name != "" {
			out = append(out, it.Name)
		}
	}
	return out, nil
}
