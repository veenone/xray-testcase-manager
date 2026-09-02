import { useMemo, useState } from "react";
import { useProfile } from "../contexts/ProfileContext";
import { Modal } from "./Modal";
import { announce } from "./LiveRegion";
import { BulkRenameTests, errMsg } from "../api";
import type { BulkEditResult, TestRename } from "../api";
import { useTestSummaries } from "../queries/summaries";
import { computeRenames, renameCounts, SUMMARY_MAX } from "../lib/rename";

interface Props {
  testKeys: string[];
  onComplete: (result: BulkEditResult) => void;
  onCancel: () => void;
}

type Mode = "prefix" | "suffix" | "both";

// PREVIEW_LIMIT caps rendered rows so a 500-test selection still types
// smoothly. The counts above the list are computed across every row, so the
// summary line stays true even when the list is cut short.
const PREVIEW_LIMIT = 200;

const TITLE_ID = "bulk-rename-title";

export function BulkRenameModal({ testKeys, onComplete, onCancel }: Props) {
  const { activeId } = useProfile();
  const [mode, setMode] = useState<Mode>("prefix");
  const [prefix, setPrefix] = useState("");
  const [suffix, setSuffix] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState<BulkEditResult | null>(null);

  const summariesQuery = useTestSummaries(activeId, testKeys);
  const tests = useMemo(() => summariesQuery.data ?? [], [summariesQuery.data]);

  // A hidden input must never contribute to the result, so the affix a mode
  // does not use is read as empty rather than merely being off-screen.
  const activePrefix = mode === "suffix" ? "" : prefix;
  const activeSuffix = mode === "prefix" ? "" : suffix;

  const rows = useMemo(
    () => computeRenames(tests, { prefix: activePrefix, suffix: activeSuffix }),
    [tests, activePrefix, activeSuffix],
  );
  const counts = useMemo(() => renameCounts(rows), [rows]);

  const shown = rows.slice(0, PREVIEW_LIMIT);
  const typed = activePrefix !== "" || activeSuffix !== "";
  const canApply = counts.changed > 0 && !busy && result === null;
  // Keys can vanish between selection and modal open (a sync deleted them).
  // Say so rather than quietly previewing fewer rows than the toolbar counted.
  const missing = testKeys.length - tests.length;
  // An affix that is only spaces is legitimate but usually a typo, and trailing
  // spaces are invisible in the preview.
  const affixIsBlank =
    (activePrefix !== "" && activePrefix.trim() === "") ||
    (activeSuffix !== "" && activeSuffix.trim() === "");

  async function apply() {
    const renames: TestRename[] = rows
      .filter((r) => r.state === "changed")
      // expectedBefore lets the backend reject a rename computed from a summary
      // a sync has since moved, instead of silently reverting it.
      .map((r) => ({ key: r.key, summary: r.after, expectedBefore: r.before }));
    if (renames.length === 0) return;

    setBusy(true);
    setError("");
    try {
      const r = await BulkRenameTests(activeId, renames);
      announce(`Renamed ${r.succeeded.length} tests`);
      // Matching BulkEditModal: a clean result closes, anything else stays open
      // so the failures can be read. With the expectedBefore guard a partial
      // failure is a normal outcome rather than an edge case.
      if (r.failed.length === 0) {
        onComplete(r);
        return;
      }
      setResult(r);
      setBusy(false);
    } catch (e) {
      setError(errMsg(e));
      setBusy(false);
    }
  }

  return (
    <Modal onClose={onCancel} className="modal bulk-modal" labelledBy={TITLE_ID}>
      <div className="pending-head">
        <h2 id={TITLE_ID}>
          Rename summaries ({testKeys.length}{" "}
          {testKeys.length === 1 ? "test" : "tests"})
        </h2>
        <button className="btn btn-ghost" onClick={onCancel} title="Close">
          ✕
        </button>
      </div>

      <div className="rename-body">
        <fieldset className="rename-mode">
          <legend>What to add</legend>
          {(["prefix", "suffix", "both"] as Mode[]).map((m) => (
            <label key={m}>
              <input
                type="radio"
                name="rename-mode"
                value={m}
                checked={mode === m}
                onChange={() => setMode(m)}
                disabled={result !== null}
              />
              {m === "prefix" ? "Prefix" : m === "suffix" ? "Suffix" : "Both"}
            </label>
          ))}
        </fieldset>

        {mode !== "suffix" && (
          <label className="rename-field">
            <span>Prefix</span>
            <input
              className="detail-input"
              autoFocus
              placeholder="[SMOKE] "
              value={prefix}
              onChange={(e) => setPrefix(e.target.value)}
              disabled={result !== null}
            />
          </label>
        )}
        {mode !== "prefix" && (
          <label className="rename-field">
            <span>Suffix</span>
            <input
              className="detail-input"
              placeholder=" (v2)"
              value={suffix}
              onChange={(e) => setSuffix(e.target.value)}
              disabled={result !== null}
            />
          </label>
        )}

        <p className="rename-counts" aria-live="polite">
          {!typed
            ? "Type a prefix or suffix to see what changes."
            : `${counts.changed} will change, ${counts.unchanged} unchanged, ${counts.tooLong} too long`}
        </p>

        {summariesQuery.isPending ? (
          <p className="muted">Loading summaries…</p>
        ) : summariesQuery.isError ? (
          <p className="error-text">{errMsg(summariesQuery.error)}</p>
        ) : (
          <>
            <ul className="rename-list">
              {shown.map((r) => (
                <li key={r.key} className={`rename-row rename-${r.state}`}>
                  <span className="mono rename-key">{r.key}</span>
                  <span className="rename-before">{r.before}</span>
                  <span className="rename-arrow" aria-hidden="true">
                    →
                  </span>
                  <span className="rename-after">{r.after}</span>
                  {r.reason && <span className="muted"> · {r.reason}</span>}
                </li>
              ))}
            </ul>
            {rows.length > PREVIEW_LIMIT && (
              <p className="muted">
                Showing {PREVIEW_LIMIT} of {rows.length.toLocaleString()}.
              </p>
            )}
          </>
        )}

        {missing > 0 && (
          <p className="muted">
            {missing} of the {testKeys.length} selected tests are no longer in
            the local cache.
          </p>
        )}
        {affixIsBlank && <p className="muted">This is only spaces.</p>}
        {counts.unchanged > 0 && typed && (
          <p className="muted">
            {counts.unchanged} tests already have this. They stay as they are.
          </p>
        )}
        {/* When the affix alone is near the limit every row fails, and a list of
            512 identical rows explains nothing. Say it once. */}
        {counts.changed === 0 && counts.tooLong > 0 ? (
          <p className="warn-text">
            This is too long to add to any of the selected tests.
          </p>
        ) : (
          counts.tooLong > 0 && (
            <p className="warn-text">
              {counts.tooLong} tests would go over the {SUMMARY_MAX} character
              limit in Jira. They are left out.
            </p>
          )
        )}

        {result && (
          <div className="rename-result">
            {result.succeeded.length > 0 && (
              <p className="ok-text">
                ✓ Queued pending changes on {result.succeeded.length}{" "}
                {result.succeeded.length === 1 ? "test" : "tests"}.
              </p>
            )}
            {result.failed.length > 0 && (
              <>
                <p className="warn-text">Failed ({result.failed.length}):</p>
                <ul className="commit-fail-list">
                  {result.failed.map((f, i) => (
                    <li key={i}>
                      <span className="mono">{f.testKey}</span>: {f.error}
                    </li>
                  ))}
                </ul>
              </>
            )}
          </div>
        )}
        {error && <div className="error-text">{error}</div>}
      </div>

      <div className="pending-actions">
        <button
          className="btn"
          onClick={() => (result ? onComplete(result) : onCancel())}
          disabled={busy}
        >
          {result ? "Close" : "Cancel"}
        </button>
        {!result && (
          <button className="btn btn-primary" onClick={apply} disabled={!canApply}>
            {busy ? "Renaming…" : `Rename ${counts.changed} tests`}
          </button>
        )}
      </div>
    </Modal>
  );
}
