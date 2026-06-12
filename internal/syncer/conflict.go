package syncer

import (
	"context"
	"encoding/json"
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

	// Remote steps are fetched only when a step change (content, delete, or
	// reorder) is present.
	var (
		remoteSteps map[string]jira.Step
		remoteOrder string // JSON id list in remote index order
	)
	needSteps := false
	for _, c := range changes {
		switch c.EntityType {
		case "test_step", "test_step_delete", "test_step_order":
			needSteps = true
		}
	}
	if needSteps {
		steps, serr := e.client.GetTestSteps(ctx, testKey)
		if serr != nil {
			return scan, serr
		}
		remoteSteps = make(map[string]jira.Step, len(steps))
		for _, s := range steps {
			remoteSteps[s.ID] = s
		}
		ordered := make([]jira.Step, len(steps))
		copy(ordered, steps)
		sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Index < ordered[j].Index })
		ids := make([]string, 0, len(ordered))
		for _, s := range ordered {
			ids = append(ids, s.ID)
		}
		if b, mErr := json.Marshal(ids); mErr == nil {
			remoteOrder = string(b)
		}
	}

	for _, c := range changes {
		switch c.EntityType {
		case "test_step_order":
			e.classifyOrder(c, remoteOrder, &scan)
			continue
		case "test_step_delete":
			e.classifyDelete(c, remoteSteps, &scan)
			continue
		}
		base, mine, remote, label, checked := conflictTriple(c, remoteTest, remoteSteps, nil)
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

// classifyDelete handles a local step deletion: drop it when the remote already
// deleted the step, keep it silent when the remote step is unchanged since my
// base, and conflict when I'm deleting a step someone else edited.
func (e *Engine) classifyDelete(c testrepo.PendingChange, steps map[string]jira.Step, scan *conflictScan) {
	_, xrayID, ok := parseStepKey(c.EntityKey)
	if !ok {
		return // malformed — leave it for the commit pass to report
	}
	var base testrepo.Step
	_ = json.Unmarshal([]byte(c.BeforeVal), &base)

	rs, exists := steps[xrayID]
	if !exists {
		scan.dropIDs = append(scan.dropIDs, c.ID) // remote also deleted it
		return
	}
	if rs.Action == base.Action && rs.Data == base.Data && rs.Expected == base.Expected {
		return // remote unchanged since my base — safe to delete (clean)
	}
	// I'm deleting a step that was edited upstream — let the user decide. The
	// remote snapshot doubles as the value to restore on "keep theirs".
	remoteSnap := testrepo.Step{
		XrayID: xrayID, Index: rs.Index,
		Action: rs.Action, Data: rs.Data, Expected: rs.Expected,
		CalledTestKey: rs.CalledTestKey,
	}
	rb, _ := json.Marshal(remoteSnap)
	scan.conflicts = append(scan.conflicts, ConflictField{
		PendingID:  c.ID,
		EntityType: c.EntityType,
		EntityKey:  c.EntityKey,
		Field:      c.Field,
		Label:      "Step you deleted (edited in Jira)",
		Base:       stepSummary(base.Action, base.Data, base.Expected),
		Remote:     string(rb),
		Mine:       "Delete this step",
	})
}

// stepSummary is a short one-line description of a step for the conflict table.
func stepSummary(action, data, expected string) string {
	s := strings.TrimSpace(action)
	if s == "" {
		s = strings.TrimSpace(expected)
	}
	if len([]rune(s)) > 80 {
		s = string([]rune(s)[:80]) + "…"
	}
	return s
}

// classifyOrder handles a step-reorder change: compare the remote order to the
// order I started from (clean) and the order I want (already applied).
func (e *Engine) classifyOrder(c testrepo.PendingChange, remoteOrder string, scan *conflictScan) {
	switch remoteOrder {
	case c.AfterVal:
		scan.dropIDs = append(scan.dropIDs, c.ID) // remote already in my order
	case c.BeforeVal, "":
		// remote order unchanged from my base (or unknown) — apply my reorder
	default:
		// Raw JSON id lists: the resolver re-applies the chosen order verbatim,
		// and the modal renders the (short) list in a scrollable cell.
		scan.conflicts = append(scan.conflicts, ConflictField{
			PendingID:  c.ID,
			EntityType: c.EntityType,
			EntityKey:  c.EntityKey,
			Field:      c.Field,
			Label:      "Step order",
			Base:       c.BeforeVal,
			Remote:     remoteOrder,
			Mine:       c.AfterVal,
		})
	}
}

// conflictTriple returns the (base, mine, remote) values plus a display label
// for a pending change, and whether this change type/field is conflict-checked
// in phase 1 (Test standard fields + step content). Unchecked types report
// checked=false so the caller auto-merges them.
func conflictTriple(c testrepo.PendingChange, t jira.Test, steps map[string]jira.Step, customFs map[string]string) (base, mine, remote, label string, checked bool) {
	base, mine = c.BeforeVal, c.AfterVal
	_ = customFs // custom-field conflict detection is deferred to phase 3 (the
	// remote custom-field fetch is stubbed; checking it would false-positive).
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
