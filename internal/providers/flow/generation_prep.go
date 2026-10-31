package flow

import (
	"SnakeMedia/internal/browser"
	"context"
	"errors"
	"fmt"
	"time"

	"SnakeMedia/internal/config"
	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/providers/flow/chrome"
	"SnakeMedia/internal/providers/flow/session"
)

// generationBrowser returns the account's headless Chrome, which mints the
// reCAPTCHA Enterprise tokens every billable Flow mutation needs.
func (s *Provider) generationBrowser(ctx context.Context, acc session.StoredAccount) (*browser.Browser, error) {
	if s.accounts == nil {
		return nil, errors.New("dịch vụ tài khoản chưa được liên kết để cấp token reCAPTCHA")
	}
	browser, err := s.accounts.EnsureAccountHeadlessBrowser(ctx, acc)
	if err != nil {
		return nil, fmt.Errorf("không khởi động được trình duyệt Chrome để cấp bảo mật: %w", err)
	}
	if browser == nil {
		return nil, errors.New("trình duyệt Chrome chưa được khởi tạo")
	}
	return browser, nil
}

// sendBillable posts a billable RPC (one that carries a reCAPTCHA token).
// From the account's Flow page it leaves Chrome like Flow's own requests, with
// the browser that minted the token; rpcFromGo sends it from Go's HTTP client
// instead (kept for measuring the difference). The page is held so a project
// being opened elsewhere cannot navigate it mid-request.
func (s *Provider) sendBillable(ctx context.Context, client *chrome.FlowClient, browser *browser.Browser, rpcID, fReq string) ([]byte, error) {
	timeout := time.Duration(s.cfg.Get().Flow.GenerationTimeoutSeconds) * time.Second
	if s.rpcFromGo {
		return client.Do(ctx, rpcID, fReq, timeout)
	}
	page := s.pageLock(browser.Port)
	page.RLock()
	defer page.RUnlock()
	return client.DoFromBrowser(ctx, browser.Port, rpcID, fReq, timeout)
}

// mintGenerationToken mints one single-use reCAPTCHA Enterprise token with the
// site key the account's Flow page loaded. Only when the page does not expose
// it is the built-in key used, and that fallback is logged. The page is held
// so a project being opened elsewhere cannot navigate it mid-mint.
func (s *Provider) mintGenerationToken(ctx context.Context, browser *browser.Browser, policy config.AccountConfig, action string) (string, error) {
	page := s.pageLock(browser.Port)
	page.RLock()
	defer page.RUnlock()
	token, err := chrome.MintRuntimeRecaptchaToken(ctx, browser.Port, policy, action)
	if errors.Is(err, chrome.ErrNoRuntimeSiteKey) {
		logger.Get().Warn("reCAPTCHA site key not found on the Flow page; using the built-in key", "action", action)
		token, err = chrome.MintRecaptchaToken(ctx, browser.Port, policy, action)
	}
	if err != nil {
		return "", fmt.Errorf("lỗi cấp token reCAPTCHA Enterprise từ trình duyệt: %w", err)
	}
	if token == "" {
		return "", errors.New("token reCAPTCHA Enterprise trả về rỗng")
	}
	return token, nil
}

// uploadReferences uploads each reference image with its own token: tokens are
// single-use, so reusing the generation token would make the generation fail.
func (s *Provider) uploadReferences(ctx context.Context, client *chrome.FlowClient, acc session.StoredAccount, browser *browser.Browser, policy config.AccountConfig, refs []ReferenceImage, projectID string) ([]string, error) {
	mediaIDs := make([]string, 0, len(refs))
	for _, ref := range refs {
		token, err := s.mintGenerationToken(ctx, browser, policy, chrome.ActionImageGeneration)
		if err != nil {
			return nil, fmt.Errorf("không tải ảnh tham chiếu %q lên được: %w", ref.FileName, err)
		}
		mediaID, err := s.UploadReferenceImage(ctx, client, acc, ref.Base64, ref.MimeType, ref.FileName, token, projectID)
		if err != nil {
			return nil, fmt.Errorf("không tải ảnh tham chiếu %q lên được: %w", ref.FileName, err)
		}
		mediaIDs = append(mediaIDs, mediaID)
	}
	return mediaIDs, nil
}

// withImageGenerationRPC discovers BatchGenerateImages, which only a project
// page loads. A build where discovery failed is remembered for RPCCacheSeconds,
// during which requests use the verified fallback ID instead of paying the
// project-page round trip again; after that discovery is tried again.
func (s *Provider) withImageGenerationRPC(ctx context.Context, acc session.StoredAccount, client *chrome.FlowClient, methods []string) *chrome.FlowClient {
	if client.Credentials().RPCs.Has(chrome.MethodGenerateImages) {
		return client
	}
	build := client.Credentials().BuildLabel
	s.mu.Lock()
	retryAt, missing := s.undiscoverableBuilds[build]
	s.mu.Unlock()
	if missing && time.Now().Before(retryAt) {
		logger.Get().Debug("BatchGenerateImages still undiscovered; using the verified fallback ID", "build", build, "retryAt", retryAt)
		return client
	}
	discovered, err := s.projectPageClient(ctx, acc, append(append([]string(nil), methods...), chrome.MethodGenerateImages)...)
	if err != nil {
		logger.Get().Warn("BatchGenerateImages not discovered; using the verified fallback ID", "account", acc.ID, "build", build, "error", err)
		s.mu.Lock()
		if s.undiscoverableBuilds == nil {
			s.undiscoverableBuilds = make(map[string]time.Time)
		}
		s.undiscoverableBuilds[build] = time.Now().Add(time.Duration(s.cfg.Get().Flow.RPCCacheSeconds) * time.Second)
		s.mu.Unlock()
		return client
	}
	return discovered
}
