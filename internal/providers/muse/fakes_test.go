package muse

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"SnakeMedia/internal/config"
	"SnakeMedia/internal/media"
)

// fakePage scripts a Muse tab. Fields are read under mu.
type fakePage struct {
	mu          sync.Mutex
	loggedIn    bool
	sendErr     error
	sendRes     SendResult
	sends       []string
	threads     []string
	attachments [][]Attachment
	threadFails bool
	history     []AssistantEvent
	replyAfter  int
	// onSend runs inside Send (without the lock) before it answers.
	onSend    func()
	statuses  []string // returned in order; the last one repeats
	statusErr error
	events    []AssistantEvent
	files     map[string][]byte
	quota     *MuseQuota
	cancelled []string
}

func (f *fakePage) Login(context.Context) (LoginState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return LoginState{LoggedIn: f.loggedIn, HTTP: 200}, nil
}

func (f *fakePage) Send(_ context.Context, text string, atts []Attachment, thread string) (SendResult, error) {
	f.mu.Lock()
	hook := f.onSend
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends = append(f.sends, text)
	f.threads = append(f.threads, thread)
	f.attachments = append(f.attachments, atts)
	if f.threadFails && thread != "" {
		return SendResult{Reason: "thread closed"}, nil
	}
	return f.sendRes, f.sendErr
}

func (f *fakePage) Status(context.Context, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.statusErr != nil {
		return "", f.statusErr
	}
	if len(f.statuses) == 0 {
		return "idle", nil
	}
	s := f.statuses[0]
	if len(f.statuses) > 1 {
		f.statuses = f.statuses[1:]
	}
	return s, nil
}

// Result returns the chat history: history before the reply arrives, then
// history followed by events once replyAfter sends (default 1) were made.
func (f *fakePage) Result(context.Context, string) ([]AssistantEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sends) < max(f.replyAfter, 1) {
		return f.history, nil
	}
	return append(append([]AssistantEvent(nil), f.history...), f.events...), nil
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
	return nil
}

func (f *fakePage) Quota(context.Context) (*MuseQuota, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.quota, nil
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
	c := cfg.Get()
	c.Muse.AutoSave = false
	_, _ = cfg.Update(c)
	p := New(dir, cfg, media.NewStore(filepath.Join(dir, "media")))
	p.pageFor = func(context.Context, storedAccount) (Page, error) { return page, nil }
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

func addSignedIn(t *testing.T, p *Provider, name string) storedAccount {
	t.Helper()
	acc := p.newAccount(name, MuseSourceBrowser)
	acc.Status = MuseAccountSignedIn
	if err := p.accounts.add(acc); err != nil {
		t.Fatal(err)
	}
	return acc
}

func submit(t *testing.T, p *Provider, kind MuseJobKind, prompts ...string) []MuseJob {
	t.Helper()
	res := p.SubmitJobs(MuseJobRequest{Kind: kind, Prompts: prompts, Ratio: "9:16", Count: 1})
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
