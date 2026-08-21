package settings_test

import (
	"path/filepath"
	"testing"

	"xray-test-manager/internal/settings"
	"xray-test-manager/internal/store"
)

func newManager(t *testing.T) *settings.Manager {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return settings.NewManager(st)
}

// newManagerWithStore is newManager plus the store, for tests that need to
// write a raw row behind the manager's back.
func newManagerWithStore(t *testing.T) (*settings.Manager, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return settings.NewManager(st), st
}

// TestTourSeenVersionRoundTrips covers the onboarding tour's "already seen"
// marker (RND_P_4TFINT_05-335).
func TestTourSeenVersionRoundTrips(t *testing.T) {
	m := newManager(t)

	got, err := m.Get()
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.TourSeenVersion != 0 {
		t.Errorf("fresh install has TourSeenVersion %d, want 0 (never seen)", got.TourSeenVersion)
	}

	if err := m.SetTourSeenVersion(1); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err = m.Get()
	if err != nil {
		t.Fatalf("get after set: %v", err)
	}
	if got.TourSeenVersion != 1 {
		t.Errorf("got TourSeenVersion %d, want 1", got.TourSeenVersion)
	}
}

// TestTourSeenVersionIgnoresUnparsableValue checks a hand-edited or corrupted
// row degrades to "never seen" rather than erroring the whole settings load,
// which runs at startup and would otherwise break the app.
func TestTourSeenVersionIgnoresUnparsableValue(t *testing.T) {
	m, st := newManagerWithStore(t)
	if err := m.SetTourSeenVersion(3); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, err := st.DB().Exec(
		`INSERT INTO app_setting (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		"tour_seen_version", "not-a-number",
	); err != nil {
		t.Fatalf("write raw setting: %v", err)
	}

	got, err := m.Get()
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.TourSeenVersion != 0 {
		t.Errorf("got %d, want 0 for an unparsable value", got.TourSeenVersion)
	}
}

func TestDefaultSettingsAreEmpty(t *testing.T) {
	m := newManager(t)
	s, err := m.Get()
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if s.DefaultProfileID != "" {
		t.Errorf("DefaultProfileID = %q, want empty by default", s.DefaultProfileID)
	}
}

func TestSetAndGetDefaultProfile(t *testing.T) {
	m := newManager(t)

	if err := m.SetDefaultProfileID("abc-123"); err != nil {
		t.Fatalf("set: %v", err)
	}

	s, _ := m.Get()
	if s.DefaultProfileID != "abc-123" {
		t.Errorf("DefaultProfileID = %q, want abc-123", s.DefaultProfileID)
	}

	// Overwrite (upsert).
	if err := m.SetDefaultProfileID("def-456"); err != nil {
		t.Fatalf("set 2: %v", err)
	}
	s, _ = m.Get()
	if s.DefaultProfileID != "def-456" {
		t.Errorf("DefaultProfileID = %q, want def-456 after overwrite", s.DefaultProfileID)
	}

	// Theme persists independently of the default profile.
	if err := m.SetTheme("dark"); err != nil {
		t.Fatalf("set theme: %v", err)
	}
	s, _ = m.Get()
	if s.Theme != "dark" || s.DefaultProfileID != "def-456" {
		t.Errorf("settings = %+v, want theme dark + default def-456", s)
	}
}

func TestIgnoreWordsRoundTrip(t *testing.T) {
	m := newManager(t)

	if words, err := m.GetIgnoreWords(); err != nil || len(words) != 0 {
		t.Fatalf("initial GetIgnoreWords = %v, %v; want empty", words, err)
	}
	if err := m.AddIgnoreWord("  EUICC "); err != nil {
		t.Fatalf("AddIgnoreWord: %v", err)
	}
	if err := m.AddIgnoreWord("euicc"); err != nil { // duplicate (post-normalise)
		t.Fatalf("AddIgnoreWord dup: %v", err)
	}
	if err := m.AddIgnoreWord("widgetized"); err != nil {
		t.Fatalf("AddIgnoreWord 2: %v", err)
	}
	words, err := m.GetIgnoreWords()
	if err != nil {
		t.Fatalf("GetIgnoreWords: %v", err)
	}
	if len(words) != 2 || words[0] != "euicc" || words[1] != "widgetized" {
		t.Errorf("words = %v, want [euicc widgetized]", words)
	}
}

func TestRemoveIgnoreWord(t *testing.T) {
	m := newManager(t)
	for _, w := range []string{"euicc", "widgetized", "pkcs"} {
		if err := m.AddIgnoreWord(w); err != nil {
			t.Fatalf("AddIgnoreWord %q: %v", w, err)
		}
	}
	// Remove is normalised (trim/lowercase) and drops only the match.
	if err := m.RemoveIgnoreWord("  Widgetized "); err != nil {
		t.Fatalf("RemoveIgnoreWord: %v", err)
	}
	// Removing an absent word is a no-op, not an error.
	if err := m.RemoveIgnoreWord("nothere"); err != nil {
		t.Fatalf("RemoveIgnoreWord absent: %v", err)
	}
	words, err := m.GetIgnoreWords()
	if err != nil {
		t.Fatalf("GetIgnoreWords: %v", err)
	}
	if len(words) != 2 || words[0] != "euicc" || words[1] != "pkcs" {
		t.Errorf("words = %v, want [euicc pkcs]", words)
	}
}
