import { Fragment, useEffect, useMemo, useState } from "react";
import { useViewState } from "../lib/viewState";
import {
  ListBugsWithTests,
  ListTestsForBug,
  SyncBugs,
  CreateContainerAndAllocate,
  ListContainers,
  AllocateTests,
  BrowserOpenURL,
  GetTestRunHistory,
  errMsg,
} from "../api";
import type { BugWithTests, BugTest, TestRunEntry, Container } from "../api";
import { formatDateTime } from "../dates";
import { Pager } from "./Pager";
import { SortControl } from "./SortControl";
import { TestDetail } from "./TestDetail";
import { usePrompt } from "./usePrompt";
import { keyCompare, cmpStr, applyDir } from "../sort";

interface Props {
  profileId: string;
  refreshKey: number;
  jiraUrl: string;
  onOpenTest: (testKey: string) => void;
}

function cmpBug(a: BugWithTests, b: BugWithTests, field: string): number {
  switch (field) {
    case "status":
      return cmpStr(a.status, b.status) || keyCompare(a.key, b.key);
    case "project":
      return cmpStr(a.projectKey, b.projectKey) || keyCompare(a.key, b.key);
    case "priority":
      return cmpStr(a.priority, b.priority) || keyCompare(a.key, b.key);
    default:
      return keyCompare(a.key, b.key);
  }
}

// BugsPanel is a master-detail view of the defects linked to the profile's
// tests: a filterable, paginated bug list on the left and, for the selected
// bug, a detail pane on the right showing its full info plus the affected tests
// enriched with their consolidated run status. Bug keys open in the browser;
// test keys open the test detail.
export function BugsPanel({ profileId, refreshKey, jiraUrl, onOpenTest }: Props) {
  const [bugs, setBugs] = useState<BugWithTests[]>([]);
  const [filter, setFilter] = useViewState(profileId, "bugs", "filter", "");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [selected, setSelected] = useViewState(profileId, "bugs", "selected", "");
  const [tests, setTests] = useState<BugTest[]>([]);
  // Checked bugs for the bulk "Create Test Execution" action. This is kept
  // independent of `selected` (the detail-pane row): ticking a checkbox must
  // not change which bug is shown in the detail pane, and vice versa.
  const [checked, setChecked] = useState<Set<string>>(new Set());
  const [creating, setCreating] = useState(false);
  // State for "Add tests to existing execution" modal.
  const [addToExecOpen, setAddToExecOpen] = useState(false);
  const [addToExecLoading, setAddToExecLoading] = useState(false);
  const [addToExecExecs, setAddToExecExecs] = useState<Container[]>([]);
  const [addToExecTarget, setAddToExecTarget] = useState("");
  const [addToExecFilter, setAddToExecFilter] = useState("");
  const [addToExecWorking, setAddToExecWorking] = useState(false);
  const [addToExecError, setAddToExecError] = useState("");
  const [page, setPage] = useViewState(profileId, "bugs", "page", 0); // 0-based
  const [pageSize, setPageSize] = useViewState(profileId, "bugs", "pageSize", 15);
  const [sortField, setSortField] = useViewState(profileId, "bugs", "sortField", "key");
  const [sortDesc, setSortDesc] = useViewState(profileId, "bugs", "sortDesc", true);
  const [syncing, setSyncing] = useState(false);
  const { prompt, promptUI } = usePrompt();
  // Local refresh nonce: bumped after a bugs-only sync to re-pull the list
  // without forcing a full profile refresh.
  const [nonce, setNonce] = useState(0);

  // In-view read-only test detail sidebar: detailKey is session-persisted so
  // returning to the Bugs view restores the open detail; detailVersion is
  // ephemeral (bumped to force a re-fetch on re-open).
  const [detailKey, setDetailKey] = useViewState<string | null>(profileId, "bugs", "detailKey", null);
  const [detailVersion, setDetailVersion] = useState(0);

  // Ephemeral expand state for the affected-tests table. Not session-persisted
  // because it's a transient drill-down, not a view preference.
  const [expandedTests, setExpandedTests] = useState<Set<string>>(new Set());
  // Cache for per-test run history fetched on first expand (keyed by test key).
  const [runHistoryCache, setRunHistoryCache] = useState<Map<string, TestRunEntry[]>>(new Map());
  // Set of test keys whose run history is currently loading.
  const [runHistoryLoading, setRunHistoryLoading] = useState<Set<string>>(new Set());
  // Per-test sort state for the run-history breakdown. Keyed by test key.
  // Default: updatedAt descending (newest first).
  type RunSort = { field: string; desc: boolean };
  const [runSortMap, setRunSortMap] = useState<Map<string, RunSort>>(new Map());

  function getRunSort(testKey: string): RunSort {
    return runSortMap.get(testKey) ?? { field: "updatedAt", desc: true };
  }

  function toggleRunSort(testKey: string, field: string) {
    setRunSortMap((prev) => {
      const current = prev.get(testKey) ?? { field: "updatedAt", desc: true };
      const next: RunSort =
        current.field === field
          ? { field, desc: !current.desc }
          : { field, desc: true };
      return new Map(prev).set(testKey, next);
    });
  }

  function sortedRuns(runs: TestRunEntry[], sort: RunSort): TestRunEntry[] {
    return [...runs].sort((a, b) => {
      let cmp = 0;
      switch (sort.field) {
        case "runStatus":
          cmp = (a.runStatus ?? "").localeCompare(b.runStatus ?? "");
          break;
        case "createdAt":
          cmp = (a.createdAt ?? "").localeCompare(b.createdAt ?? "");
          break;
        case "updatedAt":
          cmp = (a.updatedAt ?? "").localeCompare(b.updatedAt ?? "");
          break;
        default:
          cmp = 0;
      }
      return sort.desc ? -cmp : cmp;
    });
  }

  // syncBugs refreshes only the defect issues from Jira (partial sync), so the
  // Bugs panel can update without re-running preconditions / containers /
  // requirements (RND_P_4TFINT_05-214).
  async function syncBugs() {
    setSyncing(true);
    setError("");
    setNotice("");
    try {
      await SyncBugs(profileId);
      setNonce((n) => n + 1);
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setSyncing(false);
    }
  }

  // Toggle the run-history expand for a test row. Fetches history on first
  // expand if not already cached.
  function toggleTestExpand(testKey: string) {
    setExpandedTests((prev) => {
      const next = new Set(prev);
      if (next.has(testKey)) {
        next.delete(testKey);
      } else {
        next.add(testKey);
        // Fetch history only if not already cached.
        if (!runHistoryCache.has(testKey)) {
          setRunHistoryLoading((l) => { const nl = new Set(l); nl.add(testKey); return nl; });
          GetTestRunHistory(profileId, testKey)
            .then((entries) => {
              setRunHistoryCache((m) => new Map(m).set(testKey, entries ?? []));
            })
            .catch(() => {
              setRunHistoryCache((m) => new Map(m).set(testKey, []));
            })
            .finally(() => {
              setRunHistoryLoading((l) => { const nl = new Set(l); nl.delete(testKey); return nl; });
            });
        }
      }
      return next;
    });
  }

  // Toggle a bug's checkbox without disturbing the detail-pane selection.
  function toggleChecked(key: string) {
    setChecked((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }

  // De-duplicated union of the linked test keys across all checked bugs. A bug
  // with no linked tests contributes nothing, so the union can be empty even
  // when bugs are checked - in that case the action is disabled.
  const unionTestKeys = useMemo(() => {
    const set = new Set<string>();
    for (const b of bugs) {
      if (checked.has(b.key)) {
        for (const k of b.testKeys ?? []) set.add(k);
      }
    }
    return [...set];
  }, [bugs, checked]);

  // Create a Test Execution whose members are the union of the checked bugs'
  // linked tests, to isolate a run that verifies only those bugs
  // (RND_P_4TFINT_05-222).
  async function createExecFromBugs() {
    if (unionTestKeys.length === 0) return;
    const checkedKeys = bugs
      .filter((b) => checked.has(b.key))
      .map((b) => b.key);
    const joined = checkedKeys.join(", ");
    const defaultName =
      joined.length > 60
        ? `Verify bugs: ${joined.slice(0, 57)}...`
        : `Verify bugs: ${joined}`;
    const name = await prompt({
      title: "New Test Execution",
      defaultValue: defaultName,
      placeholder: "Test Execution name",
      submitLabel: "Create",
    });
    if (!name || !name.trim()) return;
    setCreating(true);
    setError("");
    setNotice("");
    try {
      const res = await CreateContainerAndAllocate(
        profileId,
        "testexec",
        name.trim(),
        unionTestKeys,
      );
      setNotice(
        `Created Test Execution ${res.tempKey} with ${res.added} test${
          res.added === 1 ? "" : "s"
        }. It will appear in Containers.`,
      );
      setChecked(new Set());
      setNonce((n) => n + 1);
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setCreating(false);
    }
  }

  // Open the "Add tests to existing execution" modal: load the list of Test
  // Executions then show the picker.
  async function openAddToExec() {
    if (unionTestKeys.length === 0) return;
    setAddToExecOpen(true);
    setAddToExecError("");
    setAddToExecFilter("");
    setAddToExecTarget("");
    setAddToExecExecs([]);
    setAddToExecLoading(true);
    try {
      const cs = await ListContainers(profileId, "testexec");
      const list = cs ?? [];
      setAddToExecExecs(list);
      if (list.length > 0) setAddToExecTarget(list[0].key);
    } catch (e) {
      setAddToExecError(errMsg(e));
    } finally {
      setAddToExecLoading(false);
    }
  }

  // Confirm: allocate the union test keys to the chosen execution.
  async function confirmAddToExec() {
    if (!addToExecTarget || unionTestKeys.length === 0) return;
    setAddToExecWorking(true);
    setAddToExecError("");
    try {
      const r = await AllocateTests(profileId, addToExecTarget, unionTestKeys);
      const added = r.added.length;
      const already = r.alreadyMembers.length;
      setAddToExecOpen(false);
      setNotice(
        `Added ${added} test${added === 1 ? "" : "s"} to ${addToExecTarget}` +
          (already > 0 ? ` (${already} already present).` : "."),
      );
      setChecked(new Set());
    } catch (e) {
      setAddToExecError(errMsg(e));
    } finally {
      setAddToExecWorking(false);
    }
  }

  useEffect(() => {
    if (!profileId) return;
    let cancelled = false;
    ListBugsWithTests(profileId)
      .then((bs) => {
        if (!cancelled) setBugs(bs ?? []);
      })
      .catch((e) => {
        if (!cancelled) setError(errMsg(e));
      });
    return () => {
      cancelled = true;
    };
  }, [profileId, refreshKey, nonce]);

  const isDemo = /^(demo$|demo:|mock:)/i.test((jiraUrl ?? "").trim());
  const canLink = !!jiraUrl && !isDemo;
  function openBug(key: string) {
    const base = (jiraUrl ?? "").trim().replace(/\/+$/, "");
    if (base && canLink && !key.startsWith("NEW-"))
      BrowserOpenURL(`${base}/browse/${key}`);
  }

  const shown = useMemo(() => {
    const f = filter.trim().toLowerCase();
    const base = !f
      ? bugs
      : bugs.filter(
          (b) =>
            b.key.toLowerCase().includes(f) ||
            b.summary.toLowerCase().includes(f) ||
            b.projectKey.toLowerCase().includes(f) ||
            b.status.toLowerCase().includes(f),
        );
    return [...base].sort((a, b) => applyDir(cmpBug(a, b, sortField), sortDesc));
  }, [bugs, filter, sortField, sortDesc]);

  // Reset to the first page whenever the data source or the filter changes.
  useEffect(() => {
    setPage(0);
  }, [profileId, refreshKey, filter, sortField, sortDesc]);

  // Keep a valid selection: default to the first shown bug, and re-point when
  // the current one is filtered out.
  useEffect(() => {
    if (shown.length === 0) {
      setSelected("");
    } else if (!shown.some((b) => b.key === selected)) {
      setSelected(shown[0].key);
    }
  }, [shown, selected]);

  // Clear the per-test expand state whenever the user switches to a different
  // bug, so stale expansions from the previous selection don't carry over.
  useEffect(() => {
    setExpandedTests(new Set());
  }, [selected]);

  // Load the affected tests (with run status) for the selected bug.
  useEffect(() => {
    if (!profileId || !selected) {
      setTests([]);
      return;
    }
    let cancelled = false;
    ListTestsForBug(profileId, selected)
      .then((ts) => {
        if (!cancelled) setTests(ts ?? []);
      })
      .catch((e) => {
        if (!cancelled) setError(errMsg(e));
      });
    return () => {
      cancelled = true;
    };
  }, [profileId, selected, refreshKey]);

  const totalPages = Math.max(1, Math.ceil(shown.length / pageSize));
  const safePage = Math.min(Math.max(0, page), totalPages - 1);
  const paged = shown.slice(safePage * pageSize, safePage * pageSize + pageSize);
  const sel = bugs.find((b) => b.key === selected) ?? null;

  return (
    <div className={`bugs-md${detailKey ? " bugs-md-with-detail" : ""}`}>
      {promptUI}
      <div className="bugs-md-list">
        <div className="bugs-md-head">
          <span className="bugs-md-title">Bugs</span>
          <button
            className="btn"
            onClick={createExecFromBugs}
            disabled={creating || unionTestKeys.length === 0}
            title={
              checked.size === 0
                ? "Tick one or more bugs to create a Test Execution from their linked tests"
                : unionTestKeys.length === 0
                  ? "The checked bugs have no linked tests"
                  : `Create a Test Execution containing the ${unionTestKeys.length} test${
                      unionTestKeys.length === 1 ? "" : "s"
                    } linked to the ${checked.size} checked bug${
                      checked.size === 1 ? "" : "s"
                    }`
            }
          >
            {creating
              ? "Creating…"
              : `Create Test Execution${
                  unionTestKeys.length > 0 ? ` (${unionTestKeys.length})` : ""
                }`}
          </button>
          <button
            className="btn"
            onClick={openAddToExec}
            disabled={unionTestKeys.length === 0}
            title={
              checked.size === 0
                ? "Tick one or more bugs to add their linked tests to an existing Test Execution"
                : unionTestKeys.length === 0
                  ? "The checked bugs have no linked tests"
                  : `Add the ${unionTestKeys.length} test${
                      unionTestKeys.length === 1 ? "" : "s"
                    } linked to the ${checked.size} checked bug${
                      checked.size === 1 ? "" : "s"
                    } to an existing Test Execution`
            }
          >
            {`Add to execution${unionTestKeys.length > 0 ? ` (${unionTestKeys.length})` : ""}`}
          </button>
          <button
            className="btn"
            onClick={syncBugs}
            disabled={syncing}
            title="Refresh just the linked bugs from Jira (partial sync)"
          >
            {syncing ? "Syncing…" : "Sync"}
          </button>
        </div>
        {error && <div className="error-text">{error}</div>}
        {notice && <p className="reqs-notice muted">{notice}</p>}
        <input
          className="search bugs-md-filter"
          placeholder="Filter bugs by key, summary, project, status…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
        <SortControl
          fields={[
            { value: "key", label: "Key" },
            { value: "status", label: "Status" },
            { value: "project", label: "Project" },
            { value: "priority", label: "Priority" },
          ]}
          field={sortField}
          desc={sortDesc}
          onChange={(f, d) => {
            setSortField(f);
            setSortDesc(d);
          }}
        />
        {shown.length === 0 ? (
          <p className="muted bugs-md-empty">
            {bugs.length === 0
              ? "No bugs linked to this profile's tests. File one from a failed test in a Test Execution, or sync a demo profile."
              : "No bugs match the filter."}
          </p>
        ) : (
          <>
            <ul className="bugs-md-items">
              {paged.map((b) => (
                <li
                  key={b.key}
                  className={`bugs-md-item${b.key === selected ? " bugs-md-item-selected" : ""}`}
                  onClick={() => setSelected(b.key)}
                >
                  <div className="bugs-md-item-top">
                    <input
                      type="checkbox"
                      className="bugs-md-check"
                      checked={checked.has(b.key)}
                      title="Include this bug's linked tests when creating a Test Execution"
                      onClick={(e) => e.stopPropagation()}
                      onChange={(e) => {
                        e.stopPropagation();
                        toggleChecked(b.key);
                      }}
                    />
                    <span className="mono bugs-md-key">{b.key}</span>
                    <span className="muted">{b.projectKey}</span>
                    {b.status && <span className="status-pill">{b.status}</span>}
                  </div>
                  <div className="bugs-md-item-summary">
                    {b.summary || "(no summary)"}
                  </div>
                  <div className="bugs-md-item-meta muted">
                    {b.priority} · {b.testKeys.length} test
                    {b.testKeys.length === 1 ? "" : "s"}
                  </div>
                </li>
              ))}
            </ul>
            <Pager
              compact
              page={safePage}
              pageSize={pageSize}
              total={shown.length}
              onPage={setPage}
              onPageSize={(n) => {
                setPageSize(n);
                setPage(0);
              }}
            />
          </>
        )}
      </div>

      <div className="bugs-md-detail">
        {!sel ? (
          <p className="muted">Select a bug to see its details.</p>
        ) : (
          <>
            <div className="bugs-md-detail-head">
              {canLink && !sel.key.startsWith("NEW-") ? (
                <button
                  className="mono bug-link-key bugs-md-detail-key"
                  onClick={() => openBug(sel.key)}
                  title={`Open ${sel.key} in Jira`}
                >
                  {sel.key}
                </button>
              ) : (
                <span className="mono bugs-md-detail-key">{sel.key}</span>
              )}
              <span className="muted">{sel.projectKey}</span>
              {sel.status && <span className="status-pill">{sel.status}</span>}
              {sel.priority && (
                <span className="muted bugs-md-detail-priority">
                  {sel.priority}
                </span>
              )}
            </div>
            <h2 className="bugs-md-detail-summary">
              {sel.summary || "(no summary)"}
            </h2>

            <h4>Affected tests ({tests.length})</h4>
            {tests.length === 0 ? (
              <p className="muted">No affected tests.</p>
            ) : (
              <table className="board-table">
                <thead>
                  <tr>
                    <th style={{ width: "1.5rem" }} />
                    <th>Test</th>
                    <th style={{ width: "1.5rem" }} />
                    <th>Project</th>
                    <th>Summary</th>
                    <th>Status</th>
                    <th>Latest result</th>
                  </tr>
                </thead>
                <tbody>
                  {tests.map((t) => {
                    const isExpanded = expandedTests.has(t.key);
                    const isLoading = runHistoryLoading.has(t.key);
                    const history = runHistoryCache.get(t.key);
                    return (
                      <Fragment key={t.key}>
                        <tr>
                          <td>
                            <button
                              className="btn-icon"
                              title={isExpanded ? "Collapse run history" : "Expand run history"}
                              onClick={() => toggleTestExpand(t.key)}
                              style={{ fontSize: "0.75rem", padding: "0 0.25rem" }}
                            >
                              {isExpanded ? "▾" : "▸"}
                            </button>
                          </td>
                          <td>
                            <button
                              className="mono bug-link-key"
                              onClick={() => onOpenTest(t.key)}
                              title={`Open ${t.key} in Browse`}
                            >
                              {t.key}
                            </button>
                          </td>
                          <td>
                            <button
                              className="btn-icon"
                              title={`Open ${t.key} detail here`}
                              onClick={() => {
                                setDetailKey(t.key);
                                setDetailVersion((v) => v + 1);
                              }}
                              style={{ fontSize: "0.75rem", padding: "0 0.25rem" }}
                            >
                              ↗
                            </button>
                          </td>
                          <td className="muted">{t.project || "—"}</td>
                          <td>{t.summary}</td>
                          <td>{t.status || "—"}</td>
                          <td>
                            {t.runStatus ? (
                              <span
                                className={`run-badge run-${t.runStatus.toLowerCase()}`}
                              >
                                {t.runStatus}
                              </span>
                            ) : (
                              <span className="muted">not run</span>
                            )}
                          </td>
                        </tr>
                        {isExpanded && (
                          <tr>
                            <td colSpan={7} style={{ padding: "0.25rem 0.5rem 0.5rem 2rem", background: "var(--bg-subtle, #f8f8f8)" }}>
                              {isLoading ? (
                                <span className="muted">Loading run history…</span>
                              ) : !history || history.length === 0 ? (
                                <span className="muted">No run history for this test.</span>
                              ) : (
                                <table className="board-table" style={{ fontSize: "0.85em" }}>
                                  <thead>
                                    {(() => {
                                      const sort = getRunSort(t.key);
                                      function SortTh({ field, children }: { field: string; children: React.ReactNode }) {
                                        const active = sort.field === field;
                                        return (
                                          <th
                                            style={{ cursor: "pointer", userSelect: "none", whiteSpace: "nowrap" }}
                                            onClick={() => toggleRunSort(t.key, field)}
                                            title={`Sort by ${field}`}
                                          >
                                            {children}
                                            {active ? (sort.desc ? " ▾" : " ▴") : ""}
                                          </th>
                                        );
                                      }
                                      return (
                                        <tr>
                                          <th>Execution</th>
                                          <SortTh field="runStatus">Result</SortTh>
                                          <th>Fix Version(s)</th>
                                          <th>Plan(s)</th>
                                          <th>Environment</th>
                                          <th>Date</th>
                                          <th>By</th>
                                          <SortTh field="createdAt">Created</SortTh>
                                          <SortTh field="updatedAt">Updated</SortTh>
                                          <th>Defects</th>
                                        </tr>
                                      );
                                    })()}
                                  </thead>
                                  <tbody>
                                    {sortedRuns(history, getRunSort(t.key)).map((r, i) => (
                                      <tr key={`${r.execKey}-${i}`}>
                                        <td>
                                          {canLink && r.execKey && !r.execKey.startsWith("NEW-") ? (
                                            <button
                                              className="mono bug-link-key"
                                              onClick={() => {
                                                const base = (jiraUrl ?? "").trim().replace(/\/+$/, "");
                                                BrowserOpenURL(`${base}/browse/${r.execKey}`);
                                              }}
                                              title={r.execSummary || `Open ${r.execKey} in Jira`}
                                            >
                                              {r.execKey}
                                            </button>
                                          ) : (
                                            <span className="mono" title={r.execSummary}>{r.execKey}</span>
                                          )}
                                          {r.execSummary && (
                                            <span className="muted" style={{ display: "block", fontSize: "0.9em" }}>{r.execSummary}</span>
                                          )}
                                        </td>
                                        <td>
                                          {r.runStatus ? (
                                            <span className={`run-badge run-${r.runStatus.toLowerCase()}`}>{r.runStatus}</span>
                                          ) : (
                                            <span className="muted">—</span>
                                          )}
                                        </td>
                                        <td>{r.fixVersions?.length ? r.fixVersions.join(", ") : <span className="muted">—</span>}</td>
                                        <td>{r.planKeys?.length ? r.planKeys.join(", ") : <span className="muted">—</span>}</td>
                                        <td>{r.environment || <span className="muted">—</span>}</td>
                                        <td className="muted">{formatDateTime(r.finishedAt || r.startedAt)}</td>
                                        <td>{r.executedBy || <span className="muted">—</span>}</td>
                                        <td className="muted" style={{ whiteSpace: "nowrap" }}>{formatDateTime(r.createdAt) || <span>—</span>}</td>
                                        <td className="muted" style={{ whiteSpace: "nowrap" }}>{formatDateTime(r.updatedAt) || <span>—</span>}</td>
                                        <td>
                                          {r.defects?.length ? (
                                            <span>
                                              {r.defects.map((d, di) => (
                                                <span key={d}>
                                                  {di > 0 && ", "}
                                                  {canLink && !d.startsWith("NEW-") ? (
                                                    <button
                                                      className="mono bug-link-key"
                                                      onClick={() => {
                                                        const base = (jiraUrl ?? "").trim().replace(/\/+$/, "");
                                                        BrowserOpenURL(`${base}/browse/${d}`);
                                                      }}
                                                      title={`Open ${d} in Jira`}
                                                    >
                                                      {d}
                                                    </button>
                                                  ) : (
                                                    <span className="mono">{d}</span>
                                                  )}
                                                </span>
                                              ))}
                                            </span>
                                          ) : (
                                            <span className="muted">—</span>
                                          )}
                                        </td>
                                      </tr>
                                    ))}
                                  </tbody>
                                </table>
                              )}
                            </td>
                          </tr>
                        )}
                      </Fragment>
                    );
                  })}
                </tbody>
              </table>
            )}
          </>
        )}
      </div>

      {detailKey && (
        <TestDetail
          profileId={profileId}
          testKey={detailKey}
          version={detailVersion}
          pendingForTest={[]}
          folders={[]}
          jiraUrl={jiraUrl}
          readOnly
          onClose={() => setDetailKey(null)}
          onEdited={() => {}}
        />
      )}

      {addToExecOpen && (
        <div className="modal-overlay" onClick={() => !addToExecWorking && setAddToExecOpen(false)}>
          <div className="modal bulk-modal" onClick={(e) => e.stopPropagation()}>
            <div className="pending-head">
              <h2>
                Add to Test Execution ({unionTestKeys.length}{" "}
                {unionTestKeys.length === 1 ? "test" : "tests"})
              </h2>
              <button
                className="btn btn-ghost"
                onClick={() => setAddToExecOpen(false)}
                disabled={addToExecWorking}
                title="Close"
              >
                ✕
              </button>
            </div>
            <div className="bulk-body">
              {addToExecLoading ? (
                <p className="muted">Loading Test Executions…</p>
              ) : addToExecExecs.length === 0 && !addToExecError ? (
                <p className="muted">
                  No Test Executions found. Sync containers first, or create a
                  new execution with "Create Test Execution".
                </p>
              ) : (
                <>
                  <label className="bulk-row">
                    <span>Filter</span>
                    <input
                      className="search"
                      placeholder="Filter by key or summary…"
                      value={addToExecFilter}
                      onChange={(e) => {
                        setAddToExecFilter(e.target.value);
                        // Reset target to first match when filter changes.
                        const f = e.target.value.trim().toLowerCase();
                        const filtered = !f
                          ? addToExecExecs
                          : addToExecExecs.filter(
                              (c) =>
                                c.key.toLowerCase().includes(f) ||
                                c.summary.toLowerCase().includes(f),
                            );
                        setAddToExecTarget(filtered.length > 0 ? filtered[0].key : "");
                      }}
                    />
                  </label>
                  {(() => {
                    const f = addToExecFilter.trim().toLowerCase();
                    const filtered = !f
                      ? addToExecExecs
                      : addToExecExecs.filter(
                          (c) =>
                            c.key.toLowerCase().includes(f) ||
                            c.summary.toLowerCase().includes(f),
                        );
                    return (
                      <label className="bulk-row">
                        <span>Execution</span>
                        {filtered.length === 0 ? (
                          <span className="muted">No executions match the filter.</span>
                        ) : (
                          <select
                            value={addToExecTarget}
                            onChange={(e) => setAddToExecTarget(e.target.value)}
                          >
                            {filtered.map((c) => (
                              <option key={c.key} value={c.key}>
                                {c.key} — {c.summary}
                              </option>
                            ))}
                          </select>
                        )}
                      </label>
                    );
                  })()}
                  <p className="muted bulk-preview">
                    The {unionTestKeys.length} affected test
                    {unionTestKeys.length === 1 ? "" : "s"} from the checked
                    bugs will be added to the chosen execution. Tests already in
                    it are skipped. Changes are queued locally; commit them from
                    the Pending list.
                  </p>
                </>
              )}
              {addToExecError && (
                <div className="error-text">{addToExecError}</div>
              )}
            </div>
            <div className="pending-actions">
              <button
                className="btn"
                onClick={() => setAddToExecOpen(false)}
                disabled={addToExecWorking}
              >
                Cancel
              </button>
              <button
                className="btn btn-primary"
                onClick={confirmAddToExec}
                disabled={
                  addToExecWorking ||
                  addToExecLoading ||
                  !addToExecTarget ||
                  unionTestKeys.length === 0
                }
              >
                {addToExecWorking ? "Working…" : "Add to Execution"}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
