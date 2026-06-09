import { useEffect, useState } from "react";
import { ListSyncLog, errMsg } from "../api";
import type { SyncLogEntry } from "../api";
import { formatDateTimeLong, parseJiraDate } from "../dates";

interface Props {
  profileId: string;
  refreshKey: number;
  onClose: () => void;
}

// SyncHistoryModal lists a profile's recent sync runs with success / failure
// detail (FR-1.7).
export function SyncHistoryModal({ profileId, refreshKey, onClose }: Props) {
  const [entries, setEntries] = useState<SyncLogEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!profileId) return;
    let cancelled = false;
    setLoading(true);
    setError("");
    ListSyncLog(profileId, 50)
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
            <table className="pending-table">
              <thead>
                <tr>
                  <th>Started</th>
                  <th>Duration</th>
                  <th>Outcome</th>
                  <th>Fetched</th>
                  <th>Detail</th>
                </tr>
              </thead>
              <tbody>
                {entries.map((e) => (
                  <tr key={e.id}>
                    <td>{formatTime(e.startedAt)}</td>
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
                    <td>{e.fetched.toLocaleString()}</td>
                    <td className="sync-detail">{e.error || "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </div>
  );
}

function formatTime(s: string): string {
  return formatDateTimeLong(s);
}

function duration(start: string, end: string): string {
  if (!start || !end) return "—";
  const ms = parseJiraDate(end).getTime() - parseJiraDate(start).getTime();
  if (isNaN(ms) || ms < 0) return "—";
  if (ms < 1000) return `${ms} ms`;
  return `${(ms / 1000).toFixed(1)} s`;
}
