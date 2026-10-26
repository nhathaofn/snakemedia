package flow

import (
	"context"
	"errors"
	"testing"
	"time"

	"SnakeMedia/internal/providers/flow/chrome"
	"SnakeMedia/internal/providers/flow/session"
)

func TestLeastBusyRotationBalancesConcurrentRequests(t *testing.T) {
	provider := &Provider{}
	accounts := []session.StoredAccount{
		{AccountProfile: session.AccountProfile{ID: "first"}},
		{AccountProfile: session.AccountProfile{ID: "second"}},
	}
	first, firstLease, err := provider.reserveLeastBusy(accounts)
	if err != nil {
		t.Fatal(err)
	}
	second, secondLease, err := provider.reserveLeastBusy(accounts)
	if err != nil {
		t.Fatal(err)
	}
	third, thirdLease, err := provider.reserveLeastBusy(accounts)
	if err != nil {
		t.Fatal(err)
	}
	defer firstLease.release()
	defer secondLease.release()
	defer thirdLease.release()

	if first.ID != "first" || second.ID != "second" || third.ID != "first" {
		t.Fatalf("least-busy choices = %q, %q, %q; want first, second, first", first.ID, second.ID, third.ID)
	}
}

func TestAccountRotationQueuesAtTwoConcurrentRequests(t *testing.T) {
	provider := &Provider{}
	account := session.StoredAccount{AccountProfile: session.AccountProfile{ID: "only"}}
	accounts := []session.StoredAccount{account}
	first, firstLease, err := provider.acquireLeastBusy(context.Background(), accounts)
	if err != nil {
		t.Fatal(err)
	}
	second, secondLease, err := provider.acquireLeastBusy(context.Background(), accounts)
	if err != nil {
		t.Fatal(err)
	}
	defer secondLease.release()
	if first.ID != account.ID || second.ID != account.ID {
		t.Fatalf("selected accounts = %q, %q; want the only account", first.ID, second.ID)
	}
	if _, _, err := provider.reserveLeastBusy(accounts); !errors.Is(err, errRotationSlotsFull) {
		t.Fatalf("third same-account request error = %v; want a full-slot result", err)
	}

	type reservation struct {
		lease *accountLease
		err   error
	}
	queued := make(chan reservation, 1)
	go func() {
		_, lease, err := provider.acquireLeastBusy(context.Background(), accounts)
		queued <- reservation{lease: lease, err: err}
	}()
	select {
	case result := <-queued:
		if result.lease != nil {
			result.lease.release()
		}
		t.Fatal("third same-account request was not queued")
	case <-time.After(25 * time.Millisecond):
	}

	firstLease.release()
	select {
	case result := <-queued:
		if result.err != nil {
			t.Fatalf("queued request failed after a slot opened: %v", result.err)
		}
		if result.lease == nil {
			t.Fatal("queued request acquired no lease")
		}
		defer result.lease.release()
	case <-time.After(time.Second):
		t.Fatal("queued request was not resumed when an account slot opened")
	}

	provider.rotationMu.Lock()
	active := provider.rotationAccounts[account.ID].active
	provider.rotationMu.Unlock()
	if active != provider.accountConcurrency() {
		t.Fatalf("active requests = %d; want cap %d", active, provider.accountConcurrency())
	}
}

func TestVideoAndImageShareAccountConcurrencyLimit(t *testing.T) {
	provider := &Provider{}
	account := session.StoredAccount{AccountProfile: session.AccountProfile{ID: "shared", Balance: &chrome.FlowCredits{Credits: 10}}}
	videoCandidates := []videoAccountCandidate{{account: account, cost: 1}}
	_, videoLease, err := provider.acquireVideoCandidate(context.Background(), videoCandidates)
	if err != nil {
		t.Fatal(err)
	}
	defer videoLease.release()
	_, imageLease, err := provider.acquireLeastBusy(context.Background(), []session.StoredAccount{account})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := provider.reserveVideoCandidate(videoCandidates); !errors.Is(err, errRotationSlotsFull) {
		imageLease.release()
		t.Fatalf("video request error at the shared limit = %v; want a full-slot result", err)
	}

	queued := make(chan struct {
		lease *accountLease
		err   error
	}, 1)
	go func() {
		_, lease, err := provider.acquireVideoCandidate(context.Background(), videoCandidates)
		queued <- struct {
			lease *accountLease
			err   error
		}{lease: lease, err: err}
	}()
	select {
	case result := <-queued:
		if result.lease != nil {
			result.lease.release()
		}
		imageLease.release()
		t.Fatal("video request bypassed the shared image/video limit")
	case <-time.After(25 * time.Millisecond):
	}

	imageLease.release()
	select {
	case result := <-queued:
		if result.err != nil {
			t.Fatalf("queued video request failed after an image slot opened: %v", result.err)
		}
		if result.lease == nil {
			t.Fatal("queued video request acquired no lease")
		}
		defer result.lease.release()
	case <-time.After(time.Second):
		t.Fatal("queued video request was not resumed after the image request released its slot")
	}
}

func TestVideoRotationExcludesCreditExhaustedAccountButKeepsImageRotationAvailable(t *testing.T) {
	provider := &Provider{}
	accounts := []session.StoredAccount{
		{AccountProfile: session.AccountProfile{ID: "exhausted", Balance: &chrome.FlowCredits{Credits: 0}}},
		{AccountProfile: session.AccountProfile{ID: "available", Balance: &chrome.FlowCredits{Credits: 10}}},
	}
	candidates := []videoAccountCandidate{
		{account: accounts[0], cost: 1},
		{account: accounts[1], cost: 1},
	}
	idx, lease, err := provider.reserveVideoCandidate(candidates)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.release()
	if got := candidates[idx].account.ID; got != "available" {
		t.Fatalf("video selected %q; want account with credit", got)
	}

	selected, imageLease, err := provider.reserveLeastBusy(accounts)
	if err != nil {
		t.Fatal(err)
	}
	defer imageLease.release()
	if selected.ID == "" {
		t.Fatal("image rotation did not select an account")
	}
	provider.rotationMu.Lock()
	waitingForCredit := provider.rotationAccounts["exhausted"].waitingForCredit
	provider.rotationMu.Unlock()
	if !waitingForCredit {
		t.Fatal("credit-exhausted account was not marked as waiting for credit")
	}

	if _, _, err := provider.reserveVideoCandidate([]videoAccountCandidate{{account: accounts[0], cost: 1}}); err == nil || errors.Is(err, errRotationSlotsFull) {
		t.Fatalf("credit-exhausted account should be rejected for video, got %v", err)
	}
}

func TestVideoRotationUsesDynamicCreditWeightsAndReservesCost(t *testing.T) {
	provider := &Provider{}
	candidates := []videoAccountCandidate{
		{account: session.StoredAccount{AccountProfile: session.AccountProfile{ID: "high", Balance: &chrome.FlowCredits{Credits: 100}}}, cost: 10},
		{account: session.StoredAccount{AccountProfile: session.AccountProfile{ID: "low", Balance: &chrome.FlowCredits{Credits: 20}}}, cost: 10},
	}
	used := map[string]int{}
	for range 6 {
		idx, lease, err := provider.reserveVideoCandidate(candidates)
		if err != nil {
			t.Fatal(err)
		}
		used[candidates[idx].account.ID]++
		lease.commitVideoSpend()
		lease.release()
	}
	if used["high"] <= used["low"] || used["low"] == 0 {
		t.Fatalf("credit-weighted choices = %+v; want both accounts used and high-credit account used more", used)
	}

	for range 6 {
		idx, lease, err := provider.reserveVideoCandidate(candidates)
		if err != nil {
			t.Fatalf("remaining affordable generations were rejected: %v", err)
		}
		lease.commitVideoSpend()
		lease.release()
		used[candidates[idx].account.ID]++
	}
	if _, _, err := provider.reserveVideoCandidate(candidates); err == nil {
		t.Fatal("rotation selected an account after its estimated credits were exhausted")
	}
}
