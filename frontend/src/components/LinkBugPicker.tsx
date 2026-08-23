import { useEffect, useMemo, useState } from "react";
import { ListBugsWithTests, LinkExistingBugToRun, errMsg } from "../api";
import type { BugWithTests } from "../api";
import { Modal } from "./Modal";

interface Props {
  profileId: string;
  execKey: string;
  testKey: string;
  // Bug keys already linked to this row, so they can be shown as disabled
  // in the cached list instead of offered again.
  existingKeys: string[];
  onClose: () => void;
  onLinked: () => void;
}

// A Jira issue key: one or more letters/digits starting with a letter, a
// dash, then digits (e.g. "PROJ-123"). Matches the shape used elsewhere for
// key validation (e.g. NEW- placeholder keys are excluded by callers, not
// this regex).
const BUG_KEY_RE = /^[A-Z][A-Z0-9]*-\d+$/;

// LinkBugPicker links an existing bug (already synced into the local cache,
// or any Jira key typed directly) to one Test's run result in a Test
// Execution (RND_P_4TFINT_05-296). It offers two ways to pick a bug: a
// searchable list drawn from ListBugsWithTests (works offline in demo mode,
// since it reads the local cache), and a free-text key entry for any bug not
// yet synced. Styled after CreateBugModal's modal-overlay dialog.
export function LinkBugPicker({
  profileId,
  execKey,
  testKey,
  existingKeys,
  onClose,
  onLinked,
}: Props) {
  const [bugs, setBugs] = useState<BugWithTests[]>([]);
  const [loading, setLoading] = useState(true);
  const [query, setQuery] = useState("");
  const [freeKey, setFreeKey] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  // Load the cached bug list lazily, on open.
  useEffect(() => {
    let cancelled = false;
    ListBugsWithTests(profileId)
      .then((bs) => {
        if (!cancelled) setBugs(bs ?? []);
      })
      .catch(() => {
        if (!cancelled) setBugs([]);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [profileId]);

  const existing = useMemo(() => new Set(existingKeys), [existingKeys]);

  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    const filtered = q
      ? bugs.filter(
          (b) =>
            b.key.toLowerCase().includes(q) ||
            b.summary.toLowerCase().includes(q),
        )
      : bugs;
    // Cap the rendered list — cached bug tables can be large and this is a
    // type-to-narrow picker, not a paged browse.
    return filtered.slice(0, 50);
  }, [bugs, query]);

  async function link(bugKey: string) {
    const key = bugKey.trim().toUpperCase();
    if (!key || busy) return;
    setBusy(true);
    setError("");
    try {
      await LinkExistingBugToRun(profileId, execKey, testKey, key);
      onLinked();
      onClose();
    } catch (e) {
      setError(errMsg(e));
      setBusy(false);
    }
  }

  const freeValid = BUG_KEY_RE.test(freeKey.trim().toUpperCase());

  return (
    <Modal onClose={onClose} className="modal" labelledBy="link-bug-title">
        <div className="pending-head">
          <h2 id="link-bug-title">Link bug to {testKey}</h2>
          <button
            className="btn btn-ghost"
            onClick={onClose}
            title="Cancel"
            aria-label="Cancel"
          >
            ✕
          </button>
        </div>
        <div className="bug-form">
          <label>
            Search cached bugs
            <input
              className="multiselect-search"
              type="search"
              autoFocus
              placeholder="Key or summary…"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && shown.length === 1) link(shown[0].key);
              }}
            />
          </label>
          {loading ? (
            <p className="muted">Loading cached bugs…</p>
          ) : (
            <ul className="multiselect-list link-bug-list">
              {shown.length === 0 && <li className="muted">No matches</li>}
              {shown.map((b) => (
                <li key={b.key}>
                  <button
                    type="button"
                    className={`searchable-option link-bug-option${existing.has(b.key) ? " is-selected" : ""}`}
                    disabled={busy || existing.has(b.key)}
                    onClick={() => link(b.key)}
                    title={
                      existing.has(b.key)
                        ? `${b.key} is already linked to this run`
                        : `Link ${b.key} to this run`
                    }
                  >
                    <span className="mono">{b.key}</span>
                    <span className="link-bug-summary">{b.summary}</span>
                    {b.status && (
                      <span className="status-pill link-bug-status">
                        {b.status}
                      </span>
                    )}
                  </button>
                </li>
              ))}
            </ul>
          )}
          <label>
            Or enter a bug key
            <div className="link-bug-freetext">
              <input
                className="mono"
                value={freeKey}
                placeholder="PROJECT-123"
                onChange={(e) => setFreeKey(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && freeValid) link(freeKey);
                }}
              />
              <button
                type="button"
                className="btn btn-primary"
                disabled={!freeValid || busy}
                onClick={() => link(freeKey)}
              >
                Link
              </button>
            </div>
          </label>
          {error && <div className="error-text">{error}</div>}
        </div>
        <div className="pending-actions">
          <button className="btn" onClick={onClose} disabled={busy}>
            Close
          </button>
        </div>
    </Modal>
  );
}
