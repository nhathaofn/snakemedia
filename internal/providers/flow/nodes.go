package flow

import (
	"context"
	"fmt"
	"strconv"

	"SnakeMedia/internal/engine"
	"SnakeMedia/internal/media"
	"SnakeMedia/internal/models"
)

// Node types the Flow provider contributes. The studio pages call the provider
// methods directly; a graph runs these same capabilities through the nodes.
const (
	NodeImageGenerate = "flow.image.generate"
	NodeVideoGenerate = "flow.video.generate"
	NodeVideoEdit     = "flow.video.edit"
)

// ID implements providers.Provider.
func (s *Provider) ID() string { return "flow" }

// Name implements providers.Provider.
func (s *Provider) Name() string { return "Google Flow" }

// nodeRunners are the capabilities the nodes wrap; tests replace them.
type nodeRunners struct {
	media  *media.Store
	images func(context.Context, GenerateImageRequest) models.Response[[]GeneratedImageItem]
	videos func(context.Context, GenerateVideoRequest) models.Response[[]GeneratedVideoItem]
	edit   func(context.Context, EditVideoRequest) models.Response[[]GeneratedVideoItem]
	// rejected reports a request ID Flow definitely refused, so a graph run
	// may try that send again under the next derived ID.
	rejected func(kind, id string) bool
}

// Nodes implements providers.Provider. Choices that depend on the account or the
// selected model are referenced through OptionsFrom, not copied into the spec.
func (s *Provider) Nodes() []engine.Node {
	return flowNodes(s.ID(), nodeRunners{media: s.media, images: s.GenerateImages, videos: s.GenerateVideos, edit: s.EditVideos, rejected: s.requestRejected})
}

// requestRejected reports whether the journal of request id says Flow made
// nothing for it: a rejected generation, or an edit that ended without a file.
// A missing or unreadable journal is not a rejection.
func (s *Provider) requestRejected(kind, id string) bool {
	if kind == "edit" {
		var job editJob
		if err := s.readState("jobs", id, &job); err != nil || !job.Complete {
			return false
		}
		for _, item := range job.Results {
			if item.LocalURL != "" {
				return false
			}
		}
		return true
	}
	job, err := s.loadGenerationJob(id)
	return err == nil && job.Phase == generationRejected
}

// maxNodeAttempts bounds how many definitely refused sends one prompt of a
// graph run may retry under new IDs.
const maxNodeAttempts = 50

// requestID gives one send of a graph run a stable ID derived from the run
// key and the request's content, so running the same graph again resumes the
// send through Flow's journal instead of paying twice. Outside a graph run
// (no key) it is empty and the provider picks a fresh ID.
func (r nodeRunners) requestID(ctx context.Context, kind string, parts ...any) string {
	key := engine.RunKey(ctx)
	if key == "" {
		return ""
	}
	parts = append([]any{kind}, parts...)
	for attempt := range maxNodeAttempts {
		id := engine.RequestIDFor(key, attempt, parts...)
		if r.rejected == nil || !r.rejected(kind, id) {
			return id
		}
	}
	return ""
}

func refURLs(refs []engine.MediaRef) []string {
	urls := make([]string, len(refs))
	for i, ref := range refs {
		urls[i] = ref.LocalURL
	}
	return urls
}

// ImageNodeInput is the input of flow.image.generate: one image request per
// prompt, all with the same settings and references.
type ImageNodeInput struct {
	Prompt      engine.Texts      `json:"prompt"`
	References  []engine.MediaRef `json:"references"`
	ModelID     string            `json:"modelId"`
	AspectRatio string            `json:"aspectRatio"`
	Quality     ImageQuality      `json:"quality"`
	Count       int               `json:"count"`
}

// ImageNodeOutput hands the made images on.
type ImageNodeOutput struct {
	Images []engine.MediaRef `json:"images"`
}

// VideoNodeInput is the input of flow.video.generate: one video request per
// prompt.
type VideoNodeInput struct {
	Prompt      engine.Texts      `json:"prompt"`
	References  []engine.MediaRef `json:"references"`
	ModelID     string            `json:"modelId"`
	AspectRatio string            `json:"aspectRatio"`
	Duration    int               `json:"duration"`
	Resolution  string            `json:"resolution"`
	Count       int               `json:"count"`
}

// EditNodeInput is the input of flow.video.edit: the source video rewritten
// once per prompt.
type EditNodeInput struct {
	Prompt  engine.Texts    `json:"prompt"`
	Source  engine.MediaRef `json:"source"`
	ModelID string          `json:"modelId"`
}

// VideoNodeOutput hands the made videos on.
type VideoNodeOutput struct {
	Videos []engine.MediaRef `json:"videos"`
}

func flowNodes(provider string, r nodeRunners) []engine.Node {
	prompt := engine.Port{Name: "prompt", Label: "Prompt", Type: engine.PortText, Multiple: true}
	references := engine.Port{Name: "references", Label: "Ảnh tham chiếu", Type: engine.PortImage, Multiple: true, Optional: true}
	model := func(source string) engine.Param {
		return engine.Param{Name: "modelId", Label: "Model", Kind: engine.ParamSelect, OptionsFrom: source}
	}
	return []engine.Node{
		engine.Typed(engine.Spec{
			Type:        NodeImageGenerate,
			Provider:    provider,
			Label:       "Tạo ảnh",
			Description: "Tạo ảnh từ prompt, có thể kèm ảnh tham chiếu; mỗi prompt một yêu cầu.",
			Inputs:      []engine.Port{prompt, references},
			Outputs:     []engine.Port{{Name: "images", Label: "Ảnh", Type: engine.PortImage, Multiple: true}},
			Params: []engine.Param{
				model(OptionsImageModels),
				{Name: "aspectRatio", Label: "Tỉ lệ", Kind: engine.ParamSelect, OptionsFrom: OptionsImageAspectRatios},
				{Name: "quality", Label: "Chất lượng", Kind: engine.ParamSelect, OptionsFrom: OptionsImageQualities},
				{Name: "count", Label: "Số lượng", Kind: engine.ParamInteger, Min: 1, Max: maxImagesPerRequest()},
			},
		}, r.runImages),
		engine.Typed(engine.Spec{
			Type:        NodeVideoGenerate,
			Provider:    provider,
			Label:       "Tạo video",
			Description: "Tạo video từ prompt, có thể kèm ảnh tham chiếu; mỗi prompt một yêu cầu.",
			Inputs:      []engine.Port{prompt, references},
			Outputs:     []engine.Port{{Name: "videos", Label: "Video", Type: engine.PortVideo, Multiple: true}},
			Params: []engine.Param{
				model(OptionsVideoModels),
				{Name: "aspectRatio", Label: "Tỉ lệ", Kind: engine.ParamSelect, OptionsFrom: OptionsVideoAspectRatios},
				{Name: "duration", Label: "Thời lượng", Kind: engine.ParamSelect, OptionsFrom: OptionsVideoDurations},
				{Name: "resolution", Label: "Chất lượng", Kind: engine.ParamSelect, OptionsFrom: OptionsVideoResolutions},
				{Name: "count", Label: "Số lượng", Kind: engine.ParamInteger, Min: 1, Max: maxVideosPerRequest()},
			},
		}, r.runVideos),
		engine.Typed(engine.Spec{
			Type:        NodeVideoEdit,
			Provider:    provider,
			Label:       "Chỉnh sửa video",
			Description: "Viết lại một video có sẵn theo prompt; tỉ lệ và chất lượng theo video gốc.",
			Inputs:      []engine.Port{prompt, {Name: "source", Label: "Video gốc", Type: engine.PortVideo}},
			Outputs:     []engine.Port{{Name: "videos", Label: "Video", Type: engine.PortVideo, Multiple: true}},
			// Ratio and quality follow the source video, as in Flow's editor.
			Params: []engine.Param{model(OptionsEditModels)},
		}, r.runEdit),
	}
}

func (r nodeRunners) runImages(ctx context.Context, in ImageNodeInput) (ImageNodeOutput, error) {
	prompts := in.Prompt.NonEmpty()
	if len(prompts) == 0 {
		return ImageNodeOutput{}, engine.Invalid("Chưa có prompt.")
	}
	refs, err := r.references(in.References)
	if err != nil {
		return ImageNodeOutput{}, err
	}
	var all []mediaItem
	for i, p := range prompts {
		count := max(in.Count, 1)
		items, err := engine.FromResponse(r.images(ctx, GenerateImageRequest{
			RequestID: r.requestID(ctx, generationJobImage, i, p, in.ModelID, in.AspectRatio, in.Quality, count, refURLs(in.References)),
			Prompt:    p, ModelID: in.ModelID, AspectRatio: in.AspectRatio, Quality: in.Quality,
			Count: count, References: refs,
		}))
		if err != nil {
			return ImageNodeOutput{}, err
		}
		for _, it := range items {
			all = append(all, mediaItem{jobID: it.JobID, pending: it.Pending, localURL: it.LocalURL, err: it.Error})
		}
	}
	files, err := collectMedia(all, engine.MediaImage)
	return ImageNodeOutput{Images: files}, err
}

func (r nodeRunners) runVideos(ctx context.Context, in VideoNodeInput) (VideoNodeOutput, error) {
	prompts := in.Prompt.NonEmpty()
	if len(prompts) == 0 {
		return VideoNodeOutput{}, engine.Invalid("Chưa có prompt.")
	}
	refs, err := r.references(in.References)
	if err != nil {
		return VideoNodeOutput{}, err
	}
	var all []mediaItem
	for i, p := range prompts {
		count := max(in.Count, 1)
		items, err := engine.FromResponse(r.videos(ctx, GenerateVideoRequest{
			RequestID: r.requestID(ctx, generationJobVideo, i, p, in.ModelID, in.AspectRatio, in.Duration, in.Resolution, count, refURLs(in.References)),
			Prompt:    p, ModelID: in.ModelID, AspectRatio: in.AspectRatio, Duration: in.Duration,
			Resolution: in.Resolution, Count: count, References: refs,
		}))
		if err != nil {
			return VideoNodeOutput{}, err
		}
		all = append(all, videoMediaItems(items)...)
	}
	files, err := collectMedia(all, engine.MediaVideo)
	return VideoNodeOutput{Videos: files}, err
}

func (r nodeRunners) runEdit(ctx context.Context, in EditNodeInput) (VideoNodeOutput, error) {
	prompts := in.Prompt.NonEmpty()
	if len(prompts) == 0 {
		return VideoNodeOutput{}, engine.Invalid("Chưa có prompt.")
	}
	if in.Source.LocalURL == "" {
		return VideoNodeOutput{}, engine.Invalid("Chưa có video gốc.")
	}
	var all []mediaItem
	for i, p := range prompts {
		items, err := engine.FromResponse(r.edit(ctx, EditVideoRequest{
			RequestID: r.requestID(ctx, "edit", i, p, in.ModelID, in.Source.LocalURL),
			Prompt:    p, ModelID: in.ModelID, SourceURL: in.Source.LocalURL,
		}))
		if err != nil {
			return VideoNodeOutput{}, err
		}
		all = append(all, videoMediaItems(items)...)
	}
	files, err := collectMedia(all, engine.MediaVideo)
	return VideoNodeOutput{Videos: files}, err
}

// Option sources the Flow nodes read their choices from.
const (
	OptionsImageModels       = "flow.imageModels"
	OptionsImageAspectRatios = "flow.imageAspectRatios"
	OptionsImageQualities    = "flow.imageModels.qualities"
	OptionsVideoModels       = "flow.videoModels"
	OptionsVideoAspectRatios = "flow.videoModels.aspectRatios"
	OptionsVideoDurations    = "flow.videoModels.durations"
	OptionsVideoResolutions  = "flow.videoModels.resolutions"
	OptionsEditModels        = "flow.editModels"
)

// OptionSources implements providers.OptionProvider: every choice is read
// from the account's live Flow catalogue, as the studio pages do.
func (s *Provider) OptionSources() map[string]engine.OptionSource {
	imageModel := func(ctx context.Context, sel map[string]string) (FlowModel, error) {
		list, err := engine.FromResponse(s.GetImageModels(ctx))
		if err != nil {
			return FlowModel{}, err
		}
		return pickModel(list, sel["modelId"], func(m FlowModel) (string, bool) { return m.ID, m.Default })
	}
	videoModel := func(ctx context.Context, sel map[string]string) (FlowVideoModel, error) {
		list, err := engine.FromResponse(s.GetVideoModels(ctx))
		if err != nil {
			return FlowVideoModel{}, err
		}
		return pickModel(list, sel["modelId"], func(m FlowVideoModel) (string, bool) { return m.ID, m.Default })
	}
	return map[string]engine.OptionSource{
		OptionsImageModels: func(ctx context.Context, _ map[string]string) ([]engine.Option, error) {
			list, err := engine.FromResponse(s.GetImageModels(ctx))
			return optionsOf(list, func(m FlowModel) engine.Option { return engine.Option{Value: m.ID, Label: m.Name} }), err
		},
		OptionsImageAspectRatios: func(context.Context, map[string]string) ([]engine.Option, error) {
			return valueOptions(imageAspectRatios()), nil
		},
		OptionsImageQualities: func(ctx context.Context, sel map[string]string) ([]engine.Option, error) {
			m, err := imageModel(ctx, sel)
			return optionsOf(m.AvailableQualities, func(q ImageQuality) engine.Option {
				return engine.Option{Value: string(q), Label: string(q)}
			}), err
		},
		OptionsVideoModels: func(ctx context.Context, _ map[string]string) ([]engine.Option, error) {
			list, err := engine.FromResponse(s.GetVideoModels(ctx))
			return optionsOf(list, func(m FlowVideoModel) engine.Option { return engine.Option{Value: m.ID, Label: m.Name} }), err
		},
		OptionsVideoAspectRatios: func(ctx context.Context, sel map[string]string) ([]engine.Option, error) {
			m, err := videoModel(ctx, sel)
			return valueOptions(m.AspectRatios), err
		},
		OptionsVideoDurations: func(ctx context.Context, sel map[string]string) ([]engine.Option, error) {
			m, err := videoModel(ctx, sel)
			return optionsOf(m.Durations, func(d int) engine.Option {
				return engine.Option{Value: strconv.Itoa(d), Label: fmt.Sprintf("%d giây", d)}
			}), err
		},
		OptionsVideoResolutions: func(ctx context.Context, sel map[string]string) ([]engine.Option, error) {
			m, err := videoModel(ctx, sel)
			return valueOptions(m.Resolutions), err
		},
		OptionsEditModels: func(ctx context.Context, _ map[string]string) ([]engine.Option, error) {
			list, err := engine.FromResponse(s.GetEditModels(ctx))
			return optionsOf(list, func(m FlowEditModel) engine.Option { return engine.Option{Value: m.ID, Label: m.Name} }), err
		},
	}
}

// pickModel finds the selected model, or the catalogue's default (else the
// first) when none is selected.
func pickModel[T any](list []T, id string, key func(T) (string, bool)) (T, error) {
	var zero T
	if len(list) == 0 {
		return zero, engine.Invalid("Flow chưa trả model nào cho tài khoản.")
	}
	fallback := list[0]
	for _, m := range list {
		mid, isDefault := key(m)
		if id != "" && mid == id {
			return m, nil
		}
		if isDefault {
			fallback = m
		}
	}
	if id != "" {
		return zero, engine.Invalid(fmt.Sprintf("Model %q không có trong danh sách của Flow.", id))
	}
	return fallback, nil
}

func optionsOf[T any](list []T, option func(T) engine.Option) []engine.Option {
	out := make([]engine.Option, 0, len(list))
	for _, v := range list {
		out = append(out, option(v))
	}
	return out
}

func valueOptions(values []string) []engine.Option {
	return optionsOf(values, func(v string) engine.Option { return engine.Option{Value: v, Label: v} })
}
