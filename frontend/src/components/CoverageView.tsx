import { useCallback, useEffect, useMemo, useState } from "react";
import {
  ListCanonicalRequirements,
  CreateCanonicalRequirement,
  DeleteCanonicalRequirement,
  SetCanonicalMembers,
  ListCanonicalReuse,
  ListVersions,
  SetMemberVersion,
  GetParamModel,
  UpsertCoverageNode,
  DeleteCoverageNode,
  GetCoverageReport,
  ListCoverageGaps,
  ListCoverageCandidateTests,
  ListValueTests,
  SetValueTests,
  DetectStaleCoverageMappings,
  ImportCoverageTemplate,
  ExportCoverageReport,
  DownloadCoverageTemplate,
  SeedDemoCoverageExample,
  SeedPKCS11Reference,
  SeedEUICCReference,
  ListRequirementsWithCoverage,
  errMsg,
} from "../api";
import type {
  CanonicalRequirement,
  ParamModel,
  CoverageReport,
  CoverageGap,
  ReuseRow,
  CandidateTest,
  StaleMapping,
  ValueCoverage,
  Version,
} from "../api";
import { coverage } from "../../wailsjs/go/models";
import { useConfirm } from "./useConfirm";
import { VersionBar } from "./VersionBar";
import { ChangeRequestsPanel } from "./ChangeRequestsPanel";
import { VersionDashboard } from "./VersionDashboard";
import { CoverageMap } from "./CoverageMap";
import { BrowseTestsPicker } from "./CoverageTestPicker";
import { CoverageGuide } from "./CoverageGuide";
import { CoveragePublishPanel } from "./CoveragePublishPanel";
import { Modal } from "./Modal";

interface Props {
  profileId: string;
  refreshKey: number;
  isDemo?: boolean;
  demoVariant?: "pkcs" | "euicc" | "";
  onChanged?: () => void;
}

const STATUS_LABEL: Record<string, string> = {
  PASSED: "Passed",
  FAILED: "Failed",
  NOTRUN: "Not run",
  UNCOVERED: "Uncovered",
};

function statusClass(vc: ValueCoverage | undefined): string {
  if (!vc || !vc.tested) return "cov-pill cov-gap";
  switch (vc.runStatus) {
    case "PASSED":
      return "cov-pill cov-pass";
    case "FAILED":
      return "cov-pill cov-fail";
    default:
      return "cov-pill cov-notrun";
  }
}

function statusText(vc: ValueCoverage | undefined): string {
  if (!vc || !vc.tested) return "Gap";
  return STATUS_LABEL[vc.runStatus] ?? vc.runStatus;
}

// CoverageView is the bounded coverage module surface: a list of canonical
// functional requirements on the left, and for the selected one a parameter
// coverage matrix, a gap list, and a reuse view — all local-only (no Jira
// admin). Populate a model by importing the Excel template, then map tests to
// parameter values to drive the coverage %.
export function CoverageView({ profileId, refreshKey, isDemo, demoVariant, onChanged }: Props) {
  const [canon, setCanon] = useState<CanonicalRequirement[]>([]);
  const [selected, setSelected] = useState("");
  const [versions, setVersions] = useState<Version[]>([]);
  const [versionId, setVersionId] = useState("");
  const [model, setModel] = useState<ParamModel | null>(null);
  const [report, setReport] = useState<CoverageReport | null>(null);
  const [gaps, setGaps] = useState<CoverageGap[]>([]);
  const [reuse, setReuse] = useState<ReuseRow[]>([]);
  const [stale, setStale] = useState<StaleMapping[]>([]);
  const [tab, setTab] = useState<"guide" | "matrix" | "gaps" | "reuse" | "versions" | "map">("guide");
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [newName, setNewName] = useState("");
  const [newGroupName, setNewGroupName] = useState("");
  const [mapping, setMapping] = useState<{ valueId: string; label: string } | null>(null);
  const [showMembers, setShowMembers] = useState(false);
  const { confirm, confirmUI } = useConfirm();

  const loadList = useCallback(async () => {
    if (!profileId) {
      setCanon([]);
      return;
    }
    try {
      setCanon(await ListCanonicalRequirements(profileId));
    } catch (e) {
      setError(errMsg(e));
    }
  }, [profileId]);

  useEffect(() => {
    void loadList();
  }, [loadList, refreshKey]);

  // Load versions whenever the selected canonical changes; default to first
  // stable version, else first version.
  const loadVersions = useCallback(async () => {
    if (!profileId || !selected) {
      setVersions([]);
      setVersionId("");
      return;
    }
    try {
      const vs = await ListVersions(profileId, selected);
      setVersions(vs ?? []);
      setVersionId((prev) =>
        prev && (vs ?? []).some((v) => v.id === prev)
          ? prev
          : ((vs ?? []).find((v) => v.status === "stable")?.id ?? vs?.[0]?.id ?? "")
      );
    } catch (e) {
      setError(errMsg(e));
    }
  }, [profileId, selected]);

  useEffect(() => {
    void loadVersions();
  }, [loadVersions]);

  const loadSelected = useCallback(async () => {
    if (!profileId || !selected || !versionId) {
      setModel(null);
      setReport(null);
      setGaps([]);
      setReuse([]);
      setStale([]);
      return;
    }
    setLoading(true);
    setError("");
    try {
      const [m, r, g, ru, st] = await Promise.all([
        GetParamModel(profileId, versionId),
        GetCoverageReport(profileId, versionId),
        ListCoverageGaps(profileId, versionId),
        ListCanonicalReuse(profileId, selected),
        DetectStaleCoverageMappings(profileId, versionId),
      ]);
      setModel(m);
      setReport(r);
      setGaps(g);
      setReuse(ru);
      setStale(st);
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setLoading(false);
    }
  }, [profileId, selected, versionId]);

  useEffect(() => {
    void loadSelected();
  }, [loadSelected, refreshKey]);

  const reload = async () => {
    await loadVersions();
    await loadSelected();
    onChanged?.();
  };

  const staleByValue = useMemo(() => {
    const m: Record<string, number> = {};
    for (const s of stale) m[s.valueId] = (m[s.valueId] ?? 0) + 1;
    return m;
  }, [stale]);

  async function createCanonical() {
    const name = newName.trim();
    if (!name) return;
    setBusy(true);
    try {
      const id = await CreateCanonicalRequirement(profileId, name, "", "");
      setNewName("");
      await loadList();
      setSelected(id);
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }

  async function deleteCanonical(c: CanonicalRequirement) {
    const ok = await confirm({
      title: `Delete "${c.name}"?`,
      message: "This deletes its parameter model, mappings, and memberships, but leaves your Jira tests untouched.",
      danger: true,
    });
    if (!ok) return;
    try {
      await DeleteCanonicalRequirement(profileId, c.id);
      if (selected === c.id) setSelected("");
      await loadList();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  async function importTemplate() {
    if (!versionId) return;
    setBusy(true);
    setNotice("");
    try {
      const sum = await ImportCoverageTemplate(profileId, versionId);
      if (sum.groups === 0 && sum.values === 0) {
        setNotice("Import cancelled or empty.");
      } else {
        setNotice(
          `Imported ${sum.groups} groups, ${sum.values} values, ${sum.mappedTests} test mappings` +
            (sum.skipped ? ` (${sum.skipped} skipped)` : ""),
        );
      }
      await reload();
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }

  async function exportReport() {
    if (!versionId) return;
    setBusy(true);
    try {
      const path = await ExportCoverageReport(profileId, versionId);
      setNotice(path ? `Exported to ${path}` : "Export cancelled.");
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }

  async function downloadTemplate() {
    setBusy(true);
    setNotice("");
    try {
      const path = await DownloadCoverageTemplate();
      setNotice(path ? `Template saved to ${path}` : "Download cancelled.");
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }

  async function loadDemoExample() {
    setBusy(true);
    setNotice("");
    setError("");
    try {
      const id = await SeedDemoCoverageExample(profileId);
      await loadList();
      setSelected(id);
      onChanged?.();
      setNotice("Loaded the Login demo example (10/12 = 83.3%), aligned with the demo's Login tests.");
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }

  async function loadPkcsReference() {
    setBusy(true);
    setNotice("");
    setError("");
    try {
      const isEuicc = demoVariant === "euicc";
      const s = isEuicc
        ? await SeedEUICCReference(profileId)
        : await SeedPKCS11Reference(profileId);
      await loadList();
      onChanged?.();
      setNotice(
        isEuicc
          ? `Mapped eUICC coverage onto the synced demo-euicc data: ${s.features} features, ${s.versions} versions, ` +
              `${s.changeRequests} change requests, ${s.mappings} value-to-test mappings. ` +
              `Sync the demo-euicc profile first if you haven't already.`
          : `Mapped PKCS#11 coverage onto the synced demo-pkcs data: ${s.features} features, ${s.versions} versions, ` +
              `${s.changeRequests} change requests, ${s.mappings} value-to-test mappings. ` +
              `Sync the demo-pkcs profile first if you haven't already.`,
      );
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }

  async function addGroup() {
    const name = newGroupName.trim();
    if (!name || !selected || !versionId) return;
    try {
      await UpsertCoverageNode(
        profileId,
        coverage.NodeEdit.createFrom({
          kind: "group",
          canonicalId: selected,
          versionId,
          name,
          sortOrder: model?.groups.length ?? 0,
        }),
      );
      setNewGroupName("");
      await reload();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  async function deleteNode(kind: string, id: string) {
    try {
      await DeleteCoverageNode(profileId, kind, id);
      await reload();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  async function applyVersionToAll() {
    if (!versionId || reuse.length === 0) return;
    setBusy(true);
    try {
      await Promise.all(
        reuse.map((row) => SetMemberVersion(profileId, selected, row.requirementKey, versionId)),
      );
      await reload();
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }

  const selectedCanon = canon.find((c) => c.id === selected);
  const versionName = versions.find((v) => v.id === versionId)?.name ?? "";

  return (
    <div className="cov-root" data-tour="coverage-body">
      <aside className="cov-list">
        <div className="cov-list-head" data-tour="coverage-tools">
          <input
            className="cov-input"
            placeholder="New functional requirement…"
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") void createCanonical();
            }}
          />
          <button className="btn" disabled={busy || !newName.trim()} onClick={() => void createCanonical()}>
            Add
          </button>
        </div>
        {canon.length === 0 && <p className="cov-empty">No functional requirements yet. Add one, then import its parameter template.</p>}
        <ul className="cov-canon-list">
          {canon.map((c) => (
            <li
              key={c.id}
              className={`cov-canon${selected === c.id ? " cov-canon-active" : ""}`}
              onClick={() => setSelected(c.id)}
            >
              <span className="cov-canon-name">{c.name}</span>
              <span className="cov-canon-meta">{c.memberCount} proj</span>
              <button
                className="cov-x"
                title="Delete"
                onClick={(e) => {
                  e.stopPropagation();
                  void deleteCanonical(c);
                }}
              >
                ×
              </button>
            </li>
          ))}
        </ul>
      </aside>

      <section className="cov-detail">
        {error && <div className="cov-error">{error}</div>}
        {notice && <div className="cov-notice">{notice}</div>}
        {!selected ? (
          <>
            <nav className="cov-tabs">
              <button
                className={`cov-tab${tab === "map" ? " cov-tab-active" : ""}`}
                onClick={() => setTab("map")}
              >
                Coverage Map
              </button>
            </nav>
            {tab === "map" ? (
              <CoverageMap profileId={profileId} refreshKey={refreshKey} isDemo={isDemo} demoVariant={demoVariant} />
            ) : (
              <div className="cov-welcome">
                <h2>Parameter-level coverage</h2>
                <p>
                  This tab is a local workspace, and <strong>sync does not populate it</strong>. Sync only loads your
                  tests and requirements; you build the coverage model here from them.
                </p>
                <ol>
                  <li>
                    In the left panel, name a function (e.g. <code>C_Sign</code>) and click <strong>Add</strong>.
                  </li>
                  <li>
                    Select it, then <strong>Import template…</strong> to load a parameter workbook, or use
                    <strong> Add group</strong> to build it by hand.
                  </li>
                  <li>
                    Use <strong>Map…</strong> on each value to attach the tests that exercise it. The coverage % and
                    gap list update live.
                  </li>
                  <li>
                    Use <strong>Members</strong> to link the customer requirements that reuse this function.
                  </li>
                </ol>
                <div className="cov-welcome-actions">
                  <button className="btn" disabled={busy} onClick={() => void downloadTemplate()}>
                    Download blank template…
                  </button>
                  {isDemo && (
                    <button className="btn btn-primary" disabled={busy} onClick={() => void loadDemoExample()}>
                      Load demo example (Login)
                    </button>
                  )}
                  {isDemo && demoVariant && (
                    <button className="btn btn-primary" disabled={busy} onClick={() => void loadPkcsReference()}>
                      {demoVariant === "euicc" ? "Load eUICC coverage" : "Load PKCS#11 coverage"}
                    </button>
                  )}
                </div>
                {canon.length > 0 && <p className="cov-muted">Or pick an existing functional requirement on the left.</p>}
              </div>
            )}
          </>
        ) : (
          <>
            <header className="cov-detail-head">
              <h2>{selectedCanon?.name}</h2>
              {report && (
                <span className="cov-headline" title="Required parameter values covered">
                  {report.percent}% · {report.testedValues}/{report.totalValues}
                </span>
              )}
              <div className="cov-actions">
                <button className="btn" disabled={busy || !versionId} onClick={() => void importTemplate()}>
                  Import template…
                </button>
                <button className="btn" disabled={busy || !versionId} onClick={() => void exportReport()}>
                  Export report…
                </button>
                <button className="btn" disabled={busy} onClick={() => void downloadTemplate()}>
                  Blank template…
                </button>
                <button className="btn" onClick={() => setShowMembers(true)}>
                  Members ({reuse.length})
                </button>
              </div>
            </header>

            <VersionBar
              versions={versions}
              value={versionId}
              onChange={setVersionId}
              profileId={profileId}
              canonicalId={selected}
              onChanged={() => void reload()}
            />

            {stale.length > 0 && (
              <div className="cov-stale">
                ⚠ {stale.length} mapping{stale.length === 1 ? "" : "s"} reference tests no longer in the local cache (kept, not counted).
              </div>
            )}

            <CoveragePublishPanel profileId={profileId} versionId={versionId} />

            <nav className="cov-tabs">
              <button
                className={`cov-tab${tab === "guide" ? " cov-tab-active" : ""}`}
                onClick={() => setTab("guide")}
              >
                Guide
              </button>
              <button
                className={`cov-tab${tab === "matrix" ? " cov-tab-active" : ""}`}
                onClick={() => setTab("matrix")}
              >
                Coverage
              </button>
              <button
                className={`cov-tab${tab === "gaps" ? " cov-tab-active" : ""}`}
                onClick={() => setTab("gaps")}
              >
                Gaps ({gaps.length})
              </button>
              <button
                className={`cov-tab${tab === "reuse" ? " cov-tab-active" : ""}`}
                onClick={() => setTab("reuse")}
              >
                Reuse ({reuse.length})
              </button>
              <button
                className={`cov-tab${tab === "versions" ? " cov-tab-active" : ""}`}
                onClick={() => setTab("versions")}
              >
                Versions &amp; CRs
              </button>
              <button
                className={`cov-tab${tab === "map" ? " cov-tab-active" : ""}`}
                onClick={() => setTab("map")}
              >
                Coverage Map
              </button>
            </nav>

            {tab === "guide" ? (
              <CoverageGuide />
            ) : tab === "map" ? (
              <CoverageMap profileId={profileId} refreshKey={refreshKey} isDemo={isDemo} demoVariant={demoVariant} />
            ) : tab === "versions" ? (
              <div className="cov-versions-tab">
                <ChangeRequestsPanel
                  profileId={profileId}
                  canonicalId={selected}
                  versions={versions}
                  onChanged={() => void reload()}
                />
                <VersionDashboard
                  profileId={profileId}
                  canonicalId={selected}
                />
                <div className="cov-member-locks">
                  <div className="cov-member-locks-header">
                    <h3 className="cov-section-title">
                      Customer version locks
                      {versionId
                        ? ` (relative to ${versionName})`
                        : ""}
                    </h3>
                    {versionId && reuse.length > 0 && (
                      <button
                        className="btn"
                        disabled={busy}
                        onClick={() => void applyVersionToAll()}
                        title={`Set all member locks to ${versionName}`}
                      >
                        Apply {versionName} to all
                      </button>
                    )}
                  </div>
                  {versions.length === 0 ? (
                    <p className="cov-muted">No versions yet. Create a version first to assign member locks.</p>
                  ) : reuse.length === 0 ? (
                    <p className="cov-muted">No member requirements linked to this canonical.</p>
                  ) : (
                    <table className="cov-member-locks-table">
                      <thead>
                        <tr>
                          <th>Requirement</th>
                          <th>Project</th>
                          <th>Locked version</th>
                        </tr>
                      </thead>
                      <tbody>
                        {reuse.map((row) => (
                          <tr
                            key={row.requirementKey}
                            className={
                              versionId !== "" && row.acceptedVersionId === versionId
                                ? "cov-member-current"
                                : undefined
                            }
                          >
                            <td className="cov-member-key">{row.requirementKey}</td>
                            <td className="cov-muted">{row.projectKey || "—"}</td>
                            <td>
                              <select
                                className="cov-select"
                                value={row.acceptedVersionId}
                                onChange={(e) => {
                                  void SetMemberVersion(
                                    profileId,
                                    selected,
                                    row.requirementKey,
                                    e.target.value,
                                  ).then(() => reload());
                                }}
                              >
                                <option value="">— Unassigned —</option>
                                {versions.map((v) => (
                                  <option key={v.id} value={v.id}>
                                    {v.name}
                                  </option>
                                ))}
                              </select>
                              {versionId !== "" && row.acceptedVersionId === versionId && (
                                <span className="cov-member-badge">current</span>
                              )}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  )}
                </div>
              </div>
            ) : loading ? (
              <p className="cov-empty">Loading…</p>
            ) : tab === "matrix" ? (
              <div className="cov-matrix">
                {!versionId && (
                  <p className="cov-empty">Select or create a version above to view the coverage matrix.</p>
                )}
                {versionId && report &&
                  report.groups.map((g) => {
                    const grp = model?.groups.find((x) => x.id === g.groupId);
                    return (
                      <div key={g.groupId} className="cov-group">
                        <div className="cov-group-head">
                          <span className="cov-group-name">{g.name}</span>
                          <span className="cov-group-pct">
                            {g.tested}/{g.total} ({g.percent}%)
                          </span>
                          <button className="cov-x" title="Delete group" onClick={() => void deleteNode("group", g.groupId)}>
                            ×
                          </button>
                        </div>
                        <table className="cov-table">
                          <tbody>
                            {grp?.parameters.flatMap((p) => {
                              const paramTested = p.values.filter((v) => report.values[v.id]?.tested === true).length;
                              return [
                                <tr key={`ph-${p.id}`} className="cov-param-head">
                                  <td colSpan={3} className="cov-param-name">{p.name}</td>
                                  <td className="cov-param-pct">{paramTested}/{p.values.length} tested</td>
                                </tr>,
                                ...p.values.map((v) => {
                                  const vc = report.values[v.id];
                                  return (
                                    <tr key={v.id} className="cov-vrow">
                                      <td className="cov-vlabel">
                                        <span className="cov-vrow-bullet" aria-hidden="true">•</span>{" "}
                                        {v.valueLabel}
                                        {v.valueKind !== "value" && <span className="cov-kind">{v.valueKind}</span>}
                                        {!v.isRequired && <span className="cov-kind cov-optional">optional</span>}
                                      </td>
                                      <td>
                                        <span className={statusClass(vc)}>{statusText(vc)}</span>
                                        {staleByValue[v.id] ? <span className="cov-stale-dot" title="stale mapping">⚠</span> : null}
                                      </td>
                                      <td className="cov-tests">{vc?.testKeys.join(", ")}</td>
                                      <td>
                                        <button className="btn" onClick={() => setMapping({ valueId: v.id, label: v.valueLabel })}>
                                          Map…
                                        </button>
                                      </td>
                                    </tr>
                                  );
                                }),
                              ];
                            })}
                          </tbody>
                        </table>
                      </div>
                    );
                  })}
                {versionId && (
                  <div className="cov-addgroup">
                    <input
                      className="cov-input"
                      placeholder="Add group…"
                      value={newGroupName}
                      onChange={(e) => setNewGroupName(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter") void addGroup();
                      }}
                    />
                    <button className="btn" disabled={!newGroupName.trim()} onClick={() => void addGroup()}>
                      Add group
                    </button>
                  </div>
                )}
              </div>
            ) : tab === "gaps" ? (
              <table className="cov-table cov-gaps">
                <thead>
                  <tr>
                    <th>Group</th>
                    <th>Parameter</th>
                    <th>Value</th>
                    <th>Kind</th>
                  </tr>
                </thead>
                <tbody>
                  {gaps.length === 0 && (
                    <tr>
                      <td colSpan={4} className="cov-empty">No gaps. Every required value has a test. 🎉</td>
                    </tr>
                  )}
                  {gaps.map((g) => (
                    <tr key={g.valueId}>
                      <td>{g.groupName}</td>
                      <td>{g.paramName}</td>
                      <td>{g.valueLabel}</td>
                      <td>{g.errorCode || g.valueKind}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : (
              <table className="cov-table cov-reuse">
                <thead>
                  <tr>
                    <th>Project</th>
                    <th>Requirement</th>
                    <th>Summary</th>
                    <th>Status</th>
                  </tr>
                </thead>
                <tbody>
                  {reuse.length === 0 && (
                    <tr>
                      <td colSpan={4} className="cov-empty">No member requirements. Use "Members" to link the customer requirements that reuse this.</td>
                    </tr>
                  )}
                  {reuse.map((r) => (
                    <tr key={r.requirementKey}>
                      <td>{r.projectKey || "—"}</td>
                      <td>{r.requirementKey}</td>
                      <td>{r.summary}</td>
                      <td>{r.status}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </>
        )}
      </section>

      {mapping && (
        <MapTestsModal
          profileId={profileId}
          canonicalId={selected}
          valueId={mapping.valueId}
          valueLabel={mapping.label}
          onClose={() => setMapping(null)}
          onSaved={async () => {
            setMapping(null);
            await reload();
          }}
        />
      )}
      {showMembers && selectedCanon && (
        <MembersModal
          profileId={profileId}
          canonicalId={selected}
          current={reuse.map((r) => r.requirementKey)}
          onClose={() => setShowMembers(false)}
          onSaved={async () => {
            setShowMembers(false);
            await reload();
          }}
        />
      )}
      {confirmUI}
    </div>
  );
}

// MapTestsModal lets the user pick which Tests exercise a parameter value, from
// the pool of tests linked to the canonical's member requirements.
function MapTestsModal({
  profileId,
  canonicalId,
  valueId,
  valueLabel,
  onClose,
  onSaved,
}: {
  profileId: string;
  canonicalId: string;
  valueId: string;
  valueLabel: string;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [cands, setCands] = useState<CandidateTest[]>([]);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [manual, setManual] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [showPicker, setShowPicker] = useState(false);

  useEffect(() => {
    void (async () => {
      try {
        const [c, current] = await Promise.all([
          ListCoverageCandidateTests(profileId, canonicalId),
          ListValueTests(profileId, valueId),
        ]);
        setCands(c);
        setPicked(new Set(current));
      } catch (e) {
        setError(errMsg(e));
      }
    })();
  }, [profileId, canonicalId, valueId]);

  function toggle(key: string) {
    setPicked((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }

  async function save() {
    setBusy(true);
    try {
      const extra = manual
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);
      const keys = Array.from(new Set([...picked, ...extra]));
      await SetValueTests(profileId, valueId, keys);
      onSaved();
    } catch (e) {
      setError(errMsg(e));
      setBusy(false);
    }
  }

  // Merge keys chosen in the BrowseTestsPicker into the manual field, skipping
  // any already present in the candidate checkboxes (picked) or manual text.
  function handlePickerAdd(keys: string[]) {
    const existingManual = new Set(
      manual
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean),
    );
    const toAdd = keys.filter((k) => !picked.has(k) && !existingManual.has(k));
    if (toAdd.length === 0) return;
    const base = manual.trim();
    setManual(base ? `${base}, ${toAdd.join(", ")}` : toAdd.join(", "));
  }

  const handleClosePicker = useCallback(() => setShowPicker(false), []);

  // Compute the keys already selected so the picker can show them as disabled.
  const pickerExcludeKeys = [
    ...picked,
    ...manual.split(",").map((s) => s.trim()).filter(Boolean),
  ];

  return (
    <>
      <Modal onClose={onClose} className="modal map-tests-modal" labelledBy="cov-map-tests-title">
          <div className="pending-head">
            <h2 id="cov-map-tests-title">Map tests to "{valueLabel}"</h2>
            <button className="btn btn-ghost" onClick={onClose} title="Close">✕</button>
          </div>
          <div className="cov-body">
            {error && <div className="cov-error">{error}</div>}
            <div className="cov-cands">
              {cands.length === 0 && <p className="cov-empty">No candidate tests (link member requirements that have covering tests).</p>}
              {cands.map((c) => (
                <label key={c.testKey} className="cov-cand">
                  <input type="checkbox" checked={picked.has(c.testKey)} onChange={() => toggle(c.testKey)} />
                  <span className="cov-cand-key">{c.testKey}</span>
                  <span className="cov-cand-sum">{c.summary}</span>
                </label>
              ))}
            </div>
            <div className="cov-manual">
              Other test keys (comma-separated):
              <div className="cov-manual-row">
                <input
                  className="cov-input"
                  value={manual}
                  onChange={(e) => setManual(e.target.value)}
                  placeholder="TEST-1234, TEST-1235"
                />
                <button className="btn" type="button" onClick={() => setShowPicker(true)}>
                  Browse tests…
                </button>
              </div>
            </div>
          </div>
          <div className="pending-actions">
            <button className="btn" onClick={onClose}>Cancel</button>
            <button className="btn btn-primary" disabled={busy} onClick={() => void save()}>Save</button>
          </div>
      </Modal>
      {showPicker && (
        <BrowseTestsPicker
          profileId={profileId}
          excludeKeys={pickerExcludeKeys}
          onClose={handleClosePicker}
          onAdd={handlePickerAdd}
        />
      )}
    </>
  );
}

// MembersModal links the customer/project requirements that reuse this canonical
// functional requirement, picked from the profile's synced requirements.
function MembersModal({
  profileId,
  canonicalId,
  current,
  onClose,
  onSaved,
}: {
  profileId: string;
  canonicalId: string;
  current: string[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const [reqs, setReqs] = useState<{ key: string; summary: string; project: string }[]>([]);
  const [picked, setPicked] = useState<Set<string>>(new Set(current));
  const [filter, setFilter] = useState("");
  const [manual, setManual] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    void (async () => {
      try {
        const list = await ListRequirementsWithCoverage(profileId);
        setReqs(list.map((r) => ({ key: r.key, summary: r.summary, project: r.projectKey ?? "" })));
      } catch (e) {
        setError(errMsg(e));
      }
    })();
  }, [profileId]);

  const shown = reqs.filter(
    (r) => !filter || r.key.toLowerCase().includes(filter.toLowerCase()) || r.summary.toLowerCase().includes(filter.toLowerCase()),
  );

  async function save() {
    setBusy(true);
    try {
      const extra = manual.split(",").map((s) => s.trim()).filter(Boolean);
      const keys = Array.from(new Set([...picked, ...extra]));
      await SetCanonicalMembers(profileId, canonicalId, keys);
      onSaved();
    } catch (e) {
      setError(errMsg(e));
      setBusy(false);
    }
  }

  function toggle(key: string) {
    setPicked((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }

  return (
    <Modal onClose={onClose} className="modal members-modal" labelledBy="cov-members-title">
        <div className="pending-head">
          <h2 id="cov-members-title">Member requirements</h2>
          <button className="btn btn-ghost" onClick={onClose} title="Close">✕</button>
        </div>
        <div className="cov-body">
          {error && <div className="cov-error">{error}</div>}
          <input className="cov-input" placeholder="Filter…" value={filter} onChange={(e) => setFilter(e.target.value)} />
          <div className="cov-cands">
            {shown.map((r) => (
              <label key={r.key} className="cov-cand">
                <input type="checkbox" checked={picked.has(r.key)} onChange={() => toggle(r.key)} />
                <span className="cov-cand-key">{r.key}</span>
                <span className="cov-cand-sum">{r.summary}</span>
              </label>
            ))}
          </div>
          <label className="cov-manual">
            Other requirement keys (comma-separated):
            <input className="cov-input" value={manual} onChange={(e) => setManual(e.target.value)} placeholder="CUST-HSM-BANK-010" />
          </label>
        </div>
        <div className="pending-actions">
          <button className="btn" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary" disabled={busy} onClick={() => void save()}>Save</button>
        </div>
    </Modal>
  );
}
