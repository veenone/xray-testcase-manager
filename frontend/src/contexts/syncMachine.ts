import type { SyncProgress } from "../api";

// syncMachine — the pure reducer behind SyncContext (spec §5.2). It models the
// sync/commit lifecycle as one state machine so the mutual-exclusion invariant
// ("never sync and commit at once", "don't switch profiles mid-sync") lives in
// one place instead of the five hand-rolled guards previously scattered across
// App.tsx (a `syncing` bool + a `syncRunningRef` + a `committing` bool).
//
// This module is deliberately pure and UI-free: the provider (SyncContext) owns
// the async orchestration, dialogs, and side effects; this file owns only the
// state transitions, so they can be unit-tested exhaustively before the guards
// are removed from App.
//
// Behaviour it reproduces from the pre-refactor App.tsx (kept verbatim — this is
// a structural refactor, not a behaviour change):
//
//   status    = the whole-sync / whole-commit lock. 'syncing' spans a sync from
//               start to its finally (old `syncRunningRef`); 'committing' spans a
//               commit (old `committing`). The two can never overlap.
//   pulling   = the old early-release `syncing` bool: true from SYNC_START until
//               the terminal progress event (test pull done), then false while
//               the best-effort *tail* (folders / preconditions / containers /
//               custom fields) keeps running. Drives ONLY the Sync button's
//               disabled state, so the button re-enables for the tail while
//               `status` stays 'syncing'.
//   progress  = the status-bar progress object. Both the sync channel and the
//               spellcheck channel write it; spellcheck never changes `status`.
//
// Transition table (any action not listed for a status leaves state unchanged):
//
//   idle       SYNC_START        -> syncing   (pulling=true, progress=initial, syncError cleared iff clearError)
//   idle       COMMIT_START      -> committing
//   syncing    SYNC_PROGRESS done-> syncing   (pulling=false, progress=null)   // tail begins
//   syncing    SYNC_PROGRESS      -> syncing   (progress=p)
//   syncing    SYNC_ERROR         -> syncing   (syncError=msg)
//   syncing    SYNC_END           -> idle      (pulling=false, progress=null)
//   committing COMMIT_END         -> idle
//   *          SPELLCHECK_PROGRESS-> *         (progress only; status untouched)
//
// SYNC_START / COMMIT_START are ignored unless status==='idle' — the reducer is
// the invariant's last line of defence even though the provider's actions also
// gate on the can* selectors before dispatching.

export type SyncStatus = "idle" | "syncing" | "committing";

export interface SyncMachineState {
  status: SyncStatus;
  // Sync test-pull in progress — the Sync button's disabled flag. Released at
  // the terminal progress event while `status` stays 'syncing' for the tail.
  pulling: boolean;
  progress: SyncProgress | null;
  syncError: string;
}

export const initialSyncState: SyncMachineState = {
  status: "idle",
  pulling: false,
  progress: null,
  syncError: "",
};

export type SyncAction =
  // A sync begins. `initialProgress` mirrors the immediate bar doSync shows
  // before the first event lands; syncTests starts without one. `clearError`
  // drops any prior error banner — doSync clears it, but syncTests does not
  // (it surfaces failures via a toast and leaves an existing banner alone), so
  // the flag is opt-in to preserve that pre-refactor asymmetry.
  | { type: "SYNC_START"; initialProgress?: SyncProgress; clearError?: boolean }
  // A frame from the sync:progress channel. A frame with done:true is the
  // terminal test-pull signal (releases the button, clears the bar).
  | { type: "SYNC_PROGRESS"; progress: SyncProgress }
  // The sync's catch block — record the message, stay 'syncing' until SYNC_END.
  | { type: "SYNC_ERROR"; message: string }
  // The sync's finally — return to idle regardless of how it ended.
  | { type: "SYNC_END" }
  | { type: "COMMIT_START" }
  | { type: "COMMIT_END" }
  // A frame from the spellcheck:progress channel — shares the bar, never the
  // sync lifecycle.
  | { type: "SPELLCHECK_PROGRESS"; progress: SyncProgress };

export function syncReducer(
  state: SyncMachineState,
  action: SyncAction,
): SyncMachineState {
  switch (action.type) {
    case "SYNC_START":
      if (state.status !== "idle") return state;
      return {
        ...state,
        status: "syncing",
        pulling: true,
        progress: action.initialProgress ?? null,
        syncError: action.clearError ? "" : state.syncError,
      };

    case "SYNC_PROGRESS":
      if (state.status !== "syncing") return state;
      if (action.progress.done) {
        // Terminal test-pull frame: the button re-enables and the bar clears,
        // but the sync (its tail) is still running, so status stays 'syncing'.
        return { ...state, pulling: false, progress: null };
      }
      return { ...state, progress: action.progress };

    case "SYNC_ERROR":
      if (state.status !== "syncing") return state;
      return { ...state, syncError: action.message };

    case "SYNC_END":
      if (state.status !== "syncing") return state;
      return { ...state, status: "idle", pulling: false, progress: null };

    case "COMMIT_START":
      if (state.status !== "idle") return state;
      return { ...state, status: "committing" };

    case "COMMIT_END":
      if (state.status !== "committing") return state;
      return { ...state, status: "idle" };

    case "SPELLCHECK_PROGRESS":
      // Shares the status bar; independent of the sync/commit lifecycle.
      return {
        ...state,
        progress: action.progress.done ? null : action.progress,
      };

    default:
      return state;
  }
}

// canSync / canCommit — a sync or commit may start only from idle. They are
// identical today but kept distinct per the spec (semantic clarity; the two
// could diverge later).
export const canSync = (s: SyncMachineState): boolean => s.status === "idle";
export const canCommit = (s: SyncMachineState): boolean => s.status === "idle";

// canSwitchProfile — false while a sync is in flight (through the tail, since
// status stays 'syncing') and, preserving the pre-refactor `syncActive`, also
// while any progress bar is showing (a spellcheck scan locks the picker too).
// A commit does NOT block switching — matching the pre-refactor behaviour.
export const canSwitchProfile = (s: SyncMachineState): boolean =>
  s.status !== "syncing" && s.progress === null;
