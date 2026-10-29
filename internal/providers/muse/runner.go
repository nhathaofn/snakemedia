package muse

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
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

// referenceLines say how a job's n attached images are used, worded from n.
// The setup message has every account treat each request on its own and use
// an attached or earlier image only when the request itself says so, so a job
// with images must say it. The prompt may give an image any role; without
// one, a single image is the subject and several images are separate elements
// (never merged), each kept as drawn. Images are numbered in the order
// attached (their file names, image_N) for the prompt to name them.
func referenceLines(kind MuseJobKind, n int) []string {
	what := kindWord[kind]
	keep := "keep its design, colors, details and art style exactly"
	if kind == MuseJobVideo {
		keep += ", consistent in every frame"
	}
	if n == 1 {
		return []string{
			fmt.Sprintf("Reference: The attached image (image 1) shows the subject of this %s; %s. "+
				"Do not copy the image's background, framing or pose; take those from the prompt. "+
				"If the prompt gives the image another role (for example the style only, or the opening frame), follow the prompt instead.", what, keep),
		}
	}
	return []string{
		fmt.Sprintf("Reference: %d images are attached, numbered in the order attached (%s). "+
			"Each image shows a separate element of this %s, such as a character, an object, a product or a setting. Do not merge them into one subject.", n, imageNames(n), what),
		`- If the prompt says what an image is for (for example "image 1 is the main character, image 2 is the product"), follow the prompt.`,
		fmt.Sprintf("- If it does not, include every element shown and %s.", strings.Replace(keep, "its", "each one's", 1)),
		"- For how an element looks, its image overrides any description of it in the prompt.",
		"- Keep the art style the images share unless the prompt asks for another. Do not copy their backgrounds, framing or poses; take those from the prompt.",
	}
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

// buildMessage is the chat message sent for a job, in English like the setup
// message: one labelled line each for what to make, how to use the attached
// images (when there are any), and the ratio and duration asked
// (formatLines), the video guide (longVideoLines for several clips,
// shortVideoLines for one),
// then the user's prompt unchanged.
func buildMessage(job MuseJob) string {
	lines := []string{kindRequest[job.Kind]}
	if job.Kind == MuseJobImage && job.BoardFrames > 0 {
		lines = append([]string{"Task: Create the storyboard images of a video."}, boardImageLines(job)...)
	}
	if job.SourceVideo != "" {
		lines = append(lines, v2vLines(job)...)
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
	}
	lines = append(lines, formatLines(job)...)
	switch {
	case isLong(job):
		lines = append(lines, longVideoLines(job)...)
	case job.Kind == MuseJobVideo:
		lines = append(lines, shortVideoLines(job)...)
	}
	return strings.Join(lines, "\n") + "\nPrompt:\n" + job.Prompt
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
	acc, err := p.accounts.Record(accountID)
	if err != nil {
		return err
	}
	// The account's browser stays as it is (no sign-in window, no restart on
	// a new proxy, no removal) while this job works in it.
	release := p.accounts.Acquire(accountID)
	defer release()
	cfg := p.cfg.Get().Muse
	if job.Status == MuseJobQueued {
		started := false
		job, err = p.updateJob(jobID, func(j *MuseJob) {
			// The user may have cancelled the job since it was planned.
			if j.Status != MuseJobQueued {
				return
			}
			started = true
			resetForQueue(j)
			j.NextCheckAt = ""
			j.Status, j.AccountID, j.Step = MuseJobProcessing, accountID, "Kiểm tra đăng nhập"
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
	// Until the send, a failure (browser, proxy, page not ready) costs nothing
	// on Muse: it is marked notSent so it does not use up an attempt.
	page, err := p.pageFor(ctx, acc)
	if err != nil {
		return &notSentError{err: err}
	}
	defer closePage(page)
	if err := p.waitSignedIn(ctx, page, time.Duration(p.cfg.Get().Accounts.PageReadySeconds)*time.Second); err != nil {
		return err
	}

	if job.SessionID == "" {
		if err := p.checkStaleQuota(ctx, accountID, page, cfg); err != nil {
			return err
		}
		atts, err := p.attachments(ctx, job, acc, page)
		if err != nil {
			var input *inputError
			if !errors.As(err, &input) && ctx.Err() == nil {
				err = &notSentError{err: err}
			}
			return err
		}
		if job, err = p.send(ctx, job, acc, page, cfg, buildMessage(job), atts); err != nil {
			return err
		}
	}
	for {
		ended, err := p.poll(ctx, job, page, cfg)
		if err != nil {
			return err
		}
		err = p.collect(ctx, job, accountID, page, ended, cfg)
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
		if sendCtx.Err() != nil {
			return job, sendCtx.Err()
		}
		// Muse may have taken it: the chat is read again later and only a
		// newer reply counts.
		logger.Get().Warn("muse continue outcome unknown", "job", job.ID, "error", err)
		return job, fmt.Errorf("Không rõ Muse đã nhận lời nhắc làm tiếp chưa: %w", err)
	}
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

// send waits for the account's send slot and its send gap, then posts the
// message in a new chat. Only a definite answer from Muse comes back
// retryable; a lost answer is errSendUnknown.
//
// One send per account runs at a time, gap included: two jobs on one account
// (ThreadsPerAccount > 1) would otherwise read the same last send time and
// send together.
func (p *Provider) send(ctx context.Context, job MuseJob, acc accounts.Record, page Page, cfg config.MuseConfig, text string, atts []Attachment) (MuseJob, error) {
	release, err := p.sendSlot(ctx, acc.ID)
	if err != nil {
		return job, err
	}
	defer release()
	if fresh, err := p.accounts.Record(acc.ID); err == nil {
		acc = fresh
	}
	if last, err := time.Parse(time.RFC3339Nano, museView(acc).LastSentAt); err == nil {
		if wait := last.Add(p.gap(cfg)).Sub(p.now()); wait > 0 {
			p.updateJob(job.ID, func(j *MuseJob) {
				j.Step = fmt.Sprintf("Nghỉ ~%ds rồi mới gửi (tránh gửi dồn)", int(wait.Seconds()+0.5))
			})
			if err := p.sleep(ctx, wait); err != nil {
				return job, err
			}
		}
	}
	job, err = p.updateJob(job.ID, func(j *MuseJob) { j.Sending, j.Step = true, "Gửi lệnh" })
	if err != nil {
		return job, err
	}
	// Cancelling the job must not cut a send off halfway: Muse may already
	// have accepted it and would bill a generation nobody follows. The send
	// runs to its own timeout and is cancelled on Muse afterwards; only
	// stopping the app interrupts it (the job then fails as unknown on restart).
	sendCtx, stopSend := p.sendContext(ctx)
	defer stopSend()
	res, err := page.Send(sendCtx, text, atts)
	if err != nil {
		if sendCtx.Err() != nil {
			return job, sendCtx.Err()
		}
		logger.Get().Warn("muse send outcome unknown", "job", job.ID, "error", err)
		return job, errSendUnknown
	}
	if !res.OK && strings.HasPrefix(res.Reason, "not_ready") {
		// The page never found Muse's chat client, even after going home:
		// nothing left the app, so no attempt and no send gap are spent.
		p.updateJob(job.ID, func(j *MuseJob) { j.Sending = false })
		return job, &notSentError{err: errPageNotReady}
	}
	p.updateAccount(acc.ID, func(_ *accounts.Record, d *museData) { d.LastSentAt = p.now().UTC().Format(time.RFC3339Nano) })
	if res.OK && res.SessionID == "" {
		// Muse took the message but named no chat: it may be generating, and
		// without its chat the job can neither be read back nor safely re-sent.
		logger.Get().Warn("muse accepted a send without a session", "job", job.ID)
		return job, errSendUnknown
	}
	if !res.OK {
		p.updateJob(job.ID, func(j *MuseJob) { j.Sending = false })
		// A refusal is often a used-up quota; read it so the account rests.
		p.refreshQuota(sendCtx, acc.ID, page)
		return job, &rejectedSend{reason: res.Reason}
	}
	kept := false
	job, err = p.updateJob(job.ID, func(j *MuseJob) {
		// A job queued again meanwhile starts over without this chat. A
		// cancelled one keeps it, so a bulk retry knows Muse took it; either
		// way the chat is cancelled on Muse below.
		if j.Status == MuseJobQueued {
			return
		}
		j.SessionID, j.Sending = res.SessionID, false
		j.SentAt = p.now().UTC().Format(time.RFC3339)
		if j.Status == MuseJobProcessing {
			kept = true
			j.Step = "Muse đang làm"
		}
	})
	if ctx.Err() != nil || (err == nil && !kept) {
		// Cancelled while sending: stop the generation Muse just started.
		_ = page.Cancel(sendCtx, res.SessionID)
		return job, context.Canceled
	}
	return job, err
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

// poll waits until the session has ended and returns how it ended
// (sessionCompleted or sessionInterrupted). While its state is unknown (not
// listed, or a state Muse has not been seen to use) the chat itself is read:
// a file of the job's kind means it is done. The time limit counts from the
// send; at the limit the chat is read once more before Muse is asked to stop.
func (p *Provider) poll(ctx context.Context, job MuseJob, page Page, cfg config.MuseConfig) (string, error) {
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
	for errorsInRow := 0; ; {
		if err := p.sleep(ctx, time.Duration(cfg.PollIntervalMS)*time.Millisecond); err != nil {
			return "", err
		}
		status, err := page.Status(ctx, job.SessionID)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if errorsInRow++; errorsInRow >= cfg.PollErrorLimit {
				return "", fmt.Errorf("Không đọc được trạng thái phiên Muse: %w", err)
			}
			status = sessionRunning
		} else {
			errorsInRow = 0
			noteStatus(status)
		}
		if status == sessionCompleted || status == sessionInterrupted {
			// Right after a continue the chat may still show the last turn's
			// end: only a newer reply ends this one.
			if job.RepliesSeen == 0 || p.newReply(ctx, job, page) {
				return status, nil
			}
			status = sessionRunning
		}
		overdue := p.now().Sub(sent) > limit
		if overdue && job.RepliesSeen > 0 && !p.newReply(ctx, job, page) {
			// The continue got no answer in time. Sending the video again in a
			// new chat would pay for its clips twice: it fails instead.
			return "", &longIncompleteError{continues: job.Continues, detail: "Muse không trả lời lời nhắc làm tiếp"}
		}
		if (status != sessionRunning || overdue) && p.chatHasResult(ctx, job, page) {
			return sessionCompleted, nil
		}
		if overdue {
			if err := page.Cancel(ctx, job.SessionID); err != nil {
				if ctx.Err() != nil {
					return "", ctx.Err()
				}
				// Muse may still be making the file: check the chat again
				// later rather than send a second generation.
				return "", fmt.Errorf("Quá %d phút Muse chưa làm xong và không dừng được phiên: %w", int(limit.Minutes()), err)
			}
			return "", &museError{msg: fmt.Sprintf("Quá %d phút Muse chưa làm xong.", int(limit.Minutes()))}
		}
	}
}

// chatHasResult reports whether the chat already holds a file of the job's
// kind. A long video's chat holds its clips long before the joined file, so
// for it only a status line saying it is done counts.
func (p *Provider) chatHasResult(ctx context.Context, job MuseJob, page Page) bool {
	events, err := page.Result(ctx, job.SessionID)
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
func (p *Provider) newReply(ctx context.Context, job MuseJob, page Page) bool {
	events, err := page.Result(ctx, job.SessionID)
	return err == nil && len(events) > job.RepliesSeen
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
	replies, err := page.Result(ctx, job.SessionID)
	if err != nil {
		return err
	}
	wanted, missing := wantedMedia(replies, job.Kind)
	var long longStatus
	if isLong(job) {
		if long, err = p.longOutcome(ctx, job, accountID, page, replies); err != nil {
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
		return p.noResult(ctx, accountID, page, ended, replies, cfg)
	}
	p.updateJob(job.ID, func(j *MuseJob) { j.Step = "Tải file" })
	files := make([]string, 0, len(wanted))
	for k, m := range wanted {
		url, err := p.download(ctx, page, m, fmt.Sprintf("muse-%s-%d", job.ID, k+1))
		if err != nil {
			return err
		}
		files = append(files, url)
	}
	if isLong(job) && long.State == "" && p.tooShort(ctx, job, files[0]) {
		// No status line and only a clip so far: Muse may still be joining
		// (it has ended a turn early with one clip and sent the video later).
		return fmt.Errorf("Muse chưa giao đủ video %d giây", job.Duration)
	}
	saved := p.saveOutputs(job, files)
	note := p.measureNote(ctx, job, files)
	succeeded := false
	done, err := p.jobs.update(job.ID, func(j *MuseJob) {
		if j.Status != MuseJobProcessing {
			return // cancelled meanwhile: keep the user's decision
		}
		j.Status, j.Files, j.Error, j.SavedFiles, j.FormatNote = MuseJobSucceeded, files, "", saved, note
		j.ChangeNote = long.Changed
		j.Step = fmt.Sprintf("Đã tải %d file", len(files))
		j.FinishedAt = p.now().UTC().Format(time.RFC3339)
		succeeded = true
	})
	if err != nil {
		logger.Get().Warn("cannot save muse job", "job", job.ID, "error", err)
	} else {
		// The library is filled before the frontend hears of the success,
		// since that event is when it reloads the library.
		if succeeded {
			p.addToLibrary(done)
		}
		p.emit(events.EventMuseJobUpdated, done)
		if succeeded && done.BoardFrames > 0 && done.Kind == MuseJobImage {
			p.releaseAfter(done)
		}
	}
	p.refreshQuotaLater(accountID, page)
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

// tooShort reports whether a downloaded video is clearly shorter than the
// job asked for. A file that cannot be measured is not judged.
func (p *Provider) tooShort(ctx context.Context, job MuseJob, file string) bool {
	path, err := p.media.Path(file)
	if err != nil {
		return false
	}
	meta, err := media.ProbeVideo(ctx, path, p.cfg.Get().Flow)
	return err == nil && meta.DurationSeconds > 0 && meta.DurationSeconds < float64(job.Duration)-durationTolerance
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
// waits for it).
func (p *Provider) refreshQuotaLater(accountID string, page Page) {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		defer logger.Recover("cập nhật hạn mức Muse")
		p.refreshQuota(p.lifetime(), accountID, page)
	}()
}

// noResult explains a chat whose session ended without a file of the job's
// kind, from what Muse reports rather than from its wording:
//   - an interrupted run is Muse's own failure: another chat may work;
//   - a completed run with the account's usage gone (Muse's subscription,
//     read now) is a used-up account: the job moves to another one;
//   - any other completed run is Muse declining the prompt.
func (p *Provider) noResult(ctx context.Context, accountID string, page Page, ended string, replies []AssistantEvent, cfg config.MuseConfig) error {
	reply := replyText(replies)
	if ended == sessionInterrupted {
		msg := "Muse dừng giữa chừng, không có kết quả."
		if reply != "" {
			msg += " Muse trả lời: \"" + reply + "\""
		}
		return &museError{msg: msg}
	}
	p.refreshQuota(ctx, accountID, page)
	if p.accountPaused(accountID, cfg) {
		return &usageError{}
	}
	return &policyError{reply: reply}
}

// download stores one result file. Muse reports each file's size, which is
// both the expected length and the limit. A file whose size Muse left out is
// read whole (the page reads the file's own size) under the app's download cap.
func (p *Provider) download(ctx context.Context, page Page, m ResultMedia, id string) (string, error) {
	limit := m.ByteLen
	if limit <= 0 {
		limit = p.cfg.Get().Accounts.MaxDownloadBytes
	}
	reader, writer := io.Pipe()
	defer reader.Close()
	go func() {
		defer logger.Recover("tải file Muse", func(err error) { writer.CloseWithError(err) })
		n, err := page.Download(ctx, m.Path, writer)
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

// attach reads the job's reference images and checks them against caps.
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
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, &inputError{msg: "Không đọc được ảnh tham chiếu (có thể đã bị xoá): " + filepath.Base(path)}
		}
		if int64(len(data)) > caps.MaxFileBytes {
			return nil, &inputError{msg: fmt.Sprintf("%s vượt %d MB, giới hạn một file của Muse.", filepath.Base(path), caps.MaxFileBytes>>20)}
		}
		atts = append(atts, Attachment{Filename: fmt.Sprintf("image_%d%s", k+1, ext), MimeType: mime, DataBase64: base64.StdEncoding.EncodeToString(data), Category: "image"})
	}
	return atts, nil
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

// waitSignedIn polls the page until it reports a signed-in Muse session. A
// page still loading reports "not signed in" or fails, so it keeps polling. A
// signed-in tab on another route (no chat client) is sent home once. Only a
// session Muse refuses (an HTTP answer other than 200) means signed out; a
// page that never got ready is notSent, so the job waits without cost.
func (p *Provider) waitSignedIn(ctx context.Context, page Page, timeout time.Duration) error {
	deadline := p.now().Add(timeout)
	wentHome := false
	var last LoginState
	var lastErr error
	for {
		state, err := page.Login(ctx)
		if err == nil && state.LoggedIn {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		last, lastErr = state, err
		if err == nil && state.HTTP == 200 && !state.Chat && !wentHome {
			wentHome = true
			if err := page.Home(ctx); err != nil && ctx.Err() != nil {
				return ctx.Err()
			}
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
	}
	return &notSentError{err: errPageNotReady}
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
		rejected   *rejectedSend
		input      *inputError
		muse       *museError
		notSent    *notSentError
		incomplete *longIncompleteError
		waitAcc    *waitAccountError
	)
	switch {
	case errors.Is(err, errNotSignedIn):
		p.updateAccount(accountID, func(r *accounts.Record, _ *museData) {
			r.Status, r.Message = accounts.StatusSignedOut, "Phiên Muse đã đăng xuất; hãy mở Chrome để đăng nhập lại."
		})
		if hadSession {
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
	case errors.As(err, &notSent):
		at := wait(notSentWait)
		if hadSession {
			log("page not ready", "re-check later", err)
			p.updateJob(jobID, func(j *MuseJob) {
				if j.Status == MuseJobProcessing {
					j.Step, j.Error = "Kiểm tra lại lúc "+at, err.Error()
				}
			})
			return
		}
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
			j.SendUnknown = true
		})
	case errors.As(err, &usage):
		p.restAccount(accountID, cfg)
		handOn("usage", fmt.Sprintf("%s hết lượt Muse, chờ tài khoản khác", accName))
	case errors.As(err, &rejected) && p.accountPaused(accountID, cfg):
		handOn("usage", fmt.Sprintf("%s hết lượt Muse, chờ tài khoản khác", accName))
	case hadSession && !errors.As(err, &muse):
		// Muse already accepted this job: read it back again rather than fail
		// it, because the obvious next step (Retry) would pay a second time.
		if job.Rechecks+1 >= cfg.MaxRechecks {
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
		policy   *policyError
		rejected *rejectedSend
		input    *inputError
		muse     *museError
	)
	switch {
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
	restQuota = "quota"
	restLimit = "limit"
)

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

// checkStaleQuota reads the quota again before a send when the stored one
// says the weekly usage is gone (its rest ran out, so it is stale). Still used
// up, the account rests again and the job goes back without an attempt.
func (p *Provider) checkStaleQuota(ctx context.Context, accountID string, page Page, cfg config.MuseConfig) error {
	acc, err := p.accounts.Record(accountID)
	if err != nil || !museView(acc).Quota.weeklyOut() {
		return nil
	}
	p.refreshQuota(ctx, accountID, page)
	if p.accountPaused(accountID, cfg) {
		return &notSentError{err: errAccountResting}
	}
	return nil
}

// errAccountResting means the account's quota is still used up.
var errAccountResting = errors.New("Tài khoản vẫn hết lượt Muse.")

// refreshQuota reads the account's quota from its page and stores it.
func (p *Provider) refreshQuota(ctx context.Context, accountID string, page Page) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	q, err := page.Quota(ctx)
	if err != nil || q == nil {
		if !errors.Is(err, context.Canceled) {
			logger.Get().Warn("cannot refresh muse quota", "account", accountID, "error", err, "empty", q == nil)
		}
		return
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
	if job.OutputDir == "" || len(files) == 0 {
		return nil
	}
	var saved []string
	for _, fileURL := range files {
		if current, ok := p.jobs.get(job.ID); !ok || current.Status != MuseJobProcessing {
			return saved
		}
		path, err := p.media.SaveTo(fileURL, job.OutputDir, media.OutputName(job.Index, "muse"))
		if err != nil {
			logger.Get().Warn("muse save failed", "job", job.ID, "dir", job.OutputDir, "error", err)
			continue
		}
		saved = append(saved, path)
	}
	return saved
}
