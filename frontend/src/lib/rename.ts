// The bulk-rename rule (RND_P_4TFINT_05-354).
//
// This lives in TypeScript rather than Go on purpose. The modal previews the
// result live, so a Go implementation would mean the same rule written twice,
// once for the preview and once for the apply, and the two would drift. The
// modal computes each new summary here, shows exactly that string, and sends
// exactly that string. See the design spec's "Where the rule lives".

// SUMMARY_MAX is Jira's issue-summary limit. A computed summary longer than
// this is reported as too-long and excluded from the apply.
export const SUMMARY_MAX = 255;

export type RenameState = "changed" | "unchanged" | "too-long";

export interface RenameRow {
  key: string;
  before: string;
  after: string;
  state: RenameState;
  /** Why the row is unchanged or too long, shown in the preview. */
  reason?: string;
  /**
   * `after` split into the text this rename inserts and the text it did not
   * touch, so the preview can mark the change instead of asking the reader to
   * diff two near-identical strings by eye. Concatenating these three always
   * reproduces `after` exactly, which is what the tests assert.
   */
  addedPrefix: string;
  body: string;
  addedSuffix: string;
}

export interface RenameInput {
  key: string;
  summary: string;
}

export interface RenameOptions {
  prefix: string;
  suffix: string;
}

// charLength counts characters rather than UTF-16 code units, so an emoji or
// any astral character counts once, the way a person reading the summary counts
// it. "x".length and [..."x"].length differ the moment a summary holds one.
function charLength(s: string): number {
  return [...s].length;
}

// computeRenames applies the affixes to every test, in input order, and
// classifies each result. An affix the summary already carries is not added
// again, so the operation is safe to run twice over a selection that mixes
// already-renamed and new tests.
export function computeRenames(
  tests: RenameInput[],
  opts: RenameOptions,
): RenameRow[] {
  const { prefix, suffix } = opts;

  return tests.map((t): RenameRow => {
    const before = t.summary;

    if (prefix === "" && suffix === "") {
      return {
        key: t.key,
        before,
        after: before,
        state: "unchanged",
        addedPrefix: "",
        body: before,
        addedSuffix: "",
      };
    }

    const hasPrefix = prefix !== "" && before.startsWith(prefix);
    const hasSuffix = suffix !== "" && before.endsWith(suffix);

    const addPrefix = prefix !== "" && !hasPrefix ? prefix : "";
    const addSuffix = suffix !== "" && !hasSuffix ? suffix : "";

    if (addPrefix === "" && addSuffix === "") {
      const parts: string[] = [];
      if (hasPrefix) parts.push("prefix");
      if (hasSuffix) parts.push("suffix");
      return {
        key: t.key,
        before,
        after: before,
        state: "unchanged",
        reason: `already has this ${parts.join(" and ")}`,
        addedPrefix: "",
        body: before,
        addedSuffix: "",
      };
    }

    const after = addPrefix + before + addSuffix;

    if (charLength(after) > SUMMARY_MAX) {
      // A legacy or imported summary can already be over the limit. Blaming the
      // affix there would send the user to shorten the wrong thing.
      const wasAlreadyOver = charLength(before) > SUMMARY_MAX;
      return {
        key: t.key,
        before,
        after,
        state: "too-long",
        reason: wasAlreadyOver
          ? `already over ${SUMMARY_MAX} characters`
          : `over ${SUMMARY_MAX} characters`,
        addedPrefix: addPrefix,
        body: before,
        addedSuffix: addSuffix,
      };
    }

    return {
      key: t.key,
      before,
      after,
      state: "changed",
      addedPrefix: addPrefix,
      body: before,
      addedSuffix: addSuffix,
    };
  });
}

// renameCounts tallies the states for the summary line above the preview. It is
// always called with every row, never the truncated render list, so the counts
// describe the whole selection.
export function renameCounts(rows: RenameRow[]): {
  changed: number;
  unchanged: number;
  tooLong: number;
} {
  let changed = 0;
  let unchanged = 0;
  let tooLong = 0;
  for (const r of rows) {
    if (r.state === "changed") changed++;
    else if (r.state === "unchanged") unchanged++;
    else tooLong++;
  }
  return { changed, unchanged, tooLong };
}
