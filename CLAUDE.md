# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

**Xray Test Manager** — a lightweight Windows desktop application for managing
**Xray test cases in Jira Data Center** at scale (10k+ test cases, ceiling 50k).
It exists because the Jira browser UI is too slow for bulk test-case work.

Local-first: test data is synced into a local SQLite cache for instant
browse / search / filter; edits are tracked locally and pushed back to Jira
**on commit**. Jira is always the system of record.

Full planning and requirements live in the Outline collection **"Xray Test
Manager"** — Overview, Architecture, Functional Requirements (FR-1…FR-13),
Non-Functional Requirements, Roadmap (7 phases), Risks & Open Questions.
Requirement IDs (`FR-1.4`, `FR-13.5`, …) are referenced throughout the code and
in commit messages; keep using them.

## Stack

- **Go** backend + **Wails v2** desktop shell + **React + TypeScript** frontend.
- **SQLite** local store via the pure-Go `modernc.org/sqlite` driver (no cgo).
- Single distributable **Windows** executable; WebView2 is the only runtime
  prerequisite.
- Target server: **Jira DC 8.14+** (Personal Access Tokens) and **Xray
  Server / DC 8.4.0**.

## Layout

```
main.go              Wails entry point, window options, bound App
app.go               App struct — backend wired to the React frontend
internal/
  jira/              Jira DC + Xray Server REST client (/rest/api/2/ + /rest/raven/2.0/)
  store/             SQLite local store, schema, and migrations
  profile/           Connection profiles + OS credential storage
  settings/          Global app preferences (default profile, theme)
  testrepo/          Local Test repository — browse/search/filter + all local mutations
  syncer/            Pull-sync engine (Jira -> store) and commit engine (store -> Jira)
frontend/            React + TypeScript (Vite), rendered in WebView2
  src/api.ts         Re-exports the generated Wails bindings as the frontend's typed API
  wailsjs/           GENERATED bindings — do not hand-edit
scripts/release.ps1  Version-stamp + bundle portable exe and Inno Setup installer into dist/
```

## Architecture — the big picture

**The bound `App` is the only boundary.** Every exported method on `App` in
`app.go` is callable from React (Wails generates `frontend/wailsjs` bindings from
them). `app.go` is a thin adapter: it validates, loads the profile + PAT, and
delegates to `internal/`. Keep real logic out of it. Adding/renaming an exported
`App` method changes the frontend API surface — `frontend/src/api.ts` re-exports
the generated bindings, so regenerate (`wails dev`/`wails build`) and update
`api.ts` to match.

**`requireStore()` guards every store-dependent method.** The Windows GUI has no
console, so a backend init failure must surface as a returned error, not a
nil-pointer panic. `startup` captures any failure into `startupErr`; `Health()`
and `GetDiagnostics()` stay callable even when the store failed to open so the
frontend can show what actually broke.

**Local edits are a journal, committed later — not written through.** Mutating
methods (`EditTestField`, `TransitionTest`, `BulkEditTests`, step/precondition/
container/folder ops, review verdicts) do **not** call Jira. They write to the
`pending_change` table (coalesced by `UNIQUE(profile_id, entity_type,
entity_key, field)`, so repeated edits collapse and reverting to the original
drops the row) plus an `audit_log` entry. `CommitPendingChanges` is the single
place that pushes to Jira, via `syncer.Engine.CommitChanges`, grouping pending
rows by entity and applying field updates → transitions → step CRUD per Test.
Failed/conflicted Tests keep their pending rows so the user can retry or discard.

**Conflict detection is optimistic.** Each pending change records the remote
`updated` it was based on (`base_version`). At commit, the engine re-fetches the
remote `updated`; if it moved, the Test is reported as a `Conflict` and held
back (FR-1.4). The user syncs, then either overrides (rebase onto remote) or
keeps remote (discard local).

**Created entities use temporary keys.** New Preconditions/Containers/Steps get
a placeholder key locally; the commit path creates them in Jira first, then
rewrites dependent pending rows (e.g. associations) to the real key before the
per-Test pass runs.

**Steps and custom-field values load lazily.** The bulk sync pulls Test fields
only — fetching steps/custom fields per Test would be thousands of extra round
trips. `GetTestSteps` / `GetTestCustomFields` fetch on first detail-panel open
and cache; `forceRefresh` re-pulls.

**Schema migrations are ordered.** `store.go` defines `baseSchema` →
`applyMigrations` → `indexSchema`, so an old DB gets missing columns added
before any index references them. Bump the `schemaVersion` constant on any
schema change (it surfaces in diagnostics).

## Demo mode — how to run without Jira

Create a profile with **Jira base URL `demo`** (any project key, any token).
`jira.isDemoURL` short-circuits every REST call to a deterministic generator
serving ~5,000 tests plus sample Test Sets/Plans/Executions, folders, and
preconditions, so the whole UI works end to end offline. The header shows a
yellow `DEMO` chip. This is the default way to exercise the app today.

## Conventions

- Go: standard `gofmt`; package names lowercase; document exported identifiers.
- Backend logic lives in `internal/`; `app.go` only adapts it to Wails bindings.
- `internal/` is import-private to this module — keep it that way.
- Jira is the system of record. The local store is a cache plus a pending-change
  journal — never authoritative.
- Credentials (PAT) go to the Windows Credential Manager — never the database,
  never plaintext, never logs.
- Code markers: `TODO(xtm): desc` for planned work; reference the FR / phase.
  Live-Jira REST calls not yet verified against a real instance are stubbed and
  marked `NOTE`/`TODO(xtm)` in `internal/jira/`.

## Building, running, and testing

```powershell
wails dev                       # run with hot reload (Go + Vite)
wails build                     # produce build/bin/xray-test-manager.exe
go build ./...                  # compile-check the Go backend only
go test ./...                   # run all Go tests (the backend has broad coverage)
go test ./internal/syncer/...   # one package
go test ./internal/testrepo/ -run TestImportTests   # one test by name
gofmt -w .                      # format

# Frontend (usually run via wails)
cd frontend; npm run build      # tsc typecheck + vite build
cd frontend; npx vitest run     # Vitest unit tests (query hooks + contexts)
cd frontend; npx tsc --noEmit   # typecheck only
```

Prerequisites: Go 1.25+, Node.js, and the Wails CLI
(`go install github.com/wailsapp/wails/v2/cmd/wails@latest`).

Most backend behavior is verified by Go unit tests against the store and the
demo client. When changing backend logic, add or update the `_test.go` beside
it. The frontend has a Vitest suite (`frontend/src/**/*.test.ts{,x}`) covering
the TanStack Query hooks and the React contexts (`src/contexts/`); when changing
that logic, add or update the `.test.tsx` beside it.

## Releasing

Version is single-sourced in `wails.json` (`info.productVersion`).
`scripts/release.ps1 -Version X.Y.Z` stamps it, builds the portable exe, compiles
the installer from `build/windows/installer/installer.iss` with Inno Setup (needs
`ISCC.exe`), bundles the user guide, and writes `SHA256SUMS.txt` into `dist/`
(`-NoInstaller` skips the installer). Pushing a `vX.Y.Z` tag triggers
`.github/workflows/release.yml` on `windows-latest` to build and publish a
GitHub Release. See README for the artifact table.

## Current status

**Feature-complete in demo mode (Phases 0–6).** Implemented and exercised end to
end against demo data: sync, fast browse/search/filter/sort, saved views,
configurable columns, local field/step/custom-field editing with on-commit sync
and conflict resolution, workflow transitions (single + bulk), all bulk
operations, Test Sets/Plans/Executions (board, CRUD, allocate), Test Repository
folders, preconditions, test review (single + bulk), CSV/XLSX import + export, a
pytest scaffold generator, a statistics dashboard with a traceability Sankey,
diagnostics, sync history, light/dark themes, and profile management.

**Phase 7 pending: live Xray/Jira REST wiring.** The real REST calls in
`internal/jira/` are stubbed behind the demo short-circuit until they can be
verified against an actual Xray Server/DC 8.4.0 instance.
