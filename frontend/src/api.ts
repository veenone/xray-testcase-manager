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
  SetRequirementLinkType,
  ListRequirementLinkTypeDetails,
  GetCapabilities,
  SetShowCoverage,
  ListProfiles,
  CreateProfile,
  CreateProfileReusingToken,
  UpdateProfile,
  SyncProfileFull,
  UpdateProfileScope,
  ExportProfile,
  ImportProfile,
  UpdateProfileToken,
  DeleteProfile,
  TestConnection,
  TestProfileConnection,
  ListConnections,
  AddConnection,
  UpdateConnection,
  DeleteConnection,
  ComputeBridgeGap,
  GetBridgeMapping,
  SaveBridgeMapping,
  PublishToTarget,
  SyncProfile,
  SyncRequirements,
  SyncContainers,
  SyncBugs,
  SyncTestCalls,
  SyncTests,
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
  CreatePreconditionDetailed,
  BulkAssociatePreconditions,
  BulkReplacePreconditions,
  ListPreconditionsWithUsage,
  ListTestsForPrecondition,
  DeletePrecondition,
  ListRequirementsWithCoverage,
  ListTestsForRequirement,
  GetTestRequirements,
  SetTestRequirements,
  BulkAssociateRequirements,
  BulkReplaceRequirements,
  EditRequirementField,
  DeleteRequirement,
  ListRequirementSources,
  SetRequirementSource,
  RemoveRequirementSource,
  GetRequirementLinks,
  SetRequirementLinks,
  GetTestContainers,
  ListContainers,
  AllocateTests,
  DeallocateTests,
  SetTestRunStatus,
  BulkSetTestRunStatus,
  LinkExistingBugToRun,
  UnlinkBugFromRun,
  SetTestRunComment,
  CreateContainerAndAllocate,
  EditContainer,
  DeleteContainer,
  SetContainerEnvironments,
  BulkEditContainers,
  SeedSampleContainers,
  CleanSampleData,
  GetContainerBoard,
  ExportPytest,
  MoveTestToFolder,
  BulkMoveToFolder,
  ListTests,
  ListMatchingKeys,
  ListComponents,
  ListStatuses,
  ListPriorities,
  ListProjectComponents,
  ListProjectFixVersions,
  PreviewImport,
  ImportTests,
  CreateTest,
  CloneTest,
  ExportTests,
  ExportImportTemplate,
  ExportSummaryTemplate,
  ExportSummaryFolderTemplate,
  ExportRequirementAudit,
  ExportTraceability,
  ExportDashboard,
  AnalyzeGap,
  CreateTestsFromGaps,
  ExportBugsWithRunHistory,
  ExportGapReport,
  CreateSavedView,
  ListSavedViews,
  DeleteSavedView,
  GetTest,
  GetTestMeta,
  EditTestField,
  DiscardPendingChange,
  DiscardAllPendingChanges,
  ResolveConflictOverride,
  ResolveConflictKeepRemote,
  ResolveConflictMerge,
  RecreateDeletedTest,
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
  AddCalledTestStep,
  ListTestCallLinks,
  CloneTestSteps,
  ReorderTestSteps,
  GetStatistics,
  GetTraceabilitySankey,
  GetSubTaskTraceability,
  GetExecutionsForPlans,
  GetProfileProjectKey,
  GetRequirementTraceability,
  ScanDuplicates,
  ScanDuplicateGroupSteps,
  ScanAllDuplicateSteps,
  ExcludeFromDuplicates,
  UnexcludeFromDuplicates,
  GetBugCreateFields,
  CreateBugForTest,
  CreateRequirement,
  GetRequirementCreateFields,
  GetBugDetail,
  ListBugsWithTests,
  ListBugsForContainer,
  GetTestBugs,
  ListTestsForBug,
  GetTestRunHistory,
  GetRunRollup,
  GetExecutionMembersWithRuns,
  AnalyzeJUnitImport,
  ApplyJUnitImport,
  AnalyzeJUnitImportNewExec,
  ApplyJUnitImportNewExec,
  AnalyzeRequirementImport,
  ExportRequirementImportTemplate,
  ImportRequirements,
  // Coverage module (parameter-level coverage + canonical requirement reuse)
  ListCanonicalRequirements,
  CreateCanonicalRequirement,
  RenameCanonicalRequirement,
  DeleteCanonicalRequirement,
  SetCanonicalMembers,
  ListCanonicalReuse,
  GetParamModel,
  UpsertCoverageNode,
  DeleteCoverageNode,
  GetCoverageReport,
  ListCoverageGaps,
  ListCoverageCandidateTests,
  ListValueTests,
  SetValueTests,
  DetectStaleCoverageMappings,
  ImportCoverageTemplate,
  ExportCoverageReport,
  DownloadCoverageTemplate,
  SeedDemoCoverageExample,
  SeedPKCS11Reference,
  SeedEUICCReference,
  // Coverage Map (project-level panel + relation Sankey + config)
  ListCoverageProjects,
  SetCoverageProjects,
  GetCoverageProjectStatus,
  GetCoverageRelationSankey,
  // Coverage publish to Xray (coverage group -> Test Set) + drift detection
  PublishCoverageGroups,
  GetCoveragePublishStatus,
  // Versioning + Change Requests (Topic 2)
  ListVersions,
  CreateVersion,
  CloneVersion,
  RenameVersion,
  SetVersionStatus,
  DeleteVersion,
  SetMemberVersion,
  ListChangeRequests,
  CreateChangeRequest,
  UpdateChangeRequest,
  DeleteChangeRequest,
  SetCRDecision,
  GetVersionDistribution,
  GetCRAdoption,
  GetCRImpact,
} from "../wailsjs/go/main/App";
export { ChangeTestType } from "../wailsjs/go/main/App";
export { EventsOn, BrowserOpenURL } from "../wailsjs/runtime/runtime";

// TypeConversion is a plain-shape mirror of testrepo.TypeConversion returned
// by ChangeTestType. We define it here as an interface (consistent with how
// TestCase, Step etc. are defined) rather than re-exporting the generated
// class, which carries a constructor/convertValues that doesn't type-check
// cleanly against plain object literals.
export interface TypeConversion {
  oldType: string;
  newType: string;
  prefilled: boolean;
  canPrefill: boolean;
}

// Coverage module data shapes (mirror internal/coverage/*.go JSON tags).
export interface CanonicalRequirement {
  id: string;
  name: string;
  category: string;
  description: string;
  createdAt: string;
  updatedAt: string;
  memberCount: number;
}

export interface ReuseRow {
  canonicalId: string;
  requirementKey: string;
  projectKey: string;
  summary: string;
  status: string;
  acceptedVersionId: string;
}

export interface ParamValue {
  id: string;
  valueLabel: string;
  valueKind: string; // value | errorcode | boundary
  errorCode: string;
  isRequired: boolean;
  notes: string;
  sortOrder: number;
}

export interface Parameter {
  id: string;
  name: string;
  kind: string;
  description: string;
  sortOrder: number;
  values: ParamValue[];
}

export interface ParamGroup {
  id: string;
  name: string;
  sortOrder: number;
  parameters: Parameter[];
}

export interface ParamModel {
  versionId: string;
  groups: ParamGroup[];
}

// NodeEdit is the upsert payload for a group/parameter/value.
export interface NodeEdit {
  kind: string; // group | parameter | value
  canonicalId?: string;
  groupId?: string;
  parameterId?: string;
  id?: string;
  name: string;
  paramKind?: string;
  valueKind?: string;
  errorCode?: string;
  isRequired?: boolean;
  notes?: string;
  sortOrder?: number;
}

export interface ValueCoverage {
  valueId: string;
  testKeys: string[];
  tested: boolean;
  runStatus: string; // UNCOVERED | NOTRUN | PASSED | FAILED
  isRequired: boolean;
}

export interface GroupCoverage {
  groupId: string;
  name: string;
  total: number;
  tested: number;
  percent: number;
}

export interface CoverageReport {
  versionId: string;
  totalValues: number;
  testedValues: number;
  percent: number;
  groups: GroupCoverage[];
  values: Record<string, ValueCoverage>;
}

export interface CoverageGap {
  groupName: string;
  paramName: string;
  valueId: string;
  valueLabel: string;
  valueKind: string;
  errorCode: string;
}

export interface CandidateTest {
  testKey: string;
  summary: string;
  status: string;
}

export interface StaleMapping {
  valueId: string;
  valueLabel: string;
  testKey: string;
}

export interface CoverageImportSummary {
  groups: number;
  parameters: number;
  values: number;
  mappedTests: number;
  skipped: number;
  warnings: string[];
}

// Coverage publish to Xray: mirrors internal/coveragepublish's ReconcileState,
// GroupStatus (DetectDrift's per-group reconcile result) and Result/GroupResult
// (PublishGroups' per-run outcome). See reconcile.go's doc comments for exactly
// what each state means before touching how these render.
export type CoveragePublishState =
  | "NotPublished"
  | "InSync"
  | "LocalChanges"
  | "Drift"
  | "Conflict";

export interface CoveragePublishGroupStatus {
  groupId: string;
  groupName: string;
  containerKey: string;
  state: CoveragePublishState;
  localAdded: string[];
  localRemoved: string[];
  remoteAdded: string[];
  remoteRemoved: string[];
}

export interface CoveragePublishGroupResult {
  groupId: string;
  groupName: string;
  containerKey: string;
  created: boolean;
  added: string[];
  removed: string[];
  error?: string;
}

export interface CoveragePublishResult {
  created: number;
  updated: number;
  failed: number;
  groups: CoveragePublishGroupResult[];
}

// ProjectConfig mirrors coverage.ProjectConfig — one in-scope project for the Coverage Map.
export interface ProjectConfig {
  projectKey: string;
  role: string; // "source" | "customer"
  label: string;
  sortOrder: number;
}

// ProjectCoverageRow mirrors coverage.ProjectCoverageRow — per-project coverage rollup.
export interface ProjectCoverageRow {
  projectKey: string;
  role: string;
  label: string;
  requirementCount: number;
  functionsReused: number;
  coveredValues: number;
  totalValues: number;
  percent: number;
}

export interface PKCSSeedSummary {
  features: number;
  requirements: number;
  tests: number;
  versions: number;
  changeRequests: number;
  mappings: number;
}

// GapTest mirrors testrepo.GapTest — one comparable test row.
export interface GapTest {
  summary: string;
  description: string;
  priority: string;
  labels: string[];
  components: string[];
  folder: string;
}

// FolderMismatch mirrors testrepo.FolderMismatch — a summary-matched test whose
// folder differs between reference and target.
export interface FolderMismatch {
  summary: string;
  referenceFolder: string;
  targetFolder: string;
}

// GapResult mirrors testrepo.GapResult — a comparison outcome.
export interface GapResult {
  referenceSource: string; // "project" | "file"
  referenceCount: number;
  targetCount: number;
  matched: number;
  missingFromReference: GapTest[];
  missingFromTarget: GapTest[];
  threeWay: boolean;
  projectCount: number;
  missingFromProject: GapTest[];
  folderMismatches: FolderMismatch[];
}

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
  requirementLinkType: string; // Jira issue-link type for Test->Requirement coverage; default "tested by"
  showCoverage: boolean; // reveal the opt-in, hidden-by-default Coverage tab
}

// Capabilities mirrors backend.Capabilities — what the active profile's
// backend supports. Xray reports the full/permissive set today (see
// xray.Adapter.Capabilities); this is used to gate backend-specific UI once a
// non-Xray backend exists. Field names/casing match the generated
// wailsjs/go/models.ts backend.Capabilities shape (camelCase, driven by the
// json tags on the Go struct), matching the hand-defined-interface convention
// used by the other domain types in this file.
export interface Capabilities {
  name: string;
  idStyle: string;
  supportsJqlScope: boolean;
  stepModel: string;
  supportsTestTypes: boolean;
  supportsFolders: boolean;
  supportsPreconditionObjects: boolean;
  supportsRequirementObjects: boolean;
  supportsIssueLinkTypes: boolean;
  supportsEnvironments: boolean;
  supportsContainers: boolean;
  containerKinds: string[];
  supportsTestRuns: boolean;
  statusModel: string;
  supportsWorkflowTransitions: boolean;
  supportsBugCreation: boolean;
  supportsBugLinks: boolean;
  supportsTags: boolean;
}

export interface Profile {
  id: string;
  name: string;
  jiraUrl: string;
  projectKey: string;
  scopeJql: string;
  bugIssueType: string;
  bugProjectMode: string; // "test" | "execution" | "dedicated"
  bugProjectKey: string;
  caCert: string;
  allowUntrustedTls: boolean;
  // backend selects which system this profile connects to: "xray" (default,
  // Jira Data Center + Xray Server/DC) or "kiwi" (Kiwi TCMS). Settable from
  // the profile form's backend selector (P6.1b).
  backend: string;
  createdAt: string;
}

// Connection mirrors connection.Connection — one backend a workspace talks
// to (P6.3 bridge plumbing). A single-connection workspace's connection has
// id == workspaceId and role "both"; the bridge (B5/B6, not yet built) adds
// a second connection with role "source" or "target".
export interface Connection {
  id: string;
  workspaceId: string;
  name: string;
  backend: string; // "xray" | "kiwi"
  url: string;
  projectKey: string;
  scopeJql: string;
  bugIssueType: string;
  bugProjectMode: string; // "test" | "execution" | "dedicated"
  bugProjectKey: string;
  caCert: string;
  allowUntrustedTls: boolean;
  role: string; // "source" | "target" | "both"
  createdAt: string;
}

// BridgeGap mirrors bridge.Gap — one way the target connection's backend
// can't fully represent something the source connection's backend supports,
// returned by ComputeBridgeGap (Phase 6 bridge task B4). Feature is a stable
// machine key; Severity is "blocking" | "lossy" | "info".
export interface BridgeGap {
  feature: string;
  severity: string;
  message: string;
}

// BridgeMapping mirrors bridge.Mapping — the reversible status/step/field
// mapping used when publishing from a source connection to a target
// connection. Returned by GetBridgeMapping (the saved mapping, or a
// bridge.DefaultMapping when none is saved) and persisted by
// SaveBridgeMapping. The publish engine that applies it (B5) and the mapping
// editor UI (B6) are later tasks — B4 only computes/persists this shape.
export interface BridgeMapping {
  statusMap: Record<string, string>;
  stepMode: string; // "flatten" | "passthrough"
  fieldMap: Record<string, string>;
  unmappedPolicy: string; // "drop" | "keepInHub"
}

// BridgePublishResult mirrors bridge.PublishResult — the outcome of
// PublishToTarget (Phase 6 bridge task B5): every hub test newly created in
// the target this run, every one skipped because it was already published
// (resumability), and every one whose publish attempt failed. Containers/
// preconditions/requirements/links are not published by this call (B5b).
export interface BridgePublishResult {
  created: BridgePublishedTest[];
  alreadyPublished: string[];
  failed: BridgePublishFailure[];
}

export interface BridgePublishedTest {
  localKey: string;
  targetKey: string;
}

export interface BridgePublishFailure {
  localKey: string;
  error: string;
  // targetKey is set when CreateTest succeeded but a downstream step/status
  // write failed: the target test WAS created (with this key) and its
  // external_ref IS recorded, so a future PublishTests run will SKIP it
  // (resumability) rather than retry the failed steps/status. Empty means
  // CreateTest itself failed — nothing was created, and a retry will
  // correctly re-attempt CreateTest for this test.
  targetKey?: string;
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
  execType: string;
  cucumberScenario?: string;
  cucumberType?: string;
  genericDefinition?: string;
  /** Jira Fix Version(s) assigned to this Test issue. Empty when none are set. */
  fixVersions: string[];
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
  execType: string;
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
  // condition is the Xray precondition definition text, distinct from the Jira
  // issue description. Empty when not set or when synced from live Jira.
  condition: string;
}

// PreconditionUsage mirrors testrepo.PreconditionUsage — a Precondition plus
// how many Tests reference it, for the dedicated management view (FR-13.4).
export interface PreconditionUsage {
  key: string;
  summary: string;
  type: string;
  description: string;
  // condition is the Xray precondition definition text, distinct from the Jira
  // issue description. Empty when not set or when synced from live Jira.
  condition: string;
  testCount: number;
}

// PreconditionTest mirrors testrepo.PreconditionTest — one Test linked to a
// Precondition, with its summary and workflow status.
export interface PreconditionTest {
  key: string;
  summary: string;
  status: string;
}

// Requirement mirrors testrepo.Requirement — a requirement issue (possibly in
// another project) covered by Tests.
export interface Requirement {
  key: string;
  projectKey: string;
  issueType: string;
  summary: string;
  status: string;
  updated: string;
  priority: string;
  components: string;
  fixVersions: string;
  sprint: string;
  description: string;
  epicKey: string;
}

// RequirementCoverage mirrors testrepo.RequirementCoverage — a requirement plus
// its derived coverage (PASSED | FAILED | NOTRUN | UNCOVERED).
export interface RequirementCoverage {
  key: string;
  projectKey: string;
  issueType: string;
  summary: string;
  status: string;
  testCount: number;
  coverage: string;
  priority: string;
  components: string;
  fixVersions: string;
  sprint: string;
  description: string;
}

// RequirementTest mirrors testrepo.RequirementTest — one Test covering a
// requirement, with its consolidated run status.
export interface RequirementTest {
  key: string;
  summary: string;
  status: string;
  runStatus: string;
}

// RequirementSource mirrors testrepo.RequirementSource — a project to browse
// requirements from.
export interface RequirementSource {
  projectKey: string;
  issueTypes: string;
  scopeJql: string;
}

// ReqReqLink mirrors testrepo.ReqReqLink — one Requirement->Requirement
// directional issue link (e.g. "requires").
export interface ReqReqLink {
  fromKey: string;
  toKey: string;
  linkType: string;
  linkId: string;
}

// Container mirrors testrepo.Container — a Test Set, Test Plan or Test
// Execution (kind = "testset" / "testplan" / "testexec").
export interface Container {
  key: string;
  kind: string;
  summary: string;
  status: string;
  parentKey: string;  // parent issue key for a sub-task Test Execution; "" for standalone
  parentSummary: string;  // parent issue summary; "" when no parent or not fetched
  issueType: string;  // Jira issuetype name (e.g. "Sub Test Execution"); informational
  environments: string[]; // Xray Test Environments (Test Executions only; empty otherwise)
  fixVersions: string[]; // Jira Fix Version(s), read-only (Test Executions only; empty otherwise)
  description: string;  // Jira issue description (plain text)
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
  // isExternal is true when the member Test lives in a different Jira project
  // (no local test_case row) and its summary/status come from the external_test
  // cache.
  isExternal: boolean;
}

// TestPlanBoard mirrors testrepo.TestPlanBoard — a Test Plan's member Tests
// with consolidated execution status, plus a run-status histogram.
export interface TestPlanBoard {
  key: string;
  summary: string;
  description: string;
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
  // Set when the step calls another test (Xray "test call") instead of holding
  // manual action/data/expected content.
  calledTestKey: string;
}

// TestCallLink mirrors testrepo.TestCallLink — one "call test" relationship
// (callerKey's step calls calledKey). calledExists is false for a dangling /
// cross-project call whose target isn't in the local cache.
export interface TestCallLink {
  callerKey: string;
  callerSummary: string;
  calledKey: string;
  calledSummary: string;
  calledExists: boolean;
  stepIndex: number;
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
// membership pass. stage is a human-readable label for the running step.
export interface SyncProgress {
  phase: string;
  fetched: number;
  total: number;
  done: boolean;
  // stage is a human-readable label for the running sync step ("Fetching tests",
  // "Mapping folder membership", "Syncing containers", …), shown in the status bar.
  stage?: string;
}

// CommitResult mirrors syncer.CommitResult — per-Test outcome of pushing
// pending changes to Jira. Succeeded / Conflicted / Failed are disjoint sets.
export interface CommitResult {
  succeeded: string[];
  conflicted: Conflict[];
  failed: FailedCommit[];
  // created maps each newly-created Test's temporary "NEW-N" key to the real
  // Jira key it was assigned (FR-1). Optional so error-path literals can omit it.
  created?: CreatedTest[];
}

// CreatedTest mirrors syncer.CreatedTest — a locally-created Test's temp key and
// the real Jira key it received on commit.
export interface CreatedTest {
  tempKey: string;
  key: string;
}

// Conflict means the remote `updated` has advanced since the user's earliest
// pending edit on that Test, and at least one field overlaps — the Test was held
// back so they can resolve. fields lists the genuinely overlapping edits (empty
// when remoteDeleted).
export interface Conflict {
  testKey: string;
  testSummary: string;
  baseVersion: string;
  remoteVersion: string;
  remoteDeleted: boolean;
  fields: ConflictField[];
}

// ConflictField is one overlapping edit shown three-way in the resolution UI.
export interface ConflictField {
  pendingId: number;
  entityType: string;
  entityKey: string;
  field: string;
  label: string;
  base: string;
  remote: string;
  mine: string;
}

// ConflictDecision is the user's per-field choice sent to ResolveConflictMerge.
export interface ConflictDecision {
  pendingId: number;
  entityType: string;
  entityKey: string;
  field: string;
  choice: "mine" | "theirs";
  remoteValue: string;
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
  byCoverage: Bucket[];
  byRequirement: Bucket[];
}

// Duplicate management (mirrors testrepo shapes).
export interface DuplicateMember {
  key: string;
  summary: string;
  description: string;
  status: string;
  folderId: string;
}

export interface DuplicateGroup {
  normalizedSummary: string;
  displaySummary: string;
  stepsVerdict: "identical" | "differ" | "unscanned";
  members: DuplicateMember[];
}

export interface DuplicateReport {
  groups: DuplicateGroup[];
  groupCount: number;
  testCount: number;
  stepsIdentical: number;
  stepsDiffer: number;
  stepsUnscanned: number;
  excluded: number;
  scannedAt: string;
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
  components: string;
  folder: string;
  action: string;
  data: string;
  expected: string;
  testType: string;
  cucumberScenario: string;
  cucumberType: string;
  genericDefinition: string;
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

// RequirementImportRow mirrors testrepo.RequirementImportRow — one parsed file
// row with its existence classification. status is "new" or "existing".
export interface RequirementImportRow {
  summary: string;
  description: string;
  priority: string;
  components: string;
  fixVersions: string;
  status: string;
}

// RequirementImportPreview mirrors testrepo.RequirementImportPreview — the
// parse + classify result shown before the user commits.
export interface RequirementImportPreview {
  rows: RequirementImportRow[];
  newCount: number;
  existingCount: number;
}

// RequirementImportResult mirrors testrepo.RequirementImportResult — the
// outcome of a completed import.
export interface RequirementImportResult {
  created: number;
  skippedExisting: number;
  errors: ImportError[];
}

// StepDraft mirrors testrepo.StepDraft — one step in the New Test form (FR-1).
export interface StepDraft {
  action: string;
  data: string;
  expected: string;
}

// TestDraft mirrors testrepo.TestDraft — the New Test form payload (FR-1).
// labels is space-separated, components comma-separated, matching import.
export interface TestDraft {
  summary: string;
  description: string;
  priority: string;
  labels: string;
  components: string;
  execType: string;
  cucumberScenario: string;
  cucumberType: string;
  genericDefinition: string;
  folderId: string;
  steps: StepDraft[];
  precondKeys: string[];
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

// BugFieldOption mirrors jira.BugFieldOption — one allowed value for a
// BugCreateField select or version field.
export interface BugFieldOption {
  id: string;
  value: string;
}

// BugCreateField mirrors jira.BugCreateField — one required field on the bug
// issue type's create screen beyond project/issuetype/summary/description/
// priority/labels. Type is: "text" | "option" | "version" | "versions" |
// "number" | "date" | "array".
export interface BugCreateField {
  id: string;
  name: string;
  required: boolean;
  type: string;
  allowedValues: BugFieldOption[];
}

// BugDetail mirrors jira.BugDetail - the extended fields for a defect issue
// fetched lazily on detail-panel open (description + three custom fields).
export interface BugDetail {
  description: string;
  defectOrigin: string;
  defectAnalysis: string;
  correctionDetails: string;
  reporter: string;
  severity: string;
}

// Bug mirrors testrepo.Bug - a cached defect issue (possibly cross-project)
// linked to Tests. Returned by ListBugsForContainer for an execution's related
// defects.
export interface Bug {
  key: string;
  projectKey: string;
  issueType: string;
  summary: string;
  status: string;
  priority: string;
  updated: string;
}

// BugWithTests mirrors testrepo.BugWithTests — a bug plus the Test keys it
// affects, for the Bugs panel.
export interface BugWithTests {
  key: string;
  projectKey: string;
  // issueType is the Jira issue type of the bug (e.g. "Bug", "Defect").
  issueType: string;
  summary: string;
  status: string;
  priority: string;
  // updated is the Jira last-updated timestamp for the bug issue.
  updated: string;
  testKeys: string[];
}

// TestBug mirrors testrepo.TestBug — a bug linked to one Test, for the
// test-detail section.
export interface TestBug {
  key: string;
  projectKey: string;
  summary: string;
  status: string;
  priority: string;
}

// BugTest mirrors testrepo.BugTest — a Test affected by a bug, with its
// consolidated run status, for the bug detail pane.
export interface BugTest {
  key: string;
  project: string;
  summary: string;
  status: string;
  runStatus: string;
}

// TestRunEntry mirrors testrepo.TestRunEntry — one execution-run of a test,
// with the execution's context (plan keys, fix versions, defects, run detail).
// createdAt and updatedAt are ISO-8601 strings from Xray (empty when unknown)
// and drive sort order (newest updated first).
export interface TestRunEntry {
  execKey: string;
  execSummary: string;
  planKeys: string[];
  environment: string;
  fixVersions: string[];
  runStatus: string;
  startedAt: string;
  finishedAt: string;
  executedBy: string;
  defects: string[];
  createdAt: string;
  updatedAt: string;
  // execIssueType is the execution's Jira issue type ("Test Execution" or "Sub
  // Test Execution"); execParentKey / execParentSummary identify a sub-task
  // execution's parent issue (empty for standalone executions).
  execIssueType: string;
  execParentKey: string;
  execParentSummary: string;
  // execCreated, execUpdated, execResolved are the Test Execution issue's
  // created, updated, and resolution timestamps (ISO-8601, empty when unknown
  // or unresolved).
  execCreated: string;
  execUpdated: string;
  execResolved: string;
}

// RunRollup mirrors testrepo.RunRollup — run-result roll-up for a Test Plan or
// Test Set across the executions that ran its member tests.
export interface RunRollup {
  passed: number;
  failed: number;
  notRun: number;
  executing: number;
  aborted: number;
  blocked: number;
  total: number;
  execCount: number;
}

// ExecMemberRun mirrors testrepo.ExecMemberRun — one member test of an
// execution enriched with run details.
export interface ExecMemberRun {
  testKey: string;
  summary: string;
  status: string;
  runStatus: string;
  startedAt: string;
  finishedAt: string;
  executedBy: string;
  environment: string;
  /** Jira Fix Version(s) of this member Test issue (from test_case), not the execution's. */
  fixVersions: string[];
  /** Bug/defect keys linked to this run result. */
  defects: string[];
  /** Free-text remark/comment on this run result. */
  comment: string;
}

// JUnitMatch mirrors testrepo.JUnitMatch -- a testcase matched to an execution member.
export interface JUnitMatch {
  testcase: string;
  testKey: string;
  summary: string;
  result: string; // "PASS" | "FAIL"
  currentRun: string;
}

// JUnitSkip mirrors testrepo.JUnitSkip -- a testcase skipped with a reason.
export interface JUnitSkip {
  testcase: string;
  reason: string;
}

// JUnitImportPreview mirrors testrepo.JUnitImportPreview -- the analysis result.
export interface JUnitImportPreview {
  execKey: string;
  total: number;
  matched: JUnitMatch[];
  skipped: JUnitSkip[];
}

// JUnitNewExecRow mirrors testrepo.JUnitNewExecRow -- one testcase row in a
// new-execution JUnit import. When Create is true the test does not yet exist
// and will be created on commit. Result is "PASS", "FAIL", or "" (skipped in
// the report; the test is allocated but the run result is left unset).
export interface JUnitNewExecRow {
  testcase: string;
  testKey: string;
  summary: string;
  result: string;
  create: boolean;
}

// JUnitNewExecPreview mirrors testrepo.JUnitNewExecPreview -- the analysis of
// a JUnit report for creating a brand-new Test Execution.
export interface JUnitNewExecPreview {
  total: number;
  rows: JUnitNewExecRow[];
  skipped: JUnitSkip[];
}

// JUnitNewExecResult mirrors testrepo.JUnitNewExecResult -- the outcome of
// ApplyJUnitImportNewExec. ExecKey is the temporary key of the new execution
// (replaced with the real Jira key on commit).
export interface JUnitNewExecResult {
  execKey: string;
  created: number;
  allocated: number;
  resultsSet: number;
  failed: string[];
}

// Version mirrors coverage.Version — one named snapshot of a canonical requirement's model.
export interface Version {
  id: string;
  name: string;
  status: string; // planning | beta | stable | deprecated
  notes: string;
  sortOrder: number;
  createdAt: string;
}

// ChangeRequest mirrors coverage.ChangeRequest — a proposed change scoped to one canonical.
export interface ChangeRequest {
  id: string;
  crKey: string;
  title: string;
  status: string; // open | accepted | rejected | withdrawn
  targetVersionId: string;
  risk: string; // low | medium | high
  description: string;
  createdAt: string;
  updatedAt: string;
}

// CRDecision mirrors coverage.CRDecision — one member requirement's decision on a CR.
export interface CRDecision {
  requirementKey: string;
  projectKey: string;
  decision: string; // pending | can_accept | cannot_accept
  note: string;
}

// CRImpactResult mirrors coverage.CRImpactResult — the full impact picture for one CR.
export interface CRImpactResult {
  cr: ChangeRequest;
  decisions: CRDecision[];
  canAccept: number;
  cannotAccept: number;
  pending: number;
}

// VersionShare mirrors coverage.VersionShare — member count per version for the distribution chart.
export interface VersionShare {
  versionId: string;
  versionName: string;
  status: string;
  memberCount: number;
}

// CRShare mirrors coverage.CRShare — adoption rollup per CR for the dashboard.
export interface CRShare {
  crId: string;
  title: string;
  status: string;
  canAccept: number;
  cannotAccept: number;
  pending: number;
}

// errMsg renders any thrown value (unknown in strict mode) as a string.
export function errMsg(e: unknown): string {
  if (e instanceof Error) return e.message;
  return typeof e === "string" ? e : String(e);
}

// isDemoUrl reports whether a profile's Jira URL selects demo mode: "demo", a
// "demo:" / "mock:" prefix, or a "demo-" variant like "demo-pkcs" that picks a
// built-in dataset. Also matches "kiwi-demo" (and its "kiwi-demo:"/"kiwi-demo-"
// variants) — the offline Kiwi demo (internal/backend/kiwi/demo.go), so the
// DEMO chip shows for a kiwi-demo profile too. The single source of truth for
// the frontend — keep in sync with isDemoURL in the Go backend
// (internal/jira/demo.go) and the validation in ProfileForm.tsx.
export function isDemoUrl(url?: string): boolean {
  return /^(demo|demo[-:].*|mock:.*|kiwi-demo|kiwi-demo[-:].*)$/i.test((url ?? "").trim());
}

// isKiwiDemoUrl reports whether a profile's Jira URL selects the offline Kiwi
// demo specifically (as opposed to the Xray demo). Kept separate from
// demoVariant, whose return type is pinned to the Xray-demo theme names
// ("pkcs" | "euicc" | "") consumed by CoverageMap/CoverageView.
export function isKiwiDemoUrl(url?: string): boolean {
  return /^(kiwi-demo|kiwi-demo[-:].*)$/i.test((url ?? "").trim());
}

// demoVariant returns the named demo dataset variant embedded in a profile's
// Jira URL. Returns "pkcs" for demo-pkcs (or demo-pkcs:...), "euicc" for
// demo-euicc (or demo-euicc:...), or "" for plain demo / non-demo profiles.
// Mirrors demoVariant in the Go backend (internal/jira/demo_theme.go).
export function demoVariant(url?: string): "pkcs" | "euicc" | "" {
  const s = (url ?? "").trim().toLowerCase();
  if (s === "demo-pkcs" || s.startsWith("demo-pkcs:")) return "pkcs";
  if (s === "demo-euicc" || s.startsWith("demo-euicc:")) return "euicc";
  return "";
}
