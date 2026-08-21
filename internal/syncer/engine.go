// Package syncer pulls Xray data from Jira into the local store (FR-1).
package syncer

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"xray-test-manager/internal/backend"
	"xray-test-manager/internal/testrepo"
)

// Bounded concurrency for the container-sync fetch passes. The backend's shared
// rate limiter caps the actual request rate; these bounds just let several
// fetches be in flight so the limiter stays fed instead of a serial walk. DB
// writes are always applied serially after each concurrent fetch phase, so the
// local store is never written from multiple goroutines at once.
const (
	execFetchConcurrency    = 8 // per-execution runs + exec-plan fetches
	crossProjectConcurrency = 8 // per-test cross-project execution discovery
)

// pageSize is the Jira search page size. Jira DC commonly caps maxResults at
// 100 for /search; the engine pages until the reported total is reached.
const pageSize = 100

// throttle is the pause between pages — keeps a large sync within Jira DC's
// default rate limit (FR-1.8 / Q11).
const throttle = 200 * time.Millisecond

// Progress reports sync advancement to the caller, which forwards it to the UI.
// Phase distinguishes the work being reported ("" / "tests" = the Test pull,
// "folders" = the Test Repository membership pass) so the UI can label a phase
// that would otherwise look stalled because it isn't fetching Test pages.
type Progress struct {
	Phase   string `json:"phase"`
	Fetched int    `json:"fetched"`
	Total   int    `json:"total"`
	Done    bool   `json:"done"`
	// Stage is a human-readable label for the running phase ("Fetching tests",
	// "Mapping folder membership", "Syncing containers", …) so the UI can show
	// the user which step a sync is on — including the best-effort tail work
	// (folders / preconditions / containers / custom fields) that is otherwise
	// silent. The Sync button stays disabled until the whole sync completes.
	Stage string `json:"stage"`
}

// emitStage sends a label-only progress event for a sync stage that has no item
// count, so the UI shows what's running.
func emitStage(onProgress func(Progress), stage string) {
	if onProgress != nil {
		onProgress(Progress{Stage: stage})
	}
}

// mapRunRows converts a slice of backend.TestRun records (fetched from the
// backend) into testrepo.TestRunRow values ready for storage. execKey is the
// owning execution's Jira key. envFallback is joined and used when a run has
// no Environment value of its own (the Xray testexec/test endpoint omits
// per-run environments; they are set on the execution as a whole).
func mapRunRows(runs []backend.TestRun, execKey string, envFallback []string) []testrepo.TestRunRow {
	rows := make([]testrepo.TestRunRow, 0, len(runs))
	for _, tr := range runs {
		defectsJSON := "[]"
		if len(tr.Defects) > 0 {
			if b, jerr := json.Marshal(tr.Defects); jerr == nil {
				defectsJSON = string(b)
			}
		}
		env := tr.Environment
		if env == "" && len(envFallback) > 0 {
			env = strings.Join(envFallback, ", ")
		}
		rows = append(rows, testrepo.TestRunRow{
			ExecKey:     execKey,
			TestKey:     tr.TestKey,
			RunStatus:   tr.Status,
			StartedAt:   tr.StartedAt,
			FinishedAt:  tr.FinishedAt,
			ExecutedBy:  tr.ExecutedBy,
			Environment: env,
			Defects:     defectsJSON,
			Comment:     tr.Comment,
			CreatedAt:   tr.CreatedAt,
			UpdatedAt:   tr.UpdatedAt,
		})
	}
	return rows
}

// Engine runs a pull sync for one profile.
type Engine struct {
	backend backend.Backend
	repo    *testrepo.Repository
	// crossProjectSources is the profile's configured source projects (already
	// scoped to exclude the profile's own project). The container sync searches
	// these for Test Executions that include the profile's tests. Empty means
	// cross-project execution discovery is skipped entirely.
	crossProjectSources []string
}

// Option configures an Engine at construction.
type Option func(*Engine)

// WithCrossProjectSources sets the source projects the container sync searches
// for cross-project Test Executions. Pass the already-scoped list (profile
// project excluded); an empty list skips discovery.
func WithCrossProjectSources(sources []string) Option {
	return func(e *Engine) { e.crossProjectSources = sources }
}

// New returns a sync engine bound to a backend and the local repository.
func New(b backend.Backend, repo *testrepo.Repository, opts ...Option) *Engine {
	e := &Engine{backend: b, repo: repo}
	for _, o := range opts {
		o(e)
	}
	return e
}

// pullTests fetches the project's Tests page by page into the local store,
// emitting a "Fetching tests" progress update per page. Returns the number
// fetched and the reported total.
func (e *Engine) pullTests(ctx context.Context, profileID, projectKey, scopeJQL, since string, onProgress func(Progress)) (fetched, total int, err error) {
	total = -1
	for total < 0 || fetched < total {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fetched, total, ctxErr
		}

		tests, pageTotal, pageErr := e.backend.SearchTestsPage(ctx, projectKey, scopeJQL, since, fetched, pageSize)
		if pageErr != nil {
			return fetched, total, fmt.Errorf("fetch page at offset %d: %w", fetched, pageErr)
		}
		total = pageTotal

		if uErr := e.repo.UpsertTests(profileID, toRepoTests(tests)); uErr != nil {
			return fetched, total, uErr
		}
		fetched += len(tests)

		if onProgress != nil {
			onProgress(Progress{Stage: "Fetching tests", Fetched: fetched, Total: total})
		}

		if len(tests) == 0 {
			break // defensive: avoid an infinite loop if total is misreported
		}
		if fetched < total {
			time.Sleep(throttle)
		}
	}
	return fetched, total, nil
}

// Sync pulls the Test Repository folder tree, the project's Tests, and the
// project's Preconditions into the local store, calling onProgress after each
// Test page. If `since` is empty, this is a full sync; otherwise it is an
// incremental sync that only fetches Tests updated since the watermark
// (FR-1.2). Upserts are idempotent, so an interrupted sync is safe to re-run.
func (e *Engine) Sync(ctx context.Context, profileID, projectKey, scopeJQL, since string, onProgress func(Progress)) error {
	fetched, total, err := e.pullTests(ctx, profileID, projectKey, scopeJQL, since, onProgress)
	if err != nil {
		return err
	}

	// Stage order matters. The test pull must come first: folder membership and
	// precondition links are both keyed by test, so either running before tests
	// exist silently maps nothing (an earlier first-sync bug). Preconditions run
	// ahead of the folder walk because they are by far the longest stage and
	// were previously starved on a first sync (RND_P_4TFINT_05-336).
	//
	// Unlike the other best-effort stages below, a precondition failure is
	// recorded rather than only logged. Swallowing it is what let a sync stamp
	// its watermark and report success over an empty Preconditions view.
	var stageFailures []testrepo.StageFailure
	emitStage(onProgress, "Syncing preconditions")
	if err := e.syncPreconditions(ctx, profileID, projectKey, onProgress); err != nil {
		log.Printf("xtm: precondition sync failed: %v", err)
		stageFailures = append(stageFailures, testrepo.StageFailure{
			Stage:   "preconditions",
			Message: err.Error(),
		})
	}

	// The folder tree refreshes every sync; the per-folder membership walk is
	// best-effort and never blocks the Test pull.
	emitStage(onProgress, "Loading folders")
	e.syncFolders(ctx, profileID, projectKey, since == "", onProgress)

	// Requirements, bugs and containers are best-effort too: a Xray REST quirk
	// (an absent issue type, a pagination cap, a permissions gap) is logged but
	// must never fail the whole sync — the Tests are already in.
	emitStage(onProgress, "Syncing requirements")
	if err := e.syncRequirements(ctx, profileID, projectKey, onProgress); err != nil {
		log.Printf("xtm: requirement sync failed (continuing): %v", err)
	}

	// Bugs sync BEFORE containers: the normal bug harvest wipes-and-replaces the
	// bug / test_bug caches (ReplaceAllBugs/ReplaceAllBugLinks), while the
	// container pass then ADDITIVELY merges bugs reached through cross-project
	// member Tests (UpsertBugs/UpsertBugLinks). Running bugs first guarantees the
	// container harvest is not clobbered by the wipe (#219).
	emitStage(onProgress, "Syncing bugs")
	if err := e.syncBugs(ctx, profileID, projectKey, onProgress); err != nil {
		log.Printf("xtm: bug sync failed (continuing): %v", err)
	}

	emitStage(onProgress, "Syncing containers")
	if err := e.syncContainers(ctx, profileID, projectKey, onProgress); err != nil {
		log.Printf("xtm: container sync failed (continuing): %v", err)
	}

	emitStage(onProgress, "Syncing custom fields")
	if err := e.syncCustomFields(ctx, profileID, projectKey); err != nil {
		return err
	}

	emitStage(onProgress, "Syncing project field options")
	e.syncProjectFieldOptions(ctx, profileID, projectKey)

	if err := e.repo.SetSyncState(profileID); err != nil {
		return err
	}
	if onProgress != nil {
		onProgress(Progress{Fetched: fetched, Total: total, Done: true})
	}
	if len(stageFailures) > 0 {
		return &PartialSyncError{StageFailures: stageFailures}
	}
	return nil
}

// PartialSyncError reports that a sync ran to the end and its data is usable,
// but at least one stage did not complete. It is an error so a caller cannot
// mistake the run for clean, and typed so the caller can record which stages
// failed rather than only that something did (RND_P_4TFINT_05-336).
type PartialSyncError struct {
	StageFailures []testrepo.StageFailure
}

func (e *PartialSyncError) Error() string {
	if len(e.StageFailures) == 0 {
		return "sync completed with a failed stage"
	}
	return fmt.Sprintf("sync completed with %d failed stage(s): %s: %s",
		len(e.StageFailures), e.StageFailures[0].Stage, e.StageFailures[0].Message)
}

// syncFolders refreshes the Test Repository folder tree and maps Tests to their
// folder (FR-13.1). Membership comes from two sources: any Test keys embedded in
// SyncRequirements pulls only the requirement coverage data — the per-view
// partial sync behind the Requirements tab's refresh button (#7).
func (e *Engine) SyncRequirements(ctx context.Context, profileID, projectKey string, onProgress func(Progress)) error {
	return e.syncRequirements(ctx, profileID, projectKey, onProgress)
}

// SyncContainers pulls only the Test Sets / Plans / Executions — the per-view
// partial sync behind the Containers tab's refresh button (#7).
func (e *Engine) SyncContainers(ctx context.Context, profileID, projectKey string, onProgress func(Progress)) error {
	return e.syncContainers(ctx, profileID, projectKey, onProgress)
}

// SyncBugs reconciles only the defect issues linked to the profile's tests — the
// per-view partial sync behind the Bugs panel's refresh button, so refreshing
// bugs doesn't trigger the preconditions / containers / requirements passes
// (RND_P_4TFINT_05-214).
func (e *Engine) SyncBugs(ctx context.Context, profileID, projectKey string, onProgress func(Progress)) error {
	return e.syncBugs(ctx, profileID, projectKey, onProgress)
}

// SyncTests pulls just the project's Tests and refreshes the Test Repository
// folder membership, the per-view partial sync behind the test-case (Browse)
// view's own Sync button. Unlike the full Sync it does not advance the sync
// watermark or run the requirement / container / bug passes.
func (e *Engine) SyncTests(ctx context.Context, profileID, projectKey, scopeJQL, since string, onProgress func(Progress)) error {
	fetched, total, err := e.pullTests(ctx, profileID, projectKey, scopeJQL, since, onProgress)
	if err != nil {
		return err
	}
	emitStage(onProgress, "Loading folders")
	e.syncFolders(ctx, profileID, projectKey, since == "", onProgress)
	if onProgress != nil {
		onProgress(Progress{Fetched: fetched, Total: total, Done: true})
	}
	return nil
}

// SyncBugRunData refreshes the run/execution data for every test that has at
// least one bug linked to it in the local store. It calls
// TestExecutionsForTest per affected test, additively upserts any newly
// discovered executions and their container links, then replaces the run rows
// for EVERY execution that test belongs to (not only newly discovered ones) so
// the run-history breakdown reflects up-to-date results.
//
// Errors per test or per execution are logged and skipped (best-effort).
// The pass respects ctx cancellation between test iterations.
// In demo mode, per-item throttle sleeps are skipped.
func (e *Engine) SyncBugRunData(ctx context.Context, profileID string, onProgress func(Progress)) error {
	emitStage(onProgress, "Refreshing affected-test runs")
	testKeys, err := e.repo.BugAffectedTestKeys(profileID)
	if err != nil {
		return fmt.Errorf("bug affected test keys: %w", err)
	}
	isDemo := e.backend.IsDemo()
	for i, testKey := range testKeys {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		containers, links, err := e.backend.TestExecutionsForTest(ctx, testKey)
		if err != nil {
			log.Printf("xtm: SyncBugRunData: %s: TestExecutionsForTest: %v (skipping)", testKey, err)
			if !isDemo {
				time.Sleep(throttle)
			}
			continue
		}
		if len(containers) != len(links) {
			log.Printf("xtm: SyncBugRunData: %s: container/link length mismatch (%d vs %d), skipping",
				testKey, len(containers), len(links))
			continue
		}

		// Upsert ALL executions returned by the API additively (new ones get
		// proper container rows; existing ones are no-ops due to INSERT OR
		// IGNORE semantics). TestExecutionsForTest returns every execution that
		// contains testKey across all projects, so this set covers both new and
		// pre-existing executions.
		repoContainers := make([]testrepo.Container, 0, len(containers))
		repoLinks := make([]testrepo.ContainerLink, 0, len(links))
		for idx, c := range containers {
			repoContainers = append(repoContainers, testrepo.Container{
				Key:           c.Key,
				Kind:          c.Kind,
				Summary:       c.Summary,
				Status:        c.Status,
				ParentKey:     c.ParentKey,
				ParentSummary: c.ParentSummary,
				IssueType:     c.IssueType,
				Labels:        c.Labels,
				Environments:  c.Environments,
				FixVersions:   c.FixVersions,
				Created:       c.Created,
				Updated:       c.Updated,
				Resolved:      c.Resolved,
				Description:   c.Description,
			})
			repoLinks = append(repoLinks, testrepo.ContainerLink{
				ContainerKey: links[idx].ContainerKey,
				TestKey:      links[idx].TestKey,
				RunStatus:    links[idx].RunStatus,
			})
		}
		if uErr := e.repo.UpsertContainers(profileID, repoContainers); uErr != nil {
			log.Printf("xtm: SyncBugRunData: %s: upsert containers: %v (skipping runs)", testKey, uErr)
			if !isDemo {
				time.Sleep(throttle)
			}
			continue
		}
		if lErr := e.repo.UpsertContainerLinks(profileID, repoLinks); lErr != nil {
			log.Printf("xtm: SyncBugRunData: %s: upsert links: %v (skipping runs)", testKey, lErr)
			if !isDemo {
				time.Sleep(throttle)
			}
			continue
		}

		// Refresh run rows for each execution returned by the API.
		for _, ct := range repoContainers {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			runs, rErr := e.backend.GetTestRuns(ctx, ct.Key)
			if rErr != nil {
				log.Printf("xtm: SyncBugRunData: %s: get runs for %s: %v (skipping)", testKey, ct.Key, rErr)
			} else {
				if sErr := e.repo.ReplaceRunsForExec(profileID, ct.Key, mapRunRows(runs, ct.Key, ct.Environments)); sErr != nil {
					log.Printf("xtm: SyncBugRunData: %s: replace runs for %s: %v (skipping)", testKey, ct.Key, sErr)
				}
			}
			if !isDemo {
				time.Sleep(throttle)
			}
		}
		if onProgress != nil {
			onProgress(Progress{Stage: "Refreshing affected-test runs", Fetched: i + 1, Total: len(testKeys)})
		}
		if !isDemo {
			time.Sleep(throttle)
		}
	}
	return nil
}

// the tree response (applied on every sync, free), and — on a full sync only —
// a per-folder walk for instances whose tree doesn't carry Test keys. It is
// best-effort: every failure is logged and swallowed so a folder-API problem
// can't block or fail the Test pull. Demo mode carries folder membership on the
// Tests themselves, so neither pass is needed there.
func (e *Engine) syncFolders(ctx context.Context, profileID, projectKey string, fullSync bool, onProgress func(Progress)) {
	res, err := e.backend.FolderTree(ctx, projectKey)
	if err != nil {
		log.Printf("xtm: folder tree sync: %v", err)
		return
	}
	if len(res.Folders) == 0 {
		return
	}
	repoFolders := make([]testrepo.Folder, len(res.Folders))
	for i, f := range res.Folders {
		repoFolders[i] = testrepo.Folder{
			ID:             f.ID,
			ParentID:       f.ParentID,
			Name:           f.Name,
			XrayID:         f.XrayID,
			TestCount:      f.TestCount,
			TotalTestCount: f.TotalTestCount,
		}
	}
	if err := e.repo.UpsertFolders(profileID, repoFolders); err != nil {
		log.Printf("xtm: upsert folders: %v", err)
		return
	}
	// Membership embedded in the tree is free — apply it every sync.
	if len(res.TreeMembership) > 0 {
		if err := e.repo.ApplyTestFolders(profileID, res.TreeMembership); err != nil {
			log.Printf("xtm: apply tree membership: %v", err)
		}
		return
	}
	// Otherwise walk the folders that actually contain Tests (empty folders are
	// skipped via their testCount, so this is one call per non-empty folder, not
	// per folder). That's cheap enough to run on every sync — full or not.
	e.syncFolderMembership(ctx, profileID, projectKey, res.FoldersWithTests, onProgress)
	_ = fullSync
}

// syncFolderMembership maps Tests to their Test Repository folder by fetching
// the member Tests of each folder that actually has any (empty folders were
// already filtered out). It emits "folders"-phase progress so the walk doesn't
// look stalled, and a per-folder failure is logged and skipped rather than
// aborting — a partial mapping still helps, and the tree itself is already saved.
func (e *Engine) syncFolderMembership(ctx context.Context, profileID, projectKey string, folders []backend.FolderRef, onProgress func(Progress)) {
	if len(folders) == 0 {
		return
	}
	total := len(folders)
	testFolder := map[string]string{}
	for i, f := range folders {
		if ctx.Err() != nil {
			return
		}
		keys, err := e.backend.ListTestsInFolder(ctx, projectKey, f.ID)
		if err != nil {
			log.Printf("xtm: folder %s (%s) membership: %v", f.Path, f.ID, err)
		} else {
			for _, k := range keys {
				testFolder[k] = f.Path
			}
		}
		if onProgress != nil {
			onProgress(Progress{Phase: "folders", Stage: "Mapping folder membership", Fetched: i + 1, Total: total})
		}
		time.Sleep(throttle)
	}
	if len(testFolder) == 0 {
		return
	}
	if err := e.repo.ApplyTestFolders(profileID, testFolder); err != nil {
		log.Printf("xtm: apply test folders: %v", err)
	}
}

// syncPreconditions pulls the Preconditions for a project and reconciles the
// Test-to-Precondition links. An empty result is tolerated — the real-Jira
// implementation is currently a no-op pending live verification (FR-13.4),
// but demo mode populates them.
func (e *Engine) syncPreconditions(ctx context.Context, profileID, projectKey string, onProgress func(Progress)) error {
	gen := time.Now().UnixMilli()
	progress := func(done, total int) {
		if onProgress != nil {
			onProgress(Progress{Phase: "preconditions", Stage: "Syncing preconditions", Fetched: done, Total: total})
		}
	}

	// batches counts how many times persist actually ran. Zero means the
	// instance has no Precondition issue type (ListPreconditionsStream returns
	// nil without calling onBatch), which is benign: skip the sweep, since
	// there was no pass to reconcile against.
	batches := 0

	// persist commits one batch: the precondition rows and their links, both
	// stamped with this pass's generation. Nothing is deleted here; the sweep
	// below runs only if the whole pass succeeds.
	persist := func(pre []backend.Precondition, links map[string][]string) error {
		batches++
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

	if batches == 0 {
		return nil
	}
	if _, err := e.repo.SweepTestPreconditions(profileID, gen); err != nil {
		return err
	}
	return nil
}

// syncContainers pulls the project's Test Sets, Test Plans and Test
// Executions and reconciles their Test memberships (FR-1.3). An empty result
// is tolerated — the real-Jira implementation is currently a no-op pending
// live verification, but demo mode populates them.
func (e *Engine) syncContainers(ctx context.Context, profileID, projectKey string, onProgress func(Progress)) error {
	containers, links, err := e.backend.ListContainers(ctx, projectKey, func(done, total int) {
		if onProgress != nil {
			onProgress(Progress{Phase: "containers", Stage: "Syncing containers", Fetched: done, Total: total})
		}
	})
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}
	if len(containers) == 0 && len(links) == 0 {
		return nil
	}
	repoContainers := make([]testrepo.Container, len(containers))
	for i, c := range containers {
		repoContainers[i] = testrepo.Container{
			Key:           c.Key,
			Kind:          c.Kind,
			Summary:       c.Summary,
			Status:        c.Status,
			ParentKey:     c.ParentKey,
			ParentSummary: c.ParentSummary,
			IssueType:     c.IssueType,
			Labels:        c.Labels,
			Environments:  c.Environments,
			FixVersions:   c.FixVersions,
			Created:       c.Created,
			Updated:       c.Updated,
			Resolved:      c.Resolved,
			Description:   c.Description,
		}
	}
	if err := e.repo.UpsertContainers(profileID, repoContainers); err != nil {
		return err
	}
	repoLinks := make([]testrepo.ContainerLink, len(links))
	for i, l := range links {
		repoLinks[i] = testrepo.ContainerLink{
			ContainerKey: l.ContainerKey,
			TestKey:      l.TestKey,
			RunStatus:    l.RunStatus,
		}
	}
	if err := e.repo.ReplaceAllContainerLinks(profileID, repoLinks); err != nil {
		return err
	}

	// Cache the basics of member Tests that live in another project (so have no
	// test_case row), so the board can render them instead of dropping them
	// (#219). Best-effort: on any error, log and continue — members still show by
	// key, just without a cached summary / status.
	missing, err := e.repo.ContainerMemberKeysMissingTests(profileID)
	if err != nil {
		log.Printf("xtm: find external member tests: %v", err)
		return nil
	}
	if len(missing) == 0 {
		return nil
	}
	basics, err := e.backend.ListTestsBasic(ctx, missing)
	if err != nil {
		log.Printf("xtm: fetch external member basics: %v", err)
		return nil
	}
	externals := make([]testrepo.ExternalTest, len(basics))
	for i, b := range basics {
		externals[i] = testrepo.ExternalTest{
			Key:        b.Key,
			Summary:    b.Summary,
			Status:     b.Status,
			ProjectKey: b.ProjectKey,
		}
	}
	if err := e.repo.ReplaceExternalTests(profileID, externals); err != nil {
		log.Printf("xtm: cache external member tests: %v", err)
	}

	// Harvest bugs reached THROUGH these cross-project member Tests. The normal
	// syncBugs pass walks only the profile's own test_case keys, so a bug linked
	// solely to a foreign member (the reported case: bug + execution in project A,
	// member Test in project B) is never collected. Keep only links whose target
	// issue type matches the profile's configured defect type, and merge them
	// ADDITIVELY (UpsertBugs/UpsertBugLinks) so the bugs syncBugs already wrote are
	// not clobbered (#219). Best-effort: log and swallow.
	e.harvestExternalBugs(profileID, basics)

	// Fetch test runs and exec-plan associations for every Test Execution.
	// Best-effort: a failed fetch for one execution is logged and skipped so a
	// single bad execution cannot abort the whole container sync. The fetches run
	// concurrently (paced by the backend rate limiter); the store writes are then
	// applied serially so the DB is never written from multiple goroutines.
	type execFetch struct {
		execKey string
		env     []string
		runs    []backend.TestRun
		runsOK  bool
		plans   []string
		plansOK bool
	}
	var execs []execFetch
	for _, c := range containers {
		if c.Kind == backend.KindTestExec {
			execs = append(execs, execFetch{execKey: c.Key, env: c.Environments})
		}
	}
	{
		sem := make(chan struct{}, execFetchConcurrency)
		var wg sync.WaitGroup
		for i := range execs {
			if ctx.Err() != nil {
				break
			}
			i := i
			sem <- struct{}{}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				if ctx.Err() != nil {
					return
				}
				if runs, err := e.backend.GetTestRuns(ctx, execs[i].execKey); err != nil {
					log.Printf("xtm: get test runs for %s: %v (skipping)", execs[i].execKey, err)
				} else {
					execs[i].runs, execs[i].runsOK = runs, true
				}
				if plans, err := e.backend.ExecPlans(ctx, execs[i].execKey); err != nil {
					log.Printf("xtm: get exec plans for %s: %v (skipping)", execs[i].execKey, err)
				} else {
					execs[i].plans, execs[i].plansOK = plans, true
				}
			}()
		}
		wg.Wait()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, ef := range execs {
		if ef.runsOK {
			// The Xray Server/DC testexec/test endpoint does not return a
			// per-run environment; fall back to the execution's environments.
			if err := e.repo.ReplaceRunsForExec(profileID, ef.execKey, mapRunRows(ef.runs, ef.execKey, ef.env)); err != nil {
				log.Printf("xtm: store test runs for %s: %v (skipping)", ef.execKey, err)
			}
		}
		if ef.plansOK && len(ef.plans) > 0 {
			if err := e.repo.ReplaceExecPlans(profileID, ef.execKey, ef.plans); err != nil {
				log.Printf("xtm: store exec plans for %s: %v (skipping)", ef.execKey, err)
			}
		}
	}

	// Cross-project discovery only runs when the profile configures source
	// projects; otherwise it is skipped entirely (no global per-test scan).
	if len(e.crossProjectSources) > 0 {
		emitStage(onProgress, "Discovering cross-project executions")
		knownKeys, knownErr := e.repo.AllContainerKeys(profileID)
		if knownErr != nil {
			log.Printf("xtm: cross-project discovery: read container keys: %v (skipping)", knownErr)
			return nil
		}
		e.discoverCrossProjectExecs(ctx, profileID, knownKeys, onProgress)
	}
	return nil
}

// discoverCrossProjectExecs finds Test Executions in the profile's configured
// cross-project source projects that include one of the profile's own tests,
// and adds those executions, their matching links, and their runs additively.
// It replaces the old per-test global /testexec scan (O(all tests)) with a
// search bounded to the configured source projects (O(executions in those
// projects)), reusing the same ListContainers path the main container sync
// uses — so it relies only on endpoints that sync already exercises.
//
// Only executions whose membership includes at least one of the profile's tests
// are kept, and only those matching links. The caller gates this on a non-empty
// source list. Store writes are batched and serial (the DB is never written
// from multiple goroutines); per-source and per-exec errors are logged and
// skipped (best-effort).
func (e *Engine) discoverCrossProjectExecs(ctx context.Context, profileID string, knownExecKeys map[string]bool, onProgress func(Progress)) {
	// The set of the profile's own tests, for membership matching.
	testKeys, err := e.repo.AllTestKeys(profileID)
	if err != nil {
		log.Printf("xtm: cross-project discovery: list test keys: %v (skipping)", err)
		return
	}
	mine := make(map[string]bool, len(testKeys))
	for _, k := range testKeys {
		mine[k] = true
	}

	// Search each source project's Test Executions (reusing ListContainers) and
	// keep the executions that include one of our tests, with only those links.
	seen := map[string]bool{}
	var newContainers []testrepo.Container
	var newLinks []testrepo.ContainerLink
	for _, src := range e.crossProjectSources {
		if ctx.Err() != nil {
			return
		}
		emitStage(onProgress, "Scanning "+src+" for cross-project executions")
		containers, links, err := e.backend.ListContainers(ctx, src, nil)
		if err != nil {
			log.Printf("xtm: cross-project discovery: list %s containers: %v (skipping)", src, err)
			continue
		}
		linksByContainer := map[string][]backend.ContainerLink{}
		for _, l := range links {
			linksByContainer[l.ContainerKey] = append(linksByContainer[l.ContainerKey], l)
		}
		for _, c := range containers {
			if c.Kind != backend.KindTestExec || knownExecKeys[c.Key] || seen[c.Key] {
				continue
			}
			var kept []testrepo.ContainerLink
			for _, l := range linksByContainer[c.Key] {
				if mine[l.TestKey] {
					kept = append(kept, testrepo.ContainerLink{
						ContainerKey: l.ContainerKey,
						TestKey:      l.TestKey,
						RunStatus:    l.RunStatus,
					})
				}
			}
			if len(kept) == 0 {
				continue // this execution doesn't touch any of our tests
			}
			seen[c.Key] = true
			newContainers = append(newContainers, testrepo.Container{
				Key:           c.Key,
				Kind:          c.Kind,
				Summary:       c.Summary,
				Status:        c.Status,
				ParentKey:     c.ParentKey,
				ParentSummary: c.ParentSummary,
				IssueType:     c.IssueType,
				Labels:        c.Labels,
				Environments:  c.Environments,
				FixVersions:   c.FixVersions,
				Created:       c.Created,
				Updated:       c.Updated,
				Resolved:      c.Resolved,
				Description:   c.Description,
			})
			newLinks = append(newLinks, kept...)
		}
	}
	if len(newContainers) == 0 {
		return
	}
	// Container rows first so the link foreign-key side is satisfied.
	if err := e.repo.UpsertContainers(profileID, newContainers); err != nil {
		log.Printf("xtm: cross-project discovery: upsert containers: %v (skipping)", err)
		return
	}
	if err := e.repo.UpsertContainerLinks(profileID, newLinks); err != nil {
		log.Printf("xtm: cross-project discovery: upsert links: %v (skipping)", err)
		return
	}

	// Fetch runs for the discovered executions concurrently, store serially.
	type runFetch struct {
		execKey string
		env     []string
		runs    []backend.TestRun
		ok      bool
	}
	runs := make([]runFetch, len(newContainers))
	for i, c := range newContainers {
		runs[i] = runFetch{execKey: c.Key, env: c.Environments}
	}
	{
		sem := make(chan struct{}, crossProjectConcurrency)
		var wg sync.WaitGroup
		for i := range runs {
			if ctx.Err() != nil {
				break
			}
			i := i
			sem <- struct{}{}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				if ctx.Err() != nil {
					return
				}
				if r, err := e.backend.GetTestRuns(ctx, runs[i].execKey); err != nil {
					log.Printf("xtm: cross-project discovery: get runs for %s: %v (skipping)", runs[i].execKey, err)
				} else {
					runs[i].runs, runs[i].ok = r, true
				}
			}()
		}
		wg.Wait()
	}
	for _, rf := range runs {
		if rf.ok {
			_ = e.repo.ReplaceRunsForExec(profileID, rf.execKey, mapRunRows(rf.runs, rf.execKey, rf.env))
		}
	}
}

// harvestExternalBugs upserts the defect issues (and their Test links) reached
// through cross-project member Tests, additively, so they are not clobbered by
// the wipe-and-replace normal bug sync (which already ran). Only links whose
// target issue type matches the profile's configured bug issue type are kept.
func (e *Engine) harvestExternalBugs(profileID string, basics []backend.TestBasic) {
	bugType := strings.ToLower(strings.TrimSpace(e.repo.ProfileBugIssueType(profileID)))
	if bugType == "" {
		bugType = "bug"
	}
	bugByKey := map[string]testrepo.Bug{}
	links := []testrepo.BugLink{}
	for _, b := range basics {
		for _, ln := range b.IssueLinks {
			if strings.ToLower(strings.TrimSpace(ln.IssueType)) != bugType {
				continue
			}
			bugByKey[ln.Key] = testrepo.Bug{
				Key:        ln.Key,
				ProjectKey: ln.ProjectKey,
				IssueType:  ln.IssueType,
				Summary:    ln.Summary,
				Status:     ln.Status,
				Priority:   ln.Priority,
			}
			links = append(links, testrepo.BugLink{TestKey: b.Key, BugKey: ln.Key, LinkID: ln.LinkID})
		}
	}
	if len(links) == 0 {
		return
	}
	bugs := make([]testrepo.Bug, 0, len(bugByKey))
	for _, b := range bugByKey {
		bugs = append(bugs, b)
	}
	if err := e.repo.UpsertBugs(profileID, bugs); err != nil {
		log.Printf("xtm: upsert cross-project bugs: %v", err)
		return
	}
	if err := e.repo.UpsertBugLinks(profileID, links); err != nil {
		log.Printf("xtm: upsert cross-project bug links: %v", err)
	}
}

// syncRequirements refreshes requirement issues and their Test coverage links
// from the configured requirement sources (plus, in the real path, requirements
// linked to synced Tests regardless of project). Best-effort like the other
// secondary syncs.
func (e *Engine) syncRequirements(ctx context.Context, profileID, projectKey string, onProgress func(Progress)) error {
	sources, err := e.repo.ListRequirementSources(profileID)
	if err != nil {
		return err
	}
	specs := make([]backend.RequirementSourceSpec, len(sources))
	for i, s := range sources {
		specs[i] = backend.RequirementSourceSpec{
			ProjectKey: s.ProjectKey,
			IssueTypes: strings.Fields(s.IssueTypes),
			ScopeJQL:   s.ScopeJQL,
		}
	}

	reqs, links, err := e.backend.ListRequirements(ctx, projectKey, specs, nil)
	if err != nil {
		return err
	}

	repoReqs := make([]testrepo.Requirement, len(reqs))
	for i, rq := range reqs {
		repoReqs[i] = testrepo.Requirement{
			Key:         rq.Key,
			ProjectKey:  rq.ProjectKey,
			IssueType:   rq.IssueType,
			Summary:     rq.Summary,
			Status:      rq.Status,
			Updated:     rq.Updated,
			Priority:    rq.Priority,
			Components:  rq.Components,
			FixVersions: rq.FixVersions,
			Sprint:      rq.Sprint,
			Description: rq.Description,
			EpicKey:     rq.EpicKey,
		}
	}
	if err := e.repo.ReplaceAllRequirements(profileID, repoReqs); err != nil {
		return err
	}

	repoLinks := make([]testrepo.RequirementLink, len(links))
	for i, l := range links {
		repoLinks[i] = testrepo.RequirementLink{
			TestKey:        l.TestKey,
			RequirementKey: l.RequirementKey,
			LinkID:         l.LinkID,
		}
	}
	if err := e.repo.ReplaceAllRequirementLinks(profileID, repoLinks); err != nil {
		return err
	}

	// Sync Requirement->Requirement links (e.g. "requires") if any are available.
	// The live path returns empty until the harvest is implemented (see
	// ListReqToReqLinks in internal/jira/requirements.go).
	reqKeys := make([]string, len(reqs))
	for i, rq := range reqs {
		reqKeys[i] = rq.Key
	}
	rrLinks, err := e.backend.ListReqToReqLinks(ctx, reqKeys)
	if err != nil {
		return err
	}
	repoRRLinks := make([]testrepo.ReqReqLink, len(rrLinks))
	for i, l := range rrLinks {
		repoRRLinks[i] = testrepo.ReqReqLink{
			FromKey:  l.FromKey,
			ToKey:    l.ToKey,
			LinkType: l.LinkType,
			LinkID:   l.LinkID,
		}
	}
	return e.repo.ReplaceAllReqReqLinks(profileID, repoRRLinks)
}

// syncBugs discovers the defect issues for the profile and reconciles the local
// cache. It runs two complementary passes and merges the results:
//
//  1. A project-wide search (ListProjectBugs) that returns ALL bugs in the
//     configured bug project, regardless of whether they are linked to any
//     synced Test. This fills the Bugs panel with every defect the team has
//     filed, not just those reachable from synced Tests.
//
//  2. The test-link harvest (ListBugs) that reaches bugs through the synced
//     Tests' issuelinks. This is the only source of BugLink records (which
//     Test each bug is linked to) and also captures cross-project bugs that
//     may not live in the configured bug project.
//
// The two bug sets are merged (deduped by key); the project-wide record wins
// when both sources carry the same key because it includes the Updated field.
// The final merged bugs and the harvest links are stored via ReplaceAllBugs /
// ReplaceAllBugLinks. If the project-wide search fails it is logged and the
// sync continues with the link-harvest bugs only.
func (e *Engine) syncBugs(ctx context.Context, profileID, projectKey string, onProgress func(Progress)) error {
	// testKeys is a non-nil slice (possibly empty). ListBugs/demoBugs treat nil
	// as "no filter" and a non-nil empty slice as "nothing", so a profile with
	// zero synced tests correctly yields no bugs rather than every defect.
	testKeys, err := e.repo.AllTestKeys(profileID)
	if err != nil {
		return err
	}
	issueType := e.repo.ProfileBugIssueType(profileID)

	// Resolve which project to search for all bugs. When the profile is set to
	// "dedicated" mode with a non-empty key, search that project; otherwise use
	// the test project (the default).
	bugProject := projectKey
	if e.repo.ProfileBugProjectMode(profileID) == "dedicated" {
		if k := e.repo.ProfileBugProjectKey(profileID); k != "" {
			bugProject = k
		}
	}

	// Pass 1: project-wide bug search (best-effort). Labelled so the sync bar
	// shows this phase is running before the (longer) per-test harvest.
	emitStage(onProgress, "Syncing bugs (project search)")
	projectBugs, projectBugErr := e.backend.ListProjectBugs(ctx, bugProject, issueType)
	if projectBugErr != nil {
		log.Printf("xtm: project-wide bug search failed (continuing with link harvest only): %v", projectBugErr)
		projectBugs = nil
	}

	// Pass 2: link-harvest (authoritative source for BugLink records). Report
	// per-chunk progress (test keys processed) so the sync bar advances instead
	// of sitting silent through the harvest of a large project (#322 follow-up).
	harvestBugs, links, err := e.backend.ListBugs(ctx, projectKey, testKeys, issueType,
		func(done, total int) {
			if onProgress != nil {
				onProgress(Progress{Phase: "bugs", Stage: "Syncing bugs", Fetched: done, Total: total})
			}
		})
	if err != nil {
		return err
	}

	// Merge: project-wide bugs seed the map (they have the Updated field);
	// harvest bugs are added for any key not yet present (e.g. cross-project
	// bugs linked to tests that are not in the bug project).
	merged := make(map[string]backend.Bug, len(projectBugs)+len(harvestBugs))
	for _, b := range projectBugs {
		merged[b.Key] = b
	}
	for _, b := range harvestBugs {
		if _, exists := merged[b.Key]; !exists {
			merged[b.Key] = b
		}
	}

	repoBugs := make([]testrepo.Bug, 0, len(merged))
	for _, b := range merged {
		repoBugs = append(repoBugs, testrepo.Bug{
			Key: b.Key, ProjectKey: b.ProjectKey, IssueType: b.IssueType,
			Summary: b.Summary, Status: b.Status, Priority: b.Priority, Updated: b.Updated,
		})
	}
	if err := e.repo.ReplaceAllBugs(profileID, repoBugs); err != nil {
		return err
	}
	repoLinks := make([]testrepo.BugLink, 0, len(links))
	for _, l := range links {
		repoLinks = append(repoLinks, testrepo.BugLink{TestKey: l.TestKey, BugKey: l.BugKey, LinkID: l.LinkID})
	}
	return e.repo.ReplaceAllBugLinks(profileID, repoLinks)
}

// syncCustomFields pulls the custom field definitions configured for the
// project's Test issue type (FR-2.6) and caches them. An empty result is
// tolerated — the real-Jira implementation is currently a no-op pending live
// verification, but demo mode populates them.
func (e *Engine) syncCustomFields(ctx context.Context, profileID, projectKey string) error {
	defs, err := e.backend.ListCustomFields(ctx, projectKey)
	if err != nil {
		return fmt.Errorf("list custom fields: %w", err)
	}
	if len(defs) == 0 {
		return nil
	}
	repoDefs := make([]testrepo.CustomFieldDef, len(defs))
	for i, d := range defs {
		repoDefs[i] = testrepo.CustomFieldDef{FieldID: d.ID, Name: d.Name, Type: d.Type}
	}
	return e.repo.UpsertCustomFields(profileID, repoDefs)
}

// syncProjectFieldOptions fetches components and fix versions for each
// in-scope project key (the profile's own project plus every configured
// requirement source project, de-duplicated) and caches them in
// project_field_option. Best-effort: a per-project or per-field failure is
// logged and skipped so a single unavailable project cannot abort the sync.
func (e *Engine) syncProjectFieldOptions(ctx context.Context, profileID, projectKey string) {
	// Collect all in-scope project keys, de-duplicated.
	keys := []string{projectKey}
	seen := map[string]bool{projectKey: true}
	sources, err := e.repo.ListRequirementSources(profileID)
	if err != nil {
		log.Printf("xtm: syncProjectFieldOptions: list requirement sources: %v (continuing)", err)
	} else {
		for _, s := range sources {
			if !seen[s.ProjectKey] {
				seen[s.ProjectKey] = true
				keys = append(keys, s.ProjectKey)
			}
		}
	}

	for _, key := range keys {
		if ctx.Err() != nil {
			return
		}
		components, err := e.backend.ProjectComponents(ctx, key)
		if err != nil {
			log.Printf("xtm: syncProjectFieldOptions: components for %s: %v (skipping)", key, err)
		} else if storeErr := e.repo.ReplaceProjectFieldOptions(profileID, key, "component", components); storeErr != nil {
			log.Printf("xtm: syncProjectFieldOptions: store components for %s: %v (skipping)", key, storeErr)
		}

		if ctx.Err() != nil {
			return
		}
		versions, err := e.backend.ProjectVersions(ctx, key)
		if err != nil {
			log.Printf("xtm: syncProjectFieldOptions: versions for %s: %v (skipping)", key, err)
		} else if storeErr := e.repo.ReplaceProjectFieldOptions(profileID, key, "fixversion", versions); storeErr != nil {
			log.Printf("xtm: syncProjectFieldOptions: store versions for %s: %v (skipping)", key, storeErr)
		}
	}
}

// toRepoTests maps the backend's Test type to the repository's TestCase.
func toRepoTests(in []backend.Test) []testrepo.TestCase {
	out := make([]testrepo.TestCase, len(in))
	for i, t := range in {
		out[i] = testrepo.TestCase{
			Key:               t.Key,
			ID:                t.ID,
			Summary:           t.Summary,
			Description:       t.Description,
			Status:            t.Status,
			Priority:          t.Priority,
			Labels:            t.Labels,
			Components:        t.Components,
			Updated:           t.Updated,
			FolderID:          t.FolderID,
			ExecType:          t.ExecType,
			FixVersions:       t.FixVersions,
			CucumberScenario:  t.CucumberScenario,
			CucumberType:      t.CucumberType,
			GenericDefinition: t.GenericDefinition,
		}
	}
	return out
}
