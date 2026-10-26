package services

import (
	"context"
	"sync"
)

// RequestRegistry tracks running long requests by their request ID so the
// frontend can stop waiting on one. Cancelling only stops this app's work: a
// job Flow already accepted keeps running there and stays resumable.
type RequestRegistry struct {
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

// NewRequestRegistry returns an empty registry.
func NewRequestRegistry() *RequestRegistry {
	return &RequestRegistry{cancels: make(map[string]context.CancelFunc)}
}

// Start derives a cancellable context for id. The returned done func must be
// called when the request ends; it releases the context and the registration.
// An empty id is not registered and cannot be cancelled.
func (r *RequestRegistry) Start(parent context.Context, id string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	if id == "" {
		return ctx, cancel
	}
	r.mu.Lock()
	r.cancels[id] = cancel
	r.mu.Unlock()
	return ctx, func() {
		r.mu.Lock()
		delete(r.cancels, id)
		r.mu.Unlock()
		cancel()
	}
}

// Cancel stops the running request with id and reports whether one was running.
func (r *RequestRegistry) Cancel(id string) bool {
	r.mu.Lock()
	cancel, ok := r.cancels[id]
	r.mu.Unlock()
	if ok {
		cancel()
	}
	return ok
}
