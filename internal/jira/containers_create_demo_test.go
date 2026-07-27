package jira

import (
	"context"
	"strings"
	"testing"
)

// TestCreateContainerDemoReturnsNonEmptyKey verifies that CreateContainer on a
// demo client never returns the empty key that used to make the coverage
// publish engine report every group as a failure.
func TestCreateContainerDemoReturnsNonEmptyKey(t *testing.T) {
	c := NewClient("demo", "token")

	key, err := c.CreateContainer(context.Background(), "DEMO", KindTestSet, "Login Feature v1.0 - Group A")
	if err != nil {
		t.Fatalf("CreateContainer: %v", err)
	}
	if key == "" {
		t.Fatal("CreateContainer returned an empty key in demo mode")
	}
}

// TestCreateContainerDemoDistinctSummariesGetDistinctKeys verifies that two
// Test Sets with different summaries (as coverage publish produces, one per
// group) get different keys, so publishing multiple groups can never write
// memberships into the same container.
func TestCreateContainerDemoDistinctSummariesGetDistinctKeys(t *testing.T) {
	c := NewClient("demo", "token")
	ctx := context.Background()

	keyA, err := c.CreateContainer(ctx, "DEMO", KindTestSet, "Login Feature v1.0 - Group A")
	if err != nil {
		t.Fatalf("CreateContainer group A: %v", err)
	}
	keyB, err := c.CreateContainer(ctx, "DEMO", KindTestSet, "Login Feature v1.0 - Group B")
	if err != nil {
		t.Fatalf("CreateContainer group B: %v", err)
	}
	if keyA == keyB {
		t.Fatalf("distinct summaries produced the same key %q", keyA)
	}
}

// TestCreateContainerDemoSameSummaryGetsDistinctKeys verifies that calling
// CreateContainer twice with the SAME summary still yields two different
// keys. The board Test Set create flow lets a user create two containers of
// the same kind and project with an identical summary (CreateContainerAllocation
// in internal/testrepo has no uniqueness validation), and commitContainerCreate
// renames a local placeholder container to whatever key CreateContainer
// returns. If two calls with the same summary ever returned the same key
// again, the second rename would collide with the first on test_container's
// (profile_id, jira_key) primary key.
func TestCreateContainerDemoSameSummaryGetsDistinctKeys(t *testing.T) {
	c := NewClient("demo", "token")
	ctx := context.Background()

	first, err := c.CreateContainer(ctx, "DEMO", KindTestPlan, "Login Feature v1.0 - Group A")
	if err != nil {
		t.Fatalf("CreateContainer (first): %v", err)
	}
	second, err := c.CreateContainer(ctx, "DEMO", KindTestPlan, "Login Feature v1.0 - Group A")
	if err != nil {
		t.Fatalf("CreateContainer (second): %v", err)
	}
	if first == second {
		t.Fatalf("same summary produced the same key twice: %q", first)
	}
}

// TestCreateContainerDemoKeyShape verifies the returned key carries the
// distinct per-kind infix (so a demo-created key can never collide with a
// ListContainers-generated -TS-/-TP-/-TE- key or a generated test key) and
// defaults an empty projectKey to "DEMO".
func TestCreateContainerDemoKeyShape(t *testing.T) {
	c := NewClient("demo", "token")
	ctx := context.Background()

	key, err := c.CreateContainer(ctx, "", KindTestExec, "Login Feature v1.0 - Group A")
	if err != nil {
		t.Fatalf("CreateContainer: %v", err)
	}
	if !strings.HasPrefix(key, "DEMO-") {
		t.Fatalf("expected key to default projectKey to DEMO, got %q", key)
	}
	if !strings.Contains(key, "CVTE") {
		t.Fatalf("expected key to carry the testexec infix CVTE, got %q", key)
	}
}
