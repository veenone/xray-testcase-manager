// Package settings manages global (non-profile) application preferences
// (FR-12.2), stored as key-value rows in the local database.
package settings

import (
	"database/sql"
	"errors"
	"fmt"

	"xray-test-manager/internal/store"
)

const (
	keyDefaultProfileID    = "default_profile_id"
	keyTheme               = "theme"
	keyRequirementLinkType = "requirement_link_type"
)

// Settings holds the global application preferences.
type Settings struct {
	DefaultProfileID    string `json:"defaultProfileId"`
	Theme               string `json:"theme"`               // "light" | "dark" | "system" | "" (= light)
	RequirementLinkType string `json:"requirementLinkType"` // issue-link type for Test->Requirement coverage; default "tested by"
}

// Manager reads and writes global settings.
type Manager struct {
	db *sql.DB
}

// NewManager returns a settings manager backed by the given store.
func NewManager(s *store.Store) *Manager {
	return &Manager{db: s.DB()}
}

// Get returns the current settings, with zero values for anything unset.
// RequirementLinkType defaults to "tested by" when no value has been persisted.
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
	s.DefaultProfileID = def
	s.Theme = theme
	if rlt == "" {
		rlt = "tested by"
	}
	s.RequirementLinkType = rlt
	return s, nil
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
