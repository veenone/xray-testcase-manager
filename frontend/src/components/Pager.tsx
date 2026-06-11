import { useState } from "react";

interface Props {
  page: number; // 0-based
  pageSize: number;
  total: number;
  onPage: (p: number) => void;
  onPageSize: (n: number) => void;
  pageSizeOptions?: number[];
  // compact stacks the controls vertically — for narrow side panels (the
  // requirement / precondition master lists) where a single row would cramp.
  compact?: boolean;
}

const DEFAULT_OPTIONS = [10, 15, 25, 50, 100];

// Pager is the shared pagination control used by the board, duplicates,
// requirements and precondition lists: a rows-per-page selector, the range and
// page count, Prev/Next, and a manual "go to page" input.
export function Pager({
  page,
  pageSize,
  total,
  onPage,
  onPageSize,
  pageSizeOptions = DEFAULT_OPTIONS,
  compact = false,
}: Props) {
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  const safePage = Math.min(Math.max(0, page), totalPages - 1);
  const [goto, setGoto] = useState("");

  function submitGoto() {
    const n = parseInt(goto, 10);
    if (!Number.isNaN(n)) {
      onPage(Math.min(totalPages - 1, Math.max(0, n - 1)));
    }
    setGoto("");
  }

  return (
    <div className={`board-pager pager${compact ? " pager-compact" : ""}`}>
      <label className="board-pagesize">
        <span className="muted">Rows per page</span>
        <select
          value={pageSize}
          onChange={(e) => onPageSize(Number(e.target.value))}
        >
          {pageSizeOptions.map((n) => (
            <option key={n} value={n}>
              {n}
            </option>
          ))}
        </select>
      </label>
      <span className="muted board-pager-range">
        {(safePage * pageSize + 1).toLocaleString()}–
        {Math.min((safePage + 1) * pageSize, total).toLocaleString()} of{" "}
        {total.toLocaleString()} · page {safePage + 1} of {totalPages}
      </span>
      <span className="board-pager-nav">
        <button
          className="btn"
          disabled={safePage === 0}
          onClick={() => onPage(Math.max(0, safePage - 1))}
        >
          ‹ Prev
        </button>
        <input
          className="pager-goto"
          type="number"
          min={1}
          max={totalPages}
          placeholder="#"
          value={goto}
          onChange={(e) => setGoto(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") submitGoto();
          }}
          title="Go to page"
        />
        <button className="btn" onClick={submitGoto} disabled={goto === ""}>
          Go
        </button>
        <button
          className="btn"
          disabled={safePage >= totalPages - 1}
          onClick={() => onPage(Math.min(totalPages - 1, safePage + 1))}
        >
          Next ›
        </button>
      </span>
    </div>
  );
}
