import { useEffect, useRef, useState } from "react";
import { AnalyzeJUnitImport, ApplyJUnitImport, errMsg } from "../api";
import type { JUnitImportPreview, JUnitMatch } from "../api";
import { fileToBase64 } from "../files";

interface Props {
  profileId: string;
  execKey: string;
  onCancel: () => void;
  onApplied: (succeeded: number, failed: number) => void;
}

// JUnitImportModal reads a JUnit XML file, calls AnalyzeJUnitImport to preview
// which testcases match execution members, then applies with ApplyJUnitImport.
export function JUnitImportModal({ profileId, execKey, onCancel, onApplied }: Props) {
  const [preview, setPreview] = useState<JUnitImportPreview | null>(null);
  const [analyzeError, setAnalyzeError] = useState("");
  const [applying, setApplying] = useState(false);
  const [applyError, setApplyError] = useState("");
  // filePickedRef tracks whether the user selected a file; using a ref so the
  // window-focus cancel-detection handler always reads the current value and
  // does not close over a stale initial false.
  const filePickedRef = useRef(false);
  const inputRef = useRef<HTMLInputElement>(null);

  // Open the native file picker as soon as the modal mounts.
  useEffect(() => {
    if (inputRef.current) {
      inputRef.current.click();
    }
    // Detect file-picker cancel: when window regains focus without a file being
    // picked, close the modal after a short delay (the change event fires first
    // if a file is chosen, setting filePickedRef.current = true before this runs).
    function onFocus() {
      setTimeout(() => {
        if (!filePickedRef.current) onCancel();
      }, 300);
    }
    window.addEventListener("focus", onFocus, { once: true });
    return () => window.removeEventListener("focus", onFocus);
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  async function onFileChange(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) {
      onCancel();
      return;
    }
    filePickedRef.current = true;
    setAnalyzeError("");
    try {
      const { b64 } = await fileToBase64(file);
      const result = await AnalyzeJUnitImport(profileId, execKey, b64);
      setPreview(result);
    } catch (err) {
      setAnalyzeError(errMsg(err));
    }
  }

  async function applyImport(matches: JUnitMatch[]) {
    setApplying(true);
    setApplyError("");
    try {
      const res = await ApplyJUnitImport(profileId, execKey, matches);
      onApplied(res.succeeded?.length ?? 0, res.failed?.length ?? 0);
    } catch (err) {
      setApplyError(errMsg(err));
      setApplying(false);
    }
  }

  const matchedCount = preview?.matched?.length ?? 0;
  const skippedCount = preview?.skipped?.length ?? 0;

  return (
    <div className="modal-overlay" onClick={onCancel}>
      <div className="modal bulk-modal" onClick={(e) => e.stopPropagation()}>
        {/* Hidden file input — triggered on mount */}
        <input
          ref={inputRef}
          type="file"
          accept=".xml"
          style={{ display: "none" }}
          onChange={onFileChange}
        />

        <div className="pending-head">
          <h2>Import results (JUnit XML)</h2>
          <button className="btn btn-ghost" onClick={onCancel} title="Close">
            ✕
          </button>
        </div>

        <div className="bulk-body">
          {/* Analyzing phase: no preview yet and no error */}
          {!preview && !analyzeError && (
            <p className="muted">Analyzing…</p>
          )}

          {/* Parse / analyze error */}
          {analyzeError && (
            <div className="error-text">{analyzeError}</div>
          )}

          {/* Preview phase */}
          {preview && (
            <>
              <p>
                Parsed <strong>{preview.total}</strong> testcase{preview.total !== 1 ? "s" : ""}.{" "}
                <strong>{matchedCount}</strong> will be applied,{" "}
                <strong>{skippedCount}</strong> skipped.
              </p>

              {matchedCount > 0 && (
                <section>
                  <h3 style={{ margin: "0.75rem 0 0.4rem" }}>Matched ({matchedCount})</h3>
                  <table className="board-table" style={{ fontSize: "0.85rem" }}>
                    <thead>
                      <tr>
                        <th>Testcase</th>
                        <th>Test</th>
                        <th>Summary</th>
                        <th>Current</th>
                        <th>New result</th>
                      </tr>
                    </thead>
                    <tbody>
                      {preview.matched.map((m, i) => (
                        <tr key={i}>
                          <td className="mono" style={{ fontSize: "0.8rem" }}>{m.testcase}</td>
                          <td className="mono">{m.testKey}</td>
                          <td>{m.summary}</td>
                          <td>
                            {m.currentRun ? (
                              <span className={`run-badge run-${m.currentRun.toLowerCase()}`}>
                                {m.currentRun}
                              </span>
                            ) : (
                              <span className="muted">—</span>
                            )}
                          </td>
                          <td>
                            <span className={`run-badge run-${m.result.toLowerCase()}`}>
                              {m.result}
                            </span>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </section>
              )}

              {skippedCount > 0 && (
                <section>
                  <h3 style={{ margin: "0.75rem 0 0.4rem" }}>Skipped ({skippedCount})</h3>
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
                          <td className="mono" style={{ fontSize: "0.8rem" }}>{s.testcase}</td>
                          <td className="muted">{s.reason}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </section>
              )}

              {matchedCount === 0 && skippedCount === 0 && (
                <p className="muted">No testcases found in the XML file.</p>
              )}

              {applyError && <div className="error-text" style={{ marginTop: "0.5rem" }}>{applyError}</div>}
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
              onClick={() => applyImport(preview.matched)}
              disabled={applying || matchedCount === 0}
            >
              {applying ? "Applying…" : `Apply ${matchedCount} result${matchedCount !== 1 ? "s" : ""}`}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
