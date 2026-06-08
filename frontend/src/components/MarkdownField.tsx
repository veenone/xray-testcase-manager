import { useEffect, useRef, useState } from "react";

import { Markdown } from "./Markdown";

interface Props {
  value: string;
  onChange: (value: string) => void;
  // onCommit fires on blur — the same place the previous bare textarea saved.
  onCommit: () => void;
  placeholder?: string;
  rows?: number;
  // multiline picks a <textarea> (default) vs a single-line <input> for the
  // editor; both render markdown the same way when idle.
  multiline?: boolean;
  // className is applied to BOTH the editor and the rendered view so the
  // field keeps the same footprint as it toggles between them.
  className?: string;
}

// MarkdownField shows its value as rendered markdown, and switches to a raw
// editor when the user clicks or tabs into it — committing on blur. This keeps
// editing exactly as it was (plain text, save-on-blur) while giving readers a
// formatted view of descriptions and step text.
export function MarkdownField({
  value,
  onChange,
  onCommit,
  placeholder,
  rows = 4,
  multiline = true,
  className,
}: Props) {
  const [editing, setEditing] = useState(false);
  const ref = useRef<HTMLTextAreaElement & HTMLInputElement>(null);

  // On entering edit mode, focus the editor and drop the caret at the end.
  useEffect(() => {
    if (editing && ref.current) {
      const el = ref.current;
      el.focus();
      const end = el.value.length;
      el.setSelectionRange(end, end);
    }
  }, [editing]);

  function finishEditing() {
    setEditing(false);
    onCommit();
  }

  if (editing) {
    const shared = {
      ref,
      value,
      className,
      placeholder,
      onChange: (e: React.ChangeEvent<HTMLTextAreaElement & HTMLInputElement>) =>
        onChange(e.target.value),
      onBlur: finishEditing,
    };
    return multiline ? <textarea {...shared} rows={rows} /> : <input {...shared} />;
  }

  // Idle view. role/tabIndex make it keyboard-reachable; Enter also opens the
  // editor so it's not mouse-only.
  return (
    <div
      className={`md-view ${className ?? ""}`}
      role="textbox"
      tabIndex={0}
      title="Click to edit"
      onClick={() => setEditing(true)}
      onFocus={() => setEditing(true)}
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          e.preventDefault();
          setEditing(true);
        }
      }}
    >
      {value.trim() ? (
        <Markdown>{value}</Markdown>
      ) : (
        <span className="muted md-placeholder">
          {placeholder ?? "Click to edit"}
        </span>
      )}
    </div>
  );
}
