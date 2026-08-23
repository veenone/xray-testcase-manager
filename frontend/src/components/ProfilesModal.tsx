import { useEffect, useState } from "react";
import type { Profile } from "../api";
import { ProfileForm } from "./ProfileForm";
import { useConfirm } from "./useConfirm";
import { Modal } from "./Modal";

interface Props {
  profiles: Profile[];
  activeId: string;
  defaultProfileId: string;
  onClose: () => void;
  // Toggle the launch-default for a profile (clears it if already default).
  onSetDefault: (id: string) => void;
  // Export a profile's config (no credential) to a file.
  onExport: (id: string) => void;
  // Import a profile from a file; resolves to the created profile or null.
  onImport: () => Promise<Profile | null>;
  // Persist a created/edited profile (replace-or-append + refresh in App).
  onSaved: (p: Profile) => void;
  // Delete a profile. The modal asks for confirmation first via useConfirm.
  onDelete: (id: string) => Promise<void>;
}

// ProfilesModal is the master-detail profile manager: a list of every profile
// on the left (with a star to set the launch default) and the selected
// profile's ProfileForm on the right. New / Import create or bring in profiles;
// Export / Delete act on the selected one. App owns the profile state, so every
// mutation flows through the callback props.
export function ProfilesModal({
  profiles,
  activeId,
  defaultProfileId,
  onClose,
  onSetDefault,
  onExport,
  onImport,
  onSaved,
  onDelete,
}: Props) {
  // Nothing is open when the modal appears — the user must explicitly pick a
  // profile to edit or choose to create one. Auto-loading the active profile
  // here is exactly what let people edit their live connection by accident.
  const [selectedId, setSelectedId] = useState("");
  const [creating, setCreating] = useState(false);
  const { confirm, confirmUI } = useConfirm();

  // If the open profile disappears from under us (deleted), fall back to the
  // calm start state — never silently snap to the active profile instead.
  useEffect(() => {
    if (creating) return;
    if (selectedId && !profiles.some((p) => p.id === selectedId)) {
      setSelectedId("");
    }
  }, [profiles, selectedId, creating]);

  const selected = profiles.find((p) => p.id === selectedId) ?? null;
  const editingActive = !!selected && selected.id === activeId;

  function startCreate() {
    // Clear any open profile so Cancel returns to the start state, not back
    // to whatever was previously being edited.
    setSelectedId("");
    setCreating(true);
  }

  return (
    <>
    <Modal
      onClose={onClose}
      className="modal profiles-modal"
      labelledBy="profiles-modal-title"
    >
        <div className="profiles-modal-head">
          <div className="profiles-modal-head-text">
            <h2 id="profiles-modal-title">Manage Profiles</h2>
            <span className="profiles-modal-sub">
              {creating
                ? "A new profile won't become active until you switch to it."
                : "Editing here won't switch your active connection."}
            </span>
          </div>
          <button className="btn btn-ghost" onClick={onClose} title="Close" aria-label="Close">
            ✕
          </button>
        </div>

        <div className="profiles-modal-body">
          <div className="profiles-list">
            <div className="profiles-create-cta">
              <button
                className="btn btn-primary btn-block"
                onClick={startCreate}
                title="Create a new profile"
              >
                <svg
                  width="15"
                  height="15"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2.4"
                  strokeLinecap="round"
                  aria-hidden="true"
                >
                  <path d="M12 5v14M5 12h14" />
                </svg>
                Create new profile
              </button>
              <button
                className="btn btn-block profiles-import-btn"
                onClick={async () => {
                  const p = await onImport();
                  if (p) {
                    setCreating(false);
                    setSelectedId(p.id);
                  }
                }}
                title="Import a profile from a file"
              >
                Import from file…
              </button>
            </div>
            <div className="profiles-list-label">Your profiles</div>
            <ul className="profiles-list-items">
              {profiles.map((p) => (
                <li
                  key={p.id}
                  className={`profiles-list-row${
                    !creating && p.id === selectedId
                      ? " profiles-list-row-selected"
                      : ""
                  }`}
                  onClick={() => {
                    setCreating(false);
                    setSelectedId(p.id);
                  }}
                >
                  <button
                    className="profiles-star"
                    title={
                      defaultProfileId === p.id
                        ? "Default on launch (click to clear)"
                        : "Set as default on launch"
                    }
                    aria-label={
                      defaultProfileId === p.id ? "Clear default" : "Set as default"
                    }
                    onClick={(e) => {
                      e.stopPropagation();
                      onSetDefault(p.id);
                    }}
                  >
                    {defaultProfileId === p.id ? "★" : "☆"}
                  </button>
                  <span className="profiles-row-name">{p.name}</span>
                  <span className="profiles-row-key muted">
                    ({p.projectKey})
                  </span>
                  {p.id === activeId && (
                    <span className="profiles-badge-active">Active</span>
                  )}
                </li>
              ))}
            </ul>
          </div>

          <div className="profiles-detail">
            {creating ? (
              <>
                <div className="profiles-mode-head">
                  <span className="profiles-mode-kicker profiles-mode-kicker-new">
                    New profile
                  </span>
                </div>
                <div className="profiles-mode-title">Add a connection</div>
                <ProfileForm
                  hideHeading
                  profiles={profiles}
                  onCreated={(p) => {
                    onSaved(p);
                    setCreating(false);
                    setSelectedId(p.id);
                  }}
                  onCancel={() => setCreating(false)}
                />
              </>
            ) : selected ? (
              <>
                <div className="profiles-mode-head">
                  <span className="profiles-mode-kicker profiles-mode-kicker-edit">
                    Editing profile
                  </span>
                  <span className="profiles-mode-title">
                    · <span className="mono">{selected.name}</span>
                  </span>
                </div>
                {editingActive && (
                  <div className="profiles-active-caution">
                    <svg
                      width="15"
                      height="15"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                      aria-hidden="true"
                    >
                      <path d="M12 9v4m0 4h.01M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z" />
                    </svg>
                    <span>
                      <b>This is your active profile.</b> Saving updates the
                      Jira connection you&apos;re using right now. It
                      won&apos;t create a new one.
                    </span>
                  </div>
                )}
                <ProfileForm
                  key={selected.id}
                  hideHeading
                  profile={selected}
                  profiles={profiles}
                  onCreated={(p) => {
                    onSaved(p);
                    setSelectedId(p.id);
                  }}
                  extraActions={
                    <>
                      <button
                        className="btn"
                        onClick={() => onExport(selected.id)}
                        title="Export this profile (without its token)"
                      >
                        Export
                      </button>
                      <button
                        className="btn btn-danger"
                        onClick={async () => {
                          const ok = await confirm({
                            title: `Delete profile "${selected.name}"?`,
                            message:
                              "This removes its stored token and all cached test data. This cannot be undone.",
                            confirmLabel: "Delete profile",
                            danger: true,
                          });
                          if (ok) await onDelete(selected.id);
                        }}
                        title="Delete this profile"
                      >
                        Delete
                      </button>
                    </>
                  }
                />
              </>
            ) : (
              <div className="profiles-empty">
                <div className="profiles-empty-mark">
                  <svg
                    width="22"
                    height="22"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="1.7"
                    aria-hidden="true"
                  >
                    <rect x="3" y="4" width="18" height="16" rx="2" />
                    <path d="M3 9h18" />
                    <circle cx="7" cy="6.5" r="0.6" fill="currentColor" />
                  </svg>
                </div>
                <h3>Nothing open yet</h3>
                <p>
                  Pick a profile on the left to edit it, or create a new one.
                  Your active connection stays as it is until you choose.
                </p>
                <button
                  className="btn btn-primary btn-block"
                  onClick={startCreate}
                >
                  Create new profile
                </button>
              </div>
            )}
          </div>
        </div>
    </Modal>
    {confirmUI}
    </>
  );
}
