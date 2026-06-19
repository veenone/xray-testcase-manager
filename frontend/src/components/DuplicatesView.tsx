import { useCallback, useEffect, useRef, useState } from "react";

import {
  ScanDuplicates,
  ScanDuplicateGroupSteps,
  ExcludeFromDuplicates,
  EditTestField,
  GetTestSteps,
  errMsg,
} from "../api";
import type {
  DuplicateReport,
  DuplicateGroup,
  Folder,
  PendingChange,
  Step,
} from "../api";
import { TestDetail } from "./TestDetail";
import { Pager } from "./Pager";

type Filter = "all" | "identical" | "differ" | "excluded";

interface Props {
  profileId: string;
  refreshKey: number;
  folders: Folder[];
  pendingByTestKey: Map<string, PendingChange[]>;
  onChanged: () => void;
}

const VERDICT_LABEL: Record<string, string> = {
  identical: "steps identical",
  differ: "steps differ",
  unscanned: "steps not scanned",
};

// One member's steps, for the side-by-side comparison.
interface CompareMember {
  key: string;
  steps: Step[];
}

// One member's raw summary + description, for the side-by-side summary comparison.
interface SummaryMember {
  key: string;
  summary: string;
  description: string;
}

// normStep is the normalized comparison key for a single step (case / whitespace
// insensitive), so the side-by-side view can highlight rows that actually differ.
function normStep(s: Step | undefined): string {
  if (!s) return "∅";
  const n = (x: string) => (x || "").trim().toLowerCase().replace(/\s+/g, " ");
  return `${n(s.action)}|${n(s.data)}|${n(s.expected)}`;
}

// DuplicatesView is the duplicate-test management tab: scan, triage groups,
// exclude false positives, compare steps side-by-side, and edit real duplicates.
export function DuplicatesView({
  profileId,
  refreshKey,
  folders,
  pendingByTestKey,
  onChanged,
}: Props) {
  const [report, setReport] = useState<DuplicateReport | null>(null);
  const [error, setError] = useState("");
  const [filter, setFilter] = useState<Filter>("all");
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [scanningGroup, setScanningGroup] = useState<string>("");
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const [editing, setEditing] = useState<{ key: string; value: string } | null>(
    null,
  );
  const [detailVersion, setDetailVersion] = useState(0);
  const cancelEditRef = useRef(false);

  // Pagination of the (filtered) group list.
  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState(15);

  // Side-by-side step comparison (#4).
  const [compare, setCompare] = useState<{
    title: string;
    members: CompareMember[];
  } | null>(null);

  // Side-by-side raw-summary (+ description) comparison.
  const [summaryCompare, setSummaryCompare] = useState<{
    title: string;
    members: SummaryMember[];
  } | null>(null);

  const load = useCallback(() => {
    if (!profileId) return;
    ScanDuplicates(profileId)
      .then((r) => setReport(r as unknown as DuplicateReport))
      .catch((e) => setError(errMsg(e)));
  }, [profileId]);

  useEffect(() => {
    load();
  }, [load, refreshKey]);

  // Compare steps: record fingerprints (updates the verdict) AND open a
  // side-by-side view of every member's steps.
  async function compareSteps(g: DuplicateGroup) {
    setScanningGroup(g.normalizedSummary);
    setError("");
    try {
      await ScanDuplicateGroupSteps(profileId, g.normalizedSummary);
      const members: CompareMember[] = [];
      for (const m of g.members) {
        let steps: Step[] = [];
        try {
          steps = (await GetTestSteps(profileId, m.key, false)) ?? [];
        } catch {
          steps = [];
        }
        members.push({ key: m.key, steps });
      }
      setCompare({ title: g.displaySummary, members });
      load();
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setScanningGroup("");
    }
  }

  // Compare summaries: open a side-by-side view of every member's raw summary
  // (and description). Members share a NORMALIZED summary, so the raw values can
  // still differ in case / whitespace / punctuation. No backend round trip.
  function compareSummaries(g: DuplicateGroup) {
    const members: SummaryMember[] = g.members.map((m) => ({
      key: m.key,
      summary: m.summary,
      description: m.description ?? "",
    }));
    setSummaryCompare({ title: g.displaySummary, members });
  }

  async function exclude(key: string) {
    try {
      await ExcludeFromDuplicates(profileId, key);
      load();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  async function saveSummary() {
    if (!editing) return;
    if (cancelEditRef.current) {
      cancelEditRef.current = false;
      return;
    }
    try {
      await EditTestField(profileId, editing.key, "summary", editing.value);
      setEditing(null);
      onChanged();
      load();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  function toggle(norm: string) {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(norm)) next.delete(norm);
      else next.add(norm);
      return next;
    });
  }

  const groups = (report?.groups ?? []).filter((g) => {
    if (filter === "all") return true;
    if (filter === "excluded") return false; // excluded shown via count only
    return g.stepsVerdict === filter;
  });

  const totalPages = Math.max(1, Math.ceil(groups.length / pageSize));
  const safePage = Math.min(page, totalPages - 1);
  const pageGroups = groups.slice(
    safePage * pageSize,
    (safePage + 1) * pageSize,
  );

  function changeFilter(f: Filter) {
    setFilter(f);
    setPage(0);
  }

  return (
    <div className="dup-view">
      <div className="dup-toolbar">
        <button
          className="btn btn-primary"
          onClick={() => {
            setPage(0);
            load();
          }}
        >
          ⟳ Scan
        </button>
        {report?.scannedAt && (
          <span className="muted">
            steps last scanned {new Date(report.scannedAt).toLocaleString()}
          </span>
        )}
        <span style={{ flex: 1 }} />
        <span className="muted">filter:</span>
        <div className="dup-seg">
          {(["all", "identical", "differ"] as Filter[]).map((f) => (
            <button
              key={f}
              className={`dup-seg-btn${filter === f ? " on" : ""}`}
              onClick={() => changeFilter(f)}
            >
              {f === "all"
                ? "All"
                : f === "identical"
                  ? "Steps identical"
                  : "Steps differ"}
            </button>
          ))}
        </div>
      </div>

      {error && <div className="error-text dup-error">{error}</div>}

      {report && (
        <div className="dup-tiles">
          <div className="dup-tile t-grp">
            <b>{report.groupCount}</b>
            <span>duplicate groups</span>
          </div>
          <div className="dup-tile t-grp">
            <b>{report.testCount}</b>
            <span>duplicate tests</span>
          </div>
          <div className="dup-tile t-dup">
            <b>{report.stepsIdentical}</b>
            <span>steps identical</span>
          </div>
          <div className="dup-tile t-diff">
            <b>{report.stepsDiffer}</b>
            <span>steps differ</span>
          </div>
          <div className="dup-tile t-muted">
            <b>{report.stepsUnscanned}</b>
            <span>not scanned</span>
          </div>
          <div className="dup-tile t-muted">
            <b>{report.excluded}</b>
            <span>excluded</span>
          </div>
        </div>
      )}

      <div className="dup-body">
        <div className="dup-list-col">
          <div className="dup-list">
            {pageGroups.map((g) => {
              const open = expanded.has(g.normalizedSummary);
              return (
                <div className="dup-group" key={g.normalizedSummary}>
                  <div
                    className="dup-ghead"
                    onClick={() => toggle(g.normalizedSummary)}
                  >
                    <span className="dup-caret">{open ? "▾" : "▸"}</span>
                    <span className="dup-gtitle">"{g.displaySummary}"</span>
                    <span className="dup-pill p-n">
                      {g.members.length} tests
                    </span>
                    <span className="dup-pill p-sum">summary identical</span>
                    <span className={`dup-pill p-${g.stepsVerdict}`}>
                      {VERDICT_LABEL[g.stepsVerdict]}
                    </span>
                    <button
                      className="btn dup-cmp"
                      onClick={(e) => {
                        e.stopPropagation();
                        compareSummaries(g);
                      }}
                    >
                      Compare summaries
                    </button>
                    <button
                      className="btn dup-cmp"
                      onClick={(e) => {
                        e.stopPropagation();
                        compareSteps(g);
                      }}
                      disabled={scanningGroup === g.normalizedSummary}
                    >
                      {scanningGroup === g.normalizedSummary
                        ? "Comparing…"
                        : "Compare steps"}
                    </button>
                  </div>
                  {open && (
                    <div className="dup-members">
                      {g.members.map((m) => {
                        const dirty =
                          (pendingByTestKey.get(m.key) ?? []).length > 0;
                        return (
                          <div className="dup-mrow" key={m.key}>
                            <span className="dup-mkey">
                              {m.key}
                              {dirty && (
                                <span
                                  className="dup-dot"
                                  title="Uncommitted edits"
                                >
                                  {" "}
                                  ●
                                </span>
                              )}
                            </span>
                            <span className="dup-mstatus">
                              {m.status || "—"}
                            </span>
                            {editing?.key === m.key ? (
                              <input
                                className="detail-input dup-edit"
                                value={editing.value}
                                autoFocus
                                onChange={(e) =>
                                  setEditing({ key: m.key, value: e.target.value })
                                }
                                onKeyDown={(e) => {
                                  if (e.key === "Enter") {
                                    e.preventDefault();
                                    (e.target as HTMLInputElement).blur();
                                  }
                                  if (e.key === "Escape") {
                                    cancelEditRef.current = true;
                                    setEditing(null);
                                  }
                                }}
                                onBlur={saveSummary}
                              />
                            ) : (
                              <span className="dup-mfolder">
                                {m.folderId || "—"}
                              </span>
                            )}
                            <div className="dup-acts">
                              <button
                                className="dup-act act-ex"
                                onClick={() => exclude(m.key)}
                              >
                                Exclude
                              </button>
                              <button
                                className="dup-act act-edit"
                                onClick={() =>
                                  setEditing({ key: m.key, value: m.summary })
                                }
                              >
                                Edit summary
                              </button>
                              <button
                                className="dup-act act-edit"
                                onClick={() => {
                                  setSelectedKey(m.key);
                                  setDetailVersion((v) => v + 1);
                                }}
                              >
                                Description / Steps
                              </button>
                            </div>
                          </div>
                        );
                      })}
                    </div>
                  )}
                </div>
              );
            })}
            {report && groups.length === 0 && (
              <div className="muted dup-empty">
                No duplicate groups for this filter.
              </div>
            )}
          </div>

          {groups.length > 0 && (
            <Pager
              page={safePage}
              pageSize={pageSize}
              total={groups.length}
              onPage={setPage}
              onPageSize={(n) => {
                setPageSize(n);
                setPage(0);
              }}
            />
          )}
        </div>

        {selectedKey && (
          <TestDetail
            profileId={profileId}
            testKey={selectedKey}
            version={detailVersion}
            pendingForTest={pendingByTestKey.get(selectedKey) ?? []}
            folders={folders}
            onClose={() => setSelectedKey(null)}
            onEdited={() => {
              onChanged();
              load();
            }}
          />
        )}
      </div>

      {compare && (
        <StepCompareModal
          title={compare.title}
          members={compare.members}
          onClose={() => setCompare(null)}
        />
      )}

      {summaryCompare && (
        <SummaryCompareModal
          title={summaryCompare.title}
          members={summaryCompare.members}
          onClose={() => setSummaryCompare(null)}
        />
      )}
    </div>
  );
}

// StepCompareModal shows the steps of every duplicate-group member side by side,
// one column per test, aligned by step position, with differing rows highlighted.
function StepCompareModal({
  title,
  members,
  onClose,
}: {
  title: string;
  members: CompareMember[];
  onClose: () => void;
}) {
  const maxSteps = members.reduce((m, c) => Math.max(m, c.steps.length), 0);

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div
        className="modal dup-compare-modal"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="pending-head">
          <h2>Compare steps — "{title}"</h2>
          <button className="btn btn-ghost" onClick={onClose} title="Close">
            ✕
          </button>
        </div>

        <div className="dup-compare-wrap">
          {maxSteps === 0 ? (
            <p className="muted">None of these tests have steps to compare.</p>
          ) : (
            <table className="dup-compare-table">
              <thead>
                <tr>
                  <th className="dup-compare-idx">#</th>
                  {members.map((m) => (
                    <th key={m.key} className="mono">
                      {m.key}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {Array.from({ length: maxSteps }).map((_, i) => {
                  const cells = members.map((m) => m.steps[i]);
                  const differs =
                    new Set(cells.map((s) => normStep(s))).size > 1;
                  return (
                    <tr
                      key={i}
                      className={differs ? "dup-compare-diff" : undefined}
                    >
                      <td className="dup-compare-idx">{i + 1}</td>
                      {cells.map((s, j) => (
                        <td key={members[j].key}>
                          {s ? (
                            <div className="dup-compare-step">
                              <div className="dup-compare-action">
                                {s.action || <span className="muted">—</span>}
                              </div>
                              {s.data && (
                                <div className="dup-compare-sub">
                                  <span className="dup-compare-lbl">data</span>
                                  {s.data}
                                </div>
                              )}
                              {s.expected && (
                                <div className="dup-compare-sub">
                                  <span className="dup-compare-lbl">
                                    expected
                                  </span>
                                  {s.expected}
                                </div>
                              )}
                            </div>
                          ) : (
                            <span className="muted">(no step)</span>
                          )}
                        </td>
                      ))}
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>

        <div className="pending-actions">
          <span className="muted dup-compare-legend">
            Highlighted rows differ between tests.
          </span>
          <button className="btn btn-primary" onClick={onClose}>
            Close
          </button>
        </div>
      </div>
    </div>
  );
}

// SummaryCompareModal shows the raw summary (and description) of every
// duplicate-group member side by side, one column per test. Members share a
// NORMALIZED summary, so the raw values can still differ in case / whitespace /
// punctuation; rows whose raw values are not all equal are highlighted.
function SummaryCompareModal({
  title,
  members,
  onClose,
}: {
  title: string;
  members: SummaryMember[];
  onClose: () => void;
}) {
  // Show the description row only if at least one member has a description.
  const hasDescription = members.some((m) => (m.description || "").trim() !== "");
  const rows: { label: string; value: (m: SummaryMember) => string }[] = [
    { label: "summary", value: (m) => m.summary },
  ];
  if (hasDescription) {
    rows.push({ label: "description", value: (m) => m.description });
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div
        className="modal dup-compare-modal"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="pending-head">
          <h2>Compare summaries — "{title}"</h2>
          <button className="btn btn-ghost" onClick={onClose} title="Close">
            ✕
          </button>
        </div>

        <div className="dup-compare-wrap">
          <p className="muted dup-compare-ref">
            Grouped by normalized summary: "{title}". Members can still differ in
            case, whitespace, or punctuation.
          </p>
          <table className="dup-compare-table">
            <thead>
              <tr>
                <th className="dup-compare-idx">field</th>
                {members.map((m) => (
                  <th key={m.key} className="mono">
                    {m.key}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => {
                const values = members.map((m) => row.value(m));
                const differs = new Set(values).size > 1;
                return (
                  <tr
                    key={row.label}
                    className={differs ? "dup-compare-diff" : undefined}
                  >
                    <td className="dup-compare-idx">{row.label}</td>
                    {values.map((v, j) => (
                      <td key={members[j].key}>
                        {v && v.trim() !== "" ? (
                          <div className="dup-compare-step">{v}</div>
                        ) : (
                          <span className="muted">—</span>
                        )}
                      </td>
                    ))}
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>

        <div className="pending-actions">
          <span className="muted dup-compare-legend">
            Highlighted rows differ between tests.
          </span>
          <button className="btn btn-primary" onClick={onClose}>
            Close
          </button>
        </div>
      </div>
    </div>
  );
}
