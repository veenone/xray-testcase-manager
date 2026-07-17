// Package backend defines the storage/tracker-agnostic contract that the sync
// engine and app layer target. Concrete backends (Xray today, Kiwi later)
// implement the Backend interface in a sub-package.
//
// The DTOs in this file are neutral mirrors of the structs currently returned
// by internal/jira. For this phase they intentionally keep the SAME field
// names and types as their jira.* originals to minimise churn during the
// migration; renames/normalisation happen in a later phase.
package backend

// User identifies the authenticated account on the target backend.
type User struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Email       string `json:"emailAddress"`
}

// Test is a single test case pulled from the backend.
type Test struct {
	Key         string
	ID          string
	Summary     string
	Description string
	Status      string
	Priority    string
	Labels      []string
	Components  []string
	Updated     string
	FolderID    string
	// ExecType is the test type / execution type (Manual / Automated /
	// Generic / Cucumber for Xray). Best-effort; may be empty.
	ExecType string
	// FixVersions are the fix version names assigned to this test. Read-only
	// display values; never edited locally.
	FixVersions []string
}

// BugLinkRef is a linked issue reached from a Test (key + issue type), used to
// harvest bugs reached through cross-project member Tests.
type BugLinkRef struct {
	Key        string
	IssueType  string
	LinkID     string
	ProjectKey string
	Summary    string
	Status     string
	Priority   string
}

// TestBasic is a lightweight test projection (key/summary/status + links).
type TestBasic struct {
	Key        string
	Summary    string
	Status     string
	ProjectKey string
	// IssueLinks are the issues this Test links to (key + issue type).
	IssueLinks []BugLinkRef
}

// Step is one manual test step (or a "test call" step when CalledTestKey set).
type Step struct {
	ID       string `json:"id"`
	Index    int    `json:"index"`
	Action   string `json:"action"`
	Data     string `json:"data"`
	Expected string `json:"expected"`
	// CalledTestKey is set when the step invokes another Test rather than
	// holding manual action/data/expected content.
	CalledTestKey string `json:"calledTestKey"`
}

// Container is a Test Set / Test Plan / Test Execution grouping.
type Container struct {
	Key           string
	Kind          string
	Summary       string
	Status        string
	ParentKey     string
	ParentSummary string
	IssueType     string
	Environments  []string
	FixVersions   []string
	Created       string
	Updated       string
	Resolved      string
	Description   string
}

// ContainerLink associates a Test with a Container (and its run status).
type ContainerLink struct {
	ContainerKey string
	TestKey      string
	RunStatus    string
}

// Precondition is an Xray precondition object.
type Precondition struct {
	Key         string
	Summary     string
	Type        string
	Description string
	// Condition is the precondition definition text, distinct from the issue
	// description. May be empty on live until the field id is verified.
	Condition string
}

// TestMeta carries created/updated audit metadata for a Test.
type TestMeta struct {
	Created   string `json:"created"`
	Creator   string `json:"creator"`
	Updated   string `json:"updated"`
	UpdatedBy string `json:"updatedBy"`
}

// TestRun is one execution result of a Test within a Test Execution.
type TestRun struct {
	TestKey     string
	Status      string
	StartedAt   string
	FinishedAt  string
	ExecutedBy  string
	Environment string
	Defects     []string
	Comment     string
	CreatedAt   string
	UpdatedAt   string
}

// Transition is an available workflow transition from the current status.
type Transition struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	To   string `json:"to"`
}

// CustomFieldDef describes a custom field available on the backend.
type CustomFieldDef struct {
	ID   string
	Name string
	Type string
}

// Folder is a node in the Test Repository folder tree.
type Folder struct {
	ID             string
	ParentID       string
	Name           string
	XrayID         string
	TestCount      int
	TotalTestCount int
}

// FolderRef is a folder id + path pair.
type FolderRef struct {
	ID   string
	Path string
}

// FolderTreeResult is the full folder tree plus test membership.
type FolderTreeResult struct {
	Folders          []Folder
	TreeMembership   map[string]string // testKey -> folder path
	FoldersWithTests []FolderRef       // only folders whose testCount > 0
}

// Requirement is a requirement/coverage source issue.
type Requirement struct {
	Key         string
	ProjectKey  string
	IssueType   string
	Summary     string
	Status      string
	Updated     string
	Priority    string
	Components  string // comma-separated component names
	FixVersions string // comma-separated fix version names
	Sprint      string
	Description string
	EpicKey     string
}

// RequirementLink associates a Test with a Requirement.
type RequirementLink struct {
	TestKey        string
	RequirementKey string
	LinkID         string
}

// RequirementSourceSpec scopes a requirement pull to a project + issue types.
type RequirementSourceSpec struct {
	ProjectKey string
	IssueTypes []string
	ScopeJQL   string
}

// ReqToReqLink is a requirement-to-requirement link.
type ReqToReqLink struct {
	FromKey  string
	ToKey    string
	LinkType string
	LinkID   string
}

// Bug is a defect issue reachable from a Test.
type Bug struct {
	Key        string
	ProjectKey string
	IssueType  string
	Summary    string
	Status     string
	Priority   string
	Updated    string
}

// BugLink associates a Test with a Bug (and the Jira link id).
type BugLink struct {
	TestKey string
	BugKey  string
	LinkID  string
}

// BugFieldOption is one allowed value of a create-screen field.
type BugFieldOption struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

// BugCreateField describes a field on the bug create screen.
type BugCreateField struct {
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	Required      bool             `json:"required"`
	Type          string           `json:"type"`
	AllowedValues []BugFieldOption `json:"allowedValues"`
}

// BugDetail carries the extended fields of a single bug.
type BugDetail struct {
	Description       string `json:"description"`
	DefectOrigin      string `json:"defectOrigin"`
	DefectAnalysis    string `json:"defectAnalysis"`
	CorrectionDetails string `json:"correctionDetails"`
	Reporter          string `json:"reporter"`
	Severity          string `json:"severity"`
}
