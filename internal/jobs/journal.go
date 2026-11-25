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
	active    map[string]bool
	keepID    func(T) bool
	retired   []string
	retiredID map[string]bool
	// blocked stops every write: the file belongs to a newer build.
	blocked error

	updatesMu sync.Mutex
	updateQ   []*recordUpdate[T]
	updating  bool
	updated   *sync.Cond
}

type recordUpdate[T Record] struct {
	id     string
	change func(*T)
	result T
	err    error
	panic  any
	done   bool // guarded by updatesMu
}

// journalFile is the journal on disk (FormatVersion). Journals written before
// versioning are a bare JSON array of records.
type journalFile[T Record] struct {
	FormatVersion int      `json:"formatVersion"`
	Records       []T      `json:"records"`
	RetiredIDs    []string `json:"retiredIds,omitempty"`
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
	// KeepID retains a removed record's ID within the same disk transaction.
	KeepID func(T) bool
}

// New returns a journal stored at path. Call Load before using it.
func New[T Record](path string, opts Options[T]) *Journal[T] {
	j := &Journal[T]{path: path, limit: opts.Limit, normalize: opts.Normalize, transient: opts.Transient, keepID: opts.KeepID, retiredID: map[string]bool{}, items: []T{}, byID: map[string]int{}, active: map[string]bool{}}
	j.updated = sync.NewCond(&j.updatesMu)
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
	j.retired = nil
	clear(j.retiredID)
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
		if err == nil {
			for _, id := range file.RetiredIDs {
				if !j.retiredID[id] {
					j.retired = append(j.retired, id)
					j.retiredID[id] = true
				}
			}
		}
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

// ListActive returns a copy of every unfinished record (!Finished()), oldest
// first, without copying or iterating over finished records.
func (j *Journal[T]) ListActive() []T {
	j.mu.Lock()
	defer j.mu.Unlock()
	if len(j.active) == 0 {
		return []T{}
	}
	idxs := make([]int, 0, len(j.active))
	for id := range j.active {
		if i, ok := j.byID[id]; ok {
			idxs = append(idxs, i)
		}
	}
	slices.Sort(idxs)
	out := make([]T, len(idxs))
	for k, i := range idxs {
		out[k] = j.items[i]
	}
	return out
}

// Path is the journal file.
func (j *Journal[T]) Path() string { return j.path }

// Flush retries persistence after a previous write failed. An idempotent
// submission must not acknowledge records held only in memory.
func (j *Journal[T]) Flush() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.saveLocked()
}

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

func (j *Journal[T]) Retired(id string) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.retiredID[id]
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
	if j.blocked != nil {
		return nil, j.blocked
	}
	seen := make(map[string]bool, len(records))
	for _, r := range records {
		id := r.JobID()
		if _, dup := j.byID[id]; dup || seen[id] || j.retiredID[id] {
			return nil, fmt.Errorf("tác vụ %s đã có", id)
		}
		seen[id] = true
	}
	prev := slices.Clone(j.items)
	prevRetired := len(j.retired)
	for _, r := range records {
		j.items = append(j.items, r)
		id := r.JobID()
		j.byID[id] = len(j.items) - 1
		if !r.Finished() {
			j.active[id] = true
		}
	}
	trimmed := j.trimLocked()
	if err := j.saveLocked(); err != nil {
		j.items = prev
		j.undoRetirementLocked(prevRetired)
		j.reindexLocked()
		return nil, err
	}
	return trimmed, nil
}

// SaveBatch applies updates to existing records and appends adds in one
// atomic write. Memory is restored if validation or writing fails.
func (j *Journal[T]) SaveBatch(adds []T, updates []T) error {
	if len(adds) == 0 && len(updates) == 0 {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.blocked != nil {
		return j.blocked
	}
	prev := slices.Clone(j.items)
	prevRetired := len(j.retired)
	seenAdd := make(map[string]bool, len(adds))
	for _, u := range updates {
		i, ok := j.byID[u.JobID()]
		if !ok {
			j.items = prev
			return ErrNotFound
		}
		j.items[i] = u
	}
	for _, a := range adds {
		id := a.JobID()
		if _, dup := j.byID[id]; dup || seenAdd[id] || j.retiredID[id] {
			j.items = prev
			return fmt.Errorf("tác vụ %s đã có", id)
		}
		seenAdd[id] = true
	}
	j.items = append(j.items, adds...)
	j.trimLocked()
	j.reindexLocked()
	if err := j.saveLocked(); err != nil {
		j.items = prev
		j.undoRetirementLocked(prevRetired)
		j.reindexLocked()
		return err
	}
	return nil
}

// Update applies change to a record and persists the journal, unless the
// journal's Transient option calls the change progress only. The returned
// record is a copy taken before the write.
func (j *Journal[T]) Update(id string, change func(*T)) (T, error) {
	request := &recordUpdate[T]{id: id, change: change}
	j.updatesMu.Lock()
	j.updateQ = append(j.updateQ, request)
	for !request.done {
		if j.updating {
			j.updated.Wait()
			continue
		}
		batch := j.updateQ
		j.updateQ = nil
		j.updating = true
		j.updatesMu.Unlock()

		j.commitUpdates(batch)

		j.updatesMu.Lock()
		for _, completed := range batch {
			completed.done = true
		}
		j.updating = false
		j.updated.Broadcast()
	}
	j.updatesMu.Unlock()
	if request.panic != nil {
		panic(request.panic)
	}
	return request.result, request.err
}

// UpdateTransient applies change in memory only: progress text and similar
// values a restart may lose. They reach disk with the record's next Update.
func (j *Journal[T]) UpdateTransient(id string, change func(*T)) (T, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	i, ok := j.byID[id]
	if !ok {
		var zero T
		return zero, ErrNotFound
	}
	record := j.items[i]
	change(&record)
	j.setRecordLocked(i, record)
	return record, nil
}

func (j *Journal[T]) setRecordLocked(i int, record T) {
	j.items[i] = record
	if record.Finished() {
		delete(j.active, record.JobID())
	} else {
		j.active[record.JobID()] = true
	}
}

// Concurrent Update calls queued during an earlier write share one commit.
// The journal stays locked until the whole batch has committed or rolled back.
func (j *Journal[T]) commitUpdates(batch []*recordUpdate[T]) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.blocked != nil {
		for _, request := range batch {
			request.err = j.blocked
		}
		return
	}
	before := make(map[int]T, len(batch))
	persist := false
	for _, request := range batch {
		i, ok := j.byID[request.id]
		if !ok {
			request.err = ErrNotFound
			continue
		}
		current := j.items[i]
		request.result = current
		if request.panic = changeRecord(request.change, &request.result); request.panic != nil {
			continue
		}
		if _, seen := before[i]; !seen {
			before[i] = current
		}
		j.setRecordLocked(i, request.result)
		persist = persist || j.transient == nil || !j.transient(current, request.result)
	}
	if !persist {
		return
	}
	if err := j.saveLocked(); err != nil {
		for i, record := range before {
			j.setRecordLocked(i, record)
		}
		for _, request := range batch {
			if request.err == nil && request.panic == nil {
				var zero T
				request.result, request.err = zero, err
			}
		}
	}
}

// A callback may panic in another caller's batch. Return it to the caller
// that supplied the callback so its own recovery runs and the queue survives.
func changeRecord[T Record](change func(*T), record *T) (failure any) {
	defer func() { failure = recover() }()
	change(record)
	return nil
}

// UpdateMany applies change to each listed record (unknown IDs are skipped)
// and persists the journal once, so a bulk action on N records is one write
// instead of N. change reports whether it changed the record; only those
// are returned, in the order of ids.
func (j *Journal[T]) UpdateMany(ids []string, change func(*T) bool) ([]T, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.blocked != nil {
		return nil, j.blocked
	}
	prev := slices.Clone(j.items)
	var changed []T
	persist := false
	for _, id := range ids {
		i, ok := j.byID[id]
		if !ok {
			continue
		}
		before := j.items[i]
		record := before
		if !change(&record) {
			continue
		}
		j.items[i] = record
		if record.Finished() {
			delete(j.active, record.JobID())
		} else {
			j.active[record.JobID()] = true
		}
		changed = append(changed, record)
		if j.transient == nil || !j.transient(before, record) {
			persist = true
		}
	}
	if !persist {
		return changed, nil
	}
	if err := j.saveLocked(); err != nil {
		j.items = prev
		j.reindexLocked()
		return nil, err
	}
	return changed, nil
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
	if j.blocked != nil {
		return nil, j.blocked
	}
	prev := slices.Clone(j.items)
	prevRetired := len(j.retired)
	var removed []string
	j.items = slices.DeleteFunc(j.items, func(r T) bool {
		if drop(r) {
			j.retireLocked(r)
			removed = append(removed, r.JobID())
			return true
		}
		return false
	})
	if len(removed) == 0 {
		return nil, nil
	}
	j.reindexLocked()
	if err := j.saveLocked(); err != nil {
		j.items = prev
		j.undoRetirementLocked(prevRetired)
		j.reindexLocked()
		return nil, err
	}
	return removed, nil
}

// Remove deletes the record with id.
func (j *Journal[T]) Remove(id string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.blocked != nil {
		return j.blocked
	}
	i, ok := j.byID[id]
	if !ok {
		return ErrNotFound
	}
	prev := slices.Clone(j.items)
	prevRetired := len(j.retired)
	j.retireLocked(j.items[i])
	j.items = slices.Delete(j.items, i, i+1)
	j.reindexLocked()
	if err := j.saveLocked(); err != nil {
		j.items = prev
		j.undoRetirementLocked(prevRetired)
		j.reindexLocked()
		return err
	}
	return nil
}

func (j *Journal[T]) reindexLocked() {
	j.byID = make(map[string]int, len(j.items))
	j.active = make(map[string]bool)
	for i, r := range j.items {
		id := r.JobID()
		j.byID[id] = i
		if !r.Finished() {
			j.active[id] = true
		}
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
			j.retireLocked(r)
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

func (j *Journal[T]) retireLocked(record T) {
	id := record.JobID()
	if j.keepID != nil && j.keepID(record) && !j.retiredID[id] {
		j.retiredID[id] = true
		j.retired = append(j.retired, id)
	}
}

func (j *Journal[T]) undoRetirementLocked(from int) {
	for _, id := range j.retired[from:] {
		delete(j.retiredID, id)
	}
	j.retired = j.retired[:from]
}

// The caller holds mu until the write succeeds or its changes are rolled back.
// Readers and other updates only observe the committed state.
func (j *Journal[T]) saveLocked() error {
	if j.blocked != nil {
		return j.blocked
	}
	data, err := json.Marshal(journalFile[T]{FormatVersion: FormatVersion, Records: j.items, RetiredIDs: j.retired})
	if err != nil {
		return err
	}
	return j.write(data)
}

func (j *Journal[T]) write(data []byte) error {
	if err := os.MkdirAll(filepath.Dir(j.path), 0o700); err != nil {
		return err
	}
	return utils.WriteFileAtomic(j.path, data, 0o600)
}
