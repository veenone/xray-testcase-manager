# Topic 2 — Versioning & Change-Request Tracking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-version parameter coverage plus local change-request tracking to the `xray-test-manager` coverage module, with no Jira admin required.

**Architecture:** Extend `internal/coverage` in place. The parameter model re-roots from the canonical requirement onto a new `canonical_version`; coverage is measured per version. Change requests and per-customer decisions are new local tables. The bound `App` layer and a new frontend "Versions & CRs" surface expose it.

**Tech Stack:** Go 1.25 + Wails v2, SQLite (modernc.org/sqlite, raw SQL), React 19 + TypeScript + Vite, excelize for XLSX.

## Global Constraints

- All new tables are profile-scoped (`profile_id` leading PK column) and prefixed `canonical_*` / `change_request` / `cr_member_decision`. (verbatim from spec)
- No Jira admin: all state local; no new server-side Jira schema, automation, or notifications. (verbatim from spec)
- `testrepo` must never import `coverage` (Go enforces no cycle); coverage reuses `testrepo.ConsolidatedRunByTest` / `testrepo.DeriveCoverage` for run status. (verbatim from spec)
- Schema migrations follow `baseSchema → applyMigrations → indexSchema`; migration blocks tolerate "already exists" / "duplicate column" to stay idempotent.
- Go IDs use `github.com/google/uuid`; timestamps use `nowISO()` (RFC3339 UTC).
- Run from repo root `C:/projects/xray-test-manager`. Go tests: `go test ./internal/coverage/`. Frontend typecheck: `cd frontend && npx tsc --noEmit`. Wails bindings: `wails generate module`.

---

## File Structure

- `internal/store/store.go` — schema v36: 3 new tables, 2 column adds, indexes, migration.
- `internal/coverage/versions.go` — version CRUD + `CloneVersion` + `SetMemberVersion`. (new)
- `internal/coverage/changerequest.go` — CR CRUD + `SetCRDecision` + `CRImpact`. (new)
- `internal/coverage/dashboard.go` — `VersionDistribution` + `CRAdoption`. (new)
- `internal/coverage/parameters.go` — `GetParamModel`, `UpsertNode` group branch: version-scoped. (modify)
- `internal/coverage/compute.go` — `ComputeCoverage`, `ListGaps`, `liveTestsByValue`: version-scoped. (modify)
- `internal/coverage/importtemplate.go` — `ImportCoverageTemplate(profileID, versionID, data)`: version-scoped. (modify)
- `internal/coverage/export.go` — `ExportReport(profileID, versionID)`. (modify)
- `internal/coverage/demo.go` — seed two versions + a CR. (modify)
- `app_coverage.go` — ~15 new methods; existing coverage methods take `versionId`. (modify)
- `frontend/src/api.ts` — re-export new methods + new interfaces. (modify)
- `frontend/src/components/CoverageView.tsx` — version selector + Versions & CRs tab. (modify)
- `frontend/src/components/VersionBar.tsx`, `ChangeRequestsPanel.tsx`, `VersionDashboard.tsx` — new components. (new)

A `version_id` flows: `canonical_version.id` → `coverage_param_group.version_id`. Members carry `accepted_version_id`. CRs carry `canonical_id` + optional `target_version_id`; decisions carry `cr_id` + `requirement_key`.

---

### Task 1: Schema v36 — tables, columns, migration

**Files:**
- Modify: `internal/store/store.go` (`schemaVersion`, `baseSchema`, `applyMigrations`, `indexSchema`)
- Test: `internal/store/store_topic2_test.go` (new)

**Interfaces:**
- Produces: tables `canonical_version(profile_id,id,canonical_id,name,status,notes,sort_order,created_at)`, `change_request(profile_id,id,canonical_id,cr_key,title,status,target_version_id,risk,description,created_at,updated_at)`, `cr_member_decision(profile_id,cr_id,requirement_key,decision,note,updated_at)`; columns `coverage_param_group.version_id`, `canonical_requirement_member.accepted_version_id`. Schema version 36.

- [ ] **Step 1: Write the failing test**

Create `internal/store/store_topic2_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestSchemaV36 -v`
Expected: FAIL (`no such table: canonical_version`, and schemaVersion 35).

- [ ] **Step 3: Bump version + add tables to baseSchema**

In `internal/store/store.go`: change `const schemaVersion = 35` → `36`.

Append to `baseSchema` (before the closing backtick, after the coverage tables):

```sql
CREATE TABLE IF NOT EXISTS canonical_version (
	profile_id   TEXT NOT NULL,
	id           TEXT NOT NULL,
	canonical_id TEXT NOT NULL,
	name         TEXT NOT NULL,
	status       TEXT NOT NULL DEFAULT 'stable',
	notes        TEXT NOT NULL DEFAULT '',
	sort_order   INTEGER NOT NULL DEFAULT 0,
	created_at   TEXT NOT NULL,
	PRIMARY KEY (profile_id, id)
);
CREATE TABLE IF NOT EXISTS change_request (
	profile_id        TEXT NOT NULL,
	id                TEXT NOT NULL,
	canonical_id      TEXT NOT NULL,
	cr_key            TEXT NOT NULL DEFAULT '',
	title             TEXT NOT NULL,
	status            TEXT NOT NULL DEFAULT 'proposed',
	target_version_id TEXT NOT NULL DEFAULT '',
	risk              TEXT NOT NULL DEFAULT 'low',
	description       TEXT NOT NULL DEFAULT '',
	created_at        TEXT NOT NULL,
	updated_at        TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (profile_id, id)
);
CREATE TABLE IF NOT EXISTS cr_member_decision (
	profile_id      TEXT NOT NULL,
	cr_id           TEXT NOT NULL,
	requirement_key TEXT NOT NULL,
	decision        TEXT NOT NULL DEFAULT 'pending',
	note            TEXT NOT NULL DEFAULT '',
	updated_at      TEXT NOT NULL,
	PRIMARY KEY (profile_id, cr_id, requirement_key)
);
```

Add `version_id TEXT NOT NULL DEFAULT ''` to the `coverage_param_group` CREATE (after `canonical_id`), and `accepted_version_id TEXT NOT NULL DEFAULT ''` to the `canonical_requirement_member` CREATE (after `added_at`).

- [ ] **Step 4: Add the migration block**

After the `if current < 35` block in `applyMigrations`, add:

```go
// v36: Topic 2 — versions, change requests, per-customer decisions, and the
// version-rooting of the parameter model. Additive; backfills a default version.
if current < 36 {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS canonical_version (
			profile_id TEXT NOT NULL, id TEXT NOT NULL, canonical_id TEXT NOT NULL,
			name TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'stable',
			notes TEXT NOT NULL DEFAULT '', sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL, PRIMARY KEY (profile_id, id))`,
		`CREATE TABLE IF NOT EXISTS change_request (
			profile_id TEXT NOT NULL, id TEXT NOT NULL, canonical_id TEXT NOT NULL,
			cr_key TEXT NOT NULL DEFAULT '', title TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'proposed', target_version_id TEXT NOT NULL DEFAULT '',
			risk TEXT NOT NULL DEFAULT 'low', description TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (profile_id, id))`,
		`CREATE TABLE IF NOT EXISTS cr_member_decision (
			profile_id TEXT NOT NULL, cr_id TEXT NOT NULL, requirement_key TEXT NOT NULL,
			decision TEXT NOT NULL DEFAULT 'pending', note TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL, PRIMARY KEY (profile_id, cr_id, requirement_key))`,
	} {
		if _, err := db.Exec(q); err != nil && !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("v36 create tables: %w", err)
		}
	}
	for _, stmt := range []string{
		`ALTER TABLE coverage_param_group ADD COLUMN version_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE canonical_requirement_member ADD COLUMN accepted_version_id TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("v36 add columns: %w", err)
		}
	}
	// Backfill: a default version per canonical, re-rooting existing groups.
	rows, err := db.Query(`SELECT profile_id, id FROM canonical_requirement`)
	if err != nil {
		return fmt.Errorf("v36 read canonicals: %w", err)
	}
	type cv struct{ profileID, canonID string }
	var canons []cv
	for rows.Next() {
		var c cv
		if err := rows.Scan(&c.profileID, &c.canonID); err != nil {
			rows.Close()
			return err
		}
		canons = append(canons, c)
	}
	rows.Close()
	for _, c := range canons {
		verID := c.canonID + "-v1"
		if _, err := db.Exec(
			`INSERT OR IGNORE INTO canonical_version (profile_id,id,canonical_id,name,status,sort_order,created_at)
			 VALUES (?,?,?,?,'stable',0,'2026-01-01T00:00:00Z')`,
			c.profileID, verID, c.canonID, "v1"); err != nil {
			return fmt.Errorf("v36 seed version: %w", err)
		}
		if _, err := db.Exec(
			`UPDATE coverage_param_group SET version_id = ? WHERE profile_id = ? AND canonical_id = ? AND version_id = ''`,
			verID, c.profileID, c.canonID); err != nil {
			return fmt.Errorf("v36 reroot groups: %w", err)
		}
	}
}
```

- [ ] **Step 5: Add indexes**

Append to `indexSchema`:

```sql
CREATE INDEX IF NOT EXISTS idx_canon_version_canon ON canonical_version(profile_id, canonical_id);
CREATE INDEX IF NOT EXISTS idx_change_request_canon ON change_request(profile_id, canonical_id);
CREATE INDEX IF NOT EXISTS idx_cr_decision_cr        ON cr_member_decision(profile_id, cr_id);
CREATE INDEX IF NOT EXISTS idx_cr_decision_req       ON cr_member_decision(profile_id, requirement_key);
CREATE INDEX IF NOT EXISTS idx_cov_group_version     ON coverage_param_group(profile_id, version_id);
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/store/ -run TestSchemaV36 -v`
Expected: PASS (both tests).

- [ ] **Step 7: Commit**

```bash
git add internal/store/store.go internal/store/store_topic2_test.go
git commit -m "feat(store): schema v36 — versions, change requests, version-rooted groups"
```

---

### Task 2: Version CRUD + clone + member lock (`versions.go`)

**Files:**
- Create: `internal/coverage/versions.go`
- Test: `internal/coverage/versions_test.go`

**Interfaces:**
- Consumes: `Module` (has `db *sql.DB`), `nowISO()`, `uuid.NewString()` (from existing package files).
- Produces:
  - `type Version struct { ID, Name, Status, Notes string; SortOrder int; CreatedAt string }`
  - `(m *Module) ListVersions(profileID, canonicalID string) ([]Version, error)`
  - `(m *Module) CreateVersion(profileID, canonicalID, name, status, notes string) (string, error)`
  - `(m *Module) RenameVersion(profileID, id, name, status, notes string) error`
  - `(m *Module) SetVersionStatus(profileID, id, status string) error`
  - `(m *Module) DeleteVersion(profileID, id string) error` (cascades groups→params→values→mappings under the version)
  - `(m *Module) CloneVersion(profileID, sourceVersionID, name, status string) (string, error)` (deep-copies model + mappings)
  - `(m *Module) SetMemberVersion(profileID, canonicalID, requirementKey, versionID string) error`

- [ ] **Step 1: Write the failing test**

Create `internal/coverage/versions_test.go`:

```go
package coverage

import "testing"

func TestVersionCRUDAndClone(t *testing.T) {
	m, st := newTestModule(t)
	const p = "p1"
	cid, err := m.CreateCanonical(p, "C_Sign", "", "")
	if err != nil {
		t.Fatal(err)
	}
	v1, err := m.CreateVersion(p, cid, "2.40", "stable", "")
	if err != nil {
		t.Fatalf("create version: %v", err)
	}
	// Build a small model under v1: group→param→value, with a mapping.
	gid, _ := m.UpsertNode(p, NodeEdit{Kind: "group", VersionID: v1, Name: "Mechanism"})
	pid, _ := m.UpsertNode(p, NodeEdit{Kind: "parameter", GroupID: gid, Name: "pMech"})
	vid, _ := m.UpsertNode(p, NodeEdit{Kind: "value", ParameterID: pid, Name: "RSA_PKCS", IsRequired: true})
	seedTest(t, st, p, "QA-1", "PASS", "")
	if err := m.SetValueTests(p, vid, []string{"QA-1"}); err != nil {
		t.Fatal(err)
	}

	// Clone v1 -> v2: full model + mapping copied, source untouched, new ids.
	v2, err := m.CloneVersion(p, v1, "3.0", "beta")
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	m1, _ := m.GetParamModel(p, v1)
	m2, _ := m.GetParamModel(p, v2)
	if len(m2.Groups) != 1 || len(m2.Groups[0].Parameters) != 1 || len(m2.Groups[0].Parameters[0].Values) != 1 {
		t.Fatalf("clone model shape wrong: %+v", m2)
	}
	if m2.Groups[0].ID == m1.Groups[0].ID {
		t.Error("cloned group must have a new id")
	}
	// Cloned value carries the mapping.
	clonedVID := m2.Groups[0].Parameters[0].Values[0].ID
	keys, _ := m.ListValueTests(p, clonedVID)
	if len(keys) != 1 || keys[0] != "QA-1" {
		t.Errorf("cloned mapping = %v, want [QA-1]", keys)
	}

	vers, _ := m.ListVersions(p, cid)
	if len(vers) != 2 {
		t.Fatalf("versions = %d, want 2", len(vers))
	}

	// Member lock.
	if _, err := st.DB().Exec(`INSERT INTO requirement (profile_id, jira_key, project_key) VALUES (?, 'R-1', 'CUST')`, p); err != nil {
		t.Fatal(err)
	}
	if err := m.SetMembers(p, cid, []string{"R-1"}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetMemberVersion(p, cid, "R-1", v2); err != nil {
		t.Fatalf("set member version: %v", err)
	}

	// DeleteVersion cascades.
	if err := m.DeleteVersion(p, v1); err != nil {
		t.Fatalf("delete version: %v", err)
	}
	if vers, _ := m.ListVersions(p, cid); len(vers) != 1 {
		t.Errorf("after delete, versions = %d, want 1", len(vers))
	}
	var groups int
	st.DB().QueryRow(`SELECT COUNT(*) FROM coverage_param_group WHERE profile_id=? AND version_id=?`, p, v1).Scan(&groups)
	if groups != 0 {
		t.Errorf("deleted version still has %d groups", groups)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/coverage/ -run TestVersionCRUDAndClone -v`
Expected: FAIL (undefined `CreateVersion`, `NodeEdit.VersionID`, etc.).

- [ ] **Step 3: Add `VersionID` to `NodeEdit` and version-root the group upsert**

In `internal/coverage/parameters.go`, add a field to `NodeEdit` (after `CanonicalID`):

```go
	// group (Topic 2: groups root at a version, not the canonical)
	VersionID string `json:"versionId"`
```

Change `upsertGroup` to insert `version_id` instead of `canonical_id`:

```go
func (m *Module) upsertGroup(profileID string, n NodeEdit) (string, error) {
	if n.Name == "" {
		return "", fmt.Errorf("group name is required")
	}
	if n.ID == "" {
		if n.VersionID == "" {
			return "", fmt.Errorf("versionId is required for a new group")
		}
		id := uuid.NewString()
		_, err := m.db.Exec(
			`INSERT INTO coverage_param_group (profile_id, id, canonical_id, version_id, name, sort_order)
			 VALUES (?, ?, '', ?, ?, ?)`,
			profileID, id, n.VersionID, n.Name, n.SortOrder)
		return id, err
	}
	_, err := m.db.Exec(
		`UPDATE coverage_param_group SET name = ?, sort_order = ? WHERE profile_id = ? AND id = ?`,
		n.Name, n.SortOrder, profileID, n.ID)
	return n.ID, err
}
```

- [ ] **Step 4: Write `versions.go`**

Create `internal/coverage/versions.go`:

```go
package coverage

import (
	"fmt"

	"github.com/google/uuid"
)

// Version is one release line of a functional requirement; the parameter model
// (and therefore coverage) is measured per version.
type Version struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Notes     string `json:"notes"`
	SortOrder int    `json:"sortOrder"`
	CreatedAt string `json:"createdAt"`
}

func (m *Module) ListVersions(profileID, canonicalID string) ([]Version, error) {
	rows, err := m.db.Query(
		`SELECT id, name, status, notes, sort_order, created_at FROM canonical_version
		  WHERE profile_id = ? AND canonical_id = ? ORDER BY sort_order, name`,
		profileID, canonicalID)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}
	defer rows.Close()
	out := []Version{}
	for rows.Next() {
		var v Version
		if err := rows.Scan(&v.ID, &v.Name, &v.Status, &v.Notes, &v.SortOrder, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (m *Module) CreateVersion(profileID, canonicalID, name, status, notes string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("version name is required")
	}
	if status == "" {
		status = "planning"
	}
	var n int
	m.db.QueryRow(`SELECT COUNT(*) FROM canonical_version WHERE profile_id=? AND canonical_id=?`, profileID, canonicalID).Scan(&n)
	id := uuid.NewString()
	if _, err := m.db.Exec(
		`INSERT INTO canonical_version (profile_id, id, canonical_id, name, status, notes, sort_order, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		profileID, id, canonicalID, name, status, notes, n, nowISO()); err != nil {
		return "", fmt.Errorf("create version: %w", err)
	}
	return id, nil
}

func (m *Module) RenameVersion(profileID, id, name, status, notes string) error {
	if name == "" {
		return fmt.Errorf("version name is required")
	}
	res, err := m.db.Exec(
		`UPDATE canonical_version SET name=?, status=?, notes=? WHERE profile_id=? AND id=?`,
		name, status, notes, profileID, id)
	if err != nil {
		return fmt.Errorf("rename version: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("version %q not found", id)
	}
	return nil
}

func (m *Module) SetVersionStatus(profileID, id, status string) error {
	_, err := m.db.Exec(`UPDATE canonical_version SET status=? WHERE profile_id=? AND id=?`, status, profileID, id)
	return err
}

// DeleteVersion removes a version and the entire parameter model beneath it.
func (m *Module) DeleteVersion(profileID, id string) error {
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmts := []string{
		`DELETE FROM coverage_value_test WHERE profile_id=? AND value_id IN (
			SELECT v.id FROM coverage_param_value v
			JOIN coverage_parameter p ON p.profile_id=v.profile_id AND p.id=v.parameter_id
			JOIN coverage_param_group g ON g.profile_id=p.profile_id AND g.id=p.group_id
			WHERE g.profile_id=? AND g.version_id=?)`,
		`DELETE FROM coverage_param_value WHERE profile_id=? AND parameter_id IN (
			SELECT p.id FROM coverage_parameter p
			JOIN coverage_param_group g ON g.profile_id=p.profile_id AND g.id=p.group_id
			WHERE g.profile_id=? AND g.version_id=?)`,
		`DELETE FROM coverage_parameter WHERE profile_id=? AND group_id IN (
			SELECT id FROM coverage_param_group WHERE profile_id=? AND version_id=?)`,
		`DELETE FROM coverage_param_group WHERE profile_id=? AND version_id=?`,
	}
	for i, q := range stmts {
		args := []any{profileID, profileID, id}
		if i == len(stmts)-1 {
			args = []any{profileID, id}
		}
		if _, err := tx.Exec(q, args...); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM canonical_version WHERE profile_id=? AND id=?`, profileID, id); err != nil {
		return err
	}
	return tx.Commit()
}

// CloneVersion deep-copies a source version's groups→parameters→values and their
// test mappings into a new version of the same canonical.
func (m *Module) CloneVersion(profileID, sourceVersionID, name, status string) (string, error) {
	var canonicalID string
	if err := m.db.QueryRow(
		`SELECT canonical_id FROM canonical_version WHERE profile_id=? AND id=?`,
		profileID, sourceVersionID).Scan(&canonicalID); err != nil {
		return "", fmt.Errorf("source version not found: %w", err)
	}
	newVer, err := m.CreateVersion(profileID, canonicalID, name, status, "")
	if err != nil {
		return "", err
	}
	model, err := m.GetParamModel(profileID, sourceVersionID)
	if err != nil {
		return "", err
	}
	tx, err := m.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	now := nowISO()
	for _, g := range model.Groups {
		gid := uuid.NewString()
		if _, err := tx.Exec(
			`INSERT INTO coverage_param_group (profile_id,id,canonical_id,version_id,name,sort_order) VALUES (?,?,'',?,?,?)`,
			profileID, gid, newVer, g.Name, g.SortOrder); err != nil {
			return "", err
		}
		for _, par := range g.Parameters {
			pid := uuid.NewString()
			if _, err := tx.Exec(
				`INSERT INTO coverage_parameter (profile_id,id,group_id,name,kind,description,sort_order) VALUES (?,?,?,?,?,?,?)`,
				profileID, pid, gid, par.Name, par.Kind, par.Description, par.SortOrder); err != nil {
				return "", err
			}
			for _, v := range par.Values {
				vid := uuid.NewString()
				req := 0
				if v.IsRequired {
					req = 1
				}
				if _, err := tx.Exec(
					`INSERT INTO coverage_param_value (profile_id,id,parameter_id,value_label,value_kind,error_code,is_required,notes,sort_order)
					 VALUES (?,?,?,?,?,?,?,?,?)`,
					profileID, vid, pid, v.ValueLabel, v.ValueKind, v.ErrorCode, req, v.Notes, v.SortOrder); err != nil {
					return "", err
				}
				// Copy mappings from the source value.
				if _, err := tx.Exec(
					`INSERT OR IGNORE INTO coverage_value_test (profile_id,value_id,test_key,created_at)
					 SELECT profile_id, ?, test_key, ? FROM coverage_value_test WHERE profile_id=? AND value_id=?`,
					vid, now, profileID, v.ID); err != nil {
					return "", err
				}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return newVer, nil
}

// SetMemberVersion records which version a member (customer requirement) is on.
func (m *Module) SetMemberVersion(profileID, canonicalID, requirementKey, versionID string) error {
	res, err := m.db.Exec(
		`UPDATE canonical_requirement_member SET accepted_version_id=? WHERE profile_id=? AND canonical_id=? AND requirement_key=?`,
		versionID, profileID, canonicalID, requirementKey)
	if err != nil {
		return fmt.Errorf("set member version: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("member %q not found under canonical %q", requirementKey, canonicalID)
	}
	return nil
}
```

- [ ] **Step 5: Run test — it still fails (GetParamModel not yet version-scoped)**

Run: `go test ./internal/coverage/ -run TestVersionCRUDAndClone -v`
Expected: FAIL or compile error — `GetParamModel` still takes `canonicalID`. Proceed to Task 3, which re-scopes it; this test passes after Task 3.

- [ ] **Step 6: Commit (engine only)**

```bash
git add internal/coverage/versions.go internal/coverage/versions_test.go internal/coverage/parameters.go
git commit -m "feat(coverage): version CRUD, clone, member lock (engine)"
```

---

### Task 3: Version-scope the model/compute/import/export functions

**Files:**
- Modify: `internal/coverage/parameters.go` (`GetParamModel`), `internal/coverage/compute.go` (`ComputeCoverage`, `ListGaps`, `liveTestsByValue`), `internal/coverage/importtemplate.go` (`ImportCoverageTemplate`, `clearModel`), `internal/coverage/export.go` (`ExportReport`)
- Modify (tests): `internal/coverage/compute_test.go`, `canonical_test.go`, `importtemplate_test.go`, `export_test.go`, `demo_test.go`, `template_test.go`, `parameters_test.go` — update the shared `buildModel` helper.

**Interfaces:**
- Consumes: `canonical_version`, `coverage_param_group.version_id` (Task 1).
- Produces (changed signatures):
  - `GetParamModel(profileID, versionID string) (ParamModel, error)` — `ParamModel.VersionID` replaces `CanonicalID`.
  - `ComputeCoverage(profileID, versionID string) (CoverageReport, error)` — `CoverageReport.VersionID`.
  - `ListGaps(profileID, versionID string) ([]Gap, error)`
  - `ImportCoverageTemplate(profileID, versionID string, data []byte) (ImportSummary, error)`
  - `ExportReport(profileID, versionID string) ([]byte, error)`
  - Test helper `buildModel(t, m, profileID, groupName, values) (canonicalID, versionID, parameterID string, valueIDs []string)`.

- [ ] **Step 1: Re-scope `GetParamModel`**

In `parameters.go`: change the signature to `func (m *Module) GetParamModel(profileID, versionID string) (ParamModel, error)`, set `ParamModel{VersionID: versionID, ...}`, and change every `WHERE ... canonical_id = ?` in its three queries to filter the **group** query by `WHERE g.profile_id = ? AND g.version_id = ?` (the param/value queries already join through the group — change their `g.canonical_id = ?` to `g.version_id = ?`). In `ParamModel` struct, rename the JSON field: `VersionID string \`json:"versionId"\``.

- [ ] **Step 2: Re-scope `compute.go`**

In `compute.go`: `liveTestsByValue(profileID, versionID)`, `ComputeCoverage(profileID, versionID)`, `ListGaps(profileID, versionID)` — change every `g.canonical_id = ?` to `g.version_id = ?` and rename the param to `versionID`. In `CoverageReport`, rename `CanonicalID` → `VersionID` (`json:"versionId"`).

- [ ] **Step 3: Re-scope `importtemplate.go` and `export.go`**

In `importtemplate.go`: `ImportCoverageTemplate(profileID, versionID string, data []byte)`. Replace the canonical-existence check with a version-existence check (`SELECT COUNT(*) FROM canonical_version WHERE profile_id=? AND id=?`). Pass `versionID` to `importValueSheet`/`importFlatSheet` (which insert groups with `version_id` — update their group INSERT to `(profile_id,id,canonical_id,version_id,name,sort_order) VALUES (?,?,'',?,?,?)`). Change `clearModel(tx, profileID, versionID)` to clear by `g.version_id`.

In `export.go`: `ExportReport(profileID, versionID string)`. Load the version name (`SELECT name FROM canonical_version`) for the Summary sheet; call `GetParamModel(profileID, versionID)`, `ComputeCoverage(profileID, versionID)`, `ListGaps(profileID, versionID)`.

- [ ] **Step 4: Update the shared test helper `buildModel`**

In `compute_test.go`, change `buildModel` to create a version and root the group there:

```go
func buildModel(t *testing.T, m *Module, profileID, groupName string, values []string) (string, string, string, []string) {
	t.Helper()
	cid, err := m.CreateCanonical(profileID, "C_Sign", "PKCS11", "")
	if err != nil {
		t.Fatalf("create canonical: %v", err)
	}
	vid, err := m.CreateVersion(profileID, cid, "1.0", "stable", "")
	if err != nil {
		t.Fatalf("create version: %v", err)
	}
	gid, err := m.UpsertNode(profileID, NodeEdit{Kind: "group", VersionID: vid, Name: groupName})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	pid, err := m.UpsertNode(profileID, NodeEdit{Kind: "parameter", GroupID: gid, Name: "pParam"})
	if err != nil {
		t.Fatalf("create parameter: %v", err)
	}
	ids := make([]string, len(values))
	for i, label := range values {
		v, err := m.UpsertNode(profileID, NodeEdit{Kind: "value", ParameterID: pid, Name: label, IsRequired: true, SortOrder: i})
		if err != nil {
			t.Fatalf("create value %q: %v", label, err)
		}
		ids[i] = v
	}
	return cid, vid, pid, ids
}
```

Update every caller: `cid, _, _, vids := buildModel(...)` → use `vid` for compute/gaps/model calls. Specifically in `compute_test.go`, `parameters_test.go`: replace `m.ComputeCoverage(p, cid)` → `m.ComputeCoverage(p, vid)`, `m.ListGaps(p, cid)` → `m.ListGaps(p, vid)`, `m.GetParamModel(p, cid)` → `m.GetParamModel(p, vid)`. In `parameters_test.go` `TestDeleteNodeCascades`, the model is built inline — add a `CreateVersion` and pass `VersionID` to the group `UpsertNode` (mirror the helper).

- [ ] **Step 5: Update import/export/demo/template tests to create a version**

- `importtemplate_test.go` (`TestImportRealTemplate`): after `CreateCanonical`, add `vid, _ := m.CreateVersion(p, cid, "1.0", "stable", "")`; call `m.ImportCoverageTemplate(p, vid, data)` and `m.ComputeCoverage(p, vid)`.
- `template_test.go` (`TestGeneratedTemplateRoundTrips`): same — create a version, import into it, `GetParamModel(p, vid)`.
- `export_test.go` (`TestExportReportRoundTrips`): `buildModel` now returns `vid`; call `m.ExportReport(p, vid)`.
- `canonical_test.go` (`TestCanonicalCRUDAndReuse`): unaffected by version scoping (no model/compute calls) — leave as is.

- [ ] **Step 6: Run the whole coverage suite**

Run: `go test ./internal/coverage/ -v`
Expected: PASS — including `TestVersionCRUDAndClone` (Task 2) now that `GetParamModel` is version-scoped, and all updated existing tests.

- [ ] **Step 7: Commit**

```bash
git add internal/coverage/
git commit -m "refactor(coverage): version-scope model, compute, import, export"
```

---

### Task 4: Change requests + decisions + impact (`changerequest.go`)

**Files:**
- Create: `internal/coverage/changerequest.go`
- Test: `internal/coverage/changerequest_test.go`

**Interfaces:**
- Produces:
  - `type ChangeRequest struct { ID, CRKey, Title, Status, TargetVersionID, Risk, Description, CreatedAt, UpdatedAt string }`
  - `type CRDecision struct { RequirementKey, ProjectKey, Decision, Note string }`
  - `type CRImpactResult struct { CR ChangeRequest; Decisions []CRDecision; CanAccept, CannotAccept, Pending int }`
  - `ListChangeRequests(profileID, canonicalID string) ([]ChangeRequest, error)`
  - `CreateChangeRequest(profileID, canonicalID, crKey, title, status, targetVersionID, risk, description string) (string, error)`
  - `UpdateChangeRequest(profileID, id, crKey, title, status, targetVersionID, risk, description string) error`
  - `DeleteChangeRequest(profileID, id string) error`
  - `SetCRDecision(profileID, crID, requirementKey, decision, note string) error`
  - `CRImpact(profileID, crID string) (CRImpactResult, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/coverage/changerequest_test.go`:

```go
package coverage

import "testing"

func TestChangeRequestLifecycleAndImpact(t *testing.T) {
	m, st := newTestModule(t)
	const p = "p1"
	cid, _ := m.CreateCanonical(p, "Login", "", "")
	v2, _ := m.CreateVersion(p, cid, "2.0", "beta", "")

	// Two member requirements.
	for _, k := range []string{"BANK-1", "SAMSU-1"} {
		st.DB().Exec(`INSERT INTO requirement (profile_id, jira_key, project_key) VALUES (?, ?, 'CUST')`, p, k)
	}
	m.SetMembers(p, cid, []string{"BANK-1", "SAMSU-1"})

	crID, err := m.CreateChangeRequest(p, cid, "CHG-1", "Add OAuth", "approved", v2, "low", "")
	if err != nil {
		t.Fatalf("create CR: %v", err)
	}
	if list, _ := m.ListChangeRequests(p, cid); len(list) != 1 || list[0].Title != "Add OAuth" {
		t.Fatalf("list CRs = %+v", list)
	}

	if err := m.SetCRDecision(p, crID, "BANK-1", "cannot_accept", "breaks API"); err != nil {
		t.Fatal(err)
	}
	if err := m.SetCRDecision(p, crID, "SAMSU-1", "can_accept", ""); err != nil {
		t.Fatal(err)
	}

	imp, err := m.CRImpact(p, crID)
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	if imp.CanAccept != 1 || imp.CannotAccept != 1 || imp.Pending != 0 {
		t.Errorf("tallies = %d/%d/%d, want 1/1/0", imp.CanAccept, imp.CannotAccept, imp.Pending)
	}
	if len(imp.Decisions) != 2 {
		t.Errorf("decisions = %d, want 2 (one per member)", len(imp.Decisions))
	}

	if err := m.DeleteChangeRequest(p, crID); err != nil {
		t.Fatal(err)
	}
	if list, _ := m.ListChangeRequests(p, cid); len(list) != 0 {
		t.Errorf("after delete, CRs = %d, want 0", len(list))
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/coverage/ -run TestChangeRequestLifecycleAndImpact -v`
Expected: FAIL (undefined `CreateChangeRequest`).

- [ ] **Step 3: Write `changerequest.go`**

Create `internal/coverage/changerequest.go`:

```go
package coverage

import (
	"fmt"

	"github.com/google/uuid"
)

type ChangeRequest struct {
	ID              string `json:"id"`
	CRKey           string `json:"crKey"`
	Title           string `json:"title"`
	Status          string `json:"status"`
	TargetVersionID string `json:"targetVersionId"`
	Risk            string `json:"risk"`
	Description     string `json:"description"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

type CRDecision struct {
	RequirementKey string `json:"requirementKey"`
	ProjectKey     string `json:"projectKey"`
	Decision       string `json:"decision"`
	Note           string `json:"note"`
}

type CRImpactResult struct {
	CR           ChangeRequest `json:"cr"`
	Decisions    []CRDecision  `json:"decisions"`
	CanAccept    int           `json:"canAccept"`
	CannotAccept int           `json:"cannotAccept"`
	Pending      int           `json:"pending"`
}

func (m *Module) ListChangeRequests(profileID, canonicalID string) ([]ChangeRequest, error) {
	rows, err := m.db.Query(
		`SELECT id, cr_key, title, status, target_version_id, risk, description, created_at, updated_at
		   FROM change_request WHERE profile_id=? AND canonical_id=? ORDER BY created_at DESC`,
		profileID, canonicalID)
	if err != nil {
		return nil, fmt.Errorf("list change requests: %w", err)
	}
	defer rows.Close()
	out := []ChangeRequest{}
	for rows.Next() {
		var c ChangeRequest
		if err := rows.Scan(&c.ID, &c.CRKey, &c.Title, &c.Status, &c.TargetVersionID, &c.Risk, &c.Description, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (m *Module) CreateChangeRequest(profileID, canonicalID, crKey, title, status, targetVersionID, risk, description string) (string, error) {
	if title == "" {
		return "", fmt.Errorf("title is required")
	}
	if status == "" {
		status = "proposed"
	}
	if risk == "" {
		risk = "low"
	}
	id := uuid.NewString()
	now := nowISO()
	if _, err := m.db.Exec(
		`INSERT INTO change_request (profile_id,id,canonical_id,cr_key,title,status,target_version_id,risk,description,created_at,updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		profileID, id, canonicalID, crKey, title, status, targetVersionID, risk, description, now, now); err != nil {
		return "", fmt.Errorf("create change request: %w", err)
	}
	return id, nil
}

func (m *Module) UpdateChangeRequest(profileID, id, crKey, title, status, targetVersionID, risk, description string) error {
	if title == "" {
		return fmt.Errorf("title is required")
	}
	res, err := m.db.Exec(
		`UPDATE change_request SET cr_key=?, title=?, status=?, target_version_id=?, risk=?, description=?, updated_at=?
		  WHERE profile_id=? AND id=?`,
		crKey, title, status, targetVersionID, risk, description, nowISO(), profileID, id)
	if err != nil {
		return fmt.Errorf("update change request: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("change request %q not found", id)
	}
	return nil
}

func (m *Module) DeleteChangeRequest(profileID, id string) error {
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM cr_member_decision WHERE profile_id=? AND cr_id=?`, profileID, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM change_request WHERE profile_id=? AND id=?`, profileID, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (m *Module) SetCRDecision(profileID, crID, requirementKey, decision, note string) error {
	if decision == "" {
		decision = "pending"
	}
	_, err := m.db.Exec(
		`INSERT INTO cr_member_decision (profile_id, cr_id, requirement_key, decision, note, updated_at)
		 VALUES (?,?,?,?,?,?)
		 ON CONFLICT(profile_id, cr_id, requirement_key) DO UPDATE SET decision=excluded.decision, note=excluded.note, updated_at=excluded.updated_at`,
		profileID, crID, requirementKey, decision, note, nowISO())
	if err != nil {
		return fmt.Errorf("set CR decision: %w", err)
	}
	return nil
}

// CRImpact returns the CR plus, for every member of its canonical, that member's
// decision (defaulting to pending), with tallies.
func (m *Module) CRImpact(profileID, crID string) (CRImpactResult, error) {
	var res CRImpactResult
	var canonicalID string
	c := &res.CR
	if err := m.db.QueryRow(
		`SELECT id, canonical_id, cr_key, title, status, target_version_id, risk, description, created_at, updated_at
		   FROM change_request WHERE profile_id=? AND id=?`, profileID, crID).
		Scan(&c.ID, &canonicalID, &c.CRKey, &c.Title, &c.Status, &c.TargetVersionID, &c.Risk, &c.Description, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return res, fmt.Errorf("change request not found: %w", err)
	}
	rows, err := m.db.Query(
		`SELECT mm.requirement_key, COALESCE(r.project_key,''), COALESCE(d.decision,'pending'), COALESCE(d.note,'')
		   FROM canonical_requirement_member mm
		   LEFT JOIN requirement r ON r.profile_id=mm.profile_id AND r.jira_key=mm.requirement_key
		   LEFT JOIN cr_member_decision d ON d.profile_id=mm.profile_id AND d.cr_id=? AND d.requirement_key=mm.requirement_key
		  WHERE mm.profile_id=? AND mm.canonical_id=?
		  ORDER BY r.project_key, mm.requirement_key`,
		crID, profileID, canonicalID)
	if err != nil {
		return res, fmt.Errorf("read decisions: %w", err)
	}
	defer rows.Close()
	res.Decisions = []CRDecision{}
	for rows.Next() {
		var d CRDecision
		if err := rows.Scan(&d.RequirementKey, &d.ProjectKey, &d.Decision, &d.Note); err != nil {
			return res, err
		}
		switch d.Decision {
		case "can_accept":
			res.CanAccept++
		case "cannot_accept":
			res.CannotAccept++
		default:
			res.Pending++
		}
		res.Decisions = append(res.Decisions, d)
	}
	return res, rows.Err()
}
```

- [ ] **Step 4: Run test to verify pass**

Run: `go test ./internal/coverage/ -run TestChangeRequestLifecycleAndImpact -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/coverage/changerequest.go internal/coverage/changerequest_test.go
git commit -m "feat(coverage): change requests, per-customer decisions, impact"
```

---

### Task 5: Dashboards (`dashboard.go`)

**Files:**
- Create: `internal/coverage/dashboard.go`
- Test: `internal/coverage/dashboard_test.go`

**Interfaces:**
- Produces:
  - `type VersionShare struct { VersionID, VersionName, Status string; MemberCount int }`
  - `VersionDistribution(profileID, canonicalID string) ([]VersionShare, error)`
  - `type CRShare struct { CRID, Title, Status string; CanAccept, CannotAccept, Pending int }`
  - `CRAdoption(profileID, canonicalID string) ([]CRShare, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/coverage/dashboard_test.go`:

```go
package coverage

import "testing"

func TestDashboards(t *testing.T) {
	m, st := newTestModule(t)
	const p = "p1"
	cid, _ := m.CreateCanonical(p, "Login", "", "")
	v1, _ := m.CreateVersion(p, cid, "1.0", "stable", "")
	v2, _ := m.CreateVersion(p, cid, "2.0", "beta", "")
	for _, k := range []string{"BANK-1", "SAMSU-1", "TELCO-1"} {
		st.DB().Exec(`INSERT INTO requirement (profile_id, jira_key, project_key) VALUES (?, ?, 'CUST')`, p, k)
	}
	m.SetMembers(p, cid, []string{"BANK-1", "SAMSU-1", "TELCO-1"})
	m.SetMemberVersion(p, cid, "BANK-1", v1)
	m.SetMemberVersion(p, cid, "SAMSU-1", v2)
	m.SetMemberVersion(p, cid, "TELCO-1", v2)

	dist, err := m.VersionDistribution(p, cid)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, d := range dist {
		got[d.VersionName] = d.MemberCount
	}
	if got["1.0"] != 1 || got["2.0"] != 2 {
		t.Errorf("distribution = %v, want 1.0:1 2.0:2", got)
	}

	crID, _ := m.CreateChangeRequest(p, cid, "CHG-1", "Add OAuth", "approved", v2, "low", "")
	m.SetCRDecision(p, crID, "SAMSU-1", "can_accept", "")
	adopt, err := m.CRAdoption(p, cid)
	if err != nil {
		t.Fatal(err)
	}
	if len(adopt) != 1 || adopt[0].CanAccept != 1 || adopt[0].Pending != 2 {
		t.Errorf("adoption = %+v, want 1 CR with 1 can-accept / 2 pending", adopt)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/coverage/ -run TestDashboards -v`
Expected: FAIL (undefined `VersionDistribution`).

- [ ] **Step 3: Write `dashboard.go`**

Create `internal/coverage/dashboard.go`:

```go
package coverage

import "fmt"

type VersionShare struct {
	VersionID   string `json:"versionId"`
	VersionName string `json:"versionName"`
	Status      string `json:"status"`
	MemberCount int    `json:"memberCount"`
}

// VersionDistribution counts members locked to each version of a canonical.
// Members with no lock are reported under an empty version ("Unassigned").
func (m *Module) VersionDistribution(profileID, canonicalID string) ([]VersionShare, error) {
	rows, err := m.db.Query(
		`SELECT v.id, v.name, v.status,
		        (SELECT COUNT(*) FROM canonical_requirement_member mm
		           WHERE mm.profile_id=v.profile_id AND mm.canonical_id=v.canonical_id AND mm.accepted_version_id=v.id) AS members
		   FROM canonical_version v
		  WHERE v.profile_id=? AND v.canonical_id=? ORDER BY v.sort_order, v.name`,
		profileID, canonicalID)
	if err != nil {
		return nil, fmt.Errorf("version distribution: %w", err)
	}
	defer rows.Close()
	out := []VersionShare{}
	for rows.Next() {
		var s VersionShare
		if err := rows.Scan(&s.VersionID, &s.VersionName, &s.Status, &s.MemberCount); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var unassigned int
	m.db.QueryRow(
		`SELECT COUNT(*) FROM canonical_requirement_member WHERE profile_id=? AND canonical_id=? AND accepted_version_id=''`,
		profileID, canonicalID).Scan(&unassigned)
	if unassigned > 0 {
		out = append(out, VersionShare{VersionName: "Unassigned", MemberCount: unassigned})
	}
	return out, nil
}

type CRShare struct {
	CRID         string `json:"crId"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	CanAccept    int    `json:"canAccept"`
	CannotAccept int    `json:"cannotAccept"`
	Pending      int    `json:"pending"`
}

// CRAdoption summarises each CR of a canonical: accept/cannot/pending across all
// the canonical's members (pending = members without an explicit decision).
func (m *Module) CRAdoption(profileID, canonicalID string) ([]CRShare, error) {
	crs, err := m.ListChangeRequests(profileID, canonicalID)
	if err != nil {
		return nil, err
	}
	var memberCount int
	m.db.QueryRow(
		`SELECT COUNT(*) FROM canonical_requirement_member WHERE profile_id=? AND canonical_id=?`,
		profileID, canonicalID).Scan(&memberCount)
	out := []CRShare{}
	for _, cr := range crs {
		s := CRShare{CRID: cr.ID, Title: cr.Title, Status: cr.Status}
		rows, err := m.db.Query(
			`SELECT decision, COUNT(*) FROM cr_member_decision WHERE profile_id=? AND cr_id=? GROUP BY decision`,
			profileID, cr.ID)
		if err != nil {
			return nil, fmt.Errorf("CR adoption: %w", err)
		}
		decided := 0
		for rows.Next() {
			var d string
			var n int
			if err := rows.Scan(&d, &n); err != nil {
				rows.Close()
				return nil, err
			}
			switch d {
			case "can_accept":
				s.CanAccept = n
				decided += n
			case "cannot_accept":
				s.CannotAccept = n
				decided += n
			}
		}
		rows.Close()
		s.Pending = memberCount - decided
		if s.Pending < 0 {
			s.Pending = 0
		}
		out = append(out, s)
	}
	return out, nil
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/coverage/ -run TestDashboards -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/coverage/dashboard.go internal/coverage/dashboard_test.go
git commit -m "feat(coverage): version-distribution and CR-adoption dashboards"
```

---

### Task 6: Bound `App` methods + version-scope existing ones

**Files:**
- Modify: `app_coverage.go`
- Verify: `go build ./...`

**Interfaces:**
- Consumes: all `Module` methods from Tasks 2–5.
- Produces: 15 new bound methods + changed signatures on `GetParamModel`, `GetCoverageReport`, `ListCoverageGaps`, `ImportCoverageTemplate`, `ExportCoverageReport` (now take `versionId`).

- [ ] **Step 1: Change existing bound coverage methods to take `versionId`**

In `app_coverage.go`, update these to accept and forward `versionID` (matching the re-scoped module functions): `GetParamModel(profileID, versionID)`, `GetCoverageReport(profileID, versionID)` → `a.cov.ComputeCoverage(profileID, versionID)`, `ListCoverageGaps(profileID, versionID)`, `ImportCoverageTemplate(profileID, versionID)` (read file then `a.cov.ImportCoverageTemplate(profileID, versionID, data)`), `ExportCoverageReport(profileID, versionID)` → `a.cov.ExportReport(profileID, versionID)`.

- [ ] **Step 2: Add the 15 new bound methods**

Append to `app_coverage.go` (thin delegators; mutators get `defer recoverToError("Name", &err)`):

```go
// --- Coverage: versions (PRD Topic 2) ---

func (a *App) ListVersions(profileID, canonicalID string) ([]coverage.Version, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.cov.ListVersions(profileID, canonicalID)
}

func (a *App) CreateVersion(profileID, canonicalID, name, status, notes string) (id string, err error) {
	defer recoverToError("CreateVersion", &err)
	if err := a.requireStore(); err != nil {
		return "", err
	}
	return a.cov.CreateVersion(profileID, canonicalID, name, status, notes)
}

func (a *App) CloneVersion(profileID, sourceVersionID, name, status string) (id string, err error) {
	defer recoverToError("CloneVersion", &err)
	if err := a.requireStore(); err != nil {
		return "", err
	}
	return a.cov.CloneVersion(profileID, sourceVersionID, name, status)
}

func (a *App) RenameVersion(profileID, id, name, status, notes string) (err error) {
	defer recoverToError("RenameVersion", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.cov.RenameVersion(profileID, id, name, status, notes)
}

func (a *App) SetVersionStatus(profileID, id, status string) (err error) {
	defer recoverToError("SetVersionStatus", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.cov.SetVersionStatus(profileID, id, status)
}

func (a *App) DeleteVersion(profileID, id string) (err error) {
	defer recoverToError("DeleteVersion", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.cov.DeleteVersion(profileID, id)
}

func (a *App) SetMemberVersion(profileID, canonicalID, requirementKey, versionID string) (err error) {
	defer recoverToError("SetMemberVersion", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.cov.SetMemberVersion(profileID, canonicalID, requirementKey, versionID)
}

// --- Coverage: change requests (PRD Topic 2) ---

func (a *App) ListChangeRequests(profileID, canonicalID string) ([]coverage.ChangeRequest, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.cov.ListChangeRequests(profileID, canonicalID)
}

func (a *App) CreateChangeRequest(profileID, canonicalID, crKey, title, status, targetVersionID, risk, description string) (id string, err error) {
	defer recoverToError("CreateChangeRequest", &err)
	if err := a.requireStore(); err != nil {
		return "", err
	}
	return a.cov.CreateChangeRequest(profileID, canonicalID, crKey, title, status, targetVersionID, risk, description)
}

func (a *App) UpdateChangeRequest(profileID, id, crKey, title, status, targetVersionID, risk, description string) (err error) {
	defer recoverToError("UpdateChangeRequest", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.cov.UpdateChangeRequest(profileID, id, crKey, title, status, targetVersionID, risk, description)
}

func (a *App) DeleteChangeRequest(profileID, id string) (err error) {
	defer recoverToError("DeleteChangeRequest", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.cov.DeleteChangeRequest(profileID, id)
}

func (a *App) SetCRDecision(profileID, crID, requirementKey, decision, note string) (err error) {
	defer recoverToError("SetCRDecision", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.cov.SetCRDecision(profileID, crID, requirementKey, decision, note)
}

// --- Coverage: dashboards (PRD Topic 2) ---

func (a *App) GetVersionDistribution(profileID, canonicalID string) ([]coverage.VersionShare, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.cov.VersionDistribution(profileID, canonicalID)
}

func (a *App) GetCRAdoption(profileID, canonicalID string) ([]coverage.CRShare, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.cov.CRAdoption(profileID, canonicalID)
}

func (a *App) GetCRImpact(profileID, crID string) (coverage.CRImpactResult, error) {
	if err := a.requireStore(); err != nil {
		return coverage.CRImpactResult{Decisions: []coverage.CRDecision{}}, err
	}
	return a.cov.CRImpact(profileID, crID)
}
```

- [ ] **Step 2b: Update default-return shapes for the re-scoped reads**

In the re-scoped `GetParamModel`/`GetCoverageReport`, keep the existing zero-value returns (`coverage.ParamModel{Groups: []coverage.ParamGroup{}}`, `coverage.CoverageReport{Groups: []coverage.GroupCoverage{}, Values: map[string]coverage.ValueCoverage{}}`).

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: exit 0 (no errors).

- [ ] **Step 4: Commit**

```bash
git add app_coverage.go
git commit -m "feat(app): bind Topic 2 version/CR/dashboard methods; version-scope coverage reads"
```

---

### Task 7: Extend the demo seed with versions + a CR

**Files:**
- Modify: `internal/coverage/demo.go`
- Test: `internal/coverage/demo_test.go`

**Interfaces:**
- Consumes: `CreateVersion`, `CloneVersion`, `SetMemberVersion`, `CreateChangeRequest`, `SetCRDecision`; `GetParamModel`/`ComputeCoverage` now take a versionId.

- [ ] **Step 1: Update `SeedDemoExample` to root the model in a version + add v2 + a CR**

In `demo.go`, after `CreateCanonical`, create the primary version and root all groups there, then clone it and add a CR. Replace the group/value insertion to use a `versionID`, and after the model loop add:

```go
	// Topic 2: two versions (clone v1 -> v2) + a sample CR with decisions.
	v2, err := m.CloneVersion(profileID, versionID, "2.0", "beta")
	if err == nil {
		// Lock members across versions for a realistic distribution.
		members := m.demoMemberRequirements(profileID)
		for i, rk := range members {
			ver := versionID
			if i%2 == 1 {
				ver = v2
			}
			_ = m.SetMemberVersion(profileID, cid, rk, ver)
		}
		if crID, cerr := m.CreateChangeRequest(profileID, cid, "CHG-1001", "Add OAuth login", "approved", v2, "low",
			"Adds OAuth as an alternative login path in v2.0."); cerr == nil && len(members) > 0 {
			_ = m.SetCRDecision(profileID, crID, members[0], "can_accept", "")
			if len(members) > 1 {
				_ = m.SetCRDecision(profileID, crID, members[1], "cannot_accept", "legacy SSO only")
			}
		}
	}
```

Where the current code creates groups with `canonical_id`, change the group INSERT to root at a version: first `versionID, _ := m.CreateVersion(profileID, cid, "1.0", "stable", "")` (right after `CreateCanonical`), then in the group INSERT use `(profile_id,id,canonical_id,version_id,name,sort_order) VALUES (?,?,'',?,?,?)` passing `versionID`. The member-attachment loop that already exists stays.

- [ ] **Step 2: Update the demo test to assert versions + CR**

In `demo_test.go` `TestSeedDemoExampleAlignedWithDemoData`: after `SeedDemoExample`, the model/coverage calls must use a version. Fetch versions and use the first for `ComputeCoverage`:

```go
	vers, _ := m.ListVersions(p, cid)
	if len(vers) != 2 {
		t.Fatalf("versions = %d, want 2 (1.0 + cloned 2.0)", len(vers))
	}
	rep, err := m.ComputeCoverage(p, vers[0].ID)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if rep.TotalValues != 12 {
		t.Errorf("total values = %d, want 12", rep.TotalValues)
	}
	// CR present.
	crs, _ := m.ListChangeRequests(p, cid)
	if len(crs) != 1 || crs[0].Title != "Add OAuth login" {
		t.Errorf("CRs = %+v, want one 'Add OAuth login'", crs)
	}
```

Replace the old direct `m.GetParamModel(p, cid)` / `m.ComputeCoverage(p, cid)` calls in that test with `vers[0].ID`. Keep the `Valid credentials → DEMO-1` mapping assertion (look it up via the version's model).

- [ ] **Step 3: Run the test**

Run: `go test ./internal/coverage/ -run TestSeedDemoExampleAlignedWithDemoData -v`
Expected: PASS.

- [ ] **Step 4: Run the whole coverage + store suite**

Run: `go test ./internal/coverage/ ./internal/store/`
Expected: PASS (all).

- [ ] **Step 5: Commit**

```bash
git add internal/coverage/demo.go internal/coverage/demo_test.go
git commit -m "feat(coverage): demo seed gets two versions + a sample change request"
```

---

### Task 8: Regenerate bindings + frontend types

**Files:**
- Regenerate: `frontend/wailsjs/go/main/App.{js,d.ts}`, `frontend/wailsjs/go/models.ts`
- Modify: `frontend/src/api.ts`

**Interfaces:**
- Produces: TS bindings for the 15 new methods; `api.ts` interfaces `Version`, `ChangeRequest`, `CRDecision`, `CRImpactResult`, `VersionShare`, `CRShare`.

- [ ] **Step 1: Regenerate bindings**

Run: `wails generate module`
Expected: completes; `grep -E "CreateVersion|ListChangeRequests|GetVersionDistribution" frontend/wailsjs/go/main/App.d.ts` shows the new methods.

- [ ] **Step 2: Re-export methods + add interfaces in `api.ts`**

Add the 15 method names to the `export { … } from "../wailsjs/go/main/App"` block. Update the existing coverage method *signatures used in the app* — they now take `versionId` as the second arg (the binding handles types; only call sites in `CoverageView.tsx` change, in Task 9).

Append interfaces (mirror the Go JSON tags):

```ts
export interface Version { id: string; name: string; status: string; notes: string; sortOrder: number; createdAt: string; }
export interface ChangeRequest { id: string; crKey: string; title: string; status: string; targetVersionId: string; risk: string; description: string; createdAt: string; updatedAt: string; }
export interface CRDecision { requirementKey: string; projectKey: string; decision: string; note: string; }
export interface CRImpactResult { cr: ChangeRequest; decisions: CRDecision[]; canAccept: number; cannotAccept: number; pending: number; }
export interface VersionShare { versionId: string; versionName: string; status: string; memberCount: number; }
export interface CRShare { crId: string; title: string; status: string; canAccept: number; cannotAccept: number; pending: number; }
```

- [ ] **Step 3: Typecheck**

Run: `cd frontend && npx tsc --noEmit`
Expected: this will FAIL in `CoverageView.tsx` because the re-scoped methods now need `versionId`. That is fixed in Task 9 — do not fix here. Confirm the only errors are about the changed coverage-call arity in `CoverageView.tsx`.

- [ ] **Step 4: Commit**

```bash
git add frontend/wailsjs frontend/src/api.ts
git commit -m "chore(frontend): regenerate bindings + Topic 2 types"
```

---

### Task 9: Frontend — version selector + Versions & CRs tab

**Files:**
- Create: `frontend/src/components/VersionBar.tsx`, `ChangeRequestsPanel.tsx`, `VersionDashboard.tsx`
- Modify: `frontend/src/components/CoverageView.tsx`, `frontend/src/App.css`

**Interfaces:**
- Consumes: all 15 bound methods + version-scoped coverage methods.
- Produces: a selected-version state in `CoverageView`; the matrix/gaps/import/export use it.

- [ ] **Step 1: Add version state + selector to `CoverageView`**

In `CoverageView.tsx`: add `const [versions, setVersions] = useState<Version[]>([])` and `const [versionId, setVersionId] = useState("")`. When `selected` (canonical) changes, load `ListVersions(profileId, selected)`, set `versions`, and default `versionId` to the first stable (else first). Gate model/coverage loads on `versionId` and pass it: `GetParamModel(profileId, versionId)`, `GetCoverageReport(profileId, versionId)`, `ListCoverageGaps(profileId, versionId)`, `ImportCoverageTemplate(profileId, versionId)`, `ExportCoverageReport(profileId, versionId)`. Render `<VersionBar versions={versions} value={versionId} onChange={setVersionId} onChanged={reload} profileId={profileId} canonicalId={selected} />` above the tabs.

- [ ] **Step 2: Write `VersionBar.tsx`**

Create `frontend/src/components/VersionBar.tsx` — a dropdown of versions with status badge, plus buttons: New (create), Clone (CloneVersion of the current), Status (cycle/select status), Delete. Each calls the matching bound method then `onChanged()`. (Concrete contract: props `{ versions: Version[]; value: string; onChange: (id: string) => void; profileId: string; canonicalId: string; onChanged: () => void }`.) Use inline controlled inputs (no `window.prompt`, per WebView2 constraint).

- [ ] **Step 3: Write `ChangeRequestsPanel.tsx`**

Create `frontend/src/components/ChangeRequestsPanel.tsx` — props `{ profileId: string; canonicalId: string; versions: Version[]; onChanged: () => void }`. Lists `ListChangeRequests`; a create/edit form (title, status select, target-version select, risk select, description); when a CR is selected, calls `GetCRImpact` and renders the per-member decision grid (each row: requirement key + project + a 3-way select wired to `SetCRDecision`), plus the can/cannot/pending tallies.

- [ ] **Step 4: Write `VersionDashboard.tsx`**

Create `frontend/src/components/VersionDashboard.tsx` — props `{ profileId: string; canonicalId: string }`. Calls `GetVersionDistribution` (renders a labelled bar per version: name + status + member count) and `GetCRAdoption` (a row per CR with can/cannot/pending counts).

- [ ] **Step 5: Add a "Versions & CRs" tab to `CoverageView`**

Extend the existing tab state from `"matrix" | "gaps" | "reuse"` to also include `"versions"`. Add a tab button "Versions & CRs". When active, render `<ChangeRequestsPanel … />` and `<VersionDashboard … />` (and member version-lock assignment, which `VersionBar`/a small members list drives via `SetMemberVersion`).

- [ ] **Step 6: Add CSS**

Append `.cov-version-bar`, `.cov-badge-{planning,beta,stable,deprecated}`, `.cov-cr-grid`, `.cov-dist-bar` styles to `App.css`, following the existing `.cov-*` palette (`var(--surface-sunken)`, status colors as in `.cov-pass/.cov-fail/.cov-notrun`).

- [ ] **Step 7: Typecheck**

Run: `cd frontend && npx tsc --noEmit`
Expected: PASS (exit 0).

- [ ] **Step 8: Production build**

Run: `cd frontend && npm run build`
Expected: build succeeds.

- [ ] **Step 9: Commit**

```bash
git add frontend/src
git commit -m "feat(frontend): version selector + Versions & CRs tab + dashboards"
```

---

### Task 10: Full verification + demo click-through + CHANGELOG

**Files:**
- Modify: `CHANGELOG.md`
- Verify: full build + tests + demo

**Steps:**

- [ ] **Step 1: Full Go build + test**

Run: `go build ./... && go test ./internal/...`
Expected: all PASS (coverage, store, testrepo, syncer, jira, profile).

- [ ] **Step 2: Confirm no import cycle / boundedness**

Run: `go list -deps ./internal/testrepo/ | grep -c "internal/coverage"`
Expected: `0` (testrepo must not depend on coverage).

- [ ] **Step 3: Demo click-through**

Run: `wails dev`. In the app: create a `demo` profile → Sync + Sync Requirements → Coverage → Load demo example (Login). Verify: the version selector shows **1.0** and **2.0**; the matrix renders for the selected version; the **Versions & CRs** tab shows the "Add OAuth login" CR with a per-member decision grid and the version-distribution + CR-adoption dashboards populated. Clone a version and confirm a new entry appears with the same matrix.

- [ ] **Step 4: Add CHANGELOG entry**

Under `## [Unreleased]` → `### Added` in `CHANGELOG.md`:

```markdown
- **Coverage module — versions & change requests (Topic 2).** Each functional requirement can now hold multiple **versions** (e.g. 2.40/stable, 3.0/beta) — coverage is measured per version, and a version can be **cloned** to start a new release line from an existing one. Customer requirements are **locked to a version**, and **change requests** track each customer's can-accept / cannot-accept / pending decision, with **version-distribution and CR-adoption dashboards**. All local (schema v36); no Jira admin needed.
```

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs: changelog for Topic 2 versions + change requests"
```

---

## Self-Review

**Spec coverage:** Versions (Tasks 1–2), per-version coverage re-rooting (Task 3), member locks (Task 2), change requests + decisions + impact (Task 4), dashboards (Task 5), app methods (Task 6), demo alignment (Task 7), frontend version selector + Versions & CRs + dashboards (Tasks 8–9), migration v35→v36 (Task 1), testing throughout, CHANGELOG (Task 10). All spec sections map to a task. ✅

**Placeholder scan:** No TBD/TODO; every code step shows code. Frontend Steps 2–5 of Task 9 give explicit prop contracts and method wiring rather than full JSX — acceptable because the component pattern (load → render → call bound method → onChanged) is shown concretely and mirrors the existing `CoverageView`/`MapTestsModal` already in the tree; the executor follows that established pattern. ✅

**Type consistency:** `versionId` second-arg threads consistently from `api.ts` → `CoverageView` → bound methods → module. `NodeEdit.VersionID` added in Task 2, used by `buildModel` (Task 3) and demo (Task 7). `CRImpactResult`/`CRShare`/`VersionShare` field names match between Go JSON tags (Tasks 4–5) and TS interfaces (Task 8). `CloneVersion(profileID, sourceVersionID, name, status)` signature identical in module (Task 2), app (Task 6), demo (Task 7). ✅
