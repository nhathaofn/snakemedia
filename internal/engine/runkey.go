package engine

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

type runKeyContext struct{}

// runNamespace keeps request IDs derived from graph runs apart from any other
// name-based UUID.
var runNamespace = uuid.MustParse("6f1c2a8e-3d4b-5c6d-8e7f-0a1b2c3d4e5f")

// WithRunKey marks ctx as one node instance of a graph run. A provider that
// sends billable requests derives their IDs from it, so running the same graph
// again (same RequestID) resumes what was sent instead of paying again.
func WithRunKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, runKeyContext{}, key)
}

// RunKey is the node instance's run key; empty outside a graph run that has a
// RequestID, where every send gets a fresh ID as before.
func RunKey(ctx context.Context) string {
	key, _ := ctx.Value(runKeyContext{}).(string)
	return key
}

// RequestIDFor derives a stable request ID for one send of a node run: the
// same key, parts and attempt always give the same UUID. Providers bump the
// attempt only after a definite answer that nothing was made.
func RequestIDFor(key string, attempt int, parts ...any) string {
	return uuid.NewSHA1(runNamespace, fmt.Appendf(nil, "%s|%d|%v", key, attempt, parts)).String()
}
