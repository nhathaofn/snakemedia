// Package muse is the muse.ai provider: a durable job queue spread over the
// Muse accounts of the shared accounts service, and image/video generation
// through Muse's own chat client in each account's tab.
package muse

import (
	"context"
	"errors"
	"fmt"
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
)

// pumpInterval is how often the queue is re-planned without any trigger, so
// paused accounts resume and quota resets are noticed.
const pumpInterval = 30 * time.Second

// Provider owns the Muse job queue; accounts and their browsers belong to
// the accounts service.
type Provider struct {
	cfg      *config.Manager
	media    *media.Store
	root     string // Muse's data folder
	accounts *accounts.Service
	jobs     *jobStore
	// library lists every file finished jobs made, kept apart from the queue.
	library *jobs.Journal[MuseLibraryItem]
	// defaultOutputDir is the batch folder until the user picks one.
	defaultOutputDir string

	// Seams for tests.
	pageFor func(context.Context, accounts.Record) (Page, error)
	now     func() time.Time
	sleep   func(context.Context, time.Duration) error
	gap     func(config.MuseConfig) time.Duration

	mu      sync.Mutex
	emit    func(events.Event, any)
	ctx     context.Context
	stop    context.CancelFunc
	running map[string]string // job ID -> account ID
	cancels map[string]context.CancelFunc
	// sendSlots (one per account) lets one send at a time through, gap
	// included; navs (one per account) guards its tab against navigation.
	// journalWarning says the job journal could not be read on start and was
	// moved aside, so the user checks jobs that may have been sent.
	journalWarning string
	sendSlots      map[string]chan struct{}
	navs           map[string]*sync.RWMutex
	wake           chan struct{}
	wg             sync.WaitGroup
}

// New prepares the provider over the Muse accounts of accts; Start loads the
// journal and begins work. Finished files are copied into outputDir unless the
// user chose another folder.
func New(dataDir, outputDir string, cfg *config.Manager, store *media.Store, accts *accounts.Service) *Provider {
	root := filepath.Join(dataDir, "muse")
	p := &Provider{
		cfg: cfg, media: store, root: root, accounts: accts, defaultOutputDir: outputDir,
		now:       time.Now,
		sleep:     sleepContext,
		gap:       sendGap,
		emit:      func(events.Event, any) {},
		running:   make(map[string]string),
		cancels:   make(map[string]context.CancelFunc),
		sendSlots: make(map[string]chan struct{}),
		navs:      make(map[string]*sync.RWMutex),
		wake:      make(chan struct{}, 1),
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
			if !j.finished() && j.OutputDir != "" {
				dirs = append(dirs, j.OutputDir)
			}
		}
		return dirs
	})
	p.jobs = newJobStore(filepath.Join(root, "jobs.json"), func() int { return p.cfg.Get().Muse.MaxJobs })
	p.library = newLibrary(filepath.Join(root, "library.json"))
	p.pageFor = p.openPage
	// A signed-in, re-enabled or rested account may take queued jobs now.
	accts.OnChange(p.trigger)
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
		if errors.Is(err, errJournalCorrupt) {
			p.mu.Lock()
			p.journalWarning = strings.TrimSuffix(err.Error(), ": "+errJournalCorrupt.Error())
			p.mu.Unlock()
		}
	}
	p.loadLibrary()
	for _, job := range p.jobs.list() {
		if job.Status != MuseJobProcessing {
			continue
		}
		if job.Sending {
			p.jobs.update(job.ID, func(j *MuseJob) {
				j.Status, j.Error, j.Step, j.Sending, j.SendUnknown = MuseJobFailed, errSendUnknown.Error(), "", false, true
				j.FinishedAt = p.now().UTC().Format(time.RFC3339)
			})
			continue
		}
		if job.SessionID != "" {
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
	p.sweepReferences()
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
}

func (p *Provider) loop() {
	defer p.wg.Done()
	ticker := time.NewTicker(pumpInterval)
	defer ticker.Stop()
	for {
		p.safePump()
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
		case <-p.wake:
		}
	}
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
	p.mu.Lock()
	if p.ctx == nil || p.ctx.Err() != nil {
		p.mu.Unlock()
		return
	}
	// Listed under the lock: a run ends by saving its job and only then leaving
	// running, so no ended job is seen here as still to run.
	jobs := p.jobs.list()
	started := make(map[string]bool)
	for _, a := range plan(due(jobs, now), views, p.cfg.Get().Muse, p.running, now) {
		ctx, cancel := context.WithCancel(p.ctx)
		p.running[a.jobID] = a.accountID
		p.cancels[a.jobID] = cancel
		started[a.jobID] = true
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			p.runOne(ctx, a.jobID, a.accountID)
		}()
	}
	for id := range p.running {
		started[id] = true
	}
	p.mu.Unlock()
	p.noteWaiting(jobs, views, started)
}

// noteWaiting tells the user why a sent job is not being checked: only the
// account that sent it can read its chat, and that account cannot work now.
func (p *Provider) noteWaiting(jobs []MuseJob, accs []MuseAccount, running map[string]bool) {
	for _, job := range jobs {
		if job.Status != MuseJobProcessing || job.SessionID == "" || running[job.ID] {
			continue
		}
		step := waitingStep(job, accs)
		if step == "" || step == job.Step {
			continue
		}
		p.updateJob(job.ID, func(j *MuseJob) {
			if j.Status == MuseJobProcessing {
				j.Step = step
			}
		})
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

// sendSlot waits for the account's single send slot; release frees it.
func (p *Provider) sendSlot(ctx context.Context, accountID string) (release func(), err error) {
	p.mu.Lock()
	slot := p.sendSlots[accountID]
	if slot == nil {
		slot = make(chan struct{}, 1)
		p.sendSlots[accountID] = slot
	}
	p.mu.Unlock()
	select {
	case slot <- struct{}{}:
		return func() { <-slot }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// navLock is the account tab's navigation guard.
func (p *Provider) navLock(accountID string) *sync.RWMutex {
	p.mu.Lock()
	defer p.mu.Unlock()
	nav := p.navs[accountID]
	if nav == nil {
		nav = &sync.RWMutex{}
		p.navs[accountID] = nav
	}
	return nav
}

// errRunStopping refuses to queue a job again while its cancelled run is
// still finishing (a send in flight runs to its own end).
var errRunStopping = errors.New("Tác vụ đang dừng, thử lại sau vài giây.")

// runActive reports whether a run of the job has not ended yet.
func (p *Provider) runActive(jobID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.running[jobID]
	return ok
}

func (p *Provider) finishRun(jobID string) {
	job, ok := p.jobs.get(jobID)
	p.mu.Lock()
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
	p.trigger()
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

// accountRemoved fails the jobs a removed account was running: their chats
// can no longer be read, so they are not sent again from another account.
func (p *Provider) accountRemoved(rec accounts.Record) {
	if rec.Provider != accounts.ProviderMuse {
		return
	}
	for _, job := range p.jobs.list() {
		if job.Status == MuseJobProcessing && job.AccountID == rec.ID {
			p.updateJob(job.ID, func(j *MuseJob) {
				j.Status, j.Step, j.FinishedAt = MuseJobFailed, "", p.now().UTC().Format(time.RFC3339)
				j.Error = errAccountGone
			})
		}
	}
}

// openPage starts (or reuses) the account's browser with muse.ai open.
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
