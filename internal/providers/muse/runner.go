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

	"SnakeMedia/internal/config"
	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/media"
)

// RatioOption is one aspect ratio Muse understands, with the wording sent to it.
type RatioOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Ratios lists the aspect ratios offered for Muse jobs, in display order.
var Ratios = []RatioOption{
	{"9:16", "9:16 dọc"}, {"16:9", "16:9 ngang"}, {"1:1", "1:1 vuông"}, {"4:3", "4:3 ngang"}, {"3:4", "3:4 dọc"},
}

// imageTypes are the reference image formats Muse accepts.
var imageTypes = map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp"}

// resultExtensions name downloaded files whose type Muse reports.
var resultExtensions = map[string]string{"video/mp4": ".mp4", "image/webp": ".webp", "image/png": ".png", "image/jpeg": ".jpg"}

const v2vAnalysisPrompt = "Analyze this video in detail: camera angle, character motion, timing, and ambient audio style."
const v2vRemakePrefix = "Based on the camera movement, timing, and character actions from the video above, recreate the scene with the following specifications:"

// buildMessage is the chat message sent for a job: what to make, the ratio,
// then the user's prompt unchanged, followed by any reference manifest.
func buildMessage(job MuseJob) string {
	var head string
	if job.Kind == MuseJobV2V {
		head = "Generate photorealistic video"
		for _, r := range Ratios {
			if r.Value == job.Ratio {
				head += " · aspect ratio " + r.Value
			}
		}
		msg := head + "\n" + v2vRemakePrefix + "\n" + job.Prompt
		if job.Manifest != "" {
			msg += "\n\n" + job.Manifest
		}
		return msg
	}
	head = "Tạo video"
	if job.Kind == MuseJobImage {
		head = "Tạo ảnh"
		if job.Count > 1 {
			head += fmt.Sprintf(" · %d ảnh", job.Count)
		}
	}
	for _, r := range Ratios {
		if r.Value == job.Ratio {
			head += " · tỷ lệ " + r.Label
		}
	}
	msg := head + "\n" + job.Prompt
	if job.Manifest != "" {
		msg += "\n\n" + job.Manifest
	}
	return msg
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
	if !ok {
		return nil
	}
	acc, err := p.accounts.get(accountID)
	if err != nil {
		return err
	}
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
	page, err := p.pageFor(ctx, acc)
	if err != nil {
		return err
	}
	if signedIn, err := p.waitSignedIn(ctx, page, time.Duration(cfg.PageReadySeconds)*time.Second); err != nil {
		return err
	} else if !signedIn {
		return errNotSignedIn
	}

	if job.Kind == MuseJobV2V {
		return p.executeV2V(ctx, job, acc, page, cfg)
	}
	resumed := job.SessionID != ""
	if !resumed {
		if job, err = p.sendNew(ctx, job, acc, page, cfg); err != nil {
			return err
		}
	}
	if err := p.poll(ctx, job, page, cfg, resumed); err != nil {
		return err
	}
	return p.collect(ctx, job, accountID, page, cfg)
}

// executeV2V runs the two turns of a video-to-video job in one chat: Muse
// analyses the source video, then remakes it from the prompt and images.
func (p *Provider) executeV2V(ctx context.Context, job MuseJob, acc storedAccount, page Page, cfg config.MuseConfig) error {
	resumed := job.SessionID != ""
	if !resumed {
		video, err := p.sourceAttachment(job, cfg)
		if err != nil {
			return err
		}
		job, err = p.send(ctx, job, acc, page, cfg, v2vAnalysisPrompt, []Attachment{video}, "", "Chặng 1: gửi video nguồn để Muse phân tích")
		if err != nil {
			return err
		}
	}
	if job.V2VTurn < 2 {
		if err := p.poll(ctx, job, page, cfg, resumed); err != nil {
			return err
		}
		atts, err := p.attachments(job, cfg)
		if err != nil {
			return err
		}
		sent, err := p.send(ctx, job, acc, page, cfg, buildMessage(job), atts, job.SessionID, "Chặng 2: gửi ảnh và prompt để làm video mới")
		if err != nil {
			return err
		}
		job, resumed = sent, false
	}
	if err := p.poll(ctx, job, page, cfg, resumed); err != nil {
		return err
	}
	return p.collect(ctx, job, acc.ID, page, cfg)
}

// sendNew sends a regular job: into its storyboard chat when it continues one
// on this account, otherwise (or when that chat refuses) into a new chat.
func (p *Provider) sendNew(ctx context.Context, job MuseJob, acc storedAccount, page Page, cfg config.MuseConfig) (MuseJob, error) {
	atts, err := p.attachments(job, cfg)
	if err != nil {
		return job, err
	}
	text := buildMessage(job)
	if job.Thread != "" && (job.PinAccount == "" || job.PinAccount == acc.ID) {
		sent, err := p.send(ctx, job, acc, page, cfg, text, atts, job.Thread, "Gửi lệnh vào chat storyboard")
		var rejected *rejectedSend
		if !errors.As(err, &rejected) {
			return sent, err
		}
		p.updateJob(job.ID, func(j *MuseJob) {
			j.ThreadError = "Không gửi được vào chat storyboard (" + rejected.reason + "), làm ở chat mới"
		})
	}
	return p.send(ctx, job, acc, page, cfg, text, atts, "", "Gửi lệnh")
}

// send waits out the account's send gap, then posts one message. Only a
// definite answer from Muse comes back retryable; a lost answer is
// errSendUnknown. A message into a thread keeps that thread's session.
func (p *Provider) send(ctx context.Context, job MuseJob, acc storedAccount, page Page, cfg config.MuseConfig, text string, atts []Attachment, thread, step string) (MuseJob, error) {
	if fresh, err := p.accounts.get(acc.ID); err == nil {
		acc = fresh
	}
	if last, err := time.Parse(time.RFC3339Nano, acc.LastSentAt); err == nil {
		if wait := last.Add(p.gap(cfg)).Sub(p.now()); wait > 0 {
			p.updateJob(job.ID, func(j *MuseJob) {
				j.Step = fmt.Sprintf("Nghỉ ~%ds rồi mới gửi (tránh gửi dồn)", int(wait.Seconds()+0.5))
			})
			if err := p.sleep(ctx, wait); err != nil {
				return job, err
			}
		}
	}
	// A message into an existing chat lands after earlier replies and files;
	// remember them so collect only takes what this message produces.
	var base int
	var seen []string
	if thread != "" {
		history, err := page.Result(ctx, thread)
		if err != nil {
			return job, err
		}
		base, seen = len(history), mediaPaths(history)
	}
	job, err := p.updateJob(job.ID, func(j *MuseJob) {
		j.Sending, j.Step, j.HistoryBase, j.SeenPaths = true, step, base, seen
	})
	if err != nil {
		return job, err
	}
	// Cancelling the job must not cut a send off halfway: Muse may already
	// have accepted it and would bill a generation nobody follows. The send
	// runs to its own timeout and is cancelled on Muse afterwards; only
	// stopping the app interrupts it (the job then fails as unknown on restart).
	sendCtx, stopSend := p.sendContext(ctx)
	defer stopSend()
	res, err := page.Send(sendCtx, text, atts, thread)
	if err != nil {
		if sendCtx.Err() != nil {
			return job, sendCtx.Err()
		}
		logger.Get().Warn("muse send outcome unknown", "job", job.ID, "error", err)
		return job, errSendUnknown
	}
	p.updateAccount(acc.ID, func(a *storedAccount) { a.LastSentAt = p.now().UTC().Format(time.RFC3339Nano) })
	if !res.OK || res.SessionID == "" {
		p.updateJob(job.ID, func(j *MuseJob) { j.Sending = false })
		// A refusal is often a used-up quota; read it so the account rests.
		p.refreshQuota(sendCtx, acc.ID, page)
		return job, &rejectedSend{reason: res.Reason}
	}
	job, err = p.updateJob(job.ID, func(j *MuseJob) {
		j.SessionID, j.Sending = res.SessionID, false
		if j.Kind == MuseJobV2V {
			j.V2VTurn++
		}
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

// mediaPaths lists every file path in a chat history.
func mediaPaths(events []AssistantEvent) []string {
	var paths []string
	for _, e := range events {
		for _, m := range append(append([]ResultMedia(nil), e.Videos...), e.Images...) {
			if m.Path != "" {
				paths = append(paths, m.Path)
			}
		}
	}
	return paths
}

// poll waits until the session stops running. A fresh session may take a few
// seconds to be listed, so "not running" counts only after it was seen running
// or after a grace period.
func (p *Provider) poll(ctx context.Context, job MuseJob, page Page, cfg config.MuseConfig, resumed bool) error {
	limit := time.Duration(cfg.VideoTimeoutMinutes) * time.Minute
	if job.Kind == MuseJobImage {
		limit = time.Duration(cfg.ImageTimeoutMinutes) * time.Minute
	}
	started, err := time.Parse(time.RFC3339, job.StartedAt)
	if err != nil {
		started = p.now()
	}
	sentAt := p.now()
	seen := resumed
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
			status = "running"
		} else {
			errorsInRow = 0
		}
		if status == "running" {
			seen = true
		} else if seen || p.now().Sub(sentAt) > grace {
			return nil
		}
		if p.now().Sub(started) > limit {
			_ = page.Cancel(ctx, job.SessionID)
			return &museError{msg: fmt.Sprintf("Quá %d phút Muse chưa làm xong.", int(limit.Minutes()))}
		}
	}
}

// collect reads the finished session and downloads its files.
func (p *Provider) collect(ctx context.Context, job MuseJob, accountID string, page Page, cfg config.MuseConfig) error {
	events, err := page.Result(ctx, job.SessionID)
	if err != nil {
		return err
	}
	what, prefix := "video", "video/"
	if job.Kind == MuseJobImage {
		what, prefix = "ảnh", "image/"
	}
	// Only replies after this job's message count. History may be capped, so
	// files are also matched against what the chat already held.
	replies := events
	if job.HistoryBase > 0 && job.HistoryBase <= len(events) {
		replies = events[job.HistoryBase:]
	}
	seen := make(map[string]bool, len(job.SeenPaths))
	for _, path := range job.SeenPaths {
		seen[path] = true
	}
	var wanted []ResultMedia
	var texts []string
	for _, e := range replies {
		if e.Text != "" {
			texts = append(texts, e.Text)
		}
		for _, m := range append(append([]ResultMedia(nil), e.Videos...), e.Images...) {
			if !m.Missing && m.Path != "" && !seen[m.Path] && strings.HasPrefix(m.Mime, prefix) {
				wanted = append(wanted, m)
			}
		}
	}
	if job.Kind == MuseJobImage && len(wanted) > max(job.Count, 1) {
		wanted = wanted[:max(job.Count, 1)]
	}
	if len(wanted) == 0 {
		err := classifyReply(strings.Join(texts, "\n"), what)
		var usage *usageError
		if errors.As(err, &usage) {
			p.refreshQuota(ctx, accountID, page)
		}
		return err
	}
	p.updateJob(job.ID, func(j *MuseJob) { j.Step = "Tải file" })
	files := make([]string, 0, len(wanted))
	for k, m := range wanted {
		url, err := p.download(ctx, page, m, fmt.Sprintf("muse-%s-%d", job.ID, k+1), cfg.MaxDownloadBytes)
		if err != nil {
			return err
		}
		files = append(files, url)
	}
	if job.Kind == MuseJobV2V && job.KeepAudio {
		p.updateJob(job.ID, func(j *MuseJob) { j.Step = "Ghép âm thanh gốc" })
		for i, url := range files {
			if replaced, err := p.keepSourceAudio(ctx, job, url, i); err != nil {
				logger.Get().Warn("muse v2v audio not kept", "job", job.ID, "error", err)
				p.updateJob(job.ID, func(j *MuseJob) { j.ThreadError = "Không ghép được âm thanh gốc: " + err.Error() })
			} else {
				files[i] = replaced
			}
		}
	}
	saved := p.saveOutputs(job, files)
	p.updateJob(job.ID, func(j *MuseJob) {
		if j.Status != MuseJobProcessing {
			return // cancelled meanwhile: keep the user's decision
		}
		j.Status, j.Files, j.Error, j.SavedFiles = MuseJobSucceeded, files, "", saved
		j.Step = fmt.Sprintf("Đã tải %d file", len(files))
		j.FinishedAt = p.now().UTC().Format(time.RFC3339)
	})
	go p.refreshQuota(context.WithoutCancel(ctx), accountID, page)
	return nil
}

func (p *Provider) download(ctx context.Context, page Page, m ResultMedia, id string, limit int64) (string, error) {
	reader, writer := io.Pipe()
	defer reader.Close()
	go func() {
		n, err := page.Download(ctx, m.Path, writer)
		if err == nil && m.ByteLen > 0 && n != m.ByteLen {
			err = fmt.Errorf("Tải file lỗi: %d/%d byte", n, m.ByteLen)
		}
		writer.CloseWithError(err)
	}()
	ext := resultExtensions[m.Mime]
	if ext == "" {
		ext = ".bin"
	}
	return p.media.PutReader(id, m.Mime, ext, reader, limit)
}

// attachments loads the job's reference images in order.
func (p *Provider) attachments(job MuseJob, cfg config.MuseConfig) ([]Attachment, error) {
	atts := make([]Attachment, 0, len(job.References))
	var total int64
	for k, ref := range job.References {
		path, err := p.media.Path(ref)
		if err != nil {
			return nil, &inputError{msg: err.Error()}
		}
		mime, ok := imageTypes[strings.ToLower(filepath.Ext(path))]
		if !ok {
			return nil, &inputError{msg: "Ảnh tham chiếu không đúng định dạng: " + filepath.Base(path)}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, &inputError{msg: "Không đọc được ảnh tham chiếu (có thể đã bị xoá): " + filepath.Base(path)}
		}
		if total += int64(len(data)); total > cfg.MaxAttachmentBytes {
			return nil, &inputError{msg: fmt.Sprintf("Ảnh gửi kèm vượt %d MB (giới hạn của Muse). Bớt ảnh hoặc dùng ảnh nhỏ hơn.", cfg.MaxAttachmentBytes>>20)}
		}
		ext := strings.TrimPrefix(filepath.Ext(path), ".")
		atts = append(atts, Attachment{Filename: fmt.Sprintf("anh_%d.%s", k+1, ext), MimeType: mime, DataBase64: base64.StdEncoding.EncodeToString(data), Category: "image"})
	}
	return atts, nil
}

// waitSignedIn polls the page until it reports a signed-in Muse session. A
// page still loading reports "not signed in" or fails, so it keeps polling.
func (p *Provider) waitSignedIn(ctx context.Context, page Page, timeout time.Duration) (bool, error) {
	deadline := p.now().Add(timeout)
	for {
		state, err := page.Login(ctx)
		if err == nil && state.LoggedIn {
			return true, nil
		}
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if !p.now().Before(deadline) {
			return false, nil
		}
		if err := p.sleep(ctx, 3*time.Second); err != nil {
			return false, err
		}
	}
}

// recordFailure decides what happens to a job after err:
//   - a sent job with a transport error keeps its chat and is checked again;
//   - Muse's own failure (no file, too slow) retries in a new chat;
//   - a refused prompt, a bad input or an unknown send fails at once;
//   - an account that is signed out or out of quota hands the job to another
//     account without spending an attempt.
func (p *Provider) recordFailure(jobID, accountID string, err error) {
	job, ok := p.jobs.get(jobID)
	if !ok || job.Status != MuseJobProcessing {
		return
	}
	cfg := p.cfg.Get().Muse
	now := p.now().UTC().Format(time.RFC3339)
	accName := accountID
	if acc, e := p.accounts.get(accountID); e == nil {
		accName = acc.Name
	}
	failed := func(msg string) {
		p.updateJob(jobID, func(j *MuseJob) {
			j.Status, j.Error, j.Step, j.Sending, j.FinishedAt = MuseJobFailed, msg, "", false, now
		})
	}
	requeue := func(msg string, spend bool) {
		p.updateJob(jobID, func(j *MuseJob) {
			resetForQueue(j)
			j.Error = msg
			if !spend && j.Attempts > 0 {
				j.Attempts--
			}
		})
	}
	var (
		policy   *policyError
		usage    *usageError
		rejected *rejectedSend
		input    *inputError
		muse     *museError
	)
	switch {
	case errors.Is(err, errNotSignedIn):
		p.updateAccount(accountID, func(a *storedAccount) {
			a.Status, a.Message = MuseAccountSignedOut, "Phiên Muse đã đăng xuất; hãy mở Chrome để đăng nhập lại."
		})
		if job.SessionID != "" {
			p.updateJob(jobID, func(j *MuseJob) {
				j.Step, j.Error = "Chờ tài khoản đăng nhập lại để kiểm tra kết quả", err.Error()
			})
			return
		}
		requeue(fmt.Sprintf("%s chưa đăng nhập Muse", accName), false)
	case errors.As(err, &policy), errors.As(err, &input), errors.Is(err, errSendUnknown):
		failed(err.Error())
	case errors.As(err, &usage):
		p.restAccount(accountID, cfg)
		requeue(fmt.Sprintf("%s hết lượt Muse, chờ tài khoản khác", accName), false)
	case errors.As(err, &rejected) && p.accountPaused(accountID, cfg):
		requeue(fmt.Sprintf("%s hết lượt Muse, chờ tài khoản khác", accName), false)
	case job.SessionID != "" && !errors.As(err, &muse):
		// Muse already accepted this job: read it back again rather than fail
		// it, because the obvious next step (Retry) would pay a second time.
		if job.Rechecks+1 >= cfg.MaxRechecks {
			failed(fmt.Sprintf("Muse đã nhận lệnh nhưng app không đọc được kết quả sau %d lần (%v). Mở chat trên Muse để lấy kết quả trước khi chạy lại, vì chạy lại sẽ tốn thêm 1 lượt.", cfg.MaxRechecks, err))
			return
		}
		p.updateJob(jobID, func(j *MuseJob) {
			j.Rechecks++
			j.Error = fmt.Sprintf("Lỗi lần %d, đang kiểm tra lại phiên: %v", j.Rechecks, err)
		})
	case job.Attempts >= cfg.MaxAttempts:
		failed(err.Error())
	default:
		requeue(fmt.Sprintf("Lỗi lần %d, đang thử lại: %v", job.Attempts, err), true)
	}
}

// restAccount pauses an account Muse said is out of usage, until its quota
// resets when known, otherwise for UsagePauseMinutes.
func (p *Provider) restAccount(accountID string, cfg config.MuseConfig) {
	until := p.now().Add(time.Duration(cfg.UsagePauseMinutes) * time.Minute)
	if acc, err := p.accounts.get(accountID); err == nil {
		if reset := acc.Quota.resetsAt(); reset.After(p.now()) {
			until = reset
		}
	}
	p.updateAccount(accountID, func(a *storedAccount) {
		a.PausedUntil = until.UTC().Format(time.RFC3339)
		a.Message = "Muse báo hết lượt; tạm nghỉ tới " + until.Local().Format("15:04 02/01")
	})
}

// accountPaused refreshes nothing: it reads the stored quota and pause.
func (p *Provider) accountPaused(accountID string, cfg config.MuseConfig) bool {
	acc, err := p.accounts.get(accountID)
	return err == nil && !pausedUntil(acc.MuseAccount, cfg, p.now()).IsZero()
}

// refreshQuota reads the account's quota from its page and stores it.
func (p *Provider) refreshQuota(ctx context.Context, accountID string, page Page) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	q, err := page.Quota(ctx)
	if err != nil || q == nil {
		return
	}
	p.updateAccount(accountID, func(a *storedAccount) { a.Quota = q })
}

// sourceAttachment loads the job's source video as an attachment for V2V analysis.
func (p *Provider) sourceAttachment(job MuseJob, cfg config.MuseConfig) (Attachment, error) {
	if job.SourceVideo == "" {
		return Attachment{}, &inputError{msg: "Chưa chọn video nguồn cho tác vụ V2V"}
	}
	path, err := p.media.Path(job.SourceVideo)
	if err != nil {
		return Attachment{}, &inputError{msg: err.Error()}
	}
	ext := strings.ToLower(filepath.Ext(path))
	mime, ok := media.VideoTypes[ext]
	if !ok {
		return Attachment{}, &inputError{msg: "Video nguồn không đúng định dạng: " + filepath.Base(path)}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Attachment{}, &inputError{msg: "Không đọc được video nguồn (có thể đã bị xoá): " + filepath.Base(path)}
	}
	if cfg.MaxSourceVideoBytes > 0 && int64(len(data)) > cfg.MaxSourceVideoBytes {
		return Attachment{}, &inputError{msg: fmt.Sprintf("Video gửi kèm vượt %d MB (giới hạn của Muse). Chọn video nhỏ hơn.", cfg.MaxSourceVideoBytes>>20)}
	}
	return Attachment{
		Filename:   filepath.Base(path),
		MimeType:   mime,
		DataBase64: base64.StdEncoding.EncodeToString(data),
		Category:   "video",
	}, nil
}

// keepSourceAudio extracts the audio from job.SourceVideo and replaces the audio
// track of the generated video at url, saving the result as a new media file.
func (p *Provider) keepSourceAudio(ctx context.Context, job MuseJob, url string, i int) (string, error) {
	if job.SourceVideo == "" {
		return url, errors.New("không có video nguồn để lấy âm thanh")
	}
	srcPath, err := p.media.Path(job.SourceVideo)
	if err != nil {
		return url, fmt.Errorf("không tìm thấy file video nguồn: %w", err)
	}
	videoPath, err := p.media.Path(url)
	if err != nil {
		return url, fmt.Errorf("không tìm thấy file video đã tạo: %w", err)
	}

	cfg := p.cfg.Get().Muse
	ffmpegExe, err := findFFmpeg(cfg.FFmpegPath)
	if err != nil {
		return url, err
	}

	tmpOut, err := os.CreateTemp("", "muse-v2v-mux-*.mp4")
	if err != nil {
		return url, err
	}
	tmpOutPath := tmpOut.Name()
	_ = tmpOut.Close()
	defer os.Remove(tmpOutPath)

	muxFn := p.muxAudio
	if muxFn == nil {
		muxFn = defaultMuxAudio
	}
	if err := muxFn(ctx, ffmpegExe, videoPath, srcPath, tmpOutPath); err != nil {
		return url, err
	}

	data, err := os.ReadFile(tmpOutPath)
	if err != nil {
		return url, fmt.Errorf("đọc video sau khi ghép âm thanh lỗi: %w", err)
	}
	return p.media.Put(fmt.Sprintf("muse-v2v-%s-%d", job.ID, i+1), "video/mp4", ".mp4", data)
}

func defaultMuxAudio(ctx context.Context, ffmpegExe, videoPath, sourcePath, outPath string) error {
	tmpAudio, err := os.CreateTemp("", "muse-v2v-aud-*.aac")
	if err != nil {
		return err
	}
	tmpAudioPath := tmpAudio.Name()
	_ = tmpAudio.Close()
	defer os.Remove(tmpAudioPath)

	if err := extractAudio(ctx, ffmpegExe, sourcePath, tmpAudioPath); err != nil {
		return fmt.Errorf("trích âm thanh gốc lỗi: %w", err)
	}
	if err := replaceAudio(ctx, ffmpegExe, videoPath, tmpAudioPath, outPath); err != nil {
		return fmt.Errorf("ghép âm thanh gốc lỗi: %w", err)
	}
	return nil
}

// saveOutputs copies successful output files to OutputDir if AutoSave is enabled.
func (p *Provider) saveOutputs(job MuseJob, files []string) []string {
	cfg := p.cfg.Get().Muse
	if !cfg.AutoSave || len(files) == 0 {
		return nil
	}
	outDir := cfg.OutputDir
	if outDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			logger.Get().Warn("muse autosave: cannot find home directory", "error", err)
			return nil
		}
		outDir = filepath.Join(home, "Videos", "SnakeMedia Muse")
	}
	dateDir := filepath.Join(outDir, p.now().Format("2006-01-02"))
	if err := os.MkdirAll(dateDir, 0o755); err != nil {
		logger.Get().Warn("muse autosave: mkdir failed", "dir", dateDir, "error", err)
		return nil
	}

	name := job.Label
	if name == "" {
		name = job.Prompt
	}
	slug := sanitizeSlug(name)

	var saved []string
	for k, fileURL := range files {
		srcPath, err := p.media.Path(fileURL)
		if err != nil {
			logger.Get().Warn("muse autosave: media path failed", "url", fileURL, "error", err)
			continue
		}
		ext := filepath.Ext(srcPath)
		baseName := fmt.Sprintf("%s-%d", slug, k+1)
		destPath := filepath.Join(dateDir, baseName+ext)
		for suffix := 2; fileExists(destPath); suffix++ {
			destPath = filepath.Join(dateDir, fmt.Sprintf("%s-%d%s", baseName, suffix, ext))
		}

		data, err := os.ReadFile(srcPath)
		if err != nil {
			logger.Get().Warn("muse autosave: read src failed", "path", srcPath, "error", err)
			continue
		}
		if err := os.WriteFile(destPath, data, 0o644); err != nil {
			logger.Get().Warn("muse autosave: write dest failed", "path", destPath, "error", err)
			continue
		}
		saved = append(saved, destPath)
	}
	return saved
}

func sanitizeSlug(s string) string {
	if idx := strings.IndexAny(s, "\r\n"); idx != -1 {
		s = s[:idx]
	}
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' || r < 32 {
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
	if len(clean) > 60 {
		clean = strings.TrimSpace(clean[:60])
		clean = strings.Trim(clean, "- .")
	}
	if clean == "" {
		return "muse"
	}
	return clean
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
