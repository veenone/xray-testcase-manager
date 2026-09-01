import { useEffect, useState } from "react";
import { useProfile } from "../contexts/ProfileContext";
import { SearchPreconditionsCrossProject, errMsg } from "../api";
import type { Precondition } from "../api";
import { ProjectSidebar } from "./ProjectSidebar";
import { Pager } from "./Pager";
import { Modal } from "./Modal";

interface Props {
  // Keys already linked, shown disabled.
  excludeKeys?: string[];
  // Configured source project keys, for the left project filter sidebar.
  sourceProjects?: string[];
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
  excludeKeys,
  sourceProjects,
  onPick,
  onCancel,
}: Props) {
  const { activeId: profileId } = useProfile();
  const [search, setSearch] = useState("");
  const [results, setResults] = useState<Precondition[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState(PAGE_SIZE);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [project, setProject] = useState("");
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const exclude = new Set(excludeKeys ?? []);
  const projects = sourceProjects ?? [];
  // Show the owning-project badge only when browsing all projects (#322).
  const showProjectBadge = project === "";

  useEffect(() => {
    setPage(0);
  }, [search, project]);

  useEffect(() => {
    let cancelled = false;
    const handle = setTimeout(() => {
      setLoading(true);
      setError("");
      SearchPreconditionsCrossProject(profileId, search, project, page * pageSize, pageSize)
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
  }, [profileId, search, project, page, pageSize]);

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
    <Modal
      onClose={onCancel}
      className="modal pending-modal"
      labelledBy="pick-precondition-title"
    >
        <div className="pending-head">
          <h2 id="pick-precondition-title">
            Link preconditions from another project
          </h2>
          <button className="btn btn-ghost" onClick={onCancel} title="Close">
            ✕
          </button>
        </div>
        <div className="add-tests-body">
          {projects.length > 0 && (
            <ProjectSidebar
              projects={projects}
              selected={project}
              onSelect={setProject}
            />
          )}
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
              <div className="pick-table-wrap">
                <table className="pick-table">
                  <tbody>
                    {results.map((p) => {
                      const already = exclude.has(p.key);
                      return (
                        <tr
                          key={p.key}
                          className={already ? "pick-row-disabled" : "pick-row"}
                          onClick={() => {
                            if (!already && !busy) toggle(p.key);
                          }}
                        >
                          <td className="pick-check">
                            <input
                              type="checkbox"
                              disabled={already || busy}
                              checked={already || picked.has(p.key)}
                              onChange={() => toggle(p.key)}
                              onClick={(e) => e.stopPropagation()}
                            />
                          </td>
                          <td className="pick-key mono">{p.key}</td>
                          <td className="pick-summary">
                            {p.summary}
                            {showProjectBadge && (
                              <span className="pick-proj-badge">
                                {p.key.split("-")[0]}
                              </span>
                            )}
                            {already && (
                              <span className="muted"> · already linked</span>
                            )}
                          </td>
                        </tr>
                      );
                    })}
                    {results.length === 0 && (
                      <tr>
                        <td className="pick-empty" colSpan={3}>
                          No preconditions match.
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            )}
            {total > 0 && (
              <Pager
                page={page}
                pageSize={pageSize}
                total={total}
                onPage={setPage}
                onPageSize={(n) => {
                  setPageSize(n);
                  setPage(0);
                }}
              />
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
    </Modal>
  );
}
