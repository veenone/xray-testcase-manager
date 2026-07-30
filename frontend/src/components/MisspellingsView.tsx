import { useEffect, useMemo, useState } from "react";
import {
  ListMisspellings,
  ApplyCorrection,
  AddIgnoreWord,
  GetIgnoreWords,
  RemoveIgnoreWord,
  GetTest,
  errMsg,
} from "../api";
import type { TestCase } from "../api";

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

type SortKey = "test" | "field" | "word" | "context";

// findingId is a stable identity for a finding, so selection survives re-sorts.
function findingId(f: Finding): string {
  return `${f.testKey}|${f.field}|${f.offset}|${f.word}`;
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

export default function MisspellingsView({ profileId, onChanged }: Props) {
  const [findings, setFindings] = useState<Finding[]>([]);
  const [scanned, setScanned] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [showIgnore, setShowIgnore] = useState(false);
  const [sortKey, setSortKey] = useState<SortKey>("test");
  const [sortDir, setSortDir] = useState<"asc" | "desc">("asc");
  const [selectedId, setSelectedId] = useState<string | null>(null);

  // Reset when the profile changes so findings from another profile never
  // linger in the view.
  useEffect(() => {
    setFindings([]);
    setScanned(false);
    setError("");
    setShowIgnore(false);
    setSelectedId(null);
  }, [profileId]);

  async function scan() {
    if (!profileId) return;
    setLoading(true);
    setError("");
    setSelectedId(null);
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

  const sorted = useMemo(() => {
    const val = (f: Finding): string => {
      switch (sortKey) {
        case "test":
          return f.testKey.toLowerCase();
        case "field":
          return (FIELD_LABEL[f.field] ?? f.field).toLowerCase();
        case "word":
          return f.word.toLowerCase();
        case "context":
          return f.snippet.toLowerCase();
      }
    };
    const arr = [...findings];
    arr.sort((a, b) => {
      const av = val(a);
      const bv = val(b);
      const c = av < bv ? -1 : av > bv ? 1 : 0;
      return sortDir === "asc" ? c : -c;
    });
    return arr;
  }, [findings, sortKey, sortDir]);

  const selected = useMemo(
    () => sorted.find((f) => findingId(f) === selectedId) ?? null,
    [sorted, selectedId],
  );

  function toggleSort(k: SortKey) {
    if (sortKey === k) {
      setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    } else {
      setSortKey(k);
      setSortDir("asc");
    }
  }

  function sortIndicator(k: SortKey): string {
    if (sortKey !== k) return "";
    return sortDir === "asc" ? " ▲" : " ▼";
  }

  // applyReplacement writes an arbitrary replacement for the flagged word
  // through the pending-change pipeline, then re-scans. Used both by the
  // suggestion chips (matchCased) and the drawer's custom-replacement input.
  async function applyReplacement(f: Finding, replacement: string) {
    const r = replacement.trim();
    if (!r) return;
    try {
      await ApplyCorrection(
        profileId,
        f.testKey,
        f.field,
        f.word,
        f.offset,
        f.length,
        r,
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
      if (selected && selected.word.toLowerCase() === lower) setSelectedId(null);
    } catch (e) {
      setError(errMsg(e));
    }
  }

  const columns: Array<{ key: SortKey; label: string }> = [
    { key: "test", label: "Test" },
    { key: "field", label: "Field" },
    { key: "word", label: "Word" },
    { key: "context", label: "Context" },
  ];

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
        <button className="btn" onClick={() => setShowIgnore(true)}>
          Ignore list
        </button>
      </div>

      {error && <div className="error-text msp-error">{error}</div>}

      <div className="msp-split">
        <div className="msp-body">
          {!scanned && !loading && !error && (
            <div className="msp-empty">
              <p className="muted">
                Scan every synced test for spelling issues across its summary,
                description, and Gherkin / Definition bodies. Nothing changes
                until you apply a suggestion, and each fix is queued as a pending
                change.
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
                  {columns.map((c) => (
                    <th
                      key={c.key}
                      className="sortable"
                      onClick={() => toggleSort(c.key)}
                      title={`Sort by ${c.label}`}
                    >
                      {c.label}
                      {sortIndicator(c.key)}
                    </th>
                  ))}
                  <th>Suggestions</th>
                  <th aria-label="Actions" />
                </tr>
              </thead>
              <tbody>
                {sorted.map((f) => {
                  const id = findingId(f);
                  return (
                    <tr
                      key={id}
                      className={id === selectedId ? "selected" : ""}
                      onClick={() => setSelectedId(id)}
                    >
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
                            className="suggestion-chip"
                            onClick={(e) => {
                              e.stopPropagation();
                              void applyReplacement(f, matchCase(f.word, s));
                            }}
                            title={`Replace "${f.word}" with "${matchCase(f.word, s)}"`}
                          >
                            {s}
                          </button>
                        ))}
                      </td>
                      <td className="typo-actions">
                        <button
                          className="btn msp-ignore-btn"
                          onClick={(e) => {
                            e.stopPropagation();
                            void ignore(f);
                          }}
                        >
                          Ignore
                        </button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>

        {selected && (
          <MisspellingDrawer
            key={findingId(selected)}
            profileId={profileId}
            finding={selected}
            onClose={() => setSelectedId(null)}
            onApply={applyReplacement}
            onIgnore={ignore}
          />
        )}
      </div>

      {showIgnore && (
        <IgnoreListModal
          onClose={() => setShowIgnore(false)}
          onChangedList={() => {
            if (scanned) void scan();
          }}
        />
      )}
    </div>
  );
}

// fieldText extracts the full text of a finding's field from a fetched test.
function fieldText(tc: TestCase, field: string): string {
  switch (field) {
    case "summary":
      return tc.summary ?? "";
    case "description":
      return tc.description ?? "";
    case "cucumber_scenario":
      return tc.cucumberScenario ?? "";
    case "generic_definition":
      return tc.genericDefinition ?? "";
    default:
      return "";
  }
}

// highlight renders the field text with the flagged word wrapped in a yellow
// mark. It highlights the exact occurrence at the finding's offset when that
// still matches (ASCII-safe), else the first case-insensitive occurrence.
function highlight(text: string, offset: number, length: number, word: string) {
  let start = -1;
  let end = -1;
  if (
    offset >= 0 &&
    offset + length <= text.length &&
    text.slice(offset, offset + length).toLowerCase() === word.toLowerCase()
  ) {
    start = offset;
    end = offset + length;
  } else {
    const idx = text.toLowerCase().indexOf(word.toLowerCase());
    if (idx >= 0) {
      start = idx;
      end = idx + word.length;
    }
  }
  if (start < 0) return <>{text}</>;
  return (
    <>
      {text.slice(0, start)}
      <mark className="typo-hl">{text.slice(start, end)}</mark>
      {text.slice(end)}
    </>
  );
}

// MisspellingDrawer is the right-side panel for the selected finding: the word,
// its suggestions, a custom replacement, an ignore action, and the full field
// text with the flagged word highlighted in context.
function MisspellingDrawer({
  profileId,
  finding,
  onClose,
  onApply,
  onIgnore,
}: {
  profileId: string;
  finding: Finding;
  onClose: () => void;
  onApply: (f: Finding, replacement: string) => void;
  onIgnore: (f: Finding) => void;
}) {
  const [text, setText] = useState<string | null>(null);
  const [err, setErr] = useState("");
  const [custom, setCustom] = useState("");

  useEffect(() => {
    let cancelled = false;
    setText(null);
    setErr("");
    GetTest(profileId, finding.testKey)
      .then((tc) => {
        if (!cancelled) setText(fieldText(tc, finding.field));
      })
      .catch((e) => {
        if (!cancelled) setErr(errMsg(e));
      });
    return () => {
      cancelled = true;
    };
  }, [profileId, finding]);

  return (
    <aside className="msp-drawer">
      <div className="msp-drawer-head">
        <div className="msp-drawer-title">
          <span className="mono">{finding.testKey}</span>
          <span className="muted">
            {" · "}
            {FIELD_LABEL[finding.field] ?? finding.field}
          </span>
        </div>
        <button className="btn btn-ghost" onClick={onClose} title="Close">
          ✕
        </button>
      </div>

      <div className="msp-drawer-body">
        <div className="msp-drawer-word">
          <span className="typo-word">{finding.word}</span>
        </div>

        <div className="msp-drawer-field-label">Suggestions</div>
        {finding.suggestions.length > 0 ? (
          <div className="msp-drawer-suggests">
            {finding.suggestions.map((s) => (
              <button
                key={s}
                className="suggestion-chip"
                onClick={() => onApply(finding, matchCase(finding.word, s))}
                title={`Replace with "${matchCase(finding.word, s)}"`}
              >
                {s}
              </button>
            ))}
          </div>
        ) : (
          <p className="muted msp-no-suggests">No suggestions for this word.</p>
        )}

        <div className="msp-drawer-field-label">Replace with</div>
        <div className="msp-replace">
          <input
            className="detail-input"
            placeholder="Type a replacement…"
            value={custom}
            onChange={(e) => setCustom(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && custom.trim()) {
                e.preventDefault();
                onApply(finding, custom.trim());
              }
            }}
          />
          <button
            className="btn btn-primary"
            onClick={() => onApply(finding, custom.trim())}
            disabled={!custom.trim()}
          >
            Replace
          </button>
        </div>

        <div className="msp-drawer-actions">
          <button className="btn msp-ignore-btn" onClick={() => onIgnore(finding)}>
            Ignore word
          </button>
        </div>

        <div className="msp-drawer-field-label">Context</div>
        {err ? (
          <div className="error-text">{err}</div>
        ) : text === null ? (
          <p className="muted">Loading…</p>
        ) : (
          <pre className="msp-context-pre">
            {highlight(text, finding.offset, finding.length, finding.word)}
          </pre>
        )}
      </div>
    </aside>
  );
}

// IgnoreListModal manages the global spellcheck ignore list. It stays usable
// with many words: add at the top, filter to narrow, and remove via a compact
// wrapping grid of chips in a capped, scrollable area.
function IgnoreListModal({
  onClose,
  onChangedList,
}: {
  onClose: () => void;
  onChangedList: () => void;
}) {
  const [words, setWords] = useState<string[]>([]);
  const [input, setInput] = useState("");
  const [filter, setFilter] = useState("");
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

  const shown = useMemo(() => {
    const q = filter.trim().toLowerCase();
    return q ? words.filter((w) => w.includes(q)) : words;
  }, [words, filter]);

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
            Words here are skipped by every scan, across all profiles (the list
            is global, not per-profile). Add product terms, acronyms, or names
            the checker keeps flagging.
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

          <div className="ignore-manage">
            <span className="ignore-count">
              {filter.trim()
                ? `${shown.length} of ${words.length}`
                : `${words.length}`}{" "}
              word{words.length === 1 ? "" : "s"}
            </span>
            {words.length > 0 && (
              <input
                className="detail-input ignore-filter"
                placeholder="Filter…"
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
              />
            )}
          </div>

          {words.length === 0 ? (
            <p className="muted ignore-empty">No ignored words yet.</p>
          ) : shown.length === 0 ? (
            <p className="muted ignore-empty">No words match "{filter}".</p>
          ) : (
            <div className="ignore-chips">
              {shown.map((w) => (
                <span key={w} className="ignore-chip">
                  <span className="mono">{w}</span>
                  <button
                    className="ignore-chip-x"
                    onClick={() => void remove(w)}
                    title={`Remove "${w}"`}
                    aria-label={`Remove ${w}`}
                  >
                    ✕
                  </button>
                </span>
              ))}
            </div>
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
