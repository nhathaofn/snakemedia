package session

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/config"
	"SnakeMedia/internal/providers/flow/chrome"
	"github.com/google/uuid"
)

// seedFlowAccount stores an account the rotation accepts: verified credits and
// unexpired core Google session cookies.
func seedFlowAccount(t *testing.T, s *Service, email string, verified bool) AccountProfile {
	t.Helper()
	future := float64(time.Now().Add(24 * time.Hour).Unix())
	session := testSession(email)
	session.CreditsVerified = verified
	session.Credentials.Cookies = []chrome.CookieItem{
		{Name: "SID", Value: "sid", Domain: ".google.com", Path: "/", Expires: future, Secure: true},
		{Name: "HSID", Value: "hsid", Domain: ".google.com", Path: "/", Expires: future, Secure: true},
		{Name: "SSID", Value: "ssid", Domain: ".google.com", Path: "/", Expires: future, Secure: true},
	}
	acc, err := s.persistSession(context.Background(), filepath.Join(s.paths.DataDir, "profiles", uuid.NewString()), session, nil)
	if err != nil {
		t.Fatal(err)
	}
	return acc
}

// testFlowService never reaches Google: every live seam fails unless a test
// replaces it, so a unit test cannot silently depend on the network.

func TestUpdateCredentialsKeepsNewerSession(t *testing.T) {
	accounts := testAccountService(t)
	acc := seedFlowAccount(t, accounts, "first@example.invalid", true)
	stored, err := accounts.findAccount(acc.ID)
	if err != nil {
		t.Fatal(err)
	}

	rotated := stored.Credentials
	rotated.Cookies = append(rotated.Cookies, chrome.CookieItem{Name: "rotated", Value: "1", Domain: ".google.com", Path: "/"})
	if err := accounts.UpdateCredentials(acc.ID, rotated); err != nil {
		t.Fatal(err)
	}
	if got, _ := accounts.findAccount(acc.ID); len(got.Credentials.Cookies) != len(rotated.Cookies) || got.CookieCount != len(rotated.Cookies) {
		t.Fatalf("rotated cookies not persisted: %+v", got.Credentials.Cookies)
	}

	stale := stored.Credentials
	stale.AtToken = "older-session"
	stale.Cookies = nil
	if err := accounts.UpdateCredentials(acc.ID, stale); err != nil {
		t.Fatal(err)
	}
	if got, _ := accounts.findAccount(acc.ID); len(got.Credentials.Cookies) != len(rotated.Cookies) {
		t.Fatal("an older session snapshot overwrote newer credentials")
	}
}

func TestVisibleBrowserTakesOverIdleHeadlessProfile(t *testing.T) {
	accounts := testAccountService(t)
	acc := seedFlowAccount(t, accounts, "first@example.invalid", true)
	stored, err := accounts.findAccount(acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	var launches []bool
	accounts.start = func(_ context.Context, opts chrome.LaunchOptions, _ config.AccountConfig) (*chrome.Browser, error) {
		launches = append(launches, opts.Headless)
		// Port 1 refuses connections, so closing this fake browser is immediate.
		return &chrome.Browser{Port: 1, ProfileDir: opts.ProfileDir, Headless: opts.Headless}, nil
	}

	if _, err := accounts.EnsureAccountHeadlessBrowser(context.Background(), stored); err != nil {
		t.Fatal(err)
	}
	release := accounts.AcquireProfile(stored.ProfileDir)
	if res := accounts.OpenAccountBrowser(context.Background(), acc.ID); res.Success || !strings.Contains(res.Error, "tạo ảnh") {
		t.Fatalf("visible window replaced a browser still generating: %+v", res)
	}
	release()

	if res := accounts.OpenAccountBrowser(context.Background(), acc.ID); !res.Success {
		t.Fatalf("idle headless profile blocked the visible window: %s", res.Error)
	}
	if len(launches) != 2 || !launches[0] || launches[1] {
		t.Fatalf("expected headless then visible launch, got %v", launches)
	}
}

func TestRefreshClosesOnlyItsOwnIdleHeadlessBrowser(t *testing.T) {
	accounts := testAccountService(t)
	acc := seedFlowAccount(t, accounts, "first@example.invalid", true)
	stored, err := accounts.findAccount(acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	accounts.query = func(context.Context, chrome.Credentials, config.AccountConfig) (chrome.FlowCredits, chrome.Credentials, error) {
		return chrome.FlowCredits{}, chrome.Credentials{}, errors.New("direct HTTP unavailable")
	}
	accounts.inspect = func(context.Context, int, config.AccountConfig) (*chrome.FlowSessionDetails, error) {
		return testSession(acc.Email), nil
	}
	accounts.start = func(_ context.Context, opts chrome.LaunchOptions, _ config.AccountConfig) (*chrome.Browser, error) {
		return &chrome.Browser{Port: 1, ProfileDir: opts.ProfileDir, Headless: opts.Headless}, nil
	}
	profile, err := accounts.profilePath(stored.ProfileDir)
	if err != nil {
		t.Fatal(err)
	}
	refresh := func() {
		t.Helper()
		accounts.mu.Lock()
		delete(accounts.nextRefresh, acc.ID)
		accounts.mu.Unlock()
		if res := accounts.RefreshAccount(context.Background(), acc.ID); !res.Success {
			t.Fatalf("refresh failed: %s", res.Error)
		}
	}
	running := func() bool {
		accounts.mu.Lock()
		defer accounts.mu.Unlock()
		return accounts.browsers[profile] != nil
	}

	refresh()
	if running() {
		t.Fatal("refresh left its own idle headless browser running")
	}

	release := accounts.AcquireProfile(stored.ProfileDir)
	refresh()
	if !running() {
		t.Fatal("refresh closed a headless browser an image request depends on")
	}
	release()

	refresh()
	if !running() {
		t.Fatal("refresh closed a headless browser it did not start")
	}
}
