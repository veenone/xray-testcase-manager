package xray

import (
	"context"
	"testing"

	"xray-test-manager/internal/jira"
)

// TestAdapterSmokeDemo proves the adapter delegates to the wrapped jira client
// and maps jira.* -> backend.* correctly, end to end, in demo mode. It does not
// re-test jira internals; it only checks that reads flow through and land in the
// neutral DTOs with their fields intact.
func TestAdapterSmokeDemo(t *testing.T) {
	a := New(jira.NewClient("demo", ""))
	ctx := context.Background()

	// Connection: the mapped User must carry the display name through.
	user, err := a.TestConnection(ctx)
	if err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	if user == nil || user.DisplayName == "" {
		t.Fatalf("expected a non-empty mapped user, got %+v", user)
	}

	// IsDemo must reflect the wrapped client.
	if !a.IsDemo() {
		t.Fatalf("expected IsDemo() == true for a demo client")
	}

	// Tests: the first demo page must be non-empty and mapped.
	tests, total, err := a.SearchTestsPage(ctx, "DEMO", "", "", 0, 50)
	if err != nil {
		t.Fatalf("SearchTestsPage: %v", err)
	}
	if len(tests) == 0 || total == 0 {
		t.Fatalf("expected seeded tests, got %d (total %d)", len(tests), total)
	}
	if tests[0].Key == "" || tests[0].Summary == "" {
		t.Fatalf("mapped Test missing key/summary: %+v", tests[0])
	}

	// ListTestsBasic maps nested IssueLinks, including the Priority field. The
	// first XRAYINT cross-project member carries a priced bug link in demo mode.
	basics, err := a.ListTestsBasic(ctx, []string{"XRAYINT-1"})
	if err != nil {
		t.Fatalf("ListTestsBasic: %v", err)
	}
	if len(basics) == 0 {
		t.Fatalf("expected a basic for XRAYINT-1")
	}
	if len(basics[0].IssueLinks) > 0 {
		if basics[0].IssueLinks[0].Priority == "" {
			t.Fatalf("mapped BugLinkRef dropped Priority: %+v", basics[0].IssueLinks[0])
		}
	}

	// Containers read maps both slices.
	containers, links, err := a.ListContainers(ctx, "DEMO", nil)
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if len(containers) == 0 {
		t.Fatalf("expected seeded containers, got 0")
	}
	if containers[0].Key == "" || containers[0].Kind == "" {
		t.Fatalf("mapped Container missing key/kind: %+v", containers[0])
	}
	if len(links) == 0 {
		t.Fatalf("expected seeded container links, got 0")
	}

	// Preconditions read maps the slice and the membership map.
	pcs, membership, err := a.ListPreconditions(ctx, "DEMO", nil)
	if err != nil {
		t.Fatalf("ListPreconditions: %v", err)
	}
	if len(pcs) == 0 {
		t.Fatalf("expected seeded preconditions, got 0")
	}
	if pcs[0].Key == "" || pcs[0].Summary == "" {
		t.Fatalf("mapped Precondition missing key/summary: %+v", pcs[0])
	}
	if membership == nil {
		t.Fatalf("expected a non-nil precondition membership map")
	}

	// Capabilities reports the full Xray set.
	caps := a.Capabilities()
	if caps.Name != "xray" || !caps.SupportsContainers || caps.SupportsTags {
		t.Fatalf("unexpected capabilities: %+v", caps)
	}
	if len(caps.ContainerKinds) != 3 {
		t.Fatalf("expected 3 container kinds, got %v", caps.ContainerKinds)
	}
}
