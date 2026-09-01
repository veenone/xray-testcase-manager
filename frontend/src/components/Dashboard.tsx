import { useEffect, useState } from "react";
import { useProfile } from "../contexts/ProfileContext";
import { useViewState } from "../lib/viewState";
import {
  ListFolders,
  ListComponents,
  ListStatuses,
  ExportDashboard,
  errMsg,
} from "../api";
import type { Bucket } from "../api";
import { testrepo } from "../../wailsjs/go/models";
import { DuplicatesCard } from "./DuplicatesCard";
import { useStatistics } from "../queries/statistics";

interface Props {
  onOpenDuplicates?: () => void;
}

// Dashboard renders the per-profile statistics view (FR-9), computed entirely
// from the local store. It recomputes whenever the profile changes or a sync /
// commit invalidates the stats query, so the numbers track the cache without a
// Jira call. Optional Folder / Component / Status filters narrow every panel to
// the matching subset of Tests (RND_P_4TFINT_05-228).
export function Dashboard({ onOpenDuplicates }: Props) {
  const { activeId: profileId } = useProfile();
  // XLSX export state (RND_P_4TFINT_05): mirror TraceabilityTabs' notice pattern.
  const [exporting, setExporting] = useState(false);
  const [exportNotice, setExportNotice] = useState("");
  const [exportErr, setExportErr] = useState("");

  // Filter selections + their option lists, loaded from existing bindings.
  const [folder, setFolder] = useViewState(profileId, "dashboard", "folder", "");
  const [component, setComponent] = useViewState(profileId, "dashboard", "component", "");
  const [status, setStatus] = useViewState(profileId, "dashboard", "status", "");
  const [folderOptions, setFolderOptions] = useState<testrepo.Folder[]>([]);
  const [componentOptions, setComponentOptions] = useState<Bucket[]>([]);
  const [statusOptions, setStatusOptions] = useState<string[]>([]);
  const hasFilter = folder !== "" || component !== "" || status !== "";

  // Load option lists when the profile changes. Selections are preserved via
  // useViewState (keyed by profileId) so no explicit reset is needed here.
  useEffect(() => {
    if (!profileId) return;
    let cancelled = false;
    ListFolders(profileId)
      .then((f) => {
        if (!cancelled) setFolderOptions(f ?? []);
      })
      .catch((e) => console.error("list folders:", errMsg(e)));
    ListComponents(profileId)
      .then((c) => {
        if (!cancelled) setComponentOptions(c ?? []);
      })
      .catch((e) => console.error("list components:", errMsg(e)));
    ListStatuses(profileId)
      .then((s) => {
        if (!cancelled) setStatusOptions(s ?? []);
      })
      .catch((e) => console.error("list statuses:", errMsg(e)));
    return () => {
      cancelled = true;
    };
  }, [profileId]);

  const statsQuery = useStatistics(profileId, folder, component, status);
  const stats = statsQuery.data ?? null;
  const loading = statsQuery.isFetching;
  const error = statsQuery.error ? errMsg(statsQuery.error) : "";

  // Export the dashboard to XLSX honouring the current folder/component/status
  // filters, so the workbook matches the on-screen scope.
  async function exportDashboard() {
    setExporting(true);
    setExportErr("");
    setExportNotice("");
    try {
      const path = await ExportDashboard(profileId, folder, component, status);
      if (path) setExportNotice(`Saved to ${path}`);
    } catch (e) {
      setExportErr(errMsg(e));
    } finally {
      setExporting(false);
    }
  }

  if (loading && !stats) {
    return <div className="dashboard muted" data-tour="dashboard-body">Loading…</div>;
  }
  if (error) {
    return <div className="dashboard error-text" data-tour="dashboard-body">{error}</div>;
  }
  if (!stats) {
    return null;
  }

  const filterBar = (
    <div className="dashboard-filters" data-tour="dashboard-tools">
      <select
        className="dashboard-filter"
        value={folder}
        onChange={(e) => setFolder(e.target.value)}
        title="Limit the dashboard to one Test Repository folder and its subfolders"
      >
        <option value="">All folders</option>
        {folderOptions.map((f) => (
          <option key={f.id} value={f.id}>
            {f.id}
          </option>
        ))}
      </select>
      <select
        className="dashboard-filter"
        value={component}
        onChange={(e) => setComponent(e.target.value)}
        title="Limit the dashboard to a component"
      >
        <option value="">All components</option>
        {componentOptions.map((c) => (
          <option key={c.label} value={c.label}>
            {c.label}
          </option>
        ))}
      </select>
      <select
        className="dashboard-filter"
        value={status}
        onChange={(e) => setStatus(e.target.value)}
        title="Limit the dashboard to a status"
      >
        <option value="">All statuses</option>
        {statusOptions.map((s) => (
          <option key={s} value={s}>
            {s}
          </option>
        ))}
      </select>
      {hasFilter && (
        <button
          className="btn btn-ghost"
          onClick={() => {
            setFolder("");
            setComponent("");
            setStatus("");
          }}
          title="Clear all dashboard filters"
        >
          Clear
        </button>
      )}
    </div>
  );

  if (stats.total === 0) {
    return (
      <div className="dashboard" data-tour="dashboard-body">
        {filterBar}
        <p className="muted">
          {hasFilter
            ? "No tests match these filters."
            : "No tests cached yet. Run a sync to fill in the dashboard."}
        </p>
      </div>
    );
  }

  return (
    <div className="dashboard" data-tour="dashboard-body">
      <div className="dashboard-head">
        {filterBar}
        <div className="dashboard-head-actions">
          <button
            className="btn"
            onClick={exportDashboard}
            disabled={exporting}
            title="Export the dashboard (summary and breakdowns) to XLSX, using the current filters"
          >
            {exporting ? "Exporting…" : "Export XLSX"}
          </button>
          <button
            className="btn btn-primary"
            onClick={() => void statsQuery.refetch()}
            title="Recompute the dashboard from the local cache"
          >
            ↻ Refresh
          </button>
        </div>
      </div>
      {exportErr && <p className="error-text">{exportErr}</p>}
      {exportNotice && <p className="muted">{exportNotice}</p>}
      <DuplicatesCard
        profileId={profileId}
        onOpen={() => onOpenDuplicates?.()}
      />
      <div className="stat-tiles">
        <Tile label="Total tests" value={stats.total.toLocaleString()} />
        <Tile
          label="Pending changes"
          value={stats.pendingChanges.toLocaleString()}
          accent={stats.pendingChanges > 0}
        />
        <Tile label="Statuses" value={String(stats.byStatus.length)} />
        <Tile label="Distinct labels" value={String(stats.byLabel.length)} />
      </div>

      <div className="stat-grid">
        <BarPanel title="By status" buckets={stats.byStatus} />
        <BarPanel title="By priority" buckets={stats.byPriority} />
        <BarPanel
          title="By folder (top-level)"
          buckets={stats.byFolder}
          empty="No folders synced."
        />
        <BarPanel
          title="Top labels"
          buckets={stats.byLabel}
          empty="No labels in use."
        />
        <BarPanel
          title="By component"
          buckets={stats.byComponent}
          empty="No components synced."
        />
        <BarPanel
          title="By requirement"
          buckets={stats.byRequirement}
          empty="No requirements synced."
        />
      </div>

      <div className="stat-grid">
        {(stats.testSets > 0 ||
        stats.testPlans > 0 ||
        stats.testExecutions > 0) && (
        <div className="stat-panel">
          <h4>Test Sets &amp; Plans</h4>
          <ul className="container-stat-list">
            <li>
              <span>Test Sets</span>
              <span>
                {stats.testSets} ·{" "}
                {stats.testsInSet.toLocaleString()} of{" "}
                {stats.total.toLocaleString()} tests covered
              </span>
            </li>
            <li>
              <span>Test Plans</span>
              <span>
                {stats.testPlans} ·{" "}
                {stats.testsInPlan.toLocaleString()} tests covered
              </span>
            </li>
            <li>
              <span>Test Executions</span>
              <span>
                {stats.testExecutions} ·{" "}
                {stats.executedTests.toLocaleString()} tests executed
              </span>
            </li>
          </ul>
        </div>
      )}

      {stats.byRunStatus.length > 0 && (
        <BarPanel
          title="Execution coverage"
          subtitle={`${stats.executedTests.toLocaleString()} of ${stats.total.toLocaleString()} tests in an execution`}
          buckets={stats.byRunStatus}
          runColors
        />
      )}

      {stats.byCoverage.length > 0 && (
        <BarPanel
          title="Requirement coverage"
          subtitle={`${stats.byCoverage
            .reduce((n, b) => n + b.count, 0)
            .toLocaleString()} requirements`}
          buckets={stats.byCoverage}
          covColors
        />
      )}

        <div className="dashboard-trend-span">
          <TrendPanel buckets={stats.updatedTrend} />
        </div>
      </div>

      <p className="muted dashboard-note">
        Computed from the local cache. Execution coverage and Test Set / Plan
        stats appear once executions are synced.
      </p>
    </div>
  );
}

function Tile({
  label,
  value,
  accent,
}: {
  label: string;
  value: string;
  accent?: boolean;
}) {
  return (
    <div className={`stat-tile${accent ? " stat-tile-accent" : ""}`}>
      <div className="stat-tile-value">{value}</div>
      <div className="stat-tile-label">{label}</div>
    </div>
  );
}

function BarPanel({
  title,
  subtitle,
  buckets,
  empty,
  runColors,
  covColors,
}: {
  title: string;
  subtitle?: string;
  buckets: Bucket[];
  empty?: string;
  runColors?: boolean;
  covColors?: boolean;
}) {
  const max = buckets.reduce((m, b) => Math.max(m, b.count), 0) || 1;
  const fillClass = (label: string) => {
    if (runColors) return `stat-bar-fill run-${label.toLowerCase()}`;
    if (covColors) return `stat-bar-fill cov-${label.toLowerCase()}`;
    return "stat-bar-fill";
  };
  return (
    <div className="stat-panel">
      <h4>
        {title}
        {subtitle && <span className="stat-panel-sub">{subtitle}</span>}
      </h4>
      {buckets.length === 0 ? (
        <p className="muted">{empty ?? "No data."}</p>
      ) : (
        <ul className="stat-bars">
          {buckets.map((b) => (
            <li key={b.label} className="stat-bar-row">
              <span className="stat-bar-label" title={b.label}>
                {b.label}
              </span>
              <span className="stat-bar-track">
                <span
                  className={fillClass(b.label)}
                  style={{ width: `${(b.count / max) * 100}%` }}
                />
              </span>
              <span className="stat-bar-count">{b.count.toLocaleString()}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function TrendPanel({ buckets }: { buckets: Bucket[] }) {
  const max = buckets.reduce((m, b) => Math.max(m, b.count), 0) || 1;
  return (
    <div className="stat-panel stat-trend-panel">
      <h4>Recently updated (by month)</h4>
      {buckets.length === 0 ? (
        <p className="muted">No update timestamps available.</p>
      ) : (
        <div className="stat-trend">
          {buckets.map((b) => (
            <div key={b.label} className="stat-trend-col" title={`${b.count}`}>
              <div className="stat-trend-bar-wrap">
                <div
                  className="stat-trend-bar"
                  style={{ height: `${(b.count / max) * 100}%` }}
                />
              </div>
              <div className="stat-trend-x">{b.label.slice(2)}</div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
