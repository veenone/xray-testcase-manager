package kiwi

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// This file is P5.1's deliverable: the Kiwi TestCase WRITE surface
// (CreateTest, UpdateIssue, FieldsForJira) plus the tag/component diff and
// name->id resolution helpers they share. Everything here is scoped to a
// single TestCase — container/run writes (P5.2), bug writes (P5.3), and the
// commit-engine capability gating that routes status/steps correctly for
// Kiwi (P5.4) are explicitly OUT of scope; see p5_0-kiwi-write-spec.md.
//
// IMPORTANT caller note for P5.4: internal/syncer/commit.go already has a
// FEW call sites that invoke backend.UpdateIssue(ctx, key, FieldsForJira(...))
// for entity kinds OTHER than a test_case (e.g. commitMemberships' "container_edit"
// branch sends a container's key through this exact path to rename a
// TestPlan/TestRun). This adapter's UpdateIssue always treats key as a
// TestCase pk (TestCase.update) — it has no way to know from key+fields
// alone that a caller meant a container. Until the commit engine's
// capability gating (P5.4) routes container edits through Kiwi's own
// container-write methods (P5.2) instead of the shared UpdateIssue path,
// pointing a Kiwi backend at a live commit for a container_edit row would
// incorrectly attempt TestCase.update against a TestPlan/TestRun id. Flagging
// for the P5.4 task rather than working around it here (out of this task's
// scope).

// kiwiPriorityRow/kiwiStatusRow/kiwiCategoryRow are the id-bearing decode
// shapes for Priority.filter/TestCaseStatus.filter/Category.filter used by
// name/value -> id resolution. The read path's kiwiValue/kiwiName types
// (caps.go/convert.go) intentionally drop the id (ListPriorities/ListStatuses
// only need display strings), so these are separate structs rather than a
// reused type.
type kiwiPriorityRow struct {
	ID    int    `json:"id"`
	Value string `json:"value"`
}

type kiwiStatusRow struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type kiwiCategoryRow struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// kiwiCreatedCase decodes the row TestCase.create returns (spec
// p4_verify-report.md "Create-path facts": "Returns the new row incl.
// integer id" — only id is needed here).
type kiwiCreatedCase struct {
	ID int `json:"id"`
}

// resolvePriorityID resolves a Priority VALUE string (e.g. "P1") to its
// Kiwi pk via Priority.filter({"value": value}) — the exact-match narrowing
// query, mirroring ListPriorities' {"is_active":true} filter call but keyed
// on value instead. Errors (rather than guessing) when no row matches: an
// unresolvable priority is a real data problem, not something to paper over
// with an invented default.
func (a *Adapter) resolvePriorityID(ctx context.Context, value string) (int, error) {
	var rows []kiwiPriorityRow
	if err := a.c.call(ctx, "Priority.filter", []any{map[string]any{"value": value}}, &rows); err != nil {
		return 0, err
	}
	if len(rows) > 0 {
		return rows[0].ID, nil
	}
	// The requested priority name is not one of Kiwi's (e.g. an Xray "High"
	// published across the bridge into Kiwi, whose priorities are P1..P5).
	// Rather than hard-fail the create/update, fall back to Kiwi's first
	// available priority so a cross-backend publish degrades gracefully (the
	// bridge gap report already flags that priority may not map). A
	// Kiwi-native edit always passes a real Kiwi priority, so this fallback
	// only triggers on a genuine name mismatch. Filter on {"is_active":true}
	// (mirroring ListPriorities) so the fallback can only land on an ACTIVE
	// priority — never an archived/inactive one — which also keeps the pick
	// deterministic against the same set ListPriorities surfaces.
	var all []kiwiPriorityRow
	if err := a.c.call(ctx, "Priority.filter", []any{map[string]any{"is_active": true}}, &all); err != nil {
		return 0, err
	}
	if len(all) == 0 {
		return 0, fmt.Errorf("kiwi: no priorities available to resolve %q", value)
	}
	return all[0].ID, nil
}

// resolveStatusID resolves a TestCaseStatus NAME (e.g. "CONFIRMED") to its
// Kiwi pk via TestCaseStatus.filter({"name": name}).
func (a *Adapter) resolveStatusID(ctx context.Context, name string) (int, error) {
	var rows []kiwiStatusRow
	if err := a.c.call(ctx, "TestCaseStatus.filter", []any{map[string]any{"name": name}}, &rows); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, fmt.Errorf("kiwi: case_status %q not found", name)
	}
	return rows[0].ID, nil
}

// resolveDefaultStatusID picks the initial case_status id for CreateTest,
// which Kiwi requires but this app's neutral CreateTest signature has no
// slot for (spec: "Required fields (verified): summary, case_status,
// category, priority" with no settable-status equivalent on the interface).
// Documented default choice (brief: "pick a sensible default (e.g. the
// first CONFIRMED/PROPOSED status id)"): prefer "PROPOSED" — Kiwi's own
// convention for a newly authored, not-yet-reviewed case — then "CONFIRMED"
// as a fallback for an instance that pruned PROPOSED from its status list,
// then simply the first TestCaseStatus row returned (Kiwi's schema has no
// "is_default" flag to key off instead). This is a real, documented
// decision — not an invented remote-mutating behavior — because every Kiwi
// instance ships with a non-empty TestCaseStatus table (it's how Kiwi's own
// UI populates the status dropdown) and TestCase.create hard-requires SOME
// case_status id.
func (a *Adapter) resolveDefaultStatusID(ctx context.Context) (int, error) {
	var rows []kiwiStatusRow
	if err := a.c.call(ctx, "TestCaseStatus.filter", []any{map[string]any{}}, &rows); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, fmt.Errorf("kiwi: no TestCaseStatus rows available on this instance; cannot create a test case")
	}
	for _, want := range []string{"PROPOSED", "CONFIRMED"} {
		for _, r := range rows {
			if strings.EqualFold(r.Name, want) {
				return r.ID, nil
			}
		}
	}
	return rows[0].ID, nil
}

// resolveCategoryID resolves the Category id CreateTest requires, scoped to
// projectKey's product via Category.filter({"product__name": projectKey}) —
// the same `product__name` scoping convention ProjectComponents/
// ProjectVersions already use (adapter.go). This is the brief's "resolve a
// category for the product" option: a product commonly has exactly one
// (or a small, curated) set of categories, so when more than one is
// returned this picks the lowest id deterministically (documented arbitrary
// tiebreak, not a semantic choice — there is no "default category" concept
// in Kiwi to key off instead).
//
// When projectKey is empty, or the product has NO categories at all, this
// returns a clear error rather than inventing/creating one: a Kiwi Category
// only ever exists because someone created it under a Product (spec
// p4_verify-report.md's create-path facts: "Category.create -> {name,
// product}"), so silently fabricating one here would be a real
// remote-mutating guess this task must not make.
func (a *Adapter) resolveCategoryID(ctx context.Context, projectKey string) (int, error) {
	if strings.TrimSpace(projectKey) == "" {
		return 0, fmt.Errorf("kiwi: CreateTest requires a product (projectKey) to resolve a Category")
	}
	var rows []kiwiCategoryRow
	if err := a.c.call(ctx, "Category.filter", []any{map[string]any{"product__name": projectKey}}, &rows); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, fmt.Errorf("kiwi: no Category found for product %q; create one in Kiwi (Category.create needs {name, product}) before creating tests here", projectKey)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows[0].ID, nil
}

// parseLabelSet splits a FieldsForJira "labels" value back into names.
// strings.Fields (whitespace-separated) matches the convention already used
// throughout this codebase for the labels field (internal/jira's
// FieldsForJira does the same split; internal/testrepo joins Labels with a
// space when writing the column) — Xray/Jira labels never contain spaces,
// so this format carries no ambiguity.
func parseLabelSet(v string) []string {
	return strings.Fields(v)
}

// parseComponentSet splits a FieldsForJira "components" value back into
// names. Unlike labels, component names CAN contain spaces (e.g. "User
// Management" — see internal/testrepo's CSV export), so this uses a
// comma-separated convention instead (mirroring exportcsv.go/gapanalysis.go's
// `strings.Join(components, ", ")`), trimming surrounding whitespace on each
// name. There is no existing pending-change producer for a "components"
// field yet (EditTestField's whitelist has no "components" entry today), so
// this format is this task's documented design choice for when one is
// added, kept consistent with the rest of the codebase's CSV convention
// rather than invented from nothing.
func parseComponentSet(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// diffNames computes the add/remove sets to turn current into wanted,
// case-sensitive exact-name comparison (Tag/Component names are matched by
// literal name in Kiwi's own add_tag/remove_tag/add_component/
// remove_component RPCs — spec: "component by NAME not id"). Both slices are
// sorted for deterministic RPC ordering (and deterministic test
// assertions). A name present in both sets is left alone — this is what
// makes an add of an already-present tag a no-op (brief: "Keep it
// idempotent") rather than an extra RPC call.
func diffNames(current, wanted []string) (adds, removes []string) {
	curSet := make(map[string]bool, len(current))
	for _, c := range current {
		curSet[c] = true
	}
	wantSet := make(map[string]bool, len(wanted))
	for _, w := range wanted {
		wantSet[w] = true
	}
	for w := range wantSet {
		if !curSet[w] {
			adds = append(adds, w)
		}
	}
	for c := range curSet {
		if !wantSet[c] {
			removes = append(removes, c)
		}
	}
	sort.Strings(adds)
	sort.Strings(removes)
	return adds, removes
}

// applyTagDiff fetches id's current tags (reusing the P4.6 single-case
// fetch), diffs against wanted, and applies the delta via
// TestCase.add_tag/TestCase.remove_tag (spec: both take (case_id, name) and
// add_tag returns null on success).
func (a *Adapter) applyTagDiff(ctx context.Context, id int, wanted []string) error {
	current, err := a.fetchTagsForCase(ctx, id)
	if err != nil {
		return err
	}
	adds, removes := diffNames(current, wanted)
	for _, name := range adds {
		if err := a.c.call(ctx, "TestCase.add_tag", []any{id, name}, nil); err != nil {
			return fmt.Errorf("kiwi: add_tag %q: %w", name, err)
		}
	}
	for _, name := range removes {
		if err := a.c.call(ctx, "TestCase.remove_tag", []any{id, name}, nil); err != nil {
			return fmt.Errorf("kiwi: remove_tag %q: %w", name, err)
		}
	}
	return nil
}

// applyComponentDiff is applyTagDiff's Component counterpart, using
// TestCase.add_component/TestCase.remove_component — LIVE-VERIFIED to take
// the component NAME, not its id (p4_verify-report.md: "passing the numeric
// id errors 'Component matching query does not exist'").
func (a *Adapter) applyComponentDiff(ctx context.Context, id int, wanted []string) error {
	current, err := a.fetchComponentsForCase(ctx, id)
	if err != nil {
		return err
	}
	adds, removes := diffNames(current, wanted)
	for _, name := range adds {
		if err := a.c.call(ctx, "TestCase.add_component", []any{id, name}, nil); err != nil {
			return fmt.Errorf("kiwi: add_component %q: %w", name, err)
		}
	}
	for _, name := range removes {
		if err := a.c.call(ctx, "TestCase.remove_component", []any{id, name}, nil); err != nil {
			return fmt.Errorf("kiwi: remove_component %q: %w", name, err)
		}
	}
	return nil
}

// FieldsForJira translates the app's neutral field/value pairs (spec
// vocabulary mirrored from internal/jira's FieldsForJira: summary,
// description, priority, labels) into a Kiwi-oriented values map that
// UpdateIssue finishes. This is a PURE map translation — no ctx, no RPC —
// because id resolution (priority value -> id, case_status name -> id)
// needs an RPC round trip and belongs in UpdateIssue, not here (brief item
// 1). Two extra keys beyond the brief's literal list are supported for
// forward-compat with later engine wiring, both harmless no-ops today
// because nothing currently produces them:
//   - "status" (the pending-change journal's field name for a status
//     transition, see internal/testrepo's editableFields/commit.go's
//     statusChange routing) is carried through as "case_status" so a future
//     P5.4 change that routes a Kiwi settable-status edit as a plain field
//     update can call UpdateIssue directly without a second translation
//     path (brief item 2: "support a status/case_status field here too").
//   - "components" is carried through unresolved, exactly like "labels",
//     for the same reason (item 3) — EditTestField has no "components"
//     entry yet, so nothing sends this today.
//
// Anything else (no Kiwi analog) is silently omitted, matching internal/jira's
// FieldsForJira behavior for an unrecognized field name.
func (a *Adapter) FieldsForJira(updates map[string]string) map[string]any {
	out := make(map[string]any, len(updates))
	for f, v := range updates {
		switch f {
		case "summary":
			out["summary"] = v
		case "description":
			out["text"] = v
		case "priority":
			out["priority"] = v
		case "status", "case_status":
			out["case_status"] = v
		case "labels":
			out["labels"] = v
		case "components":
			out["components"] = v
		}
	}
	return out
}

// UpdateIssue applies a FieldsForJira-shaped map to the TestCase identified
// by key (spec item 2). Plain-string fields (summary/text) are passed
// through as-is; priority/case_status are resolved name/value -> id via the
// TestCaseStatus.filter/Priority.filter RPCs (resolvePriorityID/
// resolveStatusID above) before being sent — Kiwi's TestCase.update expects
// FK ids, not the display strings the rest of this app uses (spec:
// "Settable status = {\"case_status\": <id>}"). labels/components are NOT
// part of the TestCase.update values dict: Kiwi writes them via dedicated
// m2m RPCs (item 3), so they are diffed and applied separately via
// applyTagDiff/applyComponentDiff after the TestCase.update call (if any)
// completes. An empty resulting values map skips the TestCase.update call
// entirely rather than sending a no-op update.
func (a *Adapter) UpdateIssue(ctx context.Context, key string, fields map[string]any) error {
	id, err := parseKiwiID(key)
	if err != nil {
		return err
	}

	values := map[string]any{}
	if v, ok := fields["summary"].(string); ok {
		values["summary"] = v
	}
	if v, ok := fields["text"].(string); ok {
		values["text"] = v
	}
	if v, ok := fields["priority"].(string); ok {
		pid, err := a.resolvePriorityID(ctx, v)
		if err != nil {
			return err
		}
		values["priority"] = pid
	}
	if v, ok := fields["case_status"].(string); ok {
		sid, err := a.resolveStatusID(ctx, v)
		if err != nil {
			return err
		}
		values["case_status"] = sid
	}

	if len(values) > 0 {
		if err := a.c.call(ctx, "TestCase.update", []any{id, values}, nil); err != nil {
			return err
		}
	}

	if v, ok := fields["labels"].(string); ok {
		if err := a.applyTagDiff(ctx, id, parseLabelSet(v)); err != nil {
			return err
		}
	}
	if v, ok := fields["components"].(string); ok {
		if err := a.applyComponentDiff(ctx, id, parseComponentSet(v)); err != nil {
			return err
		}
	}
	return nil
}

// CreateTest creates a new TestCase via TestCase.create, then attaches the
// given labels/components via the same add_tag/add_component RPCs UpdateIssue
// uses (spec item 4). Kiwi's create RPC hard-requires {summary, case_status,
// category, priority} (p4_verify-report.md's create-path facts) — none of
// which have a settable-status/category slot on this app's neutral CreateTest
// signature, so case_status uses resolveDefaultStatusID's documented default
// and category is resolved from projectKey via resolveCategoryID (erroring,
// not guessing, when the product has no Category — see that function's doc
// comment). description maps to Kiwi's single `text` field, omitted when
// empty (is_automated/author are left at Kiwi's own defaults: Manual,
// caller). The returned id is the new TestCase's Kiwi pk, stringified — this
// becomes the entity's key; the temp-key rename choreography that rewrites
// dependent pending rows onto it is P5.5, not here.
//
// If the TestCase.create call itself succeeds but a later add_tag/
// add_component call fails, the TestCase now exists remotely (orphaned
// under its correctly-created summary/status/category/priority, just
// missing the requested tags/components) but this still returns a non-nil
// error — the created id is returned ALONGSIDE the error so a caller that
// chooses to inspect it can recover, but internal/syncer/commit.go's current
// contract treats any non-nil CreateTest error as "the whole create failed"
// and never renames the temp key onto the returned id (see commit.go's
// commitTestCreates: `if err != nil { ...; continue }` — the realKey is
// simply dropped). This partial-success window is NEW to Kiwi and does not
// exist for Xray: Xray's CreateTest is a single atomic issue POST with
// labels/components embedded in the payload (internal/jira), so it can never
// half-succeed. The engine's drop-the-id-on-any-error contract is what's
// pre-existing; the multi-RPC create-then-attach shape that can trip it is
// Kiwi-specific. Reconciling it needs an engine change (out of this
// Kiwi-only task's scope) — flagging it for the P5.4/P5.5 engine work.
func (a *Adapter) CreateTest(ctx context.Context, projectKey, summary, description, priority string, labels, components []string) (string, error) {
	priorityID, err := a.resolvePriorityID(ctx, priority)
	if err != nil {
		return "", fmt.Errorf("resolve priority: %w", err)
	}
	statusID, err := a.resolveDefaultStatusID(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve default case_status: %w", err)
	}
	categoryID, err := a.resolveCategoryID(ctx, projectKey)
	if err != nil {
		return "", fmt.Errorf("resolve category: %w", err)
	}

	values := map[string]any{
		"summary":     summary,
		"case_status": statusID,
		"category":    categoryID,
		"priority":    priorityID,
	}
	if strings.TrimSpace(description) != "" {
		values["text"] = description
	}

	var created kiwiCreatedCase
	if err := a.c.call(ctx, "TestCase.create", []any{values}, &created); err != nil {
		return "", err
	}
	key := strconv.Itoa(created.ID)

	for _, name := range labels {
		if s := strings.TrimSpace(name); s != "" {
			if err := a.c.call(ctx, "TestCase.add_tag", []any{created.ID, s}, nil); err != nil {
				return key, fmt.Errorf("test case created (id %d) but add_tag %q failed: %w", created.ID, s, err)
			}
		}
	}
	for _, name := range components {
		if s := strings.TrimSpace(name); s != "" {
			if err := a.c.call(ctx, "TestCase.add_component", []any{created.ID, s}, nil); err != nil {
				return key, fmt.Errorf("test case created (id %d) but add_component %q failed: %w", created.ID, s, err)
			}
		}
	}
	return key, nil
}
