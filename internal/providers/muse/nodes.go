package muse

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"SnakeMedia/internal/engine"
)

// Node types the Muse provider contributes. Each queues one job per prompt,
// waits for them on the queue (spread over the accounts like any job) and
// returns the files they produced, so other nodes can take them as input.
const (
	NodeImageGenerate = "muse.image.generate"
	NodeVideoGenerate = "muse.video.generate"
)

// NodeRequest is the input of a Muse node: one job per prompt and repeat.
type NodeRequest struct {
	Prompt     engine.Texts      `json:"prompt"`
	References []engine.MediaRef `json:"references"`
	Repeat     int               `json:"repeat"`
	Label      string            `json:"label"`
	// CancelOnStop cancels the node's jobs Muse has not been sent yet when the
	// graph stops; jobs already sent keep running (never paid twice).
	CancelOnStop bool `json:"cancelOnStop"`
}

// NodeResult is what a Muse node hands on: every file made, and the jobs that
// failed with their reasons.
type NodeResult struct {
	Files  []engine.MediaRef `json:"files"`
	Failed []string          `json:"failed"`
}

// ID implements providers.Provider.
func (p *Provider) ID() string { return "muse" }

// Name implements providers.Provider.
func (p *Provider) Name() string { return "Muse" }

// Nodes implements providers.Provider.
func (p *Provider) Nodes() []engine.Node {
	node := func(kind MuseJobKind, nodeType, label, description string, out engine.PortType) engine.Node {
		return engine.Typed(engine.Spec{
			Type: nodeType, Provider: p.ID(), Label: label, Description: description,
			Inputs: []engine.Port{
				{Name: "prompt", Label: "Prompt", Type: engine.PortText, Multiple: true},
				{Name: "references", Label: "Ảnh tham chiếu", Type: engine.PortImage, Multiple: true, Optional: true},
			},
			Outputs: []engine.Port{{Name: "files", Label: "Kết quả", Type: out, Multiple: true}},
			Params: []engine.Param{
				{Name: "repeat", Label: "Số lần mỗi prompt", Kind: engine.ParamInteger, Min: 1},
				{Name: "label", Label: "Tên", Kind: engine.ParamText},
				{Name: "cancelOnStop", Label: "Huỷ tác vụ chưa gửi khi dừng", Kind: engine.ParamToggle},
			},
		}, func(ctx context.Context, req NodeRequest) (NodeResult, error) {
			refs := make([]string, 0, len(req.References))
			for _, ref := range req.References {
				if ref.Kind == engine.MediaVideo {
					return NodeResult{}, engine.Invalid(fmt.Sprintf("%s là video, Muse chỉ nhận ảnh tham chiếu.", ref.LocalURL))
				}
				refs = append(refs, ref.LocalURL)
			}
			jobs, err := engine.FromResponse(p.submitJobs(MuseJobRequest{
				Kind: kind, Prompts: req.Prompt.NonEmpty(), Repeat: req.Repeat, References: refs, Label: req.Label,
			}, engine.RunKey(ctx)))
			if err != nil {
				return NodeResult{}, err
			}
			return p.awaitJobs(ctx, jobs, req.CancelOnStop)
		})
	}
	return []engine.Node{
		node(MuseJobImage, NodeImageGenerate, "Muse: tạo ảnh",
			"Tạo ảnh bằng agent Muse, mỗi prompt một chat, chia cho các tài khoản đang dùng được.", engine.PortImage),
		node(MuseJobVideo, NodeVideoGenerate, "Muse: tạo video",
			"Tạo video bằng agent Muse, mỗi prompt một chat, chia cho các tài khoản đang dùng được.", engine.PortVideo),
	}
}

// awaitJobs waits until every job has ended and collects their files. When
// ctx ends first, jobs already sent keep running on the queue and the node is
// pending on them; jobs not sent yet are cancelled if cancelOnStop.
func (p *Provider) awaitJobs(ctx context.Context, jobs []MuseJob, cancelOnStop bool) (NodeResult, error) {
	res := NodeResult{Files: []engine.MediaRef{}, Failed: []string{}}
	pending := make([]string, len(jobs))
	for i, j := range jobs {
		pending[i] = j.ID
	}
	for len(pending) > 0 {
		left := pending[:0]
		current := p.jobs.snapshot(pending)
		for _, id := range pending {
			job, ok := current[id]
			switch {
			case !ok:
				res.Failed = append(res.Failed, id+": tác vụ đã bị xoá")
			case job.Status == MuseJobSucceeded:
				for _, f := range job.Files {
					res.Files = append(res.Files, engine.RefFor(f, mediaKind(job.Kind)))
				}
			case job.finished():
				res.Failed = append(res.Failed, fmt.Sprintf("%s: %s", truncate(job.Prompt, 60), job.Error))
			default:
				left = append(left, id)
			}
		}
		if pending = left; len(pending) == 0 {
			break
		}
		if err := p.sleep(ctx, time.Duration(p.cfg.Get().Muse.PollIntervalMS)*time.Millisecond); err != nil {
			return res, p.stopWaiting(pending, cancelOnStop, err)
		}
	}
	if len(res.Files) == 0 && len(res.Failed) > 0 {
		return res, errors.New(strings.Join(res.Failed, "\n"))
	}
	return res, nil
}

// stopWaiting settles the jobs a node still waits on when its graph stops.
// Unsent jobs are cancelled if asked; any job Muse may already have taken
// keeps running and makes the node pending on it.
func (p *Provider) stopWaiting(ids []string, cancelOnStop bool, cause error) error {
	var running []string
	for _, id := range ids {
		if cancelOnStop {
			cancelled := false
			_, err := p.updateJob(id, func(j *MuseJob) {
				// Checked under the journal lock: a job the runner has
				// picked up in the meantime is left alone.
				if j.Status != MuseJobQueued || j.SessionID != "" || j.Sending {
					return
				}
				j.Status, j.Step, j.Error = MuseJobCancelled, "", "Đã huỷ vì luồng dừng"
				j.FinishedAt = p.now().UTC().Format(time.RFC3339)
				cancelled = true
			})
			if err == nil && cancelled {
				continue
			}
		}
		running = append(running, id)
	}
	if len(running) == 0 {
		return cause
	}
	return engine.Pending(fmt.Sprintf("%d tác vụ Muse vẫn chạy trên hàng đợi.", len(running)), running)
}

func mediaKind(kind MuseJobKind) engine.MediaKind {
	if kind == MuseJobVideo {
		return engine.MediaVideo
	}
	return engine.MediaImage
}
