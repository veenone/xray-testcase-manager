import { useEffect, useRef, useState } from "react";
import { useProfile } from "../contexts/ProfileContext";
import type { CSSProperties } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useViewState } from "../lib/viewState";
import {
  ListMatchingKeys,
  ListStatuses,
  ExportTests,
  CreateSavedView,
  ListSavedViews,
  DeleteSavedView,
  errMsg,
} from "../api";
import { usePrompt } from "./usePrompt";
import { useNotice } from "./useNotice";
import { formatDate } from "../dates";
import { REVIEW_ENABLED, useCapabilities } from "../features";
import type {
  TestQuery,
  TestCase,
  PendingChange,
  SavedView,
} from "../api";
import { useTests } from "../queries/tests";
import { useTestCallerKeys } from "../queries/testCalls";

interface Props {
  folderId: string;
  containerKey: string;
  component: string;
  selectedKey: string | null;
  pendingByTestKey: Map<string, PendingChange[]>;
  selectedSet: Set<string>;
  onSelect: (key: string) => void;
  onToggleSelect: (key: string) => void;
  onToggleSelectPage: (keys: string[]) => void;
  onSelectAllMatching: (keys: string[]) => void;
  onSync?: () => void;
  syncing?: boolean;
}

// Page-size choices for the grid pager. The backend caps a page at 500.
const PAGE_SIZE_OPTIONS = [50, 100, 200, 500];
const DEFAULT_PAGE_SIZE = 100;
// Stable empty fallback so an unloaded caller-keys query doesn't mint a new Set
// each render.
const EMPTY_CALLER_KEYS: ReadonlySet<string> = new Set();

type SortCol = "key" | "summary" | "status" | "updated";

// Configurable grid columns (FR-11.3). The select-checkbox column is fixed and
// not part of this list. Column visibility + order persist in localStorage.
type ColKey =
  | "key"
  | "summary"
  | "status"
  | "priority"
  | "labels"
  | "components"
  | "execType"
  | "updated";

interface ColDef {
  key: ColKey;
  label: string;
  sortCol?: SortCol;
}

const ALL_COLUMNS: ColDef[] = [
  { key: "key", label: "Key", sortCol: "key" },
  { key: "summary", label: "Summary", sortCol: "summary" },
  { key: "status", label: "Status", sortCol: "status" },
  { key: "priority", label: "Priority" },
  { key: "labels", label: "Labels" },
  { key: "components", label: "Components" },
  { key: "execType", label: "Exec type" },
  { key: "updated", label: "Updated", sortCol: "updated" },
];

// EXEC_TYPE_OPTIONS is the fixed Xray Test Type (execution type) vocabulary
// offered in the Browse filter bar and the bulk-edit / detail editors.
const EXEC_TYPE_OPTIONS = ["Manual", "Automated", "Generic", "Cucumber"];

// Fixed column geometry. Row virtualization positions rows independently, so
// they only line up with each other and the sticky header when every column
// has a stable width (P2). `summary` grows to absorb slack; the rest are fixed,
// and content that overflows a cell is clipped with an ellipsis. Rows are a
// fixed height so the virtualizer's size estimate is exact.
const SELECT_W = 40;
const ROW_HEIGHT = 34;
const COL_WIDTH: Record<ColKey, number> = {
  key: 130,
  summary: 260,
  status: 130,
  priority: 100,
  labels: 200,
  components: 200,
  execType: 110,
  updated: 130,
};
function colStyle(key: ColKey): CSSProperties {
  return key === "summary"
    ? { flex: "1 1 0", minWidth: COL_WIDTH.summary }
    : { flex: `0 0 ${COL_WIDTH[key]}px`, width: COL_WIDTH[key] };
}
const SELECT_STYLE: CSSProperties = { flex: `0 0 ${SELECT_W}px`, width: SELECT_W };

const COL_LABEL = Object.fromEntries(
  ALL_COLUMNS.map((c) => [c.key, c.label]),
) as Record<ColKey, string>;
const COL_SORT = Object.fromEntries(
  ALL_COLUMNS.filter((c) => c.sortCol).map((c) => [c.key, c.sortCol]),
) as Partial<Record<ColKey, SortCol>>;

interface ColState {
  key: ColKey;
  visible: boolean;
}

const COLUMNS_STORAGE_KEY = "xtm.gridColumns";

function defaultColumns(): ColState[] {
  return ALL_COLUMNS.map((c) => ({ key: c.key, visible: true }));
}

// loadColumns reads the saved config and reconciles it with the known columns,
// so a column added in a newer version still appears and an unknown one is
// dropped.
function loadColumns(): ColState[] {
  try {
    const raw = localStorage.getItem(COLUMNS_STORAGE_KEY);
    if (!raw) return defaultColumns();
    const saved = JSON.parse(raw) as ColState[];
    const known = new Set<string>(ALL_COLUMNS.map((c) => c.key));
    const seen = new Set<string>();
    const result: ColState[] = [];
    for (const s of saved) {
      if (known.has(s.key) && !seen.has(s.key)) {
        result.push({ key: s.key, visible: !!s.visible });
        seen.add(s.key);
      }
    }
    for (const c of ALL_COLUMNS) {
      if (!seen.has(c.key)) result.push({ key: c.key, visible: true });
    }
    return result;
  } catch {
    return defaultColumns();
  }
}

// renderCell returns the table cell for one column of one Test row. `style`
// carries the column's fixed width so the cell aligns as a flex item under the
// virtualized layout (P2).
function renderCell(
  key: ColKey,
  t: TestCase,
  hasPending: boolean,
  callsOthers: boolean,
  style: CSSProperties,
) {
  switch (key) {
    case "key":
      return (
        <td key="key" className="mono" style={style}>
          {hasPending && (
            <span className="row-dirty-dot" title="Pending edits">
              ●
            </span>
          )}
          {t.key}
          {callsOthers && (
            <span
              className="row-calls-badge"
              title="This test calls another test in its steps"
            >
              ⮡
            </span>
          )}
        </td>
      );
    case "summary":
      return (
        <td key="summary" className="summary-cell" style={style}>
          {t.summary}
        </td>
      );
    case "status":
      return (
        <td key="status" style={style}>
          {t.status ? (
            <span className="status-pill">{t.status}</span>
          ) : (
            <span className="muted">—</span>
          )}
        </td>
      );
    case "priority":
      return (
        <td key="priority" style={style}>
          {t.priority || "—"}
        </td>
      );
    case "labels":
      return (
        <td key="labels" className="labels-cell" style={style}>
          {t.labels && t.labels.length > 0 ? (
            t.labels.map((l) => (
              <span key={l} className="label-chip">
                {l}
              </span>
            ))
          ) : (
            <span className="muted">—</span>
          )}
        </td>
      );
    case "components":
      return (
        <td key="components" className="labels-cell" style={style}>
          {t.components && t.components.length > 0 ? (
            t.components.map((c) => (
              <span key={c} className="component-chip">
                {c}
              </span>
            ))
          ) : (
            <span className="muted">—</span>
          )}
        </td>
      );
    case "execType":
      return (
        <td key="execType" style={style}>
          {t.execType || "—"}
        </td>
      );
    case "updated":
      return (
        <td key="updated" className="muted" style={style}>
          {formatDate(t.updated)}
        </td>
      );
  }
}

export function TestTable({
  folderId,
  containerKey,
  component,
  selectedKey,
  pendingByTestKey,
  selectedSet,
  onSelect,
  onToggleSelect,
  onToggleSelectPage,
  onSelectAllMatching,
  onSync,
  syncing,
}: Props) {
  const { activeId: profileId } = useProfile();
  // Gates the exec-type filter to what the active profile's backend actually
  // supports (P6.2a).
  const caps = useCapabilities(profileId);
  const [search, setSearch] = useViewState(profileId, "browse", "search", "");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  const [status, setStatus] = useViewState(profileId, "browse", "status", "");
  const [execType, setExecType] = useViewState(profileId, "browse", "execType", "");
  const [review, setReview] = useState("");
  const [sortBy, setSortBy] = useViewState<SortCol>(profileId, "browse", "sortBy", "key");
  // Default to newest-first by issue number (RND_P_4TFINT_05-202). The key sort
  // is numeric on the trailing number, so this lists the highest issue keys
  // first; saved views restore their own direction.
  const [desc, setDesc] = useViewState(profileId, "browse", "desc", true);
  const [offset, setOffset] = useViewState(profileId, "browse", "offset", 0);
  const [pageSize, setPageSize] = useViewState(profileId, "browse", "pageSize", DEFAULT_PAGE_SIZE);
  const [pageInput, setPageInput] = useState("");

  // The browse grid's page comes from the query cache (audit A3, Phase 2),
  // refreshed by invalidateProfileData on a mutation.
  const q: TestQuery = {
    search: debouncedSearch,
    status: status.trim(),
    folderId,
    containerKey,
    component,
    execType,
    review,
    sortBy,
    desc,
    limit: pageSize,
    offset,
  };
  const testsQuery = useTests(profileId, q);
  const page = testsQuery.data ?? { tests: [], total: 0 };
  const loading = testsQuery.isFetching;
  const listError = testsQuery.error ? errMsg(testsQuery.error) : "";
  // Keys of tests that call another test in their steps — drives the grid cue.
  const callerKeys = useTestCallerKeys(profileId).data ?? EMPTY_CALLER_KEYS;
  // Roving tabindex over the grid rows, tracked by index because rows are
  // virtualized (off-screen rows aren't in the DOM). Exactly one row is in the
  // tab order; Arrow/Home/End move focus by scrolling the target row into view
  // and focusing it (X6).
  const [focusedIndex, setFocusedIndex] = useState(-1);
  const scrollRef = useRef<HTMLDivElement>(null);
  const pendingFocusRef = useRef<number | null>(null);
  const [error, setError] = useState("");
  const [selectingAll, setSelectingAll] = useState(false);
  const [selectAllError, setSelectAllError] = useState("");

  const [savedViews, setSavedViews] = useState<SavedView[]>([]);
  const [activeView, setActiveView] = useState("");
  const [statusOptions, setStatusOptions] = useState<string[]>([]);
  const { prompt } = usePrompt();
  const { notice } = useNotice();

  const [columns, setColumns] = useState<ColState[]>(loadColumns);
  const [showColumns, setShowColumns] = useState(false);
  useEffect(() => {
    localStorage.setItem(COLUMNS_STORAGE_KEY, JSON.stringify(columns));
  }, [columns]);

  // (Caller badges now come from the useTestCallerKeys query above.)
  const visibleColumns = columns.filter((c) => c.visible);

  // Virtualize the row body so a large page (up to 500 rows) only mounts the
  // ~visible slice instead of every <tr> (P2). Rows are a fixed height, so a
  // constant estimate is exact and no per-row measurement is needed.
  const rowVirtualizer = useVirtualizer({
    count: page.tests.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 12,
  });

  // The single keyboard-tabbable row index: the last-focused row if still on
  // this page, else the open row, else the first row — always a valid index so
  // the grid stays reachable by Tab after paging/filtering.
  const selectedIndex = page.tests.findIndex((t) => t.key === selectedKey);
  const tabbableIndex =
    focusedIndex >= 0 && focusedIndex < page.tests.length
      ? focusedIndex
      : selectedIndex >= 0
        ? selectedIndex
        : 0;

  // Total fixed width of the visible columns; the grid never shrinks below this,
  // so columns keep their widths and the scroll container pans horizontally.
  const gridMinWidth =
    SELECT_W + visibleColumns.reduce((sum, c) => sum + COL_WIDTH[c.key], 0);

  // Focus a row by index. The row may not be mounted when navigation asks for
  // it, so moveFocus records the target and a post-render effect retries.
  function focusRowByIndex(index: number): boolean {
    const el = scrollRef.current?.querySelector<HTMLElement>(
      `tr[data-rowindex="${index}"]`,
    );
    if (el) {
      el.focus();
      return true;
    }
    return false;
  }
  function moveFocus(target: number) {
    if (page.tests.length === 0) return;
    const clamped = Math.max(0, Math.min(page.tests.length - 1, target));
    setFocusedIndex(clamped);
    rowVirtualizer.scrollToIndex(clamped, { align: "auto" });
    if (!focusRowByIndex(clamped)) pendingFocusRef.current = clamped;
  }
  useEffect(() => {
    if (
      pendingFocusRef.current != null &&
      focusRowByIndex(pendingFocusRef.current)
    ) {
      pendingFocusRef.current = null;
    }
  });

  function toggleColumn(key: ColKey) {
    setColumns((prev) =>
      prev.map((c) => (c.key === key ? { ...c, visible: !c.visible } : c)),
    );
  }
  function moveColumn(key: ColKey, dir: -1 | 1) {
    setColumns((prev) => {
      const i = prev.findIndex((c) => c.key === key);
      const j = i + dir;
      if (i < 0 || j < 0 || j >= prev.length) return prev;
      const next = [...prev];
      [next[i], next[j]] = [next[j], next[i]];
      return next;
    });
  }

  useEffect(() => {
    let cancelled = false;
    ListSavedViews(profileId)
      .then((vs) => {
        if (!cancelled) setSavedViews(vs ?? []);
      })
      .catch((e) => console.error("list views:", errMsg(e)));
    // Workflow statuses for the filter dropdown (FR-4): authoritative list from
    // Jira's Test workflow, unioned with statuses present in the synced data.
    ListStatuses(profileId)
      .then((s) => {
        if (!cancelled) setStatusOptions(s ?? []);
      })
      .catch((e) => console.error("list statuses:", errMsg(e)));
    return () => {
      cancelled = true;
    };
  }, [profileId]);

  async function saveView() {
    const name = await prompt({
      title: "Save current filter as",
      placeholder: "View name",
      submitLabel: "Save",
    });
    if (!name || !name.trim()) return;
    const query = JSON.stringify({ search, status, execType, review, sortBy, desc });
    setError("");
    try {
      const v = await CreateSavedView(profileId, name.trim(), query);
      setSavedViews((prev) => [v, ...prev]);
      setActiveView(v.id);
    } catch (e) {
      setError(errMsg(e));
    }
  }

  function applyView(id: string) {
    setActiveView(id);
    if (!id) return;
    const v = savedViews.find((x) => x.id === id);
    if (!v) return;
    try {
      const q = JSON.parse(v.query) as Partial<{
        search: string;
        status: string;
        execType: string;
        review: string;
        sortBy: SortCol;
        desc: boolean;
      }>;
      setSearch(q.search ?? "");
      setStatus(q.status ?? "");
      setExecType(q.execType ?? "");
      setReview(q.review ?? "");
      setSortBy(q.sortBy ?? "key");
      setDesc(q.desc ?? false);
    } catch {
      // Ignore a malformed saved query rather than break the grid.
    }
  }

  async function deleteActiveView() {
    if (!activeView) return;
    setError("");
    try {
      await DeleteSavedView(profileId, activeView);
      setSavedViews((prev) => prev.filter((v) => v.id !== activeView));
      setActiveView("");
    } catch (e) {
      setError(errMsg(e));
    }
  }

  useEffect(() => {
    const t = setTimeout(() => setDebouncedSearch(search), 250);
    return () => clearTimeout(t);
  }, [search]);

  useEffect(() => {
    setOffset(0);
  }, [
    debouncedSearch,
    status,
    execType,
    review,
    folderId,
    containerKey,
    component,
    sortBy,
    desc,
    pageSize,
    profileId,
  ]);

  // (The browse page fetch now lives in the useTests query above.)

  function toggleSort(col: SortCol) {
    if (sortBy === col) {
      setDesc((d) => !d);
    } else {
      setSortBy(col);
      setDesc(false);
    }
  }

  const from = page.total === 0 ? 0 : offset + 1;
  const to = Math.min(offset + pageSize, page.total);
  const totalPages = Math.max(1, Math.ceil(page.total / pageSize));
  const currentPage = Math.floor(offset / pageSize) + 1;

  // goToPage clamps to the valid range and moves the offset to that page's
  // first row.
  function goToPage(n: number) {
    const clamped = Math.min(Math.max(1, n), totalPages);
    setOffset((clamped - 1) * pageSize);
  }

  // commitPageInput parses the jump-to-page box and navigates. Invalid input is
  // ignored; the box is then cleared so it shows the live page again.
  function commitPageInput() {
    const n = parseInt(pageInput, 10);
    if (!Number.isNaN(n)) goToPage(n);
    setPageInput("");
  }

  const pageKeys = page.tests.map((t) => t.key);
  const allOnPageSelected =
    pageKeys.length > 0 && pageKeys.every((k) => selectedSet.has(k));
  const someOnPageSelected =
    !allOnPageSelected && pageKeys.some((k) => selectedSet.has(k));

  // The Gmail-style "select all matching" banner shows when the current
  // page is fully selected AND the filter has more results we haven't yet
  // included. selectedSet.size meeting page.total means the user already
  // extended to the full result set — hide the banner in that case.
  const canSelectAllMatching =
    allOnPageSelected &&
    page.total > pageKeys.length &&
    selectedSet.size < page.total;

  async function exportTests() {
    try {
      const q: TestQuery = {
        search: debouncedSearch,
        status: status.trim(),
        folderId,
        containerKey,
        component,
        execType,
        review,
        sortBy,
        desc,
        limit: 0,
        offset: 0,
      };
      const path = await ExportTests(profileId, q);
      if (path) await notice({ title: "Tests exported", message: path });
    } catch (e) {
      await notice({ title: "Export failed", message: errMsg(e), tone: "error" });
    }
  }

  async function selectAllMatching() {
    if (selectingAll) return;
    setSelectingAll(true);
    setSelectAllError("");
    try {
      const q: TestQuery = {
        search: debouncedSearch,
        status: status.trim(),
        folderId,
        containerKey,
        component,
        execType,
        review,
        sortBy,
        desc,
        limit: 0,
        offset: 0,
      };
      const keys = await ListMatchingKeys(profileId, q);
      onSelectAllMatching(keys);
    } catch (e) {
      setSelectAllError(errMsg(e));
    } finally {
      setSelectingAll(false);
    }
  }

  return (
    <div className="testtable">
      <div className="filters">
        <input
          data-tour="search"
          className="search"
          placeholder="Search key, summary, description…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <input
          className="status-filter"
          placeholder="Status…"
          list="status-options"
          value={status}
          onChange={(e) => setStatus(e.target.value)}
          title="Filter by workflow status (pick from the list or type)"
        />
        <datalist id="status-options">
          {statusOptions.map((s) => (
            <option key={s} value={s} />
          ))}
        </datalist>
        {caps.supportsTestTypes && (
          <select
            className="exectype-filter"
            value={execType}
            onChange={(e) => setExecType(e.target.value)}
            title="Filter by execution type (Xray Test Type)"
          >
            <option value="">Any exec type</option>
            {EXEC_TYPE_OPTIONS.map((o) => (
              <option key={o} value={o}>
                {o}
              </option>
            ))}
          </select>
        )}
        {REVIEW_ENABLED && (
          <select
            className="review-filter"
            value={review}
            onChange={(e) => setReview(e.target.value)}
            title="Filter by review verdict"
          >
            <option value="">Any review</option>
            <option value="approved">Approved</option>
            <option value="rejected">Rejected</option>
            <option value="pending">Pending review</option>
            <option value="unreviewed">Unreviewed</option>
          </select>
        )}
        <div className="saved-views">
          <select
            className="view-select"
            value={activeView}
            onChange={(e) => applyView(e.target.value)}
            title="Apply a saved filter"
          >
            <option value="">Saved views…</option>
            {savedViews.map((v) => (
              <option key={v.id} value={v.id}>
                {v.name}
              </option>
            ))}
          </select>
          {activeView && (
            <button
              className="btn btn-ghost view-del"
              onClick={deleteActiveView}
              title="Delete this saved view"
            >
              ✕
            </button>
          )}
          <button className="btn view-save" onClick={saveView}>
            Save view
          </button>
        </div>
        {onSync && (
          <button
            className="btn"
            onClick={onSync}
            disabled={syncing}
            title="Pull the latest test data from Jira (tests only)"
          >
            {syncing ? "Syncing…" : "Sync"}
          </button>
        )}
        <button
          className="btn"
          onClick={exportTests}
          title="Export the filtered tests to CSV or XLSX"
          disabled={page.total === 0}
        >
          Export
        </button>
        {!selectedKey && (
        <div className="columns-menu">
          <button
            className="btn"
            onClick={() => setShowColumns((s) => !s)}
            title="Show / hide / reorder columns"
          >
            Columns
          </button>
          {showColumns && (
            <>
              <div
                className="columns-backdrop"
                onClick={() => setShowColumns(false)}
              />
              <div className="columns-panel">
                {columns.map((c, i) => (
                  <div key={c.key} className="columns-row">
                    <label className="columns-label">
                      <input
                        type="checkbox"
                        checked={c.visible}
                        onChange={() => toggleColumn(c.key)}
                      />
                      {COL_LABEL[c.key]}
                    </label>
                    <span className="columns-reorder">
                      <button
                        className="btn btn-ghost"
                        disabled={i === 0}
                        onClick={() => moveColumn(c.key, -1)}
                        title="Move up"
                      >
                        ▲
                      </button>
                      <button
                        className="btn btn-ghost"
                        disabled={i === columns.length - 1}
                        onClick={() => moveColumn(c.key, 1)}
                        title="Move down"
                      >
                        ▼
                      </button>
                    </span>
                  </div>
                ))}
              </div>
            </>
          )}
        </div>
        )}
      </div>

      {/* A live load failure (listError) always wins over a stale imperative
          save/delete-view error, so a real fetch error is never masked. */}
      {(listError || error) && (
        <div className="error-text table-error">{listError || error}</div>
      )}

      {canSelectAllMatching && (
        <div className="select-all-banner">
          You've selected all {pageKeys.length} tests on this page.{" "}
          <button
            className="link-btn"
            onClick={selectAllMatching}
            disabled={selectingAll}
          >
            {selectingAll
              ? "Selecting…"
              : `Select all ${page.total.toLocaleString()} matching this filter`}
          </button>
          {selectAllError && (
            <span className="error-text">  {selectAllError}</span>
          )}
        </div>
      )}

      <div data-tour="grid" className="table-scroll" ref={scrollRef}>
        <table className="grid-table" style={{ minWidth: gridMinWidth }}>
          <thead>
            <tr>
              <th className="select-col" style={SELECT_STYLE}>
                <input
                  type="checkbox"
                  checked={allOnPageSelected}
                  ref={(el) => {
                    if (el) el.indeterminate = someOnPageSelected;
                  }}
                  disabled={pageKeys.length === 0}
                  onChange={() => onToggleSelectPage(pageKeys)}
                  aria-label={
                    allOnPageSelected
                      ? "Clear page selection"
                      : "Select all on this page"
                  }
                  title={
                    allOnPageSelected
                      ? "Clear page selection"
                      : "Select all on this page"
                  }
                />
              </th>
              {visibleColumns.map((c) => {
                const sortCol = COL_SORT[c.key];
                return sortCol ? (
                  <SortHeader
                    key={c.key}
                    col={sortCol}
                    label={COL_LABEL[c.key]}
                    sortBy={sortBy}
                    desc={desc}
                    onSort={toggleSort}
                    style={colStyle(c.key)}
                  />
                ) : (
                  <th key={c.key} style={colStyle(c.key)}>
                    {COL_LABEL[c.key]}
                  </th>
                );
              })}
            </tr>
          </thead>
          <tbody
            style={{
              height: rowVirtualizer.getTotalSize(),
              position: "relative",
            }}
          >
            {rowVirtualizer.getVirtualItems().map((vrow) => {
              const t = page.tests[vrow.index];
              const hasPending = pendingByTestKey.has(t.key);
              const isSelected = selectedSet.has(t.key);
              const callsOthers = callerKeys.has(t.key);
              return (
                <tr
                  key={t.key}
                  data-rowindex={vrow.index}
                  className={
                    (t.key === selectedKey ? "row-selected " : "") +
                    (isSelected ? "row-checked" : "")
                  }
                  style={{ transform: `translateY(${vrow.start}px)` }}
                  tabIndex={vrow.index === tabbableIndex ? 0 : -1}
                  aria-selected={isSelected}
                  onClick={() => onSelect(t.key)}
                  onFocus={() => setFocusedIndex(vrow.index)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault();
                      onSelect(t.key);
                    } else if (e.key === "ArrowDown") {
                      e.preventDefault();
                      moveFocus(vrow.index + 1);
                    } else if (e.key === "ArrowUp") {
                      e.preventDefault();
                      moveFocus(vrow.index - 1);
                    } else if (e.key === "Home") {
                      e.preventDefault();
                      moveFocus(0);
                    } else if (e.key === "End") {
                      e.preventDefault();
                      moveFocus(page.tests.length - 1);
                    }
                  }}
                >
                  <td
                    className="select-col"
                    style={SELECT_STYLE}
                    onClick={(e) => e.stopPropagation()}
                  >
                    <input
                      type="checkbox"
                      checked={isSelected}
                      aria-label={`Select ${t.key}`}
                      onChange={() => onToggleSelect(t.key)}
                    />
                  </td>
                  {visibleColumns.map((c) =>
                    renderCell(c.key, t, hasPending, callsOthers, colStyle(c.key)),
                  )}
                </tr>
              );
            })}
          </tbody>
        </table>
        {!loading && page.tests.length === 0 && (
          <div className="grid-empty muted">
            {page.total === 0 &&
            debouncedSearch === "" &&
            status.trim() === "" &&
            folderId === "" &&
            containerKey === "" &&
            component === "" &&
            review === ""
              ? "No tests yet. Run a sync to pull them from Jira."
              : "No tests match the current filter."}
          </div>
        )}
      </div>

      <div className="table-footer">
        <div className="pager">
          <select
            className="page-size"
            value={pageSize}
            onChange={(e) => setPageSize(Number(e.target.value))}
            title="Rows per page"
          >
            {PAGE_SIZE_OPTIONS.map((n) => (
              <option key={n} value={n}>
                {n} / page
              </option>
            ))}
          </select>
          <button
            className="btn page-btn"
            disabled={currentPage <= 1 || loading}
            onClick={() => goToPage(1)}
            title="First page"
          >
            «
          </button>
          <button
            className="btn page-btn"
            disabled={currentPage <= 1 || loading}
            onClick={() => goToPage(currentPage - 1)}
            title="Previous page"
          >
            ‹
          </button>
          <span className="page-indicator">
            Page{" "}
            <input
              className="page-jump"
              type="text"
              inputMode="numeric"
              value={pageInput}
              placeholder={String(currentPage)}
              disabled={loading}
              onChange={(e) =>
                setPageInput(e.target.value.replace(/[^0-9]/g, ""))
              }
              onBlur={commitPageInput}
              onKeyDown={(e) => {
                if (e.key === "Enter") (e.target as HTMLInputElement).blur();
              }}
              title="Jump to page"
            />{" "}
            of {totalPages.toLocaleString()}
          </span>
          <button
            className="btn page-btn"
            disabled={currentPage >= totalPages || loading}
            onClick={() => goToPage(currentPage + 1)}
            title="Next page"
          >
            ›
          </button>
          <button
            className="btn page-btn"
            disabled={currentPage >= totalPages || loading}
            onClick={() => goToPage(totalPages)}
            title="Last page"
          >
            »
          </button>
        </div>
        <span className="muted count">
          {loading
            ? "Loading…"
            : `${from.toLocaleString()}–${to.toLocaleString()} of ${page.total.toLocaleString()}`}
        </span>
      </div>
    </div>
  );
}

function SortHeader({
  col,
  label,
  sortBy,
  desc,
  onSort,
  style,
}: {
  col: SortCol;
  label: string;
  sortBy: SortCol;
  desc: boolean;
  onSort: (c: SortCol) => void;
  style: CSSProperties;
}) {
  const active = sortBy === col;
  return (
    <th
      className="sortable"
      style={style}
      aria-sort={active ? (desc ? "descending" : "ascending") : "none"}
    >
      <button type="button" className="sort-btn" onClick={() => onSort(col)}>
        {label}
        <span className="sort-caret" aria-hidden="true">
          {active ? (desc ? " ▼" : " ▲") : ""}
        </span>
      </button>
    </th>
  );
}
