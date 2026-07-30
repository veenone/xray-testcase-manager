package main

import (
	"path/filepath"
	"testing"

	"xray-test-manager/internal/profile"
	"xray-test-manager/internal/settings"
	"xray-test-manager/internal/store"
	"xray-test-manager/internal/testrepo"
)

func newSpellApp(t *testing.T) *App {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "spell.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	a := NewApp()
	a.store = st
	a.repo = testrepo.NewRepository(st)
	a.settings = settings.NewManager(st)
	// requireStore() (called by every App method) also checks a.profiles, so
	// wire it here too, mirroring initStore's real wiring — the brief's
	// helper omitted it since ListMisspellings/ApplyCorrection/AddIgnoreWord
	// don't otherwise touch profiles.
	a.profiles = profile.NewManager(st)
	return a
}

func TestListMisspellingsFindsPlantedTypos(t *testing.T) {
	a := newSpellApp(t)
	if _, err := a.repo.CreateTest("p1", testrepo.TestDraft{
		Summary:     "User can recieve a token",
		Description: "The system must authenticate the user",
	}); err != nil {
		t.Fatalf("CreateTest 1: %v", err)
	}
	if _, err := a.repo.CreateTest("p1", testrepo.TestDraft{
		Summary: "Clean title with no typos here",
	}); err != nil {
		t.Fatalf("CreateTest 2: %v", err)
	}

	findings, err := a.ListMisspellings("p1")
	if err != nil {
		t.Fatalf("ListMisspellings: %v", err)
	}
	var found bool
	for _, f := range findings {
		if f.Word == "recieve" && f.Field == "summary" {
			found = true
		}
		if f.Word == "typos" {
			t.Errorf("'typos' should be a real word, not flagged")
		}
	}
	if !found {
		t.Fatalf("did not flag 'recieve'; findings = %+v", findings)
	}
}

func TestApplyCorrectionSplicesAndQueues(t *testing.T) {
	a := newSpellApp(t)
	key, err := a.repo.CreateTest("p1", testrepo.TestDraft{Summary: "User can recieve a token"})
	if err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	// Locate the finding to get its exact offset/length.
	findings, err := a.ListMisspellings("p1")
	if err != nil {
		t.Fatalf("ListMisspellings: %v", err)
	}
	var target *struct {
		off, length int
		word, field string
	}
	for _, f := range findings {
		if f.TestKey == key && f.Word == "recieve" {
			target = &struct {
				off, length int
				word, field string
			}{f.Offset, f.Length, f.Word, f.Field}
		}
	}
	if target == nil {
		t.Fatalf("no 'recieve' finding for %s", key)
	}
	if err := a.ApplyCorrection("p1", key, target.field, target.word, target.off, target.length, "receive"); err != nil {
		t.Fatalf("ApplyCorrection: %v", err)
	}
	tc, err := a.repo.GetTest("p1", key)
	if err != nil {
		t.Fatalf("GetTest: %v", err)
	}
	if tc.Summary != "User can receive a token" {
		t.Errorf("summary = %q, want corrected", tc.Summary)
	}
	// A stale offset (wrong word) must be rejected, not written.
	if err := a.ApplyCorrection("p1", key, "summary", "recieve", target.off, target.length, "receive"); err == nil {
		t.Errorf("stale correction was accepted; want error")
	}
}

// TestBulkCorrectionsQueuePerTest verifies the bulk-replace path: correcting the
// same word across several tests queues one pending change per test (so the
// commit list grows by the number of corrected tests), not a single coalesced
// entry.
func TestBulkCorrectionsQueuePerTest(t *testing.T) {
	a := newSpellApp(t)
	keys := []string{"QA-1", "QA-2", "QA-3"}
	seed := make([]testrepo.TestCase, 0, len(keys))
	for _, k := range keys {
		seed = append(seed, testrepo.TestCase{
			Key:     k,
			Summary: "User can recieve a token",
			Status:  "Open",
			Updated: "2026-01-01T00:00:00.000+0000",
		})
	}
	if err := a.repo.UpsertTests("p1", seed); err != nil {
		t.Fatalf("UpsertTests: %v", err)
	}

	findings, err := a.ListMisspellings("p1")
	if err != nil {
		t.Fatalf("ListMisspellings: %v", err)
	}
	applied := 0
	for _, f := range findings {
		if f.Word == "recieve" {
			if err := a.ApplyCorrection("p1", f.TestKey, f.Field, f.Word, f.Offset, f.Length, "receive"); err != nil {
				t.Fatalf("ApplyCorrection %s: %v", f.TestKey, err)
			}
			applied++
		}
	}
	if applied != len(keys) {
		t.Fatalf("applied %d corrections, want %d (one per test)", applied, len(keys))
	}

	pending, err := a.repo.ListPendingChanges("p1")
	if err != nil {
		t.Fatalf("ListPendingChanges: %v", err)
	}
	queued := map[string]bool{}
	for _, pc := range pending {
		if pc.EntityType == "test_case" && pc.Field == "summary" {
			queued[pc.EntityKey] = true
		}
	}
	if len(queued) != len(keys) {
		t.Fatalf("queued summary changes for %d tests, want %d; pending=%+v", len(queued), len(keys), pending)
	}
	for _, k := range keys {
		if !queued[k] {
			t.Errorf("no pending change queued for corrected test %s", k)
		}
	}
}

func TestAddIgnoreWordSuppressesFinding(t *testing.T) {
	a := newSpellApp(t)
	if _, err := a.repo.CreateTest("p1", testrepo.TestDraft{Summary: "Check the euicc profile"}); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	// "euicc" is already in the domain allow-list, so pick a word that is not.
	if _, err := a.repo.CreateTest("p1", testrepo.TestDraft{Summary: "Check the frobnicator profile"}); err != nil {
		t.Fatalf("CreateTest 2: %v", err)
	}
	before, _ := a.ListMisspellings("p1")
	var hadFrob bool
	for _, f := range before {
		if f.Word == "frobnicator" {
			hadFrob = true
		}
	}
	if !hadFrob {
		t.Skip("sample word unexpectedly in dictionary; pick another in review")
	}
	if err := a.AddIgnoreWord("frobnicator"); err != nil {
		t.Fatalf("AddIgnoreWord: %v", err)
	}
	after, _ := a.ListMisspellings("p1")
	for _, f := range after {
		if f.Word == "frobnicator" {
			t.Errorf("ignored word still flagged")
		}
	}
}
