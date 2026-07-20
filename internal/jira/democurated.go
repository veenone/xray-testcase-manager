package jira

import "fmt"

// Curated demo Test Execution memberships.
//
// The generated membership loop in demoContainersAndLinks cannot produce a
// type-mixed or a type-homogeneous Cucumber execution by accident: it assigns
// execution by i%demoExecCount (8) while demoExecTypeForIndex cycles i%4, and
// because 4 divides 8 every test the loop links to a given execution shares one
// ExecType. Executions that showcase a specific test-type mix therefore have to
// be seeded explicitly.
//
// demoCuratedExecLinks is the single source of truth for those seeded
// memberships. Both derivations of execution membership read it:
//
//   - demoContainersAndLinks, which emits ContainerLink rows (board membership)
//   - demoTestRuns, which emits TestRun rows (per-test run history)
//
// Keeping one list means the two can no longer disagree about who is in an
// execution — previously the curated links existed only on the ContainerLink
// side, so curated members rendered with a run status but no executor, dates,
// defects or comment.

// demoCucumberExecIndex is the 0-based index of the curated all-Cucumber
// execution. It sits immediately after the demoExecCount generated executions
// (0..7), so it is DEMO-TE-9 and the generated loop never touches it — which is
// what lets it stay type-homogeneous.
const demoCucumberExecIndex = demoExecCount

// demoCucumberExecKey returns the key of the curated all-Cucumber execution.
func demoCucumberExecKey(projectKey string) string {
	return fmt.Sprintf("%s-TE-%d", projectKey, demoCucumberExecIndex+1)
}

// curatedMember is one seeded execution membership: a test index (0-based, so
// index 3 is DEMO-4) and the run status it carries in that execution.
type curatedMember struct {
	testIndex int
	runStatus string
}

// curatedExec is the curated membership of one execution, identified by its
// 0-based execution index.
type curatedExec struct {
	execIndex int
	members   []curatedMember
}

// demoCuratedExecLinks returns the curated memberships for a demo project.
//
// This is an ordered slice, not a map: demo output must be byte-stable across
// runs (the generators avoid time.Now and rand for the same reason), and Go
// randomises map iteration order.
//
// Test indices are chosen so that demoExecTypeForIndex gives the intended type:
// i%4==3 is Cucumber (DEMO-4, DEMO-8, DEMO-12, DEMO-16) and i%4==2 is Generic
// (DEMO-3, DEMO-7). None of them collide with the generated loop's assignment
// for the executions used here — the loop would place them in execs 3 and 7 —
// but both readers dedupe anyway, so this stays correct if that ever changes.
func demoCuratedExecLinks() []curatedExec {
	return []curatedExec{
		// DEMO-TE-1: a deliberately MIXED execution. The generated loop already
		// puts Manual tests here (i%8==0 => i%4==0), so adding Cucumber and
		// Generic members makes one execution show three test types at once.
		{execIndex: 0, members: []curatedMember{
			{testIndex: 3, runStatus: "PASS"}, // DEMO-4  Cucumber
			{testIndex: 7, runStatus: "FAIL"}, // DEMO-8  Cucumber
			{testIndex: 2, runStatus: "TODO"}, // DEMO-3  Generic
			{testIndex: 6, runStatus: "PASS"}, // DEMO-7  Generic
		}},
		// DEMO-TE-9: a HOMOGENEOUS all-Cucumber execution, for exercising views
		// that group or filter an execution by test type.
		{execIndex: demoCucumberExecIndex, members: demoCucumberExecMembers()},
	}
}

// demoCucumberExecMembers returns the members of the curated all-Cucumber
// execution (DEMO-TE-9).
//
// Every member MUST be a Cucumber test for the execution to stay
// type-homogeneous. Cucumber tests are those where testIndex%4 == 3:
//
//	testIndex 3 -> DEMO-4    testIndex 11 -> DEMO-12
//	testIndex 7 -> DEMO-8    testIndex 15 -> DEMO-16
//
// NOTE: every one of these currently has a plain "Scenario" body. The
// "Scenario Outline" + Examples branch in makeDemoTest is gated on i%8==0,
// which implies i%4==0 (Manual) — so no Cucumber test can ever reach it. That
// branch is dead code; see the separate note in the report.
//
// runStatus must be one of: PASS, FAIL, TODO, EXECUTING, ABORTED, BLOCKED.
//
// The four statuses below are chosen to exercise distinct UI paths rather than
// to look tidy: FAIL drives the defect + comment rendering (demoTestRuns
// synthesises a BUGS-*/SUP-* defect key and a remark for every FAIL), while
// TODO and EXECUTING cover the non-terminal states that a PASS/FAIL-only
// execution would leave untested.
func demoCucumberExecMembers() []curatedMember {
	return []curatedMember{
		{testIndex: 3, runStatus: "PASS"},       // DEMO-4
		{testIndex: 7, runStatus: "FAIL"},       // DEMO-8
		{testIndex: 11, runStatus: "TODO"},      // DEMO-12
		{testIndex: 15, runStatus: "EXECUTING"}, // DEMO-16
	}
}
