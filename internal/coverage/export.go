package coverage

import (
	"fmt"
	"strconv"
	"strings"

	"xray-test-manager/internal/testrepo"
)

// ExportReport renders a version's coverage as a styled workbook:
// a Summary sheet (overall + per-group %), one sheet per group (every value with
// its tested/run-status and mapped tests), and a Gaps sheet (the required,
// untested values). PMs paste these into Jira/Confluence — the sanctioned
// admin-free sharing path.
func (m *Module) ExportReport(profileID, versionID string) ([]byte, error) {
	var name string
	if err := m.db.QueryRow(
		`SELECT name FROM canonical_version WHERE profile_id = ? AND id = ?`,
		profileID, versionID).Scan(&name); err != nil {
		return nil, fmt.Errorf("load version: %w", err)
	}

	model, err := m.GetParamModel(profileID, versionID)
	if err != nil {
		return nil, err
	}
	report, err := m.ComputeCoverage(profileID, versionID)
	if err != nil {
		return nil, err
	}
	gaps, err := m.ListGaps(profileID, versionID)
	if err != nil {
		return nil, err
	}

	sheets := []testrepo.NamedRows{summarySheet(name, report)}
	for _, g := range model.Groups {
		sheets = append(sheets, groupSheet(g, report))
	}
	sheets = append(sheets, gapsSheet(gaps))

	// Profile-wide sheets (independent of versionID — intentional).
	pc, _ := m.ProjectCoverage(profileID)
	projs, _ := m.ListProjects(profileID)
	funcs, _ := m.ListCanonical(profileID)
	reuseByFunc := make(map[string][]ReuseRow, len(funcs))
	for _, fn := range funcs {
		reuse, _ := m.ListReuse(profileID, fn.ID)
		reuseByFunc[fn.ID] = reuse
	}
	sheets = append(sheets, byProjectSheet(pc))
	sheets = append(sheets, reuseMapSheet(funcs, reuseByFunc, projs))

	return testrepo.WriteXLSXSheets(sheets)
}

func summarySheet(name string, r CoverageReport) testrepo.NamedRows {
	rows := [][]string{
		{"Functional requirement", name},
		{"Overall coverage", pctStr(r.Percent)},
		{"Tested / total (required values)", fmt.Sprintf("%d / %d", r.TestedValues, r.TotalValues)},
		{"", ""},
		{"Group", "Tested", "Total", "Coverage"},
	}
	for _, g := range r.Groups {
		rows = append(rows, []string{g.Name, strconv.Itoa(g.Tested), strconv.Itoa(g.Total), pctStr(g.Percent)})
	}
	return testrepo.NamedRows{Name: "Summary", Header: []string{"Coverage Summary", ""}, Rows: rows}
}

func groupSheet(g ParamGroup, r CoverageReport) testrepo.NamedRows {
	rows := [][]string{}
	for _, p := range g.Parameters {
		for _, v := range p.Values {
			vc := r.Values[v.ID]
			rows = append(rows, []string{
				p.Name,
				v.ValueLabel,
				v.ValueKind,
				requiredStr(v.IsRequired),
				statusLabel(vc),
				strings.Join(vc.TestKeys, ", "),
			})
		}
	}
	return testrepo.NamedRows{
		Name:   sheetName(g.Name),
		Header: []string{"Parameter", "Value", "Kind", "Required", "Status", "Mapped Tests"},
		Rows:   rows,
	}
}

func gapsSheet(gaps []Gap) testrepo.NamedRows {
	rows := make([][]string, 0, len(gaps))
	for _, g := range gaps {
		rows = append(rows, []string{g.GroupName, g.ParamName, g.ValueLabel, g.ValueKind, g.ErrorCode})
	}
	return testrepo.NamedRows{
		Name:   "Gaps",
		Header: []string{"Group", "Parameter", "Value", "Kind", "Error Code"},
		Rows:   rows,
	}
}

func pctStr(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) + "%" }

func requiredStr(req bool) string {
	if req {
		return "yes"
	}
	return "no"
}

// statusLabel turns a value's coverage into a human cell: untested values read
// "GAP", tested ones carry their run status.
func statusLabel(vc ValueCoverage) string {
	if !vc.Tested {
		if vc.IsRequired {
			return "GAP"
		}
		return "—"
	}
	return vc.RunStatus
}

// sheetName trims a group name to Excel's 31-char sheet-name limit.
func sheetName(name string) string {
	if len(name) > 31 {
		return name[:31]
	}
	return name
}

// byProjectSheet produces the "By project" profile-wide sheet for ExportReport.
// One row per project, coverage % formatted to one decimal place.
func byProjectSheet(rows []ProjectCoverageRow) testrepo.NamedRows {
	dataRows := make([][]string, 0, len(rows))
	for _, r := range rows {
		label := r.Label
		if label == "" {
			label = r.ProjectKey
		}
		dataRows = append(dataRows, []string{
			label,
			r.Role,
			strconv.Itoa(r.RequirementCount),
			strconv.Itoa(r.FunctionsReused),
			strconv.Itoa(r.CoveredValues),
			strconv.Itoa(r.TotalValues),
			fmt.Sprintf("%.1f", r.Percent),
		})
	}
	return testrepo.NamedRows{
		Name:   "By project",
		Header: []string{"Project", "Role", "Requirements", "Functions reused", "Covered", "Total", "Coverage %"},
		Rows:   dataRows,
	}
}

// reuseMapSheet produces the "Reuse map" profile-wide sheet for ExportReport.
// Rows are canonical functions; columns are in-scope projects. Each cell holds
// the project's member requirement key(s) for that function, joined by ", ".
func reuseMapSheet(funcs []CanonicalRequirement, reuseByFunc map[string][]ReuseRow, projects []ProjectConfig) testrepo.NamedRows {
	header := make([]string, 0, len(projects)+1)
	header = append(header, "Function")
	for _, p := range projects {
		lbl := p.Label
		if lbl == "" {
			lbl = p.ProjectKey
		}
		header = append(header, lbl)
	}

	dataRows := make([][]string, 0, len(funcs))
	for _, fn := range funcs {
		row := make([]string, 0, len(projects)+1)
		row = append(row, fn.Name)

		// Group requirement keys by project key for this canonical function.
		keysByProj := map[string][]string{}
		for _, rr := range reuseByFunc[fn.ID] {
			if rr.ProjectKey != "" && rr.RequirementKey != "" {
				keysByProj[rr.ProjectKey] = append(keysByProj[rr.ProjectKey], rr.RequirementKey)
			}
		}

		for _, p := range projects {
			row = append(row, strings.Join(keysByProj[p.ProjectKey], ", "))
		}
		dataRows = append(dataRows, row)
	}

	return testrepo.NamedRows{
		Name:   "Reuse map",
		Header: header,
		Rows:   dataRows,
	}
}
