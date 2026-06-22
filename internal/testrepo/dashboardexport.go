package testrepo

import "strconv"

// This file holds the XLSX export for the statistics Dashboard (RND_P_4TFINT_05).
// It mirrors the Traceability export: build a []namedRows from the same data the
// dashboard renders and hand them to writeXLSXSheets. The export honours the
// active folder/component/status filters so the workbook records the scope it
// was taken under.

// bucketRows turns a distribution into {Label, Count} string rows.
func bucketRows(buckets []Bucket) [][]string {
	rows := make([][]string, 0, len(buckets))
	for _, b := range buckets {
		rows = append(rows, []string{b.Label, strconv.Itoa(b.Count)})
	}
	return rows
}

// filterValue renders a filter value for the Summary sheet, showing "(all)" when
// the filter is unset.
func filterValue(v string) string {
	if v == "" {
		return "(all)"
	}
	return v
}

// ExportDashboardSheets builds the Summary sheet plus one sheet per breakdown
// bucket for the statistics Dashboard and renders them to a single XLSX
// workbook's bytes (RND_P_4TFINT_05). The folder/component/status filters are
// passed through to GetStatistics so the export matches the on-screen scope, and
// the Summary sheet records which filters were active.
func (r *Repository) ExportDashboardSheets(profileID, folder, component, status string) ([]byte, error) {
	stats, err := r.GetStatistics(profileID, folder, component, status)
	if err != nil {
		return nil, err
	}

	summary := [][]string{
		{"Total tests", strconv.Itoa(stats.Total)},
		{"Pending changes", strconv.Itoa(stats.PendingChanges)},
		{"Executed tests", strconv.Itoa(stats.ExecutedTests)},
		{"Test Sets", strconv.Itoa(stats.TestSets)},
		{"Test Plans", strconv.Itoa(stats.TestPlans)},
		{"Test Executions", strconv.Itoa(stats.TestExecutions)},
		{"Tests in a set", strconv.Itoa(stats.TestsInSet)},
		{"Tests in a plan", strconv.Itoa(stats.TestsInPlan)},
		{"Folder filter", filterValue(folder)},
		{"Component filter", filterValue(component)},
		{"Status filter", filterValue(status)},
	}

	sheets := []namedRows{
		{Name: "Summary", Header: []string{"Metric", "Value"}, Rows: summary},
		{Name: "By status", Header: []string{"Label", "Count"}, Rows: bucketRows(stats.ByStatus)},
		{Name: "By priority", Header: []string{"Label", "Count"}, Rows: bucketRows(stats.ByPriority)},
		{Name: "By folder", Header: []string{"Label", "Count"}, Rows: bucketRows(stats.ByFolder)},
		{Name: "By label", Header: []string{"Label", "Count"}, Rows: bucketRows(stats.ByLabel)},
		{Name: "By component", Header: []string{"Label", "Count"}, Rows: bucketRows(stats.ByComponent)},
		{Name: "Run status", Header: []string{"Label", "Count"}, Rows: bucketRows(stats.ByRunStatus)},
		{Name: "Requirement coverage", Header: []string{"Label", "Count"}, Rows: bucketRows(stats.ByCoverage)},
		{Name: "Updated trend", Header: []string{"Label", "Count"}, Rows: bucketRows(stats.UpdatedTrend)},
	}

	return writeXLSXSheets(sheets)
}
