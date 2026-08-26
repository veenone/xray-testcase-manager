// The onboarding tours, as data (RND_P_4TFINT_05-335).
//
// Every step targets a `data-tour` attribute rather than a CSS class. Classes
// churn with styling work; a dedicated attribute is an explicit contract, so a
// reader can see at the element that something depends on it.
//
// CONSTRAINT: every target must be an element that is ALWAYS MOUNTED while its
// tour's view is showing. Conditionally rendered targets (the pending-changes
// badge, an open detail panel, anything inside a modal) are the main way tours
// break, because the step lands on nothing. The Browse tour explains the
// local-edit and commit ideas from stable anchors instead of spotlighting the
// widgets themselves; the per-view tours anchor on each view's own tab (always
// mounted in the topbar) plus the shared More menu, for the same reason.

export const TOUR_VERSION = 1;

export interface TourStep {
  id: string;
  /** Value of the target element's data-tour attribute. */
  target: string;
  title: string;
  body: string;
  side?: "top" | "bottom" | "left" | "right";
}

// The Browse tour: the core "sync → browse → edit → commit" loop. It is the
// first-run tour and stays inside Browse.
const BROWSE_STEPS: TourStep[] = [
  {
    id: "profile",
    target: "profile",
    title: "Your connection",
    body: "Each profile points at one Jira project. Switch here to work on a different project. The DEMO chip means you are on sample data, not a real Jira.",
    side: "bottom",
  },
  {
    id: "sync",
    target: "sync",
    title: "Pull from Jira",
    body: "Sync copies tests from Jira into a local cache on this machine. That cache is what makes browsing 10,000 tests instant. Jira stays the system of record.",
    side: "bottom",
  },
  {
    id: "views",
    target: "views",
    title: "The views",
    body: "Tests, preconditions, requirements, test plans and coverage each get their own view. Each one has its own quick tour — open it from the More menu while you're on that view.",
    side: "bottom",
  },
  {
    id: "search",
    target: "search",
    title: "Find tests fast",
    body: "Search and filters run against the local cache, so results appear as you type. Combine them with the folder tree to narrow to exactly the tests you want.",
    side: "bottom",
  },
  {
    id: "grid",
    target: "grid",
    title: "Browse and edit",
    body: "Click a test to open its details, where you can edit fields and steps. Tick the checkboxes to act on many tests at once.",
    side: "top",
  },
  {
    id: "pending",
    target: "pending",
    title: "Edits stay local until you commit",
    body: "This is the part that surprises people: your edits are not written to Jira as you make them. They collect as pending changes, shown here, and a Commit button appears once you have some. Nothing reaches Jira until you press it.",
    side: "bottom",
  },
  {
    id: "restart",
    target: "more",
    title: "That's the loop",
    body: "Sync, browse, edit, commit. Everything else builds on it. You can run this tour again any time from this menu.",
    side: "bottom",
  },
];

// A per-view tour walks the view's own tab (an always-mounted anchor in the
// topbar), then its main content area, then its primary control, then the
// shared "re-run" step. `bodyTarget`/`toolsTarget` are `data-tour` values added
// inside each view component; a step whose target isn't mounted is filtered out
// at run time, so a view that omits one degrades gracefully.
interface ViewTour {
  /** view id (matches App's `view`); also the `tab-<view>` anchor. */
  view: string;
  title: string;
  /** what the view is for — shown on its tab. */
  purpose: string;
  /** what the main content area holds — shown on `<view>-body`. */
  body: string;
  /** what the primary control does — shown on `<view>-tools`. */
  tools: string;
}

function viewTour(v: ViewTour): TourStep[] {
  return [
    {
      id: `${v.view}-intro`,
      target: `tab-${v.view}`,
      title: v.title,
      body: v.purpose,
      side: "bottom",
    },
    {
      id: `${v.view}-body`,
      target: `${v.view}-body`,
      title: "The main area",
      body: v.body,
      side: "top",
    },
    {
      id: `${v.view}-tools`,
      target: `${v.view}-tools`,
      title: "Your main control here",
      body: v.tools,
      side: "bottom",
    },
    {
      id: `${v.view}-rerun`,
      target: "more",
      title: "Run this again",
      body: "Re-open this view's tour any time from the More menu.",
      side: "bottom",
    },
  ];
}

// TOURS maps a view id (the same ids App uses for `view`) to its tour. Views
// without an entry simply have no tour.
export const TOURS: Record<string, TourStep[]> = {
  browse: BROWSE_STEPS,
  preconditions: viewTour({
    view: "preconditions",
    title: "Preconditions",
    purpose:
      "Reusable setup steps shared across tests. Browse and edit them here, and spot duplicate definitions to clean up.",
    body: "The precondition list is on the left; click one to view and edit its details on the right.",
    tools: "Filter the list by key, summary or type as you type.",
  }),
  requirements: viewTour({
    view: "requirements",
    title: "Requirements",
    purpose:
      "The requirements your tests cover. See each one's coverage status and link tests to it.",
    body: "Requirements are listed on the left; select one to see its details and linked tests on the right.",
    tools: "Filter requirements by key or summary as you type.",
  }),
  duplicates: viewTour({
    view: "duplicates",
    title: "Duplicates",
    purpose:
      "Finds tests with near-identical summaries or steps, so you can merge or remove the copies.",
    body: "Duplicate groups appear here after a scan; open a group to compare and resolve the copies.",
    tools: "Switch between finding duplicate tests and duplicate preconditions.",
  }),
  gapanalysis: viewTour({
    view: "gapanalysis",
    title: "Gap Analysis",
    purpose:
      "Requirements that no test covers yet — your coverage gaps, listed so you can close them.",
    body: "Set up the analysis at the top; the uncovered requirements appear below.",
    tools: "Run the analysis to find requirements with no covering tests.",
  }),
  testcalls: viewTour({
    view: "testcalls",
    title: "Test Calls",
    purpose:
      "Shows which tests call other tests in their steps, including cross-project calls and any cycles.",
    body: "The call relationships between your tests are shown here, grouped by caller.",
    tools: "Re-pull the latest call links to refresh the graph.",
  }),
  dashboard: viewTour({
    view: "dashboard",
    title: "Dashboard",
    purpose:
      "A live overview — test counts, status breakdowns, and a traceability Sankey.",
    body: "Summary tiles and charts for the current selection fill this panel.",
    tools: "Narrow every panel by folder, component or status here.",
  }),
  traceability: viewTour({
    view: "traceability",
    title: "Traceability",
    purpose:
      "Follow the thread from requirements to tests to executions across the project.",
    body: "Sankey diagrams tracing coverage render in this panel.",
    tools: "Switch between the Requirement, Execution and Sub-task views — and export to XLSX — here.",
  }),
  plans: viewTour({
    view: "plans",
    title: "Containers",
    purpose:
      "Test Sets, Test Plans and Test Executions — the containers that group tests. Create them and allocate tests here.",
    body: "The container board — pick a Test Set, Plan or Execution to see and manage its members.",
    tools: "Switch between the container board and the cross-container Bugs list.",
  }),
  coverage: viewTour({
    view: "coverage",
    title: "Coverage",
    purpose:
      "Map requirements to functions or components and see where coverage is reused.",
    body: "Functional requirements are on the left; select one to map coverage and see reuse on the right.",
    tools: "Add a new functional requirement to map from here.",
  }),
  misspellings: viewTour({
    view: "misspellings",
    title: "Spellcheck",
    purpose:
      "Scans summaries, descriptions and steps for spelling issues and suggests fixes you can apply in bulk.",
    body: "Spelling findings appear here after a scan, each with suggested corrections you can apply.",
    tools: "Start a spellcheck scan across all synced tests from here.",
  }),
};
