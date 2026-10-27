package muse

import (
	"slices"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/config"
)

// pausedUntil returns when an account may work again, or the zero time when it
// may work now. A used-up weekly usage rests it until the reset, unless it may
// go on with its additional tokens; Muse saying "limit" rests it for a while.
func pausedUntil(acc MuseAccount, cfg config.MuseConfig, now time.Time) time.Time {
	if acc.PausedUntil != "" {
		if until, err := time.Parse(time.RFC3339, acc.PausedUntil); err == nil && until.After(now) {
			return until
		}
	}
	q := acc.Quota
	if !q.weeklyOut() {
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
	return acc.Enabled && acc.Status == accounts.StatusSignedIn && acc.NoRoute == "" && pausedUntil(acc, cfg, now).IsZero()
}

// due leaves out the jobs held back until a later time (NextCheckAt), so plan
// never starts them early. An unreadable hold does not hold the job.
func due(jobs []MuseJob, now time.Time) []MuseJob {
	out := make([]MuseJob, 0, len(jobs))
	for _, j := range jobs {
		if j.NextCheckAt != "" {
			if t, err := time.Parse(time.RFC3339Nano, j.NextCheckAt); err == nil && t.After(now) {
				continue
			}
		}
		out = append(out, j)
	}
	return out
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
func plan(jobs []MuseJob, accs []MuseAccount, cfg config.MuseConfig, running map[string]string, now time.Time) []assignment {
	busy := make(map[string]int, len(accs))
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
		i := slices.IndexFunc(accs, func(a MuseAccount) bool { return a.ID == id })
		if i < 0 {
			return MuseAccount{}, false
		}
		return accs[i], true
	}

	for _, job := range jobs {
		if _, ok := running[job.ID]; ok || job.Status != MuseJobProcessing || job.SessionID == "" {
			continue
		}
		// Quota does not matter here: the generation already happened.
		if acc, ok := find(job.AccountID); ok && acc.Enabled && acc.Status == accounts.StatusSignedIn && acc.NoRoute == "" && canTake(acc) {
			take(job, acc)
		}
	}

	queued := make([]MuseJob, 0)
	for _, job := range jobs {
		if _, ok := running[job.ID]; !ok && job.Status == MuseJobQueued && !job.Held {
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
		for _, acc := range accs {
			if usable(acc, cfg, now) && canTake(acc) {
				pool = append(pool, acc)
			}
		}
		if len(pool) == 0 {
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
