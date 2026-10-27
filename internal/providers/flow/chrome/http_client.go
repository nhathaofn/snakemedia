package chrome

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
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
	return batchParams(c, c.RPCs.ids())
}

// batchParams is the runtime query string of every batchexecute call: values come
// from the account's own browser session, never from constants.
func batchParams(c Credentials, rpcIDs string) url.Values {
	params := url.Values{"rpcids": {rpcIDs}, "source-path": {c.SourcePath}, "bl": {c.BuildLabel}, "f.sid": {c.SessionID}, "hl": {c.Language}, "_reqid": {strconv.FormatUint(requestID.Add(1), 10)}, "rt": {"c"}}
	if c.AuthUser != "" {
		params.Set("authuser", c.AuthUser)
	}
	return params
}

func setBatchHeaders(req *http.Request, c Credentials, origin string) {
	// Flow may send a delayed HTTP/1.1 error after a batchexecute response. Do not
	// return this connection to Go's idle pool, where that late response is noisy.
	req.Close = true
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	req.Header.Set("X-Same-Domain", "1")
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept-Language", c.Language)
	req.Header.Set("Referer", origin+c.SourcePath)
}

// cookieInScope keeps only cookies the provider host would receive itself.
func cookieInScope(domain, host string) bool {
	d := strings.TrimPrefix(domain, ".")
	return d == host || (strings.Contains(d, ".") && strings.HasSuffix(host, "."+d))
}

// flowCookieJar loads the provider-scoped, unexpired session cookies for origin.
func flowCookieJar(origin *url.URL, items []CookieItem) (*cookiejar.Jar, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	var cookies []*http.Cookie
	for _, item := range items {
		if !cookieInScope(item.Domain, origin.Hostname()) {
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
		return nil, errors.New("Phiên Flow không có cookie còn hiệu lực.")
	}
	return jar, nil
}

// rotateCookies applies the provider's Set-Cookie rotation to the stored cookies.
func rotateCookies(cookies []CookieItem, origin *url.URL, resp *http.Response) []CookieItem {
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
		filtered := cookies[:0]
		for _, old := range cookies {
			if old.Name != updated.Name || strings.TrimPrefix(old.Domain, ".") != strings.TrimPrefix(domain, ".") || old.Path != path {
				filtered = append(filtered, old)
			}
		}
		cookies = filtered
		if cookie.MaxAge >= 0 {
			cookies = append(cookies, updated)
		}
	}
	return cookies
}
func rpcBody(c Credentials) url.Values {
	batch := []any{[]any{c.RPCs.Credits, "[]", nil, "generic"}}
	if c.RPCs.Availability != "" {
		batch = append(batch, []any{c.RPCs.Availability, "[]", nil, "generic"})
	}
	envelope, _ := json.Marshal([]any{batch})
	return url.Values{"f.req": {string(envelope)}, "at": {c.AtToken}}
}
