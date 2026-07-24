import { useCallback, useEffect, useState } from "react";
import { GetCoveragePublishStatus, PublishCoverageGroups, errMsg } from "../api";
import type {
  CoveragePublishGroupStatus,
  CoveragePublishResult,
  CoveragePublishState,
} from "../api";
import { useCapabilities } from "../features";
import { useConfirm } from "./useConfirm";

interface Props {
  profileId: string;
  versionId: string;
}

const STATE_LABEL: Record<CoveragePublishState, string> = {
  NotPublished: "Not published",
  InSync: "Published",
  LocalChanges: "Local changes",
  Drift: "Drift",
  Conflict: "Conflict",
};

const STATE_BADGE_CLASS: Record<CoveragePublishState, string> = {
  NotPublished: "cov-badge-pub-notpublished",
  InSync: "cov-badge-pub-insync",
  LocalChanges: "cov-badge-pub-local",
  Drift: "cov-badge-pub-drift",
  Conflict: "cov-badge-pub-conflict",
};

// CoveragePublishPanel drives coverage-group -> Xray Test Set publishing for
// the version currently selected in CoverageView: a per-group status chip
// (Not published / Published / Local changes / Drift / Conflict), a "Publish
// to Xray" action that surfaces partial failures explicitly (one group
// failing never hides the others' results), and a drift panel, collapsed by
// default since InSync/NotPublished is the common case, listing exactly what
// changed in Jira per group. A test added to a Test Set in Jira carries no
// value-level information (Xray can't express which parameter value it
// covers), so the drift panel only ever explains that and points at the
// existing "Map…" test picker in the Coverage tab; it never offers an
// "accept remote" action that would have to guess a value.
//
// A separate file rather than folding into CoverageView.tsx, which is
// already large: this keeps the publish/drift concern (its own fetch, its
// own busy-state, its own honest-limit copy) self-contained and easy to gate
// or remove as a unit.
export function CoveragePublishPanel({ profileId, versionId }: Props) {
  const caps = useCapabilities(profileId);
  // Mirrors the compound backend guard in app_coverage_publish.go exactly
  // (SupportsContainers && KindTestSet in ContainerKinds) so this never
  // drifts out of sync with what the backend actually gates on.
  const supportsTestSets = caps.supportsContainers && (caps.containerKinds?.includes("testset") ?? false);

  const [statuses, setStatuses] = useState<CoveragePublishGroupStatus[]>([]);
  const [loading, setLoading] = useState(false);
  const [publishing, setPublishing] = useState(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState<CoveragePublishResult | null>(null);
  const [showDetails, setShowDetails] = useState(false);
  const [expandedGroupId, setExpandedGroupId] = useState<string | null>(null);
  const { confirm, confirmUI } = useConfirm();

  const loadStatus = useCallback(async () => {
    if (!profileId || !versionId) {
      setStatuses([]);
      return;
    }
    setLoading(true);
    setError("");
    try {
      const s = await GetCoveragePublishStatus(profileId, versionId);
      // The generated binding types `state` as a plain string; narrow it to
      // the closed ReconcileState union so callers get exhaustiveness.
      setStatuses((s ?? []).map((g) => ({ ...g, state: g.state as CoveragePublishState })));
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setLoading(false);
    }
  }, [profileId, versionId]);

  useEffect(() => {
    if (!supportsTestSets) return;
    void loadStatus();
  }, [loadStatus, supportsTestSets]);

  // A publish result, an expanded drift row, and the status list itself all
  // belong to a specific version; carrying any of them over to a newly
  // selected version would show stale group names, container keys, or
  // Drift/Conflict chips under the new version until the re-fetch resolves.
  // Cleared synchronously (not left to loadStatus's own fetch) so the render
  // right after a version switch never shows another version's data.
  useEffect(() => {
    setResult(null);
    setExpandedGroupId(null);
    setStatuses([]);
  }, [versionId]);

  async function publish() {
    if (!profileId || !versionId || publishing) return;
    // Drift/Conflict groups have Jira-side changes that publishing would
    // silently overwrite. Ask first, but only then: when nothing is in
    // Drift or Conflict this stays a single click, matching the common case.
    const impacted = statuses.filter((s) => s.state === "Drift" || s.state === "Conflict").length;
    if (impacted > 0) {
      const ok = await confirm({
        title: "Overwrite changes made in Jira?",
        message: `${impacted} coverage group${impacted === 1 ? "" : "s"} changed in Jira since the last publish. Publishing now replaces ${
          impacted === 1 ? "its" : "their"
        } Test Set membership with the local coverage model, discarding ${
          impacted === 1 ? "that" : "those"
        } Jira-side change${impacted === 1 ? "" : "s"}.`,
        confirmLabel: "Publish anyway",
        danger: true,
      });
      if (!ok) return;
    }
    setPublishing(true);
    setError("");
    setResult(null);
    try {
      const r = await PublishCoverageGroups(profileId, versionId);
      setResult(r);
      await loadStatus();
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setPublishing(false);
    }
  }

  if (!supportsTestSets || !versionId) return null;

  const counts: Partial<Record<CoveragePublishState, number>> = {};
  for (const s of statuses) counts[s.state] = (counts[s.state] ?? 0) + 1;

  const summaryParts: string[] = [];
  if (counts.InSync) summaryParts.push(`${counts.InSync} published`);
  if (counts.LocalChanges) summaryParts.push(`${counts.LocalChanges} pending publish`);
  if (counts.Drift) summaryParts.push(`${counts.Drift} drift`);
  if (counts.Conflict) summaryParts.push(`${counts.Conflict} conflict`);
  if (counts.NotPublished) summaryParts.push(`${counts.NotPublished} not published`);
  // error takes priority over the "no groups" reading: with an empty status
  // list AND a failed fetch, the empty list is a symptom of the failure, not
  // a real "nothing to publish" state, so don't say both at once.
  const summary =
    statuses.length === 0
      ? loading
        ? "Loading publish status…"
        : error
          ? "Publish status unavailable."
          : "No coverage groups to publish yet."
      : summaryParts.join(" · ");

  return (
    <div className="cov-publish">
      <div className="cov-publish-head">
        <span className="cov-section-title">Xray publish</span>
        <span className="cov-muted">{summary}</span>
        {statuses.length > 0 && (
          <button className="btn btn-ghost" onClick={() => setShowDetails((v) => !v)}>
            {showDetails ? "Hide details" : "Details"}
          </button>
        )}
        <button
          className="btn btn-primary"
          disabled={publishing || loading || statuses.length === 0}
          onClick={() => void publish()}
          title="Push every coverage group's current test mapping to its Xray Test Set"
        >
          {publishing ? "Publishing…" : "Publish to Xray"}
        </button>
      </div>

      {error && <div className="cov-error">{error}</div>}

      {result && (
        <div className={result.failed > 0 ? "cov-error" : "cov-notice"}>
          <div className="cov-publish-result-head">
            <span>
              Publish complete: {result.created} created, {result.updated} updated
              {result.failed > 0 ? `, ${result.failed} failed` : ""}.
            </span>
            <button className="btn btn-ghost" onClick={() => setResult(null)} title="Dismiss">
              ✕
            </button>
          </div>
          {result.failed > 0 && (
            <ul className="cov-publish-fail-list">
              {(result.groups ?? [])
                .filter((g) => g.error)
                .map((g) => (
                  <li key={g.groupId}>
                    <strong>{g.groupName}</strong>: {g.error}
                  </li>
                ))}
            </ul>
          )}
        </div>
      )}

      {showDetails && statuses.length > 0 && (
        <ul className="cov-publish-list">
          {statuses.map((gs) => (
            <li key={gs.groupId} className="cov-publish-item">
              <div className="cov-publish-row">
                <span className="cov-group-name">{gs.groupName}</span>
                <span className={`cov-badge ${STATE_BADGE_CLASS[gs.state]}`}>
                  {STATE_LABEL[gs.state] ?? gs.state}
                </span>
                {gs.containerKey && <span className="cov-muted">{gs.containerKey}</span>}
                {(gs.state === "Drift" || gs.state === "Conflict") && (
                  <button
                    className="btn btn-ghost"
                    onClick={() =>
                      setExpandedGroupId((v) => (v === gs.groupId ? null : gs.groupId))
                    }
                  >
                    {expandedGroupId === gs.groupId ? "Hide drift" : "View drift"}
                  </button>
                )}
              </div>
              {expandedGroupId === gs.groupId &&
                (gs.state === "Drift" || gs.state === "Conflict") && <DriftDetail gs={gs} />}
            </li>
          ))}
        </ul>
      )}
      {confirmUI}
    </div>
  );
}

// DriftDetail renders what changed on each side since the last publish for
// one Drift or Conflict group. It only ever explains: it has no "accept
// remote" action, because a test added to the Test Set in Jira carries no
// value-level information for this package to act on (see reconcile.go).
function DriftDetail({ gs }: { gs: CoveragePublishGroupStatus }) {
  const remoteAdded = gs.remoteAdded ?? [];
  const remoteRemoved = gs.remoteRemoved ?? [];
  const localAdded = gs.localAdded ?? [];
  const localRemoved = gs.localRemoved ?? [];

  return (
    <div className="cov-publish-drift">
      <p className="cov-muted">
        Jira's Test Set{gs.containerKey ? ` (${gs.containerKey})` : ""} no longer matches the last
        published snapshot
        {gs.state === "Conflict" ? ", and the local coverage model changed too" : ""}.
      </p>
      {remoteAdded.length > 0 && (
        <div className="cov-publish-drift-section">
          <strong>Added in Jira:</strong> <span className="cov-tests">{remoteAdded.join(", ")}</span>
          <p className="cov-muted">
            Xray has no way to record which parameter value {remoteAdded.length === 1 ? "this test" : "these tests"}{" "}
            cover: a test added directly to a Test Set in Jira carries no value-level information.
            Assign {remoteAdded.length === 1 ? "it" : "them"} yourself: on the Coverage tab, open the
            covering value's row and use "Map…" to attach the test.
          </p>
        </div>
      )}
      {remoteRemoved.length > 0 && (
        <div className="cov-publish-drift-section">
          <strong>Removed in Jira:</strong>{" "}
          <span className="cov-tests">{remoteRemoved.join(", ")}</span>
        </div>
      )}
      {(localAdded.length > 0 || localRemoved.length > 0) && (
        <div className="cov-publish-drift-section">
          <strong>Also changed locally since publish:</strong>{" "}
          {localAdded.length > 0 && <span className="cov-tests">+{localAdded.join(", ")} </span>}
          {localRemoved.length > 0 && <span className="cov-tests">-{localRemoved.join(", ")}</span>}
        </div>
      )}
      <p className="cov-muted">
        Republishing this group overwrites the Test Set's membership with the local coverage
        model: any test added directly in Jira and not yet mapped to a value above will be
        removed from the Test Set.
      </p>
    </div>
  );
}
