package testrepo

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/xuri/excelize/v2"
)

// This file holds the flat-row and flow-row producers that back the
// Traceability XLSX export (RND_P_4TFINT_05-221). Each kind exposes a Flow-rows
// builder (the Sankey edge list with resolved node labels) and a Table-rows
// builder (one flat row per traceability thread), honouring the same filters the
// matching Sankey producer takes.

// flowRowsFromSankey turns a Sankey's links into a resolved edge list: each
// row is {Source label, Target label, Value}, mapping endpoint node ids to their
// node labels so the export is readable without the id scheme.
func flowRowsFromSankey(sk Sankey) [][]string {
	labelByID := make(map[string]string, len(sk.Nodes))
	for _, n := range sk.Nodes {
		labelByID[n.ID] = n.Label
	}
	resolve := func(id string) string {
		if lbl, ok := labelByID[id]; ok {
			return lbl
		}
		return id
	}
	rows := make([][]string, 0, len(sk.Links))
	for _, l := range sk.Links {
		rows = append(rows, []string{resolve(l.Source), resolve(l.Target), strconv.Itoa(l.Value)})
	}
	return rows
}

// ExecutionFlowRows returns the execution Sankey's edge list with resolved
// labels, honouring the same plan/exec/cross-project filters as
// GetTraceabilitySankey.
func (r *Repository) ExecutionFlowRows(profileID, projectKey string, planFilters, execFilters []string, crossProjectOnly bool) ([][]string, error) {
	sk, err := r.GetTraceabilitySankey(profileID, projectKey, planFilters, execFilters, crossProjectOnly)
	if err != nil {
		return nil, err
	}
	return flowRowsFromSankey(sk), nil
}

// ExecutionTableRows returns one flat row per execution thread (a Test's run in
// a Test Execution): Test Plan, Test Execution, Test, Run status. It reuses the
// exact WHERE/filter handling of GetTraceabilitySankey so the table matches the
// diagram. The Test Plan column carries the same single-bucket attribution the
// Sankey uses ("No plan" / "Multiple plans" / the plan label).
func (r *Repository) ExecutionTableRows(profileID, projectKey string, planFilters, execFilters []string, crossProjectOnly bool) ([][]string, error) {
	plansByTest, err := r.testPlanMemberships(profileID)
	if err != nil {
		return nil, err
	}
	summaryByKey, err := r.containerSummaries(profileID)
	if err != nil {
		return nil, err
	}

	q := `SELECT l.container_key, l.test_key, l.run_status
		 FROM test_container_test l
		 JOIN test_container c
		   ON c.profile_id = l.profile_id AND c.jira_key = l.container_key
		 WHERE l.profile_id = ? AND c.kind = 'testexec'`
	args := []any{profileID}
	if execs := nonEmptyKeys(execFilters); len(execs) > 0 {
		q += " AND l.container_key IN (" + sqlPlaceholders(len(execs)) + ")"
		for _, e := range execs {
			args = append(args, e)
		}
	}
	if plans := nonEmptyKeys(planFilters); len(plans) > 0 {
		q += " AND l.test_key IN (SELECT test_key FROM test_container_test" +
			" WHERE profile_id = ? AND container_key IN (" + sqlPlaceholders(len(plans)) + "))"
		args = append(args, profileID)
		for _, p := range plans {
			args = append(args, p)
		}
	}
	q += " ORDER BY l.container_key, l.test_key"

	rows, err := r.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("read execution threads: %w", err)
	}
	defer rows.Close()

	out := [][]string{}
	for rows.Next() {
		var execKey, testKey, runStatus string
		if err := rows.Scan(&execKey, &testKey, &runStatus); err != nil {
			return nil, err
		}
		// Cross-project filter: same predicate as the Sankey producer.
		if crossProjectOnly && projectKey != "" && projectKeyOf(execKey) == projectKey {
			continue
		}
		_, planLabel := planBucket(plansByTest[testKey], summaryByKey)
		out = append(out, []string{
			planLabel,
			orKey(summaryByKey[execKey], execKey),
			testKey,
			runStatus,
		})
	}
	return out, rows.Err()
}

// RequirementFlowRows returns the requirement Sankey's edge list with resolved
// labels, honouring the same requirement filter as GetRequirementTraceability.
func (r *Repository) RequirementFlowRows(profileID string, reqFilters []string) ([][]string, error) {
	sk, err := r.GetRequirementTraceability(profileID, reqFilters)
	if err != nil {
		return nil, err
	}
	return flowRowsFromSankey(sk), nil
}

// RequirementTableRows returns one flat row per requirement coverage thread:
// Requirement, Coverage, Test plan, Test, Run result. It reuses
// ListRequirementsWithCoverage and ListTestsForRequirement so the table tracks
// the same data as the requirement Sankey. Uncovered requirements still produce
// one synthetic row (No plan / No test), matching the diagram.
func (r *Repository) RequirementTableRows(profileID string, reqFilters []string) ([][]string, error) {
	reqs, err := r.ListRequirementsWithCoverage(profileID)
	if err != nil {
		return nil, err
	}
	plansByTest, err := r.testPlanMemberships(profileID)
	if err != nil {
		return nil, err
	}
	summaryByKey, err := r.containerSummaries(profileID)
	if err != nil {
		return nil, err
	}

	filterSet := map[string]bool{}
	for _, k := range nonEmptyKeys(reqFilters) {
		filterSet[k] = true
	}

	out := [][]string{}
	for _, rq := range reqs {
		if len(filterSet) > 0 && !filterSet[rq.Key] {
			continue
		}
		reqLabel := rq.Key
		if rq.Summary != "" {
			reqLabel = rq.Key + " — " + rq.Summary
		}
		covLabel := requirementCoverageLabel(rq.Coverage)
		tests, err := r.ListTestsForRequirement(profileID, rq.Key)
		if err != nil {
			return nil, err
		}
		if len(tests) == 0 {
			out = append(out, []string{reqLabel, covLabel, "No plan", "No test", "Not run"})
			continue
		}
		for _, tk := range tests {
			_, planLabel := planBucket(plansByTest[tk.Key], summaryByKey)
			_, resLabel := runResultNode(tk.RunStatus)
			out = append(out, []string{reqLabel, covLabel, planLabel, tk.Key, resLabel})
		}
	}
	return out, nil
}

// SubTaskFlowRows returns the sub-task Sankey's edge list with resolved labels,
// honouring the same parent filter and crossProject behavior as
// GetSubTaskTraceability.
func (r *Repository) SubTaskFlowRows(profileID string, parentFilters []string, crossProject bool) ([][]string, error) {
	sk, err := r.GetSubTaskTraceability(profileID, parentFilters, crossProject)
	if err != nil {
		return nil, err
	}
	return flowRowsFromSankey(sk), nil
}

// SubTaskTableRows returns one flat row per sub-task execution thread: Parent,
// Test Execution, Test, Run status. It reuses the exact WHERE/filter handling of
// GetSubTaskTraceability, including crossProject: when false, members that live
// only in external_test (no test_case row) are excluded.
func (r *Repository) SubTaskTableRows(profileID string, parentFilters []string, crossProject bool) ([][]string, error) {
	summaryByKey, err := r.containerSummaries(profileID)
	if err != nil {
		return nil, err
	}

	q := `SELECT c.parent_key, l.container_key, l.test_key, l.run_status
		 FROM test_container_test l
		 JOIN test_container c
		   ON c.profile_id = l.profile_id AND c.jira_key = l.container_key
		 WHERE l.profile_id = ? AND c.kind = 'testexec' AND c.parent_key != ''`
	args := []any{profileID}
	if !crossProject {
		q += ` AND EXISTS (SELECT 1 FROM test_case t
			 WHERE t.profile_id = l.profile_id AND t.jira_key = l.test_key)`
	}
	if parents := nonEmptyKeys(parentFilters); len(parents) > 0 {
		q += " AND c.parent_key IN (" + sqlPlaceholders(len(parents)) + ")"
		for _, p := range parents {
			args = append(args, p)
		}
	}
	q += " ORDER BY c.parent_key, l.container_key, l.test_key"

	rows, err := r.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("read sub-task threads: %w", err)
	}
	defer rows.Close()

	out := [][]string{}
	for rows.Next() {
		var parentKey, execKey, testKey, runStatus string
		if err := rows.Scan(&parentKey, &execKey, &testKey, &runStatus); err != nil {
			return nil, err
		}
		out = append(out, []string{
			parentKey,
			orKey(summaryByKey[execKey], execKey),
			testKey,
			runStatus,
		})
	}
	return out, rows.Err()
}

// traceabilityKind names the active Traceability tab being exported.
type traceabilityKind string

const (
	kindRequirement traceabilityKind = "requirement"
	kindExecution   traceabilityKind = "execution"
	kindSubTask     traceabilityKind = "subtask"
)

// outlineTreeFills is the per-level fill palette used by writeOutlineTreeSheet.
// Levels are indexed from 0 (the root node column); if more levels than colors
// exist, the palette is clamped to the last entry.
var outlineTreeFills = []string{
	"8EAADB", // level 0: medium blue
	"D9E1F2", // level 1: light blue
	"EDF1FA", // level 2: very light blue
	"F2F2F2", // level 3+: near-white
}

// writeOutlineTreeSheet renders a sorted flat table (header = node column
// labels, rows = [][]string where each entry is a full path from root to leaf)
// into the named sheet as a staircase collapsible outline tree. Shared leading
// prefixes are de-duplicated: when two consecutive rows share the same value in
// column i, only the first row emits a cell in column i; subsequent rows skip
// it, which collapses the ancestor node into a single parent row. Each row is
// placed in the spreadsheet with the cell in column i (0-based) and all other
// columns blank. Outline level i is set via SetRowOutlineLevel when i >= 1;
// level 0 rows need no call (the default). OutlineSummaryBelow is set false so
// collapse controls appear on the parent (above its children).
//
// Returns maxLevel, the highest outline level emitted (so the caller can inject
// sheetFormatPr/@outlineLevelRow via setSheetOutlineLevelRow).
func writeOutlineTreeSheet(f *excelize.File, sheet string, header []string, rows [][]string) (maxLevel int, err error) {
	below := false
	if err := f.SetSheetProps(sheet, &excelize.SheetPropsOptions{OutlineSummaryBelow: &below}); err != nil {
		return 0, fmt.Errorf("set sheet props: %w", err)
	}

	n := len(header)
	if n == 0 {
		return 0, nil
	}

	// Build one fill style per level.
	const borderColor = "BFBFBF"
	borders := []excelize.Border{
		{Type: "left", Color: borderColor, Style: 1},
		{Type: "top", Color: borderColor, Style: 1},
		{Type: "right", Color: borderColor, Style: 1},
		{Type: "bottom", Color: borderColor, Style: 1},
	}
	wrapTop := &excelize.Alignment{Vertical: "top", WrapText: true}

	levelStyles := make([]int, n)
	for i := 0; i < n; i++ {
		fill := outlineTreeFills[i]
		if i >= len(outlineTreeFills) {
			fill = outlineTreeFills[len(outlineTreeFills)-1]
		}
		sid, serr := f.NewStyle(&excelize.Style{
			Fill:      excelize.Fill{Type: "pattern", Color: []string{fill}, Pattern: 1},
			Alignment: wrapTop,
			Border:    borders,
		})
		if serr != nil {
			return 0, fmt.Errorf("new level %d style: %w", i, serr)
		}
		levelStyles[i] = sid
	}

	headerStyle, herr := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"305496"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
		Border:    borders,
	})
	if herr != nil {
		return 0, fmt.Errorf("new header style: %w", herr)
	}

	// Set sensible column widths before writing cells.
	colWidths := make([]float64, n)
	for i, h := range header {
		w := float64(len([]rune(h))) + 4.0
		if w < 14 {
			w = 14
		}
		if w > 50 {
			w = 50
		}
		colWidths[i] = w
	}
	for _, row := range rows {
		for i, cell := range row {
			if i >= n {
				break
			}
			w := float64(len([]rune(cell))) + 4.0
			if w > 50 {
				w = 50
			}
			if w > colWidths[i] {
				colWidths[i] = w
			}
		}
	}
	for i, w := range colWidths {
		col, cerr := excelize.ColumnNumberToName(i + 1)
		if cerr != nil {
			return 0, fmt.Errorf("column name: %w", cerr)
		}
		if serr := f.SetColWidth(sheet, col, col, w); serr != nil {
			return 0, fmt.Errorf("set col width: %w", serr)
		}
	}

	// Header row (row 1).
	lastCol, cerr := excelize.ColumnNumberToName(n)
	if cerr != nil {
		return 0, fmt.Errorf("last column name: %w", cerr)
	}
	for i, h := range header {
		col, cerr := excelize.ColumnNumberToName(i + 1)
		if cerr != nil {
			return 0, fmt.Errorf("column name: %w", cerr)
		}
		if serr := f.SetCellStr(sheet, col+"1", h); serr != nil {
			return 0, fmt.Errorf("set header cell: %w", serr)
		}
	}
	if serr := f.SetCellStyle(sheet, "A1", lastCol+"1", headerStyle); serr != nil {
		return 0, fmt.Errorf("set header style: %w", serr)
	}

	// Freeze the header row.
	if serr := f.SetPanes(sheet, &excelize.Panes{
		Freeze:      true,
		YSplit:      1,
		TopLeftCell: "A2",
		ActivePane:  "bottomLeft",
	}); serr != nil {
		return 0, fmt.Errorf("freeze panes: %w", serr)
	}

	// Data rows: de-duplicate shared prefixes.
	// prev tracks the full path of the last emitted row.
	prev := make([]string, 0, n)
	rowNum := 2
	emittedMax := 0

	for _, cur := range rows {
		if len(cur) == 0 {
			continue
		}
		// Clamp to n columns.
		if len(cur) > n {
			cur = cur[:n]
		}

		// Find the first column where this row differs from the previous row.
		start := 0
		for start < len(prev) && start < len(cur) && cur[start] == prev[start] {
			start++
		}

		// Emit one spreadsheet row per node level from start onwards.
		for i := start; i < len(cur); i++ {
			// Each node sits in its own column (i); other columns are blank.
			col, cerr := excelize.ColumnNumberToName(i + 1)
			if cerr != nil {
				return 0, fmt.Errorf("column name: %w", cerr)
			}
			if serr := f.SetCellStr(sheet, fmt.Sprintf("%s%d", col, rowNum), cur[i]); serr != nil {
				return 0, fmt.Errorf("set cell: %w", serr)
			}

			// Outline level: 0 for root (no call needed), >= 1 for nested.
			if i >= 1 {
				if serr := f.SetRowOutlineLevel(sheet, rowNum, uint8(i)); serr != nil {
					return 0, fmt.Errorf("set outline level: %w", serr)
				}
				if i > emittedMax {
					emittedMax = i
				}
			}

			// Style the row with the per-level fill across all node columns.
			styleIdx := i
			if styleIdx >= len(levelStyles) {
				styleIdx = len(levelStyles) - 1
			}
			if serr := f.SetCellStyle(sheet, fmt.Sprintf("A%d", rowNum), fmt.Sprintf("%s%d", lastCol, rowNum), levelStyles[styleIdx]); serr != nil {
				return 0, fmt.Errorf("set row style: %w", serr)
			}

			rowNum++
		}

		prev = cur
	}

	return emittedMax, nil
}

// traceabilityTableSheetXML is the worksheet part name for the Table sheet in
// the traceability export. Table is created first (by renaming Sheet1), so
// excelize assigns it the first part slot.
const traceabilityTableSheetXML = "xl/worksheets/sheet1.xml"

// ExportTraceabilitySheets builds the Flow + Table sheets for the active
// traceability tab and renders them to a single XLSX workbook's bytes. kind is
// "requirement", "execution", or "subtask"; the matching filter slices are used
// (the others are ignored). crossProject is threaded to the execution and
// subtask producers, controlling whether cross-project members are drawn.
//
// The Table sheet is a collapsible outline tree: each node column is a nesting
// level, and rows that share a leading prefix are de-duplicated so they collapse
// under a single parent row. The Flow sheet remains a flat table.
func (r *Repository) ExportTraceabilitySheets(profileID, projectKey, kind string, planFilters, execFilters, reqFilters, parentFilters []string, crossProject bool) ([]byte, error) {
	var flow, table [][]string
	var flowHeader, tableHeader []string
	var err error

	switch traceabilityKind(kind) {
	case kindRequirement:
		flowHeader = []string{"Source", "Target", "Value"}
		tableHeader = []string{"Requirement", "Coverage", "Test plan", "Test", "Run result"}
		if flow, err = r.RequirementFlowRows(profileID, reqFilters); err != nil {
			return nil, err
		}
		if table, err = r.RequirementTableRows(profileID, reqFilters); err != nil {
			return nil, err
		}
	case kindExecution:
		flowHeader = []string{"Source", "Target", "Value"}
		tableHeader = []string{"Test Plan", "Test Execution", "Test", "Run status"}
		if flow, err = r.ExecutionFlowRows(profileID, projectKey, planFilters, execFilters, crossProject); err != nil {
			return nil, err
		}
		if table, err = r.ExecutionTableRows(profileID, projectKey, planFilters, execFilters, crossProject); err != nil {
			return nil, err
		}
	case kindSubTask:
		flowHeader = []string{"Source", "Target", "Value"}
		tableHeader = []string{"Parent", "Test Execution", "Test", "Run status"}
		if flow, err = r.SubTaskFlowRows(profileID, parentFilters, crossProject); err != nil {
			return nil, err
		}
		if table, err = r.SubTaskTableRows(profileID, parentFilters, crossProject); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown traceability kind %q", kind)
	}

	// Build the workbook with excelize directly so the Table sheet can be a
	// collapsible outline tree. Table is created first (by renaming the default
	// Sheet1) so its worksheet part is sheet1.xml, which is the part
	// setSheetOutlineLevelRow targets for the XML injection.
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	// Table sheet: rename Sheet1 -> "Table" so it stays sheet1.xml.
	defaultSheet := f.GetSheetName(0)
	if serr := f.SetSheetName(defaultSheet, "Table"); serr != nil {
		return nil, fmt.Errorf("rename Table sheet: %w", serr)
	}

	maxLevel, terr := writeOutlineTreeSheet(f, "Table", tableHeader, table)
	if terr != nil {
		return nil, fmt.Errorf("write Table sheet: %w", terr)
	}

	// Flow sheet: a plain flat table added after Table so it is sheet2.xml.
	if _, serr := f.NewSheet("Flow"); serr != nil {
		return nil, fmt.Errorf("create Flow sheet: %w", serr)
	}
	st, sterr := newSheetStyles(f)
	if sterr != nil {
		return nil, fmt.Errorf("flow sheet styles: %w", sterr)
	}
	flowRows := make([][]string, 0, len(flow)+1)
	if len(flowHeader) > 0 {
		flowRows = append(flowRows, flowHeader)
	}
	flowRows = append(flowRows, flow...)
	if ferr := fillSheet(f, "Flow", flowRows, st); ferr != nil {
		return nil, fmt.Errorf("fill Flow sheet: %w", ferr)
	}

	// Set Table as the active sheet so it opens first.
	if tableIdx, idxErr := f.GetSheetIndex("Table"); idxErr == nil {
		f.SetActiveSheet(tableIdx)
	}

	buf, werr := f.WriteToBuffer()
	if werr != nil {
		return nil, fmt.Errorf("write workbook: %w", werr)
	}

	data := buf.Bytes()
	if maxLevel >= 1 {
		data, err = setSheetOutlineLevelRow(data, traceabilityTableSheetXML, maxLevel)
		if err != nil {
			return nil, fmt.Errorf("inject outline level: %w", err)
		}
	}
	return data, nil
}

// writeOutlineTreeSheetFromBytes is a thin helper used in tests: it opens a
// workbook from raw bytes with excelize and returns the file handle. The caller
// is responsible for calling f.Close().
func writeOutlineTreeSheetFromBytes(data []byte) (*excelize.File, error) {
	return excelize.OpenReader(bytes.NewReader(data))
}
