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
  // checked holds lowercased words: checking one finding selects every finding
  // sharing that word, so a bulk action hits all occurrences across tests.
  const [checked, setChecked] = useState<Set<string>>(new Set());

  // Reset when the profile changes so findings from another profile never
  // linger in the view.
  useEffect(() => {
    setFindings([]);
    setScanned(false);
    setError("");
    setShowIgnore(false);
    setSelectedId(null);
    setChecked(new Set());
  }, [profileId]);

  async function scan() {
    if (!profileId) return;
    setLoading(true);
    setError("");
    setSelectedId(null);
    setChecked(new Set());
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

  const distinctWords = useMemo(
    () => Array.from(new Set(findings.map((f) => f.word.toLowerCase()))),
    [findings],
  );
  const allChecked =
    distinctWords.length > 0 && distinctWords.every((w) => checked.has(w));
  const checkedFindings = useMemo(
    () => sorted.filter((f) => checked.has(f.word.toLowerCase())),
    [sorted, checked],
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

  function toggleWord(word: string) {
    const lower = word.toLowerCase();
    setChecked((prev) => {
      const next = new Set(prev);
      if (next.has(lower)) next.delete(lower);
      else next.add(lower);
      return next;
    });
  }

  function toggleAll() {
    setChecked(allChecked ? new Set() : new Set(distinctWords));
  }

  // applyReplacement writes an arbitrary replacement for the flagged word
  // through the pending-change pipeline, then re-scans.
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

  // bulkApply replaces every checked finding, tolerating individual stale
  // failures (a re-scan afterwards surfaces anything that could not be applied,
  // e.g. two occurrences of the same word in one field).
  async function bulkApply(replacementFor: (f: Finding) => string) {
    const list = checkedFindings;
    if (list.length === 0) return;
    setLoading(true);
    setError("");
    let firstErr = "";
    for (const f of list) {
      const r = replacementFor(f).trim();
      if (!r) continue;
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
      } catch (e) {
        if (!firstErr) firstErr = errMsg(e);
      }
    }
    onChanged();
    await scan();
    setLoading(false);
    if (firstErr) {
      setError(`${firstErr} (some occurrences may need another pass)`);
    }
  }

  async function ignore(f: Finding) {
    try {
      await AddIgnoreWord(f.word);
      const lower = f.word.toLowerCase();
      setFindings((prev) => prev.filter((x) => x.word.toLowerCase() !== lower));
      if (selected && selected.word.toLowerCase() === lower) setSelectedId(null);
      setChecked((prev) => {
        const next = new Set(prev);
        next.delete(lower);
        return next;
      });
    } catch (e) {
      setError(errMsg(e));
    }
  }

  async function bulkIgnore() {
    const wordsToIgnore = Array.from(checked);
    if (wordsToIgnore.length === 0) return;
    setError("");
    try {
      for (const w of wordsToIgnore) await AddIgnoreWord(w);
      setFindings((prev) => prev.filter((x) => !checked.has(x.word.toLowerCase())));
      setChecked(new Set());
      setSelectedId(null);
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

  // Suggestions for the single selected word (same across its findings).
  const singleWord = checked.size === 1 ? Array.from(checked)[0] : "";
  const singleWordSuggestions = singleWord
    ? findings.find((f) => f.word.toLowerCase() === singleWord)?.suggestions ?? []
    : [];

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

      {checked.size > 0 && (
        <div className="msp-bulk">
          <span className="msp-bulk-count">
            {checkedFindings.length} finding{checkedFindings.length === 1 ? "" : "s"}
            {" · "}
            {checked.size} word{checked.size === 1 ? "" : "s"} selected
          </span>
          {checked.size === 1 && singleWordSuggestions.length > 0 && (
            <div className="msp-bulk-suggests">
              <span className="muted">Replace all:</span>
              {singleWordSuggestions.map((s) => (
                <button
                  key={s}
                  className="suggestion-chip"
                  onClick={() => void bulkApply((f) => matchCase(f.word, s))}
                  title={`Replace all occurrences with "${s}"`}
                >
                  {s}
                </button>
              ))}
            </div>
          )}
          {checked.size === 1 && <BulkCustomReplace onReplace={(v) => void bulkApply(() => v)} />}
          <span className="msp-bulk-spacer" />
          <button className="btn" onClick={() => void bulkIgnore()}>
            Ignore selected
          </button>
          <button className="btn btn-ghost" onClick={() => setChecked(new Set())}>
            Clear
          </button>
        </div>
      )}

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
                  <th className="msp-check-col">
                    <input
                      type="checkbox"
                      checked={allChecked}
                      onChange={toggleAll}
                      title="Select all words"
                    />
                  </th>
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
                  const isChecked = checked.has(f.word.toLowerCase());
                  return (
                    <tr
                      key={id}
                      className={id === selectedId ? "selected" : ""}
                      onClick={() => setSelectedId(id)}
                    >
                      <td
                        className="msp-check-col"
                        onClick={(e) => e.stopPropagation()}
                      >
                        <input
                          type="checkbox"
                          checked={isChecked}
                          onChange={() => toggleWord(f.word)}
                          title={`Select all "${f.word}"`}
                        />
                      </td>
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

// BulkCustomReplace is a small input for replacing every selected occurrence
// with a typed word.
function BulkCustomReplace({ onReplace }: { onReplace: (value: string) => void }) {
  const [v, setV] = useState("");
  return (
    <div className="msp-bulk-replace">
      <input
        className="detail-input"
        placeholder="Replace all with…"
        value={v}
        onChange={(e) => setV(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && v.trim()) {
            e.preventDefault();
            onReplace(v.trim());
            setV("");
          }
        }}
      />
      <button
        className="btn"
        onClick={() => {
          if (v.trim()) {
            onReplace(v.trim());
            setV("");
          }
        }}
        disabled={!v.trim()}
      >
        Replace all
      </button>
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

// findRange locates the flagged word in the field text: the exact occurrence at
// the finding's offset when that still matches (ASCII-safe), else the first
// case-insensitive occurrence. Returns null when the word is not found.
function findRange(
  text: string,
  offset: number,
  length: number,
  word: string,
): [number, number] | null {
  if (
    offset >= 0 &&
    offset + length <= text.length &&
    text.slice(offset, offset + length).toLowerCase() === word.toLowerCase()
  ) {
    return [offset, offset + length];
  }
  const idx = text.toLowerCase().indexOf(word.toLowerCase());
  return idx >= 0 ? [idx, idx + word.length] : null;
}

// markedText renders text with [start,end) wrapped in a mark of the given class.
function markedText(text: string, start: number, end: number, cls: string) {
  return (
    <>
      {text.slice(0, start)}
      <mark className={cls}>{text.slice(start, end)}</mark>
      {text.slice(end)}
    </>
  );
}

// MisspellingDrawer is the right-side panel for the selected finding: the word,
// its suggestions, a custom replacement with a live before/after preview, an
// ignore action, and the full field text with the flagged word highlighted.
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

  const range =
    text !== null
      ? findRange(text, finding.offset, finding.length, finding.word)
      : null;
  const trimmed = custom.trim();

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
              if (e.key === "Enter" && trimmed) {
                e.preventDefault();
                onApply(finding, trimmed);
              }
            }}
          />
          <button
            className="btn btn-primary"
            onClick={() => onApply(finding, trimmed)}
            disabled={!trimmed}
          >
            Replace
          </button>
        </div>

        {trimmed && text !== null && range && (
          <div className="msp-preview">
            <div className="msp-preview-row">
              <span className="msp-preview-tag">Before</span>
              <span className="msp-preview-text">
                {markedText(text, range[0], range[1], "typo-hl")}
              </span>
            </div>
            <div className="msp-preview-row">
              <span className="msp-preview-tag">After</span>
              <span className="msp-preview-text">
                {text.slice(0, range[0])}
                <mark className="typo-hl-after">{trimmed}</mark>
                {text.slice(range[1])}
              </span>
            </div>
          </div>
        )}

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
            {range
              ? markedText(text, range[0], range[1], "typo-hl")
              : text}
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
