import { useEffect, useState } from "react";
import {
  ListConnections,
  ComputeBridgeGap,
  GetBridgeMapping,
  SaveBridgeMapping,
  PublishToTarget,
  errMsg,
} from "../api";
import type {
  Connection,
  BridgeGap,
  BridgeMapping,
  BridgePublishResult,
} from "../api";

interface Props {
  // The active workspace (P6.3): source/target connections are both scoped
  // to this workspace's connection list (ListConnections(activeId)).
  activeId: string;
  onClose: () => void;
  // Lets step 1 hand off to the Connections manager (B6a) when the
  // workspace doesn't have a second connection to bridge to yet.
  onOpenConnections: () => void;
}

type Step = "select" | "gap" | "map" | "publish";

const STEPS: Array<{ key: Step; label: string }> = [
  { key: "select", label: "Select" },
  { key: "gap", label: "Gap report" },
  { key: "map", label: "Mapping" },
  { key: "publish", label: "Publish" },
];

const BACKEND_LABEL: Record<string, string> = { xray: "Xray", kiwi: "Kiwi" };
const ROLE_LABEL: Record<string, string> = {
  source: "Source",
  target: "Target",
  both: "Both",
};

const SEVERITY_LABEL: Record<string, string> = {
  blocking: "Blocking",
  lossy: "Lossy",
  info: "Info",
};

function connLabel(c: Connection): string {
  return `${c.name} (${BACKEND_LABEL[c.backend] ?? c.backend})`;
}

// BridgeWizard walks a user through publishing/migrating the active
// workspace's tests from a source connection to a target connection
// (Phase 6 bridge, B6b): pick connections, review the capability-gap
// report, adjust the status/step mapping, then run the publish and show
// the result. Only PublishToTarget writes anything — the source is never
// mutated by this wizard.
export function BridgeWizard({ activeId, onClose, onOpenConnections }: Props) {
  const [step, setStep] = useState<Step>("select");

  // Step 1: select
  const [connections, setConnections] = useState<Connection[]>([]);
  const [connLoading, setConnLoading] = useState(true);
  const [connError, setConnError] = useState("");
  const [sourceId, setSourceId] = useState("");
  const [targetId, setTargetId] = useState("");

  // Step 2: gap report
  const [gaps, setGaps] = useState<BridgeGap[] | null>(null);
  const [gapLoading, setGapLoading] = useState(false);
  const [gapError, setGapError] = useState("");
  const [lossAck, setLossAck] = useState(false);

  // Step 3: mapping
  const [mapping, setMapping] = useState<BridgeMapping | null>(null);
  const [mapLoading, setMapLoading] = useState(false);
  const [mapError, setMapError] = useState("");
  const [mapSaving, setMapSaving] = useState(false);

  // Step 4: publish
  const [publishing, setPublishing] = useState(false);
  const [publishError, setPublishError] = useState("");
  const [result, setResult] = useState<BridgePublishResult | null>(null);
  const [showFailures, setShowFailures] = useState(false);

  // Load the workspace's connections once and pick sensible defaults: the
  // primary connection (id === activeId, which holds the pulled data) as
  // source, the first other target/both connection as target.
  useEffect(() => {
    let cancelled = false;
    setConnLoading(true);
    setConnError("");
    ListConnections(activeId)
      .then((list) => {
        if (cancelled) return;
        setConnections(list);
        const src = list.find((c) => c.id === activeId)?.id ?? list[0]?.id ?? "";
        setSourceId(src);
        const tgt =
          list.find(
            (c) => c.id !== src && (c.role === "target" || c.role === "both"),
          )?.id ?? "";
        setTargetId(tgt);
      })
      .catch((e) => {
        if (!cancelled) setConnError(errMsg(e));
      })
      .finally(() => {
        if (!cancelled) setConnLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [activeId]);

  useEffect(() => {
    if (step !== "gap") return;
    let cancelled = false;
    setGapLoading(true);
    setGapError("");
    setGaps(null);
    setLossAck(false);
    ComputeBridgeGap(sourceId, targetId)
      .then((g) => {
        if (!cancelled) setGaps(g);
      })
      .catch((e) => {
        if (!cancelled) setGapError(errMsg(e));
      })
      .finally(() => {
        if (!cancelled) setGapLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [step, sourceId, targetId]);

  useEffect(() => {
    if (step !== "map") return;
    let cancelled = false;
    setMapLoading(true);
    setMapError("");
    setMapping(null);
    GetBridgeMapping(activeId, sourceId, targetId)
      .then((m) => {
        if (!cancelled) setMapping(m);
      })
      .catch((e) => {
        if (!cancelled) setMapError(errMsg(e));
      })
      .finally(() => {
        if (!cancelled) setMapLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [step, activeId, sourceId, targetId]);

  const source = connections.find((c) => c.id === sourceId) ?? null;
  const target = connections.find((c) => c.id === targetId) ?? null;

  const canProceedSelect =
    !connLoading && !!sourceId && !!targetId && sourceId !== targetId;

  const blocking = (gaps ?? []).filter((g) => g.severity === "blocking");
  const lossy = (gaps ?? []).filter((g) => g.severity === "lossy");
  const info = (gaps ?? []).filter((g) => g.severity === "info");
  const canProceedGap =
    gaps !== null && blocking.length === 0 && (lossy.length === 0 || lossAck);

  function handleSelectNext() {
    if (!canProceedSelect) return;
    setStep("gap");
  }

  function handleGapNext() {
    if (!canProceedGap) return;
    setStep("map");
  }

  function setStatusTarget(sourceStatus: string, value: string) {
    setMapping((m) =>
      m ? { ...m, statusMap: { ...m.statusMap, [sourceStatus]: value } } : m,
    );
  }

  function setStepMode(mode: string) {
    setMapping((m) => (m ? { ...m, stepMode: mode } : m));
  }

  async function handleMapNext() {
    if (!mapping) return;
    setMapSaving(true);
    setMapError("");
    try {
      await SaveBridgeMapping(activeId, sourceId, targetId, mapping);
      setStep("publish");
    } catch (e) {
      setMapError(errMsg(e));
    } finally {
      setMapSaving(false);
    }
  }

  async function runPublish() {
    setPublishing(true);
    setPublishError("");
    try {
      const r = await PublishToTarget(activeId, sourceId, targetId);
      setResult(r);
    } catch (e) {
      setPublishError(errMsg(e));
    } finally {
      setPublishing(false);
    }
  }

  function handleBack() {
    if (step === "gap") setStep("select");
    else if (step === "map") setStep("gap");
    else if (step === "publish") {
      // Drop the prior run's result/error so re-entering the publish step
      // shows a fresh "Publish tests to target" button, not a stale summary.
      setResult(null);
      setPublishError("");
      setShowFailures(false);
      setStep("map");
    }
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal pending-modal bridge-modal" onClick={(e) => e.stopPropagation()}>
        <div className="pending-head">
          <h2>Bridge tests to another connection</h2>
          <button className="btn btn-ghost" onClick={onClose} title="Close" aria-label="Close">
            ✕
          </button>
        </div>

        <div className="bridge-steps">
          {STEPS.map((s, i) => (
            <span
              key={s.key}
              className={`bridge-step${s.key === step ? " bridge-step-active" : ""}`}
            >
              {i + 1}. {s.label}
            </span>
          ))}
        </div>

        <div className="bulk-body bridge-body">
          {step === "select" && (
            <>
              {connLoading && <p className="muted">Loading connections…</p>}
              {connError && <div className="error-text">{connError}</div>}
              {!connLoading && !connError && (
                <>
                  <label className="bulk-row">
                    <span>Source (has the pulled data)</span>
                    <select value={sourceId} onChange={(e) => setSourceId(e.target.value)}>
                      {connections.map((c) => (
                        <option key={c.id} value={c.id}>
                          {connLabel(c)}
                          {c.id === activeId ? " · primary" : ""}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label className="bulk-row">
                    <span>Target (where tests get published)</span>
                    <select value={targetId} onChange={(e) => setTargetId(e.target.value)}>
                      <option value="">(choose a target connection)</option>
                      {connections.map((c) => (
                        <option key={c.id} value={c.id}>
                          {connLabel(c)} · {ROLE_LABEL[c.role] ?? c.role}
                        </option>
                      ))}
                    </select>
                  </label>

                  {sourceId && targetId && sourceId === targetId && (
                    <div className="error-text">
                      Source and target must be different connections.
                    </div>
                  )}

                  {connections.length < 2 && (
                    <div className="select-all-banner">
                      This workspace only has one connection. Add a target
                      connection (e.g. a Kiwi instance) before you can bridge.{" "}
                      <button className="link-btn" onClick={onOpenConnections}>
                        Open Connections…
                      </button>
                    </div>
                  )}
                </>
              )}
            </>
          )}

          {step === "gap" && (
            <>
              <p className="muted">
                Publishing from <strong>{source ? connLabel(source) : sourceId}</strong>{" "}
                to <strong>{target ? connLabel(target) : targetId}</strong>.
              </p>
              {gapLoading && <p className="muted">Computing gap…</p>}
              {gapError && <div className="error-text">{gapError}</div>}
              {!gapLoading && !gapError && gaps && (
                <>
                  {blocking.length > 0 && (
                    <div className="bridge-gap-group bridge-gap-blocking">
                      <p className="error-text bridge-gap-title">
                        Blocking: the target can't represent this, so publishing is disabled.
                      </p>
                      <ul className="commit-fail-list">
                        {blocking.map((g, i) => (
                          <li key={i}>{g.message}</li>
                        ))}
                      </ul>
                    </div>
                  )}
                  {lossy.length > 0 && (
                    <div className="bridge-gap-group bridge-gap-lossy conflict-text">
                      <p className="bridge-gap-title">
                        Lossy: publishing will lose or degrade this.
                      </p>
                      <ul className="commit-fail-list">
                        {lossy.map((g, i) => (
                          <li key={i}>{g.message}</li>
                        ))}
                      </ul>
                    </div>
                  )}
                  {info.length > 0 && (
                    <div className="bridge-gap-group">
                      <p className="muted bridge-gap-title">
                        Info: handled, for reference.
                      </p>
                      <ul className="commit-fail-list muted">
                        {info.map((g, i) => (
                          <li key={i}>{g.message}</li>
                        ))}
                      </ul>
                    </div>
                  )}
                  {blocking.length === 0 && lossy.length === 0 && info.length === 0 && (
                    <p className="ok-text">No gaps. The target can fully represent this data.</p>
                  )}

                  {lossy.length > 0 && blocking.length === 0 && (
                    <label className="bulk-row bridge-ack-row">
                      <span>
                        <input
                          type="checkbox"
                          checked={lossAck}
                          onChange={(e) => setLossAck(e.target.checked)}
                        />{" "}
                        I understand the losses above
                      </span>
                    </label>
                  )}
                </>
              )}
            </>
          )}

          {step === "map" && (
            <>
              {mapLoading && <p className="muted">Loading mapping…</p>}
              {mapError && <div className="error-text">{mapError}</div>}
              {!mapLoading && !mapError && mapping && (
                <>
                  <div className="bulk-row">
                    <span>Status mapping (source → target)</span>
                    {Object.keys(mapping.statusMap).length === 0 ? (
                      <p className="muted">No statuses to map.</p>
                    ) : (
                      <div className="bridge-status-map">
                        {Object.entries(mapping.statusMap).map(([src, tgt]) => (
                          <div className="bridge-status-row" key={src}>
                            <span className="mono">{src}</span>
                            <span aria-hidden="true">→</span>
                            <input
                              className="detail-input detail-input-inline"
                              value={tgt}
                              onChange={(e) => setStatusTarget(src, e.target.value)}
                            />
                          </div>
                        ))}
                      </div>
                    )}
                  </div>

                  <div className="bulk-row">
                    <span>Step mode</span>
                    <div className="bridge-radio-group">
                      <label>
                        <input
                          type="radio"
                          name="bridge-step-mode"
                          checked={mapping.stepMode === "flatten"}
                          onChange={() => setStepMode("flatten")}
                        />{" "}
                        Flatten (steps become numbered text)
                      </label>
                      <label>
                        <input
                          type="radio"
                          name="bridge-step-mode"
                          checked={mapping.stepMode === "passthrough"}
                          onChange={() => setStepMode("passthrough")}
                        />{" "}
                        Passthrough (keep structured steps)
                      </label>
                    </div>
                  </div>

                  <p className="muted">
                    Field mapping and the unmapped-field policy use the
                    workspace defaults for this connection pair.
                  </p>
                </>
              )}
            </>
          )}

          {step === "publish" && (
            <>
              <p className="muted">
                Publishing from <strong>{source ? connLabel(source) : sourceId}</strong>{" "}
                to <strong>{target ? connLabel(target) : targetId}</strong>. Already-published
                tests are skipped, so this is safe to re-run.
              </p>

              {!result && (
                <button className="btn btn-primary" onClick={runPublish} disabled={publishing}>
                  {publishing ? "Publishing…" : "Publish tests to target"}
                </button>
              )}

              {publishError && <div className="error-text">{publishError}</div>}

              {result && (
                <div className="bridge-result">
                  <p className="ok-text">✓ Created {result.created.length}</p>
                  <p className="muted">Already published: {result.alreadyPublished.length}</p>
                  <p className={result.failed.length > 0 ? "error-text" : "muted"}>
                    Failed: {result.failed.length}
                  </p>

                  {result.failed.length > 0 && (
                    <>
                      <button
                        className="link-btn"
                        onClick={() => setShowFailures((v) => !v)}
                      >
                        {showFailures ? "Hide" : "Show"} failures ({result.failed.length})
                      </button>
                      {showFailures && (
                        <ul className="commit-fail-list">
                          {result.failed.map((f, i) => (
                            <li key={i}>
                              <span className="mono">{f.localKey}</span>: {f.error}
                              {f.targetKey && (
                                <span className="warn-text">
                                  {" "}
                                  (created in target as {f.targetKey} but incomplete;
                                  a retry will skip it)
                                </span>
                              )}
                            </li>
                          ))}
                        </ul>
                      )}
                    </>
                  )}

                  <div className="form-actions">
                    <button className="btn" onClick={runPublish} disabled={publishing}>
                      {publishing ? "Publishing…" : "Publish again"}
                    </button>
                  </div>
                </div>
              )}
            </>
          )}
        </div>

        <div className="pending-actions">
          <div>
            {step !== "select" && (
              <button className="btn" onClick={handleBack} disabled={publishing || mapSaving}>
                Back
              </button>
            )}
          </div>
          <div className="form-actions form-actions-end">
            <button className="btn" onClick={onClose}>
              {step === "publish" ? "Close" : "Cancel"}
            </button>
            {step === "select" && (
              <button className="btn btn-primary" onClick={handleSelectNext} disabled={!canProceedSelect}>
                Next
              </button>
            )}
            {step === "gap" && (
              <button className="btn btn-primary" onClick={handleGapNext} disabled={!canProceedGap}>
                Next
              </button>
            )}
            {step === "map" && (
              <button
                className="btn btn-primary"
                onClick={handleMapNext}
                disabled={!mapping || mapSaving}
              >
                {mapSaving ? "Saving…" : "Next"}
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
