package muse

import (
	"context"
	"sync"
	"time"
)

// accountSlots manages per-account semaphores (send/download/quota/login) and
// per-tab route/navigation locks. Maps are initialized via newAccountSlots.
type accountSlots struct {
	mu            sync.Mutex
	send          map[string]chan struct{}
	download      map[string]chan struct{}
	imageDownload map[string]chan struct{}
	quota         map[string]chan struct{}
	login         map[string]chan struct{}
	navs          map[string]*sync.RWMutex
	routes        map[string]*sync.Mutex
}

func newAccountSlots() *accountSlots {
	return &accountSlots{
		send:          make(map[string]chan struct{}),
		download:      make(map[string]chan struct{}),
		imageDownload: make(map[string]chan struct{}),
		quota:         make(map[string]chan struct{}),
		login:         make(map[string]chan struct{}),
		navs:          make(map[string]*sync.RWMutex),
		routes:        make(map[string]*sync.Mutex),
	}
}

func (s *accountSlots) acquireCap(ctx context.Context, slotsPtr *map[string]chan struct{}, accountID string, cap int) (func(), error) {
	if cap <= 0 {
		cap = 1
	}
	s.mu.Lock()
	if *slotsPtr == nil {
		*slotsPtr = make(map[string]chan struct{})
	}
	slot := (*slotsPtr)[accountID]
	if slot == nil {
		slot = make(chan struct{}, cap)
		(*slotsPtr)[accountID] = slot
	}
	s.mu.Unlock()
	select {
	case slot <- struct{}{}:
		return func() { <-slot }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *accountSlots) nav(key string) *sync.RWMutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.navs == nil {
		s.navs = make(map[string]*sync.RWMutex)
	}
	lock := s.navs[key]
	if lock == nil {
		lock = &sync.RWMutex{}
		s.navs[key] = lock
	}
	return lock
}

func (s *accountSlots) route(key string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.routes == nil {
		s.routes = make(map[string]*sync.Mutex)
	}
	lock := s.routes[key]
	if lock == nil {
		lock = &sync.Mutex{}
		s.routes[key] = lock
	}
	return lock
}

func (s *accountSlots) dropAccount(accountID string) {
	prefix := accountID + ":"
	s.mu.Lock()
	for k := range s.navs {
		if k == accountID || (len(k) >= len(prefix) && k[:len(prefix)] == prefix) {
			delete(s.navs, k)
		}
	}
	for k := range s.routes {
		if k == accountID || (len(k) >= len(prefix) && k[:len(prefix)] == prefix) {
			delete(s.routes, k)
		}
	}
	s.mu.Unlock()
}

// maxConcurrentStateRefreshes caps how many background account state refreshes
// can open headless Chrome instances concurrently, preventing launch storms
// when many degraded accounts exist at batch start.
const maxConcurrentStateRefreshes = 2

// accountRuntime holds per-account transient state guarded by Provider.mu
// (verification cache, refresh coordination, backoff, quota cooldown, and
// browser recycle markers).
type accountRuntime struct {
	verifiedPorts     map[string]verifiedSession
	sessionEpoch      map[string]int64
	lastRefresh       map[string]refreshOutcome
	stateRefreshSeen  map[string]string
	stateRefreshBusy  map[string]bool
	stateRefreshSlots chan struct{}
	backoff           map[string]time.Time
	quotaAttempt      map[string]time.Time
	quotaPending      map[string]*quotaRefresh
	recycleBrowser    map[string]bool
}

func newAccountRuntime() accountRuntime {
	return accountRuntime{
		verifiedPorts:     make(map[string]verifiedSession),
		sessionEpoch:      make(map[string]int64),
		lastRefresh:       make(map[string]refreshOutcome),
		stateRefreshSeen:  make(map[string]string),
		stateRefreshBusy:  make(map[string]bool),
		stateRefreshSlots: make(chan struct{}, maxConcurrentStateRefreshes),
		backoff:           make(map[string]time.Time),
		quotaAttempt:      make(map[string]time.Time),
		quotaPending:      make(map[string]*quotaRefresh),
		recycleBrowser:    make(map[string]bool),
	}
}

func (r *accountRuntime) removeAccount(accountID string) {
	delete(r.backoff, accountID)
	delete(r.quotaAttempt, accountID)
	delete(r.sessionEpoch, accountID)
	delete(r.lastRefresh, accountID)
	delete(r.stateRefreshSeen, accountID)
	delete(r.stateRefreshBusy, accountID)
	delete(r.verifiedPorts, accountID)
	delete(r.recycleBrowser, accountID)
}
