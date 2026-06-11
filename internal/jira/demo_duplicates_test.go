package jira

import "testing"

func TestDemoSeedsDuplicateClusters(t *testing.T) {
	// Indices 0,1 share a summary AND identical steps; 2,3 share a summary but
	// differ in steps.
	t0 := makeDemoTest("DEMO", 0)
	t1 := makeDemoTest("DEMO", 1)
	t2 := makeDemoTest("DEMO", 2)
	t3 := makeDemoTest("DEMO", 3)

	if t0.Summary != t1.Summary {
		t.Errorf("indices 0,1 should share a summary: %q vs %q", t0.Summary, t1.Summary)
	}
	if t2.Summary != t3.Summary {
		t.Errorf("indices 2,3 should share a summary: %q vs %q", t2.Summary, t3.Summary)
	}
	if t0.Summary == t2.Summary {
		t.Error("the two clusters should have different summaries")
	}

	fp := func(steps []Step) string {
		out := ""
		for _, s := range steps {
			out += s.Action + "|" + s.Data + "|" + s.Expected + "\n"
		}
		return out
	}
	if fp(demoStepsForKey(t0.Key)) != fp(demoStepsForKey(t1.Key)) {
		t.Error("cluster A (0,1) should have identical steps")
	}
	if fp(demoStepsForKey(t2.Key)) == fp(demoStepsForKey(t3.Key)) {
		t.Error("cluster B (2,3) should have differing steps")
	}
}
