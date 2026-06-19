import { useEffect, useState } from "react";
import {
  GetStatistics,
  GetTraceabilitySankey,
  GetRequirementTraceability,
  GetSubTaskTraceability,
  ExportTraceability,
  ListRequirementsWithCoverage,
  ListContainers,
  GetExecutionsForPlans,
  GetProfileProjectKey,
  ListBugsWithTests,
  BrowserOpenURL,
  errMsg,
} from "../api";
import type {
  Statistics,
  Sankey,
  Container,
  RequirementCoverage,
  BugWithTests,
} from "../api";
import { SankeyChart } from "./SankeyChart";
import { RequirementSankey } from "./RequirementSankey";
import { MultiSelect } from "./MultiSelect";

interface Props {
  profileId: string;
  refreshKey: number;
  jiraUrl?: string;
}

type Tab = "req" | "exec" | "subtask";

// TraceabilityTabs is the dedicated Traceability view: three Sankeys
// (requirement coverage, plan -> execution -> status, and sub-task
// parent -> execution -> status) behind a tab bar, each with its own filters.
// Computed entirely from the local store; recomputes on refreshKey.
export function TraceabilityTabs({ profileId, refreshKey, jiraUrl }: Props) {
  const [tab, setTab] = useState<Tab>("exec");
  const [stats, setStats] = useState<Statistics | null>(null);
  const [statsErr, setStatsErr] = useState("");
  const [exporting, setExporting] = useState(false);
  const [exportNotice, setExportNotice] = useState("");
  const [exportErr, setExportErr] = useState("");

  // Requirement traceability.
  const [reqSankey, setReqSankey] = useState<Sankey | null>(null);
  const [reqSankeyErr, setReqSankeyErr] = useState("");
  const [reqSel, setReqSel] = useState<string[]>([]);
  const [reqOptions, setReqOptions] = useState<RequirementCoverage[]>([]);

  // Plan/Execution traceability + cross-project bugs.
  const [sankey, setSankey] = useState<Sankey | null>(null);
  const [sankeyErr, setSankeyErr] = useState("");
  const [plans, setPlans] = useState<Container[]>([]);
  const [execs, setExecs] = useState<Container[]>([]);
  const [planSel, setPlanSel] = useState<string[]>([]);
  const [execSel, setExecSel] = useState<string[]>([]);
  const [crossProject, setCrossProject] = useState(false);
  const [projectKey, setProjectKey] = useState("");
  const [crossBugs, setCrossBugs] = useState<BugWithTests[]>([]);

  // Sub-task (parent) traceability.
  const [subSankey, setSubSankey] = useState<Sankey | null>(null);
  const [subSankeyErr, setSubSankeyErr] = useState("");
  const [parents, setParents] = useState<string[]>([]);
  const [parentSel, setParentSel] = useState<string[]>([]);

  useEffect(() => {
    if (!profileId) return;
    let cancelled = false;
    setStatsErr("");
    GetStatistics(profileId, "", "", "")
      .then((s) => {
        if (!cancelled) setStats(s);
      })
      .catch((e) => {
        if (!cancelled) setStatsErr(errMsg(e));
      });
    return () => {
      cancelled = true;
    };
  }, [profileId, refreshKey]);

  useEffect(() => {
    if (!profileId) return;
    let cancelled = false;
    GetProfileProjectKey(profileId)
      .then((k) => {
        if (!cancelled) setProjectKey(k ?? "");
      })
      .catch(() => {
        if (!cancelled) setProjectKey("");
      });
    return () => {
      cancelled = true;
    };
  }, [profileId]);

  // Requirement filter options.
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
  }, [profileId, refreshKey]);

  // Test Plan options.
  useEffect(() => {
    if (!profileId) return;
    let cancelled = false;
    setPlanSel([]);
    ListContainers(profileId, "testplan")
      .then((tp) => {
        if (!cancelled) setPlans(tp ?? []);
      })
      .catch((e) => console.error("list plans:", errMsg(e)));
    return () => {
      cancelled = true;
    };
  }, [profileId, refreshKey]);

  // Parent options: distinct parent keys of the synced sub-task executions.
  useEffect(() => {
    if (!profileId) return;
    let cancelled = false;
    setParentSel([]);
    ListContainers(profileId, "testexec")
      .then((te) => {
        if (cancelled) return;
        const ps = Array.from(
          new Set((te ?? []).filter((c) => c.parentKey).map((c) => c.parentKey)),
        ).sort();
        setParents(ps);
      })
      .catch((e) => console.error("list executions:", errMsg(e)));
    return () => {
      cancelled = true;
    };
  }, [profileId, refreshKey]);

  // Execution options cascade from the selected plans; prune stale execSel.
  useEffect(() => {
    if (!profileId) return;
    let cancelled = false;
    GetExecutionsForPlans(profileId, planSel)
      .then((te) => {
        if (cancelled) return;
        const opts = te ?? [];
        setExecs(opts);
        setExecSel((cur) => cur.filter((k) => opts.some((c) => c.key === k)));
      })
      .catch((e) => console.error("executions for plans:", errMsg(e)));
    return () => {
      cancelled = true;
    };
  }, [profileId, refreshKey, planSel]);

  // Cross-project bugs (only when the toggle is on).
  useEffect(() => {
    if (!profileId || !crossProject) {
      setCrossBugs([]);
      return;
    }
    let cancelled = false;
    ListBugsWithTests(profileId)
      .then((bs) => {
        if (cancelled) return;
        const pk = projectKey.trim();
        setCrossBugs(
          (bs ?? []).filter((b) => pk && b.projectKey && b.projectKey !== pk),
        );
      })
      .catch((e) => console.error("cross-project bugs:", errMsg(e)));
    return () => {
      cancelled = true;
    };
  }, [profileId, refreshKey, crossProject, projectKey]);

  // Requirement Sankey.
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
  }, [profileId, refreshKey, reqSel]);

  // Plan/Execution Sankey.
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
        setSankeyErr(errMsg(e));
        console.error("traceability:", errMsg(e));
      });
    return () => {
      cancelled = true;
    };
  }, [profileId, refreshKey, planSel, execSel, crossProject]);

  // Sub-task (parent) Sankey.
  useEffect(() => {
    if (!profileId) return;
    let cancelled = false;
    setSubSankeyErr("");
    GetSubTaskTraceability(profileId, parentSel)
      .then((sk) => {
        if (!cancelled) setSubSankey(sk);
      })
      .catch((e) => {
        if (cancelled) return;
        setSubSankeyErr(errMsg(e));
        console.error("sub-task traceability:", errMsg(e));
      });
    return () => {
      cancelled = true;
    };
  }, [profileId, refreshKey, parentSel]);

  // Export the active tab's diagram (Flow + Table sheets) honouring its current
  // filters. The kind selects which filter slices the backend uses.
  async function exportActive() {
    const kind =
      tab === "req" ? "requirement" : tab === "subtask" ? "subtask" : "execution";
    setExporting(true);
    setExportErr("");
    setExportNotice("");
    try {
      const path = await ExportTraceability(
        profileId,
        kind,
        planSel,
        execSel,
        crossProject,
        reqSel,
        parentSel,
      );
      if (path) setExportNotice(`Saved to ${path}`);
    } catch (e) {
      setExportErr(errMsg(e));
    } finally {
      setExporting(false);
    }
  }

  function openCrossBug(key: string) {
    const base = (jiraUrl ?? "").trim().replace(/\/+$/, "");
    const isDemo = /^(demo$|demo:|mock:)/i.test((jiraUrl ?? "").trim());
    if (base && !isDemo && !key.startsWith("NEW-")) {
      BrowserOpenURL(`${base}/browse/${key}`);
    }
  }

  if (statsErr) {
    return <div className="dashboard error-text">{statsErr}</div>;
  }
  if (!stats) {
    return <div className="dashboard muted">Loading…</div>;
  }
  if (stats.total === 0) {
    return (
      <div className="dashboard">
        <p className="muted">
          No tests cached yet. Run a sync to populate traceability.
        </p>
      </div>
    );
  }

  return (
    <div className="dashboard">
      <div className="containers-mode trace-tabs">
        <button
          className={`seg-btn${tab === "req" ? " seg-btn-active" : ""}`}
          onClick={() => setTab("req")}
        >
          Requirement
        </button>
        <button
          className={`seg-btn${tab === "exec" ? " seg-btn-active" : ""}`}
          onClick={() => setTab("exec")}
        >
          Execution
        </button>
        <button
          className={`seg-btn${tab === "subtask" ? " seg-btn-active" : ""}`}
          onClick={() => setTab("subtask")}
        >
          Sub-task
        </button>
        <button
          className="btn btn-ghost trace-export"
          onClick={exportActive}
          disabled={exporting}
          title="Export the active tab's traceability (Flow + Table) to XLSX"
        >
          {exporting ? "Exporting…" : "Export XLSX"}
        </button>
      </div>
      {exportErr && <p className="error-text">{exportErr}</p>}
      {exportNotice && <p className="muted">{exportNotice}</p>}

      {tab === "req" && (
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
              Couldn&apos;t build the requirement traceability flow: {reqSankeyErr}
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
      )}

      {tab === "exec" && (
        <div className="stat-panel sankey-panel">
          <div className="sankey-head">
            <h4>
              Execution traceability
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
          {crossProject && (
            <div className="crossproject-bugs">
              <h5>
                Cross-project bugs
                <span className="stat-panel-sub">
                  defects filed outside {projectKey || "this project"} but linked
                  to its tests
                </span>
              </h5>
              {crossBugs.length === 0 ? (
                <p className="muted">No cross-project bugs linked.</p>
              ) : (
                <ul className="crossproject-bug-list">
                  {crossBugs.map((b) => (
                    <li key={b.key}>
                      <button
                        className="mono bug-link-key"
                        onClick={() => openCrossBug(b.key)}
                        title={`Open ${b.key} in Jira`}
                      >
                        {b.key}
                      </button>
                      <span className="muted">{b.projectKey}</span>
                      {b.status && <span className="status-pill">{b.status}</span>}
                      <span className="crossproject-bug-summary">
                        {b.summary || "(no summary)"}
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}
        </div>
      )}

      {tab === "subtask" && (
        <div className="stat-panel sankey-panel">
          <div className="sankey-head">
            <h4>
              Sub-task traceability
              <span className="stat-panel-sub">
                how sub-task executions flow from their parent issue through
                executions to run results
              </span>
            </h4>
            {parents.length > 0 && (
              <label className="sankey-filter">
                <span className="muted">Parents</span>
                <MultiSelect
                  allLabel={`All parents (${parents.length})`}
                  title="Filter by one or more parent issues"
                  selected={parentSel}
                  onChange={setParentSel}
                  options={parents.map((p) => ({ value: p, label: p }))}
                />
              </label>
            )}
          </div>
          {subSankeyErr ? (
            <p className="error-text sankey-empty">
              Couldn&apos;t build the sub-task traceability flow: {subSankeyErr}
            </p>
          ) : (
            <SankeyChart
              data={subSankey ?? { nodes: [], links: [] }}
              filtered={parentSel.length > 0}
              onClearFilter={() => setParentSel([])}
              columns={["Parent issues", "Test Executions", "Run Status"]}
              emptyHint="No sub-task executions to trace yet — sync a project that has sub-task Test Executions (or a demo profile)."
              filteredHint="No sub-task execution runs match the selected parent."
            />
          )}
        </div>
      )}
    </div>
  );
}
