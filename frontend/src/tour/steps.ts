// The onboarding tour, as data (RND_P_4TFINT_05-335).
//
// Every step targets a `data-tour` attribute rather than a CSS class. Classes
// churn with styling work; a dedicated attribute is an explicit contract, so a
// reader can see at the element that something depends on it.
//
// CONSTRAINT: every target must be an element that is ALWAYS MOUNTED while the
// Browse view is showing. Conditionally rendered targets (the pending-changes
// badge, an open detail panel, anything inside a modal) are the main way tours
// break, because the step lands on nothing. That is why the local-edit and
// commit ideas are explained from stable anchors instead of being spotlighted
// on the widgets themselves, and why this release stays inside the core loop
// rather than driving navigation between views.

export const TOUR_VERSION = 1;

export interface TourStep {
  id: string;
  /** Value of the target element's data-tour attribute. */
  target: string;
  title: string;
  body: string;
  side?: "top" | "bottom" | "left" | "right";
}

export const TOUR_STEPS: TourStep[] = [
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
    body: "Tests, preconditions, requirements, test plans and coverage each get their own view. This tour stays in Browse.",
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
