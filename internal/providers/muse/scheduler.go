package muse

import (
	"slices"
	"time"

	"SnakeMedia/internal/config"
)

// pausedUntil returns when an account may work again, or the zero time when it
// may work now. A used-up weekly quota rests it until the reset (unless top-up
// tokens or KeepGoing allow more); Muse saying "limit" rests it for a while.
func pausedUntil(acc MuseAccount, cfg config.MuseConfig, now time.Time) time.Time {
	if acc.PausedUntil != "" {
		if until, err := time.Parse(time.RFC3339, acc.PausedUntil); err == nil && until.After(now) {
			return until
		}
	}
	q := acc.Quota
	if !q.weeklyOut() || cfg.KeepGoing {
		return time.Time{}
	}
	reset := q.resetsAt()
	if !reset.IsZero() && !reset.After(now) {
		return time.Time{} // past the reset, the stored numbers are stale
	}
	if cfg.UseTopup && q.topupLeft() {
		return time.Time{}
	}
	if reset.IsZero() {
		return now.Add(time.Duration(cfg.UsagePauseMinutes) * time.Minute)
	}
	return reset
}

// usable reports whether an account may take new jobs.
func usable(acc MuseAccount, cfg config.MuseConfig, now time.Time) bool {
	return acc.Enabled && acc.Status == MuseAccountSignedIn && pausedUntil(acc, cfg, now).IsZero()
}

// assignment pairs a job with the account that runs it.
type assignment struct {
	jobID     string
	accountID string
}

// plan chooses which jobs start now, and where. It is pure: running maps job
// IDs already being worked on to their account.
//
// A job already sent (it has a SessionID) resumes only on the account that
// sent it, because only that account can read the chat. Queued jobs go to
// usable accounts within the thread limits, oldest first.
func plan(jobs []MuseJob, accounts []MuseAccount, cfg config.MuseConfig, running map[string]string, pausedProjects map[string]bool, now time.Time) []assignment {
	busy := make(map[string]int, len(accounts))
	for _, accID := range running {
		busy[accID]++
	}
	total := len(running)
	active := func() int {
		n := 0
		for _, b := range busy {
			if b > 0 {
				n++
			}
		}
		return n
	}
	canTake := func(acc MuseAccount) bool {
		b := busy[acc.ID]
		return b < cfg.ThreadsPerAccount && total < cfg.TotalThreads && (b > 0 || active() < cfg.MaxAccounts)
	}
	var out []assignment
	take := func(job MuseJob, acc MuseAccount) {
		busy[acc.ID]++
		total++
		out = append(out, assignment{jobID: job.ID, accountID: acc.ID})
	}
	find := func(id string) (MuseAccount, bool) {
		i := slices.IndexFunc(accounts, func(a MuseAccount) bool { return a.ID == id })
		if i < 0 {
			return MuseAccount{}, false
		}
		return accounts[i], true
	}

	for _, job := range jobs {
		if _, ok := running[job.ID]; ok || job.Status != MuseJobProcessing || job.SessionID == "" {
			continue
		}
		// Quota does not matter here: the generation already happened.
		if acc, ok := find(job.AccountID); ok && acc.Enabled && acc.Status == MuseAccountSignedIn && canTake(acc) {
			take(job, acc)
		}
	}

	queued := make([]MuseJob, 0)
	for _, job := range jobs {
		if _, ok := running[job.ID]; !ok && job.Status == MuseJobQueued && !job.Held {
			if pausedProjects != nil && pausedProjects[job.ProjectID] {
				continue
			}
			queued = append(queued, job)
		}
	}
	slices.SortStableFunc(queued, func(a, b MuseJob) int {
		switch {
		case a.CreatedAt < b.CreatedAt:
			return -1
		case a.CreatedAt > b.CreatedAt:
			return 1
		}
		return 0
	})
	for _, job := range queued {
		var pool []MuseAccount
		for _, acc := range accounts {
			if usable(acc, cfg, now) && canTake(acc) {
				pool = append(pool, acc)
			}
		}
		var pin *MuseAccount
		if job.Thread != "" && job.PinAccount != "" {
			if acc, ok := find(job.PinAccount); ok && usable(acc, cfg, now) {
				pin = &acc
			}
		}
		if pin != nil {
			var pinnedPool []MuseAccount
			for _, acc := range pool {
				if acc.ID == pin.ID {
					pinnedPool = append(pinnedPool, acc)
				}
			}
			pool = pinnedPool
		}
		if len(pool) == 0 {
			if pin != nil {
				continue
			}
			break
		}
		if cfg.Mode != config.MuseModeSequential {
			slices.SortStableFunc(pool, func(a, b MuseAccount) int {
				if d := busy[a.ID] - busy[b.ID]; d != 0 {
					return d
				}
				if d := quotaUse(a) - quotaUse(b); d != 0 {
					if d < 0 {
						return -1
					}
					return 1
				}
				switch {
				case a.LastSentAt < b.LastSentAt:
					return -1
				case a.LastSentAt > b.LastSentAt:
					return 1
				}
				return 0
			})
		}
		take(job, pool[0])
	}
	return out
}

func quotaUse(acc MuseAccount) float64 {
	if acc.Quota == nil {
		return 0
	}
	return acc.Quota.PercentUsed
}
