package flow

import (
	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/providers"
	"SnakeMedia/internal/providers/flow/session"
)

// Plugin joins Google Flow to the app: its account site and the provider.
func Plugin() providers.Plugin {
	return providers.Plugin{
		Site: func(cfg *config.Manager) accounts.Site { return session.NewSite(cfg) },
		New: func(d providers.Deps) providers.Provider {
			p := New(session.NewService(d.Accounts, d.Config), d.Config, d.Media, d.ProductName)
			p.tools = d.Tools
			return p
		},
	}
}

var (
	_ providers.Lifecycle      = (*Provider)(nil)
	_ providers.OptionProvider = (*Provider)(nil)
)
