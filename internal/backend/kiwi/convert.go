package kiwi

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"xray-test-manager/internal/backend"
)

// kiwiNames is a lenient decoder for a Kiwi m2m field that may serialize
// EITHER as a bare string array (`["smoke","regression"]`) OR as an array
// of objects carrying a name/value (`[{"id":1,"name":"smoke"}]`). modernrpc
// serializes m2m relations inconsistently across Kiwi versions/plugins, and
// no spec §9 fixture pins the tag/component element shape (spec §3.2 only
// says "m2m names"), so hard-typing these as []string risks a whole-response
// json.Unmarshal failure against a real instance — which would break the
// entire test pull at once, the opposite of the brief's "inferred fields
// degrade safely / best-effort" rule. This type accepts both shapes and
// falls back to nil (not an error) on any genuinely unexpected shape, so one
// odd field can never crash the whole TestCase decode.
type kiwiNames []string

func (n *kiwiNames) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*n = nil
		return nil
	}

	// Shape 1: a bare string array.
	var strs []string
	if err := json.Unmarshal(b, &strs); err == nil {
		*n = strs
		return nil
	}

	// Shape 2: an array of objects with a name or value field.
	var objs []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(b, &objs); err == nil {
		out := make([]string, 0, len(objs))
		for _, o := range objs {
			switch {
			case o.Name != "":
				out = append(out, o.Name)
			case o.Value != "":
				out = append(out, o.Value)
			}
		}
		*n = out
		return nil
	}

	// Genuinely unexpected shape (e.g. a bare scalar, or objects with
	// neither name nor value): degrade to nil rather than failing the
	// entire enclosing TestCase.filter response.
	*n = nil
	return nil
}

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
// maps into backend.Test / backend.TestBasic / backend.TestMeta (spec
// §3.2, fixture §9.1b). FK fields are the resolved display forms Kiwi
// annotates alongside the raw FK id (`case_status__name`,
// `priority__value`, matching the fixture's own naming convention).
// ProductName (`category__product__name`) reuses the exact dunder lookup
// path spec §2 gives for scoping TestCase.filter by product — no fixture
// shows the field on a TestCase row, so this is an inference from that cited
// lookup path, not an invented shape; see convert_test/report for the note.
// Tag/Component are read as plain name-string arrays per the spec table's
// "(m2m names)" annotation (§3.2).
type kiwiTestCase struct {
	ID             int       `json:"id"`
	Summary        string    `json:"summary"`
	Text           string    `json:"text"`
	CaseStatusName string    `json:"case_status__name"`
	PriorityValue  string    `json:"priority__value"`
	IsAutomated    bool      `json:"is_automated"`
	AuthorUsername string    `json:"author__username"`
	CreateDate     string    `json:"create_date"`
	Tag            kiwiNames `json:"tag"`
	Component      kiwiNames `json:"component"`
	ProductName    string    `json:"category__product__name"`
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

// toTest maps a kiwiTestCase to the neutral backend.Test per spec §3.2.
// ExecType derives from is_automated (Automated/Manual); FolderID is always
// empty (no folder concept in Kiwi core, spec §3.10); FixVersions is always
// nil (fix versions live on the Run, not the TestCase, spec §3.2); Updated
// is left empty because Kiwi's TestCase has no native `updated` field and
// the only alternative (TestCase.history) has no documented RPC response
// shape in spec §9 — populating it would mean inventing an API shape, which
// the brief says to avoid (see report "spec gaps").
func toTest(tc kiwiTestCase) backend.Test {
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
		Labels:      []string(tc.Tag),
		Components:  []string(tc.Component),
		Updated:     "",
		FolderID:    "",
		ExecType:    execType,
		FixVersions: nil,
	}
}

// toTestBasic maps a kiwiTestCase to the neutral backend.TestBasic per spec
// §3.1. IssueLinks is always nil: Kiwi core has no cross-issue link concept
// on a TestCase.
func toTestBasic(tc kiwiTestCase) backend.TestBasic {
	return backend.TestBasic{
		Key:        strconv.Itoa(tc.ID),
		Summary:    tc.Summary,
		Status:     tc.CaseStatusName,
		ProjectKey: tc.ProductName,
		IssueLinks: nil,
	}
}

// toTestMeta maps a kiwiTestCase to the neutral backend.TestMeta per spec
// §3.1 ("GetTestMeta ... Updated best-effort"). Updated/UpdatedBy are left
// empty: Kiwi exposes no native updated timestamp on TestCase, and the
// documented alternative (TestCase.history) has no response shape given in
// spec §9 to map against, so we use only what's actually documented
// (create_date, author__username) rather than invent one.
func toTestMeta(tc kiwiTestCase) backend.TestMeta {
	return backend.TestMeta{
		Created:   tc.CreateDate,
		Creator:   tc.AuthorUsername,
		Updated:   "",
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
