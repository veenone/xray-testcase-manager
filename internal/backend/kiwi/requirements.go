package kiwi

import (
	"encoding/json"
	"strconv"

	"xray-test-manager/internal/backend"
)

// This file is the P4.3 requirements-plugin read path: it implements the
// ONLY two RPC methods kiwi-tcms-requirements exposes over JSON-RPC
// (tcms_requirements/rpc.py — spec §4.3, §8.1): `Requirement.filter` and
// `Requirement.coverage`. Everything else in the plugin's model (req->req
// links, doc-control/safety-class/programme fields) is NOT reachable via
// RPC today (spec §3.8, OQ-3/OQ-4) and is simply not populated; since
// backend/dto.go is out of scope for this task (no new capability/DTO
// fields), there is also no passthrough/"extras" bag on backend.Requirement
// or backend.RequirementLink to carry plugin-only fields into — anything
// the plugin returns beyond the fields listed below is read into a Go DTO
// (if decoded at all) and then dropped, not silently lost data we could
// have kept: it was never representable on the neutral DTO to begin with.

// kiwiRequirement mirrors one row of a `Requirement.filter({})` REGISTRY
// query (spec §3.8's field-map table, §8.1: "query supports
// {identifier/status/priority/q} (returns registry rows with id,
// identifier, title, status, priority, level, jira_issue_key)"). Only the
// fields spec §3.8's neutral-mapping table actually consumes are decoded;
// identifier/level/jira_issue_key exist on the plugin's response but have
// no backend.Requirement field to land in (spec §3.8: "ProjectKey/Updated/
// Description/Components/FixVersions/Sprint/EpicKey -- not in RPC shape ->
// \"\""), so they are intentionally omitted from this DTO rather than
// decoded and discarded.
type kiwiRequirement struct {
	ID       int    `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}

// toRequirement maps a kiwiRequirement to the neutral backend.Requirement
// per spec §3.8's literal field table: Key<-id, Summary<-title,
// Status<-status, Priority<-priority, IssueType<-the constant "requirement"
// (the plugin models one requirement "type"), and every other
// backend.Requirement field stays "" because the RPC shape has no source
// for it (ProjectKey especially: OQ-4, the plugin's small response carries
// no product).
func toRequirement(r kiwiRequirement) backend.Requirement {
	return backend.Requirement{
		Key:        strconv.Itoa(r.ID),
		ProjectKey: "",
		IssueType:  "requirement",
		Summary:    r.Title,
		Status:     r.Status,
		Priority:   r.Priority,
	}
}

// kiwiRequirementCoverage mirrors the response of
// `Requirement.coverage(requirement_id)`. Spec §4.3/§8.1 cite this RPC's
// shape ONLY as an aggregate/stats object:
//
//	{id, identifier, link_count, suspect_count, link_types{}}
//
// No p4_0 §9 fixture exists for Requirement.coverage, and the spec never
// documents what is INSIDE `link_types{}` — only that it exists. The P4.3
// brief nonetheless directs building backend.RequirementLink rows (i.e.
// individual test<->requirement associations) from this call, which
// requires per-test case ids to exist somewhere in the response. This is a
// genuinely INFERRED shape, flagged here per the brief's "flag + degrade
// safely" instruction for exactly this call:
//
//	FLAG: `link_types` is assumed to be a map from link-type name (e.g.
//	"verifies") to an array of the covering TestCase ids, either as bare
//	ints ([42, 43]) or as objects carrying a case id under a "case",
//	"case_id", or "id" key ([{"case": 42}]). THIS IS UNCONFIRMED — no cited
//	source pins it. If a live Kiwi instance's `link_types` turns out to be
//	pure per-type COUNTS (e.g. {"verifies": 2}) rather than case-id lists,
//	extractCoverageCaseIDs degrades to "no links recoverable for that type"
//	(nil, not an error) rather than panicking or failing the whole
//	ListRequirements call — the Requirement objects themselves (from
//	Requirement.filter) are unaffected either way.
type kiwiRequirementCoverage struct {
	ID           int                        `json:"id"`
	LinkCount    int                        `json:"link_count"`
	SuspectCount int                        `json:"suspect_count"`
	LinkTypes    map[string]json.RawMessage `json:"link_types"`
}

// extractCoverageCaseIDs recovers TestCase ids from one per-link-type value
// of kiwiRequirementCoverage.LinkTypes, trying (in order) a bare-int array
// and an array of objects carrying the id under case/case_id/id. Any other
// shape (e.g. a bare count) degrades to nil — see the FLAG on
// kiwiRequirementCoverage above.
func extractCoverageCaseIDs(raw json.RawMessage) []int {
	var ids []int
	if err := json.Unmarshal(raw, &ids); err == nil {
		return ids
	}

	var objs []struct {
		Case   *int `json:"case"`
		CaseID *int `json:"case_id"`
		ID     *int `json:"id"`
	}
	if err := json.Unmarshal(raw, &objs); err == nil {
		out := make([]int, 0, len(objs))
		for _, o := range objs {
			switch {
			case o.Case != nil:
				out = append(out, *o.Case)
			case o.CaseID != nil:
				out = append(out, *o.CaseID)
			case o.ID != nil:
				out = append(out, *o.ID)
			}
		}
		return out
	}

	// Neither shape matched (e.g. a bare count like `2`): no case ids
	// recoverable for this link type.
	return nil
}

// toRequirementLinks builds the backend.RequirementLink rows for one
// requirement from its Requirement.coverage response. LinkID is
// synthesized as "<requirementKey>-<testKey>" because the plugin's small
// response shape has no link primary key of its own (spec §3.8, OQ-4 — the
// same synthesis rule the per-case Requirement.filter variant would need).
// The plugin-only link_type/suspect/coverage_notes fields, and the
// aggregate LinkCount/SuspectCount this call also returns, are not carried:
// backend.RequirementLink has no field for them (dto.go is out of scope for
// this task).
func toRequirementLinks(cov kiwiRequirementCoverage, requirementKey string) []backend.RequirementLink {
	var links []backend.RequirementLink
	for _, raw := range cov.LinkTypes {
		for _, caseID := range extractCoverageCaseIDs(raw) {
			testKey := strconv.Itoa(caseID)
			links = append(links, backend.RequirementLink{
				TestKey:        testKey,
				RequirementKey: requirementKey,
				LinkID:         requirementKey + "-" + testKey,
			})
		}
	}
	return links
}
