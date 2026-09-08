# Kiwi to Jira Bug Routing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a Kiwi TCMS profile file defects into a dedicated Jira project, creating the issue in Jira and hyperlinking it back on the Kiwi execution.

**Architecture:** A Kiwi profile gains a second `connection` row with role `"bugs"` holding an independently configured Jira connection. The sync and commit engines take an optional second `backend.Backend` used only for bug work, so `CreateBug` goes to Jira while `CreateBugLink` goes to Kiwi. When no bug connection exists the second backend is nil and every path behaves exactly as it does today.

**Tech Stack:** Go 1.25+, Wails v2.15, React 19 + TypeScript, SQLite via `modernc.org/sqlite` (no cgo, so no `-race`), Kiwi TCMS JSON-RPC, Jira DC REST.

**Spec:** `docs/superpowers/specs/2026-09-04-kiwi-jira-bug-routing-design.md`

**Jira:** RND_P_4TFINT_05-359, fixVersion V1.10.0

## Global Constraints

- Credentials never reach the database, a log line, or an error string. They go to the Windows Credential Manager via `a.creds`, keyed by the connection id.
- Jira is the system of record. The local store is a cache plus a pending-change journal.
- `internal/` is import-private to this module.
- Kiwi's `SupportsBugCreation` stays `false`. Routing is reported by a separate `SupportsBugRouting` field.
- Nothing already created remotely is ever deleted to clean up a partial failure.
- **No schema change and no `schemaVersion` bump.** The `connection` table already has a `role` column (`internal/store/store.go:566`), so the bug connection is a new row in an existing table.
- Backend logic lives in `internal/`; `app.go` only adapts it to Wails bindings.
- Go tests go beside the code as `_test.go`. Frontend tests go beside the component as `.test.tsx` and run under Vitest.
- Run `gofmt -w .` before every Go commit.
- Commit messages carry no AI attribution and no `Co-Authored-By` trailer.

## File Structure

| File | Responsibility |
|---|---|
| `internal/connection/connection.go` | Add `"bugs"` to the role vocabulary, make `Primary` deterministic, add `ByRole`. |
| `internal/connection/connection_test.go` | Prove a second row cannot displace the primary. |
| `internal/backend/backend.go` | Add `SupportsBugRouting` to `Capabilities` and the `BugKeyReader` optional interface. |
| `internal/backend/xray/adapter.go` | Implement `BugKeyReader` by delegating to the Jira client. |
| `internal/jira/bugs.go` | Add `ListBugsByKeys`, a JQL `key in (...)` fetch. |
| `internal/backend/kiwi/bugs.go` (new) | Kiwi's `CreateBugLink` and `ListBugs` over `TestExecution.add_link` / `get_links`. |
| `internal/backend/kiwi/bugs_test.go` (new) | httptest-backed tests for both RPCs. |
| `internal/testrepo/bugcrud.go` | Carry `execKey` and `createdKey` in the bug-create payload; add `MarkBugCreated`. |
| `internal/syncer/engine.go` | `WithBugBackend` option; `syncBugs` hydrates linked keys from the bug backend. |
| `internal/syncer/commit.go` | `commitBugCreates` routes create and link to different backends and retries safely. |
| `app.go` | Bug-connection CRUD, `bugBackendFor`, wiring at the four `syncer.New` sites, capability merge, delete cleanup. |
| `frontend/src/components/ProfileForm.tsx` | Bug-connection section for Kiwi profiles. |
| `frontend/src/components/ProfileForm.test.tsx` | Cover the section's visibility and round-trip. |
| `CHANGELOG.md`, `docs/user-guide/USER_GUIDE.md` | Document the feature. |

---

### Task 1: A second connection row is safe

Two behaviors in `internal/connection` were written when a workspace had exactly one connection. Both must change before anything writes a second row.

**Files:**
- Modify: `internal/connection/connection.go:81-90` (`roleOrDefault`), `internal/connection/connection.go:128-135` (`Primary`)
- Test: `internal/connection/connection_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `func (m *Manager) ByRole(workspaceID, role string) (Connection, error)` returning `ErrNotFound` when no row matches; role `"bugs"` surviving a write/read round trip; `Primary(workspaceID)` returning the row whose `id == workspaceID`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/connection/connection_test.go`:

```go
// TestBugsRoleSurvivesARoundTrip pins the role vocabulary. roleOrDefault
// normalizes anything it does not recognize to "both", so a bug connection
// written before "bugs" was a known role would read back as a second PRIMARY
// connection rather than a bug connection.
func TestBugsRoleSurvivesARoundTrip(t *testing.T) {
	m := newManager(t)
	now := time.Now().UTC().Truncate(time.Second)

	if _, err := m.Create("bug1", "w1", "Bug Jira", "xray", "https://jira.example.com", "DEF",
		"", "Bug", "dedicated", "DEF", "", false, "bugs", now); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := m.Get("bug1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Role != "bugs" {
		t.Errorf("role = %q, want %q", got.Role, "bugs")
	}
}

// TestPrimaryIgnoresABugConnection is the ordering trap. Primary used
// ORDER BY created_at, id LIMIT 1, so two rows sharing a created_at break the
// tie by id, and a generated connection id can sort ahead of a profile id.
// Primary must select the row whose id equals the workspace id.
func TestPrimaryIgnoresABugConnection(t *testing.T) {
	m := newManager(t)
	now := time.Now().UTC().Truncate(time.Second)

	// "aaa" sorts before the workspace id "w1" and shares its created_at.
	if _, err := m.Create("aaa", "w1", "Bug Jira", "xray", "https://jira.example.com", "DEF",
		"", "Bug", "dedicated", "DEF", "", false, "bugs", now); err != nil {
		t.Fatalf("create bug connection: %v", err)
	}
	if _, err := m.Create("w1", "w1", "Primary", "kiwi", "https://kiwi.example.com", "SNMP",
		"", "Bug", "test", "", "", false, "both", now); err != nil {
		t.Fatalf("create primary: %v", err)
	}

	p, err := m.Primary("w1")
	if err != nil {
		t.Fatalf("primary: %v", err)
	}
	if p.ID != "w1" {
		t.Errorf("primary id = %q, want the workspace id w1 (a bug connection displaced it)", p.ID)
	}
}

// TestByRoleFindsTheBugConnection is the lookup the app uses to decide whether
// a profile routes bugs. No profile column records it: the role on the
// connection row is the single source of truth.
func TestByRoleFindsTheBugConnection(t *testing.T) {
	m := newManager(t)
	now := time.Now().UTC().Truncate(time.Second)

	if _, err := m.Create("w1", "w1", "Primary", "kiwi", "https://kiwi.example.com", "SNMP",
		"", "Bug", "test", "", "", false, "both", now); err != nil {
		t.Fatalf("create primary: %v", err)
	}
	if _, err := m.ByRole("w1", "bugs"); !errors.Is(err, connection.ErrNotFound) {
		t.Fatalf("ByRole on a workspace with no bug connection = %v, want ErrNotFound", err)
	}

	if _, err := m.Create("bug1", "w1", "Bug Jira", "xray", "https://jira.example.com", "DEF",
		"", "Bug", "dedicated", "DEF", "", false, "bugs", now); err != nil {
		t.Fatalf("create bug connection: %v", err)
	}
	got, err := m.ByRole("w1", "bugs")
	if err != nil {
		t.Fatalf("ByRole: %v", err)
	}
	if got.ID != "bug1" || got.ProjectKey != "DEF" {
		t.Errorf("got = %+v, want the bug connection bug1/DEF", got)
	}
}
```

Add `"errors"` to the test file's import block if it is not already there.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/connection/ -run 'TestBugsRole|TestPrimaryIgnores|TestByRole' -v`

Expected: FAIL. `TestBugsRoleSurvivesARoundTrip` reports `role = "both", want "bugs"`; `TestPrimaryIgnoresABugConnection` reports `primary id = "aaa"`; `TestByRoleFindsTheBugConnection` fails to compile with `m.ByRole undefined`.

- [ ] **Step 3: Widen the role vocabulary**

In `internal/connection/connection.go`, replace the `roleOrDefault` switch case list:

```go
// roleOrDefault normalises the connection role, falling back to "both" for
// blank or unrecognized values.
//
// "bugs" names a connection used only for bug work: a Kiwi workspace whose
// defects are filed into a Jira project holds one alongside its primary
// connection (RND_P_4TFINT_05-359). It must be listed here, because an
// unrecognized role silently reads back as "both", which would turn a bug
// connection into a second primary.
func roleOrDefault(role string) string {
	switch strings.TrimSpace(role) {
	case "source", "target", "both", "bugs":
		return strings.TrimSpace(role)
	default:
		return "both"
	}
}
```

Update the `Role` field's doc comment on the `Connection` struct:

```go
	// Role is 'source', 'target', 'both', or 'bugs'. A single-connection
	// workspace's connection is always 'both'. 'bugs' marks the Jira
	// connection a Kiwi workspace files its defects into.
	Role      string    `json:"role"`
```

- [ ] **Step 4: Make Primary deterministic and add ByRole**

Replace `Primary` in `internal/connection/connection.go` and add `ByRole` after it:

```go
// Primary returns the workspace's primary connection: the row whose id equals
// the workspace id, which is the invariant internal/profile's syncConnection
// maintains.
//
// This selects by id rather than taking the first row by (created_at, id). A
// workspace can now hold a second, non-primary connection (role "bugs"), and
// both rows can carry the same created_at, which left the tie to be broken by
// id: a generated connection id sorting before a profile id would return the
// wrong row.
func (m *Manager) Primary(workspaceID string) (Connection, error) {
	row := m.db.QueryRow(
		`SELECT `+selectColumns+` FROM connection WHERE workspace_id = ? AND id = ?`,
		workspaceID, workspaceID)
	c, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Connection{}, ErrNotFound
	}
	return c, err
}

// ByRole returns the workspace's connection with the given role, or
// ErrNotFound. It is how a caller asks "does this workspace route bugs
// somewhere else?" without a profile column recording the answer: the role on
// the connection row is the single source of truth. When more than one row
// shares a role the oldest wins, which keeps the result stable.
func (m *Manager) ByRole(workspaceID, role string) (Connection, error) {
	row := m.db.QueryRow(
		`SELECT `+selectColumns+` FROM connection WHERE workspace_id = ? AND role = ? ORDER BY created_at, id LIMIT 1`,
		workspaceID, roleOrDefault(role))
	c, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Connection{}, ErrNotFound
	}
	return c, err
}
```

- [ ] **Step 5: Run the whole connection and profile suites**

Run: `go test ./internal/connection/ ./internal/profile/ -v`

Expected: PASS. The profile suite matters here because `Manager.syncConnection` writes the primary row and `profile_connection_sync_test.go` asserts `Primary` finds it.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/connection
git add internal/connection/connection.go internal/connection/connection_test.go
git commit -m "feat(connection): allow a second 'bugs' connection per workspace (-359)"
```

---

### Task 2: The bug-create payload carries execKey and createdKey

Kiwi hyperlinks live on a Test Execution, not a Test, so the commit path needs the execution key that `CreateBugForTest` already receives but currently only writes to the audit log. `createdKey` is what makes a retry safe: it records that the Jira issue already exists so a second commit attempt links instead of creating a duplicate.

**Files:**
- Modify: `internal/testrepo/bugcrud.go`
- Test: `internal/testrepo/bugcrud_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: the `bug_create` pending change's `AfterVal` JSON gains `"execKey"` (written at queue time) and `"createdKey"` (written only after a remote create succeeded but its link failed); `func (r *Repository) MarkBugCreated(profileID string, changeID int64, realKey string) error`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/testrepo/bugcrud_test.go`:

```go
// TestBugCreatePayloadCarriesTheExecutionKey pins the payload shape the commit
// path reads. Kiwi hyperlinks attach to a Test Execution, so the execution key
// has to survive from the queue into the commit; before this it was written
// only to the audit log.
func TestBugCreatePayloadCarriesTheExecutionKey(t *testing.T) {
	repo, profileID := newRepoWithProfile(t)

	if _, err := repo.CreateBugForTest(profileID, "T-1", "EXEC-9", testrepo.BugDraft{
		ProjectKey: "DEF", IssueType: "Bug", Summary: "Boom",
	}); err != nil {
		t.Fatalf("create bug: %v", err)
	}

	rows, err := repo.ListPendingChanges(profileID)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	var payload struct {
		ExecKey    string `json:"execKey"`
		CreatedKey string `json:"createdKey"`
	}
	found := false
	for _, r := range rows {
		if r.EntityType != "bug_create" {
			continue
		}
		found = true
		if err := json.Unmarshal([]byte(r.AfterVal), &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
	}
	if !found {
		t.Fatal("no bug_create pending change was queued")
	}
	if payload.ExecKey != "EXEC-9" {
		t.Errorf("execKey = %q, want EXEC-9", payload.ExecKey)
	}
	if payload.CreatedKey != "" {
		t.Errorf("createdKey = %q, want empty before any commit attempt", payload.CreatedKey)
	}
}

// TestMarkBugCreatedRecordsTheRealKey is what makes a retry safe. When Jira
// created the issue but the link back failed, the pending row survives so the
// user can retry. Without the real key recorded, that retry would create a
// SECOND Jira issue.
func TestMarkBugCreatedRecordsTheRealKey(t *testing.T) {
	repo, profileID := newRepoWithProfile(t)

	if _, err := repo.CreateBugForTest(profileID, "T-1", "EXEC-9", testrepo.BugDraft{
		ProjectKey: "DEF", IssueType: "Bug", Summary: "Boom",
	}); err != nil {
		t.Fatalf("create bug: %v", err)
	}
	rows, err := repo.ListPendingChanges(profileID)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	var changeID int64
	for _, r := range rows {
		if r.EntityType == "bug_create" {
			changeID = r.ID
		}
	}
	if changeID == 0 {
		t.Fatal("no bug_create pending change was queued")
	}

	if err := repo.MarkBugCreated(profileID, changeID, "DEF-42"); err != nil {
		t.Fatalf("mark created: %v", err)
	}

	rows, err = repo.ListPendingChanges(profileID)
	if err != nil {
		t.Fatalf("list pending after mark: %v", err)
	}
	var payload struct {
		CreatedKey string `json:"createdKey"`
		Summary    string `json:"summary"`
		ExecKey    string `json:"execKey"`
	}
	for _, r := range rows {
		if r.ID != changeID {
			continue
		}
		if err := json.Unmarshal([]byte(r.AfterVal), &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
	}
	if payload.CreatedKey != "DEF-42" {
		t.Errorf("createdKey = %q, want DEF-42", payload.CreatedKey)
	}
	// The rest of the payload must survive: the retry still needs it to link.
	if payload.Summary != "Boom" || payload.ExecKey != "EXEC-9" {
		t.Errorf("mark overwrote the rest of the payload: %+v", payload)
	}
}
```

If `newRepoWithProfile` does not exist in that package's tests, use whatever helper the neighbouring tests in `internal/testrepo/bugcrud_test.go` already use to build a `*testrepo.Repository` with a profile row, and keep the same signature shape. Add `"encoding/json"` to the imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/testrepo/ -run 'TestBugCreatePayloadCarries|TestMarkBugCreated' -v`

Expected: FAIL. The first reports `execKey = "", want EXEC-9`; the second fails to compile with `repo.MarkBugCreated undefined`.

- [ ] **Step 3: Widen the payload and add MarkBugCreated**

In `internal/testrepo/bugcrud.go`, add the two fields to `bugCreatePayload`:

```go
	// ExecKey is the Test Execution the bug was raised from, or "" when it was
	// raised from a Test outside any execution. Kiwi hyperlinks attach to an
	// execution rather than a test case, so the commit path needs it.
	ExecKey string `json:"execKey,omitempty"`
	// CreatedKey is set only by MarkBugCreated, after a remote create
	// succeeded but the link back failed. Its presence tells a retry the issue
	// already exists, so it links instead of creating a duplicate.
	CreatedKey string `json:"createdKey,omitempty"`
```

Pass the execution key when the payload is built (`internal/testrepo/bugcrud.go:57-60`):

```go
	payload, _ := json.Marshal(bugCreatePayload{
		ProjectKey: d.ProjectKey, IssueType: issueType, Summary: d.Summary, Description: d.Description,
		Priority: d.Priority, Labels: d.Labels, TestKey: testKey, ExecKey: execKey, Fields: d.Fields,
	})
```

Add `MarkBugCreated` at the end of `internal/testrepo/bugcrud.go`:

```go
// MarkBugCreated records the real issue key on a queued bug_create whose
// remote create succeeded but whose link back to the Test failed.
//
// The pending row deliberately survives that failure so the user can retry
// (the spec's partial-failure rule: never delete an issue that already
// exists). A bare retry would call CreateBug again and file a duplicate, so
// the key is written into the payload and the commit path skips the create
// when it is present. Every other field is preserved: the retry still needs
// them to build the link.
func (r *Repository) MarkBugCreated(profileID string, changeID int64, realKey string) error {
	var afterVal string
	err := r.db.QueryRow(
		`SELECT after_val FROM pending_change WHERE id = ? AND profile_id = ?`,
		changeID, profileID).Scan(&afterVal)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("pending change %d not found", changeID)
	}
	if err != nil {
		return fmt.Errorf("read pending change: %w", err)
	}

	var p bugCreatePayload
	if err := json.Unmarshal([]byte(afterVal), &p); err != nil {
		return fmt.Errorf("malformed bug payload: %w", err)
	}
	p.CreatedKey = realKey
	next, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("encode bug payload: %w", err)
	}
	if _, err := r.db.Exec(
		`UPDATE pending_change SET after_val = ? WHERE id = ? AND profile_id = ?`,
		string(next), changeID, profileID); err != nil {
		return fmt.Errorf("record created bug key: %w", err)
	}
	return nil
}
```

Add `"database/sql"` and `"errors"` to the file's imports if they are not already present.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/testrepo/ -run 'TestBugCreatePayloadCarries|TestMarkBugCreated' -v`

Expected: PASS.

- [ ] **Step 5: Run the full testrepo suite**

Run: `go test ./internal/testrepo/`

Expected: PASS. `omitempty` on both new fields keeps existing golden payloads byte-identical.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/testrepo
git add internal/testrepo/bugcrud.go internal/testrepo/bugcrud_test.go
git commit -m "feat(testrepo): carry execKey and createdKey on a queued bug create (-359)"
```

---

### Task 3: Capabilities gain SupportsBugRouting

**Files:**
- Modify: `internal/backend/backend.go:70-71`
- Modify: `app.go:837-846` (`GetCapabilities`)
- Test: `app_capabilities_test.go` (create if absent)

**Interfaces:**
- Consumes: `Manager.ByRole(workspaceID, role string) (Connection, error)` from Task 1.
- Produces: `backend.Capabilities.SupportsBugRouting bool` with JSON tag `supportsBugRouting`, `true` only when the profile has a usable `"bugs"` connection.

- [ ] **Step 1: Add the field**

In `internal/backend/backend.go`, beside the existing bug capabilities:

```go
	SupportsBugCreation         bool `json:"supportsBugCreation"`
	SupportsBugLinks            bool `json:"supportsBugLinks"`
	// SupportsBugRouting reports that this profile files its defects into a
	// SEPARATE backend: a Kiwi workspace with a Jira bug connection
	// (RND_P_4TFINT_05-359). It is a property of the profile's configuration,
	// not of the adapter, so no adapter's Capabilities() sets it; app.go's
	// GetCapabilities merges it in.
	//
	// SupportsBugCreation stays whatever the primary adapter reports. Kiwi
	// still cannot create a bug; something else does it on Kiwi's behalf, and
	// flipping the adapter's own flag would make it lie.
	SupportsBugRouting bool `json:"supportsBugRouting"`
```

- [ ] **Step 2: Write the failing test**

Create `app_capabilities_test.go` in the repo root (package `main`):

```go
package main

import (
	"testing"
)

// TestBugRoutingCapabilityFollowsTheConnection pins the flag's meaning: it
// reports configuration, not adapter ability. A Kiwi profile with no bug
// connection reports both flags false; adding the connection flips ONLY
// SupportsBugRouting, because Kiwi still cannot create a Jira issue itself.
func TestBugRoutingCapabilityFollowsTheConnection(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)

	caps, err := a.GetCapabilities(profileID)
	if err != nil {
		t.Fatalf("capabilities: %v", err)
	}
	if caps.SupportsBugRouting {
		t.Error("SupportsBugRouting = true with no bug connection configured")
	}
	if caps.SupportsBugCreation {
		t.Error("SupportsBugCreation = true, want false: Kiwi cannot create Jira issues")
	}

	if _, err := a.SaveBugConnection(profileID, "https://jira.example.com", "DEF", "Bug", "tok", "", false); err != nil {
		t.Fatalf("save bug connection: %v", err)
	}

	caps, err = a.GetCapabilities(profileID)
	if err != nil {
		t.Fatalf("capabilities after routing: %v", err)
	}
	if !caps.SupportsBugRouting {
		t.Error("SupportsBugRouting = false after configuring a bug connection")
	}
	if caps.SupportsBugCreation {
		t.Error("SupportsBugCreation flipped to true; the adapter's own ability did not change")
	}
}
```

`newTestAppWithKiwiProfile` and `SaveBugConnection` arrive in Task 4. Write this test now, leave it failing to compile, and note in the commit that Task 4 turns it green. If the repo root has an existing helper that builds an `*App` against a temp store, reuse it and rename the call accordingly.

- [ ] **Step 3: Merge the flag in GetCapabilities**

Replace `GetCapabilities` in `app.go`:

```go
// GetCapabilities reports what the profile's backend can do. Most fields come
// straight from the adapter; SupportsBugRouting is merged in here because it
// describes the profile's CONFIGURATION (does it have a bug connection?)
// rather than anything the adapter knows about itself.
func (a *App) GetCapabilities(profileID string) (backend.Capabilities, error) {
	if err := a.requireStore(); err != nil {
		return backend.Capabilities{}, err
	}
	b, err := a.backendFor(profileID)
	if err != nil {
		return backend.Capabilities{}, err
	}
	caps := b.Capabilities()
	if _, err := a.connections.ByRole(profileID, "bugs"); err == nil {
		caps.SupportsBugRouting = true
	}
	return caps, nil
}
```

- [ ] **Step 4: Verify the backend package still builds**

Run: `go build ./... && go test ./internal/backend/...`

Expected: PASS. The new root-package test does not compile yet; that is expected and Task 4 fixes it.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/backend app.go
git add internal/backend/backend.go app.go app_capabilities_test.go
git commit -m "feat(backend): add SupportsBugRouting capability (-359)"
```

---

### Task 4: Bug-connection CRUD on App

**Files:**
- Modify: `app.go` (near the connections block at `app.go:613`), `app.go:590-611` (`DeleteProfile`)
- Test: `app_bugconnection_test.go` (new), `app_capabilities_test.go` (from Task 3)

**Interfaces:**
- Consumes: `Manager.ByRole` from Task 1.
- Produces:
  - `func (a *App) GetBugConnection(profileID string) (connection.Connection, error)` returning a zero `Connection` and nil error when none is configured.
  - `func (a *App) SaveBugConnection(profileID, url, projectKey, issueType, token, caCert string, allowUntrustedTLS bool) (connection.Connection, error)`.
  - `func (a *App) DeleteBugConnection(profileID string) error`.
  - `func (a *App) bugBackendFor(profileID string) (backend.Backend, error)` returning `(nil, nil)` when no bug connection exists.

- [ ] **Step 1: Write the failing tests**

Create `app_bugconnection_test.go` in the repo root (package `main`):

```go
package main

import (
	"errors"
	"testing"

	"xray-test-manager/internal/connection"
)

// TestSaveBugConnectionStoresTheCredentialOutsideTheDatabase is the security
// rule the whole feature inherits: a token reaches the OS credential manager
// and never a column. The bug connection is a SECOND credential, keyed by its
// own connection id, which is the rule connection.go already documents.
func TestSaveBugConnectionStoresTheCredentialOutsideTheDatabase(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)

	c, err := a.SaveBugConnection(profileID, "https://jira.example.com", "DEF", "Bug", "s3cret", "", false)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if c.ID == profileID {
		t.Fatal("bug connection reused the profile id; it must have its own id and its own credential")
	}
	if c.Role != "bugs" {
		t.Errorf("role = %q, want bugs", c.Role)
	}

	tok, err := a.creds.Load(c.ID)
	if err != nil {
		t.Fatalf("load bug credential: %v", err)
	}
	if tok != "s3cret" {
		t.Errorf("stored credential = %q, want the token passed in", tok)
	}

	// The primary connection's credential is untouched.
	if _, err := a.creds.Load(profileID); err != nil {
		t.Errorf("primary credential disturbed: %v", err)
	}
}

// TestSaveBugConnectionIsIdempotent checks a second save edits the existing row
// rather than accumulating bug connections.
func TestSaveBugConnectionIsIdempotent(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)

	first, err := a.SaveBugConnection(profileID, "https://jira.example.com", "DEF", "Bug", "tok", "", false)
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	second, err := a.SaveBugConnection(profileID, "https://jira2.example.com", "OPS", "Defect", "tok2", "", false)
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("second save created a new row (%s then %s)", first.ID, second.ID)
	}
	got, err := a.GetBugConnection(profileID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.URL != "https://jira2.example.com" || got.ProjectKey != "OPS" || got.BugIssueType != "Defect" {
		t.Errorf("got = %+v, want the second save's values", got)
	}
}

// TestGetBugConnectionIsEmptyWhenUnconfigured keeps the frontend simple: an
// unconfigured profile is not an error condition.
func TestGetBugConnectionIsEmptyWhenUnconfigured(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)

	got, err := a.GetBugConnection(profileID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != "" {
		t.Errorf("got = %+v, want a zero Connection", got)
	}
}

// TestDeleteBugConnectionRemovesTheCredentialToo guards against a token
// outliving the configuration that justified storing it.
func TestDeleteBugConnectionRemovesTheCredentialToo(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)

	c, err := a.SaveBugConnection(profileID, "https://jira.example.com", "DEF", "Bug", "tok", "", false)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := a.DeleteBugConnection(profileID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := a.connections.ByRole(profileID, "bugs"); !errors.Is(err, connection.ErrNotFound) {
		t.Errorf("bug connection still present after delete: %v", err)
	}
	if tok, err := a.creds.Load(c.ID); err == nil && tok != "" {
		t.Error("bug credential survived the delete")
	}
}

// TestDeleteProfileRemovesTheBugConnection is the cleanup path. The primary
// connection's credential is keyed by the profile id, so DeleteProfile already
// removed it; the bug connection has its own id and was being left behind.
func TestDeleteProfileRemovesTheBugConnection(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)

	c, err := a.SaveBugConnection(profileID, "https://jira.example.com", "DEF", "Bug", "tok", "", false)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := a.DeleteProfile(profileID); err != nil {
		t.Fatalf("delete profile: %v", err)
	}
	if _, err := a.connections.Get(c.ID); !errors.Is(err, connection.ErrNotFound) {
		t.Errorf("bug connection row survived profile delete: %v", err)
	}
	if tok, err := a.creds.Load(c.ID); err == nil && tok != "" {
		t.Error("bug credential survived profile delete")
	}
}

// TestBugBackendForIsNilWhenUnconfigured pins the routing default. Every
// engine that takes a bug backend must behave exactly as before when this
// returns nil, which is what keeps Xray profiles untouched.
func TestBugBackendForIsNilWhenUnconfigured(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)

	b, err := a.bugBackendFor(profileID)
	if err != nil {
		t.Fatalf("bugBackendFor: %v", err)
	}
	if b != nil {
		t.Errorf("got %T, want nil for a profile with no bug connection", b)
	}
}
```

Add the shared helper to the same file:

```go
// newTestAppWithKiwiProfile builds an App against a temp store with one saved
// Kiwi profile, and returns the profile id.
func newTestAppWithKiwiProfile(t *testing.T) (*App, string) {
	t.Helper()
	a := NewApp()
	a.ctx = context.Background()
	if err := a.startupWithDBPath(filepath.Join(t.TempDir(), "test.db")); err != nil {
		t.Fatalf("startup: %v", err)
	}
	t.Cleanup(func() { _ = a.shutdownStore() })

	p, err := a.CreateProfile("Kiwi", "https://kiwi.example.com", "SNMP", "u:p", "", "", "kiwi")
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	return a, p.ID
}
```

Before writing this helper, read the existing root-package tests and match how they build an `App` against a temp store. If a helper already exists, use it and delete this one. If `CreateProfile`'s signature differs, use the real one; the only requirement is a saved Kiwi profile whose credential is stored.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test . -run 'TestSaveBugConnection|TestGetBugConnection|TestDeleteBugConnection|TestDeleteProfileRemoves|TestBugBackendFor|TestBugRoutingCapability' -v`

Expected: FAIL to compile with `a.SaveBugConnection undefined`, `a.GetBugConnection undefined`, `a.DeleteBugConnection undefined`, `a.bugBackendFor undefined`.

- [ ] **Step 3: Implement the CRUD**

Add to `app.go`, in the connections block that starts at `app.go:613`:

```go
// --- Bug connection (RND_P_4TFINT_05-359) ---
//
// A Kiwi workspace has no Jira-style issue type, so its defects go into a
// separate Jira project. That target is a second connection row with role
// "bugs", configured independently of the profile's own connection and of any
// other profile. The role on the row is the only record that a profile routes
// bugs; no profile column mirrors it.

// bugConnectionRole is the connection role reserved for the bug target.
const bugConnectionRole = "bugs"

// GetBugConnection returns the profile's bug connection, or a zero Connection
// when none is configured. An unconfigured profile is the normal case, not an
// error, so the frontend can render the form either way.
func (a *App) GetBugConnection(profileID string) (c connection.Connection, err error) {
	defer recoverToError("GetBugConnection", &err)
	if err := a.requireStore(); err != nil {
		return connection.Connection{}, err
	}
	got, err := a.connections.ByRole(profileID, bugConnectionRole)
	if errors.Is(err, connection.ErrNotFound) {
		return connection.Connection{}, nil
	}
	if err != nil {
		return connection.Connection{}, err
	}
	return got, nil
}

// SaveBugConnection creates or updates the profile's bug connection and stores
// its token in the OS credential manager under the connection's own id. A
// blank token on an update keeps the stored one, so the form can round-trip
// without re-showing or re-sending the secret.
func (a *App) SaveBugConnection(profileID, url, projectKey, issueType, token, caCert string, allowUntrustedTLS bool) (c connection.Connection, err error) {
	defer recoverToError("SaveBugConnection", &err)
	if err := a.requireStore(); err != nil {
		return connection.Connection{}, err
	}
	if strings.TrimSpace(url) == "" || strings.TrimSpace(projectKey) == "" {
		return connection.Connection{}, errors.New("a bug connection needs a URL and a project key")
	}
	if strings.TrimSpace(issueType) == "" {
		issueType = "Bug"
	}

	existing, err := a.connections.ByRole(profileID, bugConnectionRole)
	id := ""
	switch {
	case err == nil:
		id = existing.ID
	case errors.Is(err, connection.ErrNotFound):
		id = uuid.NewString()
	default:
		return connection.Connection{}, err
	}

	if strings.TrimSpace(token) == "" && id != "" {
		if _, lErr := a.creds.Load(id); lErr != nil {
			return connection.Connection{}, errors.New("a bug connection needs a token")
		}
	}

	// The bug target is always a Jira/Xray backend: it is where the issue is
	// created. bugProjectMode "dedicated" records that the bug project is the
	// connection's own project rather than the tests' project.
	saved, err := a.connections.Put(id, profileID, "Bug tracker", "xray", url, projectKey,
		"", issueType, "dedicated", projectKey, caCert, allowUntrustedTLS,
		bugConnectionRole, time.Now().UTC())
	if err != nil {
		return connection.Connection{}, err
	}
	if strings.TrimSpace(token) != "" {
		if err := a.creds.Save(saved.ID, token); err != nil {
			return connection.Connection{}, fmt.Errorf("store bug credentials: %w", err)
		}
	}
	return saved, nil
}

// DeleteBugConnection removes the bug connection and its credential. The
// profile keeps working; it simply stops offering bug creation.
func (a *App) DeleteBugConnection(profileID string) (err error) {
	defer recoverToError("DeleteBugConnection", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	c, err := a.connections.ByRole(profileID, bugConnectionRole)
	if errors.Is(err, connection.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := a.connections.Delete(c.ID); err != nil {
		return err
	}
	if err := a.creds.Delete(c.ID); err != nil {
		log.Printf("xtm: delete bug credentials for connection %s: %v", c.ID, err)
	}
	return nil
}

// bugBackendFor returns the Backend a profile's bugs are filed into, or
// (nil, nil) when the profile has no bug connection. A nil result is the
// normal case for every Xray profile and for a Kiwi profile that has not been
// configured: the engines treat it as "route bug work to the primary backend,
// exactly as before".
func (a *App) bugBackendFor(profileID string) (backend.Backend, error) {
	c, err := a.connections.ByRole(profileID, bugConnectionRole)
	if errors.Is(err, connection.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	token, err := a.creds.Load(c.ID)
	if err != nil {
		return nil, fmt.Errorf("load bug credentials: %w", err)
	}
	return newBackend(c.Backend, c.URL, token, c.CACert, c.AllowUntrustedTLS), nil
}
```

Add `"github.com/google/uuid"` to `app.go`'s imports if it is not already there.

- [ ] **Step 4: Clean up on profile delete**

In `app.go`'s `DeleteProfile`, after the existing `a.creds.Delete(id)` call:

```go
	// A profile can own more than one connection (a Kiwi profile's bug
	// connection). Each has its OWN credential keyed by its connection id, so
	// deleting the profile's own credential is not enough.
	if conns, cErr := a.connections.List(id); cErr == nil {
		for _, c := range conns {
			if c.ID == id {
				continue // the primary connection's credential was deleted above
			}
			if err := a.creds.Delete(c.ID); err != nil {
				log.Printf("xtm: delete credentials for connection %s: %v", c.ID, err)
			}
			if err := a.connections.Delete(c.ID); err != nil {
				log.Printf("xtm: delete connection %s: %v", c.ID, err)
			}
		}
	} else {
		log.Printf("xtm: list connections for %s: %v", id, cErr)
	}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test . -run 'TestSaveBugConnection|TestGetBugConnection|TestDeleteBugConnection|TestDeleteProfileRemoves|TestBugBackendFor|TestBugRoutingCapability' -v`

Expected: PASS, including `TestBugRoutingCapabilityFollowsTheConnection` from Task 3.

- [ ] **Step 6: Commit**

```bash
gofmt -w app.go
git add app.go app_bugconnection_test.go
git commit -m "feat(app): configure a Jira bug connection on a profile (-359)"
```

---

### Task 5: Commit routes the create and the link to different backends

**Files:**
- Modify: `internal/syncer/engine.go:96-121` (Engine struct and options), `internal/syncer/commit.go:1195-1235` (`commitBugCreates`)
- Test: `internal/syncer/commit_bugrouting_test.go` (new)

**Interfaces:**
- Consumes: `MarkBugCreated` from Task 2.
- Produces: `func WithBugBackend(b backend.Backend) Option`; `func (e *Engine) bugTarget() backend.Backend` returning the bug backend when set and the primary otherwise.

- [ ] **Step 1: Write the failing test**

Create `internal/syncer/commit_bugrouting_test.go`:

```go
package syncer_test

import (
	"context"
	"errors"
	"testing"

	"xray-test-manager/internal/backend"
	"xray-test-manager/internal/syncer"
	"xray-test-manager/internal/testrepo"
)

// recordingBackend counts which backend received which bug call.
type recordingBackend struct {
	backend.Backend // embedded so only the methods under test need defining
	name            string
	created         []string
	linked          [][2]string
	createErr       error
	linkErr         error
}

func (r *recordingBackend) CreateBug(ctx context.Context, projectKey, issueType, summary, description, priority string, labels []string, extraFields map[string]any) (string, error) {
	r.created = append(r.created, projectKey+"/"+summary)
	if r.createErr != nil {
		return "", r.createErr
	}
	return "DEF-42", nil
}

func (r *recordingBackend) CreateBugLink(ctx context.Context, testKey, bugKey string) error {
	r.linked = append(r.linked, [2]string{testKey, bugKey})
	return r.linkErr
}

// TestBugCreateGoesToTheBugBackendAndTheLinkStaysHome is the routing rule. The
// two halves of filing a bug belong to different servers when a bug backend is
// configured: Jira creates the issue, Kiwi carries the hyperlink.
func TestBugCreateGoesToTheBugBackendAndTheLinkStaysHome(t *testing.T) {
	repo, profileID := newSyncerRepoWithProfile(t)
	if _, err := repo.CreateBugForTest(profileID, "T-1", "EXEC-9", testrepo.BugDraft{
		ProjectKey: "DEF", IssueType: "Bug", Summary: "Boom",
	}); err != nil {
		t.Fatalf("queue bug: %v", err)
	}

	primary := &recordingBackend{name: "kiwi"}
	bugs := &recordingBackend{name: "jira"}
	e := syncer.New(primary, repo, syncer.WithBugBackend(bugs))

	res, err := e.CommitChanges(context.Background(), profileID, "SNMP")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("unexpected failures: %+v", res.Failed)
	}
	if len(bugs.created) != 1 {
		t.Errorf("bug backend received %d creates, want 1", len(bugs.created))
	}
	if len(primary.created) != 0 {
		t.Errorf("primary backend received %d creates, want 0", len(primary.created))
	}
	if len(primary.linked) != 1 {
		t.Errorf("primary backend received %d links, want 1", len(primary.linked))
	}
	if len(bugs.linked) != 0 {
		t.Errorf("bug backend received %d links, want 0", len(bugs.linked))
	}
}

// TestNoBugBackendRoutesEverythingToThePrimary is the regression guard for
// every Xray profile: without a bug backend nothing about this path changes.
func TestNoBugBackendRoutesEverythingToThePrimary(t *testing.T) {
	repo, profileID := newSyncerRepoWithProfile(t)
	if _, err := repo.CreateBugForTest(profileID, "T-1", "", testrepo.BugDraft{
		ProjectKey: "DEF", IssueType: "Bug", Summary: "Boom",
	}); err != nil {
		t.Fatalf("queue bug: %v", err)
	}

	primary := &recordingBackend{name: "xray"}
	e := syncer.New(primary, repo)

	if _, err := e.CommitChanges(context.Background(), profileID, "QA"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(primary.created) != 1 || len(primary.linked) != 1 {
		t.Errorf("primary got %d creates and %d links, want 1 and 1", len(primary.created), len(primary.linked))
	}
}

// TestALinkFailureKeepsTheCreatedBug is the partial-failure rule. The Jira
// issue exists; deleting it would be worse than losing the link. The commit
// reports the failure, the pending row survives for a retry, and the real key
// is recorded so the retry links instead of creating a duplicate.
func TestALinkFailureKeepsTheCreatedBug(t *testing.T) {
	repo, profileID := newSyncerRepoWithProfile(t)
	if _, err := repo.CreateBugForTest(profileID, "T-1", "EXEC-9", testrepo.BugDraft{
		ProjectKey: "DEF", IssueType: "Bug", Summary: "Boom",
	}); err != nil {
		t.Fatalf("queue bug: %v", err)
	}

	primary := &recordingBackend{name: "kiwi", linkErr: errors.New("execution not found")}
	bugs := &recordingBackend{name: "jira"}
	e := syncer.New(primary, repo, syncer.WithBugBackend(bugs))

	res, err := e.CommitChanges(context.Background(), profileID, "SNMP")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(res.Failed) != 1 {
		t.Fatalf("got %d failures, want exactly 1 naming the link", len(res.Failed))
	}

	// The pending row survives so the user can retry.
	pending, err := repo.ListPendingChanges(profileID)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	found := false
	for _, p := range pending {
		if p.EntityType == "bug_create" {
			found = true
		}
	}
	if !found {
		t.Fatal("the bug_create pending row was cleared despite the link failing")
	}

	// The retry links the EXISTING issue instead of creating a second one.
	primary.linkErr = nil
	if _, err := e.CommitChanges(context.Background(), profileID, "SNMP"); err != nil {
		t.Fatalf("retry commit: %v", err)
	}
	if len(bugs.created) != 1 {
		t.Errorf("bug backend created %d issues across the retry, want 1", len(bugs.created))
	}
}
```

`newSyncerRepoWithProfile` must build a `*testrepo.Repository` over a temp store with one profile row. Reuse whatever helper the existing `internal/syncer` tests already use for this; if none exists, write one in this file matching their pattern.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/syncer/ -run 'TestBugCreateGoesTo|TestNoBugBackend|TestALinkFailure' -v`

Expected: FAIL to compile with `syncer.WithBugBackend undefined`.

- [ ] **Step 3: Add the option**

In `internal/syncer/engine.go`, add the field to `Engine`:

```go
type Engine struct {
	backend backend.Backend
	repo    *testrepo.Repository
	// bugBackend is the backend bug ISSUES are created in and read from, when
	// it differs from the primary one. A Kiwi workspace has no Jira-style
	// issue type, so its defects live in a separate Jira project
	// (RND_P_4TFINT_05-359). nil means bug work goes to the primary backend,
	// which is every Xray profile and every unconfigured Kiwi profile.
	//
	// Only the ISSUE moves. The LINK between a test and its bug stays on the
	// primary backend, because that is where the test lives.
	bugBackend backend.Backend
	// crossProjectSources is the profile's configured source projects (already
	// scoped to exclude the profile's own project). The container sync searches
	// these for Test Executions that include the profile's tests. Empty means
	// cross-project execution discovery is skipped entirely.
	crossProjectSources []string
}
```

And the option plus accessor beside `WithCrossProjectSources`:

```go
// WithBugBackend routes bug issue reads and writes to a second backend. Pass
// nil (or omit the option) to keep every bug call on the primary backend.
func WithBugBackend(b backend.Backend) Option {
	return func(e *Engine) { e.bugBackend = b }
}

// bugTarget is the backend that owns bug ISSUES: the configured bug backend
// when there is one, the primary backend otherwise. Every bug create and every
// bug read goes through this rather than reading e.backend directly, so the
// routing decision is stated once.
func (e *Engine) bugTarget() backend.Backend {
	if e.bugBackend != nil {
		return e.bugBackend
	}
	return e.backend
}
```

- [ ] **Step 4: Route commitBugCreates**

Replace the body of `commitBugCreates` in `internal/syncer/commit.go`:

```go
// commitBugCreates creates each queued Bug issue, repoints the placeholder key
// to the real one, then links it to its Test. Reported under the test key.
//
// The two halves can land on different servers. The ISSUE is created on
// bugTarget() (a Kiwi workspace files its defects into Jira); the LINK is
// always written on the primary backend, because that is where the Test lives.
//
// A create that succeeds followed by a link that fails keeps the issue: it
// already exists remotely and deleting it would be worse than losing the link.
// The real key is recorded on the pending row so a retry links the existing
// issue instead of filing a duplicate.
func (e *Engine) commitBugCreates(ctx context.Context, profileID string, rows []testrepo.PendingChange, result *CommitResult) {
	for _, c := range rows {
		var p struct {
			ProjectKey  string         `json:"projectKey"`
			IssueType   string         `json:"issueType"`
			Summary     string         `json:"summary"`
			Description string         `json:"description"`
			Priority    string         `json:"priority"`
			Labels      []string       `json:"labels"`
			TestKey     string         `json:"testKey"`
			ExecKey     string         `json:"execKey"`
			CreatedKey  string         `json:"createdKey"`
			Fields      map[string]any `json:"fields"`
		}
		if err := json.Unmarshal([]byte(c.AfterVal), &p); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: c.EntityKey, Error: "malformed bug payload: " + err.Error()})
			continue
		}

		key := c.EntityKey
		if p.CreatedKey != "" {
			// A previous attempt already created this issue and only the link
			// failed. Skip the create so the retry cannot file a duplicate.
			key = p.CreatedKey
		} else {
			realKey, err := e.bugTarget().CreateBug(ctx, p.ProjectKey, p.IssueType, p.Summary, p.Description, p.Priority, p.Labels, p.Fields)
			if err != nil {
				result.Failed = append(result.Failed, FailedCommit{TestKey: p.TestKey, Error: "create bug: " + sanitizeError(err.Error())})
				continue
			}
			if realKey != "" && realKey != c.EntityKey {
				if rErr := e.repo.RenameBug(profileID, c.EntityKey, realKey); rErr != nil {
					_ = rErr // remote create already succeeded; a cache-rename hiccup must not fail the commit
				}
				key = realKey
			}
		}

		linkTarget := p.TestKey
		if e.bugBackend != nil && p.ExecKey != "" {
			// The primary backend carries the link. On Kiwi that link is a
			// hyperlink on the Test Execution the bug was raised from, so the
			// execution key is the correct anchor when there is one.
			linkTarget = p.ExecKey
		}
		if err := e.backend.CreateBugLink(ctx, linkTarget, key); err != nil {
			if mErr := e.repo.MarkBugCreated(profileID, c.ID, key); mErr != nil {
				log.Printf("xtm: record created bug key for retry: %v", mErr)
			}
			result.Failed = append(result.Failed, FailedCommit{
				TestKey: p.TestKey,
				Error:   "created " + key + " but linking it failed (retry to link it): " + sanitizeError(err.Error()),
			})
			continue
		}
		if err := e.repo.CommitPendingChanges(profileID, []int64{c.ID}); err != nil {
			result.Failed = append(result.Failed, FailedCommit{TestKey: key, Error: "Jira created the bug but local cleanup failed: " + err.Error()})
			continue
		}
		result.Succeeded = append(result.Succeeded, key)
	}
}
```

Add `"log"` to `internal/syncer/commit.go`'s imports if it is not already there.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/syncer/ -run 'TestBugCreateGoesTo|TestNoBugBackend|TestALinkFailure' -v`

Expected: PASS.

- [ ] **Step 6: Run the full syncer suite**

Run: `go test ./internal/syncer/`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/syncer
git add internal/syncer/engine.go internal/syncer/commit.go internal/syncer/commit_bugrouting_test.go
git commit -m "feat(syncer): create bugs on a second backend, link them on the primary (-359)"
```

---

### Task 6: Kiwi writes and reads execution hyperlinks

Kiwi's `TestExecution.add_link` and `get_links` were confirmed against a live instance: `get_links` returns `[]` for an execution with no links, and `add_link` rejects a call missing `url` with `[('url', ['This field is required.'])]`.

**Files:**
- Create: `internal/backend/kiwi/bugs.go`, `internal/backend/kiwi/bugs_test.go`
- Modify: `internal/backend/kiwi/adapter.go:942-960` (delete the two stubs now implemented)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `func (a *Adapter) CreateBugLink(ctx context.Context, execKey, bugKey string) error`; `func (a *Adapter) ListBugs(ctx context.Context, testProjectKey string, testKeys []string, issueType string, onProgress func(done, total int)) ([]backend.Bug, []backend.BugLink, error)` returning links plus key-only `Bug` records for the caller to hydrate.

- [ ] **Step 1: Write the failing tests**

Create `internal/backend/kiwi/bugs_test.go`:

```go
package kiwi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newBugKiwi serves an instance with one execution carrying two hyperlinks,
// one of which points at a Jira issue.
func newBugKiwi(t *testing.T, calls *[]string) *Adapter {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
			ID     int               `json:"id"`
		}
		_ = json.Unmarshal(body, &req)
		if calls != nil {
			*calls = append(*calls, req.Method+" "+string(body))
		}
		w.Header().Set("Content-Type", "application/json")

		switch req.Method {
		case "Auth.login":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":"sid"}`, req.ID)
		case "TestExecution.filter":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":[{"id":7,"case":1,"case__summary":"Test 1"}]}`, req.ID)
		case "TestExecution.get_links":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":[`+
				`{"id":1,"name":"DEF-42","url":"https://jira.example.com/browse/DEF-42"},`+
				`{"id":2,"name":"notes","url":"https://wiki.example.com/page"}]}`, req.ID)
		case "TestExecution.add_link":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"id":3}}`, req.ID)
		default:
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":[]}`, req.ID)
		}
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "u:p")
}

// TestCreateBugLinkSendsAUrl pins the field shape a live instance demanded:
// add_link answered [('url', ['This field is required.'])] without it.
func TestCreateBugLinkSendsAUrl(t *testing.T) {
	var calls []string
	a := newBugKiwi(t, &calls)
	a.SetBugBrowseBase("https://jira.example.com/browse/")

	if err := a.CreateBugLink(context.Background(), "7", "DEF-42"); err != nil {
		t.Fatalf("create bug link: %v", err)
	}

	var addCall string
	for _, c := range calls {
		if strings.HasPrefix(c, "TestExecution.add_link ") {
			addCall = c
		}
	}
	if addCall == "" {
		t.Fatal("add_link was never called")
	}
	if !strings.Contains(addCall, `"url"`) {
		t.Errorf("add_link payload has no url field: %s", addCall)
	}
	if !strings.Contains(addCall, "https://jira.example.com/browse/DEF-42") {
		t.Errorf("add_link payload does not carry the bug's browse URL: %s", addCall)
	}
	if !strings.Contains(addCall, "DEF-42") {
		t.Errorf("add_link payload does not name the bug: %s", addCall)
	}
}

// TestCreateBugLinkNeedsAnExecution names the caller error rather than sending
// a request that cannot work: Kiwi hyperlinks attach to an execution.
func TestCreateBugLinkNeedsAnExecution(t *testing.T) {
	a := newBugKiwi(t, nil)
	if err := a.CreateBugLink(context.Background(), "", "DEF-42"); err == nil {
		t.Fatal("want an error when no execution is given")
	}
}

// TestListBugsReadsExecutionHyperlinks is the read half. Kiwi holds only the
// link; the issue's summary and status live in Jira, so the Bug records come
// back carrying just a key for the caller to hydrate.
func TestListBugsReadsExecutionHyperlinks(t *testing.T) {
	a := newBugKiwi(t, nil)
	a.SetBugBrowseBase("https://jira.example.com/browse/")

	bugs, links, err := a.ListBugs(context.Background(), "SNMP", []string{"1"}, "Bug", nil)
	if err != nil {
		t.Fatalf("list bugs: %v", err)
	}
	if len(bugs) != 1 || bugs[0].Key != "DEF-42" {
		t.Fatalf("got bugs %+v, want exactly the Jira link DEF-42 (the wiki link is not a bug)", bugs)
	}
	if len(links) != 1 || links[0].BugKey != "DEF-42" || links[0].TestKey != "1" {
		t.Fatalf("got links %+v, want test 1 linked to DEF-42", links)
	}
}

// TestListBugsWithoutABrowseBaseReturnsNothing keeps an unconfigured profile
// quiet: with no Jira base URL there is no way to tell a bug link from any
// other hyperlink, and guessing would fill the Bugs view with wiki pages.
func TestListBugsWithoutABrowseBaseReturnsNothing(t *testing.T) {
	a := newBugKiwi(t, nil)

	bugs, links, err := a.ListBugs(context.Background(), "SNMP", []string{"1"}, "Bug", nil)
	if err != nil {
		t.Fatalf("list bugs: %v", err)
	}
	if len(bugs) != 0 || len(links) != 0 {
		t.Fatalf("got %d bugs and %d links, want none without a browse base", len(bugs), len(links))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/backend/kiwi/ -run 'TestCreateBugLink|TestListBugs' -v`

Expected: FAIL. `a.SetBugBrowseBase undefined`, and `CreateBugLink` returns `ErrUnsupported`.

- [ ] **Step 3: Implement the two RPCs**

Create `internal/backend/kiwi/bugs.go`:

```go
package kiwi

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"xray-test-manager/internal/backend"
)

// Kiwi has no Jira-style issue type. What it has is a hyperlink on a Test
// Execution, so a defect filed elsewhere is represented here as a link from an
// execution to that issue's browse URL.
//
// Both RPCs were confirmed against a live instance before this was written:
// TestExecution.get_links returns [] for an execution with no links, and
// TestExecution.add_link rejects a call missing "url" with
// [('url', ['This field is required.'])].

// SetBugBrowseBase records the Jira browse prefix (for example
// "https://jira.example.com/browse/") for the project this workspace files its
// defects into.
//
// It is what makes a hyperlink recognizable as a bug. Kiwi stores links as
// plain URLs with no type, so without the prefix there is no way to tell a
// defect from a wiki page, and ListBugs reports nothing rather than guessing.
func (a *Adapter) SetBugBrowseBase(base string) {
	a.bugBrowseBase = strings.TrimSpace(base)
}

// bugKeyFromURL returns the issue key a hyperlink points at, or "" when the
// link is not a bug link for this workspace's configured tracker.
func (a *Adapter) bugKeyFromURL(rawURL string) string {
	if a.bugBrowseBase == "" {
		return ""
	}
	if !strings.HasPrefix(rawURL, a.bugBrowseBase) {
		return ""
	}
	key := strings.Trim(strings.TrimPrefix(rawURL, a.bugBrowseBase), "/")
	if key == "" || strings.Contains(key, "/") {
		return ""
	}
	return key
}

// CreateBugLink hyperlinks an already-created bug onto a Test Execution.
//
// The first argument is the EXECUTION to attach to, not a test case: that is
// what Kiwi's link model anchors to, and the syncer passes the execution key
// when a bug backend is configured.
func (a *Adapter) CreateBugLink(ctx context.Context, execKey, bugKey string) error {
	if strings.TrimSpace(execKey) == "" {
		return errors.New("kiwi: a bug link needs the Test Execution it was raised from")
	}
	if strings.TrimSpace(bugKey) == "" {
		return errors.New("kiwi: a bug link needs an issue key")
	}
	if a.bugBrowseBase == "" {
		return errors.New("kiwi: no bug tracker URL is configured for this profile")
	}

	execID, err := parseKiwiID(execKey)
	if err != nil {
		return fmt.Errorf("kiwi: %w", err)
	}
	var out any
	return a.call(ctx, "TestExecution.add_link", []any{map[string]any{
		"execution_id": execID,
		"name":         bugKey,
		"url":          a.bugBrowseBase + bugKey,
		"is_defect":    true,
	}}, &out)
}

// ListBugs harvests the bug links off the executions covering the given tests.
//
// Kiwi holds only the link, so each returned Bug carries just its key: the
// summary, status and priority live in the tracker and are filled in by the
// caller (the syncer hydrates them through the bug backend). Returning
// key-only records keeps this adapter free of any Jira knowledge.
func (a *Adapter) ListBugs(ctx context.Context, testProjectKey string, testKeys []string, issueType string, onProgress func(done, total int)) ([]backend.Bug, []backend.BugLink, error) {
	if a.bugBrowseBase == "" || len(testKeys) == 0 {
		return nil, nil, nil
	}

	execs, err := a.executionsForCases(ctx, testKeys)
	if err != nil {
		return nil, nil, err
	}

	seen := map[string]struct{}{}
	bugs := []backend.Bug{}
	links := []backend.BugLink{}
	for i, ex := range execs {
		if err := ctx.Err(); err != nil {
			return bugs, links, err
		}
		var raw []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
			URL  string `json:"url"`
		}
		if err := a.call(ctx, "TestExecution.get_links", []any{map[string]any{
			"execution_id": ex.ID,
		}}, &raw); err != nil {
			return bugs, links, err
		}
		for _, l := range raw {
			key := a.bugKeyFromURL(l.URL)
			if key == "" {
				continue
			}
			if _, dup := seen[key]; !dup {
				seen[key] = struct{}{}
				bugs = append(bugs, backend.Bug{Key: key, IssueType: issueType})
			}
			links = append(links, backend.BugLink{
				TestKey: ex.CaseKey,
				BugKey:  key,
				LinkID:  fmt.Sprint(l.ID),
			})
		}
		if onProgress != nil {
			onProgress(i+1, len(execs))
		}
	}
	return bugs, links, nil
}
```

Add the field to the `Adapter` struct in `internal/backend/kiwi/adapter.go`:

```go
	// bugBrowseBase is the browse-URL prefix of the tracker this workspace
	// files its defects into, set by SetBugBrowseBase. Empty means bug links
	// are not recognizable, so the bug read reports nothing (see bugs.go).
	bugBrowseBase string
```

Delete the now-superseded stubs at `internal/backend/kiwi/adapter.go:942-944` (`ListBugs`) and `:958-960` (`CreateBugLink`).

Two helpers this file assumes: `a.call(ctx, method string, params []any, out any) error` is the adapter's existing JSON-RPC helper, and `parseKiwiID(string) (int, error)` is the existing numeric-id parser used by `ListTestsInFolder`. Read `internal/backend/kiwi/adapter.go` and use the real names; if `executionsForCases(ctx, caseKeys []string) ([]struct{ID int; CaseKey string}, error)` does not exist, add it in `bugs.go` as a `TestExecution.filter` call with `{"case__in": ids}`, returning each row's `id` and its `case` as a string.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/backend/kiwi/ -run 'TestCreateBugLink|TestListBugs' -v`

Expected: PASS.

- [ ] **Step 5: Run the full kiwi suite**

Run: `go test ./internal/backend/kiwi/`

Expected: PASS. `TestCapabilitiesBaseValues` still expects `SupportsBugCreation: false`, which is unchanged by design.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/backend/kiwi
git add internal/backend/kiwi/bugs.go internal/backend/kiwi/bugs_test.go internal/backend/kiwi/adapter.go
git commit -m "feat(kiwi): read and write bug hyperlinks on test executions (-359)"
```

---

### Task 7: Fetch specific bugs by key

Kiwi's `ListBugs` returns keys with no detail. Hydrating them needs a fetch-these-keys read that no adapter has today, and that only Jira can answer cheaply.

**Files:**
- Modify: `internal/backend/backend.go` (after `TestPreconditionReader` at `:247-249`)
- Modify: `internal/backend/xray/adapter.go`, `internal/jira/bugs.go`
- Test: `internal/jira/bugs_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `backend.BugKeyReader` with `ListBugsByKeys(ctx context.Context, keys []string) ([]Bug, error)`; implemented by `*xray.Adapter` and `*jira.Client`.

- [ ] **Step 1: Write the failing test**

Append to `internal/jira/bugs_test.go`:

```go
// TestListBugsByKeysChunksTheJQL guards the URL-length limit. A JQL
// "key in (...)" with a few thousand keys exceeds what Jira accepts on a GET,
// so the fetch is chunked; the caller must still see one flat result.
func TestListBugsByKeysChunksTheJQL(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query().Get("jql"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"startAt":0,"maxResults":50,"total":1,"issues":[
			{"key":"DEF-1","fields":{"project":{"key":"DEF"},"issuetype":{"name":"Bug"},
			 "summary":"Boom","status":{"name":"Open"},"priority":{"name":"High"},
			 "updated":"2026-09-01T10:00:00.000+0000"}}]}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	keys := make([]string, 0, 120)
	for i := 0; i < 120; i++ {
		keys = append(keys, fmt.Sprintf("DEF-%d", i+1))
	}

	bugs, err := c.ListBugsByKeys(context.Background(), keys)
	if err != nil {
		t.Fatalf("list by keys: %v", err)
	}
	if len(queries) < 2 {
		t.Errorf("issued %d searches for 120 keys, want the request chunked", len(queries))
	}
	if len(bugs) == 0 {
		t.Fatal("no bugs returned")
	}
	for _, q := range queries {
		if !strings.Contains(q, "key in (") {
			t.Errorf("query is not a key lookup: %q", q)
		}
	}
}

// TestListBugsByKeysWithNoKeysMakesNoRequest keeps an unconfigured or empty
// profile from issuing a meaningless "key in ()" search.
func TestListBugsByKeysWithNoKeysMakesNoRequest(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	bugs, err := NewClient(srv.URL, "tok").ListBugsByKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("list by keys: %v", err)
	}
	if len(bugs) != 0 {
		t.Errorf("got %d bugs, want none", len(bugs))
	}
	if called {
		t.Error("an HTTP request was made for an empty key list")
	}
}
```

Match the import block and client constructor to the neighbouring tests in that file.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/jira/ -run TestListBugsByKeys -v`

Expected: FAIL to compile with `c.ListBugsByKeys undefined`.

- [ ] **Step 3: Add the optional interface**

In `internal/backend/backend.go`, after `TestPreconditionReader`:

```go
// BugKeyReader is an optional capability: fetching specific bug issues by key.
//
// It is deliberately kept off Backend. Only a Jira-style tracker can answer it
// cheaply (one JQL "key in (...)" search), and no other adapter has a better
// answer than the project-wide ListProjectBugs it already provides.
//
// The syncer needs it when a workspace's bugs live in a DIFFERENT backend from
// its tests (RND_P_4TFINT_05-359): the primary backend reports which keys are
// linked, and this fills in what those keys actually are. Callers type-assert
// and fall back to the project-wide read when the backend does not implement
// it.
type BugKeyReader interface {
	ListBugsByKeys(ctx context.Context, keys []string) ([]Bug, error)
}
```

- [ ] **Step 4: Implement it on the Jira client and the Xray adapter**

Add to `internal/jira/bugs.go`:

```go
// bugKeyChunk caps how many keys go into one JQL "key in (...)" clause. Jira
// DC accepts the search on a GET, so the whole query rides in the URL and a
// long list would exceed the server's URL limit.
const bugKeyChunk = 50

// ListBugsByKeys fetches exactly the issues named, in chunks.
//
// This is the read a workspace needs when its tests and its bugs live in
// different systems: the test side reports which keys are linked, and this
// turns those keys into full records without pulling the whole bug project.
func (c *Client) ListBugsByKeys(ctx context.Context, keys []string) ([]Bug, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	out := []Bug{}
	for start := 0; start < len(keys); start += bugKeyChunk {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		end := start + bugKeyChunk
		if end > len(keys) {
			end = len(keys)
		}
		quoted := make([]string, 0, end-start)
		for _, k := range keys[start:end] {
			quoted = append(quoted, `"`+strings.ReplaceAll(k, `"`, `\"`)+`"`)
		}
		jql := "key in (" + strings.Join(quoted, ",") + ") ORDER BY key ASC"

		chunk, err := c.searchBugs(ctx, jql)
		if err != nil {
			return out, err
		}
		out = append(out, chunk...)
	}
	return out, nil
}
```

`searchBugs(ctx, jql)` is the paged search loop `ListProjectBugs` already runs (`internal/jira/bugs.go:924-...`). Extract that loop into `searchBugs` and have `ListProjectBugs` call it, so both reads share one implementation rather than duplicating the paging.

Add to `internal/backend/xray/adapter.go`:

```go
// ListBugsByKeys implements backend.BugKeyReader by delegating to the Jira
// client's key lookup.
func (a *Adapter) ListBugsByKeys(ctx context.Context, keys []string) ([]backend.Bug, error) {
	bugs, err := a.c.ListBugsByKeys(ctx, keys)
	if err != nil {
		return nil, err
	}
	return toBackendBugs(bugs), nil
}
```

Use whatever the adapter's existing client field and `jira.Bug` to `backend.Bug` converter are actually named. Read how the neighbouring `ListProjectBugs` does it and mirror it exactly.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/jira/ -run TestListBugsByKeys -v && go test ./internal/jira/ ./internal/backend/...`

Expected: PASS, including the existing `ListProjectBugs` tests, which now run through the extracted `searchBugs`.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/jira internal/backend
git add internal/backend/backend.go internal/backend/xray/adapter.go internal/jira/bugs.go internal/jira/bugs_test.go
git commit -m "feat(jira): add BugKeyReader for fetching specific bugs by key (-359)"
```

---

### Task 8: Sync hydrates linked bugs from the bug backend

**Files:**
- Modify: `internal/syncer/engine.go:990-1058` (`syncBugs`)
- Test: `internal/syncer/syncbugs_routing_test.go` (new)

**Interfaces:**
- Consumes: `bugTarget()` and `WithBugBackend` from Task 5; `backend.BugKeyReader` from Task 7.
- Produces: no new exported API. `syncBugs` skips the project-wide pass and hydrates by key when a bug backend is configured.

- [ ] **Step 1: Write the failing test**

Create `internal/syncer/syncbugs_routing_test.go`:

```go
package syncer_test

import (
	"context"
	"testing"

	"xray-test-manager/internal/backend"
	"xray-test-manager/internal/syncer"
)

// hydratingBackend records which reads it received and answers key lookups.
type hydratingBackend struct {
	backend.Backend
	projectReads int
	keyReads     [][]string
}

func (h *hydratingBackend) ListProjectBugs(ctx context.Context, projKey, issueType string) ([]backend.Bug, error) {
	h.projectReads++
	return []backend.Bug{{Key: "DEF-999", Summary: "Unrelated"}}, nil
}

func (h *hydratingBackend) ListBugsByKeys(ctx context.Context, keys []string) ([]backend.Bug, error) {
	h.keyReads = append(h.keyReads, keys)
	out := make([]backend.Bug, 0, len(keys))
	for _, k := range keys {
		out = append(out, backend.Bug{Key: k, Summary: "Filed from Kiwi", Status: "Open"})
	}
	return out, nil
}

// linkOnlyBackend stands in for Kiwi: it reports which bugs are linked but
// knows nothing about them.
type linkOnlyBackend struct {
	backend.Backend
}

func (l *linkOnlyBackend) ListBugs(ctx context.Context, testProjectKey string, testKeys []string, issueType string, onProgress func(done, total int)) ([]backend.Bug, []backend.BugLink, error) {
	return []backend.Bug{{Key: "DEF-42"}},
		[]backend.BugLink{{TestKey: "1", BugKey: "DEF-42", LinkID: "5"}}, nil
}

func (l *linkOnlyBackend) ListProjectBugs(ctx context.Context, projKey, issueType string) ([]backend.Bug, error) {
	return nil, nil
}

// TestSyncHydratesOnlyLinkedBugs is the read rule the design settled on: the
// Bugs view shows what this workspace's executions actually link to, not every
// issue in the Jira project. The project-wide pass is skipped entirely.
func TestSyncHydratesOnlyLinkedBugs(t *testing.T) {
	repo, profileID := newSyncerRepoWithProfile(t)
	seedTest(t, repo, profileID, "1")

	primary := &linkOnlyBackend{}
	bugs := &hydratingBackend{}
	e := syncer.New(primary, repo, syncer.WithBugBackend(bugs))

	if err := e.SyncBugsOnly(context.Background(), profileID, "SNMP", nil); err != nil {
		t.Fatalf("sync bugs: %v", err)
	}

	if bugs.projectReads != 0 {
		t.Errorf("project-wide bug search ran %d times, want 0 with a bug backend configured", bugs.projectReads)
	}
	if len(bugs.keyReads) != 1 {
		t.Fatalf("key hydration ran %d times, want 1", len(bugs.keyReads))
	}
	if len(bugs.keyReads[0]) != 1 || bugs.keyReads[0][0] != "DEF-42" {
		t.Errorf("hydrated %v, want exactly the linked key DEF-42", bugs.keyReads[0])
	}

	stored, err := repo.ListBugsWithTests(profileID)
	if err != nil {
		t.Fatalf("read stored bugs: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("stored %d bugs, want 1", len(stored))
	}
	if stored[0].Summary != "Filed from Kiwi" {
		t.Errorf("bug was stored key-only (summary %q); hydration did not reach the cache", stored[0].Summary)
	}
}
```

`SyncBugsOnly` is the existing exported entry point at `internal/syncer/engine.go:273` that runs the bug stage on its own. Read that function and use its real name and signature. `seedTest` inserts one test row for the profile; reuse the existing helper the syncer tests already use for that, and if there is none, write one that calls the same repo method the pull path uses.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/syncer/ -run TestSyncHydratesOnlyLinkedBugs -v`

Expected: FAIL. `project-wide bug search ran 1 times, want 0` and the summary is empty, because `syncBugs` still routes both passes to the primary backend and never hydrates.

- [ ] **Step 3: Route the bug reads**

In `internal/syncer/engine.go`, replace the project-wide pass and add hydration. The link harvest (pass 2) stays on `e.backend`, because links come from the tests' own system:

```go
	// Pass 1: project-wide bug search (best-effort). Labelled so the sync bar
	// shows this phase is running before the (longer) per-test harvest.
	//
	// Skipped entirely when a bug backend is configured. That backend holds a
	// Jira project this workspace files INTO, so a project-wide read would
	// pull every unrelated defect in it. The linked keys are the whole answer
	// there (RND_P_4TFINT_05-359).
	var projectBugs []backend.Bug
	if e.bugBackend == nil {
		emitStage(onProgress, "Syncing bugs (project search)")
		var projectBugErr error
		projectBugs, projectBugErr = e.backend.ListProjectBugs(ctx, bugProject, issueType)
		if projectBugErr != nil {
			log.Printf("xtm: project-wide bug search failed (continuing with link harvest only): %v", projectBugErr)
			projectBugs = nil
		}
	}
```

Then, after the merge loop builds `merged` and before `repoBugs` is assembled, add:

```go
	// When bugs live in another backend the harvest returns keys with no
	// detail: the primary backend holds only the link. Fill them in from the
	// backend that owns the issues, fetching exactly the linked keys.
	if e.bugBackend != nil && len(merged) > 0 {
		reader, ok := e.bugBackend.(backend.BugKeyReader)
		if !ok {
			return fmt.Errorf("bug backend %s cannot fetch issues by key", e.bugBackend.Capabilities().Name)
		}
		keys := make([]string, 0, len(merged))
		for k := range merged {
			keys = append(keys, k)
		}
		sort.Strings(keys) // deterministic request order, and a stable test
		emitStage(onProgress, "Syncing bugs (fetching linked issues)")
		hydrated, err := reader.ListBugsByKeys(ctx, keys)
		if err != nil {
			return fmt.Errorf("fetch linked bugs: %w", err)
		}
		for _, b := range hydrated {
			merged[b.Key] = b
		}
	}
```

Add `"sort"` to the file's imports if it is not already there.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/syncer/ -run TestSyncHydratesOnlyLinkedBugs -v`

Expected: PASS.

- [ ] **Step 5: Run the full syncer suite**

Run: `go test ./internal/syncer/`

Expected: PASS. With no bug backend, `e.bugBackend == nil` keeps both passes exactly as they were.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/syncer
git add internal/syncer/engine.go internal/syncer/syncbugs_routing_test.go
git commit -m "feat(syncer): hydrate linked bugs from the bug backend (-359)"
```

---

### Task 9: Wire the bug backend into every engine

**Files:**
- Modify: `app.go:931`, `app.go:1133`, `app.go:1960`, `app.go:1995` (the four `syncer.New` sites)
- Test: `app_bugconnection_test.go`

**Interfaces:**
- Consumes: `bugBackendFor` from Task 4, `WithBugBackend` from Task 5, `SetBugBrowseBase` from Task 6.
- Produces: no new exported API.

- [ ] **Step 1: Write the failing test**

Append to `app_bugconnection_test.go`:

```go
// TestBugBackendForCarriesTheConnectionSettings checks the backend that comes
// back is bound to the BUG connection, not the profile's own. A profile whose
// tests live in Kiwi must not end up filing its bugs into Kiwi.
func TestBugBackendForCarriesTheConnectionSettings(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)

	if _, err := a.SaveBugConnection(profileID, "https://jira.example.com", "DEF", "Bug", "tok", "", false); err != nil {
		t.Fatalf("save bug connection: %v", err)
	}
	b, err := a.bugBackendFor(profileID)
	if err != nil {
		t.Fatalf("bugBackendFor: %v", err)
	}
	if b == nil {
		t.Fatal("no bug backend returned after configuring one")
	}
	if name := b.Capabilities().Name; name != "xray" {
		t.Errorf("bug backend is %q, want xray: bugs are filed into Jira", name)
	}
}

// TestPrimaryBackendLearnsTheBugBrowseBase pins the wiring that makes Kiwi's
// link read work: without the tracker's browse URL, Kiwi cannot tell a bug
// hyperlink from any other link and reports nothing.
func TestPrimaryBackendLearnsTheBugBrowseBase(t *testing.T) {
	a, profileID := newTestAppWithKiwiProfile(t)

	if _, err := a.SaveBugConnection(profileID, "https://jira.example.com", "DEF", "Bug", "tok", "", false); err != nil {
		t.Fatalf("save bug connection: %v", err)
	}
	base, err := a.bugBrowseBaseFor(profileID)
	if err != nil {
		t.Fatalf("bugBrowseBaseFor: %v", err)
	}
	if base != "https://jira.example.com/browse/" {
		t.Errorf("browse base = %q, want the connection URL plus /browse/", base)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test . -run 'TestBugBackendForCarries|TestPrimaryBackendLearns' -v`

Expected: FAIL to compile with `a.bugBrowseBaseFor undefined`.

- [ ] **Step 3: Add the browse-base helper**

Add to `app.go`, beside `bugBackendFor`:

```go
// bugBrowseBaseFor returns the browse-URL prefix of the profile's bug tracker,
// or "" when no bug connection is configured.
//
// The primary backend needs it to recognize its own bug links: Kiwi stores
// hyperlinks as plain untyped URLs, so the prefix is the only thing separating
// a defect from a wiki page.
func (a *App) bugBrowseBaseFor(profileID string) (string, error) {
	c, err := a.connections.ByRole(profileID, bugConnectionRole)
	if errors.Is(err, connection.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimRight(c.URL, "/") + "/browse/", nil
}
```

- [ ] **Step 4: Add a single wiring helper and use it at all four sites**

Add to `app.go`:

```go
// bugRoutingOptions returns the syncer options that route a profile's bug work
// to its bug connection, and tells the primary backend how to recognize bug
// links. It returns no options when the profile has no bug connection, which
// is what keeps every Xray profile's behaviour identical.
//
// A configuration error here is logged rather than returned: a broken bug
// connection must not stop a sync or a commit of everything else.
func (a *App) bugRoutingOptions(profileID string, primary backend.Backend) []syncer.Option {
	bugB, err := a.bugBackendFor(profileID)
	if err != nil {
		log.Printf("xtm: bug connection for %s unusable, bug work stays on the primary backend: %v", profileID, err)
		return nil
	}
	if bugB == nil {
		return nil
	}
	base, err := a.bugBrowseBaseFor(profileID)
	if err != nil {
		log.Printf("xtm: bug tracker URL for %s: %v", profileID, err)
		return nil
	}
	if setter, ok := primary.(interface{ SetBugBrowseBase(string) }); ok {
		setter.SetBugBrowseBase(base)
	}
	return []syncer.Option{syncer.WithBugBackend(bugB)}
}
```

Rewrite the first sync site (`app.go:931`):

```go
	primary := newBackend(p.Backend, p.JiraURL, token, p.CACert, p.AllowUntrustedTLS)
	opts := append([]syncer.Option{
		syncer.WithCrossProjectSources(scopeCrossProjectSources(p.CrossProjectSources, p.ProjectKey)),
	}, a.bugRoutingOptions(profileID, primary)...)
	engine := syncer.New(primary, a.repo, opts...)
```

Apply the identical change at `app.go:1133`.

Rewrite the commit site (`app.go:1960`):

```go
	b := newBackend(p.Backend, p.JiraURL, token, p.CACert, p.AllowUntrustedTLS)
	b.SetRequirementLinkType(s.RequirementLinkType)
	engine := syncer.New(b, a.repo, a.bugRoutingOptions(profileID, b)...)
	return engine.CommitChanges(a.ctx, profileID, p.ProjectKey)
```

Apply the identical change at `app.go:1995`, keeping its `CommitChangesForIDs` call.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test . -run 'TestBugBackendFor|TestPrimaryBackendLearns' -v`

Expected: PASS.

- [ ] **Step 6: Build and run everything**

Run: `go build ./... && go test ./...`

Expected: PASS across all packages.

- [ ] **Step 7: Commit**

```bash
gofmt -w app.go
git add app.go app_bugconnection_test.go
git commit -m "feat(app): route bug work through the profile's bug connection (-359)"
```

---

### Task 10: Configure the bug connection in the profile form

**Files:**
- Modify: `frontend/src/components/ProfileForm.tsx`, `frontend/src/api.ts`
- Test: `frontend/src/components/ProfileForm.test.tsx`

**Interfaces:**
- Consumes: `GetBugConnection`, `SaveBugConnection`, `DeleteBugConnection` from Task 4, re-exported through `frontend/src/api.ts`.
- Produces: no new module exports.

- [ ] **Step 1: Regenerate the Wails bindings**

Run: `wails build -s`

Then re-export the three new methods from `frontend/src/api.ts` beside the existing profile methods. `frontend/wailsjs/` is generated and must never be hand-edited; if the build cannot run in this environment, run `wails dev` once instead and stop it after the bindings are written.

- [ ] **Step 2: Write the failing test**

Append to `frontend/src/components/ProfileForm.test.tsx`:

```tsx
// The bug-connection section only makes sense for a backend that cannot file
// its own defects. An Xray profile already files into its own Jira.
it("offers a bug tracker only for a Kiwi profile", async () => {
  const { rerender } = render(<ProfileForm {...baseProps} backend="xray" />);
  expect(screen.queryByRole("group", { name: /bug tracker/i })).toBeNull();

  rerender(<ProfileForm {...baseProps} backend="kiwi" />);
  expect(
    await screen.findByRole("group", { name: /bug tracker/i }),
  ).toBeInTheDocument();
});

// A saved connection must come back into the form, minus the token: the
// credential lives in the OS credential manager and is never read back.
it("round-trips a saved bug connection without its token", async () => {
  vi.mocked(GetBugConnection).mockResolvedValue({
    id: "bug1",
    url: "https://jira.example.com",
    projectKey: "DEF",
    bugIssueType: "Bug",
  } as never);

  render(<ProfileForm {...baseProps} backend="kiwi" />);

  expect(await screen.findByLabelText(/bug tracker url/i)).toHaveValue(
    "https://jira.example.com",
  );
  expect(screen.getByLabelText(/bug project key/i)).toHaveValue("DEF");
  expect(screen.getByLabelText(/bug tracker token/i)).toHaveValue("");
});

// Leaving the token blank on an edit must not wipe the stored credential, so
// the form sends "" and the backend keeps what it has.
it("sends a blank token when the user does not retype it", async () => {
  vi.mocked(GetBugConnection).mockResolvedValue({
    id: "bug1",
    url: "https://jira.example.com",
    projectKey: "DEF",
    bugIssueType: "Bug",
  } as never);
  const user = userEvent.setup();

  render(<ProfileForm {...baseProps} backend="kiwi" />);
  await user.click(await screen.findByRole("button", { name: /save/i }));

  expect(SaveBugConnection).toHaveBeenCalledWith(
    baseProps.profileId,
    "https://jira.example.com",
    "DEF",
    "Bug",
    "",
    "",
    false,
  );
});
```

Match `baseProps`, the render helper, and the mocking style to the tests already in that file. Add `GetBugConnection` and `SaveBugConnection` to the file's existing `vi.mock("../api", ...)` block.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd frontend; npx vitest run src/components/ProfileForm.test.tsx`

Expected: FAIL. No element matches the "bug tracker" group.

- [ ] **Step 4: Add the section**

In `frontend/src/components/ProfileForm.tsx`, render this only when the selected backend is `kiwi`:

```tsx
{backend === "kiwi" && (
  <fieldset className="profile-bug-tracker">
    <legend>Bug tracker</legend>
    <p className="field-hint">
      Kiwi has no issue type of its own, so defects raised here are filed
      into a Jira project. Leave this empty to keep bug reporting off.
    </p>

    <label className="rename-field">
      <span>Bug tracker URL</span>
      <input
        className="detail-input"
        placeholder="https://jira.example.com"
        value={bugURL}
        onChange={(e) => setBugURL(e.target.value)}
      />
    </label>

    <label className="rename-field">
      <span>Bug project key</span>
      <input
        className="detail-input"
        placeholder="DEF"
        value={bugProjectKey}
        onChange={(e) => setBugProjectKey(e.target.value)}
      />
    </label>

    <label className="rename-field">
      <span>Bug issue type</span>
      <input
        className="detail-input"
        placeholder="Bug"
        value={bugIssueType}
        onChange={(e) => setBugIssueType(e.target.value)}
      />
    </label>

    <label className="rename-field">
      <span>Bug tracker token</span>
      <input
        className="detail-input"
        type="password"
        autoComplete="new-password"
        placeholder={bugConnectionId ? "Unchanged" : "Personal access token"}
        value={bugToken}
        onChange={(e) => setBugToken(e.target.value)}
      />
    </label>
    <p className="field-hint">
      The token is stored in Windows Credential Manager, never in the
      database. Leave it blank to keep the one already saved.
    </p>
  </fieldset>
)}
```

Load the existing connection on mount for a saved profile, and save it alongside the profile. Follow the file's existing state and submit patterns rather than inventing new ones. On save: call `SaveBugConnection` when the URL and project key are both non-empty, and `DeleteBugConnection` when the URL has been cleared.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd frontend; npx vitest run src/components/ProfileForm.test.tsx`

Expected: PASS.

- [ ] **Step 6: Typecheck and run the whole frontend suite**

Run: `cd frontend; npx tsc --noEmit && npx vitest run`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/ProfileForm.tsx frontend/src/components/ProfileForm.test.tsx frontend/src/api.ts frontend/wailsjs
git commit -m "feat(ui): configure a Jira bug tracker on a Kiwi profile (-359)"
```

---

### Task 11: Offer Create Bug when routing is configured

**Files:**
- Modify: whichever component gates the Create Bug action on `SupportsBugCreation`
- Test: that component's `.test.tsx`

**Interfaces:**
- Consumes: `Capabilities.supportsBugRouting` from Task 3.
- Produces: no new module exports.

- [ ] **Step 1: Find every gate**

Run: `cd frontend; grep -rn "supportsBugCreation" src/`

Every hit is a place that must also accept routing.

- [ ] **Step 2: Write the failing test**

In the test file beside the component that gates the button, add:

```tsx
// A Kiwi profile cannot create a Jira issue itself, so supportsBugCreation
// stays false. With a bug connection configured something else does it on
// Kiwi's behalf, and the action has to be offered.
it("offers Create Bug when the profile routes bugs elsewhere", async () => {
  renderWithCaps({ supportsBugCreation: false, supportsBugRouting: true });
  expect(
    await screen.findByRole("button", { name: /create bug/i }),
  ).toBeInTheDocument();
});

it("hides Create Bug when the backend cannot file one and nothing is routed", () => {
  renderWithCaps({ supportsBugCreation: false, supportsBugRouting: false });
  expect(screen.queryByRole("button", { name: /create bug/i })).toBeNull();
});
```

`renderWithCaps` should render the component with a capabilities object; match how the existing tests in that file supply capabilities.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd frontend; npx vitest run <that test file>`

Expected: FAIL. The button is absent in the first case.

- [ ] **Step 4: Widen each gate**

At every hit from Step 1, change the condition:

```tsx
const canCreateBug = caps.supportsBugCreation || caps.supportsBugRouting;
```

and use `canCreateBug` in place of the direct read.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd frontend; npx vitest run && npx tsc --noEmit`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add frontend/src
git commit -m "feat(ui): offer Create Bug when a profile routes bugs to Jira (-359)"
```

---

### Task 12: Document the feature

**Files:**
- Modify: `CHANGELOG.md`, `docs/user-guide/USER_GUIDE.md`

**Interfaces:**
- Consumes: the shipped behaviour from every earlier task.
- Produces: no code.

- [ ] **Step 1: Add the changelog entry**

Under `## [Unreleased]` in `CHANGELOG.md`, in the `### Added` list:

```markdown
- **Kiwi TCMS bugs into a Jira project** (-359). A Kiwi profile can now be given
  a Jira bug tracker: URL, project key, issue type and its own token, configured
  independently on the profile. Filing a bug creates the issue in Jira and adds
  a hyperlink back on the Kiwi execution. Sync reads those hyperlinks and fetches
  only the issues they name, so the Bugs view shows what relates to the product
  rather than the whole Jira project. If Jira creates the issue but the link back
  fails, the issue is kept and the commit says so, and retrying links the
  existing issue instead of filing a duplicate.
```

- [ ] **Step 2: Add the user-guide section**

In `docs/user-guide/USER_GUIDE.md`, in the section covering profiles, add a subsection after the Kiwi profile setup. Follow the surrounding heading level and voice:

```markdown
### Reporting Kiwi defects into Jira

Kiwi TCMS has no issue type of its own, so a defect raised against a Kiwi test
has nowhere to live. Point the profile at a Jira project instead.

Open the profile, and in **Bug tracker** fill in the Jira URL, the project key,
the issue type (usually `Bug`) and a personal access token for that Jira. The
token is stored in Windows Credential Manager, never in the database. Leave the
section empty to keep bug reporting switched off.

Once it is set, **Create Bug** appears on a failed test the same way it does for
an Xray profile. Committing creates the issue in Jira and adds a link to it on
the Kiwi execution the bug was raised from. The next sync reads those links and
fills in each issue's summary, status and priority.

If Jira creates the issue but the link back to Kiwi fails, the issue is kept and
the commit reports which link is missing. Commit again to add it; the existing
issue is reused rather than a second one being filed.
```

- [ ] **Step 3: Verify the docs build is untouched**

Run: `go build ./... && go test ./...`

Expected: PASS. Documentation changes touch no code, so this is a guard against a stray edit.

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md docs/user-guide/USER_GUIDE.md
git commit -m "docs: cover Kiwi bug reporting into Jira (-359)"
```

---

## Self-Review

**1. Spec coverage.**

| Spec section | Task |
|---|---|
| Goal: Kiwi profile files defects into Jira | 4, 5, 6, 9 |
| Verified: get_links / add_link exist, url required | 6 |
| Shape of the problem: two calls, two servers | 5 |
| Approach A: bug backend on the engine | 5, 9 |
| Configuration: independent, on the Kiwi profile | 4, 10 |
| Configuration: second connection row, role "bugs" | 1, 4 |
| Configuration: credential keyed by connection id | 4 |
| Constraint: `roleOrDefault` must know "bugs" | 1 |
| Constraint: `Primary` must select by id | 1 |
| Capabilities: `SupportsBugCreation` stays false | 3, 11 |
| Capabilities: `SupportsBugRouting` added, UI gates on it | 3, 11 |
| Flow: create to Jira, link to Kiwi | 5, 6 |
| Reads: hyperlinks to keys to issues | 6, 7, 8 |
| Failure: keep the bug, report, allow retry | 2, 5 |
| Failure: unreachable bug connection fails bug work only | 9 |
| Out of scope: repointing, pre-existing bugs, Xray routing, write-back | not implemented, by design |
| Testing: routing, failure, capability, credential, second row, frontend | 1, 3, 4, 5, 8, 10, 11 |

No gaps.

**2. Placeholder scan.** No "TBD", no "handle edge cases", no "similar to Task N". Four steps say "match the existing helper / read how the neighbouring code does it": Task 2 Step 1 (`newRepoWithProfile`), Task 5 Step 1 (`newSyncerRepoWithProfile`), Task 7 Step 4 (the Xray adapter's client field and converter) and Task 8 Step 1 (`SyncBugsOnly`, `seedTest`). These are deliberate. Each names the exact thing to look up and why, in a codebase whose test helpers this plan should follow rather than duplicate.

**3. Type consistency.** `WithBugBackend(b backend.Backend) Option` and `bugTarget()` are defined in Task 5 and used in Tasks 8 and 9. `MarkBugCreated(profileID string, changeID int64, realKey string) error` is defined in Task 2 and called in Task 5. `ByRole(workspaceID, role string) (Connection, error)` is defined in Task 1 and called in Tasks 3, 4 and 9. `SetBugBrowseBase(string)` is defined in Task 6 and called in Task 9 through an interface assertion, so `app.go` needs no Kiwi import. `BugKeyReader.ListBugsByKeys(ctx, keys []string) ([]Bug, error)` is defined in Task 7 and asserted in Task 8. `bugConnectionRole` is defined in Task 4 and used in Task 9. `SupportsBugRouting` is added in Task 3 and read in Task 11. The payload fields `execKey` and `createdKey` are added in Task 2 and read in Task 5 under the same JSON names.
