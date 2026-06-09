// api.ts — typed access to the Wails Go backend.
//
// The data interfaces below are plain shapes that match the JSON the backend
// returns. They are intentionally separate from the generated wailsjs model
// classes so plain object literals (initial state, query objects) type-check.

export {
  Health,
  GetDiagnostics,
  ReadLog,
  ExportDiagnostics,
  GetSettings,
  SetDefaultProfile,
  SetTheme,
  ListProfiles,
  CreateProfile,
  SyncProfileFull,
  UpdateProfileScope,
  ExportProfile,
  ImportProfile,
  UpdateProfileToken,
  DeleteProfile,
  TestConnection,
  SyncProfile,
  GetSyncState,
  ListSyncLog,
  ListFolders,
  CreateFolder,
  RenameFolder,
  DeleteFolder,
  GetTestPreconditions,
  ListAllPreconditions,
  SetTestPreconditions,
  EditPreconditionField,
  CreatePrecondition,
  BulkAssociatePreconditions,
  GetTestContainers,
  ListContainers,
  AllocateTests,
  DeallocateTests,
  SetTestRunStatus,
  CreateContainerAndAllocate,
  EditContainer,
  DeleteContainer,
  SeedSampleContainers,
  GetContainerBoard,
  ExportPytest,
  MoveTestToFolder,
  BulkMoveToFolder,
  ListTests,
  ListMatchingKeys,
  ListComponents,
  ListStatuses,
  PreviewImport,
  ImportTests,
  ExportTests,
  ExportImportTemplate,
  CreateSavedView,
  ListSavedViews,
  DeleteSavedView,
  GetTest,
  GetTestMeta,
  EditTestField,
  DiscardPendingChange,
  ResolveConflictOverride,
  ResolveConflictKeepRemote,
  ListPendingChanges,
  ListAuditEntries,
  CommitPendingChanges,
  CommitPendingChangesByIDs,
  BulkEditTests,
  GetTestTransitions,
  TransitionTest,
  AddTestComment,
  GetBulkTransitionOptions,
  BulkTransitionTests,
  GetTestSteps,
  CheckJiraTestSteps,
  GetTestReview,
  SetTestReview,
  BulkReviewTests,
  GetTestCustomFields,
  EditTestCustomField,
  EditTestStepField,
  DeleteTestStep,
  AddTestStep,
  ReorderTestSteps,
  GetStatistics,
  GetTraceabilitySankey,
} from "../wailsjs/go/main/App";
export { EventsOn, BrowserOpenURL } from "../wailsjs/runtime/runtime";

export interface HealthInfo {
  ok: boolean;
  error: string;
  dbPath: string;
  logPath: string;
}

// Settings mirrors settings.Settings — global app preferences (FR-12.2).
export interface Settings {
  defaultProfileId: string;
  theme: string; // "light" | "dark" | "system" | "" (= light)
}

export interface Profile {
  id: string;
  name: string;
  jiraUrl: string;
  projectKey: string;
  scopeJql: string;
  createdAt: string;
}

// Diagnostics mirrors app.Diagnostics — the environment + state summary shown
// in the diagnostics view (FR-12.4).
export interface Diagnostics {
  version: string;
  dbPath: string;
  logPath: string;
  os: string;
  arch: string;
  goVersion: string;
  schemaVersion: number;
  profileCount: number;
  startupError: string;
}

export interface TestCase {
  key: string;
  id: string;
  summary: string;
  description: string;
  status: string;
  priority: string;
  labels: string[];
  components: string[];
  updated: string;
  folderId: string;
}

export interface TestPage {
  tests: TestCase[];
  total: number;
}

export interface TestQuery {
  search: string;
  status: string;
  folderId: string;
  containerKey: string;
  component: string;
  review: string;
  sortBy: string;
  desc: boolean;
  limit: number;
  offset: number;
}

export interface SyncState {
  profileId: string;
  lastSyncedAt: string;
  testCount: number;
}

// SyncLogEntry mirrors testrepo.SyncLogEntry — one sync run's outcome (FR-1.7).
export interface SyncLogEntry {
  id: number;
  startedAt: string;
  finishedAt: string;
  outcome: string;
  fetched: number;
  error: string;
}

// SavedView mirrors testrepo.SavedView — a named browse filter (FR-11.4). The
// query is an opaque JSON string the frontend owns.
export interface SavedView {
  id: string;
  name: string;
  query: string;
  createdAt: string;
}

export interface Folder {
  id: string;
  parentId: string;
  name: string;
  testCount: number;
  totalTestCount: number;
}

export interface Precondition {
  key: string;
  summary: string;
  type: string;
  description: string;
}

// Container mirrors testrepo.Container — a Test Set, Test Plan or Test
// Execution (kind = "testset" / "testplan" / "testexec").
export interface Container {
  key: string;
  kind: string;
  summary: string;
  status: string;
}

// AllocateResult mirrors testrepo.AllocateResult — the outcome of a bulk
// allocation (FR-3.4–3.6).
export interface AllocateResult {
  added: string[];
  alreadyMembers: string[];
}

// DeallocateResult mirrors testrepo.DeallocateResult — the outcome of removing
// Tests from a container (FR-3.4–3.6).
export interface DeallocateResult {
  removed: string[];
  notMembers: string[];
}

// CreateContainerResult mirrors testrepo.CreateContainerResult — the outcome
// of creating a new container and allocating Tests to it (FR-3.4–3.6).
export interface CreateContainerResult {
  tempKey: string;
  added: number;
}

// SeedResult mirrors testrepo.SeedResult — how much sample container data was
// generated.
export interface SeedResult {
  sets: number;
  plans: number;
  executions: number;
  linked: number;
}

// TestPlanBoardRow mirrors testrepo.TestPlanBoardRow — one Test on a Test Plan
// board (FR-13.7) with its consolidated execution status.
export interface TestPlanBoardRow {
  testKey: string;
  summary: string;
  status: string;
  runStatus: string;
}

// TestPlanBoard mirrors testrepo.TestPlanBoard — a Test Plan's member Tests
// with consolidated execution status, plus a run-status histogram.
export interface TestPlanBoard {
  key: string;
  summary: string;
  rows: TestPlanBoardRow[];
  runCounts: Bucket[];
}

// ContainerMembership mirrors testrepo.ContainerMembership — a Test Set, Test
// Plan or Test Execution a Test belongs to (FR-1.3). kind is
// "testset" / "testplan" / "testexec"; runStatus is the Test Run result for
// execution memberships, empty otherwise.
export interface ContainerMembership {
  key: string;
  kind: string;
  summary: string;
  status: string;
  runStatus: string;
}

// JiraStepInfo mirrors app.JiraStepInfo — what Jira reports about a Test's
// steps, used to detect "Jira has steps but the tool shows none" (FR-2.5).
export interface JiraStepInfo {
  count: number;
  allBlank: boolean;
}

// Step mirrors testrepo.Step — one ordered step in an Xray Test (FR-2.5).
// xrayId is Xray's per-step identifier, kept around so a future step
// editor can target each row individually.
// TestMeta mirrors jira.TestMeta — issue-level metadata for the detail summary.
export interface TestMeta {
  created: string;
  creator: string;
  updated: string;
  updatedBy: string;
}

export interface Step {
  xrayId: string;
  index: number;
  action: string;
  data: string;
  expected: string;
}

// CustomFieldValue mirrors testrepo.CustomFieldValue — a Jira custom field on
// the Test issue type with this Test's value (FR-2.6).
export interface CustomFieldValue {
  fieldId: string;
  name: string;
  type: string;
  value: string;
}

export interface PendingChange {
  id: number;
  entityType: string;
  entityKey: string;
  field: string;
  beforeVal: string;
  afterVal: string;
  baseVersion: string;
  createdAt: string;
}

export interface AuditEntry {
  id: number;
  occurredAt: string;
  actor: string;
  entityType: string;
  entityKey: string;
  action: string;
  field: string;
  beforeVal: string;
  afterVal: string;
  note: string;
}

// SyncProgress mirrors the Go syncer.Progress payload emitted on "sync:progress".
// phase is "" / "tests" for the Test pull or "folders" for the Test Repository
// membership pass.
export interface SyncProgress {
  phase: string;
  fetched: number;
  total: number;
  done: boolean;
}

// CommitResult mirrors syncer.CommitResult — per-Test outcome of pushing
// pending changes to Jira. Succeeded / Conflicted / Failed are disjoint sets.
export interface CommitResult {
  succeeded: string[];
  conflicted: Conflict[];
  failed: FailedCommit[];
}

// Conflict means the remote `updated` has advanced since the user's earliest
// pending edit on that Test — the PUT was held back so they can resolve.
export interface Conflict {
  testKey: string;
  baseVersion: string;
  remoteVersion: string;
}

export interface FailedCommit {
  testKey: string;
  error: string;
}

// Bulk-edit (FR-3) operation descriptor and result types.
export interface BulkEdit {
  operation: string;
  field: string;
  value: string;
}

export interface BulkEditResult {
  succeeded: string[];
  failed: BulkFailure[];
}

export interface BulkFailure {
  testKey: string;
  error: string;
}

// Transition is one workflow move available from a Test's current status
// (FR-4.2). The detail panel uses {name → to} as the dropdown label.
export interface Transition {
  id: string;
  name: string;
  to: string;
}

// BulkTransitionOptions is what the bulk-transition modal asks for on
// open: a histogram of current statuses across the selection, and the
// union of target statuses reachable from at least one of those.
export interface BulkTransitionOptions {
  currentStatusCounts: { [status: string]: number };
  reachableTargets: string[];
}

// BulkTransitionResult mirrors app.BulkTransitionResult — per-Test outcome
// of a bulk transition. Succeeded / Skipped / Failed are disjoint sets.
export interface BulkTransitionResult {
  succeeded: string[];
  skipped: BulkTransitionSkip[];
  failed: BulkFailure[];
}

export interface BulkTransitionSkip {
  testKey: string;
  reason: string;
}

// Bucket is one (label, count) pair in a dashboard distribution (FR-9).
export interface Bucket {
  label: string;
  count: number;
}

// Statistics mirrors testrepo.Statistics — the per-profile dashboard rollup
// computed from the local store (FR-9).
export interface Statistics {
  total: number;
  pendingChanges: number;
  executedTests: number;
  testSets: number;
  testPlans: number;
  testExecutions: number;
  testsInSet: number;
  testsInPlan: number;
  byStatus: Bucket[];
  byPriority: Bucket[];
  byLabel: Bucket[];
  byFolder: Bucket[];
  byComponent: Bucket[];
  updatedTrend: Bucket[];
  byRunStatus: Bucket[];
}

// Import types (FR-10) mirror the testrepo shapes.
export interface ImportPreview {
  headers: string[];
  rowCount: number;
}

export interface ImportMapping {
  summary: string;
  description: string;
  priority: string;
  labels: string;
  folder: string;
  action: string;
  data: string;
  expected: string;
}

export interface ImportError {
  row: number;
  message: string;
}

export interface ImportResult {
  created: number;
  skipped: number;
  errors: ImportError[];
}

// Review mirrors testrepo.Review — a Test's review state. An empty verdict
// means it hasn't been reviewed.
export interface Review {
  verdict: string; // "approved" | "rejected" | "pending" | ""
  reviewer: string;
  note: string;
  reviewedAt: string;
}

// Traceability Sankey (FR-9): Plan -> Execution -> run-status flow.
export interface SankeyNode {
  id: string;
  label: string;
  layer: number;
  value: number;
}

export interface SankeyLink {
  source: string;
  target: string;
  value: number;
}

export interface Sankey {
  nodes: SankeyNode[];
  links: SankeyLink[];
}

// errMsg renders any thrown value (unknown in strict mode) as a string.
export function errMsg(e: unknown): string {
  if (e instanceof Error) return e.message;
  return typeof e === "string" ? e : String(e);
}
