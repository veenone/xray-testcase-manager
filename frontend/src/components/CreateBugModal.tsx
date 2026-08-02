import { useEffect, useState } from "react";
import { CreateBugForTest, GetBugCreateFields, errMsg } from "../api";
import type { BugCreateField } from "../api";
import { MultiSelect } from "./MultiSelect";
import type { MultiOption } from "./MultiSelect";
import {
  buildCreateFieldsPayload,
  createFieldsValid,
  initCreateFieldDefaults,
} from "./createFields";

interface Props {
  profileId: string;
  testKey: string;
  testSummary: string;
  execKey: string;
  onClose: () => void;
  onCreated: () => void;
}

const PRIORITIES = ["Highest", "High", "Medium", "Low", "Lowest"];

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
        setExtraValues(initCreateFieldDefaults(fields ?? []));
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

  const extraValid = () => createFieldsValid(extraFields, extraValues);

  async function create() {
    if (!summary.trim()) return;
    if (!extraValid()) return;
    setBusy(true);
    setError("");
    try {
      const fields = buildCreateFieldsPayload(extraFields, extraValues);
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

    // "text" fields render as a multiline textarea so long-form inputs like
    // "Steps to Reproduce" are usable.
    if (f.type === "text") {
      return (
        <label key={f.id}>
          {f.name}{f.required ? " *" : ""}
          <textarea
            rows={4}
            value={typeof rawVal === "string" ? rawVal : ""}
            onChange={(e) => setSingleExtra(f.id, e.target.value)}
            required={f.required}
          />
        </label>
      );
    }

    if (f.type === "number" || f.type === "date") {
      return (
        <label key={f.id}>
          {f.name}{f.required ? " *" : ""}
          <input
            type={f.type === "number" ? "number" : "date"}
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
            {!selected && <option value="">(select)</option>}
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
      // Map BugFieldOption {id, value} -> MultiOption {value, label} so the
      // checkbox-dropdown MultiSelect can render them. Selected ids flow back
      // via onChange; buildFieldValue converts them to [{id},...] for Jira.
      const opts: MultiOption[] = f.allowedValues.map((av) => ({
        value: av.id,
        label: av.value,
      }));
      return (
        <label key={f.id}>
          {f.name}{f.required ? " *" : ""}
          <MultiSelect
            options={opts}
            selected={selected}
            onChange={(next) => setMultiExtra(f.id, next)}
            allLabel={`Select ${f.name}…`}
          />
        </label>
      );
    }

    // Fallback: treat as multiline text.
    return (
      <label key={f.id}>
        {f.name}{f.required ? " *" : ""}
        <textarea
          rows={4}
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
