package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"SnakeMedia/internal/config"
	"github.com/gorilla/websocket"
)

// Target is one entry of Chrome's /json listing (a tab, worker or frame).
type Target struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// Page reports whether t is a tab and returns its parsed URL.
func (t Target) Page() (*url.URL, bool) {
	u, err := url.Parse(t.URL)
	return u, err == nil && t.Type == "page"
}

// LocalWebSocket accepts only a loopback debugger socket on the browser's own port.
func LocalWebSocket(raw string, port int) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "ws" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost") && u.Port() == strconv.Itoa(port) && u.User == nil
}

// ReadBounded reads r fully, failing once it exceeds limit bytes.
func ReadBounded(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("response exceeds configured size limit")
	}
	return data, nil
}

// LocalJSON calls one of Chrome's local DevTools HTTP endpoints with GET.
func LocalJSON(ctx context.Context, port int, path string, policy config.AccountConfig, out any) error {
	return devtools(ctx, http.MethodGet, port, path, policy, out)
}

func devtools(ctx context.Context, method string, port int, path string, policy config.AccountConfig, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: time.Duration(policy.RequestTimeoutSeconds) * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("Chrome CDP chưa sẵn sàng.")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("Chrome CDP trả về phản hồi không hợp lệ.")
	}
	data, err := ReadBounded(resp.Body, policy.MaxResponseBytes)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

// Targets lists the browser's tabs and other debuggable targets.
func Targets(ctx context.Context, port int, policy config.AccountConfig) ([]Target, error) {
	var targets []Target
	err := LocalJSON(ctx, port, "/json", policy, &targets)
	return targets, err
}

// NewTab opens a blank tab and returns it.
func NewTab(ctx context.Context, port int, policy config.AccountConfig) (Target, error) {
	var created Target
	err := devtools(ctx, http.MethodPut, port, "/json/new?about:blank", policy, &created)
	return created, err
}

// Call sends one CDP command on conn and waits for its reply. Replies to other
// IDs and events go to handlers. ctx must carry a deadline; callers close conn
// on cancellation to interrupt a pending read.
func Call(ctx context.Context, conn *websocket.Conn, id int, method string, params any, result any, handlers ...func(string, json.RawMessage)) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		return errors.New("CDP operation requires a deadline")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_ = conn.SetWriteDeadline(deadline)
	_ = conn.SetReadDeadline(deadline)
	if err := conn.WriteJSON(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return contextErr(ctx, err)
	}
	for {
		var msg struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := conn.ReadJSON(&msg); err != nil {
			return contextErr(ctx, err)
		}
		if msg.ID != id {
			for _, handle := range handlers {
				handle(msg.Method, msg.Params)
			}
			continue
		}
		if msg.Error != nil {
			return fmt.Errorf("CDP %s: %s (%d)", method, msg.Error.Message, msg.Error.Code)
		}
		if result == nil {
			return nil
		}
		return json.Unmarshal(msg.Result, result)
	}
}

// EvalError is a JavaScript exception thrown by an evaluated expression. The
// page answered, so the outcome is definite.
type EvalError struct{ Detail string }

func (e *EvalError) Error() string { return "Lỗi trong trang: " + e.Detail }

// Evaluate runs an expression that resolves to a JSON string and decodes it into out.
func Evaluate(ctx context.Context, conn *websocket.Conn, id int, expression string, out any) error {
	var reply struct {
		Result struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"result"`
		Exception *struct {
			Text      string `json:"text"`
			Exception *struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	params := map[string]any{"expression": expression, "awaitPromise": true, "returnByValue": true}
	if err := Call(ctx, conn, id, "Runtime.evaluate", params, &reply); err != nil {
		return err
	}
	if e := reply.Exception; e != nil {
		detail := e.Text
		if e.Exception != nil && e.Exception.Description != "" {
			detail = e.Exception.Description
		}
		if r := []rune(detail); len(r) > 300 {
			detail = string(r[:300]) + "…"
		}
		return &EvalError{Detail: detail}
	}
	if reply.Result.Value == "" || (reply.Result.Type != "" && reply.Result.Type != "string") {
		return errors.New("Trang trả về dữ liệu không hợp lệ.")
	}
	return json.Unmarshal([]byte(reply.Result.Value), out)
}

// Conn is one DevTools connection to a target that numbers its own commands
// and closes itself when a call's context ends. It is not safe for concurrent use.
type Conn struct {
	ws   *websocket.Conn
	next int
}

// Dial connects to target t of the browser listening on port. readLimit bounds
// one message; zero keeps the websocket default.
func Dial(ctx context.Context, t Target, port int, readLimit int64) (*Conn, error) {
	if !LocalWebSocket(t.WebSocketDebuggerURL, port) {
		return nil, errors.New("Địa chỉ tab Chrome không hợp lệ.")
	}
	ws, _, err := websocket.DefaultDialer.DialContext(ctx, t.WebSocketDebuggerURL, nil)
	if err != nil {
		return nil, err
	}
	if readLimit > 0 {
		ws.SetReadLimit(readLimit)
	}
	return &Conn{ws: ws}, nil
}

func (c *Conn) Close() error { return c.ws.Close() }

// Call sends one command; see the package-level Call.
func (c *Conn) Call(ctx context.Context, method string, params any, out any) error {
	stop := context.AfterFunc(ctx, func() { _ = c.ws.Close() })
	defer stop()
	c.next++
	return Call(ctx, c.ws, c.next, method, params, out)
}

// Evaluate runs a JSON-returning expression; see the package-level Evaluate.
func (c *Conn) Evaluate(ctx context.Context, expression string, out any) error {
	stop := context.AfterFunc(ctx, func() { _ = c.ws.Close() })
	defer stop()
	c.next++
	return Evaluate(ctx, c.ws, c.next, expression, out)
}

// contextErr prefers the context's reason over the socket error it caused.
func contextErr(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}
