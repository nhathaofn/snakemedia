package flow

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"SnakeMedia/internal/engine"
)

// references reads reference images handed on by another node (files in the
// media store) into the form Flow's request carries. Only the media store is
// read, so a graph cannot point Flow at an arbitrary file.
func (r nodeRunners) references(refs []engine.MediaRef) ([]ReferenceImage, error) {
	out := make([]ReferenceImage, 0, len(refs))
	for _, ref := range refs {
		if ref.Kind == engine.MediaVideo {
			return nil, engine.Invalid(fmt.Sprintf("%s là video, không dùng làm ảnh tham chiếu được.", ref.LocalURL))
		}
		if r.media == nil {
			return nil, engine.Invalid("Chưa có thư mục media.")
		}
		path, err := r.media.Path(ref.LocalURL)
		if err != nil {
			return nil, engine.Invalid(err.Error())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("Không đọc được ảnh tham chiếu %s: %w", ref.LocalURL, err)
		}
		if len(data) > maxReferenceImageBytes() {
			return nil, engine.Invalid(fmt.Sprintf("Ảnh tham chiếu %s vượt %d MB.", filepath.Base(path), maxReferenceImageBytes()>>20))
		}
		mimeType := ref.MimeType
		if mimeType == "" {
			mimeType = strings.Split(http.DetectContentType(data), ";")[0]
		}
		if !slices.Contains(referenceMimeTypes(), mimeType) {
			return nil, engine.Invalid(fmt.Sprintf("Flow không nhận ảnh tham chiếu dạng %s (%s).", mimeType, filepath.Base(path)))
		}
		out = append(out, ReferenceImage{Base64: base64.StdEncoding.EncodeToString(data), MimeType: mimeType, FileName: filepath.Base(path)})
	}
	return out, nil
}

// mediaItem is one result of an image or video request, as nodes see it.
type mediaItem struct {
	jobID    string
	pending  bool
	localURL string
	err      string
}

func videoMediaItems(items []GeneratedVideoItem) []mediaItem {
	out := make([]mediaItem, 0, len(items))
	for _, it := range items {
		out = append(out, mediaItem{jobID: it.JobID, pending: it.Pending, localURL: it.LocalURL, err: it.Error})
	}
	return out
}

// collectMedia hands on the downloaded files. Work Flow still holds makes
// the node pending (never a success with missing files), naming the jobs to
// resume; nothing made at all is an error.
func collectMedia(items []mediaItem, kind engine.MediaKind) ([]engine.MediaRef, error) {
	files := []engine.MediaRef{}
	var pendingJobs, errs []string
	for _, it := range items {
		switch {
		case it.pending:
			if it.jobID != "" && !slices.Contains(pendingJobs, it.jobID) {
				pendingJobs = append(pendingJobs, it.jobID)
			}
		case it.localURL != "":
			files = append(files, engine.RefFor(it.localURL, kind))
		case it.err != "":
			errs = append(errs, it.err)
		}
	}
	if len(pendingJobs) > 0 {
		return files, engine.Pending("Flow vẫn đang xử lý; tiếp tục job đã gửi thay vì gửi lại.", pendingJobs)
	}
	if len(files) == 0 {
		if len(errs) > 0 {
			return files, errors.New(strings.Join(errs, "\n"))
		}
		return files, errors.New("Flow không trả tệp nào.")
	}
	return files, nil
}
