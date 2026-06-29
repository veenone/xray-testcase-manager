# demo-pkcs Backend Variant Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a `demo-pkcs` profile serve a PKCS#11-themed dataset across every view (Browse, Requirements, Preconditions, Test Sets/Plans/Executions, Steps, Folders) — so a normal Sync produces PKCS data, with the coverage layer (parameter models/versions/CRs) on top.

**Architecture:** The demo Jira backend's flavor comes from five package vars (`demoFeatures`, `demoConditions`, `demoFolderCategories`, `preconditionDefs`, `featurePreconditions`). We bundle these into a `demoTheme` value, add `themeFor(baseURL)` that returns a PKCS theme for `demo-pkcs` and the generic theme otherwise, and thread `theme` through the ~7 generators that read the vocab + their call sites. The structural machinery (counts, links, run statuses, executions, determinism) is unchanged. The coverage seed is adapted to map to the *synced* PKCS tests/requirements by feature name.

**Tech Stack:** Go 1.25, SQLite (modernc), Wails v2, React/TS.

## Global Constraints

- The generic demo dataset must be byte-for-byte unchanged: the default theme (`themeFor` for non-`demo-pkcs` URLs) returns the existing vocabulary, so every existing `internal/jira` test stays green. (verbatim requirement)
- `demoVariant(baseURL)` returns `"pkcs"` only for a `demo-pkcs` / `demo-pkcs:` URL (case-insensitive, trimmed); everything else returns `""` (generic). `isDemoURL` already matches the `demo-` prefix (do not change it).
- Determinism is sacred: every generator stays a pure function of its inputs (no time/random in IDs) so re-sync is stable. Existing tests assert this.
- PKCS theme uses exactly three features: `C_Sign`, `C_GenerateKeyPair`, `C_Verify`. Test summaries are `"<feature> <condition>"`, e.g. `"C_Sign with RSA-2048"`.
- Peripheral demo features (bugs, custom fields, cross-project/sub-task executions, transitions) stay generic under `demo-pkcs` — out of scope.
- Run from repo root `C:/projects/xray-test-manager`. Go tests: `go test ./internal/jira/ ./internal/coverage/`. Build: `go build ./...`. Frontend: `cd frontend && npx tsc --noEmit`.
- Work on branch `feature/pkcs-reference-dataset` (PR #42).

---

## File Structure

- `internal/jira/demo_theme.go` — **new.** `folderCategory` / `precondDef` named types, the `demoTheme` struct, `genericTheme`, `pkcsTheme`, `demoVariant`, `themeFor`.
- `internal/jira/demo.go` — change `demoFolderCategories` / `preconditionDefs` to use the named types; thread `theme` into `makeDemoTest`, `demoTestsPage`, `demoTestForKey`, `demoTestBasicForKey`, `demoStepsForKey`, `demoFolders`, `demoFolderForFeature`, `demoCategoryIndexForFeature`.
- `internal/jira/requirements.go` — thread `theme` into `demoRequirements`; PKCS branch emits functional + per-customer requirements.
- `internal/jira/preconditions.go`, `internal/jira/containers.go` — thread `theme` into `demoPreconditionsAndLinks`, `demoContainersAndLinks`.
- `internal/jira/{search,steps,meta,folders,requirements,preconditions,containers}.go` — call sites pass `themeFor(c.baseURL)`.
- `internal/jira/demo_test.go`, `demo_duplicates_test.go` — update call sites to pass `genericTheme`; add PKCS-theme assertions.
- `internal/coverage/demopkcs.go` — adapt `SeedPKCSReference` to map to *synced* PKCS data (query by feature name) instead of writing its own tests/requirements.
- `frontend/src/components/CoverageView.tsx` — relabel the button to "Load PKCS#11 coverage" and update its notice copy.
- `CHANGELOG.md` — entry.

---

### Task 1: Theme abstraction, selector, and PKCS vocabulary

**Files:**
- Create: `internal/jira/demo_theme.go`
- Modify: `internal/jira/demo.go` (change `demoFolderCategories` and `preconditionDefs` to named types)
- Test: `internal/jira/demo_theme_test.go`

**Interfaces:**
- Produces:
  - `type folderCategory struct { Name string; Features []string }`
  - `type precondDef struct { Summary, Type string }`
  - `type demoTheme struct { Variant string; Features []string; Conditions []string; Categories []folderCategory; Preconditions []precondDef; FeaturePre map[string][]int; TestCount int }`
  - `func demoVariant(baseURL string) string`
  - `func themeFor(baseURL string) demoTheme`
  - `genericTheme demoTheme`, `pkcsTheme demoTheme`

- [ ] **Step 1: Write the failing test** — `internal/jira/demo_theme_test.go`:

```go
package jira

import "testing"

func TestThemeForSelectsVariant(t *testing.T) {
	if themeFor("demo").Variant != "" {
		t.Errorf("plain demo should be the generic theme")
	}
	if themeFor("https://jira.example.com").Variant != "" {
		t.Errorf("real URL should be the generic theme")
	}
	pk := themeFor("demo-pkcs")
	if pk.Variant != "pkcs" {
		t.Fatalf("demo-pkcs Variant = %q, want pkcs", pk.Variant)
	}
	if len(pk.Features) != 3 || pk.Features[0] != "C_Sign" {
		t.Errorf("pkcs features = %v, want [C_Sign C_GenerateKeyPair C_Verify]", pk.Features)
	}
	// Generic theme still exposes the original 30 features.
	if len(themeFor("demo").Features) < 20 {
		t.Errorf("generic theme lost its features: %d", len(themeFor("demo").Features))
	}
	// Every PKCS feature maps to a folder category and to preconditions.
	for _, f := range pk.Features {
		if _, ok := pk.FeaturePre[f]; !ok {
			t.Errorf("pkcs feature %q has no preconditions mapping", f)
		}
	}
}
```

- [ ] **Step 2: Run to verify fail**

Run: `go test ./internal/jira/ -run TestThemeForSelectsVariant -v`
Expected: FAIL (undefined `themeFor`).

- [ ] **Step 3: Introduce named types in demo.go**

In `internal/jira/demo.go`, change the two anonymous-struct vars to use named types (defined in the new file). Replace:

```go
var demoFolderCategories = []struct {
	Name     string
	Features []string
}{
```
with `var demoFolderCategories = []folderCategory{` and replace
```go
var preconditionDefs = []struct {
	Summary string
	Type    string
}{
```
with `var preconditionDefs = []precondDef{`. (The literal contents are unchanged.)

- [ ] **Step 4: Write `demo_theme.go`**

```go
package jira

import "strings"

// folderCategory groups demo features into a Test Repository category.
type folderCategory struct {
	Name     string
	Features []string
}

// precondDef is a distinct demo precondition.
type precondDef struct {
	Summary string
	Type    string
}

// demoTheme bundles the vocabulary that gives a demo dataset its flavour. The
// structural generators stay the same; only this vocabulary changes per variant.
type demoTheme struct {
	Variant       string
	Features      []string
	Conditions    []string
	Categories    []folderCategory
	Preconditions []precondDef
	FeaturePre    map[string][]int
	TestCount     int
}

// demoVariant returns the demo dataset variant selected by a profile URL:
// "pkcs" for a demo-pkcs URL, "" (generic) otherwise. isDemoURL still gates
// whether demo mode is on at all.
func demoVariant(baseURL string) string {
	u := strings.ToLower(strings.TrimSpace(baseURL))
	if u == "demo-pkcs" || strings.HasPrefix(u, "demo-pkcs:") {
		return "pkcs"
	}
	return ""
}

// themeFor returns the theme for a profile URL.
func themeFor(baseURL string) demoTheme {
	if demoVariant(baseURL) == "pkcs" {
		return pkcsTheme
	}
	return genericTheme
}

// genericTheme is the original web-app demo vocabulary (references the existing
// package vars so the generic dataset is unchanged).
var genericTheme = demoTheme{
	Variant:       "",
	Features:      demoFeatures,
	Conditions:    demoConditions,
	Categories:    demoFolderCategories,
	Preconditions: preconditionDefs,
	FeaturePre:    featurePreconditions,
	TestCount:     demoTestCount,
}

// pkcsTheme is the PKCS#11 demo vocabulary: three signing-family functions,
// PKCS-shaped conditions, a signing/key-management folder split, and HSM
// preconditions.
var pkcsTheme = demoTheme{
	Variant:  "pkcs",
	Features: []string{"C_Sign", "C_GenerateKeyPair", "C_Verify"},
	Conditions: []string{
		"with RSA-2048", "with ECDSA P-256", "with SHA-256",
		"on an invalid session", "on a closed session", "with a NULL buffer",
		"expecting CKR_BUFFER_TOO_SMALL", "expecting CKR_ARGUMENTS_BAD",
		"on a read-only session", "with a tampered input",
		"after the matching C_*Init", "without a prior C_*Init",
	},
	Categories: []folderCategory{
		{"Signing", []string{"C_Sign", "C_Verify"}},
		{"Key management", []string{"C_GenerateKeyPair"}},
	},
	Preconditions: []precondDef{
		{"HSM is initialized", "Manual"},
		{"A session is open on the token", "Manual"},
		{"User is logged in to the token", "Manual"},
		{"An RSA-2048 key pair exists", "Manual"},
		{"An EC P-256 key pair exists", "Manual"},
		{"The signing operation was initialized (C_SignInit)", "Manual"},
	},
	FeaturePre: map[string][]int{
		"C_Sign":            {0, 1, 2, 3, 5},
		"C_GenerateKeyPair": {0, 1, 2},
		"C_Verify":          {0, 1, 3, 4},
	},
	TestCount: 240, // smaller, fully PKCS-flavoured corpus
}
```

- [ ] **Step 5: Run to verify pass + no regression**

Run: `go test ./internal/jira/ -run TestThemeForSelectsVariant -v` → PASS.
Run: `go test ./internal/jira/` → all existing tests PASS (generic vars unchanged).

- [ ] **Step 6: Commit**

```bash
git add internal/jira/demo_theme.go internal/jira/demo.go internal/jira/demo_theme_test.go
git commit -m "feat(demo): theme abstraction + PKCS vocabulary + themeFor selector"
```

---

### Task 2: Thread theme through the test + step generators

**Files:**
- Modify: `internal/jira/demo.go` (`makeDemoTest`, `demoTestsPage`, `demoTestForKey`, `demoTestBasicForKey`, `demoStepsForKey`, `demoFolderForFeature`, `demoCategoryIndexForFeature`), `internal/jira/meta.go` (`demoTestMeta`), and call sites in `internal/jira/search.go`, `internal/jira/steps.go`, `internal/jira/meta.go`
- Modify (tests): `internal/jira/demo_test.go`, `internal/jira/demo_duplicates_test.go`
- Test: add a PKCS assertion to `demo_theme_test.go`

**Interfaces:**
- Consumes: `demoTheme`, `themeFor` (Task 1).
- Produces (changed signatures): `makeDemoTest(theme demoTheme, projectKey string, i int) Test`; `demoTestsPage(theme demoTheme, projectKey string, startAt, maxResults int) ([]Test, int)`; `demoTestForKey(theme demoTheme, key string) Test`; `demoTestBasicForKey(theme demoTheme, key string) TestBasic`; `demoStepsForKey(theme demoTheme, testKey string) []Step`; `demoTestMeta(theme demoTheme, key string) TestMeta`; `demoFolderForFeature(theme demoTheme, feature string) string`; `demoCategoryIndexForFeature(theme demoTheme, feature string) int`.

- [ ] **Step 1: Add the failing PKCS assertion** to `demo_theme_test.go`:

```go
func TestMakeDemoTestPKCSThemed(t *testing.T) {
	pk := themeFor("demo-pkcs")
	// Feature cycles features[i%len]; C_Sign is index 0.
	tst := makeDemoTest(pk, "PKCS", 0)
	if got := tst.Summary; got == "" || got[:6] != "C_Sign" {
		t.Errorf("pkcs test summary = %q, want it to start with C_Sign", got)
	}
	// Generic is unchanged.
	g := makeDemoTest(genericTheme, "QA", 0)
	if g.Summary == "" {
		t.Error("generic test summary empty")
	}
}
```

Run: `go test ./internal/jira/ -run TestMakeDemoTestPKCSThemed -v` → FAIL (makeDemoTest takes 2 args).

- [ ] **Step 2: Thread `theme` through the generators**

In `internal/jira/demo.go`:
- `makeDemoTest(theme demoTheme, projectKey string, i int) Test`: replace `demoFeatures` → `theme.Features`, `demoConditions` → `theme.Conditions`. **Guard the generic-only duplicate-cluster special-case** (`switch i { case 0,1: ... case 2,3: ... }`) with `if theme.Variant == "" { switch i { ... } }` so PKCS tests aren't relabelled "user can log in". Change `demoFolderForFeature(feature)` (in the returned `FolderID`) → `demoFolderForFeature(theme, feature)`.
- `demoTestsPage(theme demoTheme, ...)`: use `theme.TestCount` in place of `demoTestCount` for the total/clamp; pass `theme` to `makeDemoTest`.
- `demoTestForKey(theme demoTheme, key string)`: pass `theme` to `makeDemoTest`.
- `demoTestBasicForKey(theme demoTheme, key string)`: if it builds from `makeDemoTest`/feature, thread `theme`; otherwise pass through.
- `demoStepsForKey(theme demoTheme, testKey string)`: for `theme.Variant=="pkcs"`, return PKCS steps (e.g. `C_OpenSession`, `<feature>Init`, `<feature>`, `Inspect CKR_* return`); for generic, the existing steps. Keep it deterministic from `testKey`.
- `demoFolderForFeature(theme demoTheme, feature string)` and `demoCategoryIndexForFeature(theme demoTheme, feature string)`: use `theme.Categories`.
- `internal/jira/meta.go` `demoTestMeta(theme demoTheme, key string)`: thread `theme` to whatever it calls.

Update **call sites** to pass `themeFor(c.baseURL)`:
- `search.go:423` `demoTestBasicForKey(themeFor(c.baseURL), k)`, `search.go:493` `demoTestForKey(themeFor(c.baseURL), key)`, and the `demoTestsPage(...)` call in search.go.
- `steps.go:201` `demoStepsForKey(themeFor(c.baseURL), key)`.
- `meta.go:32` `demoTestMeta(themeFor(c.baseURL), key)`.

- [ ] **Step 3: Update the existing tests to pass `genericTheme`**

In `demo_test.go` and `demo_duplicates_test.go`, change `makeDemoTest("QA", i)` → `makeDemoTest(genericTheme, "QA", i)`, `demoStepsForKey(key)` → `demoStepsForKey(genericTheme, key)`, `makeDemoTest("DEMO", n)` → `makeDemoTest(genericTheme, "DEMO", n)`. (The assertions are about generic behaviour — they stay valid.)

- [ ] **Step 4: Run the jira suite**

Run: `go test ./internal/jira/` → all PASS (incl. the new PKCS assertion and the unchanged generic tests).

- [ ] **Step 5: Commit**

```bash
git add internal/jira/
git commit -m "feat(demo): thread theme through test/step generators"
```

---

### Task 3: Thread theme through folders + requirements (with PKCS customer reqs)

**Files:**
- Modify: `internal/jira/demo.go` (`demoFolders`), `internal/jira/requirements.go` (`demoRequirements`), call site `internal/jira/folders.go:179`, `internal/jira/requirements.go:61`
- Test: `internal/jira/demo_theme_test.go` (requirements assertion)

**Interfaces:**
- Consumes: `demoTheme`.
- Produces: `demoFolders(theme demoTheme) []Folder`; `demoRequirements(theme demoTheme, testProjectKey string) ([]Requirement, []RequirementLink)`.

- [ ] **Step 1: Failing test** — append to `demo_theme_test.go`:

```go
func TestDemoRequirementsPKCSHasFunctionalAndCustomer(t *testing.T) {
	reqs, links := demoRequirements(themeFor("demo-pkcs"), "PKCS")
	var func0, cust0 bool
	for _, r := range reqs {
		if r.ProjectKey == "FUNC" && r.Summary == "C_Sign" {
			func0 = true
		}
		if r.ProjectKey == "CUST-HSM-BANK" {
			cust0 = true
		}
	}
	if !func0 {
		t.Error("pkcs requirements missing the C_Sign functional requirement (FUNC project)")
	}
	if !cust0 {
		t.Error("pkcs requirements missing a CUST-HSM-BANK customer requirement")
	}
	if len(links) == 0 {
		t.Error("pkcs requirements have no test links")
	}
	// Generic requirements unchanged (project PRD).
	gr, _ := demoRequirements(genericTheme, "DEMO")
	if len(gr) == 0 || gr[0].ProjectKey != "PRD" {
		t.Errorf("generic requirements changed: %+v", gr[:1])
	}
}
```

Run: `go test ./internal/jira/ -run TestDemoRequirementsPKCS -v` → FAIL.

- [ ] **Step 2: Thread theme into `demoFolders` + `demoFolderForFeature`**

`demoFolders(theme demoTheme) []Folder`: iterate `theme.Categories` instead of `demoFolderCategories`. Update call site `folders.go:179` → `demoFolders(themeFor(c.baseURL))`. (`demoFolderForFeature`/`demoCategoryIndexForFeature` already took `theme` in Task 2.)

- [ ] **Step 3: Thread theme into `demoRequirements` with a PKCS branch**

`demoRequirements(theme demoTheme, testProjectKey string)`:
- Generic branch (`theme.Variant == ""`): unchanged (existing `PRD-1..24` logic using `demoFeatures`).
- PKCS branch (`theme.Variant == "pkcs"`): for each feature `f` in `theme.Features` (index `fi`):
  - One **functional** requirement: `Key: "FUNC-PKCS11-" + code(f)`, `ProjectKey: "FUNC"`, `IssueType: "Requirement"`, `Summary: f`, `Status: "Approved"`.
  - Two **customer** requirements: `Key: "CUST-HSM-BANK-" + code(f)` (ProjectKey `CUST-HSM-BANK`) and `"CUST-HSM-SAMSU-" + code(f)` (ProjectKey `CUST-HSM-SAMSU`), `IssueType: "Story"`, `Summary: f + " — <BANK|SAMSU> customer requirement"`.
  - Links: link the customer requirements to the feature's tests — the tests for feature index `fi` are `{testProjectKey}-{n}` where `(n-1) % len(theme.Features) == fi`. Emit a `RequirementLink{TestKey, RequirementKey, LinkID}` for, say, the first 6 such test numbers per customer requirement (bounded, within `theme.TestCount`).
  - `code(f)` maps `C_Sign→SIG`, `C_GenerateKeyPair→KG`, `C_Verify→VER`; add a small `func pkcsCode(feature string) string` in `demo_theme.go`.
Update call site `requirements.go:61` → `demoRequirements(themeFor(c.baseURL), profileProjectKey)`.

- [ ] **Step 4: Run jira suite** → PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/jira/
git commit -m "feat(demo): theme folders + PKCS functional/customer requirements"
```

---

### Task 4: Thread theme through preconditions + containers

**Files:**
- Modify: `internal/jira/demo.go` (`demoPreconditionsAndLinks`, `demoContainersAndLinks`), call sites `preconditions.go:90`, `containers.go:88`
- Test: `demo_theme_test.go`

**Interfaces:**
- Consumes: `demoTheme`.
- Produces: `demoPreconditionsAndLinks(theme demoTheme, projectKey string) ([]Precondition, map[string][]string, error)`; `demoContainersAndLinks(theme demoTheme, projectKey string) ([]Container, []ContainerLink, error)`.

- [ ] **Step 1: Failing test** — append:

```go
func TestDemoPreconditionsPKCSThemed(t *testing.T) {
	pre, links, err := demoPreconditionsAndLinks(themeFor("demo-pkcs"), "PKCS")
	if err != nil {
		t.Fatal(err)
	}
	if len(pre) == 0 || pre[0].Summary[:3] != "HSM" {
		t.Errorf("pkcs preconditions not themed: %+v", pre[:1])
	}
	if len(links) == 0 {
		t.Error("pkcs preconditions have no test links")
	}
	// Generic unchanged.
	gp, _, _ := demoPreconditionsAndLinks(genericTheme, "DEMO")
	if len(gp) == 0 || gp[0].Summary != "User account exists" {
		t.Errorf("generic preconditions changed: %+v", gp[:1])
	}
}
```

Run → FAIL.

- [ ] **Step 2: Thread theme**

`demoPreconditionsAndLinks(theme demoTheme, projectKey string)`: build from `theme.Preconditions` and `theme.FeaturePre` (instead of `preconditionDefs`/`featurePreconditions`); assign per-test by the test's feature `theme.Features[(i)%len(theme.Features)]`, bounded by `theme.TestCount`. Update call site `preconditions.go:90`.

`demoContainersAndLinks(theme demoTheme, projectKey string)`: where it names Test Sets/Plans from `demoFolderCategories`, use `theme.Categories`; where it picks member tests by feature, use `theme.Features`. Keep the structure (sets per category, plans per release, executions per cycle, run statuses). Update call site `containers.go:88`.

- [ ] **Step 3: Run jira suite** → PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/jira/
git commit -m "feat(demo): theme preconditions + containers"
```

---

### Task 5: Adapt the coverage seed to map to synced PKCS data

**Files:**
- Modify: `internal/coverage/demopkcs.go`, `internal/coverage/demopkcs_test.go`
- Modify: `frontend/src/components/CoverageView.tsx` (button label + notice)

**Interfaces:**
- Consumes: synced PKCS rows (`requirement` with `FUNC-PKCS11-*` / `CUST-HSM-*`, `test_case` with summaries beginning with the feature name).
- Produces: `SeedPKCSReference(profileID string) (PKCSSeedSummary, error)` — now writes **only the coverage layer** (canonicals + members + versions + models + value→test mappings + CRs), mapping to synced data.

- [ ] **Step 1: Update the test for the new contract** — `demopkcs_test.go`:

Rewrite `TestSeedPKCSReferenceIsConsistent` to first **seed synced PKCS data** the way the backend would (insert into `requirement`: `FUNC-PKCS11-SIG/KG/VER` + `CUST-HSM-BANK/SAMSU-*`; into `test_case`: ~8 tests per feature with summaries like `"C_Sign with RSA-2048"`; link tests→customer reqs in `test_requirement`; a `testexec` container with run statuses), then call `SeedPKCSReference`, then assert: 3 canonicals; each has 2 versions + 2 members (the customer reqs) + a CR with 2 decisions; coverage partial; **`DetectStaleMappings(p,"")==0`** (mappings point at the seeded tests); no `PKCS-*`/`FUNC-*` rows were *written by the seeder* (it only reads them). Keep idempotency assertions.

Run → FAIL (seeder still writes its own Jira rows).

- [ ] **Step 2: Rewrite `SeedPKCSReference`**

Remove Phase 1 (the raw-SQL writes of folders/requirements/tests/links/executions/`sync_state`) and `clearPKCSReference`. Keep `deletePriorPKCSCanonicals` (idempotency). The new body, per feature `f` (C_Sign/KG/Verify):
1. `cid := CreateCanonical(profileID, f.fn, "PKCS11", …)`.
2. Members: `SELECT jira_key FROM requirement WHERE profile_id=? AND project_key IN ('CUST-HSM-BANK','CUST-HSM-SAMSU') AND summary LIKE f.fn||'%'` → `SetMembers`.
3. Candidate tests: `SELECT jira_key FROM test_case WHERE profile_id=? AND summary LIKE f.fn||'%' ORDER BY jira_key` → the pool to map values to.
4. Build the parameter model (the existing `f.groups` definitions) on a `2.40` version; map non-gap values round-robin to the candidate test keys via `SetValueTests`. Clone → `3.0`. Member version locks. Change request + decisions (members resolved from step 2).
If a feature has no synced tests (pool empty), still create the canonical + model but leave mappings empty (coverage 0%) — don't error.

- [ ] **Step 3: Relabel the button**

In `CoverageView.tsx`, `loadPkcsReference`'s notice and the button text change from "Load PKCS#11 reference data" to **"Load PKCS#11 coverage"**, and the notice explains it maps to the synced PKCS tests (sync the `demo-pkcs` profile first). The handler is otherwise unchanged.

- [ ] **Step 4: Run** `go test ./internal/coverage/ -run TestSeedPKCSReferenceIsConsistent -v` → PASS; `cd frontend && npx tsc --noEmit` → clean.

- [ ] **Step 5: Commit**

```bash
git add internal/coverage/demopkcs.go internal/coverage/demopkcs_test.go frontend/src/components/CoverageView.tsx
git commit -m "feat(coverage): map PKCS coverage seed to synced demo-pkcs data"
```

---

### Task 6: Full verification + CHANGELOG + final review

**Steps:**

- [ ] **Step 1: Full build + suites**

Run: `go build ./...` then `go test ./internal/...` → all PASS. `cd frontend && npx tsc --noEmit && npm run build` → green.

- [ ] **Step 2: Regenerate bindings if any bound signature changed**

`wails generate module` (only `SeedPKCSReference`'s shape could change — verify `App.d.ts`). Commit any binding delta.

- [ ] **Step 3: Demo click-through**

`wails dev`. Create a profile with jiraUrl `demo-pkcs` → **Sync**. Verify **Browse** shows `C_Sign …` / `C_GenerateKeyPair …` / `C_Verify …` tests foldered under Signing / Key management; **Requirements** shows `FUNC-PKCS11-*` + `CUST-HSM-*`; **Preconditions** shows HSM-themed entries; **Containers** shows PKCS Test Sets / Plans / Executions. Then Coverage → **Load PKCS#11 coverage** → the 3 features map to the synced tests (matrix keys openable in Browse).

- [ ] **Step 4: CHANGELOG**

Replace the prior PKCS bullet under `## [Unreleased]` with one describing the `demo-pkcs` *demo variant*: a `demo-pkcs` profile now syncs a PKCS#11 dataset across all views (tests/requirements/preconditions/sets/plans/executions/steps), and **Load PKCS#11 coverage** layers the parameter models/versions/CRs onto the synced tests.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs: changelog for demo-pkcs backend variant"
```

- [ ] **Step 6: Final whole-branch review** (subagent-driven flow): dispatch the final reviewer over `feature/pkcs-reference-dataset` vs its base, focused on: generic demo dataset unchanged (regression risk), determinism preserved, and the coverage seed correctly mapping to synced data.

---

## Self-Review

**Spec coverage:** theme abstraction + selector (Task 1); tests/steps themed (Task 2); folders + functional/customer requirements (Task 3); preconditions + containers (Task 4); coverage seed mapped to synced data + button relabel (Task 5); verify + changelog + review (Task 6). The four entity groups the user named (tests, requirements, preconditions, executions/plans) are all covered. ✅

**Placeholder scan:** Tasks 2–5 describe the threading per named generator with exact signatures and call-site line refs rather than reproducing each generator's full body — acceptable because it's a mechanical vocab-swap over code the implementer reads, and the new abstraction (Task 1) and every test are given in full. No "TBD"/"handle edge cases". ✅

**Type consistency:** `demoTheme`/`folderCategory`/`precondDef`/`themeFor`/`demoVariant`/`pkcsCode` are defined in Task 1 and consumed unchanged in Tasks 2–5. The `theme demoTheme` first-parameter convention is uniform across every threaded generator. `SeedPKCSReference` keeps its signature (Task 5) so the binding is stable. ✅
