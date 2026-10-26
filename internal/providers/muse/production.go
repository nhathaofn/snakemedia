package muse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"SnakeMedia/internal/logger"
	"SnakeMedia/pkg/utils"
	"github.com/google/uuid"
)

type ProductionStatus string

const (
	ProductionStatusQueued  ProductionStatus = "queued"
	ProductionStatusRunning ProductionStatus = "running"
	ProductionStatusReview  ProductionStatus = "review"
	ProductionStatusDone    ProductionStatus = "done"
	ProductionStatusError   ProductionStatus = "error"
	ProductionStatusStopped ProductionStatus = "stopped"
)

type Production struct {
	ID              string           `json:"id"`
	Title           string           `json:"title"`
	Config          ScriptConfig     `json:"config"`
	Scenes          []SceneItem      `json:"scenes"`
	References      []string         `json:"refs"`
	Status          ProductionStatus `json:"status"`
	Step            string           `json:"step,omitempty"`
	Error           string           `json:"error,omitempty"`
	BoardFiles      []string         `json:"boardFiles,omitempty"`
	ThreadSessionID string           `json:"threadSessionId,omitempty"`
	ThreadAccountID string           `json:"threadAccountId,omitempty"`
	CurrentSceneIdx int              `json:"currentSceneIdx"`
	SceneVideoFiles []string         `json:"sceneVideoFiles,omitempty"`
	FinalVideoFile  string           `json:"finalVideoFile,omitempty"`
	BoardJobID      string           `json:"boardJobId,omitempty"`
	CurrentJobID    string           `json:"currentJobId,omitempty"`
	CreatedAt       string           `json:"createdAt"`
	UpdatedAt       string           `json:"updatedAt"`
}

type productionStore struct {
	mu     sync.Mutex
	path   string
	items  []Production
	loaded bool
	err    error
}

func newProductionStore(path string) *productionStore {
	return &productionStore{path: path}
}

func (s *productionStore) loadLocked() error {
	if s.err != nil || s.loaded {
		return s.err
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.items = []Production{}
		s.loaded = true
		return nil
	}
	if err != nil {
		return err
	}
	var res []Production
	if err := json.Unmarshal(data, &res); err != nil {
		return err
	}
	s.items = res
	s.loaded = true
	return nil
}

func (s *productionStore) saveLocked() error {
	raw, err := json.MarshalIndent(s.items, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	return utils.WriteFileAtomic(s.path, raw, 0o600)
}

func (s *productionStore) list() []Production {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.loadLocked()
	res := make([]Production, len(s.items))
	copy(res, s.items)
	return res
}

func (s *productionStore) get(id string) (Production, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.loadLocked()
	for _, it := range s.items {
		if it.ID == id {
			return it, nil
		}
	}
	return Production{}, errors.New("Không tìm thấy production")
}

func (s *productionStore) add(item Production) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	item.CreatedAt = now
	item.UpdatedAt = now
	s.items = append(s.items, item)
	return s.saveLocked()
}

func (s *productionStore) update(id string, fn func(*Production)) (Production, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return Production{}, err
	}
	for i := range s.items {
		if s.items[i].ID == id {
			fn(&s.items[i])
			s.items[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			return s.items[i], s.saveLocked()
		}
	}
	return Production{}, errors.New("Không tìm thấy production")
}

func (s *productionStore) remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	var updated []Production
	for _, it := range s.items {
		if it.ID != id {
			updated = append(updated, it)
		}
	}
	s.items = updated
	return s.saveLocked()
}

// Parts of a production, recorded on each child job (MuseJob.ProductionPart).
const (
	partBoard = "board"
	partAll   = "all"
)

func scenePart(i int) string { return fmt.Sprintf("scene-%d", i+1) }

// childState is where a production's child job stands.
type childState int

const (
	childPending childState = iota
	childDone
	childFailed
)

// childOf reports a child job's state. A child that was removed, cancelled or
// finished without a file is a failure, never something to wait for.
func (p *Provider) childOf(jobID string) (MuseJob, childState, string) {
	job, ok := p.jobs.get(jobID)
	if !ok {
		return job, childFailed, "tác vụ con đã bị xoá khỏi hàng đợi"
	}
	switch job.Status {
	case MuseJobSucceeded:
		if len(job.Files) == 0 {
			return job, childFailed, "Muse không trả về file"
		}
		return job, childDone, ""
	case MuseJobFailed:
		return job, childFailed, job.Error
	case MuseJobCancelled:
		return job, childFailed, "tác vụ con đã bị huỷ"
	default:
		return job, childPending, ""
	}
}

// findChild returns a live or finished child already created for part, so a
// production that crashed between queuing a job and recording it adopts that
// job instead of paying for a second one.
func (p *Provider) findChild(prodID, part string) (MuseJob, bool) {
	var found MuseJob
	ok := false
	for _, job := range p.jobs.list() {
		if job.ProductionID != prodID || job.ProductionPart != part {
			continue
		}
		if job.Status == MuseJobFailed || job.Status == MuseJobCancelled {
			continue
		}
		if !ok || job.CreatedAt > found.CreatedAt {
			found, ok = job, true
		}
	}
	return found, ok
}

func (p *Provider) failProduction(id, msg string) {
	p.productions.update(id, func(pr *Production) {
		pr.Status, pr.Error, pr.Step = ProductionStatusError, msg, ""
	})
}

// launchPart queues the child job for part (or adopts one already queued) and
// records it. The job is complete when written: thread, pinned account and
// production link included.
func (p *Provider) launchPart(prod Production, part string, req MuseJobRequest, decorate func(*MuseJob), record func(*Production, string)) {
	job, ok := p.findChild(prod.ID, part)
	if !ok {
		res := p.submit(req, func(j *MuseJob) {
			j.ProductionID, j.ProductionPart = prod.ID, part
			if decorate != nil {
				decorate(j)
			}
		})
		if !res.Success || len(res.Data) == 0 {
			p.failProduction(prod.ID, "Không gửi được tác vụ ("+part+"): "+res.Error)
			return
		}
		job = res.Data[0]
	}
	p.productions.update(prod.ID, func(pr *Production) { record(pr, job.ID) })
}

// inThread continues the storyboard chat on its account when there is one.
func inThread(prod Production) func(*MuseJob) {
	return func(j *MuseJob) {
		if prod.ThreadSessionID != "" {
			j.Thread, j.PinAccount = prod.ThreadSessionID, prod.ThreadAccountID
		}
	}
}

// pumpProductions advances every production one step. It never waits: each
// step either launches a child job or reacts to one that finished.
func (p *Provider) pumpProductions(ctx context.Context) {
	for _, prod := range p.productions.list() {
		switch prod.Status {
		case ProductionStatusQueued:
			p.startProduction(prod)
		case ProductionStatusRunning:
			p.advanceProduction(ctx, prod)
		}
	}
}

func (p *Provider) startProduction(prod Production) {
	if prod.Config.Board == "" || len(prod.BoardFiles) > 0 {
		p.productions.update(prod.ID, func(pr *Production) {
			pr.Status, pr.Step = ProductionStatusRunning, "Bắt đầu làm video..."
		})
		return
	}
	expect := min(len(prod.Scenes), BoardMax)
	req := MuseJobRequest{
		Kind:       MuseJobImage,
		Prompts:    []string{BuildBoardPrompt(prod.Scenes, prod.Config, prod.References, "")},
		Ratio:      prod.Config.Ratio,
		Count:      1, // the real board size is set below; it may exceed the per-job limit users get
		References: prod.References,
		Label:      fmt.Sprintf("%s - Storyboard", prod.Title),
	}
	p.launchPart(prod, partBoard, req, func(j *MuseJob) { j.Count = expect }, func(pr *Production, id string) {
		pr.Status, pr.Step, pr.BoardJobID = ProductionStatusRunning, "Đang tạo storyboard...", id
	})
}

func (p *Provider) advanceProduction(ctx context.Context, prod Production) {
	if prod.BoardJobID != "" {
		job, state, msg := p.childOf(prod.BoardJobID)
		switch state {
		case childPending:
			return
		case childFailed:
			p.failProduction(prod.ID, "Vẽ storyboard thất bại: "+msg)
			return
		}
		p.productions.update(prod.ID, func(pr *Production) {
			pr.BoardFiles, pr.ThreadSessionID, pr.ThreadAccountID, pr.BoardJobID = job.Files, job.SessionID, job.AccountID, ""
			pr.Step = "Storyboard hoàn thành, tiếp tục làm video..."
			if pr.Config.Board == "review" {
				pr.Status, pr.Step = ProductionStatusReview, "Chờ duyệt storyboard"
			}
		})
		return
	}
	if prod.Config.Board != "" && len(prod.BoardFiles) == 0 {
		p.startProduction(prod) // the board was lost (redrawn or retried): make it again
		return
	}

	if prod.CurrentJobID != "" {
		job, state, msg := p.childOf(prod.CurrentJobID)
		switch state {
		case childPending:
			return
		case childFailed:
			p.failProduction(prod.ID, fmt.Sprintf("Cảnh %d bị lỗi: %s", prod.CurrentSceneIdx+1, msg))
			return
		}
		updated, err := p.productions.update(prod.ID, func(pr *Production) {
			pr.SceneVideoFiles = append(pr.SceneVideoFiles, job.Files[0])
			pr.CurrentJobID = ""
			pr.CurrentSceneIdx++
		})
		if err != nil {
			return
		}
		prod = updated
	}

	total := len(prod.Scenes)
	if prod.Config.Duration == "each" {
		if i := prod.CurrentSceneIdx; i < total {
			req := MuseJobRequest{
				Kind:       MuseJobVideo,
				Prompts:    []string{BuildPrompt([]SceneItem{prod.Scenes[i]}, prod.Config, prod.References, "")},
				Ratio:      prod.Config.Ratio,
				References: prod.References,
				Label:      fmt.Sprintf("%s - Cảnh %d/%d", prod.Title, i+1, total),
			}
			p.launchPart(prod, scenePart(i), req, inThread(prod), func(pr *Production, id string) {
				pr.CurrentJobID, pr.Step = id, fmt.Sprintf("Đang làm cảnh %d/%d...", i+1, total)
			})
			return
		}
	} else if len(prod.SceneVideoFiles) == 0 {
		req := MuseJobRequest{
			Kind:       MuseJobVideo,
			Prompts:    []string{BuildPrompt(prod.Scenes, prod.Config, prod.References, "")},
			Ratio:      prod.Config.Ratio,
			References: prod.References,
			Label:      prod.Title,
		}
		p.launchPart(prod, partAll, req, inThread(prod), func(pr *Production, id string) {
			pr.CurrentJobID, pr.Step = id, "Đang làm toàn bộ video..."
		})
		return
	}
	p.finishProduction(ctx, prod)
}

// finishProduction merges the scenes and adds music once every scene is made.
func (p *Provider) finishProduction(ctx context.Context, prod Production) {
	each := prod.Config.Duration == "each"
	needMerge := each && len(prod.SceneVideoFiles) > 1
	finalVideo := ""
	if len(prod.SceneVideoFiles) > 0 {
		finalVideo = prod.SceneVideoFiles[0]
	}

	if needMerge {
		p.productions.update(prod.ID, func(pr *Production) {
			pr.Step = "Đang ghép các cảnh video..."
		})
		mergeRes := p.MergeVideos(ctx, prod.SceneVideoFiles, prod.Title)
		if mergeRes.Success {
			finalVideo = mergeRes.Data
		} else {
			logger.Get().Warn("production merge failed", "error", mergeRes.Error)
		}
	}

	if prod.Config.Music && finalVideo != "" {
		p.productions.update(prod.ID, func(pr *Production) {
			pr.Step = "Đang lồng nhạc nền ngẫu nhiên..."
		})
		musicDir := filepath.Join(filepath.Dir(p.profileRoot), "music")
		musicList := findMusicFiles(musicDir)
		if len(musicList) > 0 {
			chosenMusic := musicList[rand.Intn(len(musicList))]
			cfg := p.cfg.Get().Muse
			ffmpegPath, _ := findFFmpeg(cfg.FFmpegPath)
			ffprobePath, _ := findFFprobe(ffmpegPath)
			outMusic := strings.TrimSuffix(finalVideo, filepath.Ext(finalVideo)) + "_music.mp4"
			if err := addMusic(ctx, ffmpegPath, ffprobePath, finalVideo, chosenMusic, outMusic); err == nil {
				finalVideo = outMusic
			}
		}
	}

	p.productions.update(prod.ID, func(pr *Production) {
		pr.Status = ProductionStatusDone
		pr.Step = "Hoàn thành"
		pr.FinalVideoFile = finalVideo
	})
}

func findMusicFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var res []string
	validExts := map[string]bool{".mp3": true, ".m4a": true, ".aac": true, ".wav": true, ".ogg": true, ".flac": true}
	for _, e := range entries {
		if !e.IsDir() && validExts[strings.ToLower(filepath.Ext(e.Name()))] {
			res = append(res, filepath.Join(dir, e.Name()))
		}
	}
	return res
}
