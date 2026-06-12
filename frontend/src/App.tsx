import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import "./App.css";
import {
  Health,
  ListProfiles,
  SyncProfile,
  SyncProfileFull,
  GetSyncState,
  ListFolders,
  CreateFolder,
  RenameFolder,
  DeleteFolder,
  ListContainers,
  ListComponents,
  UpdateProfileScope,
  ExportProfile,
  ImportProfile,
  UpdateProfileToken,
  GetSettings,
  SetDefaultProfile,
  SetTheme,
  ResolveConflictOverride,
  ResolveConflictKeepRemote,
  ListPendingChanges,
  DiscardPendingChange,
  DiscardAllPendingChanges,
  CommitPendingChanges,
  CommitPendingChangesByIDs,
  EventsOn,
  errMsg,
} from "./api";
import type {
  HealthInfo,
  Profile,
  SyncState,
  SyncProgress,
  Folder,
  Container,
  Bucket,
  PendingChange,
  CommitResult,
} from "./api";
import { ProfileForm } from "./components/ProfileForm";
import { TestTable } from "./components/TestTable";
import { TestDetail } from "./components/TestDetail";
import { NewTestPanel } from "./components/NewTestPanel";
import { FolderTree } from "./components/FolderTree";
import { ContainerList } from "./components/ContainerList";
import { ComponentList } from "./components/ComponentList";
import { PendingChangesModal } from "./components/PendingChangesModal";
import { BulkReviewModal } from "./components/BulkReviewModal";
import { BulkEditModal } from "./components/BulkEditModal";
import { BulkTransitionModal } from "./components/BulkTransitionModal";
import { BulkAllocateModal } from "./components/BulkAllocateModal";
import { BulkMoveModal } from "./components/BulkMoveModal";
import { BulkPreconditionsModal } from "./components/BulkPreconditionsModal";
import { BulkRequirementsModal } from "./components/BulkRequirementsModal";
import { Dashboard } from "./components/Dashboard";
import { ContainersView } from "./components/ContainersView";
import { PreconditionsView } from "./components/PreconditionsView";
import { RequirementsView } from "./components/RequirementsView";
import { DuplicatesView } from "./components/DuplicatesView";
import { TestCallsView } from "./components/TestCallsView";
import { DiagnosticsModal } from "./components/DiagnosticsModal";
import { SyncHistoryModal } from "./components/SyncHistoryModal";
import { ImportTestsModal } from "./components/ImportTestsModal";
import { Menu } from "./components/Menu";
import { AboutModal } from "./components/AboutModal";
import { usePrompt } from "./components/usePrompt";

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
  const { prompt, promptUI } = usePrompt();
  const [loadingProfiles, setLoadingProfiles] = useState(false);
  const [showForm, setShowForm] = useState(false);
  // When set, the profile modal opens in edit mode for this profile (FR-5).
  const [editingProfile, setEditingProfile] = useState<Profile | null>(null);

  const [syncState, setSyncState] = useState<SyncState | null>(null);
  const [progress, setProgress] = useState<SyncProgress | null>(null);
  const [syncError, setSyncError] = useState("");
  // syncing drives the Sync button label/disabled state; it is released as soon
  // as the Test pull finishes (testsDone) so the button doesn't look stuck while
  // the best-effort tail work runs. syncRunningRef tracks the whole backend Sync
  // call, so a concurrent sync can't be started while the tail is still going.
  const [syncing, setSyncing] = useState(false);
  const syncRunningRef = useRef(false);

  const [folders, setFolders] = useState<Folder[]>([]);
  const [selectedFolder, setSelectedFolder] = useState<string>("");

  // Browse grouping (FR-11.6): group the grid by folder (the default tree),
  // Test Set, Test Plan or Component. The container dimensions filter the grid
  // to a chosen container's members; Component filters to a chosen component.
  const [groupBy, setGroupBy] = useState<
    "folder" | "testset" | "testplan" | "component"
  >("folder");
  const [groupContainers, setGroupContainers] = useState<Container[]>([]);
  const [selectedContainer, setSelectedContainer] = useState<string>("");
  const [components, setComponents] = useState<Bucket[]>([]);
  const [selectedComponent, setSelectedComponent] = useState<string>("");

  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const [showNewTest, setShowNewTest] = useState(false);
  const [newTestFolder, setNewTestFolder] = useState<string>("");
  const [refreshKey, setRefreshKey] = useState(0);
  const [detailVersion, setDetailVersion] = useState(0);

  const [pendingChanges, setPendingChanges] = useState<PendingChange[]>([]);
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
    | "testcalls"
    | "dashboard"
    | "plans"
  >("browse");
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

  // First: check whether the backend started up cleanly.
  useEffect(() => {
    Health()
      .then(setHealth)
      .catch((e) =>
        setHealth({
          ok: false,
          error: `Health check itself failed: ${errMsg(e)}`,
          dbPath: "",
          logPath: "",
        }),
      );
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
  }, [pendingChanges]);

  const reloadPending = useCallback(() => {
    if (!activeId) {
      setPendingChanges([]);
      return;
    }
    ListPendingChanges(activeId)
      .then(setPendingChanges)
      .catch((e) => console.error("list pending:", errMsg(e)));
  }, [activeId]);

  useEffect(() => {
    reloadPending();
  }, [reloadPending, refreshKey]);

  // Refresh the sync summary and folder tree when the active profile changes
  // or a sync finishes.
  const loadProfileData = useCallback(() => {
    if (!activeId) {
      setSyncState(null);
      setFolders([]);
      return;
    }
    GetSyncState(activeId)
      .then(setSyncState)
      .catch((e) => console.error("sync state:", errMsg(e)));
    ListFolders(activeId)
      .then(setFolders)
      .catch((e) => console.error("list folders:", errMsg(e)));
  }, [activeId]);

  useEffect(() => {
    loadProfileData();
  }, [loadProfileData, refreshKey]);

  // Clear folder + container + component + row selection when the profile changes.
  useEffect(() => {
    setSelectedFolder("");
    setSelectedContainer("");
    setSelectedComponent("");
    setSelectedSet(new Set());
  }, [activeId]);

  // Load the containers backing the group-by sidebar when grouping by Test Set
  // or Test Plan, and reset the chosen container whenever the dimension or
  // profile changes so the grid doesn't keep filtering by a now-hidden key.
  useEffect(() => {
    setSelectedContainer("");
    if (!activeId || (groupBy !== "testset" && groupBy !== "testplan")) {
      setGroupContainers([]);
      return;
    }
    let cancelled = false;
    ListContainers(activeId, groupBy)
      .then((cs) => {
        if (!cancelled) setGroupContainers(cs ?? []);
      })
      .catch((e) => console.error("list containers:", errMsg(e)));
    return () => {
      cancelled = true;
    };
  }, [activeId, groupBy, refreshKey]);

  // Load the distinct components backing the group-by-component sidebar, and
  // reset the chosen component when the dimension or profile changes.
  useEffect(() => {
    setSelectedComponent("");
    if (!activeId || groupBy !== "component") {
      setComponents([]);
      return;
    }
    let cancelled = false;
    ListComponents(activeId)
      .then((cs) => {
        if (!cancelled) setComponents(cs ?? []);
      })
      .catch((e) => console.error("list components:", errMsg(e)));
    return () => {
      cancelled = true;
    };
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
      setRefreshKey((k) => k + 1);
      reloadPending();
    } catch (e) {
      window.alert(errMsg(e));
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
      setRefreshKey((k) => k + 1);
      reloadPending();
    } catch (e) {
      window.alert(errMsg(e));
    }
  }

  async function deleteFolder(path: string) {
    if (!activeId) return;
    if (!window.confirm(`Delete folder "${path}"? It must be empty.`)) return;
    try {
      await DeleteFolder(activeId, path);
      if (selectedFolder === path) setSelectedFolder("");
      setRefreshKey((k) => k + 1);
      reloadPending();
    } catch (e) {
      window.alert(errMsg(e));
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
    syncRunningRef.current = true;
    setSyncing(true);
    setSyncError("");
    setProgress({ phase: "", fetched: 0, total: 0, done: false });
    try {
      await (full ? SyncProfileFull(activeId) : SyncProfile(activeId));
      setRefreshKey((k) => k + 1);
      setDetailVersion((v) => v + 1);
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
  function runFullSync() {
    if (!activeId || syncRunningRef.current) return;
    if (
      !window.confirm(
        "Full resync re-pulls every test and re-maps Test Repository folders. " +
          "This can take a while on large projects. Continue?",
      )
    ) {
      return;
    }
    doSync(true);
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
    "menu:view-plans": () => setView("plans"),
    "menu:view-duplicates": () => setView("duplicates"),
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

  // editScope adjusts the active profile's JQL scope override (FR-5.4). It
  // takes effect on the next sync.
  async function editScope() {
    if (!activeProfile) return;
    const next = await prompt({
      title: "Scope JQL — narrows which tests sync (blank = all)",
      defaultValue: activeProfile.scopeJql,
      placeholder: "e.g. labels = smoke",
      submitLabel: "Save",
    });
    if (next === null) return;
    try {
      await UpdateProfileScope(activeId, next.trim());
      setProfiles((prev) =>
        prev.map((p) =>
          p.id === activeId ? { ...p, scopeJql: next.trim() } : p,
        ),
      );
    } catch (e) {
      console.error("update scope:", errMsg(e));
    }
  }

  // toggleDefault sets the active profile as the launch default, or clears it
  // if it's already the default (FR-12.2).
  async function toggleDefault() {
    if (!activeId) return;
    const next = defaultProfileId === activeId ? "" : activeId;
    try {
      await SetDefaultProfile(next);
      setDefaultProfileId(next);
    } catch (e) {
      console.error("set default profile:", errMsg(e));
    }
  }

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

  // exportProfile writes the active profile's config (no credential) to a file
  // the user picks (FR-5.5).
  async function exportProfile() {
    if (!activeId) return;
    try {
      const path = await ExportProfile(activeId);
      if (path) window.alert(`Profile exported to:\n${path}`);
    } catch (e) {
      window.alert(`Export failed: ${errMsg(e)}`);
    }
  }

  // importProfile creates a profile from a chosen config file, then prompts for
  // its PAT (the credential isn't part of the exported file) (FR-5.5).
  async function importProfile() {
    try {
      const p = await ImportProfile();
      if (!p.id) return; // cancelled
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
    } catch (e) {
      window.alert(`Import failed: ${errMsg(e)}`);
    }
  }

  // setToken updates the active profile's stored PAT (FR-5.5) — for imported
  // profiles or token rotation.
  async function setToken() {
    if (!activeProfile) return;
    const token = await prompt({
      title: `New Personal Access Token for "${activeProfile.name}"`,
      placeholder: "Paste token",
      password: true,
      submitLabel: "Update token",
    });
    if (token === null || !token.trim()) return;
    try {
      await UpdateProfileToken(activeId, token.trim());
      window.alert("Token updated.");
    } catch (e) {
      window.alert(`Token update failed: ${errMsg(e)}`);
    }
  }

  // handleCreated handles both a newly-created profile and an edited one: it
  // replaces the existing entry when the id is already known, otherwise appends.
  // After an edit, the cached data may have been cleared (project/URL change),
  // so the views are refreshed.
  function handleCreated(p: Profile) {
    setProfiles((prev) =>
      prev.some((x) => x.id === p.id)
        ? prev.map((x) => (x.id === p.id ? p : x))
        : [...prev, p],
    );
    setActiveId(p.id);
    setShowForm(false);
    setEditingProfile(null);
    setSelectedKey(null);
    setRefreshKey((k) => k + 1);
    setDetailVersion((v) => v + 1);
    reloadPending();
  }

  // editActiveProfile opens the profile modal in edit mode for the active
  // profile — e.g. to correct a wrong project key (FR-5).
  function editActiveProfile() {
    if (!activeProfile) return;
    setEditingProfile(activeProfile);
    setShowForm(true);
  }

  // Called by TestDetail after a successful inline edit. Refreshes the
  // grid (so it shows the new value) and the pending list. Deliberately
  // does NOT bump detailVersion — TestDetail already has the new value in
  // its own local state, and re-fetching mid-edit would risk clobbering
  // a field the user is still typing in.
  function handleEdited() {
    setRefreshKey((k) => k + 1);
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
      setRefreshKey((k) => k + 1);
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
      setRefreshKey((k) => k + 1);
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
    setCommitting(true);
    setLastCommitResult(null);
    try {
      const result = await CommitPendingChanges(activeId);
      setLastCommitResult(result);
      applyCreatedRemap(result);
      setRefreshKey((k) => k + 1);
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
    setCommitting(true);
    setLastCommitResult(null);
    try {
      const result = await CommitPendingChangesByIDs(activeId, ids);
      setLastCommitResult(result);
      applyCreatedRemap(result);
      setRefreshKey((k) => k + 1);
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
      setRefreshKey((k) => k + 1);
      setDetailVersion((v) => v + 1);
      reloadPending();
    } catch (e) {
      console.error("keep remote:", errMsg(e));
    }
  }

  function closePendingModal() {
    setShowPending(false);
    setLastCommitResult(null);
  }

  const activeProfile = profiles.find((p) => p.id === activeId);
  const isDemo =
    !!activeProfile &&
    /^(demo$|demo:|mock:)/i.test(activeProfile.jiraUrl.trim());

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
            Try removing the database file and relaunching, or check the log
            for a more detailed error.
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
            className="profile-select"
            value={activeId}
            onChange={(e) => {
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
            label="Profile"
            title="Profile actions"
            items={[
              {
                key: "default",
                label:
                  defaultProfileId === activeId
                    ? "Default on launch"
                    : "Set as default",
                checked: defaultProfileId === activeId,
                onClick: toggleDefault,
                title: "Auto-select this profile when the app starts",
              },
              {
                key: "edit",
                label: "Edit profile…",
                onClick: editActiveProfile,
                title: "Edit name, Jira URL, project key, or scope",
              },
              {
                key: "scope",
                label: activeProfile?.scopeJql ? "Edit scope ●" : "Set scope…",
                onClick: editScope,
                title: activeProfile?.scopeJql
                  ? `Scope: ${activeProfile.scopeJql}`
                  : "Narrow which tests sync with a JQL scope",
              },
              {
                key: "token",
                label: "Set token…",
                onClick: setToken,
                title: "Set or rotate the active profile's token",
              },
              { key: "d1", divider: true },
              {
                key: "export",
                label: "Export profile…",
                onClick: exportProfile,
                title: "Export the active profile (without its token)",
              },
              {
                key: "import",
                label: "Import profile…",
                onClick: importProfile,
              },
              { key: "d2", divider: true },
              {
                key: "new",
                label: "New profile…",
                onClick: () => setShowForm(true),
              },
            ]}
          />
        </div>

        <nav className="view-tabs topbar-zone topbar-center">
          <button
            className={`view-tab${view === "browse" ? " view-tab-active" : ""}`}
            onClick={() => setView("browse")}
          >
            Browse
          </button>
          <button
            className={`view-tab${view === "preconditions" ? " view-tab-active" : ""}`}
            onClick={() => setView("preconditions")}
          >
            Preconditions
          </button>
          <button
            className={`view-tab${view === "requirements" ? " view-tab-active" : ""}`}
            onClick={() => setView("requirements")}
          >
            Requirements
          </button>
          <button
            className={`view-tab${view === "duplicates" ? " view-tab-active" : ""}`}
            onClick={() => setView("duplicates")}
          >
            Duplicates
          </button>
          <button
            className={`view-tab${view === "testcalls" ? " view-tab-active" : ""}`}
            onClick={() => setView("testcalls")}
          >
            Test Calls
          </button>
          <button
            className={`view-tab${view === "dashboard" ? " view-tab-active" : ""}`}
            onClick={() => setView("dashboard")}
          >
            Dashboard
          </button>
          <button
            className={`view-tab${view === "plans" ? " view-tab-active" : ""}`}
            onClick={() => setView("plans")}
          >
            Containers
          </button>
        </nav>

        <div className="topbar-zone topbar-right">
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
                  "Force a full re-sync, ignoring the incremental watermark — " +
                  "re-maps Test Repository folder membership",
              },
              {
                key: "history",
                label: "Sync history",
                onClick: () => setShowSyncHistory(true),
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
            ]}
          />

          <button
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
          <button
            className="btn btn-primary"
            onClick={() => setShowBulkTransition(true)}
          >
            Bulk transition…
          </button>
          <button
            className="btn btn-primary"
            onClick={() => setShowBulkAllocate(true)}
          >
            Allocate…
          </button>
          {folders.length > 0 && (
            <button
              className="btn btn-primary"
              onClick={() => setShowBulkMove(true)}
            >
              Move to folder…
            </button>
          )}
          <button
            className="btn btn-primary"
            onClick={() => setShowBulkPreconditions(true)}
          >
            Preconditions…
          </button>
          <button
            className="btn btn-primary"
            onClick={() => setShowBulkRequirements(true)}
          >
            Requirements…
          </button>
          <button
            className="btn btn-primary"
            onClick={() => setShowBulkReview(true)}
          >
            Review…
          </button>
          <button className="btn" onClick={() => setSelectedSet(new Set())}>
            Clear
          </button>
        </div>
      )}

      {view === "preconditions" ? (
        <main className="content content-preconditions">
          <PreconditionsView
            profileId={activeId}
            refreshKey={refreshKey}
            onChanged={() => {
              setRefreshKey((k) => k + 1);
              reloadPending();
            }}
          />
        </main>
      ) : view === "requirements" ? (
        <main className="content content-requirements">
          <RequirementsView
            profileId={activeId}
            refreshKey={refreshKey}
            onChanged={() => {
              setRefreshKey((k) => k + 1);
              reloadPending();
            }}
          />
        </main>
      ) : view === "duplicates" ? (
        <main className="content content-dashboard">
          <DuplicatesView
            profileId={activeId}
            refreshKey={refreshKey}
            folders={folders}
            pendingByTestKey={pendingByTestKey}
            onChanged={() => {
              setRefreshKey((k) => k + 1);
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
              setRefreshKey((k) => k + 1);
              reloadPending();
            }}
          />
        </main>
      ) : view === "dashboard" ? (
        <main className="content content-dashboard">
          <Dashboard profileId={activeId} refreshKey={refreshKey} onOpenDuplicates={() => setView("duplicates")} />
        </main>
      ) : view === "plans" ? (
        <main className="content content-dashboard">
          <ContainersView
            profileId={activeId}
            refreshKey={refreshKey}
            isDemo={isDemo}
            onChanged={() => {
              setRefreshKey((k) => k + 1);
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
              <option value="folder">Group by: Folder</option>
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
              folders.length > 0 ? (
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
                onClose={() => setSelectedKey(null)}
                onEdited={handleEdited}
              />
            )
          )}
        </main>
      )}

      {showForm && (
        <div
          className="modal-overlay"
          onClick={() => {
            setShowForm(false);
            setEditingProfile(null);
          }}
        >
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <ProfileForm
              profile={editingProfile ?? undefined}
              profiles={profiles}
              onCreated={handleCreated}
              onCancel={() => {
                setShowForm(false);
                setEditingProfile(null);
              }}
            />
          </div>
        </div>
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
            setRefreshKey((k) => k + 1);
            setDetailVersion((v) => v + 1);
            reloadPending();
            setSelectedSet(new Set());
            setShowBulkEdit(false);
          }}
          onCancel={() => {
            setRefreshKey((k) => k + 1);
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
            setRefreshKey((k) => k + 1);
            setDetailVersion((v) => v + 1);
            reloadPending();
            setSelectedSet(new Set());
            setShowBulkTransition(false);
          }}
          onCancel={() => {
            setRefreshKey((k) => k + 1);
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
            setRefreshKey((k) => k + 1);
            setDetailVersion((v) => v + 1);
            reloadPending();
            setSelectedSet(new Set());
            setShowBulkAllocate(false);
          }}
          onCancel={() => {
            setRefreshKey((k) => k + 1);
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
            setRefreshKey((k) => k + 1);
            setDetailVersion((v) => v + 1);
            reloadPending();
            setSelectedSet(new Set());
            setShowBulkMove(false);
          }}
          onCancel={() => {
            setRefreshKey((k) => k + 1);
            setDetailVersion((v) => v + 1);
            reloadPending();
            setShowBulkMove(false);
          }}
        />
      )}

      {showDiagnostics && (
        <DiagnosticsModal onClose={() => setShowDiagnostics(false)} />
      )}

      {showAbout && <AboutModal onClose={() => setShowAbout(false)} />}

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
            setRefreshKey((k) => k + 1);
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
            setRefreshKey((k) => k + 1);
            setDetailVersion((v) => v + 1);
            reloadPending();
            setSelectedSet(new Set());
            setShowBulkPreconditions(false);
          }}
          onCancel={() => {
            setRefreshKey((k) => k + 1);
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
            setRefreshKey((k) => k + 1);
            setDetailVersion((v) => v + 1);
            reloadPending();
            setSelectedSet(new Set());
            setShowBulkRequirements(false);
          }}
          onCancel={() => setShowBulkRequirements(false)}
        />
      )}

      {showBulkReview && (
        <BulkReviewModal
          profileId={activeId}
          testKeys={[...selectedSet]}
          onComplete={() => {
            setRefreshKey((k) => k + 1);
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
