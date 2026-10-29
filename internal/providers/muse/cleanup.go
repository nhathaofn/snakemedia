package muse

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/models"
)

// MuseCleanResult is how "Dọn đoạn chat phụ" went on one account.
type MuseCleanResult struct {
	AccountID string `json:"accountId"`
	Account   string `json:"account"`
	// Reply is Muse's answer; Error is set when the account was skipped or the
	// message failed.
	Reply string `json:"reply,omitempty"`
	Error string `json:"error,omitempty"`
}

// errCleanBusy skips an account whose jobs still need their chats.
var errCleanBusy = errors.New("Tài khoản còn tác vụ đang chạy; dọn sau khi tác vụ xong để không mất chat của nó.")

// CleanChats sends the cleanup message (config Muse.CleanupMessage) in a new
// chat on every enabled, signed-in Muse account, and Muse deletes the
// account's other chats. Deleting a chat loses the job sent in it, so an
// account with a job in progress (sent or running) is skipped, and an account
// being cleaned takes no new job until its reply came.
func (p *Provider) CleanChats(ctx context.Context) models.Response[[]MuseCleanResult] {
	cfg := p.cfg.Get().Muse
	text := strings.TrimSpace(cfg.CleanupMessage)
	if text == "" {
		return fail[[]MuseCleanResult](errors.New("Chưa có tin nhắn dọn đoạn chat phụ trong Cài đặt Muse."), models.CodeInvalidArgument)
	}
	records, err := p.accounts.Records(accounts.ProviderMuse)
	if err != nil {
		return fail[[]MuseCleanResult](err, models.CodeInternal)
	}
	var signedIn []accounts.Record
	for _, rec := range records {
		if rec.Enabled && rec.Status == accounts.StatusSignedIn {
			signedIn = append(signedIn, rec)
		}
	}
	if len(signedIn) == 0 {
		return fail[[]MuseCleanResult](errors.New("Không có tài khoản Muse nào đã đăng nhập."), models.CodeUnavailable)
	}
	results := make([]MuseCleanResult, len(signedIn))
	var wg sync.WaitGroup
	for i, rec := range signedIn {
		results[i] = MuseCleanResult{AccountID: rec.ID, Account: rec.Label()}
		if !p.reserveForClean(rec.ID) {
			results[i].Error = errCleanBusy.Error()
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer logger.Recover("dọn chat Muse", func(err error) { results[i].Error = err.Error() })
			defer p.endClean(rec.ID)
			reply, err := p.cleanAccount(ctx, rec, text, cfg.VideoTimeoutMinutes)
			results[i].Reply = reply
			if err != nil {
				results[i].Error = err.Error()
				logger.Get().Warn("muse chat cleanup failed", "account", rec.ID, "error", err)
			}
		}()
	}
	wg.Wait()
	return models.SuccessResponse(results)
}

// cleanAccount sends text in a new chat on the account and waits for Muse's
// reply. Deleting many chats can take Muse a while, so it waits as long as a
// video.
func (p *Provider) cleanAccount(ctx context.Context, rec accounts.Record, text string, minutes int) (string, error) {
	release := p.accounts.Acquire(rec.ID)
	defer release()
	page, err := p.pageFor(ctx, rec)
	if err != nil {
		return "", err
	}
	defer closePage(page)
	if err := p.waitSignedIn(ctx, page, time.Duration(p.cfg.Get().Accounts.PageReadySeconds)*time.Second); err != nil {
		return "", err
	}
	reply, _, err := sendNote(ctx, page, text, time.Duration(minutes)*time.Minute, p.cfg.Get().Muse, p.sleep, p.now)
	return reply, err
}

// reserveForClean keeps the scheduler off the account while it is cleaned,
// unless one of its jobs is running or waits to be checked in its chat.
func (p *Provider) reserveForClean(accountID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cleaning[accountID] {
		return false
	}
	for _, acc := range p.running {
		if acc == accountID {
			return false
		}
	}
	for _, job := range p.jobs.list() {
		if job.Status == MuseJobProcessing && job.AccountID == accountID {
			return false
		}
	}
	p.cleaning[accountID] = true
	return true
}

// endClean gives the account back to the scheduler.
func (p *Provider) endClean(accountID string) {
	p.mu.Lock()
	delete(p.cleaning, accountID)
	p.mu.Unlock()
	p.trigger()
}
