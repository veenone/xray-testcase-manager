// Package store is the local SQLite persistence layer.
//
// The local database is a cache of Jira/Xray data plus a journal of pending
// changes; Jira remains the system of record. SQLite is accessed through the
// pure-Go modernc.org/sqlite driver so the app ships as a single binary with
// no cgo toolchain.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

// schemaVersion is bumped whenever the schema changes.
const schemaVersion = 30

// SchemaVersion returns the schema version this build writes — surfaced in the
// diagnostics view (FR-12.4).
func SchemaVersion() int { return schemaVersion }

// baseSchema is the canonical table layout for a fresh install. Indexes that
// might reference columns added by a migration live in indexSchema instead,
// so applyMigrations runs *between* baseSchema and indexSchema — that way an
// older database has its missing columns added before any index tries to
// reference them.
const baseSchema = `
CREATE TABLE IF NOT EXISTS meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

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
	allow_untrusted_tls INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS sync_state (
	profile_id     TEXT PRIMARY KEY,
	last_synced_at TEXT,
	test_count     INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS test_folder (
	profile_id       TEXT NOT NULL,
	id               TEXT NOT NULL,
	parent_id        TEXT NOT NULL DEFAULT '',
	name             TEXT NOT NULL,
	test_count       INTEGER NOT NULL DEFAULT 0,
	total_test_count INTEGER NOT NULL DEFAULT 0,
	xray_id          TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (profile_id, id)
);

CREATE TABLE IF NOT EXISTS test_case (
	profile_id  TEXT NOT NULL,
	jira_key    TEXT NOT NULL,
	jira_id     TEXT NOT NULL,
	summary     TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	status      TEXT NOT NULL DEFAULT '',
	priority    TEXT NOT NULL DEFAULT '',
	labels      TEXT NOT NULL DEFAULT '',
	updated_at  TEXT NOT NULL DEFAULT '',
	folder_id   TEXT NOT NULL DEFAULT '',
	components  TEXT NOT NULL DEFAULT '',
	exec_type   TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (profile_id, jira_key)
);

CREATE TABLE IF NOT EXISTS precondition (
	profile_id  TEXT NOT NULL,
	jira_key    TEXT NOT NULL,
	summary     TEXT NOT NULL,
	type        TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (profile_id, jira_key)
);

CREATE TABLE IF NOT EXISTS test_precondition (
	profile_id       TEXT NOT NULL,
	test_key         TEXT NOT NULL,
	precondition_key TEXT NOT NULL,
	PRIMARY KEY (profile_id, test_key, precondition_key)
);

CREATE TABLE IF NOT EXISTS pending_change (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	profile_id   TEXT NOT NULL,
	entity_type  TEXT NOT NULL,
	entity_key   TEXT NOT NULL,
	field        TEXT NOT NULL,
	before_val   TEXT NOT NULL DEFAULT '',
	after_val    TEXT NOT NULL DEFAULT '',
	base_version TEXT NOT NULL DEFAULT '',
	created_at   TEXT NOT NULL,
	UNIQUE (profile_id, entity_type, entity_key, field)
);

CREATE TABLE IF NOT EXISTS audit_log (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	profile_id  TEXT NOT NULL,
	occurred_at TEXT NOT NULL,
	actor       TEXT NOT NULL DEFAULT '',
	entity_type TEXT NOT NULL,
	entity_key  TEXT NOT NULL,
	action      TEXT NOT NULL,
	field       TEXT NOT NULL DEFAULT '',
	before_val  TEXT NOT NULL DEFAULT '',
	after_val   TEXT NOT NULL DEFAULT '',
	note        TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS test_step (
	profile_id      TEXT NOT NULL,
	test_key        TEXT NOT NULL,
	xray_id         TEXT NOT NULL,
	idx             INTEGER NOT NULL,
	action          TEXT NOT NULL DEFAULT '',
	data            TEXT NOT NULL DEFAULT '',
	expected        TEXT NOT NULL DEFAULT '',
	called_test_key TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (profile_id, test_key, xray_id)
);

CREATE TABLE IF NOT EXISTS test_container (
	profile_id TEXT NOT NULL,
	jira_key   TEXT NOT NULL,
	kind       TEXT NOT NULL,
	summary    TEXT NOT NULL DEFAULT '',
	status     TEXT NOT NULL DEFAULT '',
	parent_key TEXT NOT NULL DEFAULT '',
	issue_type TEXT NOT NULL DEFAULT '',
	environments TEXT NOT NULL DEFAULT '',
	fix_versions TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (profile_id, jira_key)
);

CREATE TABLE IF NOT EXISTS test_container_test (
	profile_id    TEXT NOT NULL,
	container_key TEXT NOT NULL,
	test_key      TEXT NOT NULL,
	run_status    TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (profile_id, container_key, test_key)
);

CREATE TABLE IF NOT EXISTS saved_view (
	profile_id TEXT NOT NULL,
	id         TEXT NOT NULL,
	name       TEXT NOT NULL,
	query      TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	PRIMARY KEY (profile_id, id)
);

CREATE TABLE IF NOT EXISTS custom_field (
	profile_id TEXT NOT NULL,
	field_id   TEXT NOT NULL,
	name       TEXT NOT NULL,
	type       TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (profile_id, field_id)
);

CREATE TABLE IF NOT EXISTS test_custom_field (
	profile_id TEXT NOT NULL,
	test_key   TEXT NOT NULL,
	field_id   TEXT NOT NULL,
	value      TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (profile_id, test_key, field_id)
);

CREATE TABLE IF NOT EXISTS sync_log (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	profile_id  TEXT NOT NULL,
	started_at  TEXT NOT NULL,
	finished_at TEXT NOT NULL DEFAULT '',
	outcome     TEXT NOT NULL DEFAULT '',
	fetched     INTEGER NOT NULL DEFAULT 0,
	error       TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS app_setting (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS test_review (
	profile_id  TEXT NOT NULL,
	test_key    TEXT NOT NULL,
	verdict     TEXT NOT NULL DEFAULT '',
	reviewer    TEXT NOT NULL DEFAULT '',
	note        TEXT NOT NULL DEFAULT '',
	reviewed_at TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (profile_id, test_key)
);

-- Requirement issues linked to Tests for coverage/traceability. A requirement
-- may live in a different project than the profile's Test project, so it carries
-- its own project_key.
CREATE TABLE IF NOT EXISTS requirement (
	profile_id  TEXT NOT NULL,
	jira_key    TEXT NOT NULL,
	project_key TEXT NOT NULL DEFAULT '',
	issue_type  TEXT NOT NULL DEFAULT '',
	summary     TEXT NOT NULL DEFAULT '',
	status      TEXT NOT NULL DEFAULT '',
	updated_at  TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (profile_id, jira_key)
);

-- Test <-> Requirement coverage links (the Jira issue link). link_id is Jira's
-- issueLink id, kept so a link can be removed precisely.
CREATE TABLE IF NOT EXISTS test_requirement (
	profile_id      TEXT NOT NULL,
	test_key        TEXT NOT NULL,
	requirement_key TEXT NOT NULL,
	link_id         TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (profile_id, test_key, requirement_key)
);

-- Where to look for requirements to browse/manage (besides those already linked
-- to synced Tests, which are fetched by key regardless of project).
CREATE TABLE IF NOT EXISTS requirement_source (
	profile_id  TEXT NOT NULL,
	project_key TEXT NOT NULL,
	issue_types TEXT NOT NULL DEFAULT '',
	scope_jql   TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (profile_id, project_key)
);

CREATE TABLE IF NOT EXISTS duplicate_ignore (
	profile_id TEXT NOT NULL,
	test_key   TEXT NOT NULL,
	created_at TEXT NOT NULL,
	PRIMARY KEY (profile_id, test_key)
);

CREATE TABLE IF NOT EXISTS duplicate_step_scan (
	profile_id  TEXT NOT NULL,
	test_key    TEXT NOT NULL,
	fingerprint TEXT NOT NULL,
	scanned_at  TEXT NOT NULL,
	PRIMARY KEY (profile_id, test_key)
);

-- Defect issues (possibly cross-project) linked to Tests. Discovered via
-- issuelinks on sync; also created local-first via CreateBugForTest.
CREATE TABLE IF NOT EXISTS bug (
	profile_id  TEXT NOT NULL,
	jira_key    TEXT NOT NULL,
	project_key TEXT NOT NULL DEFAULT '',
	issue_type  TEXT NOT NULL DEFAULT '',
	summary     TEXT NOT NULL DEFAULT '',
	status      TEXT NOT NULL DEFAULT '',
	priority    TEXT NOT NULL DEFAULT '',
	updated_at  TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (profile_id, jira_key)
);

-- Test <-> Bug links. link_id is Jira's issueLink id.
CREATE TABLE IF NOT EXISTS test_bug (
	profile_id TEXT NOT NULL,
	test_key   TEXT NOT NULL,
	bug_key    TEXT NOT NULL,
	link_id    TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (profile_id, test_key, bug_key)
);

-- Cache of the basics of external member Tests: Tests that belong to a Test
-- Execution (or other container) but live in a different Jira project than the
-- profile's, so the bulk test pull (which fetches only the profile's project)
-- never caches them in test_case. The container board LEFT JOINs this so such
-- members still render with a summary/status instead of being dropped.
CREATE TABLE IF NOT EXISTS external_test (
	profile_id  TEXT NOT NULL,
	jira_key    TEXT NOT NULL,
	summary     TEXT NOT NULL DEFAULT '',
	status      TEXT NOT NULL DEFAULT '',
	project_key TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (profile_id, jira_key)
);

-- Per-execution run details for each Test: status, timing, executor,
-- environment, and defect keys as returned by the Xray Test Run REST API.
-- created_at and updated_at carry the run's creation and last-update timestamps
-- from Xray (ISO-8601), enabling sorting by recency (schema v30).
CREATE TABLE IF NOT EXISTS test_run (
	profile_id  TEXT NOT NULL,
	exec_key    TEXT NOT NULL,
	test_key    TEXT NOT NULL,
	run_status  TEXT DEFAULT '',
	started_at  TEXT DEFAULT '',
	finished_at TEXT DEFAULT '',
	executed_by TEXT DEFAULT '',
	environment TEXT DEFAULT '',
	defects     TEXT DEFAULT '',
	created_at  TEXT DEFAULT '',
	updated_at  TEXT DEFAULT '',
	PRIMARY KEY (profile_id, exec_key, test_key)
);

-- Explicit link from a Test Execution to the Test Plan(s) it belongs to.
-- Mirrors the Xray association returned by the Test Execution detail endpoint.
CREATE TABLE IF NOT EXISTS exec_plan (
	profile_id TEXT NOT NULL,
	exec_key   TEXT NOT NULL,
	plan_key   TEXT NOT NULL,
	PRIMARY KEY (profile_id, exec_key, plan_key)
);
`

// indexSchema is applied *after* applyMigrations so every column referenced
// here is guaranteed to exist (either from baseSchema on a fresh install or
// from an ALTER on an upgraded database).
const indexSchema = `
CREATE INDEX IF NOT EXISTS idx_test_folder_parent      ON test_folder(profile_id, parent_id);
CREATE INDEX IF NOT EXISTS idx_test_case_status        ON test_case(profile_id, status);
CREATE INDEX IF NOT EXISTS idx_test_case_updated       ON test_case(profile_id, updated_at);
CREATE INDEX IF NOT EXISTS idx_test_case_folder        ON test_case(profile_id, folder_id);
CREATE INDEX IF NOT EXISTS idx_test_precondition_test  ON test_precondition(profile_id, test_key);
CREATE INDEX IF NOT EXISTS idx_test_requirement_test   ON test_requirement(profile_id, test_key);
CREATE INDEX IF NOT EXISTS idx_test_requirement_req    ON test_requirement(profile_id, requirement_key);
CREATE INDEX IF NOT EXISTS idx_pending_change_profile  ON pending_change(profile_id);
CREATE INDEX IF NOT EXISTS idx_audit_log_profile_time  ON audit_log(profile_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_test_step_test          ON test_step(profile_id, test_key, idx);
CREATE INDEX IF NOT EXISTS idx_test_container_kind     ON test_container(profile_id, kind);
CREATE INDEX IF NOT EXISTS idx_test_container_test_key ON test_container_test(profile_id, test_key);
CREATE INDEX IF NOT EXISTS idx_sync_log_profile_time   ON sync_log(profile_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_duplicate_ignore_profile   ON duplicate_ignore(profile_id);
CREATE INDEX IF NOT EXISTS idx_duplicate_step_scan_profile ON duplicate_step_scan(profile_id);
CREATE INDEX IF NOT EXISTS idx_test_bug_test ON test_bug(profile_id, test_key);
CREATE INDEX IF NOT EXISTS idx_test_bug_bug  ON test_bug(profile_id, bug_key);
`

// Store wraps the SQLite connection for one local database file.
type Store struct {
	db *sql.DB
}

// Open opens (creating if absent) the SQLite database at path. The sequence
// is: create / verify tables → run migrations → create indexes. Splitting
// indexes off ensures every column an index references already exists.
func Open(path string) (*Store, error) {
	// busy_timeout makes a transiently-locked database wait rather than fail
	// immediately with "database is locked" — e.g. when a previous instance
	// is still releasing the file as a new one launches. The _pragma query
	// is applied by the modernc driver to every pooled connection.
	// WAL lets readers and a writer proceed concurrently (so the UI's many
	// parallel queries don't serialise behind a sync's write transaction), and
	// busy_timeout makes a transiently-locked database wait rather than fail
	// Open with "database is locked". Both pragmas are applied by the modernc
	// driver to every pooled connection.
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if _, err := db.Exec(baseSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply tables: %w", err)
	}
	if err := applyMigrations(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply migrations: %w", err)
	}
	if _, err := db.Exec(indexSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply indexes: %w", err)
	}
	if _, err := db.Exec(
		"INSERT INTO meta (key, value) VALUES ('schema_version', ?) "+
			"ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		schemaVersion,
	); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("record schema version: %w", err)
	}
	return &Store{db: db}, nil
}

// applyMigrations upgrades older databases to the current schema. Fresh
// installs already match the canonical layout via CREATE TABLE IF NOT EXISTS;
// this function handles the deltas for in-place upgrades from earlier
// versions.
func applyMigrations(db *sql.DB) error {
	current, err := readSchemaVersion(db)
	if err != nil {
		return err
	}

	// v3: add folder_id to test_case. Fresh installs already have it from
	// the CREATE above; this ALTER catches v1/v2 databases. The
	// "duplicate column" error is tolerated so the migration is idempotent.
	if current < 3 {
		if _, err := db.Exec(
			`ALTER TABLE test_case ADD COLUMN folder_id TEXT NOT NULL DEFAULT ''`,
		); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("v3 add folder_id: %w", err)
		}
	}
	// v4: precondition / test_precondition tables. Additive — covered by
	// CREATE TABLE IF NOT EXISTS in baseSchema, no explicit step needed.
	// v5: pending_change / audit_log tables. Also additive.
	// v6: test_step table for cached Xray Test Steps. Also additive.
	// v7: test_container / test_container_test tables for Test Sets, Test
	// Plans and Test Executions plus their Test memberships. Also additive.
	// v8: saved_view table for saved browse filters. Also additive.
	// v9: custom_field / test_custom_field tables for Jira custom fields on the
	// Test issue type. Also additive.
	// v10: sync_log table for sync history. Also additive.
	// v12: app_setting table for global settings (default profile). Additive.
	//
	// v11: scope_jql column on profiles for the per-profile JQL scope override
	// (FR-5.4). Fresh installs get it from the CREATE above; this ALTER catches
	// pre-v11 databases.
	if current < 11 {
		if _, err := db.Exec(
			`ALTER TABLE profiles ADD COLUMN scope_jql TEXT NOT NULL DEFAULT ''`,
		); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("v11 add scope_jql: %w", err)
		}
	}
	// v14: add components to test_case for the Jira components field (group-by /
	// filter). Fresh installs get it from the CREATE above; this ALTER catches
	// pre-v14 databases. Stored as a newline-delimited, newline-bounded string
	// (see testrepo.encodeComponents) so a LIKE filter can match one component
	// exactly without partial-name collisions.
	if current < 14 {
		if _, err := db.Exec(
			`ALTER TABLE test_case ADD COLUMN components TEXT NOT NULL DEFAULT ''`,
		); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("v14 add components: %w", err)
		}
	}
	// v15: add test counts to test_folder so the folder tree can show per-folder
	// totals like Xray's Test Repository. Fresh installs get them from the CREATE
	// above; these ALTERs catch pre-v15 databases.
	if current < 15 {
		for _, col := range []string{"test_count", "total_test_count"} {
			if _, err := db.Exec(
				fmt.Sprintf(`ALTER TABLE test_folder ADD COLUMN %s INTEGER NOT NULL DEFAULT 0`, col),
			); err != nil && !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("v15 add %s: %w", col, err)
			}
		}
	}
	// v16: add the native Xray folder id to test_folder so a committed folder
	// move can address the Xray Test Repository move endpoint (which takes the
	// numeric folder id, not the path). Fresh installs get it from the CREATE.
	if current < 16 {
		if _, err := db.Exec(
			`ALTER TABLE test_folder ADD COLUMN xray_id TEXT NOT NULL DEFAULT ''`,
		); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("v16 add xray_id: %w", err)
		}
	}
	// v19: add called_test_key to test_step for "call test" steps (a step that
	// invokes another Test). Fresh installs get it from the CREATE above; this
	// ALTER catches pre-v19 databases.
	if current < 19 {
		if _, err := db.Exec(
			`ALTER TABLE test_step ADD COLUMN called_test_key TEXT NOT NULL DEFAULT ''`,
		); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("v19 add called_test_key: %w", err)
		}
	}
	// v21: per-profile bug issue type (the Jira issuetype name used when filing a
	// defect, default "Bug"). Fresh installs get it from the CREATE above; this
	// ALTER catches pre-v21 databases.
	if current < 21 {
		if _, err := db.Exec(
			`ALTER TABLE profiles ADD COLUMN bug_issue_type TEXT NOT NULL DEFAULT 'Bug'`,
		); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("v21 add bug_issue_type: %w", err)
		}
	}
	// v22: per-profile bug project — which project a filed defect lands in
	// (mode "test" / "execution" / "dedicated") and the dedicated key. Fresh
	// installs get these from the CREATE above; these ALTERs catch pre-v22 DBs.
	if current < 22 {
		for _, stmt := range []string{
			`ALTER TABLE profiles ADD COLUMN bug_project_mode TEXT NOT NULL DEFAULT 'test'`,
			`ALTER TABLE profiles ADD COLUMN bug_project_key TEXT NOT NULL DEFAULT ''`,
		} {
			if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("v22 add bug project columns: %w", err)
			}
		}
	}
	// v23: sub-task Test Execution support — parent_key (the parent issue key
	// for a sub-task execution; empty for standalone) and issue_type (the Jira
	// issuetype name, informational). Fresh installs get these from the CREATE
	// above; these ALTERs catch pre-v23 databases.
	if current < 23 {
		for _, col := range []string{"parent_key", "issue_type"} {
			if _, err := db.Exec(
				fmt.Sprintf(`ALTER TABLE test_container ADD COLUMN %s TEXT NOT NULL DEFAULT ''`, col),
			); err != nil && !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("v23 add %s: %w", col, err)
			}
		}
	}
	// v24: add exec_type to test_case for the Xray Test Type (a.k.a. execution
	// type: Manual / Automated / Generic / Cucumber). Fresh installs get it from
	// the CREATE above; this ALTER catches pre-v24 databases. The "duplicate
	// column" error is tolerated so the migration is idempotent.
	if current < 24 {
		if _, err := db.Exec(
			`ALTER TABLE test_case ADD COLUMN exec_type TEXT NOT NULL DEFAULT ''`,
		); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("v24 add exec_type: %w", err)
		}
	}
	// v25: add environments to test_container for the Xray Test Environments
	// field on Test Executions (a JSON array of environment names). Fresh
	// installs get it from the CREATE above; this ALTER catches pre-v25
	// databases. The "duplicate column" error is tolerated so the migration is
	// idempotent.
	if current < 25 {
		if _, err := db.Exec(
			`ALTER TABLE test_container ADD COLUMN environments TEXT NOT NULL DEFAULT ''`,
		); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("v25 add environments: %w", err)
		}
	}
	// v26: external_test cache for cross-project execution members (additive,
	// covered by CREATE TABLE IF NOT EXISTS, no ALTER needed).
	// v27: add fix_versions to test_container for the standard Jira Fix Version(s)
	// field on Test Executions (a JSON array of version names), shown read-only.
	// Fresh installs get it from the CREATE above; this ALTER catches pre-v27
	// databases. The "duplicate column" error is tolerated so the migration is
	// idempotent.
	if current < 27 {
		if _, err := db.Exec(
			`ALTER TABLE test_container ADD COLUMN fix_versions TEXT NOT NULL DEFAULT ''`,
		); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("v27 add fix_versions: %w", err)
		}
	}
	// v28: test_run stores per-execution run details for each Test (status,
	// timing, executor, environment, defects). exec_plan records the explicit
	// link from a Test Execution to its Test Plan(s). Both are additive new
	// tables; CREATE TABLE IF NOT EXISTS makes the migration idempotent.
	if current < 28 {
		for _, q := range []string{
			`CREATE TABLE IF NOT EXISTS test_run (
				profile_id  TEXT NOT NULL,
				exec_key    TEXT NOT NULL,
				test_key    TEXT NOT NULL,
				run_status  TEXT DEFAULT '',
				started_at  TEXT DEFAULT '',
				finished_at TEXT DEFAULT '',
				executed_by TEXT DEFAULT '',
				environment TEXT DEFAULT '',
				defects     TEXT DEFAULT '',
				PRIMARY KEY (profile_id, exec_key, test_key)
			)`,
			`CREATE TABLE IF NOT EXISTS exec_plan (
				profile_id TEXT NOT NULL,
				exec_key   TEXT NOT NULL,
				plan_key   TEXT NOT NULL,
				PRIMARY KEY (profile_id, exec_key, plan_key)
			)`,
		} {
			if _, err := db.Exec(q); err != nil && !strings.Contains(err.Error(), "already exists") {
				return fmt.Errorf("v28 create test_run/exec_plan: %w", err)
			}
		}
	}
	// v29: per-profile TLS trust options for connecting through internal CAs or
	// self-signed certificates (RND_P_4TFINT_05-243). ca_cert holds a PEM-encoded
	// CA certificate (optional); allow_untrusted_tls disables certificate
	// verification when set (1). Fresh installs get these from the CREATE above;
	// these ALTERs catch pre-v29 databases.
	if current < 29 {
		for _, stmt := range []string{
			`ALTER TABLE profiles ADD COLUMN ca_cert TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE profiles ADD COLUMN allow_untrusted_tls INTEGER NOT NULL DEFAULT 0`,
		} {
			if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("v29 add TLS columns: %w", err)
			}
		}
	}
	// v30: add created_at and updated_at to test_run so runs can be sorted by
	// recency (RND_P_4TFINT_05 run-info refinement). Both carry ISO-8601 strings
	// from Xray (empty string when unknown). Fresh installs get the columns from
	// the CREATE above; these ALTERs catch pre-v30 databases.
	if current < 30 {
		for _, col := range []string{"created_at", "updated_at"} {
			if _, err := db.Exec(
				fmt.Sprintf(`ALTER TABLE test_run ADD COLUMN %s TEXT DEFAULT ''`, col),
			); err != nil && !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("v30 add %s to test_run: %w", col, err)
			}
		}
	}
	return nil
}

// readSchemaVersion returns the recorded schema version, or 0 for a database
// that has never had one written.
func readSchemaVersion(db *sql.DB) (int, error) {
	var raw string
	err := db.QueryRow(
		"SELECT value FROM meta WHERE key = 'schema_version'",
	).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	v, _ := strconv.Atoi(raw)
	return v, nil
}

// DB exposes the underlying connection for the repository layer.
func (s *Store) DB() *sql.DB { return s.db }

// Close releases the database connection.
func (s *Store) Close() error { return s.db.Close() }
