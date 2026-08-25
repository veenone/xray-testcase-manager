# App.tsx decomposition into domain contexts — design spec

**Status:** proposed · **Date:** 2026-08-26 · **Findings:** A1, A2, A4, A8 (audit "Next" lane)
**Depends on:** the frontend server-data layer (spec `2026-08-25-frontend-data-layer-design.md`, Phases 2–4) having landed first.

## 1. Problem

`App.tsx` is a ~1,888-line god component holding **47 `useState` hooks** plus refs, spanning unrelated domains — profiles, sync lifecycle, bulk selection, ~15 modal-visibility booleans, view routing, folder/container/component selection. Everything re-renders the whole tree on any change, every new view re-declares the same 4–5 props, and cross-cutting invariants (no sync during commit, etc.) are enforced by hand in five separate handlers. (Audit **A1**, **A2**, **A4**, **A8**.)

## 2. Why this spec is sequenced after the data layer

The data-layer work (Phases 2–4) removes `refreshKey`, `detailVersion`, `reloadPending`, and moves server data (`pendingChanges`, tests, folders, …) into TanStack Query hooks. That deletes a large slice of `App.tsx` state before this spec touches it — turning "decompose 47 useState" into "extract a handful of genuinely client-side domains." Designing now is fine; **implementation must wait** for the data layer to merge, so the two efforts don't fight over `App.tsx`.

## 3. Goals / non-goals

**Goals**
- Split `App.tsx`'s remaining client state into small, focused domain contexts.
- Model the sync/commit mutual-exclusion as one reducer state machine (A8).
- Provide `notice`/`confirm`/`prompt` once via a root `DialogProvider` (A2), removing the duplicate instances in `App.tsx` and `TestDetail.tsx`.
- Replace the copy-pasted `onChanged` refresh ritual with one `afterMutation()` hook (A4).

**Non-goals**
- No behavior or visual change — this is a structural refactor.
- No changes to component internals beyond swapping prop reads for context reads.
- Not the data layer (its own spec) and not component-level performance work (P-series).

## 4. Decision record

| Decision | Choice | Rationale |
|----------|--------|-----------|
| State mechanism | **React Context + `useReducer`** | Dependency-minimal (matches the app's ethos); server state already lives in TanStack Query, so remaining client state is small. |
| Re-render control | **Several small, focused contexts** | A change in one domain doesn't re-render consumers of another. Split value vs dispatch where a context's value changes frequently. |
| Modal visibility | **One `useModal` reducer** | Only one modal is open at a time, so `openModal: ModalId | null` + a param payload replaces ~15 booleans. |
| Migration | **Incremental, one context per PR, each unit-tested** | Vitest exists now (data-layer Phase 0.5); `App.tsx` shrinks and stays working at every step. |

## 5. Target contexts

Each context lives in `frontend/src/contexts/<Name>Context.tsx`, exposes a typed hook (`useProfile()`, …), and throws if used outside its provider.

### 5.1 ProfileContext (A2)
- State: `profiles`, `activeId`, `defaultProfileId`, `theme`, `showCoverage`, and the profile's capabilities.
- Actions: `setActiveProfile(id)`, `setDefault(id)`, `setTheme(t)`, `reloadProfiles()`.
- Removes the `profileId={activeId}` prop threaded into nearly every view — the single most-drilled value in the app.

### 5.2 SyncContext — the state machine (A8)
- A `useReducer` over `{ status: 'idle' | 'syncing' | 'committing', syncState, progress, syncError, lastCommitResult }`.
- Selectors: `canSync`, `canCommit`, `canSwitchProfile` — one source of truth replacing the five hand-rolled guards split across a `syncing` boolean and a `syncRunningRef`.
- Actions: `doSync()`, `runFullSync()`, `syncTests()`, `commit(ids?)` — each dispatches transitions and is a no-op if its `can*` selector is false.

### 5.3 SelectionContext (A1)
- State: `selectedSet` (bulk multi-select) and `selectedKey` (the open detail row).
- Actions: `toggle(key)`, `togglePage(keys)`, `selectAllMatching(keys)`, `clear()`, `open(key)`.
- Its own context because selection changes are high-frequency; keeping it separate stops a selection toggle from re-rendering profile/sync consumers.

### 5.4 NavContext
- State: `view`, `groupBy`, `groupContainers`, `selectedContainer`, `components`, `selectedComponent`, `selectedFolder`.
- Actions: `setView`, `setGroupBy`, and the container/component/folder selectors.

### 5.5 UI / modals
- One `useModal` reducer: `openModal: ModalId | null` (a string union of the modal ids) + a small `modalParams` payload for the two modals that need one (`editingProfile`, `newTestFolder`).
- Hook: `useModal()` → `{ open(id, params?), close(), current, params }`.

## 6. Cross-cutting providers & hooks

### 6.1 DialogProvider (A2)
A single root provider exposes `notice`/`confirm`/`prompt` via `useNotice()`/`useConfirm()`/`usePrompt()` to any depth, replacing the separate instances currently created in both `App.tsx` and `TestDetail.tsx` (which today render duplicate dialog roots).

### 6.2 afterMutation() hook (A4)
Replaces the copy-pasted `setRefreshKey+1 / reloadPending() / clear selection / close modal` ritual (~15 call sites). Post-data-layer it is: **invalidate the affected queries + clear selection (optional) + close the active modal**. One reference passed to every modal's `onComplete`.

## 7. Provider composition

`main.tsx` (or an `AppProviders` wrapper) nests, outermost to innermost:
```
QueryClientProvider            (already present from the data layer)
  DialogProvider
    ProfileContext.Provider
      SyncContext.Provider     (reads activeId; gates on canSwitchProfile)
        NavContext.Provider
          SelectionContext.Provider
            ModalProvider
              <AppShell/>       (was App.tsx — now thin layout + routing)
```

## 8. Migration plan (incremental; each step its own PR, each green)

Order chosen so the biggest prop-drilling win comes first and the highest-risk piece (the state machine) is isolated:

1. **ProfileContext** — extract `profiles`/`activeId`/etc.; replace `profileId` props with `useProfile()` in consumers. Biggest drilling reduction.
2. **DialogProvider** — hoist notice/confirm/prompt to root; delete the duplicate in `TestDetail.tsx`.
3. **SyncContext (state machine)** — model `idle|syncing|committing`; replace the five scattered guards with `can*` selectors. Unit-test the reducer thoroughly.
4. **SelectionContext** — extract bulk + open-row selection.
5. **NavContext + ModalProvider + afterMutation()** — collapse routing and the ~15 modal booleans; wire `afterMutation()` through the bulk modals.

After step 5, `App.tsx` is a thin `AppShell` (layout + routing), not a state hub.

## 9. Verification

- Per context: RTL unit tests — the provider supplies state, the hook throws outside a provider, actions dispatch correctly. The **SyncContext reducer** gets dedicated tests for every `can*` selector across `idle`/`syncing`/`committing` (the invariant this whole finding is about).
- Per step: `tsc` clean + `vite build` green + a `wails dev` demo smoke-test of the touched flows.

## 10. Risks & mitigations

| Risk | Mitigation |
|------|-----------|
| Context re-render churn | Small, single-domain contexts; split value/dispatch where a context updates frequently (Selection). |
| Behavior drift during extraction | Incremental, one domain per PR; behavior-preserving swaps only; per-PR smoke-test. |
| Merge collision with data-layer / a11y PRs | Sequenced after the data layer lands; `App.tsx` is the shared file, so these run strictly after #97/#98/#99 merge. |
| State-machine regressions | The reducer is pure and unit-tested exhaustively before the guards are removed from the handlers. |

## 11. Out of scope / follow-ups

- Component-internal performance (P5/P6/P7 memoization, TestDetail waterfall) — separate work.
- Any further data-layer entities — covered by the data-layer plan.
