package main

import (
	"embed"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/buildinfo"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/engine"
	"SnakeMedia/internal/events"
	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/models"
	"SnakeMedia/internal/providers/flow"
	"SnakeMedia/internal/providers/flow/chrome"
	"SnakeMedia/internal/providers/muse"
)

//go:embed all:frontend/dist
var assets embed.FS

// wails.json is the single source of truth for application name and version.
//
//go:embed wails.json
var manifest []byte

func main() {
	if err := run(); err != nil {
		logger.Get().Error("application exited with error", "error", err)
		// A GUI build has no console: without this the app would just not open.
		showFatal(startupFailure(err, logger.FilePath()))
		_ = logger.Close()
		os.Exit(1)
	}
}

func run() error {
	info, err := buildinfo.Parse(manifest)
	if err != nil {
		return err
	}

	app, err := NewApp(info)
	if err != nil {
		return err
	}
	cfg := app.initialConfig()

	return wails.Run(&options.App{
		Title: info.ProductName,
		// Placeholder size: the real size depends on the primary screen, which is only
		// known once the runtime is up. The window starts hidden and is sized to
		// cfg.WindowScale of that screen, centred and shown in App.domReady.
		Width:       config.MinWindowWidth,
		Height:      config.MinWindowHeight,
		MinWidth:    config.MinWindowWidth,
		MinHeight:   config.MinWindowHeight,
		StartHidden: true,
		// The frontend renders its own title bar (components/layout/TitleBar).
		Frameless: true,
		AssetServer: &assetserver.Options{
			Assets:     assets,
			Middleware: app.media.Middleware,
		},
		BackgroundColour:   windowBackground(cfg.Theme),
		OnStartup:          app.startup,
		OnDomReady:         app.domReady,
		OnShutdown:         app.shutdown,
		SingleInstanceLock: singleInstanceLock(info, app),
		Bind: []interface{}{
			app,
		},
		// Enums generated into frontend/wailsjs/go/models.ts.
		EnumBind: []interface{}{
			config.AllThemes,
			config.AllLogLevels,
			events.AllEvents,
			models.AllCodes,
			app.providerEnum,
			accounts.AllStatuses,
			accounts.AllSources,
			engine.AllPortTypes,
			engine.AllParamKinds,
			chrome.AllTiers,
			flow.AllImageQualities,
			flow.AllImageProjectModes,
			flow.AllImageTaskStatuses,
			config.AllMuseModes,
			config.AllImageFormats,
			muse.AllMuseJobKinds,
			muse.AllMuseJobModes,
			muse.AllMuseJobStatuses,
			muse.AllMuseRefRoles,
			muse.AllMuseRefModes,
		},
	})
}

// startupFailure is the title and text shown when the app cannot start.
func startupFailure(err error, logFile string) (title, message string) {
	message = "Ứng dụng không khởi động được:\n\n" + err.Error()
	if logFile != "" {
		message += "\n\nChi tiết trong file log:\n" + logFile
	}
	return "Không mở được ứng dụng", message
}

// windowBackground is painted before the webview loads, avoiding a flash of the
// wrong colour. Keep in sync with --bg-primary in frontend/src/styles/tokens.css.
func windowBackground(theme config.Theme) *options.RGBA {
	if theme == config.ThemeLight {
		return &options.RGBA{R: 248, G: 250, B: 252, A: 255}
	}
	return &options.RGBA{R: 15, G: 23, B: 42, A: 255}
}
