package syncer

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"xray-test-manager/internal/jira"
	"xray-test-manager/internal/testrepo"
)

// CommitResult reports the outcome of pushing pending changes to Jira,
// per-Test. Succeeded, Conflicted and Failed are disjoint sets of Test keys.
// Created maps each newly-created Test's temporary "NEW-N" key to the real Jira
// key it was assigned, so the UI can re-point an open detail view (FR-1).
type CommitResult struct {
	Succeeded  []string       `json:"succeeded"`
	Conflicted []Conflict     `json:"conflicted"`
	Failed     []FailedCommit `json:"failed"`
	Created    []CreatedTest  `json:"created"`
}

// CreatedTest records that a locally-created Test (TempKey, "NEW-N") was created
// in Jira under Key during this commit (FR-1).
type CreatedTest struct {
	TempKey string `json:"tempKey"`
	Key     string `json:"key"`
}

// Conflict means the remote `updated` has moved since the user's earliest
// pending edit for this Test (FR-1.4). The PUT is held back so the user can
// resolve — sync to pull in the remote change and either re-commit or
// discard.
type Conflict struct {
	TestKey       string `json:"testKey"`
	BaseVersion   string `json:"baseVersion"`
	RemoteVersion string `json:"remoteVersion"`
}

// FailedCommit is one Test whose pending changes could not be committed
// for a non-conflict reason (network error, Jira validation, missing
// transition, etc.).
type FailedCommit struct {
	TestKey string `json:"testKey"`
	Error   string `json:"error"`
}

// CommitChanges pushes a profile's pending changes to Jira (FR-1.5). For
// each Test:
//
//  1. Fetch the current remote `updated` (FR-1.4 conflict pre-check). If
//     the remote has moved since the oldest pending edit's base_version,
//     skip the Test and surface a conflict.
//  2. PUT any non-status field updates.
//  3. POST a workflow transition (FR-4.2) if a status change is queued.
//  4. DELETE removed steps, then PUT step field updates, then POST new
//     steps (FR-2.5).
//  5. Delete the pending rows and audit "commit".
//
// Failures and conflicts leave pending rows in place so the user can
// resolve and retry.
func (e *Engine) CommitChanges(ctx context.Context, profileID, projectKey string) (CommitResult, error) {
	return e.commitChanges(ctx, profileID, projectKey, nil)
}

// CommitChangesForIDs commits only the pending changes whose ids are in the
// given set (selective commit), grouping and ordering them exactly as a full
// commit would. The caller is expected to pass a whole entity's changes
// together (e.g. all of one Test's rows) so a partial push can't leave sibling
// edits stranded against a now-advanced remote version.
func (e *Engine) CommitChangesForIDs(ctx context.Context, profileID, projectKey string, ids []int64) (CommitResult, error) {
	only := make(map[int64]bool, len(ids))
	for _, id := range ids {
		only[id] = true
	}
	return e.commitChanges(ctx, profileID, projectKey, only)
}

// commitChanges is the shared commit core. When only is non-nil, pending
// changes are filtered to that id set; nil means commit everything.
func (e *Engine) commitChanges(ctx context.Context, profileID, projectKey string, only map[int64]bool) (CommitResult, error) {
	result := CommitResult{
		Succeeded:  []string{},
		Conflicted: []Conflict{},
		Failed:     []FailedCommit{},
		Created:    []CreatedTest{},
	}

	changes, err := e.repo.ListPendingChanges(profileID)
	if err != nil {
		return result, err
	}
	changes = filterChangesByID(changes, only)

	// Create new Preconditions first (FR-13.5) so any pending association that
	// references a temporary precondition key is rewritten to the real key
	// before the per-Test pass reads it; re-read on success to pick up the
	// rewritten association rows.
	if e.commitPreconditionCreates(ctx, profileID, projectKey, changes, &result) {
		changes, err = e.repo.ListPendingChanges(profileID)
		if err != nil {
			return result, err
		}
		changes = filterChangesByID(changes, only)
	}

	// Create new Containers (Test Sets / Plans / Executions) next, for the same
	// reason: a freshly-created Execution gets its real key here, and
	// RenameContainer rewrites the still-pending membership and run-status rows
	// that referenced its temporary key — so adding tests to, or setting results
	// in, a brand-new Execution in the same commit targets the created issue
	// rather than the placeholder. Re-read so later passes see the real keys.
	if e.commitContainerCreates(ctx, profileID, changes, &result) {
		changes, err = e.repo.ListPendingChanges(profileID)
		if err != nil {
			return result, err
		}
		changes = filterChangesByID(changes, only)
	}

	// Create new Tests next (FR-1 interactive create, FR-10 import): a brand-new
	// Test gets its real key here, and RenameTest rewrites the still-pending
	// folder / precondition / step rows that referenced its "NEW-N" placeholder —
	// so attaching a folder or preconditions to a just-created Test in the same
	// commit targets the created issue. Re-read so later passes see the real keys.
	var newTestRows []testrepo.PendingChange
	for _, c := range changes {
		if c.EntityType == "test_create" {
			newTestRows = append(newTestRows, c)
		}
	}
	if len(newTestRows) > 0 {
		e.commitTestCreates(ctx, profileID, projectKey, newTestRows, &result)
		changes, err = e.repo.ListPendingChanges(profileID)
		if err != nil {
			return result, err
		}
		changes = filterChangesByID(changes, only)
	}

	// Group by parent Test key, preserving discovery order so the commit
	// run is deterministic. Step entity_keys are "<testKey>:<xrayID>" — we
	// strip the suffix to put step changes under the same Test bucket as
	// field changes on that Test.
	byTest := make(map[string][]testrepo.PendingChange)
	order := make([]string, 0)
	// Container allocations (FR-3.4–3.6) are per-Container, not per-Test, so
	// they're handled in their own pass after the per-Test commits.
	membershipRows := make([]testrepo.PendingChange, 0)
	// Precondition edits are keyed by the Precondition's own issue key, not a
	// Test, so they commit in their own pass grouped by precondition.
	preconditionEditRows := make([]testrepo.PendingChange, 0)
	// Precondition deletes (FR-13.4), keyed by the Precondition's issue key.
	preconditionDeleteRows := make([]testrepo.PendingChange, 0)
	// Folder operations (FR-13.3) are repository-level, keyed by folder path.
	folderRows := make([]testrepo.PendingChange, 0)
	// Test reviews commit as a Jira comment on the Test, keyed by Test key.
	reviewRows := make([]testrepo.PendingChange, 0)
	// Free-text comments (FR-4.4) also post as Jira comments.
	commentRows := make([]testrepo.PendingChange, 0)
	// Test-run result updates, keyed by "<execKey>:<testKey>".
	runRows := make([]testrepo.PendingChange, 0)
	// Requirement coverage-link changes, keyed by test key.
	requirementRows := make([]testrepo.PendingChange, 0)
	for _, c := range changes {
		if c.EntityType == "test_membership_add" ||
			c.EntityType == "test_membership_remove" ||
			c.EntityType == "test_container_add" ||
			c.EntityType == "container_edit" ||
			c.EntityType == "container_delete" {
			membershipRows = append(membershipRows, c)
			continue
		}
		if c.EntityType == "precondition_edit" {
			preconditionEditRows = append(preconditionEditRows, c)
			continue
		}
		if c.EntityType == "precondition_delete" {
			preconditionDeleteRows = append(preconditionDeleteRows, c)
			continue
		}
		if c.EntityType == "folder_create" || c.EntityType == "folder_rename" || c.EntityType == "folder_delete" {
			folderRows = append(folderRows, c)
			continue
		}
		if c.EntityType == "test_review" {
			reviewRows = append(reviewRows, c)
			continue
		}
		if c.EntityType == "issue_comment" {
			commentRows = append(commentRows, c)
			continue
		}
		if c.EntityType == "requirement_set" {
			requirementRows = append(requirementRows, c)
			continue
		}
		if c.EntityType == "test_run" {
			runRows = append(runRows, c)
			continue
		}
		testKey, ok := parentTestKey(c)
		if !ok {
			continue
		}
		if _, seen := byTest[testKey]; !seen {
			order = append(order, testKey)
		}
		byTest[testKey] = append(byTest[testKey], c)
	}

testLoop:
	for _, testKey := range order {
		testChanges := byTest[testKey]

		// Conflict pre-check.
		remoteUpdated, err := e.client.GetIssueUpdated(ctx, testKey)
		if err != nil {
			result.Failed = append(result.Failed, FailedCommit{
				TestKey: testKey,
				Error:   "conflict pre-check failed: " + sanitizeError(err.Error()),
			})
			continue
		}
		if remoteUpdated != "" {
			oldest := oldestBaseVersion(testChanges)
			if oldest != "" && isRemoteAhead(remoteUpdated, oldest) {
				result.Conflicted = append(result.Conflicted, Conflict{
					TestKey:       testKey,
					BaseVersion:   oldest,
					RemoteVersion: remoteUpdated,
				})
				continue
			}
		}

		// Split the bucket into:
		//   - one status transition (at most)
		//   - test_case field updates (summary, description, priority, labels)
		//   - per-step field updates, keyed by xrayID
		//   - step deletions, keyed by xrayID
		var statusChange *testrepo.PendingChange
		var orderChange *testrepo.PendingChange
		var folderChange *testrepo.PendingChange
		var preconditionChange *testrepo.PendingChange
		fieldChanges := make([]testrepo.PendingChange, 0, len(testChanges))
		customFields := make(map[string]string)
		stepChanges := make(map[string][]testrepo.PendingChange)
		stepDeletes := make([]string, 0)
		stepAdds := make([]testrepo.Step, 0)
		for i := range testChanges {
			c := testChanges[i]
			switch c.EntityType {
			case "test_case":
				switch c.Field {
				case "status":
					cc := c
					statusChange = &cc
				case "folder":
					cc := c
					folderChange = &cc
				default:
					fieldChanges = append(fieldChanges, c)
				}
			case "test_step":
				_, xrayID, ok := parseStepKey(c.EntityKey)
				if !ok {
					result.Failed = append(result.Failed, FailedCommit{
						TestKey: testKey,
						Error:   fmt.Sprintf("malformed step entity_key %q", c.EntityKey),
					})
					continue testLoop
				}
				stepChanges[xrayID] = append(stepChanges[xrayID], c)
			case "test_step_delete":
				_, xrayID, ok := parseStepKey(c.EntityKey)
				if !ok {
					result.Failed = append(result.Failed, FailedCommit{
						TestKey: testKey,
						Error:   fmt.Sprintf("malformed step entity_key %q", c.EntityKey),
					})
					continue testLoop
				}
				stepDeletes = append(stepDeletes, xrayID)
			case "test_step_add":
				_, tempID, ok := parseStepKey(c.EntityKey)
				if !ok {
					result.Failed = append(result.Failed, FailedCommit{
						TestKey: testKey,
						Error:   fmt.Sprintf("malformed step entity_key %q", c.EntityKey),
					})
					continue testLoop
				}
				var s testrepo.Step
				if err := json.Unmarshal([]byte(c.AfterVal), &s); err != nil {
					result.Failed = append(result.Failed, FailedCommit{
						TestKey: testKey,
						Error:   fmt.Sprintf("malformed step_add payload for %q: %s", c.EntityKey, err),
					})
					continue testLoop
				}
				s.XrayID = tempID
				stepAdds = append(stepAdds, s)
			case "test_step_order":
				cc := c
				orderChange = &cc
			case "precondition_set":
				cc := c
				preconditionChange = &cc
			case "custom_field":
				_, fieldID, ok := parseStepKey(c.EntityKey)
				if !ok {
					result.Failed = append(result.Failed, FailedCommit{
						TestKey: testKey,
						Error:   fmt.Sprintf("malformed custom field entity_key %q", c.EntityKey),
					})
					continue testLoop
				}
				customFields[fieldID] = c.AfterVal
			}
		}

		// PUT non-status field updates plus any custom fields in one call.
		// Standard fields go through FieldsForJira; custom fields are keyed by
		// their raw Jira id (FR-2.6).
		if len(fieldChanges) > 0 || len(customFields) > 0 {
			updates := make(map[string]string, len(fieldChanges))
			for _, c := range fieldChanges {
				updates[c.Field] = c.AfterVal
			}
			fields := jira.FieldsForJira(updates)
			for fieldID, value := range customFields {
				fields[fieldID] = value
			}
			if err := e.client.UpdateIssue(ctx, testKey, fields); err != nil {
				result.Failed = append(result.Failed, FailedCommit{
					TestKey: testKey,
					Error:   sanitizeError(err.Error()),
				})
				continue
			}
		}

		// POST workflow transition if a status change is pending.
		if statusChange != nil {
			if err := e.applyTransition(ctx, testKey, statusChange); err != nil {
				result.Failed = append(result.Failed, FailedCommit{
					TestKey: testKey,
					Error:   err.Error(),
				})
				continue
			}
		}

		// Test Repository move (FR-13.3). The pending change stores the target
		// folder *path*; the Xray move endpoint needs the native folder id, so
		// resolve it from the synced tree first.
		if folderChange != nil {
			xrayFolderID, ferr := e.repo.FolderXrayID(profileID, folderChange.AfterVal)
			if ferr != nil {
				result.Failed = append(result.Failed, FailedCommit{
					TestKey: testKey,
					Error:   "move to folder: " + sanitizeError(ferr.Error()),
				})
				continue
			}
			if err := e.client.MoveTestToFolder(ctx, projectKey, testKey, xrayFolderID); err != nil {
				result.Failed = append(result.Failed, FailedCommit{
					TestKey: testKey,
					Error:   "move to folder: " + sanitizeError(err.Error()),
				})
				continue
			}
		}

		// Associate / disassociate Preconditions (FR-13.5 / 13.6) by diffing
		// the before / after sets into add and remove lists.
		if preconditionChange != nil {
			add, remove, perr := diffPreconditionSets(preconditionChange.BeforeVal, preconditionChange.AfterVal)
			if perr != nil {
				result.Failed = append(result.Failed, FailedCommit{
					TestKey: testKey,
					Error:   "preconditions: " + perr.Error(),
				})
				continue
			}
			if err := e.client.UpdateTestPreconditions(ctx, testKey, add, remove); err != nil {
				result.Failed = append(result.Failed, FailedCommit{
					TestKey: testKey,
					Error:   "preconditions: " + sanitizeError(err.Error()),
				})
				continue
			}
		}

		for _, xrayID := range stepDeletes {
			if err := e.client.DeleteTestStep(ctx, testKey, xrayID); err != nil {
				result.Failed = append(result.Failed, FailedCommit{
					TestKey: testKey,
					Error:   fmt.Sprintf("delete step %s: %s", xrayID, sanitizeError(err.Error())),
				})
				continue testLoop
			}
		}

		// PUT each step that has pending edits, batching the step's changes
		// into one body. The first step to fail aborts further step PUTs
		// for this Test — the user resolves and retries.
		for xrayID, changes := range stepChanges {
			fields := make(map[string]string, len(changes))
			for _, c := range changes {
				fields[c.Field] = c.AfterVal
			}
			if err := e.client.UpdateTestStep(ctx, testKey, xrayID, fields); err != nil {
				result.Failed = append(result.Failed, FailedCommit{
					TestKey: testKey,
					Error:   fmt.Sprintf("update step %s: %s", xrayID, sanitizeError(err.Error())),
				})
				continue testLoop
			}
		}

		// POST new steps last, in index order — Xray appends each created
		// step to the end of the list, so creating them ascending preserves
		// the order the user arranged. On success we rename the local "new-N"
		// placeholder to the real id Xray returned.
		sort.SliceStable(stepAdds, func(i, j int) bool {
			return stepAdds[i].Index < stepAdds[j].Index
		})
		// idMap translates a newly-created step's temporary "new-N" id to the
		// real id Xray assigned, so a reorder queued against the temp id can
		// still target the right remote step.
		idMap := make(map[string]string, len(stepAdds))
		for _, s := range stepAdds {
			newID, err := e.client.CreateTestStep(ctx, testKey, s.Action, s.Data, s.Expected)
			if err != nil {
				result.Failed = append(result.Failed, FailedCommit{
					TestKey: testKey,
					Error:   fmt.Sprintf("add step: %s", sanitizeError(err.Error())),
				})
				continue testLoop
			}
			if newID != "" {
				idMap[s.XrayID] = newID
			}
			if err := e.repo.RenameTestStepID(profileID, testKey, s.XrayID, newID); err != nil {
				// The remote create already succeeded; a cache-rename hiccup
				// must not fail the commit. The stale placeholder reconciles
				// on the next steps refresh.
				continue
			}
		}

		// Apply the new step order last, once every step has its real id.
		// PUT each step to its target 1-based position in order; steps deleted
		// in this same commit are skipped, and temp ids are mapped to real
		// ones.
		if orderChange != nil {
			deleted := make(map[string]struct{}, len(stepDeletes))
			for _, id := range stepDeletes {
				deleted[id] = struct{}{}
			}
			var order []string
			if err := json.Unmarshal([]byte(orderChange.AfterVal), &order); err != nil {
				result.Failed = append(result.Failed, FailedCommit{
					TestKey: testKey,
					Error:   fmt.Sprintf("malformed step order payload: %s", err),
				})
				continue testLoop
			}
			pos := 0
			for _, id := range order {
				if _, gone := deleted[id]; gone {
					continue
				}
				if real, ok := idMap[id]; ok {
					id = real
				}
				pos++
				if err := e.client.MoveTestStep(ctx, testKey, id, pos); err != nil {
					result.Failed = append(result.Failed, FailedCommit{
						TestKey: testKey,
						Error:   fmt.Sprintf("reorder step %s: %s", id, sanitizeError(err.Error())),
					})
					continue testLoop
				}
			}
		}

		ids := make([]int64, len(testChanges))
		for i, c := range testChanges {
			ids[i] = c.ID
		}
		if err := e.repo.CommitPendingChanges(profileID, ids); err != nil {
			result.Failed = append(result.Failed, FailedCommit{
				TestKey: testKey,
				Error:   "Jira accepted update but local cleanup failed: " + err.Error(),
			})
			continue
		}
		result.Succeeded = append(result.Succeeded, testKey)

		// Selective commit: our PUT just advanced this Test's remote `updated`,
		// so any of its OTHER pending edits (not in this commit) would now look
		// stale and conflict on their next commit. Re-base them onto the fresh
		// remote version so a per-item commit doesn't poison the Test's
		// remaining items. (Full commits push everything at once, so there's
		// nothing left to re-base.)
		if only != nil {
			if upd, uerr := e.client.GetIssueUpdated(ctx, testKey); uerr == nil && upd != "" {
				_ = e.repo.RebaseTestConflict(profileID, testKey, upd)
			}
		}
	}

	e.commitMemberships(ctx, profileID, membershipRows, &result)
	e.commitPreconditionEdits(ctx, profileID, preconditionEditRows, &result)
	e.commitPreconditionDeletes(ctx, profileID, preconditionDeleteRows, &result)
	e.commitFolders(ctx, profileID, projectKey, folderRows, &result)
	e.commitReviews(ctx, profileID, reviewRows, &result)
	e.commitComments(ctx, profileID, commentRows, &result)
	e.commitRuns(ctx, profileID, runRows, &result)
	e.commitRequirements(ctx, profileID, requirementRows, &result)

	return result, nil
}

// commitRuns pushes Test-run result updates to Xray. Each pending row is keyed
// by "<execKey>:<testKey>"; reported under the Test key.
func (e *Engine) commitRuns(ctx context.Context, profileID string, rows []testrepo.PendingChange, result *CommitResult) {
	for _, c := range rows {
		execKey, testKey, ok := splitRunEntityKey(c.EntityKey)
		if !ok {
			result.Failed = append(result.Failed, FailedCommit{TestKey: c.EntityKey, Error: "malformed run-status key"})
			continue
		}
		if err := e.client.SetTestRunStatus(ctx, execKey, testKey, c.AfterVal); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: testKey, Error: "set run status: " + sanitizeError(err.Error())})
			continue
		}
		if err := e.repo.CommitPendingChanges(profileID, []int64{c.ID}); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: testKey, Error: "Xray accepted the result but local cleanup failed: " + err.Error()})
			continue
		}
		result.Succeeded = append(result.Succeeded, testKey)
	}
}

// commitRequirements pushes Test coverage-link changes: it diffs each
// requirement_set row's before (links, with ids) against its after (the new key
// set) and creates/removes the Jira issue links. Keyed by Test key.
func (e *Engine) commitRequirements(ctx context.Context, profileID string, rows []testrepo.PendingChange, result *CommitResult) {
	for _, c := range rows {
		testKey := c.EntityKey
		var before []struct {
			Key    string `json:"key"`
			LinkID string `json:"linkId"`
		}
		var after []string
		if err := json.Unmarshal([]byte(c.BeforeVal), &before); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: testKey, Error: "malformed requirement snapshot: " + err.Error()})
			continue
		}
		if err := json.Unmarshal([]byte(c.AfterVal), &after); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: testKey, Error: "malformed requirement set: " + err.Error()})
			continue
		}

		afterSet := make(map[string]bool, len(after))
		for _, k := range after {
			afterSet[k] = true
		}
		beforeSet := make(map[string]bool, len(before))
		removeLinkIDs := []string{}
		for _, l := range before {
			beforeSet[l.Key] = true
			if !afterSet[l.Key] {
				removeLinkIDs = append(removeLinkIDs, l.LinkID)
			}
		}
		add := []string{}
		for _, k := range after {
			if !beforeSet[k] {
				add = append(add, k)
			}
		}

		if err := e.client.UpdateTestRequirements(ctx, testKey, add, removeLinkIDs); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: testKey, Error: "update requirement links: " + sanitizeError(err.Error())})
			continue
		}
		if err := e.repo.CommitPendingChanges(profileID, []int64{c.ID}); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: testKey, Error: "Jira updated links but local cleanup failed: " + err.Error()})
			continue
		}
		result.Succeeded = append(result.Succeeded, testKey)
	}
}

// splitRunEntityKey parses "<execKey>:<testKey>" (issue keys have no colon).
func splitRunEntityKey(entityKey string) (execKey, testKey string, ok bool) {
	i := strings.Index(entityKey, ":")
	if i < 0 {
		return "", "", false
	}
	return entityKey[:i], entityKey[i+1:], true
}

// commitComments posts each queued free-text comment on its Test (FR-4.4).
func (e *Engine) commitComments(ctx context.Context, profileID string, rows []testrepo.PendingChange, result *CommitResult) {
	for _, c := range rows {
		if err := e.client.AddComment(ctx, c.EntityKey, c.AfterVal); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: c.EntityKey, Error: "post comment: " + sanitizeError(err.Error())})
			continue
		}
		if err := e.repo.CommitPendingChanges(profileID, []int64{c.ID}); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: c.EntityKey, Error: "Jira accepted the comment but local cleanup failed: " + err.Error()})
			continue
		}
		result.Succeeded = append(result.Succeeded, c.EntityKey)
	}
}

// commitReviews posts each Test review as a comment on its Test (test review).
func (e *Engine) commitReviews(ctx context.Context, profileID string, rows []testrepo.PendingChange, result *CommitResult) {
	for _, c := range rows {
		var rv struct {
			Verdict  string `json:"verdict"`
			Reviewer string `json:"reviewer"`
			Note     string `json:"note"`
		}
		if err := json.Unmarshal([]byte(c.AfterVal), &rv); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: c.EntityKey, Error: "malformed review payload: " + err.Error()})
			continue
		}
		body := reviewComment(rv.Verdict, rv.Reviewer, rv.Note)
		if err := e.client.AddComment(ctx, c.EntityKey, body); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: c.EntityKey, Error: "post review: " + sanitizeError(err.Error())})
			continue
		}
		if err := e.repo.CommitPendingChanges(profileID, []int64{c.ID}); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: c.EntityKey, Error: "Jira accepted the review but local cleanup failed: " + err.Error()})
			continue
		}
		result.Succeeded = append(result.Succeeded, c.EntityKey)
	}
}

// reviewComment renders the Jira comment body for a Test review.
func reviewComment(verdict, reviewer, note string) string {
	label := strings.ToUpper(verdict)
	if label == "" {
		label = "CLEARED"
	}
	body := "Test review: " + label
	if reviewer != "" {
		body += " by " + reviewer
	}
	if note != "" {
		body += " — " + note
	}
	return body
}

// commitTestCreates creates imported Tests (FR-10), renaming each local "NEW-N"
// placeholder to the real key Jira assigned. Reported under the created key.
func (e *Engine) commitTestCreates(ctx context.Context, profileID, projectKey string, rows []testrepo.PendingChange, result *CommitResult) {
	for _, c := range rows {
		var p struct {
			Summary     string `json:"summary"`
			Description string `json:"description"`
			Priority    string `json:"priority"`
			Labels      string `json:"labels"`
			Components  string `json:"components"`
			Steps       []struct {
				Action   string `json:"action"`
				Data     string `json:"data"`
				Expected string `json:"expected"`
			} `json:"steps"`
		}
		if err := json.Unmarshal([]byte(c.AfterVal), &p); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: c.EntityKey, Error: "malformed test payload: " + err.Error()})
			continue
		}
		var labels []string
		if p.Labels != "" {
			labels = strings.Fields(p.Labels)
		}
		var components []string
		for _, comp := range strings.Split(p.Components, ",") {
			if s := strings.TrimSpace(comp); s != "" {
				components = append(components, s)
			}
		}
		realKey, err := e.client.CreateTest(ctx, projectKey, p.Summary, p.Description, p.Priority, labels, components)
		if err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: c.EntityKey, Error: "create test: " + sanitizeError(err.Error())})
			continue
		}
		key := c.EntityKey
		if realKey != "" && realKey != c.EntityKey {
			if rErr := e.repo.RenameTest(profileID, c.EntityKey, realKey); rErr != nil {
				// Remote create succeeded; a cache-rename hiccup must not fail
				// the commit.
				_ = rErr
			}
			key = realKey
			// Record the temp→real mapping so the UI can re-point an open detail
			// view from the placeholder key to the created issue (FR-1).
			result.Created = append(result.Created, CreatedTest{TempKey: c.EntityKey, Key: realKey})
		}
		// Create the imported Test's steps (FR-10.7).
		stepErr := ""
		for _, s := range p.Steps {
			if _, sErr := e.client.CreateTestStep(ctx, key, s.Action, s.Data, s.Expected); sErr != nil {
				stepErr = sanitizeError(sErr.Error())
				break
			}
		}
		if stepErr != "" {
			// The Test was created but a step failed; leave the pending row so
			// the user can resolve and retry.
			result.Failed = append(result.Failed, FailedCommit{TestKey: key, Error: "create step: " + stepErr})
			continue
		}
		if err := e.repo.CommitPendingChanges(profileID, []int64{c.ID}); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: key, Error: "Jira created test but local cleanup failed: " + err.Error()})
			continue
		}
		result.Succeeded = append(result.Succeeded, key)
	}
}

// commitFolders pushes Test Repository folder operations (FR-13.3), reported
// under the folder path.
func (e *Engine) commitFolders(ctx context.Context, profileID, projectKey string, rows []testrepo.PendingChange, result *CommitResult) {
	for _, c := range rows {
		var err error
		switch c.EntityType {
		case "folder_create":
			var p struct {
				Name       string `json:"name"`
				ParentPath string `json:"parentPath"`
			}
			if jErr := json.Unmarshal([]byte(c.AfterVal), &p); jErr != nil {
				result.Failed = append(result.Failed, FailedCommit{TestKey: c.EntityKey, Error: "malformed folder payload: " + jErr.Error()})
				continue
			}
			err = e.client.CreateFolder(ctx, projectKey, p.ParentPath, p.Name)
		case "folder_rename":
			var p struct {
				Path string `json:"path"`
				Name string `json:"name"`
			}
			if jErr := json.Unmarshal([]byte(c.AfterVal), &p); jErr != nil {
				result.Failed = append(result.Failed, FailedCommit{TestKey: c.EntityKey, Error: "malformed folder payload: " + jErr.Error()})
				continue
			}
			err = e.client.RenameFolder(ctx, projectKey, c.EntityKey, p.Name)
		case "folder_delete":
			err = e.client.DeleteFolder(ctx, projectKey, c.EntityKey)
		}
		if err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: c.EntityKey, Error: "folder: " + sanitizeError(err.Error())})
			continue
		}
		if err := e.repo.CommitPendingChanges(profileID, []int64{c.ID}); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: c.EntityKey, Error: "Jira accepted folder change but local cleanup failed: " + err.Error()})
			continue
		}
		result.Succeeded = append(result.Succeeded, c.EntityKey)
	}
}

// commitPreconditionCreates creates new Preconditions (FR-13.5), renaming each
// local "new-precond-N" placeholder to the real key Jira assigned so pending
// associations against it are rewritten. Returns true if any were processed
// (so the caller re-reads pending changes).
func (e *Engine) commitPreconditionCreates(ctx context.Context, profileID, projectKey string, changes []testrepo.PendingChange, result *CommitResult) bool {
	processed := false
	for _, c := range changes {
		if c.EntityType != "precondition_add" {
			continue
		}
		var payload struct {
			Summary     string `json:"summary"`
			Type        string `json:"type"`
			Description string `json:"description"`
			ProjectKey  string `json:"projectKey"`
		}
		if err := json.Unmarshal([]byte(c.AfterVal), &payload); err != nil {
			result.Failed = append(result.Failed, FailedCommit{
				TestKey: c.EntityKey,
				Error:   fmt.Sprintf("malformed precondition payload: %s", err),
			})
			continue
		}
		pk := payload.ProjectKey
		if pk == "" {
			pk = projectKey
		}
		realKey, err := e.client.CreatePrecondition(ctx, pk, payload.Summary, payload.Type, payload.Description)
		if err != nil {
			result.Failed = append(result.Failed, FailedCommit{
				TestKey: c.EntityKey,
				Error:   "create precondition: " + sanitizeError(err.Error()),
			})
			continue
		}
		if realKey != "" {
			if err := e.repo.RenamePrecondition(profileID, c.EntityKey, realKey); err != nil {
				// Remote create already succeeded; a cache-rename hiccup must
				// not fail the commit.
				_ = err
			}
		}
		if err := e.repo.CommitPendingChanges(profileID, []int64{c.ID}); err != nil {
			result.Failed = append(result.Failed, FailedCommit{
				TestKey: c.EntityKey,
				Error:   "Jira created precondition but local cleanup failed: " + err.Error(),
			})
			continue
		}
		key := realKey
		if key == "" {
			key = c.EntityKey
		}
		result.Succeeded = append(result.Succeeded, key)
		processed = true
	}
	return processed
}

// commitPreconditionEdits pushes Precondition field edits (FR-13.5), grouping a
// Precondition's pending field changes into one issue update. Reported under
// the Precondition key.
func (e *Engine) commitPreconditionEdits(ctx context.Context, profileID string, rows []testrepo.PendingChange, result *CommitResult) {
	byPrecondition := make(map[string][]testrepo.PendingChange)
	order := make([]string, 0)
	for _, c := range rows {
		if _, seen := byPrecondition[c.EntityKey]; !seen {
			order = append(order, c.EntityKey)
		}
		byPrecondition[c.EntityKey] = append(byPrecondition[c.EntityKey], c)
	}

	for _, key := range order {
		group := byPrecondition[key]
		updates := make(map[string]string, len(group))
		ids := make([]int64, len(group))
		for i, c := range group {
			updates[c.Field] = c.AfterVal
			ids[i] = c.ID
		}
		if err := e.client.UpdateIssue(ctx, key, jira.FieldsForJira(updates)); err != nil {
			result.Failed = append(result.Failed, FailedCommit{
				TestKey: key,
				Error:   "update precondition: " + sanitizeError(err.Error()),
			})
			continue
		}
		if err := e.repo.CommitPendingChanges(profileID, ids); err != nil {
			result.Failed = append(result.Failed, FailedCommit{
				TestKey: key,
				Error:   "Jira accepted precondition update but local cleanup failed: " + err.Error(),
			})
			continue
		}
		result.Succeeded = append(result.Succeeded, key)
	}
}

// commitPreconditionDeletes deletes Preconditions in Jira (FR-13.4), reported
// under the Precondition key. The local row is dropped only after Jira confirms
// the delete so a failure leaves it pending for retry.
func (e *Engine) commitPreconditionDeletes(ctx context.Context, profileID string, rows []testrepo.PendingChange, result *CommitResult) {
	for _, c := range rows {
		if err := e.client.DeletePrecondition(ctx, c.EntityKey); err != nil {
			result.Failed = append(result.Failed, FailedCommit{
				TestKey: c.EntityKey,
				Error:   "delete precondition: " + sanitizeError(err.Error()),
			})
			continue
		}
		if err := e.repo.CommitPendingChanges(profileID, []int64{c.ID}); err != nil {
			result.Failed = append(result.Failed, FailedCommit{
				TestKey: c.EntityKey,
				Error:   "Jira deleted precondition but local cleanup failed: " + err.Error(),
			})
			continue
		}
		result.Succeeded = append(result.Succeeded, c.EntityKey)
	}
}

// commitContainerCreates creates new Containers (FR-3.4–3.6) before the
// membership / run-status passes, renaming each local "new-container-N"
// placeholder to the real key Jira assigned (RenameContainer rewrites the
// dependent pending rows). Returns true if any were processed so the caller
// re-reads pending changes and the later passes see the real keys.
func (e *Engine) commitContainerCreates(ctx context.Context, profileID string, changes []testrepo.PendingChange, result *CommitResult) bool {
	processed := false
	for _, c := range changes {
		if c.EntityType != "test_container_add" {
			continue
		}
		processed = true
		key, err := e.commitContainerCreate(ctx, profileID, c)
		if err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: key, Error: err.Error()})
			continue
		}
		if err := e.repo.CommitPendingChanges(profileID, []int64{c.ID}); err != nil {
			result.Failed = append(result.Failed, FailedCommit{
				TestKey: key,
				Error:   "Jira created container but local cleanup failed: " + err.Error(),
			})
			continue
		}
		result.Succeeded = append(result.Succeeded, key)
	}
	return processed
}

// commitMemberships pushes container-allocation pending rows (FR-3.4–3.6).
// A test_membership_add row adds Tests to an existing Container; a
// test_container_add row first creates the Container, then populates it. Each
// is reported under the Container key. These are additive, so no conflict
// pre-check is applied.
func (e *Engine) commitMemberships(ctx context.Context, profileID string, rows []testrepo.PendingChange, result *CommitResult) {
	for _, c := range rows {
		var key string
		var err error
		switch c.EntityType {
		case "test_container_add":
			key, err = e.commitContainerCreate(ctx, profileID, c)
		case "test_membership_remove":
			key, err = e.commitMembershipRemove(ctx, c)
		case "container_edit":
			key, err = c.EntityKey, e.client.UpdateIssue(ctx, c.EntityKey, jira.FieldsForJira(map[string]string{"summary": c.AfterVal}))
		case "container_delete":
			key, err = e.commitContainerDelete(ctx, c)
		default:
			key, err = e.commitMembershipAdd(ctx, c)
		}
		if err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: key, Error: err.Error()})
			continue
		}
		if err := e.repo.CommitPendingChanges(profileID, []int64{c.ID}); err != nil {
			result.Failed = append(result.Failed, FailedCommit{
				TestKey: key,
				Error:   "Jira accepted allocation but local cleanup failed: " + err.Error(),
			})
			continue
		}
		result.Succeeded = append(result.Succeeded, key)
	}
}

// commitMembershipAdd adds Tests to an existing Container, returning the
// Container key for result reporting.
func (e *Engine) commitMembershipAdd(ctx context.Context, c testrepo.PendingChange) (string, error) {
	var payload struct {
		Kind    string   `json:"kind"`
		Members []string `json:"members"`
	}
	if err := json.Unmarshal([]byte(c.AfterVal), &payload); err != nil {
		return c.EntityKey, fmt.Errorf("malformed membership payload: %s", err)
	}
	if err := e.client.AddTestsToContainer(ctx, payload.Kind, c.EntityKey, payload.Members); err != nil {
		return c.EntityKey, fmt.Errorf("allocate to %s: %s", c.EntityKey, sanitizeError(err.Error()))
	}
	return c.EntityKey, nil
}

// commitMembershipRemove removes Tests from an existing Container, returning the
// Container key for result reporting.
func (e *Engine) commitMembershipRemove(ctx context.Context, c testrepo.PendingChange) (string, error) {
	var payload struct {
		Kind    string   `json:"kind"`
		Members []string `json:"members"`
	}
	if err := json.Unmarshal([]byte(c.AfterVal), &payload); err != nil {
		return c.EntityKey, fmt.Errorf("malformed membership payload: %s", err)
	}
	if err := e.client.RemoveTestsFromContainer(ctx, payload.Kind, c.EntityKey, payload.Members); err != nil {
		return c.EntityKey, fmt.Errorf("deallocate from %s: %s", c.EntityKey, sanitizeError(err.Error()))
	}
	return c.EntityKey, nil
}

// commitContainerDelete deletes a Container, reading its kind from the delete
// snapshot. Returns the Container key for result reporting.
func (e *Engine) commitContainerDelete(ctx context.Context, c testrepo.PendingChange) (string, error) {
	var snap struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal([]byte(c.BeforeVal), &snap); err != nil {
		return c.EntityKey, fmt.Errorf("malformed container snapshot: %s", err)
	}
	if err := e.client.DeleteContainer(ctx, snap.Kind, c.EntityKey); err != nil {
		return c.EntityKey, fmt.Errorf("delete container %s: %s", c.EntityKey, sanitizeError(err.Error()))
	}
	return c.EntityKey, nil
}

// commitContainerCreate creates a new Container, renames the local placeholder
// to the real key, then allocates the Tests. It returns the real key (or the
// placeholder if Jira returned none) for result reporting.
func (e *Engine) commitContainerCreate(ctx context.Context, profileID string, c testrepo.PendingChange) (string, error) {
	var payload struct {
		Kind       string   `json:"kind"`
		Summary    string   `json:"summary"`
		ProjectKey string   `json:"projectKey"`
		Members    []string `json:"members"`
	}
	if err := json.Unmarshal([]byte(c.AfterVal), &payload); err != nil {
		return c.EntityKey, fmt.Errorf("malformed container payload: %s", err)
	}
	realKey, err := e.client.CreateContainer(ctx, payload.ProjectKey, payload.Kind, payload.Summary)
	if err != nil {
		return c.EntityKey, fmt.Errorf("create container: %s", sanitizeError(err.Error()))
	}
	target := c.EntityKey
	if realKey != "" {
		if err := e.repo.RenameContainer(profileID, c.EntityKey, realKey); err != nil {
			// The remote create already succeeded; a cache-rename hiccup must
			// not fail the commit. The placeholder reconciles on next sync.
			_ = err
		}
		target = realKey
	}
	if err := e.client.AddTestsToContainer(ctx, payload.Kind, target, payload.Members); err != nil {
		return target, fmt.Errorf("allocate to %s: %s", target, sanitizeError(err.Error()))
	}
	return target, nil
}

// diffPreconditionSets decodes the before / after Precondition key-lists from a
// precondition_set pending row and returns the keys to add (in after, not
// before) and remove (in before, not after).
func diffPreconditionSets(beforeJSON, afterJSON string) (add, remove []string, err error) {
	var before, after []string
	if err := json.Unmarshal([]byte(beforeJSON), &before); err != nil {
		return nil, nil, fmt.Errorf("decode before set: %w", err)
	}
	if err := json.Unmarshal([]byte(afterJSON), &after); err != nil {
		return nil, nil, fmt.Errorf("decode after set: %w", err)
	}
	beforeSet := make(map[string]struct{}, len(before))
	for _, k := range before {
		beforeSet[k] = struct{}{}
	}
	afterSet := make(map[string]struct{}, len(after))
	for _, k := range after {
		afterSet[k] = struct{}{}
	}
	for _, k := range after {
		if _, ok := beforeSet[k]; !ok {
			add = append(add, k)
		}
	}
	for _, k := range before {
		if _, ok := afterSet[k]; !ok {
			remove = append(remove, k)
		}
	}
	return add, remove, nil
}

// filterChangesByID keeps only the changes whose id is in the set. A nil set
// means "keep everything" (a full commit).
func filterChangesByID(changes []testrepo.PendingChange, only map[int64]bool) []testrepo.PendingChange {
	if only == nil {
		return changes
	}
	out := make([]testrepo.PendingChange, 0, len(only))
	for _, c := range changes {
		if only[c.ID] {
			out = append(out, c)
		}
	}
	return out
}

// parentTestKey extracts the parent Test key for a pending change so
// CommitChanges can group test_case and test_step changes together per
// Test. Returns false for unrecognised entity types.
func parentTestKey(c testrepo.PendingChange) (string, bool) {
	switch c.EntityType {
	case "test_case", "test_step_order", "precondition_set":
		// These are test-level changes — entity_key is the Test key itself,
		// no "<key>:<step>" suffix.
		return c.EntityKey, true
	case "test_step", "test_step_delete", "test_step_add", "custom_field":
		k, _, ok := parseStepKey(c.EntityKey)
		return k, ok
	}
	return "", false
}

// parseStepKey splits a "<testKey>:<xrayID>" pending entity_key. Mirrors
// testrepo.parseStepEntityKey but lives here too so the syncer doesn't
// depend on an exported helper for a one-line split.
func parseStepKey(s string) (testKey, xrayID string, ok bool) {
	i := strings.Index(s, ":")
	if i < 0 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

// applyTransition resolves the transition ID by target status name and POSTs
// it. The current Jira status is the pending change's BeforeVal — that's
// what Jira holds until our commit lands.
func (e *Engine) applyTransition(ctx context.Context, testKey string, change *testrepo.PendingChange) error {
	transitions, err := e.client.GetTransitions(ctx, testKey, change.BeforeVal)
	if err != nil {
		return fmt.Errorf("fetch transitions: %s", sanitizeError(err.Error()))
	}
	var transitionID string
	for _, t := range transitions {
		if t.To == change.AfterVal {
			transitionID = t.ID
			break
		}
	}
	if transitionID == "" {
		return fmt.Errorf(
			"no transition available to status %q from %q",
			change.AfterVal, change.BeforeVal,
		)
	}
	if err := e.client.PostTransition(ctx, testKey, transitionID); err != nil {
		return fmt.Errorf("post transition: %s", sanitizeError(err.Error()))
	}
	return nil
}

// oldestBaseVersion returns the earliest base_version among a Test's pending
// changes, ignoring empty values. The oldest is used for the conflict
// pre-check so any field that was edited before a remote update triggers a
// conflict on the whole Test.
func oldestBaseVersion(changes []testrepo.PendingChange) string {
	oldest := ""
	for _, c := range changes {
		if c.BaseVersion == "" {
			continue
		}
		if oldest == "" || c.BaseVersion < oldest {
			oldest = c.BaseVersion
		}
	}
	return oldest
}

// isRemoteAhead returns true if remote's timestamp is strictly later than
// base's. Both arguments are timestamps as Jira returns them — typically
// "yyyy-MM-ddTHH:mm:ss.SSS-HHMM" but RFC 3339 variants are also accepted.
// On parse failure the function is permissive (returns false) so a malformed
// remote string can't manufacture a phantom conflict.
func isRemoteAhead(remote, base string) bool {
	rt, ok1 := parseJiraTime(remote)
	bt, ok2 := parseJiraTime(base)
	if !ok1 || !ok2 {
		return false
	}
	return rt.After(bt)
}

var jiraTimeFormats = []string{
	"2006-01-02T15:04:05.000-0700",
	"2006-01-02T15:04:05-0700",
	"2006-01-02T15:04:05.000Z07:00",
	time.RFC3339Nano,
	time.RFC3339,
}

func parseJiraTime(s string) (time.Time, bool) {
	for _, f := range jiraTimeFormats {
		if t, err := time.Parse(f, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// sanitizeError trims long Jira error responses so the UI shows a short,
// single-line message in the per-Test failure list.
func sanitizeError(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}
