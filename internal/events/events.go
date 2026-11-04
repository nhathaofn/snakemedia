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
	// EventMuseJobUpdated carries one muse.MuseJob whenever its state changes.
	EventMuseJobUpdated Event = "muse:job-updated"
	// EventMuseJobsRemoved carries the []string IDs of jobs removed at once
	// (bulk removal or trim), so listeners drop them without a reload.
	EventMuseJobsRemoved Event = "muse:jobs-removed"
	// EventFlowImageTaskUpdated carries one flow.ImageTask whenever its state changes.
	EventFlowImageTaskUpdated Event = "flow:image-task-updated"
	// EventFlowImageTasksAdded carries the []flow.ImageTask added at once.
	EventFlowImageTasksAdded Event = "flow:image-tasks-added"
	// EventFlowImageTasksRemoved carries the []string IDs of tasks removed or
	// trimmed at once, so listeners drop them without reloading the queue.
	EventFlowImageTasksRemoved Event = "flow:image-tasks-removed"
	// EventToolMissing carries the tools.Request of a feature that needs a
	// tool the machine does not have; the user decides whether to download it.
	EventToolMissing Event = "tools:missing"
	// EventToolProgress carries tools.Progress while a tool downloads.
	EventToolProgress Event = "tools:progress"
	// EventAccountsUpdated carries the full []accounts.Account list.
	EventAccountsUpdated Event = "accounts:updated"
	// EventUpdateAvailable carries the updater.Release a startup check found.
	EventUpdateAvailable Event = "update:available"
	// EventUpdateProgress carries updater.Progress while an installer downloads.
	EventUpdateProgress Event = "update:progress"
)

// AllEvents is bound to the frontend as a TypeScript enum (events.Event).
var AllEvents = []struct {
	Value  Event
	TSName string
}{
	{EventAppReady, "AppReady"},
	{EventConfigUpdated, "ConfigUpdated"},
	{EventMuseJobUpdated, "MuseJobUpdated"},
	{EventMuseJobsRemoved, "MuseJobsRemoved"},
	{EventFlowImageTaskUpdated, "FlowImageTaskUpdated"},
	{EventFlowImageTasksAdded, "FlowImageTasksAdded"},
	{EventFlowImageTasksRemoved, "FlowImageTasksRemoved"},
	{EventToolMissing, "ToolMissing"},
	{EventToolProgress, "ToolProgress"},
	{EventAccountsUpdated, "AccountsUpdated"},
	{EventUpdateAvailable, "UpdateAvailable"},
	{EventUpdateProgress, "UpdateProgress"},
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
