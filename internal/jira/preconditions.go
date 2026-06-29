package jira

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Precondition mirrors a Xray Precondition issue (FR-13.4).
type Precondition struct {
	Key         string
	Summary     string
	Type        string
	Description string
}

// resolvePreconditionType finds the Jira issue type used for Xray Preconditions
// on this instance and caches it. The name varies across instances — it is
// commonly "Pre-Condition" (hyphenated), sometimes "Precondition" — so matching
// is done on the letters-only form (see normalizeTypeName): an exact normalised
// "precondition" first, then any type whose normalised name contains it.
// Returns an empty id (no error) when the instance has no such type — then
// there are no Preconditions to sync and none can be created.
func (c *Client) resolvePreconditionType(ctx context.Context) (id, name string, err error) {
	c.precondTypeOnce.Do(func() {
		var types []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Subtask bool   `json:"subtask"`
		}
		if e := c.get(ctx, "/rest/api/2/issuetype", &types); e != nil {
			c.precondTypeErr = e
			return
		}
		// Match on the letters-only name so "Precondition", "Pre-Condition",
		// "Pre Condition" and "PreCondition" all resolve. Prefer an exact
		// normalised match, then a contains-match fallback.
		for _, t := range types {
			if normalizeTypeName(t.Name) == "precondition" {
				c.precondTypeID, c.precondTypeName = t.ID, t.Name
				return
			}
		}
		for _, t := range types {
			if strings.Contains(normalizeTypeName(t.Name), "precondition") {
				c.precondTypeID, c.precondTypeName = t.ID, t.Name
				return
			}
		}
	})
	return c.precondTypeID, c.precondTypeName, c.precondTypeErr
}

// normalizeTypeName lowercases a name and drops every non-letter, so spacing
// and punctuation variants of an issue-type name compare equal.
func normalizeTypeName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ListPreconditions returns all Preconditions for a project plus a mapping
// from Test key to the keys of the Preconditions linked to it. Demo URLs
// short-circuit to generated data.
//
// Xray Server/DC has no single "list all preconditions" endpoint, so this
// searches Precondition issues by JQL (paged) for the master list, then reads
// each Precondition's associated Tests via the Xray association endpoint
// (GET /rest/raven/1.0/api/precondition/{key}/test) to build the link map —
// one request per Precondition rather than per Test, which scales with the
// (usually small) number of Preconditions.
//
// NOTE(xtm): the precondition *type* lives in an instance-specific custom field
// that JQL search can't address by name, so Type is left empty for live Jira
// pending verification on a real Xray Server 8.4.0 instance; demo populates it.
// onProgress (optional) is called once per precondition as its associated Tests
// are read — the slow part of a precondition sync — so the UI can show progress.
func (c *Client) ListPreconditions(ctx context.Context, projectKey string, onProgress func(done, total int)) ([]Precondition, map[string][]string, error) {
	if isDemoURL(c.baseURL) {
		return demoPreconditionsAndLinks(themeFor(c.baseURL), projectKey)
	}

	typeID, typeName, err := c.resolvePreconditionType(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve precondition issue type: %w", err)
	}
	if typeID == "" {
		log.Printf("xtm: no Precondition issue type on this instance — skipping precondition sync")
		return []Precondition{}, map[string][]string{}, nil
	}

	preconditions, err := c.searchPreconditions(ctx, projectKey, typeID)
	if err != nil {
		return nil, nil, fmt.Errorf("search preconditions: %w", err)
	}

	// test key -> set of precondition keys, built from each precondition's
	// associated tests. Best-effort per precondition so one inaccessible
	// precondition can't abort the whole sync.
	links := map[string][]string{}
	total := len(preconditions)
	for i, p := range preconditions {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		testKeys, err := c.listPreconditionTests(ctx, p.Key)
		if err != nil {
			log.Printf("xtm: precondition %s tests: %v", p.Key, err)
		} else {
			for _, tk := range testKeys {
				links[tk] = append(links[tk], p.Key)
			}
		}
		if onProgress != nil {
			onProgress(i+1, total)
		}
		time.Sleep(throttlePreconditions)
	}
	log.Printf("xtm: preconditions: %d found (type %q) for %s", len(preconditions), typeName, projectKey)
	return preconditions, links, nil
}

// throttlePreconditions paces the per-precondition association reads.
const throttlePreconditions = 150 * time.Millisecond

// searchPreconditions finds every Precondition issue in a project via JQL,
// matching by issue-type id (robust to renamed/localised types), paging until
// the reported total is reached.
func (c *Client) searchPreconditions(ctx context.Context, projectKey, typeID string) ([]Precondition, error) {
	jql := fmt.Sprintf(`project = "%s" AND issuetype = %s ORDER BY key ASC`, projectKey, typeID)

	out := []Precondition{}
	startAt := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		q := url.Values{}
		q.Set("jql", jql)
		q.Set("startAt", strconv.Itoa(startAt))
		q.Set("maxResults", "100")
		q.Set("fields", "summary,description")

		var resp struct {
			Total  int `json:"total"`
			Issues []struct {
				Key    string `json:"key"`
				Fields struct {
					Summary     string `json:"summary"`
					Description string `json:"description"`
				} `json:"fields"`
			} `json:"issues"`
		}
		if err := c.get(ctx, "/rest/api/2/search?"+q.Encode(), &resp); err != nil {
			var he *HTTPError
			if errors.As(err, &he) && he.Code == http.StatusBadRequest {
				log.Printf("xtm: precondition search rejected: %v", err)
				return []Precondition{}, nil
			}
			return nil, err
		}
		for _, iss := range resp.Issues {
			out = append(out, Precondition{
				Key:         iss.Key,
				Summary:     iss.Fields.Summary,
				Description: iss.Fields.Description,
			})
		}
		startAt += len(resp.Issues)
		if len(resp.Issues) == 0 || startAt >= resp.Total {
			break
		}
		time.Sleep(throttlePreconditions)
	}
	return out, nil
}

// listPreconditionTests returns the Test keys associated with one Precondition.
// Maps to GET /rest/raven/1.0/api/precondition/{key}/test, which returns a bare
// array of {id, key, ...}; tolerates a {"tests":[…]} wrapper like the other
// Xray association endpoints. Paged to respect Xray's per-request cap (200).
func (c *Client) listPreconditionTests(ctx context.Context, preconditionKey string) ([]string, error) {
	keys := []string{}
	seen := map[string]bool{}
	for page := 1; page <= ravenMaxPages; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path := fmt.Sprintf("/rest/raven/1.0/api/precondition/%s/test?page=%d&limit=%d",
			preconditionKey, page, ravenPageLimit)
		body, err := c.getBytes(ctx, path)
		if err != nil {
			return nil, err
		}
		links, err := parseContainerTests(KindTestSet, preconditionKey, body)
		if err != nil {
			return nil, err
		}
		added := 0
		for _, l := range links {
			if seen[l.TestKey] {
				continue
			}
			seen[l.TestKey] = true
			keys = append(keys, l.TestKey)
			added++
		}
		if len(links) < ravenPageLimit || added == 0 {
			break
		}
		time.Sleep(throttlePreconditions)
	}
	return keys, nil
}

// CreatePrecondition creates a new Precondition issue and returns its key
// (FR-13.5). Demo URLs short-circuit to a no-op, returning an empty key (the
// placeholder is reconciled on the next sync).
//
// Maps to POST /rest/api/2/issue with the resolved Precondition issue type id
// (the type name varies per instance), summary and description. NOTE(xtm): Xray
// stores the precondition type (Manual / Generic / Cucumber) in an
// instance-specific custom field; setting it on create needs that field id, so
// ptype is accepted but not sent until it can be verified on a live instance.
func (c *Client) CreatePrecondition(ctx context.Context, projectKey, summary, ptype, description string) (string, error) {
	_ = ptype
	if isDemoURL(c.baseURL) {
		return "", nil
	}
	typeID, _, err := c.resolvePreconditionType(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve precondition issue type: %w", err)
	}
	if typeID == "" {
		return "", fmt.Errorf("this Jira instance has no Precondition issue type")
	}
	fields := map[string]any{
		"project":   map[string]string{"key": projectKey},
		"issuetype": map[string]string{"id": typeID},
		"summary":   summary,
	}
	if strings.TrimSpace(description) != "" {
		fields["description"] = description
	}
	body := map[string]any{"fields": fields}
	var resp struct {
		Key string `json:"key"`
	}
	if err := c.writeJSONReturning(ctx, http.MethodPost, "/rest/api/2/issue", body, &resp); err != nil {
		return "", err
	}
	return resp.Key, nil
}

// UpdateTestPreconditions associates / disassociates Preconditions with a Test
// (FR-13.5 / 13.6). add and remove are Precondition keys. Demo URLs
// short-circuit to a no-op.
//
// Xray Server/DC exposes the association from the *precondition* side, not the
// test side — POST /rest/raven/1.0/api/precondition/{preconditionKey}/test with
// an {"add":[testKey]} / {"remove":[testKey]} body (the test-side path returns
// 404). This is the write counterpart of the GET used during sync, so one POST
// is issued per precondition.
func (c *Client) UpdateTestPreconditions(ctx context.Context, testKey string, add, remove []string) error {
	if len(add) == 0 && len(remove) == 0 {
		return nil
	}
	if isDemoURL(c.baseURL) {
		return nil
	}
	for _, pk := range add {
		if err := c.post(ctx,
			fmt.Sprintf("/rest/raven/1.0/api/precondition/%s/test", pk),
			map[string]any{"add": []string{testKey}},
		); err != nil {
			return err
		}
	}
	for _, pk := range remove {
		if err := c.post(ctx,
			fmt.Sprintf("/rest/raven/1.0/api/precondition/%s/test", pk),
			map[string]any{"remove": []string{testKey}},
		); err != nil {
			return err
		}
	}
	return nil
}

// DeletePrecondition deletes a Precondition issue (FR-13.4 management). Demo
// URLs short-circuit to a no-op.
//
// Maps to DELETE /rest/api/2/issue/{key}. NOTE(xtm): issue deletion can be
// permission-restricted; verify on a live instance.
func (c *Client) DeletePrecondition(ctx context.Context, preconditionKey string) error {
	if isDemoURL(c.baseURL) {
		return nil
	}
	return c.delete(ctx, "/rest/api/2/issue/"+preconditionKey)
}
