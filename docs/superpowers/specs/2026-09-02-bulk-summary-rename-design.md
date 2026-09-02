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
   still computed so the preview can show what would have happened.

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
