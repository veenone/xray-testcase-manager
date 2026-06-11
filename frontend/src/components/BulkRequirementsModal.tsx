import { useEffect, useState } from "react";
import { ListRequirementsWithCoverage, BulkAssociateRequirements, errMsg } from "../api";
import type { RequirementCoverage, BulkEditResult } from "../api";

interface Props {
  profileId: string;
  testKeys: string[];
  onComplete: () => void;
  onCancel: () => void;
}

// BulkRequirementsModal links or unlinks a requirement across the selected
// Tests at once.
export function BulkRequirementsModal({
  profileId,
  testKeys,
  onComplete,
  onCancel,
}: Props) {
  const [requirements, setRequirements] = useState<RequirementCoverage[]>([]);
  const [target, setTarget] = useState("");
  const [mode, setMode] = useState<"add" | "remove">("add");
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [applying, setApplying] = useState(false);
  const [applyError, setApplyError] = useState("");
  const [result, setResult] = useState<BulkEditResult | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    ListRequirementsWithCoverage(profileId)
      .then((rs) => {
        if (cancelled) return;
        setRequirements(rs ?? []);
        setTarget(rs && rs.length > 0 ? rs[0].key : "");
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
  }, [profileId]);

  async function apply() {
    if (!target) return;
    setApplying(true);
    setApplyError("");
    try {
      const r = await BulkAssociateRequirements(
        profileId,
        testKeys,
        [target],
        mode === "add",
      );
      setResult(r);
    } catch (e) {
      setApplyError(errMsg(e));
    } finally {
      setApplying(false);
    }
  }

  return (
    <div className="modal-overlay" onClick={onCancel}>
      <div className="modal bulk-modal" onClick={(e) => e.stopPropagation()}>
        <div className="pending-head">
          <h2>
            Requirements ({testKeys.length}{" "}
            {testKeys.length === 1 ? "test" : "tests"})
          </h2>
          <button className="btn btn-ghost" onClick={onCancel} title="Close">
            ✕
          </button>
        </div>

        {!result && (
          <div className="bulk-body">
            <label className="bulk-row">
              <span>Action</span>
              <select
                value={mode}
                onChange={(e) => setMode(e.target.value as "add" | "remove")}
              >
                <option value="add">Link</option>
                <option value="remove">Unlink</option>
              </select>
            </label>

            <label className="bulk-row">
              <span>Requirement</span>
              {loading ? (
                <span className="muted">Loading…</span>
              ) : (
                <select
                  value={target}
                  onChange={(e) => setTarget(e.target.value)}
                  disabled={requirements.length === 0}
                >
                  {requirements.length === 0 && (
                    <option value="">None synced</option>
                  )}
                  {requirements.map((r) => (
                    <option key={r.key} value={r.key}>
                      {r.key} — {r.summary}
                    </option>
                  ))}
                </select>
              )}
            </label>

            {loadError && <div className="error-text">{loadError}</div>}

            <p className="muted bulk-preview">
              {mode === "add"
                ? "The requirement is linked to tests that don't already cover it."
                : "The requirement link is removed from tests that have it."}{" "}
              Changes are queued locally; commit them from the Pending list.
            </p>

            {applyError && <div className="error-text">{applyError}</div>}
          </div>
        )}

        {result && (
          <div className="bulk-body">
            {result.succeeded.length > 0 && (
              <p className="ok-text">
                ✓ Updated {result.succeeded.length}{" "}
                {result.succeeded.length === 1 ? "test" : "tests"}.
              </p>
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
                disabled={applying || loading || !target}
              >
                {applying ? "Applying…" : "Apply"}
              </button>
            </>
          ) : (
            <button className="btn btn-primary" onClick={onComplete}>
              Done
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
