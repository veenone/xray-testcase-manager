import { useState } from "react";
import {
  CreateProfile,
  CreateProfileReusingToken,
  UpdateProfile,
  TestConnection,
  errMsg,
} from "../api";
import type { Profile } from "../api";

interface Props {
  onCreated: (p: Profile) => void;
  onCancel?: () => void;
  // When set, the form edits this profile instead of creating a new one (FR-5).
  profile?: Profile;
  // Existing profiles — drives the "reuse token" option when creating (FR-5).
  profiles?: Profile[];
}

// projectKeyError validates a Jira project key, rejecting trailing slashes,
// spaces, and other invalid characters. Jira DC project keys start with a letter
// and contain letters, digits, and underscores (e.g. RND_P_4TFINT_05) — we
// accept any case and upper-case on input.
function projectKeyError(key: string): string {
  const k = key.trim();
  if (k === "") return "";
  if (!/^[A-Z][A-Z0-9_]+$/.test(k)) {
    return "Project key must start with a letter and contain only letters, digits, and underscores — no spaces, slashes, or other special characters.";
  }
  return "";
}

// normalizeJiraUrl trims surrounding whitespace and strips trailing slashes so
// the stored base URL is clean (e.g. "https://jira.example.com/" or a pasted
// ".../secure/Dashboard.jspa " is reduced to the bare origin/base). The backend
// also TrimRights "/", but normalizing here keeps the stored value tidy and the
// connection test honest.
function normalizeJiraUrl(url: string): string {
  return url.trim().replace(/[\s/]+$/, "");
}

// jiraUrlError validates the Jira base URL. Demo URLs (demo / mock) are allowed;
// otherwise it must be a well-formed http(s) URL with a host and no spaces.
function jiraUrlError(url: string): string {
  const u = normalizeJiraUrl(url);
  if (u === "") return "";
  if (/^(demo|(demo|mock):.*)$/i.test(u)) return "";
  if (/\s/.test(u)) {
    return "The URL must not contain spaces.";
  }
  let parsed: URL;
  try {
    parsed = new URL(u);
  } catch {
    return "Enter a full base URL, e.g. https://jira.example.com (or 'demo').";
  }
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    return "The URL must start with http:// or https:// (or be 'demo').";
  }
  if (!parsed.hostname) {
    return "The URL must include a host, e.g. https://jira.example.com.";
  }
  return "";
}

export function ProfileForm({ onCreated, onCancel, profile, profiles }: Props) {
  const isEdit = !!profile;
  const others = (profiles ?? []).filter((p) => p.id !== profile?.id);
  const [name, setName] = useState(profile?.name ?? "");
  const [jiraUrl, setJiraUrl] = useState(profile?.jiraUrl ?? "");
  const [projectKey, setProjectKey] = useState(profile?.projectKey ?? "");
  const [scopeJql, setScopeJql] = useState(profile?.scopeJql ?? "");
  const [token, setToken] = useState("");
  // Reuse a stored PAT from an existing profile (create only). "" = enter a new
  // token below.
  const [reuseFrom, setReuseFrom] = useState("");

  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState("");
  const [testOk, setTestOk] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const keyError = projectKeyError(projectKey);
  const urlError = jiraUrlError(jiraUrl);
  // A token is needed for create unless reusing one from another profile; on
  // edit a blank token keeps the stored PAT.
  const tokenSatisfied = isEdit || reuseFrom !== "" || token.trim() !== "";
  const canTest =
    jiraUrl.trim() !== "" && urlError === "" && token.trim() !== "";
  const canSave =
    name.trim() !== "" &&
    jiraUrl.trim() !== "" &&
    urlError === "" &&
    projectKey.trim() !== "" &&
    keyError === "" &&
    tokenSatisfied;

  // Warn when an edit changes the project/URL — the cached data will be cleared.
  const willClearCache =
    isEdit &&
    (projectKey.trim() !== profile!.projectKey ||
      normalizeJiraUrl(jiraUrl) !== profile!.jiraUrl);

  async function test() {
    setTesting(true);
    setTestResult("");
    setTestOk(false);
    try {
      const user = await TestConnection(normalizeJiraUrl(jiraUrl), token.trim());
      setTestResult(`Connected as ${user}`);
      setTestOk(true);
    } catch (e) {
      setTestResult(errMsg(e));
    } finally {
      setTesting(false);
    }
  }

  async function save() {
    setSaving(true);
    setError("");
    try {
      const key = projectKey.trim().toUpperCase();
      let p: Profile;
      if (isEdit) {
        p = await UpdateProfile(
          profile!.id,
          name.trim(),
          normalizeJiraUrl(jiraUrl),
          key,
          scopeJql.trim(),
          token.trim(),
        );
      } else if (reuseFrom !== "") {
        p = await CreateProfileReusingToken(
          name.trim(),
          normalizeJiraUrl(jiraUrl),
          key,
          scopeJql.trim(),
          reuseFrom,
        );
      } else {
        p = await CreateProfile(
          name.trim(),
          normalizeJiraUrl(jiraUrl),
          key,
          scopeJql.trim(),
          token.trim(),
        );
      }
      onCreated(p);
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="profile-form">
      <h2>{isEdit ? "Edit profile" : "New profile"}</h2>
      <label>
        Profile name
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="QA — Project X"
        />
      </label>
      <label>
        Jira base URL
        <input
          value={jiraUrl}
          onChange={(e) => setJiraUrl(e.target.value)}
          onBlur={() => setJiraUrl(normalizeJiraUrl(jiraUrl))}
          placeholder="https://jira.example.com"
          spellCheck={false}
        />
        {urlError && <span className="field-error">{urlError}</span>}
      </label>
      <label>
        Project key
        <input
          value={projectKey}
          onChange={(e) => setProjectKey(e.target.value.toUpperCase())}
          placeholder="QA"
          spellCheck={false}
        />
        {keyError && <span className="field-error">{keyError}</span>}
      </label>
      <label>
        Scope JQL (optional)
        <input
          value={scopeJql}
          onChange={(e) => setScopeJql(e.target.value)}
          placeholder="e.g. labels = smoke — narrows which tests sync"
        />
      </label>
      {!isEdit && others.length > 0 && (
        <label>
          Personal Access Token
          <select
            value={reuseFrom}
            onChange={(e) => setReuseFrom(e.target.value)}
          >
            <option value="">Enter a new token…</option>
            {others.map((p) => (
              <option key={p.id} value={p.id}>
                Reuse token from: {p.name} ({p.projectKey})
              </option>
            ))}
          </select>
        </label>
      )}

      {(isEdit || reuseFrom === "") && (
        <label>
          {others.length > 0 && !isEdit ? "New token" : "Personal Access Token"}
          <input
            type="password"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder={
              isEdit
                ? "Leave blank to keep the current token"
                : "Jira PAT — stored in Windows Credential Manager"
            }
          />
        </label>
      )}

      {willClearCache && (
        <div className="form-warning">
          Changing the project key or Jira URL will clear this profile's cached
          data — re-sync afterwards to pull the new project.
        </div>
      )}

      <div className="form-actions">
        <button className="btn" onClick={test} disabled={!canTest || testing}>
          {testing ? "Testing…" : "Test connection"}
        </button>
        {testResult && (
          <span className={testOk ? "ok-text" : "error-text"}>
            {testResult}
          </span>
        )}
      </div>

      {error && <div className="error-text">{error}</div>}

      <div className="form-actions form-actions-end">
        {onCancel && (
          <button className="btn" onClick={onCancel} disabled={saving}>
            Cancel
          </button>
        )}
        <button
          className="btn btn-primary"
          onClick={save}
          disabled={!canSave || saving}
        >
          {saving ? "Saving…" : isEdit ? "Save changes" : "Create profile"}
        </button>
      </div>
    </div>
  );
}
