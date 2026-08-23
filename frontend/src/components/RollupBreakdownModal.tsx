import { useState } from "react";

import type { RollupMember } from "../api";
import { Modal } from "./Modal";

// Human label for the consolidated bucket, shown in the modal title/hint.
const STATUS_TITLE: Record<string, string> = {
  PASS: "Passed",
  FAIL: "Failed",
  EXECUTING: "Executing",
  ABORTED: "Aborted",
  BLOCKED: "Blocked",
  "(not run)": "Not run",
};

const KNOWN = ["PASS", "FAIL", "EXECUTING", "ABORTED", "BLOCKED"];

// StatusChip renders a run result using the shared run-badge palette, so the
// consolidated result and each per-execution result read the same as elsewhere.
function StatusChip({ raw }: { raw: string }) {
  const s = (raw || "").toUpperCase();
  const cls = KNOWN.includes(s) ? `run-badge run-${s.toLowerCase()}` : "run-badge";
  const label = s && s !== "TODO" ? raw : "not run";
  return <span className={cls}>{label}</span>;
}

interface Props {
  kindLabel: string;
  containerKey: string;
  // The clicked roll-up bucket, e.g. "FAIL" or "(not run)".
  status: string;
  members: RollupMember[];
  loading: boolean;
  onClose: () => void;
}

// RollupBreakdownModal explains one roll-up badge: it lists the member tests
// whose consolidated result is `status`, and for each shows the per-execution
// results behind it, so the worst-wins relationship is visible. Informational
// only; nothing here mutates data.
export function RollupBreakdownModal({
  kindLabel,
  containerKey,
  status,
  members,
  loading,
  onClose,
}: Props) {
  // Per-test execution detail is collapsed by default to keep the modal short;
  // the user expands the tests they want to inspect.
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const toggle = (key: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });

  const shown = members.filter((m) => m.consolidated === status);
  const title = STATUS_TITLE[status] ?? status;

  return (
    <Modal onClose={onClose} className="modal rollup-breakdown-modal" labelledBy="rollup-breakdown-title">
        <div className="pending-head">
          <h2 id="rollup-breakdown-title">
            {title}: {shown.length} test{shown.length === 1 ? "" : "s"} in{" "}
            {containerKey}
          </h2>
          <button className="btn btn-ghost" onClick={onClose} title="Close">
            ✕
          </button>
        </div>

        <div className="rollup-breakdown-body">
          <p className="rollup-breakdown-hint">
            Each test below consolidates to <strong>{title}</strong> across the{" "}
            {kindLabel.toLowerCase()}'s executions that ran it. The worst result
            wins, so one failing run makes the whole test fail. Nothing here
            changes any data.
          </p>

          {loading ? (
            <p className="muted">Loading…</p>
          ) : shown.length === 0 ? (
            <p className="muted">No tests in this result.</p>
          ) : (
            <ul className="rollup-breakdown-list">
              {shown.map((m) => {
                const isOpen = expanded.has(m.testKey);
                return (
                  <li key={m.testKey} className="rollup-breakdown-item">
                    <button
                      type="button"
                      className="rollup-breakdown-test"
                      onClick={() => toggle(m.testKey)}
                      aria-expanded={isOpen}
                      title={isOpen ? "Hide executions" : "Show executions"}
                    >
                      <span className="rollup-breakdown-caret" aria-hidden="true">
                        {isOpen ? "▾" : "▸"}
                      </span>
                      <span className="mono rollup-breakdown-key">{m.testKey}</span>
                      <span className="rollup-breakdown-summary">{m.summary}</span>
                      <span className="rollup-breakdown-runcount">
                        {m.runs.length} exec{m.runs.length === 1 ? "" : "s"}
                      </span>
                      <StatusChip raw={m.consolidated} />
                    </button>
                    {isOpen &&
                      (m.runs.length === 0 ? (
                        <div className="rollup-breakdown-runs muted">
                          No execution has run this test yet.
                        </div>
                      ) : (
                        <ul className="rollup-breakdown-runs">
                          {m.runs.map((run) => (
                            <li key={run.execKey} className="rollup-breakdown-run">
                              <span className="mono">{run.execKey}</span>
                              <span className="rollup-breakdown-exec-summary">
                                {run.execSummary}
                              </span>
                              <StatusChip raw={run.status} />
                            </li>
                          ))}
                        </ul>
                      ))}
                  </li>
                );
              })}
            </ul>
          )}
        </div>

        <div className="rollup-breakdown-foot">
          <button className="btn btn-primary" onClick={onClose}>
            Close
          </button>
        </div>
    </Modal>
  );
}
