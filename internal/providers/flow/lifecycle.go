package flow

import (
	"context"

	"SnakeMedia/internal/events"
)

// Start restores and runs both durable queues under the provider lifetime.
func (s *Provider) Start(ctx context.Context, emit func(events.Event, any)) {
	s.queue.start(ctx, emit)
	s.videoQueue.start(ctx, emit)
}

// Close stops both schedulers before waiting for their workers. Unfinished
// requests keep their IDs in the journals for reconciliation after restart.
func (s *Provider) Close() {
	s.queue.mu.Lock()
	stopImages := s.queue.stop
	s.queue.mu.Unlock()
	s.videoQueue.mu.Lock()
	stopVideos := s.videoQueue.stop
	s.videoQueue.mu.Unlock()
	for _, stop := range []context.CancelFunc{stopImages, stopVideos} {
		if stop != nil {
			stop()
		}
	}
	s.queue.close()
	s.videoQueue.close()
}
