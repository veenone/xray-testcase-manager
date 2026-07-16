package kiwi

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"xray-test-manager/internal/backend"
)

// This file is P5.2's deliverable: the Kiwi container (TestPlan) + run
// (TestRun/TestExecution) WRITE surface — CreateContainer,
// AddTestsToContainer, RemoveTestsFromContainer, SetTestRunStatus, and
// DeleteContainer — plus the id-resolution helpers they need. It follows the
// same "resolve deterministically from real existing rows; error rather than
// guess or fabricate" convention write.go established for CreateTest's
// category/status/priority resolution (P5.1). Bug writes (P5.3) and the
// commit-engine capability gating that decides WHICH of these Kiwi write
// paths the engine actually calls for a given pending-change row (P5.4) are
// out of scope; see p5_0-kiwi-write-spec.md / p5_2-brief.md.
//
// RPC method names below were confirmed against
// https://kiwitcms.readthedocs.io (fetched during this task) rather than
// only inferred from p4_verify-report.md's create-path facts:
//   - TestPlan.create({product, product_version, name, type[, parent, text]})
//     — the docs' own example shows product/product_version/type as
//     INTEGER ids, matching this file's id-resolution approach.
//   - TestPlan.add_case(plan_id, case_id) / TestPlan.remove_case(plan_id,
//     case_id) — both confirmed present.
//   - TestPlan has NO documented whole-object "remove" RPC (only
//     remove_case/remove_tag/update_case_order) — DeleteContainer(KindTestPlan)
//     stays ErrUnsupported; this is a genuine gap in Kiwi's RPC surface, not
//     an oversight here.
//   - TestRun.create({build, manager, plan, summary}) — docs' example shows
//     build/manager/plan as INTEGER ids (pk of Build/User/TestPlan).
//   - TestRun.add_case(run_id, case_id) — confirmed present, returns the new
//     TestExecution row.
//   - TestRun.remove_case is documented as DEPRECATED "in favor of
//     TestExecution.remove()" — RemoveTestsFromContainer(KindTestExec) calls
//     TestExecution.remove({"run":...,"case":...}) instead of the deprecated
//     alias (see removeCasesFromRun below).
//   - TestRun.remove(query) — confirmed present; used by DeleteContainer for
//     KindTestExec.
//   - TestExecutionStatus.filter(query)/create(values) — confirmed present
//     as a dedicated module (tcms.rpc.api.testexecutionstatus), but the docs
//     do NOT enumerate the row's fields. resolveExecStatusID assumes the
//     same {id, name} shape TestCaseStatus.filter uses (write.go's
//     resolveStatusID) — p4_verify-report.md's live TestExecution row
//     exposing `status__name="PASSED"` is strong indirect evidence for a
//     `name` field, but this is FLAGGED for live-Kiwi confirmation (see
//     p5_2-report.md).

// --- id-bearing decode shapes for container/run write resolution ---
//
// These mirror write.go's kiwiPriorityRow/kiwiStatusRow/kiwiCategoryRow
// pattern: the read path's decode structs (convert.go) intentionally drop
// ids for fields ListContainers/GetTestRuns never need to resolve by id, so
// separate structs are used here rather than widening the read-path types.

type kiwiProductRow struct {
	ID int `json:"id"`
}

type kiwiPlanTypeRow struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// kiwiVersionRow is Version.filter's id-bearing shape (convert.go's
// kiwiValue only carries `value`, which CreateContainer's version resolution
// cannot use).
type kiwiVersionRow struct {
	ID    int    `json:"id"`
	Value string `json:"value"`
}

// kiwiPlanForRunRow is a narrower TestPlan.filter shape than convert.go's
// kiwiTestPlan (read path): resolveDefaultPlanForRun only needs the plan id
// and its product_version id to seed a best-effort TestRun.create.
type kiwiPlanForRunRow struct {
	ID             int `json:"id"`
	ProductVersion int `json:"product_version"`
}

type kiwiBuildRow struct {
	ID int `json:"id"`
}

// kiwiUserRow is User.filter's id-bearing shape (convert.go's kiwiUser only
// carries username/first_name/last_name/email for TestConnection's display
// mapping — no id, which resolveManagerID needs).
type kiwiUserRow struct {
	ID int `json:"id"`
}

type kiwiExecStatusRow struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// kiwiExecRow is TestExecution.filter's id-only shape for
// SetTestRunStatus's exec lookup (it only needs the pk to call
// TestExecution.update on).
type kiwiExecRow struct {
	ID int `json:"id"`
}

type kiwiCreatedPlan struct {
	ID int `json:"id"`
}

type kiwiCreatedRun struct {
	ID int `json:"id"`
}

// --- resolution helpers ---

// resolveProductID resolves a Product NAME (projectKey) to its Kiwi pk via
// Product.filter({"name": projectKey}) — the same product-scoping value
// every other projectKey-scoped filter in this package uses
// (ProjectComponents/ProjectVersions/resolveCategoryID all filter on
// `product__name`; this is the one write path that needs the Product's OWN
// id, since TestPlan.create's `product` field is the FK id itself).
func (a *Adapter) resolveProductID(ctx context.Context, projectKey string) (int, error) {
	if strings.TrimSpace(projectKey) == "" {
		return 0, fmt.Errorf("kiwi: a product (projectKey) is required")
	}
	var rows []kiwiProductRow
	if err := a.c.call(ctx, "Product.filter", []any{map[string]any{"name": projectKey}}, &rows); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, fmt.Errorf("kiwi: no Product named %q", projectKey)
	}
	return rows[0].ID, nil
}

// resolveDefaultPlanTypeID picks a PlanType id for CreateContainer(KindTestPlan),
// which Kiwi requires but the neutral CreateContainer(projectKey, kind, summary)
// signature has no slot for. Documented default choice, mirroring
// resolveDefaultStatusID's convention in write.go: prefer "Unit" (Kiwi's own
// first/default PlanType in a stock install), then "Function", then "System"
// (the type used in this task's live-verified seed data, p4_verify-report.md),
// then simply the lowest-id row (Kiwi's schema has no "is_default" flag to
// key off instead).
func (a *Adapter) resolveDefaultPlanTypeID(ctx context.Context) (int, error) {
	var rows []kiwiPlanTypeRow
	if err := a.c.call(ctx, "PlanType.filter", []any{map[string]any{}}, &rows); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, fmt.Errorf("kiwi: no PlanType rows available on this instance; cannot create a Test Plan")
	}
	for _, want := range []string{"Unit", "Function", "System"} {
		for _, r := range rows {
			if strings.EqualFold(r.Name, want) {
				return r.ID, nil
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows[0].ID, nil
}

// resolveDefaultVersionID picks a Version id under projectKey's product for
// CreateContainer(KindTestPlan)'s required `product_version` field, via
// Version.filter({"product__name": projectKey}) — the same product__name
// scoping ProjectVersions already uses. Lowest-id tiebreak when more than one
// version exists (documented arbitrary tiebreak, same convention as
// resolveCategoryID). Errors rather than guessing when the product has no
// Version at all: every Kiwi Product gets an auto "unspecified" Version on
// creation (p4_verify-report.md's seed notes), so an empty result here means
// projectKey itself did not resolve to a real product.
func (a *Adapter) resolveDefaultVersionID(ctx context.Context, projectKey string) (int, error) {
	var rows []kiwiVersionRow
	if err := a.c.call(ctx, "Version.filter", []any{map[string]any{"product__name": projectKey}}, &rows); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, fmt.Errorf("kiwi: no Version found for product %q; create one in Kiwi before creating a Test Plan here", projectKey)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows[0].ID, nil
}

// resolveDefaultPlanForRun is CreateContainer(KindTestExec)'s core
// impedance-gap resolution (p5_0-kiwi-write-spec.md item 1 / p5_2-brief.md
// item 1): Kiwi's TestRun.create hard-requires a `plan` id, but the neutral
// CreateContainer(projectKey, kind, summary) signature carries no plan
// context at all. Rather than fabricating one (calling TestPlan.create as a
// silent side effect of creating a Test Execution — explicitly forbidden by
// the brief), this resolves an EXISTING TestPlan under the product,
// deterministically (lowest id), exactly like resolveCategoryID/
// resolveDefaultVersionID resolve other required-but-unsupplied fields
// elsewhere in this package.
//
// When the product has NO existing TestPlan, this is the genuine STOP-and-
// report case: there is no real plan to attach the new TestRun to, and
// picking "none" is not a choice this method can make up. The caller's error
// message spells out the fix (create a Test Plan for the product first).
func (a *Adapter) resolveDefaultPlanForRun(ctx context.Context, projectKey string) (planID, productVersionID int, err error) {
	if strings.TrimSpace(projectKey) == "" {
		return 0, 0, fmt.Errorf("a product (projectKey) is required to resolve a Test Plan")
	}
	var rows []kiwiPlanForRunRow
	if err := a.c.call(ctx, "TestPlan.filter", []any{map[string]any{"product__name": projectKey}}, &rows); err != nil {
		return 0, 0, err
	}
	if len(rows) == 0 {
		return 0, 0, fmt.Errorf(
			"no existing Test Plan found for product %q — Kiwi's TestRun.create requires a plan id, and the neutral "+
				"CreateContainer(kind=testexec) call carries none; create a Test Plan for this product first "+
				"(CreateContainer(kind=testplan) or directly in Kiwi), then retry", projectKey)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows[0].ID, rows[0].ProductVersion, nil
}

// resolveDefaultBuildID picks a Build id under productVersionID for
// CreateContainer(KindTestExec)'s required `build` field, via
// Build.filter({"version": productVersionID}). Errors (not guesses) when the
// resolved plan's version has no Build at all — same "resolve existing data,
// STOP if none" rule as resolveDefaultPlanForRun.
func (a *Adapter) resolveDefaultBuildID(ctx context.Context, productVersionID int) (int, error) {
	var rows []kiwiBuildRow
	if err := a.c.call(ctx, "Build.filter", []any{map[string]any{"version": productVersionID}}, &rows); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, fmt.Errorf("no Build found for product_version %d; create one in Kiwi before creating a Test Execution here", productVersionID)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows[0].ID, nil
}

// resolveManagerID picks an active Kiwi user id to use as
// CreateContainer(KindTestExec)'s required TestRun `manager` field, via
// User.filter({"is_active":true}) — the SAME query TestConnection uses to
// resolve the authenticated user's display info (adapter.go), but this needs
// the numeric id TestRun.create expects rather than the display fields
// kiwiUser carries. Kiwi's RPC surface has no "who am I" call distinct from
// this filter, so — like TestConnection — the first active user is taken
// (lowest id tiebreak for determinism); a real deployment's session-login
// user is very typically the only/first active row returned for a scoped
// token.
func (a *Adapter) resolveManagerID(ctx context.Context) (int, error) {
	var rows []kiwiUserRow
	if err := a.c.call(ctx, "User.filter", []any{map[string]any{"is_active": true}}, &rows); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, fmt.Errorf("no active Kiwi user found to use as the Test Run manager")
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows[0].ID, nil
}

// resolveExecStatusID resolves a TestExecutionStatus NAME (e.g. "PASSED") to
// its Kiwi pk via TestExecutionStatus.filter({"name": name}) — the
// TestExecution-result counterpart to write.go's resolveStatusID
// (TestCaseStatus), per p4_verify-report.md's explicit distinction: "status
// is a TestExecutionStatus (NOT TestCaseStatus)". See this file's header
// comment for why the {id,name} row shape is flagged, not fully confirmed.
func (a *Adapter) resolveExecStatusID(ctx context.Context, name string) (int, error) {
	var rows []kiwiExecStatusRow
	if err := a.c.call(ctx, "TestExecutionStatus.filter", []any{map[string]any{"name": name}}, &rows); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, fmt.Errorf("kiwi: TestExecutionStatus %q not found", name)
	}
	return rows[0].ID, nil
}

// --- CreateContainer ---

// CreateContainer creates a new TestPlan or TestRun, branching on kind
// (spec item 1). There is no KindTestSet case: Kiwi has no Test Set concept
// (spec §3.5/§4.1, matching ListContainers/Capabilities' ContainerKinds).
func (a *Adapter) CreateContainer(ctx context.Context, projectKey, kind, summary string) (string, error) {
	switch kind {
	case backend.KindTestPlan:
		return a.createTestPlan(ctx, projectKey, summary)
	case backend.KindTestExec:
		return a.createTestRun(ctx, projectKey, summary)
	default:
		return "", fmt.Errorf("kiwi: unsupported container kind %q", kind)
	}
}

// createTestPlan implements CreateContainer(KindTestPlan): resolve
// product/type/product_version (all Kiwi requires beyond name/summary, per
// p4_verify-report.md's create-path facts and the docs' own TestPlan.create
// example), then TestPlan.create. This is a full, non-degraded
// implementation — the KindTestExec impedance gap below does not apply to
// plans, since a TestPlan needs no OTHER container to already exist.
func (a *Adapter) createTestPlan(ctx context.Context, projectKey, summary string) (string, error) {
	productID, err := a.resolveProductID(ctx, projectKey)
	if err != nil {
		return "", fmt.Errorf("resolve product: %w", err)
	}
	typeID, err := a.resolveDefaultPlanTypeID(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve plan type: %w", err)
	}
	versionID, err := a.resolveDefaultVersionID(ctx, projectKey)
	if err != nil {
		return "", fmt.Errorf("resolve product version: %w", err)
	}

	values := map[string]any{
		"name":            summary,
		"product":         productID,
		"type":            typeID,
		"product_version": versionID,
	}
	var created kiwiCreatedPlan
	if err := a.c.call(ctx, "TestPlan.create", []any{values}, &created); err != nil {
		return "", err
	}
	return strconv.Itoa(created.ID), nil
}

// createTestRun implements CreateContainer(KindTestExec): the neutral-model
// -> Kiwi TestRun impedance gap (p5_0-kiwi-write-spec.md item 1,
// p5_2-brief.md item 1). CreateContainer's signature is
// (projectKey, kind, summary) — no plan, no build, no manager — but
// TestRun.create hard-requires all three as ids. This method attempts the
// brief's best-effort resolution (an existing plan under the product, a
// build under that plan's version, the authenticated/active user as
// manager) and returns a clear error — WITHOUT calling TestRun.create at
// all — the moment any of those three cannot be resolved from real existing
// data. It never calls TestPlan.create/Build.create/etc. to synthesize
// what's missing: that would be exactly the "fabricated remote-mutating
// call" the brief forbids.
func (a *Adapter) createTestRun(ctx context.Context, projectKey, summary string) (string, error) {
	planID, versionID, err := a.resolveDefaultPlanForRun(ctx, projectKey)
	if err != nil {
		return "", fmt.Errorf(
			"kiwi: cannot create a Test Execution for product %q: %w (neutral CreateContainer(kind=testexec) -> "+
				"Kiwi TestRun.create({plan,build,manager}) impedance gap — see container_write.go's createTestRun doc comment)",
			projectKey, err)
	}
	buildID, err := a.resolveDefaultBuildID(ctx, versionID)
	if err != nil {
		return "", fmt.Errorf("kiwi: cannot create a Test Execution under plan %d: %w", planID, err)
	}
	managerID, err := a.resolveManagerID(ctx)
	if err != nil {
		return "", fmt.Errorf("kiwi: cannot create a Test Execution: %w", err)
	}

	values := map[string]any{
		"summary": summary,
		"plan":    planID,
		"build":   buildID,
		"manager": managerID,
	}
	var created kiwiCreatedRun
	if err := a.c.call(ctx, "TestRun.create", []any{values}, &created); err != nil {
		return "", err
	}
	return strconv.Itoa(created.ID), nil
}

// --- membership: AddTestsToContainer / RemoveTestsFromContainer ---

// containerAddCaseMethod maps a container kind to its Kiwi add_case RPC
// (spec item 2): both TestPlan.add_case(plan_id, case_id) and
// TestRun.add_case(run_id, case_id) are confirmed-present, same-shaped
// calls, so AddTestsToContainer needs only this one branch.
func containerAddCaseMethod(kind string) (string, error) {
	switch kind {
	case backend.KindTestPlan:
		return "TestPlan.add_case", nil
	case backend.KindTestExec:
		return "TestRun.add_case", nil
	default:
		return "", fmt.Errorf("kiwi: unsupported container kind %q", kind)
	}
}

// AddTestsToContainer links each test to the container, one add_case RPC
// per test (Kiwi's add_case takes a single case id, not a batch — spec
// item 2). Empty testKeys is a no-op, matching internal/jira's
// AddTestsToContainer convention.
func (a *Adapter) AddTestsToContainer(ctx context.Context, kind, containerKey string, testKeys []string) error {
	if len(testKeys) == 0 {
		return nil
	}
	containerID, err := parseKiwiID(containerKey)
	if err != nil {
		return err
	}
	method, err := containerAddCaseMethod(kind)
	if err != nil {
		return err
	}
	for _, tk := range testKeys {
		caseID, err := parseKiwiID(tk)
		if err != nil {
			return err
		}
		if err := a.c.call(ctx, method, []any{containerID, caseID}, nil); err != nil {
			return fmt.Errorf("kiwi: %s(%d, %d): %w", method, containerID, caseID, err)
		}
	}
	return nil
}

// RemoveTestsFromContainer unlinks each test from the container (spec item
// 3). The two kinds are NOT symmetric with AddTestsToContainer's add_case
// pairing:
//   - KindTestPlan uses TestPlan.remove_case(plan_id, case_id) — confirmed
//     present and current (not deprecated).
//   - KindTestExec does NOT use TestRun.remove_case: the docs mark it
//     deprecated "in favor of TestExecution.remove()", so this calls
//     TestExecution.remove({"run":containerID,"case":caseID}) instead —
//     removing the per-case TestExecution row directly, which is the
//     documented current way to drop a case from a run.
//
// Either RPC returning "method not found" (-32601) is translated to
// backend.ErrUnsupported (wrapped, so errors.Is still matches) rather than a
// bare error, per the brief's "probe the exact method name; report if
// absent → best-effort/UNSUP" guidance — this lets a future commit-engine
// caller (P5.4) treat an instance that genuinely lacks the RPC as a skipped
// capability rather than a hard commit failure.
func (a *Adapter) RemoveTestsFromContainer(ctx context.Context, kind, containerKey string, testKeys []string) error {
	if len(testKeys) == 0 {
		return nil
	}
	containerID, err := parseKiwiID(containerKey)
	if err != nil {
		return err
	}
	switch kind {
	case backend.KindTestPlan:
		for _, tk := range testKeys {
			caseID, err := parseKiwiID(tk)
			if err != nil {
				return err
			}
			if err := a.c.call(ctx, "TestPlan.remove_case", []any{containerID, caseID}, nil); err != nil {
				if isMethodNotFound(err) {
					return fmt.Errorf("%w: TestPlan.remove_case not registered on this Kiwi instance", backend.ErrUnsupported)
				}
				return fmt.Errorf("kiwi: TestPlan.remove_case(%d, %d): %w", containerID, caseID, err)
			}
		}
		return nil
	case backend.KindTestExec:
		for _, tk := range testKeys {
			caseID, err := parseKiwiID(tk)
			if err != nil {
				return err
			}
			if err := a.c.call(ctx, "TestExecution.remove", []any{map[string]any{"run": containerID, "case": caseID}}, nil); err != nil {
				if isMethodNotFound(err) {
					return fmt.Errorf("%w: TestExecution.remove not registered on this Kiwi instance", backend.ErrUnsupported)
				}
				return fmt.Errorf("kiwi: TestExecution.remove(run=%d, case=%d): %w", containerID, caseID, err)
			}
		}
		return nil
	default:
		return fmt.Errorf("kiwi: unsupported container kind %q", kind)
	}
}

// --- SetTestRunStatus ---

// SetTestRunStatus sets a Test's run result inside a Test Execution (spec
// item 4), mirroring internal/jira's SetTestRunStatus contract: execKey +
// testKey identify the (run, case) pair, matching how ExecPlans/GetTestRuns
// already treat execKey as the Kiwi TestRun id (adapter.go doc comments).
// It finds the TestExecution row for (run=execKey, case=testKey) via
// TestExecution.filter, resolves the status NAME to a TestExecutionStatus id
// via resolveExecStatusID, then calls TestExecution.update(exec_id,
// {"status": id}) — deliberately omitting `tested_by` (p4_verify-report.md
// shows TestExecution.update accepting {status, tested_by} together, but
// this method's neutral signature has no "who" to attribute the result to;
// Kiwi defaults tested_by from the caller on write, same as TestCase.create's
// author).
func (a *Adapter) SetTestRunStatus(ctx context.Context, execKey, testKey, status string) error {
	runID, err := parseKiwiID(execKey)
	if err != nil {
		return err
	}
	caseID, err := parseKiwiID(testKey)
	if err != nil {
		return err
	}
	status = strings.TrimSpace(status)
	if status == "" {
		return fmt.Errorf("kiwi: a run status is required")
	}

	var execs []kiwiExecRow
	if err := a.c.call(ctx, "TestExecution.filter", []any{map[string]any{"run": runID, "case": caseID}}, &execs); err != nil {
		return err
	}
	if len(execs) == 0 {
		return fmt.Errorf("kiwi: no Test Execution found for run %d / case %d — add the test to the run before setting a result", runID, caseID)
	}
	sort.Slice(execs, func(i, j int) bool { return execs[i].ID < execs[j].ID })

	statusID, err := a.resolveExecStatusID(ctx, status)
	if err != nil {
		return err
	}

	if err := a.c.call(ctx, "TestExecution.update", []any{execs[0].ID, map[string]any{"status": statusID}}, nil); err != nil {
		return err
	}
	return nil
}

// --- DeleteContainer ---

// DeleteContainer best-effort deletes a container (spec item 6):
//   - KindTestExec: TestRun.remove({"pk": id}) — confirmed present RPC.
//     A "method not found" response degrades to ErrUnsupported rather than
//     a hard error, same convention as RemoveTestsFromContainer above.
//   - KindTestPlan: Kiwi's TestPlan RPC module has NO documented
//     whole-object remove method (only remove_case/remove_tag/
//     update_case_order) — this stays ErrUnsupported unconditionally rather
//     than guessing an RPC name that very likely does not exist.
func (a *Adapter) DeleteContainer(ctx context.Context, kind, containerKey string) error {
	id, err := parseKiwiID(containerKey)
	if err != nil {
		return err
	}
	switch kind {
	case backend.KindTestExec:
		if err := a.c.call(ctx, "TestRun.remove", []any{map[string]any{"pk": id}}, nil); err != nil {
			if isMethodNotFound(err) {
				return fmt.Errorf("%w: TestRun.remove not registered on this Kiwi instance", backend.ErrUnsupported)
			}
			return fmt.Errorf("kiwi: TestRun.remove(pk=%d): %w", id, err)
		}
		return nil
	case backend.KindTestPlan:
		return fmt.Errorf("%w: Kiwi's TestPlan RPC surface has no whole-object delete method (only remove_case/remove_tag)", backend.ErrUnsupported)
	default:
		return fmt.Errorf("kiwi: unsupported container kind %q", kind)
	}
}
