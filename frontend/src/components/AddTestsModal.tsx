import { useEffect, useState } from "react";
import { useProfile } from "../contexts/ProfileContext";
import {
  ListTests,
  ListMatchingKeys,
  AllocateTests,
  ListFolders,
  errMsg,
} from "../api";
import type { TestCase, Folder, TestQuery } from "../api";
import { FolderTree } from "./FolderTree";
import { Modal } from "./Modal";

interface Props {
  containerKey: string;
  existingKeys: string[];
  onDone: () => void;
  onCancel: () => void;
  // When provided, picked test keys are passed to onAdd instead of being
  // allocated to a container — lets the Preconditions view reuse this picker to
  // link tests to a Precondition. targetLabel customises the heading.
  onAdd?: (keys: string[]) => Promise<void>;
  targetLabel?: string;
}

// AddTestsModal searches the test cache and adds the chosen tests to a
// container (Test Set / Plan / Execution) — the container-side counterpart to
// allocating from the browse grid. Already-member tests are shown disabled.
export function AddTestsModal({
  containerKey,
  existingKeys,
  onDone,
  onCancel,
  onAdd,
  targetLabel,
}: Props) {
  const { activeId: profileId } = useProfile();
  const [search, setSearch] = useState("");
  const [results, setResults] = useState<TestCase[]>([]);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [folders, setFolders] = useState<Folder[]>([]);
  const [folderId, setFolderId] = useState("");
  const [page, setPage] = useState(0);
  const [total, setTotal] = useState(0);
  const [selectingAll, setSelectingAll] = useState(false);
  const [selectAllError, setSelectAllError] = useState("");

  const PAGE_SIZE = 50;
  const existing = new Set(existingKeys);

  // Reset to the first page whenever the query (search or folder) changes.
  useEffect(() => {
    setPage(0);
  }, [search, folderId]);

  // Load the Test Repository folder tree once, so tests can be narrowed by
  // folder the same way the Browse view does.
  useEffect(() => {
    let cancelled = false;
    ListFolders(profileId)
      .then((fs) => {
        if (!cancelled) setFolders(fs ?? []);
      })
      .catch(() => {
        /* folders are optional navigation; ignore load failures */
      });
    return () => {
      cancelled = true;
    };
  }, [profileId]);

  // buildQuery is the single source of truth for what the modal is showing.
  // The list and "select all matching" MUST use the same filters, or the
  // banner would select a different set than the one on screen.
  function buildQuery(limit: number, offset: number): TestQuery {
    return {
      search,
      status: "",
      folderId,
      containerKey: "",
      component: "",
      execType: "",
      review: "",
      sortBy: "key",
      desc: false,
      limit,
      offset,
    };
  }

  useEffect(() => {
    let cancelled = false;
    const handle = setTimeout(() => {
      setLoading(true);
      ListTests(profileId, buildQuery(PAGE_SIZE, page * PAGE_SIZE))
        .then((p) => {
          if (cancelled) return;
          setResults(p.tests ?? []);
          setTotal(p.total ?? 0);
        })
        .catch((e) => {
          if (!cancelled) setError(errMsg(e));
        })
        .finally(() => {
          if (!cancelled) setLoading(false);
        });
    }, 250);
    return () => {
      cancelled = true;
      clearTimeout(handle);
    };
  }, [profileId, search, folderId, page]);

  function toggle(key: string) {
    setPicked((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }

  // Rows already in the target container are disabled, so they are not
  // selectable and must not count toward "the whole page is selected".
  const selectableKeys = results
    .filter((t) => !existing.has(t.key))
    .map((t) => t.key);
  const allPageSelected =
    selectableKeys.length > 0 && selectableKeys.every((k) => picked.has(k));
  const somePageSelected = selectableKeys.some((k) => picked.has(k));

  function togglePage() {
    setPicked((prev) => {
      const next = new Set(prev);
      if (allPageSelected) selectableKeys.forEach((k) => next.delete(k));
      else selectableKeys.forEach((k) => next.add(k));
      return next;
    });
  }

  async function selectAllMatching() {
    if (selectingAll) return;
    setSelectingAll(true);
    setSelectAllError("");
    try {
      // limit 0 means "every match, unpaged", the same contract the Browse
      // grid's select-all uses.
      const keys = await ListMatchingKeys(profileId, buildQuery(0, 0));
      setPicked(new Set((keys ?? []).filter((k) => !existing.has(k))));
    } catch (e) {
      setSelectAllError(errMsg(e));
    } finally {
      setSelectingAll(false);
    }
  }

  async function add() {
    if (picked.size === 0) return;
    setBusy(true);
    setError("");
    try {
      if (onAdd) await onAdd([...picked]);
      else await AllocateTests(profileId, containerKey, [...picked]);
      onDone();
    } catch (e) {
      setError(errMsg(e));
      setBusy(false);
    }
  }

  return (
    <Modal onClose={onCancel} className="modal pending-modal" labelledBy="add-tests-title">
        <div className="pending-head">
          <h2 id="add-tests-title">Add tests to {targetLabel ?? containerKey}</h2>
          <button className="btn btn-ghost" onClick={onCancel} title="Close">
            ✕
          </button>
        </div>

        <div className="add-tests-body">
          {folders.length > 0 && (
            <div className="add-tests-folders">
              <FolderTree
                folders={folders}
                selected={folderId}
                onSelect={setFolderId}
                readOnly
              />
            </div>
          )}
          <div className="add-tests-main">
            <input
              className="detail-input"
              autoFocus
              placeholder="Search key, summary, description…"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
            {loading ? (
              <p className="muted">Searching…</p>
            ) : (
              <>
                <div className="add-test-selectall">
                  <label>
                    <input
                      type="checkbox"
                      checked={allPageSelected}
                      ref={(el) => {
                        // Indeterminate is a DOM property, not an attribute,
                        // so React cannot set it declaratively.
                        if (el)
                          el.indeterminate = !allPageSelected && somePageSelected;
                      }}
                      disabled={selectableKeys.length === 0}
                      onChange={togglePage}
                    />
                    {allPageSelected
                      ? `Deselect all ${selectableKeys.length} on this page`
                      : `Select all ${selectableKeys.length} on this page`}
                  </label>
                  <span className="muted">{picked.size} selected</span>
                </div>
                <ul className="add-test-list">
                  {results.map((t) => {
                    const already = existing.has(t.key);
                    return (
                      <li
                        key={t.key}
                        className={already ? "add-test-already" : ""}
                      >
                        <label>
                          <input
                            type="checkbox"
                            disabled={already}
                            checked={already || picked.has(t.key)}
                            onChange={() => toggle(t.key)}
                          />
                          <span className="mono">{t.key}</span> {t.summary}
                          {already && (
                            <span className="muted"> · already a member</span>
                          )}
                        </label>
                      </li>
                    );
                  })}
                  {results.length === 0 && (
                    <li className="muted">No tests match.</li>
                  )}
                </ul>
                {allPageSelected && total > PAGE_SIZE && (
                  <div className="select-all-banner">
                    You've selected all {selectableKeys.length} tests on this
                    page.{" "}
                    <button
                      className="link-btn"
                      onClick={selectAllMatching}
                      disabled={selectingAll}
                    >
                      {selectingAll
                        ? "Selecting…"
                        : `Select all ${total.toLocaleString()} matching this filter`}
                    </button>
                    {selectAllError && (
                      <span className="error-text"> {selectAllError}</span>
                    )}
                  </div>
                )}
              </>
            )}
            {total > PAGE_SIZE && (
              <div className="add-test-pager">
                <button
                  className="btn"
                  disabled={page === 0 || loading}
                  onClick={() => setPage((p) => Math.max(0, p - 1))}
                >
                  ‹ Prev
                </button>
                <span className="muted">
                  {(page * PAGE_SIZE + 1).toLocaleString()}–
                  {Math.min((page + 1) * PAGE_SIZE, total).toLocaleString()} of{" "}
                  {total.toLocaleString()}
                </span>
                <button
                  className="btn"
                  disabled={(page + 1) * PAGE_SIZE >= total || loading}
                  onClick={() => setPage((p) => p + 1)}
                >
                  Next ›
                </button>
              </div>
            )}
            {error && <div className="error-text">{error}</div>}
          </div>
        </div>

        <div className="pending-actions">
          <button className="btn" onClick={onCancel} disabled={busy}>
            Cancel
          </button>
          <button
            className="btn btn-primary"
            onClick={add}
            disabled={busy || picked.size === 0}
          >
            {busy ? "Adding…" : `Add ${picked.size} test${picked.size === 1 ? "" : "s"}`}
          </button>
        </div>
    </Modal>
  );
}
