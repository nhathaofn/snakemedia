package muse

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/config"
	"github.com/gorilla/websocket"
)

// Execute the real script: a fake CDP response cannot catch losing an Error's
// non-enumerable message when Muse also returns a generic send_failed reason.
func TestPageSendDiagnostics(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is needed to execute the page script")
	}
	script := `const assert = require('node:assert/strict');
const client = { ensureActiveChatSendReady() {}, sendMessage: async () => answer };
global.document = {body: {__reactFiber: {memoizedProps: {value: client}}}, querySelectorAll: () => []};
let answer;
const page = (` + pageSource + `)(1024);
(async () => {
 const error = new Error('invalid chat request body: length limit exceeded');
 error.code = 'invalid_params'; error.kind = 'jarvis_rejection';
 answer = {ok: false, reason: 'send_failed', error};
 const rejected = await page.send('private prompt', [], '');
 assert.equal(rejected.reason, 'send_failed');
 assert.equal(rejected.diagnostics.message, error.message);
 assert.equal(rejected.diagnostics.code, 'invalid_params');
 assert.equal(rejected.diagnostics.kind, 'jarvis_rejection');
 assert.equal(rejected.diagnostics.stack, undefined);
 error.message = 'private prompt ' + 'A'.repeat(256);
 const redacted = await page.send('private prompt', [], '');
 assert.equal(redacted.diagnostics.message, '[prompt] [data]');
 answer = {ok: true, sessionId: 'accepted-chat'};
 const accepted = await page.send('private prompt', [], '');
 assert.equal(accepted.sessionId, 'accepted-chat');
 assert.equal(accepted.diagnostics, null);
})().catch(e => { console.error(e); process.exitCode = 1; });`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node)
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("page diagnostics: %v\n%s", err, out)
	}
}

// fakeDevTools serves /json and one tab's debugger socket. Runtime.evaluate
// answers by the page script method named in the expression, like the page:
// a short call on a tab without the script reports it missing, a full call
// injects it, and a tab away from muse.ai runs nothing.
type fakeDevTools struct {
	t       *testing.T
	server  *httptest.Server
	port    int
	tabURL  string
	answers map[string]func(args []json.RawMessage) any
	mu      sync.Mutex
	methods []string
	// calls lists the page methods run; dials counts debugger sockets opened,
	// injections the full calls that sent the page script.
	calls      []string
	dials      int
	injections int
	injected   bool
	away       bool
	sockets    []*websocket.Conn
	// dropOn closes the socket instead of answering a call of that method.
	dropOn string
}

var callPattern = regexp.MustCompile(`o\[("\w+")\]\(\.\.\.(\[.*\])\)\}\)\}\)\(\)$`)

func newFakeDevTools(t *testing.T, tabURL string) *fakeDevTools {
	f := &fakeDevTools{t: t, tabURL: tabURL, answers: map[string]func([]json.RawMessage) any{}}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f.port = listener.Addr().(*net.TCPAddr).Port
	mux := http.NewServeMux()
	mux.HandleFunc("/json", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode([]browser.Target{{ID: "1", Type: "page", URL: f.tabURL, WebSocketDebuggerURL: fmt.Sprintf("ws://127.0.0.1:%d/devtools/page/1", f.port)}})
	})
	mux.HandleFunc("/devtools/page/1", f.serveSocket)
	f.server = &httptest.Server{Listener: listener, Config: &http.Server{Handler: mux}}
	f.server.Start()
	t.Cleanup(f.server.Close)
	return f
}

// counts returns how many sockets were dialled and page scripts sent.
func (f *fakeDevTools) counts() (dials, injections int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dials, f.injections
}

// ran counts the runs of a page method.
func (f *fakeDevTools) ran(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == name {
			n++
		}
	}
	return n
}

// set changes the fake's state under its lock.
func (f *fakeDevTools) set(change func(f *fakeDevTools)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change(f)
}

// dropSockets closes every open debugger socket, as a replaced tab does.
func (f *fakeDevTools) dropSockets() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.sockets {
		c.Close()
	}
	f.sockets = nil
}

func (f *fakeDevTools) serveSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	f.mu.Lock()
	f.dials++
	f.sockets = append(f.sockets, conn)
	f.mu.Unlock()
	for {
		var msg struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Expression string `json:"expression"`
			} `json:"params"`
		}
		if conn.ReadJSON(&msg) != nil {
			return
		}
		f.mu.Lock()
		f.methods = append(f.methods, msg.Method)
		f.mu.Unlock()
		result := map[string]any{}
		if msg.Method == "Runtime.evaluate" {
			value, drop := f.evaluate(msg.Params.Expression)
			if drop {
				return
			}
			result = map[string]any{"result": map[string]any{"type": "string", "value": value}}
		}
		conn.WriteJSON(map[string]any{"id": msg.ID, "result": result})
	}
}

// evaluate answers one call expression; drop closes the socket instead.
func (f *fakeDevTools) evaluate(expr string) (value string, drop bool) {
	m := callPattern.FindStringSubmatch(expr)
	if m == nil || !strings.Contains(expr, `window["`+pageKey+`"]`) {
		f.t.Errorf("unexpected expression tail: %.80q", expr[max(0, len(expr)-120):])
		return "", true
	}
	var name string
	json.Unmarshal([]byte(m[1]), &name)
	full := strings.Contains(expr, "function pageMain(CHUNK)")
	f.mu.Lock()
	switch {
	case f.away:
		f.mu.Unlock()
		return `{"away":true}`, false
	case !full && !f.injected:
		f.mu.Unlock()
		return `{"missing":true}`, false
	case full:
		f.injections++
		f.injected = true
	}
	f.calls = append(f.calls, name)
	dropOn := f.dropOn
	f.mu.Unlock()
	if name == dropOn {
		return "", true
	}
	var args []json.RawMessage
	json.Unmarshal([]byte(m[2]), &args)
	answer, ok := f.answers[name]
	if !ok {
		f.t.Errorf("unexpected call %s", name)
		return "", true
	}
	v, _ := json.Marshal(map[string]any{"v": answer(args)})
	return string(v), false
}

func newTestPage(f *fakeDevTools) *chromePage {
	return &chromePage{port: f.port, policy: config.DefaultAccountConfig(), callTimeout: 5 * time.Second, sendTimeout: 5 * time.Second}
}

func TestChromePageCallsAndChunkedDownload(t *testing.T) {
	f := newFakeDevTools(t, "https://muse.ai/")
	file := bytes.Repeat([]byte("ab"), chunkSize/2+10) // just over one chunk
	closed := false
	f.answers["login"] = func([]json.RawMessage) any { return LoginState{LoggedIn: true, HTTP: 200} }
	f.answers["send"] = func(args []json.RawMessage) any {
		var text string
		json.Unmarshal(args[0], &text)
		return SendResult{OK: text == "hello", SessionID: "s1"}
	}
	f.answers["open"] = func([]json.RawMessage) any { return map[string]any{"id": 7, "size": len(file)} }
	f.answers["slice"] = func(args []json.RawMessage) any {
		i, _ := strconv.Atoi(string(args[1]))
		end := min((i+1)*chunkSize, len(file))
		return base64.StdEncoding.EncodeToString(file[i*chunkSize : end])
	}
	f.answers["closeFile"] = func([]json.RawMessage) any { closed = true; return nil }

	page := newTestPage(f)
	defer page.Close()
	ctx := context.Background()
	if state, err := page.Login(ctx); err != nil || !state.LoggedIn {
		t.Fatalf("login = %+v, %v", state, err)
	}
	if res, err := page.Send(ctx, "hello", nil); err != nil || !res.OK || res.SessionID != "s1" {
		t.Fatalf("send = %+v, %v", res, err)
	}
	var out bytes.Buffer
	n, err := page.Download(ctx, "sandbox:///out.mp4", &out)
	if err != nil || n != int64(len(file)) || !bytes.Equal(out.Bytes(), file) || !closed {
		t.Fatalf("download n=%d err=%v closed=%v", n, err, closed)
	}
	// Login, open, both slices and closeFile share the kept connection; the
	// send dials its own. The script went to the tab once.
	if dials, injections := f.counts(); dials != 2 || injections != 1 {
		t.Fatalf("dials = %d, injections = %d, want 2 and 1", dials, injections)
	}
}

func TestChromePageInjectsAgainAfterReload(t *testing.T) {
	f := newFakeDevTools(t, "https://muse.ai/")
	f.answers["status"] = func([]json.RawMessage) any { return "running" }
	page := newTestPage(f)
	defer page.Close()
	ctx := context.Background()
	for range 3 {
		if status, err := page.Status(ctx, "s1"); err != nil || status != "running" {
			t.Fatalf("status = %q, %v", status, err)
		}
	}
	f.set(func(f *fakeDevTools) { f.injected = false }) // the tab loaded again
	if _, err := page.Status(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	if dials, injections := f.counts(); dials != 1 || injections != 2 {
		t.Fatalf("dials = %d, injections = %d, want 1 and 2", dials, injections)
	}
}

func TestChromePageRejectsOversizedBlobAndClosesIt(t *testing.T) {
	f := newFakeDevTools(t, MuseURL)
	page := newTestPage(f)
	defer page.Close()
	f.answers["open"] = func(args []json.RawMessage) any {
		if len(args) != 2 || string(args[1]) != strconv.FormatInt(page.policy.MaxDownloadBytes, 10) {
			t.Error("download cap was not sent to the page")
		}
		return map[string]any{"id": 7, "size": page.policy.MaxDownloadBytes + 1}
	}
	f.answers["closeFile"] = func([]json.RawMessage) any { return nil }
	var out bytes.Buffer
	if _, err := page.Download(context.Background(), "/out.mp4", &out); err == nil {
		t.Fatal("oversized Blob accepted")
	}
	if f.ran("slice") != 0 || f.ran("closeFile") != 1 || out.Len() != 0 {
		t.Fatal("oversized Blob was read or leaked")
	}
}

func TestChromePageRedialsAStaleConnectionForReads(t *testing.T) {
	f := newFakeDevTools(t, "https://muse.ai/")
	f.answers["status"] = func([]json.RawMessage) any { return "completed" }
	page := newTestPage(f)
	defer page.Close()
	ctx := context.Background()
	if _, err := page.Status(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	f.dropSockets() // the tab was replaced
	if status, err := page.Status(ctx, "s1"); err != nil || status != "completed" {
		t.Fatalf("status = %q, %v", status, err)
	}
	if dials, _ := f.counts(); dials != 2 {
		t.Fatalf("dials = %d, want 2", dials)
	}
}

func TestChromePageNeverRepeatsASend(t *testing.T) {
	f := newFakeDevTools(t, "https://muse.ai/")
	f.answers["login"] = func([]json.RawMessage) any { return LoginState{LoggedIn: true, HTTP: 200} }
	f.answers["send"] = func([]json.RawMessage) any { return SendResult{OK: true, SessionID: "s1"} }
	page := newTestPage(f)
	defer page.Close()
	ctx := context.Background()
	if _, err := page.Login(ctx); err != nil {
		t.Fatal(err)
	}
	f.set(func(f *fakeDevTools) { f.dropOn = "send" }) // the answer is lost
	if _, err := page.Send(ctx, "hello", nil); err == nil {
		t.Fatal("a lost send answer must come back as an error")
	}
	if n := f.ran("send"); n != 1 {
		t.Fatalf("send ran %d times, want 1", n)
	}
}

func TestChromePageAwayFromMuse(t *testing.T) {
	f := newFakeDevTools(t, "https://muse.ai/")
	f.answers["login"] = func([]json.RawMessage) any { return LoginState{LoggedIn: true, HTTP: 200} }
	page := newTestPage(f)
	defer page.Close()
	ctx := context.Background()
	if _, err := page.Login(ctx); err != nil {
		t.Fatal(err)
	}
	f.set(func(f *fakeDevTools) { f.away = true }) // the kept tab went to another site
	if _, err := page.Login(ctx); err != errTabMissing {
		t.Fatalf("err = %v, want errTabMissing", err)
	}
	if n := f.ran("login"); n != 1 {
		t.Fatalf("login ran %d times, want 1: a page away from muse.ai runs nothing", n)
	}
}

func TestChromePageClosedDialsPerCall(t *testing.T) {
	f := newFakeDevTools(t, "https://muse.ai/")
	f.answers["login"] = func([]json.RawMessage) any { return LoginState{LoggedIn: true, HTTP: 200} }
	page := newTestPage(f)
	ctx := context.Background()
	if _, err := page.Login(ctx); err != nil {
		t.Fatal(err)
	}
	page.Close()
	for range 2 {
		if _, err := page.Login(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if dials, _ := f.counts(); dials != 3 {
		t.Fatalf("dials = %d, want 3", dials)
	}
}

func TestChromePageWithoutMuseTab(t *testing.T) {
	f := newFakeDevTools(t, "https://example.com/")
	page := &chromePage{port: f.port, policy: config.DefaultAccountConfig(), callTimeout: 5 * time.Second}
	if _, err := page.Login(context.Background()); err != errTabMissing {
		t.Fatalf("err = %v", err)
	}
}

func TestPageSessionsAndUserMessages(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is needed to execute the page script")
	}
	script := `const assert = require('node:assert/strict');
let listCalls = 0;
let sessionsPayload = [{ session_id: 's0', status: 'completed' }];
const historyMap = {
  s1: {
    chat_events: [
      { role: 'user', display_text: 'Task: Create a new video.\nprompt:\nhello world' },
      { role: 'assistant', display_text: 'ok' }
    ]
  },
  s2: {
    chat_events: [
      { role: 'user', content: [{ text: 'part 1' }, { text: 'part 2' }] }
    ]
  },
  s3: {
    chat_events: [{ role: 'user' }]
  }
};
global.window = {
  __hatchEarlyGatewayRuntimeState: {
    activeRuntime: {
      getSnapshot: () => ({
        async sendRequest(method, params) {
          if (method === 'sessions.list') {
            listCalls++;
            return { sessions: sessionsPayload };
          }
          if (method === 'chat.history') {
            return historyMap[params.session_id] || { chat_events: [] };
          }
          throw new Error('unexpected ' + method);
        }
      })
    }
  }
};
const page = (` + pageSource + `)(1024);
(async () => {
  const first = await page.sessions(false);
  assert.deepEqual(first, [{ id: 's0', status: 'completed' }]);
  sessionsPayload = [{ session_id: 's1', status: 'running' }, { session_id: 's0', status: 'completed' }];
  // Cached within LIST_SHARE_MS when fresh is false, bypassed when fresh is true.
  const cached = await page.sessions(false);
  assert.equal(cached.length, 1);
  assert.equal(listCalls, 1);
  const fresh = await page.sessions(true);
  assert.equal(fresh.length, 2);
  assert.equal(listCalls, 2);
  const u1 = await page.userMessages('s1');
  assert.deepEqual(u1, [{ text: 'Task: Create a new video.\nprompt:\nhello world', hasText: true }]);
  const u2 = await page.userMessages('s2');
  assert.deepEqual(u2, [{ text: 'part 1\npart 2', hasText: true }]);
  const u3 = await page.userMessages('s3');
  assert.deepEqual(u3, [{ text: '', hasText: false }]);
})().catch(e => { console.error(e); process.exitCode = 1; });`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node)
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("page sessions/userMessages: %v\n%s", err, out)
	}
}

func TestChromePagePreSendErrorsWrappedForSendNotReads(t *testing.T) {
	ctx := context.Background()

	// 1. Missing tab -> Login returns errTabMissing directly; Send/SendTo wrap preSendError.
	fNoTab := newFakeDevTools(t, "https://example.com/")
	pNoTab := newTestPage(fNoTab)
	if _, err := pNoTab.Login(ctx); err != errTabMissing {
		t.Fatalf("login missing tab = %v, want errTabMissing", err)
	}
	if _, err := pNoTab.Send(ctx, "hi", nil); !errors.Is(err, errTabMissing) || !isPreSendError(err) {
		t.Fatalf("send missing tab = %v", err)
	}
	if _, err := pNoTab.SendTo(ctx, "s1", "hi"); !errors.Is(err, errTabMissing) || !isPreSendError(err) {
		t.Fatalf("sendTo missing tab = %v", err)
	}

	// 2. Away from muse.ai -> Send wraps preSendError without running send on tab.
	fAway := newFakeDevTools(t, "https://muse.ai/")
	fAway.set(func(f *fakeDevTools) { f.away = true })
	pAway := newTestPage(fAway)
	if _, err := pAway.Send(ctx, "hi", nil); !errors.Is(err, errTabMissing) || !isPreSendError(err) {
		t.Fatalf("send away = %v", err)
	}
	if n := fAway.ran("send"); n != 0 {
		t.Fatalf("away send ran %d times", n)
	}

	// 3. CDP unreachable -> Send wraps preSendError.
	fDown := newFakeDevTools(t, "https://muse.ai/")
	fDown.server.Close()
	pDown := newTestPage(fDown)
	if _, err := pDown.Send(ctx, "hi", nil); err == nil || !isPreSendError(err) {
		t.Fatalf("send cdp down = %v", err)
	}

	// 4. Dropped after send started -> NOT preSendError.
	fDrop := newFakeDevTools(t, "https://muse.ai/")
	fDrop.set(func(f *fakeDevTools) { f.dropOn = "send" })
	pDrop := newTestPage(fDrop)
	if _, err := pDrop.Send(ctx, "hi", nil); err == nil || isPreSendError(err) {
		t.Fatalf("send drop in-flight = %v", err)
	}
}

func isPreSendError(err error) bool {
	var pre *preSendError
	return errors.As(err, &pre)
}

func TestPageScriptWaitsBrieflyForRuntimeInSessions(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is needed to execute the page script")
	}
	script := `const assert = require('node:assert/strict');
global.document = { body: { children: [], contains: () => false } };
global.location = { pathname: '/' };
global.window = {};
setTimeout(() => {
  global.window.__hatchEarlyGatewayRuntimeState = {
    activeRuntime: {
      getSnapshot: () => ({
        async sendRequest(method) {
          assert.equal(method, 'sessions.list');
          return { sessions: [{ session_id: 's1', status: 'running' }] };
        }
      })
    }
  };
}, 180);
const page = (` + pageSource + `)(1024);
(async () => {
  const list = await page.sessions(true);
  assert.deepEqual(list, [{ id: 's1', status: 'running' }]);
})().catch(e => { console.error(e); process.exitCode = 1; });`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node)
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("page sessions runtime wait: %v\n%s", err, out)
	}
}

func TestChromePageInlineOpenDownloadSkipsSliceAndClose(t *testing.T) {
	f := newFakeDevTools(t, MuseURL)
	payload := []byte("small-webp-image-payload")
	f.answers["open"] = func([]json.RawMessage) any {
		return map[string]any{
			"id":   0,
			"size": len(payload),
			"data": base64.StdEncoding.EncodeToString(payload),
		}
	}
	f.answers["slice"] = func([]json.RawMessage) any {
		t.Error("slice should not be called for inline open payload")
		return ""
	}
	f.answers["closeFile"] = func([]json.RawMessage) any {
		t.Error("closeFile should not be called when open did not retain a file handle")
		return nil
	}
	page := newTestPage(f)
	defer page.Close()
	var out bytes.Buffer
	n, err := page.Download(context.Background(), "/out.webp", &out)
	if err != nil || n != int64(len(payload)) || !bytes.Equal(out.Bytes(), payload) {
		t.Fatalf("inline download = (%d, %v), bytes=%q", n, err, out.Bytes())
	}
	if f.ran("slice") != 0 || f.ran("closeFile") != 0 {
		t.Fatalf("unexpected slice=%d closeFile=%d", f.ran("slice"), f.ran("closeFile"))
	}
}

func TestPageScriptCachesChatAndInlinesSmallDownload(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is needed to execute the page script")
	}
	script := `const assert = require('node:assert/strict');
let fiberScans = 0;
const client = { ensureActiveChatSendReady() {}, sendMessage: async () => ({ ok: true, sessionId: 's1' }) };
const rootHost = { stateNode: null };
const chatFiber = { return: rootHost, memoizedProps: { value: client } };
const rootFiber = new Proxy({ tag: 3, child: chatFiber }, {
  get(target, prop) {
    if (prop === 'child') fiberScans++;
    return target[prop];
  }
});
chatFiber.return = rootFiber;
rootHost.stateNode = { current: rootFiber };
const rootEl = { __reactContainer$test: rootHost };
const bodyEl = {
  children: [rootEl],
  contains(el) { return el === bodyEl || el === rootEl; }
};
global.document = { body: bodyEl };
global.location = { pathname: '/' };
global.fetch = async () => ({ status: 200, async json() { return {}; } });
const fakeFiles = {
  '/small.webp': new Blob([new Uint8Array([1, 2, 3, 4])]),
  '/large.mp4': new Blob([new Uint8Array(1500).fill(65)])
};
global.window = {
  __hatchEarlyGatewayRuntimeState: {
    activeRuntime: {
      getSnapshot: () => ({
        async sendRequest(method, params) {
          if (method === 'fs.raw') return { data: fakeFiles['/' + params.path.replace(/^\/+/, '')] };
          throw new Error('unexpected ' + method);
        }
      })
    }
  }
};
const page = (` + pageSource + `)(1024);
(async () => {
  await page.login();
  await page.login();
  await page.send('hello', [], '');
  assert.equal(fiberScans, 1, 'chat() should cache fiber across login and send');

  // Route change invalidates cache.
  global.location.pathname = '/settings';
  const away = await page.login();
  assert.equal(away.chat, true); // tree still exists in this mock, but had to rescan on path change
  assert.equal(fiberScans, 2);

  // Unmounted chat fiber returns null instead of stale client.
  global.location.pathname = '/';
  await page.login();
  chatFiber.return = null;
  rootFiber.child = null;
  const unmounted = await page.login();
  assert.equal(unmounted.chat, false);

  // Small blob <= CHUNK (1024) inlines data with id=0; large blob uses slice + closeFile.
  const small = await page.open('/small.webp', 10000);
  assert.equal(small.id, 0);
  assert.equal(small.size, 4);
  assert.equal(Buffer.from(small.data, 'base64').toString('hex'), '01020304');

  const big = await page.open('/large.mp4', 10000);
  assert.ok(big.id > 0);
  assert.equal(big.size, 1500);
  const c0 = Buffer.from(await page.slice(big.id, 0), 'base64');
  const c1 = Buffer.from(await page.slice(big.id, 1), 'base64');
  assert.equal(c0.length, 1024);
  assert.equal(c1.length, 476);
  page.closeFile(big.id);

  // Stalled /api/session fetch aborts at timeoutMs rather than hanging the CDP call.
  global.fetch = (_url, opts) => new Promise((_resolve, reject) => {
    opts.signal.addEventListener('abort', () => reject(new Error('aborted')), { once: true });
  });
  const timedOut = await page.login(40);
  assert.equal(timedOut.loggedIn, false);
  assert.equal(timedOut.error, 'aborted');
})().catch(e => { console.error(e); process.exitCode = 1; });`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node)
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("page cache/download: %v\n%s", err, out)
	}
}

func TestPageScriptEnforcesVisibilityAndFastFailsUnreachableGateway(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is needed to execute the page script")
	}
	script := `const assert = require('node:assert/strict');
class MockDoc {}
let hiddenState = true;
Object.defineProperty(MockDoc.prototype, 'hidden', { get: () => hiddenState, configurable: true });
Object.defineProperty(MockDoc.prototype, 'visibilityState', { get: () => hiddenState ? 'hidden' : 'visible', configurable: true });
let visEvents = 0, sendCalls = 0, reconnectCalls = 0;
global.Document = MockDoc;
global.Event = function(type) { this.type = type; };
const client = {
  ensureActiveChatSendReady() {},
  sendMessage: async () => { sendCalls++; return { ok: true, sessionId: 'ok-1' }; }
};
const doc = new MockDoc();
doc.body = { __reactFiber: { memoizedProps: { value: client } }, querySelectorAll: () => [] };
doc.dispatchEvent = (e) => { if (e?.type === 'visibilitychange') visEvents++; };
global.document = doc;
global.location = { pathname: '/' };
let simNow = 1000;
Date.now = () => simNow;
const realSetTimeout = setTimeout;
global.setTimeout = (fn, ms) => realSetTimeout(() => { simNow += Math.max(ms, 1); fn(); }, 1);
let snap = {
  connectionState: 'error',
  isConnected: false,
  recoveryPaused: true,
  lastFailureKind: 'gateway_unreachable',
  lastDialRefused: true,
  ensureLiveConnection() { reconnectCalls++; }
};
global.window = {
  dispatchEvent() {},
  __hatchEarlyGatewayRuntimeState: { activeRuntime: { getSnapshot: () => snap } }
};
const page = (` + pageSource + `)(1024);
(async () => {
  // Initial load overrode Document.prototype and fired visibilitychange because document.hidden was true.
  assert.equal(doc.hidden, false);
  assert.equal(doc.visibilityState, 'visible');
  assert.ok(visEvents >= 1);

  // Dead gateway returns not_ready quickly without invoking sendMessage.
  const dead = await page.send('prompt 1', [], '');
  assert.equal(dead.ok, false);
  assert.ok(dead.reason.startsWith('not_ready (gateway:'), dead.reason);
  assert.equal(sendCalls, 0);
  assert.ok(reconnectCalls >= 1);

  // Once reconnect wakes gateway, send proceeds normally.
  snap.ensureLiveConnection = () => { snap.isConnected = true; snap.connectionState = 'connected'; snap.recoveryPaused = false; };
  const ok = await page.send('prompt 2', [], '');
  assert.equal(ok.ok, true);
  assert.equal(ok.sessionId, 'ok-1');
  assert.equal(sendCalls, 1);
})().catch(e => { console.error(e); process.exitCode = 1; });`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node)
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gateway wake/fast-fail test failed: %v\n%s", err, out)
	}
}

func TestPageScriptSkipsTRPCProxyAndCachesBodyFiber(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is needed to execute the page script")
	}
	script := `const assert = require('node:assert/strict');
const trpcSym = Symbol.for('trpc_untypedClient');
const makeTrpcProxy = () => {
  const fn = () => {};
  return new Proxy(fn, {
    get(_t, prop) {
      if (prop === 'then') return undefined;
      if (prop === trpcSym) return { query() {}, mutation() {} };
      return new Proxy(fn, {
        get() { return makeTrpcProxy(); },
        apply() { throw new TypeError('e[i] is not a function'); }
      });
    }
  });
};
const objectProxy = new Proxy({}, {
  get(_t, prop) {
    if (prop === 'then') return undefined;
    return () => { throw new TypeError('catch-all object proxy'); };
  }
});
let sendCalls = 0;
let scans = 0;
const realClient = {
  ensureActiveChatSendReady() {},
  sendMessage: async (text) => { sendCalls++; return { ok: true, sessionId: 'real-' + text }; }
};
const chatFiber = { memoizedProps: { value: realClient } };
const objProxyFiber = { memoizedProps: { value: objectProxy }, child: chatFiber };
const trpcFiber = { memoizedProps: { value: makeTrpcProxy() }, child: objProxyFiber };
const bodyFiber = new Proxy({ memoizedProps: {}, child: trpcFiber }, {
  get(t, prop) {
    if (prop === 'child') scans++;
    return t[prop];
  }
});
trpcFiber.return = bodyFiber;
objProxyFiber.return = trpcFiber;
chatFiber.return = objProxyFiber;
const rootFiber = { tag: 3, child: bodyFiber };
bodyFiber.return = rootFiber;

const bodyEl = {
  __reactFiber$app: bodyFiber,
  children: [],
  contains(el) { return el === bodyEl; }
};
global.document = { body: bodyEl };
global.location = { pathname: '/' };
global.fetch = async () => ({ status: 200, async json() { return {}; } });
global.window = { __hatchEarlyGatewayRuntimeState: { activeRuntime: { getSnapshot: () => ({ isConnected: true }) } } };

const page = (` + pageSource + `)(1024);
(async () => {
  const l1 = await page.login();
  assert.equal(l1.loggedIn, true);
  assert.equal(l1.chat, true);
  const res = await page.send('hello', [], '');
  assert.equal(res.ok, true);
  assert.equal(res.sessionId, 'real-hello');
  assert.equal(sendCalls, 1);
  // fiberMounted must recognize bodyFiber as an ancestor even though bodyFiber.return !== null.
  assert.equal(scans, 1, 'should reuse cached realClient fiber on body __reactFiber');
})().catch(e => { console.error(e); process.exitCode = 1; });`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node)
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("trpc proxy / body fiber cache test failed: %v\n%s", err, out)
	}
}
