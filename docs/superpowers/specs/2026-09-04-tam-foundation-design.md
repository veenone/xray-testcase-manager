# Task Activity Manager: foundation design

**Status:** proposed · **Date:** 2026-09-04
**Mockup:** [`assets/2026-09-04-tam-shell-backlog.svg`](assets/2026-09-04-tam-shell-backlog.svg), the TAM shell with the Backlog view and the issue detail panel open.

## 1. What we're building

Task Activity Manager (TAM) is a spin-off of Xray Test Manager (XTM) aimed at agile task work rather than test management. It is for scrum masters, product owners, and team members who need to manage Jira tasks, epics, stories, bugs, and requirements; run sprints and kanban boards; track burndown; keep ritual notes in Confluence; and report on progress. It stays a local-first Windows desktop app on the same Go, Wails, and React stack, talking to the same Jira DC.

The longer-term goal is a suite: TAM and XTM sharing one codebase spine, one set of connection profiles, and traceability links between stories or requirements and tests.

This document covers the foundation only: the repository shape, what becomes shared code, the issue data model, the write model, and the seams that let the two apps grow into a suite. Each feature subsystem (issues, hierarchy, boards, reports, Confluence) gets its own spec on top of this one.

## 2. Decisions

| Decision | Choice | Why |
|---|---|---|
| How TAM relates to XTM in code | One repo, Go workspace, shared `core` module extracted incrementally | Gives the suite structure from day one without a big-bang refactor of the working XTM. Reuse stays real because nothing lands in `core` until an app needs it. |
| Write model | Hybrid: pending-change journal for bulk work, live write-through for board actions | Bulk edits and Excel import want review-then-commit; a card moved on a board has to be visible to the team right away. |
| First subsystem after foundation | Issue CRUD, Excel import, cross-project links, requirements | Everything else is a view over issues. This proves sync and the hybrid write model on real data first. |
| Confluence depth | Link pages to rituals and render them read-only in the app | Useful, bounded, and no risk of overwriting team docs. Write support can come later. |
| What "suite" means | Shared profiles and credentials; story/requirement to test cross-links; a unified launcher later | The first two fall out of the shared core. The launcher only needs both apps to render inside the same shell. |
| Requirements | The same Jira issue type XTM links tests to | One requirement concept, two views. TAM owns its lifecycle; XTM shows coverage against it. |

## 3. Repository layout

```
agile-suite/                   holds both apps
  go.work
  core/                        Go module: the shared spine
    jira/        Jira DC REST client (/rest/api/2/) and the agile client (/rest/agile/1.0/)
    store/       SQLite open, ordered migrations, schemaVersion machinery
    journal/     pending_change + audit_log, commit orchestration, base_version conflict check
    profile/     connection profiles + Windows Credential Manager
    settings/    global preferences (default profile, theme, requirement link type)
    backend/     the backend.Backend interface + capabilities
    demo/        deterministic offline generators (issues, sprints, boards, tests)
  xtm/                         Go module: Xray Test Manager, moved in as-is
  tam/                         Go module: Task Activity Manager, a new Wails app
  frontend/
    core/        npm workspace package: contexts, query-key helpers, app shell, dialogs, theme
    xtm/         XTM's React app
    tam/         TAM's React app
```

Extraction is pull-based. XTM moves into the repo untouched. A `core` package is created only when TAM needs it: lift XTM's implementation, point both apps at it, and land it as one reviewed PR with XTM's full test suite proving nothing moved. `core` never holds speculative code.

Each app keeps its own SQLite file. Only the profile store is shared, so a Jira connection set up once works in both apps and the two schemas cannot collide.

## 4. Shared vs. app-specific

| Shared in `core` | Stays in `xtm` | New in `tam` |
|---|---|---|
| Jira REST client: auth, issue search/get/create/update, links, transitions | Xray client (`/rest/raven/2.0/`) | Agile client: boards, sprints, backlog, rank |
| Store infrastructure and the migration pattern | Test/step/precondition/container tables | issue, issue_link, board, sprint, ritual_doc tables |
| Journal: `pending_change`, `audit_log`, group-by-entity commit, base_version check | The Test-specific commit pass (fields, then transitions, then step CRUD) | The issue-specific commit pass |
| Profiles + credentials, settings, backend interface, demo framework | Xray and Kiwi backends | The Jira-agile backend |
| Frontend contexts, query layer, dialog/modal/nav shell, theme | Test views | Issue, board, report, ritual views |

The XTM data-layer and context-decomposition work already done (the TanStack Query hooks and the `DialogProvider`, `ProfileContext`, `SyncContext`, `SelectionContext`, `NavContext`, and `ModalContext` providers) is the frontend spine that moves into `frontend/core`. That work was the prerequisite for this split.

## 5. TAM data model

All tables carry `profile_id` and are scoped to the active profile, as in XTM.

- `issue`: `key`, `id`, `project`, `type` (task | epic | story | bug | requirement), `summary`, `description`, `status`, `assignee`, `reporter`, `priority`, `labels`, `sprint_id`, `parent_key` (epic link or parent), `story_points`, `rank`, remote `created` and `updated`, `synced_at`. Custom fields load lazily when the detail panel opens. XTM does the same for test steps, and for the same reason: fetching them per issue during the bulk sync would mean thousands of extra round trips.
- `issue_link`: `from_key`, `to_key`, `link_type`. Carries cross-project links, epic to story, story to task, and the requirement-to-test seam.
- `board`: `id`, `name`, `type` (scrum | kanban), `project`, and the column-to-status mapping.
- `sprint`: `id`, `board_id`, `name`, `state` (future | active | closed), `start`, `end`, `complete`, `goal`.
- `ritual_doc`: entity (sprint or board), `ritual` (planning | standup | review | retro), Confluence `page_id`, `url`, `title`.
- `pending_change` and `audit_log` come from `core/journal`, unchanged.

Requirements are the Jira issue type XTM already covers. TAM owns their lifecycle (create, edit, prioritize); XTM shows which tests cover them. The link type between a requirement and a test ("tested by" today) becomes one shared setting in `core/settings` so both apps agree.

## 6. Write model

There are two paths and one journal.

On the journal path, Excel import, bulk edit, bulk link, and the create/edit forms write to `pending_change`, then push on Commit through XTM's engine with the base_version conflict check. This is the existing XTM machinery, unchanged.

On the live path, moving a card, changing status, re-ranking, and adding to or removing from a sprint call Jira immediately. On success the cached row is updated and an `audit_log` entry is written. On failure the error is shown and nothing changes locally.

The coherence rule that keeps the two honest: a live write to an issue refreshes `base_version` on any pending rows for that issue, so a later Commit does not report a false conflict against a change the user made live moments ago. Live actions never discard pending edits silently.

## 7. Suite seams

`core/profile` is the single profile store. Both apps read it and the Windows Credential Manager entries behind it, which is what "set up Jira once, use it in both" means in practice.

Cross-links need no new data. A story or requirement's detail panel lists the tests linked to it (the "Covered by tests" section in the mockup), and XTM's test detail shows the linked story. Both are views over Jira issue links using the shared link-type setting.

For the launcher, both apps render inside the same `frontend/core` shell: topbar, profile picker, nav rail, status bar, and dialog root. A future `suite.exe` hosting both as modules then becomes a packaging job rather than a rewrite. The nav rail in the mockup already reserves a "Suite" section for it.

## 8. Frontend

TAM reuses the six contexts, the query-key and invalidation helpers, and the shell. Its own views are:

- Backlog, the issue grid descended from XTM's `TestTable`. It filters by type, sprint, and text; supports bulk select, import, and new issue; and marks rows with a pending-change dot the way XTM's grid does.
- Issue detail, with fields, links, covered-by-tests, and activity. "Save edit" journals the change; "Move to sprint" writes live. The mockup's footer note spells out that split so users are not surprised.
- Epics, the epic to story to task tree.
- Boards, kanban and the active sprint, where dragging a card is a live write.
- Reports: burndown, velocity, and sprint and kanban analytics, reusing XTM's dashboard and Sankey patterns.
- Rituals, the Confluence pages attached to sprints and boards, rendered read-only.

The mockup shows the shell with Backlog open. Boards, Reports, and Rituals get their own mockups in their subsystem specs.

## 9. Phasing

Each phase is its own spec, plan, and build, and `main` stays shippable throughout.

- Phase 0, Foundation: monorepo and `go.work`; move XTM; scaffold TAM (Wails app, shell, demo profile); extract `core/profile`, `core/settings`, `core/store`, `core/jira`, `core/journal`, `core/backend`, and `frontend/core` as TAM reaches for each.
- Phase 1, Issues: sync by project (JQL), Backlog grid, detail panel, create and edit via the journal, Excel import (XTM's importer), cross-project links, requirements.
- Phase 2, Epic and story hierarchy: tree view, epic and story management.
- Phase 3, Boards and sprints: agile client, sprint management, kanban with live drag.
- Phase 4, Burndown and analytics: sprint and kanban reports.
- Phase 5, Confluence: link and read ritual docs.
- Phase 6, Suite: cross-link views in both apps, then the launcher.

## 10. Verification

Go unit tests per `core` and `tam` package run against the store and the demo generator, which is XTM's proven pattern. TAM gets a demo mode (`Jira URL = demo`) with deterministic issues, sprints, and boards so the whole UI runs offline, matching XTM. Vitest covers the shared contexts and hooks in `frontend/core`. Every `core` extraction PR must keep XTM's full Go and frontend suites green; that is the proof nothing moved.

## 11. Risks

| Risk | Mitigation |
|---|---|
| Extracting `core` destabilises XTM | Pull-based, one package per PR, XTM's suites as the gate. XTM is never edited for TAM's sake beyond import paths. |
| The two write paths drift apart | The coherence rule in §6 plus a single audit log. The issue commit pass is the only place that pushes journaled changes. |
| Jira Agile API shapes vary by instance | Same discipline as XTM's `NOTE(xtm)` markers: mark unverified shapes and verify against a real instance. |
| Requirement link type mismatch between apps | One shared setting in `core/settings`, read by both. |
| Scope creep across 11 feature areas | Strict phasing; each subsystem is its own spec. |

## 12. Out of scope for the foundation

Board UI, reports, Confluence rendering, the launcher, and any Kiwi or other-backend support for TAM. These are later phases or later decisions.
