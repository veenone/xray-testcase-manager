package store_test

import (
	"path/filepath"
	"testing"

	"xray-test-manager/internal/store"
)

// TestExternalRefPutGetRoundTrip covers the Phase 6 bridge task B5 accessors:
// a Get before any Put reports ok=false, a Put makes it visible, and a second
// Put on the same (workspace, entityType, localID, connection) key updates
// the row in place rather than erroring or duplicating it — the upsert the
// publish engine's resumability depends on.
func TestExternalRefPutGetRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "external_ref_accessors.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	const (
		workspace  = "ws-1"
		entityType = "test"
		localID    = "QA-1"
		connection = "kiwi-target"
	)

	// Get of an absent row returns ok=false, no error.
	if key, ok, err := st.ExternalRef(workspace, entityType, localID, connection); err != nil {
		t.Fatalf("get absent: %v", err)
	} else if ok {
		t.Fatalf("get absent: ok = true, want false (key=%q)", key)
	}

	// Put then Get round-trips.
	if err := st.PutExternalRef(workspace, entityType, localID, connection, "1001", "v1"); err != nil {
		t.Fatalf("put: %v", err)
	}
	key, ok, err := st.ExternalRef(workspace, entityType, localID, connection)
	if err != nil {
		t.Fatalf("get after put: %v", err)
	}
	if !ok {
		t.Fatalf("get after put: ok = false, want true")
	}
	if key != "1001" {
		t.Fatalf("get after put: externalKey = %q, want %q", key, "1001")
	}

	// A second Put on the same PK updates rather than erroring or duplicating.
	if err := st.PutExternalRef(workspace, entityType, localID, connection, "1001", "v2"); err != nil {
		t.Fatalf("second put: %v", err)
	}
	key, ok, err = st.ExternalRef(workspace, entityType, localID, connection)
	if err != nil {
		t.Fatalf("get after second put: %v", err)
	}
	if !ok || key != "1001" {
		t.Fatalf("get after second put: (%q, %v), want (\"1001\", true)", key, ok)
	}

	var count int
	if err := st.DB().QueryRow(
		`SELECT COUNT(*) FROM external_ref WHERE profile_id = ? AND entity_type = ? AND local_id = ? AND connection = ?`,
		workspace, entityType, localID, connection,
	).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("row count after second put = %d, want 1 (upsert, not duplicate)", count)
	}

	// A different connection for the same local entity is a distinct row —
	// this is exactly what dual-publish needs (one local_id, two
	// external_ref rows, one per connection).
	if err := st.PutExternalRef(workspace, entityType, localID, "xray-source", "QA-1", ""); err != nil {
		t.Fatalf("put second connection: %v", err)
	}
	if key, ok, err := st.ExternalRef(workspace, entityType, localID, "xray-source"); err != nil || !ok || key != "QA-1" {
		t.Fatalf("get second connection: (%q, %v, %v), want (\"QA-1\", true, nil)", key, ok, err)
	}
	if key, ok, err := st.ExternalRef(workspace, entityType, localID, connection); err != nil || !ok || key != "1001" {
		t.Fatalf("original connection unaffected: (%q, %v, %v), want (\"1001\", true, nil)", key, ok, err)
	}
}
