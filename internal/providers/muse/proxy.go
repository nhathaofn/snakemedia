package muse

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

type ProxyType string

const (
	ProxyTypeStatic   ProxyType = "static"
	ProxyTypeRotating ProxyType = "rotating"
)

type ProxyStatus string

const (
	ProxyStatusLive         ProxyStatus = "live"
	ProxyStatusRateLimited  ProxyStatus = "rate_limited"
	ProxyStatusLimitReached ProxyStatus = "limit_reached"
	ProxyStatusDead         ProxyStatus = "dead"
)

type ParsedProxy struct {
	Raw      string    `json:"raw"`
	Type     ProxyType `json:"type"`
	Provider string    `json:"provider"` // "static", "proxyvn", "tmproxy", "shoplike", "custom_url"
	Scheme   string    `json:"scheme"`   // "http", "socks5"
	Host     string    `json:"host,omitempty"`
	Port     int       `json:"port,omitempty"`
	User     string    `json:"user,omitempty"`
	Pass     string    `json:"pass,omitempty"`
	Key      string    `json:"key,omitempty"`
	URL      string    `json:"url,omitempty"`
	Error    string    `json:"error,omitempty"`
}

type ProxyCheckResult struct {
	Raw               string      `json:"raw"`
	Status            ProxyStatus `json:"status"`
	OK                bool        `json:"ok"`
	IP                string      `json:"ip,omitempty"`
	Port              int         `json:"port,omitempty"`
	LatencyMS         int64       `json:"latencyMs,omitempty"`
	Message           string      `json:"message,omitempty"`
	NextChangeSeconds int         `json:"nextChangeSeconds,omitempty"`
	CheckedAt         string      `json:"checkedAt"`
}

var rotatingPrefixRe = regexp.MustCompile(`(?i)^(proxyvn|tmproxy|shoplike):\s*(.+)$`)
var ipPortRe = regexp.MustCompile(`(\d{1,3}(?:\.\d{1,3}){3}):(\d+)`)

// ParseProxy parses a single proxy line into structured format.
func ParseProxy(line string) *ParsedProxy {
	raw := strings.TrimSpace(line)
	if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(raw, "//") {
		return nil
	}

	// 1. Key xoay có tiền tố: proxyvn: KEY, tmproxy: KEY, shoplike: KEY
	if m := rotatingPrefixRe.FindStringSubmatch(raw); len(m) == 3 {
		prov := strings.ToLower(m[1])
		key := strings.TrimSpace(m[2])
		return &ParsedProxy{
			Raw:      raw,
			Type:     ProxyTypeRotating,
			Provider: prov,
			Key:      key,
		}
	}

	// 2. URL xoay IP (chỉ coi là API xoay nếu có path hoặc query)
	if strings.HasPrefix(strings.ToLower(raw), "http://") || strings.HasPrefix(strings.ToLower(raw), "https://") {
		if u, err := url.Parse(raw); err == nil {
			if (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
				prov := "custom_url"
				lower := strings.ToLower(raw)
				if strings.Contains(lower, "proxy.vn") || strings.Contains(lower, "proxyxoay") {
					prov = "proxyvn"
				} else if strings.Contains(lower, "tmproxy.com") {
					prov = "tmproxy"
				} else if strings.Contains(lower, "shoplike.vn") {
					prov = "shoplike"
				}
				return &ParsedProxy{
					Raw:      raw,
					Type:     ProxyTypeRotating,
					Provider: prov,
					URL:      raw,
				}
			}
		}
	}

	// 3. Static proxies
	// Check scheme
	scheme := "http"
	working := raw
	lowerWorking := strings.ToLower(working)
	if strings.HasPrefix(lowerWorking, "socks5://") || strings.HasPrefix(lowerWorking, "socks5h://") {
		scheme = "socks5"
		working = working[strings.Index(working, "://")+3:]
	} else if strings.HasPrefix(lowerWorking, "http://") || strings.HasPrefix(lowerWorking, "https://") {
		scheme = "http"
		working = working[strings.Index(working, "://")+3:]
	}

	// user:pass@host:port
	if atIdx := strings.LastIndex(working, "@"); atIdx != -1 {
		userInfo := working[:atIdx]
		hostPort := working[atIdx+1:]
		user, pass := userInfo, ""
		if colon := strings.Index(userInfo, ":"); colon != -1 {
			user = userInfo[:colon]
			pass = userInfo[colon+1:]
		}
		host, portStr, err := net.SplitHostPort(hostPort)
		if err != nil {
			return &ParsedProxy{Raw: raw, Error: "Sai định dạng host:port"}
		}
		port, err := strconv.Atoi(portStr)
		if err != nil || port < 1 || port > 65535 {
			return &ParsedProxy{Raw: raw, Error: "Cổng không hợp lệ"}
		}
		return &ParsedProxy{
			Raw:      raw,
			Type:     ProxyTypeStatic,
			Provider: "static",
			Scheme:   scheme,
			Host:     host,
			Port:     port,
			User:     user,
			Pass:     pass,
		}
	}

	// host:port:user:pass or host:port
	parts := strings.Split(working, ":")
	if len(parts) == 4 {
		port, err := strconv.Atoi(parts[1])
		if err != nil || port < 1 || port > 65535 {
			return &ParsedProxy{Raw: raw, Error: "Cổng không hợp lệ"}
		}
		return &ParsedProxy{
			Raw:      raw,
			Type:     ProxyTypeStatic,
			Provider: "static",
			Scheme:   scheme,
			Host:     parts[0],
			Port:     port,
			User:     parts[2],
			Pass:     parts[3],
		}
	}
	if len(parts) == 2 {
		port, err := strconv.Atoi(parts[1])
		if err != nil || port < 1 || port > 65535 {
			return &ParsedProxy{Raw: raw, Error: "Cổng không hợp lệ"}
		}
		return &ParsedProxy{
			Raw:      raw,
			Type:     ProxyTypeStatic,
			Provider: "static",
			Scheme:   scheme,
			Host:     parts[0],
			Port:     port,
		}
	}

	return &ParsedProxy{Raw: raw, Error: "Định dạng không được hỗ trợ"}
}

// checkStatic verifies CONNECT to muse.ai:443 through a static proxy.
func checkStatic(p *ParsedProxy, timeout time.Duration) ProxyCheckResult {
	checkedAt := time.Now().UTC().Format(time.RFC3339)
	if p.Error != "" {
		return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusDead, OK: false, Message: p.Error, CheckedAt: checkedAt}
	}
	t0 := time.Now()

	if p.Scheme == "socks5" {
		var auth *proxy.Auth
		if p.User != "" || p.Pass != "" {
			auth = &proxy.Auth{User: p.User, Password: p.Pass}
		}
		dialer, err := proxy.SOCKS5("tcp", net.JoinHostPort(p.Host, strconv.Itoa(p.Port)), auth, &net.Dialer{Timeout: timeout})
		if err != nil {
			return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusDead, OK: false, Message: err.Error(), CheckedAt: checkedAt}
		}
		conn, err := dialer.Dial("tcp", "muse.ai:443")
		if err != nil {
			return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusDead, OK: false, Message: err.Error(), CheckedAt: checkedAt}
		}
		_ = conn.Close()
		lat := time.Since(t0).Milliseconds()
		return ProxyCheckResult{
			Raw: p.Raw, Status: ProxyStatusLive, OK: true, IP: p.Host, Port: p.Port, LatencyMS: lat,
			Message: fmt.Sprintf("SOCKS5 hoạt động (%dms)", lat), CheckedAt: checkedAt,
		}
	}

	// HTTP CONNECT
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(p.Host, strconv.Itoa(p.Port)), timeout)
	if err != nil {
		return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusDead, OK: false, Message: err.Error(), CheckedAt: checkedAt}
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	req := "CONNECT muse.ai:443 HTTP/1.1\r\nHost: muse.ai:443\r\n"
	if p.User != "" {
		cred := base64.StdEncoding.EncodeToString([]byte(p.User + ":" + p.Pass))
		req += "Proxy-Authorization: Basic " + cred + "\r\n"
	}
	req += "\r\n"

	if _, err := conn.Write([]byte(req)); err != nil {
		return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusDead, OK: false, Message: err.Error(), CheckedAt: checkedAt}
	}

	reader := bufio.NewReader(conn)
	respLine, err := reader.ReadString('\n')
	if err != nil {
		return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusDead, OK: false, Message: "Không đọc được phản hồi: " + err.Error(), CheckedAt: checkedAt}
	}
	lat := time.Since(t0).Milliseconds()
	if strings.Contains(respLine, " 200 ") {
		return ProxyCheckResult{
			Raw: p.Raw, Status: ProxyStatusLive, OK: true, IP: p.Host, Port: p.Port, LatencyMS: lat,
			Message: fmt.Sprintf("HTTP hoạt động (%dms)", lat), CheckedAt: checkedAt,
		}
	}
	if strings.Contains(respLine, " 407 ") {
		return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusDead, OK: false, Message: "Sai thông tin xác thực proxy (407)", CheckedAt: checkedAt}
	}
	return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusDead, OK: false, Message: strings.TrimSpace(respLine), CheckedAt: checkedAt}
}

// fetchRotating calls the provider API to get the current or a new IP. Its
// message reaches the UI, the job journal and the log, so the key is removed
// from it and provider bodies are shortened.
func fetchRotating(ctx context.Context, p *ParsedProxy, requestNew bool) ProxyCheckResult {
	res := fetchRotatingRaw(ctx, p, requestNew)
	res.Message = redactSecret(res.Message, p.Key, p.URL)
	return res
}

func fetchRotatingRaw(ctx context.Context, p *ParsedProxy, requestNew bool) ProxyCheckResult {
	checkedAt := time.Now().UTC().Format(time.RFC3339)
	client := &http.Client{Timeout: 8 * time.Second}

	switch p.Provider {
	case "proxyvn":
		ep := fmt.Sprintf("https://proxyxoay.shop/api/get.php?key=%s", url.QueryEscape(p.Key))
		req, _ := http.NewRequestWithContext(ctx, "GET", ep, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0")
		t0 := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusDead, OK: false, Message: transportMessage(err), CheckedAt: checkedAt}
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		lat := time.Since(t0).Milliseconds()

		var res struct {
			Status  any    `json:"status"`
			Code    int    `json:"code"`
			Message string `json:"message"`
			Proxy   string `json:"proxy"`
			HTTP    string `json:"proxyhttp"`
			Data    struct {
				Proxy string `json:"proxy"`
				HTTP  string `json:"http"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &res); err == nil {
			proxyStr := res.HTTP
			if proxyStr == "" {
				proxyStr = res.Proxy
			}
			if proxyStr == "" {
				proxyStr = res.Data.HTTP
			}
			if proxyStr == "" {
				proxyStr = res.Data.Proxy
			}
			msg := strings.ToLower(res.Message)
			if m := ipPortRe.FindStringSubmatch(proxyStr); len(m) == 3 {
				port, _ := strconv.Atoi(m[2])
				return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusLive, OK: true, IP: m[1], Port: port, LatencyMS: lat, Message: fmt.Sprintf("Live (%dms)", lat), CheckedAt: checkedAt}
			}
			if strings.Contains(msg, "hết hạn") || strings.Contains(msg, "không tồn tại") {
				return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusLimitReached, OK: false, Message: res.Message, CheckedAt: checkedAt}
			}
			if strings.Contains(msg, "đợi") || strings.Contains(msg, "chờ") || strings.Contains(msg, "wait") {
				return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusRateLimited, OK: false, Message: res.Message, CheckedAt: checkedAt}
			}
		}
		if m := ipPortRe.FindStringSubmatch(string(body)); len(m) == 3 {
			port, _ := strconv.Atoi(m[2])
			return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusLive, OK: true, IP: m[1], Port: port, LatencyMS: lat, Message: fmt.Sprintf("Live (%dms)", lat), CheckedAt: checkedAt}
		}
		return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusDead, OK: false, Message: string(body), CheckedAt: checkedAt}

	case "tmproxy":
		ep := "https://api.tmproxy.com/api/proxy/get-current-proxy"
		if requestNew {
			ep = "https://api.tmproxy.com/api/proxy/get-new-proxy"
		}
		payload, _ := json.Marshal(map[string]any{"api_key": p.Key, "id_location": 0})
		req, _ := http.NewRequestWithContext(ctx, "POST", ep, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		t0 := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusDead, OK: false, Message: transportMessage(err), CheckedAt: checkedAt}
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		lat := time.Since(t0).Milliseconds()

		var res struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    struct {
				HTTPS       string `json:"https"`
				SOCKS5      string `json:"socks5"`
				NextRequest int    `json:"next_request"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &res); err == nil {
			if res.Code == 0 {
				proxyStr := res.Data.HTTPS
				if proxyStr == "" {
					proxyStr = res.Data.SOCKS5
				}
				if m := ipPortRe.FindStringSubmatch(proxyStr); len(m) == 3 {
					port, _ := strconv.Atoi(m[2])
					return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusLive, OK: true, IP: m[1], Port: port, LatencyMS: lat, Message: fmt.Sprintf("Live (%dms)", lat), NextChangeSeconds: res.Data.NextRequest, CheckedAt: checkedAt}
				}
			}
			msg := strings.ToLower(res.Message)
			if res.Code == 1 || strings.Contains(msg, "hết hạn") || strings.Contains(msg, "expired") {
				return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusLimitReached, OK: false, Message: res.Message, CheckedAt: checkedAt}
			}
			if res.Code == 5 || strings.Contains(msg, "đợi") || strings.Contains(msg, "wait") {
				return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusRateLimited, OK: false, Message: res.Message, CheckedAt: checkedAt}
			}
		}
		return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusDead, OK: false, Message: string(body), CheckedAt: checkedAt}

	case "shoplike":
		ep := fmt.Sprintf("http://api.shoplike.vn/rest/proxy/getcurrentproxy?key=%s", url.QueryEscape(p.Key))
		if requestNew {
			ep = fmt.Sprintf("http://api.shoplike.vn/rest/proxy/getnewproxy?key=%s", url.QueryEscape(p.Key))
		}
		req, _ := http.NewRequestWithContext(ctx, "GET", ep, nil)
		t0 := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusDead, OK: false, Message: transportMessage(err), CheckedAt: checkedAt}
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		lat := time.Since(t0).Milliseconds()

		var res struct {
			Status string `json:"status"`
			Mess   string `json:"mess"`
			Data   struct {
				Proxy string `json:"proxy"`
				Time  int    `json:"time"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &res); err == nil {
			if res.Status == "success" {
				if m := ipPortRe.FindStringSubmatch(res.Data.Proxy); len(m) == 3 {
					port, _ := strconv.Atoi(m[2])
					return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusLive, OK: true, IP: m[1], Port: port, LatencyMS: lat, Message: fmt.Sprintf("Live (%dms)", lat), CheckedAt: checkedAt}
				}
			}
			msg := strings.ToLower(res.Mess)
			if strings.Contains(msg, "hết hạn") || strings.Contains(msg, "không tồn tại") {
				return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusLimitReached, OK: false, Message: res.Mess, CheckedAt: checkedAt}
			}
			if strings.Contains(msg, "đợi") || strings.Contains(msg, "chờ") {
				return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusRateLimited, OK: false, Message: res.Mess, CheckedAt: checkedAt}
			}
		}
		return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusDead, OK: false, Message: string(body), CheckedAt: checkedAt}

	default: // custom_url
		req, _ := http.NewRequestWithContext(ctx, "GET", p.URL, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0")
		t0 := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusDead, OK: false, Message: transportMessage(err), CheckedAt: checkedAt}
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		lat := time.Since(t0).Milliseconds()

		if m := ipPortRe.FindStringSubmatch(string(body)); len(m) == 3 {
			port, _ := strconv.Atoi(m[2])
			return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusLive, OK: true, IP: m[1], Port: port, LatencyMS: lat, Message: fmt.Sprintf("Live (%dms)", lat), CheckedAt: checkedAt}
		}
		return ProxyCheckResult{Raw: p.Raw, Status: ProxyStatusDead, OK: false, Message: string(body), CheckedAt: checkedAt}
	}
}

// CheckProxy runs a health check on any parsed proxy.
func CheckProxy(ctx context.Context, p *ParsedProxy, requestNew bool) ProxyCheckResult {
	if p == nil {
		return ProxyCheckResult{Status: ProxyStatusDead, OK: false, Message: "Proxy rỗng", CheckedAt: time.Now().UTC().Format(time.RFC3339)}
	}
	if p.Type == ProxyTypeRotating {
		return fetchRotating(ctx, p, requestNew)
	}
	return checkStatic(p, 8*time.Second)
}

func parseProxyList(text string) []string {
	var list []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "//") && !seen[trimmed] {
			seen[trimmed] = true
			list = append(list, trimmed)
		}
	}
	return list
}

// pickProxy selects a proxy for an account following HaoHao rules:
// - "none" disables proxy for the account;
// - explicit line uses that proxy;
// - empty auto-selects and pins;
// - if all proxies in the list are dead, returns error (never falls back to real machine IP).
func pickProxy(acc storedAccount, accounts []storedAccount, text string, dead map[string]time.Time, now time.Time) (*ParsedProxy, string, error) {
	if acc.Proxy == "none" {
		return nil, "", nil
	}
	if acc.Proxy != "" {
		p := ParseProxy(acc.Proxy)
		if p == nil || p.Error != "" {
			return nil, "", fmt.Errorf("Proxy của %s không hợp lệ: %s", acc.Name, maskProxyLine(acc.Proxy))
		}
		return p, "", nil
	}

	lines := parseProxyList(text)
	if len(lines) == 0 {
		// An account that used a proxy from the list must not quietly go out
		// on the real IP once the list is emptied; only "none" means direct.
		if acc.ProxyAuto != "" {
			return nil, "", fmt.Errorf("Danh sách proxy đang trống nhưng %s vẫn được gán proxy. Thêm proxy hoặc chọn \"Không dùng proxy\" cho tài khoản này.", acc.Name)
		}
		return nil, "", nil
	}

	isDead := func(line string) bool {
		if until, ok := dead[line]; ok && until.After(now) {
			return true
		}
		return false
	}

	var live []string
	for _, l := range lines {
		if !isDead(l) {
			live = append(live, l)
		}
	}

	if acc.ProxyAuto != "" {
		for _, l := range live {
			if l == acc.ProxyAuto {
				return ParseProxy(acc.ProxyAuto), acc.ProxyAuto, nil
			}
		}
	}

	if len(live) == 0 {
		return nil, "", errors.New("Tất cả proxy đều chết — kiểm tra lại danh sách proxy")
	}

	usedCount := func(line string) int {
		count := 0
		for _, a := range accounts {
			if a.ID != acc.ID {
				active := a.Proxy
				if active == "" {
					active = a.ProxyAuto
				}
				if active == line {
					count++
				}
			}
		}
		return count
	}

	best := live[0]
	bestUsed := usedCount(best)
	for _, l := range live[1:] {
		if u := usedCount(l); u < bestUsed {
			best = l
			bestUsed = u
		}
	}

	return ParseProxy(best), best, nil
}

// transportMessage describes a failed provider request without its URL, which
// carries the account's key.
func transportMessage(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	return "Không kết nối được nhà cung cấp proxy: " + err.Error()
}

// maxProviderMessage caps provider text shown to the user (bodies may be HTML).
const maxProviderMessage = 160

// redactSecret removes secrets from provider text and shortens it.
func redactSecret(msg string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			msg = strings.ReplaceAll(msg, secret, "***")
		}
	}
	return truncate(strings.Join(strings.Fields(msg), " "), maxProviderMessage)
}

// maskProxyLine shows which proxy a line names without its password or key.
func maskProxyLine(line string) string {
	p := ParseProxy(line)
	switch {
	case p == nil || p.Error != "":
		return "(dòng proxy sai định dạng)"
	case p.Type == ProxyTypeRotating && p.Provider != "custom_url":
		return p.Provider + ":***"
	case p.Type == ProxyTypeRotating:
		if u, err := url.Parse(p.URL); err == nil {
			return u.Scheme + "://" + u.Host + "/***"
		}
		return "(link API proxy)"
	default:
		return fmt.Sprintf("%s://%s:%d", p.Scheme, p.Host, p.Port)
	}
}
