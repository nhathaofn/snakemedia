package muse

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"SnakeMedia/internal/models"
	"github.com/google/uuid"
)

// sourceVideoPrefix names the media files ImportSourceVideo creates. They are
// swept like references: once no job uses them and they are old enough.
const sourceVideoPrefix = "muse-ref-video-"

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

// planPrompts decides every prompt's images (promptRefs) and checks them
// against Muse's limits. The result is written into the jobs, so a job sent
// again attaches the same images whatever the library holds by then. Graph
// runs (runKey) use only their own images: their job IDs are derived from
// them, and a library edit must not make a re-run queue new jobs.
func (p *Provider) planPrompts(req MuseJobRequest, prompts []string, fixed []MuseAsset, runKey string) ([]promptPlan, error) {
	var all []MuseAsset
	if runKey == "" {
		all = p.assets.List()
	}
	caps, _ := p.knownCapabilities()
	plans := make([]promptPlan, len(prompts))
	for pi, prompt := range prompts {
		refs, info := promptRefs(prompt, pi, fixed, all, req.References, req.RefMode)
		if len(p.missingImages(refs)) > 0 {
			return nil, fmt.Errorf("Prompt %d: có ảnh tư liệu không còn trong thư mục media. Hãy sửa tư liệu đó.", pi+1)
		}
		files := len(refs)
		if req.Storyboard != nil {
			files += req.Storyboard.Frames
		}
		if req.SourceVideo != "" {
			files++
		}
		if files > 0 {
			if caps == nil {
				return nil, errLimitsUnknown
			}
			if files > caps.MaxFiles {
				return nil, fmt.Errorf("Prompt %d cần %d file, Muse nhận tối đa %d mỗi tin nhắn. Bớt tư liệu hoặc khung storyboard.", pi+1, files, caps.MaxFiles)
			}
		}
		plans[pi] = promptPlan{refs, info}
	}
	return plans, nil
}

// storyboardJob is the image job that draws a video job's opening frames.
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

// CheckPrompt tells the create screen what a prompt will attach from the
// asset library and what may go wrong; it changes nothing.
func (p *Provider) CheckPrompt(prompt string, assetIDs []string) models.Response[MusePromptCheck] {
	all := p.assets.List()
	fixed, err := p.assetsByID(assetIDs)
	if err != nil {
		return fail[MusePromptCheck](err, models.CodeInvalidArgument)
	}
	refs, info := promptRefs(prompt, 0, fixed, all, nil, MuseRefShared)
	tags := []string{}
	for _, a := range append(fixed, mentionedAssets(prompt, all)...) {
		tags = appendOnce(tags, a.Tag)
	}
	warnings := refWarnings(prompt, info, unknownTags(prompt, all))
	if warnings == nil {
		warnings = []string{}
	}
	return models.SuccessResponse(MusePromptCheck{Tags: tags, Images: len(refs), Warnings: warnings})
}

// VideoFilePatterns lists the videos Muse takes as file-picker patterns;
// empty until Muse's limits list any.
func (p *Provider) VideoFilePatterns() []string {
	caps, _ := p.knownCapabilities()
	out := []string{}
	for _, ext := range caps.videoExtensions() {
		out = append(out, "*"+ext)
	}
	return out
}

// ImportSourceVideo copies a picked video into the media folder for v2v.
func (p *Provider) ImportSourceVideo(path string) models.Response[MuseReference] {
	caps, _ := p.knownCapabilities()
	if caps == nil {
		return fail[MuseReference](errLimitsUnknown, models.CodeUnavailable)
	}
	if len(caps.VideoTypes) == 0 {
		return fail[MuseReference](errors.New("Muse chưa cho tài khoản gửi kèm video (hoặc giới hạn đọc trước khi app hỗ trợ video). Hãy kiểm tra lại một tài khoản Muse."), models.CodeUnavailable)
	}
	ext := strings.ToLower(filepath.Ext(path))
	mime, ok := caps.videoMime(ext)
	if !ok {
		return fail[MuseReference](fmt.Errorf("Muse không nhận video %s (nhận %s).", filepath.Base(path), strings.Join(caps.videoExtensions(), ", ")), models.CodeInvalidArgument)
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return fail[MuseReference](fmt.Errorf("Không đọc được %s.", filepath.Base(path)), models.CodeInvalidArgument)
	}
	if info.Size() > caps.MaxFileBytes {
		return fail[MuseReference](fmt.Errorf("%s lớn hơn %d MB, giới hạn một file của Muse.", filepath.Base(path), caps.MaxFileBytes>>20), models.CodeInvalidArgument)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fail[MuseReference](err, models.CodeInternal)
	}
	url, err := p.media.Put(sourceVideoPrefix+uuid.NewString(), mime, ext, data)
	if err != nil {
		return fail[MuseReference](err, models.CodeInternal)
	}
	return models.SuccessResponse(MuseReference{LocalURL: url, FileName: filepath.Base(path), Size: info.Size()})
}
