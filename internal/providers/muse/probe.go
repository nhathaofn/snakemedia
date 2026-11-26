package muse

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/logger"
)

const (
	// videoCheckTimeout bounds how long one account video check waits.
	videoCheckTimeout = 5 * time.Minute
	// videoCheckQuotaTimeout bounds the quota read after a video check finishes.
	videoCheckQuotaTimeout = 15 * time.Second
)

func videoCheckPrompt() string {
	return buildMessage(MuseJob{
		Kind:     MuseJobVideo,
		Duration: 2,
		Prompt:   "A cute cat sitting still by a sunny window.",
	})
}

func checkPollDelay(elapsed time.Duration) time.Duration {
	switch {
	case elapsed >= 2*time.Minute:
		return 6 * time.Second
	case elapsed >= 30*time.Second:
		return 4 * time.Second
	default:
		return 2500 * time.Millisecond
	}
}

// CheckVideo runs an active video generation probe when explicitly requested by
// the user (CheckAccountVideo), detecting video bans and 24h daily video limits.
func (s *Site) CheckVideo(ctx context.Context, b *browser.Browser, rec accounts.Record, insp accounts.Inspection) (accounts.Inspection, error) {
	cfg := s.cfg.Get().Muse
	now := s.nowFn()
	var data museData
	switch v := insp.Data.(type) {
	case museData:
		data = v
	default:
		_ = accounts.DecodeData(rec, &data)
	}

	if rec.SkipVideoCheck {
		insp.Data = data
		insp.Usage = accountUsage(data)
		if insp.Note == "" {
			insp.Note = "Tài khoản còn sống (đã tắt kiểm tra tạo video, chỉ tạo ảnh)."
		}
		return insp, nil
	}

	if data.Quota != nil && data.Quota.weeklyOut() && (!cfg.UseTopup || !data.Quota.topupLeft()) {
		until := now.Add(time.Duration(cfg.UsagePauseMinutes) * time.Minute)
		if reset := data.Quota.resetsAt(); reset.After(now) {
			until = reset
		}
		data.RestReason = restQuota
		insp.Data = data
		insp.Usage = accountUsage(data)
		insp.PausedUntil = until
		insp.AutoResume = false
		insp.Note = "Tài khoản đã hết hạn mức tuần; tạm nghỉ tới " + until.Local().Format("15:04 02/01")
		return insp, nil
	}

	page := s.openSitePage(b)
	defer closePage(page)

	var beforeSet map[string]bool
	if tracker, ok := page.(sessionTracker); ok {
		before, snapErr := tracker.Sessions(ctx, true)
		if snapErr != nil && ctx.Err() == nil {
			_ = page.Home(ctx)
			_ = s.waitVideoTabReady(ctx, page, 15*time.Second)
			before, snapErr = tracker.Sessions(ctx, true)
		}
		if snapErr == nil {
			beforeSet = make(map[string]bool, len(before))
			for _, sid := range sessionIDs(before) {
				beforeSet[sid] = true
			}
		}
	}

	prompt := videoCheckPrompt()
	sendTimeout := max(time.Duration(cfg.SendTimeoutSeconds)*time.Second, 2*time.Minute)
	sendAttempt := func() (SendResult, error) {
		sendCtx, cancelSend := context.WithTimeout(ctx, sendTimeout)
		defer cancelSend()
		return page.Send(sendCtx, prompt, nil)
	}
	res, err := sendAttempt()
	for retry := 0; err == nil && strings.HasPrefix(res.Reason, "not_ready") && retry < 2; retry++ {
		if s.sleepFn(ctx, 2*time.Second) != nil {
			return insp, ctx.Err()
		}
		_ = s.waitVideoTabReady(ctx, page, 15*time.Second)
		res, err = sendAttempt()
	}
	if err != nil {
		if b != nil && b.Port > 0 {
			if info, aErr := s.checkAway(ctx, b.Port, s.cfg.Get().Accounts, false); aErr == nil {
				if kind, detail := classifyAwayTab(info); kind == restrictDeadAccount {
					insp.Fatal = "Tài khoản không vào được Muse: " + detail
					insp.DisableAccount = true
					return insp, nil
				}
			}
		}
		return insp, fmt.Errorf("Không gửi được lệnh kiểm tra tạo video (thử lại hoặc Mở Chrome kiểm tra): %w", err)
	}
	if strings.HasPrefix(res.Reason, "not_ready") {
		return insp, fmt.Errorf("Trang Muse chưa sẵn sàng khung chat để kiểm tra tạo video (%s). Hãy thử lại.", res.Reason)
	}

	data.LastSentAt = s.nowFn().UTC().Format(time.RFC3339Nano)
	if !res.OK {
		quota, _ := page.Quota(ctx)
		if quota != nil {
			data.Quota = quota
		}
		kind, _ := classifyRestrictionForKind(MuseJobVideo, res.Reason, res.Diagnostics, "", quota)
		weeklyOut := quota != nil && quota.weeklyOut() && (!cfg.UseTopup || !quota.topupLeft())
		if kind == restrictNone && !weeklyOut {
			if isTransientVideoCheckFailure(res.Reason, res.Diagnostics) {
				insp.Data, insp.Usage = data, accountUsage(data)
				return insp, fmt.Errorf("Lỗi tạm thời khi gửi lệnh kiểm tra tạo video (%s). Hãy thử lại.", firstNonEmpty(diagText(res.Diagnostics), res.Reason))
			}
		}
		return s.classifyCheckFailure(res.Reason, res.Diagnostics, "", quota, data, insp), nil
	}

	sid := res.SessionID
	if sid == "" && beforeSet != nil {
		if tracker, ok := page.(sessionTracker); ok {
			if cur, sErr := tracker.Sessions(ctx, true); sErr == nil {
				newIDs := unseenSessionIDs(cur, beforeSet)
				if len(newIDs) == 1 {
					sid = newIDs[0]
				}
			}
		}
	}
	if sid == "" {
		insp.Data, insp.Usage = data, accountUsage(data)
		return insp, errors.New("Muse không trả về mã phiên khi kiểm tra tạo video; hãy thử lại.")
	}

	limit := time.Duration(cfg.VideoTimeoutMinutes) * time.Minute
	if limit <= 0 || limit > videoCheckTimeout {
		limit = videoCheckTimeout
	}
	ended, replies, pollErr := s.waitVideoCheck(ctx, page, sid, limit, cfg.PollErrorLimit)
	if pollErr != nil {
		insp.Data, insp.Usage = data, accountUsage(data)
		return insp, fmt.Errorf("Kiểm tra tạo video chưa hoàn tất (thử lại): %w", pollErr)
	}

	wanted, _ := wantedMedia(replies, MuseJobVideo)
	qCtx, cancelQ := context.WithTimeout(ctx, videoCheckQuotaTimeout)
	quota, _ := page.Quota(qCtx)
	cancelQ()
	if quota != nil {
		data.Quota = quota
	}
	for _, m := range wanted {
		if !m.Missing {
			data.RestReason = ""
			insp.Data = data
			insp.Usage = accountUsage(data)
			insp.Resume = true
			insp.VideoVerified = true
			insp.Note = "Tạo video thử thành công."
			return insp, nil
		}
	}

	reply := replyText(replies)
	if ended == sessionInterrupted {
		kind, _ := classifyRestrictionForKind(MuseJobVideo, "", nil, reply, quota)
		weeklyOut := quota != nil && quota.weeklyOut() && (!cfg.UseTopup || !quota.topupLeft())
		if kind == restrictNone && !weeklyOut {
			insp.Data, insp.Usage = data, accountUsage(data)
			return insp, errors.New("Phiên kiểm tra tạo video bị ngắt giữa chừng; hãy thử lại.")
		}
	}
	return s.classifyCheckFailure("", nil, reply, quota, data, insp), nil
}

func isTransientVideoCheckFailure(reason string, diag *SendDiagnostics) bool {
	if diag != nil && diag.RequestTimedOut {
		return true
	}
	if isSessionOrGatewayFailureText(reason) || isSessionOrGatewayFailureText(diagText(diag)) {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(reason + " " + diagText(diag)))
	for _, marker := range []string{"rate_limit", "rate limit", "too many requests", "http 429", "status 429", "temporarily unavailable", "timeout", "timed out"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return strings.EqualFold(strings.TrimSpace(reason), "send_failed") && strings.TrimSpace(diagText(diag)) == ""
}

func (s *Site) waitVideoTabReady(ctx context.Context, page Page, budget time.Duration) error {
	deadline := s.nowFn().Add(budget)
	for {
		st, err := page.Login(ctx)
		if err == nil && st.LoggedIn && st.Chat {
			return nil
		}
		if ctx.Err() != nil || !s.nowFn().Before(deadline) {
			return err
		}
		if err := s.sleepFn(ctx, 1500*time.Millisecond); err != nil {
			return err
		}
	}
}

func (s *Site) waitVideoCheck(ctx context.Context, page Page, sid string, limit time.Duration, errLimit int) (string, []AssistantEvent, error) {
	if errLimit <= 0 {
		errLimit = 5
	}
	start := s.nowFn()
	errorsInRow := 0
	for {
		elapsed := s.nowFn().Sub(start)
		if err := s.sleepFn(ctx, checkPollDelay(elapsed)); err != nil {
			return "", nil, err
		}
		status, err := page.Status(ctx, sid)
		if err != nil {
			if ctx.Err() != nil {
				return "", nil, ctx.Err()
			}
			errorsInRow++
			if errorsInRow >= errLimit {
				return "", nil, fmt.Errorf("không đọc được trạng thái phiên kiểm tra: %w", err)
			}
			status = sessionRunning
		} else {
			errorsInRow = 0
		}
		overdue := !s.nowFn().Before(start.Add(limit))
		// Only fetch full chat.history once sessions.list leaves "running" (or on errors/limit),
		// avoiding repeated heavy chat.history polls while Muse renders.
		if status != sessionRunning || errorsInRow > 0 || overdue {
			replies, rErr := page.Result(ctx, sid)
			if rErr == nil {
				wanted, _ := wantedMedia(replies, MuseJobVideo)
				for _, m := range wanted {
					if !m.Missing {
						return sessionCompleted, replies, nil
					}
				}
				if status == sessionCompleted {
					// Give chat.history a brief settle window when sessions.list flips
					// to completed right before the media resource/presentation lands.
					if err := s.sleepFn(ctx, 2*time.Second); err != nil {
						return "", nil, err
					}
					if fresh, fErr := page.Result(ctx, sid); fErr == nil {
						replies = fresh
						wanted, _ = wantedMedia(replies, MuseJobVideo)
						for _, m := range wanted {
							if !m.Missing {
								return sessionCompleted, replies, nil
							}
						}
					}
					return status, replies, nil
				}
				if status == sessionInterrupted {
					return status, replies, nil
				}
			}
		}
		if overdue {
			_ = page.Cancel(ctx, sid)
			return "", nil, fmt.Errorf("quá %d phút không nhận được kết quả tạo video thử", int(limit.Minutes()))
		}
	}
}

func (s *Site) classifyCheckFailure(reason string, diag *SendDiagnostics, reply string, quota *MuseQuota, data museData, insp accounts.Inspection) accounts.Inspection {
	cfg := s.cfg.Get().Muse
	now := s.nowFn()
	kind, detail := classifyRestriction(reason, diag, reply, quota)
	if kind == restrictDailyVideo24h {
		until := now.Add(24 * time.Hour)
		data.RestReason = restDailyVideo
		insp.Data = data
		insp.Usage = accountUsage(data)
		insp.DisableAccount = true
		insp.AutoResume = true
		insp.PausedUntil = until
		insp.Note = fmt.Sprintf("Hết hạn tạo video trong ngày (24h); tự tắt tới %s", until.Local().Format("15:04 02/01"))
		if detail != "" {
			insp.Note += " (" + truncate(detail, 120) + ")"
		}
		logger.Get().Warn("muse check hit 24h daily video limit", "detail", detail, "until", until)
		return insp
	}
	if kind == restrictDeadAccount {
		data.RestReason = ""
		insp.Data = data
		insp.Usage = accountUsage(data)
		insp.Fatal = (&accountRestrictedError{dead: true, reason: detail}).Error()
		insp.DisableAccount = true
		return insp
	}
	if quota != nil && quota.weeklyOut() && (!cfg.UseTopup || !quota.topupLeft()) {
		until := now.Add(time.Duration(cfg.UsagePauseMinutes) * time.Minute)
		if reset := quota.resetsAt(); reset.After(now) {
			until = reset
		}
		data.RestReason = restQuota
		insp.Data = data
		insp.Usage = accountUsage(data)
		insp.PausedUntil = until
		insp.AutoResume = false
		insp.Note = "Tài khoản hết hạn mức tuần; tạm nghỉ tới " + until.Local().Format("15:04 02/01")
		return insp
	}

	if detail == "" {
		detail = firstNonEmpty(reason, reply, "Muse không trả về video khi kiểm tra")
	}
	data.RestReason = ""
	insp.SignedIn = true
	insp.Data = data
	insp.Usage = accountUsage(data)
	insp.VideoCheckDisabled = true
	insp.Note = "Không tạo được video (đã tắt kiểm tra video, chỉ tạo ảnh): " + detail
	return insp
}
