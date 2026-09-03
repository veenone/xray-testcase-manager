package kiwi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newProductKiwi serves a fixed product list and returns test cases only for an
// exactly-matching product name, which is what Kiwi itself does: its filters
// are case-sensitive, so "SPHERE HSM" matches nothing at all.
func newProductKiwi(t *testing.T) *Adapter {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
			ID     int               `json:"id"`
		}
		_ = json.Unmarshal(body, &req)
		w.Header().Set("Content-Type", "application/json")

		switch req.Method {
		case "Auth.login":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":"sid"}`, req.ID)
		case "Product.filter":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":[`+
				`{"id":4,"name":"Sphere HSM"},{"id":5,"name":"Balto"},{"id":6,"name":"SNMP"}]}`, req.ID)
		case "TestCase.filter":
			filter := ""
			if len(req.Params) > 0 {
				filter = string(req.Params[0])
			}
			// Only the exact name matches, mirroring Kiwi's case sensitivity.
			if strings.Contains(filter, `"category__product__name":"Sphere HSM"`) {
				fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":[%s]}`, req.ID,
					caseRowInCategory(1, 335, "Phase-1"))
				return
			}
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":[]}`, req.ID)
		default:
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":[]}`, req.ID)
		}
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "u:p")
}

// TestSearchTestsPageSuggestsTheRealCasing is the reported bug: typing
// "SPHERE HSM" for "Sphere HSM" synced nothing and said nothing, because Kiwi
// filters are case-sensitive and an unknown product is indistinguishable from
// an empty one on the wire.
func TestSearchTestsPageSuggestsTheRealCasing(t *testing.T) {
	_, _, err := newProductKiwi(t).SearchTestsPage(
		context.Background(), "SPHERE HSM", "", "", 0, 100)
	if err == nil {
		t.Fatal("want an error naming the right casing, got a silent empty sync")
	}
	if !strings.Contains(err.Error(), "Sphere HSM") {
		t.Errorf("error %q does not suggest the real product name", err)
	}
}

// TestSearchTestsPageNamesAvailableProductsWhenNoMatch covers a plain typo,
// where no casing of the name exists.
func TestSearchTestsPageNamesAvailableProductsWhenNoMatch(t *testing.T) {
	_, _, err := newProductKiwi(t).SearchTestsPage(
		context.Background(), "Sphere HMS", "", "", 0, 100)
	if err == nil {
		t.Fatal("want an error for a product that does not exist")
	}
	for _, want := range []string{"Sphere HSM", "Balto", "SNMP"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not list the available product %q", err, want)
		}
	}
}

// TestSearchTestsPageAcceptsAGenuinelyEmptyProduct guards the false positive.
// A product that exists and simply has no tests must sync cleanly, not error.
func TestSearchTestsPageAcceptsAGenuinelyEmptyProduct(t *testing.T) {
	page, total, err := newProductKiwi(t).SearchTestsPage(
		context.Background(), "Balto", "", "", 0, 100)
	if err != nil {
		t.Fatalf("an existing empty product must not error: %v", err)
	}
	if len(page) != 0 || total != 0 {
		t.Errorf("got %d tests / total %d, want an empty result", len(page), total)
	}
}

// TestSearchTestsPageDoesNotProbeProductsOnASuccessfulSync keeps the check off
// the hot path: the extra Product.filter call only happens when a sync came
// back empty, never on every page of a working one.
func TestSearchTestsPageDoesNotProbeProductsOnASuccessfulSync(t *testing.T) {
	a, k := newCountingKiwi(t, 5)
	if _, _, err := a.SearchTestsPage(context.Background(), "Prod", "", "", 0, 100); err != nil {
		t.Fatalf("page: %v", err)
	}
	if got := k.count("Product.filter"); got != 0 {
		t.Errorf("Product.filter called %d times on a successful sync, want 0", got)
	}
}
