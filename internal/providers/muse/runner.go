package muse

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/logger"
	"github.com/google/uuid"
)

// kindWord names a kind in the English message.
var kindWord = map[MuseJobKind]string{MuseJobImage: "image", MuseJobVideo: "video"}

// kindMime is the result MIME prefix each kind collects.
var kindMime = map[MuseJobKind]string{MuseJobImage: "image/", MuseJobVideo: "video/"}

// kindName words a kind in messages to the user.
var kindName = map[MuseJobKind]string{MuseJobImage: "ảnh", MuseJobVideo: "video"}

// rejectedSend is Muse definitely refusing a send: nothing was started.
type rejectedSend struct{ reason string }

func (e *rejectedSend) Error() string {
	reason := e.reason
	if reason == "" {
		reason = "không rõ lý do"
	}
	return "Muse không nhận lệnh: " + reason
}

// inputError is a problem with the job itself; retrying cannot fix it.
type inputError struct{ msg string }

func (e *inputError) Error() string { return e.msg }

// sendGap is the random pause between two sends on one account.
func sendGap(cfg config.MuseConfig) time.Duration {
	lo, hi := cfg.SendGapMinSeconds, cfg.SendGapMaxSeconds
	return time.Duration(lo)*time.Second + time.Duration(rand.Float64()*float64(hi-lo)*float64(time.Second))
}

// runOne runs one assigned job and records how it ended. Cancelling the job or
// stopping the app leaves a sent job processing, so it can be checked again.
func (p *Provider) runOne(ctx context.Context, jobID, accountID string) {
	defer p.finishRun(jobID)
	defer logger.Recover("tác vụ Muse", func(err error) { p.recordFailure(jobID, accountID, panicFailure(p.jobs, jobID, err)) })
	err := p.execute(ctx, jobID, accountID)
	if err == nil || ctx.Err() != nil {
		return
	}
	p.recordFailure(jobID, accountID, err)
}

// panicFailure is the failure recorded for a run that panicked. A panic while
// the message was on its way leaves Muse's answer unknown, like a lost send
// (ADR 0012): the job is failed with errSendUnknown, never sent again on its
// own. Otherwise the usual rules apply (a job with a chat is re-checked).
func panicFailure(jobs *jobStore, jobID string, err error) error {
	if job, ok := jobs.get(jobID); ok && job.Sending {
		return errSendUnknown
	}
	return err
}

func (p *Provider) execute(ctx context.Context, jobID, accountID string) error {
	job, ok := p.jobs.get(jobID)
	if !ok || job.finished() {
		return nil
	}
	if accountID == "api" || p.cfg.Get().Muse.UseAPI {
		return p.executeAPI(ctx, jobID)
	}
	acc, err := p.accounts.Record(accountID)
	if err != nil {
		return err
	}
	// The account's browser stays as it is (no sign-in window, no restart on
	// a new proxy, no removal) while this job works outside an explicit recycle.
	lease, err := acquireAccountLease(ctx, p.accounts, accountID)
	if err != nil {
		return err
	}
	defer lease.Release()
	cfg := p.cfg.Get().Muse
	if job.Status == MuseJobQueued {
		// Another concurrent thread may have just disabled this account (e.g.
		// video-disabled / 24h limit / signed-out / dead) after pump planned it.
		if !usableForJob(museView(acc), job.Kind, cfg, p.now()) {
			return nil
		}
		started := false
		job, err = p.updateJob(jobID, func(j *MuseJob) {
			// The user may have stopped or cancelled the job since it was planned.
			if j.Status != MuseJobQueued || j.Held || waitsForBoard(*j) {
				return
			}
			started = true
			resetForQueue(j)
			j.NextCheckAt = ""
			j.Status, j.AccountID, j.Step = MuseJobProcessing, accountID, "Chuẩn bị phiên"
			j.StartedAt = p.now().UTC().Format(time.RFC3339)
			j.Attempts++
		})
		if err != nil {
			return err
		}
		if !started {
			return nil
		}
	}
	if !job.Kind.valid() {
		return &inputError{msg: fmt.Sprintf("Muse không còn hỗ trợ loại tác vụ %q.", job.Kind)}
	}
	var page Page
	defer func() { closePage(page) }()

	recoveredOnce := false
	recoverAccount := func(cause error, reason string, forResume bool) (bool, error) {
		if recoveredOnce || ctx.Err() != nil {
			return false, cause
		}
		p.updateJob(job.ID, func(j *MuseJob) {
			if j.Status == MuseJobProcessing {
				j.Step = "Làm mới tài khoản & luồng"
			}
		})
		recoveredOnce = true
		var (
			freshAcc accounts.Record
			ok       bool
			refErr   error
		)
		if forResume {
			freshAcc, ok, refErr = p.refreshAccountForResume(ctx, acc.ID, job.Kind, &page, lease, reason)
		} else {
			freshAcc, ok, refErr = p.refreshAccountForRun(ctx, acc.ID, job.Kind, &page, lease, reason)
		}
		if freshAcc.ID != "" {
			acc = freshAcc
		}
		if ok {
			return true, nil
		}
		if refErr != nil {
			return false, refErr
		}
		return false, cause
	}
	recoverPreSend := func(cause error, reason string) (bool, error) {
		return recoverAccount(cause, reason, false)
	}
	recoverResume := func(cause error, reason string) (bool, error) {
		return recoverAccount(cause, reason, true)
	}

	if job.SessionID == "" && !job.Sending {
		for {
			if page == nil {
				page, err = p.pageFor(ctx, acc)
				if err != nil {
					if ok, rErr := recoverPreSend(&notSentError{err: err}, "open_page"); ok {
						continue
					} else {
						return rErr
					}
				}
			}
			if err := p.ensureSignedIn(ctx, job.ID, acc, page); err != nil {
				if errors.Is(err, errTabCapReached) {
					return err
				}
				// On Chrome browsers, waitSignedIn already tried Home once; recycle
				// Chrome & refresh account once before reporting signed-out/error.
				if cp, isChrome := page.(*chromePage); isChrome && cp.port > 0 {
					if ok, rErr := recoverPreSend(err, "ensure_signed_in"); ok {
						continue
					} else {
						return rErr
					}
				}
				return err
			}
			if err := p.assignTab(ctx, job.ID, acc.ID, page, cfg); err != nil {
				if errors.Is(err, errTabCapReached) {
					return err
				}
				if ok, rErr := recoverPreSend(&notSentError{err: err}, "assign_tab"); ok {
					continue
				} else {
					return rErr
				}
			}
			p.updateJob(job.ID, func(j *MuseJob) {
				if j.Status == MuseJobProcessing {
					j.Step = "Chuẩn bị gửi"
				}
			})
			if err := p.checkStaleQuota(ctx, accountID, page, cfg); err != nil {
				if !errors.Is(err, errAccountResting) {
					if ok, rErr := recoverPreSend(err, "check_quota"); ok {
						continue
					} else {
						return rErr
					}
				}
				return err
			}
			if len(job.References) > 0 || len(job.Frames) > 0 {
				p.updateJob(job.ID, func(j *MuseJob) {
					if j.Status == MuseJobProcessing {
						j.Step = "Chuẩn bị đính kèm"
					}
				})
			}
			atts, err := p.attachments(ctx, job, acc, page)
			if err != nil {
				var input *inputError
				if !errors.As(err, &input) && ctx.Err() == nil {
					if ok, rErr := recoverPreSend(&notSentError{err: err}, "attachments"); ok {
						continue
					} else {
						return rErr
					}
				}
				return err
			}
			var nextErr error
			job, page, nextErr = p.send(ctx, job, acc, lease, page, cfg, buildMessage(job), atts, !recoveredOnce)
			if nextErr != nil {
				var retryErr *recoverableSendError
				if errors.As(nextErr, &retryErr) {
					if ok, rErr := recoverPreSend(retryErr.inner, retryErr.reason); ok {
						continue
					} else {
						return rErr
					}
				}
				var notSent *notSentError
				if errors.As(nextErr, &notSent) && !errors.Is(notSent.err, errAccountResting) {
					var reconciledNotSent *reconcileNotSentError
					if errors.As(nextErr, &reconciledNotSent) {
						// Reconcile already reset the tab and confirmed no chat was
						// created; mark Chrome for recycle if needed and requeue cleanly.
						p.markRecycleBrowser(acc.ID)
						return nextErr
					}
					if ok, rErr := recoverPreSend(nextErr, "send_not_sent"); ok {
						continue
					} else {
						return rErr
					}
				}
				return nextErr
			}
			break
		}
	} else {
		page, err = p.pageFor(ctx, acc)
		if err != nil {
			return &notSentError{err: err}
		}
		if err := p.ensureSignedIn(ctx, job.ID, acc, page); err != nil {
			if ok, rErr := recoverResume(err, "resume_signed_in"); ok {
				// Re-check signed in and bound cleanly after refresh.
				if page == nil {
					return &notSentError{err: errPageNotReady}
				}
			} else {
				return rErr
			}
		}
		if err := p.assignTab(ctx, job.ID, acc.ID, page, cfg); err != nil {
			if errors.Is(err, errTabCapReached) {
				return err
			}
			if ok, rErr := recoverResume(&notSentError{err: err}, "resume_assign_tab"); !ok {
				return rErr
			}
		}
		if job.SessionID == "" && job.Sending && job.PreSendTracked {
			if job, page, err = p.reconcileInterruptedSend(ctx, job, acc, lease, page); err != nil {
				return err
			}
			if job.SessionID == "" {
				// Confirmed not created before restart; retry once on refreshed state.
				return &notSentError{err: errors.New("Tác vụ chưa kịp gửi tới Muse trước khi app đóng")}
			}
		}
	}

	for {
		ended, replies, hasReplies, err := p.pollWithReplies(ctx, job, lease, &page, cfg)
		if err != nil {
			return err
		}
		err = p.collectWithReplies(ctx, job, accountID, lease, &page, ended, cfg, replies, hasReplies)
		var more *continueNeeded
		if !errors.As(err, &more) {
			return err
		}
		if job, err = p.sendContinueWithLease(ctx, job, acc, lease, &page, more.seen); err != nil {
			return err
		}
	}
}

// errPageNotReady means the account's tab answered but Muse's chat client
// never showed up: the account is signed in (or cannot be told), so nothing
// is marked signed out and the job waits for the page.
var errPageNotReady = errors.New("Trang Muse của tài khoản chưa sẵn sàng (chưa có khung chat).")

// assignTab binds a chromePage to the account's shared muse.ai tab slot,
// opening a tab only when none exists yet. Non-job callers (refresh, reopen)
// receive a unique generated holder ID so tabPool slot release and hasOtherBusy
// never collide on empty IDs.
func (p *Provider) assignTab(ctx context.Context, jobID, accountID string, page Page, cfg config.MuseConfig) error {
	cp, isChrome := page.(*chromePage)
	if !isChrome || cp.port <= 0 || p.tabs == nil {
		return nil
	}
	cp.mu.Lock()
	already := cp.targetID != ""
	cp.mu.Unlock()
	if already {
		return nil
	}
	holderID := jobID
	if holderID == "" {
		holderID = "op-" + uuid.NewString()
	}
	targetID, err := p.tabs.acquire(ctx, accountID, holderID, cp.port, cfg.ThreadsPerAccount, cp.policy)
	if err != nil {
		return err
	}
	port := cp.port
	policy := cp.policy
	cp.bindTab(targetID, holderID, p.tabNavLock(accountID, targetID), p.tabRouteLock(accountID, targetID), func(dirty bool) {
		releaseCtx := context.Background()
		p.mu.Lock()
		if p.ctx != nil {
			releaseCtx = p.ctx
		}
		p.mu.Unlock()
		p.tabs.release(releaseCtx, accountID, port, targetID, holderID, dirty, func() int {
			return p.cfg.Get().Muse.ThreadsPerAccount
		}, policy)
	})
	return nil
}

// ensureSignedIn verifies login on page unless the same Chrome process was
// confirmed signed in earlier. Concurrent threads on one fresh Chrome share one
// check per launch via loginSlot.
func (p *Provider) ensureSignedIn(ctx context.Context, jobID string, acc accounts.Record, page Page) error {
	cp, isChrome := page.(*chromePage)
	if isChrome && cp.port > 0 {
		if acc.Status == accounts.StatusSignedIn && p.sessionVerified(acc.ID, cp.port) {
			return nil
		}
		releaseLogin, err := p.loginSlot(ctx, acc.ID)
		if err != nil {
			return err
		}
		defer releaseLogin()
		if acc.Status == accounts.StatusSignedIn && p.sessionVerified(acc.ID, cp.port) {
			return nil
		}
		// If another thread already owns a tab on this Chrome, bind the tab
		// before waitSignedIn so its nav/route locks coordinate cleanly.
		if p.tabs != nil && p.tabs.hasBusy(acc.ID, cp.port) {
			if err := p.assignTab(ctx, jobID, acc.ID, page, p.cfg.Get().Muse); err != nil {
				if errors.Is(err, errTabCapReached) {
					return err
				}
				return &notSentError{err: err}
			}
		}
	}
	p.updateJob(jobID, func(j *MuseJob) {
		if j.Status == MuseJobProcessing {
			j.Step = "Kiểm tra đăng nhập"
		}
	})
	if err := p.waitSignedIn(ctx, page, time.Duration(p.cfg.Get().Accounts.PageReadySeconds)*time.Second); err != nil {
		p.clearSessionVerified(acc.ID)
		return err
	}
	if isChrome && cp.port > 0 {
		p.markSessionVerified(acc.ID, cp.port)
	}
	return nil
}

// waitSignedIn polls the page until it reports a signed-in Muse session. A
// page still loading reports "not signed in" or fails, so it keeps polling. A
// signed-in tab on another route (no chat client) is sent home once. Only a
// session Muse refuses (an HTTP answer other than 200) means signed out; a
// page that never got ready is notSent, so the job waits without cost.
func (p *Provider) waitSignedIn(ctx context.Context, page Page, timeout time.Duration) error {
	return p.waitSignedInWithHome(ctx, page, timeout, false)
}

func (p *Provider) waitSignedInWithHome(ctx context.Context, page Page, timeout time.Duration, wentHome bool) error {
	deadline := p.now().Add(timeout)
	checkedAway := false
	unauthCount := 0
	noChatCount := 0
	gatewayCount := 0
	oauthWaitCount := 0
	var last LoginState
	var lastErr error
	for {
		state, err := page.Login(ctx)
		if err == nil && state.LoggedIn && state.Gateway == "" {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		last, lastErr = state, err
		if errors.Is(err, errTabMissing) && !checkedAway {
			if cp, ok := page.(*chromePage); ok && cp.port > 0 {
				if info, aErr := inspectAwayTab(ctx, cp.port, cp.policy, false); aErr == nil && info.URL != "" {
					if kind, detail := classifyAwayTab(info); kind == restrictDeadAccount {
						return &accountRestrictedError{dead: true, reason: detail}
					}
					if isOAuthOrLoginBounce(info) && oauthWaitCount < 2 {
						oauthWaitCount++
						if !p.now().Before(deadline) {
							return errNotSignedIn
						}
						if err := p.sleep(ctx, time.Duration(p.cfg.Get().Accounts.PollIntervalMS)*time.Millisecond); err != nil {
							return err
						}
						continue
					}
				}
				checkedAway = true
				if info, aErr := inspectAwayTab(ctx, cp.port, cp.policy, true); aErr == nil && info.URL != "" {
					if kind, detail := classifyAwayTab(info); kind == restrictDeadAccount {
						return &accountRestrictedError{dead: true, reason: detail}
					} else if detail != "" {
						// Navigated to Muse and immediately bounced to an external login page.
						return errNotSignedIn
					}
				}
			}
		}
		if err == nil {
			if reason := state.blockedReason(); reason != "" {
				return &accountRestrictedError{dead: true, reason: reason}
			}
			if !state.LoggedIn && state.HTTP == 200 && !state.Chat && state.Notice != "" {
				if kind, detail := classifyText("", "", state.Notice, nil); kind == restrictDeadAccount || kind == restrictVideoDisabled {
					return &accountRestrictedError{dead: kind == restrictDeadAccount, reason: detail}
				}
			}
			if state.HTTP != 0 && state.HTTP != 200 {
				if unauthCount++; unauthCount >= 2 {
					return errNotSignedIn
				}
				// Try reloading Home once before concluding signed out: an idle
				// Chrome tab can return 401 on /api/session until navigation
				// refreshes the session cookie or runs silent SSO.
				if !wentHome {
					wentHome = true
					if hErr := page.Home(ctx); hErr != nil && ctx.Err() != nil {
						return ctx.Err()
					}
				}
			} else {
				unauthCount = 0
			}
		} else {
			unauthCount = 0
		}
		if err == nil && state.LoggedIn && state.Gateway != "" {
			if gatewayCount++; gatewayCount == 1 && !wentHome {
				wentHome = true
				if hErr := page.Home(ctx); hErr != nil && ctx.Err() != nil {
					return ctx.Err()
				}
			}
		} else {
			gatewayCount = 0
		}
		if err == nil && state.HTTP == 200 && !state.Chat {
			if !wentHome {
				wentHome = true
				if err := page.Home(ctx); err != nil && ctx.Err() != nil {
					return ctx.Err()
				}
			} else {
				noChatCount++
				if noChatCount >= 2 && isLoginPath(state.Path) {
					return errNotSignedIn
				}
			}
		} else {
			noChatCount = 0
		}
		if !p.now().Before(deadline) {
			break
		}
		if err := p.sleep(ctx, time.Duration(p.cfg.Get().Accounts.PollIntervalMS)*time.Millisecond); err != nil {
			return err
		}
	}
	switch {
	case lastErr != nil:
		return &notSentError{err: fmt.Errorf("Không đọc được trang Muse của tài khoản: %w", lastErr)}
	case last.HTTP != 200 && last.HTTP != 0:
		return errNotSignedIn
	case last.Gateway != "":
		return &notSentError{err: fmt.Errorf("Kết nối Muse bị gián đoạn (%s)", last.Gateway)}
	}
	return &notSentError{err: errPageNotReady}
}

func isLoginPath(path string) bool {
	lower := strings.ToLower(strings.TrimSpace(path))
	for _, seg := range []string{"/login", "/signin", "/sign-in", "/auth"} {
		if strings.Contains(lower, seg) {
			return true
		}
	}
	return false
}
