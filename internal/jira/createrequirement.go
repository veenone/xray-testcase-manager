package jira

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// CreateRequirement creates a new requirement issue in Jira and returns its key.
// extraFields carries createmeta-driven custom-field values (already Jira-shaped
// by the caller, keyed by field id, e.g. customfield_14312 -> {"id":"..."}),
// merged into the create payload so instances that mark custom fields required
// on the requirement create screen succeed. Demo URLs return a synthetic key
// without contacting Jira.
func (c *Client) CreateRequirement(ctx context.Context, projectKey, issueType, summary, description, priority, components, fixVersions string, extraFields map[string]any) (string, error) {
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
	// Merge createmeta-driven extra fields. They never override the basic fields
	// above (project / issuetype / summary / description / priority / components /
	// fixVersions), since those are skipped in GetRequirementCreateFields.
	for k, v := range extraFields {
		if _, exists := fields[k]; !exists {
			fields[k] = v
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

// requirementCreateSkipFields are the requirement create-screen fields the New
// Requirement form already collects, so GetRequirementCreateFields omits them
// from the "extra required fields" it returns. Note "labels" is deliberately
// NOT skipped: unlike CreateBug, CreateRequirement never sets labels, so a
// required Labels field must surface as an extra field (else it would be
// silently dropped and the create would 400).
var requirementCreateSkipFields = map[string]bool{
	"project": true, "issuetype": true, "summary": true,
	"description": true, "priority": true, "components": true,
	"fixVersions": true,
}

// GetRequirementCreateFields returns the required fields on the requirement
// issue type's create screen beyond the ones the New Requirement form already
// collects (project / issuetype / summary / description / priority / components
// / fixVersions), so the form can render and collect them before commit. The
// result reuses BugCreateField as a generic createmeta field descriptor.
// Demo URLs return a representative set without a network call.
//
// NOTE(xtm): the available required fields vary per project/issuetype; verify
// against the live Xray Server/DC 8.4.0 instance.
func (c *Client) GetRequirementCreateFields(ctx context.Context, projectKey, issueType string) ([]BugCreateField, error) {
	if isDemoURL(c.baseURL) {
		return demoRequirementCreateFields(), nil
	}
	if strings.TrimSpace(issueType) == "" {
		issueType = "Story"
	}
	path := "/rest/api/2/issue/createmeta?projectKeys=" + url.QueryEscape(projectKey) +
		"&issuetypeNames=" + url.QueryEscape(issueType) +
		"&expand=projects.issuetypes.fields"

	var meta struct {
		Projects []struct {
			IssueTypes []struct {
				Fields map[string]struct {
					Name     string `json:"name"`
					Required bool   `json:"required"`
					Schema   struct {
						Type  string `json:"type"`
						Items string `json:"items"`
					} `json:"schema"`
					AllowedValues []struct {
						ID    string `json:"id"`
						Value string `json:"value"`
						Name  string `json:"name"`
					} `json:"allowedValues"`
				} `json:"fields"`
			} `json:"issuetypes"`
		} `json:"projects"`
	}
	if err := c.get(ctx, path, &meta); err != nil {
		return nil, err
	}

	var out []BugCreateField
	for _, proj := range meta.Projects {
		for _, it := range proj.IssueTypes {
			for id, fd := range it.Fields {
				if requirementCreateSkipFields[id] || !fd.Required {
					continue
				}
				typ := bugCreateFieldKind(fd.Schema.Type, fd.Schema.Items)
				avs := make([]BugFieldOption, 0, len(fd.AllowedValues))
				for _, av := range fd.AllowedValues {
					v := av.Value
					if v == "" {
						v = av.Name
					}
					avs = append(avs, BugFieldOption{ID: av.ID, Value: v})
				}
				out = append(out, BugCreateField{
					ID:            id,
					Name:          fd.Name,
					Required:      true,
					Type:          typ,
					AllowedValues: avs,
				})
			}
		}
	}
	return out, nil
}

// demoRequirementCreateFields returns a representative required custom field for
// demo mode (mirroring a "Req. type" select), so the full create flow works
// offline.
func demoRequirementCreateFields() []BugCreateField {
	return []BugCreateField{
		{
			ID:       "customfield_14312",
			Name:     "Req. type",
			Required: true,
			Type:     "option",
			AllowedValues: []BugFieldOption{
				{ID: "15201", Value: "Functional"},
				{ID: "15202", Value: "Non-Functional"},
				{ID: "15203", Value: "Security"},
				{ID: "15204", Value: "Performance"},
			},
		},
	}
}
