import { useEffect, useState } from "react";
import { useProfile } from "../contexts/ProfileContext";
import {
  ListRequirementsWithCoverage,
  BulkAssociateRequirements,
  BulkReplaceRequirements,
  errMsg,
} from "../api";
import type { RequirementCoverage, BulkEditResult } from "../api";
import { Modal } from "./Modal";
import { SearchableSelect } from "./SearchableSelect";

interface Props {
  testKeys: string[];
  onComplete: () => void;
  onCancel: () => void;
}

type Mode = "add" | "remove" | "replace";

// togglePick flips a key in a staged multi-select set.
function togglePick(prev: Set<string>, key: string): Set<string> {
  const next = new Set(prev);
  if (next.has(key)) next.delete(key);
  else next.add(key);
  return next;
}

// BulkRequirementsModal links, unlinks, or swaps (Replace mode) a requirement
// across the selected Tests at once (RND_P_4TFINT_05-231).
export function BulkRequirementsModal({
  testKeys,
  onComplete,
  onCancel,
}: Props) {
  const { activeId: profileId } = useProfile();
  const [requirements, setRequirements] = useState<RequirementCoverage[]>([]);
  const [target, setTarget] = useState("");
  const [mode, setMode] = useState<Mode>("add");
  const [toRemove, setToRemove] = useState<Set<string>>(new Set());
  const [toAdd, setToAdd] = useState<Set<string>>(new Set());
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

  const canApply =
    mode === "replace" ? toRemove.size > 0 || toAdd.size > 0 : !!target;

  async function apply() {
    setApplying(true);
    setApplyError("");
    try {
      let r: BulkEditResult;
      if (mode === "replace") {
        r = await BulkReplaceRequirements(
          profileId,
          testKeys,
          [...toRemove],
          [...toAdd],
        );
      } else {
        if (!target) return;
        r = await BulkAssociateRequirements(
          profileId,
          testKeys,
          [target],
          mode === "add",
        );
      }
      setResult(r);
    } catch (e) {
      setApplyError(errMsg(e));
    } finally {
      setApplying(false);
    }
  }

  return (
    <Modal onClose={onCancel} className="modal bulk-modal" labelledBy="bulk-requirements-title">
        <div className="pending-head">
          <h2 id="bulk-requirements-title">
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
                onChange={(e) => setMode(e.target.value as Mode)}
              >
                <option value="add">Link</option>
                <option value="remove">Unlink</option>
                <option value="replace">Replace</option>
              </select>
            </label>

            {mode !== "replace" && (
              <label className="bulk-row">
                <span>Requirement</span>
                {loading ? (
                  <span className="muted">Loading…</span>
                ) : (
                  <SearchableSelect
                    value={target}
                    onChange={setTarget}
                    disabled={requirements.length === 0}
                    placeholder={
                      requirements.length === 0 ? "None synced" : "Select…"
                    }
                    options={requirements.map((r) => ({
                      value: r.key,
                      label: `${r.key} — ${r.summary}`,
                    }))}
                  />
                )}
              </label>
            )}

            {mode === "replace" && !loading && (
              <>
                <div className="bulk-row bulk-swap-row">
                  <span>Remove</span>
                  <ul className="bulk-swap-list">
                    {requirements.length === 0 && (
                      <li className="muted">None synced</li>
                    )}
                    {requirements.map((r) => (
                      <li key={r.key}>
                        <label>
                          <input
                            type="checkbox"
                            checked={toRemove.has(r.key)}
                            onChange={() =>
                              setToRemove((s) => togglePick(s, r.key))
                            }
                          />
                          <span>
                            {r.key} — {r.summary}
                          </span>
                        </label>
                      </li>
                    ))}
                  </ul>
                </div>
                <div className="bulk-row bulk-swap-row">
                  <span>Add</span>
                  <ul className="bulk-swap-list">
                    {requirements.length === 0 && (
                      <li className="muted">None synced</li>
                    )}
                    {requirements.map((r) => (
                      <li key={r.key}>
                        <label>
                          <input
                            type="checkbox"
                            checked={toAdd.has(r.key)}
                            onChange={() =>
                              setToAdd((s) => togglePick(s, r.key))
                            }
                          />
                          <span>
                            {r.key} — {r.summary}
                          </span>
                        </label>
                      </li>
                    ))}
                  </ul>
                </div>
              </>
            )}

            {loadError && <div className="error-text">{loadError}</div>}

            <p className="muted bulk-preview">
              {mode === "add" &&
                "The requirement is linked to tests that don't already cover it. "}
              {mode === "remove" &&
                "The requirement link is removed from tests that have it. "}
              {mode === "replace" &&
                "For each test, checked Remove links are dropped and checked Add links are created, all in one apply. "}
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
                disabled={applying || loading || !canApply}
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
    </Modal>
  );
}
