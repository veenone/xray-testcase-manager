package kiwi

import (
	"context"

	"xray-test-manager/internal/backend"
)

// Adapter implements backend.Backend against a Kiwi TCMS instance's
// JSON-RPC API. In THIS task (P4.1) only the following are real:
// TestConnection, Capabilities, IsDemo, SetRequirementLinkType, RemoteAhead,
// and the transport/auth/error-typing/plugin-detection-probe they sit on.
// Every other method is a stub returning backend.ErrUnsupported (or an
// EMPTY zero-value read where the spec already decided that — see §3 of
// p4_0-kiwi-integration-spec.md) with a // P4.2 or // P4.3 marker for the
// task that fills it in.
type Adapter struct {
	c *Client
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
func (a *Adapter) TestConnection(ctx context.Context) (*backend.User, error) {
	if err := a.c.Login(ctx); err != nil {
		return nil, err
	}
	var users []kiwiUser
	if err := a.c.call(ctx, "User.filter", []any{map[string]any{"is_active": true}}, &users); err != nil {
		return nil, err
	}
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

func (a *Adapter) SearchTestsPage(ctx context.Context, projectKey, scopeJQL, since string, startAt, maxResults int) ([]backend.Test, int, error) {
	return nil, 0, backend.ErrUnsupported // P4.2
}

func (a *Adapter) ListTestsBasic(ctx context.Context, keys []string) ([]backend.TestBasic, error) {
	return nil, backend.ErrUnsupported // P4.2
}

func (a *Adapter) GetTestFields(ctx context.Context, key string) (backend.Test, error) {
	return backend.Test{}, backend.ErrUnsupported // P4.2
}

func (a *Adapter) CreateTest(ctx context.Context, projectKey, summary, description, priority string, labels, components []string) (string, error) {
	return "", backend.ErrUnsupported // P4.2 (write, out of P4 read scope)
}

func (a *Adapter) UpdateIssue(ctx context.Context, key string, fields map[string]any) error {
	return backend.ErrUnsupported // P4.2 (write)
}

func (a *Adapter) GetTestMeta(ctx context.Context, key string) (backend.TestMeta, error) {
	return backend.TestMeta{}, backend.ErrUnsupported // P4.2
}

// --- concurrency ---

// RemoteVersion is stubbed until P4.2 wires the content-hash token (spec
// §5: hash of summary|text|case_status|priority|sorted(tags)|sorted(components)).
func (a *Adapter) RemoteVersion(ctx context.Context, entityType, externalKey string) (backend.VersionToken, error) {
	return "", backend.ErrUnsupported // P4.2
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

func (a *Adapter) GetTestSteps(ctx context.Context, key string) ([]backend.Step, error) {
	return nil, backend.ErrUnsupported // P4.2 (inline-text flattening, spec §7)
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

func (a *Adapter) ListContainers(ctx context.Context, projectKey string, onProgress func(done, total int)) ([]backend.Container, []backend.ContainerLink, error) {
	return nil, nil, backend.ErrUnsupported // P4.2
}

func (a *Adapter) TestExecutionsForTest(ctx context.Context, testKey string) ([]backend.Container, []backend.ContainerLink, error) {
	return nil, nil, backend.ErrUnsupported // P4.2
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

func (a *Adapter) GetTestRuns(ctx context.Context, execKey string) ([]backend.TestRun, error) {
	return nil, backend.ErrUnsupported // P4.2
}

func (a *Adapter) ExecPlans(ctx context.Context, execKey string) ([]string, error) {
	return nil, backend.ErrUnsupported // P4.2
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

func (a *Adapter) ListRequirements(ctx context.Context, profileProjectKey string, sources []backend.RequirementSourceSpec, onProgress func(done, total int)) ([]backend.Requirement, []backend.RequirementLink, error) {
	return nil, nil, nil // P4.3 — EMPTY until the requirements plugin is detected+wired (spec §3.8)
}

func (a *Adapter) UpdateTestRequirements(ctx context.Context, testKey string, add []string, removeLinkIDs []string) error {
	return backend.ErrUnsupported // P4.3 (write)
}

func (a *Adapter) ListIssueLinkTypes(ctx context.Context) ([]string, error) {
	return nil, backend.ErrUnsupported // P4.3 — gated on requirements-plugin detection (spec §3.8)
}

func (a *Adapter) CreateRequirement(ctx context.Context, projectKey, issueType, summary, description, priority, components, fixVersions string) (string, error) {
	return "", backend.ErrUnsupported // P4.3 (write)
}

func (a *Adapter) DeleteRequirement(ctx context.Context, requirementKey string) error {
	return backend.ErrUnsupported // P4.3 (write)
}

func (a *Adapter) UpdateRequirementLinks(ctx context.Context, fromKey string, add []string, removeLinkIDs []string) error {
	return backend.ErrUnsupported // P4.3 (write)
}

func (a *Adapter) ListReqToReqLinks(ctx context.Context, reqKeys []string) ([]backend.ReqToReqLink, error) {
	return nil, nil // P4.3 — EMPTY: no req->req RPC in the plugin today (spec §3.8, OQ-3)
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

func (a *Adapter) ListStatuses(ctx context.Context, projectKey string) ([]string, error) {
	return nil, backend.ErrUnsupported // P4.2
}

func (a *Adapter) ListPriorities(ctx context.Context, projectKey string) ([]string, error) {
	return nil, backend.ErrUnsupported // P4.2
}

func (a *Adapter) ProjectComponents(ctx context.Context, projectKey string) ([]string, error) {
	return nil, backend.ErrUnsupported // P4.2
}

func (a *Adapter) ProjectVersions(ctx context.Context, projectKey string) ([]string, error) {
	return nil, backend.ErrUnsupported // P4.2
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

// Capabilities reports the base-Kiwi feature set (no plugins detected).
// Plugin deltas (SupportsRequirementObjects, SupportsIssueLinkTypes, and a
// future SupportsReview field) are wired in P4.3 once the plugin-detection
// probe (caps.go) is connected to this method — this task does not add any
// new Capabilities fields (per the P4.1 brief). Spec §4.1.
func (a *Adapter) Capabilities() backend.Capabilities {
	return backend.Capabilities{
		Name:                        "kiwi",
		IDStyle:                     "numeric", // Kiwi pks are ints (spec §4.1; see p4_1-report.md for the "integer" vs "numeric" note)
		SupportsJQLScope:            false,     // Product/Version/Build + ORM filters, not JQL
		StepModel:                   "inline-text",
		SupportsTestTypes:           true, // is_automated -> Manual/Automated
		SupportsFolders:             false,
		SupportsPreconditionObjects: false,
		SupportsRequirementObjects:  false, // flips true with the requirements plugin (P4.3)
		SupportsIssueLinkTypes:      false, // flips true with the requirements plugin (P4.3)
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
}
