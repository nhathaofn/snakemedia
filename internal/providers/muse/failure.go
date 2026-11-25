package muse

import (
	"context"
	"errors"
	"fmt"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/logger"
)

// Policy for jobs Muse did not run.
const (
	// maxLimitReplies caps how often one job may be handed on after a chat
	// ended without a file on a used-up account: each cost a chat turn.
	maxLimitReplies = 3
	// maxNotSentRetries caps consecutive pre-send / page-not-ready failures
	// before failing the job, preventing an infinite requeue loop across
	// accounts when Muse's page or chat client cannot accept sends.
	maxNotSentRetries = 6
	// notSentWait holds a job back after its account's page was not ready.
	notSentWait = time.Minute
	// recheckWait is the first pause before a sent job is read again after an
	// error; it doubles per re-check up to recheckWaitMax.
	recheckWait    = 15 * time.Second
	recheckWaitMax = 5 * time.Minute
)

func (p *Provider) singleAccountNotSentWait() time.Duration {
	d := notSentWait
	if p.cfg != nil {
		if req := time.Duration(p.cfg.Get().Accounts.RequestTimeoutSeconds) * time.Second; req > 0 && req < d {
			d = req
		}
	}
	return max(d, 5*time.Second)
}

// recheckDelay is the pause before the n-th re-check (n from 1).
func recheckDelay(n int) time.Duration {
	d := recheckWait
	for i := 1; i < n && d < recheckWaitMax; i++ {
		d *= 2
	}
	return min(d, recheckWaitMax)
}

// recordFailure decides what happens to a job after err:
//   - a failure before anything was sent waits a while without spending an
//     attempt (a sent job keeps its chat and waits the same way);
//   - a sent job with a transport error keeps its chat and is checked again,
//     later each time;
//   - Muse's own failure (interrupted, too slow) retries in a new chat;
//   - a declined prompt, a bad input or an unknown send fails at once;
//   - an account that is signed out or out of usage hands the job to
//     another account without spending an attempt, a few times at most.
//
// Each decision is logged (job, account, class, action), never the prompt.
func (p *Provider) recordFailure(jobID, accountID string, err error) {
	job, ok := p.jobs.get(jobID)
	if !ok || job.Status != MuseJobProcessing {
		return
	}
	cfg := p.cfg.Get().Muse
	now := p.now().UTC().Format(time.RFC3339)
	accName := accountID
	if acc, e := p.accounts.Record(accountID); e == nil {
		accName = acc.Label()
	}
	hadSession := job.SessionID != ""
	mustStayOnAccount := hadSession || (job.Sending && job.PreSendTracked)
	log := func(class, action string, detail error) {
		args := []any{"job", jobID, "account", accountID, "class", class, "hadSession", hadSession, "action", action}
		if detail != nil {
			args = append(args, "error", detail)
		}
		logger.Get().Warn("muse job failure", args...)
	}
	// Every change re-checks the status under the journal lock: a cancel that
	// lands while the failure is classified wins.
	failed := func(msg string) {
		p.updateJob(jobID, func(j *MuseJob) {
			if j.Status != MuseJobProcessing {
				return
			}
			j.Status, j.Error, j.Step, j.Sending, j.FinishedAt = MuseJobFailed, msg, "", false, now
		})
	}
	requeue := func(msg string, spend bool) {
		p.updateJob(jobID, func(j *MuseJob) {
			if j.Status != MuseJobProcessing {
				return
			}
			resetForQueue(j)
			j.Error = msg
			if !spend && j.Attempts > 0 {
				j.Attempts--
			}
		})
	}
	// handOn requeues a job Muse answered with a limit instead of making it,
	// until that happened maxLimitReplies times.
	handOn := func(class, msg string) {
		n := 0
		p.updateJob(jobID, func(j *MuseJob) {
			if j.Status == MuseJobProcessing {
				j.LimitReplies++
			}
			n = j.LimitReplies
		})
		if n >= maxLimitReplies {
			log(class, "failed: too many limit replies", nil)
			failed(fmt.Sprintf("Tài khoản hết lượt Muse %d lần cho tác vụ này (mỗi lần tốn một lượt chat). Kiểm tra hạn mức ở Trang chủ → Tài khoản rồi bấm Chạy lại.", n))
			return
		}
		log(class, "requeued to another account", nil)
		requeue(msg, false)
	}
	wait := func(d time.Duration) string {
		until := p.now().Add(d)
		p.holdUntil(jobID, until)
		return until.Local().Format("15:04:05")
	}
	var (
		policy     *policyError
		usage      *usageError
		dailyLimit *dailyVideoLimitError
		restricted *accountRestrictedError
		rejected   *rejectedSend
		input      *inputError
		muse       *museError
		notSent    *notSentError
		incomplete *longIncompleteError
		waitAcc    *waitAccountError
	)
	stayInLongChat := job.Continues > 0 || errors.As(err, new(*stayInChatError))
	switch {
	case errors.As(err, &restricted):
		if restricted.dead {
			p.clearSessionVerified(accountID)
			p.disableRestrictedAccount(accountID, restricted.Error())
		} else {
			p.disableVideoForAccount(accountID, restricted.reason)
		}
		if stayInLongChat {
			log("account restricted", "failed", err)
			failed(restricted.Error())
			return
		}
		log("account restricted", "requeued to another account", err)
		requeue(fmt.Sprintf("%s: %v", accName, restricted), false)
	case errors.As(err, &dailyLimit):
		until := p.pauseDailyVideoLimit(accountID, dailyLimit.reason)
		if stayInLongChat {
			log("daily video limit", "waits for 24h auto-resume", err)
			p.holdUntil(jobID, until)
			p.updateJob(jobID, func(j *MuseJob) {
				if j.Status == MuseJobProcessing {
					j.Step = fmt.Sprintf("Chờ %s tự bật lại lúc %s", accName, until.Local().Format("15:04 02/01"))
					j.Error = dailyLimit.Error()
				}
			})
			return
		}
		handOn("daily video limit", fmt.Sprintf("%s hết hạn tạo video trong ngày (tự tắt tới %s), chờ tài khoản khác", accName, until.Local().Format("15:04 02/01")))
	case errors.Is(err, errNotSignedIn):
		p.clearSessionVerified(accountID)
		p.updateAccount(accountID, func(r *accounts.Record, _ *museData) {
			r.Status, r.Message = accounts.StatusSignedOut, "Phiên Muse đã đăng xuất; hãy mở Chrome để đăng nhập lại."
		})
		if mustStayOnAccount {
			log("signed out", "waits for sign-in", nil)
			p.updateJob(jobID, func(j *MuseJob) {
				if j.Status == MuseJobProcessing {
					j.Step, j.Error = fmt.Sprintf("Chờ tài khoản %s đăng nhập lại để kiểm tra kết quả", accName), err.Error()
				}
			})
			return
		}
		log("signed out", "requeued to another account", nil)
		requeue(fmt.Sprintf("%s chưa đăng nhập Muse", accName), false)
	case errors.Is(err, errTabCapReached):
		log("thread limit reduced", "requeued", nil)
		requeue("Số luồng trên tài khoản vừa giảm; xếp hàng lại.", false)
	case errors.As(err, &notSent):
		p.clearSessionVerified(accountID)
		resting := errors.Is(notSent.err, errAccountResting)
		if !resting {
			p.markRecycleBrowser(accountID)
		}
		if mustStayOnAccount {
			at := wait(p.singleAccountNotSentWait())
			log("page not ready", "re-check later", err)
			p.updateJob(jobID, func(j *MuseJob) {
				if j.Status == MuseJobProcessing {
					j.Step, j.Error = "Kiểm tra lại lúc "+at, err.Error()
				}
			})
			return
		}
		if !resting {
			n := 0
			p.updateJob(jobID, func(j *MuseJob) {
				if j.Status == MuseJobProcessing {
					j.NotSentRetries++
				}
				n = j.NotSentRetries
			})
			if n >= maxNotSentRetries {
				log("page not ready", "failed: too many pre-send retries", err)
				failed(fmt.Sprintf("Không gửi được lệnh tới Muse sau %d lần thử (%v). Hãy kiểm tra tài khoản rồi bấm Chạy lại.", n, notSent.err))
				return
			}
		}
		hasOther := !resting && p.hasOtherUsableAccount(accountID, job.Kind, cfg, p.now())
		if hasOther {
			p.backoffAccount(accountID, accountNotReadyBackoff)
		}
		if resting || hasOther {
			log("page not ready", "requeued to another account", err)
			requeue(fmt.Sprintf("%s: %v", accName, err), false)
			return
		}
		at := wait(p.singleAccountNotSentWait())
		log("page not ready", "requeued without an attempt", err)
		requeue(fmt.Sprintf("%s: %v Thử lại lúc %s.", accName, err, at), false)
	case errors.As(err, &policy), errors.As(err, &input), errors.As(err, &incomplete), errors.As(err, new(*tooShortError)):
		log(failureClass(err), "failed", nil)
		failed(err.Error())
	case errors.As(err, &waitAcc):
		// The chat holds clips already paid for: wait for this account.
		log("usage", "waits for the account's rest", nil)
		p.holdUntil(jobID, waitAcc.until)
		p.updateJob(jobID, func(j *MuseJob) {
			if j.Status == MuseJobProcessing {
				j.Step, j.Error = "Chờ "+accName+" có lượt lại để làm tiếp", err.Error()
			}
		})
	case errors.Is(err, errSendUnknown):
		// Marked, so a retry warns that Muse may already have made it.
		log(failureClass(err), "failed", nil)
		p.updateJob(jobID, func(j *MuseJob) {
			if j.Status != MuseJobProcessing {
				return
			}
			j.Status, j.Error, j.Step, j.Sending, j.FinishedAt = MuseJobFailed, err.Error(), "", false, now
			j.PreSendTracked, j.PreSendSessions = false, nil
			j.SendUnknown = true
		})
	case errors.As(err, &usage):
		p.restAccount(accountID, cfg)
		handOn("usage", fmt.Sprintf("%s hết lượt Muse, chờ tài khoản khác", accName))
	case errors.As(err, &rejected) && p.accountPaused(accountID, cfg):
		handOn("usage", fmt.Sprintf("%s hết lượt Muse, chờ tài khoản khác", accName))
	case mustStayOnAccount && !errors.As(err, &muse):
		// Muse already accepted this job (or an interrupted tracked send must
		// still be reconciled on this account): read it back again rather than
		// fail or hand off to another account, which could pay a second time.
		if job.Rechecks+1 >= cfg.MaxRechecks {
			if !hadSession {
				log("send unknown", "failed after re-checks", err)
				p.updateJob(jobID, func(j *MuseJob) {
					if j.Status != MuseJobProcessing {
						return
					}
					j.Status, j.Error, j.Step, j.Sending, j.FinishedAt = MuseJobFailed, errSendUnknown.Error(), "", false, now
					j.PreSendTracked, j.PreSendSessions = false, nil
					j.SendUnknown = true
				})
				return
			}
			log("read error", "failed after re-checks", err)
			failed(fmt.Sprintf("Muse đã nhận lệnh nhưng app không đọc được kết quả sau %d lần (%v). Mở chat trên Muse để lấy kết quả trước khi chạy lại, vì chạy lại sẽ tốn thêm 1 lượt.", cfg.MaxRechecks, err))
			return
		}
		at := wait(recheckDelay(job.Rechecks + 1))
		log("read error", "re-check later", err)
		p.updateJob(jobID, func(j *MuseJob) {
			if j.Status != MuseJobProcessing {
				return
			}
			j.Rechecks++
			j.Step = "Kiểm tra lại lúc " + at
			j.Error = fmt.Sprintf("Lỗi lần %d, đang kiểm tra lại phiên: %v", j.Rechecks, err)
		})
	case job.Attempts >= cfg.MaxAttempts:
		log(failureClass(err), "failed after attempts", nil)
		failed(err.Error())
	default:
		log(failureClass(err), "retry in a new chat", nil)
		requeue(fmt.Sprintf("Lỗi lần %d, đang thử lại: %v", job.Attempts, err), true)
	}
}

// failureClass names an error for the log without Muse's reply (which may
// repeat the prompt).
func failureClass(err error) string {
	var (
		policy     *policyError
		dailyLimit *dailyVideoLimitError
		restricted *accountRestrictedError
		rejected   *rejectedSend
		input      *inputError
		muse       *museError
	)
	switch {
	case errors.As(err, &restricted):
		return "account restricted"
	case errors.As(err, &dailyLimit):
		return "daily video limit"
	case errors.As(err, &policy):
		return "policy"
	case errors.As(err, &input):
		return "input"
	case errors.Is(err, errSendUnknown):
		return "send unknown"
	case errors.As(err, &rejected):
		return "rejected"
	case errors.As(err, &muse):
		return "no result"
	case errors.As(err, new(*longIncompleteError)):
		return "long video incomplete"
	}
	return "error"
}

// Why an account rests, kept so a later account check only ends the rest it
// can judge: a rest for the weekly quota ends once the quota has room again,
// a rest for any other limit Muse named runs its time.
const (
	restQuota      = "quota"
	restLimit      = "limit"
	restDailyVideo = "daily_video"
)

// disableRestrictedAccount marks a dead or login-banned account as StatusError
// and turns Enabled off so it stops taking jobs immediately.
func (p *Provider) disableRestrictedAccount(accountID, msg string) {
	p.updateAccount(accountID, func(a *accounts.Record, _ *museData) {
		a.Status = accounts.StatusError
		a.Enabled = false
		a.AutoResume = false
		a.PausedUntil = ""
		a.Message = msg
	})
}

// disableVideoForAccount turns video probing and video scheduling off when Muse
// disables video generation on an account that still works for images.
func (p *Provider) disableVideoForAccount(accountID, reason string) {
	p.updateAccount(accountID, func(a *accounts.Record, _ *museData) {
		a.Status = accounts.StatusSignedIn
		a.SkipVideoCheck = true
		msg := "Không tạo được video (đã tắt kiểm tra video, chỉ tạo ảnh)"
		if reason != "" {
			msg += ": " + truncate(reason, 120)
		}
		a.Message = msg
	})
}

// pauseDailyVideoLimit disables the account for 24h from now when Muse reports
// the daily video generation limit, flagging AutoResume so it comes back
// automatically once the 24h window passes.
func (p *Provider) pauseDailyVideoLimit(accountID, reason string) time.Time {
	until := p.now().Add(24 * time.Hour)
	p.updateAccount(accountID, func(a *accounts.Record, d *museData) {
		a.Enabled = false
		a.AutoResume = true
		a.PausedUntil = until.UTC().Format(time.RFC3339)
		msg := "Hết hạn tạo video trong ngày (24h); tự tắt tới " + until.Local().Format("15:04 02/01")
		if reason != "" {
			msg += " (" + truncate(reason, 120) + ")"
		}
		a.Message = msg
		d.RestReason = restDailyVideo
	})
	return until
}

// restAccount pauses an account Muse said is out of usage: until its weekly
// usage resets when that is what ran out, otherwise for UsagePauseMinutes.
func (p *Provider) restAccount(accountID string, cfg config.MuseConfig) {
	until := p.now().Add(time.Duration(cfg.UsagePauseMinutes) * time.Minute)
	reason := restLimit
	if acc, err := p.accounts.Record(accountID); err == nil {
		if q := museView(acc).Quota; q.weeklyOut() {
			reason = restQuota
			if reset := q.resetsAt(); reset.After(p.now()) {
				until = reset
			}
		}
	}
	p.updateAccount(accountID, func(a *accounts.Record, d *museData) {
		a.PausedUntil = until.UTC().Format(time.RFC3339)
		a.Message = "Muse báo hết lượt; tạm nghỉ tới " + until.Local().Format("15:04 02/01")
		d.RestReason = reason
	})
}

// accountPaused refreshes nothing: it reads the stored quota and pause.
func (p *Provider) accountPaused(accountID string, cfg config.MuseConfig) bool {
	acc, err := p.accounts.Record(accountID)
	return err == nil && !pausedUntil(museView(acc), cfg, p.now()).IsZero()
}

const (
	// topupQuotaRefreshTTL avoids re-reading subscription quota before every
	// job when the weekly bucket is exhausted but top-up tokens are active.
	topupQuotaRefreshTTL = 15 * time.Minute
	// quotaCheckCooldown prevents back-to-back or concurrent quota RPCs on the
	// same account when multiple tabs inspect a stale quota at once.
	quotaCheckCooldown = 2 * time.Minute
)

func (p *Provider) quotaRefreshTimeout() time.Duration {
	if p.cfg != nil {
		if req := p.cfg.Get().Accounts.RequestTimeoutSeconds; req > 0 {
			return time.Duration(req) * time.Second
		}
	}
	return 15 * time.Second
}

func quotaCheckedTime(q *MuseQuota) (time.Time, bool) {
	if q == nil || q.CheckedAt == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, q.CheckedAt)
	return t, err == nil
}

func (p *Provider) detachedQuotaPage(accountID string, page Page) Page {
	if cp, ok := page.(*chromePage); ok && cp.port > 0 {
		return &chromePage{
			port:        cp.port,
			policy:      cp.policy,
			callTimeout: cp.callTimeout,
			sendTimeout: cp.sendTimeout,
			nav:         p.navLock(accountID),
			routeMu:     p.routeLock(accountID),
		}
	}
	return page
}

// checkStaleQuota reads the quota again before a send when the stored one
// says the weekly usage is gone (its rest ran out, so it is stale). Still used
// up, the account rests again and the job goes back without an attempt.
// Accounts with active top-up tokens refresh in the background after TTL so
// pre-send never stalls waiting on Muse's subscription endpoint.
func (p *Provider) checkStaleQuota(ctx context.Context, accountID string, page Page, cfg config.MuseConfig) error {
	acc, err := p.accounts.Record(accountID)
	if err != nil {
		return nil
	}
	q := museView(acc).Quota
	if !q.weeklyOut() {
		return nil
	}
	if cfg.UseTopup && q.topupLeft() {
		if p.accountPaused(accountID, cfg) {
			return &notSentError{err: errAccountResting}
		}
		checked, hasChecked := quotaCheckedTime(q)
		if (!hasChecked || p.now().Sub(checked) >= topupQuotaRefreshTTL) && p.claimQuotaAttempt(accountID, quotaCheckCooldown) {
			p.refreshQuotaLater(accountID, p.detachedQuotaPage(accountID, page))
		}
		return nil
	}
	if p.slots != nil && p.slots.quota != nil {
		release, err := p.quotaSlot(ctx, accountID)
		if err != nil {
			return err
		}
		defer release()
		acc, err = p.accounts.Record(accountID)
		if err != nil {
			return nil
		}
		q = museView(acc).Quota
		if !q.weeklyOut() {
			return nil
		}
		if cfg.UseTopup && q.topupLeft() {
			if p.accountPaused(accountID, cfg) {
				return &notSentError{err: errAccountResting}
			}
			return nil
		}
	}
	checked, hasChecked := quotaCheckedTime(q)
	freshEnough := hasChecked && p.now().Sub(checked) < quotaCheckCooldown
	if !freshEnough && p.claimQuotaAttempt(accountID, quotaCheckCooldown) {
		p.refreshQuota(ctx, accountID, page)
	}
	if p.accountPaused(accountID, cfg) {
		return &notSentError{err: errAccountResting}
	}
	return nil
}

// errAccountResting means the account's quota is still used up.
var errAccountResting = errors.New("Tài khoản vẫn hết lượt Muse.")

// refreshQuota reads the account's quota from its page and stores it.
func (p *Provider) refreshQuota(ctx context.Context, accountID string, page Page) {
	started := p.now()
	p.recordQuotaAttempt(accountID)
	ctx, cancel := context.WithTimeout(ctx, p.quotaRefreshTimeout())
	defer cancel()
	q, err := page.Quota(ctx)
	if err != nil || q == nil {
		if !errors.Is(err, context.Canceled) {
			logger.Get().Warn("cannot refresh muse quota", "account", accountID, "error", err, "empty", q == nil)
		}
		return
	}
	q.CheckedAt = started.UTC().Format(time.RFC3339Nano)
	var before float64 = -1
	accepted := false
	p.updateAccount(accountID, func(_ *accounts.Record, d *museData) {
		if checked, ok := quotaCheckedTime(d.Quota); ok && checked.After(started) {
			return
		}
		if d.Quota != nil {
			before = d.Quota.PercentUsed
		}
		d.Quota = q
		accepted = true
	})
	if accepted {
		logger.Get().Info("muse quota read", "account", accountID, "percentBefore", before, "percent", q.PercentUsed, "topup", q.TopupBalance)
	}
}
