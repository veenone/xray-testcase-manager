package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// preconditionKey is the zero-padded key for the nth generated precondition, so
// keys sort lexicographically in the same order they are searched.
func preconditionKey(n int) string { return fmt.Sprintf("PC-%06d", n) }

// newPreconditionServerWithAssocHandler serves a project of `count`
// preconditions: the issue-type lookup, the paged JQL search, and an
// association endpoint supplied by the caller.
func newPreconditionServerWithAssocHandler(t *testing.T, count int, assoc http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/2/issuetype":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "42", "name": "Pre-Condition"},
			})
		case r.URL.Path == "/rest/api/2/search":
			startAt, _ := strconv.Atoi(r.URL.Query().Get("startAt"))
			maxResults, _ := strconv.Atoi(r.URL.Query().Get("maxResults"))
			issues := []map[string]any{}
			for i := startAt; i < count && i < startAt+maxResults; i++ {
				issues = append(issues, map[string]any{
					"key": preconditionKey(i + 1),
					"fields": map[string]any{
						"summary":     fmt.Sprintf("Precondition %d", i+1),
						"description": "d",
					},
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"total": count, "issues": issues})
		case strings.HasPrefix(r.URL.Path, "/rest/raven/1.0/api/precondition/"):
			assoc(w, r)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// newPreconditionServer is newPreconditionServerWithAssocHandler with a default
// association handler: each precondition links to exactly one test, named after
// the precondition so the mapping is checkable.
func newPreconditionServer(t *testing.T, count int) *httptest.Server {
	t.Helper()
	return newPreconditionServerWithAssocHandler(t, count, func(w http.ResponseWriter, r *http.Request) {
		// .../precondition/{key}/test
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		key := parts[len(parts)-2]
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		arr := []map[string]any{}
		if page <= 1 {
			arr = append(arr, map[string]any{"key": "T-" + key})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(arr)
	})
}

// TestListPreconditionsStreamEmitsBatches pins the core of the -336 fix: work
// reaches the caller as it is resolved, so an interrupted pass has already
// persisted the earlier batches.
func TestListPreconditionsStreamEmitsBatches(t *testing.T) {
	srv := newPreconditionServer(t, 450)
	defer srv.Close()
	c := newTestClient(srv)

	var sizes []int
	totalPre := 0
	err := c.ListPreconditionsStream(context.Background(), "QA", nil,
		func(pre []Precondition, links map[string][]string) error {
			sizes = append(sizes, len(pre))
			totalPre += len(pre)
			return nil
		})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	want := []int{200, 200, 50}
	if len(sizes) != len(want) {
		t.Fatalf("got %d batches %v, want %d %v", len(sizes), sizes, len(want), want)
	}
	for i := range want {
		if sizes[i] != want[i] {
			t.Errorf("batch %d size %d, want %d", i, sizes[i], want[i])
		}
	}
	if totalPre != 450 {
		t.Errorf("streamed %d preconditions, want 450", totalPre)
	}
}

// TestListPreconditionsStreamAbortsOnBatchError checks a store failure inside
// onBatch stops the pass instead of being swallowed.
func TestListPreconditionsStreamAbortsOnBatchError(t *testing.T) {
	srv := newPreconditionServer(t, 450)
	defer srv.Close()
	c := newTestClient(srv)

	sentinel := errors.New("store is down")
	calls := 0
	err := c.ListPreconditionsStream(context.Background(), "QA", nil,
		func(pre []Precondition, links map[string][]string) error {
			calls++
			return sentinel
		})
	if !errors.Is(err, sentinel) {
		t.Fatalf("got error %v, want it to wrap %v", err, sentinel)
	}
	if calls != 1 {
		t.Errorf("onBatch called %d times, want 1 (pass must abort on first error)", calls)
	}
}

// TestListPreconditionsStreamLinksBackToPreconditions checks the link map a
// batch carries points test keys at the preconditions in that same batch.
func TestListPreconditionsStreamLinksBackToPreconditions(t *testing.T) {
	srv := newPreconditionServer(t, 3)
	defer srv.Close()
	c := newTestClient(srv)

	got := map[string][]string{}
	err := c.ListPreconditionsStream(context.Background(), "QA", nil,
		func(pre []Precondition, links map[string][]string) error {
			for tk, pks := range links {
				got[tk] = append(got[tk], pks...)
			}
			return nil
		})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	for i := 1; i <= 3; i++ {
		key := preconditionKey(i)
		linked := got["T-"+key]
		if len(linked) != 1 || linked[0] != key {
			t.Errorf("test T-%s links to %v, want [%s]", key, linked, key)
		}
	}
}

// TestListPreconditionsStillCollectsEverything checks the non-streaming wrapper
// keeps its old behaviour now that it is implemented over the stream.
func TestListPreconditionsStillCollectsEverything(t *testing.T) {
	srv := newPreconditionServer(t, 250)
	defer srv.Close()
	c := newTestClient(srv)

	pres, links, err := c.ListPreconditions(context.Background(), "QA", nil)
	if err != nil {
		t.Fatalf("ListPreconditions: %v", err)
	}
	if len(pres) != 250 {
		t.Fatalf("got %d preconditions, want 250", len(pres))
	}
	if len(links) != 250 {
		t.Fatalf("got %d linked tests, want 250", len(links))
	}
	keys := make([]string, len(pres))
	for i, p := range pres {
		keys[i] = p.Key
	}
	if !sort.StringsAreSorted(keys) {
		t.Error("collected preconditions are not in key order")
	}
}

// TestListPreconditionsStreamReportsProgress checks progress counts run across
// batches rather than restarting at each one.
func TestListPreconditionsStreamReportsProgress(t *testing.T) {
	srv := newPreconditionServer(t, 250)
	defer srv.Close()
	c := newTestClient(srv)

	var maxDone, gotTotal int
	err := c.ListPreconditionsStream(context.Background(), "QA",
		func(done, total int) {
			if done > maxDone {
				maxDone = done
			}
			gotTotal = total
		},
		func(pre []Precondition, links map[string][]string) error { return nil })
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if gotTotal != 250 {
		t.Errorf("progress total %d, want 250", gotTotal)
	}
	if maxDone != 250 {
		t.Errorf("progress peaked at %d, want 250 (counter must span batches)", maxDone)
	}
}
