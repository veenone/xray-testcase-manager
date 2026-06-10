import { Fragment, useEffect, useState } from "react";
import { ListSyncLog, errMsg } from "../api";
import type { SyncLogEntry } from "../api";
import { formatDateTimeLong, parseJiraDate } from "../dates";

interface Props {
  profileId: string;
  refreshKey: number;
  onClose: () => void;
}

const PAGE_SIZE = 15;

// SyncHistoryModal lists a profile's recent sync runs with success / failure
// detail (FR-1.7). Long error messages are shown in an expandable detail row
// rather than crammed into a column, and the list is paged (15 per page).
export function SyncHistoryModal({ profileId, refreshKey, onClose }: Props) {
  const [entries, setEntries] = useState<SyncLogEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [page, setPage] = useState(0);
  const [expanded, setExpanded] = useState<Set<number>>(new Set());

  useEffect(() => {
    if (!profileId) return;
    let cancelled = false;
    setLoading(true);
    setError("");
    setPage(0);
    setExpanded(new Set());
    ListSyncLog(profileId, 200)
      .then((es) => {
        if (!cancelled) setEntries(es ?? []);
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

  const totalPages = Math.max(1, Math.ceil(entries.length / PAGE_SIZE));
  const safePage = Math.min(page, totalPages - 1);
  const pageEntries = entries.slice(
    safePage * PAGE_SIZE,
    (safePage + 1) * PAGE_SIZE,
  );

  function toggle(id: number) {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div
        className="modal pending-modal"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="pending-head">
          <h2>Sync history</h2>
          <button className="btn btn-ghost" onClick={onClose} title="Close">
            ✕
          </button>
        </div>

        <div className="pending-table-wrap">
          {error && <div className="error-text">{error}</div>}
          {loading ? (
            <p className="muted pending-empty">Loading…</p>
          ) : entries.length === 0 ? (
            <p className="muted pending-empty">No syncs recorded yet.</p>
          ) : (
            <table className="pending-table synchist-table">
              <thead>
                <tr>
                  <th>Started</th>
                  <th>Duration</th>
                  <th>Outcome</th>
                  <th className="synchist-num">Fetched</th>
                  <th aria-label="Detail" />
                </tr>
              </thead>
              <tbody>
                {pageEntries.map((e) => {
                  const hasDetail = !!e.error;
                  const isOpen = expanded.has(e.id);
                  return (
                    <Fragment key={e.id}>
                      <tr
                        className={
                          "synchist-row" +
                          (hasDetail ? " synchist-clickable" : "")
                        }
                        onClick={() => hasDetail && toggle(e.id)}
                      >
                        <td>{formatDateTimeLong(e.startedAt)}</td>
                        <td>{duration(e.startedAt, e.finishedAt)}</td>
                        <td>
                          <span
                            className={
                              e.outcome === "success"
                                ? "run-badge run-pass"
                                : "run-badge run-fail"
                            }
                          >
                            {e.outcome}
                          </span>
                        </td>
                        <td className="synchist-num">
                          {e.fetched.toLocaleString()}
                        </td>
                        <td className="synchist-detail-cell">
                          {hasDetail ? (
                            <span className="synchist-caret">
                              {isOpen ? "▾ hide" : "▸ detail"}
                            </span>
                          ) : (
                            <span className="muted">—</span>
                          )}
                        </td>
                      </tr>
                      {hasDetail && isOpen && (
                        <tr className="synchist-detail-row">
                          <td colSpan={5}>
                            <pre className="synchist-detail">{e.error}</pre>
                          </td>
                        </tr>
                      )}
                    </Fragment>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>

        {entries.length > PAGE_SIZE && (
          <div className="board-pager synchist-pager">
            <span className="muted board-pager-range">
              {(safePage * PAGE_SIZE + 1).toLocaleString()}–
              {Math.min(
                (safePage + 1) * PAGE_SIZE,
                entries.length,
              ).toLocaleString()}{" "}
              of {entries.length.toLocaleString()} · page {safePage + 1} of{" "}
              {totalPages}
            </span>
            <span className="board-pager-nav">
              <button
                className="btn"
                disabled={safePage === 0}
                onClick={() => setPage((p) => Math.max(0, p - 1))}
              >
                ‹ Prev
              </button>
              <button
                className="btn"
                disabled={safePage >= totalPages - 1}
                onClick={() => setPage((p) => Math.min(totalPages - 1, p + 1))}
              >
                Next ›
              </button>
            </span>
          </div>
        )}
      </div>
    </div>
  );
}

function duration(start: string, end: string): string {
  if (!start || !end) return "—";
  const ms = parseJiraDate(end).getTime() - parseJiraDate(start).getTime();
  if (isNaN(ms) || ms < 0) return "—";
  if (ms < 1000) return `${ms} ms`;
  return `${(ms / 1000).toFixed(1)} s`;
}
