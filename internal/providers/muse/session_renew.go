package muse

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/browser"
)

var _ accounts.SessionRefresher = (*Site)(nil)

func (s *Site) sessionEndpoint() string {
	if s.sessionURL != "" {
		return s.sessionURL
	}
	return strings.TrimRight(MuseURL, "/") + "/api/session"
}

func (s *Site) renewUserAgent() string {
	if s.userAgent != nil {
		return s.userAgent()
	}
	return browser.LastUserAgent()
}

// RefreshSession sends one quiet HTTP GET to /api/session through the
// account's proxy-routed HTTP client so rotating Muse cookies stay alive while
// Chrome is closed, without triggering page reloads or websocket wakeups.
func (s *Site) RefreshSession(ctx context.Context, client *http.Client, rec accounts.Record) (accounts.RefreshResult, error) {
	endpoint := s.sessionEndpoint()
	target, err := url.Parse(endpoint)
	if err != nil {
		return accounts.RefreshResult{}, err
	}
	now := s.nowFn()
	cookieHeader, hasExpiredAuth := buildCookieHeader(rec.Cookies, target, now)
	if cookieHeader == "" {
		if hasExpiredAuth {
			return accounts.RefreshResult{
				SignedOut: true,
				Message:   "Cookie phiên Muse đã hết hạn. Bấm Mở Chrome để đăng nhập lại.",
			}, nil
		}
		return accounts.RefreshResult{}, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return accounts.RefreshResult{}, err
	}
	req.Header.Set("Cookie", cookieHeader)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", MuseURL)
	if ua := s.renewUserAgent(); ua != "" {
		req.Header.Set("User-Agent", ua)
	}

	resp, err := client.Do(req)
	if err != nil {
		return accounts.RefreshResult{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return accounts.RefreshResult{}, err
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	isJSON := strings.Contains(contentType, "application/json")
	if strings.TrimSpace(resp.Header.Get("cf-mitigated")) != "" || (!isJSON && looksLikeHTMLBody(body)) {
		return accounts.RefreshResult{}, fmt.Errorf("muse session refresh hit challenge/html (HTTP %d)", resp.StatusCode)
	}

	merged := mergeSetCookies(rec.Cookies, resp.Cookies(), target.Hostname(), s.AcceptsCookie, now)

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return accounts.RefreshResult{
			Cookies:   merged,
			SignedOut: true,
			Message:   "Phiên Muse đã hết hạn. Bấm Mở Chrome để đăng nhập lại.",
		}, nil
	case http.StatusForbidden:
		if isJSON {
			return accounts.RefreshResult{
				Cookies:   merged,
				SignedOut: true,
				Message:   "Phiên Muse đã bị từ chối. Bấm Mở Chrome để đăng nhập lại.",
			}, nil
		}
		return accounts.RefreshResult{}, fmt.Errorf("muse session refresh blocked (HTTP 403)")
	case http.StatusOK:
		if !isJSON {
			return accounts.RefreshResult{}, fmt.Errorf("muse session refresh expected json, got %q", contentType)
		}
		var payload map[string]any
		if json.Unmarshal(body, &payload) == nil && payload != nil {
			if auth, ok := payload["authenticated"].(bool); ok && !auth {
				return accounts.RefreshResult{
					Cookies:   merged,
					SignedOut: true,
					Message:   "Phiên Muse đã đăng xuất. Bấm Mở Chrome để đăng nhập lại.",
				}, nil
			}
		}
		return accounts.RefreshResult{Cookies: merged}, nil
	default:
		return accounts.RefreshResult{}, fmt.Errorf("muse session refresh HTTP %d", resp.StatusCode)
	}
}

func buildCookieHeader(cookies []browser.Cookie, target *url.URL, now time.Time) (header string, hasExpired bool) {
	host := strings.ToLower(target.Hostname())
	reqPath := target.Path
	if reqPath == "" {
		reqPath = "/"
	}
	pairs := make([]string, 0, len(cookies))
	for _, c := range cookies {
		if c.Name == "" || !cookieMatchesHost(c.Domain, host) {
			continue
		}
		cPath := c.Path
		if cPath == "" {
			cPath = "/"
		}
		if !strings.HasPrefix(reqPath, cPath) {
			continue
		}
		if c.Expires > 0 && int64(c.Expires) <= now.Unix() {
			hasExpired = true
			continue
		}
		pairs = append(pairs, c.Name+"="+c.Value)
	}
	return strings.Join(pairs, "; "), hasExpired
}

func cookieMatchesHost(domain, host string) bool {
	d := strings.ToLower(strings.TrimSpace(domain))
	if d == "" || host == "" {
		return false
	}
	if d[0] == '.' {
		root := d[1:]
		return host == root || strings.HasSuffix(host, d)
	}
	return host == d || strings.HasSuffix(host, "."+d)
}

func mergeSetCookies(existing []browser.Cookie, incoming []*http.Cookie, defaultHost string, accepts func(string) bool, now time.Time) []browser.Cookie {
	if len(incoming) == 0 {
		return existing
	}
	out := make([]browser.Cookie, len(existing))
	copy(out, existing)
	indexByKey := make(map[string]int, len(out))
	for i, c := range out {
		indexByKey[cookieKey(c.Domain, c.Path, c.Name)] = i
	}
	for _, nc := range incoming {
		if nc == nil || nc.Name == "" {
			continue
		}
		domain := nc.Domain
		if domain == "" {
			domain = defaultHost
		}
		if accepts != nil && !accepts(domain) {
			continue
		}
		path := nc.Path
		if path == "" {
			path = "/"
		}
		var exp float64
		if !nc.Expires.IsZero() {
			exp = float64(nc.Expires.Unix())
		} else if nc.MaxAge > 0 {
			exp = float64(now.Add(time.Duration(nc.MaxAge) * time.Second).Unix())
		} else if nc.MaxAge < 0 {
			exp = float64(now.Add(-time.Second).Unix())
		}
		updated := browser.Cookie{
			Name:     nc.Name,
			Value:    nc.Value,
			Domain:   domain,
			Path:     path,
			Expires:  exp,
			HTTPOnly: nc.HttpOnly,
			Secure:   nc.Secure,
		}
		key := cookieKey(updated.Domain, updated.Path, updated.Name)
		if idx, exists := indexByKey[key]; exists {
			out[idx] = updated
		} else {
			// Also replace same-name cookie on same host with/without leading dot if present.
			replaced := false
			for i, prev := range out {
				if prev.Name == updated.Name && prev.Path == updated.Path && strings.TrimPrefix(prev.Domain, ".") == strings.TrimPrefix(updated.Domain, ".") {
					out[i] = updated
					replaced = true
					break
				}
			}
			if !replaced {
				indexByKey[key] = len(out)
				out = append(out, updated)
			}
		}
	}
	return browser.PersistSessionCookies(out, now, browser.DefaultSessionCookieTTL)
}

func cookieKey(domain, path, name string) string {
	return strings.ToLower(domain) + "\x00" + path + "\x00" + name
}

func looksLikeHTMLBody(body []byte) bool {
	trimmed := strings.ToLower(strings.TrimSpace(string(body)))
	return strings.HasPrefix(trimmed, "<!doctype") || strings.HasPrefix(trimmed, "<html")
}
