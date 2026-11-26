package accounts

import (
	"context"
	"testing"
	"time"

	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/models"
	"SnakeMedia/internal/proxy"
)

func TestOpenFailureCleansRouteAndClosesFreshBrowser(t *testing.T) {
	s, pool, _ := testService(t, Inspection{SignedIn: true})
	rec := seed(t, s, Record{Account: Account{ID: "proxy-fail", Proxy: "1.2.3.4:8080"}})
	t.Cleanup(s.proxies.Close)

	s.ensureTab = func(context.Context, int, config.AccountConfig, browser.Site) error {
		return &browser.NavigateError{URL: "https://example.test/", Text: "net::ERR_PROXY_CONNECTION_FAILED"}
	}
	if _, err := s.Browser(context.Background(), rec.ID); err == nil {
		t.Fatal("expected Browser to fail on proxy error")
	}
	if u, err := s.ProxyURL(rec.ID); err == nil || u != nil {
		t.Fatalf("failed open() leaked route: url=%v err=%v", u, err)
	}
	pool.mu.Lock()
	closed := append([]string(nil), pool.closed...)
	pool.mu.Unlock()
	if len(closed) == 0 || closed[0] != rec.ProfileDir {
		t.Fatalf("failed open() did not close fresh zombie browser: closed=%v", closed)
	}
}

func TestRotateIPRefusesWhenAccountInUse(t *testing.T) {
	s, _, _ := testService(t, Inspection{SignedIn: true})
	seed(t, s, Record{Account: Account{ID: "busy-rot", Proxy: "proxyvn:test-key"}})
	release := s.Acquire("busy-rot")
	defer release()

	res := s.RotateIP(context.Background(), "busy-rot")
	if res.Success || res.Code != models.CodeUnavailable {
		t.Fatalf("RotateIP while account in use = %+v, want CodeUnavailable", res)
	}
}

func TestImportCookiesUsesSharedProxyListAndSkipsAutoCheckWhenEmpty(t *testing.T) {
	rawCookies := `[{"name":"sid","value":"abc","domain":".example.test","path":"/","sameSite":"no_restriction"}]`

	t.Run("empty proxy list assigns proxy.None without auto-launching Chrome", func(t *testing.T) {
		s, pool, _ := testService(t, Inspection{SignedIn: true})
		res := s.ImportCookies(ProviderMuse, "NoProxyAcc", rawCookies)
		if !res.Success {
			t.Fatalf("ImportCookies: %+v", res)
		}
		if res.Data.Proxy != proxy.None {
			t.Fatalf("Proxy = %q, want %q", res.Data.Proxy, proxy.None)
		}
		time.Sleep(40 * time.Millisecond)
		pool.mu.Lock()
		wants := len(pool.wants)
		pool.mu.Unlock()
		if wants != 0 {
			t.Fatalf("empty proxy list must not auto-launch Chrome on real IP; got %d launches", wants)
		}
		rec, _ := s.Record(res.Data.ID)
		if len(rec.Cookies) != 1 || rec.Cookies[0].SameSite != "None" {
			t.Fatalf("expected SameSite no_restriction normalized to None, got %+v", rec.Cookies)
		}
	})

	t.Run("populated proxy list assigns auto shared proxy", func(t *testing.T) {
		s, _, _ := testService(t, Inspection{SignedIn: true})
		if r := s.SaveProxies("1.2.3.4:8080"); !r.Success {
			t.Fatal(r.Error)
		}
		res := s.ImportCookies(ProviderMuse, "ProxiedAcc", rawCookies)
		if !res.Success {
			t.Fatalf("ImportCookies: %+v", res)
		}
		if res.Data.Proxy != "" {
			t.Fatalf("Proxy = %q, want empty (shared proxy list)", res.Data.Proxy)
		}
	})
}

func TestSyncCookiesPersistsSameSiteChangeAndSlidingExpiryExtension(t *testing.T) {
	s, _, _ := testService(t, Inspection{SignedIn: true})
	now := time.Now().UTC()
	initial := []browser.Cookie{{
		Name:     "sid",
		Value:    "tok",
		Domain:   "example.test",
		Path:     "/",
		Expires:  float64(now.Add(1 * time.Hour).Unix()),
		SameSite: "Lax",
	}}
	seed(t, s, Record{Account: Account{ID: "cookie-sync"}, Cookies: initial})

	// SameSite change triggers persistence.
	changedSameSite := []browser.Cookie{{
		Name:     "sid",
		Value:    "tok",
		Domain:   "example.test",
		Path:     "/",
		Expires:  float64(now.Add(1 * time.Hour).Unix()),
		SameSite: "None",
	}}
	s.readCookies = func(context.Context, int, config.AccountConfig, func(string) bool) ([]browser.Cookie, error) {
		return changedSameSite, nil
	}
	s.SyncCookies(context.Background(), "cookie-sync", 1234)
	rec, _ := s.Record("cookie-sync")
	if len(rec.Cookies) != 1 || rec.Cookies[0].SameSite != "None" {
		t.Fatalf("SameSite change was not persisted: %+v", rec.Cookies)
	}

	// Sliding expiry extension (>5m) triggers persistence even with same value.
	extended := []browser.Cookie{{
		Name:     "sid",
		Value:    "tok",
		Domain:   "example.test",
		Path:     "/",
		Expires:  float64(now.Add(24 * time.Hour).Unix()),
		SameSite: "None",
	}}
	s.readCookies = func(context.Context, int, config.AccountConfig, func(string) bool) ([]browser.Cookie, error) {
		return extended, nil
	}
	s.SyncCookies(context.Background(), "cookie-sync", 1234)
	rec, _ = s.Record("cookie-sync")
	if rec.Cookies[0].Expires < float64(now.Add(23*time.Hour).Unix()) {
		t.Fatalf("sliding expiry extension was not persisted: %+v", rec.Cookies)
	}
}

func TestRunCheckSkipsCookieSyncOnIdentityConflictAndWatchAppliesFatalWhenSignedOut(t *testing.T) {
	t.Run("runCheck skips SyncCookies when identity belongs to another account", func(t *testing.T) {
		s, _, _ := testService(t, Inspection{SignedIn: true, Identity: "taken@example.test"})
		seed(t, s, Record{Account: Account{ID: "owner", Identity: "taken@example.test", Status: StatusSignedIn}})
		seed(t, s, Record{Account: Account{ID: "dup", Status: StatusUnverified}})

		synced := 0
		s.readCookies = func(context.Context, int, config.AccountConfig, func(string) bool) ([]browser.Cookie, error) {
			synced++
			return []browser.Cookie{{Name: "sid", Value: "stolen", Domain: "example.test", Path: "/"}}, nil
		}
		res := s.Check(context.Background(), "dup")
		if res.Success {
			t.Fatalf("expected conflict failure, got %+v", res)
		}
		if synced != 0 {
			t.Fatalf("SyncCookies ran %d times on identity conflict, want 0", synced)
		}
	})

	t.Run("watch settles Fatal immediately even when SignedIn is false", func(t *testing.T) {
		s, _, base := testService(t, Inspection{SignedIn: false, Fatal: "Tài khoản bị khóa", DisableAccount: true})
		seed(t, s, Record{Account: Account{ID: "banned-watch", Status: StatusSigningIn, Enabled: true}})
		s.watch(&browser.Browser{Port: 4444}, "banned-watch", base)

		rec, _ := s.Record("banned-watch")
		if rec.Status != StatusError || rec.Enabled || rec.Message != "Tài khoản bị khóa" {
			t.Fatalf("watch did not settle Fatal on SignedIn=false: %+v", rec.Account)
		}
	})
}
