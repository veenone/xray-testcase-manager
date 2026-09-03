package kiwi

import (
	"strconv"
	"strings"

	"xray-test-manager/internal/backend"
)

// kiwiUser mirrors the subset of Kiwi's User RPC output that
// Adapter.TestConnection needs: username, first_name, last_name, email
// (standard Django User fields). Spec §3.1.
type kiwiUser struct {
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
}

// toUser maps a kiwiUser to the neutral backend.User, falling back to the
// username when no first/last name is set.
func toUser(u kiwiUser) *backend.User {
	display := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if display == "" {
		display = u.Username
	}
	return &backend.User{
		Name:        u.Username,
		DisplayName: display,
		Email:       u.Email,
	}
}

// kiwiTestCase mirrors the fields of a `TestCase.filter` row this package
// maps into backend.Test / backend.TestBasic / backend.TestMeta, per the
// LIVE-VERIFIED shape in p4_verify-report.md (P4.6 fix, superseding the P4.2
// inferred shape): the row carries neither `tag` nor `component` (both keys
// are simply absent — fetched separately via Tag.filter/Component.filter,
// see adapter.go's fetchTagsFor{Case,Cases}/fetchComponentsFor{Case,Cases})
// nor `category__product__name` (only `category__name` is present, so
// ProjectKey stays "" — see toTestBasic). The row DOES carry `history_date`,
// which was present-but-unused before this fix; toTest/toTestMeta now map it
// to Updated (Kiwi's TestCase has no native `updated` field).
type kiwiTestCase struct {
	ID             int    `json:"id"`
	Summary        string `json:"summary"`
	Text           string `json:"text"`
	CaseStatusName string `json:"case_status__name"`
	PriorityValue  string `json:"priority__value"`
	IsAutomated    bool   `json:"is_automated"`
	AuthorUsername string `json:"author__username"`
	CreateDate     string `json:"create_date"`
	HistoryDate    string `json:"history_date"`
	// Category is Kiwi's per-product grouping of test cases, which is the
	// closest analogue to an Xray Test Repository folder: one per test,
	// scoped to the product, and used to organise the repository. Both keys
	// are on the live row (verified against a real instance) and were
	// previously decoded by neither, so every test synced with an empty
	// FolderID. The id is what folder membership is keyed on, since category
	// names are only unique within a product.
	CategoryID   int    `json:"category"`
	CategoryName string `json:"category__name"`
}

// kiwiDefaultCategory is the category Kiwi creates for every product. Tests
// sitting in it have not been filed anywhere, so it maps to "no folder"
// rather than to a folder literally named "--default--".
const kiwiDefaultCategory = "--default--"

// categoryFolderID returns the folder id for a test's category, or "" when the
// test is unfiled. Kiwi's category ids are globally unique, so the id alone
// identifies the folder.
func categoryFolderID(tc kiwiTestCase) string {
	if tc.CategoryID == 0 || tc.CategoryName == kiwiDefaultCategory {
		return ""
	}
	return strconv.Itoa(tc.CategoryID)
}

// flattenSteps is the SINGLE shared transform for Kiwi's inline-text step
// model (spec §7, §3.3): Kiwi has no step objects, only one `text` field per
// TestCase, so "flattening" collapses to at most one neutral Step whose
// Action holds the raw text verbatim (empty text -> no steps). This exact
// function backs BOTH GetTestSteps (returns the step list directly) and
// toTest (derives Description from the same call), so the two can never
// drift out of sync — Description is always steps[0].Action whenever steps
// are non-empty.
func flattenSteps(text string) []backend.Step {
	if text == "" {
		return []backend.Step{}
	}
	return []backend.Step{{ID: "1", Index: 1, Action: text}}
}

// toTest maps a kiwiTestCase to the neutral backend.Test per spec §3.2,
// plus its separately-fetched tag/component names (P4.6 fix item 1 — see
// kiwiTestCase's doc comment). ExecType derives from is_automated
// (Automated/Manual); FolderID is always empty (no folder concept in Kiwi
// core, spec §3.10); FixVersions is always nil (fix versions live on the
// Run, not the TestCase, spec §3.2); Updated maps from tc.HistoryDate (P4.6
// fix item 3 — Kiwi's TestCase has no native `updated` field, but
// `history_date` serves the same purpose and needs no extra RPC).
func toTest(tc kiwiTestCase, tags, components []string) backend.Test {
	steps := flattenSteps(tc.Text)
	description := tc.Text
	if len(steps) > 0 {
		description = steps[0].Action
	}
	execType := "Manual"
	if tc.IsAutomated {
		execType = "Automated"
	}
	key := strconv.Itoa(tc.ID)
	return backend.Test{
		Key:         key,
		ID:          key,
		Summary:     tc.Summary,
		Description: description,
		Status:      tc.CaseStatusName,
		Priority:    tc.PriorityValue,
		Labels:      tags,
		Components:  components,
		Updated:     tc.HistoryDate,
		FolderID:    categoryFolderID(tc),
		ExecType:    execType,
		FixVersions: nil,
	}
}

// toTestBasic maps a kiwiTestCase to the neutral backend.TestBasic per spec
// §3.1. IssueLinks is always nil: Kiwi core has no cross-issue link concept
// on a TestCase. ProjectKey is always "" (P4.6 fix item 2): the
// live-verified TestCase.filter row exposes only `category__name`, never
// `category__product__name` — product name would need a separate
// Category/Product lookup, deferred rather than invented.
func toTestBasic(tc kiwiTestCase) backend.TestBasic {
	return backend.TestBasic{
		Key:        strconv.Itoa(tc.ID),
		Summary:    tc.Summary,
		Status:     tc.CaseStatusName,
		ProjectKey: "",
		IssueLinks: nil,
	}
}

// toTestMeta maps a kiwiTestCase to the neutral backend.TestMeta per spec
// §3.1 ("GetTestMeta ... Updated best-effort"). Updated maps from
// tc.HistoryDate (P4.6 fix item 3, same source toTest uses for
// backend.Test.Updated, so the two never disagree); UpdatedBy stays empty —
// history_date carries no accompanying "who" field on the row.
func toTestMeta(tc kiwiTestCase) backend.TestMeta {
	return backend.TestMeta{
		Created:   tc.CreateDate,
		Creator:   tc.AuthorUsername,
		Updated:   tc.HistoryDate,
		UpdatedBy: "",
	}
}

// kiwiTestPlan mirrors a `TestPlan.filter` row (spec §3.5: "TestPlan ->
// Container{Kind:KindTestPlan, Key:id, Summary:name, ParentKey:parent,
// Description:text}" — id/name/parent/text are the literal Kiwi field names
// the spec table cites). Parent is a pointer because a root plan has no
// parent (null).
type kiwiTestPlan struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Parent *int   `json:"parent"`
	Text   string `json:"text"`
}

// toContainerFromPlan maps a kiwiTestPlan to the neutral backend.Container,
// Kind=KindTestPlan, per spec §3.5.
func toContainerFromPlan(p kiwiTestPlan) backend.Container {
	parentKey := ""
	if p.Parent != nil && *p.Parent != 0 {
		parentKey = strconv.Itoa(*p.Parent)
	}
	return backend.Container{
		Key:         strconv.Itoa(p.ID),
		Kind:        backend.KindTestPlan,
		Summary:     p.Name,
		ParentKey:   parentKey,
		Description: p.Text,
	}
}

// kiwiTestRun mirrors a `TestRun.filter` row (spec §3.5: "TestRun ->
// Container{Kind:KindTestExec, Key:id, Summary:summary, ParentKey:plan,
// Environments:[build.name], Created:start_date, Resolved:stop_date}").
// BuildName reuses the `build__name` annotation convention the §9.1c
// TestExecution fixture already demonstrates for a Build FK. Plan is a
// plain int (never null): spec §3.5 ExecPlans note says "a Kiwi run belongs
// to exactly one plan".
type kiwiTestRun struct {
	ID        int    `json:"id"`
	Summary   string `json:"summary"`
	Plan      int    `json:"plan"`
	BuildName string `json:"build__name"`
	StartDate string `json:"start_date"`
	StopDate  string `json:"stop_date"`
}

// toContainerFromRun maps a kiwiTestRun to the neutral backend.Container,
// Kind=KindTestExec, per spec §3.5.
func toContainerFromRun(r kiwiTestRun) backend.Container {
	var envs []string
	if r.BuildName != "" {
		envs = []string{r.BuildName}
	}
	return backend.Container{
		Key:          strconv.Itoa(r.ID),
		Kind:         backend.KindTestExec,
		Summary:      r.Summary,
		ParentKey:    strconv.Itoa(r.Plan),
		Environments: envs,
		Created:      r.StartDate,
		Resolved:     r.StopDate,
	}
}

// kiwiTestExecution mirrors a `TestExecution.filter` row — spec §9.1c gives
// this exact fixture shape (id, run, case, status__name,
// assignee__username, tested_by__username, build__name, start_date,
// stop_date). It backs both the neutral backend.TestRun DTO (spec §3.7) and
// container membership links (spec §3.5).
type kiwiTestExecution struct {
	ID               int    `json:"id"`
	Run              int    `json:"run"`
	Case             int    `json:"case"`
	StatusName       string `json:"status__name"`
	AssigneeUsername string `json:"assignee__username"`
	TestedByUsername string `json:"tested_by__username"`
	BuildName        string `json:"build__name"`
	StartDate        string `json:"start_date"`
	StopDate         string `json:"stop_date"`
}

// toTestRunDTO maps a kiwiTestExecution to the neutral backend.TestRun per
// spec §3.7. ExecutedBy prefers tested_by__username, falling back to
// assignee__username (spec: "ExecutedBy | tested_by__username | fallback
// assignee__username"). Defects is always nil in P4.2: populating it needs
// TestExecution.get_links, explicitly deferred to P4.3 (spec §3.7/§3.9, and
// the P4.1 report's phase assignment). CreatedAt/UpdatedAt reuse the
// execution's own start_date/stop_date as the best-effort value the spec
// allows ("run start_date/stop_date ... best-effort").
func toTestRunDTO(e kiwiTestExecution) backend.TestRun {
	executedBy := e.TestedByUsername
	if executedBy == "" {
		executedBy = e.AssigneeUsername
	}
	return backend.TestRun{
		TestKey:     strconv.Itoa(e.Case),
		Status:      e.StatusName,
		StartedAt:   e.StartDate,
		FinishedAt:  e.StopDate,
		ExecutedBy:  executedBy,
		Environment: e.BuildName,
		Defects:     nil,
		CreatedAt:   e.StartDate,
		UpdatedAt:   e.StopDate,
	}
}

// toExecContainerLink maps a kiwiTestExecution to the ContainerLink between
// its parent TestRun (as a KindTestExec Container) and the executed Test,
// per spec §3.5 ("TestRun -> ... TestExecution.filter({"run":runid}) ->
// ContainerLink{ContainerKey:run, TestKey:case, RunStatus:status__name}").
func toExecContainerLink(e kiwiTestExecution) backend.ContainerLink {
	return backend.ContainerLink{
		ContainerKey: strconv.Itoa(e.Run),
		TestKey:      strconv.Itoa(e.Case),
		RunStatus:    e.StatusName,
	}
}

// kiwiName and kiwiValue are the two shapes the metadata RPCs (spec §3.12)
// return: TestCaseStatus.filter/Component.filter rows expose a `name`,
// Priority.filter/Version.filter rows expose a `value`.
type kiwiName struct {
	Name string `json:"name"`
}

type kiwiValue struct {
	Value string `json:"value"`
}

// kiwiTagRow mirrors one row of Tag.filter({"case":id}) /
// Tag.filter({"case__in":[...]}) — LIVE-VERIFIED shape (p4_verify-report.md,
// shape-tag.json): {"id":1,"name":"smoke","case":1,"plan":null,"run":null,
// "execution":null,"bugs":null}. Case is the TestCase id the tag is attached
// to; it is what lets a batched case__in fetch (adapter.go's
// fetchTagsForCases) regroup a single flat result array back by case id.
type kiwiTagRow struct {
	Name string `json:"name"`
	Case int    `json:"case"`
}

// kiwiComponentRow mirrors one row of Component.filter({"cases":id}) /
// Component.filter({"cases__in":[...]}) — LIVE-VERIFIED shape
// (p4_verify-report.md, shape-component.json): {"id":1,"name":"Login",
// "product":1,"initial_owner":2,"description":"...","cases":1}. Note the
// field is `cases`, not `case` — a verified asymmetry between the Tag and
// Component RPCs' case-reference field names, kept exactly as observed
// rather than normalized.
type kiwiComponentRow struct {
	Name  string `json:"name"`
	Cases int    `json:"cases"`
}

// tagNamesByCase groups a batched Tag.filter({"case__in":ids}) result by
// case id (adapter.go's fetchTagsForCases). A case with no tags simply has
// no key in the returned map (nil slice on lookup), which is the same
// "absent = no tags" shape a single-case fetch would degrade to.
func tagNamesByCase(rows []kiwiTagRow) map[int][]string {
	out := map[int][]string{}
	for _, r := range rows {
		out[r.Case] = append(out[r.Case], r.Name)
	}
	return out
}

// componentNamesByCase is tagNamesByCase's Component.filter counterpart,
// grouping by the row's `cases` field (adapter.go's
// fetchComponentsForCases).
func componentNamesByCase(rows []kiwiComponentRow) map[int][]string {
	out := map[int][]string{}
	for _, r := range rows {
		out[r.Cases] = append(out[r.Cases], r.Name)
	}
	return out
}
