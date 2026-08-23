import { useState } from "react";
import {
  PreviewImport,
  ImportTests,
  ExportImportTemplate,
  errMsg,
} from "../api";
import type { ImportMapping, ImportResult } from "../api";
import { useNotice } from "./useNotice";
import { Modal } from "./Modal";

interface Props {
  profileId: string;
  onComplete: () => void;
  onCancel: () => void;
}

const FIELDS: Array<{ key: keyof ImportMapping; label: string; required?: boolean }> = [
  { key: "summary", label: "Summary", required: true },
  { key: "description", label: "Description" },
  { key: "priority", label: "Priority" },
  { key: "labels", label: "Labels" },
  { key: "components", label: "Components" },
  { key: "folder", label: "Folder" },
  { key: "action", label: "Step action" },
  { key: "data", label: "Step data" },
  { key: "expected", label: "Step expected" },
  { key: "testType", label: "Test Type" },
  { key: "cucumberScenario", label: "Cucumber Scenario" },
  { key: "cucumberType", label: "Scenario Type" },
  { key: "genericDefinition", label: "Generic Test Definition" },
];

const EMPTY_MAPPING: ImportMapping = {
  summary: "",
  description: "",
  priority: "",
  labels: "",
  components: "",
  folder: "",
  action: "",
  data: "",
  expected: "",
  testType: "",
  cucumberScenario: "",
  cucumberType: "",
  genericDefinition: "",
};

// ImportTestsModal imports Tests from a CSV file (FR-10): pick a file, map
// columns to Test fields, validate (dry run), then import as local pending
// creates committed on the next sync.
export function ImportTestsModal({ profileId, onComplete, onCancel }: Props) {
  const [content, setContent] = useState(""); // base64 of the file
  const [isXlsx, setIsXlsx] = useState(false);
  const [fileName, setFileName] = useState("");
  const [headers, setHeaders] = useState<string[]>([]);
  const [rowCount, setRowCount] = useState(0);
  const [mapping, setMapping] = useState<ImportMapping>(EMPTY_MAPPING);
  const [validation, setValidation] = useState<ImportResult | null>(null);
  const [result, setResult] = useState<ImportResult | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // Unknown components are dropped by default: a component Jira does not have
  // fails the whole test at commit, and losing the label is easier to undo
  // than an import that cannot be committed (RND_P_4TFINT_05-340). Nothing is
  // dropped without the user seeing the warning first, see run() below.
  const [dropUnknown, setDropUnknown] = useState(true);
  const { notice, noticeUI } = useNotice();

  function pickFile(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;
    setFileName(file.name);
    const xlsx = file.name.toLowerCase().endsWith(".xlsx");
    setIsXlsx(xlsx);
    const reader = new FileReader();
    reader.onload = async () => {
      const b64 = arrayBufferToBase64(reader.result as ArrayBuffer);
      setContent(b64);
      setValidation(null);
      setResult(null);
      setError("");
      try {
        const pv = await PreviewImport(b64, xlsx);
        setHeaders(pv.headers ?? []);
        setRowCount(pv.rowCount ?? 0);
        setMapping(guessMapping(pv.headers ?? []));
      } catch (err) {
        setError(errMsg(err));
      }
    };
    reader.readAsArrayBuffer(file);
  }

  async function run(dryRun: boolean) {
    setBusy(true);
    setError("");
    try {
      // Import can be pressed without pressing Validate first, which would
      // drop a column's values with the user never having seen the warning.
      // Dry-run first in that case and stop on unknown components; pressing
      // Import again goes through, because validation is set by then.
      if (!dryRun && !validation) {
        const check = await ImportTests(
          profileId,
          content,
          isXlsx,
          mapping,
          true,
          dropUnknown,
        );
        if ((check.unknownComponents ?? []).length > 0) {
          setValidation(check);
          return;
        }
      }
      const r = await ImportTests(
        profileId,
        content,
        isXlsx,
        mapping,
        dryRun,
        dropUnknown,
      );
      if (dryRun) setValidation(r);
      else setResult(r);
    } catch (err) {
      setError(errMsg(err));
    } finally {
      setBusy(false);
    }
  }

  async function downloadTemplate() {
    try {
      const path = await ExportImportTemplate();
      if (path) await notice({ title: "Template saved", message: path });
    } catch (err) {
      await notice({ title: "Template export failed", message: errMsg(err), tone: "error" });
    }
  }

  const canRun = headers.length > 0 && mapping.summary !== "";

  return (
    <>
    <Modal onClose={onCancel} className="modal pending-modal" labelledBy="import-tests-title">
        <div className="pending-head">
          <h2 id="import-tests-title">Import tests (CSV or XLSX)</h2>
          <button className="btn btn-ghost" onClick={onCancel} title="Close">
            ✕
          </button>
        </div>

        <div className="bulk-body">
          {!result && (
            <>
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
                  {fileName} ({rowCount} row{rowCount === 1 ? "" : "s"})
                </p>
              )}

              {headers.length > 0 && (
                <div className="import-mapping">
                  {FIELDS.map((f) => (
                    <label key={f.key} className="bulk-row">
                      <span>
                        {f.label}
                        {f.required ? " *" : ""}
                      </span>
                      <select
                        value={mapping[f.key]}
                        onChange={(e) =>
                          setMapping((m) => ({ ...m, [f.key]: e.target.value }))
                        }
                      >
                        <option value="">(not mapped)</option>
                        {headers.map((h) => (
                          <option key={h} value={h}>
                            {h}
                          </option>
                        ))}
                      </select>
                    </label>
                  ))}
                </div>
              )}

              {validation && (
                <div className="import-validation">
                  <p className={validation.errors.length ? "warn-text" : "ok-text"}>
                    {validation.created} valid row
                    {validation.created === 1 ? "" : "s"}
                    {validation.errors.length > 0 &&
                      `, ${validation.errors.length} skipped`}
                    .
                  </p>
                  {validation.errors.length > 0 && (
                    <ul className="commit-fail-list">
                      {validation.errors.slice(0, 20).map((er, i) => (
                        <li key={i}>row {er.row}: {er.message}</li>
                      ))}
                    </ul>
                  )}
                  {(validation.unknownComponents ?? []).length > 0 && (
                    <div className="import-unknown-components">
                      <p className="warn-text">
                        ⚠ This project has no component named{" "}
                        {validation.unknownComponents.length === 1
                          ? ""
                          : "any of these:"}
                      </p>
                      <ul className="commit-fail-list">
                        {validation.unknownComponents.map((uc) => (
                          <li key={uc.name}>
                            <span className="mono">{uc.name}</span>
                            {uc.suggestion && (
                              <> · did you mean <span className="mono">{uc.suggestion}</span>?</>
                            )}
                          </li>
                        ))}
                      </ul>
                      <label className="import-unknown-choice">
                        <input
                          type="checkbox"
                          checked={dropUnknown}
                          onChange={(e) => setDropUnknown(e.target.checked)}
                        />
                        Import without these components
                      </label>
                      <p className="muted">
                        {dropUnknown
                          ? "The tests import with their other components. You can set the right one later with a bulk edit."
                          : "The tests import as-is and will fail on commit until the component exists in Jira."}
                      </p>
                    </div>
                  )}
                </div>
              )}

              {error && <div className="error-text">{error}</div>}
            </>
          )}

          {result && (
            <p className="ok-text">
              ✓ Imported {result.created} test{result.created === 1 ? "" : "s"} as
              pending creates
              {result.skipped > 0 ? ` (${result.skipped} skipped)` : ""}. Commit
              them from the Pending list.
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
                className="btn"
                onClick={() => run(true)}
                disabled={busy || !canRun}
              >
                Validate
              </button>
              <button
                className="btn btn-primary"
                onClick={() => run(false)}
                disabled={busy || !canRun}
              >
                {busy ? "Working…" : "Import"}
              </button>
            </>
          ) : (
            <button className="btn btn-primary" onClick={onComplete}>
              Done
            </button>
          )}
        </div>
    </Modal>
    {noticeUI}
    </>
  );
}

// arrayBufferToBase64 encodes a file's bytes for transport to the Go backend,
// which handles both CSV and binary XLSX uniformly.
function arrayBufferToBase64(buf: ArrayBuffer): string {
  const bytes = new Uint8Array(buf);
  let binary = "";
  const chunk = 0x8000;
  for (let i = 0; i < bytes.length; i += chunk) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunk));
  }
  return btoa(binary);
}

// guessMapping pre-selects columns whose header matches a field name.
function guessMapping(headers: string[]): ImportMapping {
  const find = (...names: string[]) =>
    headers.find((h) => names.includes(h.trim().toLowerCase())) ?? "";
  return {
    summary: find("summary"),
    description: find("description"),
    priority: find("priority"),
    labels: find("labels"),
    components: find("components"),
    folder: find("folder"),
    action: find("action"),
    data: find("data"),
    expected: find("expected"),
    testType: find("test type"),
    cucumberScenario: find("cucumber scenario"),
    cucumberType: find("cucumber test type", "scenario type"),
    genericDefinition: find("generic test definition", "generic definition"),
  };
}
