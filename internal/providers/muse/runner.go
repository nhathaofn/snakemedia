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
	"strings"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/logger"
)

// kindRequest is how a message asks Muse's agent for each kind. Muse's media
// tools take only a prompt and images (no ratio, duration or count
// parameters), so everything else stays in the user's prompt, unchanged.
var kindRequest = map[MuseJobKind]string{
	MuseJobImage: "Tạo ảnh",
	MuseJobVideo: "Tạo video",
}

// kindMime is the result MIME prefix each kind collects.
var kindMime = map[MuseJobKind]string{MuseJobImage: "image/", MuseJobVideo: "video/"}

// kindName words a kind in messages to the user.
var kindName = map[MuseJobKind]string{MuseJobImage: "ảnh", MuseJobVideo: "video"}

// buildMessage is the chat message sent for a job: what to make, then the
// user's prompt unchanged.
func buildMessage(job MuseJob) string {
	return kindRequest[job.Kind] + "\n" + job.Prompt
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
	err := p.execute(ctx, jobID, accountID)
	if err == nil || ctx.Err() != nil {
		return
	}
	p.recordFailure(jobID, accountID, err)
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
	if err := p.waitSignedIn(ctx, page, time.Duration(p.cfg.Get().Accounts.PageReadySeconds)*time.Second); err != nil {
		return err
	}

	if job.SessionID == "" {
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
	if err := p.poll(ctx, job, page, cfg); err != nil {
		return err
	}
	return p.collect(ctx, job, accountID, page)
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
	job, err = p.updateJob(job.ID, func(j *MuseJob) {
		j.SessionID, j.Sending = res.SessionID, false
		j.SentAt = p.now().UTC().Format(time.RFC3339)
		if j.Status == MuseJobProcessing {
			j.Step = "Muse đang làm"
		}
	})
	if ctx.Err() != nil {
		// Cancelled while sending: stop the generation Muse just started.
		_ = page.Cancel(sendCtx, res.SessionID)
		return job, ctx.Err()
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

// sessionRunning is the session state Muse reports while it works.
const sessionRunning = "running"

// sessionEnded lists the session states that mean Muse stopped working on a
// chat. Any other state, or a session not listed (yet), keeps the job waiting:
// reading a chat too early would find no file and send the job again.
// Only "idle" is confirmed from muse.ai; the others are the usual words for an
// ended run. Every distinct state is logged once ("muse session status seen"),
// so this table is corrected from the logs.
var sessionEnded = map[string]bool{
	"idle": true, "completed": true, "done": true, "finished": true,
	"failed": true, "error": true, "cancelled": true, "canceled": true, "stopped": true,
}

// statusLog remembers which session states were logged.
var statusLog = onceSet{seen: map[string]bool{}}

// noteStatus logs a session state the first time it is seen, with how the
// runner treats it, and reports whether it logged.
func noteStatus(status string) bool {
	if !statusLog.first(status) {
		return false
	}
	treated := "unknown"
	switch {
	case status == sessionRunning:
		treated = "running"
	case sessionEnded[status]:
		treated = "ended"
	}
	logger.Get().Info("muse session status seen", "status", status, "treated", treated)
	return true
}

// poll waits until the session has ended. While its state is unknown (not
// listed, or a state not in sessionEnded) the chat itself is read: files of the
// job's kind mean it is done. The time limit counts from the send; at the limit
// the chat is read once more before Muse is asked to stop.
func (p *Provider) poll(ctx context.Context, job MuseJob, page Page, cfg config.MuseConfig) error {
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
	// A fresh session may take a few seconds to be listed.
	const grace = 30 * time.Second
	for errorsInRow := 0; ; {
		if err := p.sleep(ctx, time.Duration(cfg.PollIntervalMS)*time.Millisecond); err != nil {
			return err
		}
		status, err := page.Status(ctx, job.SessionID)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errorsInRow++; errorsInRow >= cfg.PollErrorLimit {
				return fmt.Errorf("Không đọc được trạng thái phiên Muse: %w", err)
			}
			status = sessionRunning
		} else {
			errorsInRow = 0
			noteStatus(status)
		}
		if sessionEnded[status] {
			return nil
		}
		unknown := status != sessionRunning && p.now().Sub(pollStart) > grace
		overdue := p.now().Sub(sent) > limit
		if (unknown || overdue) && p.chatHasResult(ctx, job, page) {
			return nil
		}
		if overdue {
			if err := page.Cancel(ctx, job.SessionID); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				// Muse may still be making the file: check the chat again
				// later rather than send a second generation.
				return fmt.Errorf("Quá %d phút Muse chưa làm xong và không dừng được phiên: %w", int(limit.Minutes()), err)
			}
			return &museError{msg: fmt.Sprintf("Quá %d phút Muse chưa làm xong.", int(limit.Minutes()))}
		}
	}
}

// chatHasResult reports whether the chat already holds a file of the job's kind.
func (p *Provider) chatHasResult(ctx context.Context, job MuseJob, page Page) bool {
	events, err := page.Result(ctx, job.SessionID)
	if err != nil {
		return false
	}
	wanted, _ := wantedMedia(events, job.Kind)
	return len(wanted) > 0
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
func (p *Provider) collect(ctx context.Context, job MuseJob, accountID string, page Page) error {
	events, err := page.Result(ctx, job.SessionID)
	if err != nil {
		return err
	}
	wanted, missing := wantedMedia(events, job.Kind)
	if len(wanted) == 0 && missing {
		// Muse made the file but cannot hand it over right now: read the chat
		// again later instead of paying for a new one.
		return fmt.Errorf("Muse đã tạo %s nhưng chưa lấy được file", kindName[job.Kind])
	}
	if len(wanted) == 0 {
		err := classifyEvents(events, kindName[job.Kind])
		var usage *usageError
		if errors.As(err, &usage) {
			p.refreshQuota(ctx, accountID, page)
		}
		return err
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
	saved := p.saveOutputs(job, files)
	succeeded := false
	done, err := p.updateJob(job.ID, func(j *MuseJob) {
		if j.Status != MuseJobProcessing {
			return // cancelled meanwhile: keep the user's decision
		}
		j.Status, j.Files, j.Error, j.SavedFiles = MuseJobSucceeded, files, "", saved
		j.Step = fmt.Sprintf("Đã tải %d file", len(files))
		j.FinishedAt = p.now().UTC().Format(time.RFC3339)
		succeeded = true
	})
	if err == nil && succeeded {
		p.addToLibrary(done)
	}
	// The quota is read in the background (tracked, so Close waits for it).
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.refreshQuota(p.lifetime(), accountID, page)
	}()
	return nil
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

// capabilities returns what the account may send: the stored limits while
// they are recent, otherwise read from the page (fresh forces a read). A
// failed read falls back to the stored limits. read reports a page read.
func (p *Provider) capabilities(ctx context.Context, acc accounts.Record, page Page, fresh bool) (caps *MuseCapabilities, read bool, err error) {
	var data museData
	_ = accounts.DecodeData(acc, &data)
	stored := data.Capabilities
	if stored != nil && !fresh {
		if checked, err := time.Parse(time.RFC3339, stored.CheckedAt); err == nil && p.now().Sub(checked) < capabilitiesTTL {
			return stored, false, nil
		}
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
	if len(job.References) == 0 {
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
	if len(job.References) > caps.MaxFiles {
		return nil, &inputError{msg: fmt.Sprintf("Muse nhận tối đa %d file mỗi tin nhắn (tác vụ có %d ảnh).", caps.MaxFiles, len(job.References))}
	}
	atts := make([]Attachment, 0, len(job.References))
	for k, ref := range job.References {
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
		atts = append(atts, Attachment{Filename: fmt.Sprintf("anh_%d%s", k+1, ext), MimeType: mime, DataBase64: base64.StdEncoding.EncodeToString(data), Category: "image"})
	}
	return atts, nil
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
	// maxLimitReplies caps how often one job may be handed on after Muse
	// answered "limit" or "verify": each answer cost a chat turn.
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
//   - Muse's own failure (no file, too slow) retries in a new chat;
//   - a refused prompt, a bad input or an unknown send fails at once;
//   - an account that is signed out, out of quota or asked to verify hands
//     the job to another account without spending an attempt, a few times
//     at most.
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
			failed(fmt.Sprintf("Muse trả lời hết lượt hoặc cần xác minh %d lần cho tác vụ này (mỗi lần tốn một lượt chat). Kiểm tra hạn mức các tài khoản ở Trang chủ → Tài khoản rồi bấm Chạy lại.", n))
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
		policy   *policyError
		usage    *usageError
		verify   *verifyError
		rejected *rejectedSend
		input    *inputError
		muse     *museError
		notSent  *notSentError
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
	case errors.As(err, &policy), errors.As(err, &input), errors.Is(err, errSendUnknown):
		log(failureClass(err), "failed", nil)
		failed(err.Error())
	case errors.As(err, &verify):
		p.updateAccount(accountID, func(r *accounts.Record, _ *museData) {
			r.Status, r.Message = accounts.StatusError, "Muse yêu cầu xác minh tài khoản; mở Chrome của tài khoản để xác minh rồi bấm Kiểm tra."
		})
		handOn("verify", fmt.Sprintf("%s cần xác minh trên Muse, chờ tài khoản khác", accName))
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

// refreshQuota reads the account's quota from its page and stores it.
func (p *Provider) refreshQuota(ctx context.Context, accountID string, page Page) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	q, err := page.Quota(ctx)
	if err != nil || q == nil {
		return
	}
	p.updateAccount(accountID, func(_ *accounts.Record, d *museData) { d.Quota = q })
}

// outputDir is where finished files are copied: the configured folder, or
// the app's default.
func (p *Provider) outputDir() string {
	if dir := p.cfg.Get().Muse.OutputDir; dir != "" {
		return dir
	}
	return p.defaultOutputDir
}

// saveOutputs copies finished files into a dated folder under outputDir when
// AutoSave is on. A job cancelled meanwhile keeps the user's decision: its
// files stay in the media folder and are not copied out.
func (p *Provider) saveOutputs(job MuseJob, files []string) []string {
	if !p.cfg.Get().Muse.AutoSave || len(files) == 0 {
		return nil
	}
	dateDir := filepath.Join(p.outputDir(), p.now().Format("2006-01-02"))
	name := job.Label
	if name == "" {
		name = job.Prompt
	}
	slug := sanitizeSlug(name)

	var saved []string
	for k, fileURL := range files {
		if current, ok := p.jobs.get(job.ID); !ok || current.Status != MuseJobProcessing {
			return saved
		}
		path, err := p.media.SaveTo(fileURL, dateDir, fmt.Sprintf("%s-%d", slug, k+1))
		if err != nil {
			logger.Get().Warn("muse autosave failed", "job", job.ID, "dir", dateDir, "error", err)
			continue
		}
		saved = append(saved, path)
	}
	return saved
}

// sanitizeSlug turns the first line of s into a short file name.
func sanitizeSlug(s string) string {
	if idx := strings.IndexAny(s, "\r\n"); idx != -1 {
		s = s[:idx]
	}
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < 32 {
			b.WriteRune('-')
		} else {
			b.WriteRune(r)
		}
	}
	clean := b.String()
	for strings.Contains(clean, "--") {
		clean = strings.ReplaceAll(clean, "--", "-")
	}
	clean = strings.Trim(clean, "- .")
	if r := []rune(clean); len(r) > 60 {
		clean = strings.Trim(string(r[:60]), "- .")
	}
	if clean == "" {
		return "muse"
	}
	return clean
}
