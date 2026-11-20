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
	"sync"

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
	museRequestReserve      = 128 << 10
	museReferenceEdge       = 2048
	museReferenceQuality    = 85
	museReferenceMinEdge    = 128
	museReferenceMaxPixels  = 64 << 20
	attachReduceConcurrency = 4
)

func parallelIndex(n, workers int, fn func(int) error) error {
	if n <= 0 {
		return nil
	}
	if n == 1 || workers <= 1 {
		for i := range n {
			if err := fn(i); err != nil {
				return err
			}
		}
		return nil
	}
	w := min(n, workers)
	jobs := make(chan int)
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(w)
	for range w {
		go func() {
			defer wg.Done()
			for i := range jobs {
				errs[i] = fn(i)
			}
		}()
	}
	for i := range n {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

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
	var toReduce []int
	for i, att := range out {
		if att.Category != "image" {
			continue
		}
		if len(att.DataBase64) <= target && int64(base64.StdEncoding.DecodedLen(len(att.DataBase64))) <= caps.MaxFileBytes {
			continue
		}
		toReduce = append(toReduce, i)
	}
	if err := parallelIndex(len(toReduce), attachReduceConcurrency, func(idx int) error {
		att := out[toReduce[idx]]
		prepared, err := reduceAttachment(att, target, caps)
		if err != nil {
			return &inputError{msg: fmt.Sprintf("Không thể giảm dung lượng ảnh %s để gửi Muse: %v", att.Filename, err)}
		}
		out[toReduce[idx]] = prepared
		return nil
	}); err != nil {
		return nil, err
	}
	changed := len(toReduce)
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
	prevW, prevH := 0, 0
	bounds := img.Bounds()
	startEdge := min(museReferenceEdge, max(info.Width, info.Height))
	for edge := max(museReferenceMinEdge, startEdge); ; edge = max(museReferenceMinEdge, edge*3/4) {
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
		if w == prevW && h == prevH {
			if edge == museReferenceMinEdge {
				break
			}
			continue
		}
		prevW, prevH = w, h
		var scaled image.Image = img
		if w != bounds.Dx() || h != bounds.Dy() {
			dst := image.NewNRGBA(image.Rect(0, 0, w, h))
			draw.BiLinear.Scale(dst, dst.Bounds(), img, bounds, draw.Src, nil)
			scaled = dst
		}
		var encoded bytes.Buffer
		ext, mime := ".png", pngMime
		if useJPEG {
			ext, mime = ".jpg", jpegMime
			err = jpeg.Encode(&encoded, scaled, &jpeg.Options{Quality: museReferenceQuality})
		} else {
			err = png.Encode(&encoded, scaled)
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

const (
	maxCachedAttachments = 32
	attachmentTargetStep = 64 << 10
)

type attachmentCacheKey struct {
	path         string
	modTime      int64
	size         int64
	maxFileBytes int64
	targetBucket int
	capsSig      string
}

type cachedAttachment struct {
	ext        string
	mimeType   string
	dataBase64 string
}

// attachmentCache bounds repeated disk reads, image scaling and base64
// encoding when a batch reuses the same reference images across many prompts.
type attachmentCache struct {
	mu    sync.Mutex
	order []attachmentCacheKey
	items map[attachmentCacheKey]cachedAttachment
}

func newAttachmentCache() *attachmentCache {
	return &attachmentCache{items: make(map[attachmentCacheKey]cachedAttachment)}
}

func (c *attachmentCache) clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	clear(c.items)
	c.order = nil
	c.mu.Unlock()
}

func (c *attachmentCache) get(k attachmentCacheKey) (cachedAttachment, bool) {
	if c == nil {
		return cachedAttachment{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.items[k]
	return v, ok
}

func (c *attachmentCache) put(k attachmentCacheKey, v cachedAttachment) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.items[k]; !exists {
		if len(c.order) >= maxCachedAttachments {
			oldest := c.order[0]
			c.order = c.order[1:]
			delete(c.items, oldest)
		}
		c.order = append(c.order, k)
	}
	c.items[k] = v
}

func attachmentTargetBucket(target int) int {
	if target <= attachmentTargetStep {
		return target
	}
	return (target / attachmentTargetStep) * attachmentTargetStep
}

func capsSignature(caps *MuseCapabilities) string {
	if caps == nil {
		return ""
	}
	jpg, _ := caps.imageMime(".jpg")
	png, _ := caps.imageMime(".png")
	return jpg + "|" + png
}
