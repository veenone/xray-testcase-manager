// Package syncer pulls Xray data from Jira into the local store (FR-1).
package syncer

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
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
			onProgress(Progress{Stage: "Fetching tests", Fetched: fetched, Total: total})
		}

		if len(tests) == 0 {
			break // defensive: avoid an infinite loop if total is misreported
		}
		if fetched < total {
			time.Sleep(throttle)
		}
	}

	// Folders sync AFTER the Tests are in the store. Folder membership stamps
	// folder_id onto existing test_case rows, so running it before the Test pull
	// (as it used to) left the first sync's folders empty — the rows didn't exist
	// yet. The tree refreshes every sync; the per-folder membership walk is
	// best-effort and never blocks the Test pull.
	emitStage(onProgress, "Loading folders")
	e.syncFolders(ctx, profileID, projectKey, since == "", onProgress)

	// Preconditions and containers are best-effort, like folders: a Xray REST
	// quirk (an absent issue type, a pagination cap, a permissions gap) is
	// logged but must never fail the whole sync — the Tests are already in.
	emitStage(onProgress, "Syncing preconditions")
	if err := e.syncPreconditions(ctx, profileID, projectKey, onProgress); err != nil {
		log.Printf("xtm: precondition sync failed (continuing): %v", err)
	}

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
	preconditions, links, err := e.client.ListPreconditions(ctx, projectKey, func(done, total int) {
		if onProgress != nil {
			onProgress(Progress{Phase: "preconditions", Stage: "Syncing preconditions", Fetched: done, Total: total})
		}
	})
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
func (e *Engine) syncContainers(ctx context.Context, profileID, projectKey string, onProgress func(Progress)) error {
	containers, links, err := e.client.ListContainers(ctx, projectKey, func(done, total int) {
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
			Environments:  c.Environments,
			FixVersions:   c.FixVersions,
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
	basics, err := e.client.ListTestsBasic(ctx, missing)
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

	// Fetch test runs and exec-plan associations for every Test Execution. This
	// is best-effort: a failed fetch for one execution is logged and skipped so a
	// single bad execution cannot abort the whole container sync.
	for _, c := range containers {
		if c.Kind != jira.KindTestExec {
			continue
		}
		execKey := c.Key
		if err := ctx.Err(); err != nil {
			return err
		}

		runs, err := e.client.GetTestRuns(ctx, execKey)
		if err != nil {
			log.Printf("xtm: get test runs for %s: %v (skipping)", execKey, err)
		} else {
			rows := make([]testrepo.TestRunRow, 0, len(runs))
			for _, tr := range runs {
				defectsJSON := "[]"
				if len(tr.Defects) > 0 {
					b, jerr := json.Marshal(tr.Defects)
					if jerr == nil {
						defectsJSON = string(b)
					}
				}
				// The Xray Server/DC testexec/test endpoint does not return a
				// per-run environment; the environment is set on the Test
				// Execution as a whole. Fall back to the execution's environments
				// so the run history shows where each run executed.
				env := tr.Environment
				if env == "" && len(c.Environments) > 0 {
					env = strings.Join(c.Environments, ", ")
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
					CreatedAt:   tr.CreatedAt,
					UpdatedAt:   tr.UpdatedAt,
				})
			}
			if err := e.repo.ReplaceRunsForExec(profileID, execKey, rows); err != nil {
				log.Printf("xtm: store test runs for %s: %v (skipping)", execKey, err)
			}
		}

		plans, err := e.client.ExecPlans(ctx, execKey)
		if err != nil {
			log.Printf("xtm: get exec plans for %s: %v (skipping)", execKey, err)
		} else if len(plans) > 0 {
			if err := e.repo.ReplaceExecPlans(profileID, execKey, plans); err != nil {
				log.Printf("xtm: store exec plans for %s: %v (skipping)", execKey, err)
			}
		}
	}

	emitStage(onProgress, "Discovering cross-project executions")
	knownKeys, knownErr := e.repo.AllContainerKeys(profileID)
	if knownErr != nil {
		log.Printf("xtm: cross-project discovery: read container keys: %v (skipping)", knownErr)
		return nil
	}
	e.discoverCrossProjectExecs(ctx, profileID, knownKeys, onProgress)
	return nil
}

// discoverCrossProjectExecs performs a per-test cross-project execution
// discovery pass after the main container sync. For each test key in the
// profile, it calls jira.Client.TestExecutionsForTest to find Test Executions
// in any project (not only the profile project) that include that test. Newly
// found containers and links are written additively via UpsertContainerLinks
// so they do not overwrite the project-scoped links that syncContainers already
// wrote.
//
// Errors per test are logged and skipped; a failure on one test does not abort
// the rest. This is intentionally best-effort: the caller (syncContainers) logs
// a notice if we cannot read the initial key list, but otherwise continues.
//
// In demo mode, time.Sleep is skipped so the full 5000-test pass completes
// without a 750-second delay.
func (e *Engine) discoverCrossProjectExecs(ctx context.Context, profileID string, knownExecKeys map[string]bool, onProgress func(Progress)) {
	testKeys, err := e.repo.AllTestKeys(profileID)
	if err != nil {
		log.Printf("xtm: cross-project discovery: list test keys: %v (skipping)", err)
		return
	}
	isDemo := e.client.IsDemo()

	for _, testKey := range testKeys {
		if ctx.Err() != nil {
			return
		}
		containers, links, err := e.client.TestExecutionsForTest(ctx, testKey)
		if err != nil {
			log.Printf("xtm: cross-project discovery: %s: %v (skipping)", testKey, err)
			if !isDemo {
				time.Sleep(throttle)
			}
			continue
		}

		// Filter to executions not already known from the project sync so we
		// do not re-insert rows that ReplaceAllContainerLinks already covered.
		var newContainers []testrepo.Container
		var newLinks []testrepo.ContainerLink
		for i, c := range containers {
			if knownExecKeys[c.Key] {
				continue
			}
			newContainers = append(newContainers, testrepo.Container{
				Key:           c.Key,
				Kind:          c.Kind,
				Summary:       c.Summary,
				Status:        c.Status,
				ParentKey:     c.ParentKey,
				ParentSummary: c.ParentSummary,
				IssueType:     c.IssueType,
				Environments:  c.Environments,
				FixVersions:   c.FixVersions,
			})
			newLinks = append(newLinks, testrepo.ContainerLink{
				ContainerKey: links[i].ContainerKey,
				TestKey:      links[i].TestKey,
				RunStatus:    links[i].RunStatus,
			})
		}

		if len(newContainers) > 0 {
			// Upsert the container rows first so the foreign-key side is satisfied.
			if uErr := e.repo.UpsertContainers(profileID, newContainers); uErr != nil {
				log.Printf("xtm: cross-project discovery: %s: upsert containers: %v (skipping)", testKey, uErr)
			} else if lErr := e.repo.UpsertContainerLinks(profileID, newLinks); lErr != nil {
				log.Printf("xtm: cross-project discovery: %s: upsert links: %v (skipping)", testKey, lErr)
			} else {
				// Also fetch and store runs for each newly discovered execution.
				for _, ct := range newContainers {
					if ctx.Err() != nil {
						return
					}
					runs, rErr := e.client.GetTestRuns(ctx, ct.Key)
					if rErr != nil {
						log.Printf("xtm: cross-project discovery: %s: get runs for %s: %v (skipping)", testKey, ct.Key, rErr)
					} else {
						rows := make([]testrepo.TestRunRow, 0, len(runs))
						for _, tr := range runs {
							defectsJSON := "[]"
							if len(tr.Defects) > 0 {
								b, jerr := json.Marshal(tr.Defects)
								if jerr == nil {
									defectsJSON = string(b)
								}
							}
							env := tr.Environment
							if env == "" && len(ct.Environments) > 0 {
								env = strings.Join(ct.Environments, ", ")
							}
							rows = append(rows, testrepo.TestRunRow{
								ExecKey:     ct.Key,
								TestKey:     tr.TestKey,
								RunStatus:   tr.Status,
								StartedAt:   tr.StartedAt,
								FinishedAt:  tr.FinishedAt,
								ExecutedBy:  tr.ExecutedBy,
								Environment: env,
								Defects:     defectsJSON,
								CreatedAt:   tr.CreatedAt,
								UpdatedAt:   tr.UpdatedAt,
							})
						}
						_ = e.repo.ReplaceRunsForExec(profileID, ct.Key, rows)
					}
					if !isDemo {
						time.Sleep(throttle)
					}
				}
			}
		}
		if !isDemo {
			time.Sleep(throttle)
		}
	}
}

// harvestExternalBugs upserts the defect issues (and their Test links) reached
// through cross-project member Tests, additively, so they are not clobbered by
// the wipe-and-replace normal bug sync (which already ran). Only links whose
// target issue type matches the profile's configured bug issue type are kept.
func (e *Engine) harvestExternalBugs(profileID string, basics []jira.TestBasic) {
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
	specs := make([]jira.RequirementSourceSpec, len(sources))
	for i, s := range sources {
		specs[i] = jira.RequirementSourceSpec{
			ProjectKey: s.ProjectKey,
			IssueTypes: strings.Fields(s.IssueTypes),
			ScopeJQL:   s.ScopeJQL,
		}
	}

	reqs, links, err := e.client.ListRequirements(ctx, projectKey, specs, nil)
	if err != nil {
		return err
	}

	repoReqs := make([]testrepo.Requirement, len(reqs))
	for i, rq := range reqs {
		repoReqs[i] = testrepo.Requirement{
			Key:        rq.Key,
			ProjectKey: rq.ProjectKey,
			IssueType:  rq.IssueType,
			Summary:    rq.Summary,
			Status:     rq.Status,
			Updated:    rq.Updated,
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
	return e.repo.ReplaceAllRequirementLinks(profileID, repoLinks)
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

	// Pass 1: project-wide bug search (best-effort).
	projectBugs, projectBugErr := e.client.ListProjectBugs(ctx, bugProject, issueType)
	if projectBugErr != nil {
		log.Printf("xtm: project-wide bug search failed (continuing with link harvest only): %v", projectBugErr)
		projectBugs = nil
	}

	// Pass 2: link-harvest (authoritative source for BugLink records).
	harvestBugs, links, err := e.client.ListBugs(ctx, projectKey, testKeys, issueType, nil)
	if err != nil {
		return err
	}

	// Merge: project-wide bugs seed the map (they have the Updated field);
	// harvest bugs are added for any key not yet present (e.g. cross-project
	// bugs linked to tests that are not in the bug project).
	merged := make(map[string]jira.Bug, len(projectBugs)+len(harvestBugs))
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
			ExecType:    t.ExecType,
			FixVersions: t.FixVersions,
		}
	}
	return out
}
