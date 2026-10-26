package muse

import (
	"context"

	"SnakeMedia/internal/engine"
)

// Node types the Muse provider contributes. A node queues the jobs and returns
// them; results arrive through job updates.
const NodeJobsSubmit = "muse.jobs.submit"

// ID implements providers.Provider.
func (p *Provider) ID() string { return "muse" }

// Name implements providers.Provider.
func (p *Provider) Name() string { return "Muse" }

// Nodes implements providers.Provider.
func (p *Provider) Nodes() []engine.Node {
	return []engine.Node{
		engine.Typed(engine.Spec{
			Type:        NodeJobsSubmit,
			Provider:    p.ID(),
			Label:       "Muse: tạo ảnh/video",
			Description: "Đưa mỗi prompt vào hàng đợi Muse, chia cho các tài khoản đang dùng được.",
			Inputs: []engine.Port{
				{Name: "prompts", Label: "Prompt", Type: engine.PortText, Multiple: true},
				{Name: "references", Label: "Ảnh tham chiếu", Type: engine.PortImage, Multiple: true, Optional: true},
			},
			Outputs: []engine.Port{{Name: "files", Label: "Kết quả", Type: engine.PortMedia, Multiple: true}},
			Params: []engine.Param{
				{Name: "kind", Label: "Loại", Kind: engine.ParamSelect, OptionsFrom: "muse.kinds"},
				{Name: "ratio", Label: "Tỉ lệ", Kind: engine.ParamSelect, OptionsFrom: "muse.ratios"},
				{Name: "count", Label: "Số ảnh", Kind: engine.ParamInteger, Min: 1, Max: MaxImagesPerJob},
			},
		}, func(_ context.Context, req MuseJobRequest) ([]MuseJob, error) {
			return engine.FromResponse(p.SubmitJobs(req))
		}),
	}
}
