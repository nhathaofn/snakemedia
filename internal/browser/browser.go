package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"SnakeMedia/internal/config"
	"SnakeMedia/pkg/utils"
	"github.com/gorilla/websocket"
)

type browserVersion struct {
	Browser   string `json:"Browser"`
	UserAgent string `json:"User-Agent"`
	WebSocket string `json:"webSocketDebuggerUrl"`
}

// headless reports whether the running Chrome has no window; an adopted
// browser from an earlier run must not be handed out as a visible one.
func (v browserVersion) headless() bool {
	return strings.Contains(v.Browser, "Headless") || strings.Contains(v.UserAgent, "HeadlessChrome")
}

// This app-owned marker records an explicit, dynamically allocated port. It
// avoids Chrome's special port-zero automation mode while allowing reconnection.
const endpointFileName = ".cdp-endpoint.json"

type browserEndpoint struct {
	Port      int    `json:"port"`
	WebSocket string `json:"webSocket"`
}

// Browser identifies a specific launched browser, rather than any server that
// happens to answer at a shared port. Process completion is always reaped.
type Browser struct {
	Port       int
	ProfileDir string
	Version    string
	Headless   bool
	endpoint   string
	cmd        *exec.Cmd
	done       chan struct{}
}

func (b *Browser) Validate(ctx context.Context, policy config.AccountConfig) error {
	var v browserVersion
	if err := LocalJSON(ctx, b.Port, "/json/version", policy, &v); err != nil {
		return err
	}
	if v.WebSocket != b.endpoint || !LocalWebSocket(v.WebSocket, b.Port) {
		return errors.New("Chrome đã đổi phiên; hãy mở lại đúng profile.")
	}
	return nil
}

// StartBrowser uses Chrome's OS-assigned port by default. An explicit preferred
// port is honored only when free; it never attaches to an unrelated listener.
func StartBrowser(ctx context.Context, opts LaunchOptions, policy config.AccountConfig) (*Browser, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(policy.BrowserStartupTimeoutSeconds)*time.Second)
	defer cancel()
	// Chrome may still be running after a previous app exit. A dynamic-port file
	// is scoped to the requested profile and includes the browser's exact ID.
	activeFile := filepath.Join(opts.ProfileDir, "DevToolsActivePort")
	endpointFile := filepath.Join(opts.ProfileDir, endpointFileName)
	if data, err := os.ReadFile(endpointFile); err == nil {
		var saved browserEndpoint
		if json.Unmarshal(data, &saved) == nil && LocalWebSocket(saved.WebSocket, saved.Port) {
			b := &Browser{Port: saved.Port, ProfileDir: opts.ProfileDir, endpoint: saved.WebSocket}
			var v browserVersion
			if LocalJSON(ctx, b.Port, "/json/version", policy, &v) == nil && v.WebSocket == b.endpoint {
				b.Version, b.Headless = v.Browser, v.headless()
				return b, nil
			}
		}
	}
	if data, err := os.ReadFile(activeFile); err == nil {
		parts := strings.Fields(string(data))
		if len(parts) == 2 {
			port, err := strconv.Atoi(parts[0])
			if err == nil && port >= config.MinPort && port <= config.MaxPort {
				b := &Browser{Port: port, ProfileDir: opts.ProfileDir, endpoint: fmt.Sprintf("ws://127.0.0.1:%d%s", port, parts[1])}
				var v browserVersion
				if LocalJSON(ctx, port, "/json/version", policy, &v) == nil && v.WebSocket == b.endpoint {
					b.Version, b.Headless = v.Browser, v.headless()
					return b, nil
				}
			}
		}
	}
	{
		// Reserve an OS-selected port first, then pass its explicit number. Chrome
		// treats a literal --remote-debugging-port=0 as automation even when visible.
		listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(opts.Port)))
		if err != nil {
			return nil, errors.New("Không giữ được cổng CDP cho Chrome.")
		}
		opts.Port = listener.Addr().(*net.TCPAddr).Port
		_ = listener.Close()
	}
	// A stopped browser leaves this runtime marker behind. Do not let its stale
	// port pin the startup loop to the wrong endpoint while Chrome assigns a new one.
	if err := os.Remove(activeFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("clear stale CDP marker: %w", err)
	}
	cmd, err := Launch(opts)
	if err != nil {
		return nil, err
	}
	b := &Browser{Port: opts.Port, ProfileDir: opts.ProfileDir, Headless: opts.Headless, cmd: cmd, done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(b.done) }()
	ticker := time.NewTicker(time.Duration(policy.PollIntervalMS) * time.Millisecond)
	defer ticker.Stop()
	for {
		if b.Port == 0 {
			if data, err := os.ReadFile(activeFile); err == nil {
				parts := strings.Fields(string(data))
				if len(parts) == 2 {
					port, e := strconv.Atoi(parts[0])
					if e == nil && port >= config.MinPort && port <= config.MaxPort {
						b.Port = port
						b.endpoint = fmt.Sprintf("ws://127.0.0.1:%d%s", port, parts[1])
					}
				}
			}
		}
		if b.Port != 0 {
			var v browserVersion
			if LocalJSON(ctx, b.Port, "/json/version", policy, &v) == nil && LocalWebSocket(v.WebSocket, b.Port) && (b.endpoint == "" || v.WebSocket == b.endpoint) {
				b.endpoint = v.WebSocket
				b.Version = v.Browser
				data, _ := json.Marshal(browserEndpoint{Port: b.Port, WebSocket: b.endpoint})
				if err := utils.WriteFileAtomic(endpointFile, data, 0o600); err != nil {
					_ = b.Close(policy)
					return nil, fmt.Errorf("save browser endpoint: %w", err)
				}
				return b, nil
			}
		}
		select {
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			return nil, fmt.Errorf("Chrome startup: %w", ctx.Err())
		case <-b.done:
			return nil, errors.New("Chrome đã thoát trước khi mở profile. Hãy đóng cửa sổ đang giữ profile rồi thử lại.")
		case <-ticker.C:
		}
	}
}

// Adopted reports whether this handle attached to a browser an earlier run
// launched instead of starting it; its command-line flags are then unknown.
func (b *Browser) Adopted() bool { return b != nil && b.cmd == nil }

// Close requests a graceful browser shutdown so Chrome flushes its profile. Only
// a process launched by this handle may be forcibly terminated on timeout.
func (b *Browser) Close(policy config.AccountConfig) error {
	if b == nil {
		return nil
	}
	if b.done != nil {
		select {
		case <-b.done:
			return nil
		default:
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(policy.RequestTimeoutSeconds)*time.Second)
	defer cancel()
	if err := b.Validate(ctx, policy); err == nil {
		if conn, _, err := websocket.DefaultDialer.DialContext(ctx, b.endpoint, nil); err == nil {
			stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
			_ = Call(ctx, conn, 1, "Browser.close", nil, nil)
			stop()
			_ = conn.Close()
		}
	} else if b.cmd == nil {
		conn, dialErr := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(b.Port)))
		if conn != nil {
			_ = conn.Close()
		}
		if isConnectionRefused(dialErr) {
			return nil
		}
		return fmt.Errorf("không xác nhận được phiên Chrome để đóng: %w", err)
	}
	if b.done == nil {
		ticker := time.NewTicker(time.Duration(policy.PollIntervalMS) * time.Millisecond)
		defer ticker.Stop()
		for {
			conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(b.Port)))
			if conn != nil {
				_ = conn.Close()
			}
			if isConnectionRefused(err) {
				return nil
			}
			select {
			case <-ctx.Done():
				return errors.New("Chrome chưa đóng xong; chưa thể xóa profile.")
			case <-ticker.C:
			}
		}
	}
	select {
	case <-b.done:
		return nil
	case <-ctx.Done():
		if err := b.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
		return errors.New("Chrome phải dừng cưỡng bức. Hãy thử xóa lại sau khi tiến trình đã thoát.")
	}
}

// ExistingBrowser finds only a profile-scoped endpoint; it never adopts whatever
// happens to be listening on a configured or global port.
func ExistingBrowser(ctx context.Context, profile string, policy config.AccountConfig) (*Browser, error) {
	if data, err := os.ReadFile(filepath.Join(profile, endpointFileName)); err == nil {
		var saved browserEndpoint
		if json.Unmarshal(data, &saved) != nil || !LocalWebSocket(saved.WebSocket, saved.Port) {
			return nil, errors.New("invalid profile browser endpoint")
		}
		b := &Browser{Port: saved.Port, ProfileDir: profile, endpoint: saved.WebSocket}
		return b, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if data, err := os.ReadFile(filepath.Join(profile, "DevToolsActivePort")); err == nil {
		parts := strings.Fields(string(data))
		if len(parts) != 2 {
			return nil, errors.New("invalid legacy browser endpoint")
		}
		port, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, err
		}
		endpoint := fmt.Sprintf("ws://127.0.0.1:%d%s", port, parts[1])
		if !LocalWebSocket(endpoint, port) {
			return nil, errors.New("invalid legacy browser endpoint")
		}
		return &Browser{Port: port, ProfileDir: profile, endpoint: endpoint}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return nil, nil
}
