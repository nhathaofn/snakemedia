package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	cases := []struct {
		raw      string
		wantType Type
		wantProv string
		wantHost string
		wantPort int
		wantUser string
		wantPass string
		wantKey  string
	}{
		{
			raw:      "1.2.3.4:8080",
			wantType: TypeStatic, wantProv: "static", wantHost: "1.2.3.4", wantPort: 8080,
		},
		{
			raw:      "1.2.3.4:8080:myuser:mypass",
			wantType: TypeStatic, wantProv: "static", wantHost: "1.2.3.4", wantPort: 8080, wantUser: "myuser", wantPass: "mypass",
		},
		{
			raw:      "myuser:mypass@1.2.3.4:8080",
			wantType: TypeStatic, wantProv: "static", wantHost: "1.2.3.4", wantPort: 8080, wantUser: "myuser", wantPass: "mypass",
		},
		{
			raw:      "socks5://alice:secret@10.0.0.1:1080",
			wantType: TypeStatic, wantProv: "static", wantHost: "10.0.0.1", wantPort: 1080, wantUser: "alice", wantPass: "secret",
		},
		{
			raw:      "proxyvn: test-key-123",
			wantType: TypeRotating, wantProv: "proxyvn", wantKey: "test-key-123",
		},
		{
			raw:      "tmproxy: tm-key-456",
			wantType: TypeRotating, wantProv: "tmproxy", wantKey: "tm-key-456",
		},
		{
			raw:      "shoplike: sl-key-789",
			wantType: TypeRotating, wantProv: "shoplike", wantKey: "shoplike: sl-key-789",
		},
		{
			raw:      "https://proxyxoay.shop/api/get.php?key=xyz",
			wantType: TypeRotating, wantProv: "proxyvn",
		},
	}

	for _, tc := range cases {
		p := Parse(tc.raw)
		if p == nil {
			t.Fatalf("Parse(%q) = nil", tc.raw)
		}
		if p.Type != tc.wantType {
			t.Errorf("Parse(%q).Type = %v, want %v", tc.raw, p.Type, tc.wantType)
		}
		if tc.wantProv != "" && p.Provider != tc.wantProv {
			t.Errorf("Parse(%q).Provider = %v, want %v", tc.raw, p.Provider, tc.wantProv)
		}
		if tc.wantHost != "" && p.Host != tc.wantHost {
			t.Errorf("Parse(%q).Host = %v, want %v", tc.raw, p.Host, tc.wantHost)
		}
		if tc.wantPort != 0 && p.Port != tc.wantPort {
			t.Errorf("Parse(%q).Port = %v, want %v", tc.raw, p.Port, tc.wantPort)
		}
		if tc.wantUser != "" && p.User != tc.wantUser {
			t.Errorf("Parse(%q).User = %v, want %v", tc.raw, p.User, tc.wantUser)
		}
		if tc.wantPass != "" && p.Pass != tc.wantPass {
			t.Errorf("Parse(%q).Pass = %v, want %v", tc.raw, p.Pass, tc.wantPass)
		}
	}
}

func TestPick(t *testing.T) {
	now := time.Now()
	dead := make(map[string]time.Time)

	a := Assignment{Account: "1", Name: "Acc1", Proxy: None}
	if p, line, err := pick(a, nil, "1.2.3.4:8080", dead, now); err != nil || p != nil || line != "" {
		t.Fatalf("none must go direct, got p=%v, line=%v, err=%v", p, line, err)
	}

	a = Assignment{Account: "1", Name: "Acc1", Proxy: "5.6.7.8:8080"}
	if p, _, err := pick(a, nil, "1.2.3.4:8080", dead, now); err != nil || p == nil || p.Host != "5.6.7.8" {
		t.Fatalf("expected explicit proxy, got p=%v, err=%v", p, err)
	}

	a = Assignment{Account: "1", Name: "Acc1"}
	if p, line, err := pick(a, nil, "1.2.3.4:8080\n2.3.4.5:8080", dead, now); err != nil || p == nil || line != "1.2.3.4:8080" {
		t.Fatalf("expected auto-select 1.2.3.4:8080, got p=%v, line=%v, err=%v", p, line, err)
	}

	// The least shared line wins.
	others := []Assignment{{Account: "2", Auto: "1.2.3.4:8080"}}
	if _, line, _ := pick(a, others, "1.2.3.4:8080\n2.3.4.5:8080", dead, now); line != "2.3.4.5:8080" {
		t.Fatalf("expected the unused line, got %v", line)
	}

	a.Auto = "2.3.4.5:8080"
	if p, line, err := pick(a, nil, "1.2.3.4:8080\n2.3.4.5:8080", dead, now); err != nil || p == nil || line != "2.3.4.5:8080" {
		t.Fatalf("expected sticky 2.3.4.5:8080, got line=%v", line)
	}

	dead["1.2.3.4:8080"] = now.Add(10 * time.Minute)
	dead["2.3.4.5:8080"] = now.Add(10 * time.Minute)
	if _, _, err := pick(Assignment{Account: "1", Name: "Acc1"}, nil, "1.2.3.4:8080\n2.3.4.5:8080", dead, now); err == nil {
		t.Fatalf("expected error when all proxies are dead")
	}
}

// An account on the proxy list is never sent out directly while the list is
// empty, whether or not it was pinned before; only None means direct.
func TestEmptiedListDoesNotFallBackToRealIP(t *testing.T) {
	for _, a := range []Assignment{{Account: "a", Name: "A", Auto: "1.2.3.4:80"}, {Account: "b", Name: "B"}} {
		if p, _, err := pick(a, nil, "", nil, time.Now()); err == nil || p != nil {
			t.Fatalf("%s: got proxy %v, err %v; want an error", a.Name, p, err)
		}
	}
	a := Assignment{Account: "a", Name: "A"}
	a.Auto, a.Proxy = "", None
	if p, _, err := pick(a, nil, "", nil, time.Now()); err != nil || p != nil {
		t.Fatalf("\"none\" must go direct: %v %v", p, err)
	}
}

// Provider errors and bodies never carry the key, and a bad explicit proxy
// line is reported without its password.
func TestSecretsAreRedacted(t *testing.T) {
	msg := transportMessage(&url.Error{Op: "Get", URL: "https://proxyxoay.shop/api/get.php?key=SECRET", Err: errors.New("i/o timeout")})
	if strings.Contains(msg, "SECRET") {
		t.Fatalf("key leaked: %s", msg)
	}
	if got := redactSecret("bad key SECRET "+strings.Repeat("x", 500), "SECRET"); strings.Contains(got, "SECRET") || len([]rune(got)) > maxProviderMessage+1 {
		t.Fatalf("redactSecret = %q", got)
	}
	if got := Mask("1.2.3.4:80:user:hunter2"); strings.Contains(got, "hunter2") || strings.Contains(got, "user") {
		t.Fatalf("Mask = %q", got)
	}
	a := Assignment{Name: "A", Proxy: "1.2.3.4:notaport:user:hunter2"}
	if _, _, err := pick(a, nil, "", nil, time.Now()); err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("err = %v", err)
	}
}

// A signed-in account waits for its pinned line instead of moving to another
// IP while that line is dead; it moves only once the line leaves the list.
func TestKeptPinIsNotMovedWhileDead(t *testing.T) {
	now := time.Now()
	dead := map[string]time.Time{"1.2.3.4:8080": now.Add(time.Minute)}
	a := Assignment{Account: "a", Name: "A", Auto: "1.2.3.4:8080", Keep: true}
	if p, _, err := pick(a, nil, "1.2.3.4:8080\n2.3.4.5:8080", dead, now); err == nil || p != nil {
		t.Fatalf("kept pin moved: %v %v", p, err)
	}
	if _, line, err := pick(a, nil, "2.3.4.5:8080", dead, now); err != nil || line != "2.3.4.5:8080" {
		t.Fatalf("a removed pin must be replaced: %v %v", line, err)
	}
	a.Keep = false
	if _, line, err := pick(a, nil, "1.2.3.4:8080\n2.3.4.5:8080", dead, now); err != nil || line != "2.3.4.5:8080" {
		t.Fatalf("an unkept pin moves to a live line: %v %v", line, err)
	}
}

func TestDeadSetIsSafeUnderConcurrency(t *testing.T) {
	m := NewManager(filepath.Join(t.TempDir(), "proxies.dat"))
	now := time.Now()
	m.now = func() time.Time { return now }
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for k := 0; k < 500; k++ {
				m.MarkDead("1.2.3.4:80")
			}
		}()
		go func() {
			defer wg.Done()
			for k := 0; k < 500; k++ {
				if dead := m.deadSet(); len(dead) > 1 {
					t.Error("unexpected entries")
				}
			}
		}()
	}
	wg.Wait()
	dead := m.deadSet()
	dead["other"] = now.Add(time.Hour)
	if _, leaked := m.deadSet()["other"]; leaked {
		t.Fatal("deadSet must return a copy")
	}
	m.now = func() time.Time { return now.Add(11 * time.Minute) }
	if len(m.deadSet()) != 0 {
		t.Fatal("expired marks must be dropped")
	}
}

// An unreadable list blocks the route instead of going direct.
func TestCorruptStoreBlocksRoute(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxies.dat")
	if err := os.WriteFile(path, []byte("not a proxy store"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager(path)
	if _, err := m.Route(context.Background(), Assignment{Account: "a", Name: "A"}, nil); err == nil {
		t.Fatal("Route went direct with an unreadable proxy store")
	}
	if _, err := m.Text(); err == nil {
		t.Fatal("Text hid the store error")
	}
}

// Every account browser blocks WebRTC from bypassing the proxy, direct too.
func TestDirectRouteBlocksWebRTCLeak(t *testing.T) {
	m := NewManager(filepath.Join(t.TempDir(), "proxies.dat"))
	route, err := m.Route(context.Background(), Assignment{Account: "a", Name: "A", Proxy: None}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if route.Key != DirectRoute || route.URL != nil || !strings.Contains(strings.Join(route.Args, " "), "disable_non_proxied_udp") {
		t.Fatalf("route = %+v", route)
	}
}

// A static proxy is reached through a local bridge that Chrome and Go
// clients share.
func TestStaticRouteUsesLocalBridge(t *testing.T) {
	m := NewManager(filepath.Join(t.TempDir(), "proxies.dat"))
	t.Cleanup(m.Close)
	route, err := m.Route(context.Background(), Assignment{Account: "a", Name: "A", Proxy: "1.2.3.4:8080"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if route.Key != "1.2.3.4:8080" || route.IP != "1.2.3.4" || route.URL == nil || route.URL.Hostname() != "127.0.0.1" {
		t.Fatalf("route = %+v", route)
	}
	if route.Args[0] != "--proxy-server="+route.URL.String() {
		t.Fatalf("Chrome flags %v do not use the bridge %v", route.Args, route.URL)
	}
}

func TestProxyStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxies.json")
	store := NewStore(path)

	if text, err := store.GetText(); err != nil || text != "" {
		t.Fatalf("expected empty text initially, got %q", text)
	}

	input := "1.2.3.4:8080\nproxyvn:secret_key\n"
	if err := store.SaveText(input); err != nil {
		t.Fatalf("SaveText failed: %v", err)
	}

	store2 := NewStore(path)
	if got, err := store2.GetText(); err != nil || got != input {
		t.Fatalf("GetText = %q, want %q", got, input)
	}

	// Verify file is not plain text
	raw, _ := os.ReadFile(path)
	if string(raw) == input {
		t.Fatalf("proxies.json should be encrypted, but is plain text")
	}

	// The list can be emptied and read back empty.
	if err := store2.SaveText(""); err != nil {
		t.Fatalf("saving an empty list failed: %v", err)
	}
	if got, err := NewStore(path).GetText(); err != nil || got != "" {
		t.Fatalf("emptied list read back as %q, %v", got, err)
	}
}

func TestBridgeManager(t *testing.T) {
	bm := newBridges(func() time.Duration { return 2 * time.Second })
	p := &Parsed{
		Scheme: "http",
		Host:   "127.0.0.1",
		Port:   9999,
	}

	port, err := bm.GetBridge(p)
	if err != nil {
		t.Fatalf("GetBridge failed: %v", err)
	}
	if port <= 0 {
		t.Fatalf("expected valid port, got %d", port)
	}

	// Check that port is listening
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	if err != nil {
		t.Fatalf("could not connect to local bridge on port %d: %v", port, err)
	}
	_ = conn.Close()

	// Same target should return same port
	port2, err := bm.GetBridge(p)
	if err != nil || port2 != port {
		t.Fatalf("expected cached port %d, got %d (err: %v)", port, port2, err)
	}
}

// A https:// proxy line is refused rather than used as plain http.
func TestHTTPSProxyLineIsRefused(t *testing.T) {
	if p := Parse("https://user:pw@1.2.3.4:443"); p == nil || p.Error == "" {
		t.Fatalf("https proxy accepted: %+v", p)
	}
}

// fakeUpstream is an HTTP proxy that answers CONNECT with status and records
// the first request line of plain requests.
func fakeUpstream(t *testing.T, status int) (*Parsed, <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	lines := make(chan string, 8)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				buf := make([]byte, 4096)
				n, _ := c.Read(buf)
				head := string(buf[:n])
				lines <- strings.SplitN(head, "\r\n", 2)[0] + "|" + head
				if strings.HasPrefix(head, "CONNECT") {
					fmt.Fprintf(c, "HTTP/1.1 %d X\r\n\r\n", status)
					return
				}
				fmt.Fprint(c, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
			}()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	return &Parsed{Raw: fmt.Sprintf("127.0.0.1:%d:u:pw", port), Type: TypeStatic, Scheme: "http", Host: "127.0.0.1", Port: port, User: "u", Pass: "pw"}, lines
}

// Plain HTTP goes to an HTTP upstream in proxy form with its credentials.
func TestBridgeForwardsPlainHTTPInProxyForm(t *testing.T) {
	up, lines := fakeUpstream(t, 200)
	bm := newBridges(func() time.Duration { return 2 * time.Second })
	t.Cleanup(bm.Close)
	port, err := bm.GetBridge(up)
	if err != nil {
		t.Fatal(err)
	}
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fmt.Fprint(c, "GET http://example.test/a HTTP/1.1\r\nHost: example.test\r\n\r\n")
	select {
	case got := <-lines:
		if !strings.HasPrefix(got, "GET http://example.test/a HTTP/1.1|") || !strings.Contains(got, "Proxy-Authorization: Basic ") || !strings.Contains(got, "Connection: close") {
			t.Fatalf("upstream got %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("upstream never got the request")
	}
}

// A client that connects and sends nothing is dropped after the timeout.
func TestBridgeDropsSilentClients(t *testing.T) {
	up, _ := fakeUpstream(t, 200)
	bm := newBridges(func() time.Duration { return 200 * time.Millisecond })
	t.Cleanup(bm.Close)
	port, _ := bm.GetBridge(up)
	c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := c.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected the bridge to close a silent connection")
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("bridge kept a silent connection open")
	}
}

// Bridges no route uses are closed once their grace has passed.
func TestBridgePrune(t *testing.T) {
	now := time.Now()
	bm := newBridges(func() time.Duration { return time.Second })
	bm.now = func() time.Time { return now }
	t.Cleanup(bm.Close)
	kept, _ := bm.GetBridge(&Parsed{Scheme: "http", Host: "127.0.0.1", Port: 1})
	dropped, _ := bm.GetBridge(&Parsed{Scheme: "http", Host: "127.0.0.1", Port: 2})
	if n := bm.Prune(map[int]bool{kept: true}); n != 0 {
		t.Fatalf("a fresh bridge was pruned (%d)", n)
	}
	bm.now = func() time.Time { return now.Add(bridgeGrace + time.Second) }
	if n := bm.Prune(map[int]bool{kept: true}); n != 1 {
		t.Fatalf("pruned %d bridges, want 1", n)
	}
	if _, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", dropped), time.Second); err == nil {
		t.Fatal("pruned bridge still listens")
	}
	if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", kept), time.Second); err != nil {
		t.Fatal("kept bridge closed")
	} else {
		_ = c.Close()
	}
}

// CheckAll feeds its results into automatic picks and never returns secrets.
func TestCheckAllMarksDeadAndAlive(t *testing.T) {
	live, _ := fakeUpstream(t, 200)
	refused, _ := fakeUpstream(t, 403)
	m := NewManager(filepath.Join(t.TempDir(), "proxies.dat"))
	m.SetTimeout(func() time.Duration { return 2 * time.Second })
	m.MarkDead(live.Raw)
	if err := m.Save(live.Raw + "\n" + refused.Raw); err != nil {
		t.Fatal(err)
	}
	results, err := m.CheckAll(context.Background())
	if err != nil || len(results) != 2 {
		t.Fatalf("CheckAll = %v, %v", results, err)
	}
	for _, r := range results {
		if strings.Contains(r.Raw, "pw") {
			t.Fatalf("result leaks the password: %q", r.Raw)
		}
	}
	dead := m.deadSet()
	if _, ok := dead[live.Raw]; ok {
		t.Fatal("a proxy that answered stays marked dead")
	}
	if _, ok := dead[refused.Raw]; !ok {
		t.Fatal("a failing proxy is not marked dead")
	}
}
