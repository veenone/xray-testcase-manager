import { useEffect, useState } from "react";
import { useProfile } from "../contexts/ProfileContext";
import {
  SetCoverageProjects,
  SeedPKCS11Reference,
  SeedEUICCReference,
  errMsg,
} from "../api";
import type { ProjectConfig, Sankey } from "../api";
import { useCoverageMapData } from "../queries/coverage";
import { SankeyChart } from "./SankeyChart";

interface Props {
  isDemo?: boolean;
  demoVariant?: "pkcs" | "euicc" | "";
}

const EMPTY_SANKEY: Sankey = { nodes: [], links: [] };

// CoverageMap renders three regions: a per-project coverage panel, a
// project→function→coverage relation Sankey, and an editable project-config
// strip. All three are loaded together and the Save button reloads all three.
// The map is built from canonical functions and their member requirements, so
// it stays empty until a coverage model exists — a demo Sync alone does not
// populate it (see the empty state, which offers the demo seed action).
export function CoverageMap({ isDemo, demoVariant }: Props) {
  const { activeId: profileId } = useProfile();
  // The map's three reads come from the query cache with a stable key (Phase
  // 4c); a mutation refreshes it via invalidateProfileData, and save/seed
  // refetch it directly.
  const mapQuery = useCoverageMapData(profileId);
  const rows = mapQuery.data?.rows ?? [];
  const sankey = mapQuery.data?.sankey ?? EMPTY_SANKEY;
  const loadError = mapQuery.error ? errMsg(mapQuery.error) : "";
  const [draftProjects, setDraftProjects] = useState<ProjectConfig[]>([]);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [seeding, setSeeding] = useState(false);

  // Re-seed the editable project-config draft whenever the map data loads or
  // refetches — matching the old loadAll, which re-seeded (and reset the "Saved"
  // indicator) on every run.
  useEffect(() => {
    if (!mapQuery.data) return;
    setDraftProjects(mapQuery.data.projects ?? []);
    setSaved(false);
  }, [mapQuery.data]);

  function updateDraft(idx: number, field: keyof ProjectConfig, value: string | number) {
    setDraftProjects((prev) =>
      prev.map((p, i) => (i === idx ? { ...p, [field]: value } : p)),
    );
  }

  function addRow() {
    setDraftProjects((prev) => [
      ...prev,
      { projectKey: "", role: "source", label: "", sortOrder: prev.length },
    ]);
  }

  function removeRow(idx: number) {
    setDraftProjects((prev) => prev.filter((_, i) => i !== idx));
  }

  async function save() {
    setSaving(true);
    setError("");
    setSaved(false);
    try {
      await SetCoverageProjects(profileId, draftProjects);
      setSaved(true);
      await mapQuery.refetch();
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setSaving(false);
    }
  }

  // Demo convenience: seed the reference coverage layer (PKCS#11 or eUICC,
  // depending on the profile variant) onto the synced demo tests/requirements,
  // then reload the map. Mirrors the welcome-screen action, surfaced here so it's
  // reachable when the map is empty.
  async function loadReferenceCoverage() {
    setSeeding(true);
    setError("");
    try {
      if (demoVariant === "euicc") {
        await SeedEUICCReference(profileId);
      } else {
        await SeedPKCS11Reference(profileId);
      }
      await mapQuery.refetch();
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setSeeding(false);
    }
  }

  return (
    <div className="cov-map">
      {(loadError || error) && (
        <div className="cov-error">{loadError || error}</div>
      )}

      {/* Per-project coverage panel */}
      <section className="cov-map-section">
        <h3 className="cov-section-title">Coverage by project</h3>
        {rows.length === 0 ? (
          <div className="cov-map-empty">
            <p className="cov-muted">
              No coverage data yet. This view is built from your canonical functions
              and the customer requirements that reuse them; a <strong>Sync</strong> alone
              doesn&apos;t populate it. Define a coverage model on the <strong>Coverage</strong> tab
              {isDemo && demoVariant === "euicc"
                ? ", or load the eUICC demo coverage onto the synced demo-euicc tests:"
                : isDemo && demoVariant === "pkcs"
                ? ", or load the PKCS#11 demo coverage onto the synced demo-pkcs tests:"
                : "."}
            </p>
            {isDemo && demoVariant && (
              <button
                className="btn btn-primary"
                disabled={seeding}
                onClick={() => void loadReferenceCoverage()}
              >
                {seeding ? "Loading…" : demoVariant === "euicc" ? "Load eUICC coverage" : "Load PKCS#11 coverage"}
              </button>
            )}
          </div>
        ) : (
          <div className="cov-map-rows">
            {rows.map((row) => (
              <div key={row.projectKey} className="cov-map-row">
                <span className="cov-map-label">
                  {row.label || row.projectKey}
                </span>
                <div className="cov-bar-wrap">
                  <div
                    className="cov-bar"
                    style={{ width: `${Math.min(100, row.percent)}%` }}
                  />
                </div>
                <span className="cov-map-pct">{row.percent.toFixed(1)}%</span>
                <span className="cov-muted cov-map-meta">
                  {row.requirementCount} reqs &middot; {row.functionsReused} functions
                </span>
              </div>
            ))}
          </div>
        )}
      </section>

      {/* Relation Sankey */}
      <section className="cov-map-section">
        <h3 className="cov-section-title">Coverage relation</h3>
        <SankeyChart
          data={sankey}
          columns={["Project", "Function", "Coverage"]}
          emptyHint="No coverage relation yet. Define a coverage model (or load the PKCS#11 demo coverage above) to see the flow."
        />
      </section>

      {/* Project-config strip */}
      <section className="cov-map-section">
        <h3 className="cov-section-title">In-scope project configuration</h3>
        <div className="cov-map-config">
          {draftProjects.length === 0 && (
            <p className="cov-muted">
              No projects configured. Add a row to get started.
            </p>
          )}
          {draftProjects.map((p, i) => (
            <div key={i} className="cov-map-config-row">
              <input
                className="cov-input"
                placeholder="Project key"
                value={p.projectKey}
                onChange={(e) => updateDraft(i, "projectKey", e.target.value)}
              />
              <select
                className="cov-select"
                value={p.role}
                onChange={(e) => updateDraft(i, "role", e.target.value)}
              >
                <option value="source">source</option>
                <option value="customer">customer</option>
              </select>
              <input
                className="cov-input"
                placeholder="Display label"
                value={p.label}
                onChange={(e) => updateDraft(i, "label", e.target.value)}
              />
              <button
                className="cov-x"
                title="Remove row"
                onClick={() => removeRow(i)}
              >
                ×
              </button>
            </div>
          ))}
          <div className="cov-map-config-actions">
            <button className="btn" onClick={addRow}>
              + Add project
            </button>
            <button
              className="btn btn-primary"
              disabled={saving}
              onClick={() => void save()}
            >
              {saving ? "Saving…" : "Save"}
            </button>
            {saved && <span className="cov-notice">Saved.</span>}
          </div>
        </div>
      </section>
    </div>
  );
}
