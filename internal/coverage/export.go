package coverage

import (
	"fmt"
	"strconv"
	"strings"

	"xray-test-manager/internal/testrepo"
)

// ExportReport renders a canonical requirement's coverage as a styled workbook:
// a Summary sheet (overall + per-group %), one sheet per group (every value with
// its tested/run-status and mapped tests), and a Gaps sheet (the required,
// untested values). PMs paste these into Jira/Confluence — the sanctioned
// admin-free sharing path.
func (m *Module) ExportReport(profileID, canonicalID string) ([]byte, error) {
	var name string
	if err := m.db.QueryRow(
		`SELECT name FROM canonical_requirement WHERE profile_id = ? AND id = ?`,
		profileID, canonicalID).Scan(&name); err != nil {
		return nil, fmt.Errorf("load canonical: %w", err)
	}

	model, err := m.GetParamModel(profileID, canonicalID)
	if err != nil {
		return nil, err
	}
	report, err := m.ComputeCoverage(profileID, canonicalID)
	if err != nil {
		return nil, err
	}
	gaps, err := m.ListGaps(profileID, canonicalID)
	if err != nil {
		return nil, err
	}

	sheets := []testrepo.NamedRows{summarySheet(name, report)}
	for _, g := range model.Groups {
		sheets = append(sheets, groupSheet(g, report))
	}
	sheets = append(sheets, gapsSheet(gaps))
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
