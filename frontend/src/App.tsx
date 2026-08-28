import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import "./App.css";
import {
  Health,
  ListProfiles,
  SyncProfile,
  SyncProfileFull,
  CreateFolder,
  RenameFolder,
  DeleteFolder,
  ExportProfile,
  ImportProfile,
  UpdateProfileToken,
  GetSettings,
  SetDefaultProfile,
  SetTheme,
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

// applyTheme resolves the preference ("system" follows the OS) and sets the
// data-theme attribute the CSS tokens key off (FR-12.2).
function applyTheme(theme: string) {
  const dark =
    theme === "dark" ||
    (theme === "system" &&
      window.matchMedia?.("(prefers-color-scheme: dark)").matches);
  document.documentElement.dataset.theme = dark ? "dark" : "light";
}

function App() {
  const [health, setHealth] = useState<HealthInfo | null>(null);

  const [profiles, setProfiles] = useState<Profile[]>([]);
  const [activeId, setActiveId] = useState<string>("");
  const [defaultProfileId, setDefaultProfileId] = useState<string>("");
  const [theme, setThemeState] = useState<string>("light");
  // The Coverage module is opt-in; its top-nav tab is hidden until enabled.
  const [showCoverage, setShowCoverage] = useState(false);
  // Which onboarding tour version this user has already been through.
  // TOUR_VERSION means "seen"; anything lower means it is still owed.
  const [tourSeenVersion, setTourSeenVersion] = useState(TOUR_VERSION);
  const { prompt, promptUI } = usePrompt();
  const { confirm, confirmUI } = useConfirm();
  const { notice, noticeUI } = useNotice();
  const [loadingProfiles, setLoadingProfiles] = useState(false);
  const [showForm, setShowForm] = useState(false);
  const [showProfiles, setShowProfiles] = useState(false);
  // Connections manager for the active workspace (P6.3 B6a) — add/edit/delete
  // the connections (e.g. a Kiwi target) a workspace talks to, beyond its
  // primary one. The prerequisite UI for the bridge wizard (B6b).
  const [showConnections, setShowConnections] = useState(false);
  const [showBridge, setShowBridge] = useState(false);
  // When set, the profile modal opens in edit mode for this profile (FR-5).
  const [editingProfile, setEditingProfile] = useState<Profile | null>(null);

  const [progress, setProgress] = useState<SyncProgress | null>(null);
  const [syncError, setSyncError] = useState("");
  // syncing drives the Sync button label/disabled state; it is released as soon
  // as the Test pull finishes (testsDone) so the button doesn't look stuck while
  // the best-effort tail work runs. syncRunningRef tracks the whole backend Sync
  // call, so a concurrent sync can't be started while the tail is still going.
  const [syncing, setSyncing] = useState(false);
  const syncRunningRef = useRef(false);
  const prevProfileRef = useRef<string>("");

  const [selectedFolder, setSelectedFolder] = useState<string>("");

  // Browse grouping (FR-11.6): group the grid by folder (the default tree),
  // Test Set, Test Plan or Component. The container dimensions filter the grid
  // to a chosen container's members; Component filters to a chosen component.
  const [groupBy, setGroupBy] = useState<
    "folder" | "testset" | "testplan" | "component"
  >("folder");
  const [selectedContainer, setSelectedContainer] = useState<string>("");
  const [selectedComponent, setSelectedComponent] = useState<string>("");

  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const [showNewTest, setShowNewTest] = useState(false);
  const [newTestFolder, setNewTestFolder] = useState<string>("");
  const [refreshKey, setRefreshKey] = useState(0);
  const [detailVersion, setDetailVersion] = useState(0);

  const queryClient = useQueryClient();
  const pendingQuery = usePendingChanges(activeId);
  const pendingChanges = pendingQuery.data ?? [];
  // App-shell profile-scoped loads (Phase 4b). These replace imperative fetch
  // effects; refreshKey is still the bridge (Phase 4c retires it).
  const syncState = useSyncState(activeId).data ?? null;
  const folders = useFolders(activeId).data ?? [];
  const groupContainers = useGroupContainers(activeId, groupBy).data ?? [];
  const components = useComponents(activeId, groupBy).data ?? [];
  const [showPending, setShowPending] = useState(false);
  const [committing, setCommitting] = useState(false);
  const [lastCommitResult, setLastCommitResult] = useState<CommitResult | null>(
    null,
  );

  const [selectedSet, setSelectedSet] = useState<Set<string>>(new Set());
  const [showBulkEdit, setShowBulkEdit] = useState(false);
  const [showBulkTransition, setShowBulkTransition] = useState(false);
  const [showBulkAllocate, setShowBulkAllocate] = useState(false);
  const [showBulkMove, setShowBulkMove] = useState(false);
  const [showBulkPreconditions, setShowBulkPreconditions] = useState(false);
  const [showBulkRequirements, setShowBulkRequirements] = useState(false);
  const [showBulkReview, setShowBulkReview] = useState(false);

  const [view, setView] = useState<
    | "browse"
    | "preconditions"
    | "requirements"
    | "duplicates"
    | "gapanalysis"
    | "testcalls"
    | "dashboard"
    | "traceability"
    | "plans"
    | "coverage"
    | "misspellings"
  >("browse");

  // The onboarding tour (-335). Steps target Browse-only elements and the tour
  // can be replayed from any view, so it switches to Browse before starting.
  const { start: startTour } = useTour({
    onFinish: () => setTourSeenVersion(TOUR_VERSION),
  });
  const [showDiagnostics, setShowDiagnostics] = useState(false);
  const [showSyncHistory, setShowSyncHistory] = useState(false);
  const [showImport, setShowImport] = useState(false);
  const [showAbout, setShowAbout] = useState(false);

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

  // Load profiles once the backend reports healthy.
  useEffect(() => {
    if (!health || !health.ok) return;
    setLoadingProfiles(true);
    Promise.all([ListProfiles(), GetSettings()])
      .then(([ps, s]) => {
        setProfiles(ps);
        setDefaultProfileId(s.defaultProfileId ?? "");
        const t = s.theme || "light";
        setThemeState(t);
        applyTheme(t);
        setShowCoverage(!!s.showCoverage);
        setTourSeenVersion(s.tourSeenVersion ?? 0);
        if (ps.length > 0) {
          const def =
            s.defaultProfileId && ps.some((p) => p.id === s.defaultProfileId)
              ? s.defaultProfileId
              : ps[0].id;
          setActiveId(def);
        }
      })
      .catch((e) => console.error("load profiles:", errMsg(e)))
      .finally(() => setLoadingProfiles(false));
  }, [health]);

  // Subscribe to sync progress events for the lifetime of the app. The Sync
  // button stays disabled for the WHOLE sync (tests + folders + preconditions +
  // containers + custom fields); the engine emits a terminal Progress{done:true}
  // when everything is finished, which clears the syncing state here directly
  // (not only in the SyncProfile promise's finally) so the button never sticks.
  // Each non-terminal event carries a stage label shown in the status bar.
  useEffect(() => {
    return EventsOn("sync:progress", (p: SyncProgress) => {
      if (p.done) {
        setProgress(null);
        setSyncing(false);
      } else {
        setProgress(p);
      }
    });
  }, []);

  // The Spellcheck scan reports on its own channel, but its bar shares the
  // bottom status bar so it looks like every other sync. It never touches the
  // syncing state, so the Sync button stays enabled during a scan.
  useEffect(() => {
    return EventsOn("spellcheck:progress", (p: SyncProgress) => {
      setProgress(p.done ? null : p);
    });
  }, []);

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

  // reloadPending now invalidates the pending query instead of manually
  // refetching, so the existing ~35 call sites keep working during the
  // migration (the strangler bridge). The query itself loads on mount and when
  // activeId changes, so no refreshKey-driven effect is needed.
  const reloadPending = useCallback(() => {
    if (activeId) {
      queryClient.invalidateQueries({ queryKey: keys.pending(activeId) });
    }
  }, [queryClient, activeId]);

  // refreshProfileData is the single "refresh all profile lists" signal that
  // mutation handlers call. During the Phase 4c transition it BOTH bumps the
  // legacy refreshKey counter (still folded into the list/dashboard query keys)
  // AND invalidates those families directly. Once every hook is off refreshKey,
  // the counter bump is dropped and only the invalidation remains.
  const refreshProfileData = useCallback(() => {
    setRefreshKey((k) => k + 1);
    invalidateProfileData(queryClient, activeId);
  }, [queryClient, activeId]);

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
  // component whenever the dimension, profile, or refresh signal changes, so the
  // grid doesn't keep filtering by a now-hidden key.
  useEffect(() => {
    setSelectedContainer("");
  }, [activeId, groupBy, refreshKey]);

  useEffect(() => {
    setSelectedComponent("");
  }, [activeId, groupBy, refreshKey]);

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

  function toggleSelect(key: string) {
    setSelectedSet((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }

  function toggleSelectPage(keys: string[]) {
    setSelectedSet((prev) => {
      const allSelected = keys.every((k) => prev.has(k));
      const next = new Set(prev);
      if (allSelected) {
        for (const k of keys) next.delete(k);
      } else {
        for (const k of keys) next.add(k);
      }
      return next;
    });
  }

  // selectAllMatching replaces the current selection with every key that
  // matches the table's filter (FR-3.1). TestTable owns the query and the
  // backend call; this handler just absorbs the result.
  function selectAllMatching(keys: string[]) {
    setSelectedSet(new Set(keys));
  }

  async function doSync(full: boolean) {
    if (!activeId || syncRunningRef.current) return;
    if (committing) {
      await notice({
        title: "Commit in progress",
        message:
          "A commit is still running. Please wait for it to finish before syncing.",
        tone: "info",
      });
      return;
    }
    syncRunningRef.current = true;
    setSyncing(true);
    setSyncError("");
    setProgress({ phase: "", fetched: 0, total: 0, done: false });
    try {
      await (full ? SyncProfileFull(activeId) : SyncProfile(activeId));
      refreshProfileData();
      setDetailVersion((v) => v + 1);
      // Start the tour on the FIRST successful sync rather than at launch:
      // before a sync the grid is empty, so half the steps would spotlight
      // elements with no data behind them and teach nothing (-335).
      if (tourSeenVersion < TOUR_VERSION) startTour("browse", () => setView("browse"));
    } catch (e) {
      setSyncError(errMsg(e));
    } finally {
      syncRunningRef.current = false;
      setSyncing(false);
      setProgress(null);
    }
  }

  function runSync() {
    doSync(false);
  }

  // runFullSync forces a full re-pull, ignoring the incremental watermark, so
  // the Test Repository folder membership (skipped on routine resyncs) is
  // refreshed. It can be slow on large projects, so confirm first.
  async function runFullSync() {
    if (!activeId || syncRunningRef.current) return;
    if (committing) {
      await notice({
        title: "Commit in progress",
        message:
          "A commit is still running. Please wait for it to finish before syncing.",
        tone: "info",
      });
      return;
    }
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
  // (RND_P_4TFINT_05-260).
  async function syncTests() {
    if (!activeId || syncRunningRef.current) return;
    if (committing) {
      await notice({
        title: "Commit in progress",
        message:
          "A commit is still running. Please wait for it to finish before syncing.",
        tone: "info",
      });
      return;
    }
    syncRunningRef.current = true;
    setSyncing(true);
    try {
      await SyncTests(activeId);
      refreshProfileData();
    } catch (e) {
      await notice({ title: "Sync failed", message: errMsg(e), tone: "error" });
    } finally {
      syncRunningRef.current = false;
      setSyncing(false);
    }
  }

  // Native menu bar (built in main.go) drives the same actions via events. A ref
  // holds the latest handlers so a single subscription always sees current
  // state, rather than capturing stale closures.
  const menuActions = useRef<Record<string, () => void>>({});
  menuActions.current = {
    "menu:sync": runSync,
    "menu:full-sync": runFullSync,
    "menu:new-profile": () => setShowForm(true),
    "menu:import": () => setShowImport(true),
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
    "menu:sync-history": () => setShowSyncHistory(true),
    "menu:diagnostics": () => setShowDiagnostics(true),
    "menu:about": () => setShowAbout(true),
  };
  useEffect(() => {
    const unsubs = Object.keys(menuActions.current).map((event) =>
      EventsOn(event, () => menuActions.current[event]?.()),
    );
    return () => unsubs.forEach((u) => u && u());
  }, []);

  // chooseTheme applies + persists a colour-theme preference (FR-12.2).
  async function chooseTheme(next: string) {
    setThemeState(next);
    applyTheme(next);
    try {
      await SetTheme(next);
    } catch (e) {
      console.error("set theme:", errMsg(e));
    }
  }

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

  // setDefaultFor toggles the launch-default for a specific profile (clears it
  // if it's already the default), used by the Manage Profiles modal.
  async function setDefaultFor(id: string) {
    const next = defaultProfileId === id ? "" : id;
    try {
      await SetDefaultProfile(next);
      setDefaultProfileId(next);
    } catch (e) {
      console.error("set default profile:", errMsg(e));
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
    if (remaining.length === 0) setShowProfiles(false);
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
    setShowForm(false);
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

  // Called when the user clicks "Commit" in the pending modal. Pushes all
  // pending changes to Jira; per-Test results land in lastCommitResult.
  // Committed pending rows are deleted by the backend; failures stay.
  async function handleCommit() {
    if (!activeId || committing) return;
    if (syncing || syncRunningRef.current) {
      await notice({
        title: "Sync in progress",
        message:
          "A sync is still running. Please wait for it to finish before committing.",
        tone: "info",
      });
      return;
    }
    setCommitting(true);
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
      setCommitting(false);
    }
  }

  // handleCommitIds commits a selected subset of pending changes (selective
  // commit) — the per-item Commit button in the modal. Same result handling as
  // a full commit; only the chosen item leaves the list on success.
  async function handleCommitIds(ids: number[]) {
    if (!activeId || committing || ids.length === 0) return;
    if (syncing || syncRunningRef.current) {
      await notice({
        title: "Sync in progress",
        message:
          "A sync is still running. Please wait for it to finish before committing.",
        tone: "info",
      });
      return;
    }
    setCommitting(true);
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
      setCommitting(false);
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
    setShowPending(false);
    setLastCommitResult(null);
  }

  const activeProfile = profiles.find((p) => p.id === activeId);
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
  // on the newly-selected profile. Used to lock the profile switcher.
  const syncActive = syncing || syncRunningRef.current || progress !== null;

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
                onClick: () => setShowProfiles(true),
                title: "Manage profiles: add, edit, set default, export, or delete",
              },
              {
                key: "connections",
                label: "Connections…",
                onClick: () => setShowConnections(true),
                title:
                  "Manage this workspace's connections. Add a target " +
                  "(e.g. Kiwi) alongside its primary connection.",
              },
              {
                key: "bridge",
                label: "Bridge…",
                onClick: () => setShowBridge(true),
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
              onClick={() => setShowPending(true)}
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
                  onClick: () => setShowImport(true),
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
                  onClick: () => setShowSyncHistory(true),
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
                  onClick: () => setShowDiagnostics(true),
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
            disabled={syncing}
          >
            {syncing ? "Syncing…" : "Sync"}
          </button>
        </div>
      </header>

      {view === "browse" && selectedSet.size > 0 && (
        <div className="bulk-toolbar">
          <span className="bulk-count">{selectedSet.size} selected</span>
          <button
            className="btn btn-primary"
            onClick={() => setShowBulkEdit(true)}
          >
            Bulk edit…
          </button>
          {caps.supportsWorkflowTransitions && (
            <button
              className="btn btn-primary"
              onClick={() => setShowBulkTransition(true)}
            >
              Bulk transition…
            </button>
          )}
          <button
            className="btn btn-primary"
            onClick={() => setShowBulkAllocate(true)}
          >
            Allocate…
          </button>
          {caps.supportsFolders && folders.length > 0 && (
            <button
              className="btn btn-primary"
              onClick={() => setShowBulkMove(true)}
            >
              Move to folder…
            </button>
          )}
          {caps.supportsPreconditionObjects && (
            <button
              className="btn btn-primary"
              onClick={() => setShowBulkPreconditions(true)}
            >
              Preconditions…
            </button>
          )}
          {caps.supportsRequirementObjects && (
            <button
              className="btn btn-primary"
              onClick={() => setShowBulkRequirements(true)}
            >
              Requirements…
            </button>
          )}
          {REVIEW_ENABLED && (
            <button
              className="btn btn-primary"
              onClick={() => setShowBulkReview(true)}
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
            profileId={activeId}
            onChanged={() => {
              refreshProfileData();
              reloadPending();
            }}
          />
        </main>
      ) : view === "requirements" && caps.supportsRequirementObjects ? (
        <main className="content content-requirements">
          <RequirementsView
            profileId={activeId}
            onChanged={() => {
              refreshProfileData();
              reloadPending();
            }}
          />
        </main>
      ) : view === "duplicates" ? (
        <main className="content content-dashboard">
          <DuplicatesView
            profileId={activeId}
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
            profileId={activeId}
            onChanged={() => {
              refreshProfileData();
              reloadPending();
            }}
          />
        </main>
      ) : view === "testcalls" ? (
        <main className="content content-dashboard">
          <TestCallsView
            profileId={activeId}
            refreshKey={refreshKey}
            onChanged={() => {
              refreshProfileData();
              reloadPending();
            }}
          />
        </main>
      ) : view === "dashboard" ? (
        <main className="content content-dashboard">
          <Dashboard profileId={activeId} refreshKey={refreshKey} onOpenDuplicates={() => setView("duplicates")} />
        </main>
      ) : view === "traceability" ? (
        <main className="content content-dashboard">
          <TraceabilityTabs
            profileId={activeId}
            jiraUrl={activeProfile?.jiraUrl ?? ""}
          />
        </main>
      ) : view === "plans" ? (
        <main className="content content-containers">
          <ContainersView
            profileId={activeId}
            refreshKey={refreshKey}
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
            profileId={activeId}
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
            profileId={activeId}
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
            profileId={activeId}
            folderId={groupBy === "folder" ? selectedFolder : ""}
            containerKey={
              groupBy === "testset" || groupBy === "testplan"
                ? selectedContainer
                : ""
            }
            component={groupBy === "component" ? selectedComponent : ""}
            refreshKey={refreshKey}
            selectedKey={selectedKey}
            pendingByTestKey={pendingByTestKey}
            selectedSet={selectedSet}
            onSelect={setSelectedKey}
            onToggleSelect={toggleSelect}
            onToggleSelectPage={toggleSelectPage}
            onSelectAllMatching={selectAllMatching}
            onSync={syncTests}
            syncing={syncing}
          />
          {showNewTest ? (
            <NewTestPanel
              profileId={activeId}
              folders={folders}
              initialFolderId={newTestFolder}
              onCreated={handleTestCreated}
              onCancel={() => setShowNewTest(false)}
            />
          ) : (
            selectedKey && (
              <TestDetail
                profileId={activeId}
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

      {showForm && (
        <Modal
          onClose={() => {
            setShowForm(false);
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
                setShowForm(false);
                setEditingProfile(null);
              }}
            />
        </Modal>
      )}

      {showProfiles && (
        <ProfilesModal
          profiles={profiles}
          activeId={activeId}
          defaultProfileId={defaultProfileId}
          onClose={() => setShowProfiles(false)}
          onSetDefault={setDefaultFor}
          onExport={exportProfile}
          onImport={importProfile}
          onSaved={handleCreated}
          onDelete={deleteProfile}
        />
      )}

      {showConnections && activeId && (
        <ConnectionsModal
          activeId={activeId}
          onClose={() => setShowConnections(false)}
        />
      )}

      {showBridge && activeId && (
        <BridgeWizard
          activeId={activeId}
          onClose={() => setShowBridge(false)}
          onOpenConnections={() => {
            setShowBridge(false);
            setShowConnections(true);
          }}
        />
      )}

      {showPending && (
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
          committing={committing}
          lastResult={lastCommitResult}
        />
      )}

      {showBulkEdit && (
        <BulkEditModal
          profileId={activeId}
          testKeys={[...selectedSet]}
          onComplete={() => {
            refreshProfileData();
            setDetailVersion((v) => v + 1);
            reloadPending();
            setSelectedSet(new Set());
            setShowBulkEdit(false);
          }}
          onCancel={() => {
            refreshProfileData();
            setDetailVersion((v) => v + 1);
            reloadPending();
            setShowBulkEdit(false);
          }}
        />
      )}

      {showBulkTransition && (
        <BulkTransitionModal
          profileId={activeId}
          testKeys={[...selectedSet]}
          onComplete={() => {
            refreshProfileData();
            setDetailVersion((v) => v + 1);
            reloadPending();
            setSelectedSet(new Set());
            setShowBulkTransition(false);
          }}
          onCancel={() => {
            refreshProfileData();
            setDetailVersion((v) => v + 1);
            reloadPending();
            setShowBulkTransition(false);
          }}
        />
      )}

      {showBulkAllocate && (
        <BulkAllocateModal
          profileId={activeId}
          testKeys={[...selectedSet]}
          onComplete={() => {
            refreshProfileData();
            setDetailVersion((v) => v + 1);
            reloadPending();
            setSelectedSet(new Set());
            setShowBulkAllocate(false);
          }}
          onCancel={() => {
            refreshProfileData();
            setDetailVersion((v) => v + 1);
            reloadPending();
            setShowBulkAllocate(false);
          }}
        />
      )}

      {showBulkMove && (
        <BulkMoveModal
          profileId={activeId}
          testKeys={[...selectedSet]}
          folders={folders}
          onComplete={() => {
            refreshProfileData();
            setDetailVersion((v) => v + 1);
            reloadPending();
            setSelectedSet(new Set());
            setShowBulkMove(false);
          }}
          onCancel={() => {
            refreshProfileData();
            setDetailVersion((v) => v + 1);
            reloadPending();
            setShowBulkMove(false);
          }}
        />
      )}

      {showDiagnostics && (
        <DiagnosticsModal onClose={() => setShowDiagnostics(false)} />
      )}

      {showAbout && (
        <AboutModal
          onClose={() => setShowAbout(false)}
          onTakeTour={() => startTour(view)}
        />
      )}

      {showSyncHistory && (
        <SyncHistoryModal
          profileId={activeId}
          refreshKey={refreshKey}
          onClose={() => setShowSyncHistory(false)}
        />
      )}

      {showImport && (
        <ImportTestsModal
          profileId={activeId}
          onComplete={() => {
            refreshProfileData();
            reloadPending();
            setShowImport(false);
          }}
          onCancel={() => setShowImport(false)}
        />
      )}

      {showBulkPreconditions && (
        <BulkPreconditionsModal
          profileId={activeId}
          testKeys={[...selectedSet]}
          onComplete={() => {
            refreshProfileData();
            setDetailVersion((v) => v + 1);
            reloadPending();
            setSelectedSet(new Set());
            setShowBulkPreconditions(false);
          }}
          onCancel={() => {
            refreshProfileData();
            setDetailVersion((v) => v + 1);
            reloadPending();
            setShowBulkPreconditions(false);
          }}
        />
      )}

      {showBulkRequirements && (
        <BulkRequirementsModal
          profileId={activeId}
          testKeys={[...selectedSet]}
          onComplete={() => {
            refreshProfileData();
            setDetailVersion((v) => v + 1);
            reloadPending();
            setSelectedSet(new Set());
            setShowBulkRequirements(false);
          }}
          onCancel={() => setShowBulkRequirements(false)}
        />
      )}

      {REVIEW_ENABLED && showBulkReview && (
        <BulkReviewModal
          profileId={activeId}
          testKeys={[...selectedSet]}
          onComplete={() => {
            refreshProfileData();
            setDetailVersion((v) => v + 1);
            reloadPending();
            setSelectedSet(new Set());
            setShowBulkReview(false);
          }}
          onCancel={() => setShowBulkReview(false)}
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

      {promptUI}
      {confirmUI}
      {noticeUI}
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
