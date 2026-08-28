import { useEffect, useMemo, useRef, useState } from "react";
import { useViewState } from "../lib/viewState";
import {
  SeedSampleContainers,
  CleanSampleData,
  CreateContainerAndAllocate,
  EditContainer,
  DeleteContainer,
  SetContainerEnvironments,
  DeallocateTests,
  SetTestRunStatus,
  BulkSetTestRunStatus,
  UnlinkBugFromRun,
  SetTestRunComment,
  BulkEditContainers,
  ExportPytest,
  SyncContainers,
  BrowserOpenURL,
  GetRunRollupBreakdown,
  errMsg,
} from "../api";
import type { Bucket, Bug, ExecMemberRun, RollupMember } from "../api";
import { RollupBreakdownModal } from "./RollupBreakdownModal";
import { SortControl } from "./SortControl";
import { SearchableSelect } from "./SearchableSelect";
import { keyCompare, cmpStr, applyDir } from "../sort";
import { Menu } from "./Menu";
import { AddTestsModal } from "./AddTestsModal";
import { BugsPanel } from "./BugsPanel";
import { JUnitImportModal } from "./JUnitImportModal";
import { JUnitNewExecModal } from "./JUnitNewExecModal";
import { CreateBugModal } from "./CreateBugModal";
import { LinkBugPicker } from "./LinkBugPicker";
import { TestDetail } from "./TestDetail";
import { usePrompt } from "./usePrompt";
import {
  useContainers,
  useContainerBoard,
  useContainerBugs,
  useContainerMembers,
  useContainerRollup,
} from "../queries/containers";
import { useConfirm } from "./useConfirm";
import { useNotice } from "./useNotice";
import { useCapabilities } from "../features";

interface Props {
  profileId: string;
  refreshKey: number;
  onChanged: () => void;
  // Sample-data generation is a demo aid — only offered for demo profiles so a
  // user can't seed fake containers into a real project (FR-5).
  isDemo: boolean;
  jiraUrl?: string;
  onOpenTest?: (testKey: string) => void;
}

const KINDS: Array<{ value: string; label: string }> = [
  { value: "testset", label: "Test Set" },
  { value: "testplan", label: "Test Plan" },
  { value: "testexec", label: "Test Execution" },
];

// Standard Xray Test Run result vocabulary (mirrors testrepo.RunStatuses).
const RUN_STATUSES = ["TODO", "EXECUTING", "PASS", "FAIL", "ABORTED", "BLOCKED"];

// ContainersView manages Test Sets / Plans / Executions (FR-13.7 + container
// CRUD): pick a kind and a container, see its member Tests with run status, and
// create / rename / delete. Computed from the local store; recomputes when the
// profile changes or a sync/commit bumps refreshKey.
export function ContainersView({
  profileId,
  refreshKey,
  onChanged,
  isDemo,
  jiraUrl,
  onOpenTest,
}: Props) {
  // Gates the Test Execution environment controls to what the active
  // profile's backend actually supports (P6.2a).
  const caps = useCapabilities(profileId);
  const [kind, setKind] = useViewState(profileId, "containers", "kind", "testplan");
  const [cStatus, setCStatus] = useViewState(profileId, "containers", "cStatus", "");
  // Execution-type filter (Test Execution kind only): "" = all, "standalone", "subtask".
  const [cExecType, setCExecType] = useViewState(profileId, "containers", "cExecType", "");
  // Environment filter (Test Execution kind only): "" = any; otherwise keep only
  // executions whose environments array contains the value (mirrors
  // ContainerQuery.Environment server-side, applied client-side here since the
  // environments are already loaded on each container).
  const [cEnv, setCEnv] = useViewState(profileId, "containers", "cEnv", "");
  // Label filter (all kinds): "" = any; otherwise keep only containers whose
  // labels array contains the value.
  const [cLabel, setCLabel] = useViewState(profileId, "containers", "cLabel", "");
  // Inline environment editor (selected execution): a draft of a new env name.
  const [envDraft, setEnvDraft] = useState("");
  // Batch environment editor (all currently-filtered executions): chosen
  // operation, the env name to apply, and an in-flight guard.
  const [batchEnvOp, setBatchEnvOp] = useState<"add_env" | "remove_env" | "set_env">("add_env");
  const [batchEnvName, setBatchEnvName] = useState("");
  const [batchEnvBusy, setBatchEnvBusy] = useState(false);
  // The batch-environment controls live in a popover so the filter bar stays a
  // single compact row instead of a dedicated tools strip (#310 follow-up).
  const [envToolsOpen, setEnvToolsOpen] = useState(false);
  const [cSortField, setCSortField] = useViewState(profileId, "containers", "cSortField", "key");
  const [cSortDesc, setCSortDesc] = useViewState(profileId, "containers", "cSortDesc", false);
  const [rowSortField, setRowSortField] = useState("key");
  const [rowSortDesc, setRowSortDesc] = useState(false);
  const containersQuery = useContainers(profileId, kind);
  const containers = containersQuery.data ?? [];
  const listError = containersQuery.error ? errMsg(containersQuery.error) : "";
  const [selected, setSelected] = useViewState(profileId, "containers", "selected", "");
  // The selected container's detail reads come from the query cache with stable
  // keys (Phase 4c), refreshed by the same invalidation as the container list.
  const boardQuery = useContainerBoard(profileId, selected);
  const board = boardQuery.data ?? null;
  const relatedBugs = useContainerBugs(profileId, selected).data ?? [];
  const membersQuery = useContainerMembers(profileId, selected, kind);
  const memberRuns = useMemo(() => {
    const m = new Map<string, ExecMemberRun>();
    for (const r of membersQuery.data ?? []) m.set(r.testKey, r);
    return m;
  }, [membersQuery.data]);
  const rollup = useContainerRollup(profileId, selected, kind).data ?? null;
  const [bugFor, setBugFor] = useState<{ testKey: string; summary: string } | null>(null);
  // Test key whose Defects cell has an open LinkBugPicker (Test Execution
  // member table only).
  const [linkBugFor, setLinkBugFor] = useState<string | null>(null);
  const [mode, setMode] = useViewState<"containers" | "bugs">(profileId, "containers", "mode", "containers");
  // Whether the related-bugs collapsible section in the container card is open.
  // Collapsed by default so a large bug list never hides the member table below.
  const [bugsExpanded, setBugsExpanded] = useState(false);
  const loading = containersQuery.isFetching;
  const [error, setError] = useState("");
  const [seeding, setSeeding] = useState(false);
  const [syncing, setSyncing] = useState(false);
  // Bulk execution-result selection: the set of Test keys checked in the board
  // (Test Execution only).
  const [selectedRuns, setSelectedRuns] = useState<Set<string>>(new Set());
  const [bulkStatus, setBulkStatus] = useState("PASS");
  const [editingName, setEditingName] = useState(false);
  const [nameDraft, setNameDraft] = useState("");
  const [showAdd, setShowAdd] = useState(false);
  const [showJUnitImport, setShowJUnitImport] = useState(false);
  const [showJUnitNewExec, setShowJUnitNewExec] = useState(false);
  const [boardPage, setBoardPage] = useState(0);
  // Active fix-version filter for the member table (Test Execution only). Empty
  // string means "All". Clicking an execution fix-version chip toggles it; a
  // second click clears the filter (single-select toggle).
  const [memberFvFilter, setMemberFvFilter] = useState("");
  // Run-status filter for the member table, driven by clicking a segment of the
  // run colorbar or one of its count badges. Empty string means "all"; the
  // "(not run)" bucket matches members with a blank run status.
  const [memberRunFilter, setMemberRunFilter] = useState("");
  // Clickable roll-up breakdown: which bucket's modal is open, and the member
  // detail (lazily fetched on first badge click, cached per selected container).
  const [breakdownStatus, setBreakdownStatus] = useState<string | null>(null);
  const [breakdown, setBreakdown] = useState<RollupMember[] | null>(null);
  const [breakdownFor, setBreakdownFor] = useState("");
  const [breakdownLoading, setBreakdownLoading] = useState(false);
  // Collapse the selected container's detail (parent, environments, fix
  // versions, name, run bar) to a single header line to give the member table
  // more room. Persisted per profile so the choice sticks across selections.
  const [cardCollapsed, setCardCollapsed] = useViewState(
    profileId,
    "containers",
    "cardCollapsed",
    false,
  );
  const { prompt, promptUI } = usePrompt();
  const { confirm, confirmUI } = useConfirm();
  const { notice, noticeUI } = useNotice();

  // In-view read-only test detail sidebar for Test Execution member rows:
  // detailKey is session-persisted so the panel restores on returning to this
  // view; detailVersion is ephemeral and bumped on each open to force a
  // re-fetch even when reopening the same test.
  const [detailKey, setDetailKey] = useViewState<string | null>(profileId, "containers", "detailKey", null);
  const [detailVersion, setDetailVersion] = useState(0);

  // Member tables can be long (an Execution may hold hundreds of tests), so the
  // board is paged client-side. The page size is user-selectable; default 15.
  const [pageSize, setPageSize] = useState(15);
  const PAGE_SIZE_OPTIONS = [10, 15, 25, 50, 100, 200];

  // removeTest unassigns a single Test from the selected container (FR-3.4–3.6),
  // queued for commit.
  async function removeTest(testKey: string) {
    if (!selected) return;
    setError("");
    try {
      await DeallocateTests(profileId, selected, [testKey]);
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  // setRunStatus updates a Test's run result in the selected Test Execution,
  // queued for commit to Xray.
  async function setRunStatus(testKey: string, status: string) {
    if (!selected || !status) return;
    setError("");
    try {
      await SetTestRunStatus(profileId, selected, testKey, status);
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  // unlinkBug removes one defect link from a Test's run result in the
  // selected Test Execution, queued for commit to Xray.
  async function unlinkBug(testKey: string, bugKey: string) {
    if (!selected) return;
    setError("");
    try {
      await UnlinkBugFromRun(profileId, selected, testKey, bugKey);
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  // setRunComment updates a Test's run remark in the selected Test Execution,
  // queued for commit to Xray.
  async function setRunComment(testKey: string, comment: string) {
    if (!selected) return;
    setError("");
    try {
      await SetTestRunComment(profileId, selected, testKey, comment);
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  const kindLabel = KINDS.find((k) => k.value === kind)?.label ?? "container";
  const selectedContainer = containers.find((c) => c.key === selected) ?? null;

  const statusOptions = useMemo(() => {
    const s = new Set<string>();
    for (const c of containers) if (c.status) s.add(c.status);
    return [...s].sort();
  }, [containers]);

  // Per-status counts for the pill filter (denominator = all containers for the
  // current kind, before any status/type/env filtering).
  const statusCounts = useMemo(() => {
    const m = new Map<string, number>();
    m.set("", containers.length);
    for (const c of containers) {
      if (c.status) m.set(c.status, (m.get(c.status) ?? 0) + 1);
    }
    return m;
  }, [containers]);

  const viewContainers = useMemo(() => {
    const base = containers.filter(
      (c) =>
        (!cStatus || c.status === cStatus) &&
        (!cLabel || (c.labels ?? []).includes(cLabel)) &&
        (kind !== "testexec" ||
          !cExecType ||
          (cExecType === "subtask" ? !!c.parentKey : !c.parentKey)) &&
        (kind !== "testexec" ||
          !cEnv ||
          (c.environments ?? []).includes(cEnv)),
    );
    return [...base].sort((a, b) => {
      let cmp: number;
      switch (cSortField) {
        case "summary":
          cmp = cmpStr(a.summary, b.summary) || keyCompare(a.key, b.key);
          break;
        case "status":
          cmp = cmpStr(a.status, b.status) || keyCompare(a.key, b.key);
          break;
        default:
          cmp = keyCompare(a.key, b.key);
      }
      return applyDir(cmp, cSortDesc);
    });
  }, [containers, cStatus, cLabel, cExecType, cEnv, kind, cSortField, cSortDesc]);

  // Distinct environments across the loaded executions, for the filter dropdown.
  const envOptions = useMemo(() => {
    const s = new Set<string>();
    for (const c of containers)
      for (const e of c.environments ?? []) if (e) s.add(e);
    return [...s].sort();
  }, [containers]);

  // Distinct labels across all loaded containers, for the label filter.
  const labelOptions = useMemo(() => {
    const s = new Set<string>();
    for (const c of containers)
      for (const l of c.labels ?? []) if (l) s.add(l);
    return [...s].sort();
  }, [containers]);

  useEffect(() => {
    if (viewContainers.length === 0) return;
    setSelected((cur) => {
      if (viewContainers.some((c) => c.key === cur)) return cur;
      return viewContainers[0].key;
    });
  }, [viewContainers]);

  // commitInlineRename saves the detail card's editable name (an inline path to
  // the same rename CRUD as the Actions menu).
  async function commitInlineRename() {
    setEditingName(false);
    const name = nameDraft.trim();
    if (!selectedContainer || !name || name === selectedContainer.summary) return;
    setError("");
    try {
      await EditContainer(profileId, selected, name);
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  // addEnvironment / removeEnvironment edit the selected execution's Test
  // Environments, computing the new set and queuing it via
  // SetContainerEnvironments (committed to Jira as a custom-field update).
  async function addEnvironment() {
    const name = envDraft.trim();
    if (!selectedContainer || !name) return;
    const cur = selectedContainer.environments ?? [];
    if (cur.includes(name)) {
      setEnvDraft("");
      return;
    }
    setEnvDraft("");
    setError("");
    try {
      await SetContainerEnvironments(profileId, selected, [...cur, name]);
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  async function removeEnvironment(name: string) {
    if (!selectedContainer) return;
    const cur = selectedContainer.environments ?? [];
    setError("");
    try {
      await SetContainerEnvironments(
        profileId,
        selected,
        cur.filter((e) => e !== name),
      );
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  // applyBatchEnv runs one environment operation across every execution in the
  // current filtered view (viewContainers), via the bulk BulkEditContainers
  // path. set_env replaces each execution's environments, so it is confirmed.
  async function applyBatchEnv() {
    const name = batchEnvName.trim();
    const keys = viewContainers.map((c) => c.key);
    if (keys.length === 0) return;
    if (batchEnvOp !== "set_env" && !name) return;
    if (
      batchEnvOp === "set_env" &&
      !window.confirm(
        `Set environments to "${name || "(none)"}" on ${keys.length} execution${keys.length === 1 ? "" : "s"}? ` +
          "This will replace their current environments.",
      )
    )
      return;
    setBatchEnvBusy(true);
    setError("");
    try {
      const res = await BulkEditContainers(profileId, keys, {
        operation: batchEnvOp,
        field: "",
        value: name,
      });
      if (res.failed && res.failed.length > 0) {
        setError(
          `Updated ${res.succeeded.length}, failed ${res.failed.length}: ${res.failed[0].error}`,
        );
      }
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBatchEnvBusy(false);
    }
  }

  async function seed() {
    setSeeding(true);
    setError("");
    try {
      await SeedSampleContainers(profileId);
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setSeeding(false);
    }
  }

  // cleanSample removes the sample containers a previous seed created, so a real
  // project can start fresh. Real synced data is untouched (FR-5).
  async function cleanSample() {
    if (
      !(await confirm({
        title: "Clean sample data",
        message:
          "Remove all sample Test Sets, Plans, and Executions created by 'Regenerate sample data'? " +
          "Your real synced containers won't be affected.",
        confirmLabel: "Delete",
        danger: true,
      }))
    )
      return;
    setError("");
    try {
      const removed = await CleanSampleData(profileId);
      await notice({
        title: "Sample data cleaned",
        message:
          removed > 0
            ? `Removed ${removed} sample container${removed === 1 ? "" : "s"}.`
            : "No sample data found for this project.",
      });
      setSelected("");
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  // toggleRun / toggleRunAll manage the bulk execution-result selection.
  function toggleRun(testKey: string) {
    setSelectedRuns((prev) => {
      const next = new Set(prev);
      if (next.has(testKey)) next.delete(testKey);
      else next.add(testKey);
      return next;
    });
  }

  function toggleRowSort(field: string) {
    if (rowSortField === field) {
      setRowSortDesc((d) => !d);
    } else {
      setRowSortField(field);
      setRowSortDesc(false);
    }
  }
  function rowSortIndicator(field: string): string {
    if (rowSortField !== field) return "";
    return rowSortDesc ? " ↓" : " ↑";
  }

  // applyBulkRunStatus sets one result on every selected Test in the execution.
  async function applyBulkRunStatus() {
    if (!selected || selectedRuns.size === 0) return;
    setError("");
    try {
      const res = await BulkSetTestRunStatus(
        profileId,
        selected,
        [...selectedRuns],
        bulkStatus,
      );
      if (res.failed && res.failed.length > 0) {
        setError(
          `Set ${res.succeeded.length}, failed ${res.failed.length}: ${res.failed[0].error}`,
        );
      }
      setSelectedRuns(new Set());
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  function openParent(parentKey: string) {
    const base = (jiraUrl ?? "").trim().replace(/\/+$/, "");
    if (base && !isDemo && !parentKey.startsWith("NEW-")) {
      BrowserOpenURL(`${base}/browse/${parentKey}`);
    }
  }
  // openBug mirrors BugsPanel.openBug: open the defect in Jira when a real URL
  // exists (skip in demo mode and for not-yet-committed NEW- keys).
  function openBug(bugKey: string) {
    const base = (jiraUrl ?? "").trim().replace(/\/+$/, "");
    if (base && !isDemo && !bugKey.startsWith("NEW-")) {
      BrowserOpenURL(`${base}/browse/${bugKey}`);
    }
  }

  async function newContainer() {
    const name = await prompt({
      title: `New ${kindLabel}`,
      placeholder: `${kindLabel} name`,
      submitLabel: "Create",
    });
    if (!name || !name.trim()) return;
    setError("");
    try {
      await CreateContainerAndAllocate(profileId, kind, name.trim(), []);
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  async function renameContainer() {
    if (!selected) return;
    const cur = containers.find((c) => c.key === selected);
    const name = await prompt({
      title: `Rename ${kindLabel}`,
      defaultValue: cur?.summary ?? "",
      submitLabel: "Rename",
    });
    if (name === null || !name.trim()) return;
    setError("");
    try {
      await EditContainer(profileId, selected, name.trim());
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  async function generatePytest(style: string) {
    if (!selected) return;
    setError("");
    try {
      const path = await ExportPytest(profileId, selected, style);
      if (path) await notice({ title: "Scaffold saved", message: path });
    } catch (e) {
      setError(errMsg(e));
    }
  }

  async function deleteContainer() {
    if (!selected) return;
    if (
      !(await confirm({
        title: `Delete ${kindLabel}`,
        message: `Delete this ${kindLabel}? Its test memberships are removed too (committed on sync).`,
        confirmLabel: "Delete",
        danger: true,
      }))
    )
      return;
    setError("");
    try {
      await DeleteContainer(profileId, selected);
      setSelected("");
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  useEffect(() => {
    setBugFor(null);
    setLinkBugFor(null);
  }, [mode, selected, kind]);

  useEffect(() => {
    setCStatus("");
    setCExecType("");
    setEnvToolsOpen(false);
  }, [kind]);

  // Close the batch-environment popover on Escape, matching the Menu component.
  useEffect(() => {
    if (!envToolsOpen) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") setEnvToolsOpen(false);
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [envToolsOpen]);

  // Collapse the bugs section whenever the user picks a different container so
  // a previously-expanded list from another container doesn't carry over.
  useEffect(() => {
    setBugsExpanded(false);
  }, [selected]);

  useEffect(() => {
    const rows = containersQuery.data;
    if (!rows) return;
    setSelected((cur) =>
      cur && rows.some((c) => c.key === cur) ? cur : rows.length > 0 ? rows[0].key : "",
    );
  }, [containersQuery.data]);

  useEffect(() => {
    setBoardPage(0);
  }, [rowSortField, rowSortDesc]);

  // The board / related bugs / member runs / roll-up now load via the
  // useContainer* queries above. Reset the board's view state when the selected
  // container changes (the old board load effect did this before fetching).
  useEffect(() => {
    setError("");
    setBoardPage(0);
    setSelectedRuns(new Set());
    setMemberFvFilter("");
    setMemberRunFilter("");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected]);

  // Reset the cached breakdown when the selected container changes, so a stale
  // one is never shown for a different plan/set.
  useEffect(() => {
    setBreakdown(null);
    setBreakdownFor("");
    setBreakdownStatus(null);
  }, [selected]);

  // openBreakdown opens the informational modal for one roll-up bucket, fetching
  // the member breakdown for the selected container on first use.
  async function openBreakdown(status: string) {
    if (!selected) return;
    setBreakdownStatus(status);
    if (breakdownFor === selected && breakdown) return;
    setBreakdownLoading(true);
    try {
      const rows = await GetRunRollupBreakdown(profileId, selected);
      setBreakdown(rows ?? []);
      setBreakdownFor(selected);
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBreakdownLoading(false);
    }
  }

  // Client-side paging of the member table.
  const allRows = useMemo(() => {
    let rows = board?.rows ?? [];
    // Apply the fix-version filter for Test Executions: keep only members whose
    // own fixVersions array (from the memberRuns enrichment) contains the chosen
    // value. When no filter is active every row is shown.
    if (kind === "testexec" && memberFvFilter) {
      rows = rows.filter((r) =>
        (memberRuns.get(r.testKey)?.fixVersions ?? []).includes(memberFvFilter),
      );
    }
    // Run-status filter from clicking the colorbar / a count badge. "(not run)"
    // matches members with no run result (blank), mirroring how the backend
    // buckets runCounts (blankAs(runStatus, "(not run)")).
    if (memberRunFilter) {
      rows = rows.filter((r) =>
        memberRunFilter === "(not run)"
          ? !r.runStatus
          : r.runStatus === memberRunFilter,
      );
    }
    return [...rows].sort((a, b) => {
      let cmp: number;
      switch (rowSortField) {
        case "summary":
          cmp = cmpStr(a.summary, b.summary) || keyCompare(a.testKey, b.testKey);
          break;
        case "status":
          cmp = cmpStr(a.status, b.status) || keyCompare(a.testKey, b.testKey);
          break;
        case "result":
          cmp =
            cmpStr(a.runStatus, b.runStatus) ||
            keyCompare(a.testKey, b.testKey);
          break;
        default:
          cmp = keyCompare(a.testKey, b.testKey);
      }
      return applyDir(cmp, rowSortDesc);
    });
  }, [board, kind, memberRuns, memberFvFilter, memberRunFilter, rowSortField, rowSortDesc]);

  // Toggle the run-status filter from a colorbar segment / badge and reset to
  // the first page so the (possibly shorter) filtered list starts at the top.
  const pickRunFilter = (label: string) => {
    setMemberRunFilter((cur) => (cur === label ? "" : label));
    setBoardPage(0);
  };
  const boardTotalPages = Math.max(
    1,
    Math.ceil(allRows.length / pageSize),
  );
  const safePage = Math.min(boardPage, boardTotalPages - 1);
  const pageRows = allRows.slice(
    safePage * pageSize,
    (safePage + 1) * pageSize,
  );

  return (
    <div
      data-tour="plans-body"
      className={`board${mode === "bugs" ? " board--bugs" : ""}${detailKey && mode !== "bugs" ? " board--with-exec-detail" : ""}`}
    >
      <div className="board-exec-body">
      <div className="containers-mode" data-tour="plans-tools">
        <button
          className={`seg-btn${mode === "containers" ? " seg-btn-active" : ""}`}
          onClick={() => setMode("containers")}
        >
          Containers
        </button>
        <button
          className={`seg-btn${mode === "bugs" ? " seg-btn-active" : ""}`}
          onClick={() => setMode("bugs")}
        >
          Bugs
        </button>
      </div>

      {mode === "bugs" ? (
        <BugsPanel
          profileId={profileId}
          refreshKey={refreshKey}
          jiraUrl={jiraUrl ?? ""}
          onOpenTest={onOpenTest ?? (() => {})}
        />
      ) : (
        <>
      <div className="board-head">
        <label className="board-picker board-picker--secondary" data-tour="plans-type">
          <span>Type</span>
          <select
            className="app-select container-type-select"
            value={kind}
            onChange={(e) => {
              // Switching type changes the member set, so close any open test
              // detail (it belongs to the previous type's selection).
              setKind(e.target.value);
              setDetailKey(null);
            }}
          >
            {KINDS.map((k) => (
              <option key={k.value} value={k.value}>
                {k.label}
              </option>
            ))}
          </select>
        </label>
        {/* Status filter: a compact dropdown between Type and the item picker
            (replaces the old pill row to save horizontal space). It reads as the
            tertiary control — recessed until a status is applied, when it adopts
            the accent so an active filter is obvious at a glance. */}
        <label className="board-picker board-picker--tertiary">
          <span>Status</span>
          <select
            className={`app-select container-filter-select${
              cStatus ? " is-filtering" : ""
            }`}
            value={cStatus}
            onChange={(e) => setCStatus(e.target.value)}
            title="Filter containers by status"
          >
            <option value="">All statuses ({statusCounts.get("") ?? 0})</option>
            {statusOptions.map((s) => (
              <option key={s} value={s}>
                {s} ({statusCounts.get(s) ?? 0})
              </option>
            ))}
          </select>
        </label>
        {labelOptions.length > 0 && (
          <label className="board-picker board-picker--tertiary">
            <span>Label</span>
            <select
              className={`app-select container-filter-select${
                cLabel ? " is-filtering" : ""
              }`}
              value={cLabel}
              onChange={(e) => setCLabel(e.target.value)}
              title="Filter containers by label"
            >
              <option value="">All labels</option>
              {labelOptions.map((l) => (
                <option key={l} value={l}>
                  {l}
                </option>
              ))}
            </select>
          </label>
        )}
        <label className="board-picker board-picker--primary" data-tour="plans-pick">
          <span>{kindLabel}</span>
          {loading ? (
            <span className="muted">Loading…</span>
          ) : (
            <SearchableSelect
              className="container-exec-filter"
              value={selected}
              onChange={setSelected}
              disabled={viewContainers.length === 0}
              title={`Pick a ${kindLabel} (type to filter)`}
              placeholder={
                viewContainers.length === 0 ? "None" : `Select a ${kindLabel}…`
              }
              options={viewContainers.map((c) => ({
                value: c.key,
                label: `${c.key} · ${c.summary}`,
                className: c.parentKey ? "is-subtask" : undefined,
              }))}
            />
          )}
        </label>

        <div className="board-head-actions">
          <button
            className="btn"
            onClick={async () => {
              setSyncing(true);
              setError("");
              try {
                await SyncContainers(profileId);
                onChanged();
              } catch (e) {
                setError(errMsg(e));
              } finally {
                setSyncing(false);
              }
            }}
            disabled={syncing}
            title="Refresh just the Test Sets / Plans / Executions from Jira (partial sync)"
          >
            {syncing ? "Syncing…" : "Sync"}
          </button>
          <button className="btn btn-primary" onClick={newContainer} title={`New ${kindLabel}`} data-tour="plans-new">
            + New
          </button>
          {kind === "testexec" && (
            <button
              className="btn"
              onClick={() => setShowJUnitNewExec(true)}
              title="Create a new Test Execution from a JUnit XML report"
            >
              Import JUnit
            </button>
          )}
          <Menu
            label="Actions"
            align="right"
            triggerClassName="btn"
            title={`${kindLabel} actions`}
            items={[
              {
                key: "rename",
                label: `Rename ${kindLabel}…`,
                onClick: renameContainer,
                disabled: !selected,
              },
              {
                key: "pytest",
                label: "Generate pytest…",
                onClick: () => generatePytest("function"),
                disabled: !selected,
                title:
                  "Plain pytest: one @pytest.mark.xray function per test",
              },
              {
                key: "pytest-unittest",
                label: "Generate pytest (unittest class)…",
                onClick: () => generatePytest("unittest"),
                disabled: !selected,
                title:
                  "unittest.TestCase subclass: one test method per test " +
                  "(runs under python -m unittest and pytest)",
              },
              {
                key: "delete",
                label: `Delete ${kindLabel}…`,
                onClick: deleteContainer,
                disabled: !selected,
                danger: true,
              },
              { key: "d", divider: true },
              // Regenerating sample data is a demo-only aid — hidden for real
              // projects so fake containers can't be seeded into them (FR-5).
              ...(isDemo
                ? [
                    {
                      key: "seed",
                      label: seeding
                        ? "Generating…"
                        : "Regenerate sample data",
                      onClick: seed,
                      disabled: seeding,
                      title:
                        "Regenerate sample sets / plans / executions from synced tests",
                    },
                  ]
                : []),
              {
                key: "clean",
                label: "Clean sample data…",
                onClick: cleanSample,
                danger: true,
                title:
                  "Remove sample containers created by 'Regenerate sample data' (real data untouched)",
              },
            ]}
          />
        </div>
      </div>

      <div className="container-filter-bar">
        {kind === "testexec" && (
          <select
            className="container-status-filter app-select"
            value={cExecType}
            onChange={(e) => setCExecType(e.target.value)}
            title="Filter by execution type"
          >
            <option value="">All executions</option>
            <option value="standalone">Standalone</option>
            <option value="subtask">Sub-task</option>
          </select>
        )}
        {caps.supportsEnvironments && kind === "testexec" && envOptions.length > 0 && (
          <select
            className="container-status-filter app-select"
            value={cEnv}
            onChange={(e) => setCEnv(e.target.value)}
            title="Filter by test environment"
          >
            <option value="">All environments</option>
            {envOptions.map((e) => (
              <option key={e} value={e}>
                {e}
              </option>
            ))}
          </select>
        )}
        {/* Consolidated-results summary for the selected Test Plan / Set, shown
            inline on the filter row (moved here from the detail card; the label
            filter it replaced now sits beside the Status filter above). */}
        {kind !== "testexec" && rollup && rollup.total > 0 && (
          <div className="container-rollup">
            <span
              className="container-rollup-label"
              title={
                `Each test in this ${kindLabel} gets one combined result across the ` +
                `${rollup.execCount} Test Execution${rollup.execCount === 1 ? "" : "s"} that ran it. ` +
                `The worst result wins, so any FAIL makes the test FAIL overall. These badges count ` +
                `tests by that combined result; "(not run)" means no execution has recorded a result yet. ` +
                `Click a badge to see which tests are behind it.`
              }
            >
              Consolidated results across {rollup.execCount} execution{rollup.execCount === 1 ? "" : "s"}
              <span className="rollup-info" aria-hidden="true">ⓘ</span>
            </span>
            <div className="board-counts">
              {rollup.passed > 0 && (
                <RunBadge
                  status="PASS"
                  count={rollup.passed}
                  onPick={() => openBreakdown("PASS")}
                  pickHint="See the tests behind this result"
                />
              )}
              {rollup.failed > 0 && (
                <RunBadge
                  status="FAIL"
                  count={rollup.failed}
                  onPick={() => openBreakdown("FAIL")}
                  pickHint="See the tests behind this result"
                />
              )}
              {rollup.executing > 0 && (
                <RunBadge
                  status="EXECUTING"
                  count={rollup.executing}
                  onPick={() => openBreakdown("EXECUTING")}
                  pickHint="See the tests behind this result"
                />
              )}
              {rollup.aborted > 0 && (
                <RunBadge
                  status="ABORTED"
                  count={rollup.aborted}
                  onPick={() => openBreakdown("ABORTED")}
                  pickHint="See the tests behind this result"
                />
              )}
              {rollup.blocked > 0 && (
                <RunBadge
                  status="BLOCKED"
                  count={rollup.blocked}
                  onPick={() => openBreakdown("BLOCKED")}
                  pickHint="See the tests behind this result"
                />
              )}
              {rollup.notRun > 0 && (
                <RunBadge
                  status="(not run)"
                  count={rollup.notRun}
                  onPick={() => openBreakdown("(not run)")}
                  pickHint="See the tests behind this result"
                />
              )}
            </div>
          </div>
        )}
        <span className="muted container-filter-count">
          {viewContainers.length} of {containers.length}
        </span>

        {/* View controls (sort) and the batch-environment action sit in their
            own right-aligned group so they read as tools, not status filters
            (#310), while staying on the filter row to save vertical space. */}
        <div className="filter-tools">
          <SortControl
            fields={[
              { value: "key", label: "Key" },
              { value: "summary", label: "Name" },
              { value: "status", label: "Status" },
            ]}
            field={cSortField}
            desc={cSortDesc}
            onChange={(f, d) => {
              setCSortField(f);
              setCSortDesc(d);
            }}
          />
          {caps.supportsEnvironments && kind === "testexec" && (
            <div className="menu">
              <button
                className="btn"
                onClick={() => setEnvToolsOpen((o) => !o)}
                title="Apply an environment change to every execution currently shown"
                aria-haspopup="dialog"
                aria-expanded={envToolsOpen}
              >
                Set env on shown
                <span className="menu-caret" aria-hidden="true">
                  ▾
                </span>
              </button>
              {envToolsOpen && (
                <>
                  <div
                    className="menu-backdrop"
                    onClick={() => setEnvToolsOpen(false)}
                  />
                  <div className="env-tools-panel menu-panel-right" role="dialog">
                    <span className="env-tools-title">
                      Set environment on the {viewContainers.length} shown
                    </span>
                    <div className="env-tools-row">
                      <select
                        className="container-status-filter app-select"
                        value={batchEnvOp}
                        onChange={(e) =>
                          setBatchEnvOp(
                            e.target.value as "add_env" | "remove_env" | "set_env",
                          )
                        }
                        title="Batch environment operation"
                      >
                        <option value="add_env">Add env</option>
                        <option value="remove_env">Remove env</option>
                        <option value="set_env">Set env</option>
                      </select>
                      <input
                        className="container-env-add app-select"
                        list="container-env-names"
                        value={batchEnvName}
                        placeholder="Environment…"
                        onChange={(e) => setBatchEnvName(e.target.value)}
                      />
                      <datalist id="container-env-names">
                        {envOptions.map((e) => (
                          <option key={e} value={e} />
                        ))}
                      </datalist>
                    </div>
                    <button
                      className="btn btn-primary env-tools-apply"
                      onClick={async () => {
                        await applyBatchEnv();
                        setEnvToolsOpen(false);
                      }}
                      disabled={
                        batchEnvBusy ||
                        viewContainers.length === 0 ||
                        (batchEnvOp !== "set_env" && !batchEnvName.trim())
                      }
                      title="Apply to all executions currently shown by the filter"
                    >
                      {batchEnvBusy
                        ? "Applying…"
                        : `Apply to ${viewContainers.length}`}
                    </button>
                  </div>
                </>
              )}
            </div>
          )}
        </div>
      </div>

      {(listError || error || boardQuery.error) && (
        <div className="error-text">
          {listError || error || errMsg(boardQuery.error)}
        </div>
      )}

      {!loading && containers.length === 0 && (
        <p className="muted">
          You don't have any {kindLabel}s yet. Create one, run a sync, or
          generate sample data.
        </p>
      )}

      {selectedContainer && (
        <div className="container-card">
          <div className="container-card-top">
            <button
              type="button"
              className="collapse-caret"
              onClick={() => setCardCollapsed(!cardCollapsed)}
              aria-expanded={!cardCollapsed}
              title={cardCollapsed ? "Show details" : "Hide details"}
            >
              {cardCollapsed ? "▸" : "▾"}
            </button>
            <span className={`kind-badge kind-${kind}`}>{kindLabel}</span>
            <span className="mono container-card-key">
              {selectedContainer.key}
            </span>
            {selectedContainer.status && (
              <span className="status-pill">{selectedContainer.status}</span>
            )}
            <span className="container-card-count">
              {(board?.rows.length ?? 0).toLocaleString()} test
              {(board?.rows.length ?? 0) === 1 ? "" : "s"}
            </span>
            <button
              className="btn container-card-add"
              onClick={() => setShowAdd(true)}
              title={`Assign tests to this ${kindLabel}`}
            >
              + Add tests
            </button>
            {kind === "testexec" && (
              <button
                className="btn"
                onClick={() => setShowJUnitImport(true)}
                title="Import test run results from a JUnit XML report"
              >
                Import results (JUnit XML)
              </button>
            )}
          </div>

          {!cardCollapsed && (
            <>
          {selectedContainer.parentKey && (
            <div className="container-parent">
              <button
                className="mono container-parent-link"
                onClick={() => openParent(selectedContainer.parentKey)}
                title={`Open parent ${selectedContainer.parentKey} in Jira`}
              >
                ↳ {selectedContainer.parentKey}
              </button>
              {selectedContainer.issueType && (
                <span className="container-parent-type">
                  {selectedContainer.issueType}
                </span>
              )}
            </div>
          )}

          {caps.supportsEnvironments && kind === "testexec" && (
            <div className="container-environments">
              <span className="container-env-label">Environments</span>
              <div className="container-env-chips">
                {(selectedContainer.environments ?? []).length === 0 && (
                  <span className="muted">None</span>
                )}
                {(selectedContainer.environments ?? []).map((env) => (
                  <span key={env} className="env-chip">
                    {env}
                    <button
                      className="env-chip-remove"
                      title={`Remove ${env}`}
                      aria-label={`Remove ${env}`}
                      onClick={() => removeEnvironment(env)}
                    >
                      ✕
                    </button>
                  </span>
                ))}
                <input
                  className="container-env-add app-select"
                  value={envDraft}
                  placeholder="Add environment…"
                  onChange={(e) => setEnvDraft(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") addEnvironment();
                  }}
                  title="Type an environment name and press Enter"
                />
              </div>
            </div>
          )}

          {kind === "testexec" &&
            (selectedContainer.fixVersions ?? []).length > 0 && (
              <div className="container-environments">
                <span className="container-env-label">Fix version(s)</span>
                <div className="container-env-chips">
                  {memberFvFilter && (
                    <button
                      key="__all"
                      className="env-chip fix-version-chip fix-version-chip--all"
                      title="Clear fix-version filter (show all members)"
                      onClick={() => setMemberFvFilter("")}
                    >
                      All
                    </button>
                  )}
                  {(selectedContainer.fixVersions ?? []).map((fv) => (
                    <button
                      key={fv}
                      className={`env-chip fix-version-chip${memberFvFilter === fv ? " fix-version-chip--active" : ""}`}
                      title={
                        memberFvFilter === fv
                          ? `Showing only members with fix version ${fv} (click to clear)`
                          : `Filter member table to fix version ${fv}`
                      }
                      onClick={() =>
                        setMemberFvFilter((cur) => (cur === fv ? "" : fv))
                      }
                    >
                      {fv}
                    </button>
                  ))}
                </div>
              </div>
            )}

          {editingName ? (
            <input
              className="container-card-name-edit"
              autoFocus
              value={nameDraft}
              onChange={(e) => setNameDraft(e.target.value)}
              onBlur={commitInlineRename}
              onKeyDown={(e) => {
                if (e.key === "Enter") commitInlineRename();
                if (e.key === "Escape") setEditingName(false);
              }}
            />
          ) : (
            <h2
              className="container-card-name"
              title="Click to rename"
              onClick={() => {
                setNameDraft(selectedContainer.summary);
                setEditingName(true);
              }}
            >
              {selectedContainer.summary || "(untitled)"}
            </h2>
          )}

          {board && board.runCounts.length > 0 && (
            <div className="container-card-runs">
              <RunBar
                counts={board.runCounts}
                active={memberRunFilter}
                onPick={pickRunFilter}
              />
              <div className="board-counts">
                {board.runCounts.map((b) => (
                  <RunBadge
                    key={b.label}
                    status={b.label}
                    count={b.count}
                    active={memberRunFilter === b.label}
                    onPick={() => pickRunFilter(b.label)}
                  />
                ))}
              </div>
            </div>
          )}
            </>
          )}

          {/* Related defects reached through this container's member Tests -
              shown for executions, or for any container that has linked bugs.
              Surfaces a bug that reaches this execution only via a cross-project
              member Test, which the per-test Bugs panel cannot show (#219).
              The section is collapsible so a large bug list can never push the
              member table below off-screen (-274). */}
          {(kind === "testexec" || relatedBugs.length > 0) && (
            <div className="tp-bugs-collapsible">
              <button
                className="tp-bugs-header"
                onClick={() => setBugsExpanded((e) => !e)}
                title={bugsExpanded ? "Collapse related bugs" : "Expand related bugs"}
              >
                <span
                  className="tp-bugs-chevron"
                  style={{ transform: bugsExpanded ? "rotate(90deg)" : "none" }}
                >
                  ▶
                </span>
                <span>Related bugs</span>
                {relatedBugs.length > 0 && (
                  <span className="container-bugs-count">
                    ({relatedBugs.length})
                  </span>
                )}
              </button>
              {bugsExpanded && (
                <div className="tp-bugs-body">
                  {relatedBugs.length === 0 ? (
                    <span className="muted">None</span>
                  ) : (
                    <ul className="container-bugs-list">
                      {relatedBugs.map((b) => (
                        <li key={b.key} className="container-bug">
                          <button
                            className="mono container-bug-key"
                            onClick={() => openBug(b.key)}
                            title={
                              isDemo || !jiraUrl
                                ? b.key
                                : `Open ${b.key} in Jira`
                            }
                          >
                            {b.key}
                          </button>
                          <span className="container-bug-summary">{b.summary}</span>
                          {b.status && (
                            <span className="status-pill container-bug-status">
                              {b.status}
                            </span>
                          )}
                        </li>
                      ))}
                    </ul>
                  )}
                </div>
              )}
            </div>
          )}
        </div>
      )}

      {kind === "testexec" && selectedRuns.size > 0 && (
        <div className="board-bulk">
          <span className="bulk-count">{selectedRuns.size} selected</span>
          <span className="muted">Set result:</span>
          <select
            className="run-select"
            value={bulkStatus}
            onChange={(e) => setBulkStatus(e.target.value)}
          >
            {RUN_STATUSES.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
          <button className="btn btn-primary" onClick={applyBulkRunStatus}>
            Apply to selected
          </button>
          <button className="btn" onClick={() => setSelectedRuns(new Set())}>
            Clear
          </button>
        </div>
      )}

      <div className="board-scroll">
      {board && containers.length > 0 && (
        <table className="board-table">
          <thead>
            <tr>
              {kind === "testexec" && (
                <th className="board-check-col">
                  <input
                    type="checkbox"
                    checked={
                      pageRows.length > 0 &&
                      pageRows.every((r) => selectedRuns.has(r.testKey))
                    }
                    onChange={(e) => {
                      const checked = e.target.checked;
                      setSelectedRuns((prev) => {
                        const next = new Set(prev);
                        pageRows.forEach((r) =>
                          checked
                            ? next.add(r.testKey)
                            : next.delete(r.testKey),
                        );
                        return next;
                      });
                    }}
                    title="Select all on this page"
                  />
                </th>
              )}
              <th
                className="board-sort-th"
                onClick={() => toggleRowSort("key")}
                title="Sort by test key"
              >
                Test{rowSortIndicator("key")}
              </th>
              <th
                className="board-sort-th"
                onClick={() => toggleRowSort("summary")}
                title="Sort by summary"
              >
                Summary{rowSortIndicator("summary")}
              </th>
              <th
                className="board-sort-th"
                onClick={() => toggleRowSort("status")}
                title="Sort by status"
              >
                Status{rowSortIndicator("status")}
              </th>
              <th
                className="board-sort-th"
                onClick={() => toggleRowSort("result")}
                title="Sort by run result"
              >
                Execution{rowSortIndicator("result")}
              </th>
              {kind === "testexec" && <th title="Run date (finished or started)">Date</th>}
              {kind === "testexec" && <th title="Executed by">By</th>}
              {kind === "testexec" && <th title="Environment">Environment</th>}
              {kind === "testexec" && <th title="Member test's own Fix Version(s)">Fix Version</th>}
              {kind === "testexec" && <th title="Defects linked to this run">Defects</th>}
              {kind === "testexec" && <th title="Remark / comment on this run">Remarks</th>}
              <th aria-label="Remove" />
            </tr>
          </thead>
          <tbody>
            {allRows.length === 0 ? (
              <tr>
                <td colSpan={kind === "testexec" ? 12 : 5} className="muted">
                  This {kindLabel.toLowerCase()} doesn't have any tests yet.
                  Use "+ Add tests" to add some.
                </td>
              </tr>
            ) : (
              pageRows.map((r) => (
                <tr
                  key={r.testKey}
                  className={selectedRuns.has(r.testKey) ? "board-row-sel" : ""}
                >
                  {kind === "testexec" && (
                    <td className="board-check-col">
                      <input
                        type="checkbox"
                        checked={selectedRuns.has(r.testKey)}
                        onChange={() => toggleRun(r.testKey)}
                      />
                    </td>
                  )}
                  <td className="mono">
                    {r.testKey}
                    {r.isExternal && (
                      <span
                        className="ext-badge"
                        title="This test lives in a different Jira project than this profile"
                      >
                        ext
                      </span>
                    )}
                    <button
                      className="btn-icon"
                      title={`Open ${r.testKey} detail here`}
                      onClick={() => {
                        setDetailKey(r.testKey);
                        setDetailVersion((v) => v + 1);
                      }}
                      style={{ fontSize: "0.75rem", padding: "0 0.25rem", marginLeft: "0.25rem" }}
                    >
                      ↗
                    </button>
                  </td>
                  <td>{r.summary}</td>
                  <td>{r.status || "—"}</td>
                  <td>
                    {kind === "testexec" ? (
                      <select
                        className={`run-select run-${(r.runStatus || "todo").toLowerCase()}`}
                        value={r.runStatus || ""}
                        onChange={(e) => setRunStatus(r.testKey, e.target.value)}
                        title="Set this test's result in this execution"
                      >
                        {!r.runStatus && <option value="">Set result…</option>}
                        {RUN_STATUSES.map((s) => (
                          <option key={s} value={s}>
                            {s}
                          </option>
                        ))}
                      </select>
                    ) : r.runStatus ? (
                      <span
                        className={`run-badge run-${r.runStatus.toLowerCase()}`}
                      >
                        {r.runStatus}
                      </span>
                    ) : (
                      <span className="muted">not run</span>
                    )}
                  </td>
                  {kind === "testexec" && (
                    <ExecRunCells
                      run={memberRuns.get(r.testKey)}
                      activeFixVersion={memberFvFilter}
                    />
                  )}
                  {kind === "testexec" && (
                    <DefectsCell
                      testKey={r.testKey}
                      defects={memberRuns.get(r.testKey)?.defects ?? []}
                      isDemo={isDemo}
                      onOpenBug={openBug}
                      onUnlink={unlinkBug}
                      onLinkClick={() => setLinkBugFor(r.testKey)}
                    />
                  )}
                  {kind === "testexec" && (
                    <RemarksCell
                      testKey={r.testKey}
                      comment={memberRuns.get(r.testKey)?.comment ?? ""}
                      onSave={setRunComment}
                    />
                  )}
                  <td className="board-remove-cell">
                    {kind === "testexec" &&
                      /^fail/i.test(r.runStatus || "") && (
                        <button
                          className="btn btn-ghost board-bug"
                          title="Create a bug for this failed test"
                          aria-label="Create bug for this failed test"
                          onClick={() => setBugFor({ testKey: r.testKey, summary: r.summary })}
                        >
                          🐞
                        </button>
                      )}
                    <button
                      className="btn btn-ghost board-remove"
                      onClick={() => removeTest(r.testKey)}
                      title={`Remove from this ${kindLabel}`}
                    >
                      ✕
                    </button>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      )}
      </div>

      {board && allRows.length > 0 && (
        <div className="board-pager">
          <label className="board-pagesize">
            <span className="muted">Rows per page</span>
            <select
              className="app-select"
              value={pageSize}
              onChange={(e) => {
                setPageSize(Number(e.target.value));
                setBoardPage(0);
              }}
            >
              {PAGE_SIZE_OPTIONS.map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </select>
          </label>
          <span className="muted board-pager-range">
            {(safePage * pageSize + 1).toLocaleString()}–
            {Math.min(
              (safePage + 1) * pageSize,
              allRows.length,
            ).toLocaleString()}{" "}
            of {allRows.length.toLocaleString()} · page {safePage + 1} of{" "}
            {boardTotalPages}
          </span>
          <span className="board-pager-nav">
            <button
              className="btn"
              disabled={safePage === 0}
              onClick={() => setBoardPage((p) => Math.max(0, p - 1))}
            >
              ‹ Prev
            </button>
            <button
              className="btn"
              disabled={safePage >= boardTotalPages - 1}
              onClick={() =>
                setBoardPage((p) => Math.min(boardTotalPages - 1, p + 1))
              }
            >
              Next ›
            </button>
          </span>
        </div>
      )}
        </>
      )}

      {showAdd && selectedContainer && (
        <AddTestsModal
          profileId={profileId}
          containerKey={selected}
          existingKeys={(board?.rows ?? []).map((r) => r.testKey)}
          onCancel={() => setShowAdd(false)}
          onDone={() => {
            setShowAdd(false);
            onChanged();
          }}
        />
      )}

      {bugFor && (
        <CreateBugModal
          profileId={profileId}
          testKey={bugFor.testKey}
          testSummary={bugFor.summary}
          execKey={selected}
          onClose={() => setBugFor(null)}
          onCreated={() => {
            setBugFor(null);
            onChanged();
          }}
        />
      )}

      {linkBugFor && selected && (
        <LinkBugPicker
          profileId={profileId}
          execKey={selected}
          testKey={linkBugFor}
          existingKeys={memberRuns.get(linkBugFor)?.defects ?? []}
          onClose={() => setLinkBugFor(null)}
          onLinked={onChanged}
        />
      )}

      {showJUnitImport && selected && kind === "testexec" && (
        <JUnitImportModal
          profileId={profileId}
          execKey={selected}
          onCancel={() => setShowJUnitImport(false)}
          onApplied={(succeeded, failed) => {
            setShowJUnitImport(false);
            onChanged();
            notice({
              title: "JUnit import applied",
              message:
                `${succeeded} result${succeeded !== 1 ? "s" : ""} queued. Commit them from the Pending list.` +
                (failed > 0 ? ` (${failed} failed)` : ""),
            });
          }}
        />
      )}

      {showJUnitNewExec && kind === "testexec" && (
        <JUnitNewExecModal
          profileId={profileId}
          onCancel={() => setShowJUnitNewExec(false)}
          onApplied={(result) => {
            setShowJUnitNewExec(false);
            onChanged();
            notice({
              title: "Execution created",
              message:
                `Created ${result.execKey}: ${result.created} test${result.created !== 1 ? "s" : ""} created, ` +
                `${result.allocated} allocated, ${result.resultsSet} result${result.resultsSet !== 1 ? "s" : ""} set` +
                (result.failed && result.failed.length > 0
                  ? ` (${result.failed.length} failed)`
                  : "") +
                ". Queued, commit it from the Pending list.",
            });
          }}
        />
      )}

      {breakdownStatus !== null && (
        <RollupBreakdownModal
          kindLabel={kindLabel}
          containerKey={selected}
          status={breakdownStatus}
          members={breakdown ?? []}
          loading={breakdownLoading}
          onClose={() => setBreakdownStatus(null)}
        />
      )}

      {promptUI}
      {confirmUI}
      {noticeUI}
      </div>

      {detailKey && mode !== "bugs" && (
        <TestDetail
          profileId={profileId}
          testKey={detailKey}
          version={detailVersion}
          pendingForTest={[]}
          folders={[]}
          jiraUrl={jiraUrl ?? ""}
          readOnly
          onClose={() => setDetailKey(null)}
          onEdited={() => {}}
        />
      )}
    </div>
  );
}

// ExecRunCells renders the four read-only run-context cells (Date, By,
// Environment, Fix Version) for one member row in a Test Execution. Rendered
// as a fragment so the cells sit inline in the <tr> alongside the editable
// result cell.
function ExecRunCells({
  run,
  activeFixVersion,
}: {
  run: ExecMemberRun | undefined;
  activeFixVersion: string;
}) {
  const dateStr = run?.finishedAt || run?.startedAt || "";
  const fvs = run?.fixVersions ?? [];
  const fvLabel = fvs.length > 0 ? fvs.join(", ") : "—";
  return (
    <>
      <td className="muted board-run-date">
        {dateStr ? formatRunDate(dateStr) : "—"}
      </td>
      <td className="muted board-run-by">
        {run?.executedBy || "—"}
      </td>
      <td className="muted board-run-env">
        {run?.environment || "—"}
      </td>
      <td
        className={`muted board-run-fv${activeFixVersion && fvs.includes(activeFixVersion) ? " board-run-fv--match" : ""}`}
        title={fvs.length > 1 ? fvs.join(", ") : undefined}
      >
        {fvLabel}
      </td>
    </>
  );
}

// DefectsCell renders one member row's linked defects as removable chips plus
// a "＋" control that opens the LinkBugPicker for that row (RND_P_4TFINT_05-296).
// Each chip links to Jira when a real profile URL exists (mirrors
// ContainersView.openBug's isDemo / NEW- key gating).
function DefectsCell({
  testKey,
  defects,
  isDemo,
  onOpenBug,
  onUnlink,
  onLinkClick,
}: {
  testKey: string;
  defects: string[];
  isDemo: boolean;
  onOpenBug: (bugKey: string) => void;
  onUnlink: (testKey: string, bugKey: string) => void;
  onLinkClick: () => void;
}) {
  return (
    <td className="board-defects-cell">
      <div className="defect-chip-row">
        {defects.map((d) => (
          <span key={d} className="defect-chip">
            {!isDemo && !d.startsWith("NEW-") ? (
              <button
                type="button"
                className="defect-chip-key"
                onClick={() => onOpenBug(d)}
                title={`Open ${d} in Jira`}
              >
                {d}
              </button>
            ) : (
              <span className="defect-chip-key">{d}</span>
            )}
            <button
              type="button"
              className="defect-chip-remove"
              onClick={() => onUnlink(testKey, d)}
              title={`Unlink ${d} from this run`}
              aria-label={`Unlink ${d} from this run`}
            >
              ✕
            </button>
          </span>
        ))}
        <button
          type="button"
          className="btn btn-ghost defect-chip-add"
          onClick={onLinkClick}
          title="Link an existing bug to this run"
          aria-label="Link an existing bug to this run"
        >
          ＋
        </button>
      </div>
    </td>
  );
}

// RemarksCell is an inline-editable text control for one member row's run
// comment, seeded from the loaded/staged value and saved on blur or Enter
// (RND_P_4TFINT_05-296). The draft resyncs to the incoming comment whenever
// the field is not actively focused, so a refresh (after save, sync, or
// another edit) shows up immediately without clobbering in-progress typing.
function RemarksCell({
  testKey,
  comment,
  onSave,
}: {
  testKey: string;
  comment: string;
  onSave: (testKey: string, comment: string) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(comment);
  // Set on Escape so the immediately-following blur discards the edit instead
  // of saving it. A ref (not state) so the blur handler sees it even if it
  // fires before React re-renders with the reset draft.
  const cancelled = useRef(false);

  useEffect(() => {
    if (!editing) setDraft(comment);
  }, [comment, editing]);

  function handleBlur() {
    setEditing(false);
    if (cancelled.current) {
      cancelled.current = false;
      return;
    }
    const value = draft.trim();
    if (value !== comment) onSave(testKey, value);
  }

  return (
    <td className="board-remarks-cell">
      <input
        className="remarks-input"
        value={draft}
        placeholder="Add remark…"
        title={comment || undefined}
        onFocus={() => setEditing(true)}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={handleBlur}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            (e.target as HTMLInputElement).blur();
          } else if (e.key === "Escape") {
            cancelled.current = true;
            setDraft(comment);
            (e.target as HTMLInputElement).blur();
          }
        }}
      />
    </td>
  );
}

// formatRunDate formats an ISO date/time string to a compact local date+time
// string (YYYY-MM-DD HH:MM), matching the pattern used elsewhere in the app
// (e.g. TestDetail run-history rows). Returns "" for empty/invalid input.
function formatRunDate(iso: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  const pad = (n: number) => String(n).padStart(2, "0");
  return (
    `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ` +
    `${pad(d.getHours())}:${pad(d.getMinutes())}`
  );
}

// RunBar is a compact stacked bar of run-status proportions for the selected
// container, a glanceable view of how its tests are doing. When onPick is
// given, each segment is clickable and filters the member table to that run
// status; the active segment stays lit while the rest dim.
function RunBar({
  counts,
  active,
  onPick,
}: {
  counts: Bucket[];
  active?: string;
  onPick?: (label: string) => void;
}) {
  const sum = counts.reduce((a, b) => a + b.count, 0) || 1;
  const filtering = !!active;
  return (
    <div
      className={`run-bar${onPick ? " run-bar--clickable" : ""}${filtering ? " run-bar--filtering" : ""}`}
      title={
        onPick
          ? "Run-status distribution. Click a segment to filter the list"
          : "Run-status distribution"
      }
    >
      {counts.map((b) => (
        <span
          key={b.label}
          className={`${runSegClass(b.label)}${active === b.label ? " run-seg--active" : ""}`}
          style={{ width: `${(b.count / sum) * 100}%` }}
          title={`${b.label}: ${b.count}${onPick ? " (click to filter)" : ""}`}
          role={onPick ? "button" : undefined}
          onClick={onPick ? () => onPick(b.label) : undefined}
        />
      ))}
    </div>
  );
}

function runSegClass(label: string): string {
  if (label === "(not run)") return "run-seg";
  return `run-seg run-${label.toLowerCase()}`;
}

// RunBadge shows a run-status count. When onPick is given it acts as a filter
// toggle for the member table (active = currently filtering to this status);
// without onPick it is a plain, non-interactive label (e.g. the roll-up).
function RunBadge({
  status,
  count,
  active,
  onPick,
  pickHint,
}: {
  status: string;
  count: number;
  active?: boolean;
  onPick?: () => void;
  // Overrides the click tooltip. Defaults to the member-table filter wording.
  pickHint?: string;
}) {
  const base =
    status === "(not run)" ? "run-badge" : `run-badge run-${status.toLowerCase()}`;
  const cls = `${base}${onPick ? " run-badge--click" : ""}${active ? " run-badge--active" : ""}`;
  const label = status === "(not run)" ? "not run" : status;
  return (
    <span
      className={cls}
      role={onPick ? "button" : undefined}
      onClick={onPick}
      title={onPick ? (pickHint ?? `Filter the list to ${label}`) : undefined}
    >
      {label} {count}
    </span>
  );
}
