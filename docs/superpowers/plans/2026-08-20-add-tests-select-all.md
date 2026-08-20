# Select-All When Adding Tests (`-324`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user tick every test on the page, or every test matching the current folder and search, when adding tests to a Test Set / Plan / Execution, instead of clicking one row at a time.

**Architecture:** Frontend only. `AddTestsModal` gains a header select-all checkbox for the visible page and, when the page is fully selected and more results exist, a banner offering to select everything matching the filter. The second path calls the already-bound `ListMatchingKeys`, mirroring the pattern `TestTable` uses in the Browse view.

**Tech Stack:** React 19 + TypeScript, Vite. No new dependencies. No backend changes.

**Spec:** `docs/superpowers/specs/2026-08-20-v1.10.0-design.md` (section 2)

## Global Constraints

- No backend changes. `ListMatchingKeys` already exists (`app.go:3678`) and is already re-exported through `frontend/src/api.ts`.
- `frontend/wailsjs/` is generated. Never hand-edit it. Nothing in this plan requires regenerating it.
- Reuse the existing CSS classes `select-all-banner` and `link-btn`. Do not introduce a parallel visual language for the same idea.
- Tests already in the target container render disabled and pre-checked. They must never be counted as selectable, never toggled, and never submitted.
- There is no frontend test runner in this project. Verification is `npm run build` (which typechecks) plus the manual demo-mode script in Step 6.
- No AI attribution in commit messages.

---

### Task 1: Page and filter select-all in AddTestsModal

**Files:**
- Modify: `frontend/src/components/AddTestsModal.tsx` (whole file, 226 lines)
- Reference only, do not modify: `frontend/src/components/TestTable.tsx:502-527` and `:680-696` (the pattern being mirrored)

**Interfaces:**
- Consumes: `ListMatchingKeys(profileId: string, q: TestQuery): Promise<string[]>` from `../api`, already exported.
- Produces: no exported surface change. `AddTestsModal`'s props are untouched, so both call sites (the Containers view and the Preconditions view via `onAdd`) pick this up for free.

- [ ] **Step 1: Add the imports and selection state**

In `frontend/src/components/AddTestsModal.tsx`, extend the existing import on line 2:

```tsx
import {
  ListTests,
  ListMatchingKeys,
  AllocateTests,
  ListFolders,
  errMsg,
} from "../api";
import type { TestCase, Folder, TestQuery } from "../api";
```

Add two pieces of state beside the existing ones (after `const [total, setTotal] = useState(0);` on line 40):

```tsx
  const [selectingAll, setSelectingAll] = useState(false);
  const [selectAllError, setSelectAllError] = useState("");
```

- [ ] **Step 2: Extract the query so the list and select-all cannot drift**

The list effect currently builds its query inline (lines 70-82). A select-all that built its own copy would silently diverge the moment a filter is added. Hoist it into one value both use.

Add above the list effect:

```tsx
  // buildQuery is the single source of truth for what the modal is showing.
  // The list and "select all matching" MUST use the same filters, or the
  // banner would select a different set than the one on screen.
  function buildQuery(limit: number, offset: number): TestQuery {
    return {
      search,
      status: "",
      folderId,
      containerKey: "",
      component: "",
      execType: "",
      review: "",
      sortBy: "key",
      desc: false,
      limit,
      offset,
    };
  }
```

Then replace the inline object inside the `ListTests` call with `buildQuery(PAGE_SIZE, page * PAGE_SIZE)`.

- [ ] **Step 3: Add the page-level toggle**

Add below the existing `toggle` function (line 108):

```tsx
  // Rows already in the target container are disabled, so they are not
  // selectable and must not count toward "the whole page is selected".
  const selectableKeys = results
    .filter((t) => !existing.has(t.key))
    .map((t) => t.key);
  const allPageSelected =
    selectableKeys.length > 0 && selectableKeys.every((k) => picked.has(k));
  const somePageSelected = selectableKeys.some((k) => picked.has(k));

  function togglePage() {
    setPicked((prev) => {
      const next = new Set(prev);
      if (allPageSelected) selectableKeys.forEach((k) => next.delete(k));
      else selectableKeys.forEach((k) => next.add(k));
      return next;
    });
  }

  async function selectAllMatching() {
    if (selectingAll) return;
    setSelectingAll(true);
    setSelectAllError("");
    try {
      const keys = await ListMatchingKeys(profileId, buildQuery(0, 0));
      setPicked(new Set((keys ?? []).filter((k) => !existing.has(k))));
    } catch (e) {
      setSelectAllError(errMsg(e));
    } finally {
      setSelectingAll(false);
    }
  }
```

Note `limit: 0` in the select-all query. That is what `TestTable.tsx:517` passes to mean "every match, unpaged".

- [ ] **Step 4: Render the header checkbox and the banner**

Replace the `<ul className="add-test-list">` block (lines 156-182) with a header row above the same list:

```tsx
              <>
                <div className="add-test-selectall">
                  <label>
                    <input
                      type="checkbox"
                      checked={allPageSelected}
                      ref={(el) => {
                        // Indeterminate is not an attribute, only a property.
                        if (el) el.indeterminate = !allPageSelected && somePageSelected;
                      }}
                      disabled={selectableKeys.length === 0}
                      onChange={togglePage}
                    />
                    {allPageSelected
                      ? `Deselect all ${selectableKeys.length} on this page`
                      : `Select all ${selectableKeys.length} on this page`}
                  </label>
                  <span className="muted">{picked.size} selected</span>
                </div>
                <ul className="add-test-list">
                  {results.map((t) => {
                    const already = existing.has(t.key);
                    return (
                      <li
                        key={t.key}
                        className={already ? "add-test-already" : ""}
                      >
                        <label>
                          <input
                            type="checkbox"
                            disabled={already}
                            checked={already || picked.has(t.key)}
                            onChange={() => toggle(t.key)}
                          />
                          <span className="mono">{t.key}</span> {t.summary}
                          {already && (
                            <span className="muted"> · already a member</span>
                          )}
                        </label>
                      </li>
                    );
                  })}
                  {results.length === 0 && (
                    <li className="muted">No tests match.</li>
                  )}
                </ul>
                {allPageSelected && total > PAGE_SIZE && (
                  <div className="select-all-banner">
                    You've selected all {selectableKeys.length} tests on this page.{" "}
                    <button
                      className="link-btn"
                      onClick={selectAllMatching}
                      disabled={selectingAll}
                    >
                      {selectingAll
                        ? "Selecting…"
                        : `Select all ${total.toLocaleString()} matching this filter`}
                    </button>
                    {selectAllError && (
                      <span className="error-text"> {selectAllError}</span>
                    )}
                  </div>
                )}
              </>
```

- [ ] **Step 5: Style the header row**

Add to the stylesheet that already defines `.add-test-list` and `.add-tests-main` (find it with `grep -rn "add-test-list" frontend/src --include=*.css`):

```css
.add-test-selectall {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 6px 4px;
  border-bottom: 1px solid var(--border);
  font-size: 0.9em;
}

.add-test-selectall label {
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
}
```

Use whatever border token the neighbouring rules use; `--border` is the expected name but confirm against the file rather than assuming.

- [ ] **Step 6: Typecheck, build, and verify manually**

Run:

```bash
cd frontend && npm run build
```

Expected: `tsc` reports no errors and Vite builds.

Then run the app against demo mode (`wails dev`, profile with Jira base URL `demo`) and walk this script:

1. Open a Test Plan, click Add tests. Pick a folder with more than 50 tests.
2. Tick the header checkbox. Exactly the 50 visible rows check, the footer reads `Add 50 tests`, and the banner appears.
3. Click `Select all N matching this filter`. The footer count rises to the folder's full total.
4. Untick one row. The header checkbox goes indeterminate and the banner disappears.
5. Type a search term. The list narrows; confirm the header checkbox now covers only the narrowed page.
6. Add tests, reopen the modal, and confirm the tests just added render as `already a member`, are not counted by the header checkbox, and cannot be re-added.
7. Repeat step 2 from the Preconditions view's Add tests button, which reuses this component through `onAdd`.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/AddTestsModal.tsx frontend/src/
git commit -m "feat(containers): select all tests on the page or matching the filter (-324)

Adding a folder's tests to a Test Set / Plan / Execution meant ticking rows
one at a time. The add-tests picker now has a page-level select-all and,
when the page is fully selected, a banner that selects every test matching
the current folder and search via ListMatchingKeys. Tests already in the
container stay excluded from both. The Preconditions view reuses the same
picker and gets this too."
```

---

## Self-review notes

- Spec section 2 has four requirements: page-level tri-state toggle (Step 3 and 4), select-all-matching via `ListMatchingKeys` (Step 3), no shared abstraction extracted (honored, the logic is local to the modal), and the Preconditions reuse path benefiting for free (verified in Step 6 item 7).
- `buildQuery` in Step 2 is an addition beyond the spec's letter. The spec says select-all must use "the identical query object the list effect uses"; hoisting is the only way to make that structurally true rather than a copy that drifts.
- The `indeterminate` ref callback is required because React has no `indeterminate` attribute; it is a DOM property only.
