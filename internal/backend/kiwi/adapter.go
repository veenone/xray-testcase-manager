package kiwi

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"xray-test-manager/internal/backend"
)

// Adapter implements backend.Backend against a Kiwi TCMS instance's
// JSON-RPC API. P4.1 built TestConnection, Capabilities, IsDemo,
// SetRequirementLinkType, RemoteAhead, and the transport/auth/error-typing/
// plugin-detection-probe mechanism they sit on. P4.2 added the core read
// mapping: the test pull (SearchTestsPage/GetTestFields/GetTestSteps/
// ListTestsBasic/GetTestMeta), containers + runs (ListContainers/
// TestExecutionsForTest/GetTestRuns/ExecPlans), metadata (ListStatuses/
// ListPriorities/ProjectComponents/ProjectVersions), and the history_date
// RemoteVersion. P4.3 wired the plugin-detection probe into
// TestConnection/Capabilities (hasRequirementsPlugin/hasReviewPlugin) and
// implemented the requirements-plugin read path (ListRequirements, via
// Requirement.filter + Requirement.coverage — see requirements.go). P4.5
// (this task) made that detection LAZY: app.go's backend factory builds a
// fresh Adapter per call, and the sync path never calls TestConnection, so
// caching the probe result only inside TestConnection left every sync-time
// read stuck with hasRequirementsPlugin at its false zero-value. See
// ensureDetected below — it is now the single place detection runs, called
// from TestConnection AND every plugin-gated ctx read method. Review plugin
// integration beyond caching hasReviewPlugin is explicitly DEFERRED (needs a
// backend.Backend interface extension — see hasReviewPlugin's doc comment).
// Bugs, req->req links, and every remaining WRITE method stay stubs (Phase
// 5), and the genuinely-no-analog EMPTY reads (preconditions, folders,
// transitions, custom fields) stay zero-value per §3 of
// p4_0-kiwi-integration-spec.md.
type Adapter struct {
	c *Client

	// hasRequirementsPlugin is set by ensureDetected's plugin-detection probe
	// (spec §4.3, P4.3 brief item (a); made lazy by P4.5 — see ensureDetected
	// below): true when the kiwi-tcms-requirements plugin's Requirement.filter
	// RPC is registered on the server. Capabilities() reads it to flip
	// SupportsRequirementObjects, and ListRequirements reads it to decide
	// between the real plugin-backed read and the EMPTY (base-Kiwi) return.
	// Zero-value (false) is the safe default before detection has run — same
	// "off until proven present" behavior as a confirmed-absent probe.
	hasRequirementsPlugin bool

	// hasReviewPlugin is set by the same ensureDetected probe, against the
	// kiwi-tcms-review-workflow plugin's ReviewRequest.filter RPC. It is
	// cached here and DELIBERATELY UNEXPOSED in this task: per the P4.3
	// brief, full review-plugin integration (a new backend.Capabilities
	// SupportsReview field, a review-read interface method, and the
	// XTM-verdict mapping) is an interface extension deferred to a future
	// task. This flag exists purely so that future task doesn't have to
	// redo detection.
	hasReviewPlugin bool

	// pageMu/pageCache hold one sync's worth of TestCase rows. Kiwi's
	// TestCase.filter has no server-side limit or offset — verified against a
	// live instance, which answers "Cannot resolve keyword 'limit'" because the
	// RPC only accepts model field lookups — so a page has to be sliced from
	// the full scoped result. Without this cache every page refetched the whole
	// product: on a real 18,583-test product that was 186 full fetches, about
	// eight minutes, to store 18,583 rows once.
	//
	// The cache is keyed by scope and reset when the caller asks for offset 0,
	// which is how the sync engine starts every pull (engine.go's pullTests
	// walks offsets from 0). That keeps it to one pull's lifetime: a later sync
	// starts at 0 and refetches, so an edited or new test is never missed.
	pageMu    sync.Mutex
	pageKey   string
	pageCache []kiwiTestCase

	// detectMu/detectDone guard ensureDetected (P4.5): detectMu serializes
	// concurrent callers so two goroutines racing into ensureDetected on the
	// same Adapter never double-probe or observe a half-written pair of
	// flags, and detectDone latches once a probe round produced a CONFIRMED
	// result for both plugins (see detectPlugin's confirmed return value in
	// caps.go) so every later call is a no-op. This is deliberately NOT a
	// bare sync.Once: a raw transport failure during a probe is
	// unconfirmed — it carries no signal about whether the plugin is
	// actually there — so latching it as "done" forever would permanently
	// poison detection from one flaky request. See ensureDetected's doc
	// comment for the full retry rule.
	detectMu   sync.Mutex
	detectDone bool
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
// Once the core connection succeeds, TestConnection also runs plugin
// detection via ensureDetected (spec §4.3; made lazy and idempotent by
// P4.5 — see ensureDetected's doc comment). Routing through ensureDetected
// here rather than probing directly keeps this the SAME code path
// ListRequirements/ListIssueLinkTypes use when they self-detect during a
// sync that never called TestConnection at all. ensureDetected never
// returns a hard error (a probe failure degrades to "flag off, retry
// later"), so it never fails TestConnection as a whole — see
// detectPlugin's doc comment in caps.go for exactly how each outcome
// (absent / installed-but-degraded / unknown transport failure) is
// classified.
func (a *Adapter) TestConnection(ctx context.Context) (*backend.User, error) {
	if err := a.c.Login(ctx); err != nil {
		return nil, err
	}
	var users []kiwiUser
	if err := a.c.call(ctx, "User.filter", []any{map[string]any{"is_active": true}}, &users); err != nil {
		return nil, err
	}

	_ = a.ensureDetected(ctx) // never returns a hard error; see doc comment

	if len(users) == 0 {
		return &backend.User{}, nil
	}
	return toUser(users[0]), nil
}

// ensureDetected runs the plugin-detection probes (detectPlugin, caps.go)
// exactly once per Adapter — lazily, on whichever call needs the result
// first — and is safe to call repeatedly and concurrently. P4.5: this
// closes the gap where TestConnection was the ONLY place detection ran,
// but app.go's backend factory builds a FRESH Adapter per call and the
// sync path never calls TestConnection, so a Kiwi profile's sync-time
// ListRequirements always saw hasRequirementsPlugin at its unset false
// zero-value and silently returned zero requirements even when the
// requirements plugin was actually installed.
//
// Guard: detectMu + detectDone rather than a bare sync.Once, because a
// probe outcome can be UNCONFIRMED (a raw transport failure carries no
// signal either way — see detectPlugin's confirmed return). Only a
// CONFIRMED round (both the requirements and review probes resolved to
// registered / confirmed-absent / installed-but-degraded) latches
// detectDone=true; an unconfirmed round leaves it false so the very next
// ensureDetected call retries the probes instead of caching a false
// negative forever from one flaky request. Concurrent callers serialize on
// detectMu, so two goroutines racing into ensureDetected on the same
// Adapter never double-probe or observe a half-written pair of flags.
//
// ensureDetected itself never returns a non-nil error today — a probe
// failure always degrades to "flags off, retry later", exactly as
// TestConnection's original P4.3 behavior did. The error return exists so
// callers have one obviously-fallible call shape without a second
// interface surface, reserved for a future case where a caller needs
// detection to fail loudly instead of degrading.
func (a *Adapter) ensureDetected(ctx context.Context) error {
	a.detectMu.Lock()
	defer a.detectMu.Unlock()
	if a.detectDone {
		return nil
	}

	reqPresent, reqConfirmed := detectPlugin(ctx, a.c, requirementsProbeMethod)
	revPresent, revConfirmed := detectPlugin(ctx, a.c, reviewProbeMethod)
	a.hasRequirementsPlugin = reqPresent
	a.hasReviewPlugin = revPresent

	if reqConfirmed && revConfirmed {
		a.detectDone = true
	}
	return nil
}

// IsDemo reports whether this adapter targets the deterministic offline
// kiwi-demo generator (P4.4, demo.go) — true whenever the Client was built
// against a "kiwi-demo" URL.
func (a *Adapter) IsDemo() bool { return a.c.demo != nil }

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

// scopedCases returns every TestCase in a product, sorted by id, fetching from
// the server only when the scope changes or a new pull begins.
//
// startAt == 0 means the caller is at the top of a fresh pull, so the cache is
// discarded and the scope re-fetched. Every later offset in that same pull is
// served from memory. See the pageCache field comment for why paging cannot be
// pushed to the server.
func (a *Adapter) scopedCases(ctx context.Context, projectKey string, startAt int) ([]kiwiTestCase, error) {
	a.pageMu.Lock()
	defer a.pageMu.Unlock()

	if startAt > 0 && a.pageKey == projectKey && a.pageCache != nil {
		return a.pageCache, nil
	}

	filter := map[string]any{}
	if projectKey != "" {
		filter["category__product__name"] = projectKey
	}
	rows, err := a.fetchTestCases(ctx, filter)
	if err != nil {
		return nil, err
	}
	// Sorted once, here, so every page slices the same stable order. Kiwi
	// rejects order_by, so this cannot be asked of the server either.
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })

	a.pageKey, a.pageCache = projectKey, rows
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

// fetchTagsForCases and fetchComponentsForCases are the P4.6 fix for item 1
// (tags/components are NOT on the TestCase row — live-verified, see
// kiwiTestCase's doc comment): they fetch names for a whole PAGE of case ids
// in ONE call each, via Kiwi's Django `__in` lookup
// (Tag.filter({"case__in":ids}) / Component.filter({"cases__in":ids})),
// grouping the flat result by case id. SearchTestsPage calls these ONCE per
// page — never once per case — so a bulk pull stays O(1) extra RPCs per
// page instead of O(n).

func (a *Adapter) fetchTagsForCases(ctx context.Context, ids []int) (map[int][]string, error) {
	if len(ids) == 0 {
		return map[int][]string{}, nil
	}
	var rows []kiwiTagRow
	if err := a.c.call(ctx, "Tag.filter", []any{map[string]any{"case__in": ids}}, &rows); err != nil {
		return nil, err
	}
	return tagNamesByCase(rows), nil
}

func (a *Adapter) fetchComponentsForCases(ctx context.Context, ids []int) (map[int][]string, error) {
	if len(ids) == 0 {
		return map[int][]string{}, nil
	}
	var rows []kiwiComponentRow
	if err := a.c.call(ctx, "Component.filter", []any{map[string]any{"cases__in": ids}}, &rows); err != nil {
		return nil, err
	}
	return componentNamesByCase(rows), nil
}

// fetchTagsForCase and fetchComponentsForCase are the single-case forms
// used by GetTestFields (spec fix item 1: "GetTestFields (single case) may
// use the non-batched {"case": id} / {"cases": id} form" — one RPC per call
// is fine here since GetTestFields already fetches exactly one TestCase).

func (a *Adapter) fetchTagsForCase(ctx context.Context, id int) ([]string, error) {
	var rows []kiwiTagRow
	if err := a.c.call(ctx, "Tag.filter", []any{map[string]any{"case": id}}, &rows); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.Name)
	}
	return names, nil
}

func (a *Adapter) fetchComponentsForCase(ctx context.Context, id int) ([]string, error) {
	var rows []kiwiComponentRow
	if err := a.c.call(ctx, "Component.filter", []any{map[string]any{"cases": id}}, &rows); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.Name)
	}
	return names, nil
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
	rows, err := a.scopedCases(ctx, projectKey, startAt)
	if err != nil {
		return nil, 0, err
	}

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
	// Tags/components are not on the TestCase row (live-verified, P4.6): fetch
	// them for the whole page in one call each, then attach by case id.
	ids := make([]int, len(page))
	for i, tc := range page {
		ids[i] = tc.ID
	}
	tagsByCase, err := a.fetchTagsForCases(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	compsByCase, err := a.fetchComponentsForCases(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	out := make([]backend.Test, len(page))
	for i, tc := range page {
		out[i] = toTest(tc, tagsByCase[tc.ID], compsByCase[tc.ID])
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

// SearchTestsAcrossProjects / SearchPreconditionsAcrossProjects have no Kiwi
// analog (Kiwi has no cross-Product issue-key search and no separate
// precondition issues), so they return no candidates. Cross-project linking
// (RND_P_4TFINT_05-322) is an Xray-only capability today.
func (a *Adapter) SearchTestsAcrossProjects(ctx context.Context, projectKeys []string, query string, offset, limit int) ([]backend.TestBasic, int, error) {
	return []backend.TestBasic{}, 0, nil
}

func (a *Adapter) SearchPreconditionsAcrossProjects(ctx context.Context, projectKeys []string, query string, offset, limit int) ([]backend.Precondition, int, error) {
	return []backend.Precondition{}, 0, nil
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
	tags, err := a.fetchTagsForCase(ctx, id)
	if err != nil {
		return backend.Test{}, err
	}
	comps, err := a.fetchComponentsForCase(ctx, id)
	if err != nil {
		return backend.Test{}, err
	}
	return toTest(tc, tags, comps), nil
}

// CreateTest, UpdateIssue are implemented in write.go (P5.1 — the Kiwi
// TestCase write surface).

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

// RemoteVersion returns the TestCase's history_date as its version token —
// the SAME token the pull path stores as base_version (toTest maps
// history_date to Test.Updated, which testrepo persists as updated_at and
// captures into each pending change's base_version). Keeping RemoteVersion in
// that identical token space is what makes conflict detection correct: an
// UNCHANGED remote yields base == remote (RemoteAhead false, no spurious
// conflict), and any save advances history_date (django-simple-history's
// per-object last-modified stamp, live-verified P4.6) so a genuinely changed
// remote yields base != remote (RemoteAhead true). This mirrors Xray, where
// the pull `updated` and RemoteVersion are the identical Jira `updated`
// timestamp. An earlier implementation hashed the row's content instead,
// which lived in a DIFFERENT token space than base_version (a hash vs a
// timestamp) and so tripped conflict detection on every commit.
//
// entityType is accepted but unused: every caller in this codebase passes
// "test" (internal/syncer/commit.go), and Kiwi has no other entity kind with
// pending-change concurrency tracking in P4.2 (preconditions/containers are
// read-only here).
func (a *Adapter) RemoteVersion(ctx context.Context, entityType, externalKey string) (backend.VersionToken, error) {
	id, err := parseKiwiID(externalKey)
	if err != nil {
		return "", err
	}
	tc, err := a.fetchTestCaseByID(ctx, id)
	if err != nil {
		return "", err
	}
	return backend.VersionToken(tc.HistoryDate), nil
}

// RemoteAhead treats the history_date tokens as opaque and compares them for
// inequality, not order ("ahead" means "different"): history_date is a
// monotonically-advancing save stamp, so any string difference means the
// remote moved since base was pulled. Both an empty base AND an empty remote
// are treated conservatively as "not ahead": an empty token means "no version
// info yet" (RemoteVersion not wired, or the entity was never read), and
// neither side should manufacture a spurious ahead/conflict signal from a
// missing value. (Spec's literal formula `base != "" && base != remote` would
// also report "ahead" when remote=="" and base!="" ; we additionally guard
// remote=="" for the same reason we guard base=="" — documented deviation,
// not an invented rule.)
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

// CreateTestStep is P5's write counterpart to GetTestSteps/flattenSteps:
// Kiwi has no step objects, only the single `text` field a TestCase carries
// (spec §3.3, §7), so "create a step" means APPENDING the new step's content
// onto that field rather than inserting a row anywhere. The B5 publish
// engine's StepMode=="flatten" path already joins a whole test's steps into
// one `action` string before calling this, but this stays robust to a caller
// that populates data/expected too: the appended block is assembled from
// whichever of action/data/expected are non-empty (data prefixed "Data: ",
// expected prefixed "Expected: "), newline-joined. That block is appended to
// the TestCase's existing text — separated by a blank line when the existing
// text is non-empty, otherwise used as the whole text — so repeat calls
// accumulate instead of clobbering prior content. The write reuses
// UpdateIssue's exact TestCase.update({"text": ...}) call shape (write.go)
// rather than re-implementing the RPC.
//
// Kiwi steps have no independent identity (there is no per-step id to
// return), so this always returns the fixed id "1" — the same id
// flattenSteps assigns the single neutral Step it produces when reading the
// text back, so a caller that immediately re-reads sees the step it just
// wrote.
func (a *Adapter) CreateTestStep(ctx context.Context, key, action, data, expected string) (string, error) {
	id, err := parseKiwiID(key)
	if err != nil {
		return "", err
	}
	tc, err := a.fetchTestCaseByID(ctx, id)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(action)
	if data != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("Data: " + data)
	}
	if expected != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("Expected: " + expected)
	}
	block := b.String()

	newText := block
	if tc.Text != "" {
		newText = tc.Text + "\n\n" + block
	}

	if err := a.c.call(ctx, "TestCase.update", []any{id, map[string]any{"text": newText}}, nil); err != nil {
		return "", err
	}
	return "1", nil
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

// Cucumber/Generic body fields have no Kiwi analog (#54): ok=false so the
// commit engine skips them.
func (a *Adapter) CucumberScenarioFieldValue(ctx context.Context, v string) (string, any, bool, error) {
	return "", nil, false, nil
}

func (a *Adapter) CucumberTypeFieldValue(ctx context.Context, v string) (string, any, bool, error) {
	return "", nil, false, nil
}

func (a *Adapter) GenericDefinitionFieldValue(ctx context.Context, v string) (string, any, bool, error) {
	return "", nil, false, nil
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

// CreateContainer, AddTestsToContainer, RemoveTestsFromContainer,
// SetTestRunStatus, and DeleteContainer are implemented in
// container_write.go (P5.2 — the Kiwi container/run WRITE surface).

// SetContainerEnvironments stays ErrUnsupported: Kiwi has no Test
// Environments concept on a TestRun/TestExecution (a Build stands in for an
// "environment" elsewhere in this adapter — see Capabilities'
// SupportsEnvironments comment — but there is no separate multi-value
// environment field to write). P5.2 confirmed this is a genuine EMPTY/UNSUP,
// not a deferred implementation.
func (a *Adapter) SetContainerEnvironments(ctx context.Context, execKey string, envs []string) error {
	return backend.ErrUnsupported // P4.2/P5.2 (write) — no Kiwi analog
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
// P4.3 brief deliverable (c)). It calls ensureDetected FIRST (P4.5) so this
// method self-detects the requirements plugin the first time it runs on a
// given Adapter, instead of depending on a prior explicit TestConnection
// call — this is what makes the sync path (which never calls
// TestConnection) actually pull requirements. When the plugin was NOT
// detected (a.hasRequirementsPlugin is false after ensureDetected — either
// confirmed absent, or an unconfirmed probe that left it off pending
// retry), it returns empty+nil per spec §3.8's EMPTY class for base Kiwi:
// base Kiwi's TestCase.requirement is just a ≤255-char label field, not
// traceability, so there is nothing to read and this is NOT an error
// condition.
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
	_ = a.ensureDetected(ctx) // never returns a hard error; see doc comment
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

// ListIssueLinkTypes returns the requirements-plugin's static typed-link
// vocabulary when the plugin was detected, else empty (spec §3.8: "static:
// [\"verifies\",\"validates\",\"derives-from\",\"related\"] (the plugin's
// link_type values, from rpc.py per-case output link_type)"). This is a
// fixed list, not an RPC call — the plugin exposes no "list link types" RPC,
// only these values appearing as the `link_type` field on per-case rows.
// Absent-plugin returns (nil, nil), not an error, mirroring ListRequirements'
// EMPTY behavior.
//
// NOTE: this task only advertises the capability and the read-only type
// list. Carrying the CHOSEN link type per coverage link on WRITE (so an
// edit can say a test "validates" vs "verifies" a requirement) is Phase 5:
// backend.RequirementLink has no LinkType field yet, and dto.go is out of
// scope here.
//
// Like ListRequirements, this calls ensureDetected FIRST (P4.5) so it
// self-detects the plugin on a fresh Adapter rather than depending on a
// prior TestConnection call.
func (a *Adapter) ListIssueLinkTypes(ctx context.Context) ([]string, error) {
	_ = a.ensureDetected(ctx) // never returns a hard error; see doc comment
	if !a.hasRequirementsPlugin {
		return nil, nil
	}
	return []string{"verifies", "validates", "derives-from", "related"}, nil
}

// ListIssueLinkTypeDetails mirrors ListIssueLinkTypes but in the richer
// name+direction shape the config dropdown consumes. Kiwi's requirements
// plugin exposes only a flat vocabulary (no separate inward/outward
// directions), so Inward and Outward echo the Name. Absent-plugin returns
// (nil, nil), matching ListIssueLinkTypes.
func (a *Adapter) ListIssueLinkTypeDetails(ctx context.Context) ([]backend.IssueLinkType, error) {
	names, err := a.ListIssueLinkTypes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]backend.IssueLinkType, len(names))
	for i, n := range names {
		out[i] = backend.IssueLinkType{Name: n, Inward: n, Outward: n}
	}
	return out, nil
}

func (a *Adapter) CreateRequirement(ctx context.Context, projectKey, issueType, summary, description, priority, components, fixVersions string, extraFields map[string]any) (string, error) {
	return "", backend.ErrUnsupported // Phase 5 (write)
}

func (a *Adapter) GetRequirementCreateFields(ctx context.Context, projectKey, issueType string) ([]backend.BugCreateField, error) {
	return nil, backend.ErrUnsupported // Phase 5 (write); Kiwi has no requirement create screen
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

// FieldsForJira is implemented in write.go (P5.1).

// --- capabilities ---

// Capabilities reports the base-Kiwi feature set, with the
// requirements-plugin delta applied if detection already found it (spec
// §4.1, §4.2). When the requirements plugin is present, BOTH
// SupportsRequirementObjects AND SupportsIssueLinkTypes flip true — the
// full spec §4.2 delta (typed test<->requirement links
// verifies/validates/derives-from/related, served by ListIssueLinkTypes).
// A SupportsReview field is deliberately NOT wired here — see
// Adapter.hasReviewPlugin's doc comment for the deferred review scope.
//
// CAVEAT (P4.5, documented follow-up, deliberately NOT fixed by this
// task): Capabilities() has no ctx, so unlike ListRequirements/
// ListIssueLinkTypes/TestConnection it cannot call ensureDetected itself —
// it only ever reads whatever hasRequirementsPlugin already holds. If
// Capabilities() is called on an Adapter that has NOT yet had a ctx call
// (TestConnection or a plugin-gated read) run on it, hasRequirementsPlugin
// is still its zero value (false), so the caps below are the safe,
// plugin-off default (brief: "base caps when detection hasn't run") — this
// is CORRECT here, but it means app.go's GetCapabilities (called on a
// freshly-built, never-yet-used Adapter per newBackend's stateless
// pattern) will always report plugin capabilities off to the frontend, even
// against a Kiwi server that has the requirements plugin installed.
// Surfacing plugin capabilities through that frontend-facing path is a
// separate cross-cutting change (giving Capabilities()/GetCapabilities a
// ctx, or having app.go run a detection call before reading capabilities)
// left for a future capability-gating task; this task's job was only the
// DATA pull (ListRequirements actually returning rows during a sync), which
// ensureDetected now delivers regardless of what Capabilities() reports.
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
		SupportsIssueLinkTypes:      false, // flipped below if the requirements plugin was detected (typed links verifies/validates/derives-from/related, spec §3.8/§4.2)
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
		caps.SupportsIssueLinkTypes = true // typed test<->requirement links (spec §3.8/§4.2); ListIssueLinkTypes serves the static vocabulary
	}
	return caps
}
