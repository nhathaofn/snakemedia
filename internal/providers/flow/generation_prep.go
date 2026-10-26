package flow

import (
	"context"
	"errors"
	"fmt"

	"SnakeMedia/internal/config"
	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/providers/flow/chrome"
	"SnakeMedia/internal/providers/flow/session"
)

// generationBrowser returns the account's headless Chrome, which mints the
// reCAPTCHA Enterprise tokens every billable Flow mutation needs.
func (s *Provider) generationBrowser(ctx context.Context, acc session.StoredAccount) (*chrome.Browser, error) {
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

// mintGenerationToken mints one single-use reCAPTCHA Enterprise token.
func mintGenerationToken(ctx context.Context, browser *chrome.Browser, policy config.AccountConfig, action string) (string, error) {
	token, err := chrome.MintRecaptchaToken(ctx, browser.Port, policy, action)
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
func (s *Provider) uploadReferences(ctx context.Context, client *chrome.FlowClient, acc session.StoredAccount, browser *chrome.Browser, policy config.AccountConfig, refs []ReferenceImage, projectID string) ([]string, error) {
	mediaIDs := make([]string, 0, len(refs))
	for _, ref := range refs {
		token, err := mintGenerationToken(ctx, browser, policy, chrome.ActionImageGeneration)
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
// page loads. A build where discovery fails is remembered so later requests do
// not pay the project-page round trip again; they use the verified fallback ID.
func (s *Provider) withImageGenerationRPC(ctx context.Context, acc session.StoredAccount, client *chrome.FlowClient, methods []string) *chrome.FlowClient {
	if client.Credentials().RPCs.Has(chrome.MethodGenerateImages) {
		return client
	}
	build := client.Credentials().BuildLabel
	s.mu.Lock()
	missing := s.undiscoverableBuilds[build]
	s.mu.Unlock()
	if missing {
		return client
	}
	discovered, err := s.projectPageClient(ctx, acc, append(append([]string(nil), methods...), chrome.MethodGenerateImages)...)
	if err != nil {
		logger.Get().Warn("BatchGenerateImages not discovered; using the verified fallback ID", "account", acc.ID, "build", build, "error", err)
		s.mu.Lock()
		if s.undiscoverableBuilds == nil {
			s.undiscoverableBuilds = make(map[string]bool)
		}
		s.undiscoverableBuilds[build] = true
		s.mu.Unlock()
		return client
	}
	return discovered
}
