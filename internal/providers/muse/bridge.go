package muse

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
	"time"

	"golang.org/x/net/proxy"
)

type localBridge struct {
	listener net.Listener
	port     int
	target   *ParsedProxy
	closed   chan struct{}
}

type bridgeManager struct {
	mu      sync.Mutex
	bridges map[string]*localBridge
}

func newBridgeManager() *bridgeManager {
	return &bridgeManager{
		bridges: make(map[string]*localBridge),
	}
}

// GetBridge returns a 127.0.0.1 port that forwards traffic to upstream p.
func (m *bridgeManager) GetBridge(p *ParsedProxy) (int, error) {
	if p == nil || p.Host == "" || p.Port == 0 {
		return 0, fmt.Errorf("invalid upstream proxy")
	}

	key := fmt.Sprintf("%s://%s:%d:%s:%s", p.Scheme, p.Host, p.Port, p.User, p.Pass)
	m.mu.Lock()
	defer m.mu.Unlock()

	if b, ok := m.bridges[key]; ok {
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
	}
	m.bridges[key] = b

	go b.serve()
	return port, nil
}

func (b *localBridge) serve() {
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
		go b.handleClient(client)
	}
}

func (b *localBridge) handleClient(client net.Conn) {
	defer client.Close()
	reader := bufio.NewReader(client)

	req, err := http.ReadRequest(reader)
	if err != nil {
		return
	}

	target := b.target
	var upstream net.Conn

	if target.Scheme == "socks5" {
		var auth *proxy.Auth
		if target.User != "" || target.Pass != "" {
			auth = &proxy.Auth{User: target.User, Password: target.Pass}
		}
		dialer, err := proxy.SOCKS5("tcp", net.JoinHostPort(target.Host, strconv.Itoa(target.Port)), auth, &net.Dialer{Timeout: 15 * time.Second})
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
		upstream, err = net.DialTimeout("tcp", net.JoinHostPort(target.Host, strconv.Itoa(target.Port)), 15*time.Second)
		if err != nil {
			return
		}
	}
	defer upstream.Close()

	if req.Method == http.MethodConnect {
		if target.Scheme != "socks5" {
			connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", req.Host, req.Host)
			if target.User != "" {
				cred := base64.StdEncoding.EncodeToString([]byte(target.User + ":" + target.Pass))
				connectReq += "Proxy-Authorization: Basic " + cred + "\r\n"
			}
			connectReq += "\r\n"

			if _, err := upstream.Write([]byte(connectReq)); err != nil {
				return
			}
			upReader := bufio.NewReader(upstream)
			respLine, err := upReader.ReadString('\n')
			if err != nil || !strings.Contains(respLine, " 200 ") {
				return
			}
			// Skip headers until \r\n\r\n
			for {
				line, err := upReader.ReadString('\n')
				if err != nil || line == "\r\n" || line == "\n" {
					break
				}
			}
		}

		if _, err := client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
			return
		}

		pipe(client, upstream)
		return
	}

	// Plain HTTP forward
	if target.Scheme != "socks5" && target.User != "" {
		cred := base64.StdEncoding.EncodeToString([]byte(target.User + ":" + target.Pass))
		req.Header.Set("Proxy-Authorization", "Basic "+cred)
	}
	if err := req.Write(upstream); err != nil {
		return
	}
	pipe(client, upstream)
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

func (m *bridgeManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range m.bridges {
		close(b.closed)
		_ = b.listener.Close()
	}
	m.bridges = make(map[string]*localBridge)
}
