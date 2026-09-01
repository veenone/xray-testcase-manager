import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import "./App.css";
import {
  Health,
  SyncProfile,
  SyncProfileFull,
  CreateFolder,
  RenameFolder,
  DeleteFolder,
  ExportProfile,
  ImportProfile,
  UpdateProfileToken,
  SetShowCoverage,
  ResolveConflictOverride,
  ResolveConflictKeepRemote,
  ResolveConflictMerge,
  RecreateDeletedTest,
  DiscardPendingChange,
  DiscardAllPendingChanges,
  CommitPendingChanges,
  CommitPendingChangesByIDs,
  DeleteProfile,
  EventsOn,
  SyncTests,
  errMsg,
  isDemoUrl,
  demoVariant,
} from "./api";
import type {
  HealthInfo,
  Profile,
  SyncProgress,
  Folder,
  PendingChange,
  CommitResult,
  ConflictDecision,
} from "./api";
import { ProfileForm } from "./components/ProfileForm";
import { Modal } from "./components/Modal";
import { ProfilesModal } from "./components/ProfilesModal";
import { ConnectionsModal } from "./components/ConnectionsModal";
import { BridgeWizard } from "./components/BridgeWizard";
import { TestTable } from "./components/TestTable";
import { TestDetail } from "./components/TestDetail";
import { NewTestPanel } from "./components/NewTestPanel";
import { FolderTree } from "./components/FolderTree";
import { ContainerList } from "./components/ContainerList";
import { ComponentList } from "./components/ComponentList";
import { PendingChangesModal } from "./components/PendingChangesModal";
import { BulkReviewModal } from "./components/BulkReviewModal";
import { REVIEW_ENABLED, invalidateCapabilities, useCapabilities } from "./features";
import { clearViewState } from "./lib/viewState";
import { useProfile } from "./contexts/ProfileContext";
import { useSync } from "./contexts/SyncContext";
import { useSelection } from "./contexts/SelectionContext";
import { useNav } from "./contexts/NavContext";
import { useModal } from "./contexts/ModalContext";
import { usePendingChanges } from "./queries/pending";
import {
  useSyncState,
  useFolders,
  useComponents,
  useGroupContainers,
} from "./queries/app";
import { invalidateProfileData } from "./queries/invalidate";
import { keys } from "./queries/keys";
import { BulkEditModal } from "./components/BulkEditModal";
import { BulkTransitionModal } from "./components/BulkTransitionModal";
import { BulkAllocateModal } from "./components/BulkAllocateModal";
import { BulkMoveModal } from "./components/BulkMoveModal";
import { BulkPreconditionsModal } from "./components/BulkPreconditionsModal";
import { BulkRequirementsModal } from "./components/BulkRequirementsModal";
import { Dashboard } from "./components/Dashboard";
import { TraceabilityTabs } from "./components/TraceabilityTabs";
import { ContainersView } from "./components/ContainersView";
import { PreconditionsView } from "./components/PreconditionsView";
import { RequirementsView } from "./components/RequirementsView";
import { CoverageView } from "./components/CoverageView";
import { DuplicatesView } from "./components/DuplicatesView";
import { GapAnalysisView } from "./components/GapAnalysisView";
import { TestCallsView } from "./components/TestCallsView";
import MisspellingsView from "./components/MisspellingsView";
import { DiagnosticsModal } from "./components/DiagnosticsModal";
import { SyncHistoryModal } from "./components/SyncHistoryModal";
import { ImportTestsModal } from "./components/ImportTestsModal";
import { Menu } from "./components/Menu";
import { AboutModal } from "./components/AboutModal";
import { LiveRegion } from "./components/LiveRegion";
import { usePrompt } from "./components/usePrompt";
import { useConfirm } from "./components/useConfirm";
import { useNotice } from "./components/useNotice";
import { useTour } from "./tour/useTour";
import { TOUR_VERSION } from "./tour/steps";

function App() {
  const [health, setHealth] = useState<HealthInfo | null>(null);

  // Active-workspace state now lives in ProfileContext (spec §5.1). setTheme /
  // setDefault keep their former App-local names (chooseTheme / setDefaultFor)
  // so existing call sites are unchanged.
  const {
    profiles,
    setProfiles,
    activeId,
    setActiveId,
    defaultProfileId,
    setDefaultProfileId,
    theme,
    showCoverage,
    setShowCoverage,
    loadingProfiles,
    activeProfile,
    setTheme: chooseTheme,
    setDefault: setDefaultFor,
    reloadProfiles,
  } = useProfile();
  // Which onboarding tour version this user has already been through.
  // TOUR_VERSION means "seen"; anything lower means it is still owed.
  const [tourSeenVersion, setTourSeenVersion] = useState(TOUR_VERSION);
  const { prompt } = usePrompt();
  const { confirm } = useConfirm();
  const { notice } = useNotice();
  // The ~16 modal-visibility booleans collapse to one reducer in ModalContext
  // (spec §5.5); isOpen/openModal/closeModal replace the showX/setShowX pairs.
  const { isOpen, openModal, closeModal } = useModal();
  // editingProfile stays App-local — it's the form modal's edit target (FR-5),
  // meaningful only while the "form" modal is open.
  const [editingProfile, setEditingProfile] = useState<Profile | null>(null);

  // The sync/commit lifecycle + its mutual-exclusion invariant now live in
  // SyncContext (spec §5.2). `pulling` is the old early-release `syncing` flag
  // (Sync button); status/can* replace the syncRunningRef + committing guards.
  const {
    status: syncStatus,
    pulling,
    progress,
    syncError,
    canSync,
    canCommit,
    canSwitchProfile,
    beginSync,
    failSync,
    endSync,
    beginCommit,
    endCommit,
  } = useSync();
  const prevProfileRef = useRef<string>("");

  // View routing + browse grouping + the New Test panel now live in NavContext
  // (spec §5.4). The cross-context reset effects (clearing the sidebar + bulk
  // selection on profile / group-by change) stay in App — they coordinate Nav
  // with SelectionContext.
  const {
    view,
    setView,
    groupBy,
    setGroupBy,
    selectedFolder,
    setSelectedFolder,
    selectedContainer,
    setSelectedContainer,
    selectedComponent,
    setSelectedComponent,
    showNewTest,
    setShowNewTest,
    newTestFolder,
    setNewTestFolder,
  } = useNav();

  // Browse selection lives in SelectionContext (spec §5.3); TestTable now reads
  // the toggle actions directly from it. App keeps the value + raw setters for
  // its composite handlers (profile-change reset, applyCreatedRemap remap, the
  // bulk modals' testKeys snapshot + onComplete clears, the open detail row).
  const { selectedKey, setSelectedKey, selectedSet, setSelectedSet } =
    useSelection();
  const [detailVersion, setDetailVersion] = useState(0);

  const queryClient = useQueryClient();
  const pendingQuery = usePendingChanges(activeId);
  const pendingChanges = pendingQuery.data ?? [];
  // App-shell profile-scoped loads (Phase 4b), refreshed by invalidateProfileData.
  const syncState = useSyncState(activeId).data ?? null;
  const folders = useFolders(activeId).data ?? [];
  const groupContainers = useGroupContainers(activeId, groupBy).data ?? [];
  const components = useComponents(activeId, groupBy).data ?? [];
  const [lastCommitResult, setLastCommitResult] = useState<CommitResult | null>(
    null,
  );


  // The onboarding tour (-335). Steps target Browse-only elements and the tour
  // can be replayed from any view, so it switches to Browse before starting.
  const { start: startTour } = useTour({
    onFinish: () => setTourSeenVersion(TOUR_VERSION),
  });

  // Resizeable browse sidebar (FR-11): drag the divider to widen it for long
  // folder names / deep nesting; the width persists across sessions.
  const [sidebarWidth, setSidebarWidth] = useState<number>(() => {
    const saved = Number(localStorage.getItem("xtm.sidebarWidth"));
    return saved >= 160 && saved <= 640 ? saved : 240;
  });
  useEffect(() => {
    localStorage.setItem("xtm.sidebarWidth", String(sidebarWidth));
  }, [sidebarWidth]);

  function startSidebarResize(e: React.MouseEvent) {
    e.preventDefault();
    const startX = e.clientX;
    const startW = sidebarWidth;
    const onMove = (ev: MouseEvent) =>
      setSidebarWidth(Math.min(640, Math.max(160, startW + ev.clientX - startX)));
    const onUp = () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
    };
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
  }

  // First: check whether the backend started up cleanly. Startup can take a
  // moment (e.g. a slow first-run migration), during which Health() reports
  // ok:false with an EMPTY error because the service layer isn't wired yet.
  // Treat that (and a Health() call that throws because the runtime isn't
  // ready) as "still starting" and retry, so a slow start doesn't render a
  // false "Backend failed to start". A non-empty error, or ok:true, is
  // definitive and stops the polling.
  useEffect(() => {
    let cancelled = false;
    let tries = 0;
    const maxTries = 40; // ~10s at 250ms
    const check = () => {
      Health()
        .then((h) => {
          if (cancelled) return;
          if (!h.ok && !h.error && tries < maxTries) {
            tries++;
            setTimeout(check, 250);
            return;
          }
          setHealth(h);
        })
        .catch((e) => {
          if (cancelled) return;
          if (tries < maxTries) {
            tries++;
            setTimeout(check, 250);
            return;
          }
          setHealth({
            ok: false,
            error: `Health check itself failed: ${errMsg(e)}`,
            dbPath: "",
            logPath: "",
          });
        });
    };
    check();
    return () => {
      cancelled = true;
    };
  }, []);

  // Load profiles once the backend reports healthy. ProfileContext owns the
  // profile/settings state; App keeps only the tour version (onboarding state)
  // from the returned Settings.
  useEffect(() => {
    if (!health || !health.ok) return;
    reloadProfiles().then((s) => {
      if (s) setTourSeenVersion(s.tourSeenVersion ?? 0);
    });
  }, [health, reloadProfiles]);

  // The sync:progress and spellcheck:progress subscriptions moved into
  // SyncProvider (spec §5.2); the reducer there owns the button-release and
  // status-bar behaviour they used to drive.

  // Pending changes grouped by parent Test key — drives the dirty markers
  // in the grid and the per-field dot in the detail panel. test_step rows
  // are bucketed under their parent Test so the row dot lights up for step
  // edits too, and the detail panel can render per-step dirty markers.
  const pendingByTestKey = useMemo(() => {
    const m = new Map<string, PendingChange[]>();
    for (const p of pendingChanges) {
      let testKey: string | null = null;
      if (
        p.entityType === "test_case" ||
        p.entityType === "test_step_order" ||
        p.entityType === "precondition_set" ||
        p.entityType === "requirement_set"
      ) {
        // Test-level changes keyed by the bare Test key.
        testKey = p.entityKey;
      } else if (
        p.entityType.startsWith("test_step") ||
        p.entityType === "custom_field"
      ) {
        // test_step* and custom_field all key as "<testKey>:<suffix>" — bucket
        // them under the parent Test so the grid + detail dirty markers cover
        // step edits and custom field edits alike.
        const colon = p.entityKey.indexOf(":");
        if (colon > 0) testKey = p.entityKey.substring(0, colon);
      }
      if (!testKey) continue;
      const arr = m.get(testKey);
      if (arr) arr.push(p);
      else m.set(testKey, [p]);
    }
    return m;
  }, [pendingQuery.data]);

  // reloadPending invalidates the pending query rather than manually refetching,
  // so the ~35 call sites stay a one-liner. The query loads on mount and on
  // activeId change, so no dedicated load effect is needed.
  const reloadPending = useCallback(() => {
    if (activeId) {
      queryClient.invalidateQueries({ queryKey: keys.pending(activeId) });
    }
  }, [queryClient, activeId]);

  // refreshProfileData is the single "refresh all profile lists" signal that
  // mutation handlers call: it invalidates every profile-scoped query family so
  // the affected lists refetch (Phase 4c — the legacy refreshKey counter is gone).
  const refreshProfileData = useCallback(() => {
    invalidateProfileData(queryClient, activeId);
  }, [queryClient, activeId]);

  // afterMutation is the shared "a modal finished mutating" ritual (spec §6.2):
  // refresh the profile lists, re-seed the open detail panel, reload the pending
  // journal, optionally drop the bulk selection, and close the active modal. It
  // replaces the block copy-pasted into every bulk modal's onComplete/onCancel.
  // (The import modal and the review/requirements cancels keep their own shapes
  // — their rituals genuinely differ.)
  function afterMutation(opts?: { clearSelection?: boolean }) {
    refreshProfileData();
    setDetailVersion((v) => v + 1);
    reloadPending();
    if (opts?.clearSelection) setSelectedSet(new Set());
    closeModal();
  }

  // Refresh the sync summary and folder tree when the active profile changes
  // or a sync finishes.
  // Clear folder + container + component + row selection when the profile changes.
  // Also drop the previous profile's view session state so it isn't inherited.
  useEffect(() => {
    if (prevProfileRef.current && prevProfileRef.current !== activeId) {
      clearViewState(prevProfileRef.current);
    }
    prevProfileRef.current = activeId;
    setSelectedFolder("");
    setSelectedContainer("");
    setSelectedComponent("");
    setSelectedSet(new Set());
  }, [activeId]);

  // The group-by sidebar lists (containers / components) now load via
  // useGroupContainers / useComponents (Phase 4b). These effects keep only the
  // side effect the queries can't express: resetting the chosen container /
  // component whenever the dimension or profile changes, so the grid doesn't
  // keep filtering by a now-hidden key.
  useEffect(() => {
    setSelectedContainer("");
  }, [activeId, groupBy]);

  useEffect(() => {
    setSelectedComponent("");
  }, [activeId, groupBy]);

  // --- Test Repository folder management (FR-13.3) ---

  async function createFolder(parentPath: string) {
    if (!activeId) return;
    const name = await prompt({
      title: parentPath ? `New subfolder under ${parentPath}` : "New top-level folder",
      placeholder: "Folder name",
      submitLabel: "Create",
    });
    if (!name || !name.trim()) return;
    try {
      await CreateFolder(activeId, parentPath, name.trim());
      refreshProfileData();
      reloadPending();
    } catch (e) {
      await notice({ title: "Create folder failed", message: errMsg(e), tone: "error" });
    }
  }

  async function renameFolder(path: string, currentName: string) {
    if (!activeId) return;
    const name = await prompt({
      title: `Rename folder "${currentName}"`,
      defaultValue: currentName,
      submitLabel: "Rename",
    });
    if (name === null || !name.trim() || name.trim() === currentName) return;
    try {
      await RenameFolder(activeId, path, name.trim());
      if (selectedFolder === path || selectedFolder.startsWith(path + "/")) {
        setSelectedFolder("");
      }
      refreshProfileData();
      reloadPending();
    } catch (e) {
      await notice({ title: "Rename failed", message: errMsg(e), tone: "error" });
    }
  }

  async function deleteFolder(path: string) {
    if (!activeId) return;
    if (!(await confirm({ title: "Delete folder", message: `Delete folder "${path}"? It must be empty.`, confirmLabel: "Delete", danger: true }))) return;
    try {
      await DeleteFolder(activeId, path);
      if (selectedFolder === path) setSelectedFolder("");
      refreshProfileData();
      reloadPending();
    } catch (e) {
      await notice({ title: "Delete failed", message: errMsg(e), tone: "error" });
    }
  }

  // Shared gate for the three sync entry points (SyncContext's canSync is the
  // single source of truth now). Returns false — with a "commit in progress"
  // notice when a commit is what's blocking — if a sync may not start. A sync
  // already running is a silent no-op, matching the pre-refactor behaviour.
  async function ensureCanSync(): Promise<boolean> {
    if (!activeId) return false;
    if (!canSync) {
      if (syncStatus === "committing") {
        await notice({
          title: "Commit in progress",
          message:
            "A commit is still running. Please wait for it to finish before syncing.",
          tone: "info",
        });
      }
      return false;
    }
    return true;
  }

  async function doSync(full: boolean) {
    if (!(await ensureCanSync())) return;
    beginSync({
      initialProgress: { phase: "", fetched: 0, total: 0, done: false },
      clearError: true,
    });
    try {
      await (full ? SyncProfileFull(activeId) : SyncProfile(activeId));
      refreshProfileData();
      setDetailVersion((v) => v + 1);
      // Start the tour on the FIRST successful sync rather than at launch:
      // before a sync the grid is empty, so half the steps would spotlight
      // elements with no data behind them and teach nothing (-335).
      if (tourSeenVersion < TOUR_VERSION) startTour("browse", () => setView("browse"));
    } catch (e) {
      failSync(errMsg(e));
    } finally {
      endSync();
    }
  }

  function runSync() {
    doSync(false);
  }

  // runFullSync forces a full re-pull, ignoring the incremental watermark, so
  // the Test Repository folder membership (skipped on routine resyncs) is
  // refreshed. It can be slow on large projects, so confirm first.
  async function runFullSync() {
    if (!(await ensureCanSync())) return;
    if (
      !(await confirm({
        title: "Full resync",
        message:
          "A full resync re-pulls every test and re-maps Test Repository folders. " +
          "It can take a while on large projects. Continue?",
        confirmLabel: "Continue",
        danger: false,
      }))
    ) {
      return;
    }
    doSync(true);
  }

  // syncTests does a targeted pull of test cases and folder membership, giving
  // the Browse view a quick refresh without running the full sync pipeline
  // (RND_P_4TFINT_05-260). Unlike doSync it shows no initial bar and leaves any
  // prior error banner in place (it reports failures via a toast instead).
  async function syncTests() {
    if (!(await ensureCanSync())) return;
    beginSync();
    try {
      await SyncTests(activeId);
      refreshProfileData();
    } catch (e) {
      await notice({ title: "Sync failed", message: errMsg(e), tone: "error" });
    } finally {
      endSync();
    }
  }

  // Native menu bar (built in main.go) drives the same actions via events. A ref
  // holds the latest handlers so a single subscription always sees current
  // state, rather than capturing stale closures.
  const menuActions = useRef<Record<string, () => void>>({});
  menuActions.current = {
    "menu:sync": runSync,
    "menu:full-sync": runFullSync,
    "menu:new-profile": () => openModal("form"),
    "menu:import": () => openModal("import"),
    "menu:view-browse": () => setView("browse"),
    "menu:view-preconditions": () => setView("preconditions"),
    "menu:view-requirements": () => setView("requirements"),
    "menu:view-dashboard": () => setView("dashboard"),
    "menu:view-traceability": () => setView("traceability"),
    "menu:view-plans": () => setView("plans"),
    "menu:view-duplicates": () => setView("duplicates"),
    "menu:view-gapanalysis": () => setView("gapanalysis"),
    "menu:view-testcalls": () => setView("testcalls"),
    "menu:view-coverage": () => setView("coverage"),
    "menu:sync-history": () => openModal("syncHistory"),
    "menu:diagnostics": () => openModal("diagnostics"),
    "menu:about": () => openModal("about"),
  };
  useEffect(() => {
    const unsubs = Object.keys(menuActions.current).map((event) =>
      EventsOn(event, () => menuActions.current[event]?.()),
    );
    return () => unsubs.forEach((u) => u && u());
  }, []);

  // toggleCoverage shows/hides the opt-in Coverage tab and persists it. Leaving
  // the Coverage view when hiding so a hidden view isn't left on screen.
  async function toggleCoverage() {
    const next = !showCoverage;
    setShowCoverage(next);
    if (!next && view === "coverage") setView("browse");
    try {
      await SetShowCoverage(next);
    } catch (e) {
      console.error("set show coverage:", errMsg(e));
    }
  }

  // exportProfile writes a profile's config (no credential) to a file the user
  // picks (FR-5.5).
  async function exportProfile(id: string) {
    if (!id) return;
    try {
      const path = await ExportProfile(id);
      if (path) await notice({ title: "Profile exported", message: path });
    } catch (e) {
      await notice({ title: "Export failed", message: errMsg(e), tone: "error" });
    }
  }

  // importProfile creates a profile from a chosen config file, then prompts for
  // its PAT (the credential isn't part of the exported file) (FR-5.5). Returns
  // the created profile, or null if cancelled.
  async function importProfile(): Promise<Profile | null> {
    try {
      const p = await ImportProfile();
      if (!p.id) return null; // cancelled
      setProfiles((prev) => [...prev, p]);
      setActiveId(p.id);
      setSelectedKey(null);
      const token = await prompt({
        title: `Enter the Personal Access Token for "${p.name}"`,
        placeholder: "Paste token (or cancel to set it later)",
        password: true,
        submitLabel: "Save token",
      });
      if (token && token.trim()) {
        await UpdateProfileToken(p.id, token.trim());
      }
      return p;
    } catch (e) {
      await notice({ title: "Import failed", message: errMsg(e), tone: "error" });
      return null;
    }
  }

  // deleteProfile removes a profile (its token + cached data are purged by the
  // backend). The Manage Profiles modal confirms first. If the active profile is
  // deleted, switches to the default (if still set) or the first remaining
  // profile. (FR-5.3)
  async function deleteProfile(id: string) {
    if (!profiles.some((p) => p.id === id)) return;
    try {
      await DeleteProfile(id);
    } catch (e) {
      await notice({ title: "Delete failed", message: errMsg(e), tone: "error" });
      return;
    }
    const remaining = profiles.filter((p) => p.id !== id);
    setProfiles(remaining);
    if (defaultProfileId === id) setDefaultProfileId("");
    if (activeId === id) {
      const next =
        defaultProfileId && defaultProfileId !== id
          ? defaultProfileId
          : remaining[0]?.id ?? "";
      setActiveId(next);
      setSelectedKey(null);
      refreshProfileData();
      reloadPending();
    }
    if (remaining.length === 0) closeModal();
  }

  // handleCreated handles both a newly-created profile and an edited one: it
  // replaces the existing entry when the id is already known, otherwise appends.
  // After an edit, the cached data may have been cleared (project/URL change),
  // so the views are refreshed. Also drops any cached capabilities for this
  // id -- an edit may have flipped the profile's Backend (Xray<->Kiwi), and
  // without this the UI would keep gating on the old backend's capabilities
  // until an app restart (useCapabilities is keyed on profileId, which is
  // unchanged across an edit).
  function handleCreated(p: Profile) {
    invalidateCapabilities(p.id);
    setProfiles((prev) =>
      prev.some((x) => x.id === p.id)
        ? prev.map((x) => (x.id === p.id ? p : x))
        : [...prev, p],
    );
    setActiveId(p.id);
    // handleCreated is wired to both the form modal (onCreated) and the
    // Profiles manager (onSaved). Close only the form — the old code did
    // setShowForm(false), a no-op when the manager (or nothing) was open, so
    // saving from the manager must leave it open.
    if (isOpen("form")) closeModal();
    setEditingProfile(null);
    setSelectedKey(null);
    refreshProfileData();
    setDetailVersion((v) => v + 1);
    reloadPending();
  }

  // Called by TestDetail after a successful inline edit. Refreshes the
  // grid (so it shows the new value) and the pending list. Deliberately
  // does NOT bump detailVersion — TestDetail already has the new value in
  // its own local state, and re-fetching mid-edit would risk clobbering
  // a field the user is still typing in.
  function handleEdited() {
    refreshProfileData();
    reloadPending();
  }

  // openNewTest launches the create panel, pre-filling a folder when one is
  // passed (per-folder action) or when a folder is the active browse filter.
  function openNewTest(folderId: string) {
    setSelectedKey(null);
    setNewTestFolder(folderId);
    setShowNewTest(true);
  }

  // handleTestCreated closes the panel, refreshes pending + list, and opens the
  // freshly-created (still uncommitted) Test in the detail panel.
  function handleTestCreated(tempKey: string) {
    setShowNewTest(false);
    setSelectedKey(tempKey);
    handleEdited(); // refreshes the pending list and the table
  }

  // Called when the user discards a pending change from the modal. The
  // backend reverts test_case to before_val, so the detail panel needs to
  // re-fetch too.
  async function handleDiscard(id: number) {
    if (!activeId) return;
    try {
      await DiscardPendingChange(activeId, id);
      refreshProfileData();
      setDetailVersion((v) => v + 1);
      reloadPending();
    } catch (e) {
      console.error("discard:", errMsg(e));
    }
  }

  // handleDiscardAll reverts every pending change at once (the modal's
  // "Discard all" action). The modal confirms first.
  async function handleDiscardAll() {
    if (!activeId) return;
    try {
      await DiscardAllPendingChanges(activeId);
      refreshProfileData();
      setDetailVersion((v) => v + 1);
      reloadPending();
    } catch (e) {
      console.error("discard all:", errMsg(e));
    }
  }

  // applyCreatedRemap re-points an open detail view from a just-committed
  // "NEW-N" placeholder to the real Jira key the backend assigned (FR-1), so the
  // panel shows the actual ticket instead of a key that no longer exists.
  function applyCreatedRemap(result: CommitResult) {
    const created = result.created ?? [];
    if (created.length === 0) return;
    setSelectedKey((cur) => {
      if (!cur) return cur;
      const match = created.find((c) => c.tempKey === cur);
      return match ? match.key : cur;
    });
  }

  // Shared gate for the two commit entry points. Returns false — with a "sync
  // in progress" notice when a sync is what's blocking — if a commit may not
  // start. A commit already running is a silent no-op.
  async function ensureCanCommit(): Promise<boolean> {
    if (!activeId) return false;
    if (!canCommit) {
      if (syncStatus === "syncing") {
        await notice({
          title: "Sync in progress",
          message:
            "A sync is still running. Please wait for it to finish before committing.",
          tone: "info",
        });
      }
      return false;
    }
    return true;
  }

  // Called when the user clicks "Commit" in the pending modal. Pushes all
  // pending changes to Jira; per-Test results land in lastCommitResult.
  // Committed pending rows are deleted by the backend; failures stay.
  async function handleCommit() {
    if (!(await ensureCanCommit())) return;
    beginCommit();
    setLastCommitResult(null);
    try {
      const result = await CommitPendingChanges(activeId);
      setLastCommitResult(result);
      applyCreatedRemap(result);
      refreshProfileData();
      setDetailVersion((v) => v + 1);
      reloadPending();
    } catch (e) {
      setLastCommitResult({
        succeeded: [],
        conflicted: [],
        failed: [{ testKey: "", error: errMsg(e) }],
      });
    } finally {
      endCommit();
    }
  }

  // handleCommitIds commits a selected subset of pending changes (selective
  // commit) — the per-item Commit button in the modal. Same result handling as
  // a full commit; only the chosen item leaves the list on success.
  async function handleCommitIds(ids: number[]) {
    if (ids.length === 0) return;
    if (!(await ensureCanCommit())) return;
    beginCommit();
    setLastCommitResult(null);
    try {
      const result = await CommitPendingChangesByIDs(activeId, ids);
      setLastCommitResult(result);
      applyCreatedRemap(result);
      refreshProfileData();
      setDetailVersion((v) => v + 1);
      reloadPending();
    } catch (e) {
      setLastCommitResult({
        succeeded: [],
        conflicted: [],
        failed: [{ testKey: "", error: errMsg(e) }],
      });
    } finally {
      endCommit();
    }
  }

  // resolveConflictOverride re-bases a conflicting Test onto the remote version
  // (keep mine) and immediately re-commits so the override takes effect.
  async function resolveConflictOverride(testKey: string, remoteVersion: string) {
    if (!activeId) return;
    try {
      await ResolveConflictOverride(activeId, testKey, remoteVersion);
    } catch (e) {
      setLastCommitResult({
        succeeded: [],
        conflicted: [],
        failed: [{ testKey, error: errMsg(e) }],
      });
      return;
    }
    await handleCommit();
  }

  // resolveConflictKeepRemote discards a conflicting Test's local edits (keep
  // remote) and refreshes.
  async function resolveConflictKeepRemote(testKey: string) {
    if (!activeId) return;
    try {
      await ResolveConflictKeepRemote(activeId, testKey);
      setLastCommitResult((prev) =>
        prev
          ? {
              ...prev,
              conflicted: prev.conflicted.filter((c) => c.testKey !== testKey),
            }
          : prev,
      );
      refreshProfileData();
      setDetailVersion((v) => v + 1);
      reloadPending();
    } catch (e) {
      console.error("keep remote:", errMsg(e));
    }
  }

  // resolveConflictMerge applies per-field keep-mine / keep-theirs decisions for
  // a conflicting Test, then re-commits so the merge takes effect.
  async function resolveConflictMerge(
    testKey: string,
    remoteVersion: string,
    decisions: ConflictDecision[],
  ) {
    if (!activeId) return;
    try {
      await ResolveConflictMerge(activeId, testKey, remoteVersion, decisions);
    } catch (e) {
      setLastCommitResult({
        succeeded: [],
        conflicted: [],
        failed: [{ testKey, error: errMsg(e) }],
      });
      return;
    }
    await handleCommit();
  }

  // resolveConflictRecreate turns a remotely-deleted Test's held edits into a
  // brand-new local Test, then drops it from the conflict list.
  async function resolveConflictRecreate(testKey: string) {
    if (!activeId) return;
    try {
      await RecreateDeletedTest(activeId, testKey);
      setLastCommitResult((prev) =>
        prev
          ? {
              ...prev,
              conflicted: prev.conflicted.filter((c) => c.testKey !== testKey),
            }
          : prev,
      );
      refreshProfileData();
      setDetailVersion((v) => v + 1);
      reloadPending();
    } catch (e) {
      console.error("recreate deleted test:", errMsg(e));
    }
  }

  function closePendingModal() {
    closeModal();
    setLastCommitResult(null);
  }

  const isDemo = isDemoUrl(activeProfile?.jiraUrl);
  const demoVar = demoVariant(activeProfile?.jiraUrl);
  // Gates the Xray-shaped UI below to what the active profile's backend
  // actually supports (Kiwi, etc.) — defaultCapabilities (all true) while
  // loading, so an Xray profile is never affected (P6.2a).
  const caps = useCapabilities(activeId);

  // A sync is in flight when the main Sync is running (syncing) OR any partial
  // per-view refresh is emitting progress. Both a full pull and a partial sync
  // write to the store keyed by the active profile, so switching profiles while
  // either runs would race the in-flight writes and land stale progress events
  // on the newly-selected profile. SyncContext.canSwitchProfile is the single
  // source of truth for locking the profile switcher (false during a sync + its
  // tail, and during a spellcheck scan; a commit does not lock it).
  const syncActive = !canSwitchProfile;

  if (!health) {
    return <div className="centered muted">Loading…</div>;
  }

  if (!health.ok) {
    return (
      <div className="centered">
        <div className="onboard">
          <h2>Backend failed to start</h2>
          <pre className="backend-error">
            {health.error || "(no error message reported)"}
          </pre>
          {health.dbPath && (
            <p className="muted">
              Database path: <code>{health.dbPath}</code>
            </p>
          )}
          {health.logPath && (
            <p className="muted">
              Full log: <code>{health.logPath}</code>
            </p>
          )}
          <p className="muted">
            Try removing the database file and relaunching. If that doesn't
            help, check the log for more detail.
          </p>
        </div>
      </div>
    );
  }

  if (loadingProfiles) {
    return <div className="centered muted">Loading…</div>;
  }

  if (profiles.length === 0) {
    return (
      <div className="centered">
        <div className="onboard">
          <h1>Xray Test Manager</h1>
          <p className="muted">
            Connect to your Jira Data Center project to get started.
          </p>
          <ProfileForm onCreated={handleCreated} />
        </div>
      </div>
    );
  }

  return (
    <div className="app">
      <header className="topbar">
        <div className="topbar-zone topbar-left">
          <span className="brand">Xray Test Manager</span>
          {isDemo && <span className="demo-chip">DEMO</span>}
          <select
            data-tour="profile"
            className="profile-select"
            value={activeId}
            disabled={syncActive}
            title={
              syncActive
                ? "You can't switch profiles while a sync is running"
                : "Switch active profile"
            }
            onChange={(e) => {
              // Guard against a race with an in-flight sync: the sync writes to
              // the store keyed by the old profile, so switching mid-sync would
              // point the UI at a new profile while stale progress events and
              // reloads are still landing. The disabled attribute prevents it;
              // this early return is belt-and-braces for menu/keyboard paths.
              if (syncActive) return;
              setActiveId(e.target.value);
              setSelectedKey(null);
            }}
          >
            {profiles.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name} ({p.projectKey})
              </option>
            ))}
          </select>
          <Menu
            label="⚙ Manage"
            title="Manage profiles and connections"
            items={[
              {
                key: "profiles",
                label: "Manage Profiles…",
                onClick: () => openModal("profiles"),
                title: "Manage profiles: add, edit, set default, export, or delete",
              },
              {
                key: "connections",
                label: "Connections…",
                onClick: () => openModal("connections"),
                title:
                  "Manage this workspace's connections. Add a target " +
                  "(e.g. Kiwi) alongside its primary connection.",
              },
              {
                key: "bridge",
                label: "Bridge…",
                onClick: () => openModal("bridge"),
                title:
                  "Publish or migrate this workspace's tests from one connection to another",
              },
            ]}
          />
        </div>

        <nav data-tour="views" className="view-tabs topbar-zone topbar-center">
          <button
            data-tour="tab-browse"
            className={`view-tab${view === "browse" ? " view-tab-active" : ""}`}
            onClick={() => setView("browse")}
          >
            Browse
          </button>
          {caps.supportsPreconditionObjects && (
            <button
              data-tour="tab-preconditions"
              className={`view-tab${view === "preconditions" ? " view-tab-active" : ""}`}
              onClick={() => setView("preconditions")}
            >
              Preconditions
            </button>
          )}
          {caps.supportsRequirementObjects && (
            <button
              data-tour="tab-requirements"
              className={`view-tab${view === "requirements" ? " view-tab-active" : ""}`}
              onClick={() => setView("requirements")}
            >
              Requirements
            </button>
          )}
          <button
            data-tour="tab-duplicates"
            className={`view-tab${view === "duplicates" ? " view-tab-active" : ""}`}
            onClick={() => setView("duplicates")}
          >
            Duplicates
          </button>
          <button
            data-tour="tab-gapanalysis"
            className={`view-tab${view === "gapanalysis" ? " view-tab-active" : ""}`}
            onClick={() => setView("gapanalysis")}
          >
            Gap Analysis
          </button>
          <button
            data-tour="tab-testcalls"
            className={`view-tab${view === "testcalls" ? " view-tab-active" : ""}`}
            onClick={() => setView("testcalls")}
          >
            Test Calls
          </button>
          <button
            data-tour="tab-dashboard"
            className={`view-tab${view === "dashboard" ? " view-tab-active" : ""}`}
            onClick={() => setView("dashboard")}
          >
            Dashboard
          </button>
          <button
            data-tour="tab-traceability"
            className={`view-tab${view === "traceability" ? " view-tab-active" : ""}`}
            onClick={() => setView("traceability")}
          >
            Traceability
          </button>
          <button
            data-tour="tab-plans"
            className={`view-tab${view === "plans" ? " view-tab-active" : ""}`}
            onClick={() => setView("plans")}
          >
            Containers
          </button>
          {showCoverage && (
            <button
              data-tour="tab-coverage"
              className={`view-tab${view === "coverage" ? " view-tab-active" : ""}`}
              onClick={() => setView("coverage")}
            >
              Coverage
            </button>
          )}
          <button
            data-tour="tab-misspellings"
            className={`view-tab${view === "misspellings" ? " view-tab-active" : ""}`}
            onClick={() => setView("misspellings")}
          >
            Spellcheck
          </button>
        </nav>

        <div data-tour="pending" className="topbar-zone topbar-right">
          {pendingChanges.length > 0 && (
            <button
              className="btn-pending"
              onClick={() => openModal("pending")}
              title="Show uncommitted edits"
            >
              <span className="pending-dot" aria-hidden="true">
                ●
              </span>
              {pendingChanges.length} pending
            </button>
          )}

          {view === "browse" && (
            <button
              className="btn btn-primary"
              onClick={() =>
                openNewTest(groupBy === "folder" ? selectedFolder : "")
              }
              title="Create a new test case"
            >
              ＋ New Test
            </button>
          )}

          {/* Wrapper carries the tour target: Menu renders its own
              button internally, so the attribute cannot go on it. */}
          <span data-tour="more">
            <Menu
              label="More"
              align="right"
              title="Tools & diagnostics"
              items={[
                {
                  key: "importtests",
                  label: "Import tests…",
                  onClick: () => openModal("import"),
                  title: "Import tests from a CSV or XLSX file",
                },
                {
                  key: "fullsync",
                  label: "Full resync (re-pull folders)",
                  onClick: runFullSync,
                  title:
                    "Force a full re-sync, ignoring the incremental watermark. " +
                    "This re-maps Test Repository folder membership.",
                },
                {
                  key: "history",
                  label: "Sync history",
                  onClick: () => openModal("syncHistory"),
                },
                // Help group. The tour sat between the sync actions and
                // Diagnostics with nothing separating it, which made it easy
                // to miss in an eleven-item menu.
                { key: "help-div", divider: true },
                {
                  key: "tour",
                  label: "🧭 Take the tour",
                  onClick: () => startTour(view),
                  title: "Replay this view's walkthrough",
                },
                {
                  key: "diag",
                  label: "Diagnostics",
                  onClick: () => openModal("diagnostics"),
                  title: "Logs & diagnostics",
                },
                { key: "td", divider: true },
                {
                  key: "t-light",
                  label: "Theme: Light",
                  checked: theme === "light",
                  onClick: () => chooseTheme("light"),
                },
                {
                  key: "t-dark",
                  label: "Theme: Dark",
                  checked: theme === "dark",
                  onClick: () => chooseTheme("dark"),
                },
                {
                  key: "t-system",
                  label: "Theme: System",
                  checked: theme === "system",
                  onClick: () => chooseTheme("system"),
                },
                { key: "cov-div", divider: true },
                {
                  key: "show-coverage",
                  label: "Show Coverage tab",
                  checked: showCoverage,
                  onClick: () => void toggleCoverage(),
                  title:
                    "Reveal the Coverage module tab (opt-in; hidden by default)",
                },
              ]}
            />
          </span>

          <button
            data-tour="sync"
            className="btn btn-primary"
            onClick={runSync}
            disabled={pulling}
          >
            {pulling ? "Syncing…" : "Sync"}
          </button>
        </div>
      </header>

      {view === "browse" && selectedSet.size > 0 && (
        <div className="bulk-toolbar">
          <span className="bulk-count">{selectedSet.size} selected</span>
          <button
            className="btn btn-primary"
            onClick={() => openModal("bulkEdit")}
          >
            Bulk edit…
          </button>
          {caps.supportsWorkflowTransitions && (
            <button
              className="btn btn-primary"
              onClick={() => openModal("bulkTransition")}
            >
              Bulk transition…
            </button>
          )}
          <button
            className="btn btn-primary"
            onClick={() => openModal("bulkAllocate")}
          >
            Allocate…
          </button>
          {caps.supportsFolders && folders.length > 0 && (
            <button
              className="btn btn-primary"
              onClick={() => openModal("bulkMove")}
            >
              Move to folder…
            </button>
          )}
          {caps.supportsPreconditionObjects && (
            <button
              className="btn btn-primary"
              onClick={() => openModal("bulkPreconditions")}
            >
              Preconditions…
            </button>
          )}
          {caps.supportsRequirementObjects && (
            <button
              className="btn btn-primary"
              onClick={() => openModal("bulkRequirements")}
            >
              Requirements…
            </button>
          )}
          {REVIEW_ENABLED && (
            <button
              className="btn btn-primary"
              onClick={() => openModal("bulkReview")}
            >
              Review…
            </button>
          )}
          <button className="btn" onClick={() => setSelectedSet(new Set())}>
            Clear
          </button>
        </div>
      )}

      {view === "preconditions" && caps.supportsPreconditionObjects ? (
        <main className="content content-preconditions">
          <PreconditionsView
            onChanged={() => {
              refreshProfileData();
              reloadPending();
            }}
          />
        </main>
      ) : view === "requirements" && caps.supportsRequirementObjects ? (
        <main className="content content-requirements">
          <RequirementsView
            onChanged={() => {
              refreshProfileData();
              reloadPending();
            }}
          />
        </main>
      ) : view === "duplicates" ? (
        <main className="content content-dashboard">
          <DuplicatesView
            folders={folders}
            pendingByTestKey={pendingByTestKey}
            onChanged={() => {
              refreshProfileData();
              reloadPending();
            }}
          />
        </main>
      ) : view === "gapanalysis" ? (
        <main className="content content-gapanalysis">
          <GapAnalysisView
            onChanged={() => {
              refreshProfileData();
              reloadPending();
            }}
          />
        </main>
      ) : view === "testcalls" ? (
        <main className="content content-dashboard">
          <TestCallsView
            onChanged={() => {
              refreshProfileData();
              reloadPending();
            }}
          />
        </main>
      ) : view === "dashboard" ? (
        <main className="content content-dashboard">
          <Dashboard onOpenDuplicates={() => setView("duplicates")} />
        </main>
      ) : view === "traceability" ? (
        <main className="content content-dashboard">
          <TraceabilityTabs
            jiraUrl={activeProfile?.jiraUrl ?? ""}
          />
        </main>
      ) : view === "plans" ? (
        <main className="content content-containers">
          <ContainersView
            isDemo={isDemo}
            jiraUrl={activeProfile?.jiraUrl ?? ""}
            onOpenTest={(k) => {
              setSelectedKey(k);
              setView("browse");
            }}
            onChanged={() => {
              refreshProfileData();
              reloadPending();
            }}
          />
        </main>
      ) : view === "coverage" ? (
        <main className="content content-coverage">
          <CoverageView
            isDemo={isDemo}
            demoVariant={demoVar}
            onChanged={() => {
              refreshProfileData();
              reloadPending();
            }}
          />
        </main>
      ) : view === "misspellings" ? (
        <main className="content">
          <MisspellingsView
            onChanged={() => {
              refreshProfileData();
              reloadPending();
            }}
          />
        </main>
      ) : (
        <main className="content">
          <div className="browse-sidebar" style={{ width: sidebarWidth }}>
            <select
              className="groupby-select"
              value={groupBy}
              onChange={(e) => {
                setGroupBy(
                  e.target.value as
                    | "folder"
                    | "testset"
                    | "testplan"
                    | "component",
                );
                setSelectedFolder("");
                setSelectedKey(null);
              }}
            >
              {caps.supportsFolders && (
                <option value="folder">Group by: Folder</option>
              )}
              <option value="testset">Group by: Test Set</option>
              <option value="testplan">Group by: Test Plan</option>
              <option value="component">Group by: Component</option>
            </select>
            {groupBy === "component" ? (
              <ComponentList
                components={components}
                selected={selectedComponent}
                emptyLabel="No components synced."
                onSelect={(name) => {
                  setSelectedComponent(name);
                  setSelectedKey(null);
                }}
              />
            ) : groupBy === "folder" ? (
              !caps.supportsFolders ? (
                <div className="browse-sidebar-empty">
                  <p className="muted">
                    Folders are not supported by this backend.
                  </p>
                </div>
              ) : folders.length > 0 ? (
                <FolderTree
                  folders={folders}
                  selected={selectedFolder}
                  onSelect={(id) => {
                    setSelectedFolder(id);
                    setSelectedKey(null);
                  }}
                  onCreate={createFolder}
                  onRename={renameFolder}
                  onDelete={deleteFolder}
                  onNewTest={(folderId) => openNewTest(folderId)}
                />
              ) : (
                <div className="browse-sidebar-empty">
                  <p className="muted">No folders synced.</p>
                  <button
                    className="link-btn"
                    onClick={() => createFolder("")}
                  >
                    ＋ New folder
                  </button>
                </div>
              )
            ) : (
              <ContainerList
                containers={groupContainers}
                selected={selectedContainer}
                emptyLabel={
                  groupBy === "testset"
                    ? "No Test Sets synced."
                    : "No Test Plans synced."
                }
                onSelect={(key) => {
                  setSelectedContainer(key);
                  setSelectedKey(null);
                }}
              />
            )}
          </div>
          <div
            className="sidebar-resizer"
            onMouseDown={startSidebarResize}
            title="Drag to resize the sidebar"
          />
          <TestTable
            folderId={groupBy === "folder" ? selectedFolder : ""}
            containerKey={
              groupBy === "testset" || groupBy === "testplan"
                ? selectedContainer
                : ""
            }
            component={groupBy === "component" ? selectedComponent : ""}
            pendingByTestKey={pendingByTestKey}
            onSync={syncTests}
            syncing={pulling}
          />
          {showNewTest ? (
            <NewTestPanel
              folders={folders}
              initialFolderId={newTestFolder}
              onCreated={handleTestCreated}
              onCancel={() => setShowNewTest(false)}
            />
          ) : (
            selectedKey && (
              <TestDetail
                testKey={selectedKey}
                version={detailVersion}
                pendingForTest={pendingByTestKey.get(selectedKey) ?? []}
                folders={folders}
                jiraUrl={activeProfile?.jiraUrl ?? ""}
                onClose={() => setSelectedKey(null)}
                onEdited={handleEdited}
                onCloned={handleTestCreated}
              />
            )
          )}
        </main>
      )}

      {isOpen("form") && (
        <Modal
          onClose={() => {
            closeModal();
            setEditingProfile(null);
          }}
          className="modal"
          label="Profile"
        >
            <ProfileForm
              profile={editingProfile ?? undefined}
              profiles={profiles}
              onCreated={handleCreated}
              onCancel={() => {
                closeModal();
                setEditingProfile(null);
              }}
            />
        </Modal>
      )}

      {isOpen("profiles") && (
        <ProfilesModal
          profiles={profiles}
          activeId={activeId}
          defaultProfileId={defaultProfileId}
          onClose={() => closeModal()}
          onSetDefault={setDefaultFor}
          onExport={exportProfile}
          onImport={importProfile}
          onSaved={handleCreated}
          onDelete={deleteProfile}
        />
      )}

      {isOpen("connections") && activeId && (
        <ConnectionsModal
          activeId={activeId}
          onClose={() => closeModal()}
        />
      )}

      {isOpen("bridge") && activeId && (
        <BridgeWizard
          activeId={activeId}
          onClose={() => closeModal()}
          onOpenConnections={() => {
            closeModal();
            openModal("connections");
          }}
        />
      )}

      {isOpen("pending") && (
        <PendingChangesModal
          changes={pendingChanges}
          onDiscard={handleDiscard}
          onDiscardAll={handleDiscardAll}
          onCommit={handleCommit}
          onCommitIds={handleCommitIds}
          onJumpTo={(key) => {
            setSelectedKey(key);
            closePendingModal();
          }}
          onResolveOverride={resolveConflictOverride}
          onResolveKeepRemote={resolveConflictKeepRemote}
          onResolveMerge={resolveConflictMerge}
          onResolveRecreate={resolveConflictRecreate}
          onClose={closePendingModal}
          committing={syncStatus === "committing"}
          lastResult={lastCommitResult}
        />
      )}

      {isOpen("bulkEdit") && (
        <BulkEditModal
          testKeys={[...selectedSet]}
          onComplete={() => afterMutation({ clearSelection: true })}
          onCancel={() => afterMutation()}
        />
      )}

      {isOpen("bulkTransition") && (
        <BulkTransitionModal
          testKeys={[...selectedSet]}
          onComplete={() => afterMutation({ clearSelection: true })}
          onCancel={() => afterMutation()}
        />
      )}

      {isOpen("bulkAllocate") && (
        <BulkAllocateModal
          testKeys={[...selectedSet]}
          onComplete={() => afterMutation({ clearSelection: true })}
          onCancel={() => afterMutation()}
        />
      )}

      {isOpen("bulkMove") && (
        <BulkMoveModal
          testKeys={[...selectedSet]}
          folders={folders}
          onComplete={() => afterMutation({ clearSelection: true })}
          onCancel={() => afterMutation()}
        />
      )}

      {isOpen("diagnostics") && (
        <DiagnosticsModal onClose={() => closeModal()} />
      )}

      {isOpen("about") && (
        <AboutModal
          onClose={() => closeModal()}
          onTakeTour={() => startTour(view)}
        />
      )}

      {isOpen("syncHistory") && (
        <SyncHistoryModal
          onClose={() => closeModal()}
        />
      )}

      {isOpen("import") && (
        <ImportTestsModal
          onComplete={() => {
            refreshProfileData();
            reloadPending();
            closeModal();
          }}
          onCancel={() => closeModal()}
        />
      )}

      {isOpen("bulkPreconditions") && (
        <BulkPreconditionsModal
          testKeys={[...selectedSet]}
          onComplete={() => afterMutation({ clearSelection: true })}
          onCancel={() => afterMutation()}
        />
      )}

      {isOpen("bulkRequirements") && (
        <BulkRequirementsModal
          testKeys={[...selectedSet]}
          onComplete={() => afterMutation({ clearSelection: true })}
          onCancel={() => closeModal()}
        />
      )}

      {REVIEW_ENABLED && isOpen("bulkReview") && (
        <BulkReviewModal
          testKeys={[...selectedSet]}
          onComplete={() => afterMutation({ clearSelection: true })}
          onCancel={() => closeModal()}
        />
      )}

      <footer className="app-statusbar">
        {progress && !progress.done ? (
          <SyncBar progress={progress} />
        ) : syncError ? (
          <span className="error-text">Sync failed: {syncError}</span>
        ) : syncState ? (
          <span className="muted sync-info">
            {syncState.testCount.toLocaleString()} tests
            {syncState.lastSyncedAt
              ? ` · last synced ${new Date(
                  syncState.lastSyncedAt,
                ).toLocaleString()}`
              : " · never synced"}
          </span>
        ) : (
          <span className="muted sync-info">&nbsp;</span>
        )}
      </footer>

      <LiveRegion />
    </div>
  );
}

function SyncBar({ progress }: { progress: SyncProgress }) {
  const hasCount = progress.total > 0;
  const pct = hasCount
    ? Math.round((progress.fetched / progress.total) * 100)
    : 0;
  // Prefer the explicit stage label; fall back to a phase-derived label.
  const stage =
    progress.stage || (progress.phase === "folders" ? "Folders" : "Syncing");
  return (
    <div className="syncbar">
      {hasCount && (
        <div className="syncbar-track">
          <div className="syncbar-fill" style={{ width: `${pct}%` }} />
        </div>
      )}
      <span className="muted">
        {stage}
        {hasCount
          ? `: ${progress.fetched.toLocaleString()} / ${progress.total.toLocaleString()}`
          : "…"}
      </span>
    </div>
  );
}

export default App;
