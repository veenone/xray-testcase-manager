package bridge

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"xray-test-manager/internal/store"
)

// MappingStore persists Mapping values in the bridge_mapping table (schema
// v44), keyed by (workspace, source connection, target connection). It
// mirrors the shape of internal/connection's Manager: a thin wrapper over
// *sql.DB, JSON-encoding the Mapping into a single column rather than
// normalising StatusMap/FieldMap into rows — the mapping is always read and
// written as a whole, so JSON keeps this store trivial and schema-stable as
// Mapping's shape evolves.
type MappingStore struct {
	db *sql.DB
}

// NewMappingStore returns a bridge-mapping store backed by the given local
// store.
func NewMappingStore(s *store.Store) *MappingStore {
	return &MappingStore{db: s.DB()}
}

// Get returns the saved mapping for (workspace, source, target). ok is false
// when no mapping has been saved yet — the caller (App.GetBridgeMapping)
// falls back to DefaultMapping in that case.
func (m *MappingStore) Get(workspaceID, sourceConnectionID, targetConnectionID string) (Mapping, bool, error) {
	var raw string
	err := m.db.QueryRow(
		`SELECT mapping_json FROM bridge_mapping
		 WHERE workspace_id = ? AND source_connection_id = ? AND target_connection_id = ?`,
		workspaceID, sourceConnectionID, targetConnectionID,
	).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return Mapping{}, false, nil
	}
	if err != nil {
		return Mapping{}, false, fmt.Errorf("get bridge mapping: %w", err)
	}
	if raw == "" {
		return Mapping{}, true, nil
	}
	var mp Mapping
	if err := json.Unmarshal([]byte(raw), &mp); err != nil {
		return Mapping{}, false, fmt.Errorf("decode bridge mapping: %w", err)
	}
	return mp, true, nil
}

// Save upserts the mapping for (workspace, source, target), JSON-encoding it
// into mapping_json and stamping updated_at.
func (m *MappingStore) Save(workspaceID, sourceConnectionID, targetConnectionID string, mp Mapping) error {
	raw, err := json.Marshal(mp)
	if err != nil {
		return fmt.Errorf("encode bridge mapping: %w", err)
	}
	_, err = m.db.Exec(
		`INSERT INTO bridge_mapping
		   (workspace_id, source_connection_id, target_connection_id, mapping_json, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(workspace_id, source_connection_id, target_connection_id) DO UPDATE SET
		   mapping_json = excluded.mapping_json,
		   updated_at = excluded.updated_at`,
		workspaceID, sourceConnectionID, targetConnectionID, string(raw), time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("save bridge mapping: %w", err)
	}
	return nil
}
