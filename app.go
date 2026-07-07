package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	goruntime "runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"xray-test-manager/internal/coverage"
	"xray-test-manager/internal/jira"
	"xray-test-manager/internal/profile"
	"xray-test-manager/internal/settings"
	"xray-test-manager/internal/store"
	"xray-test-manager/internal/syncer"
	"xray-test-manager/internal/testrepo"
)

// recoverToError converts a panic in a bound App method into a returned error
// instead of crashing the whole app. Wails runs each bound call in a goroutine,
// so an unrecovered panic would terminate the process. The helper logs the panic
// with a full stack trace (so the root cause is diagnosable from the logs) and,
// when the method has a named error return that is still nil, sets it to a
// generic internal error. Use as: defer recoverToError("MethodName", &err).
func recoverToError(method string, errp *error) {
	if r := recover(); r != nil {
		log.Printf("xtm: PANIC recovered in %s: %v\n%s", method, r, debug.Stack())
		if errp != nil && *errp == nil {
			*errp = fmt.Errorf("internal error in %s: %v", method, r)
		}
	}
}

// App is the Wails application backend. Exported methods on App are bound and
// callable from the React frontend.
type App struct {
	ctx        context.Context
	store      *store.Store
	profiles   *profile.Manager
	creds      profile.CredentialStore
	settings   *settings.Manager
	repo       *testrepo.Repository
	cov        *coverage.Module
	dbPath     string
	logPath    string
	startupErr string

	// statusCache holds the per-profile workflow status list fetched from Jira,
	// for the session — workflow config rarely changes, so one fetch is enough.
	// priorityCache does the same for the Jira priority scheme (FR-1); both are
	// guarded by statusMu.
	statusMu      sync.Mutex
	statusCache   map[string][]string
	priorityCache map[string][]string
}

// HealthInfo reports whether the backend initialised successfully. The
// frontend calls Health() first and surfaces any error so users see what
// actually failed instead of a blank screen or a cryptic nil-pointer panic.
type HealthInfo struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error"`
	DBPath  string `json:"dbPath"`
	LogPath string `json:"logPath"`
}

// NewApp creates a new App application struct.
func NewApp() *App {
	return &App{statusCache: map[string][]string{}, priorityCache: map[string][]string{}}
}

// startup wires the service layer. Failures are captured into startupErr
// instead of being swallowed — the GUI has no console on Windows, so
// without this they would be invisible.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// File logging in the app data dir so startup output is visible even
	// when launched without a console.
	if path, err := setupFileLogging(); err == nil {
		a.logPath = path
		log.Printf("xtm: starting up — log at %s", path)
	}

	if err := a.initStore(); err != nil {
		a.startupErr = err.Error()
		log.Printf("xtm: startup failed: %v", err)
	}
}

// initStore opens the local database and constructs the service objects.
func (a *App) initStore() error {
	dbPath, err := defaultDBPath()
	if err != nil {
		return fmt.Errorf("resolve database path: %w", err)
	}
	a.dbPath = dbPath

	st, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open local store at %s: %w", dbPath, err)
	}
	a.store = st
	a.profiles = profile.NewManager(st)
	a.creds = profile.NewCredentialStore()
	a.settings = settings.NewManager(st)
	a.repo = testrepo.NewRepository(st)
	a.cov = coverage.New(st, a.repo)
	log.Printf("xtm: local store ready at %s", dbPath)
	return nil
}

// shutdown closes the local database when the window is closed.
func (a *App) shutdown(ctx context.Context) {
	if a.store != nil {
		if err := a.store.Close(); err != nil {
			log.Printf("xtm: close local store: %v", err)
		}
	}
}

// Health reports backend startup status. Always safe to call.
func (a *App) Health() HealthInfo {
	return HealthInfo{
		OK:      a.startupErr == "" && a.profiles != nil,
		Error:   a.startupErr,
		DBPath:  a.dbPath,
		LogPath: a.logPath,
	}
}

// --- Logs & diagnostics (FR-12.4) ---

// Diagnostics is the environment + state summary shown in the diagnostics view.
type Diagnostics struct {
	Version       string `json:"version"`
	DBPath        string `json:"dbPath"`
	LogPath       string `json:"logPath"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	GoVersion     string `json:"goVersion"`
	SchemaVersion int    `json:"schemaVersion"`
	ProfileCount  int    `json:"profileCount"`
	StartupError  string `json:"startupError"`
}

// GetDiagnostics returns an environment + state summary for the diagnostics
// view (FR-12.4). Safe to call even if the store failed to initialise.
func (a *App) GetDiagnostics() Diagnostics {
	d := Diagnostics{
		Version:       productVersion(),
		DBPath:        a.dbPath,
		LogPath:       a.logPath,
		OS:            goruntime.GOOS,
		Arch:          goruntime.GOARCH,
		GoVersion:     goruntime.Version(),
		SchemaVersion: store.SchemaVersion(),
		StartupError:  a.startupErr,
	}
	if a.profiles != nil {
		if ps, err := a.profiles.List(); err == nil {
			d.ProfileCount = len(ps)
		}
	}
	return d
}

// ReadLog returns the last maxLines lines of the app log (FR-12.4). A maxLines
// of 0 or less returns the whole file.
func (a *App) ReadLog(maxLines int) (string, error) {
	if a.logPath == "" {
		return "(no log file configured)", nil
	}
	data, err := os.ReadFile(a.logPath)
	if err != nil {
		return "", fmt.Errorf("read log: %w", err)
	}
	if maxLines <= 0 {
		return string(data), nil
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n"), nil
}

// ExportDiagnostics writes the environment summary plus the recent log to a
// timestamped text file in the app data dir and returns its path (FR-12.4).
func (a *App) ExportDiagnostics() (string, error) {
	d := a.GetDiagnostics()
	logTail, _ := a.ReadLog(1000)

	var b strings.Builder
	fmt.Fprintln(&b, "Xray Test Manager — diagnostics")
	fmt.Fprintf(&b, "Version:        %s\n", d.Version)
	fmt.Fprintf(&b, "Generated:      %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "OS / Arch:      %s / %s\n", d.OS, d.Arch)
	fmt.Fprintf(&b, "Go version:     %s\n", d.GoVersion)
	fmt.Fprintf(&b, "Schema version: %d\n", d.SchemaVersion)
	fmt.Fprintf(&b, "Profiles:       %d\n", d.ProfileCount)
	fmt.Fprintf(&b, "Database:       %s\n", d.DBPath)
	fmt.Fprintf(&b, "Log file:       %s\n", d.LogPath)
	if d.StartupError != "" {
		fmt.Fprintf(&b, "Startup error:  %s\n", d.StartupError)
	}
	fmt.Fprintf(&b, "\n--- recent log ---\n%s\n", logTail)

	dir := filepath.Dir(a.dbPath)
	if dir == "" || dir == "." {
		if cfg, err := os.UserConfigDir(); err == nil {
			dir = filepath.Join(cfg, "xray-test-manager")
		}
	}
	path := filepath.Join(dir, fmt.Sprintf("diagnostics-%d.txt", time.Now().Unix()))
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("write diagnostics: %w", err)
	}
	return path, nil
}

// requireStore is the guard every store-dependent bound method calls. It
// turns a backend init failure into a useful frontend error instead of a
// nil-pointer panic deep in the call chain.
func (a *App) requireStore() error {
	if a.startupErr != "" {
		return fmt.Errorf("local store unavailable: %s", a.startupErr)
	}
	if a.profiles == nil || a.repo == nil {
		return errors.New("local store not initialised")
	}
	return nil
}

// --- Profiles (FR-5) ---

// ListProfiles returns all configured connection profiles.
func (a *App) ListProfiles() ([]profile.Profile, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.profiles.List()
}

// CreateProfile stores a new profile and saves its PAT to the OS credential
// manager. The token is never written to the database. scopeJQL is an optional
// JQL fragment that narrows which Tests sync (FR-5.4). caCert and
// allowUntrustedTLS configure TLS trust for the new profile (RND_P_4TFINT_05-243).
func (a *App) CreateProfile(name, jiraURL, projectKey, scopeJQL, bugIssueType, bugProjectMode, bugProjectKey, token, caCert string, allowUntrustedTLS bool) (profile.Profile, error) {
	if err := a.requireStore(); err != nil {
		return profile.Profile{}, err
	}
	p, err := a.profiles.Create(name, jiraURL, projectKey, scopeJQL, bugIssueType, bugProjectMode, bugProjectKey, caCert, allowUntrustedTLS)
	if err != nil {
		return profile.Profile{}, err
	}
	if err := a.creds.Save(p.ID, token); err != nil {
		_ = a.profiles.Delete(p.ID) // don't leave a credential-less profile behind
		return profile.Profile{}, fmt.Errorf("save credentials: %w", err)
	}
	return p, nil
}

// CreateProfileReusingToken creates a new profile that reuses the Personal
// Access Token already stored for an existing profile (FR-5) — convenient when
// several projects share one Jira instance. The token is copied within the OS
// credential manager and never exposed to the frontend.
func (a *App) CreateProfileReusingToken(name, jiraURL, projectKey, scopeJQL, bugIssueType, bugProjectMode, bugProjectKey, sourceProfileID string) (profile.Profile, error) {
	if err := a.requireStore(); err != nil {
		return profile.Profile{}, err
	}
	token, err := a.creds.Load(sourceProfileID)
	if err != nil {
		return profile.Profile{}, fmt.Errorf("read token from the selected profile: %w", err)
	}
	if strings.TrimSpace(token) == "" {
		return profile.Profile{}, fmt.Errorf("the selected profile has no stored token to reuse")
	}
	p, err := a.profiles.Create(name, jiraURL, projectKey, scopeJQL, bugIssueType, bugProjectMode, bugProjectKey, "", false)
	if err != nil {
		return profile.Profile{}, err
	}
	if err := a.creds.Save(p.ID, token); err != nil {
		_ = a.profiles.Delete(p.ID) // don't leave a credential-less profile behind
		return profile.Profile{}, fmt.Errorf("save credentials: %w", err)
	}
	return p, nil
}

// UpdateProfile edits an existing profile (FR-5) — e.g. to fix a wrong project
// key. If the project key or Jira URL changes, the locally cached data (which
// belongs to the old project) is purged so the next sync pulls the correct
// project cleanly, and the per-session status/priority caches are dropped. A
// non-empty token replaces the stored PAT; an empty token leaves it unchanged.
// caCert and allowUntrustedTLS update the TLS trust settings (RND_P_4TFINT_05-243).
func (a *App) UpdateProfile(id, name, jiraURL, projectKey, scopeJQL, bugIssueType, bugProjectMode, bugProjectKey, token, caCert string, allowUntrustedTLS bool) (profile.Profile, error) {
	if err := a.requireStore(); err != nil {
		return profile.Profile{}, err
	}
	old, err := a.profiles.Get(id)
	if err != nil {
		return profile.Profile{}, err
	}
	if err := a.profiles.Update(id, name, jiraURL, projectKey, scopeJQL, bugIssueType, bugProjectMode, bugProjectKey, caCert, allowUntrustedTLS); err != nil {
		return profile.Profile{}, err
	}
	if strings.TrimSpace(token) != "" {
		if err := a.creds.Save(id, token); err != nil {
			return profile.Profile{}, fmt.Errorf("save credentials: %w", err)
		}
	}
	if old.ProjectKey != projectKey || old.JiraURL != jiraURL {
		if err := a.repo.PurgeProfile(id); err != nil {
			return profile.Profile{}, fmt.Errorf("clear cached data for the new project: %w", err)
		}
		a.statusMu.Lock()
		delete(a.statusCache, id)
		delete(a.priorityCache, id)
		a.statusMu.Unlock()
	}
	return a.profiles.Get(id)
}

// UpdateProfileScope changes a profile's JQL scope override (FR-5.4). It takes
// effect on the next sync.
func (a *App) UpdateProfileScope(id, scopeJQL string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.profiles.UpdateScope(id, scopeJQL)
}

// profileConfig is the shareable shape of a profile — everything except the
// credential, which never leaves the OS credential manager (FR-5.5).
type profileConfig struct {
	Name           string `json:"name"`
	JiraURL        string `json:"jiraUrl"`
	ProjectKey     string `json:"projectKey"`
	ScopeJQL       string `json:"scopeJql"`
	BugIssueType   string `json:"bugIssueType"`
	BugProjectMode string `json:"bugProjectMode"`
	BugProjectKey  string `json:"bugProjectKey"`
}

// ExportProfile writes a profile's configuration (without its token) to a
// user-chosen JSON file (FR-5.5). Returns the saved path, or "" if cancelled.
func (a *App) ExportProfile(id string) (string, error) {
	if err := a.requireStore(); err != nil {
		return "", err
	}
	p, err := a.profiles.Get(id)
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(profileConfig{
		Name: p.Name, JiraURL: p.JiraURL, ProjectKey: p.ProjectKey, ScopeJQL: p.ScopeJQL,
		BugIssueType: p.BugIssueType, BugProjectMode: p.BugProjectMode, BugProjectKey: p.BugProjectKey,
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode profile: %w", err)
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export profile",
		DefaultFilename: exportFilename(sanitizeFilename(p.Name) + "-profile.json"),
		Filters:         []runtime.FileFilter{{DisplayName: "JSON", Pattern: "*.json"}},
	})
	if err != nil {
		return "", fmt.Errorf("save dialog: %w", err)
	}
	if path == "" {
		return "", nil // cancelled
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write profile: %w", err)
	}
	return path, nil
}

// ImportProfile creates a profile from a user-chosen JSON config file (FR-5.5).
// The new profile has no credential — set one with UpdateProfileToken before
// syncing. A zero-value profile (empty id) is returned when the dialog is
// cancelled.
func (a *App) ImportProfile() (profile.Profile, error) {
	if err := a.requireStore(); err != nil {
		return profile.Profile{}, err
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Import profile",
		Filters: []runtime.FileFilter{{DisplayName: "JSON", Pattern: "*.json"}},
	})
	if err != nil {
		return profile.Profile{}, fmt.Errorf("open dialog: %w", err)
	}
	if path == "" {
		return profile.Profile{}, nil // cancelled
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return profile.Profile{}, fmt.Errorf("read file: %w", err)
	}
	var cfg profileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return profile.Profile{}, fmt.Errorf("not a valid profile file: %w", err)
	}
	if strings.TrimSpace(cfg.Name) == "" || strings.TrimSpace(cfg.JiraURL) == "" || strings.TrimSpace(cfg.ProjectKey) == "" {
		return profile.Profile{}, fmt.Errorf("profile file is missing name, URL or project key")
	}
	return a.profiles.Create(cfg.Name, cfg.JiraURL, cfg.ProjectKey, cfg.ScopeJQL, cfg.BugIssueType, cfg.BugProjectMode, cfg.BugProjectKey, "", false)
}

// UpdateProfileToken replaces a profile's stored PAT in the OS credential
// manager (FR-5.5 / FR-8.3) — used to complete an imported profile or rotate a
// token. The token is never written to the database.
func (a *App) UpdateProfileToken(id, token string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	if _, err := a.profiles.Get(id); err != nil {
		return err
	}
	return a.creds.Save(id, token)
}

// sanitizeFilename strips path-unsafe characters from a profile name for use in
// a default export filename.
func sanitizeFilename(name string) string {
	repl := strings.NewReplacer(
		"/", "-", "\\", "-", ":", "-", "*", "-", "?", "-",
		"\"", "-", "<", "-", ">", "-", "|", "-", " ", "_",
	)
	out := strings.TrimSpace(repl.Replace(name))
	if out == "" {
		return "profile"
	}
	return out
}

// exportFilename prefixes a default export filename with a local YYYYMMDDHHMM
// timestamp, e.g. "202606261430_dashboard.xlsx". This keeps saved exports
// sorted chronologically and stops a second export silently overwriting an
// earlier one. The original title and extension are preserved (the files are
// genuine XLSX/CSV, so their extension is kept accurate).
func exportFilename(name string) string {
	return time.Now().Format("200601021504") + "_" + name
}

// tlsOptions derives jira.Option values from a profile's TLS settings. When
// neither CACert nor AllowUntrustedTLS is set the returned slice is empty and
// NewClient uses the default system trust -- identical to the pre-feature
// behaviour (RND_P_4TFINT_05-243).
func tlsOptions(p profile.Profile) []jira.Option {
	var opts []jira.Option
	if p.CACert != "" {
		opts = append(opts, jira.WithCACert(p.CACert))
	}
	if p.AllowUntrustedTLS {
		opts = append(opts, jira.WithInsecureTLS(true))
	}
	return opts
}

// DeleteProfile removes a profile and its stored credentials.
func (a *App) DeleteProfile(id string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	if err := a.profiles.Delete(id); err != nil {
		return err
	}
	if err := a.creds.Delete(id); err != nil {
		log.Printf("xtm: delete credentials for %s: %v", id, err)
	}
	// Remove the profile's cached data so it doesn't linger (FR-5.3).
	if err := a.repo.PurgeProfile(id); err != nil {
		log.Printf("xtm: purge cached data for %s: %v", id, err)
	}
	// Clear the default-profile setting if it pointed at this profile.
	if s, err := a.settings.Get(); err == nil && s.DefaultProfileID == id {
		if err := a.settings.SetDefaultProfileID(""); err != nil {
			log.Printf("xtm: clear default profile after delete: %v", err)
		}
	}
	return nil
}

// --- Global settings (FR-12.2) ---

// GetSettings returns the global application preferences (default profile).
func (a *App) GetSettings() (settings.Settings, error) {
	if err := a.requireStore(); err != nil {
		return settings.Settings{}, err
	}
	return a.settings.Get()
}

// SetDefaultProfile records which profile is auto-selected on launch (FR-12.2).
// An empty id clears the default.
func (a *App) SetDefaultProfile(profileID string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.settings.SetDefaultProfileID(profileID)
}

// SetTheme records the colour theme preference (FR-12.2): "light", "dark" or
// "system".
func (a *App) SetTheme(theme string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.settings.SetTheme(theme)
}

// SetRequirementLinkType persists the issue-link type name used when linking
// Tests to Requirements on commit (FR-13 / #275). The change takes effect on
// the next CommitPendingChanges call. Pass "" to revert to auto-resolve.
func (a *App) SetRequirementLinkType(name string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.settings.SetRequirementLinkType(name)
}

// ListRequirementLinkTypes returns all issue-link type names defined on the
// given profile's Jira instance, for populating the link-type config dropdown.
// Demo mode returns a preset list without a network call.
func (a *App) ListRequirementLinkTypes(profileID string) ([]string, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	p, err := a.profiles.Get(profileID)
	if err != nil {
		return nil, err
	}
	token, err := a.creds.Load(profileID)
	if err != nil {
		return nil, fmt.Errorf("load credentials: %w", err)
	}
	return jira.NewClient(p.JiraURL, token, tlsOptions(p)...).ListIssueLinkTypes(a.ctx)
}

// SetShowCoverage records whether the (opt-in, hidden-by-default) Coverage
// top-nav tab is shown.
func (a *App) SetShowCoverage(show bool) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.settings.SetShowCoverage(show)
}

// --- Connection & sync (FR-1, FR-8) ---

// TestConnection verifies a Jira URL and PAT, returning the display name of
// the authenticated user (FR-8.4). It does not depend on the local store --
// useful for diagnosing PAT issues even if the store failed to initialise.
// caCert and allowUntrustedTLS are applied directly so the connection test
// reflects the profile's TLS settings before the profile is saved.
func (a *App) TestConnection(jiraURL, token, caCert string, allowUntrustedTLS bool) (string, error) {
	var opts []jira.Option
	if caCert != "" {
		opts = append(opts, jira.WithCACert(caCert))
	}
	if allowUntrustedTLS {
		opts = append(opts, jira.WithInsecureTLS(true))
	}
	user, err := jira.NewClient(jiraURL, token, opts...).TestConnection(a.ctx)
	if err != nil {
		return "", err
	}
	return user.DisplayName, nil
}

// TestProfileConnection verifies a saved profile's connection using its stored
// PAT. Unlike TestConnection, no token is required from the caller -- it is
// loaded from the credential manager so the button works when editing a profile
// (where the token field is blank). jiraURL, caCert, and allowUntrustedTLS
// reflect unsaved form edits, not the saved profile (FR-8.4). Returns the
// authenticated user's display name.
func (a *App) TestProfileConnection(profileID, jiraURL, caCert string, allowUntrustedTLS bool) (string, error) {
	token, err := a.creds.Load(profileID)
	if err != nil {
		return "", fmt.Errorf("load credentials for profile %s: %w", profileID, err)
	}
	var opts []jira.Option
	if caCert != "" {
		opts = append(opts, jira.WithCACert(caCert))
	}
	if allowUntrustedTLS {
		opts = append(opts, jira.WithInsecureTLS(true))
	}
	user, err := jira.NewClient(jiraURL, token, opts...).TestConnection(a.ctx)
	if err != nil {
		return "", err
	}
	return user.DisplayName, nil
}

// SyncProfile syncs a profile, emitting "sync:progress" events to the
// frontend as pages complete. The first sync (no watermark) is a full pull;
// subsequent syncs use the previous sync's timestamp as a watermark for an
// incremental fetch (FR-1.1 / FR-1.2).
func (a *App) SyncProfile(profileID string) error {
	return a.runSync(profileID, false)
}

// runPartialSync builds the Jira client + sync engine for a profile and runs a
// single sub-phase (requirements / containers / bugs) — the per-view refresh
// actions (#7). Unlike a full Sync it doesn't touch the watermark or sync
// history, but it emits the same "sync:progress" events (an initial stage label,
// the per-item counts the phase reports, and a terminal Done) so the shared
// status bar reflects the partial sync just like a full one.
func (a *App) runPartialSync(profileID, stage string, fn func(*syncer.Engine, string, func(syncer.Progress)) error) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	p, err := a.profiles.Get(profileID)
	if err != nil {
		return err
	}
	token, err := a.creds.Load(profileID)
	if err != nil {
		return fmt.Errorf("load credentials: %w", err)
	}
	engine := syncer.New(jira.NewClient(p.JiraURL, token, tlsOptions(p)...), a.repo)

	onProgress := func(pr syncer.Progress) {
		runtime.EventsEmit(a.ctx, "sync:progress", pr)
	}
	// Show the phase label immediately, before the first counted item.
	onProgress(syncer.Progress{Stage: stage})
	// Always clear the status bar when the phase ends, success or failure.
	defer runtime.EventsEmit(a.ctx, "sync:progress", syncer.Progress{Done: true})

	return fn(engine, p.ProjectKey, onProgress)
}

// SyncRequirements refreshes just the requirement coverage from Jira (#7).
func (a *App) SyncRequirements(profileID string) error {
	return a.runPartialSync(profileID, "Syncing requirements", func(e *syncer.Engine, projectKey string, onProgress func(syncer.Progress)) error {
		return e.SyncRequirements(a.ctx, profileID, projectKey, onProgress)
	})
}

// SyncContainers refreshes just the Test Sets / Plans / Executions from Jira (#7).
func (a *App) SyncContainers(profileID string) error {
	return a.runPartialSync(profileID, "Syncing containers", func(e *syncer.Engine, projectKey string, onProgress func(syncer.Progress)) error {
		return e.SyncContainers(a.ctx, profileID, projectKey, onProgress)
	})
}

// SyncBugs reconciles defect issues linked to the profile's tests (partial
// sync behind the Bugs panel's refresh button). It also refreshes the
// run/execution data for all bug-affected tests so the run-history breakdown
// updates without a full re-sync. The run-data pass is best-effort: an error
// there does not fail the bug sync.
func (a *App) SyncBugs(profileID string) error {
	return a.runPartialSync(profileID, "Syncing bugs", func(e *syncer.Engine, projectKey string, onProgress func(syncer.Progress)) error {
		if err := e.SyncBugs(a.ctx, profileID, projectKey, onProgress); err != nil {
			return err
		}
		// Best-effort: refresh run data for bug-affected tests. A failure here
		// is logged internally by SyncBugRunData and does not fail the sync.
		if runErr := e.SyncBugRunData(a.ctx, profileID, onProgress); runErr != nil {
			log.Printf("xtm: SyncBugs: run-data refresh: %v (continuing)", runErr)
		}
		return nil
	})
}

// SyncTests refreshes just the project's Tests (and folder membership) from
// Jira, the per-view partial sync behind the Browse / test-case view's Sync
// button. Incremental against the last full sync's watermark; it does not
// advance the watermark.
func (a *App) SyncTests(profileID string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.runPartialSync(profileID, "Syncing tests", func(e *syncer.Engine, projectKey string, onProgress func(syncer.Progress)) error {
		p, err := a.profiles.Get(profileID)
		if err != nil {
			return err
		}
		state, err := a.repo.GetSyncState(profileID)
		if err != nil {
			return fmt.Errorf("read sync state: %w", err)
		}
		return e.SyncTests(a.ctx, profileID, projectKey, p.ScopeJQL, state.LastSyncedAt, onProgress)
	})
}

// GetBugDetail fetches the extended fields for a defect issue: description,
// Defect Origin, Defect Analysis, and Correction Details. These are not cached
// locally (the bug table only holds summary/status/priority); they are fetched
// lazily on detail-panel open, mirroring GetTestMeta / GetTestCustomFields.
//
// Returns an empty BugDetail without a network call when bugKey looks like a
// locally-created (not-yet-committed) issue.
func (a *App) GetBugDetail(profileID, bugKey string) (jira.BugDetail, error) {
	if err := a.requireStore(); err != nil {
		return jira.BugDetail{}, err
	}
	// Local / not-yet-committed bug keys carry a "NEW-" prefix and have no Jira
	// issue yet.
	if strings.HasPrefix(bugKey, "NEW-") {
		return jira.BugDetail{}, nil
	}
	p, err := a.profiles.Get(profileID)
	if err != nil {
		return jira.BugDetail{}, err
	}
	token, err := a.creds.Load(profileID)
	if err != nil {
		return jira.BugDetail{}, fmt.Errorf("load credentials: %w", err)
	}
	return jira.NewClient(p.JiraURL, token, tlsOptions(p)...).GetBugDetail(a.ctx, bugKey)
}

// SyncTestCalls refreshes the "call test" relationships by re-pulling steps for
// every test currently known to call another (RND_P_4TFINT_05-207), without a
// full profile sync. It catches calls added, removed or retargeted on those
// tests. A test that only became a caller in Jira since the last full sync is
// picked up once its steps are loaded (open it) or on the next full sync, since
// call links derive from the lazily-loaded step cache. Best-effort: a per-test
// fetch failure is collected and reported but does not abort the others.
func (a *App) SyncTestCalls(profileID string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	callers, err := a.repo.DistinctTestCallers(profileID)
	if err != nil {
		return err
	}
	// Drop uncommitted local callers up front: their steps never came from Jira,
	// so re-pulling would fail or wipe them. Filtering here also makes the
	// progress Total reflect the work actually done.
	refresh := make([]string, 0, len(callers))
	for _, key := range callers {
		if isLocalTestKey(key) {
			continue
		}
		refresh = append(refresh, key)
	}
	if len(refresh) == 0 {
		return nil
	}
	p, err := a.profiles.Get(profileID)
	if err != nil {
		return err
	}
	token, err := a.creds.Load(profileID)
	if err != nil {
		return fmt.Errorf("load credentials: %w", err)
	}
	client := jira.NewClient(p.JiraURL, token, tlsOptions(p)...)

	// Per-caller progress on a dedicated channel (not the global "sync:progress")
	// so the Test Calls view shows its own bar without touching the footer sync
	// bar. A deferred terminal event clears the bar however the loop exits.
	n := len(refresh)
	defer runtime.EventsEmit(a.ctx, "testcalls:progress", syncer.Progress{Done: true})

	var firstErr error
	for i, key := range refresh {
		runtime.EventsEmit(a.ctx, "testcalls:progress", syncer.Progress{
			Stage:   "Refreshing test calls",
			Fetched: i + 1,
			Total:   n,
		})
		remote, err := client.GetTestSteps(a.ctx, key)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("refresh %s: %w", key, err)
			}
			continue
		}
		steps := make([]testrepo.Step, len(remote))
		for i, s := range remote {
			steps[i] = testrepo.Step{
				XrayID:        s.ID,
				Index:         s.Index,
				Action:        s.Action,
				Data:          s.Data,
				Expected:      s.Expected,
				CalledTestKey: s.CalledTestKey,
			}
		}
		if err := a.repo.SetTestSteps(profileID, key, steps); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("cache %s: %w", key, err)
			}
		}
	}
	return firstErr
}

// SyncProfileFull forces a full re-sync, ignoring the stored watermark. Use it
// to re-pull data the incremental path skips — notably the Test Repository
// folder membership walk, which only runs on a full sync (it is one Jira call
// per folder, too costly to repeat on every routine resync).
func (a *App) SyncProfileFull(profileID string) error {
	return a.runSync(profileID, true)
}

// runSync is the shared sync path behind SyncProfile (incremental) and
// SyncProfileFull (forced full). forceFull blanks the watermark so the engine
// treats the run as a full pull.
func (a *App) runSync(profileID string, forceFull bool) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	p, err := a.profiles.Get(profileID)
	if err != nil {
		return err
	}
	token, err := a.creds.Load(profileID)
	if err != nil {
		return fmt.Errorf("load credentials: %w", err)
	}
	state, err := a.repo.GetSyncState(profileID)
	if err != nil {
		return fmt.Errorf("read sync state: %w", err)
	}
	since := state.LastSyncedAt
	if forceFull {
		since = ""
	}
	engine := syncer.New(jira.NewClient(p.JiraURL, token, tlsOptions(p)...), a.repo)
	started := time.Now().UTC()
	var lastFetched int
	syncErr := engine.Sync(a.ctx, profileID, p.ProjectKey, p.ScopeJQL, since, func(pr syncer.Progress) {
		lastFetched = pr.Fetched
		runtime.EventsEmit(a.ctx, "sync:progress", pr)
	})

	// Record the run in the sync history (FR-1.7). A logging failure must not
	// mask the sync result.
	outcome, errMsg := "success", ""
	if syncErr != nil {
		outcome, errMsg = "error", syncErr.Error()
	}
	if logErr := a.repo.RecordSyncLog(
		profileID, started.Format(time.RFC3339),
		time.Now().UTC().Format(time.RFC3339), outcome, lastFetched, errMsg,
	); logErr != nil {
		log.Printf("xtm: record sync log: %v", logErr)
	}
	return syncErr
}

// ListSyncLog returns a profile's recent sync runs with success / failure
// detail (FR-1.7).
func (a *App) ListSyncLog(profileID string, limit int) ([]testrepo.SyncLogEntry, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.ListSyncLog(profileID, limit)
}

// GetSyncState reports when a profile last synced and how many Tests it holds.
func (a *App) GetSyncState(profileID string) (testrepo.SyncState, error) {
	if err := a.requireStore(); err != nil {
		return testrepo.SyncState{}, err
	}
	return a.repo.GetSyncState(profileID)
}

// --- Test Repository (FR-13) ---

// ListFolders returns the synced Test Repository folder tree for a profile.
func (a *App) ListFolders(profileID string) ([]testrepo.Folder, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.ListFolders(profileID)
}

// CreateFolder adds a Test Repository folder under parentPath ("" = top level)
// and queues it for creation on commit (FR-13.3).
func (a *App) CreateFolder(profileID, parentPath, name string) (testrepo.Folder, error) {
	if err := a.requireStore(); err != nil {
		return testrepo.Folder{}, err
	}
	return a.repo.CreateFolder(profileID, parentPath, name)
}

// RenameFolder renames a folder (cascading to descendants and their Tests) and
// queues the change for commit (FR-13.3).
func (a *App) RenameFolder(profileID, path, newName string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.RenameFolder(profileID, path, newName)
}

// DeleteFolder removes an empty folder and queues the deletion for commit
// (FR-13.3).
func (a *App) DeleteFolder(profileID, path string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.DeleteFolder(profileID, path)
}

// GetTestPreconditions returns the Preconditions linked to a Test.
func (a *App) GetTestPreconditions(profileID, testKey string) ([]testrepo.Precondition, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.ListTestPreconditions(profileID, testKey)
}

// GetTestContainers returns the Test Sets, Test Plans and Test Executions a
// Test belongs to (FR-1.3), each with the Test's run status for execution
// memberships.
func (a *App) GetTestContainers(profileID, testKey string) ([]testrepo.ContainerMembership, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.ListContainersForTest(profileID, testKey)
}

// --- Precondition associations (FR-13.5 / 13.6) ---

// ListAllPreconditions returns every cached Precondition for a profile — the
// master list the association pickers use.
func (a *App) ListAllPreconditions(profileID string) ([]testrepo.Precondition, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.ListAllPreconditions(profileID)
}

// SetTestPreconditions replaces a Test's Precondition associations with the
// given set and queues the change for commit (FR-13.5).
func (a *App) SetTestPreconditions(profileID, testKey string, precondKeys []string) (err error) {
	defer recoverToError("SetTestPreconditions", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.SetTestPreconditions(profileID, testKey, precondKeys)
}

// EditPreconditionField applies a local edit to a Precondition's summary or
// description and queues it for commit (FR-13.5).
func (a *App) EditPreconditionField(profileID, preconditionKey, field, newValue string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.EditPreconditionField(profileID, preconditionKey, field, newValue)
}

// CreatePrecondition creates a new Precondition locally and queues it for
// creation in Jira on commit (FR-13.5), returning its temporary key so the
// caller can associate it immediately. Project key comes from the profile.
func (a *App) CreatePrecondition(profileID, summary string) (string, error) {
	if err := a.requireStore(); err != nil {
		return "", err
	}
	p, err := a.profiles.Get(profileID)
	if err != nil {
		return "", err
	}
	return a.repo.CreatePrecondition(profileID, p.ProjectKey, summary, "", "")
}

// BulkAssociatePreconditions adds (add=true) or removes (add=false) the given
// Preconditions across a batch of Tests (FR-13.6).
func (a *App) BulkAssociatePreconditions(profileID string, testKeys, precondKeys []string, add bool) (result testrepo.BulkEditResult, err error) {
	defer recoverToError("BulkAssociatePreconditions", &err)
	empty := testrepo.BulkEditResult{Succeeded: []string{}, Failed: []testrepo.BulkFailure{}}
	if err := a.requireStore(); err != nil {
		return empty, err
	}
	return a.repo.BulkAssociatePreconditions(profileID, testKeys, precondKeys, add)
}

// BulkReplacePreconditions swaps Preconditions across a batch of Tests: per Test
// it removes toRemove and adds toAdd in one apply (FR-13.6).
func (a *App) BulkReplacePreconditions(profileID string, testKeys, toRemove, toAdd []string) (result testrepo.BulkEditResult, err error) {
	defer recoverToError("BulkReplacePreconditions", &err)
	empty := testrepo.BulkEditResult{Succeeded: []string{}, Failed: []testrepo.BulkFailure{}}
	if err := a.requireStore(); err != nil {
		return empty, err
	}
	return a.repo.BulkReplacePreconditions(profileID, testKeys, toRemove, toAdd)
}

// --- Precondition management view (FR-13.4) ---

// ListPreconditionsWithUsage returns every cached Precondition with the count
// of Tests referencing it — the rows the dedicated management view lists.
func (a *App) ListPreconditionsWithUsage(profileID string) ([]testrepo.PreconditionUsage, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.ListPreconditionsWithUsage(profileID)
}

// ListTestsForPrecondition returns the Tests linked to one Precondition, each
// with its summary and status, for the management view's detail pane.
func (a *App) ListTestsForPrecondition(profileID, preconditionKey string) ([]testrepo.PreconditionTest, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.ListTestsForPrecondition(profileID, preconditionKey)
}

// CreatePreconditionDetailed creates a new Precondition locally with a type and
// description and queues it for creation in Jira on commit (FR-13.4 / 13.5),
// returning its temporary key. Project key comes from the profile.
func (a *App) CreatePreconditionDetailed(profileID, summary, ptype, description string) (string, error) {
	if err := a.requireStore(); err != nil {
		return "", err
	}
	p, err := a.profiles.Get(profileID)
	if err != nil {
		return "", err
	}
	return a.repo.CreatePrecondition(profileID, p.ProjectKey, summary, ptype, description)
}

// DeletePrecondition removes a Precondition and its Test links and queues the
// deletion for commit (FR-13.4).
func (a *App) DeletePrecondition(profileID, preconditionKey string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.DeletePrecondition(profileID, preconditionKey)
}

// --- Requirements & coverage ---

// ListRequirementsWithCoverage returns every cached requirement with a derived
// coverage status from its covering Tests' run results.
func (a *App) ListRequirementsWithCoverage(profileID string) ([]testrepo.RequirementCoverage, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.ListRequirementsWithCoverage(profileID)
}

// GetRequirementTraceability builds the requirement sign-off traceability flow:
// requirement coverage -> covering Test run result -> Test review sign-off.
// reqFilter (a requirement key, or "" for all) narrows the flow to one
// requirement, which then appears as a labelled first node.
func (a *App) GetRequirementTraceability(profileID string, reqFilters []string) (testrepo.Sankey, error) {
	if err := a.requireStore(); err != nil {
		return testrepo.Sankey{Nodes: []testrepo.SankeyNode{}, Links: []testrepo.SankeyLink{}}, err
	}
	return a.repo.GetRequirementTraceability(profileID, reqFilters)
}

// ListTestsForRequirement returns the Tests covering a requirement with each
// Test's consolidated run status.
func (a *App) ListTestsForRequirement(profileID, requirementKey string) ([]testrepo.RequirementTest, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.ListTestsForRequirement(profileID, requirementKey)
}

// GetTestRequirements returns the requirements a Test covers (for the detail
// panel).
func (a *App) GetTestRequirements(profileID, testKey string) ([]testrepo.Requirement, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.GetTestRequirements(profileID, testKey)
}

// SetTestRequirements replaces the set of requirements a Test covers and queues
// the link changes for commit (FR-13 traceability).
func (a *App) SetTestRequirements(profileID, testKey string, requirementKeys []string) (err error) {
	defer recoverToError("SetTestRequirements", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.SetTestRequirements(profileID, testKey, requirementKeys)
}

// BulkAssociateRequirements adds or removes requirement links across many Tests
// at once.
func (a *App) BulkAssociateRequirements(profileID string, testKeys, requirementKeys []string, add bool) (result testrepo.BulkEditResult, err error) {
	defer recoverToError("BulkAssociateRequirements", &err)
	empty := testrepo.BulkEditResult{Succeeded: []string{}, Failed: []testrepo.BulkFailure{}}
	if err := a.requireStore(); err != nil {
		return empty, err
	}
	return a.repo.BulkAssociateRequirements(profileID, testKeys, requirementKeys, add)
}

// BulkReplaceRequirements swaps requirement links across a batch of Tests: per
// Test it removes toRemove and adds toAdd in one apply.
func (a *App) BulkReplaceRequirements(profileID string, testKeys, toRemove, toAdd []string) (result testrepo.BulkEditResult, err error) {
	defer recoverToError("BulkReplaceRequirements", &err)
	empty := testrepo.BulkEditResult{Succeeded: []string{}, Failed: []testrepo.BulkFailure{}}
	if err := a.requireStore(); err != nil {
		return empty, err
	}
	return a.repo.BulkReplaceRequirements(profileID, testKeys, toRemove, toAdd)
}

// EditRequirementField applies a local edit to a requirement field (summary)
// and queues a pending change for commit. The requirement may live in another
// project; the edit lands there at commit time.
func (a *App) EditRequirementField(profileID, requirementKey, field, newValue string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.EditRequirementField(profileID, requirementKey, field, newValue)
}

// DeleteRequirement removes a requirement and its coverage links locally and
// queues the deletion for commit (a permission-sensitive, often cross-project
// Jira issue delete).
func (a *App) DeleteRequirement(profileID, requirementKey string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.DeleteRequirement(profileID, requirementKey)
}

// ListRequirementSources returns the configured requirement sources for a
// profile.
func (a *App) ListRequirementSources(profileID string) ([]testrepo.RequirementSource, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.ListRequirementSources(profileID)
}

// SetRequirementSource adds or updates a requirement source (a project to browse
// requirements from). Takes effect on the next sync.
func (a *App) SetRequirementSource(profileID, projectKey, issueTypes, scopeJQL string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.SetRequirementSource(profileID, projectKey, issueTypes, scopeJQL)
}

// RemoveRequirementSource deletes a requirement source.
func (a *App) RemoveRequirementSource(profileID, projectKey string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.RemoveRequirementSource(profileID, projectKey)
}

// GetRequirementLinks returns the outbound Requirement->Requirement links for a
// requirement (e.g. "requires" links). Keyed by fromKey; all link types are
// returned.
func (a *App) GetRequirementLinks(profileID, requirementKey string) ([]testrepo.ReqReqLink, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.GetRequirementLinks(profileID, requirementKey)
}

// SetRequirementLinks replaces this requirement's outbound links of the given
// linkType (e.g. "requires") to the supplied target keys and queues the change
// for commit. The local store is updated immediately; Jira is updated on the
// next commit (FR-13 traceability).
func (a *App) SetRequirementLinks(profileID, requirementKey, linkType string, linkedKeys []string) (err error) {
	defer recoverToError("SetRequirementLinks", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.SetRequirementLinks(profileID, requirementKey, linkType, linkedKeys)
}

// --- Bug (defect) tracking ---

// GetBugCreateFields returns the required fields for the profile's bug issue
// type's create screen (beyond project/issuetype/summary/description/priority/
// labels), so the Create Bug form can render and collect them before the commit.
// The target project is resolved the same way CreateBugForTest does (profile
// bug-project mode), with an empty execKey (the project key is available from
// the profile before any specific execution is known).
func (a *App) GetBugCreateFields(profileID string) (fields []jira.BugCreateField, err error) {
	defer recoverToError("GetBugCreateFields", &err)
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	p, err := a.profiles.Get(profileID)
	if err != nil {
		return nil, err
	}
	token, err := a.creds.Load(profileID)
	if err != nil {
		return nil, fmt.Errorf("load credentials: %w", err)
	}
	projKey := bugProjectKey(p, "")
	return jira.NewClient(p.JiraURL, token, tlsOptions(p)...).GetBugCreateFields(a.ctx, projKey, p.BugIssueType)
}

// CreateBugForTest queues a new Bug issue linked to a failed Test, committed to
// Jira on the next sync. The bug's project and issue type come from the profile.
// extraFields carries any additional field values collected from the
// createmeta-driven Create Bug form (keyed by Jira field id, values already
// shaped for the POST body). Returns the placeholder key.
func (a *App) CreateBugForTest(profileID, testKey, execKey, summary, description, priority string, labels []string, extraFields map[string]any) (key string, err error) {
	defer recoverToError("CreateBugForTest", &err)
	if err := a.requireStore(); err != nil {
		return "", err
	}
	p, err := a.profiles.Get(profileID)
	if err != nil {
		return "", err
	}
	return a.repo.CreateBugForTest(profileID, testKey, execKey, testrepo.BugDraft{
		ProjectKey: bugProjectKey(p, execKey), IssueType: p.BugIssueType, Summary: summary,
		Description: description, Priority: priority, Labels: labels, Fields: extraFields,
	})
}

// bugProjectKey resolves which Jira project a filed defect lands in, from the
// profile's bug-project mode: the test's project (default), the Test
// Execution's project (parsed from execKey), or a dedicated key. Each falls
// back to the profile project so a bug always has a project.
func bugProjectKey(p profile.Profile, execKey string) string {
	switch p.BugProjectMode {
	case "execution":
		if proj := projectOfKey(execKey); proj != "" {
			return proj
		}
	case "dedicated":
		if k := strings.TrimSpace(p.BugProjectKey); k != "" {
			return k
		}
	}
	return p.ProjectKey
}

// projectOfKey returns the Jira project key from an issue key ("PROJ-123" ->
// "PROJ", "XRAYINT-TE-1" -> "XRAYINT"): the text before the first hyphen.
func projectOfKey(key string) string {
	if i := strings.IndexByte(key, '-'); i > 0 {
		return key[:i]
	}
	return ""
}

// ListBugsWithTests returns every cached bug with the Tests it affects.
func (a *App) ListBugsWithTests(profileID string) ([]testrepo.BugWithTests, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.ListBugsWithTests(profileID)
}

// GetTestBugs returns the bugs linked to a Test (for the detail section).
func (a *App) GetTestBugs(profileID, testKey string) ([]testrepo.TestBug, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.GetTestBugs(profileID, testKey)
}

// ListTestsForBug returns the Tests a bug affects, each with its consolidated
// run status (for the bug detail pane).
func (a *App) ListTestsForBug(profileID, bugKey string) ([]testrepo.BugTest, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.ListTestsForBug(profileID, bugKey)
}

// ListBugsForContainer returns the bugs reached through any member Test of a
// container (an execution's related defects), including bugs reached only via a
// cross-project member Test (#219).
func (a *App) ListBugsForContainer(profileID, containerKey string) ([]testrepo.Bug, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.ListBugsForContainer(profileID, containerKey)
}

// --- Test run history & rollups ---

// GetTestRunHistory returns a test's run history across executions. Before
// reading from the local store it triggers a best-effort cross-project
// execution discovery for real (non-local) test keys so that executions in
// other projects, which the project-scoped sync misses, appear in the history
// without requiring a full re-sync.
func (a *App) GetTestRunHistory(profileID, testKey string) ([]testrepo.TestRunEntry, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	if !isLocalTestKey(testKey) {
		a.refreshCrossProjectExecsForTest(profileID, testKey)
	}
	return a.repo.GetTestRunHistory(profileID, testKey)
}

// refreshCrossProjectExecsForTest performs a lightweight per-test cross-project
// execution discovery: it looks up every Test Execution that testKey belongs to
// (in any project) and additively merges newly found containers, links, and runs
// into the local store. This is called lazily when GetTestRunHistory is invoked
// so that cross-project sub-task executions (which the project-scoped sync
// misses) appear in the run history without requiring a full re-sync.
//
// Errors are logged and ignored -- this is best-effort; the caller returns
// whatever the local store already has.
func (a *App) refreshCrossProjectExecsForTest(profileID, testKey string) {
	p, err := a.profiles.Get(profileID)
	if err != nil {
		log.Printf("xtm: refreshCrossProjectExecsForTest: load profile: %v", err)
		return
	}
	token, err := a.creds.Load(profileID)
	if err != nil {
		log.Printf("xtm: refreshCrossProjectExecsForTest: load credentials: %v", err)
		return
	}
	cl := jira.NewClient(p.JiraURL, token, tlsOptions(p)...)
	ctx := context.Background()
	containers, links, err := cl.TestExecutionsForTest(ctx, testKey)
	if err != nil {
		log.Printf("xtm: refreshCrossProjectExecsForTest: %s: %v", testKey, err)
		return
	}
	if len(containers) == 0 {
		return
	}
	// Defensive: containers and links must be parallel slices (same length,
	// same index). TestExecutionsForTest guarantees this, but guard here to
	// prevent an out-of-bounds panic if that contract is ever relaxed.
	if len(containers) != len(links) {
		log.Printf("xtm: refreshCrossProjectExecsForTest: %s: container/link length mismatch (%d vs %d), skipping",
			testKey, len(containers), len(links))
		return
	}
	// Check which containers are already in the local store so we do not
	// overwrite links that the project-scoped sync already wrote correctly.
	knownKeys, err := a.repo.AllContainerKeys(profileID)
	if err != nil {
		log.Printf("xtm: refreshCrossProjectExecsForTest: list known keys: %v", err)
		return
	}

	var newContainers []testrepo.Container
	var newLinks []testrepo.ContainerLink
	for i, c := range containers {
		if knownKeys[c.Key] {
			continue
		}
		newContainers = append(newContainers, testrepo.Container{
			Key:           c.Key,
			Kind:          c.Kind,
			Summary:       c.Summary,
			Status:        c.Status,
			ParentKey:     c.ParentKey,
			ParentSummary: c.ParentSummary,
			IssueType:     c.IssueType,
			Environments:  c.Environments,
			FixVersions:   c.FixVersions,
			Created:       c.Created,
			Updated:       c.Updated,
			Resolved:      c.Resolved,
			Description:   c.Description,
		})
		newLinks = append(newLinks, testrepo.ContainerLink{
			ContainerKey: links[i].ContainerKey,
			TestKey:      links[i].TestKey,
			RunStatus:    links[i].RunStatus,
		})
	}
	if len(newContainers) == 0 {
		return
	}
	if err := a.repo.UpsertContainers(profileID, newContainers); err != nil {
		log.Printf("xtm: refreshCrossProjectExecsForTest: upsert containers: %v", err)
		return
	}
	if err := a.repo.UpsertContainerLinks(profileID, newLinks); err != nil {
		log.Printf("xtm: refreshCrossProjectExecsForTest: upsert links: %v", err)
		return
	}
	for _, ct := range newContainers {
		runs, rErr := cl.GetTestRuns(ctx, ct.Key)
		if rErr != nil {
			log.Printf("xtm: refreshCrossProjectExecsForTest: get runs for %s: %v", ct.Key, rErr)
			continue
		}
		rows := make([]testrepo.TestRunRow, 0, len(runs))
		for _, tr := range runs {
			defectsJSON := "[]"
			if len(tr.Defects) > 0 {
				if b, jerr := json.Marshal(tr.Defects); jerr == nil {
					defectsJSON = string(b)
				}
			}
			env := tr.Environment
			if env == "" && len(ct.Environments) > 0 {
				env = strings.Join(ct.Environments, ", ")
			}
			rows = append(rows, testrepo.TestRunRow{
				ExecKey:     ct.Key,
				TestKey:     tr.TestKey,
				RunStatus:   tr.Status,
				StartedAt:   tr.StartedAt,
				FinishedAt:  tr.FinishedAt,
				ExecutedBy:  tr.ExecutedBy,
				Environment: env,
				Defects:     defectsJSON,
				CreatedAt:   tr.CreatedAt,
				UpdatedAt:   tr.UpdatedAt,
			})
		}
		_ = a.repo.ReplaceRunsForExec(profileID, ct.Key, rows)
	}
}

// GetRunRollup returns the run-result roll-up for a Test Plan or Test Set.
func (a *App) GetRunRollup(profileID, containerKey string) (testrepo.RunRollup, error) {
	if err := a.requireStore(); err != nil {
		return testrepo.RunRollup{}, err
	}
	return a.repo.GetRunRollup(profileID, containerKey)
}

// GetExecutionMembersWithRuns returns an execution's member tests with run details.
func (a *App) GetExecutionMembersWithRuns(profileID, execKey string) ([]testrepo.ExecMemberRun, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.GetExecutionMembersWithRuns(profileID, execKey)
}

// --- Local editing & change tracking (FR-2 / FR-1.5 / FR-12.6) ---

// EditTestField applies a local edit to a Test field and queues a pending
// change for commit. Editable fields: summary, description, priority, labels.
// Repeated edits to the same field are coalesced; reverting to the original
// value drops the pending change.
func (a *App) EditTestField(profileID, testKey, field, newValue string) (err error) {
	defer recoverToError("EditTestField", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.EditTestField(profileID, testKey, field, newValue)
}

// DiscardPendingChange reverts a queued change and removes it from the
// pending list.
func (a *App) DiscardPendingChange(profileID string, changeID int64) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.DiscardPendingChange(profileID, changeID)
}

// DiscardAllPendingChanges reverts every queued change for a profile and clears
// the pending list — the "Discard all" action. Returns how many were discarded.
func (a *App) DiscardAllPendingChanges(profileID string) (int, error) {
	if err := a.requireStore(); err != nil {
		return 0, err
	}
	return a.repo.DiscardAllPendingChanges(profileID)
}

// ResolveConflictOverride re-bases a Test's pending changes onto the remote
// version so the next commit overrides the remote change (FR-1.4).
func (a *App) ResolveConflictOverride(profileID, testKey, remoteVersion string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.RebaseTestConflict(profileID, testKey, remoteVersion)
}

// ResolveConflictKeepRemote discards a Test's pending changes, keeping the
// remote version (FR-1.4).
func (a *App) ResolveConflictKeepRemote(profileID, testKey string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.DiscardTestChanges(profileID, testKey)
}

// ResolveConflictMerge applies per-field conflict decisions (keep mine / keep
// theirs) for a Test, then re-bases the remaining changes so a re-commit
// succeeds (FR-1.4, conflict management). decisions come from the resolution
// modal built off CommitResult.Conflicted[].Fields.
func (a *App) ResolveConflictMerge(profileID, testKey, remoteVersion string, decisions []testrepo.ConflictDecision) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.ResolveConflictMerge(profileID, testKey, remoteVersion, decisions)
}

// RecreateDeletedTest converts a remotely-deleted Test's held local edits into a
// brand-new local Test (FR-1.4 remote-delete recreate). Returns the new "NEW-N"
// key so the frontend can re-point an open detail view.
func (a *App) RecreateDeletedTest(profileID, testKey string) (string, error) {
	if err := a.requireStore(); err != nil {
		return "", err
	}
	return a.repo.RecreateDeletedTest(profileID, testKey)
}

// ListPendingChanges returns all uncommitted local edits for a profile.
func (a *App) ListPendingChanges(profileID string) ([]testrepo.PendingChange, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.ListPendingChanges(profileID)
}

// ListAuditEntries returns the most recent audit log entries for a profile
// (newest first). Defaults to 200 entries.
func (a *App) ListAuditEntries(profileID string, limit int) ([]testrepo.AuditEntry, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.ListAuditEntries(profileID, limit)
}

// CommitPendingChanges pushes a profile's local edits to Jira (FR-1.5).
// Returns a per-Test result describing what succeeded and what failed —
// failed entries stay in the local pending list so the user can retry or
// discard them.
func (a *App) CommitPendingChanges(profileID string) (out syncer.CommitResult, err error) {
	defer recoverToError("CommitPendingChanges", &err)
	empty := syncer.CommitResult{
		Succeeded:  []string{},
		Conflicted: []syncer.Conflict{},
		Failed:     []syncer.FailedCommit{},
	}
	if err := a.requireStore(); err != nil {
		return empty, err
	}
	p, err := a.profiles.Get(profileID)
	if err != nil {
		return empty, err
	}
	token, err := a.creds.Load(profileID)
	if err != nil {
		return empty, fmt.Errorf("load credentials: %w", err)
	}
	s, settingsErr := a.settings.Get()
	if settingsErr != nil {
		return empty, fmt.Errorf("read settings: %w", settingsErr)
	}
	client := jira.NewClient(p.JiraURL, token, tlsOptions(p)...)
	client.SetRequirementLinkType(s.RequirementLinkType)
	engine := syncer.New(client, a.repo)
	return engine.CommitChanges(a.ctx, profileID, p.ProjectKey)
}

// CommitPendingChangesByIDs pushes only the selected pending changes to Jira
// (selective commit). The frontend passes all of one item's change ids together
// (e.g. every row of a single Test) so a partial push doesn't strand sibling
// edits against an advanced remote version.
func (a *App) CommitPendingChangesByIDs(profileID string, changeIDs []int64) (out syncer.CommitResult, err error) {
	defer recoverToError("CommitPendingChangesByIDs", &err)
	empty := syncer.CommitResult{
		Succeeded:  []string{},
		Conflicted: []syncer.Conflict{},
		Failed:     []syncer.FailedCommit{},
	}
	if err := a.requireStore(); err != nil {
		return empty, err
	}
	if len(changeIDs) == 0 {
		return empty, nil
	}
	p, err := a.profiles.Get(profileID)
	if err != nil {
		return empty, err
	}
	token, err := a.creds.Load(profileID)
	if err != nil {
		return empty, fmt.Errorf("load credentials: %w", err)
	}
	s, settingsErr := a.settings.Get()
	if settingsErr != nil {
		return empty, fmt.Errorf("read settings: %w", settingsErr)
	}
	client := jira.NewClient(p.JiraURL, token, tlsOptions(p)...)
	client.SetRequirementLinkType(s.RequirementLinkType)
	engine := syncer.New(client, a.repo)
	return engine.CommitChangesForIDs(a.ctx, profileID, p.ProjectKey, changeIDs)
}

// --- Workflow transitions (FR-4.2) ---

// GetTestTransitions returns the workflow transitions available from a
// Test's current local status — used by the detail UI to populate the
// "Move to…" picker. Behind the scenes this reads the Test's current
// status from the local store and asks Jira (or the demo generator) what
// is reachable from there.
func (a *App) GetTestTransitions(profileID, testKey string) ([]jira.Transition, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	if isLocalTestKey(testKey) {
		// Uncommitted local Test — no Jira issue yet, so no transitions.
		return []jira.Transition{}, nil
	}
	test, err := a.repo.GetTest(profileID, testKey)
	if err != nil {
		return nil, err
	}
	p, err := a.profiles.Get(profileID)
	if err != nil {
		return nil, err
	}
	token, err := a.creds.Load(profileID)
	if err != nil {
		return nil, fmt.Errorf("load credentials: %w", err)
	}
	return jira.NewClient(p.JiraURL, token, tlsOptions(p)...).GetTransitions(a.ctx, testKey, test.Status)
}

// TransitionTest queues a workflow transition locally (FR-4.2). The change
// is pushed to Jira on commit via POST /rest/api/2/issue/{key}/transitions.
func (a *App) TransitionTest(profileID, testKey, targetStatus string) (err error) {
	defer recoverToError("TransitionTest", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.TransitionTest(profileID, testKey, targetStatus)
}

// EditTestStepField queues a local edit to one field of one Test Step
// (FR-2.5). The change is pushed to Xray on commit via PUT
// /rest/raven/2.0/api/test/{key}/steps/{stepId}.
func (a *App) EditTestStepField(profileID, testKey, xrayID, field, newValue string) (err error) {
	defer recoverToError("EditTestStepField", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.EditTestStepField(profileID, testKey, xrayID, field, newValue)
}

// DeleteTestStep queues a Test Step for deletion (FR-2.5). The step is
// hidden locally and the DELETE call to Xray fires at commit time.
func (a *App) DeleteTestStep(profileID, testKey, xrayID string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.DeleteTestStep(profileID, testKey, xrayID)
}

// AddTestStep appends a new Step to a Test locally and queues it for creation
// in Xray on commit (FR-2.5). The returned Step carries a temporary xray_id
// the commit path swaps for the real one once Xray assigns it.
func (a *App) AddTestStep(profileID, testKey, action, data, expected string) (testrepo.Step, error) {
	if err := a.requireStore(); err != nil {
		return testrepo.Step{}, err
	}
	return a.repo.AddTestStep(profileID, testKey, action, data, expected)
}

// AddCalledTestStep appends a "call test" step — a step that invokes another
// Test (Xray test call) — to a Test, queued for commit (FR-2.5, #2).
func (a *App) AddCalledTestStep(profileID, testKey, calledTestKey string) (testrepo.Step, error) {
	if err := a.requireStore(); err != nil {
		return testrepo.Step{}, err
	}
	return a.repo.AddCalledTestStep(profileID, testKey, calledTestKey)
}

// ListTestCallLinks returns every "call test" relationship in the cache —
// which tests call which — for the Test Calls view and grid cue (#2 follow-up).
func (a *App) ListTestCallLinks(profileID string) ([]testrepo.TestCallLink, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.ListTestCallLinks(profileID)
}

// CloneTestSteps appends Steps of sourceKey onto targetKey, queuing each as a
// local step-add for commit (FR-2.5) — a quick way to seed a Test from an
// existing one. stepIDs selects which source steps to copy (a selective clone);
// an empty stepIDs clones them all. The source's steps are loaded lazily
// (fetched from Jira on a cache miss) so cloning works even before the source's
// detail panel has been opened. Returns the target's full step list afterward.
func (a *App) CloneTestSteps(profileID, targetKey, sourceKey string, stepIDs []string) ([]testrepo.Step, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(sourceKey) == "" || sourceKey == targetKey {
		return nil, fmt.Errorf("pick a different test to clone steps from")
	}
	sourceSteps, err := a.GetTestSteps(profileID, sourceKey, false)
	if err != nil {
		return nil, fmt.Errorf("load steps from %s: %w", sourceKey, err)
	}
	// A non-empty stepIDs narrows the clone to the chosen steps, preserving their
	// order in the source; empty means clone everything.
	if len(stepIDs) > 0 {
		want := make(map[string]bool, len(stepIDs))
		for _, id := range stepIDs {
			want[id] = true
		}
		filtered := make([]testrepo.Step, 0, len(stepIDs))
		for _, s := range sourceSteps {
			if want[s.XrayID] {
				filtered = append(filtered, s)
			}
		}
		sourceSteps = filtered
	}
	if len(sourceSteps) == 0 {
		return nil, fmt.Errorf("no steps selected to clone from %s", sourceKey)
	}
	return a.repo.CloneTestSteps(profileID, targetKey, sourceSteps)
}

// ReorderTestSteps records a new ordering for a Test's steps (FR-2.5). The
// orderedXrayIDs slice must be exactly the current step set, permuted; the new
// positions are pushed to Xray on commit.
func (a *App) ReorderTestSteps(profileID, testKey string, orderedXrayIDs []string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.ReorderTestSteps(profileID, testKey, orderedXrayIDs)
}

// --- Test steps (FR-2.5) ---

// GetTestSteps returns the cached Steps for a Test, fetching from Xray on
// a cache miss. forceRefresh bypasses the cache so the user can pull the
// latest steps without a full sync — handy after someone else edits in
// Jira directly.
//
// We don't refresh during the bulk sync flow: that would mean one extra
// HTTP call per Test (5,000 round trips for a typical demo dataset). The
// detail panel calls this lazily on first open instead.
func (a *App) GetTestSteps(profileID, testKey string, forceRefresh bool) ([]testrepo.Step, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	if isLocalTestKey(testKey) {
		// Uncommitted local Test — its steps live only in the local store; never
		// fetch from Jira (the placeholder key would 400).
		return a.repo.ListTestSteps(profileID, testKey)
	}
	if !forceRefresh {
		cached, err := a.repo.ListTestSteps(profileID, testKey)
		if err != nil {
			return nil, err
		}
		if len(cached) > 0 {
			return cached, nil
		}
	}

	p, err := a.profiles.Get(profileID)
	if err != nil {
		return nil, err
	}
	token, err := a.creds.Load(profileID)
	if err != nil {
		return nil, fmt.Errorf("load credentials: %w", err)
	}
	remote, err := jira.NewClient(p.JiraURL, token, tlsOptions(p)...).GetTestSteps(a.ctx, testKey)
	if err != nil {
		return nil, err
	}

	steps := make([]testrepo.Step, len(remote))
	for i, s := range remote {
		steps[i] = testrepo.Step{
			XrayID:        s.ID,
			Index:         s.Index,
			Action:        s.Action,
			Data:          s.Data,
			Expected:      s.Expected,
			CalledTestKey: s.CalledTestKey,
		}
	}
	if err := a.repo.SetTestSteps(profileID, testKey, steps); err != nil {
		return nil, err
	}
	return steps, nil
}

// JiraStepInfo reports what Jira itself says about a Test's steps, independent
// of the local cache — used to detect the "this Test has steps in Jira but the
// tool shows none" situation before the user adds a (rejected) empty step.
type JiraStepInfo struct {
	Count    int  `json:"count"`    // number of steps Jira returned
	AllBlank bool `json:"allBlank"` // steps exist but their content didn't map (shape issue)
}

// CheckJiraTestSteps asks Jira directly how many steps a Test has (FR-2.5),
// bypassing the local cache. The detail panel calls it when its Steps panel is
// empty so it can warn that Jira actually has steps that failed to load —
// rather than letting the user add a blank step that Xray will reject.
func (a *App) CheckJiraTestSteps(profileID, testKey string) (JiraStepInfo, error) {
	if err := a.requireStore(); err != nil {
		return JiraStepInfo{}, err
	}
	if isLocalTestKey(testKey) {
		// Uncommitted local Test — nothing in Jira to compare against.
		return JiraStepInfo{}, nil
	}
	p, err := a.profiles.Get(profileID)
	if err != nil {
		return JiraStepInfo{}, err
	}
	token, err := a.creds.Load(profileID)
	if err != nil {
		return JiraStepInfo{}, fmt.Errorf("load credentials: %w", err)
	}
	remote, err := jira.NewClient(p.JiraURL, token, tlsOptions(p)...).GetTestSteps(a.ctx, testKey)
	if err != nil {
		return JiraStepInfo{}, err
	}
	info := JiraStepInfo{Count: len(remote), AllBlank: len(remote) > 0}
	for _, s := range remote {
		if s.Action != "" || s.Data != "" || s.Expected != "" {
			info.AllBlank = false
			break
		}
	}
	return info, nil
}

// --- Custom fields (FR-2.6) ---

// GetTestCustomFields returns a Test's custom fields (definition + value),
// fetching values from Jira on a cache miss. forceRefresh re-pulls from Jira.
// Definitions come from the sync; values are fetched lazily on first open, the
// same pattern as steps.
func (a *App) GetTestCustomFields(profileID, testKey string, forceRefresh bool) ([]testrepo.CustomFieldValue, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	if isLocalTestKey(testKey) {
		// Uncommitted local Test — serve whatever is cached locally; never fetch
		// from Jira (the placeholder key would 400).
		return a.repo.ListTestCustomFields(profileID, testKey)
	}
	if !forceRefresh {
		has, err := a.repo.HasCustomFieldValues(profileID, testKey)
		if err != nil {
			return nil, err
		}
		if has {
			return a.repo.ListTestCustomFields(profileID, testKey)
		}
	}

	p, err := a.profiles.Get(profileID)
	if err != nil {
		return nil, err
	}
	token, err := a.creds.Load(profileID)
	if err != nil {
		return nil, fmt.Errorf("load credentials: %w", err)
	}
	values, err := jira.NewClient(p.JiraURL, token, tlsOptions(p)...).GetTestCustomFields(a.ctx, testKey)
	if err != nil {
		return nil, err
	}
	if len(values) > 0 {
		if err := a.repo.SetTestCustomFields(profileID, testKey, values); err != nil {
			return nil, err
		}
	}
	return a.repo.ListTestCustomFields(profileID, testKey)
}

// EditTestCustomField queues a local edit to one custom field of a Test
// (FR-2.6). The change is pushed to Jira on commit as an issue field update.
func (a *App) EditTestCustomField(profileID, testKey, fieldID, newValue string) (err error) {
	defer recoverToError("EditTestCustomField", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.EditTestCustomField(profileID, testKey, fieldID, newValue)
}

// --- Bulk operations (FR-3) ---

// BulkEditTests applies a single field-level operation to a batch of Tests,
// queuing a pending change for each modified Test. The changes are then
// pushed to Jira through the existing commit flow.
func (a *App) BulkEditTests(profileID string, testKeys []string, op testrepo.BulkEdit) (result testrepo.BulkEditResult, err error) {
	defer recoverToError("BulkEditTests", &err)
	empty := testrepo.BulkEditResult{
		Succeeded: []string{},
		Failed:    []testrepo.BulkFailure{},
	}
	if err := a.requireStore(); err != nil {
		return empty, err
	}
	return a.repo.BulkEditTests(profileID, testKeys, op)
}

// BulkTransitionOptions answers two questions the bulk-transition modal
// needs before the user can confirm: how the selected Tests break down by
// current status, and which target statuses are reachable from at least
// one of those current statuses (FR-3.8).
type BulkTransitionOptions struct {
	CurrentStatusCounts map[string]int `json:"currentStatusCounts"`
	ReachableTargets    []string       `json:"reachableTargets"`
}

// BulkTransitionResult reports per-Test outcomes from a bulk workflow
// transition. Succeeded / Skipped / Failed are disjoint sets of Test keys.
// Skipped means the Test exists in the selection but no transition leads
// to the chosen target (or the Test is already at the target).
type BulkTransitionResult struct {
	Succeeded []string               `json:"succeeded"`
	Skipped   []BulkTransitionSkip   `json:"skipped"`
	Failed    []testrepo.BulkFailure `json:"failed"`
}

// BulkTransitionSkip is one Test that wasn't queued for transition with an
// explanation the UI can show next to its key.
type BulkTransitionSkip struct {
	TestKey string `json:"testKey"`
	Reason  string `json:"reason"`
}

// GetBulkTransitionOptions inspects a selection of Tests and returns the
// data the bulk-transition modal needs to render: a histogram of current
// statuses and the union of reachable target statuses across those
// statuses.
//
// To keep API calls down, we fetch transitions once per distinct current
// status. For real Jira this trades strict correctness on conditional
// transitions (where availability can vary per-issue) for far fewer round
// trips — any conditional-only transition that isn't actually reachable
// for a given Test will surface as a per-Test failure at commit time.
func (a *App) GetBulkTransitionOptions(profileID string, testKeys []string) (BulkTransitionOptions, error) {
	out := BulkTransitionOptions{
		CurrentStatusCounts: map[string]int{},
		ReachableTargets:    []string{},
	}
	if err := a.requireStore(); err != nil {
		return out, err
	}
	if len(testKeys) == 0 {
		return out, nil
	}

	// Tally current statuses; skip unreadable rows silently (the test may
	// have been removed since the user selected it).
	for _, key := range testKeys {
		test, err := a.repo.GetTest(profileID, key)
		if err != nil {
			continue
		}
		out.CurrentStatusCounts[test.Status]++
	}
	if len(out.CurrentStatusCounts) == 0 {
		return out, nil
	}

	p, err := a.profiles.Get(profileID)
	if err != nil {
		return out, err
	}
	token, err := a.creds.Load(profileID)
	if err != nil {
		return out, fmt.Errorf("load credentials: %w", err)
	}
	client := jira.NewClient(p.JiraURL, token, tlsOptions(p)...)

	// We need a representative key per status to call GetTransitions for
	// real Jira — the demo path ignores the key. Walk testKeys once and
	// snapshot the first key for each status seen.
	repByStatus := make(map[string]string, len(out.CurrentStatusCounts))
	for _, key := range testKeys {
		test, err := a.repo.GetTest(profileID, key)
		if err != nil {
			continue
		}
		if _, ok := repByStatus[test.Status]; !ok {
			repByStatus[test.Status] = key
		}
	}

	targets := make(map[string]struct{})
	for status, key := range repByStatus {
		ts, err := client.GetTransitions(a.ctx, key, status)
		if err != nil {
			return out, fmt.Errorf("fetch transitions for %q: %w", status, err)
		}
		for _, t := range ts {
			targets[t.To] = struct{}{}
		}
	}
	for t := range targets {
		out.ReachableTargets = append(out.ReachableTargets, t)
	}
	sort.Strings(out.ReachableTargets)
	return out, nil
}

// BulkTransitionTests queues a workflow transition to targetStatus for each
// selected Test where such a transition exists (FR-3.8). Tests already in
// the target status are skipped; tests with no transition to the target are
// skipped with a reason; other failures (DB / API) are surfaced per-Test
// without aborting the run.
//
// Transitions are looked up once per distinct current status and cached
// for the duration of the call — see GetBulkTransitionOptions for the
// caveat about conditional transitions.
func (a *App) BulkTransitionTests(profileID string, testKeys []string, targetStatus string) (_ BulkTransitionResult, err error) {
	defer recoverToError("BulkTransitionTests", &err)
	result := BulkTransitionResult{
		Succeeded: []string{},
		Skipped:   []BulkTransitionSkip{},
		Failed:    []testrepo.BulkFailure{},
	}
	if err := a.requireStore(); err != nil {
		return result, err
	}
	if len(testKeys) == 0 {
		return result, nil
	}
	if targetStatus == "" {
		return result, fmt.Errorf("target status is required")
	}

	p, err := a.profiles.Get(profileID)
	if err != nil {
		return result, err
	}
	token, err := a.creds.Load(profileID)
	if err != nil {
		return result, fmt.Errorf("load credentials: %w", err)
	}
	client := jira.NewClient(p.JiraURL, token, tlsOptions(p)...)

	transitionsByStatus := make(map[string][]jira.Transition)

	for _, key := range testKeys {
		test, err := a.repo.GetTest(profileID, key)
		if err != nil {
			result.Failed = append(result.Failed, testrepo.BulkFailure{
				TestKey: key, Error: err.Error(),
			})
			continue
		}
		if test.Status == targetStatus {
			result.Skipped = append(result.Skipped, BulkTransitionSkip{
				TestKey: key, Reason: "already in target status",
			})
			continue
		}
		ts, cached := transitionsByStatus[test.Status]
		if !cached {
			ts, err = client.GetTransitions(a.ctx, key, test.Status)
			if err != nil {
				result.Failed = append(result.Failed, testrepo.BulkFailure{
					TestKey: key, Error: "fetch transitions: " + err.Error(),
				})
				continue
			}
			transitionsByStatus[test.Status] = ts
		}
		reachable := false
		for _, t := range ts {
			if t.To == targetStatus {
				reachable = true
				break
			}
		}
		if !reachable {
			result.Skipped = append(result.Skipped, BulkTransitionSkip{
				TestKey: key,
				Reason:  fmt.Sprintf("no transition to %q from %q", targetStatus, test.Status),
			})
			continue
		}
		if err := a.repo.TransitionTest(profileID, key, targetStatus); err != nil {
			result.Failed = append(result.Failed, testrepo.BulkFailure{
				TestKey: key, Error: err.Error(),
			})
			continue
		}
		result.Succeeded = append(result.Succeeded, key)
	}
	return result, nil
}

// SeedSampleContainers populates the local store with sample Test Sets, Test
// Plans and Test Executions linked to the profile's synced Tests, so the
// board / grouping / coverage features can be exercised before the real Xray
// container endpoints are wired. Project key comes from the active profile.
func (a *App) SeedSampleContainers(profileID string) (testrepo.SeedResult, error) {
	if err := a.requireStore(); err != nil {
		return testrepo.SeedResult{}, err
	}
	p, err := a.profiles.Get(profileID)
	if err != nil {
		return testrepo.SeedResult{}, err
	}
	return a.repo.SeedSampleContainers(profileID, p.ProjectKey)
}

// CleanSampleData removes the sample Test Sets / Plans / Executions previously
// created by SeedSampleContainers for this profile's project (FR-5), so the
// project can start fresh without affecting real synced data. Returns the number
// of sample containers removed.
func (a *App) CleanSampleData(profileID string) (int, error) {
	if err := a.requireStore(); err != nil {
		return 0, err
	}
	p, err := a.profiles.Get(profileID)
	if err != nil {
		return 0, err
	}
	return a.repo.CleanSampleContainers(profileID, p.ProjectKey)
}

// GetTraceabilitySankey returns the Plan -> Execution -> run-status traceability
// flow for the dashboard (FR-9), optionally narrowed to one Test Plan and/or one
// Test Execution (pass "" for either to include all).
func (a *App) GetTraceabilitySankey(profileID string, planFilters, execFilters []string, crossProjectOnly bool) (testrepo.Sankey, error) {
	if err := a.requireStore(); err != nil {
		return testrepo.Sankey{}, err
	}
	// The cross-project filter compares each Execution's project key against the
	// active profile's project.
	projectKey := ""
	if p, err := a.profiles.Get(profileID); err == nil {
		projectKey = p.ProjectKey
	}
	return a.repo.GetTraceabilitySankey(profileID, projectKey, planFilters, execFilters, crossProjectOnly)
}

// GetSubTaskTraceability returns the Parent -> Execution -> run-status flow over
// sub-task Test Executions (FR-9). parentFilters narrows to chosen parent
// issues; empty includes all. crossProject controls whether members that live
// only in another project (cached in external_test, absent from test_case) are
// drawn (true) or excluded (false); the UI defaults it on.
func (a *App) GetSubTaskTraceability(profileID string, parentFilters []string, crossProject bool) (testrepo.Sankey, error) {
	if err := a.requireStore(); err != nil {
		return testrepo.Sankey{}, err
	}
	return a.repo.GetSubTaskTraceability(profileID, parentFilters, crossProject)
}

// GetExecutionsForPlans returns the Test Executions sharing a Test with the
// given Test Plans, to cascade the dashboard's Execution filter (#5a). Empty
// planKeys returns all executions.
func (a *App) GetExecutionsForPlans(profileID string, planKeys []string) ([]testrepo.Container, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.ExecutionsForPlans(profileID, planKeys)
}

// GetProfileProjectKey returns the active profile's Jira project key, used by the
// dashboard to flag cross-project bugs (#5b).
func (a *App) GetProfileProjectKey(profileID string) (string, error) {
	if err := a.requireStore(); err != nil {
		return "", err
	}
	p, err := a.profiles.Get(profileID)
	if err != nil {
		return "", err
	}
	return p.ProjectKey, nil
}

// --- pytest helper (FR-7.2) ---

// ExportPytest generates a Python test scaffold from a Test Set / Plan /
// Execution and writes it to a user-chosen .py file (FR-7.2). style is
// "function" (plain pytest, the default) or "unittest" (a unittest.TestCase
// subclass). Returns the saved path, or "" if cancelled.
func (a *App) ExportPytest(profileID, containerKey, style string) (string, error) {
	if err := a.requireStore(); err != nil {
		return "", err
	}
	code, err := a.repo.GeneratePytest(profileID, containerKey, style)
	if err != nil {
		return "", err
	}
	suffix := ""
	if style == "unittest" {
		suffix = "_unittest"
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save Python scaffold",
		DefaultFilename: exportFilename("test_" + sanitizeFilename(containerKey) + suffix + ".py"),
		Filters:         []runtime.FileFilter{{DisplayName: "Python", Pattern: "*.py"}},
	})
	if err != nil {
		return "", fmt.Errorf("save dialog: %w", err)
	}
	if path == "" {
		return "", nil
	}
	if err := os.WriteFile(path, []byte(code), 0o644); err != nil {
		return "", fmt.Errorf("write pytest: %w", err)
	}
	return path, nil
}

// --- Container board (FR-13.7) ---

// GetContainerBoard returns the read-only board for a Test Set / Plan /
// Execution: its member Tests with run status (direct for an execution,
// consolidated across executions otherwise), computed from the local store.
func (a *App) GetContainerBoard(profileID, containerKey string) (testrepo.TestPlanBoard, error) {
	if err := a.requireStore(); err != nil {
		return testrepo.TestPlanBoard{}, err
	}
	return a.repo.GetContainerBoard(profileID, containerKey)
}

// --- Container allocation (FR-3.4–3.6) ---

// ListContainers returns the cached Test Sets / Plans / Executions of a kind
// ("testset" / "testplan" / "testexec") for the allocation picker.
func (a *App) ListContainers(profileID, kind string) ([]testrepo.Container, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.ListContainers(profileID, kind)
}

// AllocateTests adds Tests to an existing Container locally and queues the
// membership for commit (FR-3.4–3.6, add-only). Tests already in the Container
// are reported back without being re-queued.
func (a *App) AllocateTests(profileID, containerKey string, testKeys []string) (result testrepo.AllocateResult, err error) {
	defer recoverToError("AllocateTests", &err)
	empty := testrepo.AllocateResult{Added: []string{}, AlreadyMembers: []string{}}
	if err := a.requireStore(); err != nil {
		return empty, err
	}
	return a.repo.AllocateTests(profileID, containerKey, testKeys)
}

// DeallocateTests removes Tests from a Container locally and queues the removal
// for commit (FR-3.4–3.6). Tests that weren't members are reported back.
func (a *App) DeallocateTests(profileID, containerKey string, testKeys []string) (result testrepo.DeallocateResult, err error) {
	defer recoverToError("DeallocateTests", &err)
	empty := testrepo.DeallocateResult{Removed: []string{}, NotMembers: []string{}}
	if err := a.requireStore(); err != nil {
		return empty, err
	}
	return a.repo.DeallocateTests(profileID, containerKey, testKeys)
}

// RunStatuses returns the Test Run result vocabulary for execution result
// editing.
func (a *App) RunStatuses() []string {
	return testrepo.RunStatuses
}

// SetTestRunStatus updates a Test's run result within a Test Execution and
// queues it for commit to Xray.
func (a *App) SetTestRunStatus(profileID, execKey, testKey, status string) (err error) {
	defer recoverToError("SetTestRunStatus", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.SetTestRunStatus(profileID, execKey, testKey, status)
}

// BulkSetTestRunStatus applies one run result to several Tests in a Test
// Execution at once (FR-3 bulk), queued for commit to Xray like single updates.
func (a *App) BulkSetTestRunStatus(profileID, execKey string, testKeys []string, status string) (result testrepo.BulkEditResult, err error) {
	defer recoverToError("BulkSetTestRunStatus", &err)
	if err := a.requireStore(); err != nil {
		return testrepo.BulkEditResult{}, err
	}
	return a.repo.BulkSetTestRunStatus(profileID, execKey, testKeys, status), nil
}

// AnalyzeJUnitImport decodes a base64-encoded JUnit XML report and matches
// its testcases to member tests of the given execution by summary, returning
// a preview of what would be applied.
func (a *App) AnalyzeJUnitImport(profileID, execKey, xmlBase64 string) (testrepo.JUnitImportPreview, error) {
	if err := a.requireStore(); err != nil {
		return testrepo.JUnitImportPreview{}, err
	}
	return a.repo.AnalyzeJUnitImport(profileID, execKey, xmlBase64)
}

// ApplyJUnitImport sets the run result for each matched testcase in the given
// execution, queuing pending changes for commit.
func (a *App) ApplyJUnitImport(profileID, execKey string, matches []testrepo.JUnitMatch) (result testrepo.BulkEditResult, err error) {
	defer recoverToError("ApplyJUnitImport", &err)
	if err := a.requireStore(); err != nil {
		return testrepo.BulkEditResult{}, err
	}
	return a.repo.ApplyJUnitImport(profileID, execKey, matches)
}

// AnalyzeJUnitImportNewExec decodes a base64-encoded JUnit XML report and
// classifies each testcase against all tests for the profile, returning a
// preview of what a new Test Execution would contain. When createMissing is
// true, unmatched testcases are queued for creation; otherwise they are
// reported as skipped.
func (a *App) AnalyzeJUnitImportNewExec(profileID, xmlBase64 string, createMissing bool) (testrepo.JUnitNewExecPreview, error) {
	if err := a.requireStore(); err != nil {
		return testrepo.JUnitNewExecPreview{}, err
	}
	return a.repo.AnalyzeJUnitImportNewExec(profileID, xmlBase64, createMissing)
}

// ApplyJUnitImportNewExec queues pending changes to create a new Test
// Execution from a JUnit analysis preview: new tests are created for rows
// with Create=true, all tests are allocated to the execution, and run results
// are set for rows with a non-empty Result. The projectKey is resolved from
// the active profile so the commit engine can create the execution in Jira.
func (a *App) ApplyJUnitImportNewExec(profileID, summary string, rows []testrepo.JUnitNewExecRow) (out testrepo.JUnitNewExecResult, err error) {
	defer recoverToError("ApplyJUnitImportNewExec", &err)
	var empty testrepo.JUnitNewExecResult
	if err := a.requireStore(); err != nil {
		return empty, err
	}
	p, err := a.profiles.Get(profileID)
	if err != nil {
		return empty, err
	}
	return a.repo.ApplyJUnitImportNewExec(profileID, p.ProjectKey, summary, rows)
}

// EditContainer renames a Test Set / Plan / Execution and queues the change
// for commit (container CRUD).
func (a *App) EditContainer(profileID, key, summary string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.EditContainer(profileID, key, summary)
}

// DeleteContainer removes a Test Set / Plan / Execution and queues the deletion
// for commit (container CRUD).
func (a *App) DeleteContainer(profileID, key string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.DeleteContainer(profileID, key)
}

// SetContainerEnvironments replaces a Test Execution's Test Environments and
// queues the change for commit (RND_P_4TFINT_05-229). The set is pushed to Jira
// as a custom-field update on commit.
func (a *App) SetContainerEnvironments(profileID, containerKey string, envs []string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.SetContainerEnvironments(profileID, containerKey, envs)
}

// BulkEditContainers applies a Test Environments operation (set_env / add_env /
// remove_env) across a batch of containers, queuing a pending change per
// container (RND_P_4TFINT_05-229).
func (a *App) BulkEditContainers(profileID string, containerKeys []string, op testrepo.BulkEdit) (result testrepo.BulkEditResult, err error) {
	defer recoverToError("BulkEditContainers", &err)
	empty := testrepo.BulkEditResult{
		Succeeded: []string{},
		Failed:    []testrepo.BulkFailure{},
	}
	if err := a.requireStore(); err != nil {
		return empty, err
	}
	return a.repo.BulkEditContainers(profileID, containerKeys, op)
}

// CreateContainerAndAllocate creates a new Test Set / Plan / Execution locally
// and allocates the given Tests to it (FR-3.4–3.6). The Container is created in
// Jira on commit; until then it carries a temporary key. The project comes
// from the active profile.
func (a *App) CreateContainerAndAllocate(profileID, kind, summary string, testKeys []string) (out testrepo.CreateContainerResult, err error) {
	defer recoverToError("CreateContainerAndAllocate", &err)
	var empty testrepo.CreateContainerResult
	if err := a.requireStore(); err != nil {
		return empty, err
	}
	p, err := a.profiles.Get(profileID)
	if err != nil {
		return empty, err
	}
	return a.repo.CreateContainerAllocation(profileID, p.ProjectKey, kind, summary, testKeys)
}

// --- Statistics dashboard (FR-9) ---

// GetStatistics returns the dashboard rollup for a profile, computed entirely
// from the local store (FR-9.5) — status / priority / label / folder
// distributions, a last-updated trend, and the pending-change count. The
// optional folder / component / status arguments narrow every panel to the
// matching subset of Tests (empty string = no constraint).
func (a *App) GetStatistics(profileID, folder, component, status string) (testrepo.Statistics, error) {
	if err := a.requireStore(); err != nil {
		return testrepo.Statistics{}, err
	}
	return a.repo.GetStatistics(profileID, folder, component, status)
}

// --- Duplicate management (FR — duplicate management) ---

// ScanDuplicates returns the duplicate report for a profile (FR — duplicate
// management): summary groups computed from the local cache, with step verdicts
// from prior step scans. Instant, no Jira call.
func (a *App) ScanDuplicates(profileID string) (testrepo.DuplicateReport, error) {
	if err := a.requireStore(); err != nil {
		return testrepo.DuplicateReport{}, err
	}
	return a.repo.ScanDuplicates(profileID)
}

// ScanDuplicateGroupSteps fetches steps for one group's members (bounded), records
// each fingerprint, and returns the recomputed group with its steps verdict.
func (a *App) ScanDuplicateGroupSteps(profileID, normalizedSummary string) (testrepo.DuplicateGroup, error) {
	if err := a.requireStore(); err != nil {
		return testrepo.DuplicateGroup{}, err
	}
	keys, err := a.repo.DuplicateGroupMemberKeys(profileID, normalizedSummary)
	if err != nil {
		return testrepo.DuplicateGroup{}, err
	}
	for _, key := range keys {
		// Force a fresh pull so the verdict reflects current Jira step content;
		// GetTestSteps caches into test_step and (in demo) is deterministic.
		steps, err := a.GetTestSteps(profileID, key, true)
		if err != nil {
			// Best-effort: skip members we can't fetch; they stay unscanned.
			continue
		}
		if err := a.repo.RecordStepScan(profileID, key, steps); err != nil {
			return testrepo.DuplicateGroup{}, err
		}
	}
	return a.repo.ScanDuplicateGroup(profileID, normalizedSummary)
}

// ScanAllDuplicateSteps walks every duplicate group whose steps verdict is still
// unscanned, records each member's step fingerprint, and sets the group verdict,
// emitting "dup:scan-progress" so the Duplicates toolbar can show a progress bar.
// It uses its own event channel (not the global "sync:progress") so the walk does
// not show in the footer sync bar or reset global sync state. It returns the number
// of groups scanned. Per-group errors are swallowed so one bad group does not abort
// the run.
func (a *App) ScanAllDuplicateSteps(profileID string) (int, error) {
	if err := a.requireStore(); err != nil {
		return 0, err
	}
	defer runtime.EventsEmit(a.ctx, "dup:scan-progress", syncer.Progress{Done: true})
	return a.repo.ScanAllDuplicateSteps(profileID,
		// Force a fresh pull so each member's verdict reflects current Jira step
		// content, exactly like ScanDuplicateGroupSteps. Steps load lazily, so a
		// never-opened member has an empty cache that would otherwise fingerprint
		// to "" and wrongly read as identical.
		func(key string) ([]testrepo.Step, error) { return a.GetTestSteps(profileID, key, true) },
		func(done, total int) {
			runtime.EventsEmit(a.ctx, "dup:scan-progress", syncer.Progress{
				Stage:   "Scanning duplicate steps",
				Fetched: done,
				Total:   total,
			})
		},
	)
}

// ExcludeFromDuplicates permanently ignores a Test in duplicate scans (local).
func (a *App) ExcludeFromDuplicates(profileID, testKey string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.ExcludeFromDuplicates(profileID, testKey)
}

// UnexcludeFromDuplicates restores a previously-excluded Test.
func (a *App) UnexcludeFromDuplicates(profileID, testKey string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.UnexcludeFromDuplicates(profileID, testKey)
}

// --- Test Repository moves (FR-13.3) ---

// MoveTestToFolder relocates a Test in the Test Repository tree locally and
// queues the move for commit (FR-13.3). folderID is the full folder path, or
// empty for the repository root.
func (a *App) MoveTestToFolder(profileID, testKey, folderID string) (err error) {
	defer recoverToError("MoveTestToFolder", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.MoveTestToFolder(profileID, testKey, folderID)
}

// BulkMoveToFolder moves a batch of Tests to one Test Repository folder
// (FR-13.3 bulk), queuing a pending change per moved Test.
func (a *App) BulkMoveToFolder(profileID string, testKeys []string, folderID string) (result testrepo.BulkEditResult, err error) {
	defer recoverToError("BulkMoveToFolder", &err)
	empty := testrepo.BulkEditResult{Succeeded: []string{}, Failed: []testrepo.BulkFailure{}}
	if err := a.requireStore(); err != nil {
		return empty, err
	}
	return a.repo.BulkMoveToFolder(profileID, testKeys, folderID)
}

// --- Saved views (FR-11.4) ---

// CreateSavedView stores the current browse filter under a name. The query is
// an opaque JSON blob owned by the frontend.
func (a *App) CreateSavedView(profileID, name, query string) (testrepo.SavedView, error) {
	if err := a.requireStore(); err != nil {
		return testrepo.SavedView{}, err
	}
	return a.repo.CreateSavedView(profileID, name, query)
}

// ListSavedViews returns a profile's saved browse filters, newest first.
func (a *App) ListSavedViews(profileID string) ([]testrepo.SavedView, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.ListSavedViews(profileID)
}

// DeleteSavedView removes a saved browse filter.
func (a *App) DeleteSavedView(profileID, id string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.DeleteSavedView(profileID, id)
}

// --- Test review ---

// GetTestReview returns a Test's current review state.
func (a *App) GetTestReview(profileID, testKey string) (testrepo.Review, error) {
	if err := a.requireStore(); err != nil {
		return testrepo.Review{}, err
	}
	return a.repo.GetTestReview(profileID, testKey)
}

// SetTestReview records a review verdict (approved / rejected / pending, or ""
// to clear) for a Test and queues it for commit as a Jira comment.
func (a *App) SetTestReview(profileID, testKey, verdict, reviewer, note string) (err error) {
	defer recoverToError("SetTestReview", &err)
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.SetTestReview(profileID, testKey, verdict, reviewer, note)
}

// AddTestComment queues a free-text comment to post on a Test (FR-4.4), e.g.
// the reason for a workflow transition.
func (a *App) AddTestComment(profileID, testKey, body string) error {
	if err := a.requireStore(); err != nil {
		return err
	}
	return a.repo.AddTestComment(profileID, testKey, body)
}

// BulkReviewTests applies one review verdict to many Tests at once.
func (a *App) BulkReviewTests(profileID string, testKeys []string, verdict, reviewer, note string) (result testrepo.BulkEditResult, err error) {
	defer recoverToError("BulkReviewTests", &err)
	empty := testrepo.BulkEditResult{Succeeded: []string{}, Failed: []testrepo.BulkFailure{}}
	if err := a.requireStore(); err != nil {
		return empty, err
	}
	return a.repo.BulkSetReview(profileID, testKeys, verdict, reviewer, note)
}

// --- Import (FR-10) ---

// PreviewImport parses an import file's header row and counts its data rows so
// the import UI can offer column mapping (FR-10.4 / 10.5). contentB64 is the
// base64-encoded file; isXlsx selects the XLSX parser over CSV.
func (a *App) PreviewImport(contentB64 string, isXlsx bool) (testrepo.ImportPreview, error) {
	if err := a.requireStore(); err != nil {
		return testrepo.ImportPreview{}, err
	}
	records, err := decodeImport(contentB64, isXlsx)
	if err != nil {
		return testrepo.ImportPreview{}, err
	}
	return testrepo.ParseImportPreview(records)
}

// ImportTests validates an import against a column mapping and, unless dryRun,
// creates a local pending Test per valid row (FR-10.1 / 10.2 / 10.5 / 10.6).
func (a *App) ImportTests(profileID, contentB64 string, isXlsx bool, mapping testrepo.ImportMapping, dryRun bool) (testrepo.ImportResult, error) {
	empty := testrepo.ImportResult{Errors: []testrepo.ImportError{}}
	if err := a.requireStore(); err != nil {
		return empty, err
	}
	records, err := decodeImport(contentB64, isXlsx)
	if err != nil {
		return empty, err
	}
	return a.repo.ImportTests(profileID, records, mapping, dryRun)
}

// CreateRequirement queues a brand-new Requirement locally (temp NEW-REQ-N key)
// for the given project + issue type, pushed to Jira on commit.
// The project key and issue types come from the configured requirement sources.
// Returns the temp key.
func (a *App) CreateRequirement(profileID, projectKey, issueType, summary, description, priority, components, fixVersions string) (key string, err error) {
	defer recoverToError("CreateRequirement", &err)
	if err := a.requireStore(); err != nil {
		return "", err
	}
	if strings.TrimSpace(summary) == "" {
		return "", fmt.Errorf("a summary is required to create a requirement")
	}
	if strings.TrimSpace(projectKey) == "" {
		return "", fmt.Errorf("a project key is required to create a requirement")
	}
	return a.repo.CreateRequirement(profileID, projectKey, issueType, summary, description, priority, components, fixVersions)
}

// CreateTest queues a brand-new Test locally (temp NEW-N key) with optional
// steps, folder and precondition links, pushed to Jira on commit (FR-1).
// Returns the temp key so the frontend can open the new Test in the detail panel.
func (a *App) CreateTest(profileID string, draft testrepo.TestDraft) (key string, err error) {
	defer recoverToError("CreateTest", &err)
	if err := a.requireStore(); err != nil {
		return "", err
	}
	if strings.TrimSpace(draft.Summary) == "" {
		return "", fmt.Errorf("a summary is required to create a test")
	}
	return a.repo.CreateTest(profileID, draft)
}

// CloneTest drafts a new local Test copying an existing Test's fields and steps
// (RND_P_4TFINT_05-206), pushed to Jira on commit. Returns the temp key so the
// frontend can open the clone in the detail panel.
func (a *App) CloneTest(profileID, sourceKey string) (string, error) {
	if err := a.requireStore(); err != nil {
		return "", err
	}
	if strings.TrimSpace(sourceKey) == "" {
		return "", fmt.Errorf("a source test key is required to clone")
	}
	return a.repo.CloneTest(profileID, sourceKey)
}

// decodeImport base64-decodes an uploaded file and parses it into rows.
func decodeImport(contentB64 string, isXlsx bool) ([][]string, error) {
	data, err := base64.StdEncoding.DecodeString(contentB64)
	if err != nil {
		return nil, fmt.Errorf("decode upload: %w", err)
	}
	return testrepo.ParseRecords(data, isXlsx)
}

// AnalyzeGap diffs a reference test list against an uploaded target list by
// normalized summary. refSource "project" uses the active project's cached
// tests (refB64 ignored); "file" parses refB64. The target is always a file.
func (a *App) AnalyzeGap(profileID, refSource, refB64 string, refXlsx bool, targetB64 string, targetXlsx bool, threeWay, compareFolders bool) (testrepo.GapResult, error) {
	if err := a.requireStore(); err != nil {
		return testrepo.GapResult{}, err
	}
	var reference []testrepo.GapTest
	switch refSource {
	case "project":
		tests, err := a.repo.ListTestsForExport(profileID, testrepo.Query{})
		if err != nil {
			return testrepo.GapResult{}, err
		}
		reference = testrepo.GapRowsFromTests(tests)
	case "file":
		recs, err := decodeImport(refB64, refXlsx)
		if err != nil {
			return testrepo.GapResult{}, fmt.Errorf("reference file: %w", err)
		}
		reference, err = testrepo.ParseGapRows(recs)
		if err != nil {
			return testrepo.GapResult{}, fmt.Errorf("reference file: %w", err)
		}
	default:
		return testrepo.GapResult{}, fmt.Errorf("unknown reference source %q", refSource)
	}
	targetRecs, err := decodeImport(targetB64, targetXlsx)
	if err != nil {
		return testrepo.GapResult{}, fmt.Errorf("target file: %w", err)
	}
	target, err := testrepo.ParseGapRows(targetRecs)
	if err != nil {
		return testrepo.GapResult{}, fmt.Errorf("target file: %w", err)
	}

	// Three-way only applies when the reference is a file (otherwise the
	// reference IS the project). Fetch the project list to diff against.
	opts := testrepo.GapOptions{ReferenceSource: refSource, CompareFolders: compareFolders}
	var project []testrepo.GapTest
	if threeWay && refSource == "file" {
		tests, err := a.repo.ListTestsForExport(profileID, testrepo.Query{})
		if err != nil {
			return testrepo.GapResult{}, err
		}
		project = testrepo.GapRowsFromTests(tests)
		opts.ThreeWay = true
	}
	return testrepo.AnalyzeGap(reference, target, project, opts), nil
}

// CreateTestsFromGaps adds the selected gaps as local pending Tests (committed
// on the next sync), reusing the import create path.
func (a *App) CreateTestsFromGaps(profileID string, gaps []testrepo.GapTest) (result testrepo.ImportResult, err error) {
	defer recoverToError("CreateTestsFromGaps", &err)
	if err := a.requireStore(); err != nil {
		return testrepo.ImportResult{}, err
	}
	return a.repo.CreateTestsFromGaps(profileID, gaps)
}

// ExportGapReport writes the gap-analysis report to a user-chosen file. format
// is "csv" or "xlsx" and sets the default extension/filter; the actually-written
// format follows the saved file's extension. Returns the saved path, or "".
func (a *App) ExportGapReport(result testrepo.GapResult, format string) (string, error) {
	if err := a.requireStore(); err != nil {
		return "", err
	}
	defaultName := "gap-analysis-report.csv"
	filters := []runtime.FileFilter{
		{DisplayName: "CSV", Pattern: "*.csv"},
		{DisplayName: "Excel", Pattern: "*.xlsx"},
	}
	if format == "xlsx" {
		defaultName = "gap-analysis-report.xlsx"
		filters = []runtime.FileFilter{
			{DisplayName: "Excel", Pattern: "*.xlsx"},
			{DisplayName: "CSV", Pattern: "*.csv"},
		}
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export gap analysis report",
		DefaultFilename: exportFilename(defaultName),
		Filters:         filters,
	})
	if err != nil {
		return "", fmt.Errorf("save dialog: %w", err)
	}
	if path == "" {
		return "", nil
	}
	writeFormat := "csv"
	if strings.HasSuffix(strings.ToLower(path), ".xlsx") {
		writeFormat = "xlsx"
	}
	data, err := testrepo.BuildGapReport(result, time.Now().Format(time.RFC3339), writeFormat)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write report: %w", err)
	}
	return path, nil
}

// ExportTests writes the Tests matching a query to a user-chosen XLSX or CSV
// file (FR-10.8). XLSX is the default; the format follows the saved file's
// extension. Returns the saved path, or "" if cancelled.
func (a *App) ExportTests(profileID string, q testrepo.Query) (string, error) {
	if err := a.requireStore(); err != nil {
		return "", err
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export tests",
		DefaultFilename: exportFilename("tests-export.xlsx"),
		Filters: []runtime.FileFilter{
			{DisplayName: "Excel", Pattern: "*.xlsx"},
			{DisplayName: "CSV", Pattern: "*.csv"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("save dialog: %w", err)
	}
	if path == "" {
		return "", nil
	}
	format := "xlsx"
	if strings.HasSuffix(strings.ToLower(path), ".csv") {
		format = "csv"
	}
	data, err := a.repo.ExportTests(profileID, q, format)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write export: %w", err)
	}
	return path, nil
}

// ExportRequirementAudit writes the requirement coverage / sign-off audit to a
// user-chosen XLSX or CSV file. XLSX is the default; the format follows the
// saved file's extension. Returns the saved path, or "" if cancelled.
func (a *App) ExportRequirementAudit(profileID string) (string, error) {
	if err := a.requireStore(); err != nil {
		return "", err
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export requirement audit",
		DefaultFilename: exportFilename("requirement-audit.xlsx"),
		Filters: []runtime.FileFilter{
			{DisplayName: "Excel", Pattern: "*.xlsx"},
			{DisplayName: "CSV", Pattern: "*.csv"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("save dialog: %w", err)
	}
	if path == "" {
		return "", nil
	}
	format := "xlsx"
	if strings.HasSuffix(strings.ToLower(path), ".csv") {
		format = "csv"
	}
	data, err := a.repo.ExportRequirementAudit(profileID, format)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write export: %w", err)
	}
	return path, nil
}

// ExportBugsWithRunHistory exports the selected bugs with their affected-test
// run history to an XLSX file chosen by the user. Returns the saved path, or
// "" (no error) when the user cancels the save dialog. Best-effort on the
// live GetBugDetail per bug: if the fetch fails, the live-only fields are left
// blank and the cached fields are still exported.
func (a *App) ExportBugsWithRunHistory(profileID string, bugKeys []string) (string, error) {
	if err := a.requireStore(); err != nil {
		return "", err
	}
	if len(bugKeys) == 0 {
		return "", fmt.Errorf("no bug keys provided")
	}

	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export bugs",
		DefaultFilename: exportFilename("bugs-run-history.xlsx"),
		Filters:         []runtime.FileFilter{{DisplayName: "Excel", Pattern: "*.xlsx"}},
	})
	if err != nil {
		return "", fmt.Errorf("save dialog: %w", err)
	}
	if path == "" {
		return "", nil
	}

	// Load profile and credentials for the live detail fetch.
	p, err := a.profiles.Get(profileID)
	if err != nil {
		return "", err
	}
	token, err := a.creds.Load(profileID)
	if err != nil {
		return "", fmt.Errorf("load credentials: %w", err)
	}
	cl := jira.NewClient(p.JiraURL, token, tlsOptions(p)...)

	exports := make([]testrepo.BugExport, 0, len(bugKeys))
	for _, key := range bugKeys {
		bug, err := a.repo.GetBug(profileID, key)
		if err != nil {
			log.Printf("xtm: ExportBugsWithRunHistory: get bug %s: %v (skipping)", key, err)
			continue
		}
		ex := testrepo.BugExport{
			Key:        bug.Key,
			ProjectKey: bug.ProjectKey,
			IssueType:  bug.IssueType,
			Status:     bug.Status,
			Priority:   bug.Priority,
			Summary:    bug.Summary,
		}

		// Best-effort live fetch for extended fields.
		if detail, dErr := cl.GetBugDetail(a.ctx, key); dErr != nil {
			log.Printf("xtm: ExportBugsWithRunHistory: GetBugDetail %s: %v (leaving detail fields blank)", key, dErr)
		} else {
			ex.Description = detail.Description
			ex.DefectOrigin = detail.DefectOrigin
			ex.DefectAnalysis = detail.DefectAnalysis
			ex.CorrectionDetails = detail.CorrectionDetails
			ex.Reporter = detail.Reporter
			ex.Severity = detail.Severity
		}

		// A failure here drops only the affected-test rows: the bug still appears
		// in the "Bugs" sheet (with its cached fields and a zero affected count)
		// rather than vanishing from the export entirely.
		affectedTests, tErr := a.repo.ListTestsForBug(profileID, key)
		if tErr != nil {
			log.Printf("xtm: ExportBugsWithRunHistory: list affected tests %s: %v (bug exported without test rows)", key, tErr)
			affectedTests = nil
		}
		ex.AffectedTests = affectedTests

		ex.RunHistory = make(map[string][]testrepo.TestRunEntry, len(affectedTests))
		for _, bt := range affectedTests {
			hist, hErr := a.repo.GetTestRunHistory(profileID, bt.Key)
			if hErr != nil {
				log.Printf("xtm: ExportBugsWithRunHistory: run history %s/%s: %v (skipping)", key, bt.Key, hErr)
				continue
			}
			ex.RunHistory[bt.Key] = hist
		}
		exports = append(exports, ex)
	}

	data, err := a.repo.BuildBugExportWorkbook(exports)
	if err != nil {
		return "", fmt.Errorf("build workbook: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write export: %w", err)
	}
	return path, nil
}

// ExportTraceability exports the active Traceability tab (kind "requirement",
// "execution", or "subtask") to an XLSX with a Flow sheet (the Sankey edge list
// with resolved labels) and a Table sheet (flat one-row-per-thread records),
// respecting that tab's current filters (RND_P_4TFINT_05-221). crossProject is
// threaded to the execution producer; the sub-task producer ignores it until
// Task 12 wires cross-project sub-tasks. Returns the saved path, or "" if
// cancelled.
func (a *App) ExportTraceability(profileID, kind string, planFilters, execFilters []string, crossProject bool, reqFilters, parentFilters []string) (string, error) {
	if err := a.requireStore(); err != nil {
		return "", err
	}
	projectKey, err := a.GetProfileProjectKey(profileID)
	if err != nil {
		return "", err
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export traceability",
		DefaultFilename: exportFilename("traceability-" + kind + ".xlsx"),
		Filters:         []runtime.FileFilter{{DisplayName: "Excel", Pattern: "*.xlsx"}},
	})
	if err != nil {
		return "", fmt.Errorf("save dialog: %w", err)
	}
	if path == "" {
		return "", nil
	}
	data, err := a.repo.ExportTraceabilitySheets(profileID, projectKey, kind, planFilters, execFilters, reqFilters, parentFilters, crossProject)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write export: %w", err)
	}
	return path, nil
}

// ExportDashboard exports the statistics Dashboard to an XLSX with a Summary
// sheet and one sheet per breakdown distribution, respecting the current
// folder/component/status filters (RND_P_4TFINT_05). Returns the saved path, or
// "" if cancelled.
func (a *App) ExportDashboard(profileID, folder, component, status string) (string, error) {
	if err := a.requireStore(); err != nil {
		return "", err
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export dashboard",
		DefaultFilename: exportFilename("dashboard.xlsx"),
		Filters:         []runtime.FileFilter{{DisplayName: "Excel", Pattern: "*.xlsx"}},
	})
	if err != nil {
		return "", fmt.Errorf("save dialog: %w", err)
	}
	if path == "" {
		return "", nil
	}
	data, err := a.repo.ExportDashboardSheets(profileID, folder, component, status)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write export: %w", err)
	}
	return path, nil
}

// ExportImportTemplate writes a starter CSV import template to a user-chosen
// file (FR-10.3). Returns the saved path, or "" if cancelled.
func (a *App) ExportImportTemplate() (string, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save import template",
		DefaultFilename: exportFilename("test-import-template.csv"),
		Filters:         []runtime.FileFilter{{DisplayName: "CSV", Pattern: "*.csv"}},
	})
	if err != nil {
		return "", fmt.Errorf("save dialog: %w", err)
	}
	if path == "" {
		return "", nil
	}
	if err := os.WriteFile(path, []byte(testrepo.ImportTemplateCSV()), 0o644); err != nil {
		return "", fmt.Errorf("write template: %w", err)
	}
	return path, nil
}

// ExportSummaryTemplate writes the summary-only gap-analysis template (just a
// Summary column) to a user-chosen file. Returns the saved path, or "" if
// cancelled.
func (a *App) ExportSummaryTemplate() (string, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save summary-only template",
		DefaultFilename: exportFilename("gap-summary-template.csv"),
		Filters:         []runtime.FileFilter{{DisplayName: "CSV", Pattern: "*.csv"}},
	})
	if err != nil {
		return "", fmt.Errorf("save dialog: %w", err)
	}
	if path == "" {
		return "", nil
	}
	if err := os.WriteFile(path, []byte(testrepo.SummaryTemplateCSV()), 0o644); err != nil {
		return "", fmt.Errorf("write template: %w", err)
	}
	return path, nil
}

// ExportSummaryFolderTemplate writes the summary + folder gap-analysis template
// to a user-chosen file. Returns the saved path, or "" if cancelled.
func (a *App) ExportSummaryFolderTemplate() (string, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save summary + folder template",
		DefaultFilename: exportFilename("gap-summary-folder-template.csv"),
		Filters:         []runtime.FileFilter{{DisplayName: "CSV", Pattern: "*.csv"}},
	})
	if err != nil {
		return "", fmt.Errorf("save dialog: %w", err)
	}
	if path == "" {
		return "", nil
	}
	if err := os.WriteFile(path, []byte(testrepo.SummaryFolderTemplateCSV()), 0o644); err != nil {
		return "", fmt.Errorf("write template: %w", err)
	}
	return path, nil
}

// --- Requirement import (-267) ---

// ExportRequirementImportTemplate writes a starter CSV requirement import
// template to a user-chosen file. Returns the saved path, or "" if cancelled.
func (a *App) ExportRequirementImportTemplate() (string, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save requirement import template",
		DefaultFilename: exportFilename("requirement-import-template.csv"),
		Filters:         []runtime.FileFilter{{DisplayName: "CSV", Pattern: "*.csv"}},
	})
	if err != nil {
		return "", fmt.Errorf("save dialog: %w", err)
	}
	if path == "" {
		return "", nil
	}
	if err := os.WriteFile(path, []byte(testrepo.RequirementImportTemplateCSV()), 0o644); err != nil {
		return "", fmt.Errorf("write template: %w", err)
	}
	return path, nil
}

// AnalyzeRequirementImport parses an uploaded file and classifies each row as
// "new" or "existing" by comparing normalized summaries against the store.
func (a *App) AnalyzeRequirementImport(profileID, contentB64 string, isXlsx bool) (testrepo.RequirementImportPreview, error) {
	empty := testrepo.RequirementImportPreview{Rows: []testrepo.RequirementImportRow{}}
	if err := a.requireStore(); err != nil {
		return empty, err
	}
	records, err := decodeImport(contentB64, isXlsx)
	if err != nil {
		return empty, err
	}
	return a.repo.AnalyzeRequirementImport(profileID, records)
}

// ImportRequirements parses an uploaded file and creates local pending
// Requirements for rows whose normalized summary is not already in the store.
// Existing-by-summary rows are skipped. Returns a result summary.
func (a *App) ImportRequirements(profileID, projectKey, issueType, contentB64 string, isXlsx bool) (testrepo.RequirementImportResult, error) {
	empty := testrepo.RequirementImportResult{Errors: []testrepo.ImportError{}}
	if err := a.requireStore(); err != nil {
		return empty, err
	}
	if strings.TrimSpace(projectKey) == "" {
		return empty, fmt.Errorf("a project key is required")
	}
	records, err := decodeImport(contentB64, isXlsx)
	if err != nil {
		return empty, err
	}
	return a.repo.ImportRequirements(profileID, projectKey, issueType, records)
}

// --- Browse (FR-11) ---

// ListTests returns a filtered, sorted, paginated page of Tests for a profile.
func (a *App) ListTests(profileID string, q testrepo.Query) (testrepo.Page, error) {
	if err := a.requireStore(); err != nil {
		return testrepo.Page{}, err
	}
	return a.repo.ListTests(profileID, q)
}

// ListMatchingKeys returns the Jira keys of every Test that matches the
// query's filter (FR-3.1), regardless of pagination. Used by the
// "select all N matching" banner to fill the bulk selection from the
// full result set in a single round trip.
func (a *App) ListMatchingKeys(profileID string, q testrepo.Query) ([]string, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.ListMatchingKeys(profileID, q)
}

// ListComponents returns the distinct Jira components across a profile's Tests
// (with a count each), for the group-by-component sidebar.
func (a *App) ListComponents(profileID string) ([]testrepo.Bucket, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.ListComponents(profileID)
}

// ListStatuses returns the statuses for the browse filter: the Test issue
// type's workflow statuses from Jira (in workflow order, cached per profile for
// the session) unioned with the statuses actually present on synced Tests — so
// the dropdown is both authoritative and never empty.
func (a *App) ListStatuses(profileID string) ([]string, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	synced, err := a.repo.ListTestStatuses(profileID)
	if err != nil {
		return nil, err
	}
	return mergeStatuses(a.workflowStatuses(profileID), synced), nil
}

// workflowStatuses returns the Test workflow statuses from Jira, cached per
// profile for the session. A fetch failure yields nil (synced statuses still
// fill the dropdown) and is not cached, so a later call can retry.
func (a *App) workflowStatuses(profileID string) []string {
	a.statusMu.Lock()
	cached, ok := a.statusCache[profileID]
	a.statusMu.Unlock()
	if ok {
		return cached
	}
	p, err := a.profiles.Get(profileID)
	if err != nil {
		return nil
	}
	token, err := a.creds.Load(profileID)
	if err != nil {
		return nil
	}
	statuses, err := jira.NewClient(p.JiraURL, token).ListStatuses(a.ctx, p.ProjectKey)
	if err != nil || len(statuses) == 0 {
		return nil
	}
	a.statusMu.Lock()
	a.statusCache[profileID] = statuses
	a.statusMu.Unlock()
	return statuses
}

// ListPriorities returns the priority names valid for the Test issue type in the
// profile's project (FR-1), resolved from Jira createmeta and cached per profile
// for the session. These are exactly the values Jira accepts when creating a
// Test — not the global priority scheme.
func (a *App) ListPriorities(profileID string) ([]string, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	// Return ONLY the Test issue type's allowed priorities (from createmeta).
	// We deliberately do NOT union these with the priorities present on synced
	// Tests: that union re-introduced out-of-scheme / legacy values and made the
	// dropdown look like the global priority list. The New Test form must offer
	// exactly what Jira accepts when creating a Test.
	return a.jiraPriorities(profileID), nil
}

// jiraPriorities returns the Jira priority scheme, cached per profile for the
// session. A fetch failure yields nil (synced priorities still fill the list)
// and is not cached, so a later call can retry.
func (a *App) jiraPriorities(profileID string) []string {
	a.statusMu.Lock()
	cached, ok := a.priorityCache[profileID]
	a.statusMu.Unlock()
	if ok {
		return cached
	}
	p, err := a.profiles.Get(profileID)
	if err != nil {
		return nil
	}
	token, err := a.creds.Load(profileID)
	if err != nil {
		return nil
	}
	priorities, err := jira.NewClient(p.JiraURL, token).ListPriorities(a.ctx, p.ProjectKey)
	if err != nil || len(priorities) == 0 {
		return nil
	}
	a.statusMu.Lock()
	a.priorityCache[profileID] = priorities
	a.statusMu.Unlock()
	return priorities
}

// ListProjectComponents returns the Jira components configured for a project
// key, as cached by the last sync. The list is used to populate the component
// field in the requirement create/edit form. Returns an empty slice (not nil)
// when no options are cached.
func (a *App) ListProjectComponents(profileID, projectKey string) ([]string, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.ListProjectFieldOptions(profileID, projectKey, "component")
}

// ListProjectFixVersions returns the Jira fix versions configured for a
// project key, as cached by the last sync. The list is used to populate the
// fix version field in the requirement create/edit form. Returns an empty
// slice (not nil) when no options are cached.
func (a *App) ListProjectFixVersions(profileID, projectKey string) ([]string, error) {
	if err := a.requireStore(); err != nil {
		return nil, err
	}
	return a.repo.ListProjectFieldOptions(profileID, projectKey, "fixversion")
}

// isLocalTestKey reports whether a Test key is a not-yet-committed local
// placeholder (the "NEW-N" keys minted by import / interactive create, FR-1).
// Such Tests don't exist in Jira yet, so per-Test Jira fetches (steps, custom
// fields, transitions, metadata) must serve local data or empty rather than
// calling Jira, which would 400 on the unknown key.
func isLocalTestKey(key string) bool {
	return strings.HasPrefix(key, "NEW-")
}

// mergeStatuses returns the workflow statuses in order, then any synced statuses
// not already listed (sorted) — leading with the authoritative workflow order
// while still surfacing anything unexpected in the data.
func mergeStatuses(workflow, synced []string) []string {
	seen := make(map[string]struct{}, len(workflow)+len(synced))
	out := make([]string, 0, len(workflow)+len(synced))
	for _, s := range workflow {
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	extra := []string{}
	for _, s := range synced {
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		extra = append(extra, s)
	}
	sort.Strings(extra)
	return append(out, extra...)
}

// GetTest returns one Test by its Jira key. For tests not in the local cache
// (e.g. cross-project members), it falls back to a live Jira fetch so the
// detail panel can show real data (FR-2).
func (a *App) GetTest(profileID, key string) (testrepo.TestCase, error) {
	if err := a.requireStore(); err != nil {
		return testrepo.TestCase{}, err
	}
	tc, err := a.repo.GetTest(profileID, key)
	if err == nil {
		return tc, nil
	}
	if !errors.Is(err, testrepo.ErrNotFound) || isLocalTestKey(key) {
		return testrepo.TestCase{}, err
	}
	// Not in cache and not a local placeholder -- try a live Jira fetch so
	// cross-project member Tests can be shown in the detail panel.
	p, profileErr := a.profiles.Get(profileID)
	if profileErr != nil {
		return testrepo.TestCase{}, err // return original ErrNotFound
	}
	if p.JiraURL == "demo" {
		return testrepo.TestCase{}, err // demo profiles have no real Jira to call
	}
	token, credErr := a.creds.Load(profileID)
	if credErr != nil {
		return testrepo.TestCase{}, err // return original ErrNotFound
	}
	t, fetchErr := jira.NewClient(p.JiraURL, token, tlsOptions(p)...).GetTestFields(a.ctx, key)
	if fetchErr != nil {
		return testrepo.TestCase{}, err // return original ErrNotFound on live failure
	}
	return testrepo.TestCase{
		Key:         t.Key,
		ID:          t.ID,
		Summary:     t.Summary,
		Description: t.Description,
		Status:      t.Status,
		Priority:    t.Priority,
		Labels:      t.Labels,
		Components:  t.Components,
		Updated:     t.Updated,
		FolderID:    t.FolderID,
		ExecType:    t.ExecType,
		FixVersions: t.FixVersions,
	}, nil
}

// GetTestMeta fetches a Test's created / creator / updated / last-updated-by
// metadata from Jira for the detail summary (FR-2). Fetched lazily on detail
// open (like steps / custom fields) rather than stored, since it isn't needed
// for browsing.
func (a *App) GetTestMeta(profileID, testKey string) (jira.TestMeta, error) {
	if err := a.requireStore(); err != nil {
		return jira.TestMeta{}, err
	}
	if isLocalTestKey(testKey) {
		// Uncommitted local Test — no Jira issue metadata yet.
		return jira.TestMeta{}, nil
	}
	p, err := a.profiles.Get(profileID)
	if err != nil {
		return jira.TestMeta{}, err
	}
	token, err := a.creds.Load(profileID)
	if err != nil {
		return jira.TestMeta{}, fmt.Errorf("load credentials: %w", err)
	}
	return jira.NewClient(p.JiraURL, token).GetTestMeta(a.ctx, testKey)
}

// defaultDBPath returns <user-config-dir>/xray-test-manager/xtm.db, creating
// the directory if needed.
func defaultDBPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	appDir := filepath.Join(dir, "xray-test-manager")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(appDir, "xtm.db"), nil
}

// setupFileLogging redirects the standard log output to a file in the app
// data dir so startup output is visible without a console. Failures here
// are non-fatal — file logging is purely for visibility.
func setupFileLogging() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	appDir := filepath.Join(dir, "xray-test-manager")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return "", err
	}
	logPath := filepath.Join(appDir, "xtm.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	log.SetOutput(f)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	return logPath, nil
}
