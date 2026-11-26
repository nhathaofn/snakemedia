package muse

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"

	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/logger"
)

//go:embed page.js
var pageSource string

// pageKey is the window property the page script lives under. It changes with
// the script, so a tab still holding a script from an older build of the app
// gets the current one.
var pageKey = func() string {
	h := fnv.New32a()
	h.Write([]byte(pageSource))
	return fmt.Sprintf("__mss_%08x", h.Sum32())
}()

// visibleShim keeps Muse (a Next.js app) rendering in a hidden or background
// tab: it always reports "visible", and falls back to a timer when
// requestAnimationFrame is throttled so headless hydration stays responsive
// while idle tabs run zero timers.
const visibleShim = `(() => {
  try {
    const hd = Object.getOwnPropertyDescriptor(Document.prototype, 'hidden');
    if (!hd || hd.configurable) Object.defineProperty(Document.prototype, 'hidden', { get: () => false, configurable: true });
    const vd = Object.getOwnPropertyDescriptor(Document.prototype, 'visibilityState');
    if (!vd || vd.configurable) Object.defineProperty(Document.prototype, 'visibilityState', { get: () => 'visible', configurable: true });
  } catch (e) {}
  if (window.__mssRafPatched || typeof window.requestAnimationFrame !== 'function' || typeof window.cancelAnimationFrame !== 'function') return;
  window.__mssRafPatched = true;
  const raf = window.requestAnimationFrame.bind(window), caf = window.cancelAnimationFrame.bind(window);
  const t = new Map(); let seq = 0;
  window.requestAnimationFrame = cb => {
    const id = ++seq; let done = false, rId = 0, timer = 0;
    const run = ts => { if (done) return; done = true; clearTimeout(timer); if (rId) caf(rId); t.delete(id); cb(ts); };
    rId = raf(run);
    if (!done) { timer = setTimeout(() => run(performance.now()), 32); t.set(id, [rId, timer]); }
    return id;
  };
  window.cancelAnimationFrame = id => { const v = t.get(id); if (v) { caf(v[0]); clearTimeout(v[1]); t.delete(id); } };
})();`

// errTabMissing means the account's browser has no muse.ai tab.
var errTabMissing = errors.New("Chrome của tài khoản không có tab Muse.")

// chromePage drives the muse.ai tab of one account's Chrome over CDP. Calls
// share one kept connection until Close; the script injected under pageKey
// keeps page state (open files) between calls.
type chromePage struct {
	port        int
	policy      config.AccountConfig
	callTimeout time.Duration
	sendTimeout time.Duration
	// nav is shared by callers using the same tab: calls hold it for reading,
	// and loading another page waits for them and holds it alone, so a
	// navigation never cuts another call short. Nil for a one-off page (an
	// account check).
	nav *sync.RWMutex
	// routeMu serializes route-mutating React/Next.js operations on the same
	// shared tab (send / SendTo / Home / Login) in lock order routeMu -> nav.
	routeMu  *sync.Mutex
	targetID string
	jobID    string
	onClose  func(dirty bool)

	// mu guards the kept connection. One call uses it at a time (busy); a
	// call that finds it busy (two reads side by side) dials its own, and so
	// does every call once the page is closed.
	mu     sync.Mutex
	conn   *browser.Conn
	busy   bool
	closed bool
	dirty  bool
}

// markDirty flags the assigned tab to be closed on Close instead of reused.
func (p *chromePage) markDirty() {
	p.mu.Lock()
	p.dirty = true
	p.mu.Unlock()
}

// bindTab assigns this page to a tab target (shared or exclusive) and release hook.
func (p *chromePage) bindTab(targetID, jobID string, nav *sync.RWMutex, routeMu *sync.Mutex, onClose func(dirty bool)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.targetID != targetID && !p.busy && p.conn != nil {
		_ = p.conn.Close()
		p.conn = nil
	}
	p.targetID = targetID
	p.jobID = jobID
	p.nav = nav
	p.routeMu = routeMu
	p.onClose = onClose
}

func (p *chromePage) boundTab() (targetID, jobID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.targetID, p.jobID
}

// Close ends the kept connection; a call still using it ends it when done.
// The page stays usable, each later call on a connection of its own.
func (p *chromePage) Close() {
	p.mu.Lock()
	p.closed = true
	if !p.busy && p.conn != nil {
		_ = p.conn.Close()
		p.conn = nil
	}
	dirty := p.dirty
	onClose := p.onClose
	p.onClose = nil
	p.mu.Unlock()
	if onClose != nil {
		onClose(dirty)
	}
}

// closePage ends a page's kept connection, for pages that keep one.
func closePage(page Page) {
	if c, ok := page.(interface{ Close() }); ok {
		c.Close()
	}
}

// dial connects to the account's muse.ai tab.
func (p *chromePage) dial(ctx context.Context) (*browser.Conn, error) {
	t, err := p.tab(ctx)
	if err != nil {
		return nil, err
	}
	// One file chunk is chunkSize bytes base64-encoded inside a JSON string.
	return browser.Dial(ctx, t, p.port, 4*chunkSize)
}

// connect returns a connection for one call, whether it is the kept one
// from an earlier call (reused), and done, which gives it back: a failed call
// drops the kept connection, so the next call finds the tab again. keep false
// always dials a connection of its own.
func (p *chromePage) connect(ctx context.Context, keep bool) (conn *browser.Conn, reused bool, done func(failed bool), err error) {
	p.mu.Lock()
	if !keep || p.closed || p.busy {
		p.mu.Unlock()
		conn, err := p.dial(ctx)
		if err != nil {
			return nil, false, nil, err
		}
		return conn, false, func(bool) { _ = conn.Close() }, nil
	}
	p.busy = true
	conn, reused = p.conn, p.conn != nil
	p.conn = nil
	p.mu.Unlock()
	if conn == nil {
		if conn, err = p.dial(ctx); err != nil {
			p.mu.Lock()
			p.busy = false
			p.mu.Unlock()
			return nil, false, nil, err
		}
	}
	return conn, reused, func(failed bool) {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.busy = false
		if failed || p.closed {
			_ = conn.Close()
			return
		}
		p.conn = conn
	}, nil
}

func (p *chromePage) tab(ctx context.Context) (browser.Target, error) {
	p.mu.Lock()
	targetID := p.targetID
	p.mu.Unlock()
	if targetID != "" {
		targets, err := browser.Targets(ctx, p.port, p.policy)
		if err != nil {
			return browser.Target{}, err
		}
		for _, t := range targets {
			if t.ID == targetID {
				if u, ok := t.Page(); ok && museURL(u) {
					return t, nil
				}
				return browser.Target{}, errTabMissing
			}
		}
		return browser.Target{}, errTabMissing
	}
	t, found, err := browser.FindTab(ctx, p.port, p.policy, museURL)
	if err != nil {
		return browser.Target{}, err
	}
	if !found {
		return browser.Target{}, errTabMissing
	}
	return t, nil
}

const awayTabExpr = `({url:String(location.href||""),ready:String(document.readyState||""),title:String(document.title||""),text:String(document.body?.innerText||"").replace(/\s+/g," ").trim().slice(0,400)})`

// inspectAwayTab checks a Chrome browser that has no muse.ai tab to read
// where the tab landed, optionally navigating back to MuseURL once first.
func inspectAwayTab(ctx context.Context, port int, policy config.AccountConfig, navigate bool) (awayTabInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(policy.RequestTimeoutSeconds)*time.Second)
	defer cancel()
	findPage := func(targets []browser.Target) (away *browser.Target, onMuse *url.URL) {
		var blank *browser.Target
		for i := range targets {
			u, ok := targets[i].Page()
			if !ok {
				continue
			}
			if museURL(u) {
				return nil, u
			}
			if targets[i].URL != "" && targets[i].URL != "about:blank" && !strings.HasPrefix(targets[i].URL, "chrome://") {
				if away == nil {
					away = &targets[i]
				}
			} else if blank == nil {
				blank = &targets[i]
			}
		}
		if away != nil {
			return away, nil
		}
		return blank, nil
	}
	targets, err := browser.Targets(ctx, port, policy)
	if err != nil {
		return awayTabInfo{}, err
	}
	target, onMuse := findPage(targets)
	if onMuse != nil {
		return awayTabInfo{URL: onMuse.String(), Ready: "complete"}, nil
	}
	if target == nil {
		return awayTabInfo{}, errTabMissing
	}
	if navigate {
		if err := browser.NavigateWithPolicy(ctx, *target, port, musePage, policy); err != nil {
			return awayTabInfo{}, err
		}
		targets, err = browser.Targets(ctx, port, policy)
		if err != nil {
			return awayTabInfo{}, err
		}
		target, onMuse = findPage(targets)
		if onMuse != nil {
			return awayTabInfo{URL: onMuse.String(), Ready: "complete"}, nil
		}
		if target == nil {
			return awayTabInfo{}, errTabMissing
		}
	}
	conn, err := browser.Dial(ctx, *target, port, 1<<20)
	if err != nil {
		return awayTabInfo{URL: target.URL}, nil
	}
	defer conn.Close()
	var info awayTabInfo
	if err := conn.Evaluate(ctx, awayTabExpr, &info); err != nil {
		return awayTabInfo{URL: target.URL}, nil
	}
	if info.URL == "" {
		info.URL = target.URL
	}
	return info, nil
}

// pageReply is what a call expression resolves to: the method's answer (v),
// or that the tab has no page script yet (missing) or has left muse.ai
// (away), in which case nothing ran.
type pageReply struct {
	Away    bool            `json:"away"`
	Missing bool            `json:"missing"`
	Value   json.RawMessage `json:"v"`
}

// callForm wraps a call to window[pageKey][name]; its verbs are the page
// object, the method name and the arguments, all as JS expressions.
const callForm = `(async()=>{if(!(location.protocol==="https:"&&/^(www\.)?muse\.ai$/.test(location.host)))return '{"away":true}';const o=%s;if(!o)return '{"missing":true}';return JSON.stringify({v:await o[%s](...%s)})})()`

// pageObjects are the page object expressions of the short and full forms.
// The full one embeds page.js, so both are built once, not per call.
var pageObjects = sync.OnceValues(func() (short, full string) {
	key, _ := json.Marshal(pageKey)
	short = fmt.Sprintf("window[%s]", key)
	full = fmt.Sprintf("(window[%s]||(window[%s]=(%s)(%d)))", key, key, pageSource, chunkSize)
	return short, full
})

// callExpressions are the two forms of a call to window[pageKey][name]: short
// runs it only when the tab already has the page script, full injects the
// script first (a fresh load or a navigation). Both run only on muse.ai: a
// kept connection follows its tab wherever it goes. full is built only when
// asked for, as most calls find the script already there.
func callExpressions(name string, args []any) (short string, full func() string, err error) {
	if args == nil {
		args = []any{}
	}
	encodedArgs, err := json.Marshal(args)
	if err != nil {
		return "", nil, err
	}
	encodedName, _ := json.Marshal(name)
	shortObject, fullObject := pageObjects()
	short = fmt.Sprintf(callForm, shortObject, encodedName, encodedArgs)
	full = func() string { return fmt.Sprintf(callForm, fullObject, encodedName, encodedArgs) }
	return short, full, nil
}

// invoke calls window[pageKey][name](...args) on the kept connection,
// sending the page script only when the tab does not have it yet. A read that
// fails on a kept connection (its tab may have been replaced) is asked once
// more on a new one. A send never uses the kept connection and is never asked
// twice: Muse may have taken it.
func (p *chromePage) invoke(ctx context.Context, timeout time.Duration, name string, out any, args ...any) error {
	unlock := p.readNav()
	defer unlock()
	return p.call(ctx, timeout, name, out, args...)
}

func (p *chromePage) navLock() *sync.RWMutex {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.nav
}

// readNav holds the tab against Home until the returned func is called.
func (p *chromePage) readNav() func() {
	nav := p.navLock()
	if nav == nil {
		return func() {}
	}
	nav.RLock()
	return nav.RUnlock
}

// call is invoke for a caller already holding readNav. The lock is not
// taken again: a waiting Home would block a second RLock.
func (p *chromePage) call(ctx context.Context, timeout time.Duration, name string, out any, args ...any) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	short, full, err := callExpressions(name, args)
	if err != nil {
		return wrapPreSend(name, err)
	}
	keep := name != "send"
	conn, reused, done, err := p.connect(ctx, keep)
	if err != nil {
		return wrapPreSend(name, err)
	}
	err = evaluateCall(ctx, conn, short, full, out)
	var page *browser.EvalError
	if err != nil && reused && ctx.Err() == nil && !errors.As(err, &page) {
		done(true)
		if conn, _, done, err = p.connect(ctx, keep); err != nil {
			return wrapPreSend(name, err)
		}
		err = evaluateCall(ctx, conn, short, full, out)
	}
	done(err != nil)
	if errors.Is(err, errTabMissing) {
		return wrapPreSend(name, err)
	}
	return err
}

func wrapPreSend(name string, err error) error {
	if name == "send" && err != nil {
		return &preSendError{err: err}
	}
	return err
}

// evaluateCall runs the short form of a call, then the full one when the tab
// has no page script, and decodes the method's answer into out.
func evaluateCall(ctx context.Context, conn *browser.Conn, short string, full func() string, out any) error {
	var reply pageReply
	if err := conn.Evaluate(ctx, short, &reply); err != nil {
		return err
	}
	if reply.Missing {
		reply = pageReply{}
		if err := conn.Evaluate(ctx, full(), &reply); err != nil {
			return err
		}
	}
	if reply.Away {
		return errTabMissing
	}
	if out == nil || len(reply.Value) == 0 {
		return nil
	}
	return json.Unmarshal(reply.Value, out)
}

// requestTimeout bounds fast checks (login, pre-send probes) based on
// RequestTimeoutSeconds while keeping longer test callTimeouts intact and
// allowing slow-proxy cold loads enough room.
func (p *chromePage) requestTimeout() time.Duration {
	t := p.callTimeout
	if req := time.Duration(p.policy.RequestTimeoutSeconds*2) * time.Second; req > 0 && (t <= 0 || req < t) {
		t = req
	}
	if t <= 0 {
		return 15 * time.Second
	}
	return t
}

func (p *chromePage) lockRoute() func() {
	p.mu.Lock()
	rm := p.routeMu
	p.mu.Unlock()
	if rm == nil {
		return func() {}
	}
	rm.Lock()
	return rm.Unlock
}

func (p *chromePage) Login(ctx context.Context) (LoginState, error) {
	unlockRoute := p.lockRoute()
	defer unlockRoute()
	var state LoginState
	timeout := p.requestTimeout()
	fetchMS := max(timeout.Milliseconds()*4/5, 1000)
	err := p.invoke(ctx, timeout, "login", &state, fetchMS)
	return state, err
}

// Send starts a new chat. A page without Muse's chat client (an odd route the
// user clicked to) is sent home and tried once more; nothing was sent then.
func (p *chromePage) Send(ctx context.Context, text string, atts []Attachment) (SendResult, error) {
	return p.send(ctx, text, atts, "")
}

// SendTo posts text into an existing chat. page.js routes the message to that
// thread; a page without Muse's chat client is sent home and tried once more.
func (p *chromePage) SendTo(ctx context.Context, sessionID, text string) (SendResult, error) {
	return p.send(ctx, text, nil, sessionID)
}

// send posts text, in a new chat when sessionID is empty, retrying once from
// Muse's home page when the page had no chat client (nothing was sent then).
// Holds routeMu across the whole attempt so concurrent shared-tab callers never
// interleave Next.js thread transitions (Send vs SendTo vs Home vs Login).
func (p *chromePage) send(ctx context.Context, text string, atts []Attachment, sessionID string) (SendResult, error) {
	var r SendResult
	defer func() {
		if r.OK || r.Reason == "" {
			return
		}
		encodedBytes := 0
		for _, att := range atts {
			encodedBytes += len(att.DataBase64)
		}
		logger.Get().Warn("muse client send refused", "reason", r.Reason, "attachments", len(atts), "base64Bytes", encodedBytes, "existingChat", sessionID != "", "diagnostics", r.Diagnostics)
	}()
	unlockRoute := p.lockRoute()
	defer unlockRoute()
	if err := p.invoke(ctx, p.sendTimeout, "send", &r, text, atts, sessionID); err != nil {
		p.markDirty()
		return r, err
	}
	if !strings.HasPrefix(r.Reason, "not_ready") {
		return r, nil
	}
	if err := p.homeNav(ctx); err != nil {
		p.markDirty()
		return r, wrapPreSend("send", err)
	}
	err := p.invoke(ctx, p.sendTimeout, "send", &r, text, atts, sessionID)
	if err != nil || (!r.OK && strings.HasPrefix(r.Reason, "not_ready")) {
		p.markDirty()
	}
	return r, err
}

// Home loads Muse's home page once no other route operation or call is using the tab.
func (p *chromePage) Home(ctx context.Context) error {
	unlockRoute := p.lockRoute()
	defer unlockRoute()
	return p.homeNav(ctx)
}

func (p *chromePage) homeNav(ctx context.Context) error {
	if nav := p.navLock(); nav != nil {
		nav.Lock()
		defer nav.Unlock()
	}
	ctx, cancel := context.WithTimeout(ctx, p.callTimeout)
	defer cancel()
	t, err := p.tab(ctx)
	if err != nil {
		return err
	}
	return browser.NavigateWithPolicy(ctx, t, p.port, musePage, p.policy)
}

func (p *chromePage) Status(ctx context.Context, sessionID string) (string, error) {
	var status string
	err := p.invoke(ctx, p.callTimeout, "status", &status, sessionID)
	return status, err
}

func (p *chromePage) Sessions(ctx context.Context, fresh bool) ([]SessionInfo, error) {
	var out []SessionInfo
	err := p.invoke(ctx, p.callTimeout, "sessions", &out, fresh)
	return out, err
}

func (p *chromePage) UserMessages(ctx context.Context, sessionID string) ([]UserEvent, error) {
	var out []UserEvent
	err := p.invoke(ctx, p.callTimeout, "userMessages", &out, sessionID)
	return out, err
}

func (p *chromePage) Result(ctx context.Context, sessionID string) ([]AssistantEvent, error) {
	var events []AssistantEvent
	err := p.invoke(ctx, p.callTimeout, "result", &events, sessionID)
	return events, err
}

func (p *chromePage) Cancel(ctx context.Context, sessionID string) error {
	var ignored any
	return p.invoke(ctx, p.callTimeout, "cancel", &ignored, sessionID)
}

func (p *chromePage) Quota(ctx context.Context) (*MuseQuota, error) {
	var sub *subscription
	if err := p.invoke(ctx, max(p.callTimeout, p.sendTimeout), "quota", &sub); err != nil {
		return nil, err
	}
	return parseQuota(sub, time.Now()), nil
}

// Capabilities reads Muse's upload limits, which may mean scanning Muse's
// scripts once per build, so it gets the longer send timeout.
func (p *chromePage) Capabilities(ctx context.Context) (*MuseCapabilities, error) {
	var raw rawCapabilities
	if err := p.invoke(ctx, max(p.callTimeout, p.sendTimeout), "capabilities", &raw); err != nil {
		return nil, err
	}
	return parseCapabilities(raw, time.Now()), nil
}

func (p *chromePage) Download(ctx context.Context, path string, sink io.Writer) (written int64, err error) {
	var file struct {
		ID   int    `json:"id"`
		Size int64  `json:"size"`
		Data string `json:"data"`
	}
	// The tab is held for the whole file: Home between two slices would
	// reload the page and drop the open file.
	unlock := p.readNav()
	defer unlock()
	if err := p.call(ctx, p.callTimeout, "open", &file, path, p.policy.MaxDownloadBytes); err != nil {
		return 0, err
	}
	if file.ID != 0 {
		defer func() {
			var ignored any
			_ = p.call(context.WithoutCancel(ctx), p.callTimeout, "closeFile", &ignored, file.ID)
		}()
	}
	if file.Size <= 0 || file.Size > p.policy.MaxDownloadBytes {
		return 0, errors.New("Tệp Muse rỗng hoặc vượt giới hạn tải media.")
	}
	if file.Data != "" {
		data, err := base64.StdEncoding.DecodeString(file.Data)
		if err != nil {
			return 0, err
		}
		if int64(len(data)) != file.Size {
			return 0, fmt.Errorf("Tải file lỗi: %d/%d byte", len(data), file.Size)
		}
		n, err := sink.Write(data)
		return int64(n), err
	}
	type chunkWriteResult struct {
		written int64
		err     error
	}
	dlCtx, cancelDL := context.WithCancel(ctx)
	defer cancelDL()
	chunks := make(chan string, 1)
	done := make(chan chunkWriteResult, 1)
	go func() {
		var total int64
		defer logger.Recover("ghi chunk Muse", func(err error) { done <- chunkWriteResult{total, err} })
		for chunk := range chunks {
			data, err := base64.StdEncoding.DecodeString(chunk)
			if err != nil {
				done <- chunkWriteResult{total, err}
				return
			}
			n, err := sink.Write(data)
			total += int64(n)
			if err != nil {
				done <- chunkWriteResult{total, err}
				return
			}
		}
		done <- chunkWriteResult{total, nil}
	}()
	for i := 0; int64(i)*chunkSize < file.Size; i++ {
		var chunk string
		if err := p.call(dlCtx, p.callTimeout, "slice", &chunk, file.ID, i); err != nil {
			cancelDL()
			close(chunks)
			r := <-done
			if r.err != nil {
				return r.written, r.err
			}
			return r.written, err
		}
		select {
		case chunks <- chunk:
		case r := <-done:
			cancelDL()
			return r.written, r.err
		}
	}
	close(chunks)
	r := <-done
	return r.written, r.err
}
