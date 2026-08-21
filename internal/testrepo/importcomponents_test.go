package testrepo_test

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"xray-test-manager/internal/testrepo"
)

// A cell holding several components must be quoted, or the comma splits it
// into two spreadsheet columns.
const componentCSV = "Summary,Components\n" +
	"Login works,Auth\n" +
	"Logout works,\"Auth, Telco Feature\"\n" +
	"Reset works,auth\n"

var componentMapping = testrepo.ImportMapping{Summary: "Summary", Components: "Components"}

// seedComponents caches the project's component list the way a sync would.
func seedComponents(t *testing.T, repo *testrepo.Repository, names ...string) {
	t.Helper()
	if err := repo.ReplaceProjectFieldOptions("p1", "PROJ", "component", names); err != nil {
		t.Fatalf("seed components: %v", err)
	}
}

// importedComponents reads back the Components value of each queued test
// create, sorted. ListPendingChanges does not promise import order, so callers
// compare as a multiset rather than by row.
func importedComponents(t *testing.T, repo *testrepo.Repository) []string {
	t.Helper()
	pcs, err := repo.ListPendingChanges("p1")
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	out := []string{}
	for _, pc := range pcs {
		var p struct {
			Summary    string `json:"summary"`
			Components string `json:"components"`
		}
		if err := json.Unmarshal([]byte(pc.AfterVal), &p); err != nil || p.Summary == "" {
			continue
		}
		out = append(out, p.Components)
	}
	sort.Strings(out)
	return out
}

// TestImportReportsUnknownComponents is the core of RND_P_4TFINT_05-340: a
// component the project does not have must be surfaced at validation time,
// instead of only showing up as a failed commit later.
func TestImportReportsUnknownComponents(t *testing.T) {
	repo := newRepo(t)
	seedComponents(t, repo, "Auth", "Billing")

	res, err := repo.ImportTests("p1", "PROJ", recordsOf(t, componentCSV), componentMapping, true, false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	// "Auth" is known. "Telco Feature" is not. "auth" is not either, because
	// Jira matches component names exactly.
	names := []string{}
	for _, uc := range res.UnknownComponents {
		names = append(names, uc.Name)
	}
	want := "Telco Feature,auth"
	if got := strings.Join(names, ","); got != want {
		t.Fatalf("want unknown %q, got %q", want, got)
	}

	// A case-only mismatch is the common cause, so it carries the real spelling.
	for _, uc := range res.UnknownComponents {
		switch uc.Name {
		case "auth":
			if uc.Suggestion != "Auth" {
				t.Errorf(`want suggestion "Auth" for "auth", got %q`, uc.Suggestion)
			}
		case "Telco Feature":
			if uc.Suggestion != "" {
				t.Errorf("want no suggestion for a genuinely new name, got %q", uc.Suggestion)
			}
		}
	}
}

// TestImportUnknownComponentsDoNotSkipRows pins that an unknown component is a
// warning, not a validation failure. The row is otherwise fine.
func TestImportUnknownComponentsDoNotSkipRows(t *testing.T) {
	repo := newRepo(t)
	seedComponents(t, repo, "Auth")

	res, err := repo.ImportTests("p1", "PROJ", recordsOf(t, componentCSV), componentMapping, true, false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Created != 3 {
		t.Errorf("want 3 valid rows, got %d", res.Created)
	}
	if res.Skipped != 0 {
		t.Errorf("want nothing skipped, got %d", res.Skipped)
	}
	if len(res.Errors) != 0 {
		t.Errorf("want no row errors, got %+v", res.Errors)
	}
}

// TestImportDropsUnknownComponents covers the way out of the block: the tests
// import with their valid components and without the ones Jira would reject.
func TestImportDropsUnknownComponents(t *testing.T) {
	repo := newRepo(t)
	seedComponents(t, repo, "Auth", "Billing")

	if _, err := repo.ImportTests("p1", "PROJ", recordsOf(t, componentCSV), componentMapping, false, true); err != nil {
		t.Fatalf("import: %v", err)
	}
	// "Auth" survives on both rows that had it, "Telco Feature" and the
	// wrong-case "auth" are gone. Sorted, so the empty value leads.
	got := importedComponents(t, repo)
	want := []string{"", "Auth", "Auth"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("want components %v, got %v", want, got)
	}
}

// TestImportKeepsUnknownComponentsWhenNotDropping checks the opposite choice is
// honoured, since a user may prefer to create the component in Jira and retry.
func TestImportKeepsUnknownComponentsWhenNotDropping(t *testing.T) {
	repo := newRepo(t)
	seedComponents(t, repo, "Auth")

	if _, err := repo.ImportTests("p1", "PROJ", recordsOf(t, componentCSV), componentMapping, false, false); err != nil {
		t.Fatalf("import: %v", err)
	}
	got := importedComponents(t, repo)
	if len(got) < 2 || !strings.Contains(got[1], "Telco Feature") {
		t.Fatalf("want the unknown component preserved, got %v", got)
	}
}

// TestImportSkipsComponentCheckWhenNoneCached guards the case that would
// otherwise flag every component as unknown. An empty cache means the profile
// has not synced its project field options, not that the project has no
// components, and guessing wrong here would block a valid import.
func TestImportSkipsComponentCheckWhenNoneCached(t *testing.T) {
	repo := newRepo(t)
	// Deliberately no seedComponents call.

	res, err := repo.ImportTests("p1", "PROJ", recordsOf(t, componentCSV), componentMapping, true, false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(res.UnknownComponents) != 0 {
		t.Fatalf("want no unknown components when nothing is cached, got %+v", res.UnknownComponents)
	}
}

// TestImportSkipsComponentCheckWhenColumnUnmapped checks an import that never
// mentions components does no component work at all.
func TestImportSkipsComponentCheckWhenColumnUnmapped(t *testing.T) {
	repo := newRepo(t)
	seedComponents(t, repo, "Auth")

	res, err := repo.ImportTests("p1", "PROJ", recordsOf(t, componentCSV),
		testrepo.ImportMapping{Summary: "Summary"}, true, false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(res.UnknownComponents) != 0 {
		t.Fatalf("want no unknown components when the column is unmapped, got %+v", res.UnknownComponents)
	}
}

// TestImportUnknownComponentsDeduplicated checks a name repeated across many
// rows is reported once, so a 500-row file does not produce a 500-item warning.
func TestImportUnknownComponentsDeduplicated(t *testing.T) {
	repo := newRepo(t)
	seedComponents(t, repo, "Auth")

	csv := "Summary,Components\nA,Nope\nB,Nope\nC,Nope\n"
	res, err := repo.ImportTests("p1", "PROJ", recordsOf(t, csv), componentMapping, true, false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(res.UnknownComponents) != 1 || res.UnknownComponents[0].Name != "Nope" {
		t.Fatalf("want one deduplicated entry, got %+v", res.UnknownComponents)
	}
}
