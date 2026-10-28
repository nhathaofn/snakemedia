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
	"strings"
	"sync"
	"time"

	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/config"
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
// tab: it always reports "visible", and falls back to a 16 ms timer when
// requestAnimationFrame is throttled.
const visibleShim = `(() => {
  Object.defineProperty(Document.prototype, 'visibilityState', { get: () => 'visible', configurable: true });
  Object.defineProperty(Document.prototype, 'hidden', { get: () => false, configurable: true });
  const raf = window.requestAnimationFrame.bind(window), caf = window.cancelAnimationFrame.bind(window);
  const t = new Map(); let seq = 0;
  window.requestAnimationFrame = cb => {
    const id = ++seq; let done = false;
    const run = ts => { if (done) return; done = true; t.delete(id); cb(ts); };
    t.set(id, [raf(run), setTimeout(() => run(performance.now()), 16)]);
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
	// nav is shared by every job on the account's tab: calls hold it for
	// reading, and loading another page waits for them and holds it alone, so
	// a navigation never cuts another job's send or status read short. Nil
	// for a one-off page (an account check).
	nav *sync.RWMutex

	// mu guards the kept connection. One call uses it at a time (busy); a
	// call that finds it busy (two reads side by side) dials its own, and so
	// does every call once the page is closed.
	mu     sync.Mutex
	conn   *browser.Conn
	busy   bool
	closed bool
}

// Close ends the kept connection; a call still using it ends it when done.
// The page stays usable, each later call on a connection of its own.
func (p *chromePage) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	if !p.busy && p.conn != nil {
		_ = p.conn.Close()
		p.conn = nil
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
	t, found, err := browser.FindTab(ctx, p.port, p.policy, museURL)
	if err != nil {
		return browser.Target{}, err
	}
	if !found {
		return browser.Target{}, errTabMissing
	}
	return t, nil
}

// pageReply is what a call expression resolves to: the method's answer (v),
// or that the tab has no page script yet (missing) or has left muse.ai
// (away), in which case nothing ran.
type pageReply struct {
	Away    bool            `json:"away"`
	Missing bool            `json:"missing"`
	Value   json.RawMessage `json:"v"`
}

// callExpressions are the two forms of a call to window[pageKey][name]: short
// runs it only when the tab already has the page script, full injects the
// script first (a fresh load or a navigation). Both run only on muse.ai: a
// kept connection follows its tab wherever it goes.
func callExpressions(name string, args []any) (short, full string, err error) {
	if args == nil {
		args = []any{}
	}
	encodedArgs, err := json.Marshal(args)
	if err != nil {
		return "", "", err
	}
	encodedName, _ := json.Marshal(name)
	key, _ := json.Marshal(pageKey)
	const form = `(async()=>{if(!(location.protocol==="https:"&&/^(www\.)?muse\.ai$/.test(location.host)))return '{"away":true}';const o=%s;if(!o)return '{"missing":true}';return JSON.stringify({v:await o[%s](...%s)})})()`
	short = fmt.Sprintf(form, fmt.Sprintf("window[%s]", key), encodedName, encodedArgs)
	full = fmt.Sprintf(form, fmt.Sprintf("(window[%s]||(window[%s]=(%s)(%d)))", key, key, pageSource, chunkSize), encodedName, encodedArgs)
	return short, full, nil
}

// invoke calls window[pageKey][name](...args) on the kept connection,
// sending the page script only when the tab does not have it yet. A read that
// fails on a kept connection (its tab may have been replaced) is asked once
// more on a new one. A send never uses the kept connection and is never asked
// twice: Muse may have taken it.
func (p *chromePage) invoke(ctx context.Context, timeout time.Duration, name string, out any, args ...any) error {
	if p.nav != nil {
		p.nav.RLock()
		defer p.nav.RUnlock()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	short, full, err := callExpressions(name, args)
	if err != nil {
		return err
	}
	keep := name != "send"
	conn, reused, done, err := p.connect(ctx, keep)
	if err != nil {
		return err
	}
	err = evaluateCall(ctx, conn, short, full, out)
	var page *browser.EvalError
	if err != nil && reused && ctx.Err() == nil && !errors.As(err, &page) {
		done(true)
		if conn, _, done, err = p.connect(ctx, keep); err != nil {
			return err
		}
		err = evaluateCall(ctx, conn, short, full, out)
	}
	done(err != nil)
	return err
}

// evaluateCall runs the short form of a call, then the full one when the tab
// has no page script, and decodes the method's answer into out.
func evaluateCall(ctx context.Context, conn *browser.Conn, short, full string, out any) error {
	var reply pageReply
	if err := conn.Evaluate(ctx, short, &reply); err != nil {
		return err
	}
	if reply.Missing {
		reply = pageReply{}
		if err := conn.Evaluate(ctx, full, &reply); err != nil {
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

func (p *chromePage) Login(ctx context.Context) (LoginState, error) {
	var state LoginState
	err := p.invoke(ctx, p.callTimeout, "login", &state)
	return state, err
}

// Send starts a new chat. A page without Muse's chat client (an odd route the
// user clicked to) is sent home and tried once more; nothing was sent then.
func (p *chromePage) Send(ctx context.Context, text string, atts []Attachment) (SendResult, error) {
	var r SendResult
	if err := p.invoke(ctx, p.sendTimeout, "send", &r, text, atts, ""); err != nil {
		return r, err
	}
	if !strings.HasPrefix(r.Reason, "not_ready") {
		return r, nil
	}
	if err := p.Home(ctx); err != nil {
		return r, nil
	}
	err := p.invoke(ctx, p.sendTimeout, "send", &r, text, atts, "")
	return r, err
}

// Home loads Muse's home page once no other call is using the tab.
func (p *chromePage) Home(ctx context.Context) error {
	if p.nav != nil {
		p.nav.Lock()
		defer p.nav.Unlock()
	}
	ctx, cancel := context.WithTimeout(ctx, p.callTimeout)
	defer cancel()
	t, err := p.tab(ctx)
	if err != nil {
		return err
	}
	return browser.Navigate(ctx, t, p.port, musePage)
}

func (p *chromePage) Status(ctx context.Context, sessionID string) (string, error) {
	var status string
	err := p.invoke(ctx, p.callTimeout, "status", &status, sessionID)
	return status, err
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
		ID   int   `json:"id"`
		Size int64 `json:"size"`
	}
	if err := p.invoke(ctx, p.callTimeout, "open", &file, path); err != nil {
		return 0, err
	}
	defer func() {
		var ignored any
		_ = p.invoke(context.WithoutCancel(ctx), p.callTimeout, "closeFile", &ignored, file.ID)
	}()
	for i := 0; int64(i)*chunkSize < file.Size; i++ {
		var chunk string
		if err := p.invoke(ctx, p.callTimeout, "slice", &chunk, file.ID, i); err != nil {
			return written, err
		}
		data, err := base64.StdEncoding.DecodeString(chunk)
		if err != nil {
			return written, err
		}
		n, err := sink.Write(data)
		written += int64(n)
		if err != nil {
			return written, err
		}
	}
	return written, nil
}
