package providers

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

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
	id     string
	log    *[]string
	source string
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
func (p *stubProvider) Close() { *p.log = append(*p.log, "close "+p.id) }

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
	set.Close()
	if got := strings.Join(log, ","); got != "start one,start two,close one,close two" {
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
