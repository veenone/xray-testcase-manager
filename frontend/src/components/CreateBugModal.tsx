import { useEffect, useState } from "react";
import { CreateBugForTest, GetBugCreateFields, errMsg } from "../api";
import type { BugCreateField } from "../api";

interface Props {
  profileId: string;
  testKey: string;
  testSummary: string;
  execKey: string;
  onClose: () => void;
  onCreated: () => void;
}

const PRIORITIES = ["Highest", "High", "Medium", "Low", "Lowest"];

// buildFieldValue converts the raw user selection for a BugCreateField into the
// Jira-shaped value expected by the POST body:
//   text / number / date -> plain string
//   option / version     -> {id: selectedId}
//   versions             -> [{id: id1}, {id: id2}, ...]
//   array                -> [{id: id} for each selected]
function buildFieldValue(field: BugCreateField, raw: string | string[]): unknown {
  switch (field.type) {
    case "option":
    case "version":
      return typeof raw === "string" && raw ? { id: raw } : undefined;
    case "versions":
    case "array": {
      const ids = Array.isArray(raw) ? raw : raw ? [raw] : [];
      return ids.length ? ids.map((id) => ({ id })) : undefined;
    }
    default:
      // text / number / date — plain string
      return typeof raw === "string" ? raw : undefined;
  }
}

// CreateBugModal files a Bug-type Jira issue against a test marked FAILED in an
// execution. Local-first: the bug is queued and pushed on the next Commit.
// Required fields beyond summary/priority/labels/description are fetched from
// GetBugCreateFields (createmeta-driven) and rendered dynamically.
export function CreateBugModal({
  profileId,
  testKey,
  testSummary,
  execKey,
  onClose,
  onCreated,
}: Props) {
  const [summary, setSummary] = useState("");
  const [priority, setPriority] = useState("Medium");
  const [labels, setLabels] = useState("");
  const [description, setDescription] = useState(
    `Found while executing ${execKey}.\nTest ${testKey} "${testSummary}" was marked FAILED.\n\nSteps / actual result:\n`,
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  // Extra required fields loaded from createmeta.
  const [extraFields, setExtraFields] = useState<BugCreateField[]>([]);
  const [extraLoading, setExtraLoading] = useState(true);
  // Raw user values: string for single-value fields, string[] for versions.
  const [extraValues, setExtraValues] = useState<Record<string, string | string[]>>({});

  // Load required extra fields on mount.
  useEffect(() => {
    let cancelled = false;
    GetBugCreateFields(profileId)
      .then((fields) => {
        if (cancelled) return;
        setExtraFields(fields ?? []);
        // Initialise defaults: first allowedValue for selects, "" for text.
        const defaults: Record<string, string | string[]> = {};
        for (const f of fields ?? []) {
          if (f.type === "versions" || f.type === "array") {
            defaults[f.id] = [];
          } else if (
            (f.type === "option" || f.type === "version") &&
            f.allowedValues?.length
          ) {
            defaults[f.id] = f.allowedValues[0].id;
          } else {
            defaults[f.id] = "";
          }
        }
        setExtraValues(defaults);
      })
      .catch(() => {
        // Non-fatal: if createmeta fails, show the base form without extra fields.
        if (!cancelled) setExtraFields([]);
      })
      .finally(() => {
        if (!cancelled) setExtraLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [profileId]);

  // Validate that all required extra fields have a non-empty value.
  function extraValid(): boolean {
    for (const f of extraFields) {
      if (!f.required) continue;
      const v = extraValues[f.id];
      if (Array.isArray(v)) {
        if (v.length === 0) return false;
      } else if (!v) {
        return false;
      }
    }
    return true;
  }

  async function create() {
    if (!summary.trim()) return;
    if (!extraValid()) return;
    setBusy(true);
    setError("");
    try {
      // Build the Jira-shaped extra fields object.
      const fields: Record<string, unknown> = {};
      for (const f of extraFields) {
        const shaped = buildFieldValue(f, extraValues[f.id] ?? "");
        if (shaped !== undefined) {
          fields[f.id] = shaped;
        }
      }
      await CreateBugForTest(
        profileId,
        testKey,
        execKey,
        summary.trim(),
        description,
        priority,
        labels.trim() ? labels.trim().split(/[\s,]+/) : [],
        fields,
      );
      onCreated();
      onClose();
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }

  function setSingleExtra(id: string, value: string) {
    setExtraValues((prev) => ({ ...prev, [id]: value }));
  }

  function setMultiExtra(id: string, selected: string[]) {
    setExtraValues((prev) => ({ ...prev, [id]: selected }));
  }

  function renderExtraField(f: BugCreateField) {
    const rawVal = extraValues[f.id] ?? "";

    if (f.type === "text" || f.type === "number" || f.type === "date") {
      return (
        <label key={f.id}>
          {f.name}{f.required ? " *" : ""}
          <input
            type={f.type === "number" ? "number" : f.type === "date" ? "date" : "text"}
            value={typeof rawVal === "string" ? rawVal : ""}
            onChange={(e) => setSingleExtra(f.id, e.target.value)}
            required={f.required}
          />
        </label>
      );
    }

    if (f.type === "option" || f.type === "version") {
      const selected = typeof rawVal === "string" ? rawVal : "";
      return (
        <label key={f.id}>
          {f.name}{f.required ? " *" : ""}
          <select value={selected} onChange={(e) => setSingleExtra(f.id, e.target.value)}>
            {!selected && <option value="">— select —</option>}
            {f.allowedValues.map((av) => (
              <option key={av.id} value={av.id}>
                {av.value}
              </option>
            ))}
          </select>
        </label>
      );
    }

    if (f.type === "versions" || f.type === "array") {
      const selected = Array.isArray(rawVal) ? rawVal : [];
      return (
        <label key={f.id}>
          {f.name}{f.required ? " *" : ""}
          <select
            multiple
            size={Math.min(f.allowedValues.length, 4)}
            value={selected}
            onChange={(e) => {
              const opts = Array.from(e.target.options);
              setMultiExtra(f.id, opts.filter((o) => o.selected).map((o) => o.value));
            }}
          >
            {f.allowedValues.map((av) => (
              <option key={av.id} value={av.id}>
                {av.value}
              </option>
            ))}
          </select>
        </label>
      );
    }

    // Fallback: treat as text.
    return (
      <label key={f.id}>
        {f.name}{f.required ? " *" : ""}
        <input
          type="text"
          value={typeof rawVal === "string" ? rawVal : ""}
          onChange={(e) => setSingleExtra(f.id, e.target.value)}
        />
      </label>
    );
  }

  const canSubmit = !busy && !!summary.trim() && !extraLoading && extraValid();

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <div className="pending-head">
          <h2>Create bug for {testKey}</h2>
          <button className="btn btn-ghost" onClick={onClose} title="Cancel" aria-label="Cancel">
            ✕
          </button>
        </div>
        <div className="bug-form">
          <label>
            Summary
            <input
              value={summary}
              autoFocus
              onChange={(e) => setSummary(e.target.value)}
              placeholder="Short defect title"
            />
          </label>
          <label>
            Priority
            <select value={priority} onChange={(e) => setPriority(e.target.value)}>
              {PRIORITIES.map((p) => (
                <option key={p} value={p}>{p}</option>
              ))}
            </select>
          </label>
          <label>
            Labels (space or comma separated)
            <input
              value={labels}
              onChange={(e) => setLabels(e.target.value)}
              placeholder="regression login"
            />
          </label>
          <label>
            Description
            <textarea
              rows={6}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </label>
          {extraLoading && (
            <div className="field-hint">Loading required fields…</div>
          )}
          {!extraLoading && extraFields.map(renderExtraField)}
          {error && <div className="error-text">{error}</div>}
        </div>
        <div className="pending-actions">
          <button className="btn" onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button
            className="btn btn-primary"
            onClick={create}
            disabled={!canSubmit}
          >
            {busy ? "Filing…" : "Create bug"}
          </button>
        </div>
      </div>
    </div>
  );
}
