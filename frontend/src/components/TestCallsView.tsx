import { useEffect, useMemo, useState } from "react";
import { useViewState } from "../lib/viewState";
import { ListTestCallLinks, SyncTestCalls, EventsOn, errMsg } from "../api";
import type { TestCallLink, SyncProgress } from "../api";
import { TestDetail } from "./TestDetail";
import { Pager } from "./Pager";

interface Props {
  profileId: string;
  refreshKey: number;
  onChanged?: () => void;
}

// findCallCycles returns the set of test keys that take part in a call cycle —
// A calls B and B (directly or transitively) calls A. A cyclic test call would
// recurse forever when executed, so it's the most important thing to flag here;
// the view paints every test in the cycle red.
//
// It uses Tarjan's strongly-connected-components algorithm: a node lies on a
// cycle exactly when it belongs to an SCC of more than one node, or has a
// self-loop. One O(V+E) pass finds them all, independent of traversal order.
function findCallCycles(links: TestCallLink[]): Set<string> {
  const adj = new Map<string, string[]>();
  const selfLoops = new Set<string>();
  for (const l of links) {
    const out = adj.get(l.callerKey);
    if (out) out.push(l.calledKey);
    else adj.set(l.callerKey, [l.calledKey]);
    if (l.callerKey === l.calledKey) selfLoops.add(l.callerKey);
  }

  let counter = 0;
  const index = new Map<string, number>();
  const lowlink = new Map<string, number>();
  const onStack = new Set<string>();
  const stack: string[] = [];
  const inCycle = new Set<string>();

  const strongConnect = (v: string) => {
    index.set(v, counter);
    lowlink.set(v, counter);
    counter++;
    stack.push(v);
    onStack.add(v);

    for (const w of adj.get(v) ?? []) {
      if (!index.has(w)) {
        strongConnect(w);
        lowlink.set(v, Math.min(lowlink.get(v)!, lowlink.get(w)!));
      } else if (onStack.has(w)) {
        lowlink.set(v, Math.min(lowlink.get(v)!, index.get(w)!));
      }
    }

    // v is an SCC root: pop the component off the stack.
    if (lowlink.get(v) === index.get(v)) {
      const scc: string[] = [];
      let w: string;
      do {
        w = stack.pop()!;
        onStack.delete(w);
        scc.push(w);
      } while (w !== v);
      if (scc.length > 1) for (const n of scc) inCycle.add(n);
    }
  };

  for (const v of adj.keys()) {
    if (!index.has(v)) strongConnect(v);
  }
  for (const n of selfLoops) inCycle.add(n);
  return inCycle;
}

// TestCallsView shows the "call test" relationships across the project: which
// tests call which (#2 follow-up). Grouped by caller, broken calls flagged, and
// cyclic calls highlighted. Clicking a test opens its full detail.
export function TestCallsView({ profileId, refreshKey, onChanged }: Props) {
  const [links, setLinks] = useState<TestCallLink[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [detailKey, setDetailKey] = useViewState(profileId, "testcalls", "detailKey", "");
  const [detailVersion, setDetailVersion] = useState(0);
  const [page, setPage] = useViewState(profileId, "testcalls", "page", 0);
  const [pageSize, setPageSize] = useViewState(profileId, "testcalls", "pageSize", 25);
  // Caller keys whose callee list is collapsed (default: all expanded).
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const [syncing, setSyncing] = useState(false);
  const [syncError, setSyncError] = useState("");
  // Live per-caller progress for the toolbar bar while a sync runs.
  const [syncProgress, setSyncProgress] = useState<SyncProgress | null>(null);
  // When the last manual sync finished (local clock), for the toolbar note.
  const [lastSynced, setLastSynced] = useState<Date | null>(null);
  // Bumped after a partial sync to re-pull the (now refreshed) call links.
  const [reload, setReload] = useState(0);

  useEffect(() => {
    if (!profileId) return;
    let cancelled = false;
    setLoading(true);
    setError("");
    ListTestCallLinks(profileId)
      .then((ls) => {
        if (!cancelled) setLinks(ls ?? []);
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
  }, [profileId, refreshKey, detailVersion, reload]);

  // While a sync runs, mirror its "testcalls:progress" events into the toolbar's
  // local progress bar. This is a dedicated channel (not the global
  // "sync:progress") so the refresh does not touch the footer sync bar. The
  // terminal done event clears it.
  useEffect(() => {
    if (!syncing) return;
    return EventsOn("testcalls:progress", (p: SyncProgress) => {
      if (p.done) setSyncProgress(null);
      else setSyncProgress(p);
    });
  }, [syncing]);

  // syncCalls re-pulls steps for the known caller tests so the call graph
  // refreshes without a full profile sync (RND_P_4TFINT_05-207).
  async function syncCalls() {
    setSyncing(true);
    setSyncError("");
    setSyncProgress(null);
    try {
      await SyncTestCalls(profileId);
      setLastSynced(new Date());
      setReload((r) => r + 1);
      onChanged?.();
    } catch (e) {
      setSyncError(errMsg(e));
    } finally {
      setSyncing(false);
      setSyncProgress(null);
    }
  }

  // Group the flat edge list by caller, preserving step order.
  const callers = useMemo(() => {
    const m = new Map<string, { summary: string; calls: TestCallLink[] }>();
    for (const l of links) {
      const entry = m.get(l.callerKey) ?? { summary: l.callerSummary, calls: [] };
      entry.calls.push(l);
      m.set(l.callerKey, entry);
    }
    return [...m.entries()].sort((a, b) => a[0].localeCompare(b[0]));
  }, [links]);

  const cycles = useMemo(() => findCallCycles(links), [links]);
  const brokenCount = useMemo(
    () => links.filter((l) => !l.calledExists).length,
    [links],
  );
  const callerCount = callers.length;

  // Paginate the caller cards so a project with many linked tests stays usable.
  const totalPages = Math.max(1, Math.ceil(callerCount / pageSize));
  const safePage = Math.min(page, totalPages - 1);
  const pageCallers = callers.slice(
    safePage * pageSize,
    safePage * pageSize + pageSize,
  );

  function toggle(key: string) {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }
  const expandAll = () => setCollapsed(new Set());
  const collapseAll = () => setCollapsed(new Set(callers.map(([k]) => k)));

  if (loading && links.length === 0) {
    return <div className="dashboard muted">Loading call relationships…</div>;
  }
  if (error) {
    return <div className="dashboard error-text">{error}</div>;
  }

  return (
    <div className="testcalls">
      <div className="dup-toolbar">
        <button
          className="btn btn-primary"
          onClick={syncCalls}
          disabled={syncing}
          title="Re-pull steps for the known caller tests to refresh the call graph (no full sync)"
        >
          {syncing ? "Syncing…" : "↻ Sync"}
        </button>
        {syncing && syncProgress && !syncProgress.done && (
          <TestCallsSyncBar progress={syncProgress} />
        )}
        {lastSynced && !syncing && (
          <span className="muted">
            calls last synced {lastSynced.toLocaleString()}
          </span>
        )}
        <span style={{ flex: 1 }} />
        <span className="muted">
          Which tests call other tests (Xray test calls) in their steps.
        </span>
      </div>

      {syncError && <div className="error-text dup-error">{syncError}</div>}

      <div className="dup-tiles">
        <div className="dup-tile t-grp">
          <b>{callerCount}</b>
          <span>calling</span>
        </div>
        <div className="dup-tile t-grp">
          <b>{links.length}</b>
          <span>relationships</span>
        </div>
        <div className="dup-tile t-diff">
          <b>{brokenCount}</b>
          <span>broken</span>
        </div>
        <div className="dup-tile t-dup">
          <b>{cycles.size}</b>
          <span>in a cycle</span>
        </div>
      </div>

      <div className="testcalls-body">
        {links.length === 0 ? (
        <p className="muted testcalls-empty">
          No call-test relationships yet. Open a test, add a{" "}
          <b>+ Call test</b> step in its detail panel, and the caller → called
          links appear here.
        </p>
      ) : (
        <>
          <div className="testcalls-controls">
            <div className="testcalls-bulk">
              <button className="btn" onClick={expandAll} title="Expand every caller">
                Expand all
              </button>
              <button className="btn" onClick={collapseAll} title="Collapse every caller">
                Collapse all
              </button>
            </div>
          </div>
          <ul className="testcalls-list">
            {pageCallers.map(([callerKey, { summary, calls }]) => {
              const isCollapsed = collapsed.has(callerKey);
              return (
                <li
                  key={callerKey}
                  className={`testcalls-card${cycles.has(callerKey) ? " testcalls-card-cycle" : ""}`}
                >
                  <div className="testcalls-caller">
                    <button
                      className="testcalls-toggle"
                      onClick={() => toggle(callerKey)}
                      aria-expanded={!isCollapsed}
                      title={isCollapsed ? "Expand calls" : "Collapse calls"}
                    >
                      {isCollapsed ? "▸" : "▾"}
                    </button>
                    <button
                      className="link-btn testcalls-key"
                      onClick={() => setDetailKey(callerKey)}
                      title="Open this test's detail"
                    >
                      <span className="mono">{callerKey}</span>
                    </button>
                    <span className="testcalls-summary">{summary}</span>
                    <span
                      className="testcalls-count"
                      title={`${calls.length} call${calls.length === 1 ? "" : "s"}`}
                    >
                      {calls.length}
                    </span>
                    {cycles.has(callerKey) && (
                      <span className="testcalls-badge testcalls-badge-cycle" title="This test is part of a call cycle">
                        cycle
                      </span>
                    )}
                  </div>
                  {!isCollapsed && (
                    <ul className="testcalls-callees">
                      {calls.map((l) => (
                  <li key={`${l.calledKey}-${l.stepIndex}`} className="testcalls-callee">
                    <span className="testcalls-arrow" aria-hidden="true">
                      ⮡
                    </span>
                    {l.calledExists ? (
                      <button
                        className="link-btn testcalls-key"
                        onClick={() => setDetailKey(l.calledKey)}
                        title="Open the called test's detail"
                      >
                        <span className="mono">{l.calledKey}</span>
                      </button>
                    ) : (
                      <span className="mono testcalls-key-broken">{l.calledKey}</span>
                    )}
                    <span className="testcalls-summary">
                      {l.calledExists ? l.calledSummary : ""}
                    </span>
                    {!l.calledExists && (
                      <span
                        className="testcalls-badge testcalls-badge-broken"
                        title="The called test isn't in the local cache (deleted, never synced, or in another project)"
                      >
                        missing
                      </span>
                    )}
                    {cycles.has(l.calledKey) && (
                      <span className="testcalls-badge testcalls-badge-cycle" title="Part of a call cycle">
                        cycle
                      </span>
                    )}
                          </li>
                        ))}
                      </ul>
                  )}
                </li>
              );
            })}
          </ul>
          <div className="testcalls-pager">
            <Pager
              page={safePage}
              pageSize={pageSize}
              total={callerCount}
              onPage={setPage}
              onPageSize={(n) => {
                setPageSize(n);
                setPage(0);
              }}
            />
          </div>
        </>
      )}
      </div>

      {detailKey && (
        <div className="detail-slideover" onClick={() => setDetailKey("")}>
          <div onClick={(e) => e.stopPropagation()}>
            <TestDetail
              profileId={profileId}
              testKey={detailKey}
              version={detailVersion}
              pendingForTest={[]}
              folders={[]}
              onClose={() => setDetailKey("")}
              onEdited={() => {
                setDetailVersion((v) => v + 1);
                onChanged?.();
              }}
            />
          </div>
        </div>
      )}
    </div>
  );
}

// TestCallsSyncBar renders the per-caller refresh progress in the toolbar,
// reusing the shared syncbar styling so it matches the Duplicates view and the
// app's status-bar sync readout.
function TestCallsSyncBar({ progress }: { progress: SyncProgress }) {
  const hasCount = progress.total > 0;
  const pct = hasCount
    ? Math.round((progress.fetched / progress.total) * 100)
    : 0;
  const stage = progress.stage || "Refreshing test calls";
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
