import { useEffect, useMemo, useState } from "react";
import { useProfile } from "../contexts/ProfileContext";
import { Modal } from "./Modal";
import {
  GetBulkTransitionOptions,
  BulkTransitionTests,
  errMsg,
} from "../api";
import type { BulkTransitionOptions, BulkTransitionResult } from "../api";

interface Props {
  testKeys: string[];
  onComplete: (result: BulkTransitionResult) => void;
  onCancel: () => void;
}

// BulkTransitionModal lets the user move many Tests through the workflow
// in one go (FR-3.8). On open it asks the backend which target statuses
// are actually reachable from the current selection — so the dropdown
// never offers a status that would skip every Test.
export function BulkTransitionModal({
  testKeys,
  onComplete,
  onCancel,
}: Props) {
  const { activeId: profileId } = useProfile();
  const [options, setOptions] = useState<BulkTransitionOptions | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [target, setTarget] = useState("");
  const [applying, setApplying] = useState(false);
  const [applyError, setApplyError] = useState("");
  const [result, setResult] = useState<BulkTransitionResult | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setLoadError("");
    GetBulkTransitionOptions(profileId, testKeys)
      .then((o) => {
        if (cancelled) return;
        setOptions(o);
        if (o.reachableTargets.length > 0) {
          setTarget(o.reachableTargets[0]);
        }
      })
      .catch((e) => {
        if (!cancelled) setLoadError(errMsg(e));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [profileId, testKeys]);

  // Ordered list of (status, count) so the rendered summary is stable
  // across re-renders.
  const statusCounts = useMemo(() => {
    if (!options) return [] as Array<[string, number]>;
    return Object.entries(options.currentStatusCounts).sort((a, b) =>
      a[0].localeCompare(b[0]),
    );
  }, [options]);

  async function apply() {
    if (!target) return;
    setApplying(true);
    setApplyError("");
    try {
      const r = await BulkTransitionTests(profileId, testKeys, target);
      setResult(r);
      if (r.skipped.length === 0 && r.failed.length === 0) {
        onComplete(r);
      }
    } catch (e) {
      setApplyError(errMsg(e));
    } finally {
      setApplying(false);
    }
  }

  return (
    <Modal onClose={onCancel} className="modal bulk-modal" labelledBy="bulk-transition-title">
        <div className="pending-head">
          <h2 id="bulk-transition-title">
            Bulk transition ({testKeys.length}{" "}
            {testKeys.length === 1 ? "test" : "tests"})
          </h2>
          <button className="btn btn-ghost" onClick={onCancel} title="Close">
            ✕
          </button>
        </div>

        {loading && <div className="bulk-body muted">Loading workflow…</div>}
        {loadError && (
          <div className="bulk-body error-text">
            Could not load workflow: {loadError}
          </div>
        )}

        {!loading && !loadError && options && !result && (
          <div className="bulk-body">
            <div className="bulk-row">
              <span>Selection</span>
              <div className="status-summary">
                {statusCounts.length === 0 ? (
                  <span className="muted">no readable tests</span>
                ) : (
                  statusCounts.map(([s, n]) => (
                    <span key={s} className="status-pill">
                      {s}: {n}
                    </span>
                  ))
                )}
              </div>
            </div>

            <label className="bulk-row">
              <span>Move to</span>
              <select
                value={target}
                onChange={(e) => setTarget(e.target.value)}
                disabled={options.reachableTargets.length === 0}
              >
                {options.reachableTargets.length === 0 && (
                  <option value="">No reachable targets</option>
                )}
                {options.reachableTargets.map((t) => (
                  <option key={t} value={t}>
                    {t}
                  </option>
                ))}
              </select>
            </label>

            <p className="muted bulk-preview">
              Tests already at the target status, and tests with no
              transition leading to it, are skipped. The rest are queued
              as local pending changes; commit them from the Pending list.
            </p>

            {applyError && <div className="error-text">{applyError}</div>}
          </div>
        )}

        {result && (
          <div className="bulk-body">
            {result.succeeded.length > 0 && (
              <p className="ok-text">
                ✓ Queued transitions on {result.succeeded.length}{" "}
                {result.succeeded.length === 1 ? "test" : "tests"}.
              </p>
            )}
            {result.skipped.length > 0 && (
              <div className="warn-text">
                <p>Skipped ({result.skipped.length}):</p>
                <ul className="commit-fail-list">
                  {result.skipped.map((s, i) => (
                    <li key={i}>
                      <span className="mono">{s.testKey}</span>: {s.reason}
                    </li>
                  ))}
                </ul>
              </div>
            )}
            {result.failed.length > 0 && (
              <div className="error-text">
                <p>Failed ({result.failed.length}):</p>
                <ul className="commit-fail-list">
                  {result.failed.map((f, i) => (
                    <li key={i}>
                      <span className="mono">{f.testKey}</span>: {f.error}
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </div>
        )}

        <div className="pending-actions">
          {!result ? (
            <>
              <button className="btn" onClick={onCancel} disabled={applying}>
                Cancel
              </button>
              <button
                className="btn btn-primary"
                onClick={apply}
                disabled={
                  applying ||
                  loading ||
                  !target ||
                  (options?.reachableTargets.length ?? 0) === 0
                }
              >
                {applying ? "Applying…" : "Apply"}
              </button>
            </>
          ) : (
            <button className="btn btn-primary" onClick={onCancel}>
              Close
            </button>
          )}
        </div>
    </Modal>
  );
}
