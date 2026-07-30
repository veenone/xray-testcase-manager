package main

import (
	"fmt"

	"xray-test-manager/internal/spellcheck"
	"xray-test-manager/internal/syncer"
	"xray-test-manager/internal/testrepo"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ListMisspellings scans every synced test in the profile for spelling errors
// across summary, description, and the Cucumber/Generic bodies. It reads only
// the local store (works fully offline) and folds the user's ignore list into
// the checker.
func (a *App) ListMisspellings(profileID string) (findings []spellcheck.Finding, err error) {
	defer recoverToError("ListMisspellings", &err)
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	ignore, err := a.settings.GetIgnoreWords()
	if err != nil {
		return nil, err
	}
	checker := spellcheck.NewDefaultChecker(ignore)

	// Emit progress on a dedicated channel so the view can show a bar (the scan
	// is O(tests) and can take a few seconds over a large repository). Each page
	// is scanned as it is fetched, so progress reflects real work, not just the
	// paging. A final Done event clears the bar.
	const stage = "Scanning for typos"
	// emit is nil-ctx safe so the method is unit-testable without a Wails
	// lifecycle context.
	emit := func(pr syncer.Progress) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "spellcheck:progress", pr)
		}
	}
	emit(syncer.Progress{Stage: stage})
	defer emit(syncer.Progress{Done: true})

	offset := 0
	for {
		page, err := a.repo.ListTests(profileID, testrepo.Query{Limit: 500, Offset: offset})
		if err != nil {
			return nil, err
		}
		texts := make([]spellcheck.TestText, 0, len(page.Tests))
		for _, tc := range page.Tests {
			texts = append(texts, spellcheck.TestText{
				Key:               tc.Key,
				Summary:           tc.Summary,
				Description:       tc.Description,
				CucumberScenario:  tc.CucumberScenario,
				GenericDefinition: tc.GenericDefinition,
			})
		}
		findings = append(findings, spellcheck.ScanTests(texts, checker)...)
		offset += len(page.Tests)
		emit(syncer.Progress{Stage: stage, Fetched: offset, Total: page.Total})
		if len(page.Tests) == 0 || offset >= page.Total {
			break
		}
	}
	return findings, nil
}

// ApplyCorrection replaces the flagged word at [offset,offset+length) in the
// given field with replacement, then routes the edit through the existing
// pending-change pipeline. The word argument is validated against the current
// text so a stale finding (edited since the scan) is rejected rather than
// corrupting the field.
func (a *App) ApplyCorrection(profileID, testKey, field, word string, offset, length int, replacement string) (err error) {
	defer recoverToError("ApplyCorrection", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	tc, err := a.repo.GetTest(profileID, testKey)
	if err != nil {
		return err
	}
	var cur string
	switch field {
	case "summary":
		cur = tc.Summary
	case "description":
		cur = tc.Description
	case "cucumber_scenario":
		cur = tc.CucumberScenario
	case "generic_definition":
		cur = tc.GenericDefinition
	default:
		return fmt.Errorf("field %q is not spellcheck-correctable", field)
	}
	if offset < 0 || length <= 0 || offset+length > len(cur) || cur[offset:offset+length] != word {
		return fmt.Errorf("correction is stale, please re-scan")
	}
	newValue := cur[:offset] + replacement + cur[offset+length:]
	// Record a spellcheck-specific audit message for the activity.
	note := fmt.Sprintf("Spellcheck: replaced %q with %q in %s", word, replacement, field)
	return a.repo.EditTestFieldWithAudit(profileID, testKey, field, newValue, "spellcheck-fix", note)
}

// AddIgnoreWord adds a word to the global spellcheck ignore list so future
// scans skip it across all profiles.
func (a *App) AddIgnoreWord(word string) (err error) {
	defer recoverToError("AddIgnoreWord", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.settings.AddIgnoreWord(word)
}

// GetIgnoreWords returns the global spellcheck ignore list (lowercased words),
// so the frontend can display and manage it.
func (a *App) GetIgnoreWords() (words []string, err error) {
	defer recoverToError("GetIgnoreWords", &err)
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.settings.GetIgnoreWords()
}

// RemoveIgnoreWord drops a word from the global spellcheck ignore list, so it is
// flagged again on future scans.
func (a *App) RemoveIgnoreWord(word string) (err error) {
	defer recoverToError("RemoveIgnoreWord", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.settings.RemoveIgnoreWord(word)
}
