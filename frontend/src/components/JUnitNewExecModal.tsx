import { useRef, useState } from "react";
import { AnalyzeJUnitImportNewExec, ApplyJUnitImportNewExec, errMsg } from "../api";
import type { JUnitNewExecPreview, JUnitNewExecResult } from "../api";
import { fileToBase64 } from "../files";

interface Props {
  profileId: string;
  onCancel: () => void;
  onApplied: (result: JUnitNewExecResult) => void;
}

// JUnitNewExecModal creates a brand-new Test Execution from a JUnit XML report.
// Flow: (1) params step — enter execution summary, toggle create-missing, pick
// a file; (2) preview step — review the row table and skipped list; (3) apply.
export function JUnitNewExecModal({ profileId, onCancel, onApplied }: Props) {
  const [summary, setSummary] = useState("");
  const [createMissing, setCreateMissing] = useState(true);
  const [preview, setPreview] = useState<JUnitNewExecPreview | null>(null);
  const [analyzeError, setAnalyzeError] = useState("");
  const [analyzing, setAnalyzing] = useState(false);
  const [applying, setApplying] = useState(false);
  const [applyError, setApplyError] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

  async function onFileChange(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;
    // Reset file input so the same file can be re-picked after a param change.
    e.target.value = "";
    setAnalyzeError("");
    setApplyError("");
    setPreview(null);
    setAnalyzing(true);
    try {
      const { b64 } = await fileToBase64(file);
      const result = await AnalyzeJUnitImportNewExec(profileId, b64, createMissing);
      setPreview(result);
    } catch (err) {
      setAnalyzeError(errMsg(err));
    } finally {
      setAnalyzing(false);
    }
  }

  async function applyImport() {
    if (!preview || !summary.trim()) return;
    setApplying(true);
    setApplyError("");
    try {
      const result = await ApplyJUnitImportNewExec(profileId, summary.trim(), preview.rows);
      onApplied(result);
    } catch (err) {
      setApplyError(errMsg(err));
      setApplying(false);
    }
  }

  // Compute preview counts from the rows.
  const existingCount = preview ? preview.rows.filter((r) => !r.create).length : 0;
  const toCreateCount = preview ? preview.rows.filter((r) => r.create).length : 0;
  const passCount = preview ? preview.rows.filter((r) => r.result === "PASS").length : 0;
  const failCount = preview ? preview.rows.filter((r) => r.result === "FAIL").length : 0;
  const unsetCount = preview ? preview.rows.filter((r) => !r.result).length : 0;
  const skippedCount = preview?.skipped?.length ?? 0;

  const summaryTrimmed = summary.trim();
  const canApply = !!preview && preview.rows.length > 0 && !!summaryTrimmed && !applying;

  return (
    <div className="modal-overlay" onClick={onCancel}>
      <div className="modal bulk-modal" onClick={(e) => e.stopPropagation()}>
        {/* Hidden file input */}
        <input
          ref={inputRef}
          type="file"
          accept=".xml"
          style={{ display: "none" }}
          onChange={onFileChange}
        />

        <div className="pending-head">
          <h2>New execution from JUnit XML</h2>
          <button className="btn btn-ghost" onClick={onCancel} title="Close">
            ✕
          </button>
        </div>

        <div className="bulk-body">
          {/* Params section — always visible so the user can adjust before re-picking */}
          <div className="junit-new-exec-params">
            <label className="junit-new-exec-label">
              <span>Execution summary</span>
              <input
                className="junit-new-exec-input"
                type="text"
                placeholder="e.g. Nightly run 2026-06-24"
                value={summary}
                onChange={(e) => setSummary(e.target.value)}
                disabled={applying}
                autoFocus
              />
            </label>
            <label className="junit-new-exec-checkbox">
              <input
                type="checkbox"
                checked={createMissing}
                onChange={(e) => setCreateMissing(e.target.checked)}
                disabled={applying}
              />
              <span>Create missing tests</span>
            </label>
            <button
              className="btn"
              onClick={() => inputRef.current?.click()}
              disabled={applying || analyzing || !summaryTrimmed}
              title={summaryTrimmed ? "Choose a JUnit XML file to analyze" : "Enter an execution summary first"}
            >
              {analyzing ? "Analyzing…" : preview ? "Re-pick JUnit XML file…" : "Choose JUnit XML file…"}
            </button>
          </div>

          {/* Analyze error */}
          {analyzeError && (
            <div className="error-text">{analyzeError}</div>
          )}

          {/* Preview section */}
          {preview && (
            <>
              <p className="junit-new-exec-summary-line">
                Create execution{" "}
                <strong>«{summaryTrimmed || "(untitled)"}»</strong> with{" "}
                <strong>{preview.rows.length}</strong> test
                {preview.rows.length !== 1 ? "s" : ""} (
                {existingCount} existing{toCreateCount > 0 ? `, ${toCreateCount} new` : ""}
                ){". "}
                <span className={`run-badge run-pass`}>PASS</span> {passCount}{" / "}
                <span className={`run-badge run-fail`}>FAIL</span> {failCount}{" / "}
                <span className="muted">unset {unsetCount}</span>.
              </p>

              {preview.rows.length > 0 && (
                <section>
                  <h3 style={{ margin: "0.75rem 0 0.4rem" }}>
                    Rows ({preview.rows.length})
                  </h3>
                  <table className="board-table" style={{ fontSize: "0.85rem" }}>
                    <thead>
                      <tr>
                        <th>Testcase</th>
                        <th>Test</th>
                        <th>Summary</th>
                        <th>Result</th>
                      </tr>
                    </thead>
                    <tbody>
                      {preview.rows.map((row, i) => (
                        <tr key={i}>
                          <td className="mono" style={{ fontSize: "0.8rem" }}>
                            {row.testcase}
                          </td>
                          <td className="mono">
                            {row.create ? (
                              <span className="kind-badge kind-testexec">NEW</span>
                            ) : (
                              row.testKey
                            )}
                          </td>
                          <td>{row.summary}</td>
                          <td>
                            {row.result ? (
                              <span
                                className={`run-badge run-${row.result.toLowerCase()}`}
                              >
                                {row.result}
                              </span>
                            ) : (
                              <span className="muted">—</span>
                            )}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </section>
              )}

              {preview.rows.length === 0 && !skippedCount && (
                <p className="muted">No testcases found in the XML file.</p>
              )}

              {skippedCount > 0 && (
                <section>
                  <h3 style={{ margin: "0.75rem 0 0.4rem" }}>
                    Skipped ({skippedCount})
                  </h3>
                  <table className="board-table" style={{ fontSize: "0.85rem" }}>
                    <thead>
                      <tr>
                        <th>Testcase</th>
                        <th>Reason</th>
                      </tr>
                    </thead>
                    <tbody>
                      {preview.skipped.map((s, i) => (
                        <tr key={i}>
                          <td className="mono" style={{ fontSize: "0.8rem" }}>
                            {s.testcase}
                          </td>
                          <td className="muted">{s.reason}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </section>
              )}

              {applyError && (
                <div className="error-text" style={{ marginTop: "0.5rem" }}>
                  {applyError}
                </div>
              )}
            </>
          )}
        </div>

        <div className="pending-actions">
          <button className="btn" onClick={onCancel} disabled={applying}>
            Cancel
          </button>
          {preview && (
            <button
              className="btn btn-primary"
              onClick={applyImport}
              disabled={!canApply}
              title={
                !summaryTrimmed
                  ? "Enter an execution summary first"
                  : preview.rows.length === 0
                  ? "No rows to apply"
                  : "Create the execution and queue results for commit"
              }
            >
              {applying ? "Creating…" : "Create execution & apply"}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
