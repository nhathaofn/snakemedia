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

// MuseJobMode is the screen a job was made from: Đơn (one prompt, shown as a
// gallery) or Hàng loạt (a batch, shown as a queue). Both share one queue.
type MuseJobMode string

const (
	MuseJobSingle MuseJobMode = "single"
	MuseJobBatch  MuseJobMode = "batch"
)

var AllMuseJobModes = []struct {
	Value  MuseJobMode
	TSName string
}{{MuseJobSingle, "Single"}, {MuseJobBatch, "Batch"}}

// valid reports whether the mode is a known one; empty is not.
func (m MuseJobMode) valid() bool { return m == MuseJobSingle || m == MuseJobBatch }

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
	ID   string      `json:"id"`
	Kind MuseJobKind `json:"kind"`
	// Mode is the screen the job was made from; journals written before it
	// existed leave it empty, which reads as MuseJobBatch.
	Mode   MuseJobMode `json:"mode,omitempty"`
	Prompt string      `json:"prompt"`
	// Label is a short title for the queue and saved file names; empty uses the prompt.
	Label string `json:"label,omitempty"`
	// References are local media URLs of the attached images, in order.
	References []string `json:"references"`
	// OutputDir is the batch's own folder (under the folder the user chose,
	// see batchFolder) a Hàng loạt job's files are saved into, and Index the
	// prompt's place in that batch (from 1), which numbers them
	// (media.OutputName). Đơn jobs have no folder: nothing is saved.
	OutputDir string `json:"outputDir,omitempty"`
	Index     int    `json:"index,omitempty"`
	// Ratio ("9:16") and Duration (seconds, video only) are what the message
	// asks Muse for besides the prompt; empty or 0 leaves it to the prompt.
	Ratio    string `json:"ratio,omitempty"`
	Duration int    `json:"duration,omitempty"`
	// FormatNote says how the made file differs from Ratio or Duration.
	FormatNote string `json:"formatNote,omitempty"`
	// ChangeNote is what Muse changed from the prompt to get a long video past
	// its video tool (the reply's CHANGED line).
	ChangeNote string `json:"changeNote,omitempty"`
	// Continues counts the continue messages sent into a long video's chat
	// (longvideo.go); RepliesSeen is how many chat events were read before the
	// last one, so only a newer reply ends its turn. Both are written before
	// the message leaves the app.
	Continues   int `json:"continues,omitempty"`
	RepliesSeen int `json:"repliesSeen,omitempty"`
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
	Sending bool `json:"sending,omitempty"`
	// SendUnknown marks a job failed because its send's answer was lost: Muse
	// may have made it, so sending it again may pay twice.
	SendUnknown bool     `json:"sendUnknown,omitempty"`
	Attempts    int      `json:"attempts"`
	Error       string   `json:"error,omitempty"`
	Files       []string `json:"files"`
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
	j.Status, j.Step, j.Error, j.FormatNote, j.ChangeNote = MuseJobQueued, "", "", "", ""
	j.Continues, j.RepliesSeen = 0, 0
	j.AccountID, j.SessionID, j.Sending, j.SendUnknown, j.Rechecks = "", "", false, false, 0
	j.Files, j.SavedFiles = []string{}, []string{}
	j.StartedAt, j.SentAt, j.FinishedAt = "", "", ""
}

// mayBePaid reports whether Muse accepted (has a chat) or may have accepted
// (the send's answer was lost) the job's last send. Journals written before
// SendUnknown existed are recognised by their error.
func (j MuseJob) mayBePaid() bool {
	return j.SessionID != "" || j.SendUnknown || j.Error == errSendUnknown.Error()
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
