package muse

import (
	"errors"
	"fmt"
	"os"
	"slices"

	"SnakeMedia/internal/jobs"
	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/models"
)

// MuseLibraryItem is one file a finished Muse job produced. The library keeps
// its own record, so clearing or trimming the queue never empties it; removing
// an item forgets it and leaves the file on disk.
type MuseLibraryItem struct {
	ID string `json:"id"`
	// Job is the job that made the file; it may since have left the queue.
	Job       string      `json:"jobId"`
	Kind      MuseJobKind `json:"kind"`
	LocalURL  string      `json:"localUrl"`
	SavedPath string      `json:"savedPath,omitempty"`
	Prompt    string      `json:"prompt"`
	Label     string      `json:"label,omitempty"`
	AccountID string      `json:"accountId,omitempty"`
	CreatedAt string      `json:"createdAt"`
}

// JobID implements jobs.Record.
func (i MuseLibraryItem) JobID() string { return i.ID }

// Finished implements jobs.Record; the library has no cap, so nothing is trimmed.
func (i MuseLibraryItem) Finished() bool { return true }

// libraryItems lists the files of a succeeded job as library items, one per
// file, keyed by job and position so adding a job twice changes nothing.
func libraryItems(job MuseJob) []MuseLibraryItem {
	items := make([]MuseLibraryItem, 0, len(job.Files))
	for k, url := range job.Files {
		item := MuseLibraryItem{
			ID: fmt.Sprintf("%s-%d", job.ID, k+1), Job: job.ID, Kind: job.Kind, LocalURL: url,
			Prompt: job.Prompt, Label: job.Label, AccountID: job.AccountID, CreatedAt: job.FinishedAt,
		}
		if k < len(job.SavedFiles) {
			item.SavedPath = job.SavedFiles[k]
		}
		if item.CreatedAt == "" {
			item.CreatedAt = job.CreatedAt
		}
		items = append(items, item)
	}
	return items
}

// loadLibrary reads the library. The first time (no library file yet) it is
// filled from the succeeded jobs still in the queue, so files made before the
// library existed stay listed.
func (p *Provider) loadLibrary() {
	_, statErr := os.Stat(p.library.Path())
	if err := p.library.Load(); err != nil {
		logger.Get().Warn("muse library reset", "error", err)
		return
	}
	if !errors.Is(statErr, os.ErrNotExist) {
		return
	}
	var items []MuseLibraryItem
	for _, job := range p.jobs.list() {
		if job.Status == MuseJobSucceeded {
			items = append(items, libraryItems(job)...)
		}
	}
	if len(items) > 0 {
		if err := p.library.Add(items...); err != nil {
			logger.Get().Warn("muse library migration failed", "error", err)
		}
	}
}

// addToLibrary records the files of a job that just succeeded.
func (p *Provider) addToLibrary(job MuseJob) {
	var fresh []MuseLibraryItem
	for _, item := range libraryItems(job) {
		if _, ok := p.library.Get(item.ID); !ok {
			fresh = append(fresh, item)
		}
	}
	if len(fresh) == 0 {
		return
	}
	if err := p.library.Add(fresh...); err != nil {
		logger.Get().Warn("cannot add to muse library", "job", job.ID, "error", err)
	}
}

// ListLibrary returns every library item, newest first.
func (p *Provider) ListLibrary() models.Response[[]MuseLibraryItem] {
	items := p.library.List()
	slices.Reverse(items)
	return models.SuccessResponse(items)
}

// RemoveLibraryItems forgets the given items; their files stay on disk.
func (p *Provider) RemoveLibraryItems(ids []string) models.Response[int] {
	n, err := p.library.RemoveWhere(func(i MuseLibraryItem) bool { return slices.Contains(ids, i.ID) })
	if err != nil {
		return fail[int](err, models.CodeInternal)
	}
	return models.SuccessResponse(n)
}

// newLibrary returns the library journal kept next to the job journal.
func newLibrary(path string) *jobs.Journal[MuseLibraryItem] {
	return jobs.New(path, jobs.Options[MuseLibraryItem]{})
}
