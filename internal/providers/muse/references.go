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
	return p.sweepMedia(referencePrefix, usedReferences(p.jobs.list()), p.readMediaDir())
}

// sweepUnused is sweepReferences then sweepAssetImages over one read of the
// journal and of the media folder.
func (p *Provider) sweepUnused() {
	jobs := p.jobs.list()
	entries := p.readMediaDir()
	p.sweepMedia(referencePrefix, usedReferences(jobs), entries)
	p.sweepMedia(assetPrefix, p.usedAssetImages(jobs), entries)
}

// usedReferences is the set of reference images and source videos jobs use.
func usedReferences(jobs []MuseJob) map[string]bool {
	used := map[string]bool{}
	for _, job := range jobs {
		for _, ref := range job.References {
			used[ref] = true
		}
		if job.SourceVideo != "" {
			used[job.SourceVideo] = true
		}
	}
	return used
}

// readMediaDir lists the media folder; nil when it cannot be read.
func (p *Provider) readMediaDir() []os.DirEntry {
	entries, err := os.ReadDir(p.media.Dir())
	if err != nil {
		return nil
	}
	return entries
}

// sweepMedia deletes the media files among entries named with prefix whose
// local URL is not in used and that are older than referenceRetention.
func (p *Provider) sweepMedia(prefix string, used map[string]bool, entries []os.DirEntry) int {
	dir := p.media.Dir()
	cutoff := p.now().Add(-referenceRetention)
	removed := 0
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || !strings.HasPrefix(name, prefix) || used[media.Route+name] {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			logger.Get().Warn("muse media not removed", "file", name, "error", err)
			continue
		}
		removed++
	}
	if removed > 0 {
		logger.Get().Info("muse unused media removed", "prefix", prefix, "count", removed)
	}
	return removed
}
