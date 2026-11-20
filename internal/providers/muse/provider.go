// Package muse is the muse.ai provider: a durable job queue spread over the
// Muse accounts of the shared accounts service, and image/video generation
// through Muse's own chat client in each account's tab.
package muse

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/events"
	"SnakeMedia/internal/jobs"
	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/media"
	"SnakeMedia/internal/models"
	"SnakeMedia/internal/tools"
)

// pumpInterval is how often the queue is re-planned without any trigger, so
// paused accounts resume and quota resets are noticed.
const pumpInterval = 30 * time.Second

// apiCapacityTTL and apiCapacityRetryCooldown bound how often SnakeMedia
// queries muse2api's /readyz to scale concurrency across its active accounts.
const (
	apiCapacityTTL           = 15 * time.Second
	apiCapacityRetryCooldown = 5 * time.Second
)

// Provider owns the Muse job queue; accounts and their browsers belong to
// the accounts service.
type Provider struct {
	submitMu sync.Mutex // serializes submission IDs and batch numbering
	saveMu   sync.Mutex // serializes output retries with collection
	assetMu  sync.Mutex // serializes asset tag uniqueness and bulk changes
	cfg      *config.Manager
	media    *media.Store
	// tools finds ffprobe for videos whose header Go cannot read; nil in tests.
	tools    *tools.Service
	root     string // Muse's data folder
	accounts *accounts.Service
	jobs     *jobStore
	// library lists every file finished jobs made, kept apart from the queue.
	library *jobs.Journal[MuseLibraryItem]
	// assets and templates are the reusable references and saved prompts
	// (assets.go), kept apart from the queue.
	assets    *jobs.Journal[MuseAsset]
	templates *jobs.Journal[MusePromptTemplate]
	// defaultOutputDir is the batch folder until the user picks one.
	defaultOutputDir string

	// Seams for tests.
	pageFor  func(context.Context, accounts.Record) (Page, error)
	probeAPI func(context.Context, string) (apiCapacity, error)
	now      func() time.Time
	sleep    func(context.Context, time.Duration) error
	gap      func(config.MuseConfig) time.Duration

	mu       sync.Mutex
	emit     func(events.Event, any)
	ctx      context.Context
	stop     context.CancelFunc
	running  map[string]string // job ID -> account ID
	lastMode MuseJobMode       // last newly assigned screen; next mode gets a turn
	// Cached muse2api worker capacity (guarded by mu).
	apiEndpoint    string
	apiCap         apiCapacity
	apiCheckedAt   time.Time
	apiAttemptedAt time.Time
	apiChecking    bool
	// cleaning holds the accounts "Dọn đoạn chat phụ" is working on; they
	// take no job meanwhile (cleanup.go).
	cleaning map[string]bool
	cancels  map[string]context.CancelFunc
	// sendSlots (one per account) lets one send at a time through, gap
	// included; navs (one per account) guards its tab against navigation.
	// journalWarning says the job journal could not be read on start and was
	// moved aside, so the user checks jobs that may have been sent.
	journalWarning     string
	sendSlots          map[string]chan struct{}
	downloadSlots      map[string]chan struct{}
	imageDownloadSlots map[string]chan struct{}
	quotaSlots         map[string]chan struct{}
	loginSlots         map[string]chan struct{}
	quotaPending       map[string]int
	navs               map[string]*sync.RWMutex
	tabs               *tabPool
	verifiedPorts      map[string]verifiedSession
	sessionEpoch       map[string]int64
	lastRefresh        map[string]refreshOutcome
	stateRefreshSeen   map[string]string
	stateRefreshBusy   map[string]bool
	accountBackoff     map[string]time.Time
	quotaAttempt       map[string]time.Time
	recycleBrowser     map[string]bool
	attCache           *attachmentCache
	wake               chan struct{}
	wg                 sync.WaitGroup
}

// New prepares the provider over the Muse accounts of accts; Start loads the
// journal and begins work. Finished files are copied into outputDir unless the
// user chose another folder.
func New(dataDir, outputDir string, cfg *config.Manager, store *media.Store, accts *accounts.Service) *Provider {
	root := filepath.Join(dataDir, "muse")
	p := &Provider{
		cfg: cfg, media: store, root: root, accounts: accts, defaultOutputDir: outputDir,
		now:                time.Now,
		sleep:              sleepContext,
		gap:                sendGap,
		emit:               func(events.Event, any) {},
		running:            make(map[string]string),
		cleaning:           make(map[string]bool),
		cancels:            make(map[string]context.CancelFunc),
		sendSlots:          make(map[string]chan struct{}),
		downloadSlots:      make(map[string]chan struct{}),
		imageDownloadSlots: make(map[string]chan struct{}),
		quotaSlots:         make(map[string]chan struct{}),
		loginSlots:         make(map[string]chan struct{}),
		quotaPending:       make(map[string]int),
		navs:               make(map[string]*sync.RWMutex),
		tabs:               newTabPool(),
		verifiedPorts:      make(map[string]verifiedSession),
		sessionEpoch:       make(map[string]int64),
		lastRefresh:        make(map[string]refreshOutcome),
		stateRefreshSeen:   make(map[string]string),
		stateRefreshBusy:   make(map[string]bool),
		accountBackoff:     make(map[string]time.Time),
		quotaAttempt:       make(map[string]time.Time),
		recycleBrowser:     make(map[string]bool),
		attCache:           newAttachmentCache(),
		wake:               make(chan struct{}, 1),
	}
	// The folders Muse saves results to are the app's own: the default, the
	// one the batch form remembers, and those queued batches still save into.
	store.AllowRoot(outputDir)
	// Muse makes its images as WebP; they are saved in the format the user picked.
	store.ConvertImages("muse-", func() string { return string(p.cfg.Get().Muse.ImageFormat) })
	store.AllowRootsFrom(func() []string {
		dirs := []string{p.cfg.Get().Muse.OutputDir}
		if p.jobs == nil {
			return dirs
		}
		for _, j := range p.jobs.list() {
			if j.OutputDir != "" {
				dirs = append(dirs, j.OutputDir)
			}
		}
		return dirs
	})
	p.jobs = newJobStore(filepath.Join(root, "jobs.json"), func() int { return p.cfg.Get().Muse.MaxJobs })
	p.library = newLibrary(filepath.Join(root, "library.json"))
	p.assets = newAssets(filepath.Join(root, "assets.json"))
	p.templates = newTemplates(filepath.Join(root, "templates.json"))
	p.pageFor = p.openPage
	p.probeAPI = fetchAPICapacity
	// A signed-in, re-enabled, updated-config or rested account may scale or take jobs now.
	cfg.OnChange(p.trigger)
	accts.OnChange(p.trigger)
	accts.OnBrowserClosed(p.accountBrowserClosed)
	accts.OnAutoResumed(p.onAccountAutoResumed)
	accts.OnRemove(p.accountRemoved)
	return p
}

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Start loads the journal, settles jobs interrupted by the last exit and runs
// the queue until Close. emit broadcasts state changes to the frontend.
func (p *Provider) Start(ctx context.Context, emit func(events.Event, any)) {
	p.mu.Lock()
	p.ctx, p.stop = context.WithCancel(ctx)
	if emit != nil {
		p.emit = emit
	}
	p.mu.Unlock()
	if err := p.jobs.load(); err != nil {
		logger.Get().Warn("muse job journal reset", "error", err)
		if errors.Is(err, errJournalCorrupt) || errors.Is(err, jobs.ErrNewerFormat) {
			p.mu.Lock()
			p.journalWarning = strings.TrimSuffix(err.Error(), ": "+errJournalCorrupt.Error())
			p.mu.Unlock()
		}
	}
	p.loadLibrary()
	p.loadStudio()
	for _, job := range p.jobs.list() {
		if job.Status != MuseJobProcessing {
			continue
		}
		if job.Sending {
			if job.PreSendTracked && job.AccountID != "" && (p.cfg == nil || !p.cfg.Get().Muse.UseAPI) {
				if _, err := p.accounts.Record(job.AccountID); err == nil {
					continue // interrupted during send with a session snapshot: reconcile on the same account
				}
				p.jobs.update(job.ID, func(j *MuseJob) {
					j.Status, j.Step, j.FinishedAt = MuseJobFailed, "", p.now().UTC().Format(time.RFC3339)
					j.Sending, j.PreSendTracked, j.PreSendSessions = false, false, nil
					j.Error = errAccountGone
				})
				continue
			}
			p.jobs.update(job.ID, func(j *MuseJob) {
				j.Status, j.Error, j.Step, j.Sending, j.SendUnknown = MuseJobFailed, errSendUnknown.Error(), "", false, true
				j.PreSendTracked, j.PreSendSessions = false, nil
				j.FinishedAt = p.now().UTC().Format(time.RFC3339)
			})
			continue
		}
		if job.SessionID != "" {
			if job.AccountID == "api" {
				continue // API jobs resume polling their task ID via muse2api
			}
			if _, err := p.accounts.Record(job.AccountID); err == nil {
				continue // sent jobs are checked again on their own account
			}
			// Only the account that sent a job can read its chat; without it the
			// job is failed rather than sent again from another account.
			p.jobs.update(job.ID, func(j *MuseJob) {
				j.Status, j.Step, j.FinishedAt = MuseJobFailed, "", p.now().UTC().Format(time.RFC3339)
				j.Error = errAccountGone
			})
			continue
		}
		p.jobs.update(job.ID, func(j *MuseJob) {
			resetForQueue(j)
			if j.Attempts > 0 {
				j.Attempts--
			}
		})
	}
	p.sweepUnused()
	p.releaseWaiting()
	if p.cfg != nil && p.cfg.Get().Muse.UseAPI {
		p.apiCapacityFor(p.cfg.Get().Muse, p.now())
	}
	p.wg.Add(1)
	go p.loop()
}

// lifetime is the context of background work: the app's until Start, then
// the queue's.
func (p *Provider) lifetime() context.Context {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ctx == nil {
		return context.Background()
	}
	return p.ctx
}

// Close stops the queue. Running jobs stay as they are in the journal and
// continue on the next start.
func (p *Provider) Close() {
	p.mu.Lock()
	stop := p.stop
	p.mu.Unlock()
	if stop != nil {
		stop()
	}
	p.wg.Wait()
	p.attCache.clear()
}

// accountNotReadyBackoff keeps a failing account out of scheduling long enough
// for healthy sibling accounts to pick up requeued jobs before it retries cleanly.
const accountNotReadyBackoff = 15 * time.Second

func (p *Provider) loop() {
	defer p.wg.Done()
	timer := time.NewTimer(pumpInterval)
	defer timer.Stop()
	for {
		p.safePump()
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(p.nextWakeDelay())
		select {
		case <-p.ctx.Done():
			return
		case <-timer.C:
		case <-p.wake:
		}
	}
}

func (p *Provider) nextWakeDelay() time.Duration {
	d := pumpInterval
	now := p.now()
	p.mu.Lock()
	for id, until := range p.accountBackoff {
		if !now.Before(until) {
			delete(p.accountBackoff, id)
			p.mu.Unlock()
			return 250 * time.Millisecond
		}
		if wait := until.Sub(now); wait < d {
			d = wait
		}
	}
	p.mu.Unlock()
	if p.jobs != nil {
		for _, job := range p.jobs.list() {
			if job.Held || (job.Status != MuseJobQueued && job.Status != MuseJobProcessing) || job.NextCheckAt == "" {
				continue
			}
			t, err := time.Parse(time.RFC3339Nano, job.NextCheckAt)
			if err != nil {
				continue
			}
			if wait := t.Sub(now); wait > 0 && wait < d {
				d = wait
			}
		}
	}
	if d < 250*time.Millisecond {
		return 250 * time.Millisecond
	}
	return d
}

// safePump runs one pump; a panic in it is logged and the loop carries on
// at the next tick instead of the queue stopping for the rest of the run.
func (p *Provider) safePump() {
	defer logger.Recover("hàng đợi Muse")
	p.pump()
}

// trigger asks the loop to re-plan soon.
func (p *Provider) trigger() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *Provider) pump() {
	if p.accounts != nil {
		p.accounts.ResumeExpired()
	}
	records, err := p.accounts.Records(accounts.ProviderMuse)
	if err != nil {
		return
	}
	views := make([]MuseAccount, len(records))
	for i, rec := range records {
		views[i] = museView(rec)
		if err := p.accounts.RouteError(rec); err != nil {
			views[i].NoRoute = err.Error()
		}
	}
	now := p.now()
	fullCfg := p.cfg.Get()
	cfg := fullCfg.Muse
	p.mu.Lock()
	pumpCtx := p.ctx
	p.mu.Unlock()
	if pumpCtx != nil && pumpCtx.Err() == nil && p.tabs != nil && p.tabs.hasExcess(cfg.ThreadsPerAccount) {
		p.tabs.reconcileIdle(pumpCtx, cfg.ThreadsPerAccount, fullCfg.Accounts)
	}
	apiCap := p.apiCapacityFor(cfg, now)
	p.mu.Lock()
	if p.ctx == nil || p.ctx.Err() != nil {
		p.mu.Unlock()
		return
	}
	// Listed under the lock: a run ends by saving its job and only then leaving
	// running, so no ended job is seen here as still to run.
	jobs := p.jobs.list()
	started := make(map[string]bool)
	free := slices.DeleteFunc(slices.Clone(views), func(v MuseAccount) bool {
		if p.cleaning[v.ID] {
			return true
		}
		if until, ok := p.accountBackoff[v.ID]; ok {
			if now.Before(until) {
				return true
			}
			delete(p.accountBackoff, v.ID)
		}
		return false
	})
	var chromeStagger time.Duration
	firstChrome := true
	for _, a := range planWithCapacity(due(jobs, now), free, cfg, p.running, now, apiCap, p.lastMode) {
		if job, ok := p.jobs.get(a.jobID); ok && job.Status == MuseJobQueued {
			p.lastMode = job.Mode
			if p.lastMode == "" {
				p.lastMode = MuseJobBatch
			}
		}
		var stagger time.Duration
		if a.accountID != "api" {
			if firstChrome {
				firstChrome = false
			} else {
				chromeStagger += time.Duration(300+rand.IntN(300)) * time.Millisecond
				if chromeStagger > 5*time.Second {
					chromeStagger = 5 * time.Second
				}
				stagger = chromeStagger
			}
		}
		ctx, cancel := context.WithCancel(p.ctx)
		p.running[a.jobID] = a.accountID
		p.cancels[a.jobID] = cancel
		started[a.jobID] = true
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			defer logger.Recover("tác vụ Muse", func(err error) { p.finishRun(a.jobID) })
			p.runStaggered(ctx, a.jobID, a.accountID, stagger)
		}()
	}
	runningAccs := make(map[string]bool, len(p.running))
	for id, accID := range p.running {
		started[id] = true
		if accID != "" {
			runningAccs[accID] = true
		}
	}
	cleaningSnap := make(map[string]bool, len(p.cleaning))
	for accID, on := range p.cleaning {
		if on {
			cleaningSnap[accID] = true
		}
	}
	p.mu.Unlock()
	p.noteWaiting(jobs, views, started, cfg, now)
	p.triggerNeededAccountRefreshes(jobs, records, cfg, runningAccs, cleaningSnap)
}

// noteWaiting tells the user why a sent job is not being checked: only the
// account that sent it can read its chat, and that account cannot work now. It
// also shows why an unheld queued job is waiting.
func (p *Provider) noteWaiting(jobs []MuseJob, accs []MuseAccount, running map[string]bool, cfg config.MuseConfig, now time.Time) {
	cleaning, backedOff := p.backedOffAccountsSnapshot(now)
	for _, job := range jobs {
		if running[job.ID] {
			continue
		}
		if job.Status == MuseJobProcessing && (job.SessionID != "" || (job.Sending && job.PreSendTracked)) {
			step := waitingStep(job, accs)
			if step == "" || step == job.Step {
				continue
			}
			p.updateJob(job.ID, func(j *MuseJob) {
				if j.Status == MuseJobProcessing {
					j.Step = step
				}
			})
			continue
		}
		if job.Status == MuseJobQueued && !job.Held && !waitsForBoard(job) {
			step := queuedWaitingStep(job, accs, running, cleaning, backedOff, cfg, now)
			if step == job.Step {
				continue
			}
			p.updateJob(job.ID, func(j *MuseJob) {
				if j.Status == MuseJobQueued && !j.Held && !waitsForBoard(*j) {
					j.Step = step
				}
			})
		}
	}
}

// queuedWaitingStep summarizes why an unheld queued job is still waiting.
func queuedWaitingStep(job MuseJob, accs []MuseAccount, running, cleaning, backedOff map[string]bool, cfg config.MuseConfig, now time.Time) string {
	if job.NextCheckAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, job.NextCheckAt); err == nil && t.After(now) {
			return "Thử lại lúc " + t.Local().Format("15:04:05")
		}
	}
	if cfg.UseAPI {
		if len(running) > 0 {
			return "Chờ lượt API đang chạy xong"
		}
		return ""
	}
	usableCount, backedOffCount := 0, 0
	for _, acc := range accs {
		if job.AccountID != "" && acc.ID != job.AccountID {
			continue
		}
		if cleaning[acc.ID] || acc.NoRoute != "" || !usableForJob(acc, job.Kind, cfg, now) {
			continue
		}
		usableCount++
		if backedOff[acc.ID] {
			backedOffCount++
		}
	}
	switch {
	case usableCount == 0:
		return "Chờ tài khoản Muse khả dụng"
	case backedOffCount == usableCount:
		return "Chờ tài khoản sẵn sàng lại"
	case len(running) > 0:
		return "Chờ tài khoản chạy xong"
	default:
		return ""
	}
}

// waitingStep words what a sent job waits for when its account cannot run it.
func waitingStep(job MuseJob, accs []MuseAccount) string {
	i := slices.IndexFunc(accs, func(a MuseAccount) bool { return a.ID == job.AccountID })
	if i < 0 {
		return ""
	}
	switch acc := accs[i]; {
	case !acc.Enabled:
		return fmt.Sprintf("Chờ bật lại tài khoản %s để kiểm tra kết quả", acc.Name)
	case acc.Status != accounts.StatusSignedIn:
		return fmt.Sprintf("Chờ tài khoản %s đăng nhập lại để kiểm tra kết quả", acc.Name)
	case acc.NoRoute != "":
		return "Chờ proxy để kiểm tra kết quả: " + acc.NoRoute
	}
	return ""
}

// holdUntil keeps a job off the queue until t (zero clears it). The hold is
// saved with the job, so a restart does not retry it early.
func (p *Provider) holdUntil(jobID string, t time.Time) {
	at := ""
	if !t.IsZero() {
		at = t.UTC().Format(time.RFC3339Nano)
	}
	p.updateJob(jobID, func(j *MuseJob) { j.NextCheckAt = at })
}

func (p *Provider) claimQuotaAttempt(accountID string, cooldown time.Duration) bool {
	if accountID == "" {
		return true
	}
	now := p.now()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.quotaAttempt == nil {
		p.quotaAttempt = make(map[string]time.Time)
	}
	if last, ok := p.quotaAttempt[accountID]; ok && now.Sub(last) < cooldown {
		return false
	}
	p.quotaAttempt[accountID] = now
	return true
}

func (p *Provider) recordQuotaAttempt(accountID string) {
	if accountID == "" {
		return
	}
	now := p.now()
	p.mu.Lock()
	if p.quotaAttempt == nil {
		p.quotaAttempt = make(map[string]time.Time)
	}
	p.quotaAttempt[accountID] = now
	p.mu.Unlock()
}

func (p *Provider) markRecycleBrowser(accountID string) {
	if accountID == "" {
		return
	}
	p.mu.Lock()
	if p.recycleBrowser == nil {
		p.recycleBrowser = make(map[string]bool)
	}
	p.recycleBrowser[accountID] = true
	p.mu.Unlock()
}

func (p *Provider) clearRecycleBrowser(accountID string) {
	if accountID == "" {
		return
	}
	p.mu.Lock()
	delete(p.recycleBrowser, accountID)
	p.mu.Unlock()
}

func (p *Provider) takeRecycleBrowser(accountID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.recycleBrowser[accountID] {
		return false
	}
	delete(p.recycleBrowser, accountID)
	return true
}

func (p *Provider) backoffAccount(accountID string, d time.Duration) {
	if accountID == "" || d <= 0 {
		return
	}
	until := p.now().Add(d)
	p.mu.Lock()
	if p.accountBackoff == nil {
		p.accountBackoff = make(map[string]time.Time)
	}
	if cur := p.accountBackoff[accountID]; until.After(cur) {
		p.accountBackoff[accountID] = until
	}
	p.mu.Unlock()
}

func (p *Provider) backedOffAccountsSnapshot(now time.Time) (cleaning, backedOff map[string]bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.cleaning) > 0 {
		cleaning = make(map[string]bool, len(p.cleaning))
		for k, v := range p.cleaning {
			if v {
				cleaning[k] = true
			}
		}
	}
	for id, until := range p.accountBackoff {
		if !now.Before(until) {
			delete(p.accountBackoff, id)
			continue
		}
		if backedOff == nil {
			backedOff = make(map[string]bool)
		}
		backedOff[id] = true
	}
	return cleaning, backedOff
}

func (p *Provider) hasOtherUsableAccount(excludeID string, kind MuseJobKind, cfg config.MuseConfig, now time.Time) bool {
	if p.accounts == nil || cfg.UseAPI {
		return false
	}
	records, err := p.accounts.Records(accounts.ProviderMuse)
	if err != nil {
		return false
	}
	cleaning, _ := p.backedOffAccountsSnapshot(now)
	for _, rec := range records {
		if rec.ID == excludeID || cleaning[rec.ID] {
			continue
		}
		if p.accounts.RouteError(rec) != nil {
			continue
		}
		view := museView(rec)
		if view.NoRoute != "" {
			continue
		}
		if usableForJob(view, kind, cfg, now) {
			return true
		}
	}
	return false
}

// sendSlot waits for the account's single send slot; release frees it.
func (p *Provider) sendSlot(ctx context.Context, accountID string) (release func(), err error) {
	return p.accountSlot(ctx, accountID, p.sendSlots)
}

// downloadSlot keeps only one result Blob open per account, even when several
// generations run together. Other accounts can still download in parallel.
func (p *Provider) downloadSlot(ctx context.Context, accountID string) (func(), error) {
	return p.accountSlotCap(ctx, accountID, &p.downloadSlots, 1)
}

const collectImageConcurrency = 3

// imageDownloadSlot allows bounded concurrent small-image downloads on one
// account (such as storyboard frames) while video jobs stay serialized by downloadSlot.
func (p *Provider) imageDownloadSlot(ctx context.Context, accountID string) (func(), error) {
	return p.accountSlotCap(ctx, accountID, &p.imageDownloadSlots, collectImageConcurrency)
}

func (p *Provider) accountSlot(ctx context.Context, accountID string, slots map[string]chan struct{}) (release func(), err error) {
	return p.accountSlotCap(ctx, accountID, &slots, 1)
}

func (p *Provider) accountSlotCap(ctx context.Context, accountID string, slotsPtr *map[string]chan struct{}, cap int) (release func(), err error) {
	if cap <= 0 {
		cap = 1
	}
	p.mu.Lock()
	if *slotsPtr == nil {
		*slotsPtr = make(map[string]chan struct{})
	}
	slots := *slotsPtr
	slot := slots[accountID]
	if slot == nil {
		slot = make(chan struct{}, cap)
		slots[accountID] = slot
	}
	p.mu.Unlock()
	select {
	case slot <- struct{}{}:
		return func() { <-slot }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// loginSlot serializes initial Chrome login checks per account so concurrent
// threads on one fresh Chrome verify sign-in at most once.
func (p *Provider) loginSlot(ctx context.Context, accountID string) (func(), error) {
	return p.accountSlotCap(ctx, accountID, &p.loginSlots, 1)
}

// navLock is the account tab's navigation guard.
func (p *Provider) navLock(key string) *sync.RWMutex {
	p.mu.Lock()
	defer p.mu.Unlock()
	nav := p.navs[key]
	if nav == nil {
		nav = &sync.RWMutex{}
		p.navs[key] = nav
	}
	return nav
}

func (p *Provider) tabNavLock(accountID, targetID string) *sync.RWMutex {
	if targetID != "" {
		return p.navLock(accountID + ":" + targetID)
	}
	return p.navLock(accountID)
}

// errRunStopping refuses to queue a job again while its cancelled run is
// still finishing (a send in flight runs to its own end).
var errRunStopping = errors.New("Tác vụ đang dừng, thử lại sau vài giây.")

// errJobEnded refuses to cancel a job that has already ended.
var errJobEnded = errors.New("Tác vụ đã kết thúc.")

// runActive reports whether a run of the job has not ended yet.
func (p *Provider) runActive(jobID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.running[jobID]
	return ok
}

// errJobChanged refuses a change to a job that started or ended after the
// caller looked at it.
var errJobChanged = errors.New("Tác vụ vừa đổi trạng thái; hãy thử lại.")

// updateIdle changes a job no run holds, but only while allowed still holds
// for its saved state. p.mu is held across the check and the save, so pump
// (which starts runs under p.mu) cannot start the job in between and have
// its run reset under it.
func (p *Provider) updateIdle(id string, allowed func(MuseJob) bool, change func(*MuseJob)) (MuseJob, error) {
	p.mu.Lock()
	if _, ok := p.running[id]; ok {
		p.mu.Unlock()
		return MuseJob{}, errRunStopping
	}
	refused := false
	job, err := p.jobs.update(id, func(j *MuseJob) {
		if refused = !allowed(*j); !refused {
			change(j)
		}
	})
	p.mu.Unlock()
	if err != nil {
		logger.Get().Warn("cannot save muse job", "job", id, "error", err)
		return job, err
	}
	if refused {
		return job, errJobChanged
	}
	p.emit(events.EventMuseJobUpdated, job)
	return job, nil
}

// idleCode is the response code of an updateIdle error.
func idleCode(err error) models.Code {
	switch {
	case errors.Is(err, errJobNotFound):
		return models.CodeNotFound
	case errors.Is(err, errRunStopping), errors.Is(err, errJobChanged):
		return models.CodeInvalidArgument
	}
	return models.CodeInternal
}

func (p *Provider) runStaggered(ctx context.Context, jobID, accountID string, stagger time.Duration) {
	if stagger > 0 {
		if err := p.sleep(ctx, stagger); err != nil {
			p.finishRun(jobID)
			return
		}
	}
	p.runOne(ctx, jobID, accountID)
}

func (p *Provider) finishRun(jobID string) {
	job, ok := p.jobs.get(jobID)
	p.mu.Lock()
	accountID := p.running[jobID]
	if cancel := p.cancels[jobID]; cancel != nil {
		cancel()
	}
	delete(p.cancels, jobID)
	delete(p.running, jobID)
	p.mu.Unlock()
	if ok && job.finished() && job.NextCheckAt != "" {
		p.updateJob(jobID, func(j *MuseJob) {
			if j.finished() {
				j.NextCheckAt = ""
			}
		})
	}
	if ok && job.BoardFrames > 0 && job.Kind == MuseJobImage {
		p.releaseWaiting()
	}
	p.closeIdleBrowserIfDone(accountID)
	p.trigger()
}

// closeIdleBrowserIfDone closes the account's headless Chrome only when it has
// no active work left and can no longer run jobs (e.g. signed out, paused or
// disabled), keeping healthy warm browsers alive for fast reuse until
// IdleBrowserMinutes expires.
func (p *Provider) closeIdleBrowserIfDone(accountID string) {
	if accountID == "" || accountID == "api" || p.accounts == nil || p.jobs == nil {
		return
	}
	p.mu.Lock()
	stopped := p.ctx == nil || p.ctx.Err() != nil
	stillRunning := p.quotaPending[accountID] > 0
	for _, runningAcc := range p.running {
		if runningAcc == accountID {
			stillRunning = true
			break
		}
	}
	p.mu.Unlock()
	if stopped || stillRunning || p.accounts.InUse(accountID) {
		return
	}
	now := p.now()
	cfg := p.cfg.Get().Muse
	acc, err := p.accounts.Record(accountID)
	view := museView(acc)
	recycle := p.takeRecycleBrowser(accountID)
	if recycle || err != nil || p.accounts.RouteError(acc) != nil || !usable(view, cfg, now) {
		p.dropBrowserState(accountID)
		_ = p.accounts.CloseHeadlessIfIdle(accountID)
	}
}

// updateJob persists a change and tells the frontend.
func (p *Provider) updateJob(id string, change func(*MuseJob)) (MuseJob, error) {
	job, err := p.jobs.update(id, change)
	if err != nil {
		logger.Get().Warn("cannot save muse job", "job", id, "error", err)
		return job, err
	}
	p.emit(events.EventMuseJobUpdated, job)
	return job, nil
}

// updateJobs is updateJob for many jobs in one journal write: change runs
// under the journal's lock and reports whether it changed the job, so it
// re-checks the job's state itself. Each changed job is emitted as updateJob
// would; on a failed write nothing is emitted, like a failed updateJob.
func (p *Provider) updateJobs(ids []string, change func(*MuseJob) bool) []MuseJob {
	if len(ids) == 0 {
		return nil
	}
	changed, err := p.jobs.updateMany(ids, change)
	if err != nil {
		logger.Get().Warn("cannot save muse jobs", "jobs", len(ids), "error", err)
		return nil
	}
	for _, job := range changed {
		p.emit(events.EventMuseJobUpdated, job)
	}
	return changed
}

// updateAccount changes an account's shared fields and Muse data together,
// keeping its displayed usage in step with the quota.
func (p *Provider) updateAccount(id string, change func(r *accounts.Record, d *museData)) {
	_, err := p.accounts.Update(id, func(r *accounts.Record) {
		var data museData
		_ = accounts.DecodeData(*r, &data)
		change(r, &data)
		if err := r.SetData(data); err == nil {
			r.Usage = accountUsage(data)
		}
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		logger.Get().Warn("cannot save muse account", "account", id, "error", err)
	}
}

const errAccountGone = "Tài khoản đã bị xoá nên không kiểm tra được kết quả. Bấm Chạy lại để gửi bằng tài khoản khác."

func (p *Provider) dropBrowserState(accountID string) {
	p.clearSessionVerified(accountID)
	if p.tabs != nil {
		p.tabs.dropAccount(accountID)
	}
	prefix := accountID + ":"
	p.mu.Lock()
	for k := range p.navs {
		if k == accountID || strings.HasPrefix(k, prefix) {
			delete(p.navs, k)
		}
	}
	p.mu.Unlock()
}

func (p *Provider) accountBrowserClosed(rec accounts.Record) {
	if rec.Provider != accounts.ProviderMuse {
		return
	}
	p.dropBrowserState(rec.ID)
}

// accountRemoved fails the jobs a removed account was running: their chats
// can no longer be read, so they are not sent again from another account.
func (p *Provider) accountRemoved(rec accounts.Record) {
	if rec.Provider != accounts.ProviderMuse {
		return
	}
	p.dropBrowserState(rec.ID)
	p.clearRecycleBrowser(rec.ID)
	p.mu.Lock()
	delete(p.accountBackoff, rec.ID)
	delete(p.quotaAttempt, rec.ID)
	delete(p.sessionEpoch, rec.ID)
	delete(p.lastRefresh, rec.ID)
	delete(p.stateRefreshSeen, rec.ID)
	delete(p.stateRefreshBusy, rec.ID)
	p.mu.Unlock()
	for _, job := range p.jobs.list() {
		if job.Status == MuseJobProcessing && job.AccountID == rec.ID {
			p.updateJob(job.ID, func(j *MuseJob) {
				j.Status, j.Step, j.FinishedAt = MuseJobFailed, "", p.now().UTC().Format(time.RFC3339)
				j.Error = errAccountGone
			})
		}
	}
}

// openPage starts (or reuses) the account's browser with muse.ai open on one shared foreground tab.
func (p *Provider) openPage(ctx context.Context, rec accounts.Record) (Page, error) {
	b, err := p.accounts.Browser(ctx, rec.ID)
	if err != nil {
		return nil, err
	}
	cfg := p.cfg.Get()
	return &chromePage{
		port:        b.Port,
		policy:      cfg.Accounts,
		callTimeout: time.Duration(cfg.Accounts.RequestTimeoutSeconds*4) * time.Second,
		sendTimeout: time.Duration(cfg.Muse.SendTimeoutSeconds) * time.Second,
		nav:         p.navLock(rec.ID),
	}, nil
}

// sortedJobs returns jobs newest first, the order the queue screen shows.
func sortedJobs(jobs []MuseJob) []MuseJob {
	if len(jobs) == 0 {
		return []MuseJob{}
	}
	slices.SortStableFunc(jobs, func(a, b MuseJob) int {
		switch {
		case a.CreatedAt > b.CreatedAt:
			return -1
		case a.CreatedAt < b.CreatedAt:
			return 1
		}
		return 0
	})
	return jobs
}

// featureVideoCheck is named to the user when checking a made video needs a
// tool the machine lacks.
const featureVideoCheck = "Kiểm tra video Muse"

// ffprobe is ProbeVideo's lookup (internal/tools).
func (p *Provider) ffprobe() (string, error) {
	if p.tools == nil {
		return "", tools.ErrMissing
	}
	return p.tools.Program(featureVideoCheck, "ffprobe")
}
