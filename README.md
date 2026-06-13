# Xray Test Manager

A lightweight **Windows and macOS** desktop application for managing **Xray test
cases in Jira Data Center** at scale — built for QA teams whose projects hold
10,000+ test cases and have outgrown the Jira browser UI.

## Why

The Jira/Xray web interface becomes slow and cumbersome with very large test
suites — navigation, filtering, and especially bulk edits. Xray Test Manager
synchronises test data into a fast local store and gives testers a dedicated,
bulk-first interface, writing changes back to Jira on commit.

## Status

✅ **Feature-complete in demo mode.** Phases 0–6 are implemented and exercised
end to end against the built-in demo data: sync, fast browse/search/filter/sort,
saved views, configurable columns, local field/step/custom-field editing with
on-commit sync and conflict resolution, workflow transitions (single + bulk),
all bulk operations, Test Sets / Plans / Executions (board, detail view, CRUD,
assign/remove tests), Test Repository folders (browse/move + CRUD),
preconditions, test review (single + bulk), CSV/XLSX import and export, a
pytest scaffold generator, a statistics dashboard with a traceability Sankey,
diagnostics, sync history, light/dark themes, and profile management.

🔌 **Pending: live Xray/Jira wiring.** Every read/write is proven against demo
data; the real-Jira REST calls are stubbed (`NOTE`/`TODO(xtm)` markers in
`internal/jira/`) until they can be verified against an actual Xray Server/DC
8.4.0 instance. Until then the app is fully usable in demo mode.

## Stack

- **Go** + **Wails v2** + **React / TypeScript**
- **SQLite** local store (pure-Go `modernc.org/sqlite` — no cgo)
- **Windows:** a single `.exe`; only WebView2 is required (built into Windows 11)
- **macOS:** a universal `.app` (Apple Silicon + Intel); uses the built-in WKWebView
- Secrets in the OS-native store — Windows Credential Manager / macOS Keychain
- Targets **Jira DC 8.14+** and **Xray Server / DC 8.4.0**

## Development

Prerequisites: Go 1.25+, Node.js, and the Wails CLI
(`go install github.com/wailsapp/wails/v2/cmd/wails@latest`).

```sh
wails dev                              # run with live reload
wails build                            # Windows: build/bin/xray-test-manager.exe
wails build -platform darwin/universal # macOS:   build/bin/xray-test-manager.app
go build ./...                         # compile-check the Go backend only
```

## Releasing & distribution

Releases ship Windows and macOS artifacts plus checksums:

| Artifact | Use |
| --- | --- |
| `xray-test-manager-<ver>-windows-amd64.exe` | Windows portable — run directly, no install |
| `xray-test-manager-<ver>-windows-amd64-installer.exe` | Windows Inno Setup installer (Start-menu entry, uninstaller) |
| `xray-test-manager-<ver>-macos-universal.zip` | macOS universal `.app` (Apple Silicon + Intel) |
| `xray-test-manager-<ver>-user-guide.zip` | User guide (markdown + screenshots) |
| `SHA256SUMS.txt` / `SHA256SUMS-macos.txt` | Integrity check for the above |

> **macOS Gatekeeper:** the `.app` is not yet code-signed/notarized, so on first
> launch macOS may block it. Right-click the app → **Open** (then confirm), or run
> `xattr -dr com.apple.quarantine xray-test-manager.app` to clear the quarantine flag.

**Build locally** (`scripts/release.ps1` builds, version-stamps, bundles into
`dist/`, and writes checksums):

```powershell
# Stamp + build the portable exe and the installer (needs Inno Setup / ISCC.exe)
./scripts/release.ps1 -Version 0.2.0

# Portable exe only (skip the installer)
./scripts/release.ps1 -Version 0.2.0 -NoInstaller
```

The version is the single source of truth in `wails.json` (`info.productVersion`);
the script stamps it and Wails bakes it into the installer and the exe metadata.

**Cut a GitHub release** — push a tag and CI (`.github/workflows/release.yml`)
runs two jobs: `release-windows` (on `windows-latest`) builds the installer and
portable exe, and `release-macos` (on `macos-latest`) builds the universal `.app`.
Both publish to the same GitHub Release. (`scripts/release.ps1` is Windows-only;
the macOS `.app` is built directly with `wails build`.)

```powershell
git tag v0.2.0
git push origin v0.2.0
```

## Demo mode

No Jira instance handy? Create a profile with **Jira base URL `demo`** (any
project key, any token). The backend short-circuits the sync and serves
~5,000 deterministically-generated tests — plus sample Test Sets / Plans /
Executions, folders and preconditions — so the full UI can be exercised end to
end. The header shows a yellow `DEMO` chip while a demo profile is active.

## Roadmap

| Phase | Theme | Status |
| --- | --- | --- |
| 0 | Foundations — scaffold, local store, profiles, PAT auth | ✅ |
| 1 | MVP — fast browse / search / filter / sort, saved views, columns | ✅ |
| 2 | Local editing (fields / steps / custom fields) + on-commit sync, conflict resolution | ✅ |
| 3 | Bulk operations (edit / transition / allocate / move / preconditions / review) | ✅ |
| 4 | Workflow transitions, statistics dashboard, traceability Sankey | ✅ |
| 5 | XLSX / CSV import + export | ✅ |
| 6 | pytest scaffold, containers & folder CRUD, test review, themes, diagnostics | ✅ |
| 7 | Live Xray/Jira REST wiring (verify against a real instance) | 🔌 pending |

Full planning, requirements (FR-1…FR-13) and design notes are maintained in the
project's Outline documentation collection.
