import { useEffect, useState } from "react";

import {
  CreateTest,
  ListComponents,
  ListPriorities,
  ListAllPreconditions,
  errMsg,
} from "../api";
import type { Folder, Precondition, TestDraft, StepDraft } from "../api";
import { CloneStepsModal } from "./CloneStepsModal";
import { MarkdownField } from "./MarkdownField";

interface Props {
  profileId: string;
  folders: Folder[];
  initialFolderId?: string;
  onCreated: (tempKey: string) => void;
  onCancel: () => void;
}

type StepRow = StepDraft & { _key: string };

let stepCounter = 0;
const blankStep = (): StepRow => ({ _key: `s${stepCounter++}`, action: "", data: "", expected: "" });

// NewTestPanel is the interactive "New Test" form (FR-1). It collects fields
// locally and submits one CreateTest call; the new Test then opens in the
// normal detail panel for further editing.
export function NewTestPanel({
  profileId,
  folders,
  initialFolderId,
  onCreated,
  onCancel,
}: Props) {
  const [summary, setSummary] = useState("");
  const [description, setDescription] = useState("");
  const [priority, setPriority] = useState("");
  const [labels, setLabels] = useState("");
  const [components, setComponents] = useState("");
  const [folderId, setFolderId] = useState(initialFolderId ?? "");
  const [steps, setSteps] = useState<StepRow[]>([]);
  const [precondKeys, setPrecondKeys] = useState<string[]>([]);

  const [stepsOpen, setStepsOpen] = useState(false);
  const [showClone, setShowClone] = useState(false);
  const [preOpen, setPreOpen] = useState(false);
  const [componentOptions, setComponentOptions] = useState<string[]>([]);
  const [priorityOptions, setPriorityOptions] = useState<string[]>([]);
  const [allPreconditions, setAllPreconditions] = useState<Precondition[]>([]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  // Resizeable width, sharing the detail panel's persisted key for a uniform feel.
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

  useEffect(() => {
    if (!profileId) return;
    ListComponents(profileId)
      .then((bs) => setComponentOptions((bs ?? []).map((b) => b.label)))
      .catch(() => setComponentOptions([]));
    ListPriorities(profileId)
      .then((ps) => setPriorityOptions(ps ?? []))
      .catch(() => setPriorityOptions([]));
    ListAllPreconditions(profileId)
      .then((ps) => setAllPreconditions(ps ?? []))
      .catch(() => setAllPreconditions([]));
  }, [profileId]);

  function updateStep(i: number, field: keyof StepDraft, value: string) {
    setSteps((prev) =>
      prev.map((s, j) => (j === i ? { ...s, [field]: value } : s)),
    );
  }

  const availablePreconditions = allPreconditions.filter(
    (p) => !precondKeys.includes(p.key),
  );

  async function submit() {
    if (!summary.trim()) {
      setError("A summary is required.");
      return;
    }
    setSaving(true);
    setError("");
    const draft: TestDraft = {
      summary: summary.trim(),
      description,
      priority,
      labels: labels.trim(),
      components: components.trim(),
      folderId,
      steps: steps.map(({ action, data, expected }) => ({ action, data, expected })),
      precondKeys,
    };
    try {
      // Cast to any: the Wails-generated binding expects its own class type but
      // the runtime serialises the plain object identically (deviation from plan).
      const tempKey = await CreateTest(profileId, draft as any);
      onCreated(tempKey);
    } catch (e) {
      setError(errMsg(e));
      setSaving(false);
    }
  }

  return (
    <aside className="detail new-test-panel" style={{ width }}>
      <div className="detail-resizer" onMouseDown={startResize} title="Drag to resize" />
      <div className="detail-head">
        <div className="detail-head-id">
          <span className="detail-key">New Test</span>
          <span className="ntp-badge">unsaved</span>
        </div>
        <button className="btn btn-ghost detail-close" onClick={onCancel} title="Cancel">
          ✕
        </button>
      </div>

      <div className="detail-body">
        {error && <div className="error-text detail-save-error">{error}</div>}

        <div className="field-label">Summary *</div>
        <input
          className="detail-input"
          value={summary}
          onChange={(e) => setSummary(e.target.value)}
          placeholder="What does this test verify?"
          autoFocus
        />

        <div className="field-label">Description</div>
        <textarea
          className="detail-input"
          rows={4}
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="Markdown supported"
        />

        <div className="ntp-row">
          <div className="ntp-col">
            <div className="field-label">Priority</div>
            <select
              className="detail-input"
              value={priority}
              onChange={(e) => setPriority(e.target.value)}
            >
              <option value="">(default)</option>
              {priorityOptions.map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </select>
          </div>
          <div className="ntp-col">
            <div className="field-label">Folder</div>
            <select
              className="detail-input"
              value={folderId}
              onChange={(e) => setFolderId(e.target.value)}
            >
              <option value="">(none)</option>
              {folders.map((f) => (
                <option key={f.id} value={f.id}>
                  {f.id}
                </option>
              ))}
            </select>
          </div>
        </div>

        <div className="ntp-row">
          <div className="ntp-col">
            <div className="field-label">Labels</div>
            <input
              className="detail-input"
              value={labels}
              onChange={(e) => setLabels(e.target.value)}
              placeholder="space-separated"
            />
          </div>
          <div className="ntp-col">
            <div className="field-label">Components</div>
            <input
              className="detail-input"
              list="ntp-components"
              value={components}
              onChange={(e) => setComponents(e.target.value)}
              placeholder="comma-separated"
            />
            <datalist id="ntp-components">
              {componentOptions.map((c) => (
                <option key={c} value={c} />
              ))}
            </datalist>
          </div>
        </div>

        {/* Steps */}
        <div className="ntp-section">
          <button
            className="ntp-section-head"
            onClick={() => setStepsOpen((v) => !v)}
            type="button"
          >
            <span>Steps · {steps.length}</span>
            <span className="ntp-caret">{stepsOpen ? "▾" : "▸"}</span>
          </button>
          {stepsOpen && (
            <div className="ntp-section-body">
              {steps.map((s, i) => (
                <div className="ntp-step" key={s._key}>
                  <div className="ntp-step-head">
                    <span className="muted">Step {i + 1}</span>
                    <button
                      className="btn btn-ghost ntp-step-del"
                      onClick={() => setSteps((prev) => prev.filter((_, j) => j !== i))}
                      title="Remove step"
                      type="button"
                    >
                      ✕
                    </button>
                  </div>
                  <MarkdownField
                    className="detail-input"
                    value={s.action}
                    onChange={(v) => updateStep(i, "action", v)}
                    onCommit={() => {}}
                    rows={2}
                    placeholder="Action — markdown supported"
                  />
                  <MarkdownField
                    className="detail-input"
                    value={s.data}
                    onChange={(v) => updateStep(i, "data", v)}
                    onCommit={() => {}}
                    multiline={false}
                    placeholder="Data — markdown supported"
                  />
                  <MarkdownField
                    className="detail-input"
                    value={s.expected}
                    onChange={(v) => updateStep(i, "expected", v)}
                    onCommit={() => {}}
                    rows={2}
                    placeholder="Expected result — markdown supported"
                  />
                </div>
              ))}
              <div className="ntp-step-actions">
                <button
                  className="btn ntp-add"
                  onClick={() => setSteps((prev) => [...prev, blankStep()])}
                  type="button"
                >
                  ＋ add step
                </button>
                <button
                  className="btn ntp-clone"
                  onClick={() => setShowClone(true)}
                  type="button"
                  title="Copy steps from an existing test"
                >
                  Clone from…
                </button>
              </div>
            </div>
          )}
        </div>

        {/* Preconditions */}
        <div className="ntp-section">
          <button
            className="ntp-section-head"
            onClick={() => setPreOpen((v) => !v)}
            type="button"
          >
            <span>Preconditions · {precondKeys.length}</span>
            <span className="ntp-caret">{preOpen ? "▾" : "▸"}</span>
          </button>
          {preOpen && (
            <div className="ntp-section-body">
              {precondKeys.map((k) => {
                const p = allPreconditions.find((x) => x.key === k);
                return (
                  <div className="ntp-pc" key={k}>
                    <span className="ntp-pc-key">{k}</span>
                    <span className="ntp-pc-sum">{p?.summary ?? ""}</span>
                    <button
                      className="btn btn-ghost ntp-step-del"
                      onClick={() => setPrecondKeys((prev) => prev.filter((x) => x !== k))}
                      title="Unlink"
                      type="button"
                    >
                      ✕
                    </button>
                  </div>
                );
              })}
              <select
                className="detail-input"
                value=""
                onChange={(e) => {
                  if (e.target.value) {
                    setPrecondKeys((prev) => [...prev, e.target.value]);
                  }
                }}
              >
                <option value="">＋ link precondition…</option>
                {availablePreconditions.map((p) => (
                  <option key={p.key} value={p.key}>
                    {p.key} — {p.summary}
                  </option>
                ))}
              </select>
            </div>
          )}
        </div>
      </div>

      <div className="ntp-foot">
        <button className="btn" onClick={onCancel} disabled={saving}>
          Cancel
        </button>
        <button
          className="btn btn-primary"
          onClick={submit}
          disabled={saving || !summary.trim()}
        >
          {saving ? "Creating…" : "Create →"}
        </button>
      </div>

      {showClone && (
        <CloneStepsModal
          profileId={profileId}
          targetLabel="the new test"
          onCancel={() => setShowClone(false)}
          onConfirm={(_sourceKey, _stepIds, cloned) => {
            setSteps((prev) => [
              ...prev,
              ...cloned.map((s) => ({ ...s, _key: `s${stepCounter++}` })),
            ]);
            setStepsOpen(true);
            setShowClone(false);
          }}
        />
      )}
    </aside>
  );
}
