// Package providers is where external generation services plug into the app.
// A provider (Google Flow, Muse) contributes an accounts Site, owns its
// protocol and caches, and exposes its capabilities as engine nodes. Adding
// one is a new package plus one Plugin line in app.go.
package providers

import (
	"context"
	"fmt"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/engine"
	"SnakeMedia/internal/events"
	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/media"
	"SnakeMedia/internal/paths"
	"SnakeMedia/internal/tools"
)

// Provider is one external service contributing nodes to the engine.
type Provider interface {
	// ID prefixes the provider's node types, e.g. "flow" → "flow.image.generate".
	ID() string
	Name() string
	Nodes() []engine.Node
}

// OptionProvider is a provider whose node settings read their choices at run
// time (Param.OptionsFrom).
type OptionProvider interface {
	OptionSources() map[string]engine.OptionSource
}

// Lifecycle is a provider with background work: started once the app runs,
// closed (waiting for it) before accounts and browsers close.
type Lifecycle interface {
	Start(ctx context.Context, emit func(events.Event, any))
	Close()
}

// Deps are the shared services a provider is built on.
type Deps struct {
	Paths    paths.Paths
	Config   *config.Manager
	Media    *media.Store
	Accounts *accounts.Service
	// Tools finds external programs and asks the user to download a missing one.
	Tools       *tools.Service
	ProductName string
}

// Plugin is how a provider joins the app: its accounts Site first (the
// account service is built from every Site), then the provider itself.
type Plugin struct {
	Site func(cfg *config.Manager) accounts.Site
	New  func(Deps) Provider
}

// Set is the app's providers, built in plugin order.
type Set struct {
	sites     []accounts.Site
	built     []Provider
	news      []func(Deps) Provider
	closeWait time.Duration // 0 means closeWait; tests shorten it
}

// Prepare makes every plugin's Site; Build then makes the providers.
func Prepare(cfg *config.Manager, plugins ...Plugin) *Set {
	set := &Set{}
	for _, p := range plugins {
		set.sites = append(set.sites, p.Site(cfg))
		set.news = append(set.news, p.New)
	}
	return set
}

// Sites are the plugins' account sites.
func (s *Set) Sites() []accounts.Site { return s.sites }

// Build makes the providers once the shared services exist.
func (s *Set) Build(deps Deps) []Provider {
	s.built = make([]Provider, 0, len(s.news))
	for _, newProvider := range s.news {
		s.built = append(s.built, newProvider(deps))
	}
	return s.built
}

// Register adds every provider's nodes and option sources, plus extra nodes,
// and checks that each setting's options can be resolved.
func (s *Set) Register(reg *engine.Registry, extra ...engine.Node) error {
	for _, p := range s.built {
		if err := reg.Register(p.Nodes()...); err != nil {
			return fmt.Errorf("%s: %w", p.ID(), err)
		}
		if op, ok := p.(OptionProvider); ok {
			if err := reg.RegisterOptions(op.OptionSources()); err != nil {
				return fmt.Errorf("%s: %w", p.ID(), err)
			}
		}
	}
	if err := reg.Register(extra...); err != nil {
		return err
	}
	return reg.Check()
}

// Start starts every provider with background work, in plugin order.
func (s *Set) Start(ctx context.Context, emit func(events.Event, any)) {
	for _, p := range s.built {
		if l, ok := p.(Lifecycle); ok {
			l.Start(ctx, emit)
		}
	}
}

// closeWait bounds how long Close waits for all providers together. Their
// journals keep unfinished jobs resumable, so a provider stuck on a slow call
// is left behind (and ends with the process) rather than holding up the exit;
// the account browsers it may be waiting on close right after.
const closeWait = 10 * time.Second

// Close closes every provider with background work, in plugin order, for at
// most closeWait in all.
func (s *Set) Close() {
	wait := s.closeWait
	if wait <= 0 {
		wait = closeWait
	}
	deadline := time.After(wait)
	for _, p := range s.built {
		l, ok := p.(Lifecycle)
		if !ok {
			continue
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			defer logger.Recover("đóng " + p.Name())
			l.Close()
		}()
		select {
		case <-done:
		case <-deadline:
			logger.Get().Warn("provider did not close in time, leaving it to end with the app", "provider", p.ID(), "wait", wait)
			return
		}
	}
}

// Find returns the built provider of type T (the app's typed handle for its
// bound methods).
func Find[T Provider](s *Set) T {
	for _, p := range s.built {
		if t, ok := p.(T); ok {
			return t
		}
	}
	var zero T
	return zero
}
