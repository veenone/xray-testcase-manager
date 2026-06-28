# Topic 2 — Multi-Customer Versioning & Change-Request Tracking

**Date:** 2026-06-28
**Status:** Approved design (pre-implementation)
**Module:** `internal/coverage` (extension)
**Depends on:** Topic 1 (canonical requirement reuse) + Topic 3 (parameter-level coverage), already implemented.

## Context

The PRD's Topic 2 lets different customers run different *versions* of a functional
requirement (e.g. Bank locked to PKCS#11 v2.40, Samsung on v3.0) and tracks
*change requests* (CRs) and which customers accept them. The PRD assumes Jira
server administration (new projects, custom fields, Groovy automation, email
notifications, permission groups). **This deployment has no Jira admin**, so —
exactly as with Topics 1 and 3 — all Topic 2 logic is emulated **locally** in the
SQLite cache. The server-only pieces (automation rules, notifications, permission
groups) are explicitly **out of scope**; they cannot be built without admin.

Decision (confirmed): coverage is measured **per version** — each version owns its
own parameter model and coverage %, matching the PRD literally (v3.0 adds P-521
mechanism + tests that v2.40 lacks). The parameter model therefore re-roots from
the canonical requirement onto a **version**.

In-scope capabilities (all four confirmed):
1. Versions of a functional requirement (status: planning/beta/stable/deprecated).
2. Per-customer version locks (which version each member requirement is on).
3. Change requests with per-customer accept / cannot-accept / pending decisions.
4. Dashboards: version distribution + CR adoption.

## Approach

Extend the existing `internal/coverage` module in place (Approach 1). Rejected
alternatives: a separate `versioning` package (doesn't deliver per-version
coverage), and version-as-cloned-canonical (explodes the canonical list, breaks
the reuse model).

## Schema (migration to v36)

All tables profile-scoped, prefixed, additive. SQLite via modernc; migrations
follow the existing `baseSchema → applyMigrations → indexSchema` pattern.

```sql
-- Versions of a functional requirement; the parameter model now hangs off these.
CREATE TABLE canonical_version (
    profile_id   TEXT NOT NULL,
    id           TEXT NOT NULL,
    canonical_id TEXT NOT NULL,
    name         TEXT NOT NULL,                 -- "2.40", "3.0", "3.1"
    status       TEXT NOT NULL DEFAULT 'stable',-- planning|beta|stable|deprecated
    notes        TEXT NOT NULL DEFAULT '',
    sort_order   INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL,
    PRIMARY KEY (profile_id, id)
);

-- Local change request targeting a canonical (and optionally a version).
CREATE TABLE change_request (
    profile_id        TEXT NOT NULL,
    id                TEXT NOT NULL,
    canonical_id      TEXT NOT NULL,
    cr_key            TEXT NOT NULL DEFAULT '',  -- human ref e.g. "CHG-2024-0451"
    title             TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'proposed', -- proposed|reviewed|approved|implemented|released
    target_version_id TEXT NOT NULL DEFAULT '',
    risk              TEXT NOT NULL DEFAULT 'low',      -- low|medium|high
    description       TEXT NOT NULL DEFAULT '',
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (profile_id, id)
);

-- Per-customer (member requirement) decision on a CR.
CREATE TABLE cr_member_decision (
    profile_id      TEXT NOT NULL,
    cr_id           TEXT NOT NULL,
    requirement_key TEXT NOT NULL,        -- logical FK -> canonical_requirement_member
    decision        TEXT NOT NULL DEFAULT 'pending', -- can_accept|cannot_accept|pending
    note            TEXT NOT NULL DEFAULT '',
    updated_at      TEXT NOT NULL,
    PRIMARY KEY (profile_id, cr_id, requirement_key)
);
```

Altered existing tables:
- `coverage_param_group` gains `version_id TEXT` (the model now roots at a version).
- `canonical_requirement_member` gains `accepted_version_id TEXT NOT NULL DEFAULT ''`.

Migration v36 (idempotent, tolerant of "already exists"/"duplicate column"):
1. Create the three new tables.
2. `ALTER coverage_param_group ADD COLUMN version_id`.
3. `ALTER canonical_requirement_member ADD COLUMN accepted_version_id`.
4. **Backfill:** for each existing `canonical_requirement`, create a default
   `canonical_version` (name "v1", status stable) and set `version_id` on its
   groups. (Brand-new unreleased data, but the backfill keeps any dev DB working.)

Indexes (into `indexSchema`): `canonical_version(profile_id, canonical_id)`,
`change_request(profile_id, canonical_id)`, `cr_member_decision(profile_id, cr_id)`,
`cr_member_decision(profile_id, requirement_key)`, `coverage_param_group(profile_id, version_id)`.

## Module structure (`internal/coverage/`)

New files:
- **`versions.go`** — `ListVersions`, `CreateVersion`, `RenameVersion`,
  `SetVersionStatus`, `DeleteVersion`, **`CloneVersion`** (deep-copies a source
  version's groups → parameters → values, and its `coverage_value_test` mappings,
  into a new version — the "v3.0 = copy of v2.40, then tweak the delta" workflow),
  and `SetMemberVersion`.
- **`changerequest.go`** — `ListChangeRequests`, `CreateChangeRequest`,
  `UpdateChangeRequest`, `DeleteChangeRequest`, `SetCRDecision`, and `CRImpact`
  (affected members + their decisions + counts for one CR).
- **`dashboard.go`** — `VersionDistribution` (member count per version) and
  `CRAdoption` (per-CR accept/cannot-accept/pending tallies across the canonical).

Refactored (version-scoped) in existing files:
- `GetParamModel(profileID, versionID)`, `ComputeCoverage(profileID, versionID)`,
  `ListGaps(profileID, versionID)`, `ImportCoverageTemplate(profileID, versionID, data)`,
  `ExportReport(profileID, versionID)` — these now key off a version, since the
  model lives under a version.
- `UpsertNode`/`DeleteNode`: groups now carry `version_id` (a new group's
  `NodeEdit` gets `versionId` instead of `canonicalId`).
- Unchanged (canonical-scoped): canonical CRUD, members/reuse, `ListCandidateTests`,
  `DetectStaleMappings` — these belong to the functional requirement, not a version.

`testrepo` run-status helpers (`ConsolidatedRunByTest`, `DeriveCoverage`) are
reused unchanged — coverage colour logic stays single-sourced.

## `app.go` surface (`app_coverage.go`)

~15 new bound methods (thin delegators, `requireStore` + `recoverToError` on
mutators): `ListVersions`, `CreateVersion`, `CloneVersion`, `RenameVersion`,
`SetVersionStatus`, `DeleteVersion`, `SetMemberVersion`, `ListChangeRequests`,
`CreateChangeRequest`, `UpdateChangeRequest`, `DeleteChangeRequest`, `SetCRDecision`,
`GetVersionDistribution`, `GetCRAdoption`, `GetCRImpact`.

Existing coverage methods change signature to take `versionId` (breaking, but all
unreleased/uncommitted). Wails bindings regenerated.

## Frontend

`CoverageView` gains a **version selector** between the canonical list and the
matrix; selecting a version drives that version's matrix/gaps/import/export.
A new **"Versions & CRs"** tab hosts:
- Versions list — status badges, create/clone/rename/delete, and per-member
  version-lock assignment.
- Change Requests — create/edit (title, status, target version, risk), a
  per-customer decision grid (can-accept / cannot-accept / pending), and a CR
  impact summary.
- Dashboards — version-distribution bar, CR-adoption breakdown.

New components: `VersionBar.tsx`, `ChangeRequestsPanel.tsx`, `VersionDashboard.tsx`.
`CoverageView` holds the selected-version state and threads it to the matrix.

## Data flow

Select canonical → load versions → pick one (default: latest stable, else first) →
matrix/gaps/import/export operate on that version. Versions & CRs tab manages
versions, member locks, CRs, and decisions. Dashboards derive from
`canonical_requirement_member.accepted_version_id` (distribution) and
`cr_member_decision` (adoption).

## Demo alignment

Extend `SeedDemoExample` (the Login example): create two versions — "v1.0"/stable
and "v2.0"/beta (the latter via `CloneVersion` of v1.0) — lock a couple of member
requirements to each, and add one sample CR ("Add OAuth login" → v2.0) with
per-member decisions. So Topic 2 shows populated in demo mode, consistent with the
existing Login alignment.

## Testing

Go unit tests (follow existing `internal/coverage` patterns, demo-client backed):
- Version CRUD; **`CloneVersion` deep-copy correctness** (new ids, full tree +
  mappings copied, source unaffected).
- Member version-lock + `VersionDistribution` counts.
- CR CRUD + `SetCRDecision` + `CRImpact` (affected members + decision tallies).
- **Two versions of one canonical with different coverage %** (the headline
  per-version result).
- **Migration v35→v36**: default version created per existing canonical, groups
  re-rooted, idempotent on re-open.
- Demo seed asserts versions + CR present.

Verification: `go build ./...`, full `go test ./internal/...`, frontend `tsc`, and
a demo-mode click-through (load demo example → version selector shows v1.0/v2.0 →
CR + decisions visible → dashboards populate).

## Boundedness

Still one module: additions are version/CR/dashboard files + prefixed tables;
frontend gains new components + version state in `CoverageView`. No new Jira/server
dependency — fully local, no-admin. `testrepo` never imports `coverage` (no cycle).

## Out of scope (needs Jira admin — cannot build)

Jira automation rules, customer email notifications, permission/visibility groups.
Recorded for completeness; revisit only if admin access changes.

## Build approach

Implemented as a sequential core (schema + version-rooting refactor is the
dependency root and can't be parallelized), then a small swarm fan-out for the
genuinely independent slices once the schema contract is fixed: `versions.go` and
`changerequest.go` engines in parallel, then integration (app methods + bindings)
and frontend, then tests. Pilot one canonical with two versions through compute
before wiring the full UI.
