package store_test

import (
	"path/filepath"
	"testing"

	"xray-test-manager/internal/store"
)

func TestSchemaV36Topic2Tables(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "v36.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	if store.SchemaVersion() < 36 {
		t.Errorf("schemaVersion = %d, want >= 36", store.SchemaVersion())
	}
	db := st.DB()
	for _, tbl := range []string{"canonical_version", "change_request", "cr_member_decision"} {
		var name string
		if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&name); err != nil || name != tbl {
			t.Fatalf("table %q missing: err=%v", tbl, err)
		}
	}
	// New columns usable.
	probes := []string{
		`INSERT INTO canonical_version (profile_id,id,canonical_id,name,status,sort_order,created_at) VALUES ('p','v','c','2.40','stable',0,'2026-01-01T00:00:00Z')`,
		`INSERT INTO coverage_param_group (profile_id,id,canonical_id,version_id,name,sort_order) VALUES ('p','g','c','v','Session',0)`,
		`INSERT INTO canonical_requirement_member (profile_id,canonical_id,requirement_key,added_at,accepted_version_id) VALUES ('p','c','R-1','2026-01-01T00:00:00Z','v')`,
		`INSERT INTO change_request (profile_id,id,canonical_id,title,status,risk,created_at) VALUES ('p','cr','c','Add OAuth','proposed','low','2026-01-01T00:00:00Z')`,
		`INSERT INTO cr_member_decision (profile_id,cr_id,requirement_key,decision,updated_at) VALUES ('p','cr','R-1','can_accept','2026-01-01T00:00:00Z')`,
	}
	for _, q := range probes {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("probe failed: %v\n%s", err, q)
		}
	}
}

func TestSchemaV36MigrationIdempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "v36idem.db")
	for i := 0; i < 2; i++ {
		st, err := store.Open(p)
		if err != nil {
			t.Fatalf("open #%d: %v", i, err)
		}
		st.Close()
	}
}
