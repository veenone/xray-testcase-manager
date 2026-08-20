# Precondition Sync Durability (`-336`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the precondition sync stage durable and fast, so a new profile's first sync persists preconditions incrementally, never wipes valid links on an interrupted run, and reports honestly when it fails.

**Architecture:** Four changes to the existing stage. The link table gains a generation column so a wipe-and-rewrite becomes mark-and-sweep. A new optional `PreconditionStreamer` backend interface lets the Xray path report batches as it goes, which the engine commits per batch. The per-item 150ms sleep is replaced by bounded concurrency under the client's existing token-bucket limiter. Finally the stage stops swallowing its own failure.

**Tech Stack:** Go 1.25, `modernc.org/sqlite` (pure Go, no cgo), `golang.org/x/time/rate` (already a dependency), standard `testing`.

**Spec:** `docs/superpowers/specs/2026-08-20-v1.10.0-design.md` (sections 1.1 through 1.6)

## Global Constraints

- Schema version constant `schemaVersion` in `internal/store/store.go:21` goes from `48` to `49`. Bump it exactly once, in Task 1.
- Migrations in `applyMigrations` are written **unconditionally** with duplicate-tolerance, not `if current < N` gated. Follow the v46/v47 idiom: run the `ALTER TABLE`, and swallow an error containing `"duplicate column"`.
- `internal/` is import-private to this module. Nothing here becomes exported outside it.
- Jira is the system of record. The local store is a cache plus a pending-change journal, never authoritative.
- Credentials (PAT) never reach the database, a log line, or an error string.
- No AI attribution in commit messages. No `Co-Authored-By` trailer.
- Go formatting is `gofmt`. Run `gofmt -w .` before each commit.
- Every task ends green: `go build ./...` and `go test ./...` both pass.

---

### Task 1: Mark-and-sweep for the link table

Replaces the destructive `DELETE FROM test_precondition WHERE profile_id = ?` with a generation-stamped upsert plus a sweep that only runs after a clean pass.

**Files:**
- Modify: `internal/store/store.go:21` (schemaVersion), `:101-106` (baseSchema `test_precondition`), and the migration block near `:1290`
- Modify: `internal/testrepo/testrepo.go:643-679` (replace `ReplaceAllTestPreconditions`)
- Test: `internal/testrepo/testrepo_test.go:509` (rewrite the existing test, add three new ones)

**Interfaces:**
- Consumes: nothing from earlier tasks; this is the base.
- Produces:
  - `func (r *Repository) MarkTestPreconditions(profileID string, gen int64, links map[string][]string) error`
  - `func (r *Repository) SweepTestPreconditions(profileID string, gen int64) (int64, error)`
  - `ReplaceAllTestPreconditions` is **removed**; Task 2 rewires its only caller.

- [ ] **Step 1: Write the failing tests**

Replace `TestReplaceAllTestPreconditionsClearsStaleLinks` in `internal/testrepo/testrepo_test.go` with the following four tests. The first preserves the behavior the old test guarded (stale links do disappear on a clean pass); the rest are new.

```go
func TestSweepTestPreconditionsClearsStaleLinks(t *testing.T) {
	repo := newRepo(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "Login test"},
	}); err != nil {
		t.Fatalf("upsert tests: %v", err)
	}
	if err := repo.UpsertPreconditions("p1", []testrepo.Precondition{
		{Key: "QA-P-1", Summary: "Stale"},
	}); err != nil {
		t.Fatalf("upsert preconditions: %v", err)
	}
	if err := repo.MarkTestPreconditions("p1", 100, map[string][]string{
		"QA-1": {"QA-P-1"},
	}); err != nil {
		t.Fatalf("mark gen 100: %v", err)
	}

	// A later clean pass finds no links at all, then sweeps.
	if err := repo.MarkTestPreconditions("p1", 200, map[string][]string{}); err != nil {
		t.Fatalf("mark gen 200: %v", err)
	}
	deleted, err := repo.SweepTestPreconditions("p1", 200)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if deleted != 1 {
		t.Errorf("swept %d rows, want 1", deleted)
	}

	got, err := repo.ListTestPreconditions("p1", "QA-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d preconditions, want 0 after sweep", len(got))
	}
}

func TestMarkTestPreconditionsWithoutSweepKeepsOlderLinks(t *testing.T) {
	// This is the -336 regression: an interrupted pass must never delete
	// links it simply did not reach.
	repo := newRepo(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "Login test"},
		{Key: "QA-2", ID: "2", Summary: "Logout test"},
	}); err != nil {
		t.Fatalf("upsert tests: %v", err)
	}
	if err := repo.UpsertPreconditions("p1", []testrepo.Precondition{
		{Key: "QA-P-1", Summary: "One"},
		{Key: "QA-P-2", Summary: "Two"},
	}); err != nil {
		t.Fatalf("upsert preconditions: %v", err)
	}
	if err := repo.MarkTestPreconditions("p1", 100, map[string][]string{
		"QA-1": {"QA-P-1"},
		"QA-2": {"QA-P-2"},
	}); err != nil {
		t.Fatalf("mark gen 100: %v", err)
	}

	// A new pass gets through only the first batch, then dies. No sweep runs.
	if err := repo.MarkTestPreconditions("p1", 200, map[string][]string{
		"QA-1": {"QA-P-1"},
	}); err != nil {
		t.Fatalf("mark gen 200: %v", err)
	}

	for _, tk := range []string{"QA-1", "QA-2"} {
		got, err := repo.ListTestPreconditions("p1", tk)
		if err != nil {
			t.Fatalf("list %s: %v", tk, err)
		}
		if len(got) != 1 {
			t.Errorf("%s has %d preconditions, want 1 preserved", tk, len(got))
		}
	}
}

func TestSweepTestPreconditionsIsProfileScoped(t *testing.T) {
	repo := newRepo(t)
	for _, pid := range []string{"p1", "p2"} {
		if err := repo.UpsertTests(pid, []testrepo.TestCase{
			{Key: "QA-1", ID: "1", Summary: "Login test"},
		}); err != nil {
			t.Fatalf("upsert tests %s: %v", pid, err)
		}
		if err := repo.UpsertPreconditions(pid, []testrepo.Precondition{
			{Key: "QA-P-1", Summary: "One"},
		}); err != nil {
			t.Fatalf("upsert preconditions %s: %v", pid, err)
		}
		if err := repo.MarkTestPreconditions(pid, 100, map[string][]string{
			"QA-1": {"QA-P-1"},
		}); err != nil {
			t.Fatalf("mark %s: %v", pid, err)
		}
	}

	if _, err := repo.SweepTestPreconditions("p1", 200); err != nil {
		t.Fatalf("sweep p1: %v", err)
	}

	got, err := repo.ListTestPreconditions("p2", "QA-1")
	if err != nil {
		t.Fatalf("list p2: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("p2 has %d preconditions, want 1 (sweep must not cross profiles)", len(got))
	}
}

func TestMarkTestPreconditionsIsIdempotent(t *testing.T) {
	// Batches can overlap on retry; marking the same link twice in one
	// generation must not error or duplicate.
	repo := newRepo(t)
	if err := repo.UpsertTests("p1", []testrepo.TestCase{
		{Key: "QA-1", ID: "1", Summary: "Login test"},
	}); err != nil {
		t.Fatalf("upsert tests: %v", err)
	}
	if err := repo.UpsertPreconditions("p1", []testrepo.Precondition{
		{Key: "QA-P-1", Summary: "One"},
	}); err != nil {
		t.Fatalf("upsert preconditions: %v", err)
	}
	links := map[string][]string{"QA-1": {"QA-P-1"}}
	if err := repo.MarkTestPreconditions("p1", 100, links); err != nil {
		t.Fatalf("first mark: %v", err)
	}
	if err := repo.MarkTestPreconditions("p1", 100, links); err != nil {
		t.Fatalf("second mark: %v", err)
	}

	got, err := repo.ListTestPreconditions("p1", "QA-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d preconditions, want 1 (no duplicates)", len(got))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/testrepo/ -run 'TestSweepTestPreconditions|TestMarkTestPreconditions' -v`

Expected: compile failure, `repo.MarkTestPreconditions undefined` and `repo.SweepTestPreconditions undefined`.

- [ ] **Step 3: Add the schema column**

In `internal/store/store.go`, change line 21 from `const schemaVersion = 48` to:

```go
const schemaVersion = 49
```

In `baseSchema`, change the `test_precondition` table (currently lines 101-106) to:

```sql
CREATE TABLE IF NOT EXISTS test_precondition (
	profile_id       TEXT NOT NULL,
	test_key         TEXT NOT NULL,
	precondition_key TEXT NOT NULL,
	sync_gen         INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (profile_id, test_key, precondition_key)
);
```

In `applyMigrations`, after the v47 block (near line 1296), append:

```go
	// v49: sync_gen on test_precondition, so the precondition sync can
	// mark-and-sweep instead of wiping the table before it rewrites it
	// (RND_P_4TFINT_05-336). An interrupted pass must leave stale links
	// behind rather than delete valid ones. Existing rows default to
	// generation 0, older than any real generation, so the first sync after
	// upgrade sweeps them normally. Applied unconditionally; tolerated when
	// the column already exists.
	if _, err := db.Exec(
		`ALTER TABLE test_precondition ADD COLUMN sync_gen INTEGER NOT NULL DEFAULT 0`,
	); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("v49 add test_precondition.sync_gen: %w", err)
	}
```

- [ ] **Step 4: Replace the store method**

In `internal/testrepo/testrepo.go`, delete `ReplaceAllTestPreconditions` (lines 643-679) and put these two in its place:

```go
// MarkTestPreconditions upserts a batch of Test-to-Precondition links, stamping
// each with the current sync generation. Safe to call repeatedly during one
// pass: rows already present at this generation are left alone, and rows
// carried over from an earlier generation are re-stamped rather than
// duplicated. Nothing is deleted here — see SweepTestPreconditions.
func (r *Repository) MarkTestPreconditions(profileID string, gen int64, links map[string][]string) error {
	if len(links) == 0 {
		return nil
	}
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(
		`INSERT INTO test_precondition (profile_id, test_key, precondition_key, sync_gen)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(profile_id, test_key, precondition_key) DO UPDATE SET
		   sync_gen = excluded.sync_gen`)
	if err != nil {
		return fmt.Errorf("prepare mark link: %w", err)
	}
	defer stmt.Close()

	for testKey, preKeys := range links {
		for _, pk := range preKeys {
			if _, err := stmt.Exec(profileID, testKey, pk, gen); err != nil {
				return fmt.Errorf("mark link %s -> %s: %w", testKey, pk, err)
			}
		}
	}
	return tx.Commit()
}

// SweepTestPreconditions removes a profile's Test-to-Precondition links left
// over from an earlier sync generation, and returns how many it deleted. This
// is what makes removed links actually disappear on resync.
//
// Call it ONLY after a precondition pass completed cleanly. Sweeping after a
// partial pass would delete links the pass simply never reached, which is the
// defect this whole mechanism exists to prevent (RND_P_4TFINT_05-336).
func (r *Repository) SweepTestPreconditions(profileID string, gen int64) (int64, error) {
	res, err := r.db.Exec(
		`DELETE FROM test_precondition WHERE profile_id = ? AND sync_gen < ?`,
		profileID, gen,
	)
	if err != nil {
		return 0, fmt.Errorf("sweep precondition links: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("sweep precondition links rows: %w", err)
	}
	return n, nil
}
```

- [ ] **Step 5: Fix the one remaining caller so the package builds**

`internal/syncer/engine.go:478` still calls `ReplaceAllTestPreconditions`. Task 2 rewires this stage properly; for now make it compile and behave correctly by using a wall-clock generation:

```go
	gen := time.Now().UnixMilli()
	if err := e.repo.MarkTestPreconditions(profileID, gen, links); err != nil {
		return err
	}
	if _, err := e.repo.SweepTestPreconditions(profileID, gen); err != nil {
		return err
	}
	return nil
```

Confirm `time` is already imported in `engine.go`; it is, for the throttle constants.

- [ ] **Step 6: Run tests to verify they pass**

Run: `gofmt -w . && go build ./... && go test ./internal/testrepo/ ./internal/store/ ./internal/syncer/ -v`

Expected: PASS, including the four tests from Step 1.

- [ ] **Step 7: Commit**

```bash
git add internal/store/store.go internal/testrepo/testrepo.go internal/testrepo/testrepo_test.go internal/syncer/engine.go
git commit -m "fix(sync): mark-and-sweep precondition links instead of wipe-and-rewrite (-336)

An interrupted precondition pass used to DELETE every link for the profile
and then write nothing, leaving a new profile with an empty Preconditions
view. Links now carry a sync generation; the sweep of older generations runs
only after a pass completes cleanly, so a partial run leaves stale links
rather than destroying valid ones.

Schema v49."
```

---

### Task 2: Stream preconditions in batches

Persist as the pass runs rather than only at the end, so an interrupted sync keeps what it already fetched.

**Files:**
- Modify: `internal/backend/backend.go` (add `PreconditionStreamer` near the `Backend` interface at `:152`)
- Modify: `internal/jira/preconditions.go:94-137` (add `ListPreconditionsStream`, reimplement `ListPreconditions` over it)
- Modify: `internal/backend/xray/adapter.go:254` (implement the streamer)
- Modify: `internal/syncer/engine.go:453-479` (use the streamer when available)
- Test: `internal/jira/preconditions_test.go`, `internal/syncer/engine_test.go`

**Interfaces:**
- Consumes: `MarkTestPreconditions` and `SweepTestPreconditions` from Task 1.
- Produces:
  - `backend.PreconditionStreamer` interface with `ListPreconditionsStream(ctx context.Context, projectKey string, onProgress func(done, total int), onBatch func(pre []Precondition, links map[string][]string) error) error`
  - `func (c *jira.Client) ListPreconditionsStream(ctx context.Context, projectKey string, onProgress func(done, total int), onBatch func(pre []Precondition, links map[string][]string) error) error`
  - `const jira.preconditionBatchSize = 200`
  - `jira.ListPreconditions` keeps its exact current signature and behavior.

- [ ] **Step 1: Write the failing tests**

Add to `internal/jira/preconditions_test.go`:

```go
func TestListPreconditionsStreamEmitsBatches(t *testing.T) {
	// 450 preconditions at a batch size of 200 must arrive as 200/200/50,
	// so an interrupted pass has already persisted the earlier batches.
	srv := newPreconditionServer(t, 450)
	defer srv.Close()
	c := newTestClient(t, srv.URL)

	var sizes []int
	var totalPre int
	err := c.ListPreconditionsStream(context.Background(), "QA", nil,
		func(pre []jira.Precondition, links map[string][]string) error {
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

func TestListPreconditionsStreamAbortsOnBatchError(t *testing.T) {
	// A store failure inside onBatch must stop the pass, not be swallowed.
	srv := newPreconditionServer(t, 450)
	defer srv.Close()
	c := newTestClient(t, srv.URL)

	sentinel := errors.New("store is down")
	calls := 0
	err := c.ListPreconditionsStream(context.Background(), "QA", nil,
		func(pre []jira.Precondition, links map[string][]string) error {
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
```

`newPreconditionServer(t, n)` and `newTestClient(t, url)` are helpers. `preconditions_test.go` already builds an `httptest` server for `TestListPreconditionsResolvesTypeAndPaginates` (`:23`); extract that setup into `newPreconditionServer(t *testing.T, count int) *httptest.Server` taking the precondition count, and reuse the existing client construction as `newTestClient`. Add `"errors"` to the test file's imports.

Add to `internal/syncer/engine_test.go`:

```go
func TestSyncPreconditionsPersistsEachBatch(t *testing.T) {
	// The -336 regression at the engine level: a backend that streams three
	// batches and then fails must leave the first two batches in the store.
	repo, engine := newStreamingPreconditionEngine(t, 3 /* batches */, 2 /* fail on batch index */)

	err := engine.Sync(context.Background(), "p1", "QA", nil)
	if err == nil {
		t.Fatal("expected the sync to report the precondition failure")
	}

	pre, err := repo.ListPreconditionsWithUsage("p1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(pre) == 0 {
		t.Fatal("no preconditions persisted; batches before the failure must survive")
	}
}

func TestSyncPreconditionsFallsBackForNonStreamingBackend(t *testing.T) {
	// Kiwi does not implement PreconditionStreamer. It must still sync.
	repo, engine := newNonStreamingPreconditionEngine(t)

	if err := engine.Sync(context.Background(), "p1", "QA", nil); err != nil {
		t.Fatalf("sync: %v", err)
	}
	pre, err := repo.ListPreconditionsWithUsage("p1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(pre) != 1 {
		t.Fatalf("got %d preconditions, want 1 through the fallback path", len(pre))
	}
}
```

Build `newStreamingPreconditionEngine` and `newNonStreamingPreconditionEngine` as fake-backend constructors in the test file, following the fake-backend pattern already used elsewhere in `engine_test.go`. The streaming fake implements both `Backend` and `PreconditionStreamer`; the non-streaming fake implements only `Backend`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/jira/ ./internal/syncer/ -run Precondition -v`

Expected: compile failure, `c.ListPreconditionsStream undefined`.

- [ ] **Step 3: Add the optional backend interface**

In `internal/backend/backend.go`, below the `Backend` interface, add:

```go
// PreconditionStreamer is implemented by backends that can report preconditions
// incrementally instead of only at the end of a full pass.
//
// The Xray precondition pass costs one HTTP round trip per precondition, so a
// project with thousands of them runs for minutes. Buffering the whole result
// meant an interruption anywhere in that window persisted nothing
// (RND_P_4TFINT_05-336). A backend that implements this lets the syncer commit
// as it goes.
//
// It is deliberately NOT part of Backend: backends whose preconditions arrive
// in one cheap call gain nothing from it, and the syncer falls back to
// Backend.ListPreconditions when the assertion fails.
type PreconditionStreamer interface {
	ListPreconditionsStream(
		ctx context.Context,
		projectKey string,
		onProgress func(done, total int),
		onBatch func(pre []Precondition, links map[string][]string) error,
	) error
}
```

- [ ] **Step 4: Implement streaming in the Jira client**

In `internal/jira/preconditions.go`, add the batch size constant beside the existing throttle constant:

```go
// preconditionBatchSize is how many preconditions are accumulated before being
// handed to the caller's onBatch. Small enough that an interrupted sync loses
// little, large enough that the store write is not per-item.
const preconditionBatchSize = 200
```

Replace `ListPreconditions` (lines 94-137) with a wrapper plus the streaming implementation:

```go
func (c *Client) ListPreconditions(ctx context.Context, projectKey string, onProgress func(done, total int)) ([]Precondition, map[string][]string, error) {
	allPre := []Precondition{}
	allLinks := map[string][]string{}
	err := c.ListPreconditionsStream(ctx, projectKey, onProgress,
		func(pre []Precondition, links map[string][]string) error {
			allPre = append(allPre, pre...)
			for tk, pks := range links {
				allLinks[tk] = append(allLinks[tk], pks...)
			}
			return nil
		})
	if err != nil {
		return nil, nil, err
	}
	return allPre, allLinks, nil
}

// ListPreconditionsStream walks a project's Preconditions and reports them to
// onBatch in chunks of preconditionBatchSize as they are resolved, so a caller
// can persist incrementally. onProgress (optional) is called once per
// precondition as its associated Tests are read, the slow part of the pass.
//
// A non-nil error from onBatch aborts the walk and is returned wrapped, so a
// store failure cannot be silently absorbed.
func (c *Client) ListPreconditionsStream(
	ctx context.Context,
	projectKey string,
	onProgress func(done, total int),
	onBatch func(pre []Precondition, links map[string][]string) error,
) error {
	if isDemoURL(c.baseURL) {
		pre, links, err := demoPreconditionsAndLinks(themeFor(c.baseURL), projectKey)
		if err != nil {
			return err
		}
		return onBatch(pre, links)
	}

	typeID, typeName, err := c.resolvePreconditionType(ctx)
	if err != nil {
		return fmt.Errorf("resolve precondition issue type: %w", err)
	}
	if typeID == "" {
		log.Printf("xtm: no Precondition issue type on this instance — skipping precondition sync")
		return nil
	}

	preconditions, err := c.searchPreconditions(ctx, projectKey, typeID)
	if err != nil {
		return fmt.Errorf("search preconditions: %w", err)
	}

	total := len(preconditions)
	for start := 0; start < total; start += preconditionBatchSize {
		end := start + preconditionBatchSize
		if end > total {
			end = total
		}
		chunk := preconditions[start:end]

		// links: test key -> precondition keys, for this chunk only.
		links := map[string][]string{}
		for i, p := range chunk {
			if err := ctx.Err(); err != nil {
				return err
			}
			testKeys, err := c.listPreconditionTests(ctx, p.Key)
			if err != nil {
				log.Printf("xtm: precondition %s tests: %v", p.Key, err)
			} else {
				for _, tk := range testKeys {
					links[tk] = append(links[tk], p.Key)
				}
			}
			if onProgress != nil {
				onProgress(start+i+1, total)
			}
			time.Sleep(throttlePreconditions)
		}
		if err := onBatch(chunk, links); err != nil {
			return fmt.Errorf("persist precondition batch: %w", err)
		}
	}
	log.Printf("xtm: preconditions: %d found (type %q) for %s", total, typeName, projectKey)
	return nil
}
```

The `time.Sleep` stays for now. Task 3 removes it along with the sequential loop.

- [ ] **Step 5: Implement the streamer on the Xray adapter**

In `internal/backend/xray/adapter.go`, beside `ListPreconditions` (`:254`), add:

```go
// ListPreconditionsStream satisfies backend.PreconditionStreamer by delegating
// to the Jira client's streaming walk, translating each batch into the neutral
// backend types.
func (a *Adapter) ListPreconditionsStream(
	ctx context.Context,
	projectKey string,
	onProgress func(done, total int),
	onBatch func(pre []backend.Precondition, links map[string][]string) error,
) error {
	return a.client.ListPreconditionsStream(ctx, projectKey, onProgress,
		func(jp []jira.Precondition, links map[string][]string) error {
			out := make([]backend.Precondition, len(jp))
			for i, p := range jp {
				out[i] = backend.Precondition{
					Key:         p.Key,
					Summary:     p.Summary,
					Type:        p.Type,
					Description: p.Description,
					Condition:   p.Condition,
				}
			}
			return onBatch(out, links)
		})
}
```

Match the field mapping to whatever `Adapter.ListPreconditions` at `:254` already does; if it maps fewer or differently named fields, copy that mapping exactly rather than the list above.

- [ ] **Step 6: Wire the engine to stream**

Replace the body of `syncPreconditions` in `internal/syncer/engine.go` (currently `:453-479`):

```go
func (e *Engine) syncPreconditions(ctx context.Context, profileID, projectKey string, onProgress func(Progress)) error {
	gen := time.Now().UnixMilli()
	progress := func(done, total int) {
		if onProgress != nil {
			onProgress(Progress{Phase: "preconditions", Stage: "Syncing preconditions", Fetched: done, Total: total})
		}
	}

	// persist commits one batch: the precondition rows and their links, both
	// stamped with this pass's generation. Nothing is deleted here; the sweep
	// below runs only if the whole pass succeeds.
	persist := func(pre []backend.Precondition, links map[string][]string) error {
		repoPre := make([]testrepo.Precondition, len(pre))
		for i, p := range pre {
			repoPre[i] = testrepo.Precondition{
				Key:         p.Key,
				Summary:     p.Summary,
				Type:        p.Type,
				Description: p.Description,
				Condition:   p.Condition,
			}
		}
		if err := e.repo.UpsertPreconditions(profileID, repoPre); err != nil {
			return err
		}
		return e.repo.MarkTestPreconditions(profileID, gen, links)
	}

	if s, ok := e.backend.(backend.PreconditionStreamer); ok {
		if err := s.ListPreconditionsStream(ctx, projectKey, progress, persist); err != nil {
			return fmt.Errorf("list preconditions: %w", err)
		}
	} else {
		// Backends without incremental support (Kiwi) still sync, just in one
		// shot at the end.
		pre, links, err := e.backend.ListPreconditions(ctx, projectKey, progress)
		if err != nil {
			return fmt.Errorf("list preconditions: %w", err)
		}
		if err := persist(pre, links); err != nil {
			return err
		}
	}

	if _, err := e.repo.SweepTestPreconditions(profileID, gen); err != nil {
		return err
	}
	return nil
}
```

Note what changed beyond streaming: the old `if len(preconditions) == 0 && len(links) == 0 { return nil }` guard is gone. It existed to stop an empty result wiping the table, which mark-and-sweep now handles. Task 4 restores the distinction it was standing in for, between "no precondition type on this instance" and "the pass failed".

- [ ] **Step 7: Run tests to verify they pass**

Run: `gofmt -w . && go build ./... && go test ./internal/jira/ ./internal/syncer/ ./internal/backend/... -v`

Expected: PASS, including both new `preconditions_test.go` tests, both new `engine_test.go` tests, and the pre-existing `TestListPreconditionsResolvesTypeAndPaginates` and `TestListPreconditionsNoTypeIsSoft` unchanged.

- [ ] **Step 8: Commit**

```bash
git add internal/backend/backend.go internal/backend/xray/adapter.go internal/jira/preconditions.go internal/jira/preconditions_test.go internal/syncer/engine.go internal/syncer/engine_test.go
git commit -m "fix(sync): persist preconditions in batches as the pass runs (-336)

A precondition pass over thousands of items runs for minutes and used to
hold everything in memory until the end, so any interruption saved nothing.
Backends can now implement the optional PreconditionStreamer interface to
report batches of 200 as they resolve, which the engine commits per batch.
Backends without it (Kiwi) fall back to the existing one-shot path."
```

---

### Task 3: Concurrency, and deleting the sleep

**Files:**
- Modify: `internal/jira/preconditions.go` (remove `throttlePreconditions`, add concurrency and retry)
- Test: `internal/jira/preconditions_test.go`

**Interfaces:**
- Consumes: `ListPreconditionsStream` from Task 2.
- Produces: `const jira.preconditionFetchConcurrency = 8`. No signature changes.

- [ ] **Step 1: Write the failing tests**

Add to `internal/jira/preconditions_test.go`:

```go
func TestListPreconditionsStreamFetchesConcurrently(t *testing.T) {
	// The old walk slept 150ms per precondition, so 40 items took 6 seconds.
	// With concurrency and no sleep this must finish well inside a second.
	srv := newPreconditionServer(t, 40)
	defer srv.Close()
	c := newTestClient(t, srv.URL)

	start := time.Now()
	err := c.ListPreconditionsStream(context.Background(), "QA", nil,
		func(pre []jira.Precondition, links map[string][]string) error { return nil })
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("took %v for 40 preconditions, want well under 2s (is the sleep still there?)", elapsed)
	}
}

func TestListPreconditionsStreamPreservesOrderUnderConcurrency(t *testing.T) {
	// Batches must stay in key order regardless of which goroutine finishes
	// first, so progress and logs stay deterministic.
	srv := newPreconditionServer(t, 250)
	defer srv.Close()
	c := newTestClient(t, srv.URL)

	var keys []string
	err := c.ListPreconditionsStream(context.Background(), "QA", nil,
		func(pre []jira.Precondition, links map[string][]string) error {
			for _, p := range pre {
				keys = append(keys, p.Key)
			}
			return nil
		})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if len(keys) != 250 {
		t.Fatalf("got %d keys, want 250", len(keys))
	}
	if !sort.StringsAreSorted(keys) {
		t.Error("precondition keys arrived out of order")
	}
}

func TestListPreconditionTestsRetriesOn401(t *testing.T) {
	// The live instance intermittently 401s a single association read with a
	// token that worked moments earlier. One retry must recover it.
	var hits int32
	srv := newPreconditionServerWithAssocHandler(t, 1, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Client must be authenticated to access this resource."}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"key":"QA-1"}]`))
	})
	defer srv.Close()
	c := newTestClient(t, srv.URL)

	var got map[string][]string
	err := c.ListPreconditionsStream(context.Background(), "QA", nil,
		func(pre []jira.Precondition, links map[string][]string) error {
			got = links
			return nil
		})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if len(got["QA-1"]) != 1 {
		t.Errorf("got %v, want the link recovered after the 401 retry", got)
	}
	if atomic.LoadInt32(&hits) < 2 {
		t.Errorf("association endpoint hit %d times, want at least 2 (no retry happened)", hits)
	}
}
```

Extend the test helper into `newPreconditionServerWithAssocHandler(t *testing.T, count int, assoc http.HandlerFunc) *httptest.Server`, and define `newPreconditionServer` in terms of it with a default handler. Add `"net/http"`, `"sort"`, `"sync/atomic"`, and `"time"` to the test imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/jira/ -run 'Concurrent|PreservesOrder|RetriesOn401' -v`

Expected: the timing test FAILs (40 × 150ms is 6 seconds) and the retry test FAILs (no retry, so the link is dropped).

- [ ] **Step 3: Add retry to the association read**

In `internal/jira/preconditions.go`, replace the `throttlePreconditions` constant with:

```go
// preconditionFetchConcurrency bounds how many per-precondition association
// reads run at once. The shared client rate limiter (Client.do) caps the actual
// request rate at syncReqPerSec; this just keeps several requests in flight so
// the limiter stays fed, replacing the old one-at-a-time-with-a-sleep walk that
// spent 15 minutes asleep on a 6000-precondition project.
const preconditionFetchConcurrency = 8

// preconditionRetries is how many times a single association read is retried.
// The live Xray instance intermittently answers 401 with a token that worked
// moments earlier, and times out on the same endpoint; both drop that
// precondition's links silently. This is mitigation, not a root-cause fix.
const preconditionRetries = 3
```

Add the retrying wrapper beside `listPreconditionTests`:

```go
// listPreconditionTestsRetrying calls listPreconditionTests, retrying with
// exponential backoff. Returns the last error if every attempt fails.
func (c *Client) listPreconditionTestsRetrying(ctx context.Context, key string) ([]string, error) {
	var lastErr error
	backoff := 500 * time.Millisecond
	for attempt := 0; attempt < preconditionRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
		}
		testKeys, err := c.listPreconditionTests(ctx, key)
		if err == nil {
			return testKeys, nil
		}
		lastErr = err
	}
	return nil, lastErr
}
```

- [ ] **Step 4: Replace the sequential chunk loop with a worker pool**

In `ListPreconditionsStream`, replace the inner `for i, p := range chunk { ... }` loop with the concurrent version, following the pattern at `containers.go:158`:

```go
		// Resolve this chunk's associations concurrently, paced by the shared
		// client rate limiter. Results are collected per index so the output
		// stays in key order regardless of goroutine completion order.
		perPre := make([][]string, len(chunk))
		var progMu sync.Mutex // onProgress may not be concurrency-safe
		sem := make(chan struct{}, preconditionFetchConcurrency)
		var wg sync.WaitGroup
		for i, p := range chunk {
			if ctx.Err() != nil {
				break
			}
			i, p := i, p
			sem <- struct{}{}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				if ctx.Err() != nil {
					return
				}
				testKeys, err := c.listPreconditionTestsRetrying(ctx, p.Key)
				if err != nil {
					log.Printf("xtm: precondition %s tests: %v", p.Key, err)
					atomic.AddInt64(&dropped, 1)
				} else {
					perPre[i] = testKeys
				}
				if onProgress != nil {
					n := atomic.AddInt64(&done, 1)
					progMu.Lock()
					onProgress(int(n), total)
					progMu.Unlock()
				}
			}()
		}
		wg.Wait()
		if err := ctx.Err(); err != nil {
			return err
		}

		links := map[string][]string{}
		for i, testKeys := range perPre {
			for _, tk := range testKeys {
				links[tk] = append(links[tk], chunk[i].Key)
			}
		}
```

Declare `var done, dropped int64` once before the outer chunk loop, so progress counts across chunks and the drop count covers the whole pass. Log the drop count beside the existing summary line:

```go
	if dropped > 0 {
		log.Printf("xtm: preconditions: %d of %d had unreadable test links for %s", dropped, total, projectKey)
	}
```

Add `"sync"` and `"sync/atomic"` to the file's imports. `time` stays, now used only by the retry backoff.

- [ ] **Step 5: Run tests to verify they pass**

Run: `gofmt -w . && go build ./... && go test ./internal/jira/ ./internal/syncer/ -v`

Expected: PASS. The 40-precondition timing test should now complete in well under a second.

- [ ] **Step 6: Commit**

```bash
git add internal/jira/preconditions.go internal/jira/preconditions_test.go
git commit -m "perf(sync): fetch precondition associations concurrently, drop the fixed sleep (-336)

The stage slept 150ms per precondition on top of the shared token-bucket
limiter that already paces every request, which is 15 minutes of dead time
on a 6000-precondition project. Replaced with 8-way bounded concurrency,
matching the container sync. Association reads now retry with backoff, since
the live instance intermittently 401s a read whose token works moments
later and silently drops that precondition's links."
```

---

### Task 4: Run preconditions before the folder walk

**Files:**
- Modify: `internal/syncer/engine.go:171-190` (stage order)
- Test: `internal/syncer/engine_test.go`

**Interfaces:**
- Consumes: the reworked `syncPreconditions` from Tasks 2 and 3.
- Produces: nothing new; ordering only.

- [ ] **Step 1: Write the failing test**

Add to `internal/syncer/engine_test.go`:

```go
func TestSyncRunsPreconditionsBeforeFolders(t *testing.T) {
	// Preconditions sit behind a full folder walk today, so on a first sync
	// they are the last thing to start and the first thing to be cut short.
	// They depend only on tests existing, so they belong right after the pull.
	var order []string
	repo, engine := newOrderRecordingEngine(t, &order)

	if err := engine.Sync(context.Background(), "p1", "QA", nil); err != nil {
		t.Fatalf("sync: %v", err)
	}
	_ = repo

	idxOf := func(stage string) int {
		for i, s := range order {
			if s == stage {
				return i
			}
		}
		t.Fatalf("stage %q never ran; got %v", stage, order)
		return -1
	}
	if idxOf("preconditions") > idxOf("folders") {
		t.Errorf("preconditions ran after folders; got order %v", order)
	}
	if idxOf("tests") > idxOf("preconditions") {
		t.Errorf("preconditions ran before the test pull; got order %v", order)
	}
}
```

`newOrderRecordingEngine(t, &order)` builds an engine over a fake backend whose methods append their stage name to the slice. Follow the fake-backend pattern already in `engine_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/syncer/ -run TestSyncRunsPreconditionsBeforeFolders -v`

Expected: FAIL, `preconditions ran after folders`.

- [ ] **Step 3: Move the stage**

In `internal/syncer/engine.go` `Sync()`, move the precondition block (currently `:182-185`) to sit immediately after the test pull and before the folder stage. The resulting order is: tests, preconditions, folders, requirements, bugs, containers, custom fields, project field options, `SetSyncState`.

Update the comment at `:171-175`, which currently explains only why folders must follow the test pull, to record that preconditions have the same dependency and now run first of the two:

```go
	// Stage order matters. The test pull must come first: folder membership and
	// precondition links are both keyed by test, so either running before tests
	// exist silently maps nothing (an earlier first-sync bug). Preconditions run
	// ahead of the folder walk because they are by far the longest stage and
	// were previously starved on a first sync (RND_P_4TFINT_05-336).
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `gofmt -w . && go build ./... && go test ./internal/syncer/ -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/syncer/engine.go internal/syncer/engine_test.go
git commit -m "fix(sync): run the precondition stage before the folder walk (-336)

Preconditions are the longest stage and sat third, behind a full folder
walk, so on a first sync they were the last to start and the first to be
cut short. They depend only on tests existing, so they move to second."
```

---

### Task 5: Stop swallowing the failure

**Files:**
- Modify: `internal/store/store.go` (`sync_log.stage_failures` column, v49 migration block)
- Modify: `internal/testrepo/synclog.go` (record and read stage failures)
- Modify: `internal/syncer/engine.go` (stage outcome, skip the sweep on a partial pass)
- Modify: `app.go` (surface it), `frontend/src/components/SyncHistoryModal.tsx` (render it)
- Test: `internal/testrepo/synclog_test.go`, `internal/syncer/engine_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1 through 4.
- Produces:
  - `testrepo.SyncLogEntry` gains `StageFailures []StageFailure \`json:"stageFailures"\``
  - `type testrepo.StageFailure struct { Stage string \`json:"stage"\`; Message string \`json:"message"\` }`
  - `RecordSyncLog` gains a trailing `stageFailures []StageFailure` parameter.
  - `Outcome` gains the value `"partial"` alongside the existing `"success"` and `"error"`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/testrepo/synclog_test.go` (create the file if absent, package `testrepo_test`):

```go
func TestRecordSyncLogRoundTripsStageFailures(t *testing.T) {
	repo := newRepo(t)
	want := []testrepo.StageFailure{
		{Stage: "preconditions", Message: "context deadline exceeded"},
	}
	if err := repo.RecordSyncLog("p1", "2026-08-20T10:00:00Z", "2026-08-20T10:05:00Z", "partial", 120, "", want); err != nil {
		t.Fatalf("record: %v", err)
	}

	got, err := repo.ListSyncLog("p1", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	if got[0].Outcome != "partial" {
		t.Errorf("outcome %q, want %q", got[0].Outcome, "partial")
	}
	if len(got[0].StageFailures) != 1 || got[0].StageFailures[0].Stage != "preconditions" {
		t.Errorf("got stage failures %+v, want %+v", got[0].StageFailures, want)
	}
}

func TestListSyncLogTolueratesLegacyRowsWithNoStageFailures(t *testing.T) {
	// Rows written before v49 have an empty stage_failures column.
	repo := newRepo(t)
	if err := repo.RecordSyncLog("p1", "2026-08-20T10:00:00Z", "2026-08-20T10:05:00Z", "success", 120, "", nil); err != nil {
		t.Fatalf("record: %v", err)
	}
	got, err := repo.ListSyncLog("p1", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got[0].StageFailures) != 0 {
		t.Errorf("got %+v, want no stage failures", got[0].StageFailures)
	}
}
```

Add to `internal/syncer/engine_test.go`:

```go
func TestSyncReportsPartialWhenPreconditionStageFails(t *testing.T) {
	// The -336 headline: a failed precondition stage used to be logged and
	// dropped, so the sync stamped its watermark and reported success.
	repo, engine := newFailingPreconditionEngine(t)

	err := engine.Sync(context.Background(), "p1", "QA", nil)
	if err == nil {
		t.Fatal("sync reported clean despite the precondition stage failing")
	}

	logs, lerr := repo.ListSyncLog("p1", 10)
	if lerr != nil {
		t.Fatalf("list sync log: %v", lerr)
	}
	if len(logs) == 0 || logs[0].Outcome != "partial" {
		t.Fatalf("got outcome %v, want a partial entry", logs)
	}
	if len(logs[0].StageFailures) == 0 || logs[0].StageFailures[0].Stage != "preconditions" {
		t.Errorf("got %+v, want a preconditions stage failure", logs[0].StageFailures)
	}
}

func TestFailedPreconditionStageSkipsTheSweep(t *testing.T) {
	// The sweep must not run after a partial pass, or it deletes links the
	// pass never reached.
	repo, engine := newPartialPreconditionEngine(t)

	// Seed a link from an earlier successful sync.
	if err := repo.UpsertTests("p1", []testrepo.TestCase{{Key: "QA-1", ID: "1", Summary: "T"}}); err != nil {
		t.Fatalf("seed tests: %v", err)
	}
	if err := repo.UpsertPreconditions("p1", []testrepo.Precondition{{Key: "QA-P-9", Summary: "Old"}}); err != nil {
		t.Fatalf("seed preconditions: %v", err)
	}
	if err := repo.MarkTestPreconditions("p1", 1, map[string][]string{"QA-1": {"QA-P-9"}}); err != nil {
		t.Fatalf("seed link: %v", err)
	}

	_ = engine.Sync(context.Background(), "p1", "QA", nil)

	got, err := repo.ListTestPreconditions("p1", "QA-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d links, want the pre-existing link preserved after a partial pass", len(got))
	}
}

func TestNoPreconditionTypeIsNotAFailure(t *testing.T) {
	// An instance with no Precondition issue type is benign, not partial.
	repo, engine := newNoPreconditionTypeEngine(t)

	if err := engine.Sync(context.Background(), "p1", "QA", nil); err != nil {
		t.Fatalf("sync: %v", err)
	}
	logs, err := repo.ListSyncLog("p1", 10)
	if err != nil {
		t.Fatalf("list sync log: %v", err)
	}
	if len(logs) == 0 || logs[0].Outcome != "success" {
		t.Errorf("got %v, want a clean success", logs)
	}
}
```

Build `newFailingPreconditionEngine`, `newPartialPreconditionEngine`, and `newNoPreconditionTypeEngine` as fake-backend constructors: the first returns an error from the stream immediately, the second errors after one successful batch, the third streams nothing and returns nil (the no-type case, which `ListPreconditionsStream` signals by returning nil having called `onBatch` zero times).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/testrepo/ ./internal/syncer/ -run 'StageFailure|Partial|NoPreconditionType|SkipsTheSweep' -v`

Expected: compile failure, `RecordSyncLog` takes 6 arguments not 7, and `testrepo.StageFailure` undefined.

- [ ] **Step 3: Add the column**

In `internal/store/store.go`, add `stage_failures` to the `sync_log` CREATE in `baseSchema` (line 201):

```sql
CREATE TABLE IF NOT EXISTS sync_log (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	profile_id     TEXT NOT NULL,
	started_at     TEXT NOT NULL,
	finished_at    TEXT NOT NULL DEFAULT '',
	outcome        TEXT NOT NULL DEFAULT '',
	fetched        INTEGER NOT NULL DEFAULT 0,
	error          TEXT NOT NULL DEFAULT '',
	stage_failures TEXT NOT NULL DEFAULT ''
);
```

And in `applyMigrations`, beside the `sync_gen` ALTER added in Task 1:

```go
	// v49: stage_failures on sync_log — a JSON array of {stage, message} for
	// best-effort stages that errored without failing the whole sync. Until
	// now a failed precondition stage was logged and dropped, so the sync
	// stamped its watermark and reported success (RND_P_4TFINT_05-336).
	// Applied unconditionally; tolerated when the column already exists.
	if _, err := db.Exec(
		`ALTER TABLE sync_log ADD COLUMN stage_failures TEXT NOT NULL DEFAULT ''`,
	); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("v49 add sync_log.stage_failures: %w", err)
	}
```

- [ ] **Step 4: Record and read stage failures**

Rewrite `internal/testrepo/synclog.go`:

```go
package testrepo

import (
	"encoding/json"
	"fmt"
)

// StageFailure records one best-effort sync stage that errored without
// aborting the whole run.
type StageFailure struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

// SyncLogEntry records the outcome of one sync run (FR-1.7).
type SyncLogEntry struct {
	ID         int64  `json:"id"`
	StartedAt  string `json:"startedAt"`
	FinishedAt string `json:"finishedAt"`
	// Outcome is "success", "partial" or "error". "partial" means the run
	// finished and its data is usable, but at least one stage did not
	// complete — see StageFailures.
	Outcome       string         `json:"outcome"`
	Fetched       int            `json:"fetched"`
	Error         string         `json:"error"`
	StageFailures []StageFailure `json:"stageFailures"`
}

// RecordSyncLog appends a sync run's outcome to the history (FR-1.7).
func (r *Repository) RecordSyncLog(profileID, startedAt, finishedAt, outcome string, fetched int, errMsg string, stageFailures []StageFailure) error {
	encoded := ""
	if len(stageFailures) > 0 {
		b, err := json.Marshal(stageFailures)
		if err != nil {
			return fmt.Errorf("encode stage failures: %w", err)
		}
		encoded = string(b)
	}
	if _, err := r.db.Exec(
		`INSERT INTO sync_log (profile_id, started_at, finished_at, outcome, fetched, error, stage_failures)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		profileID, startedAt, finishedAt, outcome, fetched, errMsg, encoded,
	); err != nil {
		return fmt.Errorf("record sync log: %w", err)
	}
	return nil
}

// ListSyncLog returns a profile's most recent sync runs, newest first. A limit
// ≤ 0 or > 200 defaults to 50.
func (r *Repository) ListSyncLog(profileID string, limit int) ([]SyncLogEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.Query(
		`SELECT id, started_at, finished_at, outcome, fetched, error, stage_failures
		 FROM sync_log WHERE profile_id = ?
		 ORDER BY started_at DESC, id DESC LIMIT ?`,
		profileID, limit)
	if err != nil {
		return nil, fmt.Errorf("list sync log: %w", err)
	}
	defer rows.Close()

	out := []SyncLogEntry{}
	for rows.Next() {
		var e SyncLogEntry
		var encoded string
		if err := rows.Scan(&e.ID, &e.StartedAt, &e.FinishedAt, &e.Outcome, &e.Fetched, &e.Error, &encoded); err != nil {
			return nil, err
		}
		// Rows written before v49 carry an empty column, not "[]".
		if encoded != "" {
			if err := json.Unmarshal([]byte(encoded), &e.StageFailures); err != nil {
				return nil, fmt.Errorf("decode stage failures: %w", err)
			}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
```

- [ ] **Step 5: Make the engine report the failure**

Two changes in `internal/syncer/engine.go`.

First, `syncPreconditions` must distinguish a benign skip from a real failure and must not sweep after a partial pass. Change the ending of the function written in Task 2:

```go
	// batches counts how many times persist actually ran. Zero means the
	// instance has no Precondition issue type (ListPreconditionsStream returns
	// nil without calling onBatch), which is benign: skip the sweep, since
	// there was no pass to reconcile against.
	batches := 0
```

Increment `batches` inside `persist`, and replace the trailing sweep with:

```go
	if batches == 0 {
		return nil
	}
	if _, err := e.repo.SweepTestPreconditions(profileID, gen); err != nil {
		return err
	}
	return nil
```

The sweep is now unreachable on any error path, because every error return above it is a plain `return err`.

Second, in `Sync()`, replace the swallowing call site with one that records the failure:

```go
	var stageFailures []testrepo.StageFailure
	emitStage(onProgress, "Syncing preconditions")
	if err := e.syncPreconditions(ctx, profileID, projectKey, onProgress); err != nil {
		log.Printf("xtm: precondition sync failed: %v", err)
		stageFailures = append(stageFailures, testrepo.StageFailure{
			Stage:   "preconditions",
			Message: err.Error(),
		})
	}
```

Thread `stageFailures` to wherever `Sync` calls `RecordSyncLog`, passing it as the new trailing argument, and choose the outcome:

```go
	outcome := "success"
	if len(stageFailures) > 0 {
		outcome = "partial"
	}
```

`Sync` returns a non-nil error when `stageFailures` is non-empty, so the caller and the UI both see it:

```go
	if len(stageFailures) > 0 {
		return fmt.Errorf("sync completed with %d failed stage(s): %s",
			len(stageFailures), stageFailures[0].Message)
	}
```

Update the other `RecordSyncLog` call sites (the error paths) to pass `nil` for the new parameter. Find them with `grep -rn "RecordSyncLog" --include=*.go .`

- [ ] **Step 6: Surface it in the UI**

In `frontend/src/components/SyncHistoryModal.tsx`, the sync history rows render `outcome`. Add the `partial` case: render it with a warning tone rather than the success tone, and below the row render each `stageFailures` entry as `${stage}: ${message}`. Follow whatever badge or status-pill markup the component already uses for `success` and `error`; do not invent a new visual language.

Regenerate the Wails bindings so `StageFailure` and the widened `SyncLogEntry` reach TypeScript:

```bash
wails build
```

Then confirm `frontend/wailsjs/go/models.ts` contains `StageFailure`. Do not hand-edit anything under `frontend/wailsjs/`.

- [ ] **Step 7: Run tests to verify they pass**

Run: `gofmt -w . && go build ./... && go test ./... && cd frontend && npm run build`

Expected: all Go tests PASS, and the frontend typechecks and builds.

- [ ] **Step 8: Commit**

```bash
git add internal/store/store.go internal/testrepo/synclog.go internal/testrepo/synclog_test.go internal/syncer/engine.go internal/syncer/engine_test.go app.go frontend/src/components/SyncHistoryModal.tsx frontend/wailsjs
git commit -m "fix(sync): report a partial sync when the precondition stage fails (-336)

The stage was best-effort: its error was logged and dropped, so the run
stamped its watermark and reported success while the Preconditions view sat
empty. Failures are now recorded on sync_log as stage_failures with a new
'partial' outcome, surfaced in sync history, and the sweep is skipped so a
partial pass cannot delete links it never reached. An instance with no
Precondition issue type stays benign and reports success."
```

---

## Self-review notes

- Spec section 1.1 is Task 1; 1.2 is Task 2; 1.3 is Task 3; 1.4 is Task 4; 1.5 is Task 5; 1.6's test list is distributed across all five.
- The spec's "four commits" became five tasks: stage ordering (1.4) was split out of the visible-failure work because a reviewer could reasonably accept one and reject the other.
- `ReplaceAllTestPreconditions` is removed in Task 1 and its caller is patched in the same task, so every task ends with a building tree.
- Task 5's `batches == 0` check is the replacement for the `len(preconditions) == 0 && len(links) == 0` guard that Task 2 removes. Both facts are stated in the tasks that touch them.
