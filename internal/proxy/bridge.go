package proxy

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	xproxy "golang.org/x/net/proxy"

	"SnakeMedia/internal/logger"
)

// bridgeGrace keeps a bridge nobody's route names yet: a route is picked
// before its browser finishes starting (up to the startup timeout).
const bridgeGrace = 5 * time.Minute

type localBridge struct {
	listener net.Listener
	port     int
	target   *Parsed
	closed   chan struct{}
	timeout  func() time.Duration
	// active counts open client connections; lastUsed (guarded by bridges.mu)
	// is the last time a route asked for the bridge.
	active   atomic.Int32
	lastUsed time.Time
}

type bridges struct {
	mu      sync.Mutex
	bridges map[string]*localBridge
	now     func() time.Time
	// timeout bounds dialing the upstream and reading a request or the
	// upstream's CONNECT answer, so a hung proxy never pins a goroutine.
	timeout func() time.Duration
}

func newBridges(timeout func() time.Duration) *bridges {
	return &bridges{
		bridges: make(map[string]*localBridge),
		now:     time.Now,
		timeout: timeout,
	}
}

func bridgeKey(p *Parsed) string {
	return fmt.Sprintf("%s://%s:%d:%s:%s", p.Scheme, p.Host, p.Port, p.User, p.Pass)
}

// GetBridge returns a 127.0.0.1 port that forwards traffic to upstream p.
func (m *bridges) GetBridge(p *Parsed) (int, error) {
	if p == nil || p.Host == "" || p.Port == 0 {
		return 0, fmt.Errorf("invalid upstream proxy")
	}

	key := bridgeKey(p)
	m.mu.Lock()
	defer m.mu.Unlock()

	if b, ok := m.bridges[key]; ok {
		b.lastUsed = m.now()
		return b.port, nil
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("local bridge listen error: %w", err)
	}

	port := ln.Addr().(*net.TCPAddr).Port
	b := &localBridge{
		listener: ln,
		port:     port,
		target:   p,
		closed:   make(chan struct{}),
		timeout:  m.timeout,
		lastUsed: m.now(),
	}
	m.bridges[key] = b

	go b.serve()
	return port, nil
}

// Prune closes bridges no route uses any more: not on a port in keep, no open
// connection and not asked for within bridgeGrace. Rotating proxies get a new
// upstream on every rotation, so without this their bridges pile up.
func (m *bridges) Prune(keep map[int]bool) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	closed := 0
	for key, b := range m.bridges {
		if keep[b.port] || b.active.Load() > 0 || m.now().Sub(b.lastUsed) < bridgeGrace {
			continue
		}
		b.close()
		delete(m.bridges, key)
		closed++
	}
	return closed
}

func (b *localBridge) close() {
	close(b.closed)
	_ = b.listener.Close()
}

func (b *localBridge) serve() {
	defer logger.Recover("cầu nối proxy")
	for {
		client, err := b.listener.Accept()
		if err != nil {
			select {
			case <-b.closed:
				return
			default:
				time.Sleep(50 * time.Millisecond)
				continue
			}
		}
		b.active.Add(1)
		go func() {
			defer b.active.Add(-1)
			defer logger.Recover("kết nối qua proxy")
			b.handleClient(client)
		}()
	}
}

func (b *localBridge) handleClient(client net.Conn) {
	defer client.Close()
	timeout := b.timeout()
	reader := bufio.NewReader(client)

	_ = client.SetReadDeadline(time.Now().Add(timeout))
	req, err := http.ReadRequest(reader)
	if err != nil {
		return
	}
	_ = client.SetReadDeadline(time.Time{})

	target := b.target
	var upstream net.Conn

	if target.Scheme == "socks5" {
		var auth *xproxy.Auth
		if target.User != "" || target.Pass != "" {
			auth = &xproxy.Auth{User: target.User, Password: target.Pass}
		}
		dialer, err := xproxy.SOCKS5("tcp", net.JoinHostPort(target.Host, strconv.Itoa(target.Port)), auth, &net.Dialer{Timeout: timeout})
		if err != nil {
			return
		}
		dest := req.Host
		if !strings.Contains(dest, ":") {
			dest = net.JoinHostPort(dest, "80")
		}
		upstream, err = dialer.Dial("tcp", dest)
		if err != nil {
			return
		}
	} else {
		// HTTP proxy
		var err error
		upstream, err = net.DialTimeout("tcp", net.JoinHostPort(target.Host, strconv.Itoa(target.Port)), timeout)
		if err != nil {
			return
		}
	}
	defer upstream.Close()

	if req.Method == http.MethodConnect {
		if target.Scheme != "socks5" {
			connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", req.Host, req.Host)
			if target.User != "" {
				connectReq += "Proxy-Authorization: " + basicAuth(target) + "\r\n"
			}
			connectReq += "\r\n"

			_ = upstream.SetDeadline(time.Now().Add(timeout))
			if _, err := upstream.Write([]byte(connectReq)); err != nil {
				return
			}
			upReader := bufio.NewReader(upstream)
			resp, err := http.ReadResponse(upReader, &http.Request{Method: http.MethodConnect})
			if err != nil {
				return
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK || upReader.Buffered() > 0 {
				return
			}
			_ = upstream.SetDeadline(time.Time{})
		}

		if _, err := client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
			return
		}

		pipe(client, upstream)
		return
	}

	// Plain HTTP: one request per connection. The upstream closes after its
	// answer, so a browser reusing the connection can never send a request for
	// another host down this one's tunnel.
	req.Close = true
	req.Header.Set("Connection", "close")
	req.Header.Del("Proxy-Connection")
	if target.Scheme == "socks5" {
		err = req.Write(upstream)
	} else {
		// An HTTP proxy needs the absolute URL (and its credentials).
		if target.User != "" {
			req.Header.Set("Proxy-Authorization", basicAuth(target))
		}
		err = req.WriteProxy(upstream)
	}
	if err != nil {
		return
	}
	pipe(client, upstream)
}

func basicAuth(p *Parsed) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(p.User+":"+p.Pass))
}

func pipe(c1, c2 net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(c1, c2)
		_ = c1.SetDeadline(time.Now())
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(c2, c1)
		_ = c2.SetDeadline(time.Now())
	}()
	wg.Wait()
}

func (m *bridges) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range m.bridges {
		b.close()
	}
	m.bridges = make(map[string]*localBridge)
}
