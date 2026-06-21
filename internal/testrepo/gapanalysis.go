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

// GapResult is the outcome of a comparison. MissingFromReference is in the
// target but not the reference (addable as tests); MissingFromTarget is in the
// reference but not the target (report-only). Both gap lists are deduplicated
// by normalized summary.
type GapResult struct {
	ReferenceSource      string    `json:"referenceSource"` // "project" | "file"
	ReferenceCount       int       `json:"referenceCount"`
	TargetCount          int       `json:"targetCount"`
	Matched              int       `json:"matched"`
	MissingFromReference []GapTest `json:"missingFromReference"`
	MissingFromTarget    []GapTest `json:"missingFromTarget"`
}

// normalizeSummary is the match key: trim, collapse internal whitespace runs to
// single spaces, lowercase. Blank stays blank.
func normalizeSummary(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
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

// AnalyzeGap compares two lists by normalized summary.
func AnalyzeGap(reference, target []GapTest, referenceSource string) GapResult {
	refSet := summarySet(reference)
	targetSet := summarySet(target)
	matched := 0
	for k := range refSet {
		if targetSet[k] {
			matched++
		}
	}
	return GapResult{
		ReferenceSource:      referenceSource,
		ReferenceCount:       len(reference),
		TargetCount:          len(target),
		Matched:              matched,
		MissingFromReference: missing(target, refSet),
		MissingFromTarget:    missing(reference, targetSet),
	}
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

// gapTestToPayload converts an exported GapTest to an import payload (joined
// labels/components, no steps) for insertLocalTest.
func gapTestToPayload(g GapTest) testCreatePayload {
	return testCreatePayload{
		Summary:     g.Summary,
		Description: g.Description,
		Priority:    g.Priority,
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
		{},
		{"Missing from reference (in target, not reference)"},
		gapReportHeader,
	}
	for _, g := range result.MissingFromReference {
		rows = append(rows, gapRow(g))
	}
	rows = append(rows, []string{}, []string{"Missing from target (in reference, not target)"}, gapReportHeader)
	for _, g := range result.MissingFromTarget {
		rows = append(rows, gapRow(g))
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
