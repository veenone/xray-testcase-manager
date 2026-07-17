package xray

import (
	"xray-test-manager/internal/backend"
	"xray-test-manager/internal/jira"
)

// This file maps the jira.* shapes returned/accepted by *jira.Client to the
// neutral backend.* DTOs and back. For this phase the field names and types
// are identical, so each helper is a mechanical field-by-field copy. Keeping
// the mapping explicit (rather than a type alias) is deliberate: it is the seam
// that lets a future backend diverge from the jira shapes without touching the
// interface or its callers.

func toUser(u *jira.User) *backend.User {
	if u == nil {
		return nil
	}
	return &backend.User{Name: u.Name, DisplayName: u.DisplayName, Email: u.Email}
}

func toTest(t jira.Test) backend.Test {
	return backend.Test{
		Key:         t.Key,
		ID:          t.ID,
		Summary:     t.Summary,
		Description: t.Description,
		Status:      t.Status,
		Priority:    t.Priority,
		Labels:      t.Labels,
		Components:  t.Components,
		Updated:     t.Updated,
		FolderID:    t.FolderID,
		ExecType:    t.ExecType,
		FixVersions: t.FixVersions,
	}
}

func toTests(in []jira.Test) []backend.Test {
	if in == nil {
		return nil
	}
	out := make([]backend.Test, len(in))
	for i, t := range in {
		out[i] = toTest(t)
	}
	return out
}

func toBugLinkRef(r jira.BugLinkRef) backend.BugLinkRef {
	return backend.BugLinkRef{
		Key:        r.Key,
		IssueType:  r.IssueType,
		LinkID:     r.LinkID,
		ProjectKey: r.ProjectKey,
		Summary:    r.Summary,
		Status:     r.Status,
		Priority:   r.Priority,
	}
}

func toTestBasic(t jira.TestBasic) backend.TestBasic {
	tb := backend.TestBasic{
		Key:        t.Key,
		Summary:    t.Summary,
		Status:     t.Status,
		ProjectKey: t.ProjectKey,
	}
	if t.IssueLinks != nil {
		tb.IssueLinks = make([]backend.BugLinkRef, len(t.IssueLinks))
		for i, l := range t.IssueLinks {
			tb.IssueLinks[i] = toBugLinkRef(l)
		}
	}
	return tb
}

func toTestBasics(in []jira.TestBasic) []backend.TestBasic {
	if in == nil {
		return nil
	}
	out := make([]backend.TestBasic, len(in))
	for i, t := range in {
		out[i] = toTestBasic(t)
	}
	return out
}

func toStep(s jira.Step) backend.Step {
	return backend.Step{
		ID:            s.ID,
		Index:         s.Index,
		Action:        s.Action,
		Data:          s.Data,
		Expected:      s.Expected,
		CalledTestKey: s.CalledTestKey,
	}
}

func toSteps(in []jira.Step) []backend.Step {
	if in == nil {
		return nil
	}
	out := make([]backend.Step, len(in))
	for i, s := range in {
		out[i] = toStep(s)
	}
	return out
}

func toContainer(c jira.Container) backend.Container {
	return backend.Container{
		Key:           c.Key,
		Kind:          c.Kind,
		Summary:       c.Summary,
		Status:        c.Status,
		ParentKey:     c.ParentKey,
		ParentSummary: c.ParentSummary,
		IssueType:     c.IssueType,
		Environments:  c.Environments,
		FixVersions:   c.FixVersions,
		Created:       c.Created,
		Updated:       c.Updated,
		Resolved:      c.Resolved,
		Description:   c.Description,
	}
}

func toContainers(in []jira.Container) []backend.Container {
	if in == nil {
		return nil
	}
	out := make([]backend.Container, len(in))
	for i, c := range in {
		out[i] = toContainer(c)
	}
	return out
}

func toContainerLink(l jira.ContainerLink) backend.ContainerLink {
	return backend.ContainerLink{
		ContainerKey: l.ContainerKey,
		TestKey:      l.TestKey,
		RunStatus:    l.RunStatus,
	}
}

func toContainerLinks(in []jira.ContainerLink) []backend.ContainerLink {
	if in == nil {
		return nil
	}
	out := make([]backend.ContainerLink, len(in))
	for i, l := range in {
		out[i] = toContainerLink(l)
	}
	return out
}

func toPrecondition(p jira.Precondition) backend.Precondition {
	return backend.Precondition{
		Key:         p.Key,
		Summary:     p.Summary,
		Type:        p.Type,
		Description: p.Description,
		Condition:   p.Condition,
	}
}

func toPreconditions(in []jira.Precondition) []backend.Precondition {
	if in == nil {
		return nil
	}
	out := make([]backend.Precondition, len(in))
	for i, p := range in {
		out[i] = toPrecondition(p)
	}
	return out
}

func toTestRun(r jira.TestRun) backend.TestRun {
	return backend.TestRun{
		TestKey:     r.TestKey,
		Status:      r.Status,
		StartedAt:   r.StartedAt,
		FinishedAt:  r.FinishedAt,
		ExecutedBy:  r.ExecutedBy,
		Environment: r.Environment,
		Defects:     r.Defects,
		Comment:     r.Comment,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

func toTestRuns(in []jira.TestRun) []backend.TestRun {
	if in == nil {
		return nil
	}
	out := make([]backend.TestRun, len(in))
	for i, r := range in {
		out[i] = toTestRun(r)
	}
	return out
}

func toTransition(t jira.Transition) backend.Transition {
	return backend.Transition{ID: t.ID, Name: t.Name, To: t.To}
}

func toTransitions(in []jira.Transition) []backend.Transition {
	if in == nil {
		return nil
	}
	out := make([]backend.Transition, len(in))
	for i, t := range in {
		out[i] = toTransition(t)
	}
	return out
}

func toCustomFieldDef(f jira.CustomFieldDef) backend.CustomFieldDef {
	return backend.CustomFieldDef{ID: f.ID, Name: f.Name, Type: f.Type}
}

func toCustomFieldDefs(in []jira.CustomFieldDef) []backend.CustomFieldDef {
	if in == nil {
		return nil
	}
	out := make([]backend.CustomFieldDef, len(in))
	for i, f := range in {
		out[i] = toCustomFieldDef(f)
	}
	return out
}

func toFolder(f jira.Folder) backend.Folder {
	return backend.Folder{
		ID:             f.ID,
		ParentID:       f.ParentID,
		Name:           f.Name,
		XrayID:         f.XrayID,
		TestCount:      f.TestCount,
		TotalTestCount: f.TotalTestCount,
	}
}

func toFolders(in []jira.Folder) []backend.Folder {
	if in == nil {
		return nil
	}
	out := make([]backend.Folder, len(in))
	for i, f := range in {
		out[i] = toFolder(f)
	}
	return out
}

func toFolderRef(r jira.FolderRef) backend.FolderRef {
	return backend.FolderRef{ID: r.ID, Path: r.Path}
}

func toFolderTreeResult(r jira.FolderTreeResult) backend.FolderTreeResult {
	out := backend.FolderTreeResult{
		Folders:        toFolders(r.Folders),
		TreeMembership: r.TreeMembership,
	}
	if r.FoldersWithTests != nil {
		out.FoldersWithTests = make([]backend.FolderRef, len(r.FoldersWithTests))
		for i, fr := range r.FoldersWithTests {
			out.FoldersWithTests[i] = toFolderRef(fr)
		}
	}
	return out
}

func toRequirement(r jira.Requirement) backend.Requirement {
	return backend.Requirement{
		Key:         r.Key,
		ProjectKey:  r.ProjectKey,
		IssueType:   r.IssueType,
		Summary:     r.Summary,
		Status:      r.Status,
		Updated:     r.Updated,
		Priority:    r.Priority,
		Components:  r.Components,
		FixVersions: r.FixVersions,
		Sprint:      r.Sprint,
		Description: r.Description,
		EpicKey:     r.EpicKey,
	}
}

func toRequirements(in []jira.Requirement) []backend.Requirement {
	if in == nil {
		return nil
	}
	out := make([]backend.Requirement, len(in))
	for i, r := range in {
		out[i] = toRequirement(r)
	}
	return out
}

func toRequirementLink(l jira.RequirementLink) backend.RequirementLink {
	return backend.RequirementLink{
		TestKey:        l.TestKey,
		RequirementKey: l.RequirementKey,
		LinkID:         l.LinkID,
	}
}

func toRequirementLinks(in []jira.RequirementLink) []backend.RequirementLink {
	if in == nil {
		return nil
	}
	out := make([]backend.RequirementLink, len(in))
	for i, l := range in {
		out[i] = toRequirementLink(l)
	}
	return out
}

func fromRequirementSourceSpec(s backend.RequirementSourceSpec) jira.RequirementSourceSpec {
	return jira.RequirementSourceSpec{
		ProjectKey: s.ProjectKey,
		IssueTypes: s.IssueTypes,
		ScopeJQL:   s.ScopeJQL,
	}
}

func fromRequirementSourceSpecs(in []backend.RequirementSourceSpec) []jira.RequirementSourceSpec {
	if in == nil {
		return nil
	}
	out := make([]jira.RequirementSourceSpec, len(in))
	for i, s := range in {
		out[i] = fromRequirementSourceSpec(s)
	}
	return out
}

func toReqToReqLink(l jira.ReqToReqLink) backend.ReqToReqLink {
	return backend.ReqToReqLink{
		FromKey:  l.FromKey,
		ToKey:    l.ToKey,
		LinkType: l.LinkType,
		LinkID:   l.LinkID,
	}
}

func toReqToReqLinks(in []jira.ReqToReqLink) []backend.ReqToReqLink {
	if in == nil {
		return nil
	}
	out := make([]backend.ReqToReqLink, len(in))
	for i, l := range in {
		out[i] = toReqToReqLink(l)
	}
	return out
}

func toBug(b jira.Bug) backend.Bug {
	return backend.Bug{
		Key:        b.Key,
		ProjectKey: b.ProjectKey,
		IssueType:  b.IssueType,
		Summary:    b.Summary,
		Status:     b.Status,
		Priority:   b.Priority,
		Updated:    b.Updated,
	}
}

func toBugs(in []jira.Bug) []backend.Bug {
	if in == nil {
		return nil
	}
	out := make([]backend.Bug, len(in))
	for i, b := range in {
		out[i] = toBug(b)
	}
	return out
}

func toBugLink(l jira.BugLink) backend.BugLink {
	return backend.BugLink{TestKey: l.TestKey, BugKey: l.BugKey, LinkID: l.LinkID}
}

func toBugLinks(in []jira.BugLink) []backend.BugLink {
	if in == nil {
		return nil
	}
	out := make([]backend.BugLink, len(in))
	for i, l := range in {
		out[i] = toBugLink(l)
	}
	return out
}

func toBugFieldOption(o jira.BugFieldOption) backend.BugFieldOption {
	return backend.BugFieldOption{ID: o.ID, Value: o.Value}
}

func toBugCreateField(f jira.BugCreateField) backend.BugCreateField {
	bf := backend.BugCreateField{
		ID:       f.ID,
		Name:     f.Name,
		Required: f.Required,
		Type:     f.Type,
	}
	if f.AllowedValues != nil {
		bf.AllowedValues = make([]backend.BugFieldOption, len(f.AllowedValues))
		for i, o := range f.AllowedValues {
			bf.AllowedValues[i] = toBugFieldOption(o)
		}
	}
	return bf
}

func toBugCreateFields(in []jira.BugCreateField) []backend.BugCreateField {
	if in == nil {
		return nil
	}
	out := make([]backend.BugCreateField, len(in))
	for i, f := range in {
		out[i] = toBugCreateField(f)
	}
	return out
}

func toBugDetail(d jira.BugDetail) backend.BugDetail {
	return backend.BugDetail{
		Description:       d.Description,
		DefectOrigin:      d.DefectOrigin,
		DefectAnalysis:    d.DefectAnalysis,
		CorrectionDetails: d.CorrectionDetails,
		Reporter:          d.Reporter,
		Severity:          d.Severity,
	}
}

func toTestMeta(m jira.TestMeta) backend.TestMeta {
	return backend.TestMeta{
		Created:   m.Created,
		Creator:   m.Creator,
		Updated:   m.Updated,
		UpdatedBy: m.UpdatedBy,
	}
}
