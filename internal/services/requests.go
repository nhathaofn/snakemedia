package services

import (
	"context"
	"sync"
)

// RequestRegistry tracks running long requests by their request ID so the
// frontend can stop waiting on one. Cancelling only stops this app's work: a
// job Flow already accepted keeps running there and stays resumable.
type RequestRegistry struct {
	mu   sync.Mutex
	next uint64
	// Calls that share an ID (a generate and its resume) are all kept, so a
	// second call never hides the first from Cancel.
	cancels map[string]map[uint64]context.CancelFunc
}

// NewRequestRegistry returns an empty registry.
func NewRequestRegistry() *RequestRegistry {
	return &RequestRegistry{cancels: make(map[string]map[uint64]context.CancelFunc)}
}

// Start derives a cancellable context for id. The returned done func must be
// called when the request ends; it releases the context and only its own
// registration. An empty id is not registered and cannot be cancelled.
func (r *RequestRegistry) Start(parent context.Context, id string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	if id == "" {
		return ctx, cancel
	}
	r.mu.Lock()
	r.next++
	token := r.next
	if r.cancels[id] == nil {
		r.cancels[id] = make(map[uint64]context.CancelFunc)
	}
	r.cancels[id][token] = cancel
	r.mu.Unlock()
	return ctx, func() {
		r.mu.Lock()
		delete(r.cancels[id], token)
		if len(r.cancels[id]) == 0 {
			delete(r.cancels, id)
		}
		r.mu.Unlock()
		cancel()
	}
}

// Cancel stops every running call with id and reports whether one was running.
func (r *RequestRegistry) Cancel(id string) bool {
	r.mu.Lock()
	calls := make([]context.CancelFunc, 0, len(r.cancels[id]))
	for _, cancel := range r.cancels[id] {
		calls = append(calls, cancel)
	}
	r.mu.Unlock()
	for _, cancel := range calls {
		cancel()
	}
	return len(calls) > 0
}

// CancelAll stops every running call; used when the app shuts down so no
// provider work outlives the browsers and proxy bridges it runs on.
func (r *RequestRegistry) CancelAll() int {
	r.mu.Lock()
	var calls []context.CancelFunc
	for _, byToken := range r.cancels {
		for _, cancel := range byToken {
			calls = append(calls, cancel)
		}
	}
	r.mu.Unlock()
	for _, cancel := range calls {
		cancel()
	}
	return len(calls)
}
