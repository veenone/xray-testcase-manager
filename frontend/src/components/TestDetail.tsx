import { useEffect, useRef, useState } from "react";
import {
  GetTest,
  GetTestPreconditions,
  GetTestRequirements,
  SetTestRequirements,
  ListRequirementsWithCoverage,
  ListAllPreconditions,
  SetTestPreconditions,
  EditPreconditionField,
  CreatePrecondition,
  GetTestContainers,
  DeallocateTests,
  GetTestTransitions,
  GetTestSteps,
  CheckJiraTestSteps,
  GetTestMeta,
  GetTestReview,
  SetTestReview,
  GetTestCustomFields,
  EditTestCustomField,
  TransitionTest,
  AddTestComment,
  EditTestField,
  EditTestStepField,
  DeleteTestStep,
  AddTestStep,
  AddCalledTestStep,
  CloneTest,
  CloneTestSteps,
  ReorderTestSteps,
  MoveTestToFolder,
  BrowserOpenURL,
  GetTestBugs,
  errMsg,
} from "../api";
import type {
  TestCase,
  Precondition,
  Requirement,
  RequirementCoverage,
  ContainerMembership,
  PendingChange,
  Transition,
  Step,
  Folder,
  CustomFieldValue,
  Review,
  JiraStepInfo,
  TestMeta,
  TestBug,
} from "../api";

import { usePrompt } from "./usePrompt";
import { useConfirm } from "./useConfirm";
import { MarkdownField } from "./MarkdownField";
import { MultiAddSelect } from "./MultiAddSelect";
import { CloneStepsModal } from "./CloneStepsModal";
import { PickTestModal } from "./PickTestModal";
import { formatDateTime } from "../dates";
import { REVIEW_ENABLED } from "../features";

const REVIEWER_KEY = "xtm.reviewer";

interface Props {
  profileId: string;
  testKey: string;
  version: number;
  pendingForTest: PendingChange[];
  folders: Folder[];
  // The active profile's Jira base URL, used to link the test key to its real
  // Jira issue (RND_P_4TFINT_05-211). Optional / empty hides the link.
  jiraUrl?: string;
  onClose: () => void;
  onEdited: () => void;
  // Called with the new temp key after cloning this test into a fresh draft.
  // Optional: omitted where the panel is a read-only slide-over, which hides
  // the Clone action.
  onCloned?: (tempKey: string) => void;
}

type EditableField = "summary" | "description" | "priority" | "labels";

export function TestDetail({
  profileId,
  testKey,
  version,
  pendingForTest,
  folders,
  jiraUrl,
  onClose,
  onEdited,
  onCloned,
}: Props) {
  const { prompt, promptUI } = usePrompt();
  const { confirm, confirmUI } = useConfirm();

  // The test key links to its real Jira issue, opened in the system browser
  // (RND_P_4TFINT_05-211). Suppressed for demo profiles and for uncommitted
  // "NEW-" drafts, which have no Jira URL yet.
  const isDemoProfile = /^(demo$|demo:|mock:)/i.test((jiraUrl ?? "").trim());
  const canLinkToJira =
    !!jiraUrl && !isDemoProfile && !testKey.startsWith("NEW-");
  function openInJira() {
    const base = (jiraUrl ?? "").trim().replace(/\/+$/, "");
    if (base) BrowserOpenURL(`${base}/browse/${testKey}`);
  }
  function openBugInJira(bugKey: string) {
    const base = (jiraUrl ?? "").trim().replace(/\/+$/, "");
    if (base) BrowserOpenURL(`${base}/browse/${bugKey}`);
  }

  // Resizeable panel width (FR-11) — drag the left edge to widen for long
  // descriptions / steps; the width persists across sessions.
  const [width, setWidth] = useState<number>(() => {
    const saved = Number(localStorage.getItem("xtm.detailWidth"));
    return saved >= 320 && saved <= 900 ? saved : 440;
  });
  useEffect(() => {
    localStorage.setItem("xtm.detailWidth", String(width));
  }, [width]);

  function startResize(e: React.MouseEvent) {
    e.preventDefault();
    const startX = e.clientX;
    const startW = width;
    // The panel is anchored to the right, so dragging left (negative delta)
    // widens it.
    const onMove = (ev: MouseEvent) =>
      setWidth(Math.min(900, Math.max(320, startW - (ev.clientX - startX))));
    const onUp = () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
    };
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
  }

  const [test, setTest] = useState<TestCase | null>(null);
  const [meta, setMeta] = useState<TestMeta | null>(null);
  const [preconditions, setPreconditions] = useState<Precondition[]>([]);
  const [allPreconditions, setAllPreconditions] = useState<Precondition[]>([]);
  const [requirements, setRequirements] = useState<Requirement[]>([]);
  const [bugs, setBugs] = useState<TestBug[]>([]);
  const [allRequirements, setAllRequirements] = useState<RequirementCoverage[]>(
    [],
  );
  const [containers, setContainers] = useState<ContainerMembership[]>([]);
  const [customFields, setCustomFields] = useState<CustomFieldValue[]>([]);
  const [review, setReview] = useState<Review | null>(null);
  const [reviewer, setReviewer] = useState(
    () => localStorage.getItem(REVIEWER_KEY) ?? "",
  );
  const [reviewNote, setReviewNote] = useState("");
  const [transitions, setTransitions] = useState<Transition[]>([]);
  const [steps, setSteps] = useState<Step[]>([]);
  const [stepsLoading, setStepsLoading] = useState(false);
  const [stepsError, setStepsError] = useState("");
  const [showCloneSteps, setShowCloneSteps] = useState(false);
  const [showCallPicker, setShowCallPicker] = useState(false);
  const [cloning, setCloning] = useState(false);
  // What Jira itself reports about this Test's steps — used to warn when the
  // panel is empty but Jira actually has steps (a load/shape problem), so the
  // user doesn't add a blank step that Xray rejects.
  const [jiraStepInfo, setJiraStepInfo] = useState<JiraStepInfo | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [saveError, setSaveError] = useState("");

  // Local editable state — initialised from the loaded Test, then driven by
  // the user. Each blur compares against `test` (the last persisted value)
  // and saves only on a real change.
  const [summary, setSummary] = useState("");
  const [description, setDescription] = useState("");
  const [priority, setPriority] = useState("");
  const [labels, setLabels] = useState("");

  // Tracks the previously-shown key so we can detect a just-committed new Test
  // (its key flips from a "NEW-N" placeholder to the real Jira key) and force a
  // fresh pull from Jira — the local cache still holds temporary step IDs (FR-1).
  const prevKeyRef = useRef<string>("");

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError("");
    setSaveError("");
    const justCommitted =
      prevKeyRef.current.startsWith("NEW-") && !testKey.startsWith("NEW-");
    prevKeyRef.current = testKey;
    const skipBugs = testKey.startsWith("NEW-");
    Promise.all([
      GetTest(profileId, testKey),
      GetTestPreconditions(profileId, testKey),
      GetTestContainers(profileId, testKey),
      ListAllPreconditions(profileId),
      GetTestReview(profileId, testKey),
      GetTestRequirements(profileId, testKey),
      ListRequirementsWithCoverage(profileId),
      skipBugs ? Promise.resolve([]) : GetTestBugs(profileId, testKey),
    ])
      .then(([t, pre, cons, allPre, rev, reqs, allReqs, testBugs]) => {
        if (cancelled) return;
        setTest(t);
        setSummary(t.summary);
        setDescription(t.description);
        setPriority(t.priority);
        setLabels((t.labels ?? []).join(" "));
        setPreconditions(pre);
        setContainers(cons ?? []);
        setAllPreconditions(allPre ?? []);
        setReview(rev);
        setRequirements(reqs ?? []);
        setAllRequirements(allReqs ?? []);
        setBugs((testBugs as TestBug[]) ?? []);
        setReviewNote(rev?.note ?? "");
        // Transitions load alongside but can fail without blocking the
        // rest of the detail panel — workflow may not be set up yet, or
        // the user may not have edit permission.
        GetTestTransitions(profileId, testKey)
          .then((ts) => {
            if (!cancelled) setTransitions(ts ?? []);
          })
          .catch((e) => {
            if (!cancelled) console.error("transitions:", errMsg(e));
          });
        // Steps load lazily: cache hit is instant, cache miss makes one
        // Xray call. Failure renders inline next to the Steps heading
        // rather than blocking the whole panel.
        setStepsLoading(true);
        setStepsError("");
        setJiraStepInfo(null);
        GetTestSteps(profileId, testKey, justCommitted)
          .then((s) => {
            if (cancelled) return;
            setSteps(s ?? []);
            // If nothing loaded, ask Jira whether this Test actually has steps,
            // so we can warn instead of letting the user add a blank one.
            if ((s ?? []).length === 0) {
              CheckJiraTestSteps(profileId, testKey)
                .then((info) => {
                  if (!cancelled) setJiraStepInfo(info);
                })
                .catch((e) => console.error("check jira steps:", errMsg(e)));
            }
          })
          .catch((e) => {
            if (!cancelled) setStepsError(errMsg(e));
          })
          .finally(() => {
            if (!cancelled) setStepsLoading(false);
          });
        // Custom fields load lazily too — definitions come from sync, values
        // on first open. Failure is non-blocking.
        GetTestCustomFields(profileId, testKey, justCommitted)
          .then((cf) => {
            if (!cancelled) setCustomFields(cf ?? []);
          })
          .catch((e) => {
            if (!cancelled) console.error("custom fields:", errMsg(e));
          });
        // Created / creator / last-updated-by load lazily from Jira (one issue
        // call incl. changelog). Non-blocking — the summary still shows the
        // synced "Updated" timestamp if this fails.
        setMeta(null);
        GetTestMeta(profileId, testKey)
          .then((m) => {
            if (!cancelled) setMeta(m);
          })
          .catch((e) => console.error("test meta:", errMsg(e)));
      })
      .catch((e) => {
        if (!cancelled) setError(errMsg(e));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [profileId, testKey, version]);

  async function saveField(field: EditableField, value: string) {
    if (!test) return;

    let backendValue: string;
    switch (field) {
      case "summary":
        backendValue = test.summary;
        break;
      case "description":
        backendValue = test.description;
        break;
      case "priority":
        backendValue = test.priority;
        break;
      case "labels":
        backendValue = (test.labels ?? []).join(" ");
        break;
    }
    if (value === backendValue) return;

    setSaveError("");
    try {
      await EditTestField(profileId, testKey, field, value);
      // Reflect the new persisted value locally so subsequent diffs work.
      const updated: TestCase = { ...test };
      switch (field) {
        case "summary":
          updated.summary = value;
          break;
        case "description":
          updated.description = value;
          break;
        case "priority":
          updated.priority = value;
          break;
        case "labels":
          updated.labels = value.split(/\s+/).filter(Boolean);
          break;
      }
      setTest(updated);
      onEdited();
    } catch (e) {
      setSaveError(`Save failed: ${errMsg(e)}`);
    }
  }

  // refreshSteps re-fetches Steps from Xray, bypassing the cache. Useful
  // after someone else edits steps directly in Jira.
  async function refreshSteps() {
    setStepsLoading(true);
    setStepsError("");
    try {
      const s = await GetTestSteps(profileId, testKey, true);
      setSteps(s ?? []);
      // Refresh resolved the load — clear the warning. If it's still empty,
      // re-check Jira so the banner reflects reality.
      if ((s ?? []).length > 0) {
        setJiraStepInfo(null);
      } else {
        try {
          setJiraStepInfo(await CheckJiraTestSteps(profileId, testKey));
        } catch (e) {
          console.error("check jira steps:", errMsg(e));
        }
      }
    } catch (e) {
      setStepsError(errMsg(e));
    } finally {
      setStepsLoading(false);
    }
  }

  // deallocateContainer removes this test from a Test Set / Plan / Execution
  // (FR-3.4–3.6) and refreshes the membership list.
  async function deallocateContainer(containerKey: string) {
    if (
      !(await confirm({
        title: "Remove from container",
        message: `Remove ${testKey} from ${containerKey}? The membership change is committed to Jira on commit.`,
        confirmLabel: "Remove",
      }))
    )
      return;
    setSaveError("");
    try {
      await DeallocateTests(profileId, containerKey, [testKey]);
      const cons = await GetTestContainers(profileId, testKey);
      setContainers(cons ?? []);
      onEdited();
    } catch (e) {
      setSaveError(`Remove failed: ${errMsg(e)}`);
    }
  }

  // moveToFolder relocates the test in the Test Repository (FR-13.3). The new
  // folder is reflected locally so the next diff works, then queued for commit.
  async function moveToFolder(folderId: string) {
    if (!test || folderId === test.folderId) return;
    setSaveError("");
    try {
      await MoveTestToFolder(profileId, testKey, folderId);
      setTest({ ...test, folderId });
      onEdited();
    } catch (e) {
      setSaveError(`Move failed: ${errMsg(e)}`);
    }
  }

  // applyRequirements replaces the test's covered-requirement set and refreshes
  // the displayed list. The link changes commit as Jira issue links.
  async function applyRequirements(nextKeys: string[]) {
    setSaveError("");
    try {
      await SetTestRequirements(profileId, testKey, nextKeys);
      const refreshed = await GetTestRequirements(profileId, testKey);
      setRequirements(refreshed ?? []);
      onEdited();
    } catch (e) {
      setSaveError(`Requirement update failed: ${errMsg(e)}`);
    }
  }

  // addRequirements links every picked requirement in a single apply: the union
  // of the already-linked keys and the newly ticked ones is sent to
  // applyRequirements once, so it coalesces into one pending change
  // (RND_P_4TFINT_05-224).
  function addRequirements(keys: string[]) {
    const linked = new Set(requirements.map((r) => r.key));
    const additions = keys.filter((k) => k && !linked.has(k));
    if (additions.length === 0) return;
    applyRequirements([...linked, ...additions]);
  }

  async function removeRequirement(key: string) {
    if (
      !(await confirm({
        title: "Unlink requirement",
        message: `Unlink ${key} from ${testKey}? The requirement isn't deleted; the coverage link is removed on commit.`,
        confirmLabel: "Unlink",
      }))
    )
      return;
    applyRequirements(requirements.map((r) => r.key).filter((k) => k !== key));
  }

  // applyPreconditions replaces the test's precondition set, then refreshes the
  // displayed list from the store (FR-13.5). Add/remove both route here.
  async function applyPreconditions(nextKeys: string[]) {
    setSaveError("");
    try {
      await SetTestPreconditions(profileId, testKey, nextKeys);
      const refreshed = await GetTestPreconditions(profileId, testKey);
      setPreconditions(refreshed ?? []);
      onEdited();
    } catch (e) {
      setSaveError(`Precondition update failed: ${errMsg(e)}`);
    }
  }

  async function removePrecondition(key: string) {
    if (
      !(await confirm({
        title: "Unlink precondition",
        message: `Unlink ${key} from ${testKey}? The precondition itself isn't deleted; the association is removed on commit.`,
        confirmLabel: "Unlink",
      }))
    )
      return;
    applyPreconditions(
      preconditions.map((p) => p.key).filter((k) => k !== key),
    );
  }

  // addPreconditions links every picked precondition in a single apply: the
  // union of the already-linked keys and the newly ticked ones is sent to
  // applyPreconditions once, so it coalesces into one pending change
  // (RND_P_4TFINT_05-224).
  function addPreconditions(keys: string[]) {
    const linked = new Set(preconditions.map((p) => p.key));
    const additions = keys.filter((k) => k && !linked.has(k));
    if (additions.length === 0) return;
    applyPreconditions([...linked, ...additions]);
  }

  // createAndAssociatePrecondition creates a brand-new Precondition (FR-13.5)
  // and links it to this test. It gets a temporary key until commit creates
  // the issue in Jira.
  async function createAndAssociatePrecondition() {
    const summary = await prompt({
      title: "New precondition",
      placeholder: "Precondition summary",
      submitLabel: "Create",
    });
    if (!summary || !summary.trim()) return;
    setSaveError("");
    try {
      const tempKey = await CreatePrecondition(profileId, summary.trim());
      const all = await ListAllPreconditions(profileId);
      setAllPreconditions(all ?? []);
      await applyPreconditions([...preconditions.map((p) => p.key), tempKey]);
    } catch (e) {
      setSaveError(`Create precondition failed: ${errMsg(e)}`);
    }
  }

  // addStep appends a new, empty step locally (FR-2.5). The backend returns
  // it with a temporary id; the user fills the fields in place (each blur
  // folds into the queued create) and it lands in Jira on commit.
  async function addStep() {
    setSaveError("");
    try {
      const s = await AddTestStep(profileId, testKey, "", "", "");
      setSteps((prev) => [...prev, s]);
      onEdited();
    } catch (e) {
      setSaveError(`Add step failed: ${errMsg(e)}`);
    }
  }

  // cloneThisTest drafts a new local test copying this one's fields and steps
  // (RND_P_4TFINT_05-206), then opens the fresh draft in the detail panel.
  async function cloneThisTest() {
    setCloning(true);
    setSaveError("");
    try {
      const tempKey = await CloneTest(profileId, testKey);
      onCloned?.(tempKey);
    } catch (e) {
      setSaveError(`Clone test failed: ${errMsg(e)}`);
      setCloning(false);
    }
  }

  // duplicateStep appends a copy of an existing step on this same test
  // (RND_P_4TFINT_05-204) — a call step clones its called test, a manual step
  // clones its action/data/expected. The copy is a new queued step the user can
  // reorder; it lands in Jira on commit like any added step.
  async function duplicateStep(step: Step) {
    setSaveError("");
    try {
      const s = step.calledTestKey
        ? await AddCalledTestStep(profileId, testKey, step.calledTestKey)
        : await AddTestStep(profileId, testKey, step.action, step.data, step.expected);
      setSteps((prev) => [...prev, s]);
      onEdited();
    } catch (e) {
      setSaveError(`Duplicate step failed: ${errMsg(e)}`);
    }
  }

  // moveStep swaps a step with its neighbour and persists the whole new order
  // (FR-2.5). The reorder is a single test-level pending change; on failure we
  // roll the local list back so the UI matches what was actually saved.
  async function moveStep(index: number, dir: "up" | "down") {
    const target = dir === "up" ? index - 1 : index + 1;
    if (target < 0 || target >= steps.length) return;
    const previous = steps;
    const next = [...steps];
    [next[index], next[target]] = [next[target], next[index]];
    setSteps(next);
    setSaveError("");
    try {
      await ReorderTestSteps(
        profileId,
        testKey,
        next.map((s) => s.xrayId),
      );
      onEdited();
    } catch (e) {
      setSteps(previous);
      setSaveError(`Reorder failed: ${errMsg(e)}`);
    }
  }

  // applyTransition records the workflow transition locally (FR-4.2). After
  // the local write, we re-query for the transitions available from the new
  // status so the next pick reflects the post-transition workflow position.
  async function applyTransition(targetStatus: string) {
    if (!test || !targetStatus) return;
    setSaveError("");
    try {
      await TransitionTest(profileId, testKey, targetStatus);
      setTest({ ...test, status: targetStatus });
      // FR-4.4: optionally capture a comment for this transition.
      const comment = await prompt({
        title: `Comment for moving to "${targetStatus}"`,
        placeholder: "Optional — leave blank to skip",
        submitLabel: "Save",
      });
      if (comment && comment.trim()) {
        await AddTestComment(profileId, testKey, comment.trim());
      }
      try {
        const ts = await GetTestTransitions(profileId, testKey);
        setTransitions(ts ?? []);
      } catch (e) {
        console.error("re-fetch transitions:", errMsg(e));
      }
      onEdited();
    } catch (e) {
      setSaveError(`Transition failed: ${errMsg(e)}`);
    }
  }

  // setVerdict records (or clears) a review verdict (test review). The reviewer
  // name is remembered across tests via localStorage.
  async function setVerdict(verdict: string) {
    const who = reviewer.trim();
    localStorage.setItem(REVIEWER_KEY, who);
    setSaveError("");
    try {
      await SetTestReview(profileId, testKey, verdict, who, reviewNote.trim());
      const rev = await GetTestReview(profileId, testKey);
      setReview(rev);
      setReviewNote(rev?.note ?? "");
      onEdited();
    } catch (e) {
      setSaveError(`Review failed: ${errMsg(e)}`);
    }
  }

  const isDirty = (field: string) =>
    pendingForTest.some((p) => p.field === field);

  return (
    <aside className="detail" style={{ width }}>
      <div
        className="detail-resizer"
        onMouseDown={startResize}
        title="Drag to resize"
      />
      <div className="detail-head">
        <div className="detail-head-id">
          {canLinkToJira ? (
            <button
              className="mono detail-key detail-key-link"
              onClick={openInJira}
              title="Open this test in Jira (browser)"
            >
              {testKey}
              <span className="detail-key-ext" aria-hidden="true">
                ↗
              </span>
            </button>
          ) : (
            <span className="mono detail-key">{testKey}</span>
          )}
          {test && (
            <span className="status-pill detail-head-status">
              {test.status || "—"}
            </span>
          )}
        </div>
        <div className="detail-head-actions">
          {onCloned && !testKey.startsWith("NEW-") && (
            <button
              className="btn btn-ghost detail-clone"
              onClick={cloneThisTest}
              disabled={cloning}
              title="Create a new test that copies this test's fields and steps"
            >
              {cloning ? "Cloning…" : "⧉ Clone"}
            </button>
          )}
          <button
            className="btn btn-ghost detail-close"
            onClick={onClose}
            title="Close"
          >
            ✕
          </button>
        </div>
      </div>

      {testKey.startsWith("NEW-") && (
        <div className="detail-uncommitted-banner">
          Uncommitted — this test is local only and will be created in Jira when
          you commit.
        </div>
      )}

      {loading && <div className="muted detail-body">Loading…</div>}
      {error && <div className="error-text detail-body">{error}</div>}

      {test && !loading && (
        <div className="detail-body">
          {saveError && (
            <div className="error-text detail-save-error">{saveError}</div>
          )}

          <div className="field-label">
            Summary {isDirty("summary") && <DirtyDot />}
          </div>
          <input
            className="detail-input"
            value={summary}
            onChange={(e) => setSummary(e.target.value)}
            onBlur={() => saveField("summary", summary)}
            spellCheck
          />

          <dl className="detail-fields">
            <dt>
              Status {isDirty("status") && <DirtyDot />}
            </dt>
            <dd>
              <div className="status-row">
                <span className="status-pill">{test.status || "—"}</span>
                {transitions.length > 0 && (
                  <select
                    className="transition-select"
                    value=""
                    onChange={(e) => {
                      if (e.target.value) applyTransition(e.target.value);
                    }}
                  >
                    <option value="">Move to…</option>
                    {transitions.map((t) => (
                      <option key={t.id} value={t.to}>
                        {t.name} → {t.to}
                      </option>
                    ))}
                  </select>
                )}
              </div>
            </dd>

            <dt>
              Priority {isDirty("priority") && <DirtyDot />}
            </dt>
            <dd>
              <input
                className="detail-input detail-input-inline"
                value={priority}
                onChange={(e) => setPriority(e.target.value)}
                onBlur={() => saveField("priority", priority)}
              />
            </dd>

            <dt>
              Labels {isDirty("labels") && <DirtyDot />}
            </dt>
            <dd>
              <input
                className="detail-input detail-input-inline"
                value={labels}
                onChange={(e) => setLabels(e.target.value)}
                onBlur={() => saveField("labels", labels)}
                placeholder="space-separated"
              />
            </dd>

            {folders.length > 0 && (
              <>
                <dt>
                  Folder {isDirty("folder") && <DirtyDot />}
                </dt>
                <dd>
                  <select
                    className="detail-input detail-input-inline"
                    value={test.folderId}
                    onChange={(e) => moveToFolder(e.target.value)}
                  >
                    <option value="">(repository root)</option>
                    {folders.map((f) => (
                      <option key={f.id} value={f.id}>
                        {f.id}
                      </option>
                    ))}
                  </select>
                </dd>
              </>
            )}

            <dt>Created</dt>
            <dd>{formatDateTime(meta?.created)}</dd>

            <dt>Creator</dt>
            <dd>{meta?.creator || "—"}</dd>

            <dt>Updated</dt>
            <dd>{formatDateTime(meta?.updated || test.updated)}</dd>

            <dt>Updated by</dt>
            <dd>{meta?.updatedBy || "—"}</dd>
          </dl>

          {REVIEW_ENABLED && (
            <>
          <h4>
            Review {isDirty("review") && <DirtyDot />}
          </h4>
          <div className="review-box">
            <div className="review-state">
              <span
                className={`review-verdict review-${review?.verdict || "none"}`}
              >
                {verdictLabel(review?.verdict)}
              </span>
              {review?.verdict && (review.reviewer || review.reviewedAt) && (
                <span className="muted review-meta">
                  {review.reviewer ? `by ${review.reviewer}` : ""}
                  {review.reviewedAt
                    ? ` · ${new Date(review.reviewedAt).toLocaleDateString()}`
                    : ""}
                </span>
              )}
            </div>
            <input
              className="detail-input detail-input-inline review-reviewer"
              value={reviewer}
              onChange={(e) => setReviewer(e.target.value)}
              placeholder="Reviewer name"
            />
            <input
              className="detail-input detail-input-inline review-note"
              value={reviewNote}
              onChange={(e) => setReviewNote(e.target.value)}
              placeholder="Review note (optional)"
            />
            <div className="review-actions">
              <button
                className="btn review-approve"
                onClick={() => setVerdict("approved")}
              >
                Approve
              </button>
              <button
                className="btn review-reject"
                onClick={() => setVerdict("rejected")}
              >
                Reject
              </button>
              <button className="btn" onClick={() => setVerdict("pending")}>
                Pending
              </button>
              {review?.verdict && (
                <button
                  className="btn btn-ghost review-clear"
                  onClick={() => setVerdict("")}
                  title="Clear the review"
                >
                  Clear
                </button>
              )}
            </div>
          </div>
            </>
          )}

          {customFields.length > 0 && (
            <>
              <h4>Custom Fields</h4>
              <dl className="detail-fields">
                {customFields.map((f) => (
                  <CustomFieldRow
                    key={f.fieldId}
                    profileId={profileId}
                    testKey={testKey}
                    field={f}
                    pendingForTest={pendingForTest}
                    onLocalChange={(value) =>
                      setCustomFields((prev) =>
                        prev.map((p) =>
                          p.fieldId === f.fieldId ? { ...p, value } : p,
                        ),
                      )
                    }
                    onEdited={onEdited}
                  />
                ))}
              </dl>
            </>
          )}

          <h4>
            Preconditions {isDirty("preconditions") && <DirtyDot />}
          </h4>
          {preconditions.length === 0 ? (
            <p className="muted">None linked</p>
          ) : (
            <ul className="pre-list">
              {preconditions.map((p) => (
                <PreconditionRow
                  key={p.key}
                  profileId={profileId}
                  precondition={p}
                  onRemove={removePrecondition}
                  onEdited={onEdited}
                />
              ))}
            </ul>
          )}
          <div className="pre-add pre-add-row">
            <MultiAddSelect
              placeholder="+ Add precondition…"
              onAdd={addPreconditions}
              options={allPreconditions
                .filter((p) => !preconditions.some((lp) => lp.key === p.key))
                .map((p) => ({
                  value: p.key,
                  label: `${p.key} — ${p.summary}`,
                }))}
            />
            <button
              type="button"
              className="btn btn-ghost pre-add-new"
              onClick={createAndAssociatePrecondition}
              title="Create a brand-new precondition and link it"
            >
              ＋ New
            </button>
          </div>

          {(() => {
            const sets = containers.filter((c) => c.kind === "testset");
            const plans = containers.filter((c) => c.kind === "testplan");
            const execs = containers.filter((c) => c.kind === "testexec");
            if (containers.length === 0) {
              return (
                <>
                  <h4>Memberships</h4>
                  <p className="muted">Not in any set, plan or execution.</p>
                </>
              );
            }
            return (
              <>
                {sets.length > 0 && (
                  <ContainerSection
                    title="Test Sets"
                    items={sets}
                    onRemove={deallocateContainer}
                  />
                )}
                {plans.length > 0 && (
                  <ContainerSection
                    title="Test Plans"
                    items={plans}
                    onRemove={deallocateContainer}
                  />
                )}
                {execs.length > 0 && (
                  <ContainerSection
                    title="Test Executions"
                    items={execs}
                    showRunStatus
                    onRemove={deallocateContainer}
                  />
                )}
              </>
            );
          })()}

          <h4>
            Requirements {isDirty("requirements") && <DirtyDot />}
          </h4>
          {requirements.length === 0 ? (
            <p className="muted">Not linked to any requirement.</p>
          ) : (
            <ul className="pre-list req-link-list">
              {requirements.map((rq) => (
                <li key={rq.key}>
                  <span className="mono">{rq.key}</span>
                  <span className="muted req-link-project">{rq.projectKey}</span>
                  <span className="req-link-summary">{rq.summary}</span>
                  {rq.status && (
                    <span className="status-pill req-link-status">
                      {rq.status}
                    </span>
                  )}
                  <button
                    className="btn btn-ghost pre-remove"
                    onClick={() => removeRequirement(rq.key)}
                    title="Unlink this requirement"
                  >
                    ✕
                  </button>
                </li>
              ))}
            </ul>
          )}
          <MultiAddSelect
            className="pre-add"
            placeholder="+ Link requirement…"
            onAdd={addRequirements}
            options={allRequirements
              .filter((r) => !requirements.some((lr) => lr.key === r.key))
              .map((r) => ({
                value: r.key,
                label: `${r.key} — ${r.summary}`,
              }))}
          />

          <h4>Bugs</h4>
          {bugs.length === 0 ? (
            <p className="muted">No linked bugs.</p>
          ) : (
            <ul className="pre-list bug-link-list">
              {bugs.map((b) => (
                <li key={b.key}>
                  {canLinkToJira && !b.key.startsWith("NEW-") ? (
                    <button
                      className="mono bug-link-key"
                      onClick={() => openBugInJira(b.key)}
                      title={`Open ${b.key} in Jira (browser)`}
                    >
                      {b.key}
                    </button>
                  ) : (
                    <span className="mono">{b.key}</span>
                  )}
                  <span className="muted req-link-project">{b.projectKey}</span>
                  <span className="req-link-summary">{b.summary}</span>
                  {b.status && (
                    <span className="status-pill req-link-status">{b.status}</span>
                  )}
                </li>
              ))}
            </ul>
          )}

          <h4>
            Description {isDirty("description") && <DirtyDot />}
          </h4>
          <MarkdownField
            className="detail-desc-edit"
            value={description}
            onChange={setDescription}
            onCommit={() => saveField("description", description)}
            rows={8}
            placeholder="No description. Click to add — markdown supported."
          />

          <h4 className="steps-head">
            Steps
            {pendingForTest.some((p) => p.entityType === "test_step_order") && (
              <span className="steps-reordered" title="Step order changed">
                reordered
              </span>
            )}
            <button
              className="link-btn steps-refresh"
              onClick={refreshSteps}
              disabled={stepsLoading}
              title="Re-fetch steps from Jira"
            >
              {stepsLoading ? "Loading…" : "Refresh"}
            </button>
            <button
              className="link-btn steps-clone"
              onClick={() => setShowCloneSteps(true)}
              disabled={stepsLoading}
              title="Append a copy of another test's steps to this one"
            >
              Clone from…
            </button>
          </h4>
          {stepsError && <div className="error-text">{stepsError}</div>}
          {!stepsError &&
            !stepsLoading &&
            steps.length === 0 &&
            (jiraStepInfo && jiraStepInfo.count > 0 ? (
              <div className="steps-warning">
                ⚠ Jira reports {jiraStepInfo.count} step
                {jiraStepInfo.count === 1 ? "" : "s"} for this test that didn't
                load here. Don't add new steps yet — that would create
                duplicates.{" "}
                <button className="link-btn" onClick={refreshSteps}>
                  Load from Jira
                </button>
              </div>
            ) : (
              <p className="muted">No steps defined for this test.</p>
            ))}
          {!stepsError &&
            !stepsLoading &&
            steps.length > 0 &&
            steps.every((s) => !s.action && !s.data && !s.expected) && (
              <div className="steps-warning">
                ⚠ These steps loaded without content — this Xray instance may use
                a step format the tool doesn't recognise yet. Avoid editing them
                to prevent overwriting the real steps in Jira.
              </div>
            )}
          {steps.length > 0 && (
            <ol className="steps-list">
              {steps.map((s, i) => (
                <StepRow
                  key={s.xrayId}
                  profileId={profileId}
                  testKey={testKey}
                  step={s}
                  pendingForTest={pendingForTest}
                  isFirst={i === 0}
                  isLast={i === steps.length - 1}
                  confirm={confirm}
                  onMove={(dir) => moveStep(i, dir)}
                  onDuplicate={() => duplicateStep(s)}
                  onLocalChange={(field, value) => {
                    setSteps((prev) =>
                      prev.map((p) =>
                        p.xrayId === s.xrayId ? { ...p, [field]: value } : p,
                      ),
                    );
                  }}
                  onLocalDelete={(xrayId) => {
                    setSteps((prev) => prev.filter((p) => p.xrayId !== xrayId));
                  }}
                  onEdited={onEdited}
                />
              ))}
            </ol>
          )}
          {!stepsError && !stepsLoading && (
            <div className="steps-add-row">
              <button className="link-btn steps-add" onClick={addStep}>
                + Add step
              </button>
              <button
                className="link-btn steps-add"
                onClick={() => setShowCallPicker(true)}
                title="Add a step that calls another test"
              >
                + Call test
              </button>
            </div>
          )}

          <p className="muted detail-note">
            Edits are saved locally and queued in <b>Pending</b> until you
            commit them to Jira. Reordering steps lands in a later update.
          </p>
        </div>
      )}
      {showCallPicker && (
        <PickTestModal
          profileId={profileId}
          heading={`Call a test from ${testKey}`}
          excludeKey={testKey}
          onCancel={() => setShowCallPicker(false)}
          onPick={async (calledKey) => {
            const s = await AddCalledTestStep(profileId, testKey, calledKey);
            setSteps((prev) => [...prev, s]);
            setShowCallPicker(false);
            onEdited();
          }}
        />
      )}

      {showCloneSteps && (
        <CloneStepsModal
          profileId={profileId}
          targetLabel={testKey}
          excludeKey={testKey}
          onCancel={() => setShowCloneSteps(false)}
          onConfirm={async (sourceKey, stepIds) => {
            const newSteps = await CloneTestSteps(
              profileId,
              testKey,
              sourceKey,
              stepIds,
            );
            setSteps(newSteps ?? []);
            setShowCloneSteps(false);
            onEdited();
          }}
        />
      )}
      {promptUI}
      {confirmUI}
    </aside>
  );
}

type StepField = "action" | "data" | "expected";

interface StepRowProps {
  profileId: string;
  testKey: string;
  step: Step;
  pendingForTest: PendingChange[];
  isFirst: boolean;
  isLast: boolean;
  confirm: (opts: {
    title: string;
    message?: string;
    confirmLabel?: string;
  }) => Promise<boolean>;
  onMove: (dir: "up" | "down") => void;
  onDuplicate: () => void;
  onLocalChange: (field: StepField, value: string) => void;
  onLocalDelete: (xrayId: string) => void;
  onEdited: () => void;
}

// StepRow renders one editable Test Step (FR-2.5). Each field saves on
// blur, mirroring the same pattern used by the Test-level field editors.
// Dirty markers come from the pendingForTest prop — we filter to rows
// belonging to this step's entity_key.
function StepRow({
  profileId,
  testKey,
  step,
  pendingForTest,
  isFirst,
  isLast,
  confirm,
  onMove,
  onDuplicate,
  onLocalChange,
  onLocalDelete,
  onEdited,
}: StepRowProps) {
  const [action, setAction] = useState(step.action);
  const [data, setData] = useState(step.data);
  const [expected, setExpected] = useState(step.expected);
  const [saveError, setSaveError] = useState("");

  const entityKey = `${testKey}:${step.xrayId}`;
  // A step that's only queued for creation (not yet in Jira) shows a "new"
  // badge and, on delete, just cancels the queued add rather than scheduling
  // a remote removal.
  const isNew = pendingForTest.some(
    (p) => p.entityType === "test_step_add" && p.entityKey === entityKey,
  );

  async function deleteStep() {
    const ok = await confirm({
      title: isNew ? "Discard step" : "Delete step",
      message: isNew
        ? "Discard this new step? It hasn't been sent to Jira yet."
        : "Delete this step? It will be removed from Jira on commit.",
      confirmLabel: isNew ? "Discard" : "Delete",
    });
    if (!ok) return;
    setSaveError("");
    try {
      await DeleteTestStep(profileId, testKey, step.xrayId);
      onLocalDelete(step.xrayId);
      onEdited();
    } catch (e) {
      setSaveError(errMsg(e));
    }
  }

  const isDirty = (field: StepField) =>
    pendingForTest.some(
      (p) =>
        p.entityType === "test_step" &&
        p.entityKey === entityKey &&
        p.field === field,
    );

  async function save(field: StepField, value: string) {
    let backendValue: string;
    switch (field) {
      case "action":
        backendValue = step.action;
        break;
      case "data":
        backendValue = step.data;
        break;
      case "expected":
        backendValue = step.expected;
        break;
    }
    if (value === backendValue) return;
    setSaveError("");
    try {
      await EditTestStepField(profileId, testKey, step.xrayId, field, value);
      onLocalChange(field, value);
      onEdited();
    } catch (e) {
      setSaveError(errMsg(e));
    }
  }

  // A "call test" step invokes another test instead of holding manual content;
  // it has no editable action/data/expected, just the called test + controls.
  if (step.calledTestKey) {
    return (
      <li>
        <div className="step-head step-call-head">
          <span className="step-call-label">
            ⮡ Calls <span className="mono">{step.calledTestKey}</span>
            {isNew && <span className="step-new-badge">new</span>}
          </span>
          <div className="step-move">
            <button
              className="btn btn-ghost step-move-btn"
              onClick={() => onMove("up")}
              disabled={isFirst}
              title="Move step up"
            >
              ▲
            </button>
            <button
              className="btn btn-ghost step-move-btn"
              onClick={() => onMove("down")}
              disabled={isLast}
              title="Move step down"
            >
              ▼
            </button>
          </div>
          <button
            className="btn btn-ghost step-duplicate"
            onClick={onDuplicate}
            title="Duplicate this call"
          >
            ⧉
          </button>
          <button
            className="btn btn-ghost step-delete"
            onClick={deleteStep}
            title={isNew ? "Discard this call" : "Delete this call"}
          >
            ✕
          </button>
        </div>
        {saveError && <div className="error-text step-save-error">{saveError}</div>}
      </li>
    );
  }

  return (
    <li>
      <div className="step-head">
        <MarkdownField
          className="step-edit step-edit-action"
          value={action}
          onChange={setAction}
          onCommit={() => save("action", action)}
          rows={2}
          placeholder="(action)"
        />
        {isNew && <span className="step-new-badge">new</span>}
        <div className="step-move">
          <button
            className="btn btn-ghost step-move-btn"
            onClick={() => onMove("up")}
            disabled={isFirst}
            title="Move step up"
          >
            ▲
          </button>
          <button
            className="btn btn-ghost step-move-btn"
            onClick={() => onMove("down")}
            disabled={isLast}
            title="Move step down"
          >
            ▼
          </button>
        </div>
        <button
          className="btn btn-ghost step-duplicate"
          onClick={onDuplicate}
          title="Duplicate this step"
        >
          ⧉
        </button>
        <button
          className="btn btn-ghost step-delete"
          onClick={deleteStep}
          title={isNew ? "Discard this new step" : "Delete this step"}
        >
          ✕
        </button>
      </div>
      {isDirty("action") && <DirtyDot />}
      <div className="step-row">
        <span className="step-label">
          Data {isDirty("data") && <DirtyDot />}
        </span>
        <MarkdownField
          className="step-edit"
          value={data}
          onChange={setData}
          onCommit={() => save("data", data)}
          multiline={false}
          placeholder="(optional)"
        />
      </div>
      <div className="step-row">
        <span className="step-label">
          Expected {isDirty("expected") && <DirtyDot />}
        </span>
        <MarkdownField
          className="step-edit"
          value={expected}
          onChange={setExpected}
          onCommit={() => save("expected", expected)}
          rows={2}
          placeholder="(expected result)"
        />
      </div>
      {saveError && <div className="error-text step-save-error">{saveError}</div>}
    </li>
  );
}

// CustomFieldRow renders one Jira custom field as a label + editable value
// (FR-2.6). All types edit as text — typed editors (select / date) need live
// editmeta. Saves on blur; a dirty marker comes from the pending journal.
function CustomFieldRow({
  profileId,
  testKey,
  field,
  pendingForTest,
  onLocalChange,
  onEdited,
}: {
  profileId: string;
  testKey: string;
  field: CustomFieldValue;
  pendingForTest: PendingChange[];
  onLocalChange: (value: string) => void;
  onEdited: () => void;
}) {
  const [value, setValue] = useState(field.value);
  const [saveError, setSaveError] = useState("");

  const entityKey = `${testKey}:${field.fieldId}`;
  const isDirty = pendingForTest.some(
    (p) => p.entityType === "custom_field" && p.entityKey === entityKey,
  );

  async function save() {
    if (value === field.value) return;
    setSaveError("");
    try {
      await EditTestCustomField(profileId, testKey, field.fieldId, value);
      onLocalChange(value);
      onEdited();
    } catch (e) {
      setValue(field.value);
      setSaveError(errMsg(e));
    }
  }

  return (
    <>
      <dt>
        {field.name} {isDirty && <DirtyDot />}
      </dt>
      <dd>
        <input
          className="detail-input detail-input-inline"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onBlur={save}
        />
        {saveError && <div className="error-text">{saveError}</div>}
      </dd>
    </>
  );
}

// PreconditionRow renders one linked Precondition with an inline-editable
// summary (FR-13.5 update) and a remove button (FR-13.6). Editing the summary
// updates the Precondition issue itself, so it changes everywhere that
// precondition is linked.
function PreconditionRow({
  profileId,
  precondition,
  onRemove,
  onEdited,
}: {
  profileId: string;
  precondition: Precondition;
  onRemove: (key: string) => void;
  onEdited: () => void;
}) {
  const [summary, setSummary] = useState(precondition.summary);
  const [saveError, setSaveError] = useState("");

  async function save() {
    if (summary === precondition.summary) return;
    setSaveError("");
    try {
      await EditPreconditionField(profileId, precondition.key, "summary", summary);
      onEdited();
    } catch (e) {
      setSummary(precondition.summary);
      setSaveError(errMsg(e));
    }
  }

  return (
    <li>
      <span className="mono">{precondition.key}</span>
      <input
        className="pre-summary-edit"
        value={summary}
        onChange={(e) => setSummary(e.target.value)}
        onBlur={save}
      />
      <button
        className="btn btn-ghost pre-remove"
        onClick={() => onRemove(precondition.key)}
        title="Remove this precondition"
      >
        ✕
      </button>
      {saveError && <span className="error-text">{saveError}</span>}
    </li>
  );
}

// ContainerSection lists the Test Sets / Plans / Executions a Test belongs to
// (FR-1.3). Execution memberships also show the Test Run status badge.
function ContainerSection({
  title,
  items,
  showRunStatus,
  onRemove,
}: {
  title: string;
  items: ContainerMembership[];
  showRunStatus?: boolean;
  onRemove: (containerKey: string) => void;
}) {
  return (
    <>
      <h4>{title}</h4>
      {items.length === 0 ? (
        <p className="muted">None linked</p>
      ) : (
        <ul className="pre-list">
          {items.map((c) => (
            <li key={c.key}>
              <span className="mono">{c.key}</span> — {c.summary}
              {showRunStatus && c.runStatus && (
                <RunStatusBadge status={c.runStatus} />
              )}
              <button
                className="btn btn-ghost pre-remove"
                onClick={() => onRemove(c.key)}
                title="Remove this test from the container"
              >
                ✕
              </button>
            </li>
          ))}
        </ul>
      )}
    </>
  );
}

// RunStatusBadge renders a Test Run result with a status-coloured pill. The
// class is derived from the lowercased status so the CSS can theme PASS / FAIL
// / TODO distinctly while unknown statuses fall back to a neutral style.
function RunStatusBadge({ status }: { status: string }) {
  return (
    <span className={`run-badge run-${status.toLowerCase()}`}>{status}</span>
  );
}

function verdictLabel(verdict?: string): string {
  switch (verdict) {
    case "approved":
      return "Approved";
    case "rejected":
      return "Rejected";
    case "pending":
      return "Pending review";
    default:
      return "Not reviewed";
  }
}

function DirtyDot() {
  return (
    <span className="dirty-dot" title="Pending edit">
      ●
    </span>
  );
}
