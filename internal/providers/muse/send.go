package muse

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/logger"
)

// stayInChatError marks an error from a follow-up turn inside an already-started
// chat (such as long-video continuation) so failure settlement never requeues
// or hands off the chat to another account, even on the first continue attempt.
type stayInChatError struct{ err error }

func (e *stayInChatError) Error() string { return e.err.Error() }
func (e *stayInChatError) Unwrap() error { return e.err }

// accountQuota returns the stored quota snapshot for accountID, or nil.
func (p *Provider) accountQuota(accountID string) *MuseQuota {
	if p.accounts == nil {
		return nil
	}
	if acc, err := p.accounts.Record(accountID); err == nil {
		return museView(acc).Quota
	}
	return nil
}

// settleRestriction updates account state/session after a non-none restriction
// classification and maps it to its domain error cleanly across send, continue,
// and poll paths.
func (p *Provider) settleRestriction(ctx context.Context, kind accountRestriction, detail string, jobKind MuseJobKind, accountID string, lease *accountLease, page *Page, reasonTag string) error {
	if kind == restrictNone {
		return nil
	}
	if kind == restrictVideoDisabled {
		p.disableVideoForAccount(accountID, detail)
	}
	_, _, refErr := p.refreshAccountForRun(ctx, accountID, jobKind, page, lease, reasonTag)
	if refErr != nil {
		var refRestricted *accountRestrictedError
		switch {
		case errors.Is(refErr, errNotSignedIn):
			return refErr
		case errors.As(refErr, &refRestricted) && refRestricted.dead:
			return refErr
		case errors.As(refErr, new(*dailyVideoLimitError)) && kind != restrictDeadAccount:
			if kind == restrictDailyVideo24h {
				return &dailyVideoLimitError{reason: detail}
			}
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
	return nil
}

// sendContinue posts continueMessage into a long video's chat. Continues and
// RepliesSeen are written first, so after a lost answer or a restart the job
// only waits for a reply newer than the ones already read; it is never sent
// again in a new chat. SentAt moves to now: the time limit counts from here.
func (p *Provider) sendContinue(ctx context.Context, job MuseJob, acc accounts.Record, page Page, seen int) (MuseJob, error) {
	pCopy := page
	updated, err := p.sendContinueWithLease(ctx, job, acc, nil, &pCopy, seen)
	return updated, err
}

func (p *Provider) sendContinueWithLease(ctx context.Context, job MuseJob, acc accounts.Record, lease *accountLease, pagePtr *Page, seen int) (MuseJob, error) {
	var page Page
	if pagePtr != nil {
		page = *pagePtr
	}
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
		p.refreshQuota(sendCtx, acc.ID, page)
		freshQuota := p.accountQuota(acc.ID)
		kind, detail := classifyRestrictionForKind(job.Kind, res.Reason, res.Diagnostics, "", freshQuota)
		if rErr := p.settleRestriction(sendCtx, kind, detail, job.Kind, acc.ID, lease, pagePtr, "continue_restriction"); rErr != nil {
			return job, &stayInChatError{err: rErr}
		}
		if latest, recErr := p.accounts.Record(acc.ID); recErr == nil {
			if until := pausedUntil(museView(latest), p.cfg.Get().Muse, p.now()); !until.IsZero() {
				return job, &waitAccountError{until: until}
			}
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
				switch outcome {
				case reconcileFound:
					if sid != "" {
						p.recordCancelledSession(job.ID, sid)
						if page != nil {
							_ = page.Cancel(sendCtx, sid)
						}
					} else {
						p.markCancelledSendUnknown(job.ID)
					}
				case reconcileNotSent:
					p.clearSendingState(job.ID)
				default:
					p.markCancelledSendUnknown(job.ID)
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
				var evalErr *browser.EvalError
				if errors.As(err, &evalErr) {
					p.markRecycleBrowser(acc.ID)
					return job, page, &museError{msg: fmt.Sprintf("Lỗi trong trang Muse khi gửi lệnh (chưa tạo phiên mới): %v", evalErr)}
				}
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
			if ctx.Err() != nil {
				p.markCancelledSendUnknown(job.ID)
				return job, page, context.Canceled
			}
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
		if res.Reason != "" {
			return job, page, &notSentError{err: fmt.Errorf("%w (%s)", errPageNotReady, res.Reason)}
		}
		return job, page, &notSentError{err: errPageNotReady}
	}
	p.clearRecycleBrowser(acc.ID)
	p.updateAccount(acc.ID, func(_ *accounts.Record, d *museData) { d.LastSentAt = p.now().UTC().Format(time.RFC3339Nano) })
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
		freshQuota := p.accountQuota(acc.ID)
		kind, detail := classifyRestrictionForKind(job.Kind, res.Reason, res.Diagnostics, "", freshQuota)
		if kind == restrictNone && canRecover && isRecoverableSendRejection(res.Reason, res.Diagnostics, job.Kind) {
			return job, page, &recoverableSendError{reason: "send_refused", inner: &rejectedSend{reason: res.Reason}}
		}
		if rErr := p.settleRestriction(sendCtx, kind, detail, job.Kind, acc.ID, lease, &page, "send_restriction"); rErr != nil {
			return job, page, rErr
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
		j.SessionID, j.Sending, j.PreSendTracked, j.PreSendSessions, j.NotSentRetries = res.SessionID, false, false, nil, 0
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

// markCancelledSendUnknown records that a cancelled in-flight send could not
// be confirmed as not sent, so RetryFailed / mayBePaid refuses to silently
// double-send it.
func (p *Provider) markCancelledSendUnknown(jobID string) {
	_, _ = p.updateJob(jobID, func(j *MuseJob) {
		if j.Status == MuseJobQueued {
			return
		}
		j.Sending, j.PreSendTracked, j.PreSendSessions = false, false, nil
		j.SendUnknown = true
	})
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
	if len(job.References) == 0 && len(job.Frames) == 0 {
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

// attach reads the job's reference images in order, normalizing outgoing
// attachments to <= 2048px JPEG when Muse accepts .jpg while keeping Vault and
// MediaDir originals untouched, and reusing cached compressed/encoded results.
func (p *Provider) attach(job MuseJob, caps *MuseCapabilities) ([]Attachment, error) {
	images := append(slices.Clone(job.Frames), job.References...)
	total := len(images)
	atts := make([]Attachment, 0, total)
	if len(images) > 0 {
		type imageMeta struct {
			path    string
			ext     string
			mime    string
			modTime int64
			size    int64
		}
		metas := make([]imageMeta, len(images))
		empty := make([]Attachment, 0, total)
		_, jpegOK := caps.imageMime(".jpg")
		for k, ref := range images {
			path, err := p.media.OpenPath(ref)
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
			emptyExt := ext
			if jpegOK {
				emptyExt = ".jpg"
			}
			empty = append(empty, Attachment{Filename: fmt.Sprintf("image_%d%s", k+1, emptyExt), MimeType: mime, Category: "image"})
		}
		text := buildMessage(job)
		room := museInlineRequestBudget - inlineRequestBytes(text, empty)
		if room <= 0 {
			return nil, &inputError{msg: "Nội dung lệnh quá lớn để đính kèm ảnh gửi Muse. Hãy rút ngắn lệnh."}
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
			if cached, ok := p.attCache.get(key); ok && len(cached.dataBase64) <= target && int64(base64.StdEncoding.DecodedLen(len(cached.dataBase64))) <= caps.MaxFileBytes && (!jpegOK || cached.ext == ".jpg") {
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
			if needsOutgoingJPEGNorm(data, m.ext, target, caps) {
				reduceBudget := bucket
				if reduceBudget <= 0 || reduceBudget > target {
					reduceBudget = target
				}
				att, err = reduceAttachment(att, reduceBudget, caps)
				if err != nil {
					return &inputError{msg: fmt.Sprintf("Không thể chuẩn hoá ảnh %s để gửi Muse: %v", fmt.Sprintf("image_%d%s", item.idx+1, m.ext), err)}
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
