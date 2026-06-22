import { useEffect, useMemo, useState } from "react";
import {
  AnalyzeGap,
  CreateTestsFromGaps,
  ExportGapReport,
  ExportImportTemplate,
  ExportSummaryTemplate,
  ExportSummaryFolderTemplate,
  errMsg,
} from "../api";
import type { GapResult, GapTest } from "../api";
import { useNotice } from "./useNotice";
import { Pager } from "./Pager";

interface Props {
  profileId: string;
  onChanged: () => void;
}

type Picked = { name: string; b64: string; xlsx: boolean };
type TemplateKind = "full" | "summary" | "folder";

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

// GapList renders a GapTest list with a pinned (static) pager below a scrollable
// body, 15 per page by default. When selectable, it shows per-row checkboxes and
// a select-all; selection is by index into the full list (stable across pages).
function GapList({
  items,
  emptyText,
  selectable,
  selected,
  onToggle,
  onToggleAll,
}: {
  items: GapTest[];
  emptyText: string;
  selectable?: boolean;
  selected?: Set<number>;
  onToggle?: (i: number) => void;
  onToggleAll?: (all: boolean) => void;
}) {
  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState(15);
  useEffect(() => {
    setPage(0);
  }, [items]);

  if (items.length === 0) return <p className="muted gap-empty">{emptyText}</p>;

  const totalPages = Math.max(1, Math.ceil(items.length / pageSize));
  const safePage = Math.min(Math.max(0, page), totalPages - 1);
  const start = safePage * pageSize;
  const pageItems = items.slice(start, start + pageSize);
  const allSelected = !!selected && selected.size === items.length;

  return (
    <>
      {selectable && (
        <label className="gap-selectall">
          <input
            type="checkbox"
            checked={allSelected}
            onChange={() => onToggleAll?.(!allSelected)}
          />
          Select all ({items.length})
        </label>
      )}
      <ul className="gap-list">
        {pageItems.map((g, j) => {
          const i = start + j;
          return (
            <li key={i} className={`gap-item${selectable ? "" : " gap-item-static"}`}>
              {selectable && (
                <input
                  type="checkbox"
                  checked={selected?.has(i) ?? false}
                  onChange={() => onToggle?.(i)}
                />
              )}
              <div className="gap-item-text">
                <span className="gap-item-summary">{g.summary}</span>
                {g.description && <span className="muted gap-item-desc">{g.description}</span>}
                {g.folder && <span className="muted gap-item-folder">{g.folder}</span>}
              </div>
            </li>
          );
        })}
      </ul>
      <Pager
        compact
        page={safePage}
        pageSize={pageSize}
        total={items.length}
        onPage={setPage}
        onPageSize={(n) => {
          setPageSize(n);
          setPage(0);
        }}
      />
    </>
  );
}

// GapAnalysisView compares a reference test list (the active project, or an
// uploaded file) against an uploaded target list by test summary, with optional
// three-way diff against the project and folder-mismatch reporting. Gaps are
// shown side by side, addable as new tests, and exportable as a CSV/Excel report.
export function GapAnalysisView({ profileId, onChanged }: Props) {
  const [refSource, setRefSource] = useState<"project" | "file">("project");
  const [templateKind, setTemplateKind] = useState<TemplateKind>("full");
  const [compareBy, setCompareBy] = useState<"summary" | "summaryFolder">("summary");
  const [threeWay, setThreeWay] = useState(false);
  const [refFile, setRefFile] = useState<Picked | null>(null);
  const [targetFile, setTargetFile] = useState<Picked | null>(null);
  const [result, setResult] = useState<GapResult | null>(null);
  const [selRef, setSelRef] = useState<Set<number>>(new Set());
  const [selProj, setSelProj] = useState<Set<number>>(new Set());
  const [busy, setBusy] = useState(false);
  const { notice, noticeUI } = useNotice();

  const canRun = !!targetFile && (refSource === "project" || !!refFile);
  const isThreeWay = refSource === "file" && threeWay;

  function resetResult() {
    setResult(null);
    setSelRef(new Set());
    setSelProj(new Set());
  }

  async function pick(setter: (v: Picked) => void, e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;
    try {
      const { b64, xlsx } = await fileToBase64(file);
      setter({ name: file.name, b64, xlsx });
      resetResult();
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
        isThreeWay,
        compareBy === "summaryFolder",
      );
      setResult(r);
      setSelRef(new Set(r.missingFromReference.map((_, i) => i)));
      setSelProj(new Set(r.missingFromProject.map((_, i) => i)));
    } catch (err) {
      await notice({ title: "Analysis failed", message: errMsg(err), tone: "error" });
    } finally {
      setBusy(false);
    }
  }

  async function downloadTemplate() {
    try {
      const path =
        templateKind === "summary"
          ? await ExportSummaryTemplate()
          : templateKind === "folder"
            ? await ExportSummaryFolderTemplate()
            : await ExportImportTemplate();
      if (path) await notice({ title: "Template saved", message: path });
    } catch (err) {
      await notice({ title: "Template export failed", message: errMsg(err), tone: "error" });
    }
  }

  async function addTests(gaps: GapTest[], successLabel: string) {
    if (gaps.length === 0) {
      await notice({ title: "Nothing selected", message: "Select at least one item to add." });
      return;
    }
    setBusy(true);
    try {
      const res = await CreateTestsFromGaps(profileId, gaps);
      onChanged();
      await notice({
        title: successLabel,
        message: `Created ${res.created} test${res.created === 1 ? "" : "s"} as pending creates${res.skipped ? ` (${res.skipped} skipped)` : ""}. Commit them from the Pending list.`,
      });
    } catch (err) {
      await notice({ title: "Add failed", message: errMsg(err), tone: "error" });
    } finally {
      setBusy(false);
    }
  }

  async function exportReport(format: "csv" | "xlsx") {
    if (!result) return;
    setBusy(true);
    try {
      // The generated GapResult model carries a convertValues method (nested
      // arrays); the plain api.GapResult is the same JSON, so cast through
      // unknown for the binding's typed arg.
      const path = await ExportGapReport(
        result as unknown as Parameters<typeof ExportGapReport>[0],
        format,
      );
      if (path) await notice({ title: "Report saved", message: path });
    } catch (err) {
      await notice({ title: "Export failed", message: errMsg(err), tone: "error" });
    } finally {
      setBusy(false);
    }
  }

  function toggleIn(set: Set<number>, setter: (s: Set<number>) => void, i: number) {
    const next = new Set(set);
    if (next.has(i)) next.delete(i);
    else next.add(i);
    setter(next);
  }
  function toggleAll(items: GapTest[], setter: (s: Set<number>) => void, all: boolean) {
    setter(all ? new Set(items.map((_, i) => i)) : new Set());
  }

  const selectedRefGaps = useMemo(
    () => (result ? result.missingFromReference.filter((_, i) => selRef.has(i)) : []),
    [result, selRef],
  );
  const selectedProjGaps = useMemo(
    () => (result ? result.missingFromProject.filter((_, i) => selProj.has(i)) : []),
    [result, selProj],
  );

  return (
    <div className="gap-view">
      <div className="gap-inner">
        <section className="gap-card gap-setup">
          <div className="gap-row">
            <span className="gap-label">Reference</span>
            <div className="gap-seg">
              <button
                className={`seg-btn${refSource === "project" ? " seg-btn-active" : ""}`}
                onClick={() => { setRefSource("project"); resetResult(); }}
              >
                Active project
              </button>
              <button
                className={`seg-btn${refSource === "file" ? " seg-btn-active" : ""}`}
                onClick={() => { setRefSource("file"); resetResult(); }}
              >
                Upload file
              </button>
            </div>
            {refSource === "file" && (
              <label className="gap-file">
                <input type="file" accept=".csv,.xlsx,text/csv" onChange={(e) => pick(setRefFile, e)} />
                {refFile && <span className="muted gap-file-name">{refFile.name}</span>}
              </label>
            )}
          </div>

          <div className="gap-row">
            <span className="gap-label">Target</span>
            <label className="gap-file">
              <input type="file" accept=".csv,.xlsx,text/csv" onChange={(e) => pick(setTargetFile, e)} />
              {targetFile && <span className="muted gap-file-name">{targetFile.name}</span>}
            </label>
          </div>

          <div className="gap-row">
            <span className="gap-label">Compare by</span>
            <div className="gap-seg">
              <button
                className={`seg-btn${compareBy === "summary" ? " seg-btn-active" : ""}`}
                onClick={() => { setCompareBy("summary"); resetResult(); }}
              >
                Summary
              </button>
              <button
                className={`seg-btn${compareBy === "summaryFolder" ? " seg-btn-active" : ""}`}
                onClick={() => { setCompareBy("summaryFolder"); resetResult(); }}
              >
                Summary + folder
              </button>
            </div>
            {refSource === "file" && (
              <label className="gap-check">
                <input
                  type="checkbox"
                  checked={threeWay}
                  onChange={(e) => { setThreeWay(e.target.checked); resetResult(); }}
                />
                Three-way (also complete the project)
              </label>
            )}
          </div>

          <div className="gap-row gap-row-template">
            <span className="gap-label">Template</span>
            <div className="gap-seg">
              <button className={`seg-btn${templateKind === "full" ? " seg-btn-active" : ""}`} onClick={() => setTemplateKind("full")}>Full</button>
              <button className={`seg-btn${templateKind === "summary" ? " seg-btn-active" : ""}`} onClick={() => setTemplateKind("summary")}>Summary only</button>
              <button className={`seg-btn${templateKind === "folder" ? " seg-btn-active" : ""}`} onClick={() => setTemplateKind("folder")}>Summary + folder</button>
            </div>
            <button className="link-btn" onClick={downloadTemplate}>Download template</button>
          </div>

          <div className="gap-run">
            <p className="muted gap-hint">
              Matched by test summary. Files use the import template columns (Summary required);
              summary-only adds default Priority/Description on create.
              {compareBy === "summaryFolder" && " Folder differences are reported separately."}
            </p>
            <button className="btn btn-primary" onClick={runAnalysis} disabled={busy || !canRun}>
              {busy ? "Working…" : "Run analysis"}
            </button>
          </div>
        </section>

        {result && (
          <>
            <section className="gap-card gap-dash">
              <div className="gap-tile">
                <span className="gap-tile-n">{result.matched}</span>
                <span className="gap-tile-l">Matched</span>
              </div>
              <div className="gap-tile">
                <span className="gap-tile-n">{result.missingFromTarget.length}</span>
                <span className="gap-tile-l">Orphaned in reference</span>
              </div>
              <div className="gap-tile">
                <span className="gap-tile-n">{result.missingFromReference.length}</span>
                <span className="gap-tile-l">Orphaned in target</span>
              </div>
              <div className="gap-tile gap-tile-total">
                <span className="gap-tile-n">{result.missingFromReference.length + result.missingFromTarget.length}</span>
                <span className="gap-tile-l">Total gap</span>
              </div>
              {result.threeWay && (
                <div className="gap-tile gap-tile-proj">
                  <span className="gap-tile-n">{result.missingFromProject.length}</span>
                  <span className="gap-tile-l">Missing from project</span>
                </div>
              )}
              <div className="gap-dash-actions">
                <button className="btn" onClick={() => exportReport("csv")} disabled={busy}>Export CSV</button>
                <button className="btn" onClick={() => exportReport("xlsx")} disabled={busy}>Export Excel</button>
              </div>
            </section>

            <div className="gap-cols">
              <section className="gap-card gap-panel">
                <div className="gap-panel-head">
                  <span className="gap-dir gap-dir-in">Target&nbsp;→&nbsp;Reference</span>
                  <span className="gap-count">{result.missingFromReference.length}</span>
                </div>
                <p className="muted gap-panel-sub">In the target, missing from the reference — addable as tests.</p>
                <GapList
                  items={result.missingFromReference}
                  emptyText="None — the reference already covers every target test."
                  selectable
                  selected={selRef}
                  onToggle={(i) => toggleIn(selRef, setSelRef, i)}
                  onToggleAll={(all) => toggleAll(result.missingFromReference, setSelRef, all)}
                />
                {result.missingFromReference.length > 0 && (
                  <div className="gap-panel-actions">
                    <button
                      className="btn btn-primary"
                      onClick={() => addTests(selectedRefGaps, "Gaps added")}
                      disabled={busy || selRef.size === 0}
                    >
                      Add selected as tests ({selRef.size})
                    </button>
                  </div>
                )}
              </section>

              <section className="gap-card gap-panel">
                <div className="gap-panel-head">
                  <span className="gap-dir gap-dir-out">Reference&nbsp;→&nbsp;Target</span>
                  <span className="gap-count">{result.missingFromTarget.length}</span>
                </div>
                <p className="muted gap-panel-sub">In the reference, missing from the target — report only.</p>
                <GapList items={result.missingFromTarget} emptyText="None." />
              </section>
            </div>

            {result.threeWay && (
              <section className="gap-card gap-panel">
                <div className="gap-panel-head">
                  <span className="gap-dir gap-dir-proj">Reference&nbsp;∪&nbsp;Target&nbsp;→&nbsp;Project</span>
                  <span className="gap-count">{result.missingFromProject.length}</span>
                </div>
                <p className="muted gap-panel-sub">
                  In the reference or target but not yet in the project — add these to complete the project (committed to Jira on sync).
                </p>
                <GapList
                  items={result.missingFromProject}
                  emptyText="None — the project already contains every reference/target test."
                  selectable
                  selected={selProj}
                  onToggle={(i) => toggleIn(selProj, setSelProj, i)}
                  onToggleAll={(all) => toggleAll(result.missingFromProject, setSelProj, all)}
                />
                {result.missingFromProject.length > 0 && (
                  <div className="gap-panel-actions">
                    <button
                      className="btn btn-primary"
                      onClick={() => addTests(selectedProjGaps, "Added to project")}
                      disabled={busy || selProj.size === 0}
                    >
                      Add selected to project ({selProj.size})
                    </button>
                  </div>
                )}
              </section>
            )}

            {result.folderMismatches.length > 0 && (
              <section className="gap-card gap-panel">
                <div className="gap-panel-head">
                  <span className="gap-dir gap-dir-folder">Folder mismatches</span>
                  <span className="gap-count">{result.folderMismatches.length}</span>
                </div>
                <p className="muted gap-panel-sub">Matched by summary, but the folder location differs.</p>
                <table className="board-table gap-folder-table">
                  <thead>
                    <tr><th>Summary</th><th>Reference folder</th><th>Target folder</th></tr>
                  </thead>
                  <tbody>
                    {result.folderMismatches.map((m, i) => (
                      <tr key={i}>
                        <td>{m.summary}</td>
                        <td className="muted">{m.referenceFolder || "—"}</td>
                        <td className="muted">{m.targetFolder || "—"}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </section>
            )}
          </>
        )}
      </div>
      {noticeUI}
    </div>
  );
}
