package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"SnakeMedia/internal/accounts"
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
	"SnakeMedia/internal/providers/muse"
	"SnakeMedia/internal/proxy"
	"SnakeMedia/internal/services"
	"SnakeMedia/internal/updater"
	"SnakeMedia/internal/window"
)

// App is the Wails-bound controller. Every exported method is exposed to the
// frontend, so keep internal helpers unexported.
type App struct {
	ctx                 context.Context
	info                buildinfo.Info
	cfg                 *config.Manager
	system              *services.SystemService
	settings            *services.SettingsService
	accounts            *accounts.Service
	providers           *providers.Set
	providerEnum        accounts.ProviderEnum
	flow                *flow.Provider
	muse                *muse.Provider
	media               *media.Store
	batchOutputDefaults map[accounts.Provider]string
	nodes               *engine.Registry
	dispatcher          *events.Dispatcher
	requests            *services.RequestRegistry
	updates             *services.UpdateService
	// startupNotice is shown once the window is up (config reset to defaults).
	startupNotice string
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
	var notice string
	if cfgErr != nil {
		log.Warn("configuration could not be loaded, using defaults", "error", cfgErr)
		notice = "Không đọc được file cài đặt, app đang dùng cài đặt mặc định. Hãy kiểm tra lại trong Cài đặt.\n\n" + cfgErr.Error()
		if errors.Is(cfgErr, config.ErrNewerConfig) {
			notice = config.ErrNewerConfig.Error()
		}
	}
	log.Info("starting application",
		"name", info.Name, "version", info.Version, "commit", info.Commit, "dataDir", p.DataDir)

	// Every provider joins through one Plugin line; its accounts Site, nodes,
	// option sources and background work come with it.
	set := providers.Prepare(cfgManager, flow.Plugin(), muse.Plugin())
	accts := accounts.New(p.AccountsDir, cfgManager, set.Sites()...)
	store := media.NewStore(p.MediaDir)
	// Files are saved or revealed only under the app's output folders (Muse
	// adds its own), folders the user picks in a dialog and the batch folders
	// remembered in the settings.
	store.AllowRoot(p.ImageOutputDir)
	store.AllowRootsFrom(func() []string { return []string{cfgManager.Get().Flow.BatchOutputDir} })
	set.Build(providers.Deps{Paths: p, Config: cfgManager, Media: store, Accounts: accts, ProductName: info.ProductName})
	nodes := engine.NewRegistry()
	if err := set.Register(nodes, store.SaveNode()); err != nil {
		return nil, err
	}
	return &App{
		info:                info,
		cfg:                 cfgManager,
		system:              services.NewSystemService(info),
		settings:            services.NewSettingsService(cfgManager),
		accounts:            accts,
		providers:           set,
		providerEnum:        accounts.Providers(set.Sites()...),
		flow:                providers.Find[*flow.Provider](set),
		muse:                providers.Find[*muse.Provider](set),
		media:               store,
		batchOutputDefaults: map[accounts.Provider]string{accounts.ProviderFlow: p.ImageOutputDir, accounts.ProviderMuse: p.MuseOutputDir},
		nodes:               nodes,
		requests:            services.NewRequestRegistry(),
		startupNotice:       notice,
		updates:             services.NewUpdateService(updater.New(info.Name, info.Version, info.UpdateRepository, p.UpdatesDir), p.LogDir),
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
	emit := func(event events.Event, data any) { a.dispatcher.Emit(event, data) }
	a.updates.Start(emit, func() { runtime.Quit(ctx) })
	a.accounts.Start(ctx, emit)
	a.providers.Start(ctx, emit)
}

func (a *App) domReady(ctx context.Context) {
	logger.Get().Info("frontend ready")
	a.showMainWindow(ctx)
	a.dispatcher.Emit(events.EventAppReady)
	if a.cfg.Get().Updates.CheckOnStartup {
		a.updates.CheckInBackground(ctx)
	}
	if a.startupNotice != "" {
		// Off the callback: the dialog waits for the user.
		go func() {
			defer logger.Recover("thông báo khởi động")
			_, _ = runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
				Type: runtime.WarningDialog, Title: a.info.ProductName, Message: a.startupNotice,
			})
		}()
	}
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
	// Stop waiting on Flow/Muse calls first: their journals keep the jobs
	// resumable, while the browsers and proxy bridges they use close below.
	if n := a.requests.CancelAll(); n > 0 {
		logger.Get().Info("cancelled running requests on shutdown", "count", n)
	}
	a.providers.Close()
	a.accounts.Close()
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

// CheckForUpdate looks for a newer release of the app.
func (a *App) CheckForUpdate() models.Response[updater.Release] {
	return logged("CheckForUpdate", a.updates.Check(a.ctx))
}

// InstallUpdate downloads and verifies the newer release, then closes the app
// so its installer can run; the installer starts the app again. Progress
// arrives as events.EventUpdateProgress.
func (a *App) InstallUpdate() models.Response[bool] {
	return logged("InstallUpdate", a.updates.Install(a.ctx))
}

// GetUpdateProgress returns the running update download's progress.
func (a *App) GetUpdateProgress() models.Response[updater.Progress] {
	return a.updates.Progress()
}

// OpenLogFolder shows the folder holding app.log.
func (a *App) OpenLogFolder() models.Response[bool] {
	return logged("OpenLogFolder", a.updates.OpenLogFolder())
}

// CheckVideoTools reports whether ffmpeg and ffprobe, which the video
// watermark removal needs, are found (Flow settings path or PATH).
func (a *App) CheckVideoTools() models.Response[media.VideoTools] {
	return models.SuccessResponse(media.CheckVideoTools(a.cfg.Get().Flow))
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

// GetAccounts lists every provider's accounts in saved order.
func (a *App) GetAccounts() models.Response[[]accounts.Account] {
	return logged("GetAccounts", a.accounts.List(""))
}

// AddAccount saves an account and opens Chrome on the provider's page to sign in.
func (a *App) AddAccount(provider accounts.Provider, name string) models.Response[accounts.Account] {
	return logged("AddAccount", a.accounts.Add(a.ctx, provider, name))
}

// ImportAccountCookies saves an account from cookies exported on a signed-in page.
func (a *App) ImportAccountCookies(provider accounts.Provider, name, cookies string) models.Response[accounts.Account] {
	return logged("ImportAccountCookies", a.accounts.ImportCookies(provider, name, cookies))
}

// ReplaceAccountCookies stores a new cookie export for an existing account.
func (a *App) ReplaceAccountCookies(id, cookies string) models.Response[accounts.Account] {
	return logged("ReplaceAccountCookies", a.accounts.ReplaceCookies(id, cookies))
}

// CheckAccount confirms an account's session and reads its usage.
func (a *App) CheckAccount(id string) models.Response[accounts.Account] {
	return logged("CheckAccount", a.accounts.Check(a.ctx, id))
}

// OpenAccountBrowser shows the account's Chrome on its provider's page.
func (a *App) OpenAccountBrowser(id string) models.Response[accounts.Account] {
	return logged("OpenAccountBrowser", a.accounts.OpenBrowser(a.ctx, id))
}

// RenameAccount changes an account's label.
func (a *App) RenameAccount(id, name string) models.Response[accounts.Account] {
	return logged("RenameAccount", a.accounts.Rename(id, name))
}

// SetAccountEnabled includes or excludes an account from work.
func (a *App) SetAccountEnabled(id string, enabled bool) models.Response[accounts.Account] {
	return logged("SetAccountEnabled", a.accounts.SetEnabled(id, enabled))
}

// SetAccountProxy assigns a proxy to an account ("" picks from the list, "none" goes direct).
func (a *App) SetAccountProxy(id, proxyLine string) models.Response[accounts.Account] {
	return logged("SetAccountProxy", a.accounts.SetProxy(id, proxyLine))
}

// RotateAccountIP asks an account's rotating proxy for a new IP.
func (a *App) RotateAccountIP(id string) models.Response[proxy.CheckResult] {
	return logged("RotateAccountIP", a.accounts.RotateIP(a.ctx, id))
}

// RemoveAccount deletes an account and its Chrome profile.
func (a *App) RemoveAccount(id string) models.Response[bool] {
	return logged("RemoveAccount", a.accounts.Remove(id))
}

// GetProxies returns the shared proxy list.
func (a *App) GetProxies() models.Response[string] {
	return a.accounts.Proxies()
}

// SaveProxies replaces the shared proxy list.
func (a *App) SaveProxies(text string) models.Response[bool] {
	return logged("SaveProxies", a.accounts.SaveProxies(text))
}

// CheckProxies tests every proxy in the list.
func (a *App) CheckProxies() models.Response[[]proxy.CheckResult] {
	return logged("CheckProxies", a.accounts.CheckProxies(a.ctx))
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

// GetBatchOutputDirectory returns the folder a provider's batch form saves
// into: the last one chosen there, else the app's default folder (created).
func (a *App) GetBatchOutputDirectory(provider accounts.Provider) models.Response[string] {
	def, ok := a.batchOutputDefaults[provider]
	if !ok {
		return models.ErrorResponse[string]("Nguồn tạo không hợp lệ.", models.CodeInvalidArgument)
	}
	if dir := batchOutputDir(a.cfg.Get(), provider); dir != "" {
		return models.SuccessResponse(dir)
	}
	if err := os.MkdirAll(def, 0o755); err != nil {
		return models.ErrorResponse[string]("Không tạo được thư mục lưu mặc định: "+err.Error(), models.CodeInternal)
	}
	return models.SuccessResponse(def)
}

// ChooseBatchOutputDirectory opens the folder picker for a provider's batch
// form and remembers the folder for its next batches.
func (a *App) ChooseBatchOutputDirectory(provider accounts.Provider) models.Response[media.DirectoryResult] {
	current := a.GetBatchOutputDirectory(provider)
	if !current.Success {
		return models.ErrorResponse[media.DirectoryResult](current.Error, current.Code)
	}
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:                "Chọn thư mục lưu",
		DefaultDirectory:     current.Data,
		CanCreateDirectories: true,
	})
	if err != nil {
		return models.ErrorResponse[media.DirectoryResult]("Không mở được hộp thoại chọn thư mục: "+err.Error(), models.CodeInternal)
	}
	if dir == "" {
		return models.SuccessResponse(media.DirectoryResult{Canceled: true})
	}
	a.media.AllowRoot(dir)
	cfg := a.cfg.Get()
	if provider == accounts.ProviderFlow {
		cfg.Flow.BatchOutputDir = dir
	} else {
		cfg.Muse.OutputDir = dir
	}
	if _, err := a.cfg.Update(cfg); err != nil {
		return models.ErrorResponse[media.DirectoryResult]("Không lưu được thư mục đã chọn: "+err.Error(), models.CodeInternal)
	}
	return models.SuccessResponse(media.DirectoryResult{Path: dir})
}

// batchOutputDir is the batch folder remembered for provider ("" for none).
func batchOutputDir(cfg config.AppConfig, provider accounts.Provider) string {
	if provider == accounts.ProviderFlow {
		return cfg.Flow.BatchOutputDir
	}
	return cfg.Muse.OutputDir
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
	extensions := a.flow.SourceVideoExtensions()
	patterns := make([]string, len(extensions))
	for i, ext := range extensions {
		patterns[i] = "*" + ext
	}
	return a.media.ImportVideo(extensions, func() (string, error) {
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

// GetNodeOptions lists the choices of a node setting (Param.OptionsFrom),
// read from the provider; selected holds the node's other settings.
func (a *App) GetNodeOptions(source string, selected map[string]string) models.Response[[]engine.Option] {
	return logged("GetNodeOptions", engine.Respond(a.nodes.Options(a.ctx, source, selected)))
}

// RunGraph runs a node graph and reports each node's outcome. CancelGeneration
// with the graph's RequestID stops it; jobs a provider already accepted keep
// running and are reported as pending.
func (a *App) RunGraph(g engine.Graph) models.Response[engine.GraphResult] {
	ctx, done := a.requests.Start(a.ctx, g.RequestID)
	defer done()
	return logged("RunGraph", engine.Respond(a.nodes.RunGraph(ctx, g)))
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

// GetMuseOptions returns what the Muse create screens may offer, as read from Muse.
func (a *App) GetMuseOptions() models.Response[muse.MuseOptions] {
	return a.muse.GetOptions()
}

// GetFlowImageTasks lists the image queue (Ảnh → All).
func (a *App) GetFlowImageTasks() models.Response[flow.ImageQueueState] {
	return logged("GetFlowImageTasks", a.flow.ListImageTasks())
}

// SubmitFlowImageTasks queues one image task per prompt.
func (a *App) SubmitFlowImageTasks(req flow.ImageTaskRequest) models.Response[[]flow.ImageTask] {
	return logged("SubmitFlowImageTasks", a.flow.SubmitImageTasks(req))
}

// RunFlowImageTasks starts held tasks (all when ids is empty).
func (a *App) RunFlowImageTasks(ids []string) models.Response[int] {
	return logged("RunFlowImageTasks", a.flow.RunImageTasks(ids))
}

// HoldFlowImageTasks pauses queued tasks (all when ids is empty).
func (a *App) HoldFlowImageTasks(ids []string) models.Response[int] {
	return logged("HoldFlowImageTasks", a.flow.HoldImageTasks(ids))
}

// CancelFlowImageTask stops a queued or running task.
func (a *App) CancelFlowImageTask(id string) models.Response[flow.ImageTask] {
	return logged("CancelFlowImageTask", a.flow.CancelImageTask(id))
}

// EditFlowImageTask changes the prompt of a task that is not running.
func (a *App) EditFlowImageTask(id, prompt string) models.Response[flow.ImageTask] {
	return logged("EditFlowImageTask", a.flow.EditImageTask(id, prompt))
}

// RetryFlowImageTasks queues failed or cancelled tasks again.
func (a *App) RetryFlowImageTasks(ids []string) models.Response[int] {
	return logged("RetryFlowImageTasks", a.flow.RetryImageTasks(ids))
}

// RemoveFlowImageTasks deletes tasks that are not running.
func (a *App) RemoveFlowImageTasks(ids []string) models.Response[int] {
	return logged("RemoveFlowImageTasks", a.flow.RemoveImageTasks(ids))
}

// ClearFinishedFlowImageTasks deletes the succeeded tasks.
func (a *App) ClearFinishedFlowImageTasks() models.Response[int] {
	return logged("ClearFinishedFlowImageTasks", a.flow.ClearFinishedImageTasks())
}

// AddFlowImageTaskReferences adds references to tasks that have not sent a request.
func (a *App) AddFlowImageTaskReferences(ids []string, refs []flow.ImageTaskReference) models.Response[int] {
	return logged("AddFlowImageTaskReferences", a.flow.AddImageTaskReferences(ids, refs))
}

// PickFlowImageReferences asks the user for reference images and imports them.
func (a *App) PickFlowImageReferences() models.Response[[]flow.ImageTaskReference] {
	paths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Chọn ảnh tham chiếu",
		Filters: []runtime.FileFilter{{DisplayName: "Ảnh Flow nhận", Pattern: strings.Join(a.flow.ReferenceFilePatterns(), ";")}},
	})
	if err != nil {
		return models.ErrorResponse[[]flow.ImageTaskReference]("Không mở được hộp thoại chọn ảnh: "+err.Error(), models.CodeInternal)
	}
	return logged("PickFlowImageReferences", a.flow.ImportImageReferences(paths))
}

// ImportFlowProjectImageReference copies one Flow project image for use as a queue reference.
func (a *App) ImportFlowProjectImageReference(accountID, projectID string, mode flow.ImageProjectMode, mediaID string) models.Response[flow.ImageTaskReference] {
	return logged("ImportFlowProjectImageReference", a.flow.ImportProjectImageReference(a.ctx, accountID, projectID, mode, mediaID))
}

// PickMuseReferences asks the user for reference images and imports them.
func (a *App) PickMuseReferences() models.Response[[]muse.MuseReference] {
	patterns := a.muse.ImageFilePatterns()
	if len(patterns) == 0 {
		// Muse's limits are read with the account; without them nothing can be checked.
		return a.muse.ImportReferences(nil)
	}
	paths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Chọn ảnh tham chiếu",
		Filters: []runtime.FileFilter{{DisplayName: "Ảnh Muse nhận", Pattern: strings.Join(patterns, ";")}},
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

// HoldMuseQueued puts the queued jobs of kind and mode (all when empty) on hold.
func (a *App) HoldMuseQueued(kind muse.MuseJobKind, mode muse.MuseJobMode) models.Response[int] {
	return logged("HoldMuseQueued", a.muse.HoldQueued(kind, mode))
}

// RetryMuseFailed restarts the failed or cancelled jobs of kind and mode (all when empty).
func (a *App) RetryMuseFailed(kind muse.MuseJobKind, mode muse.MuseJobMode) models.Response[int] {
	return logged("RetryMuseFailed", a.muse.RetryFailed(kind, mode))
}

// ClearMuseFinished clears the succeeded jobs of kind and mode (all when empty) from the queue.
func (a *App) ClearMuseFinished(kind muse.MuseJobKind, mode muse.MuseJobMode) models.Response[int] {
	return logged("ClearMuseFinished", a.muse.ClearFinished(kind, mode))
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

// RevealFile opens the system file manager with a stored or saved file
// selected (Flow and Muse alike).
func (a *App) RevealFile(path string) models.Response[bool] {
	return logged("RevealFile", a.media.Reveal(path))
}

// ListMuseLibrary lists every file finished Muse jobs made, newest first.
func (a *App) ListMuseLibrary() models.Response[[]muse.MuseLibraryItem] {
	return logged("ListMuseLibrary", a.muse.ListLibrary())
}

// ListMuseAssets lists the Muse asset library (characters, products, settings).
func (a *App) ListMuseAssets() models.Response[[]muse.MuseAsset] {
	return logged("ListMuseAssets", a.muse.ListAssets())
}

// SaveMuseAsset adds or replaces an asset of the Muse library.
func (a *App) SaveMuseAsset(asset muse.MuseAsset) models.Response[muse.MuseAsset] {
	return logged("SaveMuseAsset", a.muse.SaveAsset(asset))
}

// RemoveMuseAssets deletes assets from the Muse library.
func (a *App) RemoveMuseAssets(ids []string) models.Response[int] {
	return logged("RemoveMuseAssets", a.muse.RemoveAssets(ids))
}

// PickMuseAssetImages asks the user for an asset's images and imports them.
func (a *App) PickMuseAssetImages() models.Response[[]muse.MuseReference] {
	patterns := a.muse.ImageFilePatterns()
	if len(patterns) == 0 {
		return a.muse.ImportAssetImages(nil)
	}
	paths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Chọn ảnh tư liệu",
		Filters: []runtime.FileFilter{{DisplayName: "Ảnh Muse nhận", Pattern: strings.Join(patterns, ";")}},
	})
	if err != nil {
		return models.ErrorResponse[[]muse.MuseReference]("Không mở được hộp thoại chọn ảnh: "+err.Error(), models.CodeInternal)
	}
	return logged("PickMuseAssetImages", a.muse.ImportAssetImages(paths))
}

// PickMuseSourceVideo asks the user for a video to recreate (v2v) and
// imports it; an empty LocalURL means the dialog was closed.
func (a *App) PickMuseSourceVideo() models.Response[muse.MuseReference] {
	patterns := a.muse.VideoFilePatterns()
	if len(patterns) == 0 {
		return a.muse.ImportSourceVideo("")
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Chọn video mẫu",
		Filters: []runtime.FileFilter{{DisplayName: "Video Muse nhận", Pattern: strings.Join(patterns, ";")}},
	})
	if err != nil {
		return models.ErrorResponse[muse.MuseReference]("Không mở được hộp thoại chọn video: "+err.Error(), models.CodeInternal)
	}
	if path == "" {
		return models.SuccessResponse(muse.MuseReference{})
	}
	return logged("PickMuseSourceVideo", a.muse.ImportSourceVideo(path))
}

// CheckMusePrompt says what a prompt attaches from the asset library and
// what may go wrong.
func (a *App) CheckMusePrompt(prompt string, assetIDs []string) models.Response[muse.MusePromptCheck] {
	return a.muse.CheckPrompt(prompt, assetIDs)
}

// ListMuseTemplates lists the saved Muse prompts.
func (a *App) ListMuseTemplates() models.Response[[]muse.MusePromptTemplate] {
	return logged("ListMuseTemplates", a.muse.ListTemplates())
}

// SaveMuseTemplate adds or replaces a saved Muse prompt.
func (a *App) SaveMuseTemplate(t muse.MusePromptTemplate) models.Response[muse.MusePromptTemplate] {
	return logged("SaveMuseTemplate", a.muse.SaveTemplate(t))
}

// RemoveMuseTemplate deletes a saved Muse prompt.
func (a *App) RemoveMuseTemplate(id string) models.Response[bool] {
	return logged("RemoveMuseTemplate", a.muse.RemoveTemplate(id))
}

// CleanMuseChats sends the cleanup message on every signed-in Muse account,
// so Muse deletes its other chats; accounts with a job in progress are skipped.
func (a *App) CleanMuseChats() models.Response[[]muse.MuseCleanResult] {
	return logged("CleanMuseChats", a.muse.CleanChats(a.ctx))
}

// RemoveMuseLibraryItems forgets library items; their files stay on disk.
func (a *App) RemoveMuseLibraryItems(ids []string) models.Response[int] {
	return logged("RemoveMuseLibraryItems", a.muse.RemoveLibraryItems(ids))
}
