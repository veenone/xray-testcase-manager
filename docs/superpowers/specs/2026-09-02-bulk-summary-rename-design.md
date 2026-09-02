# Bulk Summary Rename — Design Spec

**Date:** 2026-09-02
**Base:** `main` @ `a578e9f`
**Jira:** `RND_P_4TFINT_05-354`
**Status:** approved (design); pending spec review

## Goal

Rename many test summaries at once by adding a common prefix, a common suffix,
or both. Nothing else about the summary changes.

Teams tag batches of tests by hand today: opening each test, clicking into the
summary, and typing the same `[SMOKE] ` in front of it. For a folder of two
hundred tests that is an afternoon. The Browse grid already knows how to select
those tests, and the local store already journals summary edits, so the missing
piece is a way to express one rule and apply it across the selection.

Deliberately **not** search and replace. Replacing arbitrary substrings across
hundreds of summaries is a different and much riskier feature: the blast radius
of a wrong pattern is unbounded, and a preview cannot make a regex safe. Adding
an affix is bounded, reversible by eye, and covers the case people actually ask
for.

## Scope decisions

- **The rule lives in TypeScript, not Go.** See "Where the rule lives" below.
  This is the load-bearing decision in the spec.
- **Selection only.** The modal renames the current grid selection. Applying to
  "everything matching the filter" is out of scope; the grid's select-all
  already turns a filter into a selection, so the capability exists by
  composition.
- **No automatic separator.** Typing `[SMOKE]` produces `[SMOKE]Login works`,
  not `[SMOKE] Login works`. Inserting a space the user did not type would be a
  silent edit to their input, and the preview makes the absence obvious
  immediately.
- **Read-only preview.** Rows cannot be excluded inside the modal. Selection has
  one home, the grid, and a second selection model in the modal could disagree
  with it.

## Where the rule lives

The backend can already compute per-test values. `applyBulkOperation`
(`internal/testrepo/testrepo.go:4553`) receives each test's current value, which
is how `append` works for descriptions:

```go
case "append":
    return current + "\n" + op.Value, nil
```

`editableFields` already contains `summary`. So `prefix` and `suffix` could be
two more cases, and the backend work would be roughly six lines.

**That was rejected.** The modal has to show a live preview, so the rule would
then exist twice: once in Go for the apply, once in TypeScript for the preview.
Both copies would also need the skip-if-already-affixed rule and the length
limit. Two implementations of one rule that agree on the day they are written is
precisely the failure the `-324` work hoisted `buildQuery` to prevent.

**Chosen: the frontend computes each new summary once.** That exact string is
both previewed and sent. Drift is not merely unlikely, it is unrepresentable.
Consequences:

- Skipped tests are never sent, so they produce no pending row and no audit
  entry for a no-op.
- Each rename arrives as an ordinary `summary` field edit, byte-identical to
  what a manual edit produces, so the pending-change journal, conflict
  detection, and commit path need no changes at all.
- Go cannot unit-test the rule. Offset by making the rule a pure module
  (`lib/rename.ts`) with its own Vitest table, rather than logic buried in a
  component.

The runner-up (rule in Go, preview in TS, kept honest by parallel test tables)
remains viable if the operation ever needs to be scriptable or replayable from
the backend, since `prefix` would then be a first-class operation in the audit
log rather than a `set`.

## Backend

Two additions. Neither touches the commit path.

### `GetTestSummaries`

```go
func (a *App) GetTestSummaries(profileID string, testKeys []string) ([]testrepo.TestSummary, error)
```

`TestSummary` is `{Key, Summary string}`. No equivalent exists: every current
read is either one test by key or a paged query. The modal needs current
summaries for an arbitrary key list, because a selection can span pages and
select-all-matching yields keys with no loaded rows.

Unknown keys are omitted rather than erroring. A key can vanish between
selection and modal open (a sync deleted it), and that should cost one row, not
the whole dialog.

### `BulkRenameTests`

```go
func (a *App) BulkRenameTests(profileID string, renames []testrepo.TestRename) (testrepo.BulkEditResult, error)
```

`TestRename` is `{Key, Summary string}`. The implementation loops
`EditTestField(profileID, key, "summary", newSummary)` and collects per-test
outcomes into the existing `BulkEditResult`, exactly as `BulkEditTests` does. An
unknown or deleted key is reported as a `BulkFailure`, never a panic. An empty
list is a no-op returning an empty result.

It is a separate method from `BulkEditTests` rather than a new `BulkEdit`
operation because `BulkEdit` carries one `Value` for every test, and this
carries a different value per test. Overloading it would mean either a magic
encoding in `Value` or an unused field on a widely used struct.

## Frontend

### `lib/rename.ts`

The whole rule, pure and testable, with no React and no I/O:

```ts
export type RenameState = "changed" | "unchanged" | "too-long";

export interface RenameRow {
  key: string;
  before: string;
  after: string;
  state: RenameState;
  reason?: string;   // why it is unchanged or too long, for the UI
}

export function computeRenames(
  tests: { key: string; summary: string }[],
  opts: { prefix: string; suffix: string },
): RenameRow[];
```

Rules, in order:

1. With both affixes empty, every row is `unchanged`.
2. A summary already starting with the prefix does not get it again. Same for a
   summary already ending with the suffix. When that leaves nothing to add, the
   row is `unchanged` with reason "already has this prefix" (or suffix, or
   both).
3. Otherwise `after = prefix + before + suffix`, minus any affix skipped by
   rule 2.
4. If `after` exceeds `SUMMARY_MAX = 255`, the row is `too-long`. Its `after` is
   still computed so the preview can show what would have happened. A `before`
   that was already over the limit says so instead of blaming the affix (N4).

Only `changed` rows are sent to `BulkRenameTests`.

Matching is exact and case-sensitive, consistent with how Jira treats the text.
Comparison is on the raw string, so a prefix differing only in trailing
whitespace counts as different, which is correct: `[SMOKE]` and `[SMOKE] ` do
produce different summaries.

### `components/BulkRenameModal.tsx`

Built on the existing accessible `Modal` wrapper. Props follow the other six
bulk modals exactly: `{ testKeys, onComplete, onCancel }`.

**Mode.** A segmented control, Prefix / Suffix / Both, rendered as a radio group
so all three options are visible in one glance and reachable by arrow key. A
`<select>` would match `BulkEditModal`, but it hides two of three options behind
a click and this choice governs which inputs appear. Switching mode clears the
affix that mode no longer uses, so a hidden input can never contribute to the
result.

**Inputs.** One labelled text input per active affix, with a worked placeholder
(`[SMOKE] ` and ` (v2)`) that demonstrates the no-automatic-separator rule
without a sentence of explanation.

**Preview.** One row per selected test: the key in mono, then the before value,
then the after value with the inserted affix visually marked so the change is
readable without diffing two strings by eye. Rows carry their state:
`unchanged` rows are muted and give their reason, `too-long` rows are flagged.

The list renders at most `PREVIEW_LIMIT = 200` rows with a "showing 200 of 512"
note beneath. The counts above are always computed across the whole selection,
so the summary line never lies even when the list is truncated. This keeps
per-keystroke re-render bounded; the grid's virtualization is not reused here
because the modal is short-lived and a fixed cap is far less machinery.

**Counts.** A single line above the list: how many change, how many do not, how
many are too long. It sits in an `aria-live="polite"` region (the `LiveRegion`
component added in #97) so a screen-reader user hears the effect of typing
rather than only sighted users seeing it. This is the one place the earlier
frontend audit's `aria-live` finding gets addressed for a new surface.

**States.**

| State | What the user sees |
|---|---|
| No affix typed | Every row unchanged. Apply disabled. "Type a prefix or suffix to see what changes." |
| Summaries loading | Skeleton rows, inputs already usable |
| Fetch failed | Inline error, Apply disabled, Cancel still works |
| Nothing would change | Apply disabled, counts explain why |
| Some skipped | Apply enabled for the rest, with a note naming the skipped count |
| Applying | Apply shows a busy label, inputs disabled |

**Copy.** Plain and short, no em dashes:

- Title: "Rename summaries"
- Empty preview: "Type a prefix or suffix to see what changes."
- Counts: "184 will change, 12 unchanged, 4 too long"
- Skip note: "12 tests already have this prefix. They stay as they are."
- Length note: "4 tests would go over Jira's 255 character limit. They are left out."
- Apply: "Rename 184 tests"

### Wiring

`ModalContext` gains a `bulkRename` key. The entry sits with the other bulk
actions in the Browse toolbar, enabled only when the selection is non-empty. On
success, `afterMutation({ clearSelection: true })`, matching every other bulk
modal.

## Data flow

```
selection (SelectionContext) -> testKeys
  -> useTestSummaries(keys)      one fetch, TanStack Query cache
  -> computeRenames(...)         pure, re-runs per keystroke, no I/O
  -> preview rows + counts
  -> Apply -> BulkRenameTests(changed rows only)
  -> afterMutation({ clearSelection: true })
```

Everything after the initial fetch is local, so typing stays responsive at five
hundred tests.

## Testing

**Vitest on `computeRenames`**, which is where the rule actually is: empty
affixes, prefix only, suffix only, both, already-prefixed, already-suffixed,
already-both, a result landing on exactly 255, a result at 256, multi-byte
characters counted correctly, and an empty test list.

**Go on `BulkRenameTests`**: a pending row per rename with the right before and
after, an unknown key reported as a failure while its siblings still succeed, an
empty list as a no-op, and the audit entry matching a manual summary edit.

**Go on `GetTestSummaries`**: keys returned in request order, unknown keys
omitted, profile scoping.

**Component test on the modal.** This would be the repo's first: existing tests
cover `contexts/`, `queries/`, and `lib/` but no components. Worth adding here
because the preview-to-payload correspondence (only `changed` rows are sent) is
the property most likely to regress and cannot be seen from `computeRenames`
alone.

## Out of scope

- Search and replace within summaries.
- Applying to a filter rather than the current selection.
- Renaming anything other than the summary.
- Undo beyond the existing mechanism: renames are pending changes, so they are
  already discardable before commit like any other edit.

---

## Negative cases

Added after the design was approved, from a hardening pass over the plan. Two
of these are defects the plan would otherwise have shipped.

### N1. More than 32,765 selected tests breaks the read

`ListTestSummaries` as first planned builds one `IN (?, ?, …)` clause. Probed
against this driver:

```
n=32765  ok
n=32766  FAILED: SQL logic error: too many SQL variables (1)
```

The limit is 32,765 keys plus the profile id. `CLAUDE.md` states a ceiling of
50,000 tests, and select-all-matching turns a filter into a selection of every
match, so this is reachable rather than theoretical: one select-all on a large
project and the modal fails to open.

**Handled by chunking.** `ListTestSummaries` splits the key list into batches of
`summaryChunkSize = 5000`, queries each, and merges before re-emitting in
request order. No user-visible ceiling and no change to the signature.

The existing `sqlPlaceholders` helper (`internal/testrepo/sankey.go:229`) has
the same unguarded shape, but its current callers pass container and plan keys,
which are orders of magnitude fewer than tests. Left alone deliberately; worth
knowing if it is ever pointed at test keys.

### N2. A sync during the open modal makes the preview stale

`canSync` is `status === "idle"` (`contexts/syncMachine.ts:137`) and knows
nothing about open modals, so a sync can start while the modal is open and
rewrite `test_case.summary` underneath it. The preview was computed from
summaries fetched at open. Applying then writes `prefix + stale summary`,
silently reverting whatever the sync brought in and queueing that reversion for
Jira. Data loss with a remote write, from a small but real window.

Blocking sync while the modal is open was the runner-up. It was rejected
because it would teach the sync machine about modals, a coupling the context
refactor deliberately avoided, and it only narrows the window rather than
closing it.

**Handled by optimistic concurrency, matching how commits already work.**
`TestRename` carries the value the preview was computed from:

```go
type TestRename struct {
    Key            string `json:"key"`
    Summary        string `json:"summary"`
    ExpectedBefore string `json:"expectedBefore"`
}
```

`BulkRenameTests` reads the current summary and applies the rename only when it
still equals `ExpectedBefore`. A mismatch is reported as an ordinary
`BulkFailure` with the reason "summary changed since the preview was taken", so
the rest of the batch still applies and the user is told which rows to look at.
This is the same shape as the `base_version` check the commit path already uses,
and it is race-free rather than merely unlikely.

### N3. Every row is too long

When the affix alone approaches the limit, every row lands in `too-long` and the
counts read "0 will change, 0 unchanged, 512 too long", which is accurate and
useless. Apply is disabled by the existing `changed > 0` rule, and the modal adds
one message for this case rather than making the user infer it from a list of
512 identical rows: "This prefix is too long to add to any of the selected
tests."

### N4. A summary already over the limit before the rename

A legacy or imported summary can already exceed 255. Adding an affix makes it
`too-long`, but the affix is not the cause, and telling the user their prefix is
too long would send them to fix the wrong thing. `computeRenames` distinguishes
the two: when `before` already exceeds the limit the reason is "already over 255
characters", otherwise "over 255 characters". Both are excluded from the apply.

### N5. Selected tests missing from the cache

`ListTestSummaries` omits keys the profile does not have (N1's contract). A test
can be deleted by a sync between selection and modal open. The modal compares
what it asked for against what it received and notes the difference rather than
quietly previewing fewer rows than the toolbar said were selected: "3 of the 200
selected tests are no longer in the local cache."

### N6. Partial failure on apply

`BulkRenameTests` reports per-test outcomes, and N2 makes non-empty failures a
normal occurrence rather than an edge case. The modal follows `BulkEditModal`
(`components/BulkEditModal.tsx:106`) exactly: close on a clean result, otherwise
stay open and render the succeeded count plus the failed list, so the user can
read what happened before dismissing it.

### N7. An affix of only whitespace

`" "` is a legitimate prefix and is not blocked. It is also usually a typo, and
trailing spaces are invisible in a preview. The modal shows a hint rather than an
error when the active affix is non-empty and entirely whitespace: "This prefix is
only spaces."

### N8. Unicode normalisation

`"é"` as one code point and as `e` plus a combining accent are different strings,
so `startsWith` treats a summary carrying one form as not having a prefix written
in the other, and the affix is added again. Not handled. Normalising would mean
deciding a form and rewriting summaries the user did not ask to change, which is
worse than the rare duplicate. Recorded so it is a known limit rather than a
surprise.
