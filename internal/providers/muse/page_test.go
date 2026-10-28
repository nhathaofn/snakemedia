package muse

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
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
