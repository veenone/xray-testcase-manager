import { useEffect, useState } from "react";
import {
  ListRequirementSources,
  SetRequirementSource,
  RemoveRequirementSource,
  ListRequirementLinkTypes,
  SetRequirementLinkType,
  GetSettings,
  errMsg,
} from "../api";
import type { RequirementSource } from "../api";

interface Props {
  profileId: string;
  onClose: () => void;
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
  const [linkTypes, setLinkTypes] = useState<string[]>([]);
  const [selectedLinkType, setSelectedLinkType] = useState("Tested By");
  const [linkTypeError, setLinkTypeError] = useState("");
  const [linkTypeBusy, setLinkTypeBusy] = useState(false);

  function reload() {
    ListRequirementSources(profileId)
      .then((s) => setSources(s ?? []))
      .catch((e) => setError(errMsg(e)));
  }

  useEffect(() => {
    reload();
    // Load available link types and the current setting in parallel.
    ListRequirementLinkTypes(profileId)
      .then((types) => setLinkTypes(types ?? []))
      .catch(() => setLinkTypes([]));
    GetSettings()
      .then((s) => setSelectedLinkType(s.requirementLinkType || "Tested By"))
      .catch(() => {});
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

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal pending-modal" onClick={(e) => e.stopPropagation()}>
        <div className="pending-head">
          <h2>Requirement sources</h2>
          <button className="btn btn-ghost" onClick={onClose} title="Close">
            ✕
          </button>
        </div>

        <div className="bulk-body">
          <div className="src-field" style={{ marginBottom: "1rem" }}>
            <span className="src-field-label">Link type used when linking tests to requirements</span>
            <span className="src-field-help">
              The Jira issue-link type created when a test is linked to a
              requirement (via "Add tests" or the test-detail panel). Default is
              "Tested By". Changes take effect on the next commit.
            </span>
            <div style={{ display: "flex", gap: "0.5rem", alignItems: "center" }}>
              <select
                className="detail-input"
                value={selectedLinkType}
                disabled={linkTypeBusy}
                onChange={(e) => saveLinkType(e.target.value)}
                style={{ flex: 1 }}
              >
                {linkTypes.length === 0 ? (
                  <option value={selectedLinkType}>{selectedLinkType}</option>
                ) : (
                  linkTypes.map((t) => (
                    <option key={t} value={t}>
                      {t}
                    </option>
                  ))
                )}
              </select>
              {linkTypeBusy && <span className="muted">Saving…</span>}
            </div>
            {linkTypeError && <div className="error-text">{linkTypeError}</div>}
          </div>

          <p className="muted">
            Projects to pull requirements from, besides those already linked to
            your synced tests. Applies on the next sync.
          </p>

          {sources.length === 0 ? (
            <p className="muted">No sources configured.</p>
          ) : (
            <ul className="src-list">
              {sources.map((s) => (
                <li key={s.projectKey} className={editingSource === s.projectKey ? "src-list-editing" : ""}>
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
                    disabled={editingSource !== null && editingSource !== s.projectKey}
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

          <div className="src-add">
            {editingSource !== null && (
              <p className="muted src-edit-notice">
                Editing source <strong>{editingSource}</strong>. Change the project key below to rename it.
              </p>
            )}
            <label className="src-field">
              <span className="src-field-label">Project key</span>
              <span className="src-field-help">
                The Jira project to pull requirement issues from (e.g. PRD). Use
                the project's key, not its name.
              </span>
              <input
                className="detail-input"
                placeholder="e.g. PRD"
                value={projectKey}
                onChange={(e) => setProjectKey(e.target.value)}
              />
            </label>
            <label className="src-field">
              <span className="src-field-label">Issue types</span>
              <span className="src-field-help">
                Which Jira issue types in this project count as requirements —
                space-separated (e.g. <code>Story Epic</code>). These are the
                issues that will appear in the Requirements view and can be
                linked to tests. Leave blank to include any issue type.
              </span>
              <input
                className="detail-input"
                placeholder="e.g. Story Epic"
                value={issueTypes}
                onChange={(e) => setIssueTypes(e.target.value)}
              />
            </label>
            <label className="src-field">
              <span className="src-field-label">Scope JQL (optional)</span>
              <span className="src-field-help">
                Optional JQL to narrow which issues are pulled from this project
                (e.g. <code>fixVersion = "2.0"</code>). It is combined with the
                issue types above; leave blank to pull all of them.
              </span>
              <input
                className="detail-input"
                placeholder="e.g. fixVersion = &quot;2.0&quot;"
                value={scopeJql}
                onChange={(e) => setScopeJql(e.target.value)}
              />
            </label>
          </div>
          {error && <div className="error-text">{error}</div>}
        </div>

        <div className="pending-actions">
          <button className="btn" onClick={onClose}>
            Close
          </button>
          {editingSource !== null && (
            <button className="btn" onClick={cancelEdit} disabled={busy}>
              Cancel edit
            </button>
          )}
          <button
            className="btn btn-primary"
            onClick={save}
            disabled={busy || !projectKey.trim()}
          >
            {editingSource !== null ? "Save changes" : "Add / update source"}
          </button>
        </div>
      </div>
    </div>
  );
}
