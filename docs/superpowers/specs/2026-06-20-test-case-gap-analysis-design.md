# Test Case Gap Analysis

**Status:** Approved (design) — 2026-06-20
**Branch:** `feature/gap-analysis` (cut from `main` @ v1.5.0; kept rebased, not merged without sign-off)
**Area:** New cross-cutting feature spanning store/testrepo, app bindings, and a new frontend view, plus an app-wide themed-dialog sweep.

## Problem

Teams need to know how their project's test cases compare against an external
reference list (e.g. a spec-derived or vendor-provided list): which tests exist
in one but not the other. Today there is no way to diff two test-case lists,
surface the gaps, turn the gaps into new tests, or produce a management report.

Separately, the app still uses browser-native `window.alert` / `window.confirm`
in ~18 places (including the import-template download), which are out-of-theme in
the WebView2 runtime.

## Goal

A **Test Case Gap Analysis** view that compares a reference list against a target
list by test summary, reports the gaps in both directions, lets the user add the
addable gaps as new tests (reusing the import pipeline), and exports a management
report. Plus: replace every browser alert/confirm in the app with the themed
dialog system.

Non-goals: fuzzy/similarity matching (exact-normalized only); a per-file column
mapping UI (columns are auto-detected from the import template headers);
materializing the reference file into a persistent project container; comparing
on fields other than summary.

## Locked decisions

- **One unified view** with a reference-source toggle; target is always an
  uploaded file.
- **Reference source:** (a) the active project's cached tests, or (b) an uploaded
  file. Both files use the **import template header names**; columns auto-detected
  (Summary required) — no manual mapping UI.
- **Match key:** normalized **Summary** — `strings.TrimSpace` + collapse internal
  whitespace to single spaces + `strings.ToLower`. Two tests match iff their
  normalized summaries are equal.
- **In-memory comparison:** nothing is persisted during analysis. Only chosen
  gaps become `NEW-*` local tests, and the report is written on demand.
- **Two gap directions shown;** only "missing from reference" (target-only) is
  addable as new tests. "Missing from target" (reference-only) is report-only.
- **Report:** CSV/XLSX (extension-driven), via the shared `writeCSV`/`writeXLSX`;
  a metadata/summary header block + both gap lists.
- **Template:** reuse the existing import template (`ExportImportTemplate`).
- **Alert/confirm:** app-wide sweep — add a themed `useNotice` hook, reuse
  `useConfirm`, replace all ~18 sites.

## Architecture

### Backend — `internal/testrepo/gapanalysis.go` (new)

Types:

```go
// GapTest is one comparable test row (parsed from a file or read from the
// project cache). Carries the import fields so an added gap becomes a complete
// local test.
type GapTest struct {
    Summary     string   `json:"summary"`
    Description string   `json:"description"`
    Priority    string   `json:"priority"`
    Labels      []string `json:"labels"`
    Components  []string `json:"components"`
    Folder      string   `json:"folder"`
}

// GapResult is the outcome of a comparison.
type GapResult struct {
    ReferenceSource      string    `json:"referenceSource"` // "project" | "file"
    ReferenceCount       int       `json:"referenceCount"`
    TargetCount          int       `json:"targetCount"`
    Matched              int       `json:"matched"`
    MissingFromReference []GapTest `json:"missingFromReference"` // in target, not reference (#4a; addable)
    MissingFromTarget    []GapTest `json:"missingFromTarget"`    // in reference, not target (#4b; report-only)
}
```

Functions:

- `normalizeSummary(s string) string` — trim, collapse runs of whitespace to one
  space, lowercase.
- `AnalyzeGap(reference, target []GapTest, referenceSource string) GapResult` —
  builds a normalized-summary set per side; `MissingFromReference` = target rows
  whose key ∉ reference set; `MissingFromTarget` = reference rows whose key ∉
  target set; `Matched` = count of reference rows whose key ∈ target set. Blank
  summaries are skipped (cannot be a match key). Duplicate keys within a side
  collapse to one for set membership but each input row is still classified.
- `parseGapRows(records [][]string) ([]GapTest, error)` — reuses the import
  parser's header auto-detection (`guessMapping`-equivalent on the header row)
  and row→payload grouping, producing `[]GapTest`. Errors if no Summary column
  is found.
- `gapRowsFromTests(tests []TestCase) []GapTest` — maps project `TestCase`s
  (from `ListTestsForExport(profileID, Query{})`) into `GapTest`s.
- `CreateTestsFromGaps(profileID string, gaps []GapTest) (ImportResult, error)`
  — for each gap, `insertLocalTest` (the same shared helper import uses), inside
  one transaction, returning an `ImportResult` (Created/Skipped/Errors). Blank
  summaries are skipped with an error row.
- `buildGapReport(result GapResult, generatedAt string, format string) ([]byte, error)`
  — assembles `[][]string`: a metadata block (generated timestamp, reference
  source, counts), a blank row, a "Missing from reference" section (header +
  rows), a blank row, a "Missing from target" section; then `writeCSV` or
  `writeXLSX`. The timestamp is passed in from the binding (Go test determinism;
  the app supplies `time.Now`).

The import grouping logic currently lives inside `ImportTests`/`ParseImportPreview`
in `importcsv.go`. If a small shared helper (header auto-map + row→`GapTest`/payload)
can be factored without disturbing import behavior, do so; otherwise duplicate the
minimal grouping in `parseGapRows`. Prefer reuse, but do not refactor import in a
way that changes its behavior.

### Bindings — `app.go`

- `AnalyzeGap(profileID, refSource, refB64 string, refXlsx bool, targetB64 string, targetXlsx bool) (testrepo.GapResult, error)`
  — when `refSource == "project"`, reference = `gapRowsFromTests(ListTestsForExport(profileID, Query{}))` and `refB64` is ignored; when `"file"`, decode+parse `refB64`. Always decode+parse the target. Calls `AnalyzeGap`.
- `CreateTestsFromGaps(profileID string, gaps []testrepo.GapTest) (testrepo.ImportResult, error)`.
- `ExportGapReport(result testrepo.GapResult) (string, error)` — `SaveFileDialog`
  (CSV/Excel filters, default `gap-analysis-report.csv`), `format` by extension,
  `buildGapReport` (timestamp = `time.Now`), `os.WriteFile`, return path or "".
- Template download reuses the existing `ExportImportTemplate()`.

File transport mirrors import exactly: frontend `FileReader.readAsArrayBuffer` →
base64 → backend `base64.StdEncoding.DecodeString` → `ParseRecords(data, isXlsx)`.

### Frontend

- `frontend/src/components/GapAnalysisView.tsx` (new): reference-source toggle
  (Active project / Upload file); a target file picker (always) and a reference
  file picker (only when source = file); a "Download template" button
  (`ExportImportTemplate`); a "Run analysis" button; a results panel — matched
  count + `ReferenceCount`/`TargetCount`, a "Missing from reference" list with
  per-row checkboxes + select-all and an "Add selected as tests" button, and a
  read-only "Missing from target" list; an "Export report" button. All feedback
  via `useNotice`/`useConfirm` (no browser dialogs).
- `frontend/src/components/useNotice.tsx` (new): a themed alert/notice hook
  mirroring `useConfirm` — `notice({ title, message?, tone? }) => Promise<void>`
  returning `{ notice, noticeUI }`, rendered with the same `.modal-overlay`/
  `.modal` shell.
- `frontend/src/api.ts`: export `AnalyzeGap`, `CreateTestsFromGaps`,
  `ExportGapReport`; interfaces `GapTest`, `GapResult`.
- `frontend/src/App.tsx`: add `"gapanalysis"` to the `view` union; a tab/menu
  entry; a render clause mounting `GapAnalysisView` with `profileId`,
  `refreshKey`, and an `onChanged` that bumps refresh + reloads pending (so added
  gaps appear in Pending Changes).

### Alert/confirm sweep (#8/#9) — app-wide

Add `useNotice`; reuse `useConfirm`. Replace every `window.alert` →
`notice(...)` and every `window.confirm` → `await confirm(...)`, wiring each
component's `noticeUI`/`confirmUI` into its render. Sites (from the codebase
inventory; re-grep before editing to confirm line numbers):

- `App.tsx` — sync error, folder-list error, delete-folder confirm, folder-delete
  error, delete-profile confirm, profile export success/failure, import failure,
  pending-delete failure (9).
- `ContainersView.tsx` — delete-container confirm, delete error, scaffold-export
  success, one more confirm (4).
- `TestTable.tsx` — export success, export failure (2).
- `PreconditionsView.tsx` — delete confirm (1).
- `RequirementsView.tsx` — delete confirm (1).
- `ImportTestsModal.tsx` — template-download success + failure (2; the #9 named
  target).
- `GapAnalysisView.tsx` — all of its own feedback (new).

Final state: a single themed dialog system; zero `window.alert`/`window.confirm`
in `frontend/src` (verified by grep at the end).

## Data flow (end to end)

```
Reference source = project:  ListTestsForExport(Query{}) -> gapRowsFromTests
Reference source = file:     pick file -> b64 -> ParseRecords -> parseGapRows
Target (always file):        pick file -> b64 -> ParseRecords -> parseGapRows
AnalyzeGap(reference, target, refSource) -> GapResult (in memory)
  shows: Matched, MissingFromReference (addable), MissingFromTarget (report-only)
Add selected (MissingFromReference) -> CreateTestsFromGaps -> insertLocalTest ->
  NEW-* tests + pending_change (import-equivalent) -> Pending Changes -> commit on sync
Export report -> ExportGapReport(GapResult) -> SaveFileDialog -> writeCSV/XLSX
Download template -> ExportImportTemplate (existing)
```

## Error handling

- No Summary column in a file → themed error notice; analysis does not run.
- Empty or unparseable file → themed error notice.
- `CreateTestsFromGaps` / `ExportGapReport` failure → themed error notice;
  partial creates report Created/Skipped/Errors like import.
- Demo-safe: no Jira calls anywhere in the feature — pure local cache + file I/O.

## Testing

Go (`internal/testrepo`):
- `normalizeSummary`: trim, internal-whitespace collapse, case fold, idempotence.
- `AnalyzeGap`: target-only gap, reference-only gap, matched, whitespace/case
  equivalence matches, empty reference, empty target, blank-summary rows skipped,
  duplicate keys within a side.
- `parseGapRows`: header auto-detection; missing-Summary error; multi-row step
  rows don't create spurious gap entries (summary-bearing rows only).
- `CreateTestsFromGaps`: creates one `NEW-*` test + one `test_create` pending
  change per gap; blank-summary skipped; fields (description/priority/labels/
  components/folder) carried through.
- `buildGapReport`: metadata header present; both sections present with correct
  rows; CSV and XLSX both produced.

Frontend: `npx tsc --noEmit` clean; `npm run build`; full `wails build`. Demo
click-through (pending human verification): project-vs-file and file-vs-file
analysis, add gaps → appear in Pending Changes, export report (CSV + XLSX),
download template — all feedback via themed notices; grep confirms no
`window.alert`/`window.confirm` remain.

## Build order (for the plan)

1. `useNotice` hook + app-wide alert/confirm sweep (independent, lands first so
   the new view uses it from the start).
2. Backend: `gapanalysis.go` types + `normalizeSummary` + `AnalyzeGap` +
   `parseGapRows` + `gapRowsFromTests` + tests.
3. Backend: `CreateTestsFromGaps` + `buildGapReport` + tests.
4. Bindings (`AnalyzeGap`, `CreateTestsFromGaps`, `ExportGapReport`) + `api.ts`.
5. `GapAnalysisView.tsx` + `App.tsx` wiring.
6. Full build + demo verification.
