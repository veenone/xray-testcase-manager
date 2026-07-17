import { useState, type ReactNode } from "react";
import {
  CreateProfile,
  CreateProfileReusingToken,
  UpdateProfile,
  TestConnection,
  TestProfileConnection,
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
  // Optional extra footer controls (e.g. Export / Delete) rendered left of
  // Cancel / Save. Used by the Manage Profiles modal.
  extraActions?: ReactNode;
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

// jiraUrlError validates the Jira base URL. Demo URLs are allowed — "demo", a
// "demo:" / "mock:" prefix, or a "demo-" suffixed variant like "demo-pkcs" (used
// to pick a specific built-in demo dataset); otherwise it must be a well-formed
// http(s) URL with a host and no spaces. Keep in sync with isDemoURL in the Go
// backend (internal/jira/demo.go). "kiwi-demo" (and "kiwi-demo:"/"kiwi-demo-"
// variants) is also allowed — the offline Kiwi demo, routed by app.go's
// backend factory to kiwi.New instead of the Xray path (kiwi.IsKiwiDemoURL,
// internal/backend/kiwi/demo.go).
function jiraUrlError(url: string): string {
  const u = normalizeJiraUrl(url);
  if (u === "") return "";
  if (/^(demo|demo[-:].*|mock:.*|kiwi-demo|kiwi-demo[-:].*)$/i.test(u)) return "";
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

export function ProfileForm({ onCreated, onCancel, profile, profiles, extraActions }: Props) {
  const isEdit = !!profile;
  const others = (profiles ?? []).filter((p) => p.id !== profile?.id);
  const [name, setName] = useState(profile?.name ?? "");
  // backend selects which system the profile connects to: "xray" (default,
  // Jira Data Center + Xray Server/DC) or "kiwi" (Kiwi TCMS). Seeded from the
  // saved profile on edit; new profiles default to Xray so existing behavior
  // is unchanged unless the user explicitly picks Kiwi.
  const [backend, setBackend] = useState(profile?.backend === "kiwi" ? "kiwi" : "xray");
  const backendIsKiwi = backend === "kiwi";
  // Only offer to reuse a credential from a profile on the same backend --
  // an Xray PAT and a Kiwi "username:password" string aren't interchangeable.
  const reusable = others.filter((p) => (p.backend === "kiwi") === backendIsKiwi);
  const [jiraUrl, setJiraUrl] = useState(profile?.jiraUrl ?? "");
  const [projectKey, setProjectKey] = useState(profile?.projectKey ?? "");
  const [scopeJql, setScopeJql] = useState(profile?.scopeJql ?? "");
  const [bugIssueType, setBugIssueType] = useState(profile?.bugIssueType ?? "");
  const [bugProjectMode, setBugProjectMode] = useState(
    profile?.bugProjectMode || "test",
  );
  const [bugProjectKey, setBugProjectKey] = useState(
    profile?.bugProjectKey ?? "",
  );
  const [token, setToken] = useState("");
  // Kiwi's credential is a session-login username + password, combined into a
  // single "username:password" string and passed as the existing token
  // parameter (internal/backend/kiwi splits on the first ":"). Two separate
  // fields so the user isn't asked to hand-craft the combined string.
  const [kiwiUsername, setKiwiUsername] = useState("");
  const [kiwiPassword, setKiwiPassword] = useState("");
  const [showToken, setShowToken] = useState(false);
  // Reuse a stored credential from an existing profile (create only). "" =
  // enter new credentials below.
  const [reuseFrom, setReuseFrom] = useState("");
  const [caCert, setCaCert] = useState(profile?.caCert ?? "");
  const [allowUntrustedTLS, setAllowUntrustedTLS] = useState(profile?.allowUntrustedTls ?? false);

  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState("");
  const [testOk, setTestOk] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  // Kiwi has no Jira-style project key -- "Product" is a free-form name, not
  // validated against the strict key format.
  const keyError = backendIsKiwi ? "" : projectKeyError(projectKey);
  const urlError = jiraUrlError(jiraUrl);

  const kiwiUser = kiwiUsername.trim();
  const kiwiPass = kiwiPassword.trim();
  const kiwiCredComplete = kiwiUser !== "" && kiwiPass !== "";
  // Exactly one of username/password filled is never a valid credential --
  // catches a partial edit before it's combined into "username:password".
  const kiwiCredInvalid = (kiwiUser !== "" || kiwiPass !== "") && !kiwiCredComplete;

  // currentToken is the value sent as the App methods' token/credential
  // parameter: the raw PAT for Xray, or the combined "username:password" for
  // Kiwi. "" means "leave the stored credential unchanged" on edit.
  function currentToken(): string {
    if (backendIsKiwi) {
      return kiwiCredComplete ? `${kiwiUser}:${kiwiPass}` : "";
    }
    return token.trim();
  }

  const credEnteredForCreate = backendIsKiwi ? kiwiCredComplete : token.trim() !== "";
  // A credential is needed for create unless reusing one from another
  // profile; on edit a blank credential keeps the stored one.
  const tokenSatisfied = isEdit || reuseFrom !== "" || credEnteredForCreate;
  const canTest =
    jiraUrl.trim() !== "" &&
    urlError === "" &&
    !kiwiCredInvalid &&
    (credEnteredForCreate || isEdit);
  const canSave =
    name.trim() !== "" &&
    jiraUrl.trim() !== "" &&
    urlError === "" &&
    projectKey.trim() !== "" &&
    keyError === "" &&
    tokenSatisfied &&
    !kiwiCredInvalid;

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
      let user: string;
      if (isEdit && currentToken() === "") {
        // Editing with no new credential typed: test against the stored one.
        // TestProfileConnection reads the backend type from the saved
        // profile, so it doesn't need it passed in here.
        user = await TestProfileConnection(profile!.id, normalizeJiraUrl(jiraUrl), caCert.trim(), allowUntrustedTLS);
      } else {
        // No saved profile yet (or a new credential was typed) -- route
        // through the selected backend so testing a live-Kiwi URL hits the
        // Kiwi backend, not Xray.
        user = await TestConnection(normalizeJiraUrl(jiraUrl), currentToken(), caCert.trim(), allowUntrustedTLS, backend);
      }
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
      // Kiwi's "Product" is a free-form name, not a Jira-style project key --
      // don't force it upper-case.
      const key = backendIsKiwi ? projectKey.trim() : projectKey.trim().toUpperCase();
      const bugProjKey =
        bugProjectMode === "dedicated"
          ? bugProjectKey.trim().toUpperCase()
          : "";
      let p: Profile;
      if (isEdit) {
        p = await UpdateProfile(
          profile!.id,
          name.trim(),
          normalizeJiraUrl(jiraUrl),
          key,
          scopeJql.trim(),
          bugIssueType.trim(),
          bugProjectMode,
          bugProjKey,
          currentToken(),
          caCert.trim(),
          allowUntrustedTLS,
          backend,
        );
      } else if (reuseFrom !== "") {
        p = await CreateProfileReusingToken(
          name.trim(),
          normalizeJiraUrl(jiraUrl),
          key,
          scopeJql.trim(),
          bugIssueType.trim(),
          bugProjectMode,
          bugProjKey,
          reuseFrom,
          backend,
        );
      } else {
        p = await CreateProfile(
          name.trim(),
          normalizeJiraUrl(jiraUrl),
          key,
          scopeJql.trim(),
          bugIssueType.trim(),
          bugProjectMode,
          bugProjKey,
          currentToken(),
          caCert.trim(),
          allowUntrustedTLS,
          backend,
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
        Backend
        <select
          value={backend}
          onChange={(e) => {
            setBackend(e.target.value);
            setReuseFrom(""); // the reuse list is backend-filtered; drop a now-invalid selection
          }}
        >
          <option value="xray">Xray / Jira</option>
          <option value="kiwi">Kiwi TCMS</option>
        </select>
      </label>
      <label>
        {backendIsKiwi ? "Kiwi server URL" : "Jira base URL"}
        <input
          value={jiraUrl}
          onChange={(e) => setJiraUrl(e.target.value)}
          onBlur={() => setJiraUrl(normalizeJiraUrl(jiraUrl))}
          placeholder={backendIsKiwi ? "https://kiwi.example.com" : "https://jira.example.com"}
          spellCheck={false}
        />
        {urlError && <span className="field-error">{urlError}</span>}
      </label>
      <label>
        {backendIsKiwi ? "Product" : "Project key"}
        <input
          value={projectKey}
          onChange={(e) =>
            setProjectKey(backendIsKiwi ? e.target.value : e.target.value.toUpperCase())
          }
          placeholder={backendIsKiwi ? "e.g. MyProduct" : "QA"}
          spellCheck={false}
        />
        {keyError && <span className="field-error">{keyError}</span>}
      </label>
      {!backendIsKiwi && (
        <label>
          Scope JQL (optional)
          <input
            value={scopeJql}
            onChange={(e) => setScopeJql(e.target.value)}
            placeholder="e.g. labels = smoke — narrows which tests sync"
          />
        </label>
      )}
      {!backendIsKiwi && (
        <label>
          Bug issue type
          <input
            value={bugIssueType}
            onChange={(e) => setBugIssueType(e.target.value)}
            placeholder="Bug — Jira issuetype used when filing a defect"
            spellCheck={false}
          />
        </label>
      )}
      {!backendIsKiwi && (
        <label>
          Bug project
          <select
            value={bugProjectMode}
            onChange={(e) => setBugProjectMode(e.target.value)}
          >
            <option value="test">Same as test (the test's project)</option>
            <option value="execution">Same as the Test Execution</option>
            <option value="dedicated">Dedicated project…</option>
          </select>
        </label>
      )}
      {!backendIsKiwi && bugProjectMode === "dedicated" && (
        <label>
          Dedicated bug project key
          <input
            value={bugProjectKey}
            onChange={(e) => setBugProjectKey(e.target.value.toUpperCase())}
            placeholder="e.g. DEFECTS — project where bugs are filed"
            spellCheck={false}
          />
        </label>
      )}
      {!isEdit && reusable.length > 0 && (
        <label>
          {backendIsKiwi ? "Credential" : "Personal Access Token"}
          <select
            value={reuseFrom}
            onChange={(e) => setReuseFrom(e.target.value)}
          >
            <option value="">Enter new credentials…</option>
            {reusable.map((p) => (
              <option key={p.id} value={p.id}>
                Reuse credential from: {p.name} ({p.projectKey})
              </option>
            ))}
          </select>
        </label>
      )}

      {(isEdit || reuseFrom === "") && backendIsKiwi && (
        <>
          <label>
            Kiwi username
            <input
              value={kiwiUsername}
              onChange={(e) => setKiwiUsername(e.target.value)}
              placeholder={
                isEdit
                  ? "Leave blank (with password) to keep the current credential"
                  : "Kiwi TCMS username"
              }
              autoComplete="off"
              spellCheck={false}
            />
          </label>
          <label>
            Kiwi password
            <div className="pat-field">
              <input
                type={showToken ? "text" : "password"}
                value={kiwiPassword}
                onChange={(e) => setKiwiPassword(e.target.value)}
                placeholder={
                  isEdit
                    ? "Leave blank (with username) to keep the current credential"
                    : "Kiwi TCMS password"
                }
                autoComplete="off"
              />
              <button
                type="button"
                className="btn btn-ghost pat-toggle"
                onClick={() => setShowToken((v) => !v)}
                title={showToken ? "Hide password" : "Show password"}
                aria-label={showToken ? "Hide password" : "Show password"}
              >
                {showToken ? "🙈" : "👁"}
              </button>
            </div>
            {kiwiCredInvalid && (
              <span className="field-error">
                Enter both username and password, or leave both blank to keep
                the current credential.
              </span>
            )}
          </label>
        </>
      )}

      {(isEdit || reuseFrom === "") && !backendIsKiwi && (
        <label>
          {reusable.length > 0 && !isEdit ? "New token" : "Personal Access Token"}
          <div className="pat-field">
            <input
              type={showToken ? "text" : "password"}
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder={
                isEdit
                  ? "Leave blank to keep the current token"
                  : "Jira PAT — stored in Windows Credential Manager"
              }
              autoComplete="off"
            />
            <button
              type="button"
              className="btn btn-ghost pat-toggle"
              onClick={() => setShowToken((v) => !v)}
              title={showToken ? "Hide token" : "Show token"}
              aria-label={showToken ? "Hide token" : "Show token"}
            >
              {showToken ? "🙈" : "👁"}
            </button>
          </div>
        </label>
      )}

      <details className="profile-form-advanced">
        <summary>Advanced: TLS / certificate settings</summary>
        <label>
          CA certificate (PEM, optional)
          <textarea
            value={caCert}
            onChange={(e) => setCaCert(e.target.value)}
            placeholder={"-----BEGIN CERTIFICATE-----\n…\n-----END CERTIFICATE-----"}
            rows={5}
            spellCheck={false}
          />
          <span className="field-hint">
            Paste a PEM-encoded CA certificate to trust when connecting to this Jira
            instance. Required when the server uses a private or internal CA that is
            not in the system trust store (e.g. on macOS with a corporate CA).
          </span>
        </label>
        <label className="profile-form-checkbox">
          <input
            type="checkbox"
            checked={allowUntrustedTLS}
            onChange={(e) => setAllowUntrustedTLS(e.target.checked)}
          />
          Allow untrusted certificate (skip TLS verification)
          <span className="field-hint field-hint-warn">
            Disables all TLS certificate checks. Only enable this for trusted
            internal servers where no CA certificate is available. This is insecure
            and should not be used in production.
          </span>
        </label>
      </details>

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
        {extraActions && <div className="profile-form-extra">{extraActions}</div>}
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
