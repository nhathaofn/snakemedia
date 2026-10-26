package main

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"SnakeMedia/internal/buildinfo"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/engine"
	"SnakeMedia/internal/events"
	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/media"
	"SnakeMedia/internal/models"
	"SnakeMedia/internal/paths"
	"SnakeMedia/internal/providers"
	"SnakeMedia/internal/providers/flow"
	"SnakeMedia/internal/providers/flow/session"
	"SnakeMedia/internal/providers/muse"
	"SnakeMedia/internal/services"
	"SnakeMedia/internal/window"
)

// App is the Wails-bound controller. Every exported method is exposed to the
// frontend, so keep internal helpers unexported.
type App struct {
	ctx                  context.Context
	info                 buildinfo.Info
	cfg                  *config.Manager
	system               *services.SystemService
	settings             *services.SettingsService
	account              *session.Service
	flow                 *flow.Provider
	muse                 *muse.Provider
	media                *media.Store
	imageOutputDirectory string
	nodes                *engine.Registry
	dispatcher           *events.Dispatcher
	requests             *services.RequestRegistry
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

	accounts := session.NewService(p, cfgManager)
	store := media.NewStore(p.MediaDir)
	flowProvider := flow.New(accounts, cfgManager, store, info.ProductName)
	museProvider := muse.New(p.DataDir, cfgManager, store)
	nodes := engine.NewRegistry()
	for _, provider := range []providers.Provider{flowProvider, museProvider} {
		if err := nodes.Register(provider.Nodes()...); err != nil {
			return nil, err
		}
	}
	if err := nodes.Register(store.SaveNode()); err != nil {
		return nil, err
	}
	return &App{
		info:                 info,
		cfg:                  cfgManager,
		system:               services.NewSystemService(info),
		settings:             services.NewSettingsService(cfgManager),
		account:              accounts,
		flow:                 flowProvider,
		muse:                 museProvider,
		media:                store,
		imageOutputDirectory: p.ImageOutputDir,
		nodes:                nodes,
		requests:             services.NewRequestRegistry(),
	}, nil
}

// logged records a failed provider call. The frontend shows the message, but
// without this the log file would never say why Flow or an account failed.
func logged[T any](method string, res models.Response[T]) models.Response[T] {
	if !res.Success {
		logger.Get().Warn("request failed", "method", method, "code", res.Code, "error", res.Error)
	}
	return res
}

// initialConfig returns the configuration used to build window options.
func (a *App) initialConfig() config.AppConfig {
	return a.cfg.Get()
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.dispatcher = events.NewDispatcher(ctx)
	a.muse.Start(ctx, func(event events.Event, data any) { a.dispatcher.Emit(event, data) })
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
	a.muse.Close()
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
func (a *App) LaunchLoginBrowser(sessionID string) models.Response[session.LoginProcessInfo] {
	return logged("LaunchLoginBrowser", a.account.LaunchLoginBrowser(a.ctx, sessionID))
}

// OpenAccountBrowser launches Chrome with a specific account's saved profile directory.
func (a *App) OpenAccountBrowser(id string) models.Response[session.LoginProcessInfo] {
	return logged("OpenAccountBrowser", a.account.OpenAccountBrowser(a.ctx, id))
}

// CheckLoginStatus queries the Chrome CDP port to report browser connection and page status.
func (a *App) CheckLoginStatus(sessionID string) models.Response[session.LoginStatusInfo] {
	return a.account.CheckLoginStatus(a.ctx, sessionID)
}

func (a *App) CancelLogin(sessionID string) models.Response[bool] {
	return logged("CancelLogin", a.account.CancelLogin(sessionID))
}

// GetAccounts returns the list of saved accounts.
func (a *App) GetAccounts() models.Response[[]session.AccountProfile] {
	return logged("GetAccounts", a.account.GetAccounts(a.ctx))
}

// RemoveAccount deletes an account by id.
func (a *App) RemoveAccount(id string) models.Response[bool] {
	return logged("RemoveAccount", a.account.RemoveAccount(a.ctx, id))
}

// RefreshAccount queries latest credits and cookies from the active Flow tab.
func (a *App) RefreshAccount(id string) models.Response[session.AccountProfile] {
	return logged("RefreshAccount", a.account.RefreshAccount(a.ctx, id))
}

// GetImageModels queries available image generation models from Google Flow using the rotated account pool.
func (a *App) GetImageModels() models.Response[[]flow.FlowModel] {
	return logged("GetImageModels", a.flow.GetImageModels(a.ctx))
}

// GetActiveRotationCount returns the number of connected accounts with valid cookies available for rotation.
func (a *App) GetActiveRotationCount() models.Response[int] {
	return a.flow.GetActiveRotationCount(a.ctx)
}

// GenerateImages dispatches image generation using a rotated account.
func (a *App) GenerateImages(req flow.GenerateImageRequest) models.Response[[]flow.GeneratedImageItem] {
	ctx, done := a.requests.Start(a.ctx, req.RequestID)
	defer done()
	return logged("GenerateImages", a.flow.GenerateImages(ctx, req))
}

// ResumeImageGeneration checks a persisted Flow image request without creating another job.
func (a *App) ResumeImageGeneration(id string) models.Response[[]flow.GeneratedImageItem] {
	ctx, done := a.requests.Start(a.ctx, id)
	defer done()
	return logged("ResumeImageGeneration", a.flow.ResumeImageGeneration(ctx, id))
}

// ListPendingImageGenerations restores one studio's image jobs that can be reconciled after a restart.
func (a *App) ListPendingImageGenerations(mode flow.ImageProjectMode) models.Response[[]flow.GeneratedImageItem] {
	return a.flow.ListPendingImageGenerations(mode)
}

// SaveMedia asks the user for a folder and copies a downloaded image or video there.
func (a *App) SaveMedia(localURL string) models.Response[media.SaveResult] {
	return a.media.Save(localURL, func(defaultDir string) (string, error) {
		return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
			Title:                "Chọn thư mục lưu",
			DefaultDirectory:     defaultDir,
			CanCreateDirectories: true,
		})
	})
}

// GetDefaultImageOutputDirectory returns the per-user default folder for Image > All.
func (a *App) GetDefaultImageOutputDirectory() models.Response[string] {
	if err := os.MkdirAll(a.imageOutputDirectory, 0o755); err != nil {
		return models.ErrorResponse[string]("Không tạo được thư mục lưu ảnh mặc định: "+err.Error(), models.CodeInternal)
	}
	return models.SuccessResponse(a.imageOutputDirectory)
}

// ChooseImageOutputDirectory opens the native folder picker for Image > All.
func (a *App) ChooseImageOutputDirectory(defaultDir string) models.Response[media.DirectoryResult] {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:                "Chọn thư mục lưu ảnh",
		DefaultDirectory:     defaultDir,
		CanCreateDirectories: true,
	})
	if err != nil {
		return models.ErrorResponse[media.DirectoryResult]("Không mở được hộp thoại chọn thư mục: "+err.Error(), models.CodeInternal)
	}
	if dir == "" {
		return models.SuccessResponse(media.DirectoryResult{Canceled: true})
	}
	return models.SuccessResponse(media.DirectoryResult{Path: dir})
}

// SaveImageToTaskFolder saves one generated image under the selected root and
// its prompt-batch folder.
func (a *App) SaveImageToTaskFolder(localURL, outputDir, taskID string) models.Response[media.SaveResult] {
	return a.media.SaveImageToTaskFolder(localURL, outputDir, taskID)
}

// GetStudioConfig returns the choices and limits shared by the Flow studios.
func (a *App) GetStudioConfig() models.Response[flow.StudioConfig] {
	return logged("GetStudioConfig", a.flow.GetStudioConfig(a.ctx))
}

// ListFlowImageProjects lists projects from connected Flow accounts for the
// image reference library.
func (a *App) ListFlowImageProjects(mode flow.ImageProjectMode) models.Response[[]flow.FlowImageProject] {
	return logged("ListImageProjects", a.flow.ListImageProjects(a.ctx, mode))
}

// ListFlowProjectImages lists image files in one Flow project.
func (a *App) ListFlowProjectImages(accountID, projectID string, mode flow.ImageProjectMode) models.Response[[]flow.FlowProjectImage] {
	return logged("ListProjectImages", a.flow.ListProjectImages(a.ctx, accountID, projectID, mode))
}

// GetFlowProjectImageReference downloads one Flow project image for generation.
func (a *App) GetFlowProjectImageReference(accountID, projectID string, mode flow.ImageProjectMode, mediaID string) models.Response[flow.FlowProjectImageReference] {
	return logged("GetProjectImageReference", a.flow.GetProjectImageReference(a.ctx, accountID, projectID, mode, mediaID))
}

// GetVideoModels lists the text-to-video model families of the next account in rotation.
func (a *App) GetVideoModels() models.Response[[]flow.FlowVideoModel] {
	return logged("GetVideoModels", a.flow.GetVideoModels(a.ctx))
}

// GenerateVideos creates videos from a prompt and waits for Flow to finish them.
func (a *App) GenerateVideos(req flow.GenerateVideoRequest) models.Response[[]flow.GeneratedVideoItem] {
	ctx, done := a.requests.Start(a.ctx, req.RequestID)
	defer done()
	return logged("GenerateVideos", a.flow.GenerateVideos(ctx, req))
}

// ResumeVideoGeneration checks a persisted Flow video request without creating another job.
func (a *App) ResumeVideoGeneration(id string) models.Response[[]flow.GeneratedVideoItem] {
	ctx, done := a.requests.Start(a.ctx, id)
	defer done()
	return logged("ResumeVideoGeneration", a.flow.ResumeVideoGeneration(ctx, id))
}

// ListPendingVideoGenerations restores video jobs that can be reconciled after a restart.
func (a *App) ListPendingVideoGenerations() models.Response[[]flow.GeneratedVideoItem] {
	return a.flow.ListPendingVideoGenerations()
}

// PickVideo asks the user for a video file and copies it into the media folder.
func (a *App) PickVideo() models.Response[media.ImportResult] {
	patterns := make([]string, 0, len(media.VideoTypes))
	for ext := range media.VideoTypes {
		patterns = append(patterns, "*"+ext)
	}
	slices.Sort(patterns)
	return a.media.ImportVideo(func() (string, error) {
		return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
			Title:   "Chọn video gốc",
			Filters: []runtime.FileFilter{{DisplayName: "Video", Pattern: strings.Join(patterns, ";")}},
		})
	})
}

// UploadSourceVideo uploads an imported source video to Google Flow via Scotty.
func (a *App) UploadSourceVideo(localURL string) models.Response[flow.UploadedSourceVideo] {
	return logged("UploadSourceVideo", a.flow.UploadSourceVideo(a.ctx, localURL))
}

// GetEditModels lists the model families that can edit a video for the next account.
func (a *App) GetEditModels() models.Response[[]flow.FlowEditModel] {
	return logged("GetEditModels", a.flow.GetEditModels(a.ctx))
}

// EditVideos rewrites an imported video with a prompt and waits for Flow to finish.
func (a *App) EditVideos(req flow.EditVideoRequest) models.Response[[]flow.GeneratedVideoItem] {
	ctx, done := a.requests.Start(a.ctx, req.RequestID)
	defer done()
	return logged("EditVideos", a.flow.EditVideos(ctx, req))
}

// ListNodes describes every node type the engine can run (for the node editor).
func (a *App) ListNodes() models.Response[[]engine.Spec] {
	return models.SuccessResponse(a.nodes.Specs())
}

// ListVideoEditJobs exposes the persisted edit journal for explicit recovery.
func (a *App) ListVideoEditJobs() models.Response[[]flow.GeneratedVideoItem] {
	return logged("ListVideoEditJobs", a.flow.ListVideoEditJobs(a.ctx))
}

// ResumeVideoEdit follows an existing mutation without submitting another edit.
func (a *App) ResumeVideoEdit(id string) models.Response[[]flow.GeneratedVideoItem] {
	ctx, done := a.requests.Start(a.ctx, id)
	defer done()
	return logged("ResumeVideoEdit", a.flow.ResumeVideoEdit(ctx, id))
}

// CancelGeneration stops waiting on a running generate, edit or resume call.
// A job Flow already accepted keeps running there and can be checked again
// with the same request ID; nothing is billed twice.
func (a *App) CancelGeneration(requestID string) models.Response[bool] {
	return models.SuccessResponse(a.requests.Cancel(requestID))
}

// GetMuseOptions returns the ratios and limits of the Muse create screen.
func (a *App) GetMuseOptions() models.Response[muse.MuseOptions] {
	return a.muse.GetOptions()
}

// GetMuseAccounts lists saved Muse accounts.
func (a *App) GetMuseAccounts() models.Response[[]muse.MuseAccount] {
	return logged("GetMuseAccounts", a.muse.GetAccounts())
}

// AddMuseAccount saves a Muse account and opens Chrome for the user to sign in.
func (a *App) AddMuseAccount(name string) models.Response[muse.MuseAccount] {
	return logged("AddMuseAccount", a.muse.AddAccount(a.ctx, name))
}

// ImportMuseCookies saves a Muse account from cookies exported on muse.ai.
func (a *App) ImportMuseCookies(name, cookies string) models.Response[muse.MuseAccount] {
	return logged("ImportMuseCookies", a.muse.ImportCookies(name, cookies))
}

// ReplaceMuseCookies stores a new cookie export for an existing Muse account.
func (a *App) ReplaceMuseCookies(id, cookies string) models.Response[muse.MuseAccount] {
	return logged("ReplaceMuseCookies", a.muse.ReplaceCookies(id, cookies))
}

// CheckMuseAccount confirms an account's Muse session and reads its quota.
func (a *App) CheckMuseAccount(id string) models.Response[muse.MuseAccount] {
	return logged("CheckMuseAccount", a.muse.CheckAccount(a.ctx, id))
}

// OpenMuseAccountBrowser shows the account's Chrome on muse.ai.
func (a *App) OpenMuseAccountBrowser(id string) models.Response[muse.MuseAccount] {
	return logged("OpenMuseAccountBrowser", a.muse.OpenAccountBrowser(a.ctx, id))
}

// SetMuseAccountEnabled includes or excludes an account from the Muse queue.
func (a *App) SetMuseAccountEnabled(id string, enabled bool) models.Response[muse.MuseAccount] {
	return logged("SetMuseAccountEnabled", a.muse.SetAccountEnabled(id, enabled))
}

// RemoveMuseAccount deletes a Muse account and its Chrome profile.
func (a *App) RemoveMuseAccount(id string) models.Response[bool] {
	return logged("RemoveMuseAccount", a.muse.RemoveAccount(id))
}

// PickMuseReferences asks the user for reference images and imports them.
func (a *App) PickMuseReferences() models.Response[[]muse.MuseReference] {
	paths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Chọn ảnh tham chiếu",
		Filters: []runtime.FileFilter{{DisplayName: "Ảnh", Pattern: strings.Join(muse.LocalImageTypes(), ";")}},
	})
	if err != nil {
		return models.ErrorResponse[[]muse.MuseReference]("Không mở được hộp thoại chọn ảnh: "+err.Error(), models.CodeInternal)
	}
	return logged("PickMuseReferences", a.muse.ImportReferences(paths))
}

// SubmitMuseJobs queues one Muse job per prompt.
func (a *App) SubmitMuseJobs(req muse.MuseJobRequest) models.Response[[]muse.MuseJob] {
	return logged("SubmitMuseJobs", a.muse.SubmitJobs(req))
}

// ListMuseJobs returns the Muse queue, newest first.
func (a *App) ListMuseJobs() models.Response[[]muse.MuseJob] {
	return a.muse.ListJobs()
}

// CancelMuseJob stops a queued or running Muse job.
func (a *App) CancelMuseJob(id string) models.Response[muse.MuseJob] {
	return logged("CancelMuseJob", a.muse.CancelJob(id))
}

// RetryMuseJob sends a failed or cancelled Muse job again in a new chat.
func (a *App) RetryMuseJob(id string) models.Response[muse.MuseJob] {
	return logged("RetryMuseJob", a.muse.RetryJob(id))
}

// RemoveMuseJob deletes a Muse job that is not running.
func (a *App) RemoveMuseJob(id string) models.Response[bool] {
	return logged("RemoveMuseJob", a.muse.RemoveJob(id))
}

// RunMuseJobs starts queued jobs that were placed on hold.
func (a *App) RunMuseJobs(ids []string) models.Response[int] {
	return logged("RunMuseJobs", a.muse.RunJobs(ids))
}

// HoldMuseQueued puts all queued jobs on hold.
func (a *App) HoldMuseQueued() models.Response[int] {
	return logged("HoldMuseQueued", a.muse.HoldQueued())
}

// RetryMuseFailed restarts all failed or cancelled jobs.
func (a *App) RetryMuseFailed() models.Response[int] {
	return logged("RetryMuseFailed", a.muse.RetryFailed())
}

// ClearMuseFinished clears all succeeded jobs from the queue.
func (a *App) ClearMuseFinished() models.Response[int] {
	return logged("ClearMuseFinished", a.muse.ClearFinished())
}

// RemoveMuseJobs deletes multiple jobs from the queue.
func (a *App) RemoveMuseJobs(ids []string) models.Response[int] {
	return logged("RemoveMuseJobs", a.muse.RemoveJobs(ids))
}

// EditAndRetryMuseJob modifies a job prompt and re-queues it.
func (a *App) EditAndRetryMuseJob(id, prompt string) models.Response[muse.MuseJob] {
	return logged("EditAndRetryMuseJob", a.muse.EditAndRetry(id, prompt))
}

// OpenMuseOutputFolder opens the folder where completed files are saved.
func (a *App) OpenMuseOutputFolder() models.Response[bool] {
	return logged("OpenMuseOutputFolder", a.muse.OpenOutputFolder())
}

// RevealMuseFile opens the system file manager and selects the file.
func (a *App) RevealMuseFile(path string) models.Response[bool] {
	return logged("RevealMuseFile", a.muse.RevealFile(path))
}

// GetMuseProxies returns the raw proxy list text.
func (a *App) GetMuseProxies() models.Response[string] {
	return a.muse.GetProxies()
}

// SaveMuseProxies saves the raw proxy list text.
func (a *App) SaveMuseProxies(text string) models.Response[bool] {
	return logged("SaveMuseProxies", a.muse.SaveProxies(text))
}

// CheckMuseProxies tests all proxies in the list.
func (a *App) CheckMuseProxies() models.Response[[]muse.ProxyCheckResult] {
	return logged("CheckMuseProxies", a.muse.CheckProxies(a.ctx))
}

// SetMuseAccountProxy configures a specific proxy for an account ("" for auto, "none" for direct).
func (a *App) SetMuseAccountProxy(id, proxyLine string) models.Response[muse.MuseAccount] {
	return logged("SetMuseAccountProxy", a.muse.SetAccountProxy(id, proxyLine))
}

// RotateMuseAccountIP requests a new IP for a rotating proxy assigned to an account.
func (a *App) RotateMuseAccountIP(id string) models.Response[muse.ProxyCheckResult] {
	return logged("RotateMuseAccountIP", a.muse.RotateAccountIP(a.ctx, id))
}

// GetMuseStudio returns studio projects, tagged assets and templates.
func (a *App) GetMuseStudio() models.Response[muse.StudioData] {
	return a.muse.GetStudio()
}

// SaveMuseStudio saves studio projects, tagged assets and templates.
func (a *App) SaveMuseStudio(data muse.StudioData) models.Response[bool] {
	return logged("SaveMuseStudio", a.muse.SaveStudio(data))
}

// CreateMuseStudioProject creates a new studio project.
func (a *App) CreateMuseStudioProject(name string) models.Response[muse.StudioProject] {
	return logged("CreateMuseStudioProject", a.muse.CreateStudioProject(name))
}

// SetMuseProjectPaused pauses or resumes execution for a project.
func (a *App) SetMuseProjectPaused(id string, paused bool) models.Response[bool] {
	return logged("SetMuseProjectPaused", a.muse.SetProjectPaused(id, paused))
}

// SetMuseActiveProject sets the current active project.
func (a *App) SetMuseActiveProject(id string) models.Response[bool] {
	return logged("SetMuseActiveProject", a.muse.SetActiveProject(id))
}

// AddMuseStudioAsset adds a tagged reference image asset to the studio.
func (a *App) AddMuseStudioAsset(asset muse.StudioAsset) models.Response[muse.StudioAsset] {
	return logged("AddMuseStudioAsset", a.muse.AddStudioAsset(asset))
}

// RemoveMuseStudioAsset removes an asset from the studio.
func (a *App) RemoveMuseStudioAsset(id string) models.Response[bool] {
	return logged("RemoveMuseStudioAsset", a.muse.RemoveStudioAsset(id))
}

// UpdateMuseStudioAsset modifies an existing asset in the studio.
func (a *App) UpdateMuseStudioAsset(asset muse.StudioAsset) models.Response[muse.StudioAsset] {
	return logged("UpdateMuseStudioAsset", a.muse.UpdateStudioAsset(asset))
}

// MergeMuseVideos combines multiple video files into one output.
func (a *App) MergeMuseVideos(videoPaths []string, customName string) models.Response[string] {
	return logged("MergeMuseVideos", a.muse.MergeVideos(a.ctx, videoPaths, customName))
}

// SubmitMuseProduction queues a new multi-scene production.
func (a *App) SubmitMuseProduction(prod muse.Production) models.Response[muse.Production] {
	return logged("SubmitMuseProduction", a.muse.SubmitProduction(prod))
}

// ListMuseProductions returns all saved productions.
func (a *App) ListMuseProductions() models.Response[[]muse.Production] {
	return a.muse.ListProductions()
}

// ApproveMuseProductionBoard approves a storyboard under review.
func (a *App) ApproveMuseProductionBoard(id string) models.Response[bool] {
	return logged("ApproveMuseProductionBoard", a.muse.ApproveProductionBoard(id))
}

// RetryMuseProduction restarts failed stages of a production.
func (a *App) RetryMuseProduction(id string) models.Response[bool] {
	return logged("RetryMuseProduction", a.muse.RetryProduction(id))
}

// RedrawMuseProductionBoard clears current storyboard and draws again.
func (a *App) RedrawMuseProductionBoard(id string) models.Response[bool] {
	return logged("RedrawMuseProductionBoard", a.muse.RedrawProductionBoard(id))
}

// RemoveMuseProduction removes a production from queue.
func (a *App) RemoveMuseProduction(id string) models.Response[bool] {
	return logged("RemoveMuseProduction", a.muse.RemoveProduction(id))
}
