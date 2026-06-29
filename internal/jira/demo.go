package jira

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Demo mode generates fake Test data so the UI can be exercised without a
// real Jira instance. A profile triggers demo mode when its Jira URL is
// "demo", "demo://…", or "mock://…" (case-insensitive). Auth tokens are
// ignored; TestConnection / SearchTestsPage / ListFolders / ListPreconditions
// short-circuit to the generators in this file.

// demoTestCount is the size of the fake dataset — enough to exercise the
// grid's pagination, search, filter and sort without being absurd.
const demoTestCount = 5000

// isDemoURL reports whether a profile's Jira URL selects demo mode. Recognises
// "demo", a "demo:" / "mock:" prefix, and "demo-" variants (e.g. "demo-pkcs",
// used to pick a specific built-in demo dataset). Keep in sync with jiraUrlError
// in the frontend (ProfileForm.tsx).
func isDemoURL(baseURL string) bool {
	u := strings.ToLower(strings.TrimSpace(baseURL))
	return u == "demo" ||
		strings.HasPrefix(u, "demo:") ||
		strings.HasPrefix(u, "demo-") ||
		strings.HasPrefix(u, "mock:")
}

// demoTestsPage returns the [startAt, startAt+maxResults) slice of the demo
// dataset plus the dataset total — matching SearchTestsPage's signature.
func demoTestsPage(theme demoTheme, projectKey string, startAt, maxResults int) ([]Test, int) {
	if projectKey == "" {
		projectKey = "DEMO"
	}
	total := theme.TestCount
	if startAt >= total {
		return nil, total
	}
	end := startAt + maxResults
	if end > total {
		end = total
	}
	out := make([]Test, 0, end-startAt)
	for i := startAt; i < end; i++ {
		out = append(out, makeDemoTest(theme, projectKey, i))
	}
	return out, total
}

// Source vocabularies. Statuses and priorities are deliberately repeated to
// produce a weighted distribution — most tests are Approved/Done, a few are
// Blocked or Deprecated.

var demoFeatures = []string{
	"Login", "Logout", "User registration", "Password reset",
	"Search", "Filter results", "Sort results", "Pagination",
	"Checkout", "Cart", "Add to cart", "Remove from cart",
	"Profile update", "Settings", "Dashboard", "Notifications",
	"Payment", "Refund", "Reports", "Export to CSV",
	"Import data", "Admin console", "Permissions", "Audit log",
	"API rate limit", "File upload", "File download",
	"Multi-factor auth", "Session timeout", "Bulk operations",
}

var demoConditions = []string{
	"with valid input",
	"with invalid input",
	"from empty state",
	"after timeout",
	"with special characters",
	"on a slow network",
	"as an admin user",
	"as a guest user",
	"with maximum boundary values",
	"with minimum boundary values",
	"after page reload",
	"with concurrent users",
}

var demoStatuses = []string{
	"Approved", "Approved", "Approved", "Approved",
	"Done", "Done", "Done",
	"In Progress", "In Progress",
	"Open",
	"Blocked",
	"Deprecated",
}

// demoStatusList is the demo workflow's distinct statuses, in workflow order —
// what ListStatuses returns for a demo profile.
func demoStatusList() []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, s := range demoStatuses {
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

var demoPriorities = []string{
	"Medium", "Medium", "Medium",
	"High", "High",
	"Low",
	"Critical",
}

// demoPriorityList is the demo instance's distinct priority names — what
// ListPriorities returns for a demo profile (FR-1).
func demoPriorityList() []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, p := range demoPriorities {
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

var demoLabels = []string{
	"smoke", "regression", "p1", "p2", "p3",
	"api", "ui", "manual", "automated", "flaky",
	"security", "performance",
}

// demoFolderCategories defines the demo Test Repository hierarchy. Feature
// names match those in demoFeatures so each test slots into the matching
// leaf folder.
var demoFolderCategories = []folderCategory{
	{"Authentication", []string{"Login", "Logout", "User registration", "Password reset", "Multi-factor auth", "Session timeout"}},
	{"Browse", []string{"Search", "Filter results", "Sort results", "Pagination"}},
	{"Commerce", []string{"Checkout", "Cart", "Add to cart", "Remove from cart", "Payment", "Refund"}},
	{"User", []string{"Profile update", "Settings", "Notifications"}},
	{"Reporting", []string{"Dashboard", "Reports", "Export to CSV", "Import data"}},
	{"Admin", []string{"Admin console", "Permissions", "Audit log"}},
	{"System", []string{"API rate limit", "File upload", "File download", "Bulk operations"}},
}

// demoFolders returns the demo folder tree for the given theme. Folder IDs are
// full paths so a folder is uniquely identified by its location in the tree
// ("/Authentication/Login"). The categories in theme.Categories determine the
// hierarchy, so PKCS and generic themes each produce their own folder structure.
func demoFolders(theme demoTheme) []Folder {
	out := make([]Folder, 0)
	for _, cat := range theme.Categories {
		catID := "/" + cat.Name
		out = append(out, Folder{ID: catID, ParentID: "", Name: cat.Name})
		for _, feat := range cat.Features {
			out = append(out, Folder{
				ID:       catID + "/" + feat,
				ParentID: catID,
				Name:     feat,
			})
		}
	}
	return out
}

// demoFolderForFeature returns the leaf folder ID holding tests for a given
// feature, or empty if the feature isn't mapped.
func demoFolderForFeature(theme demoTheme, feature string) string {
	for _, cat := range theme.Categories {
		for _, f := range cat.Features {
			if f == feature {
				return "/" + cat.Name + "/" + f
			}
		}
	}
	return ""
}

// preconditionDefs is the master list of distinct demo preconditions. Their
// indexes here are used by featurePreconditions to assign preconditions to
// tests by feature.
var preconditionDefs = []precondDef{
	{"User account exists", "Manual"},
	{"User is logged in", "Manual"},
	{"Email service is available", "Manual"},
	{"MFA device enrolled", "Manual"},
	{"Search index is populated", "Manual"},
	{"Cart has items", "Manual"},
	{"Payment method on file", "Manual"},
	{"Product catalog is loaded", "Manual"},
	{"Completed order exists", "Manual"},
	{"Admin user is logged in", "Manual"},
	{"At least one report exists", "Manual"},
	{"Database has seed data", "Manual"},
	{"Network is available", "Manual"},
	{"File system has write access", "Manual"},
	{"Multiple users are logged in", "Manual"},
}

// featurePreconditions maps each feature in demoFeatures to indexes into
// preconditionDefs. Tests inherit these preconditions based on their feature.
var featurePreconditions = map[string][]int{
	"Login":             {0},
	"Logout":            {1},
	"User registration": {2},
	"Password reset":    {0, 2},
	"Multi-factor auth": {0, 3},
	"Session timeout":   {1},
	"Search":            {4},
	"Filter results":    {4},
	"Sort results":      {4},
	"Pagination":        {4},
	"Checkout":          {5, 6},
	"Cart":              {1},
	"Add to cart":       {7},
	"Remove from cart":  {5},
	"Profile update":    {1},
	"Settings":          {1},
	"Dashboard":         {1},
	"Notifications":     {1},
	"Payment":           {6},
	"Refund":            {8},
	"Reports":           {1, 10},
	"Export to CSV":     {1, 10},
	"Import data":       {9, 11},
	"Admin console":     {9},
	"Permissions":       {9},
	"Audit log":         {1, 9},
	"API rate limit":    {12},
	"File upload":       {1, 13},
	"File download":     {1, 13},
	"Bulk operations":   {9, 14},
}

// demoTransitionsByStatus is the demo workflow: from each status, which
// transitions are available and where they lead. Every active status has a
// "Deprecate" path so the FR-2.4 deprecate-instead-of-delete flow can be
// exercised end-to-end without a real Jira.
var demoTransitionsByStatus = map[string][]Transition{
	"Open": {
		{ID: "11", Name: "Start Progress", To: "In Progress"},
		{ID: "12", Name: "Approve", To: "Approved"},
		{ID: "13", Name: "Deprecate", To: "Deprecated"},
	},
	"In Progress": {
		{ID: "21", Name: "Mark Done", To: "Done"},
		{ID: "22", Name: "Block", To: "Blocked"},
		{ID: "23", Name: "Approve", To: "Approved"},
	},
	"Approved": {
		{ID: "31", Name: "Mark Done", To: "Done"},
		{ID: "32", Name: "Deprecate", To: "Deprecated"},
	},
	"Done": {
		{ID: "41", Name: "Reopen", To: "Open"},
		{ID: "42", Name: "Deprecate", To: "Deprecated"},
	},
	"Blocked": {
		{ID: "51", Name: "Unblock", To: "In Progress"},
		{ID: "52", Name: "Deprecate", To: "Deprecated"},
	},
	"Deprecated": {
		{ID: "61", Name: "Reactivate", To: "Open"},
	},
}

func demoTransitionsForStatus(status string) []Transition {
	if ts, ok := demoTransitionsByStatus[status]; ok {
		// Return a copy so callers can't accidentally mutate the source.
		out := make([]Transition, len(ts))
		copy(out, ts)
		return out
	}
	return []Transition{}
}

// demoKeyIndex extracts the 0-based index encoded in a demo Test key
// ("PROJ-<i+1>"), or -1 if it doesn't parse.
func demoKeyIndex(testKey string) int {
	dash := strings.LastIndex(testKey, "-")
	if dash < 0 {
		return -1
	}
	n, err := strconv.Atoi(testKey[dash+1:])
	if err != nil {
		return -1
	}
	return n - 1
}

// demoCallGraph maps a caller's numeric key suffix to the callee's numeric
// suffix for the seeded demo call graph. Both keys share the caller's project
// prefix. Numbers are well within demoTestCount and avoid the duplicate-cluster
// indices (0..3, i.e. numbers 1..4). 6 and 8 both call 7 (shared callee); 9
// calls 10 (separate pair).
var demoCallGraph = map[int]int{
	6: 7,
	8: 7,
	9: 10,
}

// demoCalledSibling returns the called Test key for a caller in the demo call
// graph, or "" if the given key is not a seeded caller. The returned key reuses
// the caller's project prefix so the call stays within the same project, and
// the lookup is deterministic (same input -> same output), which is what makes
// a SyncTestCalls re-pull stable.
func demoCalledSibling(testKey string) string {
	dash := strings.LastIndex(testKey, "-")
	if dash < 0 {
		return ""
	}
	num, err := strconv.Atoi(testKey[dash+1:])
	if err != nil {
		return ""
	}
	callee, ok := demoCallGraph[num]
	if !ok {
		return ""
	}
	return testKey[:dash+1] + strconv.Itoa(callee)
}

// demoStepsForKey returns a deterministic step list for any test in demo mode
// (FR-2.5). For the PKCS theme it returns PKCS-flavoured steps derived from
// the test's feature; for the generic theme it returns the original three-step
// skeleton (plus duplicate-cluster and call-graph overrides).
func demoStepsForKey(theme demoTheme, testKey string) []Step {
	if theme.Variant == "pkcs" {
		// Derive the feature from the test key index so the steps are fully
		// deterministic (same input → same output).
		idx := demoKeyIndex(testKey)
		if idx < 0 {
			idx = 0
		}
		feature := theme.Features[idx%len(theme.Features)]
		// The initialisation call follows the C_<Fn>Init naming convention for
		// C_Sign and C_Verify; C_GenerateKeyPair has no separate init.
		initFunc := feature + "Init"
		if feature == "C_GenerateKeyPair" {
			initFunc = feature
		}
		return []Step{
			{
				ID:       testKey + "-p1",
				Index:    1,
				Action:   "Open a session (C_OpenSession)",
				Expected: "CKR_OK; a valid session handle is returned",
			},
			{
				ID:       testKey + "-p2",
				Index:    2,
				Action:   "Initialize the operation (" + initFunc + ")",
				Expected: "CKR_OK; the operation is ready",
			},
			{
				ID:       testKey + "-p3",
				Index:    3,
				Action:   "Invoke " + feature,
				Expected: "Return code is CKR_OK or the expected CKR_* error",
			},
			{
				ID:       testKey + "-p4",
				Index:    4,
				Action:   "Inspect the CKR_* return code",
				Expected: "Return code matches the test condition",
			},
		}
	}

	// Duplicate-cluster step overrides (see makeDemoTest). Match the numeric
	// suffix so the override is project-key agnostic.
	switch demoKeyIndex(testKey) {
	case 0, 1:
		// Cluster A — identical steps for both members.
		return []Step{
			{ID: "dup-a-1", Index: 1, Action: "Open the login page", Data: "", Expected: "Login form is shown"},
			{ID: "dup-a-2", Index: 2, Action: "Submit valid credentials", Data: "user / pass", Expected: "User is logged in"},
		}
	case 2:
		return []Step{
			{ID: "dup-b-3", Index: 1, Action: "Open the cart", Data: "", Expected: "Cart is shown"},
			{ID: "dup-b-3b", Index: 2, Action: "Click checkout", Data: "", Expected: "Checkout starts"},
		}
	case 3:
		// Cluster B — same summary as index 2 but DIFFERENT steps.
		return []Step{
			{ID: "dup-b-4", Index: 1, Action: "Open the cart from the menu", Data: "", Expected: "Cart page loads"},
			{ID: "dup-b-4b", Index: 2, Action: "Proceed to payment", Data: "visa", Expected: "Payment screen opens"},
		}
	}

	// Deterministic demo call graph so the Test Calls view is non-empty in
	// demo mode and a SyncTestCalls re-pull is stable (FR-2.5). A few
	// non-duplicate tests (numbers 6, 8, 9) get a "call test" step pointing at
	// a SIBLING test in the SAME project. The called key reuses the caller's
	// project prefix, so this works for any demo project key (DEMO-6 -> DEMO-7,
	// QA-6 -> QA-7, ...). Numbers 6 and 8 both call 7 (a shared callee with two
	// callers); 9 calls 10 (a separate caller/callee pair). This must stay
	// deterministic: SyncTestCalls re-pulls via demoStepsForKey, and identical
	// output on re-pull is exactly what keeps the graph from being wiped.
	if callee := demoCalledSibling(testKey); callee != "" {
		return []Step{
			{
				ID:       testKey + "-s1",
				Index:    1,
				Action:   "Set up the preconditions described in the test description.",
				Expected: "All preconditions are met and the system is in a known state.",
			},
			{
				ID:            testKey + "-call",
				Index:         2,
				Action:        "Call test " + callee,
				CalledTestKey: callee,
			},
		}
	}

	return []Step{
		{
			ID:       testKey + "-s1",
			Index:    1,
			Action:   "Set up the preconditions described in the test description.",
			Expected: "All preconditions are met and the system is in a known state.",
		},
		{
			ID:       testKey + "-s2",
			Index:    2,
			Action:   "Execute the action under test.",
			Expected: "The action completes without error.",
		},
		{
			ID:       testKey + "-s3",
			Index:    3,
			Action:   "Verify the resulting system state matches the test's expected behaviour.",
			Expected: "The observed outcome equals the expected outcome.",
		},
	}
}

// demoContainerStatuses / demoExecStatuses drive the issue status shown for
// generated Test Sets, Plans and Executions.
var demoContainerStatuses = []string{"Open", "In Progress", "Done"}
var demoExecStatuses = []string{"In Progress", "Done", "Open"}

// demoRunStatuses is the weighted Test Run result vocabulary for execution
// memberships — mostly passing, some failing / not-yet-run.
var demoRunStatuses = []string{
	"PASS", "PASS", "PASS", "PASS",
	"FAIL", "FAIL",
	"TODO", "TODO", "TODO",
	"EXECUTING",
	"ABORTED",
	"BLOCKED",
}

// demoExecTimestamps returns deterministic ISO-8601 created, updated, and
// resolved timestamps for the i-th demo Test Execution. The dates are fixed
// (not derived from time.Now) so demo mode is fully deterministic across
// calls. Resolved is non-empty only for executions whose seeded status is
// "Done" (every third execution starting at index 1), matching the pattern in
// demoExecStatuses. All other executions get an empty resolved string.
func demoExecTimestamps(idx int) (created, updated, resolved string) {
	// Base date: 2026-01-01. Each exec is spaced 7 days apart for created, and
	// 14 days apart for updated, so the history breakdown shows distinct dates.
	base := 1735689600 // 2026-01-01T00:00:00Z in Unix seconds (fixed)
	_ = base           // actual formatting uses the offset arithmetic below
	day := idx * 7
	created = fmt.Sprintf("2026-01-%02dT09:00:00Z", 1+day%28)
	updated = fmt.Sprintf("2026-01-%02dT17:00:00Z", 1+(day+14)%28)
	// demoExecStatuses cycles ["In Progress", "Done", "Open"]; "Done" is at i%3==1.
	if idx%3 == 1 {
		resolved = fmt.Sprintf("2026-01-%02dT18:00:00Z", 1+(day+14)%28)
	}
	return created, updated, resolved
}

// demoEnvironments returns a deterministic, non-empty subset of the environment
// pool for the i-th execution, cycling subsets so different executions show
// different environment chips.
func demoEnvironments(i int) []string {
	// Six deterministic subsets cycled by index; each is non-empty so every demo
	// execution shows at least one chip, and the filter has something to narrow.
	subsets := [][]string{
		{"Staging"},
		{"Prod"},
		{"Staging", "Chrome"},
		{"Prod", "Android"},
		{"Chrome"},
		{"Staging", "Prod", "Chrome", "Android"},
	}
	return subsets[i%len(subsets)]
}

// demoFixVersions returns a deterministic Jira Fix Version(s) set for the i-th
// execution, cycling a few small subsets so the read-only chips show offline.
// Test Sets / Plans are left empty (they carry no Fix Version field here).
func demoFixVersions(i int) []string {
	subsets := [][]string{
		{"1.5.0"},
		{"1.6.0"},
		{"1.5.0", "1.6.0"},
	}
	return subsets[i%len(subsets)]
}

// demoExecDescription returns a deterministic Markdown description for a demo
// Test Execution identified by key. The text uses headings and bullet points so
// the frontend markdown renderer has content to display.
func demoExecDescription(key string) string {
	return fmt.Sprintf("## %s\n\n**Scope:** Regression suite for this cycle.\n\n- Covers authentication, checkout, and API rate-limit paths\n- Environment: see execution header\n- Linked to release milestone via Fix Versions", key)
}

// demoLinkedTests caps how many of the low-numbered demo Tests get container
// memberships, keeping the demo link table small while still giving the most
// commonly-opened Tests (DEMO-1…) sets, plans and executions to display.
const demoLinkedTests = 200

// demoContainersAndLinks generates Test Sets (one per Test Repository
// category), Test Plans and Test Executions plus their Test memberships
// (FR-1.3). Execution memberships carry a deterministic run status so the
// coverage view has data to chart.
func demoContainersAndLinks(projectKey string) ([]Container, []ContainerLink, error) {
	if projectKey == "" {
		projectKey = "DEMO"
	}
	containers := make([]Container, 0)
	links := make([]ContainerLink, 0)

	setKeys := make([]string, len(demoFolderCategories))
	for i, cat := range demoFolderCategories {
		key := fmt.Sprintf("%s-TS-%d", projectKey, i+1)
		setKeys[i] = key
		containers = append(containers, Container{
			Key:     key,
			Kind:    KindTestSet,
			Summary: cat.Name + " test set",
			Status:  demoContainerStatuses[i%len(demoContainerStatuses)],
		})
	}

	const planCount = 5
	planKeys := make([]string, planCount)
	for i := 0; i < planCount; i++ {
		key := fmt.Sprintf("%s-TP-%d", projectKey, i+1)
		planKeys[i] = key
		containers = append(containers, Container{
			Key:     key,
			Kind:    KindTestPlan,
			Summary: fmt.Sprintf("Release %d.0 test plan", i+1),
			Status:  demoContainerStatuses[i%len(demoContainerStatuses)],
		})
	}

	const execCount = 8
	execKeys := make([]string, execCount)
	for i := 0; i < execCount; i++ {
		key := fmt.Sprintf("%s-TE-%d", projectKey, i+1)
		execKeys[i] = key
		created, updated, resolved := demoExecTimestamps(i)
		containers = append(containers, Container{
			Key:          key,
			Kind:         KindTestExec,
			Summary:      fmt.Sprintf("Cycle %d execution", i+1),
			Status:       demoExecStatuses[i%len(demoExecStatuses)],
			Environments: demoEnvironments(i),
			FixVersions:  demoFixVersions(i),
			Created:      created,
			Updated:      updated,
			Resolved:     resolved,
			Description:  demoExecDescription(key),
		})
	}

	// Cross-project executions (auto-discovered, #4): two Test Executions that
	// live in a DIFFERENT Jira project but run this project's tests — exactly the
	// case the traceability Sankey's "cross-project only" filter surfaces. In live
	// mode these come from Xray's per-test executions lookup; here they're seeded
	// so the feature is exercisable offline.
	const crossProject = demoCrossProjectKey
	crossExecKeys := []string{crossProject + "-TE-1", crossProject + "-TE-2"}
	for i, key := range crossExecKeys {
		created, updated, resolved := demoExecTimestamps(execCount + i)
		containers = append(containers, Container{
			Key:          key,
			Kind:         KindTestExec,
			Summary:      fmt.Sprintf("%s integration cycle %d", crossProject, i+1),
			Status:       demoExecStatuses[i%len(demoExecStatuses)],
			Environments: demoEnvironments(execCount + i),
			FixVersions:  demoFixVersions(execCount + i),
			Created:      created,
			Updated:      updated,
			Resolved:     resolved,
			Description:  demoExecDescription(key),
		})
	}

	// Cross-project member execution (#219): a Test Execution that lives in this
	// project but whose member Tests live in the XRAYINT project. Those member
	// Tests are NOT in this project's test_case set, so they only render on the
	// board via the external_test cache (populated by the sync's missing-keys pass
	// calling ListTestsBasic, which returns basics for XRAYINT-* keys below).
	xprojExecKey := fmt.Sprintf("%s-TE-XPROJ", projectKey)
	xprojCreated, xprojUpdated, xprojResolved := demoExecTimestamps(execCount + len(crossExecKeys))
	containers = append(containers, Container{
		Key:          xprojExecKey,
		Kind:         KindTestExec,
		Summary:      "Cross-project integration cycle",
		Status:       demoExecStatuses[0],
		Environments: demoEnvironments(0),
		FixVersions:  demoFixVersions(0),
		Created:      xprojCreated,
		Updated:      xprojUpdated,
		Resolved:     xprojResolved,
		Description:  demoExecDescription(xprojExecKey),
	})
	for i := 1; i <= demoExternalMembers; i++ {
		links = append(links, ContainerLink{
			ContainerKey: xprojExecKey,
			TestKey:      fmt.Sprintf("%s-%d", crossProject, i),
			RunStatus:    demoRunStatuses[i%len(demoRunStatuses)],
		})
	}

	// Sub-task Test Executions: a couple of executions that are Jira sub-tasks of
	// a parent issue (a Story here), exercising the parent-linked execution path
	// offline. They are still Kind=testexec and behave like standalone ones.
	const subExecCount = 2
	subExecKeys := make([]string, subExecCount)
	subExecOffset := execCount + len(crossExecKeys) + 1 // +1 for the xproj exec
	for i := 0; i < subExecCount; i++ {
		key := fmt.Sprintf("%s-STE-%d", projectKey, i+1)
		subExecKeys[i] = key
		created, updated, resolved := demoExecTimestamps(subExecOffset + i)
		containers = append(containers, Container{
			Key:           key,
			Kind:          KindTestExec,
			Summary:       fmt.Sprintf("Sub-execution for story %d", i+1),
			Status:        demoExecStatuses[i%len(demoExecStatuses)],
			ParentKey:     fmt.Sprintf("%s-S-%d", projectKey, i+1),
			ParentSummary: fmt.Sprintf("Story %d", i+1),
			IssueType:     "Sub Test Execution",
			Environments:  demoEnvironments(subExecOffset + i),
			FixVersions:   demoFixVersions(subExecOffset + i),
			Created:       created,
			Updated:       updated,
			Resolved:      resolved,
			Description:   demoExecDescription(key),
		})
	}

	for i := 0; i < demoLinkedTests && i < demoTestCount; i++ {
		testKey := fmt.Sprintf("%s-%d", projectKey, i+1)
		feature := demoFeatures[i%len(demoFeatures)]
		if catIdx := demoCategoryIndexForFeature(genericTheme, feature); catIdx >= 0 {
			links = append(links, ContainerLink{ContainerKey: setKeys[catIdx], TestKey: testKey})
		}
		links = append(links, ContainerLink{ContainerKey: planKeys[i%planCount], TestKey: testKey})
		links = append(links, ContainerLink{
			ContainerKey: execKeys[i%execCount],
			TestKey:      testKey,
			RunStatus:    demoRunStatuses[i%len(demoRunStatuses)],
		})
		// Every 7th linked test is also run in a cross-project execution.
		if i%7 == 0 {
			links = append(links, ContainerLink{
				ContainerKey: crossExecKeys[(i/7)%len(crossExecKeys)],
				TestKey:      testKey,
				RunStatus:    demoRunStatuses[(i+2)%len(demoRunStatuses)],
			})
		}
		// Every 5th linked test also runs in a sub-task execution.
		if i%5 == 0 {
			links = append(links, ContainerLink{
				ContainerKey: subExecKeys[(i/5)%len(subExecKeys)],
				TestKey:      testKey,
				RunStatus:    demoRunStatuses[(i+1)%len(demoRunStatuses)],
			})
		}
	}

	return containers, links, nil
}

// demoCategoryIndexForFeature returns the index of the Test Repository category
// holding a feature, or -1 if unmapped.
func demoCategoryIndexForFeature(theme demoTheme, feature string) int {
	for i, cat := range theme.Categories {
		for _, f := range cat.Features {
			if f == feature {
				return i
			}
		}
	}
	return -1
}

// demoPreconditionsAndLinks returns the demo precondition master list plus
// the test-key → precondition-keys mapping. Keys use a "<project>-P-N"
// convention so they read like Jira keys without colliding with the test
// number range.
func demoPreconditionsAndLinks(projectKey string) ([]Precondition, map[string][]string, error) {
	if projectKey == "" {
		projectKey = "DEMO"
	}

	preconditions := make([]Precondition, 0, len(preconditionDefs))
	for i, def := range preconditionDefs {
		preconditions = append(preconditions, Precondition{
			Key:         fmt.Sprintf("%s-P-%d", projectKey, i+1),
			Summary:     def.Summary,
			Type:        def.Type,
			Description: fmt.Sprintf("(Demo precondition: %s)", def.Summary),
		})
	}

	links := make(map[string][]string, demoTestCount)
	for i := 0; i < demoTestCount; i++ {
		feature := demoFeatures[i%len(demoFeatures)]
		indexes, ok := featurePreconditions[feature]
		if !ok || len(indexes) == 0 {
			continue
		}
		testKey := fmt.Sprintf("%s-%d", projectKey, i+1)
		keys := make([]string, len(indexes))
		for j, idx := range indexes {
			keys[j] = fmt.Sprintf("%s-P-%d", projectKey, idx+1)
		}
		links[testKey] = keys
	}

	return preconditions, links, nil
}

// makeDemoTest builds a deterministic Test for index i, so repeated syncs of
// a demo profile are idempotent.
// demoTestForKey returns the deterministic demo Test for a "PROJ-N" key, so the
// remote-fetch (GetTestFields) has something to return offline. Unparseable keys
// yield the first demo Test.
// demoExternalMembers is how many XRAYINT-* member Tests the demo cross-project
// execution carries — enough to exercise the external-member board path offline.
const demoExternalMembers = 4

// demoCrossProjectKey is the project the demo cross-project member Tests live in
// (distinct from the profile's project), used by both the container seed and the
// ListTestsBasic demo path so they agree on the foreign member keys.
const demoCrossProjectKey = "XRAYINT"

// demoCrossProjectSubExecKey is the key of the cross-project sub-task Test
// Execution seeded for the per-test discovery path. It lives in the
// demoCrossProjectKey (XRAYINT) project, which is a different project from the
// profile's test project (e.g. DEMO). This execution is NOT returned by
// demoContainersAndLinks("DEMO") -- it is discoverable only via
// TestExecutionsForTest, matching the live scenario where a sub-task exec in
// another project runs this project's tests but is invisible to the project-
// scoped container search.
//
// Member rule: DEMO test at index i (0-based) is a member when i%11 == 0.
// This is distinct from the 5th (sub-task) and 7th (cross-project TE) rules
// already in demoContainersAndLinks so the three populations do not collide,
// keeping test counts deterministic and easy to verify.
const demoCrossProjectSubExecKey = demoCrossProjectKey + "-STE-1"

// demoCrossProjectSubExecMember reports whether the DEMO test at 0-based
// index i is a member of the cross-project sub-task execution.
func demoCrossProjectSubExecMember(i int) bool { return i%11 == 0 }

// demoCrossProjectSubExec is the Container descriptor for the cross-project
// sub-task Test Execution seeded by the per-test discovery path. It is a
// Kind=testexec sub-task (IssueType "Sub Test Execution") whose parent is a
// story in the XRAYINT project, and which runs some of the profile project's
// tests.
func demoCrossProjectSubExec() Container {
	// Use a fixed index (20) so the timestamps are stable across calls.
	created, updated, resolved := demoExecTimestamps(20)
	return Container{
		Key:           demoCrossProjectSubExecKey,
		Kind:          KindTestExec,
		Summary:       "Cross-project sub-execution",
		Status:        "In Progress",
		ParentKey:     demoCrossProjectKey + "-S-1",
		ParentSummary: "Cross-project story 1",
		IssueType:     "Sub Test Execution",
		Environments:  []string{"Staging"},
		FixVersions:   []string{"1.5.0"},
		Created:       created,
		Updated:       updated,
		Resolved:      resolved,
		Description:   demoExecDescription(demoCrossProjectSubExecKey),
	}
}

// demoTestExecutionsForTest returns the Containers and ContainerLinks that
// TestExecutionsForTest should return for a given test key in demo mode.
// Only tests that match the demoCrossProjectSubExecMember rule (i%11 == 0)
// are members of the cross-project sub-task execution; all others return empty.
//
// The project key is inferred from the test key prefix so the rule works for
// any demo project (DEMO-11, QA-11, etc.).
func demoTestExecutionsForTest(testKey string) ([]Container, []ContainerLink) {
	idx := demoKeyIndex(testKey)
	if idx < 0 || !demoCrossProjectSubExecMember(idx) {
		return []Container{}, []ContainerLink{}
	}
	exec := demoCrossProjectSubExec()
	link := ContainerLink{
		ContainerKey: demoCrossProjectSubExecKey,
		TestKey:      testKey,
		RunStatus:    demoRunStatuses[(idx+3)%len(demoRunStatuses)],
	}
	return []Container{exec}, []ContainerLink{link}
}

// demoCrossProjectBug is the demo defect reached only through a cross-project
// member Test of the demo *-TE-XPROJ execution (#219). It lives in a defect
// project (distinct from the foreign member's project) and is linked to no
// in-project Test, so it never appears via the normal syncBugs path and is
// surfaced for the execution only because the harvest walks the foreign member's
// issue links. This is exactly the reported case the harvest fixes.
var demoCrossProjectBug = Bug{
	Key:        "BUGS-219",
	ProjectKey: demoBugProject,
	IssueType:  "Bug",
	Summary:    "Cross-project integration login fails",
	Status:     "Open",
	Priority:   "High",
}

// demoExternalStatuses cycles workflow statuses for the seeded external member
// Tests so the board shows a mix.
var demoExternalStatuses = []string{"Approved", "In Progress", "Draft", "Done"}

// demoTestBasicForKey returns the deterministic basics for a Test key, used by
// the ListTestsBasic demo path so the sync can cache cross-project (XRAYINT-*)
// execution members offline. The project key is parsed from the issue key.
func demoTestBasicForKey(theme demoTheme, key string) TestBasic {
	projectKey, idx := "DEMO", 0
	if i := strings.LastIndex(key, "-"); i > 0 {
		projectKey = key[:i]
		if n, err := strconv.Atoi(key[i+1:]); err == nil && n > 0 {
			idx = n - 1
		}
	}
	feature := theme.Features[idx%len(theme.Features)]
	condition := theme.Conditions[(idx/len(theme.Features))%len(theme.Conditions)]
	tb := TestBasic{
		Key:        key,
		Summary:    fmt.Sprintf("%s %s", feature, condition),
		Status:     demoExternalStatuses[idx%len(demoExternalStatuses)],
		ProjectKey: projectKey,
	}
	// The first XRAYINT-* cross-project member carries a bug link, so the
	// container bug harvest is exercised offline: the linked defect lives in the
	// BUGS defect project (see demoCrossProjectBug) and reaches an execution only
	// through this foreign member (#219).
	if projectKey == demoCrossProjectKey && idx == 0 {
		tb.IssueLinks = []BugLinkRef{{
			Key:        demoCrossProjectBug.Key,
			IssueType:  demoCrossProjectBug.IssueType,
			LinkID:     "xbl-1",
			ProjectKey: demoCrossProjectBug.ProjectKey,
			Summary:    demoCrossProjectBug.Summary,
			Status:     demoCrossProjectBug.Status,
			Priority:   demoCrossProjectBug.Priority,
		}}
	}
	return tb
}

func demoTestForKey(theme demoTheme, key string) Test {
	projectKey, idx := "DEMO", 0
	if i := strings.LastIndex(key, "-"); i > 0 {
		projectKey = key[:i]
		if n, err := strconv.Atoi(key[i+1:]); err == nil && n > 0 {
			idx = n - 1
		}
	}
	return makeDemoTest(theme, projectKey, idx)
}

func makeDemoTest(theme demoTheme, projectKey string, i int) Test {
	feature := theme.Features[i%len(theme.Features)]
	condition := theme.Conditions[(i/len(theme.Features))%len(theme.Conditions)]
	status := demoStatuses[i%len(demoStatuses)]
	priority := demoPriorities[(i*7+3)%len(demoPriorities)]

	// 1–3 labels, derived from i so the same Test always carries the same
	// labels. Duplicates collapse to a unique set.
	labelCount := (i % 3) + 1
	seen := make(map[string]struct{}, labelCount)
	labels := make([]string, 0, labelCount)
	for j := 0; j < labelCount; j++ {
		l := demoLabels[(i*(j+1)+11)%len(demoLabels)]
		if _, dup := seen[l]; dup {
			continue
		}
		seen[l] = struct{}{}
		labels = append(labels, l)
	}

	updated := time.Now().AddDate(0, 0, -(i % 365)).
		Format("2006-01-02T15:04:05.000-0700")

	summary := fmt.Sprintf("%s %s", feature, condition)
	// Seed two deterministic duplicate clusters for the Duplicates view demo
	// (generic theme only — PKCS tests keep their "<feature> <condition>"
	// summary so the PKCS corpus is fully themed):
	// indices 0,1 -> identical summary + identical steps; 2,3 -> identical
	// summary + differing steps.
	if theme.Variant == "" {
		switch i {
		case 0, 1:
			summary = "Duplicate demo A — user can log in"
		case 2, 3:
			summary = "Duplicate demo B — user can check out"
		}
	}
	description := fmt.Sprintf(
		"Given a user is on the %s screen\n"+
			"When they perform the action %s\n"+
			"Then the system should respond correctly\n\n"+
			"(Demo data — generated for UI testing.)",
		strings.ToLower(feature), condition)

	return Test{
		Key:         fmt.Sprintf("%s-%d", projectKey, i+1),
		ID:          fmt.Sprintf("%d", 10000+i),
		Summary:     summary,
		Description: description,
		Status:      status,
		Priority:    priority,
		Labels:      labels,
		Components:  demoComponentsForIndex(i),
		Updated:     updated,
		FolderID:    demoFolderForFeature(theme, feature),
		ExecType:    demoExecTypeForIndex(i),
		FixVersions: demoTestFixVersionsForIndex(i),
	}
}

// demoExecTypes covers all four execution-type filter options so demo data
// exercises each one. Independent of the Test Type custom field value set.
var demoExecTypes = []string{"Manual", "Automated", "Generic", "Cucumber"}

// demoExecTypeForIndex assigns a deterministic Xray Test Type (execution type)
// to a demo test by index, so repeated syncs are stable. It cycles through
// demoExecTypes, covering all four frontend filter options so demo data
// exercises each one (including "Automated", which is not in the Test Type
// custom field value set).
func demoExecTypeForIndex(i int) string {
	return demoExecTypes[i%len(demoExecTypes)]
}

// demoTestFixVersionPool is the small set of Fix Version names used for demo
// Test issues. Indices cycle deterministically so members across an execution
// genuinely differ, exercising the per-member fix-version column in the UI.
var demoTestFixVersionPool = []string{"1.5.0", "1.6.0", "1.7.0"}

// demoTestFixVersionsForIndex returns a deterministic 0-2 Fix Version set for
// a demo Test by index. About a third of tests carry no fix version, another
// third carry one, and the remainder carry two (always distinct). The result
// is derived purely from i, so repeated syncs are idempotent.
func demoTestFixVersionsForIndex(i int) []string {
	switch i % 3 {
	case 0:
		// No fix version on this test.
		return nil
	case 1:
		// One fix version.
		return []string{demoTestFixVersionPool[i%len(demoTestFixVersionPool)]}
	default:
		// Two distinct fix versions.
		first := demoTestFixVersionPool[i%len(demoTestFixVersionPool)]
		second := demoTestFixVersionPool[(i+1)%len(demoTestFixVersionPool)]
		if second == first {
			second = demoTestFixVersionPool[(i+2)%len(demoTestFixVersionPool)]
		}
		return []string{first, second}
	}
}

// demoComponentNames is the demo Jira components vocabulary (the multi-valued
// issue field, distinct from the "Component" custom field in customfields.go).
// Names deliberately include spaces ("User Management") so the grouping /
// filtering path is exercised against multi-word component names.
var demoComponentNames = []string{
	"Frontend", "Backend", "API", "Database",
	"Authentication", "Payments", "Reporting",
	"User Management", "Infrastructure", "Mobile",
}

// demoComponentsForIndex assigns a deterministic 1–2 component set to a demo
// test so the same test always carries the same components.
func demoComponentsForIndex(i int) []string {
	first := demoComponentNames[i%len(demoComponentNames)]
	if i%3 == 0 {
		// Roughly a third of tests get a second, distinct component.
		second := demoComponentNames[(i*7+3)%len(demoComponentNames)]
		if second != first {
			return []string{first, second}
		}
	}
	return []string{first}
}
