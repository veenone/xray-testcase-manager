import type { PendingChange, CommitResult } from "../api";

interface Props {
  changes: PendingChange[];
  onDiscard: (id: number) => Promise<void> | void;
  onDiscardAll: () => Promise<void> | void;
  onCommit: () => Promise<void> | void;
  onCommitIds: (ids: number[]) => Promise<void> | void;
  onJumpTo: (testKey: string) => void;
  onResolveOverride: (testKey: string, remoteVersion: string) => void;
  onResolveKeepRemote: (testKey: string) => void;
  onClose: () => void;
  committing: boolean;
  lastResult: CommitResult | null;
}

export function PendingChangesModal({
  changes,
  onDiscard,
  onDiscardAll,
  onCommit,
  onCommitIds,
  onJumpTo,
  onResolveOverride,
  onResolveKeepRemote,
  onClose,
  committing,
  lastResult,
}: Props) {
  // commitItem commits only this one row. The backend re-bases the Test's other
  // pending edits onto the new remote version afterward, so committing a single
  // item doesn't make the rest conflict.
  function commitItem(c: PendingChange) {
    onCommitIds([c.id]);
  }

  // discardAll confirms first — it reverts every uncommitted edit and can't be
  // undone.
  function discardAll() {
    if (
      window.confirm(
        `Discard all ${changes.length} pending change${
          changes.length === 1 ? "" : "s"
        }? This reverts every uncommitted edit and cannot be undone.`,
      )
    ) {
      onDiscardAll();
    }
  }

  const hasResult =
    lastResult &&
    (lastResult.succeeded.length > 0 ||
      lastResult.conflicted.length > 0 ||
      lastResult.failed.length > 0);

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div
        className="modal pending-modal"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="pending-head">
          <h2>Pending changes ({changes.length})</h2>
          <button className="btn btn-ghost" onClick={onClose} title="Close">
            ✕
          </button>
        </div>

        {hasResult && (
          <div className="commit-result">
            {lastResult!.succeeded.length > 0 && (
              <p className="ok-text">
                ✓ Committed {lastResult!.succeeded.length}{" "}
                {lastResult!.succeeded.length === 1 ? "test" : "tests"} to Jira.
              </p>
            )}
            {lastResult!.conflicted.length > 0 && (
              <div className="conflict-text">
                <p>
                  <strong>
                    Conflict{lastResult!.conflicted.length === 1 ? "" : "s"} (
                    {lastResult!.conflicted.length})
                  </strong>{" "}
                  — the remote test changed since you started editing. Choose
                  per test whether your edits win or the remote does.
                </p>
                <ul className="conflict-list">
                  {lastResult!.conflicted.map((c, i) => (
                    <li key={i} className="conflict-row">
                      <div className="conflict-row-main">
                        <button
                          className="link-btn mono"
                          onClick={() => onJumpTo(c.testKey)}
                          title="Open this test"
                        >
                          {c.testKey}
                        </button>{" "}
                        <span className="muted">
                          yours from{" "}
                          <code className="conflict-ts">{c.baseVersion}</code> ·
                          remote now{" "}
                          <code className="conflict-ts">{c.remoteVersion}</code>
                        </span>
                      </div>
                      <div className="conflict-row-actions">
                        <button
                          className="btn"
                          disabled={committing}
                          onClick={() =>
                            onResolveOverride(c.testKey, c.remoteVersion)
                          }
                          title="Re-base onto the remote version and push your edits over it"
                        >
                          Keep mine
                        </button>
                        <button
                          className="btn"
                          disabled={committing}
                          onClick={() => onResolveKeepRemote(c.testKey)}
                          title="Discard your edits and keep the remote version"
                        >
                          Keep remote
                        </button>
                      </div>
                    </li>
                  ))}
                </ul>
              </div>
            )}
            {lastResult!.failed.length > 0 && (
              <div className="error-text">
                <p>
                  Failed ({lastResult!.failed.length}) — these changes remain
                  in pending:
                </p>
                <ul className="commit-fail-list">
                  {lastResult!.failed.map((f, i) => (
                    <li key={i}>
                      {f.testKey && (
                        <span className="mono">{f.testKey}: </span>
                      )}
                      {f.error}
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </div>
        )}

        {changes.length === 0 ? (
          <p className="muted pending-empty">No pending changes.</p>
        ) : (
          <div className="pending-table-wrap">
            <table className="pending-table">
              <thead>
                <tr>
                  <th>Test</th>
                  <th>Field</th>
                  <th>Before</th>
                  <th>After</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {changes.map((c) => {
                  // Step entity_keys are "<testKey>:<xrayID>"; route the
                  // jump-to action to the parent test and label the row so
                  // the modal makes clear which kind of step change this is
                  // (edit / add / delete).
                  const isStepLike =
                    c.entityType.startsWith("test_step") ||
                    c.entityType === "custom_field";
                  const hasStepID = isStepLike && c.entityKey.includes(":");
                  const suffixLabel =
                    c.entityType === "custom_field" ? "field" : "step";
                  // test_run keys are "<execKey>:<testKey>" — jump to the test
                  // and show the execution as context.
                  const isRun = c.entityType === "test_run";
                  const parentKey = isRun
                    ? c.entityKey.substring(c.entityKey.indexOf(":") + 1)
                    : hasStepID
                      ? c.entityKey.split(":")[0]
                      : c.entityKey;
                  const stepID = hasStepID
                    ? c.entityKey.substring(c.entityKey.indexOf(":") + 1)
                    : "";
                  const runExec = isRun ? c.entityKey.split(":")[0] : "";
                  const { field, before, after } = describeChange(c);
                  return (
                    <tr key={c.id}>
                      <td>
                        <button
                          className="link-btn mono"
                          onClick={() => onJumpTo(parentKey)}
                          title="Open this test"
                        >
                          {parentKey}
                        </button>
                        {hasStepID && (
                          <span className="muted step-suffix">
                            {` · ${suffixLabel} `}
                            <span className="mono">{stepID}</span>
                          </span>
                        )}
                        {isRun && (
                          <span className="muted step-suffix">
                            {` · in `}
                            <span className="mono">{runExec}</span>
                          </span>
                        )}
                      </td>
                      <td>{field}</td>
                      <td className="pending-before">
                        {truncate(before, 100)}
                      </td>
                      <td className="pending-after">
                        {truncate(after, 100)}
                      </td>
                      <td className="pending-row-actions">
                        <button
                          className="btn btn-primary"
                          onClick={() => commitItem(c)}
                          disabled={committing}
                          title="Commit just this item to Jira"
                        >
                          Commit
                        </button>
                        <button
                          className="btn"
                          onClick={() => onDiscard(c.id)}
                          disabled={committing}
                        >
                          Discard
                        </button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}

        <div className="pending-actions">
          <p className="muted pending-footnote-inline">
            Successful commits leave this list; failures and conflicts stay
            and can be retried or discarded.
          </p>
          <button
            className="btn btn-danger"
            onClick={discardAll}
            disabled={committing || changes.length === 0}
            title="Revert every pending change"
          >
            Discard all
          </button>
          <button
            className="btn btn-primary"
            onClick={onCommit}
            disabled={committing || changes.length === 0}
          >
            {committing
              ? "Committing…"
              : changes.length === 1
                ? "Commit 1 change"
                : `Commit ${changes.length} changes`}
          </button>
        </div>
      </div>
    </div>
  );
}

// describeChange renders a pending row's field label and before/after for the
// table. Structural step ops (add / delete) carry the step as JSON in one
// column; we surface the action text so the row reads sensibly instead of
// dumping raw JSON.
function describeChange(c: PendingChange): {
  field: string;
  before: string;
  after: string;
} {
  switch (c.entityType) {
    case "test_step":
      return { field: `step:${c.field}`, before: c.beforeVal, after: c.afterVal };
    case "test_step_add":
      return { field: "step: new", before: "", after: stepAction(c.afterVal) };
    case "test_step_delete":
      return { field: "step: delete", before: stepAction(c.beforeVal), after: "" };
    case "test_step_order":
      return {
        field: "steps: reorder",
        before: orderSummary(c.beforeVal),
        after: orderSummary(c.afterVal),
      };
    case "test_membership_add":
      return {
        field: "allocate",
        before: "",
        after: membershipSummary(c.afterVal),
      };
    case "test_membership_remove":
      return {
        field: "deallocate",
        before: membershipSummary(c.afterVal),
        after: "",
      };
    case "test_container_add":
      return {
        field: "new container",
        before: "",
        after: containerSummary(c.afterVal),
      };
    case "precondition_set":
      return {
        field: "preconditions",
        before: keyCountSummary(c.beforeVal, "precondition"),
        after: keyCountSummary(c.afterVal, "precondition"),
      };
    case "requirement_set": {
      let beforeN = 0;
      try {
        beforeN = (JSON.parse(c.beforeVal) as unknown[]).length;
      } catch {
        beforeN = 0;
      }
      return {
        field: "requirements",
        before: `${beforeN} linked`,
        after: keyCountSummary(c.afterVal, "requirement"),
      };
    }
    case "precondition_add":
      return {
        field: "new precondition",
        before: "",
        after: stepActionLike(c.afterVal, "summary"),
      };
    case "precondition_edit":
      return {
        field: `precondition:${c.field}`,
        before: c.beforeVal,
        after: c.afterVal,
      };
    case "requirement_edit":
      return {
        field: `requirement:${c.field}`,
        before: c.beforeVal,
        after: c.afterVal,
      };
    case "requirement_delete":
      return {
        field: "delete requirement",
        before: stepActionLike(c.beforeVal, "summary"),
        after: "",
      };
    case "custom_field":
      return { field: "custom field", before: c.beforeVal, after: c.afterVal };
    case "test_create":
      return {
        field: "new test",
        before: "",
        after: stepActionLike(c.afterVal, "summary"),
      };
    case "test_review":
      return {
        field: "review",
        before: stepActionLike(c.beforeVal, "verdict"),
        after: stepActionLike(c.afterVal, "verdict"),
      };
    case "issue_comment":
      return { field: "comment", before: "", after: c.afterVal };
    case "test_run":
      return { field: "run result", before: c.beforeVal, after: c.afterVal };
    case "folder_create":
      return { field: "new folder", before: "", after: c.entityKey };
    case "folder_rename":
      return { field: "rename folder", before: "", after: c.entityKey };
    case "folder_delete":
      return { field: "delete folder", before: c.entityKey, after: "" };
    case "container_edit":
      return { field: "rename container", before: c.beforeVal, after: c.afterVal };
    case "container_delete":
      return {
        field: "delete container",
        before: stepActionLike(c.beforeVal, "summary"),
        after: "",
      };
    default:
      return { field: c.field, before: c.beforeVal, after: c.afterVal };
  }
}

// membershipSummary renders an allocation payload ({kind, members}) as
// "N tests" so the pending row reads clearly instead of showing raw JSON.
function membershipSummary(json: string): string {
  try {
    const p = JSON.parse(json) as { members?: string[] };
    const n = p.members?.length ?? 0;
    return `${n} ${n === 1 ? "test" : "tests"}`;
  } catch {
    return json;
  }
}

// containerSummary renders a create-container payload ({kind, summary,
// members}) as 'summary (N tests)'.
function containerSummary(json: string): string {
  try {
    const p = JSON.parse(json) as { summary?: string; members?: string[] };
    const n = p.members?.length ?? 0;
    return `${p.summary ?? ""} (${n} ${n === 1 ? "test" : "tests"})`;
  } catch {
    return json;
  }
}

// orderSummary renders a step-order JSON array as a compact "N steps: a → b →
// …" line so the reorder row reads at a glance.
function orderSummary(json: string): string {
  try {
    const ids = JSON.parse(json) as string[];
    return `${ids.length} steps: ${ids.join(" → ")}`;
  } catch {
    return json;
  }
}

// stepAction pulls the human-readable action out of a step JSON snapshot,
// falling back to the raw string if it isn't the expected shape.
function stepAction(json: string): string {
  try {
    const s = JSON.parse(json) as { action?: string };
    return s.action ?? "";
  } catch {
    return json;
  }
}

// keyCountSummary renders a JSON key-list as "N <noun>(s)".
function keyCountSummary(json: string, noun: string): string {
  try {
    const keys = JSON.parse(json) as string[];
    const n = keys.length;
    return `${n} ${noun}${n === 1 ? "" : "s"}`;
  } catch {
    return json;
  }
}

// stepActionLike pulls a named string field out of a JSON object payload,
// falling back to the raw string.
function stepActionLike(json: string, field: string): string {
  try {
    const o = JSON.parse(json) as Record<string, unknown>;
    const v = o[field];
    return typeof v === "string" ? v : json;
  } catch {
    return json;
  }
}

function truncate(s: string, n: number): string {
  if (!s) return "";
  if (s.length <= n) return s;
  return s.slice(0, n) + "…";
}
