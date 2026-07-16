package syncer

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"

	"xray-test-manager/internal/backend"
	"xray-test-manager/internal/testrepo"
)

// CommitResult reports the outcome of pushing pending changes to Jira,
// per-Test. Succeeded, Conflicted and Failed are disjoint sets of Test keys.
// Created maps each newly-created Test's temporary "NEW-N" key to the real Jira
// key it was assigned, so the UI can re-point an open detail view (FR-1).
type CommitResult struct {
	Succeeded  []string        `json:"succeeded"`
	Conflicted []Conflict      `json:"conflicted"`
	Failed     []FailedCommit  `json:"failed"`
	Created    []CreatedTest   `json:"created"`
	Skipped    []SkippedCommit `json:"skipped"`
}

// SkippedCommit is one pending row the current backend cannot commit because
// the operation is unsupported by its capabilities (e.g. Test Repository
// folders or precondition objects on a Kiwi backend). The row is NOT pushed and
// NOT cleared — it stays pending, like a conflict, so the user can discard it;
// nothing is silently dropped. Xray reports all capabilities on, so it never
// populates this list (additive/back-compatible JSON field).
type SkippedCommit struct {
	EntityKey  string `json:"entityKey"`
	EntityType string `json:"entityType"`
	Reason     string `json:"reason"`
}

// Skip reasons for capability-gated buckets. These are surfaced verbatim in
// SkippedCommit.Reason.
const (
	skipReasonFolders       = "backend does not support Test Repository folders"
	skipReasonPreconditions = "backend does not support precondition objects"
	skipReasonRequirements  = "backend does not support requirement writes"
	skipReasonReviews       = "backend does not support test reviews"
	skipReasonContainerEdit = "backend does not support container rename"
	skipReasonBugCreate     = "backend does not support bug creation"
	skipReasonComments      = "backend does not support issue comments"
	skipReasonExecType      = "backend does not support the Test Type (exec_type) field"
)

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
	TestSummary   string `json:"testSummary"`
	BaseVersion   string `json:"baseVersion"`
	RemoteVersion string `json:"remoteVersion"`
	RemoteDeleted bool   `json:"remoteDeleted"`
	// Fields lists the genuinely overlapping edits (three-way) to resolve. Empty
	// when RemoteDeleted is set.
	Fields []ConflictField `json:"fields"`
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
		Skipped:    []SkippedCommit{},
	}

	// caps drives every capability gate below. For Xray all capabilities are on,
	// so each `if caps.X { ... } else { skip }` gate takes the original branch and
	// the commit path is unchanged. For a Kiwi backend the unsupported buckets are
	// skipped (reported, kept pending) and status/steps route to field updates.
	caps := e.backend.Capabilities()

	changes, err := e.repo.ListPendingChanges(profileID)
	if err != nil {
		return result, err
	}
	changes = filterChangesByID(changes, only)

	// Create new Preconditions first (FR-13.5) so any pending association that
	// references a temporary precondition key is rewritten to the real key
	// before the per-Test pass reads it; re-read on success to pick up the
	// rewritten association rows.
	if caps.SupportsPreconditionObjects {
		if e.commitPreconditionCreates(ctx, profileID, projectKey, changes, &result) {
			changes, err = e.repo.ListPendingChanges(profileID)
			if err != nil {
				return result, err
			}
			changes = filterChangesByID(changes, only)
		}
	} else {
		e.skipRowsOfType(changes, "precondition_add", skipReasonPreconditions, &result)
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
	// Requirement field edits, keyed by the requirement's own issue key.
	requirementEditRows := make([]testrepo.PendingChange, 0)
	// Requirement deletes, keyed by the requirement's own issue key.
	requirementDeleteRows := make([]testrepo.PendingChange, 0)
	// Bug creates, keyed by the temporary bug key.
	bugCreateRows := make([]testrepo.PendingChange, 0)
	// Requirement creates, keyed by the temporary requirement key.
	requirementCreateRows := make([]testrepo.PendingChange, 0)
	// Requirement->Requirement link changes (entity_key = fromKey, field = linkType).
	reqReqLinkRows := make([]testrepo.PendingChange, 0)
	for _, c := range changes {
		if c.EntityType == "test_membership_add" ||
			c.EntityType == "test_membership_remove" ||
			c.EntityType == "test_container_add" ||
			c.EntityType == "container_edit" ||
			c.EntityType == "container_delete" ||
			c.EntityType == "container_env" {
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
		if c.EntityType == "requirement_edit" {
			requirementEditRows = append(requirementEditRows, c)
			continue
		}
		if c.EntityType == "requirement_delete" {
			requirementDeleteRows = append(requirementDeleteRows, c)
			continue
		}
		if c.EntityType == "bug_create" {
			bugCreateRows = append(bugCreateRows, c)
			continue
		}
		if c.EntityType == "requirement_create" {
			requirementCreateRows = append(requirementCreateRows, c)
			continue
		}
		if c.EntityType == "req_req_link_set" {
			reqReqLinkRows = append(reqReqLinkRows, c)
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

		// skippedIDs holds this Test's pending rows that were capability-skipped
		// (folder move / precondition association on a backend that lacks them):
		// they are recorded in result.Skipped and excluded from the final
		// CommitPendingChanges so they stay pending. Empty for Xray (all caps on).
		skippedIDs := make(map[int64]bool)

		// Conflict pre-check (FR-1.4) with per-field auto-merge: when the remote
		// `updated` has advanced past our base, fetch the current remote values
		// and classify each change. Non-overlapping edits (and edits someone else
		// already made) merge silently; only genuinely overlapping fields hold the
		// Test back for the user to resolve.
		remoteVer, err := e.backend.RemoteVersion(ctx, "test", testKey)
		if err != nil {
			result.Failed = append(result.Failed, FailedCommit{
				TestKey: testKey,
				Error:   "conflict pre-check failed: " + sanitizeError(err.Error()),
			})
			continue
		}
		if remoteVer != "" {
			oldest := oldestBaseVersion(testChanges)
			if oldest != "" && e.backend.RemoteAhead(backend.VersionToken(oldest), remoteVer) {
				scan, derr := e.detectConflicts(ctx, testKey, testChanges)
				if derr != nil {
					result.Failed = append(result.Failed, FailedCommit{
						TestKey: testKey,
						Error:   "conflict check failed: " + sanitizeError(derr.Error()),
					})
					continue
				}
				if scan.deleted {
					result.Conflicted = append(result.Conflicted, Conflict{
						TestKey:       testKey,
						BaseVersion:   oldest,
						RemoteVersion: string(remoteVer),
						RemoteDeleted: true,
					})
					continue
				}
				// Drop edits that someone else already made identically.
				if len(scan.dropIDs) > 0 {
					_ = e.repo.CommitPendingChanges(profileID, scan.dropIDs)
					testChanges = filterOutIDs(testChanges, scan.dropIDs)
				}
				if len(scan.conflicts) > 0 {
					// Hold the whole Test back; the user resolves per field, then
					// re-commits. The remaining (clean) edits go with it.
					result.Conflicted = append(result.Conflicted, Conflict{
						TestKey:       testKey,
						TestSummary:   scan.testSummary,
						BaseVersion:   oldest,
						RemoteVersion: string(remoteVer),
						Fields:        scan.conflicts,
					})
					continue
				}
				// No overlap: re-base the remaining clean edits onto the new
				// remote and commit them normally.
				if len(testChanges) == 0 {
					continue
				}
				if rErr := e.repo.RebaseTestConflict(profileID, testKey, string(remoteVer)); rErr != nil {
					result.Failed = append(result.Failed, FailedCommit{
						TestKey: testKey,
						Error:   "rebase after auto-merge failed: " + rErr.Error(),
					})
					continue
				}
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
			var execTypeRow *testrepo.PendingChange
			for i := range fieldChanges {
				c := fieldChanges[i]
				updates[c.Field] = c.AfterVal
				if c.Field == "exec_type" {
					execTypeRow = &fieldChanges[i]
				}
			}
			fields := e.backend.FieldsForJira(updates)
			// exec_type (Xray Test Type) is a custom field whose id varies per
			// instance, so FieldsForJira leaves it out: resolve the field id here
			// and inject {"value": ...}. Best-effort - if the field can't be
			// resolved, log and push the rest of the update rather than fail
			// the whole commit.
			if execType, ok := updates["exec_type"]; ok {
				fieldID, value, resolved, ferr := e.backend.ExecTypeFieldValue(ctx, execType)
				if ferr != nil {
					log.Printf("xtm: resolve Test Type field for %s failed, committing without exec_type: %v", testKey, ferr)
				} else if resolved {
					fields[fieldID] = value
				} else {
					// The backend has no exec-type field to resolve into (no Test
					// Type custom field on this instance, or a backend that has no
					// exec-type concept at all, like Kiwi). Applying the rest of the
					// update but silently clearing this row would drop the edit on
					// the floor, so record it as skipped and keep it pending instead
					// — like folder/precondition rows on a capability-less backend.
					log.Printf("xtm: no Test Type custom field on this instance, committing %s without exec_type", testKey)
					if execTypeRow != nil {
						result.Skipped = append(result.Skipped, SkippedCommit{
							EntityKey:  testKey,
							EntityType: execTypeRow.EntityType,
							Reason:     skipReasonExecType,
						})
						skippedIDs[execTypeRow.ID] = true
					}
				}
			}
			// Generic custom field edits (FR-2.6) share this PUT. The journaled
			// edit carries only the field id and a string value (no type hint), so
			// resolve the field's schema type here and shape the value the same way
			// exec_type does above. Best-effort - if the type cannot be resolved
			// the raw string is sent (CustomFieldValue defaults to it).
			for fieldID, value := range customFields {
				id, shaped, ferr := e.backend.CustomFieldValue(ctx, fieldID, value)
				if ferr != nil {
					log.Printf("xtm: resolve custom field %s type for %s failed, committing raw string: %v", fieldID, testKey, ferr)
					fields[fieldID] = value
					continue
				}
				fields[id] = shaped
			}
			if err := e.backend.UpdateIssue(ctx, testKey, fields); err != nil {
				result.Failed = append(result.Failed, FailedCommit{
					TestKey: testKey,
					Error:   sanitizeError(err.Error()),
				})
				continue
			}
		}

		// Apply a pending status change. Xray (SupportsWorkflowTransitions) POSTs a
		// workflow transition, exactly as before. A settable-status backend (Kiwi)
		// has no workflow graph, so the status is written as a plain field update —
		// FieldsForJira maps "status" to the backend's status field (case_status)
		// and UpdateIssue applies it. Both paths use the same statusChange row, so
		// base-version/conflict handling above is preserved identically.
		if statusChange != nil {
			if caps.SupportsWorkflowTransitions {
				if err := e.applyTransition(ctx, testKey, statusChange); err != nil {
					result.Failed = append(result.Failed, FailedCommit{
						TestKey: testKey,
						Error:   err.Error(),
					})
					continue
				}
			} else {
				fields := e.backend.FieldsForJira(map[string]string{"status": statusChange.AfterVal})
				if err := e.backend.UpdateIssue(ctx, testKey, fields); err != nil {
					result.Failed = append(result.Failed, FailedCommit{
						TestKey: testKey,
						Error:   "set status: " + sanitizeError(err.Error()),
					})
					continue
				}
			}
		}

		// Test Repository move (FR-13.3). The pending change stores the target
		// folder *path*; the Xray move endpoint needs the native folder id, so
		// resolve it from the synced tree first. Backends without folders (Kiwi)
		// skip the move and keep the row pending.
		if folderChange != nil && !caps.SupportsFolders {
			result.Skipped = append(result.Skipped, SkippedCommit{
				EntityKey:  testKey,
				EntityType: folderChange.EntityType,
				Reason:     skipReasonFolders,
			})
			skippedIDs[folderChange.ID] = true
		} else if folderChange != nil {
			xrayFolderID, ferr := e.repo.FolderXrayID(profileID, folderChange.AfterVal)
			if ferr != nil {
				result.Failed = append(result.Failed, FailedCommit{
					TestKey: testKey,
					Error:   "move to folder: " + sanitizeError(ferr.Error()),
				})
				continue
			}
			if err := e.backend.MoveTestToFolder(ctx, projectKey, testKey, xrayFolderID); err != nil {
				result.Failed = append(result.Failed, FailedCommit{
					TestKey: testKey,
					Error:   "move to folder: " + sanitizeError(err.Error()),
				})
				continue
			}
		}

		// Associate / disassociate Preconditions (FR-13.5 / 13.6) by diffing
		// the before / after sets into add and remove lists. Backends without
		// precondition objects (Kiwi) skip this and keep the row pending.
		if preconditionChange != nil && !caps.SupportsPreconditionObjects {
			result.Skipped = append(result.Skipped, SkippedCommit{
				EntityKey:  testKey,
				EntityType: preconditionChange.EntityType,
				Reason:     skipReasonPreconditions,
			})
			skippedIDs[preconditionChange.ID] = true
		} else if preconditionChange != nil {
			add, remove, perr := diffPreconditionSets(preconditionChange.BeforeVal, preconditionChange.AfterVal)
			if perr != nil {
				result.Failed = append(result.Failed, FailedCommit{
					TestKey: testKey,
					Error:   "preconditions: " + perr.Error(),
				})
				continue
			}
			if err := e.backend.UpdateTestPreconditions(ctx, testKey, add, remove); err != nil {
				result.Failed = append(result.Failed, FailedCommit{
					TestKey: testKey,
					Error:   "preconditions: " + sanitizeError(err.Error()),
				})
				continue
			}
		}

		// Steps. Xray applies per-step CRUD (delete/update/add/reorder). A backend
		// whose steps are a single inline-text field (Kiwi, StepModel "inline-text")
		// has no step objects, so ALL queued step changes for this Test collapse
		// into ONE text field update: the resulting neutral step list — already
		// materialised in the local cache by the step edits — is flattened back to
		// text, the inverse of the read-path flattenSteps. Per-step CRUD is
		// ErrUnsupported there and is never called. Xray keeps the branch below
		// verbatim.
		if caps.StepModel == "inline-text" {
			if len(stepDeletes) > 0 || len(stepChanges) > 0 || len(stepAdds) > 0 || orderChange != nil {
				text, terr := e.flattenStepsToText(profileID, testKey)
				if terr != nil {
					result.Failed = append(result.Failed, FailedCommit{
						TestKey: testKey,
						Error:   "flatten steps: " + sanitizeError(terr.Error()),
					})
					continue
				}
				fields := e.backend.FieldsForJira(map[string]string{"description": text})
				if err := e.backend.UpdateIssue(ctx, testKey, fields); err != nil {
					result.Failed = append(result.Failed, FailedCommit{
						TestKey: testKey,
						Error:   "update steps text: " + sanitizeError(err.Error()),
					})
					continue
				}
			}
		} else {
			for _, xrayID := range stepDeletes {
				if err := e.backend.DeleteTestStep(ctx, testKey, xrayID); err != nil {
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
				if err := e.backend.UpdateTestStep(ctx, testKey, xrayID, fields); err != nil {
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
				newID, err := e.createStep(ctx, profileID, testKey, s)
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
				// Xray's step PUT 400s unless the body carries the step's content
				// fields, so look up each step's cached action/data/expected to send
				// with the reorder. By now the cache holds the real ids (new steps
				// were renamed above), keyed the same way the order list is after
				// idMap mapping.
				stepContent := map[string]testrepo.Step{}
				if cs, err := e.repo.ListTestSteps(profileID, testKey); err == nil {
					for _, s := range cs {
						stepContent[s.XrayID] = s
					}
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
					sc := stepContent[id]
					if err := e.backend.MoveTestStep(ctx, testKey, id, pos, sc.Action, sc.Data, sc.Expected); err != nil {
						result.Failed = append(result.Failed, FailedCommit{
							TestKey: testKey,
							Error:   fmt.Sprintf("reorder step %s: %s", id, sanitizeError(err.Error())),
						})
						continue testLoop
					}
				}
			}

		}

		// Clear every committed pending row for this Test. Capability-skipped rows
		// (folder / precondition on a backend that lacks them) are excluded so they
		// stay pending — like a conflict, nothing is silently dropped. For Xray
		// skippedIDs is empty, so this is the original "clear all rows" behavior and
		// Succeeded/rebase always run.
		ids := make([]int64, 0, len(testChanges))
		for _, c := range testChanges {
			if skippedIDs[c.ID] {
				continue
			}
			ids = append(ids, c.ID)
		}
		if len(ids) > 0 {
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
				if upd, uerr := e.backend.RemoteVersion(ctx, "test", testKey); uerr == nil && upd != "" {
					_ = e.repo.RebaseTestConflict(profileID, testKey, string(upd))
				}
			}
		}
	}

	// Capability-gated bucket dispatch. Each `if caps.X { run } else { skip }` gate
	// is a no-op for Xray (all capabilities on) — it takes the run branch exactly
	// as before. A Kiwi backend skips the buckets its model can't express; those
	// rows are reported in result.Skipped and stay pending. Containers and runs
	// are supported on both backends and run unconditionally (container rename is
	// gated inside commitMemberships). Comments post as issue comments, so they
	// need the same issue-backed write surface as reviews.
	e.commitMemberships(ctx, profileID, membershipRows, caps, &result)
	if caps.SupportsPreconditionObjects {
		e.commitPreconditionEdits(ctx, profileID, preconditionEditRows, &result)
		e.commitPreconditionDeletes(ctx, profileID, preconditionDeleteRows, &result)
	} else {
		e.skipRows(preconditionEditRows, skipReasonPreconditions, &result)
		e.skipRows(preconditionDeleteRows, skipReasonPreconditions, &result)
	}
	if caps.SupportsFolders {
		e.commitFolders(ctx, profileID, projectKey, folderRows, &result)
	} else {
		e.skipRows(folderRows, skipReasonFolders, &result)
	}
	if issueBackedWrites(caps) {
		e.commitReviews(ctx, profileID, reviewRows, &result)
	} else {
		e.skipRows(reviewRows, skipReasonReviews, &result)
	}
	if issueBackedWrites(caps) {
		e.commitComments(ctx, profileID, commentRows, &result)
	} else {
		e.skipRows(commentRows, skipReasonComments, &result)
	}
	e.commitRuns(ctx, profileID, runRows, &result)
	if requirementWritesSupported(caps) {
		e.commitRequirements(ctx, profileID, requirementRows, &result)
		e.commitRequirementEdits(ctx, profileID, requirementEditRows, &result)
		e.commitRequirementDeletes(ctx, profileID, requirementDeleteRows, &result)
		e.commitRequirementCreates(ctx, profileID, requirementCreateRows, &result)
		e.commitReqReqLinks(ctx, profileID, reqReqLinkRows, &result)
	} else {
		e.skipRows(requirementRows, skipReasonRequirements, &result)
		e.skipRows(requirementEditRows, skipReasonRequirements, &result)
		e.skipRows(requirementDeleteRows, skipReasonRequirements, &result)
		e.skipRows(requirementCreateRows, skipReasonRequirements, &result)
		e.skipRows(reqReqLinkRows, skipReasonRequirements, &result)
	}
	if caps.SupportsBugCreation {
		e.commitBugCreates(ctx, profileID, bugCreateRows, &result)
	} else {
		e.skipRows(bugCreateRows, skipReasonBugCreate, &result)
	}

	return result, nil
}

// issueBackedWrites reports whether the backend stores entities as issues that
// accept generic issue writes routed through UpdateIssue / issue comments —
// workflow-transitioned Jira issues (Xray). It gates buckets that have no
// dedicated capability flag but only work against an issue-shaped backend:
// test reviews (posted as issue comments) and container rename (UpdateIssue on
// a container key). A settable-status backend (Kiwi) returns false, so those
// buckets are skipped. Xray returns true and every such bucket runs unchanged.
func issueBackedWrites(caps backend.Capabilities) bool {
	return caps.SupportsWorkflowTransitions
}

// requirementWritesSupported reports whether requirement create/edit/delete/link
// writes can be committed. This needs BOTH a requirement object model AND an
// issue-backed write surface (requirements are created/deleted as Jira issues
// and linked via issue links). Kiwi's requirements plugin is read-only — it can
// report SupportsRequirementObjects for the read/coverage path but has no create
// RPC — so requirement WRITES are treated as unsupported there. Xray has both and
// runs the buckets unchanged.
func requirementWritesSupported(caps backend.Capabilities) bool {
	return caps.SupportsRequirementObjects && issueBackedWrites(caps)
}

// skipRows records each pending row as capability-skipped (reported, kept
// pending). Used when a bucket's capability is off so the rows are neither
// pushed nor cleared.
func (e *Engine) skipRows(rows []testrepo.PendingChange, reason string, result *CommitResult) {
	for _, c := range rows {
		result.Skipped = append(result.Skipped, SkippedCommit{
			EntityKey:  c.EntityKey,
			EntityType: c.EntityType,
			Reason:     reason,
		})
	}
}

// skipRowsOfType records only the rows of the given entity type as skipped.
// Used for buckets whose rows are still interleaved in the full change list
// (e.g. precondition_add before the create pass).
func (e *Engine) skipRowsOfType(changes []testrepo.PendingChange, entityType, reason string, result *CommitResult) {
	for _, c := range changes {
		if c.EntityType == entityType {
			result.Skipped = append(result.Skipped, SkippedCommit{
				EntityKey:  c.EntityKey,
				EntityType: c.EntityType,
				Reason:     reason,
			})
		}
	}
}

// flattenStepsToText collapses a Test's cached neutral step list into a single
// text blob for a backend whose step model is one inline-text field (Kiwi). It
// is the inverse of the read-path flattenSteps: for the common single-step case
// the text is exactly that step's Action, so a read→edit→write round-trips
// losslessly; multiple steps are joined with a blank-line separator.
func (e *Engine) flattenStepsToText(profileID, testKey string) (string, error) {
	steps, err := e.repo.ListTestSteps(profileID, testKey)
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(steps))
	for _, s := range steps {
		parts = append(parts, s.Action)
	}
	return strings.Join(parts, "\n\n"), nil
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
		if err := e.backend.SetTestRunStatus(ctx, execKey, testKey, c.AfterVal); err != nil {
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

		if err := e.backend.UpdateTestRequirements(ctx, testKey, add, removeLinkIDs); err != nil {
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

// commitRequirementEdits pushes requirement field edits, grouping a
// requirement's pending field changes into one issue update. Reported under the
// requirement's own issue key (which may be in another project).
func (e *Engine) commitRequirementEdits(ctx context.Context, profileID string, rows []testrepo.PendingChange, result *CommitResult) {
	byRequirement := make(map[string][]testrepo.PendingChange)
	order := make([]string, 0)
	for _, c := range rows {
		if _, seen := byRequirement[c.EntityKey]; !seen {
			order = append(order, c.EntityKey)
		}
		byRequirement[c.EntityKey] = append(byRequirement[c.EntityKey], c)
	}

	for _, key := range order {
		group := byRequirement[key]
		updates := make(map[string]string, len(group))
		ids := make([]int64, len(group))
		for i, c := range group {
			updates[c.Field] = c.AfterVal
			ids[i] = c.ID
		}
		if err := e.backend.UpdateIssue(ctx, key, e.backend.FieldsForJira(updates)); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: key, Error: "update requirement: " + sanitizeError(err.Error())})
			continue
		}
		if err := e.repo.CommitPendingChanges(profileID, ids); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: key, Error: "Jira accepted requirement update but local cleanup failed: " + err.Error()})
			continue
		}
		result.Succeeded = append(result.Succeeded, key)
	}
}

// commitRequirementDeletes deletes requirement issues in Jira, reported under
// the requirement's own issue key. The local row is dropped only after Jira
// confirms the delete so a failure leaves it pending for retry.
func (e *Engine) commitRequirementDeletes(ctx context.Context, profileID string, rows []testrepo.PendingChange, result *CommitResult) {
	for _, c := range rows {
		if err := e.backend.DeleteRequirement(ctx, c.EntityKey); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: c.EntityKey, Error: "delete requirement: " + sanitizeError(err.Error())})
			continue
		}
		if err := e.repo.CommitPendingChanges(profileID, []int64{c.ID}); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: c.EntityKey, Error: "Jira deleted requirement but local cleanup failed: " + err.Error()})
			continue
		}
		result.Succeeded = append(result.Succeeded, c.EntityKey)
	}
}

// commitBugCreates creates each queued Bug issue, repoints the placeholder key
// to the real one, then links it to its Test. Reported under the test key.
func (e *Engine) commitBugCreates(ctx context.Context, profileID string, rows []testrepo.PendingChange, result *CommitResult) {
	for _, c := range rows {
		var p struct {
			ProjectKey  string         `json:"projectKey"`
			IssueType   string         `json:"issueType"`
			Summary     string         `json:"summary"`
			Description string         `json:"description"`
			Priority    string         `json:"priority"`
			Labels      []string       `json:"labels"`
			TestKey     string         `json:"testKey"`
			Fields      map[string]any `json:"fields"`
		}
		if err := json.Unmarshal([]byte(c.AfterVal), &p); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: c.EntityKey, Error: "malformed bug payload: " + err.Error()})
			continue
		}
		realKey, err := e.backend.CreateBug(ctx, p.ProjectKey, p.IssueType, p.Summary, p.Description, p.Priority, p.Labels, p.Fields)
		if err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: p.TestKey, Error: "create bug: " + sanitizeError(err.Error())})
			continue
		}
		key := c.EntityKey
		if realKey != "" && realKey != c.EntityKey {
			if rErr := e.repo.RenameBug(profileID, c.EntityKey, realKey); rErr != nil {
				_ = rErr // remote create already succeeded; a cache-rename hiccup must not fail the commit
			}
			key = realKey
		}
		if err := e.backend.CreateBugLink(ctx, p.TestKey, key); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: p.TestKey, Error: "link bug: " + sanitizeError(err.Error())})
			continue
		}
		if err := e.repo.CommitPendingChanges(profileID, []int64{c.ID}); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: key, Error: "Jira created the bug but local cleanup failed: " + err.Error()})
			continue
		}
		result.Succeeded = append(result.Succeeded, key)
	}
}

// commitRequirementCreates creates each queued Requirement issue in Jira and
// repoints the placeholder key to the real one (mirrors commitBugCreates).
func (e *Engine) commitRequirementCreates(ctx context.Context, profileID string, rows []testrepo.PendingChange, result *CommitResult) {
	for _, c := range rows {
		var p struct {
			ProjectKey  string `json:"projectKey"`
			IssueType   string `json:"issueType"`
			Summary     string `json:"summary"`
			Description string `json:"description"`
			Priority    string `json:"priority"`
			Components  string `json:"components"`
			FixVersions string `json:"fixVersions"`
		}
		if err := json.Unmarshal([]byte(c.AfterVal), &p); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: c.EntityKey, Error: "malformed requirement payload: " + err.Error()})
			continue
		}
		realKey, err := e.backend.CreateRequirement(ctx, p.ProjectKey, p.IssueType, p.Summary, p.Description, p.Priority, p.Components, p.FixVersions)
		if err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: c.EntityKey, Error: "create requirement: " + sanitizeError(err.Error())})
			continue
		}
		key := c.EntityKey
		if realKey != "" && realKey != c.EntityKey {
			if rErr := e.repo.RenameRequirement(profileID, c.EntityKey, realKey); rErr != nil {
				_ = rErr // remote create succeeded; cache-rename hiccup must not fail the commit
			}
			key = realKey
		}
		if err := e.repo.CommitPendingChanges(profileID, []int64{c.ID}); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: key, Error: "Jira created the requirement but local cleanup failed: " + err.Error()})
			continue
		}
		result.Succeeded = append(result.Succeeded, key)
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
		if err := e.backend.AddComment(ctx, c.EntityKey, c.AfterVal); err != nil {
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
		if err := e.backend.AddComment(ctx, c.EntityKey, body); err != nil {
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
		realKey, err := e.backend.CreateTest(ctx, projectKey, p.Summary, p.Description, p.Priority, labels, components)
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
		// Create the new Test's steps (FR-10.7). Prefer the local cache, which
		// reflects any reorder/edit made after the test was drafted — the create
		// payload (p.Steps) can be stale. We capture each real step id and clear
		// the step pending rows below, so a leftover reorder can't later PUT
		// against the temporary "new-N" ids: that mismatch is what 500s when
		// committing a new test whose steps were reorganised.
		var stepSrc []testrepo.Step
		if cached, cErr := e.repo.ListTestSteps(profileID, key); cErr == nil && len(cached) > 0 {
			stepSrc = cached
		} else {
			for _, s := range p.Steps {
				stepSrc = append(stepSrc, testrepo.Step{Action: s.Action, Data: s.Data, Expected: s.Expected})
			}
		}
		stepErr := ""
		for _, s := range stepSrc {
			newID, sErr := e.createStep(ctx, profileID, key, s)
			if sErr != nil {
				stepErr = sanitizeError(sErr.Error())
				break
			}
			if newID != "" && s.XrayID != "" {
				_ = e.repo.RenameTestStepID(profileID, key, s.XrayID, newID)
			}
		}
		if stepErr != "" {
			// The Test was created but a step failed; leave the pending row so
			// the user can resolve and retry.
			result.Failed = append(result.Failed, FailedCommit{TestKey: key, Error: "create step: " + stepErr})
			continue
		}
		// Clear the test_create row PLUS any step-level pending rows for this Test:
		// the create just materialised the steps in their final order, so those
		// changes are done and must not re-run in the per-Test pass (where they'd
		// target ids that don't exist remotely). Step rows can be keyed by the
		// temp key (the bare reorder key isn't rekeyed by RenameTest) or the real
		// key, so collect both.
		toClear := append([]int64{c.ID}, e.stepPendingRowIDs(profileID, c.EntityKey)...)
		if key != c.EntityKey {
			toClear = append(toClear, e.stepPendingRowIDs(profileID, key)...)
		}
		if err := e.repo.CommitPendingChanges(profileID, toClear); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: key, Error: "Jira created test but local cleanup failed: " + err.Error()})
			continue
		}
		result.Succeeded = append(result.Succeeded, key)
	}
}

// createStep pushes one new step to Jira — a "call test" step (resolving the
// called Test's numeric id from the cache) or a normal manual step. Returns the
// new step's id when Jira reports one.
func (e *Engine) createStep(ctx context.Context, profileID, testKey string, s testrepo.Step) (string, error) {
	if s.CalledTestKey != "" {
		calledID := ""
		if tc, err := e.repo.GetTest(profileID, s.CalledTestKey); err == nil {
			calledID = tc.ID
		}
		return e.backend.CreateCalledTestStep(ctx, testKey, s.CalledTestKey, calledID)
	}
	return e.backend.CreateTestStep(ctx, testKey, s.Action, s.Data, s.Expected)
}

// stepPendingRowIDs returns the ids of step-level pending changes for a Test —
// edits/adds/deletes keyed "<testKey>:<id>" and the reorder keyed "<testKey>".
// Used after a brand-new Test's steps are created so those rows can be cleared
// instead of re-run (they reference local "new-N" ids that don't exist in Jira).
func (e *Engine) stepPendingRowIDs(profileID, testKey string) []int64 {
	changes, err := e.repo.ListPendingChanges(profileID)
	if err != nil {
		return nil
	}
	prefix := testKey + ":"
	var ids []int64
	for _, c := range changes {
		switch c.EntityType {
		case "test_step_order":
			if c.EntityKey == testKey {
				ids = append(ids, c.ID)
			}
		case "test_step", "test_step_add", "test_step_delete":
			if strings.HasPrefix(c.EntityKey, prefix) {
				ids = append(ids, c.ID)
			}
		}
	}
	return ids
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
			err = e.backend.CreateFolder(ctx, projectKey, p.ParentPath, p.Name)
		case "folder_rename":
			var p struct {
				Path string `json:"path"`
				Name string `json:"name"`
			}
			if jErr := json.Unmarshal([]byte(c.AfterVal), &p); jErr != nil {
				result.Failed = append(result.Failed, FailedCommit{TestKey: c.EntityKey, Error: "malformed folder payload: " + jErr.Error()})
				continue
			}
			err = e.backend.RenameFolder(ctx, projectKey, c.EntityKey, p.Name)
		case "folder_delete":
			err = e.backend.DeleteFolder(ctx, projectKey, c.EntityKey)
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
		realKey, err := e.backend.CreatePrecondition(ctx, pk, payload.Summary, payload.Type, payload.Description)
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
		if err := e.backend.UpdateIssue(ctx, key, e.backend.FieldsForJira(updates)); err != nil {
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
		if err := e.backend.DeletePrecondition(ctx, c.EntityKey); err != nil {
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
func (e *Engine) commitMemberships(ctx context.Context, profileID string, rows []testrepo.PendingChange, caps backend.Capabilities, result *CommitResult) {
	for _, c := range rows {
		// container_edit (rename) writes through UpdateIssue on the CONTAINER key.
		// That is only valid where containers are Jira issues (Xray). A backend
		// with a separate container namespace (Kiwi) would misread the container
		// key as a TestCase pk, so skip the rename and keep the row pending. Xray
		// keeps the UpdateIssue path unchanged.
		if c.EntityType == "container_edit" && !issueBackedWrites(caps) {
			result.Skipped = append(result.Skipped, SkippedCommit{
				EntityKey:  c.EntityKey,
				EntityType: c.EntityType,
				Reason:     skipReasonContainerEdit,
			})
			continue
		}
		var key string
		var err error
		switch c.EntityType {
		case "test_container_add":
			key, err = e.commitContainerCreate(ctx, profileID, c)
		case "test_membership_remove":
			key, err = e.commitMembershipRemove(ctx, c)
		case "container_edit":
			key, err = c.EntityKey, e.backend.UpdateIssue(ctx, c.EntityKey, e.backend.FieldsForJira(map[string]string{"summary": c.AfterVal}))
		case "container_delete":
			key, err = e.commitContainerDelete(ctx, c)
		case "container_env":
			key, err = e.commitContainerEnv(ctx, c)
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
	if err := e.backend.AddTestsToContainer(ctx, payload.Kind, c.EntityKey, payload.Members); err != nil {
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
	if err := e.backend.RemoveTestsFromContainer(ctx, payload.Kind, c.EntityKey, payload.Members); err != nil {
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
	if err := e.backend.DeleteContainer(ctx, snap.Kind, c.EntityKey); err != nil {
		return c.EntityKey, fmt.Errorf("delete container %s: %s", c.EntityKey, sanitizeError(err.Error()))
	}
	return c.EntityKey, nil
}

// commitContainerEnv pushes a container_env pending change (the new Test
// Environments set for a Test Execution) as a custom-field update on the
// execution issue. The after value is the JSON array of environment names. The
// real Xray field write is behind the demo short-circuit (TODO(xtm)); demo
// returns success so the pending change clears on a demo commit.
func (e *Engine) commitContainerEnv(ctx context.Context, c testrepo.PendingChange) (string, error) {
	var envs []string
	if err := json.Unmarshal([]byte(c.AfterVal), &envs); err != nil {
		return c.EntityKey, fmt.Errorf("malformed environments payload: %s", err)
	}
	if err := e.backend.SetContainerEnvironments(ctx, c.EntityKey, envs); err != nil {
		return c.EntityKey, fmt.Errorf("set environments on %s: %s", c.EntityKey, sanitizeError(err.Error()))
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
	realKey, err := e.backend.CreateContainer(ctx, payload.ProjectKey, payload.Kind, payload.Summary)
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
	if err := e.backend.AddTestsToContainer(ctx, payload.Kind, target, payload.Members); err != nil {
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
	transitions, err := e.backend.GetTransitions(ctx, testKey, change.BeforeVal)
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
	if err := e.backend.PostTransition(ctx, testKey, transitionID); err != nil {
		return fmt.Errorf("post transition: %s", sanitizeError(err.Error()))
	}
	return nil
}

// filterOutIDs returns the changes whose ID is not in drop.
func filterOutIDs(changes []testrepo.PendingChange, drop []int64) []testrepo.PendingChange {
	if len(drop) == 0 {
		return changes
	}
	skip := make(map[int64]bool, len(drop))
	for _, id := range drop {
		skip[id] = true
	}
	out := make([]testrepo.PendingChange, 0, len(changes))
	for _, c := range changes {
		if !skip[c.ID] {
			out = append(out, c)
		}
	}
	return out
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

// sanitizeError trims long Jira error responses so the UI shows a short,
// single-line message in the per-Test failure list.
func sanitizeError(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

// commitReqReqLinks pushes Requirement->Requirement link changes to Jira.
// Each pending_change row has entity_key = fromKey and field = linkType; before
// holds the snapshot ([]reqqLinkSnap with ToKey/LinkID), after holds the new
// target keys ([]string).
//
// NOTE(xtm): the live Jira call (UpdateRequirementLinks) is stubbed behind the
// demo short-circuit until the "requires" link type name and direction are
// verified against the live Xray Server/DC 8.4.0 instance.
// TODO(xtm): enable the live path once UpdateRequirementLinks is verified.
func (e *Engine) commitReqReqLinks(ctx context.Context, profileID string, rows []testrepo.PendingChange, result *CommitResult) {
	for _, c := range rows {
		fromKey := c.EntityKey
		var before []struct {
			ToKey  string `json:"toKey"`
			LinkID string `json:"linkId"`
		}
		var after []string
		if err := json.Unmarshal([]byte(c.BeforeVal), &before); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: fromKey, Error: "malformed req-link snapshot: " + err.Error()})
			continue
		}
		if err := json.Unmarshal([]byte(c.AfterVal), &after); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: fromKey, Error: "malformed req-link set: " + err.Error()})
			continue
		}
		afterSet := make(map[string]bool, len(after))
		for _, k := range after {
			afterSet[k] = true
		}
		removeLinkIDs := []string{}
		for _, l := range before {
			if !afterSet[l.ToKey] {
				removeLinkIDs = append(removeLinkIDs, l.LinkID)
			}
		}
		beforeSet := make(map[string]bool, len(before))
		for _, l := range before {
			beforeSet[l.ToKey] = true
		}
		add := []string{}
		for _, k := range after {
			if !beforeSet[k] {
				add = append(add, k)
			}
		}
		if err := e.backend.UpdateRequirementLinks(ctx, fromKey, add, removeLinkIDs); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: fromKey, Error: "update req links: " + sanitizeError(err.Error())})
			continue
		}
		if err := e.repo.CommitPendingChanges(profileID, []int64{c.ID}); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: fromKey, Error: "Jira updated req links but local cleanup failed: " + err.Error()})
			continue
		}
		result.Succeeded = append(result.Succeeded, fromKey)
	}
}
