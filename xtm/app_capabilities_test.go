package main

import (
	"testing"
)

// TestBugRoutingCapabilityFollowsTheConnection pins the flag's meaning: it
// reports configuration, not adapter ability. A Kiwi profile with no bug
// connection reports both flags false; adding the connection flips ONLY
// SupportsBugRouting, because Kiwi still cannot create a Jira issue itself.
func TestBugRoutingCapabilityFollowsTheConnection(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)

	caps, err := a.GetCapabilities(profileID)
	if err != nil {
		t.Fatalf("capabilities: %v", err)
	}
	if caps.SupportsBugRouting {
		t.Error("SupportsBugRouting = true with no bug connection configured")
	}
	if caps.SupportsBugCreation {
		t.Error("SupportsBugCreation = true, want false: Kiwi cannot create Jira issues")
	}

	if _, err := a.SaveBugConnection(profileID, "https://jira.example.com", "DEF", "Bug", "tok", "", false); err != nil {
		t.Fatalf("save bug connection: %v", err)
	}

	caps, err = a.GetCapabilities(profileID)
	if err != nil {
		t.Fatalf("capabilities after routing: %v", err)
	}
	if !caps.SupportsBugRouting {
		t.Error("SupportsBugRouting = false after configuring a bug connection")
	}
	if caps.SupportsBugCreation {
		t.Error("SupportsBugCreation flipped to true; the adapter's own ability did not change")
	}
}
