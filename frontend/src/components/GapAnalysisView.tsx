import { useMemo, useState } from "react";
import {
  AnalyzeGap,
  CreateTestsFromGaps,
  ExportGapReport,
  ExportImportTemplate,
  errMsg,
} from "../api";
import type { GapResult, GapTest } from "../api";
import { useNotice } from "./useNotice";

interface Props {
  profileId: string;
  onChanged: () => void;
}

function fileToBase64(file: File): Promise<{ b64: string; xlsx: boolean }> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(new Error("could not read file"));
    reader.onload = () => {
      const bytes = new Uint8Array(reader.result as ArrayBuffer);
      let binary = "";
      const chunk = 0x8000;
      for (let i = 0; i < bytes.length; i += chunk) {
        binary += String.fromCharCode(...bytes.subarray(i, i + chunk));
      }
      resolve({ b64: btoa(binary), xlsx: file.name.toLowerCase().endsWith(".xlsx") });
    };
    reader.readAsArrayBuffer(file);
  });
}

// GapAnalysisView compares a reference test list (the active project, or an
// uploaded file) against an uploaded target list by test summary, surfaces the
// gaps in both directions, lets the user add target-only gaps as new tests, and
// exports a management report. All feedback is themed (useNotice).
export function GapAnalysisView({ profileId, onChanged }: Props) {
  const [refSource, setRefSource] = useState<"project" | "file">("project");
  const [refFile, setRefFile] = useState<{ name: string; b64: string; xlsx: boolean } | null>(null);
  const [targetFile, setTargetFile] = useState<{ name: string; b64: string; xlsx: boolean } | null>(null);
  const [result, setResult] = useState<GapResult | null>(null);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [busy, setBusy] = useState(false);
  const { notice, noticeUI } = useNotice();

  const canRun = !!targetFile && (refSource === "project" || !!refFile);

  async function pick(setter: (v: { name: string; b64: string; xlsx: boolean }) => void, e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;
    try {
      const { b64, xlsx } = await fileToBase64(file);
      setter({ name: file.name, b64, xlsx });
      setResult(null);
      setSelected(new Set());
    } catch (err) {
      await notice({ title: "Couldn't read file", message: errMsg(err), tone: "error" });
    }
  }

  async function runAnalysis() {
    if (!targetFile) return;
    setBusy(true);
    try {
      const r = await AnalyzeGap(
        profileId,
        refSource,
        refFile?.b64 ?? "",
        refFile?.xlsx ?? false,
        targetFile.b64,
        targetFile.xlsx,
      );
      setResult(r);
      setSelected(new Set(r.missingFromReference.map((_, i) => i))); // default: all addable selected
    } catch (err) {
      await notice({ title: "Analysis failed", message: errMsg(err), tone: "error" });
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

  async function addSelected() {
    if (!result) return;
    const gaps: GapTest[] = result.missingFromReference.filter((_, i) => selected.has(i));
    if (gaps.length === 0) {
      await notice({ title: "Nothing selected", message: "Select at least one gap to add." });
      return;
    }
    setBusy(true);
    try {
      const res = await CreateTestsFromGaps(profileId, gaps);
      onChanged();
      await notice({
        title: "Gaps added",
        message: `Created ${res.created} test${res.created === 1 ? "" : "s"} as pending creates${res.skipped ? ` (${res.skipped} skipped)` : ""}. Commit them from the Pending list.`,
      });
    } catch (err) {
      await notice({ title: "Add failed", message: errMsg(err), tone: "error" });
    } finally {
      setBusy(false);
    }
  }

  async function exportReport() {
    if (!result) return;
    setBusy(true);
    try {
      // The generated GapResult model carries a convertValues method (nested
      // GapTest[]); the plain api.GapResult is structurally the same JSON, so
      // cast through unknown for the binding's typed arg.
      const path = await ExportGapReport(
        result as unknown as Parameters<typeof ExportGapReport>[0],
      );
      if (path) await notice({ title: "Report saved", message: path });
    } catch (err) {
      await notice({ title: "Export failed", message: errMsg(err), tone: "error" });
    } finally {
      setBusy(false);
    }
  }

  function toggle(i: number) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(i)) next.delete(i);
      else next.add(i);
      return next;
    });
  }

  const allSelected = useMemo(
    () => !!result && result.missingFromReference.length > 0 && selected.size === result.missingFromReference.length,
    [result, selected],
  );

  return (
    <div className="gap-view">
      <div className="gap-setup">
        <div className="gap-field">
          <span className="gap-label">Reference</span>
          <label className="gap-radio">
            <input type="radio" checked={refSource === "project"} onChange={() => { setRefSource("project"); setResult(null); }} />
            Active project tests
          </label>
          <label className="gap-radio">
            <input type="radio" checked={refSource === "file"} onChange={() => { setRefSource("file"); setResult(null); }} />
            Upload file
          </label>
          {refSource === "file" && (
            <span className="gap-file">
              <input type="file" accept=".csv,.xlsx,text/csv" onChange={(e) => pick(setRefFile, e)} />
              {refFile && <span className="muted">{refFile.name}</span>}
            </span>
          )}
        </div>

        <div className="gap-field">
          <span className="gap-label">Target</span>
          <input type="file" accept=".csv,.xlsx,text/csv" onChange={(e) => pick(setTargetFile, e)} />
          {targetFile && <span className="muted">{targetFile.name}</span>}
        </div>

        <div className="gap-actions">
          <button className="link-btn" onClick={downloadTemplate}>Download template</button>
          <button className="btn btn-primary" onClick={runAnalysis} disabled={busy || !canRun}>
            {busy ? "Working…" : "Run analysis"}
          </button>
        </div>
        <p className="muted gap-hint">Files must use the import template columns (Summary required). Comparison is by test summary.</p>
      </div>

      {result && (
        <div className="gap-results">
          <div className="gap-summary">
            <span>Matched: <b>{result.matched}</b></span>
            <span>Reference: {result.referenceCount}</span>
            <span>Target: {result.targetCount}</span>
            <button className="btn" onClick={exportReport} disabled={busy}>Export report</button>
          </div>

          <div className="gap-panel">
            <div className="gap-panel-head">
              <h4>Missing from reference ({result.missingFromReference.length})</h4>
              <span className="muted">in target, not reference — addable as tests</span>
            </div>
            {result.missingFromReference.length === 0 ? (
              <p className="muted">None — the reference already covers every target test.</p>
            ) : (
              <>
                <label className="gap-selectall">
                  <input
                    type="checkbox"
                    checked={allSelected}
                    onChange={() =>
                      setSelected(allSelected ? new Set() : new Set(result.missingFromReference.map((_, i) => i)))
                    }
                  />
                  Select all
                </label>
                <ul className="gap-list">
                  {result.missingFromReference.map((g, i) => (
                    <li key={i} className="gap-item">
                      <input type="checkbox" checked={selected.has(i)} onChange={() => toggle(i)} />
                      <span className="gap-item-summary">{g.summary}</span>
                      {g.description && <span className="muted gap-item-desc">{g.description}</span>}
                    </li>
                  ))}
                </ul>
                <button className="btn btn-primary" onClick={addSelected} disabled={busy || selected.size === 0}>
                  Add selected as tests ({selected.size})
                </button>
              </>
            )}
          </div>

          <div className="gap-panel">
            <div className="gap-panel-head">
              <h4>Missing from target ({result.missingFromTarget.length})</h4>
              <span className="muted">in reference, not target — report only</span>
            </div>
            {result.missingFromTarget.length === 0 ? (
              <p className="muted">None.</p>
            ) : (
              <ul className="gap-list">
                {result.missingFromTarget.map((g, i) => (
                  <li key={i} className="gap-item">
                    <span className="gap-item-summary">{g.summary}</span>
                    {g.description && <span className="muted gap-item-desc">{g.description}</span>}
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      )}
      {noticeUI}
    </div>
  );
}
