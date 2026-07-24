package main

import (
	"errors"
	"testing"

	"xray-test-manager/internal/backend"
)

// TestCoveragePublishReturnsErrUnsupportedOnKiwi verifies both
// PublishCoverageGroups and GetCoveragePublishStatus refuse a backend with no
// Test Set container kind (Kiwi exposes TestPlan/TestRun but no Test Set)
// instead of attempting the operation, matching the capability-gating
// contract in requireContainerCapability.
func TestCoveragePublishReturnsErrUnsupportedOnKiwi(t *testing.T) {
	a := newTestApp(t)

	p, err := a.CreateProfile("Kiwi Profile", "https://kiwi.example.com", "LAB",
		"", "Bug", "test", "", "user:pass", "", false, "kiwi")
	if err != nil {
		t.Fatalf("CreateProfile(kiwi): %v", err)
	}

	if _, err := a.PublishCoverageGroups(p.ID, "v1"); !errors.Is(err, backend.ErrUnsupported) {
		t.Errorf("PublishCoverageGroups on a Kiwi profile: err = %v, want backend.ErrUnsupported", err)
	}

	if _, err := a.GetCoveragePublishStatus(p.ID, "v1"); !errors.Is(err, backend.ErrUnsupported) {
		t.Errorf("GetCoveragePublishStatus on a Kiwi profile: err = %v, want backend.ErrUnsupported", err)
	}
}
