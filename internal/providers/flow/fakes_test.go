package flow

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"SnakeMedia/internal/config"
	"SnakeMedia/internal/media"
	"SnakeMedia/internal/paths"
	"SnakeMedia/internal/providers/flow/chrome"
	"SnakeMedia/internal/providers/flow/session"
)

// fakeAccounts is an in-memory Accounts: no DPAPI store, no Chrome.
type fakeAccounts struct {
	mu         sync.Mutex
	list       []session.StoredAccount
	inUse      map[string]int
	cfgManager *config.Manager
	mediaDir   string
}

func testAccountService(t *testing.T) *fakeAccounts {
	t.Helper()
	p := paths.ForRoot(t.TempDir())
	cfg, err := config.NewManager(p.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeAccounts{inUse: map[string]int{}, cfgManager: cfg, mediaDir: p.DataDir + "/media"}
}

// seedFlowAccount stores an account the rotation accepts when verified: credits
// verified and unexpired core Google session cookies.
func seedFlowAccount(t *testing.T, f *fakeAccounts, email string, verified bool) session.AccountProfile {
	t.Helper()
	future := float64(time.Now().Add(24 * time.Hour).Unix())
	status := session.AccountSignedIn
	if verified {
		status = session.AccountConnected
	}
	acc := session.StoredAccount{
		AccountProfile: session.AccountProfile{
			ID: email, Email: email, Name: email, ProfileDir: "profile-" + email, Status: status,
			Balance: &chrome.FlowCredits{Credits: 58, TierCode: 1, Tier: chrome.TierPro, DailyLimit: 58},
		},
		Credentials: chrome.Credentials{
			RPCs:    chrome.RPCRegistry{Credits: "testCredits", Availability: "testAvailability", Models: RPCGetModels, Projects: RPCGetProjects, CreateProject: RPCCreateProject, ProjectContents: RPCGetProjectContents, Media: RPCGetMedia, EditVideo: RPCEditVideo},
			AtToken: "test-csrf", SessionID: "test-session", BuildLabel: "test-runtime-build",
			Language: "en", UserAgent: "test-runtime-agent", SourcePath: "/",
			Cookies: []chrome.CookieItem{
				{Name: "SID", Value: "sid", Domain: ".google.com", Path: "/", Expires: future, Secure: true},
				{Name: "HSID", Value: "hsid", Domain: ".google.com", Path: "/", Expires: future, Secure: true},
				{Name: "SSID", Value: "ssid", Domain: ".google.com", Path: "/", Expires: future, Secure: true},
			},
		},
	}
	f.mu.Lock()
	f.list = append(f.list, acc)
	f.mu.Unlock()
	return acc.AccountProfile
}

func (f *fakeAccounts) GetConnectedStoredAccounts() ([]session.StoredAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []session.StoredAccount
	for _, acc := range f.list {
		if acc.Status == session.AccountConnected {
			out = append(out, acc)
		}
	}
	return out, nil
}

func (f *fakeAccounts) AcquireProfile(profileDir string) func() {
	f.mu.Lock()
	f.inUse[profileDir]++
	f.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			f.mu.Lock()
			if f.inUse[profileDir]--; f.inUse[profileDir] <= 0 {
				delete(f.inUse, profileDir)
			}
			f.mu.Unlock()
		})
	}
}

func (f *fakeAccounts) EnsureAccountHeadlessBrowser(context.Context, session.StoredAccount) (*chrome.Browser, error) {
	return nil, errors.New("no Chrome in unit tests")
}

func (f *fakeAccounts) SyncSession(_ context.Context, acc session.StoredAccount) (session.StoredAccount, error) {
	return acc, nil
}

func (f *fakeAccounts) UpdateCredentials(id string, c chrome.Credentials) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.list {
		if f.list[i].ID == id {
			f.list[i].Credentials = c
			return nil
		}
	}
	return errors.New("account removed")
}

func (f *fakeAccounts) UpdateBalance(id string, balance chrome.FlowCredits, c chrome.Credentials) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.list {
		if f.list[i].ID != id {
			continue
		}
		if f.list[i].Credentials.AtToken != c.AtToken {
			return nil
		}
		f.list[i].Balance = &balance
		f.list[i].Credentials = c
		f.list[i].LastVerifiedAt = time.Now().UTC().Format(time.RFC3339Nano)
		f.list[i].Status = session.AccountConnected
		return nil
	}
	return errors.New("account removed")
}

func (f *fakeAccounts) findAccount(id string) (session.StoredAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, acc := range f.list {
		if acc.ID == id {
			return acc, nil
		}
	}
	return session.StoredAccount{}, errors.New("account not found")
}

// profilePath mirrors session.Service: the fake keys profiles by their stored path.
func (f *fakeAccounts) profilePath(dir string) (string, error) {
	return dir, nil
}

// testFlowService never reaches Google: every live seam fails unless a test
// replaces it, so a unit test cannot silently depend on the network.
func testFlowService(accounts *fakeAccounts) *Provider {
	s := New(accounts, accounts.cfgManager, media.NewStore(accounts.mediaDir))
	offline := errors.New("live Flow call in a unit test")
	s.refreshLiveCredits = func(context.Context, string) {}
	s.fetchModelsPayload = func(context.Context, session.StoredAccount) (string, error) { return "", offline }
	s.syncSession = func(_ context.Context, acc session.StoredAccount) (session.StoredAccount, error) { return acc, nil }
	s.generateLiveImages = func(context.Context, session.StoredAccount, GenerateImageRequest) ([]GeneratedImageItem, error) {
		return nil, offline
	}
	s.generateLiveVideos = func(context.Context, session.StoredAccount, GenerateVideoRequest, string) ([]GeneratedVideoItem, error) {
		return nil, offline
	}
	s.uploadLiveVideo = func(context.Context, session.StoredAccount, localVideo) (sourceRecord, error) {
		return sourceRecord{}, offline
	}
	s.editLiveVideos = func(context.Context, session.StoredAccount, EditVideoRequest, localVideo, videoFamily) ([]GeneratedVideoItem, error) {
		return nil, offline
	}
	return s
}
