import { useState } from "react";
import { Modal } from "./Modal";
import { BulkMoveToFolder, errMsg } from "../api";
import type { Folder, BulkEditResult } from "../api";

interface Props {
  profileId: string;
  testKeys: string[];
  folders: Folder[];
  onComplete: () => void;
  onCancel: () => void;
}

// BulkMoveModal moves the selected Tests to one Test Repository folder
// (FR-13.3). The folder list comes from the synced repository tree; the root
// is offered as an empty target.
export function BulkMoveModal({
  profileId,
  testKeys,
  folders,
  onComplete,
  onCancel,
}: Props) {
  const [target, setTarget] = useState("");
  const [applying, setApplying] = useState(false);
  const [applyError, setApplyError] = useState("");
  const [result, setResult] = useState<BulkEditResult | null>(null);

  async function apply() {
    setApplying(true);
    setApplyError("");
    try {
      const r = await BulkMoveToFolder(profileId, testKeys, target);
      setResult(r);
    } catch (e) {
      setApplyError(errMsg(e));
    } finally {
      setApplying(false);
    }
  }

  return (
    <Modal onClose={onCancel} className="modal bulk-modal" labelledBy="bulk-move-title">
        <div className="pending-head">
          <h2 id="bulk-move-title">
            Move to folder ({testKeys.length}{" "}
            {testKeys.length === 1 ? "test" : "tests"})
          </h2>
          <button className="btn btn-ghost" onClick={onCancel} title="Close">
            ✕
          </button>
        </div>

        {!result && (
          <div className="bulk-body">
            <label className="bulk-row">
              <span>Destination</span>
              <select
                value={target}
                onChange={(e) => setTarget(e.target.value)}
              >
                <option value="">(repository root)</option>
                {folders.map((f) => (
                  <option key={f.id} value={f.id}>
                    {f.id}
                  </option>
                ))}
              </select>
            </label>

            <p className="muted bulk-preview">
              The selected tests are moved locally and queued as pending
              changes; commit them from the Pending list. Tests already in the
              destination are unchanged.
            </p>

            {applyError && <div className="error-text">{applyError}</div>}
          </div>
        )}

        {result && (
          <div className="bulk-body">
            {result.succeeded.length > 0 && (
              <p className="ok-text">
                ✓ Moved {result.succeeded.length}{" "}
                {result.succeeded.length === 1 ? "test" : "tests"} to{" "}
                <span className="mono">{target || "(root)"}</span>.
              </p>
            )}
            {result.failed.length > 0 && (
              <div className="error-text">
                <p>Failed ({result.failed.length}):</p>
                <ul className="commit-fail-list">
                  {result.failed.map((f, i) => (
                    <li key={i}>
                      <span className="mono">{f.testKey}</span>: {f.error}
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </div>
        )}

        <div className="pending-actions">
          {!result ? (
            <>
              <button className="btn" onClick={onCancel} disabled={applying}>
                Cancel
              </button>
              <button
                className="btn btn-primary"
                onClick={apply}
                disabled={applying}
              >
                {applying ? "Moving…" : "Move"}
              </button>
            </>
          ) : (
            <button className="btn btn-primary" onClick={onComplete}>
              Done
            </button>
          )}
        </div>
    </Modal>
  );
}
