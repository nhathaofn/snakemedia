package muse

import (
	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/providers"
)

// Plugin joins Muse to the app: its account site and the provider.
func Plugin() providers.Plugin {
	return providers.Plugin{
		Site: func(cfg *config.Manager) accounts.Site { return NewSite(cfg) },
		New: func(d providers.Deps) providers.Provider {
			return New(d.Paths.DataDir, d.Paths.MuseOutputDir, d.Config, d.Media, d.Accounts)
		},
	}
}

var _ providers.Lifecycle = (*Provider)(nil)
