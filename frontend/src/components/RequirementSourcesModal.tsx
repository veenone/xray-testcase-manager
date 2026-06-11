import { useEffect, useState } from "react";
import {
  ListRequirementSources,
  SetRequirementSource,
  RemoveRequirementSource,
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

  function reload() {
    ListRequirementSources(profileId)
      .then((s) => setSources(s ?? []))
      .catch((e) => setError(errMsg(e)));
  }

  useEffect(() => {
    reload();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [profileId]);

  async function add() {
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
      setProjectKey("");
      reload();
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }

  async function remove(pk: string) {
    setError("");
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
          <p className="muted">
            Projects to pull requirements from, besides those already linked to
            your synced tests. Applies on the next sync.
          </p>

          {sources.length === 0 ? (
            <p className="muted">No sources configured.</p>
          ) : (
            <ul className="src-list">
              {sources.map((s) => (
                <li key={s.projectKey}>
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
            <input
              className="detail-input"
              placeholder="Project key (e.g. PRD)"
              value={projectKey}
              onChange={(e) => setProjectKey(e.target.value)}
            />
            <input
              className="detail-input"
              placeholder="Issue types — space-separated (e.g. Story Epic)"
              value={issueTypes}
              onChange={(e) => setIssueTypes(e.target.value)}
            />
            <input
              className="detail-input"
              placeholder="Scope JQL (optional)"
              value={scopeJql}
              onChange={(e) => setScopeJql(e.target.value)}
            />
          </div>
          {error && <div className="error-text">{error}</div>}
        </div>

        <div className="pending-actions">
          <button className="btn" onClick={onClose}>
            Close
          </button>
          <button
            className="btn btn-primary"
            onClick={add}
            disabled={busy || !projectKey.trim()}
          >
            Add / update source
          </button>
        </div>
      </div>
    </div>
  );
}
