# Kiwi Bugs into a Jira Project — Design Spec

**Date:** 2026-09-04
**Base:** `main` @ `caa163b`
**Jira:** [RND_P_4TFINT_05-359](https://jira-01.sf.st.intranet/browse/RND_P_4TFINT_05-359), fixVersion V1.10.0
**Status:** design approved; pending spec review

## Goal

Let a Kiwi TCMS profile file defects into a dedicated Jira project. A tester
running executions in Kiwi raises a bug the same way an Xray user does; the
issue is created in Jira, and the Kiwi execution carries a hyperlink to it.

Kiwi has no Jira-style issue type. It advertises `SupportsBugCreation: false`
with the note "executions-as-links, not Jira-style issues", and every bug write
returns `ErrUnsupported`. So today a Kiwi profile's Bugs view is empty and the
Create Bug button is hidden. Teams that run tests in Kiwi and track defects in
Jira have no path between the two.

## Verified before designing

Two facts this design rests on were checked against a live instance rather than
inferred from comments.

**Kiwi's link RPCs exist and work.** `TestExecution.get_links` returns `[]` for
an execution with no links, and `TestExecution.add_link` rejects a call missing
`url` with `[('url', ['This field is required.'])]`. Both are real, and the
error names the field the link needs. The comment on `ListBugs` ("best-effort
via TestExecution.get_links") was accurate.

**A dedicated bug project already exists as a concept.** `bugProjectKey`
(`app.go:1617`) already resolves a `dedicated` mode against `BugProjectKey`.
What is missing is not a project. It is a *server and a credential*.

## The shape of the problem

Bug creation is not a direct backend call. `App.CreateBugForTest`
(`app.go:1598`) queues a `bug_create` pending change and returns; the remote
work happens at commit. `commitBugCreates` (`commit.go:1196`) then makes two
calls against one backend:

```go
realKey, err := e.backend.CreateBug(ctx, p.ProjectKey, p.IssueType, ...)
...
err := e.backend.CreateBugLink(ctx, p.TestKey, key)
```

For a Kiwi profile those two halves belong to different servers: the create goes
to Jira, the link goes to Kiwi. The single `e.backend` is the whole problem.

## Approaches

**A. A bug backend on the engine (chosen).** The commit and sync engines take
an optional second `backend.Backend` used only for bug work. When a profile has
no Jira bug configuration it is nil and every path routes exactly as today.
`commitBugCreates` sends `CreateBug` to the bug backend and `CreateBugLink` to
the primary one.

Chosen because the split is genuinely per-operation, and this is the only option
that says so in one place. It also keeps both adapters ignorant of each other.

**B. A `BugBackend` interface the primary implements.** The Kiwi adapter would
hold a Jira client and route internally. Rejected: it makes the Kiwi adapter
know about Jira, which is the coupling the `backend.Backend` abstraction exists
to prevent. It would also put Jira REST knowledge in `internal/backend/kiwi`,
which no other adapter has.

**C. Resolve the backend per call site.** Every bug method asks "which backend
handles this?". Rejected: the same decision restated in eight places, and each
new bug method is a chance to get it wrong.

## Configuration

The bug connection is configured independently on the Kiwi profile: URL,
project key, issue type, credential, CA certificate, and the untrusted-TLS flag.
It is not inherited from another profile, and configuring it is optional.

**It becomes a second `connection` row, not six new profile columns.** The
connection table was built for exactly this. `internal/profile/profile.go:64-67`
records that it "exists so a workspace can eventually hold multiple backend
connections, but today there is exactly one". This is its first real consumer.
The bug connection is a row with `Role: "bugs"`, which extends the existing
`source` / `target` / `both` vocabulary the bridge uses.

**The credential is a second Credential Manager entry, keyed by the connection
id.** `internal/connection/connection.go:26-28` already states the rule:
credentials are "stored separately in the OS credential manager, keyed by this
Connection's ID". The primary connection's id equals the profile id, so today's
entries already satisfy it; the bug connection gets its own id and its own
entry, with no new naming convention invented. Deleting a profile deletes the
credential for every connection it owns. Neither credential reaches the
database, a log line, or an error string.

Two existing behaviors in `internal/connection` were written for the
one-row-per-workspace shape and have to change before a second row is safe.
Both were read rather than assumed.

- **`roleOrDefault` normalizes any unrecognized role to `"both"`**
  (`connection.go:81-90`). A row written with `Role: "bugs"` would read back as
  `"both"`, silently turning the bug connection into a second primary. The
  vocabulary must include `"bugs"` before anything writes it.
- **`Primary` returns `ORDER BY created_at, id LIMIT 1`**
  (`connection.go:128-135`), not the row whose id is the workspace id. Both rows
  can share a `created_at`, leaving the tie to be broken by id, and a generated
  connection id can sort before a profile id. `Primary` must select
  `id = workspaceID` so adding a bug connection cannot displace the profile's
  own connection.

## Capabilities

`SupportsBugCreation` describes what the primary backend can do and **stays
false for Kiwi**. Kiwi still cannot create a bug; something else can on its
behalf, and conflating those would make the flag lie.

A new `SupportsBugRouting` reports that a bug connection is configured and
usable. The UI gates the Create Bug button on
`SupportsBugCreation || SupportsBugRouting`, so an Xray profile is unaffected
and a Kiwi profile gains the button only once routing is set up.

## What flows where

```
File a bug from a Kiwi execution
  -> queue bug_create                     unchanged, local, no remote call
  -> commit:
       CreateBug      -> Jira bug connection   -> real issue key
       CreateBugLink  -> Kiwi primary          -> add_link on the execution
```

Reads mirror it. The sync's bug stage, for a profile with routing, reads the
hyperlinks off Kiwi executions via `get_links`, extracts the Jira keys, and
fetches exactly those issues from the bug connection. Bugs never filed from XTM
do not appear, which is the intent: the Bugs view shows what relates to this
product, not the whole Jira project.

## Failure

**A Jira create that succeeds followed by a Kiwi link that fails keeps the
bug.** The commit result names the missing link so it can be retried, matching
how failed commits already surface. Nothing remote is deleted: an issue that may
already have been seen or edited is worse to destroy than a link is to lose, and
the delete can fail too.

The failure is reported, never logged and swallowed. That pattern is precisely
what `-336` was, where a stage failed and the sync still claimed success.

**A missing or unreachable bug connection** fails the bug work only. The rest of
a commit or sync proceeds, and the failure names the connection rather than
surfacing a bare transport error.

## Out of scope

- **Repointing existing links** when the Jira configuration changes. Already
  linked bugs keep their hyperlinks, which still resolve; only new bugs go to
  the new project. Moving them is a migration, not a configuration change.
- **Linking to a bug that already exists in Jira** but was never filed from XTM.
  The read path is deliberately scoped to what the Kiwi executions link to.
- **Bug routing for an Xray profile.** Xray files into its own Jira and already
  supports a dedicated project through `bugProjectMode`.
- **Two-way sync of bug state into Kiwi.** The hyperlink is the only thing
  written back.

## Testing

**Go, routing:** a profile with no bug connection routes every bug call to the
primary backend exactly as today; a profile with one sends `CreateBug` to the
bug backend and `CreateBugLink` to the primary; a nil bug backend on a Kiwi
profile leaves the Create Bug path unsupported rather than panicking.

**Go, failure:** a Jira create that succeeds with a Kiwi link that fails reports
the link failure, keeps the created key, and leaves the pending row so it can be
retried; an unreachable bug connection fails the bug stage without failing the
rest of the commit.

**Go, capability:** `SupportsBugCreation` stays false for Kiwi with routing
configured, and `SupportsBugRouting` is true only when a usable bug connection
exists.

**Go, credential:** the bug credential is stored and loaded under the bug
connection's own id, and deleting a profile removes the entries for every
connection it owns.

**Go, the second connection row:** a connection saved with role `"bugs"` reads
back as `"bugs"` rather than being normalized to `"both"`, and `Primary` still
returns the profile's own connection when a bug connection shares its
`created_at`.

**Frontend:** the Create Bug button appears for a Kiwi profile only when routing
is configured, and the profile form round-trips the bug connection.
