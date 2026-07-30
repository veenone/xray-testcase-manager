import { useEffect, useState } from "react";
import {
  ListMisspellings,
  ApplyCorrection,
  AddIgnoreWord,
  GetIgnoreWords,
  RemoveIgnoreWord,
  errMsg,
} from "../api";

interface Finding {
  testKey: string;
  field: string;
  word: string;
  snippet: string;
  offset: number;
  length: number;
  suggestions: string[];
}

interface Props {
  profileId: string;
  refreshKey: number;
  onChanged: () => void;
}

const FIELD_LABEL: Record<string, string> = {
  summary: "Summary",
  description: "Description",
  cucumber_scenario: "Gherkin",
  generic_definition: "Definition",
};

export default function MisspellingsView({ profileId, onChanged }: Props) {
  const [findings, setFindings] = useState<Finding[]>([]);
  const [scanned, setScanned] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [showIgnore, setShowIgnore] = useState(false);

  // Reset when the profile changes so findings scanned from another profile
  // never linger in the view.
  useEffect(() => {
    setFindings([]);
    setScanned(false);
    setError("");
    setShowIgnore(false);
  }, [profileId]);

  async function scan() {
    if (!profileId) return;
    setLoading(true);
    setError("");
    try {
      const result = (await ListMisspellings(profileId)) as unknown as Finding[];
      setFindings(result ?? []);
      setScanned(true);
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setLoading(false);
    }
  }

  // matchCase preserves the original word's leading capital when applying a
  // lowercase suggestion (so "Recieve" -> "Receive", not "receive").
  function matchCase(original: string, suggestion: string): string {
    const isTitleCase = /^[A-Z]/.test(original) && !/^[A-Z]+$/.test(original);
    if (isTitleCase && /^[a-z]/.test(suggestion)) {
      return suggestion.charAt(0).toUpperCase() + suggestion.slice(1);
    }
    return suggestion;
  }

  async function apply(f: Finding, suggestion: string) {
    try {
      const replacement = matchCase(f.word, suggestion);
      await ApplyCorrection(
        profileId,
        f.testKey,
        f.field,
        f.word,
        f.offset,
        f.length,
        replacement,
      );
      onChanged();
      await scan();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  async function ignore(f: Finding) {
    try {
      await AddIgnoreWord(f.word);
      const lower = f.word.toLowerCase();
      setFindings((prev) => prev.filter((x) => x.word.toLowerCase() !== lower));
    } catch (e) {
      setError(errMsg(e));
    }
  }

  return (
    <div className="misspellings-view">
      <div className="msp-toolbar">
        <button
          className="btn btn-primary"
          onClick={scan}
          disabled={loading || !profileId}
        >
          {loading ? "Scanning…" : "Scan for typos"}
        </button>
        {scanned && !loading && (
          <span className="muted msp-count">
            {findings.length} {findings.length === 1 ? "issue" : "issues"} found
          </span>
        )}
        <span className="msp-toolbar-spacer" />
        <button className="btn btn-ghost" onClick={() => setShowIgnore(true)}>
          Ignore list
        </button>
      </div>

      {error && <div className="error-text msp-error">{error}</div>}

      <div className="msp-body">
        {!scanned && !loading && !error && (
          <div className="msp-empty">
            <p className="muted">
              Scan every synced test for spelling issues across its summary,
              description, and Gherkin / Definition bodies. Nothing changes until
              you apply a suggestion, and each fix is queued as a pending change.
            </p>
          </div>
        )}

        {scanned && findings.length === 0 && !loading && !error && (
          <div className="msp-empty">
            <p className="muted">No spelling issues found.</p>
          </div>
        )}

        {findings.length > 0 && (
          <table className="board-table msp-table">
            <thead>
              <tr>
                <th>Test</th>
                <th>Field</th>
                <th>Word</th>
                <th>Context</th>
                <th>Suggestions</th>
                <th aria-label="Actions" />
              </tr>
            </thead>
            <tbody>
              {findings.map((f, i) => (
                <tr key={`${f.testKey}-${f.field}-${f.offset}-${i}`}>
                  <td className="mono">{f.testKey}</td>
                  <td>{FIELD_LABEL[f.field] ?? f.field}</td>
                  <td className="typo-word">{f.word}</td>
                  <td className="typo-snippet" title={f.snippet}>
                    {f.snippet}
                  </td>
                  <td className="typo-suggestions">
                    {f.suggestions.length === 0 && (
                      <span className="muted">no suggestions</span>
                    )}
                    {f.suggestions.map((s) => (
                      <button
                        key={s}
                        className="btn btn-ghost suggestion-chip"
                        onClick={() => apply(f, s)}
                        title={`Replace "${f.word}" with "${matchCase(f.word, s)}"`}
                      >
                        {s}
                      </button>
                    ))}
                  </td>
                  <td className="typo-actions">
                    <button className="btn btn-ghost" onClick={() => ignore(f)}>
                      Ignore
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {showIgnore && (
        <IgnoreListModal
          onClose={() => setShowIgnore(false)}
          onChangedList={() => {
            // Reflect ignore-list edits in the current results.
            if (scanned) void scan();
          }}
        />
      )}
    </div>
  );
}

// IgnoreListModal manages the global spellcheck ignore list: add product terms,
// acronyms, or names the checker keeps flagging, and remove ones added by
// mistake. The list is shared across all profiles.
function IgnoreListModal({
  onClose,
  onChangedList,
}: {
  onClose: () => void;
  onChangedList: () => void;
}) {
  const [words, setWords] = useState<string[]>([]);
  const [input, setInput] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [dirty, setDirty] = useState(false);

  async function reload() {
    try {
      const w = await GetIgnoreWords();
      setWords((w ?? []).slice().sort());
    } catch (e) {
      setError(errMsg(e));
    }
  }

  useEffect(() => {
    void reload();
  }, []);

  async function add() {
    const w = input.trim().toLowerCase();
    if (!w) return;
    setBusy(true);
    setError("");
    try {
      await AddIgnoreWord(w);
      setInput("");
      setDirty(true);
      await reload();
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }

  async function remove(w: string) {
    setError("");
    try {
      await RemoveIgnoreWord(w);
      setDirty(true);
      await reload();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  function close() {
    if (dirty) onChangedList();
    onClose();
  }

  return (
    <div className="modal-overlay" onClick={close}>
      <div
        className="modal pending-modal ignore-modal"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="pending-head">
          <h2>Spellcheck ignore list</h2>
          <button className="btn btn-ghost" onClick={close} title="Close">
            ✕
          </button>
        </div>

        <div className="bulk-body">
          <p className="src-field-help">
            Words here are skipped by every scan, across all profiles. Add
            product terms, acronyms, or names the checker keeps flagging.
          </p>

          <div className="ignore-add">
            <input
              className="detail-input"
              placeholder="Add a word to ignore…"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  void add();
                }
              }}
              autoFocus
            />
            <button
              className="btn btn-primary"
              onClick={() => void add()}
              disabled={busy || !input.trim()}
            >
              Add
            </button>
          </div>

          {error && <div className="error-text">{error}</div>}

          {words.length === 0 ? (
            <p className="muted ignore-empty">No ignored words yet.</p>
          ) : (
            <ul className="ignore-list">
              {words.map((w) => (
                <li key={w}>
                  <span className="mono">{w}</span>
                  <button
                    className="btn btn-ghost ignore-remove"
                    onClick={() => void remove(w)}
                    title="Remove from ignore list"
                  >
                    ✕
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>

        <div className="pending-actions">
          <button className="btn btn-primary" onClick={close}>
            Done
          </button>
        </div>
      </div>
    </div>
  );
}
