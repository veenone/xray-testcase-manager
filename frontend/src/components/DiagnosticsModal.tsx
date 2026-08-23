import { useCallback, useEffect, useState } from "react";
import { GetDiagnostics, ReadLog, ExportDiagnostics, errMsg } from "../api";
import type { Diagnostics } from "../api";
import { Modal } from "./Modal";

interface Props {
  onClose: () => void;
}

// DiagnosticsModal shows the environment summary and recent log, and exports a
// diagnostics bundle to disk (FR-12.4).
export function DiagnosticsModal({ onClose }: Props) {
  const [diag, setDiag] = useState<Diagnostics | null>(null);
  const [logText, setLogText] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [exportedPath, setExportedPath] = useState("");

  const load = useCallback(() => {
    setLoading(true);
    setError("");
    Promise.all([GetDiagnostics(), ReadLog(500)])
      .then(([d, l]) => {
        setDiag(d);
        setLogText(l);
      })
      .catch((e) => setError(errMsg(e)))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  async function exportDiagnostics() {
    setError("");
    try {
      const path = await ExportDiagnostics();
      setExportedPath(path);
    } catch (e) {
      setError(errMsg(e));
    }
  }

  return (
    <Modal onClose={onClose} className="modal pending-modal" labelledBy="diagnostics-title">
        <div className="pending-head">
          <h2 id="diagnostics-title">Diagnostics</h2>
          <button className="btn btn-ghost" onClick={onClose} title="Close">
            ✕
          </button>
        </div>

        <div className="diag-body">
          {error && <div className="error-text">{error}</div>}

          {diag && (
            <dl className="diag-fields">
              <dt>OS</dt>
              <dd>
                {diag.os} / {diag.arch}
              </dd>
              <dt>Go</dt>
              <dd>{diag.goVersion}</dd>
              <dt>Schema</dt>
              <dd>v{diag.schemaVersion}</dd>
              <dt>Profiles</dt>
              <dd>{diag.profileCount}</dd>
              <dt>Database</dt>
              <dd className="diag-path">{diag.dbPath}</dd>
              <dt>Log file</dt>
              <dd className="diag-path">{diag.logPath}</dd>
              {diag.startupError && (
                <>
                  <dt>Startup error</dt>
                  <dd className="error-text">{diag.startupError}</dd>
                </>
              )}
            </dl>
          )}

          <h4 className="diag-log-head">Recent log</h4>
          <pre className="diag-log">
            {loading ? "Loading…" : logText || "(empty)"}
          </pre>

          {exportedPath && (
            <p className="ok-text">
              ✓ Exported to <span className="mono">{exportedPath}</span>
            </p>
          )}
        </div>

        <div className="pending-actions">
          <p className="muted pending-footnote-inline">
            Share the exported file when reporting an issue.
          </p>
          <button className="btn" onClick={load} disabled={loading}>
            Refresh
          </button>
          <button className="btn btn-primary" onClick={exportDiagnostics}>
            Export…
          </button>
        </div>
    </Modal>
  );
}
