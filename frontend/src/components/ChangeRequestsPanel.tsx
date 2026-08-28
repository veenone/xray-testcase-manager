import { useCallback, useEffect, useState } from "react";
import {
  ListChangeRequests,
  CreateChangeRequest,
  UpdateChangeRequest,
  DeleteChangeRequest,
  GetCRImpact,
  SetCRDecision,
  errMsg,
} from "../api";
import type { ChangeRequest, CRImpactResult, Version } from "../api";
import { useConfirm } from "./useConfirm";
import { Modal } from "./Modal";

interface Props {
  profileId: string;
  canonicalId: string;
  versions: Version[];
  onChanged: () => void;
}

const EMPTY_FORM = {
  crKey: "",
  title: "",
  status: "open",
  targetVersionId: "",
  risk: "medium",
  description: "",
};

// ChangeRequestsPanel lists change requests for a canonical requirement,
// supports create/edit/delete, and shows the per-member impact + decisions.
export function ChangeRequestsPanel({ profileId, canonicalId, versions, onChanged }: Props) {
  const [crs, setCrs] = useState<ChangeRequest[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [selectedCR, setSelectedCR] = useState<string>("");
  const [impact, setImpact] = useState<CRImpactResult | null>(null);
  const [impactLoading, setImpactLoading] = useState(false);
  const [busy, setBusy] = useState(false);

  // Form for create/edit
  const [editing, setEditing] = useState(false);
  const [editId, setEditId] = useState<string | null>(null);
  const [form, setForm] = useState(EMPTY_FORM);

  const { confirm } = useConfirm();

  const loadCRs = useCallback(async () => {
    if (!profileId || !canonicalId) {
      setCrs([]);
      return;
    }
    setLoading(true);
    setError("");
    try {
      setCrs(await ListChangeRequests(profileId, canonicalId));
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setLoading(false);
    }
  }, [profileId, canonicalId]);

  useEffect(() => {
    void loadCRs();
  }, [loadCRs]);

  useEffect(() => {
    if (!selectedCR) {
      setImpact(null);
      return;
    }
    setImpactLoading(true);
    setImpact(null);
    void (async () => {
      try {
        setImpact(await GetCRImpact(profileId, selectedCR));
      } catch (e) {
        setError(errMsg(e));
      } finally {
        setImpactLoading(false);
      }
    })();
  }, [profileId, selectedCR]);

  function openCreate() {
    setEditId(null);
    setForm({ ...EMPTY_FORM, targetVersionId: versions[0]?.id ?? "" });
    setEditing(true);
  }

  function openEdit(cr: ChangeRequest) {
    setEditId(cr.id);
    setForm({
      crKey: cr.crKey,
      title: cr.title,
      status: cr.status,
      targetVersionId: cr.targetVersionId,
      risk: cr.risk,
      description: cr.description,
    });
    setEditing(true);
  }

  async function submitForm() {
    if (!form.title.trim()) return;
    setBusy(true);
    setError("");
    try {
      if (editId) {
        await UpdateChangeRequest(
          profileId, editId,
          form.crKey, form.title, form.status,
          form.targetVersionId, form.risk, form.description,
        );
      } else {
        const id = await CreateChangeRequest(
          profileId, canonicalId,
          form.crKey, form.title, form.status,
          form.targetVersionId, form.risk, form.description,
        );
        setSelectedCR(id);
      }
      setEditing(false);
      setEditId(null);
      await loadCRs();
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }

  async function deleteCR(cr: ChangeRequest) {
    const ok = await confirm({
      title: `Delete CR "${cr.title}"?`,
      message: "All decisions on this change request will also be removed.",
      danger: true,
    });
    if (!ok) return;
    setBusy(true);
    setError("");
    try {
      await DeleteChangeRequest(profileId, cr.id);
      if (selectedCR === cr.id) setSelectedCR("");
      await loadCRs();
      onChanged();
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }

  async function setDecision(requirementKey: string, decision: string) {
    if (!selectedCR) return;
    try {
      await SetCRDecision(profileId, selectedCR, requirementKey, decision, "");
      setImpact(await GetCRImpact(profileId, selectedCR));
    } catch (e) {
      setError(errMsg(e));
    }
  }

  return (
    <div className="cov-cr-panel">
      <div className="cov-cr-header">
        <h3 className="cov-section-title">Change Requests</h3>
        <button className="btn btn-primary" onClick={openCreate} disabled={busy}>
          New CR
        </button>
      </div>

      {error && <div className="cov-error">{error}</div>}
      {loading && <p className="cov-empty">Loading…</p>}

      {/* CR Form */}
      {editing && (
        <Modal onClose={() => setEditing(false)} className="modal cr-form-modal" labelledBy="cr-form-title">
            <div className="pending-head">
              <h2 id="cr-form-title">{editId ? "Edit Change Request" : "New Change Request"}</h2>
              <button className="btn btn-ghost" onClick={() => setEditing(false)} title="Close">✕</button>
            </div>
            <div className="cov-body">
              {error && <div className="cov-error">{error}</div>}

              <label className="cov-field-label">Key (optional)
                <input
                  className="cov-input"
                  placeholder="CR-001"
                  value={form.crKey}
                  onChange={(e) => setForm({ ...form, crKey: e.target.value })}
                />
              </label>

              <label className="cov-field-label">Title *
                <input
                  className="cov-input"
                  placeholder="Change request title…"
                  value={form.title}
                  onChange={(e) => setForm({ ...form, title: e.target.value })}
                  autoFocus
                />
              </label>

              <div className="cov-form-row">
                <label className="cov-field-label">Status
                  <select className="cov-select" value={form.status} onChange={(e) => setForm({ ...form, status: e.target.value })}>
                    <option value="open">Open</option>
                    <option value="accepted">Accepted</option>
                    <option value="rejected">Rejected</option>
                    <option value="withdrawn">Withdrawn</option>
                  </select>
                </label>

                <label className="cov-field-label">Risk
                  <select className="cov-select" value={form.risk} onChange={(e) => setForm({ ...form, risk: e.target.value })}>
                    <option value="low">Low</option>
                    <option value="medium">Medium</option>
                    <option value="high">High</option>
                  </select>
                </label>

                <label className="cov-field-label">Target version
                  <select className="cov-select" value={form.targetVersionId} onChange={(e) => setForm({ ...form, targetVersionId: e.target.value })}>
                    <option value="">— none —</option>
                    {versions.map((v) => (
                      <option key={v.id} value={v.id}>{v.name} [{v.status}]</option>
                    ))}
                  </select>
                </label>
              </div>

              <label className="cov-field-label">Description
                <textarea
                  className="cov-input cov-textarea"
                  placeholder="Describe the change…"
                  value={form.description}
                  onChange={(e) => setForm({ ...form, description: e.target.value })}
                  rows={3}
                />
              </label>
            </div>
            <div className="pending-actions">
              <button className="btn" onClick={() => setEditing(false)}>Cancel</button>
              <button className="btn btn-primary" disabled={busy || !form.title.trim()} onClick={() => void submitForm()}>
                {editId ? "Save" : "Create"}
              </button>
            </div>
        </Modal>
      )}

      {/* CR List */}
      {crs.length === 0 && !loading && (
        <p className="cov-empty">No change requests yet. Create one to track proposed changes.</p>
      )}

      <div className="cov-cr-list">
        {crs.map((cr) => (
          <div
            key={cr.id}
            className={`cov-cr-item${selectedCR === cr.id ? " cov-cr-item-active" : ""}`}
            onClick={() => setSelectedCR(selectedCR === cr.id ? "" : cr.id)}
          >
            <div className="cov-cr-item-head">
              <span className="cov-cr-key">{cr.crKey || cr.id.slice(0, 8)}</span>
              <span className="cov-cr-title">{cr.title}</span>
              <span className={`cov-badge cov-badge-cr-${cr.status}`}>{cr.status}</span>
              <span className={`cov-badge cov-badge-risk-${cr.risk}`}>{cr.risk}</span>
              {cr.targetVersionId && (
                <span className="cov-cr-version">
                  → {versions.find((v) => v.id === cr.targetVersionId)?.name ?? cr.targetVersionId}
                </span>
              )}
              <button className="btn" onClick={(e) => { e.stopPropagation(); openEdit(cr); }}>Edit</button>
              <button className="btn" onClick={(e) => { e.stopPropagation(); void deleteCR(cr); }}>Del</button>
            </div>
            {cr.description && <p className="cov-cr-desc">{cr.description}</p>}

            {/* Impact grid for selected CR */}
            {selectedCR === cr.id && (
              <div className="cov-cr-impact" onClick={(e) => e.stopPropagation()}>
                {impactLoading && <p className="cov-empty">Loading impact…</p>}
                {impact && (
                  <>
                    <div className="cov-cr-tallies">
                      <span className="cov-pass">Can accept: {impact.canAccept}</span>
                      <span className="cov-fail">Cannot: {impact.cannotAccept}</span>
                      <span className="cov-notrun">Pending: {impact.pending}</span>
                    </div>
                    {impact.decisions.length === 0 ? (
                      <p className="cov-empty">No member requirements linked to this canonical. Link members to collect decisions.</p>
                    ) : (
                      <table className="cov-table cov-cr-grid">
                        <thead>
                          <tr>
                            <th>Project</th>
                            <th>Requirement</th>
                            <th>Decision</th>
                          </tr>
                        </thead>
                        <tbody>
                          {impact.decisions.map((d) => (
                            <tr key={`${d.requirementKey}-${d.projectKey}`}>
                              <td>{d.projectKey || "—"}</td>
                              <td>{d.requirementKey}</td>
                              <td>
                                <select
                                  className="cov-select"
                                  value={d.decision}
                                  onChange={(e) => void setDecision(d.requirementKey, e.target.value)}
                                >
                                  <option value="pending">Pending</option>
                                  <option value="can_accept">Can accept</option>
                                  <option value="cannot_accept">Cannot accept</option>
                                </select>
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    )}
                  </>
                )}
              </div>
            )}
          </div>
        ))}
      </div>

    </div>
  );
}
