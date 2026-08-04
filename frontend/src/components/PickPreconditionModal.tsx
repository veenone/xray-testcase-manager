import { useEffect, useState } from "react";
import { SearchPreconditionsCrossProject, errMsg } from "../api";
import type { Precondition } from "../api";

interface Props {
  profileId: string;
  // Keys already linked, shown disabled.
  excludeKeys?: string[];
  // Called with the chosen preconditions (in full, so the caller can cache
  // them for display). May return a promise; errors surface without closing.
  onPick: (preconditions: Precondition[]) => void | Promise<void>;
  onCancel: () => void;
}

const PAGE_SIZE = 50;

// PickPreconditionModal browses and searches Preconditions in the profile's
// configured source projects, so a test can link shared preconditions across
// projects (RND_P_4TFINT_05-322). It mirrors the requirements "Add tests"
// modal: a browsable, paginated, multi-select checkbox list. The chosen
// preconditions are returned in full so the caller can cache them for display.
export function PickPreconditionModal({
  profileId,
  excludeKeys,
  onPick,
  onCancel,
}: Props) {
  const [search, setSearch] = useState("");
  const [results, setResults] = useState<Precondition[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(0);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const exclude = new Set(excludeKeys ?? []);

  useEffect(() => {
    setPage(0);
  }, [search]);

  useEffect(() => {
    let cancelled = false;
    const handle = setTimeout(() => {
      setLoading(true);
      setError("");
      SearchPreconditionsCrossProject(profileId, search, page * PAGE_SIZE)
        .then((p) => {
          if (cancelled) return;
          setResults(p.preconditions ?? []);
          setTotal(p.total ?? 0);
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
  }, [profileId, search, page]);

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
      // The picked keys may span multiple pages; the caller only needs the ones
      // currently loaded to cache, but we pass whatever we have for each key.
      const chosen = results.filter((p) => picked.has(p.key));
      await onPick(chosen);
    } catch (e) {
      setError(errMsg(e));
      setBusy(false);
    }
  }

  return (
    <div className="modal-overlay" onClick={onCancel}>
      <div className="modal pending-modal" onClick={(e) => e.stopPropagation()}>
        <div className="pending-head">
          <h2>Link preconditions from another project</h2>
          <button className="btn btn-ghost" onClick={onCancel} title="Close">
            ✕
          </button>
        </div>
        <div className="add-tests-body">
          <div className="add-tests-main">
            <input
              className="detail-input"
              autoFocus
              placeholder="Search other projects by key or summary…"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              disabled={busy}
            />
            {loading ? (
              <p className="muted">Loading…</p>
            ) : (
              <ul className="add-test-list">
                {results.map((p) => {
                  const already = exclude.has(p.key);
                  return (
                    <li key={p.key} className={already ? "add-test-already" : ""}>
                      <label>
                        <input
                          type="checkbox"
                          disabled={already || busy}
                          checked={already || picked.has(p.key)}
                          onChange={() => toggle(p.key)}
                        />
                        <span className="mono">{p.key}</span> {p.summary}
                        <span className="pick-proj-badge">
                          {p.key.split("-")[0]}
                        </span>
                        {already && (
                          <span className="muted"> · already linked</span>
                        )}
                      </label>
                    </li>
                  );
                })}
                {results.length === 0 && (
                  <li className="muted">No preconditions match.</li>
                )}
              </ul>
            )}
            {total > PAGE_SIZE && (
              <div className="add-test-pager">
                <button
                  className="btn"
                  disabled={page === 0 || loading || busy}
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
                  disabled={(page + 1) * PAGE_SIZE >= total || loading || busy}
                  onClick={() => setPage((p) => p + 1)}
                >
                  Next ›
                </button>
              </div>
            )}
            {error && <div className="error-text">{error}</div>}
          </div>
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
            {busy
              ? "Linking…"
              : `Link ${picked.size} precondition${picked.size === 1 ? "" : "s"}`}
          </button>
        </div>
      </div>
    </div>
  );
}
