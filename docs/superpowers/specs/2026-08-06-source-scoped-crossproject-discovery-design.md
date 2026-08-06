# Source-scoped cross-project execution discovery — Design

**Date:** 2026-08-06
**Status:** Approved (pre-implementation)
**Builds on:** the P0 container-sync concurrency work (perf/container-sync-concurrency, PR #82).

## Problem

`Engine.discoverCrossProjectExecs` (the last pass of container sync) iterates **every test key in the profile** and calls `TestExecutionsForTest(testKey)` (a global `/rest/raven/2.0/api/test/{key}/testexec` lookup) to find Test Executions in other projects that include that test. That is O(all tests) API calls on every sync — the code's own comment noted ~750s of pacing on a 5000-test project. The P0 work added concurrency + a rate limiter, which helps the pacing but not the O(N) request count.

The profile already stores a list of **cross-project source projects** (`profiles.cross_project_sources`, schema v47), configured via `SetProfileCrossProjectSources` and used today only by the manual cross-project link pickers. The discovery pass ignores them.

## Goal

Bound discovery to O(executions in the configured source projects) instead of O(all tests): search Test Executions in the profile's configured source projects and keep only the ones that include this profile's tests. If no sources are configured, skip discovery entirely.

## Approved decisions (from brainstorming)

- **Strategy: bulk search in configured source projects** (chosen over incremental per-test watermark or on-demand-only).
- **No sources configured → skip auto-discovery.** Accepted behavior change: a profile with no configured sources no longer auto-discovers foreign executions (it used to scan every test globally). The manual link pickers, which already use the configured sources, are unaffected. The old global `/testexec` scan was marked "pending live verification" anyway.
- Discovery keeps running on every container sync (the bulk pass is cheap), not gated to full syncs only.

## Non-goals

- Changing `SyncBugRunData` (a separate, already-bounded per-bug-affected-test refresh that also uses `TestExecutionsForTest`). It stays.
- Removing `TestExecutionsForTest` from the backend (still used by `SyncBugRunData` + adapters).
- A new incremental watermark or a bulk JQL that infers membership without fetching it.

## Architecture

app.go resolves the profile's configured source projects and passes them into the sync. The engine, for each source project, lists that project's Test Executions + memberships (reusing the existing `ListContainers` path — no new backend interface method, so the Kiwi/Xray adapters are untouched), keeps executions with at least one member among the profile's own tests, dedupes against already-known executions, writes the new containers + links, and fetches + stores their runs.

```
app.go: scopeCrossProjectSources(p.CrossProjectSources, p.ProjectKey)
   │  crossProjectSources []string
   ▼
Engine.Sync / SyncContainers (new param) ──► syncContainers ──► discoverCrossProjectExecs(sources)
                                                                     │  (skip if len(sources)==0)
                                for each source project:  e.backend.ListContainers(source) ──► execs + members
                                keep execs whose members ∩ profile tests ≠ ∅  ─► dedupe vs known
                                ─► UpsertContainers + UpsertContainerLinks (batch, serial)
                                ─► concurrent GetTestRuns ─► ReplaceRunsForExec (serial)
```

Cost: O(executions in the source projects) API calls, versus O(all tests) before.

## Components

1. **Plumb the sources in.**
   - `Engine.Sync` and `Engine.SyncContainers` gain a `crossProjectSources []string` parameter (threaded to `syncContainers` → `discoverCrossProjectExecs`).
   - `app.go` fills it via the existing `scopeCrossProjectSources(p.CrossProjectSources, p.ProjectKey)` at both call sites (`SyncProfile`/full sync and the standalone `SyncContainers` binding).

2. **Rewrite `discoverCrossProjectExecs(ctx, profileID, crossProjectSources, knownExecKeys, onProgress)`.**
   - If `len(crossProjectSources) == 0`, return immediately.
   - Load the profile's own test keys into a set (for membership matching).
   - For each source project, call `e.backend.ListContainers(sourceProject, progressCb)` (reused). From the result keep only `Kind == testexec` containers whose membership links include a key in the profile's test set; keep only those links.
   - Dedupe discovered executions by key across sources and against `knownExecKeys`.
   - Batch write: `UpsertContainers(newContainers)` then `UpsertContainerLinks(newLinks)`.
   - Reuse the P0 concurrent-fetch/serial-write pattern to fetch runs for the new executions and `ReplaceRunsForExec`.
   - Best-effort: per-source and per-exec errors are logged and skipped; ctx cancellation checked between sources.

3. **Demo alignment.** In demo mode, `ListContainers(sourceProject)` currently generates containers whose members belong to `sourceProject`, so the membership filter finds nothing. Add a demo path so that, for a source project, one demo Test Execution references the *main* profile project's tests (mirroring the existing `demoTestExecutionsForTest` cross-project scenario), and configure a source project on the demo profile used by the tests. The exact demo mechanism is a plan detail.

## Behavior change

Profiles with no configured cross-project sources no longer auto-discover foreign executions. This is the intended, approved trade-off (faster, more intentional; the manual link pickers still cover the configured sources).

## Testing

- **Engine test (mock backend):** source projects whose executions include some profile tests and some not → assert only executions with matching members (and only their matching links) are stored; executions with no matching members are ignored; already-known executions are not duplicated.
- **No-sources test:** with an empty source list, discovery writes nothing and makes no backend calls.
- **Full-sync snapshot:** update `TestFullSyncStoreSnapshot` for the demo source configuration so the cross-project execution/link counts stay pinned (or assert the skip when the demo profile has no sources — decided in the plan).
- All existing sync tests stay green; concurrent code passes `-race`.

## Risks / notes

- **Reusing `ListContainers(source)`** also fetches the source project's Test Set / Plan memberships (not just executions). Source projects are few, and the calls are limiter-paced, so this is acceptable for v1; a lean exec-only backend method is a possible future optimization (would touch the Backend interface + adapters).
- **Live verification:** the source-project execution search reuses the already-used `searchContainers` / `listContainerTests` endpoints (unlike the old per-test `/testexec` path), so it relies only on endpoints the main container sync already exercises.
