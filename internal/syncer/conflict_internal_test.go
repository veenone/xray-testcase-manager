package syncer

import (
	"testing"

	"xray-test-manager/internal/jira"
	"xray-test-manager/internal/testrepo"
)

// TestConflictTripleCustomFieldPresenceGate verifies custom-field detection and
// its presence gate: present-and-diverged is conflict-checked; absent (the
// stubbed remote returns nothing) auto-merges instead of false-positiving.
func TestConflictTripleCustomFieldPresenceGate(t *testing.T) {
	c := testrepo.PendingChange{
		EntityType: "custom_field", EntityKey: "QA-1:cf1", Field: "value",
		BeforeVal: "Manual", AfterVal: "Automated",
	}
	base, mine, remote, label, checked := conflictTriple(c, jira.Test{}, nil, map[string]string{"cf1": "Generic"})
	if !checked || base != "Manual" || mine != "Automated" || remote != "Generic" || label != "Custom field" {
		t.Fatalf("present: base=%q mine=%q remote=%q label=%q checked=%v", base, mine, remote, label, checked)
	}
	if _, _, _, _, checked2 := conflictTriple(c, jira.Test{}, nil, map[string]string{}); checked2 {
		t.Errorf("absent custom field must not be conflict-checked (presence gate)")
	}
}
