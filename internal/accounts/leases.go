package accounts

import (
	"context"
	"path/filepath"

	"SnakeMedia/internal/browser"
)

// Acquire marks the account's browser as needed by running work until the
// returned release is called; it is then never restarted or removed.
func (s *Service) Acquire(id string) (release func()) {
	rec, err := s.store.get(id)
	if err != nil {
		return func() {}
	}
	release, _ = s.AcquireProfile(context.Background(), rec.ProfileDir)
	return release
}

// AcquireProfile is Acquire for callers that hold the account's profile path.
func (s *Service) AcquireProfile(ctx context.Context, profileDir string) (func(), error) {
	profileDir = filepath.Clean(profileDir)
	for {
		s.leaseMu.Lock()
		if err := ctx.Err(); err != nil {
			s.leaseMu.Unlock()
			return nil, err
		}
		check := s.exclusiveChecks[profileDir]
		if check == nil {
			release := s.pool.Acquire(profileDir)
			s.leaseMu.Unlock()
			return release, nil
		}
		s.leaseMu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-check:
		}
	}
}

// AcquireWork reserves shared browser work, waiting for a manual check to finish.
func (s *Service) AcquireWork(ctx context.Context, id string) (func(), error) {
	rec, err := s.store.get(id)
	if err != nil {
		return nil, err
	}
	return s.AcquireProfile(ctx, rec.ProfileDir)
}

func (s *Service) tryCheckProfile(profileDir string) (func(), error) {
	profileDir = filepath.Clean(profileDir)
	s.leaseMu.Lock()
	defer s.leaseMu.Unlock()
	if s.exclusiveChecks[profileDir] != nil || s.pool.InUse(profileDir) {
		return nil, browser.ErrProfileBusy
	}
	if s.exclusiveChecks == nil {
		s.exclusiveChecks = make(map[string]chan struct{})
	}
	check := make(chan struct{})
	s.exclusiveChecks[profileDir] = check
	release := s.pool.Acquire(profileDir)
	return func() {
		s.leaseMu.Lock()
		defer s.leaseMu.Unlock()
		release()
		delete(s.exclusiveChecks, profileDir)
		close(check)
	}, nil
}
