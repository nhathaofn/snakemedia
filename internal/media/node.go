package media

import (
	"context"

	"SnakeMedia/internal/engine"
)

// NodeSave is the node type that copies stored media into a folder.
const NodeSave = "media.save"

// SaveRequest names stored files and the folder to copy them into. A graph has
// no dialog, so the folder is a node setting; the studio pages ask the user.
type SaveRequest struct {
	Media []engine.MediaRef `json:"media"`
	Dir   string            `json:"dir"`
}

// SaveNodeResult lists where each file was saved.
type SaveNodeResult struct {
	Paths []string `json:"paths"`
}

// SaveNode exposes Save as an engine node.
func (s *Store) SaveNode() engine.Node {
	return engine.Typed(engine.Spec{
		Type:        NodeSave,
		Provider:    "media",
		Label:       "Lưu vào thư mục",
		Description: "Sao chép ảnh hoặc video đã tạo vào một thư mục, không ghi đè tệp có sẵn.",
		Inputs:      []engine.Port{{Name: "media", Label: "Ảnh hoặc video", Type: engine.PortMedia, Multiple: true}},
		Outputs:     []engine.Port{{Name: "paths", Label: "Đường dẫn", Type: engine.PortText, Multiple: true}},
		Params:      []engine.Param{{Name: "dir", Label: "Thư mục", Kind: engine.ParamText}},
	}, func(_ context.Context, req SaveRequest) (SaveNodeResult, error) {
		if req.Dir == "" {
			return SaveNodeResult{}, engine.Invalid("Chưa chọn thư mục lưu.")
		}
		dir, err := s.CheckDir(req.Dir)
		if err != nil {
			return SaveNodeResult{}, engine.Invalid(err.Error())
		}
		res := SaveNodeResult{Paths: []string{}}
		for _, m := range req.Media {
			saved, err := engine.FromResponse(s.Save(m.LocalURL, func(string) (string, error) { return dir, nil }))
			if err != nil {
				return res, err
			}
			res.Paths = append(res.Paths, saved.Path)
		}
		return res, nil
	})
}
