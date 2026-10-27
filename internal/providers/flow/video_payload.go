package flow

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"SnakeMedia/internal/logger"
	"github.com/google/uuid"
)

// Video request payloads and reply parsing for Flow's batchexecute RPCs.

// BuildVideoTextRequestWithClientIDs builds the YhhmEf payload as captured by
// SolwStudio: [[item...], context, [batchID, 2]] where each item is
// [[null, null, [[[prompt]]]], variantKey, aspect, null, [null, null, null, null, clientID, clientID]].
// Flow's own client sends one item per requested output and, for any resolution
// other than 720p, adds field 8 = [resolution] (bundle builder u(); not in the capture).
func BuildVideoTextRequestWithClientIDs(prompt, projectID, variantKey string, aspect, resolution, count int, recaptchaToken string, clientIDs []string) ([]byte, error) {
	return buildVideoRequest(prompt, projectID, variantKey, aspect, resolution, count, nil, recaptchaToken, clientIDs)
}

// BuildVideoReferenceRequestWithClientIDs builds the MZZa6b payload as captured
// by SolwStudio: each item is [[null, null, [[[prompt]]]], [[null, mediaID]...],
// variantKey, aspect, null, tailIDs]; a resolution other than 720p goes in
// field 12 (bundle builder for t8a).
func BuildVideoReferenceRequestWithClientIDs(prompt, projectID, variantKey string, aspect, resolution, count int, mediaIDs []string, recaptchaToken string, clientIDs []string) ([]byte, error) {
	if len(mediaIDs) == 0 {
		return nil, errors.New("thiếu ảnh tham chiếu")
	}
	return buildVideoRequest(prompt, projectID, variantKey, aspect, resolution, count, mediaIDs, recaptchaToken, clientIDs)
}

func buildVideoRequest(prompt, projectID, variantKey string, aspect, resolution, count int, mediaIDs []string, recaptchaToken string, clientIDs []string) ([]byte, error) {
	if strings.TrimSpace(prompt) == "" || projectID == "" || variantKey == "" {
		return nil, errors.New("thiếu prompt, project ID hoặc model video")
	}
	if limit := maxVideosPerRequest(); count < 1 || count > limit {
		return nil, fmt.Errorf("số lượng video phải từ 1 đến %d", limit)
	}
	if len(clientIDs) != 0 && len(clientIDs) != count {
		return nil, errors.New("số client ID không khớp số video yêu cầu")
	}
	if aspect != 1 && aspect != 2 {
		return nil, fmt.Errorf("tỉ lệ video %d không được hỗ trợ", aspect)
	}
	newClientID := func() string { return strings.ToUpper(uuid.NewString()) }
	refs := make([]any, 0, len(mediaIDs))
	for _, id := range mediaIDs {
		refs = append(refs, []any{nil, id})
	}
	items := make([]any, 0, count)
	for i := 0; i < count; i++ {
		workflowID := newClientID()
		clientID := newClientID()
		if len(clientIDs) > 0 {
			clientID = strings.ToUpper(clientIDs[i])
		}
		item := []any{[]any{nil, nil, [][][]string{{{prompt}}}}}
		resolutionIndex := 7 // field 8 of a text request
		if len(refs) > 0 {
			item = append(item, refs)
			resolutionIndex = 11 // field 12 of a reference request
		}
		item = append(item, variantKey, aspect, nil, []any{nil, nil, nil, nil, workflowID, clientID})
		if resolution != videoResolution720p {
			for len(item) < resolutionIndex {
				item = append(item, nil)
			}
			item = append(item, []any{resolution})
		}
		items = append(items, item)
	}
	return json.Marshal([]any{items, flowContext(projectID, recaptchaToken), []any{newClientID(), 2}})
}

type pendingVideo struct {
	statusObserved bool
	clientID       string
	// operation identifies a generation whose media ID is not known yet
	// (reference-to-video replies name the workflow, not the media).
	operation string
	name      string
	status    int
	errMsg    string
	url       string
}

// parseVideoMedia reads media entries ([name, ..., status at [5][8][0]]) from a
// payload whose media list sits at listIndex.
func parseVideoMedia(payload string, listIndex int) ([]pendingVideo, error) {
	var root []any
	if err := json.Unmarshal([]byte(payload), &root); err != nil {
		return nil, errors.New("phản hồi video không đúng cấu trúc")
	}
	if len(root) <= listIndex {
		return nil, nil
	}
	var out []pendingVideo
	for _, m := range asList(root[listIndex]) {
		media := asList(m)
		if len(media) == 0 {
			continue
		}
		name, _ := media[0].(string)
		if name == "" {
			continue
		}
		pv := pendingVideo{name: name}
		if len(media) > 2 {
			pv.operation, _ = media[2].(string)
		}
		if len(media) > 5 {
			if holder := asList(media[5]); len(holder) > 8 {
				state := asList(holder[8])
				if len(state) > 0 {
					if code, ok := state[0].(float64); ok {
						pv.status = int(code)
						pv.statusObserved = true
					}
				}
				if len(state) > 1 && state[1] != nil {
					detail, _ := json.Marshal(state[1])
					pv.errMsg = string(detail)
				}
			}
		}
		if pv.statusObserved && pv.status == videoStatusSuccess {
			pv.url = videoOutputURL(media)
		}
		out = append(out, pv)
	}
	return out, nil
}

// parseVideoOperations reads the workflows of a submit reply,
// [null, n, [[operationID, projectID, sceneID, status, ...], ...]] (SolwStudio read_operation).
func parseVideoOperations(payload string) []pendingVideo {
	var root []any
	if json.Unmarshal([]byte(payload), &root) != nil || len(root) < 3 {
		return nil
	}
	var out []pendingVideo
	for _, r := range asList(root[2]) {
		if record := asList(r); len(record) > 0 {
			if id, _ := record[0].(string); id != "" {
				out = append(out, pendingVideo{operation: id})
			}
		}
	}
	return out
}

// listingMediaSlot finds the media ID that follows an operation in the project
// listing ([title, [sec, nanos], isVideo, null, mediaID, operationID, done]).
// Ported from SolwStudio's find_media_id_in_text; isVideo may be null or a bool.
var listingMediaSlot = regexp.MustCompile(`(?:null|true|false),null,\\?"([0-9a-fA-F-]{36})\\?"`)

// mediaForOperation returns the media ID the listing records for an operation.
func mediaForOperation(listing, operation string) string {
	start := strings.Index(strings.ToLower(listing), strings.ToLower(operation))
	if start < 0 {
		return ""
	}
	window := listing[start:min(len(listing), start+800)]
	if m := listingMediaSlot.FindStringSubmatch(window); m != nil {
		return m[1]
	}
	return ""
}

// videoURL returns the finished video URL inside a GetMedia payload, if any.
func videoURL(v any) string {
	switch x := v.(type) {
	case string:
		if strings.HasPrefix(x, "https://") && strings.Contains(x, videoURLMarker) {
			return x
		}
	case []any:
		for _, item := range x {
			if u := videoURL(item); u != "" {
				return u
			}
		}
	}
	return ""
}

func videoStatusText(code int) string {
	switch code {
	case 4:
		return "Flow báo tạo video thất bại"
	case 5:
		return "Flow đã huỷ video"
	case 7:
		return "Prompt bị bộ lọc an toàn của Flow chặn"
	case 0:
		return "Flow không còn theo dõi video này"
	}
	return fmt.Sprintf("Flow trả về trạng thái %d", code)
}

func videoFailureDetail(raw string) string {
	if strings.Contains(raw, publicErrorDangerFilter) {
		return "Flow đã từ chối yêu cầu này qua bộ lọc an toàn nội dung. Đây là phản hồi từ Flow, không phải lỗi kết nối. Hãy thử nội dung khác phù hợp với chính sách của Flow."
	}
	return raw
}

func editVideoFailureDetail(raw string) string {
	if strings.Contains(raw, publicErrorDangerFilter) {
		return "Flow đã từ chối video nguồn hoặc yêu cầu chỉnh sửa này qua bộ lọc an toàn nội dung. Đây là phản hồi từ Flow, không phải lỗi kết nối. Hãy thử video nguồn khác hoặc yêu cầu chỉnh sửa khác phù hợp với chính sách của Flow."
	}
	return videoFailureDetail(raw)
}

// maxLoggedReplyBytes bounds a debug-logged Flow reply.
const maxLoggedReplyBytes = 4000

// signedQuery matches the query string of a URL; Flow media URLs carry their
// access signature there, which must not end up in log files.
var signedQuery = regexp.MustCompile(`(https?://[^\s"'\\?]+)\?[^\s"'\\]*`)

func logRawVideoReply(rpcID, payload string) {
	if len(payload) > maxLoggedReplyBytes {
		payload = payload[:maxLoggedReplyBytes]
	}
	logger.Get().Debug("flow video reply", "rpc", rpcID, "payload", redactSignedURLs(payload))
}

func redactSignedURLs(text string) string {
	return signedQuery.ReplaceAllString(text, "$1?<redacted>")
}
