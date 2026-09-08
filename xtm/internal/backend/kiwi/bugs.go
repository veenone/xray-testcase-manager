package kiwi

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"agile-suite/xtm/internal/backend"
)

// Kiwi has no Jira-style issue type. What it has is a hyperlink on a Test
// Execution, so a defect filed elsewhere is represented here as a link from an
// execution to that issue's browse URL.
//
// Both RPCs were confirmed against a live instance before this was written:
// TestExecution.get_links returns [] for an execution with no links, and
// TestExecution.add_link rejects a call missing "url" with
// [('url', ['This field is required.'])].

// SetBugBrowseBase records the Jira browse prefix (for example
// "https://jira.example.com/browse/") for the project this workspace files its
// defects into.
//
// It is what makes a hyperlink recognizable as a bug. Kiwi stores links as
// plain URLs with no type, so without the prefix there is no way to tell a
// defect from a wiki page, and ListBugs reports nothing rather than guessing.
func (a *Adapter) SetBugBrowseBase(base string) {
	a.bugBrowseBase = strings.TrimSpace(base)
}

// bugKeyFromURL returns the issue key a hyperlink points at, or "" when the
// link is not a bug link for this workspace's configured tracker.
//
// It parses rawURL with net/url rather than doing prefix arithmetic on the
// raw string, and rebuilds the comparison target from scheme+host+path only:
// a query string (e.g. a saved-filter link) or a fragment (e.g. a deep link
// to a comment) are real shapes a pasted browser URL can carry, and both are
// structurally excluded from url.URL.Path, so they can never end up inside
// the returned key.
func (a *Adapter) bugKeyFromURL(rawURL string) string {
	if a.bugBrowseBase == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	clean := u.Scheme + "://" + u.Host + u.Path
	if !strings.HasPrefix(clean, a.bugBrowseBase) {
		return ""
	}
	key := strings.Trim(strings.TrimPrefix(clean, a.bugBrowseBase), "/")
	if key == "" || strings.Contains(key, "/") {
		return ""
	}
	return key
}

// CreateBugLink hyperlinks an already-created bug onto a Test Execution.
//
// The first argument is the id of the EXECUTION itself, not a test case and
// not a run: that is what Kiwi's link model anchors to. It stays the fallback
// for a caller that holds only one key; a caller that knows the run and the
// test should use CreateRunBugLink, which resolves the execution rather than
// trusting a single id to already be one.
func (a *Adapter) CreateBugLink(ctx context.Context, execKey, bugKey string) error {
	if strings.TrimSpace(execKey) == "" {
		return errors.New("kiwi: a bug link needs the Test Execution it was raised from")
	}
	execID, err := parseKiwiID(execKey)
	if err != nil {
		return fmt.Errorf("kiwi: %w", err)
	}
	return a.addBugLink(ctx, execID, bugKey)
}

// CreateRunBugLink hyperlinks an already-created bug onto the execution of one
// test inside a Test Run (backend.RunScopedBugLinker).
//
// execKey is a Kiwi TestRun id — that is what a KindTestExec container's key
// holds, and what the bug's pending change records — so it is NOT the id
// add_link wants. The execution is resolved from the (run, case) pair exactly
// as SetTestRunStatus resolves it, which is what keeps a defect off an
// unrelated execution whose pk happens to equal the run's
// (RND_P_4TFINT_05-359).
func (a *Adapter) CreateRunBugLink(ctx context.Context, execKey, testKey, bugKey string) error {
	if strings.TrimSpace(execKey) == "" {
		return errors.New("kiwi: a bug link needs the Test Execution it was raised from")
	}
	if strings.TrimSpace(testKey) == "" {
		return errors.New("kiwi: a bug link needs the test it was raised against")
	}
	execID, err := a.executionIDForRunCase(ctx, execKey, testKey)
	if err != nil {
		return err
	}
	return a.addBugLink(ctx, execID, bugKey)
}

// addBugLink is the one place the add_link payload is built, so the two entry
// points above cannot drift in what they send. execID is an actual
// TestExecution pk by the time it gets here.
func (a *Adapter) addBugLink(ctx context.Context, execID int, bugKey string) error {
	if strings.TrimSpace(bugKey) == "" {
		return errors.New("kiwi: a bug link needs an issue key")
	}
	if a.bugBrowseBase == "" {
		return errors.New("kiwi: no bug tracker URL is configured for this profile")
	}
	var out any
	return a.c.call(ctx, "TestExecution.add_link", []any{map[string]any{
		"execution_id": execID,
		"name":         bugKey,
		"url":          a.bugBrowseBase + bugKey,
		"is_defect":    true,
	}}, &out)
}

// kiwiBugExecution is the slice element executionsForCases returns: just
// enough of a TestExecution.filter row for the bug read below to fetch links
// per execution and attribute them back to the test case that owns it.
type kiwiBugExecution struct {
	ID      int
	CaseKey string
}

// executionsForCases finds every execution covering any of the given test
// case keys, modelled on TestExecutionsForTest (adapter.go) but batched
// across many cases in one TestExecution.filter round trip via "case__in".
func (a *Adapter) executionsForCases(ctx context.Context, testKeys []string) ([]kiwiBugExecution, error) {
	ids := make([]int, 0, len(testKeys))
	for _, key := range testKeys {
		id, err := parseKiwiID(key)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	var execs []kiwiTestExecution
	if err := a.c.call(ctx, "TestExecution.filter", []any{map[string]any{"case__in": ids}}, &execs); err != nil {
		return nil, err
	}

	out := make([]kiwiBugExecution, 0, len(execs))
	for _, e := range execs {
		out = append(out, kiwiBugExecution{ID: e.ID, CaseKey: fmt.Sprint(e.Case)})
	}
	return out, nil
}

// ListBugs harvests the bug links off the executions covering the given tests.
//
// Kiwi holds only the link, so each returned Bug carries just its key: the
// summary, status and priority live in the tracker and are filled in by the
// caller (the syncer hydrates them through the bug backend). Returning
// key-only records keeps this adapter free of any Jira knowledge.
func (a *Adapter) ListBugs(ctx context.Context, testProjectKey string, testKeys []string, issueType string, onProgress func(done, total int)) ([]backend.Bug, []backend.BugLink, error) {
	if a.bugBrowseBase == "" || len(testKeys) == 0 {
		return nil, nil, nil
	}

	execs, err := a.executionsForCases(ctx, testKeys)
	if err != nil {
		return nil, nil, err
	}

	seen := map[string]struct{}{}
	bugs := []backend.Bug{}
	links := []backend.BugLink{}
	for i, ex := range execs {
		if err := ctx.Err(); err != nil {
			return bugs, links, err
		}
		var raw []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
			URL  string `json:"url"`
		}
		if err := a.c.call(ctx, "TestExecution.get_links", []any{map[string]any{
			"execution_id": ex.ID,
		}}, &raw); err != nil {
			return bugs, links, err
		}
		for _, l := range raw {
			key := a.bugKeyFromURL(l.URL)
			if key == "" {
				continue
			}
			if _, dup := seen[key]; !dup {
				seen[key] = struct{}{}
				bugs = append(bugs, backend.Bug{Key: key, IssueType: issueType})
			}
			links = append(links, backend.BugLink{
				TestKey: ex.CaseKey,
				BugKey:  key,
				LinkID:  fmt.Sprint(l.ID),
			})
		}
		if onProgress != nil {
			onProgress(i+1, len(execs))
		}
	}
	return bugs, links, nil
}
