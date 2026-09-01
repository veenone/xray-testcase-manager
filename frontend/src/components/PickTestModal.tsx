import { useEffect, useState } from "react";
import { useProfile } from "../contexts/ProfileContext";
import { ListTests, SearchTestsCrossProject, errMsg } from "../api";
import { ProjectSidebar } from "./ProjectSidebar";
import { Pager } from "./Pager";
import { Modal } from "./Modal";

interface Props {
  heading: string;
  // Test key to exclude from the list (e.g. the test being edited).
  excludeKey?: string;
  // When true, the modal only searches the profile's configured source projects
  // (cross-project), with no same-project toggle (RND_P_4TFINT_05-322).
  crossProjectOnly?: boolean;
  // Configured source project keys, for the left project filter sidebar (only
  // shown in cross-project mode).
  sourceProjects?: string[];
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
  heading,
  excludeKey,
  crossProjectOnly,
  sourceProjects,
  onPick,
  onCancel,
}: Props) {
  const { activeId: profileId } = useProfile();
  const [search, setSearch] = useState("");
  const [results, setResults] = useState<PickRow[]>([]);
  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState(PAGE_SIZE);
  const [total, setTotal] = useState(0);
  const [project, setProject] = useState("");
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // When on, search tests in the profile's configured source projects instead
  // of the profile's own cached tests. Forced (and toggle hidden) when the
  // caller opened this as a cross-project picker.
  const [crossProject, setCrossProject] = useState(!!crossProjectOnly);
  const projects = sourceProjects ?? [];
  // Show the owning-project badge only when browsing all projects; when a single
  // project is selected in the sidebar it is redundant (#322).
  const showProjectBadge = crossProject && project === "";

  useEffect(() => {
    setPage(0);
  }, [search, crossProject, project]);

  useEffect(() => {
    let cancelled = false;
    const handle = setTimeout(() => {
      setLoading(true);
      setError("");
      const req = crossProject
        ? SearchTestsCrossProject(profileId, search, project, page * pageSize, pageSize).then((p) => ({
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
            limit: pageSize,
            offset: page * pageSize,
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
  }, [profileId, search, page, pageSize, crossProject, project]);

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
    <Modal
      onClose={onCancel}
      className="modal pending-modal"
      labelledBy="pick-test-title"
    >
        <div className="pending-head">
          <h2 id="pick-test-title">{heading}</h2>
          <button className="btn btn-ghost" onClick={onCancel} title="Close">
            ✕
          </button>
        </div>
        <div className="add-tests-body">
          {crossProject && projects.length > 0 && (
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
              <div className="pick-table-wrap">
                <table className="pick-table">
                  <tbody>
                    {visible.map((t) => (
                      <tr
                        key={t.key}
                        className="pick-row"
                        onClick={() => {
                          if (!busy) pick(t.key);
                        }}
                      >
                        <td className="pick-key mono">{t.key}</td>
                        <td className="pick-summary">
                          {t.summary}
                          {showProjectBadge && t.projectKey && (
                            <span className="pick-proj-badge">
                              {t.projectKey}
                            </span>
                          )}
                        </td>
                      </tr>
                    ))}
                    {visible.length === 0 && (
                      <tr>
                        <td className="pick-empty" colSpan={2}>
                          No tests match.
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
    </Modal>
  );
}
