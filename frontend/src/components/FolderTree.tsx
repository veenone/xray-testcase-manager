import { useMemo, useState } from "react";
import type { Folder } from "../api";

interface Props {
  folders: Folder[];
  selected: string; // "" means "All tests"
  onSelect: (folderId: string) => void;
  onCreate: (parentPath: string) => void;
  onRename: (path: string, currentName: string) => void;
  onDelete: (path: string) => void;
}

// FolderIcon is a small flat folder glyph (closed / open), styled to read like
// the Xray Test Repository tree rather than a coloured emoji.
function FolderIcon({ open }: { open: boolean }) {
  return (
    <svg
      className="folder-icon"
      width="15"
      height="15"
      viewBox="0 0 16 16"
      aria-hidden="true"
    >
      {open ? (
        <path d="M1.5 4.2A1.2 1.2 0 0 1 2.7 3h3.1a1 1 0 0 1 .8.4l.6.8H13a1.2 1.2 0 0 1 1.2 1.2v.4H4.6a1.2 1.2 0 0 0-1.16.9L2.1 12.4 1.5 6V4.2zM3.9 7.3h11l-1.2 4.5a1.2 1.2 0 0 1-1.16.9H3.2a.8.8 0 0 1-.78-1l1.05-3.95a.8.8 0 0 1 .43-.45z" />
      ) : (
        <path d="M1.5 4.2A1.2 1.2 0 0 1 2.7 3h3.1a1 1 0 0 1 .8.4l.6.8H13a1.2 1.2 0 0 1 1.2 1.2v6.4A1.2 1.2 0 0 1 13 13H2.7a1.2 1.2 0 0 1-1.2-1.2V4.2z" />
      )}
    </svg>
  );
}

// folderCount renders the per-folder test count the way Xray's tree does: the
// total under the folder, with the direct count shown first when it differs.
// Empty folders get no badge to keep the tree quiet.
function folderCount(f: Folder): string | null {
  const total = f.totalTestCount || f.testCount;
  if (total <= 0) return null;
  if (f.testCount > 0 && f.testCount !== f.totalTestCount) {
    return `${f.testCount} (${f.totalTestCount})`;
  }
  return String(total);
}

export function FolderTree({
  folders,
  selected,
  onSelect,
  onCreate,
  onRename,
  onDelete,
}: Props) {
  // Index folders by parentId so each node can find its children in O(1).
  const childrenOf = useMemo(() => {
    const map = new Map<string, Folder[]>();
    for (const f of folders) {
      const arr = map.get(f.parentId);
      if (arr) arr.push(f);
      else map.set(f.parentId, [f]);
    }
    return map;
  }, [folders]);

  const roots = childrenOf.get("") ?? [];
  const allCount = useMemo(
    () => roots.reduce((sum, r) => sum + (r.totalTestCount || 0), 0),
    [roots],
  );

  return (
    <nav className="folder-tree">
      <div
        className={"folder-item" + (selected === "" ? " folder-selected" : "")}
        onClick={() => onSelect("")}
      >
        <span className="folder-caret" />
        <FolderIcon open={selected === ""} />
        <span className="folder-name">All tests</span>
        {allCount > 0 && <span className="folder-count">{allCount}</span>}
        <button
          className="folder-action"
          title="New top-level folder"
          onClick={(e) => {
            e.stopPropagation();
            onCreate("");
          }}
        >
          ＋
        </button>
      </div>
      {roots.map((root) => (
        <FolderNode
          key={root.id}
          folder={root}
          childrenOf={childrenOf}
          selected={selected}
          onSelect={onSelect}
          onCreate={onCreate}
          onRename={onRename}
          onDelete={onDelete}
        />
      ))}
    </nav>
  );
}

interface NodeProps {
  folder: Folder;
  childrenOf: Map<string, Folder[]>;
  selected: string;
  onSelect: (id: string) => void;
  onCreate: (parentPath: string) => void;
  onRename: (path: string, currentName: string) => void;
  onDelete: (path: string) => void;
}

function FolderNode({
  folder,
  childrenOf,
  selected,
  onSelect,
  onCreate,
  onRename,
  onDelete,
}: NodeProps) {
  const [open, setOpen] = useState(true);
  const children = childrenOf.get(folder.id) ?? [];
  const hasChildren = children.length > 0;
  const count = folderCount(folder);
  const isSelected = selected === folder.id;

  return (
    <div className="folder-node">
      <div
        className={"folder-item" + (isSelected ? " folder-selected" : "")}
        onClick={() => onSelect(folder.id)}
        title={folder.name}
      >
        {hasChildren ? (
          <span
            className="folder-caret folder-caret-toggle"
            onClick={(e) => {
              e.stopPropagation();
              setOpen((o) => !o);
            }}
          >
            {open ? "▾" : "▸"}
          </span>
        ) : (
          <span className="folder-caret" />
        )}
        <FolderIcon open={open && hasChildren} />
        <span className="folder-name">{folder.name}</span>
        {count && <span className="folder-count">{count}</span>}
        <span className="folder-actions">
          <button
            className="folder-action"
            title="New subfolder"
            onClick={(e) => {
              e.stopPropagation();
              onCreate(folder.id);
            }}
          >
            ＋
          </button>
          <button
            className="folder-action"
            title="Rename folder"
            onClick={(e) => {
              e.stopPropagation();
              onRename(folder.id, folder.name);
            }}
          >
            ✎
          </button>
          <button
            className="folder-action"
            title="Delete folder"
            onClick={(e) => {
              e.stopPropagation();
              onDelete(folder.id);
            }}
          >
            ✕
          </button>
        </span>
      </div>
      {hasChildren && open && (
        <div className="folder-children">
          {children.map((c) => (
            <FolderNode
              key={c.id}
              folder={c}
              childrenOf={childrenOf}
              selected={selected}
              onSelect={onSelect}
              onCreate={onCreate}
              onRename={onRename}
              onDelete={onDelete}
            />
          ))}
        </div>
      )}
    </div>
  );
}
