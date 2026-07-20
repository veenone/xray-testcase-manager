import { useEffect, useState } from "react";
import { DeleteConnection, ListConnections, errMsg } from "../api";
import type { Connection } from "../api";
import { ProfileForm } from "./ProfileForm";
import { useConfirm } from "./useConfirm";

interface Props {
  // The active workspace (today, the active profile's id) whose connections
  // this modal manages (P6.3 B6a). The primary connection (id === activeId)
  // is the workspace's own connection and is not deletable — the backend
  // already refuses that delete; the modal just hides the button for it.
  activeId: string;
  onClose: () => void;
}

const BACKEND_LABEL: Record<string, string> = { xray: "Xray", kiwi: "Kiwi" };
const ROLE_LABEL: Record<string, string> = {
  source: "Source",
  target: "Target",
  both: "Both",
};

// ConnectionsModal is the master-detail connection manager for the active
// workspace: every connection on the left (primary marked, not deletable),
// the selected connection's form on the right. It mirrors ProfilesModal's
// layout but lists/saves connections instead of profiles, reusing
// ProfileForm's field UI via its "connection" mode (see ProfileForm.tsx) so
// the backend selector + Kiwi relabels + TLS fields aren't duplicated. This
// is the prerequisite UI for the bridge wizard (B6b): ListConnections here
// is exactly what will populate that wizard's source/target pickers, and the
// role each connection is given is how the wizard will tell them apart.
export function ConnectionsModal({ activeId, onClose }: Props) {
  const [connections, setConnections] = useState<Connection[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [selectedId, setSelectedId] = useState("");
  const [creating, setCreating] = useState(false);
  const { confirm, confirmUI } = useConfirm();

  async function refresh(selectAfter?: string) {
    setLoading(true);
    setError("");
    try {
      const list = await ListConnections(activeId);
      setConnections(list);
      setCreating(false);
      if (selectAfter) {
        setSelectedId(selectAfter);
      } else {
        setSelectedId((prev) =>
          list.some((c) => c.id === prev) ? prev : activeId || list[0]?.id || "",
        );
      }
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    refresh();
    // Re-list whenever the active workspace changes; refresh is stable
    // enough for this effect's purposes (it only closes over activeId).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeId]);

  const selected = connections.find((c) => c.id === selectedId) ?? null;

  async function handleDelete(c: Connection) {
    const ok = await confirm({
      title: `Delete connection "${c.name}"?`,
      message:
        "This removes its stored credential from this workspace. This cannot be undone.",
      confirmLabel: "Delete connection",
      danger: true,
    });
    if (!ok) return;
    try {
      await DeleteConnection(c.id);
      await refresh();
    } catch (e) {
      // The backend refuses to delete the primary connection; surface that
      // (and any other failure) inline rather than losing it silently.
      setError(errMsg(e));
    }
  }

  return (
    <>
      <div className="modal-overlay" onClick={onClose}>
        <div
          className="modal profiles-modal"
          onClick={(e) => e.stopPropagation()}
        >
          <div className="profiles-modal-head">
            <h2>Connections</h2>
            <button
              className="btn btn-ghost"
              onClick={onClose}
              title="Close"
              aria-label="Close"
            >
              ✕
            </button>
          </div>

          <div className="profiles-modal-body">
            <div className="profiles-list">
              {loading ? (
                <p className="muted">Loading…</p>
              ) : (
                <ul className="profiles-list-items">
                  {connections.map((c) => (
                    <li
                      key={c.id}
                      className={`connections-list-row${
                        !creating && c.id === selectedId
                          ? " profiles-list-row-selected"
                          : ""
                      }`}
                      onClick={() => {
                        setCreating(false);
                        setSelectedId(c.id);
                      }}
                    >
                      <div className="connections-row-top">
                        <span
                          className={`backend-chip backend-chip-${c.backend}`}
                        >
                          {BACKEND_LABEL[c.backend] ?? c.backend}
                        </span>
                        <span className="profiles-row-name">{c.name}</span>
                      </div>
                      <div className="connections-row-meta muted">
                        {c.url}
                        {" · "}
                        {ROLE_LABEL[c.role] ?? c.role}
                        {c.id === activeId && " · primary"}
                      </div>
                    </li>
                  ))}
                  {connections.length === 0 && (
                    <li className="muted">No connections yet.</li>
                  )}
                </ul>
              )}
              <div className="profiles-list-footer">
                <button
                  className="btn"
                  onClick={() => setCreating(true)}
                  title="Add a connection (e.g. a Kiwi target) to this workspace"
                >
                  + Add connection
                </button>
              </div>
            </div>

            <div className="profiles-detail">
              {error && <div className="error-text">{error}</div>}
              {creating ? (
                <ProfileForm
                  mode="connection"
                  workspaceId={activeId}
                  onSaved={(c) => refresh(c.id)}
                  onCancel={() => setCreating(false)}
                />
              ) : selected ? (
                <ProfileForm
                  key={selected.id}
                  mode="connection"
                  connection={selected}
                  workspaceId={activeId}
                  onSaved={(c) => refresh(c.id)}
                  extraActions={
                    selected.id !== activeId ? (
                      <button
                        className="btn btn-danger"
                        onClick={() => handleDelete(selected)}
                        title="Delete this connection"
                      >
                        Delete
                      </button>
                    ) : undefined
                  }
                />
              ) : (
                <p className="muted">
                  {loading
                    ? "Loading…"
                    : "Select a connection, or add a new one."}
                </p>
              )}
            </div>
          </div>
        </div>
      </div>
      {confirmUI}
    </>
  );
}
