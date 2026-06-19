import { useEffect, useState } from "react";
import { ListTests, GetTestSteps, errMsg } from "../api";
import type { TestCase, Step, StepDraft } from "../api";

interface Props {
  profileId: string;
  // Shown in the heading, e.g. "DEMO-1" or "the new test".
  targetLabel: string;
  // Test key to exclude from the source list (the test being edited), if any.
  excludeKey?: string;
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
  onConfirm,
  onCancel,
}: Props) {
  // Stage 1 — source search.
  const [search, setSearch] = useState("");
  const [results, setResults] = useState<TestCase[]>([]);
  const [page, setPage] = useState(0);
  const [total, setTotal] = useState(0);
  const [searching, setSearching] = useState(false);

  // Stage 2 — step selection for the chosen source.
  const [source, setSource] = useState<TestCase | null>(null);
  const [sourceSteps, setSourceSteps] = useState<Step[]>([]);
  const [stepsLoading, setStepsLoading] = useState(false);
  const [picked, setPicked] = useState<Set<string>>(new Set());

  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    setPage(0);
  }, [search]);

  useEffect(() => {
    if (source) return; // pause searching while choosing steps
    let cancelled = false;
    const handle = setTimeout(() => {
      setSearching(true);
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
          if (!cancelled) setSearching(false);
        });
    }, 250);
    return () => {
      cancelled = true;
      clearTimeout(handle);
    };
  }, [profileId, search, page, source]);

  async function chooseSource(t: TestCase) {
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
    <div className="modal-overlay" onClick={onCancel}>
      <div className="modal pending-modal" onClick={(e) => e.stopPropagation()}>
        <div className="pending-head">
          <h2>Clone steps into {targetLabel}</h2>
          <button className="btn btn-ghost" onClick={onCancel} title="Close">
            ✕
          </button>
        </div>

        <div className="add-tests-body">
          <div className="add-tests-main">
            {!source ? (
              <>
                <p className="muted detail-note">
                  Pick a test to copy steps from. You choose which steps next.
                </p>
                <input
                  className="detail-input"
                  autoFocus
                  placeholder="Search key, summary, description…"
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                />
                {searching ? (
                  <p className="muted">Searching…</p>
                ) : (
                  <ul className="add-test-list">
                    {visibleResults.map((t) => (
                      <li key={t.key}>
                        <button
                          className="link-btn clone-src-pick"
                          onClick={() => chooseSource(t)}
                        >
                          <span className="mono">{t.key}</span> {t.summary}
                        </button>
                      </li>
                    ))}
                    {visibleResults.length === 0 && (
                      <li className="muted">No tests match.</li>
                    )}
                  </ul>
                )}
                {total > PAGE_SIZE && (
                  <div className="add-test-pager">
                    <button
                      className="btn"
                      disabled={page === 0 || searching}
                      onClick={() => setPage((p) => Math.max(0, p - 1))}
                    >
                      ‹ Prev
                    </button>
                    <span className="muted">
                      {(page * PAGE_SIZE + 1).toLocaleString()}–
                      {Math.min((page + 1) * PAGE_SIZE, total).toLocaleString()}{" "}
                      of {total.toLocaleString()}
                    </span>
                    <button
                      className="btn"
                      disabled={(page + 1) * PAGE_SIZE >= total || searching}
                      onClick={() => setPage((p) => p + 1)}
                    >
                      Next ›
                    </button>
                  </div>
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
      </div>
    </div>
  );
}
