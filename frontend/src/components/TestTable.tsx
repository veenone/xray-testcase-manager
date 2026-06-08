import { useEffect, useState } from "react";
import {
  ListTests,
  ListMatchingKeys,
  ExportTests,
  CreateSavedView,
  ListSavedViews,
  DeleteSavedView,
  errMsg,
} from "../api";
import { usePrompt } from "./usePrompt";
import type {
  TestPage,
  TestQuery,
  TestCase,
  PendingChange,
  SavedView,
} from "../api";

interface Props {
  profileId: string;
  folderId: string;
  containerKey: string;
  component: string;
  refreshKey: number;
  selectedKey: string | null;
  pendingByTestKey: Map<string, PendingChange[]>;
  selectedSet: Set<string>;
  onSelect: (key: string) => void;
  onToggleSelect: (key: string) => void;
  onToggleSelectPage: (keys: string[]) => void;
  onSelectAllMatching: (keys: string[]) => void;
}

// Page-size choices for the grid pager. The backend caps a page at 500.
const PAGE_SIZE_OPTIONS = [50, 100, 200, 500];
const DEFAULT_PAGE_SIZE = 100;

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
  { key: "updated", label: "Updated", sortCol: "updated" },
];

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

// renderCell returns the table cell for one column of one Test row.
function renderCell(key: ColKey, t: TestCase, hasPending: boolean) {
  switch (key) {
    case "key":
      return (
        <td key="key" className="mono">
          {hasPending && (
            <span className="row-dirty-dot" title="Pending edits">
              ●
            </span>
          )}
          {t.key}
        </td>
      );
    case "summary":
      return (
        <td key="summary" className="summary-cell">
          {t.summary}
        </td>
      );
    case "status":
      return (
        <td key="status">
          {t.status ? (
            <span className="status-pill">{t.status}</span>
          ) : (
            <span className="muted">—</span>
          )}
        </td>
      );
    case "priority":
      return <td key="priority">{t.priority || "—"}</td>;
    case "labels":
      return (
        <td key="labels" className="labels-cell">
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
        <td key="components" className="labels-cell">
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
    case "updated":
      return (
        <td key="updated" className="muted">
          {formatDate(t.updated)}
        </td>
      );
  }
}

export function TestTable({
  profileId,
  folderId,
  containerKey,
  component,
  refreshKey,
  selectedKey,
  pendingByTestKey,
  selectedSet,
  onSelect,
  onToggleSelect,
  onToggleSelectPage,
  onSelectAllMatching,
}: Props) {
  const [search, setSearch] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  const [status, setStatus] = useState("");
  const [review, setReview] = useState("");
  const [sortBy, setSortBy] = useState<SortCol>("key");
  const [desc, setDesc] = useState(false);
  const [offset, setOffset] = useState(0);
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE);
  const [pageInput, setPageInput] = useState("");

  const [page, setPage] = useState<TestPage>({ tests: [], total: 0 });
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [selectingAll, setSelectingAll] = useState(false);
  const [selectAllError, setSelectAllError] = useState("");

  const [savedViews, setSavedViews] = useState<SavedView[]>([]);
  const [activeView, setActiveView] = useState("");
  const { prompt, promptUI } = usePrompt();

  const [columns, setColumns] = useState<ColState[]>(loadColumns);
  const [showColumns, setShowColumns] = useState(false);
  useEffect(() => {
    localStorage.setItem(COLUMNS_STORAGE_KEY, JSON.stringify(columns));
  }, [columns]);
  const visibleColumns = columns.filter((c) => c.visible);

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
    const query = JSON.stringify({ search, status, review, sortBy, desc });
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
        review: string;
        sortBy: SortCol;
        desc: boolean;
      }>;
      setSearch(q.search ?? "");
      setStatus(q.status ?? "");
      setReview(q.review ?? "");
      setSortBy(q.sortBy ?? "key");
      setDesc(q.desc ?? false);
    } catch {
      // Ignore a malformed saved query rather than break the grid.
    }
  }

  async function deleteActiveView() {
    if (!activeView) return;
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
    review,
    folderId,
    containerKey,
    component,
    sortBy,
    desc,
    pageSize,
    profileId,
  ]);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError("");
    const q: TestQuery = {
      search: debouncedSearch,
      status: status.trim(),
      folderId,
      containerKey,
      component,
      review,
      sortBy,
      desc,
      limit: pageSize,
      offset,
    };
    ListTests(profileId, q)
      .then((p) => {
        if (!cancelled) setPage(p);
      })
      .catch((e) => {
        if (!cancelled) setError(errMsg(e));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [
    profileId,
    debouncedSearch,
    status,
    folderId,
    containerKey,
    component,
    review,
    sortBy,
    desc,
    offset,
    pageSize,
    refreshKey,
  ]);

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
        review,
        sortBy,
        desc,
        limit: 0,
        offset: 0,
      };
      const path = await ExportTests(profileId, q);
      if (path) window.alert(`Exported ${page.total} test(s) to:\n${path}`);
    } catch (e) {
      window.alert(`Export failed: ${errMsg(e)}`);
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
          className="search"
          placeholder="Search key, summary, description…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <input
          className="status-filter"
          placeholder="Status (exact match)"
          value={status}
          onChange={(e) => setStatus(e.target.value)}
        />
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

      {error && <div className="error-text table-error">{error}</div>}

      {canSelectAllMatching && (
        <div className="select-all-banner">
          All {pageKeys.length} tests on this page are selected.{" "}
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

      <div className="table-scroll">
        <table>
          <thead>
            <tr>
              <th className="select-col">
                <input
                  type="checkbox"
                  checked={allOnPageSelected}
                  ref={(el) => {
                    if (el) el.indeterminate = someOnPageSelected;
                  }}
                  disabled={pageKeys.length === 0}
                  onChange={() => onToggleSelectPage(pageKeys)}
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
                  />
                ) : (
                  <th key={c.key}>{COL_LABEL[c.key]}</th>
                );
              })}
            </tr>
          </thead>
          <tbody>
            {page.tests.map((t) => {
              const hasPending = pendingByTestKey.has(t.key);
              const isSelected = selectedSet.has(t.key);
              return (
                <tr
                  key={t.key}
                  className={
                    (t.key === selectedKey ? "row-selected " : "") +
                    (isSelected ? "row-checked" : "")
                  }
                  onClick={() => onSelect(t.key)}
                >
                  <td
                    className="select-col"
                    onClick={(e) => e.stopPropagation()}
                  >
                    <input
                      type="checkbox"
                      checked={isSelected}
                      onChange={() => onToggleSelect(t.key)}
                    />
                  </td>
                  {visibleColumns.map((c) => renderCell(c.key, t, hasPending))}
                </tr>
              );
            })}
            {!loading && page.tests.length === 0 && (
              <tr>
                <td
                  colSpan={1 + visibleColumns.length}
                  className="empty-row muted"
                >
                  {page.total === 0 &&
                  debouncedSearch === "" &&
                  status.trim() === "" &&
                  folderId === "" &&
                  containerKey === "" &&
                  component === "" &&
                  review === ""
                    ? "No tests yet — run a sync to pull them from Jira."
                    : "No tests match the current filter."}
                </td>
              </tr>
            )}
          </tbody>
        </table>
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
      {promptUI}
    </div>
  );
}

function SortHeader({
  col,
  label,
  sortBy,
  desc,
  onSort,
}: {
  col: SortCol;
  label: string;
  sortBy: SortCol;
  desc: boolean;
  onSort: (c: SortCol) => void;
}) {
  const active = sortBy === col;
  return (
    <th className="sortable" onClick={() => onSort(col)}>
      {label}
      <span className="sort-caret">{active ? (desc ? " ▼" : " ▲") : ""}</span>
    </th>
  );
}

function formatDate(s: string): string {
  if (!s) return "—";
  const d = new Date(s);
  return isNaN(d.getTime()) ? s : d.toLocaleDateString();
}
