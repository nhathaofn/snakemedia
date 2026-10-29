package accounts

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/events"
	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/proxy"
	"github.com/google/uuid"
)

// MaxName bounds an account's label.
const MaxName = 60

// cookieMarker records which cookie import a profile was seeded with, so a
// recreated profile is seeded again.
const cookieMarker = ".snakemedia-cookies"

// browserPool is the part of browser.Pool the service uses.
type browserPool interface {
	Get(ctx context.Context, profileDir string, want browser.Want) (*browser.Browser, error)
	Acquire(profileDir string) (release func())
	InUse(profileDir string) bool
	Running(profileDir string) *browser.Browser
	CloseIfIdle(profileDir string) error
	CloseAll()
	RemoveIfIdle(profileDir string) error
	NewProfile() string
	OnClosed(fn func(profileDir string))
	Reap(ctx context.Context)
}

// Service owns all accounts, their browsers and the proxy list.
type Service struct {
	cfg     *config.Manager
	store   *store
	pool    browserPool
	proxies *proxy.Manager
	sites   map[Provider]Site
	// Seams for tests.
	now       func() time.Time
	sleep     func(context.Context, time.Duration) error
	ensureTab func(context.Context, int, config.AccountConfig, browser.Site) error
	inject    func(context.Context, int, config.AccountConfig, []browser.Cookie) error

	mu        sync.Mutex
	ctx       context.Context
	emit      func(events.Event, any)
	routes    map[string]proxy.Route // account ID -> route its running browser uses
	watching  map[string]bool        // accounts with an open sign-in watcher
	listeners []func()
	removed   []func(Record)
	// notifyDelay coalesces account changes into one event (0: notify at once).
	notifyDelay time.Duration
	notifyTimer *time.Timer
	// background tracks checks and sign-in watchers so Close can let them
	// finish their last store write.
	background sync.WaitGroup
}

// notifyDelay is how long account changes are gathered before the frontend and
// the providers hear about them: a check or a send writes an account several
// times in a row, and each event carries the whole list.
const notifyDelay = 150 * time.Millisecond

// New keeps accounts, profiles and the proxy list under dir.
func New(dir string, cfg *config.Manager, sites ...Site) *Service {
	s := &Service{
		cfg:       cfg,
		store:     newStore(filepath.Join(dir, "accounts.dat")),
		pool:      browser.NewPool(cfg, filepath.Join(dir, "profiles")),
		proxies:   proxy.NewManager(filepath.Join(dir, "proxies.dat")),
		sites:     make(map[Provider]Site, len(sites)),
		now:       time.Now,
		sleep:     sleep,
		ensureTab: browser.EnsureTab,
		inject:    browser.InjectCookies,
		ctx:       context.Background(),
		emit:      func(events.Event, any) {},
		routes:    make(map[string]proxy.Route),
		watching:  make(map[string]bool),

		notifyDelay: notifyDelay,
	}
	for _, site := range sites {
		s.sites[site.Provider()] = site
	}
	s.proxies.SetTimeout(func() time.Duration {
		return time.Duration(cfg.Get().Accounts.RequestTimeoutSeconds) * time.Second
	})
	return s
}

// Start sets the lifetime of background work (sign-in watchers, cookie
// checks) and how changes reach the frontend.
func (s *Service) Start(ctx context.Context, emit func(events.Event, any)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ctx = ctx
	if emit != nil {
		s.emit = emit
	}
	s.pool.OnClosed(s.browserClosed)
	s.pool.Reap(ctx)
}

// browserClosed forgets the route of a browser the pool closed while idle and
// frees the proxy bridges nothing runs on any more.
func (s *Service) browserClosed(profileDir string) {
	if records, err := s.store.list(); err == nil {
		s.mu.Lock()
		for _, rec := range records {
			if filepath.Clean(rec.ProfileDir) == filepath.Clean(profileDir) {
				delete(s.routes, rec.ID)
			}
		}
		s.mu.Unlock()
	}
	s.pruneBridges()
}

// pruneBridges closes the local proxy bridges no recorded route uses.
func (s *Service) pruneBridges() {
	s.mu.Lock()
	inUse := make(map[int]bool, len(s.routes))
	for _, route := range s.routes {
		if route.URL != nil {
			if port, err := strconv.Atoi(route.URL.Port()); err == nil {
				inUse[port] = true
			}
		}
	}
	s.mu.Unlock()
	s.proxies.PruneBridges(inUse)
}

// Close shuts every account browser and proxy bridge down.
func (s *Service) Close() {
	s.mu.Lock()
	if s.notifyTimer != nil {
		s.notifyTimer.Stop()
		s.notifyTimer = nil
	}
	s.mu.Unlock()
	s.pool.CloseAll()
	s.proxies.Close()
	// With the browsers gone, running checks end quickly; a stuck one does
	// not hold up the app's exit for long.
	done := make(chan struct{})
	go func() {
		s.background.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(closeWait):
	}
}

// closeWait bounds how long Close waits for background account work.
const closeWait = 5 * time.Second

// goBackground runs fn as tracked background work (see Close).
func (s *Service) goBackground(fn func()) {
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		defer logger.Recover("tác vụ tài khoản")
		fn()
	}()
}

// OnChange registers fn to run after any account changes.
func (s *Service) OnChange(fn func()) {
	s.mu.Lock()
	s.listeners = append(s.listeners, fn)
	s.mu.Unlock()
}

// OnRemove registers fn to run after an account is removed.
func (s *Service) OnRemove(fn func(Record)) {
	s.mu.Lock()
	s.removed = append(s.removed, fn)
	s.mu.Unlock()
}

func (s *Service) lifetime() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ctx
}

// notify tells the frontend and the providers that accounts changed, once per
// notifyDelay however many changes arrive meanwhile. The event is built when it
// fires, so it always carries the latest state.
func (s *Service) notify() {
	s.mu.Lock()
	if s.notifyDelay <= 0 {
		s.mu.Unlock()
		s.notifyNow()
		return
	}
	if s.notifyTimer == nil {
		s.notifyTimer = time.AfterFunc(s.notifyDelay, func() {
			s.mu.Lock()
			s.notifyTimer = nil
			s.mu.Unlock()
			s.notifyNow()
		})
	}
	s.mu.Unlock()
}

func (s *Service) notifyNow() {
	s.mu.Lock()
	emit, listeners := s.emit, append([]func(){}, s.listeners...)
	s.mu.Unlock()
	if list, err := s.public(""); err == nil {
		emit(events.EventAccountsUpdated, list)
	}
	for _, fn := range listeners {
		fn()
	}
}

func (s *Service) public(provider Provider) ([]Account, error) {
	all, err := s.store.list()
	if err != nil {
		return nil, err
	}
	records := make([]Record, 0, len(all))
	for _, rec := range all {
		if provider == "" || rec.Provider == provider {
			records = append(records, rec)
		}
	}
	routeErrs := s.routeErrors(records, all)
	out := make([]Account, len(records))
	for i, rec := range records {
		out[i] = rec.Account
		out[i].NoRoute = ""
		if err := routeErrs[i]; err != nil {
			out[i].NoRoute = err.Error()
		}
		if out[i].Usage == nil {
			out[i].Usage = []UsageItem{}
		}
	}
	return out, nil
}

// ---------- Runtime API for providers ----------

// Records lists the accounts of provider ("" for all) in saved order.
func (s *Service) Records(provider Provider) ([]Record, error) {
	records, err := s.store.list()
	if err != nil || provider == "" {
		return records, err
	}
	kept := records[:0]
	for _, rec := range records {
		if rec.Provider == provider {
			kept = append(kept, rec)
		}
	}
	return kept, nil
}

// Record returns one account.
func (s *Service) Record(id string) (Record, error) { return s.store.get(id) }

// Update applies change to an account, saves it and tells listeners.
func (s *Service) Update(id string, change func(*Record)) (Record, error) {
	rec, err := s.store.update(id, s.now(), change)
	if err != nil {
		if !errors.Is(err, errNotFound) {
			logger.Get().Warn("cannot save account", "account", id, "error", err)
		}
		return rec, err
	}
	s.notify()
	return rec, nil
}

// Seed stores rec as given, without a browser. Providers' tests use it to set
// up accounts in a known state.
func (s *Service) Seed(rec Record) error {
	if err := s.store.add(rec); err != nil {
		return err
	}
	s.notify()
	return nil
}

// Browser returns the account's running browser (a window when one is open)
// or starts a headless one on the account's proxy, with its site's tab open.
func (s *Service) Browser(ctx context.Context, id string) (*browser.Browser, error) {
	rec, site, err := s.lookup(id)
	if err != nil {
		return nil, err
	}
	return s.open(ctx, rec, site, false)
}

// errNoRoute refuses a provider's HTTP request when the account's browser has
// no recorded route (not started yet, or closed for a new proxy meanwhile):
// going direct then would leak the real IP.
var errNoRoute = errors.New("Trình duyệt của tài khoản chưa chạy trên proxy nào (vừa đổi proxy hoặc đã đóng). Hãy thử lại.")

// ProxyURL is the local HTTP proxy the account's browser runs on, for the
// provider's own HTTP clients. It is nil without error only when the account
// is set to go direct ("Không dùng proxy"); an account without a recorded
// route is an error, never a direct connection.
func (s *Service) ProxyURL(id string) (*url.URL, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	route, ok := s.routes[id]
	switch {
	case !ok:
		return nil, errNoRoute
	case route.Key == proxy.DirectRoute:
		return nil, nil
	case route.URL == nil:
		return nil, errNoRoute
	}
	return route.URL, nil
}

// Acquire marks the account's browser as needed by running work until the
// returned release is called; it is then never restarted or removed.
func (s *Service) Acquire(id string) (release func()) {
	rec, err := s.store.get(id)
	if err != nil {
		return func() {}
	}
	return s.pool.Acquire(rec.ProfileDir)
}

// AcquireProfile is Acquire for callers that hold the account's profile path.
func (s *Service) AcquireProfile(profileDir string) (release func()) {
	return s.pool.Acquire(profileDir)
}

// InUse reports whether running work holds the account's browser.
func (s *Service) InUse(id string) bool {
	rec, err := s.store.get(id)
	return err == nil && s.pool.InUse(rec.ProfileDir)
}

func (s *Service) lookup(id string) (Record, Site, error) {
	rec, err := s.store.get(id)
	if err != nil {
		return Record{}, nil, err
	}
	site := s.sites[rec.Provider]
	if site == nil {
		return Record{}, nil, fmt.Errorf("Không hỗ trợ tài khoản %s.", rec.Provider)
	}
	return rec, site, nil
}

// open starts or reuses the account's browser on its proxy route, seeds
// imported cookies once per import, and makes sure the site's tab is open.
func (s *Service) open(ctx context.Context, rec Record, site Site, visible bool) (*browser.Browser, error) {
	route, err := s.route(ctx, rec)
	if err != nil {
		return nil, err
	}
	// A Chrome that fails to start says nothing about the proxy, so only a
	// page that fails to load through it marks the proxy dead.
	b, err := s.pool.Get(ctx, rec.ProfileDir, browser.Want{Visible: visible, Route: route.Key, Args: route.Args})
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	previous, had := s.routes[rec.ID]
	s.routes[rec.ID] = route
	s.mu.Unlock()
	if had && !sameRoute(previous, route) {
		s.pruneBridges()
	}
	policy := s.cfg.Get().Accounts
	if len(rec.Cookies) > 0 {
		marker := filepath.Join(b.ProfileDir, cookieMarker)
		if seeded, _ := os.ReadFile(marker); string(seeded) != rec.CookieVersion {
			if err := s.inject(ctx, b.Port, policy, rec.Cookies); err != nil {
				return nil, err
			}
			_ = os.WriteFile(marker, []byte(rec.CookieVersion), 0o600)
		}
	}
	if err := s.ensureTab(ctx, b.Port, policy, site.Page()); err != nil {
		if route.URL != nil && browser.IsProxyError(err) {
			s.proxies.MarkDead(assignment(rec).Line())
			s.Update(rec.ID, func(r *Record) { r.ProxyStatus, r.Message = string(proxy.StatusDead), err.Error() })
		}
		return nil, err
	}
	if route.URL != nil {
		// The site loaded through the proxy: that is what "live" means.
		if cur, err := s.store.get(rec.ID); err == nil && cur.ProxyStatus != string(proxy.StatusLive) {
			s.Update(rec.ID, func(r *Record) { r.ProxyStatus = string(proxy.StatusLive) })
		}
	}
	return b, nil
}

// RouteError reports why the account cannot get a proxy route now (nil when it
// can, or its browser already runs on one), without changing anything. A
// provider leaves such an account out of the accounts it hands work to.
func (s *Service) RouteError(rec Record) error {
	s.mu.Lock()
	_, ok := s.routes[rec.ID]
	s.mu.Unlock()
	if ok && s.pool.Running(rec.ProfileDir) != nil {
		return nil
	}
	records, err := s.store.list()
	if err != nil {
		return err
	}
	others := make([]proxy.Assignment, 0, len(records))
	for _, other := range records {
		others = append(others, assignment(other))
	}
	return s.proxies.Usable(assignment(rec), others)
}

// routeErrors is RouteError for each of records, with all the saved records
// read once by the caller and the proxy list and dead marks read once here.
func (s *Service) routeErrors(records, all []Record) []error {
	errs := make([]error, len(records))
	var check []int
	for i, rec := range records {
		s.mu.Lock()
		_, ok := s.routes[rec.ID]
		s.mu.Unlock()
		if !ok || s.pool.Running(rec.ProfileDir) == nil {
			check = append(check, i)
		}
	}
	if len(check) == 0 {
		return errs
	}
	others := make([]proxy.Assignment, 0, len(all))
	for _, other := range all {
		others = append(others, assignment(other))
	}
	wanted := make([]proxy.Assignment, len(check))
	for j, i := range check {
		wanted[j] = assignment(records[i])
	}
	for j, err := range s.proxies.UsableEach(wanted, others) {
		errs[check[j]] = err
	}
	return errs
}

// sameRoute reports whether two routes reach the network the same way: same
// proxy key and same local bridge address.
func sameRoute(a, b proxy.Route) bool {
	if a.Key != b.Key || (a.URL == nil) != (b.URL == nil) {
		return false
	}
	return a.URL == nil || a.URL.String() == b.URL.String()
}

// route returns the proxy route the account's browser must use: the one it
// runs on, or a fresh pick that is recorded on the account.
func (s *Service) route(ctx context.Context, rec Record) (proxy.Route, error) {
	s.mu.Lock()
	cached, ok := s.routes[rec.ID]
	s.mu.Unlock()
	if ok && s.pool.Running(rec.ProfileDir) != nil {
		return cached, nil
	}
	records, err := s.store.list()
	if err != nil {
		return proxy.Route{}, err
	}
	others := make([]proxy.Assignment, 0, len(records))
	for _, other := range records {
		others = append(others, assignment(other))
	}
	route, err := s.proxies.Route(ctx, assignment(rec), others)
	if err != nil {
		s.Update(rec.ID, func(r *Record) { r.ProxyStatus, r.Message = string(proxy.StatusDead), err.Error() })
		return proxy.Route{}, err
	}
	s.Update(rec.ID, func(r *Record) {
		if route.Auto != "" {
			r.ProxyAuto = route.Auto
		}
		if route.URL == nil {
			r.ProxyInUse, r.ProxyRotating, r.ProxyStatus, r.ProxyIP = "", false, "", ""
			return
		}
		line := assignment(*r).Line()
		r.ProxyInUse, r.ProxyRotating = proxy.Mask(line), rotating(line)
		// Live only once a page has loaded through it (open).
		r.ProxyStatus, r.ProxyIP = "", route.IP
	})
	return route, nil
}

func rotating(line string) bool {
	parsed := proxy.Parse(line)
	return parsed != nil && parsed.Type == proxy.TypeRotating
}

// assignment is how the account uses the proxy list. An account that has
// signed in keeps its pinned proxy: the site knows its session from that IP.
func assignment(rec Record) proxy.Assignment {
	return proxy.Assignment{Account: rec.ID, Name: rec.Label(), Proxy: rec.Proxy, Auto: rec.ProxyAuto, Keep: rec.Identity != ""}
}

// closeBrowser stops the account's browser so its next start picks up a new
// route or cookies. It returns browser.ErrProfileBusy, and changes nothing,
// while work holds the browser.
func (s *Service) closeBrowser(rec Record) error {
	if err := s.pool.CloseIfIdle(rec.ProfileDir); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.routes, rec.ID)
	s.mu.Unlock()
	s.pruneBridges()
	return nil
}

// ---------- Checking and signing in ----------

// waitSignedIn polls the site until it reports a signed-in session or
// timeout passes. The last inspection error is returned when it never did.
func (s *Service) waitSignedIn(ctx context.Context, site Site, b *browser.Browser, rec Record, timeout time.Duration) (Inspection, error) {
	poll := time.Duration(s.cfg.Get().Accounts.PollIntervalMS) * time.Millisecond
	deadline := s.now().Add(timeout)
	for {
		insp, err := site.Inspect(ctx, b, rec)
		if err == nil && insp.SignedIn {
			return insp, nil
		}
		if ctx.Err() != nil {
			return Inspection{}, ctx.Err()
		}
		if !s.now().Before(deadline) {
			return insp, err
		}
		if err := s.sleep(ctx, poll); err != nil {
			return Inspection{}, err
		}
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// check opens the account's page, confirms the session and reads its usage.
// Imported cookies are seeded once more when the page is signed out.
func (s *Service) check(ctx context.Context, id string) (Record, error) {
	rec, site, err := s.lookup(id)
	if err != nil {
		return Record{}, err
	}
	b, err := s.open(ctx, rec, site, false)
	if err != nil {
		updated, _ := s.Update(id, func(r *Record) { r.Status, r.Message = StatusError, err.Error() })
		return updated, err
	}
	policy := s.cfg.Get().Accounts
	timeout := time.Duration(policy.PageReadySeconds) * time.Second
	insp, err := s.waitSignedIn(ctx, site, b, rec, timeout)
	if err == nil && !insp.SignedIn && len(rec.Cookies) > 0 {
		if s.inject(ctx, b.Port, policy, rec.Cookies) == nil {
			if tab, found, _ := browser.FindTab(ctx, b.Port, policy, site.Page().Owns); found {
				_ = browser.Navigate(ctx, tab, b.Port, site.Page())
			}
			insp, err = s.waitSignedIn(ctx, site, b, rec, timeout)
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return rec, ctxErr
	}
	if err != nil {
		updated, _ := s.Update(id, func(r *Record) {
			r.Status, r.Message, r.LastCheckedAt = StatusError, err.Error(), s.now().UTC().Format(time.RFC3339)
		})
		return updated, err
	}
	return s.apply(id, insp)
}

// apply records what an inspection found. A session for another identity
// than the account's, or one already saved as another account, is refused.
func (s *Service) apply(id string, insp Inspection) (Record, error) {
	if insp.SignedIn {
		if conflict := s.identityConflict(id, insp.Identity); conflict != nil {
			updated, _ := s.Update(id, func(r *Record) {
				r.Status, r.Message, r.LastCheckedAt = StatusError, conflict.Error(), s.now().UTC().Format(time.RFC3339)
			})
			return updated, conflict
		}
	}
	return s.Update(id, func(r *Record) {
		r.LastCheckedAt = s.now().UTC().Format(time.RFC3339)
		if !insp.SignedIn {
			r.Status, r.Message = StatusSignedOut, "Chưa đăng nhập. Bấm Mở Chrome để đăng nhập lại"
			if r.Source == SourceCookies {
				r.Message += ", hoặc dán cookie mới"
			}
			r.Message += "."
			return
		}
		r.Status, r.Message = StatusSignedIn, insp.Note
		if insp.Identity != "" {
			r.Identity = insp.Identity
		}
		if insp.Usage != nil {
			r.Usage = insp.Usage
		}
		if insp.Data != nil {
			if err := r.SetData(insp.Data); err != nil {
				r.Message = err.Error()
			}
		}
		if insp.Resume {
			r.PausedUntil = ""
		}
	})
}

func (s *Service) identityConflict(id, identity string) error {
	if identity == "" {
		return nil
	}
	records, err := s.store.list()
	if err != nil {
		return err
	}
	var self Record
	for _, rec := range records {
		if rec.ID == id {
			self = rec
		}
	}
	if self.Identity != "" && !strings.EqualFold(self.Identity, identity) {
		return fmt.Errorf("Chrome đang đăng nhập tài khoản khác (%s). Dữ liệu tài khoản được giữ nguyên.", identity)
	}
	for _, rec := range records {
		if rec.ID != id && rec.Provider == self.Provider && strings.EqualFold(rec.Identity, identity) {
			return fmt.Errorf("%s đã có trong danh sách (%s).", identity, rec.Label())
		}
	}
	return nil
}

// signIn shows the account's browser on its site and watches it until the
// page is signed in, the window closes or the login timeout passes.
func (s *Service) signIn(ctx context.Context, rec Record, site Site) error {
	b, err := s.open(ctx, rec, site, true)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.watching[rec.ID] {
		s.mu.Unlock()
		return nil
	}
	s.watching[rec.ID] = true
	s.mu.Unlock()
	if rec.Status != StatusSignedIn {
		s.Update(rec.ID, func(r *Record) {
			r.Status, r.Message = StatusSigningIn, "Đăng nhập trong cửa sổ Chrome vừa mở; app tự nhận khi xong."
		})
	}
	s.goBackground(func() {
		defer func() {
			s.mu.Lock()
			delete(s.watching, rec.ID)
			s.mu.Unlock()
		}()
		s.watch(b, rec.ID, site)
	})
	return nil
}

func (s *Service) watch(b *browser.Browser, id string, site Site) {
	policy := s.cfg.Get().Accounts
	ctx, cancel := context.WithTimeout(s.lifetime(), time.Duration(policy.LoginTimeoutSeconds)*time.Second)
	defer cancel()
	signedOut := func(message string) {
		s.Update(id, func(r *Record) {
			if r.Status == StatusSigningIn {
				r.Status, r.Message = StatusSignedOut, message
			}
		})
	}
	closed := 0
	for {
		if err := s.sleep(ctx, time.Duration(policy.PollIntervalMS)*time.Millisecond); err != nil {
			signedOut("Hết thời gian chờ đăng nhập.")
			return
		}
		rec, err := s.store.get(id)
		if err != nil {
			return
		}
		if insp, err := site.Inspect(ctx, b, rec); err == nil && insp.SignedIn {
			if follow, ok := site.(SignInFollowUp); ok {
				s.Update(id, func(r *Record) {
					r.Message = "Đã đăng nhập, đang hoàn tất thiết lập tài khoản…"
				})
				insp = follow.AfterSignIn(ctx, b, rec, insp)
			}
			s.apply(id, insp)
			return
		}
		// The user closing the window (or Chrome) ends the sign-in.
		if b.Validate(ctx, policy) != nil {
			if closed++; closed >= 3 {
				signedOut("Đã đóng cửa sổ đăng nhập.")
				return
			}
			continue
		}
		closed = 0
	}
}

func cleanName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if utf8.RuneCountInString(name) > MaxName {
		return "", fmt.Errorf("Tên tài khoản tối đa %d ký tự.", MaxName)
	}
	return name, nil
}

func (s *Service) newRecord(provider Provider, name string, source Source) Record {
	if name == "" {
		// Unnamed accounts are labelled by their site ("Muse 3").
		label := string(provider)
		if site := s.sites[provider]; site != nil {
			label = LabelOf(site)
		}
		records, _ := s.Records(provider)
		name = fmt.Sprintf("%s %d", label, len(records)+1)
	}
	now := s.now().UTC().Format(time.RFC3339)
	return Record{
		Account: Account{
			ID: uuid.NewString(), Provider: provider, Name: name, Source: source,
			Status: StatusUnverified, Enabled: true, Usage: []UsageItem{}, CreatedAt: now, UpdatedAt: now,
		},
		ProfileDir: s.pool.NewProfile(),
	}
}

func (s *Service) site(provider Provider) (Site, error) {
	site := s.sites[provider]
	if site == nil {
		return nil, fmt.Errorf("Không hỗ trợ tài khoản %q.", provider)
	}
	return site, nil
}

func (s *Service) parseCookies(site Site, raw string) ([]browser.Cookie, error) {
	return browser.ParseCookies(raw, site.CookieDomain(), site.AcceptsCookie)
}
