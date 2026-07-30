package main

import (
	"fmt"

	"xray-test-manager/internal/spellcheck"
	"xray-test-manager/internal/testrepo"
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

	var texts []spellcheck.TestText
	offset := 0
	for {
		page, err := a.repo.ListTests(profileID, testrepo.Query{Limit: 500, Offset: offset})
		if err != nil {
			return nil, err
		}
		for _, tc := range page.Tests {
			texts = append(texts, spellcheck.TestText{
				Key:               tc.Key,
				Summary:           tc.Summary,
				Description:       tc.Description,
				CucumberScenario:  tc.CucumberScenario,
				GenericDefinition: tc.GenericDefinition,
			})
		}
		offset += len(page.Tests)
		if len(page.Tests) == 0 || offset >= page.Total {
			break
		}
	}
	return spellcheck.ScanTests(texts, checker), nil
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
		return fmt.Errorf("correction is stale — please re-scan")
	}
	newValue := cur[:offset] + replacement + cur[offset+length:]
	return a.repo.EditTestField(profileID, testKey, field, newValue)
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
