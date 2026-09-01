import { useEffect, useState } from "react";
import { useProfile } from "../contexts/ProfileContext";
import { Modal } from "./Modal";
import {
  ListContainers,
  AllocateTests,
  DeallocateTests,
  CreateContainerAndAllocate,
  errMsg,
} from "../api";
import type { Container } from "../api";

interface Props {
  testKeys: string[];
  onComplete: () => void;
  onCancel: () => void;
}

const KINDS: Array<{ value: string; label: string }> = [
  { value: "testset", label: "Test Set" },
  { value: "testplan", label: "Test Plan" },
  { value: "testexec", label: "Test Execution" },
];

// ApplyResult normalises the outcome of either path (allocate to existing /
// create-and-allocate) so the modal renders one summary.
interface ApplyResult {
  added: number;
  already: number;
  createdName?: string;
  target: string;
  removed?: boolean;
}

// BulkAllocateModal adds the selected Tests to a Test Set, Test Plan or Test
// Execution (FR-3.4–3.6, add-only) — either an existing container or a new one
// created on commit. Tests already in the chosen container are reported back.
export function BulkAllocateModal({
  testKeys,
  onComplete,
  onCancel,
}: Props) {
  const { activeId: profileId } = useProfile();
  const [action, setAction] = useState<"allocate" | "remove">("allocate");
  const [kind, setKind] = useState("testset");
  const [containers, setContainers] = useState<Container[]>([]);
  const [target, setTarget] = useState("");
  const [createNew, setCreateNew] = useState(false);
  const [newName, setNewName] = useState("");
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [applying, setApplying] = useState(false);
  const [applyError, setApplyError] = useState("");
  const [result, setResult] = useState<ApplyResult | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setLoadError("");
    ListContainers(profileId, kind)
      .then((cs) => {
        if (cancelled) return;
        setContainers(cs ?? []);
        setTarget(cs && cs.length > 0 ? cs[0].key : "");
        // Fall back to create-new when nothing exists for this type.
        if (!cs || cs.length === 0) setCreateNew(true);
      })
      .catch((e) => {
        if (!cancelled) setLoadError(errMsg(e));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [profileId, kind]);

  const kindLabel = KINDS.find((k) => k.value === kind)?.label ?? "container";

  const useCreateNew = action === "allocate" && createNew;

  async function apply() {
    setApplying(true);
    setApplyError("");
    try {
      if (action === "remove") {
        if (!target) return;
        const r = await DeallocateTests(profileId, target, testKeys);
        setResult({
          added: r.removed.length,
          already: r.notMembers.length,
          target,
          removed: true,
        });
      } else if (useCreateNew) {
        const name = newName.trim();
        if (!name) return;
        const r = await CreateContainerAndAllocate(
          profileId,
          kind,
          name,
          testKeys,
        );
        setResult({ added: r.added, already: 0, createdName: name, target: r.tempKey });
      } else {
        if (!target) return;
        const r = await AllocateTests(profileId, target, testKeys);
        setResult({
          added: r.added.length,
          already: r.alreadyMembers.length,
          target,
        });
      }
    } catch (e) {
      setApplyError(errMsg(e));
    } finally {
      setApplying(false);
    }
  }

  const canApply = useCreateNew ? newName.trim().length > 0 : !!target;

  return (
    <Modal onClose={onCancel} className="modal bulk-modal" labelledBy="bulk-allocate-title">
        <div className="pending-head">
          <h2 id="bulk-allocate-title">
            Allocate ({testKeys.length}{" "}
            {testKeys.length === 1 ? "test" : "tests"})
          </h2>
          <button className="btn btn-ghost" onClick={onCancel} title="Close">
            ✕
          </button>
        </div>

        {!result && (
          <div className="bulk-body">
            <label className="bulk-row">
              <span>Action</span>
              <select
                value={action}
                onChange={(e) =>
                  setAction(e.target.value as "allocate" | "remove")
                }
              >
                <option value="allocate">Allocate</option>
                <option value="remove">Remove</option>
              </select>
            </label>

            <label className="bulk-row">
              <span>Type</span>
              <select value={kind} onChange={(e) => setKind(e.target.value)}>
                {KINDS.map((k) => (
                  <option key={k.value} value={k.value}>
                    {k.label}
                  </option>
                ))}
              </select>
            </label>

            {action === "allocate" && (
              <label className="bulk-row">
                <span>Create new</span>
                <input
                  type="checkbox"
                  checked={createNew}
                  onChange={(e) => setCreateNew(e.target.checked)}
                />
              </label>
            )}

            {useCreateNew ? (
              <label className="bulk-row">
                <span>Name</span>
                <input
                  value={newName}
                  onChange={(e) => setNewName(e.target.value)}
                  placeholder={`New ${kindLabel} name`}
                  autoFocus
                />
              </label>
            ) : (
              <label className="bulk-row">
                <span>Container</span>
                {loading ? (
                  <span className="muted">Loading…</span>
                ) : (
                  <select
                    value={target}
                    onChange={(e) => setTarget(e.target.value)}
                    disabled={containers.length === 0}
                  >
                    {containers.length === 0 && (
                      <option value="">None synced for this type</option>
                    )}
                    {containers.map((c) => (
                      <option key={c.key} value={c.key}>
                        {c.key} — {c.summary}
                      </option>
                    ))}
                  </select>
                )}
              </label>
            )}

            {loadError && <div className="error-text">{loadError}</div>}

            <p className="muted bulk-preview">
              {action === "remove"
                ? "Selected tests are removed from the container. Tests not in it are skipped."
                : useCreateNew
                  ? `A new ${kindLabel} is created in Jira on commit, then the selected tests are added to it.`
                  : "Selected tests are added to the container. Tests already in it are skipped."}{" "}
              Changes are queued locally; commit them from the Pending list.
            </p>

            {applyError && <div className="error-text">{applyError}</div>}
          </div>
        )}

        {result && (
          <div className="bulk-body">
            {result.createdName ? (
              <p className="ok-text">
                ✓ Queued new {kindLabel}{" "}
                <span className="mono">{result.createdName}</span> with{" "}
                {result.added} {result.added === 1 ? "test" : "tests"}.
              </p>
            ) : result.removed ? (
              <>
                {result.added > 0 && (
                  <p className="ok-text">
                    ✓ Queued removal of {result.added}{" "}
                    {result.added === 1 ? "test" : "tests"} from{" "}
                    <span className="mono">{result.target}</span>.
                  </p>
                )}
                {result.already > 0 && (
                  <p className="muted">
                    {result.already}{" "}
                    {result.already === 1 ? "test wasn't" : "tests weren't"} in
                    the container.
                  </p>
                )}
                {result.added === 0 && (
                  <p className="muted">Nothing to remove.</p>
                )}
              </>
            ) : (
              <>
                {result.added > 0 && (
                  <p className="ok-text">
                    ✓ Queued {result.added}{" "}
                    {result.added === 1 ? "test" : "tests"} for allocation to{" "}
                    <span className="mono">{result.target}</span>.
                  </p>
                )}
                {result.already > 0 && (
                  <p className="muted">
                    {result.already}{" "}
                    {result.already === 1 ? "test was" : "tests were"} already in
                    the container.
                  </p>
                )}
                {result.added === 0 && result.already === 0 && (
                  <p className="muted">Nothing to allocate.</p>
                )}
              </>
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
                disabled={applying || (!useCreateNew && loading) || !canApply}
              >
                {applying
                  ? "Working…"
                  : action === "remove"
                    ? "Remove"
                    : "Allocate"}
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
