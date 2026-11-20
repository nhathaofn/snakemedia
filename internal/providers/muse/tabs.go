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

// errTabCapReached signals that ThreadsPerAccount dropped below active slots
// before this job claimed a tab, so it should be re-queued without blaming Chrome.
var errTabCapReached = errors.New("Số luồng trên tài khoản vừa giảm; xếp hàng lại.")

type tabSlot struct {
	targetID    string
	busy        bool
	jobID       string
	needsReload bool
}

type accountTabState struct {
	port  int
	slots []tabSlot
}

// tabPool manages a fixed-capacity pool of muse.ai tabs per account Chrome,
// opening tabs lazily as jobs arrive, reusing idle tabs across prompts, and
// shrinking excess tabs when ThreadsPerAccount decreases.
type tabPool struct {
	mu       sync.Mutex
	locks    map[string]chan struct{}
	accounts map[string]*accountTabState

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
		listTargets: browser.Targets,
		openTab:     browser.NewTab,
		navigate:    browser.Navigate,
		closeTab:    browser.CloseTab,
		sleep:       sleepContext,
		now:         time.Now,
	}
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
	return tp.hasOtherBusy(accountID, port, "")
}

func (tp *tabPool) hasOtherBusy(accountID string, port int, excludeTargetID string) bool {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	st := tp.accounts[accountID]
	if st == nil || st.port != port {
		return false
	}
	for _, s := range st.slots {
		if s.busy && s.targetID != excludeTargetID {
			return true
		}
	}
	return false
}

func (tp *tabPool) hasExcess(wantThreads int) bool {
	want := max(wantThreads, 1)
	tp.mu.Lock()
	defer tp.mu.Unlock()
	for _, st := range tp.accounts {
		if len(st.slots) > want {
			for _, s := range st.slots {
				if !s.busy {
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

// popExcessIdle removes excess idle slots from tail down to want (at least 1).
func (tp *tabPool) popExcessIdle(st *accountTabState, port, want int) []string {
	keep := max(want, 1)
	tp.mu.Lock()
	defer tp.mu.Unlock()
	if st.port != port || len(st.slots) <= keep {
		return nil
	}
	var drop []string
	for i := len(st.slots) - 1; i >= 0 && len(st.slots) > keep; i-- {
		if st.slots[i].busy {
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

// acquire assigns an idle muse.ai tab on port to jobID, opening one new tab
// when every existing tab is busy and fewer than wantThreads tabs exist.
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
	for i := range st.slots {
		if !st.slots[i].busy {
			st.slots[i].busy = true
			st.slots[i].jobID = jobID
			chosen := st.slots[i].targetID
			reload := st.slots[i].needsReload
			st.slots[i].needsReload = false
			tp.mu.Unlock()
			if excess := tp.popExcessIdle(st, port, want); len(excess) > 0 {
				tp.closeTargets(ctx, port, policy, excess)
			}
			if reload {
				if navTarget, ok := findTarget(ctx, tp, port, policy, chosen); ok {
					if err := tp.navigate(ctx, navTarget, port, musePage); err == nil {
						_ = tp.waitOnMuse(ctx, port, chosen, policy)
					}
				}
			}
			return chosen, nil
		}
	}
	if len(st.slots) >= want {
		tp.mu.Unlock()
		return "", errTabCapReached
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
	cur.slots = append(cur.slots, tabSlot{targetID: target.ID, busy: true, jobID: jobID})
	tp.mu.Unlock()
	return target.ID, nil
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
				return fmt.Errorf("Tab Muse mới điều hướng sang trang khác (%s)", t.URL)
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

// release returns targetID to the pool for reuse, or closes it when dirty (for
// non-sole tabs) or when total tabs on that Chrome exceed the current thread limit.
func (tp *tabPool) release(ctx context.Context, accountID string, port int, targetID string, dirty bool, wantThreads func() int, policy config.AccountConfig) {
	if targetID == "" || port <= 0 {
		return
	}
	want := max(wantThreads(), 1)
	needsClose := false
	tp.mu.Lock()
	st := tp.accounts[accountID]
	if st != nil && st.port == port {
		for i := range st.slots {
			if st.slots[i].targetID == targetID {
				st.slots[i].busy = false
				st.slots[i].jobID = ""
				if dirty {
					st.slots[i].needsReload = true
				}
				break
			}
		}
		needsClose = len(st.slots) > 1 && (dirty || len(st.slots) > want)
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

	want = max(wantThreads(), 1)
	keep := max(want, 1)
	remove := false
	tp.mu.Lock()
	st = tp.accounts[accountID]
	if st != nil && st.port == port && len(st.slots) > 1 && (dirty || len(st.slots) > keep) {
		for i := range st.slots {
			if st.slots[i].targetID == targetID && !st.slots[i].busy {
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

// reconcileIdle closes excess idle tabs when ThreadsPerAccount decreases.
func (tp *tabPool) reconcileIdle(ctx context.Context, wantThreads int, policy config.AccountConfig) {
	want := max(wantThreads, 1)
	tp.mu.Lock()
	var targets []string
	for accID, st := range tp.accounts {
		if len(st.slots) > want {
			for _, s := range st.slots {
				if !s.busy {
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
		var port int
		var drop []string
		if st != nil {
			port = st.port
		}
		tp.mu.Unlock()
		if st != nil && port > 0 {
			drop = tp.popExcessIdle(st, port, want)
		}
		if len(drop) > 0 {
			tp.closeTargets(reconcileCtx, port, policy, drop)
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
			if !s.busy && s.targetID != keepTargetID && len(kept)+remainingAfter >= 1 {
				drop = append(drop, s.targetID)
				continue
			}
			s.needsReload = true
			kept = append(kept, s)
		}
		st.slots = kept
	}
	tp.mu.Unlock()
	if len(drop) > 0 && ctx.Err() == nil {
		tp.closeTargets(ctx, port, policy, drop)
	}
}
