package muse

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/media"
	"github.com/google/uuid"
)

// fakePage scripts a Muse tab. Fields are read under mu.
type fakePage struct {
	mu          sync.Mutex
	loggedIn    bool
	sendErr     error
	sendRes     SendResult
	sends       []string
	attachments [][]Attachment
	history     []AssistantEvent
	// onSend runs inside Send (without the lock) before it answers.
	onSend    func()
	statuses  []string // returned in order; the last one repeats
	statusErr error
	events    []AssistantEvent
	files     map[string][]byte
	quota     *MuseQuota
	caps      *MuseCapabilities
	cancelled []string
	cancelErr error
	// http, noChat, homeFixes and homes script Login and Home.
	http      int
	noChat    bool
	homeFixes bool
	homes     int
	capsReads int
}

// Login answers like page.js: a signed-out page gets HTTP 401 unless http
// says otherwise; noChat is a signed-in tab away from the chat.
func (f *fakePage) Login(context.Context) (LoginState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	http := f.http
	if http == 0 {
		http = 200
		if !f.loggedIn {
			http = 401
		}
	}
	chat := !f.noChat
	return LoginState{LoggedIn: f.loggedIn && chat && http == 200, Chat: chat, HTTP: http}, nil
}

// Home counts navigations; it brings the chat back when homeFixes is set.
func (f *fakePage) Home(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.homes++
	if f.homeFixes {
		f.noChat = false
	}
	return nil
}

func (f *fakePage) Send(_ context.Context, text string, atts []Attachment) (SendResult, error) {
	f.mu.Lock()
	hook := f.onSend
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends = append(f.sends, text)
	f.attachments = append(f.attachments, atts)
	return f.sendRes, f.sendErr
}

func (f *fakePage) Status(context.Context, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.statusErr != nil {
		return "", f.statusErr
	}
	if len(f.statuses) == 0 {
		return "completed", nil
	}
	s := f.statuses[0]
	if len(f.statuses) > 1 {
		f.statuses = f.statuses[1:]
	}
	return s, nil
}

// Result returns the chat: history before any send, then events.
func (f *fakePage) Result(context.Context, string) ([]AssistantEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sends) == 0 {
		return f.history, nil
	}
	return f.events, nil
}

func (f *fakePage) Download(_ context.Context, path string, sink io.Writer) (int64, error) {
	f.mu.Lock()
	data, ok := f.files[path]
	f.mu.Unlock()
	if !ok {
		return 0, errors.New("missing file")
	}
	n, err := sink.Write(data)
	return int64(n), err
}

func (f *fakePage) Cancel(_ context.Context, sid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelled = append(f.cancelled, sid)
	return f.cancelErr
}

func (f *fakePage) Quota(context.Context) (*MuseQuota, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.quota, nil
}

func (f *fakePage) Capabilities(context.Context) (*MuseCapabilities, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.capsReads++
	return f.caps, nil
}

// testCaps are limits like the ones Muse's composer has.
func testCaps() *MuseCapabilities {
	return &MuseCapabilities{
		MaxFileBytes: 1 << 20, MaxFiles: 2, CheckedAt: "2026-10-26T00:00:00Z",
		ImageTypes: []MuseFileType{{Mime: "image/png", Extensions: []string{".png"}}, {Mime: "image/jpeg", Extensions: []string{".jpg", ".jpeg"}}},
		Models:     []MuseModel{{ID: "auto/auto", Name: "Auto"}},
	}
}

// testProvider returns a provider over a temp dir whose pages are page. Its
// clock is fake: sleeping advances it instantly.
func testProvider(t *testing.T, page Page) *Provider {
	t.Helper()
	dir := t.TempDir()
	cfg, err := config.NewManager(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	accts := accounts.New(filepath.Join(dir, "accounts"), cfg, NewSite(cfg))
	p := New(dir, filepath.Join(dir, "out"), cfg, media.NewStore(filepath.Join(dir, "media")), accts)
	p.pageFor = func(context.Context, accounts.Record) (Page, error) { return page, nil }
	var clockMu sync.Mutex
	clock := time.Now()
	p.now = func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return clock
	}
	p.sleep = func(ctx context.Context, d time.Duration) error {
		clockMu.Lock()
		clock = clock.Add(d)
		clockMu.Unlock()
		return ctx.Err()
	}
	p.gap = func(config.MuseConfig) time.Duration { return 0 }
	return p
}

func addSignedIn(t *testing.T, p *Provider, name string) accounts.Record {
	t.Helper()
	acc := accounts.Record{Account: accounts.Account{
		ID: uuid.NewString(), Provider: accounts.ProviderMuse, Name: name,
		Source: accounts.SourceBrowser, Status: accounts.StatusSignedIn, Enabled: true,
	}}
	if err := p.accounts.Seed(acc); err != nil {
		t.Fatal(err)
	}
	return acc
}

func submit(t *testing.T, p *Provider, kind MuseJobKind, prompts ...string) []MuseJob {
	t.Helper()
	res := p.SubmitJobs(MuseJobRequest{Kind: kind, Prompts: prompts})
	if !res.Success {
		t.Fatalf("submit: %s", res.Error)
	}
	return res.Data
}

func mustJob(t *testing.T, p *Provider, id string) MuseJob {
	t.Helper()
	job, ok := p.jobs.get(id)
	if !ok {
		t.Fatalf("job %s missing", id)
	}
	return job
}

func videoResult(path string, data []byte) ([]AssistantEvent, map[string][]byte) {
	return []AssistantEvent{{Text: "Xong", Videos: []ResultMedia{{Path: path, Mime: "video/mp4", ByteLen: int64(len(data))}}}},
		map[string][]byte{path: data}
}
