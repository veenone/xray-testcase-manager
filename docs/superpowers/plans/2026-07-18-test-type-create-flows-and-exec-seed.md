# Test-Type in Create Flows + Demo Execution Seed — Implementation Plan

> Follow-up to the Cucumber/Generic feature (same branch `feature/test-types-cucumber-generic`, updates PR #54). Executed via superpowers:subagent-driven-development.

**Goal:** Let tests be *created* with a type (New Test form, clone, CSV/XLSX import) — not only edited — and make the demo dataset show Cucumber/Generic tests inside a Test Execution with run results.

**Architecture:** All create paths funnel through one `insertLocalTest`, so the four type columns (`exec_type` + `cucumber_scenario`/`cucumber_type`/`generic_definition`) are added there once via the `TestDraft`/`testCreatePayload` structs; each surface (form, clone, import) then populates them. Demo executions are already type-agnostic; we add explicit curated member links.

**Tech Stack:** Go 1.25 + SQLite; Wails v2; React 19 + TS. No new deps.

## Global Constraints

- Run from `C:\projects\xray-test-manager`. PowerShell for shell/git/go/npm (Bash lacks them on PATH). gopls "GOROOT"/"undefined" cascades are spurious — `go build ./...` / `go test` / `npm run build` via CLI are authoritative.
- No `Co-Authored-By` / AI-attribution trailers; no "Generated with" footer.
- Type values: `Manual | Automated | Generic | Cucumber`. Body columns already exist (schema v41): `cucumber_scenario`, `cucumber_type` (`Scenario`/`Scenario Outline`), `generic_definition`.
- Reuse existing patterns: the `TestCase.ExecType`/body fields, `editableFields`, the `TestDetail.tsx` type-conditional editor, the demo determinism (index-driven, no rand/time).
- Don't disturb the Manual path or existing demo invariant tests; keep demo deterministic.
- Every task: `go build ./...` + relevant `go test` (and `npm run build` for frontend tasks) green before commit.

---

### Task 1: Backend — thread type + body through `TestDraft` → `insertLocalTest`

**Files:**
- Modify: `internal/testrepo/createtest.go` (`TestDraft` struct ~18-27; `CreateTest` payload map ~35-42; `CloneTest` ~84-102)
- Modify: `internal/testrepo/importcsv.go` (`testCreatePayload` ~59-67; `insertLocalTest` INSERT ~204-215)
- Test: `internal/testrepo/createtest_testtype_test.go` (create)

**Interfaces:**
- Produces: `TestDraft` and `testCreatePayload` gain `ExecType, CucumberScenario, CucumberType, GenericDefinition string`; `insertLocalTest` writes them to `test_case`.

- [ ] **Step 1: Failing test**

```go
package testrepo

import "testing"

func TestCreateTestPersistsTypeAndBody(t *testing.T) {
	repo := newTestRepo(t) // use the package's real helper
	key, err := repo.CreateTest("p1", TestDraft{
		Summary:          "BDD login",
		ExecType:         "Cucumber",
		CucumberType:     "Scenario",
		CucumberScenario: "Scenario: login\n  Given a user",
	})
	if err != nil {
		t.Fatal(err)
	}
	tc, err := repo.GetTest("p1", key)
	if err != nil {
		t.Fatal(err)
	}
	if tc.ExecType != "Cucumber" || tc.CucumberType != "Scenario" || tc.CucumberScenario == "" {
		t.Errorf("type/body not persisted on create: %+v", tc)
	}
}

func TestCloneCarriesTypeAndBody(t *testing.T) {
	repo := newTestRepo(t)
	src, _ := repo.CreateTest("p1", TestDraft{Summary: "Gen", ExecType: "Generic", GenericDefinition: "com.acme.Foo#bar"})
	cloneKey, err := repo.CloneTest("p1", src)
	if err != nil {
		t.Fatal(err)
	}
	tc, _ := repo.GetTest("p1", cloneKey)
	if tc.ExecType != "Generic" || tc.GenericDefinition == "" {
		t.Errorf("clone dropped type/body: %+v", tc)
	}
}
```

(Confirm the real repo-test helper name by grepping existing `internal/testrepo/*_test.go`.)

- [ ] **Step 2: Run → FAIL** (`go test ./internal/testrepo/ -run 'TestCreateTestPersistsTypeAndBody|TestCloneCarriesTypeAndBody' -v`) — undefined fields / dropped values.

- [ ] **Step 3: Add fields to `TestDraft`** (createtest.go), after `Components`:

```go
	Components  string      `json:"components"`
	ExecType          string `json:"execType"`
	CucumberScenario  string `json:"cucumberScenario"`
	CucumberType      string `json:"cucumberType"`
	GenericDefinition string `json:"genericDefinition"`
	FolderID    string      `json:"folderId"`
	Steps       []StepDraft `json:"steps"`
	PrecondKeys []string    `json:"precondKeys"`
```

- [ ] **Step 4: Add the same fields to `testCreatePayload`** (importcsv.go ~59-67), and in `CreateTest` (createtest.go) copy them from draft → payload alongside Summary/etc.

- [ ] **Step 5: Extend `insertLocalTest`** (importcsv.go ~204-215): add `exec_type, cucumber_scenario, cucumber_type, generic_definition` to the INSERT column list, add four `?` placeholders, and bind the payload values. Keep the existing columns/placeholders exactly; count must stay consistent.

- [ ] **Step 6: Fix `CloneTest`** (createtest.go ~96-102): add to the `TestDraft` it builds:

```go
		Components:  strings.Join(src.Components, ","),
		ExecType:          src.ExecType,
		CucumberScenario:  src.CucumberScenario,
		CucumberType:      src.CucumberType,
		GenericDefinition: src.GenericDefinition,
```

- [ ] **Step 7: Run → PASS**; then full `go test ./internal/testrepo/...` green.

- [ ] **Step 8: Commit** — `feat(testrepo): create + clone tests with type and body`

---

### Task 2: Import — "Test Type" + body columns

**Files:**
- Modify: `internal/testrepo/importcsv.go` (`ImportMapping` ~26-36; header auto-map; `groupImportRows` row→payload ~132-139; `ImportTemplateCSV` header ~370)
- Test: `internal/testrepo/importcsv_testtype_test.go` (create)

**Interfaces:**
- Consumes: Task 1's `testCreatePayload` fields.
- Produces: `ImportMapping` gains `TestType, CucumberScenario, CucumberType, GenericDefinition int` (column indices, -1 when absent); import populates the payload from them.

- [ ] **Step 1: Failing test** — build an in-memory CSV with headers `Summary,Test Type,Cucumber Scenario,Scenario Type,Generic Test Definition,...`, one Cucumber row + one Generic row, run the import, assert the created tests carry the type + body. Use the existing import entry (`ImportTests`) the way other `importcsv` tests do (grep `func TestImport` for the harness + how a mapping/preview is produced).

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Extend `ImportMapping`** with the four int fields (default -1).

- [ ] **Step 4: Header auto-detection** — wherever headers are matched to mapping fields (grep the header-normalization/auto-map code in importcsv.go), recognize (case-insensitive, trimmed): `"test type"` → TestType; `"cucumber scenario"` → CucumberScenario; `"cucumber test type"`/`"scenario type"` → CucumberType; `"generic test definition"`/`"generic definition"` → GenericDefinition.

- [ ] **Step 5: Populate payload** — in `groupImportRows` (or the row→`testCreatePayload` builder), when a mapping index ≥ 0, copy the cell into the payload's `ExecType`/`CucumberScenario`/`CucumberType`/`GenericDefinition`. Trim; leave empty when the column is absent or blank. (Manual/Automated rows simply have empty body cells.)

- [ ] **Step 6: Template header** — append `,Test Type,Cucumber Scenario,Scenario Type,Generic Test Definition` to `ImportTemplateCSV` (after the existing Manual step columns), so the downloadable template documents them.

- [ ] **Step 7: Run → PASS**; full `go test ./internal/testrepo/...` green (existing import tests must stay green — the new columns are optional/backward-compatible).

- [ ] **Step 8: Commit** — `feat(testrepo): import Test Type + cucumber/generic body columns`

---

### Task 3: Frontend — type-aware New Test form

**Files:**
- Modify: `frontend/src/components/NewTestPanel.tsx` (state ~37-44; draft ~111-120; JSX)
- Modify: `frontend/src/api.ts` (`TestDraft` type — grep it)
- Test: `cd frontend; npm run build` (tsc + vite) — no frontend unit runner

**Interfaces:**
- Consumes: Task 1's `TestDraft` fields; `App.CreateTest`.
- Produces: the form collects `execType` (+ cucumber/generic body) and sends them in the draft.

- [ ] **Step 1: api.ts `TestDraft`** — add `execType: string; cucumberScenario: string; cucumberType: string; genericDefinition: string;` (grep how `TestDraft` is declared/imported in api.ts; if it's the generated `testrepo.TestDraft`, prefer that model — but since the frontend constructs the draft object, ensure the TS type used by NewTestPanel includes the fields; add to the generated `testrepo.TestDraft` class in `models.ts` the same way TestCase was extended, if needed for tsc).

- [ ] **Step 2: State + selector** — add `const [execType, setExecType] = useState("Manual");` plus `cucumberScenario`, `cucumberType` (default `"Scenario"`), `genericDefinition` state. Add an "Execution type" `<select>` (options `Manual/Automated/Generic/Cucumber`) near Priority, mirroring `TestDetail`'s dropdown styling.

- [ ] **Step 3: Conditional body editor** — keyed on `execType`, mirroring `TestDetail.tsx`:
  - `Manual`/`Automated` → the existing Steps section (unchanged).
  - `Cucumber` → monospace Gherkin `<textarea>` bound to `cucumberScenario` + a Scenario Type `<select>` (`Scenario`/`Scenario Outline`) bound to `cucumberType`.
  - `Generic` → monospace `<textarea>` bound to `genericDefinition`.
  Reuse the `.cuke-editor`/`.generic-editor`/`.mono` CSS added for `TestDetail` (grep App.css to confirm the class names).

- [ ] **Step 4: Include in draft** — add `execType, cucumberScenario, cucumberType, genericDefinition` to the object submitted at ~111-120. For Manual/Automated, send empty body fields; for Cucumber/Generic, the Steps array may be empty.

- [ ] **Step 5: Build** — `cd frontend; npm run build` clean.

- [ ] **Step 6: Commit** — `feat(ui): choose test type (and body) when creating a test`

---

### Task 4: Demo — curated execution with Cucumber + Generic members

**Files:**
- Modify: `internal/jira/demo.go` (`demoContainersAndLinks` member-links loop ~692-720)
- Test: `internal/jira/demo_exec_testtype_test.go` (create)

**Interfaces:**
- Produces: at least one demo Test Execution whose members include ≥1 Cucumber and ≥1 Generic demo test, each with a run status.

- [ ] **Step 1: Failing test** — build the demo containers/links for the `demo` theme (grep how existing demo tests call `demoContainersAndLinks` / `themeFor`), then assert: among the `ContainerLink`s, some execution (`kind`/key contains `-TE-`) has at least one member whose test key maps to a Cucumber test (index `i` with `i%4==3`, i.e. keys `DEMO-4/-8/…`) AND one Generic (`i%4==2`, keys `DEMO-3/-7/…`), each with a non-empty `RunStatus`. (Assert on the returned links; pick keys by the known type cycle.)

- [ ] **Step 2: Run → FAIL** if not guaranteed (the modulo cycle makes it incidental; the test makes it explicit/guaranteed).

- [ ] **Step 3: Add explicit curated links** — after the existing member loop (~720), append deterministic `ContainerLink`s that place a known Cucumber test (e.g. `DEMO-4`, `DEMO-8`) and a known Generic test (e.g. `DEMO-3`, `DEMO-7`) into a specific execution (reuse `execKeys[0]`, i.e. `DEMO-TE-1`, or a clearly-named one) with fixed run statuses (`PASS`/`FAIL`/`TODO` from `demoRunStatuses`). Keep it deterministic; guard against duplicate `(container,test)` pairs if that key is already linked (dedupe or pick indices not already linked to that exec). Add a short comment explaining the curation.

- [ ] **Step 4: Run → PASS**; then full `go test ./internal/jira/...` green (demo invariant/count tests must stay green — if a link-count invariant asserts an exact total, update it to match the added links and note why).

- [ ] **Step 5: Commit** — `feat(demo): showcase cucumber + generic tests in a test execution`

---

### Task 5: Verify + update PR

- [ ] `go build ./... && go test ./...` all green; `cd frontend; npm run build` clean.
- [ ] Demo E2E (hot-reload already running): New Test form shows the type selector + body editor; creating a Cucumber test lands with its scenario; cloning it keeps the type; the curated execution shows Cucumber/Generic members with run statuses.
- [ ] `git push` (updates PR #54). Update the PR body to mention the create/clone/import type support + demo execution seed.

## Self-Review

- Create form + clone + import + gap all write type via the shared `insertLocalTest` (Task 1) ✓ — gap-create inherits `exec_type=''` (Manual-equivalent) which is acceptable and unchanged.
- Import backward-compatible (new columns optional, -1 when absent) ✓.
- Demo determinism preserved; invariant tests addressed in Task 4 Step 4 ✓.
- No placeholder steps; every code step shows the code or the exact grep target for a name that must be confirmed against the tree.
- Type/casing parity: Go `ExecType/CucumberScenario/CucumberType/GenericDefinition` ↔ json `execType/cucumberScenario/cucumberType/genericDefinition` ↔ TS identical, consistent with the already-merged feature.
