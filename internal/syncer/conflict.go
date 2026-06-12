package syncer

import (
	"context"
	"sort"
	"strings"

	"xray-test-manager/internal/jira"
	"xray-test-manager/internal/testrepo"
)

// ConflictField is one field (or step field) that changed both locally and
// upstream since the local edit's base — the three sides shown in the
// resolution UI (FR-1.4, conflict management phase 1).
type ConflictField struct {
	PendingID  int64  `json:"pendingId"`
	EntityType string `json:"entityType"`
	EntityKey  string `json:"entityKey"`
	Field      string `json:"field"`
	Label      string `json:"label"`
	Base       string `json:"base"`   // value the local edit started from
	Remote     string `json:"remote"` // current value in Jira (someone else's change)
	Mine       string `json:"mine"`   // my local edit
}

// conflictScan is the outcome of comparing a Test's pending changes against the
// current remote state.
type conflictScan struct {
	conflicts   []ConflictField // genuinely overlapping edits — need a decision
	dropIDs     []int64         // changes already satisfied remotely (someone made the same edit)
	deleted     bool            // the Test no longer exists in Jira
	testSummary string
}

// detectConflicts fetches the current remote field/step values for a Test and
// classifies each pending change three ways: already-applied (remote == mine →
// drop), clean (remote == base, or a type we don't conflict-check this phase →
// auto-merge), or a true conflict (remote diverged from both). Only true
// conflicts are returned; the caller auto-merges everything else.
func (e *Engine) detectConflicts(ctx context.Context, testKey string, changes []testrepo.PendingChange) (conflictScan, error) {
	var scan conflictScan

	remoteTest, err := e.client.GetTestFields(ctx, testKey)
	if err != nil {
		if isNotFoundErr(err) {
			scan.deleted = true
			return scan, nil
		}
		return scan, err
	}
	scan.testSummary = remoteTest.Summary

	// Remote steps are only needed when there are step-content edits.
	var remoteSteps map[string]jira.Step
	for _, c := range changes {
		if c.EntityType == "test_step" {
			steps, serr := e.client.GetTestSteps(ctx, testKey)
			if serr != nil {
				return scan, serr
			}
			remoteSteps = make(map[string]jira.Step, len(steps))
			for _, s := range steps {
				remoteSteps[s.ID] = s
			}
			break
		}
	}

	for _, c := range changes {
		base, mine, remote, label, checked := conflictTriple(c, remoteTest, remoteSteps)
		if !checked {
			continue // not conflict-checked this phase — auto-merge
		}
		switch {
		case remote == mine:
			scan.dropIDs = append(scan.dropIDs, c.ID)
		case remote == base:
			// clean — my edit applies on top of the remote unchanged for this field
		default:
			scan.conflicts = append(scan.conflicts, ConflictField{
				PendingID:  c.ID,
				EntityType: c.EntityType,
				EntityKey:  c.EntityKey,
				Field:      c.Field,
				Label:      label,
				Base:       base,
				Remote:     remote,
				Mine:       mine,
			})
		}
	}
	return scan, nil
}

// conflictTriple returns the (base, mine, remote) values plus a display label
// for a pending change, and whether this change type/field is conflict-checked
// in phase 1 (Test standard fields + step content). Unchecked types report
// checked=false so the caller auto-merges them.
func conflictTriple(c testrepo.PendingChange, t jira.Test, steps map[string]jira.Step) (base, mine, remote, label string, checked bool) {
	base, mine = c.BeforeVal, c.AfterVal
	switch c.EntityType {
	case "test_case":
		switch c.Field {
		case "summary":
			return base, mine, t.Summary, "Summary", true
		case "description":
			return base, mine, t.Description, "Description", true
		case "priority":
			return base, mine, t.Priority, "Priority", true
		case "status":
			return base, mine, t.Status, "Status", true
		case "labels":
			return normalizeLabels(base), normalizeLabels(mine),
				normalizeLabels(strings.Join(t.Labels, " ")), "Labels", true
		}
	case "test_step":
		_, xrayID, ok := parseStepKey(c.EntityKey)
		if !ok {
			return base, mine, "", "", false
		}
		label := "Step · " + stepFieldLabel(c.Field)
		s, exists := steps[xrayID]
		if !exists {
			return base, mine, "— deleted in Jira —", label, true
		}
		switch c.Field {
		case "action":
			return base, mine, s.Action, label, true
		case "data":
			return base, mine, s.Data, label, true
		case "expected":
			return base, mine, s.Expected, label, true
		}
	}
	return base, mine, "", "", false
}

// normalizeLabels makes a label set comparable regardless of order/spacing:
// split on whitespace, sort, rejoin (Jira labels can't contain spaces).
func normalizeLabels(s string) string {
	fields := strings.Fields(s)
	sort.Strings(fields)
	return strings.Join(fields, " ")
}

func stepFieldLabel(field string) string {
	switch field {
	case "action":
		return "Action"
	case "data":
		return "Data"
	case "expected":
		return "Expected Result"
	}
	return field
}

// isNotFoundErr reports whether a Jira client error is a 404 (the issue was
// deleted upstream).
func isNotFoundErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), " 404")
}
