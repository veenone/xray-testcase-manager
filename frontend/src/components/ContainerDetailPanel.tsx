import { useEffect, useState } from "react";
import { GetContainerBoard, errMsg, BrowserOpenURL } from "../api";
import type { TestPlanBoard } from "../api";


interface Props {
  profileId: string;
  containerKey: string;
  kind: "plan" | "exec";
  jiraUrl?: string;
  onClose: () => void;
}

// ContainerDetailPanel renders a read-only detail panel for a Test Plan or
// Test Execution in the right sidebar of the Bugs view. It fetches the board
// for the container and shows a run-status histogram and the members table.
export function ContainerDetailPanel({ profileId, containerKey, kind, jiraUrl, onClose }: Props) {
  const [board, setBoard] = useState<TestPlanBoard | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  // Resizeable panel width — shares the same localStorage key as TestDetail so
  // all three sidebar panels (test case, Test Plan, Test Execution) open at the
  // same persisted width and dragging one updates the others.
  const [width, setWidth] = useState<number>(() => {
    const saved = Number(localStorage.getItem("xtm.detailWidth"));
    return saved >= 320 && saved <= 900 ? saved : 440;
  });
  useEffect(() => {
    localStorage.setItem("xtm.detailWidth", String(width));
  }, [width]);

  function startResize(e: React.MouseEvent) {
    e.preventDefault();
    const startX = e.clientX;
    const startW = width;
    // The panel is anchored to the right, so dragging left (negative delta)
    // widens it.
    const onMove = (ev: MouseEvent) =>
      setWidth(Math.min(900, Math.max(320, startW - (ev.clientX - startX))));
    const onUp = () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
    };
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
  }

  const kindLabel = kind === "plan" ? "Test Plan" : "Test Execution";
  const isDemo = /^(demo$|demo:|mock:)/i.test((jiraUrl ?? "").trim());
  const canLink = !!jiraUrl && !isDemo;

  useEffect(() => {
    if (!profileId || !containerKey) return;
    let cancelled = false;
    setLoading(true);
    setError("");
    setBoard(null);
    GetContainerBoard(profileId, containerKey)
      .then((b) => {
        if (!cancelled) {
          setBoard(b ?? null);
          setLoading(false);
        }
      })
      .catch((e) => {
        if (!cancelled) {
          setError(errMsg(e));
          setLoading(false);
        }
      });
    return () => { cancelled = true; };
  }, [profileId, containerKey]);

  function openInJira(key: string) {
    const base = (jiraUrl ?? "").trim().replace(/\/+$/, "");
    if (base && canLink && !key.startsWith("NEW-")) {
      BrowserOpenURL(`${base}/browse/${key}`);
    }
  }

  return (
    <div className="detail" style={{ width }}>
      <div
        className="detail-resizer"
        onMouseDown={startResize}
        title="Drag to resize"
      />
      <div className="detail-head">
        <div className="detail-head-id">
          {canLink && !containerKey.startsWith("NEW-") ? (
            <button
              className="detail-key detail-key-link mono"
              onClick={() => openInJira(containerKey)}
              title={`Open ${containerKey} in Jira`}
            >
              {containerKey}
              <span className="detail-key-ext"> ↗</span>
            </button>
          ) : (
            <span className="detail-key mono">{containerKey}</span>
          )}
          <span className="detail-head-status">{kindLabel}</span>
        </div>
        <div className="detail-head-actions">
          <button className="btn-icon detail-close" onClick={onClose} title="Close">✕</button>
        </div>
      </div>

      <div className="detail-body">
        {loading ? (
          <p className="muted">Loading…</p>
        ) : error ? (
          <p className="error-text">{error}</p>
        ) : !board ? (
          <p className="muted">No data.</p>
        ) : (
          <>
            <h3>{board.summary || "(no summary)"}</h3>

            {board.runCounts && board.runCounts.length > 0 && (
              <>
                <h4>Run status</h4>
                <div className="container-detail-run-counts">
                  {board.runCounts.map((b) => (
                    <span
                      key={b.label}
                      className={`run-badge run-${b.label.toLowerCase()}`}
                      title={`${b.label}: ${b.count}`}
                    >
                      {b.label} {b.count}
                    </span>
                  ))}
                </div>
              </>
            )}

            <h4>Members ({board.rows.length})</h4>
            {board.rows.length === 0 ? (
              <p className="muted">No members.</p>
            ) : (
              <table className="board-table">
                <thead>
                  <tr>
                    <th>Key</th>
                    <th>Summary</th>
                    <th>Status</th>
                    <th>Result</th>
                  </tr>
                </thead>
                <tbody>
                  {board.rows.map((r) => (
                    <tr key={r.testKey} className={r.isExternal ? "container-detail-external" : ""}>
                      <td>
                        <span className="mono" title={r.isExternal ? "External test" : undefined}>
                          {r.testKey}
                        </span>
                        {r.isExternal && (
                          <span className="muted" style={{ fontSize: "0.8em", marginLeft: 4 }}>ext</span>
                        )}
                      </td>
                      <td>{r.summary}</td>
                      <td>{r.status || <span className="muted">—</span>}</td>
                      <td>
                        {r.runStatus ? (
                          <span className={`run-badge run-${r.runStatus.toLowerCase()}`}>
                            {r.runStatus}
                          </span>
                        ) : (
                          <span className="muted">—</span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </>
        )}
      </div>
    </div>
  );
}
