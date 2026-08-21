# Onboarding Tour (`-335`) Implementation Plan

> **DELIVERED** 2026-08-21 in PR #93, re-targeted onto `main` by #94. Two
> commits rather than three: Task 3's `tour.css` shipped with Task 2 because
> `useTour.ts` imports it and splitting would leave the tree unbuildable. The
> theme tokens are `--surface` / `--text` / `--text-muted` / `--border` /
> `--accent`, not the `--panel` / `--muted` this plan guessed.
>
> **NOT VERIFIED:** Task 2 Step 7 (the demo-mode walkthrough) and Task 3 Step 3
> (both themes) were never run. The tour has not been stepped through in a
> live app. All seven `data-tour` anchors are confirmed present in source and
> matched against the step list, and driver.js's CSS is confirmed in the built
> bundle, but nothing beyond that.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Walk a new user through XTM's core loop once, automatically after their first successful sync, and let them replay it any time from the More menu.

**Architecture:** `driver.js` renders the spotlight and popover. The tour is data: a step array naming `data-tour` attributes on permanently-mounted elements, kept separate from the hook that runs it. Whether the user has seen it persists as a version number in the existing global settings table, so a later release can re-offer a rewritten tour by bumping one constant.

**Tech Stack:** React 19 + TypeScript, Vite, `driver.js` (new dependency). Go for the settings persistence.

**Spec:** `docs/superpowers/specs/2026-08-20-v1.10.0-design.md` (section 3)

## Global Constraints

- `driver.js` is the project's **first new frontend dependency** since inception. `frontend/package.json` currently holds exactly react, react-dom, react-markdown, remark-gfm. Add one entry, nothing else.
- Everything ships offline. Vite bundles the dependency at build time; the tour must make no network request at runtime.
- `frontend/wailsjs/` is generated. Never hand-edit it. Task 1 changes a Go struct, so the bindings must be regenerated with `wails build`.
- Credentials never appear in tour copy, settings values, or logs.
- The tour must work in demo mode (Jira base URL `demo`), which is how the app is normally exercised.
- Every step targets an element that is **always mounted**. Conditionally rendered targets are the main failure mode for tours; see Task 2 Step 3.
- Schema is unchanged. Settings live in the existing key-value `app_setting` table, so there is no migration and `schemaVersion` stays where `-336` left it.
- No AI attribution in commit messages.

---

### Task 1: Persist whether the tour has been seen

**Files:**
- Modify: `internal/settings/settings.go` (key constant, struct field, `Get`, new setter)
- Modify: `app.go` (one bound method, beside `SetShowCoverage`)
- Test: `internal/settings/settings_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `settings.Settings` gains `TourSeenVersion int \`json:"tourSeenVersion"\`` (0 when never seen)
  - `func (m *Manager) SetTourSeenVersion(v int) error`
  - `func (a *App) SetTourSeenVersion(v int) error` — the bound method the frontend calls
  - Existing `GetSettings()` carries the new field to the frontend with no signature change.

- [ ] **Step 1: Write the failing test**

Add to `internal/settings/settings_test.go` (create it if absent, following the construction the other `internal/` tests use to open a temp store):

```go
func TestTourSeenVersionRoundTrips(t *testing.T) {
	m := newManager(t)

	got, err := m.Get()
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.TourSeenVersion != 0 {
		t.Errorf("fresh install has TourSeenVersion %d, want 0 (never seen)", got.TourSeenVersion)
	}

	if err := m.SetTourSeenVersion(1); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err = m.Get()
	if err != nil {
		t.Fatalf("get after set: %v", err)
	}
	if got.TourSeenVersion != 1 {
		t.Errorf("got TourSeenVersion %d, want 1", got.TourSeenVersion)
	}
}

func TestTourSeenVersionIgnoresUnparsableValue(t *testing.T) {
	// A hand-edited or corrupted row must degrade to "never seen" rather
	// than erroring the whole settings load, which would break startup.
	m := newManager(t)
	if err := m.SetTourSeenVersion(3); err != nil {
		t.Fatalf("set: %v", err)
	}
	writeRawSetting(t, m, "tour_seen_version", "not-a-number")

	got, err := m.Get()
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.TourSeenVersion != 0 {
		t.Errorf("got %d, want 0 for an unparsable value", got.TourSeenVersion)
	}
}
```

`newManager(t)` opens a `store.Store` on a temp path and returns `settings.NewManager(s)`. `writeRawSetting(t, m, key, value)` writes directly into `app_setting`; if the package has no such helper, add one in the test file using the store's `DB()`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/settings/ -run TourSeenVersion -v`

Expected: compile failure, `got.TourSeenVersion undefined` and `m.SetTourSeenVersion undefined`.

- [ ] **Step 3: Add the setting**

In `internal/settings/settings.go`, add the key beside the others in the `const` block:

```go
	keyTourSeenVersion     = "tour_seen_version"
```

Add the field to `Settings`, after `ShowCoverage`:

```go
	// TourSeenVersion is the version of the onboarding tour this user has
	// already been through, 0 when they never have. Storing a version rather
	// than a bool lets a later release re-offer a rewritten tour by bumping
	// the frontend's TOUR_VERSION constant.
	TourSeenVersion int `json:"tourSeenVersion"`
```

In `Get`, read it alongside the others:

```go
	tourSeen, err := m.value(keyTourSeenVersion)
	if err != nil {
		return Settings{}, err
	}
```

and assign it beside `s.ShowCoverage`:

```go
	// Default 0 (never seen) when unset or unparsable. A corrupted value must
	// not fail the whole settings load, which runs at startup.
	s.TourSeenVersion, _ = strconv.Atoi(tourSeen)
```

Add the setter beside `SetShowCoverage`:

```go
// SetTourSeenVersion records which version of the onboarding tour the user has
// completed or skipped, so it is not shown again until the tour changes.
func (m *Manager) SetTourSeenVersion(v int) error {
	return m.setValue(keyTourSeenVersion, strconv.Itoa(v))
}
```

`strconv` is already imported.

- [ ] **Step 4: Bind it**

In `app.go`, beside the existing `SetShowCoverage` binding, add:

```go
// SetTourSeenVersion records that the user has completed or skipped the
// onboarding tour at the given version (-335).
func (a *App) SetTourSeenVersion(v int) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.settings.SetTourSeenVersion(v)
}
```

Match the receiver field name and the guard to whatever `SetShowCoverage` uses; `a.settings` is the expected name but confirm rather than assume.

- [ ] **Step 5: Run tests to verify they pass**

Run: `gofmt -w . && go build ./... && go test ./internal/settings/ ./... -v`

Expected: PASS.

- [ ] **Step 6: Regenerate bindings and export the new method**

```bash
wails build
```

Confirm `frontend/wailsjs/go/main/App.d.ts` now declares `SetTourSeenVersion`, and `frontend/wailsjs/go/models.ts` shows `tourSeenVersion` on `Settings`. Then add `SetTourSeenVersion` to the re-export list in `frontend/src/api.ts`, in the same alphabetical position the file already uses for its other bindings.

- [ ] **Step 7: Commit**

```bash
git add internal/settings/settings.go internal/settings/settings_test.go app.go frontend/src/api.ts frontend/wailsjs
git commit -m "feat(settings): persist which onboarding tour version the user has seen (-335)

Stores a version rather than a bool so a later release can re-offer a
rewritten tour by bumping one constant. An unset or corrupted value reads
as 0 (never seen) rather than failing the settings load, which runs at
startup."
```

---

### Task 2: The tour itself

**Files:**
- Modify: `frontend/package.json` (add `driver.js`)
- Create: `frontend/src/tour/steps.ts`
- Create: `frontend/src/tour/useTour.ts`
- Modify: `frontend/src/App.tsx` (`data-tour` attributes, the hook, the More menu entry, auto-start)
- Modify: `frontend/src/components/TestTable.tsx` (two `data-tour` attributes)

**Interfaces:**
- Consumes: `SetTourSeenVersion` and `Settings.tourSeenVersion` from Task 1.
- Produces:
  - `export const TOUR_VERSION = 1` from `steps.ts`
  - `export interface TourStep { id: string; target: string; title: string; body: string; side?: "top" | "bottom" | "left" | "right" }`
  - `export const TOUR_STEPS: TourStep[]`
  - `export function useTour(opts: { onBeforeStart: () => void; onFinish: () => void }): { start: () => void }`

- [ ] **Step 1: Add the dependency**

```bash
cd frontend && npm install driver.js
```

Confirm `package.json` now lists exactly five dependencies and that `driver.js` is a real entry rather than a transitive hoist.

- [ ] **Step 2: Write the steps**

Create `frontend/src/tour/steps.ts`:

```ts
// The onboarding tour, as data (-335).
//
// Every step targets a `data-tour` attribute rather than a CSS class. Classes
// churn with styling work; a dedicated attribute is an explicit contract, so a
// reader can see at the element that something depends on it.
//
// CONSTRAINT: every target must be an element that is ALWAYS MOUNTED while the
// Browse view is showing. Conditionally rendered targets (the pending-changes
// badge, an open detail panel, anything inside a modal) are the main way tours
// break, because the step lands on nothing. That is why the local-edit and
// commit ideas are explained from stable anchors instead of being spotlighted
// on the widgets themselves, and why this release stays inside the core loop
// rather than driving navigation between views.

export const TOUR_VERSION = 1;

export interface TourStep {
  id: string;
  /** Value of the target element's data-tour attribute. */
  target: string;
  title: string;
  body: string;
  side?: "top" | "bottom" | "left" | "right";
}

export const TOUR_STEPS: TourStep[] = [
  {
    id: "profile",
    target: "profile",
    title: "Your connection",
    body: "Each profile points at one Jira project. Switch here to work on a different project. The DEMO chip means you are on sample data, not a real Jira.",
    side: "bottom",
  },
  {
    id: "sync",
    target: "sync",
    title: "Pull from Jira",
    body: "Sync copies tests from Jira into a local cache on this machine. That cache is what makes browsing 10,000 tests instant. Jira stays the system of record.",
    side: "bottom",
  },
  {
    id: "views",
    target: "views",
    title: "The views",
    body: "Tests, preconditions, requirements, test plans and coverage each get their own view. This tour stays in Browse.",
    side: "bottom",
  },
  {
    id: "search",
    target: "search",
    title: "Find tests fast",
    body: "Search and filters run against the local cache, so results appear as you type. Combine them with the folder tree to narrow to exactly the tests you want.",
    side: "bottom",
  },
  {
    id: "grid",
    target: "grid",
    title: "Browse and edit",
    body: "Click a test to open its details, where you can edit fields and steps. Tick the checkboxes to act on many tests at once.",
    side: "top",
  },
  {
    id: "pending",
    target: "pending",
    title: "Edits stay local until you commit",
    body: "This is the part that surprises people: your edits are not written to Jira as you make them. They collect as pending changes, shown here, and a Commit button appears once you have some. Nothing reaches Jira until you press it.",
    side: "bottom",
  },
  {
    id: "restart",
    target: "more",
    title: "That's the loop",
    body: "Sync, browse, edit, commit. Everything else builds on it. You can run this tour again any time from this menu.",
    side: "bottom",
  },
];
```

- [ ] **Step 3: Write the hook**

Create `frontend/src/tour/useTour.ts`:

```ts
import { driver } from "driver.js";
import "driver.js/dist/driver.css";
import "./tour.css";
import { SetTourSeenVersion } from "../api";
import { TOUR_STEPS, TOUR_VERSION } from "./steps";

interface Options {
  /** Called before the first step, to put the app in a state the steps assume. */
  onBeforeStart: () => void;
  /** Called once the tour ends, whether completed or skipped. */
  onFinish: () => void;
}

// useTour builds the driver instance and returns a start function. It is not
// stateful React, just a factory, so it can be called from a menu handler or an
// effect without re-render concerns.
export function useTour({ onBeforeStart, onFinish }: Options) {
  function markSeen() {
    // Best-effort: a failed write means the tour is offered again next launch,
    // which is a far better failure than blocking the user inside it.
    SetTourSeenVersion(TOUR_VERSION).catch(() => {});
    onFinish();
  }

  function start() {
    onBeforeStart();
    // The DOM has to settle after onBeforeStart switches views, or the first
    // selectors resolve against the outgoing view.
    requestAnimationFrame(() => {
      const steps = TOUR_STEPS.map((s) => ({
        element: `[data-tour="${s.target}"]`,
        popover: {
          title: s.title,
          description: s.body,
          side: s.side ?? "bottom",
        },
      })).filter((s) => document.querySelector(s.element) !== null);

      if (steps.length === 0) return;

      driver({
        showProgress: true,
        allowClose: true,
        nextBtnText: "Next",
        prevBtnText: "Back",
        doneBtnText: "Done",
        onDestroyed: markSeen,
        steps,
      }).drive();
    });
  }

  return { start };
}
```

The `.filter(...)` is the safety net for the always-mounted rule in `steps.ts`. If a target ever disappears, the tour skips that step rather than showing an unanchored popover.

- [ ] **Step 4: Add the target attributes**

In `frontend/src/App.tsx`:

- Line 1046, the `<select className="profile-select"`: add `data-tour="profile"`.
- Line 1101, the `<nav className="view-tabs topbar-zone topbar-center">`: add `data-tour="views"`.
- Line 1176, the `<div className="topbar-zone topbar-right">`: add `data-tour="pending"`. This zone is always mounted, unlike the pending badge inside it, which renders only when `pendingChanges.length > 0`.
- Line 1263, the Sync `<button className="btn btn-primary" onClick={runSync}`: add `data-tour="sync"`.
- The `More` `<Menu>` at line 1202: `Menu` renders a `<button>` internally, so the attribute cannot be placed on the component. Wrap it in `<span data-tour="more">…</span>`, or add a `data-tour` passthrough prop to `Menu`. Prefer the wrapper; it leaves the shared component alone.

In `frontend/src/components/TestTable.tsx`:

- The `<input className="search"` at line 532: add `data-tour="search"`.
- The `<div className="table-scroll">` at line 698: add `data-tour="grid"`.

- [ ] **Step 5: Wire the hook into App**

In `frontend/src/App.tsx`, import and instantiate:

```tsx
import { useTour } from "./tour/useTour";
import { TOUR_VERSION } from "./tour/steps";
```

```tsx
  const { start: startTour } = useTour({
    // Steps 4 and 5 target Browse-only elements, and the tour can be restarted
    // from any view, so put the app in Browse first.
    onBeforeStart: () => setView("browse"),
    onFinish: () => setTourSeenVersion(TOUR_VERSION),
  });
```

Add `tourSeenVersion` to the component's settings state, loaded from the existing `GetSettings()` call the app already makes at startup. Find it with `grep -n "GetSettings" frontend/src/App.tsx` and extend the existing handler rather than adding a second call.

Auto-start once, after the first successful sync:

```tsx
  // Auto-start the tour once, after the FIRST successful sync rather than on
  // first launch: before a sync the grid is empty, so half the steps would
  // spotlight elements with no data behind them and teach nothing.
  useEffect(() => {
    if (tourSeenVersion >= TOUR_VERSION) return;
    if (syncing || !activeId) return;
    if (page.total === 0) return;
    startTour();
  }, [tourSeenVersion, syncing, activeId, page.total]);
```

Use whatever the App-level variable for the loaded test count actually is; `page.total` is the expected name in `TestTable` but App may track it differently. If App has no such value, gate on the sync-completion path in `doSync` instead, calling `startTour()` there when `tourSeenVersion < TOUR_VERSION`. That is the more reliable hook and is preferred if it is available.

Add the menu entry to the `More` menu `items` array (line 1206), directly above the `diag` entry:

```tsx
              {
                key: "tour",
                label: "Take the tour",
                onClick: startTour,
                title: "Replay the onboarding walkthrough",
              },
```

- [ ] **Step 6: Typecheck and build**

Run:

```bash
cd frontend && npm run build
```

Expected: `tsc` reports no errors and Vite builds. Task 3 handles how it looks; at this point the tour will render with driver.js's stock styling.

- [ ] **Step 7: Verify manually in demo mode**

Run `wails dev` with a profile whose Jira base URL is `demo`. Check:

1. On a fresh profile that has never synced, the tour does **not** start on launch.
2. After the first sync completes and tests appear, the tour starts on its own.
3. All seven steps resolve and their spotlights land on the right elements.
4. Clicking Done writes the setting: restart the app and confirm the tour does not auto-start again.
5. Skipping mid-tour (Escape or the close control) also writes the setting.
6. More → Take the tour replays it, including from a non-Browse view, which must switch to Browse first.

- [ ] **Step 8: Commit**

```bash
git add frontend/package.json frontend/package-lock.json frontend/src/tour frontend/src/App.tsx frontend/src/components/TestTable.tsx
git commit -m "feat(onboarding): add a guided tour of the core loop (-335)

Seven steps covering profile, sync, views, search, the grid, the local-edit
and commit model, and where to replay it. Auto-starts once after the first
successful sync rather than on first launch, so it never spotlights an
empty grid, and can be replayed from More -> Take the tour.

Steps are data in tour/steps.ts and target data-tour attributes on
permanently mounted elements. Adds driver.js, the project's first new
frontend dependency."
```

---

### Task 3: Theme the tour

driver.js ships opinionated light styling. Both XTM themes have to look deliberate.

**Files:**
- Create: `frontend/src/tour/tour.css`

**Interfaces:**
- Consumes: the `import "./tour.css"` already written in Task 2's `useTour.ts`.
- Produces: nothing importable.

- [ ] **Step 1: Find the theme tokens**

Run: `grep -rn "^\s*--" frontend/src/*.css | head -40`

Note the actual custom property names for panel background, primary text, muted text, border, and the primary accent. The names below (`--panel`, `--text`, `--muted`, `--border`, `--accent`) are the expected ones. Use whatever the file really defines; do not introduce new tokens.

- [ ] **Step 2: Write the overrides**

Create `frontend/src/tour/tour.css`:

```css
/* Overrides for driver.js's stock styling, so the tour reads as part of the
   app in both themes. Driver's own CSS is imported first in useTour.ts, so
   these rules win by load order without needing !important. */

.driver-popover {
  background-color: var(--panel);
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.28);
  max-width: 340px;
}

.driver-popover-title {
  color: var(--text);
  font-size: 1rem;
  font-weight: 600;
}

.driver-popover-description {
  color: var(--muted);
  line-height: 1.5;
}

.driver-popover-progress-text {
  color: var(--muted);
}

/* Driver draws the popover's arrow with border colors, so each side needs its
   own rule or the arrow keeps the stock white against a dark panel. */
.driver-popover-arrow-side-top { border-top-color: var(--panel); }
.driver-popover-arrow-side-bottom { border-bottom-color: var(--panel); }
.driver-popover-arrow-side-left { border-left-color: var(--panel); }
.driver-popover-arrow-side-right { border-right-color: var(--panel); }

.driver-popover-navigation-btns button {
  background-color: transparent;
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 4px 12px;
  text-shadow: none;
  font-size: 0.9rem;
  cursor: pointer;
}

.driver-popover-navigation-btns button:hover {
  border-color: var(--accent);
}

.driver-popover-next-btn {
  background-color: var(--accent) !important;
  border-color: var(--accent) !important;
  color: #fff !important;
}

.driver-popover-close-btn {
  color: var(--muted);
}
```

The two `!important` uses are deliberate: driver.js sets the next-button colors inline on the element, which a plain rule cannot beat. Every other rule wins on load order alone.

- [ ] **Step 3: Verify both themes**

Run `wails dev` in demo mode. Start the tour from More → Take the tour, and step through all seven in **light** theme, then switch via More → Theme: Dark and step through again. Check on every step:

- Popover background matches the app's panels, not stock white.
- Title and body text meet contrast against that background.
- The arrow is the panel color, not white, on all four sides used.
- Next / Back / Done are legible and the primary button reads as primary.
- The spotlight cut-out is visible against the page dim in both themes.

Fix anything that fails before committing. This is the whole point of the task, so do not skip a theme.

- [ ] **Step 4: Build**

Run: `cd frontend && npm run build`

Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/tour/tour.css
git commit -m "style(onboarding): theme the tour popover for light and dark (-335)

driver.js ships light-only styling, so the popover, arrow and buttons now
draw from the app's own theme tokens. The next button needs !important
because driver sets its colors inline."
```

---

## Self-review notes

- Spec section 3 asks for eight steps. This plan ships **seven**, and the deviation is deliberate: the spec's separate "pending-changes badge" and "Commit" steps both target elements that are conditionally rendered (`App.tsx:1177` gates the badge on `pendingChanges.length > 0`, and Commit lives inside a modal that only opens when changes exist). A tour cannot spotlight a widget that is not on screen. Both ideas are taught in one step anchored to the always-mounted `topbar-right` zone, which is also the step where the local-first model is explained. The same reasoning merged the spec's separate "test row" and "detail panel" steps into `grid`. The always-mounted constraint is recorded in `steps.ts` so a later contributor adding a step understands it.
- Spec's "auto-start after first successful sync" is Task 2 Step 5, with a documented fallback if App does not expose a test count at that level.
- Spec's `tour_seen_version` in `internal/settings` is Task 1.
- Spec's `data-tour` attribute contract is Task 2 Steps 2 and 4.
- Spec's More-menu restart is Task 2 Step 5.
- Spec's demo-mode support needs no code; it is verified in Task 2 Step 7.
- Spec's risk about driver.js default CSS is Task 3, budgeted as its own task rather than treated as incidental, exactly as the spec asked.
