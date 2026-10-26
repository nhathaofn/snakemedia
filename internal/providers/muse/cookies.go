package muse

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Cookie is one imported muse.ai cookie. Cookies stay in Go and the account's
// own Chrome profile; they never cross the Wails bridge.
type Cookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Secure   bool    `json:"secure"`
	HTTPOnly bool    `json:"httpOnly"`
	Expires  float64 `json:"expires,omitempty"`
}

const museCookieDomain = ".muse.ai"

// museDomain reports whether a cookie domain belongs to muse.ai. Cookies for
// any other site are dropped so an import cannot plant them in the profile.
func museDomain(domain string) bool {
	d := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(domain), "."))
	return d == "muse.ai" || strings.HasSuffix(d, ".muse.ai")
}

// ParseCookies reads cookies exported from a signed-in muse.ai page: a
// Cookie-Editor JSON array (or {"cookies": [...]}), a {"name": "value"} object,
// a Netscape cookies.txt, or a "a=b; c=d" header.
func ParseCookies(raw string) ([]Cookie, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil, errors.New("Chưa dán cookie.")
	}
	var cookies []Cookie
	switch {
	case strings.HasPrefix(text, "[") || strings.HasPrefix(text, "{"):
		parsed, err := parseJSONCookies(text)
		if err != nil {
			return nil, err
		}
		cookies = parsed
	case strings.Contains(text, "\t"):
		cookies = parseNetscapeCookies(text)
	default:
		cookies = parseHeaderCookies(text)
	}
	kept := cookies[:0]
	for _, c := range cookies {
		if c.Name == "" || !museDomain(c.Domain) {
			continue
		}
		if c.Path == "" {
			c.Path = "/"
		}
		kept = append(kept, c)
	}
	if len(kept) == 0 {
		return nil, errors.New("Không tìm thấy cookie nào của muse.ai trong nội dung đã dán.")
	}
	return kept, nil
}

func parseJSONCookies(text string) ([]Cookie, error) {
	type exported struct {
		Name           string   `json:"name"`
		Value          *string  `json:"value"`
		Domain         string   `json:"domain"`
		Path           string   `json:"path"`
		Secure         *bool    `json:"secure"`
		HTTPOnly       bool     `json:"httpOnly"`
		ExpirationDate *float64 `json:"expirationDate"`
		Expires        *float64 `json:"expires"`
	}
	var list []exported
	if strings.HasPrefix(text, "[") {
		if err := json.Unmarshal([]byte(text), &list); err != nil {
			return nil, errors.New("Cookie JSON không hợp lệ.")
		}
	} else {
		var wrapped struct {
			Cookies []exported `json:"cookies"`
		}
		if err := json.Unmarshal([]byte(text), &wrapped); err == nil && wrapped.Cookies != nil {
			list = wrapped.Cookies
		} else {
			var pairs map[string]any
			if err := json.Unmarshal([]byte(text), &pairs); err != nil {
				return nil, errors.New("Cookie JSON không hợp lệ.")
			}
			cookies := make([]Cookie, 0, len(pairs))
			for name, value := range pairs {
				cookies = append(cookies, Cookie{Name: name, Value: jsonScalar(value), Domain: museCookieDomain, Path: "/", Secure: true})
			}
			return cookies, nil
		}
	}
	cookies := make([]Cookie, 0, len(list))
	for _, c := range list {
		if c.Value == nil {
			continue
		}
		cookie := Cookie{Name: c.Name, Value: *c.Value, Domain: c.Domain, Path: c.Path, Secure: c.Secure == nil || *c.Secure, HTTPOnly: c.HTTPOnly}
		if cookie.Domain == "" {
			cookie.Domain = museCookieDomain
		}
		if c.ExpirationDate != nil {
			cookie.Expires = *c.ExpirationDate
		} else if c.Expires != nil && *c.Expires > 0 {
			cookie.Expires = *c.Expires
		}
		cookies = append(cookies, cookie)
	}
	return cookies, nil
}

func jsonScalar(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(value)
	default:
		data, _ := json.Marshal(value)
		return string(data)
	}
}

// parseNetscapeCookies reads cookies.txt lines: domain, flag, path, secure,
// expiry, name, value separated by tabs.
func parseNetscapeCookies(text string) []Cookie {
	var cookies []Cookie
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		httpOnly := strings.HasPrefix(line, "#HttpOnly_")
		line = strings.TrimPrefix(line, "#HttpOnly_")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 7 {
			continue
		}
		expires, _ := strconv.ParseFloat(f[4], 64)
		cookies = append(cookies, Cookie{Name: f[5], Value: strings.TrimSpace(f[6]), Domain: f[0], Path: f[2], Secure: strings.EqualFold(f[3], "TRUE"), HTTPOnly: httpOnly, Expires: expires})
	}
	return cookies
}

func parseHeaderCookies(text string) []Cookie {
	text = strings.TrimSpace(text)
	if len(text) > 7 && strings.EqualFold(text[:7], "cookie:") {
		text = text[7:]
	}
	var cookies []Cookie
	for _, part := range strings.FieldsFunc(text, func(r rune) bool { return r == ';' || r == '\n' }) {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || strings.TrimSpace(name) == "" {
			continue
		}
		cookies = append(cookies, Cookie{Name: strings.TrimSpace(name), Value: strings.TrimSpace(value), Domain: museCookieDomain, Path: "/", Secure: true})
	}
	return cookies
}

// injectCookies writes imported cookies into the browser's profile through any
// open tab, before muse.ai is loaded.
func injectCookies(ctx context.Context, port int, cookies []Cookie) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var targets []target
	if err := devtoolsJSON(ctx, "GET", port, "/json", &targets); err != nil {
		return err
	}
	for _, t := range targets {
		if t.Type != "page" {
			continue
		}
		conn, err := dialTarget(ctx, t, port)
		if err != nil {
			return err
		}
		defer conn.Close()
		params := make([]map[string]any, 0, len(cookies))
		for _, c := range cookies {
			cookie := map[string]any{"name": c.Name, "value": c.Value, "domain": c.Domain, "path": c.Path, "secure": c.Secure, "httpOnly": c.HTTPOnly}
			if c.Expires > 0 {
				cookie["expires"] = c.Expires
			}
			params = append(params, cookie)
		}
		return conn.call(ctx, "Network.setCookies", map[string]any{"cookies": params}, nil)
	}
	return errors.New("Chrome của tài khoản chưa có tab để nạp cookie.")
}
