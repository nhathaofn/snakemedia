package browser

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"SnakeMedia/internal/config"
)

// Site describes the page a tab must show: its home URL, which URLs belong to
// it, and a script installed before the page's own scripts ("" for none).
type Site struct {
	Home string
	Owns func(*url.URL) bool
	Boot string
}

// FindTab returns the first tab that shows the site.
func FindTab(ctx context.Context, port int, policy config.AccountConfig, owns func(*url.URL) bool) (Target, bool, error) {
	targets, err := Targets(ctx, port, policy)
	if err != nil {
		return Target{}, false, err
	}
	for _, t := range targets {
		if u, ok := t.Page(); ok && owns(u) {
			return t, true, nil
		}
	}
	return Target{}, false, nil
}

// EnsureTab makes sure the browser shows the site, reusing a blank tab when
// there is one and opening a new tab otherwise.
func EnsureTab(ctx context.Context, port int, policy config.AccountConfig, site Site) error {
	targets, err := Targets(ctx, port, policy)
	if err != nil {
		return err
	}
	var blank *Target
	for i, t := range targets {
		u, ok := t.Page()
		if !ok {
			continue
		}
		if site.Owns(u) {
			return nil
		}
		if blank == nil && (t.URL == "about:blank" || t.URL == "chrome://newtab/") {
			blank = &targets[i]
		}
	}
	if blank == nil {
		created, err := NewTab(ctx, port, policy)
		if err != nil {
			return err
		}
		blank = &created
	}
	return Navigate(ctx, *blank, port, site)
}

var lastCleanUA atomic.Value

// RecordUserAgent stores the browser's clean desktop User-Agent (with any
// HeadlessChrome/ marker normalized to Chrome/) so provider HTTP requests can
// reuse the installed Chrome's exact identity.
func RecordUserAgent(raw string) {
	if raw == "" {
		return
	}
	clean := strings.ReplaceAll(raw, "HeadlessChrome/", "Chrome/")
	lastCleanUA.Store(clean)
}

// LastUserAgent returns the most recently observed clean Chrome User-Agent, or
// "" if no Chrome instance has been inspected yet.
func LastUserAgent() string {
	if v, ok := lastCleanUA.Load().(string); ok {
		return v
	}
	return ""
}

// normalizeHeadlessUserAgent strips Chrome's "HeadlessChrome/" marker from
// CDP Emulation when running headless=new, using the live browser version from
// /json/version so sites see the same browser identity as visible Chrome.
func normalizeHeadlessUserAgent(ctx context.Context, conn *Conn, port int) {
	if port <= 0 {
		return
	}
	var v browserVersion
	if err := LocalJSON(ctx, port, "/json/version", config.DefaultAccountConfig(), &v); err != nil {
		return
	}
	if v.UserAgent == "" {
		return
	}
	RecordUserAgent(v.UserAgent)
	if !strings.Contains(v.UserAgent, "HeadlessChrome/") {
		return
	}
	ua := strings.ReplaceAll(v.UserAgent, "HeadlessChrome/", "Chrome/")
	_ = conn.Call(ctx, "Emulation.setUserAgentOverride", map[string]any{"userAgent": ua}, nil)
}

// Navigate loads the site's home page in tab t, installing its boot script first.
func Navigate(ctx context.Context, t Target, port int, site Site) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	conn, err := Dial(ctx, t, port, 0)
	if err != nil {
		return err
	}
	defer conn.Close()
	normalizeHeadlessUserAgent(ctx, conn, port)
	if site.Boot != "" {
		_ = conn.Call(ctx, "Page.enable", nil, nil)
		if err := conn.Call(ctx, "Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": site.Boot}, nil); err != nil {
			return err
		}
	}
	var res struct {
		ErrorText string `json:"errorText"`
	}
	if err := conn.Call(ctx, "Page.navigate", map[string]any{"url": site.Home}, &res); err != nil {
		return err
	}
	// ERR_ABORTED is a navigation replaced by another (a redirect, a script),
	// not a failure to reach the site.
	if res.ErrorText != "" && res.ErrorText != "net::ERR_ABORTED" {
		return &NavigateError{URL: site.Home, Text: res.ErrorText}
	}
	if site.Boot != "" {
		_ = conn.Call(ctx, "Runtime.evaluate", map[string]any{"expression": site.Boot}, nil)
	}
	return nil
}

// NavigateError is a page that Chrome could not load (net::ERR_…).
type NavigateError struct {
	URL  string
	Text string
}

func (e *NavigateError) Error() string {
	if IsProxyError(e) {
		return "Không kết nối được qua proxy (" + e.Text + "). Kiểm tra proxy của tài khoản."
	}
	return "Không mở được " + e.URL + " (" + e.Text + ")."
}

// proxyErrors are Chrome's net errors that blame the proxy rather than the
// site or the browser.
var proxyErrors = []string{"net::ERR_PROXY_", "net::ERR_TUNNEL_", "net::ERR_SOCKS_", "net::ERR_NO_SUPPORTED_PROXIES", "net::ERR_MANDATORY_PROXY_"}

// IsProxyError reports whether err is a page load that failed because of the
// proxy, as opposed to Chrome failing to start or the site being slow.
func IsProxyError(err error) bool {
	var nav *NavigateError
	if !errors.As(err, &nav) {
		return false
	}
	for _, prefix := range proxyErrors {
		if strings.HasPrefix(nav.Text, prefix) {
			return true
		}
	}
	return false
}
