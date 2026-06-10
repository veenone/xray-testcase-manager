// Package syncer pulls Xray data from Jira into the local store (FR-1).
package syncer

import (
	"context"
	"fmt"
	"log"
	"time"

	"xray-test-manager/internal/jira"
	"xray-test-manager/internal/testrepo"
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
	// TestsDone marks the end of the user-visible Test pull, before the
	// best-effort folder / precondition / container / custom-field tail work.
	// The UI releases the Sync button here so it doesn't look stuck while that
	// background tail finishes (Done still fires when everything completes).
	TestsDone bool `json:"testsDone"`
}

// Engine runs a pull sync for one profile.
type Engine struct {
	client *jira.Client
	repo   *testrepo.Repository
}

// New returns a sync engine bound to a Jira client and the local repository.
func New(client *jira.Client, repo *testrepo.Repository) *Engine {
	return &Engine{client: client, repo: repo}
}

// Sync pulls the Test Repository folder tree, the project's Tests, and the
// project's Preconditions into the local store, calling onProgress after each
// Test page. If `since` is empty, this is a full sync; otherwise it is an
// incremental sync that only fetches Tests updated since the watermark
// (FR-1.2). Upserts are idempotent, so an interrupted sync is safe to re-run.
func (e *Engine) Sync(ctx context.Context, profileID, projectKey, scopeJQL, since string, onProgress func(Progress)) error {
	fetched := 0
	total := -1

	for total < 0 || fetched < total {
		if err := ctx.Err(); err != nil {
			return err
		}

		tests, pageTotal, err := e.client.SearchTestsPage(ctx, projectKey, scopeJQL, since, fetched, pageSize)
		if err != nil {
			return fmt.Errorf("fetch page at offset %d: %w", fetched, err)
		}
		total = pageTotal

		if err := e.repo.UpsertTests(profileID, toRepoTests(tests)); err != nil {
			return err
		}
		fetched += len(tests)

		if onProgress != nil {
			onProgress(Progress{Fetched: fetched, Total: total})
		}

		if len(tests) == 0 {
			break // defensive: avoid an infinite loop if total is misreported
		}
		if fetched < total {
			time.Sleep(throttle)
		}
	}

	// The user-visible Test pull is complete; everything below is best-effort
	// tail work (folders, preconditions, containers, custom fields). Signal it so
	// the UI can release the Sync button while the tail finishes in the background.
	if onProgress != nil {
		onProgress(Progress{Fetched: fetched, Total: total, TestsDone: true})
	}

	// Folders sync AFTER the Tests are in the store. Folder membership stamps
	// folder_id onto existing test_case rows, so running it before the Test pull
	// (as it used to) left the first sync's folders empty — the rows didn't exist
	// yet. The tree refreshes every sync; the per-folder membership walk is
	// best-effort and never blocks the Test pull.
	e.syncFolders(ctx, profileID, projectKey, since == "", onProgress)

	// Preconditions and containers are best-effort, like folders: a Xray REST
	// quirk (an absent issue type, a pagination cap, a permissions gap) is
	// logged but must never fail the whole sync — the Tests are already in.
	if err := e.syncPreconditions(ctx, profileID, projectKey); err != nil {
		log.Printf("xtm: precondition sync failed (continuing): %v", err)
	}

	if err := e.syncContainers(ctx, profileID, projectKey); err != nil {
		log.Printf("xtm: container sync failed (continuing): %v", err)
	}

	if err := e.syncCustomFields(ctx, profileID, projectKey); err != nil {
		return err
	}

	if err := e.repo.SetSyncState(profileID); err != nil {
		return err
	}
	if onProgress != nil {
		onProgress(Progress{Fetched: fetched, Total: total, Done: true})
	}
	return nil
}

// syncFolders refreshes the Test Repository folder tree and maps Tests to their
// folder (FR-13.1). Membership comes from two sources: any Test keys embedded in
// the tree response (applied on every sync, free), and — on a full sync only —
// a per-folder walk for instances whose tree doesn't carry Test keys. It is
// best-effort: every failure is logged and swallowed so a folder-API problem
// can't block or fail the Test pull. Demo mode carries folder membership on the
// Tests themselves, so neither pass is needed there.
func (e *Engine) syncFolders(ctx context.Context, profileID, projectKey string, fullSync bool, onProgress func(Progress)) {
	res, err := e.client.FolderTree(ctx, projectKey)
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
func (e *Engine) syncFolderMembership(ctx context.Context, profileID, projectKey string, folders []jira.FolderRef, onProgress func(Progress)) {
	if len(folders) == 0 {
		return
	}
	total := len(folders)
	testFolder := map[string]string{}
	for i, f := range folders {
		if ctx.Err() != nil {
			return
		}
		keys, err := e.client.ListTestsInFolder(ctx, projectKey, f.ID)
		if err != nil {
			log.Printf("xtm: folder %s (%s) membership: %v", f.Path, f.ID, err)
		} else {
			for _, k := range keys {
				testFolder[k] = f.Path
			}
		}
		if onProgress != nil {
			onProgress(Progress{Phase: "folders", Fetched: i + 1, Total: total})
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
func (e *Engine) syncPreconditions(ctx context.Context, profileID, projectKey string) error {
	preconditions, links, err := e.client.ListPreconditions(ctx, projectKey)
	if err != nil {
		return fmt.Errorf("list preconditions: %w", err)
	}
	if len(preconditions) == 0 && len(links) == 0 {
		return nil
	}
	repoPre := make([]testrepo.Precondition, len(preconditions))
	for i, p := range preconditions {
		repoPre[i] = testrepo.Precondition{
			Key:         p.Key,
			Summary:     p.Summary,
			Type:        p.Type,
			Description: p.Description,
		}
	}
	if err := e.repo.UpsertPreconditions(profileID, repoPre); err != nil {
		return err
	}
	return e.repo.ReplaceAllTestPreconditions(profileID, links)
}

// syncContainers pulls the project's Test Sets, Test Plans and Test
// Executions and reconciles their Test memberships (FR-1.3). An empty result
// is tolerated — the real-Jira implementation is currently a no-op pending
// live verification, but demo mode populates them.
func (e *Engine) syncContainers(ctx context.Context, profileID, projectKey string) error {
	containers, links, err := e.client.ListContainers(ctx, projectKey)
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}
	if len(containers) == 0 && len(links) == 0 {
		return nil
	}
	repoContainers := make([]testrepo.Container, len(containers))
	for i, c := range containers {
		repoContainers[i] = testrepo.Container{
			Key:     c.Key,
			Kind:    c.Kind,
			Summary: c.Summary,
			Status:  c.Status,
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
	return e.repo.ReplaceAllContainerLinks(profileID, repoLinks)
}

// syncCustomFields pulls the custom field definitions configured for the
// project's Test issue type (FR-2.6) and caches them. An empty result is
// tolerated — the real-Jira implementation is currently a no-op pending live
// verification, but demo mode populates them.
func (e *Engine) syncCustomFields(ctx context.Context, profileID, projectKey string) error {
	defs, err := e.client.ListCustomFields(ctx, projectKey)
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

// toRepoTests maps the Jira client's Test type to the repository's TestCase.
func toRepoTests(in []jira.Test) []testrepo.TestCase {
	out := make([]testrepo.TestCase, len(in))
	for i, t := range in {
		out[i] = testrepo.TestCase{
			Key:         t.Key,
			ID:          t.ID,
			Summary:     t.Summary,
			Description: t.Description,
			Status:      t.Status,
			Priority:    t.Priority,
			Labels:      t.Labels,
			Components:  t.Components,
			Updated:     t.Updated,
			FolderID:    t.FolderID,
		}
	}
	return out
}
