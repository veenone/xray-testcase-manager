import { errMsg } from "../api";
import { useProfile } from "../contexts/ProfileContext";
import { useDuplicates } from "../queries/duplicates";

interface Props {
  onOpen: () => void; // navigate to the Duplicates tab
}

// DuplicatesCard summarizes duplicate detection on the dashboard (FR — duplicate
// management), with bordered, color-coded tiles. Computed from the local cache.
// Shares the tests-mode scan query with DuplicatesView, so it dedupes to one
// cache entry and refreshes via invalidateProfileData (Phase 4c).
export function DuplicatesCard({ onOpen }: Props) {
  const { activeId: profileId } = useProfile();
  const dupQuery = useDuplicates(profileId, "tests");
  const rep = dupQuery.data ?? null;

  return (
    <div className="dup-card">
      <div className="dup-card-head">
        <span className="dup-card-title">Duplicates</span>
        <button className="btn dup-card-open" onClick={onOpen}>
          Open Duplicates →
        </button>
      </div>
      {dupQuery.error && (
        <div className="error-text">{errMsg(dupQuery.error)}</div>
      )}
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
