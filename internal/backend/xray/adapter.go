// Package xray adapts the existing *jira.Client (Xray Server/DC + Jira DC REST)
// to the neutral backend.Backend interface. It is a thin delegating shim: each
// method forwards to the wrapped client and maps between the jira.* shapes and
// the neutral backend.* DTOs. No business logic lives here.
package xray

import (
	"context"

	"xray-test-manager/internal/backend"
	"xray-test-manager/internal/jira"
)

// Adapter implements backend.Backend by wrapping a *jira.Client.
type Adapter struct {
	c *jira.Client
}

// New wraps an existing Jira/Xray client as a backend.Backend.
func New(c *jira.Client) *Adapter { return &Adapter{c: c} }

// Compile-time assertion that the adapter satisfies the interface.
var _ backend.Backend = (*Adapter)(nil)

// --- connection / auth ---

func (a *Adapter) TestConnection(ctx context.Context) (*backend.User, error) {
	u, err := a.c.TestConnection(ctx)
	if err != nil {
		return nil, err
	}
	return toUser(u), nil
}

func (a *Adapter) IsDemo() bool { return a.c.IsDemo() }

func (a *Adapter) SetRequirementLinkType(name string) { a.c.SetRequirementLinkType(name) }

// --- tests ---

func (a *Adapter) SearchTestsPage(ctx context.Context, projectKey, scopeJQL, since string, startAt, maxResults int) ([]backend.Test, int, error) {
	tests, total, err := a.c.SearchTestsPage(ctx, projectKey, scopeJQL, since, startAt, maxResults)
	if err != nil {
		return nil, 0, err
	}
	return toTests(tests), total, nil
}

func (a *Adapter) ListTestsBasic(ctx context.Context, keys []string) ([]backend.TestBasic, error) {
	tb, err := a.c.ListTestsBasic(ctx, keys)
	if err != nil {
		return nil, err
	}
	return toTestBasics(tb), nil
}

func (a *Adapter) SearchTestsAcrossProjects(ctx context.Context, projectKeys []string, query string, offset, limit int) ([]backend.TestBasic, int, error) {
	tb, total, err := a.c.SearchTestsAcrossProjects(ctx, projectKeys, query, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	return toTestBasics(tb), total, nil
}

func (a *Adapter) SearchPreconditionsAcrossProjects(ctx context.Context, projectKeys []string, query string, offset, limit int) ([]backend.Precondition, int, error) {
	pcs, total, err := a.c.SearchPreconditionsAcrossProjects(ctx, projectKeys, query, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	return toPreconditions(pcs), total, nil
}

func (a *Adapter) GetTestFields(ctx context.Context, key string) (backend.Test, error) {
	t, err := a.c.GetTestFields(ctx, key)
	if err != nil {
		return backend.Test{}, err
	}
	return toTest(t), nil
}

func (a *Adapter) CreateTest(ctx context.Context, projectKey, summary, description, priority string, labels, components []string) (string, error) {
	return a.c.CreateTest(ctx, projectKey, summary, description, priority, labels, components)
}

func (a *Adapter) UpdateIssue(ctx context.Context, key string, fields map[string]any) error {
	return a.c.UpdateIssue(ctx, key, fields)
}

func (a *Adapter) GetTestMeta(ctx context.Context, key string) (backend.TestMeta, error) {
	m, err := a.c.GetTestMeta(ctx, key)
	if err != nil {
		return backend.TestMeta{}, err
	}
	return toTestMeta(m), nil
}

// --- concurrency ---

// RemoteVersion returns the issue's current `updated` timestamp as an opaque
// VersionToken. entityType is ignored — Xray versions every entity by its
// Jira issue `updated` field.
func (a *Adapter) RemoteVersion(ctx context.Context, entityType, externalKey string) (backend.VersionToken, error) {
	upd, err := a.c.GetIssueUpdated(ctx, externalKey)
	if err != nil {
		return "", err
	}
	return backend.VersionToken(upd), nil
}

// RemoteAhead reports whether remote's Jira `updated` timestamp is strictly
// later than base's. On parse failure it is permissive (returns false) so a
// malformed remote string can't manufacture a phantom conflict.
func (a *Adapter) RemoteAhead(base, remote backend.VersionToken) bool {
	bt, ok1 := parseJiraTime(string(base))
	rt, ok2 := parseJiraTime(string(remote))
	if !ok1 || !ok2 {
		return false
	}
	return rt.After(bt)
}

// --- steps ---

func (a *Adapter) GetTestSteps(ctx context.Context, key string) ([]backend.Step, error) {
	steps, err := a.c.GetTestSteps(ctx, key)
	if err != nil {
		return nil, err
	}
	return toSteps(steps), nil
}

func (a *Adapter) CreateTestStep(ctx context.Context, key, action, data, expected string) (string, error) {
	return a.c.CreateTestStep(ctx, key, action, data, expected)
}

func (a *Adapter) UpdateTestStep(ctx context.Context, key, stepID string, fields map[string]string) error {
	return a.c.UpdateTestStep(ctx, key, stepID, fields)
}

func (a *Adapter) DeleteTestStep(ctx context.Context, key, stepID string) error {
	return a.c.DeleteTestStep(ctx, key, stepID)
}

func (a *Adapter) MoveTestStep(ctx context.Context, key, stepID string, index int, action, data, expected string) error {
	return a.c.MoveTestStep(ctx, key, stepID, index, action, data, expected)
}

func (a *Adapter) CreateCalledTestStep(ctx context.Context, key, calledTestKey, calledTestID string) (string, error) {
	return a.c.CreateCalledTestStep(ctx, key, calledTestKey, calledTestID)
}

// --- custom fields ---

func (a *Adapter) ListCustomFields(ctx context.Context, projectKey string) ([]backend.CustomFieldDef, error) {
	defs, err := a.c.ListCustomFields(ctx, projectKey)
	if err != nil {
		return nil, err
	}
	return toCustomFieldDefs(defs), nil
}

func (a *Adapter) GetTestCustomFields(ctx context.Context, testKey string) (map[string]string, error) {
	return a.c.GetTestCustomFields(ctx, testKey)
}

func (a *Adapter) CustomFieldValue(ctx context.Context, fieldID, value string) (string, any, error) {
	return a.c.CustomFieldValue(ctx, fieldID, value)
}

func (a *Adapter) ExecTypeFieldValue(ctx context.Context, execType string) (fieldID string, value any, ok bool, err error) {
	return a.c.ExecTypeFieldValue(ctx, execType)
}

func (a *Adapter) CucumberScenarioFieldValue(ctx context.Context, v string) (string, any, bool, error) {
	return a.c.CucumberScenarioFieldValue(ctx, v)
}

func (a *Adapter) CucumberTypeFieldValue(ctx context.Context, v string) (string, any, bool, error) {
	return a.c.CucumberTypeFieldValue(ctx, v)
}

func (a *Adapter) GenericDefinitionFieldValue(ctx context.Context, v string) (string, any, bool, error) {
	return a.c.GenericDefinitionFieldValue(ctx, v)
}

// --- containers ---

func (a *Adapter) ListContainers(ctx context.Context, projectKey string, onProgress func(done, total int)) ([]backend.Container, []backend.ContainerLink, error) {
	cs, links, err := a.c.ListContainers(ctx, projectKey, onProgress)
	if err != nil {
		return nil, nil, err
	}
	return toContainers(cs), toContainerLinks(links), nil
}

func (a *Adapter) TestExecutionsForTest(ctx context.Context, testKey string) ([]backend.Container, []backend.ContainerLink, error) {
	cs, links, err := a.c.TestExecutionsForTest(ctx, testKey)
	if err != nil {
		return nil, nil, err
	}
	return toContainers(cs), toContainerLinks(links), nil
}

func (a *Adapter) CreateContainer(ctx context.Context, projectKey, kind, summary string) (string, error) {
	return a.c.CreateContainer(ctx, projectKey, kind, summary)
}

func (a *Adapter) AddTestsToContainer(ctx context.Context, kind, containerKey string, testKeys []string) error {
	return a.c.AddTestsToContainer(ctx, kind, containerKey, testKeys)
}

func (a *Adapter) RemoveTestsFromContainer(ctx context.Context, kind, containerKey string, testKeys []string) error {
	return a.c.RemoveTestsFromContainer(ctx, kind, containerKey, testKeys)
}

func (a *Adapter) SetTestRunStatus(ctx context.Context, execKey, testKey, status string) error {
	return a.c.SetTestRunStatus(ctx, execKey, testKey, status)
}

func (a *Adapter) AddTestRunDefect(ctx context.Context, execKey, testKey, bugKey string) error {
	return a.c.AddTestRunDefect(ctx, execKey, testKey, bugKey)
}

func (a *Adapter) RemoveTestRunDefect(ctx context.Context, execKey, testKey, bugKey string) error {
	return a.c.RemoveTestRunDefect(ctx, execKey, testKey, bugKey)
}

func (a *Adapter) SetTestRunComment(ctx context.Context, execKey, testKey, comment string) error {
	return a.c.SetTestRunComment(ctx, execKey, testKey, comment)
}

func (a *Adapter) SetContainerEnvironments(ctx context.Context, execKey string, envs []string) error {
	return a.c.SetContainerEnvironments(ctx, execKey, envs)
}

func (a *Adapter) DeleteContainer(ctx context.Context, kind, containerKey string) error {
	return a.c.DeleteContainer(ctx, kind, containerKey)
}

func (a *Adapter) GetTestRuns(ctx context.Context, execKey string) ([]backend.TestRun, error) {
	runs, err := a.c.GetTestRuns(ctx, execKey)
	if err != nil {
		return nil, err
	}
	return toTestRuns(runs), nil
}

func (a *Adapter) ExecPlans(ctx context.Context, execKey string) ([]string, error) {
	return a.c.ExecPlans(ctx, execKey)
}

// --- preconditions ---

func (a *Adapter) ListPreconditions(ctx context.Context, projectKey string, onProgress func(done, total int)) ([]backend.Precondition, map[string][]string, error) {
	pcs, membership, err := a.c.ListPreconditions(ctx, projectKey, onProgress)
	if err != nil {
		return nil, nil, err
	}
	return toPreconditions(pcs), membership, nil
}

// ListTestPreconditions implements backend.TestPreconditionReader. Xray exposes
// the association from the test side, so one Test's Preconditions can be read
// without walking the project.
func (a *Adapter) ListTestPreconditions(ctx context.Context, testKey string) ([]backend.Precondition, error) {
	pcs, err := a.c.ListTestPreconditions(ctx, testKey)
	if err != nil {
		return nil, err
	}
	return toPreconditions(pcs), nil
}

func (a *Adapter) CreatePrecondition(ctx context.Context, projectKey, summary, ptype, description string) (string, error) {
	return a.c.CreatePrecondition(ctx, projectKey, summary, ptype, description)
}

func (a *Adapter) UpdateTestPreconditions(ctx context.Context, testKey string, add, remove []string) error {
	return a.c.UpdateTestPreconditions(ctx, testKey, add, remove)
}

func (a *Adapter) DeletePrecondition(ctx context.Context, preconditionKey string) error {
	return a.c.DeletePrecondition(ctx, preconditionKey)
}

// --- requirements ---

func (a *Adapter) ListRequirements(ctx context.Context, profileProjectKey string, sources []backend.RequirementSourceSpec, onProgress func(done, total int)) ([]backend.Requirement, []backend.RequirementLink, error) {
	reqs, links, err := a.c.ListRequirements(ctx, profileProjectKey, fromRequirementSourceSpecs(sources), onProgress)
	if err != nil {
		return nil, nil, err
	}
	return toRequirements(reqs), toRequirementLinks(links), nil
}

func (a *Adapter) UpdateTestRequirements(ctx context.Context, testKey string, add []string, removeLinkIDs []string) error {
	return a.c.UpdateTestRequirements(ctx, testKey, add, removeLinkIDs)
}

func (a *Adapter) ListIssueLinkTypes(ctx context.Context) ([]string, error) {
	return a.c.ListIssueLinkTypes(ctx)
}

func (a *Adapter) ListIssueLinkTypeDetails(ctx context.Context) ([]backend.IssueLinkType, error) {
	raw, err := a.c.ListIssueLinkTypeDetails(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]backend.IssueLinkType, len(raw))
	for i, t := range raw {
		out[i] = backend.IssueLinkType{Name: t.Name, Inward: t.Inward, Outward: t.Outward}
	}
	return out, nil
}

func (a *Adapter) CreateRequirement(ctx context.Context, projectKey, issueType, summary, description, priority, components, fixVersions string, extraFields map[string]any) (string, error) {
	return a.c.CreateRequirement(ctx, projectKey, issueType, summary, description, priority, components, fixVersions, extraFields)
}

func (a *Adapter) GetRequirementCreateFields(ctx context.Context, projectKey, issueType string) ([]backend.BugCreateField, error) {
	fields, err := a.c.GetRequirementCreateFields(ctx, projectKey, issueType)
	if err != nil {
		return nil, err
	}
	return toBugCreateFields(fields), nil
}

func (a *Adapter) DeleteRequirement(ctx context.Context, requirementKey string) error {
	return a.c.DeleteRequirement(ctx, requirementKey)
}

func (a *Adapter) UpdateRequirementLinks(ctx context.Context, fromKey string, add []string, removeLinkIDs []string) error {
	return a.c.UpdateRequirementLinks(ctx, fromKey, add, removeLinkIDs)
}

func (a *Adapter) ListReqToReqLinks(ctx context.Context, reqKeys []string) ([]backend.ReqToReqLink, error) {
	links, err := a.c.ListReqToReqLinks(ctx, reqKeys)
	if err != nil {
		return nil, err
	}
	return toReqToReqLinks(links), nil
}

// --- bugs ---

func (a *Adapter) ListBugs(ctx context.Context, testProjectKey string, testKeys []string, issueType string, onProgress func(done, total int)) ([]backend.Bug, []backend.BugLink, error) {
	bugs, links, err := a.c.ListBugs(ctx, testProjectKey, testKeys, issueType, onProgress)
	if err != nil {
		return nil, nil, err
	}
	return toBugs(bugs), toBugLinks(links), nil
}

func (a *Adapter) ListProjectBugs(ctx context.Context, projKey, issueType string) ([]backend.Bug, error) {
	bugs, err := a.c.ListProjectBugs(ctx, projKey, issueType)
	if err != nil {
		return nil, err
	}
	return toBugs(bugs), nil
}

func (a *Adapter) GetBugCreateFields(ctx context.Context, projectKey, issueType string) ([]backend.BugCreateField, error) {
	fields, err := a.c.GetBugCreateFields(ctx, projectKey, issueType)
	if err != nil {
		return nil, err
	}
	return toBugCreateFields(fields), nil
}

func (a *Adapter) CreateBug(ctx context.Context, projectKey, issueType, summary, description, priority string, labels []string, extraFields map[string]any) (string, error) {
	return a.c.CreateBug(ctx, projectKey, issueType, summary, description, priority, labels, extraFields)
}

func (a *Adapter) CreateBugLink(ctx context.Context, testKey, bugKey string) error {
	return a.c.CreateBugLink(ctx, testKey, bugKey)
}

func (a *Adapter) GetBugDetail(ctx context.Context, bugKey string) (backend.BugDetail, error) {
	d, err := a.c.GetBugDetail(ctx, bugKey)
	if err != nil {
		return backend.BugDetail{}, err
	}
	return toBugDetail(d), nil
}

// --- folders ---

func (a *Adapter) FolderTree(ctx context.Context, projectKey string) (backend.FolderTreeResult, error) {
	tree, err := a.c.FolderTree(ctx, projectKey)
	if err != nil {
		return backend.FolderTreeResult{}, err
	}
	return toFolderTreeResult(tree), nil
}

func (a *Adapter) ListFolders(ctx context.Context, projectKey string) ([]backend.Folder, error) {
	folders, err := a.c.ListFolders(ctx, projectKey)
	if err != nil {
		return nil, err
	}
	return toFolders(folders), nil
}

func (a *Adapter) ListTestsInFolder(ctx context.Context, projectKey, folderID string) ([]string, error) {
	return a.c.ListTestsInFolder(ctx, projectKey, folderID)
}

func (a *Adapter) CreateFolder(ctx context.Context, projectKey, parentPath, name string) error {
	return a.c.CreateFolder(ctx, projectKey, parentPath, name)
}

func (a *Adapter) RenameFolder(ctx context.Context, projectKey, path, newName string) error {
	return a.c.RenameFolder(ctx, projectKey, path, newName)
}

func (a *Adapter) DeleteFolder(ctx context.Context, projectKey, path string) error {
	return a.c.DeleteFolder(ctx, projectKey, path)
}

func (a *Adapter) MoveTestToFolder(ctx context.Context, projectKey, testKey, folderID string) error {
	return a.c.MoveTestToFolder(ctx, projectKey, testKey, folderID)
}

// --- workflow ---

func (a *Adapter) GetTransitions(ctx context.Context, key, currentStatus string) ([]backend.Transition, error) {
	ts, err := a.c.GetTransitions(ctx, key, currentStatus)
	if err != nil {
		return nil, err
	}
	return toTransitions(ts), nil
}

func (a *Adapter) PostTransition(ctx context.Context, key, transitionID string) error {
	return a.c.PostTransition(ctx, key, transitionID)
}

// --- metadata ---

func (a *Adapter) ListStatuses(ctx context.Context, projectKey string) ([]string, error) {
	return a.c.ListStatuses(ctx, projectKey)
}

func (a *Adapter) ListPriorities(ctx context.Context, projectKey string) ([]string, error) {
	return a.c.ListPriorities(ctx, projectKey)
}

func (a *Adapter) ProjectComponents(ctx context.Context, projectKey string) ([]string, error) {
	return a.c.ProjectComponents(ctx, projectKey)
}

func (a *Adapter) ProjectVersions(ctx context.Context, projectKey string) ([]string, error) {
	return a.c.ProjectVersions(ctx, projectKey)
}

// --- comments ---

func (a *Adapter) AddComment(ctx context.Context, issueKey, body string) error {
	return a.c.AddComment(ctx, issueKey, body)
}

// --- field payload shaping ---

func (a *Adapter) FieldsForJira(updates map[string]string) map[string]any {
	return jira.FieldsForJira(updates)
}

// --- capabilities ---

// Capabilities reports the full Xray feature set. Xray (Server/DC) supports
// every capability the app models today except tags.
func (a *Adapter) Capabilities() backend.Capabilities {
	return backend.Capabilities{
		Name:                        "xray",
		IDStyle:                     "opaque",
		SupportsJQLScope:            true,
		StepModel:                   "objects",
		SupportsTestTypes:           true,
		SupportsFolders:             true,
		SupportsPreconditionObjects: true,
		SupportsRequirementObjects:  true,
		SupportsIssueLinkTypes:      true,
		SupportsEnvironments:        true,
		SupportsContainers:          true,
		ContainerKinds:              []string{jira.KindTestSet, jira.KindTestPlan, jira.KindTestExec},
		SupportsTestRuns:            true,
		StatusModel:                 "workflow",
		SupportsWorkflowTransitions: true,
		SupportsBugCreation:         true,
		SupportsBugLinks:            true,
		SupportsTags:                false,
	}
}
