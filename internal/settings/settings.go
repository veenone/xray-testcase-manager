// Package settings manages global (non-profile) application preferences
// (FR-12.2), stored as key-value rows in the local database.
package settings

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"xray-test-manager/internal/store"
)

const (
	keyDefaultProfileID    = "default_profile_id"
	keyTheme               = "theme"
	keyRequirementLinkType = "requirement_link_type"
	keyShowCoverage        = "show_coverage"
	keySpellcheckIgnore    = "spellcheck_ignore_words"
)

// Settings holds the global application preferences.
type Settings struct {
	DefaultProfileID    string `json:"defaultProfileId"`
	Theme               string `json:"theme"`               // "light" | "dark" | "system" | "" (= light)
	RequirementLinkType string `json:"requirementLinkType"` // issue-link type NAME for Test->Requirement coverage; empty = auto-resolve by direction
	// ShowCoverage reveals the Coverage top-nav tab. The Coverage module is
	// opt-in, so it is hidden by default until the user enables it.
	ShowCoverage bool `json:"showCoverage"`
}

// Manager reads and writes global settings.
type Manager struct {
	db *sql.DB
	// ignoreMu serializes the read-modify-write of the spellcheck ignore list,
	// which Wails can invoke concurrently (each bound call runs in its own
	// goroutine). Without it, two quick Add/Remove calls could each read the
	// same list and the second write would drop the first's change.
	ignoreMu sync.Mutex
}

// NewManager returns a settings manager backed by the given store.
func NewManager(s *store.Store) *Manager {
	return &Manager{db: s.DB()}
}

// Get returns the current settings, with zero values for anything unset.
// RequirementLinkType is empty when unset, meaning the coverage link type is
// auto-resolved from the instance at commit time.
func (m *Manager) Get() (Settings, error) {
	var s Settings
	def, err := m.value(keyDefaultProfileID)
	if err != nil {
		return Settings{}, err
	}
	theme, err := m.value(keyTheme)
	if err != nil {
		return Settings{}, err
	}
	rlt, err := m.value(keyRequirementLinkType)
	if err != nil {
		return Settings{}, err
	}
	showCov, err := m.value(keyShowCoverage)
	if err != nil {
		return Settings{}, err
	}
	s.DefaultProfileID = def
	s.Theme = theme
	// An unset value means "auto-resolve": the backend picks the instance's
	// coverage link type by direction at commit time. Do NOT substitute a
	// literal name here; "tested by" is a direction label, not a link-type
	// name, and would 404 if sent as type.name. The dropdown fills in a
	// sensible selection from the instance's real link types.
	s.RequirementLinkType = rlt
	// Default false (hidden) when unset or unparsable.
	s.ShowCoverage, _ = strconv.ParseBool(showCov)
	return s, nil
}

// SetShowCoverage records whether the Coverage top-nav tab is shown.
func (m *Manager) SetShowCoverage(v bool) error {
	return m.setValue(keyShowCoverage, strconv.FormatBool(v))
}

// SetDefaultProfileID records which profile is auto-selected on launch.
func (m *Manager) SetDefaultProfileID(id string) error {
	return m.setValue(keyDefaultProfileID, id)
}

// SetTheme records the colour theme preference.
func (m *Manager) SetTheme(theme string) error {
	return m.setValue(keyTheme, theme)
}

// SetRequirementLinkType records which Jira issue-link type is used when linking
// a Test to a Requirement (FR-13 / #275). An empty name clears the explicit
// setting, reverting to auto-resolve on the next commit.
func (m *Manager) SetRequirementLinkType(name string) error {
	return m.setValue(keyRequirementLinkType, name)
}

// GetIgnoreWords returns the user's persisted spellcheck ignore list
// (lowercased words), empty when none are set.
func (m *Manager) GetIgnoreWords() ([]string, error) {
	raw, err := m.value(keySpellcheckIgnore)
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, nil
	}
	var words []string
	if err := json.Unmarshal([]byte(raw), &words); err != nil {
		return nil, fmt.Errorf("parse ignore words: %w", err)
	}
	return words, nil
}

// AddIgnoreWord adds a word (lowercased, trimmed) to the ignore list. No-op for
// blank input or a word already present. The read-modify-write is serialized by
// ignoreMu so concurrent calls cannot lose an update.
func (m *Manager) AddIgnoreWord(word string) error {
	word = strings.ToLower(strings.TrimSpace(word))
	if word == "" {
		return nil
	}
	m.ignoreMu.Lock()
	defer m.ignoreMu.Unlock()
	words, err := m.GetIgnoreWords()
	if err != nil {
		return err
	}
	for _, w := range words {
		if w == word {
			return nil
		}
	}
	words = append(words, word)
	return m.saveIgnoreWords(words)
}

// RemoveIgnoreWord drops a word (lowercased, trimmed) from the ignore list.
// No-op for blank input or a word not present. Serialized by ignoreMu.
func (m *Manager) RemoveIgnoreWord(word string) error {
	word = strings.ToLower(strings.TrimSpace(word))
	if word == "" {
		return nil
	}
	m.ignoreMu.Lock()
	defer m.ignoreMu.Unlock()
	words, err := m.GetIgnoreWords()
	if err != nil {
		return err
	}
	kept := make([]string, 0, len(words))
	for _, w := range words {
		if w != word {
			kept = append(kept, w)
		}
	}
	if len(kept) == len(words) {
		return nil
	}
	return m.saveIgnoreWords(kept)
}

// saveIgnoreWords persists the ignore list as JSON. Callers hold ignoreMu.
func (m *Manager) saveIgnoreWords(words []string) error {
	b, err := json.Marshal(words)
	if err != nil {
		return fmt.Errorf("encode ignore words: %w", err)
	}
	return m.setValue(keySpellcheckIgnore, string(b))
}

func (m *Manager) value(key string) (string, error) {
	var v string
	err := m.db.QueryRow(`SELECT value FROM app_setting WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read setting %q: %w", key, err)
	}
	return v, nil
}

func (m *Manager) setValue(key, value string) error {
	if _, err := m.db.Exec(
		`INSERT INTO app_setting (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	); err != nil {
		return fmt.Errorf("write setting %q: %w", key, err)
	}
	return nil
}
