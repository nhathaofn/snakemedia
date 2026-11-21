package muse

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/logger"
)

const (
	// sessionVerifyTTL bounds how long a single Chrome port's login check is
	// trusted without re-verifying /api/session.
	sessionVerifyTTL = 10 * time.Minute
	// recentRecoveryWindow deduplicates concurrent threads hitting an outage on
	// the same account seconds apart.
	recentRecoveryWindow = 5 * time.Second
)

type verifiedSession struct {
	port int
	at   time.Time
}

type refreshOutcome struct {
	acc   accounts.Record
	err   error
	at    time.Time
	epoch int64
}

type accountLease struct {
	accounts *accounts.Service
	id       string
	release  func()
}

func acquireAccountLease(accts *accounts.Service, id string) *accountLease {
	if accts == nil || id == "" {
		return &accountLease{}
	}
	return &accountLease{accounts: accts, id: id, release: accts.Acquire(id)}
}

func (l *accountLease) Release() {
	if l != nil && l.release != nil {
		l.release()
		l.release = nil
	}
}

// RecycleHeadless releases this goroutine's lease briefly so CloseHeadlessIfIdle
// can shut down an idle headless Chrome (and still refuses safely if another
// thread holds a lease on the same account), then re-acquires immediately.
func (l *accountLease) RecycleHeadless() error {
	if l == nil || l.accounts == nil || l.id == "" {
		return nil
	}
	l.Release()
	err := l.accounts.CloseHeadlessIfIdle(l.id)
	l.release = l.accounts.Acquire(l.id)
	return err
}

func (p *Provider) sessionVerified(accountID string, port int) bool {
	if accountID == "" || port <= 0 {
		return false
	}
	now := p.now()
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.verifiedPorts[accountID]
	if !ok || v.port != port {
		return false
	}
	return now.Sub(v.at) < sessionVerifyTTL
}

func (p *Provider) markSessionVerified(accountID string, port int) {
	if accountID == "" || port <= 0 {
		return
	}
	now := p.now()
	p.mu.Lock()
	if p.verifiedPorts != nil {
		p.verifiedPorts[accountID] = verifiedSession{port: port, at: now}
	}
	p.mu.Unlock()
}

func (p *Provider) clearSessionVerified(accountID string) {
	if accountID == "" {
		return
	}
	p.mu.Lock()
	delete(p.verifiedPorts, accountID)
	p.mu.Unlock()
}

func (p *Provider) accountRefreshEpoch(accountID string) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sessionEpoch[accountID]
}

func (p *Provider) bumpRefreshEpoch(accountID string) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sessionEpoch == nil {
		p.sessionEpoch = make(map[string]int64)
	}
	p.sessionEpoch[accountID]++
	return p.sessionEpoch[accountID]
}

func (p *Provider) recentRefreshOutcome(accountID string, sinceEpoch int64) (refreshOutcome, bool) {
	now := p.now()
	p.mu.Lock()
	defer p.mu.Unlock()
	out, ok := p.lastRefresh[accountID]
	if !ok || out.epoch <= sinceEpoch || now.Sub(out.at) > recentRecoveryWindow {
		return refreshOutcome{}, false
	}
	return out, true
}

func (p *Provider) recordRefreshOutcome(accountID string, epoch int64, acc accounts.Record, err error) {
	now := p.now()
	p.mu.Lock()
	if p.lastRefresh == nil {
		p.lastRefresh = make(map[string]refreshOutcome)
	}
	p.lastRefresh[accountID] = refreshOutcome{acc: acc, err: err, at: now, epoch: epoch}
	if err == nil {
		delete(p.accountBackoff, accountID)
	}
	p.mu.Unlock()
}

func (p *Provider) clearAccountBackoff(accountID string) {
	if accountID == "" {
		return
	}
	p.mu.Lock()
	delete(p.accountBackoff, accountID)
	p.mu.Unlock()
}

// refreshAccountForRun refreshes accountID, its session/cookies, quota/limits,
// and active/idle tabs before settling an account or connection failure.
// Sibling threads hitting the same failure concurrently share one refresh under
// loginSlot and adopt the refreshed outcome.
func (p *Provider) refreshAccountForRun(ctx context.Context, accountID string, jobKind MuseJobKind, currentPage *Page, lease *accountLease, reason string) (accounts.Record, bool, error) {
	if accountID == "" || accountID == "api" || p.accounts == nil {
		return accounts.Record{}, false, nil
	}
	acc, err := p.accounts.Record(accountID)
	if err != nil {
		return accounts.Record{}, false, err
	}
	beforeEpoch := p.accountRefreshEpoch(accountID)
	releaseLogin, err := p.loginSlot(ctx, accountID)
	if err != nil {
		return acc, false, err
	}
	defer releaseLogin()

	// Another thread may already have finished refreshing while we waited.
	if out, ok := p.recentRefreshOutcome(accountID, beforeEpoch); ok {
		if latest, recErr := p.accounts.Record(accountID); recErr == nil && latest.ID != "" {
			acc = latest
		}
		if out.err != nil {
			return acc, false, out.err
		}
		cfg := p.cfg.Get().Muse
		if usableForJob(museView(acc), jobKind, cfg, p.now()) {
			if err := p.rebindRefreshedPage(ctx, acc, currentPage); err != nil {
				return acc, false, &notSentError{err: err}
			}
			return acc, true, nil
		}
		return acc, false, p.unusableErrorForAccount(acc, jobKind, cfg)
	}

	epoch := p.bumpRefreshEpoch(accountID)
	p.clearSessionVerified(accountID)

	page := Page(nil)
	if currentPage != nil {
		page = *currentPage
	}
	oldPort := 0
	keepID := ""
	keepJobID := ""
	if cp, isChrome := page.(*chromePage); isChrome && cp.port > 0 {
		oldPort = cp.port
		keepID, keepJobID = cp.boundTab()
		if p.tabs != nil {
			p.tabs.invalidateAccount(ctx, accountID, cp.port, keepID, cp.policy)
		}
	}

	timeout := 30 * time.Second
	if p.cfg != nil {
		if ready := time.Duration(p.cfg.Get().Accounts.PageReadySeconds) * time.Second; ready > 0 {
			timeout = ready
		}
	}

	var loginErr error
	otherBusy := p.tabs != nil && oldPort > 0 && p.tabs.hasOtherBusy(accountID, oldPort, keepID, keepJobID)
	if page == nil {
		page, loginErr = p.openRefreshedPage(ctx, acc)
		if currentPage != nil {
			*currentPage = page
		}
		if loginErr == nil {
			loginErr = p.waitSignedInForRefresh(ctx, page, timeout, false)
		}
	} else if reason == "ensure_signed_in" && oldPort > 0 && !otherBusy {
		// ensureSignedIn already waited PageReadySeconds and tried Home on this
		// idle Chrome tab; fall directly into recycling headless Chrome once.
		loginErr = errNotSignedIn
	} else {
		// Navigate Home first so stale NextAuth/OAuth sessions or disconnected
		// gateways reload cleanly before judging login state.
		loginErr = p.waitSignedInForRefresh(ctx, page, timeout, true)
	}

	if errors.Is(loginErr, errNotSignedIn) && ctx.Err() == nil && p.accounts != nil {
		if cp, isChrome := page.(*chromePage); isChrome && cp.port > 0 {
			if p.accounts.RestoreCookies(ctx, accountID, cp.port) {
				logger.Get().Info("restored backed-up cookies during muse refresh", "account", accountID, "reason", reason)
				loginErr = p.waitSignedInForRefresh(ctx, page, timeout, true)
			}
		}
	}

	// If reloading the current tab still looks broken (dead tab, stale 401,
	// gateway down, or CDP hang) and no other thread is mid-work on Chrome,
	// recycle headless Chrome once cleanly and test on a fresh browser.
	var restricted *accountRestrictedError
	if loginErr != nil && !errors.As(loginErr, &restricted) && ctx.Err() == nil && lease != nil {
		if cp, isChrome := page.(*chromePage); isChrome && cp.port > 0 && (p.tabs == nil || !p.tabs.hasOtherBusy(accountID, cp.port, keepID, keepJobID)) {
			prevPort := cp.port
			logger.Get().Info("recycling muse browser during account refresh", "account", accountID, "reason", reason, "error", loginErr)
			closePage(page)
			if currentPage != nil {
				*currentPage = nil
			}
			page = nil
			p.dropBrowserState(accountID)
			if rErr := lease.RecycleHeadless(); rErr == nil {
				if freshPage, openErr := p.openRefreshedPage(ctx, acc); openErr == nil {
					page = freshPage
					if currentPage != nil {
						*currentPage = page
					}
					newPort := 0
					if ncp, ok := page.(*chromePage); ok {
						newPort = ncp.port
					}
					loginErr = p.waitSignedInForRefresh(ctx, page, timeout, newPort == prevPort)
				} else {
					loginErr = openErr
				}
			}
		}
	}

	if ctx.Err() != nil {
		return acc, false, ctx.Err()
	}

	if loginErr != nil {
		switch {
		case errors.As(loginErr, &restricted):
			if restricted.dead {
				p.disableRestrictedAccount(accountID, restricted.Error())
			} else {
				p.disableVideoForAccount(accountID, restricted.reason)
			}
			fresh, _ := p.accounts.Record(accountID)
			if fresh.ID != "" {
				acc = fresh
			}
			p.recordRefreshOutcome(accountID, epoch, acc, loginErr)
			return acc, false, loginErr
		case errors.Is(loginErr, errNotSignedIn):
			p.markRecycleBrowser(accountID)
			p.updateAccount(accountID, func(r *accounts.Record, _ *museData) {
				r.Status = accounts.StatusSignedOut
				r.Message = "Phiên Muse đã đăng xuất (đã thử làm mới); hãy mở Chrome để đăng nhập lại."
				r.LastCheckedAt = p.now().UTC().Format(time.RFC3339)
			})
			fresh, _ := p.accounts.Record(accountID)
			if fresh.ID != "" {
				acc = fresh
			}
			p.recordRefreshOutcome(accountID, epoch, acc, errNotSignedIn)
			return acc, false, errNotSignedIn
		default:
			p.markRecycleBrowser(accountID)
			var notSent *notSentError
			if !errors.As(loginErr, &notSent) {
				loginErr = &notSentError{err: loginErr}
			}
			p.recordRefreshOutcome(accountID, epoch, acc, loginErr)
			return acc, false, loginErr
		}
	}

	// Signed in! Refresh quota & capabilities so limits/bans/usage stay current.
	cfg := p.cfg.Get().Muse
	p.refreshQuota(ctx, accountID, page)
	if cp, ok := page.(*chromePage); ok && cp.port > 0 && p.accounts != nil {
		p.accounts.SyncCookies(ctx, accountID, cp.port)
	}
	var data museData
	if freshAcc, rErr := p.accounts.Record(accountID); rErr == nil {
		acc = freshAcc
		_ = accounts.DecodeData(freshAcc, &data)
	}
	if !capabilitiesFresh(data.Capabilities, p.now()) {
		capsCtx, cancelCaps := context.WithTimeout(ctx, 15*time.Second)
		if caps, cErr := page.Capabilities(capsCtx); cErr == nil && caps != nil {
			p.updateAccount(accountID, func(_ *accounts.Record, d *museData) {
				d.Capabilities = caps
			})
		}
		cancelCaps()
	}

	if freshAcc, rErr := p.accounts.Record(accountID); rErr == nil {
		acc = freshAcc
		_ = accounts.DecodeData(freshAcc, &data)
	}

	if data.Quota != nil {
		switch kind, detail := classifyRestriction("", nil, "", data.Quota); kind {
		case restrictDeadAccount:
			errDead := &accountRestrictedError{dead: true, reason: detail}
			p.disableRestrictedAccount(accountID, errDead.Error())
			fresh, _ := p.accounts.Record(accountID)
			if fresh.ID != "" {
				acc = fresh
			}
			p.recordRefreshOutcome(accountID, epoch, acc, errDead)
			return acc, false, errDead
		case restrictDailyVideo24h:
			p.pauseDailyVideoLimit(accountID, detail)
			errLimit := &dailyVideoLimitError{reason: detail}
			fresh, _ := p.accounts.Record(accountID)
			if fresh.ID != "" {
				acc = fresh
			}
			p.recordRefreshOutcome(accountID, epoch, acc, errLimit)
			return acc, false, errLimit
		case restrictVideoDisabled:
			p.disableVideoForAccount(accountID, detail)
			fresh, _ := p.accounts.Record(accountID)
			if fresh.ID != "" {
				acc = fresh
			}
			if jobKind == MuseJobVideo {
				errVid := &accountRestrictedError{dead: false, reason: detail}
				p.recordRefreshOutcome(accountID, epoch, acc, errVid)
				return acc, false, errVid
			}
		}
	}

	// Account recovered and verified alive.
	p.updateAccount(accountID, func(r *accounts.Record, d *museData) {
		r.Status = accounts.StatusSignedIn
		r.LastCheckedAt = p.now().UTC().Format(time.RFC3339)
		if !r.Enabled && r.AutoResume && r.PausedUntil != "" {
			if until, parseErr := time.Parse(time.RFC3339, r.PausedUntil); parseErr == nil && !until.After(p.now()) {
				r.Enabled = true
				r.AutoResume = false
				r.PausedUntil = ""
			}
		}
		if quotaResumes(*d, d.Quota) && r.PausedUntil != "" {
			r.PausedUntil = ""
			d.RestReason = ""
		}
		if r.PausedUntil == "" && !r.SkipVideoCheck {
			r.Message = ""
		}
	})

	if freshAcc, rErr := p.accounts.Record(accountID); rErr == nil {
		acc = freshAcc
	}
	if cp, ok := page.(*chromePage); ok && cp.port > 0 {
		p.accounts.SyncCookies(ctx, accountID, cp.port)
		p.markSessionVerified(accountID, cp.port)
	}

	view := museView(acc)
	if !pausedUntil(view, cfg, p.now()).IsZero() {
		errOut := p.unusableErrorForAccount(acc, jobKind, cfg)
		p.recordRefreshOutcome(accountID, epoch, acc, errOut)
		return acc, false, errOut
	}
	if acc.SkipVideoCheck && jobKind == MuseJobVideo {
		errVid := &accountRestrictedError{dead: false, reason: "tài khoản chỉ tạo ảnh"}
		p.recordRefreshOutcome(accountID, epoch, acc, nil)
		return acc, false, errVid
	}

	p.recordRefreshOutcome(accountID, epoch, acc, nil)
	return acc, true, nil
}

func (p *Provider) openRefreshedPage(ctx context.Context, acc accounts.Record) (Page, error) {
	page, err := p.pageFor(ctx, acc)
	if err != nil {
		return nil, err
	}
	if err := p.assignTab(ctx, "", acc.ID, page, p.cfg.Get().Muse); err != nil {
		closePage(page)
		return nil, err
	}
	return page, nil
}

func (p *Provider) rebindRefreshedPage(ctx context.Context, acc accounts.Record, currentPage *Page) error {
	if currentPage == nil {
		return nil
	}
	if _, isChrome := (*currentPage).(*chromePage); !isChrome {
		if *currentPage != nil {
			return (*currentPage).Home(ctx)
		}
		return nil
	}
	closePage(*currentPage)
	*currentPage = nil
	next, err := p.openRefreshedPage(ctx, acc)
	if err != nil {
		return err
	}
	*currentPage = next
	return nil
}

func (p *Provider) unusableErrorForAccount(acc accounts.Record, jobKind MuseJobKind, cfg config.MuseConfig) error {
	var data museData
	_ = accounts.DecodeData(acc, &data)
	switch {
	case !acc.Enabled && acc.Status == accounts.StatusError:
		return &accountRestrictedError{dead: true, reason: acc.Message}
	case !acc.Enabled && acc.AutoResume && data.RestReason == restDailyVideo:
		return &dailyVideoLimitError{reason: acc.Message}
	case acc.Status != accounts.StatusSignedIn:
		return errNotSignedIn
	case data.Quota != nil && data.Quota.weeklyOut() && (!cfg.UseTopup || !data.Quota.topupLeft()):
		return &usageError{}
	case acc.SkipVideoCheck && jobKind == MuseJobVideo:
		return &accountRestrictedError{dead: false, reason: acc.Message}
	}
	return &notSentError{err: errAccountResting}
}

func (p *Provider) waitSignedInForRefresh(ctx context.Context, page Page, timeout time.Duration, forceHome bool) error {
	wentHome := false
	if forceHome {
		if err := page.Home(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		wentHome = true
	}
	return p.waitSignedInWithHome(ctx, page, timeout, wentHome)
}

func (p *Provider) onAccountAutoResumed(rec accounts.Record) {
	if rec.Provider != accounts.ProviderMuse {
		return
	}
	p.clearSessionVerified(rec.ID)
	p.clearAccountBackoff(rec.ID)
	p.clearStateRefreshSeen(rec.ID)
	p.mu.Lock()
	ctx := p.ctx
	p.mu.Unlock()
	if ctx == nil || ctx.Err() != nil || p.accounts == nil || p.cfg == nil {
		return
	}
	cfg := p.cfg.Get().Muse
	if cfg.UseAPI || p.accounts.RouteError(rec) != nil {
		return
	}
	p.scheduleStateAccountRefresh(rec, "auto_resume_24h")
}

func stateRefreshFingerprint(rec accounts.Record) string {
	return fmt.Sprintf("%s|%t|%s|%s", rec.Status, rec.Enabled, rec.Proxy, rec.CookieVersion)
}

func isFreshManualAccount(rec accounts.Record) bool {
	return rec.Source == accounts.SourceBrowser &&
		rec.Status == accounts.StatusUnverified &&
		rec.LastCheckedAt == "" &&
		rec.Identity == "" &&
		len(rec.Cookies) == 0
}

func (p *Provider) clearStateRefreshSeen(accountID string) {
	if accountID == "" {
		return
	}
	p.mu.Lock()
	delete(p.stateRefreshSeen, accountID)
	p.mu.Unlock()
}

func hasPendingWorkForRefresh(jobs []MuseJob) bool {
	for _, job := range jobs {
		if job.Status == MuseJobQueued && !job.Held && !waitsForBoard(job) {
			return true
		}
		if job.Status == MuseJobProcessing && (job.SessionID != "" || (job.Sending && job.PreSendTracked)) {
			return true
		}
	}
	return false
}

func pauseElapsed(rec accounts.Record, now time.Time) bool {
	if rec.PausedUntil == "" {
		return true
	}
	until, err := time.Parse(time.RFC3339, rec.PausedUntil)
	return err != nil || !until.After(now)
}

// triggerNeededAccountRefreshes catches accounts that are enabled but unable
// to take pending work (SignedOut, Unverified, or non-fatal Error) and spawns
// a one-shot background refresh for their current state without periodic timers.
func (p *Provider) triggerNeededAccountRefreshes(jobs []MuseJob, records []accounts.Record, cfg config.MuseConfig, runningAccounts, cleaning map[string]bool) {
	if cfg.UseAPI || p.accounts == nil || !hasPendingWorkForRefresh(jobs) {
		return
	}
	now := p.now()
	for _, rec := range records {
		if !rec.Enabled || cleaning[rec.ID] || runningAccounts[rec.ID] {
			continue
		}
		if rec.Status != accounts.StatusSignedOut && rec.Status != accounts.StatusUnverified && rec.Status != accounts.StatusError {
			continue
		}
		if isFreshManualAccount(rec) {
			continue
		}
		if !pauseElapsed(rec, now) {
			continue
		}
		if p.accounts.InUse(rec.ID) || p.accounts.RouteError(rec) != nil {
			continue
		}
		p.scheduleStateAccountRefresh(rec, "queue_needs_account")
	}
}

func (p *Provider) scheduleStateAccountRefresh(rec accounts.Record, reason string) {
	fp := reason + ":" + stateRefreshFingerprint(rec)
	p.mu.Lock()
	ctx := p.ctx
	if ctx == nil || ctx.Err() != nil {
		p.mu.Unlock()
		return
	}
	if p.stateRefreshSeen == nil {
		p.stateRefreshSeen = make(map[string]string)
	}
	if p.stateRefreshBusy == nil {
		p.stateRefreshBusy = make(map[string]bool)
	}
	if p.stateRefreshBusy[rec.ID] || p.stateRefreshSeen[rec.ID] == fp {
		p.mu.Unlock()
		return
	}
	p.stateRefreshSeen[rec.ID] = fp
	p.stateRefreshBusy[rec.ID] = true
	p.wg.Add(1)
	p.mu.Unlock()

	go func() {
		defer p.wg.Done()
		defer logger.Recover("làm mới tài khoản Muse", func(error) {})
		defer func() {
			p.mu.Lock()
			delete(p.stateRefreshBusy, rec.ID)
			p.mu.Unlock()
			p.closeIdleBrowserIfDone(rec.ID)
			p.trigger()
		}()
		if p.accounts.InUse(rec.ID) {
			return
		}
		lease := acquireAccountLease(p.accounts, rec.ID)
		defer lease.Release()
		timeout := 45 * time.Second
		if p.cfg != nil {
			if ready := time.Duration(p.cfg.Get().Accounts.PageReadySeconds) * time.Second; ready > 0 {
				timeout = ready + 15*time.Second
			}
		}
		refCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		var page Page
		defer func() { closePage(page) }()
		_, recovered, refErr := p.refreshAccountForRun(refCtx, rec.ID, "", &page, lease, reason)
		if !recovered {
			if latest, rErr := p.accounts.Record(rec.ID); rErr == nil && latest.Proxy == rec.Proxy && latest.CookieVersion == rec.CookieVersion {
				afterFP := reason + ":" + stateRefreshFingerprint(latest)
				p.mu.Lock()
				if p.stateRefreshSeen != nil && p.stateRefreshSeen[rec.ID] == fp {
					p.stateRefreshSeen[rec.ID] = afterFP
				}
				p.mu.Unlock()
			}
		}
		logger.Get().Info("muse background account refresh", "account", rec.ID, "reason", reason, "recovered", recovered, "error", refErr)
	}()
}

// isRecoverableSendRejection reports whether a !res.OK send refusal may stem
// from an expired session, dropped gateway, or transient client state rather
// than an explicit policy refusal or quota exhaustion.
func isRecoverableSendRejection(reason string, diag *SendDiagnostics, kind MuseJobKind) bool {
	if isSessionOrGatewayFailureText(reason) || isSessionOrGatewayFailureText(diagText(diag)) {
		return true
	}
	if diag != nil && diag.RequestTimedOut {
		return true
	}
	rKind, _ := classifyRestrictionForKind(kind, reason, diag, "", nil)
	if rKind != restrictNone {
		return true
	}
	// Bare "send_failed" without clear policy/safety details often hides an
	// expired session cookie or broken gateway socket.
	return strings.EqualFold(strings.TrimSpace(reason), "send_failed")
}
