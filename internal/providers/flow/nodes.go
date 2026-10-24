package flow

import (
	"context"

	"SnakeMedia/internal/engine"
)

// Node types the Flow provider contributes. The studio pages call the provider
// methods directly today; a graph runner will call these same capabilities.
const (
	NodeImageGenerate = "flow.image.generate"
	NodeVideoGenerate = "flow.video.generate"
	NodeVideoEdit     = "flow.video.edit"
)

// ID implements providers.Provider.
func (s *Provider) ID() string { return "flow" }

// Name implements providers.Provider.
func (s *Provider) Name() string { return "Google Flow" }

// Nodes implements providers.Provider. Choices that depend on the account or the
// selected model are referenced through OptionsFrom, not copied into the spec.
func (s *Provider) Nodes() []engine.Node {
	prompt := engine.Port{Name: "prompt", Label: "Prompt", Type: engine.PortText}
	references := engine.Port{Name: "references", Label: "Ảnh tham chiếu", Type: engine.PortImage, Multiple: true, Optional: true}
	model := func(source string) engine.Param {
		return engine.Param{Name: "modelId", Label: "Model", Kind: engine.ParamSelect, OptionsFrom: source}
	}
	return []engine.Node{
		engine.Typed(engine.Spec{
			Type:        NodeImageGenerate,
			Provider:    s.ID(),
			Label:       "Tạo ảnh",
			Description: "Tạo ảnh từ prompt, có thể kèm ảnh tham chiếu.",
			Inputs:      []engine.Port{prompt, references},
			Outputs:     []engine.Port{{Name: "images", Label: "Ảnh", Type: engine.PortImage, Multiple: true}},
			Params: []engine.Param{
				model("flow.imageModels"),
				{Name: "aspectRatio", Label: "Tỉ lệ", Kind: engine.ParamSelect, OptionsFrom: "flow.imageAspectRatios"},
				{Name: "count", Label: "Số lượng", Kind: engine.ParamInteger, Min: 1, Max: MaxImagesPerRequest},
			},
		}, func(ctx context.Context, req GenerateImageRequest) ([]GeneratedImageItem, error) {
			return engine.FromResponse(s.GenerateImages(ctx, req))
		}),
		engine.Typed(engine.Spec{
			Type:        NodeVideoGenerate,
			Provider:    s.ID(),
			Label:       "Tạo video",
			Description: "Tạo video từ prompt, có thể kèm ảnh tham chiếu.",
			Inputs:      []engine.Port{prompt, references},
			Outputs:     []engine.Port{{Name: "videos", Label: "Video", Type: engine.PortVideo, Multiple: true}},
			Params: []engine.Param{
				model("flow.videoModels"),
				{Name: "aspectRatio", Label: "Tỉ lệ", Kind: engine.ParamSelect, OptionsFrom: "flow.videoModels.aspectRatios"},
				{Name: "duration", Label: "Thời lượng", Kind: engine.ParamSelect, OptionsFrom: "flow.videoModels.durations"},
				{Name: "resolution", Label: "Chất lượng", Kind: engine.ParamSelect, OptionsFrom: "flow.videoModels.resolutions"},
				{Name: "count", Label: "Số lượng", Kind: engine.ParamInteger, Min: 1, Max: MaxVideosPerRequest},
			},
		}, func(ctx context.Context, req GenerateVideoRequest) ([]GeneratedVideoItem, error) {
			return engine.FromResponse(s.GenerateVideos(ctx, req))
		}),
		engine.Typed(engine.Spec{
			Type:        NodeVideoEdit,
			Provider:    s.ID(),
			Label:       "Chỉnh sửa video",
			Description: "Viết lại một video có sẵn theo prompt; tỉ lệ và chất lượng theo video gốc.",
			Inputs:      []engine.Port{prompt, {Name: "sourceUrl", Label: "Video gốc", Type: engine.PortVideo}},
			Outputs:     []engine.Port{{Name: "videos", Label: "Video", Type: engine.PortVideo, Multiple: true}},
			// Ratio and quality follow the source video, as in Flow's editor.
			Params: []engine.Param{model("flow.editModels")},
		}, func(ctx context.Context, req EditVideoRequest) ([]GeneratedVideoItem, error) {
			return engine.FromResponse(s.EditVideos(ctx, req))
		}),
	}
}
