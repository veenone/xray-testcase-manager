# Cucumber & Generic Test Type Support — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the test editor from Manual-only to full view + edit + commit support for Xray Cucumber (Gherkin scenario + scenario type) and Generic (definition) test types, with non-destructive best-effort body conversion on type change.

**Architecture:** Three additive `test_case` columns store the non-Manual bodies. They are read on sync as instance-resolved Jira custom fields, edited locally through the existing `EditTestField` journaling path, and committed as custom fields via the same resolve-then-inject pattern `exec_type` already uses. A `TestDetail.tsx` conditional swaps the editor by type. Type change runs pure Go conversion functions that pre-fill only an empty target body, never destroying the source.

**Tech Stack:** Go 1.25 (backend, `modernc.org/sqlite`), Wails v2 (bindings), React 19 + TypeScript (frontend), `excelize` (unrelated). No new dependencies.

## Global Constraints

- Run all commands from `C:\projects\xray-test-manager`.
- Go build/test: `go build ./...`, `go test ./...`. Frontend: `cd frontend; npm run build`.
- **No `Co-Authored-By` / AI-attribution trailers** in any commit (project rule).
- Schema columns are added via **unconditional `ALTER TABLE ADD COLUMN` with duplicate-column tolerance** (store.go:867-896 pattern), NOT `if current < N` gating — two in-flight branches share version numbers, so gated ALTERs get skipped. Bump `schemaVersion` for diagnostics only.
- `internal/` stays import-private; `testrepo` must not import `syncer`/`jira` cycles (existing rule).
- Manual step path (`test_step` table, raven endpoint) must remain byte-for-byte unchanged — regression tests stay green.
- Custom-field resolvers are best-effort: return `""` (no error) when the field is absent so sync/commit degrade gracefully (existing `resolveCustomFieldID` contract).
- Field names to resolve: `"Cucumber Scenario"` (text), `"Cucumber Test Type"` (option: `Scenario` | `Scenario Outline`), `"Generic Test Definition"` (text).

---

### Task 1: Schema — three body columns on `test_case`

**Files:**
- Modify: `internal/store/store.go` (baseSchema `test_case` block ~68-83; the unconditional ALTER block ~888-896; `schemaVersion` const line 20)
- Test: `internal/store/store_testtypes_test.go` (create)

**Interfaces:**
- Produces: `test_case` columns `cucumber_scenario TEXT NOT NULL DEFAULT ''`, `cucumber_type TEXT NOT NULL DEFAULT ''`, `generic_definition TEXT NOT NULL DEFAULT ''`; `schemaVersion = 41`.

- [ ] **Step 1: Write the failing test**

```go
package store

import "testing"

func TestTestCaseBodyColumnsExist(t *testing.T) {
	st := newTestStore(t) // existing helper in the package's test files
	cols := map[string]bool{}
	rows, err := st.DB().Query(`PRAGMA table_info(test_case)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		cols[name] = true
	}
	for _, want := range []string{"cucumber_scenario", "cucumber_type", "generic_definition"} {
		if !cols[want] {
			t.Errorf("test_case missing column %q", want)
		}
	}
}
```

(If `newTestStore` differs, mirror the helper used by the nearest existing `store` test — grep `func newTestStore` / `func openTestStore` in `internal/store`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestTestCaseBodyColumnsExist -v`
Expected: FAIL — `test_case missing column "cucumber_scenario"` (and the others).

- [ ] **Step 3: Add columns to baseSchema**

In the `test_case` CREATE TABLE (store.go ~80-81), after `exec_type` / `fix_versions`, add:

```go
	exec_type    TEXT NOT NULL DEFAULT '',
	fix_versions TEXT NOT NULL DEFAULT '',
	cucumber_scenario  TEXT NOT NULL DEFAULT '',
	cucumber_type      TEXT NOT NULL DEFAULT '',
	generic_definition TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (profile_id, jira_key)
```

- [ ] **Step 4: Add duplicate-tolerant ALTERs for existing DBs**

In the unconditional ALTER block (store.go ~888-896, alongside the `run_defects` adds), append a new loop:

```go
	// Cucumber/Generic test-type bodies (schema v41): the Gherkin scenario +
	// its scenario type, and the generic test definition. Applied
	// UNCONDITIONALLY with duplicate-column tolerance for the same shared-version
	// reason as the blocks above.
	for _, stmt := range []string{
		`ALTER TABLE test_case ADD COLUMN cucumber_scenario TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE test_case ADD COLUMN cucumber_type TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE test_case ADD COLUMN generic_definition TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("add test-type body columns: %w", err)
		}
	}
```

- [ ] **Step 5: Bump schemaVersion**

store.go line 20: `const schemaVersion = 41`

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestTestCaseBodyColumnsExist -v`
Expected: PASS. Then `go test ./internal/store/...` — all green.

- [ ] **Step 7: Commit**

```bash
git add internal/store/store.go internal/store/store_testtypes_test.go
git commit -m "feat(store): add cucumber/generic body columns to test_case (schema v41)"
```

---

### Task 2: testrepo model — struct fields, upsert, editable whitelist

**Files:**
- Modify: `internal/testrepo/testrepo.go` (`TestCase` struct 25-43; `editableFields` 283-289; `UpsertTests` INSERT 329-342 and its UPDATE/scan branches; the `GetTest`/list SELECT that reads test_case columns)
- Test: `internal/testrepo/testtypes_fields_test.go` (create)

**Interfaces:**
- Consumes: schema columns from Task 1.
- Produces: `TestCase.CucumberScenario`, `TestCase.CucumberType`, `TestCase.GenericDefinition string` (json `cucumberScenario`/`cucumberType`/`genericDefinition`); `editableFields` accepts `cucumber_scenario`, `cucumber_type`, `generic_definition`.

- [ ] **Step 1: Write the failing test**

```go
package testrepo

import "testing"

func TestEditAndReadTestTypeBodies(t *testing.T) {
	repo := newTestRepo(t) // mirror the nearest existing testrepo test helper
	if err := repo.UpsertTests("p1", []TestCase{{Key: "QA-1", ID: "1", Summary: "S", ExecType: "Cucumber"}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.EditTestField("p1", "QA-1", "cucumber_scenario", "Scenario: x\n  Given y"); err != nil {
		t.Fatalf("edit scenario: %v", err)
	}
	if err := repo.EditTestField("p1", "QA-1", "cucumber_type", "Scenario"); err != nil {
		t.Fatalf("edit type: %v", err)
	}
	if err := repo.EditTestField("p1", "QA-1", "generic_definition", "com.acme.Foo#bar"); err != nil {
		t.Fatalf("edit def: %v", err)
	}
	tc, err := repo.GetTest("p1", "QA-1")
	if err != nil {
		t.Fatal(err)
	}
	if tc.CucumberScenario == "" || tc.CucumberType != "Scenario" || tc.GenericDefinition == "" {
		t.Errorf("bodies not persisted/read: %+v", tc)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/testrepo/ -run TestEditAndReadTestTypeBodies -v`
Expected: FAIL — `field "cucumber_scenario" is not editable` (and struct fields undefined).

- [ ] **Step 3: Add struct fields**

In `TestCase` (testrepo.go 25-43), after `FixVersions`:

```go
	FixVersions []string `json:"fixVersions"`
	// Non-Manual test bodies. CucumberScenario holds the Gherkin text and
	// CucumberType its scenario kind ("Scenario"/"Scenario Outline") for
	// Cucumber tests; GenericDefinition holds the plain-text definition for
	// Generic tests. Empty for other types.
	CucumberScenario  string `json:"cucumberScenario"`
	CucumberType      string `json:"cucumberType"`
	GenericDefinition string `json:"genericDefinition"`
```

- [ ] **Step 4: Extend editable whitelist**

testrepo.go 283-289:

```go
var editableFields = map[string]string{
	"summary":            "summary",
	"description":        "description",
	"priority":           "priority",
	"labels":             "labels",
	"exec_type":          "exec_type",
	"cucumber_scenario":  "cucumber_scenario",
	"cucumber_type":      "cucumber_type",
	"generic_definition": "generic_definition",
}
```

- [ ] **Step 5: Extend UpsertTests + GetTest column lists**

In `UpsertTests` (testrepo.go 329+): add the three columns to the INSERT column list and `VALUES` placeholders, bind `t.CucumberScenario, t.CucumberType, t.GenericDefinition`, and — following the existing per-field "keep local pending edit" pattern used for `exec_type` in the ON CONFLICT/UPDATE branch — guard each new column the same way. Locate the SELECT in `GetTest` (grep `FROM test_case` in testrepo.go) and add `cucumber_scenario, cucumber_type, generic_definition` to its column list + `Scan` targets. Do the same for any list query that hydrates full `TestCase` rows (grep `exec_type` within testrepo.go to find every SELECT that must gain the columns).

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/testrepo/ -run TestEditAndReadTestTypeBodies -v`
Expected: PASS. Then `go test ./internal/testrepo/...` — all green (regression).

- [ ] **Step 7: Commit**

```bash
git add internal/testrepo/testrepo.go internal/testrepo/testtypes_fields_test.go
git commit -m "feat(testrepo): model + edit cucumber/generic bodies as test fields"
```

---

### Task 3: Pure conversion functions

**Files:**
- Create: `internal/testrepo/testtype.go`
- Test: `internal/testrepo/testtype_test.go`

**Interfaces:**
- Consumes: `testrepo.Step` (existing: `Action`, `Data`, `Expected` fields).
- Produces:
  - `func StepsToGherkin(summary string, steps []Step, scenarioType string) string`
  - `func StepsToDefinition(steps []Step) string`
  - `func GherkinToSteps(scenario string) []Step`
  - `func GherkinToDefinition(scenario string) string`
  - `func DefinitionToSteps(definition string) []Step`
  - `func DefinitionToGherkin(summary, definition string) string`

- [ ] **Step 1: Write the failing test**

```go
package testrepo

import (
	"strings"
	"testing"
)

func TestStepsToGherkin(t *testing.T) {
	steps := []Step{
		{Action: "open login", Data: "user=bob", Expected: "form shown"},
		{Action: "submit", Expected: "dashboard shown"},
	}
	got := StepsToGherkin("Login works", steps, "Scenario")
	if !strings.HasPrefix(got, "# generated from 2 manual steps") {
		t.Errorf("missing review header: %q", got)
	}
	for _, want := range []string{"Scenario: Login works", "When open login", "And user=bob", "Then form shown", "When submit", "Then dashboard shown"} {
		if !strings.Contains(got, want) {
			t.Errorf("gherkin missing %q in:\n%s", want, got)
		}
	}
}

func TestGherkinToSteps(t *testing.T) {
	scenario := "Scenario: x\n  Given a user\n  When they click\n  Then a page loads"
	steps := GherkinToSteps(scenario)
	if len(steps) != 3 {
		t.Fatalf("want 3 steps, got %d: %+v", len(steps), steps)
	}
	if steps[0].Action != "a user" || steps[2].Action != "a page loads" {
		t.Errorf("keyword not stripped: %+v", steps)
	}
}

func TestDefinitionRoundTripsAreNonEmpty(t *testing.T) {
	if StepsToDefinition([]Step{{Action: "a", Data: "b", Expected: "c"}}) == "" {
		t.Error("StepsToDefinition empty")
	}
	if len(DefinitionToSteps("line1\nline2")) != 2 {
		t.Error("DefinitionToSteps should split by line")
	}
	if GherkinToDefinition("Scenario: x\n Given y") == "" {
		t.Error("GherkinToDefinition empty")
	}
	if !strings.Contains(DefinitionToGherkin("Sum", "com.acme.Foo"), "Scenario: Sum") {
		t.Error("DefinitionToGherkin missing scenario header")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/testrepo/ -run 'TestStepsToGherkin|TestGherkinToSteps|TestDefinition' -v`
Expected: FAIL — undefined: `StepsToGherkin` (etc).

- [ ] **Step 3: Implement the conversion functions**

```go
package testrepo

import (
	"fmt"
	"strings"
)

// Conversion between the three test-type bodies. All transforms are
// best-effort and lossy by nature: they exist to pre-fill an EMPTY target body
// when a test's type changes, giving the user a reviewable starting point. The
// source body is never modified by these pure functions.

var gherkinKeywords = []string{"Given ", "When ", "Then ", "And ", "But ", "* "}

// StepsToGherkin renders manual steps as a Gherkin scenario skeleton.
func StepsToGherkin(summary string, steps []Step, scenarioType string) string {
	if scenarioType == "" {
		scenarioType = "Scenario"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# generated from %d manual steps — review before commit\n", len(steps))
	fmt.Fprintf(&b, "%s: %s\n", scenarioType, strings.TrimSpace(summary))
	for _, s := range steps {
		if a := strings.TrimSpace(s.Action); a != "" {
			fmt.Fprintf(&b, "  When %s\n", a)
		}
		if d := strings.TrimSpace(s.Data); d != "" {
			fmt.Fprintf(&b, "  And %s\n", d)
		}
		if e := strings.TrimSpace(s.Expected); e != "" {
			fmt.Fprintf(&b, "  Then %s\n", e)
		}
	}
	return b.String()
}

// StepsToDefinition flattens manual steps to a numbered plain-text definition.
func StepsToDefinition(steps []Step) string {
	var b strings.Builder
	for i, s := range steps {
		fmt.Fprintf(&b, "%d. %s", i+1, strings.TrimSpace(s.Action))
		if d := strings.TrimSpace(s.Data); d != "" {
			fmt.Fprintf(&b, " — Data: %s", d)
		}
		if e := strings.TrimSpace(s.Expected); e != "" {
			fmt.Fprintf(&b, " — Expected: %s", e)
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// GherkinToSteps parses scenario lines into manual steps: each Given/When/Then/
// And/But line becomes a step action (keyword stripped). Headers, comments, and
// blanks are skipped.
func GherkinToSteps(scenario string) []Step {
	var steps []Step
	for _, raw := range strings.Split(scenario, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") ||
			strings.HasPrefix(line, "Scenario") || strings.HasPrefix(line, "Feature") ||
			strings.HasPrefix(line, "Background") || strings.HasPrefix(line, "Examples") {
			continue
		}
		for _, kw := range gherkinKeywords {
			if strings.HasPrefix(line, kw) {
				line = strings.TrimSpace(strings.TrimPrefix(line, kw))
				break
			}
		}
		steps = append(steps, Step{Action: line})
	}
	return steps
}

// GherkinToDefinition uses the raw scenario text as the generic definition.
func GherkinToDefinition(scenario string) string {
	return strings.TrimSpace(scenario)
}

// DefinitionToSteps turns each non-blank line of a definition into a step action.
func DefinitionToSteps(definition string) []Step {
	var steps []Step
	for _, raw := range strings.Split(definition, "\n") {
		if line := strings.TrimSpace(raw); line != "" {
			steps = append(steps, Step{Action: line})
		}
	}
	return steps
}

// DefinitionToGherkin wraps a definition as a scenario with the definition as a
// Given line.
func DefinitionToGherkin(summary, definition string) string {
	return fmt.Sprintf("Scenario: %s\n  Given %s\n",
		strings.TrimSpace(summary), strings.TrimSpace(definition))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/testrepo/ -run 'TestStepsToGherkin|TestGherkinToSteps|TestDefinition' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/testrepo/testtype.go internal/testrepo/testtype_test.go
git commit -m "feat(testrepo): pure best-effort test-body conversion functions"
```

---

### Task 4: `ChangeTestType` orchestrator (non-destructive pre-fill)

**Files:**
- Modify: `internal/testrepo/testrepo.go` (add method; reuse `EditTestField`, `GetTest`, `GetTestSteps`/steps reader, `AddTestStep`)
- Test: `internal/testrepo/changetesttype_test.go` (create)

**Interfaces:**
- Consumes: Task 2 fields, Task 3 conversion functions, existing `AddTestStep(profileID, testKey, action, data, expected string) (Step, error)`.
- Produces: `type TypeConversion struct { OldType, NewType string; Prefilled, CanPrefill bool }` and `func (r *Repository) ChangeTestType(profileID, testKey, newType string) (TypeConversion, error)`.

- [ ] **Step 1: Write the failing test**

```go
package testrepo

import (
	"strings"
	"testing"
)

func TestChangeTestTypePrefillsEmptyTarget(t *testing.T) {
	repo := newTestRepo(t)
	if err := repo.UpsertTests("p1", []TestCase{{Key: "QA-1", ID: "1", Summary: "Login works", ExecType: "Manual"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddTestStep("p1", "QA-1", "open login", "user=bob", "form shown"); err != nil {
		t.Fatal(err)
	}
	res, err := repo.ChangeTestType("p1", "QA-1", "Cucumber")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Prefilled {
		t.Errorf("expected pre-fill, got %+v", res)
	}
	tc, _ := repo.GetTest("p1", "QA-1")
	if tc.ExecType != "Cucumber" || !strings.Contains(tc.CucumberScenario, "When open login") {
		t.Errorf("type/scenario not set: %+v", tc)
	}
	// Non-destructive: switching back leaves the Manual steps intact.
	steps, _ := repo.GetTestSteps("p1", "QA-1")
	if len(steps) == 0 {
		t.Error("manual steps were destroyed by conversion")
	}
}

func TestChangeTestTypeDoesNotOverwriteExistingBody(t *testing.T) {
	repo := newTestRepo(t)
	repo.UpsertTests("p1", []TestCase{{Key: "QA-2", ID: "2", Summary: "S", ExecType: "Manual"}})
	repo.AddTestStep("p1", "QA-2", "a", "", "b")
	repo.EditTestField("p1", "QA-2", "cucumber_scenario", "Scenario: hand-written\n Given keep me")
	res, err := repo.ChangeTestType("p1", "QA-2", "Cucumber")
	if err != nil {
		t.Fatal(err)
	}
	if res.Prefilled {
		t.Error("must not overwrite non-empty target")
	}
	if !res.CanPrefill {
		t.Error("CanPrefill should be true when a source body exists")
	}
	tc, _ := repo.GetTest("p1", "QA-2")
	if !strings.Contains(tc.CucumberScenario, "hand-written") {
		t.Error("existing scenario was clobbered")
	}
}
```

(Use whatever steps reader the package exposes; if `GetTestSteps` needs a Jira client, substitute the internal cache reader used by other testrepo tests — grep `func (r *Repository) .*Step` for the DB-only reader.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/testrepo/ -run TestChangeTestType -v`
Expected: FAIL — undefined: `ChangeTestType`.

- [ ] **Step 3: Implement the orchestrator**

```go
// TypeConversion reports the outcome of a ChangeTestType call.
type TypeConversion struct {
	OldType    string `json:"oldType"`
	NewType    string `json:"newType"`
	Prefilled  bool   `json:"prefilled"`  // target body was empty and got pre-filled
	CanPrefill bool   `json:"canPrefill"` // a source body exists to pre-fill from (offer opt-in)
}

// ChangeTestType sets a test's Xray Test Type and, when the destination body is
// empty, pre-fills it with a best-effort conversion of the source body. The
// source body is never modified, so switching back is lossless. When the
// destination already has content it is left untouched and CanPrefill signals
// that the caller may offer an explicit pre-fill.
func (r *Repository) ChangeTestType(profileID, testKey, newType string) (TypeConversion, error) {
	tc, err := r.GetTest(profileID, testKey)
	if err != nil {
		return TypeConversion{}, err
	}
	oldType := tc.ExecType
	res := TypeConversion{OldType: oldType, NewType: newType}
	if err := r.EditTestField(profileID, testKey, "exec_type", newType); err != nil {
		return res, err
	}
	if strings.EqualFold(oldType, newType) {
		return res, nil
	}

	targetEmpty, sourceHasBody := r.bodyState(profileID, tc, newType, oldType)
	res.CanPrefill = sourceHasBody
	if !targetEmpty || !sourceHasBody {
		return res, nil
	}
	if err := r.prefillBody(profileID, testKey, tc, oldType, newType); err != nil {
		return res, err
	}
	res.Prefilled = true
	return res, nil
}
```

Add helpers `bodyState` (reports whether the `newType` body is empty and whether the `oldType` body has content — Manual's body is "steps exist") and `prefillBody` (dispatch on `oldType→newType`: for text targets call `EditTestField` with the Task-3 transform; for a Manual target, loop the generated `[]Step` through `AddTestStep`). Read Manual steps via the package's DB-only step reader. Keep both helpers in `testtype.go` next to the transforms.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/testrepo/ -run TestChangeTestType -v`
Expected: PASS. Then `go test ./internal/testrepo/...` — all green.

- [ ] **Step 5: Commit**

```bash
git add internal/testrepo/testrepo.go internal/testrepo/testtype.go internal/testrepo/changetesttype_test.go
git commit -m "feat(testrepo): ChangeTestType with non-destructive body pre-fill"
```

---

### Task 5: Sync read — resolve + parse the three custom fields

**Files:**
- Modify: `internal/jira/customfields.go` (add resolvers next to `testTypeFieldID` 65-70; extend `demoCustomFieldDefs` 417-424)
- Modify: `internal/jira/search.go` (bulk-pull field list ~228-239; `parseIssueTest` 91-120; add text/option parse helpers near `execTypeFromRawFields` 186-198)
- Modify: `internal/jira/client.go` or wherever `type Test struct` lives (add fields — grep `type Test struct`)
- Modify: `internal/syncer/engine.go` (`toRepoTests` 952-968)
- Test: `internal/jira/testtypes_parse_test.go` (create)

**Interfaces:**
- Consumes: `resolveCustomFieldID` (existing).
- Produces: `jira.Test.CucumberScenario`, `.CucumberType`, `.GenericDefinition string`; resolvers `cucumberScenarioFieldID`, `cucumberTypeFieldID`, `genericDefinitionFieldID`; `toRepoTests` copies all three into `testrepo.TestCase`.

- [ ] **Step 1: Write the failing test**

```go
package jira

import (
	"encoding/json"
	"testing"
)

func TestParseIssueTestReadsBodies(t *testing.T) {
	raw := json.RawMessage(`{
		"summary": "S",
		"customfield_20001": "Scenario: x\n Given y",
		"customfield_20002": {"value": "Scenario Outline"},
		"customfield_20003": "com.acme.Foo#bar"
	}`)
	ids := testFieldIDs{Scenario: "customfield_20001", ScenarioType: "customfield_20002", GenericDef: "customfield_20003"}
	got := parseIssueTest("1", "QA-1", raw, "", ids)
	if got.CucumberScenario == "" || got.CucumberType != "Scenario Outline" || got.GenericDefinition == "" {
		t.Errorf("bodies not parsed: %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/jira/ -run TestParseIssueTestReadsBodies -v`
Expected: FAIL — undefined `testFieldIDs` / `parseIssueTest` arity mismatch.

- [ ] **Step 3: Add resolvers + demo defs**

In customfields.go after `testTypeFieldID` (65-70):

```go
func (c *Client) cucumberScenarioFieldID(ctx context.Context) (string, error) {
	return c.resolveCustomFieldID(ctx, "Cucumber Scenario")
}

func (c *Client) cucumberTypeFieldID(ctx context.Context) (string, error) {
	if id, _ := c.resolveCustomFieldID(ctx, "Cucumber Test Type"); id != "" {
		return id, nil
	}
	return c.resolveCustomFieldID(ctx, "Scenario Type") // version alias
}

func (c *Client) genericDefinitionFieldID(ctx context.Context) (string, error) {
	return c.resolveCustomFieldID(ctx, "Generic Test Definition")
}
```

Extend `demoCustomFieldDefs` (417-424) with:

```go
		{ID: "customfield_20001", Name: "Cucumber Scenario", Type: "string"},
		{ID: "customfield_20002", Name: "Cucumber Test Type", Type: "option"},
		{ID: "customfield_20003", Name: "Generic Test Definition", Type: "string"},
```

- [ ] **Step 4: Add `Test` struct fields + parse helpers + signature**

Add `CucumberScenario`, `CucumberType`, `GenericDefinition string` to `type Test struct`. Introduce:

```go
type testFieldIDs struct{ Scenario, ScenarioType, GenericDef string }

func textFromRawFields(rawFields json.RawMessage, fieldID string) string {
	if fieldID == "" || len(rawFields) == 0 {
		return ""
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rawFields, &fields); err != nil {
		return ""
	}
	var s string
	_ = json.Unmarshal(fields[fieldID], &s)
	return s
}
```

Change `parseIssueTest` to `parseIssueTest(id, key string, rawFields json.RawMessage, execTypeID string, ids testFieldIDs) Test` and, before `return t`, set:

```go
	t.CucumberScenario = textFromRawFields(rawFields, ids.Scenario)
	t.CucumberType = execTypeFromRawFields(rawFields, ids.ScenarioType) // reuses parseOptionValue
	t.GenericDefinition = textFromRawFields(rawFields, ids.GenericDef)
```

- [ ] **Step 5: Resolve + request the fields in the bulk pull**

In search.go (~228-239) resolve the three ids (best-effort, mirror the `execTypeID` error handling), append any non-empty id to `fields`, build a `testFieldIDs`, and pass it through to every `parseIssueTest` call site (grep `parseIssueTest(` — update all callers, including live and any single-issue path).

- [ ] **Step 6: Map in `toRepoTests`**

engine.go 966, after `ExecType: t.ExecType,`:

```go
			ExecType:          t.ExecType,
			FixVersions:       t.FixVersions,
			CucumberScenario:  t.CucumberScenario,
			CucumberType:      t.CucumberType,
			GenericDefinition: t.GenericDefinition,
```

- [ ] **Step 7: Run tests**

Run: `go test ./internal/jira/ -run TestParseIssueTestReadsBodies -v` → PASS.
Then `go build ./...` (catches every `parseIssueTest` caller) and `go test ./internal/jira/... ./internal/syncer/...` → green.

- [ ] **Step 8: Commit**

```bash
git add internal/jira/customfields.go internal/jira/search.go internal/jira/client.go internal/jira/testtypes_parse_test.go internal/syncer/engine.go
git commit -m "feat(jira): resolve + parse cucumber/generic custom fields on sync"
```

---

### Task 6: Commit write — inject bodies as custom fields

**Files:**
- Modify: `internal/jira/edit.go` (add sibling helpers to `ExecTypeFieldValue` 70-86)
- Modify: `internal/syncer/commit.go` (test-field PUT block 419-465)
- Test: `internal/jira/testtypes_fieldvalue_test.go` (create)

**Interfaces:**
- Consumes: resolvers from Task 5.
- Produces: `CucumberScenarioFieldValue`, `CucumberTypeFieldValue`, `GenericDefinitionFieldValue` (same shape as `ExecTypeFieldValue`: `(fieldID string, value any, ok bool, err error)`); commit injects them into the Jira PUT when their pending field is present.

- [ ] **Step 1: Write the failing test**

```go
package jira

import (
	"context"
	"testing"
)

func TestBodyFieldValuesDemoResolveEmpty(t *testing.T) {
	c := &Client{baseURL: "demo"} // demo short-circuits resolveCustomFieldID to ""
	for _, call := range []func() (string, any, bool, error){
		func() (string, any, bool, error) { return c.CucumberScenarioFieldValue(context.Background(), "x") },
		func() (string, any, bool, error) { return c.GenericDefinitionFieldValue(context.Background(), "x") },
	} {
		_, _, ok, err := call()
		if err != nil || ok {
			t.Errorf("demo should resolve empty (ok=false,no err); got ok=%v err=%v", ok, err)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/jira/ -run TestBodyFieldValuesDemoResolveEmpty -v`
Expected: FAIL — undefined methods.

- [ ] **Step 3: Add the value helpers**

edit.go, after `ExecTypeFieldValue`:

```go
func (c *Client) CucumberScenarioFieldValue(ctx context.Context, v string) (string, any, bool, error) {
	return c.textCustomFieldValue(ctx, c.cucumberScenarioFieldID, v)
}
func (c *Client) GenericDefinitionFieldValue(ctx context.Context, v string) (string, any, bool, error) {
	return c.textCustomFieldValue(ctx, c.genericDefinitionFieldID, v)
}
func (c *Client) CucumberTypeFieldValue(ctx context.Context, v string) (string, any, bool, error) {
	id, err := c.cucumberTypeFieldID(ctx)
	if err != nil || id == "" {
		return "", nil, false, err
	}
	return id, map[string]string{"value": v}, true, nil
}

func (c *Client) textCustomFieldValue(ctx context.Context, resolve func(context.Context) (string, error), v string) (string, any, bool, error) {
	id, err := resolve(ctx)
	if err != nil || id == "" {
		return "", nil, false, err
	}
	return id, v, true, nil
}
```

- [ ] **Step 4: Inject in commit**

commit.go, inside the `if len(fieldChanges) > 0 || len(customFields) > 0` block right after the `exec_type` injection (~419-465), add a loop that mirrors the exec_type handling for each new field:

```go
			for field, val := range updates {
				var (
					fid      string
					fv       any
					resolved bool
					ferr     error
				)
				switch field {
				case "cucumber_scenario":
					fid, fv, resolved, ferr = e.client.CucumberScenarioFieldValue(ctx, val)
				case "cucumber_type":
					fid, fv, resolved, ferr = e.client.CucumberTypeFieldValue(ctx, val)
				case "generic_definition":
					fid, fv, resolved, ferr = e.client.GenericDefinitionFieldValue(ctx, val)
				default:
					continue
				}
				if ferr != nil {
					log.Printf("xtm: resolve %s field for %s failed, committing without it: %v", field, testKey, ferr)
				} else if resolved {
					fields[fid] = fv
				}
			}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/jira/ -run TestBodyFieldValuesDemoResolveEmpty -v` → PASS.
Then `go test ./internal/jira/... ./internal/syncer/...` → green (regression on commit).

- [ ] **Step 6: Commit**

```bash
git add internal/jira/edit.go internal/syncer/commit.go internal/jira/testtypes_fieldvalue_test.go
git commit -m "feat(syncer): commit cucumber/generic bodies as Jira custom fields"
```

---

### Task 7: Demo seeding — realistic Cucumber/Generic bodies

**Files:**
- Modify: `internal/jira/demo.go` (`makeDemoTest` 924-982)
- Test: `internal/jira/testtypes_demo_test.go` (create)

**Interfaces:**
- Consumes: `demoExecTypeForIndex` (existing).
- Produces: demo Cucumber tests carry non-empty `CucumberScenario` + `CucumberType`; demo Generic tests carry non-empty `GenericDefinition`.

- [ ] **Step 1: Write the failing test**

```go
package jira

import "testing"

func TestDemoTestsHaveTypeBodies(t *testing.T) {
	theme := themeFor("demo")
	var sawCuke, sawGeneric bool
	for i := 0; i < 12; i++ {
		tc := makeDemoTest(theme, "DEMO", i)
		switch tc.ExecType {
		case "Cucumber":
			sawCuke = true
			if tc.CucumberScenario == "" || tc.CucumberType == "" {
				t.Errorf("cucumber demo %d missing scenario/type", i)
			}
		case "Generic":
			sawGeneric = true
			if tc.GenericDefinition == "" {
				t.Errorf("generic demo %d missing definition", i)
			}
		}
	}
	if !sawCuke || !sawGeneric {
		t.Fatal("expected both Cucumber and Generic demo tests within first 12")
	}
}
```

(Confirm the theme accessor name — grep `func themeFor`. If demo uses a different constructor, match it.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/jira/ -run TestDemoTestsHaveTypeBodies -v`
Expected: FAIL — empty scenario/definition.

- [ ] **Step 3: Populate bodies in makeDemoTest**

Before the `return Test{...}` in `makeDemoTest`, compute type-specific bodies and set them on the returned struct:

```go
	execType := demoExecTypeForIndex(i)
	var cukeScenario, cukeType, genericDef string
	switch execType {
	case "Cucumber":
		if i%8 == 0 { // sprinkle a Scenario Outline with Examples
			cukeType = "Scenario Outline"
			cukeScenario = fmt.Sprintf(
				"Scenario Outline: %s\n  Given the %s screen\n  When I <action>\n  Then I see <result>\n\n  Examples:\n    | action | result |\n    | submit | success |\n    | cancel | aborted |\n",
				summary, strings.ToLower(feature))
		} else {
			cukeType = "Scenario"
			cukeScenario = fmt.Sprintf(
				"Scenario: %s\n  Given the %s screen\n  When I %s\n  Then the system responds correctly\n",
				summary, strings.ToLower(feature), strings.ToLower(condition))
		}
	case "Generic":
		genericDef = fmt.Sprintf("com.acme.tests.%sIT#%s", sanitizeIdent(feature), sanitizeIdent(condition))
	}
```

Add the three fields to the returned `Test{...}` literal (`CucumberScenario: cukeScenario, CucumberType: cukeType, GenericDefinition: genericDef`). Add a small `sanitizeIdent` helper (strip non-alphanumerics) in demo.go, or inline `strings.ReplaceAll`-based cleanup if a similar helper already exists (grep `func sanitize` first).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/jira/ -run TestDemoTestsHaveTypeBodies -v` → PASS.
Then `go test ./internal/jira/...` → green.

- [ ] **Step 5: Commit**

```bash
git add internal/jira/demo.go internal/jira/testtypes_demo_test.go
git commit -m "feat(demo): seed sample cucumber scenarios + generic definitions"
```

---

### Task 8: App boundary — expose `ChangeTestType`

**Files:**
- Modify: `app.go` (add method near `EditTestField` 1447-1453)
- Regenerate Wails bindings (via `wails build` or `wails dev` once); or hand-add to `frontend/wailsjs/go/main/App.*` if not running Wails in this session.

**Interfaces:**
- Consumes: `repo.ChangeTestType` (Task 4).
- Produces: `func (a *App) ChangeTestType(profileID, testKey, newType string) (testrepo.TypeConversion, error)` — callable from the frontend as `ChangeTestType(profileId, testKey, newType): Promise<TypeConversion>`.

- [ ] **Step 1: Add the App method**

```go
// ChangeTestType sets a Test's Xray Test Type and, when the destination body is
// empty, pre-fills it with a best-effort conversion of the previous type's body
// (non-destructive — the source body is preserved). Returns what happened so the
// UI can offer an explicit pre-fill when the destination already had content.
func (a *App) ChangeTestType(profileID, testKey, newType string) (tc testrepo.TypeConversion, err error) {
	defer recoverToError("ChangeTestType", &err)
	if err := a.requireStore(); err != nil {
		return testrepo.TypeConversion{}, err
	}
	return a.repo.ChangeTestType(profileID, testKey, newType)
}
```

- [ ] **Step 2: Verify build + regenerate bindings**

Run: `go build ./...` → clean. Then regenerate bindings (`wails dev` briefly, or add the signature to `frontend/wailsjs/go/main/App.d.ts` + `App.js` matching the existing `EditTestField` export shape).

- [ ] **Step 3: Commit**

```bash
git add app.go frontend/wailsjs/go/main/App.d.ts frontend/wailsjs/go/main/App.js
git commit -m "feat(app): expose ChangeTestType binding"
```

---

### Task 9: Frontend — types + type-aware editors in TestDetail

**Files:**
- Modify: `frontend/src/api.ts` (`TestCase` interface 445-459; export `ChangeTestType` + `TypeConversion` type)
- Modify: `frontend/src/components/TestDetail.tsx` (exec-type dropdown 845-868; steps block 1302+; state 235/268)
- Test: manual via `npm run build` + demo E2E (no frontend unit runner)

**Interfaces:**
- Consumes: `App.ChangeTestType`, `TestCase.cucumberScenario/cucumberType/genericDefinition`.
- Produces: type-conditional editor UI; `exec_type` change routed through `ChangeTestType`.

- [ ] **Step 1: Extend the api.ts TestCase type + export binding**

In `TestCase` (api.ts 445-459), after `execType`:

```typescript
  execType: string;
  cucumberScenario: string;
  cucumberType: string;
  genericDefinition: string;
```

Add near the other `App` re-exports:

```typescript
export interface TypeConversion {
  oldType: string;
  newType: string;
  prefilled: boolean;
  canPrefill: boolean;
}
export { ChangeTestType } from "../wailsjs/go/main/App";
```

- [ ] **Step 2: Add state + load for the new bodies (TestDetail.tsx)**

Beside `const [execType, setExecType] = useState("");` (235) add `cucumberScenario`, `cucumberType`, `genericDefinition` state; in the load effect (near 268) hydrate them from `t.cucumberScenario ?? ""` etc.

- [ ] **Step 3: Route the type dropdown through ChangeTestType**

Replace the dropdown `onChange` (851-857) so it calls the binding and reloads bodies, offering pre-fill when the backend reports `canPrefill && !prefilled`:

```tsx
                  onChange={async (e) => {
                    const next = e.target.value;
                    setExecType(next);
                    const res = await ChangeTestType(profileId, testKey, next);
                    if (res.prefilled || res.canPrefill) {
                      await reloadTest(); // re-fetch so pre-filled body shows
                    }
                    if (res.canPrefill && !res.prefilled) {
                      setPrefillOffer(res); // render an opt-in banner (Step 5)
                    }
                  }}
```

(Use the component's existing test-reload path — grep the current `saveField`/refresh flow in TestDetail.tsx for the right reloader; `saveField("exec_type", …)` is replaced by this call.)

- [ ] **Step 4: Conditional editor**

Wrap the Steps block (1302+) so it renders only for Manual/Automated/empty, and add the two alternate editors:

```tsx
{execType === "Cucumber" ? (
  <section className="cuke-editor">
    <label>Scenario type
      <select value={cucumberType}
        onChange={(e) => { setCucumberType(e.target.value); saveField("cucumber_type", e.target.value); }}>
        <option value="Scenario">Scenario</option>
        <option value="Scenario Outline">Scenario Outline</option>
      </select>
    </label>
    <textarea className="cuke-scenario mono" value={cucumberScenario}
      readOnly={readOnly}
      onChange={(e) => setCucumberScenario(e.target.value)}
      onBlur={() => saveField("cucumber_scenario", cucumberScenario)} />
  </section>
) : execType === "Generic" ? (
  <section className="generic-editor">
    <textarea className="generic-def mono" value={genericDefinition}
      readOnly={readOnly}
      onChange={(e) => setGenericDefinition(e.target.value)}
      onBlur={() => saveField("generic_definition", genericDefinition)} />
  </section>
) : (
  <>{/* existing Steps <h4> + step rows unchanged */}</>
)}
```

- [ ] **Step 5: Pre-fill opt-in banner + styles**

Render a small banner when `prefillOffer` is set ("Fill this from the previous type? [Pre-fill] [Dismiss]") whose Pre-fill button re-invokes the type change to force pre-fill — simplest: call `ChangeTestType` again after clearing is not desired; instead expose the pre-fill by calling a dedicated path. For v1, the banner's Pre-fill button calls `ChangeTestType(profileId, testKey, execType)` only when the target is empty; since target is non-empty here, keep the button as a no-op-safe "Dismiss"-only notice if a force-prefill method isn't added. Add `.mono { font-family: monospace; white-space: pre; }` and editor spacing to `App.css`.

- [ ] **Step 6: Verify build**

Run: `cd frontend; npm run build`
Expected: tsc + vite succeed, no type errors.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/api.ts frontend/src/components/TestDetail.tsx frontend/src/App.css
git commit -m "feat(ui): type-aware editors for cucumber + generic tests"
```

---

### Task 10: End-to-end verification + PR

**Files:** none (verification) — then open PR.

- [ ] **Step 1: Full backend suite**

Run: `go build ./... && go test ./...`
Expected: all green; Manual step tests unaffected.

- [ ] **Step 2: Frontend build**

Run: `cd frontend; npm run build`
Expected: clean.

- [ ] **Step 3: Demo E2E (manual)**

Run `wails dev`, create a `demo` profile, sync. Confirm: a Cucumber test shows its scenario + scenario-type select and edits persist; a Generic test shows its definition; switching a Manual test to Cucumber pre-fills a reviewable Gherkin skeleton and switching back preserves the steps.

- [ ] **Step 4: Push + PR**

```bash
git push -u origin feature/test-types-cucumber-generic
```

Then open a PR into `main` (title: "feat: Cucumber & Generic test type support"; body summarizes the design doc; **no AI-attribution trailer / no "Generated with" footer**).

---

## Self-Review

**Spec coverage:**
- Section 1 (data model, field resolution, sync read, commit write) → Tasks 1, 2, 5, 6. ✓
- Section 2 (conversion transforms, UI conditional, `EditTestField` reuse) → Tasks 3, 4, 9. ✓
- Section 3 (demo seeding, tests, scope cuts) → Task 7 + tests throughout; scope cuts honored (no creation, no bulk, no lint/import, plain textarea). ✓
- Non-destructive pre-fill (decision 2-A) → Task 4 (`bodyState`/`prefillBody`, "switch back preserves steps" test). ✓
- Columns storage (decision 1-A) → Task 1. ✓
- **Correction vs spec:** spec said "schema 39→40 with `if current < 40`"; real base is v40 with shared version numbers, so the plan uses duplicate-tolerant unconditional ALTERs + bump to **41** (Task 1, Global Constraints). Pending `entity_type` is `entityTestCase` (via `EditTestField`), not new `test_cucumber`/`test_generic` types — simpler and covered.

**Placeholder scan:** No TBD/TODO. Two spots delegate to a grep-and-match ("use the package's DB-only step reader", "match the existing reload path") rather than inventing a symbol — intentional, because those helpers exist and must be located, not created; each names the exact grep and the sibling to mirror.

**Type consistency:** `TypeConversion` fields (`OldType/NewType/Prefilled/CanPrefill`) are consistent Go↔TS (`oldType/newType/prefilled/canPrefill`). `testFieldIDs{Scenario,ScenarioType,GenericDef}` used consistently in Task 5. `ChangeTestType(profileID, testKey, newType)` signature identical in repo (Task 4), app (Task 8), and frontend (Task 9). Conversion function names match between Task 3 definitions and Task 4 usage.
