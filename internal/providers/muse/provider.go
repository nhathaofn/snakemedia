// Package muse is the muse.ai provider: Muse accounts (one Chrome profile
// each), a durable job queue spread over those accounts, and image/video
// generation through Muse's own chat client in the account's tab.
package muse

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"SnakeMedia/internal/config"
	"SnakeMedia/internal/events"
	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/media"
)

// pumpInterval is how often the queue is re-planned without any trigger, so
// paused accounts resume and quota resets are noticed.
const pumpInterval = 30 * time.Second

// Provider owns Muse accounts, their browsers and the job queue.
type Provider struct {
	cfg         *config.Manager
	media       *media.Store
	accounts    *accountStore
	browsers    *browserPool
	jobs        *jobStore
	profileRoot string
	proxies     *proxyStore
	bridges     *bridgeManager
	deadProxies map[string]time.Time
	studio      *studioStore
	productions *productionStore

	// Seams for tests.
	pageFor  func(context.Context, storedAccount) (Page, error)
	now      func() time.Time
	sleep    func(context.Context, time.Duration) error
	gap      func(config.MuseConfig) time.Duration
	muxAudio func(ctx context.Context, ffmpegPath, videoPath, sourcePath, outPath string) error

	mu       sync.Mutex
	emit     func(events.Event, any)
	ctx      context.Context
	stop     context.CancelFunc
	running  map[string]string // job ID -> account ID
	cancels  map[string]context.CancelFunc
	watching map[string]bool // accounts with an open sign-in watcher
	wake     chan struct{}
	wg       sync.WaitGroup
}

// New prepares the provider; Start loads the journal and begins work.
func New(dataDir string, cfg *config.Manager, store *media.Store) *Provider {
	root := filepath.Join(dataDir, "muse")
	p := &Provider{
		cfg: cfg, media: store,
		accounts:    newAccountStore(filepath.Join(root, "accounts.json")),
		profileRoot: filepath.Join(root, "profiles"),
		now:         time.Now,
		sleep:       sleepContext,
		gap:         sendGap,
		emit:        func(events.Event, any) {},
		running:     make(map[string]string),
		cancels:     make(map[string]context.CancelFunc),
		watching:    make(map[string]bool),
		wake:        make(chan struct{}, 1),
	}
	p.browsers = newBrowserPool(cfg, p.profileRoot)
	p.jobs = newJobStore(filepath.Join(root, "jobs.json"), func() int { return p.cfg.Get().Muse.MaxJobs })
	p.proxies = newProxyStore(filepath.Join(root, "proxies.json"))
	p.bridges = newBridgeManager()
	p.deadProxies = make(map[string]time.Time)
	p.studio = newStudioStore(filepath.Join(root, "studio.json"))
	p.productions = newProductionStore(filepath.Join(root, "productions.json"))
	p.pageFor = p.openPage
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
	}
	for _, job := range p.jobs.list() {
		if job.Status != MuseJobProcessing {
			continue
		}
		if job.Sending {
			p.jobs.update(job.ID, func(j *MuseJob) {
				j.Status, j.Error, j.Step, j.Sending = MuseJobFailed, errSendUnknown.Error(), "", false
				j.FinishedAt = p.now().UTC().Format(time.RFC3339)
			})
			continue
		}
		if job.SessionID != "" {
			continue // sent jobs are checked again on their own account
		}
		p.jobs.update(job.ID, func(j *MuseJob) {
			resetForQueue(j)
			if j.Attempts > 0 {
				j.Attempts--
			}
		})
	}
	p.wg.Add(1)
	go p.loop()
}

// Close stops the queue and the account browsers. Running jobs stay as they
// are in the journal and continue on the next start.
func (p *Provider) Close() {
	p.mu.Lock()
	stop := p.stop
	p.mu.Unlock()
	if stop != nil {
		stop()
	}
	p.wg.Wait()
	p.browsers.closeAll()
}

func (p *Provider) loop() {
	defer p.wg.Done()
	ticker := time.NewTicker(pumpInterval)
	defer ticker.Stop()
	for {
		p.pump()
		p.pumpProductions(p.ctx)
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
		case <-p.wake:
		}
	}
}

// trigger asks the loop to re-plan soon.
func (p *Provider) trigger() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *Provider) pump() {
	stored, err := p.accounts.list()
	if err != nil {
		return
	}
	accounts := make([]MuseAccount, len(stored))
	for i, a := range stored {
		accounts[i] = a.MuseAccount
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ctx.Err() != nil {
		return
	}
	for _, a := range plan(p.jobs.list(), accounts, p.cfg.Get().Muse, p.running, p.pausedProjects(), p.now()) {
		ctx, cancel := context.WithCancel(p.ctx)
		p.running[a.jobID] = a.accountID
		p.cancels[a.jobID] = cancel
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			p.runOne(ctx, a.jobID, a.accountID)
		}()
	}
}

func (p *Provider) finishRun(jobID string) {
	p.mu.Lock()
	if cancel := p.cancels[jobID]; cancel != nil {
		cancel()
	}
	delete(p.cancels, jobID)
	delete(p.running, jobID)
	p.mu.Unlock()
	p.trigger()
}

func (p *Provider) busyAccount(accountID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, id := range p.running {
		if id == accountID {
			return true
		}
	}
	return false
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

// updateAccount persists a change and tells the frontend.
func (p *Provider) updateAccount(id string, change func(*storedAccount)) (MuseAccount, error) {
	acc, err := p.accounts.update(id, change)
	if err != nil {
		if err != errAccountNotFound {
			logger.Get().Warn("cannot save muse account", "account", id, "error", err)
		}
		return MuseAccount{}, err
	}
	p.emitAccounts()
	return acc.MuseAccount, nil
}

func (p *Provider) emitAccounts() {
	if list, err := p.publicAccounts(); err == nil {
		p.emit(events.EventMuseAccountsUpdated, list)
	}
}

func (p *Provider) publicAccounts() ([]MuseAccount, error) {
	stored, err := p.accounts.list()
	if err != nil {
		return nil, err
	}
	out := make([]MuseAccount, len(stored))
	for i, a := range stored {
		out[i] = a.MuseAccount
	}
	return out, nil
}

// cookieMarker records which cookie import a profile was seeded with, so
// switching machines or recreating a profile seeds it again.
const cookieMarker = ".snakemedia-muse-cookies"

// Flags every account browser gets. WebRTC would otherwise reach STUN servers
// directly over UDP and reveal the real IP even behind a proxy.
var privacyArgs = []string{
	"--force-webrtc-ip-handling-policy=disable_non_proxied_udp",
	"--webrtc-ip-handling-policy=disable_non_proxied_udp",
}

// directRoute names a browser that runs without a proxy.
const directRoute = "direct"

// proxyRoute is how one account's Chrome reaches the network: key identifies
// the proxy (so a changed proxy restarts the browser), args are Chrome flags.
type proxyRoute struct {
	key  string
	args []string
}

// deadProxySet copies the proxies marked dead, dropping expired marks. The
// copy is taken under p.mu: job goroutines mark proxies dead concurrently.
func (p *Provider) deadProxySet(now time.Time) map[string]time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	dead := make(map[string]time.Time, len(p.deadProxies))
	for k, exp := range p.deadProxies {
		if now.After(exp) {
			delete(p.deadProxies, k)
			continue
		}
		dead[k] = exp
	}
	return dead
}

// markProxyDead keeps a failing proxy out of automatic picks for a while.
func (p *Provider) markProxyDead(raw string, now time.Time) {
	if raw == "" || raw == "none" {
		return
	}
	p.mu.Lock()
	p.deadProxies[raw] = now.Add(10 * time.Minute)
	p.mu.Unlock()
}

// accountProxyLine is the proxy line an account uses now, or "".
func accountProxyLine(acc storedAccount) string {
	if acc.Proxy == "none" {
		return ""
	}
	if acc.Proxy != "" {
		return acc.Proxy
	}
	return acc.ProxyAuto
}

// setupProxy picks the account's proxy and prepares its local bridge. Any
// failure is returned: an account is never silently sent out directly.
func (p *Provider) setupProxy(ctx context.Context, acc *storedAccount) (proxyRoute, error) {
	now := p.now()
	text, err := p.proxies.GetText()
	if err != nil {
		p.updateAccount(acc.ID, func(a *storedAccount) {
			a.ProxyStatus, a.Message = "dead", err.Error()
		})
		return proxyRoute{}, err
	}
	accounts, _ := p.accounts.list()
	parsed, autoLine, err := pickProxy(*acc, accounts, text, p.deadProxySet(now), now)
	if err != nil {
		p.updateAccount(acc.ID, func(a *storedAccount) {
			a.ProxyStatus = "dead"
			a.Message = err.Error()
		})
		return proxyRoute{}, err
	}
	if parsed == nil {
		p.updateAccount(acc.ID, func(a *storedAccount) {
			a.ProxyStatus = ""
		})
		return proxyRoute{key: directRoute, args: privacyArgs}, nil
	}

	if autoLine != "" && autoLine != acc.ProxyAuto {
		p.updateAccount(acc.ID, func(a *storedAccount) {
			a.ProxyAuto = autoLine
		})
		acc.ProxyAuto = autoLine
	}

	if parsed.Type == ProxyTypeRotating {
		res := fetchRotating(ctx, parsed, false)
		if !res.OK {
			p.markProxyDead(parsed.Raw, now)
			p.updateAccount(acc.ID, func(a *storedAccount) {
				a.ProxyStatus = string(res.Status)
				a.Message = "Proxy xoay lỗi: " + res.Message
			})
			return proxyRoute{}, fmt.Errorf("Proxy xoay lỗi: %s", res.Message)
		}
		parsed.Host = res.IP
		parsed.Port = res.Port
		parsed.Scheme = "http"
		p.updateAccount(acc.ID, func(a *storedAccount) {
			a.ProxyStatus = "live"
			a.ProxyIP = res.IP
		})
	}

	bridgePort, err := p.bridges.GetBridge(parsed)
	if err != nil {
		return proxyRoute{}, err
	}
	if parsed.Type == ProxyTypeStatic {
		p.updateAccount(acc.ID, func(a *storedAccount) {
			a.ProxyStatus = "live"
			a.ProxyIP = parsed.Host
		})
	}
	args := append([]string{fmt.Sprintf("--proxy-server=http://127.0.0.1:%d", bridgePort)}, privacyArgs...)
	return proxyRoute{key: parsed.Raw, args: args}, nil
}

// openPage starts (or reuses) the account's browser, seeds imported cookies
// once per import, and makes sure a muse.ai tab is open.
func (p *Provider) openPage(ctx context.Context, acc storedAccount) (Page, error) {
	route, err := p.setupProxy(ctx, &acc)
	if err != nil {
		return nil, err
	}
	b, err := p.browsers.get(ctx, acc.ProfileDir, false, route.key, route.args...)
	if err != nil {
		p.markProxyDead(accountProxyLine(acc), p.now())
		return nil, err
	}
	if len(acc.Cookies) > 0 {
		marker := filepath.Join(b.ProfileDir, cookieMarker)
		if seeded, _ := os.ReadFile(marker); string(seeded) != acc.CookieVersion {
			if err := injectCookies(ctx, b.Port, acc.Cookies); err != nil {
				return nil, err
			}
			_ = os.WriteFile(marker, []byte(acc.CookieVersion), 0o600)
		}
	}
	if err := openMuseTab(ctx, b.Port); err != nil {
		p.markProxyDead(accountProxyLine(acc), p.now())
		return nil, err
	}
	cfg := p.cfg.Get()
	return &chromePage{
		port:        b.Port,
		callTimeout: time.Duration(cfg.Accounts.RequestTimeoutSeconds*4) * time.Second,
		sendTimeout: time.Duration(cfg.Muse.SendTimeoutSeconds) * time.Second,
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

func (p *Provider) pausedProjects() map[string]bool {
	data := p.studio.get()
	res := make(map[string]bool)
	for _, pr := range data.Projects {
		if pr.Paused {
			res[pr.ID] = true
		}
	}
	return res
}
