package muse

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"path/filepath"
	"strings"
	"sync"

	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/media"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	// Muse normalizes incoming attachments cleanly when images are <= 2048px JPEG
	// and keeps total inline payloads within an 8 MB safe app ceiling.
	museInlineRequestBudget = 8 << 20
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
	maxFileBytes := int64(museInlineRequestBudget)
	if caps != nil && caps.MaxFileBytes > 0 {
		maxFileBytes = caps.MaxFileBytes
	}
	needsReduction := before > museInlineRequestBudget
	for _, att := range atts {
		if att.Category == "image" && int64(base64.StdEncoding.DecodedLen(len(att.DataBase64))) > maxFileBytes {
			needsReduction = true
		}
	}
	if !needsReduction {
		return atts, nil
	}
	if caps == nil {
		return nil, &inputError{msg: errLimitsUnknown.Error()}
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
		if len(att.DataBase64) <= target && int64(base64.StdEncoding.DecodedLen(len(att.DataBase64))) <= maxFileBytes {
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

func needsOutgoingJPEGNorm(data []byte, ext string, target int, caps *MuseCapabilities) bool {
	if caps == nil {
		return base64.StdEncoding.EncodedLen(len(data)) > target
	}
	if _, jpegOK := caps.imageMime(".jpg"); jpegOK {
		cleanExt := strings.ToLower(ext)
		if cleanExt != ".jpg" && cleanExt != ".jpeg" {
			return true
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || format != "jpeg" || max(cfg.Width, cfg.Height) > museReferenceEdge || jpegExifOrientation(data) > 1 {
			return true
		}
	}
	return base64.StdEncoding.EncodedLen(len(data)) > target || int64(len(data)) > caps.MaxFileBytes
}

func reduceAttachment(att Attachment, target int, caps *MuseCapabilities) (Attachment, error) {
	if caps == nil {
		return att, errLimitsUnknown
	}
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
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return att, err
	}
	if format == "jpeg" {
		if orient := jpegExifOrientation(data); orient > 1 {
			img = applyExifOrientation(img, orient)
			b := img.Bounds()
			info.Width, info.Height = b.Dx(), b.Dy()
		}
	}
	jpegMime, jpegOK := caps.imageMime(".jpg")
	pngMime, pngOK := caps.imageMime(".png")
	if !jpegOK && !pngOK {
		return att, fmt.Errorf("Muse không hỗ trợ định dạng phù hợp để giảm ảnh")
	}
	prevW, prevH := 0, 0
	bounds := img.Bounds()
	startEdge := min(museReferenceEdge, max(info.Width, info.Height))
	qualities := []int{museReferenceQuality, 78, 72, 65}
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
			xdraw.BiLinear.Scale(dst, dst.Bounds(), img, bounds, xdraw.Src, nil)
			scaled = dst
		}
		if jpegOK {
			opaqueImg := media.Opaque(scaled)
			for _, q := range qualities {
				var encoded bytes.Buffer
				if err := jpeg.Encode(&encoded, opaqueImg, &jpeg.Options{Quality: q}); err != nil {
					return att, err
				}
				if base64.StdEncoding.EncodedLen(encoded.Len()) <= target && int64(encoded.Len()) <= caps.MaxFileBytes {
					att.Filename = strings.TrimSuffix(att.Filename, filepath.Ext(att.Filename)) + ".jpg"
					att.MimeType, att.DataBase64 = jpegMime, base64.StdEncoding.EncodeToString(encoded.Bytes())
					return att, nil
				}
			}
		} else {
			var encoded bytes.Buffer
			if err := png.Encode(&encoded, scaled); err != nil {
				return att, err
			}
			if base64.StdEncoding.EncodedLen(encoded.Len()) <= target && int64(encoded.Len()) <= caps.MaxFileBytes {
				att.Filename = strings.TrimSuffix(att.Filename, filepath.Ext(att.Filename)) + ".png"
				att.MimeType, att.DataBase64 = pngMime, base64.StdEncoding.EncodeToString(encoded.Bytes())
				return att, nil
			}
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

// jpegExifOrientation reads TIFF IFD0 tag 0x0112 (Orientation, 1..8) from a JPEG
// APP1 Exif segment, returning 1 when missing or invalid.
func jpegExifOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}
	pos := 2
	for pos+4 <= len(data) {
		if data[pos] != 0xff {
			return 1
		}
		for pos < len(data) && data[pos] == 0xff {
			pos++
		}
		if pos >= len(data) {
			return 1
		}
		marker := data[pos]
		pos++
		if marker == 0xd9 || marker == 0xda {
			return 1
		}
		if marker == 0x00 || (marker >= 0xd0 && marker <= 0xd7) {
			continue
		}
		if pos+2 > len(data) {
			return 1
		}
		segLen := int(binary.BigEndian.Uint16(data[pos : pos+2]))
		if segLen < 2 || pos+segLen > len(data) {
			return 1
		}
		payload := data[pos+2 : pos+segLen]
		pos += segLen
		if marker == 0xe1 && len(payload) >= 14 && string(payload[:6]) == "Exif\x00\x00" {
			tiff := payload[6:]
			var order binary.ByteOrder
			switch string(tiff[:2]) {
			case "II":
				order = binary.LittleEndian
			case "MM":
				order = binary.BigEndian
			default:
				continue
			}
			if order.Uint16(tiff[2:4]) != 0x002a {
				continue
			}
			ifdOff := int(order.Uint32(tiff[4:8]))
			if ifdOff < 8 || ifdOff+2 > len(tiff) {
				continue
			}
			count := int(order.Uint16(tiff[ifdOff : ifdOff+2]))
			entriesStart := ifdOff + 2
			for i := 0; i < count && entriesStart+(i+1)*12 <= len(tiff); i++ {
				e := tiff[entriesStart+i*12 : entriesStart+(i+1)*12]
				if order.Uint16(e[0:2]) == 0x0112 {
					orient := int(order.Uint16(e[8:10]))
					if orient >= 1 && orient <= 8 {
						return orient
					}
					return 1
				}
			}
		}
	}
	return 1
}

func applyExifOrientation(src image.Image, orient int) image.Image {
	if orient <= 1 || orient > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	nrgba := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(nrgba, nrgba.Bounds(), src, b.Min, draw.Src)

	dw, dh := w, h
	if orient >= 5 {
		dw, dh = h, w
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch orient {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			so := nrgba.PixOffset(x, y)
			do := dst.PixOffset(dx, dy)
			copy(dst.Pix[do:do+4], nrgba.Pix[so:so+4])
		}
	}
	return dst
}
