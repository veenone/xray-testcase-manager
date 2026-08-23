import { useState } from "react";
import { Modal } from "./Modal";
import { BulkReviewTests, errMsg } from "../api";
import type { BulkEditResult } from "../api";

interface Props {
  profileId: string;
  testKeys: string[];
  onComplete: () => void;
  onCancel: () => void;
}

const REVIEWER_KEY = "xtm.reviewer";

const VERDICTS: Array<{ value: string; label: string }> = [
  { value: "approved", label: "Approve" },
  { value: "rejected", label: "Reject" },
  { value: "pending", label: "Mark pending" },
  { value: "", label: "Clear review" },
];

// BulkReviewModal applies one review verdict to every selected Test (bulk
// sign-off) — the batch counterpart to the per-test Review section.
export function BulkReviewModal({ profileId, testKeys, onComplete, onCancel }: Props) {
  const [verdict, setVerdict] = useState("approved");
  const [reviewer, setReviewer] = useState(
    () => localStorage.getItem(REVIEWER_KEY) ?? "",
  );
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState<BulkEditResult | null>(null);

  async function apply() {
    setBusy(true);
    setError("");
    localStorage.setItem(REVIEWER_KEY, reviewer.trim());
    try {
      const r = await BulkReviewTests(
        profileId,
        testKeys,
        verdict,
        reviewer.trim(),
        note.trim(),
      );
      setResult(r);
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal onClose={onCancel} className="modal bulk-modal" labelledBy="bulk-review-title">
        <div className="pending-head">
          <h2 id="bulk-review-title">Review {testKeys.length} test{testKeys.length === 1 ? "" : "s"}</h2>
          <button className="btn btn-ghost" onClick={onCancel} title="Close">
            ✕
          </button>
        </div>

        <div className="bulk-body">
          {result ? (
            <p className="ok-text">
              ✓ Reviewed {result.succeeded.length} test
              {result.succeeded.length === 1 ? "" : "s"}
              {result.failed.length > 0
                ? ` · ${result.failed.length} failed`
                : ""}
              . Commit from the Pending list.
            </p>
          ) : (
            <>
              <div className="bulk-row">
                <span>Verdict</span>
                <select
                  value={verdict}
                  onChange={(e) => setVerdict(e.target.value)}
                >
                  {VERDICTS.map((v) => (
                    <option key={v.value || "clear"} value={v.value}>
                      {v.label}
                    </option>
                  ))}
                </select>
              </div>
              <label className="bulk-row">
                <span>Reviewer</span>
                <input
                  value={reviewer}
                  onChange={(e) => setReviewer(e.target.value)}
                  placeholder="Reviewer name"
                />
              </label>
              <label className="bulk-row">
                <span>Note</span>
                <input
                  value={note}
                  onChange={(e) => setNote(e.target.value)}
                  placeholder="Review note (optional)"
                />
              </label>
              {error && <div className="error-text">{error}</div>}
            </>
          )}
        </div>

        <div className="pending-actions">
          {result ? (
            <button className="btn btn-primary" onClick={onComplete}>
              Done
            </button>
          ) : (
            <>
              <button className="btn" onClick={onCancel} disabled={busy}>
                Cancel
              </button>
              <button
                className="btn btn-primary"
                onClick={apply}
                disabled={busy}
              >
                {busy ? "Applying…" : `Apply to ${testKeys.length}`}
              </button>
            </>
          )}
        </div>
    </Modal>
  );
}
