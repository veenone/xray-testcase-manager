import { useEffect, useState } from "react";
import { ListTests, SearchTestsCrossProject, GetTestSteps, errMsg } from "../api";
import type { Step, StepDraft } from "../api";
import { ProjectSidebar } from "./ProjectSidebar";
import { Pager } from "./Pager";
import { Modal } from "./Modal";

// A normalized source row: same-project rows carry no projectKey; cross-project
// rows (RND_P_4TFINT_05-322) carry the owning project's key.
interface SourceRow {
  key: string;
  summary: string;
  projectKey?: string;
}

interface Props {
  profileId: string;
  // Shown in the heading, e.g. "DEMO-1" or "the new test".
  targetLabel: string;
  // Test key to exclude from the source list (the test being edited), if any.
  excludeKey?: string;
  // When true, only search the profile's configured source projects (cross-
  // project), with no same-project toggle (RND_P_4TFINT_05-322).
  crossProjectOnly?: boolean;
  // Configured source project keys, for the left project filter sidebar.
  sourceProjects?: string[];
  // Called with the chosen source and the selected steps. stepIds are the
  // source step xray_ids (for a backend clone); steps carries their content
  // (for callers building a local draft). May return a promise; the modal shows
  // a busy state and surfaces a thrown error without closing.
  onConfirm: (
    sourceKey: string,
    stepIds: string[],
    steps: StepDraft[],
  ) => void | Promise<void>;
  onCancel: () => void;
}

const PAGE_SIZE = 50;

// CloneStepsModal copies steps from one test onto another. It works in two
// stages: pick a source test, then pick which of its steps to copy (all by
// default, or a selective subset). Used by the detail panel (clone onto the
// open test) and the New Test panel (seed the draft).
export function CloneStepsModal({
  profileId,
  targetLabel,
  excludeKey,
  crossProjectOnly,
  sourceProjects,
  onConfirm,
  onCancel,
}: Props) {
  // Stage 1 — source search.
  const [search, setSearch] = useState("");
  const [results, setResults] = useState<SourceRow[]>([]);
  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState(PAGE_SIZE);
  const [total, setTotal] = useState(0);
  const [searching, setSearching] = useState(false);
  // When on, search source tests in the profile's configured source projects.
  // Forced (and toggle hidden) when opened as a cross-project picker.
  const [crossProject, setCrossProject] = useState(!!crossProjectOnly);
  const [project, setProject] = useState("");
  const projects = sourceProjects ?? [];
  // Show the owning-project badge only when browsing all projects (#322).
  const showProjectBadge = crossProject && project === "";

  // Stage 2 — step selection for the chosen source.
  const [source, setSource] = useState<SourceRow | null>(null);
  const [sourceSteps, setSourceSteps] = useState<Step[]>([]);
  const [stepsLoading, setStepsLoading] = useState(false);
  const [picked, setPicked] = useState<Set<string>>(new Set());

  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    setPage(0);
  }, [search, crossProject, project]);

  useEffect(() => {
    if (source) return; // pause searching while choosing steps
    let cancelled = false;
    const handle = setTimeout(() => {
      setSearching(true);
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
          if (!cancelled) setSearching(false);
        });
    }, 250);
    return () => {
      cancelled = true;
      clearTimeout(handle);
    };
  }, [profileId, search, page, pageSize, source, crossProject, project]);

  async function chooseSource(t: SourceRow) {
    setError("");
    setStepsLoading(true);
    setSource(t);
    try {
      const s = await GetTestSteps(profileId, t.key, false);
      const list = s ?? [];
      setSourceSteps(list);
      setPicked(new Set(list.map((x) => x.xrayId))); // default: all selected
      if (list.length === 0) setError(`${t.key} has no steps to clone.`);
    } catch (e) {
      setError(errMsg(e));
      setSource(null);
    } finally {
      setStepsLoading(false);
    }
  }

  function toggle(id: string) {
    setPicked((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  async function confirm() {
    if (!source || picked.size === 0) return;
    setBusy(true);
    setError("");
    try {
      const chosen = sourceSteps.filter((s) => picked.has(s.xrayId));
      await onConfirm(
        source.key,
        chosen.map((s) => s.xrayId),
        chosen.map(({ action, data, expected }) => ({ action, data, expected })),
      );
      // On success the parent unmounts this modal; don't touch state here.
    } catch (e) {
      setError(errMsg(e));
      setBusy(false);
    }
  }

  const visibleResults = results.filter((t) => t.key !== excludeKey);

  return (
    <Modal onClose={onCancel} className="modal pending-modal" labelledBy="clone-steps-title">
        <div className="pending-head">
          <h2 id="clone-steps-title">Clone steps into {targetLabel}</h2>
          <button className="btn btn-ghost" onClick={onCancel} title="Close">
            ✕
          </button>
        </div>

        <div className="add-tests-body">
          {!source && crossProject && projects.length > 0 && (
            <ProjectSidebar
              projects={projects}
              selected={project}
              onSelect={setProject}
            />
          )}
          <div className="add-tests-main">
            {!source ? (
              <>
                <p className="muted detail-note">
                  Pick a test to copy steps from. You choose which steps next.
                </p>
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
                />
                {!crossProjectOnly && (
                  <label className="pick-crossproj">
                    <input
                      type="checkbox"
                      checked={crossProject}
                      onChange={(e) => setCrossProject(e.target.checked)}
                    />
                    Search other projects
                  </label>
                )}
                {searching ? (
                  <p className="muted">Searching…</p>
                ) : (
                  <div className="pick-table-wrap">
                    <table className="pick-table">
                      <tbody>
                        {visibleResults.map((t) => (
                          <tr
                            key={t.key}
                            className="pick-row"
                            onClick={() => chooseSource(t)}
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
                        {visibleResults.length === 0 && (
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
              </>
            ) : (
              <>
                <div className="clone-src-head">
                  <span>
                    From <span className="mono">{source.key}</span> · choose steps
                  </span>
                  <button
                    className="link-btn"
                    onClick={() => {
                      setSource(null);
                      setSourceSteps([]);
                      setPicked(new Set());
                      setError("");
                    }}
                  >
                    Change test
                  </button>
                </div>
                {stepsLoading ? (
                  <p className="muted">Loading steps…</p>
                ) : sourceSteps.length === 0 ? (
                  <p className="muted">This test has no steps.</p>
                ) : (
                  <>
                    <div className="clone-step-tools">
                      <button
                        className="link-btn"
                        onClick={() =>
                          setPicked(new Set(sourceSteps.map((s) => s.xrayId)))
                        }
                      >
                        Select all
                      </button>
                      <button
                        className="link-btn"
                        onClick={() => setPicked(new Set())}
                      >
                        Select none
                      </button>
                      <span className="muted">{picked.size} selected</span>
                    </div>
                    <ol className="clone-step-list">
                      {sourceSteps.map((s, i) => (
                        <li key={s.xrayId}>
                          <label>
                            <input
                              type="checkbox"
                              checked={picked.has(s.xrayId)}
                              onChange={() => toggle(s.xrayId)}
                            />
                            <span className="clone-step-num">{i + 1}.</span>
                            <span className="clone-step-text">
                              {s.action || <span className="muted">(no action)</span>}
                              {s.expected && (
                                <span className="muted">
                                  {" "}
                                  → {s.expected}
                                </span>
                              )}
                            </span>
                          </label>
                        </li>
                      ))}
                    </ol>
                  </>
                )}
              </>
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
            onClick={confirm}
            disabled={busy || !source || picked.size === 0}
          >
            {busy
              ? "Cloning…"
              : `Clone ${picked.size} step${picked.size === 1 ? "" : "s"}`}
          </button>
        </div>
    </Modal>
  );
}
