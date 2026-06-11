import { useEffect, useState } from "react";

import { ScanDuplicates, errMsg } from "../api";
import type { DuplicateReport } from "../api";

interface Props {
  profileId: string;
  refreshKey: number;
  onOpen: () => void; // navigate to the Duplicates tab
}

// DuplicatesCard summarizes duplicate detection on the dashboard (FR — duplicate
// management), with bordered, color-coded tiles. Computed from the local cache.
export function DuplicatesCard({ profileId, refreshKey, onOpen }: Props) {
  const [rep, setRep] = useState<DuplicateReport | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!profileId) return;
    let cancelled = false;
    ScanDuplicates(profileId)
      .then((r) => {
        if (!cancelled) setRep(r as unknown as DuplicateReport);
      })
      .catch((e) => {
        if (!cancelled) setError(errMsg(e));
      });
    return () => {
      cancelled = true;
    };
  }, [profileId, refreshKey]);

  return (
    <div className="dup-card">
      <div className="dup-card-head">
        <span className="dup-card-title">Duplicates</span>
        <button className="btn dup-card-open" onClick={onOpen}>
          Open Duplicates →
        </button>
      </div>
      {error && <div className="error-text">{error}</div>}
      {rep && (
        <div className="dup-tiles">
          <div className="dup-tile t-grp"><b>{rep.groupCount}</b><span>duplicate groups</span></div>
          <div className="dup-tile t-grp"><b>{rep.testCount}</b><span>duplicate tests</span></div>
          <div className="dup-tile t-dup"><b>{rep.stepsIdentical}</b><span>steps identical</span></div>
          <div className="dup-tile t-diff"><b>{rep.stepsDiffer}</b><span>steps differ</span></div>
          <div className="dup-tile t-muted"><b>{rep.stepsUnscanned}</b><span>not scanned</span></div>
          <div className="dup-tile t-muted"><b>{rep.excluded}</b><span>excluded</span></div>
        </div>
      )}
    </div>
  );
}
