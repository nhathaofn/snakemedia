package accounts

import (
	"context"
	"errors"
	"strings"

	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/models"
	"SnakeMedia/internal/proxy"
	"github.com/google/uuid"
)

// This file is what the frontend reaches through App: every method answers
// with a models.Response.

func fail[T any](err error, code models.Code) models.Response[T] {
	return models.ErrorResponse[T](err.Error(), code)
}

func notFoundOr(err error) models.Code {
	if errors.Is(err, errNotFound) {
		return models.CodeNotFound
	}
	return models.CodeInternal
}

// List returns the accounts of provider ("" for all) in saved order.
func (s *Service) List(provider Provider) models.Response[[]Account] {
	s.ResumeExpired()
	list, err := s.public(provider)
	if err != nil {
		return fail[[]Account](err, models.CodeInternal)
	}
	return models.SuccessResponse(list)
}

// Add saves a new Chrome-backed account without launching Chrome immediately, so
// the user can configure its proxy first and open Chrome manually when ready.
func (s *Service) Add(_ context.Context, provider Provider, name string) models.Response[Account] {
	if _, err := s.site(provider); err != nil {
		return fail[Account](err, models.CodeInvalidArgument)
	}
	clean, err := cleanName(name)
	if err != nil {
		return fail[Account](err, models.CodeInvalidArgument)
	}
	rec := s.newRecord(provider, clean, SourceBrowser)
	rec.Message = "Chọn proxy (nếu cần) rồi bấm Mở Chrome để đăng nhập."
	if err := s.store.add(rec); err != nil {
		return fail[Account](err, models.CodeInternal)
	}
	s.notify()
	return models.SuccessResponse(rec.Account)
}

// ImportCookies saves an account from cookies exported on a signed-in page.
// If the shared proxy list already has usable lines, the account joins the list
// and checks through a proxy; if the list is empty it stays unverified without
// contacting the site over the real IP until the user assigns a proxy or checks.
func (s *Service) ImportCookies(provider Provider, name, raw string) models.Response[Account] {
	site, err := s.site(provider)
	if err != nil {
		return fail[Account](err, models.CodeInvalidArgument)
	}
	if name, err = cleanName(name); err != nil {
		return fail[Account](err, models.CodeInvalidArgument)
	}
	cookies, err := s.parseCookies(site, raw)
	if err != nil {
		return fail[Account](err, models.CodeInvalidArgument)
	}
	rec := s.newRecord(provider, name, SourceCookies)
	hasSharedProxy := false
	if text, tErr := s.proxies.Text(); tErr == nil {
		for _, line := range proxy.ParseList(text) {
			if p := proxy.Parse(line); p != nil && p.Error == "" {
				hasSharedProxy = true
				break
			}
		}
	}
	rec.Cookies, rec.CookieVersion = cookies, uuid.NewString()
	if hasSharedProxy {
		rec.Proxy = ""
		rec.Message = "Đang kiểm tra cookie qua proxy…"
	} else {
		rec.Message = "Đã nhập cookie. Chọn proxy (nếu cần) rồi bấm Kiểm tra."
	}
	if err := s.store.add(rec); err != nil {
		return fail[Account](err, models.CodeInternal)
	}
	s.notify()
	if hasSharedProxy {
		s.goBackground(func() { s.check(s.lifetime(), rec.ID) })
	}
	return models.SuccessResponse(rec.Account)
}

// ReplaceCookies stores a new cookie export for an existing account.
func (s *Service) ReplaceCookies(id, raw string) models.Response[Account] {
	rec, site, err := s.lookup(id)
	if err != nil {
		return fail[Account](err, notFoundOr(err))
	}
	cookies, err := s.parseCookies(site, raw)
	if err != nil {
		return fail[Account](err, models.CodeInvalidArgument)
	}
	if err := s.closeBrowserNoSync(rec); err != nil {
		return fail[Account](err, models.CodeUnavailable)
	}
	updated, err := s.Update(id, func(r *Record) {
		r.Cookies, r.CookieVersion, r.Source = cookies, uuid.NewString(), SourceCookies
		r.Status, r.Message = StatusUnverified, "Đang kiểm tra cookie…"
	})
	if err != nil {
		return fail[Account](err, notFoundOr(err))
	}
	s.goBackground(func() { s.check(s.lifetime(), id) })
	return models.SuccessResponse(updated.Account)
}

// Check confirms the account's session and reads its usage quickly without
// sending a media generation probe.
func (s *Service) Check(ctx context.Context, id string) models.Response[Account] {
	return checkResponse(s.check(ctx, id))
}

// CheckVideo confirms the session and sends a test video generation probe on
// sites that implement VideoChecker.
func (s *Service) CheckVideo(ctx context.Context, id string) models.Response[Account] {
	return checkResponse(s.checkVideo(ctx, id))
}

func checkResponse(rec Record, err error) models.Response[Account] {
	if err != nil {
		if rec.ID == "" {
			return fail[Account](err, notFoundOr(err))
		}
		return models.Response[Account]{Data: rec.Account, Error: err.Error(), Code: models.CodeUnavailable}
	}
	return models.SuccessResponse(rec.Account)
}

// OpenBrowser shows the account's Chrome so the user can sign in again or use
// the site. It is refused while work uses the hidden browser.
func (s *Service) OpenBrowser(ctx context.Context, id string) models.Response[Account] {
	rec, site, err := s.lookup(id)
	if err != nil {
		return fail[Account](err, notFoundOr(err))
	}
	if err := s.signIn(ctx, rec, site); err != nil {
		return fail[Account](err, models.CodeUnavailable)
	}
	rec, _ = s.store.get(id)
	return models.SuccessResponse(rec.Account)
}

// Rename changes the account's label.
func (s *Service) Rename(id, name string) models.Response[Account] {
	name, err := cleanName(name)
	if err != nil {
		return fail[Account](err, models.CodeInvalidArgument)
	}
	rec, err := s.Update(id, func(r *Record) { r.Name = name })
	if err != nil {
		return fail[Account](err, notFoundOr(err))
	}
	return models.SuccessResponse(rec.Account)
}

// SetEnabled includes or excludes an account from work.
func (s *Service) SetEnabled(id string, enabled bool) models.Response[Account] {
	rec, err := s.Update(id, func(r *Record) {
		r.Enabled = enabled
		r.AutoResume = false
		if enabled {
			r.PausedUntil = ""
			if strings.Contains(r.Message, "tạm nghỉ") || strings.Contains(r.Message, "tự tắt") {
				r.Message = ""
			}
		}
	})
	if err != nil {
		return fail[Account](err, notFoundOr(err))
	}
	return models.SuccessResponse(rec.Account)
}

// SetVideoCheck toggles video creation probing/scheduling for an account while
// keeping image generation enabled.
func (s *Service) SetVideoCheck(id string, enabled bool) models.Response[Account] {
	rec, err := s.Update(id, func(r *Record) {
		r.SkipVideoCheck = !enabled
		if enabled && (strings.Contains(r.Message, "chỉ tạo ảnh") || strings.Contains(r.Message, "tắt kiểm tra tạo video")) {
			r.Message = ""
		}
	})
	if err != nil {
		return fail[Account](err, notFoundOr(err))
	}
	return models.SuccessResponse(rec.Account)
}

// SetProxy assigns a proxy line to an account: "" takes one from the list,
// proxy.None goes direct. The browser restarts on its next use.
func (s *Service) SetProxy(id, line string) models.Response[Account] {
	if line != "" && line != proxy.None {
		if parsed := proxy.Parse(line); parsed == nil || parsed.Error != "" {
			return fail[Account](errors.New("Định dạng proxy không hợp lệ."), models.CodeInvalidArgument)
		}
	}
	lock := s.openLock(id)
	lock.Lock()
	defer lock.Unlock()
	rec, err := s.store.get(id)
	if err != nil {
		return fail[Account](err, notFoundOr(err))
	}
	if err := s.closeBrowserLocked(rec, true); err != nil {
		return fail[Account](err, models.CodeUnavailable)
	}
	rec, err = s.Update(id, func(r *Record) {
		r.Proxy, r.ProxyAuto, r.ProxyStatus, r.ProxyIP = line, "", "", ""
		r.ProxyInUse, r.ProxyRotating = "", false
		if line != "" && line != proxy.None {
			r.ProxyInUse, r.ProxyRotating = proxy.Mask(line), rotating(line)
		}
	})
	if err != nil {
		return fail[Account](err, models.CodeInternal)
	}
	return models.SuccessResponse(rec.Account)
}

// RotateIP asks the account's rotating proxy for a new exit IP. The browser
// must be idle so Chrome and Go HTTP clients never split-brain across old/new IPs.
func (s *Service) RotateIP(ctx context.Context, id string) models.Response[proxy.CheckResult] {
	lock := s.openLock(id)
	lock.Lock()
	defer lock.Unlock()
	rec, err := s.store.get(id)
	if err != nil {
		return fail[proxy.CheckResult](err, notFoundOr(err))
	}
	line := assignment(rec).Line()
	if line == "" {
		return fail[proxy.CheckResult](errors.New("Tài khoản không dùng proxy xoay IP."), models.CodeInvalidArgument)
	}
	if err := s.closeBrowserLocked(rec, true); err != nil {
		if errors.Is(err, browser.ErrProfileBusy) {
			return fail[proxy.CheckResult](errors.New("Tài khoản đang chạy tác vụ. Hãy đợi tác vụ xong rồi mới xoay IP."), models.CodeUnavailable)
		}
		return fail[proxy.CheckResult](err, models.CodeUnavailable)
	}
	res, err := s.proxies.Rotate(ctx, line)
	if err != nil {
		s.Update(id, func(r *Record) { r.ProxyStatus, r.Message = string(res.Status), err.Error() })
		return fail[proxy.CheckResult](err, models.CodeUnavailable)
	}
	s.Update(id, func(r *Record) { r.ProxyStatus, r.ProxyIP, r.Message = string(proxy.StatusLive), res.IP, "" })
	return models.SuccessResponse(res)
}

// Remove deletes the account and its Chrome profile (cookies, cache, login
// data). It does not delete the account on the site itself.
func (s *Service) Remove(id string) models.Response[bool] {
	rec, err := s.store.get(id)
	if err != nil {
		return fail[bool](err, notFoundOr(err))
	}
	if err := s.pool.RemoveIfIdle(rec.ProfileDir); err != nil {
		if errors.Is(err, browser.ErrProfileBusy) {
			return fail[bool](errors.New("Tài khoản đang chạy tác vụ. Hãy dừng tác vụ trước khi xoá."), models.CodeUnavailable)
		}
		return fail[bool](err, models.CodeInternal)
	}
	s.mu.Lock()
	delete(s.routes, id)
	s.mu.Unlock()
	if err := s.store.remove(id); err != nil {
		return fail[bool](err, models.CodeInternal)
	}
	s.mu.Lock()
	hooks := append([]func(Record){}, s.removed...)
	s.mu.Unlock()
	for _, fn := range hooks {
		fn(rec)
	}
	s.notify()
	return models.SuccessResponse(true)
}

// Proxies returns the shared proxy list.
func (s *Service) Proxies() models.Response[string] {
	text, err := s.proxies.Text()
	if err != nil {
		return fail[string](err, models.CodeInternal)
	}
	return models.SuccessResponse(text)
}

// SaveProxies replaces the shared proxy list. Idle browsers using the shared
// list are closed so they pick up the new list on next start; busy ones switch
// when their work finishes.
func (s *Service) SaveProxies(text string) models.Response[bool] {
	if err := s.proxies.Save(text); err != nil {
		return fail[bool](err, models.CodeInternal)
	}
	if records, err := s.store.list(); err == nil {
		for _, rec := range records {
			if rec.Proxy != proxy.None {
				_ = s.closeBrowser(rec) // busy browsers are skipped
			}
		}
	}
	s.notify()
	return models.SuccessResponse(true)
}

// CheckProxies tests every proxy in the list.
func (s *Service) CheckProxies(ctx context.Context) models.Response[[]proxy.CheckResult] {
	results, err := s.proxies.CheckAll(ctx)
	if err != nil {
		return fail[[]proxy.CheckResult](err, models.CodeInternal)
	}
	return models.SuccessResponse(results)
}
