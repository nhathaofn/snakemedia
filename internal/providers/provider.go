// Package providers is where external generation services plug into the app.
// A provider (Google Flow today) owns its accounts, protocol and caches, and
// exposes its capabilities as engine nodes.
package providers

import "SnakeMedia/internal/engine"

// Provider is one external service contributing nodes to the engine.
type Provider interface {
	// ID prefixes the provider's node types, e.g. "flow" → "flow.image.generate".
	ID() string
	Name() string
	Nodes() []engine.Node
}
