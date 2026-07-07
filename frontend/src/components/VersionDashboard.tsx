import { useCallback, useEffect, useState } from "react";
import { GetVersionDistribution, GetCRAdoption, errMsg } from "../api";
import type { VersionShare, CRShare } from "../api";

interface Props {
  profileId: string;
  canonicalId: string;
}

// VersionDashboard shows the member-count distribution across versions
// and the CR adoption (can/cannot/pending tallies per change request).
export function VersionDashboard({ profileId, canonicalId }: Props) {
  const [dist, setDist] = useState<VersionShare[]>([]);
  const [crAdoption, setCrAdoption] = useState<CRShare[]>([]);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    if (!profileId || !canonicalId) return;
    setError("");
    try {
      const [d, c] = await Promise.all([
        GetVersionDistribution(profileId, canonicalId),
        GetCRAdoption(profileId, canonicalId),
      ]);
      setDist(d ?? []);
      setCrAdoption(c ?? []);
    } catch (e) {
      setError(errMsg(e));
    }
  }, [profileId, canonicalId]);

  useEffect(() => {
    void load();
  }, [load]);

  const maxMembers = Math.max(...dist.map((d) => d.memberCount), 1);

  return (
    <div className="cov-version-dashboard">
      {error && <div className="cov-error">{error}</div>}

      <section className="cov-dash-section">
        <h4 className="cov-section-title">Member distribution by version</h4>
        {dist.length === 0 ? (
          <p className="cov-empty">No version data yet.</p>
        ) : (
          <div className="cov-dist-bars">
            {dist.map((d) => (
              <div key={d.versionId} className="cov-dist-row">
                <span className="cov-dist-label">
                  {d.versionName}
                  <span className={`cov-badge cov-badge-${d.status}`}>{d.status}</span>
                </span>
                <div className="cov-dist-bar-wrap">
                  <div
                    className="cov-dist-bar"
                    style={{ width: `${Math.round((d.memberCount / maxMembers) * 100)}%` }}
                  />
                </div>
                <span className="cov-dist-count">{d.memberCount}</span>
              </div>
            ))}
          </div>
        )}
      </section>

      {crAdoption.length > 0 && (
        <section className="cov-dash-section">
          <h4 className="cov-section-title">CR adoption</h4>
          <table className="cov-table">
            <thead>
              <tr>
                <th>Change request</th>
                <th>Status</th>
                <th className="cov-pass">Can accept</th>
                <th className="cov-fail">Cannot</th>
                <th className="cov-notrun">Pending</th>
              </tr>
            </thead>
            <tbody>
              {crAdoption.map((cr) => (
                <tr key={cr.crId}>
                  <td>{cr.title}</td>
                  <td><span className={`cov-badge cov-badge-cr-${cr.status}`}>{cr.status}</span></td>
                  <td className="cov-pass">{cr.canAccept}</td>
                  <td className="cov-fail">{cr.cannotAccept}</td>
                  <td className="cov-notrun">{cr.pending}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      )}
    </div>
  );
}
