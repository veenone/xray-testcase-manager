// Shared helpers for createmeta-driven "extra required fields" on issue-create
// forms (Create Bug, New Requirement). The backend returns each required field
// as a BugCreateField descriptor (a generic createmeta field: id, name, type,
// allowedValues); these helpers turn user input into the Jira-shaped values the
// create payload expects, seed sensible defaults, and validate completeness.
import type { BugCreateField } from "../api";

// RawFieldValue is what the form holds per field: a string for single-value
// fields, a string[] of ids for multi-value (versions / array) fields.
export type RawFieldValue = string | string[];

// buildCreateFieldValue converts the raw user selection for a field into the
// Jira-shaped value expected by the POST body:
//   text / number / date -> plain string
//   option / version     -> {id: selectedId}
//   versions / array      -> [{id: id1}, {id: id2}, ...]
// Returns undefined when there is nothing to send (so the caller can omit it).
export function buildCreateFieldValue(
  field: BugCreateField,
  raw: RawFieldValue,
): unknown {
  switch (field.type) {
    case "option":
    case "version":
      return typeof raw === "string" && raw ? { id: raw } : undefined;
    case "versions":
    case "array": {
      const ids = Array.isArray(raw) ? raw : raw ? [raw] : [];
      return ids.length ? ids.map((id) => ({ id })) : undefined;
    }
    case "stringarray": {
      // Array of plain strings (e.g. Labels): comma/space separated text input
      // becomes ["a","b"], not [{id}].
      const parts = Array.isArray(raw)
        ? raw
        : raw.split(/[\s,]+/).map((s) => s.trim()).filter(Boolean);
      return parts.length ? parts : undefined;
    }
    default:
      // text / number / date: plain string
      return typeof raw === "string" ? raw : undefined;
  }
}

// initCreateFieldDefaults seeds the form state: first allowed value for single
// selects, empty array for multi-selects, empty string for free text.
export function initCreateFieldDefaults(
  fields: BugCreateField[],
): Record<string, RawFieldValue> {
  const defaults: Record<string, RawFieldValue> = {};
  for (const f of fields) {
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
  return defaults;
}

// createFieldsValid reports whether every required field has a non-empty value.
export function createFieldsValid(
  fields: BugCreateField[],
  values: Record<string, RawFieldValue>,
): boolean {
  for (const f of fields) {
    if (!f.required) continue;
    const v = values[f.id];
    if (Array.isArray(v)) {
      if (v.length === 0) return false;
    } else if (!v) {
      return false;
    }
  }
  return true;
}

// buildCreateFieldsPayload assembles the Jira-shaped {fieldId: value} object,
// omitting fields with no value.
export function buildCreateFieldsPayload(
  fields: BugCreateField[],
  values: Record<string, RawFieldValue>,
): Record<string, unknown> {
  const payload: Record<string, unknown> = {};
  for (const f of fields) {
    const shaped = buildCreateFieldValue(f, values[f.id] ?? "");
    if (shaped !== undefined) {
      payload[f.id] = shaped;
    }
  }
  return payload;
}
