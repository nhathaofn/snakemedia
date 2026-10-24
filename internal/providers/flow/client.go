package flow

import (
	"SnakeMedia/internal/providers/flow/chrome"
	"SnakeMedia/internal/providers/flow/session"
	"context"
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
		registry, err := chrome.DiscoverFlowRPCs(ctx, browser.Port, creds.BuildLabel, s.cfg.Get().Accounts, methods...)
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
	client, err := chrome.NewFlowClient(creds, s.cfg.Get().Accounts)
	if err == nil {
		client.SetUploadPolicy(s.cfg.Get().Flow)
	}
	return client, err
}

func (s *Provider) editClient(ctx context.Context, acc session.StoredAccount, projectID string) (*chrome.FlowClient, error) {
	browser, err := s.accounts.EnsureAccountHeadlessBrowser(ctx, acc)
	if err != nil {
		return nil, err
	}
	if browser == nil {
		return nil, chrome.ErrPageNotReady
	}
	policy := s.cfg.Get().Accounts
	ctx, cancel := context.WithTimeout(ctx, time.Duration(policy.BrowserStartupTimeoutSeconds)*time.Second)
	defer cancel()
	if err := chrome.OpenProject(ctx, browser.Port, projectID, policy); err != nil {
		return nil, err
	}
	for {
		fresh, err := s.syncSession(ctx, acc)
		if err == nil {
			client, discoveryErr := s.flowClient(ctx, fresh, chrome.MethodModels, chrome.MethodEditVideo, chrome.MethodVideoStatus, chrome.MethodProjectContents, chrome.MethodMedia)
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

func (s *Provider) catalogueClient(ctx context.Context, acc session.StoredAccount) (*chrome.FlowClient, error) {
	client, err := s.flowClient(ctx, acc, chrome.MethodModels)
	if err == nil {
		return client, nil
	}
	client, err = s.flowClient(ctx, acc, chrome.MethodProjects, chrome.MethodCreateProject)
	if err != nil {
		return nil, err
	}
	project, err := s.resolveProject(ctx, client, acc, "")
	if err != nil {
		return nil, err
	}
	return s.editClient(ctx, acc, project)
}
