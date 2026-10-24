package chrome

import (
	"context"
	"errors"
	"net/url"
	"time"

	"SnakeMedia/internal/config"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// OpenProject loads the real studio route so its lazy RPC descriptors exist.
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
	for _, page := range pages {
		u, err := url.Parse(page.URL)
		if err != nil || page.Type != "page" || u.Scheme != "https" || u.Host != "flow.google.com" {
			continue
		}
		if u.Path == "/project/"+projectID {
			return nil
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
		return cdpCall(ctx, conn, 1, "Page.navigate", map[string]any{"url": FlowURL + "/project/" + projectID}, nil)
	}
	return ErrPageNotReady
}
