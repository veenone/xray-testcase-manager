package backend

import (
	"context"
	"errors"
)

// ErrUnsupported is returned by a backend for an operation its target system
// does not support. Callers gate on Capabilities first; ErrUnsupported is the
// runtime backstop.
var ErrUnsupported = errors.New("backend: operation not supported")

// VersionToken is an opaque per-entity version marker used for optimistic
// concurrency. Each backend owns both how it produces a token
// (RemoteVersion) and how it orders two tokens (RemoteAhead) — for Xray this
// is a Jira `updated` timestamp string; other backends may use a content
// hash or a numeric revision instead.
type VersionToken string

// Container kind identifiers, mirroring the jira.Kind* constants so callers
// (the sync engine) can compare Container.Kind without importing internal/jira.
const (
	KindTestSet  = "testset"
	KindTestPlan = "testplan"
	KindTestExec = "testexec"
)

// Capabilities describes what a concrete backend supports so the app and sync
// engine can gate features. Full capability gating is a later phase; this
// struct is the minimal surface introduced now.
type Capabilities struct {
	// Name identifies the backend for diagnostics (e.g. "xray").
	Name string `json:"name"`

	// IDStyle is how entity ids are shaped ("opaque" for Xray issue keys).
	IDStyle string `json:"idStyle"`

	// SupportsJQLScope reports whether saved-scope queries use JQL.
	SupportsJQLScope bool `json:"supportsJqlScope"`

	// StepModel is how test steps are represented ("objects" for Xray steps).
	StepModel string `json:"stepModel"`

	SupportsTestTypes           bool `json:"supportsTestTypes"`
	SupportsFolders             bool `json:"supportsFolders"`
	SupportsPreconditionObjects bool `json:"supportsPreconditionObjects"`
	SupportsRequirementObjects  bool `json:"supportsRequirementObjects"`
	SupportsIssueLinkTypes      bool `json:"supportsIssueLinkTypes"`
	SupportsEnvironments        bool `json:"supportsEnvironments"`
	SupportsContainers          bool `json:"supportsContainers"`

	// ContainerKinds are the container kind identifiers the backend supports.
	ContainerKinds []string `json:"containerKinds"`

	SupportsTestRuns bool `json:"supportsTestRuns"`

	// StatusModel is how statuses transition ("workflow" for Jira).
	StatusModel string `json:"statusModel"`

	SupportsWorkflowTransitions bool `json:"supportsWorkflowTransitions"`
	SupportsBugCreation         bool `json:"supportsBugCreation"`
	SupportsBugLinks            bool `json:"supportsBugLinks"`
	SupportsTags                bool `json:"supportsTags"`
}

// Backend is the storage/tracker-agnostic contract the sync engine and app
// layer target. It is a single "fat" interface: every method here is a method
// currently invoked on *jira.Client from outside the internal/jira package.
//
// A concrete backend (internal/backend/xray) implements this by delegating to
// its underlying client and mapping between the neutral DTOs in dto.go and the
// backend-native shapes.
type Backend interface {
	// --- connection / auth ---
	TestConnection(ctx context.Context) (*User, error)
	IsDemo() bool
	SetRequirementLinkType(name string)

	// --- tests ---
	SearchTestsPage(ctx context.Context, projectKey, scopeJQL, since string, startAt, maxResults int) ([]Test, int, error)
	ListTestsBasic(ctx context.Context, keys []string) ([]TestBasic, error)
	GetTestFields(ctx context.Context, key string) (Test, error)
	// SearchTestsAcrossProjects / SearchPreconditionsAcrossProjects browse or
	// search issues in the given source projects, for cross-project linking of
	// preconditions, test calls, and cloned steps (RND_P_4TFINT_05-322). An
	// empty query lists all in the source projects (browse); a non-empty query
	// narrows by key or summary. Results are paged from offset (up to limit) and
	// the total match count is returned so the caller can paginate. Search is
	// restricted to projectKeys (the profile's configured sources); an empty
	// list yields no results. A backend with no cross-project search returns an
	// empty slice and zero total.
	SearchTestsAcrossProjects(ctx context.Context, projectKeys []string, query string, offset, limit int) ([]TestBasic, int, error)
	SearchPreconditionsAcrossProjects(ctx context.Context, projectKeys []string, query string, offset, limit int) ([]Precondition, int, error)
	CreateTest(ctx context.Context, projectKey, summary, description, priority string, labels, components []string) (string, error)
	UpdateIssue(ctx context.Context, key string, fields map[string]any) error
	GetTestMeta(ctx context.Context, key string) (TestMeta, error)

	// --- concurrency ---

	// RemoteVersion returns the remote's current version token for an entity.
	// entityType distinguishes entity kinds for backends that need it (e.g. a
	// future backend versioning tests and preconditions differently); Xray
	// ignores it and always returns the issue's `updated` timestamp.
	RemoteVersion(ctx context.Context, entityType, externalKey string) (VersionToken, error)

	// RemoteAhead reports whether remote represents a state strictly later
	// than base. The backend owns the ordering — for Xray this parses both
	// tokens as timestamps; other backends may compare hashes or revisions.
	RemoteAhead(base, remote VersionToken) bool

	// --- steps ---
	GetTestSteps(ctx context.Context, key string) ([]Step, error)
	CreateTestStep(ctx context.Context, key, action, data, expected string) (string, error)
	UpdateTestStep(ctx context.Context, key, stepID string, fields map[string]string) error
	DeleteTestStep(ctx context.Context, key, stepID string) error
	MoveTestStep(ctx context.Context, key, stepID string, index int, action, data, expected string) error
	CreateCalledTestStep(ctx context.Context, key, calledTestKey, calledTestID string) (string, error)

	// --- custom fields ---
	ListCustomFields(ctx context.Context, projectKey string) ([]CustomFieldDef, error)
	GetTestCustomFields(ctx context.Context, testKey string) (map[string]string, error)
	CustomFieldValue(ctx context.Context, fieldID, value string) (string, any, error)
	ExecTypeFieldValue(ctx context.Context, execType string) (fieldID string, value any, ok bool, err error)
	// Cucumber/Generic body-field resolvers (#54): resolve a body-field value
	// to its backend field id + typed value. ok=false means the backend has no
	// such field (e.g. Kiwi), so the commit engine skips it.
	CucumberScenarioFieldValue(ctx context.Context, v string) (fieldID string, value any, ok bool, err error)
	CucumberTypeFieldValue(ctx context.Context, v string) (fieldID string, value any, ok bool, err error)
	GenericDefinitionFieldValue(ctx context.Context, v string) (fieldID string, value any, ok bool, err error)

	// --- containers (Test Sets / Plans / Executions) ---
	ListContainers(ctx context.Context, projectKey string, onProgress func(done, total int)) ([]Container, []ContainerLink, error)
	TestExecutionsForTest(ctx context.Context, testKey string) ([]Container, []ContainerLink, error)
	CreateContainer(ctx context.Context, projectKey, kind, summary string) (string, error)
	AddTestsToContainer(ctx context.Context, kind, containerKey string, testKeys []string) error
	RemoveTestsFromContainer(ctx context.Context, kind, containerKey string, testKeys []string) error
	SetTestRunStatus(ctx context.Context, execKey, testKey, status string) error
	// AddTestRunDefect/RemoveTestRunDefect link/unlink an existing bug as a
	// defect on a specific test-run; SetTestRunComment sets a run remark
	// (RND_P_4TFINT_05-296). Xray-raven-specific; a backend without run-level
	// defect/comment support returns ErrUnsupported (the commit engine gates
	// these off via issueBackedWrites).
	AddTestRunDefect(ctx context.Context, execKey, testKey, bugKey string) error
	RemoveTestRunDefect(ctx context.Context, execKey, testKey, bugKey string) error
	SetTestRunComment(ctx context.Context, execKey, testKey, comment string) error
	SetContainerEnvironments(ctx context.Context, execKey string, envs []string) error
	DeleteContainer(ctx context.Context, kind, containerKey string) error
	GetTestRuns(ctx context.Context, execKey string) ([]TestRun, error)
	ExecPlans(ctx context.Context, execKey string) ([]string, error)

	// --- preconditions ---
	ListPreconditions(ctx context.Context, projectKey string, onProgress func(done, total int)) ([]Precondition, map[string][]string, error)
	CreatePrecondition(ctx context.Context, projectKey, summary, ptype, description string) (string, error)
	UpdateTestPreconditions(ctx context.Context, testKey string, add, remove []string) error
	DeletePrecondition(ctx context.Context, preconditionKey string) error

	// --- requirements ---
	ListRequirements(ctx context.Context, profileProjectKey string, sources []RequirementSourceSpec, onProgress func(done, total int)) ([]Requirement, []RequirementLink, error)
	UpdateTestRequirements(ctx context.Context, testKey string, add []string, removeLinkIDs []string) error
	ListIssueLinkTypes(ctx context.Context) ([]string, error)
	ListIssueLinkTypeDetails(ctx context.Context) ([]IssueLinkType, error)
	CreateRequirement(ctx context.Context, projectKey, issueType, summary, description, priority, components, fixVersions string, extraFields map[string]any) (string, error)
	GetRequirementCreateFields(ctx context.Context, projectKey, issueType string) ([]BugCreateField, error)
	DeleteRequirement(ctx context.Context, requirementKey string) error
	UpdateRequirementLinks(ctx context.Context, fromKey string, add []string, removeLinkIDs []string) error
	ListReqToReqLinks(ctx context.Context, reqKeys []string) ([]ReqToReqLink, error)

	// --- bugs ---
	ListBugs(ctx context.Context, testProjectKey string, testKeys []string, issueType string, onProgress func(done, total int)) ([]Bug, []BugLink, error)
	ListProjectBugs(ctx context.Context, projKey, issueType string) ([]Bug, error)
	GetBugCreateFields(ctx context.Context, projectKey, issueType string) ([]BugCreateField, error)
	CreateBug(ctx context.Context, projectKey, issueType, summary, description, priority string, labels []string, extraFields map[string]any) (string, error)
	CreateBugLink(ctx context.Context, testKey, bugKey string) error
	GetBugDetail(ctx context.Context, bugKey string) (BugDetail, error)

	// --- folders (Test Repository) ---
	FolderTree(ctx context.Context, projectKey string) (FolderTreeResult, error)
	ListFolders(ctx context.Context, projectKey string) ([]Folder, error)
	ListTestsInFolder(ctx context.Context, projectKey, folderID string) ([]string, error)
	CreateFolder(ctx context.Context, projectKey, parentPath, name string) error
	RenameFolder(ctx context.Context, projectKey, path, newName string) error
	DeleteFolder(ctx context.Context, projectKey, path string) error
	MoveTestToFolder(ctx context.Context, projectKey, testKey, folderID string) error

	// --- workflow ---
	GetTransitions(ctx context.Context, key, currentStatus string) ([]Transition, error)
	PostTransition(ctx context.Context, key, transitionID string) error

	// --- metadata ---
	ListStatuses(ctx context.Context, projectKey string) ([]string, error)
	ListPriorities(ctx context.Context, projectKey string) ([]string, error)
	ProjectComponents(ctx context.Context, projectKey string) ([]string, error)
	ProjectVersions(ctx context.Context, projectKey string) ([]string, error)

	// --- comments ---
	AddComment(ctx context.Context, issueKey, body string) error

	// --- field payload shaping ---
	// FieldsForJira translates the app's internal field/value pairs into the
	// backend's native issue field-update payload shape. The result is opaque
	// to callers outside the backend package — it is only ever relayed
	// straight into UpdateIssue.
	FieldsForJira(updates map[string]string) map[string]any

	// --- capabilities ---
	Capabilities() Capabilities
}

// TestPreconditionReader is an optional capability: reading the Preconditions
// of one Test directly, without walking every Precondition in the project.
//
// It is deliberately kept off Backend. Only Xray exposes a test-side
// association endpoint, and widening the Backend interface would force every
// other adapter to implement a method it has no cheaper way to answer than the
// project-wide ListPreconditions it already provides. Callers type-assert and
// skip the fast path when the backend does not implement it.
type TestPreconditionReader interface {
	ListTestPreconditions(ctx context.Context, testKey string) ([]Precondition, error)
}
