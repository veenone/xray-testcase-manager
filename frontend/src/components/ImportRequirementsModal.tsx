import { useEffect, useState } from "react";
import {
  AnalyzeRequirementImport,
  ExportRequirementImportTemplate,
  ImportRequirements,
  ListRequirementSources,
  errMsg,
} from "../api";
import type { RequirementImportPreview, RequirementImportRow, RequirementSource } from "../api";
import { useNotice } from "./useNotice";
import { fileToBase64 } from "../files";

interface Props {
  profileId: string;
  onComplete: () => void;
  onCancel: () => void;
}

// ImportRequirementsModal lets users upload a CSV/XLSX of requirements, preview
// which are new vs already existing (matched by summary), and import only the
// new ones as local pending creates committed to Jira on the next sync.
export function ImportRequirementsModal({ profileId, onComplete, onCancel }: Props) {
  const [sources, setSources] = useState<RequirementSource[]>([]);
  const [projectKey, setProjectKey] = useState("");
  const [issueType, setIssueType] = useState("");
  const [fileName, setFileName] = useState("");
  const [fileB64, setFileB64] = useState("");
  const [fileXlsx, setFileXlsx] = useState(false);
  const [preview, setPreview] = useState<RequirementImportPreview | null>(null);
  const [result, setResult] = useState<{ created: number; skippedExisting: number } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const { notice, noticeUI } = useNotice();

  useEffect(() => {
    if (!profileId) return;
    ListRequirementSources(profileId)
      .then((ss) => {
        setSources(ss ?? []);
        if (ss && ss.length > 0) {
          setProjectKey(ss[0].projectKey);
          const types = (ss[0].issueTypes ?? "").split(",").map((t) => t.trim()).filter(Boolean);
          setIssueType(types[0] ?? "");
        }
      })
      .catch(() => setSources([]));
  }, [profileId]);

  const selectedSource = sources.find((s) => s.projectKey === projectKey) ?? null;
  const issueTypeOptions = (selectedSource?.issueTypes ?? "")
    .split(",")
    .map((t) => t.trim())
    .filter(Boolean);

  async function pickFile(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;
    setFileName(file.name);
    setPreview(null);
    setResult(null);
    setError("");
    try {
      const { b64, xlsx } = await fileToBase64(file);
      setFileB64(b64);
      setFileXlsx(xlsx);
      setBusy(true);
      try {
        const pv = await AnalyzeRequirementImport(profileId, b64, xlsx);
        setPreview(pv);
      } catch (err) {
        setError(errMsg(err));
      } finally {
        setBusy(false);
      }
    } catch (err) {
      setError(errMsg(err));
    }
  }

  async function downloadTemplate() {
    try {
      const path = await ExportRequirementImportTemplate();
      if (path) await notice({ title: "Template saved", message: path });
    } catch (err) {
      await notice({ title: "Template export failed", message: errMsg(err), tone: "error" });
    }
  }

  async function doImport() {
    if (!fileB64 || !projectKey) return;
    setBusy(true);
    setError("");
    try {
      const r = await ImportRequirements(profileId, projectKey, issueType, fileB64, fileXlsx);
      setResult({ created: r.created, skippedExisting: r.skippedExisting });
    } catch (err) {
      setError(errMsg(err));
    } finally {
      setBusy(false);
    }
  }

  const newCount = preview?.newCount ?? 0;
  const canImport = !!fileB64 && !!projectKey && newCount > 0 && !busy;

  return (
    <div className="modal-overlay" onClick={onCancel}>
      <div className="modal pending-modal" onClick={(e) => e.stopPropagation()}>
        <div className="pending-head">
          <h2>Import requirements (CSV or XLSX)</h2>
          <button className="btn btn-ghost" onClick={onCancel} title="Close">
            ✕
          </button>
        </div>

        <div className="bulk-body">
          {!result && (
            <>
              <div className="import-row">
                <label>
                  Project *&nbsp;
                  <select
                    className="detail-input"
                    value={projectKey}
                    onChange={(e) => {
                      setProjectKey(e.target.value);
                      const src = sources.find((s) => s.projectKey === e.target.value);
                      const types = (src?.issueTypes ?? "").split(",").map((t) => t.trim()).filter(Boolean);
                      setIssueType(types[0] ?? "");
                    }}
                  >
                    <option value="">(select project)</option>
                    {sources.map((s) => (
                      <option key={s.projectKey} value={s.projectKey}>{s.projectKey}</option>
                    ))}
                  </select>
                </label>
                &nbsp;
                <label>
                  Issue type&nbsp;
                  <select
                    className="detail-input"
                    value={issueType}
                    onChange={(e) => setIssueType(e.target.value)}
                    disabled={issueTypeOptions.length === 0}
                  >
                    {issueTypeOptions.length === 0 && <option value="">(any)</option>}
                    {issueTypeOptions.map((t) => (
                      <option key={t} value={t}>{t}</option>
                    ))}
                  </select>
                </label>
              </div>

              <div className="import-row">
                <input
                  type="file"
                  accept=".csv,.xlsx,text/csv"
                  onChange={pickFile}
                />
                <button className="link-btn" onClick={downloadTemplate}>
                  Download template
                </button>
              </div>

              {fileName && (
                <p className="muted">
                  {fileName}
                  {preview && (
                    <> ({preview.rows.length} row{preview.rows.length === 1 ? "" : "s"}
                      {": "}{preview.newCount} new, {preview.existingCount} existing)</>
                  )}
                </p>
              )}

              {busy && !preview && <p className="muted">Analyzing…</p>}

              {preview && preview.rows.length > 0 && (
                <div className="import-validation">
                  <table className="board-table">
                    <thead>
                      <tr>
                        <th>Status</th>
                        <th>Summary</th>
                        <th>Priority</th>
                        <th>Components</th>
                      </tr>
                    </thead>
                    <tbody>
                      {preview.rows.map((row: RequirementImportRow, i: number) => (
                        <tr key={i} className={row.status === "existing" ? "muted" : ""}>
                          <td>
                            <span className={row.status === "new" ? "ok-text" : "muted"}>
                              {row.status === "new" ? "NEW" : "existing"}
                            </span>
                          </td>
                          <td>{row.summary || <em>(blank)</em>}</td>
                          <td>{row.priority || "—"}</td>
                          <td>{row.components || "—"}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}

              {error && <div className="error-text">{error}</div>}
            </>
          )}

          {result && (
            <p className="ok-text">
              Imported {result.created} requirement{result.created === 1 ? "" : "s"} as
              pending creates
              {result.skippedExisting > 0
                ? ` (${result.skippedExisting} already existed, skipped)`
                : ""}
              . Commit them from the Pending list.
            </p>
          )}
        </div>

        <div className="pending-actions">
          {!result ? (
            <>
              <button className="btn" onClick={onCancel} disabled={busy}>
                Cancel
              </button>
              <button
                className="btn btn-primary"
                onClick={doImport}
                disabled={!canImport}
              >
                {busy ? "Working…" : `Import ${newCount} new`}
              </button>
            </>
          ) : (
            <button className="btn btn-primary" onClick={onComplete}>
              Done
            </button>
          )}
        </div>
      </div>
      {noticeUI}
    </div>
  );
}
