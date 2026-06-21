# Test Case Gap Analysis Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Gap Analysis view that diffs a reference test list against an uploaded target list by normalized summary, lets the user add target-only gaps as new tests (reusing the import pipeline) and export a management report, and replaces every browser alert/confirm in the app with the themed dialog system.

**Architecture:** Pure in-memory comparison in a new `internal/testrepo/gapanalysis.go`, reusing the import parser's row-grouping (factored into a shared `groupImportRows`), `insertLocalTest` for adds, and `writeCSV`/`writeXLSX` for the report. A new `GapAnalysisView.tsx` + a themed `useNotice` hook. File transport is base64 (identical to import).

**Tech Stack:** Go 1.25 (no ORM; raw SQL via modernc.org/sqlite; excelize/v2 for XLSX), Wails v2 generated bindings, React 18 + TypeScript, Vite.

## Global Constraints

- Branch: all work on `feature/gap-analysis` (already created, spec committed `1d3867f`). Do NOT merge to `main`. Do NOT push unless asked.
- No `Co-Authored-By` trailer or "Generated with Claude" line in any commit.
- No Jira calls anywhere in this feature — pure local cache + file I/O; works on demo profiles.
- Frontend has NO JS test runner: per-task frontend verification is `cd frontend && npx tsc --noEmit`; final verification is `npm run build` + `wails build`.
- After `wails build`, restore regeneration noise (`go.mod`, `frontend/package.json.md5`, and any `wailsjs` file whose only diff is line-endings) before committing; KEEP wailsjs files that gained real new bindings.
- Reuse, don't reinvent: `insertLocalTest`, `ParseRecords`, `writeCSV`/`writeXLSX`, `SaveFileDialog`, `useConfirm`. Do not change import behavior when factoring shared helpers.
- Match key = `normalizeSummary(Summary)` = trim + collapse internal whitespace to single spaces + lowercase. Compare by summary only.

---

## Task 1: `useNotice` themed-alert hook + fix the import-template alert (#9)

**Files:**
- Create: `frontend/src/components/useNotice.tsx`
- Modify: `frontend/src/components/ImportTestsModal.tsx` (the two `window.alert` at lines ~97, ~99 — the #9 named target)

**Interfaces:**
- Produces: `useNotice(): { notice: (opts: NoticeOptions) => Promise<void>; noticeUI: ReactNode }` where `NoticeOptions = { title: string; message?: string; tone?: "info" | "error" }`.

- [ ] **Step 1: Create the hook**

Create `frontend/src/components/useNotice.tsx` (mirrors `useConfirm.tsx`, single OK button, reuses the `.modal-overlay`/`.modal`/`.pending-head`/`.pending-actions` classes):

```tsx
import { useCallback, useState } from "react";
import type { ReactNode } from "react";

// WebView2 renders window.alert() as a bare, out-of-theme dialog. useNotice is
// an in-app, themed replacement: an async modal that resolves when dismissed.
// Pairs with useConfirm for one consistent dialog system across the app.

export interface NoticeOptions {
  title: string;
  message?: string;
  tone?: "info" | "error";
}

interface State {
  opts: NoticeOptions;
  resolve: () => void;
}

export function useNotice(): {
  notice: (opts: NoticeOptions) => Promise<void>;
  noticeUI: ReactNode;
} {
  const [state, setState] = useState<State | null>(null);

  const notice = useCallback(
    (opts: NoticeOptions) => new Promise<void>((resolve) => setState({ opts, resolve })),
    [],
  );

  const close = () => {
    if (state) state.resolve();
    setState(null);
  };

  const noticeUI = state ? (
    <NoticeModal {...state.opts} onClose={close} />
  ) : null;

  return { notice, noticeUI };
}

function NoticeModal({
  title,
  message,
  tone = "info",
  onClose,
}: NoticeOptions & { onClose: () => void }) {
  return (
    <div className="modal-overlay" onClick={onClose}>
      <div
        className="modal confirm-modal"
        onClick={(e) => e.stopPropagation()}
        role="alertdialog"
        aria-modal="true"
      >
        <div className="pending-head">
          <h2>{title}</h2>
          <button className="btn btn-ghost" onClick={onClose} title="Close">
            ✕
          </button>
        </div>
        {message && (
          <div className={`bulk-body confirm-message${tone === "error" ? " error-text" : ""}`}>
            {message}
          </div>
        )}
        <div className="pending-actions">
          <button className="btn btn-primary" onClick={onClose} autoFocus>
            OK
          </button>
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Typecheck the hook**

Run: `cd /c/projects/xray-test-manager/frontend && npx tsc --noEmit`
Expected: clean (an unused export is not an error).

- [ ] **Step 3: Wire useNotice into ImportTestsModal and replace the two alerts**

In `frontend/src/components/ImportTestsModal.tsx`:

Add the import after line 8 (`import type { ImportMapping, ImportResult } from "../api";`):
```tsx
import { useNotice } from "./useNotice";
```

Add the hook inside the component, next to the other `useState` calls (after line 53 `const [error, setError] = useState("");`):
```tsx
  const { notice, noticeUI } = useNotice();
```

Replace `downloadTemplate` (lines ~94–101) with:
```tsx
  async function downloadTemplate() {
    try {
      const path = await ExportImportTemplate();
      if (path) await notice({ title: "Template saved", message: path });
    } catch (err) {
      await notice({ title: "Template export failed", message: errMsg(err), tone: "error" });
    }
  }
```

Render `{noticeUI}` inside the modal — add it just before the closing `</div>` of the outer `.modal-overlay` (after the `.modal pending-modal` div closes, i.e. right before the final two `</div>` lines at ~221–222). Concretely, change the tail:
```tsx
        </div>
      </div>
      {noticeUI}
    </div>
  );
}
```
(The `{noticeUI}` sits as a sibling of the `.modal` inside `.modal-overlay`; the notice renders its own overlay on top.)

- [ ] **Step 4: Typecheck**

Run: `cd frontend && npx tsc --noEmit`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/useNotice.tsx frontend/src/components/ImportTestsModal.tsx
git commit -m "Add useNotice themed-alert hook; fix import-template download alert (#9)"
```

---

## Task 2: App-wide alert/confirm sweep (#8)

Replace every remaining `window.alert` → `notice(...)` and `window.confirm` → `await confirm(...)` so zero browser dialogs remain. **Grep-before-edit each file** to confirm current line numbers (they may drift between tasks):
`grep -n -E 'window\.(alert|confirm)\(' frontend/src/<file>`.

**Files:**
- Modify: `frontend/src/App.tsx` (9 sites: ~374, 394, 400, 407, 469, 524, 526, 551, 577)
- Modify: `frontend/src/components/ContainersView.tsx` (4 sites: ~191, 200, 305, 314)
- Modify: `frontend/src/components/PreconditionsView.tsx` (1 site: ~192)
- Modify: `frontend/src/components/RequirementsView.tsx` (1 site: ~239)
- Modify: `frontend/src/components/TestTable.tsx` (2 sites: ~471, 473)

**Interfaces:**
- Consumes: `useNotice` (Task 1); `useConfirm` (existing: `confirm(opts) => Promise<boolean>`, `confirmUI`).

For EACH file below: import the needed hook(s), instantiate inside the component (`const { notice, noticeUI } = useNotice();` and/or `const { confirm, confirmUI } = useConfirm();`), render `{noticeUI}`/`{confirmUI}` once in the component's returned JSX (as a sibling near the root), and replace the call sites. **First grep the file for an existing `useConfirm`/`useNotice` instantiation — if present, reuse it; don't double-instantiate.**

- [ ] **Step 1: App.tsx — confirm sites**

Replace the two `window.confirm` guards. Folder delete (~400):
```tsx
    if (!(await confirm({ title: "Delete folder", message: `Delete folder "${path}"? It must be empty.`, confirmLabel: "Delete", danger: true }))) return;
```
Profile delete (~469) — keep the existing message; convert `!window.confirm(<msg>)` to `!(await confirm({ title: "Delete profile", message: <msg>, confirmLabel: "Delete", danger: true }))`. Ensure the enclosing function is `async` (the delete handlers already are; verify).

- [ ] **Step 2: App.tsx — alert sites**

Replace each `window.alert(X)` with `await notice({ title: <short title>, message: X, tone: "error" | "info" })`. Map them:
- 374 sync error → `{ title: "Sync failed", message: errMsg(e), tone: "error" }`
- 394 folder list error → `{ title: "Couldn't load folders", message: errMsg(e), tone: "error" }`
- 407 folder delete error → `{ title: "Delete failed", message: errMsg(e), tone: "error" }`
- 524 export success → `{ title: "Profile exported", message: path }`
- 526 export error → `{ title: "Export failed", message: errMsg(e), tone: "error" }`
- 551 import error → `{ title: "Import failed", message: errMsg(e), tone: "error" }`
- 577 pending delete error → `{ title: "Delete failed", message: errMsg(e), tone: "error" }`
Each enclosing function must be `async` and `await` the notice (or call `void notice(...)` if it cannot be made async — prefer `await`).

Add hooks: in `App()` near the other hooks, `const { confirm, confirmUI } = useConfirm();` and `const { notice, noticeUI } = useNotice();` (grep first — App.tsx may already use `useConfirm`; if so reuse it). Imports at top:
```tsx
import { useConfirm } from "./components/useConfirm";
import { useNotice } from "./components/useNotice";
```
Render `{confirmUI}{noticeUI}` once near the root of App's returned JSX (e.g. just before the outermost closing tag).

- [ ] **Step 3: ContainersView.tsx**

Grep for an existing `useConfirm` (this file likely already uses it for container delete). Reuse it; add `useNotice` for the alert sites (200 delete error, 305 scaffold-export success). Convert the two `window.confirm` (191 delete container, 314 — grep the message) to `await confirm({...danger:true})`. Replace `window.alert` with `await notice(...)`. Ensure `{noticeUI}` is rendered (the file already renders `{confirmUI}` if it uses useConfirm; add `{noticeUI}` beside it).

- [ ] **Step 4: PreconditionsView.tsx (~192) and RequirementsView.tsx (~239)**

Each has one `window.confirm` for delete. Grep for existing `useConfirm`; these views likely already use it (the delete handlers were themed in earlier work — verify). If they still use `window.confirm`, convert to `await confirm({ title: "Delete…", message: <existing msg>, confirmLabel: "Delete", danger: true })` and ensure `{confirmUI}` is rendered.

- [ ] **Step 5: TestTable.tsx (~471, 473)**

Two `window.alert` (export success / failure). Add `useNotice`, render `{noticeUI}`, replace with `await notice({ title: "Tests exported", message: ... })` and `{ title: "Export failed", message: errMsg(e), tone: "error" }`. Make the enclosing export handler `async`.

- [ ] **Step 6: Verify zero browser dialogs remain**

Run: `cd /c/projects/xray-test-manager && grep -rn -E 'window\.(alert|confirm)\(' frontend/src | grep -v '//'`
Expected: NO matches (only comments, if any, remain).

Run: `cd frontend && npx tsc --noEmit`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/App.tsx frontend/src/components/ContainersView.tsx frontend/src/components/PreconditionsView.tsx frontend/src/components/RequirementsView.tsx frontend/src/components/TestTable.tsx
git commit -m "Replace all browser alert/confirm with themed dialogs (#8)"
```

---

## Task 3: Backend gap core — types, normalize, compare, parse

**Files:**
- Modify: `internal/testrepo/importcsv.go` (extract `groupImportRows`; `ImportTests` calls it — behavior-preserving)
- Create: `internal/testrepo/gapanalysis.go`
- Create: `internal/testrepo/gapanalysis_test.go`

**Interfaces:**
- Produces:
  - `type GapTest struct { Summary, Description, Priority string; Labels, Components []string; Folder string }` (JSON: summary, description, priority, labels, components, folder)
  - `type GapResult struct { ReferenceSource string; ReferenceCount, TargetCount, Matched int; MissingFromReference, MissingFromTarget []GapTest }`
  - `func normalizeSummary(s string) string`
  - `func AnalyzeGap(reference, target []GapTest, referenceSource string) GapResult`
  - `func parseGapRows(records [][]string) ([]GapTest, error)`
  - `func gapRowsFromTests(tests []TestCase) []GapTest`
  - `func groupImportRows(records [][]string, mapping ImportMapping) (tests []testCreatePayload, errs []ImportError, skipped int, err error)` (extracted)

- [ ] **Step 1: Extract `groupImportRows` from `ImportTests` (refactor, no behavior change)**

In `internal/testrepo/importcsv.go`, replace the body of `ImportTests` from the `header := records[0]` line through the end of the first-pass grouping loop (the block that builds `tests []testCreatePayload` and appends to `result.Errors`/`result.Skipped`, lines ~87–161) by calling a new helper, then keep the persistence tail. New helper (add below `ImportTests`):

```go
// groupImportRows maps spreadsheet rows to Test payloads using a column mapping
// (FR-10.4 / 10.7): a row with a Summary starts a Test; following rows with an
// empty Summary but step content extend the previous Test's steps. Returns the
// grouped Tests plus any per-row errors and the skipped count. Shared by
// ImportTests and gap analysis so both group identically.
func groupImportRows(records [][]string, mapping ImportMapping) (tests []testCreatePayload, errs []ImportError, skipped int, err error) {
	if len(records) < 2 {
		return nil, nil, 0, fmt.Errorf("the file has no data rows")
	}
	header := records[0]
	col := func(name string) int {
		if name == "" {
			return -1
		}
		for i, h := range header {
			if strings.EqualFold(strings.TrimSpace(h), strings.TrimSpace(name)) {
				return i
			}
		}
		return -1
	}
	summaryIdx := col(mapping.Summary)
	if summaryIdx < 0 {
		return nil, nil, 0, fmt.Errorf("the Summary field must be mapped to a column")
	}
	descIdx := col(mapping.Description)
	prioIdx := col(mapping.Priority)
	labelsIdx := col(mapping.Labels)
	componentsIdx := col(mapping.Components)
	folderIdx := col(mapping.Folder)
	actionIdx := col(mapping.Action)
	dataIdx := col(mapping.Data)
	expectedIdx := col(mapping.Expected)

	get := func(row []string, idx int) string {
		if idx < 0 || idx >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[idx])
	}

	tests = []testCreatePayload{}
	errs = []ImportError{}
	curIdx := -1
	for i := 1; i < len(records); i++ {
		rowNum := i + 1
		summary := get(records[i], summaryIdx)
		step := importStep{
			Action:   get(records[i], actionIdx),
			Data:     get(records[i], dataIdx),
			Expected: get(records[i], expectedIdx),
		}
		hasStep := step.Action != "" || step.Data != "" || step.Expected != ""

		if summary != "" {
			tests = append(tests, testCreatePayload{
				Summary:     summary,
				Description: get(records[i], descIdx),
				Priority:    get(records[i], prioIdx),
				Labels:      get(records[i], labelsIdx),
				Components:  get(records[i], componentsIdx),
				Folder:      get(records[i], folderIdx),
			})
			curIdx = len(tests) - 1
			if hasStep {
				tests[curIdx].Steps = append(tests[curIdx].Steps, step)
			}
			continue
		}
		if hasStep {
			if curIdx < 0 {
				errs = append(errs, ImportError{Row: rowNum, Message: "step row before any test summary"})
				skipped++
				continue
			}
			tests[curIdx].Steps = append(tests[curIdx].Steps, step)
			continue
		}
		errs = append(errs, ImportError{Row: rowNum, Message: "row has neither a summary nor step content"})
		skipped++
	}
	return tests, errs, skipped, nil
}
```

Then rewrite `ImportTests` to use it (keep the exact same outcome):

```go
func (r *Repository) ImportTests(profileID string, records [][]string, mapping ImportMapping, dryRun bool) (ImportResult, error) {
	result := ImportResult{Errors: []ImportError{}}
	tests, errs, skipped, err := groupImportRows(records, mapping)
	if err != nil {
		return result, err
	}
	result.Errors = errs
	result.Skipped = skipped
	result.Created = len(tests)
	if dryRun {
		return result, nil
	}

	tx, err := r.db.Begin()
	if err != nil {
		return result, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, p := range tests {
		if err := insertImportedTest(tx, profileID, p); err != nil {
			return result, err
		}
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit import: %w", err)
	}
	return result, nil
}
```

- [ ] **Step 2: Verify import behavior is unchanged**

Run: `cd /c/projects/xray-test-manager && go build ./... && go test ./internal/testrepo/ -run Import -v 2>&1 | tail -20`
Expected: builds; all existing import tests PASS.

- [ ] **Step 3: Write failing gap-core tests**

Create `internal/testrepo/gapanalysis_test.go`:

```go
package testrepo

import "testing"

func TestNormalizeSummary(t *testing.T) {
	cases := map[string]string{
		"  Login  with   VALID credentials ": "login with valid credentials",
		"Logout":                              "logout",
		"":                                    "",
	}
	for in, want := range cases {
		if got := normalizeSummary(in); got != want {
			t.Errorf("normalizeSummary(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAnalyzeGapDirectionsAndMatch(t *testing.T) {
	reference := []GapTest{{Summary: "Login"}, {Summary: "Logout"}, {Summary: "Reset password"}}
	target := []GapTest{{Summary: "login  "}, {Summary: "SSO login"}, {Summary: "Reset Password"}}

	res := AnalyzeGap(reference, target, "project")
	if res.Matched != 2 { // Login + Reset password (case/space-insensitive)
		t.Errorf("Matched = %d, want 2", res.Matched)
	}
	if len(res.MissingFromReference) != 1 || res.MissingFromReference[0].Summary != "SSO login" {
		t.Errorf("MissingFromReference = %+v, want [SSO login]", res.MissingFromReference)
	}
	if len(res.MissingFromTarget) != 1 || res.MissingFromTarget[0].Summary != "Logout" {
		t.Errorf("MissingFromTarget = %+v, want [Logout]", res.MissingFromTarget)
	}
	if res.ReferenceSource != "project" || res.ReferenceCount != 3 || res.TargetCount != 3 {
		t.Errorf("meta = %q %d %d, want project 3 3", res.ReferenceSource, res.ReferenceCount, res.TargetCount)
	}
}

func TestAnalyzeGapDedupAndBlank(t *testing.T) {
	reference := []GapTest{}
	target := []GapTest{{Summary: "Dup"}, {Summary: "dup"}, {Summary: "  "}}
	res := AnalyzeGap(reference, target, "file")
	// "Dup"/"dup" collapse to one gap; blank summary skipped.
	if len(res.MissingFromReference) != 1 {
		t.Errorf("MissingFromReference = %+v, want 1 deduped entry", res.MissingFromReference)
	}
	if len(res.MissingFromTarget) != 0 {
		t.Errorf("MissingFromTarget = %+v, want none", res.MissingFromTarget)
	}
}

func TestParseGapRowsAutoMapsAndGroups(t *testing.T) {
	records := [][]string{
		{"Summary", "Description", "Priority", "Labels", "Components", "Folder", "Action", "Data", "Expected"},
		{"Login", "Can log in", "High", "smoke api", "Auth, Frontend", "/Login", "", "", ""},
		{"Stepped test", "multi", "Medium", "smoke", "Frontend", "/X", "open page", "", "shown"},
		{"", "", "", "", "", "", "enter creds", "u/p", "logged in"}, // step row, NOT a new gap
	}
	gaps, err := parseGapRows(records)
	if err != nil {
		t.Fatalf("parseGapRows: %v", err)
	}
	if len(gaps) != 2 {
		t.Fatalf("parsed %d gaps, want 2 (step row must not create one)", len(gaps))
	}
	if gaps[0].Summary != "Login" || len(gaps[0].Labels) != 2 || len(gaps[0].Components) != 2 {
		t.Errorf("gap[0] = %+v, want Login with 2 labels + 2 components", gaps[0])
	}
}

func TestParseGapRowsNoSummaryColumn(t *testing.T) {
	records := [][]string{{"Title", "Notes"}, {"x", "y"}}
	if _, err := parseGapRows(records); err == nil {
		t.Error("parseGapRows should error when no Summary column is present")
	}
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./internal/testrepo/ -run 'NormalizeSummary|AnalyzeGap|ParseGapRows' -v 2>&1 | tail -15`
Expected: FAIL (undefined: normalizeSummary / GapTest / AnalyzeGap / parseGapRows).

- [ ] **Step 5: Implement `gapanalysis.go`**

Create `internal/testrepo/gapanalysis.go`:

```go
package testrepo

import "strings"

// GapTest is one comparable test row, carrying the import fields so an added gap
// becomes a complete local test. Steps are intentionally omitted — gap analysis
// is by summary; created gaps are summary + metadata, fleshed out later.
type GapTest struct {
	Summary     string   `json:"summary"`
	Description string   `json:"description"`
	Priority    string   `json:"priority"`
	Labels      []string `json:"labels"`
	Components  []string `json:"components"`
	Folder      string   `json:"folder"`
}

// GapResult is the outcome of a comparison. MissingFromReference is in the
// target but not the reference (addable as tests); MissingFromTarget is in the
// reference but not the target (report-only). Both gap lists are deduplicated by
// normalized summary.
type GapResult struct {
	ReferenceSource      string    `json:"referenceSource"` // "project" | "file"
	ReferenceCount       int       `json:"referenceCount"`
	TargetCount          int       `json:"targetCount"`
	Matched              int       `json:"matched"`
	MissingFromReference []GapTest `json:"missingFromReference"`
	MissingFromTarget    []GapTest `json:"missingFromTarget"`
}

// normalizeSummary is the match key: trim, collapse internal whitespace runs to
// single spaces, lowercase. Blank stays blank.
func normalizeSummary(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// summarySet returns the set of non-blank normalized summaries in a list.
func summarySet(tests []GapTest) map[string]bool {
	set := make(map[string]bool, len(tests))
	for _, t := range tests {
		if k := normalizeSummary(t.Summary); k != "" {
			set[k] = true
		}
	}
	return set
}

// missing returns the tests whose normalized summary is not in other, blanks
// skipped, deduplicated by normalized summary (first occurrence wins).
func missing(tests []GapTest, other map[string]bool) []GapTest {
	out := []GapTest{}
	seen := map[string]bool{}
	for _, t := range tests {
		k := normalizeSummary(t.Summary)
		if k == "" || other[k] || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, t)
	}
	return out
}

// AnalyzeGap compares two lists by normalized summary.
func AnalyzeGap(reference, target []GapTest, referenceSource string) GapResult {
	refSet := summarySet(reference)
	targetSet := summarySet(target)
	matched := 0
	for k := range refSet {
		if targetSet[k] {
			matched++
		}
	}
	return GapResult{
		ReferenceSource:      referenceSource,
		ReferenceCount:       len(reference),
		TargetCount:          len(target),
		Matched:              matched,
		MissingFromReference: missing(target, refSet),
		MissingFromTarget:    missing(reference, targetSet),
	}
}

// gapAutoMapping builds an ImportMapping from a header row by matching each
// canonical field name case-insensitively — the import-template contract, so no
// manual mapping UI is needed.
func gapAutoMapping(header []string) ImportMapping {
	find := func(name string) string {
		for _, h := range header {
			if strings.EqualFold(strings.TrimSpace(h), name) {
				return strings.TrimSpace(h)
			}
		}
		return ""
	}
	return ImportMapping{
		Summary:     find("Summary"),
		Description: find("Description"),
		Priority:    find("Priority"),
		Labels:      find("Labels"),
		Components:  find("Components"),
		Folder:      find("Folder"),
		Action:      find("Action"),
		Data:        find("Data"),
		Expected:    find("Expected"),
	}
}

// parseGapRows parses spreadsheet records into GapTests, auto-mapping columns by
// the import-template header names and reusing the import grouping so step rows
// don't become spurious entries. Errors if no Summary column is found.
func parseGapRows(records [][]string) ([]GapTest, error) {
	if len(records) == 0 {
		return nil, errEmptyGapFile
	}
	mapping := gapAutoMapping(records[0])
	tests, _, _, err := groupImportRows(records, mapping)
	if err != nil {
		return nil, err
	}
	out := make([]GapTest, 0, len(tests))
	for _, p := range tests {
		out = append(out, payloadToGapTest(p))
	}
	return out, nil
}

// gapRowsFromTests maps cached project Tests into GapTests for the reference side.
func gapRowsFromTests(tests []TestCase) []GapTest {
	out := make([]GapTest, 0, len(tests))
	for _, t := range tests {
		out = append(out, GapTest{
			Summary:     t.Summary,
			Description: t.Description,
			Priority:    t.Priority,
			Labels:      t.Labels,
			Components:  t.Components,
			FolderID:    "", // unused for comparison
		}.withFolder(t.FolderID))
	}
	return out
}

// payloadToGapTest converts an import payload (joined labels/components) to the
// exported GapTest (slice fields).
func payloadToGapTest(p testCreatePayload) GapTest {
	return GapTest{
		Summary:     p.Summary,
		Description: p.Description,
		Priority:    p.Priority,
		Labels:      strings.Fields(p.Labels),
		Components:  splitComponents(p.Components),
		Folder:      p.Folder,
	}
}

// splitComponents splits a comma-separated component string, trimming blanks.
func splitComponents(s string) []string {
	out := []string{}
	for _, c := range strings.Split(s, ",") {
		if t := strings.TrimSpace(c); t != "" {
			out = append(out, t)
		}
	}
	return out
}

var errEmptyGapFile = fmtErrorf("the file is empty")
```

**Note for the implementer:** the snippet above has two intentional simplifications to resolve while implementing — (a) `gapRowsFromTests` is written awkwardly; implement it plainly as building each `GapTest{Summary, Description, Priority, Labels, Components, Folder: t.FolderID}` (there is no `withFolder`/`FolderID` field on `GapTest` — that was shorthand). (b) `errEmptyGapFile`/`fmtErrorf` is shorthand for `fmt.Errorf("the file is empty")`; add `"fmt"` to imports and use it directly. Concretely use:

```go
func gapRowsFromTests(tests []TestCase) []GapTest {
	out := make([]GapTest, 0, len(tests))
	for _, t := range tests {
		out = append(out, GapTest{
			Summary: t.Summary, Description: t.Description, Priority: t.Priority,
			Labels: t.Labels, Components: t.Components, Folder: t.FolderID,
		})
	}
	return out
}
```
and replace the `errEmptyGapFile` var + `parseGapRows` empty check with an inline `fmt.Errorf("the file is empty")`. Imports: `"fmt"` and `"strings"`.

- [ ] **Step 6: Run gap-core tests**

Run: `go test ./internal/testrepo/ -run 'NormalizeSummary|AnalyzeGap|ParseGapRows' -v 2>&1 | tail -15`
Expected: PASS.

- [ ] **Step 7: Full testrepo + build**

Run: `go build ./... && go test ./internal/testrepo/ 2>&1 | tail -5`
Expected: builds; all pass (import unchanged).

- [ ] **Step 8: Commit**

```bash
git add internal/testrepo/importcsv.go internal/testrepo/gapanalysis.go internal/testrepo/gapanalysis_test.go
git commit -m "Gap analysis: compare + parse core (reuse import grouping)"
```

---

## Task 4: Backend — create gaps as tests + build report

**Files:**
- Modify: `internal/testrepo/gapanalysis.go`
- Modify: `internal/testrepo/gapanalysis_test.go`

**Interfaces:**
- Consumes: `GapTest`, `GapResult` (Task 3); `insertLocalTest`, `testCreatePayload`, `ImportResult`, `ImportError`, `encodeComponents` (existing); `writeCSV`/`writeXLSX` (existing in exportcsv.go).
- Produces:
  - `func (r *Repository) CreateTestsFromGaps(profileID string, gaps []GapTest) (ImportResult, error)`
  - `func buildGapReport(result GapResult, generatedAt, format string) ([]byte, error)`

- [ ] **Step 1: Write failing tests**

Append to `internal/testrepo/gapanalysis_test.go`:

```go
func TestCreateTestsFromGaps(t *testing.T) {
	repo := newRepo(t) // helper used by other testrepo tests (newRepo)
	gaps := []GapTest{
		{Summary: "Logout clears session", Description: "d", Priority: "High", Labels: []string{"smoke"}, Components: []string{"Auth"}, Folder: "/X"},
		{Summary: "", Description: "blank skipped"},
	}
	res, err := repo.CreateTestsFromGaps("p1", gaps)
	if err != nil {
		t.Fatalf("CreateTestsFromGaps: %v", err)
	}
	if res.Created != 1 || res.Skipped != 1 {
		t.Fatalf("result = %+v, want Created 1 Skipped 1", res)
	}
	page, _ := repo.ListTests("p1", Query{})
	if page.Total != 1 || page.Tests[0].Summary != "Logout clears session" {
		t.Fatalf("listed = %+v, want one NEW test", page.Tests)
	}
	changes, _ := repo.ListPendingChanges("p1")
	var creates int
	for _, c := range changes {
		if c.EntityType == "test_create" {
			creates++
		}
	}
	if creates != 1 {
		t.Errorf("test_create pending rows = %d, want 1", creates)
	}
}

func TestBuildGapReportHasHeaderAndSections(t *testing.T) {
	res := GapResult{
		ReferenceSource: "project", ReferenceCount: 5, TargetCount: 6, Matched: 4,
		MissingFromReference: []GapTest{{Summary: "SSO login", Description: "d"}},
		MissingFromTarget:    []GapTest{{Summary: "Legacy captcha"}},
	}
	data, err := buildGapReport(res, "2026-06-20T00:00:00Z", "csv")
	if err != nil {
		t.Fatalf("buildGapReport: %v", err)
	}
	s := string(data)
	for _, want := range []string{"2026-06-20T00:00:00Z", "project", "Missing from reference", "Missing from target", "SSO login", "Legacy captcha"} {
		if !contains(s, want) {
			t.Errorf("report missing %q\n%s", want, s)
		}
	}
	if _, err := buildGapReport(res, "t", "xlsx"); err != nil {
		t.Errorf("xlsx report: %v", err)
	}
}

func contains(haystack, needle string) bool { return strings_Contains(haystack, needle) }
```

**Implementer note:** replace the `contains`/`strings_Contains` shim with a direct `strings.Contains` call (add `"strings"` to the test imports) — the shim is only to keep the snippet import-light. Confirm the seeding helper is named `newRepo` by grepping the existing `internal/testrepo/*_test.go` (e.g. `bugcrud_test.go` uses `newRepo(t)`); use whatever it actually is.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/testrepo/ -run 'CreateTestsFromGaps|BuildGapReport' -v 2>&1 | tail -15`
Expected: FAIL (undefined: CreateTestsFromGaps / buildGapReport).

- [ ] **Step 3: Implement**

Append to `internal/testrepo/gapanalysis.go` (ensure imports include `fmt` and `strings`):

```go
// gapTestToPayload converts an exported GapTest to an import payload (joined
// labels/components, no steps) for insertLocalTest.
func gapTestToPayload(g GapTest) testCreatePayload {
	return testCreatePayload{
		Summary:     g.Summary,
		Description: g.Description,
		Priority:    g.Priority,
		Labels:      strings.Join(g.Labels, " "),
		Components:  strings.Join(g.Components, ", "),
		Folder:      g.Folder,
	}
}

// CreateTestsFromGaps creates a local pending Test (NEW-N) for each gap with a
// non-blank summary, reusing the import create path. Blank-summary gaps are
// skipped and reported, mirroring ImportTests.
func (r *Repository) CreateTestsFromGaps(profileID string, gaps []GapTest) (ImportResult, error) {
	result := ImportResult{Errors: []ImportError{}}
	tx, err := r.db.Begin()
	if err != nil {
		return result, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for i, g := range gaps {
		if strings.TrimSpace(g.Summary) == "" {
			result.Errors = append(result.Errors, ImportError{Row: i + 1, Message: "gap has no summary"})
			result.Skipped++
			continue
		}
		if _, err := insertLocalTest(tx, profileID, gapTestToPayload(g), "gap-create-local"); err != nil {
			return result, err
		}
		result.Created++
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit gap creates: %w", err)
	}
	return result, nil
}

// gapReportHeader is the per-row column order for the two gap sections.
var gapReportHeader = []string{"Summary", "Description", "Priority", "Labels", "Components", "Folder"}

// buildGapReport renders the management report: a metadata block, then the two
// gap sections. generatedAt is supplied by the caller (the binding passes
// time.Now) so the function stays testable. format is "csv" or "xlsx".
func buildGapReport(result GapResult, generatedAt, format string) ([]byte, error) {
	rows := [][]string{
		{"Test Case Gap Analysis Report"},
		{"Generated", generatedAt},
		{"Reference source", result.ReferenceSource},
		{"Reference count", fmt.Sprintf("%d", result.ReferenceCount)},
		{"Target count", fmt.Sprintf("%d", result.TargetCount)},
		{"Matched", fmt.Sprintf("%d", result.Matched)},
		{"Missing from reference", fmt.Sprintf("%d", len(result.MissingFromReference))},
		{"Missing from target", fmt.Sprintf("%d", len(result.MissingFromTarget))},
		{},
		{"Missing from reference (in target, not reference)"},
		gapReportHeader,
	}
	for _, g := range result.MissingFromReference {
		rows = append(rows, gapRow(g))
	}
	rows = append(rows, []string{}, []string{"Missing from target (in reference, not target)"}, gapReportHeader)
	for _, g := range result.MissingFromTarget {
		rows = append(rows, gapRow(g))
	}
	if format == "xlsx" {
		return writeXLSX(rows)
	}
	return writeCSV(rows)
}

func gapRow(g GapTest) []string {
	return []string{
		g.Summary, g.Description, g.Priority,
		strings.Join(g.Labels, " "), strings.Join(g.Components, ", "), g.Folder,
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/testrepo/ -run 'CreateTestsFromGaps|BuildGapReport|AnalyzeGap|ParseGapRows|NormalizeSummary' -v 2>&1 | tail -20`
Expected: PASS.

- [ ] **Step 5: Full build + test**

Run: `go build ./... && go test ./internal/testrepo/ 2>&1 | tail -5`
Expected: builds; all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/testrepo/gapanalysis.go internal/testrepo/gapanalysis_test.go
git commit -m "Gap analysis: create gaps as tests + build report"
```

---

## Task 5: App bindings + api.ts

**Files:**
- Modify: `app.go` (3 new bindings; reuse `decodeImport`, `SaveFileDialog`)
- Modify: `frontend/src/api.ts`
- Modify: `frontend/wailsjs/go/main/App.js` + `App.d.ts` + `frontend/wailsjs/go/models.ts` (hand-add for tsc; `wails build` regenerates in Task 6)

**Interfaces:**
- Consumes: `AnalyzeGap`, `CreateTestsFromGaps`, `buildGapReport`, `gapRowsFromTests`, `parseGapRows`, `ListTestsForExport`, `GapTest`, `GapResult` (testrepo); `decodeImport` (existing in app.go); `runtime.SaveFileDialog`, `os.WriteFile` (existing).
- Produces (Wails bindings):
  - `AnalyzeGap(profileID, refSource, refB64 string, refXlsx bool, targetB64 string, targetXlsx bool) (testrepo.GapResult, error)`
  - `CreateTestsFromGaps(profileID string, gaps []testrepo.GapTest) (testrepo.ImportResult, error)`
  - `ExportGapReport(result testrepo.GapResult) (string, error)`

- [ ] **Step 1: Add the bindings to `app.go`**

Find the import bindings (`PreviewImport`, `ImportTests`, `decodeImport`, `ExportImportTemplate`) and add nearby:

```go
// AnalyzeGap diffs a reference test list against an uploaded target list by
// normalized summary. refSource "project" uses the active project's cached
// tests (refB64 ignored); "file" parses refB64. The target is always a file.
func (a *App) AnalyzeGap(profileID, refSource, refB64 string, refXlsx bool, targetB64 string, targetXlsx bool) (testrepo.GapResult, error) {
	if err := a.requireStore(); err != nil {
		return testrepo.GapResult{}, err
	}
	var reference []testrepo.GapTest
	switch refSource {
	case "project":
		tests, err := a.repo.ListTestsForExport(profileID, testrepo.Query{})
		if err != nil {
			return testrepo.GapResult{}, err
		}
		reference = testrepo.GapRowsFromTests(tests)
	case "file":
		recs, err := decodeImport(refB64, refXlsx)
		if err != nil {
			return testrepo.GapResult{}, fmt.Errorf("reference file: %w", err)
		}
		reference, err = testrepo.ParseGapRows(recs)
		if err != nil {
			return testrepo.GapResult{}, fmt.Errorf("reference file: %w", err)
		}
	default:
		return testrepo.GapResult{}, fmt.Errorf("unknown reference source %q", refSource)
	}
	targetRecs, err := decodeImport(targetB64, targetXlsx)
	if err != nil {
		return testrepo.GapResult{}, fmt.Errorf("target file: %w", err)
	}
	target, err := testrepo.ParseGapRows(targetRecs)
	if err != nil {
		return testrepo.GapResult{}, fmt.Errorf("target file: %w", err)
	}
	return testrepo.AnalyzeGap(reference, target, refSource), nil
}

// CreateTestsFromGaps adds the selected gaps as local pending Tests (committed
// on the next sync), reusing the import create path.
func (a *App) CreateTestsFromGaps(profileID string, gaps []testrepo.GapTest) (testrepo.ImportResult, error) {
	if err := a.requireStore(); err != nil {
		return testrepo.ImportResult{}, err
	}
	return a.repo.CreateTestsFromGaps(profileID, gaps)
}

// ExportGapReport writes the gap-analysis report to a user-chosen CSV/XLSX file.
// Returns the saved path, or "" if cancelled.
func (a *App) ExportGapReport(result testrepo.GapResult) (string, error) {
	if err := a.requireStore(); err != nil {
		return "", err
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export gap analysis report",
		DefaultFilename: "gap-analysis-report.csv",
		Filters: []runtime.FileFilter{
			{DisplayName: "CSV", Pattern: "*.csv"},
			{DisplayName: "Excel", Pattern: "*.xlsx"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("save dialog: %w", err)
	}
	if path == "" {
		return "", nil
	}
	format := "csv"
	if strings.HasSuffix(strings.ToLower(path), ".xlsx") {
		format = "xlsx"
	}
	data, err := testrepo.BuildGapReport(result, time.Now().Format(time.RFC3339), format)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write report: %w", err)
	}
	return path, nil
}
```

**Implementer note:** the bindings call `testrepo.GapRowsFromTests`, `testrepo.ParseGapRows`, `testrepo.BuildGapReport` — these must be **exported**. In `gapanalysis.go`, rename `gapRowsFromTests` → `GapRowsFromTests`, `parseGapRows` → `ParseGapRows`, `buildGapReport` → `BuildGapReport` (and update their uses + tests in Tasks 3/4 to the exported names). `AnalyzeGap`, `CreateTestsFromGaps`, `GapTest`, `GapResult` are already exported. Keep `groupImportRows`, `normalizeSummary`, `gapAutoMapping`, `gapTestToPayload`, `payloadToGapTest`, `splitComponents`, `summarySet`, `missing`, `gapRow`, `gapReportHeader` unexported. Confirm `time` is imported in app.go (it is used elsewhere; add if needed).

- [ ] **Step 2: Build Go**

Run: `go build ./... 2>&1 | head` ; Expected: clean. (If the rename broke test references, fix them and re-run `go test ./internal/testrepo/ -run Gap`.)

- [ ] **Step 3: api.ts — exports + interfaces**

In `frontend/src/api.ts`, add to the `export { … } from "../wailsjs/go/main/App"` block: `AnalyzeGap`, `CreateTestsFromGaps`, `ExportGapReport`. Add interfaces:

```typescript
// GapTest mirrors testrepo.GapTest — one comparable test row.
export interface GapTest {
  summary: string;
  description: string;
  priority: string;
  labels: string[];
  components: string[];
  folder: string;
}

// GapResult mirrors testrepo.GapResult — a comparison outcome.
export interface GapResult {
  referenceSource: string; // "project" | "file"
  referenceCount: number;
  targetCount: number;
  matched: number;
  missingFromReference: GapTest[];
  missingFromTarget: GapTest[];
}
```

- [ ] **Step 4: Hand-add the three wailsjs bindings (so tsc passes before Task 6's regen)**

In `frontend/wailsjs/go/main/App.js` add (mirroring existing functions):
```js
export function AnalyzeGap(arg1, arg2, arg3, arg4, arg5, arg6) {
  return window['go']['main']['App']['AnalyzeGap'](arg1, arg2, arg3, arg4, arg5, arg6);
}
export function CreateTestsFromGaps(arg1, arg2) {
  return window['go']['main']['App']['CreateTestsFromGaps'](arg1, arg2);
}
export function ExportGapReport(arg1) {
  return window['go']['main']['App']['ExportGapReport'](arg1);
}
```
In `frontend/wailsjs/go/main/App.d.ts` add:
```ts
export function AnalyzeGap(arg1:string,arg2:string,arg3:string,arg4:boolean,arg5:string,arg6:boolean):Promise<testrepo.GapResult>;
export function CreateTestsFromGaps(arg1:string,arg2:Array<testrepo.GapTest>):Promise<testrepo.ImportResult>;
export function ExportGapReport(arg1:testrepo.GapResult):Promise<string>;
```
In `frontend/wailsjs/go/models.ts`, add `GapTest` and `GapResult` classes to the `testrepo` namespace (mirror an existing class with a `convertValues` helper for the nested arrays). Minimal shape:
```ts
export class GapTest {
    summary: string; description: string; priority: string;
    labels: string[]; components: string[]; folder: string;
    static createFrom(source: any = {}) { return new GapTest(source); }
    constructor(source: any = {}) {
        if ('string' === typeof source) source = JSON.parse(source);
        this.summary = source["summary"]; this.description = source["description"];
        this.priority = source["priority"]; this.labels = source["labels"];
        this.components = source["components"]; this.folder = source["folder"];
    }
}
export class GapResult {
    referenceSource: string; referenceCount: number; targetCount: number; matched: number;
    missingFromReference: GapTest[]; missingFromTarget: GapTest[];
    static createFrom(source: any = {}) { return new GapResult(source); }
    constructor(source: any = {}) {
        if ('string' === typeof source) source = JSON.parse(source);
        this.referenceSource = source["referenceSource"]; this.referenceCount = source["referenceCount"];
        this.targetCount = source["targetCount"]; this.matched = source["matched"];
        this.missingFromReference = this.convertValues(source["missingFromReference"], GapTest);
        this.missingFromTarget = this.convertValues(source["missingFromTarget"], GapTest);
    }
    convertValues(a: any, classs: any, asMap: boolean = false): any {
        if (!a) return a;
        if (a.slice && a.map) return (a as any[]).map(elem => this.convertValues(elem, classs));
        else if ("object" === typeof a) { if (asMap) { for (const key of Object.keys(a)) a[key] = new classs(a[key]); return a; } return new classs(a); }
        return a;
    }
}
```
(Match the file's existing tab/space indentation. `wails build` in Task 6 regenerates these authoritatively.)

- [ ] **Step 5: Typecheck**

Run: `cd frontend && npx tsc --noEmit` ; Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add app.go internal/testrepo/gapanalysis.go internal/testrepo/gapanalysis_test.go frontend/src/api.ts frontend/wailsjs/go/main/App.js frontend/wailsjs/go/main/App.d.ts frontend/wailsjs/go/models.ts
git commit -m "Gap analysis: app bindings + api surface"
```

---

## Task 6: GapAnalysisView + App wiring + full verification

**Files:**
- Create: `frontend/src/components/GapAnalysisView.tsx`
- Modify: `frontend/src/App.tsx` (view union ~148–156, view-menu handlers ~488–494, tab buttons ~905–950, render chain, plus the menu list that defines `menu:view-*`)
- Modify: `frontend/src/App.css` (a few `.gap-*` classes)

**Interfaces:**
- Consumes: `AnalyzeGap`, `CreateTestsFromGaps`, `ExportGapReport`, `ExportImportTemplate`, `errMsg`, `GapResult`, `GapTest` (api.ts); `useNotice`, `useConfirm` (Tasks 1/2).

- [ ] **Step 1: Create `GapAnalysisView.tsx`**

Create `frontend/src/components/GapAnalysisView.tsx`:

```tsx
import { useMemo, useState } from "react";
import {
  AnalyzeGap,
  CreateTestsFromGaps,
  ExportGapReport,
  ExportImportTemplate,
  errMsg,
} from "../api";
import type { GapResult, GapTest } from "../api";
import { useNotice } from "./useNotice";

interface Props {
  profileId: string;
  onChanged: () => void;
}

function fileToBase64(file: File): Promise<{ b64: string; xlsx: boolean }> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(new Error("could not read file"));
    reader.onload = () => {
      const bytes = new Uint8Array(reader.result as ArrayBuffer);
      let binary = "";
      const chunk = 0x8000;
      for (let i = 0; i < bytes.length; i += chunk) {
        binary += String.fromCharCode(...bytes.subarray(i, i + chunk));
      }
      resolve({ b64: btoa(binary), xlsx: file.name.toLowerCase().endsWith(".xlsx") });
    };
    reader.readAsArrayBuffer(file);
  });
}

// GapAnalysisView compares a reference test list (the active project, or an
// uploaded file) against an uploaded target list by test summary, surfaces the
// gaps in both directions, lets the user add target-only gaps as new tests, and
// exports a management report. All feedback is themed (useNotice).
export function GapAnalysisView({ profileId, onChanged }: Props) {
  const [refSource, setRefSource] = useState<"project" | "file">("project");
  const [refFile, setRefFile] = useState<{ name: string; b64: string; xlsx: boolean } | null>(null);
  const [targetFile, setTargetFile] = useState<{ name: string; b64: string; xlsx: boolean } | null>(null);
  const [result, setResult] = useState<GapResult | null>(null);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [busy, setBusy] = useState(false);
  const { notice, noticeUI } = useNotice();

  const canRun = !!targetFile && (refSource === "project" || !!refFile);

  async function pick(setter: (v: { name: string; b64: string; xlsx: boolean }) => void, e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;
    try {
      const { b64, xlsx } = await fileToBase64(file);
      setter({ name: file.name, b64, xlsx });
      setResult(null);
      setSelected(new Set());
    } catch (err) {
      await notice({ title: "Couldn't read file", message: errMsg(err), tone: "error" });
    }
  }

  async function runAnalysis() {
    if (!targetFile) return;
    setBusy(true);
    try {
      const r = await AnalyzeGap(
        profileId,
        refSource,
        refFile?.b64 ?? "",
        refFile?.xlsx ?? false,
        targetFile.b64,
        targetFile.xlsx,
      );
      setResult(r);
      setSelected(new Set(r.missingFromReference.map((_, i) => i))); // default: all addable selected
    } catch (err) {
      await notice({ title: "Analysis failed", message: errMsg(err), tone: "error" });
    } finally {
      setBusy(false);
    }
  }

  async function downloadTemplate() {
    try {
      const path = await ExportImportTemplate();
      if (path) await notice({ title: "Template saved", message: path });
    } catch (err) {
      await notice({ title: "Template export failed", message: errMsg(err), tone: "error" });
    }
  }

  async function addSelected() {
    if (!result) return;
    const gaps: GapTest[] = result.missingFromReference.filter((_, i) => selected.has(i));
    if (gaps.length === 0) {
      await notice({ title: "Nothing selected", message: "Select at least one gap to add." });
      return;
    }
    setBusy(true);
    try {
      const res = await CreateTestsFromGaps(profileId, gaps);
      onChanged();
      await notice({
        title: "Gaps added",
        message: `Created ${res.created} test${res.created === 1 ? "" : "s"} as pending creates${res.skipped ? ` (${res.skipped} skipped)` : ""}. Commit them from the Pending list.`,
      });
    } catch (err) {
      await notice({ title: "Add failed", message: errMsg(err), tone: "error" });
    } finally {
      setBusy(false);
    }
  }

  async function exportReport() {
    if (!result) return;
    setBusy(true);
    try {
      const path = await ExportGapReport(result);
      if (path) await notice({ title: "Report saved", message: path });
    } catch (err) {
      await notice({ title: "Export failed", message: errMsg(err), tone: "error" });
    } finally {
      setBusy(false);
    }
  }

  function toggle(i: number) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(i)) next.delete(i);
      else next.add(i);
      return next;
    });
  }

  const allSelected = useMemo(
    () => !!result && result.missingFromReference.length > 0 && selected.size === result.missingFromReference.length,
    [result, selected],
  );

  return (
    <div className="gap-view">
      <div className="gap-setup">
        <div className="gap-field">
          <span className="gap-label">Reference</span>
          <label className="gap-radio">
            <input type="radio" checked={refSource === "project"} onChange={() => { setRefSource("project"); setResult(null); }} />
            Active project tests
          </label>
          <label className="gap-radio">
            <input type="radio" checked={refSource === "file"} onChange={() => { setRefSource("file"); setResult(null); }} />
            Upload file
          </label>
          {refSource === "file" && (
            <span className="gap-file">
              <input type="file" accept=".csv,.xlsx,text/csv" onChange={(e) => pick(setRefFile, e)} />
              {refFile && <span className="muted">{refFile.name}</span>}
            </span>
          )}
        </div>

        <div className="gap-field">
          <span className="gap-label">Target</span>
          <input type="file" accept=".csv,.xlsx,text/csv" onChange={(e) => pick(setTargetFile, e)} />
          {targetFile && <span className="muted">{targetFile.name}</span>}
        </div>

        <div className="gap-actions">
          <button className="link-btn" onClick={downloadTemplate}>Download template</button>
          <button className="btn btn-primary" onClick={runAnalysis} disabled={busy || !canRun}>
            {busy ? "Working…" : "Run analysis"}
          </button>
        </div>
        <p className="muted gap-hint">Files must use the import template columns (Summary required). Comparison is by test summary.</p>
      </div>

      {result && (
        <div className="gap-results">
          <div className="gap-summary">
            <span>Matched: <b>{result.matched}</b></span>
            <span>Reference: {result.referenceCount}</span>
            <span>Target: {result.targetCount}</span>
            <button className="btn" onClick={exportReport} disabled={busy}>Export report</button>
          </div>

          <div className="gap-panel">
            <div className="gap-panel-head">
              <h4>Missing from reference ({result.missingFromReference.length})</h4>
              <span className="muted">in target, not reference — addable as tests</span>
            </div>
            {result.missingFromReference.length === 0 ? (
              <p className="muted">None — the reference already covers every target test.</p>
            ) : (
              <>
                <label className="gap-selectall">
                  <input
                    type="checkbox"
                    checked={allSelected}
                    onChange={() =>
                      setSelected(allSelected ? new Set() : new Set(result.missingFromReference.map((_, i) => i)))
                    }
                  />
                  Select all
                </label>
                <ul className="gap-list">
                  {result.missingFromReference.map((g, i) => (
                    <li key={i} className="gap-item">
                      <input type="checkbox" checked={selected.has(i)} onChange={() => toggle(i)} />
                      <span className="gap-item-summary">{g.summary}</span>
                      {g.description && <span className="muted gap-item-desc">{g.description}</span>}
                    </li>
                  ))}
                </ul>
                <button className="btn btn-primary" onClick={addSelected} disabled={busy || selected.size === 0}>
                  Add selected as tests ({selected.size})
                </button>
              </>
            )}
          </div>

          <div className="gap-panel">
            <div className="gap-panel-head">
              <h4>Missing from target ({result.missingFromTarget.length})</h4>
              <span className="muted">in reference, not target — report only</span>
            </div>
            {result.missingFromTarget.length === 0 ? (
              <p className="muted">None.</p>
            ) : (
              <ul className="gap-list">
                {result.missingFromTarget.map((g, i) => (
                  <li key={i} className="gap-item">
                    <span className="gap-item-summary">{g.summary}</span>
                    {g.description && <span className="muted gap-item-desc">{g.description}</span>}
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      )}
      {noticeUI}
    </div>
  );
}
```

- [ ] **Step 2: Wire into App.tsx**

(Grep-confirm each anchor first.) (a) Add `| "gapanalysis"` to the `view` union (~148–156). (b) Add `"menu:view-gapanalysis": () => setView("gapanalysis"),` to the menu-handler map (~488–494) AND add a matching item to the View menu's item list (grep for where `menu:view-duplicates`/`menu:view-traceability` menu items are declared — add a "Gap Analysis" entry the same way). (c) Add a tab button in the tab block (~905–950), mirroring an existing one:
```tsx
          <button
            className={`view-tab${view === "gapanalysis" ? " view-tab-active" : ""}`}
            onClick={() => setView("gapanalysis")}
          >
            Gap Analysis
          </button>
```
(d) Add a render clause in the view render chain (grep for `view === "duplicates" ?` / `view === "traceability" ?` and add a sibling branch):
```tsx
      ) : view === "gapanalysis" ? (
        <main className="content content-gapanalysis">
          <GapAnalysisView
            profileId={activeId}
            onChanged={() => {
              setRefreshKey((k) => k + 1);
              reloadPending();
            }}
          />
        </main>
```
(e) Import the component near the other view imports:
```tsx
import { GapAnalysisView } from "./components/GapAnalysisView";
```
Use the actual setters present in App.tsx (`setRefreshKey`, `reloadPending`, `activeId` — grep to confirm the exact names; the duplicates/traceability render clauses use the same ones).

- [ ] **Step 3: Styles**

Append to `frontend/src/App.css`:
```css
/* Gap Analysis */
.gap-view { display: flex; flex-direction: column; gap: 16px; padding: 4px 2px; }
.gap-setup { display: flex; flex-direction: column; gap: 10px; }
.gap-field { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.gap-label { font-weight: 600; min-width: 80px; }
.gap-radio { display: inline-flex; align-items: center; gap: 5px; }
.gap-file { display: inline-flex; align-items: center; gap: 8px; }
.gap-actions { display: flex; align-items: center; gap: 12px; }
.gap-hint { font-size: 12px; }
.gap-results { display: flex; flex-direction: column; gap: 16px; }
.gap-summary { display: flex; align-items: center; gap: 18px; flex-wrap: wrap; }
.gap-summary > button { margin-left: auto; }
.gap-panel { border: 1px solid var(--border); border-radius: 10px; padding: 12px 14px; background: var(--surface); }
.gap-panel-head { display: flex; align-items: baseline; gap: 10px; margin-bottom: 8px; }
.gap-panel-head h4 { margin: 0; }
.gap-selectall { display: inline-flex; align-items: center; gap: 6px; margin-bottom: 8px; }
.gap-list { list-style: none; margin: 0 0 10px; padding: 0; max-height: 320px; overflow-y: auto; }
.gap-item { display: flex; align-items: center; gap: 8px; padding: 5px 4px; border-bottom: 1px solid var(--border-subtle); }
.gap-item-summary { font-weight: 500; }
.gap-item-desc { font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
```

- [ ] **Step 4: Typecheck**

Run: `cd frontend && npx tsc --noEmit` ; Expected: clean.

- [ ] **Step 5: Full build**

Run: `cd frontend && npm run build` ; Expected: tsc + vite clean.
Run (PowerShell): `Set-Location C:\projects\xray-test-manager; $env:Path += ";$env:USERPROFILE\go\bin"; wails build` ; Expected: `Built …xray-test-manager.exe`. (The `KnownStructs`/`Not found: time.Time` stderr lines are normal binding-gen logging.)

- [ ] **Step 6: Restore binding noise, keep real regen**

Run: `git -C /c/projects/xray-test-manager status --short`, then `git restore` `go.mod`, `frontend/package.json.md5`, and any `wailsjs` file whose diff is only line-endings; KEEP the wailsjs files that legitimately gained the Gap bindings (verify with `git diff --stat`).

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/GapAnalysisView.tsx frontend/src/App.tsx frontend/src/App.css frontend/wailsjs/go/main/App.d.ts frontend/wailsjs/go/main/App.js frontend/wailsjs/go/models.ts
git commit -m "Gap analysis: view + App wiring"
```

- [ ] **Step 8: Manual verification (pending human; cannot run GUI headlessly)**

Launch `build\bin\xray-test-manager.exe` on a `demo` profile. Verify: Gap Analysis tab opens; Download template saves (themed notice); project-vs-file analysis (upload a CSV missing/extra vs the demo's tests) shows Matched + both gap lists; select gaps → Add selected → themed "Gaps added" notice → appear in Pending Changes; file-vs-file (reference = file) works; Export report saves CSV and XLSX; a file with no Summary column shows a themed error. Grep confirms zero `window.alert`/`window.confirm` remain.

---

## Self-Review notes

- **Spec coverage:** reference-source toggle + project/file (#2a/#3a, Task 6 + Task 5 binding); target always file (#2b/#3b, Task 6); compare by summary (#2c/#3c, Task 3 `AnalyzeGap`); both gap directions (#4a/#4b, Task 3); add target-only as tests via import logic (#5, Task 4 `CreateTestsFromGaps` reusing `insertLocalTest`); management report CSV/XLSX (#6, Task 4 `BuildGapReport`); template = import template (#7, Task 6 reuses `ExportImportTemplate`); themed alerts in-app (#8, Tasks 1–2 app-wide); template-download alert fixed (#9, Task 1); separate branch (#10, Global Constraints — `feature/gap-analysis`). All requirements mapped.
- **In-memory only:** no schema change; nothing persisted during analysis (Task 3/5). Confirmed.
- **Type consistency:** Go `GapTest`/`GapResult` ↔ TS `GapTest`/`GapResult` (Task 5 fields match Task 3 definitions). Binding arg order `AnalyzeGap(profileID, refSource, refB64, refXlsx, targetB64, targetXlsx)` matches the `App.js`/`App.d.ts`/view call. Exported names (`ParseGapRows`, `GapRowsFromTests`, `BuildGapReport`, `AnalyzeGap`, `CreateTestsFromGaps`) are consistent between Task 3/4 (with the Task 5 export-rename note) and the app.go bindings.
- **Reuse fidelity:** `groupImportRows` is the single grouping used by both `ImportTests` and `parseGapRows` (Task 3), so step rows can't become phantom gaps; `insertLocalTest` is the single create path (Task 4); `writeCSV`/`writeXLSX` are the single report writers (Task 4).
- **Placeholder scan:** two snippet shorthands are called out with concrete replacements inline (the `gapRowsFromTests`/`errEmptyGapFile` note in Task 3 Step 5; the `contains`/`newRepo` note in Task 4 Step 1). No bare TODO/TBD.
