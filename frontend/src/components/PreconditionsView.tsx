import { useEffect, useMemo, useState } from "react";
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
import { AddTestsModal } from "./AddTestsModal";
import { MarkdownField } from "./MarkdownField";
import { Pager } from "./Pager";

interface Props {
  profileId: string;
  refreshKey: number;
  onChanged: () => void;
}

// Xray Server/DC precondition types. The type drives how Xray interprets the
// precondition definition; Manual is the default for hand-written steps.
const PRECOND_TYPES = ["Manual", "Generic", "Cucumber"];

// PreconditionsView is the dedicated management surface for Preconditions
// (FR-13.4): a searchable master list on the left, and a detail pane on the
// right to edit a Precondition's summary / type / description, see and manage
// the Tests that reference it, create new ones, and delete. Everything is
// computed from the local store and queued for commit; it recomputes when the
// profile changes or a sync / commit bumps refreshKey.
export function PreconditionsView({ profileId, refreshKey, onChanged }: Props) {
  const [list, setList] = useState<PreconditionUsage[]>([]);
  const [selected, setSelected] = useState("");
  const [tests, setTests] = useState<PreconditionTest[]>([]);
  const [filter, setFilter] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [showCreate, setShowCreate] = useState(false);
  const [showAdd, setShowAdd] = useState(false);

  const selectedPre = list.find((p) => p.key === selected) ?? null;
  const isLocal = selected.startsWith("new-precond-");

  // Draft buffers for the markdown-rendered text fields. MarkdownField is
  // controlled (renders markdown when idle, edits raw on click), so the value
  // lives here and resyncs whenever the selected precondition reloads.
  const [summaryDraft, setSummaryDraft] = useState("");
  const [descDraft, setDescDraft] = useState("");
  useEffect(() => {
    setSummaryDraft(selectedPre?.summary ?? "");
    setDescDraft(selectedPre?.description ?? "");
  }, [selectedPre]);

  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return list;
    return list.filter(
      (p) =>
        p.key.toLowerCase().includes(q) ||
        p.summary.toLowerCase().includes(q) ||
        p.type.toLowerCase().includes(q),
    );
  }, [list, filter]);

  // Pagination of the precondition master list.
  const [listPage, setListPage] = useState(0);
  const [listPageSize, setListPageSize] = useState(15);
  useEffect(() => {
    setListPage(0);
  }, [filter]);
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
  const [testsPage, setTestsPage] = useState(0);
  const [testsPageSize, setTestsPageSize] = useState(15);
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

  // saveField persists an inline field edit (summary / type / description).
  async function saveField(
    field: "summary" | "type" | "description",
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
      !window.confirm(
        `Delete precondition ${selectedPre.key}? It will be unlinked from ` +
          `${selectedPre.testCount} test${selectedPre.testCount === 1 ? "" : "s"} ` +
          `and removed from Jira on commit.`,
      )
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
    <div className="precond-view">
      <aside className="precond-list">
        <div className="precond-list-head">
          <input
            className="precond-search"
            placeholder="Filter preconditions…"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          />
          <button
            className="btn btn-primary precond-new"
            onClick={() => setShowCreate(true)}
            title="Create a new precondition"
          >
            + New
          </button>
        </div>

        {loading ? (
          <p className="muted precond-empty">Loading…</p>
        ) : list.length === 0 ? (
          <p className="muted precond-empty">
            No preconditions yet. Create one, or run a sync to pull them from
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
                <span className="mono precond-detail-key">{selectedPre.key}</span>
                {isLocal && (
                  <span className="pending-badge" title="Not yet created in Jira">
                    new · uncommitted
                  </span>
                )}
              </div>
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
            </div>

            <div className="precond-field">
              <span>Summary</span>
              <MarkdownField
                className="detail-input"
                multiline={false}
                value={summaryDraft}
                onChange={setSummaryDraft}
                onCommit={() => saveField("summary", summaryDraft)}
                placeholder="Click to edit — markdown supported."
              />
            </div>

            <label className="precond-field">
              <span>Type</span>
              <select
                className="detail-input"
                value={selectedPre.type || "Manual"}
                onChange={(e) => saveField("type", e.target.value)}
              >
                {PRECOND_TYPES.map((t) => (
                  <option key={t} value={t}>
                    {t}
                  </option>
                ))}
                {selectedPre.type &&
                  !PRECOND_TYPES.includes(selectedPre.type) && (
                    <option value={selectedPre.type}>{selectedPre.type}</option>
                  )}
              </select>
            </label>

            <div className="precond-field">
              <span>Description</span>
              <MarkdownField
                className="detail-input precond-desc"
                value={descDraft}
                onChange={setDescDraft}
                onCommit={() => saveField("description", descDraft)}
                rows={5}
                placeholder="No description. Click to add — markdown supported."
              />
            </div>

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
                      <tr key={t.key}>
                        <td className="mono">{t.key}</td>
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
    </div>
  );
}

// CreatePreconditionModal collects a summary, type and description for a new
// Precondition (FR-13.4 / 13.5), queued for creation in Jira on commit.
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
      onCreated(key);
    } catch (e) {
      setError(errMsg(e));
      setBusy(false);
    }
  }

  return (
    <div className="modal-overlay" onClick={onCancel}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <div className="pending-head">
          <h2>New precondition</h2>
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
          <label className="precond-field">
            <span>Description</span>
            <textarea
              className="detail-input precond-desc"
              rows={3}
              placeholder="Optional"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </label>
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
      </div>
    </div>
  );
}
