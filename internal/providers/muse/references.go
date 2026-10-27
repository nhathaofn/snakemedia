package muse

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/media"
)

// referencePrefix names the media files ImportReferences creates; nothing
// else in the media folder is ever removed by the reference clean-up.
const referencePrefix = "muse-ref-"

// referenceRetention keeps an unused reference this long after it was
// imported: the create form may still hold it for the next send.
const referenceRetention = 7 * 24 * time.Hour

// sweepReferences deletes imported reference images that no job in the
// journal uses and that are older than referenceRetention. It returns how
// many files went.
func (p *Provider) sweepReferences() int {
	dir := p.media.Dir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	used := map[string]bool{}
	for _, job := range p.jobs.list() {
		for _, ref := range job.References {
			used[strings.TrimPrefix(ref, media.Route)] = true
		}
	}
	cutoff := p.now().Add(-referenceRetention)
	removed := 0
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || !strings.HasPrefix(name, referencePrefix) || used[name] {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			logger.Get().Warn("muse reference not removed", "file", name, "error", err)
			continue
		}
		removed++
	}
	if removed > 0 {
		logger.Get().Info("muse unused references removed", "count", removed)
	}
	return removed
}
