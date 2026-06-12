import { useEffect, useState } from "react";
import {
  GetStatistics,
  GetTraceabilitySankey,
  GetRequirementTraceability,
  ListRequirementsWithCoverage,
  ListContainers,
  errMsg,
} from "../api";
import type {
  Statistics,
  Bucket,
  Sankey,
  Container,
  RequirementCoverage,
} from "../api";
import { SankeyChart } from "./SankeyChart";
import { RequirementSankey } from "./RequirementSankey";
import { DuplicatesCard } from "./DuplicatesCard";
import { MultiSelect } from "./MultiSelect";

interface Props {
  profileId: string;
  refreshKey: number;
  onOpenDuplicates?: () => void;
}

// Dashboard renders the per-profile statistics view (FR-9), computed entirely
// from the local store. It recomputes whenever the profile changes or a sync /
// commit bumps refreshKey, so the numbers track the cache without a Jira call.
export function Dashboard({ profileId, refreshKey, onOpenDuplicates }: Props) {
  const [stats, setStats] = useState<Statistics | null>(null);
  const [sankey, setSankey] = useState<Sankey | null>(null);
  const [reqSankey, setReqSankey] = useState<Sankey | null>(null);
  const [reqSankeyErr, setReqSankeyErr] = useState("");
  const [reqSel, setReqSel] = useState<string[]>([]);
  const [reqOptions, setReqOptions] = useState<RequirementCoverage[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  // Local refresh: recompute the dashboard from the cache without a full sync (#7).
  const [nonce, setNonce] = useState(0);

  // Traceability filters (FR-9): narrow the flow to chosen Test Plans /
  // Executions (multi-select), and optionally to cross-project executions only.
  const [plans, setPlans] = useState<Container[]>([]);
  const [execs, setExecs] = useState<Container[]>([]);
  const [planSel, setPlanSel] = useState<string[]>([]);
  const [execSel, setExecSel] = useState<string[]>([]);
  const [crossProject, setCrossProject] = useState(false);
  const [sankeyErr, setSankeyErr] = useState("");

  useEffect(() => {
    if (!profileId) return;
    let cancelled = false;
    setLoading(true);
    setError("");
    GetStatistics(profileId)
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
  }, [profileId, refreshKey, nonce]);

  // Filter options: the project's Test Plans and Test Executions.
  useEffect(() => {
    if (!profileId) return;
    let cancelled = false;
    setPlanSel([]);
    setExecSel([]);
    Promise.all([
      ListContainers(profileId, "testplan"),
      ListContainers(profileId, "testexec"),
    ])
      .then(([tp, te]) => {
        if (cancelled) return;
        setPlans(tp ?? []);
        setExecs(te ?? []);
      })
      .catch((e) => console.error("list containers:", errMsg(e)));
    return () => {
      cancelled = true;
    };
  }, [profileId, refreshKey, nonce]);

  // The Sankey re-fetches whenever the filters change (or the data refreshes).
  useEffect(() => {
    if (!profileId) return;
    let cancelled = false;
    setSankeyErr("");
    GetTraceabilitySankey(profileId, planSel, execSel, crossProject)
      .then((sk) => {
        if (!cancelled) setSankey(sk);
      })
      .catch((e) => {
        if (cancelled) return;
        // Surface it — a silent catch made a failed call look like "no data".
        setSankeyErr(errMsg(e));
        console.error("traceability:", errMsg(e));
      });
    return () => {
      cancelled = true;
    };
  }, [profileId, refreshKey, planSel, execSel, crossProject, nonce]);

  // Requirement traceability is independent of the plan/exec filters, but can be
  // narrowed to a single requirement.
  useEffect(() => {
    if (!profileId) return;
    let cancelled = false;
    setReqSankeyErr("");
    GetRequirementTraceability(profileId, reqSel)
      .then((sk) => {
        if (!cancelled) setReqSankey(sk);
      })
      .catch((e) => {
        if (cancelled) return;
        setReqSankeyErr(errMsg(e));
        console.error("requirement traceability:", errMsg(e));
      });
    return () => {
      cancelled = true;
    };
  }, [profileId, refreshKey, reqSel, nonce]);

  // The requirement list drives the Sankey filter dropdown.
  useEffect(() => {
    if (!profileId) return;
    let cancelled = false;
    ListRequirementsWithCoverage(profileId)
      .then((rs) => {
        if (!cancelled) setReqOptions(rs ?? []);
      })
      .catch(() => {
        if (!cancelled) setReqOptions([]);
      });
    return () => {
      cancelled = true;
    };
  }, [profileId, refreshKey, nonce]);

  if (loading && !stats) {
    return <div className="dashboard muted">Loading…</div>;
  }
  if (error) {
    return <div className="dashboard error-text">{error}</div>;
  }
  if (!stats) {
    return null;
  }

  if (stats.total === 0) {
    return (
      <div className="dashboard">
        <p className="muted">
          No tests cached yet. Run a sync to populate the dashboard.
        </p>
      </div>
    );
  }

  return (
    <div className="dashboard">
      <div className="dashboard-head">
        <button
          className="btn"
          onClick={() => setNonce((n) => n + 1)}
          title="Recompute the dashboard from the local cache"
        >
          ↻ Refresh
        </button>
      </div>
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

      <div className="stat-panel sankey-panel">
        <div className="sankey-head">
          <h4>
            Requirement traceability
            <span className="stat-panel-sub">
              how each requirement flows through coverage and Test plans to run
              results
            </span>
          </h4>
          {reqOptions.length > 0 && (
            <label className="sankey-filter">
              <span className="muted">Requirements</span>
              <MultiSelect
                allLabel="All requirements"
                title="Filter by one or more requirements"
                selected={reqSel}
                onChange={setReqSel}
                options={reqOptions.map((r) => ({
                  value: r.key,
                  label: r.summary ? `${r.key} — ${r.summary}` : r.key,
                }))}
              />
            </label>
          )}
        </div>
        {reqSankeyErr ? (
          <p className="error-text sankey-empty">
            Couldn&apos;t build the requirement traceability flow:{" "}
            {reqSankeyErr}
          </p>
        ) : stats.byCoverage.length === 0 ? (
          <p className="muted sankey-empty">
            No requirement coverage yet. Add a requirement source (Requirements
            tab → Sources), link requirements to tests, then sync — the flow
            from requirement → coverage → Test plan → test result appears here.
          </p>
        ) : (
          <RequirementSankey data={reqSankey ?? { nodes: [], links: [] }} />
        )}
      </div>

      {stats.testExecutions > 0 && (
        <div className="stat-panel sankey-panel">
          <div className="sankey-head">
            <h4>
              Traceability
              <span className="stat-panel-sub">
                how test runs flow from plans through executions to outcomes
              </span>
            </h4>
            <div className="sankey-filters">
              <MultiSelect
                allLabel="All plans"
                title="Filter by one or more Test Plans"
                selected={planSel}
                onChange={setPlanSel}
                options={plans.map((p) => ({
                  value: p.key,
                  label: p.summary ? `${p.key} — ${p.summary}` : p.key,
                }))}
              />
              <MultiSelect
                allLabel={`All executions (${execs.length})`}
                title="Filter by one or more Test Executions"
                selected={execSel}
                onChange={setExecSel}
                options={execs.map((x) => ({
                  value: x.key,
                  label: x.summary ? `${x.key} — ${x.summary}` : x.key,
                }))}
              />
              <label
                className="sankey-crossproject"
                title="Show only Test Plans in this project whose runs are in a different project"
              >
                <input
                  type="checkbox"
                  checked={crossProject}
                  onChange={(e) => setCrossProject(e.target.checked)}
                />
                Cross-project only
              </label>
              {(planSel.length > 0 || execSel.length > 0 || crossProject) && (
                <button
                  className="btn btn-ghost sankey-clear"
                  onClick={() => {
                    setPlanSel([]);
                    setExecSel([]);
                    setCrossProject(false);
                  }}
                  title="Clear filters"
                >
                  ✕ Clear
                </button>
              )}
            </div>
          </div>
          {sankeyErr ? (
            <p className="error-text sankey-empty">
              Couldn&apos;t build the traceability flow: {sankeyErr}
            </p>
          ) : (
            <SankeyChart
              data={sankey ?? { nodes: [], links: [] }}
              filtered={planSel.length > 0 || execSel.length > 0 || crossProject}
              onClearFilter={() => {
                setPlanSel([]);
                setExecSel([]);
                setCrossProject(false);
              }}
            />
          )}
        </div>
      )}

      <TrendPanel buckets={stats.updatedTrend} />

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
