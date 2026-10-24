package media

import (
	"context"

	"SnakeMedia/internal/engine"
)

// NodeSave is the node type that copies stored media into a folder.
const NodeSave = "media.save"

// SaveRequest names a stored file and the folder to copy it into. A graph has
// no dialog, so the folder is a node setting; the studio pages ask the user.
type SaveRequest struct {
	LocalURL string `json:"localUrl"`
	Dir      string `json:"dir"`
}

// SaveNode exposes Save as an engine node.
func (s *Store) SaveNode() engine.Node {
	return engine.Typed(engine.Spec{
		Type:        NodeSave,
		Provider:    "media",
		Label:       "Lưu vào thư mục",
		Description: "Sao chép ảnh hoặc video đã tạo vào một thư mục, không ghi đè tệp có sẵn.",
		Inputs:      []engine.Port{{Name: "localUrl", Label: "Ảnh hoặc video", Type: engine.PortMedia}},
		Outputs:     []engine.Port{{Name: "path", Label: "Đường dẫn", Type: engine.PortText}},
		Params:      []engine.Param{{Name: "dir", Label: "Thư mục", Kind: engine.ParamText}},
	}, func(_ context.Context, req SaveRequest) (SaveResult, error) {
		if req.Dir == "" {
			return SaveResult{}, engine.Invalid("Chưa chọn thư mục lưu.")
		}
		return engine.FromResponse(s.Save(req.LocalURL, func(string) (string, error) { return req.Dir, nil }))
	})
}
