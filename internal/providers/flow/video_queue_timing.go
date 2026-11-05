package flow

import (
	"encoding/json"
	"errors"
	"os"
	"time"

	"SnakeMedia/internal/jobs"
	"SnakeMedia/internal/logger"
	"SnakeMedia/pkg/utils"
)

func (q *videoQueue) loadTiming() {
	if q.timingPath == "" {
		return
	}
	data, err := os.ReadFile(q.timingPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Get().Warn("cannot read video queue timing", "error", err)
		}
		return
	}
	if err := jobs.CheckFormat(data); err != nil {
		q.timingErr = err
		logger.Get().Warn("video queue timing belongs to a newer build", "error", err)
		return
	}
	var td videoQueueTimingData
	if err := json.Unmarshal(data, &td); err != nil {
		logger.Get().Warn("cannot decode video queue timing", "error", err)
		return
	}
	now := q.now()
	q.timingMu.Lock()
	defer q.timingMu.Unlock()
	if td.NextLaunch != "" {
		if at, err := time.Parse(time.RFC3339, td.NextLaunch); err == nil && at.After(now) {
			q.nextLaunch = at
		}
	}
	for accID, ts := range td.AccountCooldown {
		if at, err := time.Parse(time.RFC3339, ts); err == nil && at.After(now) {
			q.accountCooldown[accID] = at
		}
	}
}

func (q *videoQueue) saveTimingLocked() error {
	if q.timingErr != nil {
		return q.timingErr
	}
	if q.timingPath == "" {
		return nil
	}
	td := videoQueueTimingData{
		FormatVersion:   jobs.FormatVersion,
		AccountCooldown: make(map[string]string),
	}
	now := q.now()
	if q.nextLaunch.After(now) {
		td.NextLaunch = q.nextLaunch.UTC().Format(time.RFC3339)
	}
	for accID, at := range q.accountCooldown {
		if at.After(now) {
			td.AccountCooldown[accID] = at.UTC().Format(time.RFC3339)
		}
	}
	data, err := json.Marshal(td)
	if err != nil {
		return err
	}
	return utils.WriteFileAtomic(q.timingPath, data, 0o644)
}

func (q *videoQueue) recoverHolds() {
	for _, t := range q.journal.List() {
		if t.RequestID == "" {
			continue
		}
		phase, found, phaseErr := q.requestPhase(t.RequestID)
		if phaseErr == nil && (!found || phase == generationComplete || phase == generationRejected) {
			continue
		}
		accountID := t.AccountID
		if job, err := q.s.loadGenerationJob(t.RequestID); err == nil && job.AccountID != "" {
			accountID = job.AccountID
			if t.AccountID != accountID {
				if _, err := q.journal.Update(t.ID, func(task *VideoTask) { task.AccountID = accountID }); err != nil {
					logger.Get().Warn("cannot recover video task account", "task", t.ID, "error", err)
				}
			}
		}
		if accountID != "" {
			q.s.HoldBatchVideoAccount(accountID, t.ID, t.RequestID)
		}
	}
}

func (q *videoQueue) settleAccountCooldown(accountID, taskID string) {
	q.s.rotationMu.Lock()
	defer q.s.rotationMu.Unlock()
	if hold, ok := q.s.batchVideoHolds[accountID]; ok && hold.TaskID != taskID {
		return
	}
	if accountID == "" {
		return
	}
	cfg := q.cfg()
	cooldown := q.now().Add(q.gap(cfg.VideoQueueGapMinSeconds, cfg.VideoQueueGapMaxSeconds))
	q.timingMu.Lock()
	if q.accountCooldown == nil {
		q.accountCooldown = make(map[string]time.Time)
	}
	q.accountCooldown[accountID] = cooldown
	if err := q.saveTimingLocked(); err != nil {
		logger.Get().Warn("cannot save video queue cooldown", "error", err)
	}
	q.timingMu.Unlock()
	q.s.releaseBatchVideoAccountLocked(accountID)
	q.trigger()
}

// releaseSettledHolds also covers failures before a generation journal exists.
// An unresolved request keeps its account even when the user cancels the wait.
func (q *videoQueue) releaseSettledHolds(id string) {
	t, ok := q.journal.Get(id)
	if !ok {
		return
	}
	if t.RequestID != "" {
		phase, found, err := q.requestPhase(t.RequestID)
		if err != nil || (found && phase != generationComplete && phase != generationRejected) {
			return
		}
	}
	for accountID, taskID := range q.s.BatchVideoHeldAccounts() {
		if taskID == id {
			q.settleAccountCooldown(accountID, id)
		}
	}
}
