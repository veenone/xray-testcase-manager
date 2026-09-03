package kiwi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// caseRow renders one TestCase.filter row with the fields kiwiTestCase decodes.
func caseRow(id int) string {
	return fmt.Sprintf(
		`{"id":%d,"summary":"Test %d","text":"","case_status__name":"CONFIRMED",`+
			`"priority__value":"P1","is_automated":false,"author__username":"u",`+
			`"create_date":"2026-01-01T00:00:00Z","history_date":"2026-01-02T00:00:00Z"}`,
		id, id)
}

// countingKiwi is a mock Kiwi that records how many TestCase.filter calls it
// serves, so a test can assert the page cache actually removes refetches.
type countingKiwi struct {
	mu       sync.Mutex
	byMethod map[string]int
	rowCount int
}

func newCountingKiwi(t *testing.T, rowCount int) (*Adapter, *countingKiwi) {
	t.Helper()
	k := &countingKiwi{byMethod: map[string]int{}, rowCount: rowCount}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		var req struct {
			Method string `json:"method"`
			ID     int    `json:"id"`
		}
		_ = json.Unmarshal(body, &req)

		k.mu.Lock()
		k.byMethod[req.Method]++
		k.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "Auth.login":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":"sid"}`, req.ID)
		case "TestCase.filter":
			rows := make([]string, k.rowCount)
			for i := range rows {
				rows[i] = caseRow(i + 1)
			}
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":[%s]}`,
				req.ID, strings.Join(rows, ","))
		default:
			// Tag.filter / Component.filter and anything else: empty list.
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":[]}`, req.ID)
		}
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "u:p"), k
}

func (k *countingKiwi) count(method string) int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.byMethod[method]
}

// TestSearchTestsPageFetchesTheScopeOnce is the point of the cache. Kiwi's
// TestCase.filter has no server-side limit (verified live: "Cannot resolve
// keyword 'limit'"), so every page used to refetch the entire product. On a
// real 18,583-test product that was 186 full fetches for one sync.
func TestSearchTestsPageFetchesTheScopeOnce(t *testing.T) {
	a, k := newCountingKiwi(t, 250)
	ctx := context.Background()

	for offset := 0; offset < 250; offset += 100 {
		if _, _, err := a.SearchTestsPage(ctx, "Prod", "", "", offset, 100); err != nil {
			t.Fatalf("page at %d: %v", offset, err)
		}
	}
	if got := k.count("TestCase.filter"); got != 1 {
		t.Errorf("TestCase.filter called %d times for 3 pages, want 1", got)
	}
}

// TestSearchTestsPagePagesCorrectlyFromCache checks the slicing still works
// off the cached rows: no duplicates, no gaps, correct total on every page.
func TestSearchTestsPagePagesCorrectlyFromCache(t *testing.T) {
	a, _ := newCountingKiwi(t, 250)
	ctx := context.Background()

	seen := map[string]bool{}
	for offset := 0; offset < 250; offset += 100 {
		page, total, err := a.SearchTestsPage(ctx, "Prod", "", "", offset, 100)
		if err != nil {
			t.Fatalf("page at %d: %v", offset, err)
		}
		if total != 250 {
			t.Errorf("offset %d reported total %d, want 250", offset, total)
		}
		for _, tc := range page {
			if seen[tc.Key] {
				t.Errorf("key %s returned on more than one page", tc.Key)
			}
			seen[tc.Key] = true
		}
	}
	if len(seen) != 250 {
		t.Errorf("saw %d distinct tests across all pages, want 250", len(seen))
	}
}

// TestSearchTestsPageCacheIsScopedToTheProduct guards the correctness risk the
// cache introduces: switching product must not serve the previous product's
// rows.
func TestSearchTestsPageCacheIsScopedToTheProduct(t *testing.T) {
	a, k := newCountingKiwi(t, 10)
	ctx := context.Background()

	if _, _, err := a.SearchTestsPage(ctx, "ProdA", "", "", 0, 100); err != nil {
		t.Fatalf("ProdA: %v", err)
	}
	if _, _, err := a.SearchTestsPage(ctx, "ProdB", "", "", 0, 100); err != nil {
		t.Fatalf("ProdB: %v", err)
	}
	if got := k.count("TestCase.filter"); got != 2 {
		t.Errorf("TestCase.filter called %d times for two different products, want 2", got)
	}
}

// TestSearchTestsPageRefetchesOnANewSync pins that the cache does not outlive
// one pull. The engine restarts at offset 0 for every sync, so offset 0 is the
// signal to go back to the server; without this a second sync would replay the
// first sync's rows and never see a new or edited test.
func TestSearchTestsPageRefetchesOnANewSync(t *testing.T) {
	a, k := newCountingKiwi(t, 250)
	ctx := context.Background()

	// First sync: three pages, one fetch.
	for offset := 0; offset < 250; offset += 100 {
		if _, _, err := a.SearchTestsPage(ctx, "Prod", "", "", offset, 100); err != nil {
			t.Fatalf("sync 1 page %d: %v", offset, err)
		}
	}
	// Second sync starts over at offset 0 and must hit the server again.
	if _, _, err := a.SearchTestsPage(ctx, "Prod", "", "", 0, 100); err != nil {
		t.Fatalf("sync 2 page 0: %v", err)
	}
	if got := k.count("TestCase.filter"); got != 2 {
		t.Errorf("TestCase.filter called %d times across two syncs, want 2", got)
	}
}
