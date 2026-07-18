# Design: Cucumber & Generic Test Type Support

**Date:** 2026-07-18
**Branch:** `feature/test-types-cucumber-generic` (off `origin/main` @ 0c154ef)
**Status:** Approved for planning

## Problem

Xray Server/DC models three Test Types on the same `Test` Jira issue type, distinguished
by a **Test Type** custom field: `Manual`, `Cucumber`, `Generic`. Today
`xray-test-manager` fully supports only **Manual**. Cucumber and Generic tests sync with
their type tag but show a blank body (no editor, no content), because the app is coupled
to the Manual step model (Action / Data / Expected Result rows).

Goal: extend to full parity — **view + local edit + commit** — for Cucumber and Generic,
including best-effort, non-destructive body conversion when a test's type changes.

## How the three types differ (Xray Server/DC)

| Type | Body content | Storage / access |
|------|--------------|------------------|
| **Manual** (today) | Structured steps (Action / Data / Expected Result) | Xray raven endpoint `/rest/raven/2.0/api/test/{key}/steps` — multi-row CRUD |
| **Cucumber** | A Gherkin scenario (text) + a Scenario Type enum (`Scenario` / `Scenario Outline`) | Two plain Jira custom fields on the issue — no steps |
| **Generic** | A single plain-text definition (e.g. an automated-test classname/path) | One Jira custom field — no steps |

Key consequence: Cucumber and Generic have **no step rows**. Their body is custom-field
text read/written through the standard Jira issue API (`/rest/api/2/issue/{key}`), not the
raven step endpoint. Field ids are instance-specific and must be *discovered* from
`/rest/api/2/field`, never hard-coded.

Sources:
- [Using custom fields — Xray Server + DC](https://docs.getxray.app/display/XRAY/Using+custom+fields)
- [Importing Cucumber Tests — REST — Xray Server + DC](https://docs.getxray.app/display/XRAY/Importing+Cucumber+Tests+-+REST)
- [Tests — REST — Xray Server + DC](https://docs.getxray.app/display/XRAY/Tests+-+REST)

## Current architecture (as mapped)

- `test_case.exec_type` (`internal/store/store.go`) already stores the Test Type value and
  is synced (`internal/jira/search.go`) and committed (`internal/jira/edit.go`
  `ExecTypeFieldValue`). Manual is implicit (empty string treated as Manual).
- Manual steps live in the `test_step` table (`action`, `data`, `expected`,
  `called_test_key`); CRUD is journaled to `pending_change` and pushed by
  `internal/syncer/commit.go`.
- Test Type is resolved as a custom field via `customfields.go` `testTypeFieldID()`
  (best-effort; returns `""` when absent).
- `frontend/src/components/TestDetail.tsx` renders the manual step grid unconditionally
  and shows the type in an "Execution type" dropdown (which already edits `exec_type`).
- Demo (`internal/jira/demo.go`) tags tests with all four types (`demoExecTypes`) but
  leaves Cucumber/Generic bodies empty.
- Current `schemaVersion = 39`.

Cucumber and Generic have **zero** body-content support today.

## Design decisions (approved)

1. **Storage: new columns on `test_case`** (1:1 with the test), not a key/value table.
2. **Conversion: non-destructive, best-effort pre-fill.** Steps, scenario, and definition
   live in separate storage, so switching type never destroys a body. "Type" selects which
   body is *active* (committed to Jira); the others stay cached locally — matching Jira,
   where changing the Test Type field does not clear the other custom fields.

## Section 1 — Data model & backend read/write

**Store (`internal/store/store.go`, schemaVersion 39 → 40, additive):** three new columns
on `test_case`:

- `cucumber_scenario TEXT NOT NULL DEFAULT ''` — the Gherkin text
- `cucumber_type TEXT NOT NULL DEFAULT ''` — `Scenario` | `Scenario Outline` (empty for non-Cucumber)
- `generic_definition TEXT NOT NULL DEFAULT ''` — the plain-text definition

Add to `baseSchema` and mirror in an idempotent `if current < 40` migration block. Manual
steps stay in `test_step`, untouched. `exec_type` remains the single source of "which type."

**Field resolution (`internal/jira/customfields.go`):** best-effort resolvers alongside
`testTypeFieldID()`, each caching an instance-specific id from `/rest/api/2/field` and
returning `""` (no error) when absent:

- `cucumberScenarioFieldID()` → "Cucumber Scenario"
- `cucumberTypeFieldID()` → "Cucumber Test Type" (a.k.a. Scenario Type; select field)
- `genericDefinitionFieldID()` → "Generic Test Definition"

**Sync read (`internal/jira/search.go` + `internal/testrepo`):** during the bulk pull,
request these three field ids alongside `exec_type`; parse values into the new columns via
the existing upsert path. Empty when the instance/test lacks them.

**Commit write (`internal/syncer/commit.go` + `internal/jira/edit.go`):** these are plain
Jira custom fields, so they PUT through `/rest/api/2/issue/{key}` (the pattern
`ExecTypeFieldValue` / `EditTestCustomField` establish) — not the raven step endpoint. Text
fields send a string; `Cucumber Test Type` sends `{"value": "..."}`. On commit, only the
**active** type's body is pushed (plus the `exec_type` field); inactive bodies stay local.
Pending changes use new `entity_type` values `test_cucumber` and `test_generic`, coalesced
by the existing `UNIQUE(profile_id, entity_type, entity_key, field)`.

## Section 2 — Conversion transforms & UI

**Conversion (new `internal/testrepo/testtype.go`, pure functions, unit-tested):**
best-effort, non-destructive, pre-fill only a target body that is empty — the source body
is never touched.

| From → To | Transform |
|-----------|-----------|
| Manual → Cucumber | `Scenario: <summary>`, then per step `When <action>` / `And <data>` / `Then <expected>`; header comment "generated from N steps — review" |
| Manual → Generic | Numbered flat text: `1. <action> — Data: <data> — Expected: <expected>` |
| Cucumber → Manual | One step per Given/When line (→ action); a following Then attaches as that step's expected |
| Cucumber → Generic | Raw scenario text as the definition |
| Generic → Manual | Definition split into steps by line (action per line) |
| Generic → Cucumber | `Scenario: <summary>` with the definition as a `Given` / comment body |

**Where it runs:** inside the `exec_type` change path. When the type changes and the target
body is empty, the backend pre-fills the target column from the transform and journals it as
a pending change (so it commits normally). If the target already has content, we do **not**
overwrite — the UI offers "Pre-fill from previous type?" as an explicit opt-in. Keeping this
server-side makes it unit-testable and keeps transforms out of React.

**UI (`frontend/src/components/TestDetail.tsx`):** one conditional on `execType`:

- **Manual / Automated / (empty)** → the existing step grid, unchanged.
- **Cucumber** → a monospace Gherkin `<textarea>` + a **Scenario Type** select
  (`Scenario` / `Scenario Outline`). Plain textarea for v1 (keyword highlighting deferred).
- **Generic** → a monospace definition `<textarea>`.

**API surface:** reuse the existing `EditTestField(profileId, testKey, field, value)` for
`cucumber_scenario`, `cucumber_type`, and `generic_definition` — they are test fields and
inherit the same pending-change journaling, coalescing, revert-drop, and conflict handling
the `exec_type` edit already uses. Add a read helper only if the detail payload does not
already carry the new columns; otherwise no new editing methods.

## Section 3 — Demo seeding, testing & scope

**Demo seeding (`internal/jira/demo.go`):** populate the non-Manual bodies so the feature is
exercisable offline (deterministic, index-driven like `demoExecTypeForIndex`):

- **Cucumber** demo tests → a realistic Gherkin `Scenario` (some `Scenario Outline` with an
  `Examples:` table) + `cucumber_type`.
- **Generic** demo tests → a plausible definition (classname or script path).

**Testing (Go unit tests; no frontend runner):**

- `testtype_test.go` — the six conversion transforms, table-driven (round-trips, empty
  inputs, multi-step, Scenario Outline).
- Store migration — fresh DB creates v40; a v39 DB upgrades idempotently and gains the three
  columns.
- Sync read (demo client) — a Cucumber demo test lands with non-empty `cucumber_scenario` +
  `cucumber_type`; a Generic one with `generic_definition`.
- Edit / pending-change — editing `cucumber_scenario` journals a `test_cucumber` pending row;
  reverting to original drops it; changing `exec_type` into an empty-bodied type pre-fills +
  journals the target.
- Commit (demo/stub) — the active body PUTs as a Jira custom field; inactive bodies are not
  pushed.
- Regression — existing Manual step tests stay green (the conditional must not disturb the
  Manual path).

**Explicit scope cuts (YAGNI):**

- No from-scratch test creation (editor of synced tests only — matches today).
- No bulk type-change across many tests.
- No Gherkin syntax validation / linting; no `.feature` file import (Xray has its own
  feature-import REST — out of scope).
- No syntax highlighting in v1 (plain monospace textarea).
- Live Jira remains verified only in demo (consistent with the rest of the app, Phase 7),
  but real REST code paths are written, mirroring how Manual steps have real code.

## Files to change

**Backend (Go):**

- `internal/store/store.go` — schemaVersion → 40; three columns in `baseSchema` + `if current < 40` migration.
- `internal/jira/customfields.go` — resolvers for Cucumber Scenario, Cucumber Test Type, Generic Test Definition.
- `internal/jira/search.go` — fetch the three fields during sync.
- `internal/jira/edit.go` — field-value shaping for the custom-field PUTs.
- `internal/syncer/commit.go` — commit `test_cucumber` / `test_generic` pending rows via the Jira issue API.
- `internal/testrepo/testrepo.go` — model the new columns; upsert; route the new fields through `EditTestField`; invoke conversion on `exec_type` change.
- `internal/testrepo/testtype.go` — **new** — pure conversion functions.
- `internal/jira/demo.go` — seed demo Cucumber/Generic bodies.

**Frontend (React/TypeScript):**

- `frontend/src/api.ts` — types for the new columns; any new read helper.
- `frontend/src/components/TestDetail.tsx` — the `execType` conditional (step grid / Gherkin editor / definition editor) + Scenario Type select + pre-fill opt-in.

**Test files:** `internal/testrepo/testtype_test.go`, plus additions to store / syncer / jira demo tests.

## Verification

- `go build ./...` and `go test ./...` clean.
- Manual path unchanged (regression tests green).
- Demo E2E: a Cucumber test shows its scenario + Scenario Type and edits/commits; a Generic
  test shows its definition and edits/commits; switching a Manual test to Cucumber pre-fills
  a reviewable Gherkin skeleton without losing the steps (switch back restores them).
- `frontend` `npm run build` (tsc + vite) clean.

## Open risks

- **Custom field id discovery** on real instances: resolvers must tolerate missing fields
  (return `""`, degrade gracefully) — verified only in demo until Phase 7.
- **Conversion fidelity** is best-effort and lossy by nature; mitigated by non-destructive
  storage (source preserved) and a "review before commit" posture.
- **Cucumber Test Type field naming** varies ("Cucumber Test Type" vs "Scenario Type") across
  Xray versions; resolver should try known aliases.
