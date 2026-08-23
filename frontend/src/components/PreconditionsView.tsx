import { useEffect, useMemo, useState } from "react";
import { useViewState } from "../lib/viewState";
import {
  ListPreconditionsWithUsage,
  ListTestsForPrecondition,
  CreatePreconditionDetailed,
  EditPreconditionField,
  DeletePrecondition,
  BulkAssociatePreconditions,
  errMsg,
} from "../api";
import type { PreconditionUsage, PreconditionTest } from "../api";
import { Menu } from "./Menu";
import { useConfirm } from "./useConfirm";
import { AddTestsModal } from "./AddTestsModal";
import { TestDetail } from "./TestDetail";
import { Markdown } from "./Markdown";
import { MarkdownField } from "./MarkdownField";
import { Pager } from "./Pager";
import { SortControl } from "./SortControl";
import { keyCompare, cmpStr, applyDir } from "../sort";
import { Modal } from "./Modal";

interface Props {
  profileId: string;
  refreshKey: number;
  onChanged: () => void;
}

// Xray Server/DC precondition types. The type drives how Xray interprets the
// precondition definition; Manual is the default for hand-written steps.
const PRECOND_TYPES = ["Manual", "Generic", "Cucumber"];

function cmpPre(
  a: PreconditionUsage,
  b: PreconditionUsage,
  field: string,
): number {
  switch (field) {
    case "type":
      return cmpStr(a.type, b.type) || keyCompare(a.key, b.key);
    case "usage":
      return (a.testCount ?? 0) - (b.testCount ?? 0) || keyCompare(a.key, b.key);
    default:
      return keyCompare(a.key, b.key);
  }
}

// PreconditionsView is the dedicated management surface for Preconditions
// (FR-13.4): a searchable master list on the left, and a detail pane on the
// right to edit a Precondition's summary / type / description, see and manage
// the Tests that reference it, create new ones, and delete. Everything is
// computed from the local store and queued for commit; it recomputes when the
// profile changes or a sync / commit bumps refreshKey.
export function PreconditionsView({ profileId, refreshKey, onChanged }: Props) {
  const [list, setList] = useState<PreconditionUsage[]>([]);
  const [selected, setSelected] = useViewState(profileId, "preconditions", "selected", "");
  const [tests, setTests] = useState<PreconditionTest[]>([]);
  const [filter, setFilter] = useViewState(profileId, "preconditions", "filter", "");
  const [usageFilter, setUsageFilter] = useViewState<"all" | "with" | "without">(profileId, "preconditions", "usageFilter", "all");
  const [sortField, setSortField] = useViewState(profileId, "preconditions", "sortField", "key");
  const [sortDesc, setSortDesc] = useViewState(profileId, "preconditions", "sortDesc", true);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [showCreate, setShowCreate] = useState(false);
  const [showAdd, setShowAdd] = useState(false);
  // A test opened from the "Used by" list, docked as an inline detail beside the
  // precondition detail (mirrors the requirement / browse views, #4).
  const [detailKey, setDetailKey] = useViewState(profileId, "preconditions", "detailKey", "");
  const [detailVersion, setDetailVersion] = useState(0);
  const { confirm, confirmUI } = useConfirm();

  // editing toggles the detail pane between read-only display and an explicit
  // edit session. Resets to false whenever the selected precondition changes so
  // navigating away never leaves a stale edit session open.
  const [editing, setEditing] = useState(false);

  const selectedPre = list.find((p) => p.key === selected) ?? null;
  const isLocal = selected.startsWith("new-precond-");

  // Draft buffers for the editable text fields. These are only committed to
  // Jira when the user explicitly clicks Save (not on blur). They resync from
  // the remote whenever the selected precondition reloads, and editing resets
  // to false so navigating to a new precondition always starts in read mode.
  const [summaryDraft, setSummaryDraft] = useState("");
  const [descDraft, setDescDraft] = useState("");
  const [condDraft, setCondDraft] = useState("");
  const [typeDraft, setTypeDraft] = useState("Manual");
  // Collapsible read-only fields -- collapsed by default, reset on selection change.
  const [condOpen, setCondOpen] = useState(false);
  const [descOpen, setDescOpen] = useState(false);
  // Collapse the read-only detail fields (summary, type, condition, description)
  // to a header line, like the requirement detail. Expanded by default.
  const [detailsOpen, setDetailsOpen] = useState(true);
  useEffect(() => {
    setEditing(false);
    setSummaryDraft(selectedPre?.summary ?? "");
    setDescDraft(selectedPre?.description ?? "");
    setCondDraft(selectedPre?.condition ?? "");
    setTypeDraft(selectedPre?.type || "Manual");
    setCondOpen(false);
    setDescOpen(false);
    setDetailsOpen(true);
  }, [selectedPre]);

  // Per-bucket counts for the usage pill filter, computed from the text-filtered
  // list so counts respond to the search input.
  const usageCounts = useMemo(() => {
    const q = filter.trim().toLowerCase();
    const base = !q
      ? list
      : list.filter(
          (p) =>
            p.key.toLowerCase().includes(q) ||
            p.summary.toLowerCase().includes(q) ||
            p.type.toLowerCase().includes(q),
        );
    const withTests = base.filter((p) => (p.testCount ?? 0) > 0).length;
    return { all: base.length, with: withTests, without: base.length - withTests };
  }, [list, filter]);

  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase();
    const base = !q
      ? list
      : list.filter(
          (p) =>
            p.key.toLowerCase().includes(q) ||
            p.summary.toLowerCase().includes(q) ||
            p.type.toLowerCase().includes(q),
        );
    const usageFiltered =
      usageFilter === "all"
        ? base
        : base.filter((p) =>
            usageFilter === "with" ? (p.testCount ?? 0) > 0 : (p.testCount ?? 0) === 0,
          );
    return [...usageFiltered].sort((a, b) => applyDir(cmpPre(a, b, sortField), sortDesc));
  }, [list, filter, usageFilter, sortField, sortDesc]);

  // Pagination of the precondition master list.
  const [listPage, setListPage] = useViewState(profileId, "preconditions", "listPage", 0);
  const [listPageSize, setListPageSize] = useViewState(profileId, "preconditions", "listPageSize", 15);
  useEffect(() => {
    setListPage(0);
  }, [filter, usageFilter, sortField, sortDesc]);
  const listTotalPages = Math.max(1, Math.ceil(filtered.length / listPageSize));
  const listSafePage = Math.min(listPage, listTotalPages - 1);
  const pageList = filtered.slice(
    listSafePage * listPageSize,
    (listSafePage + 1) * listPageSize,
  );

  // Load the master list whenever the profile changes or data refreshes,
  // keeping the current selection if it still exists.
  useEffect(() => {
    if (!profileId) return;
    let cancelled = false;
    setLoading(true);
    setError("");
    ListPreconditionsWithUsage(profileId)
      .then((ps) => {
        if (cancelled) return;
        const rows = ps ?? [];
        setList(rows);
        setSelected((cur) => {
          if (cur && rows.some((p) => p.key === cur)) return cur;
          return rows.length > 0 ? rows[0].key : "";
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
  }, [profileId, refreshKey]);

  // Pagination of the "Used by" tests list.
  const [testsPage, setTestsPage] = useViewState(profileId, "preconditions", "testsPage", 0);
  const [testsPageSize, setTestsPageSize] = useViewState(profileId, "preconditions", "testsPageSize", 15);
  useEffect(() => {
    setTestsPage(0);
  }, [selected]);
  const testsTotalPages = Math.max(1, Math.ceil(tests.length / testsPageSize));
  const testsSafePage = Math.min(testsPage, testsTotalPages - 1);
  const pageTests = tests.slice(
    testsSafePage * testsPageSize,
    (testsSafePage + 1) * testsPageSize,
  );

  // Load the selected Precondition's linked tests.
  useEffect(() => {
    if (!profileId || !selected) {
      setTests([]);
      return;
    }
    let cancelled = false;
    ListTestsForPrecondition(profileId, selected)
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

  // saveField persists an inline field edit (summary / type / description / condition).
  async function saveField(
    field: "summary" | "type" | "description" | "condition",
    value: string,
  ) {
    if (!selected || !selectedPre) return;
    const current = selectedPre[field];
    if (value === current) return;
    setError("");
    try {
      await EditPreconditionField(profileId, selected, field, value);
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  // handleSave commits all draft values that differ from the stored values and
  // then exits edit mode. Fields that are unchanged are skipped (saveField
  // short-circuits on equality). Errors surface in the existing error banner.
  async function handleSave() {
    await saveField("summary", summaryDraft);
    await saveField("type", typeDraft);
    await saveField("condition", condDraft);
    await saveField("description", descDraft);
    setEditing(false);
  }

  // handleCancel discards all draft changes and returns to read-only mode.
  function handleCancel() {
    setSummaryDraft(selectedPre?.summary ?? "");
    setDescDraft(selectedPre?.description ?? "");
    setCondDraft(selectedPre?.condition ?? "");
    setTypeDraft(selectedPre?.type || "Manual");
    setEditing(false);
  }

  async function removeTest(testKey: string) {
    if (!selected) return;
    setError("");
    try {
      await BulkAssociatePreconditions(profileId, [testKey], [selected], false);
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  async function deletePrecondition() {
    if (!selectedPre) return;
    if (
      !(await confirm({
        title: "Delete precondition",
        message:
          `Delete precondition ${selectedPre.key}? It will be unlinked from ` +
          `${selectedPre.testCount} test${selectedPre.testCount === 1 ? "" : "s"} ` +
          `and removed from Jira on commit.`,
        confirmLabel: "Delete",
        danger: true,
      }))
    )
      return;
    setError("");
    try {
      await DeletePrecondition(profileId, selected);
      setSelected("");
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  return (
    <div className={`precond-view${detailKey ? " precond-with-detail" : ""}`}>
      <aside className="precond-list">
        <div className="precond-list-head">
          <input
            className="precond-search"
            placeholder="Filter preconditions…"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          />
          <SortControl
            fields={[
              { value: "key", label: "Key" },
              { value: "type", label: "Type" },
              { value: "usage", label: "Usage" },
            ]}
            field={sortField}
            desc={sortDesc}
            onChange={(f, d) => {
              setSortField(f);
              setSortDesc(d);
            }}
          />
          <button
            className="btn btn-primary precond-new"
            onClick={() => setShowCreate(true)}
            title="Create a new precondition"
          >
            + New
          </button>
        </div>
        <div className="filter-pill-row">
          {(
            [
              { value: "all", label: "All" },
              { value: "with", label: "With tests" },
              { value: "without", label: "Without tests" },
            ] as const
          ).map(({ value, label }) => (
            <button
              key={value}
              className={`filter-pill${usageFilter === value ? " filter-pill-active" : ""}`}
              onClick={() => setUsageFilter(value)}
              title={`Filter by test usage: ${label}`}
            >
              {label} {usageCounts[value]}
            </button>
          ))}
        </div>

        {loading ? (
          <p className="muted precond-empty">Loading…</p>
        ) : list.length === 0 ? (
          <p className="muted precond-empty">
            No preconditions yet. Create one, or sync to pull them from
            Jira.
          </p>
        ) : filtered.length === 0 ? (
          <p className="muted precond-empty">No preconditions match.</p>
        ) : (
          <>
            <ul className="precond-items">
              {pageList.map((p) => (
                <li key={p.key}>
                  <button
                    className={`precond-item${p.key === selected ? " precond-item-active" : ""}`}
                    onClick={() => setSelected(p.key)}
                  >
                    <div className="precond-item-top">
                      <span className="mono precond-item-key">{p.key}</span>
                      <span
                        className={`pre-type-badge pre-type-${p.type.toLowerCase()}`}
                      >
                        {p.type || "—"}
                      </span>
                    </div>
                    <div className="precond-item-summary">
                      {p.summary || "(untitled)"}
                    </div>
                    <div className="precond-item-meta muted">
                      {p.testCount} test{p.testCount === 1 ? "" : "s"}
                    </div>
                  </button>
                </li>
              ))}
            </ul>
            {filtered.length > 0 && (
              <Pager
                compact
                page={listSafePage}
                pageSize={listPageSize}
                total={filtered.length}
                onPage={setListPage}
                onPageSize={(n) => {
                  setListPageSize(n);
                  setListPage(0);
                }}
              />
            )}
          </>
        )}
      </aside>

      <section className="precond-detail">
        {error && <div className="error-text">{error}</div>}

        {!selectedPre ? (
          <p className="muted precond-detail-empty">
            Select a precondition to view and manage it.
          </p>
        ) : (
          <>
            <div className="precond-detail-head">
              <div className="precond-detail-id">
                {!editing && (
                  <button
                    type="button"
                    className="collapse-caret"
                    onClick={() => setDetailsOpen((o) => !o)}
                    aria-expanded={detailsOpen}
                    title={detailsOpen ? "Hide details" : "Show details"}
                  >
                    {detailsOpen ? "▾" : "▸"}
                  </button>
                )}
                <span className="mono precond-detail-key">{selectedPre.key}</span>
                {isLocal && (
                  <span className="pending-badge" title="Not yet created in Jira">
                    new · uncommitted
                  </span>
                )}
              </div>
              <div className="precond-detail-actions">
                {editing ? (
                  <>
                    <button
                      className="btn"
                      onClick={handleCancel}
                      title="Discard edits and return to read-only view"
                    >
                      Cancel
                    </button>
                    <button
                      className="btn btn-primary"
                      onClick={handleSave}
                      title="Save changes to the local store"
                    >
                      Save
                    </button>
                  </>
                ) : (
                  <>
                    <button
                      className="btn"
                      onClick={() => setEditing(true)}
                      title="Edit this precondition's fields"
                    >
                      Edit
                    </button>
                    <Menu
                      label="Actions"
                      align="right"
                      triggerClassName="btn"
                      title="Precondition actions"
                      items={[
                        {
                          key: "delete",
                          label: "Delete precondition…",
                          onClick: deletePrecondition,
                          danger: true,
                        },
                      ]}
                    />
                  </>
                )}
              </div>
            </div>

            {editing ? (
              <>
                <div className="precond-field">
                  <span>Summary</span>
                  <MarkdownField
                    className="detail-input"
                    multiline={false}
                    value={summaryDraft}
                    onChange={setSummaryDraft}
                    onCommit={() => {}}
                    placeholder="Markdown supported."
                  />
                </div>

                <label className="precond-field">
                  <span>Type</span>
                  <select
                    className="detail-input"
                    value={typeDraft}
                    onChange={(e) => setTypeDraft(e.target.value)}
                  >
                    {PRECOND_TYPES.map((t) => (
                      <option key={t} value={t}>
                        {t}
                      </option>
                    ))}
                    {typeDraft && !PRECOND_TYPES.includes(typeDraft) && (
                      <option value={typeDraft}>{typeDraft}</option>
                    )}
                  </select>
                </label>

                <div className="precond-field">
                  <span>Condition</span>
                  <MarkdownField
                    className="detail-input precond-desc"
                    value={condDraft}
                    onChange={setCondDraft}
                    onCommit={() => {}}
                    rows={4}
                    placeholder="Markdown supported."
                  />
                </div>

                <div className="precond-field">
                  <span>Description</span>
                  <MarkdownField
                    className="detail-input precond-desc"
                    value={descDraft}
                    onChange={setDescDraft}
                    onCommit={() => {}}
                    rows={5}
                    placeholder="Markdown supported."
                  />
                </div>
              </>
            ) : detailsOpen ? (
              <>
                <div className="precond-field">
                  <span>Summary</span>
                  <div className="precond-ro-val">
                    {selectedPre.summary.trim() ? (
                      <Markdown>{selectedPre.summary}</Markdown>
                    ) : (
                      <span className="muted">No summary.</span>
                    )}
                  </div>
                </div>

                <div className="precond-field">
                  <span>Type</span>
                  <div className="precond-ro-val">
                    {selectedPre.type || "Manual"}
                  </div>
                </div>

                {selectedPre.condition?.trim() ? (
                  <div className="detail-description">
                    <button
                      className="bugs-md-desc-toggle"
                      onClick={() => setCondOpen((o) => !o)}
                      aria-expanded={condOpen}
                    >
                      {condOpen ? "▾" : "▸"} Condition
                    </button>
                    {condOpen && (
                      <div className="bugs-md-detail-extra-text">
                        <Markdown>{selectedPre.condition}</Markdown>
                      </div>
                    )}
                  </div>
                ) : (
                  <div className="precond-field">
                    <span>Condition</span>
                    <div className="precond-ro-val">
                      <span className="muted">No condition defined.</span>
                    </div>
                  </div>
                )}

                {selectedPre.description?.trim() ? (
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
                        <Markdown>{selectedPre.description}</Markdown>
                      </div>
                    )}
                  </div>
                ) : (
                  <div className="precond-field">
                    <span>Description</span>
                    <div className="precond-ro-val">
                      <span className="muted">No description.</span>
                    </div>
                  </div>
                )}
              </>
            ) : null}

            <div className="precond-tests-head">
              <h4>
                Used by {tests.length} test{tests.length === 1 ? "" : "s"}
              </h4>
              <button
                className="btn"
                onClick={() => setShowAdd(true)}
                title="Link tests to this precondition"
              >
                + Add tests
              </button>
            </div>

            {tests.length === 0 ? (
              <p className="muted">
                No tests reference this precondition yet.
              </p>
            ) : (
              <>
                <table className="board-table precond-tests">
                  <thead>
                    <tr>
                      <th>Test</th>
                      <th>Summary</th>
                      <th>Status</th>
                      <th aria-label="Remove" />
                    </tr>
                  </thead>
                  <tbody>
                    {pageTests.map((t) => (
                      <tr
                        key={t.key}
                        className={
                          t.key === detailKey ? "precond-test-row-active" : ""
                        }
                      >
                        <td>
                          <button
                            className="link-btn mono"
                            onClick={() => setDetailKey(t.key)}
                            title="Open test detail"
                          >
                            {t.key}
                          </button>
                        </td>
                        <td>{t.summary}</td>
                        <td>{t.status || "—"}</td>
                        <td className="board-remove-cell">
                          <button
                            className="btn btn-ghost board-remove"
                            onClick={() => removeTest(t.key)}
                            title="Unlink from this precondition"
                          >
                            ✕
                          </button>
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
      </section>

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
            onChanged();
          }}
        />
      )}

      {showCreate && (
        <CreatePreconditionModal
          profileId={profileId}
          onCancel={() => setShowCreate(false)}
          onCreated={(key) => {
            setShowCreate(false);
            setSelected(key);
            onChanged();
          }}
        />
      )}

      {showAdd && selectedPre && (
        <AddTestsModal
          profileId={profileId}
          containerKey={selected}
          targetLabel={selectedPre.key}
          existingKeys={tests.map((t) => t.key)}
          onAdd={(keys) =>
            BulkAssociatePreconditions(profileId, keys, [selected], true).then(
              () => undefined,
            )
          }
          onCancel={() => setShowAdd(false)}
          onDone={() => {
            setShowAdd(false);
            onChanged();
          }}
        />
      )}
      {confirmUI}
    </div>
  );
}

// CreatePreconditionModal collects a summary, type, description and condition
// for a new Precondition (FR-13.4 / 13.5), queued for creation in Jira on
// commit. Description and Condition both use MarkdownField for consistency with
// the detail pane. The condition is applied via EditPreconditionField after
// create so the CreatePreconditionDetailed signature stays unchanged.
function CreatePreconditionModal({
  profileId,
  onCancel,
  onCreated,
}: {
  profileId: string;
  onCancel: () => void;
  onCreated: (key: string) => void;
}) {
  const [summary, setSummary] = useState("");
  const [type, setType] = useState("Manual");
  const [description, setDescription] = useState("");
  const [condition, setCondition] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function create() {
    if (!summary.trim()) return;
    setBusy(true);
    setError("");
    try {
      const key = await CreatePreconditionDetailed(
        profileId,
        summary.trim(),
        type,
        description,
      );
      // Apply the condition via EditPreconditionField so the create signature
      // stays unchanged (no wails binding regen needed).
      if (condition.trim()) {
        await EditPreconditionField(profileId, key, "condition", condition);
      }
      onCreated(key);
    } catch (e) {
      setError(errMsg(e));
      setBusy(false);
    }
  }

  return (
    <Modal onClose={onCancel} className="modal" labelledBy="new-precond-title">
        <div className="pending-head">
          <h2 id="new-precond-title">New precondition</h2>
          <button className="btn btn-ghost" onClick={onCancel} title="Close">
            ✕
          </button>
        </div>
        <div className="bulk-body">
          <label className="precond-field">
            <span>Summary</span>
            <input
              className="detail-input"
              autoFocus
              placeholder="e.g. User is logged in"
              value={summary}
              onChange={(e) => setSummary(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") create();
              }}
            />
          </label>
          <label className="precond-field">
            <span>Type</span>
            <select
              className="detail-input"
              value={type}
              onChange={(e) => setType(e.target.value)}
            >
              {PRECOND_TYPES.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          </label>
          <div className="precond-field">
            <span>Condition</span>
            <MarkdownField
              className="detail-input precond-desc"
              value={condition}
              onChange={setCondition}
              onCommit={() => {}}
              rows={3}
              placeholder="e.g. Given the user is authenticated. Markdown supported."
            />
          </div>
          <div className="precond-field">
            <span>Description</span>
            <MarkdownField
              className="detail-input precond-desc"
              value={description}
              onChange={setDescription}
              onCommit={() => {}}
              rows={3}
              placeholder="Optional. Markdown supported."
            />
          </div>
          {error && <div className="error-text">{error}</div>}
        </div>
        <div className="pending-actions">
          <button className="btn" onClick={onCancel} disabled={busy}>
            Cancel
          </button>
          <button
            className="btn btn-primary"
            onClick={create}
            disabled={busy || !summary.trim()}
          >
            {busy ? "Creating…" : "Create"}
          </button>
        </div>
    </Modal>
  );
}
