package muse

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

	"github.com/gorilla/websocket"
)

// target is one entry of Chrome's /json listing.
type target struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// musePage reports whether t is a muse.ai tab.
func (t target) musePage() bool {
	u, err := url.Parse(t.URL)
	return err == nil && t.Type == "page" && u.Scheme == "https" && (u.Host == "muse.ai" || u.Host == "www.muse.ai")
}

// localEndpoint accepts only a loopback debugger socket on the browser's own port.
func localEndpoint(raw string, port int) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "ws" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost") && u.Port() == strconv.Itoa(port) && u.User == nil
}

// devtoolsJSON calls one of Chrome's local /json endpoints.
func devtoolsJSON(ctx context.Context, method string, port int, path string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), nil)
	if err != nil {
		return err
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("Chrome của tài khoản chưa sẵn sàng.")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Chrome trả về HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

// cdpConn is one DevTools connection to a tab. It is not safe for concurrent use.
type cdpConn struct {
	ws   *websocket.Conn
	next int
}

func dialTarget(ctx context.Context, t target, port int) (*cdpConn, error) {
	if !localEndpoint(t.WebSocketDebuggerURL, port) {
		return nil, errors.New("Địa chỉ tab Muse không hợp lệ.")
	}
	ws, _, err := websocket.DefaultDialer.DialContext(ctx, t.WebSocketDebuggerURL, nil)
	if err != nil {
		return nil, err
	}
	// One file chunk is chunkSize bytes base64-encoded inside a JSON string.
	ws.SetReadLimit(4 * chunkSize)
	return &cdpConn{ws: ws}, nil
}

func (c *cdpConn) Close() error { return c.ws.Close() }

// call sends one command and waits for its reply, skipping events. Cancelling
// ctx closes the connection, interrupting a pending read.
func (c *cdpConn) call(ctx context.Context, method string, params any, out any) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		return errors.New("CDP call requires a deadline")
	}
	stop := context.AfterFunc(ctx, func() { _ = c.ws.Close() })
	defer stop()
	c.next++
	id := c.next
	_ = c.ws.SetWriteDeadline(deadline)
	_ = c.ws.SetReadDeadline(deadline)
	if err := c.ws.WriteJSON(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return contextErr(ctx, err)
	}
	for {
		var msg struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := c.ws.ReadJSON(&msg); err != nil {
			return contextErr(ctx, err)
		}
		if msg.ID != id {
			continue
		}
		if msg.Error != nil {
			return fmt.Errorf("CDP %s: %s (%d)", method, msg.Error.Message, msg.Error.Code)
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(msg.Result, out)
	}
}

// evaluate runs an expression that resolves to a JSON string and decodes it.
func (c *cdpConn) evaluate(ctx context.Context, expression string, out any) error {
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
	if err := c.call(ctx, "Runtime.evaluate", params, &reply); err != nil {
		return err
	}
	if e := reply.Exception; e != nil {
		detail := e.Text
		if e.Exception != nil && e.Exception.Description != "" {
			detail = e.Exception.Description
		}
		return &pageError{detail: truncate(detail, 300)}
	}
	if reply.Result.Type != "string" {
		return errors.New("Trang Muse trả về dữ liệu không hợp lệ.")
	}
	return json.Unmarshal([]byte(reply.Result.Value), out)
}

// pageError is a JavaScript exception thrown inside the Muse tab (for example
// Muse's runtime not being loaded yet). The page answered, so it is definite.
type pageError struct{ detail string }

func (e *pageError) Error() string { return "Lỗi trong trang Muse: " + e.detail }

// contextErr prefers the context's reason over the socket error it caused.
func contextErr(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}
