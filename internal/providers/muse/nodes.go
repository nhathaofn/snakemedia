package muse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
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

// Option sources Muse nodes read their select choices from.
const (
	OptionsImageRatios    = "muse.imageRatios"
	OptionsVideoRatios    = "muse.videoRatios"
	OptionsVideoDurations = "muse.videoDurations"
	OptionsAssets         = "muse.assets"
)

// nodeInt unmarshals JSON integers or numeric strings (e.g. from select options),
// treating "" or null as 0.
type nodeInt int

func (n *nodeInt) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*n = 0
		return nil
	}
	var iv int
	if err := json.Unmarshal(data, &iv); err == nil {
		*n = nodeInt(iv)
		return nil
	}
	var sv string
	if err := json.Unmarshal(data, &sv); err != nil {
		return err
	}
	sv = strings.TrimSpace(sv)
	if sv == "" {
		*n = 0
		return nil
	}
	parsed, err := strconv.Atoi(sv)
	if err != nil {
		return fmt.Errorf("giá trị số không hợp lệ %q", sv)
	}
	*n = nodeInt(parsed)
	return nil
}

// NodeRequest is the input of a Muse node: one job per prompt and repeat.
type NodeRequest struct {
	Prompt     engine.Texts      `json:"prompt"`
	References []engine.MediaRef `json:"references"`
	Ratio      string            `json:"ratio"`
	Duration   nodeInt           `json:"duration"`
	// Assets names fixed Tư liệu assets by @tag, exact name, or ID (comma/space
	// separated or list) attached to every prompt.
	Assets           engine.Texts `json:"assets"`
	StoryboardFrames nodeInt      `json:"storyboardFrames"`
	StoryboardReview bool         `json:"storyboardReview"`
	Repeat           int          `json:"repeat"`
	Label            string       `json:"label"`
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

// OptionSources implements providers.OptionProvider with Muse's supported
// aspect ratios, video durations, and current asset library entries.
func (p *Provider) OptionSources() map[string]engine.OptionSource {
	ratioOptions := func(kind MuseJobKind) []engine.Option {
		opts := []engine.Option{{Value: "", Label: "Theo prompt"}}
		for _, r := range museRatios {
			if slices.Contains(r.Kinds, kind) {
				opts = append(opts, engine.Option{Value: r.Value, Label: fmt.Sprintf("%s (%s)", r.Value, r.Label)})
			}
		}
		return opts
	}
	return map[string]engine.OptionSource{
		OptionsImageRatios: func(context.Context, map[string]string) ([]engine.Option, error) {
			return ratioOptions(MuseJobImage), nil
		},
		OptionsVideoRatios: func(context.Context, map[string]string) ([]engine.Option, error) {
			return ratioOptions(MuseJobVideo), nil
		},
		OptionsVideoDurations: func(context.Context, map[string]string) ([]engine.Option, error) {
			opts := []engine.Option{{Value: "0", Label: "Theo prompt"}}
			for _, d := range museDurations {
				opts = append(opts, engine.Option{Value: strconv.Itoa(d), Label: fmt.Sprintf("%d giây", d)})
			}
			return opts, nil
		},
		OptionsAssets: func(context.Context, map[string]string) ([]engine.Option, error) {
			if p == nil || p.assets == nil {
				return []engine.Option{}, nil
			}
			list := p.assets.List()
			opts := make([]engine.Option, 0, len(list))
			for _, a := range list {
				label := "@" + a.Tag
				if a.Project != "" {
					label = "[" + a.Project + "] " + label
				}
				if a.Name != "" {
					label += " · " + a.Name
				}
				opts = append(opts, engine.Option{Value: a.ID, Label: label})
			}
			return opts, nil
		},
	}
}

// Nodes implements providers.Provider.
func (p *Provider) Nodes() []engine.Node {
	node := func(kind MuseJobKind, nodeType, label, description string, out engine.PortType, params []engine.Param) engine.Node {
		return engine.Typed(engine.Spec{
			Type: nodeType, Provider: p.ID(), Label: label, Description: description,
			Inputs: []engine.Port{
				{Name: "prompt", Label: "Prompt", Type: engine.PortText, Multiple: true},
				{Name: "references", Label: "Ảnh tham chiếu", Type: engine.PortImage, Multiple: true, Optional: true},
			},
			Outputs: []engine.Port{{Name: "files", Label: "Kết quả", Type: out, Multiple: true}},
			Params:  params,
		}, func(ctx context.Context, req NodeRequest) (NodeResult, error) {
			prompts := req.Prompt.NonEmpty()
			if len(prompts) == 0 {
				return NodeResult{}, engine.Invalid("Hãy nhập prompt.")
			}
			refs := make([]string, 0, len(req.References))
			for _, ref := range req.References {
				if ref.Kind == engine.MediaVideo {
					return NodeResult{}, engine.Invalid(fmt.Sprintf("%s là video, Muse chỉ nhận ảnh tham chiếu.", ref.LocalURL))
				}
				refs = append(refs, ref.LocalURL)
			}
			assetIDs, err := p.resolveNodeAssets(req.Assets)
			if err != nil {
				return NodeResult{}, err
			}
			var board *MuseStoryboard
			if int(req.StoryboardFrames) < 0 {
				return NodeResult{}, engine.Invalid("Số khung storyboard không hợp lệ.")
			}
			if int(req.StoryboardFrames) > 0 {
				board = &MuseStoryboard{Frames: int(req.StoryboardFrames), Review: req.StoryboardReview}
			}
			jobs, err := engine.FromResponse(p.submitJobs(MuseJobRequest{
				Kind:       kind,
				Prompts:    prompts,
				Repeat:     req.Repeat,
				References: refs,
				Label:      req.Label,
				Ratio:      strings.TrimSpace(req.Ratio),
				Duration:   int(req.Duration),
				Assets:     assetIDs,
				Storyboard: board,
			}, engine.RunKey(ctx)))
			if err != nil {
				return NodeResult{}, err
			}
			return p.awaitJobs(ctx, kind, jobs, req.CancelOnStop)
		})
	}
	imageParams := []engine.Param{
		{Name: "ratio", Label: "Tỉ lệ", Kind: engine.ParamSelect, OptionsFrom: OptionsImageRatios},
		{Name: "assets", Label: "Tư liệu (@tag hoặc ID)", Kind: engine.ParamText},
		{Name: "repeat", Label: "Số lần mỗi prompt", Kind: engine.ParamInteger, Min: 1},
		{Name: "label", Label: "Tên", Kind: engine.ParamText},
		{Name: "cancelOnStop", Label: "Huỷ tác vụ chưa gửi khi dừng", Kind: engine.ParamToggle},
	}
	videoParams := []engine.Param{
		{Name: "ratio", Label: "Tỉ lệ", Kind: engine.ParamSelect, OptionsFrom: OptionsVideoRatios},
		{Name: "duration", Label: "Thời lượng (giây)", Kind: engine.ParamSelect, OptionsFrom: OptionsVideoDurations},
		{Name: "assets", Label: "Tư liệu (@tag hoặc ID)", Kind: engine.ParamText},
		{Name: "storyboardFrames", Label: "Storyboard (số khung ảnh)", Kind: engine.ParamInteger, Min: 0, Max: maxBoardFrames},
		{Name: "storyboardReview", Label: "Chờ duyệt storyboard", Kind: engine.ParamToggle},
		{Name: "repeat", Label: "Số lần mỗi prompt", Kind: engine.ParamInteger, Min: 1},
		{Name: "label", Label: "Tên", Kind: engine.ParamText},
		{Name: "cancelOnStop", Label: "Huỷ tác vụ chưa gửi khi dừng", Kind: engine.ParamToggle},
	}
	return []engine.Node{
		node(MuseJobImage, NodeImageGenerate, "Muse: tạo ảnh",
			"Tạo ảnh bằng agent Muse, mỗi prompt một chat, chia cho các tài khoản đang dùng được.", engine.PortImage, imageParams),
		node(MuseJobVideo, NodeVideoGenerate, "Muse: tạo video",
			"Tạo video bằng agent Muse, mỗi prompt một chat, chia cho các tài khoản đang dùng được.", engine.PortVideo, videoParams),
	}
}

// resolveNodeAssets resolves @tag, asset ID, or exact name tokens into unique asset IDs.
func findMatchingAsset(all []MuseAsset, tok string) *MuseAsset {
	tag := strings.TrimPrefix(tok, "@")
	for i := range all {
		if all[i].ID == tok || strings.EqualFold(all[i].Tag, tag) || (all[i].Name != "" && all[i].Name == tok) {
			return &all[i]
		}
	}
	return nil
}

func (p *Provider) resolveNodeAssets(inputs engine.Texts) ([]string, error) {
	items := inputs.NonEmpty()
	if len(items) == 0 {
		return nil, nil
	}
	all := p.assets.List()
	var ids []string
	seen := map[string]bool{}
	addMatch := func(matched *MuseAsset) {
		if !seen[matched.ID] {
			seen[matched.ID] = true
			ids = append(ids, matched.ID)
		}
	}
	for _, entry := range items {
		trimmed := strings.TrimSpace(entry)
		if trimmed == "" {
			continue
		}
		if matched := findMatchingAsset(all, trimmed); matched != nil {
			addMatch(matched)
			continue
		}
		segments := strings.FieldsFunc(trimmed, func(r rune) bool {
			return r == ',' || r == ';'
		})
		for _, seg := range segments {
			segTok := strings.TrimSpace(seg)
			if segTok == "" {
				continue
			}
			if matched := findMatchingAsset(all, segTok); matched != nil {
				addMatch(matched)
				continue
			}
			for _, raw := range strings.Fields(segTok) {
				matched := findMatchingAsset(all, raw)
				if matched == nil {
					return nil, engine.Invalid(fmt.Sprintf("Không tìm thấy tư liệu %q trong Muse.", raw))
				}
				addMatch(matched)
			}
		}
	}
	return ids, nil
}

// awaitJobs waits until target jobs end and collects their files of target kind.
// Intermediate storyboard image jobs are waited on only as prerequisites; their
// image files are never mixed into a video node's output. When ctx ends first,
// jobs already sent keep running on the queue and the node is pending on them;
// jobs not sent yet are cancelled if cancelOnStop.
func (p *Provider) awaitJobs(ctx context.Context, targetKind MuseJobKind, jobs []MuseJob, cancelOnStop bool) (NodeResult, error) {
	res := NodeResult{Files: []engine.MediaRef{}, Failed: []string{}}
	pending := make([]string, 0, len(jobs))
	boardToVideos := make(map[string][]string)
	for _, j := range jobs {
		pending = append(pending, j.ID)
		if j.Kind == targetKind && j.After != "" {
			boardToVideos[j.After] = append(boardToVideos[j.After], j.ID)
		}
	}
	for len(pending) > 0 {
		snapIDs := make([]string, 0, len(pending)+len(boardToVideos))
		snapSet := make(map[string]struct{}, len(pending))
		for _, id := range pending {
			snapIDs = append(snapIDs, id)
			snapSet[id] = struct{}{}
		}
		for boardID := range boardToVideos {
			if _, ok := snapSet[boardID]; !ok {
				snapIDs = append(snapIDs, boardID)
			}
		}
		current := p.jobs.snapshot(snapIDs)
		abandoned := make(map[string]string)
		for boardID, videoIDs := range boardToVideos {
			bj, ok := current[boardID]
			switch {
			case ok && bj.Status == MuseJobSucceeded && len(bj.Files) > 0:
				for _, vid := range videoIDs {
					if v, vOK := current[vid]; vOK && len(v.Frames) == 0 && !v.finished() && v.BoardFrames > 0 && len(bj.Files) < v.BoardFrames {
						abandoned[vid] = fmt.Sprintf("storyboard chỉ tạo được %d/%d ảnh", len(bj.Files), v.BoardFrames)
					}
				}
				delete(boardToVideos, boardID)
			case !ok:
				for _, vid := range videoIDs {
					if v, vOK := current[vid]; vOK && len(v.Frames) == 0 && !v.finished() {
						abandoned[vid] = "storyboard đã bị xoá"
					}
				}
			case bj.Status == MuseJobFailed || bj.Status == MuseJobCancelled:
				reason := bj.Error
				if reason == "" {
					reason = "storyboard không thành công"
				}
				for _, vid := range videoIDs {
					if v, vOK := current[vid]; vOK && len(v.Frames) == 0 && !v.finished() {
						abandoned[vid] = reason
					}
				}
			case bj.Status == MuseJobSucceeded && len(bj.Files) == 0:
				for _, vid := range videoIDs {
					if v, vOK := current[vid]; vOK && len(v.Frames) == 0 && !v.finished() {
						abandoned[vid] = "storyboard không tạo được ảnh"
					}
				}
			}
		}
		left := pending[:0]
		for _, id := range pending {
			if reason, drop := abandoned[id]; drop {
				job := current[id]
				res.Failed = append(res.Failed, fmt.Sprintf("%s: %s", truncate(job.Prompt, 60), reason))
				continue
			}
			job, ok := current[id]
			if !ok {
				if _, isBoard := boardToVideos[id]; !isBoard {
					res.Failed = append(res.Failed, id+": tác vụ đã bị xoá")
				}
				continue
			}
			isTarget := job.Kind == targetKind
			switch {
			case job.Status == MuseJobSucceeded:
				if isTarget {
					for _, f := range job.Files {
						res.Files = append(res.Files, engine.RefFor(f, mediaKind(job.Kind)))
					}
				}
			case job.finished():
				if isTarget {
					res.Failed = append(res.Failed, fmt.Sprintf("%s: %s", truncate(job.Prompt, 60), job.Error))
				}
			default:
				left = append(left, id)
			}
		}
		if pending = left; len(pending) == 0 {
			break
		}
		pollMS := p.cfg.Get().Muse.PollIntervalMS
		if pollMS <= 0 {
			pollMS = 500
		}
		if err := p.sleep(ctx, time.Duration(pollMS)*time.Millisecond); err != nil {
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
