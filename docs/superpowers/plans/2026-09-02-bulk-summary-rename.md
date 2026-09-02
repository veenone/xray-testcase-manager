# Bulk Summary Rename (`-354`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user add a common prefix, suffix, or both to the summaries of every selected test, previewing the exact result before applying it.

**Architecture:** The rename rule is a pure TypeScript module. The modal computes each new summary once, previews that exact string, and sends it. Two new bound Go methods read current summaries for a key list and write the renames as ordinary `summary` field edits, so the pending-change journal and commit path are untouched.

**Tech Stack:** Go 1.25, `modernc.org/sqlite`, Wails v2.15, React 19 + TypeScript, Vitest + jsdom + Testing Library, TanStack Query.

**Spec:** `docs/superpowers/specs/2026-09-02-bulk-summary-rename-design.md`

## Global Constraints

- `SUMMARY_MAX = 255`. Jira's issue-summary limit; a computed summary longer than this is excluded from the apply.
- `PREVIEW_LIMIT = 200`. Maximum preview rows rendered. Counts are always computed across the whole selection, never across the truncated list.
- Affix matching is **exact and case-sensitive**. `[SMOKE]` and `[smoke]` are different prefixes.
- **No automatic separator.** The affix is concatenated literally.
- Only rows in state `changed` are sent to the backend.
- `frontend/wailsjs/` is generated. Never hand-edit. Tasks 1 and 2 change Go signatures, so regenerate with `wails generate module` and update `frontend/src/api.ts` to match.
- `internal/` is import-private to this module.
- Jira is the system of record. The local store is a cache plus a pending-change journal.
- Credentials never reach the database, a log line, or an error string.
- Go formatting is `gofmt`. Every task ends green: `go build ./...`, `go test ./...`, `cd frontend && npm test && npm run build`.
- No AI attribution in commit messages. No `Co-Authored-By` trailer.

**Correction to the spec:** the spec says the counts sit in "the `LiveRegion` component". `LiveRegion` is actually a mounted-once singleton driven by a global `announce(message)` function (`frontend/src/components/LiveRegion.tsx`), not a nestable region. Announcing on every keystroke would be unusable. This plan renders a local `aria-live="polite"` element for the counts and calls `announce()` once for the terminal apply result, matching the other bulk modals.

---

### Task 1: Read current summaries for a key list

**Files:**
- Modify: `internal/testrepo/testrepo.go` (add `TestSummary` and `ListTestSummaries`)
- Modify: `app.go` (add the bound `GetTestSummaries`, beside `BulkEditTests` at `:2284`)
- Test: `internal/testrepo/summaries_test.go` (create)

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type testrepo.TestSummary struct { Key string \`json:"key"\`; Summary string \`json:"summary"\` }`
  - `func (r *Repository) ListTestSummaries(profileID string, testKeys []string) ([]TestSummary, error)`
  - `func (a *App) GetTestSummaries(profileID string, testKeys []string) ([]testrepo.TestSummary, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/testrepo/summaries_test.go`:

```go
package testrepo_test

import (
	"testing"

	"xray-test-manager/internal/testrepo"
)

func seedSummaryTests(t *testing.T) *testrepo.Repository {
	t.Helper()
	repo := newRepo(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "Login works"},
		{Key: "QA-2", ID: "2", Summary: "Logout works"},
		{Key: "QA-3", ID: "3", Summary: "Reset works"},
	}); err != nil {
		t.Fatalf("seed tests: %v", err)
	}
	return repo
}

func TestListTestSummariesReturnsRequestOrder(t *testing.T) {
	repo := seedSummaryTests(t)

	got, err := repo.ListTestSummaries("p1", []string{"QA-3", "QA-1"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}
	// Request order, not table order: the modal renders in the order the user
	// selected, and re-sorting here would silently reorder the preview.
	if got[0].Key != "QA-3" || got[1].Key != "QA-1" {
		t.Fatalf("got order %s,%s want QA-3,QA-1", got[0].Key, got[1].Key)
	}
	if got[0].Summary != "Reset works" {
		t.Errorf("QA-3 summary = %q, want %q", got[0].Summary, "Reset works")
	}
}

func TestListTestSummariesOmitsUnknownKeys(t *testing.T) {
	// A key can vanish between selection and modal open (a sync deleted it).
	// That must cost one row, not the whole dialog.
	repo := seedSummaryTests(t)

	got, err := repo.ListTestSummaries("p1", []string{"QA-1", "QA-NOPE"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].Key != "QA-1" {
		t.Fatalf("got %+v, want only QA-1", got)
	}
}

func TestListTestSummariesIsProfileScoped(t *testing.T) {
	repo := seedSummaryTests(t)

	got, err := repo.ListTestSummaries("p2", []string{"QA-1"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %+v, want nothing for another profile", got)
	}
}

func TestListTestSummariesEmptyInput(t *testing.T) {
	repo := seedSummaryTests(t)

	got, err := repo.ListTestSummaries("p1", nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %+v, want an empty result", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/testrepo/ -run TestListTestSummaries -v`

Expected: compile failure, `repo.ListTestSummaries undefined`.

- [ ] **Step 3: Add the type and the store method**

In `internal/testrepo/testrepo.go`, beside the other read helpers, add:

```go
// TestSummary is a Test's key and current summary, the minimum the bulk-rename
// preview needs (RND_P_4TFINT_05-354). No existing read covers an arbitrary key
// list: the paged query is filter-driven, and the per-test reads take one key.
type TestSummary struct {
	Key     string `json:"key"`
	Summary string `json:"summary"`
}

// ListTestSummaries returns the current summary for each requested key, in the
// order requested. Keys the profile does not have are omitted rather than
// erroring: a Test can disappear between selection and use, and that should
// cost one row rather than the whole operation.
func (r *Repository) ListTestSummaries(profileID string, testKeys []string) ([]TestSummary, error) {
	out := []TestSummary{}
	if len(testKeys) == 0 {
		return out, nil
	}

	placeholders := make([]string, len(testKeys))
	args := make([]any, 0, len(testKeys)+1)
	args = append(args, profileID)
	for i, k := range testKeys {
		placeholders[i] = "?"
		args = append(args, k)
	}

	rows, err := r.db.Query(
		fmt.Sprintf(
			`SELECT jira_key, summary FROM test_case
			 WHERE profile_id = ? AND jira_key IN (%s)`,
			strings.Join(placeholders, ", "),
		), args...)
	if err != nil {
		return nil, fmt.Errorf("list test summaries: %w", err)
	}
	defer rows.Close()

	found := make(map[string]string, len(testKeys))
	for rows.Next() {
		var k, s string
		if err := rows.Scan(&k, &s); err != nil {
			return nil, err
		}
		found[k] = s
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Re-emit in request order so the preview matches the user's selection
	// order rather than SQLite's scan order.
	for _, k := range testKeys {
		if s, ok := found[k]; ok {
			out = append(out, TestSummary{Key: k, Summary: s})
		}
	}
	return out, nil
}
```

`fmt` and `strings` are already imported in this file.

- [ ] **Step 4: Bind it**

In `app.go`, directly after `BulkEditTests` (ends at `:2294`), add:

```go
// GetTestSummaries returns the current summary of each given Test, in the order
// requested, for the bulk-rename preview (RND_P_4TFINT_05-354). Keys this
// profile does not have are omitted.
func (a *App) GetTestSummaries(profileID string, testKeys []string) ([]testrepo.TestSummary, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.ListTestSummaries(profileID, testKeys)
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `gofmt -w . && go build ./... && go test ./internal/testrepo/ -run TestListTestSummaries -v`

Expected: all four PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/testrepo/testrepo.go internal/testrepo/summaries_test.go app.go
git commit -m "feat(testrepo): read current summaries for a list of test keys (-354)

The bulk-rename preview needs current summaries for an arbitrary key list.
Every existing read is either one test by key or a filter-driven page, and a
selection can span pages. Results come back in request order so the preview
matches the user's selection order, and unknown keys are omitted rather than
failing the whole call."
```

---

### Task 2: Write the renames as pending summary edits

**Files:**
- Modify: `internal/testrepo/testrepo.go` (add `TestRename` and `BulkRenameTests`)
- Modify: `app.go` (add the bound `BulkRenameTests`)
- Test: `internal/testrepo/bulkrename_test.go` (create)

**Interfaces:**
- Consumes: `newRepo(t)` from `internal/testrepo/testrepo_test.go:13`.
- Produces:
  - `type testrepo.TestRename struct { Key string \`json:"key"\`; Summary string \`json:"summary"\` }`
  - `func (r *Repository) BulkRenameTests(profileID string, renames []TestRename) (BulkEditResult, error)`
  - `func (a *App) BulkRenameTests(profileID string, renames []testrepo.TestRename) (testrepo.BulkEditResult, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/testrepo/bulkrename_test.go`:

```go
package testrepo_test

import (
	"encoding/json"
	"testing"

	"xray-test-manager/internal/testrepo"
)

func seedRenameTests(t *testing.T) *testrepo.Repository {
	t.Helper()
	repo := newRepo(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "Login works"},
		{Key: "QA-2", ID: "2", Summary: "Logout works"},
	}); err != nil {
		t.Fatalf("seed tests: %v", err)
	}
	return repo
}

func TestBulkRenameTestsQueuesSummaryEdits(t *testing.T) {
	repo := seedRenameTests(t)

	res, err := repo.BulkRenameTests("p1", []testrepo.TestRename{
		{Key: "QA-1", Summary: "[SMOKE] Login works"},
		{Key: "QA-2", Summary: "[SMOKE] Logout works"},
	})
	if err != nil {
		t.Fatalf("bulk rename: %v", err)
	}
	if len(res.Succeeded) != 2 {
		t.Fatalf("succeeded %v, want both", res.Succeeded)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("failed %+v, want none", res.Failed)
	}

	// The rename must be an ordinary summary field edit, so the existing
	// journal and commit path carry it with no special casing.
	pcs, err := repo.ListPendingChanges("p1")
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	got := map[string]string{}
	for _, pc := range pcs {
		if pc.Field == "summary" {
			got[pc.EntityKey] = pc.AfterVal
		}
	}
	if got["QA-1"] != "[SMOKE] Login works" {
		t.Errorf("QA-1 pending after = %q", got["QA-1"])
	}
	if got["QA-2"] != "[SMOKE] Logout works" {
		t.Errorf("QA-2 pending after = %q", got["QA-2"])
	}
	_ = json.Marshal // keep the import honest if the shape changes
}

func TestBulkRenameTestsReportsUnknownKeyWithoutStoppingSiblings(t *testing.T) {
	repo := seedRenameTests(t)

	res, err := repo.BulkRenameTests("p1", []testrepo.TestRename{
		{Key: "QA-1", Summary: "[SMOKE] Login works"},
		{Key: "QA-GONE", Summary: "[SMOKE] Nothing"},
	})
	if err != nil {
		t.Fatalf("bulk rename: %v", err)
	}
	if len(res.Succeeded) != 1 || res.Succeeded[0] != "QA-1" {
		t.Errorf("succeeded %v, want just QA-1", res.Succeeded)
	}
	if len(res.Failed) != 1 || res.Failed[0].TestKey != "QA-GONE" {
		t.Errorf("failed %+v, want just QA-GONE", res.Failed)
	}
}

func TestBulkRenameTestsEmptyListIsNoOp(t *testing.T) {
	repo := seedRenameTests(t)

	res, err := repo.BulkRenameTests("p1", nil)
	if err != nil {
		t.Fatalf("bulk rename: %v", err)
	}
	if len(res.Succeeded) != 0 || len(res.Failed) != 0 {
		t.Errorf("got %+v, want an empty result", res)
	}
	pcs, err := repo.ListPendingChanges("p1")
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pcs) != 0 {
		t.Errorf("got %d pending changes, want none", len(pcs))
	}
}

func TestBulkRenameTestsToTheSameSummaryQueuesNothing(t *testing.T) {
	// EditTestField drops a change that returns a field to its original value.
	// Renaming to the current summary must therefore leave no pending row.
	repo := seedRenameTests(t)

	if _, err := repo.BulkRenameTests("p1", []testrepo.TestRename{
		{Key: "QA-1", Summary: "Login works"},
	}); err != nil {
		t.Fatalf("bulk rename: %v", err)
	}
	pcs, err := repo.ListPendingChanges("p1")
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	for _, pc := range pcs {
		if pc.EntityKey == "QA-1" && pc.Field == "summary" {
			t.Errorf("a no-op rename queued a pending change: %+v", pc)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/testrepo/ -run TestBulkRename -v`

Expected: compile failure, `repo.BulkRenameTests undefined`.

- [ ] **Step 3: Add the type and the store method**

In `internal/testrepo/testrepo.go`, beside `BulkEditTests`, add:

```go
// TestRename is one Test's new summary (RND_P_4TFINT_05-354). Unlike BulkEdit,
// which carries one Value applied to every Test, a rename carries a different
// value per Test, because the prefix / suffix rule produces a different result
// for each summary.
type TestRename struct {
	Key     string `json:"key"`
	Summary string `json:"summary"`
}

// BulkRenameTests applies a precomputed summary to each Test, queueing each as
// an ordinary "summary" field edit so the pending-change journal, conflict
// detection and commit path need no special casing.
//
// The rename rule itself lives in the frontend (frontend/src/lib/rename.ts):
// the modal previews the exact string it sends, so preview and result cannot
// disagree. This method deliberately does not re-derive anything.
func (r *Repository) BulkRenameTests(profileID string, renames []TestRename) (BulkEditResult, error) {
	result := BulkEditResult{Succeeded: []string{}, Failed: []BulkFailure{}}
	for _, rn := range renames {
		if err := r.EditTestField(profileID, rn.Key, "summary", rn.Summary); err != nil {
			result.Failed = append(result.Failed, BulkFailure{
				TestKey: rn.Key,
				Error:   err.Error(),
			})
			continue
		}
		result.Succeeded = append(result.Succeeded, rn.Key)
	}
	return result, nil
}
```

Check `BulkFailure`'s field names against its definition near `BulkEditResult` in the same file and match them exactly rather than trusting the names above.

- [ ] **Step 4: Bind it**

In `app.go`, directly after `GetTestSummaries` from Task 1, add:

```go
// BulkRenameTests applies a precomputed summary to each given Test
// (RND_P_4TFINT_05-354). The prefix / suffix rule runs in the frontend, which
// previews the exact strings it sends here.
func (a *App) BulkRenameTests(profileID string, renames []testrepo.TestRename) (result testrepo.BulkEditResult, err error) {
	defer recoverToError("BulkRenameTests", &err)
	empty := testrepo.BulkEditResult{
		Succeeded: []string{},
		Failed:    []testrepo.BulkFailure{},
	}
	if err := a.requireStore(); err != nil {
		return empty, err
	}
	return a.repo.BulkRenameTests(profileID, renames)
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `gofmt -w . && go build ./... && go test ./internal/testrepo/ -run TestBulkRename -v`

Expected: all four PASS. If `TestBulkRenameTestsToTheSameSummaryQueuesNothing` fails, read `EditTestField`'s revert-to-original handling and adjust the test to the real behaviour rather than changing the implementation: the point of that test is to document what happens, not to force a behaviour.

- [ ] **Step 6: Regenerate the bindings and re-export**

```bash
wails generate module
```

Confirm `frontend/wailsjs/go/main/App.d.ts` declares `GetTestSummaries` and `BulkRenameTests`, and that `frontend/wailsjs/go/models.ts` contains `TestSummary` and `TestRename`.

Then in `frontend/src/api.ts`, add both names to the existing single `export { ... }` block (it starts at line 7), and add the matching hand-written interfaces beside the other data shapes, because `api.ts` declares its own types rather than re-exporting the generated classes:

```ts
// TestSummary mirrors testrepo.TestSummary — a Test's key and current summary,
// the minimum the bulk-rename preview needs (-354).
export interface TestSummary {
  key: string;
  summary: string;
}

// TestRename mirrors testrepo.TestRename — one Test's precomputed new summary.
export interface TestRename {
  key: string;
  summary: string;
}
```

- [ ] **Step 7: Commit**

```bash
git add internal/testrepo/testrepo.go internal/testrepo/bulkrename_test.go app.go frontend/src/api.ts frontend/wailsjs
git commit -m "feat(testrepo): apply precomputed summaries to many tests (-354)

BulkEdit carries one Value for every Test; a prefix / suffix rename produces
a different value per Test, so this takes an explicit per-Test summary rather
than overloading BulkEdit with a magic encoding.

Each rename is queued as an ordinary summary field edit, so the journal,
conflict detection and commit path carry it unchanged. An unknown key is
reported as a failure while its siblings still succeed."
```

---

### Task 3: The rename rule

**Files:**
- Create: `frontend/src/lib/rename.ts`
- Test: `frontend/src/lib/rename.test.ts` (create)

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `const SUMMARY_MAX = 255`
  - `type RenameState = "changed" | "unchanged" | "too-long"`
  - `interface RenameRow { key: string; before: string; after: string; state: RenameState; reason?: string }`
  - `function computeRenames(tests: { key: string; summary: string }[], opts: { prefix: string; suffix: string }): RenameRow[]`
  - `function renameCounts(rows: RenameRow[]): { changed: number; unchanged: number; tooLong: number }`

- [ ] **Step 1: Write the failing test**

Create `frontend/src/lib/rename.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { computeRenames, renameCounts, SUMMARY_MAX } from "./rename";

const t = (key: string, summary: string) => ({ key, summary });

describe("computeRenames", () => {
  it("leaves everything unchanged when both affixes are empty", () => {
    const rows = computeRenames([t("QA-1", "Login works")], {
      prefix: "",
      suffix: "",
    });
    expect(rows[0].state).toBe("unchanged");
    expect(rows[0].after).toBe("Login works");
  });

  it("adds a prefix literally, with no separator", () => {
    const rows = computeRenames([t("QA-1", "Login works")], {
      prefix: "[SMOKE]",
      suffix: "",
    });
    expect(rows[0].state).toBe("changed");
    expect(rows[0].after).toBe("[SMOKE]Login works");
  });

  it("adds a suffix", () => {
    const rows = computeRenames([t("QA-1", "Login works")], {
      prefix: "",
      suffix: " (v2)",
    });
    expect(rows[0].after).toBe("Login works (v2)");
  });

  it("adds both affixes", () => {
    const rows = computeRenames([t("QA-1", "Login works")], {
      prefix: "[A] ",
      suffix: " [B]",
    });
    expect(rows[0].after).toBe("[A] Login works [B]");
  });

  it("does not re-add a prefix the summary already has", () => {
    const rows = computeRenames([t("QA-1", "[SMOKE] Login works")], {
      prefix: "[SMOKE] ",
      suffix: "",
    });
    expect(rows[0].state).toBe("unchanged");
    expect(rows[0].after).toBe("[SMOKE] Login works");
    expect(rows[0].reason).toMatch(/prefix/i);
  });

  it("does not re-add a suffix the summary already has", () => {
    const rows = computeRenames([t("QA-1", "Login works (v2)")], {
      prefix: "",
      suffix: " (v2)",
    });
    expect(rows[0].state).toBe("unchanged");
    expect(rows[0].reason).toMatch(/suffix/i);
  });

  it("still adds the suffix when only the prefix is already present", () => {
    const rows = computeRenames([t("QA-1", "[A] Login works")], {
      prefix: "[A] ",
      suffix: " [B]",
    });
    expect(rows[0].state).toBe("changed");
    expect(rows[0].after).toBe("[A] Login works [B]");
  });

  it("is case-sensitive, so a different case is a different prefix", () => {
    const rows = computeRenames([t("QA-1", "[smoke] Login")], {
      prefix: "[SMOKE] ",
      suffix: "",
    });
    expect(rows[0].state).toBe("changed");
    expect(rows[0].after).toBe("[SMOKE] [smoke] Login");
  });

  it("accepts a result of exactly the maximum length", () => {
    const before = "x".repeat(SUMMARY_MAX - 3);
    const rows = computeRenames([t("QA-1", before)], {
      prefix: "abc",
      suffix: "",
    });
    expect(rows[0].after).toHaveLength(SUMMARY_MAX);
    expect(rows[0].state).toBe("changed");
  });

  it("flags a result one character over the maximum", () => {
    const before = "x".repeat(SUMMARY_MAX - 3);
    const rows = computeRenames([t("QA-1", before)], {
      prefix: "abcd",
      suffix: "",
    });
    expect(rows[0].state).toBe("too-long");
    // The computed value is kept so the preview can show what would happen.
    expect(rows[0].after).toHaveLength(SUMMARY_MAX + 1);
  });

  it("counts length in characters, not UTF-16 code units", () => {
    // An emoji is two code units but one character to a person and to Jira's
    // limit as users perceive it. Using [...s].length keeps them in step.
    const before = "🙂".repeat(SUMMARY_MAX - 1);
    const rows = computeRenames([t("QA-1", before)], { prefix: "x", suffix: "" });
    expect(rows[0].state).toBe("changed");
  });

  it("returns nothing for an empty test list", () => {
    expect(computeRenames([], { prefix: "x", suffix: "" })).toEqual([]);
  });

  it("preserves input order", () => {
    const rows = computeRenames(
      [t("QA-3", "c"), t("QA-1", "a"), t("QA-2", "b")],
      { prefix: "p", suffix: "" },
    );
    expect(rows.map((r) => r.key)).toEqual(["QA-3", "QA-1", "QA-2"]);
  });
});

describe("renameCounts", () => {
  it("tallies each state", () => {
    const rows = computeRenames(
      [
        t("QA-1", "Login works"),
        t("QA-2", "[A] Logout works"),
        t("QA-3", "x".repeat(SUMMARY_MAX)),
      ],
      { prefix: "[A] ", suffix: "" },
    );
    expect(renameCounts(rows)).toEqual({ changed: 1, unchanged: 1, tooLong: 1 });
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd frontend && npx vitest run src/lib/rename.test.ts`

Expected: FAIL, cannot resolve `./rename`.

- [ ] **Step 3: Write the module**

Create `frontend/src/lib/rename.ts`:

```ts
// The bulk-rename rule (RND_P_4TFINT_05-354).
//
// This lives in TypeScript rather than Go on purpose. The modal previews the
// result live, so a Go implementation would mean the same rule written twice,
// once for the preview and once for the apply, and the two would drift. The
// modal computes each new summary here, shows exactly that string, and sends
// exactly that string. See the design spec's "Where the rule lives".

// SUMMARY_MAX is Jira's issue-summary limit. A computed summary longer than
// this is reported as too-long and excluded from the apply.
export const SUMMARY_MAX = 255;

export type RenameState = "changed" | "unchanged" | "too-long";

export interface RenameRow {
  key: string;
  before: string;
  after: string;
  state: RenameState;
  /** Why the row is unchanged or too long, shown in the preview. */
  reason?: string;
}

export interface RenameInput {
  key: string;
  summary: string;
}

export interface RenameOptions {
  prefix: string;
  suffix: string;
}

// charLength counts characters rather than UTF-16 code units, so an emoji or
// any astral character counts once, the way a person reading the summary counts
// it. "x".length and [..."x"].length differ the moment a summary holds one.
function charLength(s: string): string[] | never[] | string[] {
  return [...s];
}

// computeRenames applies the affixes to every test, in input order, and
// classifies each result. An affix the summary already carries is not added
// again, so the operation is safe to run twice over a selection that mixes
// already-renamed and new tests.
export function computeRenames(
  tests: RenameInput[],
  opts: RenameOptions,
): RenameRow[] {
  const { prefix, suffix } = opts;

  return tests.map((t) => {
    const before = t.summary;

    if (prefix === "" && suffix === "") {
      return { key: t.key, before, after: before, state: "unchanged" as const };
    }

    const hasPrefix = prefix !== "" && before.startsWith(prefix);
    const hasSuffix = suffix !== "" && before.endsWith(suffix);

    const addPrefix = prefix !== "" && !hasPrefix ? prefix : "";
    const addSuffix = suffix !== "" && !hasSuffix ? suffix : "";

    if (addPrefix === "" && addSuffix === "") {
      const parts: string[] = [];
      if (hasPrefix) parts.push("prefix");
      if (hasSuffix) parts.push("suffix");
      return {
        key: t.key,
        before,
        after: before,
        state: "unchanged" as const,
        reason: `already has this ${parts.join(" and ")}`,
      };
    }

    const after = addPrefix + before + addSuffix;

    if (charLength(after).length > SUMMARY_MAX) {
      return {
        key: t.key,
        before,
        after,
        state: "too-long" as const,
        reason: `over ${SUMMARY_MAX} characters`,
      };
    }

    return { key: t.key, before, after, state: "changed" as const };
  });
}

// renameCounts tallies the states for the summary line above the preview. It is
// always called with every row, never the truncated render list, so the counts
// describe the whole selection.
export function renameCounts(rows: RenameRow[]): {
  changed: number;
  unchanged: number;
  tooLong: number;
} {
  let changed = 0;
  let unchanged = 0;
  let tooLong = 0;
  for (const r of rows) {
    if (r.state === "changed") changed++;
    else if (r.state === "unchanged") unchanged++;
    else tooLong++;
  }
  return { changed, unchanged, tooLong };
}
```

Simplify `charLength` to `function charLength(s: string): string[] { return [...s]; }` when writing it out; the union in the signature above is a typo guard, not intent.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd frontend && npx vitest run src/lib/rename.test.ts`

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/rename.ts frontend/src/lib/rename.test.ts
git commit -m "feat(frontend): the bulk-rename prefix and suffix rule (-354)

A pure module so the rule has one home and its own tests. An affix the
summary already carries is not added again, so a rename is safe to run twice
over a selection that mixes renamed and new tests. Length is counted in
characters rather than UTF-16 code units, so an emoji counts once."
```

---

### Task 4: The modal

**Files:**
- Create: `frontend/src/components/BulkRenameModal.tsx`
- Create: `frontend/src/queries/summaries.ts`
- Modify: `frontend/src/queries/keys.ts` (add the query key)
- Modify: `frontend/src/App.css` (the modal's own rules)
- Test: `frontend/src/components/BulkRenameModal.test.tsx` (create)

**Interfaces:**
- Consumes: `computeRenames`, `renameCounts`, `SUMMARY_MAX`, `RenameRow` from Task 3; `GetTestSummaries`, `BulkRenameTests`, `TestSummary`, `TestRename` from Tasks 1 and 2.
- Produces: `function BulkRenameModal(props: { testKeys: string[]; onComplete: (r: BulkEditResult) => void; onCancel: () => void })`

- [ ] **Step 1: Add the query**

In `frontend/src/queries/keys.ts`, add to the existing key factory, matching the file's established shape:

```ts
  testSummaries: (profileId: string, testKeys: string[]) =>
    ["testSummaries", profileId, [...testKeys].sort().join(",")] as const,
```

The keys are sorted into the cache key so the same selection made in a different order reuses one cache entry.

Create `frontend/src/queries/summaries.ts`, following the shape of a neighbouring file in `frontend/src/queries/`:

```ts
import { useQuery } from "@tanstack/react-query";
import { GetTestSummaries } from "../api";
import type { TestSummary } from "../api";
import { keys } from "./keys";

// useTestSummaries loads the current summary of each selected Test for the
// bulk-rename preview (-354). One fetch per selection; the preview then
// recomputes locally on every keystroke with no further I/O.
export function useTestSummaries(profileId: string, testKeys: string[]) {
  return useQuery<TestSummary[]>({
    queryKey: keys.testSummaries(profileId, testKeys),
    queryFn: () => GetTestSummaries(profileId, testKeys),
    enabled: !!profileId && testKeys.length > 0,
  });
}
```

- [ ] **Step 2: Write the failing component test**

This is the repo's first component test; existing tests cover `contexts/`, `queries/` and `lib/`. Create `frontend/src/components/BulkRenameModal.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BulkRenameModal } from "./BulkRenameModal";

const bulkRename = vi.fn();

vi.mock("../api", () => ({
  GetTestSummaries: vi.fn(async () => [
    { key: "QA-1", summary: "Login works" },
    { key: "QA-2", summary: "[SMOKE] Logout works" },
  ]),
  BulkRenameTests: (...args: unknown[]) => bulkRename(...args),
  errMsg: (e: unknown) => String(e),
}));

vi.mock("../contexts/ProfileContext", () => ({
  useProfile: () => ({ activeId: "p1" }),
}));

function renderModal() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <BulkRenameModal
        testKeys={["QA-1", "QA-2"]}
        onComplete={() => {}}
        onCancel={() => {}}
      />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  bulkRename.mockReset();
  bulkRename.mockResolvedValue({ succeeded: ["QA-1"], failed: [] });
});

describe("BulkRenameModal", () => {
  it("previews the prefix on each summary as you type", async () => {
    renderModal();
    await screen.findByText("Login works");

    await userEvent.type(screen.getByLabelText(/prefix/i), "[SMOKE] ");

    await waitFor(() =>
      expect(screen.getByText("[SMOKE] Login works")).toBeInTheDocument(),
    );
  });

  it("sends only the rows that actually change", async () => {
    renderModal();
    await screen.findByText("Login works");

    await userEvent.type(screen.getByLabelText(/prefix/i), "[SMOKE] ");
    await userEvent.click(screen.getByRole("button", { name: /rename/i }));

    await waitFor(() => expect(bulkRename).toHaveBeenCalledTimes(1));
    // QA-2 already carries the prefix, so it must not be in the payload.
    expect(bulkRename).toHaveBeenCalledWith("p1", [
      { key: "QA-1", summary: "[SMOKE] Login works" },
    ]);
  });

  it("disables apply until something would change", async () => {
    renderModal();
    await screen.findByText("Login works");

    expect(screen.getByRole("button", { name: /rename/i })).toBeDisabled();
  });
});
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd frontend && npx vitest run src/components/BulkRenameModal.test.tsx`

Expected: FAIL, cannot resolve `./BulkRenameModal`.

`@testing-library/react` (^16.3.2), `@testing-library/jest-dom` (^7.0.1), `vitest` (^4.1.11) and `jsdom` (^29.1.1) are already dev dependencies. `@testing-library/user-event` is **not**, so add it in this step:

```bash
cd frontend && npm i -D @testing-library/user-event
```

- [ ] **Step 4: Write the modal**

Create `frontend/src/components/BulkRenameModal.tsx`:

```tsx
import { useMemo, useState } from "react";
import { useProfile } from "../contexts/ProfileContext";
import { Modal } from "./Modal";
import { announce } from "./LiveRegion";
import { BulkRenameTests, errMsg } from "../api";
import type { BulkEditResult, TestRename } from "../api";
import { useTestSummaries } from "../queries/summaries";
import { computeRenames, renameCounts, SUMMARY_MAX } from "../lib/rename";

interface Props {
  testKeys: string[];
  onComplete: (result: BulkEditResult) => void;
  onCancel: () => void;
}

type Mode = "prefix" | "suffix" | "both";

// PREVIEW_LIMIT caps rendered rows so a 500-test selection still types
// smoothly. The counts above the list are computed across every row, so the
// summary line stays true even when the list is cut short.
const PREVIEW_LIMIT = 200;

const TITLE_ID = "bulk-rename-title";

export function BulkRenameModal({ testKeys, onComplete, onCancel }: Props) {
  const { activeId } = useProfile();
  const [mode, setMode] = useState<Mode>("prefix");
  const [prefix, setPrefix] = useState("");
  const [suffix, setSuffix] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const summariesQuery = useTestSummaries(activeId, testKeys);
  const tests = summariesQuery.data ?? [];

  // A hidden input must never contribute to the result, so the affix a mode
  // does not use is read as empty rather than merely being off-screen.
  const activePrefix = mode === "suffix" ? "" : prefix;
  const activeSuffix = mode === "prefix" ? "" : suffix;

  const rows = useMemo(
    () => computeRenames(tests, { prefix: activePrefix, suffix: activeSuffix }),
    [tests, activePrefix, activeSuffix],
  );
  const counts = useMemo(() => renameCounts(rows), [rows]);

  const shown = rows.slice(0, PREVIEW_LIMIT);
  const canApply = counts.changed > 0 && !busy;

  async function apply() {
    const renames: TestRename[] = rows
      .filter((r) => r.state === "changed")
      .map((r) => ({ key: r.key, summary: r.after }));
    if (renames.length === 0) return;

    setBusy(true);
    setError("");
    try {
      const result = await BulkRenameTests(activeId, renames);
      announce(`Renamed ${result.succeeded.length} tests`);
      onComplete(result);
    } catch (e) {
      setError(errMsg(e));
      setBusy(false);
    }
  }

  return (
    <Modal onClose={onCancel} className="modal pending-modal" labelledBy={TITLE_ID}>
      <div className="pending-head">
        <h2 id={TITLE_ID}>Rename summaries</h2>
        <button className="btn btn-ghost" onClick={onCancel} title="Close">
          ✕
        </button>
      </div>

      <div className="rename-body">
        <fieldset className="rename-mode">
          <legend>What to add</legend>
          {(["prefix", "suffix", "both"] as Mode[]).map((m) => (
            <label key={m}>
              <input
                type="radio"
                name="rename-mode"
                value={m}
                checked={mode === m}
                onChange={() => setMode(m)}
              />
              {m === "prefix" ? "Prefix" : m === "suffix" ? "Suffix" : "Both"}
            </label>
          ))}
        </fieldset>

        {mode !== "suffix" && (
          <label className="rename-field">
            <span>Prefix</span>
            <input
              className="detail-input"
              autoFocus
              placeholder="[SMOKE] "
              value={prefix}
              onChange={(e) => setPrefix(e.target.value)}
            />
          </label>
        )}
        {mode !== "prefix" && (
          <label className="rename-field">
            <span>Suffix</span>
            <input
              className="detail-input"
              placeholder=" (v2)"
              value={suffix}
              onChange={(e) => setSuffix(e.target.value)}
            />
          </label>
        )}

        <p className="rename-counts" aria-live="polite">
          {activePrefix === "" && activeSuffix === ""
            ? "Type a prefix or suffix to see what changes."
            : `${counts.changed} will change, ${counts.unchanged} unchanged, ${counts.tooLong} too long`}
        </p>

        {summariesQuery.isPending ? (
          <p className="muted">Loading summaries…</p>
        ) : summariesQuery.isError ? (
          <p className="error-text">{errMsg(summariesQuery.error)}</p>
        ) : (
          <>
            <ul className="rename-list">
              {shown.map((r) => (
                <li key={r.key} className={`rename-row rename-${r.state}`}>
                  <span className="mono rename-key">{r.key}</span>
                  <span className="rename-before">{r.before}</span>
                  <span className="rename-arrow" aria-hidden="true">
                    →
                  </span>
                  <span className="rename-after">{r.after}</span>
                  {r.reason && <span className="muted"> · {r.reason}</span>}
                </li>
              ))}
            </ul>
            {rows.length > PREVIEW_LIMIT && (
              <p className="muted">
                Showing {PREVIEW_LIMIT} of {rows.length.toLocaleString()}.
              </p>
            )}
          </>
        )}

        {counts.unchanged > 0 && (activePrefix !== "" || activeSuffix !== "") && (
          <p className="muted">
            {counts.unchanged} tests already have this. They stay as they are.
          </p>
        )}
        {counts.tooLong > 0 && (
          <p className="warn-text">
            {counts.tooLong} tests would go over Jira's {SUMMARY_MAX} character
            limit. They are left out.
          </p>
        )}
        {error && <div className="error-text">{error}</div>}
      </div>

      <div className="pending-actions">
        <button className="btn" onClick={onCancel} disabled={busy}>
          Cancel
        </button>
        <button className="btn btn-primary" onClick={apply} disabled={!canApply}>
          {busy ? "Renaming…" : `Rename ${counts.changed} tests`}
        </button>
      </div>
    </Modal>
  );
}
```

Check `Modal`'s prop names against `frontend/src/components/Modal.tsx:18-27` and the `pending-head` / `pending-actions` markup against a neighbouring bulk modal, and match whatever those actually use.

- [ ] **Step 5: Style it**

Add to `frontend/src/App.css`, beside the other modal rules. Use the existing tokens (`--border`, `--text-muted`, `--warn-text`, `--accent`); do not introduce new ones:

```css
/* Bulk rename modal (-354). */
.rename-mode {
  display: flex;
  align-items: center;
  gap: 14px;
  margin: 0 0 10px;
  padding: 0;
  border: 0;
}
.rename-mode legend {
  float: left;
  margin-right: 10px;
  padding: 0;
  color: var(--text-muted);
  font-size: 12px;
}
.rename-mode label {
  display: flex;
  align-items: center;
  gap: 5px;
  cursor: pointer;
}
.rename-field {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}
.rename-field > span {
  min-width: 54px;
  color: var(--text-muted);
  font-size: 12px;
}
.rename-counts {
  margin: 10px 0 6px;
  font-size: 13px;
}
.rename-list {
  list-style: none;
  margin: 0;
  padding: 0;
  max-height: 42vh;
  overflow: auto;
  font-size: 13px;
}
.rename-row {
  display: flex;
  align-items: baseline;
  gap: 6px;
  padding: 4px 4px;
  border-bottom: 1px solid var(--border-subtle);
}
.rename-key {
  min-width: 108px;
}
.rename-before {
  color: var(--text-muted);
}
.rename-arrow {
  color: var(--text-muted);
}
/* Unchanged rows stay legible but recede; too-long rows are the only ones
   that need to catch the eye, since they are the ones being dropped. */
.rename-unchanged .rename-after {
  color: var(--text-muted);
}
.rename-too-long .rename-after {
  color: var(--warn-text);
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd frontend && npx vitest run src/components/BulkRenameModal.test.tsx && npm run build`

Expected: three PASS, and a clean build.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/BulkRenameModal.tsx frontend/src/components/BulkRenameModal.test.tsx frontend/src/queries/summaries.ts frontend/src/queries/keys.ts frontend/src/App.css
git commit -m "feat(frontend): bulk rename modal with a live preview (-354)

Mode is a radio group rather than a dropdown: it governs which inputs appear,
and a select would hide two of three options behind a click. The affix a mode
does not use is read as empty, so a hidden input can never reach the result.

The preview renders at most 200 rows while the counts are computed across the
whole selection, so typing stays smooth at 500 tests without the summary line
lying. Counts sit in an aria-live region.

First component test in the repo; existing tests cover contexts, queries and
lib only. It pins the property most likely to regress, that only rows which
actually change are sent."
```

---

### Task 5: Wire it into Browse

**Files:**
- Modify: `frontend/src/contexts/ModalContext.tsx` (add the `bulkRename` id)
- Modify: `frontend/src/App.tsx` (toolbar button and modal mount)

**Interfaces:**
- Consumes: `BulkRenameModal` from Task 4.
- Produces: nothing further.

- [ ] **Step 1: Add the modal id**

In `frontend/src/contexts/ModalContext.tsx`, add to the `ModalId` union (it starts at `:22`), beside the other bulk entries:

```ts
  | "bulkRename"
```

- [ ] **Step 2: Add the toolbar button**

In `frontend/src/App.tsx`, in the `bulk-toolbar` block (starts at `:1196`), directly after the "Bulk edit…" button:

```tsx
          <button
            className="btn btn-primary"
            onClick={() => openModal("bulkRename")}
          >
            Rename summaries…
          </button>
```

- [ ] **Step 3: Mount the modal**

In `frontend/src/App.tsx`, beside the other bulk modal mounts (`BulkEditModal` is at `:1547`), add:

```tsx
      {isOpen("bulkRename") && (
        <BulkRenameModal
          testKeys={[...selectedSet]}
          onComplete={() => afterMutation({ clearSelection: true })}
          onCancel={() => afterMutation()}
        />
      )}
```

And add the import beside the other bulk modal imports (`:68-73`):

```tsx
import { BulkRenameModal } from "./components/BulkRenameModal";
```

- [ ] **Step 4: Verify the whole suite**

Run:

```bash
gofmt -w . && go build ./... && go test ./... && cd frontend && npm test && npm run build
```

Expected: everything passes.

- [ ] **Step 5: Verify manually in demo mode**

Run `wails dev` with a profile whose Jira base URL is `demo`, then:

1. Select several tests in Browse. "Rename summaries…" appears in the bulk toolbar.
2. Open it. Every row shows its current summary and Apply is disabled.
3. Type a prefix. Every row updates as you type and the counts track it.
4. Select a folder whose tests already carry that prefix, reopen, and confirm they read as unchanged and the Apply count excludes them.
5. Type a very long prefix and confirm rows flip to too-long and drop out of the count.
6. Apply. The pending count rises by exactly the changed count, and the grid shows the new summaries.
7. Open Pending changes and confirm each row is an ordinary summary edit that can be discarded.
8. Switch to Suffix, then Both, and confirm the hidden affix does not contribute.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/contexts/ModalContext.tsx frontend/src/App.tsx
git commit -m "feat(ui): open bulk rename from the Browse toolbar (-354)

Sits beside the other bulk actions and follows their contract exactly:
testKeys from the current selection, afterMutation with a selection clear on
success."
```

---

## Self-review notes

- **Spec coverage.** Spec "Backend" is Tasks 1 and 2; "lib/rename.ts" is Task 3; "BulkRenameModal" and its states, copy and preview cap are Task 4; "Wiring" is Task 5; the spec's testing section is distributed across all five.
- **One spec correction, recorded in Global Constraints.** The spec calls for the counts to sit in "the LiveRegion component". `LiveRegion` is a mounted-once singleton driven by `announce()`, and announcing per keystroke would be unusable, so the counts get a local `aria-live="polite"` element and `announce()` reports only the terminal result.
- **Type consistency.** `TestSummary` and `TestRename` are `{key, summary}` in Go, in `api.ts`, and at every call site. `RenameRow.state` is `changed` / `unchanged` / `too-long` in the module, the tests, and the CSS class names (`rename-changed`, `rename-unchanged`, `rename-too-long`).
- **Task 2 Step 5 deliberately allows the last test to be rewritten** rather than the implementation. `TestBulkRenameTestsToTheSameSummaryQueuesNothing` documents `EditTestField`'s revert-to-original behaviour, which the plan asserts from the CLAUDE.md description rather than from reading the function. If it behaves differently, the test should record the real behaviour.
- **Two verification steps depend on judgement, not assertions:** Task 4 Step 4 and Task 5 say to check `Modal`'s props and the `pending-head` markup against the real files, because the surrounding components were restructured across roughly fifty commits and this plan was written from a reading of them rather than from having built against them.
