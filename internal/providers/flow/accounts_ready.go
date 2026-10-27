package flow

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"SnakeMedia/internal/providers/flow/session"
)

// errNoFlowAccount is returned when no connected account passes the cookie check.
var errNoFlowAccount = errors.New("không có tài khoản Google Flow nào có cookie hợp lệ. Vui lòng đăng nhập hoặc làm mới tài khoản.")

// readyCandidate reserves one of candidates with reserve, holds its profile
// (its headless browser must outlive the work) and syncs its Flow session from
// Chrome. Accounts signed out of Google are skipped so the work goes on while
// another one is still connected. release frees the profile and the lease;
// lease is returned for work that commits a spend on it.
func readyCandidate[C any](
	ctx context.Context,
	s *Provider,
	candidates []C,
	account func(C) session.StoredAccount,
	reserve func(context.Context, []C) (int, *accountLease, error),
) (chosen C, synced session.StoredAccount, lease *accountLease, release func(), err error) {
	var skipped []string
	remaining := slices.Clone(candidates)
	for len(remaining) > 0 {
		idx, next, err := reserve(ctx, remaining)
		if err != nil {
			return chosen, synced, nil, nil, err
		}
		acc := account(remaining[idx])
		fresh, releaseNext, err := s.holdAndSync(ctx, acc, next)
		if err == nil {
			return remaining[idx], fresh, next, releaseNext, nil
		}
		if !errors.Is(err, session.ErrSessionSignedOut) {
			return chosen, synced, nil, nil, fmt.Errorf("không đồng bộ được phiên Flow của %s: %w", acc.Email, err)
		}
		skipped = append(skipped, acc.Email)
		remaining = slices.Delete(remaining, idx, idx+1)
	}
	if len(skipped) == 0 {
		return chosen, synced, nil, nil, errNoFlowAccount
	}
	return chosen, synced, nil, nil, fmt.Errorf("các tài khoản đã đăng xuất khỏi Google (%s); hãy đăng nhập lại ở mục Tài khoản", strings.Join(skipped, ", "))
}

// readySpecific leases one given account, holds its profile and syncs its
// session; used for work that must stay on the account that owns it.
func (s *Provider) readySpecific(ctx context.Context, acc session.StoredAccount) (session.StoredAccount, func(), error) {
	lease, err := s.acquireSpecificAccount(ctx, acc)
	if err != nil {
		return session.StoredAccount{}, nil, err
	}
	return s.holdAndSync(ctx, acc, lease)
}

// holdAndSync holds acc's profile and reads its session fresh from Chrome; on
// failure both the profile and lease are let go.
func (s *Provider) holdAndSync(ctx context.Context, acc session.StoredAccount, lease *accountLease) (session.StoredAccount, func(), error) {
	releaseProfile := s.accounts.AcquireProfile(acc.ProfileDir)
	synced, err := s.syncSession(ctx, acc)
	if err != nil {
		releaseProfile()
		lease.release()
		return session.StoredAccount{}, nil, err
	}
	return synced, func() {
		releaseProfile()
		lease.release()
	}, nil
}

// leastBusy adapts acquireLeastBusy to readyCandidate's index form.
func (s *Provider) leastBusy(ctx context.Context, accounts []session.StoredAccount) (int, *accountLease, error) {
	acc, lease, err := s.acquireLeastBusy(ctx, accounts)
	if err != nil {
		return -1, nil, err
	}
	return slices.IndexFunc(accounts, func(a session.StoredAccount) bool { return a.ID == acc.ID }), lease, nil
}

func sameAccount(acc session.StoredAccount) session.StoredAccount { return acc }

func candidateAccount(c videoAccountCandidate) session.StoredAccount { return c.account }
