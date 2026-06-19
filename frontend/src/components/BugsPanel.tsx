import { useEffect, useMemo, useState } from "react";
import {
  ListBugsWithTests,
  ListTestsForBug,
  SyncBugs,
  CreateContainerAndAllocate,
  BrowserOpenURL,
  errMsg,
} from "../api";
import type { BugWithTests, BugTest } from "../api";
import { Pager } from "./Pager";
import { SortControl } from "./SortControl";
import { usePrompt } from "./usePrompt";
import { keyCompare, cmpStr, applyDir } from "../sort";

interface Props {
  profileId: string;
  refreshKey: number;
  jiraUrl: string;
  onOpenTest: (testKey: string) => void;
}

function cmpBug(a: BugWithTests, b: BugWithTests, field: string): number {
  switch (field) {
    case "status":
      return cmpStr(a.status, b.status) || keyCompare(a.key, b.key);
    case "project":
      return cmpStr(a.projectKey, b.projectKey) || keyCompare(a.key, b.key);
    case "priority":
      return cmpStr(a.priority, b.priority) || keyCompare(a.key, b.key);
    default:
      return keyCompare(a.key, b.key);
  }
}

// BugsPanel is a master-detail view of the defects linked to the profile's
// tests: a filterable, paginated bug list on the left and, for the selected
// bug, a detail pane on the right showing its full info plus the affected tests
// enriched with their consolidated run status. Bug keys open in the browser;
// test keys open the test detail.
export function BugsPanel({ profileId, refreshKey, jiraUrl, onOpenTest }: Props) {
  const [bugs, setBugs] = useState<BugWithTests[]>([]);
  const [filter, setFilter] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [selected, setSelected] = useState("");
  const [tests, setTests] = useState<BugTest[]>([]);
  // Checked bugs for the bulk "Create Test Execution" action. This is kept
  // independent of `selected` (the detail-pane row): ticking a checkbox must
  // not change which bug is shown in the detail pane, and vice versa.
  const [checked, setChecked] = useState<Set<string>>(new Set());
  const [creating, setCreating] = useState(false);
  const [page, setPage] = useState(0); // 0-based
  const [pageSize, setPageSize] = useState(15);
  const [sortField, setSortField] = useState("key");
  const [sortDesc, setSortDesc] = useState(true);
  const [syncing, setSyncing] = useState(false);
  const { prompt, promptUI } = usePrompt();
  // Local refresh nonce: bumped after a bugs-only sync to re-pull the list
  // without forcing a full profile refresh.
  const [nonce, setNonce] = useState(0);

  // syncBugs refreshes only the defect issues from Jira (partial sync), so the
  // Bugs panel can update without re-running preconditions / containers /
  // requirements (RND_P_4TFINT_05-214).
  async function syncBugs() {
    setSyncing(true);
    setError("");
    setNotice("");
    try {
      await SyncBugs(profileId);
      setNonce((n) => n + 1);
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setSyncing(false);
    }
  }

  // Toggle a bug's checkbox without disturbing the detail-pane selection.
  function toggleChecked(key: string) {
    setChecked((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }

  // De-duplicated union of the linked test keys across all checked bugs. A bug
  // with no linked tests contributes nothing, so the union can be empty even
  // when bugs are checked - in that case the action is disabled.
  const unionTestKeys = useMemo(() => {
    const set = new Set<string>();
    for (const b of bugs) {
      if (checked.has(b.key)) {
        for (const k of b.testKeys ?? []) set.add(k);
      }
    }
    return [...set];
  }, [bugs, checked]);

  // Create a Test Execution whose members are the union of the checked bugs'
  // linked tests, to isolate a run that verifies only those bugs
  // (RND_P_4TFINT_05-222).
  async function createExecFromBugs() {
    if (unionTestKeys.length === 0) return;
    const checkedKeys = bugs
      .filter((b) => checked.has(b.key))
      .map((b) => b.key);
    const joined = checkedKeys.join(", ");
    const defaultName =
      joined.length > 60
        ? `Verify bugs: ${joined.slice(0, 57)}...`
        : `Verify bugs: ${joined}`;
    const name = await prompt({
      title: "New Test Execution",
      defaultValue: defaultName,
      placeholder: "Test Execution name",
      submitLabel: "Create",
    });
    if (!name || !name.trim()) return;
    setCreating(true);
    setError("");
    setNotice("");
    try {
      const res = await CreateContainerAndAllocate(
        profileId,
        "testexec",
        name.trim(),
        unionTestKeys,
      );
      setNotice(
        `Created Test Execution ${res.tempKey} with ${res.added} test${
          res.added === 1 ? "" : "s"
        }. It will appear in Containers.`,
      );
      setChecked(new Set());
      setNonce((n) => n + 1);
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setCreating(false);
    }
  }

  useEffect(() => {
    if (!profileId) return;
    let cancelled = false;
    ListBugsWithTests(profileId)
      .then((bs) => {
        if (!cancelled) setBugs(bs ?? []);
      })
      .catch((e) => {
        if (!cancelled) setError(errMsg(e));
      });
    return () => {
      cancelled = true;
    };
  }, [profileId, refreshKey, nonce]);

  const isDemo = /^(demo$|demo:|mock:)/i.test((jiraUrl ?? "").trim());
  const canLink = !!jiraUrl && !isDemo;
  function openBug(key: string) {
    const base = (jiraUrl ?? "").trim().replace(/\/+$/, "");
    if (base && canLink && !key.startsWith("NEW-"))
      BrowserOpenURL(`${base}/browse/${key}`);
  }

  const shown = useMemo(() => {
    const f = filter.trim().toLowerCase();
    const base = !f
      ? bugs
      : bugs.filter(
          (b) =>
            b.key.toLowerCase().includes(f) ||
            b.summary.toLowerCase().includes(f) ||
            b.projectKey.toLowerCase().includes(f) ||
            b.status.toLowerCase().includes(f),
        );
    return [...base].sort((a, b) => applyDir(cmpBug(a, b, sortField), sortDesc));
  }, [bugs, filter, sortField, sortDesc]);

  // Reset to the first page whenever the data source or the filter changes.
  useEffect(() => {
    setPage(0);
  }, [profileId, refreshKey, filter, sortField, sortDesc]);

  // Keep a valid selection: default to the first shown bug, and re-point when
  // the current one is filtered out.
  useEffect(() => {
    if (shown.length === 0) {
      setSelected("");
    } else if (!shown.some((b) => b.key === selected)) {
      setSelected(shown[0].key);
    }
  }, [shown, selected]);

  // Load the affected tests (with run status) for the selected bug.
  useEffect(() => {
    if (!profileId || !selected) {
      setTests([]);
      return;
    }
    let cancelled = false;
    ListTestsForBug(profileId, selected)
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

  const totalPages = Math.max(1, Math.ceil(shown.length / pageSize));
  const safePage = Math.min(Math.max(0, page), totalPages - 1);
  const paged = shown.slice(safePage * pageSize, safePage * pageSize + pageSize);
  const sel = bugs.find((b) => b.key === selected) ?? null;

  return (
    <div className="bugs-md">
      {promptUI}
      <div className="bugs-md-list">
        <div className="bugs-md-head">
          <span className="bugs-md-title">Bugs</span>
          <button
            className="btn"
            onClick={createExecFromBugs}
            disabled={creating || unionTestKeys.length === 0}
            title={
              checked.size === 0
                ? "Tick one or more bugs to create a Test Execution from their linked tests"
                : unionTestKeys.length === 0
                  ? "The checked bugs have no linked tests"
                  : `Create a Test Execution containing the ${unionTestKeys.length} test${
                      unionTestKeys.length === 1 ? "" : "s"
                    } linked to the ${checked.size} checked bug${
                      checked.size === 1 ? "" : "s"
                    }`
            }
          >
            {creating
              ? "Creating…"
              : `Create Test Execution${
                  unionTestKeys.length > 0 ? ` (${unionTestKeys.length})` : ""
                }`}
          </button>
          <button
            className="btn"
            onClick={syncBugs}
            disabled={syncing}
            title="Refresh just the linked bugs from Jira (partial sync)"
          >
            {syncing ? "Syncing…" : "Sync"}
          </button>
        </div>
        {error && <div className="error-text">{error}</div>}
        {notice && <p className="reqs-notice muted">{notice}</p>}
        <input
          className="search bugs-md-filter"
          placeholder="Filter bugs by key, summary, project, status…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
        <SortControl
          fields={[
            { value: "key", label: "Key" },
            { value: "status", label: "Status" },
            { value: "project", label: "Project" },
            { value: "priority", label: "Priority" },
          ]}
          field={sortField}
          desc={sortDesc}
          onChange={(f, d) => {
            setSortField(f);
            setSortDesc(d);
          }}
        />
        {shown.length === 0 ? (
          <p className="muted bugs-md-empty">
            {bugs.length === 0
              ? "No bugs linked to this profile's tests. File one from a failed test in a Test Execution, or sync a demo profile."
              : "No bugs match the filter."}
          </p>
        ) : (
          <>
            <ul className="bugs-md-items">
              {paged.map((b) => (
                <li
                  key={b.key}
                  className={`bugs-md-item${b.key === selected ? " bugs-md-item-selected" : ""}`}
                  onClick={() => setSelected(b.key)}
                >
                  <div className="bugs-md-item-top">
                    <input
                      type="checkbox"
                      className="bugs-md-check"
                      checked={checked.has(b.key)}
                      title="Include this bug's linked tests when creating a Test Execution"
                      onClick={(e) => e.stopPropagation()}
                      onChange={(e) => {
                        e.stopPropagation();
                        toggleChecked(b.key);
                      }}
                    />
                    <span className="mono bugs-md-key">{b.key}</span>
                    <span className="muted">{b.projectKey}</span>
                    {b.status && <span className="status-pill">{b.status}</span>}
                  </div>
                  <div className="bugs-md-item-summary">
                    {b.summary || "(no summary)"}
                  </div>
                  <div className="bugs-md-item-meta muted">
                    {b.priority} · {b.testKeys.length} test
                    {b.testKeys.length === 1 ? "" : "s"}
                  </div>
                </li>
              ))}
            </ul>
            <Pager
              compact
              page={safePage}
              pageSize={pageSize}
              total={shown.length}
              onPage={setPage}
              onPageSize={(n) => {
                setPageSize(n);
                setPage(0);
              }}
            />
          </>
        )}
      </div>

      <div className="bugs-md-detail">
        {!sel ? (
          <p className="muted">Select a bug to see its details.</p>
        ) : (
          <>
            <div className="bugs-md-detail-head">
              {canLink && !sel.key.startsWith("NEW-") ? (
                <button
                  className="mono bug-link-key bugs-md-detail-key"
                  onClick={() => openBug(sel.key)}
                  title={`Open ${sel.key} in Jira`}
                >
                  {sel.key}
                </button>
              ) : (
                <span className="mono bugs-md-detail-key">{sel.key}</span>
              )}
              <span className="muted">{sel.projectKey}</span>
              {sel.status && <span className="status-pill">{sel.status}</span>}
              {sel.priority && (
                <span className="muted bugs-md-detail-priority">
                  {sel.priority}
                </span>
              )}
            </div>
            <h2 className="bugs-md-detail-summary">
              {sel.summary || "(no summary)"}
            </h2>

            <h4>Affected tests ({tests.length})</h4>
            {tests.length === 0 ? (
              <p className="muted">No affected tests.</p>
            ) : (
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
                  {tests.map((t) => (
                    <tr key={t.key}>
                      <td>
                        <button
                          className="mono bug-link-key"
                          onClick={() => onOpenTest(t.key)}
                          title={`Open ${t.key}`}
                        >
                          {t.key}
                        </button>
                      </td>
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
            )}
          </>
        )}
      </div>
    </div>
  );
}
