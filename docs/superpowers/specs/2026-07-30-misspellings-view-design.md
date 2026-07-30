# Misspellings View — Design

**Date:** 2026-07-30
**Status:** Approved (pre-implementation)
**Scope:** A new in-app view that scans test cases for misspelled words and lets the user correct or ignore each finding.

## Problem

Test-case text (titles, descriptions, Gherkin/generic bodies) accumulates typos. The app already enables the native WebView2 spellchecker inside individual editors (red squiggles), but that engine exposes **no programmatic results** — it cannot power a profile-wide "here is every typo" report, and it only works while a field is open for editing. There is no way to see, across all synced tests, which ones contain spelling errors, nor to fix them in bulk from one place.

## Goal

A user opens the **Misspellings** view, clicks **Scan for typos**, and sees every misspelled word across the profile's synced test cases — grouped by test, showing the field, the word in context, and ranked suggestions. For each finding the user can **Apply** a suggestion (writes the correction back through the normal edit pipeline) or **Ignore** the word (suppressed in this and future scans). The feature works fully offline against demo data.

## Non-Goals (v1, YAGNI)

- **Test steps** (`action`/`data`/`expected`) — excluded because steps are lazily cached (only present for tests a user has already opened); scanning them would either give silent partial coverage or require thousands of on-demand backend fetches. Clean future extension.
- **Custom fields, preconditions** — future extensions.
- **Grammar / style checking** — spelling only.
- **Non-English dictionaries** — English only.

## Decisions (from brainstorming)

| Decision | Choice | Rationale |
|---|---|---|
| View behavior | Report **+ inline fix** (Apply / Ignore per finding) | Correcting in place is the point; a read-only list forces the user to hunt the test elsewhere. |
| Field scope | **Always-synced fields only**: `summary`, `description`, `cucumber_scenario`, `generic_definition` | All present in SQLite after normal sync → 100% of synced tests scannable instantly, fully offline. All four are in `editableFields`, so write-back is uniform. |
| Engine | **Go backend + embedded wordlist**, one `App.ListMisspellings` binding | Fits the architecture (logic in `internal/`, `app.go` a thin adapter); scanning stays server-side and efficient; `//go:embed` is already a proven pattern (`main.go`). |
| Noise control | **3 layers**: tokenizer heuristics + embedded domain allow-list + persisted user ignore | Test text is dense with jargon/acronyms/keys/URLs/code. Without suppression, real typos drown in false positives. |
| Apply | **Server-side** offset splice → existing `EditTestField` | Avoids frontend/backend tokenization drift; reuses the pending-change + audit pipeline; corrections flow into the normal commit-to-Xray queue. |
| Ignore persistence | **Global** app-level allow-list (settings store) | Jargon is user-wide (`eUICC` ignored once should stay ignored across profiles). |
| Scan trigger | **On-demand button** ("Scan for typos"), not automatic | A full scan is cheap but should not run on every profile switch; mirrors the "Load coverage" pattern. |

## Architecture

A pure, DB-free `internal/spellcheck` package does the linguistic work. `app.go` orchestrates: it pulls test text from the repository and runs the checker. A new `MisspellingsView.tsx` renders results and drives Apply/Ignore.

```
MisspellingsView.tsx
   │  ListMisspellings(profileID)            Apply  → ApplyCorrection → EditTestField (existing)
   ▼                                         Ignore → AddIgnoreWord (new, settings-backed)
app.go  ── paginates repo.ListTests ──►  spellcheck.ScanTests(tests, checker) []Finding
                                              │
       internal/spellcheck: Checker (tokenizer + dictionary + allow-list + suggester)
              └─ words_en.txt (//go:embed)   +   domain allow-list (embedded)
```

The engine never touches the database. The DB-facing orchestration (`ListMisspellings`) lives in `app.go`; the pure scan over already-fetched rows lives in `spellcheck.ScanTests`.

## Components

### `internal/spellcheck` (new package, pure / no DB)

- **`checker.go`** — `Checker` holds the dictionary set and the merged allow-list (embedded domain terms + injected user ignore words). Core method:
  - `CheckText(field, text string) []Finding` — tokenizes `text`, skips noise tokens (see rules), dictionary-checks the rest (case-insensitive), and attaches ranked suggestions to each unknown token.
- **`words_en.txt`** — embedded English wordlist via `//go:embed`, loaded once into a `map[string]struct{}`. **Sourcing/license to confirm in the plan** — a permissively-licensed list (e.g. SCOWL, or dwyl/english-words MIT), ~1–3 MB.
- **`allowlist.go`** — the curated embedded domain terms: `xray`, `euicc`, `pkcs`, `aspice`, `rsp`, `mno`, `isd-p`, Gherkin keywords (`given`/`when`/`then`/`scenario`/`feature`/`background`/`outline`/`examples`), and similar recurring domain vocabulary observed in the demo datasets.
- **`ScanTests(tests []TestText, c *Checker) []Finding`** — loops the four fields of each `TestText` and aggregates findings. DB-free (operates on already-fetched rows).
- **`suggest`** — bounded Levenshtein suggestions: max edit distance 2, candidate set narrowed by length window and first-letter bucketing so a large dictionary stays fast. Returns the top N (e.g. 3) ranked by distance then frequency/length heuristic.

### `app.go` (new bindings)

- `App.ListMisspellings(profileID string) ([]spellcheck.Finding, error)` — `requireStore()` guard; paginate `repo.ListTests` (500/page cap) across the whole profile; build `[]TestText`; call `spellcheck.ScanTests`; return aggregated findings. Per-test scan errors are collected and skipped, not fatal.
- `App.ApplyCorrection(profileID, testKey, field string, offset, length int, replacement string) error` — `requireStore()` guard; re-read the current field value, validate the offset window still matches the flagged word (guard against a stale finding after edits), splice in `replacement`, and call the existing `repo.EditTestField(profileID, testKey, field, newValue)`. If the window no longer matches, return a clear "finding is stale, re-scan" error rather than writing.
- `App.AddIgnoreWord(word string) error` — persist `word` (lowercased) to the global ignore list in the settings store; subsequent `ListMisspellings` calls fold it into the checker's allow-list.
- (Optional) `App.ListIgnoreWords()` / `App.RemoveIgnoreWord(word)` — for a small "manage ignored words" affordance; may be deferred to a follow-up.

### Frontend

- **`frontend/src/components/MisspellingsView.tsx`** (new) — mirrors `PreconditionsView.tsx`: props `{ profileId, refreshKey, onChanged }`; a `[profileId, refreshKey]` fetch effect with a `cancelled` guard; on-demand **Scan for typos** button; renders findings grouped by test in a `board-table`, each row showing field, word-in-context snippet, and suggestion chips with **Apply** and **Ignore** actions. Apply calls `ApplyCorrection` then `onChanged()`; Ignore calls `AddIgnoreWord` then re-scans.
- **`frontend/src/App.tsx`** — 4 edits: add `"misspellings"` to the `view` union; import the component; add a nav `<button>` tab; add a render branch. (Native "View" menu entry in `main.go` is optional — Coverage skipped it.)
- **`frontend/src/api.ts`** — add `ListMisspellings`, `ApplyCorrection`, `AddIgnoreWord` to the re-export block; add a `Finding` TS interface mirroring the Go struct.
- Wails bindings (`App.js`/`App.d.ts`/`models.ts`) regenerate via `wails generate module`.

## Data Flow

1. **Scan** — user clicks Scan → `ListMisspellings(profileID)` → paginate `ListTests` (offline SQLite) → checker flags tokens → returns `Finding { TestKey, Field, Word, Snippet, Offset, Length, Suggestions []string }` → view groups by `TestKey`.
2. **Apply** — user picks a suggestion → `ApplyCorrection(profileID, testKey, field, offset, length, replacement)` → server re-reads field, validates the window, splices, calls `EditTestField` → correction queued as a pending change + audit entry → view removes the finding and bumps `refreshKey`.
3. **Ignore** — user clicks Ignore → `AddIgnoreWord(word)` → persisted to global settings → view re-scans; the word (and all its occurrences) disappear from results and stay gone in future scans.

## Noise-Suppression Rules (tokenizer)

A token is **skipped** (not dictionary-checked) if it:
- is ALL-CAPS (treated as an acronym), or
- matches a Jira-key shape (`[A-Z][A-Z0-9]+-\d+`), or
- is a URL or email, or
- contains a digit, or
- is `camelCase` / `snake_case` / `kebab-case` (embedded case-change or separator), or
- is shorter than 3 characters, or
- is present in the embedded domain allow-list or the user ignore-list.

Everything else is checked case-insensitively against the dictionary. Unknown → a `Finding` with ranked suggestions.

## Error Handling

- `requireStore()` on every binding.
- A scan error on a single test is collected and skipped; the scan completes and reports coverage (tests scanned). One bad row never aborts the whole scan.
- `EditTestField` is already atomic, no-op-safe, and stale-safe (`baseVersion`); `ApplyCorrection` adds a pre-write offset-window check so a stale finding fails cleanly ("re-scan") instead of corrupting text.
- Empty dictionary or zero synced tests → empty result, not an error.
- Suggestions are bounded (max distance 2 + candidate bucketing) to keep large-dictionary lookups fast.

## Testing

- **Pure checker unit tests** (the bulk, in `internal/spellcheck`): known typos flagged; allow-list and ignore-list respected; acronyms / Jira keys / URLs / camelCase / digit-bearing / short tokens skipped; suggestions ranked correctly; case-insensitive matching; empty/whitespace input.
- **`ScanTests`**: synthetic multi-field tests → correct per-field findings with accurate offsets.
- **`App.ListMisspellings`** integration test on a seeded in-memory repo (reuse the existing `testrepo` test harness): plant known typos across the four fields in several tests, assert exact findings and that clean tests produce none.
- **`ApplyCorrection`**: offset splice produces the exact corrected value; a pending change is queued; a stale offset window is rejected without writing.
- **`AddIgnoreWord`**: an ignored word is absent from the subsequent scan and persists across a simulated restart (settings-backed).

## Files Touched

**New**
- `internal/spellcheck/checker.go`
- `internal/spellcheck/allowlist.go`
- `internal/spellcheck/words_en.txt` (embedded)
- `internal/spellcheck/checker_test.go`
- `frontend/src/components/MisspellingsView.tsx`

**Modified**
- `app.go` — `ListMisspellings`, `ApplyCorrection`, `AddIgnoreWord` (+ optional list/remove)
- `internal/settings` (or existing settings store) — global ignore-word list persistence
- `frontend/src/App.tsx` — view union, import, nav tab, render branch
- `frontend/src/api.ts` — re-exports + `Finding` interface
- Wails generated bindings (regenerated, not hand-edited)
- Tests: `app_*_test.go` (or a new `app_misspellings_test.go`)

## Risks / Notes

- **Wordlist license & size** — must be permissively licensed and reasonable to embed (~1–3 MB). Confirm the exact source in the plan; this is the one external artifact.
- **Suggestion quality** — a plain wordlist + Levenshtein gives decent but not Hunspell-grade suggestions. Acceptable for v1; the `suggest` function is isolated so it can be upgraded later without touching the view or bindings.
- **Ignore scope** — chosen global; if a future need arises for per-profile jargon, the ignore store can gain a profile dimension without changing the view contract.
- **No schema bump expected** — the ignore list can live in the existing settings store; if a dedicated table is cleaner, it's additive only.
