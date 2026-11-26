package muse

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"SnakeMedia/internal/config"
)

// setupReplyMax caps how much of Muse's reply to the setup message is kept on
// the account.
const setupReplyMax = 2000

// sendSetup sends the setup message (config Muse.SetupMessage) in a new chat
// of its own and waits for Muse to reply, which is all it takes for Muse to
// have saved it. The chat is left as it is. It returns the reply, and whether
// Muse took the message (it opened a chat for it): such a message is never
// sent again, even without a reply. A lost answer counts as not taken, so the
// rule is sure to reach the account. An error is only reported; the account
// is signed in either way.
func sendSetup(ctx context.Context, page Page, cfg config.MuseConfig, sleep func(context.Context, time.Duration) error, now func() time.Time) (reply string, sent bool, err error) {
	text := strings.TrimSpace(cfg.SetupMessage)
	if text == "" {
		return "", false, nil
	}
	return sendNoteWithLabel(ctx, page, text, "thiết lập", time.Duration(cfg.ImageTimeoutMinutes)*time.Minute, cfg, sleep, now)
}

// sendNote sends text in a new chat of its own and waits up to limit for
// Muse's text reply. sent reports whether Muse took the message (it opened a
// chat for it).
func sendNote(ctx context.Context, page Page, text string, limit time.Duration, cfg config.MuseConfig, sleep func(context.Context, time.Duration) error, now func() time.Time) (reply string, sent bool, err error) {
	return sendNoteWithLabel(ctx, page, text, "", limit, cfg, sleep, now)
}

func sendNoteWithLabel(ctx context.Context, page Page, text, label string, limit time.Duration, cfg config.MuseConfig, sleep func(context.Context, time.Duration) error, now func() time.Time) (reply string, sent bool, err error) {
	if limit <= 0 {
		return "", false, errors.New("Thời gian chờ phản hồi không hợp lệ.")
	}
	res, err := page.Send(ctx, text, nil)
	if err != nil {
		return "", false, fmt.Errorf("không gửi được: %w", err)
	}
	if !res.OK || res.SessionID == "" {
		reason := res.Reason
		if reason == "" {
			reason = "Muse không mở chat"
		}
		return "", false, fmt.Errorf("Muse không nhận tin nhắn (%s)", reason)
	}
	deadline := now().Add(limit)
	errLimit := max(cfg.PollErrorLimit, 1)
	errCount := 0
	pollInterval := time.Duration(cfg.PollIntervalMS) * time.Millisecond
	if pollInterval <= 0 {
		pollInterval = 500 * time.Millisecond
	}
	for {
		status, err := page.Status(ctx, res.SessionID)
		if err == nil {
			errCount = 0
			if status != sessionRunning {
				if events, rErr := page.Result(ctx, res.SessionID); rErr == nil {
					if reply := setupReply(events); reply != "" {
						return reply, true, nil
					}
					if status == sessionCompleted || status == sessionInterrupted {
						return "", true, errors.New("Muse không trả lời")
					}
				}
			}
		} else if ctx.Err() == nil {
			errCount++
			if errCount >= errLimit {
				if label != "" {
					return "", true, fmt.Errorf("không đọc được phản hồi %s: %w", label, err)
				}
				return "", true, fmt.Errorf("không đọc được phản hồi từ Muse: %w", err)
			}
		}
		if !now().Before(deadline) {
			return "", true, fmt.Errorf("quá %d phút Muse chưa trả lời", int(limit.Minutes()))
		}
		if err := sleep(ctx, pollInterval); err != nil {
			return "", true, err
		}
	}
}

// setupReply is Muse's text reply in a chat, trimmed to setupReplyMax.
func setupReply(events []AssistantEvent) string {
	var parts []string
	for _, ev := range events {
		if t := strings.TrimSpace(ev.Text); t != "" {
			parts = append(parts, t)
		}
	}
	reply := strings.Join(parts, "\n\n")
	if r := []rune(reply); len(r) > setupReplyMax {
		reply = string(r[:setupReplyMax]) + "…"
	}
	return reply
}
