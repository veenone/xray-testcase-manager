interface Props {
  // Configured source project keys.
  projects: string[];
  // Currently selected project key; "" means all projects.
  selected: string;
  onSelect: (project: string) => void;
}

// ProjectSidebar is the left-hand project filter for the cross-project link
// pickers (RND_P_4TFINT_05-322): a list of the profile's configured source
// project keys, plus "All projects". Selecting one scopes the item list on the
// right to that project.
export function ProjectSidebar({ projects, selected, onSelect }: Props) {
  return (
    <div className="xproj-sidebar">
      <div className="xproj-sidebar-head">Project</div>
      <button
        type="button"
        className={`xproj-sidebar-item${selected === "" ? " on" : ""}`}
        onClick={() => onSelect("")}
      >
        All projects
      </button>
      {projects.map((p) => (
        <button
          key={p}
          type="button"
          className={`xproj-sidebar-item${selected === p ? " on" : ""}`}
          onClick={() => onSelect(p)}
          title={p}
        >
          <span className="mono">{p}</span>
        </button>
      ))}
    </div>
  );
}
