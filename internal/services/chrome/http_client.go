package chrome

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"SnakeMedia/internal/config"
)

type HTTPError struct {
	Status     int
	RetryAfter time.Duration
}

func (e *HTTPError) Error() string { return fmt.Sprintf("Flow trả về HTTP %d", e.Status) }
func responseError(status int, retry string) error {
	var delay time.Duration
	if seconds, err := strconv.ParseInt(retry, 10, 64); err == nil && seconds > 0 {
		delay = time.Duration(min(seconds, int64(time.Hour/time.Second))) * time.Second
	} else if date, err := http.ParseTime(retry); err == nil {
		delay = min(max(time.Until(date), 0), time.Hour)
	}
	return &HTTPError{Status: status, RetryAfter: delay}
}

var requestID atomic.Uint64

func rpcParams(c Credentials) url.Values {
	params := url.Values{"rpcids": {c.RPCs.ids()}, "source-path": {c.SourcePath}, "bl": {c.BuildLabel}, "f.sid": {c.SessionID}, "hl": {c.Language}, "_reqid": {strconv.FormatUint(requestID.Add(1), 10)}, "rt": {"c"}}
	if c.AuthUser != "" {
		params.Set("authuser", c.AuthUser)
	}
	return params
}
func rpcBody(c Credentials) url.Values {
	batch := []any{[]any{c.RPCs.Credits, "[]", nil, "generic"}}
	if c.RPCs.Availability != "" {
		batch = append(batch, []any{c.RPCs.Availability, "[]", nil, "generic"})
	}
	envelope, _ := json.Marshal([]any{batch})
	return url.Values{"f.req": {string(envelope)}, "at": {c.AtToken}}
}

func readBounded(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("response exceeds configured size limit")
	}
	return data, nil
}

// QueryCreditsDirectHTTP applies cookie scope/expiry and retains Set-Cookie
// rotation, using only credentials obtained from the matching browser profile.
func QueryCreditsDirectHTTP(ctx context.Context, c Credentials, policy config.AccountConfig) (FlowCredits, Credentials, error) {
	if err := c.validate(); err != nil {
		return FlowCredits{}, c, err
	}
	c.Cookies = append([]CookieItem(nil), c.Cookies...)
	origin, _ := url.Parse(FlowURL)
	jar, _ := cookiejar.New(nil)
	var cookies []*http.Cookie
	for _, item := range c.Cookies {
		if item.Domain != ".google.com" && item.Domain != "google.com" && item.Domain != ".flow.google.com" && item.Domain != "flow.google.com" {
			continue
		}
		cookie := &http.Cookie{Name: item.Name, Value: item.Value, Domain: item.Domain, Path: item.Path, Secure: item.Secure, HttpOnly: item.HTTPOnly}
		if item.Expires > 0 {
			cookie.Expires = time.Unix(int64(item.Expires), 0)
		}
		cookies = append(cookies, cookie)
	}
	jar.SetCookies(origin, cookies)
	if len(jar.Cookies(origin)) == 0 {
		return FlowCredits{}, c, errors.New("Phiên Flow không có cookie còn hiệu lực.")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, FlowURL+batchPath+"?"+rpcParams(c).Encode(), strings.NewReader(rpcBody(c).Encode()))
	if err != nil {
		return FlowCredits{}, c, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	req.Header.Set("X-Same-Domain", "1")
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept-Language", c.Language)
	req.Header.Set("Referer", FlowURL+c.SourcePath)
	client := &http.Client{Timeout: time.Duration(policy.RequestTimeoutSeconds) * time.Second, Jar: jar, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return FlowCredits{}, c, ctx.Err()
		}
		// net/url errors include the request URL and its session query parameters.
		// Surface a useful diagnostic without exposing runtime credentials to React.
		return FlowCredits{}, c, errors.New("Không kết nối được API Flow. Kiểm tra mạng hoặc thử cập nhật lại sau.")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return FlowCredits{}, c, responseError(resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	body, err := readBounded(resp.Body, policy.MaxResponseBytes)
	if err != nil {
		return FlowCredits{}, c, err
	}
	credits, err := ParseCreditsFromText(string(body), c.RPCs.Credits)
	if err != nil {
		return FlowCredits{}, c, err
	}
	if c.RPCs.Availability != "" {
		// Optional capability metadata cannot invalidate a valid credits result.
		if availability, parseErr := parseRPCPayload(string(body), c.RPCs.Availability); parseErr == nil {
			credits.AvailabilityCode, _ = parseAvailability(availability)
		}
	}
	for _, cookie := range resp.Cookies() {
		domain := cookie.Domain
		if domain == "" {
			domain = origin.Hostname()
		}
		path := cookie.Path
		if path == "" {
			path = batchPath[:strings.LastIndex(batchPath, "/")]
		}
		updated := CookieItem{Name: cookie.Name, Value: cookie.Value, Domain: domain, Path: path, Secure: cookie.Secure, HTTPOnly: cookie.HttpOnly}
		if !cookie.Expires.IsZero() {
			updated.Expires = float64(cookie.Expires.Unix())
		}
		if cookie.MaxAge > 0 {
			updated.Expires = float64(time.Now().Add(time.Duration(cookie.MaxAge) * time.Second).Unix())
		}
		filtered := c.Cookies[:0]
		for _, old := range c.Cookies {
			if old.Name != updated.Name || strings.TrimPrefix(old.Domain, ".") != strings.TrimPrefix(domain, ".") || old.Path != path {
				filtered = append(filtered, old)
			}
		}
		c.Cookies = filtered
		if cookie.MaxAge >= 0 {
			c.Cookies = append(c.Cookies, updated)
		}
	}
	return credits, c, nil
}
