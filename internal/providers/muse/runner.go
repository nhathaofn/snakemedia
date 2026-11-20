package muse

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/events"
	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/media"
)

// kindRequest is how a message asks Muse's agent for each kind. Muse's media
// tools take only a prompt and images (no ratio, duration or count
// parameters), so everything else stays in the user's prompt, unchanged.
var kindRequest = map[MuseJobKind]string{
	MuseJobImage: "Task: Create a new image.",
	MuseJobVideo: "Task: Create a new video.",
}

// referenceLines say how a job's n attached images without roles are used.
// Images are references the video tool redraws from (it has no
// image-to-video mode). The prompt may give an image any role (a character,
// the setting, style only); without one, a single image is the subject and
// several images are separate elements, never merged. Images are numbered in the order attached (their
// file names, image_N) for the prompt to name them.
func referenceLines(kind MuseJobKind, n int) []string {
	what := kindWord[kind]
	if n == 1 {
		return []string{fmt.Sprintf("Image 1: use it as the prompt says. If the prompt gives it no role, it shows the subject of this %s: keep its look exactly, but take background, framing and pose from the prompt.", what)}
	}
	return []string{fmt.Sprintf("Images: %s, in the order attached, each show a separate element (a character, object, product or setting); never merge them. Use each as the prompt says. If the prompt gives one no role, include its element and keep its look exactly, but take backgrounds, framing and poses from the prompt.", imageNames(n))}
}

// imageNames lists n numbered images: "image 1, image 2 and image 3", or a
// range for many.
func imageNames(n int) string {
	if n > 4 {
		return fmt.Sprintf("image 1 to image %d", n)
	}
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("image %d", i+1)
	}
	return strings.Join(names[:n-1], ", ") + " and " + names[n-1]
}

// kindWord names a kind in the English message.
var kindWord = map[MuseJobKind]string{MuseJobImage: "image", MuseJobVideo: "video"}

// kindMime is the result MIME prefix each kind collects.
var kindMime = map[MuseJobKind]string{MuseJobImage: "image/", MuseJobVideo: "video/"}

// kindName words a kind in messages to the user.
var kindName = map[MuseJobKind]string{MuseJobImage: "ảnh", MuseJobVideo: "video"}

// buildMessage is the chat message sent for a job, read by Muse's agent:
// the task and request rule, how to use the attached images (when there are
// any), the ratio and duration asked (formatLines), the plan rule and, for a
// video, the one-clip or long-video guide, then the user's prompt unchanged.
func buildMessage(job MuseJob) string {
	lines := []string{kindRequest[job.Kind]}
	if job.Kind == MuseJobImage && job.BoardFrames > 0 {
		lines = append([]string{"Task: Create the storyboard images of a video."}, boardImageLines(job)...)
	}
	lines = append(lines, requestRules)
	if job.SourceVideo != "" {
		lines = append(lines, sampleLines(job)...)
	}
	first := 1
	if len(job.Frames) > 0 {
		lines = append(lines, boardVideoLines(job)...)
		first += len(job.Frames)
	}
	if n := len(job.References); n > 0 {
		switch {
		case len(job.RefInfo) == n:
			lines = append(lines, roleLines(job.Kind, job.RefInfo, first)...)
		case first > 1:
			lines = append(lines, roleLines(job.Kind, make([]MuseRefInfo, n), first)...)
		default:
			lines = append(lines, referenceLines(job.Kind, n)...)
		}
		lines = append(lines, videoReferenceLines(job)...)
	}
	lines = append(lines, formatLines(job)...)
	switch {
	case isLong(job):
		lines = append(lines, longVideoLines(job)...)
	case job.Kind == MuseJobVideo:
		lines = append(lines, shortVideoLines(job)...)
	default:
		lines = append(lines, planRule[job.Kind])
	}
	return strings.Join(lines, "\n") + "\nprompt:\n" + job.Prompt
}

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
	lease := acquireAccountLease(p.accounts, accountID)
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
			// The user may have cancelled the job since it was planned.
			if j.Status != MuseJobQueued {
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
	recoverPreSend := func(cause error, reason string) (bool, error) {
		if recoveredOnce || ctx.Err() != nil {
			return false, cause
		}
		p.updateJob(job.ID, func(j *MuseJob) {
			if j.Status == MuseJobProcessing {
				j.Step = "Làm mới tài khoản & luồng"
			}
		})
		recoveredOnce = true
		freshAcc, ok, refErr := p.refreshAccountForRun(ctx, acc.ID, job.Kind, &page, lease, reason)
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
			if len(job.References) > 0 || len(job.Frames) > 0 || job.SourceVideo != "" {
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
			if ok, rErr := recoverPreSend(err, "resume_signed_in"); ok {
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
			if ok, rErr := recoverPreSend(&notSentError{err: err}, "resume_assign_tab"); !ok {
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
		if job, err = p.sendContinue(ctx, job, acc, page, more.seen); err != nil {
			return err
		}
	}
}

// sendContinue posts continueMessage into a long video's chat. Continues and
// RepliesSeen are written first, so after a lost answer or a restart the job
// only waits for a reply newer than the ones already read; it is never sent
// again in a new chat. SentAt moves to now: the time limit counts from here.
func (p *Provider) sendContinue(ctx context.Context, job MuseJob, acc accounts.Record, page Page, seen int) (MuseJob, error) {
	release, err := p.sendSlot(ctx, acc.ID)
	if err != nil {
		return job, err
	}
	defer release()
	prevSeen, prevSentAt := job.RepliesSeen, job.SentAt
	kept := false
	job, err = p.updateJob(job.ID, func(j *MuseJob) {
		if j.Status != MuseJobProcessing {
			return
		}
		kept = true
		j.Continues++
		j.RepliesSeen = seen
		j.SentAt = p.now().UTC().Format(time.RFC3339)
		j.Step = fmt.Sprintf("Nhắc Muse làm tiếp (lần %d/%d)", j.Continues, continueLimit(*j))
	})
	if err != nil {
		return job, err
	}
	if !kept {
		return job, context.Canceled
	}
	sendCtx, stopSend := p.sendContext(ctx)
	defer stopSend()
	res, err := page.SendTo(sendCtx, job.SessionID, continueMessage)
	if err != nil {
		var pre *preSendError
		if sendCtx.Err() == nil && errors.As(err, &pre) {
			job, _ = p.updateJob(job.ID, func(j *MuseJob) {
				j.Continues--
				j.RepliesSeen, j.SentAt = prevSeen, prevSentAt
			})
			return job, &notSentError{err: pre.err}
		}
		if sendCtx.Err() != nil {
			return job, sendCtx.Err()
		}
		// Muse may have taken it: the chat is read again later and only a
		// newer reply counts.
		logger.Get().Warn("muse continue outcome unknown", "job", job.ID, "error", err)
		return job, fmt.Errorf("Không rõ Muse đã nhận lời nhắc làm tiếp chưa: %w", err)
	}
	p.clearRecycleBrowser(acc.ID)
	p.updateAccount(acc.ID, func(_ *accounts.Record, d *museData) { d.LastSentAt = p.now().UTC().Format(time.RFC3339Nano) })
	if !res.OK {
		// Definitely not taken: the next try reads the same reply again.
		job, _ = p.updateJob(job.ID, func(j *MuseJob) {
			j.Continues--
			j.RepliesSeen, j.SentAt = prevSeen, prevSentAt
		})
		if strings.HasPrefix(res.Reason, "not_ready") {
			return job, &notSentError{err: errPageNotReady}
		}
		// Never a rejectedSend: that may hand the job to another account, in a
		// new chat that would pay for the clips again. The chat is kept.
		if until := p.restingUntil(sendCtx, acc.ID, page); !until.IsZero() {
			return job, &waitAccountError{until: until}
		}
		return job, fmt.Errorf("Muse không nhận lời nhắc làm tiếp: %s", res.Reason)
	}
	job, err = p.updateJob(job.ID, func(j *MuseJob) {
		if j.Status == MuseJobProcessing {
			j.Step = "Muse đang làm tiếp"
		}
	})
	return job, err
}

type recoverableSendError struct {
	reason string
	inner  error
}

func (e *recoverableSendError) Error() string { return e.inner.Error() }
func (e *recoverableSendError) Unwrap() error { return e.inner }

type reconcileNotSentError struct{ err error }

func (e *reconcileNotSentError) Error() string { return e.err.Error() }
func (e *reconcileNotSentError) Unwrap() error { return e.err }

// send waits for the account's send slot and its send gap, then posts the
// message in a new chat. Pre-send CDP errors return notSentError; lost answers
// are reconciled against sessions.list and chat.history before falling back to
// errSendUnknown.
//
// One send per account runs at a time, gap included: two jobs on one account
// (ThreadsPerAccount > 1) would otherwise read the same last send time and
// send together.
func (p *Provider) send(ctx context.Context, job MuseJob, acc accounts.Record, lease *accountLease, page Page, cfg config.MuseConfig, text string, atts []Attachment, canRecover bool) (MuseJob, Page, error) {
	release, err := p.sendSlot(ctx, acc.ID)
	if err != nil {
		return job, page, err
	}
	defer release()
	if fresh, err := p.accounts.Record(acc.ID); err == nil {
		acc = fresh
	}
	// If another thread disabled video/account or paused this account while we
	// waited on sendSlot/stagger, abort before sleeping or sending anything.
	if job.SessionID == "" && !job.Sending && !usableForJob(museView(acc), job.Kind, cfg, p.now()) {
		return job, page, &notSentError{err: errAccountResting}
	}
	if last, err := time.Parse(time.RFC3339Nano, museView(acc).LastSentAt); err == nil {
		if wait := last.Add(p.gap(cfg)).Sub(p.now()); wait > 0 {
			p.updateJob(job.ID, func(j *MuseJob) {
				j.Step = fmt.Sprintf("Nghỉ ~%ds rồi mới gửi (tránh gửi dồn)", int(wait.Seconds()+0.5))
			})
			if err := p.sleep(ctx, wait); err != nil {
				return job, page, err
			}
			if fresh, err := p.accounts.Record(acc.ID); err == nil {
				acc = fresh
			}
			if job.SessionID == "" && !job.Sending && !usableForJob(museView(acc), job.Kind, cfg, p.now()) {
				return job, page, &notSentError{err: errAccountResting}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return job, page, err
	}
	tracked, beforeIDs, snapErr := p.snapshotSessions(ctx, acc, page)
	if !tracked && snapErr != nil && canRecover && ctx.Err() == nil {
		if freshAcc, ok, refErr := p.refreshAccountForRun(ctx, acc.ID, job.Kind, &page, lease, "pre_send_snapshot"); ok {
			acc = freshAcc
			tracked, beforeIDs, _ = p.snapshotSessions(ctx, acc, page)
		} else if refErr != nil {
			return job, page, refErr
		}
	}
	job, err = p.updateJob(job.ID, func(j *MuseJob) {
		j.Sending, j.PreSendTracked, j.PreSendSessions, j.Step = true, tracked, beforeIDs, "Gửi lệnh"
	})
	if err != nil {
		return job, page, err
	}
	// Cancelling the job must not cut a send off halfway: Muse may already
	// have accepted it and would bill a generation nobody follows. The send
	// runs to its own timeout and is cancelled on Muse afterwards; only
	// stopping the app interrupts it (resumed and reconciled on restart when
	// tracked, otherwise failed as unknown).
	sendCtx, stopSend := p.sendContext(ctx)
	defer stopSend()
	res, err := page.Send(sendCtx, text, atts)
	if err != nil {
		var pre *preSendError
		if sendCtx.Err() == nil && errors.As(err, &pre) {
			p.clearSendingState(job.ID)
			p.clearSessionVerified(acc.ID)
			if ctx.Err() != nil {
				return job, page, ctx.Err()
			}
			return job, page, &notSentError{err: pre.err}
		}
		if sendCtx.Err() != nil {
			return job, page, sendCtx.Err()
		}
		if tracked {
			sid, nextPage, outcome, recErr := p.reconcileAfterSend(sendCtx, job, acc, lease, page, text, beforeIDs, true, true)
			page = nextPage
			if ctx.Err() != nil {
				if outcome == reconcileFound && sid != "" {
					p.recordCancelledSession(job.ID, sid)
					if page != nil {
						_ = page.Cancel(sendCtx, sid)
					}
				} else {
					p.clearSendingState(job.ID)
				}
				return job, page, context.Canceled
			}
			switch outcome {
			case reconcileFound:
				logger.Get().Info("muse send recovered from chat history", "job", job.ID, "session", sid, "sendError", err)
				res = SendResult{OK: true, SessionID: sid}
				err = nil
			case reconcileNotSent:
				logger.Get().Info("muse send confirmed not created; requeueing", "job", job.ID, "sendError", err)
				p.clearSendingState(job.ID)
				return job, page, &notSentError{err: &reconcileNotSentError{err: fmt.Errorf("Không nhận được phản hồi lúc gửi và Muse chưa tạo phiên mới: %w", err)}}
			default:
				p.markRecycleBrowser(acc.ID)
				p.clearSessionVerified(acc.ID)
				// Refresh account state (without resending!) so signed-out/dead/limited
				// accounts are updated accurately and don't keep failing subsequent jobs.
				_, _, _ = p.refreshAccountForRun(sendCtx, acc.ID, job.Kind, &page, lease, "reconcile_unknown")
				logger.Get().Warn("muse send outcome unknown", "job", job.ID, "error", err, "reconcileError", recErr)
				return job, page, errSendUnknown
			}
		} else {
			p.markRecycleBrowser(acc.ID)
			p.clearSessionVerified(acc.ID)
			_, _, _ = p.refreshAccountForRun(sendCtx, acc.ID, job.Kind, &page, lease, "untracked_send_error")
			logger.Get().Warn("muse send outcome unknown", "job", job.ID, "error", err)
			return job, page, errSendUnknown
		}
	}
	if err == nil && !res.OK && strings.HasPrefix(res.Reason, "not_ready") {
		// The page never found Muse's chat client, even after going home:
		// nothing left the app, so no attempt and no send gap are spent.
		p.clearSendingState(job.ID)
		p.clearSessionVerified(acc.ID)
		return job, page, &notSentError{err: errPageNotReady}
	}
	p.clearRecycleBrowser(acc.ID)
	if res.OK {
		p.updateAccount(acc.ID, func(_ *accounts.Record, d *museData) { d.LastSentAt = p.now().UTC().Format(time.RFC3339Nano) })
	} else {
		p.updateAccount(acc.ID, func(_ *accounts.Record, d *museData) { d.LastSentAt = p.now().UTC().Format(time.RFC3339Nano) })
	}
	if res.OK && res.SessionID == "" {
		if tracked {
			sid, nextPage, outcome, recErr := p.reconcileAfterSend(sendCtx, job, acc, lease, page, text, beforeIDs, false, false)
			page = nextPage
			if outcome == reconcileFound {
				logger.Get().Info("muse accepted send matched to chat", "job", job.ID, "session", sid)
				res.SessionID = sid
			} else {
				logger.Get().Warn("muse accepted a send without a session", "job", job.ID, "reconcileError", recErr)
				return job, page, errSendUnknown
			}
		} else {
			// Muse took the message but named no chat: it may be generating, and
			// without its chat the job can neither be read back nor safely re-sent.
			logger.Get().Warn("muse accepted a send without a session", "job", job.ID)
			return job, page, errSendUnknown
		}
	}
	if !res.OK {
		p.clearSendingState(job.ID)
		// A refusal is often a used-up quota, session expiry, or account restriction.
		p.refreshQuota(sendCtx, acc.ID, page)
		var freshQuota *MuseQuota
		if latestAcc, qErr := p.accounts.Record(acc.ID); qErr == nil {
			freshQuota = museView(latestAcc).Quota
		}
		kind, detail := classifyRestrictionForKind(job.Kind, res.Reason, res.Diagnostics, "", freshQuota)
		if kind == restrictNone && canRecover && isRecoverableSendRejection(res.Reason, res.Diagnostics, job.Kind) {
			return job, page, &recoverableSendError{reason: "send_refused", inner: &rejectedSend{reason: res.Reason}}
		}
		if kind != restrictNone {
			if kind == restrictVideoDisabled {
				p.disableVideoForAccount(acc.ID, detail)
			}
			_, _, refErr := p.refreshAccountForRun(sendCtx, acc.ID, job.Kind, &page, lease, "send_restriction")
			if refErr != nil {
				var refRestricted *accountRestrictedError
				switch {
				case errors.Is(refErr, errNotSignedIn):
					return job, page, refErr
				case errors.As(refErr, &refRestricted) && refRestricted.dead:
					return job, page, refErr
				case errors.As(refErr, new(*dailyVideoLimitError)) && kind != restrictDeadAccount:
					if kind == restrictDailyVideo24h {
						return job, page, &dailyVideoLimitError{reason: detail}
					}
					return job, page, refErr
				}
			}
		}
		switch kind {
		case restrictDeadAccount:
			return job, page, &accountRestrictedError{dead: true, reason: detail}
		case restrictVideoDisabled:
			return job, page, &accountRestrictedError{dead: false, reason: detail}
		case restrictDailyVideo24h:
			if !p.accountPaused(acc.ID, cfg) {
				return job, page, &dailyVideoLimitError{reason: detail}
			}
		}
		return job, page, &rejectedSend{reason: res.Reason}
	}
	kept := false
	job, err = p.updateJob(job.ID, func(j *MuseJob) {
		// A job queued again meanwhile starts over without this chat. A
		// cancelled one keeps it, so a bulk retry knows Muse took it; either
		// way the chat is cancelled on Muse below.
		if j.Status == MuseJobQueued {
			return
		}
		j.SessionID, j.Sending, j.PreSendTracked, j.PreSendSessions = res.SessionID, false, false, nil
		j.SentAt = p.now().UTC().Format(time.RFC3339)
		if j.Status == MuseJobProcessing {
			kept = true
			j.Step = "Muse đang làm"
		}
	})
	if ctx.Err() != nil || (err == nil && !kept) {
		// Cancelled while sending: stop the generation Muse just started.
		_ = page.Cancel(sendCtx, res.SessionID)
		return job, page, context.Canceled
	}
	return job, page, err
}

// clearSendingState removes in-flight send tracking once a send definitely did
// not create a session or was rejected.
func (p *Provider) clearSendingState(jobID string) {
	_, _ = p.updateJob(jobID, func(j *MuseJob) {
		j.Sending, j.PreSendTracked, j.PreSendSessions = false, false, nil
	})
}

// recordCancelledSession attaches a recovered session ID to a cancelled job
// so it stays accounted for without returning to the queue.
func (p *Provider) recordCancelledSession(jobID, sessionID string) {
	_, _ = p.updateJob(jobID, func(j *MuseJob) {
		if j.Status == MuseJobQueued {
			return
		}
		j.SessionID = sessionID
		j.Sending, j.PreSendTracked, j.PreSendSessions = false, false, nil
	})
}

type reconcileOutcome int

const (
	reconcileUnknown reconcileOutcome = iota
	reconcileFound
	reconcileNotSent
)

const (
	reconcileChecks     = 3
	reconcileStepWait   = 1500 * time.Millisecond
	reconcileMinTimeout = 45 * time.Second
)

// reconcileInterruptedSend resolves a job whose app process stopped while
// Sending was true using the PreSendSessions snapshot saved before sending.
func (p *Provider) reconcileInterruptedSend(ctx context.Context, job MuseJob, acc accounts.Record, lease *accountLease, page Page) (MuseJob, Page, error) {
	p.updateJob(job.ID, func(j *MuseJob) {
		if j.Status == MuseJobProcessing {
			j.Step = "Kiểm tra lại lệnh gửi trước khi tắt app"
		}
	})
	sid, nextPage, outcome, recErr := p.reconcileAfterSend(ctx, job, acc, lease, page, buildMessage(job), job.PreSendSessions, false, true)
	page = nextPage
	switch outcome {
	case reconcileFound:
		p.clearRecycleBrowser(acc.ID)
		p.updateAccount(acc.ID, func(_ *accounts.Record, d *museData) { d.LastSentAt = p.now().UTC().Format(time.RFC3339Nano) })
		job, err := p.updateJob(job.ID, func(j *MuseJob) {
			if j.Status == MuseJobQueued {
				return
			}
			j.SessionID, j.Sending, j.PreSendTracked, j.PreSendSessions = sid, false, false, nil
			if j.SentAt == "" {
				j.SentAt = p.now().UTC().Format(time.RFC3339)
			}
			if j.Status == MuseJobProcessing {
				j.Step = "Muse đang làm"
			}
		})
		return job, page, err
	case reconcileNotSent:
		p.clearSendingState(job.ID)
		return job, page, &notSentError{err: errors.New("Tác vụ chưa kịp gửi tới Muse trước khi app đóng")}
	default:
		if ctx.Err() != nil {
			return job, page, ctx.Err()
		}
		if recErr != nil {
			if errors.Is(recErr, errNotSignedIn) {
				return job, page, errNotSignedIn
			}
			return job, page, fmt.Errorf("Không đối soát được lệnh đã gửi với Muse: %w", recErr)
		}
		return job, page, errSendUnknown
	}
}

// reconcileAfterSend checks sessions.list and chat.history for a session
// created after beforeIDs whose user message matches sentText. When resetTab
// is true it reloads or reopens the tab first so a hung send promise cannot
// fire after reconcileNotSent is returned.
func (p *Provider) reconcileAfterSend(ctx context.Context, job MuseJob, acc accounts.Record, lease *accountLease, page Page, sentText string, beforeIDs []string, resetTab, allowNotSent bool) (string, Page, reconcileOutcome, error) {
	timeout := reconcileMinTimeout
	if ready := time.Duration(p.cfg.Get().Accounts.PageReadySeconds)*time.Second + 20*time.Second; ready > timeout {
		timeout = ready
	}
	recCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	p.updateJob(job.ID, func(j *MuseJob) {
		if j.Status == MuseJobProcessing {
			j.Step = "Đối soát lịch sử chat trên Muse"
		}
	})

	if resetTab {
		if err := p.resetTabForReconcile(recCtx, acc, &page); err != nil {
			if errors.Is(err, errNotSignedIn) {
				if _, ok, refErr := p.refreshAccountForRun(recCtx, acc.ID, job.Kind, &page, lease, "reconcile_login"); !ok {
					if refErr != nil {
						return "", page, reconcileUnknown, refErr
					}
					return "", page, reconcileUnknown, err
				}
			} else {
				return "", page, reconcileUnknown, err
			}
		}
	}

	beforeSet := make(map[string]bool, len(beforeIDs))
	for _, id := range beforeIDs {
		if id != "" {
			beforeSet[id] = true
		}
	}

	var lastErr error
	reopened := false
	for attempt := range reconcileChecks {
		if attempt > 0 {
			if err := p.sleep(recCtx, reconcileStepWait); err != nil {
				return "", page, reconcileUnknown, err
			}
		}
		tracker, ok := page.(sessionTracker)
		if !ok {
			return "", page, reconcileUnknown, errors.New("trang không hỗ trợ đọc danh sách phiên")
		}
		current, err := tracker.Sessions(recCtx, true)
		if err != nil {
			lastErr = err
			if !reopened && recCtx.Err() == nil {
				reopened = true
				_, okRef, refErr := p.refreshAccountForRun(recCtx, acc.ID, job.Kind, &page, lease, "reconcile_sessions")
				if okRef {
					continue
				}
				if p.reopenSignedIn(recCtx, acc, &page) {
					continue
				}
				if refErr != nil {
					lastErr = refErr
				}
			}
			return "", page, reconcileUnknown, lastErr
		}
		lastErr = nil
		newIDs := unseenSessionIDs(current, beforeSet)
		if len(newIDs) == 0 {
			if allowNotSent && attempt > 0 {
				return "", page, reconcileNotSent, nil
			}
			continue
		}
		matched, inconclusive, matchErr := p.matchSentSession(recCtx, tracker, newIDs, sentText, job.Prompt)
		if matchErr != nil {
			lastErr = matchErr
		}
		if len(matched) == 1 {
			return matched[0], page, reconcileFound, nil
		}
		if len(matched) > 1 {
			return "", page, reconcileUnknown, errors.New("có nhiều phiên chat mới trùng nội dung")
		}
		if !inconclusive && allowNotSent {
			if attempt > 0 {
				return "", page, reconcileNotSent, nil
			}
		}
	}
	if lastErr == nil {
		lastErr = errors.New("không xác định được phiên mới trên Muse")
	}
	return "", page, reconcileUnknown, lastErr
}

const preSendSnapshotTimeout = 10 * time.Second

// snapshotSessions makes a quick best-effort read of the account's current
// session IDs before sending. If the session list is slow or unavailable, it
// degrades safely while surfacing the error so callers can refresh if helpful.
func (p *Provider) snapshotSessions(ctx context.Context, acc accounts.Record, page Page) (tracked bool, ids []string, snapErr error) {
	tracker, ok := page.(sessionTracker)
	if !ok {
		return false, nil, nil
	}
	snapTimeout := preSendSnapshotTimeout
	if p.cfg != nil {
		if req := time.Duration(p.cfg.Get().Accounts.RequestTimeoutSeconds) * time.Second; req > 0 && req < snapTimeout {
			snapTimeout = req
		}
	}
	snapCtx, cancel := context.WithTimeout(ctx, snapTimeout)
	defer cancel()
	before, err := tracker.Sessions(snapCtx, true)
	if err != nil {
		if ctx.Err() == nil {
			logger.Get().Warn("muse pre-send session snapshot skipped", "account", acc.ID, "error", err)
		}
		return false, nil, err
	}
	return true, sessionIDs(before), nil
}

// resetTabForReconcile reloads the tab (or reopens Chrome when the tab is gone)
// and waits for Muse to become signed in again.
func (p *Provider) resetTabForReconcile(ctx context.Context, acc accounts.Record, page *Page) error {
	if *page != nil {
		if err := (*page).Home(ctx); err == nil {
			err = p.waitSignedIn(ctx, *page, time.Duration(p.cfg.Get().Accounts.PageReadySeconds)*time.Second)
			if err == nil || errors.Is(err, errNotSignedIn) {
				return err
			}
		}
	}
	if _, err := p.reopenSignedInErr(ctx, acc, page); err != nil {
		return err
	}
	return nil
}

// reopenSignedIn closes the current page, opens a fresh tab/browser via pageFor
// and waits for Muse to report signed in.
func (p *Provider) reopenSignedIn(ctx context.Context, acc accounts.Record, page *Page) bool {
	ok, _ := p.reopenSignedInErr(ctx, acc, page)
	return ok
}

func (p *Provider) reopenSignedInErr(ctx context.Context, acc accounts.Record, page *Page) (bool, error) {
	p.clearSessionVerified(acc.ID)
	if *page != nil {
		closePage(*page)
		*page = nil
	}
	next, err := p.pageFor(ctx, acc)
	if err != nil {
		return false, err
	}
	if err := p.assignTab(ctx, "", acc.ID, next, p.cfg.Get().Muse); err != nil {
		closePage(next)
		return false, err
	}
	if err := p.waitSignedIn(ctx, next, time.Duration(p.cfg.Get().Accounts.PageReadySeconds)*time.Second); err != nil {
		closePage(next)
		return false, err
	}
	if cp, ok := next.(*chromePage); ok && cp.port > 0 {
		p.markSessionVerified(acc.ID, cp.port)
	}
	*page = next
	return true, nil
}

// matchSentSession inspects new sessions' user messages to find the chat
// created by sentText/prompt, flagging inconclusive if any new session cannot
// be read cleanly.
func (p *Provider) matchSentSession(ctx context.Context, tracker sessionTracker, newIDs []string, sentText, prompt string) (matched []string, inconclusive bool, lastErr error) {
	for _, sid := range newIDs {
		msgs, err := tracker.UserMessages(ctx, sid)
		if err != nil {
			inconclusive = true
			lastErr = err
			continue
		}
		hasReadableText := false
		matchedThis := false
		for _, m := range msgs {
			if !m.HasText {
				continue
			}
			hasReadableText = true
			if promptMatches(sentText, prompt, m.Text) {
				matchedThis = true
				break
			}
		}
		if matchedThis {
			matched = append(matched, sid)
		} else if !hasReadableText {
			inconclusive = true
		}
	}
	return matched, inconclusive, lastErr
}

func sessionIDs(list []SessionInfo) []string {
	seen := make(map[string]bool, len(list))
	out := make([]string, 0, len(list))
	for _, s := range list {
		if s.ID == "" || seen[s.ID] {
			continue
		}
		seen[s.ID] = true
		out = append(out, s.ID)
	}
	return out
}

func unseenSessionIDs(current []SessionInfo, before map[string]bool) []string {
	seen := make(map[string]bool)
	var out []string
	for _, s := range current {
		if s.ID == "" || before[s.ID] || seen[s.ID] {
			continue
		}
		seen[s.ID] = true
		out = append(out, s.ID)
	}
	return out
}

func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// promptMatches checks whether a user chat event matches the sent message or
// prompt, tolerating whitespace normalization and truncated previews.
func promptMatches(sentText, prompt, eventText string) bool {
	ev := normalizeSpace(eventText)
	if ev == "" {
		return false
	}
	sent := normalizeSpace(sentText)
	if sent != "" && ev == sent {
		return true
	}
	p := normalizeSpace(prompt)
	if p != "" && ev == p {
		return true
	}
	if len([]rune(p)) >= 20 && strings.Contains(ev, p) {
		return true
	}
	if sent != "" {
		for _, suffix := range []string{"...", "…"} {
			if strings.HasSuffix(ev, suffix) {
				prefix := strings.TrimSpace(strings.TrimSuffix(ev, suffix))
				if len([]rune(prefix)) >= 40 && strings.HasPrefix(sent, prefix) {
					return true
				}
			}
		}
	}
	return false
}

// sendContext keeps a send alive when only its job is cancelled, but not when
// the app stops.
func (p *Provider) sendContext(ctx context.Context) (context.Context, func()) {
	sendCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	stop := context.AfterFunc(p.lifetime(), cancel)
	return sendCtx, func() {
		stop()
		cancel()
	}
}

// Session states muse.ai reports in sessions.list (read from muse.ai): a
// session is "running" while Muse works (its status panel shows the activity)
// and "completed" once the run has finished, whether or not a file came out.
// "interrupted" is a run stopped before it finished. Any other state, or a
// session not listed, keeps the job waiting; each is logged once.
const (
	sessionRunning     = "running"
	sessionCompleted   = "completed"
	sessionInterrupted = "interrupted"
)

// statusLog remembers which session states were logged.
var statusLog = onceSet{seen: map[string]bool{}}

// noteStatus logs a session state the first time it is seen, with how the
// runner treats it, and reports whether it logged.
func noteStatus(status string) bool {
	if !statusLog.first(status) {
		return false
	}
	treated := "unknown"
	switch status {
	case sessionRunning:
		treated = "running"
	case sessionCompleted, sessionInterrupted:
		treated = "ended"
	}
	logger.Get().Info("muse session status seen", "status", status, "treated", treated)
	return true
}

// pollDelay scales the wait interval for long-running Muse jobs and adds ±25%
// jitter so concurrent tabs do not query session status in lockstep.
func pollDelay(base, elapsed time.Duration) time.Duration {
	if base <= 0 {
		base = time.Second
	}
	step := base
	switch {
	case elapsed >= 3*time.Minute:
		step = min(max(base*3, 12*time.Second), 15*time.Second)
	case elapsed >= time.Minute:
		step = min(max(base*2, 7*time.Second), 12*time.Second)
	}
	quarter := step / 4
	if quarter > 0 {
		step = step - quarter + rand.N(quarter*2+1)
	}
	if step < time.Second {
		return time.Second
	}
	return step
}

const imageEarlyPollMax = 2 * time.Second

func pollDelayForKind(kind MuseJobKind, base, elapsed time.Duration) time.Duration {
	if kind == MuseJobImage && elapsed < 30*time.Second && base > imageEarlyPollMax {
		base = imageEarlyPollMax
	}
	return pollDelay(base, elapsed)
}

// poll waits until the session has ended and returns how it ended
// (sessionCompleted or sessionInterrupted). While its state is unknown (not
// listed, or a state Muse has not been seen to use) the chat itself is read:
// a file of the job's kind means it is done. The time limit counts from the
// send; at the limit the chat is read once more before Muse is asked to stop.
func (p *Provider) poll(ctx context.Context, job MuseJob, page Page, cfg config.MuseConfig) (string, error) {
	ended, _, _, err := p.pollWithReplies(ctx, job, nil, &page, cfg)
	return ended, err
}

func (p *Provider) pollWithReplies(ctx context.Context, job MuseJob, lease *accountLease, page *Page, cfg config.MuseConfig) (string, []AssistantEvent, bool, error) {
	limit := time.Duration(cfg.VideoTimeoutMinutes) * time.Minute
	if job.Kind == MuseJobImage {
		limit = time.Duration(cfg.ImageTimeoutMinutes) * time.Minute
	}
	sent, err := time.Parse(time.RFC3339, job.SentAt)
	if err != nil {
		if sent, err = time.Parse(time.RFC3339, job.StartedAt); err != nil {
			sent = p.now()
		}
	}
	pollStart := p.now()
	refreshedInPoll := false
	for errorsInRow := 0; ; {
		if err := p.sleep(ctx, pollDelayForKind(job.Kind, time.Duration(cfg.PollIntervalMS)*time.Millisecond, p.now().Sub(pollStart))); err != nil {
			return "", nil, false, err
		}
		status, err := (*page).Status(ctx, job.SessionID)
		if err != nil {
			if ctx.Err() != nil {
				return "", nil, false, ctx.Err()
			}
			errorsInRow++
			if errorsInRow == 2 && !refreshedInPoll && job.AccountID != "" {
				refreshedInPoll = true
				_, okRef, refErr := p.refreshAccountForRun(ctx, job.AccountID, job.Kind, page, lease, "poll_status")
				if !okRef && refErr != nil {
					if errors.Is(refErr, errNotSignedIn) || errors.As(refErr, new(*accountRestrictedError)) {
						return "", nil, false, refErr
					}
				} else if okRef {
					errorsInRow = 0
					continue
				}
			}
			if errorsInRow >= cfg.PollErrorLimit {
				return "", nil, false, fmt.Errorf("Không đọc được trạng thái phiên Muse: %w", err)
			}
			status = sessionRunning
		} else {
			errorsInRow = 0
			noteStatus(status)
		}
		// The chat is read at most once a tick, by whichever check needs it first,
		// and reused by collect if that read concluded the poll.
		var (
			cachedReplies []AssistantEvent
			hasReplies    bool
		)
		rawChat := readChatOnce(ctx, job, *page)
		chat := func() ([]AssistantEvent, error) {
			events, rErr := rawChat()
			if rErr == nil {
				cachedReplies = events
				hasReplies = true
			}
			return events, rErr
		}
		if status == sessionCompleted || status == sessionInterrupted {
			// Right after a continue the chat may still show the last turn's
			// end: only a newer reply ends this one.
			if job.RepliesSeen == 0 || newReply(job, chat) {
				return status, cachedReplies, hasReplies, nil
			}
			status = sessionRunning
		}
		overdue := p.now().Sub(sent) > limit
		if overdue && job.RepliesSeen > 0 && !newReply(job, chat) {
			// The continue got no answer in time. Sending the video again in a
			// new chat would pay for its clips twice: it fails instead.
			return "", nil, false, &longIncompleteError{continues: job.Continues, detail: "Muse không trả lời lời nhắc làm tiếp"}
		}
		if (status != sessionRunning || overdue) && chatHasResult(job, chat) {
			return sessionCompleted, cachedReplies, hasReplies, nil
		}
		if overdue {
			if err := (*page).Cancel(ctx, job.SessionID); err != nil {
				if ctx.Err() != nil {
					return "", nil, false, ctx.Err()
				}
				// Muse may still be making the file: check the chat again
				// later rather than send a second generation.
				return "", nil, false, fmt.Errorf("Quá %d phút Muse chưa làm xong và không dừng được phiên: %w", int(limit.Minutes()), err)
			}
			return "", nil, false, &museError{msg: fmt.Sprintf("Quá %d phút Muse chưa làm xong.", int(limit.Minutes()))}
		}
	}
}

// chatHasResult reports whether the chat already holds a file of the job's
// kind. A long video's chat holds its clips long before the joined file, so
// for it only a status line saying it is done counts.
func chatHasResult(job MuseJob, chat func() ([]AssistantEvent, error)) bool {
	events, err := chat()
	if err != nil {
		return false
	}
	if isLong(job) {
		return len(events) > job.RepliesSeen && parseLongStatus(repliesAfter(events, job.RepliesSeen)).State == statusDone
	}
	wanted, _ := wantedMedia(events, job.Kind)
	return len(wanted) > 0
}

// newReply reports whether the chat holds an answer newer than the ones read
// before the job's last continue.
func newReply(job MuseJob, chat func() ([]AssistantEvent, error)) bool {
	events, err := chat()
	return err == nil && len(events) > job.RepliesSeen
}

// readChatOnce returns a reader of the job's chat that asks the page on its
// first use only and hands every later use the same answer.
func readChatOnce(ctx context.Context, job MuseJob, page Page) func() ([]AssistantEvent, error) {
	var (
		read   bool
		events []AssistantEvent
		err    error
	)
	return func() ([]AssistantEvent, error) {
		if !read {
			read = true
			events, err = page.Result(ctx, job.SessionID)
		}
		return events, err
	}
}

// wantedMedia picks the result files of kind from a chat, and reports whether
// Muse listed files of that kind it could not hand over (missing).
func wantedMedia(events []AssistantEvent, kind MuseJobKind) (wanted []ResultMedia, missing bool) {
	for _, e := range events {
		for _, m := range append(append([]ResultMedia(nil), e.Videos...), e.Images...) {
			if m.Path == "" || !strings.HasPrefix(m.Mime, kindMime[kind]) {
				continue
			}
			if m.Missing {
				missing = true
				continue
			}
			wanted = append(wanted, m)
		}
	}
	return wanted, missing
}

// collect reads the finished chat and downloads every file of the job's kind.
// ended is how the session ended (see poll).
func (p *Provider) collect(ctx context.Context, job MuseJob, accountID string, page Page, ended string, cfg config.MuseConfig) error {
	return p.collectWithReplies(ctx, job, accountID, nil, &page, ended, cfg, nil, false)
}

func (p *Provider) collectWithReplies(ctx context.Context, job MuseJob, accountID string, lease *accountLease, page *Page, ended string, cfg config.MuseConfig, replies []AssistantEvent, hasReplies bool) error {
	if !hasReplies {
		var err error
		replies, err = (*page).Result(ctx, job.SessionID)
		if err != nil && ctx.Err() == nil {
			if _, okRef, refErr := p.refreshAccountForRun(ctx, accountID, job.Kind, page, lease, "collect_result"); okRef {
				replies, err = (*page).Result(ctx, job.SessionID)
			} else if refErr != nil && (errors.Is(refErr, errNotSignedIn) || errors.As(refErr, new(*accountRestrictedError))) {
				return refErr
			}
		}
		if err != nil {
			return err
		}
	}
	wanted, missing := wantedMedia(replies, job.Kind)
	var long longStatus
	var err error
	if isLong(job) {
		if long, err = p.longOutcome(ctx, job, accountID, *page, replies); err != nil {
			return err
		}
	}
	// A video job asks for one file. A long video's chat holds every clip
	// before the joined one, and a short one may hold a cut Muse replaced
	// ("here's the exact-cut version, replacing the 5.08s cut"): the last
	// video Muse sent is the result either way.
	if job.Kind == MuseJobVideo && len(wanted) > 1 {
		wanted = wanted[len(wanted)-1:]
	}
	if len(wanted) == 0 && missing {
		// Muse made the file but cannot hand it over right now: read the chat
		// again later instead of paying for a new one.
		return fmt.Errorf("Muse đã tạo %s nhưng chưa lấy được file", kindName[job.Kind])
	}
	if len(wanted) == 0 && isLong(job) && long.State == "" {
		// Muse ends its turn while it still joins the clips ("joining now, I
		// will send the file"): read the chat again later, never a refusal.
		return fmt.Errorf("Muse chưa giao video %d giây", job.Duration)
	}
	if len(wanted) == 0 {
		return p.noResult(ctx, job, accountID, lease, page, ended, replies, cfg)
	}
	p.updateJob(job.ID, func(j *MuseJob) { j.Step = "Tải file" })
	files, err := p.downloadWanted(ctx, job, accountID, *page, wanted)
	if err != nil {
		return err
	}
	// Preserve the files even when validation cannot confirm a complete result.
	job, err = p.updateJob(job.ID, func(j *MuseJob) {
		if j.Status == MuseJobProcessing {
			j.Files = files
		}
	})
	if err != nil {
		return err
	}
	if job.Status != MuseJobProcessing {
		return nil
	}
	note := ""
	incomplete := false
	if isLong(job) {
		path, err := p.media.Path(files[0])
		if err != nil {
			return err
		}
		meta, err := media.ProbeVideo(ctx, path, p.ffprobe)
		if err != nil {
			return fmt.Errorf("Chưa xác minh được thời lượng video Muse: %w", err)
		}
		note = formatNote(job, meta.Width, meta.Height, meta.DurationSeconds)
		incomplete = math.Abs(meta.DurationSeconds-float64(job.Duration)) > durationTolerance
		if incomplete && long.State == "" {
			return fmt.Errorf("Muse chưa giao đúng video %d giây (file hiện tại %.1f giây)", job.Duration, meta.DurationSeconds)
		}
	} else {
		note = p.measureNote(ctx, job, files)
	}
	job, err = p.saveCollected(job, files)
	if err != nil {
		return err
	}
	if job.ID == "" || job.Status != MuseJobProcessing {
		return nil
	}
	saved := job.SavedFiles
	settled := false
	done, err := p.jobs.update(job.ID, func(j *MuseJob) {
		if j.Status != MuseJobProcessing {
			return // cancelled meanwhile: keep the user's decision
		}
		j.Status, j.Files, j.Error, j.SavedFiles, j.FormatNote = MuseJobSucceeded, files, "", saved, note
		j.ChangeNote = long.Changed
		if incomplete {
			j.Status = MuseJobFailed
			j.Error = "Video chưa đúng thời lượng. " + note + " File đã nhận được giữ lại; app không tự gửi tạo lại."
		}
		j.Step = fmt.Sprintf("Đã tải %d file", len(files))
		j.FinishedAt = p.now().UTC().Format(time.RFC3339)
		settled = true
	})
	if err != nil {
		logger.Get().Warn("cannot save muse job", "job", job.ID, "error", err)
		return err
	} else {
		// The library is filled before the frontend hears of the result,
		// since that event is when it reloads the library.
		if settled {
			p.addToLibrary(done)
		}
		p.emit(events.EventMuseJobUpdated, done)
		if settled && done.BoardFrames > 0 && done.Kind == MuseJobImage {
			p.releaseAfter(done)
		}
	}
	p.refreshQuotaLater(accountID, *page)
	return nil
}

// longOutcome reads how Muse said a long video's latest turn ended:
//   - TOO SHORT: a script longer than the length, nothing made (fails);
//   - REFUSED: Muse declined the prompt (policyError, never retried as is);
//   - INCOMPLETE: clips are missing. An account out of usage waits with its
//     chat (waitAccountError); otherwise a continue is sent into the chat
//     (continueNeeded) until continueLimit, then the job fails;
//   - DONE or no status line: the files are collected as usual.
func (p *Provider) longOutcome(ctx context.Context, job MuseJob, accountID string, page Page, replies []AssistantEvent) (longStatus, error) {
	st := parseLongStatus(repliesAfter(replies, job.RepliesSeen))
	switch st.State {
	case statusTooShort:
		return st, &tooShortError{need: st.Detail}
	case statusRefused:
		reason := st.Detail
		if reason == "" {
			reason = replyText(replies[min(job.RepliesSeen, len(replies)):])
		}
		return st, &policyError{reply: reason}
	case statusIncomplete:
		if until := p.restingUntil(ctx, accountID, page); !until.IsZero() {
			return st, &waitAccountError{until: until}
		}
		if job.Continues >= continueLimit(job) {
			return st, &longIncompleteError{continues: job.Continues, detail: st.Detail}
		}
		return st, &continueNeeded{seen: len(replies)}
	}
	return st, nil
}

// restingUntil reads the account's quota and returns when its rest ends, or
// zero when it may send now.
func (p *Provider) restingUntil(ctx context.Context, accountID string, page Page) time.Time {
	p.refreshQuota(ctx, accountID, page)
	acc, err := p.accounts.Record(accountID)
	if err != nil {
		return time.Time{}
	}
	return pausedUntil(museView(acc), p.cfg.Get().Muse, p.now())
}

// refreshQuotaLater reads the account's quota once in the background, so the
// account card and the next plan see what the job cost (tracked, so Close
// waits for it and idle Chrome stays alive until it completes).
func (p *Provider) refreshQuotaLater(accountID string, page Page) {
	p.mu.Lock()
	if p.quotaPending == nil {
		p.quotaPending = make(map[string]int)
	}
	p.quotaPending[accountID]++
	p.mu.Unlock()
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		defer logger.Recover("cập nhật hạn mức Muse")
		defer func() {
			p.mu.Lock()
			if p.quotaPending[accountID]--; p.quotaPending[accountID] <= 0 {
				delete(p.quotaPending, accountID)
			}
			p.mu.Unlock()
			p.closeIdleBrowserIfDone(accountID)
		}()
		qCtx, cancel := context.WithTimeout(p.lifetime(), p.quotaRefreshTimeout())
		defer cancel()
		p.refreshQuota(qCtx, accountID, page)
	}()
}

// noResult explains a chat whose session ended without a file of the job's
// kind, from what Muse reports rather than from its wording:
//   - an interrupted run is Muse's own failure: another chat may work;
//   - a completed run with the account's usage gone (Muse's subscription,
//     read now) is a used-up account: the job moves to another one;
//   - any other completed run is Muse declining the prompt.
func (p *Provider) noResult(ctx context.Context, job MuseJob, accountID string, lease *accountLease, page *Page, ended string, replies []AssistantEvent, cfg config.MuseConfig) error {
	reply := replyText(replies)
	if ended == sessionInterrupted {
		if kind, detail := classifyRestrictionForKind(job.Kind, "", nil, reply, nil); kind != restrictNone {
			if kind == restrictVideoDisabled {
				p.disableVideoForAccount(accountID, detail)
			}
			_, _, refErr := p.refreshAccountForRun(ctx, accountID, job.Kind, page, lease, "interrupted_restriction")
			if refErr != nil && (errors.Is(refErr, errNotSignedIn) || errors.As(refErr, new(*accountRestrictedError))) {
				return refErr
			}
			switch kind {
			case restrictDailyVideo24h:
				return &dailyVideoLimitError{reason: detail}
			case restrictDeadAccount:
				return &accountRestrictedError{dead: true, reason: detail}
			case restrictVideoDisabled:
				return &accountRestrictedError{dead: false, reason: detail}
			}
		}
		msg := "Muse dừng giữa chừng, không có kết quả."
		if reply != "" {
			msg += " Muse trả lời: \"" + reply + "\""
		}
		return &museError{msg: msg}
	}
	p.refreshQuota(ctx, accountID, *page)
	var freshQuota *MuseQuota
	if latestAcc, qErr := p.accounts.Record(accountID); qErr == nil {
		freshQuota = museView(latestAcc).Quota
	}
	kind, detail := classifyRestrictionForKind(job.Kind, "", nil, reply, freshQuota)
	if kind != restrictNone {
		if kind == restrictVideoDisabled {
			p.disableVideoForAccount(accountID, detail)
		}
		_, _, refErr := p.refreshAccountForRun(ctx, accountID, job.Kind, page, lease, "completed_restriction")
		if refErr != nil {
			var refRestricted *accountRestrictedError
			if errors.Is(refErr, errNotSignedIn) || (errors.As(refErr, &refRestricted) && refRestricted.dead) {
				return refErr
			}
		}
		switch kind {
		case restrictDeadAccount:
			return &accountRestrictedError{dead: true, reason: detail}
		case restrictVideoDisabled:
			return &accountRestrictedError{dead: false, reason: detail}
		case restrictDailyVideo24h:
			return &dailyVideoLimitError{reason: detail}
		}
	}
	if p.accountPaused(accountID, cfg) {
		return &usageError{}
	}
	return &policyError{reply: reply}
}

func (p *Provider) downloadWanted(ctx context.Context, job MuseJob, accountID string, page Page, wanted []ResultMedia) ([]string, error) {
	files := make([]string, len(wanted))
	if job.Kind == MuseJobVideo || len(wanted) == 1 {
		for k, m := range wanted {
			release, err := p.downloadSlot(ctx, accountID)
			if err != nil {
				return nil, err
			}
			url, err := p.download(ctx, page, m, fmt.Sprintf("muse-%s-%d", job.ID, k+1))
			release()
			if err != nil {
				return nil, err
			}
			files[k] = url
		}
		return files, nil
	}
	dlCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	err := parallelIndex(len(wanted), collectImageConcurrency, func(k int) error {
		release, err := p.imageDownloadSlot(dlCtx, accountID)
		if err != nil {
			return err
		}
		defer release()
		url, err := p.download(dlCtx, page, wanted[k], fmt.Sprintf("muse-%s-%d", job.ID, k+1))
		if err != nil {
			cancel()
			return err
		}
		files[k] = url
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// download stores one result file. Muse reports each file's size, which is
// both the expected length and the limit. A file whose size Muse left out is
// read whole (the page reads the file's own size) under the app's download cap.
func (p *Provider) download(ctx context.Context, page Page, m ResultMedia, id string) (string, error) {
	limit := p.cfg.Get().Accounts.MaxDownloadBytes
	if limit <= 0 || m.ByteLen > limit {
		return "", &inputError{msg: "Tệp Muse vượt giới hạn tải media trong cấu hình."}
	}
	if m.ByteLen > 0 {
		limit = min(limit, m.ByteLen)
	}
	reader, writer := io.Pipe()
	downloadCtx, cancel := context.WithCancel(ctx)
	finished := make(chan struct{})
	defer func() {
		reader.Close()
		cancel()
		<-finished // release the account slot only after Chrome closed its Blob
	}()
	go func() {
		defer close(finished)
		defer logger.Recover("tải file Muse", func(err error) { writer.CloseWithError(err) })
		n, err := page.Download(downloadCtx, m.Path, writer)
		if err == nil && m.ByteLen > 0 && n != m.ByteLen {
			err = fmt.Errorf("Tải file lỗi: %d/%d byte", n, m.ByteLen)
		}
		writer.CloseWithError(err)
	}()
	return p.media.PutReader(id, m.Mime, filepath.Ext(m.Path), reader, limit)
}

// capabilitiesTTL is how long limits read from Muse are trusted before they
// are read again; Muse may change them with a new build.
const capabilitiesTTL = 24 * time.Hour

// capabilitiesFresh reports whether stored limits are recent enough to trust.
func capabilitiesFresh(c *MuseCapabilities, now time.Time) bool {
	if c == nil {
		return false
	}
	checked, err := time.Parse(time.RFC3339, c.CheckedAt)
	return err == nil && now.Sub(checked) < capabilitiesTTL
}

// capabilities returns what the account may send: the stored limits while
// they are recent, otherwise read from the page (fresh forces a read). A
// failed read falls back to the stored limits. read reports a page read.
func (p *Provider) capabilities(ctx context.Context, acc accounts.Record, page Page, fresh bool) (caps *MuseCapabilities, read bool, err error) {
	var data museData
	_ = accounts.DecodeData(acc, &data)
	stored := data.Capabilities
	if !fresh && capabilitiesFresh(stored, p.now()) {
		return stored, false, nil
	}
	caps, err = page.Capabilities(ctx)
	if err != nil || caps == nil {
		if stored != nil {
			return stored, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		return nil, false, &museError{msg: "Không đọc được giới hạn đính kèm của Muse trên tài khoản này."}
	}
	p.updateAccount(acc.ID, func(_ *accounts.Record, d *museData) { d.Capabilities = caps })
	return caps, true, nil
}

// attachments loads the job's reference images in order, within the limits
// Muse's composer has for this account. Limits read earlier that refuse the
// job are read again once before the job fails, in case Muse changed them.
func (p *Provider) attachments(ctx context.Context, job MuseJob, acc accounts.Record, page Page) ([]Attachment, error) {
	if len(job.References) == 0 && len(job.Frames) == 0 && job.SourceVideo == "" {
		return nil, nil
	}
	caps, read, err := p.capabilities(ctx, acc, page, false)
	if err != nil {
		return nil, err
	}
	atts, err := p.attach(job, caps)
	var input *inputError
	if errors.As(err, &input) && !read {
		if again, read, e := p.capabilities(ctx, acc, page, true); e == nil && read {
			return p.attach(job, again)
		}
	}
	return atts, err
}

// attach reads the job's reference images and checks them against caps,
// reusing cached compressed/encoded results across batch jobs when the file's
// timestamp, size and size budget match.
func (p *Provider) attach(job MuseJob, caps *MuseCapabilities) ([]Attachment, error) {
	images := append(slices.Clone(job.Frames), job.References...)
	total := len(images)
	if job.SourceVideo != "" {
		total++
	}
	if total > caps.MaxFiles {
		return nil, &inputError{msg: fmt.Sprintf("Muse nhận tối đa %d file mỗi tin nhắn (tác vụ có %d file).", caps.MaxFiles, total)}
	}
	atts := make([]Attachment, 0, total)
	if job.SourceVideo != "" {
		att, err := p.attachVideo(job.SourceVideo, caps)
		if err != nil {
			return nil, err
		}
		atts = append(atts, att)
	}
	if len(images) > 0 {
		type imageMeta struct {
			path    string
			ext     string
			mime    string
			modTime int64
			size    int64
		}
		metas := make([]imageMeta, len(images))
		empty := append([]Attachment(nil), atts...)
		for k, ref := range images {
			path, err := p.media.Path(ref)
			if err != nil {
				return nil, &inputError{msg: err.Error()}
			}
			ext := strings.ToLower(filepath.Ext(path))
			mime, ok := caps.imageMime(ext)
			if !ok {
				return nil, &inputError{msg: "Muse không nhận định dạng ảnh này: " + filepath.Base(path)}
			}
			info, err := os.Stat(path)
			if err != nil {
				return nil, &inputError{msg: "Không đọc được ảnh tham chiếu (có thể đã bị xoá): " + filepath.Base(path)}
			}
			metas[k] = imageMeta{path: path, ext: ext, mime: mime, modTime: info.ModTime().UnixNano(), size: info.Size()}
			empty = append(empty, Attachment{Filename: fmt.Sprintf("image_%d%s", k+1, ext), MimeType: mime, Category: "image"})
		}
		text := buildMessage(job)
		room := museInlineRequestBudget - inlineRequestBytes(text, empty)
		if room <= 0 {
			return nil, &inputError{msg: "Nội dung lệnh hoặc video đính kèm quá lớn để gửi Muse. Hãy rút ngắn lệnh hoặc giảm dung lượng video."}
		}
		target := room/len(images) - 256
		if target <= 0 {
			return nil, &inputError{msg: "Không đủ dung lượng để đính kèm ảnh vào lệnh Muse."}
		}
		bucket := attachmentTargetBucket(target)
		sig := capsSignature(caps)
		preAtts := make([]Attachment, len(metas))
		type pendingImage struct {
			idx int
			key attachmentCacheKey
			att Attachment
		}
		var todo []pendingImage
		for k, m := range metas {
			key := attachmentCacheKey{
				path:         m.path,
				modTime:      m.modTime,
				size:         m.size,
				maxFileBytes: caps.MaxFileBytes,
				targetBucket: bucket,
				capsSig:      sig,
			}
			if cached, ok := p.attCache.get(key); ok && len(cached.dataBase64) <= target && int64(base64.StdEncoding.DecodedLen(len(cached.dataBase64))) <= caps.MaxFileBytes {
				preAtts[k] = Attachment{
					Filename:   fmt.Sprintf("image_%d%s", k+1, cached.ext),
					MimeType:   cached.mimeType,
					DataBase64: cached.dataBase64,
					Category:   "image",
				}
				continue
			}
			todo = append(todo, pendingImage{idx: k, key: key})
		}
		if err := parallelIndex(len(todo), attachReduceConcurrency, func(i int) error {
			item := &todo[i]
			m := metas[item.idx]
			data, err := os.ReadFile(m.path)
			if err != nil {
				return &inputError{msg: "Không đọc được ảnh tham chiếu (có thể đã bị xoá): " + filepath.Base(m.path)}
			}
			att := Attachment{
				Filename:   fmt.Sprintf("image_%d%s", item.idx+1, m.ext),
				MimeType:   m.mime,
				DataBase64: base64.StdEncoding.EncodeToString(data),
				Category:   "image",
			}
			if len(att.DataBase64) > target || int64(len(data)) > caps.MaxFileBytes {
				reduceBudget := bucket
				if reduceBudget <= 0 || reduceBudget > target {
					reduceBudget = target
				}
				att, err = reduceAttachment(att, reduceBudget, caps)
				if err != nil {
					return &inputError{msg: fmt.Sprintf("Không thể giảm dung lượng ảnh %s để gửi Muse: %v", fmt.Sprintf("image_%d%s", item.idx+1, m.ext), err)}
				}
			}
			item.att = att
			return nil
		}); err != nil {
			return nil, err
		}
		for _, item := range todo {
			p.attCache.put(item.key, cachedAttachment{
				ext:        strings.ToLower(filepath.Ext(item.att.Filename)),
				mimeType:   item.att.MimeType,
				dataBase64: item.att.DataBase64,
			})
			preAtts[item.idx] = item.att
		}
		atts = append(atts, preAtts...)
	}
	return prepareAttachments(job, atts, caps)
}

// attachVideo reads a source video and checks it against caps.
func (p *Provider) attachVideo(ref string, caps *MuseCapabilities) (Attachment, error) {
	path, err := p.media.Path(ref)
	if err != nil {
		return Attachment{}, &inputError{msg: err.Error()}
	}
	ext := strings.ToLower(filepath.Ext(path))
	mime, ok := caps.videoMime(ext)
	if !ok {
		return Attachment{}, &inputError{msg: "Muse không nhận video định dạng này: " + filepath.Base(path)}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Attachment{}, &inputError{msg: "Không đọc được video mẫu (có thể đã bị xoá): " + filepath.Base(path)}
	}
	if int64(len(data)) > caps.MaxFileBytes {
		return Attachment{}, &inputError{msg: fmt.Sprintf("%s vượt %d MB, giới hạn một file của Muse.", filepath.Base(path), caps.MaxFileBytes>>20)}
	}
	return Attachment{Filename: "video_1" + ext, MimeType: mime, DataBase64: base64.StdEncoding.EncodeToString(data), Category: "video"}, nil
}

// errPageNotReady means the account's tab answered but Muse's chat client
// never showed up: the account is signed in (or cannot be told), so nothing
// is marked signed out and the job waits for the page.
var errPageNotReady = errors.New("Trang Muse của tài khoản chưa sẵn sàng (chưa có khung chat).")

// assignTab binds a chromePage to a dedicated muse.ai tab slot for accountID,
// opening a tab only when existing tabs are busy and below ThreadsPerAccount.
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
	targetID, err := p.tabs.acquire(ctx, accountID, jobID, cp.port, cfg.ThreadsPerAccount, cp.policy)
	if err != nil {
		return err
	}
	port := cp.port
	policy := cp.policy
	cp.bindTab(targetID, p.tabNavLock(accountID, targetID), func(dirty bool) {
		releaseCtx := context.Background()
		p.mu.Lock()
		if p.ctx != nil {
			releaseCtx = p.ctx
		}
		p.mu.Unlock()
		p.tabs.release(releaseCtx, accountID, port, targetID, dirty, func() int {
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
		// If another thread already owns a tab on this Chrome, bind a separate tab
		// before waitSignedIn so any Home recovery cannot disturb active work.
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

// Policy for jobs Muse did not run.
const (
	// maxLimitReplies caps how often one job may be handed on after a chat
	// ended without a file on a used-up account: each cost a chat turn.
	maxLimitReplies = 3
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
	switch {
	case errors.As(err, &restricted):
		if restricted.dead {
			p.clearSessionVerified(accountID)
			p.disableRestrictedAccount(accountID, restricted.Error())
		} else {
			p.disableVideoForAccount(accountID, restricted.reason)
		}
		if job.Continues > 0 {
			log("account restricted", "failed", err)
			failed(restricted.Error())
			return
		}
		log("account restricted", "requeued to another account", err)
		requeue(fmt.Sprintf("%s: %v", accName, restricted), false)
	case errors.As(err, &dailyLimit):
		until := p.pauseDailyVideoLimit(accountID, dailyLimit.reason)
		if job.Continues > 0 {
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
	if p.quotaSlots != nil {
		release, err := p.accountSlot(ctx, accountID, p.quotaSlots)
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
	if q.CheckedAt == "" {
		q.CheckedAt = p.now().UTC().Format(time.RFC3339)
	}
	var before float64 = -1
	p.updateAccount(accountID, func(_ *accounts.Record, d *museData) {
		if d.Quota != nil {
			before = d.Quota.PercentUsed
		}
		d.Quota = q
	})
	logger.Get().Info("muse quota read", "account", accountID, "percentBefore", before, "percent", q.PercentUsed, "topup", q.TopupBalance)
}

// saveOutputs copies a batch job's files into the folder it was queued with,
// numbered by its place in the batch (media.OutputName). A job without a
// folder (Đơn, graph nodes) saves nothing; a job cancelled meanwhile keeps the
// user's decision: its files stay in the media folder and are not copied out.
func (p *Provider) saveOutputs(job MuseJob, files []string) []string {
	current, err := p.saveCollected(job, files)
	if err != nil {
		logger.Get().Warn("muse save failed", "job", job.ID, "error", err)
	}
	return current.SavedFiles
}

func (p *Provider) saveCollected(job MuseJob, files []string) (MuseJob, error) {
	p.saveMu.Lock()
	defer p.saveMu.Unlock()
	current, ok := p.jobs.get(job.ID)
	if !ok || current.Status != MuseJobProcessing {
		return MuseJob{}, nil
	}
	return p.writeOutputs(current, files)
}
