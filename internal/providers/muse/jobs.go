package muse

import (
	"errors"
	"reflect"

	"SnakeMedia/internal/jobs"
)

// MuseJobKind is what a job asks Muse to make.
type MuseJobKind string

const (
	MuseJobImage MuseJobKind = "image"
	MuseJobVideo MuseJobKind = "video"
)

var AllMuseJobKinds = []struct {
	Value  MuseJobKind
	TSName string
}{{MuseJobImage, "Image"}, {MuseJobVideo, "Video"}}

// valid reports whether the kind is one Muse makes.
func (k MuseJobKind) valid() bool { return k == MuseJobImage || k == MuseJobVideo }

// MuseJobStatus is a job's place in its lifecycle.
type MuseJobStatus string

const (
	MuseJobQueued     MuseJobStatus = "queued"
	MuseJobProcessing MuseJobStatus = "processing"
	MuseJobSucceeded  MuseJobStatus = "succeeded"
	MuseJobFailed     MuseJobStatus = "failed"
	MuseJobCancelled  MuseJobStatus = "cancelled"
)

var AllMuseJobStatuses = []struct {
	Value  MuseJobStatus
	TSName string
}{
	{MuseJobQueued, "Queued"}, {MuseJobProcessing, "Processing"}, {MuseJobSucceeded, "Succeeded"},
	{MuseJobFailed, "Failed"}, {MuseJobCancelled, "Cancelled"},
}

// MuseJob is one durable generation request: one prompt sent in its own Muse
// chat. SessionID is that chat: once set, the job is only ever re-checked
// there, never re-sent, so an interrupted job costs no second Muse generation.
type MuseJob struct {
	ID     string      `json:"id"`
	Kind   MuseJobKind `json:"kind"`
	Prompt string      `json:"prompt"`
	// Label is a short title for the queue and saved file names; empty uses the prompt.
	Label string `json:"label,omitempty"`
	// References are local media URLs of the attached images, in order.
	References []string `json:"references"`
	// Rechecks counts failed attempts to read back a job Muse already accepted.
	Rechecks int `json:"rechecks,omitempty"`
	// Held jobs wait in the queue until the user runs them.
	Held      bool          `json:"held,omitempty"`
	Status    MuseJobStatus `json:"status"`
	Step      string        `json:"step,omitempty"`
	AccountID string        `json:"accountId,omitempty"`
	SessionID string        `json:"sessionId,omitempty"`
	// Sending is set just before a send and cleared once Muse answers; still set
	// after a restart means the outcome is unknown.
	Sending  bool     `json:"sending,omitempty"`
	Attempts int      `json:"attempts"`
	Error    string   `json:"error,omitempty"`
	Files    []string `json:"files"`
	// SavedFiles are the copies written to the output folder.
	SavedFiles []string `json:"savedFiles,omitempty"`
	CreatedAt  string   `json:"createdAt"`
	StartedAt  string   `json:"startedAt,omitempty"`
	// SentAt is when Muse accepted the send (SessionID set); the job's time
	// limit counts from here. Older journals fall back to StartedAt.
	SentAt     string `json:"sentAt,omitempty"`
	FinishedAt string `json:"finishedAt,omitempty"`
	// LimitReplies counts how often Muse answered "limit" or "verify" instead
	// of making the job; each answer cost a chat turn, so it is capped. It
	// survives requeues and restarts and is reset only by an explicit retry.
	LimitReplies int `json:"limitReplies,omitempty"`
	// NextCheckAt holds the job back until then (RFC3339): a re-check after an
	// error, or a job whose account was not ready, waits instead of retrying
	// at once, also across a restart.
	NextCheckAt string `json:"nextCheckAt,omitempty"`
}

// resetForRetry is resetForQueue for a retry the user asked for: the limit
// reply count and any hold start over.
func resetForRetry(j *MuseJob) {
	resetForQueue(j)
	j.LimitReplies, j.NextCheckAt = 0, ""
}

// resetForQueue clears everything a previous run left behind, so a job sent
// again starts a fresh chat from its first turn.
func resetForQueue(j *MuseJob) {
	j.Status, j.Step, j.Error = MuseJobQueued, "", ""
	j.AccountID, j.SessionID, j.Sending, j.Rechecks = "", "", false, 0
	j.Files, j.SavedFiles = []string{}, []string{}
	j.StartedAt, j.SentAt, j.FinishedAt = "", "", ""
}

func (j MuseJob) finished() bool {
	return j.Status == MuseJobSucceeded || j.Status == MuseJobFailed || j.Status == MuseJobCancelled
}

// JobID implements jobs.Record.
func (j MuseJob) JobID() string { return j.ID }

// Finished implements jobs.Record: only finished jobs are trimmed.
func (j MuseJob) Finished() bool { return j.finished() }

// jobStore keeps every job in the shared durable journal (internal/jobs):
// one JSON file, rewritten atomically when a job's state changes, indexed by
// ID. Only add trims, and only finished jobs, so a job being updated never
// moves or disappears.
type jobStore struct {
	path    string
	journal *jobs.Journal[MuseJob]
}

func newJobStore(path string, maxJobs func() int) *jobStore {
	return &jobStore{path: path, journal: jobs.New(path, jobs.Options[MuseJob]{
		Limit:     maxJobs,
		Normalize: normalizeJob,
		Transient: onlyStepChanged,
	})}
}

// errJournalCorrupt marks a journal that could not be read and was moved aside;
// the queue then starts empty and the user should be told.
var errJournalCorrupt = jobs.ErrCorrupt

// normalizeJob gives a job read from disk empty slices instead of nil.
func normalizeJob(j *MuseJob) {
	if j.Files == nil {
		j.Files = []string{}
	}
	if j.SavedFiles == nil {
		j.SavedFiles = []string{}
	}
	if j.References == nil {
		j.References = []string{}
	}
}

// load reads the journal. A corrupt file is moved aside and the app starts
// with an empty queue rather than refusing to run; the error wraps
// errJournalCorrupt so the caller can tell the user.
func (s *jobStore) load() error { return s.journal.Load() }

func (s *jobStore) list() []MuseJob {
	res := s.journal.List()
	for i := range res {
		normalizeJob(&res[i])
	}
	return res
}

func (s *jobStore) get(id string) (MuseJob, bool) { return s.journal.Get(id) }

// snapshot returns the jobs with the given IDs that still exist, read under
// one lock.
func (s *jobStore) snapshot(ids []string) map[string]MuseJob { return s.journal.Snapshot(ids) }

func (s *jobStore) add(job MuseJob) error { return s.journal.Add(job) }

// update applies change to a job and persists the journal. A change of the
// Step text alone (progress shown while a job runs) stays in memory: it is
// written with the job's next real change, and a restart drops it anyway.
func (s *jobStore) update(id string, change func(*MuseJob)) (MuseJob, error) {
	job, err := s.journal.Update(id, change)
	if errors.Is(err, jobs.ErrNotFound) {
		return MuseJob{}, errJobNotFound
	}
	return job, err
}

// onlyStepChanged reports whether a and b differ in nothing but Step.
func onlyStepChanged(a, b MuseJob) bool {
	a.Step, b.Step = "", ""
	return reflect.DeepEqual(a, b)
}

// removeWhere deletes every job drop selects and returns how many went.
func (s *jobStore) removeWhere(drop func(MuseJob) bool) (int, error) {
	return s.journal.RemoveWhere(drop)
}

func (s *jobStore) remove(id string) error {
	err := s.journal.Remove(id)
	if errors.Is(err, jobs.ErrNotFound) {
		return errJobNotFound
	}
	return err
}

var errJobNotFound = errors.New("Tác vụ Muse không tồn tại.")
