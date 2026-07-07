import { useEffect, useMemo, useState } from "react";
import { ListTests, errMsg } from "../api";
import type { TestCase } from "../api";

interface Props {
  profileId: string;
  // Keys already selected or mapped — shown disabled in the list.
  excludeKeys: string[];
  onClose: () => void;
  onAdd: (keys: string[]) => void;
}

const PAGE_SIZE = 50;

// BrowseTestsPicker opens a modal that lists the active profile's tests so the
// user can pick tests visually and add them to the Coverage "Other test keys"
// field without typing keys by hand.
export function BrowseTestsPicker({ profileId, excludeKeys, onClose, onAdd }: Props) {
  const [search, setSearch] = useState("");
  const [results, setResults] = useState<TestCase[]>([]);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [page, setPage] = useState(0);
  const [total, setTotal] = useState(0);

  const excluded = useMemo(() => new Set(excludeKeys), [excludeKeys]);

  // Reset to page 0 whenever the search term changes.
  useEffect(() => {
    setPage(0);
  }, [search]);

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
        execType: "",
        review: "",
        sortBy: "key",
        desc: false,
        limit: PAGE_SIZE,
        offset: page * PAGE_SIZE,
      })
        .then((p) => {
          if (cancelled) return;
          setResults(p.tests ?? []);
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

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [onClose]);

  function toggle(key: string) {
    setPicked((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }

  function handleAdd() {
    if (picked.size === 0) return;
    onAdd([...picked]);
    onClose();
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal cov-picker-modal" onClick={(e) => e.stopPropagation()}>
        <div className="pending-head">
          <h2>Browse tests</h2>
          <button className="btn btn-ghost" onClick={onClose} title="Close">
            ✕
          </button>
        </div>

        <div className="cov-picker-body">
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
                const already = excluded.has(t.key);
                return (
                  <li key={t.key} className={already ? "add-test-already" : ""}>
                    <label>
                      <input
                        type="checkbox"
                        disabled={already}
                        checked={already || picked.has(t.key)}
                        onChange={() => toggle(t.key)}
                      />
                      <span className="mono">{t.key}</span> {t.summary}
                      {already && <span className="muted"> · already mapped</span>}
                    </label>
                  </li>
                );
              })}
              {results.length === 0 && <li className="muted">No tests match.</li>}
            </ul>
          )}
          {total > PAGE_SIZE && (
            <div className="add-test-pager">
              <button
                className="btn"
                disabled={page === 0 || loading}
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
                disabled={(page + 1) * PAGE_SIZE >= total || loading}
                onClick={() => setPage((p) => p + 1)}
              >
                Next ›
              </button>
            </div>
          )}
          {error && <div className="error-text">{error}</div>}
        </div>

        <div className="pending-actions">
          <button className="btn" onClick={onClose}>
            Cancel
          </button>
          <button
            className="btn btn-primary"
            onClick={handleAdd}
            disabled={picked.size === 0}
          >
            Add {picked.size} test{picked.size === 1 ? "" : "s"}
          </button>
        </div>
      </div>
    </div>
  );
}
