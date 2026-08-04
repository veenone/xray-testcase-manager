import { useEffect, useState } from "react";
import { SearchPreconditionsCrossProject, errMsg } from "../api";
import type { Precondition } from "../api";

interface Props {
  profileId: string;
  // Keys already linked, hidden from the results.
  excludeKeys?: string[];
  onPick: (precondition: Precondition) => void | Promise<void>;
  onCancel: () => void;
}

// PickPreconditionModal searches Preconditions in OTHER projects (live Jira) so
// a test can link a shared precondition across projects (RND_P_4TFINT_05-322).
// The chosen precondition is returned in full so the caller can cache it for
// display.
export function PickPreconditionModal({
  profileId,
  excludeKeys,
  onPick,
  onCancel,
}: Props) {
  const [search, setSearch] = useState("");
  const [results, setResults] = useState<Precondition[]>([]);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    const handle = setTimeout(() => {
      if (!search.trim()) {
        setResults([]);
        return;
      }
      setLoading(true);
      setError("");
      SearchPreconditionsCrossProject(profileId, search)
        .then((rows) => {
          if (!cancelled) setResults(rows ?? []);
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

  async function pick(pc: Precondition) {
    setBusy(true);
    setError("");
    try {
      await onPick(pc);
    } catch (e) {
      setError(errMsg(e));
      setBusy(false);
    }
  }

  const exclude = new Set(excludeKeys ?? []);
  const visible = results.filter((p) => !exclude.has(p.key));

  return (
    <div className="modal-overlay" onClick={onCancel}>
      <div className="modal pending-modal" onClick={(e) => e.stopPropagation()}>
        <div className="pending-head">
          <h2>Link precondition from another project</h2>
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
              <p className="muted">Searching…</p>
            ) : (
              <ul className="add-test-list">
                {visible.map((p) => (
                  <li key={p.key}>
                    <button
                      className="link-btn clone-src-pick"
                      onClick={() => pick(p)}
                      disabled={busy}
                    >
                      <span className="mono">{p.key}</span> {p.summary}
                      <span className="pick-proj-badge">
                        {p.key.split("-")[0]}
                      </span>
                    </button>
                  </li>
                ))}
                {visible.length === 0 && (
                  <li className="muted">
                    {search.trim()
                      ? "No preconditions match."
                      : "Type to search other projects."}
                  </li>
                )}
              </ul>
            )}
            {error && <div className="error-text">{error}</div>}
          </div>
        </div>
      </div>
    </div>
  );
}
