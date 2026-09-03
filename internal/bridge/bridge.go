// Package bridge computes the capability-gap report and the default field/
// status/step mapping used before publishing a dataset from one backend
// connection (the source) to another (the target) — Phase 6 bridge task B4.
// It is pure logic: no I/O, no store access beyond the small MappingStore in
// store.go. The publish engine that consumes a Mapping (B5) and the wizard UI
// that presents a Gap report (B6) are later tasks; this package only builds
// the report and the default mapping.
package bridge

import (
	"fmt"
	"sort"
	"strings"

	"xray-test-manager/internal/backend"
)

// Gap severities. "blocking" is reserved for a future case where publishing
// cannot proceed at all (none of the current rules produce one — every gap
// computed today is either "lossy" or "info"); ComputeGaps never blocks a
// publish outright, but the type exists so a future rule can without a
// breaking change to the Gap shape.
const (
	SeverityBlocking = "blocking"
	SeverityLossy    = "lossy"
	SeverityInfo     = "info"
)

// Gap describes one way the target backend cannot fully represent something
// the source backend supports. Feature is a stable machine key (used by the
// UI/tests to identify a specific gap regardless of message wording); Message
// is the human-readable explanation shown in the pre-flight report.
type Gap struct {
	Feature  string `json:"feature"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// ComputeGaps compares a source and target backend's static Capabilities and
// returns every gap the user must acknowledge before publishing from source
// to target. Nothing here mutates or reads a dataset — it is a pure
// capability diff, safe to call with no network access (Capabilities() is a
// static struct on every backend). The result is in a fixed, deterministic
// order: the checks always run in the same sequence, and the one check that
// can produce more than one gap (container kinds) sorts its output.
func ComputeGaps(source, target backend.Capabilities) []Gap {
	var gaps []Gap

	// Two different folder gaps. A target with no folder concept at all loses
	// placement entirely. A target that reports folders but cannot be reshaped
	// (Kiwi, whose per-product Categories are flat and not creatable through
	// this adapter) keeps a test's folder only where a matching one already
	// exists, and flattens any nesting.
	switch {
	case source.SupportsFolders && !target.SupportsFolders:
		gaps = append(gaps, Gap{
			Feature:  "folders",
			Severity: SeverityLossy,
			Message:  "Test Repository folder placement is not preserved — tests will be published without a folder location.",
		})
	case source.SupportsFolders && !target.SupportsFolderWrites:
		gaps = append(gaps, Gap{
			Feature:  "folders",
			Severity: SeverityLossy,
			Message:  "The target reports folders but cannot create them, and its folders do not nest — tests land in a matching existing folder or none, and any hierarchy is flattened.",
		})
	}
	if source.StepModel == "objects" && target.StepModel == "inline-text" {
		gaps = append(gaps, Gap{
			Feature:  "steps",
			Severity: SeverityLossy,
			Message:  "Step objects (action / data / expected result) will be flattened into a single inline text field.",
		})
	}
	if source.SupportsWorkflowTransitions && !target.SupportsWorkflowTransitions {
		gaps = append(gaps, Gap{
			Feature:  "statusModel",
			Severity: SeverityInfo,
			Message:  "Workflow-driven statuses will be mapped to the target's settable status field — see status mapping.",
		})
	}
	if source.SupportsPreconditionObjects && !target.SupportsPreconditionObjects {
		gaps = append(gaps, Gap{
			Feature:  "preconditions",
			Severity: SeverityLossy,
			Message:  "Preconditions will not be published as first-class objects in the target.",
		})
	}
	if source.SupportsRequirementObjects && !target.SupportsRequirementObjects {
		gaps = append(gaps, Gap{
			Feature:  "requirements",
			Severity: SeverityLossy,
			Message:  "Requirements will not be published as first-class objects in the target.",
		})
	}
	if source.SupportsIssueLinkTypes && !target.SupportsIssueLinkTypes {
		gaps = append(gaps, Gap{
			Feature:  "issueLinkTypes",
			Severity: SeverityLossy,
			Message:  "Typed issue links will not be preserved — the target has no issue-link-type model.",
		})
	}
	if source.SupportsEnvironments && !target.SupportsEnvironments {
		gaps = append(gaps, Gap{
			Feature:  "environments",
			Severity: SeverityLossy,
			Message:  "Execution environments will be dropped — the target has no environment field.",
		})
	}
	if source.SupportsTestTypes && !target.SupportsTestTypes {
		gaps = append(gaps, Gap{
			Feature:  "testTypes",
			Severity: SeverityInfo,
			Message:  "Test type (Manual / Automated / ...) will not be preserved as a distinct field.",
		})
	}
	if source.SupportsTags && !target.SupportsTags {
		gaps = append(gaps, Gap{
			Feature:  "tags",
			Severity: SeverityLossy,
			Message:  "Tags will not be preserved — the target has no tag model.",
		})
	}
	if source.SupportsBugCreation && !target.SupportsBugCreation {
		gaps = append(gaps, Gap{
			Feature:  "bugCreation",
			Severity: SeverityInfo,
			Message:  "Defects will not be created as native issues in the target.",
		})
	}
	if source.SupportsTestRuns && !target.SupportsTestRuns {
		gaps = append(gaps, Gap{
			Feature:  "testRuns",
			Severity: SeverityLossy,
			Message:  "Test run history will not be published to the target.",
		})
	}

	// Container kinds the source uses that the target has no equivalent for
	// (e.g. Xray Test Sets when the target is Kiwi, which has no set concept).
	// Sorted so the result is deterministic regardless of ContainerKinds order.
	var missingKinds []string
	for _, k := range source.ContainerKinds {
		if !containsString(target.ContainerKinds, k) {
			missingKinds = append(missingKinds, k)
		}
	}
	sort.Strings(missingKinds)
	for _, k := range missingKinds {
		gaps = append(gaps, Gap{
			Feature:  "containerKind:" + k,
			Severity: SeverityLossy,
			Message:  fmt.Sprintf("Container kind %q is not supported by the target — those containers will be skipped.", k),
		})
	}

	return gaps
}

func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// Mapping is the reversible field/step/status mapping used when publishing
// from a source connection to a target connection. It is persisted as JSON in
// the bridge_mapping table (store.go) and consumed by the publish engine (B5)
// — this package only builds sensible defaults; the mapping wizard UI (B6)
// lets the user edit it.
type Mapping struct {
	// StatusMap maps a source status name to a target status name.
	StatusMap map[string]string `json:"statusMap"`
	// StepMode is "flatten" (collapse step objects into one text field, for a
	// target with StepModel=="inline-text") or "passthrough" (target also
	// models steps as objects).
	StepMode string `json:"stepMode"`
	// FieldMap maps a source field key to a target field key, for fields whose
	// name/shape differs between backends. Empty by default — DefaultMapping
	// does not attempt automatic field-name inference beyond status.
	FieldMap map[string]string `json:"fieldMap"`
	// UnmappedPolicy governs what happens to data the target can't represent:
	// "drop" discards it, "keepInHub" keeps it in the local store only (never
	// sent to the target, but not lost from the workspace either).
	UnmappedPolicy string `json:"unmappedPolicy"`
}

// Step mode values.
const (
	StepModeFlatten     = "flatten"
	StepModePassthrough = "passthrough"
)

// Unmapped-field policy values.
const (
	UnmappedPolicyDrop      = "drop"
	UnmappedPolicyKeepInHub = "keepInHub"
)

// DefaultMapping builds a sensible starting Mapping for a source/target
// backend pair, given the two backends' status lists (from ListStatuses).
// StatusMap matches each source status to a target status by case-insensitive
// name (trimmed); when no name match exists it falls back to the first entry
// of targetStatuses (deterministic — same inputs always produce the same
// map). When targetStatuses is empty every source status maps to "" so the
// caller/UI can prompt the user to pick a target status before publish.
// StepMode is "flatten" whenever the target's StepModel is "inline-text",
// "passthrough" otherwise. UnmappedPolicy always defaults to "keepInHub" —
// the safer default (nothing is silently discarded; the user can still choose
// "drop" per field later in the mapping editor, B6).
func DefaultMapping(source, target backend.Capabilities, sourceStatuses, targetStatuses []string) Mapping {
	m := Mapping{
		StatusMap:      map[string]string{},
		FieldMap:       map[string]string{},
		UnmappedPolicy: UnmappedPolicyKeepInHub,
	}
	if target.StepModel == "inline-text" {
		m.StepMode = StepModeFlatten
	} else {
		m.StepMode = StepModePassthrough
	}
	_ = source // source capabilities aren't needed by the current rules, but
	// kept in the signature: a future default (e.g. source-specific field
	// hints) can use it without an API change.

	if len(targetStatuses) == 0 {
		for _, s := range sourceStatuses {
			m.StatusMap[s] = ""
		}
		return m
	}

	fallback := targetStatuses[0]
	for _, s := range sourceStatuses {
		mapped := fallback
		for _, t := range targetStatuses {
			if strings.EqualFold(strings.TrimSpace(s), strings.TrimSpace(t)) {
				mapped = t
				break
			}
		}
		m.StatusMap[s] = mapped
	}
	return m
}
