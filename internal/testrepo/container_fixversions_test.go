package testrepo

import (
	"testing"
)

// TestUpsertContainersFixVersionsRoundTrip seeds a Test Execution with Jira Fix
// Version(s) via UpsertContainers and asserts the list/detail path returns them,
// preserving the supplied order (they are read-only display values, not sorted).
// Test Sets / Plans carry none.
func TestUpsertContainersFixVersionsRoundTrip(t *testing.T) {
	r := newTestRepo(t)
	const p = "p1"

	if err := r.UpsertContainers(p, []Container{
		{Key: "EX-1", Kind: "testexec", Summary: "Cycle 1", Status: "Open", FixVersions: []string{"1.6.0", "1.5.0"}},
		{Key: "EX-2", Kind: "testexec", Summary: "Cycle 2", Status: "Open"},
		{Key: "TS-1", Kind: "testset", Summary: "A set", Status: "Open"},
	}); err != nil {
		t.Fatalf("UpsertContainers: %v", err)
	}

	got, err := r.ListContainers(p, "testexec")
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	byKey := map[string]Container{}
	for _, c := range got {
		byKey[c.Key] = c
	}

	fv := byKey["EX-1"].FixVersions
	if len(fv) != 2 || fv[0] != "1.6.0" || fv[1] != "1.5.0" {
		t.Errorf("EX-1 FixVersions = %v, want [1.6.0 1.5.0] in order", fv)
	}
	if len(byKey["EX-2"].FixVersions) != 0 {
		t.Errorf("EX-2 should have no fix versions, got %v", byKey["EX-2"].FixVersions)
	}

	// A resync (another UpsertContainers) overwrites fix versions unconditionally
	// (no pending-change guard, since they are never edited locally).
	if err := r.UpsertContainers(p, []Container{
		{Key: "EX-1", Kind: "testexec", Summary: "Cycle 1", Status: "Open", FixVersions: []string{"2.0.0"}},
	}); err != nil {
		t.Fatalf("UpsertContainers resync: %v", err)
	}
	got, err = r.ListContainers(p, "testexec")
	if err != nil {
		t.Fatalf("ListContainers after resync: %v", err)
	}
	for _, c := range got {
		if c.Key == "EX-1" {
			if len(c.FixVersions) != 1 || c.FixVersions[0] != "2.0.0" {
				t.Errorf("EX-1 FixVersions after resync = %v, want [2.0.0]", c.FixVersions)
			}
		}
	}
}
