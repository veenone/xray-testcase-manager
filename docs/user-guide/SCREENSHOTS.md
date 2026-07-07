# Screenshot capture guide

**The figures in `images/` are already captured** from a live demo profile
(`wails dev` + the headless `browse` tool). This file documents how they were
made and how to refresh or re-shoot any of them — the filenames are the contract
with [`USER_GUIDE.md`](USER_GUIDE.md), so keep them identical and a replacement
renders automatically.

Figure 29 (commit conflict) is intentionally omitted — a conflict can't be
staged on demo data, so the guide explains it in text instead.

**Refresh for 1.5.0.** Re-shoot these because their UI changed: **06**
(review filter removed), **08** (review section removed), **14** (the bulk
**Review…** action removed), **22** (container picker is now a searchable
dropdown with Standalone / Sub-task filter), **25** (the Dashboard no longer
hosts the Sankeys — they moved to the new **Traceability** tab), and **26/27**
(now on the Traceability tab, not the Dashboard). New shots to add: **37**
(Bugs panel) and **38** (Sub-task traceability Sankey).

**Refresh for 1.6.0.** Re-shoot these because their UI changed: **06** (Browse
now has an **Execution Type** column and filter), **18** (the Requirements detail
gains **+ Add tests**), **20/21** (Duplicates adds **Compare summaries**), **25**
(the Dashboard gains folder / component / status filters and **Export XLSX**),
and **27** (the Traceability toolbar gains **Export XLSX** and the **include
cross-project members** toggle). New shots to add: **39** (Gap Analysis setup),
**40** (Gap Analysis results), and **41** (Test Environments and Fix Version(s)
on a Test Execution).

**New for 1.7.0a.** Add: **42** (Run history on a test), **43** (execution run
columns: date / tester / environment), **44** (Plan / Set run roll-up), **45**
(bug affected-tests breakdown), **46** (read-only test detail in the Bugs view),
**47** (add tests to an existing execution), **48** (PAT show/hide toggle), and
**49** (TLS / certificate settings).

## How to capture

1. **Run the app on a demo profile** so every screen has rich data and no Jira is
   needed: create a profile with **Jira URL `demo`** (any project key + token),
   then **Sync** once. A yellow `DEMO` chip confirms you are in demo mode.
2. Use Windows **Snipping Tool** (`Win+Shift+S`) to capture. Prefer **window**
   or a tight **rectangular** crop over full-screen.
3. Save as **PNG** into `docs/user-guide/images/` with the filename in the table.
4. Recommended width **1280–1600 px**; enable a light theme unless the figure is
   specifically the dark-theme shot (Fig 33).
5. Avoid real secrets — demo data is safe; if you capture a live profile, blur
   the URL/keys.

## Conventions

- Keep the same theme across figures (Light) except Fig 33.
- For "menu open" shots, click the menu so the dropdown is visible in the frame.
- For annotated figures (Fig 3, Fig 4) you may add numbered callouts in an image
  editor after capturing; the captions already describe the regions.

## Shot list

| # | File | View / how to get there | State to set up |
| --- | --- | --- | --- |
| 1 | `01-first-launch.png` | First launch with **no profiles** (or delete the DB to reset) | The connect/onboarding screen with the empty profile form |
| 2 | `02-profile-form.png` | Connect screen or **Profile → New profile…** | Form filled with a demo profile (Name, URL `demo`, project key, token) |
| 3 | `03-main-window.png` | **Browse** tab after sync | Full window; a folder selected, grid populated. (Optionally annotate the 4 regions.) |
| 4 | `04-topbar.png` | Any view | Tight crop of the **top bar** only |
| 5 | `05-browse-sidebar.png` | Browse | Left sidebar with **Group by: Folder** and the folder tree; hover a folder to show its row actions |
| 6 | `06-test-grid.png` | Browse | The grid + toolbar (search, status filter, Saved views, Export, Columns) |
| 7 | `07-columns-panel.png` | Browse → **Columns** | The Columns panel open, showing toggles and reorder controls |
| 8 | `08-test-detail.png` | Browse → click a test | Detail panel open on the **fields** section |
| 9 | `09-test-steps.png` | Test detail → steps | The step table with a few steps visible |
| 10 | `10-custom-fields.png` | Test detail → custom fields | Custom-field values visible |
| 11 | `11-detail-links.png` | Test detail | The **preconditions** and **requirement coverage** sections of a test |
| 12 | `12-new-test.png` | Browse → **＋ New Test** | The New Test panel, partially filled |
| 13 | `13-import-tests.png` | **More → Import tests…** | The import modal (column-mapping step if possible) |
| 14 | `14-bulk-toolbar.png` | Browse → tick 2–3 rows | The bulk toolbar visible above the grid |
| 15 | `15-bulk-edit.png` | Bulk toolbar → **Bulk edit…** | The Bulk edit dialog open |
| 16 | `16-preconditions.png` | **Preconditions** tab | Card list (left) + a selected precondition's detail (right) |
| 17 | `17-precondition-create.png` | Preconditions → **+ New** | The New precondition dialog |
| 18 | `18-requirements.png` | **Requirements** tab | Card list with coverage pills + a selected requirement's covering-tests detail |
| 19 | `19-requirement-sources.png` | Requirements → **Sources…** | The Requirement Sources dialog (with the add-source fields) |
| 20 | `20-duplicates.png` | **Duplicates** tab | A duplicate group selected, list + detail visible |
| 21 | `21-duplicates-compare.png` | Duplicates → **Compare steps** | The side-by-side step comparison modal |
| 22 | `22-containers.png` | **Containers** tab | Kind = Test Plan, a plan selected, members listed |
| 23 | `23-execution-runstatus.png` | Containers → kind **Test Execution** | A run-result dropdown open (or the bulk "set result" bar) |
| 24 | `24-pytest.png` | Containers → Actions → **Generate pytest…** | The menu open, or the generated file/path confirmation |
| 25 | `25-dashboard.png` | **Dashboard** tab | Summary cards + breakdowns near the top |
| 26 | `26-requirement-sankey.png` | **Traceability** tab → **Requirement** | The 4-column Sankey with the requirement filter visible |
| 27 | `27-execution-sankey.png` | **Traceability** tab → **Execution** | The Plan → Execution → status Sankey |
| 28 | `28-pending-changes.png` | Make an edit, then click the **N pending** badge | The Pending Changes dialog listing queued edits |
| 30 | `30-sync-progress.png` | Click **Sync** | Capture mid-sync so the status bar shows a phase + counts |
| 31 | `31-sync-history.png` | **More → Sync history** | The sync history list |
| 32 | `32-profile-menu.png` | Click **Profile** (top-left) | The Profile menu dropdown open |
| 33 | `33-dark-theme.png` | **More → Theme: Dark**, any view | Any representative view in dark theme |
| 34 | `34-diagnostics.png` | **More → Diagnostics** | The Diagnostics dialog |
| 35 | `35-about.png` | **About** (native menu / More) | The About dialog showing the version |
| 36 | `36-clone-steps.png` | Test detail → Steps → **Clone from…** | The clone-steps picker with a source test selected |
| 37 | `37-bugs-panel.png` | **Containers** tab → **Bugs** toggle | The Bugs master list + a selected bug's detail and the tests it affects |
| 38 | `38-subtask-sankey.png` | **Traceability** tab → **Sub-task** | The Parent → Execution → run-result Sankey with the Parent filter |
| 39 | `39-gap-analysis.png` | **Gap Analysis** tab | The setup form: Reference (project / file), Target file, Compare by, Three-way, and Download template |
| 40 | `40-gap-analysis-result.png` | Gap Analysis → fill a target → **Run** | The result: missing-from-reference and missing-from-target lists with select-and-**Add tests** and **Export report** |
| 41 | `41-execution-environments.png` | **Containers** → kind **Test Execution**, select an execution | The execution detail showing editable **Test Environments** chips and read-only **Fix Version(s)** |
| 42 | `42-run-history.png` | Browse → open a test → **Run history** section | The Run history table: execution, result, date, tester, environment, plan, fix versions, defects |
| 43 | `43-execution-runs.png` | **Containers** → kind **Test Execution**, select one | Member table showing the run **Date / By / Environment** columns beside the editable result |
| 44 | `44-plan-rollup.png` | **Containers** → kind **Test Plan**, select a plan | The run roll-up bar above the board (passed / failed / not-run / executing / aborted / blocked) |
| 45 | `45-bug-breakdown.png` | **Containers → Bugs**, select a bug, expand an affected test | The affected-tests table with the **Project** column and an expanded per-test run breakdown |
| 46 | `46-bug-test-detail.png` | Bugs → affected test → open-detail (↗) icon | The read-only test detail open as a side panel to the right of the bug detail |
| 47 | `47-add-to-execution.png` | Bugs → check one or more bugs → **Add to execution…** | The picker modal listing existing Test Executions with a search box |
| 48 | `48-pat-toggle.png` | **Profile → New profile…** (or Edit) | The Personal Access Token field with its show/hide (eye) toggle |
| 49 | `49-tls-settings.png` | Profile form → expand **Advanced: TLS / certificate settings** | The CA certificate (PEM) textarea and the allow-untrusted checkbox |
| 50 | `50-junit-new-exec.png` | **Containers** → kind **Test Execution** → **New exec from JUnit XML** | The dialog: execution summary, **Create missing tests** checkbox, and the file picker |
| 52 | `52-exec-fixversion-filter.png` | **Containers** → kind **Test Execution**, select one | The member table with the per-test **Fix Version** column and full-colour run-status results |
| 53 | `53-requirement-test-detail.png` | **Requirements** → pick a requirement with covering tests → click a covering test | The covering test's read-only detail open as a right-side panel |
| 54 | `54-profile-locked-sync.png` | Any view → click **Sync**, capture mid-sync | Tight crop of the **top bar** with the profile selector greyed out and the Sync button reading "Syncing…" |

**Refresh for 1.7.0a (post-alpha).** Re-shoot these because their UI changed:
**26** (Requirement Sankey now has the **Test** column — 5 layers), **38**
(Sub-task Sankey parents show **key — summary**), and **45** (the bug affected-test
breakdown is now **grouped by Test Plan** with **Created / Updated** columns).
Figure 51 (the JUnit import **preview** into an existing execution) is omitted —
it needs a real JUnit file through a native file dialog, so the guide describes it
in text instead.

To refresh a shot: run `wails dev`, open `http://localhost:34115/` in the
`browse` tool, switch to the **Demo Project (DEMO)** profile, sync once, then
navigate to the relevant view and `browse screenshot <path>`.
