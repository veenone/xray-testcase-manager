import { useEffect, useState } from "react";
import { Modal } from "./Modal";
import { BulkEditTests, errMsg } from "../api";
import type { BulkEdit, BulkEditResult } from "../api";

interface Props {
  profileId: string;
  testKeys: string[];
  onComplete: (result: BulkEditResult) => void;
  onCancel: () => void;
}

interface OpDef {
  value: string;
  label: string;
}

interface FieldDef {
  value: string;
  label: string;
  ops: OpDef[];
  // options, when present, constrains the value to a fixed list rendered as a
  // dropdown (used for the Execution type / Xray Test Type).
  options?: string[];
}

// EXEC_TYPE_OPTIONS is the fixed Xray Test Type (execution type) vocabulary.
const EXEC_TYPE_OPTIONS = ["Manual", "Automated", "Generic", "Cucumber"];

const FIELDS: FieldDef[] = [
  { value: "summary", label: "Summary", ops: [{ value: "set", label: "Replace" }] },
  {
    value: "description",
    label: "Description",
    ops: [
      { value: "set", label: "Replace" },
      { value: "append", label: "Append" },
    ],
  },
  { value: "priority", label: "Priority", ops: [{ value: "set", label: "Set" }] },
  {
    value: "labels",
    label: "Labels",
    ops: [
      { value: "set", label: "Replace all" },
      { value: "add_label", label: "Add label" },
      { value: "remove_label", label: "Remove label" },
    ],
  },
  {
    value: "exec_type",
    label: "Execution type",
    ops: [{ value: "set", label: "Set" }],
    options: EXEC_TYPE_OPTIONS,
  },
];

export function BulkEditModal({
  profileId,
  testKeys,
  onComplete,
  onCancel,
}: Props) {
  const [field, setField] = useState("summary");
  const [operation, setOperation] = useState("set");
  const [value, setValue] = useState("");
  const [applying, setApplying] = useState(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState<BulkEditResult | null>(null);

  const fieldDef = FIELDS.find((f) => f.value === field) ?? FIELDS[0];

  // Reset operation when the chosen field doesn't support the current one.
  useEffect(() => {
    if (!fieldDef.ops.find((o) => o.value === operation)) {
      setOperation(fieldDef.ops[0].value);
    }
  }, [fieldDef, operation]);

  // Default the value to the first option when switching to a fixed-option
  // field (Execution type) so the dropdown always reflects a real choice.
  useEffect(() => {
    if (fieldDef.options && !fieldDef.options.includes(value)) {
      setValue(fieldDef.options[0]);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fieldDef]);

  async function apply() {
    if (
      (operation === "add_label" || operation === "remove_label") &&
      value.trim() === ""
    ) {
      setError("A label value is required.");
      return;
    }
    setApplying(true);
    setError("");
    try {
      const r = await BulkEditTests(profileId, testKeys, {
        operation,
        field,
        value,
      } as BulkEdit);
      setResult(r);
      if (r.failed.length === 0) {
        // All succeeded — bubble up so the parent can clear the selection
        // and close the modal.
        onComplete(r);
      }
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setApplying(false);
    }
  }

  const useTextarea =
    field === "description" ||
    (field === "labels" && operation === "set");

  return (
    <Modal onClose={onCancel} className="modal bulk-modal" labelledBy="bulk-edit-title">
        <div className="pending-head">
          <h2 id="bulk-edit-title">
            Bulk edit ({testKeys.length}{" "}
            {testKeys.length === 1 ? "test" : "tests"})
          </h2>
          <button className="btn btn-ghost" onClick={onCancel} title="Close">
            ✕
          </button>
        </div>

        {!result && (
          <div className="bulk-body">
            <label className="bulk-row">
              <span>Field</span>
              <select value={field} onChange={(e) => setField(e.target.value)}>
                {FIELDS.map((f) => (
                  <option key={f.value} value={f.value}>
                    {f.label}
                  </option>
                ))}
              </select>
            </label>

            <label className="bulk-row">
              <span>Operation</span>
              <select
                value={operation}
                onChange={(e) => setOperation(e.target.value)}
              >
                {fieldDef.ops.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
            </label>

            <label className="bulk-row bulk-row-value">
              <span>Value</span>
              {fieldDef.options ? (
                <select
                  className="detail-input"
                  value={value}
                  onChange={(e) => setValue(e.target.value)}
                >
                  {fieldDef.options.map((o) => (
                    <option key={o} value={o}>
                      {o}
                    </option>
                  ))}
                </select>
              ) : useTextarea ? (
                <textarea
                  className="detail-input"
                  value={value}
                  onChange={(e) => setValue(e.target.value)}
                  rows={5}
                  placeholder={
                    field === "labels" ? "space-separated labels" : ""
                  }
                />
              ) : (
                <input
                  className="detail-input"
                  value={value}
                  onChange={(e) => setValue(e.target.value)}
                  placeholder={
                    operation === "add_label" || operation === "remove_label"
                      ? "a single label"
                      : ""
                  }
                />
              )}
            </label>

            <p className="muted bulk-preview">
              Will queue a local pending change for {testKeys.length}{" "}
              {testKeys.length === 1 ? "test" : "tests"}
              {testKeys.length > 0 && (
                <>
                  : {testKeys.slice(0, 5).join(", ")}
                  {testKeys.length > 5 ? ", …" : ""}
                </>
              )}
              .
            </p>

            {error && <div className="error-text">{error}</div>}
          </div>
        )}

        {result && (
          <div className="bulk-body">
            {result.succeeded.length > 0 && (
              <p className="ok-text">
                ✓ Queued pending changes on {result.succeeded.length}{" "}
                {result.succeeded.length === 1 ? "test" : "tests"}.
              </p>
            )}
            {result.failed.length > 0 && (
              <div className="error-text">
                <p>Failed ({result.failed.length}):</p>
                <ul className="commit-fail-list">
                  {result.failed.map((f, i) => (
                    <li key={i}>
                      <span className="mono">{f.testKey}</span>: {f.error}
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </div>
        )}

        <div className="pending-actions">
          <p className="muted pending-footnote-inline">
            Bulk edits queue pending changes; commit them from the Pending
            list.
          </p>
          {!result ? (
            <>
              <button className="btn" onClick={onCancel} disabled={applying}>
                Cancel
              </button>
              <button
                className="btn btn-primary"
                onClick={apply}
                disabled={applying || testKeys.length === 0}
              >
                {applying ? "Applying…" : "Apply"}
              </button>
            </>
          ) : (
            <button className="btn btn-primary" onClick={onCancel}>
              Close
            </button>
          )}
        </div>
    </Modal>
  );
}
