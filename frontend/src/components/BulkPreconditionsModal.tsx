import { useEffect, useState } from "react";
import {
  ListAllPreconditions,
  BulkAssociatePreconditions,
  BulkReplacePreconditions,
  errMsg,
} from "../api";
import type { Precondition, BulkEditResult } from "../api";
import { SearchableSelect } from "./SearchableSelect";

interface Props {
  profileId: string;
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

// BulkPreconditionsModal associates, disassociates, or swaps (Replace mode)
// Preconditions across the selected Tests (FR-13.6, RND_P_4TFINT_05-231).
export function BulkPreconditionsModal({
  profileId,
  testKeys,
  onComplete,
  onCancel,
}: Props) {
  const [preconditions, setPreconditions] = useState<Precondition[]>([]);
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
    ListAllPreconditions(profileId)
      .then((ps) => {
        if (cancelled) return;
        setPreconditions(ps ?? []);
        setTarget(ps && ps.length > 0 ? ps[0].key : "");
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
        r = await BulkReplacePreconditions(
          profileId,
          testKeys,
          [...toRemove],
          [...toAdd],
        );
      } else {
        if (!target) return;
        r = await BulkAssociatePreconditions(
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
    <div className="modal-overlay" onClick={onCancel}>
      <div className="modal bulk-modal" onClick={(e) => e.stopPropagation()}>
        <div className="pending-head">
          <h2>
            Preconditions ({testKeys.length}{" "}
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
                <option value="add">Associate</option>
                <option value="remove">Disassociate</option>
                <option value="replace">Replace</option>
              </select>
            </label>

            {mode !== "replace" && (
              <label className="bulk-row">
                <span>Precondition</span>
                {loading ? (
                  <span className="muted">Loading…</span>
                ) : (
                  <SearchableSelect
                    value={target}
                    onChange={setTarget}
                    disabled={preconditions.length === 0}
                    placeholder={
                      preconditions.length === 0 ? "None synced" : "Select…"
                    }
                    options={preconditions.map((p) => ({
                      value: p.key,
                      label: `${p.key} — ${p.summary}`,
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
                    {preconditions.length === 0 && (
                      <li className="muted">None synced</li>
                    )}
                    {preconditions.map((p) => (
                      <li key={p.key}>
                        <label>
                          <input
                            type="checkbox"
                            checked={toRemove.has(p.key)}
                            onChange={() =>
                              setToRemove((s) => togglePick(s, p.key))
                            }
                          />
                          <span>
                            {p.key} — {p.summary}
                          </span>
                        </label>
                      </li>
                    ))}
                  </ul>
                </div>
                <div className="bulk-row bulk-swap-row">
                  <span>Add</span>
                  <ul className="bulk-swap-list">
                    {preconditions.length === 0 && (
                      <li className="muted">None synced</li>
                    )}
                    {preconditions.map((p) => (
                      <li key={p.key}>
                        <label>
                          <input
                            type="checkbox"
                            checked={toAdd.has(p.key)}
                            onChange={() =>
                              setToAdd((s) => togglePick(s, p.key))
                            }
                          />
                          <span>
                            {p.key} — {p.summary}
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
                "The precondition is added to tests that don't already have it. "}
              {mode === "remove" &&
                "The precondition is removed from tests that have it. "}
              {mode === "replace" &&
                "For each test, checked Remove items are dropped and checked Add items are linked, all in one apply. "}
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
      </div>
    </div>
  );
}
