package muse

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/logger"
)

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
			j.SessionID, j.Sending, j.PreSendTracked, j.PreSendSessions, j.NotSentRetries = sid, false, false, nil, 0
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
