package browser

import (
	"context"
	"errors"
	"net/url"
	"strings"
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

// Navigate loads the site's home page in tab t, installing its boot script first.
func Navigate(ctx context.Context, t Target, port int, site Site) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	conn, err := Dial(ctx, t, port, 0)
	if err != nil {
		return err
	}
	defer conn.Close()
	if site.Boot != "" {
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
