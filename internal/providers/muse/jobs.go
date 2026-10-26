package muse

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

// MuseJobKind is what a job asks Muse to make.
type MuseJobKind string

const (
	MuseJobImage MuseJobKind = "image"
	MuseJobVideo MuseJobKind = "video"
	// MuseJobV2V recreates a source video: Muse first analyses it, then makes a
	// new video in the same chat from the prompt and reference images.
	MuseJobV2V MuseJobKind = "v2v"
)

var AllMuseJobKinds = []struct {
	Value  MuseJobKind
	TSName string
}{{MuseJobImage, "Image"}, {MuseJobVideo, "Video"}, {MuseJobV2V, "V2V"}}

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

// MuseJob is one durable generation request. SessionID is the Muse chat the job
// was sent to: once set, the job is only ever re-checked there, never re-sent,
// so an interrupted job costs no second Muse generation.
type MuseJob struct {
	ID     string      `json:"id"`
	Kind   MuseJobKind `json:"kind"`
	Prompt string      `json:"prompt"`
	Ratio  string      `json:"ratio"`
	Count  int         `json:"count"`
	// Label is a short title shown in the queue (a production names its parts).
	Label string `json:"label,omitempty"`
	// ProjectID groups jobs; a paused project's queued jobs wait.
	ProjectID string `json:"projectId"`
	// References are local media URLs of the attached images, in order.
	References []string `json:"references"`
	// Manifest is appended after the prompt to tell Muse what each image is for.
	Manifest string `json:"manifest,omitempty"`
	// SourceVideo is the local media URL of a V2V source; KeepAudio puts its
	// sound back on the result.
	SourceVideo string `json:"sourceVideo,omitempty"`
	KeepAudio   bool   `json:"keepAudio,omitempty"`
	// V2VTurn is 2 once the analysis turn is done and the remake was sent.
	V2VTurn int `json:"v2vTurn,omitempty"`
	// Thread asks to continue an existing Muse chat (a storyboard) on the
	// account PinAccount that owns it; if that fails a new chat is used.
	Thread      string `json:"thread,omitempty"`
	PinAccount  string `json:"pinAccount,omitempty"`
	ThreadError string `json:"threadError,omitempty"`
	// ProductionID marks a job a production created and follows;
	// ProductionPart names which part ("board", "all", "scene-N"), so a
	// production finds its child again after a crash instead of sending twice.
	ProductionID   string `json:"productionId,omitempty"`
	ProductionPart string `json:"productionPart,omitempty"`
	// HistoryBase and SeenPaths describe the chat before this job's message
	// was sent into it: a shared storyboard thread already holds earlier
	// replies and files, which must not be taken as this job's result.
	HistoryBase int      `json:"historyBase,omitempty"`
	SeenPaths   []string `json:"seenPaths,omitempty"`
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
	FinishedAt string   `json:"finishedAt,omitempty"`
}

// resetForQueue clears everything a previous run left behind, so a job sent
// again starts a fresh chat from its first turn.
func resetForQueue(j *MuseJob) {
	j.Status, j.Step, j.Error = MuseJobQueued, "", ""
	j.AccountID, j.SessionID, j.Sending = "", "", false
	j.V2VTurn, j.ThreadError, j.HistoryBase, j.SeenPaths, j.Rechecks = 0, "", 0, nil, 0
	j.Files, j.SavedFiles = []string{}, []string{}
	j.StartedAt, j.FinishedAt = "", ""
}

func (j MuseJob) finished() bool {
	return j.Status == MuseJobSucceeded || j.Status == MuseJobFailed || j.Status == MuseJobCancelled
}

// jobStore keeps every job in one JSON file, rewritten atomically on change.
type jobStore struct {
	mu      sync.Mutex
	path    string
	maxJobs func() int
	jobs    []MuseJob // oldest first
}

func newJobStore(path string, maxJobs func() int) *jobStore {
	return &jobStore{path: path, maxJobs: maxJobs, jobs: []MuseJob{}}
}

// load reads the journal. A corrupt file is moved aside and the app starts
// with an empty queue rather than refusing to run.
func (s *jobStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.jobs = []MuseJob{}
		return nil
	}
	if err != nil {
		return err
	}
	var jobs []MuseJob
	if err := json.Unmarshal(data, &jobs); err != nil {
		backup := fmt.Sprintf("%s.corrupt-%s", s.path, time.Now().Format("20060102-150405"))
		_ = os.Rename(s.path, backup)
		return fmt.Errorf("Danh sách tác vụ Muse hỏng, đã giữ tại %s: %w", backup, err)
	}
	for i := range jobs {
		if jobs[i].Files == nil {
			jobs[i].Files = []string{}
		}
		if jobs[i].SavedFiles == nil {
			jobs[i].SavedFiles = []string{}
		}
		if jobs[i].References == nil {
			jobs[i].References = []string{}
		}
	}
	s.jobs = jobs
	return nil
}

func (s *jobStore) saveLocked() error {
	if extra := len(s.jobs) - s.maxJobs(); extra > 0 {
		kept := s.jobs[:0]
		for _, j := range s.jobs {
			if extra > 0 && j.finished() {
				extra--
				continue
			}
			kept = append(kept, j)
		}
		s.jobs = kept
	}
	data, err := json.Marshal(s.jobs)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	return utils.WriteFileAtomic(s.path, data, 0o600)
}

func (s *jobStore) list() []MuseJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.jobs) == 0 {
		return []MuseJob{}
	}
	res := make([]MuseJob, len(s.jobs))
	copy(res, s.jobs)
	for i := range res {
		if res[i].Files == nil {
			res[i].Files = []string{}
		}
		if res[i].SavedFiles == nil {
			res[i].SavedFiles = []string{}
		}
		if res[i].References == nil {
			res[i].References = []string{}
		}
	}
	return res
}

func (s *jobStore) get(id string) (MuseJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.ID == id {
			return j, true
		}
	}
	return MuseJob{}, false
}

func (s *jobStore) add(job MuseJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, job)
	return s.saveLocked()
}

// update applies change to a job and persists the journal.
func (s *jobStore) update(id string, change func(*MuseJob)) (MuseJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.jobs {
		if s.jobs[i].ID == id {
			change(&s.jobs[i])
			return s.jobs[i], s.saveLocked()
		}
	}
	return MuseJob{}, errJobNotFound
}

// removeWhere deletes every job drop selects and returns how many went.
func (s *jobStore) removeWhere(drop func(MuseJob) bool) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	before := len(s.jobs)
	s.jobs = slices.DeleteFunc(s.jobs, drop)
	if len(s.jobs) == before {
		return 0, nil
	}
	return before - len(s.jobs), s.saveLocked()
}

func (s *jobStore) remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.jobs {
		if s.jobs[i].ID == id {
			s.jobs = slices.Delete(s.jobs, i, i+1)
			return s.saveLocked()
		}
	}
	return errJobNotFound
}

var errJobNotFound = errors.New("Tác vụ Muse không tồn tại.")
