# Changelog

All notable changes to **Xray Test Manager** are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
The version is single-sourced in `wails.json` (`info.productVersion`).

## [Unreleased]

## [1.9.0] - 2026-08-11

Major feature release for the 1.9.0 line, finalizing the `1.9.0a` test builds. The
headline is a **backend-agnostic core**: the local store is now a neutral hub that
can pull from and publish to more than one test-management backend, with **Kiwi
TCMS** as the first non-Xray target and a **migration bridge** between them. On top
of that: **coverage groups publish to Xray as Test Sets** with drift detection,
**cross-project linking** for preconditions / test calls / cloned steps, a new
**Misspellings** view, **duplicate-precondition** detection, **Cucumber / Generic**
test types, container and import refinements, and faster sync with clearer progress.
Schema reaches v48.

### Added

**Backend-agnostic core + Kiwi TCMS (#50)**
- The remote layer is now a **`Backend` interface** with a neutral hub identity, so
  the syncer and commit engine no longer bind to Xray directly. Each backend
  advertises **capabilities**, and the UI hides or degrades what a backend cannot do
  (Xray keeps every existing feature unchanged).
- **Kiwi TCMS backend (token auth):** read and write against a Kiwi instance
  (Products / Versions / Builds / TestPlans / TestCases / TestRuns), mapped into the
  neutral model (inline-text steps, settable status). A **migration bridge** pulls
  from one connection and publishes to another with a capability-gap pre-flight.

**Coverage: publish to Xray (#65, #67)**
- **Publish coverage groups to Xray as Test Sets**, with **drift detection** that
  flags when a published Test Set diverges from the local coverage group. Works on a
  `demo` profile for simulation, so the flow can be exercised offline.

**Cross-project linking (#80, #322)**
- **Link across projects:** the precondition picker, the call-test picker, and the
  clone-steps picker can now reach tests and preconditions in other configured
  projects, via a per-profile **cross-project source list**. The pickers show a
  project sidebar with a table of Key / Summary, pagination, and manual page input.
- **Test Calls: cross-project vs missing (#86).** An unresolved call is now split
  into **cross-project** (the target lives in another project — expected) and
  **missing** (deleted or not synced — worth attention), each with its own badge and
  count, a legend, and a description of cross-project calls at the bottom of the view.

**New views and detections**
- **Misspellings view (#72).** Scan every test case for spelling errors in its
  summary, description, and step text, and jump straight to the offending test.
- **Duplicate preconditions (#79, RND_P_4TFINT_05-323).** The Duplicates feature now
  also detects preconditions with matching content, alongside duplicate tests.

**Test types and executions**
- **Cucumber and Generic test types (#54).** The Xray Test Type field now supports
  Cucumber and Generic in addition to Manual / Automated, across Browse, filters,
  bulk edit, and detail edit.
- **Link an existing bug and add remarks on a Test Execution run (#53,
  RND_P_4TFINT_05-296).** From a run in a Test Execution, link a bug that already
  exists (not only raise a new one) and record a free-text remark on the run.

**Requirements**
- **Collect required custom fields on requirement create (#69).** Creating a
  requirement now discovers and prompts for the target project's required custom
  fields, so the create succeeds on instances that mandate them.
- **Keyboard navigation + collapsible custom fields (#78).** Arrow keys move between
  requirements, and the custom-fields block collapses to keep the panel compact.

**Import**
- **Imported tests land in their Test Repository folder (#57)** and the **folder
  hierarchy is created** for any new folders in the import (RND_P_4TFINT_05-305).

### Changed
- **Faster container sync (#82).** Container sync is rate-limited and concurrent, and
  cross-project execution discovery is scoped to the configured source projects,
  cutting sync time on large instances.
- **Bug sync shows a progress bar (#83).** The bug-sync phase reports item counts
  like the other phases, instead of a static label.
- **Containers refinements (#73, #74, #76, #77, RND_P_4TFINT_05-310, -311).** Filter
  the container list by **label**, with filters separated from the toolbar; status
  filters sit on one row and the run colorbar is **click-to-filter**; the execution
  detail card height is capped; the test detail is docked with collapsible panels and
  a shared pager; copy was humanized.
- **Manage Profiles: explicit modes (#60).** The Manage Profiles dialog distinguishes
  viewing, editing, and creating, so the active profile is not edited by accident.
- **`demo-pkcs` gains the PKCS#11 key-management family (#61)**, and the plain `demo`
  dataset seeds a **cross-project** and a **missing** test call so the Test Calls
  view's unresolved states are demonstrable offline.

### Fixed
- **Call-test steps commit with the real Xray body (#81, RND_P_4TFINT_05-322).** A
  call step now posts `callTestIssueKey` + `testCallStep` (verified against a live
  Xray step response) instead of an unknown field, so committing a call step
  (same-project or cross-project) no longer fails with "Step fields must be provided".
  Large numeric step ids are also formatted correctly instead of in scientific
  notation.
- **Requirement coverage links use the correct direction (#68, #70,
  RND_P_4TFINT_05-275).** Coverage is linked as **"tested by"** with the issue-link
  direction resolved correctly, and the requirement sources modal was redesigned.
- **Pending-changes modal scrolls (#71, RND_P_4TFINT_05-308).** The modal body
  scrolls so the import actions stay visible on smaller windows.
- Bridge-wizard cosmetic nits (#55).

## [1.8.0] - 2026-07-07

Major feature release for the 1.8.0 line: a **requirements-centric suite** (richer requirement detail, create / import / link requirements, a configurable Test <-> Requirement link type, and an Epic layer in the requirement Sankey) alongside a new **Coverage module** (parameter-level coverage, cross-project functional-requirement reuse, per-version coverage with change requests, a graphical **Coverage Map**, an enriched Excel report, and a `demo-pkcs` dataset), plus preconditions, dashboard, test-calls and containers refinements. Covers Jira -265..-280; schema reaches v39. All local; no Jira admin.

### Added

**Requirements suite (Jira -265..-280)**
- Requirement detail shows priority, component(s), fix version(s), sprint, and a collapsible markdown description; Edit mode edits every field, with Components and Fix Versions offering the project's available values (select-or-add).
- Create a requirement (local temp key, pushed to Jira on commit), import requirements from a CSV / XLSX file (summary-dedupe like Gap Analysis, with a downloadable template), and link a requirement to other requirements ("Requires").
- Configurable **Test <-> Requirement link type** (a setting, default **"Tested By"**, chosen from the instance's link types) applied on commit, replacing the previously hardcoded link type (-275).
- Requirements toolbar realigned to the Preconditions design; New-requirement is a left-docked panel with a markdown summary.

**Coverage module**
- **Coverage module (parameter-level test coverage + functional-requirement reuse).** A new **Coverage** tab adds a bounded, local-only capability beside test management: decompose a function into parameter *values* / error codes / boundaries, map existing Tests to each value, and measure coverage as *required values with ≥1 test* (the parameter-level definition — not combinatorial). Group equivalent requirement issues from many customer projects under a local **canonical functional requirement** so coverage is defined once and reused, and see which projects reuse it. Import the existing PKCS#11 parameter-extraction Excel workbook to seed a model, and export a styled coverage report (Summary + per-group + Gaps sheets) for Jira/Confluence. Run status reuses the existing requirement-coverage roll-up. Mappings that reference deleted Tests are flagged as stale (kept, never auto-pruned). All state is local (schema v35, `coverage_*`/`canonical_*` tables); nothing requires Jira admin.
- **Coverage module — versions & change requests (Topic 2).** Each functional requirement can now hold multiple **versions** (e.g. 2.40/stable, 3.0/beta) — coverage is measured per version, and a version can be **cloned** to start a new release line from an existing one. Customer requirements are **locked to a version**, and **change requests** track each customer's can-accept / cannot-accept / pending decision, with **version-distribution and CR-adoption dashboards**. All local (schema v36); no Jira admin needed.
- **Coverage module — Coverage Map view + project configuration.** A new **Coverage Map** sub-tab (reachable without selecting a function) graphically relates your in-scope projects to the canonical functional requirements: a **per-project coverage panel** (coverage %, requirements, functions reused) and a **Sankey** flowing *customer project → function → covered / gap*. The in-scope **project keys are now configuration** (a per-profile `source` / `customer` list, schema v37) that seeds itself from existing members and drives the view and the report. The coverage report gains **By-project** and **Reuse-map** sheets alongside Summary / per-group / Gaps. Coverage modals were restyled for a consistent centered/scrolling layout. All local; no Jira admin.
- **PKCS#11 demo dataset (`demo-pkcs` profile).** Create a profile with the Jira base URL **`demo-pkcs`** and a normal **Sync** now produces a complete PKCS#11 dataset across *every* view — **Browse** (tests like `C_Sign with RSA-2048`, foldered under Signing / Key management for **C_Sign, C_GenerateKeyPair, C_Verify**), **Requirements** (`FUNC-PKCS11-*` reused by `CUST-HSM-BANK/SAMSU-*`), **Preconditions** (HSM-themed), and **Containers** (PKCS Test Sets / Plans / Executions with run status). On top of that, the Coverage tab's **Load PKCS#11 coverage** action layers the parameter models, two versions (2.40 / 3.0), member version-locks, and change requests with per-customer decisions onto the synced tests — so a value in the Coverage matrix is the same test key you can open in Browse. The demo backend is theme-driven: `demo-pkcs` swaps the demo vocabulary to PKCS while the plain `demo` dataset is unchanged. The profile form and `isDemoURL` accept a `demo-` prefix (e.g. `demo-pkcs`).

**Preconditions, Dashboard, Test Calls, Containers, Traceability**
- Preconditions: read-only detail with explicit Edit / Save / Cancel, a new Condition definition field, collapsible Condition + Description, and a sort dropdown matching the filter box.
- Dashboard: a "By requirement" chart beside "By component".
- Test Calls: the pager moved to a fixed bottom footer.
- Containers: the Test-Execution filter is width-capped with ellipsis; the Test-Plan related-bugs list is collapsible so the test list stays visible.
- Traceability: the requirement Sankey gains an **Epic** layer (Requirement -> Epic -> Coverage -> ...), falling back to the current flow when a requirement has no epic.

### Changed
- **Gap Analysis file picker** now uses the app's standard button styling instead of the browser-native "Choose File" widget.
- **Profile switching is locked during a sync** — the top-bar profile selector is disabled while any sync (full or per-view) runs, so you can't switch projects mid-pull and race the in-flight writes.

### Fixed
- **Schema-collision fix.** The requirement / precondition / epic column ALTERs now run unconditionally and idempotently (not version-gated): reusing schema version numbers across two in-flight branches on a shared DB skipped the version-gated ALTERs and broke live sync ("table requirement has no column named priority"). A regression test reproduces the collision.

### Notes
- Live Jira paths that need a real Xray Server / DC 8.4.0 instance (requirement create / import / link push, sprint / precondition-condition / epic-link custom fields, coverage push) are marked `NOTE(xtm)` and are fully functional in demo mode.

## [1.7.1] - 2026-06-26

Patch release: a live-testing fix for Test Repository subfolder creation, plus a dedicated Sync button on the test-case view.

### Added
- **Dedicated Sync button on the Browse / test-case view** (RND_P_4TFINT_05-260). A tests-only partial sync refreshes just the test grid and folder membership, incremental against the last full sync's watermark, without running the requirement / container / bug passes. The global header Sync is unchanged.

### Fixed
- **Creating a Test Repository subfolder now commits to Jira** (RND_P_4TFINT_05-252). The folder-create call targeted `.../testrepository/{project}/folders/{parentID}/folders`, which a live Xray Server / DC instance rejects with a 404; it now posts to the correct parent folder resource `.../folders/{parentID}` (verified against the docs and the live error).

## [1.7.0] - 2026-06-26

Stable **1.7.0** release. Rolls up the `1.7.0a` through `1.7.0a-5` alpha iterations (full per-iteration history in the sections below) into one release: a dedicated **Bugs management view** (detail card, affected-test run-history breakdown with sub-task and cross-project executions, markdown detail fields, Excel export), **collapsible-outline and timestamped Excel exports** across the app, **collapsible Traceability export**, and **pill-based filters** with tighter Bugs / Preconditions / Containers layouts. Highlights of the final iteration on top of `1.7.0a-4`:

### Added
- **Select all bugs across pages.** The Bugs list has a second select-all control that checks every bug matching the current filter across all pages, not just the visible one — so the whole filtered set can be exported in one action.
- **Collapsible Traceability export.** The Traceability XLSX "Table" sheet is now a collapsible outline tree that stacks each thread by node (e.g. Test Plan > Test Execution > Test > Run status), de-duplicating shared parents and using Excel row grouping (+/− controls), matching the bug export. The Flow (Sankey edge list) sheet is unchanged.

### Changed
- **Styled bug Excel export.** The exported workbook is now banded by tier — bug, test, and execution rows each have their own fill — with cell borders and word-wrapped long-text columns (Details / Description / Defect Analysis / Correction Details) for readability.
- **Timestamped export filenames.** Every export's default filename is prefixed with a `YYYYMMDDHHMM_` timestamp (e.g. `202606261430_dashboard.xlsx`), so saved exports sort chronologically and a second export never silently overwrites an earlier one.
- **Pill-based filters.** The Bugs test-linkage filter (All / With tests / Without tests), the Preconditions usage filter (All / With tests / Without tests), and the Containers status filter (All statuses + one per status) are now labelled pills with live counts, matching the Requirements coverage filter, instead of dropdowns.
- **Tighter Bugs and Preconditions layout.** In the Bugs list the filter pills and sort control share one row, the Export button (now "Export…") sits beside Sync, and the pager places the page nav next to the rows-per-page selector. In the Preconditions list the filter pills moved to their own line below the sort / New row, so the New button no longer stretches.

### Fixed
- **Bug Excel export collapse controls.** The workbook now declares its outline depth, so Excel reliably renders the Bug > Test > Execution row-group collapse (+/−) controls; previously the grouping data was present but some Excel builds showed the report flat.
- Bug list (and the Containers Test Execution / Plan / Set member lists) now scroll with the pager pinned, instead of the list growing unbounded with a hidden scrollbar.
- Bug card status pills no longer overlap the card (wider list, wrapping layout).

## [1.7.0a-4] - 2026-06-25 (alpha)

Fourth alpha iteration of the 1.7.0 work: Bugs-view improvements, plus sub-task and cross-project execution sync fixes from live testing.

### Added
- **Bug detail panel.** Selecting a bug now shows a structured detail card above the affected tests: key, status, summary, a two-column facts grid (Type / Project / Priority / Updated / Reporter / Severity / Affects), and the bug **Description** (collapsible, collapsed by default), **Defect Origin**, **Defect Analysis**, and **Correction Details** (fetched lazily on selection).
- **Dynamic right sidebar.** The bug-view side panel is a placeholder for a test, a Test Plan, or a Test Execution: the affected-test open icon shows the test; the run-history **Plan** and **Execution** links open that container's detail (summary, run-status histogram, members) in the sidebar. The panel is width-resizable and all three share one persisted width.
- **Bug list test-linkage filter.** Filter bugs by All / With tests / Without tests, with a per-card cue (a test-count badge, or a "no tests" chip).
- **Sub-task executions in the run-history breakdown.** A test's run history and the bug breakdown now distinguish **Sub Test Executions** from standalone ones with a "Sub-task" badge and a link to the parent issue.
- **Cross-project execution discovery.** Executions that live in another project (notably sub-task executions, which inherit their parent issue's project) are now discovered per test and cached, both during a full sync and lazily when a test's run history is opened, so their runs appear in the breakdown.
- **Export bugs to Excel.** An "Export to Excel" action in the Bugs toolbar exports the checked bugs (or the open bug) to a single-sheet **collapsible outline** workbook that stacks **Bug > Test > Test Execution** using Excel row grouping (the +/- controls), so a bug collapses to hide its tests and a test collapses to hide its executions. Each level carries its own detail.
- **Markdown in detail views.** The bug **Description**, **Defect Analysis**, and **Correction Details**, and the container / Test Execution **Description** in the detail sidebar, now render as GitHub-flavoured markdown.

### Changed
- **Bug sync fetches ALL bugs in the project.** Previously only bugs linked to a synced test were found; the sync now also runs a project-wide `project = <bugProject> AND issuetype = <type>` search and merges it with the test-link harvest, so unlinked project bugs appear too.
- **Sub-task Test Execution issue type is discovered from the instance** (it defaults to "Sub Test Execution" but can be renamed / localised), instead of a hardcoded name that silently missed renamed types.
- **The Bugs-view sync also refreshes affected-test run data.** Clicking Sync in the Bugs view now refreshes the run/execution data for every bug-affected test (including newly discovered cross-project and sub-task executions), so the run-history breakdown updates without needing a full project sync.

### Fixed
- **"Latest result" now matches the run history.** The affected-tests "Latest result" reads the most recent run from the run table (the same source as the breakdown), falling back to the consolidated membership status, instead of a worst-wins value that could disagree with the runs shown.
- **Run-history breakdown columns.** The run-date column is renamed **Run Date** (to distinguish it from the execution timestamps) and **every column is now sortable**. The **Created / Updated / Resolved** columns show the Test Execution issue's own timestamps (created, updated, resolution date), fetched from the execution detail and cached on the container - the per-run Xray endpoint does not return them, so they were previously blank.
- **Defect Analysis is collapsible** in the bug detail card (collapsed by default), matching the Description field, so a long analysis no longer pushes the affected tests down.

## [1.7.0a-2] - 2026-06-25 (alpha)

Second alpha iteration of the 1.7.0 work: live-testing fixes and follow-on features on top of `1.7.0a`.

### Added
- **Import JUnit XML results.** Update a Test Execution's run results by importing a JUnit-compatible report, matching each `<testcase name>` to a test by summary. Import into the selected execution, or create a new Test Execution from the report (optionally creating tests it does not match). Queued as pending changes with a preview before commit.
- **Per-test Fix Version on executions.** Each member shows its own Jira Fix Version(s) in a column; the execution's Fix Version chips filter the member list.
- **Per-run timestamps.** Test runs carry created / updated timestamps; the run-history tables are sortable by result, created, or updated.
- **Run history nested by Test Plan.** A bug's per-test run breakdown groups runs under each Test Plan and adds Created / Updated columns.
- **Traceability detail.** The Requirement Sankey gains a Test column (requirement → coverage → plan → test → result); the Sub-task Sankey labels each parent with its summary.
- **Test connection while editing a profile** using the stored token.

### Changed
- **Live Xray run data (Server/DC).** Test runs are fetched from the `testexec/{key}/test` endpoint (paginated); the Test Plan association is read from the execution issue; the execution's environment is stamped onto its runs. Verified against a live instance.
- **Requirement test detail** opens as a right-side panel instead of a full-screen overlay.
- **Bugs panel.** Scrollable list with a select-all checkbox, working rows-per-page (default 5), full-colour run-status highlighting, and a two-row header so the Sync button is not clipped.
- **Clearer Jira errors.** A failed request surfaces the Jira error message instead of the raw query URL.

### Fixed
- Cross-project test detail no longer shows "test not found" — it falls back to a live fetch.
- Bulk-operation modal dropdowns are no longer clipped by the modal bounds.
- Pager Prev/Go/Next show a clear enabled vs disabled state in the Bugs view.
- Stability: a panic-recovery safety net so a backend error surfaces as a message instead of terminating the app.

## [1.7.0a] - 2026-06-23 (alpha)

Alpha build of the 1.7.0 work, for internal validation.

### Added
- **Per-view sessions.** Each view keeps its selection, filters, search,
  pagination, and sub-tab when you switch tabs and come back; state resets on
  app restart and profile change. (RND_P_4TFINT_05-238)
- **Test run information.** Xray test runs (result, date, who, environment, plan,
  fix versions, defects) are pulled during container sync and surfaced as a Run
  history section on a test, run columns on Test Executions, and a run roll-up on
  Test Plans / Sets.
- **Bug affected-tests breakdown.** The Bugs detail gains a Project column and an
  expandable per-test run breakdown (fix version / execution / plan / environment
  / date / tester / defects). (RND_P_4TFINT_05-240)
- **Read-only test detail.** A read-only mode for the test detail panel; from a
  bug's affected tests (and the Containers Test Execution list) a test opens
  read-only in a side panel beside the detail, and the test-key navigation now
  preserves the view's state. (RND_P_4TFINT_05-239, -245)
- **Add affected tests to an existing Test Execution** from the Bugs view.
  (RND_P_4TFINT_05-242)

### Notes
- Live Xray test-run shapes are marked `NOTE(xtm)` for verification against a real
  Xray Server / DC 8.4.0 instance; demo mode is fully populated.

## [1.6.0] - 2026-06-22

### Added
- **Cross-project Test Execution support.** Tests that live in another Jira
  project now appear on the execution board (cached locally), their linked bugs
  are harvested and shown, and an "include cross-project members" toggle controls
  whether they are drawn in the execution traceability flow. (RND_P_4TFINT_05-219)
- **Traceability XLSX export.** Export the active Traceability tab to an XLSX with
  a Sankey flow sheet and a flat table sheet. (RND_P_4TFINT_05-221)
- **Create a Test Execution from selected bugs**, to isolate a run that verifies
  only those bugs. (RND_P_4TFINT_05-222)
- **Execution Type field.** The Xray Test Type (Manual / Automated / Generic /
  Cucumber) on tests, with a Browse column, a filter, bulk edit and detail edit.
  (RND_P_4TFINT_05-230)
- **Test Environments on executions** (standalone and sub-task): chips, a filter,
  and single and bulk edit. (RND_P_4TFINT_05-229)
- **Fix Version(s) on executions.** Read-only display of the Jira Fix Version(s)
  field on Test Executions. (RND_P_4TFINT_05-234)
- **Dashboard filters and export.** Folder / component / status filters, and an
  Export to XLSX (summary plus per-breakdown sheets). (RND_P_4TFINT_05-228,
  RND_P_4TFINT_05-235)
- **Duplicates: compare summaries** side by side, plus a **scan-steps progress**
  bar. (RND_P_4TFINT_05-225, RND_P_4TFINT_05-226)
- **Multi-pick add** of requirements and preconditions on a test, and **swap
  (replace)** preconditions and requirements in one step, single and bulk.
  (RND_P_4TFINT_05-224, RND_P_4TFINT_05-231)
- **Add tests to a requirement** directly from the Requirements view.
  (RND_P_4TFINT_05-233)
- **Test Case Gap Analysis.** Compare a project or an imported CSV / XLSX file
  against a project, with a dashboard overview, three-way (complete-project) and
  folder comparison, create-tests-from-gaps, and a formatted Excel report.
  (RND_P_4TFINT_05-232)
- **Live Jira / Xray REST wiring (Phase 7).** Every call in the Jira client now
  has a real implementation behind the demo short-circuit (no demo-only stubs
  remain): bug list and defect create / link, cross-project member fetch, Test
  Type, Test Environments, requirement coverage links, generic custom fields,
  Test Repository folder create / rename / delete, and issue comments. Instance-
  specific shapes are flagged for verification against a live instance.
  (RND_P_4TFINT_05-236)
- **GNU GPL-3.0 license.**

### Changed
- **Test Calls** view shows per-caller sync progress and a toolbar aligned with
  the other views. (RND_P_4TFINT_05-227)
- **Dashboard** groups the Export / Refresh buttons and tiles the summary,
  coverage and trend panels into a compact layout. (RND_P_4TFINT_05-235)

### Fixed
- The requirement / precondition picker dropdown in the test detail was truncated
  by the panel's left border. (RND_P_4TFINT_05-223)
- The Bugs search returned an empty list on a live Jira, because the live bug
  list was not yet wired. (RND_P_4TFINT_05-220)

## [1.5.0] - 2026-06-19

### Added
- **Sub-task Test Executions.** Xray sub-task Test Executions (a Test Execution
  that is a Jira sub-task of a parent issue) now sync from Jira and sit alongside
  standalone executions: filter by Standalone or Sub-task, see the parent issue
  on the container card (it opens in the browser), and pick from a type-to-filter
  searchable dropdown that tints sub-task entries. (RND_P_4TFINT_05-216)
- **Jira color in text.** Description and step text containing Jira color macros
  (`{color:#hex}…{color}`) now renders in the proper color in the read view;
  editing still shows the raw macro so it round-trips back to Jira unchanged.
  (RND_P_4TFINT_05-217)
- **Defect tracking.** Raise a bug straight from a failed test in a Test
  Execution, browse every bug linked to the profile's tests in a new Bugs panel,
  and see a test's linked bugs (including ones in other projects) as clickable
  links on its detail panel. The defect project and issue type are configurable
  per profile. (RND_P_4TFINT_05-212)
- **Sort and filter controls.** The Requirements, Preconditions, and Bugs panels
  gain a sort control (pick a field, toggle ascending or descending). The Test
  Set / Plan / Execution picker can be filtered by keyword and status, and the
  execution member table sorts by column. (RND_P_4TFINT_05-213)
- **Cross-project traceability.** On the dashboard, the Execution filter narrows
  to the executions of the selected Test Plans, cross-project executions are
  surfaced by the cross-project filter, and a cross-project bugs list links
  defects filed in other projects to this project's tests. (RND_P_4TFINT_05-215)
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
- Bug sync now respects the profile's sync scope, pulling defects only for the
  in-scope tests instead of for every synced test. (RND_P_4TFINT_05-214)
- Per-view syncs (the Containers, Requirements, and Bugs tabs) now show their
  progress in the status bar with a stage label and item count, instead of
  appearing to do nothing while they run. (RND_P_4TFINT_05-218)

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

[1.9.0]: https://github.com/veenone/xray-testcase-manager/releases/tag/v1.9.0
[1.8.0]: https://github.com/veenone/xray-testcase-manager/releases/tag/v1.8.0
[1.7.1]: https://github.com/veenone/xray-testcase-manager/releases/tag/v1.7.1
[1.7.0]: https://github.com/veenone/xray-testcase-manager/releases/tag/v1.7.0
[1.6.0]: https://github.com/veenone/xray-testcase-manager/releases/tag/v1.6.0
[1.5.0]: https://github.com/veenone/xray-testcase-manager/releases/tag/v1.5.0
[1.4.0]: https://github.com/veenone/xray-testcase-manager/releases/tag/v1.4.0
[1.3.0]: https://github.com/veenone/xray-testcase-manager/releases/tag/v1.3.0
[1.2.1]: https://github.com/veenone/xray-testcase-manager/releases/tag/v1.2.1
[1.2.0]: https://github.com/veenone/xray-testcase-manager/releases/tag/v1.2.0
[1.1.1]: https://github.com/veenone/xray-testcase-manager/releases/tag/v1.1.1
[1.1.0]: https://github.com/veenone/xray-testcase-manager/releases/tag/v1.1.0
[1.0.2]: https://github.com/veenone/xray-testcase-manager/releases/tag/v1.0.2
