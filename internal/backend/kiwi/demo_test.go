package kiwi

import (
	"context"
	"testing"
)

// This file covers the P4.4 deliverable: constructing the Kiwi adapter
// against the offline kiwi-demo generator and confirming the read path
// returns non-empty, sensible neutral DTOs -- no HTTP, no mock server.

// TestIsKiwiDemoURLRecognisesVariants pins the URL-matching rule app.go's
// factory (newBackend) and the frontend helpers rely on.
func TestIsKiwiDemoURLRecognisesVariants(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"kiwi-demo", true},
		{"KIWI-DEMO", true},
		{"  kiwi-demo  ", true},
		{"kiwi-demo:", true},
		{"kiwi-demo:anything", true},
		{"kiwi-demo-euicc", true},
		{"demo", false},
		{"demo-pkcs", false},
		{"mock:kiwi", false},
		{"https://kiwi.example.com", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsKiwiDemoURL(tc.url); got != tc.want {
			t.Errorf("IsKiwiDemoURL(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

// newDemoAdapter builds a kiwi-demo Adapter and runs TestConnection (which
// also drives the plugin-detection probe), failing the test immediately on
// any error so every other test in this file can assume a connected demo
// adapter.
func newDemoAdapter(t *testing.T) *Adapter {
	t.Helper()
	a := New("kiwi-demo", "")
	if !a.IsDemo() {
		t.Fatal("expected IsDemo()=true for a kiwi-demo URL")
	}
	if _, err := a.TestConnection(context.Background()); err != nil {
		t.Fatalf("TestConnection against the demo generator: %v", err)
	}
	return a
}

// TestDemoAdapterServesNonEmptySensibleData is the P4.4 brief's required
// test: after TestConnection, SearchTestsPage/ListContainers/
// ListRequirements/Capabilities() must all return non-empty, sensible
// neutral DTOs from the demo generator -- proving the kiwi-demo
// short-circuit actually wires end to end (Client.call -> dispatch ->
// generator -> JSON round-trip -> the same convert.go mapping a live Kiwi
// response would go through).
func TestDemoAdapterServesNonEmptySensibleData(t *testing.T) {
	a := newDemoAdapter(t)
	ctx := context.Background()

	// --- Capabilities: base Kiwi caps, with BOTH the requirements-plugin
	// delta flipped (brief: "report BOTH plugins present so requirements +
	// link-types show").
	caps := a.Capabilities()
	if caps.Name != "kiwi" {
		t.Errorf("Capabilities().Name = %q, want %q", caps.Name, "kiwi")
	}
	if !caps.SupportsRequirementObjects {
		t.Error("Capabilities().SupportsRequirementObjects = false, want true in kiwi-demo (both plugins present)")
	}
	if !caps.SupportsIssueLinkTypes {
		t.Error("Capabilities().SupportsIssueLinkTypes = false, want true in kiwi-demo (both plugins present)")
	}
	if !a.hasReviewPlugin {
		t.Error("hasReviewPlugin = false, want true in kiwi-demo (both plugins present)")
	}

	// --- SearchTestsPage: non-empty page, sensible fields, real pagination.
	page, total, err := a.SearchTestsPage(ctx, "DEMO", "", "", 0, 10)
	if err != nil {
		t.Fatalf("SearchTestsPage: %v", err)
	}
	if total == 0 {
		t.Fatal("SearchTestsPage: total = 0, want a non-empty demo dataset")
	}
	if len(page) != 10 {
		t.Fatalf("SearchTestsPage: page length = %d, want 10 (maxResults)", len(page))
	}
	for _, tst := range page {
		if tst.Key == "" || tst.Summary == "" || tst.Status == "" || tst.Priority == "" {
			t.Fatalf("SearchTestsPage: incomplete Test DTO: %#v", tst)
		}
	}
	// Pagination must actually move: page 2 differs from page 1.
	page2, total2, err := a.SearchTestsPage(ctx, "DEMO", "", "", 10, 10)
	if err != nil {
		t.Fatalf("SearchTestsPage page 2: %v", err)
	}
	if total2 != total {
		t.Fatalf("SearchTestsPage: total changed between pages (%d vs %d)", total, total2)
	}
	if len(page2) == 0 || page2[0].Key == page[0].Key {
		t.Fatalf("SearchTestsPage: page 2 did not advance past page 1 (page1[0]=%q page2[0]=%q)", page[0].Key, page2[0].Key)
	}

	// --- GetTestSteps / GetTestFields round-trip for one known test.
	steps, err := a.GetTestSteps(ctx, page[0].Key)
	if err != nil {
		t.Fatalf("GetTestSteps(%s): %v", page[0].Key, err)
	}
	if len(steps) == 0 {
		t.Fatalf("GetTestSteps(%s): expected at least one step", page[0].Key)
	}

	// --- ListContainers: non-empty plans and runs, with membership links.
	var progressCalls int
	containers, links, err := a.ListContainers(ctx, "DEMO", func(done, total int) { progressCalls++ })
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if len(containers) == 0 {
		t.Fatal("ListContainers: expected non-empty containers")
	}
	if len(links) == 0 {
		t.Fatal("ListContainers: expected non-empty container links")
	}
	if progressCalls == 0 {
		t.Error("ListContainers: onProgress was never called")
	}
	var sawPlan, sawExec bool
	for _, c := range containers {
		switch c.Kind {
		case "testplan":
			sawPlan = true
		case "testexec":
			sawExec = true
		}
		if c.Key == "" || c.Summary == "" {
			t.Fatalf("ListContainers: incomplete Container DTO: %#v", c)
		}
	}
	if !sawPlan {
		t.Error("ListContainers: expected at least one KindTestPlan container")
	}
	if !sawExec {
		t.Error("ListContainers: expected at least one KindTestExec container")
	}

	// --- ListRequirements: non-empty requirements AND links (both plugins
	// present, so this must not degrade to the EMPTY absent-plugin path).
	var reqProgress int
	reqs, reqLinks, err := a.ListRequirements(ctx, "DEMO", nil, func(done, total int) { reqProgress++ })
	if err != nil {
		t.Fatalf("ListRequirements: %v", err)
	}
	if len(reqs) == 0 {
		t.Fatal("ListRequirements: expected non-empty requirements")
	}
	if len(reqLinks) == 0 {
		t.Fatal("ListRequirements: expected non-empty requirement<->test links")
	}
	if reqProgress == 0 {
		t.Error("ListRequirements: onProgress was never called")
	}
	for _, r := range reqs {
		if r.Key == "" || r.Summary == "" || r.IssueType != "requirement" {
			t.Fatalf("ListRequirements: incomplete Requirement DTO: %#v", r)
		}
	}
	for _, l := range reqLinks {
		if l.TestKey == "" || l.RequirementKey == "" || l.LinkID == "" {
			t.Fatalf("ListRequirements: incomplete RequirementLink DTO: %#v", l)
		}
	}

	// --- ListIssueLinkTypes: the static typed-link vocabulary, since the
	// requirements plugin is present.
	lts, err := a.ListIssueLinkTypes(ctx)
	if err != nil {
		t.Fatalf("ListIssueLinkTypes: %v", err)
	}
	if len(lts) == 0 {
		t.Fatal("ListIssueLinkTypes: expected a non-empty static vocabulary")
	}

	// --- metadata lists: non-empty and sensible.
	statuses, err := a.ListStatuses(ctx, "DEMO")
	if err != nil || len(statuses) == 0 {
		t.Fatalf("ListStatuses: %v, %#v", err, statuses)
	}
	priorities, err := a.ListPriorities(ctx, "DEMO")
	if err != nil || len(priorities) == 0 {
		t.Fatalf("ListPriorities: %v, %#v", err, priorities)
	}
	components, err := a.ProjectComponents(ctx, "DEMO")
	if err != nil || len(components) == 0 {
		t.Fatalf("ProjectComponents: %v, %#v", err, components)
	}
	versions, err := a.ProjectVersions(ctx, "DEMO")
	if err != nil || len(versions) == 0 {
		t.Fatalf("ProjectVersions: %v, %#v", err, versions)
	}
}

// TestDemoTestExecutionsForTestAndGetTestRuns exercises the run-membership
// read paths against a specific demo test/run, confirming the demo's
// structural filters (case/run) are actually honored, not just "return
// everything".
func TestDemoTestExecutionsForTestAndGetTestRuns(t *testing.T) {
	a := newDemoAdapter(t)
	ctx := context.Background()

	// Case 1 is a member of plan 1 / run 102 in the fixed demo dataset.
	containers, links, err := a.TestExecutionsForTest(ctx, "1")
	if err != nil {
		t.Fatalf("TestExecutionsForTest: %v", err)
	}
	if len(containers) == 0 || len(links) == 0 {
		t.Fatalf("TestExecutionsForTest(1): expected non-empty result, got containers=%#v links=%#v", containers, links)
	}
	for _, l := range links {
		if l.TestKey != "1" {
			t.Errorf("TestExecutionsForTest(1): link for wrong test key %q", l.TestKey)
		}
	}

	runs, err := a.GetTestRuns(ctx, containers[0].Key)
	if err != nil {
		t.Fatalf("GetTestRuns(%s): %v", containers[0].Key, err)
	}
	if len(runs) == 0 {
		t.Fatalf("GetTestRuns(%s): expected non-empty runs", containers[0].Key)
	}

	plans, err := a.ExecPlans(ctx, containers[0].Key)
	if err != nil || len(plans) == 0 {
		t.Fatalf("ExecPlans(%s): %v, %#v", containers[0].Key, err, plans)
	}
}

// TestDemoRemoteVersionIsStable confirms RemoteVersion/RemoteAhead behave
// sanely (deterministic, non-empty, unequal for different tests) against
// the demo generator -- concurrency plumbing the sync engine depends on.
func TestDemoRemoteVersionIsStable(t *testing.T) {
	a := newDemoAdapter(t)
	ctx := context.Background()

	tok1, err := a.RemoteVersion(ctx, "test", "1")
	if err != nil || tok1 == "" {
		t.Fatalf("RemoteVersion(1): %v, %q", err, tok1)
	}
	tok1Again, err := a.RemoteVersion(ctx, "test", "1")
	if err != nil || tok1Again != tok1 {
		t.Fatalf("RemoteVersion(1) not stable across calls: %q vs %q", tok1, tok1Again)
	}
	tok2, err := a.RemoteVersion(ctx, "test", "2")
	if err != nil || tok2 == "" {
		t.Fatalf("RemoteVersion(2): %v, %q", err, tok2)
	}
	if tok1 == tok2 {
		t.Fatal("RemoteVersion(1) and RemoteVersion(2) collided, want distinct history_date tokens for distinct tests")
	}
	if !a.RemoteAhead(tok1, tok2) {
		t.Fatal("RemoteAhead should report true for two distinct history_date tokens")
	}
}
