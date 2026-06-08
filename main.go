package main

import (
	"embed"
	"encoding/json"

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
func appMenu(app *App) *menu.Menu {
	emit := func(event string) func(*menu.CallbackData) {
		return func(*menu.CallbackData) {
			if app.ctx != nil {
				runtime.EventsEmit(app.ctx, event)
			}
		}
	}

	m := menu.NewMenu()

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
	view.AddText("Dashboard", nil, emit("menu:view-dashboard"))
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

	return m
}
