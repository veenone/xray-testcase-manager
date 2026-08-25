# Frontend server-data layer — design spec

**Status:** proposed · **Date:** 2026-08-25 · **Findings:** A3, A5 (audit "Next" lane)
**Follow-up spec (out of scope here):** `App.tsx` decomposition into domain contexts (A1/A2/A4/A8)

## 1. Problem

The frontend has no server-state cache. Two mechanisms stand in for one:

- **A single global `refreshKey` counter** (`App.tsx`, ~20 call sites) plus a second
  `detailVersion` counter is threaded into every view as a `useEffect` dependency.
  Any mutation bumps it, forcing **every mounted view to refetch** from the Go
  backend — regardless of what actually changed. At 10k–50k tests a single field
  edit re-runs full `ListTests` / `ScanDuplicates` / `GetStatistics` queries in
  unrelated views. There is no dedup, no per-entity invalidation, no
  stale-while-revalidate. (Audit **A3**.)
- **`api.ts` re-exports ~240 generated Wails bindings with no behavior added.**
  All 284 call sites independently `try/catch`, call `errMsg`, and each decides
  whether to `console.error` (silent) or `notice()` (visible). The result is
  inconsistent error UX by construction — e.g. `loadProfileData` and
  `reloadPending` swallow failures to the console, so a failed load shows the
  user nothing. (Audit **A5**.)

The one place the app already does this right is `features.ts`
(`capabilitiesCache` + `useCapabilities` + `invalidateCapabilities`) — a cached,
invalidatable, subscription-based hook. This spec generalizes that idea with a
proven library rather than hand-rolling it everywhere.

## 2. Goals / non-goals

**Goals**
- Introduce a server-cache keyed per entity so a mutation invalidates only what
  changed.
- A thin, typed API client that normalizes errors once and gives every read a
  uniform `{ data, loading, error }` shape.
- Retire `refreshKey`, `detailVersion`, and `reloadPending`.
- Migrate incrementally, keeping the app working at every step (no test runner).

**Non-goals (deferred to the next spec)**
- Splitting `App.tsx` into domain contexts (A1/A2/A4/A8). This spec *reduces*
  App state as a side effect, which shrinks that later work.
- Changing the journaled-mutation model (local `pending_change` → commit). Reads
  and cache invalidation change; the write model does not.

## 3. Decision record

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Cache library | **TanStack Query** (`@tanstack/react-query`) | Caching, dedup, per-entity invalidation, loading/error for free; same ecosystem as `@tanstack/react-virtual` already added in #98. |
| Tuning | **Local-IPC, not network** | `retry: 0`, `refetchOnWindowFocus: false`; rely on **explicit invalidation** (Jira is synced on demand, backend is local + fast). |
| Migration | **Strangler, phased** | New layer coexists with `refreshKey`; migrate one entity/view at a time; retire the counter last. |
| Error model | **Typed `ApiError` + convention** | User actions → `notice()`; background loads → query `error` rendered inline; never `console.error`-only. |

## 4. New modules

```
frontend/src/
  lib/
    apiError.ts      # ApiError type + normalizeError(e): ApiError (wraps errMsg once)
    queryClient.ts   # configured QueryClient (retry:0, refetchOnWindowFocus:false, staleTime)
  queries/
    keys.ts          # query-key factory (single source of truth for keys)
    tests.ts         # useTests(profileId, params), useTestDetail(profileId, key)
    pending.ts       # usePendingChanges(profileId) + commit/discard mutations
    folders.ts       # useFolders(profileId)
    containers.ts    # useContainers(...)
    …                # one module per entity, added as each view migrates
```

`main.tsx` wraps `<App/>` in `<QueryClientProvider client={queryClient}>`.

### 4.1 Query-key convention

`keys.ts` is the only place keys are constructed, so invalidation targets can
never drift from reads:

```ts
export const keys = {
  tests:   (p: string, params: TestQuery) => [p, "tests", params] as const,
  test:    (p: string, k: string)         => [p, "test", k] as const,
  pending: (p: string)                    => [p, "pending"] as const,
  folders: (p: string)                    => [p, "folders"] as const,
  // …
};
```

Every key is prefixed with `profileId`, so switching profiles naturally scopes
the cache (and `queryClient.removeQueries` on profile switch replaces the manual
`clearViewState` bookkeeping).

### 4.2 Read hook shape

```ts
export function useTests(profileId: string, params: TestQuery) {
  return useQuery({
    queryKey: keys.tests(profileId, params),
    queryFn: () => client.listTests(profileId, params), // client wraps the binding + normalizeError
    enabled: !!profileId,
  });
}
```

### 4.3 Mutation shape

Mutations wrap the existing journaling App methods and invalidate exactly the
affected entities:

```ts
export function useCommitChanges(profileId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (ids?: string[]) => client.commit(profileId, ids),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: keys.pending(profileId) });
      qc.invalidateQueries({ queryKey: [profileId, "tests"] }); // prefix match
    },
  });
}
```

### 4.4 Error convention (A5)

- `client.*` throws `ApiError` (from `normalizeError`).
- **User-initiated** action failures → `notice({ tone: "error", … })` (unchanged UX, now uniform).
- **Background loads** → surfaced via the query's `error`, rendered inline in the view. **No `console.error`-only path** on primary data.

## 5. Migration plan (strangler; each phase ends green)

Every phase must end with `tsc` clean + `vite build` green, and — because there
is no automated UI test — a `wails dev` + demo-profile smoke-test of the
touched views.

- **Phase 0 — infra, zero behavior change.** Add the dependency, `queryClient`,
  `QueryClientProvider`, `apiError.ts`, `keys.ts`, and the `client` wrapper.
  Nothing consumes them yet.
- **Phase 0.5 — test runner (recommended, optional).** Add **Vitest + React
  Testing Library**. This is the single highest-leverage de-risker: it lets us
  unit-test the query/mutation hooks and each migration step, and it pays off
  again in the context-split spec. *Flagged for explicit decision at review — if
  cut, later phases rely on build + manual smoke only.*
- **Phase 1 — pilot: `pendingChanges`.** App-owned, drives the commit flow.
  Migrating it retires `reloadPending`. **Bridge:** during migration, mutations
  both `invalidateQueries` *and* still bump `refreshKey`, so un-migrated views
  keep working. Validates the whole pattern end to end on one entity.
- **Phase 2 — tests list + detail.** `TestTable` → `useTests`; `TestDetail` →
  `useTestDetail` (also collapses its fetch waterfall, partially addressing
  A6/P7).
- **Phase 3 — remaining views.** containers, preconditions, requirements,
  coverage, duplicates, misspellings, dashboard — one at a time.
- **Phase 4 — cut over.** All mutations invalidate per-entity; delete
  `refreshKey`, `detailVersion`, `reloadPending`, and the bridge code.

Each phase is a reviewable PR.

## 6. Verification

- Per phase: `cd frontend && npm run build` (tsc + vite) green.
- Per phase: manual smoke via `wails dev` with a `demo` profile — the touched
  view loads, a mutation updates only the expected views, errors surface in the
  UI (not the console).
- If Phase 0.5 is accepted: unit tests for each new hook (loading/success/error
  + that a mutation invalidates the intended keys) via a mock `client`.

## 7. Risks & mitigations

| Risk | Mitigation |
|------|-----------|
| No automated UI safety net | Incremental phases; keep the old `refreshKey` path until each view is migrated; per-phase build + smoke; Phase 0.5 test runner. |
| Demo mode must keep working | The `client` wraps the same bindings that already short-circuit to the demo generator; no change to that path. |
| Wrong `staleTime` → stale UI or over-fetch | Start conservative (short `staleTime`, explicit invalidation on mutation); tune per entity. |
| Invalidation misses an entity | `keys.ts` is the single source of keys; mutations reference the same factory; reviewed per phase. |
| Bridge complexity during migration | Bridge is deliberately dumb (invalidate + bump both) and deleted wholesale in Phase 4. |

## 8. Interaction with the next spec

Retiring `refreshKey`/`detailVersion`/`reloadPending` and moving loading/error
into query hooks removes a large slice of `App.tsx` state and prop-drilling.
The follow-up `App.tsx`-decomposition spec (A1/A2/A4/A8) then only has to model
genuinely client-side state — profile selection, sync/commit state machine,
bulk selection, modal visibility — a materially smaller change.
