package muse

import (
	"fmt"
	"os"
	"strings"

	"SnakeMedia/internal/events"
	"SnakeMedia/internal/media"
	"SnakeMedia/internal/models"
)

// writeOutputs retries only missing copies. Each successful copy is journaled
// before the next one, so a partial failure can resume without duplicating it.
// The caller holds saveMu. SavedFiles stays a compact list for older callers.
func (p *Provider) writeOutputs(job MuseJob, files []string) (MuseJob, error) {
	if job.OutputDir == "" || len(files) == 0 {
		return job, nil
	}
	paths := make(map[string]string)
	for url, path := range job.OutputPaths {
		paths[url] = path
	}
	// Before OutputPaths existed only fully saved lists can be mapped safely.
	if job.OutputPaths == nil && len(job.SavedFiles) == len(files) {
		for i, url := range files {
			paths[url] = job.SavedFiles[i]
		}
	}
	var failures []string
	for _, url := range files {
		current, ok := p.jobs.get(job.ID)
		if !ok || current.Status == MuseJobCancelled {
			return job, nil
		}
		if path := paths[url]; path != "" {
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
				continue
			}
			delete(paths, url)
		}
		path, err := p.media.SaveTo(url, job.OutputDir, media.OutputName(job.Index, "muse"))
		if err != nil {
			failures = append(failures, err.Error())
		} else {
			paths[url] = path
		}
		job, err = p.updateJob(job.ID, func(j *MuseJob) {
			j.OutputPaths = make(map[string]string, len(paths))
			j.SavedFiles = []string{}
			for _, source := range files {
				if saved := paths[source]; saved != "" {
					j.OutputPaths[source] = saved
					j.SavedFiles = append(j.SavedFiles, saved)
				}
			}
			j.SaveError = strings.Join(failures, "; ")
		})
		if err != nil {
			return job, err
		}
	}
	// Clear a previous error even when every existing copy was already present.
	return p.updateJob(job.ID, func(j *MuseJob) { j.SaveError = strings.Join(failures, "; ") })
}

// RetrySave never calls Muse: it copies the files already in the media store.
func (p *Provider) RetrySave(id string) models.Response[MuseJob] {
	p.saveMu.Lock()
	defer p.saveMu.Unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	job, ok := p.jobs.get(id)
	if !ok {
		return fail[MuseJob](errJobNotFound, models.CodeNotFound)
	}
	if _, active := p.running[id]; active || !job.finished() || job.Status == MuseJobCancelled || job.OutputDir == "" || len(job.Files) == 0 {
		return fail[MuseJob](fmt.Errorf("Tác vụ chưa sẵn sàng để lưu lại."), models.CodeInvalidArgument)
	}
	job, err := p.writeOutputs(job, job.Files)
	if err != nil {
		return fail[MuseJob](err, models.CodeInternal)
	}
	p.syncLibraryPaths(job)
	p.emit(events.EventMuseJobUpdated, job)
	return models.SuccessResponse(job)
}

func (p *Provider) syncLibraryPaths(job MuseJob) {
	for _, item := range libraryItems(job) {
		if _, ok := p.library.Get(item.ID); ok {
			_, _ = p.library.Update(item.ID, func(stored *MuseLibraryItem) { stored.SavedPath = item.SavedPath })
		}
	}
}
