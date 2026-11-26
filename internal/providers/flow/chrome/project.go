package chrome

import (
	"context"
	"errors"
	"fmt"
	"time"

	"SnakeMedia/internal/config"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// OpenProject opens the studio and waits for the project's editor to mount.
// The route is a provider contract; the project ID always comes from Flow.
func OpenProject(ctx context.Context, port int, projectID string, policy config.AccountConfig) error {
	if _, err := uuid.Parse(projectID); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(policy.BrowserStartupTimeoutSeconds)*time.Second)
	defer cancel()
	var pages []pageEntry
	if err := localJSON(ctx, port, "/json", policy, &pages); err != nil {
		return err
	}
	path := "/project/" + projectID
	for _, page := range pages {
		u, ok := page.flowPage()
		if !ok {
			continue
		}
		if !localWebSocket(page.WebSocketDebuggerURL, port) {
			return errors.New("invalid project page endpoint")
		}
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, page.WebSocketDebuggerURL, nil)
		if err != nil {
			return err
		}
		defer conn.Close()
		stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
		defer stop()
		if u.Path != path {
			var navigation struct {
				ErrorText string `json:"errorText"`
			}
			if err := cdpCall(ctx, conn, 1, "Page.navigate", map[string]any{"url": FlowURL + path}, &navigation); err != nil {
				return err
			}
			if navigation.ErrorText != "" {
				return fmt.Errorf("không mở được dự án Flow: %s", navigation.ErrorText)
			}
		}
		return waitProjectEditor(ctx, conn, path, time.Duration(policy.PollIntervalMS)*time.Millisecond)
	}
	return ErrPageNotReady
}

func waitProjectEditor(ctx context.Context, conn *websocket.Conn, path string, interval time.Duration) error {
	script := `JSON.stringify(location.origin === ` + quoteJS(FlowURL) + ` && location.pathname === ` + quoteJS(path) + ` &&
  document.readyState === 'complete' && !!document.querySelector('textarea, [contenteditable="true"], [role="textbox"]'))`
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for id := 2; ; id++ {
		var ready bool
		// The old execution context can disappear while Chrome navigates.
		if err := evaluate(ctx, conn, id, script, &ready); err == nil && ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: trình soạn dự án chưa sẵn sàng: %w", ErrPageNotReady, ctx.Err())
		case <-ticker.C:
		}
	}
}
