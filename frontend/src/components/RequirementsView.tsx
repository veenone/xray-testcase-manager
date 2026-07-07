import { useEffect, useMemo, useState } from "react";
import { useViewState } from "../lib/viewState";
import {
  ListRequirementsWithCoverage,
  ListTestsForRequirement,
  EditRequirementField,
  DeleteRequirement,
  ExportRequirementAudit,
  SyncRequirements,
  BulkAssociateRequirements,
  GetRequirementLinks,
  errMsg,
} from "../api";
import type { RequirementCoverage, RequirementTest, ReqReqLink } from "../api";
import { RequirementSourcesModal } from "./RequirementSourcesModal";
import { AddTestsModal } from "./AddTestsModal";
import { LinkRequirementsModal } from "./LinkRequirementsModal";
import { NewRequirementPanel } from "./NewRequirementPanel";
import { ImportRequirementsModal } from "./ImportRequirementsModal";
import { TestDetail } from "./TestDetail";
import { Markdown } from "./Markdown";
import { MarkdownField } from "./MarkdownField";
import { Menu } from "./Menu";
import { Pager } from "./Pager";
import { SortControl } from "./SortControl";
import { keyCompare, cmpStr, applyDir } from "../sort";
import { useConfirm } from "./useConfirm";

interface Props {
  profileId: string;
  refreshKey: number;
  onChanged?: () => void;
}

const COVERAGE_ORDER = ["FAILED", "NOTRUN", "PASSED", "UNCOVERED"];
const COVERAGE_LABEL: Record<string, string> = {
  PASSED: "Passed",
  FAILED: "Failed",
  NOTRUN: "Not run",
  UNCOVERED: "Uncovered",
};

const COVERAGE_RANK: Record<string, number> = {
  FAILED: 0,
  NOTRUN: 1,
  PASSED: 2,
  UNCOVERED: 3,
};

function cmpReq(
  a: RequirementCoverage,
  b: RequirementCoverage,
  field: string,
): number {
  switch (field) {
    case "coverage":
      return (
        (COVERAGE_RANK[a.coverage] ?? 9) - (COVERAGE_RANK[b.coverage] ?? 9) ||
        keyCompare(a.key, b.key)
      );
    case "tests":
      return (a.testCount ?? 0) - (b.testCount ?? 0) || keyCompare(a.key, b.key);
    case "status":
      return cmpStr(a.status, b.status) || keyCompare(a.key, b.key);
    default:
      return keyCompare(a.key, b.key);
  }
}

// RequirementsView is the requirement coverage / traceability surface: a
// filterable master list of requirements (each with a derived coverage status,
// even when the requirement lives in a different project) on the left, and a
// detail pane on the right listing the Tests that cover the selected
// requirement with their run result. Read-only; recomputes when the profile
// changes or a sync/commit bumps refreshKey.
export function RequirementsView({ profileId, refreshKey, onChanged }: Props) {
  const [list, setList] = useState<RequirementCoverage[]>([]);
  const [selected, setSelected] = useViewState(profileId, "requirements", "selected", "");
  const [tests, setTests] = useState<RequirementTest[]>([]);
  const [filter, setFilter] = useViewState(profileId, "requirements", "filter", "");
  const [covFilter, setCovFilter] = useViewState(profileId, "requirements", "covFilter", "");
  const [sortField, setSortField] = useViewState(profileId, "requirements", "sortField", "key");
  const [sortDesc, setSortDesc] = useViewState(profileId, "requirements", "sortDesc", true);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [showSources, setShowSources] = useState(false);
  const [showAdd, setShowAdd] = useState(false);
  const [showCreateReq, setShowCreateReq] = useState(false);
  const [showImportReqs, setShowImportReqs] = useState(false);
  const [editing, setEditing] = useState(false);
  const [draftSummary, setDraftSummary] = useState("");
  const [draftDescription, setDraftDescription] = useState("");
  const [draftPriority, setDraftPriority] = useState("");
  const [draftComponents, setDraftComponents] = useState("");
  const [draftFixVersions, setDraftFixVersions] = useState("");
  const [draftSprint, setDraftSprint] = useState("");
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");
  const { confirm, confirmUI } = useConfirm();
  // Collapsible description in the detail pane -- collapsed by default, resets on selection change.
  const [descOpen, setDescOpen] = useState(false);
  // A covering test opened in a slide-over detail panel (#5).
  const [detailKey, setDetailKey] = useViewState(profileId, "requirements", "detailKey", "");
  const [detailVersion, setDetailVersion] = useState(0);
  const [reqLinks, setReqLinks] = useState<ReqReqLink[]>([]);
  const [showLinkReqs, setShowLinkReqs] = useState(false);

  useEffect(() => {
    if (!profileId) return;
    let cancelled = false;
    setLoading(true);
    setError("");
    ListRequirementsWithCoverage(profileId)
      .then((rs) => {
        if (cancelled) return;
        setList(rs ?? []);
        setSelected((cur) =>
          cur && (rs ?? []).some((r) => r.key === cur)
            ? cur
            : rs && rs.length > 0
              ? rs[0].key
              : "",
        );
      })
      .catch((e) => {
        if (!cancelled) setError(errMsg(e));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [profileId, refreshKey]);

  useEffect(() => {
    if (!profileId || !selected) {
      setTests([]);
      return;
    }
    let cancelled = false;
    ListTestsForRequirement(profileId, selected)
      .then((ts) => {
        if (!cancelled) setTests(ts ?? []);
      })
      .catch((e) => {
        if (!cancelled) setError(errMsg(e));
      });
    return () => {
      cancelled = true;
    };
  }, [profileId, selected, refreshKey]);

  useEffect(() => {
    setEditing(false);
    setDescOpen(false);
  }, [selected]);

  useEffect(() => {
    if (!profileId || !selected) {
      setReqLinks([]);
      return;
    }
    let cancelled = false;
    GetRequirementLinks(profileId, selected)
      .then((ls) => {
        if (!cancelled) setReqLinks(ls ?? []);
      })
      .catch(() => {
        if (!cancelled) setReqLinks([]);
      });
    return () => {
      cancelled = true;
    };
  }, [profileId, selected, refreshKey]);

  const counts = useMemo(() => {
    const c: Record<string, number> = {};
    for (const r of list) c[r.coverage] = (c[r.coverage] ?? 0) + 1;
    return c;
  }, [list]);

  const filtered = useMemo(() => {
    const f = filter.trim().toLowerCase();
    const base = list.filter(
      (r) =>
        (!covFilter || r.coverage === covFilter) &&
        (!f ||
          r.key.toLowerCase().includes(f) ||
          r.summary.toLowerCase().includes(f)),
    );
    return [...base].sort((a, b) => applyDir(cmpReq(a, b, sortField), sortDesc));
  }, [list, filter, covFilter, sortField, sortDesc]);

  // Pagination of the (filtered) requirement list.
  const [page, setPage] = useViewState(profileId, "requirements", "page", 0);
  const [pageSize, setPageSize] = useViewState(profileId, "requirements", "pageSize", 15);
  useEffect(() => {
    setPage(0);
  }, [filter, covFilter, sortField, sortDesc]);
  const totalPages = Math.max(1, Math.ceil(filtered.length / pageSize));
  const safePage = Math.min(page, totalPages - 1);
  const pageItems = filtered.slice(
    safePage * pageSize,
    (safePage + 1) * pageSize,
  );

  // Pagination of the covering-tests table in the detail pane.
  const [testsPage, setTestsPage] = useViewState(profileId, "requirements", "testsPage", 0);
  const [testsPageSize, setTestsPageSize] = useViewState(profileId, "requirements", "testsPageSize", 15);
  useEffect(() => {
    setTestsPage(0);
  }, [selected]);
  const testsTotalPages = Math.max(1, Math.ceil(tests.length / testsPageSize));
  const testsSafePage = Math.min(testsPage, testsTotalPages - 1);
  const pageTests = tests.slice(
    testsSafePage * testsPageSize,
    (testsSafePage + 1) * testsPageSize,
  );

  const sel = list.find((r) => r.key === selected) ?? null;

  const [syncing, setSyncing] = useState(false);
  async function syncRequirements() {
    setSyncing(true);
    setError("");
    setNotice("");
    try {
      await SyncRequirements(profileId);
      onChanged?.();
      setNotice("Requirements refreshed from Jira.");
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setSyncing(false);
    }
  }

  async function exportAudit() {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const path = await ExportRequirementAudit(profileId);
      if (path) setNotice(`Saved audit to ${path}`);
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }

  function startEdit() {
    if (!sel) return;
    setDraftSummary(sel.summary);
    setDraftDescription(sel.description ?? "");
    setDraftPriority(sel.priority ?? "");
    setDraftComponents(sel.components ?? "");
    setDraftFixVersions(sel.fixVersions ?? "");
    setDraftSprint(sel.sprint ?? "");
    setEditing(true);
  }

  // saveEdits persists all draft field values that differ from stored values and
  // exits edit mode. Fields unchanged from their stored value are skipped.
  async function saveEdits() {
    if (!sel) return;
    setBusy(true);
    setError("");
    try {
      const fields: Array<[string, string, string]> = [
        ["summary", draftSummary.trim(), sel.summary],
        ["description", draftDescription, sel.description ?? ""],
        ["priority", draftPriority, sel.priority ?? ""],
        ["components", draftComponents, sel.components ?? ""],
        ["fix_versions", draftFixVersions, sel.fixVersions ?? ""],
        ["sprint", draftSprint, sel.sprint ?? ""],
      ];
      for (const [field, newVal, oldVal] of fields) {
        if (newVal !== oldVal) {
          await EditRequirementField(profileId, sel.key, field, newVal);
        }
      }
      setEditing(false);
      onChanged?.();
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }

  function cancelEdit() {
    setEditing(false);
  }

  async function deleteReq() {
    if (!sel) return;
    if (
      !(await confirm({
        title: "Delete requirement",
        message: `Delete requirement ${sel.key}? This removes it and its ${sel.testCount} coverage link${
          sel.testCount === 1 ? "" : "s"
        }, and queues the issue for deletion in Jira on the next commit.`,
        confirmLabel: "Delete",
        danger: true,
      }))
    )
      return;
    setBusy(true);
    setError("");
    try {
      await DeleteRequirement(profileId, sel.key);
      setSelected("");
      onChanged?.();
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    // #2: reqs-root is a flex row; NewRequirementPanel docks on the left,
    // and the reqs grid fills the remaining space on the right.
    <div className="reqs-root">
    {showCreateReq && (
      <NewRequirementPanel
        profileId={profileId}
        onCreated={(tempKey) => {
          setShowCreateReq(false);
          setSelected(tempKey);
          onChanged?.();
        }}
        onCancel={() => setShowCreateReq(false)}
      />
    )}
    <div className={`reqs${detailKey ? " reqs-with-detail" : ""}`}>
      <div className="reqs-list">
        <div className="reqs-list-head">
          {/* Primary row: filter (fills space) + Sync, Sources, Create — mirrors .precond-list-head */}
          <div className="reqs-head-row">
            <input
              className="precond-search"
              placeholder="Filter by key or summary…"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
            />
            <button
              className="btn"
              onClick={syncRequirements}
              disabled={syncing}
              title="Refresh just the requirements from Jira (partial sync)"
            >
              {syncing ? "Syncing…" : "Sync"}
            </button>
            <button
              className="btn reqs-sources-btn"
              onClick={() => setShowSources(true)}
              title="Configure which projects requirements are pulled from"
            >
              Sources…
            </button>
            <button
              className="btn btn-primary"
              onClick={() => setShowCreateReq(true)}
              title="Create a new requirement locally (pushed to Jira on commit)"
            >
              + Create
            </button>
          </div>
          {/* Secondary row: audit + import actions */}
          <div className="reqs-list-toolbar">
            <button
              className="btn"
              onClick={exportAudit}
              disabled={busy || list.length === 0}
              title="Export the coverage / sign-off audit (CSV or XLSX)"
            >
              Export audit…
            </button>
            <button
              className="btn"
              onClick={() => setShowImportReqs(true)}
              title="Import requirements from a CSV or XLSX file"
            >
              Import…
            </button>
          </div>
          {notice && <p className="reqs-notice muted">{notice}</p>}
        </div>
        <div className="reqs-coverage-summary">
          {COVERAGE_ORDER.map((c) => (
            <button
              key={c}
              className={`cov-pill cov-${c.toLowerCase()}${covFilter === c ? " cov-pill-active" : ""}`}
              onClick={() => setCovFilter(covFilter === c ? "" : c)}
              title={`Filter to ${COVERAGE_LABEL[c]}`}
            >
              {COVERAGE_LABEL[c]} {counts[c] ?? 0}
            </button>
          ))}
        </div>
        <SortControl
          fields={[
            { value: "key", label: "Key" },
            { value: "coverage", label: "Coverage" },
            { value: "tests", label: "Tests" },
            { value: "status", label: "Status" },
          ]}
          field={sortField}
          desc={sortDesc}
          onChange={(f, d) => {
            setSortField(f);
            setSortDesc(d);
          }}
        />
        {loading ? (
          <p className="muted reqs-empty">Loading…</p>
        ) : filtered.length === 0 ? (
          <p className="muted reqs-empty">
            {list.length === 0
              ? "No requirements cached. Add a requirement source and sync, or sync a demo profile."
              : "No requirements match the filter."}
          </p>
        ) : (
          <>
            <ul className="reqs-items">
              {pageItems.map((r) => (
                <li
                  key={r.key}
                  className={`reqs-item${r.key === selected ? " reqs-item-selected" : ""}`}
                  onClick={() => setSelected(r.key)}
                >
                  <div className="reqs-item-top">
                    <span className="mono reqs-key">{r.key}</span>
                    {r.issueType && (
                      <span
                        className={`kind-badge req-kind req-kind-${r.issueType.toLowerCase()}`}
                      >
                        {r.issueType}
                      </span>
                    )}
                    <span
                      className={`cov-badge cov-${r.coverage.toLowerCase()}`}
                    >
                      {COVERAGE_LABEL[r.coverage]}
                    </span>
                  </div>
                  <div className="reqs-item-summary">
                    {r.summary || "(no summary)"}
                  </div>
                  <div className="reqs-item-meta muted">
                    {r.testCount} test{r.testCount === 1 ? "" : "s"}
                  </div>
                </li>
              ))}
            </ul>
            {filtered.length > 0 && (
              <Pager
                compact
                page={safePage}
                pageSize={pageSize}
                total={filtered.length}
                onPage={setPage}
                onPageSize={(n) => {
                  setPageSize(n);
                  setPage(0);
                }}
              />
            )}
          </>
        )}
      </div>

      <div className="reqs-detail">
        {error && <div className="error-text">{error}</div>}
        {!sel ? (
          <p className="muted">Select a requirement to see its coverage.</p>
        ) : (
          <>
            <div className="reqs-detail-head">
              <span className="kind-badge req-kind">{sel.issueType}</span>
              <span className="mono reqs-detail-key">{sel.key}</span>
              <span className="muted reqs-detail-project">{sel.projectKey}</span>
              {sel.status && <span className="status-pill">{sel.status}</span>}
              <span
                className={`cov-badge cov-${sel.coverage.toLowerCase()} reqs-detail-cov`}
              >
                {COVERAGE_LABEL[sel.coverage]}
              </span>
              <div className="precond-detail-actions">
                {editing ? (
                  <>
                    <button
                      className="btn"
                      onClick={cancelEdit}
                      disabled={busy}
                      title="Discard edits and return to read-only view"
                    >
                      Cancel
                    </button>
                    <button
                      className="btn btn-primary"
                      onClick={saveEdits}
                      disabled={busy}
                      title="Save field changes to the local store"
                    >
                      Save
                    </button>
                  </>
                ) : (
                  <>
                    <button
                      className="btn"
                      onClick={startEdit}
                      disabled={busy}
                      title="Edit this requirement's fields"
                    >
                      Edit
                    </button>
                    <Menu
                      label="Actions"
                      align="right"
                      triggerClassName="btn"
                      title="Requirement actions"
                      items={[
                        {
                          key: "delete",
                          label: "Delete requirement…",
                          onClick: deleteReq,
                          danger: true,
                        },
                      ]}
                    />
                  </>
                )}
              </div>
            </div>
            {editing ? (
              <div className="reqs-detail-edit-form">
                <div className="precond-field">
                  <span>Summary</span>
                  <input
                    className="detail-input"
                    value={draftSummary}
                    autoFocus
                    disabled={busy}
                    onChange={(e) => setDraftSummary(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === "Escape") cancelEdit();
                    }}
                    placeholder="Requirement summary"
                  />
                </div>
                <div className="precond-field">
                  <span>Description</span>
                  <MarkdownField
                    className="detail-input precond-desc"
                    value={draftDescription}
                    onChange={setDraftDescription}
                    onCommit={() => {}}
                    rows={5}
                    placeholder="Markdown supported."
                  />
                </div>
                <div className="precond-field">
                  <span>Priority</span>
                  <input
                    className="detail-input"
                    value={draftPriority}
                    disabled={busy}
                    onChange={(e) => setDraftPriority(e.target.value)}
                    placeholder="e.g. High"
                  />
                </div>
                <div className="precond-field">
                  <span>Component(s)</span>
                  <input
                    className="detail-input"
                    value={draftComponents}
                    disabled={busy}
                    onChange={(e) => setDraftComponents(e.target.value)}
                    placeholder="Comma-separated, e.g. Backend, API"
                  />
                </div>
                <div className="precond-field">
                  <span>Fix Version(s)</span>
                  <input
                    className="detail-input"
                    value={draftFixVersions}
                    disabled={busy}
                    onChange={(e) => setDraftFixVersions(e.target.value)}
                    placeholder="Comma-separated, e.g. 2.0, 2.1"
                  />
                </div>
                <div className="precond-field">
                  <span>Sprint</span>
                  <input
                    className="detail-input"
                    value={draftSprint}
                    disabled={busy}
                    onChange={(e) => setDraftSprint(e.target.value)}
                    placeholder="e.g. Sprint 12"
                  />
                </div>
              </div>
            ) : (
              <h2 className="reqs-detail-summary">
                {sel.summary || "(no summary)"}
              </h2>
            )}

            {/* Read-only metadata grid + collapsible description: hidden in edit mode
                because the edit form shows these fields inline. */}
            {!editing && (
              <>
                <dl className="detail-meta">
                  {sel.priority && (
                    <>
                      <dt>Priority</dt>
                      <dd>{sel.priority}</dd>
                    </>
                  )}
                  {sel.components && (
                    <>
                      <dt>Component(s)</dt>
                      <dd>{sel.components}</dd>
                    </>
                  )}
                  {sel.fixVersions && (
                    <>
                      <dt>Fix Version(s)</dt>
                      <dd>{sel.fixVersions}</dd>
                    </>
                  )}
                  {sel.sprint && (
                    <>
                      <dt>Sprint</dt>
                      <dd>{sel.sprint}</dd>
                    </>
                  )}
                </dl>

                {sel.description && (
                  <div className="detail-description">
                    <button
                      className="bugs-md-desc-toggle"
                      onClick={() => setDescOpen((o) => !o)}
                      aria-expanded={descOpen}
                    >
                      {descOpen ? "▾" : "▸"} Description
                    </button>
                    {descOpen && (
                      <div className="bugs-md-detail-extra-text">
                        <Markdown>{sel.description}</Markdown>
                      </div>
                    )}
                  </div>
                )}
              </>
            )}

            {/* Requirement -> Requirement links */}
            <div className="precond-tests-head">
              <h4>Requires ({reqLinks.length})</h4>
              <button
                className="btn"
                onClick={() => setShowLinkReqs(true)}
                disabled={!sel}
                title="Link this requirement to other requirements it requires"
              >
                + Link requirements…
              </button>
            </div>
            {reqLinks.length === 0 ? (
              <p className="muted">This requirement has no outbound "requires" links.</p>
            ) : (
              <ul className="precond-list">
                {reqLinks.map((l) => (
                  <li key={`${l.fromKey}-${l.toKey}-${l.linkType}`} className="precond-item">
                    <span className="mono">{l.toKey}</span>
                    <span className="precond-type muted"> ({l.linkType})</span>
                  </li>
                ))}
              </ul>
            )}

            <div className="precond-tests-head">
              <h4>Covering tests ({tests.length})</h4>
              <button
                className="btn"
                onClick={() => setShowAdd(true)}
                disabled={!sel}
                title="Link tests to this requirement"
              >
                + Add tests
              </button>
            </div>
            {tests.length === 0 ? (
              <p className="muted">No tests cover this requirement.</p>
            ) : (
              <>
                <table className="board-table">
                  <thead>
                    <tr>
                      <th>Test</th>
                      <th>Summary</th>
                      <th>Status</th>
                      <th>Result</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pageTests.map((t) => (
                      <tr
                        key={t.key}
                        className={`reqs-test-row${t.key === detailKey ? " reqs-test-row-active" : ""}`}
                        onClick={() => setDetailKey(t.key)}
                        title="Open this test's detail"
                      >
                        <td className="mono">{t.key}</td>
                        <td>{t.summary}</td>
                        <td>{t.status || "—"}</td>
                        <td>
                          {t.runStatus ? (
                            <span
                              className={`run-badge run-${t.runStatus.toLowerCase()}`}
                            >
                              {t.runStatus}
                            </span>
                          ) : (
                            <span className="muted">not run</span>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                {tests.length > testsPageSize && (
                  <Pager
                    page={testsSafePage}
                    pageSize={testsPageSize}
                    total={tests.length}
                    onPage={setTestsPage}
                    onPageSize={(n) => {
                      setTestsPageSize(n);
                      setTestsPage(0);
                    }}
                  />
                )}
              </>
            )}
          </>
        )}
      </div>

      {showSources && (
        <RequirementSourcesModal
          profileId={profileId}
          onClose={() => setShowSources(false)}
        />
      )}

      {showLinkReqs && sel && (
        <LinkRequirementsModal
          profileId={profileId}
          fromKey={sel.key}
          currentLinkedKeys={reqLinks.map((l) => l.toKey)}
          onClose={() => setShowLinkReqs(false)}
          onDone={() => {
            setShowLinkReqs(false);
            onChanged?.();
          }}
        />
      )}

      {showAdd && sel && (
        <AddTestsModal
          profileId={profileId}
          containerKey={selected}
          targetLabel={sel.key}
          existingKeys={tests.map((t) => t.key)}
          onAdd={(keys) =>
            BulkAssociateRequirements(profileId, keys, [selected], true).then(
              () => undefined,
            )
          }
          onCancel={() => setShowAdd(false)}
          onDone={() => {
            setShowAdd(false);
            onChanged?.();
          }}
        />
      )}

      {showImportReqs && (
        <ImportRequirementsModal
          profileId={profileId}
          onComplete={() => {
            setShowImportReqs(false);
            onChanged?.();
          }}
          onCancel={() => setShowImportReqs(false)}
        />
      )}

      {detailKey && (
        <TestDetail
          profileId={profileId}
          testKey={detailKey}
          version={detailVersion}
          pendingForTest={[]}
          folders={[]}
          onClose={() => setDetailKey("")}
          onEdited={() => {
            setDetailVersion((v) => v + 1);
            onChanged?.();
          }}
        />
      )}
      {confirmUI}
    </div>
    </div>
  );
}
