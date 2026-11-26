package muse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"path/filepath"
	"strings"
	"time"

	"SnakeMedia/internal/config"
	"SnakeMedia/internal/events"
	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/media"
)

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
				_, okRef, refErr := p.refreshAccountForResume(ctx, job.AccountID, job.Kind, page, lease, "poll_status")
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
			if isLong(job) {
				// A long video may already have generated clips in this chat;
				// never start a brand-new chat and pay for those clips again (ADR 0013).
				return "", nil, false, &longIncompleteError{continues: job.Continues, detail: fmt.Sprintf("Quá %d phút Muse chưa làm xong", int(limit.Minutes()))}
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
			if _, okRef, refErr := p.refreshAccountForResume(ctx, accountID, job.Kind, page, lease, "collect_result"); okRef {
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
	}
	// The library is filled before the frontend hears of the result,
	// since that event is when it reloads the library.
	if settled {
		p.addToLibrary(done)
	}
	p.emit(events.EventMuseJobUpdated, done)
	if settled && done.BoardFrames > 0 && done.Kind == MuseJobImage {
		p.releaseAfter(done)
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

type quotaRefresh struct {
	next Page
}

// refreshQuotaLater coalesces completions on the same account into one queued
// quota read while another is in progress.
func (p *Provider) refreshQuotaLater(accountID string, page Page) {
	page = p.detachedQuotaPage(accountID, page)
	p.mu.Lock()
	if p.acct.quotaPending == nil {
		p.acct.quotaPending = make(map[string]*quotaRefresh)
	}
	if active := p.acct.quotaPending[accountID]; active != nil {
		active.next = page
		p.mu.Unlock()
		return
	}
	active := &quotaRefresh{}
	p.acct.quotaPending[accountID] = active
	p.mu.Unlock()
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		defer logger.Recover("cập nhật hạn mức Muse")
		defer func() {
			p.mu.Lock()
			if p.acct.quotaPending[accountID] == active {
				delete(p.acct.quotaPending, accountID)
			}
			p.mu.Unlock()
			p.closeIdleBrowserIfDone(accountID)
		}()
		for page != nil {
			func() {
				qCtx, cancel := context.WithTimeout(p.lifetime(), p.quotaRefreshTimeout())
				defer cancel()
				p.refreshQuota(qCtx, accountID, page)
			}()
			p.mu.Lock()
			page, active.next = active.next, nil
			if page == nil {
				delete(p.acct.quotaPending, accountID)
			}
			p.mu.Unlock()
		}
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
		kind, detail := classifyRestrictionForKind(job.Kind, "", nil, reply, nil)
		if rErr := p.settleRestriction(ctx, kind, detail, job.Kind, accountID, lease, page, "interrupted_restriction"); rErr != nil {
			return rErr
		}
		msg := "Muse dừng giữa chừng, không có kết quả."
		if reply != "" {
			msg += " Muse trả lời: \"" + reply + "\""
		}
		return &museError{msg: msg}
	}
	p.refreshQuota(ctx, accountID, *page)
	freshQuota := p.accountQuota(accountID)
	kind, detail := classifyRestrictionForKind(job.Kind, "", nil, reply, freshQuota)
	if rErr := p.settleRestriction(ctx, kind, detail, job.Kind, accountID, lease, page, "completed_restriction"); rErr != nil {
		return rErr
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
