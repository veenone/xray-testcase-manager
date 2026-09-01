import { describe, it, expect } from "vitest";
import {
  syncReducer,
  initialSyncState,
  canSync,
  canCommit,
  canSwitchProfile,
} from "./syncMachine";
import type { SyncMachineState, SyncAction } from "./syncMachine";
import type { SyncProgress } from "../api";

const frame = (over: Partial<SyncProgress> = {}): SyncProgress => ({
  phase: "tests",
  fetched: 1,
  total: 10,
  done: false,
  ...over,
});

// Build a state by replaying actions from the initial state — keeps tests
// expressed as transition sequences rather than hand-poked objects.
const reduce = (...actions: SyncAction[]): SyncMachineState =>
  actions.reduce(syncReducer, initialSyncState);

const SYNCING = reduce({ type: "SYNC_START" });
const COMMITTING = reduce({ type: "COMMIT_START" });

describe("syncReducer — SYNC lifecycle", () => {
  it("SYNC_START from idle enters syncing and raises the button flag", () => {
    const s = syncReducer(initialSyncState, {
      type: "SYNC_START",
      initialProgress: frame({ phase: "", fetched: 0, total: 0 }),
    });
    expect(s.status).toBe("syncing");
    expect(s.pulling).toBe(true);
    expect(s.progress).toEqual(frame({ phase: "", fetched: 0, total: 0 }));
    expect(s.syncError).toBe("");
  });

  it("SYNC_START without initialProgress leaves the bar empty (syncTests path)", () => {
    const s = syncReducer(initialSyncState, { type: "SYNC_START" });
    expect(s.status).toBe("syncing");
    expect(s.pulling).toBe(true);
    expect(s.progress).toBeNull();
  });

  it("SYNC_START with clearError drops a stale syncError (the doSync path)", () => {
    const failed = reduce(
      { type: "SYNC_START", clearError: true },
      { type: "SYNC_ERROR", message: "boom" },
      { type: "SYNC_END" },
    );
    expect(failed.syncError).toBe("boom");
    const restarted = syncReducer(failed, {
      type: "SYNC_START",
      clearError: true,
    });
    expect(restarted.syncError).toBe("");
  });

  it("SYNC_START without clearError keeps a prior error banner (the syncTests path)", () => {
    const failed = reduce(
      { type: "SYNC_START", clearError: true },
      { type: "SYNC_ERROR", message: "boom" },
      { type: "SYNC_END" },
    );
    const restarted = syncReducer(failed, { type: "SYNC_START" });
    expect(restarted.syncError).toBe("boom");
  });

  it("a non-terminal SYNC_PROGRESS frame updates the bar only", () => {
    const s = syncReducer(SYNCING, {
      type: "SYNC_PROGRESS",
      progress: frame({ fetched: 5 }),
    });
    expect(s.status).toBe("syncing");
    expect(s.pulling).toBe(true);
    expect(s.progress).toEqual(frame({ fetched: 5 }));
  });

  it("a terminal SYNC_PROGRESS frame releases the button and clears the bar but stays syncing (the tail)", () => {
    const s = syncReducer(SYNCING, {
      type: "SYNC_PROGRESS",
      progress: frame({ done: true }),
    });
    expect(s.status).toBe("syncing"); // tail still running
    expect(s.pulling).toBe(false); // button re-enabled
    expect(s.progress).toBeNull();
  });

  it("SYNC_ERROR records the message but stays syncing until SYNC_END", () => {
    const s = syncReducer(SYNCING, { type: "SYNC_ERROR", message: "nope" });
    expect(s.status).toBe("syncing");
    expect(s.syncError).toBe("nope");
  });

  it("a late non-terminal SYNC_PROGRESS after SYNC_ERROR still updates the bar (error persists)", () => {
    const s = reduce(
      { type: "SYNC_START", clearError: true },
      { type: "SYNC_ERROR", message: "partial failure" },
      { type: "SYNC_PROGRESS", progress: frame({ phase: "containers", fetched: 7 }) },
    );
    expect(s.status).toBe("syncing");
    expect(s.syncError).toBe("partial failure");
    expect(s.progress).toEqual(frame({ phase: "containers", fetched: 7 }));
  });

  it("SYNC_END returns to idle and clears the bar, keeping the error for display", () => {
    const s = reduce(
      { type: "SYNC_START" },
      { type: "SYNC_ERROR", message: "nope" },
      { type: "SYNC_END" },
    );
    expect(s.status).toBe("idle");
    expect(s.pulling).toBe(false);
    expect(s.progress).toBeNull();
    expect(s.syncError).toBe("nope");
  });

  it("SYNC_END from a mid-pull sync (no terminal frame, e.g. failure) still clears pulling", () => {
    const s = reduce(
      { type: "SYNC_START", initialProgress: frame() },
      { type: "SYNC_END" },
    );
    expect(s.status).toBe("idle");
    expect(s.pulling).toBe(false);
    expect(s.progress).toBeNull();
  });

  it("models a full happy-path sync end to end", () => {
    const s = reduce(
      { type: "SYNC_START", initialProgress: frame({ phase: "" }) },
      { type: "SYNC_PROGRESS", progress: frame({ phase: "folders", fetched: 3 }) },
      { type: "SYNC_PROGRESS", progress: frame({ done: true }) }, // tail begins
      { type: "SYNC_END" },
    );
    expect(s).toEqual(initialSyncState);
  });
});

describe("syncReducer — COMMIT lifecycle", () => {
  it("COMMIT_START from idle enters committing", () => {
    const s = syncReducer(initialSyncState, { type: "COMMIT_START" });
    expect(s.status).toBe("committing");
  });

  it("COMMIT_END returns to idle", () => {
    const s = syncReducer(COMMITTING, { type: "COMMIT_END" });
    expect(s.status).toBe("idle");
  });

  it("COMMIT_END does not disturb the sync display fields", () => {
    // lastCommitResult now lives in App state, not the machine; a commit must
    // not touch the sync bar/error fields either.
    const s = syncReducer(COMMITTING, { type: "COMMIT_END" });
    expect(s.progress).toBeNull();
    expect(s.syncError).toBe("");
    expect(s.pulling).toBe(false);
  });
});

describe("syncReducer — mutual exclusion invariant", () => {
  it("ignores COMMIT_START while syncing", () => {
    expect(syncReducer(SYNCING, { type: "COMMIT_START" })).toBe(SYNCING);
  });

  it("ignores SYNC_START while committing", () => {
    expect(syncReducer(COMMITTING, { type: "SYNC_START" })).toBe(COMMITTING);
  });

  it("ignores SYNC_START while already syncing", () => {
    expect(syncReducer(SYNCING, { type: "SYNC_START" })).toBe(SYNCING);
  });

  it("ignores COMMIT_START while already committing", () => {
    expect(syncReducer(COMMITTING, { type: "COMMIT_START" })).toBe(COMMITTING);
  });

  it("ignores sync-lifecycle actions when not syncing", () => {
    for (const action of [
      { type: "SYNC_PROGRESS", progress: frame() },
      { type: "SYNC_ERROR", message: "x" },
      { type: "SYNC_END" },
    ] as SyncAction[]) {
      expect(syncReducer(initialSyncState, action)).toBe(initialSyncState);
      expect(syncReducer(COMMITTING, action)).toBe(COMMITTING);
    }
  });

  it("ignores COMMIT_END when not committing", () => {
    expect(syncReducer(SYNCING, { type: "COMMIT_END" })).toBe(SYNCING);
  });
});

describe("syncReducer — spellcheck progress is lifecycle-independent", () => {
  it("updates the bar from idle without changing status", () => {
    const s = syncReducer(initialSyncState, {
      type: "SPELLCHECK_PROGRESS",
      progress: frame({ phase: "spellcheck", fetched: 2 }),
    });
    expect(s.status).toBe("idle");
    expect(s.progress).toEqual(frame({ phase: "spellcheck", fetched: 2 }));
  });

  it("a terminal spellcheck frame clears the bar", () => {
    const scanning = syncReducer(initialSyncState, {
      type: "SPELLCHECK_PROGRESS",
      progress: frame({ phase: "spellcheck" }),
    });
    const s = syncReducer(scanning, {
      type: "SPELLCHECK_PROGRESS",
      progress: frame({ done: true }),
    });
    expect(s.progress).toBeNull();
    expect(s.status).toBe("idle");
  });

  it("does not disturb an in-flight commit's status", () => {
    const s = syncReducer(COMMITTING, {
      type: "SPELLCHECK_PROGRESS",
      progress: frame({ phase: "spellcheck" }),
    });
    expect(s.status).toBe("committing");
    expect(s.progress).toEqual(frame({ phase: "spellcheck" }));
  });

  it("overwrites the sync bar when a spellcheck frame lands mid-sync (matches main: shared progress state)", () => {
    const syncingWithBar = syncReducer(SYNCING, {
      type: "SYNC_PROGRESS",
      progress: frame({ phase: "folders", fetched: 4 }),
    });
    const s = syncReducer(syncingWithBar, {
      type: "SPELLCHECK_PROGRESS",
      progress: frame({ phase: "spellcheck", fetched: 9 }),
    });
    expect(s.status).toBe("syncing"); // lifecycle untouched
    expect(s.progress).toEqual(frame({ phase: "spellcheck", fetched: 9 }));
  });
});

describe("selectors — canSync / canCommit", () => {
  it("allow start only from idle", () => {
    expect(canSync(initialSyncState)).toBe(true);
    expect(canCommit(initialSyncState)).toBe(true);
    expect(canSync(SYNCING)).toBe(false);
    expect(canCommit(SYNCING)).toBe(false);
    expect(canSync(COMMITTING)).toBe(false);
    expect(canCommit(COMMITTING)).toBe(false);
  });

  it("still forbid a new sync during the tail (button re-enabled, status syncing)", () => {
    const tail = syncReducer(SYNCING, {
      type: "SYNC_PROGRESS",
      progress: frame({ done: true }),
    });
    expect(tail.pulling).toBe(false);
    expect(canSync(tail)).toBe(false);
  });
});

describe("selectors — canSwitchProfile matrix", () => {
  const withProgress = (s: SyncMachineState): SyncMachineState => ({
    ...s,
    progress: frame({ phase: "spellcheck" }),
  });

  it("idle + no bar → allowed", () => {
    expect(canSwitchProfile(initialSyncState)).toBe(true);
  });

  it("idle + spellcheck bar → blocked (preserved quirk)", () => {
    expect(canSwitchProfile(withProgress(initialSyncState))).toBe(false);
  });

  it("syncing (bar showing) → blocked", () => {
    expect(canSwitchProfile(SYNCING)).toBe(false);
  });

  it("syncing tail (bar cleared, still syncing) → blocked", () => {
    const tail = syncReducer(SYNCING, {
      type: "SYNC_PROGRESS",
      progress: frame({ done: true }),
    });
    expect(tail.progress).toBeNull();
    expect(canSwitchProfile(tail)).toBe(false);
  });

  it("committing + no bar → allowed (commit does not block switching)", () => {
    expect(canSwitchProfile(COMMITTING)).toBe(true);
  });

  it("committing + spellcheck bar → blocked (bar present)", () => {
    expect(canSwitchProfile(withProgress(COMMITTING))).toBe(false);
  });
});
