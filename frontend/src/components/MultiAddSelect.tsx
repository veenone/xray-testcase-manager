import { useEffect, useRef, useState } from "react";

export interface MultiAddOption {
  value: string;
  label: string;
}

interface Props {
  options: MultiAddOption[];
  // Called once with all ticked values when the user confirms (Add N).
  onAdd: (values: string[]) => void;
  // Button label shown when the panel is closed (e.g. "+ Add precondition…").
  placeholder: string;
  // Extra class on the wrapper, so callers can keep existing spacing / the
  // left-anchored panel rule (.pre-add .multiselect-panel).
  className?: string;
  title?: string;
}

// MultiAddSelect is a multi-pick variant of SearchableSelect: a button opens a
// panel with a search box and a checkbox list of candidates, plus an "Add N"
// button that links every ticked entry in one apply (RND_P_4TFINT_05-224). It
// reuses the multiselect panel styling so it matches the rest of the app. The
// selection is staged locally and only flushed to onAdd on confirm, so callers
// can compute a single union update (one pending change, not one per item).
export function MultiAddSelect({
  options,
  onAdd,
  placeholder,
  className,
  title,
}: Props) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, [open]);

  // Reset the filter and staged ticks each time the panel closes so it reopens
  // clean (and so a successful Add never leaves stale checks behind).
  useEffect(() => {
    if (!open) {
      setQuery("");
      setPicked(new Set());
    }
  }, [open]);

  const q = query.trim().toLowerCase();
  const shown = q
    ? options.filter((o) => o.label.toLowerCase().includes(q))
    : options;

  function toggle(v: string) {
    setPicked((prev) => {
      const next = new Set(prev);
      if (next.has(v)) next.delete(v);
      else next.add(v);
      return next;
    });
  }

  function commit() {
    if (picked.size === 0) return;
    onAdd([...picked]);
    setOpen(false);
  }

  return (
    <div
      className={`multiselect searchable-select${className ? " " + className : ""}`}
      ref={ref}
    >
      <button
        type="button"
        className="multiselect-btn"
        onClick={() => setOpen((o) => !o)}
        title={title}
      >
        <span className="multiselect-summary">{placeholder}</span>
        <span className="multiselect-caret" aria-hidden="true">
          ▾
        </span>
      </button>
      {open && (
        <div className="multiselect-panel">
          {options.length > 8 && (
            <input
              className="multiselect-search"
              type="search"
              autoFocus
              placeholder="Search…"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
          )}
          <ul className="multiselect-list">
            {shown.map((o) => (
              <li key={o.value}>
                <label>
                  <input
                    type="checkbox"
                    checked={picked.has(o.value)}
                    onChange={() => toggle(o.value)}
                  />
                  <span>{o.label}</span>
                </label>
              </li>
            ))}
            {options.length === 0 && <li className="muted">No options</li>}
            {options.length > 0 && shown.length === 0 && (
              <li className="muted">No matches</li>
            )}
          </ul>
          <button
            type="button"
            className="btn btn-primary multiadd-confirm"
            disabled={picked.size === 0}
            onClick={commit}
          >
            Add {picked.size > 0 ? picked.size : ""}
          </button>
        </div>
      )}
    </div>
  );
}
