import { useState } from "react";
import { useViewState } from "../lib/viewState";
import { ExcludePreconditionFromDuplicates, errMsg } from "../api";
import { usePreconditionDuplicates } from "../queries/duplicates";
import type {
  PreconditionDuplicateGroup,
  PreconditionDuplicateMember,
} from "../api";
import { Pager } from "./Pager";
import { Modal } from "./Modal";

type Filter = "all" | "identical" | "differ";

const VERDICT_LABEL: Record<string, string> = {
  identical: "definitions identical",
  differ: "definitions differ",
};

// PreconditionDuplicatesView is the Preconditions mode of the Duplicates tab
// (RND_P_4TFINT_05-323): scan preconditions grouped by normalized summary, with
// a verdict comparing each group's definition (condition + description) text.
// Unlike Tests there are no object-level steps, so the whole scan is instant and
// local — no lazy step fetch, no progress walk.
export function PreconditionDuplicatesView({
  profileId,
}: {
  profileId: string;
}) {
  // The scan comes from the query cache with a stable key (Phase 4c); a mutation
  // refreshes it via invalidateProfileData, and the "Scan" button / exclude
  // action refetch it directly.
  const dupQuery = usePreconditionDuplicates(profileId);
  const report = dupQuery.data ?? null;
  const loadError = dupQuery.error ? errMsg(dupQuery.error) : "";
  // Kept for the exclude mutation's own failures.
  const [error, setError] = useState("");
  const [scanning, setScanning] = useState(false);
  const [filter, setFilter] = useViewState<Filter>(profileId, "precond-duplicates", "filter", "all");
  const [expanded, setExpanded] = useViewState<Set<string>>(profileId, "precond-duplicates", "expanded", new Set());
  const [page, setPage] = useViewState(profileId, "precond-duplicates", "page", 0);
  const [pageSize, setPageSize] = useViewState(profileId, "precond-duplicates", "pageSize", 15);
  const [compare, setCompare] = useState<PreconditionDuplicateGroup | null>(null);

  function toggle(norm: string) {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(norm)) next.delete(norm);
      else next.add(norm);
      return next;
    });
  }

  function changeFilter(f: Filter) {
    setFilter(f);
    setPage(0);
  }

  async function exclude(key: string) {
    try {
      await ExcludePreconditionFromDuplicates(profileId, key);
      void dupQuery.refetch();
    } catch (e) {
      setError(errMsg(e));
    }
  }

  const groups = (report?.groups ?? []).filter((g) =>
    filter === "all" ? true : g.definitionVerdict === filter,
  );
  const totalPages = Math.max(1, Math.ceil(groups.length / pageSize));
  const safePage = Math.min(page, totalPages - 1);
  const pageGroups = groups.slice(safePage * pageSize, (safePage + 1) * pageSize);

  return (
    <>
      <div className="dup-toolbar">
        <button
          className="btn btn-primary"
          disabled={scanning}
          onClick={() => {
            setPage(0);
            setScanning(true);
            void dupQuery.refetch();
            window.setTimeout(() => setScanning(false), 300);
          }}
        >
          {scanning ? "Scanning…" : "⟳ Scan"}
        </button>
        {report?.scannedAt && (
          <span className="muted">
            scanned {new Date(report.scannedAt).toLocaleString()}
          </span>
        )}
        <span style={{ flex: 1 }} />
        <span className="muted">filter:</span>
        <div className="dup-seg">
          {(["all", "identical", "differ"] as Filter[]).map((f) => (
            <button
              key={f}
              className={`dup-seg-btn${filter === f ? " on" : ""}`}
              onClick={() => changeFilter(f)}
            >
              {f === "all"
                ? "All"
                : f === "identical"
                  ? "Definitions identical"
                  : "Definitions differ"}
            </button>
          ))}
        </div>
      </div>

      {(loadError || error) && (
        <div className="error-text dup-error">{loadError || error}</div>
      )}

      {report && (
        <div className="dup-tiles">
          <div className="dup-tile t-grp">
            <b>{report.groupCount}</b>
            <span>duplicate groups</span>
          </div>
          <div className="dup-tile t-grp">
            <b>{report.preconditionCount}</b>
            <span>duplicate preconditions</span>
          </div>
          <div className="dup-tile t-dup">
            <b>{report.definitionIdentical}</b>
            <span>definitions identical</span>
          </div>
          <div className="dup-tile t-diff">
            <b>{report.definitionDiffer}</b>
            <span>definitions differ</span>
          </div>
          <div className="dup-tile t-muted">
            <b>{report.excluded}</b>
            <span>excluded</span>
          </div>
        </div>
      )}

      {report && report.groupCount === 0 && (
        <p className="muted dup-empty">
          No duplicate preconditions found. Preconditions are grouped when two or
          more share the same summary (ignoring case and spacing).
        </p>
      )}

      <div className="dup-body">
        <div className="dup-list-col">
          <div className="dup-list">
            {pageGroups.map((g) => {
              const open = expanded.has(g.normalizedSummary);
              return (
                <div className="dup-group" key={g.normalizedSummary}>
                  <div
                    className="dup-ghead"
                    onClick={() => toggle(g.normalizedSummary)}
                  >
                    <span className="dup-caret">{open ? "▾" : "▸"}</span>
                    <span className="dup-gtitle">"{g.displaySummary}"</span>
                    <span className="dup-pill p-n">
                      {g.members.length} preconditions
                    </span>
                    <span className="dup-pill p-sum">summary identical</span>
                    <span className={`dup-pill p-${g.definitionVerdict}`}>
                      {VERDICT_LABEL[g.definitionVerdict]}
                    </span>
                    <button
                      className="btn dup-cmp"
                      onClick={(e) => {
                        e.stopPropagation();
                        setCompare(g);
                      }}
                    >
                      Compare definitions
                    </button>
                  </div>
                  {open && (
                    <div className="dup-members">
                      {g.members.map((m) => (
                        <div className="dup-mrow" key={m.key}>
                          <span className="dup-mkey">{m.key}</span>
                          <span className="dup-mstatus">{m.type || "—"}</span>
                          <span className="dup-mfolder">
                            {m.testCount} test{m.testCount === 1 ? "" : "s"}
                          </span>
                          <div className="dup-acts">
                            <button
                              className="dup-act act-ex"
                              onClick={() => exclude(m.key)}
                            >
                              Exclude
                            </button>
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              );
            })}
          </div>

          {groups.length > 0 && (
            <Pager
              page={safePage}
              pageSize={pageSize}
              total={groups.length}
              onPage={setPage}
              onPageSize={(n) => {
                setPageSize(n);
                setPage(0);
              }}
            />
          )}
        </div>
      </div>

      {compare && (
        <DefinitionCompareModal group={compare} onClose={() => setCompare(null)} />
      )}
    </>
  );
}

// DefinitionCompareModal shows the raw summary / condition / description of each
// member side by side, highlighting rows that actually differ. Pure frontend —
// the text is already in the member payload.
function DefinitionCompareModal({
  group,
  onClose,
}: {
  group: PreconditionDuplicateGroup;
  onClose: () => void;
}) {
  const members = group.members;
  const rows: {
    label: string;
    value: (m: PreconditionDuplicateMember) => string;
  }[] = [
    { label: "summary", value: (m) => m.summary },
    { label: "condition", value: (m) => m.condition },
    { label: "description", value: (m) => m.description },
  ];

  return (
    <Modal onClose={onClose} className="modal dup-compare-modal" labelledBy="precond-dup-compare-title">
        <div className="pending-head">
          <h2 id="precond-dup-compare-title">Compare definitions — "{group.displaySummary}"</h2>
          <button className="btn btn-ghost" onClick={onClose} title="Close">
            ✕
          </button>
        </div>

        <div className="dup-compare-wrap">
          <p className="muted dup-compare-ref">
            These preconditions share the same normalized summary,
            "{group.normalizedSummary}". Their definition text can still differ.
          </p>
          <table className="dup-compare-table">
            <thead>
              <tr>
                <th className="dup-compare-idx">field</th>
                {members.map((m) => (
                  <th key={m.key} className="mono">
                    {m.key}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => {
                const values = members.map((m) => row.value(m));
                const differs = new Set(values).size > 1;
                return (
                  <tr
                    key={row.label}
                    className={differs ? "dup-compare-diff" : undefined}
                  >
                    <td className="dup-compare-idx">{row.label}</td>
                    {values.map((v, j) => (
                      <td key={members[j].key}>
                        {v && v.trim() !== "" ? (
                          <div className="dup-compare-step">{v}</div>
                        ) : (
                          <span className="muted">—</span>
                        )}
                      </td>
                    ))}
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
    </Modal>
  );
}
