package main

import (
	"embed"
	"encoding/json"
	goruntime "runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

// wailsConfig is the embedded wails.json, the single source of truth for the
// product version (stamped by scripts/release.ps1). productVersion() reads it
// so the About dialog never drifts from the built version.
//
//go:embed wails.json
var wailsConfig []byte

// docsURL is opened from Help → Documentation.
const docsURL = "https://docs.getxray.app/display/XRAY/REST+API"

// productVersion returns info.productVersion from the embedded wails.json, or
// "" if it can't be read.
func productVersion() string {
	var cfg struct {
		Info struct {
			ProductVersion string `json:"productVersion"`
		} `json:"info"`
	}
	if err := json.Unmarshal(wailsConfig, &cfg); err != nil {
		return ""
	}
	return cfg.Info.ProductVersion
}

func main() {
	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	err := wails.Run(&options.App{
		Title:  "Xray Test Manager",
		Width:  1280,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Menu:             appMenu(app),
		// NOTE(xtm): EnableDefaultContextMenu restores the WKWebView right-click
		// context menu (Cut/Copy/Paste/Select All) for input elements on macOS.
		// It is also harmless on Windows (WebView2 shows its own menu by default).
		// Verify on a real macOS + WKWebView build that the context menu appears
		// inside text inputs and the TextEdit submenu works as expected.
		EnableDefaultContextMenu: true,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

// appMenu builds the native menu bar. Most items emit an event the React
// frontend listens for (so the menu drives the same actions as the in-app
// buttons); Quit and Documentation are handled directly via the Wails runtime,
// and About opens an in-app dialog. The callbacks close over app so they can use
// app.ctx, which is set by the time any menu item can be clicked.
//
// On macOS the menu bar is prepended with the standard App menu
// (menu.AppMenu: About / Services / Hide / Quit, etc.) and appended with the
// standard Edit menu (menu.EditMenu: Undo / Redo / Cut / Copy / Paste /
// Select All). Without the Edit menu, Cmd+C/V/X/A and Cmd+Z/Shift+Cmd+Z do
// not route to the WKWebView responder chain and clipboard operations silently
// fail inside text inputs (RND_P_4TFINT_05-241).
//
// NOTE(xtm): The darwin-specific menu additions must be verified on a real
// macOS + Wails build. Confirm that: (a) the App menu shows the correct app
// name, (b) Cmd+C/V/X/A work in every text input, and (c) Cmd+Z/Shift+Cmd+Z
// undo/redo work in the markdown editor.
func appMenu(app *App) *menu.Menu {
	emit := func(event string) func(*menu.CallbackData) {
		return func(*menu.CallbackData) {
			if app.ctx != nil {
				runtime.EventsEmit(app.ctx, event)
			}
		}
	}

	m := menu.NewMenu()

	// On macOS, prepend the standard App menu so the menu bar is well-formed
	// (app name, Services, Hide, Quit, etc.). This is skipped on Windows/Linux
	// because AppMenu is a macOS-only Wails role.
	if goruntime.GOOS == "darwin" {
		m.Append(menu.AppMenu())
	}

	file := m.AddSubmenu("File")
	file.AddText("New Profile…", nil, emit("menu:new-profile"))
	file.AddSeparator()
	file.AddText("Sync", keys.CmdOrCtrl("r"), emit("menu:sync"))
	file.AddText("Full Resync", keys.Combo("r", keys.CmdOrCtrlKey, keys.ShiftKey), emit("menu:full-sync"))
	file.AddSeparator()
	file.AddText("Import Tests…", nil, emit("menu:import"))
	file.AddSeparator()
	file.AddText("Quit", keys.CmdOrCtrl("q"), func(*menu.CallbackData) {
		if app.ctx != nil {
			runtime.Quit(app.ctx)
		}
	})

	view := m.AddSubmenu("View")
	view.AddText("Browse", nil, emit("menu:view-browse"))
	view.AddText("Preconditions", nil, emit("menu:view-preconditions"))
	view.AddText("Requirements", nil, emit("menu:view-requirements"))
	view.AddText("Duplicates", nil, emit("menu:view-duplicates"))
	view.AddText("Gap Analysis", nil, emit("menu:view-gapanalysis"))
	view.AddText("Test Calls", nil, emit("menu:view-testcalls"))
	view.AddText("Dashboard", nil, emit("menu:view-dashboard"))
	view.AddText("Traceability", nil, emit("menu:view-traceability"))
	view.AddText("Containers", nil, emit("menu:view-plans"))

	tools := m.AddSubmenu("Tools")
	tools.AddText("Sync History", nil, emit("menu:sync-history"))
	tools.AddText("Diagnostics", nil, emit("menu:diagnostics"))

	help := m.AddSubmenu("Help")
	help.AddText("Documentation", nil, func(*menu.CallbackData) {
		if app.ctx != nil {
			runtime.BrowserOpenURL(app.ctx, docsURL)
		}
	})
	help.AddSeparator()
	help.AddText("About Xray Test Manager", nil, emit("menu:about"))

	// On macOS, append the standard Edit menu so clipboard keyboard shortcuts
	// (Cmd+C/V/X/A, Cmd+Z, Shift+Cmd+Z) are wired into the responder chain
	// and reach the focused WKWebView input. Without this, WKWebView silently
	// consumes the key events but does not act on them.
	if goruntime.GOOS == "darwin" {
		m.Append(menu.EditMenu())
	}

	return m
}
