package kiwi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// categoryRow renders one Category.filter row.
func categoryRow(id int, name string) string {
	return fmt.Sprintf(`{"id":%d,"name":%q,"product":6,"description":""}`, id, name)
}

// caseRowInCategory renders a TestCase.filter row carrying a category, the way
// a live Kiwi row does (verified against a real instance: the row has both
// "category" and "category__name").
func caseRowInCategory(id, catID int, catName string) string {
	return fmt.Sprintf(
		`{"id":%d,"summary":"Test %d","text":"","case_status__name":"CONFIRMED",`+
			`"priority__value":"P1","is_automated":false,"author__username":"u",`+
			`"create_date":"2026-01-01T00:00:00Z","history_date":"2026-01-02T00:00:00Z",`+
			`"category":%d,"category__name":%q}`,
		id, id, catID, catName)
}

// newFolderKiwi serves a product with two real categories plus Kiwi's
// "--default--", and test cases spread across them.
func newFolderKiwi(t *testing.T) *Adapter {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		var req struct {
			Method string          `json:"method"`
			Params []json.RawMessage `json:"params"`
			ID     int             `json:"id"`
		}
		_ = json.Unmarshal(body, &req)
		w.Header().Set("Content-Type", "application/json")

		switch req.Method {
		case "Auth.login":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":"sid"}`, req.ID)
		case "Category.filter":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":[%s,%s,%s]}`, req.ID,
				categoryRow(334, "--default--"),
				categoryRow(336, "Phase-2"),
				categoryRow(335, "Phase-1"))
		case "TestCase.filter":
			filter := ""
			if len(req.Params) > 0 {
				filter = string(req.Params[0])
			}
			// ListTestsInFolder narrows by category id.
			if contains(filter, `"category":335`) {
				fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":[%s,%s]}`, req.ID,
					caseRowInCategory(1, 335, "Phase-1"),
					caseRowInCategory(2, 335, "Phase-1"))
				return
			}
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":[%s,%s,%s,%s]}`, req.ID,
				caseRowInCategory(1, 335, "Phase-1"),
				caseRowInCategory(2, 335, "Phase-1"),
				caseRowInCategory(3, 336, "Phase-2"),
				caseRowInCategory(4, 334, "--default--"))
		default:
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":[]}`, req.ID)
		}
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "u:p")
}

func contains(hay, needle string) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// TestFolderTreeMapsCategories is the core of the mapping: a Kiwi category is
// the closest analogue to an Xray Test Repository folder, and both were
// arriving on the wire already.
func TestFolderTreeMapsCategories(t *testing.T) {
	tree, err := newFolderKiwi(t).FolderTree(context.Background(), "SNMP")
	if err != nil {
		t.Fatalf("folder tree: %v", err)
	}
	if len(tree.Folders) != 2 {
		t.Fatalf("got %d folders, want 2 (--default-- is not a folder): %+v",
			len(tree.Folders), tree.Folders)
	}
	// Sorted by name, so Phase-1 precedes Phase-2 regardless of id order.
	if tree.Folders[0].Name != "Phase-1" || tree.Folders[1].Name != "Phase-2" {
		t.Fatalf("folders not in name order: %+v", tree.Folders)
	}
	f := tree.Folders[0]
	if f.ID != "335" {
		t.Errorf("folder id = %q, want the category id 335", f.ID)
	}
	if f.ParentID != "" {
		t.Errorf("parent = %q, want empty: Kiwi categories are flat", f.ParentID)
	}
	if f.TestCount != 2 || f.TotalTestCount != 2 {
		t.Errorf("Phase-1 counts = %d/%d, want 2/2", f.TestCount, f.TotalTestCount)
	}
}

// TestFolderTreeTreatsDefaultCategoryAsUnfiled pins the one judgment call in
// the mapping. Kiwi creates "--default--" for every product, so it means "not
// filed" rather than naming a real folder.
func TestFolderTreeTreatsDefaultCategoryAsUnfiled(t *testing.T) {
	tree, err := newFolderKiwi(t).FolderTree(context.Background(), "SNMP")
	if err != nil {
		t.Fatalf("folder tree: %v", err)
	}
	for _, f := range tree.Folders {
		if f.Name == "--default--" {
			t.Fatalf("--default-- surfaced as a folder: %+v", f)
		}
	}
	// Case 4 lives in --default--, so it belongs to no folder.
	if path, ok := tree.TreeMembership["4"]; ok {
		t.Errorf("unfiled test mapped to folder %q, want no entry", path)
	}
	if tree.TreeMembership["1"] != "Phase-1" {
		t.Errorf("membership for test 1 = %q, want Phase-1", tree.TreeMembership["1"])
	}
}

// TestFolderTreeReportsOnlyFoldersWithTests checks the FoldersWithTests hint
// the syncer uses to skip empty folders.
func TestFolderTreeReportsOnlyFoldersWithTests(t *testing.T) {
	tree, err := newFolderKiwi(t).FolderTree(context.Background(), "SNMP")
	if err != nil {
		t.Fatalf("folder tree: %v", err)
	}
	if len(tree.FoldersWithTests) != 2 {
		t.Fatalf("got %+v, want both categories (each has tests)", tree.FoldersWithTests)
	}
}

// TestToTestCarriesTheCategoryAsFolderID is the mapping at the row level. This
// was hardcoded to "" before, so every Kiwi test synced unfiled.
func TestToTestCarriesTheCategoryAsFolderID(t *testing.T) {
	a := newFolderKiwi(t)
	page, _, err := a.SearchTestsPage(context.Background(), "SNMP", "", "", 0, 100)
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	byKey := map[string]string{}
	for _, tc := range page {
		byKey[tc.Key] = tc.FolderID
	}
	if byKey["1"] != "335" {
		t.Errorf("test 1 folder = %q, want 335", byKey["1"])
	}
	if byKey["3"] != "336" {
		t.Errorf("test 3 folder = %q, want 336", byKey["3"])
	}
	if byKey["4"] != "" {
		t.Errorf("test 4 is in --default--, folder = %q, want empty", byKey["4"])
	}
}

// TestListTestsInFolderNarrowsByCategory checks the folder drill-down asks the
// server for that category rather than filtering a full fetch locally.
func TestListTestsInFolderNarrowsByCategory(t *testing.T) {
	keys, err := newFolderKiwi(t).ListTestsInFolder(context.Background(), "SNMP", "335")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("got %v, want the two Phase-1 tests", keys)
	}
}

// TestListTestsInFolderRejectsANonCategoryID guards the id contract: folder
// ids are category ids, so anything else is a caller bug worth naming.
func TestListTestsInFolderRejectsANonCategoryID(t *testing.T) {
	_, err := newFolderKiwi(t).ListTestsInFolder(context.Background(), "SNMP", "Phase-1")
	if err == nil {
		t.Fatal("want an error for a name-shaped folder id")
	}
}

// TestFolderCapabilitiesSeparateReadFromWrite pins why the capability had to
// split: Kiwi can report folders it cannot reshape, and the UI needs to know
// the difference so it does not offer buttons that always fail.
func TestFolderCapabilitiesSeparateReadFromWrite(t *testing.T) {
	caps := newFolderKiwi(t).Capabilities()
	if !caps.SupportsFolders {
		t.Error("SupportsFolders = false, want true: categories read as folders")
	}
	if caps.SupportsFolderWrites {
		t.Error("SupportsFolderWrites = true, want false: categories cannot be reshaped")
	}
}
