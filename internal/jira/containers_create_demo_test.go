package jira

import (
	"context"
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

// TestCreateContainerDemoSameSummaryIsDeterministic verifies that calling
// CreateContainer twice with the same summary yields the same key, since the
// demo client is stateless and must stay deterministic across calls.
func TestCreateContainerDemoSameSummaryIsDeterministic(t *testing.T) {
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
	if first != second {
		t.Fatalf("same summary produced different keys: %q vs %q", first, second)
	}
}
