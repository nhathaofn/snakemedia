package proxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	xproxy "golang.org/x/net/proxy"
)

func TestPickSkipsMalformedLinesInSharedList(t *testing.T) {
	now := time.Now()
	list := "not-a-valid-line\n1.2.3.4:notaport\n5.6.7.8:8080\n"
	p, line, err := pick(Assignment{Account: "acc1", Name: "Acc1"}, nil, list, nil, now)
	if err != nil {
		t.Fatalf("pick failed despite valid line in shared list: %v", err)
	}
	if p == nil || p.Host != "5.6.7.8" || line != "5.6.7.8:8080" {
		t.Fatalf("picked = (%+v, %q), want 5.6.7.8:8080", p, line)
	}
}

func TestDeadlineDialerBoundsStalledSOCKS5Handshake(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	// Accept TCP connection and stall forever without answering SOCKS5 greeting.
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, aErr := ln.Accept()
		if aErr == nil {
			accepted <- conn
		}
	}()
	defer func() {
		select {
		case c := <-accepted:
			_ = c.Close()
		default:
		}
	}()

	d, err := xproxy.SOCKS5("tcp", ln.Addr().String(), nil, &deadlineDialer{timeout: 120 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	conn, err := d.Dial("tcp", "example.com:443")
	elapsed := time.Since(start)
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil {
		t.Fatal("expected stalled SOCKS5 handshake to time out")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("stalled SOCKS5 handshake took %v, expected bounded deadline", elapsed)
	}
}

func TestBridgePreservesBufferedUpstreamBytesOnCONNECT(t *testing.T) {
	// Fake upstream HTTP proxy that sends "200 Connection Established\r\n\r\n"
	// AND the first bytes of the tunneled payload in a single TCP write.
	upstreamLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstreamLn.Close()

	go func() {
		conn, aErr := upstreamLn.Accept()
		if aErr != nil {
			return
		}
		defer conn.Close()
		br := bufio.NewReader(conn)
		_, _ = http.ReadRequest(br)
		_, _ = conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\nEARLY-TUNNEL-PAYLOAD"))
		// Keep open briefly until client finishes reading.
		buf := make([]byte, 16)
		_, _ = conn.Read(buf)
	}()

	target := Parse(upstreamLn.Addr().String())
	if target == nil || target.Error != "" {
		t.Fatalf("Parse failed: %+v", target)
	}
	bm := newBridges(func() time.Duration { return 2 * time.Second })
	b, _, err := bm.getBridge(target, false)
	if err != nil {
		t.Fatal(err)
	}
	defer b.close()

	clientConn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", b.port), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close()
	_ = clientConn.SetDeadline(time.Now().Add(2 * time.Second))

	if _, err := clientConn.Write([]byte("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(clientConn)
	statusLine, err := br.ReadString('\n')
	if err != nil || !strings.Contains(statusLine, "200") {
		t.Fatalf("CONNECT status = %q err=%v", statusLine, err)
	}
	for {
		line, lErr := br.ReadString('\n')
		if lErr != nil {
			t.Fatalf("reading CONNECT headers: %v", lErr)
		}
		if line == "\r\n" {
			break
		}
	}
	payload := make([]byte, len("EARLY-TUNNEL-PAYLOAD"))
	if _, err := io.ReadFull(br, payload); err != nil {
		t.Fatalf("reading early tunneled bytes: %v", err)
	}
	if string(payload) != "EARLY-TUNNEL-PAYLOAD" {
		t.Fatalf("early payload = %q, want EARLY-TUNNEL-PAYLOAD", string(payload))
	}
}

func TestBridgeHTTPConnectSendsAuthWithPasswordOnly(t *testing.T) {
	var gotAuth string
	upstreamLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstreamLn.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, aErr := upstreamLn.Accept()
		if aErr != nil {
			return
		}
		defer conn.Close()
		req, rErr := http.ReadRequest(bufio.NewReader(conn))
		if rErr == nil {
			gotAuth = req.Header.Get("Proxy-Authorization")
		}
		_, _ = conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	}()

	host, portStr, _ := net.SplitHostPort(upstreamLn.Addr().String())
	portNum, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	target := &Parsed{Scheme: "http", Host: host, Port: portNum, User: "", Pass: "secret-key"}
	bm := newBridges(func() time.Duration { return 2 * time.Second })
	b, _, err := bm.getBridge(target, false)
	if err != nil {
		t.Fatal(err)
	}
	defer b.close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", b.port))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.Write([]byte("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"))
	buf := make([]byte, 64)
	_, _ = c.Read(buf)
	_ = c.Close()
	<-done

	if gotAuth != basicAuth(target) {
		t.Fatalf("Proxy-Authorization = %q, want %q", gotAuth, basicAuth(target))
	}
}
