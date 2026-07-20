package bridge_test

import (
	"reflect"
	"testing"

	"xray-test-manager/internal/backend"
	"xray-test-manager/internal/backend/kiwi"
	"xray-test-manager/internal/backend/xray"
	"xray-test-manager/internal/bridge"
)

// featureKeys extracts the Feature keys from a Gap slice, in order, so tests
// can assert both membership and order without repeating Message text.
func featureKeys(gaps []bridge.Gap) []string {
	out := make([]string, len(gaps))
	for i, g := range gaps {
		out[i] = g.Feature
	}
	return out
}

// TestComputeGaps_XrayToKiwi exercises ComputeGaps against the two real
// backends' actual Capabilities() (Xray as source, Kiwi without the
// requirements plugin as target — the common publish direction, spec
// p6_3-bridge-spec.md). Both constructors are pure (Capabilities() never
// touches the underlying client), so this needs no network and no demo mode.
func TestComputeGaps_XrayToKiwi(t *testing.T) {
	source := xray.New(nil).Capabilities()
	target := kiwi.NewFromClient(nil).Capabilities()

	gaps := bridge.ComputeGaps(source, target)

	want := []string{
		"folders",
		"steps",
		"statusModel",
		"preconditions",
		"requirements",
		"issueLinkTypes",
		"bugCreation",
		"containerKind:testset",
	}
	got := featureKeys(gaps)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("gap features = %v, want %v (in this exact order)", got, want)
	}

	// Kiwi's real Capabilities() already supports environments and test types
	// (Build ~= environment; is_automated ~= test type), so those must NOT be
	// reported as gaps for this pair — only the rule's presence matters, which
	// TestComputeGaps_AllRulesFire below verifies with a target that lacks them.
	for _, unexpected := range []string{"environments", "testTypes", "tags", "testRuns"} {
		for _, f := range got {
			if f == unexpected {
				t.Errorf("unexpected gap %q: Kiwi supports this capability, so it should not be flagged lossy", unexpected)
			}
		}
	}

	// Spot-check severities and that messages are non-empty.
	bySeverity := map[string]string{}
	for _, g := range gaps {
		bySeverity[g.Feature] = g.Severity
		if g.Message == "" {
			t.Errorf("gap %q has an empty message", g.Feature)
		}
	}
	wantSeverity := map[string]string{
		"folders":               bridge.SeverityLossy,
		"steps":                 bridge.SeverityLossy,
		"statusModel":           bridge.SeverityInfo,
		"preconditions":         bridge.SeverityLossy,
		"requirements":          bridge.SeverityLossy,
		"issueLinkTypes":        bridge.SeverityLossy,
		"bugCreation":           bridge.SeverityInfo,
		"containerKind:testset": bridge.SeverityLossy,
	}
	for feature, wantSev := range wantSeverity {
		if bySeverity[feature] != wantSev {
			t.Errorf("gap %q severity = %q, want %q", feature, bySeverity[feature], wantSev)
		}
	}
}

// TestComputeGaps_XrayToXray confirms identical capabilities (publishing to
// another connection on the SAME backend) produce no gaps at all.
func TestComputeGaps_XrayToXray(t *testing.T) {
	caps := xray.New(nil).Capabilities()
	gaps := bridge.ComputeGaps(caps, caps)
	if len(gaps) != 0 {
		t.Fatalf("Xray -> Xray gaps = %v, want none", gaps)
	}
}

// TestComputeGaps_AllRulesFire uses hand-built Capabilities (a source that
// supports everything, a target that supports nothing) so every rule
// ComputeGaps implements is exercised at least once, including the ones that
// don't currently fire for the real Xray/Kiwi pair (environments, test
// types, tags, test runs) because Kiwi happens to support those today.
func TestComputeGaps_AllRulesFire(t *testing.T) {
	source := backend.Capabilities{
		StepModel:                   "objects",
		SupportsTestTypes:           true,
		SupportsFolders:             true,
		SupportsPreconditionObjects: true,
		SupportsRequirementObjects:  true,
		SupportsIssueLinkTypes:      true,
		SupportsEnvironments:        true,
		SupportsContainers:          true,
		ContainerKinds:              []string{"testset", "testplan", "testexec"},
		SupportsTestRuns:            true,
		SupportsWorkflowTransitions: true,
		SupportsBugCreation:         true,
		SupportsTags:                true,
	}
	target := backend.Capabilities{StepModel: "inline-text"} // everything else zero-value

	gaps := bridge.ComputeGaps(source, target)
	want := []string{
		"folders", "steps", "statusModel", "preconditions", "requirements",
		"issueLinkTypes", "environments", "testTypes", "tags", "bugCreation",
		"testRuns", "containerKind:testexec", "containerKind:testplan", "containerKind:testset",
	}
	got := featureKeys(gaps)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("gap features = %v, want %v", got, want)
	}
}

// TestComputeGaps_Deterministic runs ComputeGaps twice on equivalent inputs
// (ContainerKinds supplied in a different order each time) and checks the
// output is identical both times — the container-kind gaps must be sorted,
// not dependent on input slice order.
func TestComputeGaps_Deterministic(t *testing.T) {
	target := backend.Capabilities{ContainerKinds: []string{"c"}}
	source1 := backend.Capabilities{ContainerKinds: []string{"b", "a", "c"}}
	source2 := backend.Capabilities{ContainerKinds: []string{"a", "c", "b"}}

	gaps1 := bridge.ComputeGaps(source1, target)
	gaps2 := bridge.ComputeGaps(source2, target)
	if !reflect.DeepEqual(gaps1, gaps2) {
		t.Fatalf("gaps not deterministic across equivalent input order: %v vs %v", gaps1, gaps2)
	}
	want := []string{"containerKind:a", "containerKind:b"}
	if got := featureKeys(gaps1); !reflect.DeepEqual(got, want) {
		t.Fatalf("gap features = %v, want %v", got, want)
	}

	// Running it again with the identical input must also be stable.
	gaps1b := bridge.ComputeGaps(source1, target)
	if !reflect.DeepEqual(gaps1, gaps1b) {
		t.Fatalf("ComputeGaps not stable across repeated calls with the same input")
	}
}

func TestDefaultMapping_NameMatchAndFallback(t *testing.T) {
	source := xray.New(nil).Capabilities()
	target := kiwi.NewFromClient(nil).Capabilities()

	sourceStatuses := []string{"TODO", "PASS", "FAIL"}
	targetStatuses := []string{"fail", "pass", "idle"}

	m := bridge.DefaultMapping(source, target, sourceStatuses, targetStatuses)

	wantStatusMap := map[string]string{
		"TODO": "fail", // no case-insensitive match -> deterministic fallback: first target status
		"PASS": "pass", // case-insensitive name match
		"FAIL": "fail", // case-insensitive name match
	}
	if !reflect.DeepEqual(m.StatusMap, wantStatusMap) {
		t.Errorf("StatusMap = %v, want %v", m.StatusMap, wantStatusMap)
	}
	if m.UnmappedPolicy != bridge.UnmappedPolicyKeepInHub {
		t.Errorf("UnmappedPolicy = %q, want %q", m.UnmappedPolicy, bridge.UnmappedPolicyKeepInHub)
	}
	if m.StepMode != bridge.StepModeFlatten {
		t.Errorf("StepMode = %q, want %q (target StepModel is inline-text)", m.StepMode, bridge.StepModeFlatten)
	}
}

func TestDefaultMapping_StepModePassthroughForObjectTarget(t *testing.T) {
	source := kiwi.NewFromClient(nil).Capabilities()
	target := xray.New(nil).Capabilities()

	m := bridge.DefaultMapping(source, target, nil, nil)
	if m.StepMode != bridge.StepModePassthrough {
		t.Errorf("StepMode = %q, want %q (target StepModel is objects)", m.StepMode, bridge.StepModePassthrough)
	}
}

func TestDefaultMapping_EmptyTargetStatusesMapToBlank(t *testing.T) {
	source := xray.New(nil).Capabilities()
	target := kiwi.NewFromClient(nil).Capabilities()

	m := bridge.DefaultMapping(source, target, []string{"Open", "Closed"}, nil)
	want := map[string]string{"Open": "", "Closed": ""}
	if !reflect.DeepEqual(m.StatusMap, want) {
		t.Errorf("StatusMap = %v, want %v", m.StatusMap, want)
	}
}

func TestDefaultMapping_Deterministic(t *testing.T) {
	source := xray.New(nil).Capabilities()
	target := kiwi.NewFromClient(nil).Capabilities()
	sourceStatuses := []string{"TODO", "PASS", "FAIL"}
	targetStatuses := []string{"fail", "pass", "idle"}

	m1 := bridge.DefaultMapping(source, target, sourceStatuses, targetStatuses)
	m2 := bridge.DefaultMapping(source, target, sourceStatuses, targetStatuses)
	if !reflect.DeepEqual(m1, m2) {
		t.Fatalf("DefaultMapping not deterministic: %v vs %v", m1, m2)
	}
}
