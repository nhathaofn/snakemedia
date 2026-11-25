package muse

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"SnakeMedia/internal/browser"
	"SnakeMedia/internal/config"
)

// errTabCapReached signals that ThreadsPerAccount dropped below active concurrent
// jobs before this job acquired the shared tab, so it should re-queue cleanly.
var errTabCapReached = errors.New("Số luồng trên tài khoản vừa giảm; xếp hàng lại.")

// sharedTabReloadEvery reloads an account's shared muse.ai tab cleanly back to
// Home after this many completed prompt runs so long 200-300 prompt batches do
// not accumulate unbounded Next.js DOM/React heap memory.
const sharedTabReloadEvery = 15

type tabSlot struct {
	targetID    string
	jobs        []string
	needsReload bool
	uses        int
}

func (s tabSlot) busy() bool { return len(s.jobs) > 0 }

type accountTabState struct {
	port  int
	slots []tabSlot
}

func (st *accountTabState) totalBusy() int {
	n := 0
	for _, s := range st.slots {
		n += len(s.jobs)
	}
	return n
}

// tabPool manages one shared muse.ai tab per account Chrome (Shared-Tab Mode):
// multiple concurrent threads (ThreadsPerAccount 1..5) share that single tab's
// WebSocket runtime instead of spawning one heavy Next.js renderer tab per
// thread, cutting Chrome RAM sharply while preserving full concurrency.
type tabPool struct {
	mu          sync.Mutex
	locks       map[string]chan struct{}
	accounts    map[string]*accountTabState
	reloadAfter int

	navLock     func(accountID, targetID string) *sync.RWMutex
	routeLock   func(accountID, targetID string) *sync.Mutex
	listTargets func(context.Context, int, config.AccountConfig) ([]browser.Target, error)
	openTab     func(context.Context, int, config.AccountConfig) (browser.Target, error)
	navigate    func(context.Context, browser.Target, int, browser.Site) error
	closeTab    func(context.Context, int, config.AccountConfig, string) error
	sleep       func(context.Context, time.Duration) error
	now         func() time.Time
}

func newTabPool() *tabPool {
	return &tabPool{
		locks:       make(map[string]chan struct{}),
		accounts:    make(map[string]*accountTabState),
		reloadAfter: sharedTabReloadEvery,
		listTargets: browser.Targets,
		openTab:     browser.NewTab,
		navigate:    browser.Navigate,
		closeTab:    browser.CloseTab,
		sleep:       sleepContext,
		now:         time.Now,
	}
}

func (tp *tabPool) reloadThreshold() int {
	if tp.reloadAfter > 0 {
		return tp.reloadAfter
	}
	return sharedTabReloadEvery
}

func (tp *tabPool) lockAccount(ctx context.Context, accountID string) (func(), error) {
	tp.mu.Lock()
	ch := tp.locks[accountID]
	if ch == nil {
		ch = make(chan struct{}, 1)
		tp.locks[accountID] = ch
	}
	tp.mu.Unlock()
	select {
	case ch <- struct{}{}:
		return func() { <-ch }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (tp *tabPool) hasBusy(accountID string, port int) bool {
	return tp.hasOtherBusy(accountID, port, "", "")
}

// hasOtherBusy reports whether another job (not excludeJobID, or another holder
// sharing the same tab) is currently active on accountID's Chrome.
func (tp *tabPool) hasOtherBusy(accountID string, port int, excludeTargetID, excludeJobID string) bool {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	st := tp.accounts[accountID]
	if st == nil || st.port != port {
		return false
	}
	for _, s := range st.slots {
		if len(s.jobs) == 0 {
			continue
		}
		if excludeJobID != "" {
			for _, id := range s.jobs {
				if id != excludeJobID {
					return true
				}
			}
			continue
		}
		if s.targetID == excludeTargetID {
			if len(s.jobs) > 1 {
				return true
			}
			continue
		}
		return true
	}
	return false
}

// hasExcess reports whether any Chrome holds more than 1 tracked muse.ai tab
// that has gone idle.
func (tp *tabPool) hasExcess(_ int) bool {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	for _, st := range tp.accounts {
		if len(st.slots) > 1 {
			for i := 1; i < len(st.slots); i++ {
				if !st.slots[i].busy() {
					return true
				}
			}
		}
	}
	return false
}

func (tp *tabPool) dropAccount(accountID string) {
	tp.mu.Lock()
	delete(tp.accounts, accountID)
	tp.mu.Unlock()
}

func (tp *tabPool) syncLocked(ctx context.Context, accountID string, port int, policy config.AccountConfig) (*accountTabState, []browser.Target, error) {
	targets, err := tp.listTargets(ctx, port, policy)
	if err != nil {
		return nil, nil, err
	}
	liveMuse := make([]string, 0, len(targets))
	liveSet := make(map[string]bool, len(targets))
	var blanks []browser.Target
	for _, t := range targets {
		u, ok := t.Page()
		if !ok {
			continue
		}
		if museURL(u) {
			liveMuse = append(liveMuse, t.ID)
			liveSet[t.ID] = true
		} else if t.URL == "about:blank" || t.URL == "chrome://newtab/" {
			blanks = append(blanks, t)
		}
	}
	tp.mu.Lock()
	defer tp.mu.Unlock()
	st := tp.accounts[accountID]
	if st == nil || st.port != port {
		st = &accountTabState{port: port}
		tp.accounts[accountID] = st
	}
	kept := st.slots[:0]
	seen := make(map[string]bool, len(st.slots))
	for _, s := range st.slots {
		if liveSet[s.targetID] && !seen[s.targetID] {
			seen[s.targetID] = true
			kept = append(kept, s)
		}
	}
	st.slots = kept
	for _, id := range liveMuse {
		if !seen[id] {
			seen[id] = true
			st.slots = append(st.slots, tabSlot{targetID: id})
		}
	}
	return st, blanks, nil
}

// popExcessIdle removes excess idle secondary slots from tail down to keep (at
// least 1), so Shared-Tab Mode prunes any leftover secondary tabs cleanly.
func (tp *tabPool) popExcessIdle(st *accountTabState, port, want int) []string {
	keep := max(want, 1)
	tp.mu.Lock()
	defer tp.mu.Unlock()
	if st.port != port || len(st.slots) <= keep {
		return nil
	}
	var drop []string
	for i := len(st.slots) - 1; i >= 0 && len(st.slots) > keep; i-- {
		if st.slots[i].busy() {
			continue
		}
		drop = append(drop, st.slots[i].targetID)
		st.slots = slices.Delete(st.slots, i, i+1)
	}
	return drop
}

func (tp *tabPool) closeTargets(ctx context.Context, port int, policy config.AccountConfig, ids []string) {
	for _, id := range ids {
		_ = tp.closeTab(ctx, port, policy, id)
	}
}

func removeJobFromSlot(jobs []string, jobID string) []string {
	if len(jobs) == 0 {
		return jobs
	}
	for i := len(jobs) - 1; i >= 0; i-- {
		if jobs[i] == jobID {
			return slices.Delete(jobs, i, i+1)
		}
	}
	return jobs[:len(jobs)-1]
}

// acquire binds jobID to accountID's single shared muse.ai tab on port (opening
// one tab only when none exists yet) as long as total concurrent holders stay
// within wantThreads.
func (tp *tabPool) acquire(ctx context.Context, accountID, jobID string, port, wantThreads int, policy config.AccountConfig) (string, error) {
	if port <= 0 {
		return "", errTabMissing
	}
	unlock, err := tp.lockAccount(ctx, accountID)
	if err != nil {
		return "", err
	}
	defer unlock()

	want := max(wantThreads, 1)
	st, blanks, err := tp.syncLocked(ctx, accountID, port, policy)
	if err != nil {
		return "", err
	}

	tp.mu.Lock()
	if st.totalBusy() >= want {
		tp.mu.Unlock()
		return "", errTabCapReached
	}
	if len(st.slots) > 0 {
		// Pick the primary shared tab and close any excess secondary idle tabs.
		idx := 0
		chosen := st.slots[idx].targetID
		st.slots[idx].jobs = append(st.slots[idx].jobs, jobID)
		reload := st.slots[idx].needsReload
		if reload {
			st.slots[idx].needsReload = false
			st.slots[idx].uses = 0
		}
		tp.mu.Unlock()

		if excess := tp.popExcessIdle(st, port, 1); len(excess) > 0 {
			tp.closeTargets(ctx, port, policy, excess)
		}
		if reload {
			if err := tp.reloadSharedTab(ctx, accountID, port, chosen, policy); err != nil {
				tp.mu.Lock()
				for i := range st.slots {
					if st.slots[i].targetID == chosen {
						st.slots[i].jobs = removeJobFromSlot(st.slots[i].jobs, jobID)
						st.slots[i].needsReload = true
						break
					}
				}
				tp.mu.Unlock()
				return "", err
			}
		}
		return chosen, nil
	}
	tp.mu.Unlock()

	var target browser.Target
	openedFresh := false
	if len(blanks) > 0 {
		target = blanks[0]
	} else {
		created, err := tp.openTab(ctx, port, policy)
		if err != nil {
			return "", err
		}
		target = created
		openedFresh = true
	}
	if err := tp.navigate(ctx, target, port, musePage); err != nil {
		if openedFresh {
			_ = tp.closeTab(context.WithoutCancel(ctx), port, policy, target.ID)
		}
		return "", err
	}
	if err := tp.waitOnMuse(ctx, port, target.ID, policy); err != nil {
		if openedFresh {
			_ = tp.closeTab(context.WithoutCancel(ctx), port, policy, target.ID)
		}
		return "", err
	}

	tp.mu.Lock()
	cur := tp.accounts[accountID]
	if cur != st || cur.port != port {
		tp.mu.Unlock()
		if openedFresh {
			_ = tp.closeTab(context.WithoutCancel(ctx), port, policy, target.ID)
		}
		return "", errTabMissing
	}
	cur.slots = append(cur.slots, tabSlot{targetID: target.ID, jobs: []string{jobID}})
	tp.mu.Unlock()
	return target.ID, nil
}

func (tp *tabPool) reloadSharedTab(ctx context.Context, accountID string, port int, targetID string, policy config.AccountConfig) error {
	navTarget, ok := findTarget(ctx, tp, port, policy, targetID)
	if !ok {
		return errTabMissing
	}
	if tp.routeLock != nil {
		if rm := tp.routeLock(accountID, targetID); rm != nil {
			rm.Lock()
			defer rm.Unlock()
		}
	}
	if tp.navLock != nil {
		if nav := tp.navLock(accountID, targetID); nav != nil {
			nav.Lock()
			defer nav.Unlock()
		}
	}
	if err := tp.navigate(ctx, navTarget, port, musePage); err != nil {
		return err
	}
	return tp.waitOnMuse(ctx, port, targetID, policy)
}

func (tp *tabPool) waitOnMuse(ctx context.Context, port int, targetID string, policy config.AccountConfig) error {
	waitMax := time.Duration(policy.PageReadySeconds) * time.Second
	if waitMax <= 0 {
		waitMax = 30 * time.Second
	}
	deadline := tp.now().Add(waitMax)
	for {
		targets, err := tp.listTargets(ctx, port, policy)
		if err != nil {
			return err
		}
		for _, t := range targets {
			if t.ID != targetID {
				continue
			}
			if u, ok := t.Page(); ok && museURL(u) {
				return nil
			}
			if t.URL != "" && t.URL != "about:blank" && t.URL != "chrome://newtab/" {
				return fmt.Errorf("Tab Muse mới điều hướng sang trang khác (%s)", sanitizeAwayURL(t.URL))
			}
		}
		if !tp.now().Before(deadline) {
			return errTabMissing
		}
		if err := tp.sleep(ctx, 150*time.Millisecond); err != nil {
			return err
		}
	}
}

// release detaches jobID from targetID, closes excess idle secondary tabs,
// and marks the shared tab to reload cleanly to Home on errors or after
// periodic prompt batches so Chrome's JS heap stays bounded.
func (tp *tabPool) release(ctx context.Context, accountID string, port int, targetID, jobID string, dirty bool, wantThreads func() int, policy config.AccountConfig) {
	if targetID == "" || port <= 0 {
		return
	}
	want := 0
	if wantThreads != nil {
		want = wantThreads()
	}
	needsClose := false
	reloadAfter := tp.reloadThreshold()
	tp.mu.Lock()
	st := tp.accounts[accountID]
	if st != nil && st.port == port {
		for i := range st.slots {
			if st.slots[i].targetID == targetID {
				st.slots[i].jobs = removeJobFromSlot(st.slots[i].jobs, jobID)
				st.slots[i].uses++
				if dirty || (reloadAfter > 0 && st.slots[i].uses >= reloadAfter) {
					st.slots[i].needsReload = true
				}
				needsClose = len(st.slots) > 1 && !st.slots[i].busy() && (dirty || i > 0 || (want > 0 && i >= want))
				break
			}
		}
	}
	tp.mu.Unlock()
	if !needsClose || ctx.Err() != nil {
		return
	}
	closeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	unlock, err := tp.lockAccount(closeCtx, accountID)
	if err != nil {
		return
	}
	defer unlock()

	remove := false
	tp.mu.Lock()
	st = tp.accounts[accountID]
	if st != nil && st.port == port && len(st.slots) > 1 {
		for i := range st.slots {
			if st.slots[i].targetID == targetID && !st.slots[i].busy() && (dirty || i > 0) {
				st.slots = slices.Delete(st.slots, i, i+1)
				remove = true
				break
			}
		}
	}
	tp.mu.Unlock()
	if remove {
		_ = tp.closeTab(closeCtx, port, policy, targetID)
	}
}

// reconcileIdle closes excess idle secondary tabs so each Chrome keeps 1 shared tab.
func (tp *tabPool) reconcileIdle(ctx context.Context, _ int, policy config.AccountConfig) {
	tp.mu.Lock()
	var targets []string
	for accID, st := range tp.accounts {
		if len(st.slots) > 1 {
			for _, s := range st.slots {
				if !s.busy() {
					targets = append(targets, accID)
					break
				}
			}
		}
	}
	tp.mu.Unlock()
	if len(targets) == 0 || ctx.Err() != nil {
		return
	}
	reconcileCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, accID := range targets {
		unlock, err := tp.lockAccount(reconcileCtx, accID)
		if err != nil {
			continue
		}
		tp.mu.Lock()
		st := tp.accounts[accID]
		port := 0
		if st != nil {
			port = st.port
		}
		tp.mu.Unlock()
		if st != nil && port > 0 {
			if drop := tp.popExcessIdle(st, port, 1); len(drop) > 0 {
				tp.closeTargets(reconcileCtx, port, policy, drop)
			}
		}
		unlock()
	}
}

func findTarget(ctx context.Context, tp *tabPool, port int, policy config.AccountConfig, targetID string) (browser.Target, bool) {
	targets, err := tp.listTargets(ctx, port, policy)
	if err != nil {
		return browser.Target{}, false
	}
	for _, t := range targets {
		if t.ID == targetID {
			return t, true
		}
	}
	return browser.Target{}, false
}

// invalidateAccount closes idle secondary tabs of accountID on port and marks
// remaining tabs to reload cleanly before their next reuse so every thread picks
// up refreshed cookies and runtime state.
func (tp *tabPool) invalidateAccount(ctx context.Context, accountID string, port int, keepTargetID string, policy config.AccountConfig) {
	if tp == nil || accountID == "" || port <= 0 {
		return
	}
	tp.mu.Lock()
	st := tp.accounts[accountID]
	var drop []string
	if st != nil && st.port == port {
		kept := st.slots[:0]
		total := len(st.slots)
		for i, s := range st.slots {
			remainingAfter := total - i - 1
			if !s.busy() && s.targetID != keepTargetID && len(kept)+remainingAfter >= 1 {
				drop = append(drop, s.targetID)
				continue
			}
			s.needsReload = true
			s.uses = 0
			kept = append(kept, s)
		}
		st.slots = kept
	}
	tp.mu.Unlock()
	if len(drop) > 0 && ctx.Err() == nil {
		tp.closeTargets(ctx, port, policy, drop)
	}
}
