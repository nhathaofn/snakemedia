package muse

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"SnakeMedia/internal/media"
	"SnakeMedia/internal/models"
	"github.com/google/uuid"
)

// assetsByID returns the named assets in the given order.
func (p *Provider) assetsByID(ids []string) ([]MuseAsset, error) {
	out := make([]MuseAsset, 0, len(ids))
	for _, id := range ids {
		a, ok := p.assets.Get(id)
		if !ok {
			return nil, errors.New("Tư liệu đã chọn không còn. Hãy chọn lại.")
		}
		out = append(out, a)
	}
	return out, nil
}

// promptPlan is what one prompt attaches: References and their RefInfo.
type promptPlan struct {
	refs []string
	info []MuseRefInfo
}

// planPrompts decides every prompt's images (resolvePromptRefs) and snapshots
// Vault items into immutable MediaDir copies so queued/replayed jobs always
// attach the exact images submitted even if the user renames/edits Vault files.
func (p *Provider) planPrompts(req MuseJobRequest, prompts []string, fixed []MuseAsset, runKey string) ([]promptPlan, error) {
	var all []MuseAsset
	if runKey == "" {
		all = p.assets.List()
	}
	var extraFrames []int
	if req.Storyboard != nil {
		extraFrames = make([]int, len(prompts))
		for i := range extraFrames {
			extraFrames[i] = req.Storyboard.Frames
		}
	}
	return p.planPromptSpecs(prompts, req.Project, req.References, req.RefMode, fixed, all, extraFrames)
}

func (p *Provider) planPromptSpecs(prompts []string, project string, ownRefs []string, refMode MuseRefMode, fixed, all []MuseAsset, extraFrames []int) ([]promptPlan, error) {
	caps, _ := p.knownCapabilities()
	plans := make([]promptPlan, len(prompts))
	for pi, prompt := range prompts {
		refs, info, _, _ := resolvePromptRefs(prompt, project, pi, fixed, all, ownRefs, refMode)
		if len(p.missingImages(refs)) > 0 {
			return nil, fmt.Errorf("Prompt %d: có ảnh tư liệu không còn trên đĩa. Hãy kiểm tra kho tư liệu.", pi+1)
		}
		files := len(refs)
		if pi < len(extraFrames) {
			files += extraFrames[pi]
		}
		if files > 0 && caps == nil {
			return nil, errLimitsUnknown
		}
		snapRefs := make([]string, len(refs))
		for k, ref := range refs {
			if strings.HasPrefix(ref, media.Route) {
				snapRefs[k] = ref
				continue
			}
			snap, snapErr := p.snapshotToMedia(ref)
			if snapErr != nil {
				return nil, fmt.Errorf("Prompt %d: không thể sao lưu ảnh tư liệu (%v).", pi+1, snapErr)
			}
			snapRefs[k] = snap
		}
		plans[pi] = promptPlan{refs: snapRefs, info: info}
	}
	return plans, nil
}

// estimateInlineBytes returns a fast approximate encoded request size assuming
// non-JPEG or oversized images are normalized to .jpg <= 2048px before send.
func (p *Provider) estimateInlineBytes(job MuseJob, refs []string) int64 {
	const compressedEstRaw = 240 << 10
	total := int64(len(buildMessage(job))) + int64(museRequestReserve)
	for _, ref := range refs {
		var raw int64 = compressedEstRaw
		if p != nil && p.media != nil {
			if path, err := p.media.OpenPath(ref); err == nil {
				if st, statErr := os.Stat(path); statErr == nil && st.Size() > 0 {
					raw = min(st.Size(), compressedEstRaw)
				}
			}
		}
		total += int64(base64.StdEncoding.EncodedLen(int(raw))) + 128
	}
	return total
}

// storyboardJob is the image job that draws a video job's scene images.
// The frames follow the video's ratio when Muse offers it for images.
func storyboardJob(video MuseJob, frames int) MuseJob {
	ratio := video.Ratio
	if _, ok := ratioFor(ratio, MuseJobImage); !ok {
		ratio = ""
	}
	return MuseJob{
		ID: uuid.NewString(), Kind: MuseJobImage, Mode: video.Mode, Prompt: video.Prompt,
		Label:      strings.TrimSpace(video.Label + " · storyboard"),
		References: slices.Clone(video.References), RefInfo: video.RefInfo,
		BoardFrames: frames, Held: video.Held, Status: MuseJobQueued,
		OutputDir: video.OutputDir, Index: video.Index, Ratio: ratio,
		Files: []string{}, SavedFiles: []string{},
	}
}

// CheckPrompt says what a prompt attaches from the asset library and
// what may go wrong; it changes nothing.
func (p *Provider) CheckPrompt(prompt string, assetIDs []string) models.Response[MusePromptCheck] {
	return p.CheckPromptForSpec(prompt, "", assetIDs, MuseJobVideo, 0)
}

// CheckPromptForProject checks asset resolution scoped to project (plus global _chung).
func (p *Provider) CheckPromptForProject(prompt, project string, assetIDs []string) models.Response[MusePromptCheck] {
	return p.CheckPromptForSpec(prompt, project, assetIDs, MuseJobVideo, 0)
}

// CheckPromptForSpec checks asset resolution, warnings, and inline payload budget
// for the specified job kind and optional storyboard frame count.
func (p *Provider) CheckPromptForSpec(prompt, project string, assetIDs []string, kind MuseJobKind, boardFrames int) models.Response[MusePromptCheck] {
	if !kind.valid() {
		kind = MuseJobVideo
	}
	if kind != MuseJobVideo || boardFrames < 0 {
		boardFrames = 0
	} else if boardFrames > maxBoardFrames {
		boardFrames = maxBoardFrames
	}
	all := p.assets.List()
	fixed, err := p.assetsByID(assetIDs)
	if err != nil {
		return fail[MusePromptCheck](err, models.CodeInvalidArgument)
	}
	refs, info, tags, warnings := resolvePromptRefs(prompt, project, 0, fixed, all, nil, MuseRefShared)
	job := MuseJob{Kind: kind, Prompt: prompt, References: refs, RefInfo: info}
	estRefs := refs
	if boardFrames > 0 {
		job.BoardFrames = boardFrames
		job.Frames = make([]string, boardFrames)
		estRefs = append(slices.Clone(job.Frames), refs...)
	}
	est := p.estimateInlineBytes(job, estRefs)
	if est > museInlineRequestBudget {
		warnings = appendOnce(warnings, fmt.Sprintf("Ước tính dữ liệu gửi (~%.1f MB) vượt ngưỡng an toàn %d MB; nên bớt ảnh nếu gửi thực tế bị từ chối.", float64(est)/float64(1<<20), museInlineRequestBudget>>20))
	}
	return models.SuccessResponse(MusePromptCheck{
		Tags:           tags,
		Images:         len(refs),
		Warnings:       warnings,
		EstimatedBytes: est,
		BudgetBytes:    museInlineRequestBudget,
	})
}

// PreviewMessages composes the exact messages that would be sent to Muse for a
// prompt batch without writing anything to the journal or triggering work.
func (p *Provider) PreviewMessages(req MuseJobRequest) models.Response[[]MusePromptPreview] {
	if !req.Kind.valid() {
		return fail[[]MusePromptPreview](errors.New("Loại tác vụ không hợp lệ."), models.CodeInvalidArgument)
	}
	if !req.RefMode.valid() {
		return fail[[]MusePromptPreview](errors.New("Cách gắn ảnh không hợp lệ."), models.CodeInvalidArgument)
	}
	if err := checkFormat(req.Kind, req.Ratio, req.Duration); err != nil {
		return fail[[]MusePromptPreview](err, models.CodeInvalidArgument)
	}
	if req.Storyboard != nil {
		if req.Kind != MuseJobVideo {
			return fail[[]MusePromptPreview](errors.New("Storyboard chỉ dùng cho video."), models.CodeInvalidArgument)
		}
		if req.Storyboard.Frames < 1 || req.Storyboard.Frames > maxBoardFrames {
			return fail[[]MusePromptPreview](fmt.Errorf("Storyboard có 1 đến %d khung.", maxBoardFrames), models.CodeInvalidArgument)
		}
	}
	fixed, err := p.assetsByID(req.Assets)
	if err != nil {
		return fail[[]MusePromptPreview](err, models.CodeInvalidArgument)
	}
	var prompts []string
	for _, prompt := range req.Prompts {
		if prompt = strings.TrimSpace(prompt); prompt != "" {
			prompts = append(prompts, prompt)
		}
	}
	if len(prompts) == 0 {
		return fail[[]MusePromptPreview](errors.New("Hãy nhập prompt để xem trước."), models.CodeInvalidArgument)
	}
	all := p.assets.List()
	caps, _ := p.knownCapabilities()
	out := make([]MusePromptPreview, 0, len(prompts))
	for pi, prompt := range prompts {
		refs, info, tags, warnings := resolvePromptRefs(prompt, req.Project, pi, fixed, all, req.References, req.RefMode)
		files := len(refs)
		if req.Storyboard != nil {
			files += req.Storyboard.Frames
		}
		if files > 0 && caps == nil {
			warnings = appendOnce(warnings, errLimitsUnknown.Error())
		}
		job := MuseJob{
			Kind: req.Kind, Prompt: prompt, References: refs, RefInfo: info,
			Ratio: req.Ratio, Duration: req.Duration,
		}
		sbMsg := ""
		if req.Storyboard != nil {
			sbMsg = buildMessage(storyboardJob(job, req.Storyboard.Frames))
			job.BoardFrames = req.Storyboard.Frames
			job.Frames = make([]string, req.Storyboard.Frames)
		}
		allRefs := refs
		if len(job.Frames) > 0 {
			allRefs = append(slices.Clone(job.Frames), refs...)
		}
		est := p.estimateInlineBytes(job, allRefs)
		if est > museInlineRequestBudget {
			warnings = appendOnce(warnings, fmt.Sprintf("Ước tính dữ liệu gửi (~%.1f MB) vượt ngưỡng an toàn %d MB.", float64(est)/float64(1<<20), museInlineRequestBudget>>20))
		}
		out = append(out, MusePromptPreview{
			Prompt:            prompt,
			Message:           buildMessage(job),
			StoryboardMessage: sbMsg,
			Tags:              tags,
			Images:            len(refs),
			Warnings:          warnings,
			EstimatedBytes:    est,
			BudgetBytes:       museInlineRequestBudget,
		})
	}
	return models.SuccessResponse(out)
}
