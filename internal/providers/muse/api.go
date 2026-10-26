package muse

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"SnakeMedia/internal/events"
	"SnakeMedia/internal/media"
	"SnakeMedia/internal/models"
	"github.com/google/uuid"
)

// Limits shared with the frontend through GetOptions.
const (
	MaxImagesPerJob  = 4
	MaxReferences    = 8
	MaxPromptRunes   = 8000
	MaxPromptsPerRun = 200
	MaxAccountName   = 60
)

// MuseOptions are the choices and limits the create screen offers.
type MuseOptions struct {
	Ratios             []RatioOption `json:"ratios"`
	MaxImagesPerJob    int           `json:"maxImagesPerJob"`
	MaxReferences      int           `json:"maxReferences"`
	MaxPromptLength    int           `json:"maxPromptLength"`
	MaxPromptsPerRun   int           `json:"maxPromptsPerRun"`
	MaxAttachmentBytes int64         `json:"maxAttachmentBytes"`
	ImageExtensions    []string      `json:"imageExtensions"`
	Voices             []VoiceOption `json:"voices"`
}

// MuseJobRequest submits one job per prompt with shared settings.
type MuseJobRequest struct {
	Kind        MuseJobKind `json:"kind"`
	Prompts     []string    `json:"prompts"`
	Ratio       string      `json:"ratio"`
	Count       int         `json:"count"`
	References  []string    `json:"references"`
	SourceVideo string      `json:"sourceVideo"`
	KeepAudio   bool        `json:"keepAudio"`
	Held        bool        `json:"held"`
	ProjectID   string      `json:"projectId"`
	Label       string      `json:"label"`
	PairMode    bool        `json:"pairMode"`
}

// MuseReference is an image imported for use as a reference.
type MuseReference struct {
	LocalURL string `json:"localUrl"`
	FileName string `json:"fileName"`
	Size     int64  `json:"size"`
}

func fail[T any](err error, code models.Code) models.Response[T] {
	return models.ErrorResponse[T](err.Error(), code)
}

// GetOptions returns the ratios and limits for the create screen.
func (p *Provider) GetOptions() models.Response[MuseOptions] {
	exts := make([]string, 0, len(imageTypes))
	for ext := range imageTypes {
		exts = append(exts, ext)
	}
	slices.Sort(exts)
	return models.SuccessResponse(MuseOptions{
		Ratios: Ratios, MaxImagesPerJob: MaxImagesPerJob, MaxReferences: MaxReferences,
		MaxPromptLength: MaxPromptRunes, MaxPromptsPerRun: MaxPromptsPerRun,
		MaxAttachmentBytes: p.cfg.Get().Muse.MaxAttachmentBytes, ImageExtensions: exts,
		Voices: AllVoices,
	})
}

// ---------- Accounts ----------

func (p *Provider) GetAccounts() models.Response[[]MuseAccount] {
	list, err := p.publicAccounts()
	if err != nil {
		return fail[[]MuseAccount](err, models.CodeInternal)
	}
	return models.SuccessResponse(list)
}

func cleanName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "", errors.New("Hãy đặt tên cho tài khoản.")
	}
	if utf8.RuneCountInString(name) > MaxAccountName {
		return "", fmt.Errorf("Tên tài khoản tối đa %d ký tự.", MaxAccountName)
	}
	return name, nil
}

func (p *Provider) newAccount(name string, source MuseAccountSource) storedAccount {
	id := uuid.NewString()
	return storedAccount{MuseAccount: MuseAccount{
		ID: id, Name: name, Source: source, ProfileDir: filepath.Join(p.profileRoot, id),
		Status: MuseAccountUnverified, Enabled: true,
	}}
}

// AddAccount saves a new account and opens Chrome on muse.ai for the user to
// sign in; a watcher marks it signed in once the page has a session.
func (p *Provider) AddAccount(ctx context.Context, name string) models.Response[MuseAccount] {
	name, err := cleanName(name)
	if err != nil {
		return fail[MuseAccount](err, models.CodeInvalidArgument)
	}
	acc := p.newAccount(name, MuseSourceBrowser)
	acc.Status, acc.Message = MuseAccountSigningIn, "Đăng nhập muse.ai trong cửa sổ Chrome vừa mở."
	if err := p.accounts.add(acc); err != nil {
		return fail[MuseAccount](err, models.CodeInternal)
	}
	p.emitAccounts()
	if err := p.startSignIn(ctx, acc); err != nil {
		p.updateAccount(acc.ID, func(a *storedAccount) { a.Status, a.Message = MuseAccountError, err.Error() })
		return fail[MuseAccount](err, models.CodeUnavailable)
	}
	return models.SuccessResponse(acc.MuseAccount)
}

// ImportCookies saves an account from cookies exported on a signed-in muse.ai
// page, then checks the session in the background.
func (p *Provider) ImportCookies(name, raw string) models.Response[MuseAccount] {
	name, err := cleanName(name)
	if err != nil {
		return fail[MuseAccount](err, models.CodeInvalidArgument)
	}
	cookies, err := ParseCookies(raw)
	if err != nil {
		return fail[MuseAccount](err, models.CodeInvalidArgument)
	}
	acc := p.newAccount(name, MuseSourceCookies)
	acc.Cookies, acc.CookieVersion = cookies, uuid.NewString()
	acc.Message = "Đang kiểm tra cookie…"
	if err := p.accounts.add(acc); err != nil {
		return fail[MuseAccount](err, models.CodeInternal)
	}
	p.emitAccounts()
	go p.CheckAccount(p.lifetime(), acc.ID)
	return models.SuccessResponse(acc.MuseAccount)
}

// ReplaceCookies stores a new cookie export for an existing account.
func (p *Provider) ReplaceCookies(id, raw string) models.Response[MuseAccount] {
	cookies, err := ParseCookies(raw)
	if err != nil {
		return fail[MuseAccount](err, models.CodeInvalidArgument)
	}
	acc, err := p.updateAccount(id, func(a *storedAccount) {
		a.Cookies, a.CookieVersion, a.Source = cookies, uuid.NewString(), MuseSourceCookies
		a.Status, a.Message = MuseAccountUnverified, "Đang kiểm tra cookie…"
	})
	if err != nil {
		return fail[MuseAccount](err, models.CodeNotFound)
	}
	go p.CheckAccount(p.lifetime(), id)
	return models.SuccessResponse(acc)
}

func (p *Provider) lifetime() context.Context {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ctx == nil {
		return context.Background()
	}
	return p.ctx
}

// CheckAccount opens the account's page, confirms the session and reads the
// quota. Imported cookies are seeded again once when the page is signed out.
func (p *Provider) CheckAccount(ctx context.Context, id string) models.Response[MuseAccount] {
	acc, err := p.accounts.get(id)
	if err != nil {
		return fail[MuseAccount](err, models.CodeNotFound)
	}
	cfg := p.cfg.Get().Muse
	page, err := p.pageFor(ctx, acc)
	if err != nil {
		updated, _ := p.updateAccount(id, func(a *storedAccount) { a.Status, a.Message = MuseAccountError, err.Error() })
		return models.Response[MuseAccount]{Data: updated, Error: err.Error(), Code: models.CodeUnavailable}
	}
	signedIn, err := p.waitSignedIn(ctx, page, time.Duration(cfg.PageReadySeconds)*time.Second)
	if err == nil && !signedIn && len(acc.Cookies) > 0 {
		if b := p.browsers.running(acc.ProfileDir); b != nil {
			if injectCookies(ctx, b.Port, acc.Cookies) == nil {
				if t, tabErr := (&chromePage{port: b.Port}).tab(ctx); tabErr == nil {
					_ = navigateHome(ctx, t, b.Port)
				}
				signedIn, err = p.waitSignedIn(ctx, page, time.Duration(cfg.PageReadySeconds)*time.Second)
			}
		}
	}
	if err != nil {
		return fail[MuseAccount](err, models.CodeUnavailable)
	}
	var quota *MuseQuota
	if signedIn {
		quota, _ = page.Quota(ctx)
	}
	updated, saveErr := p.updateAccount(id, func(a *storedAccount) {
		a.LastCheckedAt = p.now().UTC().Format(time.RFC3339)
		if !signedIn {
			a.Status, a.Message = MuseAccountSignedOut, "Chưa đăng nhập muse.ai. Bấm Mở Chrome để đăng nhập, hoặc dán cookie mới."
			return
		}
		a.Status, a.Message = MuseAccountSignedIn, ""
		if quota != nil {
			a.Quota = quota
			if !quota.weeklyOut() {
				a.PausedUntil = ""
			}
		}
	})
	if saveErr != nil {
		return fail[MuseAccount](saveErr, models.CodeInternal)
	}
	p.trigger()
	return models.SuccessResponse(updated)
}

// OpenAccountBrowser shows the account's Chrome so the user can sign in again
// or look at Muse. It is refused while jobs use the hidden browser.
func (p *Provider) OpenAccountBrowser(ctx context.Context, id string) models.Response[MuseAccount] {
	acc, err := p.accounts.get(id)
	if err != nil {
		return fail[MuseAccount](err, models.CodeNotFound)
	}
	if b := p.browsers.running(acc.ProfileDir); b != nil && b.Headless && p.busyAccount(id) {
		return fail[MuseAccount](errors.New("Tài khoản đang chạy tác vụ. Hãy chờ xong rồi mở Chrome."), models.CodeUnavailable)
	}
	if err := p.startSignIn(ctx, acc); err != nil {
		return fail[MuseAccount](err, models.CodeUnavailable)
	}
	updated, _ := p.accounts.get(id)
	return models.SuccessResponse(updated.MuseAccount)
}

// startSignIn opens a visible browser on muse.ai and watches it until the
// page is signed in, the window closes or the login timeout passes.
func (p *Provider) startSignIn(ctx context.Context, acc storedAccount) error {
	route, err := p.setupProxy(ctx, &acc)
	if err != nil {
		return err
	}
	b, err := p.browsers.get(ctx, acc.ProfileDir, true, route.key, route.args...)
	if err != nil {
		return err
	}
	p.mu.Lock()
	if p.watching[acc.ID] {
		p.mu.Unlock()
		return nil
	}
	p.watching[acc.ID] = true
	p.mu.Unlock()
	if acc.Status != MuseAccountSignedIn {
		p.updateAccount(acc.ID, func(a *storedAccount) {
			a.Status, a.Message = MuseAccountSigningIn, "Đăng nhập muse.ai trong cửa sổ Chrome vừa mở."
		})
	}
	go func() {
		defer func() {
			p.mu.Lock()
			delete(p.watching, acc.ID)
			p.mu.Unlock()
		}()
		p.watchSignIn(&chromePage{port: b.Port, callTimeout: 30 * time.Second}, acc)
	}()
	return nil
}

func (p *Provider) watchSignIn(page *chromePage, acc storedAccount) {
	cfg := p.cfg.Get()
	ctx, cancel := context.WithTimeout(p.lifetime(), time.Duration(cfg.Muse.LoginTimeoutMinutes)*time.Minute)
	defer cancel()
	missing := 0
	for {
		if err := p.sleep(ctx, time.Duration(cfg.Accounts.PollIntervalMS)*time.Millisecond); err != nil {
			p.updateAccount(acc.ID, func(a *storedAccount) {
				if a.Status == MuseAccountSigningIn {
					a.Status, a.Message = MuseAccountSignedOut, "Hết thời gian chờ đăng nhập."
				}
			})
			return
		}
		state, err := page.Login(ctx)
		if err == nil && state.LoggedIn {
			quota, _ := page.Quota(ctx)
			p.updateAccount(acc.ID, func(a *storedAccount) {
				a.Status, a.Message = MuseAccountSignedIn, ""
				a.LastCheckedAt = p.now().UTC().Format(time.RFC3339)
				if quota != nil {
					a.Quota = quota
				}
			})
			p.trigger()
			return
		}
		// The user closing the window (or Chrome) ends the sign-in.
		if err != nil && !errors.Is(err, errTabMissing) {
			var pageErr *pageError
			if !errors.As(err, &pageErr) {
				if missing++; missing >= 3 {
					p.updateAccount(acc.ID, func(a *storedAccount) {
						if a.Status == MuseAccountSigningIn {
							a.Status, a.Message = MuseAccountSignedOut, "Đã đóng cửa sổ đăng nhập."
						}
					})
					return
				}
				continue
			}
		}
		missing = 0
	}
}

// SetAccountEnabled includes or excludes an account from the queue.
func (p *Provider) SetAccountEnabled(id string, enabled bool) models.Response[MuseAccount] {
	acc, err := p.updateAccount(id, func(a *storedAccount) {
		a.Enabled = enabled
		if enabled {
			a.PausedUntil = ""
		}
	})
	if err != nil {
		return fail[MuseAccount](err, models.CodeNotFound)
	}
	p.trigger()
	return models.SuccessResponse(acc)
}

// RemoveAccount deletes the account and its Chrome profile. Jobs already sent
// from it can no longer be checked, so they are failed rather than re-sent.
func (p *Provider) RemoveAccount(id string) models.Response[bool] {
	acc, err := p.accounts.get(id)
	if err != nil {
		return fail[bool](err, models.CodeNotFound)
	}
	if p.busyAccount(id) {
		return fail[bool](errors.New("Tài khoản đang chạy tác vụ. Hãy dừng tác vụ trước khi xoá."), models.CodeUnavailable)
	}
	if err := p.browsers.removeProfile(acc.ProfileDir); err != nil {
		return fail[bool](err, models.CodeInternal)
	}
	if err := p.accounts.remove(id); err != nil {
		return fail[bool](err, models.CodeInternal)
	}
	for _, job := range p.jobs.list() {
		if job.Status == MuseJobProcessing && job.AccountID == id {
			p.updateJob(job.ID, func(j *MuseJob) {
				j.Status, j.Step, j.FinishedAt = MuseJobFailed, "", p.now().UTC().Format(time.RFC3339)
				j.Error = "Tài khoản đã bị xoá nên không kiểm tra được kết quả. Bấm Chạy lại để gửi bằng tài khoản khác."
			})
		}
	}
	p.emitAccounts()
	return models.SuccessResponse(true)
}

// ---------- Jobs ----------

// ImportReferences copies picked image files into the media folder so they can
// be previewed and attached.
func (p *Provider) ImportReferences(paths []string) models.Response[[]MuseReference] {
	limit := p.cfg.Get().Muse.MaxAttachmentBytes
	out := make([]MuseReference, 0, len(paths))
	for _, path := range paths {
		ext := strings.ToLower(filepath.Ext(path))
		mime, ok := imageTypes[ext]
		if !ok {
			return fail[[]MuseReference](fmt.Errorf("%s không phải ảnh PNG, JPG hoặc WEBP.", filepath.Base(path)), models.CodeInvalidArgument)
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return fail[[]MuseReference](fmt.Errorf("Không đọc được %s.", filepath.Base(path)), models.CodeInvalidArgument)
		}
		if info.Size() > limit {
			return fail[[]MuseReference](fmt.Errorf("%s lớn hơn %d MB.", filepath.Base(path), limit>>20), models.CodeInvalidArgument)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fail[[]MuseReference](err, models.CodeInternal)
		}
		url, err := p.media.Put("muse-ref-"+uuid.NewString(), mime, ext, data)
		if err != nil {
			return fail[[]MuseReference](err, models.CodeInternal)
		}
		out = append(out, MuseReference{LocalURL: url, FileName: filepath.Base(path), Size: info.Size()})
	}
	return models.SuccessResponse(out)
}

// SubmitJobs queues one job per non-empty prompt.
func (p *Provider) SubmitJobs(req MuseJobRequest) models.Response[[]MuseJob] {
	return p.submit(req, nil)
}

// submit validates req and queues one job per prompt. decorate lets internal
// callers (productions) complete each job before it is written, so a job is
// never on disk half-built.
func (p *Provider) submit(req MuseJobRequest, decorate func(*MuseJob)) models.Response[[]MuseJob] {
	if req.Kind != MuseJobImage && req.Kind != MuseJobVideo && req.Kind != MuseJobV2V {
		return fail[[]MuseJob](errors.New("Loại tác vụ không hợp lệ."), models.CodeInvalidArgument)
	}
	if req.Kind == MuseJobV2V {
		if req.SourceVideo == "" {
			return fail[[]MuseJob](errors.New("Tác vụ V2V bắt buộc có video nguồn."), models.CodeInvalidArgument)
		}
		if len(req.References) == 0 {
			return fail[[]MuseJob](errors.New("Tác vụ V2V bắt buộc có ít nhất 1 ảnh tham chiếu."), models.CodeInvalidArgument)
		}
		vpath, err := p.media.Path(req.SourceVideo)
		if err != nil {
			return fail[[]MuseJob](err, models.CodeInvalidArgument)
		}
		if _, ok := media.VideoTypes[strings.ToLower(filepath.Ext(vpath))]; !ok {
			return fail[[]MuseJob](errors.New("Video nguồn không đúng định dạng."), models.CodeInvalidArgument)
		}
	}
	if !validRatio(req.Ratio) {
		return fail[[]MuseJob](errors.New("Tỷ lệ khung hình không hợp lệ."), models.CodeInvalidArgument)
	}
	count := 1
	if req.Kind == MuseJobImage {
		if req.Count < 1 || req.Count > MaxImagesPerJob {
			return fail[[]MuseJob](fmt.Errorf("Số ảnh mỗi prompt phải từ 1 đến %d.", MaxImagesPerJob), models.CodeInvalidArgument)
		}
		count = req.Count
	}
	if len(req.References) > MaxReferences {
		return fail[[]MuseJob](fmt.Errorf("Tối đa %d ảnh tham chiếu.", MaxReferences), models.CodeInvalidArgument)
	}
	for _, ref := range req.References {
		path, err := p.media.Path(ref)
		if err != nil {
			return fail[[]MuseJob](err, models.CodeInvalidArgument)
		}
		if _, ok := imageTypes[strings.ToLower(filepath.Ext(path))]; !ok {
			return fail[[]MuseJob](errors.New("Ảnh tham chiếu không hợp lệ."), models.CodeInvalidArgument)
		}
	}
	var prompts []string
	for _, prompt := range req.Prompts {
		if prompt = strings.TrimSpace(prompt); prompt != "" {
			if utf8.RuneCountInString(prompt) > MaxPromptRunes {
				return fail[[]MuseJob](fmt.Errorf("Mỗi prompt tối đa %d ký tự.", MaxPromptRunes), models.CodeInvalidArgument)
			}
			prompts = append(prompts, prompt)
		}
	}
	if len(prompts) == 0 {
		return fail[[]MuseJob](errors.New("Hãy nhập prompt."), models.CodeInvalidArgument)
	}
	if len(prompts) > MaxPromptsPerRun {
		return fail[[]MuseJob](fmt.Errorf("Tối đa %d prompt mỗi lần.", MaxPromptsPerRun), models.CodeInvalidArgument)
	}

	studio := p.studio.get()
	projectID := req.ProjectID
	if projectID == "" {
		projectID = studio.ActiveProject
	}

	jobs := make([]MuseJob, 0, len(prompts))
	base := p.now().UTC()
	for i, prompt := range prompts {
		comp, err := composeImages(prompt, i, studio, req.References, req.PairMode, MaxReferences)
		if err != nil {
			return fail[[]MuseJob](err, models.CodeInvalidArgument)
		}
		if comp.Images == nil {
			comp.Images = []string{}
		}

		job := MuseJob{
			ID:          uuid.NewString(),
			Kind:        req.Kind,
			Prompt:      comp.UntaggedText,
			Ratio:       req.Ratio,
			Count:       count,
			References:  comp.Images,
			Manifest:    comp.Manifest,
			Status:      MuseJobQueued,
			Files:       []string{},
			SavedFiles:  []string{},
			SourceVideo: req.SourceVideo,
			KeepAudio:   req.KeepAudio,
			Held:        req.Held,
			ProjectID:   projectID,
			Label:       req.Label,
			// Distinct, ordered timestamps keep the batch in prompt order.
			CreatedAt: base.Add(time.Duration(i) * time.Millisecond).Format("2006-01-02T15:04:05.000Z07:00"),
		}
		if decorate != nil {
			decorate(&job)
		}
		if err := p.jobs.add(job); err != nil {
			return fail[[]MuseJob](err, models.CodeInternal)
		}
		p.emit(events.EventMuseJobUpdated, job)
		jobs = append(jobs, job)
	}
	p.trigger()
	return models.SuccessResponse(jobs)
}

func validRatio(ratio string) bool {
	for _, r := range Ratios {
		if r.Value == ratio {
			return true
		}
	}
	return false
}

// ListJobs returns every job, newest first.
func (p *Provider) ListJobs() models.Response[[]MuseJob] {
	return models.SuccessResponse(sortedJobs(p.jobs.list()))
}

// CancelJob stops a queued or running job. A job Muse is already working on
// is cancelled there too, so it stops using the account's quota.
func (p *Provider) CancelJob(id string) models.Response[MuseJob] {
	job, ok := p.jobs.get(id)
	if !ok {
		return fail[MuseJob](errJobNotFound, models.CodeNotFound)
	}
	if job.Status != MuseJobQueued && job.Status != MuseJobProcessing {
		return fail[MuseJob](errors.New("Tác vụ đã kết thúc."), models.CodeInvalidArgument)
	}
	p.mu.Lock()
	cancel := p.cancels[id]
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	// A send in flight is cancelled on Muse by the runner once it returns.
	if job.SessionID != "" && !job.Sending {
		if acc, err := p.accounts.get(job.AccountID); err == nil {
			go func() {
				ctx, done := context.WithTimeout(p.lifetime(), time.Minute)
				defer done()
				if page, err := p.pageFor(ctx, acc); err == nil {
					_ = page.Cancel(ctx, job.SessionID)
				}
			}()
		}
	}
	updated, err := p.updateJob(id, func(j *MuseJob) {
		j.Status, j.Step, j.Error, j.Sending = MuseJobCancelled, "", "Đã huỷ", false
		j.FinishedAt = p.now().UTC().Format(time.RFC3339)
	})
	if err != nil {
		return fail[MuseJob](err, models.CodeInternal)
	}
	return models.SuccessResponse(updated)
}

// RetryJob sends a failed or cancelled job again, in a new chat.
func (p *Provider) RetryJob(id string) models.Response[MuseJob] {
	job, ok := p.jobs.get(id)
	if !ok {
		return fail[MuseJob](errJobNotFound, models.CodeNotFound)
	}
	if job.Status != MuseJobFailed && job.Status != MuseJobCancelled {
		return fail[MuseJob](errors.New("Chỉ chạy lại được tác vụ lỗi hoặc đã huỷ."), models.CodeInvalidArgument)
	}
	updated, err := p.updateJob(id, func(j *MuseJob) {
		resetForQueue(j)
		j.Attempts = 0
	})
	if err != nil {
		return fail[MuseJob](err, models.CodeInternal)
	}
	p.trigger()
	return models.SuccessResponse(updated)
}

// RemoveJob deletes a job that is not running. Downloaded files stay in the
// media folder.
func (p *Provider) RemoveJob(id string) models.Response[bool] {
	job, ok := p.jobs.get(id)
	if !ok {
		return fail[bool](errJobNotFound, models.CodeNotFound)
	}
	if job.Status == MuseJobProcessing {
		return fail[bool](errors.New("Tác vụ đang chạy; hãy huỷ trước khi xoá."), models.CodeInvalidArgument)
	}
	if err := p.jobs.remove(id); err != nil {
		return fail[bool](err, models.CodeInternal)
	}
	return models.SuccessResponse(true)
}

// LocalImageTypes lists image extensions for the file picker.
func LocalImageTypes() []string {
	out := make([]string, 0, len(imageTypes))
	for ext := range imageTypes {
		out = append(out, "*"+ext)
	}
	slices.Sort(out)
	return out
}

// RunJobs removes the Held flag from queued jobs so the scheduler picks them up.
func (p *Provider) RunJobs(ids []string) models.Response[int] {
	count := 0
	for _, id := range ids {
		if j, ok := p.jobs.get(id); ok && j.Held {
			_, err := p.updateJob(id, func(job *MuseJob) {
				job.Held = false
			})
			if err == nil {
				count++
			}
		}
	}
	if count > 0 {
		p.trigger()
	}
	return models.SuccessResponse(count)
}

// HoldQueued marks all currently unstarted queued jobs as Held.
func (p *Provider) HoldQueued() models.Response[int] {
	count := 0
	for _, j := range p.jobs.list() {
		if j.Status == MuseJobQueued && !j.Held {
			_, err := p.updateJob(j.ID, func(job *MuseJob) {
				job.Held = true
			})
			if err == nil {
				count++
			}
		}
	}
	return models.SuccessResponse(count)
}

// RetryFailed resets all failed or cancelled jobs back to queued status.
func (p *Provider) RetryFailed() models.Response[int] {
	count := 0
	for _, j := range p.jobs.list() {
		if j.Status == MuseJobFailed || j.Status == MuseJobCancelled {
			_, err := p.updateJob(j.ID, func(job *MuseJob) {
				resetForQueue(job)
				job.Attempts, job.Held = 0, false
			})
			if err == nil {
				count++
			}
		}
	}
	if count > 0 {
		p.trigger()
	}
	return models.SuccessResponse(count)
}

// ClearFinished deletes all succeeded jobs from the journal.
func (p *Provider) ClearFinished() models.Response[int] {
	n, err := p.jobs.removeWhere(func(j MuseJob) bool {
		return j.Status == MuseJobSucceeded
	})
	if err != nil {
		return fail[int](err, models.CodeInternal)
	}
	p.emit(events.EventMuseJobsChanged, nil)
	return models.SuccessResponse(n)
}

// RemoveJobs deletes the specified jobs that are not actively processing.
func (p *Provider) RemoveJobs(ids []string) models.Response[int] {
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	n, err := p.jobs.removeWhere(func(j MuseJob) bool {
		return idSet[j.ID] && j.Status != MuseJobProcessing
	})
	if err != nil {
		return fail[int](err, models.CodeInternal)
	}
	p.emit(events.EventMuseJobsChanged, nil)
	return models.SuccessResponse(n)
}

// EditAndRetry updates the prompt of an unstarted or ended job and queues it.
func (p *Provider) EditAndRetry(id, prompt string) models.Response[MuseJob] {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return fail[MuseJob](errors.New("Prompt không được để trống."), models.CodeInvalidArgument)
	}
	if utf8.RuneCountInString(prompt) > MaxPromptRunes {
		return fail[MuseJob](fmt.Errorf("Mỗi prompt tối đa %d ký tự.", MaxPromptRunes), models.CodeInvalidArgument)
	}
	job, ok := p.jobs.get(id)
	if !ok {
		return fail[MuseJob](errJobNotFound, models.CodeNotFound)
	}
	if job.Status == MuseJobProcessing {
		return fail[MuseJob](errors.New("Tác vụ đang chạy, hãy huỷ trước khi sửa."), models.CodeInvalidArgument)
	}
	updated, err := p.updateJob(id, func(j *MuseJob) {
		resetForQueue(j)
		j.Prompt, j.Attempts, j.Held = prompt, 0, false
	})
	if err != nil {
		return fail[MuseJob](err, models.CodeInternal)
	}
	p.trigger()
	return models.SuccessResponse(updated)
}

// OpenOutputFolder opens the configured or default Muse output folder in the OS file explorer.
func (p *Provider) OpenOutputFolder() models.Response[bool] {
	dir := p.cfg.Get().Muse.OutputDir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fail[bool](err, models.CodeInternal)
		}
		dir = filepath.Join(home, "Videos", "SnakeMedia Muse")
	}
	_ = os.MkdirAll(dir, 0o755)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	if err := cmd.Start(); err != nil {
		return fail[bool](err, models.CodeInternal)
	}
	return models.SuccessResponse(true)
}

// RevealFile opens the system file explorer and selects the target file.
func (p *Provider) RevealFile(targetPath string) models.Response[bool] {
	targetPath = strings.TrimSpace(targetPath)
	if targetPath == "" {
		return fail[bool](errors.New("Đường dẫn file không được để trống."), models.CodeInvalidArgument)
	}
	if strings.HasPrefix(targetPath, "/flow-media/") {
		realPath, err := p.media.Path(targetPath)
		if err == nil {
			targetPath = realPath
		}
	}
	if _, err := os.Stat(targetPath); err != nil {
		return fail[bool](fmt.Errorf("File không tồn tại trên đĩa: %s", targetPath), models.CodeNotFound)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", "/select,", filepath.Clean(targetPath))
	case "darwin":
		cmd = exec.Command("open", "-R", targetPath)
	default:
		cmd = exec.Command("xdg-open", filepath.Dir(targetPath))
	}
	if err := cmd.Start(); err != nil {
		return fail[bool](err, models.CodeInternal)
	}
	return models.SuccessResponse(true)
}

// GetProxies returns the raw proxy list.
func (p *Provider) GetProxies() models.Response[string] {
	text, err := p.proxies.GetText()
	if err != nil {
		return fail[string](err, models.CodeInternal)
	}
	return models.SuccessResponse(text)
}

// SaveProxies persists the raw proxy list.
func (p *Provider) SaveProxies(text string) models.Response[bool] {
	if err := p.proxies.SaveText(text); err != nil {
		return fail[bool](err, models.CodeInternal)
	}
	// Idle browsers still run with the previous proxy; close them so the next
	// job opens Chrome with the new list. Busy ones switch on their next start.
	if stored, err := p.accounts.list(); err == nil {
		for _, acc := range stored {
			if !p.busyAccount(acc.ID) {
				_ = p.browsers.close(acc.ProfileDir)
			}
		}
	}
	return models.SuccessResponse(true)
}

// CheckProxies tests all configured proxies concurrently.
func (p *Provider) CheckProxies(ctx context.Context) models.Response[[]ProxyCheckResult] {
	text, err := p.proxies.GetText()
	if err != nil {
		return fail[[]ProxyCheckResult](err, models.CodeInternal)
	}
	lines := parseProxyList(text)
	if len(lines) == 0 {
		return models.SuccessResponse([]ProxyCheckResult{})
	}

	results := make([]ProxyCheckResult, len(lines))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)

	for i, line := range lines {
		wg.Add(1)
		go func(idx int, raw string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			parsed := ParseProxy(raw)
			if parsed == nil || parsed.Error != "" {
				msg := "Sai cú pháp"
				if parsed != nil && parsed.Error != "" {
					msg = parsed.Error
				}
				results[idx] = ProxyCheckResult{
					Raw:       raw,
					Status:    ProxyStatusDead,
					OK:        false,
					Message:   msg,
					CheckedAt: time.Now().UTC().Format(time.RFC3339),
				}
				return
			}

			if parsed.Type == ProxyTypeStatic {
				results[idx] = checkStatic(parsed, 10*time.Second)
			} else {
				results[idx] = fetchRotating(ctx, parsed, false)
			}
		}(i, line)
	}

	wg.Wait()
	return models.SuccessResponse(results)
}

// SetAccountProxy assigns a proxy to an account.
func (p *Provider) SetAccountProxy(id, proxyLine string) models.Response[MuseAccount] {
	line := strings.TrimSpace(proxyLine)
	if line != "" && line != "none" {
		parsed := ParseProxy(line)
		if parsed == nil || parsed.Error != "" {
			return fail[MuseAccount](errors.New("Định dạng proxy không hợp lệ"), models.CodeInvalidArgument)
		}
	}
	acc, err := p.accounts.get(id)
	if err != nil {
		return fail[MuseAccount](err, models.CodeNotFound)
	}
	_ = p.browsers.close(acc.ProfileDir)

	updated, err := p.accounts.update(id, func(a *storedAccount) {
		a.Proxy = line
		a.ProxyAuto = ""
		a.ProxyStatus = ""
		a.ProxyIP = ""
	})
	if err != nil {
		return fail[MuseAccount](err, models.CodeInternal)
	}
	p.emitAccounts()
	return models.SuccessResponse(updated.MuseAccount)
}

// RotateAccountIP requests a new IP for a rotating proxy assigned to an account.
func (p *Provider) RotateAccountIP(ctx context.Context, id string) models.Response[ProxyCheckResult] {
	acc, err := p.accounts.get(id)
	if err != nil {
		return fail[ProxyCheckResult](err, models.CodeNotFound)
	}
	targetProxy := acc.Proxy
	if targetProxy == "" {
		targetProxy = acc.ProxyAuto
	}
	if targetProxy == "" || targetProxy == "none" {
		return fail[ProxyCheckResult](errors.New("Tài khoản không cấu hình proxy xoay IP"), models.CodeInvalidArgument)
	}
	parsed := ParseProxy(targetProxy)
	if parsed == nil || parsed.Type != ProxyTypeRotating {
		return fail[ProxyCheckResult](errors.New("Proxy của tài khoản không phải proxy xoay IP"), models.CodeInvalidArgument)
	}

	res := fetchRotating(ctx, parsed, true)
	if !res.OK {
		p.updateAccount(id, func(a *storedAccount) {
			a.ProxyStatus = string(res.Status)
			a.Message = "Đổi IP thất bại: " + res.Message
		})
		return fail[ProxyCheckResult](fmt.Errorf("Đổi IP thất bại: %s", res.Message), models.CodeInternal)
	}

	_ = p.browsers.close(acc.ProfileDir)

	p.updateAccount(id, func(a *storedAccount) {
		a.ProxyStatus = "live"
		a.ProxyIP = res.IP
		a.Message = ""
	})
	return models.SuccessResponse(res)
}

// GetStudio returns current studio configuration, assets and projects.
func (p *Provider) GetStudio() models.Response[StudioData] {
	return models.SuccessResponse(p.studio.get())
}

// SaveStudio persists studio data.
func (p *Provider) SaveStudio(data StudioData) models.Response[bool] {
	if err := p.studio.save(data); err != nil {
		return fail[bool](err, models.CodeInternal)
	}
	return models.SuccessResponse(true)
}

// CreateStudioProject creates a new project in the studio.
func (p *Provider) CreateStudioProject(name string) models.Response[StudioProject] {
	name = strings.TrimSpace(name)
	if name == "" {
		return fail[StudioProject](errors.New("Tên dự án không được để trống"), models.CodeInvalidArgument)
	}
	proj := StudioProject{
		ID:        uuid.NewString(),
		Name:      name,
		Paused:    false,
		CreatedAt: p.now().UTC().Format(time.RFC3339),
	}
	data := p.studio.get()
	data.Projects = append(data.Projects, proj)
	if err := p.studio.save(data); err != nil {
		return fail[StudioProject](err, models.CodeInternal)
	}
	return models.SuccessResponse(proj)
}

// SetProjectPaused pauses or resumes scheduling for a project.
func (p *Provider) SetProjectPaused(id string, paused bool) models.Response[bool] {
	data := p.studio.get()
	found := false
	for i := range data.Projects {
		if data.Projects[i].ID == id {
			data.Projects[i].Paused = paused
			found = true
			break
		}
	}
	if !found {
		return fail[bool](errors.New("Không tìm thấy dự án"), models.CodeNotFound)
	}
	if err := p.studio.save(data); err != nil {
		return fail[bool](err, models.CodeInternal)
	}
	p.trigger()
	return models.SuccessResponse(true)
}

// SetActiveProject changes the current active project.
func (p *Provider) SetActiveProject(id string) models.Response[bool] {
	data := p.studio.get()
	data.ActiveProject = id
	if err := p.studio.save(data); err != nil {
		return fail[bool](err, models.CodeInternal)
	}
	return models.SuccessResponse(true)
}

// AddStudioAsset adds a tagged reference image asset to the studio.
func (p *Provider) AddStudioAsset(asset StudioAsset) models.Response[StudioAsset] {
	res, err := p.studio.addAsset(asset)
	if err != nil {
		return fail[StudioAsset](err, models.CodeInvalidArgument)
	}
	return models.SuccessResponse(res)
}

// RemoveStudioAsset deletes an asset by ID.
func (p *Provider) RemoveStudioAsset(id string) models.Response[bool] {
	if err := p.studio.removeAsset(id); err != nil {
		return fail[bool](err, models.CodeInternal)
	}
	return models.SuccessResponse(true)
}

// UpdateStudioAsset updates an existing asset.
func (p *Provider) UpdateStudioAsset(asset StudioAsset) models.Response[StudioAsset] {
	res, err := p.studio.updateAsset(asset)
	if err != nil {
		return fail[StudioAsset](err, models.CodeInvalidArgument)
	}
	return models.SuccessResponse(res)
}

// MergeVideos concatenates selected video files into one output file.
func (p *Provider) MergeVideos(ctx context.Context, videoPaths []string, customName string) models.Response[string] {
	if len(videoPaths) == 0 {
		return fail[string](errors.New("Chưa chọn video nào để ghép"), models.CodeInvalidArgument)
	}

	cfg := p.cfg.Get().Muse
	ffmpegPath, err := findFFmpeg(cfg.FFmpegPath)
	if err != nil {
		return fail[string](err, models.CodeUnavailable)
	}

	var realPaths []string
	for _, vp := range videoPaths {
		if strings.HasPrefix(vp, "/flow-media/") {
			if path, err := p.media.Path(vp); err == nil {
				realPaths = append(realPaths, path)
				continue
			}
		}
		realPaths = append(realPaths, vp)
	}

	outDir := cfg.OutputDir
	if outDir == "" {
		home, _ := os.UserHomeDir()
		outDir = filepath.Join(home, "Videos", "SnakeMedia Muse")
	}
	_ = os.MkdirAll(outDir, 0o755)

	base := strings.TrimSpace(customName)
	if base == "" {
		base = fmt.Sprintf("Muse_Merged_%s", time.Now().Format("20060102_150405"))
	}
	base = sanitizeSlug(base)
	outFile := filepath.Join(outDir, base+".mp4")
	for k := 2; fileExists(outFile); k++ {
		outFile = filepath.Join(outDir, fmt.Sprintf("%s-%d.mp4", base, k))
	}

	if err := mergeVideos(ctx, ffmpegPath, realPaths, outFile); err != nil {
		return fail[string](err, models.CodeInternal)
	}

	return models.SuccessResponse(outFile)
}

// SubmitProduction creates a multi-scene video production.
func (p *Provider) SubmitProduction(prod Production) models.Response[Production] {
	if strings.TrimSpace(prod.Title) == "" {
		prod.Title = fmt.Sprintf("Video %s", time.Now().Format("02/01 15:04"))
	}
	if len(prod.Scenes) == 0 {
		return fail[Production](errors.New("Kịch bản phải có ít nhất 1 cảnh"), models.CodeInvalidArgument)
	}
	prod.Status = ProductionStatusQueued
	prod.CurrentSceneIdx = 0
	prod.SceneVideoFiles = []string{}
	prod.BoardFiles = []string{}
	if err := p.productions.add(prod); err != nil {
		return fail[Production](err, models.CodeInternal)
	}
	p.trigger()
	return models.SuccessResponse(prod)
}

// ListProductions returns all productions.
func (p *Provider) ListProductions() models.Response[[]Production] {
	return models.SuccessResponse(p.productions.list())
}

// ApproveProductionBoard approves storyboard and allows video generation to proceed.
func (p *Provider) ApproveProductionBoard(id string) models.Response[bool] {
	_, err := p.productions.update(id, func(pr *Production) {
		if pr.Status == ProductionStatusReview {
			pr.Status = ProductionStatusRunning
			pr.Step = "Đã duyệt storyboard, tiếp tục làm video..."
		}
	})
	if err != nil {
		return fail[bool](err, models.CodeNotFound)
	}
	p.trigger()
	return models.SuccessResponse(true)
}

// RetryProduction restarts a failed production from where it stopped. Scenes
// already made are kept. A child job still queued or running would be sent a
// second time, so retry waits until it has ended.
func (p *Provider) RetryProduction(id string) models.Response[bool] {
	prod, err := p.productions.get(id)
	if err != nil {
		return fail[bool](err, models.CodeNotFound)
	}
	if prod.Status != ProductionStatusError {
		return fail[bool](errors.New("Chỉ thử lại được video bị lỗi."), models.CodeInvalidArgument)
	}
	for _, jobID := range []string{prod.BoardJobID, prod.CurrentJobID} {
		if job, ok := p.jobs.get(jobID); ok && !job.finished() {
			return fail[bool](errors.New("Tác vụ con của video này vẫn đang chạy; hãy chờ nó kết thúc hoặc huỷ nó trước."), models.CodeInvalidArgument)
		}
	}
	_, err = p.productions.update(id, func(pr *Production) {
		pr.Error, pr.BoardJobID, pr.CurrentJobID = "", "", ""
		pr.Status, pr.Step = ProductionStatusRunning, "Đang thử lại..."
		if pr.Config.Board != "" && len(pr.BoardFiles) == 0 {
			pr.Status = ProductionStatusQueued
		}
	})
	if err != nil {
		return fail[bool](err, models.CodeInternal)
	}
	p.trigger()
	return models.SuccessResponse(true)
}

// RedrawProductionBoard clears storyboard and requests a new one.
func (p *Provider) RedrawProductionBoard(id string) models.Response[bool] {
	_, err := p.productions.update(id, func(pr *Production) {
		pr.BoardFiles = []string{}
		pr.ThreadSessionID = ""
		pr.ThreadAccountID = ""
		pr.BoardJobID = ""
		pr.Status = ProductionStatusQueued
		pr.Step = "Đang vẽ lại storyboard..."
	})
	if err != nil {
		return fail[bool](err, models.CodeNotFound)
	}
	p.trigger()
	return models.SuccessResponse(true)
}

// RemoveProduction deletes a production from the list.
func (p *Provider) RemoveProduction(id string) models.Response[bool] {
	if err := p.productions.remove(id); err != nil {
		return fail[bool](err, models.CodeInternal)
	}
	return models.SuccessResponse(true)
}
