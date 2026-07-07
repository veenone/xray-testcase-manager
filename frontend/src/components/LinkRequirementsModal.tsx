import { useEffect, useState } from "react";
import {
  ListRequirementsWithCoverage,
  SetRequirementLinks,
  errMsg,
} from "../api";
import type { RequirementCoverage } from "../api";

interface Props {
  profileId: string;
  /** The requirement whose outbound "requires" links are being edited. */
  fromKey: string;
  /** Currently linked target keys (pre-selected in the modal). */
  currentLinkedKeys: string[];
  onClose: () => void;
  onDone: () => void;
}

const PAGE_SIZE = 50;

// LinkRequirementsModal lets the user pick other requirements that the selected
// requirement "requires", replacing the current set and queuing the change for
// commit. Layout mirrors AddTestsModal.
export function LinkRequirementsModal({
  profileId,
  fromKey,
  currentLinkedKeys,
  onClose,
  onDone,
}: Props) {
  const [all, setAll] = useState<RequirementCoverage[]>([]);
  const [filter, setFilter] = useState("");
  const [selected, setSelected] = useState<Set<string>>(
    new Set(currentLinkedKeys),
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [page, setPage] = useState(0);

  useEffect(() => {
    ListRequirementsWithCoverage(profileId)
      .then((rs) => setAll(rs ?? []))
      .catch((e) => setError(errMsg(e)));
  }, [profileId]);

  // Reset to first page when filter changes.
  useEffect(() => {
    setPage(0);
  }, [filter]);

  const candidates = all.filter(
    (r) =>
      r.key !== fromKey &&
      (!filter.trim() ||
        r.key.toLowerCase().includes(filter.trim().toLowerCase()) ||
        r.summary.toLowerCase().includes(filter.trim().toLowerCase())),
  );

  const total = candidates.length;
  const pageItems = candidates.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE);

  function toggle(key: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.add(key);
      }
      return next;
    });
  }

  async function save() {
    setBusy(true);
    setError("");
    try {
      await SetRequirementLinks(
        profileId,
        fromKey,
        "requires",
        Array.from(selected),
      );
      onDone();
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal pending-modal" onClick={(e) => e.stopPropagation()}>
        <div className="pending-head">
          <h2>Link requirements to {fromKey}</h2>
          <button className="btn btn-ghost" onClick={onClose} title="Close">
            ✕
          </button>
        </div>

        <div className="add-tests-body">
          <div className="add-tests-main">
            <p className="muted" style={{ margin: "0 0 8px" }}>
              Select requirements that <strong>{fromKey}</strong> requires. The
              change is queued locally and pushed to Jira on the next commit.
            </p>
            {error && <div className="error-text">{error}</div>}
            <input
              className="detail-input"
              placeholder="Filter by key or summary…"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              autoFocus
            />
            <ul className="add-test-list">
              {pageItems.length === 0 ? (
                <li className="muted">No other requirements available.</li>
              ) : (
                pageItems.map((r) => (
                  <li key={r.key}>
                    <label>
                      <input
                        type="checkbox"
                        checked={selected.has(r.key)}
                        onChange={() => toggle(r.key)}
                      />
                      <span className="mono">{r.key}</span>
                      {r.summary || "(no summary)"}
                    </label>
                  </li>
                ))
              )}
            </ul>
            {total > PAGE_SIZE && (
              <div className="add-test-pager">
                <button
                  className="btn"
                  disabled={page === 0}
                  onClick={() => setPage((p) => Math.max(0, p - 1))}
                >
                  ‹ Prev
                </button>
                <span className="muted">
                  {(page * PAGE_SIZE + 1).toLocaleString()}–
                  {Math.min((page + 1) * PAGE_SIZE, total).toLocaleString()} of{" "}
                  {total.toLocaleString()}
                </span>
                <button
                  className="btn"
                  disabled={(page + 1) * PAGE_SIZE >= total}
                  onClick={() => setPage((p) => p + 1)}
                >
                  Next ›
                </button>
              </div>
            )}
          </div>
        </div>

        <div className="pending-actions">
          <button className="btn" onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button className="btn btn-primary" onClick={save} disabled={busy}>
            {busy
              ? "Saving…"
              : `Link ${selected.size} requirement${selected.size === 1 ? "" : "s"}`}
          </button>
        </div>
      </div>
    </div>
  );
}
