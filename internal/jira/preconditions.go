package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Precondition mirrors a Xray Precondition issue (FR-13.4).
type Precondition struct {
	Key         string
	Summary     string
	Type        string
	Description string
	// Condition is the Xray precondition definition text, distinct from the Jira
	// issue description. NOTE(xtm): the condition text lives in an
	// instance-specific Xray custom field; its field id varies per deployment, so
	// Condition is left empty for live Jira until the field id can be verified on
	// a real Xray Server/DC 8.4.0 instance. Demo mode populates it.
	Condition string
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
	allPre := []Precondition{}
	allLinks := map[string][]string{}
	err := c.ListPreconditionsStream(ctx, projectKey, onProgress,
		func(pre []Precondition, links map[string][]string) error {
			allPre = append(allPre, pre...)
			for tk, pks := range links {
				allLinks[tk] = append(allLinks[tk], pks...)
			}
			return nil
		})
	if err != nil {
		return nil, nil, err
	}
	return allPre, allLinks, nil
}

// ListPreconditionsStream walks a project's Preconditions and reports them to
// onBatch in chunks of preconditionBatchSize as they are resolved, so a caller
// can persist incrementally. onProgress (optional) is called once per
// precondition as its associated Tests are read, the slow part of the pass.
//
// A non-nil error from onBatch aborts the walk and is returned wrapped, so a
// store failure cannot be silently absorbed. An instance with no Precondition
// issue type returns nil having called onBatch zero times, which callers read
// as a benign skip rather than an empty project.
func (c *Client) ListPreconditionsStream(
	ctx context.Context,
	projectKey string,
	onProgress func(done, total int),
	onBatch func(pre []Precondition, links map[string][]string) error,
) error {
	if isDemoURL(c.baseURL) {
		pre, links, err := demoPreconditionsAndLinks(themeFor(c.baseURL), projectKey)
		if err != nil {
			return err
		}
		return onBatch(pre, links)
	}

	typeID, typeName, err := c.resolvePreconditionType(ctx)
	if err != nil {
		return fmt.Errorf("resolve precondition issue type: %w", err)
	}
	if typeID == "" {
		log.Printf("xtm: no Precondition issue type on this instance — skipping precondition sync")
		return nil
	}

	preconditions, err := c.searchPreconditions(ctx, projectKey, typeID)
	if err != nil {
		return fmt.Errorf("search preconditions: %w", err)
	}

	total := len(preconditions)
	var done, dropped int64
	for start := 0; start < total; start += preconditionBatchSize {
		end := start + preconditionBatchSize
		if end > total {
			end = total
		}
		chunk := preconditions[start:end]

		// Resolve this chunk's associations concurrently, paced by the shared
		// client rate limiter. Results are collected per index so the output
		// stays in key order regardless of goroutine completion order. Reads
		// are best-effort per precondition, so one inaccessible precondition
		// costs its own links and nothing else.
		perPre := make([][]string, len(chunk))
		var progMu sync.Mutex // onProgress may not be concurrency-safe
		sem := make(chan struct{}, preconditionFetchConcurrency)
		var wg sync.WaitGroup
		for i, p := range chunk {
			if ctx.Err() != nil {
				break
			}
			sem <- struct{}{}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				if ctx.Err() != nil {
					return
				}
				testKeys, err := c.listPreconditionTestsRetrying(ctx, p.Key)
				if err != nil {
					log.Printf("xtm: precondition %s tests: %v", p.Key, err)
					atomic.AddInt64(&dropped, 1)
				} else {
					perPre[i] = testKeys
				}
				if onProgress != nil {
					n := atomic.AddInt64(&done, 1)
					progMu.Lock()
					onProgress(int(n), total)
					progMu.Unlock()
				}
			}()
		}
		wg.Wait()
		if err := ctx.Err(); err != nil {
			return err
		}

		// links: test key -> precondition keys, for this chunk only.
		links := map[string][]string{}
		for i, testKeys := range perPre {
			for _, tk := range testKeys {
				links[tk] = append(links[tk], chunk[i].Key)
			}
		}
		if err := onBatch(chunk, links); err != nil {
			return fmt.Errorf("persist precondition batch: %w", err)
		}
	}
	log.Printf("xtm: preconditions: %d found (type %q) for %s", total, typeName, projectKey)
	if dropped > 0 {
		log.Printf("xtm: preconditions: %d of %d had unreadable test links for %s", dropped, total, projectKey)
	}
	return nil
}

// preconditionBatchSize is how many preconditions are accumulated before being
// handed to the caller's onBatch. Small enough that an interrupted sync loses
// little, large enough that the store write is not per-item.
const preconditionBatchSize = 200

// preconditionFetchConcurrency bounds how many per-precondition association
// reads run at once. The shared client rate limiter (Client.do) caps the actual
// request rate at syncReqPerSec; this just keeps several requests in flight so
// the limiter stays fed, replacing the old one-at-a-time-with-a-sleep walk that
// spent 15 minutes asleep on a 6000-precondition project.
const preconditionFetchConcurrency = 8

// preconditionRetries is how many times a single association read is retried.
// The live Xray instance intermittently answers 401 with a token that worked
// moments earlier, and times out on the same endpoint; both drop that
// precondition's links silently. This is mitigation, not a root-cause fix.
const preconditionRetries = 3

// listPreconditionTestsRetrying calls listPreconditionTests, retrying with
// exponential backoff. Returns the last error if every attempt fails.
func (c *Client) listPreconditionTestsRetrying(ctx context.Context, key string) ([]string, error) {
	var lastErr error
	backoff := 500 * time.Millisecond
	for attempt := 0; attempt < preconditionRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
		}
		testKeys, err := c.listPreconditionTests(ctx, key)
		if err == nil {
			return testKeys, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

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
	}
	return keys, nil
}

// ListTestPreconditions returns the Preconditions linked to a single Test.
//
// Maps to GET /rest/raven/1.0/api/test/{key}/preconditions, verified against
// Xray Server 8.4.0 on 2026-08-21. Note the plural path segment: the singular
// /precondition returns 404, which is why the association was previously read
// only from the precondition side (see UpdateTestPreconditions below).
//
// The endpoint returns key, rank and type but no summary or description, so
// those are filled in with one batched issue search rather than one request per
// precondition. Unlike the project-wide sync this does populate Type, which the
// endpoint reports directly.
//
// A Jira key that is not a Test answers 400; that is treated as "no
// preconditions" rather than an error, matching searchPreconditions. Demo URLs
// short-circuit to generated data.
func (c *Client) ListTestPreconditions(ctx context.Context, testKey string) ([]Precondition, error) {
	if isDemoURL(c.baseURL) {
		return demoTestPreconditions(themeFor(c.baseURL), testKey)
	}

	keys, types, err := c.testPreconditionKeys(ctx, testKey)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return []Precondition{}, nil
	}

	details, err := c.preconditionDetails(ctx, keys)
	if err != nil {
		// The links are the part that matters; a failed enrichment should not
		// lose them. Fall back to key-only entries.
		log.Printf("xtm: precondition details for %s: %v", testKey, err)
		details = map[string]Precondition{}
	}

	out := make([]Precondition, 0, len(keys))
	for _, k := range keys {
		p := details[k]
		p.Key = k
		if t := types[k]; t != "" {
			p.Type = t
		}
		out = append(out, p)
	}
	return out, nil
}

// testPreconditionKeys reads the Test's associated Precondition keys (and the
// type each one reports), paging to respect Xray's per-request cap.
func (c *Client) testPreconditionKeys(ctx context.Context, testKey string) ([]string, map[string]string, error) {
	keys := []string{}
	types := map[string]string{}
	seen := map[string]bool{}

	for page := 1; page <= ravenMaxPages; page++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		path := fmt.Sprintf("/rest/raven/1.0/api/test/%s/preconditions?page=%d&limit=%d",
			testKey, page, ravenPageLimit)
		body, status, err := c.getBytesStatus(ctx, path)
		if err != nil {
			if status == http.StatusBadRequest {
				// Not a Test issue (or no longer one) — nothing to link. A 403
				// or anything else is a real failure and must surface, so an
				// unreadable test is never shown as one with no preconditions.
				log.Printf("xtm: test preconditions rejected for %s: %v", testKey, err)
				return []string{}, map[string]string{}, nil
			}
			return nil, nil, err
		}
		entries, err := parseTestPreconditions(body)
		if err != nil {
			return nil, nil, fmt.Errorf("parse preconditions for %s: %w", testKey, err)
		}
		added := 0
		for _, e := range entries {
			if e.Key == "" || seen[e.Key] {
				continue
			}
			seen[e.Key] = true
			keys = append(keys, e.Key)
			types[e.Key] = e.Type
			added++
		}
		if len(entries) < ravenPageLimit || added == 0 {
			break
		}
	}
	return keys, types, nil
}

// testPreconditionEntry is one row of the test-side association response.
type testPreconditionEntry struct {
	Key  string `json:"key"`
	Type string `json:"type"`
}

// parseTestPreconditions decodes the association response, which is a bare JSON
// array, tolerating a {"preconditions":[…]} wrapper like the other Xray
// association endpoints.
func parseTestPreconditions(body []byte) ([]testPreconditionEntry, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var entries []testPreconditionEntry
		if err := json.Unmarshal(trimmed, &entries); err != nil {
			return nil, err
		}
		return entries, nil
	}
	var wrapped struct {
		Preconditions []testPreconditionEntry `json:"preconditions"`
	}
	if err := json.Unmarshal(trimmed, &wrapped); err != nil {
		return nil, err
	}
	return wrapped.Preconditions, nil
}

// preconditionDetails fetches summary and description for the given
// Precondition keys in one search, keyed by issue key.
func (c *Client) preconditionDetails(ctx context.Context, keys []string) (map[string]Precondition, error) {
	quoted := make([]string, len(keys))
	for i, k := range keys {
		quoted[i] = `"` + k + `"`
	}
	q := url.Values{}
	q.Set("jql", fmt.Sprintf("key in (%s)", strings.Join(quoted, ", ")))
	q.Set("maxResults", strconv.Itoa(len(keys)))
	q.Set("fields", "summary,description")

	var resp struct {
		Issues []struct {
			Key    string `json:"key"`
			Fields struct {
				Summary     string `json:"summary"`
				Description string `json:"description"`
			} `json:"fields"`
		} `json:"issues"`
	}
	if err := c.get(ctx, "/rest/api/2/search?"+q.Encode(), &resp); err != nil {
		return nil, err
	}
	out := make(map[string]Precondition, len(resp.Issues))
	for _, iss := range resp.Issues {
		out[iss.Key] = Precondition{
			Key:         iss.Key,
			Summary:     iss.Fields.Summary,
			Description: iss.Fields.Description,
		}
	}
	return out, nil
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
