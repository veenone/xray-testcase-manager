import { useEffect, useRef, useState } from "react";
import {
  GetTestPreconditions,
  GetTestRequirements,
  SetTestRequirements,
  ListRequirementsWithCoverage,
  ListAllPreconditions,
  SetTestPreconditions,
  EditPreconditionField,
  CreatePrecondition,
  CacheExternalPreconditions,
  GetProfileCrossProjectSources,
  GetTestContainers,
  DeallocateTests,
  GetTestTransitions,
  ListStatuses,
  GetTestSteps,
  CheckJiraTestSteps,
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
  ChangeTestType,
  errMsg,
  isDemoUrl,
} from "../api";
import type {
  TestCase,
  TypeConversion,
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
  TestRunEntry,
} from "../api";

import { usePrompt } from "./usePrompt";
import { useConfirm } from "./useConfirm";
import { MarkdownField } from "./MarkdownField";
import { MultiAddSelect } from "./MultiAddSelect";
import { CloneStepsModal } from "./CloneStepsModal";
import { PickTestModal } from "./PickTestModal";
import { PickPreconditionModal } from "./PickPreconditionModal";
import { Modal } from "./Modal";
import { formatDateTime } from "../dates";
import { REVIEW_ENABLED, useCapabilities } from "../features";
import { useQueryClient } from "@tanstack/react-query";
import { useTest, useTestMeta, useTestRunHistory } from "../queries/testDetail";
import { keys } from "../queries/keys";

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
  // When true, all editing controls are hidden and every mutating handler
  // short-circuits. Read display (fields, steps, preconditions, requirements,
  // custom fields, bugs, run history) remains fully functional.
  readOnly?: boolean;
}

type EditableField =
  | "summary"
  | "description"
  | "priority"
  | "labels"
  | "cucumber_scenario"
  | "cucumber_type"
  | "generic_definition";

// EXEC_TYPE_OPTIONS is the fixed Xray Test Type (execution type) vocabulary.
const EXEC_TYPE_OPTIONS = ["Manual", "Automated", "Generic", "Cucumber"];

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
  readOnly,
}: Props) {
  const { prompt, promptUI } = usePrompt();
  const { confirm, confirmUI } = useConfirm();
  // Gates the Xray-shaped sections below (preconditions, requirements, exec
  // type, folder) to what the active profile's backend actually supports
  // (P6.2a). Status/step editing is left un-gated here — that's P6.2b.
  const caps = useCapabilities(profileId);

  // The test key links to its real Jira issue, opened in the system browser
  // (RND_P_4TFINT_05-211). Suppressed for demo profiles and for uncommitted
  // "NEW-" drafts, which have no Jira URL yet.
  const isDemoProfile = isDemoUrl(jiraUrl);
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

  // Load the profile's cross-project source projects once, to gate the
  // "Other project" link buttons (RND_P_4TFINT_05-322).
  useEffect(() => {
    let cancelled = false;
    GetProfileCrossProjectSources(profileId)
      .then((s) => {
        if (!cancelled) setCrossProjectSources(s ?? "");
      })
      .catch(() => {
        if (!cancelled) setCrossProjectSources("");
      });
    return () => {
      cancelled = true;
    };
  }, [profileId]);

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

  const [preconditions, setPreconditions] = useState<Precondition[]>([]);
  const [allPreconditions, setAllPreconditions] = useState<Precondition[]>([]);
  const [precondLoading, setPrecondLoading] = useState(false);
  const [precondError, setPrecondError] = useState("");
  // Cross-project precondition picker open state (RND_P_4TFINT_05-322).
  const [showCrossPrecond, setShowCrossPrecond] = useState(false);
  const [requirements, setRequirements] = useState<Requirement[]>([]);
  const [bugs, setBugs] = useState<TestBug[]>([]);
  const [allRequirements, setAllRequirements] = useState<RequirementCoverage[]>(
    [],
  );
  const [swapKind, setSwapKind] = useState<"precondition" | "requirement" | null>(
    null,
  );
  const [containers, setContainers] = useState<ContainerMembership[]>([]);
  const [customFields, setCustomFields] = useState<CustomFieldValue[]>([]);
  const [review, setReview] = useState<Review | null>(null);
  const [reviewer, setReviewer] = useState(
    () => localStorage.getItem(REVIEWER_KEY) ?? "",
  );
  const [reviewNote, setReviewNote] = useState("");
  const [transitions, setTransitions] = useState<Transition[]>([]);
  // The settable-status dropdown's option list (P6.2b, Kiwi statusModel
  // "settable"): every valid status, same source TestTable's workflow-status
  // filter uses. Xray never reads this — it uses `transitions` instead.
  const [allStatuses, setAllStatuses] = useState<string[]>([]);
  const [steps, setSteps] = useState<Step[]>([]);
  const [stepsLoading, setStepsLoading] = useState(false);
  const [stepsError, setStepsError] = useState("");
  // Custom Fields section is collapsed by default: it holds many rarely-needed
  // Xray fields that clutter the test-case review (RND_P_4TFINT_05-321). A
  // triangle toggle expands it on demand.
  const [customFieldsOpen, setCustomFieldsOpen] = useState(false);
  const [showCloneSteps, setShowCloneSteps] = useState(false);
  const [showCallPicker, setShowCallPicker] = useState(false);
  // Cross-project link affordances (RND_P_4TFINT_05-322): the profile's
  // configured source projects gate the "Other project" buttons; separate
  // modal flags open the pickers in cross-project-only mode.
  const [crossProjectSources, setCrossProjectSources] = useState("");
  const crossProjectSourceList = crossProjectSources
    .split(/[\s,;]+/)
    .map((s) => s.trim())
    .filter(Boolean);
  const crossProjectEnabled = crossProjectSourceList.length > 0;
  const [showCrossCall, setShowCrossCall] = useState(false);
  const [showCrossClone, setShowCrossClone] = useState(false);
  const [cloning, setCloning] = useState(false);
  // What Jira itself reports about this Test's steps — used to warn when the
  // panel is empty but Jira actually has steps (a load/shape problem), so the
  // user doesn't add a blank step that Xray rejects.
  const [jiraStepInfo, setJiraStepInfo] = useState<JiraStepInfo | null>(null);
  // Sort state for the run history table. Default: updatedAt descending.
  const [runHistorySort, setRunHistorySort] = useState<{ field: string; desc: boolean }>(
    { field: "updatedAt", desc: true },
  );

  function toggleRunHistorySort(field: string) {
    setRunHistorySort((prev) =>
      prev.field === field
        ? { field, desc: !prev.desc }
        : { field, desc: true },
    );
  }

  function sortedRunHistory(runs: TestRunEntry[]): TestRunEntry[] {
    return [...runs].sort((a, b) => {
      let cmp = 0;
      switch (runHistorySort.field) {
        case "runStatus":
          cmp = (a.runStatus ?? "").localeCompare(b.runStatus ?? "");
          break;
        case "createdAt":
          cmp = (a.createdAt ?? "").localeCompare(b.createdAt ?? "");
          break;
        case "updatedAt":
          cmp = (a.updatedAt ?? "").localeCompare(b.updatedAt ?? "");
          break;
        default:
          cmp = 0;
      }
      return runHistorySort.desc ? -cmp : cmp;
    });
  }
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
  const [execType, setExecType] = useState("");
  const [cucumberScenario, setCucumberScenario] = useState("");
  const [cucumberType, setCucumberType] = useState("");
  const [genericDefinition, setGenericDefinition] = useState("");
  // Set when ChangeTestType reports canPrefill && !prefilled (destination body
  // already had content, so nothing was auto-filled). Cleared on dismiss.
  const [prefillNotice, setPrefillNotice] = useState<TypeConversion | null>(null);
  // Internal counter that forces the main load effect to re-run after a type
  // change so any pre-filled body is hydrated from the backend.
  const [localReloadKey, setLocalReloadKey] = useState(0);

  // Isolated, read-only detail sections now come from the query cache (audit
  // A3, Phase 2b). `reload` folds version + localReloadKey into the key as the
  // migration bridge (a bump refetches).
  const reload = `${version}:${localReloadKey}`;
  const metaQuery = useTestMeta(profileId, testKey, reload);
  const meta = metaQuery.data ?? null;
  const runHistoryQuery = useTestRunHistory(profileId, testKey, reload);
  const runHistory = runHistoryQuery.data ?? null;
  const runHistoryLoading = runHistoryQuery.isFetching;
  const runHistoryError = runHistoryQuery.error
    ? errMsg(runHistoryQuery.error)
    : "";

  // The `test` itself now lives in the query cache (audit A3, Phase 2b),
  // decoupled from the Promise.all below. Optimistic edits (field save, folder
  // move, status transition) patch it via queryClient.setQueryData on this key.
  const queryClient = useQueryClient();
  const testQuery = useTest(profileId, testKey, reload);
  const test = testQuery.data ?? null;
  // The panel is loading until BOTH the secondary Promise.all (`loading`) and
  // the test read resolve — either finishing first must not flash empty content.
  const panelLoading = loading || testQuery.isPending;
  // A test-read failure used to reject the Promise.all and land in `error`; now
  // that GetTest is its own query, surface its error the same way so the panel
  // never goes silently blank on a failed load.
  const displayError =
    error || (testQuery.error ? errMsg(testQuery.error) : "");
  // Guards the one-time seed of the editable draft buffers (summary, labels, …)
  // from a freshly-loaded test. Keyed on testKey:reload so a new test or a
  // reload re-seeds, but an optimistic setQueryData (same key) does NOT — which
  // is what keeps a folder/status change from wiping the user's unsaved drafts.
  const seededRef = useRef<string>("");
  const seedKey = `${testKey}:${reload}`;

  // Tracks the previously-shown key so we can detect a just-committed new Test
  // (its key flips from a "NEW-N" placeholder to the real Jira key) and force a
  // fresh pull from Jira — the local cache still holds temporary step IDs (FR-1).
  const prevKeyRef = useRef<string>("");

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError("");
    setSaveError("");
    setPrefillNotice(null);
    const justCommitted =
      prevKeyRef.current.startsWith("NEW-") && !testKey.startsWith("NEW-");
    prevKeyRef.current = testKey;
    const skipBugs = testKey.startsWith("NEW-");
    Promise.all([
      GetTestPreconditions(profileId, testKey, false),
      GetTestContainers(profileId, testKey),
      ListAllPreconditions(profileId),
      GetTestReview(profileId, testKey),
      GetTestRequirements(profileId, testKey),
      ListRequirementsWithCoverage(profileId),
      skipBugs ? Promise.resolve([]) : GetTestBugs(profileId, testKey),
    ])
      .then(([pre, cons, allPre, rev, reqs, allReqs, testBugs]) => {
        if (cancelled) return;
        // The `test` itself and its editable draft buffers now load via useTest
        // + the seed effect below, decoupled from this waterfall (Phase 2b).
        setPreconditions(pre);
        setContainers(cons ?? []);
        setAllPreconditions(allPre ?? []);
        setReview(rev);
        setRequirements(reqs ?? []);
        setAllRequirements(allReqs ?? []);
        setBugs((testBugs as TestBug[]) ?? []);
        setReviewNote(rev?.note ?? "");
        // Transitions (Xray workflow) / all-statuses (Kiwi settable status)
        // load in their own effect below, gated on
        // caps.supportsWorkflowTransitions.
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
        // (Test meta and run history now load via useTestMeta /
        // useTestRunHistory queries, decoupled from this waterfall.)
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
  }, [profileId, testKey, version, localReloadKey]);

  // Seed the editable draft buffers from a freshly-loaded test, exactly once per
  // load (see seededRef). This replaces the in-.then seeding that the useTest
  // migration removed. It runs when testQuery.data arrives; the seededRef guard
  // makes optimistic setQueryData patches (folder/status/edit) NOT re-seed, so
  // they never clobber the user's unsaved drafts.
  useEffect(() => {
    const t = testQuery.data;
    if (!t) return;
    if (seededRef.current === seedKey) return;
    seededRef.current = seedKey;
    let seeded = t;
    // Default cucumberType to "Scenario" for Cucumber tests with no stored type,
    // reflecting it in the cache (matches the old setTest patch) and persisting
    // it once when editable so a commit won't omit it.
    if (t.execType === "Cucumber" && (t.cucumberType ?? "") === "") {
      setCucumberType("Scenario");
      seeded = { ...t, cucumberType: "Scenario" };
      queryClient.setQueryData(keys.testDetail(profileId, testKey, reload), seeded);
      if (!readOnly) {
        EditTestField(profileId, testKey, "cucumber_type", "Scenario").catch(
          (e) => console.error("default cucumberType:", errMsg(e)),
        );
      }
    } else {
      setCucumberType(t.cucumberType ?? "");
    }
    setSummary(seeded.summary);
    setDescription(seeded.description);
    setPriority(seeded.priority);
    setLabels((seeded.labels ?? []).join(" "));
    setExecType(seeded.execType ?? "");
    setCucumberScenario(seeded.cucumberScenario ?? "");
    setGenericDefinition(seeded.genericDefinition ?? "");
  }, [testQuery.data, seedKey, profileId, testKey, reload, readOnly, queryClient]);

  // Status source (P6.2b): Xray (workflow) loads the transitions available
  // from the current status; Kiwi (settable) loads every valid status once
  // per profile, the same source TestTable's workflow-status filter uses.
  // Gated on caps.supportsWorkflowTransitions so a Kiwi profile never calls
  // GetTestTransitions (there are no transitions to fetch).
  useEffect(() => {
    let cancelled = false;
    if (caps.supportsWorkflowTransitions) {
      // Transitions load alongside the rest of the panel but can fail
      // without blocking it — workflow may not be set up yet, or the user
      // may not have edit permission.
      GetTestTransitions(profileId, testKey)
        .then((ts) => {
          if (!cancelled) setTransitions(ts ?? []);
        })
        .catch((e) => {
          if (!cancelled) console.error("transitions:", errMsg(e));
        });
    } else {
      ListStatuses(profileId)
        .then((s) => {
          if (!cancelled) setAllStatuses(s ?? []);
        })
        .catch((e) => {
          if (!cancelled) console.error("list statuses:", errMsg(e));
        });
    }
    return () => {
      cancelled = true;
    };
  }, [profileId, testKey, caps.supportsWorkflowTransitions]);

  async function saveField(field: EditableField, value: string) {
    if (readOnly) return;
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
      case "cucumber_scenario":
        backendValue = test.cucumberScenario ?? "";
        break;
      case "cucumber_type":
        backendValue = test.cucumberType ?? "";
        break;
      case "generic_definition":
        backendValue = test.genericDefinition ?? "";
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
        case "cucumber_scenario":
          updated.cucumberScenario = value;
          break;
        case "cucumber_type":
          updated.cucumberType = value;
          break;
        case "generic_definition":
          updated.genericDefinition = value;
          break;
      }
      queryClient.setQueryData(
        keys.testDetail(profileId, testKey, reload),
        updated,
      );
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
    if (readOnly) return;
    if (
      !(await confirm({
        title: "Remove from container",
        message: `Remove ${testKey} from ${containerKey}? This change is queued and applied to Jira when you commit.`,
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
    if (readOnly) return;
    if (!test || folderId === test.folderId) return;
    setSaveError("");
    try {
      await MoveTestToFolder(profileId, testKey, folderId);
      queryClient.setQueryData(
        keys.testDetail(profileId, testKey, reload),
        (prev: TestCase | undefined) => (prev ? { ...prev, folderId } : prev),
      );
      onEdited();
    } catch (e) {
      setSaveError(`Move failed: ${errMsg(e)}`);
    }
  }

  // applyRequirements replaces the test's covered-requirement set and refreshes
  // the displayed list. The link changes commit as Jira issue links.
  async function applyRequirements(nextKeys: string[]) {
    if (readOnly) return;
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
    if (readOnly) return;
    const linked = new Set(requirements.map((r) => r.key));
    const additions = keys.filter((k) => k && !linked.has(k));
    if (additions.length === 0) return;
    applyRequirements([...linked, ...additions]);
  }

  async function removeRequirement(key: string) {
    if (readOnly) return;
    if (
      !(await confirm({
        title: "Unlink requirement",
        message: `Unlink ${key} from ${testKey}? This doesn't delete the requirement, just removes the coverage link when you commit.`,
        confirmLabel: "Unlink",
      }))
    )
      return;
    applyRequirements(requirements.map((r) => r.key).filter((k) => k !== key));
  }

  // refreshPreconditions re-reads the links from Xray, bypassing the cache.
  // The sync is the only other thing that fills them in, so this is the way out
  // when a sync dropped them (RND_P_4TFINT_05-339).
  async function refreshPreconditions() {
    setPrecondLoading(true);
    setPrecondError("");
    try {
      const pre = await GetTestPreconditions(profileId, testKey, true);
      setPreconditions(pre ?? []);
      const all = await ListAllPreconditions(profileId);
      setAllPreconditions(all ?? []);
    } catch (e) {
      setPrecondError(errMsg(e));
    } finally {
      setPrecondLoading(false);
    }
  }

  // applyPreconditions replaces the test's precondition set, then refreshes the
  // displayed list from the store (FR-13.5). Add/remove both route here.
  async function applyPreconditions(nextKeys: string[]) {
    if (readOnly) return;
    setSaveError("");
    try {
      await SetTestPreconditions(profileId, testKey, nextKeys);
      const refreshed = await GetTestPreconditions(profileId, testKey, false);
      setPreconditions(refreshed ?? []);
      onEdited();
    } catch (e) {
      setSaveError(`Precondition update failed: ${errMsg(e)}`);
    }
  }

  async function removePrecondition(key: string) {
    if (readOnly) return;
    if (
      !(await confirm({
        title: "Unlink precondition",
        message: `Unlink ${key} from ${testKey}? This doesn't delete the precondition, just removes the link when you commit.`,
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
    if (readOnly) return;
    const linked = new Set(preconditions.map((p) => p.key));
    const additions = keys.filter((k) => k && !linked.has(k));
    if (additions.length === 0) return;
    applyPreconditions([...linked, ...additions]);
  }

  // addCrossProjectPreconditions links one or more preconditions from another
  // project (RND_P_4TFINT_05-322). It first caches the foreign preconditions
  // locally so they display with their summaries, then links their keys.
  async function addCrossProjectPreconditions(pcs: Precondition[]) {
    if (readOnly) return;
    setShowCrossPrecond(false);
    const linked = new Set(preconditions.map((p) => p.key));
    const additions = pcs.filter((p) => p.key && !linked.has(p.key));
    if (additions.length === 0) return;
    setSaveError("");
    try {
      await CacheExternalPreconditions(profileId, additions);
      const all = await ListAllPreconditions(profileId);
      setAllPreconditions(all ?? []);
      await applyPreconditions([
        ...preconditions.map((p) => p.key),
        ...additions.map((p) => p.key),
      ]);
    } catch (e) {
      setSaveError(`Link precondition failed: ${errMsg(e)}`);
    }
  }

  // createAndAssociatePrecondition creates a brand-new Precondition (FR-13.5)
  // and links it to this test. It gets a temporary key until commit creates
  // the issue in Jira.
  async function createAndAssociatePrecondition() {
    if (readOnly) return;
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
    if (readOnly) return;
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
    if (readOnly) return;
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
    if (readOnly) return;
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
    if (readOnly) return;
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
    if (readOnly) return;
    if (!test || !targetStatus) return;
    setSaveError("");
    try {
      await TransitionTest(profileId, testKey, targetStatus);
      queryClient.setQueryData(
        keys.testDetail(profileId, testKey, reload),
        (prev: TestCase | undefined) =>
          prev ? { ...prev, status: targetStatus } : prev,
      );
      // FR-4.4: optionally capture a comment for this transition.
      const comment = await prompt({
        title: `Comment for moving to "${targetStatus}"`,
        placeholder: "Optional (leave blank to skip)",
        submitLabel: "Save",
      });
      if (comment && comment.trim()) {
        await AddTestComment(profileId, testKey, comment.trim());
      }
      // Xray (workflow): re-query the transitions available from the new
      // status. Kiwi (settable): there are no transitions to fetch.
      if (caps.supportsWorkflowTransitions) {
        try {
          const ts = await GetTestTransitions(profileId, testKey);
          setTransitions(ts ?? []);
        } catch (e) {
          console.error("re-fetch transitions:", errMsg(e));
        }
      }
      onEdited();
    } catch (e) {
      setSaveError(`Transition failed: ${errMsg(e)}`);
    }
  }

  // setVerdict records (or clears) a review verdict (test review). The reviewer
  // name is remembered across tests via localStorage.
  async function setVerdict(verdict: string) {
    if (readOnly) return;
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
          {!readOnly && onCloned && !testKey.startsWith("NEW-") && (
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
          This test hasn't been created in Jira yet. It'll be created when you
          commit.
        </div>
      )}

      {panelLoading && <div className="muted detail-body">Loading…</div>}
      {displayError && (
        <div className="error-text detail-body">{displayError}</div>
      )}

      {test && !panelLoading && (
        <div className="detail-body">
          {saveError && (
            <div className="error-text detail-save-error">{saveError}</div>
          )}

          <div className="field-label">
            Summary {isDirty("summary") && <DirtyDot />}
          </div>
          {readOnly ? (
            <p className="detail-input detail-input-static">{summary}</p>
          ) : (
            <input
              className="detail-input"
              value={summary}
              onChange={(e) => setSummary(e.target.value)}
              onBlur={() => saveField("summary", summary)}
              spellCheck
            />
          )}

          <dl className="detail-fields">
            <dt>
              Status {isDirty("status") && <DirtyDot />}
            </dt>
            <dd>
              <div className="status-row">
                <span className="status-pill">{test.status || "—"}</span>
                {caps.supportsWorkflowTransitions ? (
                  // Xray: the existing workflow-transition picker, unchanged.
                  !readOnly &&
                  transitions.length > 0 && (
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
                  )
                ) : (
                  // Kiwi (statusModel "settable"): no workflow, just a
                  // dropdown of every valid status. Picking one calls the
                  // same TransitionTest — the commit engine routes it to
                  // Kiwi's case_status field.
                  !readOnly &&
                  allStatuses.length > 0 && (
                    <select
                      className="transition-select"
                      value={test.status || ""}
                      onChange={(e) => {
                        if (e.target.value && e.target.value !== test.status) {
                          applyTransition(e.target.value);
                        }
                      }}
                    >
                      {allStatuses.map((s) => (
                        <option key={s} value={s}>
                          {s}
                        </option>
                      ))}
                    </select>
                  )
                )}
              </div>
            </dd>

            <dt>
              Priority {isDirty("priority") && <DirtyDot />}
            </dt>
            <dd>
              {readOnly ? (
                <span>{priority || "—"}</span>
              ) : (
                <input
                  className="detail-input detail-input-inline"
                  value={priority}
                  onChange={(e) => setPriority(e.target.value)}
                  onBlur={() => saveField("priority", priority)}
                />
              )}
            </dd>

            <dt>
              Labels {isDirty("labels") && <DirtyDot />}
            </dt>
            <dd>
              {readOnly ? (
                <span>{labels || "—"}</span>
              ) : (
                <input
                  className="detail-input detail-input-inline"
                  value={labels}
                  onChange={(e) => setLabels(e.target.value)}
                  onBlur={() => saveField("labels", labels)}
                  placeholder="space-separated"
                />
              )}
            </dd>

            {caps.supportsTestTypes && (
              <>
                <dt>
                  Execution type {isDirty("exec_type") && <DirtyDot />}
                </dt>
                <dd>
                  {readOnly ? (
                    <span>{execType || "—"}</span>
                  ) : (
                    <select
                      className="detail-input detail-input-inline"
                      value={execType}
                      onChange={async (e) => {
                        const next = e.target.value;
                        setExecType(next);
                        try {
                          const res = await ChangeTestType(profileId, testKey, next);
                          // Re-fetch the test so any pre-filled body appears.
                          setLocalReloadKey((k) => k + 1);
                          if (res.canPrefill && !res.prefilled) {
                            setPrefillNotice(res);
                          }
                        } catch (err) {
                          setSaveError(`Type change failed: ${errMsg(err)}`);
                        }
                      }}
                    >
                      <option value="">—</option>
                      {EXEC_TYPE_OPTIONS.map((o) => (
                        <option key={o} value={o}>
                          {o}
                        </option>
                      ))}
                    </select>
                  )}
                </dd>
              </>
            )}

            {caps.supportsFolders && folders.length > 0 && (
              <>
                <dt>
                  Folder {isDirty("folder") && <DirtyDot />}
                </dt>
                <dd>
                  {readOnly ? (
                    <span>{test.folderId || "(repository root)"}</span>
                  ) : (
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
                  )}
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
            {!readOnly && (
              <>
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
              </>
            )}
          </div>
            </>
          )}

          {customFields.length > 0 && (
            <>
              <h4 className="detail-collapse-h4">
                <button
                  type="button"
                  className="detail-collapse-toggle"
                  onClick={() => setCustomFieldsOpen((o) => !o)}
                  aria-expanded={customFieldsOpen}
                  title={
                    customFieldsOpen
                      ? "Hide custom fields"
                      : "Show custom fields"
                  }
                >
                  <span
                    className="detail-collapse-chevron"
                    style={{
                      transform: customFieldsOpen ? "rotate(90deg)" : "none",
                    }}
                    aria-hidden="true"
                  >
                    ▶
                  </span>
                  Custom Fields
                  <span className="detail-collapse-count">
                    {customFields.length}
                  </span>
                </button>
              </h4>
              {customFieldsOpen && (
                <dl className="detail-fields">
                  {customFields.map((f) => (
                    <CustomFieldRow
                      key={f.fieldId}
                      profileId={profileId}
                      testKey={testKey}
                      field={f}
                      pendingForTest={pendingForTest}
                      readOnly={readOnly}
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
              )}
            </>
          )}

          {caps.supportsPreconditionObjects && (
            <>
              <h4>
                Preconditions {isDirty("preconditions") && <DirtyDot />}
                <button
                  className="link-btn steps-refresh"
                  onClick={refreshPreconditions}
                  disabled={precondLoading}
                  title="Re-fetch preconditions from Jira"
                >
                  {precondLoading ? "Loading…" : "Refresh"}
                </button>
              </h4>
              {precondError && (
                <p className="error-text">{precondError}</p>
              )}
              {preconditions.length === 0 ? (
                <p className="muted">None linked</p>
              ) : (
                <ul className="pre-list">
                  {preconditions.map((p) => (
                    <PreconditionRow
                      key={p.key}
                      profileId={profileId}
                      precondition={p}
                      readOnly={readOnly}
                      onRemove={removePrecondition}
                      onEdited={onEdited}
                    />
                  ))}
                </ul>
              )}
              {!readOnly && (
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
                  {crossProjectEnabled && (
                    <button
                      type="button"
                      className="btn btn-ghost pre-add-new"
                      onClick={() => setShowCrossPrecond(true)}
                      title="Link a precondition from another project"
                    >
                      ↗ Other project
                    </button>
                  )}
                  <button
                    type="button"
                    className="btn btn-ghost pre-add-new"
                    onClick={() => setSwapKind("precondition")}
                    disabled={preconditions.length === 0 && allPreconditions.length === 0}
                    title="Swap preconditions: remove some and add others in one apply"
                  >
                    ⇄ Swap
                  </button>
                </div>
              )}
            </>
          )}

          {(() => {
            const sets = containers.filter((c) => c.kind === "testset");
            const plans = containers.filter((c) => c.kind === "testplan");
            const execs = containers.filter((c) => c.kind === "testexec");
            if (containers.length === 0) {
              return (
                <>
                  <h4>Memberships</h4>
                  <p className="muted">
                    This test isn't in any set, plan, or execution yet.
                  </p>
                </>
              );
            }
            return (
              <>
                {sets.length > 0 && (
                  <ContainerSection
                    title="Test Sets"
                    items={sets}
                    readOnly={readOnly}
                    onRemove={deallocateContainer}
                  />
                )}
                {plans.length > 0 && (
                  <ContainerSection
                    title="Test Plans"
                    items={plans}
                    readOnly={readOnly}
                    onRemove={deallocateContainer}
                  />
                )}
                {execs.length > 0 && (
                  <ContainerSection
                    title="Test Executions"
                    items={execs}
                    showRunStatus
                    readOnly={readOnly}
                    onRemove={deallocateContainer}
                  />
                )}
              </>
            );
          })()}

          {caps.supportsRequirementObjects && (
            <>
              <h4>
                Requirements {isDirty("requirements") && <DirtyDot />}
              </h4>
              {requirements.length === 0 ? (
                <p className="muted">
                  This test isn't linked to any requirement yet.
                </p>
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
                      {!readOnly && (
                        <button
                          className="btn btn-ghost pre-remove"
                          onClick={() => removeRequirement(rq.key)}
                          title="Unlink this requirement"
                        >
                          ✕
                        </button>
                      )}
                    </li>
                  ))}
                </ul>
              )}
              {!readOnly && (
                <div className="pre-add pre-add-row">
                  <MultiAddSelect
                    placeholder="+ Link requirement…"
                    onAdd={addRequirements}
                    options={allRequirements
                      .filter((r) => !requirements.some((lr) => lr.key === r.key))
                      .map((r) => ({
                        value: r.key,
                        label: `${r.key} — ${r.summary}`,
                      }))}
                  />
                  <button
                    type="button"
                    className="btn btn-ghost pre-add-new"
                    onClick={() => setSwapKind("requirement")}
                    disabled={requirements.length === 0 && allRequirements.length === 0}
                    title="Swap requirements: unlink some and link others in one apply"
                  >
                    ⇄ Swap
                  </button>
                </div>
              )}
            </>
          )}

          <h4>Bugs</h4>
          {bugs.length === 0 ? (
            <p className="muted">No bugs linked yet.</p>
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

          <h4>Run history</h4>
          {runHistoryError && (
            <div className="error-text">{runHistoryError}</div>
          )}
          {!runHistoryError && runHistoryLoading && (
            <p className="muted">Loading…</p>
          )}
          {!runHistoryError && !runHistoryLoading && runHistory !== null && (
            runHistory.length === 0 ? (
              <p className="muted">No run history yet.</p>
            ) : (
              <table className="run-history-table">
                <thead>
                  <tr>
                    <th>Execution</th>
                    <th
                      style={{ cursor: "pointer", userSelect: "none", whiteSpace: "nowrap" }}
                      onClick={() => toggleRunHistorySort("runStatus")}
                      title="Sort by result"
                    >
                      Result{runHistorySort.field === "runStatus" ? (runHistorySort.desc ? " ▾" : " ▴") : ""}
                    </th>
                    <th>By</th>
                    <th>Environment</th>
                    <th>Plan(s)</th>
                    <th>Fix Version(s)</th>
                    <th
                      style={{ cursor: "pointer", userSelect: "none", whiteSpace: "nowrap" }}
                      onClick={() => toggleRunHistorySort("createdAt")}
                      title="Sort by created date"
                    >
                      Created{runHistorySort.field === "createdAt" ? (runHistorySort.desc ? " ▾" : " ▴") : ""}
                    </th>
                    <th
                      style={{ cursor: "pointer", userSelect: "none", whiteSpace: "nowrap" }}
                      onClick={() => toggleRunHistorySort("updatedAt")}
                      title="Sort by updated date"
                    >
                      Updated{runHistorySort.field === "updatedAt" ? (runHistorySort.desc ? " ▾" : " ▴") : ""}
                    </th>
                    <th>Defects</th>
                  </tr>
                </thead>
                <tbody>
                  {sortedRunHistory(runHistory).map((r, i) => (
                    <tr key={`${r.execKey}-${i}`}>
                      <td>
                        {canLinkToJira && r.execKey && !r.execKey.startsWith("NEW-") ? (
                          <button
                            className="mono bug-link-key"
                            onClick={() => openBugInJira(r.execKey)}
                            title={r.execSummary || `Open ${r.execKey} in Jira`}
                          >
                            {r.execKey}
                          </button>
                        ) : (
                          <span className="mono" title={r.execSummary}>{r.execKey}</span>
                        )}
                        {r.execSummary && (
                          <span className="muted run-history-exec-summary">{r.execSummary}</span>
                        )}
                      </td>
                      <td>
                        {r.runStatus ? <RunStatusBadge status={r.runStatus} /> : <span className="muted">—</span>}
                      </td>
                      <td>{r.executedBy || <span className="muted">—</span>}</td>
                      <td>{r.environment || <span className="muted">—</span>}</td>
                      <td>{r.planKeys?.length ? r.planKeys.join(", ") : <span className="muted">—</span>}</td>
                      <td>{r.fixVersions?.length ? r.fixVersions.join(", ") : <span className="muted">—</span>}</td>
                      <td className="muted run-history-date" style={{ whiteSpace: "nowrap" }}>
                        {formatDateTime(r.createdAt) || "—"}
                      </td>
                      <td className="muted run-history-date" style={{ whiteSpace: "nowrap" }}>
                        {formatDateTime(r.updatedAt) || "—"}
                      </td>
                      <td>
                        {r.defects?.length ? (
                          <span className="run-history-defects">
                            {r.defects.map((d, di) => (
                              <span key={d}>
                                {di > 0 && ", "}
                                {canLinkToJira && !d.startsWith("NEW-") ? (
                                  <button
                                    className="mono bug-link-key"
                                    onClick={() => openBugInJira(d)}
                                    title={`Open ${d} in Jira`}
                                  >
                                    {d}
                                  </button>
                                ) : (
                                  <span className="mono">{d}</span>
                                )}
                              </span>
                            ))}
                          </span>
                        ) : (
                          <span className="muted">—</span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )
          )}

          <h4>
            Description {isDirty("description") && <DirtyDot />}
          </h4>
          {readOnly ? (
            <p className="detail-input detail-input-static detail-desc-static">
              {description || <span className="muted">No description yet.</span>}
            </p>
          ) : (
            <MarkdownField
              className="detail-desc-edit"
              value={description}
              onChange={setDescription}
              onCommit={() => saveField("description", description)}
              rows={8}
              placeholder="No description. Click to add (markdown supported)."
            />
          )}

          {prefillNotice && (
            <div className="prefill-notice">
              The previous type already had content, so it was left unchanged.{" "}
              <button
                className="link-btn"
                onClick={() => setPrefillNotice(null)}
              >
                Dismiss
              </button>
            </div>
          )}

          {execType === "Cucumber" ? (
            <section className="cuke-editor">
              <h4 className="steps-head">Cucumber scenario</h4>
              <label className="cuke-type-label">
                Scenario type{" "}
                <select
                  className="detail-input detail-input-inline"
                  value={cucumberType}
                  disabled={readOnly}
                  onChange={(e) => {
                    setCucumberType(e.target.value);
                    if (!readOnly) saveField("cucumber_type", e.target.value);
                  }}
                >
                  <option value="Scenario">Scenario</option>
                  <option value="Scenario Outline">Scenario Outline</option>
                </select>
              </label>
              <textarea
                className="cuke-scenario mono"
                value={cucumberScenario}
                readOnly={readOnly}
                onChange={(e) => setCucumberScenario(e.target.value)}
                onBlur={() => {
                  if (!readOnly) saveField("cucumber_scenario", cucumberScenario);
                }}
                rows={14}
                placeholder="Given ...\nWhen ...\nThen ..."
              />
            </section>
          ) : execType === "Generic" ? (
            <section className="generic-editor">
              <h4 className="steps-head">Generic definition</h4>
              <textarea
                className="generic-def mono"
                value={genericDefinition}
                readOnly={readOnly}
                onChange={(e) => setGenericDefinition(e.target.value)}
                onBlur={() => {
                  if (!readOnly) saveField("generic_definition", genericDefinition);
                }}
                rows={14}
                placeholder="Enter test definition…"
              />
            </section>
          ) : (
            <>
          <h4 className="steps-head">
            Steps
            {caps.stepModel === "objects" &&
              pendingForTest.some((p) => p.entityType === "test_step_order") && (
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
            {!readOnly && caps.stepModel === "objects" && (
              <button
                className="link-btn steps-clone"
                onClick={() => setShowCloneSteps(true)}
                disabled={stepsLoading}
                title="Append a copy of another test's steps to this one"
              >
                Clone from…
              </button>
            )}
            {!readOnly && caps.stepModel === "objects" && crossProjectEnabled && (
              <button
                className="link-btn steps-clone"
                onClick={() => setShowCrossClone(true)}
                disabled={stepsLoading}
                title="Clone steps from a test in another project"
              >
                ↗ Other project
              </button>
            )}
          </h4>
          {stepsError && <div className="error-text">{stepsError}</div>}
          {caps.stepModel === "objects" ? (
            // Xray: the existing multi-step CRUD list, unchanged.
            <>
              {!stepsError &&
                !stepsLoading &&
                steps.length === 0 &&
                (jiraStepInfo && jiraStepInfo.count > 0 ? (
                  <div className="steps-warning">
                    ⚠ Jira reports {jiraStepInfo.count} step
                    {jiraStepInfo.count === 1 ? "" : "s"} for this test that didn't
                    load here. Don't add new steps yet, since that would create
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
                    ⚠ These steps loaded without content. This Xray instance may use
                    a step format the tool doesn't recognise yet, so avoid editing
                    them to prevent overwriting the real steps in Jira.
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
                      readOnly={readOnly}
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
              {!readOnly && !stepsError && !stepsLoading && (
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
                  {crossProjectEnabled && (
                    <button
                      className="link-btn steps-add"
                      onClick={() => setShowCrossCall(true)}
                      title="Call a test from another project"
                    >
                      ↗ Other project
                    </button>
                  )}
                </div>
              )}
            </>
          ) : (
            // Kiwi (stepModel "inline-text"): one flattened step, edited as a
            // single multi-line text field — no add/delete/reorder/call-step.
            !stepsError &&
            !stepsLoading && (
              <InlineStepsEditor
                profileId={profileId}
                testKey={testKey}
                step={steps[0]}
                readOnly={readOnly}
                onLocalChange={(xrayId, value) =>
                  setSteps((prev) =>
                    prev.map((p) =>
                      p.xrayId === xrayId ? { ...p, action: value } : p,
                    ),
                  )
                }
                onLocalCreate={(s) => setSteps((prev) => [...prev, s])}
                onEdited={onEdited}
              />
            )
          )}

          {!readOnly && (
            <p className="muted detail-note">
              Your edits are saved locally and queued in <b>Pending</b> until
              you commit them to Jira. Reordering steps will land in a later
              update.
            </p>
          )}
            </>
          )}
        </div>
      )}
      {!readOnly && showCallPicker && (
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

      {!readOnly && showCrossCall && (
        <PickTestModal
          profileId={profileId}
          heading={`Call a test from another project`}
          excludeKey={testKey}
          crossProjectOnly
          sourceProjects={crossProjectSourceList}
          onCancel={() => setShowCrossCall(false)}
          onPick={async (calledKey) => {
            const s = await AddCalledTestStep(profileId, testKey, calledKey);
            setSteps((prev) => [...prev, s]);
            setShowCrossCall(false);
            onEdited();
          }}
        />
      )}

      {!readOnly && showCrossClone && (
        <CloneStepsModal
          profileId={profileId}
          targetLabel={testKey}
          excludeKey={testKey}
          crossProjectOnly
          sourceProjects={crossProjectSourceList}
          onCancel={() => setShowCrossClone(false)}
          onConfirm={async (sourceKey, stepIds) => {
            const newSteps = await CloneTestSteps(
              profileId,
              testKey,
              sourceKey,
              stepIds,
            );
            setSteps(newSteps ?? []);
            setShowCrossClone(false);
            onEdited();
          }}
        />
      )}

      {!readOnly && showCrossPrecond && (
        <PickPreconditionModal
          profileId={profileId}
          excludeKeys={preconditions.map((p) => p.key)}
          sourceProjects={crossProjectSourceList}
          onCancel={() => setShowCrossPrecond(false)}
          onPick={addCrossProjectPreconditions}
        />
      )}

      {!readOnly && showCloneSteps && (
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
      {!readOnly && swapKind === "precondition" && (
        <SwapModal
          title="Swap preconditions"
          current={preconditions.map((p) => ({
            key: p.key,
            label: `${p.key} — ${p.summary}`,
          }))}
          candidates={allPreconditions
            .filter((p) => !preconditions.some((lp) => lp.key === p.key))
            .map((p) => ({ key: p.key, label: `${p.key} — ${p.summary}` }))}
          onCancel={() => setSwapKind(null)}
          onConfirm={async (next) => {
            await applyPreconditions(next);
            setSwapKind(null);
          }}
        />
      )}
      {!readOnly && swapKind === "requirement" && (
        <SwapModal
          title="Swap requirements"
          current={requirements.map((rq) => ({
            key: rq.key,
            label: `${rq.key} — ${rq.summary}`,
          }))}
          candidates={allRequirements
            .filter((r) => !requirements.some((lr) => lr.key === r.key))
            .map((r) => ({ key: r.key, label: `${r.key} — ${r.summary}` }))}
          onCancel={() => setSwapKind(null)}
          onConfirm={async (next) => {
            await applyRequirements(next);
            setSwapKind(null);
          }}
        />
      )}
      {promptUI}
      {confirmUI}
    </aside>
  );
}

interface SwapItem {
  key: string;
  label: string;
}

// SwapModal lists the test's current items as checkboxes (ticked = remove) and
// a multi-pick add list, then computes next = (current minus removed) + added
// and hands it back in one apply (RND_P_4TFINT_05-231).
function SwapModal({
  title,
  current,
  candidates,
  onCancel,
  onConfirm,
}: {
  title: string;
  current: SwapItem[];
  candidates: SwapItem[];
  onCancel: () => void;
  onConfirm: (next: string[]) => void | Promise<void>;
}) {
  const [toRemove, setToRemove] = useState<Set<string>>(new Set());
  const [toAdd, setToAdd] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);

  function toggleRemove(key: string) {
    setToRemove((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }

  function toggleAdd(key: string) {
    setToAdd((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }

  async function confirm() {
    const next = [
      ...current.map((c) => c.key).filter((k) => !toRemove.has(k)),
      ...candidates.map((c) => c.key).filter((k) => toAdd.has(k)),
    ];
    setBusy(true);
    try {
      await onConfirm(next);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal onClose={onCancel} className="modal bulk-modal" labelledBy="bulk-swap-title">
        <div className="pending-head">
          <h2 id="bulk-swap-title">{title}</h2>
          <button className="btn btn-ghost" onClick={onCancel} title="Close">
            ✕
          </button>
        </div>
        <div className="bulk-body">
          <div className="bulk-row bulk-swap-row">
            <span>Remove</span>
            <ul className="bulk-swap-list">
              {current.length === 0 && <li className="muted">None linked</li>}
              {current.map((c) => (
                <li key={c.key}>
                  <label>
                    <input
                      type="checkbox"
                      checked={toRemove.has(c.key)}
                      onChange={() => toggleRemove(c.key)}
                    />
                    <span>{c.label}</span>
                  </label>
                </li>
              ))}
            </ul>
          </div>
          <div className="bulk-row bulk-swap-row">
            <span>Add</span>
            <ul className="bulk-swap-list">
              {candidates.length === 0 && (
                <li className="muted">Nothing else to add</li>
              )}
              {candidates.map((c) => (
                <li key={c.key}>
                  <label>
                    <input
                      type="checkbox"
                      checked={toAdd.has(c.key)}
                      onChange={() => toggleAdd(c.key)}
                    />
                    <span>{c.label}</span>
                  </label>
                </li>
              ))}
            </ul>
          </div>
          <p className="muted bulk-preview">
            Ticked Remove items are dropped, and ticked Add items are linked,
            in one apply. The change is queued locally, commit it from
            Pending.
          </p>
        </div>
        <div className="pending-actions">
          <button className="btn" onClick={onCancel} disabled={busy}>
            Cancel
          </button>
          <button
            className="btn btn-primary"
            onClick={confirm}
            disabled={busy || (toRemove.size === 0 && toAdd.size === 0)}
          >
            {busy ? "Applying…" : "Apply swap"}
          </button>
        </div>
    </Modal>
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
  readOnly?: boolean;
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
  readOnly,
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
    if (readOnly) return;
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
    if (readOnly) return;
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
          {!readOnly && (
            <>
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
            </>
          )}
        </div>
        {saveError && <div className="error-text step-save-error">{saveError}</div>}
      </li>
    );
  }

  return (
    <li>
      <div className="step-head">
        {readOnly ? (
          <p className="step-edit step-edit-action step-field-static">{action}</p>
        ) : (
          <MarkdownField
            className="step-edit step-edit-action"
            value={action}
            onChange={setAction}
            onCommit={() => save("action", action)}
            rows={2}
            placeholder="(action)"
          />
        )}
        {isNew && <span className="step-new-badge">new</span>}
        {!readOnly && (
          <>
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
          </>
        )}
      </div>
      {isDirty("action") && <DirtyDot />}
      <div className="step-row">
        <span className="step-label">
          Data {isDirty("data") && <DirtyDot />}
        </span>
        {readOnly ? (
          <p className="step-edit step-field-static">{data}</p>
        ) : (
          <MarkdownField
            className="step-edit"
            value={data}
            onChange={setData}
            onCommit={() => save("data", data)}
            multiline={false}
            placeholder="(optional)"
          />
        )}
      </div>
      <div className="step-row">
        <span className="step-label">
          Expected {isDirty("expected") && <DirtyDot />}
        </span>
        {readOnly ? (
          <p className="step-edit step-field-static">{expected}</p>
        ) : (
          <MarkdownField
            className="step-edit"
            value={expected}
            onChange={setExpected}
            onCommit={() => save("expected", expected)}
            rows={2}
            placeholder="(expected result)"
          />
        )}
      </div>
      {saveError && <div className="error-text step-save-error">{saveError}</div>}
    </li>
  );
}

// InlineStepsEditor renders Kiwi's single inline-text step model
// (caps.stepModel "inline-text") in place of the Xray multi-step CRUD list.
// Kiwi has no step objects — flattenSteps (read path) collapses its one
// `text` field to at most one neutral Step — so there is nothing to
// add/delete/reorder, just one multi-line field. Saving calls
// EditTestStepField on that step's id when it already exists; when there is
// no step yet (empty text), it calls AddTestStep to create the first one.
// Either way the commit engine collapses the result back to Kiwi's single
// `text` field (see internal/syncer/commit.go).
function InlineStepsEditor({
  profileId,
  testKey,
  step,
  readOnly,
  onLocalChange,
  onLocalCreate,
  onEdited,
}: {
  profileId: string;
  testKey: string;
  step: Step | undefined;
  readOnly?: boolean;
  onLocalChange: (xrayId: string, value: string) => void;
  onLocalCreate: (step: Step) => void;
  onEdited: () => void;
}) {
  const [text, setText] = useState(step?.action ?? "");
  const [saveError, setSaveError] = useState("");

  useEffect(() => {
    setText(step?.action ?? "");
  }, [step?.xrayId, step?.action]);

  async function save() {
    if (readOnly) return;
    setSaveError("");
    try {
      if (step) {
        if (text === step.action) return;
        await EditTestStepField(profileId, testKey, step.xrayId, "action", text);
        onLocalChange(step.xrayId, text);
      } else {
        if (!text.trim()) return;
        const s = await AddTestStep(profileId, testKey, text, "", "");
        onLocalCreate(s);
      }
      onEdited();
    } catch (e) {
      setSaveError(errMsg(e));
    }
  }

  return (
    <div className="steps-inline">
      {readOnly ? (
        <p className="detail-input detail-input-static detail-desc-static">
          {text || <span className="muted">No steps defined for this test.</span>}
        </p>
      ) : (
        <MarkdownField
          className="detail-desc-edit"
          value={text}
          onChange={setText}
          onCommit={save}
          rows={8}
          placeholder="Steps for this test."
        />
      )}
      {saveError && <div className="error-text step-save-error">{saveError}</div>}
    </div>
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
  readOnly,
  onLocalChange,
  onEdited,
}: {
  profileId: string;
  testKey: string;
  field: CustomFieldValue;
  pendingForTest: PendingChange[];
  readOnly?: boolean;
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
    if (readOnly) return;
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
        {readOnly ? (
          <span>{value || "—"}</span>
        ) : (
          <input
            className="detail-input detail-input-inline"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onBlur={save}
          />
        )}
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
  readOnly,
  onRemove,
  onEdited,
}: {
  profileId: string;
  precondition: Precondition;
  readOnly?: boolean;
  onRemove: (key: string) => void;
  onEdited: () => void;
}) {
  const [summary, setSummary] = useState(precondition.summary);
  const [saveError, setSaveError] = useState("");

  async function save() {
    if (readOnly) return;
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
      {readOnly ? (
        <span className="pre-summary-static">{summary}</span>
      ) : (
        <input
          className="pre-summary-edit"
          value={summary}
          onChange={(e) => setSummary(e.target.value)}
          onBlur={save}
        />
      )}
      {!readOnly && (
        <button
          className="btn btn-ghost pre-remove"
          onClick={() => onRemove(precondition.key)}
          title="Remove this precondition"
        >
          ✕
        </button>
      )}
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
  readOnly,
  onRemove,
}: {
  title: string;
  items: ContainerMembership[];
  showRunStatus?: boolean;
  readOnly?: boolean;
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
              {!readOnly && (
                <button
                  className="btn btn-ghost pre-remove"
                  onClick={() => onRemove(c.key)}
                  title="Remove this test from the container"
                >
                  ✕
                </button>
              )}
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
