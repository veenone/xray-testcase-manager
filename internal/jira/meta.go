package jira

import (
	"context"
	"time"
)

// jiraUser is the subset of a Jira user object the meta fetch needs.
type jiraUser struct {
	DisplayName string `json:"displayName"`
	Name        string `json:"name"`
}

// TestMeta is the issue-level metadata shown in the Test detail summary: who
// created the Test and when, when it was last updated, and who made that last
// change (from the issue changelog).
type TestMeta struct {
	Created   string `json:"created"`
	Creator   string `json:"creator"`
	Updated   string `json:"updated"`
	UpdatedBy string `json:"updatedBy"`
}

// GetTestMeta fetches a Test's created / creator / updated fields plus the
// author of its most recent change (FR-2). Demo URLs return generated metadata.
//
// Maps to GET /rest/api/2/issue/{key}?fields=created,updated,creator,reporter
// &expand=changelog — the last changelog history's author is "last updated by",
// falling back to the creator when there is no history.
func (c *Client) GetTestMeta(ctx context.Context, key string) (TestMeta, error) {
	if isDemoURL(c.baseURL) {
		return demoTestMeta(key), nil
	}
	var resp struct {
		Fields struct {
			Created  string    `json:"created"`
			Updated  string    `json:"updated"`
			Creator  *jiraUser `json:"creator"`
			Reporter *jiraUser `json:"reporter"`
		} `json:"fields"`
		Changelog struct {
			Histories []struct {
				Created string   `json:"created"`
				Author  jiraUser `json:"author"`
			} `json:"histories"`
		} `json:"changelog"`
	}
	if err := c.get(ctx,
		"/rest/api/2/issue/"+key+"?fields=created,updated,creator,reporter&expand=changelog",
		&resp); err != nil {
		return TestMeta{}, err
	}

	meta := TestMeta{Created: resp.Fields.Created, Updated: resp.Fields.Updated}
	meta.Creator = displayName(resp.Fields.Creator, resp.Fields.Reporter)

	// "Last updated by" is the author of the most recent change. Pick by latest
	// timestamp rather than relying on the array order (Jira returns oldest-first,
	// but be robust); same-issue history timestamps share a timezone, so a string
	// compare orders them correctly.
	latest := ""
	for _, h := range resp.Changelog.Histories {
		if h.Created >= latest {
			latest = h.Created
			meta.UpdatedBy = orEmpty(h.Author.DisplayName, h.Author.Name)
		}
	}
	if meta.UpdatedBy == "" {
		meta.UpdatedBy = meta.Creator
	}
	return meta, nil
}

// displayName returns the first non-empty display name (then login name) among
// the given users.
func displayName(users ...*jiraUser) string {
	for _, u := range users {
		if u == nil {
			continue
		}
		if n := orEmpty(u.DisplayName, u.Name); n != "" {
			return n
		}
	}
	return ""
}

func orEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// demoTestMeta returns deterministic metadata for a demo Test.
func demoTestMeta(key string) TestMeta {
	h := 0
	for _, r := range key {
		h = h*31 + int(r)
	}
	if h < 0 {
		h = -h
	}
	creators := []string{"Alice Tester", "Bob Reviewer", "Carol QA", "David Lead"}
	editors := []string{"Eve Editor", "Frank Maintainer", "Grace Author", "Heidi Triage"}
	created := time.Now().AddDate(0, 0, -(h%600 + 30)).Format("2006-01-02T15:04:05.000-0700")
	updated := time.Now().AddDate(0, 0, -(h % 30)).Format("2006-01-02T15:04:05.000-0700")
	return TestMeta{
		Created:   created,
		Creator:   creators[h%len(creators)],
		Updated:   updated,
		UpdatedBy: editors[h%len(editors)],
	}
}
