# Task Activity Manager foundation 0a: monorepo and shared Go core

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the repo into the `agile-suite` monorepo with XTM moved in intact, extract the first shared Go core (a generic SQLite runner plus the profile, connection, settings, and credential packages), and cut XTM over to a shared `profiles.db` so a Jira connection set up once is visible to both apps.

**Architecture:** A Go workspace holds `core/` and `xtm/` (TAM arrives in plan 0b). `core/store` is XTM's open-then-migrate runner generalised to take the schema as a value. `core/profile`, `core/connection`, and `core/settings` are XTM's packages lifted as they are, taking a `*sql.DB` instead of XTM's store so they can run against either database. `core/shareddb` defines the shared database (profiles, connection, app_setting) and where it lives. XTM's startup opens both databases, copies its existing profile rows into the shared one once, and injects a profile lookup into the test repository for the three bug-settings reads that used to query the profiles table directly.

**Tech Stack:** Go 1.25 with `go.work`, `modernc.org/sqlite`, `github.com/danieljoos/wincred` and `github.com/zalando/go-keyring` for credentials, Wails v2.15.0, Vite/Vitest (moved unchanged).

## Global Constraints

- The monorepo is named `agile-suite`; Go modules are `agile-suite/core` and `agile-suite/xtm`.
- XTM is never edited for TAM's sake beyond import paths, except the cutover edits this plan names explicitly (Task 4: `initStore` wiring, the `bugs.go` lookup, `shutdown`).
- Every task leaves XTM's full Go suite (`go test ./internal/...` inside `xtm/`) and frontend suite (`npx vitest run` inside `xtm/frontend/`) green.
- `core` holds only what a task in this plan needs. No speculative packages.
- The Windows Credential Manager prefix `xray-test-manager:` and the keyring service name stay exactly as they are, so stored PATs keep working after the move.
- Each app keeps its own SQLite file. Only `profiles.db` is shared, at `<user config dir>/agile-suite/profiles.db`.
- Frontends live inside their module (`xtm/frontend/`), because Wails embeds `frontend/dist` relative to `main.go` and `go:embed` cannot reach a parent directory.
- No AI attribution or mentions in any commit message or PR. Run the humanizer pass over prose, including code comments.
- Commit messages use the repo's conventional prefixes (`chore:`, `feat:`, `refactor:`, `docs:`) with no trailers.

---

## File structure

**Created**

- `go.work` (repo root) - the workspace listing `./core` and `./xtm`.
- `core/go.mod` - module `agile-suite/core`.
- `core/store/store.go`, `core/store/store_test.go` - the generic open-then-migrate runner.
- `core/shareddb/shareddb.go`, `core/shareddb/shareddb_test.go` - the shared database schema, opener, and default path.
- `core/profile/`, `core/connection/`, `core/settings/` - lifted from `xtm/internal/` (Task 3), including their tests and the platform credential files.
- `xtm/internal/sharedmigrate/sharedmigrate.go`, `sharedmigrate_test.go` - the one-time copy of XTM's profile rows into the shared database.
- `xtm/internal/testrepo/bugs_settings_test.go` - covers the injected bug-settings lookup.
- `CLAUDE.md`, `README.md` (repo root) - short suite-level guides pointing into `xtm/`.

**Moved** (Task 1, with `git mv` so history follows)

- Every tracked root file except `LICENSE` and `.gitignore` moves into `xtm/`: `main.go`, `app.go`, `app_*.go`, `bridge_publish.go`, `go.mod`, `go.sum`, `wails.json`, `README.md`, `CHANGELOG.md`, `CLAUDE.md`.
- `internal/`, `frontend/`, `build/`, `scripts/` move to `xtm/internal/`, `xtm/frontend/`, `xtm/build/`, `xtm/scripts/`.
- `docs/` and `.github/` stay at the root.

**Modified**

- `.gitignore`, `.github/workflows/build.yml`, `.github/workflows/release.yml` - paths.
- `xtm/go.mod` and every `xtm/**/*.go` import - module rename.
- `xtm/app.go` - `initStore`/`shutdown` wiring (Tasks 3 and 4).
- `xtm/internal/testrepo/bugs.go` - the lookup seam (Task 4).

---

### Task 1: Restructure into the monorepo

Move XTM into `xtm/` under a Go workspace and rename its module. Nothing about XTM's behaviour changes; the proof is its own suites passing from the new location.

**Files:**
- Create: `go.work`, `CLAUDE.md`, `README.md`
- Move: everything listed under "Moved" above
- Modify: `.gitignore`, `.github/workflows/build.yml`, `.github/workflows/release.yml`, `xtm/go.mod`, all `xtm/**/*.go`

**Interfaces:**
- Produces: module path `agile-suite/xtm` for every XTM import; `xtm/` as the Wails project root (`wails build` runs there).

- [ ] **Step 1: Create the branch and move the files**

```bash
git checkout -b chore/monorepo-restructure
mkdir xtm
git mv main.go app.go app_backend_test.go app_connection_test.go app_coverage.go \
  app_coverage_publish.go app_coverage_publish_test.go app_panic_test.go \
  app_spellcheck.go app_spellcheck_test.go bridge_publish.go go.mod go.sum wails.json xtm/
git mv internal frontend build scripts xtm/
git mv README.md CHANGELOG.md CLAUDE.md xtm/
git status --short | head -5
```

Expected: every line is `R  <old> -> xtm/<old>` (renames), no `D`/`A` pairs.

- [ ] **Step 2: Rename the module and rewrite imports**

```bash
sed -i 's#^module xray-test-manager$#module agile-suite/xtm#' xtm/go.mod
grep -rl '"xray-test-manager/' xtm --include='*.go' | xargs sed -i 's#"xray-test-manager/#"agile-suite/xtm/#g'
grep -rn '"xray-test-manager/' xtm --include='*.go' | wc -l
```

Expected: the final count is `0`.

- [ ] **Step 3: Add the workspace file**

Create `go.work` at the repo root:

```
go 1.25.0

use ./xtm
```

- [ ] **Step 4: Fix ignored paths**

In `.gitignore`, change these two lines (the `dist/` pattern has no leading slash, so it already matches `xtm/dist/`):

```
build/bin
frontend/dist
```

to

```
xtm/build/bin
xtm/frontend/dist
```

- [ ] **Step 5: Point CI at `xtm/`**

In `.github/workflows/build.yml`, add `working-directory: xtm` to the three run steps that need it. The `test` job's step becomes:

```yaml
      - name: Go tests
        # Scope to internal/ — the main package embeds frontend/dist, which
        # isn't built in this job; all tests live under internal/ anyway.
        working-directory: xtm
        run: go test ./internal/...
```

and both build steps become:

```yaml
      - name: Build (.exe)
        working-directory: xtm
        run: wails build
```

```yaml
      - name: Build universal .app
        working-directory: xtm
        run: wails build -platform darwin/universal
```

In `.github/workflows/release.yml`:

- line 62: `run: ./scripts/release.ps1 -Version ...` becomes `run: ./xtm/scripts/release.ps1 -Version ...`
- lines 71-73 and 80: every `dist/` becomes `xtm/dist/`
- line 113: add `working-directory: xtm` above `run: wails build -platform darwin/universal -clean`
- lines 156, 164, 174: `working-directory: build/bin` becomes `working-directory: xtm/build/bin`
- lines 190-191 and 198: `build/bin/` becomes `xtm/build/bin/`

`xtm/scripts/release.ps1` needs no change: it resolves `$root = Split-Path -Parent $PSScriptRoot`, which is now `xtm/`.

- [ ] **Step 6: Write the root guides**

Create `CLAUDE.md` at the root:

```markdown
# CLAUDE.md

This repository is the **agile-suite** monorepo: two desktop apps for Jira DC
that share a Go core.

- `xtm/`: Xray Test Manager. Read `xtm/CLAUDE.md` for everything about it;
  run Wails, Go tests, and the frontend from inside `xtm/`.
- `core/`: the shared Go spine (store runner, profiles, connections,
  settings, credentials). Added by packages only when an app needs them.
- `tam/`: Task Activity Manager (arrives with plan 0b).
- `docs/superpowers/`: design specs and implementation plans for the suite.

`go.work` at the root ties the modules together, so `go build ./...` and
`go test ./...` work from any module directory.
```

Create `README.md` at the root:

```markdown
# agile-suite

Desktop tools for Jira Data Center that share one code spine:

- **Xray Test Manager** (`xtm/`): manage Xray test cases at scale. See
  `xtm/README.md`.
- **Task Activity Manager** (`tam/`): agile task management (in development).

Both are Go + Wails + React apps that sync Jira into a local SQLite cache and
push edits back on commit.
```

- [ ] **Step 7: Verify XTM builds and tests from its new home**

```bash
cd xtm && go build ./... && go test ./internal/... 2>&1 | tail -3
cd frontend && npm ci --silent && npm run build 2>&1 | tail -2 && npx vitest run 2>&1 | grep -E "Tests "
```

Expected: `ok` for every package, `✓ built in`, and `Tests  <N> passed (<N>)` with the same count as before the move.

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "chore: move Xray Test Manager into xtm/ under a Go workspace

The repo becomes the agile-suite monorepo. XTM moves into xtm/ with git
renames so history follows, its module becomes agile-suite/xtm, and CI plus
the ignore file point at the new paths. Root CLAUDE.md and README describe
the layout; xtm/ keeps its own. No code change beyond import paths."
```

---

### Task 2: `core/store`, the generic open-then-migrate runner

XTM's `store.Open` does four things in order: create tables, run the migrations an older file is missing, create indexes, record the version. `core/store` keeps that exact sequence but takes the schema as a value, so the shared database and, later, TAM's own store use it without copying the runner.

**Files:**
- Create: `core/go.mod`, `core/store/store.go`, `core/store/store_test.go`
- Modify: `go.work`

**Interfaces:**
- Produces:
  - `type Migration struct { Version int; Apply func(db *sql.DB) error }`
  - `type Schema struct { Version int; Base string; Migrations []Migration; Indexes string }`
  - `func Open(path string, s Schema) (*DB, error)`
  - `func (d *DB) DB() *sql.DB`, `func (d *DB) Close() error`
  - `func ReadSchemaVersion(db *sql.DB) (int, error)`
  - `func AddColumnIfMissing(db *sql.DB, table, columnDDL string) error`

- [ ] **Step 1: Create the module and register it**

```bash
mkdir -p core/store && cd core && go mod init agile-suite/core && cd ..
```

Then edit `go.work` to:

```
go 1.25.0

use (
	./core
	./xtm
)
```

- [ ] **Step 2: Write the failing tests**

Create `core/store/store_test.go`:

```go
package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

const widgetTable = `CREATE TABLE IF NOT EXISTS widget (id TEXT PRIMARY KEY);`

func openTemp(t *testing.T, s Schema) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "t.db"), s)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestOpenFreshRecordsVersionAndCreatesTables(t *testing.T) {
	d := openTemp(t, Schema{Version: 3, Base: widgetTable})
	v, err := ReadSchemaVersion(d.DB())
	if err != nil || v != 3 {
		t.Fatalf("version = %d, %v; want 3", v, err)
	}
	if _, err := d.DB().Exec(`INSERT INTO widget (id) VALUES ('a')`); err != nil {
		t.Fatalf("widget table missing: %v", err)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	s := Schema{Version: 1, Base: widgetTable}
	for i := 0; i < 2; i++ {
		d, err := Open(path, s)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		_ = d.Close()
	}
}

func TestMigrationsRunOnlyWhenBehind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	d, err := Open(path, Schema{Version: 1, Base: widgetTable})
	if err != nil {
		t.Fatal(err)
	}
	_ = d.Close()

	calls := 0
	v2 := Schema{Version: 2, Base: widgetTable, Migrations: []Migration{{
		Version: 2,
		Apply: func(db *sql.DB) error {
			calls++
			return AddColumnIfMissing(db, "widget", "colour TEXT NOT NULL DEFAULT ''")
		},
	}}}
	d, err = Open(path, v2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.DB().Exec(`INSERT INTO widget (id, colour) VALUES ('a', 'red')`); err != nil {
		t.Fatalf("colour column missing after migration: %v", err)
	}
	_ = d.Close()
	if calls != 1 {
		t.Fatalf("migration ran %d times on upgrade; want 1", calls)
	}

	d, err = Open(path, v2)
	if err != nil {
		t.Fatal(err)
	}
	_ = d.Close()
	if calls != 1 {
		t.Fatalf("migration re-ran on an up-to-date database (%d calls)", calls)
	}
}

func TestIndexesApplyAfterMigrations(t *testing.T) {
	// The index references a column only the migration adds, which is the
	// whole reason indexes run last.
	path := filepath.Join(t.TempDir(), "t.db")
	d, err := Open(path, Schema{Version: 1, Base: widgetTable})
	if err != nil {
		t.Fatal(err)
	}
	_ = d.Close()
	s := Schema{
		Version: 2,
		Base:    widgetTable,
		Migrations: []Migration{{Version: 2, Apply: func(db *sql.DB) error {
			return AddColumnIfMissing(db, "widget", "colour TEXT NOT NULL DEFAULT ''")
		}}},
		Indexes: `CREATE INDEX IF NOT EXISTS idx_widget_colour ON widget (colour);`,
	}
	d, err = Open(path, s)
	if err != nil {
		t.Fatalf("open with index: %v", err)
	}
	_ = d.Close()
}

func TestAddColumnIfMissingIsIdempotent(t *testing.T) {
	d := openTemp(t, Schema{Version: 1, Base: widgetTable})
	for i := 0; i < 2; i++ {
		if err := AddColumnIfMissing(d.DB(), "widget", "colour TEXT NOT NULL DEFAULT ''"); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
}
```

- [ ] **Step 3: Run the tests to confirm they fail**

```bash
cd core && go test ./store/ 2>&1 | head -3
```

Expected: build failure, `undefined: Schema` (and friends).

- [ ] **Step 4: Write the runner**

Create `core/store/store.go`:

```go
// Package store is the SQLite layer both apps build on. It opens a database
// with the pragmas the desktop apps rely on and walks a caller-supplied schema
// through the same three stages XTM's store has always used: base tables,
// then the migrations an older file is missing, then indexes, recording the
// resulting version in a meta table. The schema belongs to the caller; this
// package only knows how to apply one.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

// Migration upgrades a database whose recorded version is below Version.
type Migration struct {
	Version int
	Apply   func(db *sql.DB) error
}

// Schema describes one database: the tables a fresh install gets, the ordered
// migrations that bring an older file up to date, and the indexes applied
// last so they can reference columns a migration adds.
type Schema struct {
	Version    int
	Base       string
	Migrations []Migration
	Indexes    string
}

// DB wraps an open database.
type DB struct {
	db *sql.DB
}

const metaTable = `CREATE TABLE IF NOT EXISTS meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);`

// Open opens (or creates) the SQLite file at path and applies s. Calling it on
// a database that is already current is a no-op apart from re-recording the
// version.
//
// WAL lets readers proceed alongside a writer, so the UI's parallel queries
// don't queue behind a sync's write transaction; busy_timeout makes a briefly
// locked file wait instead of failing with "database is locked" when a
// previous instance is still letting go of it.
func Open(path string, s Schema) (*DB, error) {
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := apply(db, s); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &DB{db: db}, nil
}

func apply(db *sql.DB, s Schema) error {
	if _, err := db.Exec(metaTable); err != nil {
		return fmt.Errorf("create meta: %w", err)
	}
	if s.Base != "" {
		if _, err := db.Exec(s.Base); err != nil {
			return fmt.Errorf("apply tables: %w", err)
		}
	}
	current, err := ReadSchemaVersion(db)
	if err != nil {
		return err
	}
	for _, m := range s.Migrations {
		if current >= m.Version {
			continue
		}
		if err := m.Apply(db); err != nil {
			return fmt.Errorf("migration v%d: %w", m.Version, err)
		}
	}
	if s.Indexes != "" {
		if _, err := db.Exec(s.Indexes); err != nil {
			return fmt.Errorf("apply indexes: %w", err)
		}
	}
	if _, err := db.Exec(
		"INSERT INTO meta (key, value) VALUES ('schema_version', ?) "+
			"ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		s.Version,
	); err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}
	return nil
}

// DB exposes the underlying handle for the managers built on top.
func (d *DB) DB() *sql.DB { return d.db }

// Close closes the database.
func (d *DB) Close() error { return d.db.Close() }

// ReadSchemaVersion returns the version recorded in meta, or 0 for a fresh
// file.
func ReadSchemaVersion(db *sql.DB) (int, error) {
	var raw string
	err := db.QueryRow("SELECT value FROM meta WHERE key = 'schema_version'").Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	v, _ := strconv.Atoi(raw)
	return v, nil
}

// AddColumnIfMissing runs ALTER TABLE ... ADD COLUMN and treats "duplicate
// column" as success, so a migration stays idempotent when a fresh install
// already has the column from the base schema.
func AddColumnIfMissing(db *sql.DB, table, columnDDL string) error {
	_, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", table, columnDDL))
	if err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return err
	}
	return nil
}
```

- [ ] **Step 5: Add the dependency and run the tests**

```bash
cd core && go get modernc.org/sqlite@v1.53.0 && go mod tidy && go test ./store/ -v 2>&1 | grep -E "^(=== RUN|--- |ok|FAIL)"
```

Expected: five `--- PASS` lines and `ok  	agile-suite/core/store`.

- [ ] **Step 6: Commit**

```bash
git add go.work core/
git commit -m "feat(core): shared SQLite open-then-migrate runner

core/store lifts XTM's store.Open sequence (base tables, ordered
migrations, indexes, recorded version) into a runner that takes the schema
as a value, so the shared profile database and later stores reuse it
without copying the mechanics. Covered by unit tests for fresh open,
idempotent reopen, migrations that run only when behind, index ordering,
and the tolerant add-column helper."
```

---

### Task 3: Lift profile, connection, and settings into `core`, and define the shared database

The three packages move as they are; the only code change is that their constructors take a `*sql.DB` instead of XTM's `*store.Store`. `core/shareddb` fixes the shared database's schema (the same three tables XTM already has, columns and defaults identical, so rows can be copied straight across) and its location. XTM keeps pointing the managers at its own `store.db` in this task, so behaviour is unchanged; the cutover is Task 4.

**Files:**
- Move: `xtm/internal/profile/` -> `core/profile/`, `xtm/internal/connection/` -> `core/connection/`, `xtm/internal/settings/` -> `core/settings/` (all files, including tests and `credentials_*.go`)
- Create: `core/shareddb/shareddb.go`, `core/shareddb/shareddb_test.go`
- Modify: `core/profile/profile.go`, `core/connection/connection.go`, `core/settings/settings.go`, the four test files in those packages, `xtm/app.go`, `xtm/go.mod`, `core/go.mod`

**Interfaces:**
- Consumes: `store.Open`, `store.Schema`, `store.DB` from Task 2.
- Produces:
  - `profile.NewManager(db *sql.DB) *Manager`, `connection.NewManager(db *sql.DB) *Manager`, `settings.NewManager(db *sql.DB) *Manager` (every other method unchanged)
  - `profile.NewCredentialStore() CredentialStore` (unchanged)
  - `shareddb.Schema store.Schema`, `shareddb.Open(path string) (*store.DB, error)`, `shareddb.DefaultPath() (string, error)`

- [ ] **Step 1: Move the packages**

```bash
git mv xtm/internal/profile core/profile
git mv xtm/internal/connection core/connection
git mv xtm/internal/settings core/settings
```

- [ ] **Step 2: Write the shared database package and its failing test**

Create `core/shareddb/shareddb_test.go`:

```go
package shareddb_test

import (
	"path/filepath"
	"testing"

	"agile-suite/core/shareddb"
)

func TestOpenCreatesTheSharedTables(t *testing.T) {
	d, err := shareddb.Open(filepath.Join(t.TempDir(), "profiles.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()
	for _, stmt := range []string{
		`INSERT INTO profiles (id, name, jira_url, project_key, created_at) VALUES ('p1', 'One', 'https://j', 'ONE', '2026-01-01T00:00:00Z')`,
		`INSERT INTO connection (id, workspace_id, name) VALUES ('c1', 'p1', 'One')`,
		`INSERT INTO app_setting (key, value) VALUES ('theme', 'dark')`,
	} {
		if _, err := d.DB().Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
}

func TestDefaultPathIsUnderTheSuiteDir(t *testing.T) {
	p, err := shareddb.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "profiles.db" || filepath.Base(filepath.Dir(p)) != "agile-suite" {
		t.Fatalf("unexpected path %q", p)
	}
}
```

Create `core/shareddb/shareddb.go`:

```go
// Package shareddb defines the one database both apps open: the workspaces
// (profiles), their connections, and the global key/value settings. A Jira
// connection set up in either app is visible in the other because they read
// the same file.
//
// The tables keep the XTM-era columns (scope_jql, the bug_* fields) with the
// same defaults XTM uses, so an existing XTM database can be copied in row
// for row. Splitting those into an XTM-only extension is a later change.
package shareddb

import (
	"os"
	"path/filepath"

	"agile-suite/core/store"
)

// Schema is the shared database layout.
var Schema = store.Schema{
	Version: 1,
	Base: `
CREATE TABLE IF NOT EXISTS profiles (
	id          TEXT PRIMARY KEY,
	name        TEXT NOT NULL,
	jira_url    TEXT NOT NULL,
	project_key TEXT NOT NULL,
	created_at  TEXT NOT NULL,
	scope_jql   TEXT NOT NULL DEFAULT '',
	bug_issue_type TEXT NOT NULL DEFAULT 'Bug',
	bug_project_mode TEXT NOT NULL DEFAULT 'test',
	bug_project_key TEXT NOT NULL DEFAULT '',
	ca_cert TEXT NOT NULL DEFAULT '',
	allow_untrusted_tls INTEGER NOT NULL DEFAULT 0,
	backend TEXT NOT NULL DEFAULT 'xray',
	cross_project_sources TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS connection (
	id                  TEXT PRIMARY KEY,
	workspace_id        TEXT NOT NULL,
	name                TEXT NOT NULL,
	backend             TEXT NOT NULL DEFAULT 'xray',
	url                 TEXT NOT NULL DEFAULT '',
	project_key         TEXT NOT NULL DEFAULT '',
	scope_jql           TEXT NOT NULL DEFAULT '',
	bug_issue_type      TEXT NOT NULL DEFAULT 'Bug',
	bug_project_mode    TEXT NOT NULL DEFAULT 'test',
	bug_project_key     TEXT NOT NULL DEFAULT '',
	ca_cert             TEXT NOT NULL DEFAULT '',
	allow_untrusted_tls INTEGER NOT NULL DEFAULT 0,
	role                TEXT NOT NULL DEFAULT 'both',
	created_at          TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS app_setting (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL DEFAULT ''
);
`,
}

// Open opens (or creates) the shared database at path.
func Open(path string) (*store.DB, error) { return store.Open(path, Schema) }

// DefaultPath is where both apps look for the shared database:
// <user config dir>/agile-suite/profiles.db. The directory is created if it
// does not exist.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	suiteDir := filepath.Join(dir, "agile-suite")
	if err := os.MkdirAll(suiteDir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(suiteDir, "profiles.db"), nil
}
```

- [ ] **Step 3: Rewrite the three constructors and their imports**

In `core/profile/profile.go`:

- replace the import line `"xray-test-manager/internal/connection"` (now `"agile-suite/xtm/internal/connection"` after Task 1) with `"agile-suite/core/connection"`
- delete the import line for `.../internal/store`
- replace

```go
// NewManager returns a profile manager backed by the given store.
func NewManager(s *store.Store) *Manager {
	return &Manager{db: s.DB(), conns: connection.NewManager(s)}
}
```

with

```go
// NewManager returns a profile manager over the given database, which must
// hold the shareddb tables.
func NewManager(db *sql.DB) *Manager {
	return &Manager{db: db, conns: connection.NewManager(db)}
}
```

In `core/connection/connection.go`:

- delete the import line for `.../internal/store`
- replace

```go
func NewManager(s *store.Store) *Manager {
	return &Manager{db: s.DB()}
}
```

with

```go
// NewManager returns a connection manager over the given database.
func NewManager(db *sql.DB) *Manager {
	return &Manager{db: db}
}
```

In `core/settings/settings.go`:

- delete the import line for `.../internal/store`
- replace

```go
// NewManager returns a settings manager backed by the given store.
func NewManager(s *store.Store) *Manager {
	return &Manager{db: s.DB()}
}
```

with

```go
// NewManager returns a settings manager over the given database.
func NewManager(db *sql.DB) *Manager {
	return &Manager{db: db}
}
```

Then confirm nothing in the three packages still mentions XTM's store:

```bash
grep -rn "internal/store\|\*store\.Store" core/profile core/connection core/settings
```

Expected: only the test files (fixed in the next step).

- [ ] **Step 4: Point the moved tests at the shared database**

All four test files build their store the same way. Apply the same three edits to `core/profile/profile_test.go`, `core/profile/profile_connection_sync_test.go`, `core/connection/connection_test.go`, and `core/settings/settings_test.go`:

```bash
for f in core/profile/profile_test.go core/profile/profile_connection_sync_test.go core/connection/connection_test.go core/settings/settings_test.go; do
  sed -i 's#"agile-suite/xtm/internal/store"#"agile-suite/core/shareddb"#; s#"agile-suite/xtm/internal/\(profile\|connection\|settings\)"#"agile-suite/core/\1"#g' "$f"
  sed -i 's#store\.Open(#shareddb.Open(#g; s#NewManager(st)#NewManager(st.DB())#g' "$f"
done
grep -n "shareddb.Open\|NewManager(st" core/profile/*_test.go core/connection/*_test.go core/settings/*_test.go | head
```

Expected: every `store.Open(` became `shareddb.Open(` and every `NewManager(st)` became `NewManager(st.DB())`. `settings_test.go` line 30 returns `settings.NewManager(st.DB()), st`; its callers use `st.DB()`, which `*store.DB` also provides, so they compile unchanged.

- [ ] **Step 5: Add the credential dependencies to `core` and run its tests**

```bash
cd core && go get github.com/google/uuid@v1.6.0 github.com/danieljoos/wincred@v1.2.3 github.com/zalando/go-keyring@v0.2.8 && go mod tidy
go test ./... 2>&1 | grep -E "^(ok|FAIL|---)"
```

Expected: `ok` for `agile-suite/core/store`, `agile-suite/core/shareddb`, `agile-suite/core/profile`, `agile-suite/core/connection`, `agile-suite/core/settings`, and no `FAIL`.

- [ ] **Step 6: Repoint XTM at the core packages, still on its own database**

In `xtm/app.go`, change the three imports:

```go
	"agile-suite/xtm/internal/connection"
	"agile-suite/xtm/internal/profile"
	"agile-suite/xtm/internal/settings"
```

to

```go
	"agile-suite/core/connection"
	"agile-suite/core/profile"
	"agile-suite/core/settings"
```

and the three constructor calls in `initStore`:

```go
	a.profiles = profile.NewManager(st)
	a.connections = connection.NewManager(st)
	a.creds = profile.NewCredentialStore()
	a.settings = settings.NewManager(st)
```

to

```go
	a.profiles = profile.NewManager(st.DB())
	a.connections = connection.NewManager(st.DB())
	a.creds = profile.NewCredentialStore()
	a.settings = settings.NewManager(st.DB())
```

Any other XTM file that imports one of the three packages gets the same import rewrite:

```bash
grep -rl '"agile-suite/xtm/internal/\(profile\|connection\|settings\)"' xtm --include='*.go' | xargs sed -i 's#"agile-suite/xtm/internal/\(profile\|connection\|settings\)"#"agile-suite/core/\1"#g'
cd xtm && go mod edit -require=agile-suite/core@v0.0.0 && go mod tidy
```

`go.work` makes the `core` module resolve locally; the `require` line just records the dependency.

- [ ] **Step 7: Verify XTM is unchanged in behaviour**

```bash
cd xtm && go build ./... && go test ./internal/... 2>&1 | grep -E "^(ok|FAIL)" | grep -c ok
go vet ./... 2>&1 | tail -2
```

Expected: the same number of `ok` packages as before minus three (profile, connection, settings now live in `core`), and no `FAIL`. The frontend is untouched by this task.

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "refactor(core): lift profile, connection, and settings into the shared core

The three packages move from xtm/internal to core with their tests and the
platform credential stores. Their constructors take a *sql.DB instead of
XTM's store so they run against any database that has the tables; XTM
keeps passing its own store for now, so nothing changes at runtime.

core/shareddb fixes the shared profile database: the same profiles,
connection, and app_setting tables XTM has, columns and defaults identical
so rows can be copied straight across, plus the default location
<config>/agile-suite/profiles.db. Credential Manager entries keep the
xray-test-manager: prefix so existing tokens keep working."
```

---

### Task 4: Cut XTM over to the shared `profiles.db`

XTM starts opening the shared database alongside its own, copies its existing profiles, connections, and settings across exactly once, and reads them from the shared file from then on. The three places in `testrepo` that read profile columns with raw SQL get a lookup injected by the app instead, because those rows no longer live in `store.db`.

**Files:**
- Create: `xtm/internal/sharedmigrate/sharedmigrate.go`, `xtm/internal/sharedmigrate/sharedmigrate_test.go`, `xtm/internal/testrepo/bugs_settings_test.go`
- Modify: `xtm/internal/testrepo/bugs.go`, `xtm/internal/testrepo/testrepo.go:325-332`, `xtm/app.go` (`App` struct, `initStore`, `shutdown`)

**Interfaces:**
- Consumes: `shareddb.Open`, `shareddb.DefaultPath`, `profile.NewManager(db)`, `connection.NewManager(db)`, `settings.NewManager(db)`.
- Produces:
  - `sharedmigrate.ImportFromStore(src, dst *sql.DB) error`
  - `testrepo.BugSettings{ IssueType, ProjectMode, ProjectKey string }`
  - `(*testrepo.Repository).SetBugSettingsLookup(fn func(profileID string) (BugSettings, error))`

- [ ] **Step 1: Write the failing migration test**

Create `xtm/internal/sharedmigrate/sharedmigrate_test.go`:

```go
package sharedmigrate_test

import (
	"path/filepath"
	"testing"

	"agile-suite/core/shareddb"
	"agile-suite/xtm/internal/sharedmigrate"
	"agile-suite/xtm/internal/store"
)

func openBoth(t *testing.T) (*store.Store, *shareddbDB) {
	t.Helper()
	src, err := store.Open(filepath.Join(t.TempDir(), "xtm.db"))
	if err != nil {
		t.Fatal(err)
	}
	dst, err := shareddb.Open(filepath.Join(t.TempDir(), "profiles.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close(); _ = dst.Close() })
	return src, dst
}

func count(t *testing.T, db interface {
	QueryRow(string, ...any) interface{ Scan(...any) error }
}, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestImportCopiesRowsOnceAndKeepsExistingTargets(t *testing.T) {
	src, dst := openBoth(t)
	mustExec(t, src.DB(), `INSERT INTO profiles (id, name, jira_url, project_key, created_at, bug_issue_type) VALUES ('p1', 'One', 'https://j', 'ONE', '2026-01-01T00:00:00Z', 'Defect')`)
	mustExec(t, src.DB(), `INSERT INTO connection (id, workspace_id, name, url) VALUES ('p1', 'p1', 'One', 'https://j')`)
	mustExec(t, src.DB(), `INSERT INTO app_setting (key, value) VALUES ('theme', 'dark')`)
	// A row already in the shared file must win over the copy.
	mustExec(t, dst.DB(), `INSERT INTO app_setting (key, value) VALUES ('theme', 'light')`)

	if err := sharedmigrate.ImportFromStore(src.DB(), dst.DB()); err != nil {
		t.Fatalf("first import: %v", err)
	}
	if got := count(t, dst.DB(), "profiles"); got != 1 {
		t.Fatalf("profiles copied = %d; want 1", got)
	}
	if got := count(t, dst.DB(), "connection"); got != 1 {
		t.Fatalf("connections copied = %d; want 1", got)
	}
	var theme string
	if err := dst.DB().QueryRow(`SELECT value FROM app_setting WHERE key = 'theme'`).Scan(&theme); err != nil || theme != "light" {
		t.Fatalf("theme = %q, %v; existing shared value must be kept", theme, err)
	}

	// A second profile added to the old store afterwards must NOT be copied:
	// the import is a one-time move, not a sync.
	mustExec(t, src.DB(), `INSERT INTO profiles (id, name, jira_url, project_key, created_at) VALUES ('p2', 'Two', 'https://j', 'TWO', '2026-01-02T00:00:00Z')`)
	if err := sharedmigrate.ImportFromStore(src.DB(), dst.DB()); err != nil {
		t.Fatalf("second import: %v", err)
	}
	if got := count(t, dst.DB(), "profiles"); got != 1 {
		t.Fatalf("second import copied again: profiles = %d; want 1", got)
	}
}
```

Add the two helpers at the bottom of the same file (the interface in `count` is only there to accept both handles; keep it):

```go
type shareddbDB = interface {
	DB() *sql.DB
	Close() error
}

func mustExec(t *testing.T, db *sql.DB, stmt string) {
	t.Helper()
	if _, err := db.Exec(stmt); err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
}
```

and add `"database/sql"` to the imports. Simplify `count` to take `*sql.DB` directly:

```go
func count(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}
```

and call it as `count(t, dst.DB(), "profiles")` (already the case above).

- [ ] **Step 2: Run it to confirm it fails**

```bash
cd xtm && go test ./internal/sharedmigrate/ 2>&1 | head -3
```

Expected: `undefined: sharedmigrate.ImportFromStore`.

- [ ] **Step 3: Write the migration**

Create `xtm/internal/sharedmigrate/sharedmigrate.go`:

```go
// Package sharedmigrate moves XTM's profiles, connections, and settings out of
// its own database and into the shared profile database the suite's apps
// read. It runs once per shared file: after the copy it records a marker in
// the shared meta table and never copies again, so the shared file becomes
// the only place these rows are edited. The rows in XTM's own database are
// left in place as a backup and are no longer read.
package sharedmigrate

import (
	"database/sql"
	"errors"
	"fmt"
)

const markerKey = "xtm_profiles_imported"

// ImportFromStore copies profiles, connection, and app_setting rows from src
// (XTM's store) into dst (the shared database) unless dst already carries the
// import marker. Rows that already exist in dst are kept.
func ImportFromStore(src, dst *sql.DB) error {
	done, err := imported(dst)
	if err != nil {
		return err
	}
	if done {
		return nil
	}
	tx, err := dst.Begin()
	if err != nil {
		return fmt.Errorf("begin import: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := copyRows(tx, src,
		`SELECT id, name, jira_url, project_key, created_at, scope_jql, bug_issue_type, bug_project_mode, bug_project_key, ca_cert, allow_untrusted_tls, backend, cross_project_sources FROM profiles`,
		`INSERT OR IGNORE INTO profiles (id, name, jira_url, project_key, created_at, scope_jql, bug_issue_type, bug_project_mode, bug_project_key, ca_cert, allow_untrusted_tls, backend, cross_project_sources) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		13); err != nil {
		return fmt.Errorf("copy profiles: %w", err)
	}
	if err := copyRows(tx, src,
		`SELECT id, workspace_id, name, backend, url, project_key, scope_jql, bug_issue_type, bug_project_mode, bug_project_key, ca_cert, allow_untrusted_tls, role, created_at FROM connection`,
		`INSERT OR IGNORE INTO connection (id, workspace_id, name, backend, url, project_key, scope_jql, bug_issue_type, bug_project_mode, bug_project_key, ca_cert, allow_untrusted_tls, role, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		14); err != nil {
		return fmt.Errorf("copy connections: %w", err)
	}
	if err := copyRows(tx, src,
		`SELECT key, value FROM app_setting`,
		`INSERT OR IGNORE INTO app_setting (key, value) VALUES (?, ?)`,
		2); err != nil {
		return fmt.Errorf("copy settings: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO meta (key, value) VALUES (?, '1') ON CONFLICT(key) DO UPDATE SET value = '1'`,
		markerKey,
	); err != nil {
		return fmt.Errorf("record import marker: %w", err)
	}
	return tx.Commit()
}

func imported(dst *sql.DB) (bool, error) {
	var v string
	err := dst.QueryRow(`SELECT value FROM meta WHERE key = ?`, markerKey).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read import marker: %w", err)
	}
	return v == "1", nil
}

// copyRows streams every row of selectSQL from src into insertSQL on tx. cols
// is the column count shared by both statements.
func copyRows(tx *sql.Tx, src *sql.DB, selectSQL, insertSQL string, cols int) error {
	rows, err := src.Query(selectSQL)
	if err != nil {
		return err
	}
	defer rows.Close()
	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		return err
	}
	defer stmt.Close()
	vals := make([]any, cols)
	ptrs := make([]any, cols)
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		if _, err := stmt.Exec(vals...); err != nil {
			return err
		}
	}
	return rows.Err()
}
```

- [ ] **Step 4: Run the migration test**

```bash
cd xtm && go test ./internal/sharedmigrate/ -v 2>&1 | grep -E "^(--- |ok|FAIL)"
```

Expected: `--- PASS: TestImportCopiesRowsOnceAndKeepsExistingTargets` and `ok`.

- [ ] **Step 5: Write the failing bug-settings test**

Create `xtm/internal/testrepo/bugs_settings_test.go`:

```go
package testrepo_test

import (
	"errors"
	"path/filepath"
	"testing"

	"agile-suite/xtm/internal/store"
	"agile-suite/xtm/internal/testrepo"
)

func newRepo(t *testing.T) *testrepo.Repository {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "xtm.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return testrepo.NewRepository(st)
}

func TestBugSettingsDefaultWithoutALookup(t *testing.T) {
	r := newRepo(t)
	if got := r.ProfileBugIssueType("p1"); got != "Bug" {
		t.Fatalf("issue type = %q; want Bug", got)
	}
	if got := r.ProfileBugProjectMode("p1"); got != "test" {
		t.Fatalf("mode = %q; want test", got)
	}
	if got := r.ProfileBugProjectKey("p1"); got != "" {
		t.Fatalf("key = %q; want empty", got)
	}
}

func TestBugSettingsComeFromTheLookup(t *testing.T) {
	r := newRepo(t)
	r.SetBugSettingsLookup(func(id string) (testrepo.BugSettings, error) {
		if id != "p1" {
			return testrepo.BugSettings{}, errors.New("unknown profile")
		}
		return testrepo.BugSettings{IssueType: "Defect", ProjectMode: "dedicated", ProjectKey: "BUGS"}, nil
	})
	if got := r.ProfileBugIssueType("p1"); got != "Defect" {
		t.Fatalf("issue type = %q; want Defect", got)
	}
	if got := r.ProfileBugProjectMode("p1"); got != "dedicated" {
		t.Fatalf("mode = %q; want dedicated", got)
	}
	if got := r.ProfileBugProjectKey("p1"); got != "BUGS" {
		t.Fatalf("key = %q; want BUGS", got)
	}
	// A lookup failure falls back to the defaults rather than surfacing.
	if got := r.ProfileBugIssueType("missing"); got != "Bug" {
		t.Fatalf("issue type for unknown profile = %q; want Bug", got)
	}
}
```

- [ ] **Step 6: Run it to confirm it fails**

```bash
cd xtm && go test ./internal/testrepo/ -run TestBugSettings 2>&1 | head -3
```

Expected: `undefined: testrepo.BugSettings` (and `SetBugSettingsLookup`).

- [ ] **Step 7: Add the lookup seam to the repository**

In `xtm/internal/testrepo/testrepo.go`, replace

```go
type Repository struct {
	db *sql.DB
}
```

with

```go
type Repository struct {
	db *sql.DB
	// bugSettings answers the per-profile defect settings the syncer needs.
	// Profiles live in the shared database now, so the app injects this
	// lookup instead of the repository querying a profiles table in its own
	// file. Nil means "use the defaults".
	bugSettings func(profileID string) (BugSettings, error)
}

// BugSettings are the per-profile defect settings the syncer needs.
type BugSettings struct {
	IssueType   string
	ProjectMode string
	ProjectKey  string
}

// SetBugSettingsLookup installs the function used to resolve a profile's
// defect settings.
func (r *Repository) SetBugSettingsLookup(fn func(profileID string) (BugSettings, error)) {
	r.bugSettings = fn
}
```

In `xtm/internal/testrepo/bugs.go`, replace the three readers (lines 81-111) with:

```go
// ProfileBugIssueType returns the profile's configured defect issuetype,
// defaulting to "Bug".
func (r *Repository) ProfileBugIssueType(profileID string) string {
	s, ok := r.lookupBugSettings(profileID)
	if !ok || strings.TrimSpace(s.IssueType) == "" {
		return "Bug"
	}
	return s.IssueType
}

// ProfileBugProjectMode returns the profile's bug-project mode
// ("test" | "execution" | "dedicated"), defaulting to "test".
func (r *Repository) ProfileBugProjectMode(profileID string) string {
	s, ok := r.lookupBugSettings(profileID)
	if !ok || strings.TrimSpace(s.ProjectMode) == "" {
		return "test"
	}
	return strings.TrimSpace(s.ProjectMode)
}

// ProfileBugProjectKey returns the profile's dedicated bug project key
// (non-empty only when the mode is "dedicated").
func (r *Repository) ProfileBugProjectKey(profileID string) string {
	s, ok := r.lookupBugSettings(profileID)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s.ProjectKey)
}

// lookupBugSettings runs the injected lookup; ok is false when no lookup is
// installed or it fails, in which case callers use their defaults.
func (r *Repository) lookupBugSettings(profileID string) (BugSettings, bool) {
	if r.bugSettings == nil {
		return BugSettings{}, false
	}
	s, err := r.bugSettings(profileID)
	if err != nil {
		return BugSettings{}, false
	}
	return s, true
}
```

- [ ] **Step 8: Run the bug-settings tests**

```bash
cd xtm && go test ./internal/testrepo/ -run TestBugSettings -v 2>&1 | grep -E "^(--- |ok|FAIL)"
```

Expected: two `--- PASS` lines and `ok`.

- [ ] **Step 9: Wire the shared database into XTM's startup**

In `xtm/app.go`, add the import `"agile-suite/core/shareddb"` and `"agile-suite/xtm/internal/sharedmigrate"`, and add two fields to the `App` struct next to `store`:

```go
	store  *store.Store
	shared *coreStore.DB
	sharedPath string
```

where `coreStore` is the alias for `"agile-suite/core/store"` (add `coreStore "agile-suite/core/store"` to the imports; XTM's own `store` package keeps its short name).

Replace the body of `initStore` after `a.store = st` with:

```go
	a.store = st

	sharedPath, err := shareddb.DefaultPath()
	if err != nil {
		return fmt.Errorf("resolve shared profile database path: %w", err)
	}
	shared, err := shareddb.Open(sharedPath)
	if err != nil {
		return fmt.Errorf("open shared profile database at %s: %w", sharedPath, err)
	}
	a.shared = shared
	a.sharedPath = sharedPath
	// First run after the upgrade: carry this install's profiles, connections,
	// and settings into the shared file. A no-op on every later start.
	if err := sharedmigrate.ImportFromStore(st.DB(), shared.DB()); err != nil {
		return fmt.Errorf("import profiles into the shared database: %w", err)
	}

	a.profiles = profile.NewManager(shared.DB())
	a.connections = connection.NewManager(shared.DB())
	a.bridgeMappings = bridge.NewMappingStore(st)
	a.creds = profile.NewCredentialStore()
	a.settings = settings.NewManager(shared.DB())
	a.repo = testrepo.NewRepository(st)
	a.repo.SetBugSettingsLookup(func(id string) (testrepo.BugSettings, error) {
		p, err := a.profiles.Get(id)
		if err != nil {
			return testrepo.BugSettings{}, err
		}
		return testrepo.BugSettings{
			IssueType:   p.BugIssueType,
			ProjectMode: p.BugProjectMode,
			ProjectKey:  p.BugProjectKey,
		}, nil
	})
	a.cov = coverage.New(st, a.repo)
	log.Printf("xtm: local store ready at %s; shared profiles at %s", dbPath, sharedPath)
	return nil
```

In `shutdown`, close the shared database alongside the store. Find the existing `a.store.Close()` call and add, immediately before it:

```go
	if a.shared != nil {
		if err := a.shared.Close(); err != nil {
			log.Printf("xtm: close shared profile database: %v", err)
		}
	}
```

- [ ] **Step 10: Run XTM's full suite and build**

```bash
cd xtm && go vet ./... && go test ./internal/... 2>&1 | grep -E "^(ok|FAIL)" | sort | uniq -c | head -3
go build ./... && echo BUILD_OK
```

Expected: all `ok`, no `FAIL`, `BUILD_OK`. If any existing test seeded the `profiles` table to drive bug-settings reads it will now see the defaults; find such tests with `grep -rln "bug_issue_type\|ProfileBugIssueType" internal --include='*_test.go'` and give them a `SetBugSettingsLookup` returning the values they seeded. (As of this plan, none do.)

- [ ] **Step 11: Smoke-test the upgrade path by hand**

Run `wails dev` from `xtm/` against an existing install:

1. Before launch, note the profiles the app shows.
2. Launch; the log should print `shared profiles at ...\agile-suite\profiles.db`.
3. The profile picker shows the same profiles; the token still works (Sync succeeds, or the DEMO profile syncs).
4. Quit and relaunch: the log shows the same path and no second import (the marker test covers the logic; this confirms the wiring).

- [ ] **Step 12: Commit**

```bash
git add -A
git commit -m "feat(xtm): read profiles, connections, and settings from the shared database

XTM opens the suite's shared profile database next to its own store,
copies its existing profiles, connections, and settings across once (the
import records a marker and never runs again; the old rows stay as a
backup), and reads them from the shared file from then on. A connection
set up in XTM is now visible to any other suite app.

The three places in testrepo that read profile columns with raw SQL now
go through a lookup the app injects from the profile manager, since those
rows no longer live in store.db."
```

---

## Self-review notes

- Spec coverage: §3 layout (Task 1), `core/store` and the pull-based extraction rule (Tasks 2-3), shared profiles + credentials seam (§7, Tasks 3-4), "each app keeps its own SQLite file; only the profile store is shared" (Task 4), verification rule that every core PR keeps XTM green (each task's verify step). `core/jira`, `core/journal`, `core/backend`, `core/demo`, and `frontend/core` are deliberately not in this plan: nothing in 0a reaches for them; they arrive with plan 0b and Phase 1.
- Type consistency: `store.DB` / `store.Schema` / `store.Migration` (Task 2) are what `shareddb` (Task 3) and `sharedmigrate` (Task 4) use; `profile.NewManager(db *sql.DB)` (Task 3) is what `app.go` calls in Tasks 3 and 4; `testrepo.BugSettings` / `SetBugSettingsLookup` are defined and consumed in Task 4 only.
- Placeholders: none. Every code step carries the code; every command carries its expected result.
