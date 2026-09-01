import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useReducer,
} from "react";
import type { ReactNode } from "react";
import { EventsOn } from "../api";
import type { SyncProgress } from "../api";
import {
  syncReducer,
  initialSyncState,
  canSync as canSyncSel,
  canCommit as canCommitSel,
  canSwitchProfile as canSwitchProfileSel,
} from "./syncMachine";
import type { SyncStatus } from "./syncMachine";

// SyncContext centralises the sync/commit lifecycle that App.tsx previously
// tracked with a `syncing` bool + a `syncRunningRef` + a `committing` bool, and
// enforced with five hand-rolled guards (spec §5.2 / audit A8). The pure
// reducer (syncMachine.ts) owns the transitions and the mutual-exclusion
// invariant; this provider owns the two progress-event subscriptions and
// exposes the state, the can* selectors, and thin lifecycle dispatchers.
//
// The async orchestration (calling SyncProfile/CommitPendingChanges and the
// post-run side effects — query invalidation, tour, detail re-seed, pending
// reload) stays in App: it is coupled to App-owned concerns, so pulling it in
// here would trade one coupling for another. What moves is the *invariant* —
// App now gates on canSync/canCommit/canSwitchProfile instead of re-deriving it.
//
// Note: lastCommitResult (the commit-result banner) deliberately stays in App
// state — it has its own lifecycle across the conflict resolvers, unrelated to
// the sync/commit mutual-exclusion this machine models.

interface SyncApi {
  status: SyncStatus;
  // The Sync button's disabled flag (the old early-release `syncing`).
  pulling: boolean;
  progress: SyncProgress | null;
  syncError: string;
  canSync: boolean;
  canCommit: boolean;
  canSwitchProfile: boolean;
  // Lifecycle dispatchers — App calls these around its own async orchestration.
  // doSync passes clearError; syncTests does not (see syncMachine).
  beginSync: (opts?: {
    initialProgress?: SyncProgress;
    clearError?: boolean;
  }) => void;
  failSync: (message: string) => void;
  endSync: () => void;
  beginCommit: () => void;
  endCommit: () => void;
}

const SyncContext = createContext<SyncApi | null>(null);

export function useSync(): SyncApi {
  const ctx = useContext(SyncContext);
  if (!ctx) {
    throw new Error("useSync must be used within a SyncProvider");
  }
  return ctx;
}

export function SyncProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(syncReducer, initialSyncState);

  // The sync engine emits progress for the WHOLE sync (tests + folders +
  // preconditions + containers + custom fields); the terminal frame (done:true)
  // releases the button while the tail keeps running (handled in the reducer).
  useEffect(
    () =>
      EventsOn("sync:progress", (p: SyncProgress) =>
        dispatch({ type: "SYNC_PROGRESS", progress: p }),
      ),
    [],
  );

  // Spellcheck shares the bottom status bar but never the sync lifecycle.
  useEffect(
    () =>
      EventsOn("spellcheck:progress", (p: SyncProgress) =>
        dispatch({ type: "SPELLCHECK_PROGRESS", progress: p }),
      ),
    [],
  );

  const beginSync = useCallback(
    (opts?: { initialProgress?: SyncProgress; clearError?: boolean }) =>
      dispatch({
        type: "SYNC_START",
        initialProgress: opts?.initialProgress,
        clearError: opts?.clearError,
      }),
    [],
  );
  const failSync = useCallback(
    (message: string) => dispatch({ type: "SYNC_ERROR", message }),
    [],
  );
  const endSync = useCallback(() => dispatch({ type: "SYNC_END" }), []);
  const beginCommit = useCallback(() => dispatch({ type: "COMMIT_START" }), []);
  const endCommit = useCallback(() => dispatch({ type: "COMMIT_END" }), []);

  const api = useMemo<SyncApi>(
    () => ({
      status: state.status,
      pulling: state.pulling,
      progress: state.progress,
      syncError: state.syncError,
      canSync: canSyncSel(state),
      canCommit: canCommitSel(state),
      canSwitchProfile: canSwitchProfileSel(state),
      beginSync,
      failSync,
      endSync,
      beginCommit,
      endCommit,
    }),
    [state, beginSync, failSync, endSync, beginCommit, endCommit],
  );

  return <SyncContext.Provider value={api}>{children}</SyncContext.Provider>;
}
