package chrome

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

// Scotty video upload, as the Flow web client does it (chunk_studio_1.js:
// SCOTTY_UPLOADER_FACTORY, resumable uploader AT, completion parser Yab). The
// file goes to /upload/v1/flow/upload/video/{projectID} with the resumable
// protocol: a "start" request answers with an upload URL, then one
// "upload, finalize" request carries the bytes and returns the media record.
const (
	videoUploadPath = "/upload/v1/flow/upload/video/"
	// videoUploadAttempts and videoUploadRetryDelay mirror Flow's retry loop
	// (two attempts, VIDEO_UPLOAD_RETRY_DELAY_MS = 500).
	videoUploadAttempts   = 2
	videoUploadRetryDelay = 500 * time.Millisecond
	// xsrfCookie holds the token Flow sends as X-Framework-Xsrf-Token.
	xsrfCookie = "XSRF-TOKEN"
	// maxUploadReplyBytes bounds the JSON media record returned by Scotty.
	maxUploadReplyBytes = 1 << 20
)

// VideoTrim selects the part of a video Flow keeps, in milliseconds.
type VideoTrim struct {
	StartMS int
	EndMS   int
}

// UploadedVideo is Flow's record of an uploaded video.
type UploadedVideo struct {
	MediaID     string
	WorkflowID  string
	DisplayName string
	Width       int
	Height      int
}

// UploadVideo sends a local video file to the project. trim may be nil to keep
// the whole video. Client errors (4xx other than 408/429) are not retried.
func (f *FlowClient) UploadVideo(ctx context.Context, projectID, filePath, fileName, contentType string, trim *VideoTrim, timeout time.Duration) (UploadedVideo, error) {
	if projectID == "" || strings.ContainsAny(projectID, "/?#") {
		return UploadedVideo{}, errors.New("project ID không hợp lệ")
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return UploadedVideo{}, fmt.Errorf("không đọc được tệp video: %w", err)
	}
	if info.Size() == 0 {
		return UploadedVideo{}, errors.New("tệp video rỗng")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastErr error
	for attempt := 1; attempt <= videoUploadAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return UploadedVideo{}, ctx.Err()
			case <-time.After(videoUploadRetryDelay):
			}
		}
		video, err := f.uploadVideoOnce(ctx, projectID, filePath, info.Size(), fileName, contentType, trim)
		if err == nil {
			return video, nil
		}
		lastErr = err
		var remote *HTTPError
		if ctx.Err() != nil || (errors.As(err, &remote) && remote.Status >= 400 && remote.Status < 500 && remote.Status != http.StatusRequestTimeout && remote.Status != http.StatusTooManyRequests) {
			break
		}
	}
	return UploadedVideo{}, lastErr
}

func (f *FlowClient) uploadVideoOnce(ctx context.Context, projectID, filePath string, size int64, fileName, contentType string, trim *VideoTrim) (UploadedVideo, error) {
	headers := f.videoUploadHeaders(fileName, trim)

	start, err := http.NewRequestWithContext(ctx, http.MethodPost, f.base+videoUploadPath+url.PathEscape(projectID), nil)
	if err != nil {
		return UploadedVideo{}, err
	}
	copyHeaders(start.Header, headers)
	start.Header.Set("X-Goog-Upload-Protocol", "resumable")
	start.Header.Set("X-Goog-Upload-Command", "start")
	start.Header.Set("X-Goog-Upload-Header-Content-Length", strconv.FormatInt(size, 10))
	start.Header.Set("X-Goog-Upload-Header-Content-Type", contentType)
	resp, err := f.send(ctx, start)
	if err != nil {
		return UploadedVideo{}, err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return UploadedVideo{}, responseError(resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	if status := resp.Header.Get("X-Goog-Upload-Status"); status != "active" {
		return UploadedVideo{}, fmt.Errorf("Flow không mở phiên tải video (trạng thái %q)", status)
	}
	target, err := f.uploadTarget(resp.Header.Get("X-Goog-Upload-URL"))
	if err != nil {
		return UploadedVideo{}, err
	}

	file, err := os.Open(filePath)
	if err != nil {
		return UploadedVideo{}, fmt.Errorf("không đọc được tệp video: %w", err)
	}
	defer file.Close()
	upload, err := http.NewRequestWithContext(ctx, http.MethodPost, target, file)
	if err != nil {
		return UploadedVideo{}, err
	}
	upload.ContentLength = size
	copyHeaders(upload.Header, headers)
	upload.Header.Set("Content-Type", contentType)
	upload.Header.Set("X-Goog-Upload-Command", "upload, finalize")
	upload.Header.Set("X-Goog-Upload-Offset", "0")
	resp, err = f.send(ctx, upload)
	if err != nil {
		return UploadedVideo{}, err
	}
	defer resp.Body.Close()
	data, err := readBounded(resp.Body, maxUploadReplyBytes)
	if err != nil {
		return UploadedVideo{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return UploadedVideo{}, responseError(resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	return ParseUploadedVideo(data, fileName)
}

// videoUploadHeaders are the headers Flow's uploader factory adds to every
// request of an upload: XSRF token (when the cookie exists), file name and trim.
func (f *FlowClient) videoUploadHeaders(fileName string, trim *VideoTrim) http.Header {
	h := http.Header{}
	h.Set("User-Agent", f.creds.UserAgent)
	h.Set("Accept-Language", f.creds.Language)
	h.Set("Origin", f.base)
	h.Set("Referer", f.base+f.creds.SourcePath)
	if token := f.cookieValue(xsrfCookie); token != "" {
		h.Set("X-Framework-Xsrf-Token", token)
	}
	if fileName != "" {
		h.Set("Slug", encodeURIComponent(fileName))
	}
	if trim != nil {
		h.Set("x-trim-start", strconv.Itoa(trim.StartMS))
		h.Set("x-trim-end", strconv.Itoa(trim.EndMS))
	}
	return h
}

// uploadTarget accepts the upload URL only on Google hosts over HTTPS.
func (f *FlowClient) uploadTarget(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || raw == "" {
		return "", errors.New("Flow không trả về địa chỉ tải video")
	}
	if !u.IsAbs() {
		u = f.origin.ResolveReference(u)
	}
	host := u.Hostname()
	if u.Scheme != "https" || u.User != nil || (host != f.origin.Hostname() && !strings.HasSuffix(host, ".google.com") && !strings.HasSuffix(host, ".googleapis.com")) {
		return "", fmt.Errorf("địa chỉ tải video %q không thuộc Google", host)
	}
	return u.String(), nil
}

func (f *FlowClient) send(ctx context.Context, req *http.Request) (*http.Response, error) {
	resp, err := f.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// net/url errors include the upload URL and its session parameters.
		return nil, errors.New("Không kết nối được máy chủ tải video của Flow.")
	}
	f.creds.Cookies = rotateCookies(f.creds.Cookies, f.origin, resp)
	return resp, nil
}

func (f *FlowClient) cookieValue(name string) string {
	for _, c := range f.creds.Cookies {
		if c.Name == name && cookieInScope(c.Domain, f.origin.Hostname()) {
			return c.Value
		}
	}
	return ""
}

func copyHeaders(dst, src http.Header) {
	for k, v := range src {
		dst[k] = append([]string(nil), v...)
	}
}

// encodeURIComponent matches JavaScript's: everything but A-Z a-z 0-9 - _ . ! ~ * ' ( ) is escaped.
func encodeURIComponent(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.IndexByte("-_.!~*'()", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// ParseUploadedVideo reads Scotty's completion record:
// {"media":{"name":".../media/ID","video":{"dimensions":{...}}},"mediaId":"ID",
// "workflow":{"name":".../workflows/ID","metadata":{"displayName":"..."}}}.
// Like Flow it accepts snake_case keys and falls back to the media name for the
// ID and to the media ID for the workflow.
func ParseUploadedVideo(data []byte, fileName string) (UploadedVideo, error) {
	data = bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(data), []byte(")]}'")))
	var reply map[string]any
	if err := json.Unmarshal(data, &reply); err != nil {
		return UploadedVideo{}, errors.New("phản hồi tải video không đúng cấu trúc")
	}
	field := func(obj map[string]any, camel, snake string) any {
		if v, ok := obj[camel]; ok {
			return v
		}
		return obj[snake]
	}
	object := func(obj map[string]any, camel, snake string) map[string]any {
		m, _ := field(obj, camel, snake).(map[string]any)
		return m
	}
	text := func(obj map[string]any, camel, snake string) string {
		s, _ := field(obj, camel, snake).(string)
		return s
	}
	media := object(reply, "media", "media")
	workflow := object(reply, "workflow", "workflow")

	id := text(reply, "mediaId", "media_id")
	if id == "" {
		id = text(media, "name", "name")
	}
	id = path.Base(id)
	if id == "" || id == "." || id == "/" {
		return UploadedVideo{}, errors.New("Flow không trả về mã video đã tải lên")
	}
	video := UploadedVideo{MediaID: id, WorkflowID: id, DisplayName: fileName}
	if name := text(workflow, "name", "name"); name != "" {
		video.WorkflowID = path.Base(name)
	} else if wf := text(media, "workflowId", "workflow_id"); wf != "" {
		video.WorkflowID = path.Base(wf)
	}
	if display := text(object(workflow, "metadata", "metadata"), "displayName", "display_name"); display != "" {
		if decoded, err := url.PathUnescape(display); err == nil {
			display = decoded
		}
		video.DisplayName = display
	}
	if dims := object(object(media, "video", "video"), "dimensions", "dimensions"); dims != nil {
		w, _ := dims["width"].(float64)
		h, _ := dims["height"].(float64)
		video.Width, video.Height = int(w), int(h)
	}
	return video, nil
}
