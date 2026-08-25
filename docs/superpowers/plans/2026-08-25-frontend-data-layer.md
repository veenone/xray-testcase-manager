# Frontend Data Layer — Implementation Plan (Phase 0 → Pilot)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up a TanStack Query server-cache + a typed API client, and migrate the first entity (`pendingChanges`) onto it as a working pilot — without breaking the rest of the app.

**Architecture:** A test runner goes in first (so everything after is test-driven). Then a thin, pure infrastructure layer: a typed `ApiError`/`call()` wrapper, a query-key factory, and a configured `QueryClient` mounted at the root. Then the pilot migrates `pendingChanges` from `useState` + a manual `reloadPending` loader to `usePendingChanges` (a `useQuery`), and redefines `reloadPending` as a **query invalidation** so the ~35 existing mutation call sites keep working untouched (the strangler bridge).

**Tech Stack:** React 19, TypeScript 6, Vite 8, `@tanstack/react-query`, Vitest + React Testing Library.

**Scope of this plan:** Phases 0.5 (test runner), 0 (infra), and 1 (pilot) from the design spec (`docs/superpowers/specs/2026-08-25-frontend-data-layer-design.md`). Phases 2–4 (migrating the tests list/detail, remaining views, then deleting `refreshKey`) get their own plan once the pilot validates the pattern.

## Global Constraints

- Local-IPC tuning: `QueryClient` uses `retry: 0`, `refetchOnWindowFocus: false`, `staleTime: 30_000`. Rely on explicit invalidation, not background refetch.
- Query keys are constructed ONLY via `src/queries/keys.ts`. Every key is prefixed with `profileId`.
- Error convention: user-initiated failures surface via the existing `notice()`; background loads surface via the query's `error` (rendered inline). Never `console.error`-only on primary data.
- Do not change the journaled-mutation model (local `pending_change` → commit).
- Do not touch `App.tsx`'s other state domains — the context split is a separate spec.
- Each task ends green: `npm --prefix frontend test` (once it exists) and `npm --prefix frontend run build` (tsc + vite) both pass.
- Windows/PowerShell environment; run npm via `--prefix frontend` from the repo root, or `cd frontend` first.

---

### Task 1: Test runner (Vitest + React Testing Library)

Installs the runner first so all later tasks are test-driven.

**Files:**
- Modify: `frontend/package.json` (devDependencies + scripts)
- Modify: `frontend/vite.config.ts`
- Create: `frontend/src/test/setup.ts`
- Test: `frontend/src/test/smoke.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: an `npm --prefix frontend test` command (runs `vitest run`); a jsdom + jest-dom test environment; the convention that tests import `{ describe, it, expect }` from `vitest` explicitly (no reliance on globals).

- [ ] **Step 1: Install dev dependencies**

Run:
```bash
npm --prefix frontend install -D vitest jsdom @testing-library/react @testing-library/dom @testing-library/jest-dom
```
Expected: packages added to `frontend/package.json` devDependencies.

- [ ] **Step 2: Configure Vitest in the Vite config**

Replace `frontend/vite.config.ts` with:
```ts
/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    setupFiles: "./src/test/setup.ts",
    css: false,
  },
});
```

- [ ] **Step 3: Create the test setup file**

Create `frontend/src/test/setup.ts`:
```ts
// Extends Vitest's expect with jest-dom matchers (toBeInTheDocument, etc.).
import "@testing-library/jest-dom/vitest";
```

- [ ] **Step 4: Add test scripts**

In `frontend/package.json`, add to the `"scripts"` object:
```json
    "test": "vitest run",
    "test:watch": "vitest"
```

- [ ] **Step 5: Write a smoke test**

Create `frontend/src/test/smoke.test.ts`:
```ts
import { describe, it, expect } from "vitest";
import { keyCompare } from "../sort";

describe("test runner smoke", () => {
  it("runs and evaluates assertions", () => {
    expect(1 + 1).toBe(2);
  });

  it("can import project source modules", () => {
    // keyCompare sorts Jira keys numerically on the trailing number.
    expect(keyCompare("PROJ-2", "PROJ-10")).toBeLessThan(0);
  });
});
```
Note: if `sort.ts` does not export `keyCompare`, replace the second test's import/assertion with any exported pure function from `src/sort.ts`, or delete the second test and keep only the arithmetic assertion.

- [ ] **Step 6: Run the tests**

Run: `npm --prefix frontend test`
Expected: PASS (2 tests in `smoke.test.ts`).

- [ ] **Step 7: Verify the build still passes**

Run: `npm --prefix frontend run build`
Expected: tsc clean, vite build succeeds (exit 0).

- [ ] **Step 8: Commit**

```bash
git add frontend/package.json frontend/package-lock.json frontend/vite.config.ts frontend/src/test/setup.ts frontend/src/test/smoke.test.ts
git commit -m "test(frontend): add Vitest + React Testing Library runner"
```

---

### Task 2: Data-layer infrastructure

Pure, test-driven infra: typed error wrapper, key factory, query client, provider. Nothing consumes it yet, so the app's behavior is unchanged.

**Files:**
- Modify: `frontend/package.json` (add `@tanstack/react-query`)
- Create: `frontend/src/lib/apiError.ts`
- Create: `frontend/src/lib/apiCall.ts`
- Create: `frontend/src/queries/keys.ts`
- Create: `frontend/src/lib/queryClient.ts`
- Modify: `frontend/src/main.tsx`
- Test: `frontend/src/lib/apiError.test.ts`
- Test: `frontend/src/queries/keys.test.ts`

**Interfaces:**
- Consumes: `errMsg` from `../api` (existing Wails error-message extractor).
- Produces:
  - `class ApiError extends Error` and `normalizeError(e: unknown): ApiError` (from `lib/apiError`).
  - `call<T>(fn: () => Promise<T>): Promise<T>` — awaits `fn`, rethrows failures as `ApiError` (from `lib/apiCall`).
  - `keys` — `{ tests(profileId, params), test(profileId, key), pending(profileId), folders(profileId) }`, each returning a `readonly` array beginning with `profileId` (from `queries/keys`).
  - `queryClient: QueryClient` (from `lib/queryClient`).

- [ ] **Step 1: Install TanStack Query**

Run: `npm --prefix frontend install @tanstack/react-query`
Expected: added to `frontend/package.json` dependencies.

- [ ] **Step 2: Write the failing test for normalizeError**

Create `frontend/src/lib/apiError.test.ts`:
```ts
import { describe, it, expect } from "vitest";
import { ApiError, normalizeError } from "./apiError";

describe("normalizeError", () => {
  it("wraps a plain Error into an ApiError preserving the message", () => {
    const out = normalizeError(new Error("boom"));
    expect(out).toBeInstanceOf(ApiError);
    expect(out.message).toBe("boom");
  });

  it("returns the same ApiError unchanged", () => {
    const original = new ApiError("already");
    expect(normalizeError(original)).toBe(original);
  });

  it("always yields a non-empty message", () => {
    expect(normalizeError(undefined).message.length).toBeGreaterThan(0);
  });
});
```

- [ ] **Step 3: Run it to confirm it fails**

Run: `npm --prefix frontend test -- apiError`
Expected: FAIL (cannot find module `./apiError`).

- [ ] **Step 4: Implement apiError**

Create `frontend/src/lib/apiError.ts`:
```ts
import { errMsg } from "../api";

// ApiError is the single error type the data layer throws, so callers can
// pattern-match one shape instead of the raw Wails error grab-bag.
export class ApiError extends Error {
  constructor(
    message: string,
    public readonly cause?: unknown,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

// normalizeError turns any thrown value into an ApiError, reusing the existing
// errMsg extractor so Wails error shapes still produce a readable message.
export function normalizeError(e: unknown): ApiError {
  if (e instanceof ApiError) return e;
  const message = errMsg(e) || "Unknown error";
  return new ApiError(message, e);
}
```
Note: the test mocks nothing, so `errMsg` runs for real. If importing `../api` pulls the Wails runtime and breaks the test environment, add a mock at the top of `apiError.test.ts`: `vi.mock("../api", () => ({ errMsg: (e: unknown) => (e instanceof Error ? e.message : e == null ? "" : String(e)) }));` and `import { vi } from "vitest"`.

- [ ] **Step 5: Run it to confirm it passes**

Run: `npm --prefix frontend test -- apiError`
Expected: PASS (3 tests).

- [ ] **Step 6: Implement the call() wrapper**

Create `frontend/src/lib/apiCall.ts`:
```ts
import { normalizeError } from "./apiError";

// call wraps a binding invocation so every failure surfaces as an ApiError.
// Query/mutation functions use this instead of calling bindings directly.
export async function call<T>(fn: () => Promise<T>): Promise<T> {
  try {
    return await fn();
  } catch (e) {
    throw normalizeError(e);
  }
}
```

- [ ] **Step 7: Write the failing test for the key factory**

Create `frontend/src/queries/keys.test.ts`:
```ts
import { describe, it, expect } from "vitest";
import { keys } from "./keys";

describe("query keys", () => {
  it("prefixes every key with the profile id", () => {
    expect(keys.pending("p1")[0]).toBe("p1");
    expect(keys.tests("p1", {})[0]).toBe("p1");
    expect(keys.test("p1", "PROJ-1")[0]).toBe("p1");
    expect(keys.folders("p1")[0]).toBe("p1");
  });

  it("distinguishes entities by their second segment", () => {
    expect(keys.pending("p1")[1]).toBe("pending");
    expect(keys.tests("p1", {})[1]).toBe("tests");
  });
});
```

- [ ] **Step 8: Run it to confirm it fails**

Run: `npm --prefix frontend test -- keys`
Expected: FAIL (cannot find module `./keys`).

- [ ] **Step 9: Implement the key factory**

Create `frontend/src/queries/keys.ts`:
```ts
import type { TestQuery } from "../api";

// keys is the single source of truth for query keys, so a mutation's
// invalidation target can never drift from the read it must invalidate.
export const keys = {
  tests: (profileId: string, params: TestQuery) =>
    [profileId, "tests", params] as const,
  test: (profileId: string, key: string) =>
    [profileId, "test", key] as const,
  pending: (profileId: string) => [profileId, "pending"] as const,
  folders: (profileId: string) => [profileId, "folders"] as const,
};
```
Note: if `TestQuery` is not an exported type from `../api`, use `Record<string, unknown>` for the `params` type instead.

- [ ] **Step 10: Run it to confirm it passes**

Run: `npm --prefix frontend test -- keys`
Expected: PASS (2 tests).

- [ ] **Step 11: Create the query client**

Create `frontend/src/lib/queryClient.ts`:
```ts
import { QueryClient } from "@tanstack/react-query";

// Tuned for local IPC, not network: the backend is a fast local Go process and
// Jira is synced on demand, so we do not retry or refetch in the background —
// freshness comes from explicit invalidation after mutations.
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 0,
      refetchOnWindowFocus: false,
      staleTime: 30_000,
    },
  },
});
```

- [ ] **Step 12: Mount the provider at the root**

Replace `frontend/src/main.tsx` with:
```tsx
import React from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import "./style.css";
import App from "./App";
import { queryClient } from "./lib/queryClient";

const container = document.getElementById("root");

const root = createRoot(container!);

root.render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>
  </React.StrictMode>,
);
```

- [ ] **Step 13: Run the full test + build**

Run: `npm --prefix frontend test`
Expected: PASS (all tests).
Run: `npm --prefix frontend run build`
Expected: tsc clean, vite build succeeds (exit 0).

- [ ] **Step 14: Commit**

```bash
git add frontend/package.json frontend/package-lock.json frontend/src/lib/apiError.ts frontend/src/lib/apiError.test.ts frontend/src/lib/apiCall.ts frontend/src/queries/keys.ts frontend/src/queries/keys.test.ts frontend/src/lib/queryClient.ts frontend/src/main.tsx
git commit -m "feat(frontend): data-layer infra — typed API client + query client (A5)"
```

---

### Task 3: Pilot — migrate `pendingChanges` to a query

Replaces the `pendingChanges` `useState` + manual `reloadPending` loader with `usePendingChanges`, and redefines `reloadPending` as a query invalidation. The ~35 existing `reloadPending()` call sites are deliberately left untouched — that is the strangler bridge.

**Files:**
- Create: `frontend/src/queries/pending.ts`
- Test: `frontend/src/queries/pending.test.tsx`
- Modify: `frontend/src/App.tsx` (lines ~158, ~325–367 — the `pendingChanges` state, `pendingByTestKey` memo dependency, `reloadPending`, and its effect)

**Interfaces:**
- Consumes: `call` (`lib/apiCall`), `keys` (`queries/keys`), `ListPendingChanges` + `PendingChange` (`../api`), `useQuery`/`useQueryClient` (`@tanstack/react-query`).
- Produces: `usePendingChanges(profileId: string)` returning a TanStack Query result whose `.data` is `PendingChange[] | undefined`.

- [ ] **Step 1: Write the failing test for usePendingChanges**

Create `frontend/src/queries/pending.test.tsx`:
```tsx
import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { usePendingChanges } from "./pending";
import * as api from "../api";

vi.mock("../api", () => ({
  ListPendingChanges: vi.fn(),
  errMsg: (e: unknown) => (e instanceof Error ? e.message : String(e)),
}));

function makeWrapper() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

describe("usePendingChanges", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns the pending changes on success", async () => {
    (api.ListPendingChanges as ReturnType<typeof vi.fn>).mockResolvedValue([
      { id: "1" },
    ]);
    const { result } = renderHook(() => usePendingChanges("p1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(1);
  });

  it("surfaces load failures as an error state (not silently)", async () => {
    (api.ListPendingChanges as ReturnType<typeof vi.fn>).mockRejectedValue(
      new Error("boom"),
    );
    const { result } = renderHook(() => usePendingChanges("p1"), {
      wrapper: makeWrapper(),
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error).toBeInstanceOf(Error);
  });

  it("does not fetch without a profile id", () => {
    const { result } = renderHook(() => usePendingChanges(""), {
      wrapper: makeWrapper(),
    });
    expect(result.current.fetchStatus).toBe("idle");
    expect(api.ListPendingChanges).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `npm --prefix frontend test -- pending`
Expected: FAIL (cannot find module `./pending`).

- [ ] **Step 3: Implement the pending query hook**

Create `frontend/src/queries/pending.ts`:
```ts
import { useQuery } from "@tanstack/react-query";
import { ListPendingChanges } from "../api";
import { call } from "../lib/apiCall";
import { keys } from "./keys";

// usePendingChanges loads the active profile's pending-change journal. Replaces
// the App-level useState + manual reloadPending loader (audit A3): freshness now
// comes from invalidating keys.pending(profileId) after a mutation.
export function usePendingChanges(profileId: string) {
  return useQuery({
    queryKey: keys.pending(profileId),
    queryFn: () => call(() => ListPendingChanges(profileId)),
    enabled: !!profileId,
  });
}
```

- [ ] **Step 4: Run it to confirm it passes**

Run: `npm --prefix frontend test -- pending`
Expected: PASS (3 tests).

- [ ] **Step 5: Wire the hook into App.tsx — imports**

In `frontend/src/App.tsx`, add to the React import and the component imports (near the other `./queries` / `./lib` imports; create the grouping if absent):
```tsx
import { useQueryClient } from "@tanstack/react-query";
import { usePendingChanges } from "./queries/pending";
import { keys } from "./queries/keys";
```
Ensure `useCallback` is already imported from `react` (it is).

- [ ] **Step 6: Replace the pendingChanges state with the query**

In `frontend/src/App.tsx`, find:
```tsx
  const [pendingChanges, setPendingChanges] = useState<PendingChange[]>([]);
```
Replace with:
```tsx
  const queryClient = useQueryClient();
  const pendingQuery = usePendingChanges(activeId);
  const pendingChanges = pendingQuery.data ?? [];
```
Note: `activeId` is declared earlier in the component; this line must come after it. If a lint error reports `activeId` used before declaration, move these three lines to just below the `activeId` state declaration.

- [ ] **Step 7: Redefine reloadPending as an invalidation and drop its effect**

In `frontend/src/App.tsx`, find:
```tsx
  const reloadPending = useCallback(() => {
    if (!activeId) {
      setPendingChanges([]);
      return;
    }
    ListPendingChanges(activeId)
      .then(setPendingChanges)
      .catch((e) => console.error("list pending:", errMsg(e)));
  }, [activeId]);

  useEffect(() => {
    reloadPending();
  }, [reloadPending, refreshKey]);
```
Replace with:
```tsx
  // reloadPending now invalidates the pending query instead of manually
  // refetching, so the existing ~35 call sites keep working during the
  // migration (the strangler bridge). The query itself loads on mount and when
  // activeId changes, so no refreshKey-driven effect is needed.
  const reloadPending = useCallback(() => {
    if (activeId) {
      queryClient.invalidateQueries({ queryKey: keys.pending(activeId) });
    }
  }, [queryClient, activeId]);
```

- [ ] **Step 8: Fix the pendingByTestKey memo dependency**

In `frontend/src/App.tsx`, find the end of the `pendingByTestKey` memo:
```tsx
  }, [pendingChanges]);
```
Replace with:
```tsx
  }, [pendingQuery.data]);
```
Rationale: `pendingChanges` is a fresh array reference every render (`?? []`), which would defeat the memo; `pendingQuery.data` is stable between fetches.

- [ ] **Step 9: Remove now-unused imports**

In `frontend/src/App.tsx`, if `ListPendingChanges` is no longer referenced anywhere else in the file, remove it from the `../api` import list. Leave `errMsg` and `PendingChange` (still used elsewhere). Run the build in the next step to confirm there are no unused-symbol or missing-symbol errors.

- [ ] **Step 10: Run tests + build**

Run: `npm --prefix frontend test`
Expected: PASS (all tests).
Run: `npm --prefix frontend run build`
Expected: tsc clean, vite build succeeds (exit 0). If tsc reports `setPendingChanges` is undefined, search `App.tsx` for any remaining `setPendingChanges(` references and remove/convert them (there should be none outside the block replaced in Step 7).

- [ ] **Step 11: Manual smoke-test (record the result in the PR)**

Run: `wails dev`, open a `demo` profile, then verify:
- The header "N pending" badge populates on load.
- Making an edit (e.g. edit a Test field) still updates the badge and the grid dirty-dot (proves `reloadPending` → invalidation → refetch works end to end).
- Opening the Pending Changes modal shows the changes; committing/discarding updates the list.

Note: this step is manual because there is no automated UI harness; if `wails dev` is unavailable in this environment, hand off to the reviewer and note it in the PR.

- [ ] **Step 12: Commit**

```bash
git add frontend/src/queries/pending.ts frontend/src/queries/pending.test.tsx frontend/src/App.tsx
git commit -m "feat(frontend): migrate pendingChanges to a query cache (A3 pilot)"
```

---

## Phases 2–4 (next plan, after the pilot lands)

Once Task 3 is merged and smoke-tested, a follow-up plan covers, one entity per task, each with the same read-hook + invalidation pattern proven here:
- **Phase 2:** tests list (`useTests` in `TestTable`) and detail (`useTestDetail` in `TestDetail`, collapsing its fetch waterfall).
- **Phase 3:** containers, preconditions, requirements, coverage, duplicates, misspellings, dashboard.
- **Phase 4:** convert mutations to `useMutation` with per-entity invalidation; delete `refreshKey`, `detailVersion`, and the `reloadPending` bridge.

## Self-Review

- **Spec coverage:** A5 (typed client + uniform errors) → Tasks 2. A3 (retire refreshKey refetch-everything, per-entity cache) → started in Task 3 (pending), completed across Phases 2–4. Phase 0.5 test runner → Task 1. Local-IPC tuning, key-factory single-source, error convention → Task 2 + Global Constraints. ✓
- **Placeholder scan:** none — every step has concrete code/commands; the two "if X isn't exported/available" notes give explicit fallbacks rather than deferring. ✓
- **Type consistency:** `call<T>`, `keys.pending(profileId)`, `usePendingChanges(profileId)`, `queryClient` names are used identically across Tasks 2 and 3. `usePendingChanges().data` typed `PendingChange[] | undefined`, consumed as `pendingQuery.data ?? []`. ✓
