package flow

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"SnakeMedia/internal/providers/flow/chrome"
	"github.com/google/uuid"
)

// Image request payloads and reply parsing for Flow's batchexecute RPCs.

// BuildImageBatchRequestWithClientIDs constructs the verified batchexecute inner
// payload for BatchGenerateImages, reverse-engineered from the Flow web client,
// with stable client IDs that can be looked up after a lost reply. Every
// argument is required; nothing is replaced by a default.
func BuildImageBatchRequestWithClientIDs(prompt, projectID, ratio, modelID string, count int, recaptchaToken string, refMediaIDs, clientIDs []string) ([]byte, error) {
	if len(clientIDs) != count {
		return nil, errors.New("số client ID không khớp số ảnh yêu cầu")
	}
	return buildImageBatchRequest(prompt, projectID, ratio, modelID, count, recaptchaToken, refMediaIDs, clientIDs)
}

func buildImageBatchRequest(prompt, projectID, ratio, modelID string, count int, recaptchaToken string, refMediaIDs, clientIDs []string) ([]byte, error) {
	if limit := maxImagesPerRequest(); count < 1 || count > limit {
		return nil, fmt.Errorf("số lượng ảnh phải từ 1 đến %d", limit)
	}
	aspectCode, ok := FlowImageAspect[ratio]
	if !ok {
		return nil, fmt.Errorf("tỉ lệ ảnh %q không được hỗ trợ", ratio)
	}
	if modelID == "" {
		return nil, errors.New("thiếu model tạo ảnh")
	}
	if projectID == "" {
		return nil, errors.New("thiếu project ID")
	}

	var refs []any
	if len(refMediaIDs) > 0 {
		for _, mid := range refMediaIDs {
			refs = append(refs, []any{mid, nil, nil, nil, RefTypeImage})
		}
	}

	contextObj := flowContext(projectID, recaptchaToken)

	items := make([]any, 0, count)
	for i := 0; i < count; i++ {
		seed := rand.Intn(999999999) + 1
		clientUUID := strings.ToUpper(uuid.NewString())
		if len(clientIDs) > 0 {
			clientUUID = strings.ToUpper(clientIDs[i])
		}
		// Item structure (14 elements):
		item := []any{
			nil,
			nil,
			refs, // reference media items
			seed,
			aspectCode,
			modelID,
			nil,
			contextObj,
			[][][]string{{{prompt}}},
			nil,
			nil,
			nil,
			nil,
			clientUUID,
		}
		items = append(items, item)
	}

	outerUUID := strings.ToUpper(uuid.NewString())
	innerPayload := []any{
		nil,
		items,
		1,
		contextObj,
		[]string{outerUUID},
	}

	return json.Marshal(innerPayload)
}

// BuildImageUploadRequest constructs the batchexecute inner payload for RPC maseQ (UploadImage).
func BuildImageUploadRequest(imageBase64 string, mimeType string, fileName string, projectID string, recaptchaToken string) ([]byte, error) {
	if !strings.HasPrefix(mimeType, "image/") {
		return nil, fmt.Errorf("kiểu tệp %q không phải hình ảnh", mimeType)
	}
	if fileName == "" {
		return nil, errors.New("thiếu tên tệp ảnh tham chiếu")
	}
	if projectID == "" {
		return nil, errors.New("thiếu project ID")
	}

	uuid1 := strings.ToUpper(uuid.NewString())
	uuid2 := strings.ToUpper(uuid.NewString())

	innerPayload := []any{
		flowContext(projectID, recaptchaToken),
		imageBase64,
		mimeType,
		1,
		nil,
		nil,
		nil,
		nil,
		fileName,
		nil,
		uuid1,
		uuid2,
	}

	return json.Marshal(innerPayload)
}

// BuildImageUpsampleRequest constructs the verified UpsampleImage RPC request.
// Flow's client uses target-resolution enum 1 for 2K and 2 for 4K; 1K is the
// original generation and does not make this RPC.
func BuildImageUpsampleRequest(mediaID string, quality ImageQuality, projectID string, recaptchaToken string) ([]byte, error) {
	if strings.TrimSpace(mediaID) == "" {
		return nil, errors.New("thiếu media ID ảnh cần nâng độ phân giải")
	}
	if projectID == "" {
		return nil, errors.New("thiếu project ID")
	}
	var resolution int
	switch quality {
	case ImageQuality2K:
		resolution = 1
	case ImageQuality4K:
		resolution = 2
	default:
		return nil, fmt.Errorf("mức chất lượng %q không hỗ trợ nâng độ phân giải", quality)
	}
	return json.Marshal([]any{mediaID, resolution, flowContext(projectID, recaptchaToken)})
}

// ParseUpsampledImageBytes extracts the image bytes returned in the response's
// protobuf bytes field. The batchexecute JSON envelope can contain that field at
// different nesting depths, so candidates are accepted only when their decoded
// bytes have a supported raster image content type.
func ParseUpsampledImageBytes(payload string, maxBytes int64) ([]byte, string, error) {
	var root any
	if err := json.Unmarshal([]byte(payload), &root); err != nil {
		return nil, "", fmt.Errorf("phản hồi nâng độ phân giải không phải JSON hợp lệ: %w", err)
	}
	var imageData []byte
	var contentType string
	walkJSPB(root, func(value any) bool {
		switch item := value.(type) {
		case []any:
			if data, ok := numericImageBytes(item, maxBytes); ok {
				imageData, contentType = data, detectedImageType(data)
				return false
			}
		case string:
			if data := base64ImageBytes(item, maxBytes); data != nil {
				imageData, contentType = data, detectedImageType(data)
				return false
			}
		}
		return true
	})
	if imageData == nil {
		return nil, "", errors.New("Google Flow không trả về dữ liệu ảnh hợp lệ sau khi nâng độ phân giải")
	}
	return imageData, contentType, nil
}

// base64ImageBytes decodes s (optionally a data: URL) in any base64 alphabet
// and returns it when it is a supported raster image within maxBytes.
func base64ImageBytes(s string, maxBytes int64) []byte {
	if strings.HasPrefix(s, "data:") {
		if comma := strings.IndexByte(s, ','); comma >= 0 {
			s = s[comma+1:]
		}
	}
	for _, decoder := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		data, err := decoder.DecodeString(s)
		if err == nil && len(data) > 0 && int64(len(data)) <= maxBytes && detectedImageType(data) != "" {
			return data
		}
	}
	return nil
}

func numericImageBytes(values []any, maxBytes int64) ([]byte, bool) {
	if len(values) == 0 || int64(len(values)) > maxBytes {
		return nil, false
	}
	data := make([]byte, len(values))
	for i, value := range values {
		n, ok := value.(float64)
		if !ok || n < 0 || n > 255 || n != float64(int(n)) {
			return nil, false
		}
		data[i] = byte(n)
	}
	return data, detectedImageType(data) != ""
}

func detectedImageType(data []byte) string {
	typeName := http.DetectContentType(data)
	switch typeName {
	case "image/png", "image/jpeg", "image/webp":
		return typeName
	default:
		return ""
	}
}

func parseUploadedMediaID(raw []byte, rpcID string, sent ...string) (string, error) {
	payload, err := chrome.ParseRPCPayload(string(raw), rpcID)
	if err != nil {
		return "", fmt.Errorf("phản hồi tải ảnh lên không hợp lệ: %w", err)
	}
	skip := make(map[string]bool, len(sent))
	for _, id := range sent {
		skip[strings.ToLower(id)] = true
	}
	for _, m := range uuidRegex.FindAllString(payload, -1) {
		if !skip[strings.ToLower(m)] {
			return m, nil
		}
	}
	return "", errors.New("không tìm thấy media ID trong phản hồi tải ảnh lên")
}

// mediaPathMarkers identify generated image URLs inside the ogiZ0b payload.
var mediaPathMarkers = []string{"flow-content.google/image/", "ai-sandbox-videofx/image/"}

// ParseGeneratedImages walks every string of the decoded ogiZ0b payload (strings
// holding nested JSON included) and returns one item per media ID, in order.
// Decoding first keeps escaped query characters such as = and & intact.
func ParseGeneratedImages(payload string, prompt string, modelID string, ratio string) []GeneratedImageItem {
	var root any
	if err := json.Unmarshal([]byte(payload), &root); err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var items []GeneratedImageItem
	now := time.Now().UTC().Format(time.RFC3339)
	for _, str := range jspbStrings(root) {
		mediaID := generatedMediaID(str)
		if mediaID == "" || seen[mediaID] {
			continue
		}
		seen[mediaID] = true
		items = append(items, GeneratedImageItem{
			ID:          mediaID,
			URL:         str,
			Prompt:      prompt,
			Model:       modelID,
			AspectRatio: ratio,
			CreatedAt:   now,
		})
	}
	return items
}

// generatedMediaID returns the segment between "/image/" and the query of a
// generated image URL, or "" when s is not one.
func generatedMediaID(s string) string {
	if !strings.HasPrefix(s, "https://") {
		return ""
	}
	for _, marker := range mediaPathMarkers {
		if i := strings.Index(s, marker); i >= 0 {
			id := s[i+len(marker):]
			if j := strings.IndexAny(id, "?#/"); j >= 0 {
				id = id[:j]
			}
			return id
		}
	}
	return ""
}
