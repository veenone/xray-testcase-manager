import { useEffect, useState } from "react";
import {
  ListRequirementSources,
  SetRequirementSource,
  RemoveRequirementSource,
  ListRequirementLinkTypeDetails,
  SetRequirementLinkType,
  GetSettings,
  errMsg,
} from "../api";
import type { RequirementSource } from "../api";

interface Props {
  profileId: string;
  onClose: () => void;
}

// LinkTypeOpt is one issue-link type with its directional labels. The value we
// store is always `name`; `inward`/`outward` are shown so the user can
// recognise the coverage relationship (e.g. "Tests (tested by / tests)").
interface LinkTypeOpt {
  name: string;
  inward: string;
  outward: string;
}

// normLT strips case and non-letters so "Tested By", "tested by", and
// "tested_by" compare equal.
function normLT(s: string): string {
  return (s || "").toLowerCase().replace(/[^a-z]/g, "");
}

// pickDefaultLinkType chooses the selection shown when the user has not set an
// explicit type. It matches the coverage link by DIRECTION first (a type whose
// inward label is "tested by" or outward label is "tests"), then by a name of
// "Tests", then the first type, mirroring the backend's auto-resolve so the
// dropdown shows what would actually be committed.
function pickDefaultLinkType(types: LinkTypeOpt[], stored: string): string {
  if (stored) return stored;
  const byDir = types.find(
    (t) => normLT(t.inward).includes("testedby") || normLT(t.outward) === "tests",
  );
  if (byDir) return byDir.name;
  const byName = types.find((t) => normLT(t.name) === "tests");
  if (byName) return byName.name;
  return types[0]?.name ?? "";
}

// labelForLinkType renders "Name (inward / outward)", falling back to just the
// name when a backend (e.g. Kiwi) has no distinct direction labels.
function labelForLinkType(t: LinkTypeOpt): string {
  if (t.inward && t.outward && (t.inward !== t.name || t.outward !== t.name)) {
    return `${t.name} (${t.inward} / ${t.outward})`;
  }
  return t.name;
}

// RequirementSourcesModal configures which projects requirements are pulled
// from (besides those already linked to synced Tests, which come in by key
// regardless of project). Changes take effect on the next sync.
export function RequirementSourcesModal({ profileId, onClose }: Props) {
  const [sources, setSources] = useState<RequirementSource[]>([]);
  const [projectKey, setProjectKey] = useState("");
  const [issueTypes, setIssueTypes] = useState("Story Epic");
  const [scopeJql, setScopeJql] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  // editingSource holds the original project key when editing an existing source.
  const [editingSource, setEditingSource] = useState<string | null>(null);

  // Link-type configuration state.
  const [linkTypes, setLinkTypes] = useState<LinkTypeOpt[]>([]);
  const [selectedLinkType, setSelectedLinkType] = useState("");
  const [linkTypeError, setLinkTypeError] = useState("");
  const [linkTypeBusy, setLinkTypeBusy] = useState(false);

  function reload() {
    ListRequirementSources(profileId)
      .then((s) => setSources(s ?? []))
      .catch((e) => setError(errMsg(e)));
  }

  useEffect(() => {
    reload();
    // Load the instance's link types and the current setting together, then
    // select the stored type, or auto-pick the coverage type by direction.
    let cancelled = false;
    Promise.all([
      ListRequirementLinkTypeDetails(profileId).catch(() => [] as LinkTypeOpt[]),
      GetSettings().catch(() => null),
    ]).then(([types, settings]) => {
      if (cancelled) return;
      const opts = (types ?? []) as LinkTypeOpt[];
      setLinkTypes(opts);
      setSelectedLinkType(
        pickDefaultLinkType(opts, settings?.requirementLinkType ?? ""),
      );
    });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [profileId]);

  async function saveLinkType(value: string) {
    setLinkTypeBusy(true);
    setLinkTypeError("");
    try {
      await SetRequirementLinkType(value);
      setSelectedLinkType(value);
    } catch (e) {
      setLinkTypeError(errMsg(e));
    } finally {
      setLinkTypeBusy(false);
    }
  }

  function startEdit(s: RequirementSource) {
    setProjectKey(s.projectKey);
    setIssueTypes(s.issueTypes || "");
    setScopeJql(s.scopeJql || "");
    setEditingSource(s.projectKey);
    setError("");
  }

  function cancelEdit() {
    setEditingSource(null);
    setProjectKey("");
    setIssueTypes("Story Epic");
    setScopeJql("");
    setError("");
  }

  async function save() {
    if (!projectKey.trim()) return;
    setBusy(true);
    setError("");
    try {
      await SetRequirementSource(
        profileId,
        projectKey.trim(),
        issueTypes.trim(),
        scopeJql.trim(),
      );
      // If editing and the project key changed, remove the old one.
      if (editingSource !== null && editingSource !== projectKey.trim()) {
        await RemoveRequirementSource(profileId, editingSource);
      }
      setEditingSource(null);
      setProjectKey("");
      setIssueTypes("Story Epic");
      setScopeJql("");
      reload();
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }

  async function remove(pk: string) {
    setError("");
    // If removing the source currently being edited, cancel the edit too.
    if (editingSource === pk) cancelEdit();
    try {
      await RemoveRequirementSource(profileId, pk);
      reload();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  // The selected link type always needs a matching <option>: a persisted value
  // not present in the instance's current list (or an empty auto-resolve) would
  // otherwise be unselectable (RND_P_4TFINT_05-275). Prepend it, deduped.
  const linkTypeOptions =
    selectedLinkType && !linkTypes.some((t) => t.name === selectedLinkType)
      ? [{ name: selectedLinkType, inward: "", outward: "" }, ...linkTypes]
      : linkTypes;

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div
        className="modal pending-modal req-sources"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="pending-head">
          <h2>Requirement sources</h2>
          <button className="btn btn-ghost" onClick={onClose} title="Close">
            ✕
          </button>
        </div>

        <div className="bulk-body req-sources-body">
          {/* Section: the global coverage link type. */}
          <section className="rs-section">
            <div className="rs-section-head">
              <span className="rs-eyebrow">Coverage link type</span>
            </div>
            <div className="rs-linktype">
              <div className="rs-linktype-control">
                <select
                  className="detail-input rs-select"
                  value={selectedLinkType}
                  disabled={linkTypeBusy}
                  onChange={(e) => saveLinkType(e.target.value)}
                >
                  {linkTypeOptions
                    .filter((t) => t.name)
                    .map((t) => (
                      <option key={t.name} value={t.name}>
                        {labelForLinkType(t)}
                      </option>
                    ))}
                </select>
                {linkTypeBusy && <span className="muted rs-saving">Saving…</span>}
              </div>
              <p className="src-field-help rs-linktype-help">
                The Jira issue-link type created when a test is linked to a
                requirement. Options show each type's direction labels; the
                coverage link is usually "Tests" (the requirement is "tested by"
                the test). The stored value is the link-type name. Changes take
                effect on the next commit.
              </p>
              {linkTypeError && <div className="error-text">{linkTypeError}</div>}
            </div>
          </section>

          {/* Section: requirement source projects (list + add/edit form). */}
          <section className="rs-section">
            <div className="rs-section-head">
              <span className="rs-eyebrow">Source projects</span>
              {sources.length > 0 && (
                <span className="rs-count">{sources.length}</span>
              )}
            </div>
            <p className="src-field-help rs-section-intro">
              Projects to pull requirements from, besides those already linked to
              your synced tests. Applies on the next sync.
            </p>

            <div className="rs-grid">
              {/* Left: the configured sources. */}
              <div className="rs-list-col">
                {sources.length === 0 ? (
                  <div className="rs-empty">
                    <p className="muted">No source projects yet.</p>
                    <p className="muted rs-empty-hint">
                      Add one with the form on the right.
                    </p>
                  </div>
                ) : (
                  <ul className="src-list rs-list">
                    {sources.map((s) => (
                      <li
                        key={s.projectKey}
                        className={
                          editingSource === s.projectKey ? "src-list-editing" : ""
                        }
                      >
                        <span className="mono src-project">{s.projectKey}</span>
                        <span className="muted src-types">
                          {s.issueTypes || "(any type)"}
                        </span>
                        {s.scopeJql && (
                          <span className="muted src-jql" title={s.scopeJql}>
                            {s.scopeJql}
                          </span>
                        )}
                        <button
                          className="btn btn-ghost src-edit"
                          onClick={() => startEdit(s)}
                          title="Edit this source"
                          disabled={
                            editingSource !== null &&
                            editingSource !== s.projectKey
                          }
                        >
                          Edit
                        </button>
                        <button
                          className="btn btn-ghost src-remove"
                          onClick={() => remove(s.projectKey)}
                          title="Remove source"
                        >
                          ✕
                        </button>
                      </li>
                    ))}
                  </ul>
                )}
              </div>

              {/* Right: add or edit a source. */}
              <div className="rs-form-col">
                <div className="rs-form-head">
                  <span className="rs-form-title">
                    {editingSource !== null ? "Edit source" : "Add source"}
                  </span>
                  {editingSource !== null && (
                    <button
                      className="btn btn-ghost rs-form-cancel"
                      onClick={cancelEdit}
                      disabled={busy}
                    >
                      Cancel
                    </button>
                  )}
                </div>
                {editingSource !== null && (
                  <p className="muted src-edit-notice">
                    Editing <strong>{editingSource}</strong>. Change the project
                    key to rename it.
                  </p>
                )}
                <label className="src-field">
                  <span className="src-field-label">Project key</span>
                  <input
                    className="detail-input"
                    placeholder="e.g. PRD"
                    value={projectKey}
                    onChange={(e) => setProjectKey(e.target.value)}
                  />
                  <span className="src-field-help">
                    The Jira project to pull requirement issues from. Use the
                    key, not the name.
                  </span>
                </label>
                <label className="src-field">
                  <span className="src-field-label">Issue types</span>
                  <input
                    className="detail-input"
                    placeholder="e.g. Story Epic"
                    value={issueTypes}
                    onChange={(e) => setIssueTypes(e.target.value)}
                  />
                  <span className="src-field-help">
                    Space-separated issue types that count as requirements (e.g.{" "}
                    <code>Story Epic</code>). Leave blank for any type.
                  </span>
                </label>
                <label className="src-field">
                  <span className="src-field-label">
                    Scope JQL <span className="rs-optional">optional</span>
                  </span>
                  <input
                    className="detail-input"
                    placeholder="e.g. fixVersion = &quot;2.0&quot;"
                    value={scopeJql}
                    onChange={(e) => setScopeJql(e.target.value)}
                  />
                  <span className="src-field-help">
                    Narrows which issues are pulled, combined with the issue
                    types above.
                  </span>
                </label>
              </div>
            </div>
          </section>

          {error && <div className="error-text">{error}</div>}
        </div>

        <div className="pending-actions">
          <button className="btn" onClick={onClose}>
            Close
          </button>
          <button
            className="btn btn-primary"
            onClick={save}
            disabled={busy || !projectKey.trim()}
          >
            {editingSource !== null ? "Save changes" : "Add source"}
          </button>
        </div>
      </div>
    </div>
  );
}
