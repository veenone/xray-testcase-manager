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

// viewTour builds a short tour for one non-Browse view: an intro anchored on
// the view's own tab (a stable, always-mounted element) plus a shared "re-run"
// step. Deeper in-view spotlighting can be layered on per view later.
function viewTour(view: string, title: string, purpose: string): TourStep[] {
  return [
    {
      id: `${view}-intro`,
      target: `tab-${view}`,
      title,
      body: purpose,
      side: "bottom",
    },
    {
      id: `${view}-rerun`,
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
  preconditions: viewTour(
    "preconditions",
    "Preconditions",
    "Reusable setup steps shared across tests. Browse and edit them here, and spot duplicate definitions to clean up.",
  ),
  requirements: viewTour(
    "requirements",
    "Requirements",
    "The requirements your tests cover. See each one's coverage status and link tests to it.",
  ),
  duplicates: viewTour(
    "duplicates",
    "Duplicates",
    "Finds tests with near-identical summaries or steps, so you can merge or remove the copies.",
  ),
  gapanalysis: viewTour(
    "gapanalysis",
    "Gap Analysis",
    "Requirements that no test covers yet — your coverage gaps, listed so you can close them.",
  ),
  testcalls: viewTour(
    "testcalls",
    "Test Calls",
    "Shows which tests call other tests in their steps, including cross-project calls and any cycles.",
  ),
  dashboard: viewTour(
    "dashboard",
    "Dashboard",
    "A live overview — test counts, status breakdowns, and a traceability Sankey.",
  ),
  traceability: viewTour(
    "traceability",
    "Traceability",
    "Follow the thread from requirements to tests to executions across the project.",
  ),
  plans: viewTour(
    "plans",
    "Containers",
    "Test Sets, Test Plans and Test Executions — the containers that group tests. Create them and allocate tests here.",
  ),
  coverage: viewTour(
    "coverage",
    "Coverage",
    "Map requirements to functions or components and see where coverage is reused.",
  ),
  misspellings: viewTour(
    "misspellings",
    "Spellcheck",
    "Scans summaries, descriptions and steps for spelling issues and suggests fixes you can apply in bulk.",
  ),
};
