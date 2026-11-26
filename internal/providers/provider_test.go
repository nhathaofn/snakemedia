package providers

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/engine"
	"SnakeMedia/internal/events"
)

type stubSite struct{ id accounts.Provider }

func (s stubSite) Provider() accounts.Provider { return s.id }
func (stubSite) Page() browser.Site            { return browser.Site{} }
func (stubSite) CookieDomain() string          { return ".example.test" }
func (stubSite) AcceptsCookie(string) bool     { return true }
func (stubSite) Inspect(context.Context, *browser.Browser, accounts.Record) (accounts.Inspection, error) {
	return accounts.Inspection{}, nil
}

type stubProvider struct {
	id      string
	log     *[]string
	source  string
	onClose func()
}

func (p *stubProvider) ID() string   { return p.id }
func (p *stubProvider) Name() string { return p.id }
func (p *stubProvider) Nodes() []engine.Node {
	return []engine.Node{engine.Typed(engine.Spec{
		Type:   p.id + ".make",
		Params: []engine.Param{{Name: "model", Kind: engine.ParamSelect, OptionsFrom: p.source}},
	}, func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil })}
}
func (p *stubProvider) OptionSources() map[string]engine.OptionSource {
	return map[string]engine.OptionSource{p.id + ".models": func(context.Context, map[string]string) ([]engine.Option, error) {
		return []engine.Option{{Value: "m"}}, nil
	}}
}
func (p *stubProvider) Start(context.Context, func(events.Event, any)) {
	*p.log = append(*p.log, "start "+p.id)
}
func (p *stubProvider) Close() {
	if p.onClose != nil {
		p.onClose()
	}
}

func plugin(id string, log *[]string, source string) Plugin {
	return Plugin{
		Site: func(*config.Manager) accounts.Site { return stubSite{id: accounts.Provider(id)} },
		New:  func(Deps) Provider { return &stubProvider{id: id, log: log, source: source} },
	}
}

func TestPluginsJoinTheApp(t *testing.T) {
	cfg, _ := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	var log []string
	set := Prepare(cfg, plugin("one", &log, "one.models"), plugin("two", &log, "two.models"))
	if len(set.Sites()) != 2 || set.Sites()[1].Provider() != "two" {
		t.Fatalf("every plugin brings its site: %+v", set.Sites())
	}
	set.Build(Deps{})
	reg := engine.NewRegistry()
	if err := set.Register(reg); err != nil {
		t.Fatal(err)
	}
	if specs := reg.Specs(); len(specs) != 2 {
		t.Fatalf("nodes of every provider registered: %+v", specs)
	}
	if _, err := reg.Options(context.Background(), "two.models", nil); err != nil {
		t.Fatal(err)
	}
	set.Start(context.Background(), func(events.Event, any) {})
	closed := make(chan string, 2)
	for _, provider := range set.built {
		provider.(*stubProvider).onClose = func() { closed <- provider.ID() }
	}
	set.Close()
	if len(closed) != 2 {
		t.Fatal("every plugin should close")
	}
	if got := strings.Join(log, ","); got != "start one,start two" {
		t.Fatalf("lifecycle in plugin order, got %s", got)
	}
	if p := Find[*stubProvider](set); p == nil || p.id != "one" {
		t.Fatal("Find returns the typed provider")
	}
}

func TestUnresolvedOptionSourceIsAWiringError(t *testing.T) {
	cfg, _ := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	var log []string
	set := Prepare(cfg, plugin("one", &log, "missing.models"))
	set.Build(Deps{})
	if err := set.Register(engine.NewRegistry()); err == nil {
		t.Fatal("a setting whose options nobody provides must stop the app at start")
	}
}

type stuckProvider struct {
	stubProvider
	release chan struct{}
}

func (p *stuckProvider) Close() { <-p.release }

func TestCloseDoesNotWaitForeverOnAStuckProvider(t *testing.T) {
	var log []string
	stuck := &stuckProvider{stubProvider: stubProvider{id: "stuck", log: &log}, release: make(chan struct{})}
	defer close(stuck.release)
	otherClosed := make(chan struct{})
	other := &stubProvider{id: "other", onClose: func() { close(otherClosed) }}
	set := &Set{built: []Provider{stuck, other}, closeWait: 50 * time.Millisecond}
	done := make(chan struct{})
	go func() {
		set.Close()
		close(done)
	}()
	select {
	case <-done:
		select {
		case <-otherClosed:
		default:
			t.Fatal("another provider was skipped because the first one hung")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close kept waiting on a provider that never closes")
	}
}
