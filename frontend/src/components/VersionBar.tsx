import { useState } from "react";
import {
  CreateVersion,
  CloneVersion,
  SetVersionStatus,
  DeleteVersion,
  RenameVersion,
} from "../api";
import type { Version } from "../api";
import { useConfirm } from "./useConfirm";
import { errMsg } from "../api";

interface VersionBarProps {
  versions: Version[];
  value: string;
  onChange: (id: string) => void;
  profileId: string;
  canonicalId: string;
  onChanged: () => void;
}

// VersionBar renders a version selector dropdown plus action buttons for
// creating, cloning, renaming/status-changing, and deleting versions.
export function VersionBar({
  versions,
  value,
  onChange,
  profileId,
  canonicalId,
  onChanged,
}: VersionBarProps) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  // New version inline form
  const [showNew, setShowNew] = useState(false);
  const [newName, setNewName] = useState("");
  const [newStatus, setNewStatus] = useState("planning");

  // Clone inline form
  const [showClone, setShowClone] = useState(false);
  const [cloneName, setCloneName] = useState("");
  const [cloneStatus, setCloneStatus] = useState("planning");

  // Edit status inline form
  const [showStatus, setShowStatus] = useState(false);
  const [editStatus, setEditStatus] = useState(value ? (versions.find((v) => v.id === value)?.status ?? "planning") : "planning");

  const { confirm, confirmUI } = useConfirm();

  const current = versions.find((v) => v.id === value);

  async function createVersion() {
    const name = newName.trim();
    if (!name) return;
    setBusy(true);
    setError("");
    try {
      const id = await CreateVersion(profileId, canonicalId, name, newStatus, "");
      setNewName("");
      setNewStatus("planning");
      setShowNew(false);
      onChanged();
      onChange(id);
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }

  async function cloneVersion() {
    if (!value) return;
    const name = cloneName.trim();
    if (!name) return;
    setBusy(true);
    setError("");
    try {
      const id = await CloneVersion(profileId, value, name, cloneStatus);
      setCloneName("");
      setCloneStatus("planning");
      setShowClone(false);
      onChanged();
      onChange(id);
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }

  async function changeStatus() {
    if (!value) return;
    setBusy(true);
    setError("");
    try {
      await SetVersionStatus(profileId, value, editStatus);
      setShowStatus(false);
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }

  async function deleteVersion() {
    if (!value || !current) return;
    const ok = await confirm({
      title: `Delete version "${current.name}"?`,
      message: "This removes the version and all its coverage data. This cannot be undone.",
      danger: true,
    });
    if (!ok) return;
    setBusy(true);
    setError("");
    try {
      await DeleteVersion(profileId, value);
      onChanged();
      onChange(versions.find((v) => v.id !== value)?.id ?? "");
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }

  async function renameVersion(name: string) {
    if (!value || !current) return;
    setBusy(true);
    setError("");
    try {
      await RenameVersion(profileId, value, name, current.status, current.notes);
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }
  void renameVersion; // referenced for completeness; exposed via RenameVersion

  if (versions.length === 0) {
    return (
      <div className="cov-version-bar">
        <span className="cov-muted">No versions yet.</span>
        <button className="btn btn-primary" onClick={() => setShowNew(true)} disabled={busy}>
          New version
        </button>
        {showNew && (
          <span className="cov-version-inline-form">
            <input
              className="cov-input"
              placeholder="Version name…"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter") void createVersion(); }}
              autoFocus
            />
            <select className="cov-select" value={newStatus} onChange={(e) => setNewStatus(e.target.value)}>
              <option value="planning">Planning</option>
              <option value="beta">Beta</option>
              <option value="stable">Stable</option>
              <option value="deprecated">Deprecated</option>
            </select>
            <button className="btn btn-primary" disabled={busy || !newName.trim()} onClick={() => void createVersion()}>Create</button>
            <button className="btn" onClick={() => setShowNew(false)}>Cancel</button>
          </span>
        )}
        {error && <span className="cov-error">{error}</span>}
        {confirmUI}
      </div>
    );
  }

  return (
    <div className="cov-version-bar">
      <label className="cov-version-label">Version:</label>
      <select
        className="cov-select"
        value={value}
        onChange={(e) => {
          onChange(e.target.value);
          setShowNew(false);
          setShowClone(false);
          setShowStatus(false);
        }}
      >
        {versions.map((v) => (
          <option key={v.id} value={v.id}>
            {v.name} [{v.status}]
          </option>
        ))}
      </select>

      {current && (
        <span className={`cov-badge cov-badge-${current.status}`}>{current.status}</span>
      )}

      <button
        className="btn"
        title="New version"
        onClick={() => { setShowNew(!showNew); setShowClone(false); setShowStatus(false); }}
      >
        New
      </button>
      <button
        className="btn"
        title="Clone current version"
        disabled={!value || busy}
        onClick={() => { setShowClone(!showClone); setShowNew(false); setShowStatus(false); }}
      >
        Clone
      </button>
      <button
        className="btn"
        title="Change status"
        disabled={!value || busy}
        onClick={() => {
          setEditStatus(current?.status ?? "planning");
          setShowStatus(!showStatus);
          setShowNew(false);
          setShowClone(false);
        }}
      >
        Status
      </button>
      <button
        className="btn"
        title="Delete version"
        disabled={!value || busy}
        onClick={() => void deleteVersion()}
      >
        Delete
      </button>

      {showNew && (
        <div className="cov-version-inline-form">
          <input
            className="cov-input"
            placeholder="Version name…"
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter") void createVersion(); }}
            autoFocus
          />
          <select className="cov-select" value={newStatus} onChange={(e) => setNewStatus(e.target.value)}>
            <option value="planning">Planning</option>
            <option value="beta">Beta</option>
            <option value="stable">Stable</option>
            <option value="deprecated">Deprecated</option>
          </select>
          <button className="btn btn-primary" disabled={busy || !newName.trim()} onClick={() => void createVersion()}>Create</button>
          <button className="btn" onClick={() => setShowNew(false)}>Cancel</button>
        </div>
      )}

      {showClone && (
        <div className="cov-version-inline-form">
          <input
            className="cov-input"
            placeholder="Clone name…"
            value={cloneName}
            onChange={(e) => setCloneName(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter") void cloneVersion(); }}
            autoFocus
          />
          <select className="cov-select" value={cloneStatus} onChange={(e) => setCloneStatus(e.target.value)}>
            <option value="planning">Planning</option>
            <option value="beta">Beta</option>
            <option value="stable">Stable</option>
            <option value="deprecated">Deprecated</option>
          </select>
          <button className="btn btn-primary" disabled={busy || !cloneName.trim()} onClick={() => void cloneVersion()}>Clone</button>
          <button className="btn" onClick={() => setShowClone(false)}>Cancel</button>
        </div>
      )}

      {showStatus && (
        <div className="cov-version-inline-form">
          <select className="cov-select" value={editStatus} onChange={(e) => setEditStatus(e.target.value)} autoFocus>
            <option value="planning">Planning</option>
            <option value="beta">Beta</option>
            <option value="stable">Stable</option>
            <option value="deprecated">Deprecated</option>
          </select>
          <button className="btn btn-primary" disabled={busy} onClick={() => void changeStatus()}>Set</button>
          <button className="btn" onClick={() => setShowStatus(false)}>Cancel</button>
        </div>
      )}

      {error && <div className="cov-error" style={{ marginTop: 4 }}>{error}</div>}
      {confirmUI}
    </div>
  );
}
