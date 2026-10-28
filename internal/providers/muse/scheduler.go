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
		// No reset named: the numbers rest the account for a while after they
		// were read, then count as stale (the runner reads them again first).
		checked, err := time.Parse(time.RFC3339, q.CheckedAt)
		if err != nil {
			checked = now
		}
		if until := checked.Add(time.Duration(cfg.UsagePauseMinutes) * time.Minute); until.After(now) {
			return until
		}
		return time.Time{}
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
// every usable account (signed in, enabled, routed, not resting), up to
// ThreadsPerAccount each, oldest first.
func plan(jobs []MuseJob, accs []MuseAccount, cfg config.MuseConfig, running map[string]string, now time.Time) []assignment {
	busy := make(map[string]int, len(accs))
	for _, accID := range running {
		busy[accID]++
	}
	canTake := func(acc MuseAccount) bool { return busy[acc.ID] < cfg.ThreadsPerAccount }
	var out []assignment
	take := func(job MuseJob, acc MuseAccount) {
		busy[acc.ID]++
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
		if _, ok := running[job.ID]; !ok && job.Status == MuseJobQueued && !job.Held && !waitsForBoard(job) {
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
				return compareShare(a, b, cfg, now)
			})
		}
		take(job, pool[0])
	}
	return out
}

// compareShare orders two idle accounts for the next job so that each gets a
// share of the work in step with the quota it has left, instead of the one
// with the lowest usage taking every job until it catches up:
//   - an account still on its weekly usage goes before one spending its
//     additional tokens (paid), which only take jobs when no other can;
//   - an account that never sent goes first (more room first among those);
//   - otherwise the higher idle time × room left wins. Sending resets the idle
//     time, so an account with twice the room is chosen about twice as often,
//     and an account waiting out its send gap gives way to a rested one.
func compareShare(a, b MuseAccount, cfg config.MuseConfig, now time.Time) int {
	ta, ra := room(a, cfg)
	tb, rb := room(b, cfg)
	if ta != tb {
		return ta - tb
	}
	ia, na := idle(a, now)
	ib, nb := idle(b, now)
	switch {
	case na && nb:
		return cmpDesc(ra, rb)
	case na:
		return -1
	case nb:
		return 1
	}
	if d := cmpDesc(ia*ra, ib*rb); d != 0 {
		return d
	}
	return cmpDesc(ra, rb)
}

// room is how much an account has left, as a tier (0: weekly usage, 1:
// additional tokens) and a fraction in (0, 1]. An unread quota counts as full:
// the first job reads it.
func room(acc MuseAccount, cfg config.MuseConfig) (tier int, left float64) {
	q := acc.Quota
	if q == nil {
		return 0, 1
	}
	if !q.weeklyOut() {
		return 0, max(1-q.PercentUsed/100, minRoom)
	}
	if cfg.UseTopup && q.TopupTotal > 0 {
		return 1, max(min(q.TopupBalance/q.TopupTotal, 1), minRoom)
	}
	return 1, minRoom
}

// minRoom keeps an almost used-up account in turn, just rarely.
const minRoom = 0.01

// idle is how long ago the account last sent, in seconds; never reports an
// account that has not sent (or whose time cannot be read).
func idle(acc MuseAccount, now time.Time) (seconds float64, never bool) {
	last, err := time.Parse(time.RFC3339Nano, acc.LastSentAt)
	if err != nil {
		return 0, true
	}
	return max(now.Sub(last).Seconds(), 1), false
}

func cmpDesc(a, b float64) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	}
	return 0
}
