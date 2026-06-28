package store_test

import (
	"path/filepath"
	"testing"

	"xray-test-manager/internal/store"
)

// TestSchemaV35CoverageTablesExist asserts that the coverage module's six
// local tables are created on a fresh install and that the schema version is at
// least 35.
func TestSchemaV35CoverageTablesExist(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "v35.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if store.SchemaVersion() < 35 {
		t.Errorf("schemaVersion = %d, want >= 35", store.SchemaVersion())
	}

	db := st.DB()
	want := []string{
		"canonical_requirement",
		"canonical_requirement_member",
		"coverage_param_group",
		"coverage_parameter",
		"coverage_param_value",
		"coverage_value_test",
	}
	for _, tbl := range want {
		var name string
		err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl,
		).Scan(&name)
		if err != nil || name != tbl {
			t.Fatalf("expected table %q to exist, err=%v name=%q", tbl, err, name)
		}
	}
}

// TestSchemaV35CoverageColumnsUsable probes the key columns of each coverage
// table so a missing/renamed column fails loudly rather than silently.
func TestSchemaV35CoverageColumnsUsable(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "v35cols.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	db := st.DB()

	stmts := []string{
		`INSERT INTO canonical_requirement (profile_id, id, name, category, description, created_at, updated_at)
		 VALUES ('p1','c1','C_Sign','PKCS11','','2026-01-01T00:00:00Z','')`,
		`INSERT INTO canonical_requirement_member (profile_id, canonical_id, requirement_key, added_at)
		 VALUES ('p1','c1','FUNC-1','2026-01-01T00:00:00Z')`,
		`INSERT INTO coverage_param_group (profile_id, id, canonical_id, name, sort_order)
		 VALUES ('p1','g1','c1','Mechanism',1)`,
		`INSERT INTO coverage_parameter (profile_id, id, group_id, name, kind, description, sort_order)
		 VALUES ('p1','pa1','g1','pMechanism','value','',1)`,
		`INSERT INTO coverage_param_value (profile_id, id, parameter_id, value_label, value_kind, error_code, is_required, notes, sort_order)
		 VALUES ('p1','v1','pa1','CKM_RSA_PKCS','value','',1,'',1)`,
		`INSERT INTO coverage_value_test (profile_id, value_id, test_key, created_at)
		 VALUES ('p1','v1','QA-100','2026-01-01T00:00:00Z')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("probe insert failed (column missing?): %v\n%s", err, s)
		}
	}
}

// TestSchemaV35MigrationIdempotent verifies re-opening a v35 database does not
// error (the migration block guards on "already exists").
func TestSchemaV35MigrationIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v35idem.db")
	for i := range 2 {
		st, err := store.Open(dbPath)
		if err != nil {
			t.Fatalf("open #%d: %v", i+1, err)
		}
		st.Close()
	}
}
