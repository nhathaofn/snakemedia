// Package events defines backend→frontend IPC event names and the dispatcher.
//
// Event names are bound to TypeScript via Wails EnumBind (events.Event in
// frontend/wailsjs/go/models.ts), so this file is the single source of truth.
package events

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Event is a Wails runtime event name.
type Event string

const (
	EventAppReady      Event = "app:ready"
	EventConfigUpdated Event = "config:updated"
	EventErrorOccurred Event = "error:occurred"
)

// AllEvents is bound to the frontend as a TypeScript enum (events.Event).
var AllEvents = []struct {
	Value  Event
	TSName string
}{
	{EventAppReady, "AppReady"},
	{EventConfigUpdated, "ConfigUpdated"},
	{EventErrorOccurred, "ErrorOccurred"},
}

// Dispatcher broadcasts events to the Wails frontend.
type Dispatcher struct {
	ctx context.Context
}

// NewDispatcher creates an event dispatcher bound to the Wails runtime context.
func NewDispatcher(ctx context.Context) *Dispatcher {
	return &Dispatcher{ctx: ctx}
}

// Emit broadcasts an event with optional payload to all frontend listeners.
// It is a no-op on a nil dispatcher or before the runtime context exists.
func (d *Dispatcher) Emit(event Event, optionalData ...interface{}) {
	if d == nil || d.ctx == nil {
		return
	}
	runtime.EventsEmit(d.ctx, string(event), optionalData...)
}
