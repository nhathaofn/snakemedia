package flow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"SnakeMedia/internal/logger"
	"github.com/google/uuid"
)

// journalCache keeps decoded job journals in memory so that listing pending
// jobs reads only the directory, not every file. An entry is reused while its
// file's modification time and size are unchanged, and dropped when the app
// saves that journal or the file disappears. Cached values are shared: callers
// treat them as read-only and save a changed copy.
type journalCache struct {
	mu     sync.Mutex
	byKind map[string]map[string]cachedJournal
}

type cachedJournal struct {
	modified time.Time
	size     int64
	value    any
}

// journal is one listed job journal.
type journal[T any] struct {
	id       string
	modified time.Time
	value    T
}

func (c *journalCache) lookup(kind, id string, info os.FileInfo) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.byKind[kind][id]
	if !ok || !entry.modified.Equal(info.ModTime()) || entry.size != info.Size() {
		return nil, false
	}
	return entry.value, true
}

func (c *journalCache) store(kind, id string, info os.FileInfo, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byKind == nil {
		c.byKind = map[string]map[string]cachedJournal{}
	}
	if c.byKind[kind] == nil {
		c.byKind[kind] = map[string]cachedJournal{}
	}
	c.byKind[kind][id] = cachedJournal{modified: info.ModTime(), size: info.Size(), value: value}
}

// forget drops a journal the app is rewriting, so the next listing reads it
// again even if the new file has the same time stamp and size.
func (c *journalCache) forget(kind, id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.byKind[kind], id)
}

// retain drops cached journals whose files are gone.
func (c *journalCache) retain(kind string, present map[string]bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id := range c.byKind[kind] {
		if !present[id] {
			delete(c.byKind[kind], id)
		}
	}
}

// listJournals returns every journal of kind, decoding only files that changed
// since the last listing. A journal that cannot be decoded or fails check is
// moved aside as <id>.json.corrupt and logged once; a file that cannot be read
// right now is skipped and tried again on the next listing.
func listJournals[T any](s *Provider, kind string, check func(id string, value T) error) ([]journal[T], error) {
	dir := s.stateDir(kind)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		s.journals.retain(kind, nil)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	present := make(map[string]bool, len(entries))
	out := make([]journal[T], 0, len(entries))
	for _, entry := range entries {
		id, ok := strings.CutSuffix(entry.Name(), ".json")
		if entry.IsDir() || !ok || uuid.Validate(id) != nil {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		present[id] = true
		if cached, ok := s.journals.lookup(kind, id, info); ok {
			out = append(out, journal[T]{id: id, modified: info.ModTime(), value: cached.(T)})
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			logger.Get().Warn("skipping unreadable job journal", "kind", kind, "job", id, "error", err)
			continue
		}
		var value T
		if err = json.Unmarshal(data, &value); err == nil {
			err = check(id, value)
		}
		if err != nil {
			quarantineJournal(path, kind, id, err)
			delete(present, id)
			continue
		}
		s.journals.store(kind, id, info, value)
		out = append(out, journal[T]{id: id, modified: info.ModTime(), value: value})
	}
	s.journals.retain(kind, present)
	return out, nil
}

// quarantineJournal moves a broken journal out of the listing, keeping it for
// diagnosis.
func quarantineJournal(path, kind, id string, cause error) {
	target := path + ".corrupt"
	if _, err := os.Stat(target); err == nil {
		target = fmt.Sprintf("%s.corrupt-%s", path, time.Now().Format("20060102-150405"))
	}
	if err := os.Rename(path, target); err != nil {
		logger.Get().Warn("cannot move broken job journal aside", "kind", kind, "job", id, "error", err)
		return
	}
	logger.Get().Warn("broken job journal moved aside", "kind", kind, "job", id, "file", target, "error", cause)
}
