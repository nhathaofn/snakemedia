package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"SnakeMedia/internal/config"
	"SnakeMedia/internal/paths"
	"SnakeMedia/internal/providers/flow/chrome"
	"github.com/google/uuid"
)

func testAccountService(t *testing.T) *Service {
	t.Helper()
	p := paths.ForRoot(t.TempDir())
	cfg, err := config.NewManager(p.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(p, cfg)
	s.validate = func(context.Context, *chrome.Browser, config.AccountConfig) error { return nil }
	return s
}

func testSession(email string) *chrome.FlowSessionDetails {
	return &chrome.FlowSessionDetails{Email: email, GaiaID: email + "-gaia", IsLoggedIn: true, CreditsVerified: true, Credits: chrome.FlowCredits{Credits: 58, TierCode: 1, Tier: chrome.TierPro, DailyLimit: 58}, Credentials: chrome.Credentials{RPCs: chrome.RPCRegistry{Credits: "testCredits", Availability: "testAvailability"}, AtToken: "test-csrf", SessionID: "test-session", BuildLabel: "test-runtime-build", Language: "en", UserAgent: "test-runtime-agent", SourcePath: "/", Cookies: []chrome.CookieItem{{Name: "test-cookie", Value: "test-secret", Domain: "flow.google.com", Path: "/", Secure: true}}}}
}

func seedAccount(t *testing.T, s *Service, email string) AccountProfile {
	t.Helper()
	acc, err := s.persistSession(context.Background(), filepath.Join(s.paths.DataDir, "profiles", uuid.NewString()), testSession(email), nil)
	if err != nil {
		t.Fatal(err)
	}
	return acc
}

func TestAccountZeroBalanceAndPublicView(t *testing.T) {
	s := testAccountService(t)
	acc := seedAccount(t, s, "a@example.invalid")
	zero := testSession(acc.Email)
	zero.Credits.Credits = 0
	if _, err := s.persistSession(context.Background(), acc.ProfileDir, zero, &acc); err != nil {
		t.Fatal(err)
	}
	res := s.GetAccounts(context.Background())
	if !res.Success || len(res.Data) != 1 || res.Data[0].Balance.Credits != 0 {
		t.Fatalf("zero balance not persisted: %+v", res)
	}
	if res.Data[0].Status != AccountConnected {
		t.Fatalf("expected status %s, got %s", AccountConnected, res.Data[0].Status)
	}
	public, _ := json.Marshal(res)
	for _, secret := range []string{"test-secret", "test-csrf", "test-runtime-build", "credentials", "cookieHeader", "atToken"} {
		if bytes.Contains(public, []byte(secret)) {
			t.Fatalf("public response leaked %s", secret)
		}
	}
	data, err := os.ReadFile(s.accountsFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" && bytes.Contains(data, []byte("test-secret")) {
		t.Fatal("credentials not encrypted by DPAPI")
	}
	if r := s.RemoveAccount(context.Background(), acc.ID); !r.Success {
		t.Fatal(r.Error)
	}
	if r := s.GetAccounts(context.Background()); !r.Success || len(r.Data) != 0 {
		t.Fatal("remove did not persist")
	}
}

func TestEnsureAccountHeadlessBrowser(t *testing.T) {
	s := testAccountService(t)
	accProfile := seedAccount(t, s, "headless@example.invalid")
	stored := StoredAccount{AccountProfile: accProfile}

	started := false
	s.start = func(ctx context.Context, opts chrome.LaunchOptions, policy config.AccountConfig) (*chrome.Browser, error) {
		started = true
		if !opts.Headless {
			t.Fatal("expected headless to be true")
		}
		return &chrome.Browser{Port: 9222, ProfileDir: opts.ProfileDir, Headless: opts.Headless}, nil
	}

	b, err := s.EnsureAccountHeadlessBrowser(context.Background(), stored)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !started {
		t.Fatal("expected browser to be started")
	}
	if b == nil || !b.Headless {
		t.Fatal("expected headless browser")
	}

	// second call should reuse
	started = false
	b2, err := s.EnsureAccountHeadlessBrowser(context.Background(), stored)
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if started {
		t.Fatal("expected browser to be reused without starting again")
	}
	if b2 != b {
		t.Fatal("expected same browser instance to be returned")
	}
}

func TestCorruptStoreQuarantinedAndBlocksWrites(t *testing.T) {
	s := testAccountService(t)
	if err := os.WriteFile(s.accountsFilePath(), []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if res := s.GetAccounts(context.Background()); res.Success {
		t.Fatal("corrupt store reported success")
	}
	backups, _ := filepath.Glob(s.accountsFilePath() + ".corrupt-*")
	if len(backups) != 1 {
		t.Fatal("missing corrupt backup")
	}
	if _, err := s.saveAccount(StoredAccount{AccountProfile: AccountProfile{ID: "new", Email: "new@example.invalid"}}, false); err == nil {
		t.Fatal("corrupt store overwritten")
	}
	data, _ := os.ReadFile(backups[0])
	if string(data) != "{broken" {
		t.Fatal("corrupt bytes changed")
	}
}

func TestLegacyStoreMigrationDoesNotTrustOldCredentials(t *testing.T) {
	s := testAccountService(t)
	legacy := `[{"id":"a@example.invalid","email":"a@example.invalid","status":"connected","credits":58,"cookieHeader":"old-secret","atToken":"old-token"}]`
	if err := os.WriteFile(s.accountsFilePath(), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	res := s.GetAccounts(context.Background())
	if !res.Success || len(res.Data) != 1 {
		t.Fatal(res.Error)
	}
	acc, err := s.findAccount(res.Data[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if acc.Status != AccountUnverified || acc.Balance != nil || acc.Credentials.AtToken != "" {
		t.Fatal("legacy data trusted")
	}
	data, _ := os.ReadFile(s.accountsFilePath())
	if !bytes.HasPrefix(data, []byte(accountStoreHeader)) {
		t.Fatal("legacy store not migrated")
	}
}

func TestRefreshRefusesOtherAccountBrowser(t *testing.T) {
	s := testAccountService(t)
	acc := seedAccount(t, s, "a@example.invalid")
	s.browsers[acc.ProfileDir] = &chrome.Browser{ProfileDir: acc.ProfileDir}
	s.query = func(context.Context, chrome.Credentials, config.AccountConfig) (chrome.FlowCredits, chrome.Credentials, error) {
		return chrome.FlowCredits{}, chrome.Credentials{}, errors.New("expired")
	}
	s.inspect = func(context.Context, int, config.AccountConfig) (*chrome.FlowSessionDetails, error) {
		return testSession("b@example.invalid"), nil
	}
	res := s.RefreshAccount(context.Background(), acc.ID)
	if res.Success {
		t.Fatal("accepted wrong account")
	}
	stored, err := s.findAccount(acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.GaiaID != acc.GaiaID || stored.Balance.Credits != 58 || stored.Credentials.AtToken != "test-csrf" {
		t.Fatal("account modified by mismatched identity")
	}
}

func TestRefreshConcurrencyCooldownAndRateLimit(t *testing.T) {
	s := testAccountService(t)
	acc := seedAccount(t, s, "a@example.invalid")
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	s.query = func(context.Context, chrome.Credentials, config.AccountConfig) (chrome.FlowCredits, chrome.Credentials, error) {
		calls.Add(1)
		close(entered)
		<-release
		return chrome.FlowCredits{}, chrome.Credentials{}, &chrome.HTTPError{Status: http.StatusTooManyRequests, RetryAfter: time.Minute}
	}
	done := make(chan struct{})
	go func() { defer close(done); s.RefreshAccount(context.Background(), acc.ID) }()
	<-entered
	if s.RefreshAccount(context.Background(), acc.ID).Success {
		t.Fatal("concurrent refresh accepted")
	}
	if s.RemoveAccount(context.Background(), acc.ID).Success {
		t.Fatal("concurrent removal accepted")
	}
	close(release)
	<-done
	if s.RefreshAccount(context.Background(), acc.ID).Success || calls.Load() != 1 {
		t.Fatal("cooldown did not prevent remote work")
	}
	if time.Until(s.nextRefresh[acc.ID]) < time.Second*50 {
		t.Fatal("Retry-After was not honored")
	}
}

func TestCompletedLoginIsCachedAndSaveFailureIsReported(t *testing.T) {
	for _, broken := range []bool{false, true} {
		t.Run(map[bool]string{false: "cached", true: "save failure"}[broken], func(t *testing.T) {
			s := testAccountService(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			id := uuid.NewString()
			s.login = &loginSession{id: id, ctx: ctx, cancel: cancel, browser: &chrome.Browser{ProfileDir: filepath.Join(s.paths.DataDir, "profiles", uuid.NewString())}}
			var calls int
			s.inspectIdentity = func(context.Context, int, config.AccountConfig) (*chrome.FlowSessionDetails, error) {
				calls++
				return testSession("a@example.invalid"), nil
			}
			if broken {
				if err := os.Mkdir(s.accountsFilePath(), 0700); err != nil {
					t.Fatal(err)
				}
			}
			first := s.CheckLoginStatus(context.Background(), id)
			if broken {
				if first.Success || s.login.completed != nil {
					t.Fatal("save failure claimed successful login")
				}
				return
			}
			second := s.CheckLoginStatus(context.Background(), id)
			if !first.Success || !first.Data.IsLoggedIn || !second.Success || calls != 1 {
				t.Fatal("completed login was queried again")
			}
		})
	}
}

func TestLoginIdentityIsSavedWithoutCreditsRPC(t *testing.T) {
	s := testAccountService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	id := uuid.NewString()
	profileDir := filepath.Join(s.paths.DataDir, "profiles", uuid.NewString())
	s.login = &loginSession{id: id, ctx: ctx, cancel: cancel, browser: &chrome.Browser{ProfileDir: profileDir}}
	s.inspectIdentity = func(context.Context, int, config.AccountConfig) (*chrome.FlowSessionDetails, error) {
		identity := testSession("signed-in@example.invalid")
		identity.CreditsVerified = false
		identity.Credentials.RPCs = chrome.RPCRegistry{}
		return identity, nil
	}
	s.inspect = func(context.Context, int, config.AccountConfig) (*chrome.FlowSessionDetails, error) {
		t.Fatal("login must not wait for the credits RPC")
		return nil, nil
	}
	res := s.CheckLoginStatus(context.Background(), id)
	if !res.Success || !res.Data.IsLoggedIn || res.Data.Profile == nil || res.Data.Profile.Balance != nil || res.Data.Profile.Status != AccountSignedIn {
		t.Fatalf("signed-in identity hidden by missing balance: %+v", res)
	}
	if listed := s.GetAccounts(context.Background()); !listed.Success || len(listed.Data) != 1 {
		t.Fatal("card source not persisted")
	}
	s.browsers[profileDir] = s.login.browser
	s.inspect = func(context.Context, int, config.AccountConfig) (*chrome.FlowSessionDetails, error) {
		details := testSession("signed-in@example.invalid")
		details.CreditsVerified = false
		details.CreditsErr = errors.New("RPC discovery unavailable")
		return details, nil
	}
	if refresh := s.RefreshAccount(context.Background(), res.Data.Profile.ID); refresh.Success {
		t.Fatal("failed enrichment reported as updated balance")
	}
	listed := s.GetAccounts(context.Background())
	if !listed.Success || len(listed.Data) != 1 || listed.Data[0].Balance != nil || listed.Data[0].BalanceError == "" {
		t.Fatal("failed enrichment removed the signed-in card or invented a balance")
	}
}

func TestProfileContainmentAndIdentity(t *testing.T) {
	s := testAccountService(t)
	for _, path := range []string{"", s.paths.DataDir, filepath.Join(s.paths.DataDir, "..", "outside")} {
		if _, err := s.profilePath(path); err == nil {
			t.Fatalf("accepted profile %q", path)
		}
	}
	acc := seedAccount(t, s, "a@example.invalid")
	other := testSession(acc.Email)
	other.GaiaID = "different"
	if _, err := s.persistSession(context.Background(), acc.ProfileDir, other, &acc); err == nil {
		t.Fatal("Gaia mismatch accepted")
	}
}

func TestCanceledLoginCannotLaunchLater(t *testing.T) {
	s := testAccountService(t)
	id := uuid.NewString()
	if res := s.CancelLogin(id); !res.Success {
		t.Fatal(res.Error)
	}
	if res := s.LaunchLoginBrowser(context.Background(), id); res.Success {
		t.Fatal("closed UI launched a late browser")
	}
}

func TestSyncSessionReadsFreshCredentialsFromChrome(t *testing.T) {
	s := testAccountService(t)
	acc := seedAccount(t, s, "sync@example.invalid")
	stored, err := s.findAccount(acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	cfg := s.cfgManager.Get()
	cfg.Accounts.PollIntervalMS = 500
	if _, err := s.cfgManager.Update(cfg); err != nil {
		t.Fatal(err)
	}
	s.start = func(_ context.Context, opts chrome.LaunchOptions, _ config.AccountConfig) (*chrome.Browser, error) {
		return &chrome.Browser{Port: 1, ProfileDir: opts.ProfileDir, Headless: opts.Headless}, nil
	}
	fresh := testSession(acc.Email)
	fresh.Credentials.AtToken = "rotated-csrf"
	fresh.Credentials.RPCs = chrome.RPCRegistry{}
	fresh.Credentials.Cookies = append(fresh.Credentials.Cookies, chrome.CookieItem{Name: "rotated", Value: "1", Domain: ".google.com", Path: "/"})
	s.inspectIdentity = func(context.Context, int, config.AccountConfig) (*chrome.FlowSessionDetails, error) {
		return fresh, nil
	}

	synced, err := s.SyncSession(context.Background(), stored)
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := s.findAccount(acc.ID)
	if synced.Credentials.AtToken != "rotated-csrf" || saved.Credentials.AtToken != "rotated-csrf" || saved.CookieCount != 2 {
		t.Fatalf("fresh session not stored: %+v", saved.Credentials)
	}
	if saved.Credentials.RPCs != stored.Credentials.RPCs || saved.Status != AccountConnected {
		t.Fatalf("sync must keep the discovered RPCs and the account in rotation: %+v %s", saved.Credentials.RPCs, saved.Status)
	}

	other := testSession("someone-else@example.invalid")
	s.inspectIdentity = func(context.Context, int, config.AccountConfig) (*chrome.FlowSessionDetails, error) {
		return other, nil
	}
	if _, err := s.SyncSession(context.Background(), stored); err == nil {
		t.Fatal("a session of another Google account was accepted")
	}

	anonymous := &chrome.FlowSessionDetails{Title: "Flow"}
	s.inspectIdentity = func(context.Context, int, config.AccountConfig) (*chrome.FlowSessionDetails, error) {
		return anonymous, nil
	}
	if _, err := s.SyncSession(context.Background(), stored); !errors.Is(err, ErrSessionSignedOut) {
		t.Fatalf("signed-out profile not reported: %v", err)
	}
	if saved, _ := s.findAccount(acc.ID); saved.Status != AccountExpired {
		t.Fatalf("signed-out account kept in rotation: %s", saved.Status)
	}
}
