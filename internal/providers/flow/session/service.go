package session

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/providers/flow/chrome"
)

// AccountStatus is how far a Flow account is verified.
type AccountStatus string

const (
	// AccountSignedIn: Google is signed in but the balance was not read yet.
	AccountSignedIn AccountStatus = "signed_in"
	// AccountConnected: signed in with a verified balance; the account rotates.
	AccountConnected AccountStatus = "connected"
)

// AccountProfile is the Flow provider's view of one account.
type AccountProfile struct {
	ID             string
	Email          string
	GaiaID         string
	Name           string
	ProfileDir     string
	Status         AccountStatus
	Balance        *chrome.FlowCredits
	LastVerifiedAt string
}

// StoredAccount is an account with the Flow session values it runs on.
type StoredAccount struct {
	AccountProfile
	Credentials chrome.Credentials
}

// ErrSessionSignedOut reports a profile whose Chrome is no longer signed in to Google.
var ErrSessionSignedOut = errors.New("Chrome của tài khoản đã đăng xuất khỏi Google; hãy đăng nhập lại ở mục Tài khoản.")

// Service gives the Flow provider its accounts from the shared service.
type Service struct {
	accounts        *accounts.Service
	cfg             *config.Manager
	inspectIdentity func(context.Context, int, config.AccountConfig) (*chrome.FlowSessionDetails, error)
}

// NewService serves the Flow accounts of svc.
func NewService(svc *accounts.Service, cfg *config.Manager) *Service {
	return &Service{accounts: svc, cfg: cfg, inspectIdentity: chrome.InspectFlowIdentity}
}

func view(rec accounts.Record) (StoredAccount, error) {
	var data flowData
	if err := accounts.DecodeData(rec, &data); err != nil {
		return StoredAccount{}, err
	}
	status := AccountSignedIn
	if data.LastVerifiedAt != "" {
		status = AccountConnected
	}
	return StoredAccount{
		AccountProfile: AccountProfile{
			ID: rec.ID, Email: rec.Identity, GaiaID: data.GaiaID, Name: rec.Label(), ProfileDir: rec.ProfileDir,
			Status: status, Balance: data.Balance, LastVerifiedAt: data.LastVerifiedAt,
		},
		Credentials: data.Credentials,
	}, nil
}

// GetConnectedStoredAccounts returns enabled, signed-in Flow accounts with a
// verified balance and a proxy route: the accounts generation may rotate over.
func (s *Service) GetConnectedStoredAccounts() ([]StoredAccount, error) {
	records, err := s.accounts.Records(accounts.ProviderFlow)
	if err != nil {
		return nil, err
	}
	var connected []StoredAccount
	for _, rec := range records {
		if !rec.Ready() || s.accounts.RouteError(rec) != nil {
			continue
		}
		acc, err := view(rec)
		if err != nil {
			logger.Get().Warn("unreadable Flow account data", "account", rec.ID, "error", err)
			continue
		}
		if acc.Status == AccountConnected {
			connected = append(connected, acc)
		}
	}
	return connected, nil
}

// AcquireProfile keeps the account's browser from being restarted until release.
func (s *Service) AcquireProfile(ctx context.Context, profileDir string) (func(), error) {
	return s.accounts.AcquireProfile(ctx, profileDir)
}

// EnsureAccountHeadlessBrowser returns the account's running browser or starts
// a headless one on its proxy, with Flow open.
func (s *Service) EnsureAccountHeadlessBrowser(ctx context.Context, acc StoredAccount) (*browser.Browser, error) {
	return s.accounts.Browser(ctx, acc.ID)
}

// ProxyURL is the proxy the account's own HTTP requests must use, the route
// its browser runs on; nil only when the account is set to go direct. The
// browser is started first so the route is known; a route dropped meanwhile
// (proxy changed) is an error, never a direct connection.
func (s *Service) ProxyURL(ctx context.Context, acc StoredAccount) (*url.URL, error) {
	if _, err := s.accounts.Browser(ctx, acc.ID); err != nil {
		return nil, err
	}
	return s.accounts.ProxyURL(acc.ID)
}

// SyncSession re-reads the Flow session (cookies, CSRF token, runtime values)
// from the account's own Chrome profile. A running browser keeps rotating
// Google's cookies, so the stored copy goes stale (HTTP 401) unless it is
// refreshed from the browser like this.
func (s *Service) SyncSession(ctx context.Context, acc StoredAccount) (StoredAccount, error) {
	cfg := s.cfg.Get()
	p := cfg.FlowPagePolicy()
	b, err := s.accounts.Browser(ctx, acc.ID)
	if err != nil {
		return acc, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(p.BrowserStartupTimeoutSeconds)*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Duration(p.PollIntervalMS) * time.Millisecond)
	defer ticker.Stop()
	var observations signInObservations
	for {
		details, err := s.inspectIdentity(ctx, b.Port, p)
		switch {
		case err == nil && details != nil && details.IsLoggedIn && details.Credentials.HasRuntime():
			if details.GaiaID != acc.GaiaID && acc.GaiaID != "" {
				return acc, errors.New("Chrome của tài khoản đang đăng nhập một tài khoản Google khác.")
			}
			var creds chrome.Credentials
			if err := s.update(acc.ID, func(d *flowData) {
				creds = keepRPCs(details.Credentials, d.Credentials)
				d.Credentials = creds
			}); err != nil {
				return acc, err
			}
			acc.Credentials = creds
			return acc, nil
		case observations.signedOut(err, cfg.Flow.SignedOutReads):
			_, updateErr := s.accounts.Update(acc.ID, func(r *accounts.Record) {
				if r.Status == accounts.StatusSignedIn {
					r.Status, r.Message = accounts.StatusSignedOut, ErrSessionSignedOut.Error()
				}
			})
			if updateErr != nil {
				return acc, updateErr
			}
			return acc, ErrSessionSignedOut
		case err != nil && !errors.Is(err, chrome.ErrPageNotReady) && !errors.Is(err, chrome.ErrLoginRequired):
			return acc, err
		}
		select {
		case <-ctx.Done():
			return acc, fmt.Errorf("%w: %w", chrome.ErrPageNotReady, ctx.Err())
		case <-ticker.C:
		}
	}
}

type signInObservations struct{ required int }

func (o *signInObservations) signedOut(err error, limit int) bool {
	if !errors.Is(err, chrome.ErrLoginRequired) {
		o.required = 0
		return false
	}
	o.required++
	return o.required >= limit
}

// UpdateCredentials stores RPC IDs discovered or cookies rotated during work.
// A newer session snapshot saved meanwhile (a different CSRF token) wins.
func (s *Service) UpdateCredentials(id string, c chrome.Credentials) error {
	return s.update(id, func(d *flowData) {
		if d.Credentials.AtToken == c.AtToken {
			if d.Credentials.BuildLabel == c.BuildLabel {
				c.RPCs = c.RPCs.Merge(d.Credentials.RPCs)
			}
			d.Credentials = c
		}
	})
}

// UpdateBalance stores a balance queried after a generation. A newer session
// snapshot wins so a background query cannot overwrite newer cookies.
func (s *Service) UpdateBalance(id string, balance chrome.FlowCredits, c chrome.Credentials) error {
	return s.update(id, func(d *flowData) {
		if d.Credentials.AtToken != c.AtToken {
			return
		}
		if d.Credentials.BuildLabel == c.BuildLabel {
			c.RPCs = c.RPCs.Merge(d.Credentials.RPCs)
		}
		d.Balance, d.BalanceError, d.Credentials = &balance, "", c
		d.LastVerifiedAt = time.Now().UTC().Format(time.RFC3339Nano)
	})
}

// update changes the Flow data of an account and refreshes its usage.
func (s *Service) update(id string, change func(*flowData)) error {
	var decodeErr error
	_, err := s.accounts.Update(id, func(r *accounts.Record) {
		var data flowData
		if decodeErr = accounts.DecodeData(*r, &data); decodeErr != nil {
			return
		}
		change(&data)
		if decodeErr = r.SetData(data); decodeErr == nil {
			r.Usage = usage(data)
		}
	})
	if err != nil {
		return err
	}
	return decodeErr
}
