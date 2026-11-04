package muse

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"path/filepath"
	"strings"

	"SnakeMedia/internal/logger"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	// Muse does not advertise its chat body limit. A real 22 MB inline
	// request was refused with invalid_params / length limit exceeded.
	// This is a conservative app budget, not a claimed platform limit.
	museInlineRequestBudget = 4 << 20
	// Leave space for the chat client's gateway envelope and metadata.
	museRequestReserve     = 128 << 10
	museReferenceEdge      = 2048
	museReferenceQuality   = 85
	museReferenceMinEdge   = 128
	museReferenceMaxPixels = 64 << 20
)

// inlineRequestBytes counts JSON escaping of the complete app message and
// base64 attachments. The reserve covers fields added by Muse's chat client.
func inlineRequestBytes(text string, atts []Attachment) int {
	b, _ := json.Marshal(struct {
		Message     string       `json:"message"`
		Attachments []Attachment `json:"attachments"`
	}{text, atts})
	return len(b) + museRequestReserve
}

// prepareAttachments changes only the copies sent to Muse. Library originals,
// image ordering, reference roles and the complete prompt stay unchanged.
func prepareAttachments(job MuseJob, atts []Attachment, caps *MuseCapabilities) ([]Attachment, error) {
	text := buildMessage(job)
	before := inlineRequestBytes(text, atts)
	// Count everything except image data first: videos and text cannot be
	// repaired by reducing reference images.
	imageCount := 0
	empty := append([]Attachment(nil), atts...)
	for i := range empty {
		if empty[i].Category == "image" {
			imageCount++
			empty[i].DataBase64 = ""
		}
	}
	room := museInlineRequestBudget - inlineRequestBytes(text, empty)
	if room <= 0 {
		return nil, &inputError{msg: "Nội dung lệnh hoặc video đính kèm quá lớn để gửi Muse. Hãy rút ngắn lệnh hoặc giảm dung lượng video."}
	}
	needsReduction := before > museInlineRequestBudget
	for _, att := range atts {
		if att.Category == "image" && int64(base64.StdEncoding.DecodedLen(len(att.DataBase64))) > caps.MaxFileBytes {
			needsReduction = true
		}
	}
	if !needsReduction {
		return atts, nil
	}
	if imageCount == 0 {
		return nil, &inputError{msg: "Video đính kèm quá lớn để gửi Muse. Hãy giảm dung lượng video."}
	}
	// Allow for the filename/MIME changing when PNG/WebP becomes JPEG.
	target := room/imageCount - 256
	if target <= 0 {
		return nil, &inputError{msg: "Không đủ dung lượng để đính kèm ảnh vào lệnh Muse."}
	}
	out := append([]Attachment(nil), atts...)
	changed := 0
	for i, att := range out {
		if att.Category != "image" {
			continue
		}
		if len(att.DataBase64) <= target && int64(base64.StdEncoding.DecodedLen(len(att.DataBase64))) <= caps.MaxFileBytes {
			continue
		}
		prepared, err := reduceAttachment(att, target, caps)
		if err != nil {
			return nil, &inputError{msg: fmt.Sprintf("Không thể giảm dung lượng ảnh %s để gửi Muse: %v", att.Filename, err)}
		}
		out[i] = prepared
		changed++
	}
	after := inlineRequestBytes(text, out)
	if after > museInlineRequestBudget {
		return nil, &inputError{msg: "Tổng dung lượng lệnh vẫn quá lớn để gửi Muse sau khi giảm ảnh."}
	}
	logger.Get().Info("muse reference images prepared", "job", job.ID, "attachments", len(out), "compressedImages", changed, "requestBytesBefore", before, "requestBytesAfter", after, "budgetBytes", museInlineRequestBudget)
	return out, nil
}

func reduceAttachment(att Attachment, target int, caps *MuseCapabilities) (Attachment, error) {
	data, err := base64.StdEncoding.DecodeString(att.DataBase64)
	if err != nil {
		return att, err
	}
	info, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return att, err
	}
	if info.Width <= 0 || info.Height <= 0 || int64(info.Width)*int64(info.Height) > museReferenceMaxPixels {
		return att, fmt.Errorf("kích thước ảnh vượt giới hạn xử lý")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return att, err
	}
	opaque := false
	if o, ok := img.(interface{ Opaque() bool }); ok {
		opaque = o.Opaque()
	}
	jpegMime, jpegOK := caps.imageMime(".jpg")
	pngMime, pngOK := caps.imageMime(".png")
	useJPEG := opaque && jpegOK
	if !useJPEG && !pngOK {
		return att, fmt.Errorf("Muse không hỗ trợ định dạng phù hợp để giảm ảnh")
	}
	for edge := museReferenceEdge; ; edge = max(museReferenceMinEdge, edge*3/4) {
		w, h := info.Width, info.Height
		if max(w, h) > edge {
			if w >= h {
				h = max(1, h*edge/w)
				w = edge
			} else {
				w = max(1, w*edge/h)
				h = edge
			}
		}
		dst := image.NewNRGBA(image.Rect(0, 0, w, h))
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Src, nil)
		var encoded bytes.Buffer
		ext, mime := ".png", pngMime
		if useJPEG {
			ext, mime = ".jpg", jpegMime
			err = jpeg.Encode(&encoded, dst, &jpeg.Options{Quality: museReferenceQuality})
		} else {
			err = png.Encode(&encoded, dst)
		}
		if err != nil {
			return att, err
		}
		if base64.StdEncoding.EncodedLen(encoded.Len()) <= target && int64(encoded.Len()) <= caps.MaxFileBytes {
			att.Filename = strings.TrimSuffix(att.Filename, filepath.Ext(att.Filename)) + ext
			att.MimeType, att.DataBase64 = mime, base64.StdEncoding.EncodeToString(encoded.Bytes())
			return att, nil
		}
		if edge == museReferenceMinEdge {
			break
		}
	}
	return att, fmt.Errorf("không thể giữ ảnh trong ngân sách dữ liệu của lệnh")
}
