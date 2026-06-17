import { useEffect, useRef, useState } from "react";

export interface SelectOption {
  value: string;
  label: string;
}

interface Props {
  options: SelectOption[];
  value: string;
  onChange: (value: string) => void;
  // Shown on the button when nothing is selected.
  placeholder?: string;
  disabled?: boolean;
  title?: string;
  // Extra class on the wrapper, so callers can keep existing inline spacing.
  className?: string;
}

// SearchableSelect is a single-select dropdown with a type-to-filter box, for
// picking one entry from a long list (e.g. a requirement or precondition) where
// a native <select> means scrolling through everything (RND_P_4TFINT_05-200). It
// mirrors MultiSelect's look: a button shows the current selection and opens a
// panel with a search field and the filtered options.
export function SearchableSelect({
  options,
  value,
  onChange,
  placeholder = "Select…",
  disabled = false,
  title,
  className,
}: Props) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, [open]);

  // Clear the filter whenever the panel closes.
  useEffect(() => {
    if (!open) setQuery("");
  }, [open]);

  const q = query.trim().toLowerCase();
  const shown = q
    ? options.filter((o) => o.label.toLowerCase().includes(q))
    : options;
  const current = options.find((o) => o.value === value);

  function pick(v: string) {
    onChange(v);
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
        disabled={disabled}
        onClick={() => setOpen((o) => !o)}
        title={title}
      >
        <span className="multiselect-summary">
          {current ? current.label : placeholder}
        </span>
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
                <button
                  type="button"
                  className={`searchable-option${o.value === value ? " is-selected" : ""}`}
                  onClick={() => pick(o.value)}
                >
                  {o.label}
                </button>
              </li>
            ))}
            {options.length === 0 && <li className="muted">No options</li>}
            {options.length > 0 && shown.length === 0 && (
              <li className="muted">No matches</li>
            )}
          </ul>
        </div>
      )}
    </div>
  );
}
