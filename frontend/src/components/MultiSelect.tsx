import { useEffect, useRef, useState } from "react";

export interface MultiOption {
  value: string;
  label: string;
}

interface Props {
  options: MultiOption[];
  selected: string[];
  onChange: (next: string[]) => void;
  // Label shown when nothing is selected (e.g. "All requirements").
  allLabel?: string;
  title?: string;
}

// MultiSelect is a compact checkbox dropdown: a button shows the selection
// summary ("All …" / one label / "N selected") and opens a checkbox list. Used
// for the dashboard Sankey filters where several requirements / plans /
// executions can be picked at once.
export function MultiSelect({
  options,
  selected,
  onChange,
  allLabel = "All",
  title,
}: Props) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, [open]);

  const sel = new Set(selected);
  function toggle(v: string) {
    const next = new Set(sel);
    if (next.has(v)) next.delete(v);
    else next.add(v);
    onChange([...next]);
  }

  const summary =
    selected.length === 0
      ? allLabel
      : selected.length === 1
        ? (options.find((o) => o.value === selected[0])?.label ?? selected[0])
        : `${selected.length} selected`;

  return (
    <div className="multiselect" ref={ref}>
      <button
        type="button"
        className="multiselect-btn"
        onClick={() => setOpen((o) => !o)}
        title={title}
      >
        <span className="multiselect-summary">{summary}</span>
        <span className="multiselect-caret" aria-hidden="true">
          ▾
        </span>
      </button>
      {open && (
        <div className="multiselect-panel">
          {selected.length > 0 && (
            <button
              type="button"
              className="link-btn multiselect-clear"
              onClick={() => onChange([])}
            >
              Clear all
            </button>
          )}
          <ul className="multiselect-list">
            {options.map((o) => (
              <li key={o.value}>
                <label>
                  <input
                    type="checkbox"
                    checked={sel.has(o.value)}
                    onChange={() => toggle(o.value)}
                  />
                  <span>{o.label}</span>
                </label>
              </li>
            ))}
            {options.length === 0 && <li className="muted">No options</li>}
          </ul>
        </div>
      )}
    </div>
  );
}
