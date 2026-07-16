package kiwi

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"xray-test-manager/internal/backend"
)

// Adapter implements backend.Backend against a Kiwi TCMS instance's
// JSON-RPC API. P4.1 built TestConnection, Capabilities, IsDemo,
// SetRequirementLinkType, RemoteAhead, and the transport/auth/error-typing/
// plugin-detection-probe mechanism they sit on. P4.2 added the core read
// mapping: the test pull (SearchTestsPage/GetTestFields/GetTestSteps/
// ListTestsBasic/GetTestMeta), containers + runs (ListContainers/
// TestExecutionsForTest/GetTestRuns/ExecPlans), metadata (ListStatuses/
// ListPriorities/ProjectComponents/ProjectVersions), and the content-hash
// RemoteVersion. P4.3 (this task) wires the plugin-detection probe into
// TestConnection/Capabilities (hasRequirementsPlugin/hasReviewPlugin) and
// implements the requirements-plugin read path (ListRequirements, via
// Requirement.filter + Requirement.coverage — see requirements.go). Review
// plugin integration beyond caching hasReviewPlugin is explicitly DEFERRED
// (needs a backend.Backend interface extension — see hasReviewPlugin's doc
// comment). Bugs, req->req links, and every remaining WRITE method stay
// stubs (Phase 5), and the genuinely-no-analog EMPTY reads (preconditions,
// folders, transitions, custom fields) stay zero-value per §3 of
// p4_0-kiwi-integration-spec.md.
type Adapter struct {
	c *Client

	// hasRequirementsPlugin is set once by TestConnection's plugin-detection
	// probe (spec §4.3, P4.3 brief item (a)): true when the
	// kiwi-tcms-requirements plugin's Requirement.filter RPC is registered
	// on the server. Capabilities() reads it to flip
	// SupportsRequirementObjects, and ListRequirements reads it to decide
	// between the real plugin-backed read and the EMPTY (base-Kiwi) return.
	// Zero-value (false) is the safe default before TestConnection has run —
	// same "off until proven present" behavior as a confirmed-absent probe.
	hasRequirementsPlugin bool

	// hasReviewPlugin is set by the same TestConnection probe, against the
	// kiwi-tcms-review-workflow plugin's ReviewRequest.filter RPC. It is
	// cached here and DELIBERATELY UNEXPOSED in this task: per the P4.3
	// brief, full review-plugin integration (a new backend.Capabilities
	// SupportsReview field, a review-read interface method, and the
	// XTM-verdict mapping) is an interface extension deferred to a future
	// task. This flag exists purely so that future task doesn't have to
	// redo detection.
	hasReviewPlugin bool
}

// New builds a Kiwi backend.Backend against baseURL, authenticating with
// credential ("username:password" by default — see auth.go). No I/O is
// performed at construction; authentication happens lazily via Client.Login
// (TestConnection calls it directly).
func New(baseURL, credential string, opts ...Option) *Adapter {
	return &Adapter{c: NewClient(baseURL, credential, opts...)}
}

// NewFromClient wraps an already-constructed *Client. Tests use this to
// hand the Adapter a Client pointed at an httptest.Server, or one wired
// with a custom Authenticator.
func NewFromClient(c *Client) *Adapter { return &Adapter{c: c} }

// Compile-time assertion that Adapter satisfies the full Backend interface,
// even though most methods are stubs in this task.
var _ backend.Backend = (*Adapter)(nil)

// --- connection / auth ---

// TestConnection logs in via Auth.login (spec §1.2 Option A / sessionLogin)
// then resolves the authenticated user via User.filter({"is_active":true}),
// taking the first (and, for a non-staff Kiwi user, typically only) result.
// Spec §3.1.
//
// Once the core connection succeeds, TestConnection also runs the P4.3
// plugin-detection probe (spec §4.3): it has the ctx Capabilities() lacks,
// which is why detection lives here rather than in Capabilities() itself
// (design constraint from the P4.3 brief). A probe failure never fails
// TestConnection as a whole — see detectPlugin's doc comment in caps.go for
// exactly how each outcome (absent / installed-but-degraded / unknown
// transport failure) is classified.
func (a *Adapter) TestConnection(ctx context.Context) (*backend.User, error) {
	if err := a.c.Login(ctx); err != nil {
		return nil, err
	}
	var users []kiwiUser
	if err := a.c.call(ctx, "User.filter", []any{map[string]any{"is_active": true}}, &users); err != nil {
		return nil, err
	}

	a.hasRequirementsPlugin = detectPlugin(ctx, a.c, requirementsProbeMethod)
	a.hasReviewPlugin = detectPlugin(ctx, a.c, reviewProbeMethod)

	if len(users) == 0 {
		return &backend.User{}, nil
	}
	return toUser(users[0]), nil
}

// IsDemo reports whether this adapter targets the deterministic offline
// demo generator. Always false for now — the kiwi-demo short-circuit is
// P4.4.
func (a *Adapter) IsDemo() bool { return false }

// SetRequirementLinkType is a no-op today: Kiwi core has no requirement
// link-type concept, and the requirements-plugin typed links
// (verifies/validates/derives-from/related) aren't wired until P4.3.
func (a *Adapter) SetRequirementLinkType(name string) {}

// --- tests ---

// fetchTestCases calls TestCase.filter(filter) and decodes the result into
// []kiwiTestCase (spec §3.2/§9.1b).
func (a *Adapter) fetchTestCases(ctx context.Context, filter map[string]any) ([]kiwiTestCase, error) {
	var rows []kiwiTestCase
	if err := a.c.call(ctx, "TestCase.filter", []any{filter}, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// fetchTestCaseByID fetches a single TestCase by pk via
// TestCase.filter({"pk": id}), erroring if no row is returned.
func (a *Adapter) fetchTestCaseByID(ctx context.Context, id int) (kiwiTestCase, error) {
	rows, err := a.fetchTestCases(ctx, map[string]any{"pk": id})
	if err != nil {
		return kiwiTestCase{}, err
	}
	if len(rows) == 0 {
		return kiwiTestCase{}, fmt.Errorf("kiwi: test case %d not found", id)
	}
	return rows[0], nil
}

// SearchTestsPage is the core test pull (spec §3.1, §3.2). Kiwi's
// TestCase.filter has no offset/limit in its RPC signature (spec §6): it
// always returns the FULL matching array, so pagination is emulated
// client-side by sorting the result by pk ascending and slicing
// [startAt:startAt+maxResults] — a fresh full-scope fetch per call (no
// cross-call id cache in this task; see p4_2-report.md "pagination
// approach" for the perf tradeoff, spec OQ-7).
//
// projectKey narrows via `category__product__name` — the exact dunder
// lookup path spec §2 gives as Kiwi's product-scoping example
// (`TestCase.filter({"category__product__name": <product>})`). scopeJQL is
// always ignored (Kiwi has no JQL; spec §2). since is always ignored: Kiwi's
// TestCase has no native `updated` field to filter on server-side, so a
// Kiwi pull is always "full" and the hub diffs locally via content-hash
// (spec §5 OQ-2).
func (a *Adapter) SearchTestsPage(ctx context.Context, projectKey, scopeJQL, since string, startAt, maxResults int) ([]backend.Test, int, error) {
	filter := map[string]any{}
	if projectKey != "" {
		filter["category__product__name"] = projectKey
	}
	rows, err := a.fetchTestCases(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })

	total := len(rows)
	if startAt < 0 {
		startAt = 0
	}
	if startAt >= total {
		return []backend.Test{}, total, nil
	}
	end := total
	if maxResults > 0 && startAt+maxResults < total {
		end = startAt + maxResults
	}
	page := rows[startAt:end]
	out := make([]backend.Test, len(page))
	for i, tc := range page {
		out[i] = toTest(tc)
	}
	return out, total, nil
}

// ListTestsBasic maps keys (Kiwi pks, stringified) via
// TestCase.filter({"pk__in": [...]}) per spec §3.1.
func (a *Adapter) ListTestsBasic(ctx context.Context, keys []string) ([]backend.TestBasic, error) {
	if len(keys) == 0 {
		return []backend.TestBasic{}, nil
	}
	ids := make([]int, len(keys))
	for i, k := range keys {
		id, err := parseKiwiID(k)
		if err != nil {
			return nil, err
		}
		ids[i] = id
	}
	rows, err := a.fetchTestCases(ctx, map[string]any{"pk__in": ids})
	if err != nil {
		return nil, err
	}
	out := make([]backend.TestBasic, len(rows))
	for i, tc := range rows {
		out[i] = toTestBasic(tc)
	}
	return out, nil
}

// GetTestFields is a single TestCase refetch by pk (spec §3.1).
func (a *Adapter) GetTestFields(ctx context.Context, key string) (backend.Test, error) {
	id, err := parseKiwiID(key)
	if err != nil {
		return backend.Test{}, err
	}
	tc, err := a.fetchTestCaseByID(ctx, id)
	if err != nil {
		return backend.Test{}, err
	}
	return toTest(tc), nil
}

func (a *Adapter) CreateTest(ctx context.Context, projectKey, summary, description, priority string, labels, components []string) (string, error) {
	return "", backend.ErrUnsupported // P4.2 (write, out of P4 read scope)
}

func (a *Adapter) UpdateIssue(ctx context.Context, key string, fields map[string]any) error {
	return backend.ErrUnsupported // P4.2 (write)
}

// GetTestMeta maps author/created fields per spec §3.1 ("Updated
// best-effort"); see toTestMeta for why Updated/UpdatedBy stay empty.
func (a *Adapter) GetTestMeta(ctx context.Context, key string) (backend.TestMeta, error) {
	id, err := parseKiwiID(key)
	if err != nil {
		return backend.TestMeta{}, err
	}
	tc, err := a.fetchTestCaseByID(ctx, id)
	if err != nil {
		return backend.TestMeta{}, err
	}
	return toTestMeta(tc), nil
}

// --- concurrency ---

// RemoteVersion computes the content-hash token (spec §5 option 2) for the
// TestCase identified by externalKey. entityType is accepted but unused:
// every caller in this codebase passes "test" (internal/syncer/commit.go),
// and Kiwi has no other entity kind with pending-change concurrency
// tracking in P4.2 (preconditions/containers are read-only here). See
// version.go for the exact hashed field set.
func (a *Adapter) RemoteVersion(ctx context.Context, entityType, externalKey string) (backend.VersionToken, error) {
	id, err := parseKiwiID(externalKey)
	if err != nil {
		return "", err
	}
	tc, err := a.fetchTestCaseByID(ctx, id)
	if err != nil {
		return "", err
	}
	return backend.VersionToken(contentHash(tc)), nil
}

// RemoteAhead implements the content-hash ordering rule from spec §5: two
// tokens can only be compared for inequality, not ordered, so "ahead" means
// "different". Both an empty base AND an empty remote are treated
// conservatively as "not ahead": an empty token means "no version info yet"
// (RemoteVersion not wired, or the entity was never read), and neither side
// should manufacture a spurious ahead/conflict signal from a missing value.
// (Spec's literal formula `base != "" && base != remote` would also report
// "ahead" when remote=="" and base!="" ; we additionally guard remote==""
// for the same reason we guard base=="" — documented deviation, not an
// invented rule.)
func (a *Adapter) RemoteAhead(base, remote backend.VersionToken) bool {
	if base == "" || remote == "" {
		return false
	}
	return base != remote
}

// --- steps ---

// GetTestSteps returns the flattened steps for one TestCase (spec §3.3,
// §7), using the SAME flattenSteps helper toTest calls when building
// Test.Description — the shared transform the brief requires.
func (a *Adapter) GetTestSteps(ctx context.Context, key string) ([]backend.Step, error) {
	id, err := parseKiwiID(key)
	if err != nil {
		return nil, err
	}
	tc, err := a.fetchTestCaseByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return flattenSteps(tc.Text), nil
}

func (a *Adapter) CreateTestStep(ctx context.Context, key, action, data, expected string) (string, error) {
	return "", backend.ErrUnsupported // P4.2 (write)
}

func (a *Adapter) UpdateTestStep(ctx context.Context, key, stepID string, fields map[string]string) error {
	return backend.ErrUnsupported // P4.2 (write)
}

func (a *Adapter) DeleteTestStep(ctx context.Context, key, stepID string) error {
	return backend.ErrUnsupported // P4.2 (write)
}

func (a *Adapter) MoveTestStep(ctx context.Context, key, stepID string, index int, action, data, expected string) error {
	return backend.ErrUnsupported // P4.2 (write)
}

func (a *Adapter) CreateCalledTestStep(ctx context.Context, key, calledTestKey, calledTestID string) (string, error) {
	return "", backend.ErrUnsupported // P4.2 (write)
}

// --- custom fields ---

func (a *Adapter) ListCustomFields(ctx context.Context, projectKey string) ([]backend.CustomFieldDef, error) {
	return nil, nil // P4.2 — EMPTY: Kiwi core has no per-issue custom-field registry (spec §3.4)
}

func (a *Adapter) GetTestCustomFields(ctx context.Context, testKey string) (map[string]string, error) {
	return nil, nil // P4.2 — EMPTY (spec §3.4)
}

func (a *Adapter) CustomFieldValue(ctx context.Context, fieldID, value string) (string, any, error) {
	return "", nil, backend.ErrUnsupported // P4.2 — spec §3.4: UNSUP (local concept, no Kiwi analog)
}

func (a *Adapter) ExecTypeFieldValue(ctx context.Context, execType string) (fieldID string, value any, ok bool, err error) {
	return "", nil, false, nil // P4.2 — ExecType derives from is_automated, not a field (spec §3.4)
}

// --- containers ---

// ListContainers pulls TestPlans and TestRuns for the product (spec §3.5).
// Each plan/run is fetched once, then its membership is fetched with a
// second per-container call (TestCase.filter({"plan":id}) /
// TestExecution.filter({"run":id})) exactly as spec §3.5's table
// describes it — Kiwi's RPC surface has no single call that returns
// container + membership together. There is no KindTestSet: Kiwi has no
// Test Set concept (spec §3.5, §4.1).
func (a *Adapter) ListContainers(ctx context.Context, projectKey string, onProgress func(done, total int)) ([]backend.Container, []backend.ContainerLink, error) {
	planFilter := map[string]any{}
	runFilter := map[string]any{}
	if projectKey != "" {
		planFilter["product__name"] = projectKey
		runFilter["plan__product__name"] = projectKey
	}

	var plans []kiwiTestPlan
	if err := a.c.call(ctx, "TestPlan.filter", []any{planFilter}, &plans); err != nil {
		return nil, nil, err
	}
	var runs []kiwiTestRun
	if err := a.c.call(ctx, "TestRun.filter", []any{runFilter}, &runs); err != nil {
		return nil, nil, err
	}

	total := len(plans) + len(runs)
	done := 0
	report := func() {
		done++
		if onProgress != nil {
			onProgress(done, total)
		}
	}

	containers := make([]backend.Container, 0, total)
	links := make([]backend.ContainerLink, 0)

	for _, p := range plans {
		containers = append(containers, toContainerFromPlan(p))
		cases, err := a.fetchTestCases(ctx, map[string]any{"plan": p.ID})
		if err != nil {
			return nil, nil, err
		}
		planKey := strconv.Itoa(p.ID)
		for _, c := range cases {
			links = append(links, backend.ContainerLink{ContainerKey: planKey, TestKey: strconv.Itoa(c.ID)})
		}
		report()
	}

	for _, r := range runs {
		containers = append(containers, toContainerFromRun(r))
		var execs []kiwiTestExecution
		if err := a.c.call(ctx, "TestExecution.filter", []any{map[string]any{"run": r.ID}}, &execs); err != nil {
			return nil, nil, err
		}
		for _, e := range execs {
			links = append(links, toExecContainerLink(e))
		}
		report()
	}

	return containers, links, nil
}

// TestExecutionsForTest finds the executions referencing a case and returns
// their parent runs as KindTestExec containers, plus the per-execution
// membership links (spec §3.5).
func (a *Adapter) TestExecutionsForTest(ctx context.Context, testKey string) ([]backend.Container, []backend.ContainerLink, error) {
	id, err := parseKiwiID(testKey)
	if err != nil {
		return nil, nil, err
	}
	var execs []kiwiTestExecution
	if err := a.c.call(ctx, "TestExecution.filter", []any{map[string]any{"case": id}}, &execs); err != nil {
		return nil, nil, err
	}
	if len(execs) == 0 {
		return []backend.Container{}, []backend.ContainerLink{}, nil
	}

	seen := map[int]bool{}
	runIDs := make([]int, 0, len(execs))
	for _, e := range execs {
		if !seen[e.Run] {
			seen[e.Run] = true
			runIDs = append(runIDs, e.Run)
		}
	}
	var runs []kiwiTestRun
	if err := a.c.call(ctx, "TestRun.filter", []any{map[string]any{"pk__in": runIDs}}, &runs); err != nil {
		return nil, nil, err
	}

	containers := make([]backend.Container, len(runs))
	for i, r := range runs {
		containers[i] = toContainerFromRun(r)
	}
	links := make([]backend.ContainerLink, len(execs))
	for i, e := range execs {
		links[i] = toExecContainerLink(e)
	}
	return containers, links, nil
}

func (a *Adapter) CreateContainer(ctx context.Context, projectKey, kind, summary string) (string, error) {
	return "", backend.ErrUnsupported // P4.2 (write)
}

func (a *Adapter) AddTestsToContainer(ctx context.Context, kind, containerKey string, testKeys []string) error {
	return backend.ErrUnsupported // P4.2 (write)
}

func (a *Adapter) RemoveTestsFromContainer(ctx context.Context, kind, containerKey string, testKeys []string) error {
	return backend.ErrUnsupported // P4.2 (write)
}

func (a *Adapter) SetTestRunStatus(ctx context.Context, execKey, testKey, status string) error {
	return backend.ErrUnsupported // P4.2 (write)
}

func (a *Adapter) SetContainerEnvironments(ctx context.Context, execKey string, envs []string) error {
	return backend.ErrUnsupported // P4.2 (write)
}

func (a *Adapter) DeleteContainer(ctx context.Context, kind, containerKey string) error {
	return backend.ErrUnsupported // P4.2 (write)
}

// GetTestRuns returns the per-case execution rows of a TestRun (spec §3.7).
// "execKey" is the Kiwi TestRun id, matching how the interface's Xray
// implementation treats a Test Execution issue key.
func (a *Adapter) GetTestRuns(ctx context.Context, execKey string) ([]backend.TestRun, error) {
	id, err := parseKiwiID(execKey)
	if err != nil {
		return nil, err
	}
	var execs []kiwiTestExecution
	if err := a.c.call(ctx, "TestExecution.filter", []any{map[string]any{"run": id}}, &execs); err != nil {
		return nil, err
	}
	out := make([]backend.TestRun, len(execs))
	for i, e := range execs {
		out[i] = toTestRunDTO(e)
	}
	return out, nil
}

// ExecPlans returns the plan(s) a run belongs to. A Kiwi TestRun belongs to
// exactly one TestPlan (spec §3.5), so this returns a single-element slice
// (or empty if the run id doesn't resolve).
func (a *Adapter) ExecPlans(ctx context.Context, execKey string) ([]string, error) {
	id, err := parseKiwiID(execKey)
	if err != nil {
		return nil, err
	}
	var runs []kiwiTestRun
	if err := a.c.call(ctx, "TestRun.filter", []any{map[string]any{"pk": id}}, &runs); err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return []string{}, nil
	}
	return []string{strconv.Itoa(runs[0].Plan)}, nil
}

// --- preconditions ---

func (a *Adapter) ListPreconditions(ctx context.Context, projectKey string, onProgress func(done, total int)) ([]backend.Precondition, map[string][]string, error) {
	return nil, nil, nil // P4.2 — EMPTY: Kiwi core has no precondition object (spec §3.6)
}

func (a *Adapter) CreatePrecondition(ctx context.Context, projectKey, summary, ptype, description string) (string, error) {
	return "", backend.ErrUnsupported // P4.2 (write)
}

func (a *Adapter) UpdateTestPreconditions(ctx context.Context, testKey string, add, remove []string) error {
	return backend.ErrUnsupported // P4.2 (write)
}

func (a *Adapter) DeletePrecondition(ctx context.Context, preconditionKey string) error {
	return backend.ErrUnsupported // P4.2 (write)
}

// --- requirements ---

// ListRequirements is the requirements-plugin read path (spec §3.8, §8.1;
// P4.3 brief deliverable (c)). When the plugin was NOT detected by
// TestConnection (a.hasRequirementsPlugin is false — either confirmed
// absent, or detection hasn't run yet), it returns empty+nil per spec §3.8's
// EMPTY class for base Kiwi: base Kiwi's TestCase.requirement is just a
// ≤255-char label field, not traceability, so there is nothing to read and
// this is NOT an error condition.
//
// When present, it calls the plugin's two RPC methods (the ONLY two the
// plugin exposes — spec §4.3/§8.1):
//   - Requirement.filter({}) once, for the full requirement registry
//     (spec §3.8's ListRequirements table entry cites this exact call+query
//     for pulling every Requirement row; the plugin's small response shape
//     has no product field to scope by — OQ-4 — so profileProjectKey/
//     sources cannot narrow this call and are accepted but unused, matching
//     how the xray-specific ScopeSpec fields are still under the shared
//     interface signature per spec §2).
//   - Requirement.coverage(id) once per requirement returned above, to
//     recover individual test<->requirement links (see requirements.go's
//     kiwiRequirementCoverage doc comment for the FLAGGED, unconfirmed
//     link_types shape this relies on, and how it degrades safely if wrong).
//
// onProgress reports per-requirement completion of the coverage sweep,
// mirroring ListContainers' progress-reporting shape elsewhere in this
// adapter.
func (a *Adapter) ListRequirements(ctx context.Context, profileProjectKey string, sources []backend.RequirementSourceSpec, onProgress func(done, total int)) ([]backend.Requirement, []backend.RequirementLink, error) {
	if !a.hasRequirementsPlugin {
		return nil, nil, nil
	}

	var rows []kiwiRequirement
	if err := a.c.call(ctx, "Requirement.filter", []any{map[string]any{}}, &rows); err != nil {
		return nil, nil, err
	}

	reqs := make([]backend.Requirement, len(rows))
	for i, r := range rows {
		reqs[i] = toRequirement(r)
	}

	total := len(rows)
	done := 0
	links := make([]backend.RequirementLink, 0)
	for _, r := range rows {
		var cov kiwiRequirementCoverage
		// Requirement.coverage(requirement_id) takes a single positional
		// scalar arg (spec §4.3/§8.1's literal call form), not a query
		// dict — so params is a one-element array holding the bare id,
		// unlike the *.filter calls elsewhere in this package.
		if err := a.c.call(ctx, "Requirement.coverage", []any{r.ID}, &cov); err != nil {
			return nil, nil, err
		}
		links = append(links, toRequirementLinks(cov, strconv.Itoa(r.ID))...)

		done++
		if onProgress != nil {
			onProgress(done, total)
		}
	}

	return reqs, links, nil
}

func (a *Adapter) UpdateTestRequirements(ctx context.Context, testKey string, add []string, removeLinkIDs []string) error {
	return backend.ErrUnsupported // Phase 5 (write)
}

func (a *Adapter) ListIssueLinkTypes(ctx context.Context) ([]string, error) {
	return nil, backend.ErrUnsupported // out of this task's scope: the P4.3 brief's deliverables don't call for wiring this (see Capabilities' SupportsIssueLinkTypes note); left as the P4.1 stub
}

func (a *Adapter) CreateRequirement(ctx context.Context, projectKey, issueType, summary, description, priority, components, fixVersions string) (string, error) {
	return "", backend.ErrUnsupported // Phase 5 (write)
}

func (a *Adapter) DeleteRequirement(ctx context.Context, requirementKey string) error {
	return backend.ErrUnsupported // Phase 5 (write)
}

func (a *Adapter) UpdateRequirementLinks(ctx context.Context, fromKey string, add []string, removeLinkIDs []string) error {
	return backend.ErrUnsupported // Phase 5 (write)
}

func (a *Adapter) ListReqToReqLinks(ctx context.Context, reqKeys []string) ([]backend.ReqToReqLink, error) {
	return nil, nil // EMPTY: no req->req RPC in the plugin today (spec §3.8, OQ-3); brief deliverable (4)
}

// --- bugs ---

func (a *Adapter) ListBugs(ctx context.Context, testProjectKey string, testKeys []string, issueType string, onProgress func(done, total int)) ([]backend.Bug, []backend.BugLink, error) {
	return nil, nil, backend.ErrUnsupported // P4.3 — best-effort via TestExecution.get_links (spec §3.9)
}

func (a *Adapter) ListProjectBugs(ctx context.Context, projKey, issueType string) ([]backend.Bug, error) {
	return nil, nil // P4.3 — EMPTY (spec §3.9)
}

func (a *Adapter) GetBugCreateFields(ctx context.Context, projectKey, issueType string) ([]backend.BugCreateField, error) {
	return nil, backend.ErrUnsupported // P4.3 (write)
}

func (a *Adapter) CreateBug(ctx context.Context, projectKey, issueType, summary, description, priority string, labels []string, extraFields map[string]any) (string, error) {
	return "", backend.ErrUnsupported // P4.3 (write)
}

func (a *Adapter) CreateBugLink(ctx context.Context, testKey, bugKey string) error {
	return backend.ErrUnsupported // P4.3 (write)
}

func (a *Adapter) GetBugDetail(ctx context.Context, bugKey string) (backend.BugDetail, error) {
	return backend.BugDetail{}, backend.ErrUnsupported // P4.3 — best-effort (spec §3.9)
}

// --- folders ---

func (a *Adapter) FolderTree(ctx context.Context, projectKey string) (backend.FolderTreeResult, error) {
	return backend.FolderTreeResult{}, nil // P4.2 — EMPTY: no folder tree in core (spec §3.10)
}

func (a *Adapter) ListFolders(ctx context.Context, projectKey string) ([]backend.Folder, error) {
	return nil, nil // P4.2 — EMPTY (spec §3.10)
}

func (a *Adapter) ListTestsInFolder(ctx context.Context, projectKey, folderID string) ([]string, error) {
	return nil, nil // P4.2 — EMPTY (spec §3.10)
}

func (a *Adapter) CreateFolder(ctx context.Context, projectKey, parentPath, name string) error {
	return backend.ErrUnsupported // P4.2 (write)
}

func (a *Adapter) RenameFolder(ctx context.Context, projectKey, path, newName string) error {
	return backend.ErrUnsupported // P4.2 (write)
}

func (a *Adapter) DeleteFolder(ctx context.Context, projectKey, path string) error {
	return backend.ErrUnsupported // P4.2 (write)
}

func (a *Adapter) MoveTestToFolder(ctx context.Context, projectKey, testKey, folderID string) error {
	return backend.ErrUnsupported // P4.2 (write)
}

// --- workflow ---

func (a *Adapter) GetTransitions(ctx context.Context, key, currentStatus string) ([]backend.Transition, error) {
	return nil, nil // P4.2 — EMPTY: status is settable directly, not transitioned (spec §3.11)
}

func (a *Adapter) PostTransition(ctx context.Context, key, transitionID string) error {
	return backend.ErrUnsupported // P4.2 — use TestCase.update({"case_status":id}) in the write phase (spec §3.11)
}

// --- metadata ---

// ListStatuses returns every TestCaseStatus name. Kiwi statuses are a
// global enum, not per-project, so projectKey is unused (spec §3.12's table
// filters TestCaseStatus.filter({}) — no product narrowing).
func (a *Adapter) ListStatuses(ctx context.Context, projectKey string) ([]string, error) {
	var rows []kiwiName
	if err := a.c.call(ctx, "TestCaseStatus.filter", []any{map[string]any{}}, &rows); err != nil {
		return nil, err
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Name
	}
	return out, nil
}

// ListPriorities returns every active Priority value. Global, not
// per-project (spec §3.12: Priority.filter({"is_active":true})).
func (a *Adapter) ListPriorities(ctx context.Context, projectKey string) ([]string, error) {
	var rows []kiwiValue
	if err := a.c.call(ctx, "Priority.filter", []any{map[string]any{"is_active": true}}, &rows); err != nil {
		return nil, err
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Value
	}
	return out, nil
}

// ProjectComponents returns component names scoped to the product (spec
// §3.12: Component.filter({"product__name":P})).
func (a *Adapter) ProjectComponents(ctx context.Context, projectKey string) ([]string, error) {
	filter := map[string]any{}
	if projectKey != "" {
		filter["product__name"] = projectKey
	}
	var rows []kiwiName
	if err := a.c.call(ctx, "Component.filter", []any{filter}, &rows); err != nil {
		return nil, err
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Name
	}
	return out, nil
}

// ProjectVersions returns version values scoped to the product (spec
// §3.12: Version.filter({"product__name":P})).
func (a *Adapter) ProjectVersions(ctx context.Context, projectKey string) ([]string, error) {
	filter := map[string]any{}
	if projectKey != "" {
		filter["product__name"] = projectKey
	}
	var rows []kiwiValue
	if err := a.c.call(ctx, "Version.filter", []any{filter}, &rows); err != nil {
		return nil, err
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Value
	}
	return out, nil
}

// --- comments ---

func (a *Adapter) AddComment(ctx context.Context, issueKey, body string) error {
	return backend.ErrUnsupported // P4.2 (write)
}

// --- field payload shaping ---

func (a *Adapter) FieldsForJira(updates map[string]string) map[string]any {
	return map[string]any{} // P4.2 — translate to a TestCase.update dict (spec §3.13)
}

// --- capabilities ---

// Capabilities reports the base-Kiwi feature set, with the
// requirements-plugin delta applied if TestConnection's detection probe
// found it (spec §4.1, §4.2). SupportsRequirementObjects is the ONLY field
// this task flips per the P4.3 brief's explicit deliverable (b); spec
// §4.2's "SupportsIssueLinkTypes=true" pairing and any SupportsReview field
// are deliberately NOT wired here — see this method's flip below and
// Adapter.hasReviewPlugin's doc comment for why. If Capabilities() is
// called before TestConnection ever ran, hasRequirementsPlugin is still its
// zero value (false), so the caps below are the safe, plugin-off default
// (brief: "base caps when detection hasn't run").
func (a *Adapter) Capabilities() backend.Capabilities {
	caps := backend.Capabilities{
		Name:                        "kiwi",
		IDStyle:                     "numeric", // Kiwi pks are ints (spec §4.1; see p4_1-report.md for the "integer" vs "numeric" note)
		SupportsJQLScope:            false,     // Product/Version/Build + ORM filters, not JQL
		StepModel:                   "inline-text",
		SupportsTestTypes:           true, // is_automated -> Manual/Automated
		SupportsFolders:             false,
		SupportsPreconditionObjects: false,
		SupportsRequirementObjects:  false, // flipped below if the requirements plugin was detected
		SupportsIssueLinkTypes:      false, // NOT flipped in this task: brief's deliverable (b) names only SupportsRequirementObjects; ListIssueLinkTypes stays an ErrUnsupported stub (see adapter.go's requirements section)
		SupportsEnvironments:        true,  // Build ~= environment
		SupportsContainers:          true,
		ContainerKinds:              []string{backend.KindTestPlan, backend.KindTestExec}, // no KindTestSet in Kiwi
		SupportsTestRuns:            true,
		StatusModel:                 "settable", // no workflow graph
		SupportsWorkflowTransitions: false,
		SupportsBugCreation:         false, // executions-as-links, not Jira-style issues
		SupportsBugLinks:            true,  // TestExecution hyperlinks
		SupportsTags:                true,  // Tag m2m on TestCase
	}
	if a.hasRequirementsPlugin {
		caps.SupportsRequirementObjects = true
	}
	return caps
}
