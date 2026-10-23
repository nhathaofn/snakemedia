package main

import (
	"context"
	"fmt"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"SnakeMedia/internal/buildinfo"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/events"
	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/models"
	"SnakeMedia/internal/paths"
	"SnakeMedia/internal/services"
	"SnakeMedia/internal/window"
)

// App is the Wails-bound controller. Every exported method is exposed to the
// frontend, so keep internal helpers unexported.
type App struct {
	ctx        context.Context
	info       buildinfo.Info
	cfg        *config.Manager
	system     *services.SystemService
	settings   *services.SettingsService
	account    *services.AccountService
	dispatcher *events.Dispatcher
}

// NewApp resolves storage paths, loads configuration and initialises logging.
func NewApp(info buildinfo.Info) (*App, error) {
	p, err := paths.Resolve(info.Name)
	if err != nil {
		return nil, fmt.Errorf("resolve app paths: %w", err)
	}

	cfgManager, cfgErr := config.NewManager(p.ConfigFile)
	cfg := cfgManager.Get()

	log, logErr := logger.Init(logger.Options{Level: string(cfg.LogLevel), Dir: p.LogDir})
	if logErr != nil {
		log.Warn("file logging unavailable", "error", logErr)
	}
	if cfgErr != nil {
		log.Warn("configuration could not be loaded, using defaults", "error", cfgErr)
	}
	log.Info("starting application",
		"name", info.Name, "version", info.Version, "commit", info.Commit, "dataDir", p.DataDir)

	return &App{
		info:     info,
		cfg:      cfgManager,
		system:   services.NewSystemService(info),
		settings: services.NewSettingsService(cfgManager),
		account:  services.NewAccountService(p, cfgManager),
	}, nil
}

// initialConfig returns the configuration used to build window options.
func (a *App) initialConfig() config.AppConfig {
	return a.cfg.Get()
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.dispatcher = events.NewDispatcher(ctx)
}

func (a *App) domReady(ctx context.Context) {
	logger.Get().Info("frontend ready")
	a.showMainWindow(ctx)
	a.dispatcher.Emit(events.EventAppReady)
}

// showMainWindow sizes the (initially hidden) window to the configured fraction of
// the primary screen, centres it and shows it. The window is always shown, even
// when the screen cannot be queried, so the app never stays invisible.
func (a *App) showMainWindow(ctx context.Context) {
	defer runtime.WindowShow(ctx)

	scale := a.cfg.Get().WindowScale
	screens, err := runtime.ScreenGetAll(ctx)
	if err != nil {
		logger.Get().Warn("cannot query screens, keeping default window size", "error", err)
	}
	if screen, ok := window.PrimaryScreen(screens); ok {
		size := window.Fit(screen, scale, window.Size{Width: config.MinWindowWidth, Height: config.MinWindowHeight})
		runtime.WindowSetSize(ctx, size.Width, size.Height)
		logger.Get().Info("window sized from primary screen",
			"screen", fmt.Sprintf("%dx%d", screen.Width, screen.Height),
			"scale", scale,
			"window", fmt.Sprintf("%dx%d", size.Width, size.Height))
	} else if err == nil {
		logger.Get().Warn("no usable screen reported, keeping default window size")
	}
	runtime.WindowCenter(ctx)
}

func (a *App) shutdown(_ context.Context) {
	a.account.Close()
	logger.Get().Info("shutting down")
	_ = logger.Close()
}

// onSecondInstanceLaunch focuses the existing window when the user starts the app again.
func (a *App) onSecondInstanceLaunch(data options.SecondInstanceData) {
	logger.Get().Info("second instance launch blocked", "args", data.Args)
	if a.ctx == nil {
		return
	}
	runtime.WindowUnminimise(a.ctx)
	runtime.Show(a.ctx)
}

// GetAppInfo exposes application identity and build metadata.
func (a *App) GetAppInfo() models.Response[buildinfo.Info] {
	return a.system.GetAppInfo(a.ctx)
}

// GetSystemInfo exposes host runtime information.
func (a *App) GetSystemInfo() models.Response[services.SystemInfo] {
	return a.system.GetSystemInfo(a.ctx)
}

// GetConfig exposes the current user configuration.
func (a *App) GetConfig() models.Response[config.AppConfig] {
	return a.settings.GetConfig(a.ctx)
}

// UpdateConfig validates and persists configuration, then notifies listeners.
func (a *App) UpdateConfig(newCfg config.AppConfig) models.Response[config.AppConfig] {
	res := a.settings.UpdateConfig(a.ctx, newCfg)
	if res.Success {
		a.dispatcher.Emit(events.EventConfigUpdated, res.Data)
	}
	return res
}

// LaunchLoginBrowser launches a dedicated Chrome instance for Google sign-in.
func (a *App) LaunchLoginBrowser(sessionID string) models.Response[services.LoginProcessInfo] {
	return a.account.LaunchLoginBrowser(a.ctx, sessionID)
}

// OpenAccountBrowser launches Chrome with a specific account's saved profile directory.
func (a *App) OpenAccountBrowser(id string) models.Response[services.LoginProcessInfo] {
	return a.account.OpenAccountBrowser(a.ctx, id)
}

// CheckLoginStatus queries the Chrome CDP port to report browser connection and page status.
func (a *App) CheckLoginStatus(sessionID string) models.Response[services.LoginStatusInfo] {
	return a.account.CheckLoginStatus(a.ctx, sessionID)
}

func (a *App) CancelLogin(sessionID string) models.Response[bool] {
	return a.account.CancelLogin(sessionID)
}

// GetAccounts returns the list of saved accounts.
func (a *App) GetAccounts() models.Response[[]services.AccountProfile] {
	return a.account.GetAccounts(a.ctx)
}

// RemoveAccount deletes an account by id.
func (a *App) RemoveAccount(id string) models.Response[bool] {
	return a.account.RemoveAccount(a.ctx, id)
}

// RefreshAccount queries latest credits and cookies from the active Flow tab.
func (a *App) RefreshAccount(id string) models.Response[services.AccountProfile] {
	return a.account.RefreshAccount(a.ctx, id)
}
