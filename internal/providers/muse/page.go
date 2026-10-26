package muse

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

//go:embed page.js
var pageSource string

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

// chromePage drives the muse.ai tab of one account's Chrome over CDP. Each call
// opens its own short connection; the injected window.__mss keeps page state
// (open files) between calls.
type chromePage struct {
	port        int
	callTimeout time.Duration
	sendTimeout time.Duration
}

func (p *chromePage) tab(ctx context.Context) (target, error) {
	var targets []target
	if err := devtoolsJSON(ctx, "GET", p.port, "/json", &targets); err != nil {
		return target{}, err
	}
	for _, t := range targets {
		if t.musePage() {
			return t, nil
		}
	}
	return target{}, errTabMissing
}

// openMuseTab makes sure the browser shows muse.ai, reusing a blank tab when
// there is one. The visibility shim is installed before the page loads.
func openMuseTab(ctx context.Context, port int) error {
	var targets []target
	if err := devtoolsJSON(ctx, "GET", port, "/json", &targets); err != nil {
		return err
	}
	var blank *target
	for i, t := range targets {
		if t.musePage() {
			return nil
		}
		if t.Type == "page" && blank == nil {
			blank = &targets[i]
		}
	}
	if blank == nil {
		var created target
		if err := devtoolsJSON(ctx, "PUT", port, "/json/new?about:blank", &created); err != nil {
			return err
		}
		blank = &created
	}
	return navigateHome(ctx, *blank, port)
}

func navigateHome(ctx context.Context, t target, port int) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	conn, err := dialTarget(ctx, t, port)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.call(ctx, "Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": visibleShim}, nil); err != nil {
		return err
	}
	return conn.call(ctx, "Page.navigate", map[string]any{"url": MuseURL}, nil)
}

// invoke calls window.__mss[name](...args), injecting the page script first
// when the tab does not have it yet (a fresh load or a navigation).
func (p *chromePage) invoke(ctx context.Context, timeout time.Duration, name string, out any, args ...any) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	t, err := p.tab(ctx)
	if err != nil {
		return err
	}
	conn, err := dialTarget(ctx, t, p.port)
	if err != nil {
		return err
	}
	defer conn.Close()
	if args == nil {
		args = []any{}
	}
	encodedArgs, err := json.Marshal(args)
	if err != nil {
		return err
	}
	encodedName, _ := json.Marshal(name)
	expr := fmt.Sprintf("(async()=>JSON.stringify(await (window.__mss || (%s)(%d))[%s](...%s)))()", pageSource, chunkSize, encodedName, encodedArgs)
	return conn.evaluate(ctx, expr, out)
}

func (p *chromePage) Login(ctx context.Context) (LoginState, error) {
	var state LoginState
	err := p.invoke(ctx, p.callTimeout, "login", &state)
	return state, err
}

// Send posts into thread, or starts a new chat. A page without Muse's chat
// client (an odd route the user clicked to) is sent home and tried once more;
// nothing was sent then.
func (p *chromePage) Send(ctx context.Context, text string, atts []Attachment, thread string) (SendResult, error) {
	var r SendResult
	if err := p.invoke(ctx, p.sendTimeout, "send", &r, text, atts, thread); err != nil {
		return r, err
	}
	if !strings.HasPrefix(r.Reason, "not_ready") {
		return r, nil
	}
	t, err := p.tab(ctx)
	if err != nil {
		return r, nil
	}
	if err := navigateHome(ctx, t, p.port); err != nil {
		return r, nil
	}
	err = p.invoke(ctx, p.sendTimeout, "send", &r, text, atts, thread)
	return r, err
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
	if err := p.invoke(ctx, p.callTimeout, "quota", &sub); err != nil {
		return nil, err
	}
	return parseQuota(sub, time.Now()), nil
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
