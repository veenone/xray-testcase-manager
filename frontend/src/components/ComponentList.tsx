import type { Bucket } from "../api";

interface Props {
  components: Bucket[];
  selected: string; // "" means "All tests"
  emptyLabel: string;
  onSelect: (name: string) => void;
}

// ComponentList is the browse sidebar when grouping by Component. It mirrors the
// folder tree / container list — an "All tests" entry plus one row per Jira
// component with its test count — and selecting one filters the grid to tests
// carrying that component.
export function ComponentList({
  components,
  selected,
  emptyLabel,
  onSelect,
}: Props) {
  return (
    <nav className="folder-tree">
      <div
        className={"folder-item" + (selected === "" ? " folder-selected" : "")}
        onClick={() => onSelect("")}
      >
        <span className="folder-caret" />
        <span className="folder-name">All tests</span>
      </div>
      {components.length === 0 ? (
        <div className="folder-item muted">{emptyLabel}</div>
      ) : (
        components.map((c) => (
          <div
            key={c.label}
            className={
              "folder-item" + (selected === c.label ? " folder-selected" : "")
            }
            style={{ paddingLeft: 24 }}
            onClick={() => onSelect(c.label)}
            title={`${c.label} — ${c.count} test(s)`}
          >
            <span className="folder-name">{c.label}</span>
            <span className="folder-count">{c.count}</span>
          </div>
        ))
      )}
    </nav>
  );
}
