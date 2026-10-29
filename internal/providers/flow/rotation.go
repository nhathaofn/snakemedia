package flow

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"SnakeMedia/internal/config"
	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/providers/flow/chrome"
	"SnakeMedia/internal/providers/flow/session"
)

var errRotationSlotsFull = errors.New("mọi tài khoản phù hợp đang chạy đủ số yêu cầu đồng thời cho phép")

type rotationAccountState struct {
	active           int
	activityEpoch    uint64
	snapshotKnown    bool
	snapshotSet      bool
	snapshotAt       string
	snapshotCredits  int
	snapshotResetAt  string
	videoSpent       int
	videoReserved    int
	waitingForCredit bool
}

type videoAccountCandidate struct {
	account session.StoredAccount
	cost    int
}

type accountLease struct {
	provider *Provider
	id       string
	cost     int
	once     sync.Once
}

func (s *Provider) rotationStateLocked(acc session.StoredAccount) *rotationAccountState {
	if s.rotationAccounts == nil {
		s.rotationAccounts = make(map[string]*rotationAccountState)
	}
	state := s.rotationAccounts[acc.ID]
	if state == nil {
		state = &rotationAccountState{}
		s.rotationAccounts[acc.ID] = state
	}
	known := acc.Balance != nil
	credits := 0
	resetAt := ""
	if known {
		credits = acc.Balance.Credits
		resetAt = acc.Balance.ResetAt
	}
	if !state.snapshotSet || state.snapshotKnown != known || state.snapshotAt != acc.LastVerifiedAt || state.snapshotCredits != credits || state.snapshotResetAt != resetAt {
		if state.snapshotSet {
			if !state.snapshotKnown || !known || state.snapshotResetAt != resetAt || credits > state.snapshotCredits {
				state.videoSpent = 0
			} else {
				// A credit query may finish before Flow reflects the accepted job.
				// Carry forward only the amount missing from the refreshed balance.
				state.videoSpent = max(0, credits-(state.snapshotCredits-state.videoSpent))
			}
		}
		state.snapshotKnown = known
		state.snapshotAt = acc.LastVerifiedAt
		state.snapshotCredits = credits
		state.snapshotResetAt = resetAt
		state.snapshotSet = true
	}
	state.waitingForCredit = state.snapshotKnown && state.snapshotCredits-state.videoSpent-state.videoReserved <= 0
	return state
}

func (s *Provider) reserveLeastBusy(accounts []session.StoredAccount) (session.StoredAccount, *accountLease, error) {
	s.rotationMu.Lock()
	defer s.rotationMu.Unlock()
	return s.reserveLeastBusyLocked(accounts)
}

func (s *Provider) reserveLeastBusyLocked(accounts []session.StoredAccount) (session.StoredAccount, *accountLease, error) {
	if len(accounts) == 0 {
		return session.StoredAccount{}, nil, errors.New("không có tài khoản đủ điều kiện trong vòng xoay")
	}
	minActive := int(^uint(0) >> 1)
	for _, acc := range accounts {
		if active := s.rotationStateLocked(acc).active; active < s.accountConcurrency() && active < minActive {
			minActive = active
		}
	}
	if minActive == int(^uint(0)>>1) {
		return session.StoredAccount{}, nil, errRotationSlotsFull
	}
	start := int(s.imageCursor % uint64(len(accounts)))
	selected := -1
	for offset := range accounts {
		idx := (start + offset) % len(accounts)
		if s.rotationStateLocked(accounts[idx]).active == minActive {
			selected = idx
			break
		}
	}
	if selected < 0 {
		return session.StoredAccount{}, nil, errors.New("không chọn được tài khoản trong vòng xoay")
	}
	acc := accounts[selected]
	state := s.rotationStateLocked(acc)
	state.active++
	state.activityEpoch++
	s.imageCursor = uint64((selected + 1) % len(accounts))
	return acc, &accountLease{provider: s, id: acc.ID}, nil
}

func (s *Provider) acquireLeastBusy(ctx context.Context, accounts []session.StoredAccount) (session.StoredAccount, *accountLease, error) {
	idx, lease, err := s.waitForRotationSlot(ctx, func() (int, *accountLease, error) {
		account, lease, err := s.reserveLeastBusyLocked(accounts)
		if err != nil {
			return -1, nil, err
		}
		for i := range accounts {
			if accounts[i].ID == account.ID {
				return i, lease, nil
			}
		}
		return -1, nil, errors.New("không tìm được tài khoản đã chọn trong vòng xoay")
	})
	if err != nil {
		return session.StoredAccount{}, nil, err
	}
	return accounts[idx], lease, nil
}

func (s *Provider) acquireSpecificAccount(ctx context.Context, account session.StoredAccount) (*accountLease, error) {
	_, lease, err := s.waitForRotationSlot(ctx, func() (int, *accountLease, error) {
		state := s.rotationStateLocked(account)
		if state.active >= s.accountConcurrency() {
			return -1, nil, errRotationSlotsFull
		}
		state.active++
		state.activityEpoch++
		return 0, &accountLease{provider: s, id: account.ID}, nil
	})
	return lease, err
}

// waitForRotationSlot retries a reservation after any account lease is released.
// It keeps the lock while attempting to reserve so concurrent waiters cannot
// exceed the per-account limit when they wake together.
func (s *Provider) waitForRotationSlot(ctx context.Context, reserve func() (int, *accountLease, error)) (int, *accountLease, error) {
	for {
		if err := ctx.Err(); err != nil {
			return -1, nil, err
		}
		s.rotationMu.Lock()
		if err := ctx.Err(); err != nil {
			s.rotationMu.Unlock()
			return -1, nil, err
		}
		idx, lease, err := reserve()
		if !errors.Is(err, errRotationSlotsFull) {
			s.rotationMu.Unlock()
			return idx, lease, err
		}
		if s.rotationChanged == nil {
			s.rotationChanged = make(chan struct{})
		}
		changed := s.rotationChanged
		s.rotationMu.Unlock()
		select {
		case <-ctx.Done():
			return -1, nil, ctx.Err()
		case <-changed:
		}
	}
}

func (s *Provider) notifyRotationWaitersLocked() {
	if s.rotationChanged != nil {
		close(s.rotationChanged)
		s.rotationChanged = make(chan struct{})
	}
}

// reserveVideoCandidate uses smooth weighted round robin among accounts with
// the fewest active requests. Flow's current balance is the weight, while
// dispatched and in-flight costs are subtracted from each account's balance.
func (s *Provider) reserveVideoCandidate(candidates []videoAccountCandidate) (int, *accountLease, error) {
	s.rotationMu.Lock()
	defer s.rotationMu.Unlock()
	return s.reserveVideoCandidateLocked(candidates)
}

func (s *Provider) acquireVideoCandidate(ctx context.Context, candidates []videoAccountCandidate) (int, *accountLease, error) {
	return s.waitForRotationSlot(ctx, func() (int, *accountLease, error) {
		return s.reserveVideoCandidateLocked(candidates)
	})
}

func (s *Provider) reserveVideoCandidateLocked(candidates []videoAccountCandidate) (int, *accountLease, error) {
	if len(candidates) == 0 {
		return -1, nil, errors.New("không có tài khoản video đủ điều kiện")
	}

	states := make([]*rotationAccountState, len(candidates))
	remaining := make([]int, len(candidates))
	creditEligible := make([]bool, len(candidates))
	minActive := int(^uint(0) >> 1)
	for i, candidate := range candidates {
		state := s.rotationStateLocked(candidate.account)
		states[i] = state
		if !state.snapshotKnown {
			creditEligible[i] = true
			if state.active < s.accountConcurrency() && state.active < minActive {
				minActive = state.active
			}
			continue
		}
		left := state.snapshotCredits - state.videoSpent - state.videoReserved
		remaining[i] = left
		if state.waitingForCredit || (candidate.cost > 0 && left < candidate.cost) {
			continue
		}
		creditEligible[i] = true
		if state.active < s.accountConcurrency() && state.active < minActive {
			minActive = state.active
		}
	}
	if !slices.Contains(creditEligible, true) {
		return -1, nil, errors.New("không có tài khoản đủ credit cho yêu cầu video này; hãy làm mới số dư Flow")
	}
	if minActive == int(^uint(0)>>1) {
		return -1, nil, errRotationSlotsFull
	}
	knownTotal, knownCount := 0, 0
	for i, state := range states {
		if !creditEligible[i] || state.active != minActive || !state.snapshotKnown {
			continue
		}
		knownTotal += remaining[i]
		knownCount++
	}

	neutralWeight := 1.0
	if knownCount > 0 {
		neutralWeight = float64(knownTotal) / float64(knownCount)
	}
	weights := make([]float64, len(candidates))
	totalWeight := 0.0
	for i, candidate := range candidates {
		state := states[i]
		if !creditEligible[i] || state.active != minActive {
			if s.videoCurrent != nil {
				s.videoCurrent[candidate.account.ID] = 0
			}
			continue
		}
		weight := neutralWeight
		if state.snapshotKnown {
			weight = float64(remaining[i])
		}
		if weight <= 0 {
			creditEligible[i] = false
			continue
		}
		weights[i] = weight
		totalWeight += weight
	}
	if totalWeight <= 0 {
		return -1, nil, errors.New("không có tài khoản video đủ điều kiện")
	}
	if s.videoCurrent == nil {
		s.videoCurrent = make(map[string]float64)
	}
	selected := -1
	selectedCurrent := 0.0
	for i, candidate := range candidates {
		if !creditEligible[i] || states[i].active != minActive {
			continue
		}
		current := s.videoCurrent[candidate.account.ID] + weights[i]
		s.videoCurrent[candidate.account.ID] = current
		if selected < 0 || current > selectedCurrent {
			selected, selectedCurrent = i, current
		}
	}
	if selected < 0 {
		return -1, nil, errors.New("không có tài khoản video đủ điều kiện")
	}
	chosen := candidates[selected]
	s.videoCurrent[chosen.account.ID] -= totalWeight
	states[selected].active++
	states[selected].activityEpoch++
	if chosen.cost > 0 {
		states[selected].videoReserved += chosen.cost
	}
	states[selected].waitingForCredit = states[selected].snapshotKnown && states[selected].snapshotCredits-states[selected].videoSpent-states[selected].videoReserved <= 0
	return selected, &accountLease{provider: s, id: chosen.account.ID, cost: chosen.cost}, nil
}

func (l *accountLease) commitVideoSpend() {
	if l == nil || l.provider == nil {
		return
	}
	p := l.provider
	p.rotationMu.Lock()
	if l.cost <= 0 {
		p.rotationMu.Unlock()
		return
	}
	state := p.rotationAccounts[l.id]
	if state != nil {
		state.videoReserved -= l.cost
		if state.videoReserved < 0 {
			state.videoReserved = 0
		}
		state.videoSpent += l.cost
		state.activityEpoch++
		state.waitingForCredit = state.snapshotKnown && state.snapshotCredits-state.videoSpent-state.videoReserved <= 0
	}
	l.cost = 0
	p.rotationMu.Unlock()
}

func (s *Provider) recordVideoSpend(acc session.StoredAccount, cost int) {
	if cost <= 0 {
		return
	}
	s.rotationMu.Lock()
	state := s.rotationStateLocked(acc)
	state.videoSpent += cost
	state.activityEpoch++
	state.waitingForCredit = state.snapshotKnown && state.snapshotCredits-state.videoSpent-state.videoReserved <= 0
	s.rotationMu.Unlock()
}

// creditRefresh is the refresh state of one account: a query is running, and
// pending holds the context of one more asked for while it ran.
type creditRefresh struct {
	pending context.Context
}

// coalesceCreditRefresh runs refreshAccountCredits in the background, at most
// once at a time per account. Requests arriving while it runs fold into one
// trailing run, so a burst of generations costs two Chrome balance queries
// instead of one goroutine (and query) each.
func (s *Provider) coalesceCreditRefresh(ctx context.Context, id string) {
	ctx = context.WithoutCancel(ctx)
	s.creditRefreshMu.Lock()
	if s.creditRefreshes == nil {
		s.creditRefreshes = make(map[string]*creditRefresh)
	}
	if running, ok := s.creditRefreshes[id]; ok {
		running.pending = ctx
		s.creditRefreshMu.Unlock()
		return
	}
	state := &creditRefresh{}
	s.creditRefreshes[id] = state
	s.creditRefreshMu.Unlock()
	go func() {
		done := false
		defer func() {
			// A panic still frees the account for the next request.
			if !done {
				s.creditRefreshMu.Lock()
				delete(s.creditRefreshes, id)
				s.creditRefreshMu.Unlock()
			}
		}()
		defer logger.Recover("cập nhật credit Flow")
		for {
			s.refreshAccountCredits(ctx, id)
			s.creditRefreshMu.Lock()
			next := state.pending
			state.pending = nil
			if next == nil {
				// Ending under the lock: a request arriving now starts a new run.
				delete(s.creditRefreshes, id)
				done = true
			}
			s.creditRefreshMu.Unlock()
			if next == nil {
				return
			}
			ctx = next
		}
	}()
}

func (s *Provider) refreshAccountCredits(ctx context.Context, id string) {
	if s.accounts == nil || s.cfg == nil {
		return
	}
	lockValue, _ := s.balanceRefreshLocks.LoadOrStore(id, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	s.rotationMu.Lock()
	state := s.rotationAccounts[id]
	var activityEpoch uint64
	if state != nil {
		activityEpoch = state.activityEpoch
		if state.active > 1 || state.videoReserved > 0 {
			s.rotationMu.Unlock()
			return
		}
	}
	s.rotationMu.Unlock()

	accounts, err := s.accounts.GetConnectedStoredAccounts()
	if err != nil {
		logger.Get().Warn("cannot read account before refreshing Flow credits", "account", id, "error", err)
		return
	}
	var account *session.StoredAccount
	for i := range accounts {
		if accounts[i].ID == id {
			account = &accounts[i]
			break
		}
	}
	if account == nil {
		return
	}
	policy := s.cfg.Get().FlowPagePolicy()
	refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Duration(policy.RequestTimeoutSeconds)*time.Second)
	defer cancel()
	b, err := s.accounts.EnsureAccountHeadlessBrowser(refreshCtx, *account)
	if err != nil {
		logger.Get().Warn("cannot open Chrome to refresh Flow credits", "account", id, "error", err)
		return
	}
	credits, credentials, err := chrome.QueryCredits(refreshCtx, b.Port, account.Credentials, policy)
	if err != nil {
		logger.Get().Warn("cannot refresh Flow credits after generation", "account", id, "error", err)
		return
	}
	s.rotationMu.Lock()
	state = s.rotationAccounts[id]
	stale := (state != nil && (state.activityEpoch != activityEpoch || state.active > 1 || state.videoReserved > 0)) || (state == nil && activityEpoch != 0)
	s.rotationMu.Unlock()
	if stale {
		return
	}
	// The store write happens outside rotationMu so it never blocks rotation.
	// A spend starting in between meets a balance that does not show it yet,
	// which rotationStateLocked already carries forward.
	if err := s.accounts.UpdateBalance(id, credits, credentials); err != nil {
		logger.Get().Warn("cannot persist refreshed Flow credits", "account", id, "error", err)
	}
}

func (l *accountLease) release() {
	if l == nil || l.provider == nil {
		return
	}
	l.once.Do(func() {
		p := l.provider
		p.rotationMu.Lock()
		if state := p.rotationAccounts[l.id]; state != nil {
			if state.active > 0 {
				state.active--
			}
			if l.cost > 0 {
				state.videoReserved -= l.cost
				if state.videoReserved < 0 {
					state.videoReserved = 0
				}
				state.activityEpoch++
			}
			state.waitingForCredit = state.snapshotKnown && state.snapshotCredits-state.videoSpent-state.videoReserved <= 0
		}
		l.cost = 0
		p.notifyRotationWaitersLocked()
		p.rotationMu.Unlock()
	})
}

// accountConcurrency is how many requests one account may run at once.
func (s *Provider) accountConcurrency() int {
	if s.cfg == nil {
		return config.DefaultFlowConfig().MaxConcurrentRequestsPerAccount
	}
	return s.cfg.Get().Flow.MaxConcurrentRequestsPerAccount
}
