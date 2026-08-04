import { useEffect, useState } from "react";
import { ListTests, SearchTestsCrossProject, errMsg } from "../api";

interface Props {
  profileId: string;
  heading: string;
  // Test key to exclude from the list (e.g. the test being edited).
  excludeKey?: string;
  // When true, the modal only searches the profile's configured source projects
  // (cross-project), with no same-project toggle (RND_P_4TFINT_05-322).
  crossProjectOnly?: boolean;
  onPick: (key: string) => void | Promise<void>;
  onCancel: () => void;
}

const PAGE_SIZE = 50;

// A normalized picker row: same-project results carry no projectKey; cross-
// project results (RND_P_4TFINT_05-322) carry the owning project's key.
interface PickRow {
  key: string;
  summary: string;
  projectKey?: string;
}

// PickTestModal is a single-select test search: type to filter, click a test to
// choose it. Used to pick the target of a "call test" step (#2) and the source
// of cloned steps. A toggle widens the search to tests in OTHER projects
// (RND_P_4TFINT_05-322).
export function PickTestModal({
  profileId,
  heading,
  excludeKey,
  crossProjectOnly,
  onPick,
  onCancel,
}: Props) {
  const [search, setSearch] = useState("");
  const [results, setResults] = useState<PickRow[]>([]);
  const [page, setPage] = useState(0);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // When on, search tests in the profile's configured source projects instead
  // of the profile's own cached tests. Forced (and toggle hidden) when the
  // caller opened this as a cross-project picker.
  const [crossProject, setCrossProject] = useState(!!crossProjectOnly);

  useEffect(() => {
    setPage(0);
  }, [search, crossProject]);

  useEffect(() => {
    let cancelled = false;
    const handle = setTimeout(() => {
      setLoading(true);
      setError("");
      const req = crossProject
        ? SearchTestsCrossProject(profileId, search, page * PAGE_SIZE).then((p) => ({
            tests: (p.tests ?? []).map((r) => ({
              key: r.key,
              summary: r.summary,
              projectKey: r.projectKey,
            })),
            total: p.total ?? 0,
          }))
        : ListTests(profileId, {
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
          }).then((p) => ({
            tests: (p.tests ?? []).map((t) => ({ key: t.key, summary: t.summary })),
            total: p.total ?? 0,
          }));
      req
        .then((p) => {
          if (cancelled) return;
          setResults(p.tests);
          setTotal(p.total);
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
  }, [profileId, search, page, crossProject]);

  async function pick(key: string) {
    setBusy(true);
    setError("");
    try {
      await onPick(key);
    } catch (e) {
      setError(errMsg(e));
      setBusy(false);
    }
  }

  const visible = results.filter((t) => t.key !== excludeKey);

  return (
    <div className="modal-overlay" onClick={onCancel}>
      <div className="modal pending-modal" onClick={(e) => e.stopPropagation()}>
        <div className="pending-head">
          <h2>{heading}</h2>
          <button className="btn btn-ghost" onClick={onCancel} title="Close">
            ✕
          </button>
        </div>
        <div className="add-tests-body">
          <div className="add-tests-main">
            <input
              className="detail-input"
              autoFocus
              placeholder={
                crossProject
                  ? "Search other projects by key or summary…"
                  : "Search key, summary, description…"
              }
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              disabled={busy}
            />
            {!crossProjectOnly && (
              <label className="pick-crossproj">
                <input
                  type="checkbox"
                  checked={crossProject}
                  onChange={(e) => setCrossProject(e.target.checked)}
                  disabled={busy}
                />
                Search other projects
              </label>
            )}
            {loading ? (
              <p className="muted">Searching…</p>
            ) : (
              <ul className="add-test-list">
                {visible.map((t) => (
                  <li key={t.key}>
                    <button
                      className="link-btn clone-src-pick"
                      onClick={() => pick(t.key)}
                      disabled={busy}
                    >
                      <span className="mono">{t.key}</span> {t.summary}
                      {t.projectKey && (
                        <span className="pick-proj-badge">{t.projectKey}</span>
                      )}
                    </button>
                  </li>
                ))}
                {visible.length === 0 && (
                  <li className="muted">
                    {loading ? "" : "No tests match."}
                  </li>
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
      </div>
    </div>
  );
}
