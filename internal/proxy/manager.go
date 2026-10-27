package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sync"
	"time"

	"SnakeMedia/internal/logger"
)

// None as an account's proxy means it connects directly; "" means the
// account takes a proxy from the shared list.
const None = "none"

// DirectRoute names a browser that runs without a proxy.
const DirectRoute = "direct"

// deadFor keeps a failing proxy out of automatic picks for a while.
const deadFor = 10 * time.Minute

// checkConcurrency is how many proxies CheckAll tests at once.
const checkConcurrency = 5

// defaultTimeout bounds proxy checks and bridge I/O until SetTimeout is called.
const defaultTimeout = 15 * time.Second

// PrivacyArgs are Chrome flags every account browser gets. WebRTC would
// otherwise reach STUN servers directly over UDP and reveal the real IP even
// behind a proxy.
var PrivacyArgs = []string{
	"--force-webrtc-ip-handling-policy=disable_non_proxied_udp",
	"--webrtc-ip-handling-policy=disable_non_proxied_udp",
}

// Assignment is how one account uses the proxy list.
type Assignment struct {
	Account string // account ID
	Name    string // shown in messages
	Proxy   string // "" auto, None direct, otherwise a proxy line
	Auto    string // the line an earlier automatic pick pinned
	// Keep forbids moving the account off its pinned line while that line is
	// still listed: a signed-in session that suddenly comes from another IP
	// gets flagged by the site.
	Keep bool
}

// Line is the proxy line the account uses now, or "".
func (a Assignment) Line() string {
	if a.Proxy == None {
		return ""
	}
	if a.Proxy != "" {
		return a.Proxy
	}
	return a.Auto
}

// Route is how one account's browser and HTTP clients reach the network.
type Route struct {
	// Key identifies the proxy; a browser on another key is restarted.
	Key string
	// Auto is the line an automatic pick chose (to pin on the account).
	Auto string
	// IP is the exit address when known.
	IP string
	// Args are the Chrome flags for this route.
	Args []string
	// URL is the local HTTP proxy for Go clients; nil when direct.
	URL *url.URL
}

// Manager owns the proxy list, the local bridges and the proxies that
// recently failed. It is safe for concurrent use.
type Manager struct {
	store   *Store
	bridges *bridges
	now     func() time.Time
	mu      sync.Mutex
	dead    map[string]time.Time
	timeout func() time.Duration
}

// NewManager keeps the list at path.
func NewManager(path string) *Manager {
	m := &Manager{store: NewStore(path), now: time.Now, dead: make(map[string]time.Time)}
	m.timeout = func() time.Duration { return defaultTimeout }
	m.bridges = newBridges(m.requestTimeout)
	return m
}

// SetTimeout makes proxy checks, provider API calls and bridge I/O follow the
// app's request timeout.
func (m *Manager) SetTimeout(timeout func() time.Duration) {
	m.mu.Lock()
	m.timeout = timeout
	m.mu.Unlock()
}

func (m *Manager) requestTimeout() time.Duration {
	m.mu.Lock()
	timeout := m.timeout
	m.mu.Unlock()
	return timeout()
}

// PruneBridges closes the local bridges no running browser routes through;
// inUse holds the ports of the routes still in use.
func (m *Manager) PruneBridges(inUse map[int]bool) {
	if n := m.bridges.Prune(inUse); n > 0 {
		logger.Get().Debug("closed unused proxy bridges", "count", n)
	}
}

// Text returns the saved proxy list. An unreadable store is an error, not an
// empty list: treating it as empty would send every account out on the
// machine's real IP.
func (m *Manager) Text() (string, error) { return m.store.GetText() }

// Save replaces the proxy list.
func (m *Manager) Save(text string) error { return m.store.SaveText(text) }

// Close stops the local bridges.
func (m *Manager) Close() { m.bridges.Close() }

// MarkDead keeps a failing proxy out of automatic picks for a while.
func (m *Manager) MarkDead(line string) {
	if line == "" || line == None {
		return
	}
	m.mu.Lock()
	m.dead[line] = m.now().Add(deadFor)
	m.mu.Unlock()
	logger.Get().Warn("proxy marked dead", "proxy", Mask(line), "for", deadFor)
}

// markAlive clears a dead mark once a check reached the proxy again.
func (m *Manager) markAlive(line string) {
	m.mu.Lock()
	_, was := m.dead[line]
	delete(m.dead, line)
	m.mu.Unlock()
	if was {
		logger.Get().Info("proxy answers again", "proxy", Mask(line))
	}
}

// deadSet copies the proxies marked dead, dropping expired marks.
func (m *Manager) deadSet() map[string]time.Time {
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	dead := make(map[string]time.Time, len(m.dead))
	for k, until := range m.dead {
		if now.After(until) {
			delete(m.dead, k)
			continue
		}
		dead[k] = until
	}
	return dead
}

// CheckAll tests every proxy in the list, a few at a time. A proxy that
// failed is kept out of automatic picks; one that answers is picked again.
// Results name each proxy without its password or key.
func (m *Manager) CheckAll(ctx context.Context) ([]CheckResult, error) {
	text, err := m.Text()
	if err != nil {
		return nil, err
	}
	lines := ParseList(text)
	results := make([]CheckResult, len(lines))
	timeout := m.requestTimeout()
	var wg sync.WaitGroup
	sem := make(chan struct{}, checkConcurrency)
	for i, line := range lines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			defer logger.Recover("kiểm tra proxy")
			parsed := Parse(line)
			if parsed == nil || parsed.Error != "" {
				msg := "Sai cú pháp"
				if parsed != nil {
					msg = parsed.Error
				}
				results[i] = CheckResult{Raw: Mask(line), Status: StatusDead, Message: msg, CheckedAt: m.now().UTC().Format(time.RFC3339)}
				return
			}
			res := Check(ctx, parsed, false, timeout)
			switch {
			case res.OK:
				m.markAlive(line)
			case ctx.Err() == nil && (res.Status == StatusDead || res.Status == StatusLimitReached):
				m.MarkDead(line)
			}
			res.Raw = Mask(line)
			results[i] = res
		}()
	}
	wg.Wait()
	return results, nil
}

// Rotate asks a rotating proxy's provider for a new exit IP.
func (m *Manager) Rotate(ctx context.Context, line string) (CheckResult, error) {
	parsed := Parse(line)
	if parsed == nil || parsed.Type != TypeRotating {
		return CheckResult{}, errors.New("Proxy của tài khoản không phải proxy xoay IP.")
	}
	res := fetchRotating(ctx, parsed, true, m.requestTimeout())
	res.Raw = Mask(line)
	if !res.OK {
		return res, fmt.Errorf("Đổi IP thất bại: %s", res.Message)
	}
	logger.Get().Info("rotating proxy changed IP", "proxy", res.Raw)
	return res, nil
}

// Usable reports, without side effects, why Route would not find the account a
// proxy right now (nil when it would). Lists of ready accounts use it so an
// account with no route is not offered work it cannot run.
func (m *Manager) Usable(a Assignment, others []Assignment) error {
	text, err := m.Text()
	if err != nil {
		return err
	}
	_, _, err = pick(a, others, text, m.deadSet(), m.now())
	return err
}

// Route picks the account's proxy and prepares its local bridge. Any failure
// is returned: an account is never silently sent out directly.
func (m *Manager) Route(ctx context.Context, a Assignment, others []Assignment) (Route, error) {
	text, err := m.Text()
	if err != nil {
		return Route{}, err
	}
	parsed, auto, err := pick(a, others, text, m.deadSet(), m.now())
	if err != nil {
		logger.Get().Warn("no proxy route", "account", a.Account, "error", err)
		return Route{}, err
	}
	if parsed == nil {
		logger.Get().Info("proxy route", "account", a.Account, "proxy", "direct")
		return Route{Key: DirectRoute, Args: PrivacyArgs}, nil
	}
	logger.Get().Info("proxy route", "account", a.Account, "proxy", Mask(parsed.Raw), "newPick", auto != "" && auto != a.Auto)
	route := Route{Key: parsed.Raw, Auto: auto}
	if parsed.Type == TypeRotating {
		res := fetchRotating(ctx, parsed, false, m.requestTimeout())
		if !res.OK {
			m.MarkDead(parsed.Raw)
			return Route{}, fmt.Errorf("Proxy xoay lỗi: %s", res.Message)
		}
		parsed.Host, parsed.Port, parsed.Scheme = res.IP, res.Port, "http"
	}
	port, err := m.bridges.GetBridge(parsed)
	if err != nil {
		return Route{}, err
	}
	route.IP = parsed.Host
	route.URL = &url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", port)}
	route.Args = append([]string{"--proxy-server=" + route.URL.String()}, PrivacyArgs...)
	return route, nil
}

// pick selects the account's proxy:
//   - None connects directly;
//   - an explicit line uses that proxy;
//   - otherwise the pinned automatic line is kept while it works, or the least
//     shared live line is picked (and returned to be pinned);
//   - an account that must keep its pin (Keep) waits for that line rather
//     than move to another one while it is still listed;
//   - an empty list, or every proxy dead, fails rather than fall back to the
//     real IP: only None means direct.
func pick(a Assignment, others []Assignment, text string, dead map[string]time.Time, now time.Time) (*Parsed, string, error) {
	if a.Proxy == None {
		return nil, "", nil
	}
	if a.Proxy != "" {
		p := Parse(a.Proxy)
		if p == nil || p.Error != "" {
			return nil, "", fmt.Errorf("Proxy của %s không hợp lệ: %s", a.Name, Mask(a.Proxy))
		}
		return p, "", nil
	}
	lines := ParseList(text)
	if len(lines) == 0 {
		return nil, "", fmt.Errorf("Danh sách proxy đang trống nên %s không có proxy để chạy. Thêm proxy ở Trang chủ → Proxy, hoặc chọn \"Không dùng proxy\" cho tài khoản này.", a.Name)
	}
	if a.Keep && a.Auto != "" && slices.Contains(lines, a.Auto) {
		if until, ok := dead[a.Auto]; ok && until.After(now) {
			return nil, "", fmt.Errorf("Proxy %s của %s đang không phản hồi. App không tự chuyển tài khoản đã đăng nhập sang IP khác; hãy kiểm tra proxy, chờ vài phút rồi thử lại, hoặc chọn proxy khác cho tài khoản.", Mask(a.Auto), a.Name)
		}
	}
	var live []string
	for _, line := range lines {
		if until, ok := dead[line]; !ok || !until.After(now) {
			live = append(live, line)
		}
	}
	for _, line := range live {
		if line == a.Auto {
			return Parse(line), line, nil
		}
	}
	if len(live) == 0 {
		return nil, "", errors.New("Tất cả proxy đều chết — kiểm tra lại danh sách proxy.")
	}
	used := func(line string) int {
		n := 0
		for _, o := range others {
			if o.Account != a.Account && o.Line() == line {
				n++
			}
		}
		return n
	}
	best, bestUsed := live[0], used(live[0])
	for _, line := range live[1:] {
		if u := used(line); u < bestUsed {
			best, bestUsed = line, u
		}
	}
	return Parse(best), best, nil
}
