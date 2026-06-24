package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// maxTestExecPages is a safety cap on the pagination loop in GetTestRuns to
// prevent runaway requests against a misbehaving server.
const maxTestExecPages = 100

// TestRun is one test's run record within a Test Execution as returned by the
// Xray Test Run REST endpoint. It carries the run status, timing, the executor
// identity, the environment label (if set), any defect keys linked to the run,
// and the run's creation / last-update timestamps (ISO-8601, empty if absent).
type TestRun struct {
	TestKey     string
	Status      string
	StartedAt   string
	FinishedAt  string
	ExecutedBy  string
	Environment string
	Defects     []string
	CreatedAt   string
	UpdatedAt   string
}

// GetTestRuns returns the test runs recorded for one Test Execution. Demo mode
// synthesizes runs deterministically from the execution key; the live path
// calls the Xray Server/DC REST API.
//
// Live endpoint: GET /rest/raven/1.0/api/testexec/{execKey}/test?detailed=true
// returning a JSON array of the execution's tests. The field names key, status,
// executedBy, startedOn, finishedOn, and defects (an array) are confirmed
// against a live Xray Server/DC instance. The loop pages with page (1-based) and
// limit, de-duplicating by test key and stopping when a page is short or adds no
// new tests (so a server that ignores the page param cannot loop), capped by
// maxTestExecPages.
//
// NOTE(xtm): a few details remain unconfirmed because the sampled execution did
// not exercise them: the pagination param names (page/limit vs an offset
// scheme), the shape of a non-empty defects element (the parser tolerates both
// plain "KEY" strings and {"key":...} objects), and the field carrying test
// environments (assumed testEnvironments; absent when no environment is set).
// The endpoint does not return per-run created/updated timestamps, so those stay
// empty on live (startedOn/finishedOn are used for the run dates instead).
func (c *Client) GetTestRuns(ctx context.Context, execKey string) ([]TestRun, error) {
	if isDemoURL(c.baseURL) {
		return demoTestRuns(execKey), nil
	}
	const limit = 100
	base := "/rest/raven/1.0/api/testexec/" + url.PathEscape(execKey) + "/test"
	var all []TestRun
	seen := make(map[string]bool)
	for page := 1; page <= maxTestExecPages; page++ {
		q := url.Values{}
		q.Set("detailed", "true")
		q.Set("page", fmt.Sprintf("%d", page))
		q.Set("limit", fmt.Sprintf("%d", limit))
		body, err := c.getBytes(ctx, base+"?"+q.Encode())
		if err != nil {
			return nil, err
		}
		pageRuns, err := parseTestExecTests(body)
		if err != nil {
			return nil, err
		}
		added := 0
		for _, r := range pageRuns {
			if seen[r.TestKey] {
				continue
			}
			seen[r.TestKey] = true
			all = append(all, r)
			added++
		}
		// Stop when the page is short (pagination exhausted) or added nothing new
		// (the server ignored the page param and re-returned the same set).
		if len(pageRuns) < limit || added == 0 {
			break
		}
	}
	return all, nil
}

// ExecPlans returns the Test Plan keys that a Test Execution is associated
// with. Demo mode derives the association deterministically from the execution
// key; the live path calls the Xray raven REST API.
//
// NOTE(xtm): Xray Server/DC does not expose a dedicated "plans for exec"
// endpoint. The most reliable approach on a live instance is to read the Test
// Plan custom field on the Test Execution issue via the Jira issue REST API
// (GET /rest/api/2/issue/<execKey>?fields=<testPlanFieldId>), where
// testPlanFieldId must be resolved per instance. Verify against a live Xray
// Server/DC 8.4.0 instance before removing the TODO marker.
func (c *Client) ExecPlans(ctx context.Context, execKey string) ([]string, error) {
	if isDemoURL(c.baseURL) {
		return demoExecPlans(execKey), nil
	}
	// TODO(xtm): resolve the Test Plan custom field id and read it from the
	// exec issue. Return nil for now so the sync path degrades gracefully.
	return nil, nil
}

// parseTestExecTests decodes the JSON array returned by the Xray Server/DC
// GET /rest/raven/1.0/api/testexec/{execKey}/test endpoint into TestRun values.
// Each element in the array represents one test and its run status within the
// execution. The parser is tolerant: unknown fields are ignored, missing
// optional fields are left empty, and objects with an empty "key" are skipped.
//
// The key, status, executedBy, startedOn, finishedOn, and defects fields are
// confirmed against a live Xray Server/DC instance. testEnvironments is assumed
// (absent when no environment is set) and the defects element shape is tolerated
// both as plain strings and as {"key":...} objects.
func parseTestExecTests(body []byte) ([]TestRun, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" || trimmed == "null" || trimmed == "[]" {
		return []TestRun{}, nil
	}

	// rawTest is a tolerant shape for one element of the testexec/test array.
	// key, status, executedBy, startedOn, finishedOn, and defects are confirmed
	// against a live instance; testEnvironments and the defects element shape are
	// tolerated defensively.
	type rawTest struct {
		Key              string            `json:"key"`
		Status           string            `json:"status"`
		StartedOn        string            `json:"startedOn"`
		FinishedOn       string            `json:"finishedOn"`
		Assignee         string            `json:"assignee"`
		ExecutedBy       string            `json:"executedBy"`
		TestEnvironments []string          `json:"testEnvironments"`
		Defects          []json.RawMessage `json:"defects"`
	}

	var raw []rawTest
	if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
		return nil, fmt.Errorf("parse testexec tests: %w", err)
	}

	out := make([]TestRun, 0, len(raw))
	for _, r := range raw {
		if r.Key == "" {
			continue
		}

		// Executor: prefer executedBy, fall back to assignee.
		executor := r.ExecutedBy
		if executor == "" {
			executor = r.Assignee
		}

		// Environment: take the first entry if present, join with ", " if multiple.
		env := ""
		if len(r.TestEnvironments) == 1 {
			env = r.TestEnvironments[0]
		} else if len(r.TestEnvironments) > 1 {
			env = strings.Join(r.TestEnvironments, ", ")
		}

		// Defects: tolerate both plain string keys and objects with a "key" field.
		var defects []string
		for _, d := range r.Defects {
			s := strings.TrimSpace(string(d))
			if len(s) >= 2 && s[0] == '"' {
				// Plain string: unquote it.
				var key string
				if err := json.Unmarshal(d, &key); err == nil && key != "" {
					defects = append(defects, key)
				}
			} else if len(s) >= 2 && s[0] == '{' {
				// Object form: extract the "key" field.
				var obj struct {
					Key string `json:"key"`
				}
				if err := json.Unmarshal(d, &obj); err == nil && obj.Key != "" {
					defects = append(defects, obj.Key)
				}
			}
		}
		if defects == nil {
			defects = []string{}
		}

		out = append(out, TestRun{
			TestKey:     r.Key,
			Status:      strings.ToUpper(strings.TrimSpace(r.Status)),
			StartedAt:   r.StartedOn,
			FinishedAt:  r.FinishedOn,
			ExecutedBy:  executor,
			Environment: env,
			Defects:     defects,
			// CreatedAt and UpdatedAt are not present on the testexec/test endpoint.
			// NOTE(xtm): verify whether Xray Server/DC 8.4.0 includes created/updated
			// timestamps in the detailed testexec/test response.
		})
	}
	return out, nil
}

// parseTestRuns decodes the JSON body of the Xray test-runs response into
// TestRun values. It tolerates null, empty, or missing fields to handle
// shape variation across Xray Server/DC versions.
func parseTestRuns(body []byte) ([]TestRun, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" || trimmed == "null" || trimmed == "[]" {
		return []TestRun{}, nil
	}

	// Xray may return different field names across versions.
	// NOTE(xtm): live created/updated field names for Xray Server/DC 8.4.0 are
	// assumed to be "createdOn"/"updatedOn" (matching the run detail endpoint).
	// Verify against a live instance and adjust before removing this marker.
	type rawRun struct {
		TestKey     string   `json:"testKey"`
		Status      string   `json:"status"`
		StartedOn   string   `json:"startedOn"`
		StartedAt   string   `json:"startedAt"`
		FinishedOn  string   `json:"finishedOn"`
		FinishedAt  string   `json:"finishedAt"`
		ExecutedBy  string   `json:"executedBy"`
		Environment string   `json:"testEnvironment"`
		Defects     []string `json:"defects"`
		CreatedOn   string   `json:"createdOn"`
		CreatedAt   string   `json:"createdAt"`
		UpdatedOn   string   `json:"updatedOn"`
		UpdatedAt   string   `json:"updatedAt"`
	}
	var raw []rawRun
	if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
		return nil, fmt.Errorf("parse test runs: %w", err)
	}
	out := make([]TestRun, 0, len(raw))
	for _, r := range raw {
		if r.TestKey == "" {
			continue
		}
		// Prefer startedOn over startedAt for forward compat.
		started := r.StartedOn
		if started == "" {
			started = r.StartedAt
		}
		finished := r.FinishedOn
		if finished == "" {
			finished = r.FinishedAt
		}
		// Prefer createdOn over createdAt; same for updated.
		created := r.CreatedOn
		if created == "" {
			created = r.CreatedAt
		}
		updated := r.UpdatedOn
		if updated == "" {
			updated = r.UpdatedAt
		}
		defects := r.Defects
		if defects == nil {
			defects = []string{}
		}
		out = append(out, TestRun{
			TestKey:     r.TestKey,
			Status:      strings.ToUpper(strings.TrimSpace(r.Status)),
			StartedAt:   started,
			FinishedAt:  finished,
			ExecutedBy:  r.ExecutedBy,
			Environment: r.Environment,
			Defects:     defects,
			CreatedAt:   created,
			UpdatedAt:   updated,
		})
	}
	return out, nil
}

// demoExecCount and demoExecPlanCount mirror the exec/plan counts in
// demoContainersAndLinks so the demo run seed stays in sync with the
// container seed.
const (
	demoExecCount = 8
	demoPlanCount = 5
)

// demoExecKeyIndex parses the 1-based exec number from a "<project>-TE-<n>"
// key, or -1 if the key does not match the pattern.
func demoExecKeyIndex(execKey string) int {
	// Accept keys in the form "<proj>-TE-<n>" only; other exec key forms
	// (cross-project, sub-task) yield no runs in the demo.
	parts := strings.Split(execKey, "-TE-")
	if len(parts) != 2 {
		return -1
	}
	n := 0
	for _, ch := range parts[1] {
		if ch < '0' || ch > '9' {
			return -1
		}
		n = n*10 + int(ch-'0')
	}
	if n < 1 || n > demoExecCount {
		return -1
	}
	return n - 1 // convert to 0-based index
}

// demoExecProjectKey extracts the project key from a "<project>-TE-<n>" exec
// key (the part before the first "-TE-").
func demoExecProjectKey(execKey string) string {
	if i := strings.Index(execKey, "-TE-"); i > 0 {
		return execKey[:i]
	}
	return "DEMO"
}

// demoExecExecutors is the pool of demo executor names assigned to runs
// deterministically, one per test within an execution, cycling through the
// pool.
var demoExecExecutors = []string{
	"alice", "bob", "carol", "dave", "eve",
}

// demoRunEnvironments maps each demo execution index to a single environment
// label consistent with the Container.Environments seeded by demoEnvironments.
// Each exec's runs all show the same environment (the first env in the exec's
// multi-env chip), which is enough for the run-history panel.
func demoRunEnvironment(execIdx int) string {
	// Mirrors the first element of demoEnvironments(execIdx).
	sets := []string{"Staging", "Prod", "Staging", "Prod", "Chrome", "Staging"}
	return sets[execIdx%len(sets)]
}

// demoRunDate returns a deterministic ISO-8601 date string for a run derived
// from the execution index and position within the exec (no time.Now(), no
// rand). The base date is 2026-05-01; position offsets advance the day by 1.
func demoRunDate(execIdx, pos int) string {
	// Days from the base date 2026-05-01. execIdx shifts the month; pos shifts
	// the day within each exec. The hour is derived from pos so start and
	// finish differ.
	day := 1 + (pos % 28)
	month := 5 + (execIdx % 8)
	if month > 12 {
		month = month - 12
	}
	hour := 8 + (pos % 8)
	return fmt.Sprintf("2026-%02d-%02dT%02d:00:00Z", month, day, hour)
}

// demoTestRuns returns a deterministic slice of TestRun values for a demo
// Test Execution, seeded from the demoContainersAndLinks membership for that
// execution. Each run's status matches the demoRunStatuses cycle; FAIL runs
// carry a defect key drawn from the demo bug pool so the cross-table story is
// coherent (FAILed tests have defects).
func demoTestRuns(execKey string) []TestRun {
	execIdx := demoExecKeyIndex(execKey)
	if execIdx < 0 {
		// Key does not match the demo pattern (e.g. cross-project or sub-task
		// exec): return no runs rather than synthesising noise.
		return []TestRun{}
	}
	projectKey := demoExecProjectKey(execKey)
	env := demoRunEnvironment(execIdx)

	// Collect the test indices that belong to this exec (mirrors the
	// demoContainersAndLinks loop: test i belongs to exec i%execCount).
	var runs []TestRun
	pos := 0
	for i := execIdx; i < demoLinkedTests && i < demoTestCount; i += demoExecCount {
		testNum := i + 1 // 1-based key suffix
		testKey := fmt.Sprintf("%s-%d", projectKey, testNum)
		status := demoRunStatuses[i%len(demoRunStatuses)]

		started := demoRunDate(execIdx, pos)
		finished := demoRunDate(execIdx, pos+1)
		executor := demoExecExecutors[pos%len(demoExecExecutors)]

		var defects []string
		if status == "FAIL" {
			// Derive a demo bug key consistent with demoBugs: bugs live in
			// the BUGS or SUP project, numbered from 100. Use the test index
			// modulo the total demo-bug count (12 summaries seeded).
			bugProject := demoBugProject
			if testNum%2 == 0 {
				bugProject = demoBugProject2
			}
			defects = []string{fmt.Sprintf("%s-%d", bugProject, 100+(testNum%12))}
		} else {
			defects = []string{}
		}

		// created_at is one hour before started_at; updated_at equals finished_at.
		// Both are deterministic (no time.Now(), no rand) so demo data is stable.
		createdAt := demoRunDate(execIdx, pos-1)
		updatedAt := finished

		runs = append(runs, TestRun{
			TestKey:     testKey,
			Status:      status,
			StartedAt:   started,
			FinishedAt:  finished,
			ExecutedBy:  executor,
			Environment: env,
			Defects:     defects,
			CreatedAt:   createdAt,
			UpdatedAt:   updatedAt,
		})
		pos++
	}
	return runs
}

// demoExecPlans returns the Test Plan key(s) associated with a demo Test
// Execution. Each execution is linked to two consecutive plans (wrapping
// around the plan pool) so the exec-plan link table has realistic data without
// every exec touching every plan.
func demoExecPlans(execKey string) []string {
	execIdx := demoExecKeyIndex(execKey)
	if execIdx < 0 {
		return []string{}
	}
	projectKey := demoExecProjectKey(execKey)
	// Two consecutive plan indices, wrapping around the pool.
	p1 := execIdx % demoPlanCount
	p2 := (execIdx + 1) % demoPlanCount
	plan1 := fmt.Sprintf("%s-TP-%d", projectKey, p1+1)
	plan2 := fmt.Sprintf("%s-TP-%d", projectKey, p2+1)
	if plan1 == plan2 {
		return []string{plan1}
	}
	return []string{plan1, plan2}
}
