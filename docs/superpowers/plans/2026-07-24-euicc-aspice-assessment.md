# eUICC ASPICE Assessment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an "Assess against ASPICE" action to `demo-euicc` profiles that overlays the seven ASPICE processes as coverage canonicals judged against the actual eUICC data, so the Coverage view renders a faithful ASPICE gap picture.

**Architecture:** A new Go seeder (`internal/coverage/aspiceassessment.go`) reuses the ASPICE Base-Practice catalog `aspiceFeatures()` (from `demoaspice.go`) but consults a curated verdict map `euiccASPICESatisfied` instead of the catalog's generic gap markers. For each process it creates one canonical (category `ASPICE`), one version, members = all eUICC `CUST-*` requirements, and maps each *satisfied* Base Practice to a real eUICC `test_case` (evidence) while leaving the rest as gaps. A thin app binding and a second Coverage-view button expose it.

**Tech Stack:** Go 1.25, `modernc.org/sqlite` (raw SQL), Wails v2 bindings, React 19 / TypeScript.

## Global Constraints

- No `schemaVersion` bump — additive demo/assessment data only; no new tables or columns.
- Reuse the coverage machinery verbatim: `CreateCanonical`, `SetMembers`, `CreateVersion`, `UpsertNode`, `SetValueTests`, plus `placeholders`/`toAny` and `deletePriorASPICECanonicals` — all already in `internal/coverage/`.
- Reuse the BP catalog `aspiceFeatures()` from `internal/coverage/demoaspice.go`; do NOT duplicate ASPICE process/BP text. The eUICC verdict lives ONLY in `euiccASPICESatisfied`.
- Canonical category string is `"ASPICE"`; canonical names are the process `fn` strings (e.g. `"SYS.2 System Requirements Analysis"`).
- Every label in `euiccASPICESatisfied` MUST be an existing value label in that process's `aspiceFeatures()` model (guarded by a test).
- No `Co-Authored-By` / AI-attribution trailers in commits or PR.
- gofmt: repo Go files are CRLF; verify only that *added* content is gofmt-clean via `tr -d '\r' < file | gofmt -d` (never run `gofmt -w`, which rewrites the whole file to LF).
- Branch: `feature/euicc-aspice-assessment`, stacked on `feature/demo-aspice` (which carries `aspiceFeatures()`, PR #62, not yet merged).

---

### Task 1: Curated verdict + Go seeder

**Files:**
- Create: `internal/coverage/aspiceassessment.go`
- Test: `internal/coverage/aspiceassessment_test.go`

**Interfaces:**
- Consumes (from `demoaspice.go`, same package): `aspiceFeatures() []pkcsFeature`; the `pkcsFeature{code,fn,summary,groups}` / `pkcsGroup{name,vals}` / `pkcsValue{label,kind,errCode,gap}` types; `deletePriorASPICECanonicals(profileID) error`; `placeholders(int) string`; `toAny([]string) []any`.
- Consumes (Module methods, `internal/coverage/`): `CreateCanonical(profileID,name,category,summary) (string,error)`, `SetMembers(profileID,canonicalID,[]string) error`, `CreateVersion(profileID,canonicalID,name,channel,note) (string,error)`, `UpsertNode(profileID, NodeEdit) (string,error)`, `SetValueTests(profileID,valueID,[]string) error`, `ComputeCoverage`, `ListCanonical`, `ListVersions`, `DetectStaleMappings` (for the test).
- Produces: `func (m *Module) SeedEUICCASPICEAssessment(profileID string) (ASPICEAssessmentSummary, error)` and `type ASPICEAssessmentSummary struct { Processes, Members, Tests, Mappings, Gaps int }` (json tags `processes/members/tests/mappings/gaps`).

- [ ] **Step 1: Write the failing test**

Create `internal/coverage/aspiceassessment_test.go`. Reuses `seedEUICCSync` (defined in `demoeuicc_test.go`, same package) to lay down eUICC-synced rows.

```go
package coverage

import "testing"

// coverageByCanonicalName returns the coverage percent of the first version of
// the canonical with the given name (assessment canonicals have one version).
func coverageByCanonicalName(t *testing.T, m *Module, profileID, name string) float64 {
	t.Helper()
	canons, _ := m.ListCanonical(profileID)
	for _, c := range canons {
		if c.Name != name {
			continue
		}
		vers, _ := m.ListVersions(profileID, c.ID)
		if len(vers) != 1 {
			t.Fatalf("%s versions = %d, want 1", name, len(vers))
		}
		rep, err := m.ComputeCoverage(profileID, vers[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		return rep.Percent
	}
	t.Fatalf("canonical %q not found", name)
	return 0
}

func TestSeedEUICCASPICEAssessment(t *testing.T) {
	m, _ := newTestModule(t)
	const p = "p1"

	// Stand in for a demo-euicc sync (reused from demoeuicc_test.go).
	seedEUICCSync(t, m, p)

	sum, err := m.SeedEUICCASPICEAssessment(p)
	if err != nil {
		t.Fatalf("assess: %v", err)
	}
	t.Logf("summary: %+v", sum)

	// Seven ASPICE processes seeded; 42 eUICC candidate tests found.
	if sum.Processes != 7 {
		t.Errorf("processes = %d, want 7", sum.Processes)
	}
	if sum.Tests != 42 {
		t.Errorf("tests = %d, want 42 (eUICC candidate pool)", sum.Tests)
	}
	// mappings + gaps must equal the total required values across the 7 models
	// (14+14+13+12+14+13+12 = 92).
	if sum.Mappings+sum.Gaps != 92 {
		t.Errorf("mappings+gaps = %d, want 92 (all required values)", sum.Mappings+sum.Gaps)
	}
	if sum.Mappings == 0 || sum.Gaps == 0 {
		t.Errorf("want a partial assessment (some mapped, some gaps); got %d/%d", sum.Mappings, sum.Gaps)
	}

	// Seven canonicals, each with members = all eUICC CUST-* reqs (21).
	canons, _ := m.ListCanonical(p)
	if len(canons) != 7 {
		t.Fatalf("canonicals = %d, want 7", len(canons))
	}
	for _, c := range canons {
		if c.MemberCount != 21 {
			t.Errorf("%s members = %d, want 21 (all eUICC CUST-* reqs)", c.Name, c.MemberCount)
		}
	}

	// Verdict shape: SWE.4 has no eUICC evidence (0%), SUP.10 is the strongest,
	// and a partial process sits strictly between.
	swe4 := coverageByCanonicalName(t, m, p, "SWE.4 Software Unit Verification")
	if swe4 != 0 {
		t.Errorf("SWE.4 coverage = %v%%, want 0 (no unit tests in eUICC)", swe4)
	}
	sup10 := coverageByCanonicalName(t, m, p, "SUP.10 Change Request Management")
	sys2 := coverageByCanonicalName(t, m, p, "SYS.2 System Requirements Analysis")
	if !(sup10 > sys2 && sys2 > 0 && sup10 < 100) {
		t.Errorf("verdict shape wrong: SUP.10=%v SYS.2=%v (want SUP.10 > SYS.2 > 0, SUP.10 < 100)", sup10, sys2)
	}

	// Every satisfied label must be a real value in that process's model.
	catalog := map[string]map[string]bool{}
	for _, f := range aspiceFeatures() {
		labels := map[string]bool{}
		for _, g := range f.groups {
			for _, v := range g.vals {
				labels[v.label] = true
			}
		}
		catalog[f.code] = labels
	}
	for code, sat := range euiccASPICESatisfied {
		if catalog[code] == nil {
			t.Errorf("euiccASPICESatisfied has unknown process code %q", code)
			continue
		}
		for _, label := range sat {
			if !catalog[code][label] {
				t.Errorf("satisfied label %q not a value in process %q", label, code)
			}
		}
	}

	// Every mapped evidence test is a real eUICC test_case (no stale mappings).
	stale, err := m.DetectStaleMappings(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Errorf("stale mappings = %d, want 0\n%+v", len(stale), stale)
	}

	// Idempotent: re-assess produces the same shape, no duplicate canonicals.
	sum2, err := m.SeedEUICCASPICEAssessment(p)
	if err != nil {
		t.Fatalf("re-assess: %v", err)
	}
	if sum2.Processes != 7 || sum2.Mappings+sum2.Gaps != 92 {
		t.Errorf("re-assess not idempotent: %+v", sum2)
	}
	if canons2, _ := m.ListCanonical(p); len(canons2) != 7 {
		t.Errorf("after re-assess, canonicals = %d, want 7 (no duplicates)", len(canons2))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/coverage/ -run TestSeedEUICCASPICEAssessment -count=1`
Expected: FAIL — `undefined: SeedEUICCASPICEAssessment` / `undefined: euiccASPICESatisfied`.

- [ ] **Step 3: Write the implementation**

Create `internal/coverage/aspiceassessment.go`:

```go
package coverage

import "fmt"

// SeedEUICCASPICEAssessment overlays the seven Automotive SPICE processes as
// coverage canonicals judged against a synced demo-euicc dataset, producing an
// in-app ASPICE gap assessment. It reuses the ASPICE Base-Practice catalog from
// aspiceFeatures() (demoaspice.go) but decides covered-vs-gap from the curated
// euiccASPICESatisfied verdict below — not the catalog's generic gap markers.
//
// Satisfied Base Practices are mapped to a real eUICC test_case (round-robin
// over the eUICC test pool) as clickable evidence; the rest are left unmapped
// and surface as coverage gaps. Members are all eUICC CUST-* requirements —
// each ASPICE process is assessed against the whole eUICC requirement set.
//
// It writes only coverage-layer rows (canonicals, one version each, model,
// mappings, members). It never touches requirement / test_case / sync_state
// rows. Idempotent: prior ASPICE canonicals are cleared first.

// ASPICEAssessmentSummary reports what the assessment produced / consumed.
type ASPICEAssessmentSummary struct {
	Processes int `json:"processes"`
	Members   int `json:"members"`
	Tests     int `json:"tests"`    // eUICC candidate tests found
	Mappings  int `json:"mappings"` // satisfied Base Practices given evidence
	Gaps      int `json:"gaps"`     // unmapped required values
}

// euiccASPICESatisfied is the curated verdict: per ASPICE process code, the
// Base-Practice value labels the demo-euicc dataset satisfies. Everything not
// listed is treated as a gap. Each label MUST match a value in that process's
// aspiceFeatures() model (guarded by TestSeedEUICCASPICEAssessment).
//
// Rationale per process (see the eUICC-vs-ASPICE analysis):
//   - SYS.2 / SYS.5: eUICC has system requirements, a coverage/verification
//     model, req<->test traceability, executions and change/version control —
//     but no verification-criteria attribute, no stakeholder tier, no
//     consistency reporting, no per-case result work product.
//   - SWE.1 / SWE.6: no distinct software-requirement or SW-qualification tier.
//   - SWE.4: no unit-verification level at all (0%).
//   - SUP.9: defect linking gives partial problem management.
//   - SUP.10: change requests + per-customer decisions + versions make this the
//     strongest area.
var euiccASPICESatisfied = map[string][]string{
	"SRA": { // SYS.2 System Requirements Analysis
		"System requirements specified",
		"Requirements structured & prioritized",
		"Feasibility & verifiability analyzed",
		"Operating-environment impact analyzed",
		"Verification criteria defined per requirement",
		"Every requirement has verification criteria",
		"Bidirectional: system req ↔ system test",
		"System requirements baselined",
	},
	"SQT": { // SYS.5 System Qualification Test
		"Test cases specified from system requirements",
		"Test cases selected per strategy",
		"System requirements covered by tests",
		"Integrated system tested",
		"Results recorded (pass/fail)",
		"Failures raised as problems (SUP.9)",
		"Bidirectional: system req ↔ test case",
		"Results summarized",
	},
	"SWR": { // SWE.1 Software Requirements Analysis
		"Software requirements specified",
		"Bidirectional: system ↔ SW req",
	},
	"SUV": {}, // SWE.4 Software Unit Verification — no unit tests in the dataset
	"SWQ": { // SWE.6 Software Qualification Test
		"Integrated software tested",
		"Results recorded (pass/fail)",
	},
	"PRM": { // SUP.9 Problem Resolution Management
		"Problems identified & recorded",
		"Status recorded",
		"Each problem has a unique record",
		"Root cause determined",
		"Bidirectional: problem ↔ affected work-products",
		"Bidirectional: problem ↔ change request",
	},
	"CRM": { // SUP.10 Change Request Management
		"CR management strategy defined",
		"Change requests identified & recorded",
		"Status recorded",
		"Each CR has a unique record",
		"Impact & dependencies analyzed",
		"Approval obtained before implementation",
		"Implementation reviewed",
		"Affected parties agreed",
		"Bidirectional: CR ↔ affected work-products",
	},
}

// SeedEUICCASPICEAssessment builds the ASPICE assessment overlay for a synced
// demo-euicc profile. Idempotent.
func (m *Module) SeedEUICCASPICEAssessment(profileID string) (ASPICEAssessmentSummary, error) {
	var sum ASPICEAssessmentSummary

	// Clear any prior ASPICE canonicals (reuses the demoaspice.go helper, which
	// matches on aspiceFeatures() process names).
	if err := m.deletePriorASPICECanonicals(profileID); err != nil {
		return sum, fmt.Errorf("clear prior ASPICE canonicals: %w", err)
	}

	// Members = all eUICC CUST-* requirements (whole requirement set).
	memberKeys, err := m.queryKeys(
		`SELECT jira_key FROM requirement
		  WHERE profile_id=? AND project_key IN ('CUST-MNO-CONSUMER','CUST-IOT-FLEET','CUST-M2M-AUTO')
		  ORDER BY jira_key`, profileID)
	if err != nil {
		return sum, fmt.Errorf("query eUICC members: %w", err)
	}

	// Evidence pool = all eUICC test cases.
	testKeys, err := m.queryKeys(
		`SELECT jira_key FROM test_case WHERE profile_id=? ORDER BY jira_key`, profileID)
	if err != nil {
		return sum, fmt.Errorf("query eUICC tests: %w", err)
	}
	sum.Tests = len(testKeys)

	ti := 0
	for _, f := range aspiceFeatures() {
		satisfied := make(map[string]bool, len(euiccASPICESatisfied[f.code]))
		for _, label := range euiccASPICESatisfied[f.code] {
			satisfied[label] = true
		}
		if err := m.assessOneProcess(profileID, f, satisfied, memberKeys, testKeys, &ti, &sum); err != nil {
			return sum, fmt.Errorf("assess %s: %w", f.fn, err)
		}
		sum.Processes++
	}
	return sum, nil
}

// assessOneProcess seeds one ASPICE process's canonical, version, model, members
// and evidence mappings for the eUICC assessment.
func (m *Module) assessOneProcess(profileID string, f pkcsFeature, satisfied map[string]bool,
	memberKeys, testKeys []string, ti *int, sum *ASPICEAssessmentSummary) error {

	cid, err := m.CreateCanonical(profileID, f.fn, "ASPICE", f.summary)
	if err != nil {
		return err
	}
	if err := m.SetMembers(profileID, cid, memberKeys); err != nil {
		return err
	}
	sum.Members += len(memberKeys)

	ver, err := m.CreateVersion(profileID, cid, "ASPICE 3.1 assessment", "assessment",
		"eUICC dataset assessed against Automotive SPICE 3.1 (VDA scope).")
	if err != nil {
		return err
	}

	for gi, g := range f.groups {
		gid, err := m.UpsertNode(profileID, NodeEdit{Kind: "group", VersionID: ver, Name: g.name, SortOrder: gi})
		if err != nil {
			return err
		}
		pid, err := m.UpsertNode(profileID, NodeEdit{Kind: "parameter", GroupID: gid, Name: g.name})
		if err != nil {
			return err
		}
		for vi, val := range g.vals {
			kind := val.kind
			if kind == "" {
				kind = "value"
			}
			vid, err := m.UpsertNode(profileID, NodeEdit{
				Kind: "value", ParameterID: pid, Name: val.label,
				ValueKind: kind, ErrorCode: val.errCode, IsRequired: true, SortOrder: vi,
			})
			if err != nil {
				return err
			}
			// Curated verdict: satisfied Base Practices get eUICC test evidence;
			// the rest are gaps. (The catalog's own val.gap marker is ignored.)
			if satisfied[val.label] && len(testKeys) > 0 {
				tk := testKeys[*ti%len(testKeys)]
				*ti++
				if err := m.SetValueTests(profileID, vid, []string{tk}); err != nil {
					return err
				}
				sum.Mappings++
			} else {
				sum.Gaps++
			}
		}
	}
	return nil
}

// queryKeys runs a single-column string query and collects the results.
func (m *Module) queryKeys(query, profileID string) ([]string, error) {
	rows, err := m.db.Query(query, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/coverage/ -run TestSeedEUICCASPICEAssessment -count=1 -v`
Expected: PASS. The log line shows `summary: {Processes:7 Members:147 Tests:42 Mappings:35 Gaps:57}` (Mappings = 8+8+2+0+2+6+9 = 35; Gaps = 92-35 = 57).

- [ ] **Step 5: gofmt check (do not run gofmt -w)**

Run: `tr -d '\r' < internal/coverage/aspiceassessment.go | gofmt -d | grep -E '^[+-]' | grep -v '^[+-][+-]' || echo CLEAN`
Expected: `CLEAN`.

- [ ] **Step 6: Commit**

```bash
git add internal/coverage/aspiceassessment.go internal/coverage/aspiceassessment_test.go
git commit -m "feat(coverage): assess the eUICC dataset against ASPICE"
```

---

### Task 2: App binding + Wails bindings

**Files:**
- Modify: `app_coverage.go` (after `SeedEUICCReference`, ~line 292)
- Regenerate: `frontend/wailsjs/go/main/App.js`, `App.d.ts`, `frontend/wailsjs/go/models.ts`

**Interfaces:**
- Consumes: `coverage.ASPICEAssessmentSummary` and `(*Module).SeedEUICCASPICEAssessment` (Task 1); `recoverToError` (app.go:41), `requireStore` (app.go:245).
- Produces: `func (a *App) SeedEUICCASPICEAssessment(profileID string) (coverage.ASPICEAssessmentSummary, error)` — a Wails-bound method; the frontend re-exports it as `SeedEUICCASPICEAssessment`.

- [ ] **Step 1: Add the binding**

In `app_coverage.go`, immediately after the `SeedEUICCReference` method, add:

```go
// SeedEUICCASPICEAssessment overlays the seven Automotive SPICE processes as
// coverage canonicals judged against the synced demo-euicc dataset (a curated
// in-app ASPICE gap assessment). For use on a throwaway "demo-euicc" profile.
// Idempotent.
func (a *App) SeedEUICCASPICEAssessment(profileID string) (summary coverage.ASPICEAssessmentSummary, err error) {
	defer recoverToError("SeedEUICCASPICEAssessment", &err)
	if err := a.requireStore(); err != nil {
		return coverage.ASPICEAssessmentSummary{}, err
	}
	return a.cov.SeedEUICCASPICEAssessment(profileID)
}
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: exit 0.

- [ ] **Step 3: Regenerate Wails bindings**

Run: `wails generate module`
Expected: exit 0; output KnownStructs includes `coverage.ASPICEAssessmentSummary`.

- [ ] **Step 4: Verify bindings**

Run: `grep -c SeedEUICCASPICEAssessment frontend/wailsjs/go/main/App.js frontend/wailsjs/go/main/App.d.ts; grep -c ASPICEAssessmentSummary frontend/wailsjs/go/models.ts`
Expected: `App.js:2`, `App.d.ts:1`, `models.ts:2` (or greater). Confirm the diffs are clean additions: `git diff --ignore-cr-at-eol -- frontend/wailsjs/go/models.ts | tr -d '\r' | grep -E '^[+-]' | grep -v '^[+-][+-]'` shows only the new `ASPICEAssessmentSummary` class.

- [ ] **Step 5: Commit**

```bash
git add app_coverage.go frontend/wailsjs/go/main/App.js frontend/wailsjs/go/main/App.d.ts frontend/wailsjs/go/models.ts
git commit -m "feat(app): bind SeedEUICCASPICEAssessment + regenerate wails bindings"
```

---

### Task 3: Frontend "Assess against ASPICE" button

**Files:**
- Modify: `frontend/src/api.ts` (re-export block, ~line 203)
- Modify: `frontend/src/components/CoverageView.tsx` (import ~line 24; add handler near `loadPkcsReference` ~line 292; add button near the existing "Load eUICC coverage" button ~line 457)

**Interfaces:**
- Consumes: `SeedEUICCASPICEAssessment` binding (Task 2), re-exported from `../api`; existing component state `busy`, `notice`, `error`, `profileId`, `demoVariant`, and functions `loadList`, `onChanged`, `errMsg`, `setBusy`, `setNotice`, `setError`.
- Produces: a user-visible action; no new exported types.

- [ ] **Step 1: Re-export the binding in api.ts**

In `frontend/src/api.ts`, in the `export { ... } from "../wailsjs/go/main/App";` block, add `SeedEUICCASPICEAssessment,` immediately after `SeedEUICCReference,`:

```ts
  SeedPKCS11Reference,
  SeedEUICCReference,
  SeedEUICCASPICEAssessment,
  SeedASPICEReference,
```

- [ ] **Step 2: Import it in CoverageView.tsx**

In the `from "../api"` import block, add `SeedEUICCASPICEAssessment,` next to `SeedEUICCReference,`:

```ts
  SeedPKCS11Reference,
  SeedEUICCReference,
  SeedEUICCASPICEAssessment,
  SeedASPICEReference,
```

- [ ] **Step 3: Add the handler**

In `CoverageView.tsx`, immediately after the `loadPkcsReference` function, add:

```tsx
  async function assessAspice() {
    setBusy(true);
    setNotice("");
    setError("");
    try {
      const s = await SeedEUICCASPICEAssessment(profileId);
      await loadList();
      onChanged?.();
      setNotice(
        `Assessed the eUICC dataset against ASPICE — ${s.processes} processes, ` +
          `${s.mappings} practices with evidence, ${s.gaps} gaps. ` +
          `Sync the demo-euicc profile first if you haven't already.`,
      );
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setBusy(false);
    }
  }
```

- [ ] **Step 4: Add the button**

Find the existing "Load eUICC coverage" button in `CoverageView.tsx` (the `<button>` whose label ternary includes `"Load eUICC coverage"`). Immediately after that `</button>`, still inside the same parent container, add a second button shown only for the eUICC variant:

```tsx
                    {demoVariant === "euicc" && (
                      <button
                        className="btn"
                        disabled={busy}
                        onClick={() => void assessAspice()}
                      >
                        Assess against ASPICE
                      </button>
                    )}
```

- [ ] **Step 5: Build the frontend**

Run: `cd frontend && npm run build`
Expected: `tsc` + `vite build` succeed, exit 0.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/api.ts frontend/src/components/CoverageView.tsx
git commit -m "feat(ui): add Assess against ASPICE action on demo-euicc profiles"
```

---

### Task 4: Whole-feature verification + PR

**Files:** none (verification + integration).

- [ ] **Step 1: Full Go build + vet**

Run: `go build ./... && go vet ./internal/coverage/`
Expected: exit 0, no output.

- [ ] **Step 2: Full test suite**

Run: `go test ./... -count=1`
Expected: all packages `ok`.

- [ ] **Step 3: Confirm no stray churn**

Run: `git status --short`
Expected: clean working tree (all committed). If `go.mod`/`package-lock.json`/`package.json.md5` show, they are build-tool churn — do NOT commit them.

- [ ] **Step 4: Push and open PR**

```bash
git push -u origin feature/euicc-aspice-assessment
gh pr create --base main --head feature/euicc-aspice-assessment \
  --title "feat: in-app ASPICE assessment of the eUICC demo dataset" \
  --body-file - <<'BODY'
## What

Adds an **"Assess against ASPICE"** action to `demo-euicc` profiles. It overlays
the seven ASPICE processes (SYS.2, SYS.5, SWE.1, SWE.4, SWE.6, SUP.9, SUP.10) as
coverage canonicals judged against the *actual* eUICC data, so the Coverage view
renders a faithful ASPICE gap picture — SWE.4 at 0% (no unit tests), SUP.10
strongest (change-request management present), SYS.2/SYS.5 partial.

## How

A curated verdict map (`euiccASPICESatisfied`) decides, per ASPICE Base
Practice, whether the eUICC dataset satisfies it. Satisfied practices are mapped
to a real eUICC test case as clickable evidence; the rest are gaps. Reuses the
ASPICE Base-Practice catalog `aspiceFeatures()` and the whole coverage machinery
(canonicals, version, model, value→test mapping, gap reporting). No schema
change.

Stacked on #62 (reuses `aspiceFeatures()`); rebase onto `main` after #62 merges.
BODY
```

Note: because this branch is stacked on `feature/demo-aspice`, the PR diff will include #62's commits until #62 merges. After #62 merges to `main`, rebase this branch onto `main` so the PR shows only this feature.

---

## Verification (end-to-end, demo mode)

With `wails dev`:
1. Create a profile with Jira URL `demo-euicc`; Sync.
2. Open the Coverage view; click **Assess against ASPICE**.
3. Confirm seven ASPICE canonicals appear (SYS.2 … SUP.10). Open each:
   - **SWE.4 Software Unit Verification** shows **0%** (all Base Practices are gaps).
   - **SUP.10 Change Request Management** shows the **highest** coverage.
   - **SYS.2 / SYS.5** show partial coverage; satisfied Base Practices link to real eUICC tests as evidence; the gap list names the missing practices (verification criteria, consistency, test-result-per-case, etc.).
4. Confirm the RSP-procedure canonicals from "Load eUICC coverage" still coexist (if previously loaded).
