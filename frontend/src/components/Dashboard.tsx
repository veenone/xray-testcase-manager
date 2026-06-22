import { useEffect, useState } from "react";
import {
  GetStatistics,
  ListFolders,
  ListComponents,
  ListStatuses,
  ExportDashboard,
  errMsg,
} from "../api";
import type { Statistics, Bucket } from "../api";
import { testrepo } from "../../wailsjs/go/models";
import { DuplicatesCard } from "./DuplicatesCard";

interface Props {
  profileId: string;
  refreshKey: number;
  onOpenDuplicates?: () => void;
}

// Dashboard renders the per-profile statistics view (FR-9), computed entirely
// from the local store. It recomputes whenever the profile changes or a sync /
// commit bumps refreshKey, so the numbers track the cache without a Jira call.
// Optional Folder / Component / Status filters narrow every panel to the
// matching subset of Tests (RND_P_4TFINT_05-228).
export function Dashboard({
  profileId,
  refreshKey,
  onOpenDuplicates,
}: Props) {
  const [stats, setStats] = useState<Statistics | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  // Local refresh: recompute the dashboard from the cache without a full sync (#7).
  const [nonce, setNonce] = useState(0);
  // XLSX export state (RND_P_4TFINT_05): mirror TraceabilityTabs' notice pattern.
  const [exporting, setExporting] = useState(false);
  const [exportNotice, setExportNotice] = useState("");
  const [exportErr, setExportErr] = useState("");

  // Filter selections + their option lists, loaded from existing bindings.
  const [folder, setFolder] = useState("");
  const [component, setComponent] = useState("");
  const [status, setStatus] = useState("");
  const [folderOptions, setFolderOptions] = useState<testrepo.Folder[]>([]);
  const [componentOptions, setComponentOptions] = useState<Bucket[]>([]);
  const [statusOptions, setStatusOptions] = useState<string[]>([]);
  const hasFilter = folder !== "" || component !== "" || status !== "";

  // Reset selections when the profile changes, then load the option lists.
  useEffect(() => {
    setFolder("");
    setComponent("");
    setStatus("");
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

  useEffect(() => {
    if (!profileId) return;
    let cancelled = false;
    setLoading(true);
    setError("");
    GetStatistics(profileId, folder, component, status)
      .then((s) => {
        if (!cancelled) setStats(s);
      })
      .catch((e) => {
        if (!cancelled) setError(errMsg(e));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [profileId, refreshKey, nonce, folder, component, status]);

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
    return <div className="dashboard muted">Loading…</div>;
  }
  if (error) {
    return <div className="dashboard error-text">{error}</div>;
  }
  if (!stats) {
    return null;
  }

  const filterBar = (
    <div className="dashboard-filters">
      <select
        className="dashboard-filter"
        value={folder}
        onChange={(e) => setFolder(e.target.value)}
        title="Limit the dashboard to a Test Repository folder (and its subfolders)"
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
      <div className="dashboard">
        {filterBar}
        <p className="muted">
          {hasFilter
            ? "No tests match the selected filters."
            : "No tests cached yet. Run a sync to populate the dashboard."}
        </p>
      </div>
    );
  }

  return (
    <div className="dashboard">
      <div className="dashboard-head">
        {filterBar}
        <div className="dashboard-head-actions">
          <button
            className="btn"
            onClick={exportDashboard}
            disabled={exporting}
            title="Export the dashboard (Summary + breakdowns) to XLSX, honouring the current filters"
          >
            {exporting ? "Exporting…" : "Export XLSX"}
          </button>
          <button
            className="btn btn-primary"
            onClick={() => setNonce((n) => n + 1)}
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
        refreshKey={refreshKey}
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
        Computed from the local cache (FR-9.5). Execution coverage and Test
        Set / Plan stats arrive once executions are synced.
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
