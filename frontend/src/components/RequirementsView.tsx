import { useEffect, useMemo, useState } from "react";
import {
  ListRequirementsWithCoverage,
  ListTestsForRequirement,
  EditRequirementField,
  DeleteRequirement,
  ExportRequirementAudit,
  SyncRequirements,
  BulkAssociateRequirements,
  errMsg,
} from "../api";
import type { RequirementCoverage, RequirementTest } from "../api";
import { RequirementSourcesModal } from "./RequirementSourcesModal";
import { AddTestsModal } from "./AddTestsModal";
import { TestDetail } from "./TestDetail";
import { Pager } from "./Pager";
import { SortControl } from "./SortControl";
import { keyCompare, cmpStr, applyDir } from "../sort";
import { useConfirm } from "./useConfirm";

interface Props {
  profileId: string;
  refreshKey: number;
  onChanged?: () => void;
}

const COVERAGE_ORDER = ["FAILED", "NOTRUN", "PASSED", "UNCOVERED"];
const COVERAGE_LABEL: Record<string, string> = {
  PASSED: "Passed",
  FAILED: "Failed",
  NOTRUN: "Not run",
  UNCOVERED: "Uncovered",
};

const COVERAGE_RANK: Record<string, number> = {
  FAILED: 0,
  NOTRUN: 1,
  PASSED: 2,
  UNCOVERED: 3,
};

function cmpReq(
  a: RequirementCoverage,
  b: RequirementCoverage,
  field: string,
): number {
  switch (field) {
    case "coverage":
      return (
        (COVERAGE_RANK[a.coverage] ?? 9) - (COVERAGE_RANK[b.coverage] ?? 9) ||
        keyCompare(a.key, b.key)
      );
    case "tests":
      return (a.testCount ?? 0) - (b.testCount ?? 0) || keyCompare(a.key, b.key);
    case "status":
      return cmpStr(a.status, b.status) || keyCompare(a.key, b.key);
    default:
      return keyCompare(a.key, b.key);
  }
}

// RequirementsView is the requirement coverage / traceability surface: a
// filterable master list of requirements (each with a derived coverage status,
// even when the requirement lives in a different project) on the left, and a
// detail pane on the right listing the Tests that cover the selected
// requirement with their run result. Read-only; recomputes when the profile
// changes or a sync/commit bumps refreshKey.
export function RequirementsView({ profileId, refreshKey, onChanged }: Props) {
  const [list, setList] = useState<RequirementCoverage[]>([]);
  const [selected, setSelected] = useState("");
  const [tests, setTests] = useState<RequirementTest[]>([]);
  const [filter, setFilter] = useState("");
  const [covFilter, setCovFilter] = useState("");
  const [sortField, setSortField] = useState("key");
  const [sortDesc, setSortDesc] = useState(true);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [showSources, setShowSources] = useState(false);
  const [showAdd, setShowAdd] = useState(false);
  const [editingSummary, setEditingSummary] = useState(false);
  const [draftSummary, setDraftSummary] = useState("");
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");
  const { confirm, confirmUI } = useConfirm();
  // A covering test opened in a slide-over detail panel (#5).
  const [detailKey, setDetailKey] = useState("");
  const [detailVersion, setDetailVersion] = useState(0);

  useEffect(() => {
    if (!profileId) return;
    let cancelled = false;
    setLoading(true);
    setError("");
    ListRequirementsWithCoverage(profileId)
      .then((rs) => {
        if (cancelled) return;
        setList(rs ?? []);
        setSelected((cur) =>
          cur && (rs ?? []).some((r) => r.key === cur)
            ? cur
            : rs && rs.length > 0
              ? rs[0].key
              : "",
        );
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
  }, [profileId, refreshKey]);

  useEffect(() => {
    if (!profileId || !selected) {
      setTests([]);
      return;
    }
    let cancelled = false;
    ListTestsForRequirement(profileId, selected)
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

  useEffect(() => {
    setEditingSummary(false);
  }, [selected]);

  const counts = useMemo(() => {
    const c: Record<string, number> = {};
    for (const r of list) c[r.coverage] = (c[r.coverage] ?? 0) + 1;
    return c;
  }, [list]);

  const filtered = useMemo(() => {
    const f = filter.trim().toLowerCase();
    const base = list.filter(
      (r) =>
        (!covFilter || r.coverage === covFilter) &&
        (!f ||
          r.key.toLowerCase().includes(f) ||
          r.summary.toLowerCase().includes(f)),
    );
    return [...base].sort((a, b) => applyDir(cmpReq(a, b, sortField), sortDesc));
  }, [list, filter, covFilter, sortField, sortDesc]);

  // Pagination of the (filtered) requirement list.
  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState(15);
  useEffect(() => {
    setPage(0);
  }, [filter, covFilter, sortField, sortDesc]);
  const totalPages = Math.max(1, Math.ceil(filtered.length / pageSize));
  const safePage = Math.min(page, totalPages - 1);
  const pageItems = filtered.slice(
    safePage * pageSize,
    (safePage + 1) * pageSize,
  );

  // Pagination of the covering-tests table in the detail pane.
  const [testsPage, setTestsPage] = useState(0);
  const [testsPageSize, setTestsPageSize] = useState(15);
  useEffect(() => {
    setTestsPage(0);
  }, [selected]);
  const testsTotalPages = Math.max(1, Math.ceil(tests.length / testsPageSize));
  const testsSafePage = Math.min(testsPage, testsTotalPages - 1);
  const pageTests = tests.slice(
    testsSafePage * testsPageSize,
    (testsSafePage + 1) * testsPageSize,
  );

  const sel = list.find((r) => r.key === selected) ?? null;

  const [syncing, setSyncing] = useState(false);
  async function syncRequirements() {
    setSyncing(true);
    setError("");
    setNotice("");
    try {
      await SyncRequirements(profileId);
      onChanged?.();
      setNotice("Requirements refreshed from Jira.");
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setSyncing(false);
    }
  }

  async function exportAudit() {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const path = await ExportRequirementAudit(profileId);
      if (path) setNotice(`Saved audit to ${path}`);
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }

  function startEdit() {
    if (!sel) return;
    setDraftSummary(sel.summary);
    setEditingSummary(true);
  }

  async function saveSummary() {
    if (!sel) return;
    const next = draftSummary.trim();
    if (!next || next === sel.summary) {
      setEditingSummary(false);
      return;
    }
    setBusy(true);
    setError("");
    try {
      await EditRequirementField(profileId, sel.key, "summary", next);
      setEditingSummary(false);
      onChanged?.();
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }

  async function deleteReq() {
    if (!sel) return;
    if (
      !(await confirm({
        title: "Delete requirement",
        message: `Delete requirement ${sel.key}? This removes it and its ${sel.testCount} coverage link${
          sel.testCount === 1 ? "" : "s"
        }, and queues the issue for deletion in Jira on the next commit.`,
        confirmLabel: "Delete",
        danger: true,
      }))
    )
      return;
    setBusy(true);
    setError("");
    try {
      await DeleteRequirement(profileId, sel.key);
      setSelected("");
      onChanged?.();
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="reqs">
      <div className="reqs-list">
        <div className="reqs-list-head">
          <span className="reqs-list-title">Requirements</span>
          <span className="reqs-list-actions">
            <button
              className="btn"
              onClick={syncRequirements}
              disabled={syncing}
              title="Refresh just the requirements from Jira (partial sync)"
            >
              {syncing ? "Syncing…" : "Sync"}
            </button>
            <button
              className="btn"
              onClick={exportAudit}
              disabled={busy || list.length === 0}
              title="Export the coverage / sign-off audit (CSV or XLSX)"
            >
              Export audit…
            </button>
            <button
              className="btn reqs-sources-btn"
              onClick={() => setShowSources(true)}
              title="Configure which projects requirements are pulled from"
            >
              Sources…
            </button>
          </span>
        </div>
        {notice && <p className="reqs-notice muted">{notice}</p>}
        <input
          className="search reqs-filter"
          placeholder="Filter by key or summary…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
        <div className="reqs-coverage-summary">
          {COVERAGE_ORDER.map((c) => (
            <button
              key={c}
              className={`cov-pill cov-${c.toLowerCase()}${covFilter === c ? " cov-pill-active" : ""}`}
              onClick={() => setCovFilter(covFilter === c ? "" : c)}
              title={`Filter to ${COVERAGE_LABEL[c]}`}
            >
              {COVERAGE_LABEL[c]} {counts[c] ?? 0}
            </button>
          ))}
        </div>
        <SortControl
          fields={[
            { value: "key", label: "Key" },
            { value: "coverage", label: "Coverage" },
            { value: "tests", label: "Tests" },
            { value: "status", label: "Status" },
          ]}
          field={sortField}
          desc={sortDesc}
          onChange={(f, d) => {
            setSortField(f);
            setSortDesc(d);
          }}
        />
        {loading ? (
          <p className="muted reqs-empty">Loading…</p>
        ) : filtered.length === 0 ? (
          <p className="muted reqs-empty">
            {list.length === 0
              ? "No requirements cached. Add a requirement source and sync, or sync a demo profile."
              : "No requirements match the filter."}
          </p>
        ) : (
          <>
            <ul className="reqs-items">
              {pageItems.map((r) => (
                <li
                  key={r.key}
                  className={`reqs-item${r.key === selected ? " reqs-item-selected" : ""}`}
                  onClick={() => setSelected(r.key)}
                >
                  <div className="reqs-item-top">
                    <span className="mono reqs-key">{r.key}</span>
                    {r.issueType && (
                      <span
                        className={`kind-badge req-kind req-kind-${r.issueType.toLowerCase()}`}
                      >
                        {r.issueType}
                      </span>
                    )}
                    <span
                      className={`cov-badge cov-${r.coverage.toLowerCase()}`}
                    >
                      {COVERAGE_LABEL[r.coverage]}
                    </span>
                  </div>
                  <div className="reqs-item-summary">
                    {r.summary || "(no summary)"}
                  </div>
                  <div className="reqs-item-meta muted">
                    {r.testCount} test{r.testCount === 1 ? "" : "s"}
                  </div>
                </li>
              ))}
            </ul>
            {filtered.length > 0 && (
              <Pager
                compact
                page={safePage}
                pageSize={pageSize}
                total={filtered.length}
                onPage={setPage}
                onPageSize={(n) => {
                  setPageSize(n);
                  setPage(0);
                }}
              />
            )}
          </>
        )}
      </div>

      <div className="reqs-detail">
        {error && <div className="error-text">{error}</div>}
        {!sel ? (
          <p className="muted">Select a requirement to see its coverage.</p>
        ) : (
          <>
            <div className="reqs-detail-head">
              <span className="kind-badge req-kind">{sel.issueType}</span>
              <span className="mono reqs-detail-key">{sel.key}</span>
              <span className="muted reqs-detail-project">{sel.projectKey}</span>
              {sel.status && <span className="status-pill">{sel.status}</span>}
              <span
                className={`cov-badge cov-${sel.coverage.toLowerCase()} reqs-detail-cov`}
              >
                {COVERAGE_LABEL[sel.coverage]}
              </span>
              <span className="reqs-detail-actions">
                {!editingSummary && (
                  <button
                    className="btn"
                    onClick={startEdit}
                    disabled={busy}
                    title="Edit the requirement summary"
                  >
                    Edit
                  </button>
                )}
                <button
                  className="btn btn-danger"
                  onClick={deleteReq}
                  disabled={busy}
                  title="Delete this requirement (queued for commit)"
                >
                  Delete
                </button>
              </span>
            </div>
            {editingSummary ? (
              <div className="reqs-detail-edit">
                <input
                  className="search reqs-summary-input"
                  value={draftSummary}
                  autoFocus
                  disabled={busy}
                  onChange={(e) => setDraftSummary(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") saveSummary();
                    if (e.key === "Escape") setEditingSummary(false);
                  }}
                />
                <button className="btn" onClick={saveSummary} disabled={busy}>
                  Save
                </button>
                <button
                  className="btn"
                  onClick={() => setEditingSummary(false)}
                  disabled={busy}
                >
                  Cancel
                </button>
              </div>
            ) : (
              <h2 className="reqs-detail-summary">
                {sel.summary || "(no summary)"}
              </h2>
            )}

            <div className="precond-tests-head">
              <h4>Covering tests ({tests.length})</h4>
              <button
                className="btn"
                onClick={() => setShowAdd(true)}
                disabled={!sel}
                title="Link tests to this requirement"
              >
                + Add tests
              </button>
            </div>
            {tests.length === 0 ? (
              <p className="muted">No tests cover this requirement.</p>
            ) : (
              <>
                <table className="board-table">
                  <thead>
                    <tr>
                      <th>Test</th>
                      <th>Summary</th>
                      <th>Status</th>
                      <th>Result</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pageTests.map((t) => (
                      <tr
                        key={t.key}
                        className={`reqs-test-row${t.key === detailKey ? " reqs-test-row-active" : ""}`}
                        onClick={() => setDetailKey(t.key)}
                        title="Open this test's detail"
                      >
                        <td className="mono">{t.key}</td>
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
                    ))}
                  </tbody>
                </table>
                {tests.length > testsPageSize && (
                  <Pager
                    page={testsSafePage}
                    pageSize={testsPageSize}
                    total={tests.length}
                    onPage={setTestsPage}
                    onPageSize={(n) => {
                      setTestsPageSize(n);
                      setTestsPage(0);
                    }}
                  />
                )}
              </>
            )}
          </>
        )}
      </div>

      {showSources && (
        <RequirementSourcesModal
          profileId={profileId}
          onClose={() => setShowSources(false)}
        />
      )}

      {showAdd && sel && (
        <AddTestsModal
          profileId={profileId}
          containerKey={selected}
          targetLabel={sel.key}
          existingKeys={tests.map((t) => t.key)}
          onAdd={(keys) =>
            BulkAssociateRequirements(profileId, keys, [selected], true).then(
              () => undefined,
            )
          }
          onCancel={() => setShowAdd(false)}
          onDone={() => {
            setShowAdd(false);
            onChanged?.();
          }}
        />
      )}

      {detailKey && (
        <div
          className="reqs-detail-overlay"
          onClick={() => setDetailKey("")}
        >
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
      {confirmUI}
    </div>
  );
}
