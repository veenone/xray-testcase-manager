import { useEffect, useState } from "react";
import { ListTests, AllocateTests, errMsg } from "../api";
import type { TestCase } from "../api";

interface Props {
  profileId: string;
  containerKey: string;
  existingKeys: string[];
  onDone: () => void;
  onCancel: () => void;
  // When provided, picked test keys are passed to onAdd instead of being
  // allocated to a container — lets the Preconditions view reuse this picker to
  // link tests to a Precondition. targetLabel customises the heading.
  onAdd?: (keys: string[]) => Promise<void>;
  targetLabel?: string;
}

// AddTestsModal searches the test cache and adds the chosen tests to a
// container (Test Set / Plan / Execution) — the container-side counterpart to
// allocating from the browse grid. Already-member tests are shown disabled.
export function AddTestsModal({
  profileId,
  containerKey,
  existingKeys,
  onDone,
  onCancel,
  onAdd,
  targetLabel,
}: Props) {
  const [search, setSearch] = useState("");
  const [results, setResults] = useState<TestCase[]>([]);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const existing = new Set(existingKeys);

  useEffect(() => {
    let cancelled = false;
    const handle = setTimeout(() => {
      setLoading(true);
      ListTests(profileId, {
        search,
        status: "",
        folderId: "",
        containerKey: "",
        component: "",
        review: "",
        sortBy: "key",
        desc: false,
        limit: 50,
        offset: 0,
      })
        .then((p) => {
          if (!cancelled) setResults(p.tests ?? []);
        })
        .catch((e) => {
          if (!cancelled) setError(errMsg(e));
        })
        .finally(() => {
          if (!cancelled) setLoading(false);
        });
    }, 250);
    return () => {
      cancelled = true;
      clearTimeout(handle);
    };
  }, [profileId, search]);

  function toggle(key: string) {
    setPicked((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }

  async function add() {
    if (picked.size === 0) return;
    setBusy(true);
    setError("");
    try {
      if (onAdd) await onAdd([...picked]);
      else await AllocateTests(profileId, containerKey, [...picked]);
      onDone();
    } catch (e) {
      setError(errMsg(e));
      setBusy(false);
    }
  }

  return (
    <div className="modal-overlay" onClick={onCancel}>
      <div className="modal pending-modal" onClick={(e) => e.stopPropagation()}>
        <div className="pending-head">
          <h2>Add tests to {targetLabel ?? containerKey}</h2>
          <button className="btn btn-ghost" onClick={onCancel} title="Close">
            ✕
          </button>
        </div>

        <div className="bulk-body">
          <input
            className="detail-input"
            autoFocus
            placeholder="Search key, summary, description…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          {loading ? (
            <p className="muted">Searching…</p>
          ) : (
            <ul className="add-test-list">
              {results.map((t) => {
                const already = existing.has(t.key);
                return (
                  <li
                    key={t.key}
                    className={already ? "add-test-already" : ""}
                  >
                    <label>
                      <input
                        type="checkbox"
                        disabled={already}
                        checked={already || picked.has(t.key)}
                        onChange={() => toggle(t.key)}
                      />
                      <span className="mono">{t.key}</span> {t.summary}
                      {already && (
                        <span className="muted"> · already a member</span>
                      )}
                    </label>
                  </li>
                );
              })}
              {results.length === 0 && (
                <li className="muted">No tests match.</li>
              )}
            </ul>
          )}
          {error && <div className="error-text">{error}</div>}
        </div>

        <div className="pending-actions">
          <button className="btn" onClick={onCancel} disabled={busy}>
            Cancel
          </button>
          <button
            className="btn btn-primary"
            onClick={add}
            disabled={busy || picked.size === 0}
          >
            {busy ? "Adding…" : `Add ${picked.size} test${picked.size === 1 ? "" : "s"}`}
          </button>
        </div>
      </div>
    </div>
  );
}
