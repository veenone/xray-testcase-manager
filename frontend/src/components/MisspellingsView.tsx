import { useState } from "react";
import { ListMisspellings, ApplyCorrection, AddIgnoreWord, errMsg } from "../api";

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

  async function apply(f: Finding, replacement: string) {
    try {
      await ApplyCorrection(profileId, f.testKey, f.field, f.word, f.offset, f.length, replacement);
      setFindings((prev) => prev.filter((x) => x !== f));
      onChanged();
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
      <div className="toolbar">
        <button onClick={scan} disabled={loading || !profileId}>
          {loading ? "Scanning…" : "Scan for typos"}
        </button>
        {scanned && !loading && (
          <span className="muted">
            {findings.length} {findings.length === 1 ? "issue" : "issues"} found
          </span>
        )}
      </div>

      {error && <div className="error">{error}</div>}

      {scanned && findings.length === 0 && !loading && !error && (
        <p className="muted">No spelling issues found.</p>
      )}

      {findings.length > 0 && (
        <table className="board-table">
          <thead>
            <tr>
              <th>Test</th>
              <th>Field</th>
              <th>Word</th>
              <th>Context</th>
              <th>Suggestions</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {findings.map((f, i) => (
              <tr key={`${f.testKey}-${f.field}-${f.offset}-${i}`}>
                <td>{f.testKey}</td>
                <td>{FIELD_LABEL[f.field] ?? f.field}</td>
                <td className="typo-word">{f.word}</td>
                <td className="typo-snippet">{f.snippet}</td>
                <td>
                  {f.suggestions.length === 0 && <span className="muted">—</span>}
                  {f.suggestions.map((s) => (
                    <button key={s} className="suggestion-chip" onClick={() => apply(f, s)}>
                      {s}
                    </button>
                  ))}
                </td>
                <td>
                  <button className="link-btn" onClick={() => ignore(f)}>
                    Ignore
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
