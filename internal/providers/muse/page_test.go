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

	"github.com/gorilla/websocket"
)

// fakeDevTools serves /json and one tab's debugger socket. Runtime.evaluate
// answers by the window.__mss method named in the expression.
type fakeDevTools struct {
	t       *testing.T
	server  *httptest.Server
	port    int
	tabURL  string
	answers map[string]func(args []json.RawMessage) any
	mu      sync.Mutex
	methods []string
}

var callPattern = regexp.MustCompile(`\)\["(\w+)"\]\(\.\.\.(\[.*\])\)\)\)\(\)$`)

func newFakeDevTools(t *testing.T, tabURL string) *fakeDevTools {
	f := &fakeDevTools{t: t, tabURL: tabURL, answers: map[string]func([]json.RawMessage) any{}}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f.port = listener.Addr().(*net.TCPAddr).Port
	mux := http.NewServeMux()
	mux.HandleFunc("/json", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode([]target{{ID: "1", Type: "page", URL: f.tabURL, WebSocketDebuggerURL: fmt.Sprintf("ws://127.0.0.1:%d/devtools/page/1", f.port)}})
	})
	mux.HandleFunc("/devtools/page/1", f.serveSocket)
	f.server = &httptest.Server{Listener: listener, Config: &http.Server{Handler: mux}}
	f.server.Start()
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeDevTools) serveSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
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
			m := callPattern.FindStringSubmatch(msg.Params.Expression)
			if m == nil || !strings.Contains(msg.Params.Expression, "function pageMain(CHUNK)") {
				f.t.Errorf("unexpected expression tail: %.80q", msg.Params.Expression[max(0, len(msg.Params.Expression)-120):])
				return
			}
			var args []json.RawMessage
			json.Unmarshal([]byte(m[2]), &args)
			answer, ok := f.answers[m[1]]
			if !ok {
				f.t.Errorf("unexpected call %s", m[1])
				return
			}
			value, _ := json.Marshal(answer(args))
			result = map[string]any{"result": map[string]any{"type": "string", "value": string(value)}}
		}
		conn.WriteJSON(map[string]any{"id": msg.ID, "result": result})
	}
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

	page := &chromePage{port: f.port, callTimeout: 5 * time.Second, sendTimeout: 5 * time.Second}
	ctx := context.Background()
	if state, err := page.Login(ctx); err != nil || !state.LoggedIn {
		t.Fatalf("login = %+v, %v", state, err)
	}
	if res, err := page.Send(ctx, "hello", nil, ""); err != nil || !res.OK || res.SessionID != "s1" {
		t.Fatalf("send = %+v, %v", res, err)
	}
	var out bytes.Buffer
	n, err := page.Download(ctx, "sandbox:///out.mp4", &out)
	if err != nil || n != int64(len(file)) || !bytes.Equal(out.Bytes(), file) || !closed {
		t.Fatalf("download n=%d err=%v closed=%v", n, err, closed)
	}
}

func TestChromePageWithoutMuseTab(t *testing.T) {
	f := newFakeDevTools(t, "https://example.com/")
	page := &chromePage{port: f.port, callTimeout: 5 * time.Second}
	if _, err := page.Login(context.Background()); err != errTabMissing {
		t.Fatalf("err = %v", err)
	}
}
