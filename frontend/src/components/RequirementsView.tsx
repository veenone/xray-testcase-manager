import { useEffect, useMemo, useState } from "react";
import {
  ListRequirementsWithCoverage,
  ListTestsForRequirement,
  EditRequirementField,
  DeleteRequirement,
  errMsg,
} from "../api";
import type { RequirementCoverage, RequirementTest } from "../api";
import { RequirementSourcesModal } from "./RequirementSourcesModal";

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
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [showSources, setShowSources] = useState(false);
  const [editingSummary, setEditingSummary] = useState(false);
  const [draftSummary, setDraftSummary] = useState("");
  const [busy, setBusy] = useState(false);

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
    return list.filter(
      (r) =>
        (!covFilter || r.coverage === covFilter) &&
        (!f ||
          r.key.toLowerCase().includes(f) ||
          r.summary.toLowerCase().includes(f)),
    );
  }, [list, filter, covFilter]);

  const sel = list.find((r) => r.key === selected) ?? null;

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
      !window.confirm(
        `Delete requirement ${sel.key}? This removes it and its ${sel.testCount} coverage link${
          sel.testCount === 1 ? "" : "s"
        }, and queues the issue for deletion in Jira on the next commit.`,
      )
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
          <button
            className="btn reqs-sources-btn"
            onClick={() => setShowSources(true)}
            title="Configure which projects requirements are pulled from"
          >
            Sources…
          </button>
        </div>
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
        {loading ? (
          <p className="muted reqs-empty">Loading…</p>
        ) : filtered.length === 0 ? (
          <p className="muted reqs-empty">
            {list.length === 0
              ? "No requirements cached. Add a requirement source and sync, or sync a demo profile."
              : "No requirements match the filter."}
          </p>
        ) : (
          <ul className="reqs-items">
            {filtered.map((r) => (
              <li
                key={r.key}
                className={`reqs-item${r.key === selected ? " reqs-item-selected" : ""}`}
                onClick={() => setSelected(r.key)}
              >
                <div className="reqs-item-top">
                  <span className="mono reqs-key">{r.key}</span>
                  <span className={`cov-badge cov-${r.coverage.toLowerCase()}`}>
                    {COVERAGE_LABEL[r.coverage]}
                  </span>
                </div>
                <div className="reqs-item-summary">
                  {r.summary || "(no summary)"}
                </div>
                <div className="reqs-item-meta muted">
                  {r.issueType} · {r.testCount} test
                  {r.testCount === 1 ? "" : "s"}
                </div>
              </li>
            ))}
          </ul>
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

            <h4>
              Covering tests ({tests.length})
            </h4>
            {tests.length === 0 ? (
              <p className="muted">No tests cover this requirement.</p>
            ) : (
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
                  {tests.map((t) => (
                    <tr key={t.key}>
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
    </div>
  );
}
