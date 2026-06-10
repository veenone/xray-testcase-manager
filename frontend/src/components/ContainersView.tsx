import { useEffect, useState } from "react";
import {
  ListContainers,
  GetContainerBoard,
  SeedSampleContainers,
  CleanSampleData,
  CreateContainerAndAllocate,
  EditContainer,
  DeleteContainer,
  DeallocateTests,
  SetTestRunStatus,
  BulkSetTestRunStatus,
  ExportPytest,
  errMsg,
} from "../api";
import type { Container, TestPlanBoard, Bucket } from "../api";
import { Menu } from "./Menu";
import { AddTestsModal } from "./AddTestsModal";
import { usePrompt } from "./usePrompt";

interface Props {
  profileId: string;
  refreshKey: number;
  onChanged: () => void;
  // Sample-data generation is a demo aid — only offered for demo profiles so a
  // user can't seed fake containers into a real project (FR-5).
  isDemo: boolean;
}

const KINDS: Array<{ value: string; label: string }> = [
  { value: "testset", label: "Test Set" },
  { value: "testplan", label: "Test Plan" },
  { value: "testexec", label: "Test Execution" },
];

// Standard Xray Test Run result vocabulary (mirrors testrepo.RunStatuses).
const RUN_STATUSES = ["TODO", "EXECUTING", "PASS", "FAIL", "ABORTED", "BLOCKED"];

// ContainersView manages Test Sets / Plans / Executions (FR-13.7 + container
// CRUD): pick a kind and a container, see its member Tests with run status, and
// create / rename / delete. Computed from the local store; recomputes when the
// profile changes or a sync/commit bumps refreshKey.
export function ContainersView({
  profileId,
  refreshKey,
  onChanged,
  isDemo,
}: Props) {
  const [kind, setKind] = useState("testplan");
  const [containers, setContainers] = useState<Container[]>([]);
  const [selected, setSelected] = useState("");
  const [board, setBoard] = useState<TestPlanBoard | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [seeding, setSeeding] = useState(false);
  // Bulk execution-result selection: the set of Test keys checked in the board
  // (Test Execution only).
  const [selectedRuns, setSelectedRuns] = useState<Set<string>>(new Set());
  const [bulkStatus, setBulkStatus] = useState("PASS");
  const [editingName, setEditingName] = useState(false);
  const [nameDraft, setNameDraft] = useState("");
  const [showAdd, setShowAdd] = useState(false);
  const [boardPage, setBoardPage] = useState(0);
  const { prompt, promptUI } = usePrompt();

  // Member tables can be long (an Execution may hold hundreds of tests), so the
  // board is paged client-side. The page size is user-selectable; default 25.
  const [pageSize, setPageSize] = useState(25);
  const PAGE_SIZE_OPTIONS = [10, 15, 25, 50, 100, 200];

  // removeTest unassigns a single Test from the selected container (FR-3.4–3.6),
  // queued for commit.
  async function removeTest(testKey: string) {
    if (!selected) return;
    setError("");
    try {
      await DeallocateTests(profileId, selected, [testKey]);
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  // setRunStatus updates a Test's run result in the selected Test Execution,
  // queued for commit to Xray.
  async function setRunStatus(testKey: string, status: string) {
    if (!selected || !status) return;
    setError("");
    try {
      await SetTestRunStatus(profileId, selected, testKey, status);
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  const kindLabel = KINDS.find((k) => k.value === kind)?.label ?? "container";
  const selectedContainer = containers.find((c) => c.key === selected) ?? null;

  // commitInlineRename saves the detail card's editable name (an inline path to
  // the same rename CRUD as the Actions menu).
  async function commitInlineRename() {
    setEditingName(false);
    const name = nameDraft.trim();
    if (!selectedContainer || !name || name === selectedContainer.summary) return;
    setError("");
    try {
      await EditContainer(profileId, selected, name);
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  async function seed() {
    setSeeding(true);
    setError("");
    try {
      await SeedSampleContainers(profileId);
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setSeeding(false);
    }
  }

  // cleanSample removes the sample containers a previous seed created, so a real
  // project can start fresh. Real synced data is untouched (FR-5).
  async function cleanSample() {
    if (
      !window.confirm(
        "Remove all sample Test Sets / Plans / Executions created by 'Regenerate sample data'? " +
          "Real synced containers are not affected.",
      )
    )
      return;
    setError("");
    try {
      const removed = await CleanSampleData(profileId);
      window.alert(
        removed > 0
          ? `Removed ${removed} sample container${removed === 1 ? "" : "s"}.`
          : "No sample data found for this project.",
      );
      setSelected("");
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  // toggleRun / toggleRunAll manage the bulk execution-result selection.
  function toggleRun(testKey: string) {
    setSelectedRuns((prev) => {
      const next = new Set(prev);
      if (next.has(testKey)) next.delete(testKey);
      else next.add(testKey);
      return next;
    });
  }

  // applyBulkRunStatus sets one result on every selected Test in the execution.
  async function applyBulkRunStatus() {
    if (!selected || selectedRuns.size === 0) return;
    setError("");
    try {
      const res = await BulkSetTestRunStatus(
        profileId,
        selected,
        [...selectedRuns],
        bulkStatus,
      );
      if (res.failed && res.failed.length > 0) {
        setError(
          `Set ${res.succeeded.length}, failed ${res.failed.length}: ${res.failed[0].error}`,
        );
      }
      setSelectedRuns(new Set());
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  async function newContainer() {
    const name = await prompt({
      title: `New ${kindLabel}`,
      placeholder: `${kindLabel} name`,
      submitLabel: "Create",
    });
    if (!name || !name.trim()) return;
    setError("");
    try {
      await CreateContainerAndAllocate(profileId, kind, name.trim(), []);
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  async function renameContainer() {
    if (!selected) return;
    const cur = containers.find((c) => c.key === selected);
    const name = await prompt({
      title: `Rename ${kindLabel}`,
      defaultValue: cur?.summary ?? "",
      submitLabel: "Rename",
    });
    if (name === null || !name.trim()) return;
    setError("");
    try {
      await EditContainer(profileId, selected, name.trim());
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  async function generatePytest(style: string) {
    if (!selected) return;
    setError("");
    try {
      const path = await ExportPytest(profileId, selected, style);
      if (path) window.alert(`Scaffold saved to:\n${path}`);
    } catch (e) {
      setError(errMsg(e));
    }
  }

  async function deleteContainer() {
    if (!selected) return;
    if (
      !window.confirm(
        `Delete this ${kindLabel}? Its test memberships are removed (committed on sync).`,
      )
    )
      return;
    setError("");
    try {
      await DeleteContainer(profileId, selected);
      setSelected("");
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  useEffect(() => {
    if (!profileId) return;
    let cancelled = false;
    setLoading(true);
    setError("");
    ListContainers(profileId, kind)
      .then((cs) => {
        if (cancelled) return;
        setContainers(cs ?? []);
        setSelected((cur) => {
          if (cur && (cs ?? []).some((c) => c.key === cur)) return cur;
          return cs && cs.length > 0 ? cs[0].key : "";
        });
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
  }, [profileId, kind, refreshKey]);

  useEffect(() => {
    if (!profileId || !selected) {
      setBoard(null);
      return;
    }
    let cancelled = false;
    setError("");
    setBoardPage(0);
    setSelectedRuns(new Set());
    GetContainerBoard(profileId, selected)
      .then((b) => {
        if (!cancelled) setBoard(b);
      })
      .catch((e) => {
        if (!cancelled) setError(errMsg(e));
      });
    return () => {
      cancelled = true;
    };
  }, [profileId, selected, refreshKey]);

  // Client-side paging of the member table.
  const allRows = board?.rows ?? [];
  const boardTotalPages = Math.max(
    1,
    Math.ceil(allRows.length / pageSize),
  );
  const safePage = Math.min(boardPage, boardTotalPages - 1);
  const pageRows = allRows.slice(
    safePage * pageSize,
    (safePage + 1) * pageSize,
  );

  return (
    <div className="board">
      <div className="board-head">
        <label className="board-picker">
          <span>Type</span>
          <select value={kind} onChange={(e) => setKind(e.target.value)}>
            {KINDS.map((k) => (
              <option key={k.value} value={k.value}>
                {k.label}
              </option>
            ))}
          </select>
        </label>
        <label className="board-picker">
          <span>{kindLabel}</span>
          {loading ? (
            <span className="muted">Loading…</span>
          ) : (
            <select
              value={selected}
              onChange={(e) => setSelected(e.target.value)}
              disabled={containers.length === 0}
            >
              {containers.length === 0 && <option value="">None</option>}
              {containers.map((c) => (
                <option key={c.key} value={c.key}>
                  {c.key} — {c.summary}
                </option>
              ))}
            </select>
          )}
        </label>
        <div className="board-head-actions">
          <button className="btn btn-primary" onClick={newContainer} title={`New ${kindLabel}`}>
            + New
          </button>
          <Menu
            label="Actions"
            align="right"
            triggerClassName="btn"
            title={`${kindLabel} actions`}
            items={[
              {
                key: "rename",
                label: `Rename ${kindLabel}…`,
                onClick: renameContainer,
                disabled: !selected,
              },
              {
                key: "pytest",
                label: "Generate pytest…",
                onClick: () => generatePytest("function"),
                disabled: !selected,
                title:
                  "Plain pytest: one @pytest.mark.xray function per test",
              },
              {
                key: "pytest-unittest",
                label: "Generate pytest (unittest class)…",
                onClick: () => generatePytest("unittest"),
                disabled: !selected,
                title:
                  "unittest.TestCase subclass: one test method per test " +
                  "(runs under python -m unittest and pytest)",
              },
              {
                key: "delete",
                label: `Delete ${kindLabel}…`,
                onClick: deleteContainer,
                disabled: !selected,
                danger: true,
              },
              { key: "d", divider: true },
              // Regenerating sample data is a demo-only aid — hidden for real
              // projects so fake containers can't be seeded into them (FR-5).
              ...(isDemo
                ? [
                    {
                      key: "seed",
                      label: seeding
                        ? "Generating…"
                        : "Regenerate sample data",
                      onClick: seed,
                      disabled: seeding,
                      title:
                        "Regenerate sample sets / plans / executions from synced tests",
                    },
                  ]
                : []),
              {
                key: "clean",
                label: "Clean sample data…",
                onClick: cleanSample,
                danger: true,
                title:
                  "Remove sample containers created by 'Regenerate sample data' (real data untouched)",
              },
            ]}
          />
        </div>
      </div>

      {error && <div className="error-text">{error}</div>}

      {!loading && containers.length === 0 && (
        <p className="muted">
          No {kindLabel}s yet. Create one, run a sync, or generate sample data.
        </p>
      )}

      {selectedContainer && (
        <div className="container-card">
          <div className="container-card-top">
            <span className={`kind-badge kind-${kind}`}>{kindLabel}</span>
            <span className="mono container-card-key">
              {selectedContainer.key}
            </span>
            {selectedContainer.status && (
              <span className="status-pill">{selectedContainer.status}</span>
            )}
            <span className="container-card-count">
              {(board?.rows.length ?? 0).toLocaleString()} test
              {(board?.rows.length ?? 0) === 1 ? "" : "s"}
            </span>
            <button
              className="btn container-card-add"
              onClick={() => setShowAdd(true)}
              title={`Assign tests to this ${kindLabel}`}
            >
              + Add tests
            </button>
          </div>

          {editingName ? (
            <input
              className="container-card-name-edit"
              autoFocus
              value={nameDraft}
              onChange={(e) => setNameDraft(e.target.value)}
              onBlur={commitInlineRename}
              onKeyDown={(e) => {
                if (e.key === "Enter") commitInlineRename();
                if (e.key === "Escape") setEditingName(false);
              }}
            />
          ) : (
            <h2
              className="container-card-name"
              title="Click to rename"
              onClick={() => {
                setNameDraft(selectedContainer.summary);
                setEditingName(true);
              }}
            >
              {selectedContainer.summary || "(untitled)"}
            </h2>
          )}

          {board && board.runCounts.length > 0 && (
            <div className="container-card-runs">
              <RunBar counts={board.runCounts} />
              <div className="board-counts">
                {board.runCounts.map((b) => (
                  <RunBadge key={b.label} status={b.label} count={b.count} />
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      {kind === "testexec" && selectedRuns.size > 0 && (
        <div className="board-bulk">
          <span className="bulk-count">{selectedRuns.size} selected</span>
          <span className="muted">Set result:</span>
          <select
            className="run-select"
            value={bulkStatus}
            onChange={(e) => setBulkStatus(e.target.value)}
          >
            {RUN_STATUSES.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
          <button className="btn btn-primary" onClick={applyBulkRunStatus}>
            Apply to selected
          </button>
          <button className="btn" onClick={() => setSelectedRuns(new Set())}>
            Clear
          </button>
        </div>
      )}

      {board && containers.length > 0 && (
        <table className="board-table">
          <thead>
            <tr>
              {kind === "testexec" && (
                <th className="board-check-col">
                  <input
                    type="checkbox"
                    checked={
                      pageRows.length > 0 &&
                      pageRows.every((r) => selectedRuns.has(r.testKey))
                    }
                    onChange={(e) => {
                      const checked = e.target.checked;
                      setSelectedRuns((prev) => {
                        const next = new Set(prev);
                        pageRows.forEach((r) =>
                          checked
                            ? next.add(r.testKey)
                            : next.delete(r.testKey),
                        );
                        return next;
                      });
                    }}
                    title="Select all on this page"
                  />
                </th>
              )}
              <th>Test</th>
              <th>Summary</th>
              <th>Status</th>
              <th>Execution</th>
              <th aria-label="Remove" />
            </tr>
          </thead>
          <tbody>
            {board.rows.length === 0 ? (
              <tr>
                <td colSpan={kind === "testexec" ? 6 : 5} className="muted">
                  This {kindLabel.toLowerCase()} has no tests yet — use “+ Add
                  tests”.
                </td>
              </tr>
            ) : (
              pageRows.map((r) => (
                <tr
                  key={r.testKey}
                  className={selectedRuns.has(r.testKey) ? "board-row-sel" : ""}
                >
                  {kind === "testexec" && (
                    <td className="board-check-col">
                      <input
                        type="checkbox"
                        checked={selectedRuns.has(r.testKey)}
                        onChange={() => toggleRun(r.testKey)}
                      />
                    </td>
                  )}
                  <td className="mono">{r.testKey}</td>
                  <td>{r.summary}</td>
                  <td>{r.status || "—"}</td>
                  <td>
                    {kind === "testexec" ? (
                      <select
                        className={`run-select run-${(r.runStatus || "todo").toLowerCase()}`}
                        value={r.runStatus || ""}
                        onChange={(e) => setRunStatus(r.testKey, e.target.value)}
                        title="Set this test's result in this execution"
                      >
                        {!r.runStatus && <option value="">— set result —</option>}
                        {RUN_STATUSES.map((s) => (
                          <option key={s} value={s}>
                            {s}
                          </option>
                        ))}
                      </select>
                    ) : r.runStatus ? (
                      <span
                        className={`run-badge run-${r.runStatus.toLowerCase()}`}
                      >
                        {r.runStatus}
                      </span>
                    ) : (
                      <span className="muted">not run</span>
                    )}
                  </td>
                  <td className="board-remove-cell">
                    <button
                      className="btn btn-ghost board-remove"
                      onClick={() => removeTest(r.testKey)}
                      title={`Remove from this ${kindLabel}`}
                    >
                      ✕
                    </button>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      )}

      {board && allRows.length > 0 && (
        <div className="board-pager">
          <label className="board-pagesize">
            <span className="muted">Rows per page</span>
            <select
              value={pageSize}
              onChange={(e) => {
                setPageSize(Number(e.target.value));
                setBoardPage(0);
              }}
            >
              {PAGE_SIZE_OPTIONS.map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </select>
          </label>
          <span className="muted board-pager-range">
            {(safePage * pageSize + 1).toLocaleString()}–
            {Math.min(
              (safePage + 1) * pageSize,
              allRows.length,
            ).toLocaleString()}{" "}
            of {allRows.length.toLocaleString()} · page {safePage + 1} of{" "}
            {boardTotalPages}
          </span>
          <span className="board-pager-nav">
            <button
              className="btn"
              disabled={safePage === 0}
              onClick={() => setBoardPage((p) => Math.max(0, p - 1))}
            >
              ‹ Prev
            </button>
            <button
              className="btn"
              disabled={safePage >= boardTotalPages - 1}
              onClick={() =>
                setBoardPage((p) => Math.min(boardTotalPages - 1, p + 1))
              }
            >
              Next ›
            </button>
          </span>
        </div>
      )}

      {showAdd && selectedContainer && (
        <AddTestsModal
          profileId={profileId}
          containerKey={selected}
          existingKeys={(board?.rows ?? []).map((r) => r.testKey)}
          onCancel={() => setShowAdd(false)}
          onDone={() => {
            setShowAdd(false);
            onChanged();
          }}
        />
      )}

      {promptUI}
    </div>
  );
}

// RunBar is a compact stacked bar of run-status proportions for the selected
// container — a glanceable view of how its tests are doing.
function RunBar({ counts }: { counts: Bucket[] }) {
  const sum = counts.reduce((a, b) => a + b.count, 0) || 1;
  return (
    <div className="run-bar" title="Run-status distribution">
      {counts.map((b) => (
        <span
          key={b.label}
          className={runSegClass(b.label)}
          style={{ width: `${(b.count / sum) * 100}%` }}
          title={`${b.label}: ${b.count}`}
        />
      ))}
    </div>
  );
}

function runSegClass(label: string): string {
  if (label === "(not run)") return "run-seg";
  return `run-seg run-${label.toLowerCase()}`;
}

function RunBadge({ status, count }: { status: string; count: number }) {
  const cls =
    status === "(not run)" ? "run-badge" : `run-badge run-${status.toLowerCase()}`;
  return (
    <span className={cls}>
      {status === "(not run)" ? "not run" : status} {count}
    </span>
  );
}
