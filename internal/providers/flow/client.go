package flow

import (
	"SnakeMedia/internal/providers/flow/chrome"
	"SnakeMedia/internal/providers/flow/session"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

func (s *Provider) flowClient(ctx context.Context, acc session.StoredAccount, methods ...string) (*chrome.FlowClient, error) {
	if !acc.Credentials.HasRuntime() && s.syncSession != nil {
		fresh, err := s.syncSession(ctx, acc)
		if err != nil {
			return nil, err
		}
		acc = fresh
	}
	creds := acc.Credentials
	if !creds.RPCs.Has(methods...) {
		browser, err := s.accounts.EnsureAccountHeadlessBrowser(ctx, acc)
		if err != nil {
			return nil, err
		}
		if browser == nil {
			return nil, chrome.ErrPageNotReady
		}
		registry, err := chrome.DiscoverFlowRPCs(ctx, browser.Port, creds.BuildLabel, s.cfg.Get().FlowPagePolicy(), methods...)
		if err != nil {
			return nil, err
		}
		// Partial discoveries (home vs project bundles) must never clobber
		// RPC IDs already discovered for this Flow build.
		creds.RPCs = registry.Merge(creds.RPCs)
		if err := s.accounts.UpdateCredentials(acc.ID, creds); err != nil {
			return nil, err
		}
	}
	// The client's own requests leave on the account's proxy, never the real IP.
	proxyURL, err := s.accounts.ProxyURL(ctx, acc)
	if err != nil {
		return nil, err
	}
	client, err := chrome.NewFlowClient(creds, s.cfg.Get().FlowPagePolicy())
	if err == nil {
		client.SetUploadPolicy(s.cfg.Get().Flow)
		client.SetProxy(proxyURL)
		if s.syncSession != nil {
			client.SetReadSessionRefresh(func(ctx context.Context) (*chrome.FlowClient, error) {
				fresh, err := s.syncSession(ctx, acc)
				if err != nil {
					return nil, err
				}
				return s.flowClient(ctx, fresh, methods...)
			})
		}
	}
	return client, err
}

// readFlowPayload repeats a read-only RPC once after an explicit HTTP 401.
// Resolving the method again uses the fresh browser session's build and RPCs.
func readFlowPayload(ctx context.Context, client *chrome.FlowClient, method string, input any, timeout time.Duration) (string, error) {
	switch method {
	case chrome.MethodModels, chrome.MethodProjects, chrome.MethodProjectContents, chrome.MethodMedia, chrome.MethodVideoStatus:
	default:
		return "", fmt.Errorf("phương thức Flow %q không thuộc nhóm đọc", method)
	}
	read := func() (string, error) {
		id := client.Credentials().RPCs.ID(method)
		envelope, err := BuildBatchEnvelope(id, input)
		if err != nil {
			return "", err
		}
		body, err := client.Do(ctx, id, envelope, timeout)
		if err != nil {
			return "", err
		}
		return chrome.ParseRPCPayload(string(body), id)
	}
	payload, err := read()
	if !isUnauthorized(err) || ctx.Err() != nil {
		return payload, err
	}
	refreshed, refreshErr := client.RefreshReadSession(ctx)
	if refreshErr != nil {
		return "", fmt.Errorf("không đọc lại được phiên Flow sau khi máy chủ trả về 401: %w", refreshErr)
	}
	if !refreshed {
		return payload, err
	}
	return read()
}

func (s *Provider) editClient(ctx context.Context, acc session.StoredAccount, projectID string) (*chrome.FlowClient, error) {
	return s.projectClient(ctx, acc, projectID, chrome.MethodModels, chrome.MethodEditVideo, chrome.MethodVideoStatus, chrome.MethodProjectContents, chrome.MethodMedia)
}

// projectClient opens projectID in the account's browser so the project page's
// lazy bundles load, then discovers methods from them.
func (s *Provider) projectClient(ctx context.Context, acc session.StoredAccount, projectID string, methods ...string) (*chrome.FlowClient, error) {
	browser, err := s.accounts.EnsureAccountHeadlessBrowser(ctx, acc)
	if err != nil {
		return nil, err
	}
	if browser == nil {
		return nil, chrome.ErrPageNotReady
	}
	policy := s.cfg.Get().FlowPagePolicy()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(policy.BrowserStartupTimeoutSeconds)*time.Second)
	defer cancel()
	// Opening the project navigates the account's Flow tab: wait until no
	// token is being minted or request sent from it, and keep others off it
	// until its bundles are discovered.
	page := s.pageLock(browser.Port)
	page.Lock()
	defer page.Unlock()
	if err := chrome.OpenProject(ctx, browser.Port, projectID, policy); err != nil {
		return nil, err
	}
	for {
		fresh := acc
		var err error
		if s.syncSession != nil {
			fresh, err = s.syncSession(ctx, acc)
		}
		if err == nil {
			client, discoveryErr := s.flowClient(ctx, fresh, methods...)
			if discoveryErr == nil {
				return client, nil
			}
			err = discoveryErr
		}
		if errors.Is(err, session.ErrSessionSignedOut) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(time.Duration(policy.PollIntervalMS) * time.Millisecond):
		}
	}
}

// projectPageClient discovers methods that only a project page loads (project
// contents, media). The Flow home page lacks them, so when discovery there
// fails it opens the account's project and discovers again.
func (s *Provider) projectPageClient(ctx context.Context, acc session.StoredAccount, methods ...string) (*chrome.FlowClient, error) {
	client, err := s.flowClient(ctx, acc, methods...)
	if err == nil {
		return client, nil
	}
	home, homeErr := s.homePageClient(ctx, acc)
	if homeErr != nil {
		return nil, homeErr
	}
	project, projectErr := s.resolveProject(ctx, home, acc, "")
	if projectErr != nil {
		return nil, projectErr
	}
	return s.projectClient(ctx, acc, project, methods...)
}

// homePageClient discovers the project-list methods of the Flow home page. A
// headless Chrome that has just started may not have loaded those bundles yet,
// so discovery is retried until the browser startup timeout.
func (s *Provider) homePageClient(ctx context.Context, acc session.StoredAccount) (*chrome.FlowClient, error) {
	policy := s.cfg.Get().FlowPagePolicy()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(policy.BrowserStartupTimeoutSeconds)*time.Second)
	defer cancel()
	for {
		client, err := s.flowClient(ctx, acc, chrome.MethodProjects, chrome.MethodCreateProject)
		if err == nil {
			return client, nil
		}
		if errors.Is(err, session.ErrSessionSignedOut) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(time.Duration(policy.PollIntervalMS) * time.Millisecond):
		}
	}
}

func (s *Provider) catalogueClient(ctx context.Context, acc session.StoredAccount) (*chrome.FlowClient, error) {
	client, err := s.flowClient(ctx, acc, chrome.MethodModels)
	if err == nil {
		return client, nil
	}
	// A browser that has just started has not loaded the bundles yet; the
	// home page client waits for them.
	client, err = s.homePageClient(ctx, acc)
	if err != nil {
		return nil, err
	}
	project, err := s.resolveProject(ctx, client, acc, "")
	if err != nil {
		return nil, err
	}
	return s.projectClient(ctx, acc, project, chrome.MethodModels)
}

// pageLock guards the Flow tab of the account browser on port: opening a
// project (navigation) takes it exclusively, minting a token or sending from
// the page shares it.
func (s *Provider) pageLock(port int) *sync.RWMutex {
	lock, _ := s.pageLocks.LoadOrStore(port, &sync.RWMutex{})
	return lock.(*sync.RWMutex)
}

// mintLock serialises reCAPTCHA token minting per Chrome port so concurrent
// requests cannot overlap global window.Object.assign patches in recaptcha.go.
// Lock order: mintLock -> pageLock.RLock.
func (s *Provider) mintLock(port int) *sync.Mutex {
	lock, _ := s.mintLocks.LoadOrStore(port, &sync.Mutex{})
	return lock.(*sync.Mutex)
}
