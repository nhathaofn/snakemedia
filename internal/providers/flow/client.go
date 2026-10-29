package flow

import (
	"SnakeMedia/internal/providers/flow/chrome"
	"SnakeMedia/internal/providers/flow/session"
	"context"
	"sync"
	"time"
)

func (s *Provider) flowClient(ctx context.Context, acc session.StoredAccount, methods ...string) (*chrome.FlowClient, error) {
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
		// Account-only descriptors may live in a different lazy bundle.
		if registry.Credits == "" {
			registry.Credits = creds.RPCs.Credits
		}
		if registry.Availability == "" {
			registry.Availability = creds.RPCs.Availability
		}
		creds.RPCs = registry
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
	}
	return client, err
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
		fresh, err := s.syncSession(ctx, acc)
		if err == nil {
			client, discoveryErr := s.flowClient(ctx, fresh, methods...)
			if discoveryErr == nil {
				return client, nil
			}
			err = discoveryErr
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
