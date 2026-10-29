// Package jobs is the durable job journal providers share: every record of one
// queue in one JSON file, rewritten atomically when a record changes, indexed
// by ID, with a corrupt file moved aside instead of blocking the app.
//
// What a record means (its statuses, when it may be sent again) belongs to the
// provider; the journal only keeps records safe across crashes and restarts.
package jobs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"SnakeMedia/pkg/utils"
)

// Record is one job a journal keeps.
type Record interface {
	// JobID identifies the record within its journal.
	JobID() string
	// Finished reports a record that may be trimmed once the journal is full.
	Finished() bool
}

// ErrNotFound is returned for an ID the journal does not hold.
var ErrNotFound = errors.New("Tác vụ không tồn tại.")

// ErrCorrupt marks a journal that could not be read and was moved aside; the
// queue then starts empty and the user should be told.
var ErrCorrupt = errors.New("journal corrupt")

// Journal keeps the records of one queue, oldest first.
type Journal[T Record] struct {
	mu   sync.Mutex
	path string
	// limit caps the number of records; nil or a result below 1 means no cap.
	limit func() int
	// normalize fixes a record after it is read (nil slices and the like).
	normalize func(*T)
	// transient reports a change that need not reach disk on its own.
	transient func(before, after T) bool
	items     []T
	byID      map[string]int
	// blocked stops every write: the file belongs to a newer build.
	blocked error

	// Group commit: changed counts changes, saved is the last change on disk.
	// One writer at a time writes outside mu; changes made meanwhile wait and
	// share the next write, so concurrent updates cost one write, not one each.
	flushed  *sync.Cond
	changed  uint64
	saved    uint64
	writing  bool
	failedAt uint64
	failErr  error
}

// journalFile is the journal on disk (FormatVersion). Journals written before
// versioning are a bare JSON array of records.
type journalFile[T Record] struct {
	FormatVersion int `json:"formatVersion"`
	Records       []T `json:"records"`
}

// Options configure a journal.
type Options[T Record] struct {
	// Limit returns the record cap; only finished records are ever trimmed.
	Limit func() int
	// Normalize fixes a record after it is read from disk.
	Normalize func(*T)
	// Transient reports an Update whose change is only progress (shown while a
	// job runs, lost on a restart anyway): it stays in memory and reaches disk
	// with the record's next real change. Nil persists every Update.
	Transient func(before, after T) bool
}

// New returns a journal stored at path. Call Load before using it.
func New[T Record](path string, opts Options[T]) *Journal[T] {
	j := &Journal[T]{path: path, limit: opts.Limit, normalize: opts.Normalize, transient: opts.Transient, items: []T{}, byID: map[string]int{}}
	j.flushed = sync.NewCond(&j.mu)
	return j
}

// Load reads the journal. A missing file is an empty queue. A corrupt file is
// moved aside, the queue starts empty and the error wraps ErrCorrupt with
// where the old file now is, so the caller can tell the user. A file saved by
// a newer build stays untouched: the queue is empty and every write fails
// with ErrNewerFormat until that build (or a later one) runs again.
func (j *Journal[T]) Load() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	data, err := os.ReadFile(j.path)
	if errors.Is(err, os.ErrNotExist) {
		j.items = []T{}
		j.reindexLocked()
		return nil
	}
	if err != nil {
		return err
	}
	if err := CheckFormat(data); err != nil {
		j.items, j.blocked = []T{}, err
		j.reindexLocked()
		return fmt.Errorf("Không mở được danh sách tác vụ %s: %w", j.path, err)
	}
	var items []T
	if isObject(data) {
		var file journalFile[T]
		err = json.Unmarshal(data, &file)
		items = file.Records
	} else {
		err = json.Unmarshal(data, &items)
	}
	if err != nil {
		backup := fmt.Sprintf("%s.corrupt-%s", j.path, time.Now().Format("20060102-150405"))
		_ = os.Rename(j.path, backup)
		j.items = []T{}
		j.reindexLocked()
		return fmt.Errorf("Danh sách tác vụ hỏng, đã giữ tại %s (%v): %w", backup, err, ErrCorrupt)
	}
	if items == nil {
		items = []T{}
	}
	if j.normalize != nil {
		for i := range items {
			j.normalize(&items[i])
		}
	}
	j.items = items
	j.reindexLocked()
	return nil
}

// List returns a copy of every record, oldest first.
func (j *Journal[T]) List() []T {
	j.mu.Lock()
	defer j.mu.Unlock()
	return slices.Clone(j.items)
}

// Path is the journal file.
func (j *Journal[T]) Path() string { return j.path }

// Get returns the record with id.
func (j *Journal[T]) Get(id string) (T, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if i, ok := j.byID[id]; ok {
		return j.items[i], true
	}
	var zero T
	return zero, false
}

// Snapshot returns the records with the given IDs that still exist, read
// under one lock.
func (j *Journal[T]) Snapshot(ids []string) map[string]T {
	j.mu.Lock()
	defer j.mu.Unlock()
	res := make(map[string]T, len(ids))
	for _, id := range ids {
		if i, ok := j.byID[id]; ok {
			res[id] = j.items[i]
		}
	}
	return res
}

// Add appends records and persists the journal. Only Add trims: the oldest
// finished records go while the journal is over its cap, so a record being
// updated never moves or disappears.
func (j *Journal[T]) Add(records ...T) error {
	_, err := j.AddTrimming(records...)
	return err
}

// AddTrimming is Add that also returns the IDs of the finished records trimmed
// to make room, so listeners can drop them without reloading the queue.
func (j *Journal[T]) AddTrimming(records ...T) ([]string, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, r := range records {
		if _, dup := j.byID[r.JobID()]; dup {
			return nil, fmt.Errorf("tác vụ %s đã có", r.JobID())
		}
	}
	for _, r := range records {
		j.items = append(j.items, r)
		j.byID[r.JobID()] = len(j.items) - 1
	}
	trimmed := j.trimLocked()
	return trimmed, j.saveLocked()
}

// Update applies change to a record and persists the journal, unless the
// journal's Transient option calls the change progress only. The returned
// record is a copy taken before the write.
func (j *Journal[T]) Update(id string, change func(*T)) (T, error) {
	return j.update(id, change, true)
}

// UpdateTransient applies change in memory only: progress text and similar
// values a restart may lose. They reach disk with the record's next Update.
func (j *Journal[T]) UpdateTransient(id string, change func(*T)) (T, error) {
	return j.update(id, change, false)
}

func (j *Journal[T]) update(id string, change func(*T), persist bool) (T, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	i, ok := j.byID[id]
	if !ok {
		var zero T
		return zero, ErrNotFound
	}
	before := j.items[i]
	change(&j.items[i])
	record := j.items[i]
	if !persist || (j.transient != nil && j.transient(before, record)) {
		return record, nil
	}
	return record, j.saveLocked()
}

// RemoveWhere deletes every record drop selects and returns how many went.
func (j *Journal[T]) RemoveWhere(drop func(T) bool) (int, error) {
	ids, err := j.RemoveWhereIDs(drop)
	return len(ids), err
}

// RemoveWhereIDs is RemoveWhere returning the IDs of the removed records.
func (j *Journal[T]) RemoveWhereIDs(drop func(T) bool) ([]string, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	var removed []string
	j.items = slices.DeleteFunc(j.items, func(r T) bool {
		if drop(r) {
			removed = append(removed, r.JobID())
			return true
		}
		return false
	})
	if len(removed) == 0 {
		return nil, nil
	}
	j.reindexLocked()
	return removed, j.saveLocked()
}

// Remove deletes the record with id.
func (j *Journal[T]) Remove(id string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	i, ok := j.byID[id]
	if !ok {
		return ErrNotFound
	}
	j.items = slices.Delete(j.items, i, i+1)
	j.reindexLocked()
	return j.saveLocked()
}

func (j *Journal[T]) reindexLocked() {
	j.byID = make(map[string]int, len(j.items))
	for i, r := range j.items {
		j.byID[r.JobID()] = i
	}
}

func (j *Journal[T]) trimLocked() (trimmed []string) {
	if j.limit == nil {
		return nil
	}
	extra := len(j.items) - j.limit()
	if j.limit() < 1 || extra <= 0 {
		return nil
	}
	kept := make([]T, 0, len(j.items))
	for _, r := range j.items {
		if extra > 0 && r.Finished() {
			extra--
			trimmed = append(trimmed, r.JobID())
			continue
		}
		kept = append(kept, r)
	}
	j.items = kept
	j.reindexLocked()
	return trimmed
}

// saveLocked returns once the journal on disk holds every change made so far
// (it may be written by another caller's write that started later). mu is
// released while the file is written, so readers are not held up by the disk.
func (j *Journal[T]) saveLocked() error {
	if j.blocked != nil {
		return j.blocked
	}
	j.changed++
	mine := j.changed
	for j.saved < mine {
		if j.failedAt >= mine {
			return j.failErr
		}
		if j.writing {
			j.flushed.Wait()
			continue
		}
		data, err := json.Marshal(journalFile[T]{FormatVersion: FormatVersion, Records: j.items})
		if err != nil {
			return err
		}
		upTo := j.changed
		j.writing = true
		j.mu.Unlock()
		err = j.write(data)
		j.mu.Lock()
		j.writing = false
		if err != nil {
			j.failedAt, j.failErr = upTo, err
		} else {
			j.saved = upTo
		}
		j.flushed.Broadcast()
		if err != nil {
			return err
		}
	}
	return nil
}

func (j *Journal[T]) write(data []byte) error {
	if err := os.MkdirAll(filepath.Dir(j.path), 0o700); err != nil {
		return err
	}
	return utils.WriteFileAtomic(j.path, data, 0o600)
}
