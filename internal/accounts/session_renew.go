package accounts

import (
	"context"
	"errors"
	"hash/fnv"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/proxy"
	"github.com/google/uuid"
)

const (
	sessionRenewCheckInterval    = 15 * time.Minute
	sessionRenewStartupDelay     = 2 * time.Minute
	sessionRenewQuietInterval    = 6 * time.Hour
	sessionRenewNearExpiryWindow = 24 * time.Hour
	sessionRenewExpiringInterval = 2 * time.Hour
	sessionRenewMinCooldown      = 30 * time.Minute
	sessionRenewJitterMax        = 30 * time.Minute
)

// SessionRefresher is implemented by a Site that can quietly refresh an
// account's session cookies over HTTP through the account's proxy route
// without launching Chrome.
type SessionRefresher interface {
	RefreshSession(ctx context.Context, client *http.Client, rec Record) (RefreshResult, error)
}

// RefreshResult describes one background HTTP session renewal attempt.
type RefreshResult struct {
	// Cookies replaces the account's backed-up cookies when non-empty.
	Cookies []browser.Cookie
	// SignedOut indicates an explicit server rejection (e.g. HTTP 401/403 API
	// JSON). Transient network, proxy, or CDN challenge errors leave it false.
	SignedOut bool
	Message   string
}

func (s *Service) startSessionRenewal(ctx context.Context) {
	s.goBackground(func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(sessionRenewStartupDelay):
		}
		ticker := time.NewTicker(sessionRenewCheckInterval)
		defer ticker.Stop()
		s.RenewDueSessions(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.RenewDueSessions(ctx)
			}
		}
	})
}

// RenewDueSessions refreshes backed-up session cookies over HTTP for signed-in
// accounts whose browser is closed and whose cookies are due or nearing expiry.
func (s *Service) RenewDueSessions(ctx context.Context) {
	records, err := s.store.list()
	if err != nil {
		return
	}
	now := s.now()
	for _, rec := range records {
		if ctx.Err() != nil {
			return
		}
		site, sErr := s.site(rec.Provider)
		if sErr != nil {
			continue
		}
		refresher, ok := site.(SessionRefresher)
		if !ok {
			continue
		}
		if !s.shouldRenewSession(rec, now) {
			continue
		}
		s.renewOneSession(ctx, rec, refresher, now)
	}
}

func (s *Service) shouldRenewSession(rec Record, now time.Time) bool {
	if rec.Status != StatusSignedIn || !rec.Enabled || len(rec.Cookies) == 0 {
		return false
	}
	if s.InUse(rec.ID) || s.pool.Running(rec.ProfileDir) != nil {
		return false
	}
	s.mu.Lock()
	lastAttempt := s.sessionRenewAttempt[rec.ID]
	s.mu.Unlock()

	if !lastAttempt.IsZero() && now.Sub(lastAttempt) < sessionRenewMinCooldown {
		return false
	}
	lastTouch := lastAttempt
	if t, err := time.Parse(time.RFC3339, rec.LastCheckedAt); err == nil && t.After(lastTouch) {
		lastTouch = t
	}
	jitter := sessionRenewJitter(rec.ID)
	if exp, ok := earliestAuthCookieExpiry(rec.Cookies); ok && exp.Sub(now) < sessionRenewNearExpiryWindow {
		return lastTouch.IsZero() || now.Sub(lastTouch) >= sessionRenewExpiringInterval+jitter/4
	}
	return lastTouch.IsZero() || now.Sub(lastTouch) >= sessionRenewQuietInterval+jitter
}

func (s *Service) renewOneSession(ctx context.Context, rec Record, refresher SessionRefresher, now time.Time) {
	s.mu.Lock()
	if s.sessionRenewAttempt == nil {
		s.sessionRenewAttempt = make(map[string]time.Time)
	}
	s.sessionRenewAttempt[rec.ID] = now
	s.mu.Unlock()

	client, cleanup, err := s.httpClientForAccount(ctx, rec)
	if err != nil {
		logger.Get().Debug("skip session renew without proxy route", "account", rec.ID, "error", err)
		return
	}
	defer cleanup()

	policy := s.cfg.Get().Accounts
	timeout := time.Duration(policy.RequestTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	res, err := refresher.RefreshSession(reqCtx, client, rec)
	if err != nil {
		logger.Get().Debug("quiet session renew failed", "account", rec.ID, "provider", rec.Provider, "error", err)
		return
	}
	// Do not overwrite live Chrome state if Chrome opened or a job started in
	// the meantime.
	if s.InUse(rec.ID) || s.pool.Running(rec.ProfileDir) != nil {
		return
	}
	nowUTC := now.UTC().Format(time.RFC3339)
	if res.SignedOut {
		msg := strings.TrimSpace(res.Message)
		if msg == "" {
			msg = "Phiên đăng nhập đã hết hạn. Bấm Mở Chrome để đăng nhập lại."
		}
		_, _ = s.Update(rec.ID, func(r *Record) {
			if r.Status != StatusSignedIn {
				return
			}
			r.Status = StatusSignedOut
			r.Message = msg
			r.LastCheckedAt = nowUTC
		})
		return
	}
	if len(res.Cookies) == 0 {
		return
	}
	merged := preserveSessionCookieExpiry(rec.Cookies, res.Cookies, now)
	persisted := browser.PersistSessionCookies(merged, now, browser.DefaultSessionCookieTTL)
	if cookiesEqual(rec.Cookies, persisted) && cookiesExpiryEqual(rec.Cookies, persisted) {
		return
	}
	version := uuid.NewString()
	_, _ = s.Update(rec.ID, func(r *Record) {
		if r.Status != StatusSignedIn {
			return
		}
		r.Cookies = persisted
		r.CookieVersion = version
		// Leave cookieMarker on disk at the previous version so the next
		// Chrome launch injects the freshly rotated cookies before navigation.
	})
}

func (s *Service) httpClientForAccount(ctx context.Context, rec Record) (*http.Client, func(), error) {
	route, err := s.route(ctx, rec)
	if err != nil {
		return nil, nil, err
	}
	policy := s.cfg.Get().Accounts
	timeout := time.Duration(policy.RequestTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ForceAttemptHTTP2:     true,
	}
	if route.Key != proxy.DirectRoute {
		if route.URL == nil {
			s.pruneBridges()
			return nil, nil, errors.New("proxy route missing URL")
		}
		transport.Proxy = http.ProxyURL(route.URL)
	}
	cleanup := func() {
		transport.CloseIdleConnections()
		if s.pool.Running(rec.ProfileDir) == nil {
			s.pruneBridges()
		}
	}
	return &http.Client{Transport: transport, Timeout: timeout}, cleanup, nil
}

func sessionRenewJitter(id string) time.Duration {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	minutes := h.Sum32() % uint32(sessionRenewJitterMax/time.Minute)
	return time.Duration(minutes) * time.Minute
}

// preserveSessionCookieExpiry keeps existing synthetic expiration timestamps
// on session cookies (Expires <= 0 in incoming) when the value is unchanged and
// still comfortably outside sessionRenewNearExpiryWindow, avoiding noisy
// CookieVersion churn during quiet HTTP renewals.
func preserveSessionCookieExpiry(existing, incoming []browser.Cookie, now time.Time) []browser.Cookie {
	out := make([]browser.Cookie, len(incoming))
	minKeep := float64(now.Add(sessionRenewNearExpiryWindow).Unix())
	for i, c := range incoming {
		if c.Expires <= 0 {
			for _, old := range existing {
				if old.Name == c.Name && old.Domain == c.Domain && old.Path == c.Path &&
					old.Value == c.Value && old.Expires > minKeep {
					c.Expires = old.Expires
					break
				}
			}
		}
		out[i] = c
	}
	return out
}

func earliestAuthCookieExpiry(cookies []browser.Cookie) (time.Time, bool) {
	var earliest time.Time
	found := false
	for _, c := range cookies {
		if c.Expires <= 0 || !isAuthSessionCookieName(c.Name) {
			continue
		}
		exp := time.Unix(int64(c.Expires), 0)
		if !found || exp.Before(earliest) {
			earliest = exp
			found = true
		}
	}
	return earliest, found
}

func isAuthSessionCookieName(name string) bool {
	lower := strings.ToLower(name)
	for _, ignore := range []string{"__cf_bm", "cf_clearance", "_ga", "_gid", "_gat", "mp_", "ph_"} {
		if lower == ignore || strings.HasPrefix(lower, ignore) {
			return false
		}
	}
	return strings.Contains(lower, "sess") ||
		strings.Contains(lower, "auth") ||
		strings.Contains(lower, "token") ||
		strings.Contains(lower, "sid") ||
		strings.HasPrefix(name, "__Host-") ||
		strings.HasPrefix(name, "__Secure-") ||
		strings.HasPrefix(lower, "hatch_")
}

func cookiesExpiryEqual(a, b []browser.Cookie) bool {
	if len(a) != len(b) {
		return false
	}
	byKey := make(map[string]float64, len(a))
	for _, c := range a {
		byKey[c.Domain+"\x00"+c.Path+"\x00"+c.Name] = c.Expires
	}
	for _, c := range b {
		prev, ok := byKey[c.Domain+"\x00"+c.Path+"\x00"+c.Name]
		if !ok {
			return false
		}
		if int64(prev) != int64(c.Expires) {
			return false
		}
	}
	return true
}

func (s *Service) browserClosing(profileDir string, b *browser.Browser) {
	if b == nil || b.Port <= 0 {
		return
	}
	records, err := s.store.list()
	if err != nil {
		return
	}
	clean := filepath.Clean(profileDir)
	for _, rec := range records {
		if filepath.Clean(rec.ProfileDir) == clean {
			syncCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			s.SyncCookies(syncCtx, rec.ID, b.Port)
			cancel()
		}
	}
}
