package muse

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseProxy(t *testing.T) {
	cases := []struct {
		raw      string
		wantType ProxyType
		wantProv string
		wantHost string
		wantPort int
		wantUser string
		wantPass string
		wantKey  string
	}{
		{
			raw:      "1.2.3.4:8080",
			wantType: ProxyTypeStatic, wantProv: "static", wantHost: "1.2.3.4", wantPort: 8080,
		},
		{
			raw:      "1.2.3.4:8080:myuser:mypass",
			wantType: ProxyTypeStatic, wantProv: "static", wantHost: "1.2.3.4", wantPort: 8080, wantUser: "myuser", wantPass: "mypass",
		},
		{
			raw:      "myuser:mypass@1.2.3.4:8080",
			wantType: ProxyTypeStatic, wantProv: "static", wantHost: "1.2.3.4", wantPort: 8080, wantUser: "myuser", wantPass: "mypass",
		},
		{
			raw:      "socks5://alice:secret@10.0.0.1:1080",
			wantType: ProxyTypeStatic, wantProv: "static", wantHost: "10.0.0.1", wantPort: 1080, wantUser: "alice", wantPass: "secret",
		},
		{
			raw:      "proxyvn: test-key-123",
			wantType: ProxyTypeRotating, wantProv: "proxyvn", wantKey: "test-key-123",
		},
		{
			raw:      "tmproxy: tm-key-456",
			wantType: ProxyTypeRotating, wantProv: "tmproxy", wantKey: "tm-key-456",
		},
		{
			raw:      "shoplike: sl-key-789",
			wantType: ProxyTypeRotating, wantProv: "shoplike", wantKey: "shoplike: sl-key-789",
		},
		{
			raw:      "https://proxyxoay.shop/api/get.php?key=xyz",
			wantType: ProxyTypeRotating, wantProv: "proxyvn",
		},
	}

	for _, tc := range cases {
		p := ParseProxy(tc.raw)
		if p == nil {
			t.Fatalf("ParseProxy(%q) = nil", tc.raw)
		}
		if p.Type != tc.wantType {
			t.Errorf("ParseProxy(%q).Type = %v, want %v", tc.raw, p.Type, tc.wantType)
		}
		if tc.wantProv != "" && p.Provider != tc.wantProv {
			t.Errorf("ParseProxy(%q).Provider = %v, want %v", tc.raw, p.Provider, tc.wantProv)
		}
		if tc.wantHost != "" && p.Host != tc.wantHost {
			t.Errorf("ParseProxy(%q).Host = %v, want %v", tc.raw, p.Host, tc.wantHost)
		}
		if tc.wantPort != 0 && p.Port != tc.wantPort {
			t.Errorf("ParseProxy(%q).Port = %v, want %v", tc.raw, p.Port, tc.wantPort)
		}
		if tc.wantUser != "" && p.User != tc.wantUser {
			t.Errorf("ParseProxy(%q).User = %v, want %v", tc.raw, p.User, tc.wantUser)
		}
		if tc.wantPass != "" && p.Pass != tc.wantPass {
			t.Errorf("ParseProxy(%q).Pass = %v, want %v", tc.raw, p.Pass, tc.wantPass)
		}
	}
}

func TestPickProxy(t *testing.T) {
	now := time.Now()
	dead := make(map[string]time.Time)

	// 1. none
	acc := storedAccount{MuseAccount: MuseAccount{ID: "1", Name: "Acc1", Proxy: "none"}}
	p, line, err := pickProxy(acc, nil, "1.2.3.4:8080", dead, now)
	if err != nil || p != nil || line != "" {
		t.Fatalf("expected nil for none proxy, got p=%v, line=%v, err=%v", p, line, err)
	}

	// 2. explicit line
	acc = storedAccount{MuseAccount: MuseAccount{ID: "1", Name: "Acc1", Proxy: "5.6.7.8:8080"}}
	p, line, err = pickProxy(acc, nil, "1.2.3.4:8080", dead, now)
	if err != nil || p == nil || p.Host != "5.6.7.8" {
		t.Fatalf("expected explicit proxy, got p=%v, err=%v", p, err)
	}

	// 3. auto-select from pool
	acc = storedAccount{MuseAccount: MuseAccount{ID: "1", Name: "Acc1"}}
	p, line, err = pickProxy(acc, nil, "1.2.3.4:8080\n2.3.4.5:8080", dead, now)
	if err != nil || p == nil || line != "1.2.3.4:8080" {
		t.Fatalf("expected auto-select 1.2.3.4:8080, got p=%v, line=%v, err=%v", p, line, err)
	}

	// 4. sticky auto-proxy
	acc.ProxyAuto = "2.3.4.5:8080"
	p, line, err = pickProxy(acc, nil, "1.2.3.4:8080\n2.3.4.5:8080", dead, now)
	if err != nil || p == nil || line != "2.3.4.5:8080" {
		t.Fatalf("expected sticky 2.3.4.5:8080, got line=%v", line)
	}

	// 5. all dead error
	dead["1.2.3.4:8080"] = now.Add(10 * time.Minute)
	dead["2.3.4.5:8080"] = now.Add(10 * time.Minute)
	acc = storedAccount{MuseAccount: MuseAccount{ID: "1", Name: "Acc1"}}
	_, _, err = pickProxy(acc, nil, "1.2.3.4:8080\n2.3.4.5:8080", dead, now)
	if err == nil {
		t.Fatalf("expected error when all proxies are dead")
	}
}

func TestProxyStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxies.json")
	store := newProxyStore(path)

	if text, err := store.GetText(); err != nil || text != "" {
		t.Fatalf("expected empty text initially, got %q", text)
	}

	input := "1.2.3.4:8080\nproxyvn:secret_key\n"
	if err := store.SaveText(input); err != nil {
		t.Fatalf("SaveText failed: %v", err)
	}

	store2 := newProxyStore(path)
	if got, err := store2.GetText(); err != nil || got != input {
		t.Fatalf("GetText = %q, want %q", got, input)
	}

	// Verify file is not plain text
	raw, _ := os.ReadFile(path)
	if string(raw) == input {
		t.Fatalf("proxies.json should be encrypted, but is plain text")
	}
}

func TestBridgeManager(t *testing.T) {
	bm := newBridgeManager()
	p := &ParsedProxy{
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
