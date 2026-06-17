# Changelog

All notable changes to **Xray Test Manager** are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
The version is single-sourced in `wails.json` (`info.productVersion`).

## [1.5.0] - 2026-06-17

### Added
- **Searchable pickers.** Adding a requirement or precondition (the bulk modals
  and the per-test "+ Link requirement" / "+ Add precondition" controls) and the
  dashboard traceability filters now have a type-to-filter search box, so long
  lists no longer mean scrolling. (RND_P_4TFINT_05-200)
- **Duplicate a step.** Each step in the detail panel has a Duplicate action that
  appends a copy on the same test, for both manual steps and call-test steps.
  (RND_P_4TFINT_05-204)
- **Clone a test.** A Clone action in the detail header drafts a new local test
  copying the source's fields and steps (call steps preserved) and opens the
  fresh draft for editing. (RND_P_4TFINT_05-206)
- **Spell check on test text.** The browser's native spell check is enabled on
  step Action / Data / Expected, descriptions, and summaries, flagging typos as
  you type, fully offline. (RND_P_4TFINT_05-201)
- **Sync Test Calls.** The Test Calls view has a Sync button that re-pulls steps
  for the known caller tests to refresh the call graph, without a full profile
  sync. (RND_P_4TFINT_05-207)
- **Open a test in Jira.** The test key in the detail panel header links to the
  test's real Jira issue, opened in the system browser. (RND_P_4TFINT_05-211)

### Fixed
- Committing changes no longer fails when a newly added step is reordered and
  then deleted before commit: the cancelled step's temporary id is pruned from
  the queued reorder, so the commit doesn't reorder a step Jira never created.
  (RND_P_4TFINT_05-203)

### Changed
- Lists sort Jira keys by their issue number numerically (KEY-2 before KEY-10
  before KEY-100) instead of as text, and the Browse grid defaults to newest
  first. The Preconditions and requirement lists, and the pickers they back,
  order newest first. (RND_P_4TFINT_05-202, RND_P_4TFINT_05-205)

## [1.4.0] - 2026-06-14

### Added
- **Commit conflict management.** When a Test changed in Jira since you started
  editing it, commit now does a three-way merge: non-overlapping edits merge
  automatically and only genuinely overlapping fields stop you, resolved per
  field (keep mine / keep theirs) in a Base / Theirs / Mine table in the
  pending-changes dialog. Covers test fields, step content, step reorder/delete,
  and custom fields; a remotely deleted Test can be recreated as a new local
  Test. (RND_P_4TFINT_05-198)
- **macOS support.** Builds and runs on macOS as a universal `.app` (Apple
  Silicon + Intel) alongside Windows. The PAT is stored in the macOS Keychain
  (Windows Credential Manager on Windows). CI builds and publishes the `.app`
  on each release, with optional code-signing + notarization.
  (RND_P_4TFINT_05-199)
- **Manage Profiles modal.** A master-detail Manage Profiles dialog (with delete
  profile) in the top bar replaces the profile dropdown.

### Fixed
- **Create / Edit Profile** — the Project Key field accepts underscores, so Jira
  Data Center keys like `RND_P_4TFINT_05` / `RND_I_XXXXX_XX` validate correctly
  (previously only letters/digits were allowed). (RND_P_4TFINT_05-197)
- **Create / Edit Profile** — the Jira base URL is normalized (surrounding
  whitespace and trailing slashes stripped) and validated as a well-formed
  http(s) URL (or `demo`).

### Changed
- CI runs a build check (Windows + macOS + Go tests) on every pull request, and
  the release workflow now publishes both Windows and macOS artifacts.
- Frontend toolchain bumped (TypeScript 6, Vite) and GitHub Actions pin updates.

## [1.3.0] - 2026-06-12

### Added
- **Call-test steps.** A test step can now call another test (Xray "test call")
  instead of holding manual Action/Data/Expected content. Add one with
  **+ Call test** in the Steps header; it renders as "⮡ Calls KEY" and reorders,
  deletes, and commits like any other step. (Schema v19 adds `called_test_key`.)
- **Test Calls view.** A new tab mapping which tests call which: grouped by
  caller, broken/missing call targets flagged, call cycles detected (Tarjan SCC)
  and highlighted, a "calls" badge on caller rows in the Browse grid, plus
  pagination and expand-all / collapse-all controls.
- **Multi-select Sankey filters.** Both dashboard traceability diagrams filter by
  several requirements / Test Plans / Test Executions at once via a checkbox
  dropdown.
- **Cross-project execution filter.** The traceability Sankey gains a
  **Cross-project only** toggle that surfaces Test Plans in the current project
  whose runs live in a different project. Cross-project executions are
  auto-discovered during sync (seeded in demo; live wiring documented).
- **Per-view partial sync.** Requirements and Containers each get a **Sync**
  button to refresh just their data; the Dashboard gets a **Refresh**.
- **Open a covering test from Requirements.** Clicking a covering test in a
  requirement's coverage list opens its full detail in a slide-over.

### Fixed
- Committing a newly created test whose steps were reordered before commit no
  longer fails with a server error (HTTP 500); the steps are created in their
  final order and the stale reorder is cleared. Covered by a regression test.

### Changed
- The view tabs (Browse / Preconditions / … ) always sit on their own row,
  separate from New Test / More / Sync, at every window width.
- Installer switched from NSIS to **Inno Setup**.
- GitHub Actions pinned to commit SHAs and kept current via **Dependabot**
  (supply-chain hardening).

## [1.2.1] - 2026-06-11

### Fixed
- Step reorder failing with HTTP 400 — the reorder PUT now includes the step
  fields.

### Changed
- Requirements and Duplicates added to the View menu; themed "Discard all"
  confirmation dialog.

## [1.2.0] - 2026-06-11

### Added
- Duplicate detection with side-by-side step compare.
- Requirements & coverage (multi-phase): fetch requirements + coverage links,
  coverage view, link/unlink from the test detail, bulk-link from the grid,
  edit/delete requirement issues, sign-off audit export, dashboard coverage
  panel, and a dedicated requirement-traceability Sankey. (Store schema v17.)

## [1.1.1] - 2026-06-10

### Added
- Profile editing, bulk execution results, sample-data controls, and sync UX
  improvements.

## [1.1.0] - 2026-06-10

### Added
- Interactive test-case creation (the New Test panel).

### Changed
- CI release actions bumped to Node 24.

## [1.0.2] - 2026-06-09

Earlier baseline release (sync, fast browse/search/filter/sort, local editing
with on-commit sync, bulk operations, Test Sets/Plans/Executions, Test
Repository folders, preconditions, CSV/XLSX import & export, pytest scaffold,
statistics dashboard, diagnostics, light/dark themes, profile management).

[1.5.0]: https://github.com/veenone/xray-testcase-manager/releases/tag/v1.5.0
[1.4.0]: https://github.com/veenone/xray-testcase-manager/releases/tag/v1.4.0
[1.3.0]: https://github.com/veenone/xray-testcase-manager/releases/tag/v1.3.0
[1.2.1]: https://github.com/veenone/xray-testcase-manager/releases/tag/v1.2.1
[1.2.0]: https://github.com/veenone/xray-testcase-manager/releases/tag/v1.2.0
[1.1.1]: https://github.com/veenone/xray-testcase-manager/releases/tag/v1.1.1
[1.1.0]: https://github.com/veenone/xray-testcase-manager/releases/tag/v1.1.0
[1.0.2]: https://github.com/veenone/xray-testcase-manager/releases/tag/v1.0.2
