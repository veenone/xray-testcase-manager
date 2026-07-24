# Design: In-app ASPICE assessment of the eUICC demo dataset

**Date:** 2026-07-24
**Status:** Approved (brainstorming), pending spec review

## Context

The `demo-euicc` dataset models seven GSMA RSP procedures with a
requirement→test→coverage spine. A question arose: *does that dataset satisfy
Automotive SPICE (ASPICE)?* Analysis showed it maps only partially — the eUICC
demo carries the SYS.2/SYS.5-style evidence ASPICE inspects (system
requirements, qualification tests, bidirectional traceability, change-request
management), but lacks software-tier requirements, unit/integration test
levels, explicit verification criteria, and consistency reporting.

This feature makes that assessment **live in the app**: a `demo-euicc` profile
gains an "Assess against ASPICE" action that overlays the seven ASPICE
processes as coverage canonicals, judged against the actual eUICC data, so the
existing coverage view renders a faithful ASPICE gap picture.

The verdict is a **curated assessment** (chosen over data-computed or
mechanical round-robin): each ASPICE Base Practice is pre-classified as
satisfied-by-eUICC or a gap, encoding the analysis. Satisfied practices link to
real eUICC test cases as clickable evidence; the rest are gaps. The verdict is
authored, not recomputed — appropriate because "does the dataset contain a
software-unit-test level?" is an assessment judgment, not a value the store can
introspect.

## Goal / non-goals

**Goal:** On a synced `demo-euicc` profile, one action seeds seven
ASPICE-process canonicals whose per-process coverage and gap list reflect the
real eUICC-vs-ASPICE assessment, reusing the entire existing coverage
machinery (canonicals, versions, parameter model, value→test mapping, gap
reporting, coverage roll-up).

**Non-goals:** No new coverage UI (reuses the Coverage view). No version
cloning, change requests, or customer decisions (this is an assessment
snapshot, not a variant negotiation). Not generalised beyond eUICC. No schema
change.

## Architecture

A single lens overlaid on existing eUICC data. Two coexisting canonical sets
on the same profile: the RSP-procedure canonicals (from `SeedEUICCReference`)
and the ASPICE-process canonicals (from this feature), distinguished by
category tag. The coverage view already lists canonicals; both sets appear
side by side.

### Components

1. **`internal/coverage/aspiceassessment.go`** (new, ~180 lines):
   - `ASPICEAssessmentSummary` — `{Processes, Members, Tests, Mappings, Gaps}`:
     processes seeded, total member links, eUICC candidate tests found, BP
     value→test mappings created (satisfied), unmapped required values (gaps).
   - `euiccASPICESatisfied` — the curated verdict: `map[processCode][]string`
     listing the Base-Practice value labels the eUICC dataset satisfies. This
     is the one authored artifact; everything else is mechanical. Every label
     in the map MUST exist as a value in that process's `aspiceFeatures()`
     group (guarded by a test).
   - `SeedEUICCASPICEAssessment(profileID)` — clears prior ASPICE canonicals
     (idempotent), then for each process seeds one canonical.
   - `seedOneASPICEProcess(...)` — creates the canonical (category `ASPICE`),
     one version (`ASPICE 3.1 assessment`), the Base-Practice parameter model
     from `aspiceFeatures()`; sets members = all eUICC customer requirements
     (every `CUST-*` row — each ASPICE process is assessed against the whole
     eUICC requirement set); maps each satisfied BP value to a real eUICC
     `test_case` row (round-robin over the eUICC test pool) and leaves the rest
     unmapped.

2. **BP catalog reuse.** The seven processes and their Base-Practice values come
   from the existing `aspiceFeatures()` in `internal/coverage/demoaspice.go`
   (from PR #62). The seeder **ignores** that catalog's generic `vg/ecg` gap
   markers and consults `euiccASPICESatisfied` instead — so the ASPICE content
   lives in one place and the eUICC verdict lives in one place.

3. **`app_coverage.go`** — `SeedEUICCASPICEAssessment(profileID)
   (coverage.ASPICEAssessmentSummary, error)` with the standard
   `recoverToError` + `requireStore` wrapping, delegating to `a.cov`.

4. **Frontend** — a second action button "Assess against ASPICE" in the
   Coverage view, gated on `demoVariant === "euicc"`, next to "Load eUICC
   coverage". Calls the new binding, then reloads the canonical list. Wails
   bindings regenerate (`SeedEUICCASPICEAssessment` +
   `ASPICEAssessmentSummary`).

### Data flow

```
demo-euicc profile (synced)
  requirement rows: FUNC-EUICC-*, CUST-{MNO,IOT,M2M}-*
  test_case rows:   eUICC procedure tests
        │
        ▼  "Assess against ASPICE"  (SeedEUICCASPICEAssessment)
  for each ASPICE process P (SYS.2 … SUP.10):
    canonical(P, category=ASPICE)
      members  = eUICC CUST-* requirements
      version  = "ASPICE 3.1 assessment"
      model    = aspiceFeatures()[P].groups            (BP catalog)
      for each BP value v in the model:
        if v.label ∈ euiccASPICESatisfied[P.code]:
          map v → one eUICC test_case (round-robin)    → covered (evidence)
        else:
          leave v unmapped                             → gap
        │
        ▼
  Coverage view: per-process coverage % = satisfied ÷ total BPs;
  gap list = the ASPICE practices the eUICC dataset does not satisfy.
```

### The curated verdict (indicative shape)

Coverage per process reflects the assessment — high where eUICC has evidence,
zero where it structurally lacks a level:

| Process | Indicative coverage | Satisfied → evidence | Gaps |
|---|---|---|---|
| SYS.2 System Requirements Analysis | ~70% | requirements specified/structured; bidirectional trace to test | verification criteria per req; consistency ensured |
| SYS.5 System Qualification Test | ~65% | test cases specified; req↔test trace; system tested | test strategy; test-result-per-case; results summarized |
| SUP.10 Change Request Management | ~85% | CRs recorded; impact & decision; unique record | approve-before-implement gate; closure trace |
| SUP.9 Problem Resolution Management | ~50% | problems recorded; problem↔change trace | RCA taxonomy; corrective-action verification; closure |
| SYS/SWE traceability rules | partial | req↔test both ways | consistency, orphan detection |
| SWE.1 Software Requirements Analysis | ~15% | — | no software-requirement tier |
| SWE.6 Software Qualification Test | ~10% | — | no distinct SW qualification level |
| SWE.4 Software Unit Verification | 0% | — | no unit tests in the dataset |

The exact satisfied-set per process is authored in `euiccASPICESatisfied` and
reviewed as the deliverable's substance.

## Error handling

- Seeder wrapped by `recoverToError`; `requireStore` guards a nil store.
- If the eUICC test pool is empty (profile not synced), satisfied BPs get no
  evidence mapping and the process shows 0% — same graceful degradation as
  `SeedEUICCReference`. The button's notice tells the user to sync first.
- Idempotent: re-running clears prior ASPICE canonicals (by name match) before
  re-seeding, so no duplicates.

## Testing

New `internal/coverage/aspiceassessment_test.go`:
- Seed the eUICC sync fixture (reuse `seedEUICCSync`), run
  `SeedEUICCASPICEAssessment`.
- Assert 7 ASPICE canonicals created; each has 1 version and members = all
  eUICC customer requirements (every `CUST-*` row).
- Assert the **verdict shape**: SWE.4 coverage = 0% (no satisfied BPs);
  SUP.10 coverage is the highest; a process with a partial set is strictly
  between 0 and 100.
- Assert every satisfied BP maps to a real eUICC `test_case` (evidence
  resolves); `DetectStaleMappings` = 0.
- Assert every label in `euiccASPICESatisfied` exists in the corresponding
  `aspiceFeatures()` process model (guards the authored map against drift).
- Idempotent re-seed: same canonical count, no duplicates.

Verification: `go build ./...`, `go vet`, `gofmt` (LF-normalized on added
content), full `go test ./...`, frontend `tsc` + `vite build`.

## Dependencies & branching

Reuses `aspiceFeatures()` from PR #62 (`feature/demo-aspice`), not yet merged.
This work is branched off `feature/demo-aspice` (stacked). The PR targets
`main`; once #62 merges, rebase onto `main` so the PR shows only this feature's
diff.

## Open risks

- **Authored verdict, not computed.** The covered/gap split is a human
  assessment; the tool renders it but cannot prove it. Acceptable and stated —
  it mirrors how a real ASPICE assessor rates evidence.
- **Illustrative evidence.** A given eUICC test standing in as evidence for a
  Base Practice is representative, not a literal per-BP proof — same
  "assertion, not proof" caveat the coverage module already carries.
