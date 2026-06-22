package testrepo

import (
	"fmt"
	"strings"
)

// GapTest is one comparable test row, carrying the import fields so an added
// gap becomes a complete local test. Steps are intentionally omitted — gap
// analysis is by summary; created gaps are summary + metadata, fleshed out later.
type GapTest struct {
	Summary     string   `json:"summary"`
	Description string   `json:"description"`
	Priority    string   `json:"priority"`
	Labels      []string `json:"labels"`
	Components  []string `json:"components"`
	Folder      string   `json:"folder"`
}

// FolderMismatch is a test matched by summary on both sides whose folder
// location differs — surfaced when folder comparison is enabled.
type FolderMismatch struct {
	Summary         string `json:"summary"`
	ReferenceFolder string `json:"referenceFolder"`
	TargetFolder    string `json:"targetFolder"`
}

// GapResult is the outcome of a comparison. MissingFromReference is in the
// target but not the reference (addable as tests); MissingFromTarget is in the
// reference but not the target (report-only). Both gap lists are deduplicated
// by normalized summary. The ThreeWay fields are populated only when a project
// list is supplied (reference-from-file three-way): MissingFromProject is every
// summary in (reference ∪ target) not yet in the project, so the project can be
// completed. FolderMismatches is populated when folder comparison is on.
type GapResult struct {
	ReferenceSource      string           `json:"referenceSource"` // "project" | "file"
	ReferenceCount       int              `json:"referenceCount"`
	TargetCount          int              `json:"targetCount"`
	Matched              int              `json:"matched"`
	MissingFromReference []GapTest        `json:"missingFromReference"`
	MissingFromTarget    []GapTest        `json:"missingFromTarget"`
	ThreeWay             bool             `json:"threeWay"`
	ProjectCount         int              `json:"projectCount"`
	MissingFromProject   []GapTest        `json:"missingFromProject"`
	FolderMismatches     []FolderMismatch `json:"folderMismatches"`
}

// GapOptions configures a comparison.
type GapOptions struct {
	ReferenceSource string // "project" | "file"
	ThreeWay        bool   // also diff against the project list (reference-from-file only)
	CompareFolders  bool   // report folder mismatches among summary-matched tests
}

// normalizeSummary is the match key: trim, collapse internal whitespace runs to
// single spaces, lowercase. Blank stays blank.
func normalizeSummary(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// normalizeFolder trims surrounding whitespace and a trailing slash so folder
// paths compare cleanly. Case is preserved (folder paths are case-sensitive).
func normalizeFolder(s string) string {
	t := strings.TrimSpace(s)
	if len(t) > 1 {
		t = strings.TrimRight(t, "/")
	}
	return t
}

// summarySet returns the set of non-blank normalized summaries in a list.
func summarySet(tests []GapTest) map[string]bool {
	set := make(map[string]bool, len(tests))
	for _, t := range tests {
		if k := normalizeSummary(t.Summary); k != "" {
			set[k] = true
		}
	}
	return set
}

// missing returns the tests whose normalized summary is not in other, blanks
// skipped, deduplicated by normalized summary (first occurrence wins).
func missing(tests []GapTest, other map[string]bool) []GapTest {
	out := []GapTest{}
	seen := map[string]bool{}
	for _, t := range tests {
		k := normalizeSummary(t.Summary)
		if k == "" || other[k] || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, t)
	}
	return out
}

// unionBySummary merges two lists into one deduplicated by normalized summary,
// preferring the target's row for a summary present in both (target is the
// authoritative external list), blanks skipped.
func unionBySummary(reference, target []GapTest) []GapTest {
	out := []GapTest{}
	seen := map[string]bool{}
	add := func(t GapTest) {
		k := normalizeSummary(t.Summary)
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		out = append(out, t)
	}
	for _, t := range target {
		add(t)
	}
	for _, t := range reference {
		add(t)
	}
	return out
}

// folderMismatches returns summary-matched tests whose folders differ. Built
// from the first folder seen per normalized summary on each side.
func folderMismatches(reference, target []GapTest) []FolderMismatch {
	refFolder := firstFolderBySummary(reference)
	targetFolder := firstFolderBySummary(target)
	out := []FolderMismatch{}
	seen := map[string]bool{}
	for _, t := range target {
		k := normalizeSummary(t.Summary)
		if k == "" || seen[k] {
			continue
		}
		rf, ok := refFolder[k]
		if !ok {
			continue // not matched on the reference side
		}
		seen[k] = true
		if normalizeFolder(rf) != normalizeFolder(targetFolder[k]) {
			out = append(out, FolderMismatch{Summary: t.Summary, ReferenceFolder: rf, TargetFolder: targetFolder[k]})
		}
	}
	return out
}

// firstFolderBySummary maps each normalized summary to the first folder seen.
func firstFolderBySummary(tests []GapTest) map[string]string {
	m := map[string]string{}
	for _, t := range tests {
		k := normalizeSummary(t.Summary)
		if k == "" {
			continue
		}
		if _, ok := m[k]; !ok {
			m[k] = t.Folder
		}
	}
	return m
}

// AnalyzeGap compares a reference and target list by normalized summary, with
// optional three-way (against project) and folder comparison.
func AnalyzeGap(reference, target, project []GapTest, opts GapOptions) GapResult {
	refSet := summarySet(reference)
	targetSet := summarySet(target)
	matched := 0
	for k := range refSet {
		if targetSet[k] {
			matched++
		}
	}
	res := GapResult{
		ReferenceSource:      opts.ReferenceSource,
		ReferenceCount:       len(reference),
		TargetCount:          len(target),
		Matched:              matched,
		MissingFromReference: missing(target, refSet),
		MissingFromTarget:    missing(reference, targetSet),
		MissingFromProject:   []GapTest{},
		FolderMismatches:     []FolderMismatch{},
	}
	if opts.ThreeWay {
		projectSet := summarySet(project)
		res.ThreeWay = true
		res.ProjectCount = len(project)
		res.MissingFromProject = missing(unionBySummary(reference, target), projectSet)
	}
	if opts.CompareFolders {
		res.FolderMismatches = folderMismatches(reference, target)
	}
	return res
}

// gapAutoMapping builds an ImportMapping from a header row by matching each
// canonical field name case-insensitively — the import-template contract, so
// no manual mapping UI is needed.
func gapAutoMapping(header []string) ImportMapping {
	find := func(name string) string {
		for _, h := range header {
			if strings.EqualFold(strings.TrimSpace(h), name) {
				return strings.TrimSpace(h)
			}
		}
		return ""
	}
	return ImportMapping{
		Summary:     find("Summary"),
		Description: find("Description"),
		Priority:    find("Priority"),
		Labels:      find("Labels"),
		Components:  find("Components"),
		Folder:      find("Folder"),
		Action:      find("Action"),
		Data:        find("Data"),
		Expected:    find("Expected"),
	}
}

// ParseGapRows parses spreadsheet records into GapTests, auto-mapping columns
// by the import-template header names and reusing the import grouping so step
// rows don't become spurious entries. Errors if no Summary column is found.
func ParseGapRows(records [][]string) ([]GapTest, error) {
	if len(records) == 0 {
		return nil, fmt.Errorf("the file is empty")
	}
	mapping := gapAutoMapping(records[0])
	tests, _, _, err := groupImportRows(records, mapping)
	if err != nil {
		return nil, err
	}
	out := make([]GapTest, 0, len(tests))
	for _, p := range tests {
		out = append(out, payloadToGapTest(p))
	}
	return out, nil
}

// GapRowsFromTests maps cached project Tests into GapTests for the reference side.
func GapRowsFromTests(tests []TestCase) []GapTest {
	out := make([]GapTest, 0, len(tests))
	for _, t := range tests {
		out = append(out, GapTest{
			Summary:     t.Summary,
			Description: t.Description,
			Priority:    t.Priority,
			Labels:      t.Labels,
			Components:  t.Components,
			Folder:      t.FolderID,
		})
	}
	return out
}

// payloadToGapTest converts an import payload (joined labels/components) to
// the exported GapTest (slice fields).
func payloadToGapTest(p testCreatePayload) GapTest {
	return GapTest{
		Summary:     p.Summary,
		Description: p.Description,
		Priority:    p.Priority,
		Labels:      strings.Fields(p.Labels),
		Components:  splitComponents(p.Components),
		Folder:      p.Folder,
	}
}

// splitComponents splits a comma-separated component string, trimming blanks.
func splitComponents(s string) []string {
	out := []string{}
	for _, c := range strings.Split(s, ",") {
		if t := strings.TrimSpace(c); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// Defaults applied to a gap added as a test when the source file (e.g. a
// summary-only template) did not provide the field, so created tests have a
// sensible priority and a description rather than blanks.
const (
	defaultGapPriority    = "Medium"
	defaultGapDescription = "(added from gap analysis)"
)

// gapTestToPayload converts an exported GapTest to an import payload (joined
// labels/components, no steps) for insertLocalTest, filling empty Priority and
// Description with defaults.
func gapTestToPayload(g GapTest) testCreatePayload {
	priority := g.Priority
	if strings.TrimSpace(priority) == "" {
		priority = defaultGapPriority
	}
	description := g.Description
	if strings.TrimSpace(description) == "" {
		description = defaultGapDescription
	}
	return testCreatePayload{
		Summary:     g.Summary,
		Description: description,
		Priority:    priority,
		Labels:      strings.Join(g.Labels, " "),
		Components:  strings.Join(g.Components, ", "),
		Folder:      g.Folder,
	}
}

// CreateTestsFromGaps creates a local pending Test (NEW-N) for each gap with a
// non-blank summary, reusing the import create path. Blank-summary gaps are
// skipped and reported, mirroring ImportTests.
func (r *Repository) CreateTestsFromGaps(profileID string, gaps []GapTest) (ImportResult, error) {
	result := ImportResult{Errors: []ImportError{}}
	tx, err := r.db.Begin()
	if err != nil {
		return result, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for i, g := range gaps {
		if strings.TrimSpace(g.Summary) == "" {
			result.Errors = append(result.Errors, ImportError{Row: i + 1, Message: "gap has no summary"})
			result.Skipped++
			continue
		}
		if _, err := insertLocalTest(tx, profileID, gapTestToPayload(g), "gap-create-local"); err != nil {
			return result, err
		}
		result.Created++
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit gap creates: %w", err)
	}
	return result, nil
}

// gapReportHeader is the per-row column order for the two gap sections.
var gapReportHeader = []string{"Summary", "Description", "Priority", "Labels", "Components", "Folder"}

// BuildGapReport renders the management report: a metadata block, then the two
// gap sections. generatedAt is supplied by the caller (the binding passes
// time.Now) so the function stays testable. format is "csv" or "xlsx".
func BuildGapReport(result GapResult, generatedAt, format string) ([]byte, error) {
	rows := [][]string{
		{"Test Case Gap Analysis Report"},
		{"Generated", generatedAt},
		{"Reference source", result.ReferenceSource},
		{"Reference count", fmt.Sprintf("%d", result.ReferenceCount)},
		{"Target count", fmt.Sprintf("%d", result.TargetCount)},
		{"Matched", fmt.Sprintf("%d", result.Matched)},
		{"Missing from reference", fmt.Sprintf("%d", len(result.MissingFromReference))},
		{"Missing from target", fmt.Sprintf("%d", len(result.MissingFromTarget))},
	}
	if result.ThreeWay {
		rows = append(rows,
			[]string{"Project count", fmt.Sprintf("%d", result.ProjectCount)},
			[]string{"Missing from project", fmt.Sprintf("%d", len(result.MissingFromProject))},
		)
	}
	rows = append(rows,
		[]string{},
		[]string{"Missing from reference (in target, not reference)"},
		gapReportHeader,
	)
	for _, g := range result.MissingFromReference {
		rows = append(rows, gapRow(g))
	}
	rows = append(rows, []string{}, []string{"Missing from target (in reference, not target)"}, gapReportHeader)
	for _, g := range result.MissingFromTarget {
		rows = append(rows, gapRow(g))
	}
	if result.ThreeWay {
		rows = append(rows, []string{}, []string{"Missing from project (in reference or target, not project)"}, gapReportHeader)
		for _, g := range result.MissingFromProject {
			rows = append(rows, gapRow(g))
		}
	}
	if len(result.FolderMismatches) > 0 {
		rows = append(rows, []string{}, []string{"Folder mismatches (matched by summary, different folder)"},
			[]string{"Summary", "Reference folder", "Target folder"})
		for _, m := range result.FolderMismatches {
			rows = append(rows, []string{m.Summary, m.ReferenceFolder, m.TargetFolder})
		}
	}
	if format == "xlsx" {
		return writeXLSX(rows)
	}
	return writeCSV(rows)
}

func gapRow(g GapTest) []string {
	return []string{
		g.Summary, g.Description, g.Priority,
		strings.Join(g.Labels, " "), strings.Join(g.Components, ", "), g.Folder,
	}
}
