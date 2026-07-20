package bridge_test

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	_ "modernc.org/sqlite"

	"xray-test-manager/internal/bridge"
	"xray-test-manager/internal/store"
)

func newMappingStore(t *testing.T) *bridge.MappingStore {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return bridge.NewMappingStore(st)
}

// TestBridgeMappingMigrationIsAdditiveAndIdempotent seeds a pre-v44 database
// (schema_version 43, no bridge_mapping table — the exact shape B1-B3 left
// the schema in) and confirms store.Open both creates the table and is safe
// to call twice, matching the pattern of the existing v2->v3 migration test.
func TestBridgeMappingMigrationIsAdditiveAndIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "upgrade.db")

	seed, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	if _, err := seed.Exec(`
		CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
		INSERT INTO meta (key, value) VALUES ('schema_version', '43');
	`); err != nil {
		t.Fatalf("seed pre-v44 layout: %v", err)
	}
	_ = seed.Close()

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("upgrade Open failed: %v", err)
	}
	defer st.Close()

	var name string
	if err := st.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='bridge_mapping'`,
	).Scan(&name); err != nil {
		t.Fatalf("bridge_mapping table missing after upgrade: %v", err)
	}

	// Re-opening must not error (idempotent migration).
	st2, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("second open after migration failed: %v", err)
	}
	_ = st2.Close()

	if store.SchemaVersion() < 44 {
		t.Errorf("schemaVersion = %d, want >= 44", store.SchemaVersion())
	}
}

func TestMappingStore_GetMissingReturnsNotOK(t *testing.T) {
	ms := newMappingStore(t)
	_, ok, err := ms.Get("w1", "src", "tgt")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for an unsaved mapping")
	}
}

func TestMappingStore_SaveGetRoundTrip(t *testing.T) {
	ms := newMappingStore(t)

	want := bridge.Mapping{
		StatusMap:      map[string]string{"Open": "idle", "Passed": "pass"},
		StepMode:       bridge.StepModeFlatten,
		FieldMap:       map[string]string{"priority": "importance"},
		UnmappedPolicy: bridge.UnmappedPolicyKeepInHub,
	}
	if err := ms.Save("w1", "src", "tgt", want); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, ok, err := ms.Get("w1", "src", "tgt")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true after save")
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round-tripped mapping = %+v, want %+v", got, want)
	}

	// A different (source, target) pair on the same workspace must stay
	// unaffected (the primary key is the full triple).
	if _, ok, err := ms.Get("w1", "src", "other-target"); err != nil || ok {
		t.Errorf("unrelated (source, target) pair should not have a saved mapping: ok=%v err=%v", ok, err)
	}

	// Save again (upsert) with a changed mapping and confirm it overwrites,
	// not duplicates, the row.
	updated := want
	updated.UnmappedPolicy = bridge.UnmappedPolicyDrop
	if err := ms.Save("w1", "src", "tgt", updated); err != nil {
		t.Fatalf("save (update): %v", err)
	}
	got2, ok, err := ms.Get("w1", "src", "tgt")
	if err != nil || !ok {
		t.Fatalf("get after update: ok=%v err=%v", ok, err)
	}
	if got2.UnmappedPolicy != bridge.UnmappedPolicyDrop {
		t.Errorf("UnmappedPolicy after update = %q, want %q", got2.UnmappedPolicy, bridge.UnmappedPolicyDrop)
	}
}
